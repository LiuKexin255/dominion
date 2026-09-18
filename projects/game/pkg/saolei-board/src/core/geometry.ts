/**
 * geometry.ts — Screenshot-space board layout constants and helpers.
 *
 * These constants are the SCREENSHOT-space counterparts of the agent's
 * `projects/game/agent/src/mcp/saolei/geometry.ts` values. The agent uses
 * CLIENT space (Y=104 after a 96 px chrome offset) for posting `WM_*`
 * messages; recognition reads the captured screenshot (full window incl.
 * non-client chrome), so it uses the SCREENSHOT-space board top Y=200.
 * Both share X=24 and cell size 32 (`specs/018-saolei-mcp/research.md` D6).
 */

import type { BoardGeometry } from "./types.js";

/** Default layout targeting classic Win32 Microsoft Minesweeper. */
export const DEFAULT_GEOMETRY: BoardGeometry = {
  originXPx: 24,
  originYPx: 200,
  cellSizePx: 32,
};

/** Merge a partial override onto the default geometry. */
export function resolveGeometry(
  override?: Partial<BoardGeometry>,
): BoardGeometry {
  return { ...DEFAULT_GEOMETRY, ...override };
}

/**
 * Auto-detect board dimensions from the screenshot's pixel size and the board
 * geometry. The board occupies `originXPx` left margin and `originYPx` top
 * margin, then `cellSizePx` per cell; the remaining extent divided by the
 * cell size (floored) gives the cell counts. Clamped to ≥1.
 */
export function detectBoardSize(
  imgWidth: number,
  imgHeight: number,
  geometry: BoardGeometry = DEFAULT_GEOMETRY,
): { width: number; height: number } {
  const width = Math.max(
    1,
    Math.floor((imgWidth - geometry.originXPx) / geometry.cellSizePx),
  );
  const height = Math.max(
    1,
    Math.floor((imgHeight - geometry.originYPx) / geometry.cellSizePx),
  );
  return { width, height };
}

/**
 * Top-left pixel offset of cell `(x, y)` in screenshot space. The cell occupies
 * the square `[originX + x*cellSize, originY + y*cellSize]` of size
 * `cellSize × cellSize`.
 */
export function cellOrigin(
  x: number,
  y: number,
  geometry: BoardGeometry = DEFAULT_GEOMETRY,
): { x: number; y: number } {
  return {
    x: geometry.originXPx + x * geometry.cellSizePx,
    y: geometry.originYPx + y * geometry.cellSizePx,
  };
}
