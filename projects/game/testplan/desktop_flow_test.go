// Package testplan contains the desktop-flow large tests: the /api/v2 flow
// control stream from the desktop's side, with the test acting as the desktop
// client (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §6
// 验收锚点; specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §6).
// Cases cover the connection-confirmation first frame, the take-over of a
// second stream, the operation delivery → receipt → recognition loop through
// the team chain, the in-flight failure when the desktop vanishes, the idle
// heartbeat liveness, and the uplink size guard — one test per concern
// (style/large_test.md §测试组织).
package testplan

import (
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"google.golang.org/protobuf/proto"
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

// assertActiveStatusFrame checks one downlink frame carries exactly the ACTIVE
// status signal — the connection confirmation and heartbeat shape
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2/§2.3).
func assertActiveStatusFrame(t *testing.T, what string, frame *game.TeamFrame) {
	t.Helper()

	parts := frame.GetFlowParts().GetParts()
	if len(parts) != 1 {
		t.Fatalf("%s parts = %d, want 1 status signal", what, len(parts))
	}
	signal := parts[0].GetStatus()
	if signal == nil {
		t.Fatalf("%s part = %T, want a StatusSignal", what, parts[0].GetKind())
	}
	if signal.GetStatus() != game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE {
		t.Errorf("%s status = %v, want the ACTIVE signal", what, signal.GetStatus())
	}
}

// TestDesktopFlowFirstStatusFrame covers the connection confirmation
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.2): the
// server pushes a STATUS_ACTIVE status frame immediately after the downlink
// stream opens — the first-frame signal the real desktop's 10s Connect window
// waits for; no uplink probe is involved.
func TestDesktopFlowFirstStatusFrame(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-first-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	stream := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer stream.Close()

	assertActiveStatusFrame(t, "first frame", readFlowTeamFrame(t, stream, 10*time.Second))
}

// TestDesktopFlowSecondConnectionTakesOver covers the take-over baseline
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.4): a
// second downlink stream on the same session ends the first, and the second
// stream is the one that stays serviceable.
func TestDesktopFlowSecondConnectionTakesOver(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-takeover-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	first := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer first.Close()

	second := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer second.Close()

	// The first stream was ended by the takeover: its next reads terminate
	// instead of delivering further frames. The read window exceeds the
	// heartbeat interval, so an un-taken-over idle stream would keep yielding
	// heartbeats rather than a timeout error; only a real close breaks the
	// loop early.
	deadline := time.Now().Add(flowReadTimeout)
	for {
		if _, err := readFlowTeamFrameNoFatal(first, flowHeartbeatTimeout); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first flow stream was never closed by the second stream's takeover")
		}
	}
}

