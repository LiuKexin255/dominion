/**
 * geometry.ts — Fixed board-layout constants and the grid→WM_* client-pixel
 * formula for the saolei MCP
 * (specs/024-tool-render-coord-fix/data-model.md §3;
 * specs/024-tool-render-coord-fix/contracts/coordinate-space-contract.md §2..§4;
 * specs/024-tool-render-coord-fix/research.md D1/D2).
 *
 * Coordinate space: the constants and `center()` are in **WM_* client
 * coordinates** — the coordinate space of the `WM_LBUTTONDOWN` /
 * `WM_RBUTTONDOWN` `lParam` consumed by the desktop's
 * `MOUSE_INPUT_METHOD_WINDOW_MESSAGE` click path
 * (`projects/game/desktop/app_operation.go` `runMouseMoveAndClick` →
 * `projects/game/desktop/internal/operation/window_message_windows.go`
 * `ExecuteWindowMessageClick` → `makeLPARAM`), whose origin is the bound
 * window's client-area top-left and which EXCLUDES the non-client chrome
 * (title bar, menu bar, borders). See
 * https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown
 * (lParam = client coords, chrome-excluded). The desktop posts the coordinate
 * verbatim — no `ScreenshotToScreenCoords`, no `SetCursorPos`, no foreground
 * move (specs/024-tool-render-coord-fix/research.md D8).
 *
 * Chrome compensation: the screenshot capture
 * (`projects/game/desktop/internal/capture/capture.go` `CaptureWindow`) grabs
 * the FULL window via `DwmGetWindowAttribute` `DWMWA_EXTENDED_FRAME_BOUNDS`
 * (https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmgetwindowattribute
 * — full-window bounds incl. non-client chrome), so the board top in the
 * screenshot space is 200 px (specs/018-saolei-mcp/research.md D6). The
 * non-client chrome height on the target Microsoft Minesweeper is 96 px
 * (operator-measured), so the client-space board top is 200 − 96 = 104 px.
 * The compensation is applied HERE (the operation originator) so the desktop
 * receives a correct client coordinate and posts it verbatim — a refactor
 * over a patch (Constitution §II; reconcile the coordinate space at the
 * source, do not layer an opposing offset in the desktop).
 *
 * WINDOW_MESSAGE-only invariant (coordinate-space-contract.md §2/§3/
 * Compensation scope): `center()` is consumed ONLY by the three saolei cell
 * tools (`saolei_click` / `saolei_flag` / `saolei_chord_click`), all of which
 * dispatch `MOUSE_INPUT_METHOD_WINDOW_MESSAGE`. The generic mouse tools
 * (`projects/game/agent/src/tools/mouse_click/`, `tools/mouse_move/`) default
 * to `SIMULATED` and consume SCREENSHOT-space coordinates — the desktop's
 * `SIMULATED` branch adds the window origin via `ScreenshotToScreenCoords`
 * (`projects/game/desktop/internal/operation/convert.go`), so compensating
 * them would shift the click UP by the chrome height and miss. Therefore:
 * (a) do NOT route the generic mouse tools through `center()`; (b) do NOT
 * generalize the compensation into the desktop's shared `runMouseMoveAndClick`
 * (both branches would break `SIMULATED`); (c) keep the saolei cell tools on
 * `WINDOW_MESSAGE` (client coords are wrong for the `SIMULATED` path —
 * specs/018-saolei-mcp/research.md D5).
 *
 * The values target the standard Microsoft Minesweeper window layout and are
 * window-layout-specific (018 D6 posture retained); re-tune the constants
 * here (one place) without touching the tool contracts.
 */

/** Grid left-edge offset from the window's left edge, in pixels. Screenshot
 * space; the left non-client chrome is only the window border (~3 px,
 * sub-cell), so NO X chrome compensation is applied
 * (coordinate-space-contract.md §4). */
export const BOARD_ORIGIN_X_PX = 24;

/**
 * Grid top-edge offset in SCREENSHOT (full-window) space, in pixels — matches
 * the screenshot/visual layout (specs/018-saolei-mcp/research.md D6).
 * Compensated to client space via CHROME_OFFSET_Y_PX below.
 */
export const BOARD_ORIGIN_Y_PX_SCREENSHOT = 200;

/**
 * Non-client chrome height (the screenshot↔client Y difference): title bar +
 * menu bar + borders on the target Microsoft Minesweeper, operator-measured
 * 96 px (specs/024-tool-render-coord-fix/research.md D2). Window-layout-
 * specific — re-tune per bound window.
 */
export const CHROME_OFFSET_Y_PX = 96;

/**
 * Grid top-edge offset in CLIENT (WM_* lParam) space, in pixels — the board
 * top the `WINDOW_MESSAGE` click path expects. = screenshot − chrome = 104.
 */
export const BOARD_ORIGIN_Y_PX = BOARD_ORIGIN_Y_PX_SCREENSHOT - CHROME_OFFSET_Y_PX;

/** Cell width = cell height, in pixels. Identical in both screenshot and
 * client coordinate spaces (no DPI scaling applied). */
export const CELL_SIZE_PX = 32;

/**
 * Compute the WM_* client-pixel centre of cell `(x, y)` per
 * specs/024-tool-render-coord-fix/data-model.md §3:
 *
 *   centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *              = 24 + x*32 + 16
 *   centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *              = 104 + y*32 + 16
 *
 * Used ONLY by `saolei_click`/`saolei_flag`/`saolei_chord_click` to build
 * `MouseMoveAndClickPart{ xPx: centerX, yPx: centerY,
 * method: MOUSE_INPUT_METHOD_WINDOW_MESSAGE }`.
 * Worked: center(4,4) = (168, 248) — row 4's centre in client space
 * (data-model.md §3).
 */
export function center(
	x: number,
	y: number,
): { xPx: number; yPx: number } {
	return {
		xPx: BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2,
		yPx: BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2,
	};
}
