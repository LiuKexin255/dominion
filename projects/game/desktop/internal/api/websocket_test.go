package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	tracecontext "dominion/common/gopkg/otel/tracecontext"
	game "dominion/projects/game"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
)

// wsTestServer creates an httptest.Server that upgrades to WebSocket and calls handler.
func wsTestServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			t.Logf("websocket accept failed: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		handler(conn)
	}))
}

// Test_convertToWS verifies URL scheme conversion from HTTP to WebSocket.
func Test_convertToWS(t *testing.T) {
	tests := []struct {
		name    string
		httpURL string
		want    string
		wantErr bool
	}{
		{
			name:    "https to wss",
			httpURL: "https://game.liukexin.com",
			want:    "wss://game.liukexin.com",
		},
		{
			name:    "http to ws",
			httpURL: "http://localhost:8080",
			want:    "ws://localhost:8080",
		},
		{
			name:    "https with path strips path",
			httpURL: "https://example.com/api",
			want:    "wss://example.com",
		},
		{
			name:    "invalid URL returns error",
			httpURL: "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToWS(tt.httpURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("convertToWS(%q) expected error, got nil", tt.httpURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("convertToWS(%q) unexpected error: %v", tt.httpURL, err)
			}
			if got != tt.want {
				t.Errorf("convertToWS(%q) = %q, want %q", tt.httpURL, got, tt.want)
			}
		})
	}
}

// TestWSClient_Connect_URL verifies that Connect dials the correct URL path.
func TestWSClient_Connect_URL(t *testing.T) {
	// given: mock server that captures the request path
	var gotPath string
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		// read and discard — just need the handshake to succeed
	})
	defer srv.Close()

	// when: client connects
	ws := &WSClient{}
	err := ws.Connect(context.Background(), srv.URL, "sess-123", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	// then: extract the path the server saw
	// We check by reconstructing the expected URL the client would build.
	// The httptest server URL is http://127.0.0.1:PORT, so convertToWS gives ws://127.0.0.1:PORT
	wsURL, _ := convertToWS(srv.URL)
	expectedPath := "/api/v1/sessions/sess-123/connect"

	// Make a direct request to verify path — use a second server for path capture
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv2.Close()

	ws2 := &WSClient{}
	err = ws2.Connect(context.Background(), srv2.URL, "sess-123", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws2.Close()

	// then
	if gotPath != expectedPath {
		t.Errorf("request path = %q, want %q", gotPath, expectedPath)
	}

	// verify wsURL conversion is consistent
	_ = wsURL
}

// TestWSClient_Connect_EnvHeader verifies that Connect sends the env header.
func TestWSClient_Connect_EnvHeader(t *testing.T) {
	// given: mock server that captures the env header
	var gotEnv string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEnv = r.Header.Get("env")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	// when: client connects with env header
	ws := &WSClient{}
	err := ws.Connect(context.Background(), srv.URL, "session-1", "production")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	// then: env header was sent
	if gotEnv != "production" {
		t.Errorf("env header = %q, want %q", gotEnv, "production")
	}
}

// TestWSClient_SendRecvFrame verifies protojson round-trip for SendFrame/RecvFrame.
func TestWSClient_SendRecvFrame(t *testing.T) {
	// given: mock server that reads a frame and responds with a status signal
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		// verify received frame is valid protojson
		frame := new(game.AgentFrame)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, frame); err != nil {
			return
		}

		// respond with a status signal instead
		respFrame := &game.AgentFrame{
			SessionId: frame.GetSessionId(),
			Payload: &game.AgentFrame_Status{
				Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE},
			},
		}
		resp, _ := protojson.Marshal(respFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: client connects and sends a status signal
	ws := &WSClient{}
	err := ws.Connect(context.Background(), srv.URL, "test-session", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	sendFrame := &game.AgentFrame{
		SessionId: "test-session",
		Payload: &game.AgentFrame_Status{
			Status: &game.StatusSignal{
				Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE,
			},
		},
	}
	err = ws.SendFrame(context.Background(), sendFrame)
	if err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: received frame carries the status signal
	resp, err := ws.RecvFrame(context.Background())
	if err != nil {
		t.Fatalf("RecvFrame() unexpected error: %v", err)
	}
	if resp.GetSessionId() != "test-session" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "test-session")
	}
	statusPayload := resp.GetStatus()
	if statusPayload == nil {
		t.Fatal("Status payload is nil, want non-nil")
	}
	if statusPayload.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("Status.Status = %q, want %q", statusPayload.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	}
}

