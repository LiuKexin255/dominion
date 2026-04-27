package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"

	"github.com/coder/websocket"
)

// newEchoWS starts an httptest server that accepts WebSocket upgrades and
// echoes every text frame back to the client. The server URL (ws://...) is
// returned along with a shutdown function.
func newEchoWS(t *testing.T) (url string, shutdown func()) {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(handler)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", srv.Close
}

// newWriterWS starts an httptest server that accepts WebSocket upgrades,
// writes the provided envelopes sequentially, then closes.
func newWriterWS(t *testing.T, envelopes ...*gw.GameWebSocketEnvelope) (url string, shutdown func()) {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for _, env := range envelopes {
			data, err := EncodeEnvelope(env)
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(handler)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", srv.Close
}

func TestClient_ConnectAndClose(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// when
	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	if !client.IsConnected() {
		t.Fatalf("IsConnected should be true after Connect")
	}

	// then
	if err := client.Close(); err != nil {
		t.Fatalf("Close unexpected error: %v", err)
	}
}

func TestClient_DoubleClose(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}

	// when
	if err := client.Close(); err != nil {
		t.Fatalf("first Close unexpected error: %v", err)
	}

	// then
	if err := client.Close(); err != nil {
		t.Fatalf("second Close unexpected error: %v", err)
	}
}

func TestClient_SendHello(t *testing.T) {
	// given: echo server that returns what it receives
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	if err := client.SendHello(ctx, "session-1"); err != nil {
		t.Fatalf("SendHello unexpected error: %v", err)
	}

	// then: read the echoed message and verify envelope
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	if env.SessionId != "session-1" {
		t.Fatalf("SessionId: got %q, want %q", env.SessionId, "session-1")
	}
	hello := env.GetHello()
	if hello == nil {
		t.Fatalf("expected hello payload, got %T", env.Payload)
	}
	if hello.Role != AgentRole {
		t.Fatalf("Role: got %v, want %v", hello.Role, AgentRole)
	}
}

func TestClient_SendMediaInit(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	segment := []byte{0x00, 0x01, 0x02, 0x03}

	// when
	if err := client.SendMediaInit(ctx, "session-1", MimeTypeMP4, segment); err != nil {
		t.Fatalf("SendMediaInit unexpected error: %v", err)
	}

	// then
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	mi := env.GetMediaInit()
	if mi == nil {
		t.Fatalf("expected media_init payload, got %T", env.Payload)
	}
	if mi.MimeType != MimeTypeMP4 {
		t.Fatalf("MimeType: got %q, want %q", mi.MimeType, MimeTypeMP4)
	}
	if string(mi.Segment) != string(segment) {
		t.Fatalf("Segment: got %v, want %v", mi.Segment, segment)
	}
}

func TestClient_SendMediaInitOversized(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	oversized := make([]byte, domain.MaxSegmentSize+1)

	// when
	err := client.SendMediaInit(ctx, "session-1", MimeTypeMP4, oversized)

	// then
	if err == nil {
		t.Fatalf("SendMediaInit with oversized segment should return error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error should mention exceeds, got: %v", err)
	}
}

func TestClient_SendMediaSegmentOversized(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	oversized := make([]byte, domain.MaxSegmentSize+1)

	// when
	err := client.SendMediaSegment(ctx, "session-1", "seg-1", oversized, true)

	// then
	if err == nil {
		t.Fatalf("SendMediaSegment with oversized segment should return error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error should mention exceeds, got: %v", err)
	}
}

func TestClient_SendControlAck(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	if err := client.SendControlAck(ctx, "session-1", "op-123"); err != nil {
		t.Fatalf("SendControlAck unexpected error: %v", err)
	}

	// then
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	ack := env.GetControlAck()
	if ack == nil {
		t.Fatalf("expected control_ack payload, got %T", env.Payload)
	}
	if ack.OperationId != "op-123" {
		t.Fatalf("OperationId: got %q, want %q", ack.OperationId, "op-123")
	}
}

func TestClient_SendControlResult(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	if err := client.SendControlResult(ctx, "session-1", "op-456", gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED); err != nil {
		t.Fatalf("SendControlResult unexpected error: %v", err)
	}

	// then
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	cr := env.GetControlResult()
	if cr == nil {
		t.Fatalf("expected control_result payload, got %T", env.Payload)
	}
	if cr.OperationId != "op-456" {
		t.Fatalf("OperationId: got %q, want %q", cr.OperationId, "op-456")
	}
	if cr.Status != gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED {
		t.Fatalf("Status: got %v, want SUCCEEDED", cr.Status)
	}
}

func TestClient_SendPong(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	if err := client.SendPong(ctx, "session-1", "nonce-abc"); err != nil {
		t.Fatalf("SendPong unexpected error: %v", err)
	}

	// then
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	pong := env.GetPong()
	if pong == nil {
		t.Fatalf("expected pong payload, got %T", env.Payload)
	}
	if pong.Nonce != "nonce-abc" {
		t.Fatalf("Nonce: got %q, want %q", pong.Nonce, "nonce-abc")
	}
}

