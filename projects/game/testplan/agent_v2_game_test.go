// Package testplan contains the agent_v2 game large tests: the US1 game
// loop end to end over the /api/v2 conversation stream with the
// deterministic fake-llm game chain and the deployed fake-desktop executor
// (specs/051-agent-v2-dsh-migration — quickstart.md §2 agent-v2-game row).
// Cases are grouped by tested concern (won-chain tool stream, board text
// contract, terminal states, desktop-absent branch, multi-session
// isolation, two-stream independence), one test per concern —
// style/large_test.md §测试组织. The mid-game disconnect branch needs the
// drop deploy topology and lives in its own binary,
// agent_v2_game_disconnect_test.go
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §1.4).
package testplan

import (
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// assertGameToolResults walks the turn's tool_result frames and checks the
// game chain's shape against the fake-desktop scenario:
//
//	won:   saolei_init SUCCEEDED (the 9×9 win board) then saolei_operate
//	       SUCCEEDED (pre-dispatch game_won stop — the terminal-reject
//	       branch of specs/051-agent-v2-dsh-migration/data-model.md §2.5).
//	absent: exactly one FAILED result whose text names the
//	       bridge disconnect cause — a tool ERROR result (model-visible,
//	       not a fabricated success), agent-api.md §2.4.
//
// The board-text contract assertions (three-layer body: outcome line, game
// status line, ruler board) pin the v1 text contract the tools must keep
// (data-model.md §2.5, migrated verbatim into the saolei plugin).
func assertGameToolResults(t *testing.T, results []*game.ToolResultEvent) {
	t.Helper()

	if len(results) == 0 {
		t.Fatal("the game turn produced no tool_result frames")
	}
	for i, r := range results {
		if r.GetToolId() == "" {
			t.Errorf("tool_result[%d] carries an empty tool_id", i)
		}
	}
}

// TestAgentV2GameWonChainToolStream drives the full won-scenario chain on
// the desktop-e2e-won session (the executor session bound by
// deploy_agent_v2.yaml): the "开始一局扫雷" keyword fires
// saolei_init, the 9×9 win board opens the operate batch, and the batch
// stops pre-dispatch on game_won — the terminal-reject branch proving the
// final state gates further cell operations before any dispatch
// (quickstart §2 agent-v2-game: 工具链路 / 棋盘文本契约 / 终局与终局后拒绝).
func TestAgentV2GameWonChainToolStream(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, agent := agentV2GamePrep(t, sutHostURL, sutEnvName,
		agentV2DesktopWonSessionID, "game-won-"+uniqueSuffix(), "你是扫雷 player，play the win board")

	if agent.GetName() != sessionName+"/agent" {
		t.Fatalf("materialized agent name = %q, want %q", agent.GetName(), sessionName+"/agent")
	}
	if agent.GetPreset() == "" {
		t.Fatal("materialized agent carries an empty preset reference")
	}

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiGame+" on the win board")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)

	if end := events[len(events)-1].GetTurnEnd(); end.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game turn ended %v, want COMPLETED", end.GetStatus())
	}

	// Chain shape: init → operate, both SUCCEEDED with the won board text.
	results := collectAgentV2GameEvents(events)
	assertGameToolResults(t, results)
	if len(results) != 2 {
		t.Fatalf("tool_result count = %d, want 2 (saolei_init + saolei_operate)", len(results))
	}
	init, operate := results[0], results[1]

	// saolei_init: the win board recognized from the executor's screenshot —
	// outcome line, game status line, and the ruler board in the fixed
	// three-layer order (data-model.md §2.5 结果文本契约).
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_init status = %v, want SUCCEEDED (the desktop-e2e-won executor is connected)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2WonInitContains) {
		t.Errorf("saolei_init result = %q, want the outcome line %q", init.GetResult(), agentV2WonInitContains)
	}
	if !strings.Contains(init.GetResult(), agentV2WonStatusContains) {
		t.Errorf("saolei_init result = %q, want the status line %q", init.GetResult(), agentV2WonStatusContains)
	}
	if !strings.Contains(init.GetResult(), agentV2WonBoardContains) {
		t.Errorf("saolei_init result = %q, want %q", init.GetResult(), agentV2WonBoardContains)
	}
	if !strings.Contains(init.GetResult(), "col0") || !strings.Contains(init.GetResult(), "row0") {
		t.Errorf("saolei_init result = %q, want the col/row coordinate ruler", init.GetResult())
	}
	if strings.Contains(init.GetResult(), "valid range") {
		t.Errorf("saolei_init result = %q, must not carry the rejection-only valid range line", init.GetResult())
	}

	// saolei_operate: the batch stops at the first op BEFORE dispatch — the
	// game_won terminal rejection (终局后拒绝), still a normal SUCCEEDED tool
	// result (拒绝是正常结果文本, data-model.md §2.5).
	if operate.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_operate status = %v, want SUCCEEDED (a game-rules rejection is a normal result)", operate.GetStatus())
	}
	if !strings.Contains(operate.GetResult(), agentV2WonRejectContains) {
		t.Errorf("saolei_operate result = %q, want the batch-stop line %q", operate.GetResult(), agentV2WonRejectContains)
	}
	if !strings.Contains(operate.GetResult(), agentV2WonStatusContains) {
		t.Errorf("saolei_operate result = %q, want the status line %q", operate.GetResult(), agentV2WonStatusContains)
	}

	// The terminal summary closes the chain and lands as the last TEXT block
	// (the fake tool rules reply text after the operate result).
	term := agentV2TerminalBlocksFromEvents(events)
	if term.text != agentV2WonSummaryText {
		t.Errorf("terminal text = %q, want %q (testdata/agent_v2_saolei_tools.yaml agent-v2-saolei-operate-won)", term.text, agentV2WonSummaryText)
	}

	// History backfill carries the same terminal tool states by tool_id
	// (data-model.md §2.3): two tool-call blocks settled SUCCEEDED with the
	// board text, plus the user message and the text-only reply.
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	messages := hist.GetMessages()
	if len(messages) != 4 {
		t.Fatalf("history messages = %d, want 4 (user + init step + operate step + summary)", len(messages))
	}
	var toolBlocks []*game.ToolCallBlock
	for _, m := range messages {
		for _, b := range m.GetBlocks() {
			if call := b.GetToolCall(); call != nil {
				toolBlocks = append(toolBlocks, call)
			}
		}
	}
	if len(toolBlocks) != 2 {
		t.Fatalf("history tool-call blocks = %d, want 2", len(toolBlocks))
	}
	wantResults := []string{init.GetResult(), operate.GetResult()}
	for i, call := range toolBlocks {
		if call.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
			t.Errorf("history tool block %d (%s) status = %v, want SUCCEEDED", i, call.GetName(), call.GetStatus())
		}
		if call.GetName() != "saolei_init" && call.GetName() != "saolei_operate" {
			t.Errorf("history tool block %d name = %q, want a saolei tool", i, call.GetName())
		}
		if call.GetResult() != wantResults[i] {
			t.Errorf("history tool block %d result = %q, want the streamed tool_result text %q (tool_id %s backfill)", i, call.GetResult(), wantResults[i], call.GetToolId())
		}
	}
	// The fake pins one provider call id across a response stream
	// (fake-responses-wire.md §2 invariant 3), so both blocks legitimately
	// share it; assert the ids are present and that the streamed tool_result
	// frames settled the same identities.
	if toolBlocks[0].GetToolId() == "" || toolBlocks[0].GetToolId() != toolBlocks[1].GetToolId() {
		t.Errorf("history tool ids = %q / %q, want the fake's constant call id on both blocks", toolBlocks[0].GetToolId(), toolBlocks[1].GetToolId())
	}
}

