/**
 * geometry.ts — Fixed board-layout constants and grid→window-client pixel
 * formula for the saolei MCP (data-model.md §5; research.md D6).
 *
 * These are window-client coordinates (relative to the bound window's
 * top-left, DPI-corrected). The desktop's `WINDOW_MESSAGE` mouse path uses
 * them directly, packing them into `WM_*` `lParam` with NO screen-offset
 * addition (unlike `SIMULATED`, which adds window bounds via
 * `ScreenshotToScreenCoords`).
 *
 * Source: spec Clarifications Round 2 D4 / FR-007. The values target the
 * standard Microsoft Minesweeper window layout. Hardcoded here so a future
 * board layout can be parameterised without touching the tool contracts.
 */

/** Grid left-edge offset from the window's left edge, in pixels. */
export const BOARD_ORIGIN_X_PX = 24;

/** Grid top-edge offset from the window's top edge, in pixels. */
export const BOARD_ORIGIN_Y_PX = 200;

/** Cell width = cell height, in pixels. */
export const CELL_SIZE_PX = 32;

/**
 * Compute the window-client pixel centre of cell `(x, y)` per
 * data-model.md §5:
 *
 *   centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *              = 24 + x*32 + 16
 *   centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2
 *              = 200 + y*32 + 16
 *
 * Used by `saolei_click`/`saolei_flag`/`saolei_chord_click` to build
 * `MouseMoveAndClickPart{ x: centerX, y: centerY, ... }`.
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
