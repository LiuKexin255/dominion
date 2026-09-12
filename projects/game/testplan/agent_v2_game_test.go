// Package testplan contains the agent_v2 team game large tests: the saolei
// game loop end to end over the team stream with the deterministic fake-llm
// chain and either the deployed fake-desktop executor (the won topology) or
// the test's own flow connection. Cases are grouped by tested concern (won
// chain on the executor, terminal won + review continuation, terminal lost +
// review stop, desktop-absent, multi-session isolation, stream independence),
// one test per concern — style/large_test.md §测试组织. The disconnect branch
// needs the drop deploy topology and lives in its own binary,
// agent_v2_game_disconnect_test.go.
package testplan

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentV2TeamGameWonChainOnExecutor drives the won-scenario chain on the
// desktop-e2e-won session (the executor bound by deploy_agent_v2.yaml): the
// user Send drives the planner opening, the structural continuation drives
// the player, and saolei_init recognizes the executor's win board. The
// terminal init result concludes the player turn
// (specs/062-team-game-end-handoff/spec.md FR-002 ①): the scripted operate
// batch never runs (agent_v2_saolei_tools.yaml agent-v2-saolei-init-operate)
// and no review follows (init writes no terminal event), so the chain rests
// after two turns. The merged history backfills the settled init call.
func TestAgentV2TeamGameWonChainOnExecutor(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, agentV2DesktopWonSessionID, "game-won")
	// The executor re-dials after fault cycles; wait for its connection fact
	// before sending so the init dispatch cannot race the reconnect.
	waitTeamDesktopConnected(t, ctx, sutHostURL, sutEnvName, sessionName, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)

	// The chain rests after the player turn: the init-terminal conclusion
	// writes no terminal event, so no planner review turn follows.
	turns := groupTeamMemberTurns(events)
	if len(turns) != 2 {
		t.Fatalf("member turns = %d, want 2 (planner opening + player game, no review)", len(turns))
	}
	if turns[0].member != "planner" {
		t.Fatalf("first turn member = %v, want 'planner'", turns[0].member)
	}
	if _, text := teamTurnBlocks(turns[0]); text != teamPlannerOpeningText {
		t.Errorf("planner opening text = %q, want %q", text, teamPlannerOpeningText)
	}
	playerTurn := turns[1]
	if playerTurn.member != "player" {
		t.Fatalf("second turn member = %v, want 'player'", playerTurn.member)
	}
	if status := teamTurnEndStatus(playerTurn); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game turn ended %v, want COMPLETED", status)
	}

	// Chain shape: exactly the init result — the win board is terminal at
	// init, so the turn concludes there and the scripted operate batch is
	// never requested (no second model output).
	results := teamTurnToolResults(playerTurn)
	if len(results) != 1 {
		t.Fatalf("tool_result count = %d, want 1 (saolei_init only — the terminal result concludes the turn)", len(results))
	}
	init := results[0]
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_init status = %v, want SUCCEEDED (the desktop-e2e-won executor is connected)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2WonInitContains) || !strings.Contains(init.GetResult(), agentV2WonStatusContains) || !strings.Contains(init.GetResult(), agentV2WonBoardContains) {
		t.Errorf("saolei_init result = %q, want the recognized win board (%q / %q / %q)",
			init.GetResult(), agentV2WonInitContains, agentV2WonStatusContains, agentV2WonBoardContains)
	}
	assertTerminalTurnEndsWithToolBlock(t, playerTurn)
	// The conclusion ends the turn after the init step: a second model output
	// (the scripted operate batch) would surface as a second step.
	assertSingleModelStep(t, playerTurn)

	// Backfill: the player's merge entry carries the settled init call whose
	// result equals the streamed tool_result text.
	toolBlocks := teamMergedToolCallBlocks(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName))
	if len(toolBlocks) != 1 {
		t.Fatalf("history tool-call blocks = %d, want 1 (the init call)", len(toolBlocks))
	}
	if toolBlocks[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Errorf("history tool block (%s) status = %v, want SUCCEEDED", toolBlocks[0].GetName(), toolBlocks[0].GetStatus())
	}
	if toolBlocks[0].GetName() != "saolei_init" {
		t.Errorf("history tool block name = %q, want saolei_init", toolBlocks[0].GetName())
	}
	if toolBlocks[0].GetResult() != init.GetResult() {
		t.Errorf("history tool block result = %q, want the streamed text %q", toolBlocks[0].GetResult(), init.GetResult())
	}
	if toolBlocks[0].GetToolId() == "" {
		t.Error("history tool block lacks its provider call id")
	}
	assertTerminalHistoriesNoAbortTraces(t, ctx, sutHostURL, sutEnvName, sessionName)
}

