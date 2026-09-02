// Package testplan contains the desktop-flow large tests: the /api/v2 flow
// control stream from the desktop's side, with the test acting as the
// desktop client wherever the deployed fake-desktop executors cannot be
// observed directly (specs/051-agent-v2-dsh-migration/contracts/
// desktop-bridge.md §6 验收锚点; quickstart.md §2 desktop-flow row). Cases
// cover the connection probe, the take-over of a second connection, the
// operation delivery → receipt → recognition loop, and the in-flight
// failure when the desktop vanishes — one test per concern
// (style/large_test.md §测试组织).
package testplan

import (
	"errors"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	gamev2 "dominion/projects/game/v2"

	"github.com/gorilla/websocket"
)

// containsAll reports whether s contains every listed substring (the
// board-text assertions each check several contract lines at once).
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestDesktopFlowProbeEcho covers the connect probe (desktop-bridge.md §1
// 探测行): the first UserFrame carrying the ACTIVE status signal is
// answered with a status echo frame — the application-layer confirmation
// the real desktop's 10s Connect window waits for.
func TestDesktopFlowProbeEcho(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-probe-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)

	conn, echo := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	if len(echo.GetFlowParts().GetParts()) != 1 {
		t.Fatalf("probe reply parts = %d, want 1 status signal", len(echo.GetFlowParts().GetParts()))
	}
	signal := echo.GetFlowParts().GetParts()[0].GetStatus()
	if signal == nil {
		t.Fatalf("probe reply part = %T, want a StatusSignal", echo.GetFlowParts().GetParts()[0].GetKind())
	}
	if signal.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE {
		t.Errorf("probe reply status = %v, want the echoed ACTIVE signal", signal.GetStatus())
	}
}

// TestDesktopFlowSecondConnectionTakesOver covers the v1 take-over baseline
// (desktop-bridge.md §2: a new connection for the session ends the previous
// one): a second flow connection on the same session closes the first, and
// the second connection is the one that stays serviceable.
func TestDesktopFlowSecondConnectionTakesOver(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-takeover-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)

	first, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer first.Close()

	// The second connection attaches: it answers its probe and takes the
	// session over.
	second, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer second.Close()

	// The first connection was ended by the takeover: its next read fails
	// with a close instead of another echo. A late echo may have raced in
	// before the attach — drain until the read errors.
	deadline := time.Now().Add(wsReadTimeout)
	for {
		if _, err := readFlowTeamFrameNoFatal(first, 5*time.Second); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first flow connection was never closed by the second connection's takeover")
		}
	}
}

// TestDesktopFlowOperationDeliveryAndReceipt drives the delivery → receipt →
// recognition loop with the test as the desktop (desktop-bridge.md §6
// 验收锚点 1): materialize, send the game keyword, read the F2 new-game
// dispatch off the flow stream, answer it with the recognizable win-board
// screenshot, and observe the agent recognize it — the conversation stream
// carries the SUCCEEDED tool_result with the board text.
func TestDesktopFlowOperationDeliveryAndReceipt(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-receipt-" + uniqueSuffix()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		sessionID, "flow-receipt-"+uniqueSuffix(), "recognize the receipt")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()

	// The turn's tool events stream while the flow dispatch is served on
	// this goroutine — collect via the async drain.
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiGame+" through the flow connection")
	ch := drainAgentV2TurnAsync(stream)

	serveWonInitReceipt(t, flow, sessionID, wsReadTimeout)

	var events []*gamev2.ChatEvent
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("send stream: %v", r.err)
		}
		events = r.events
	case <-time.After(wsReadTimeout):
		t.Fatal("game turn did not complete after the flow receipt")
	}
	assertAgentV2TurnWellFormed(t, sessionName, events)

	results := collectAgentV2GameEvents(events)
	if len(results) == 0 {
		t.Fatal("the game turn produced no tool_result frames")
	}
	init := results[0]
	if init.GetStatus() != gamev2.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_init tool_result status = %v, want SUCCEEDED (the receipt screenshot is recognizable)", init.GetStatus())
	}
	if !containsAll(init.GetResult(), agentV2WonInitContains, agentV2WonStatusContains, agentV2WonBoardContains) {
		t.Errorf("saolei_init result = %q, want the recognized win board (%q / %q / %q)",
			init.GetResult(), agentV2WonInitContains, agentV2WonStatusContains, agentV2WonBoardContains)
	}
}

// TestDesktopFlowDisconnectFailsInFlight covers the vanish path: the test
// accepts the F2 dispatch and then closes the flow connection without
// replying — the bridge settles the in-flight dispatch FAILED "desktop
// disconnected", the tool result is a model-visible error, and the turn
// completes (desktop-bridge.md §2 断连结算, data-model.md §2.6).
func TestDesktopFlowDisconnectFailsInFlight(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-vanish-" + uniqueSuffix()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		sessionID, "flow-vanish-"+uniqueSuffix(), "vanish mid-dispatch")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiGame+" and then the desktop vanishes")
	ch := drainAgentV2TurnAsync(stream)

	// Accept the dispatch, then drop the connection instead of replying.
	sawDispatch := false
	for !sawDispatch {
		frame, err := readFlowTeamFrameNoFatal(flow, wsReadTimeout)
		if err != nil {
			t.Fatalf("read flow dispatch: %v", err)
		}
		for _, part := range frame.GetFlowParts().GetParts() {
			if press := part.GetKeyboardPress(); press != nil && press.GetKey() == game.KeyboardKey_KEYBOARD_KEY_F2 {
				sawDispatch = true
			}
		}
	}
	if err := flow.Close(); err != nil && !errors.Is(err, websocket.ErrCloseSent) {
		t.Fatalf("close flow connection: %v", err)
	}

	var events []*gamev2.ChatEvent
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("send stream: %v", r.err)
		}
		events = r.events
	case <-time.After(wsReadTimeout):
		t.Fatal("turn did not settle after the flow connection vanished")
	}
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if events[len(events)-1].GetTurnEnd().GetStatus() != gamev2.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("vanish turn ended %v, want COMPLETED (回合存活)", events[len(events)-1].GetTurnEnd().GetStatus())
	}

	results := collectAgentV2GameEvents(events)
	if len(results) == 0 {
		t.Fatal("the vanish turn produced no tool_result frames")
	}
	init := results[0]
	if init.GetStatus() != gamev2.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("saolei_init tool_result status = %v, want FAILED (in-flight dispatch settled on disconnect)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2DisconnectedContain) {
		t.Errorf("saolei_init error text = %q, want %q", init.GetResult(), agentV2DisconnectedContain)
	}
}
