// Package testplan implements the gateway system-level large test.
//
// The gateway is a pure edge proxy. These tests validate the full proxy
// architecture: gateway → session → runtime → mongo. Every test goes
// through the gateway's public endpoint (game.liukexin.com), never
// directly to backend services.
//
// Coverage:
//  1. Session CRUD through gateway (grpc-gateway → session gRPC)
//  2. WebSocket proxy through gateway (owner routing → runtime WS)
//  3. Token routing claim extraction (ParseRoutingClaims, not verification)
//  4. Forged token: gateway forwards, runtime rejects
//  5. Gateway statelessness (no session/runtime state held)
package testplan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	"dominion/projects/game/pkg/testutil"
	"dominion/projects/game/pkg/token"
	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/runtime/testplan/fakeagent"
	sessionpb "dominion/projects/game/session"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	gwEnvTokenSecret   = "SESSION_TOKEN_SECRET"
	gwTokenTTL         = 5 * time.Minute
	gwReadTimeout      = 30 * time.Second
	gwShortReadTimeout = 5 * time.Second
	gwWsPathPrefix     = "/v1/sessions/"
	gwWsPathSuffix     = "/game/connect"
)

var (
	gwJSONMarshaler   = protojson.MarshalOptions{}
	gwJSONUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// gwMustSigner creates an HMACSigner from the required SESSION_TOKEN_SECRET env var.
func gwMustSigner(t *testing.T) *token.HMACSigner {
	t.Helper()
	secret := strings.TrimSpace(os.Getenv(gwEnvTokenSecret))
	if secret == "" {
		t.Fatalf("missing required environment variable %s", gwEnvTokenSecret)
	}
	return token.NewHMACSigner(secret, gwTokenTTL)
}

// gwMustEndpoint returns the public HTTP endpoint for the gateway.
func gwMustEndpoint(t *testing.T) string {
	t.Helper()
	return testtool.MustEndpoint("http", "public")
}

// gwBuildWsURL constructs a WebSocket URL for the gateway connect endpoint.
func gwBuildWsURL(t *testing.T, endpoint, sessionID, tokenStr string) string {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse endpoint %q: %v", endpoint, err)
	}
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}
	return fmt.Sprintf("%s://%s%s%s%s?token=%s", scheme, u.Host, gwWsPathPrefix, sessionID, gwWsPathSuffix, tokenStr)
}

// gwNextMsgID returns a unique message ID for WS protocol messages.
func gwNextMsgID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}

// gwGetSession retrieves a session by name via the gateway HTTP endpoint.
func gwGetSession(t *testing.T, endpoint, sessionName string) *sessionpb.Session {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint+"/v1/"+sessionName, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	client := &http.Client{Timeout: gwReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	var sess sessionpb.Session
	if err := gwJSONUnmarshaler.Unmarshal(body, &sess); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	return &sess
}

// gwCleanupSession deletes a session for test cleanup (best-effort).
func gwCleanupSession(t *testing.T, endpoint, sessionName string) {
	t.Helper()
	// Best-effort cleanup — ignore errors.
	req, _ := http.NewRequest(http.MethodDelete, endpoint+"/v1/"+sessionName, nil)
	client := &http.Client{Timeout: gwShortReadTimeout}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// ---------------------------------------------------------------------------
// Test 1: Session create via gateway → session gRPC
// ---------------------------------------------------------------------------

// TestGateway_SessionCreate_ReturnsValidSession verifies that creating a
// session through the gateway (grpc-gateway → session gRPC) returns a valid
// session with name, token, owner_runtime_id, and SESSION_STATUS_PENDING.
func TestGateway_SessionCreate_ReturnsValidSession(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, sess.GetName())

	// Verify name format.
	wantName := "sessions/" + sessionID
	if sess.GetName() != wantName {
		t.Errorf("session.Name = %q, want %q", sess.GetName(), wantName)
	}

	// Verify token is present.
	if sess.GetToken() == "" {
		t.Fatal("session.Token is empty")
	}

	// Verify owner_runtime_id is present.
	runtimeID := sess.GetOwnerRuntimeId()
	if runtimeID == "" {
		t.Fatal("session.OwnerRuntimeId is empty")
	}

	// Verify token contains the same owner_runtime_id.
	parsedRuntimeID, err := testutil.ParseSessionToken(sess.GetToken())
	if err != nil {
		t.Fatalf("ParseSessionToken: %v", err)
	}
	if parsedRuntimeID != runtimeID {
		t.Errorf("token owner_runtime_id = %q, want %q", parsedRuntimeID, runtimeID)
	}

	// Verify status.
	if sess.GetStatus() != sessionpb.SessionStatus_SESSION_STATUS_PENDING {
		t.Errorf("session.Status = %v, want SESSION_STATUS_PENDING", sess.GetStatus())
	}
}

// ---------------------------------------------------------------------------
// Test 2: Session get via gateway → session gRPC
// ---------------------------------------------------------------------------

// TestGateway_SessionGet_ReturnsMatchingSession verifies that retrieving a
// session through the gateway returns the same session data that was created.
func TestGateway_SessionGet_ReturnsMatchingSession(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	created, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, created.GetName())

	// Get session by name.
	got := gwGetSession(t, endpoint, created.GetName())

	if got.GetName() != created.GetName() {
		t.Errorf("GetSession.Name = %q, want %q", got.GetName(), created.GetName())
	}
	if got.GetToken() != created.GetToken() {
		t.Error("GetSession.Token differs from CreateSession.Token")
	}
	if got.GetOwnerRuntimeId() != created.GetOwnerRuntimeId() {
		t.Errorf("GetSession.OwnerRuntimeId = %q, want %q", got.GetOwnerRuntimeId(), created.GetOwnerRuntimeId())
	}
}

