import { describe, expect, it } from "vitest";

import type { CellStatus, GameState } from "./types";
import { isWin } from "./win";

/**
 * win.test.ts — Unit tests for the pure `isWin` predicate.
 *
 * Covers every branch of FR-009/FR-010 (spec
 * `specs/027-chat-bubble-game-state/spec.md`): all-revealed-numbers board ⇒
 * true; all-FLAG board ⇒ true; mixed numbers+flags ⇒ true; any INITIAL ⇒ false;
 * HIT_MINE ⇒ false; MINE ⇒ false; UNKNOWN (otherwise a win) ⇒ false. Pure unit
 * — no DI, no `vi.mock` (style/javascript.md §测试).
 */

/** Build a square `GameState` from row arrays of `CellStatus`. */
function board(rows: CellStatus[][]): GameState {
  const height = rows.length;
  const width = rows[0]?.length ?? 0;
  return { width, height, grid: rows };
}

describe("isWin", () => {
  it("returns true for an all-revealed-numbers board", () => {
    const state = board([
      ["0", "1", "2"],
      ["3", "4", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(true);
  });

  it("returns true for an all-FLAG board", () => {
    const state = board([
      ["FLAG", "FLAG", "FLAG"],
      ["FLAG", "FLAG", "FLAG"],
      ["FLAG", "FLAG", "FLAG"],
    ]);
    expect(isWin(state)).toBe(true);
  });

  it("returns true for a mixed numbers+flags board", () => {
    const state = board([
      ["0", "1", "FLAG"],
      ["2", "FLAG", "3"],
      ["FLAG", "4", "0"],
    ]);
    expect(isWin(state)).toBe(true);
  });

  it("returns false when any cell is INITIAL", () => {
    const state = board([
      ["0", "1", "2"],
      ["3", "INITIAL", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is HIT_MINE", () => {
    const state = board([
      ["0", "1", "2"],
      ["3", "HIT_MINE", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is MINE", () => {
    const state = board([
      ["0", "1", "2"],
      ["3", "MINE", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(false);
  });

  it("returns false when any cell is UNKNOWN (otherwise a win)", () => {
    const state = board([
      ["0", "1", "2"],
      ["3", "UNKNOWN", "5"],
      ["6", "7", "8"],
    ]);
    expect(isWin(state)).toBe(false);
  });
});
