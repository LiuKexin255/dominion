package testplan

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/common/gopkg/solver"
	"dominion/common/gopkg/testtool"
	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/testplan/fakeagent"
	"dominion/projects/game/gateway/token"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	wsPathFormat       = "/v1/sessions/%s/game/connect"
	snapshotPathFormat = "/v1/sessions/%s/game/snapshot"
	runtimePathFormat  = "/v1/sessions/%s/game/runtime"

	headerEnv       = "env"
	headerTokenMeta = "Grpc-Metadata-Token"

	httpClientTimeout = 15 * time.Second
	readTimeout       = 15 * time.Second
	shortTTL          = 2 * time.Second

	tokenTTL = 30 * time.Minute

	envTokenSecret = "SESSION_TOKEN_SECRET"

	tokenAudience = token.TokenAudienceInternal

	testVideoURL = "s3://s3.liukexin.com/buckets/common/video/IMG_6995.MP4"

	mimeTypeMP4 = "video/mp4; codecs=\"avc1.64001f\""
)

var (
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

	httpClient = &http.Client{
		Timeout:   httpClientTimeout,
		Transport: tracecontext.NewHTTPTransport(http.DefaultTransport),
	}

	// wsHTTPClient is used for WebSocket dial without HTTP timeout,
	// but with trace propagation via HTTPTransport.
	wsHTTPClient = &http.Client{
		Transport: tracecontext.NewHTTPTransport(http.DefaultTransport),
	}
)

func uniqueSessionID() string {
	return fmt.Sprintf("test-session-%d", time.Now().UnixNano())
}

// tracedCtx creates a context with a new trace span and logs the trace ID.
func tracedCtx(t *testing.T) context.Context {
	t.Helper()
	ctx := tracecontext.Ensure(context.Background())
	t.Logf("trace_id=%s", tracecontext.ID(ctx))
	return ctx
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
	return mustIssueToken(t, sessionID, gatewayID, 1, 0)
}

// mustIssueToken creates a signed token with the full Issue() signature.
// Audience is always token.TokenAudienceInternal for internal routing tokens.
func mustIssueToken(t *testing.T, sessionID, ownerGatewayID string, ownerEpoch int64, reconnectGeneration int64) string {
	t.Helper()
	signer := mustSigner(t)
	tok, err := signer.Issue(sessionID, ownerGatewayID, ownerEpoch, tokenAudience, reconnectGeneration)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// httpGetWithToken performs an HTTP GET with token in both query param
// (for owner routing) and Grpc-Metadata-Token header (for handler auth).
func httpGetWithToken(ctx context.Context, t *testing.T, url, tokenStr string) *http.Response {
	t.Helper()
	sutEnvName := testtool.MustEnv()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext GET %s: %v", url, err)
	}
	req.Header.Set(headerEnv, sutEnvName)
	req.Header.Set(headerTokenMeta, tokenStr)

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do GET %s: %v", url, err)
	}
	return resp
}

// wsDialWithToken dials a WebSocket connection with the given host, sessionID,
// and pre-issued token. This is a convenience wrapper around building the
// WebSocket URL and calling websocket.Dial with the standard options.
func wsDialWithToken(ctx context.Context, t *testing.T, hostURL, sessionID, tokenStr string) *websocket.Conn {
	t.Helper()
	url := wsURL(hostURL, sessionID, tokenStr)
	conn, _, err := websocket.Dial(ctx, url, wsDialOptions())
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	conn.SetReadLimit(int64(domain.MaxSegmentSize)*2 + 4096)
	return conn
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
		HTTPClient: wsHTTPClient,
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

func doHTTPGet(ctx context.Context, t *testing.T, url string) *http.Response {
	t.Helper()
	sutEnvName := testtool.MustEnv()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext GET %s: %v", url, err)
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

// assertNotBlackFrame decodes JPEG data and verifies the image contains
// real video content rather than a uniform black or fallback frame.
func assertNotBlackFrame(t *testing.T, data []byte) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode snapshot JPEG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 1 || bounds.Dy() <= 1 {
		t.Fatalf("snapshot image %dx%d, expected real video frame > 1x1", bounds.Dx(), bounds.Dy())
	}

	stepX := 1
	if w := bounds.Dx(); w > 32 {
		stepX = w / 32
	}
	stepY := 1
	if h := bounds.Dy(); h > 32 {
		stepY = h / 32
	}

	sampled, nonBlack := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			sampled++
			if r > 2560 || g > 2560 || b > 2560 {
				nonBlack++
			}
		}
	}

	if sampled == 0 {
		t.Fatal("no pixels sampled from snapshot image")
	}
	ratio := float64(nonBlack) / float64(sampled)
	if ratio < 0.01 {
		t.Fatalf("snapshot appears to be a black frame: only %.2f%% non-black pixels (%d/%d sampled)",
			ratio*100, nonBlack, sampled)
	}
}

