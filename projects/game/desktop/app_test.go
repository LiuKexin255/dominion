package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
	"dominion/projects/game/desktop/internal/capture"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
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

// TestConnect_ProbeSuccess verifies that Connect stores a.ws
// when the probe round-trip succeeds.
func TestConnect_ProbeSuccess(t *testing.T) {
	// given: mock WS server that responds to the probe status signal with any frame
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		// read the probe frame
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		frame := new(game.UserFrame)
		if err := proto.Unmarshal(data, frame); err != nil {
			return
		}
		// respond with a status signal (any response proves the round-trip).
		// Status rides as a FlowPart kind (spec 023 C3 / FR-003).
		respFrame := &game.TeamFrame{
			SessionId:  frame.GetSessionId(),
			TemplateId: frame.GetTemplateId(),
			FrameId:    "test-status-frame",
			CreateTime: timestamppb.Now(),
			Payload: &game.TeamFrame_FlowParts{
				FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
				}},
			},
		}
		resp, _ := proto.Marshal(respFrame)
		conn.Write(ctx, websocket.MessageBinary, resp)
	})
	defer srv.Close()

	// when: Connect with a valid template + session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	status, err := app.Connect("saolei", "test-session")

	// then: probe succeeds, ws and sessionID are stored, status returned
	if err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if app.ws == nil {
		t.Fatal("expected app.ws to be non-nil after successful Connect")
	}
	if app.sessionID != "test-session" {
		t.Fatalf("expected sessionID %q, got %q", "test-session", app.sessionID)
	}
	if app.template != "saolei" {
		t.Fatalf("expected template %q, got %q", "saolei", app.template)
	}
	if status != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE.String() {
		t.Fatalf("expected status %q, got %q", game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE.String(), status)
	}

	// clean up
	app.CloseAgent()
}

// TestConnect_ProbeFailure verifies that Connect closes WS
// and does NOT store state when the probe times out (no response).
func TestConnect_ProbeFailure(t *testing.T) {
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

	// when: Connect with a valid session ID
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	_, err := app.Connect("saolei", "test-session")

	// then: probe fails, ws is nil, error returned
	if err == nil {
		t.Fatal("Connect() expected error, got nil")
	}
	if app.ws != nil {
		t.Fatal("expected app.ws to be nil after failed Connect")
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

// TestConnect_EmptyTemplateOrSessionID verifies empty params return error immediately.
func TestConnect_EmptyTemplateOrSessionID(t *testing.T) {
	tests := []struct {
		name     string
		template string
		session  string
		wantErr  string
	}{
		{name: "empty template", template: "", session: "s1", wantErr: "template"},
		{name: "empty session_id", template: "saolei", session: "", wantErr: "session_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: App with no connection
			logger := applog.NewLogger()
			app := NewApp(logger)
			app.SetContext(context.Background())

			// when: Connect with invalid params
			_, err := app.Connect(tt.template, tt.session)

			// then: immediate error, no ws state change
			if err == nil {
				t.Fatal("Connect() expected error, got nil")
			}
			if app.ws != nil {
				t.Fatal("expected app.ws to be nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error should mention %q, got: %s", tt.wantErr, err.Error())
			}
		})
	}
}

// TestConnect_ProbeTimeout verifies 10-second timeout triggers properly.
func TestConnect_ProbeTimeout(t *testing.T) {
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

	// when: Connect should time out after 10 seconds
	start := time.Now()
	_, err := app.Connect("saolei", "test-session")
	elapsed := time.Since(start)

	// then: probe times out
	if err == nil {
		t.Fatal("Connect() expected error from timeout, got nil")
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

// Test_executeAgentOperation_NoWindowSelected verifies spec 025 FR-005: when no
// window is selected the result is FAILED with "no window selected" and no
// screenshot is attached (precondition early-return — no screenshot is
// possible without a selected window). Replaces the former "no window bound"
// guard removed in spec 025 FR-006.
func Test_executeAgentOperation_NoWindowSelected(t *testing.T) {
	// given: App with no selected window (selectedWin zero value)
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	op := &game.FlowPart{
		Kind: &game.FlowPart_MouseClick{
			MouseClick: &game.MouseClickPart{
				ToolId: "op-no-window",
				Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			},
		},
	}

	// when
	result := app.executeAgentOperation(op)

	// then: FAILED precondition, no screenshot
	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if !strings.Contains(result.GetMessage(), "no window selected") {
		t.Errorf("expected message to mention 'no window selected', got %q", result.GetMessage())
	}
	if result.GetScreenshot() != nil {
		t.Errorf("expected nil screenshot when no window is selected, got non-nil")
	}
	if result.GetToolId() != "op-no-window" {
		t.Errorf("expected tool_id %q, got %q", "op-no-window", result.GetToolId())
	}
}

// Test_executeAgentOperation_ActionAndScreenshotFail_NoEarlyReturn verifies
// Rule 5: when the action fails AND the screenshot capture fails, the
// function must NOT early-return on the action error — it must reach the
// screenshot phase and record both failures in the result message. A click
// action on the Linux stub fails inside ExecuteClickAtCurrentPos ("not
// supported") and CaptureWindow (screenshot) also returns "not supported",
// which is the exact Rule 5 scenario. The click path performs no window
// bounds capture or coordinate conversion, so only the click action and the
// screenshot capture failures are recorded. Status reflects the action
// failure (never SUCCEEDED), screenshot is nil.
func Test_executeAgentOperation_ActionAndScreenshotFail_NoEarlyReturn(t *testing.T) {
	// given: App with a selected window (Linux stubs make every executor fail,
	// but the resolved handle lets executeAgentOperation proceed to the action
	// phase rather than short-circuiting at "no window selected"). A click
	// action on the Linux stub fails inside ExecuteClickAtCurrentPos ("not
	// supported") and CaptureWindow (screenshot) also returns "not supported",
	// which is the exact Rule 5 scenario. The click path performs no window
	// bounds capture or coordinate conversion, so only the click action and the
	// screenshot capture failures are recorded. Status reflects the action
	// failure (never SUCCEEDED), screenshot is nil.
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	op := &game.FlowPart{
		Kind: &game.FlowPart_MouseClick{
			MouseClick: &game.MouseClickPart{
				ToolId: "op-both-fail",
				Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			},
		},
	}

	// when
	result := app.executeAgentOperation(op)

	// then: status FAILED (action outcome), screenshot nil (capture failed),
	// and the message records BOTH the click action failure and the screenshot
	// capture failure — proving the screenshot phase ran despite the action
	// error rather than early-returning. The click path performs no window
	// bounds capture, so the message must NOT mention "capture window bounds".
	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if result.GetScreenshot() != nil {
		t.Errorf("expected nil screenshot when capture fails, got non-nil")
	}
	msg := result.GetMessage()
	if !strings.Contains(msg, "click action") {
		t.Errorf("message should record the click action failure, got %q", msg)
	}
	if strings.Contains(msg, "capture window bounds") {
		t.Errorf("click path must not capture window bounds, but message mentions it: %q", msg)
	}
	if !strings.Contains(msg, "screenshot capture failed") {
		t.Errorf("message should record screenshot capture failure (proves no early return on action error), got %q", msg)
	}
	if result.GetToolId() != "op-both-fail" {
		t.Errorf("expected tool_id %q, got %q", "op-both-fail", result.GetToolId())
	}
}

// TestReadLoop_ExitsOnRecvError verifies the reader error path: when
// RecvFrame errors (connection closed by the peer), readLoop logs the failure,
// returns, and closes recvDone so CloseAgent and the reconnect handover
// unblock (FR-010, specs/041-realtime-init-push/contracts/
// realtime-channel-contract.md §3.1 Exit).
func TestReadLoop_ExitsOnRecvError(t *testing.T) {
	// given: mock WS server that closes the connection immediately, causing
	// the first RecvFrame to error.
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		conn.Close(websocket.StatusNormalClosure, "")
	})
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// connect a WS client directly (bypassing Connect's probe)
	ws := &api.WSClient{}
	if err := ws.Connect(context.Background(), srv.URL, "saolei", "sess-recv", "test"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer ws.Close()
	app.ws = ws

	// when: run readLoop synchronously — it errors on the first RecvFrame,
	// logs, and returns.
	app.recvDone = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.readLoop("sess-recv")
	}()

	// then: the reader exited and recvDone is closed.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit within 2s after a recv error")
	}
	select {
	case <-app.recvDone:
	default:
		t.Fatal("recvDone not closed after readLoop exited")
	}
}

