/**
 * @fileoverview Saolei board state machine — cell-state enumeration, the
 * per-session board grid, and the lifecycle transitions that enforce the
 * operate→update protocol. This module holds state and performs mechanical
 * transitions only; legality rules (FR-016..023) live in `validation.ts` and
 * gate these transitions. See specs/018-saolei-mcp/data-model.md §1-2.
 */

/**
 * State of a single board cell (data-model.md §1). Number states (`zero`..
 * `eight`) and `boom` are terminal for that cell; only `saolei_flag` produces
 * or clears a `flag` (FR-020). Coordinates are zero-indexed from the top-left.
 */
export type CellState =
  | "block"
  | "zero"
  | "one"
  | "two"
  | "three"
  | "four"
  | "five"
  | "six"
  | "seven"
  | "eight"
  | "flag"
  | "boom";

/** All cell states in display order; reused as the zod enum source in tools. */
export const CELL_STATES: readonly CellState[] = [
  "block",
  "zero",
  "one",
  "two",
  "three",
  "four",
  "five",
  "six",
  "seven",
  "eight",
  "flag",
  "boom",
];

/** Revealed number cells, terminal for that cell (data-model.md §1). */
export const NUMBER_CELL_STATES: readonly CellState[] = [
  "zero",
  "one",
  "two",
  "three",
  "four",
  "five",
  "six",
  "seven",
  "eight",
];

/**
 * Board lifecycle marker (data-model.md §2). `awaiting-update` has NO
 * automatic timeout (FR-011a); recovery is `saolei_init` only.
 */
export type BoardLifecycle =
  | "uninitialized"
  | "ready"
  | "awaiting-update"
  | "terminal";

/** A single `(x, y, state)` cell update reported by `saolei_update`. */
export interface CellUpdate {
  x: number;
  y: number;
  state: CellState;
}

/**
 * BoardState owns the 2D cell grid and lifecycle marker for one session. The
 * grid is row-major (`grid[y][x]`); a new instance starts `uninitialized` and
 * only `init` produces a populated `ready` board. Transitions are direct
 * assignments — callers gate them through `validation.ts` (FR-016..023) so the
 * board never has to encode reason strings.
 */
export class BoardState {
  width = 0;
  height = 0;
  grid: CellState[][] = [];
  lifecycle: BoardLifecycle = "uninitialized";

  /**
   * (Re)initialize the board to an `x` by `y` grid of `block` cells and enter
   * `ready`. Allowed from any state (FR-009); always resets all state.
   *
   * `x` and `y` MUST be positive integers: a zero/negative dimension would
   * produce an unusable empty board (every coordinate out of bounds, no cells
   * to operate on). The saolei_init tool enforces `.positive()` at the zod
   * schema; this guard defends the public method directly so a misuse fails
   * loudly instead of silently producing a degenerate board.
   */
  init(width: number, height: number): void {
    if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) {
      throw new Error(
        `board.init requires positive integer dimensions, got ${width}×${height}`,
      );
    }
    this.width = width;
    this.height = height;
    this.grid = [];
    for (let y = 0; y < height; y++) {
      const row: CellState[] = [];
      for (let x = 0; x < width; x++) {
        row.push("block");
      }
      this.grid.push(row);
    }
    this.lifecycle = "ready";
  }

  /**
   * Enter `awaiting-update` after an operation was dispatched. The caller MUST
   * have validated the board is `ready` (FR-017); `awaiting-update` blocks all
   * further positional operations until `saolei_update` (FR-010).
   */
  enterAwaitingUpdate(): void {
    this.lifecycle = "awaiting-update";
  }

  /**
   * Commit a batch of observed cell updates atomically and resolve the
   * lifecycle: `awaiting-update → ready`, or `→ terminal` when any updated cell
   * is `boom` (FR-023). The caller MUST have validated every transition is
   * legal (FR-022) and that the board is `awaiting-update`; this method only
   * writes and transitions. No partial application is possible because legality
   * is decided before this runs.
   */
  commitUpdate(cells: readonly CellUpdate[]): void {
    let hasBoom = false;
    for (const cell of cells) {
      this.grid[cell.y][cell.x] = cell.state;
      if (cell.state === "boom") {
        hasBoom = true;
      }
    }
    this.lifecycle = hasBoom ? "terminal" : "ready";
  }

  /** Read the cell at `(x, y)`. Caller MUST ensure the coordinate is in bounds. */
  cellAt(x: number, y: number): CellState {
    return this.grid[y][x];
  }

  /** Board summary echoed in `SaoleiToolResult.board` (data-model.md §7). */
  toSummary(): { width: number; height: number; lifecycle: BoardLifecycle } {
    return { width: this.width, height: this.height, lifecycle: this.lifecycle };
  }
}
