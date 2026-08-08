// Package testplan contains the saolei MCP large-test suite.
//
// agent_saolei_test.go validates the deployed agent's saolei MCP path
// (specs/025-desktop-image-state-refine) end-to-end on the saolei TEAM graph
// (specs/031-team-template-mode): the team's player agent — the only holder
// of the saolei MCP tools (FR-010/FR-028) — drives the
// model→tool_call→loopback-MCP→OperationBridge→desktop-WS chain, and the
// test "plays the desktop" — it reads the operation FlowParts the player
// dispatches (KeyboardPressPart{F2} for saolei_init,
// MouseMoveAndClickPart for the merged saolei_operate cell ops) and replies
// with a FlowResultPart (control channel, spec 025 FR-023/FR-024) carrying a
// real Minesweeper screenshot so the agent's @dominion/game-saolei-board
// recognition engine decodes the board.
//
// The cell tools are the spec 039 single dual-form `saolei_operate`
// (specs/039-planner-memory-calibration US1 — FR-001/FR-002): the fixture
// chain (sample_saolei_tools.yaml) returns a BATCH call
// (operations: [click{3,4}, click{5,6}]) after every saolei_init, so one
// tool invocation dispatches BOTH ops IN ORDER and returns ONE result. The
// failure triage (contract saolei-operate-contract.md §2) is exercised by
// seeding the recognized board via saolei_init with different screenshots:
// harmless no-op rejections (click a revealed cell) SKIP and the batch
// continues; structural rejections (out-of-bounds) and terminal boards
// (game_won/game_over) STOP the batch.
//
// Coverage:
//   - TestAgentSaoleiTextBoardFlow: init→operate batch on a recognizable
//     in-progress board — both ops dispatch IN ORDER (cells (3,4) then
//     (5,6)) from ONE call with ONE result; then a SECOND turn drives the
//     SINGLE form (type/x/y) and dispatches the SAME cell — proving the
//     dual forms are equivalent (single == length-1 batch, FR-001/FR-002).
//     Each tool returns a TEXT board (no image block) and the screenshot
//     stays on the control channel; every result carries `game status:
//     playing` (spec 027 FR-012/FR-014).
//   - TestAgentSaoleiOperateNoOpSkip: an operate batch whose ops target
//     revealed cells is SKIPPED (harmless no-op — no dispatch reaches the
//     desktop) and the single result reports `executed 0 ops, skipped 2
//     no-op ops` (FR-002 — the former "rejected: cell_already_revealed"
//     semantics became a skip in 039).
//   - TestAgentSaoleiOperateStructuralStop: an operate batch with an
//     out-of-bounds second op STOPS at op 2 — op 1 dispatches, op 2 never
//     reaches the desktop, and the result reports `stopped at op 2
//     (out_of_bounds)` (FR-002).
//   - TestAgentSaoleiWonGameStatusAndTerminalReject: a 9×9 win screenshot
//     (saolei_10.png, counter `000`) seeds a terminal-won state — init
//     surfaces `game status: won` and a following cell op stops the batch
//     as `stopped at op 1 (game_won)` (spec 027 FR-021..023) carrying the
//     won status line.
//   - TestAgentSaoleiLostGameStatusAndTerminalReject: a 16×16 loss screenshot
//     (saolei_5.png) seeds a terminal-lost state — init surfaces
//     `game status: lost` and a following cell op stops the batch as
//     `stopped at op 1 (game_over)` (existing terminal-loss).
//   - TestAgentSaoleiOverFlagBoardStaysPlaying: a 9×9 over-flag screenshot
//     (saolei_9.png — grid all-revealed, 11 flags, counter `-01`) seeds a
//     NON-terminal state — init surfaces `game status: playing` (NOT `won`)
//     and a following batch of cell ops is skipped as no-ops, never stopped
//     as `game_won` (spec 028 — the counter-informed isWin eliminates the
//     grid-only false-positive win).
//   - TestAgentSaoleiRemainToolNoDispatch: the read-only saolei_remain tool
//     (spec 029 US2) returns the remain grid for a recognized board while
//     dispatching ZERO operations to the desktop (FR-007).
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