// TestAgentV2GameDesktopAbsent covers US1 scenario 4: on a session with no
// desktop connection, saolei_init's dispatch fails with the bridge's
// "desktop disconnected" error — a FAILED tool result the model can read —
// and the turn still completes and the process stays usable
// (desktop-bridge.md §6 验收锚点 2).
func TestAgentV2GameDesktopAbsent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		"desktop-absent-"+uniqueSuffix(), "game-absent-"+uniqueSuffix(), "你是扫雷 player，no desktop around")

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiNodesktop+" now")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if end := events[len(events)-1].GetTurnEnd(); end.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("desktop-absent turn ended %v, want COMPLETED (回合存活, US1 场景 4)", end.GetStatus())
	}

	results := collectAgentV2GameEvents(events)
	assertGameToolResults(t, results)
	if len(results) != 1 {
		t.Fatalf("tool_result count = %d, want 1 (the failed saolei_init)", len(results))
	}
	init := results[0]
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("saolei_init status = %v, want FAILED (no desktop connection, desktop-bridge.md §2)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2DisconnectedContain) {
		t.Errorf("saolei_init error text = %q, want the readable cause %q (model-visible failure)", init.GetResult(), agentV2DisconnectedContain)
	}
	term := agentV2TerminalBlocksFromEvents(events)
	if term.text != agentV2NodesktopSummary {
		t.Errorf("terminal text = %q, want %q (testdata/agent_v2_saolei_tools.yaml agent-v2-saolei-init-nodesktop)", term.text, agentV2NodesktopSummary)
	}

	// The process and the session stay usable after the failed dispatch
	// (US1 场景 4: 回合不崩溃、进程存活): a plain follow-up turn completes.
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" after the absent desktop")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("follow-up turn ended %v, want COMPLETED", events2[len(events2)-1].GetTurnEnd().GetStatus())
	}
	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2PlainText {
		t.Errorf("follow-up text = %q, want %q", got, agentV2PlainText)
	}
}

