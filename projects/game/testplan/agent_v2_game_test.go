// Package testplan contains the agent_v2 team game large tests: the saolei
// game loop end to end over the team stream with the deterministic fake-llm
// chain and either the deployed fake-desktop executor (the won topology) or
// the test's own flow connection. Cases are grouped by tested concern (won
// chain on the executor, terminal won + review continuation, terminal lost +
// review stop, per-handoff game-stats announcements across games, the
// prompt-side roster/snapshot increments, desktop-absent, multi-session
// isolation, stream independence), one test per concern —
// style/large_test.md §测试组织. The disconnect branch needs the drop deploy
// topology and lives in its own binary, agent_v2_game_disconnect_test.go.
package testplan

import (
	"fmt"
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
	// continue strategy. The review entry is gated on the saolei
	// announcement's result line — its keywords ("本局游戏结束：胜利") must
	// match the review drive's LAST user message, the announcement the
	// system member sends before the drain (team_planner.yaml
	// team-planner-review-continue;
	// specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §3),
	// so this turn occurring at all proves terminal-unit continuity AND the
	// announcement reaching the planner's model input.
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
	// The win board at init: the terminal init result concludes the player
	// turn (specs/062-team-game-end-handoff/spec.md FR-002 ①), so the
	// scripted operate batch is never requested — one F2 reply completes the
	// chain.
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
	if len(connectedResults) != 1 || connectedResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("connected session tool results = %+v, want the single SUCCEEDED saolei_init (the terminal init concludes the turn)", connectedResults)
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
	// Game 2 opened on the already-won board: the terminal init result
	// concludes the turn (specs/062-team-game-end-handoff/spec.md FR-002 ①)
	// before the scripted operate batch (agent-v2-saolei-init-operate), so
	// the turn carries exactly the one init result and no second model step.
	game2 := teamTurnToolResults(turns[3])
	if len(game2) != 1 || game2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 2 tool results = %+v, want the single SUCCEEDED init (the terminal init concludes the turn)", game2)
	}
	if !strings.Contains(game2[0].GetResult(), agentV2WonStatusContains) {
		t.Errorf("game 2 init result = %q, want the win board recognized at init", game2[0].GetResult())
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[3])
	assertSingleModelStep(t, turns[3])

	// 静止: after the review the structural continuation handed the next
	// input back to the player, which the merged value reflects.
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "player" {
		t.Errorf("active_member after the chain settled = %q, want \"player\" (the structural continuation's activation)", got)
	}
}

