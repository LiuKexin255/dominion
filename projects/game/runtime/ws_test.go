package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dominion/projects/game/pkg/token"
	"dominion/projects/game/runtime/config"
	"dominion/projects/game/runtime/domain"
	"dominion/projects/game/runtime/domain/sessionmanager"
	"dominion/projects/game/runtime/service"

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

func (v *testVerifierWS) VerifyWithGrace(_ string, _ time.Duration) (*token.Claims, error) {
	return v.claims, v.err
}

func newTestRuntimeServiceWS(verifier *testVerifierWS) *service.RuntimeService {
	if verifier == nil {
		verifier = &testVerifierWS{
			claims: &token.Claims{
				SessionID:      "test-session",
				OwnerRuntimeID: "rt-test",
				OwnerEpoch:     1,
				ExpiresAt:      time.Now().Add(5 * time.Minute).Unix(),
			},
		}
	}
	signer := token.NewHMACSigner("test-secret", 1*time.Hour)
	cfg := &config.RuntimeConfig{
		RuntimeID: "rt-test",
	}
	svc := service.NewRuntimeService(
		sessionmanager.NewManager("rt-test"),
		service.NewControlExecutor(),
		cfg,
		signer,
		verifier,
	)
	return svc
}

func newTestWSHandler(svc *service.RuntimeService) *WebSocketHandler {
	return NewWebSocketHandler(svc)
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

func Test_toDomainPayload_controlRequest_allActions(t *testing.T) {
	tests := []struct {
		name      string
		env       *GameWebSocketEnvelope
		wantKind  domain.OperationKind
		wantFlash bool
	}{
		{
			name: "mouse click action",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-click",
						Action: &GameControlRequest_MouseClick{
							MouseClick: &GameMouseClick{
								Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
								X:      100,
								Y:      200,
							},
						},
					},
				},
			},
			wantKind:  domain.OperationKindMouseClick,
			wantFlash: false,
		},
		{
			name: "mouse double click action",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-2",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-dblclick",
						Action: &GameControlRequest_MouseDoubleClick{
							MouseDoubleClick: &GameMouseDoubleClick{
								Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
								X:      50,
								Y:      75,
							},
						},
					},
				},
			},
			wantKind: domain.OperationKindMouseDoubleClick,
		},
		{
			name: "mouse drag action",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-3",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-drag",
						Action: &GameControlRequest_MouseDrag{
							MouseDrag: &GameMouseDrag{
								Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
								FromX:  10,
								FromY:  20,
								ToX:    100,
								ToY:    200,
							},
						},
					},
				},
			},
			wantKind: domain.OperationKindMouseDrag,
		},
		{
			name: "mouse hover action",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-4",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-hover",
						Action: &GameControlRequest_MouseHover{
							MouseHover: &GameMouseHover{
								X: 300,
								Y: 400,
							},
						},
					},
				},
			},
			wantKind: domain.OperationKindMouseHover,
		},
		{
			name: "mouse hold action",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-5",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId: "op-hold",
						Action: &GameControlRequest_MouseHold{
							MouseHold: &GameMouseHold{
								Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
								X:          500,
								Y:          600,
								DurationMs: 3000,
							},
						},
					},
				},
			},
			wantKind: domain.OperationKindMouseHold,
		},
		{
			name: "control request with flash_snapshot",
			env: &GameWebSocketEnvelope{
				SessionId: "test-session",
				MessageId: "control-flash",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{
						OperationId:   "op-flash",
						FlashSnapshot: true,
						Action: &GameControlRequest_MouseClick{
							MouseClick: &GameMouseClick{
								Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
								X:      0,
								Y:      0,
							},
						},
					},
				},
			},
			wantKind:  domain.OperationKindMouseClick,
			wantFlash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domainPayload, ok := toDomainPayload(tt.env).(domain.ControlRequestPayload)
			if !ok {
				t.Fatal("toDomainPayload() is not ControlRequestPayload")
			}
			if domainPayload.ActionKind != tt.wantKind {
				t.Fatalf("ActionKind = %q, want %q", domainPayload.ActionKind, tt.wantKind)
			}
			if domainPayload.FlashSnapshot != tt.wantFlash {
				t.Fatalf("FlashSnapshot = %v, want %v", domainPayload.FlashSnapshot, tt.wantFlash)
			}
		})
	}
}