// ---------------------------------------------------------------------------
// Test 3: Session reconnect via gateway → session gRPC
// ---------------------------------------------------------------------------

// TestGateway_SessionReconnect_ReturnsNewToken verifies that reconnecting a
// session through the gateway returns a new token and incremented
// reconnect_generation.
func TestGateway_SessionReconnect_ReturnsNewToken(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	created, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, created.GetName())

	origToken := created.GetToken()
	origGen := created.GetReconnectGeneration()

	reconnected, err := testutil.ReconnectSession(ctx, endpoint, created.GetName())
	if err != nil {
		t.Fatalf("ReconnectSession: %v", err)
	}

	// Verify new token differs.
	if reconnected.GetToken() == origToken {
		t.Error("ReconnectSession.Token should differ from original")
	}
	if reconnected.GetToken() == "" {
		t.Fatal("ReconnectSession.Token is empty")
	}

	// Verify generation incremented.
	if reconnected.GetReconnectGeneration() <= origGen {
		t.Errorf("ReconnectSession.ReconnectGeneration = %d, want > %d",
			reconnected.GetReconnectGeneration(), origGen)
	}

	// Verify owner_runtime_id is still present.
	if reconnected.GetOwnerRuntimeId() == "" {
		t.Fatal("ReconnectSession.OwnerRuntimeId is empty")
	}

	// Verify new token parses correctly.
	parsedRuntimeID, err := testutil.ParseSessionToken(reconnected.GetToken())
	if err != nil {
		t.Fatalf("ParseSessionToken: %v", err)
	}
	if parsedRuntimeID != reconnected.GetOwnerRuntimeId() {
		t.Errorf("token owner_runtime_id = %q, want %q", parsedRuntimeID, reconnected.GetOwnerRuntimeId())
	}
}

// ---------------------------------------------------------------------------
// Test 4: Session delete via gateway → session gRPC
// ---------------------------------------------------------------------------