// TestAgentSaoleiTextBoardFlow drives the saolei_operate DUAL-FORM contract
// end-to-end through the deployed agent against a RECOGNIZABLE Minesweeper
// board (specs/039-planner-memory-calibration US1 — FR-001/FR-002;
// contracts/saolei-operate-contract.md §1-3):
//
//  1. Turn 1 ("please start saolei game"): a saolei_init tool_call
//     dispatches F2; the test replies with the recognizable in-progress
//     board (saolei_1.png — 16×16 all-INITIAL). The fake-LLM chain
//     (sample_saolei_tools.yaml) then returns ONE saolei_operate BATCH call
//     (operations: [click{3,4}, click{5,6}]).
//  2. The batch dispatches op 1 (click 3,4) — the test reads the
//     MouseMoveAndClickPart, asserts the cell centre (136,248), replies;
//     then op 2 (click 5,6) — same cycle at (200,312). ORDER PRESERVED
//     (FR-001): op 2's dispatch arrives only after op 1's desktop reply.
//  3. ONE result returns for the whole call: `saolei_operate → executed 2
//     ops` + `game status: playing` + the final text board (FR-002 — single
//     return). The fixture terminates the loop with the final text.
//  4. Turn 2 ("please drive a saolei single operate"): the fake-LLM returns
//     a SINGLE-FORM saolei_operate call (type/x/y — sample_saolei_single_op.
//     yaml). It dispatches EXACTLY ONE operation at the SAME cell (3,4) as
//     turn 1's first op — the dual forms are equivalent (single == length-1
//     operations, FR-001/FR-002) — and returns `executed 1 ops`.
//  5. ListMessages returns the tool_call/tool_result MessageParts for both
//     forms; the batch args_json carries the operations array with both
//     ops, the single args_json carries type/x/y; every saolei tool_result
//     is a TEXT board (FR-012) with NO screenshot (FR-022) and neutral
//     status (spec 023 D12).
func TestAgentSaoleiTextBoardFlow(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	profileName := fmt.Sprintf("saolei-%s", uniqueSuffix())

	// given: a saolei-enabled profile. The model name is non-Anthropic so
	// ModelProviderCache routes to the OpenAI platform (fake-llm). The three
	// saolei tools are surfaced via the loopback MCP client, each backed by
	// the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWSTrace(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper screenshot (16×16 all-INITIAL) used for
	// every dispatch reply. Recognition is monotonic-safe across identical
	// frames (no revealed cells to regress — saolei-board README §状态校验),
	// so init + two updates against the same PNG all succeed.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// ── Turn 1: the BATCH form (operations array). ──────────────────────
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

	// then (2): the fixture chains ONE saolei_operate BATCH call whose ops
	// dispatch IN ORDER — op 1 (3,4) dispatches first (assert its centre),
	// the test replies, then op 2 (5,6) dispatches and is replied to. Both
	// dispatches carry LEFT_CLICK + WINDOW_MESSAGE (spec 025 FR-019
	// retained; specs/018-saolei-mcp/contracts/proto-operation-contract.md
	// §3) at the fixture's cell centres (saolei_fixtures_test.go).
	for _, step := range []struct {
		cellX, cellY     int32
		centerX, centerY int32
	}{
		{saoleiClick1X, saoleiClick1Y, saoleiClick1CenterX, saoleiClick1CenterY},
		{saoleiClick2X, saoleiClick2Y, saoleiClick2CenterX, saoleiClick2CenterY},
	} {
		opFrame := readOperationFrame(t, conn)
		mmc := frameMouseMoveAndClick(opFrame)
		if mmc == nil {
			t.Fatalf("saolei_operate op (%d,%d) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
				step.cellX, step.cellY, opFrame.GetFlowParts().GetParts())
		}
		if err := assertMouseMoveAndClick(mmc, step.centerX, step.centerY,
			game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
			t.Errorf("saolei_operate op (%d,%d) dispatch mismatch: %v", step.cellX, step.cellY, err)
		}
		respondToOperationWithScreenshot(t, conn, sessionID, opFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", step.cellX, step.cellY), screenshot)
	}

	// then (3): the SINGLE result for the batch arrives (FR-002 — one
	// return per saolei_operate call), reporting both ops executed, then the
	// fixture terminates the tool loop with the final text.
	batchResult := drainWSFrame(t, conn, frameHasToolResult)
	if batchResult == nil {
		t.Fatal("did not receive the saolei_operate tool_result after the two dispatch replies")
	}
	if !strings.Contains(frameToolResult(batchResult).GetMessage(), "saolei_operate → executed 2 ops") {
		t.Errorf("batch tool_result = %q, want to contain \"saolei_operate → executed 2 ops\" (FR-002 — one return for the whole batch)", frameToolResult(batchResult).GetMessage())
	}
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameHasText(f)
	})
	if textFrame == nil {
		t.Fatal("did not receive a final text frame — the saolei init→operate chain did not complete")
	}
	if !strings.Contains(frameText(textFrame), expectedSaoleiFinalText) {
		t.Errorf("final text = %q, want to contain %q", frameText(textFrame), expectedSaoleiFinalText)
	}

	// ── Turn 2: the SINGLE form (type/x/y) — dual-form equivalence. ─────
	// when: a user turn matching the "single operate" keyword
	// (sample_saolei_single_op.yaml) returns a single-form saolei_operate
	// call (type=click, x=3, y=4) against the still-recognized 16×16 board.
	sendText(t, conn, sessionID, "please drive a saolei single operate")

	// then: EXACTLY ONE dispatch at the SAME cell centre as turn 1's op 1
	// ((3,4) → 136,248) — the single form is equivalent to a length-1
	// operations batch (FR-001/FR-002).
	singleOpFrame := readOperationFrame(t, conn)
	singleMMC := frameMouseMoveAndClick(singleOpFrame)
	if singleMMC == nil {
		t.Fatalf("single-form saolei_operate did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			singleOpFrame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(singleMMC, saoleiClick1CenterX, saoleiClick1CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("single-form saolei_operate dispatch mismatch: %v", err)
	}
	respondToOperationWithScreenshot(t, conn, sessionID, singleOpFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
		fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)

	// No second dispatch may follow (the single form is exactly one op): the
	// next tool_result must be the `executed 1 ops` result with NO
	// intervening operation frame.
	singleResult := drainWSFrame(t, conn, frameHasToolResult)
	if singleResult == nil {
		t.Fatal("did not receive the single-form saolei_operate tool_result")
	}
	if !strings.Contains(frameToolResult(singleResult).GetMessage(), "saolei_operate → executed 1 ops") {
		t.Errorf("single-form tool_result = %q, want to contain \"saolei_operate → executed 1 ops\"", frameToolResult(singleResult).GetMessage())
	}
	// Drain the final text so the turn settles.
	textFrame = drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if textFrame == nil {
		t.Fatal("did not receive the final text frame for the single-form turn")
	}

	// then (4): ListMessages returns the conversation history. Each saolei
	// tool invocation surfaces as a tool_call MessagePart (name + args_json)
	// plus its tool_result MessagePart (spec 023 FR-002/FR-006/FR-009).
	lmr := listMessages(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, "player")
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_init") {
		t.Errorf("ListMessages did not surface a saolei_init tool_call MessagePart (spec 023 FR-006)")
	}
	if !messagesContainToolCall(lmr.GetMessages(), "saolei_operate") {
		t.Errorf("ListMessages did not surface a saolei_operate tool_call MessagePart (spec 023 FR-006)")
	}
	// tool_call args_json carries the model's arguments verbatim (research.md
	// D3): the batch form carries the operations array with both ops, the
	// single form carries type/x/y (FR-001 dual form).
	batchArgs := firstToolCallArgsJSON(lmr.GetMessages(), "saolei_operate")
	if !strings.Contains(batchArgs, `"operations"`) ||
		!strings.Contains(batchArgs, `"type":"click"`) ||
		!strings.Contains(batchArgs, `"x":3`) || !strings.Contains(batchArgs, `"y":4`) ||
		!strings.Contains(batchArgs, `"x":5`) || !strings.Contains(batchArgs, `"y":6`) {
		t.Errorf("saolei_operate batch args_json = %q, want the operations array [click(3,4), click(5,6)] (FR-001)", batchArgs)
	}

	// then (5): spec 025 FR-012/FR-022 — every saolei tool_result carries a
	// TEXT board and NO screenshot (the screenshot is consumed for
	// recognition only and stays on the control channel as
	// FlowResultPart.screenshot). The tool_result message is the MCP-returned
	// text ("new game started" for init, "saolei_operate → executed N ops"
	// for the operate calls), and the ToolResultPart.screenshot MUST be nil.
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
			// "new game started"; a legal operate call returns
			// "saolei_operate → executed ...". A regression that returns
			// "unable to recognize" would fail both checks.
			if !strings.Contains(msg, "new game started") &&
				!strings.Contains(msg, "saolei_operate →") {
				t.Errorf("saolei tool_result message = %q, want to contain \"new game started\" or \"saolei_operate →\" (spec 025 FR-012 text-board return)", msg)
			}
			// FR-012/FR-014 (specs/027-chat-bubble-game-state): every saolei
			// tool_result on a recognized in-progress board carries the line
			// `game status: playing`. This flow uses saolei_1.png (16×16
			// all-INITIAL) for init + both updates, so every recognized state
			// is in-progress and every result must surface the playing
			// status (specs/027-chat-bubble-game-state/contracts/saolei-mcp-
			// status-contract.md §2/§3).
			if !strings.Contains(msg, "game status: playing") {
				t.Errorf("saolei tool_result message = %q, want to contain \"game status: playing\" (spec 027 FR-012/FR-014 — in-progress board surfaces the playing status)", msg)
			}
		}
	}
	if saoleiResultCount == 0 {
		t.Fatal("ListMessages returned no saolei tool_result MessageParts — the init→operate chain produced no recognized tool results")
	}

	// then (6): per spec 023 D12, saolei is an MCP tool — the reconstructed
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

	// then (7): no operation FlowPart appears in Message.content. Operations
	// are control-only (spec 023 FR-004/FR-005).
	assertMessageContentDisplayOnly(t, lmr.GetMessages())
}

