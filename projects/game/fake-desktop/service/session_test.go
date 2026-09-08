package service

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	game "dominion/projects/game"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

// serveAgentSide upgrades one WebSocket connection and plays the agent side of
// the bridge: it records the probe frame, answers with a status frame, then
// dispatches the given operation parts and collects the receipts in order.
func serveAgentSide(t *testing.T, ops []*game.FlowPart) (*httptest.Server, func() []*game.FlowResultPart) {
	t.Helper()
	var receipts []*game.FlowResultPart
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Receipts carry screenshot PNGs well past coder/websocket's 32 KiB
		// default read limit; mirror the executor's 10 MiB limit.
		conn.SetReadLimit(readLimit)
		defer conn.CloseNow()

		// Probe: read the first UserFrame, answer with a status frame.
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		probe := new(game.UserFrame)
		if err := proto.Unmarshal(data, probe); err != nil {
			return
		}
		if probe.GetTemplateId() != "saolei" || probe.GetSessionId() == "" {
			t.Errorf("probe frame identity = (%s, %s), want (saolei, non-empty)", probe.GetTemplateId(), probe.GetSessionId())
		}
		if n := len(probe.GetFlowParts().GetParts()); n != 1 {
			t.Errorf("probe flow_parts = %d parts, want 1", n)
		}
		reply := &game.TeamFrame{Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{
			Kind: &game.FlowPart_Status{Status: &game.StatusSignal{
				Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE,
			}},
		}}}}}
		if err := writeTeamFrame(r.Context(), conn, reply); err != nil {
			return
		}
		close(ready)

		// Dispatch the scripted operations and collect the receipts.
		for _, op := range ops {
			if err := writeTeamFrame(r.Context(), conn, &game.TeamFrame{
				Payload: &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{op}}},
			}); err != nil {
				return
			}
			for {
				_, data, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				frame := new(game.UserFrame)
				if err := proto.Unmarshal(data, frame); err != nil {
					return
				}
				results := frame.GetFlowParts().GetParts()
				if len(results) == 0 {
					continue
				}
				receipts = append(receipts, results[0].GetFlowResult())
				break
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []*game.FlowResultPart { return receipts }
}

// writeTeamFrame sends one binary-proto TeamFrame from the agent side.
func writeTeamFrame(ctx context.Context, conn *websocket.Conn, frame *game.TeamFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}

func TestServeOnceProbeAndReceipts(t *testing.T) {
	ops := []*game.FlowPart{
		keyPress("op-f2", game.KeyboardKey_KEYBOARD_KEY_F2),
		cellCenter("op-click", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 3, 4),
	}
	srv, receiptsOf := serveAgentSide(t, ops)

	scenario, err := loadScenario(scenarioProgressive)
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveOnce(ctx, wsURL(srv.URL), Config{
			Template: "saolei",
			Session:  "s1",
		}, scenario, slog.Default())
	}()

	// The scripted exchange completes once both receipts landed; the
	// executor then blocks on read until the handler returns (the connection
	// closes — serveOnce's read error may race the last receipt, so the
	// receipt count wins over an early exit).
	deadline := time.After(5 * time.Second)
receiptsLoop:
	for {
		if got := receiptsOf(); len(got) == 2 {
			break
		}
		select {
		case err := <-done:
			if len(receiptsOf()) >= 2 {
				break receiptsLoop
			}
			t.Fatalf("serveOnce exited before both receipts: %v (receipts=%d)", err, len(receiptsOf()))
		case <-deadline:
			t.Fatalf("timed out waiting for receipts, got %d", len(receiptsOf()))
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	// done may already have been consumed by the poll loop above (the
	// handler closes the connection once both receipts are out); wait only
	// if serveOnce is still running.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	receipts := receiptsOf()
	if receipts[0].GetMessage() != "F2 pressed, new game started" {
		t.Errorf("receipt[0] message = %q", receipts[0].GetMessage())
	}
	assertScreenshot(t, receipts[0], mustFixture(t, "saolei_1.png"))
	if receipts[1].GetMessage() != "click(3,4) executed" {
		t.Errorf("receipt[1] message = %q", receipts[1].GetMessage())
	}
	assertScreenshot(t, receipts[1], mustFixture(t, "saolei_3.png"))
	if receipts[1].GetToolId() != "op-click" {
		t.Errorf("receipt[1] tool_id = %q, want op-click", receipts[1].GetToolId())
	}
}

func TestServeOnceDisconnectInjection(t *testing.T) {
	ops := []*game.FlowPart{
		cellCenter("op-1", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 0, 0),
	}
	srv, receiptsOf := serveAgentSide(t, ops)

	scenario, err := loadScenario(scenarioWon)
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveOnce(ctx, wsURL(srv.URL), Config{
			Template: "saolei",
			Session:  "s1",
			Fault:    Fault{DisconnectAfterOps: 1},
		}, scenario, slog.Default())
	}()

	// The injection tears the connection down after the single receipt, so
	// serveOnce returns (the fault sentinel error) on its own.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("serveOnce returned nil after the disconnect injection, want the fault error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the injected disconnect")
	}
	if got := receiptsOf(); len(got) != 1 {
		t.Fatalf("receipts = %d, want 1", len(got))
	}
}

func TestConvertToWS(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "http://gateway:8080", want: "ws://gateway:8080"},
		{in: "https://game.liukexin.com", want: "wss://game.liukexin.com"},
		{in: "://bad", wantErr: true},
	}
	for _, tt := range tests {
		got, err := convertToWS(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("convertToWS(%q) expected an error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("convertToWS(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("convertToWS(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// wsURL mirrors the test helper shape of the gateway tests: an http test
// server URL to its ws:// form.
func wsURL(httpURL string) string {
	got, err := convertToWS(httpURL)
	if err != nil {
		return httpURL
	}
	return got
}

// mustFixture reads one embedded fixture, failing the test on error.
func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fixture(name)
	if err != nil {
		t.Fatalf("fixture(%s): %v", name, err)
	}
	return data
}