func Test_toDomainPayload_controlRequest_allButtons(t *testing.T) {
	buttons := []struct {
		name   string
		button GameMouseButton
	}{
		{name: "left", button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT},
		{name: "right", button: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT},
		{name: "middle", button: GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE},
	}

	actions := []struct {
		name    string
		build   func(b GameMouseButton) isGameControlRequest_Action
		wantErr bool
	}{
		{
			name: "click",
			build: func(b GameMouseButton) isGameControlRequest_Action {
				return &GameControlRequest_MouseClick{MouseClick: &GameMouseClick{Button: b, X: 1, Y: 1}}
			},
		},
		{
			name: "double_click",
			build: func(b GameMouseButton) isGameControlRequest_Action {
				return &GameControlRequest_MouseDoubleClick{MouseDoubleClick: &GameMouseDoubleClick{Button: b, X: 1, Y: 1}}
			},
		},
		{
			name: "drag",
			build: func(b GameMouseButton) isGameControlRequest_Action {
				return &GameControlRequest_MouseDrag{MouseDrag: &GameMouseDrag{Button: b, FromX: 0, FromY: 0, ToX: 1, ToY: 1}}
			},
		},
		{
			name: "hold",
			build: func(b GameMouseButton) isGameControlRequest_Action {
				return &GameControlRequest_MouseHold{MouseHold: &GameMouseHold{Button: b, X: 1, Y: 1, DurationMs: 1000}}
			},
		},
	}

	for _, action := range actions {
		for _, btn := range buttons {
			t.Run(action.name+"_"+btn.name, func(t *testing.T) {
				env := &GameWebSocketEnvelope{
					SessionId: "test-session",
					MessageId: "btn-test",
					Payload: &GameWebSocketEnvelope_ControlRequest{
						ControlRequest: &GameControlRequest{
							OperationId: "op-btn",
							Action:      action.build(btn.button),
						},
					},
				}

				domainPayload, ok := toDomainPayload(env).(domain.ControlRequestPayload)
				if !ok {
					t.Fatal("toDomainPayload() is not ControlRequestPayload")
				}
				if domainPayload.OperationID != "op-btn" {
					t.Fatalf("OperationID = %q, want %q", domainPayload.OperationID, "op-btn")
				}
			})
		}
	}
}

func Test_toDomainPayload_controlRequest_noAction(t *testing.T) {
	env := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "control-no-action",
		Payload: &GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &GameControlRequest{
				OperationId: "op-no-action",
			},
		},
	}

	payload := toDomainPayload(env)

	errPayload, ok := payload.(domain.ErrorPayload)
	if !ok {
		t.Fatalf("expected ErrorPayload for missing action, got %T", payload)
	}
	if errPayload.Code != "protocol_error" {
		t.Fatalf("Code = %q, want %q", errPayload.Code, "protocol_error")
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
	verifier := &testVerifierWS{err: token.ErrTokenInvalid}
	svc := newTestRuntimeServiceWS(verifier)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "bad-token")

	_, resp, err := websocket.Dial(ctx, url, nil)

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
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	wsURL := fmt.Sprintf("ws://%s/v1/sessions/test-session/game/connect",
		server.Listener.Addr().String())

	_, resp, err := websocket.Dial(ctx, wsURL, nil)

	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocket_ConnectAndHello(t *testing.T) {
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	conn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("connect and hello: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

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
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	conn1, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT)
	if err != nil {
		t.Fatalf("first agent connect: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)

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
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

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

	segment := []byte("fake-init-segment")
	initHash := sha256.Sum256(segment)
	initID := hex.EncodeToString(initHash[:])
	mediaEnv := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "media-1",
		Payload: &GameWebSocketEnvelope_MediaInit{
			MediaInit: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   initID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  segment,
			},
		},
	}
	if err := wsWrite(ctx, agentConn, mediaEnv); err != nil {
		t.Fatalf("write media_init: %v", err)
	}

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
	if initPayload.GetMimeType() != "video/mp4; codecs=\"avc1.64001f\"" {
		t.Fatalf("MimeType = %q, want %q", initPayload.GetMimeType(), "video/mp4; codecs=\"avc1.64001f\"")
	}
	if string(initPayload.GetSegment()) != "fake-init-segment" {
		t.Fatalf("Segment = %q, want %q", string(initPayload.GetSegment()), "fake-init-segment")
	}
}