// TestAgentSaoleiOperateNoOpSkip verifies the harmless no-op triage of a
// saolei_operate batch (specs/039-planner-memory-calibration FR-002 —
// contract saolei-operate-contract.md §2): an operation targeting an
// already-revealed cell is a no-op (cell_already_revealed — HARMLESS_NOOP)
// and is SKIPPED — the batch continues and the desktop receives NO operation
// for it; the single result reports `executed 0 ops, skipped 2 no-op ops`.
// The test seeds the session with a recognizable board whose cells (3,4) and
// (5,6) are revealed numbers (saolei_2.png golden — 9×9 partially revealed),
// then lets fake-LLM chain saolei_init → the shared operate batch
// [click{3,4}, click{5,6}]. The former 025 rejection contract
// ("rejected: cell_already_revealed") became a SKIP in 039 — the no-op does
// not stop the batch.
func TestAgentSaoleiOperateNoOpSkip(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-noop-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper board (9×9, partially revealed) whose cells
	// (3,4) and (5,6) are revealed numbers (see
	// projects/game/pkg/saolei-board/testdata/saolei_2.golden.txt — row3
	// col3=1, row6 col5=0). A click on a revealed number is a harmless no-op
	// (cell_already_revealed — FR-002 skip, not reject).
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

	// when: fake-LLM chains saolei_init → the shared operate batch
	// [click{3,4}, click{5,6}] (sample_saolei_tools.yaml). Both ops target
	// revealed cells on the seeded board → both are harmless no-ops → SKIPPED
	// without dispatch; the batch completes and returns once.
	//
	// Drain frames until the operate tool_result arrives, asserting NO
	// operation FlowPart (keyboard/mouse with a tool_id) appears in between —
	// a skipped op must never reach the desktop (FR-002). The bounded loop
	// tolerates the interleaved display frames (saolei_init tool_result,
	// saolei_operate tool_call) that precede the result.
	const drainLimit = 8
	var operateMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("a no-op saolei_operate op dispatched an operation FlowPart (tool_id=%q) — harmless no-ops MUST be skipped without reaching the desktop (FR-002)", opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			if strings.Contains(tr.GetMessage(), "saolei_operate →") {
				operateMessage = tr.GetMessage()
				break
			}
		}
	}
	if operateMessage == "" {
		t.Fatalf("did not receive a saolei_operate tool_result within %d frames — the all-no-op batch did not complete", drainLimit)
	}

	// then: the single result reports the skip triage — `executed 0 ops,
	// skipped 2 no-op ops` — with the playing status line (the revealed
	// board is non-terminal) and NO rejection prefix (the 039 semantics:
	// harmless no-ops are skipped, not rejected).
	if !strings.Contains(operateMessage, "saolei_operate → executed 0 ops, skipped 2 no-op ops") {
		t.Errorf("operate result = %q, want to contain \"saolei_operate → executed 0 ops, skipped 2 no-op ops\" (FR-002 skip triage)", operateMessage)
	}
	if !strings.Contains(operateMessage, "game status: playing") {
		t.Errorf("operate result = %q, want to contain \"game status: playing\" (the revealed board is non-terminal)", operateMessage)
	}
	if strings.Contains(operateMessage, "rejected:") {
		t.Errorf("operate result = %q — harmless no-ops are SKIPPED in 039, not rejected (FR-002)", operateMessage)
	}
}

