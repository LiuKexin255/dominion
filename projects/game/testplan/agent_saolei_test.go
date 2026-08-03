// Package testplan contains the saolei MCP large-test suite.
//
// agent_saolei_test.go validates the deployed agent's saolei MCP path
// (specs/025-desktop-image-state-refine) end-to-end on the saolei TEAM graph
// (specs/031-team-template-mode): the team's player agent — the only holder
// of the saolei MCP tools (FR-010/FR-028) — drives the
// model→tool_call→loopback-MCP→OperationBridge→desktop-WS chain, and the
// test "plays the desktop" — it reads the operation FlowParts the player
// dispatches (KeyboardPressPart{F2} for saolei_init,
// MouseMoveAndClickPart for saolei_click) and replies with a FlowResultPart
// (control channel, spec 025 FR-023/FR-024) carrying a real Minesweeper
// screenshot so the agent's @dominion/game-saolei-board recognition engine
// decodes the board.
//
// The team turn (one user input = one graph invoke, D10) routes through the
// per-session TurnLoop exactly like the pre-team single agent; each test
// sets up the team stack via setupTeamSession (session → saolei TeamProfile
// → CreateTeam) before connecting — CreateTeam MUST precede Connect (no
// lazy creation, FR-033).
//
// Coverage (spec 025 FR-012..FR-018, FR-022; spec 027 FR-012..FR-015,
// FR-021..023; spec 028 FR-006/FR-012, SC-004):
//   - TestAgentSaoleiTextBoardFlow: init→click→click on a recognizable
//     in-progress board; each tool returns a TEXT board (no image block) and
//     the screenshot stays on the control channel; every result carries
//     `game status: playing` (spec 027 FR-012/FR-014).
//   - TestAgentSaoleiIllegalMovePreDispatchReject: a click on an already-
//     revealed cell is rejected BEFORE dispatch (no operation FlowPart reaches
//     the desktop) with a stable reason code the model can act on.
//   - TestAgentSaoleiWonGameStatusAndTerminalReject: a 9×9 win screenshot
//     (saolei_10.png, counter `000`) seeds a terminal-won state — init
//     surfaces `game status: won` and a following cell op is rejected
//     pre-dispatch as `game_won` (spec 027 FR-021..023) carrying the won
//     status line.
//   - TestAgentSaoleiLostGameStatusAndTerminalReject: a 16×16 loss screenshot
//     (saolei_5.png) seeds a terminal-lost state — init surfaces
//     `game status: lost` and a following cell op is rejected pre-dispatch as
//     `game_over` (existing terminal-loss) carrying the lost status line.
//   - TestAgentSaoleiOverFlagBoardStaysPlaying: a 9×9 over-flag screenshot
//     (saolei_9.png — grid all-revealed, 11 flags, counter `-01`) seeds a
//     NON-terminal state — init surfaces `game status: playing` (NOT `won`)
//     and a following cell op is NOT rejected as `game_won` (spec 028 — the
//     counter-informed isWin eliminates the grid-only false-positive win).
//   - TestAgentSaoleiRemainToolNoDispatch: the read-only saolei_remain tool
//     (spec 029 US2) returns the remain grid for a recognized board while
//     dispatching ZERO operations to the desktop (FR-007). Turn 1 seeds the
//     state via saolei_init (saolei_1.png → 16×16); turn 2 drives
//     saolei_remain via a distinct keyword and asserts no FlowPart operation
//     is dispatched and the result carries `saolei_remain → computed`,
//     `game status: playing`, and `board size 16*16`.
//
// Organised by MODULE per style/large_test.md (not by scenario/spec-id); it
// reuses the shared helpers in helpers_test.go and the saolei fixtures in
// saolei_fixtures_test.go.
package testplan