// TestAgentV2TeamGameStatsAnnouncedPerHandoff drives two terminal games in
// one session over the test's own desktop half and asserts the US1
// announcement face (specs/065-agent-v2-team-refine/spec.md SC-001): game 1
// is a won game whose operate batch stops on its FIRST dispatch (the win
// board answers the first click), game 2 is a lost game whose batch lands
// BOTH operations (click then flag) before the losing receipt — the
// multi-operation comparison. Each handoff contributes exactly one
// `member="saolei"` merged entry whose body is the gameStatsText template
// (specs/065-agent-v2-team-refine/data-model.md §3) carrying the numbers the
// flow script actually served; the planner review fixtures are anchored on
// the stats template key line, so the two review turns firing (continue,
// then stop) is the "the announcement reached the planner's review input"
// evidence; and the player's next drives consume the announcements (sender
// "saolei" view entries + live member_view frames). The proto team face
// still carries only the two materialized members.
func TestAgentV2TeamGameStatsAnnouncedPerHandoff(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-stats-chain-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "team-stats-chain")

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	// Game 1 (won): the compatible in-progress 9×9 board at init, the win
	// board on the first click → one successful dispatch. Game 2 (lost): the
	// fresh 16×16 board at init, the same playing board on the click and the
	// loss board on the flag → both dispatches succeed before the terminal
	// receipt.
	counts := new(teamFlowScriptCounts)
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardCompatWinPNG, saoleiBoardInitPNG},
		stepBoards: [][]byte{saoleiBoardWinPNG, saoleiBoardInitPNG, saoleiBoardLossPNG},
		counts:     counts,
	}, wsReadTimeout)

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)
	assertTeamStreamMessagesMatchList(t, ctx, sutHostURL, sutEnvName, sessionName, events)

	turns := groupTeamMemberTurns(events)
	wantMembers := []string{"planner", "player", "planner", "player", "planner", "player"}
	if len(turns) != len(wantMembers) {
		t.Fatalf("member turns = %d, want %d (opening, won game, continue review, lost game, stop review, stop ack)", len(turns), len(wantMembers))
	}
	for i, want := range wantMembers {
		if turns[i].member != want {
			t.Fatalf("turn %d member = %v, want %v", i, turns[i].member, want)
		}
	}

	// Game 1: init + the terminal win receipt; the batch stopped at its
	// first dispatch, so the turn carries exactly the init and operate
	// results.
	game1 := teamTurnToolResults(turns[1])
	if len(game1) != 2 || game1[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || game1[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 1 tool results = %+v, want init + operate SUCCEEDED", game1)
	}
	if !strings.Contains(game1[0].GetResult(), agentV2ProgStatusContains) || !strings.Contains(game1[0].GetResult(), agentV2WonBoardContains) || !strings.Contains(game1[1].GetResult(), agentV2WonStatusContains) {
		t.Errorf("game 1 results = %q / %q, want a playing 9×9 init and the won status", game1[0].GetResult(), game1[1].GetResult())
	}
	if status := teamTurnEndStatus(turns[1]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 1 turn ended %v, want COMPLETED", status)
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[1])

	// Review 1 (continue): the fixture keyword is the announcement's result
	// line, so this turn firing proves the announcement rode the review's
	// model input (specs/065-agent-v2-team-refine/contracts/
	// game-stats-broadcast.md §5).
	if _, text := teamTurnBlocks(turns[2]); text != teamPlannerReviewContinueText {
		t.Errorf("review 1 text = %q, want %q (stats-anchored review fixture)", text, teamPlannerReviewContinueText)
	}

	// Game 2: init + the two-operation loss receipt.
	game2 := teamTurnToolResults(turns[3])
	if len(game2) != 2 || game2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || game2[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 2 tool results = %+v, want init + operate SUCCEEDED", game2)
	}
	if !strings.Contains(game2[0].GetResult(), agentV2ProgStatusContains) || !strings.Contains(game2[0].GetResult(), agentV2ProgBoardContains) || !strings.Contains(game2[1].GetResult(), agentV2LostStatusContains) {
		t.Errorf("game 2 results = %q / %q, want a playing 16×16 init and the lost status", game2[0].GetResult(), game2[1].GetResult())
	}
	assertTerminalTurnEndsWithToolBlock(t, turns[3])

	if _, text := teamTurnBlocks(turns[4]); text != teamPlannerReviewStopText {
		t.Errorf("review 2 text = %q, want %q", text, teamPlannerReviewStopText)
	}
	reviewResults := teamTurnToolResults(turns[4])
	if len(reviewResults) != 1 || reviewResults[0].GetResult() != teamMemoryAddedResult {
		t.Errorf("review 2 tool results = %+v, want the single memory add", reviewResults)
	}
	if _, text := teamTurnBlocks(turns[5]); text != teamPlayerResumeStopText {
		t.Errorf("stop acknowledgement = %q, want %q", text, teamPlayerResumeStopText)
	}

	// The desktop served one F2 reply per game and exactly three successful
	// cell receipts: game 1 stopped on its first, game 2 landed both batch
	// operations before the loss. The completed script already proves no
	// extra dispatch was answered; the counts pair the announced totals with
	// what the desktop actually served (SC-001).
	if counts.initServed != 2 || counts.stepServed != 3 {
		t.Errorf("flow receipts served = %d init / %d step, want 2 / 3", counts.initServed, counts.stepServed)
	}

	// Merged sequence: exactly one saolei announcement per handoff, in game
	// order, each the template with the game's dispatched numbers.
	merged := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	saoleiEntries := teamMessagesForMember(merged, agentV2SaoleiMember)
	wantStats := []string{
		agentV2GameStatsText("胜利", 1, 1, 0, 0),
		agentV2GameStatsText("失败", 2, 1, 1, 0),
	}
	if len(saoleiEntries) != len(wantStats) {
		t.Fatalf("merged saolei entries = %d, want %d (one per handoff)", len(saoleiEntries), len(wantStats))
	}
	for i, want := range wantStats {
		entry := saoleiEntries[i]
		if got := agentV2MessageText(entry.GetMessage()); got != want {
			t.Errorf("saolei entry[%d] = %q, want %q", i, got, want)
		}
		if role := entry.GetMessage().GetRole(); role != game.Role_ROLE_AGENT {
			t.Errorf("saolei entry[%d] role = %v, want AGENT", i, role)
		}
		if blocks := entry.GetMessage().GetBlocks(); len(blocks) != 1 || blocks[0].GetText() == nil {
			t.Errorf("saolei entry[%d] blocks = %+v, want one text block", i, blocks)
		}
	}

	// The announcement is fanned out at the handoff: the first saolei frame
	// precedes the first review turn's turn_start.
	announceAt := firstTeamFrameIndex(events, func(event *game.ChatEvent) bool {
		frame := event.GetTeamMessage()
		return frame != nil && frame.GetMember() == agentV2SaoleiMember
	})
	reviewAt := firstTeamFrameIndex(events, func(event *game.ChatEvent) bool {
		return event.GetTurnStart() != nil && event.GetTurnId() == turns[2].turnID
	})
	if announceAt < 0 || reviewAt < 0 || announceAt > reviewAt {
		t.Errorf("announcement frame at %d, review turn_start at %d, want the announcement first", announceAt, reviewAt)
	}

	// Planner view: the review drives consumed both announcements as relay
	// inputs, and the first one's live frame arrived with its review turn.
	plannerSaolei := memberViewEntriesForSender(listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "planner"), agentV2SaoleiMember)
	if len(plannerSaolei) != len(wantStats) {
		t.Fatalf("planner view saolei entries = %d, want %d", len(plannerSaolei), len(wantStats))
	}
	for i, want := range wantStats {
		if got := agentV2MessageText(plannerSaolei[i].GetMessage()); got != agentV2SaoleiRelayText(want) {
			t.Errorf("planner view saolei entry[%d] = %q, want %q", i, got, agentV2SaoleiRelayText(want))
		}
	}
	assertTeamMemberViewLiveAt(t, ctx, sutHostURL, sutEnvName, sessionName, events, "planner", agentV2SaoleiMember, turns[2].turnID)

	// Player view: the game-2 drive consumed game 1's announcement and the
	// stop-ack drive consumed game 2's — the next structural drive input
	// carrying the broadcast (FR-004).
	playerSaolei := memberViewEntriesForSender(listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "player"), agentV2SaoleiMember)
	if len(playerSaolei) != len(wantStats) {
		t.Fatalf("player view saolei entries = %d, want %d", len(playerSaolei), len(wantStats))
	}
	for i, want := range wantStats {
		if got := agentV2MessageText(playerSaolei[i].GetMessage()); got != agentV2SaoleiRelayText(want) {
			t.Errorf("player view saolei entry[%d] = %q, want %q", i, got, agentV2SaoleiRelayText(want))
		}
	}
	assertTeamMemberViewLiveAt(t, ctx, sutHostURL, sutEnvName, sessionName, events, "player", agentV2SaoleiMember, turns[3].turnID)

	// The proto team face excludes the system member.
	assertTeamProtoRosterExcludesSaolei(t, getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName))
	assertTerminalHistoriesNoAbortTraces(t, ctx, sutHostURL, sutEnvName, sessionName)
}