// TestAgentV2TeamGameTerminalWonAndReviewContinues covers US2 场景 4 with the
// continue behavior (quickstart.md V4): a full game terminates won on the
// operate receipt (the test's own desktop half seeds an in-progress board and
// answers the first cell dispatch with the win board) — the terminal operate
// result concludes the player turn
// (specs/062-team-game-end-handoff/spec.md FR-001), so the scripted won
// summary never runs — the gameEnded fact drives the planner review, and the
// review's next-game instruction structurally drives the player into a second
// game WITHOUT any further user input. The second game's init already
// recognizes the win board, so that turn concludes at the init result
// (specs/062-team-game-end-handoff/spec.md FR-002 ①). The single Send stream
// covers all four member turns until the team rests; the planner view shows
// the player's verbatim process relay.
func TestAgentV2TeamGameTerminalWonAndReviewContinues(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-won-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "team-won")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardCompatWinPNG, saoleiBoardWinPNG},
		stepBoards: [][]byte{saoleiBoardWinPNG},
	}, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)
	assertTeamStreamMessagesMatchList(t, ctx, sutHostURL, sutEnvName, sessionName, events)

	turns := groupTeamMemberTurns(events)
	if len(turns) != 4 {
		t.Fatalf("member turns = %d, want 4 (planner opening, player game 1, planner review, player game 2)", len(turns))
	}
	wantMembers := []string{
		"planner",
		"player",
		"planner",
		"player",
	}
	for i, want := range wantMembers {
		if turns[i].member != want {
			t.Fatalf("turn %d member = %v, want %v", i, turns[i].member, want)
		}
	}
	// Game 1: init recognizes the in-progress board, the first cell dispatch
	// returns the win board, and the terminal operate result concludes the
	// turn (specs/062-team-game-end-handoff/spec.md FR-001) — the scripted
	// won summary (agent_v2_saolei_tools.yaml agent-v2-saolei-operate-won)
	// never runs.
	game1 := teamTurnToolResults(turns[1])
	if len(game1) != 2 || game1[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || game1[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 1 tool results = %+v, want init + operate SUCCEEDED", game1)
	}
	if !strings.Contains(game1[0].GetResult(), agentV2ProgStatusContains) || !strings.Contains(game1[0].GetResult(), agentV2WonBoardContains) {
		t.Errorf("game 1 init result = %q, want a playing 9*9 board", game1[0].GetResult())
	}
	if !strings.Contains(game1[1].GetResult(), agentV2WonStatusContains) {
		t.Errorf("game 1 operate result = %q, want the won terminal status", game1[1].GetResult())
	}
	if status := teamTurnEndStatus(turns[1]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 1 turn ended %v, want COMPLETED (the conclusion is invisible in the terminal status)", status)
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[1])

	// Review: the planner consumes the player's raw process and emits the
	// continue strategy. The review entry is gated on the terminal result
	// text: its keywords ("game status: won") must match the review drive's
	// LAST user message — the terminal <player-tool-call> relay itself under
	// the 062 turn conclusion (team_planner.yaml team-planner-review-continue),
	// so this turn occurring at all proves the terminal unit reached the
	// planner's model input (specs/062-team-game-end-handoff/spec.md SC-003).
	// The planner view assertion below pins the relay form itself.
	if _, text := teamTurnBlocks(turns[2]); text != teamPlannerReviewContinueText {
		t.Errorf("review text = %q, want %q", text, teamPlannerReviewContinueText)
	}

	// Game 2 opened without any user input: the second init recognizes the
	// win board, so the terminal init result concludes the turn
	// (specs/062-team-game-end-handoff/spec.md FR-002 ①) before the scripted
	// operate batch (agent-v2-saolei-init-operate) — the turn carries exactly
	// the one init result.
	game2 := teamTurnToolResults(turns[3])
	if len(game2) != 1 || game2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 2 tool results = %+v, want the single init SUCCEEDED (the terminal init concludes the turn)", game2)
	}
	if !strings.Contains(game2[0].GetResult(), agentV2WonStatusContains) {
		t.Errorf("game 2 init result = %q, want the won board recognized at init", game2[0].GetResult())
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[3])
	// Same init-terminal topology as the won chain: one model step, no second
	// output (the scripted operate batch never runs).
	assertSingleModelStep(t, turns[3])

	// No user input after the first Send: exactly one USER merge entry.
	merged := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	userEntries := teamMessagesForMember(merged, "user")
	if len(userEntries) != 1 || agentV2MessageText(userEntries[0].GetMessage()) != teamStartMessage {
		t.Fatalf("USER merge entries = %d, want exactly the single Send (结构性续驱无需用户触发)", len(userEntries))
	}

	// Post-review context completeness: the player turn after the review
	// assembles its model input from the player's own session log, so the
	// merged sequence must carry the player's terminal tool units settled —
	// the game 1 operate receipt that concluded the reviewed turn and the
	// game 2 init receipt. Each is matched to its streamed tool_result by
	// provider call id (specs/062-team-game-end-handoff/spec.md SC-003:
	// session-log completeness / List 回填).
	mergedBlocks := teamMergedToolCallBlocks(merged)
	assertMergedToolResultSettled(t, mergedBlocks, game1[1])
	assertMergedToolResultSettled(t, mergedBlocks, game2[0])

	// The planner view carries the player's raw process as a sender-annotated
	// relay (specs/060-agent-v2-team-optimize/contracts/team-api.md §4:
	// verbatim tool result inside the tag pair, no head line, no truncation),
	// and the player view carries the review relay. Matching the complete
	// operate result text pins the terminal unit: the game 1 init relay
	// carries the playing board, so only the operate receipt carries the won
	// status verbatim (FR-005).
	plannerView := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "planner")
	terminalUnitResult := game1[1].GetResult()
	sawTerminalRelay := false
	for _, entry := range plannerView {
		if entry.GetSender() != "player" {
			continue
		}
		text := agentV2MessageText(entry.GetMessage())
		if !strings.Contains(text, "tool: saolei_operate") || !strings.Contains(text, terminalUnitResult) {
			continue
		}
		sawTerminalRelay = true
		if !strings.HasPrefix(text, "<player-tool-call>\n") {
			t.Errorf("player tool relay = %q, want the tag-wrapped form with no head line", text)
		}
		if !strings.HasSuffix(text, "\n</player-tool-call>") {
			t.Errorf("player tool relay = %q, want the closed tag pair (no truncation)", text)
		}
	}
	if !sawTerminalRelay {
		t.Errorf("planner view has no relayed <player-tool-call> unit carrying the full terminal operate result (FR-005/SC-003)")
	}
	// The player view carries the review relay that structurally drove the
	// next game (turns[3] above): the tag pair wraps the verbatim review body
	// with no head line.
	playerView := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	sawReviewRelay := false
	for _, entry := range playerView {
		if entry.GetSender() != "planner" {
			continue
		}
		text := agentV2MessageText(entry.GetMessage())
		if !strings.Contains(text, teamPlannerReviewContinueText) {
			continue
		}
		sawReviewRelay = true
		if !strings.HasPrefix(text, "<planner-message>\n") || !strings.HasSuffix(text, "\n</planner-message>") {
			t.Errorf("review relay = %q, want the tag pair around the verbatim body with no head line", text)
		}
	}
	if !sawReviewRelay {
		t.Error("player view has no relayed review message after the game-end drive")
	}
	assertTerminalHistoriesNoAbortTraces(t, ctx, sutHostURL, sutEnvName, sessionName)
}