// TestReadLoop_ExecutesOperationAndSurvivesWait verifies the core readLoop
// behavior (spec 025 FR-023/FR-024; FR-009 in
// specs/041-realtime-init-push/contracts/realtime-channel-contract.md §3.2/§3.3):
// an inbound operation FlowPart is executed and its FlowResultPart is sent
// back over the WebSocket to the agent on the control channel, and the
// trailing wait signal does NOT terminate the continuous reader (FR-008).
func TestReadLoop_ExecutesOperationAndSurvivesWait(t *testing.T) {
	// given: mock WS server sends a flowParts frame with a MouseClickPart, then
	// a wait signal. It captures client-sent frames (the tool result) so the
	// test can assert the result was returned to the agent over the WS.
	clickFrame := &game.TeamFrame{
		SessionId:  "op-session",
		TemplateId: "saolei",
		FrameId:    "srv-click-1",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{
					ToolId: "click-1",
					Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
				}}},
			}},
		},
	}
	waitFrame := &game.TeamFrame{
		SessionId:  "op-session",
		TemplateId: "saolei",
		FrameId:    "srv-wait-1",
		Payload:    &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{}}}}}},
	}
	sentFrames := make(chan *game.TeamFrame, 4)
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		go func() {
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				var f game.TeamFrame
				if err := proto.Unmarshal(data, &f); err == nil {
					select {
					case sentFrames <- &f:
					default:
					}
				}
			}
		}()
		for _, f := range []*game.TeamFrame{clickFrame, waitFrame} {
			data, _ := proto.Marshal(f)
			if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
		select {} // keep the connection open until teardown
	})
	defer srv.Close()

	// given: an App with a connected WSClient. No window is selected, so
	// executeAgentOperation fails fast with "no window selected" — the result
	// frame is still produced and sent over the WS.
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	app.ws = &api.WSClient{}
	if err := app.ws.Connect(context.Background(), srv.URL, "saolei", "op-session", "test-env"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// when: run the continuous reader (the wait signal does NOT terminate it —
	// FR-008)
	app.recvDone = make(chan struct{})
	go app.readLoop("op-session")
	t.Cleanup(func() { app.CloseAgent() })

	// then: the operation was executed and its result sent to the agent over
	// the WS as a flowParts frame carrying a flowResult (FAILED: no window).
	var resultPart *game.FlowResultPart
	deadline := time.After(2 * time.Second)
	for resultPart == nil {
		select {
		case f := <-sentFrames:
			fp := f.GetFlowParts()
			if fp != nil && len(fp.GetParts()) > 0 {
				resultPart = fp.GetParts()[0].GetFlowResult()
			}
		case <-deadline:
			t.Fatal("no flow-result frame sent over WS within 2s")
		}
	}
	if resultPart.GetToolId() != "click-1" {
		t.Errorf("result tool_id = %q, want %q", resultPart.GetToolId(), "click-1")
	}
	if resultPart.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Errorf("result status = %v, want FAILED (no window selected)", resultPart.GetStatus())
	}

	// and: the wait signal did NOT terminate the reader — recvDone is still
	// open (FR-008).
	select {
	case <-app.recvDone:
		t.Fatal("readLoop terminated on the wait signal; it must survive wait (FR-008)")
	default:
	}
}

// TestReadLoop_ExecutesNewPartKinds is the regression guard for the readLoop
// filter: it must admit KeyboardPressPart and MouseMoveAndClickPart (the two
// operation kinds from spec 018-saolei-mcp FR-004a/FR-004b), not just
// MouseMovePart/MouseClickPart. Each subtest sends one operation kind through
// readLoop with no window bound, so executeAgentOperation fails fast — but a
// FlowResultPart is still produced and sent over the WS (spec 025 FR-023). If
// the filter regresses (drops the operation), no result is sent.
func TestReadLoop_ExecutesNewPartKinds(t *testing.T) {
	tests := []struct {
		name   string
		part   *game.FlowPart
		toolID string
	}{
		{
			name: "KeyboardPressPart (saolei_init F2 dispatch)",
			part: &game.FlowPart{Kind: &game.FlowPart_KeyboardPress{KeyboardPress: &game.KeyboardPressPart{
				ToolId: "kb-1",
				Key:    game.KeyboardKey_KEYBOARD_KEY_F2,
			}}},
			toolID: "kb-1",
		},
		{
			name: "MouseMoveAndClickPart (saolei cell operations)",
			part: &game.FlowPart{Kind: &game.FlowPart_MouseMoveAndClick{MouseMoveAndClick: &game.MouseMoveAndClickPart{
				ToolId: "wm-click-1",
				XPx:    40,
				YPx:    216,
				Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
				Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
			}}},
			toolID: "wm-click-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runReadLoopFilterAdmissionTest(t, tt.part, tt.toolID)
		})
	}
}