// TestAgentSaoleiOperateStructuralStop verifies the structural-stop triage of
// a saolei_operate batch (specs/039-planner-memory-calibration FR-002 —
// contract saolei-operate-contract.md §2): an out-of-bounds operation is a
// structural rejection (STRUCTURAL_REASONS) — the batch STOPS at that op,
// earlier successful ops take effect and the remaining ops are NOT executed;
// the single result reports `stopped at op K (reason)`.
//
// The test seeds a live 16×16 board (turn 1 init + the shared operate batch),
// then drives turn 2 with the "structural stop" keyword
// (sample_saolei_structural_stop.yaml): operations [click{3,4}, click{99,99}]
// — op 1 (3,4) is legal and dispatches (the test replies), op 2 (99,99) is
// out-of-bounds → the batch stops at op 2 with exactly ONE dispatch having
// reached the desktop.
func TestAgentSaoleiOperateStructuralStop(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-struct-%s", uniqueSuffix())

	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// ── Turn 1: seed the recognized 16×16 board via the shared init→operate
	// chain (both ops execute against the all-INITIAL board).
	sendText(t, conn, sessionID, "please start saolei game")
	initFrame := readOperationFrame(t, conn)
	if frameKeyboardPress(initFrame) == nil {
		t.Fatalf("saolei_init did not dispatch a KeyboardPressPart FlowPart; frame parts: %v",
			initFrame.GetFlowParts().GetParts())
	}
	respondToOperationWithScreenshot(t, conn, sessionID, initFrame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED, "F2 pressed, new game started", screenshot)
	for i := 0; i < 2; i++ {
		opFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(opFrame) == nil {
			t.Fatalf("turn 1: expected a saolei_operate dispatch, got: %v", opFrame.GetFlowParts().GetParts())
		}
		respondToOperationWithScreenshot(t, conn, sessionID, opFrame,
			game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
			fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)
	}
	textFrame := drainWSFrame(t, conn, func(f *game.TeamFrame) bool { return frameHasText(f) })
	if textFrame == nil {
		t.Fatal("turn 1 did not complete — no final text frame")
	}

	// ── Turn 2: the structural-stop batch. ──────────────────────────────
	// when: a user turn matching the "structural stop" keyword
	// (sample_saolei_structural_stop.yaml) returns saolei_operate with
	// operations [click{3,4}, click{99,99}].
	sendText(t, conn, sessionID, "please trigger a saolei structural stop")

	// then: op 1 (3,4) is legal on the seeded all-INITIAL board and
	// dispatches; the test replies.
	op1Frame := readOperationFrame(t, conn)
	mmc1 := frameMouseMoveAndClick(op1Frame)
	if mmc1 == nil {
		t.Fatalf("structural-stop batch op 1 did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
			op1Frame.GetFlowParts().GetParts())
	}
	if err := assertMouseMoveAndClick(mmc1, saoleiClick1CenterX, saoleiClick1CenterY,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK); err != nil {
		t.Errorf("structural-stop batch op 1 dispatch mismatch: %v", err)
	}
	respondToOperationWithScreenshot(t, conn, sessionID, op1Frame,
		game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED,
		fmt.Sprintf("cell at (%d,%d) revealed", saoleiClick1X, saoleiClick1Y), screenshot)

	// then: op 2 (99,99) is out-of-bounds → the batch STOPS at op 2 — the
	// desktop receives NO further dispatch and the single result reports the
	// stop with the reason (FR-002).
	const drainLimit = 8
	var stopMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("structural-stop batch dispatched a further operation FlowPart (tool_id=%q) after the out-of-bounds op — the batch MUST stop at op 2 (FR-002)", opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			if strings.Contains(tr.GetMessage(), "saolei_operate →") {
				stopMessage = tr.GetMessage()
				break
			}
		}
	}
	if stopMessage == "" {
		t.Fatalf("did not receive the structural-stop tool_result within %d frames", drainLimit)
	}
	if !strings.Contains(stopMessage, "saolei_operate → stopped at op 2 (out_of_bounds)") {
		t.Errorf("operate result = %q, want to contain \"saolei_operate → stopped at op 2 (out_of_bounds)\" (FR-002 structural stop)", stopMessage)
	}
}