func TestWebSocket_PingPong(t *testing.T) {
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	url := makeWSURL(server, "test-session", "any-token")

	conn, err := connectAndHello(ctx, url, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

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

func Test_toDomainPayload_mediaInit(t *testing.T) {
	env := &GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "media-init-1",
		Payload: &GameWebSocketEnvelope_MediaInit{
			MediaInit: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "init-1",
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  []byte("init-segment-bytes"),
			},
		},
	}

	payload := toDomainPayload(env)

	initPayload, ok := payload.(domain.MediaInitPayload)
	if !ok {
		t.Fatalf("toDomainPayload() returned %T, want MediaInitPayload", payload)
	}
	if initPayload.StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", initPayload.StreamID, "stream-1")
	}
	if initPayload.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", initPayload.InitID, "init-1")
	}
	if initPayload.Codec != "h264-avc" {
		t.Fatalf("Codec = %q, want %q", initPayload.Codec, "h264-avc")
	}
	if initPayload.MimeType != "video/mp4; codecs=\"avc1.64001f\"" {
		t.Fatalf("MimeType = %q, want %q", initPayload.MimeType, "video/mp4; codecs=\"avc1.64001f\"")
	}
	if string(initPayload.Segment) != "init-segment-bytes" {
		t.Fatalf("Segment = %q, want %q", string(initPayload.Segment), "init-segment-bytes")
	}
}

func Test_toDomainPayload_mediaSegment(t *testing.T) {
	ra := true
	env := &GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "media-seg-1",
		Payload: &GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &GameMediaSegment{
				StreamId:      "stream-1",
				InitId:        "init-1",
				Sequence:      42,
				Segment:       []byte("fMP4-chunk"),
				RandomAccess:  &ra,
				DurationMs:    33,
				Discontinuity: true,
			},
		},
	}

	payload := toDomainPayload(env)

	segPayload, ok := payload.(domain.MediaSegmentPayload)
	if !ok {
		t.Fatalf("toDomainPayload() returned %T, want MediaSegmentPayload", payload)
	}
	if segPayload.StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", segPayload.StreamID, "stream-1")
	}
	if segPayload.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", segPayload.InitID, "init-1")
	}
	if segPayload.Sequence != 42 {
		t.Fatalf("Sequence = %d, want %d", segPayload.Sequence, 42)
	}
	if !segPayload.RandomAccess {
		t.Fatal("RandomAccess = false, want true")
	}
	if segPayload.DurationMS != 33 {
		t.Fatalf("DurationMS = %d, want %d", segPayload.DurationMS, 33)
	}
	if !segPayload.Discontinuity {
		t.Fatal("Discontinuity = false, want true")
	}
}

func Test_toProtoPayload_mediaInitRoundTrip(t *testing.T) {
	original := domain.MediaInitPayload{
		StreamID: "stream-rt",
		InitID:   "init-rt",
		Codec:    "h264-avc",
		MimeType: "video/mp4; codecs=\"avc1.64001f\"",
		Segment:  []byte("round-trip-init"),
	}

	protoPayload := toProtoPayload(original)
	env := &GameWebSocketEnvelope{SessionId: "s1", MessageId: "m1", Payload: protoPayload}
	roundTripped := toDomainPayload(env)

	result, ok := roundTripped.(domain.MediaInitPayload)
	if !ok {
		t.Fatalf("round-trip returned %T, want MediaInitPayload", roundTripped)
	}
	if result.StreamID != original.StreamID {
		t.Fatalf("StreamID = %q, want %q", result.StreamID, original.StreamID)
	}
	if result.InitID != original.InitID {
		t.Fatalf("InitID = %q, want %q", result.InitID, original.InitID)
	}
	if result.Codec != original.Codec {
		t.Fatalf("Codec = %q, want %q", result.Codec, original.Codec)
	}
	if result.MimeType != original.MimeType {
		t.Fatalf("MimeType = %q, want %q", result.MimeType, original.MimeType)
	}
	if string(result.Segment) != string(original.Segment) {
		t.Fatalf("Segment = %q, want %q", string(result.Segment), string(original.Segment))
	}
}