// runReadLoopFilterAdmissionTest verifies op passes the readLoop filter and
// reaches executeAgentOperation by asserting a FlowResultPart with toolID is
// sent back over the WS. The setup mirrors
// TestReadLoop_ExecutesOperationAndSurvivesWait but is intentionally minimal —
// this is a regression guard for filter admission.
func runReadLoopFilterAdmissionTest(t *testing.T, op *game.FlowPart, toolID string) {
	t.Helper()

	// given: a flowParts frame carrying the op, followed by a wait signal. The
	// wait does NOT terminate the continuous reader (FR-008); the server
	// captures client-sent frames.
	contentFrame := &game.TeamFrame{
		SessionId:  "filter-session",
		TemplateId: "saolei",
		FrameId:    "srv-content-1",
		Payload: &game.TeamFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{op}},
		},
	}
	waitFrame := &game.TeamFrame{
		SessionId:  "filter-session",
		TemplateId: "saolei",
		FrameId:    "srv-wait-1",
		Payload:    &game.TeamFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{}}}}}},
	}
	sentFrames := make(chan *game.TeamFrame, 4)
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		go func() {
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				var f game.TeamFrame
				if err := proto.Unmarshal(data, &f); err == nil {
					select {
					case sentFrames <- &f:
					default:
					}
				}
			}
		}()
		for _, f := range []*game.TeamFrame{contentFrame, waitFrame} {
			data, _ := proto.Marshal(f)
			if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
		select {}
	})
	defer srv.Close()

	// given: an App with a connected WSClient. No window is selected, so
	// executeAgentOperation fails fast — but the FlowResultPart must still be
	// produced and sent (proving the filter admitted op).
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	app.ws = &api.WSClient{}
	if err := app.ws.Connect(context.Background(), srv.URL, "saolei", "filter-session", "test-env"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// when: run the continuous reader (the wait signal does NOT terminate it —
	// FR-008)
	app.recvDone = make(chan struct{})
	go app.readLoop("filter-session")
	t.Cleanup(func() { app.CloseAgent() })

	// then: a FlowResultPart with toolID was sent over the WS as a flowParts
	// frame. If the filter dropped op, no result is sent (regression).
	var resultPart *game.FlowResultPart
	deadline := time.After(2 * time.Second)
	for resultPart == nil {
		select {
		case f := <-sentFrames:
			fp := f.GetFlowParts()
			if fp != nil && len(fp.GetParts()) > 0 {
				resultPart = fp.GetParts()[0].GetFlowResult()
			}
		case <-deadline:
			t.Fatalf("no flow-result frame sent over WS within 2s (filter may have dropped the Part)")
		}
	}
	if resultPart.GetToolId() != toolID {
		t.Errorf("tool_id = %q, want %q", resultPart.GetToolId(), toolID)
	}
	if resultPart.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Errorf("status = %v, want FAILED (no window selected — expected precondition failure, not filter rejection)",
			resultPart.GetStatus())
	}
}

// TestConnect_ReconnectHandoverWaitsForPriorReader verifies the reconnect
// handover (specs/041-realtime-init-push/contracts/realtime-channel-contract.md
// §3.1; specs/041-realtime-init-push/research.md D4): a second Connect closes
// the prior WS and waits on
// the prior recvDone before starting a new reader, so the old reader has
// exited and cannot close the new recvDone.
func TestConnect_ReconnectHandoverWaitsForPriorReader(t *testing.T) {
	// given: a mock WS server serving two connections — each answers the
	// probe; both stay open so their readers keep running until torn down
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		probeResp := &game.TeamFrame{
			SessionId:  "reconnect-session",
			TemplateId: "saolei",
			FrameId:    "probe-resp",
			Payload: &game.TeamFrame_FlowParts{
				FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
				}},
			},
		}
		data, _ := proto.Marshal(probeResp)
		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			return
		}
		// both connections stay open; the test tears them down via CloseAgent
		select {}
	})
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	// when: first Connect starts reader 1
	if _, err := app.Connect("saolei", "reconnect-session"); err != nil {
		t.Fatalf("first Connect() unexpected error: %v", err)
	}
	firstDone := app.recvDone
	t.Cleanup(func() { app.CloseAgent() })

	// then: reader 1 is running
	select {
	case <-firstDone:
		t.Fatal("reader 1 exited before reconnect; expected it running")
	default:
	}

	// when: second Connect — reconnect handover waits on reader 1, then
	// starts reader 2
	if _, err := app.Connect("saolei", "reconnect-session"); err != nil {
		t.Fatalf("second Connect() unexpected error: %v", err)
	}
	secondDone := app.recvDone

	// then: reader 1 has exited (handover waited on it) and reader 2 is a
	// fresh, running reader
	select {
	case <-firstDone:
	default:
		t.Fatal("reader 1 still running after reconnect handover; Connect must wait on the prior recvDone")
	}
	if secondDone == firstDone {
		t.Fatal("second Connect reused the old recvDone; it must start a new reader")
	}
	select {
	case <-secondDone:
		t.Fatal("reader 2 exited immediately; expected it running")
	default:
	}
}

// TestCloseAgent_CleanClose verifies FR-010 /
// specs/041-realtime-init-push/contracts/realtime-channel-contract.md §3.1
// Close: CloseAgent tears the socket down (unblocking the reader's
// RecvFrame), waits on recvDone, and clears a.ws.
func TestCloseAgent_CleanClose(t *testing.T) {
	// given: mock WS server answering the probe, then staying open
	srv := mockWSServer(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		probeResp := &game.TeamFrame{
			SessionId:  "close-session",
			TemplateId: "saolei",
			FrameId:    "probe-resp",
			Payload: &game.TeamFrame_FlowParts{
				FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
				}},
			},
		}
		data, _ := proto.Marshal(probeResp)
		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			return
		}
		select {} // keep the connection open until CloseAgent tears it down
	})
	defer srv.Close()

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.cfg = api.Config{GatewayURL: srv.URL}

	// given: a connected app with the continuous reader running
	if _, err := app.Connect("saolei", "close-session"); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}

	// when: CloseAgent tears the socket down
	if err := app.CloseAgent(); err != nil {
		t.Fatalf("CloseAgent() unexpected error: %v", err)
	}

	// then: the reader exited (recvDone closed) and a.ws was cleared
	select {
	case <-app.recvDone:
	default:
		t.Fatal("recvDone not closed after CloseAgent; reader still running")
	}
	if app.ws != nil {
		t.Fatal("expected a.ws to be nil after CloseAgent")
	}
}

// Test_executeAgentOperation_KeyboardPressPart_RoutesToKeyboardExecutor
// verifies the FR-004a routing: a KeyboardPressPart with KEY_F2 (saolei_init)
// reaches ExecuteKeyboardPress. On the Linux test host the Win32 stub returns
// "not supported", so the result must be FAILED with a message containing
// "keyboard press" — proving the keyboard path was taken rather than the
// mouse path.
func Test_executeAgentOperation_KeyboardPressPart_RoutesToKeyboardExecutor(t *testing.T) {
	// given: App with a selected window (Linux stubs make every executor fail,
	// but the resolved handle lets executeAgentOperation proceed to the action
	// phase rather than short-circuiting at "no window selected").
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	op := &game.FlowPart{
		Kind: &game.FlowPart_KeyboardPress{
			KeyboardPress: &game.KeyboardPressPart{
				ToolId: "kb-f2",
				Key:    game.KeyboardKey_KEYBOARD_KEY_F2,
			},
		},
	}

	// when
	result := app.executeAgentOperation(op)

	// then: FAILED with a "keyboard press" routing error, and no mouse-path
	// artifact (no "capture window bounds" — the keyboard path does not
	// capture window bounds).
	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if !strings.Contains(result.GetMessage(), "keyboard press") {
		t.Errorf("expected message to mention 'keyboard press' (routing proof), got %q", result.GetMessage())
	}
	if strings.Contains(result.GetMessage(), "capture window bounds") {
		t.Errorf("keyboard path must not capture window bounds, but message mentions it: %q", result.GetMessage())
	}
	if result.GetToolId() != "kb-f2" {
		t.Errorf("expected tool_id %q, got %q", "kb-f2", result.GetToolId())
	}
}