import (
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentSaoleiTextBoardFlow drives a full saolei init→click→click
// sequence through the deployed agent against a RECOGNIZABLE Minesweeper
// board and verifies the 025 text-board return contract end-to-end
// (specs/025-desktop-image-state-refine spec.md FR-012/FR-013/FR-022;
// contracts/saolei-mcp-contract.md §3):
//
//  1. A user turn matching the fake-LLM "saolei-start" keyword triggers a
//     saolei_init tool_call.
//  2. The agent executes saolei_init via the loopback MCP server, which
//     dispatches a KeyboardPressPart{F2} through OperationBridge to the WS
//     as a flow_parts frame (control channel).
//  3. The test (playing desktop) reads that operation FlowPart and replies
//     with a FlowResultPart (control channel, spec 025 FR-023/FR-024)
//     carrying a real Minesweeper screenshot (saoleiBoardInitPNG — 16×16
//     all-INITIAL). The agent's @dominion/game-saolei-board engine
//     recognizes the board and seeds the per-session state.
//  4. fake-LLM returns saolei_click{3,4} (legal on an all-INITIAL board);
//     the agent dispatches a MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE}
//     at the cell centre. The test replies with the same screenshot (monotonic
//     update — no revealed cells to regress), and the agent returns the
//     updated TEXT board.
//  5. fake-LLM returns saolei_click{5,6} (also legal); same dispatch/reply
//     cycle. fake-LLM then returns the final text response.
//  6. ListMessages returns Messages whose content.parts include a tool_call
//     and a tool_result MessagePart for each saolei tool invocation. Every
//     saolei tool_result carries a TEXT board (FR-012) and NO screenshot
//     (FR-022 — the screenshot is consumed for recognition only and stays on
//     the control channel as FlowResultPart.screenshot, FR-026). Per spec 023
//     D12 the saolei (MCP) tool_result status is neutral (UNSPECIFIED).
//  7. No operation FlowPart (KeyboardPress / MouseMoveAndClick) appears in
//     Message.content — operations are control-only (FR-005 / FR-004).
func TestAgentSaoleiTextBoardFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The four
	// saolei tools are surfaced via the loopback MCP client, each backed by
	// the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper screenshot (16×16 all-INITIAL) used for
	// every dispatch reply. Recognition is monotonic-safe across identical
	// frames (no revealed cells to regress — saolei-board README §状态校验),
	// so init + two updates against the same PNG all succeed.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// when: a user turn whose text matches the "saolei-start" keyword,
	// making fake-LLM return the first saolei_init tool_call.
	sendText(t, conn, sessionID, "please start saolei game")

	// then (1): saolei_init dispatches an F2 KeyboardPressPart FlowPart. The
	// test plays desktop: read the flow_parts frame, assert F2, reply with a
	// FlowResultPart carrying the recognizable screenshot (spec 025 FR-023).
	initFrame := readOperationFrame(t, conn)
	kp := frameKeyboardPress(initFrame)
	if kp == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	if kp.GetKey() != game.KeyboardKey_KEYBOARD_KEY_F2 {
		t.Errorf("saolei_init key = %v, want KEYBOARD_KEY_F2 (spec 025 FR-019 retained)", kp.GetKey())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then (2): saolei_click(3,4) dispatches a MouseMoveAndClickPart at the
	// cell centre with LEFT_CLICK + WINDOW_MESSAGE (spec 025 FR-019 retained;
	// specs/018-saolei-mcp/contracts/proto-operation-contract.md §3). The
	// all-INITIAL board makes (3,4) a legal reveal, so it dispatches.
	click1Frame := readOperationFrame(t, conn)
	mmc1 := frameMouseMoveAndClick(click1Frame)
	if mmc1 == nil {
		t.Fatalf("saolei_click(3,4) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			click1Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc1, saoleiClick1CenterX, saoleiClick1CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_click(3,4) dispatch mismatch: %v", err)
	}
	respondToOperationWithScreenshot(t, conn, sessionID, click1Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cell at (3,4) revealed", screenshot)

	// then (3): saolei_click(5,6) dispatches back-to-back (spec 023 FR-021
	// retained — the four tools are callable back-to-back with no intervening
	// step; 025 only adds pre-dispatch validation, which (5,6) on an
	// all-INITIAL board passes).
	click2Frame := readOperationFrame(t, conn)
	mmc2 := frameMouseMoveAndClick(click2Frame)
	if mmc2 == nil {
		t.Fatalf("saolei_click(5,6) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			click2Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc2, saoleiClick2CenterX, saoleiClick2CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("saolei_click(5,6) dispatch mismatch: %v", err)
	}
	respondToOperationWithScreenshot(t, conn, sessionID, click2Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "cell at (5,6) revealed", screenshot)

	// then (4): the model emits the final text response, proving the whole
	// init→click→click chain completed and the connection remains usable.
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame — saolei init→click→click chain did not complete")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}

	// then (5): ListMessages returns the conversation history. Each saolei
	// tool invocation surfaces as a tool_call MessagePart (name + args_json)
	// plus its tool_result MessagePart (spec 023 FR-002/FR-006/FR-009).
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_init") {
		t.Errorf("ListMessages did not surface a saolei_init tool_call MessagePart (spec 023 FR-006)")
	}
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_click") {
		t.Errorf("ListMessages did not surface a saolei_click tool_call MessagePart (spec 023 FR-006)")
	}
	// tool_call args_json carries the model's arguments verbatim (research.md D3).
	if got := firstToolCallArgsJSON(lmr.GetMessages(), "saolei_click"); got == "" {
		t.Errorf("ListMessages saolei_click tool_call args_json is empty (spec 023 FR-002 / research.md D3)")
	}

	// then (6): spec 025 FR-012/FR-022 — every saolei tool_result carries a
	// TEXT board and NO screenshot (the screenshot is consumed for recognition
	// only and stays on the control channel as FlowResultPart.screenshot). The
	// tool_result message is the MCP-returned text ("new game started" for
	// init, "<tool> at (x,y) → dispatched" for legal cell ops), and the
	// ToolResultPart.screenshot MUST be nil.
	saoleiResultCount := 0
	for _, m := range lmr.GetMessages() {
		for _, p := range m.GetContent().GetParts() {
			tr := p.GetToolResult()
			if tr == nil {
				continue
			}
			// Only saolei tool_results flow through this profile (no native
			// mouse tools declared); collect every tool_result and assert the
			// 025 text-only contract on each.
			saoleiResultCount++
			if tr.GetScreenshot() != nil {
				t.Errorf("saolei tool_result carries a screenshot — spec 025 FR-022 forbids model-facing image content (the screenshot MUST stay on the control channel)")
			}
			msg := tr.GetMessage()
			// FR-012: the result is a recognized TEXT board. init returns
			// "new game started"; a legal cell op returns "→ dispatched". A
			// regression that returns "unable to recognize" would fail both
			// checks.
			if !strings.Contains(msg, "new game started") &&
				!strings.Contains(msg, "dispatched") {
				t.Errorf("saolei tool_result message = %q, want to contain \"new game started\" or \"dispatched\" (spec 025 FR-012 text-board return)", msg)
			}
			// FR-012/FR-014 (specs/027-chat-bubble-game-state): every saolei
			// tool_result on a recognized in-progress board carries the line
			// `game status: playing`. This flow uses saolei_1.png (16×16
			// all-INITIAL) for init + two updates, so every recognized state
			// is in-progress and every result must surface the playing
			// status (specs/027-chat-bubble-game-state/contracts/saolei-mcp-
			// status-contract.md §2/§3).
			if !strings.Contains(msg, "game status: playing") {
				t.Errorf("saolei tool_result message = %q, want to contain \"game status: playing\" (spec 027 FR-012/FR-014 — in-progress board surfaces the playing status)", msg)
			}
		}
	}
	if saoleiResultCount == 0 {
		t.Fatal("ListMessages returned no saolei tool_result MessageParts — the init→click→click chain produced no recognized tool results")
	}

	// then (7): per spec 023 D12, saolei is an MCP tool — the reconstructed
	// tool_result status is neutral (TOOL_RESULT_STATUS_UNSPECIFIED), NEVER
	// FAILED. (025 does not change this: the rejection outcome is conveyed
	// by the result text, not by flipping the status — the model can act on
	// a rejected move as a normal tool result.)
	for i, m := range lmr.GetMessages() {
		for _, status := range messageToolResultStatuses(m) {
			if status != game.ToolResultStatus_TOOL_RESULT_STATUS_UNSPECIFIED {
				t.Errorf("message[%d]: saolei tool_result status = %v, want UNSPECIFIED (neutral) per spec 023 D12", i, status)
			}
		}
	}

	// then (8): no operation FlowPart appears in Message.content. Operations
	// are control-only (spec 023 FR-004/FR-005).
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// TestAgentSaoleiIllegalMovePreDispatchReject verifies spec 025 FR-014/FR-015c:
// a saolei_click on an already-revealed cell is rejected BEFORE dispatch with
// a stable reason code, and the desktop receives no operation for it. The
// test seeds the session with a recognizable board whose cell (3,4) is a
// revealed number "1" (saoleiBoardRevealedPNG / saolei_2.png golden), then
// lets fake-LLM chain saolei_init → saolei_click{3,4} (the existing fixture
// chaining). The agent validates the click against the recognized state,
// rejects it as cell_already_revealed, and returns a text outcome — WITHOUT
// dispatching a MouseMoveAndClickPart.
//
// The "no dispatch" assertion is the key: the bounded drain below fails the
// test if ANY operation FlowPart (carrying a tool_id) arrives before the
// rejection tool_result — that would mean an illegal move reached the desktop.
func TestAgentSaoleiIllegalMovePreDispatchReject(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-reject-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper board (9×9, partially revealed) whose cell
	// (3,4) is the number "1" (already revealed) — see
	// projects/game/pkg/saolei-board/testdata/saolei_2.golden.txt. A left-click
	// on a revealed number is rejected as cell_already_revealed (FR-015c).
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardRevealedPNG)

	// given: a user turn triggers saolei_init. The agent dispatches F2; the
	// test replies with the recognizable screenshot so the agent seeds the
	// recognized state from the 9×9 partially-revealed board.
	sendText(t, conn, sessionID, "please start saolei game")

	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed", screenshot)

	// when: fake-LLM chains saolei_init → saolei_click{3,4} (the existing
	// fixture chaining in sample_saolei_tools.yaml saolei-init-followup-click).
	// The agent validates (3,4) against the recognized 9×9 board and MUST
	// reject it pre-dispatch.
	//
	// Drain frames until the rejection tool_result arrives, asserting NO
	// operation FlowPart (keyboard/mouse with a tool_id) appears in between —
	// a rejection that still dispatched would be an FR-014 violation. The
	// bounded loop tolerates the interleaved display frames (saolei_init
	// tool_result, saolei_click tool_call) that precede the rejection.
	const drainLimit = 8
	var rejectedMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_click(3,4) dispatched an operation FlowPart (tool_id=%q) — an illegal move on a revealed cell MUST be rejected BEFORE dispatch (spec 025 FR-014/FR-015c)",
				opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			if strings.Contains(tr.GetMessage(), "cell_already_revealed") {
				rejectedMessage = tr.GetMessage()
				break
			}
		}
	}
	if rejectedMessage == "" {
		t.Fatalf("did not receive a cell_already_revealed rejection within %d frames — saolei_click(3,4) on a revealed cell was not rejected pre-dispatch", drainLimit)
	}

	// then: the rejection outcome carries the stable reason code (FR-016) the
	// model can act on, plus the current board and the valid coordinate range.
	if !strings.Contains(rejectedMessage, "rejected: cell_already_revealed") {
		t.Errorf("rejection message = %q, want to contain \"rejected: cell_already_revealed\" (spec 025 FR-016)", rejectedMessage)
	}
	// The current board is returned so the model can pick a different cell.
	// The 9×8 board renders numbers/initials/flags; assert at least the
	// "valid range" guidance is present (FR-016: rejection includes the valid
	// coordinate range).
	if !strings.Contains(rejectedMessage, "valid range:") {
		t.Errorf("rejection message = %q, want to contain \"valid range:\" (spec 025 FR-016 — rejection includes the valid coordinate range)", rejectedMessage)
	}
}