// TestAgentSaoleiWonGameStatusAndTerminalReject verifies spec 027 FR-012..015
// (game-status line) + FR-021..023 (post-win terminal rejection) end-to-end
// on the deployed agent with the REAL recognition engine, under the 039
// saolei_operate triage (FR-002 — a terminal board stops the batch at the
// offending op). The test seeds the session with a real 9×9 win screenshot
// (saoleiBoardWinPNG / saolei_10.png — every cell is a revealed number or
// FLAG; no INITIAL/HIT_MINE/MINE/UNKNOWN), so @dominion/game-saolei-board's
// isWin(state) returns true (specs/027-chat-bubble-game-state/data-model.md
// §1). It then asserts:
//
//  1. The saolei_init result text contains `game status: won` (FR-012/FR-013
//     — a recognized win is surfaced on the operation that produced the
//     terminal board).
//  2. A following saolei_operate op is rejected BEFORE dispatch as
//     `game_won` — the batch stops at op 1 (`stopped at op 1 (game_won)`,
//     the 039 result shape replacing the former "rejected: game_won" body)
//     and NO operation FlowPart reaches the desktop.
//  3. The stop body carries `game status: won` (FR-023).
//
// Mirrors TestAgentSaoleiOperateNoOpSkip's drain-and-assert-no-operation
// pattern: the bounded drain tolerates interleaved display frames
// (saolei_init tool_result, saolei_operate tool_call) that precede the stop
// result.
func TestAgentSaoleiWonGameStatusAndTerminalReject(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-win-%s", uniqueSuffix())

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

	// then: fake-LLM chains saolei_init → the shared operate batch
	// [click{3,4}, click{5,6}] (sample_saolei_tools.yaml). On the seeded win
	// board op 1 is rejected as game_won BEFORE any dispatch — the batch
	// stops at op 1 (FR-002/FR-021..023,
	// specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5).
	//
	// Drain frames until the stop result arrives, asserting NO operation
	// FlowPart appears in between — a rejection that still dispatched would
	// be an FR-021 violation. The bounded loop also collects the saolei_init
	// tool_result so the won status line can be asserted on the operation
	// that produced the terminal board.
	const drainLimit = 8
	var initMessage string
	var stopMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_operate op on a won board dispatched an operation FlowPart (tool_id=%q) — a cell op after a recognized win MUST stop the batch BEFORE dispatch as game_won (spec 027 FR-021..023)", opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "stopped at op 1 (game_won)") {
				stopMessage = msg
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

	// then (2): the post-win cell op stopped the batch at op 1 as game_won.
	if stopMessage == "" {
		t.Fatalf("did not receive a game_won stop result within %d frames — the batch did not stop at op 1 on the won board", drainLimit)
	}
	if !strings.Contains(stopMessage, "saolei_operate → stopped at op 1 (game_won)") {
		t.Errorf("stop result = %q, want to contain \"saolei_operate → stopped at op 1 (game_won)\" (spec 027 FR-021..023 / 039 FR-002)", stopMessage)
	}

	// then (3): the stop body carries `game status: won` (FR-023).
	if !strings.Contains(stopMessage, "game status: won") {
		t.Errorf("stop result = %q, want to contain \"game status: won\" (spec 027 FR-023 — game_won stop carries the won status line)", stopMessage)
	}
}

