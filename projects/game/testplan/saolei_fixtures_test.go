// Package testplan contains shared saolei large-test fixtures: the real
// Minesweeper screenshots (embedded PNGs) the agent_v2 team suites answer
// their fake-desktop receipts with (style/large_test.md §反模式3 — shared
// fixtures live in one file, not copied per suite).
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
)

// saoleiBoardInitPNG is a real Minesweeper screenshot (16×16, all INITIAL)
// recognized as an in-progress game.
//
//go:embed testdata/saolei_1.png
var saoleiBoardInitPNG []byte

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

// saoleiBoardCompatWinPNG is a real Minesweeper screenshot (9×9, almost
// every cell INITIAL plus 6 flags — saolei_8.png) recognized as an
// in-progress game. The team-mode terminal-win flow seeds a game with this
// board and then answers the first operate dispatch with saoleiBoardWinPNG:
// the two boards are cell-compatible (a same-game successor — no revealed
// cell regresses, checkCompatible in
// projects/game/pkg/saolei-board/src/core/validate.ts), so the operate
// result carries `game status: won` and the GameRuntime records the terminal
// game event that triggers the planner review.
//
//go:embed testdata/saolei_8.png
var saoleiBoardCompatWinPNG []byte
