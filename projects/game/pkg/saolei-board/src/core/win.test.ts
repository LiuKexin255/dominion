import { describe, expect, it } from "vitest";

import type { CellStatus, GameState, MineCounter } from "./types";
import { isWin } from "./win";

/**
 * win.test.ts — Unit tests for the pure `isWin` predicate.
 *
 * Covers every branch of FR-005..010 (spec
 * `specs/028-saolei-win-counter-fix/spec.md`): the conjunction of
 *   (a) no cell is `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`, AND
 *   (b) `mineCounter === { decoded: true; value: 0 }`.
 * Positive cases (all-revealed-numbers / all-FLAG / mixed) ⇒ `true` only when
 * the counter reads `000`; counter non-zero ⇒ `false` (FR-006); counter `000`
 * with an `INITIAL` cell ⇒ `false` (FR-007); counter `undefined` or
 * `{ decoded: false }` ⇒ `false` (lenient, FR-008); `HIT_MINE`/`MINE` loss ⇒
 * `false` regardless of counter (FR-010). Pure unit — no DI, no `vi.mock`
 * (`style/javascript.md` §测试).
 */

/** A decoded `000` mine counter — `flags === mines` (the counter half of a win). */
const COUNTER_ZERO: MineCounter = { decoded: true, value: 0 };

/** A decoded `-01` mine counter — over-flagged (the `saolei_9` shape). */
const COUNTER_NEG_ONE: MineCounter = { decoded: true, value: -1 };

/** An undecodable mine counter (lenient ⇒ not a win). */
const COUNTER_UNDECODABLE: MineCounter = { decoded: false };

/** Build a square `GameState` from row arrays of `CellStatus`, optionally
 *  carrying a decoded mine counter (default `undefined` — lenient). */
function board(rows: CellStatus[][], mineCounter?: MineCounter): GameState {
  const height = rows.length;
  const width = rows[0]?.length ?? 0;
  return { width, height, grid: rows, mineCounter };
}

describe("isWin", () => {
  it("returns true for an all-revealed-numbers board with a decoded 000 counter", () => {
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "4", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(true);
  });

  it("returns true for an all-FLAG board with a decoded 000 counter", () => {
    const state = board(
      [
        ["FLAG", "FLAG", "FLAG"],
        ["FLAG", "FLAG", "FLAG"],
        ["FLAG", "FLAG", "FLAG"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(true);
  });

  it("returns true for a mixed numbers+flags board with a decoded 000 counter", () => {
    const state = board(
      [
        ["0", "1", "FLAG"],
        ["2", "FLAG", "3"],
        ["FLAG", "4", "0"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(true);
  });

  it("returns false when the grid is all-revealed but the counter is non-zero (over-flagged, FR-006)", () => {
    // The `saolei_9` shape: grid all-revealed/flagged, but 11 flags on a
    // 10-mine board ⇒ counter `-01` ⇒ flags ≠ mines ⇒ not a win.
    const state = board(
      [
        ["0", "1", "FLAG"],
        ["2", "FLAG", "3"],
        ["FLAG", "4", "0"],
      ],
      COUNTER_NEG_ONE,
    );
    expect(isWin(state)).toBe(false);
  });

  it("returns false when the counter reads 000 but the grid has an INITIAL cell (FR-007)", () => {
    // The `saolei_11` shape: counter `000` (flags == mines) but the grid is
    // not fully revealed — counter-alone is not a win.
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "INITIAL", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(false);
  });

  it("returns false when mineCounter is undefined (lenient, FR-008)", () => {
    // An all-revealed grid with no counter populated (e.g. a synthetic state)
    // ⇒ undefined counter ⇒ never claim a win on an absent counter.
    const state = board([
      ["0", "1", "2"],
      ["3", "4", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(false);
  });

  it("returns false when mineCounter is undecodable (lenient, FR-008)", () => {
    // An all-revealed grid with `{ decoded: false }` ⇒ never claim a win on
    // an uncertain counter.
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "4", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_UNDECODABLE,
    );
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is HIT_MINE even if the counter reads 000 (FR-010)", () => {
    // Loss takes precedence over win: a HIT_MINE cell already fails the grid
    // half, so the predicate returns false regardless of the counter.
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "HIT_MINE", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is MINE even if the counter reads 000 (FR-010)", () => {
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "MINE", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is UNKNOWN (otherwise a win)", () => {
    const state = board(
      [
        ["0", "1", "2"],
        ["3", "UNKNOWN", "5"],
        ["6", "7", "8"],
      ],
      COUNTER_ZERO,
    );
    expect(isWin(state)).toBe(false);
  });
});