// TestAgentSaoleiLostGameStatusAndTerminalReject verifies spec 027 FR-012..015
// (game-status line) on the loss branch + the existing terminal-loss
// rejection (spec 025 FR-015b `game_over`, restated by specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5)
// end-to-end on the deployed agent with the REAL recognition engine, under
// the 039 saolei_operate triage (FR-002 — a terminal board stops the batch
// at the offending op). The test seeds the session with a real 16×16 loss
// screenshot (saoleiBoardLossPNG / saolei_5.png — contains HIT_MINE "X" and
// MINE "M" cells; see projects/game/pkg/saolei-board/testdata/saolei_5.golden.
// txt), so the agent's existing isTerminalState(state) loss signal fires. It
// then asserts:
//
//  1. The saolei_init result text contains `game status: lost` (FR-012/FR-013
//     — a recognized loss is surfaced on the operation that produced the
//     terminal board).
//  2. A following saolei_operate op stops the batch at op 1 as `game_over`
//     (the 039 result shape replacing the former "rejected: game_over" body)
//     and NO operation FlowPart reaches the desktop.
//  3. The stop body carries `game status: lost` (FR-012..015).
//
// Mirrors TestAgentSaoleiOperateNoOpSkip's drain-and-assert-no-operation
// pattern.
func TestAgentSaoleiLostGameStatusAndTerminalReject(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-loss-%s", uniqueSuffix())

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

	// then: fake-LLM chains saolei_init → the shared operate batch. On the
	// seeded loss board op 1 is rejected as game_over BEFORE any dispatch —
	// the batch stops at op 1 (FR-002 / existing terminal-loss).
	//
	// Drain frames until the stop result arrives, asserting NO operation
	// FlowPart appears in between. The bounded loop also collects the
	// saolei_init tool_result so the lost status line can be asserted on the
	// operation that produced the terminal board.
	const drainLimit = 8
	var initMessage string
	var stopMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_operate op on a lost board dispatched an operation FlowPart (tool_id=%q) — a cell op after a recognized loss MUST stop the batch BEFORE dispatch as game_over (spec 025 FR-015b / specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5)",
				opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "stopped at op 1 (game_over)") {
				stopMessage = msg
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

	// then (2): the post-loss cell op stopped the batch at op 1 as game_over.
	if stopMessage == "" {
		t.Fatalf("did not receive a game_over stop result within %d frames — the batch did not stop at op 1 on the lost board", drainLimit)
	}
	if !strings.Contains(stopMessage, "saolei_operate → stopped at op 1 (game_over)") {
		t.Errorf("stop result = %q, want to contain \"saolei_operate → stopped at op 1 (game_over)\" (spec 025 FR-015b / specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5 — existing terminal-loss)", stopMessage)
	}

	// then (3): the stop body carries `game status: lost` (FR-012..015
	// — every rejection with a recognized state carries the status line).
	if !strings.Contains(stopMessage, "game status: lost") {
		t.Errorf("stop result = %q, want to contain \"game status: lost\" (spec 027 FR-012..015 — game_over stop carries the lost status line)", stopMessage)
	}
}