// TestAgentV2TeamGameStatsPromptFaces materializes a team on a session
// seeded with 12 memories and asserts the prompt-side increments of the
// feature: the shared team section carries the saolei system member's roster
// line and the input-side-only caveat
// (specs/065-agent-v2-team-refine/contracts/team-member-source.md §3/§4 —
// the same strings the team plugin unit tests pin), and the planner's fixed
// memory snapshot injects the 10 most recently updated entries newest-first
// (specs/065-agent-v2-team-refine/spec.md FR-007; data-model.md §1.5), the
// two oldest entries truncated away.
func TestAgentV2TeamGameStatsPromptFaces(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)
	sessionID := "team-stats-prompt-" + uniqueSuffix()
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)
	playerPreset, plannerPreset := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-stats-prompt", "stats prompt")

	// Seed 12 memories BEFORE materialization: the planner prefetches the
	// snapshot at its setup, so this is what fixes the injected entry set.
	// The creates are spaced past the service's millisecond update_time
	// resolution so the recency order is strictly deterministic.
	var contentsNewestFirst []string
	for i := 1; i <= 12; i++ {
		content := fmt.Sprintf("T015 快照夹具 %02d", i)
		createMemory(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, fmt.Sprintf("snapshot-m%02d", i), content)
		contentsNewestFirst = append([]string{content}, contentsNewestFirst...)
		time.Sleep(3 * time.Millisecond)
	}
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, playerPreset.GetName(), plannerPreset.GetName(), "", "")

	// The team section lands on every member: both prompts carry the saolei
	// roster line and the input-side-only caveat.
	for _, member := range []string{"player", "planner"} {
		prompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, member).GetSystemPrompt()
		for _, want := range []string{agentV2TeamSectionSaoleiRosterLine, agentV2TeamSectionInputSideLine} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s system_prompt lacks %q:\n%s", member, want, prompt)
			}
		}
	}

	// The planner snapshot truncates to the 10 most recent entries, newest
	// first; the two oldest are gone.
	plannerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt()
	got := memorySnapshotEntries(plannerPrompt)
	want := contentsNewestFirst[:10]
	if len(got) != len(want) {
		t.Fatalf("planner snapshot entries = %d, want %d (the 10 most recent of 12):\n%s", len(got), len(want), plannerPrompt)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("snapshot entry[%d] = %q, want %q (update-time descending)", i, got[i], want[i])
		}
	}
	for _, truncated := range contentsNewestFirst[10:] {
		if strings.Contains(plannerPrompt, truncated) {
			t.Errorf("planner system_prompt carries the truncated oldest entry %q", truncated)
		}
	}

	// The snapshot section is planner-only: the player prompt has none.
	playerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player").GetSystemPrompt()
	if strings.Contains(playerPrompt, "长期记忆：") {
		t.Errorf("player system_prompt carries a memory snapshot, want none:\n%s", playerPrompt)
	}
}