// TestAgentSaoleiWonGameStatusAndTerminalReject verifies spec 027 FR-012..015
// (game-status line) + FR-021..023 (post-win game_won terminal rejection)
// end-to-end on the deployed agent with the REAL recognition engine. The test
// seeds the session with a real 9×9 win screenshot (saoleiBoardWinPNG /
// saolei_10.png — every cell is a revealed number or FLAG; no INITIAL/
// HIT_MINE/MINE/UNKNOWN), so @dominion/game-saolei-board's isWin(state)
// returns true (specs/027-chat-bubble-game-state/data-model.md §1). It then
// asserts:
//
//  1. The saolei_init result text contains `game status: won` (FR-012/FR-013
//     — a recognized win is surfaced on the operation that produced the
//     terminal board).
//  2. A following saolei_click(x,y) is rejected pre-dispatch as `game_won`
//     (FR-021..023) — NO operation FlowPart reaches the desktop (the win is
//     terminal, symmetric with how a loss rejects further ops as game_over).
//  3. The game_won rejection body carries `game status: won` (FR-023).
//  4. The rejection follows the existing 025 FR-016 contract: body contains
//     the current text board and the valid coordinate range.
//
// Mirrors TestAgentSaoleiIllegalMovePreDispatchReject's drain-and-assert-no-
// operation pattern: the bounded drain tolerates interleaved display frames
// (saolei_init tool_result, saolei_click tool_call) that precede the
// rejection tool_result.
func TestAgentSaoleiWonGameStatusAndTerminalReject(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-win-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The four
	// saolei tools are surfaced via the loopback MCP client, each backed by
	// the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A real 9×9 win Minesweeper screenshot (all cells revealed/flagged,
	// no INITIAL/HIT_MINE/MINE/UNKNOWN) — see
	// projects/game/pkg/saolei-board/testdata/saolei_10.golden.txt. The
	// agent's recognition engine decodes it; isWin(state) returns true
	// (specs/027-chat-bubble-game-state/data-model.md §1), so gameStatus
	// returns "won" and any subsequent cell op is terminal-blocked as
	// game_won (FR-021..023).
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardWinPNG)

	// when: a user turn triggers saolei_init (sample_saolei_start.yaml
	// keyword "start saolei"). The agent dispatches F2; the test replies
	// with the win screenshot so the agent seeds the recognized state from
	// the 9×9 win board.
	sendText(t, conn, sessionID, "please start saolei game")

	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then: fake-LLM chains saolei_init → saolei_click{3,4} (the existing
	// sample_saolei_tools.yaml saolei-init-followup-click chaining). The
	// agent validates (3,4) against the recognized win board: isWin(state)
	// is true ⇒ validateMove returns game_won BEFORE any dispatch
	// (FR-021..023, specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5).
	//
	// Drain frames until the rejection tool_result arrives, asserting NO
	// operation FlowPart (keyboard/mouse with a tool_id) appears in between
	// — a rejection that still dispatched would be an FR-021 violation. The
	// bounded loop also collects the saolei_init tool_result so the won
	// status line can be asserted on the operation that produced the
	// terminal board (FR-012/FR-013).
	const drainLimit = 8
	var initMessage string
	var rejectedMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_click(3,4) on a won board dispatched an operation FlowPart (tool_id=%q) — a cell op after a recognized win MUST be rejected BEFORE dispatch as game_won (spec 027 FR-021..023)",
				opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "rejected: game_won") {
				rejectedMessage = msg
				break
			}
		}
	}

	// then (1): the saolei_init result carries `game status: won`
	// (FR-012/FR-013 — the operation that produced the terminal-won board
	// surfaces the won status).
	if initMessage == "" {
		t.Fatalf("did not receive a saolei_init tool_result within %d frames — the init→recognition chain did not produce a recognized init result", drainLimit)
	}
	if !strings.Contains(initMessage, "game status: won") {
		t.Errorf("saolei_init result message = %q, want to contain \"game status: won\" (spec 027 FR-012/FR-013 — a recognized win surfaces on the operation that produced the terminal board)", initMessage)
	}

	// then (2): the post-win cell op was rejected pre-dispatch as game_won.
	if rejectedMessage == "" {
		t.Fatalf("did not receive a game_won rejection within %d frames — saolei_click(3,4) on a won board was not rejected pre-dispatch", drainLimit)
	}
	if !strings.Contains(rejectedMessage, "rejected: game_won") {
		t.Errorf("rejection message = %q, want to contain \"rejected: game_won\" (spec 027 FR-021..023)", rejectedMessage)
	}

	// then (3): the rejection body carries `game status: won` (FR-023).
	if !strings.Contains(rejectedMessage, "game status: won") {
		t.Errorf("rejection message = %q, want to contain \"game status: won\" (spec 027 FR-023 — game_won rejection carries the won status line)", rejectedMessage)
	}

	// then (4): the rejection follows the existing 025 FR-016 contract
	// (restated by specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5 / FR-023): body contains the
	// current text board and the valid coordinate range.
	if !strings.Contains(rejectedMessage, "valid range:") {
		t.Errorf("rejection message = %q, want to contain \"valid range:\" (spec 025 FR-016 / 027 FR-023 — rejection includes the valid coordinate range)", rejectedMessage)
	}
}