// TestAgentSaoleiOverFlagBoardStaysPlaying verifies spec 028 FR-006/FR-012 +
// SC-004 (the counter-informed win fix) end-to-end on the deployed agent with
// the REAL recognition engine, under the 039 saolei_operate triage. The test
// seeds the session with a real 9×9 over-flag screenshot (saoleiBoardOverFlagPNG
// / saolei_9.png — grid fully revealed/flagged, 11 flags, top-left mine
// counter reads `-01` because 11 flags exceed the 10 mines). Under the
// PRE-028 grid-only `isWin` rule this board was a FALSE-POSITIVE win; under
// the 028 counter-informed rule, `isWin(state)` additionally requires
// `state.mineCounter === {decoded: true, value: 0}`
// (specs/028-saolei-win-counter-fix/contracts/saolei-mcp-win-contract.md),
// so this over-flag board ⇒ `playing`. The test asserts:
//
//  1. The saolei_init result carries `game status: playing` and does NOT
//     carry `game status: won` (FR-012 — the false-positive win is
//     eliminated).
//  2. A following saolei_operate batch whose ops target revealed/flagged
//     cells is SKIPPED as no-ops (`executed 0 ops, skipped 2 no-op ops`)
//     and is NOT stopped as `game_won` — the fall-through proves the
//     counter cross-check took effect: under the grid-only rule the same
//     ops would have been stopped as `game_won` BEFORE reaching the cell
//     rule.
//  3. The skip result carries `game status: playing` (the over-flag board
//     is non-terminal).
//  4. NO operation FlowPart is dispatched (the no-op skips are pre-dispatch,
//     symmetric with the terminal stops).
func TestAgentSaoleiOverFlagBoardStaysPlaying(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("saolei-overflag-%s", uniqueSuffix())

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

	// then: fake-LLM chains saolei_init → the shared operate batch
	// [click{3,4}, click{5,6}] (sample_saolei_tools.yaml). On the over-flag
	// board every cell is revealed/flagged, so both ops are harmless no-ops
	// (cell_already_revealed / cell_is_flagged) → SKIPPED — and the batch is
	// NOT stopped as game_won (the counter-informed isWin returned false).
	// The bounded drain also collects the saolei_init tool_result so the
	// playing status line can be asserted on the operation that produced the
	// recognized board.
	const drainLimit = 8
	var initMessage string
	var operateMessage string
	for i := 0; i < drainLimit; i++ {
		frame := readWSFrame(t, conn)
		// The no-op skips are pre-dispatch: NO operation FlowPart may reach
		// the desktop (symmetric with the terminal stops — FR-002).
		if opID := frameOperationToolID(frame); opID != "" {
			t.Fatalf("saolei_operate op on the over-flag board dispatched an operation FlowPart (tool_id=%q) — no-op skips MUST be pre-dispatch (spec 025 FR-014)", opID)
		}
		if tr := frameToolResult(frame); tr != nil {
			msg := tr.GetMessage()
			if strings.Contains(msg, "new game started") && initMessage == "" {
				initMessage = msg
			}
			if strings.Contains(msg, "saolei_operate →") {
				operateMessage = msg
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

	// then (2): the post-init batch was skipped as no-ops, NOT stopped as
	// game_won. This fall-through is the direct evidence the counter
	// cross-check took effect: the isWin branch (which would stop the batch
	// at op 1 as game_won under the grid-only rule) did NOT fire.
	if operateMessage == "" {
		t.Fatalf("did not receive a saolei_operate tool_result within %d frames — the all-no-op batch did not complete", drainLimit)
	}
	if !strings.Contains(operateMessage, "saolei_operate → executed 0 ops, skipped 2 no-op ops") {
		t.Errorf("operate result = %q, want to contain \"saolei_operate → executed 0 ops, skipped 2 no-op ops\" (spec 025 FR-015c / 039 FR-002 — no-op skips)", operateMessage)
	}
	if strings.Contains(operateMessage, "game_won") {
		t.Errorf("operate result = %q — over-flag board cell ops were stopped as game_won; the counter-informed isWin did NOT take effect (spec 028 SC-004 — a non-won board MUST NOT stop cell ops as game_won)", operateMessage)
	}

	// then (3): the skip result carries `game status: playing`
	// (FR-012..015 — every result with a recognized state carries the status
	// line; the over-flag board's status is `playing`, not `won`).
	if !strings.Contains(operateMessage, "game status: playing") {
		t.Errorf("operate result = %q, want to contain \"game status: playing\" (spec 028 FR-012 — over-flag board result carries the playing status, not won)", operateMessage)
	}
	if strings.Contains(operateMessage, "game status: won") {
		t.Errorf("operate result = %q — over-flag board result carries `game status: won`; the counter-informed status is wrong (spec 028 SC-004)", operateMessage)
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
//     (sample_saolei_tools.yaml) then chains init→the operate batch
//     [click{3,4}, click{5,6}]→final text — the SAME chain every saolei
//     test uses — so the test "plays the desktop" through it; turn 1
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
// init→operate chain (saolei-init-followup-operate, match_result_contains=[])
// matches every saolei_init result. A saolei_remain follow-up on
// tool_name=saolei_init would either lose to it (operate < remain) or, if
// named to sort first, hijack the existing saolei tests' init flow. The
// all-INITIAL 16×16 init result is identical for this test and
// TestAgentSaoleiTextBoardFlow, so it cannot be disambiguated by content.
// A second-turn keyword (matcher.go Match branch) isolates saolei_remain:
// no other suite sends it, so existing suites are unaffected.
//
// The "no dispatch" assertion mirrors TestAgentSaoleiOperateNoOpSkip's
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
	// three saolei tools are surfaced via the loopback MCP client, each
	// backed by the real @dominion/game-saolei-board recognition engine.
	sessionID := setupTeamSession(t, sutHostURL, sutEnvName, saoleiTemplateID, profileName, "gpt-4", "gpt-4")
	conn := connectAgentWS(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper screenshot (16×16 all-INITIAL) used for
	// turn 1's init + operate replies. Recognition is monotonic-safe across
	// identical frames (no revealed cells to regress), so init + the two
	// batch ops against the same PNG all succeed. The final recognized state
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
	// the operate batch [click{3,4}, click{5,6}] → final text. Play the
	// desktop through both ops so turn 1 completes; the recognized state
	// seeded above persists across turns within the session.
	for _, step := range []struct{ cellX, cellY int32 }{
		{saoleiClick1X, saoleiClick1Y},
		{saoleiClick2X, saoleiClick2Y},
	} {
		clickFrame := readOperationFrame(t, conn)
		if frameMouseMoveAndClick(clickFrame) == nil {
			t.Fatalf("saolei_operate op (%d,%d) did not dispatch a MouseMoveAndClickPart FlowPart; frame parts: %v",
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
