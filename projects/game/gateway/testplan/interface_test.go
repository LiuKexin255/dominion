package testplan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/pkg/solver"
	"dominion/pkg/testtool"
	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/testplan/fakeagent"
	"dominion/projects/game/pkg/token"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	wsPathFormat       = "/v1/sessions/%s/game/connect"
	snapshotPathFormat = "/v1/sessions/%s/game/snapshot"
	runtimePathFormat  = "/v1/sessions/%s/game/runtime"

	headerEnv = "env"

	httpClientTimeout = 15 * time.Second
	readTimeout       = 15 * time.Second

	tokenTTL = 30 * time.Minute

	envTokenSecret = "SESSION_TOKEN_SECRET"

	testVideoURL = "s3://s3.liukexin.com/buckets/common/video/IMG_6995.MP4"

	mimeTypeMP4 = "video/mp4; codecs=\"avc1.64001f\""
)

var (
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

	httpClient = &http.Client{Timeout: httpClientTimeout}
)

func uniqueSessionID() string {
	return fmt.Sprintf("test-session-%d", time.Now().UnixNano())
}

func mustSigner(t *testing.T) *token.HMACSigner {
	t.Helper()
	secretKey := strings.TrimSpace(os.Getenv(envTokenSecret))
	if secretKey == "" {
		t.Fatalf("missing required environment variable %s", envTokenSecret)
	}
	return token.NewHMACSigner(secretKey, tokenTTL)
}

func mustGatewayID(t *testing.T) string {
	t.Helper()
	resolver, err := solver.NewDeployStatefulResolver()
	if err != nil {
		t.Fatalf("create stateful resolver: %v", err)
	}
	target, err := solver.ParseTarget("game/gateway:http")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	instances, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		t.Fatalf("resolve gateway instances: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("no gateway instances found")
	}
	return instances[0].Hostname
}

func issueToken(t *testing.T, sessionID, gatewayID string) string {
	t.Helper()
	signer := mustSigner(t)
	tok, err := signer.Issue(sessionID, gatewayID, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

func wsURL(hostURL, sessionID, tokenStr string) string {
	scheme := "wss"
	if strings.HasPrefix(hostURL, "http://") {
		scheme = "ws"
		hostURL = strings.TrimPrefix(hostURL, "http://")
	} else {
		hostURL = strings.TrimPrefix(hostURL, "https://")
	}
	return fmt.Sprintf("%s://%s"+wsPathFormat+"?token=%s", scheme, hostURL, sessionID, tokenStr)
}

func dialWeb(ctx context.Context, t *testing.T, hostURL, sessionID, gatewayID string) *websocket.Conn {
	t.Helper()
	tok := issueToken(t, sessionID, gatewayID)
	url := wsURL(hostURL, sessionID, tok)

	conn, _, err := websocket.Dial(ctx, url, wsDialOptions())
	if err != nil {
		t.Fatalf("websocket.Dial web: %v", err)
	}
	conn.SetReadLimit(int64(domain.MaxSegmentSize)*2 + 4096)

	hello := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("hello-web"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WEB,
			},
		},
	}
	if err := writeEnvelope(ctx, conn, hello); err != nil {
		conn.Close(websocket.StatusNormalClosure, "hello failed")
		t.Fatalf("write hello web: %v", err)
	}

	return conn
}