// Test_executeAgentOperation_MouseMoveAndClickPart_WindowMessageRoutes
// verifies FR-004d routing: a MouseMoveAndClickPart with WINDOW_MESSAGE
// method reaches the window-message PostMessage path (no OS cursor movement,
// no screen-coordinate conversion). On the Linux stub the executor returns
// "not supported"; the message must mention "window-message" and must NOT
// mention "capture window bounds" (that step only runs for SIMULATED).
func Test_executeAgentOperation_MouseMoveAndClickPart_WindowMessageRoutes(t *testing.T) {
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	op := &game.FlowPart{
		Kind: &game.FlowPart_MouseMoveAndClick{
			MouseMoveAndClick: &game.MouseMoveAndClickPart{
				ToolId: "wm-click",
				XPx:    40,
				YPx:    216,
				Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
				Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
			},
		},
	}

	result := app.executeAgentOperation(op)

	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if !strings.Contains(result.GetMessage(), "window-message") {
		t.Errorf("expected message to mention 'window-message' (routing proof), got %q", result.GetMessage())
	}
	if strings.Contains(result.GetMessage(), "capture window bounds") {
		t.Errorf("WINDOW_MESSAGE path must not capture window bounds, but message mentions it: %q", result.GetMessage())
	}
	if result.GetToolId() != "wm-click" {
		t.Errorf("expected tool_id %q, got %q", "wm-click", result.GetToolId())
	}
}

// Test_executeAgentOperation_MouseMoveAndClickPart_SimulatedRoutes verifies
// FR-004c routing: a MouseMoveAndClickPart with SIMULATED method (and the
// implicit UNSPECIFIED→SIMULATED fallback) reaches the existing
// SetCursorPos+SendInput path. The Linux CaptureWindowBounds stub fails
// first, so the message must mention "capture window bounds" — proving the
// SIMULATED path was taken rather than the WINDOW_MESSAGE path.
func Test_executeAgentOperation_MouseMoveAndClickPart_SimulatedRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method game.MouseInputMethod
	}{
		{
			name:   "explicit SIMULATED",
			method: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED,
		},
		{
			// FR-004c: UNSPECIFIED must be treated as SIMULATED so legacy
			// callers (who omit the field) keep the prior behavior.
			name:   "UNSPECIFIED collapses to SIMULATED",
			method: game.MouseInputMethod_MOUSE_INPUT_METHOD_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := applog.NewLogger()
			app := NewApp(logger)
			app.SetContext(context.Background())
			selectWindowForTest(t, app, capture.WindowRef{
				Handle:      1,
				Title:       "stub-window",
				ScaleFactor: 1.0,
			})

			op := &game.FlowPart{
				Kind: &game.FlowPart_MouseMoveAndClick{
					MouseMoveAndClick: &game.MouseMoveAndClickPart{
						ToolId: "sim-click",
						XPx:    400,
						YPx:    300,
						Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
						Method: tt.method,
					},
				},
			}

			result := app.executeAgentOperation(op)

			if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
				t.Fatalf("expected FAILED status, got %s", got)
			}
			if !strings.Contains(result.GetMessage(), "capture window bounds") {
				t.Errorf("expected SIMULATED path to capture window bounds (routing proof), got %q", result.GetMessage())
			}
			if strings.Contains(result.GetMessage(), "window-message") {
				t.Errorf("SIMULATED path must not invoke window-message executor, but message mentions it: %q", result.GetMessage())
			}
		})
	}
}

// Test_executeAgentOperation_MouseMovePart_WindowMessageRoutes verifies the
// MouseMovePart WINDOW_MESSAGE branch: it reaches the WM_MOUSEMOVE stub
// (returns "window-message move") without capturing window bounds.
func Test_executeAgentOperation_MouseMovePart_WindowMessageRoutes(t *testing.T) {
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	op := &game.FlowPart{
		Kind: &game.FlowPart_MouseMove{
			MouseMove: &game.MouseMovePart{
				ToolId: "wm-move",
				XPx:    100,
				YPx:    100,
				Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
			},
		},
	}

	result := app.executeAgentOperation(op)

	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if !strings.Contains(result.GetMessage(), "window-message") {
		t.Errorf("expected message to mention 'window-message' (routing proof), got %q", result.GetMessage())
	}
	if strings.Contains(result.GetMessage(), "capture window bounds") {
		t.Errorf("WINDOW_MESSAGE path must not capture window bounds, but message mentions it: %q", result.GetMessage())
	}
}

// Test_executeAgentOperation_MouseClickPart_WindowMessageRejected verifies
// the protocol-level guard: a standalone MouseClickPart with WINDOW_MESSAGE
// method is rejected because MouseClickPart carries no coordinates to pack
// into lParam. The model expresses window-message clicks via
// MouseMoveAndClickPart (FR-004b), never via standalone MouseClickPart.
func Test_executeAgentOperation_MouseClickPart_WindowMessageRejected(t *testing.T) {
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	op := &game.FlowPart{
		Kind: &game.FlowPart_MouseClick{
			MouseClick: &game.MouseClickPart{
				ToolId: "wm-click-bad",
				Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
				Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
			},
		},
	}

	result := app.executeAgentOperation(op)

	if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %s", got)
	}
	if !strings.Contains(result.GetMessage(), "WINDOW_MESSAGE method is not supported") {
		t.Errorf("expected rejection of MouseClickPart+WINDOW_MESSAGE, got %q", result.GetMessage())
	}
	if strings.Contains(result.GetMessage(), "click action") {
		t.Errorf("must not invoke click executor (rejection is synchronous), but message mentions 'click action': %q", result.GetMessage())
	}
}

// Test_executeAgentOperation_MouseClickPart_SimulatedUnchangedBehavior
// verifies FR-004c's backward-compat guarantee: an explicit MouseClickPart
// with SIMULATED method (and the implicit UNSPECIFIED→SIMULATED fallback)
// must keep taking the existing click path. On Linux the click executor
// returns "not supported"; the message must mention "click action" (the
// existing SIMULATED path's error wrapping) and must NOT mention
// "WINDOW_MESSAGE method is not supported" (the rejection message).
func Test_executeAgentOperation_MouseClickPart_SimulatedUnchangedBehavior(t *testing.T) {
	tests := []struct {
		name   string
		method game.MouseInputMethod
	}{
		{name: "explicit SIMULATED", method: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED},
		{name: "UNSPECIFIED collapses to SIMULATED", method: game.MouseInputMethod_MOUSE_INPUT_METHOD_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := applog.NewLogger()
			app := NewApp(logger)
			app.SetContext(context.Background())
			selectWindowForTest(t, app, capture.WindowRef{
				Handle:      1,
				Title:       "stub-window",
				ScaleFactor: 1.0,
			})

			op := &game.FlowPart{
				Kind: &game.FlowPart_MouseClick{
					MouseClick: &game.MouseClickPart{
						ToolId: "sim-click-only",
						Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
						Method: tt.method,
					},
				},
			}

			result := app.executeAgentOperation(op)

			if got := result.GetStatus(); got != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
				t.Fatalf("expected FAILED status, got %s", got)
			}
			if !strings.Contains(result.GetMessage(), "click action") {
				t.Errorf("SIMULATED path must invoke click executor (existing behavior), got %q", result.GetMessage())
			}
			if strings.Contains(result.GetMessage(), "WINDOW_MESSAGE method is not supported") {
				t.Errorf("SIMULATED path must not hit WINDOW_MESSAGE rejection, got %q", result.GetMessage())
			}
		})
	}
}

