package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/gateway/domain/sessionmanager"
	"dominion/projects/game/gateway/service"
	"dominion/projects/game/pkg/token"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

type testVerifierWS struct {
	claims *token.Claims
	err    error
}

func (v *testVerifierWS) Verify(_ string) (*token.Claims, error) {
	return v.claims, v.err
}

func newTestGatewayServiceWS(verifier *testVerifierWS) *service.GatewayService {
	if verifier == nil {
		verifier = &testVerifierWS{
			claims: &token.Claims{
				SessionID: "test-session",
				GatewayID: "gw-test",
				ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
			},
		}
	}
	return service.NewGatewayService(
		sessionmanager.NewManager("gw-test"),
		service.NewControlExecutor(),
		"gw-test",
		verifier,
	)
}

func makeWSURL(server *httptest.Server, sessionID, tokenStr string) string {
	return fmt.Sprintf("ws://%s/v1/sessions/%s/game/connect?token=%s",
		server.Listener.Addr().String(), sessionID, tokenStr)
}

func wsWrite(ctx context.Context, conn *websocket.Conn, env *GameWebSocketEnvelope) error {
	data, err := protojson.Marshal(env)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func wsRead(ctx context.Context, conn *websocket.Conn) (*GameWebSocketEnvelope, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	env := new(GameWebSocketEnvelope)
	if err := protojson.Unmarshal(data, env); err != nil {
		return nil, err
	}
	return env, nil
}

func connectAndHello(ctx context.Context, url, sessionID string, role GameClientRole) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}

	hello := &GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: "hello-msg",
		Payload: &GameWebSocketEnvelope_Hello{
			Hello: &GameHello{Role: role},
		},
	}
	if err := wsWrite(ctx, conn, hello); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}
	return conn, nil
}

func Test_toDomainPayload_controlRequestMouseDragRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		env  *GameWebSocketEnvelope
	}{
		{
			name: "mouse drag keeps button coordinates and duration",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-drag",
						Kind:        GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG,
						Mouse: &GameMouseAction{
							Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
							FromX:      10,
							FromY:      20,
							ToX:        100,
							ToY:        200,
							DurationMs: 500,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domainPayload, ok := toDomainPayload(tt.env).(domain.ControlRequestPayload)
			if !ok {
				t.Fatal("toDomainPayload() is not control_request")
			}
			if domainPayload.Kind != domain.OperationKindMouseDrag {
				t.Fatalf("Kind = %q, want %q", domainPayload.Kind, domain.OperationKindMouseDrag)
			}
			if domainPayload.Button != "left" {
				t.Fatalf("Button = %q, want %q", domainPayload.Button, "left")
			}
			if domainPayload.FromX != 10 || domainPayload.FromY != 20 || domainPayload.ToX != 100 || domainPayload.ToY != 200 {
				t.Fatalf("domain drag coordinates = (%d,%d)->(%d,%d), want (10,20)->(100,200)",
					domainPayload.FromX, domainPayload.FromY, domainPayload.ToX, domainPayload.ToY)
			}

			protoPayload, ok := toProtoPayload(domainPayload).(*GameWebSocketEnvelope_ControlRequest)
			if !ok {
				t.Fatal("toProtoPayload() is not control_request")
			}
			mouse := protoPayload.ControlRequest.GetMouse()
			if mouse.GetButton() != GameMouseButton_GAME_MOUSE_BUTTON_LEFT {
				t.Fatalf("Button = %v, want %v", mouse.GetButton(), GameMouseButton_GAME_MOUSE_BUTTON_LEFT)
			}
			if mouse.GetFromX() != 10 || mouse.GetFromY() != 20 || mouse.GetToX() != 100 || mouse.GetToY() != 200 {
				t.Fatalf("proto drag coordinates = (%d,%d)->(%d,%d), want (10,20)->(100,200)",
					mouse.GetFromX(), mouse.GetFromY(), mouse.GetToX(), mouse.GetToY())
			}
			if mouse.GetDurationMs() != 500 {
				t.Fatalf("DurationMs = %d, want %d", mouse.GetDurationMs(), 500)
			}
		})
	}
}