// TestAgentV2TeamGameTerminalLostAndReviewStops covers US2 场景 4 with the
// stop behavior (quickstart.md V4: "不开局"): the operate receipt is the loss
// board — the terminal operate result concludes the player turn
// (specs/062-team-game-end-handoff/spec.md FR-001), so the scripted lost
// summary never runs — the gameEnded fact drives the planner review WITHOUT a
// next-game instruction, and the structurally driven player consumes it and
// opens no new game — the stream then ends at the static point with no other
// drive.
func TestAgentV2TeamGameTerminalLostAndReviewStops(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-lost-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "team-lost")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	// One F2 init (the progressive 16×16 board) and one losing cell
	// dispatch; the batch stops at the terminal loss.
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardInitPNG},
		stepBoards: [][]byte{saoleiBoardLossPNG},
	}, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)
	assertTeamStreamMessagesMatchList(t, ctx, sutHostURL, sutEnvName, sessionName, events)

	turns := groupTeamMemberTurns(events)
	if len(turns) != 4 {
		t.Fatalf("member turns = %d, want 4 (planner opening, player game, planner review, player stop ack)", len(turns))
	}
	// Game: init sees the 16×16 in-progress board, the first cell dispatch
	// loses the game, and the terminal operate result concludes the turn
	// (specs/062-team-game-end-handoff/spec.md FR-001) — the scripted lost
	// summary (agent_v2_saolei_tools.yaml agent-v2-saolei-operate-lost) never
	// runs.
	gameResults := teamTurnToolResults(turns[1])
	if len(gameResults) != 2 || gameResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || gameResults[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game tool results = %+v, want init + operate SUCCEEDED", gameResults)
	}
	if !strings.Contains(gameResults[0].GetResult(), agentV2ProgStatusContains) || !strings.Contains(gameResults[0].GetResult(), agentV2ProgBoardContains) {
		t.Errorf("init result = %q, want the playing 16*16 board", gameResults[0].GetResult())
	}
	if !strings.Contains(gameResults[1].GetResult(), agentV2LostStatusContains) {
		t.Errorf("operate result = %q, want the lost terminal status", gameResults[1].GetResult())
	}
	if status := teamTurnEndStatus(turns[1]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game turn ended %v, want COMPLETED (the conclusion is invisible in the terminal status)", status)
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[1])
	if turns[2].member != "planner" {
		t.Fatalf("review turn member = %v, want 'planner' (gameEnded drive)", turns[2].member)
	}
	if _, text := teamTurnBlocks(turns[2]); text != teamPlannerReviewStopText {
		t.Errorf("review text = %q, want %q", text, teamPlannerReviewStopText)
	}
	if turns[3].member != "player" {
		t.Fatalf("post-review turn member = %v, want 'player' (structural continuation)", turns[3].member)
	}
	if _, text := teamTurnBlocks(turns[3]); text != teamPlayerResumeStopText {
		t.Errorf("stop acknowledgement = %q, want %q (不开局)", text, teamPlayerResumeStopText)
	}
	// The review writes its fixed cross-game observation through the memory
	// tool (T023); the player's stop acknowledgement dispatches nothing, so
	// no second init was ever dispatched and the game face still has exactly
	// its two results.
	reviewResults := teamTurnToolResults(turns[2])
	if len(reviewResults) != 1 || reviewResults[0].GetResult() != teamMemoryAddedResult {
		t.Errorf("review tool results = %+v, want the single memory add", reviewResults)
	}
	if got := len(teamTurnToolResults(turns[3])); got != 0 {
		t.Errorf("stop-ack tool results = %d, want 0 (no second game opened)", got)
	}
	assertTerminalHistoriesNoAbortTraces(t, ctx, sutHostURL, sutEnvName, sessionName)
}