// TestDesktopFlowOperationDeliveryAndReceipt drives the delivery → receipt →
// recognition loop with the test as the desktop
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §6 验收锚点)
// through the team chain: the user Send drives the planner opening, the
// structural continuation drives the player, the F2 new-game dispatch arrives
// on the downlink stream, the test answers it with the recognizable win-board
// screenshot, and the team stream carries the SUCCEEDED tool results with the
// board text.
func TestDesktopFlowOperationDeliveryAndReceipt(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-receipt-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "flow-receipt")

	flow := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	// The win board at init: the terminal init result concludes the player
	// turn (specs/062-team-game-end-handoff/spec.md FR-002 ①), so the
	// scripted operate batch is never requested — the single F2 reply closes
	// the chain.
	scriptCh := serveTeamFlowScript(ctx, flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardWinPNG},
	}, flowReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, flowReadTimeout)
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
// accepts the F2 dispatch and then closes the downlink stream without
// replying — the bridge settles the in-flight dispatch FAILED "desktop
// disconnected", the tool result is a model-visible error, and the member
// turn completes
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §2.5).
func TestDesktopFlowDisconnectFailsInFlight(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "flow-vanish-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "flow-vanish")

	flow := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	ch := drainTeamStreamAsync(stream)

	// Accept the dispatch, then drop the stream instead of replying.
	sawDispatch := false
	for !sawDispatch {
		frame, err := readFlowTeamFrameNoFatal(flow, flowReadTimeout)
		if err != nil {
			t.Fatalf("read flow dispatch: %v", err)
		}
		for _, part := range frame.GetFlowParts().GetParts() {
			if press := part.GetKeyboardPress(); press != nil && press.GetKey() == game.KeyboardKey_KEYBOARD_KEY_F2 {
				sawDispatch = true
			}
		}
	}
	flow.Close()

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

// TestDesktopFlowIdleHeartbeatKeepsStreamAlive covers SC-004: with no
// operation request in flight the server keeps pushing ~10s status heartbeats
// on the open downlink stream and the stream stays readable across idle
// windows instead of being dropped by an intermediate layer
// (specs/066-desktop-bridge-split/spec.md US1-3 / SC-004).
func TestDesktopFlowIdleHeartbeatKeepsStreamAlive(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-idle-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	stream := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer stream.Close()

	// The first frame is the connection confirmation; two more heartbeats on
	// the same stream with no uplink traffic prove the periodic liveness.
	assertActiveStatusFrame(t, "first frame", readFlowTeamFrame(t, stream, 10*time.Second))
	assertActiveStatusFrame(t, "heartbeat 1", readFlowHeartbeat(t, stream, flowHeartbeatTimeout))
	assertActiveStatusFrame(t, "heartbeat 2", readFlowHeartbeat(t, stream, flowHeartbeatTimeout))
}

// TestDesktopFlowOversizedReceipt covers SC-003: the uplink is a raw proto
// payload (no base64/JSON inflation) — a ≤5MiB receipt body is accepted as
// exactly the marshaled SendFrameRequest — and a >8MiB body is rejected by
// the gateway's Content-Length precheck with a clear 413, never silently
// truncated
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §1.1/§3.3).
func TestDesktopFlowOversizedReceipt(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "flow-oversize-" + uniqueSuffix()
	ensureAgentV2Session(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)

	stream := openDesktopStream(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer stream.Close()
	// Consume the connection confirmation: the session needs an active
	// downlink stream for SendFrame (contract §2.6).
	assertActiveStatusFrame(t, "first frame", readFlowTeamFrame(t, stream, 10*time.Second))

	// A ≤5MiB body is sent as the raw marshaled proto and accepted. The
	// tool_id matches no in-flight dispatch, so agent_v2 logs and ignores it —
	// the case exercises the transport size guard, not recognition.
	smallReceipt := flowReceiptFrame("", "oversize-small", game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, make([]byte, 5<<20-256))
	smallPayload, err := proto.Marshal(&game.SendFrameRequest{Frame: smallReceipt})
	if err != nil {
		t.Fatalf("marshal small SendFrameRequest: %v", err)
	}
	if len(smallPayload) > 5<<20 {
		t.Fatalf("small payload = %d bytes, want <= 5MiB", len(smallPayload))
	}
	status, body, err := postFlowPayloadNoFatal(ctx, stream, smallPayload)
	if err != nil {
		t.Fatalf("post <=5MiB payload: %v", err)
	}
	if status != 200 {
		t.Fatalf("post <=5MiB payload status=%d, body=%s, want 200", status, body)
	}

	// A >8MiB body hits the Content-Length precheck: 413 text/plain, before
	// the body is read.
	bigReceipt := flowReceiptFrame("", "oversize-big", game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, make([]byte, 8<<20))
	bigPayload, err := proto.Marshal(&game.SendFrameRequest{Frame: bigReceipt})
	if err != nil {
		t.Fatalf("marshal big SendFrameRequest: %v", err)
	}
	if len(bigPayload) <= 8<<20 {
		t.Fatalf("big payload = %d bytes, want > 8MiB", len(bigPayload))
	}
	status, body, err = postFlowPayloadNoFatal(ctx, stream, bigPayload)
	if err != nil {
		t.Fatalf("post >8MiB payload: %v", err)
	}
	if status != 413 {
		t.Fatalf("post >8MiB payload status=%d, body=%s, want 413", status, body)
	}
	if !strings.Contains(string(body), "8MiB") {
		t.Errorf("413 body = %q, want the size-limit attribution", body)
	}
}