func Test_pathParsing(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "valid session ID",
			path: "/v1/sessions/sess-123/game/connect",
			want: "sess-123",
		},
		{
			name:    "missing prefix",
			path:    "/sessions/sess-123/game/connect",
			wantErr: true,
		},
		{
			name:    "missing suffix",
			path:    "/v1/sessions/sess-123/connect",
			wantErr: true,
		},
		{
			name:    "empty session ID",
			path:    "/v1/sessions//game/connect",
			wantErr: true,
		},
		{
			name:    "session ID with slashes",
			path:    "/v1/sessions/a/b/game/connect",
			wantErr: true,
		},
		{
			name:    "root path",
			path:    "/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSessionID(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseSessionID() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSessionID() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseSessionID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWebSocket_InvalidToken(t *testing.T) {
	svc := newTestGatewayServiceWS(&testVerifierWS{err: token.ErrTokenInvalid})
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "bad-token")

	// when
	_, resp, err := websocket.Dial(ctx, url, nil)

	// then
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if resp == nil {
		t.Fatal("expected HTTP response")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocket_MissingToken(t *testing.T) {
	svc := newTestGatewayServiceWS(nil)
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	wsURL := fmt.Sprintf("ws://%s/v1/sessions/test-session/game/connect",
		server.Listener.Addr().String())

	// when
	_, resp, err := websocket.Dial(ctx, wsURL, nil)

	// then
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocket_ConnectAndHello(t *testing.T) {
	svc := newTestGatewayServiceWS(nil)
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	// when
	conn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("connect and hello: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// then: verify connection is alive with ping/pong
	ping := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "ping-verify",
		Payload: &GameWebSocketEnvelope_Ping{
			Ping: &GamePing{Nonce: "hello-verify"},
		},
	}
	if err := wsWrite(ctx, conn, ping); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	pong, err := wsRead(readCtx, conn)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.GetPong() == nil {
		t.Fatal("expected pong response")
	}
	if pong.GetPong().GetNonce() != "hello-verify" {
		t.Fatalf("Nonce = %q, want %q", pong.GetPong().GetNonce(), "hello-verify")
	}
}

func TestWebSocket_DuplicateAgent(t *testing.T) {
	svc := newTestGatewayServiceWS(nil)
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	// given: first agent connects and sends hello
	conn1, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT)
	if err != nil {
		t.Fatalf("first agent connect: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)

	// when: second agent connects and sends hello
	conn2, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("second agent dial: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")

	hello2 := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "hello-2",
		Payload: &GameWebSocketEnvelope_Hello{
			Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT},
		},
	}
	if err := wsWrite(ctx, conn2, hello2); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// then: second agent receives error
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := wsRead(readCtx, conn2)
	if err != nil {
		return
	}
	if resp.GetError() == nil {
		t.Fatal("expected error message for duplicate agent")
	}
}

func TestWebSocket_MessageRouting(t *testing.T) {
	svc := newTestGatewayServiceWS(nil)
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	// given: agent and web connected
	agentConn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT)
	if err != nil {
		t.Fatalf("agent connect: %v", err)
	}
	defer agentConn.Close(websocket.StatusNormalClosure, "")

	webConn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("web connect: %v", err)
	}
	defer webConn.Close(websocket.StatusNormalClosure, "")

	// when: agent sends media_init
	mediaEnv := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "media-1",
		Payload: &GameWebSocketEnvelope_MediaInit{
			MediaInit: &GameMediaInit{
				MimeType: "video/mp4",
				Segment:  []byte("fake-init-segment"),
			},
		},
	}
	if err := wsWrite(ctx, agentConn, mediaEnv); err != nil {
		t.Fatalf("write media_init: %v", err)
	}

	// then: web receives media_init
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	received, err := wsRead(readCtx, webConn)
	if err != nil {
		t.Fatalf("read from web: %v", err)
	}
	initPayload := received.GetMediaInit()
	if initPayload == nil {
		t.Fatal("expected media_init message on web connection")
	}
	if initPayload.GetMimeType() != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", initPayload.GetMimeType(), "video/mp4")
	}
	if string(initPayload.GetSegment()) != "fake-init-segment" {
		t.Fatalf("Segment = %q, want %q", string(initPayload.GetSegment()), "fake-init-segment")
	}
}

func TestWebSocket_PingPong(t *testing.T) {
	svc := newTestGatewayServiceWS(nil)
	handler := NewWebSocketHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	// given: web client connected
	conn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// when: web sends ping
	ping := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "ping-1",
		Payload: &GameWebSocketEnvelope_Ping{
			Ping: &GamePing{Nonce: "nonce-123"},
		},
	}
	if err := wsWrite(ctx, conn, ping); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// then: web receives pong
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	pong, err := wsRead(readCtx, conn)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.GetPong() == nil {
		t.Fatal("expected pong message")
	}
	if pong.GetPong().GetNonce() != "nonce-123" {
		t.Fatalf("Nonce = %q, want %q", pong.GetPong().GetNonce(), "nonce-123")
	}
}
