/**
 * win.ts — Pure win classifier for a recognized `GameState`.
 *
 * A board is a win when no cell is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN`
 * — i.e. every cell is a revealed number (`"0"`..`"8"`) or `FLAG`. This is the
 * classic Win32 Minesweeper win condition: on a win all unflagged mines are
 * auto-flagged and all non-mine cells are revealed. `UNKNOWN` is treated
 * leniently (a board the library is not sure about is NOT classified a win).
 *
 * Spec: `specs/027-chat-bubble-game-state/spec.md` FR-009..011.
 * Design: `specs/027-chat-bubble-game-state/data-model.md` §1,
 *          `specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §1.
 *
 * Pure function of `GameState` — no I/O, no mutation, no side effects
 * (FR-011). It does not import the recognition engine; it reasons over an
 * already-recognized state.
 */

import type { CellStatus, GameState } from "./types";

/** Cell statuses that are NOT compatible with a win — a board containing any
 *  of these is not won (FR-009/FR-010). */
const NON_WIN_CELLS: ReadonlySet<CellStatus> = new Set<CellStatus>([
  "INITIAL", // an unrevealed cell ⇒ game still in progress
  "HIT_MINE", // a triggered mine ⇒ a loss
  "MINE", // an end-game revealed mine ⇒ a loss
  "UNKNOWN", // recognition uncertain ⇒ lenient (do not claim a win)
]);

/**
 * Pure win classifier (FR-009..011). Returns true iff NO cell is `INITIAL`,
 * `HIT_MINE`, `MINE`, or `UNKNOWN` — i.e. every cell is a revealed number
 * (`"0"`..`"8"`) or `FLAG`. Single short-circuiting pass over `state.grid`.
 */
export function isWin(state: GameState): boolean {
  for (const row of state.grid) {
    for (const cell of row) {
      if (NON_WIN_CELLS.has(cell)) return false;
    }
  }
  return true;
}