func Test_toProtoPayload_mediaSegmentRoundTrip(t *testing.T) {
	original := domain.MediaSegmentPayload{
		StreamID:      "stream-rt",
		InitID:        "init-rt",
		Sequence:      99,
		Segment:       []byte("round-trip-seg"),
		RandomAccess:  true,
		DurationMS:    100,
		Discontinuity: false,
	}

	protoPayload := toProtoPayload(original)
	env := &GameWebSocketEnvelope{SessionId: "s1", MessageId: "m1", Payload: protoPayload}
	roundTripped := toDomainPayload(env)

	result, ok := roundTripped.(domain.MediaSegmentPayload)
	if !ok {
		t.Fatalf("round-trip returned %T, want MediaSegmentPayload", roundTripped)
	}
	if result.StreamID != original.StreamID {
		t.Fatalf("StreamID = %q, want %q", result.StreamID, original.StreamID)
	}
	if result.InitID != original.InitID {
		t.Fatalf("InitID = %q, want %q", result.InitID, original.InitID)
	}
	if result.Sequence != original.Sequence {
		t.Fatalf("Sequence = %d, want %d", result.Sequence, original.Sequence)
	}
	if result.RandomAccess != original.RandomAccess {
		t.Fatalf("RandomAccess = %v, want %v", result.RandomAccess, original.RandomAccess)
	}
	if result.DurationMS != original.DurationMS {
		t.Fatalf("DurationMS = %d, want %d", result.DurationMS, original.DurationMS)
	}
	if result.Discontinuity != original.Discontinuity {
		t.Fatalf("Discontinuity = %v, want %v", result.Discontinuity, original.Discontinuity)
	}
}