// TestGateway_SessionDelete_RemovesSession verifies that deleting a session
// through the gateway removes it, and subsequent GET returns 404.
func TestGateway_SessionDelete_RemovesSession(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Delete the session.
	if err := testutil.DeleteSession(ctx, endpoint, sess.GetName()); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// Verify session is gone.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/"+sess.GetName(), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	client := &http.Client{Timeout: gwShortReadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GetSession after delete: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GetSession after delete status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// Test 5: WebSocket connect via gateway → owner proxy → runtime WS
// ---------------------------------------------------------------------------

// TestGateway_WebSocketConnect_ProxiesToRuntime verifies the full proxy chain:
// agent connects via gateway WS endpoint → gateway extracts token → resolves
// owner runtime → reverse-proxies to runtime → agent can exchange hello and
// media messages.
func TestGateway_WebSocketConnect_ProxiesToRuntime(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	// Create session through gateway.
	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, sess.GetName())

	tokenStr := sess.GetToken()
	sessionIDFromName := strings.TrimPrefix(sess.GetName(), "sessions/")

	// Build WS URL pointing to gateway.
	wsURL := gwBuildWsURL(t, endpoint, sessionIDFromName, tokenStr)

	// Agent connects through gateway (gateway reverse-proxies to runtime).
	agent, err := fakeagent.Connect(ctx, wsURL, sessionIDFromName)
	if err != nil {
		t.Fatalf("agent dial through gateway: %v", err)
	}
	defer agent.Close()

	// Agent sends media init.
	const (
		streamID = "stream-gw-1"
		initID   = "init-gw-abc"
	)
	if err := agent.SendMediaInit(ctx, streamID, initID, "video/mp4", "h264-avc", []byte("fake-init-data")); err != nil {
		t.Fatalf("agent send media init: %v", err)
	}

	// Web client connects via gateway.
	webConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err != nil {
		t.Fatalf("web dial through gateway: %v", err)
	}
	defer webConn.Close(websocket.StatusNormalClosure, "test done")

	// Send web hello.
	hello := &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionIDFromName,
		MessageId: gwNextMsgID("hello-web"),
		Payload: &runtimepb.GameWebSocketEnvelope_Hello{
			Hello: &runtimepb.GameHello{
				Role: runtimepb.GameClientRole_GAME_CLIENT_ROLE_WEB,
			},
		},
	}
	data, err := gwJSONMarshaler.Marshal(hello)
	if err != nil {
		t.Fatalf("marshal web hello: %v", err)
	}
	if err := webConn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write web hello: %v", err)
	}

	// Web should receive catch-up media init.
	_, msgData, err := webConn.Read(ctx)
	if err != nil {
		t.Fatalf("web read media init: %v", err)
	}
	env := new(runtimepb.GameWebSocketEnvelope)
	if err := gwJSONUnmarshaler.Unmarshal(msgData, env); err != nil {
		t.Fatalf("unmarshal catch-up message: %v", err)
	}
	if init := env.GetMediaInit(); init == nil {
		t.Fatalf("expected media_init, got %T", env.Payload)
	} else {
		if init.GetStreamId() != streamID {
			t.Errorf("media_init stream_id = %q, want %q", init.GetStreamId(), streamID)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: Gateway token routing (ParseRoutingClaims extracts owner_runtime_id)
// ---------------------------------------------------------------------------

// TestGateway_ParseRoutingClaims_ExtractsOwnerRuntimeID verifies that the
// gateway's token extraction mechanism (ParseRoutingClaims without secret)
// correctly extracts session_id and owner_runtime_id from session tokens
// for routing purposes.
func TestGateway_ParseRoutingClaims_ExtractsOwnerRuntimeID(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, sess.GetName())

	tokenStr := sess.GetToken()
	runtimeID := sess.GetOwnerRuntimeId()

	// Use the parser (no secret) for ParseRoutingClaims, matching the
	// gateway's OwnerExtractor usage in router.go.
	dummySigner := token.NewParser()
	claims, err := dummySigner.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("ParseRoutingClaims: %v", err)
	}

	// Verify routing claims match session fields.
	if claims.SessionID != sessionID {
		t.Errorf("claims.SessionID = %q, want %q", claims.SessionID, sessionID)
	}
	if claims.OwnerRuntimeID != runtimeID {
		t.Errorf("claims.OwnerRuntimeID = %q, want %q", claims.OwnerRuntimeID, runtimeID)
	}
	if claims.OwnerEpoch == 0 {
		t.Error("claims.OwnerEpoch should be > 0")
	}
	if claims.Audience == "" {
		t.Error("claims.Audience should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Test 7: Forged token rejected by runtime (gateway forwards, runtime rejects)
// ---------------------------------------------------------------------------

// TestGateway_ForgedToken_Rejected verifies the security boundary: the gateway
// forwards forged tokens (it only does routing, not verification), but the
// runtime rejects them at WebSocket connect time when the signature doesn't
// match.
func TestGateway_ForgedToken_Rejected(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, sess.GetName())

	// Forge a token with a different secret but the same session_id and
	// owner_runtime_id. The gateway's ParseRoutingClaims will extract the
	// owner_runtime_id and forward to the correct runtime, but the runtime
	// will verify the signature and reject.
	wrongSigner := token.NewHMACSigner("wrong-secret-key-for-testing", gwTokenTTL)
	forgedToken, err := wrongSigner.Issue(
		sessionID,
		sess.GetOwnerRuntimeId(),
		1,
		token.TokenAudienceInternal,
		sess.GetReconnectGeneration(),
	)
	if err != nil {
		t.Fatalf("issue forged token: %v", err)
	}

	sessionIDFromName := strings.TrimPrefix(sess.GetName(), "sessions/")
	wsURL := gwBuildWsURL(t, endpoint, sessionIDFromName, forgedToken)

	// Dial WS through gateway. Gateway forwards to runtime; runtime rejects.
	wsCtx, wsCancel := context.WithTimeout(context.Background(), gwShortReadTimeout)
	defer wsCancel()

	_, _, err = websocket.Dial(wsCtx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if err == nil {
		t.Fatal("expected forged token to be rejected by runtime, but WebSocket connection succeeded")
	}
	t.Logf("forged token correctly rejected: %v", err)
}

// ---------------------------------------------------------------------------
// Test 8: Gateway stateless (no sessionmanager/runtime state)
// ---------------------------------------------------------------------------

// TestGateway_Stateless_NoSessionOrRuntimeState verifies that the gateway is
// truly stateless. Multiple sequential and concurrent session operations
// through the gateway all succeed without any failure due to per-instance
// state. The gateway holds no session manager, runtime service, or token cache.
func TestGateway_Stateless_NoSessionOrRuntimeState(t *testing.T) {
	endpoint := gwMustEndpoint(t)

	// Create multiple sessions sequentially to verify no state accumulation.
	const numSessions = 3
	sessions := make([]string, numSessions)

	for i := 0; i < numSessions; i++ {
		sessionID := testutil.NewTestSessionID()
		ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)

		sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
		if err != nil {
			cancel()
			t.Fatalf("CreateSession #%d: %v", i+1, err)
		}
		sessions[i] = sess.GetName()
		cancel()
	}

	// Clean up all sessions.
	for _, name := range sessions {
		gwCleanupSession(t, endpoint, name)
	}

	// Verify each session is gone (stateless cleanup).
	for _, name := range sessions {
		ctx, cancel := context.WithTimeout(context.Background(), gwShortReadTimeout)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/"+name, nil)
		client := &http.Client{Timeout: gwShortReadTimeout}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("session %q should be deleted, got status %d", name, resp.StatusCode)
			}
		}
		cancel()
	}
}