// TestAgentSaoleiLostGameStatusAndTerminalReject verifies spec 027 FR-012..015
// (game-status line) on the loss branch + the existing terminal-loss
// rejection (spec 025 FR-015b `game_over`, restated by specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5)
// end-to-end on the deployed agent with the REAL recognition engine. The test
// seeds the session with a real 16×16 loss screenshot (saoleiBoardLossPNG /
// saolei_5.png — contains HIT_MINE "X" and MINE "M" cells; see
// projects/game/pkg/saolei-board/testdata/saolei_5.golden.txt), so the
// agent's existing isTerminalState(state) loss signal fires. It then asserts:
//
//  1. The saolei_init result text contains `game status: lost` (FR-012/FR-013
//     — a recognized loss is surfaced on the operation that produced the
//     terminal board).
//  2. A following saolei_click(x,y) is rejected pre-dispatch as `game_over`
//     (existing terminal-loss behaviour — specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5) and NO
//     operation FlowPart reaches the desktop.
//  3. The game_over rejection body now carries `game status: lost`
//     (FR-012..015 — every rejection with a recognized state carries the
//     status line; specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §2).
//  4. The rejection follows the existing 025 FR-016 contract: body contains
//     the current text board and the valid coordinate range.
//
// Mirrors TestAgentSaoleiIllegalMovePreDispatchReject's drain-and-assert-no-
// operation pattern.
func TestAgentSaoleiLostGameStatusAndTerminalReject(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-loss-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The four
	// saolei tools are surfaced via the loopback MCP client, each backed by
	// the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A real 16×16 loss Minesweeper screenshot (contains HIT_MINE "X" and
	// MINE "M" cells — see
	// projects/game/pkg/saolei-board/testdata/saolei_5.golden.txt). The
	// agent's recognition engine decodes it; isTerminalState(state) fires
	// (the existing loss signal — HIT_MINE/MINE presence), so gameStatus
	// returns "lost" and any subsequent cell op is terminal-blocked as
	// game_over (existing terminal-loss, specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5).
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardLossPNG)

	// when: a user turn triggers saolei_init (sample_saolei_start.yaml
	// keyword "start saolei"). The agent dispatches F2; the test replies
	// with the loss screenshot so the agent seeds the recognized state
	// from the 16×16 loss board.
	sendText(t, conn, sessionID, "please start saolei game")

	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then: fake-LLM chains saolei_init → saolei_click{3,4} (the existing
	// sample_saolei_tools.yaml saolei-init-followup-click chaining). The
	// agent validates (3,4) against the recognized loss board:
	// isTerminalState(state) is true ⇒ validateMove returns game_over
	// BEFORE any dispatch (existing terminal-loss, specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5).
	//
	// Drain frames until the rejection tool_result arrives, asserting NO
	// operation FlowPart appears in between. The bounded loop also
	// collects the saolei_init tool_result so the lost status line can be
	// asserted on the operation that produced the terminal board.
	const drainLimit = 8
	var initMessage string
	var rejectedMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_click(3,4) on a lost board dispatched an operation FlowPart (tool_id=%q) — a cell op after a recognized loss MUST be rejected BEFORE dispatch as game_over (spec 025 FR-015b / specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5)",
				opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "rejected: game_over") {
				rejectedMessage = msg
				break
			}
		}
	}

	// then (1): the saolei_init result carries `game status: lost`
	// (FR-012/FR-013 — the operation that produced the terminal-lost board
	// surfaces the lost status).
	if initMessage == "" {
		t.Fatalf("did not receive a saolei_init tool_result within %d frames — the init→recognition chain did not produce a recognized init result", drainLimit)
	}
	if !strings.Contains(initMessage, "game status: lost") {
		t.Errorf("saolei_init result message = %q, want to contain \"game status: lost\" (spec 027 FR-012/FR-013 — a recognized loss surfaces on the operation that produced the terminal board)", initMessage)
	}

	// then (2): the post-loss cell op was rejected pre-dispatch as game_over.
	if rejectedMessage == "" {
		t.Fatalf("did not receive a game_over rejection within %d frames — saolei_click(3,4) on a lost board was not rejected pre-dispatch", drainLimit)
	}
	if !strings.Contains(rejectedMessage, "rejected: game_over") {
		t.Errorf("rejection message = %q, want to contain \"rejected: game_over\" (spec 025 FR-015b / specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5 — existing terminal-loss)", rejectedMessage)
	}

	// then (3): the rejection body carries `game status: lost` (FR-012..015
	// — every rejection with a recognized state carries the status line;
	// specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §2).
	if !strings.Contains(rejectedMessage, "game status: lost") {
		t.Errorf("rejection message = %q, want to contain \"game status: lost\" (spec 027 FR-012..015 — game_over rejection carries the lost status line)", rejectedMessage)
	}

	// then (4): the rejection follows the existing 025 FR-016 contract:
	// body contains the current text board and the valid coordinate range.
	if !strings.Contains(rejectedMessage, "valid range:") {
		t.Errorf("rejection message = %q, want to contain \"valid range:\" (spec 025 FR-016 — rejection includes the valid coordinate range)", rejectedMessage)
	}
}

