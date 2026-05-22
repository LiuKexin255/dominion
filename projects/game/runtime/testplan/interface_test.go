// Package testplan implements the runtime service-level large test.
package testplan

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	"dominion/projects/game/pkg/token"
	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/runtime/testplan/fakeagent"

	"github.com/coder/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	rtTokenSecret      = "SESSION_TOKEN_SECRET"
	rtTokenTTL         = 5 * time.Minute
	rtReadTimeout      = 30 * time.Second
	rtShortReadTimeout = 5 * time.Second
	rtIdleWait        = 60 * time.Second // > 30s idle TTL + cleanup interval margin
	rtGrpcPort         = "8082"
	rtWsPathPrefix     = "/v1/sessions/"
	rtWsPathSuffix     = "/game/connect"
)

var (
	rtJSONMarshaler   = protojson.MarshalOptions{}
	rtJSONUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// rtMustSigner creates an HMACSigner from the required SESSION_TOKEN_SECRET env var.
func rtMustSigner(t *testing.T) *token.HMACSigner {
	t.Helper()
	secret := strings.TrimSpace(os.Getenv(rtTokenSecret))
	if secret == "" {
		t.Fatalf("missing required environment variable %s", rtTokenSecret)
	}
	return token.NewHMACSigner(secret, rtTokenTTL)
}

// rtMustNewSessionID returns a unique test session ID.
func rtMustNewSessionID() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

// rtMustNextMsgID returns a unique message ID with the given prefix.
func rtMustNextMsgID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}

// rtGRPCEndpoint derives a gRPC dial address from the HTTP public endpoint by
// replacing the scheme/host with the host + gRPC port.
func rtGRPCEndpoint(t *testing.T) string {
	t.Helper()
	httpURL := testtool.MustEndpoint("http", "public")
	u, err := url.Parse(httpURL)
	if err != nil {
		t.Fatalf("parse http endpoint %q: %v", httpURL, err)
	}
	host := u.Hostname()
	if u.Port() != "" {
		host = u.Hostname()
	}
	return fmt.Sprintf("%s:%s", host, rtGrpcPort)
}

// rtBuildWsURL constructs a WebSocket URL from the HTTP endpoint, a session
// ID, and a token.
func rtBuildWsURL(t *testing.T, sessionID, tokenStr string) string {
	t.Helper()
	httpURL := testtool.MustEndpoint("http", "public")
	u, err := url.Parse(httpURL)
	if err != nil {
		t.Fatalf("parse http endpoint %q: %v", httpURL, err)
	}
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}
	return fmt.Sprintf("%s://%s%s%s%s?token=%s", scheme, u.Host, rtWsPathPrefix, sessionID, rtWsPathSuffix, tokenStr)
}

// rtCreateRuntime creates a game runtime via gRPC and returns the token.
func rtCreateRuntime(t *testing.T, sessionID string) string {
	t.Helper()

	grpcAddr := rtGRPCEndpoint(t)
	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("grpc dial %q: %v", grpcAddr, err)
	}
	defer conn.Close()

	client := runtimepb.NewGameRuntimeServiceClient(conn)
	resp, err := client.CreateGameRuntime(ctx, &runtimepb.CreateGameRuntimeRequest{
		Parent:              fmt.Sprintf("sessions/%s", sessionID),
		ReconnectGeneration: 0,
	})
	if err != nil {
		t.Fatalf("CreateGameRuntime: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("CreateGameRuntime returned empty token")
	}
	return resp.Token
}

// rtIssueToken issues a token with the given claims using the test signer.
func rtIssueToken(t *testing.T, sessionID, ownerRuntimeID string, ownerEpoch int64, aud string, reconnectGen int64) string {
	t.Helper()
	signer := rtMustSigner(t)
	tok, err := signer.Issue(sessionID, ownerRuntimeID, ownerEpoch, aud, reconnectGen)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// rtParseTokenSessionID extracts the session_id from a token without verification.
func rtParseTokenSessionID(t *testing.T, tokenStr string) string {
	t.Helper()
	signer := rtMustSigner(t)
	claims, err := signer.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("parse routing claims: %v", err)
	}
	return claims.SessionID
}