func TestWebSocket_OwnerSelf_ProceedsNormally(t *testing.T) {
	svc := newTestRuntimeServiceWS(nil)
	handler := newTestWSHandler(svc)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx := context.Background()
	wsURL := makeWSURL(server, "test-session", "any-token")

	conn, err := connectAndHello(ctx, wsURL, "test-session", GameClientRole_GAME_CLIENT_ROLE_WEB)
	if err != nil {
		t.Fatalf("connect and hello: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ping := &GameWebSocketEnvelope{
		SessionId: "test-session",
		MessageId: "ping-verify",
		Payload: &GameWebSocketEnvelope_Ping{
			Ping: &GamePing{Nonce: "owner-self-verify"},
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
	if pong.GetPong().GetNonce() != "owner-self-verify" {
		t.Fatalf("Nonce = %q, want %q", pong.GetPong().GetNonce(), "owner-self-verify")
	}
}
func TestActionKindFromProto(t *testing.T) {
	tests := []struct {
		name    string
		req     *GameControlRequest
		want    domain.OperationKind
		wantErr bool
	}{
		{
			name: "mouse_click",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseClick{MouseClick: &GameMouseClick{}},
			},
			want: domain.OperationKindMouseClick,
		},
		{
			name: "mouse_double_click",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseDoubleClick{MouseDoubleClick: &GameMouseDoubleClick{}},
			},
			want: domain.OperationKindMouseDoubleClick,
		},
		{
			name: "mouse_drag",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseDrag{MouseDrag: &GameMouseDrag{}},
			},
			want: domain.OperationKindMouseDrag,
		},
		{
			name: "mouse_hover",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseHover{MouseHover: &GameMouseHover{}},
			},
			want: domain.OperationKindMouseHover,
		},
		{
			name: "mouse_hold",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseHold{MouseHold: &GameMouseHold{}},
			},
			want: domain.OperationKindMouseHold,
		},
		{
			name:    "no_action",
			req:     &GameControlRequest{},
			wantErr: true,
		},
		{
			name:    "nil_request",
			req:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ActionKindFromProto(tt.req)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ActionKindFromProto() expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ActionKindFromProto() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ActionKindFromProto() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProtoOperationKind(t *testing.T) {
	tests := []struct {
		name string
		kind domain.OperationKind
		want GameControlOperationKind
	}{
		{
			name: "mouse_click",
			kind: domain.OperationKindMouseClick,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
		},
		{
			name: "mouse_double_click",
			kind: domain.OperationKindMouseDoubleClick,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK,
		},
		{
			name: "mouse_drag",
			kind: domain.OperationKindMouseDrag,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG,
		},
		{
			name: "mouse_hover",
			kind: domain.OperationKindMouseHover,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER,
		},
		{
			name: "mouse_hold",
			kind: domain.OperationKindMouseHold,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD,
		},
		{
			name: "unknown_kind",
			kind: domain.OperationKind("unknown"),
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProtoOperationKind(tt.kind)
			if got != tt.want {
				t.Fatalf("ProtoOperationKind(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestDomainOperationKind(t *testing.T) {
	tests := []struct {
		name    string
		kind    GameControlOperationKind
		want    domain.OperationKind
		wantErr bool
	}{
		{
			name: "mouse_click",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
			want: domain.OperationKindMouseClick,
		},
		{
			name: "mouse_double_click",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK,
			want: domain.OperationKindMouseDoubleClick,
		},
		{
			name: "mouse_drag",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG,
			want: domain.OperationKindMouseDrag,
		},
		{
			name: "mouse_hover",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER,
			want: domain.OperationKindMouseHover,
		},
		{
			name: "mouse_hold",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD,
			want: domain.OperationKindMouseHold,
		},
		{
			name:    "unspecified",
			kind:    GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED,
			wantErr: true,
		},
		{
			name:    "unknown_enum_value",
			kind:    GameControlOperationKind(999),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DomainOperationKind(tt.kind)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("DomainOperationKind(%v) expected error, got %q", tt.kind, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DomainOperationKind(%v) unexpected error: %v", tt.kind, err)
			}
			if got != tt.want {
				t.Fatalf("DomainOperationKind(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestMouseButtonConvert(t *testing.T) {
	t.Run("ProtoMouseButton", func(t *testing.T) {
		tests := []struct {
			name   string
			button string
			want   GameMouseButton
		}{
			{name: "left", button: "left", want: GameMouseButton_GAME_MOUSE_BUTTON_LEFT},
			{name: "right", button: "right", want: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT},
			{name: "middle", button: "middle", want: GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE},
			{name: "left_case_insensitive", button: "LEFT", want: GameMouseButton_GAME_MOUSE_BUTTON_LEFT},
			{name: "unknown_string", button: "unknown", want: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED},
			{name: "empty_string", button: "", want: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := ProtoMouseButton(tt.button)
				if got != tt.want {
					t.Fatalf("ProtoMouseButton(%q) = %v, want %v", tt.button, got, tt.want)
				}
			})
		}
	})

	t.Run("DomainMouseButton", func(t *testing.T) {
		tests := []struct {
			name    string
			button  GameMouseButton
			want    string
			wantErr bool
		}{
			{
				name:   "left",
				button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				want:   "left",
			},
			{
				name:   "right",
				button: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
				want:   "right",
			},
			{
				name:   "middle",
				button: GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE,
				want:   "middle",
			},
			{
				name:    "unspecified",
				button:  GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				wantErr: true,
			},
			{
				name:    "unknown_enum_value",
				button:  GameMouseButton(999),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := DomainMouseButton(tt.button)

				if tt.wantErr {
					if err == nil {
						t.Fatalf("DomainMouseButton(%v) expected error, got %q", tt.button, got)
					}
					return
				}
				if err != nil {
					t.Fatalf("DomainMouseButton(%v) unexpected error: %v", tt.button, err)
				}
				if got != tt.want {
					t.Fatalf("DomainMouseButton(%v) = %q, want %q", tt.button, got, tt.want)
				}
			})
		}
	})
}

func TestMouseButtonRoundTrip(t *testing.T) {
	buttons := []string{"left", "right", "middle"}

	for _, btn := range buttons {
		t.Run(btn, func(t *testing.T) {
			proto := ProtoMouseButton(btn)
			got, err := DomainMouseButton(proto)
			if err != nil {
				t.Fatalf("round-trip DomainMouseButton(%v) unexpected error: %v", proto, err)
			}
			if got != btn {
				t.Fatalf("round-trip %q: ProtoMouseButton → DomainMouseButton = %q, want %q", btn, got, btn)
			}
		})
	}
}

// Ensure error messages contain the expected detail for debugging.
func TestMouseButtonErrorMessages(t *testing.T) {
	t.Run("DomainMouseButton_unspecified_error", func(t *testing.T) {
		_, err := DomainMouseButton(GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED)
		if err == nil {
			t.Fatal("expected error for UNSPECIFIED")
		}
		if _, ok := err.(fmt.Stringer); ok {
			t.Logf("error: %v", err)
		}
	})

	t.Run("DomainOperationKind_unspecified_error", func(t *testing.T) {
		_, err := DomainOperationKind(GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED)
		if err == nil {
			t.Fatal("expected error for UNSPECIFIED")
		}
	})
}

func TestValidateWebSocketEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "empty session_id",
			env: &GameWebSocketEnvelope{
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "empty message_id",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "missing payload oneof",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
			},
			wantErr: true,
		},
		{
			name: "valid envelope",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebSocketEnvelope(tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateWebSocketEnvelope() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateWebSocketEnvelope() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateHello(t *testing.T) {
	tests := []struct {
		name    string
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "missing hello payload",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "valid hello with unspecified role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED},
				},
			},
			wantErr: true,
		},
		{
			name: "valid hello with agent role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT},
				},
			},
		},
		{
			name: "valid hello with web role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_WEB},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHello(tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateHello() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateHello() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRolePayload(t *testing.T) {
	tests := []struct {
		name    string
		role    GameClientRole
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "agent sending media_init",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaInit{
					MediaInit: &GameMediaInit{MimeType: "video/mp4", Segment: []byte("init")},
				},
			},
		},
		{
			name: "agent sending control_ack",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlAck{
					ControlAck: &GameControlAck{OperationId: "op-1"},
				},
			},
		},
		{
			name: "agent sending control_result",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlResult{
					ControlResult: &GameControlResult{OperationId: "op-1"},
				},
			},
		},
		{
			name: "agent sending pong",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Pong{Pong: &GamePong{Nonce: "n1"}},
			},
		},
		{
			name: "agent sending error",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Error{Error: &GameError{Code: "err", Message: "test"}},
			},
		},
		{
			name: "agent sending control_request",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{OperationId: "op-1"},
				},
			},
			wantErr: true,
		},
		{
			name: "agent sending ping",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
			wantErr: true,
		},
		{
			name: "web sending control_request",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{OperationId: "op-1"},
				},
			},
		},
		{
			name: "web sending ping",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
		},
		{
			name: "web sending media_segment",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaSegment{
					MediaSegment: &GameMediaSegment{StreamId: "stream-1", InitId: "init-1", Sequence: 1, Segment: []byte("seg")},
				},
			},
			wantErr: true,
		},
		{
			name: "web sending media_init",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaInit{
					MediaInit: &GameMediaInit{MimeType: "video/mp4", Segment: []byte("init")},
				},
			},
			wantErr: true,
		},
		{
			name: "web sending control_ack",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlAck{
					ControlAck: &GameControlAck{OperationId: "op-1"},
				},
			},
			wantErr: true,
		},
		{
			name: "unspecified role",
			role: GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRolePayload(tt.role, tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateRolePayload() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateRolePayload() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateControlRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *GameControlRequest
		wantErr bool
	}{
		{
			name: "empty operation_id",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "action not set",
			req: &GameControlRequest{
				OperationId: "op-1",
			},
			wantErr: true,
		},
		{
			name: "click with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
				},
			},
		},
		{
			name: "click with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:      100,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "click with negative x",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      -1,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "double_click with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDoubleClick{
					MouseDoubleClick: &GameMouseDoubleClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
						X:      300,
						Y:      400,
					},
				},
			},
		},
		{
			name: "double_click with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDoubleClick{
					MouseDoubleClick: &GameMouseDoubleClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:      300,
						Y:      400,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "drag with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDrag{
					MouseDrag: &GameMouseDrag{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						FromX:  10,
						FromY:  20,
						ToX:    100,
						ToY:    200,
					},
				},
			},
		},
		{
			name: "drag with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDrag{
					MouseDrag: &GameMouseDrag{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						FromX:  10,
						FromY:  20,
						ToX:    100,
						ToY:    200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hover with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHover{
					MouseHover: &GameMouseHover{
						X: 500,
						Y: 600,
					},
				},
			},
		},
		{
			name: "hover with negative x",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHover{
					MouseHover: &GameMouseHover{
						X: -1,
						Y: 600,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE,
						X:          100,
						Y:          200,
						DurationMs: 1000,
					},
				},
			},
		},
		{
			name: "hold with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:          100,
						Y:          200,
						DurationMs: 1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with duration_ms=0",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: 0,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with duration_ms=30001",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: 30001,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with negative duration_ms",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: -1,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateControlRequest(tt.req)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateControlRequest() expected error, got nil")
			}
		})
	}
}