// --- Debug hold tests (T010/T011) ---
//
// These tests cover the debug-mode tool-result hold control plane
// (specs/022-desktop-debug-mode contracts/debug-control-plane.md §1.1/§1.2/§2,
// data-model.md). They never depend on the real Wails runtime: emitDebugEvent
// is overridden to a no-op or recorder because runtime.EventsEmit calls
// log.Fatalf when the context lacks the Wails "events" value.

// noopEmit replaces emitDebugEvent with a no-op for the duration of a test.
// It returns a cleanup func that restores the original.
func noopEmit(t *testing.T) {
	t.Helper()
	orig := emitDebugEvent
	emitDebugEvent = func(context.Context, string, ...interface{}) {}
	t.Cleanup(func() { emitDebugEvent = orig })
}

// recorderEmit replaces emitDebugEvent with a recorder that appends each event
// name to a slice under a mutex. It returns the slice (shared) and a cleanup.
func recorderEmit(t *testing.T) (*[]string, *sync.Mutex) {
	t.Helper()
	orig := emitDebugEvent
	var mu sync.Mutex
	events := []string{}
	emitDebugEvent = func(_ context.Context, name string, _ ...interface{}) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}
	t.Cleanup(func() { emitDebugEvent = orig })
	return &events, &mu
}

// emitRecord is a single recorded event: name + payload (the variadic argument
// the production code passes to runtime.EventsEmit).
type emitRecord struct {
	name    string
	payload map[string]any
}

// recorderEmitPayload replaces emitDebugEvent with a recorder that captures
// the event name and its payload map. Used by tests that assert on the
// EXTENDED result-held payload (specs/023-saolei-mcp-refine/contracts/
// debug-drawer-contract.md §2). Returns the slice (shared) and a cleanup.
func recorderEmitPayload(t *testing.T) (*[]emitRecord, *sync.Mutex) {
	t.Helper()
	orig := emitDebugEvent
	var mu sync.Mutex
	var recorded []emitRecord
	emitDebugEvent = func(_ context.Context, name string, args ...interface{}) {
		var payload map[string]any
		if len(args) > 0 {
			if m, ok := args[0].(map[string]any); ok {
				payload = m
			}
		}
		mu.Lock()
		recorded = append(recorded, emitRecord{name: name, payload: payload})
		mu.Unlock()
	}
	t.Cleanup(func() { emitDebugEvent = orig })
	return &recorded, &mu
}