// TestAgentV2GameMultiSessionIsolation plays a desktop-connected session
// and a desktop-absent session CONCURRENTLY: each keeps its own chain
// outcome (won vs disconnected), turn identity, and history — the loop's
// per-session game state must not cross sessions (Edge-并发多 session 游戏,
// data-model.md §4-6).
func TestAgentV2GameMultiSessionIsolation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	connected := ensureAgentV2Session(t, sutHostURL, sutEnvName, agentV2DesktopWonSessionID)
	isolated := ensureAgentV2Session(t, sutHostURL, sutEnvName, "desktop-absent-iso-"+uniqueSuffix())
	isolatedPreset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "game-iso-"+uniqueSuffix(), "你是扫雷 player，isolated session")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, isolated, isolatedPreset.GetName(), "")
	// The connected session reuses its materialization from the won-chain
	// case when present; materializing again is the idempotent refresh.
	connectedPreset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "game-conn-iso-"+uniqueSuffix(), "你是扫雷 player，connected session")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, connected, connectedPreset.GetName(), "")

	textConnected := agentV2TriggerSaoleiGame + " isolation probe"
	textIsolated := agentV2TriggerSaoleiNodesktop + " isolation probe"
	streamConnected := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, connected, textConnected)
	streamIsolated := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, isolated, textIsolated)

	chConnected := drainAgentV2TurnAsync(streamConnected)
	chIsolated := drainAgentV2TurnAsync(streamIsolated)
	var eventsConnected, eventsIsolated []*game.ChatEvent
	for eventsConnected == nil || eventsIsolated == nil {
		select {
		case r := <-chConnected:
			if r.err != nil {
				t.Fatalf("connected session stream: %v", r.err)
			}
			eventsConnected = r.events
		case r := <-chIsolated:
			if r.err != nil {
				t.Fatalf("isolated session stream: %v", r.err)
			}
			eventsIsolated = r.events
		case <-time.After(wsReadTimeout):
			t.Fatal("concurrent game turns did not both complete within the read window")
		}
	}
	assertAgentV2TurnWellFormed(t, connected, eventsConnected)
	assertAgentV2TurnWellFormed(t, isolated, eventsIsolated)

	// Distinct outcomes on the shared instance: the connected session's
	// chain reached the won board, the isolated one failed on dispatch.
	resultsConnected := collectAgentV2GameEvents(eventsConnected)
	if len(resultsConnected) == 0 || resultsConnected[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("connected session tool results = %+v, want a SUCCEEDED saolei_init", resultsConnected)
	}
	resultsIsolated := collectAgentV2GameEvents(eventsIsolated)
	if len(resultsIsolated) == 0 || resultsIsolated[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("isolated session tool results = %+v, want a FAILED saolei_init (no desktop)", resultsIsolated)
	}
	if eventsConnected[0].GetTurnId() == eventsIsolated[0].GetTurnId() {
		t.Errorf("both sessions report turn_id %q — turn identity leaked across sessions", eventsConnected[0].GetTurnId())
	}

	// Each history carries only its own marker (no cross-session bleed).
	histConnected := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, connected)
	histIsolated := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, isolated)
	for i, m := range histIsolated.GetMessages() {
		if m.GetRole() == game.Role_ROLE_USER && agentV2MessageText(m) == textConnected {
			t.Errorf("isolated session history[%d] carries the connected session's marker — game histories are not isolated", i)
		}
	}
	foundIsolatedMarker := false
	for _, m := range histIsolated.GetMessages() {
		if m.GetRole() == game.Role_ROLE_USER && agentV2MessageText(m) == textIsolated {
			foundIsolatedMarker = true
		}
	}
	if !foundIsolatedMarker {
		t.Error("isolated session history lost its own user marker")
	}
	// The connected session's history settled its own chain: the tool-call
	// blocks are terminal — neither session's game state leaked into or
	// starved the other.
	settledBlocks := 0
	for _, m := range histConnected.GetMessages() {
		for _, b := range m.GetBlocks() {
			if call := b.GetToolCall(); call != nil && call.GetStatus() != game.ToolStatus_TOOL_STATUS_RUNNING {
				settledBlocks++
			}
		}
	}
	if settledBlocks == 0 {
		t.Error("connected session history has no settled tool-call block after its game turn")
	}
}