// TestAgentV2TeamGameDesktopAbsent covers US2 场景 8's absent half: on a
// session with no desktop connection the player's saolei_init dispatch fails
// with the bridge's readable error (a model-visible FAILED result, not a fake
// success), the turn still completes, and a later Send keeps working (the
// team does not enter an undefined state).
func TestAgentV2TeamGameDesktopAbsent(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-absent-"+uniqueSuffix(), "absent")

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	playerTurns := teamTurnsForMember(events, "player")
	if len(playerTurns) != 1 {
		t.Fatalf("player turns = %d, want 1 (the failed init)", len(playerTurns))
	}
	if status := teamTurnEndStatus(playerTurns[0]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("desktop-absent turn ended %v, want COMPLETED (回合存活)", status)
	}
	results := teamTurnToolResults(playerTurns[0])
	if len(results) != 1 || results[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("tool results = %+v, want one FAILED saolei_init", results)
	}
	if !strings.Contains(results[0].GetResult(), agentV2DisconnectedContain) {
		t.Errorf("saolei_init error = %q, want %q (model-visible failure)", results[0].GetResult(), agentV2DisconnectedContain)
	}
	if _, text := teamTurnBlocks(playerTurns[0]); text != agentV2NodesktopSummary {
		t.Errorf("absent summary = %q, want %q", text, agentV2NodesktopSummary)
	}

	// The team stays usable: the current player activation digests a later
	// Send (team-player-user-intake).
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	playerTurns2 := teamTurnsForMember(events2, "player")
	if len(playerTurns2) != 1 {
		t.Fatalf("follow-up player turns = %d, want 1", len(playerTurns2))
	}
	if _, text := teamTurnBlocks(playerTurns2[0]); text != teamPlayerUserIntakeText {
		t.Errorf("follow-up text = %q, want %q", text, teamPlayerUserIntakeText)
	}
}