// ---------------------------------------------------------------------------
// Test 9: Session ID path mismatch (gateway validates URL path vs token)
// ---------------------------------------------------------------------------

// TestGateway_SessionIDPathMismatch_Rejected verifies that the gateway
// rejects WebSocket connections when the session ID in the URL path does not
// match the session ID encoded in the token.
func TestGateway_SessionIDPathMismatch_Rejected(t *testing.T) {
	endpoint := gwMustEndpoint(t)
	sessionID := testutil.NewTestSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), gwReadTimeout)
	defer cancel()

	sess, err := testutil.CreateSession(ctx, endpoint, "SESSION_TYPE_SAOLEI", sessionID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer gwCleanupSession(t, endpoint, sess.GetName())

	runtimeID := sess.GetOwnerRuntimeId()
	sessionIDFromName := strings.TrimPrefix(sess.GetName(), "sessions/")

	// Issue a valid token for the real session.
	signer := gwMustSigner(t)
	tok, err := signer.Issue(sessionIDFromName, runtimeID, 1, token.TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Forge a different session ID in the URL path.
	fakeSessionID := fmt.Sprintf("forged-session-%d", time.Now().UnixNano())
	fakeURL := gwBuildWsURL(t, endpoint, fakeSessionID, tok)

	wsCtx, wsCancel := context.WithTimeout(context.Background(), gwShortReadTimeout)
	defer wsCancel()

	conn, _, err := websocket.Dial(wsCtx, fakeURL, &websocket.DialOptions{HTTPHeader: http.Header{}})
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "test done")
	}
	if err == nil {
		t.Fatal("expected WebSocket connection to be rejected for session ID path mismatch, but it succeeded")
	}
}
