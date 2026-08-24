/**
 * win.ts — Pure win classifier for a recognized `GameState`.
 *
 * A board is a win **iff** BOTH:
 *   (a) no cell is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` — i.e. every
 *       cell is a revealed number (`"0"`..`"8"`) or `FLAG` (the classic
 *       Win32 Minesweeper win condition — on a win all unflagged mines are
 *       auto-flagged and all non-mine cells are revealed), AND
 *   (b) the top-left mine counter reads exactly `000` — i.e.
 *       `state.mineCounter === { decoded: true; value: 0 }` (the game's own
 *       `mines − flags` display ⇒ `flags === mines`).
 *
 * The grid-only rule is necessary but not sufficient: a board can look fully
 * revealed/flagged yet not be won when the player over-flagged (so
 * `mines − flags` ≠ 0 — the `saolei_9` false-positive case, where 11 flags on
 * a 10-mine board make the counter read `-01`). The counter is the game's own
 * ground-truth display; the conjunction of the two halves is necessary and
 * sufficient for a win (`saolei_10` is the genuine win — both hold; `saolei_9`
 * fails the counter half; `saolei_11` fails the grid half).
 *
 * Leniency (FR-008): `mineCounter === undefined` (not decoded in this pass)
 * or `{ decoded: false }` ⇒ the predicate returns `false` — never claim a win
 * on an absent/uncertain counter. A missed win (false negative, recoverable —
 * the operator/agent simply continues) is preferred over a false win
 * (terminal, blocks all further play). This mirrors the library's existing
 * `UNKNOWN`-cell leniency ([027 FR-010](../../specs/027-chat-bubble-game-state/spec.md))
 * and [025 FR-018](../../specs/025-desktop-image-state-refine/spec.md)).
 *
 * Loss precedence (FR-010, unchanged from 027): a `HIT_MINE`/`MINE` cell
 * already makes the grid half (a) fail ⇒ the predicate returns `false`
 * regardless of the counter. (`isTerminalState` in the saolei MCP stays
 * loss-only and is checked before `isWin`, so a loss surfaces `game_over` not
 * `game_won`.)
 *
 * Spec: `specs/028-saolei-win-counter-fix/spec.md` FR-005..010 (supersedes the
 * grid-only [027 FR-009..011](../../specs/027-chat-bubble-game-state/spec.md);
 * the grid condition is retained as one half of the conjunction).
 * Design: `specs/028-saolei-win-counter-fix/data-model.md` entity 4,
 *          `specs/028-saolei-win-counter-fix/contracts/saolei-board-api.md`,
 *          `specs/028-saolei-win-counter-fix/research.md` D7.
 *
 * Pure function of `GameState` — no I/O, no mutation, no side effects
 * (FR-009). It does not import the recognition engine; it reasons over an
 * already-recognized state.
 */

import type { CellStatus, GameState } from "./types.js";

/** Cell statuses that are NOT compatible with a win — a board containing any
 *  of these fails the grid half (FR-009/FR-010). */
const NON_WIN_CELLS: ReadonlySet<CellStatus> = new Set<CellStatus>([
  "INITIAL", // an unrevealed cell ⇒ game still in progress
  "HIT_MINE", // a triggered mine ⇒ a loss
  "MINE", // an end-game revealed mine ⇒ a loss
  "UNKNOWN", // recognition uncertain ⇒ lenient (do not claim a win)
]);

/**
 * Pure win classifier (FR-005..010). Returns `true` **iff** BOTH:
 *   (a) no cell is `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` (every cell is a
 *       revealed number `"0"`..`"8"` or `FLAG`), AND
 *   (b) `state.mineCounter === { decoded: true; value: 0 }` (the top-left
 *       mine counter reads exactly `000` — `flags === mines`).
 *
 * Lenient on an absent or undecodable counter (FR-008): `mineCounter ===
 * undefined` or `{ decoded: false }` ⇒ `false`. The grid half short-circuits
 * on the first offending cell; the counter half is checked once the grid
 * half passes.
 */
export function isWin(state: GameState): boolean {
  for (const row of state.grid) {
    for (const cell of row) {
      if (NON_WIN_CELLS.has(cell)) return false;
    }
  }
  return state.mineCounter?.decoded === true && state.mineCounter.value === 0;
}