// waitForHold polls the holds map (up to ~1s) until toolID appears, then
// returns true. Used to synchronize a confirm/cancel against a holdAndRelease
// call running in another goroutine.
func waitForHold(t *testing.T, app *App, toolID string) bool {
	t.Helper()
	for i := 0; i < 100; i++ {
		app.holdsMu.Lock()
		_, ok := app.holds[toolID]
		app.holdsMu.Unlock()
		if ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestConfirmToolResult_ReleasesRegisteredHold verifies that ConfirmToolResult
// closes a manually-registered hold's confirmCh and removes it from the map
// (contracts/debug-control-plane.md §1.2).
func TestConfirmToolResult_ReleasesRegisteredHold(t *testing.T) {
	// given: an app with one manually-registered hold
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	app.holdsMu.Lock()
	h := &hold{toolID: "tool-release", confirmCh: make(chan struct{})}
	app.holds = map[string]*hold{"tool-release": h}
	app.holdsMu.Unlock()

	// when: ConfirmToolResult is called for the held toolID
	err := app.ConfirmToolResult("tool-release")

	// then: no error, confirmCh is closed, hold removed from map
	if err != nil {
		t.Fatalf("ConfirmToolResult() unexpected error: %v", err)
	}
	select {
	case <-h.confirmCh:
		// expected: channel closed
	default:
		t.Fatal("expected confirmCh to be closed")
	}
	app.holdsMu.Lock()
	_, stillHeld := app.holds["tool-release"]
	app.holdsMu.Unlock()
	if stillHeld {
		t.Fatal("expected hold to be removed from map after confirm")
	}
}

// TestConfirmToolResult_UnknownToolIDIsNoOp verifies that ConfirmToolResult on
// a toolID that is not held is a logged no-op returning nil (contracts §1.2 —
// "the result may already have been released").
func TestConfirmToolResult_UnknownToolIDIsNoOp(t *testing.T) {
	// given: an app with no holds
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when: ConfirmToolResult on an unknown toolID
	err := app.ConfirmToolResult("unknown-tool")

	// then: returns nil, no panic
	if err != nil {
		t.Fatalf("ConfirmToolResult() on unknown toolID should return nil, got: %v", err)
	}
}

// TestSetDebugMode_DisabledDrainsAllHolds verifies that disabling debug mode
// releases every currently-held result (reason "debug-off") so no turn is left
// blocked (contracts §1.1, spec Edge Case "Debug toggled OFF mid-hold").
func TestSetDebugMode_DisabledDrainsAllHolds(t *testing.T) {
	// given: an app with debug ON and three registered holds
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.debugEnabled.Store(true)

	holds := make([]*hold, 0, 3)
	app.holdsMu.Lock()
	app.holds = map[string]*hold{}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("tool-%d", i)
		h := &hold{toolID: id, confirmCh: make(chan struct{})}
		app.holds[id] = h
		holds = append(holds, h)
	}
	app.holdsMu.Unlock()

	// when: debug mode is disabled
	err := app.SetDebugMode(false)

	// then: no error, every confirmCh closed with reason "debug-off", map empty
	if err != nil {
		t.Fatalf("SetDebugMode(false) unexpected error: %v", err)
	}
	for _, h := range holds {
		select {
		case <-h.confirmCh:
		default:
			t.Fatalf("expected confirmCh for %q to be closed", h.toolID)
		}
		if h.releaseReason != "debug-off" {
			t.Fatalf("expected releaseReason %q for %q, got %q", "debug-off", h.toolID, h.releaseReason)
		}
	}
	app.holdsMu.Lock()
	mapLen := len(app.holds)
	app.holdsMu.Unlock()
	if mapLen != 0 {
		t.Fatalf("expected holds map to be empty after SetDebugMode(false), got %d entries", mapLen)
	}
}

// Test_holdAndRelease_ConfirmedBranch verifies the confirm select arm: a hold
// registered by holdAndRelease is released by ConfirmToolResult and the reason
// returned is "confirmed" (data-model.md state transition).
func Test_holdAndRelease_ConfirmedBranch(t *testing.T) {
	// given: debug ON, emitDebugEvent overridden to no-op
	noopEmit(t)
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.debugEnabled.Store(true)

	toolID := "tool-confirm"
	reasonCh := make(chan string, 1)
	// holdAndRelease now takes the raw FlowPart and builds the descriptor
	// internally via describeFlowPart. A keyboard_press F2 yields the
	// "按键 F2" summary the drawer renders.
	part := &game.FlowPart{Kind: &game.FlowPart_KeyboardPress{
		KeyboardPress: &game.KeyboardPressPart{Key: game.KeyboardKey_KEYBOARD_KEY_F2},
	}}

	// when: holdAndRelease runs in a goroutine, then ConfirmToolResult fires
	go func() {
		reasonCh <- app.holdAndRelease(toolID, part)
	}()
	if !waitForHold(t, app, toolID) {
		t.Fatal("hold was not registered before timeout")
	}
	if err := app.ConfirmToolResult(toolID); err != nil {
		t.Fatalf("ConfirmToolResult() unexpected error: %v", err)
	}
	reason := <-reasonCh

	// then: reason is "confirmed"
	if reason != "confirmed" {
		t.Fatalf("expected reason %q, got %q", "confirmed", reason)
	}
	app.holdsMu.Lock()
	mapLen := len(app.holds)
	app.holdsMu.Unlock()
	if mapLen != 0 {
		t.Fatalf("expected holds map empty after release, got %d", mapLen)
	}
}

// Test_holdAndRelease_TimeoutBranch verifies the 15-min auto-continue arm: with
// debugHoldTimeout overridden to a short duration, the reason is "timeout"
// (spec FR-013).
func Test_holdAndRelease_TimeoutBranch(t *testing.T) {
	// given: debug ON, timeout overridden to 10ms, emit no-op
	noopEmit(t)
	origTimeout := debugHoldTimeout
	debugHoldTimeout = 10 * time.Millisecond
	t.Cleanup(func() { debugHoldTimeout = origTimeout })

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.debugEnabled.Store(true)

	// holdAndRelease builds the descriptor from this FlowPart internally;
	// the resulting summary is "移动并点击 (136, 344) · 左键 · 窗口消息".
	part := &game.FlowPart{Kind: &game.FlowPart_MouseMoveAndClick{
		MouseMoveAndClick: &game.MouseMoveAndClickPart{
			XPx:    136,
			YPx:    344,
			Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
		},
	}}

	// when: holdAndRelease runs with no confirmation
	reason := app.holdAndRelease("tool-timeout", part)

	// then: reason is "timeout", hold removed from map
	if reason != "timeout" {
		t.Fatalf("expected reason %q, got %q", "timeout", reason)
	}
	app.holdsMu.Lock()
	_, stillHeld := app.holds["tool-timeout"]
	app.holdsMu.Unlock()
	if stillHeld {
		t.Fatal("expected hold to be removed from map after timeout")
	}
}

// Test_holdAndRelease_ShutdownBranch verifies the shutdown arm: cancelling the
// app context releases the hold with reason "shutdown" (spec Edge Case
// "leaving the session with a held result").
func Test_holdAndRelease_ShutdownBranch(t *testing.T) {
	// given: debug ON, cancelable ctx, emit no-op
	noopEmit(t)
	logger := applog.NewLogger()
	app := NewApp(logger)
	ctx, cancel := context.WithCancel(context.Background())
	app.SetContext(ctx)
	app.debugEnabled.Store(true)

	toolID := "tool-shutdown"
	reasonCh := make(chan string, 1)
	// holdAndRelease builds the descriptor from this FlowPart internally;
	// the resulting summary is "左键点击 · 模拟".
	part := &game.FlowPart{Kind: &game.FlowPart_MouseClick{
		MouseClick: &game.MouseClickPart{
			Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED,
		},
	}}

	// when: holdAndRelease runs, then the context is cancelled
	go func() {
		reasonCh <- app.holdAndRelease(toolID, part)
	}()
	if !waitForHold(t, app, toolID) {
		t.Fatal("hold was not registered before timeout")
	}
	cancel()
	reason := <-reasonCh

	// then: reason is "shutdown"
	if reason != "shutdown" {
		t.Fatalf("expected reason %q, got %q", "shutdown", reason)
	}
}

// Test_holdAndRelease_EmitsHeldAndReleasedEvents verifies that the result-held
// event is emitted on entry and result-released on exit (contracts §2.1/§2.2).
func Test_holdAndRelease_EmitsHeldAndReleasedEvents(t *testing.T) {
	// given: debug ON, emit records event names, timeout short
	events, mu := recorderEmit(t)
	origTimeout := debugHoldTimeout
	debugHoldTimeout = 10 * time.Millisecond
	t.Cleanup(func() { debugHoldTimeout = origTimeout })

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.debugEnabled.Store(true)

	// holdAndRelease builds the descriptor from this FlowPart internally.
	part := &game.FlowPart{Kind: &game.FlowPart_KeyboardPress{
		KeyboardPress: &game.KeyboardPressPart{Key: game.KeyboardKey_KEYBOARD_KEY_F2},
	}}

	// when: holdAndRelease runs and auto-continues (timeout)
	app.holdAndRelease("tool-events", part)

	// then: exactly result-held then result-released were emitted, in order
	mu.Lock()
	defer mu.Unlock()
	if len(*events) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(*events), *events)
	}
	if got := (*events)[0]; got != "game:debug:result-held" {
		t.Fatalf("expected first event %q, got %q", "game:debug:result-held", got)
	}
	if got := (*events)[1]; got != "game:debug:result-released" {
		t.Fatalf("expected second event %q, got %q", "game:debug:result-released", got)
	}
}

