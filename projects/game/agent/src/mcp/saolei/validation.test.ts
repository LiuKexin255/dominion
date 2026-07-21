/**
 * validation.test.ts — Table-driven unit tests for the saolei validators.
 *
 * Pattern (`style/javascript.md` §测试): pure functions, no `vi.mock`,
 * every case is a row in a `cases: [...]` table driven by `describe.each`/
 * `it.each` so accept/reject coverage is enumerable and auditable. The
 * validators take a `GameState` and return a `ValidationResult`; no
 * collaborator injection is needed.
 *
 * Coverage (Phase 5 / US2 — T012):
 *   - `isConnected8` — 8-connectivity helper (single, adjacent, diagonal,
 *     far-apart, two components, duplicates).
 *   - `validateRange` — FR-016 in-bounds accept / out-of-bounds reject.
 *   - `validateClickPreDispatch` — FR-013 INITIAL accept / non-INITIAL
 *     reject (number, FLAG, HIT_MINE, MINE) / out-of-bounds reject.
 *   - `validateClickUpdate` — FR-013 + FR-018: target missing reject,
 *     target invalid-status reject, single number accept, cascade
 *     connected accept, disconnected reject, HIT_MINE game-over accept.
 *   - `validateUpdate` — routing: range failure short-circuits; flag /
 *     chord_click kinds reject as Phase-6 stubs.
 */

import { describe, expect, it } from "vitest";

import { CellStatus } from "./game-state";
import type { GameState, LastOp } from "./game-state";
import {
  isConnected8,
  validateClickPreDispatch,
  validateClickUpdate,
  validateRange,
  validateUpdate,
} from "./validation";
import type { CellUpdate } from "./validation";

/**
 * Build a fresh `width`×`height` grid filled with `fill` (default INITIAL).
 * Tests use this to assemble a `GameState` for a specific scenario without
 * recreating the production `createGameState` initialisation semantics.
 */
function makeState(
  width: number,
  height: number,
  fill: CellStatus = CellStatus.INITIAL,
): GameState {
  const grid: CellStatus[][] = [];
  for (let y = 0; y < height; y++) {
    const row: CellStatus[] = [];
    for (let x = 0; x < width; x++) row.push(fill);
    grid.push(row);
  }
  return {
    grid,
    width,
    height,
    pendingUpdate: false,
    lastOp: null,
    initialized: true,
  };
}

/** Place a single status into a `makeState`-built grid (mutates in place). */
function setCell(
  state: GameState,
  x: number,
  y: number,
  status: CellStatus,
): void {
  state.grid[y][x] = status;
}

const clickOp = (x: number, y: number): LastOp => ({
  kind: "click",
  target: { x, y },
});

describe("isConnected8", () => {
  it.each([
    {
      name: "empty is vacuously connected",
      cells: [],
      expected: true,
    },
    {
      name: "single cell is connected",
      cells: [{ x: 0, y: 0 }],
      expected: true,
    },
    {
      name: "horizontally adjacent",
      cells: [
        { x: 0, y: 0 },
        { x: 1, y: 0 },
      ],
      expected: true,
    },
    {
      name: "vertically adjacent",
      cells: [
        { x: 0, y: 0 },
        { x: 0, y: 1 },
      ],
      expected: true,
    },
    {
      name: "diagonally adjacent (8-connectivity)",
      cells: [
        { x: 0, y: 0 },
        { x: 1, y: 1 },
      ],
      expected: true,
    },
    {
      name: "chain through intermediates",
      cells: [
        { x: 0, y: 0 },
        { x: 1, y: 0 },
        { x: 2, y: 0 },
        { x: 2, y: 1 },
      ],
      expected: true,
    },
    {
      name: "far apart (Chebyshev > 1) is disconnected",
      cells: [
        { x: 0, y: 0 },
        { x: 2, y: 0 },
      ],
      expected: false,
    },
    {
      name: "two separate components",
      cells: [
        { x: 0, y: 0 },
        { x: 1, y: 0 },
        { x: 5, y: 5 },
        { x: 6, y: 5 },
      ],
      expected: false,
    },
    {
      name: "duplicate coordinates collapse to one cell",
      cells: [
        { x: 0, y: 0 },
        { x: 0, y: 0 },
        { x: 1, y: 0 },
      ],
      expected: true,
    },
  ])("$name", ({ cells, expected }) => {
    expect(isConnected8(cells)).toBe(expected);
  });
});

describe("validateRange (FR-016)", () => {
  it("accepts all in-bounds coordinates", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 0, y: 0, status: CellStatus.NUMBER_1 },
      { x: 2, y: 2, status: CellStatus.NUMBER_0 },
    ];
    expect(validateRange(state, cells)).toEqual({ ok: true });
  });

  it.each([
    {
      name: "x negative",
      cell: { x: -1, y: 0, status: CellStatus.NUMBER_1 },
    },
    {
      name: "y negative",
      cell: { x: 0, y: -1, status: CellStatus.NUMBER_1 },
    },
    {
      name: "x >= width",
      cell: { x: 3, y: 0, status: CellStatus.NUMBER_1 },
    },
    {
      name: "y >= height",
      cell: { x: 0, y: 3, status: CellStatus.NUMBER_1 },
    },
  ])("rejects out-of-bounds: $name", ({ cell }) => {
    const state = makeState(3, 3);
    const result = validateRange(state, [cell]);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("out of bounds");
      expect(result.reason).toContain(`(${cell.x},${cell.y})`);
    }
  });
});