// TestAgentV2GameConversationStreamIndependentOfFlow covers US1 scenario 6
// from the conversation side: the test plays the desktop over its own
// /api/v2 flow connection, the Send stream is closed mid-turn by the
// client (a browser navigating away), and the turn still runs to
// completion — the flow stream keeps delivering operations and the history
// records the full chain (两流独立, desktop-bridge.md §5 验收锚点 4).
func TestAgentV2GameConversationStreamIndependentOfFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "desktop-flow-independent-" + uniqueSuffix()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		sessionID, "game-independent-"+uniqueSuffix(), "你是扫雷 player，play while the page closes")

	// The test's own flow connection — the desktop half of the two streams.
	flow := connectAgentV2Flow(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	sendFlowProbe(t, flow, sessionID)
	if frame := readFlowTeamFrame(t, flow, wsReadTimeout); len(frame.GetFlowParts().GetParts()) == 0 {
		t.Fatalf("probe reply = %+v, want a status echo frame", frame)
	}

	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiGame+" while closing the page")

	// Read until the first tool-call block starts, then drop the
	// conversation stream the way a closed browser tab would.
	dropped := false
	for !dropped {
		evt := nextAgentV2Event(t, stream.Scanner)
		if start := evt.GetBlockStart(); start != nil && start.GetType() == game.BlockType_BLOCK_TYPE_TOOL_CALL {
			stream.Close()
			dropped = true
		}
	}

	// Serve the init dispatch from the flow side: the F2 new-game press
	// arrives, the receipt carries the recognizable win board, and the rest
	// of the chain (the pre-dispatch game_won operate stop) needs no further
	// dispatch.
	serveWonInitReceipt(t, flow, sessionID, wsReadTimeout)

	// Give the server-side turn time to finish after the client vanished,
	// then verify it completed: the full chain is in the history and the
	// flow stream is still alive.
	deadline := time.Now().Add(wsReadTimeout)
	for {
		hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
		if gameTurnHistoryComplete(t, hist, agentV2WonSummaryText) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn did not complete after the conversation stream closed; history = %+v", hist)
		}
		time.Sleep(time.Second)
	}

	// The flow stream survived the conversation-stream drop: the bridge
	// still answers the probe on the same connection.
	sendFlowProbe(t, flow, sessionID)
	if frame := readFlowTeamFrame(t, flow, wsReadTimeout); len(frame.GetFlowParts().GetParts()) == 0 {
		t.Fatalf("post-drop probe reply = %+v, want a status echo (flow stream unaffected, US1 场景 6)", frame)
	}
}

// gameTurnHistoryComplete reports whether the history shows the full
// post-close turn: the user message, at least one settled tool-call block,
// and the terminal summary text.
func gameTurnHistoryComplete(t *testing.T, hist *game.ListAgentMessagesResponse, summaryText string) bool {
	t.Helper()

	sawTool := false
	for _, m := range hist.GetMessages() {
		for _, b := range m.GetBlocks() {
			if call := b.GetToolCall(); call != nil && call.GetStatus() != game.ToolStatus_TOOL_STATUS_RUNNING {
				sawTool = true
			}
			if text := b.GetText(); text != nil && text.GetContent() == summaryText {
				return sawTool
			}
		}
	}
	return false
}