func startAgent(ctx context.Context, t *testing.T, hostURL, sessionID, gatewayID string, scenario fakeagent.Scenario) *fakeagent.Agent {
	t.Helper()
	tok := issueToken(t, sessionID, gatewayID)
	url := wsURL(hostURL, sessionID, tok)
	agent := fakeagent.New(fakeagent.Config{
		ConnectURL: url,
		SessionID:  sessionID,
		EnvHeader:  testtool.MustEnv(),
		Scenario:   scenario,
		VideoURL:   testVideoURL,
	})
	errCh := make(chan error, 1)
	go func() {
		if err := agent.Run(ctx); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
	select {
	case <-agent.Ready():
		return agent
	case err := <-errCh:
		agent.Close()
		t.Fatalf("fakeagent exited before ready: %v", err)
		return nil
	case <-ctx.Done():
		agent.Close()
		t.Fatalf("context cancelled while waiting for fakeagent: %v", ctx.Err())
		return nil
	}
}

func readEnvelope(ctx context.Context, conn *websocket.Conn) (*gw.GameWebSocketEnvelope, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}

	env := new(gw.GameWebSocketEnvelope)
	if err := jsonUnmarshaler.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env *gw.GameWebSocketEnvelope) error {
	data, err := jsonMarshaler.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func mustReadEnvelope(ctx context.Context, t *testing.T, conn *websocket.Conn) *gw.GameWebSocketEnvelope {
	t.Helper()
	env, err := readEnvelope(ctx, conn)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	return env
}

func mustReadMediaInit(ctx context.Context, t *testing.T, conn *websocket.Conn) *gw.GameMediaInit {
	t.Helper()
	for {
		env := mustReadEnvelope(ctx, t, conn)
		if mi := env.GetMediaInit(); mi != nil {
			return mi
		}
	}
}

func mustReadMediaSegment(ctx context.Context, t *testing.T, conn *websocket.Conn) *gw.GameMediaSegment {
	t.Helper()
	for {
		env := mustReadEnvelope(ctx, t, conn)
		if ms := env.GetMediaSegment(); ms != nil {
			return ms
		}
	}
}

func mustReadControlResult(ctx context.Context, t *testing.T, conn *websocket.Conn) *gw.GameControlResult {
	t.Helper()
	for {
		env := mustReadEnvelope(ctx, t, conn)
		if cr := env.GetControlResult(); cr != nil {
			return cr
		}
	}
}

func doHTTPGet(t *testing.T, url string) *http.Response {
	t.Helper()
	sutEnvName := testtool.MustEnv()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequest GET %s: %v", url, err)
	}
	req.Header.Set(headerEnv, sutEnvName)

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do GET %s: %v", url, err)
	}
	return resp
}

func decodeGameSnapshot(t *testing.T, resp *http.Response) *gw.GameSnapshot {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}

	snap := new(gw.GameSnapshot)
	if err := jsonUnmarshaler.Unmarshal(body, snap); err != nil {
		t.Fatalf("protojson.Unmarshal(GameSnapshot): %v, body=%s", err, string(body))
	}
	return snap
}

func decodeGameRuntime(t *testing.T, resp *http.Response) *gw.GameRuntime {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}

	rt := new(gw.GameRuntime)
	if err := jsonUnmarshaler.Unmarshal(body, rt); err != nil {
		t.Fatalf("protojson.Unmarshal(GameRuntime): %v, body=%s", err, string(body))
	}
	return rt
}

func messageID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}

func wsDialOptions() *websocket.DialOptions {
	return &websocket.DialOptions{
		HTTPHeader: http.Header{
			headerEnv: {testtool.MustEnv()},
		},
	}
}

func closeConn(conn *websocket.Conn) {
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "test done")
	}
}

func TestInterface_WebConnect_Success(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	conn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(conn)
}

func TestInterface_AgentConnect_Success(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()
}

func TestInterface_PathSessionMismatch_Rejected(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	tok := issueToken(t, sessionID, gatewayID)
	url := wsURL(hostURL, "different-session-id", tok)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	_, _, err := websocket.Dial(ctx, url, wsDialOptions())
	if err == nil {
		t.Fatal("expected WebSocket connection to be rejected for session ID mismatch, but it succeeded")
	}
}

