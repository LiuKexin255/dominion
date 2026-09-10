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
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentV2TeamGameWonChainOnExecutor drives the full won-scenario chain on
// the desktop-e2e-won session (the executor bound by deploy_agent_v2.yaml):
// the user Send drives the planner opening, the structural continuation
// drives the player, saolei_init recognizes the executor's win board, and the
// operate batch stops pre-dispatch on game_won — the terminal-reject branch
// proves the final state gates further cell operations before any dispatch.
// The merged history backfills the settled chain by tool_id.
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

	turns := groupTeamMemberTurns(events)
	if len(turns) != 2 {
		t.Fatalf("member turns = %d, want 2 (planner opening + player game)", len(turns))
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

	// Chain shape: init → operate, both SUCCEEDED with the won board text.
	results := teamTurnToolResults(playerTurn)
	if len(results) != 2 {
		t.Fatalf("tool_result count = %d, want 2 (saolei_init + saolei_operate)", len(results))
	}
	init, operate := results[0], results[1]
	if init.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_init status = %v, want SUCCEEDED (the desktop-e2e-won executor is connected)", init.GetStatus())
	}
	if !strings.Contains(init.GetResult(), agentV2WonInitContains) || !strings.Contains(init.GetResult(), agentV2WonStatusContains) || !strings.Contains(init.GetResult(), agentV2WonBoardContains) {
		t.Errorf("saolei_init result = %q, want the recognized win board (%q / %q / %q)",
			init.GetResult(), agentV2WonInitContains, agentV2WonStatusContains, agentV2WonBoardContains)
	}
	if operate.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("saolei_operate status = %v, want SUCCEEDED (a game-rules rejection is a normal result)", operate.GetStatus())
	}
	if !strings.Contains(operate.GetResult(), agentV2WonRejectContains) || !strings.Contains(operate.GetResult(), agentV2WonStatusContains) {
		t.Errorf("saolei_operate result = %q, want the game_won stop + status line", operate.GetResult())
	}
	if _, text := teamTurnBlocks(playerTurn); text != agentV2WonSummaryText {
		t.Errorf("terminal text = %q, want %q", text, agentV2WonSummaryText)
	}

	// Backfill: the player's merge entries carry two settled tool-call blocks
	// whose results equal the streamed tool_result texts.
	entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	var toolBlocks []*game.ToolCallBlock
	for _, entry := range entries {
		for _, block := range entry.GetMessage().GetBlocks() {
			if call := block.GetToolCall(); call != nil {
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
			t.Errorf("history tool block %d result = %q, want the streamed text %q", i, call.GetResult(), wantResults[i])
		}
	}
	// The provider mints a distinct call id per response (the request-derived
	// fake identity), so the two history blocks carry distinct ids that the
	// streamed tool_result frames settled.
	if toolBlocks[0].GetToolId() == "" || toolBlocks[1].GetToolId() == "" || toolBlocks[0].GetToolId() == toolBlocks[1].GetToolId() {
		t.Errorf("history tool ids = %q / %q, want two distinct non-empty call ids", toolBlocks[0].GetToolId(), toolBlocks[1].GetToolId())
	}
}

// TestAgentV2TeamGameTerminalWonAndReviewContinues covers US2 场景 4 with the
// continue behavior (quickstart.md V4): a full game terminates won on the
// operate receipt (the test's own desktop half seeds an in-progress board and
// answers the first cell dispatch with the win board), the gameEnded fact
// drives the planner review, and the review's next-game instruction
// structurally drives the player into a second game WITHOUT any further user
// input. The single Send stream covers all four member turns until the team
// rests; the planner view shows the player's verbatim process relay.
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
	// returns the win board, and the batch stops at the terminal status.
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
	if _, text := teamTurnBlocks(turns[1]); text != agentV2WonSummaryText {
		t.Errorf("game 1 terminal text = %q, want %q", text, agentV2WonSummaryText)
	}

	// Review: the planner consumes the player's raw process and emits the
	// continue strategy.
	if _, text := teamTurnBlocks(turns[2]); text != teamPlannerReviewContinueText {
		t.Errorf("review text = %q, want %q", text, teamPlannerReviewContinueText)
	}

	// Game 2 opened without any user input: the second init (the win board
	// already terminal → the operate batch rejects pre-dispatch).
	game2 := teamTurnToolResults(turns[3])
	if len(game2) != 2 || game2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || game2[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 2 tool results = %+v, want init + operate SUCCEEDED", game2)
	}
	if !strings.Contains(game2[0].GetResult(), agentV2WonStatusContains) {
		t.Errorf("game 2 init result = %q, want the won board recognized at init", game2[0].GetResult())
	}
	if !strings.Contains(game2[1].GetResult(), agentV2WonRejectContains) {
		t.Errorf("game 2 operate result = %q, want the pre-dispatch game_won stop %q", game2[1].GetResult(), agentV2WonRejectContains)
	}

	// No user input after the first Send: exactly one USER merge entry.
	userEntries := teamMessagesForMember(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName), "user")
	if len(userEntries) != 1 || agentV2MessageText(userEntries[0].GetMessage()) != teamStartMessage {
		t.Fatalf("USER merge entries = %d, want exactly the single Send (结构性续驱无需用户触发)", len(userEntries))
	}

	// The planner view carries the player's raw process as a sender-annotated
	// relay (FR-008: verbatim tool result body, no truncation), and the
	// player view carries the review relay.
	plannerView := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "planner")
	sawPlayerRelay := false
	for _, entry := range plannerView {
		if entry.GetSender() != "player" {
			continue
		}
		text := agentV2MessageText(entry.GetMessage())
		if strings.Contains(text, agentV2WonStatusContains) && strings.Contains(text, "<player-tool-call>") {
			sawPlayerRelay = true
		}
	}
	if !sawPlayerRelay {
		t.Error("planner view has no relayed player tool result carrying the terminal status (FR-008)")
	}
	playerView := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	sawReviewRelay := false
	for _, entry := range playerView {
		if entry.GetSender() == "planner" && strings.Contains(agentV2MessageText(entry.GetMessage()), teamPlannerReviewContinueText) {
			sawReviewRelay = true
		}
	}
	if !sawReviewRelay {
		t.Error("player view has no relayed review message after the game-end drive")
	}
}

// TestAgentV2TeamGameTerminalLostAndReviewStops covers US2 场景 4 with the
// stop behavior (quickstart.md V4: "不开局"): the operate receipt is the loss
// board, the gameEnded fact drives the planner review WITHOUT a next-game
// instruction, and the structurally driven player consumes it and opens no
// new game — the stream then ends at the static point with no other drive.
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
	// loses the game.
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
	if _, text := teamTurnBlocks(turns[1]); text != agentV2LostSummaryText {
		t.Errorf("lost terminal text = %q, want %q", text, agentV2LostSummaryText)
	}
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
	// Total tool results: 2 — no second init was ever dispatched.
	var toolResults int
	for _, turn := range turns {
		toolResults += len(teamTurnToolResults(turn))
	}
	if toolResults != 2 {
		t.Errorf("tool results = %d, want 2 (the loss review must not open another game)", toolResults)
	}
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

	// The turn completes anyway: poll the history for the terminal summary
	// (List 回填 is the dropped stream's recovery path).
	entries := waitTeamQuiescence(t, ctx, sutHostURL, sutEnvName, sessionName, func(entries []*game.TeamMessage) bool {
		for _, entry := range entries {
			if entry.GetMember() == "player" && strings.Contains(agentV2MessageText(entry.GetMessage()), agentV2WonSummaryText) {
				return true
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