// TestWSClient_SendRecvFrame_Content verifies protojson round-trip for a
// content PartBlock carrying text and an image.
func TestWSClient_SendRecvFrame_ContentImage(t *testing.T) {
	// given: mock server that reads a content frame and responds with a status signal
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		frame := new(game.AgentFrame)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, frame); err != nil {
			return
		}

		content := frame.GetContent()
		if content == nil {
			return
		}

		respFrame := &game.AgentFrame{
			SessionId: frame.GetSessionId(),
			Payload: &game.AgentFrame_Status{
				Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE},
			},
		}
		resp, _ := protojson.Marshal(respFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: client connects and sends a content frame with text+image parts
	ws := &WSClient{}
	err := ws.Connect(context.Background(), srv.URL, "test-session", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	imageData := []byte("fake-png-data")
	sendFrame := &game.AgentFrame{
		SessionId: "test-session",
		FrameId:   "frame-001",
		Sender:    game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{Parts: []*game.Part{
				{Kind: &game.Part_Text{Text: &game.TextPart{Content: "look"}}},
				{Kind: &game.Part_Image{Image: &game.ImagePart{
					Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
					Data:        imageData,
					WidthPx:     1920,
					HeightPx:    1080,
					ScaleFactor: 1.0,
					WindowTitle: "Test Window",
				}}},
			}},
		},
	}
	err = ws.SendFrame(context.Background(), sendFrame)
	if err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: received status signal confirms the content round-trip
	resp, err := ws.RecvFrame(context.Background())
	if err != nil {
		t.Fatalf("RecvFrame() unexpected error: %v", err)
	}
	statusPayload := resp.GetStatus()
	if statusPayload == nil {
		t.Fatal("Status payload is nil, want non-nil")
	}
	if statusPayload.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE {
		t.Errorf("Status.Status = %q, want %q", statusPayload.GetStatus(), game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE)
	}
}

