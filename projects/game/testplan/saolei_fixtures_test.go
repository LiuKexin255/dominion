// Package testplan contains shared saolei large-test fixtures: the real
// Minesweeper screenshots (embedded PNGs) and the saolei geometry/expectation
// constants shared by the agent_saolei and saolei_team suites
// (style/large_test.md §反模式3 — shared fixtures live in one file, not
// copied per suite).
//
// The embedded PNGs are real Minesweeper screenshots reused from the
// @dominion/game-saolei-board golden testdata. The deployed agent runs the
// REAL recognition engine in large tests (no DI seam in a deployed agent), so
// the FlowResultPart.screenshot the test "plays the desktop" returning MUST
// be a recognizable Minesweeper board, otherwise `SaoleiBoard.init` throws
// and `saolei_init` returns "unable to recognize". The bytes are
// authoritative under the saolei-board package (golden-tested in
// projects/game/pkg/saolei-board/src/core/golden.test.ts); this is a
// testdata fixture reuse, not a helper copy (style/large_test.md §反模式3
// concerns code helpers, not binary fixtures).
package testplan

import (
	_ "embed"
	"fmt"

	game "dominion/projects/game"
)

// saoleiBoardInitPNG is a real Minesweeper screenshot (16×16, all INITIAL)
// recognized as an in-progress game.
//
//go:embed testdata/saolei_1.png
var saoleiBoardInitPNG []byte

// saoleiBoardRevealedPNG is a real Minesweeper screenshot (9×9, partially
// revealed — cell (3,4) is the number "1") recognized as an in-progress
// game. Used by TestAgentSaoleiIllegalMovePreDispatchReject:
// `saolei_init` recognizes this board, then the fixture's `saolei_operate`
// batch ops (3,4)/(5,6) are skipped as no-ops (`cell_already_revealed` —
// FR-002 harmless no-op skip, the 039 successor of the 025
// `cell_already_revealed` pre-dispatch rejection) — the dispatch never
// reaches the desktop.
//
//go:embed testdata/saolei_2.png
var saoleiBoardRevealedPNG []byte

// saoleiBoardWinPNG is a real Minesweeper screenshot (9×9 win board — every
// cell is a revealed number "0".."8" or FLAG; no INITIAL/HIT_MINE/MINE/
// UNKNOWN) recognized as a terminal win. `saolei_init` recognizes this board,
// `isWin(state)` returns true (specs/027-chat-bubble-game-state/data-model.md
// §1), so the init result carries `game status: won` and any following cell
// op is rejected pre-dispatch as `game_won` (FR-021..023).
//
//go:embed testdata/saolei_10.png
var saoleiBoardWinPNG []byte

// saoleiBoardLossPNG is a real Minesweeper screenshot (16×16 loss board —
// contains HIT_MINE "X" and MINE "M" cells; see
// projects/game/pkg/saolei-board/testdata/saolei_5.golden.txt) recognized as
// a terminal loss. `saolei_init` recognizes this board, the agent's existing
// `isTerminalState(state)` loss signal fires, so the init result carries
// `game status: lost` and any following cell op is rejected pre-dispatch as
// `game_over` (existing terminal-loss,
// specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md §5).
//
//go:embed testdata/saolei_5.png
var saoleiBoardLossPNG []byte

// saoleiBoardOverFlagPNG is a real Minesweeper screenshot (9×9, grid fully
// revealed/flagged — every cell is a revealed number "0".."8" or FLAG; 11
// flags total) recognized as a NON-terminal in-progress game: the top-left
// mine counter reads `-01` (over-flagged), so the counter-informed
// `isWin(state)` returns false (specs/028-saolei-win-counter-fix) and the
// board reports `game status: playing` (NOT `won`).
//
//go:embed testdata/saolei_9.png
var saoleiBoardOverFlagPNG []byte

// saolei cell geometry constants. The fake-LLM fixture drives ONE
// saolei_operate BATCH whose ops are click{3,4} and click{5,6}
// (spec 039-planner-memory-calibration FR-001 — the merged dual-form tool;
// sample_saolei_tools.yaml saolei-init-followup-operate); their WM_*
// client-space cell centres per the formula in
// projects/game/agent/src/mcp/saolei/geometry.ts
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

// expectedSaoleiFinalText is the terminal text fake-LLM returns once
// the saolei_operate batch result reaches the model
// (sample_saolei_tools.yaml saolei-operate-final-text). The test asserts
// it to prove the whole init→operate chain completed.
const expectedSaoleiFinalText = "Minesweeper sequence complete."

// expectedSaoleiRemainFinalText is the terminal text fake-LLM returns once
// the saolei_remain tool-result loop closes
// (sample_saolei_tools.yaml saolei-remain-final-text). Used by
// TestAgentSaoleiRemainToolNoDispatch to prove the remain turn ended
// deterministically (saolei_remain dispatches nothing, so the fake-LLM
// MUST return text — not a tool_call — after its result, otherwise the
// no-match random fallback could dispatch an unrelated operation and
// muddy the zero-dispatch assertion).
const expectedSaoleiRemainFinalText = "Remaining mines computed."

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