// Test_holdAndRelease_HeldPayloadCarriesOperation verifies the EXTENDED
// result-held payload (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md
// §2): alongside toolId, the event carries an `operation` object with
// kind/summary/details so the session-top drawer can render the request
// content without proto knowledge. result-released is unchanged ({toolId,
// reason}).
func Test_holdAndRelease_HeldPayloadCarriesOperation(t *testing.T) {
	// given: emit records full payloads, timeout short
	recorded, mu := recorderEmitPayload(t)
	origTimeout := debugHoldTimeout
	debugHoldTimeout = 10 * time.Millisecond
	t.Cleanup(func() { debugHoldTimeout = origTimeout })

	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	app.debugEnabled.Store(true)

	// holdAndRelease builds the descriptor from this FlowPart internally
	// via describeFlowPart; the resulting emit payload's operation fields
	// are asserted below.
	part := &game.FlowPart{Kind: &game.FlowPart_MouseMoveAndClick{
		MouseMoveAndClick: &game.MouseMoveAndClickPart{
			XPx:    136,
			YPx:    344,
			Click:  game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
		},
	}}

	// when: holdAndRelease runs (auto-continues via timeout)
	app.holdAndRelease("tool-payload", part)

	// then: the held payload carries toolId + operation{kind,summary,details};
	// the released payload carries toolId + reason (unchanged).
	mu.Lock()
	defer mu.Unlock()
	if len(*recorded) != 2 {
		t.Fatalf("expected 2 events, got %d", len(*recorded))
	}
	held := (*recorded)[0]
	if held.name != "game:debug:result-held" {
		t.Fatalf("expected first event %q, got %q", "game:debug:result-held", held.name)
	}
	if got, ok := held.payload["toolId"].(string); !ok || got != "tool-payload" {
		t.Fatalf("expected held payload toolId %q, got %#v", "tool-payload", held.payload["toolId"])
	}
	opMap, ok := held.payload["operation"].(map[string]any)
	if !ok {
		t.Fatalf("expected held payload to carry operation map, got %#v", held.payload["operation"])
	}
	if opMap["kind"] != "mouse_move_and_click" {
		t.Fatalf("expected operation.kind %q, got %#v", "mouse_move_and_click", opMap["kind"])
	}
	if opMap["summary"] != "移动并点击 (136, 344) · 左键 · 窗口消息" {
		t.Fatalf("expected operation.summary, got %#v", opMap["summary"])
	}
	details, ok := opMap["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected operation.details map, got %#v", opMap["details"])
	}
	if details["click"] != "MOUSE_CLICK_ACTION_LEFT_CLICK" {
		t.Fatalf("expected details.click, got %#v", details["click"])
	}
	if details["method"] != "MOUSE_INPUT_METHOD_WINDOW_MESSAGE" {
		t.Fatalf("expected details.method, got %#v", details["method"])
	}

	released := (*recorded)[1]
	if released.name != "game:debug:result-released" {
		t.Fatalf("expected second event %q, got %q", "game:debug:result-released", released.name)
	}
	if got, ok := released.payload["toolId"].(string); !ok || got != "tool-payload" {
		t.Fatalf("expected released payload toolId %q, got %#v", "tool-payload", released.payload["toolId"])
	}
	if _, hasOp := released.payload["operation"]; hasOp {
		t.Fatalf("released payload MUST NOT carry operation (unchanged from 022), got %#v", released.payload)
	}
	if released.payload["reason"] != "timeout" {
		t.Fatalf("expected released reason %q, got %#v", "timeout", released.payload["reason"])
	}
}

// Test_describeFlowPart verifies the operation descriptor builder for each
// FlowPart variant (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md
// §2). The descriptor feeds the session-top drawer payload so the frontend
// can render a human-readable operation request without proto knowledge.
func Test_describeFlowPart(t *testing.T) {
	tests := []struct {
		name        string
		part        *game.FlowPart
		wantKind    string
		wantSummary string
		wantDetails map[string]any
	}{
		{
			name:        "mouse_move with WINDOW_MESSAGE",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseMove{MouseMove: &game.MouseMovePart{XPx: 100, YPx: 200, Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE}}},
			wantKind:    "mouse_move",
			wantSummary: "移动光标 (100, 200) · 窗口消息",
			wantDetails: map[string]any{
				"xPx":    int32(100),
				"yPx":    int32(200),
				"method": "MOUSE_INPUT_METHOD_WINDOW_MESSAGE",
			},
		},
		{
			name:        "mouse_click LEFT_CLICK SIMULATED",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{Click: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED}}},
			wantKind:    "mouse_click",
			wantSummary: "左键点击 · 模拟",
			wantDetails: map[string]any{
				"click":  "MOUSE_CLICK_ACTION_LEFT_CLICK",
				"method": "MOUSE_INPUT_METHOD_SIMULATED",
			},
		},
		{
			name:        "mouse_click RIGHT_CLICK",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{Click: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK, Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE}}},
			wantKind:    "mouse_click",
			wantSummary: "右键点击 · 窗口消息",
			wantDetails: map[string]any{
				"click":  "MOUSE_CLICK_ACTION_RIGHT_CLICK",
				"method": "MOUSE_INPUT_METHOD_WINDOW_MESSAGE",
			},
		},
		{
			name:        "mouse_click LEFT_RIGHT_PRESS",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseClick{MouseClick: &game.MouseClickPart{Click: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS, Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE}}},
			wantKind:    "mouse_click",
			wantSummary: "左右键同按点击 · 窗口消息",
			wantDetails: map[string]any{
				"click":  "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS",
				"method": "MOUSE_INPUT_METHOD_WINDOW_MESSAGE",
			},
		},
		{
			name:        "keyboard_press F2",
			part:        &game.FlowPart{Kind: &game.FlowPart_KeyboardPress{KeyboardPress: &game.KeyboardPressPart{Key: game.KeyboardKey_KEYBOARD_KEY_F2}}},
			wantKind:    "keyboard_press",
			wantSummary: "按键 F2",
			wantDetails: map[string]any{
				"key": "KEYBOARD_KEY_F2",
			},
		},
		{
			name:        "mouse_move_and_click at saolei cell (3,4) with LEFT_CLICK WINDOW_MESSAGE",
			part:        &game.FlowPart{Kind: &game.FlowPart_MouseMoveAndClick{MouseMoveAndClick: &game.MouseMoveAndClickPart{XPx: 136, YPx: 344, Click: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE}}},
			wantKind:    "mouse_move_and_click",
			wantSummary: "移动并点击 (136, 344) · 左键 · 窗口消息",
			wantDetails: map[string]any{
				"xPx":    int32(136),
				"yPx":    int32(344),
				"click":  "MOUSE_CLICK_ACTION_LEFT_CLICK",
				"method": "MOUSE_INPUT_METHOD_WINDOW_MESSAGE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := describeFlowPart(tt.part)
			if op == nil {
				t.Fatalf("describeFlowPart returned nil for %s", tt.name)
			}
			if op.kind != tt.wantKind {
				t.Fatalf("kind: want %q, got %q", tt.wantKind, op.kind)
			}
			if op.summary != tt.wantSummary {
				t.Fatalf("summary: want %q, got %q", tt.wantSummary, op.summary)
			}
			for k, want := range tt.wantDetails {
				got, has := op.details[k]
				if !has {
					t.Fatalf("details missing key %q in %s", k, tt.name)
				}
				if got != want {
					t.Fatalf("details[%q]: want %#v, got %#v", k, want, got)
				}
			}
		})
	}

	// Non-operation FlowPart (signal kind): describe always returns non-nil —
	// it falls back to a default "unknown" descriptor so callers never need a
	// nil check.
	{
		got := describeFlowPart(&game.FlowPart{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{}}})
		if got == nil {
			t.Fatal("describeFlowPart returned nil for a non-operation FlowPart; expected default unknown descriptor")
		}
		if got.kind != "unknown" {
			t.Fatalf("non-operation kind: want %q, got %q", "unknown", got.kind)
		}
		if got.summary != "未知操作" {
			t.Fatalf("non-operation summary: want %q, got %q", "未知操作", got.summary)
		}
		if got.details == nil {
			t.Fatal("non-operation details: want non-nil empty map, got nil")
		}
		if len(got.details) != 0 {
			t.Fatalf("non-operation details: want empty map, got %#v", got.details)
		}
	}
}