// TestWSClient_Close_NotConnected verifies Close is safe when not connected.
func TestWSClient_Close_NotConnected(t *testing.T) {
	// given: a WSClient that was never connected
	ws := &WSClient{}

	// when/then: Close should not panic and return nil
	err := ws.Close()
	if err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// TestWSConnect_Traceparent verifies that Connect injects a W3C traceparent header
// into the WebSocket upgrade request when the context carries a valid trace context.
func TestWSConnect_Traceparent(t *testing.T) {
	// given: a context with a valid trace context
	ctx := tracecontext.Ensure(context.Background())
	expectedTraceID := tracecontext.ID(ctx)

	// start a server that captures the traceparent header from the upgrade request
	var traceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	// when: connect with the trace context
	ws := &WSClient{}
	err := ws.Connect(ctx, srv.URL, "sess-trace", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	// then: traceparent header is present
	if traceparent == "" {
		t.Fatal("server received no traceparent header")
	}

	// then: traceparent matches W3C format: 00-{32hex}-{16hex}-{flags}
	matched, err := regexp.MatchString(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`, traceparent)
	if err != nil {
		t.Fatalf("regexp error: %v", err)
	}
	if !matched {
		t.Fatalf("traceparent header = %q, want format 00-{32hex}-{16hex}-{flags}", traceparent)
	}

	// then: traceparent contains the expected trace ID
	if !strings.Contains(traceparent, expectedTraceID) {
		t.Fatalf("traceparent header = %q, expected to contain trace ID %q", traceparent, expectedTraceID)
	}
}

// TestWSClient_SendFrame_NotConnected verifies SendFrame returns error when not connected.
func TestWSClient_SendFrame_NotConnected(t *testing.T) {
	// given: a WSClient that was never connected
	ws := &WSClient{}

	// when: sending a frame
	err := ws.SendFrame(context.Background(), &game.AgentFrame{
		SessionId: "x",
		Payload: &game.AgentFrame_Status{
				Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE},
		},
	})

	// then: should return error about not connected
	if err == nil {
		t.Fatal("SendFrame() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("SendFrame() error = %q, want containing 'not connected'", err.Error())
	}
}

// TestWSClient_CloseDuringRecvFrame_NoDeadlock is the R5 deadlock regression
// test: Close must return promptly while RecvFrame is blocked on conn.Read.
// The pre-fix RecvFrame held w.mu across conn.Read, and Close held w.mu across
// conn.Close — so Close waited for the mu RecvFrame never released (deadlock).
// The fix snapshots the conn under w.mu and releases it before Read/Close.
func TestWSClient_CloseDuringRecvFrame_NoDeadlock(t *testing.T) {
	// given: a server that blocks on Read (never sends), so the client's
	// RecvFrame blocks inside conn.Read. The server must actively Read so it
	// processes the client's close frame during the close handshake — a
	// select{} server would never read the close frame, stretching
	// conn.Close's waitCloseHandshake to its 5s timeout and masking the
	// w.mu deadlock this test is meant to catch.
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, _ = conn.Read(ctx)
	})
	defer srv.Close()

	ws := &WSClient{}
	if err := ws.Connect(context.Background(), srv.URL, "deadlock-test", "test-env"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	// when: RecvFrame blocks on conn.Read (background context — no timeout;
	// only Close unblocking the connection can end it)
	recvErr := make(chan error, 1)
	go func() {
		_, err := ws.RecvFrame(context.Background())
		recvErr <- err
	}()

	// Let RecvFrame enter conn.Read before calling Close.
	time.Sleep(100 * time.Millisecond)

	// then: Close must return within 2s. Under the pre-fix code Close deadlocked
	// waiting for w.mu held by the in-flight RecvFrame.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ws.Close()
	}()
	select {
	case <-done:
		// Close returned without deadlock — pass
	case <-time.After(2 * time.Second):
		t.Fatal("Close() deadlocked: did not return within 2s while RecvFrame was in-flight")
	}

	// then: RecvFrame must have unblocked (conn.Close terminated the Read) and
	// returned an error — proving the two operations no longer serialize on w.mu.
	select {
	case err := <-recvErr:
		if err == nil {
			t.Error("RecvFrame returned nil error, want an error after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RecvFrame did not unblock within 2s after Close")
	}
}

// TestWSClient_RecvFrame_ContextCancel verifies RecvFrame detects context
// cancellation via coder/websocket's AfterFunc that closes the connection.
func TestWSClient_RecvFrame_ContextCancel(t *testing.T) {
	// given: mock server that reads one frame then blocks forever (never replies)
	srv := wsTestServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		// Block — never writes back.
		select {}
	})
	defer srv.Close()

	ws := &WSClient{}
	err := ws.Connect(context.Background(), srv.URL, "test-session", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	// Send a frame first so the server consumes it and enters the blocking select.
	err = ws.SendFrame(context.Background(), &game.AgentFrame{
		SessionId: "test-session",
		Payload: &game.AgentFrame_Status{
				Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE},
		},
	})
	if err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// when: RecvFrame with a short timeout — server will never reply.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// then: RecvFrame should fail because the timeout fires and closes the connection.
	_, err = ws.RecvFrame(ctx)
	if err == nil {
		t.Fatal("RecvFrame() expected error with expiring context, got nil")
	}
}
