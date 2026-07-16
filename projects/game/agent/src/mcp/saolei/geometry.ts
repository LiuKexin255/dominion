/**
 * @fileoverview Saolei coordinate geometry — the pinned board offsets
 * (data-model.md §4, plan D-6) and the cell-centre → window-client-relative
 * pixel computation. All constants are module-level (not agent-supplied) per
 * FR-015; coordinates are window-client-relative pixels in the same space the
 * mouse tool uses.
 */

/**
 * Board top edge offset from the window's top edge (px). data-model.md §4.
 */
export const TOP_OFFSET = 200;

/**
 * Board left edge offset from the window's left edge (px). data-model.md §4.
 */
export const LEFT_OFFSET = 24;

/**
 * Cell width = cell height (square cells, px). data-model.md §4.
 */
export const BLOCK_LENGTH = 32;

/** Window-client-relative pixel coordinate. */
export interface Pixel {
  x: number;
  y: number;
}

/**
 * Compute the window-client-relative pixel at the centre of grid cell `(x, y)`.
 *
 * `X = LEFT_OFFSET + x*BLOCK_LENGTH + BLOCK_LENGTH/2`
 * `Y = TOP_OFFSET  + y*BLOCK_LENGTH + BLOCK_LENGTH/2`
 * (data-model.md §4; e.g. (0,0) → (40,216), (1,1) → (72,248)). The caller MUST
 * have validated `(x, y)` is in board bounds via `inBounds` (FR-021).
 */
export function cellCentre(x: number, y: number): Pixel {
  return {
    x: LEFT_OFFSET + x * BLOCK_LENGTH + BLOCK_LENGTH / 2,
    y: TOP_OFFSET + y * BLOCK_LENGTH + BLOCK_LENGTH / 2,
  };
}

/**
 * Bounds check for a grid coordinate against a board of `width`×`height` cells.
 * In-bounds iff `0 <= x < width && 0 <= y < height` (FR-021, data-model.md §4).
 */
export function inBounds(
  x: number,
  y: number,
  width: number,
  height: number,
): boolean {
  return x >= 0 && x < width && y >= 0 && y < height;
}