// TestAgentSaoleiOverFlagBoardStaysPlaying verifies spec 028 FR-006/FR-012 +
// SC-004 (the counter-informed win fix) end-to-end on the deployed agent with
// the REAL recognition engine. The test seeds the session with a real 9×9
// over-flag screenshot (saoleiBoardOverFlagPNG / saolei_9.png — grid fully
// revealed/flagged, 11 flags, top-left mine counter reads `-01` because 11
// flags exceed the 10 mines). Under the PRE-028 grid-only `isWin` rule this
// board was a FALSE-POSITIVE win (grid all-revealed ⇒ isWin true ⇒
// `game status: won` + `game_won` terminal rejection on the next cell op).
// Under the 028 counter-informed rule, `isWin(state)` additionally requires
// `state.mineCounter === {decoded: true, value: 0}`
// (specs/028-saolei-win-counter-fix/contracts/saolei-mcp-win-contract.md),
// so this over-flag board ⇒ `playing`. The test asserts:
//
//  1. The saolei_init result carries `game status: playing` and does NOT
//     carry `game status: won` (FR-012 — the false-positive win is
//     eliminated; the [027] text contract is preserved, only the `won`
//     decision is more accurate).
//  2. A following saolei_click(3,4) is NOT rejected as `game_won`. Because
//     isWin(state) now returns false, validateMove (projects/game/agent/src/
//     mcp/saolei/saolei-mcp.ts) does NOT short-circuit on the win branch and
//     falls through to the cell-specific rule: cell (3,4) on the over-flag
//     board is a revealed number "0" (see
//     projects/game/pkg/saolei-board/testdata/saolei_9.png grid), so the
//     click is rejected as `cell_already_revealed` — NOT `game_won`. This
//     fall-through is the direct evidence that the counter cross-check took
//     effect: under the grid-only rule the same click would have been
//     rejected as `game_won` BEFORE reaching the cell rule.
//  3. The cell rejection carries `game status: playing` (the over-flag board
//     is non-terminal, so the status line the rejection body re-derives from
//     the recognized state is `playing`, not `won`).
//  4. NO operation FlowPart is dispatched for the cell op — the
//     `cell_already_revealed` rejection is pre-dispatch (symmetric with the
//     won/loss terminal rejections; an illegal move never reaches the
//     desktop).
//
// Note on the "DISPATCHED" wording in tasks.md T013: on a fully-revealed
// over-flag board every cell is either a revealed number or a FLAG, so there
// is no legal `saolei_click` target (the only dispatchable cell op would be
// `saolei_flag` toggling an existing FLAG). The shared fake-LLM fixture
// (sample_saolei_tools.yaml `saolei-init-followup-click`) chains
// saolei_init → saolei_click{3,4} and is shared by all four existing saolei
// tests, so it cannot be specialised to emit saolei_flag here without
// breaking them. The `cell_already_revealed` fall-through (assertion 2) is
// the equivalent invariant: it proves the cell op was NOT rejected as
// `game_won`, which is the bug-fix acceptance criterion (SC-004 — "the agent
// large test no longer surfaces `game status: won` nor rejects subsequent
// cell operations as `game_won`").
func TestAgentSaoleiOverFlagBoardStaysPlaying(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-overflag-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The four
	// saolei tools are surfaced via the loopback MCP client, each backed by
	// the real @dominion/game-saolei-board recognition engine (which now
	// also decodes the top-left mine counter — spec 028 US1).
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A real 9×9 over-flag Minesweeper screenshot (grid fully revealed/
	// flagged, 11 flags, counter `-01` — the over-flagged false-positive
	// fixture added by spec 028; see
	// projects/game/pkg/saolei-board/testdata/saolei_9.png). The agent's
	// recognition engine decodes both the grid (all-revealed) AND the mine
	// counter (value -1); the counter-informed isWin(state) returns false
	// (FR-006), so gameStatus returns "playing" (FR-012) and the board is
	// NOT terminal.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardOverFlagPNG)

	// when: a user turn triggers saolei_init (sample_saolei_start.yaml
	// keyword "start saolei"). The agent dispatches F2; the test replies
	// with the over-flag screenshot so the agent seeds the recognized state
	// (grid + decoded counter) from the 9×9 over-flag board.
	sendText(t, conn, sessionID, "please start saolei game")

	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// then: fake-LLM chains saolei_init → saolei_click{3,4} (the shared
	// sample_saolei_tools.yaml saolei-init-followup-click chaining). On the
	// over-flag board cell (3,4) is a revealed number "0", so validateMove
	// (with the counter-informed isWin returning false) falls through to the
	// cell-specific rule and rejects the click as cell_already_revealed —
	// NOT as game_won. The bounded drain also collects the saolei_init
	// tool_result so the playing status line can be asserted on the
	// operation that produced the recognized board.
	const drainLimit = 8
	var initMessage string
	var rejectedMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		// A cell_already_revealed rejection is pre-dispatch: NO operation
		// FlowPart may reach the desktop for the cell op (symmetric with the
		// won/loss terminal rejections — an illegal move never dispatches).
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_click(3,4) on the over-flag board dispatched an operation FlowPart (tool_id=%q) — a cell_already_revealed rejection MUST be pre-dispatch (spec 025 FR-014)",
				opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "rejected: cell_already_revealed") {
				rejectedMessage = msg
				break
			}
		}
	}

	// then (1): the saolei_init result carries `game status: playing`
	// (FR-012 — the over-flag board is non-terminal: the counter-informed
	// isWin returned false because the decoded counter is -1, not 0).
	if initMessage == "" {
		t.Fatalf("did not receive a saolei_init tool_result within %d frames — the init→recognition chain did not produce a recognized init result", drainLimit)
	}
	if !strings.Contains(initMessage, "game status: playing") {
		t.Errorf("saolei_init result message = %q, want to contain \"game status: playing\" (spec 028 FR-012 — an over-flag board whose counter ≠ 000 is NOT a win; the counter-informed isWin returned false)", initMessage)
	}
	// The false positive the fix eliminates: the grid-only rule would have
	// surfaced `game status: won` here. Asserting its ABSENCE pins the fix.
	if strings.Contains(initMessage, "game status: won") {
		t.Errorf("saolei_init result message = %q — over-flag board (counter -01) was reported WON; the grid-only false positive is still present (spec 028 FR-006/SC-002 — counter ≠ 000 ⇒ not a win)", initMessage)
	}

	// then (2): the post-init cell op was rejected as cell_already_revealed,
	// NOT as game_won. This fall-through is the direct evidence the counter
	// cross-check took effect: validateMove's isWin branch (which would
	// return game_won under the grid-only rule) did NOT fire.
	if rejectedMessage == "" {
		t.Fatalf("did not receive a cell_already_revealed rejection within %d frames — saolei_click(3,4) on the over-flag board was not rejected pre-dispatch", drainLimit)
	}
	if !strings.Contains(rejectedMessage, "rejected: cell_already_revealed") {
		t.Errorf("rejection message = %q, want to contain \"rejected: cell_already_revealed\" (spec 025 FR-015c — click on a revealed cell)", rejectedMessage)
	}
	if strings.Contains(rejectedMessage, "rejected: game_won") {
		t.Errorf("rejection message = %q — over-flag board cell op was rejected as game_won; the counter-informed isWin did NOT take effect (spec 028 SC-004 — a non-won board MUST NOT reject cell ops as game_won)", rejectedMessage)
	}

	// then (3): the rejection body carries `game status: playing`
	// (FR-012..015 — every rejection with a recognized state carries the
	// status line; the over-flag board's status is `playing`, not `won`).
	if !strings.Contains(rejectedMessage, "game status: playing") {
		t.Errorf("rejection message = %q, want to contain \"game status: playing\" (spec 028 FR-012 — over-flag board rejection carries the playing status, not won)", rejectedMessage)
	}
	if strings.Contains(rejectedMessage, "game status: won") {
		t.Errorf("rejection message = %q — over-flag board rejection carries `game status: won`; the counter-informed status is wrong (spec 028 SC-004)", rejectedMessage)
	}

	// then (4): the rejection follows the existing 025 FR-016 contract:
	// body contains the current text board and the valid coordinate range.
	if !strings.Contains(rejectedMessage, "valid range:") {
		t.Errorf("rejection message = %q, want to contain \"valid range:\" (spec 025 FR-016 — rejection includes the valid coordinate range)", rejectedMessage)
	}
}