// TestAgentV2TeamGameMultiSessionIsolation plays a desktop-connected session
// and a desktop-absent session CONCURRENTLY: each keeps its own chain outcome
// (won board vs disconnected), turn identity, and history — the in-memory
// game state must not cross sessions.
func TestAgentV2TeamGameMultiSessionIsolation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	connectedID := "team-iso-conn-" + uniqueSuffix()
	connCtx, connName, _ := teamPrep(t, sutHostURL, sutEnvName, connectedID, "iso-conn")
	flow, _ := dialAgentV2FlowProbed(t, connCtx, sutHostURL, sutEnvName, connectedID)
	defer flow.Close()
	// The win board at init: the operate batch is rejected pre-dispatch, so
	// one F2 reply completes the chain.
	scriptCh := serveTeamFlowScript(flow, connectedID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardWinPNG},
	}, wsReadTimeout)

	absentCtx, absentName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-iso-absent-"+uniqueSuffix(), "iso-absent")

	textConnected := teamStartMessage + " connected isolation marker"
	textAbsent := teamStartMessage + " absent isolation marker"
	streamConnected := startTeamSend(t, connCtx, sutHostURL, sutEnvName, connName, textConnected)
	streamAbsent := startTeamSend(t, absentCtx, sutHostURL, sutEnvName, absentName, textAbsent)

	chConnected := drainTeamStreamAsync(streamConnected)
	chAbsent := drainTeamStreamAsync(streamAbsent)
	var eventsConnected, eventsAbsent []*game.ChatEvent
	for eventsConnected == nil || eventsAbsent == nil {
		select {
		case r := <-chConnected:
			if r.err != nil {
				t.Fatalf("connected session stream: %v", r.err)
			}
			eventsConnected = r.events
		case r := <-chAbsent:
			if r.err != nil {
				t.Fatalf("absent session stream: %v", r.err)
			}
			eventsAbsent = r.events
		case <-time.After(wsReadTimeout):
			t.Fatal("concurrent team turns did not both quiesce within the read window")
		}
	}
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, connName, eventsConnected)
	assertTeamStreamWellFormed(t, absentName, eventsAbsent)

	connectedResults := teamTurnToolResults(teamTurnsForMember(eventsConnected, "player")[0])
	if len(connectedResults) == 0 || connectedResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("connected session tool results = %+v, want a SUCCEEDED saolei_init", connectedResults)
	}
	absentResults := teamTurnToolResults(teamTurnsForMember(eventsAbsent, "player")[0])
	if len(absentResults) == 0 || absentResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("absent session tool results = %+v, want a FAILED saolei_init", absentResults)
	}
	connectedTurn := groupTeamMemberTurns(eventsConnected)[0]
	absentTurn := groupTeamMemberTurns(eventsAbsent)[0]
	if connectedTurn.turnID == absentTurn.turnID {
		t.Errorf("both sessions report turn_id %q — turn identity leaked across sessions", connectedTurn.turnID)
	}

	// Each history carries only its own user marker (no cross-session bleed).
	for i, entry := range listTeamMessages(t, connCtx, sutHostURL, sutEnvName, connName) {
		if text := agentV2MessageText(entry.GetMessage()); text == textAbsent {
			t.Errorf("connected session history[%d] carries the absent session's marker", i)
		}
	}
	foundAbsent := false
	for _, entry := range listTeamMessages(t, absentCtx, sutHostURL, sutEnvName, absentName) {
		if text := agentV2MessageText(entry.GetMessage()); text == textAbsent {
			foundAbsent = true
		}
	}
	if !foundAbsent {
		t.Error("absent session history lost its own user marker")
	}
}

