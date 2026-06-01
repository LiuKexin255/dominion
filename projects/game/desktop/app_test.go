package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockWSServer creates an httptest server that upgrades to WebSocket and calls handler.
func mockWSServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
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

// TestConnectAgent_ProbeSuccess verifies that ConnectAgent stores a.ws
// when the probe round-trip succeeds.
func TestConnectAgent_ProbeSuccess(t *testing.T) {
	// given: mock WS server that responds to the probe ping with any frame
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		// read the probe frame
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		frame := new(game.AgentFrame)
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, frame); err != nil {
			return
		}
		// respond with an ack frame (any response proves the round-trip)
		ackFrame := &game.AgentFrame{
			SessionId:  frame.GetSessionId(),
			FrameId:    "test-ack-frame",
			CreateTime: timestamppb.Now(),
			Payload: &game.AgentFrame_Ack{
				Ack: &game.AgentAckFrame{AckFrameId: frame.GetFrameId(), Message: "ok"},
			},
		}
		resp, _ := protojson.Marshal(ackFrame)
		conn.Write(ctx, websocket.MessageText, resp)
	})
	defer srv.Close()

	// when: ConnectAgent with a valid session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	err := app.ConnectAgent("test-session")

	// then: probe succeeds, ws and sessionID are stored
	if err != nil {
		t.Fatalf("ConnectAgent() unexpected error: %v", err)
	}
	if app.ws == nil {
		t.Fatal("expected app.ws to be non-nil after successful ConnectAgent")
	}
	if app.sessionID != "test-session" {
		t.Fatalf("expected sessionID %q, got %q", "test-session", app.sessionID)
	}

	// clean up
	app.CloseAgent()
}

// TestConnectAgent_ProbeFailure verifies that ConnectAgent closes WS
// and does NOT store state when the probe times out (no response).
func TestConnectAgent_ProbeFailure(t *testing.T) {
	// given: mock WS server that reads the probe but never responds
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		// Block forever — simulating an unreachable agent.
		select {}
	})
	defer srv.Close()

	// when: ConnectAgent with a valid session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	err := app.ConnectAgent("test-session")

	// then: probe fails, ws is nil, error returned
	if err == nil {
		t.Fatal("ConnectAgent() expected error, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil after failed ConnectAgent")
	}
	if app.sessionID != "" {
		t.Fatalf("expected sessionID to be empty, got %q", app.sessionID)
	}

	// the error should mention probe or receive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "probe") && !strings.Contains(errMsg, "receive") {
		t.Errorf("error message should mention probe/receive, got: %s", errMsg)
	}
}

// TestConnectAgent_EmptySessionID verifies empty sessionID returns error immediately.
func TestConnectAgent_EmptySessionID(t *testing.T) {
	// given: App with no connection
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when: ConnectAgent with empty session ID
	err := app.ConnectAgent("")

	// then: immediate error, no ws state change
	if err == nil {
		t.Fatal("ConnectAgent() expected error for empty session_id, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error should mention session_id, got: %s", err.Error())
	}
}

// TestConnectAgent_ProbeTimeout verifies 10-second timeout triggers properly.
func TestConnectAgent_ProbeTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// given: mock WS server that never responds
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		select {} // block forever
	})
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	// when: ConnectAgent should time out after 10 seconds
	start := time.Now()
	err := app.ConnectAgent("test-session")
	elapsed := time.Since(start)

	// then: probe times out
	if err == nil {
		t.Fatal("ConnectAgent() expected error from timeout, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil after timeout")
	}
	// Timeout should be around 10 seconds (allow some tolerance)
	if elapsed > 15*time.Second {
		t.Errorf("timeout took too long: %v, expected ~10s", elapsed)
	}
	t.Logf("timeout occurred after: %v", elapsed)
}