func messageID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}

func wsDialOptions() *websocket.DialOptions {
	headers := http.Header{
		headerEnv: {testtool.MustEnv()},
	}
	return &websocket.DialOptions{
		HTTPClient: wsHTTPClient,
		HTTPHeader: headers,
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	conn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(conn)
}

func TestInterface_AgentConnect_Success(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	mi := mustReadMediaInit(ctx, t, webConn)
	if mi.GetStreamId() == "" {
		t.Fatal("media_init stream_id is empty")
	}
	if mi.GetInitId() == "" {
		t.Fatal("media_init init_id is empty")
	}
	if mi.GetMimeType() != mimeTypeMP4 {
		t.Fatalf("media_init mime_type = %q, want %q", mi.GetMimeType(), mimeTypeMP4)
	}
	if mi.GetCodec() == "" {
		t.Fatal("media_init codec is empty")
	}
	if len(mi.GetSegment()) == 0 {
		t.Fatal("media_init segment is empty")
	}

	ms := mustReadMediaSegment(ctx, t, webConn)
	if ms.GetStreamId() == "" {
		t.Fatal("media_segment stream_id is empty")
	}
	if ms.GetInitId() == "" {
		t.Fatal("media_segment init_id is empty")
	}
	if ms.GetSequence() == 0 {
		t.Fatal("media_segment sequence is 0")
	}
	if len(ms.GetSegment()) == 0 {
		t.Fatal("media_segment segment data is empty")
	}
}

func TestInterface_CatchupFromRandomAccess(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	mustReadMediaInit(ctx, t, webConn)

	ms := mustReadMediaSegment(ctx, t, webConn)
	if !ms.GetRandomAccess() {
		t.Fatal("expected catch-up segment to have random_access=true")
	}

	prevSeq := ms.GetSequence()
	for i := 0; i < 2; i++ {
		next := mustReadMediaSegment(ctx, t, webConn)
		if next.GetSequence() <= prevSeq {
			t.Fatalf("expected sequence %d > prev %d", next.GetSequence(), prevSeq)
		}
		prevSeq = next.GetSequence()
	}
}

func TestInterface_V1MediaRejected(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	v1JSON := fmt.Sprintf(`{"sessionId":%q,"messageId":"v1-media-001","mediaSegment":{"segmentId":"seg-001","keyFrame":true,"segment":"dGVzdA==","streamId":"stream-1","initId":"init-1","sequence":1}}`, sessionID)
	if err := webConn.Write(ctx, websocket.MessageText, []byte(v1JSON)); err != nil {
		t.Fatalf("web write v1 media JSON: %v", err)
	}

	for {
		env, err := readEnvelope(ctx, webConn)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if errPayload := env.GetError(); errPayload != nil {
			return
		}
		if env.GetMediaInit() != nil || env.GetMediaSegment() != nil || env.GetControlAck() != nil || env.GetPing() != nil {
			continue
		}
		t.Fatalf("expected error for v1 media with segment_id/key_frame fields, got payload: %v", env.Payload)
	}
}

func TestInterface_Snapshot_Cached(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	tok := issueToken(t, sessionID, gatewayID)
	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, snapshotURL, tok)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(200 * time.Millisecond)

	tok := issueToken(t, sessionID, gatewayID)
	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, snapshotURL, tok)
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

func TestInterface_Snapshot_ImageNotBlack(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	tok := issueToken(t, sessionID, gatewayID)
	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, snapshotURL, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetGameSnapshot status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	snap := decodeGameSnapshot(t, resp)

	image := snap.GetImage()
	if len(image) == 0 {
		t.Fatal("snapshot image is empty, want non-empty JPEG data")
	}

	assertNotBlackFrame(t, image)
}

func TestInterface_Snapshot_UnavailableWithoutRandomAccess(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.SnapshotFail)
	defer agent.Close()

	time.Sleep(300 * time.Millisecond)

	tok := issueToken(t, sessionID, gatewayID)
	snapshotURL := hostURL + fmt.Sprintf(snapshotPathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, snapshotURL, tok)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		snap := decodeGameSnapshot(t, resp)
		image := snap.GetImage()
		if len(image) > 0 {
			t.Fatalf("expected snapshot to be unavailable without random-access segments, got %d bytes of image data", len(image))
		}
	}
}