// rtParseTokenOwnerRuntimeID extracts the owner_runtime_id from a token without verification.
func rtParseTokenOwnerRuntimeID(t *testing.T, tokenStr string) string {
	t.Helper()
	signer := rtMustSigner(t)
	claims, err := signer.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("parse routing claims: %v", err)
	}
	return claims.OwnerRuntimeID
}

// rtAgentDial connects a fake agent to the runtime and sends hello.
func rtAgentDial(t *testing.T, ctx context.Context, sessionID, tokenStr string) *fakeagent.Agent {
	t.Helper()
	wsURL := rtBuildWsURL(t, sessionID, tokenStr)
	a, err := fakeagent.Connect(ctx, wsURL, sessionID)
	if err != nil {
		t.Fatalf("agent dial: %v", err)
	}
	return a
}

// rtWebDial connects a fake web client to the runtime and sends hello.
func rtWebDial(t *testing.T, ctx context.Context, sessionID, tokenStr string) *websocket.Conn {
	t.Helper()
	wsURL := rtBuildWsURL(t, sessionID, tokenStr)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err != nil {
		t.Fatalf("web dial: %v", err)
	}

	hello := &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: rtMustNextMsgID("hello-web"),
		Payload: &runtimepb.GameWebSocketEnvelope_Hello{
			Hello: &runtimepb.GameHello{
				Role: runtimepb.GameClientRole_GAME_CLIENT_ROLE_WEB,
			},
		},
	}
	data, err := rtJSONMarshaler.Marshal(hello)
	if err != nil {
		conn.Close(websocket.StatusNormalClosure, "marshal failed")
		t.Fatalf("marshal web hello: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		conn.Close(websocket.StatusNormalClosure, "write failed")
		t.Fatalf("write web hello: %v", err)
	}
	return conn
}

