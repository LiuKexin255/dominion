// Package testplan contains the desktop-flow large tests: the /api/v2 flow
// control stream from the desktop's side, with the test acting as the desktop
// client (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §6
// 验收锚点; specs/059-agent-v2-team-mode/tasks.md T018). Cases cover the
// connection probe, the take-over of a second connection, the operation
// delivery → receipt → recognition loop through the team chain, and the
// in-flight failure when the desktop vanishes — one test per concern
// (style/large_test.md §测试组织).
package testplan

import (
	"errors"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

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
// 探测行): the first UserFrame carrying the ACTIVE status signal is answered
// with a status echo frame — the application-layer confirmation the real
// desktop's 10s Connect window waits for.
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

// TestDesktopFlowSecondConnectionTakesOver covers the take-over baseline
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
// recognition loop with the test as the desktop (desktop-bridge.md §6 验收
// 锚点 1) through the team chain: the user Send drives the planner opening,
// the structural continuation drives the player, the F2 new-game dispatch
// arrives on the flow stream, the test answers it with the recognizable
// win-board screenshot, and the team stream carries the SUCCEEDED tool
// results with the board text.
func TestDesktopFlowOperationDeliveryAndReceipt(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-receipt-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "flow-receipt")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	// The win board at init: the terminal init result concludes the player
	// turn (specs/062-team-game-end-handoff/spec.md FR-002 ①), so the
	// scripted operate batch is never requested — the single F2 reply closes
	// the chain.
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardWinPNG},
	}, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)

	playerTurns := teamTurnsForMember(events, "player")
	if len(playerTurns) != 1 {
		t.Fatalf("player turns = %d, want 1 (the game chain)", len(playerTurns))
	}
	results := teamTurnToolResults(playerTurns[0])
	if len(results) != 1 {
		t.Fatalf("tool_result count = %d, want 1 (saolei_init only — the terminal init result concludes the turn)", len(results))
	}
	init := results[0]
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
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
// disconnected", the tool result is a model-visible error, and the member
// turn completes (desktop-bridge.md §2 断连结算).
func TestDesktopFlowDisconnectFailsInFlight(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-vanish-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "flow-vanish")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	ch := drainTeamStreamAsync(stream)

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

	events := waitTeamStream(t, ch, "vanish stream")
	assertTeamStreamWellFormed(t, sessionName, events)
	playerTurns := teamTurnsForMember(events, "player")
	if len(playerTurns) != 1 {
		t.Fatalf("player turns = %d, want 1", len(playerTurns))
	}
	if status := teamTurnEndStatus(playerTurns[0]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("vanish turn ended %v, want COMPLETED (回合存活)", status)
	}
	results := teamTurnToolResults(playerTurns[0])
	if len(results) == 0 {
		t.Fatal("the vanish turn produced no tool_result frames")
	}
	init := results[0]
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("saolei_init tool_result status = %v, want FAILED (in-flight dispatch settled on disconnect)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2DisconnectedContain) {
		t.Errorf("saolei_init error text = %q, want %q", init.GetResult(), agentV2DisconnectedContain)
	}
}