func TestInterface_DuplicateAgent_Rejected(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	tok2 := issueToken(t, sessionID, gatewayID)
	url2 := wsURL(hostURL, sessionID, tok2)

	conn2, _, err := websocket.Dial(ctx, url2, wsDialOptions())
	if err != nil {
		return
	}
	defer closeConn(conn2)

	hello2 := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("hello-agent-2"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	if err := writeEnvelope(ctx, conn2, hello2); err != nil {
		return
	}

	env, err := readEnvelope(ctx, conn2)
	if err != nil {
		return
	}

	if errPayload := env.GetError(); errPayload == nil {
		t.Fatalf("expected error for duplicate agent, got payload: %v", env.Payload)
	}
}

func TestInterface_MediaDelivery(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	mi := mustReadMediaInit(ctx, t, webConn)
	if mi.GetMimeType() != mimeTypeMP4 {
		t.Fatalf("media_init mime_type = %q, want %q", mi.GetMimeType(), mimeTypeMP4)
	}

	ms := mustReadMediaSegment(ctx, t, webConn)
	if ms.GetSegmentId() == "" {
		t.Fatal("media_segment segment_id is empty")
	}
}

func TestInterface_CatchupFromKeyframe(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	mustReadMediaInit(ctx, t, webConn)

	ms := mustReadMediaSegment(ctx, t, webConn)
	if !ms.GetKeyFrame() {
		t.Fatal("expected catch-up segment to be a keyframe")
	}
}

func TestInterface_Snapshot_Cached(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID)
	resp := doHTTPGet(t, snapshotURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetGameSnapshot status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	snap := decodeGameSnapshot(t, resp)
	wantName := fmt.Sprintf("sessions/%s/game/snapshot", sessionID)
	if snap.GetName() != wantName {
		t.Fatalf("snapshot name = %q, want %q", snap.GetName(), wantName)
	}
}

func TestInterface_Snapshot_Refresh(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(200 * time.Millisecond)

	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID)
	resp := doHTTPGet(t, snapshotURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetGameSnapshot (no cache) status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	snap := decodeGameSnapshot(t, resp)
	wantName := fmt.Sprintf("sessions/%s/game/snapshot", sessionID)
	if snap.GetName() != wantName {
		t.Fatalf("snapshot name = %q, want %q", snap.GetName(), wantName)
	}
}

func TestInterface_Runtime_Fields(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	runtimeURL := hostURL + fmt.Sprintf(runtimePathFormat, sessionID)
	resp := doHTTPGet(t, runtimeURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetGameRuntime status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	rt := decodeGameRuntime(t, resp)
	wantName := fmt.Sprintf("sessions/%s/game/runtime", sessionID)
	if rt.GetName() != wantName {
		t.Fatalf("runtime name = %q, want %q", rt.GetName(), wantName)
	}
	if !rt.GetAgentConnected() {
		t.Fatal("runtime agent_connected = false, want true")
	}
	if rt.GetGatewayId() == "" {
		t.Fatal("runtime gateway_id is empty, want non-empty")
	}
}

func TestInterface_ControlRoundtrip(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-req"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-click-001",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      100,
					Y:      200,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	for {
		env := mustReadEnvelope(ctx, t, webConn)
		if ack := env.GetControlAck(); ack != nil {
			if ack.GetOperationId() != "op-click-001" {
				t.Fatalf("control_ack operation_id = %q, want op-click-001", ack.GetOperationId())
			}
			break
		}
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-click-001" {
		t.Fatalf("control_result operation_id = %q, want op-click-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_FlashSnapshot(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-flash"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId:   "op-flash-001",
				Kind:          gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				FlashSnapshot: true,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      50,
					Y:      50,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-flash-001" {
		t.Fatalf("control_result operation_id = %q, want op-flash-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_ConcurrentOperations(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Timeout)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq1 := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-1"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-concurrent-001",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      10,
					Y:      20,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq1); err != nil {
		t.Fatalf("web write control_request 1: %v", err)
	}

	ctrlReq2 := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-2"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-concurrent-002",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      30,
					Y:      40,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq2); err != nil {
		t.Fatalf("web write control_request 2: %v", err)
	}

	for {
		env := mustReadEnvelope(ctx, t, webConn)
		if errPayload := env.GetError(); errPayload != nil {
			return
		}
	}
}

func TestInterface_TimeoutSemantics(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Timeout)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-timeout"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-timeout-001",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      10,
					Y:      20,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), readTimeout)
	defer timeoutCancel()

	for {
		env, err := readEnvelope(timeoutCtx, webConn)
		if err != nil {
			t.Fatalf("web read timed_out result: %v", err)
		}
		if ctrlResult := env.GetControlResult(); ctrlResult != nil {
			if ctrlResult.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_TIMED_OUT {
				t.Fatalf("control_result status = %v, want TIMED_OUT", ctrlResult.GetStatus())
			}
			return
		}
	}
}

func TestInterface_AgentDisconnect_Cleanup(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Disconnect)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-disconnect"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-disconnect-001",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
				Mouse: &gw.GameMouseAction{
					Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
					X:      10,
					Y:      20,
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), readTimeout)
	defer disconnectCancel()

	for {
		env, err := readEnvelope(disconnectCtx, webConn)
		if err != nil {
			t.Fatalf("web read failed result after agent disconnect: %v", err)
		}
		if ctrlResult := env.GetControlResult(); ctrlResult != nil {
			if ctrlResult.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED {
				t.Fatalf("control_result status = %v, want FAILED", ctrlResult.GetStatus())
			}
			return
		}
	}
}
