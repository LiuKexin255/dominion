// Package testplan contains the saolei MCP large-test suite.
//
// agent_saolei_test.go validates the deployed agent's saolei MCP path
// (specs/025-desktop-image-state-refine) end-to-end: a saolei-enabled profile
// drives the model→tool_call→loopback-MCP→OperationBridge→desktop-WS chain,
// and the test "plays the desktop" — it reads the operation FlowParts the
// agent dispatches (KeyboardPressPart{F2} for saolei_init,
// MouseMoveAndClickPart for saolei_click) and replies with a FlowResultPart
// (control channel, spec 025 FR-023/FR-024) carrying a real Minesweeper
// screenshot so the agent's @dominion/game-saolei-board recognition engine
// decodes the board.
//
// Coverage (spec 025 FR-012..FR-018, FR-022; spec 027 FR-012..FR-015,
// FR-021..023):
//   - TestAgentSaoleiTextBoardFlow: init→click→click on a recognizable
//     in-progress board; each tool returns a TEXT board (no image block) and
//     the screenshot stays on the control channel; every result carries
//     `game status: playing` (spec 027 FR-012/FR-014).
//   - TestAgentSaoleiIllegalMovePreDispatchReject: a click on an already-
//     revealed cell is rejected BEFORE dispatch (no operation FlowPart reaches
//     the desktop) with a stable reason code the model can act on.
//   - TestAgentSaoleiWonGameStatusAndTerminalReject: a 9×9 win screenshot
//     (saolei_10.png) seeds a terminal-won state — init surfaces
//     `game status: won` and a following cell op is rejected pre-dispatch as
//     `game_won` (spec 027 FR-021..023) carrying the won status line.
//   - TestAgentSaoleiLostGameStatusAndTerminalReject: a 16×16 loss screenshot
//     (saolei_5.png) seeds a terminal-lost state — init surfaces
//     `game status: lost` and a following cell op is rejected pre-dispatch as
//     `game_over` (existing terminal-loss) carrying the lost status line.
//
// Organised by MODULE per style/large_test.md (not by scenario/spec-id); it
// reuses the shared helpers in helpers_test.go.
package testplan

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// saoleiBoardInitPNG is a real Minesweeper screenshot (16×16, all INITIAL)
// reused from the @dominion/game-saolei-board golden testdata. The agent's
// saolei MCP runs the REAL recognition engine in large tests (no DI seam in
// a deployed agent), so the FlowResultPart.screenshot the test "plays the
// desktop" returning MUST be a recognizable Minesweeper board, otherwise
// `SaoleiBoard.init` throws and `saolei_init` returns "unable to recognize".
// The bytes are authoritative under the saolei-board package (golden-tested
// in projects/game/pkg/saolei-board/src/core/golden.test.ts); this is a
// testdata fixture reuse, not a helper copy (style/large_test.md §反模式3
// concerns code helpers, not binary fixtures).
//
//go:embed testdata/saolei_1.png
var saoleiBoardInitPNG []byte

// saoleiBoardRevealedPNG is a real Minesweeper screenshot (9×9, partially
// revealed — cell (3,4) is the number "1") reused from the saolei-board
// golden testdata. Used by TestAgentSaoleiIllegalMovePreDispatchReject:
// `saolei_init` recognizes this board, then `saolei_click(3,4)` is rejected
// pre-dispatch as `cell_already_revealed` (FR-015c) — the dispatch never
// reaches the desktop.
//
//go:embed testdata/saolei_2.png
var saoleiBoardRevealedPNG []byte

// saoleiBoardWinPNG is a real Minesweeper screenshot (9×9 win board — every
// cell is a revealed number "0".."8" or FLAG; no INITIAL/HIT_MINE/MINE/
// UNKNOWN) reused from the saolei-board golden testdata. Used by
// TestAgentSaoleiWonGameStatusAndTerminalReject (specs/027-chat-bubble-game-
// state / research.md D12): `saolei_init` recognizes this board, the
// @dominion/game-saolei-board `isWin(state)` predicate returns true
// (specs/027-chat-bubble-game-state/data-model.md §1), so the init result
// carries `game status: won` and any following cell op is rejected
// pre-dispatch as `game_won` (FR-021..023).
//
//go:embed testdata/saolei_10.png
var saoleiBoardWinPNG []byte

// saoleiBoardLossPNG is a real Minesweeper screenshot (16×16 loss board —
// contains HIT_MINE "X" and MINE "M" cells; see
// projects/game/pkg/saolei-board/testdata/saolei_5.golden.txt) reused from
// the saolei-board golden testdata. Used by
// TestAgentSaoleiLostGameStatusAndTerminalReject (specs/027-chat-bubble-game-
// state / research.md D12): `saolei_init` recognizes this board, the agent's
// existing `isTerminalState(state)` loss signal fires, so the init result
// carries `game status: lost` and any following cell op is rejected
// pre-dispatch as `game_over` (existing terminal-loss,
// specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5)
// whose body now also carries `game status: lost` (FR-012..015).
//
//go:embed testdata/saolei_5.png
var saoleiBoardLossPNG []byte