// TestAgentV2TeamGameConversationStreamIndependentOfFlow covers US2 场景 3/8
// from the conversation side: the test plays the desktop over its own /api/v2
// flow connection, the Send stream is closed mid-turn by the client, and the
// turn still runs to completion — the flow stream keeps delivering operations
// and the merged history records the full chain (流与编排解耦, team-api.md
// §3.3).
func TestAgentV2TeamGameConversationStreamIndependentOfFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-independent-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "independent")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardWinPNG},
	}, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	// Read until the player's first tool-call block starts, then drop the
	// conversation stream the way a closed browser tab would.
	for {
		event := nextTeamEvent(t, stream.Scanner)
		if start := event.GetBlockStart(); start != nil && start.GetType() == game.BlockType_BLOCK_TYPE_TOOL_CALL && event.GetMember() == "player" {
			stream.Close()
			break
		}
	}

	// The turn completes anyway: poll the history for the settled terminal
	// tool result (List 回填 is the dropped stream's recovery path). The 062
	// conclusion ends the player turn at the terminal operate result
	// (specs/062-team-game-end-handoff/spec.md FR-001), so the settled
	// tool-call block — not a summary text — is the terminal anchor.
	entries := waitTeamQuiescence(t, ctx, sutHostURL, sutEnvName, sessionName, func(entries []*game.TeamMessage) bool {
		for _, entry := range entries {
			if entry.GetMember() != "player" {
				continue
			}
			for _, block := range entry.GetMessage().GetBlocks() {
				if call := block.GetToolCall(); call != nil && strings.Contains(call.GetResult(), agentV2WonStatusContains) {
					return true
				}
			}
		}
		return false
	})
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	if len(entries) == 0 {
		t.Fatal("the dropped stream's turn never reached the merged history")
	}

	// The flow stream survived the conversation-stream drop: the bridge still
	// answers a new probe on the same connection.
	sendFlowProbe(t, flow, sessionID)
	if frame := readFlowTeamFrame(t, flow, 10*time.Second); len(frame.GetFlowParts().GetParts()) == 0 {
		t.Fatalf("post-drop probe reply = %+v, want a status echo (flow stream unaffected)", frame)
	}
}