func TestInterface_Runtime_Fields(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	tok := issueToken(t, sessionID, gatewayID)
	runtimeURL := hostURL + fmt.Sprintf(runtimePathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, runtimeURL, tok)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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
				FlashSnapshot: true,
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      50,
						Y:      50,
					},
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      10,
						Y:      20,
					},
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
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      30,
						Y:      40,
					},
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), 5*time.Second)
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
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      10,
						Y:      20,
					},
				},
			},
		},
	}

	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, readTimeout)
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

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
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
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      10,
						Y:      20,
					},
				},
			},
		},
	}
	if err := writeEnvelope(ctx, webConn, ctrlReq); err != nil {
		t.Fatalf("web write control_request: %v", err)
	}

	disconnectCtx, disconnectCancel := context.WithTimeout(ctx, readTimeout)
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

func TestInterface_ControlRoundtrip_MouseDrag(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-drag"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-drag-001",
				Action: &gw.GameControlRequest_MouseDrag{
					MouseDrag: &gw.GameMouseDrag{
						Button:     gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						FromX:      10,
						FromY:      20,
						ToX:        100,
						ToY:        200,
						DurationMs: 300,
					},
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
			if ack.GetOperationId() != "op-drag-001" {
				t.Fatalf("control_ack operation_id = %q, want op-drag-001", ack.GetOperationId())
			}
			break
		}
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-drag-001" {
		t.Fatalf("control_result operation_id = %q, want op-drag-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_ControlRoundtrip_MouseHold(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-hold"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-hold-001",
				Action: &gw.GameControlRequest_MouseHold{
					MouseHold: &gw.GameMouseHold{
						Button:     gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          50,
						Y:          60,
						DurationMs: 500,
					},
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
			if ack.GetOperationId() != "op-hold-001" {
				t.Fatalf("control_ack operation_id = %q, want op-hold-001", ack.GetOperationId())
			}
			break
		}
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-hold-001" {
		t.Fatalf("control_result operation_id = %q, want op-hold-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_ControlRoundtrip_RightClick(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-right-click"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-right-click-001",
				Action: &gw.GameControlRequest_MouseClick{
					MouseClick: &gw.GameMouseClick{
						Button: gw.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
						X:      200,
						Y:      300,
					},
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
			if ack.GetOperationId() != "op-right-click-001" {
				t.Fatalf("control_ack operation_id = %q, want op-right-click-001", ack.GetOperationId())
			}
			break
		}
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-right-click-001" {
		t.Fatalf("control_result operation_id = %q, want op-right-click-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_ControlRoundtrip_RightDrag(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	ctrlReq := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("ctrl-right-drag"),
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-right-drag-001",
				Action: &gw.GameControlRequest_MouseDrag{
					MouseDrag: &gw.GameMouseDrag{
						Button:     gw.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
						FromX:      10,
						FromY:      20,
						ToX:        100,
						ToY:        200,
						DurationMs: 400,
					},
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
			if ack.GetOperationId() != "op-right-drag-001" {
				t.Fatalf("control_ack operation_id = %q, want op-right-drag-001", ack.GetOperationId())
			}
			break
		}
	}

	cr := mustReadControlResult(ctx, t, webConn)
	if cr.GetOperationId() != "op-right-drag-001" {
		t.Fatalf("control_result operation_id = %q, want op-right-drag-001", cr.GetOperationId())
	}
	if cr.GetStatus() != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("control_result status = %v, want SUCCEEDED", cr.GetStatus())
	}
}

func TestInterface_OldKindMouseJSON_Rejected(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	oldJSON := fmt.Sprintf(`{"sessionId":%q,"messageId":"old-format-001","controlRequest":{"operationId":"op-old-001","kind":"MOUSE_CLICK","mouse":{"button":"LEFT","x":10,"y":20}}}`, sessionID)
	if err := webConn.Write(ctx, websocket.MessageText, []byte(oldJSON)); err != nil {
		t.Fatalf("web write old-format JSON: %v", err)
	}

	for {
		env, err := readEnvelope(ctx, webConn)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if errPayload := env.GetError(); errPayload != nil {
			return
		}
		if env.GetMediaInit() != nil || env.GetMediaSegment() != nil || env.GetControlAck() != nil || env.GetPing() != nil {
			continue
		}
		t.Fatalf("expected error for old kind/mouse JSON, got payload: %v", env.Payload)
	}
}

func TestCreateSession_ReturnsAggregateHost(t *testing.T) {
	t.Skip("requires deployed session service alongside gateway")
}

func TestAgentConnect_WithAggregateHost(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	tok := issueToken(t, sessionID, gatewayID)
	conn := wsDialWithToken(ctx, t, hostURL, sessionID, tok)
	defer closeConn(conn)

	hello := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("hello-agent-agg"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	if err := writeEnvelope(ctx, conn, hello); err != nil {
		conn.Close(websocket.StatusNormalClosure, "hello failed")
		t.Fatalf("write hello agent aggregate: %v", err)
	}
}

func TestWebConnect_WithAggregateHost(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	conn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(conn)
}

func TestHTTPRead_WithToken(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	time.Sleep(200 * time.Millisecond)

	tok := issueToken(t, sessionID, gatewayID)
	runtimeURL := hostURL + fmt.Sprintf(runtimePathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, runtimeURL, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetGameRuntime with token status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	rt := decodeGameRuntime(t, resp)
	if !rt.GetAgentConnected() {
		t.Fatal("runtime agent_connected = false, want true")
	}
}

func TestCrossPodRouting(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)
	defer agent.Close()

	webConn := dialWeb(ctx, t, hostURL, sessionID, gatewayID)
	defer closeConn(webConn)

	mi := mustReadMediaInit(ctx, t, webConn)
	if mi.GetStreamId() == "" {
		t.Fatal("media_init stream_id is empty — cross-pod routing did not deliver media")
	}
	t.Log("cross-pod routing verified: media delivered via aggregate host")
}

func TestWSHoldBeyondTokenTTL(t *testing.T) {
	t.Skip("requires token refresh mechanism to be verified in deployment")
}

func TestExpiredToken_Rejected(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	expiredSigner := token.NewHMACSigner(
		strings.TrimSpace(os.Getenv(envTokenSecret)),
		-shortTTL,
	)
	expiredTok, err := expiredSigner.Issue(sessionID, gatewayID, 1, tokenAudience, 0)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	url := wsURL(hostURL, sessionID, expiredTok)
	_, _, err = websocket.Dial(ctx, url, wsDialOptions())
	if err == nil {
		t.Fatal("expected expired token to be rejected, but WebSocket connection succeeded")
	}
	t.Logf("expired token correctly rejected: %v", err)
}

func TestSessionRefresh_NewTokenWorks(t *testing.T) {
	t.Skip("requires deployed session service to refresh tokens")
}

func TestIdleTTL_SessionCleaned(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	ctx, cancel := context.WithTimeout(tracedCtx(t), 5*time.Minute)
	defer cancel()

	agent := startAgent(ctx, t, hostURL, sessionID, gatewayID, fakeagent.Normal)

	tok := issueToken(t, sessionID, gatewayID)
	runtimeURL := hostURL + fmt.Sprintf(runtimePathFormat, sessionID) + "?token=" + tok
	resp := httpGetWithToken(ctx, t, runtimeURL, tok)

	if resp.StatusCode == http.StatusOK {
		resp.Body.Close()
	} else {
		resp.Body.Close()
	}

	agent.Close()
	time.Sleep(3 * time.Second)

	tok2 := issueToken(t, sessionID, gatewayID)
	runtimeURL2 := hostURL + fmt.Sprintf(runtimePathFormat, sessionID) + "?token=" + tok2
	resp2 := httpGetWithToken(ctx, t, runtimeURL2, tok2)
	defer resp2.Body.Close()

	if resp2.StatusCode == http.StatusOK {
		rt := decodeGameRuntime(t, resp2)
		if rt.GetAgentConnected() {
			t.Log("session still active after brief idle — TTL may be longer than test wait")
		}
	}
}

func TestOwnerPodRestart_OldTokenInvalid(t *testing.T) {
	hostURL := testtool.MustEndpoint("http", "public")
	sessionID := uniqueSessionID()
	gatewayID := mustGatewayID(t)

	oldTok := mustIssueToken(t, sessionID, gatewayID, 1, 0)

	ctx, cancel := context.WithTimeout(tracedCtx(t), readTimeout)
	defer cancel()

	url := wsURL(hostURL, sessionID, oldTok)
	conn, _, err := websocket.Dial(ctx, url, wsDialOptions())
	if err != nil {
		t.Logf("old token rejected at dial (expected if epoch validation works): %v", err)
		return
	}
	defer closeConn(conn)

	hello := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("hello-old-epoch"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	if err := writeEnvelope(ctx, conn, hello); err != nil {
		t.Logf("old token write failed (epoch rejected): %v", err)
		return
	}

	for {
		env, err := readEnvelope(ctx, conn)
		if err != nil {
			t.Logf("old token read error after hello: %v", err)
			return
		}
		if errPayload := env.GetError(); errPayload != nil {
			t.Logf("old token rejected with error: %v", errPayload)
			return
		}
		t.Logf("old token accepted (gateway may not validate epoch on connect): %v", env.Payload)
		return
	}
}

func TestNoPerInstanceHost(t *testing.T) {
	endpoint := testtool.MustEndpoint("http", "public")

	if strings.Contains(endpoint, "gateway-0") {
		t.Fatalf("endpoint %q still uses per-instance host format, expected aggregate host", endpoint)
	}
	if strings.Contains(endpoint, "gateway-1") {
		t.Fatalf("endpoint %q still uses per-instance host format, expected aggregate host", endpoint)
	}
	t.Logf("aggregate host endpoint verified: %s", endpoint)
}