// --- Selected-window tests (spec 025 US1 — single source of truth) ---
//
// The selected window replaces the removed App.boundWin
// (specs/025-desktop-image-state-refine/contracts/window-select-contract.md).
// These tests cover the resolve path (no selection ⇒ graceful failure,
// re-select retargets) and the setter, which are the behaviors US1 adds; the
// operation routing tests above (which now call selectWindowForTest) cover the
// select-then-op path.

// selectWindowForTest sets the selected window handle on app and injects a
// mock window list (overriding the package-level listWindows) so
// resolveSelectedWindow returns win for the handle. The real capture.ListWindows
// returns "not supported" on the Linux test host, so operations/screenshots
// that need a resolved window require this injection. Cleanup restores the
// original listWindows. See contracts/window-select-contract.md §2.3.
func selectWindowForTest(t *testing.T, app *App, win capture.WindowRef) {
	t.Helper()
	orig := listWindows
	listWindows = func(context.Context) ([]capture.WindowRef, error) {
		return []capture.WindowRef{win}, nil
	}
	t.Cleanup(func() { listWindows = orig })
	app.SetSelectedWindow(win.Handle)
}

// TestSetSelectedWindow verifies the Wails-bound setter stores the handle on
// the App (spec 025 FR-006: selected window is set on dropdown selection).
func TestSetSelectedWindow(t *testing.T) {
	// given: a fresh App
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when: SetSelectedWindow stores a handle
	app.SetSelectedWindow(42)

	// then: the field holds the value under its mutex
	app.selectedMu.Lock()
	got := app.selectedWin
	app.selectedMu.Unlock()
	if got != 42 {
		t.Errorf("expected selectedWin=42, got %d", got)
	}
}

// TestGetSelectedWindow verifies the Wails-bound getter returns the stored
// handle (and 0 when no window is selected), so the frontend can restore the
// dropdown selection on session re-entry (spec 025 FR-001/FR-006).
func TestGetSelectedWindow(t *testing.T) {
	// given: a fresh App with no selected window
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when: nothing selected
	got, err := app.GetSelectedWindow()
	if err != nil {
		t.Fatalf("GetSelectedWindow() unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("expected selectedWin=0 (none selected), got %d", got)
	}

	// when: a window was selected
	app.SetSelectedWindow(1234)
	got, err = app.GetSelectedWindow()
	if err != nil {
		t.Fatalf("GetSelectedWindow() unexpected error: %v", err)
	}
	if got != 1234 {
		t.Errorf("expected selectedWin=1234, got %d", got)
	}
}

// Test_resolveSelectedWindow_NoSelection verifies FR-005: with no window
// selected, resolveSelectedWindow fails gracefully with "no window selected".
func Test_resolveSelectedWindow_NoSelection(t *testing.T) {
	// given: App with no selected window (selectedWin zero value)
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when
	_, err := app.resolveSelectedWindow()

	// then: graceful failure, no crash
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no window selected") {
		t.Errorf("expected 'no window selected', got %q", err.Error())
	}
}

// Test_resolveSelectedWindow_WindowNotFound verifies the FR-005 edge case:
// when the selected handle is no longer in the window list (the window closed
// between selection and use), resolve fails gracefully. The user can recover
// by selecting another window.
func Test_resolveSelectedWindow_WindowNotFound(t *testing.T) {
	// given: an empty injected window list and a selected handle that is not
	// in it.
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	orig := listWindows
	listWindows = func(context.Context) ([]capture.WindowRef, error) {
		return nil, nil
	}
	t.Cleanup(func() { listWindows = orig })
	app.SetSelectedWindow(999)

	// when
	_, err := app.resolveSelectedWindow()

	// then: graceful failure mentioning "not found"
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found', got %q", err.Error())
	}
}

// Test_resolveSelectedWindow_ReselectRetargets verifies FR-004: re-selecting a
// different window makes the next resolve return the new selection. The
// resolver reads the current handle on every call, so a mid-session re-select
// retargets subsequent operations/screenshots with no follow-up action.
func Test_resolveSelectedWindow_ReselectRetargets(t *testing.T) {
	// given: an injected list with two windows so either handle resolves
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	orig := listWindows
	listWindows = func(context.Context) ([]capture.WindowRef, error) {
		return []capture.WindowRef{
			{Handle: 100, Title: "window-A", ScaleFactor: 1.0},
			{Handle: 200, Title: "window-B", ScaleFactor: 2.0},
		}, nil
	}
	t.Cleanup(func() { listWindows = orig })

	// when: select window A; resolve
	app.SetSelectedWindow(100)
	win, err := app.resolveSelectedWindow()
	if err != nil {
		t.Fatalf("first resolve: unexpected error: %v", err)
	}
	// then: resolve returns window A
	if win.Handle != 100 || win.Title != "window-A" || win.ScaleFactor != 1.0 {
		t.Errorf("first resolve: got handle=%d title=%q scale=%v, want 100/window-A/1.0",
			win.Handle, win.Title, win.ScaleFactor)
	}

	// when: re-select window B (retarget); resolve
	app.SetSelectedWindow(200)
	win, err = app.resolveSelectedWindow()
	if err != nil {
		t.Fatalf("second resolve: unexpected error: %v", err)
	}
	// then: resolve returns window B — the new selection is now the target
	if win.Handle != 200 || win.Title != "window-B" || win.ScaleFactor != 2.0 {
		t.Errorf("second resolve: got handle=%d title=%q scale=%v, want 200/window-B/2.0",
			win.Handle, win.Title, win.ScaleFactor)
	}
}

// TestCaptureScreenshot_NoSelection verifies FR-005: CaptureScreenshot with no
// window selected fails gracefully (no crash, no silent no-op).
func TestCaptureScreenshot_NoSelection(t *testing.T) {
	// given: App with no selected window
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())

	// when
	_, err := app.CaptureScreenshot()

	// then: graceful failure
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no window selected") {
		t.Errorf("expected 'no window selected', got %q", err.Error())
	}
}

// TestCaptureScreenshot_SelectionButCaptureFails verifies that with a selected
// window the capture path is reached. On the Linux test host CaptureWindow
// returns "not supported" — the error proves resolve succeeded (no
// "no window selected" precondition failure) and capture was attempted.
func TestCaptureScreenshot_SelectionButCaptureFails(t *testing.T) {
	// given: App with a selected window
	logger := applog.NewLogger()
	app := NewApp(logger)
	app.SetContext(context.Background())
	selectWindowForTest(t, app, capture.WindowRef{
		Handle:      1,
		Title:       "stub-window",
		ScaleFactor: 1.0,
	})

	// when
	_, err := app.CaptureScreenshot()

	// then: the failure is from the capture stub, NOT from the precondition
	if err == nil {
		t.Fatal("expected error from stub capture, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected 'not supported' from Linux stub (resolve succeeded), got %q", err.Error())
	}
}
