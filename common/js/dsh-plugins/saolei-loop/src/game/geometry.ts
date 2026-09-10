/**
 * Fixed board-layout constants and the grid→WM_* client-pixel formula for the
 * GameRuntime's cell-operation dispatch (specs/051-agent-v2-dsh-migration/
 * research.md D6; data-model.md §2.5).
 *
 * Coordinate space: the constants and `center()` are in **WM_* client
 * coordinates** — the `lParam` space consumed by the desktop's
 * `MOUSE_INPUT_METHOD_WINDOW_MESSAGE` click path, whose origin is the bound
 * window's client-area top-left and which excludes the non-client chrome
 * (https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown).
 * The desktop posts the coordinate verbatim — no cursor move, no foreground
 * change. Recognition reads pixels in **screenshot** space (originY 200, full
 * window incl. chrome) inside `@dominion/game-saolei-board`; the two spaces
 * must not be mixed (projects/game/pkg/saolei-board/README.md → "坐标空间注意").
 *
 * Chrome compensation: the desktop captures the FULL window
 * (`DWMWA_EXTENDED_FRAME_BOUNDS`), so the board top is 200 px in screenshot
 * space; the non-client chrome of the target Microsoft Minesweeper is 96 px
 * (operator-measured), giving the client-space board top 200 − 96 = 104 px.
 * The compensation is
 * applied here (the operation originator) so the desktop receives a correct
 * client coordinate.
 *
 * WINDOW_MESSAGE-only invariant: `center()` is consumed only by cell
 * operations (click/flag/chord), all dispatched with `WINDOW_MESSAGE` — the
 * real cursor would visually block cells in the screenshot the recognizer
 * reads (v1 research D5). Values are window-layout-specific; re-tune them
 * here (one place) without touching the tool contracts.
 */

/** Grid left-edge offset from the window's left edge, in pixels. The left
 * non-client chrome is only the window border (~3 px, sub-cell), so no X
 * compensation is applied. */
export const BOARD_ORIGIN_X_PX = 24;

/** Grid top-edge offset in SCREENSHOT (full-window) space, in pixels. */
export const BOARD_ORIGIN_Y_PX_SCREENSHOT = 200;

/** Non-client chrome height (the screenshot↔client Y difference): title bar
 * + menu bar + borders, operator-measured on the target Microsoft
 * Minesweeper. Window-layout-specific. */
export const CHROME_OFFSET_Y_PX = 96;

/** Grid top-edge offset in CLIENT (WM_* lParam) space, in pixels — the board
 * top the `WINDOW_MESSAGE` click path expects (= screenshot − chrome = 104). */
export const BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT - CHROME_OFFSET_Y_PX;

/** Cell width = cell height, in pixels (identical in both coordinate
 * spaces; no DPI scaling applied). */
export const CELL_SIZE_PX = 32;

/**
 * Compute the WM_* client-pixel centre of cell `(x, y)`:
 *
 *   centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *   centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *
 * Worked: center(4,4) = (168, 248).
 */
export function center(x: number, y: number): { xPx: number; yPx: number } {
  return {
    xPx: BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2,
    yPx: BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2,
  };
}

/**
 * Wire value of `MouseInputMethod.MOUSE_INPUT_METHOD_WINDOW_MESSAGE`
 * (proto enum string, projects/game/game.proto `enum MouseInputMethod`). All
 * saolei cell operations dispatch with `WINDOW_MESSAGE`.
 */
export const WINDOW_MESSAGE = "MOUSE_INPUT_METHOD_WINDOW_MESSAGE";