// TestAgentSaoleiRemainToolNoDispatch verifies spec 029 US2 / FR-006..013
// (the read-only saolei_remain tool) end-to-end on the deployed agent with
// the REAL recognition engine. saolei_remain returns the remain grid for a
// recognized board while dispatching ZERO operations to the desktop
// (FR-007 — it is purely computational). The test drives two user turns:
//
//  1. Turn 1 — saolei_init (saolei_1.png → 16×16 board) seeds the
//     per-session recognized state. The shared fake-LLM fixture
//     (sample_saolei_tools.yaml) then chains init→saolei_click{3,4}→
//     saolei_click{5,6}→final text — the SAME chain every saolei test
//     uses — so the test "plays the desktop" through it; turn 1
//     completes and the recognized 16×16 state persists to turn 2.
//  2. Turn 2 — a user turn matching the "show remaining mines" keyword
//     (sample_saolei_remain.yaml) makes fake-LLM return a saolei_remain
//     tool_call. saolei_remain reads the seeded state and returns the
//     remain grid; the test asserts NO FlowPart operation is dispatched
//     for the call (the desktop receives nothing) and the result text
//     carries `saolei_remain → computed`, `game status: playing`, and
//     `board size 16*16`.
//
// Why two turns (a distinct second-turn keyword) instead of wiring
// saolei_remain into the shared init chain: MatchToolResult
// (fake-llm matcher.go) breaks ties by alphabetically-first Name, and the
// init→click chain (saolei-init-followup-click, match_result_contains=[])
// matches every saolei_init result. A saolei_remain follow-up on
// tool_name=saolei_init would either lose to it (click < remain) or, if
// named to sort first, hijack the four existing saolei tests' init flow.
// The all-INITIAL 16×16 init result is identical for this test and
// TestAgentSaoleiTextBoardFlow, so it cannot be disambiguated by content.
// A second-turn keyword (matcher.go Match branch) isolates saolei_remain:
// no other suite sends it, so existing suites are unaffected.
//
// The "no dispatch" assertion mirrors TestAgentSaoleiIllegalMovePreDispatchReject's
// drain-and-assert-no-operation pattern: the bounded drain tolerates the
// interleaved display frames (saolei_remain tool_call, its tool_result)
// around the remain call, failing if ANY operation FlowPart reaches the
// desktop.
func TestAgentSaoleiRemainToolNoDispatch(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-remain-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The
	// five saolei tools are surfaced via the loopback MCP client, each
	// backed by the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper screenshot (16×16 all-INITIAL) used for
	// turn 1's init + click replies. Recognition is monotonic-safe across
	// identical frames (no revealed cells to regress), so init + two
	// updates against the same PNG all succeed. The final recognized state
	// (16×16 all-INITIAL) persists to turn 2, where saolei_remain reads it.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// ── Turn 1: seed the recognized board state via saolei_init. ──────
	// A user turn whose text matches the "saolei-start" keyword makes
	// fake-LLM return the first saolei_init tool_call.
	sendText(t, conn, sessionID, "please start saolei game")

	// saolei_init dispatches an F2 KeyboardPressPart; reply with the
	// recognizable screenshot so the agent seeds the recognized state from
	// the 16×16 board.
	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)

	// The shared fixture (sample_saolei_tools.yaml) chains saolei_init →
	// saolei_click{3,4} → saolei_click{5,6} → final text. Play the desktop
	// through both clicks so turn 1 completes; the recognized state seeded
	// above persists across turns within the session.
	for _, step := range []struct{ cellX, cellY int32 }{
		{saoleiClick1X, saoleiClick1Y},
		{saoleiClick2X, saoleiClick2Y},
	} {
		clickFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(clickFrame) == nil {
			t.Fatalf("saolei_click(%d,%d) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
				step.cellX, step.cellY, clickFrame.GetFlowParts().GetParts())
		}
		respondToOperationWithScreenshot(t, conn, sessionID, clickFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", step.cellX, step.cellY), screenshot)
	}

	// Drain until turn 1's final text frame arrives — turn 1 is then
	// complete and turn 2 may be sent on the same session/connection.
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f) && strings.Contains(frameText(f), expectedSaoleiFinalText)
	})
	if textFrame == nil {
		t.Fatalf("turn 1 did not complete — did not receive the final text %q within the drain limit", expectedSaoleiFinalText)
	}

	// ── Turn 2: drive saolei_remain and assert zero dispatch. ────────
	// A user turn matching the "show remaining mines" keyword
	// (sample_saolei_remain.yaml) makes fake-LLM return a saolei_remain
	// tool_call. saolei_remain reads the seeded 16×16 state and returns
	// the remain grid.
	sendText(t, conn, sessionID, "please show remaining mines")

	// Drain frames around the saolei_remain call, asserting NO operation
	// FlowPart (keyboard/mouse carrying a tool_id) is dispatched —
	// saolei_remain is read-only and MUST NOT call OperationBridge.dispatch
	// (spec 029 FR-007). The bounded loop tolerates the interleaved display
	// frames (the saolei_remain tool_call, its tool_result) and collects
	// the remain tool_result so its body can be asserted.
	const drainLimit = 8
	var remainMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_remain dispatched an operation FlowPart (tool_id=%q) — saolei_remain MUST be read-only and dispatch nothing (spec 029 FR-007)", opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			if strings.Contains(tr.GetMessage(), "saolei_remain → computed") {
				remainMessage = tr.GetMessage()
				break
			}
		}
	}
	if remainMessage == "" {
		t.Fatalf("did not receive a saolei_remain tool_result within %d frames — the read-only remain query did not run", drainLimit)
	}

	// then: the remain result carries the computed outcome line, the
	// in-progress status (the all-INITIAL board is non-terminal), and the
	// 16×16 board size. The remain grid itself is all `-` on this board
	// (no revealed numbers yet — FR-009), which the unit tests cover
	// (saolei-mcp.test.ts); the large test pins the body shape + the
	// zero-dispatch invariant.
	if !strings.Contains(remainMessage, "saolei_remain → computed") {
		t.Errorf("remain message = %q, want to contain \"saolei_remain → computed\" (spec 029 FR-008 — the computed outcome line)", remainMessage)
	}
	if !strings.Contains(remainMessage, "game status: playing") {
		t.Errorf("remain message = %q, want to contain \"game status: playing\" (spec 029 FR-008 / specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §3)", remainMessage)
	}
	if !strings.Contains(remainMessage, "board size 16*16") {
		t.Errorf("remain message = %q, want to contain \"board size 16*16\" (the seeded board is saolei_1.png, 16×16)", remainMessage)
	}
}