// saoleiMcpNames is the profile MCP selection that triggers the agent's
// saolei adapter path (llm.ts builds the loopback MultiServerMCPClient for a
// saolei profile — spec 023 FR-016; the four saolei tools are registered
// inside createSaoleiMcpServer, which per spec 025 FR-013 holds a per-session
// recognized board state — no longer stateless).
var saoleiMcpNames = []string{"saolei"}

// saolei cell geometry constants. The fake-LLM fixture drives
// saolei_click{3,4} then saolei_click{5,6}; their WM_* client-space cell
// centres per the formula in projects/game/agent/src/mcp/saolei/geometry.ts
// (centerX(x) = 24 + x*32 + 16, centerY(y) = 104 + y*32 + 16) are asserted on
// the dispatched MouseMoveAndClickPart. centerY uses the client-space board
// top BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT(200) − CHROME_OFFSET_Y_PX(96)
// = 104 — the screenshot→client chrome compensation applied in the agent
// (specs/024-tool-render-coord-fix/research.md D1/D2) so the desktop's
// WINDOW_MESSAGE path posts the coordinate verbatim (desktop-facing contract
// unchanged — specs/018-saolei-mcp/contracts/proto-operation-contract.md §3;
// specs/024-tool-render-coord-fix/contracts/coordinate-space-contract.md §4/§6;
// specs/024-tool-render-coord-fix/data-model.md §3).
const (
	saoleiClick1X = 3
	saoleiClick1Y = 4
	saoleiClick2X = 5
	saoleiClick2Y = 6

	saoleiClick1CenterX = 136 // 24 + 3*32 + 16
	saoleiClick1CenterY = 248 // 104 + 4*32 + 16
	saoleiClick2CenterX = 200 // 24 + 5*32 + 16
	saoleiClick2CenterY = 312 // 104 + 6*32 + 16
)

// expectedSaoleiFinalText is the terminal text fake-LLM returns once the
// second saolei_click result reaches the model
// (sample_saolei_tools.yaml saolei-click-5-6-final-text). The test asserts
// it to prove the whole init→click→click chain completed.
const expectedSaoleiFinalText = "Minesweeper sequence complete."

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
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate minesweeper via saolei tools.",
			McpNames:     saoleiMcpNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper screenshot (16×16 all-INITIAL) used for
	// every dispatch reply. Recognition is monotonic-safe across identical
	// frames (no revealed cells to regress — saolei-board README §状态校验),
	// so init + two updates against the same PNG all succeed.
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardInitPNG)

	// when: a user turn whose text matches the "saolei-start" keyword,
	// making fake-LLM return the first saolei_init tool_call.
	sendTextWithProfile(t, conn, sessionID, profileName, "please start saolei game")

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
	textFrame := drainWSFrame(t, conn, func(f *game.AgentFrame) bool {
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
	lmr := listMessages(t, sutHostURL, sutEnvName, sessionID)
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

	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate minesweeper via saolei tools.",
			McpNames:     saoleiMcpNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
	defer conn.Close()

	// A recognizable Minesweeper board (9×9, partially revealed) whose cell
	// (3,4) is the number "1" (already revealed) — see
	// projects/game/pkg/saolei-board/testdata/saolei_2.golden.txt. A left-click
	// on a revealed number is rejected as cell_already_revealed (FR-015c).
	screenshot := buildSaoleiFlowResultScreenshot(saoleiBoardRevealedPNG)

	// given: a user turn triggers saolei_init. The agent dispatches F2; the
	// test replies with the recognizable screenshot so the agent seeds the
	// recognized state from the 9×9 partially-revealed board.
	sendTextWithProfile(t, conn, sessionID, profileName, "please start saolei game")

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
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate minesweeper via saolei tools.",
			McpNames:     saoleiMcpNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
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
	sendTextWithProfile(t, conn, sessionID, profileName, "please start saolei game")

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
	createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You operate minesweeper via saolei tools.",
			McpNames:     saoleiMcpNames,
			Enabled:      true,
		},
	})
	sessionID, _ := createSession(t, sutHostURL, sutEnvName)
	conn := connectAgentWS(t, sutHostURL, sutEnvName, sessionID)
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
	sendTextWithProfile(t, conn, sessionID, profileName, "please start saolei game")

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

// assertMouseMoveAndClick verifies a MouseMoveAndClickPart carries the
// expected centre coordinates, LEFT_CLICK action, and WINDOW_MESSAGE method
// (the desktop-facing saolei contract — spec 023 FR-020 / spec 018 FR-004b).
func assertMouseMoveAndClick(p *game.MouseMoveAndClickPart, wantX, wantY int32, wantClick game.MouseClickAction) error {
	if p.GetXPx() != wantX || p.GetYPx() != wantY {
		return fmt.Errorf("coords = (%d,%d), want (%d,%d)", p.GetXPx(), p.GetYPx(), wantX, wantY)
	}
	if p.GetClick() != wantClick {
		return fmt.Errorf("click = %v, want %v", p.GetClick(), wantClick)
	}
	if p.GetMethod() != game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE {
		return fmt.Errorf("method = %v, want WINDOW_MESSAGE", p.GetMethod())
	}
	return nil
}