func TestClient_SendError(t *testing.T) {
	// given
	wsURL, shutdown := newEchoWS(t)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	if err := client.SendError(ctx, "session-1", "ERR_INTERNAL", "something broke"); err != nil {
		t.Fatalf("SendError unexpected error: %v", err)
	}

	// then
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message unexpected error: %v", err)
	}
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope unexpected error: %v", err)
	}
	gerr := env.GetError()
	if gerr == nil {
		t.Fatalf("expected error payload, got %T", env.Payload)
	}
	if gerr.Code != "ERR_INTERNAL" {
		t.Fatalf("Code: got %q, want %q", gerr.Code, "ERR_INTERNAL")
	}
	if gerr.Message != "something broke" {
		t.Fatalf("Message: got %q, want %q", gerr.Message, "something broke")
	}
}

func TestClient_ReadLoopDispatchesControlRequest(t *testing.T) {
	// given: server sends a control_request envelope
	controlEnv := &gw.GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "msg-ctrl",
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-789",
				Kind:        gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
			},
		},
	}
	wsURL, shutdown := newWriterWS(t, controlEnv)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	ch, err := client.ReadLoop(ctx)
	if err != nil {
		t.Fatalf("ReadLoop unexpected error: %v", err)
	}

	// then
	msg, ok := <-ch
	if !ok {
		t.Fatalf("ReadLoop channel closed without message")
	}
	if msg.ControlRequest == nil {
		t.Fatalf("expected ControlRequest, got nil")
	}
	if msg.ControlRequest.OperationId != "op-789" {
		t.Fatalf("OperationId: got %q, want %q", msg.ControlRequest.OperationId, "op-789")
	}
}

func TestClient_ReadLoopDispatchesPing(t *testing.T) {
	// given: server sends a ping envelope
	pingEnv := &gw.GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "msg-ping",
		Payload: &gw.GameWebSocketEnvelope_Ping{
			Ping: &gw.GamePing{Nonce: "test-nonce"},
		},
	}
	wsURL, shutdown := newWriterWS(t, pingEnv)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	ch, err := client.ReadLoop(ctx)
	if err != nil {
		t.Fatalf("ReadLoop unexpected error: %v", err)
	}

	// then
	msg, ok := <-ch
	if !ok {
		t.Fatalf("ReadLoop channel closed without message")
	}
	if msg.Ping == nil {
		t.Fatalf("expected Ping, got nil")
	}
	if msg.Ping.Nonce != "test-nonce" {
		t.Fatalf("Nonce: got %q, want %q", msg.Ping.Nonce, "test-nonce")
	}
}

func TestClient_ReadLoopDispatchesError(t *testing.T) {
	// given: server sends an error envelope
	errEnv := &gw.GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "msg-err",
		Payload: &gw.GameWebSocketEnvelope_Error{
			Error: &gw.GameError{Code: "ERR_TEST", Message: "test error"},
		},
	}
	wsURL, shutdown := newWriterWS(t, errEnv)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	ch, err := client.ReadLoop(ctx)
	if err != nil {
		t.Fatalf("ReadLoop unexpected error: %v", err)
	}

	// then
	msg, ok := <-ch
	if !ok {
		t.Fatalf("ReadLoop channel closed without message")
	}
	if msg.Error == nil {
		t.Fatalf("expected Error, got nil")
	}
	if msg.Error.Code != "ERR_TEST" {
		t.Fatalf("Code: got %q, want %q", msg.Error.Code, "ERR_TEST")
	}
}

func TestClient_ReadLoopIgnoresUnknownType(t *testing.T) {
	// given: server sends a hello envelope (which the agent read loop ignores)
	helloEnv := &gw.GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "msg-hello",
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{Role: gw.GameClientRole_GAME_CLIENT_ROLE_WEB},
		},
	}
	controlEnv := &gw.GameWebSocketEnvelope{
		SessionId: "session-1",
		MessageId: "msg-ctrl",
		Payload: &gw.GameWebSocketEnvelope_ControlRequest{
			ControlRequest: &gw.GameControlRequest{
				OperationId: "op-after-hello",
			},
		},
	}
	wsURL, shutdown := newWriterWS(t, helloEnv, controlEnv)
	defer shutdown()

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	ch, err := client.ReadLoop(ctx)
	if err != nil {
		t.Fatalf("ReadLoop unexpected error: %v", err)
	}

	// then: should receive the control_request (hello is ignored)
	msg, ok := <-ch
	if !ok {
		t.Fatalf("ReadLoop channel closed without message")
	}
	if msg.ControlRequest == nil {
		t.Fatalf("expected ControlRequest after ignored hello, got nil")
	}
	if msg.ControlRequest.OperationId != "op-after-hello" {
		t.Fatalf("OperationId: got %q, want %q", msg.ControlRequest.OperationId, "op-after-hello")
	}
}

func TestClient_ReadLoopClosesOnServerClose(t *testing.T) {
	// given: server that immediately closes after accepting
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "immediate-close")
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect unexpected error: %v", err)
	}
	defer client.Close()

	// when
	ch, err := client.ReadLoop(ctx)
	if err != nil {
		t.Fatalf("ReadLoop unexpected error: %v", err)
	}

	// then: channel should close
	_, ok := <-ch
	if ok {
		t.Fatalf("expected channel to be closed when server disconnects")
	}
}
