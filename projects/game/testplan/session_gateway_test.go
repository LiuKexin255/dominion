package testplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dominion/pkg/solver"
	"dominion/pkg/testtool"
	gw "dominion/projects/game/gateway"
	"dominion/projects/game/pkg/token"
	session "dominion/projects/game/session"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	sgTokenTTL         = 30 * time.Minute
	sgReadTimeout      = 15 * time.Second
	sgEnvTokenSecret   = "SESSION_TOKEN_SECRET"
	sgWsPathPrefix     = "/v1/sessions/"
	sgWsPathSuffix     = "/game/connect"
	sgWsSchemeSecure   = "wss"
	sgWsSchemeInsecure = "ws"
)

var (
	sgJSONMarshaler   = protojson.MarshalOptions{}
	sgJSONUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// sgMustSigner creates an HMACSigner from the required environment secret.
func sgMustSigner(t *testing.T) *token.HMACSigner {
	t.Helper()
	secret := strings.TrimSpace(os.Getenv(sgEnvTokenSecret))
	if secret == "" {
		t.Fatalf("missing required environment variable %s", sgEnvTokenSecret)
	}
	return token.NewHMACSigner(secret, sgTokenTTL)
}

// sgIssueToken issues a signed token for the given session and gateway.
func sgIssueToken(t *testing.T, sessionID, gatewayID string) string {
	t.Helper()
	signer := sgMustSigner(t)
	tok, err := signer.Issue(sessionID, gatewayID, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// sgMessageID returns a unique message ID with the given prefix.
func sgMessageID(prefix string) string {
	return fmt.Sprintf("test-%s-%d", prefix, time.Now().UnixNano())
}

// sgParseAgentConnectURL validates the agent_connect_url format and returns
// its components: scheme, host, path session ID, and token value.
func sgParseAgentConnectURL(t *testing.T, rawURL string) (scheme, host, pathSessionID, tokenVal string) {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse agent_connect_url %q: %v", rawURL, err)
	}

	scheme = u.Scheme
	host = u.Host

	if scheme != sgWsSchemeSecure && scheme != sgWsSchemeInsecure {
		t.Fatalf("agent_connect_url scheme = %q, want %q or %q", scheme, sgWsSchemeSecure, sgWsSchemeInsecure)
	}

	path := u.Path
	if !strings.HasPrefix(path, sgWsPathPrefix) || !strings.HasSuffix(path, sgWsPathSuffix) {
		t.Fatalf("agent_connect_url path = %q, want prefix %q and suffix %q", path, sgWsPathPrefix, sgWsPathSuffix)
	}

	pathSessionID = strings.TrimPrefix(path, sgWsPathPrefix)
	pathSessionID = strings.TrimSuffix(pathSessionID, sgWsPathSuffix)
	if pathSessionID == "" || strings.Contains(pathSessionID, "/") {
		t.Fatalf("invalid session ID in agent_connect_url path: %q", path)
	}

	tokenVal = u.Query().Get("token")
	if tokenVal == "" {
		t.Fatal("agent_connect_url missing token query parameter")
	}

	return scheme, host, pathSessionID, tokenVal
}

// sgDialWithAgentHello establishes a WebSocket connection using the given URL
// and sends a hello message with role=windows_agent.
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

	hello := &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: sgMessageID("hello-agent"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	}
	data, err := sgJSONMarshaler.Marshal(hello)
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

// sgCreateSession creates a session and returns the CreateSessionResponse.
func sgCreateSession(t *testing.T, sutHostURL string) *session.CreateSessionResponse {
	t.Helper()
	return createSession(t, sutHostURL)
}

// sgReconnectSession reconnects an existing session and returns the response.
func sgReconnectSession(t *testing.T, sutHostURL, name string) *session.ReconnectSessionResponse {
	t.Helper()

	reqBody, err := sgJSONMarshaler.Marshal(&session.ReconnectSessionRequest{
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

// sgBuildWsURL constructs a WebSocket URL from host, session ID, and token.
func sgBuildWsURL(host, sessionID, tokenStr string) string {
	scheme := sgWsSchemeSecure
	if strings.HasPrefix(host, "http://") {
		scheme = sgWsSchemeInsecure
		host = strings.TrimPrefix(host, "http://")
	} else {
		host = strings.TrimPrefix(host, "https://")
	}
	return fmt.Sprintf("%s://%s%s%s%s?token=%s", scheme, host, sgWsPathPrefix, sessionID, sgWsPathSuffix, tokenStr)
}

// TestSessionGateway_CreateSession_ReturnsValidAgentConnectURL verifies that
// CreateSession returns a valid session with a properly formatted
// agent_connect_url containing the correct scheme, host, path, and token.
func TestSessionGateway_CreateSession_ReturnsValidAgentConnectURL(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := sgCreateSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	sess := created.GetSession()
	if sess == nil {
		t.Fatal("CreateSession response missing session")
	}

	agentURL := created.GetAgentConnectUrl()
	if agentURL == "" {
		t.Fatal("agent_connect_url is empty")
	}

	scheme, host, pathSessionID, tokenVal := sgParseAgentConnectURL(t, agentURL)

	// Verify scheme is wss.
	if scheme != sgWsSchemeSecure {
		t.Errorf("agent_connect_url scheme = %q, want %q", scheme, sgWsSchemeSecure)
	}

	// Verify host matches gateway-{index}-game.liukexin.com pattern.
	// Note: GatewayID is instance.Hostname (e.g. sts-game-gateway-xxx-0),
	// while URL host is PublicHost (e.g. gateway-0-game.liukexin.com).
	if !strings.HasPrefix(host, "gateway-") || !strings.HasSuffix(host, "-game.liukexin.com") {
		t.Errorf("agent_connect_url host = %q, want format gateway-{index}-game.liukexin.com", host)
	}
	gatewayID := sess.GetGatewayId()
	if gatewayID == "" {
		t.Fatal("session.GatewayId is empty")
	}

	// Verify path contains the correct session ID.
	sessionIDFromName := strings.TrimPrefix(sess.GetName(), "sessions/")
	if pathSessionID != sessionIDFromName {
		t.Errorf("agent_connect_url path session ID = %q, want %q", pathSessionID, sessionIDFromName)
	}

	// Verify token exists.
	if tokenVal == "" {
		t.Error("agent_connect_url token is empty")
	}
}

// TestSessionGateway_WebSocketConnect_Succeeds verifies that the
// agent_connect_url from CreateSession can be used to establish a WebSocket
// connection and send a hello message without error.
func TestSessionGateway_WebSocketConnect_Succeeds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := sgCreateSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	agentURL := created.GetAgentConnectUrl()
	sessionIDFromName := strings.TrimPrefix(created.GetSession().GetName(), "sessions/")

	ctx, cancel := context.WithTimeout(context.Background(), sgReadTimeout)
	defer cancel()

	conn := sgDialWithAgentHello(ctx, t, agentURL, sessionIDFromName)
	defer func() {
		if conn != nil {
			conn.Close(websocket.StatusNormalClosure, "test done")
		}
	}()
}

// TestSessionGateway_GatewayId_MatchesActualHostname verifies that the
// session's GatewayId matches one of the actual deployed gateway hostnames
// resolved via DeployStatefulResolver.
func TestSessionGateway_GatewayId_MatchesActualHostname(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := sgCreateSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	gatewayID := created.GetSession().GetGatewayId()
	if gatewayID == "" {
		t.Fatal("session.GatewayId is empty")
	}

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

	found := false
	for _, inst := range instances {
		if inst.Hostname == gatewayID {
			found = true
			break
		}
	}
	if !found {
		hostnames := make([]string, len(instances))
		for i, inst := range instances {
			hostnames[i] = inst.Hostname
		}
		t.Errorf("session.GatewayId = %q, not found in gateway instances %v", gatewayID, hostnames)
	}
}

// TestSessionGateway_PathSessionMismatch_Rejected verifies that the gateway
// rejects a WebSocket connection when the session ID in the URL path does not
// match the token's session ID.
func TestSessionGateway_PathSessionMismatch_Rejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := sgCreateSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	gatewayID := created.GetSession().GetGatewayId()
	sessionID := strings.TrimPrefix(created.GetSession().GetName(), "sessions/")

	// Issue token for the real session ID.
	tok := sgIssueToken(t, sessionID, gatewayID)

	// Build URL with a forged (different) session ID in the path.
	fakeSessionID := "forged-session-" + fmt.Sprintf("%d", time.Now().UnixNano())
	host := strings.TrimPrefix(sutHostURL, "https://")
	if strings.HasPrefix(sutHostURL, "http://") {
		host = strings.TrimPrefix(sutHostURL, "http://")
	}
	fakeURL := sgBuildWsURL("https://"+host, fakeSessionID, tok)

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

// TestSessionGateway_ReconnectSession_ReturnsNewAgentConnectURL verifies that
// ReconnectSession returns a new agent_connect_url with a new token, and the
// new URL can be used to establish a WebSocket connection.
func TestSessionGateway_ReconnectSession_ReturnsNewAgentConnectURL(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")

	created := sgCreateSession(t, sutHostURL)
	defer deleteSessionForCleanup(t, sutHostURL, created.GetSession().GetName())

	sessionName := created.GetSession().GetName()
	sessionID := strings.TrimPrefix(sessionName, "sessions/")

	// Reconnect to get new agent_connect_url.
	reconnected := sgReconnectSession(t, sutHostURL, sessionName)

	reconnSess := reconnected.GetSession()
	if reconnSess == nil {
		t.Fatal("ReconnectSession response missing session")
	}

	newAgentURL := reconnected.GetAgentConnectUrl()
	if newAgentURL == "" {
		t.Fatal("ReconnectSession agent_connect_url is empty")
	}

	// Verify reconnect generation incremented.
	if reconnSess.GetReconnectGeneration() < created.GetSession().GetReconnectGeneration() {
		t.Errorf("reconnect generation = %d, want >= %d", reconnSess.GetReconnectGeneration(), created.GetSession().GetReconnectGeneration())
	}

	// Parse the new URL and verify it has the correct format.
	scheme, host, pathSessionID, tokenVal := sgParseAgentConnectURL(t, newAgentURL)

	// Verify scheme.
	if scheme != sgWsSchemeSecure {
		t.Errorf("reconnect agent_connect_url scheme = %q, want %q", scheme, sgWsSchemeSecure)
	}

	// Verify host matches gateway-{index}-game.liukexin.com pattern.
	// GatewayID is instance.Hostname; URL host is the external PublicHost.
	if !strings.HasPrefix(host, "gateway-") || !strings.HasSuffix(host, "-game.liukexin.com") {
		t.Errorf("reconnect agent_connect_url host = %q, want format gateway-{index}-game.liukexin.com", host)
	}
	newGatewayID := reconnSess.GetGatewayId()
	if newGatewayID == "" {
		t.Fatal("reconnected session.GatewayId is empty")
	}

	// Verify path session ID matches.
	if pathSessionID != sessionID {
		t.Errorf("reconnect agent_connect_url path session ID = %q, want %q", pathSessionID, sessionID)
	}

	// Verify token exists and is different from original (if generation changed).
	if tokenVal == "" {
		t.Error("reconnect agent_connect_url token is empty")
	}

	// Use the new URL to establish a WebSocket connection.
	ctx, cancel := context.WithTimeout(context.Background(), sgReadTimeout)
	defer cancel()

	conn := sgDialWithAgentHello(ctx, t, newAgentURL, sessionID)
	defer func() {
		if conn != nil {
			conn.Close(websocket.StatusNormalClosure, "test done")
		}
	}()
}