describe("validateClickPreDispatch (FR-013)", () => {
  it("accepts an INITIAL target", () => {
    const state = makeState(3, 3);
    expect(validateClickPreDispatch(state, { x: 1, y: 1 })).toEqual({
      ok: true,
    });
  });

  it.each([
    {
      name: "revealed number",
      status: CellStatus.NUMBER_1,
    },
    {
      name: "revealed 0",
      status: CellStatus.NUMBER_0,
    },
    {
      name: "FLAG",
      status: CellStatus.FLAG,
    },
    {
      name: "HIT_MINE",
      status: CellStatus.HIT_MINE,
    },
    {
      name: "MINE",
      status: CellStatus.MINE,
    },
  ])("rejects a non-INITIAL target ($name)", ({ status }) => {
    const state = makeState(3, 3);
    setCell(state, 1, 1, status);
    const result = validateClickPreDispatch(state, { x: 1, y: 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("not INITIAL");
      expect(result.reason).toContain(`(1,1)`);
      expect(result.reason).toContain(`current=${status}`);
    }
  });

  it.each([
    { name: "x negative", target: { x: -1, y: 0 } },
    { name: "x >= width", target: { x: 3, y: 0 } },
    { name: "y negative", target: { x: 0, y: -1 } },
    { name: "y >= height", target: { x: 0, y: 3 } },
  ])("rejects out-of-bounds target: $name", ({ target }) => {
    const state = makeState(3, 3);
    const result = validateClickPreDispatch(state, target);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("out of bounds");
    }
  });
});

describe("validateClickUpdate (FR-013 + FR-018)", () => {
  it("rejects when the target cell is missing from the batch", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 0, y: 0, status: CellStatus.NUMBER_1 },
    ];
    const result = validateClickUpdate(state, clickOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("must include the target cell");
      expect(result.reason).toContain("(1,1)");
    }
  });

  it.each([
    { name: "INITIAL (no change)", status: CellStatus.INITIAL },
    { name: "FLAG", status: CellStatus.FLAG },
    { name: "MINE (non-triggered)", status: CellStatus.MINE },
  ])(
    "rejects when the target transitions to a non-number non-HIT_MINE status ($name)",
    ({ status }) => {
      const state = makeState(3, 3);
      const cells: CellUpdate[] = [
        { x: 1, y: 1, status },
      ];
      const result = validateClickUpdate(state, clickOp(1, 1), cells);
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.reason).toContain("must change target");
        expect(result.reason).toContain("0..8 or HIT_MINE");
      }
    },
  );

  it("accepts a single-cell number reveal (trivially connected)", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.NUMBER_3 },
    ];
    expect(validateClickUpdate(state, clickOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("accepts a connected cascade of number cells including the target", () => {
    const state = makeState(5, 5);
    // Target at (1,1) becomes 0; cascade reveals a connected region of
    // 0-cells plus a 1-cell on the boundary.
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.NUMBER_0 },
      { x: 2, y: 1, status: CellStatus.NUMBER_0 },
      { x: 1, y: 2, status: CellStatus.NUMBER_0 },
      { x: 2, y: 2, status: CellStatus.NUMBER_0 },
      { x: 3, y: 2, status: CellStatus.NUMBER_1 },
    ];
    expect(validateClickUpdate(state, clickOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("rejects disconnected number cells (FR-013 connectivity)", () => {
    const state = makeState(5, 5);
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.NUMBER_0 },
      { x: 4, y: 4, status: CellStatus.NUMBER_1 },
    ];
    const result = validateClickUpdate(state, clickOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("8-connected");
      expect(result.reason).contains("(1,1)");
    }
  });

  it("accepts HIT_MINE on the target with no connectivity requirement (FR-018)", () => {
    const state = makeState(3, 3);
    // Target hit a mine → game over; other coordinates may be absent or
    // disconnected; the validator must not enforce connectivity.
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.HIT_MINE },
    ];
    expect(validateClickUpdate(state, clickOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("accepts HIT_MINE alongside disconnected MINE reveals at game end", () => {
    const state = makeState(5, 5);
    // Game-over batch: target is HIT_MINE, other mines are revealed as
    // MINE in arbitrary positions — connectivity is irrelevant.
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.HIT_MINE },
      { x: 4, y: 4, status: CellStatus.MINE },
      { x: 0, y: 0, status: CellStatus.MINE },
    ];
    expect(validateClickUpdate(state, clickOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });
});

describe("validateUpdate dispatcher", () => {
  it("runs the FR-016 range check first and short-circuits on failure", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 99, y: 0, status: CellStatus.NUMBER_1 },
    ];
    const result = validateUpdate(state, clickOp(0, 0), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("out of bounds");
    }
  });

  it("routes `click` to the click validator (accept path)", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 0, y: 0, status: CellStatus.NUMBER_1 },
    ];
    expect(validateUpdate(state, clickOp(0, 0), cells)).toEqual({ ok: true });
  });

  it.each([
    {
      kind: "flag" as const,
      label: "flag (Phase 6)",
    },
    {
      kind: "chord_click" as const,
      label: "chord_click (Phase 6)",
    },
  ])(
    "rejects `$label` lastOp kind as not-yet-implemented",
    ({ kind }) => {
      const state = makeState(3, 3);
      const lastOp: LastOp = { kind, target: { x: 0, y: 0 } };
      const cells: CellUpdate[] = [
        { x: 0, y: 0, status: CellStatus.FLAG },
      ];
      const result = validateUpdate(state, lastOp, cells);
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.reason).toContain("not yet implemented");
        expect(result.reason).toContain("Phase 6");
      }
    },
  );
});