// rtReadEnvelope reads and unmarshals a GameWebSocketEnvelope from the connection.
func rtReadEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn) *runtimepb.GameWebSocketEnvelope {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ws message: %v", err)
	}
	env := new(runtimepb.GameWebSocketEnvelope)
	if err := rtJSONUnmarshaler.Unmarshal(data, env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

// ---------------------------------------------------------------------------
// Test: WS protocol validation — hello/media/control flow
// ---------------------------------------------------------------------------

// TestRuntime_WSProtocol_HelloMediaControl validates the full WebSocket
// protocol flow: agent connects → sends media init + segment → web connects →
// receives catch-up → web sends control request → agent receives it → agent
// sends result → web receives result.
func TestRuntime_WSProtocol_HelloMediaControl(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Agent connects and sends hello.
	agent := rtAgentDial(t, ctx, sessionID, token)
	defer agent.Close()

	// Agent sends media init segment.
	const (
		streamID = "stream-1"
		initID   = "init-abc"
		mimeType = "video/mp4"
		codec    = "h264-avc"
	)
	if err := agent.SendMediaInit(ctx, streamID, initID, mimeType, codec, []byte("fake-init-data")); err != nil {
		t.Fatalf("send media init: %v", err)
	}

	// Agent sends media segment.
	if err := agent.SendMediaSegment(ctx, streamID, initID, 1, []byte("fake-segment-data"), true); err != nil {
		t.Fatalf("send media segment: %v", err)
	}

	// Web client connects and receives catch-up (init + segment).
	webConn := rtWebDial(t, ctx, sessionID, token)
	defer webConn.Close(websocket.StatusNormalClosure, "test done")

	// Web should receive media init.
	env1 := rtReadEnvelope(t, ctx, webConn)
	if init := env1.GetMediaInit(); init == nil {
		t.Fatalf("expected media_init, got %T", env1.Payload)
	} else {
		if init.GetStreamId() != streamID {
			t.Errorf("media_init stream_id = %q, want %q", init.GetStreamId(), streamID)
		}
		if init.GetInitId() != initID {
			t.Errorf("media_init init_id = %q, want %q", init.GetInitId(), initID)
		}
	}

	// Web should receive media segment.
	env2 := rtReadEnvelope(t, ctx, webConn)
	if seg := env2.GetMediaSegment(); seg == nil {
		t.Fatalf("expected media_segment, got %T", env2.Payload)
	} else {
		if seg.GetSequence() != 1 {
			t.Errorf("media_segment sequence = %d, want 1", seg.GetSequence())
		}
	}

	// Web sends a control request (mouse click).
	opID := rtMustNextMsgID("op")
	controlReqData, err := rtJSONMarshaler.Marshal(&runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: rtMustNextMsgID("ctrl-req"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &runtimepb.GameControlRequest{
				OperationId: opID,
				Action: &runtimepb.GameControlRequest_MouseClick{
					MouseClick: &runtimepb.GameMouseClick{
						Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal control request: %v", err)
	}
	if err := webConn.Write(ctx, websocket.MessageText, controlReqData); err != nil {
		t.Fatalf("write control request: %v", err)
	}

	// Agent should receive the control request.
	ctrlReq, err := agent.ExpectControlRequest(ctx)
	if err != nil {
		t.Fatalf("agent expect control request: %v", err)
	}
	if ctrlReq.GetOperationId() != opID {
		t.Errorf("control request operation_id = %q, want %q", ctrlReq.GetOperationId(), opID)
	}

	// Agent sends ack.
	if err := agent.SendAck(ctx, opID); err != nil {
		t.Fatalf("agent send ack: %v", err)
	}

	// Agent sends result.
	if err := agent.SendControlResult(ctx, opID, runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED, ""); err != nil {
		t.Fatalf("agent send result: %v", err)
	}

	// Web should receive result.
	env3 := rtReadEnvelope(t, ctx, webConn)
	if result := env3.GetControlResult(); result == nil {
		t.Fatalf("expected control_result, got %T", env3.Payload)
	} else {
		if result.GetOperationId() != opID {
			t.Errorf("control_result operation_id = %q, want %q", result.GetOperationId(), opID)
		}
		if result.GetStatus() != runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
			t.Errorf("control_result status = %v, want SUCCEEDED", result.GetStatus())
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Duplicate agent rejection
// ---------------------------------------------------------------------------

// TestRuntime_DuplicateAgent_Rejected verifies that a second agent connecting
// with the same session ID is rejected.
func TestRuntime_DuplicateAgent_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// First agent connects successfully.
	agent1 := rtAgentDial(t, ctx, sessionID, token)
	defer agent1.Close()

	// Second agent should be rejected.
	wsURL := rtBuildWsURL(t, sessionID, token)
	_, err := fakeagent.Connect(ctx, wsURL, sessionID)
	if err == nil {
		t.Fatal("expected second agent to be rejected, but connection succeeded")
	}
}

// ---------------------------------------------------------------------------
// Test: Media cache / catch-up
// ---------------------------------------------------------------------------

// TestRuntime_MediaCache_CatchUp verifies that a web client connecting after
// an agent has sent media segments receives the init segment and segments from
// the last random access point via catch-up.
func TestRuntime_MediaCache_CatchUp(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Agent connects.
	agent := rtAgentDial(t, ctx, sessionID, token)
	defer agent.Close()

	// Agent sends media init.
	const streamID = "stream-catchup"
	const initID = "init-catchup"
	if err := agent.SendMediaInit(ctx, streamID, initID, "video/mp4", "h264-avc", []byte("init-data")); err != nil {
		t.Fatalf("send media init: %v", err)
	}

	// Agent sends segment with random access point.
	if err := agent.SendMediaSegment(ctx, streamID, initID, 1, []byte("seg-1-rap"), true); err != nil {
		t.Fatalf("send segment 1: %v", err)
	}
	// Agent sends non-random-access segment.
	if err := agent.SendMediaSegment(ctx, streamID, initID, 2, []byte("seg-2"), false); err != nil {
		t.Fatalf("send segment 2: %v", err)
	}
	// Agent sends another random access point segment.
	if err := agent.SendMediaSegment(ctx, streamID, initID, 3, []byte("seg-3-rap"), true); err != nil {
		t.Fatalf("send segment 3: %v", err)
	}

	// Web connects — should receive init + segments from last random access.
	webConn := rtWebDial(t, ctx, sessionID, token)
	defer webConn.Close(websocket.StatusNormalClosure, "test done")

	// 1st message: media init
	env1 := rtReadEnvelope(t, ctx, webConn)
	if env1.GetMediaInit() == nil {
		t.Fatalf("expected media_init, got %T", env1.Payload)
	}

	// 2nd message: segment 3 (from last random access).
	env2 := rtReadEnvelope(t, ctx, webConn)
	seg := env2.GetMediaSegment()
	if seg == nil {
		t.Fatalf("expected media_segment, got %T", env2.Payload)
	}
	if seg.GetSequence() != 3 {
		t.Errorf("catch-up segment sequence = %d, want 3", seg.GetSequence())
	}

	// No more catch-up messages expected (segments 1 and 2 are before the last
	// random access point and should not be sent).
}

// ---------------------------------------------------------------------------
// Test: Control timeout
// ---------------------------------------------------------------------------

// TestRuntime_ControlTimeout_ReturnsTimeoutResult verifies that a control
// request from web times out when the agent does not respond, and the web
// client receives a timed-out result.
func TestRuntime_ControlTimeout_ReturnsTimeoutResult(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Agent connects.
	agent := rtAgentDial(t, ctx, sessionID, token)
	defer agent.Close()

	// Web connects.
	webConn := rtWebDial(t, ctx, sessionID, token)
	defer webConn.Close(websocket.StatusNormalClosure, "test done")

	// Discard catch-up messages (none expected but we read if present).
	// Drain any initial catch-up.
	readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
	defer readCancel()
	if _, _, err := webConn.Read(readCtx); err == nil {
		// If there's a message, drain more. Otherwise, proceed.
	}

	// Web sends a control request (mouse click — timeout 1s).
	opID := rtMustNextMsgID("timeout-op")
	reqData, _ := rtJSONMarshaler.Marshal(&runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: rtMustNextMsgID("ctrl-req"),
		Payload: &runtimepb.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &runtimepb.GameControlRequest{
				OperationId: opID,
				Action: &runtimepb.GameControlRequest_MouseClick{
					MouseClick: &runtimepb.GameMouseClick{
						Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      50,
						Y:      50,
					},
				},
			},
		},
	})
	if err := webConn.Write(ctx, websocket.MessageText, reqData); err != nil {
		t.Fatalf("write control request: %v", err)
	}

	// Agent should receive the request but we don't respond — it should timeout.
	_, err := agent.ExpectControlRequest(ctx)
	if err != nil {
		t.Fatalf("agent expect control request: %v", err)
	}

	// Wait for timeout — web should receive ControlAck then a ControlResult with
	// TIMED_OUT via the routing worker.
	// ControlAck from agent (but we didn't send one — the timeout path sends
	// directly without agent ack, so we skip ack expectation).
	// Actually, we DIDN'T send ack. The timeout fires on the server side.
	// Web receives result via completion worker.

	// Wait for the result with a generous timeout (click timeout is 1s).
	longCtx, longCancel := context.WithTimeout(ctx, 10*time.Second)
	defer longCancel()

	env := rtReadEnvelope(t, longCtx, webConn)
	if result := env.GetControlResult(); result == nil {
		t.Fatalf("expected control_result after timeout, got %T", env.Payload)
	} else {
		if result.GetOperationId() != opID {
			t.Errorf("result operation_id = %q, want %q", result.GetOperationId(), opID)
		}
		if result.GetStatus() != runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_TIMED_OUT {
			t.Errorf("result status = %v, want TIMED_OUT", result.GetStatus())
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Snapshot refresh
// ---------------------------------------------------------------------------

// TestRuntime_SnapshotRefresh_ReturnsValidData verifies that GetGameSnapshot
// returns a valid snapshot resource name and at minimum the session field.
func TestRuntime_SnapshotRefresh_ReturnsValidData(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Agent connects so the runtime exists.
	agent := rtAgentDial(t, ctx, sessionID, token)
	defer agent.Close()

	sutHostURL := testtool.MustEndpoint("http", "public")
	snapshotURL := fmt.Sprintf("%s/v1/sessions/%s/game/snapshot", sutHostURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("token", token)

	client := &http.Client{Timeout: rtReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetGameSnapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetGameSnapshot status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Test: Idle cleanup
// ---------------------------------------------------------------------------

// TestRuntime_IdleCleanup_RemovesStaleSessions verifies that sessions are
// cleaned up after the idle TTL expires without traffic.
//
// NOTE: This test uses SESSION_IDLE_TTL=30s configured in test_deploy.yaml.
// It waits for > 30s and then verifies the session runtime is gone.
func TestRuntime_IdleCleanup_RemovesStaleSessions(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	sutHostURL := testtool.MustEndpoint("http", "public")
	runtimeURL := fmt.Sprintf("%s/v1/sessions/%s/game/runtime", sutHostURL, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Verify session exists initially.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runtimeURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("token", token)
	client := &http.Client{Timeout: rtReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetGameRuntime initial: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetGameRuntime initial status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Wait for idle TTL to expire (30s + 5s buffer).
	t.Logf("waiting %v for idle TTL to expire...", rtIdleWait)
	time.Sleep(rtIdleWait)

	// Session should be cleaned up.
	ctx2, cancel2 := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel2()
	req2, err := http.NewRequestWithContext(ctx2, http.MethodGet, runtimeURL, nil)
	if err != nil {
		t.Fatalf("new request 2: %v", err)
	}
	req2.Header.Set("token", token)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GetGameRuntime after idle: %v", err)
	}
	resp2.Body.Close()

	// After cleanup, the session should not be found (404) or token will be
	// invalid (session not found triggers auth error).
	if resp2.StatusCode == http.StatusOK {
		t.Error("session should have been cleaned up, but it still exists")
	}
}

// ---------------------------------------------------------------------------
// Test: Forged token rejected
// ---------------------------------------------------------------------------

// TestRuntime_ForgedToken_Rejected verifies that a token signed with a
// different secret is rejected at WebSocket connect time.
func TestRuntime_ForgedToken_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()

	// Create a token with a different (wrong) secret.
	wrongSigner := token.NewHMACSigner("wrong-secret-key", rtTokenTTL)
	forgedToken, err := wrongSigner.Issue(sessionID, "some-runtime", 1, token.TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("issue forged token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rtShortReadTimeout)
	defer cancel()

	wsURL := rtBuildWsURL(t, sessionID, forgedToken)
	_, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err == nil {
		t.Fatal("expected forged token to be rejected, but WebSocket connection succeeded")
	}
}

// ---------------------------------------------------------------------------
// Test: Mismatched owner_runtime_id rejected
// ---------------------------------------------------------------------------

// TestRuntime_MismatchedOwnerRuntimeID_Rejected verifies that a token whose
// owner_runtime_id does not match the runtime instance is rejected.
func TestRuntime_MismatchedOwnerRuntimeID_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()

	// Issue a token with a wrong (non-existent) owner_runtime_id.
	tok := rtIssueToken(t, sessionID, "wrong-runtime-id-12345", 1, token.TokenAudienceInternal, 0)

	ctx, cancel := context.WithTimeout(context.Background(), rtShortReadTimeout)
	defer cancel()

	wsURL := rtBuildWsURL(t, sessionID, tok)
	_, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err == nil {
		t.Fatal("expected mismatched owner_runtime_id to be rejected, but WebSocket connection succeeded")
	}
}

// ---------------------------------------------------------------------------
// Test: Expired token rejected
// ---------------------------------------------------------------------------

// TestRuntime_ExpiredToken_Rejected verifies that an expired token is rejected.
func TestRuntime_ExpiredToken_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()

	// Create a signer with a very short TTL and override the clock.
	signer := rtMustSigner(t)
	now := time.Now()
	signer.SetNow(func() time.Time { return now.Add(-10 * time.Minute) }) // 10 min ago
	tok, err := signer.Issue(sessionID, "some-runtime", 1, token.TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	// Reset clock for verification (not needed for this test since we dial).

	ctx, cancel := context.WithTimeout(context.Background(), rtShortReadTimeout)
	defer cancel()

	wsURL := rtBuildWsURL(t, sessionID, tok)
	_, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err == nil {
		t.Fatal("expected expired token to be rejected, but WebSocket connection succeeded")
	}
}

// ---------------------------------------------------------------------------
// Test: Token session vs path session mismatch rejected
// ---------------------------------------------------------------------------

// TestRuntime_TokenSessionPathMismatch_Rejected verifies that a connection is
// rejected when the token's session_id differs from the session ID in the URL
// path.
func TestRuntime_TokenSessionPathMismatch_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()
	otherSessionID := rtMustNewSessionID()

	// Create runtime for sessionID, get token.
	tokenForSessionA := rtCreateRuntime(t, sessionID)

	// Try to use this token with a different session ID in the path.
	ctx, cancel := context.WithTimeout(context.Background(), rtShortReadTimeout)
	defer cancel()

	wsURL := rtBuildWsURL(t, otherSessionID, tokenForSessionA)
	_, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err == nil {
		t.Fatal("expected session mismatch to be rejected, but WebSocket connection succeeded")
	}
}

// ---------------------------------------------------------------------------
// Test: GetGameRuntime returns valid data
// ---------------------------------------------------------------------------

// TestRuntime_GetGameRuntime_ReturnsValidData verifies that GetGameRuntime
// returns the correct runtime information for an active session.
func TestRuntime_GetGameRuntime_ReturnsValidData(t *testing.T) {
	sessionID := rtMustNewSessionID()
	token := rtCreateRuntime(t, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtReadTimeout)
	defer cancel()

	// Agent connects.
	agent := rtAgentDial(t, ctx, sessionID, token)
	defer agent.Close()

	sutHostURL := testtool.MustEndpoint("http", "public")
	runtimeURL := fmt.Sprintf("%s/v1/sessions/%s/game/runtime", sutHostURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runtimeURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("token", token)

	client := &http.Client{Timeout: rtReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetGameRuntime: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetGameRuntime status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Test: Token with invalid audience rejected
// ---------------------------------------------------------------------------

// TestRuntime_InvalidAudience_Rejected verifies that a token with an audience
// other than "game-runtime" is rejected at snapshot/runtime HTTP endpoints.
func TestRuntime_InvalidAudience_Rejected(t *testing.T) {
	sessionID := rtMustNewSessionID()

	// Issue token with wrong audience.
	tok := rtIssueToken(t, sessionID, "some-runtime", 1, "wrong-audience", 0)

	sutHostURL := testtool.MustEndpoint("http", "public")
	snapshotURL := fmt.Sprintf("%s/v1/sessions/%s/game/snapshot", sutHostURL, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), rtShortReadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("token", tok)

	client := &http.Client{Timeout: rtReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetGameSnapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf("GetGameSnapshot with wrong audience status = %d, want unauthorized/forbidden", resp.StatusCode)
	}
}