// TestAgentV2TeamGameActiveMemberTransitions covers quickstart V7-1 and the
// single merged active-member value
// (specs/060-agent-v2-team-optimize/contracts/team-api.md §1):
// GetTeam.active_member names the in-flight driving member while a turn runs
// and the activation (the next input's owner) at rest. The planner window is
// pinned on the controllable team-planner-wait turn (4s), the player window
// holds the game chain's dispatch receipts until the value is read, and the
// static windows read the value after materialization, after Cancel, and
// after the chain's last turn.
func TestAgentV2TeamGameActiveMemberTransitions(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-active-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "active")

	// 物化静止: no drive ran yet, so the activation is the planner.
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "planner" {
		t.Fatalf("active_member after materialization = %q, want \"planner\" (初始 activation)", got)
	}

	// planner 回合在途: team-planner-wait holds its turn open for 4s, so the
	// read right after its turn_start observes the driving member.
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamWaitMessage)
	for {
		event := nextTeamEvent(t, stream.Scanner)
		if event.GetTurnStart() == nil {
			continue
		}
		if event.GetMember() != "planner" {
			t.Fatalf("wait turn member = %v, want planner", event.GetMember())
		}
		break
	}
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "planner" {
		t.Errorf("active_member with the planner turn in flight = %q, want \"planner\"", got)
	}

	// Cancel terminates the turn and pauses auto-continuation; the next
	// input's owner must not change (contract §1).
	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Fatalf("cancel status = %d (body: %s), want 200", status, body)
	}
	events := waitTeamStream(t, drainTeamStreamAsync(stream), "canceled wait stream")
	if last := events[len(events)-1].GetTurnEnd(); last == nil || last.GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("wait turn terminal after cancel = %+v, want turn_end{CANCELED}", last)
	}
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "planner" {
		t.Errorf("active_member after cancel = %q, want the unchanged \"planner\" activation", got)
	}

	// player 回合在途: the next Send resumes the loop and plays the opening
	// chain against this test's flow half. Each dispatch proves the player
	// turn is in flight, so the read before the receipt is deterministic.
	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	ch := drainTeamStreamAsync(stream2)

	script := teamFlowScript{
		// Game 1 seeds the compatible in-progress board and answers the first
		// cell op with the win board; game 2 opens on the already-won board
		// (an init-terminal game, so the chain rests after its player turn).
		initBoards: [][]byte{saoleiBoardCompatWinPNG, saoleiBoardWinPNG},
		stepBoards: [][]byte{saoleiBoardWinPNG},
	}
	initIdx, stepIdx := 0, 0
	for initIdx < len(script.initBoards) || stepIdx < len(script.stepBoards) {
		frame := readFlowTeamFrame(t, flow, wsReadTimeout)
		for _, part := range frame.GetFlowParts().GetParts() {
			switch {
			case part.GetKeyboardPress() != nil:
				if initIdx >= len(script.initBoards) {
					t.Fatalf("unexpected keyboard dispatch after %d init replies", initIdx)
				}
				if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "player" {
					t.Fatalf("active_member with the player game turn in flight = %q, want \"player\"", got)
				}
				replyFlowReceipt(t, flow, sessionID, part.GetKeyboardPress().GetToolId(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, script.initBoards[initIdx])
				initIdx++
			case part.GetMouseMoveAndClick() != nil:
				if stepIdx >= len(script.stepBoards) {
					t.Fatalf("unexpected cell dispatch after %d step replies", stepIdx)
				}
				if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "player" {
					t.Fatalf("active_member with the player game turn in flight = %q, want \"player\"", got)
				}
				replyFlowReceipt(t, flow, sessionID, part.GetMouseMoveAndClick().GetToolId(), game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, script.stepBoards[stepIdx])
				stepIdx++
			}
		}
	}
	events2 := waitTeamStream(t, ch, "active transition stream")
	assertTeamStreamWellFormed(t, sessionName, events2)
	turns := groupTeamMemberTurns(events2)
	if len(turns) != 4 || turns[2].member != "planner" {
		t.Fatalf("chain turns = %v, want 4 with the planner review third (the active value crossed the review phase)", turns)
	}

	// 静止: after the review the structural continuation handed the next
	// input back to the player, which the merged value reflects.
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "player" {
		t.Errorf("active_member after the chain settled = %q, want \"player\" (the structural continuation's activation)", got)
	}
}