var validTestSegment = []byte("ftypisommoov-test-data-for-init")

func validTestInitID() string {
	hash := sha256.Sum256(validTestSegment)
	return hex.EncodeToString(hash[:])
}

func TestValidateMediaInit(t *testing.T) {
	validInitID := validTestInitID()

	// given
	tests := []struct {
		name    string
		msg     *GameMediaInit
		wantErr bool
	}{
		{
			name: "valid init passes",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: false,
		},
		{
			name: "empty stream_id rejected",
			msg: &GameMediaInit{
				StreamId: "",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "empty init_id rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "",
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "unsupported codec rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "vp9",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "invalid mime_type missing video/mp4 rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/webm; codecs=\"avc1\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "invalid mime_type missing avc1 rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"vp9\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "empty segment rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  []byte{},
			},
			wantErr: true,
		},
		{
			name: "oversized segment rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  make([]byte, domain.MaxSegmentSize+1),
			},
			wantErr: true,
		},
		{
			name: "init_id hash mismatch rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "wrong-hash-value",
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaInit(tt.msg)

			// then
			if tt.wantErr && err == nil {
				t.Fatal("ValidateMediaInit() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateMediaInit() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMediaSegment(t *testing.T) {
	// given
	ra := true

	tests := []struct {
		name    string
		msg     *GameMediaSegment
		wantErr bool
	}{
		{
			name: "valid segment passes",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: false,
		},
		{
			name: "empty stream_id rejected",
			msg: &GameMediaSegment{
				StreamId:     "",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "empty init_id rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "sequence zero rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     0,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "empty segment rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte{},
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "oversized segment rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      make([]byte, domain.MaxSegmentSize+1),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "random_access nil rejected",
			msg: &GameMediaSegment{
				StreamId: "stream-1",
				InitId:   "init-1",
				Sequence: 1,
				Segment:  []byte("moof-mdat-data"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaSegment(tt.msg)

			// then
			if tt.wantErr && err == nil {
				t.Fatal("ValidateMediaSegment() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateMediaSegment() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMediaInit_ErrorMessages(t *testing.T) {
	validInitID := validTestInitID()

	// given
	tests := []struct {
		name       string
		msg        *GameMediaInit
		wantSubstr string
	}{
		{
			name: "unsupported codec mentions codec",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1\"",
				Codec:    "vp9",
				Segment:  validTestSegment,
			},
			wantSubstr: "unsupported codec",
		},
		{
			name: "init_id mismatch mentions mismatch",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "bad-hash",
				MimeType: "video/mp4; codecs=\"avc1\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantSubstr: "mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaInit(tt.msg)

			// then
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}
