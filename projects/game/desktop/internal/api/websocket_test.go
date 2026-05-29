package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	err := ws.Connect(srv.URL, "sess-123", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	// then: extract the path the server saw
	// We check by reconstructing the expected URL the client would build.
	// The httptest server URL is http://127.0.0.1:PORT, so convertToWS gives ws://127.0.0.1:PORT
	wsURL, _ := convertToWS(srv.URL)
	expectedPath := "/api/v1/sessions/sess-123/agent/connect"

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
	err = ws2.Connect(srv2.URL, "sess-123", "test-env")
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
	err := ws.Connect(srv.URL, "session-1", "production")
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
	// given: mock server that echoes frames with modified payload
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

		// echo back with status payload instead
		echoFrame := &game.AgentFrame{
			SessionId: frame.GetSessionId(),
			Payload: &game.AgentFrame_Echo{
				Echo: &game.AgentEchoFrame{
					Data: []byte("echo-back"),
				},
			},
		}
		resp, _ := protojson.Marshal(echoFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: client connects and sends a status frame
	ws := &WSClient{}
	err := ws.Connect(srv.URL, "test-session", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	sendFrame := &game.AgentFrame{
		SessionId: "test-session",
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{
				Status: "ping",
			},
		},
	}
	err = ws.SendFrame(sendFrame)
	if err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: received frame has echoed payload
	resp, err := ws.RecvFrame()
	if err != nil {
		t.Fatalf("RecvFrame() unexpected error: %v", err)
	}
	if resp.GetSessionId() != "test-session" {
		t.Errorf("SessionId = %q, want %q", resp.GetSessionId(), "test-session")
	}
	echoPayload := resp.GetEcho()
	if echoPayload == nil {
		t.Fatal("Echo payload is nil, want non-nil")
	}
	if string(echoPayload.GetData()) != "echo-back" {
		t.Errorf("Echo.Data = %q, want %q", string(echoPayload.GetData()), "echo-back")
	}
}

// TestWSClient_SendRecvFrame_Screenshot verifies protojson round-trip for screenshot frames.
func TestWSClient_SendRecvFrame_Screenshot(t *testing.T) {
	// given: mock server that receives a screenshot and echoes it back with ack
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

		screenshot := frame.GetScreenshot()
		if screenshot == nil {
			return
		}

		// respond with ack referencing the capture
		ackFrame := &game.AgentFrame{
			SessionId: frame.GetSessionId(),
			Payload: &game.AgentFrame_Ack{
				Ack: &game.AgentAckFrame{
					AckFrameId: screenshot.GetCaptureId(),
					Message:    "screenshot received",
				},
			},
		}
		resp, _ := protojson.Marshal(ackFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: client connects and sends a screenshot frame
	ws := &WSClient{}
	err := ws.Connect(srv.URL, "test-session", "test-env")
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	defer ws.Close()

	imageData := []byte("fake-png-data")
	sendFrame := &game.AgentFrame{
		SessionId: "test-session",
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId:   "cap-001",
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        imageData,
				WidthPx:     1920,
				HeightPx:    1080,
				ClientXPx:   0,
				ClientYPx:   0,
				ScaleFactor: 1.0,
				WindowTitle: "Test Window",
			},
		},
	}
	err = ws.SendFrame(sendFrame)
	if err != nil {
		t.Fatalf("SendFrame() unexpected error: %v", err)
	}

	// then: received ack references the capture ID
	resp, err := ws.RecvFrame()
	if err != nil {
		t.Fatalf("RecvFrame() unexpected error: %v", err)
	}
	ackPayload := resp.GetAck()
	if ackPayload == nil {
		t.Fatal("Ack payload is nil, want non-nil")
	}
	if ackPayload.GetAckFrameId() != "cap-001" {
		t.Errorf("Ack.AckFrameId = %q, want %q", ackPayload.GetAckFrameId(), "cap-001")
	}
	if ackPayload.GetMessage() != "screenshot received" {
		t.Errorf("Ack.Message = %q, want %q", ackPayload.GetMessage(), "screenshot received")
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

// TestWSClient_SendFrame_NotConnected verifies SendFrame returns error when not connected.
func TestWSClient_SendFrame_NotConnected(t *testing.T) {
	// given: a WSClient that was never connected
	ws := &WSClient{}

	// when: sending a frame
	err := ws.SendFrame(&game.AgentFrame{
		SessionId: "x",
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: "test"},
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
