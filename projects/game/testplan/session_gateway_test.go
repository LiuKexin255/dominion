package testplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	"dominion/projects/game/pkg/token"
	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/session"

	"github.com/coder/websocket"
)

const (
	sgTokenTTL       = 30 * time.Minute
	sgReadTimeout    = 15 * time.Second
	sgEnvTokenSecret = "SESSION_TOKEN_SECRET"
	sgWsPathPrefix   = "/v1/sessions/"
	sgWsPathSuffix   = "/game/connect"
)

func sgMustSigner(t *testing.T) *token.HMACSigner {
	t.Helper()
	secret := strings.TrimSpace(os.Getenv(sgEnvTokenSecret))
	if secret == "" {
		t.Fatalf("missing required environment variable %s", sgEnvTokenSecret)
	}
	return token.NewHMACSigner(secret, sgTokenTTL)
}

func sgIssueToken(t *testing.T, sessionID, runtimeID string) string {
	t.Helper()
	signer := sgMustSigner(t)
	tok, err := signer.Issue(sessionID, runtimeID, 1, token.TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

func sgDialWithAgentHello(ctx context.Context, t *testing.T, wsURL, sessionID string) *websocket.Conn {
	t.Helper()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			headerEnv: {testtool.MustEnv()},
		},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	hello := &runtimepb.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: fmt.Sprintf("test-hello-agent-%d", time.Now().UnixNano()),
		Payload: &runtimepb.GameWebSocketEnvelope_Hello{
			Hello: &runtimepb.GameHello{
				Role: runtimepb.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	data, err := jsonMarshaler.Marshal(hello)
	if err != nil {
		conn.Close(websocket.StatusNormalClosure, "marshal failed")
		t.Fatalf("marshal hello: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		conn.Close(websocket.StatusNormalClosure, "write failed")
		t.Fatalf("write hello: %v", err)
	}

	return conn
}

func sgReconnectSession(t *testing.T, sutHostURL, name string) *session.ReconnectSessionResponse {
	t.Helper()

	reqBody, err := jsonMarshaler.Marshal(&session.ReconnectSessionRequest{
		Name: name,
	})
	if err != nil {
		t.Fatalf("protojson.Marshal(ReconnectSessionRequest) unexpected error: %v", err)
	}

	resp := doRequest(t, http.MethodPost, sutHostURL+"/v1/"+name+":reconnect", bytes.NewReader(reqBody))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("ReconnectSession status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(body))
	}

	return decodeReconnectSessionResponse(t, resp)
}

func sgBuildWsURL(sutHostURL, sessionID, tokenStr string) string {
	scheme := "wss"
	host := strings.TrimPrefix(sutHostURL, "https://")
	if strings.HasPrefix(sutHostURL, "http://") {
		scheme = "ws"
		host = strings.TrimPrefix(sutHostURL, "http://")
	}
	return fmt.Sprintf("%s://%s%s%s%s?token=%s", scheme, host, sgWsPathPrefix, sessionID, sgWsPathSuffix, tokenStr)
}

func TestSessionGateway_CreateSession_ReturnsValidSession(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	sess := created.GetSession()
	if sess == nil {
		t.Fatal("CreateSession response missing session")
	}

	if sess.GetToken() == "" {
		t.Fatal("session.Token is empty")
	}

	runtimeID := sess.GetOwnerRuntimeId()
	if runtimeID == "" {
		t.Fatal("session.OwnerRuntimeId is empty")
	}

	parsed, err := token.NewHMACSigner("", 0).ParseRoutingClaims(sess.GetToken())
	if err != nil {
		t.Fatalf("ParseRoutingClaims: %v", err)
	}
	if parsed.OwnerRuntimeID == "" {
		t.Fatal("token missing owner_runtime_id")
	}
}

func TestSessionGateway_WebSocketConnect_Succeeds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	sess := created.GetSession()
	sessionID := strings.TrimPrefix(sess.GetName(), "sessions/")
	wsURL := sgBuildWsURL(sutHostURL, sessionID, sess.GetToken())

	ctx, cancel := context.WithTimeout(context.Background(), sgReadTimeout)
	defer cancel()

	conn := sgDialWithAgentHello(ctx, t, wsURL, sessionID)
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "test done")
	}
}

func TestSessionGateway_OwnerRuntimeId_IsSet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	runtimeID := created.GetSession().GetOwnerRuntimeId()
	if runtimeID == "" {
		t.Fatal("session.OwnerRuntimeId is empty")
	}

	tokenStr := created.GetSession().GetToken()
	parsed, err := token.NewHMACSigner("", 0).ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("ParseRoutingClaims: %v", err)
	}
	if parsed.OwnerRuntimeID != runtimeID {
		t.Errorf("token owner_runtime_id = %q, want %q", parsed.OwnerRuntimeID, runtimeID)
	}
}

func TestSessionGateway_PathSessionMismatch_Rejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	runtimeID := created.GetSession().GetOwnerRuntimeId()
	sessionID := strings.TrimPrefix(created.GetSession().GetName(), "sessions/")

	tok := sgIssueToken(t, sessionID, runtimeID)
	fakeSessionID := fmt.Sprintf("forged-session-%d", time.Now().UnixNano())
	fakeURL := sgBuildWsURL(sutHostURL, fakeSessionID, tok)

	ctx, cancel := context.WithTimeout(context.Background(), sgReadTimeout)
	defer cancel()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			headerEnv: {testtool.MustEnv()},
		},
	}

	_, _, err := websocket.Dial(ctx, fakeURL, opts)
	if err == nil {
		t.Fatal("expected WebSocket connection to be rejected for session ID mismatch, but it succeeded")
	}
}

func TestSessionGateway_ReconnectSession_ReturnsNewToken(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := createSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	sess := created.GetSession()
	sessionName := sess.GetName()
	origToken := sess.GetToken()
	origGen := sess.GetReconnectGeneration()

	reconnected := sgReconnectSession(t, sutHostURL, sessionName)
	reconnSess := reconnected.GetSession()
	if reconnSess == nil {
		t.Fatal("ReconnectSession response missing session")
	}

	if reconnSess.GetToken() == "" {
		t.Fatal("ReconnectSession.Token is empty")
	}
	if reconnSess.GetToken() == origToken {
		t.Error("ReconnectSession.Token should differ from original")
	}
	if reconnSess.GetReconnectGeneration() <= origGen {
		t.Errorf("ReconnectSession.ReconnectGeneration = %d, want > %d",
			reconnSess.GetReconnectGeneration(), origGen)
	}

	sessionID := strings.TrimPrefix(sessionName, "sessions/")
	wsURL := sgBuildWsURL(sutHostURL, sessionID, reconnSess.GetToken())

	ctx, cancel := context.WithTimeout(context.Background(), sgReadTimeout)
	defer cancel()

	conn := sgDialWithAgentHello(ctx, t, wsURL, sessionID)
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "test done")
	}
}
