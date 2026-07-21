/**
 * validation.test.ts — Table-driven unit tests for the saolei validators.
 *
 * Pattern (`style/javascript.md` §测试): pure functions, no `vi.mock`,
 * every case is a row in a `cases: [...]` table driven by `describe.each`/
 * `it.each` so accept/reject coverage is enumerable and auditable. The
 * validators take a `GameState` and return a `ValidationResult`; no
 * collaborator injection is needed.
 *
 * Coverage:
 *   - `isConnected8` — 8-connectivity helper (single, adjacent, diagonal,
 *     far-apart, two components, duplicates).
 *   - `isAdjacentTo` — Chebyshev-1 predicate (self, orthogonal, diagonal,
 *     far, anti-symmetry).
 *   - `connectedComponents` — BFS partitioning (empty, single, two
 *     components, duplicates, chain).
 *   - `validateRange` — FR-016 in-bounds accept / out-of-bounds reject.
 *   - `validateClickPreDispatch` — FR-013 INITIAL accept / non-INITIAL
 *     reject / out-of-bounds reject.
 *   - `validateClickUpdate` — FR-013 + FR-018: target missing reject,
 *     target invalid-status reject, single number accept, cascade
 *     connected accept, disconnected reject, HIT_MINE game-over accept.
 *   - `validateFlagPreDispatch` (Phase 6) — FR-014 INITIAL accept /
 *     non-INITIAL reject / out-of-bounds reject.
 *   - `validateFlagUpdate` (Phase 6) — FR-014 single-cell INITIAL→FLAG
 *     accept, target missing reject, non-FLAG target status reject,
 *     extraneous cell reject.
 *   - `validateChordPreDispatch` (Phase 6) — FR-015 satisfied number
 *     accept, non-number reject, NUMBER_0 reject, flag count ≠ number
 *     reject, out-of-bounds reject.
 *   - `validateChordUpdate` (Phase 6) — FR-015 + FR-019: target-adjacent
 *     FLAG preservation, other neighbors updated, mine-hit exception,
 *     connectivity-via-target-neighborhood.
 *   - `validateUpdate` dispatcher (Phase 6) — routing: range failure
 *     short-circuits; flag / chord_click route to their validators.
 */

import { describe, expect, it } from "vitest";

import { CellStatus } from "./game-state";
import type { GameState, LastOp } from "./game-state";
import {
  connectedComponents,
  isAdjacentTo,
  isConnected8,
  validateChordPreDispatch,
  validateChordUpdate,
  validateClickPreDispatch,
  validateClickUpdate,
  validateFlagPreDispatch,
  validateFlagUpdate,
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

const flagOp = (x: number, y: number): LastOp => ({
  kind: "flag",
  target: { x, y },
});

const chordOp = (x: number, y: number): LastOp => ({
  kind: "chord_click",
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

describe("isAdjacentTo (8-connectivity predicate)", () => {
  it.each([
    {
      name: "self is NOT adjacent (Chebyshev=0)",
      a: { x: 1, y: 1 },
      b: { x: 1, y: 1 },
      expected: false,
    },
    {
      name: "horizontal neighbour",
      a: { x: 1, y: 1 },
      b: { x: 2, y: 1 },
      expected: true,
    },
    {
      name: "vertical neighbour",
      a: { x: 1, y: 1 },
      b: { x: 1, y: 2 },
      expected: true,
    },
    {
      name: "diagonal neighbour",
      a: { x: 1, y: 1 },
      b: { x: 2, y: 2 },
      expected: true,
    },
    {
      name: "Chebyshev=2 horizontally is NOT adjacent",
      a: { x: 1, y: 1 },
      b: { x: 3, y: 1 },
      expected: false,
    },
    {
      name: "Chebyshev=2 diagonally is NOT adjacent",
      a: { x: 0, y: 0 },
      b: { x: 2, y: 2 },
      expected: false,
    },
  ])("$name", ({ a, b, expected }) => {
    expect(isAdjacentTo(a, b)).toBe(expected);
    // Anti-symmetry: adjacency is symmetric.
    expect(isAdjacentTo(b, a)).toBe(expected);
  });
});

describe("connectedComponents (8-connectivity BFS)", () => {
  it("returns an empty array for an empty input", () => {
    expect(connectedComponents([])).toEqual([]);
  });

  it("returns a single component for one cell", () => {
    const comps = connectedComponents([{ x: 0, y: 0 }]);
    expect(comps).toHaveLength(1);
    expect(comps[0]).toEqual([{ x: 0, y: 0 }]);
  });

  it("groups adjacent cells into a single component", () => {
    const cells = [
      { x: 0, y: 0 },
      { x: 1, y: 0 },
      { x: 1, y: 1 },
    ];
    const comps = connectedComponents(cells);
    expect(comps).toHaveLength(1);
    expect(comps[0]).toHaveLength(3);
  });

  it("splits far-apart cells into separate components", () => {
    const cells = [
      { x: 0, y: 0 },
      { x: 5, y: 5 },
    ];
    const comps = connectedComponents(cells);
    expect(comps).toHaveLength(2);
  });

  it("splits a two-region cluster into two components", () => {
    const cells = [
      { x: 0, y: 0 },
      { x: 1, y: 0 },
      { x: 5, y: 5 },
      { x: 6, y: 5 },
    ];
    const comps = connectedComponents(cells);
    expect(comps).toHaveLength(2);
    const sizes = comps.map((c) => c.length).sort();
    expect(sizes).toEqual([2, 2]);
  });

  it("deduplicates coincident coordinates", () => {
    const cells = [
      { x: 0, y: 0 },
      { x: 0, y: 0 },
      { x: 1, y: 0 },
    ];
    const comps = connectedComponents(cells);
    expect(comps).toHaveLength(1);
    expect(comps[0]).toHaveLength(2);
  });
});

describe("validateFlagPreDispatch (FR-014)", () => {
  it("accepts an INITIAL target", () => {
    const state = makeState(3, 3);
    expect(validateFlagPreDispatch(state, { x: 1, y: 1 })).toEqual({
      ok: true,
    });
  });

  it.each([
    { name: "revealed number", status: CellStatus.NUMBER_1 },
    { name: "revealed 0", status: CellStatus.NUMBER_0 },
    { name: "FLAG", status: CellStatus.FLAG },
    { name: "HIT_MINE", status: CellStatus.HIT_MINE },
    { name: "MINE", status: CellStatus.MINE },
  ])("rejects a non-INITIAL target ($name)", ({ status }) => {
    const state = makeState(3, 3);
    setCell(state, 1, 1, status);
    const result = validateFlagPreDispatch(state, { x: 1, y: 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("not INITIAL");
      expect(result.reason).toContain("(1,1)");
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
    const result = validateFlagPreDispatch(state, target);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("out of bounds");
    }
  });
});

describe("validateFlagUpdate (FR-014)", () => {
  it("accepts a single-cell INITIAL→FLAG transition", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.FLAG },
    ];
    expect(validateFlagUpdate(state, flagOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("rejects when the target cell is missing from the batch", () => {
    const state = makeState(3, 3);
    // The batch is empty in target — only an extraneous cell at (0,0) which
    // is NOT the target (1,1). The validator reports the first failure it
    // detects: the extraneous cell.
    const cells: CellUpdate[] = [
      { x: 0, y: 0, status: CellStatus.FLAG },
    ];
    const result = validateFlagUpdate(state, flagOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("must change only the target cell");
    }
  });

  it.each([
    { name: "INITIAL (no change)", status: CellStatus.INITIAL },
    { name: "number 1", status: CellStatus.NUMBER_1 },
    { name: "number 0", status: CellStatus.NUMBER_0 },
    { name: "HIT_MINE", status: CellStatus.HIT_MINE },
    { name: "MINE", status: CellStatus.MINE },
  ])(
    "rejects a non-FLAG target status ($name) — only INITIAL↔FLAG is permitted",
    ({ status }) => {
      const state = makeState(3, 3);
      const cells: CellUpdate[] = [
        { x: 1, y: 1, status },
      ];
      const result = validateFlagUpdate(state, flagOp(1, 1), cells);
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.reason).toContain("INITIAL↔FLAG");
        expect(result.reason).toContain(`got=${status}`);
      }
    },
  );

  it("rejects an extraneous cell alongside the target toggle", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.FLAG },
      { x: 0, y: 0, status: CellStatus.NUMBER_1 },
    ];
    const result = validateFlagUpdate(state, flagOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("must change only the target cell");
      expect(result.reason).toContain("(0,0)");
    }
  });
});

describe("validateChordPreDispatch (FR-015)", () => {
  /**
   * Build a state whose target cell at (1,1) has the given number, with
   * the listed cells set to FLAG. The grid is 3x3 so all 8 neighbours of
   * (1,1) are in-bounds — useful for satisfied-number tests.
   */
  function makeChordState(
    targetNumber: CellStatus,
    flagCells: ReadonlyArray<{ x: number; y: number }>,
  ): GameState {
    const state = makeState(3, 3);
    setCell(state, 1, 1, targetNumber);
    for (const f of flagCells) setCell(state, f.x, f.y, CellStatus.FLAG);
    return state;
  }

  it("accepts a satisfied number (1 with 1 adjacent flag)", () => {
    const state = makeChordState(CellStatus.NUMBER_1, [{ x: 0, y: 0 }]);
    expect(validateChordPreDispatch(state, { x: 1, y: 1 })).toEqual({
      ok: true,
    });
  });

  it("accepts a satisfied number (2 with 2 adjacent flags)", () => {
    const state = makeChordState(CellStatus.NUMBER_2, [
      { x: 0, y: 0 },
      { x: 2, y: 2 },
    ]);
    expect(validateChordPreDispatch(state, { x: 1, y: 1 })).toEqual({
      ok: true,
    });
  });

  it.each([
    { name: "INITIAL", status: CellStatus.INITIAL },
    { name: "FLAG", status: CellStatus.FLAG },
    { name: "HIT_MINE", status: CellStatus.HIT_MINE },
    { name: "MINE", status: CellStatus.MINE },
    { name: "0 (blank, no mines)", status: CellStatus.NUMBER_0 },
  ])("rejects a non-number / NUMBER_0 target ($name)", ({ status }) => {
    const state = makeState(3, 3);
    setCell(state, 1, 1, status);
    const result = validateChordPreDispatch(state, { x: 1, y: 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("non-0 number 1..8");
      expect(result.reason).toContain(`current=${status}`);
    }
  });

  it("rejects when adjacent flag count is less than the number", () => {
    // Target is "3" with only 2 flags — unsatisfied.
    const state = makeChordState(CellStatus.NUMBER_3, [
      { x: 0, y: 0 },
      { x: 2, y: 2 },
    ]);
    const result = validateChordPreDispatch(state, { x: 1, y: 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("number=3");
      expect(result.reason).toContain("adjacent flags=2");
    }
  });

  it("rejects when adjacent flag count exceeds the number", () => {
    // Target is "1" with 2 flags — over-flagged.
    const state = makeChordState(CellStatus.NUMBER_1, [
      { x: 0, y: 0 },
      { x: 2, y: 2 },
    ]);
    const result = validateChordPreDispatch(state, { x: 1, y: 1 });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("number=1");
      expect(result.reason).toContain("adjacent flags=2");
    }
  });

  it.each([
    { name: "x negative", target: { x: -1, y: 0 } },
    { name: "x >= width", target: { x: 3, y: 0 } },
    { name: "y negative", target: { x: 0, y: -1 } },
    { name: "y >= height", target: { x: 0, y: 3 } },
  ])("rejects out-of-bounds target: $name", ({ target }) => {
    const state = makeState(3, 3);
    const result = validateChordPreDispatch(state, target);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("out of bounds");
    }
  });
});

describe("validateChordUpdate (FR-015 + FR-019)", () => {
  /**
   * Build a 3x3 state with target (1,1) a "2", flags at (0,0) and (2,2).
   * Satisfied chord: the 6 non-flag neighbours of (1,1) are
   *   (0,1), (1,0), (1,2), (2,1), (0,2), (2,0)
   * (excluding (0,0)/(2,2) flags and (1,1) target). Tests mutate this
   * baseline to construct accept/reject cases.
   */
  function makeSatisfiedChordState(): GameState {
    const state = makeState(3, 3);
    setCell(state, 1, 1, CellStatus.NUMBER_2);
    setCell(state, 0, 0, CellStatus.FLAG);
    setCell(state, 2, 2, CellStatus.FLAG);
    return state;
  }

  it("accepts a chord update that reveals every non-flag neighbour", () => {
    const state = makeSatisfiedChordState();
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 0, status: CellStatus.NUMBER_1 },
    ];
    expect(validateChordUpdate(state, chordOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("rejects when a target-adjacent FLAG cell is mutated (FR-015 rule 1)", () => {
    const state = makeSatisfiedChordState();
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 0, status: CellStatus.NUMBER_1 },
      { x: 0, y: 0, status: CellStatus.NUMBER_1 }, // FLAG neighbour changed
    ];
    const result = validateChordUpdate(state, chordOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("FLAG");
      expect(result.reason).toContain("(0,0)");
    }
  });

  it("tolerates a FLAG neighbour being re-reported unchanged (no-op)", () => {
    const state = makeSatisfiedChordState();
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 0, status: CellStatus.NUMBER_1 },
      { x: 0, y: 0, status: CellStatus.FLAG }, // explicit no-op
    ];
    expect(validateChordUpdate(state, chordOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("rejects when a target-adjacent INITIAL cell is left un-updated (FR-015 rule 2)", () => {
    const state = makeSatisfiedChordState();
    // Reveal only 5 of the 6 non-flag neighbours — (2,0) is missing.
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
    ];
    const result = validateChordUpdate(state, chordOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("must update target-adjacent non-number cell");
      expect(result.reason).toContain("(2,0)");
    }
  });

  it("rejects when a target-adjacent INITIAL cell becomes an illegal status (INITIAL)", () => {
    const state = makeSatisfiedChordState();
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 0, status: CellStatus.INITIAL }, // not a reveal
    ];
    const result = validateChordUpdate(state, chordOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("must become a number or MINE/HIT_MINE");
      expect(result.reason).toContain("(2,0)");
    }
  });

  it("accepts a chord that hits a mine — only the hit-mine cell required (FR-019 exception)", () => {
    const state = makeSatisfiedChordState();
    // Chord detonated the mine at (2,0) — flag was misplaced elsewhere.
    // Other neighbours are NOT required to be updated.
    const cells: CellUpdate[] = [
      { x: 2, y: 0, status: CellStatus.HIT_MINE },
    ];
    expect(validateChordUpdate(state, chordOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("accepts a chord that hits a mine alongside other game-end MINE reveals", () => {
    // Use a 6x6 grid so the far-away (5,5) MINE reveal is in-bounds.
    const wideState = makeState(6, 6);
    setCell(wideState, 1, 1, CellStatus.NUMBER_2);
    setCell(wideState, 0, 0, CellStatus.FLAG);
    setCell(wideState, 2, 2, CellStatus.FLAG);
    const cells: CellUpdate[] = [
      { x: 2, y: 0, status: CellStatus.HIT_MINE }, // adjacent to target (1,1)
      { x: 5, y: 5, status: CellStatus.MINE }, // irrelevant for connectivity
    ];
    expect(validateChordUpdate(wideState, chordOp(1, 1), cells)).toEqual({
      ok: true,
    });
  });

  it("rejects a HIT_MINE that is not adjacent to the chord target", () => {
    const wideState = makeState(6, 6);
    setCell(wideState, 1, 1, CellStatus.NUMBER_2);
    setCell(wideState, 0, 0, CellStatus.FLAG);
    setCell(wideState, 2, 2, CellStatus.FLAG);
    // (5,5) is far from target (1,1) — a chord cannot detonate it.
    const cells: CellUpdate[] = [
      { x: 5, y: 5, status: CellStatus.HIT_MINE },
    ];
    const result = validateChordUpdate(wideState, chordOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("hit-mine");
      expect(result.reason).toContain("adjacent");
    }
  });

  it("rejects a connected component of number cells not adjacent to the target (FR-015 rule 3)", () => {
    // Target (3,3) is a "1" with flag at (2,2) — satisfied. Reveal every
    // other neighbour (rule 2 passes), plus a far-away connected cluster
    // at (0,0)-(1,0) that is NOT adjacent to (3,3) and forms its own
    // connected component — rule 3 rejects it.
    const wideState = makeState(7, 7);
    setCell(wideState, 3, 3, CellStatus.NUMBER_1);
    setCell(wideState, 2, 2, CellStatus.FLAG);
    const cells: CellUpdate[] = [
      // 7 non-flag neighbours of (3,3) — rule 2 satisfied.
      { x: 2, y: 3, status: CellStatus.NUMBER_1 },
      { x: 2, y: 4, status: CellStatus.NUMBER_1 },
      { x: 3, y: 2, status: CellStatus.NUMBER_1 },
      { x: 3, y: 4, status: CellStatus.NUMBER_1 },
      { x: 4, y: 2, status: CellStatus.NUMBER_1 },
      { x: 4, y: 3, status: CellStatus.NUMBER_1 },
      { x: 4, y: 4, status: CellStatus.NUMBER_1 },
      // Far cluster — own component, NOT adjacent to target (3,3).
      { x: 0, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
    ];
    const result = validateChordUpdate(wideState, chordOp(3, 3), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("connected component");
      expect(result.reason).toContain("not adjacent to the chord target");
    }
  });

  it("accepts a cascade that chains away from the target through adjacent number cells", () => {
    // Target (3,3) is a "1" with flag at (2,2) — satisfied. Chord reveals
    // every non-flag neighbour (rule 2 passes). (2,3) reveals as 0, which
    // cascades into (1,3) — that cell is NOT a target neighbour but is
    // connected through the (2,3) cell which IS target-adjacent, so rule 3
    // passes (the connected component contains (2,3)).
    const wideState = makeState(7, 7);
    setCell(wideState, 3, 3, CellStatus.NUMBER_1);
    setCell(wideState, 2, 2, CellStatus.FLAG);
    const cells: CellUpdate[] = [
      // 7 non-flag neighbours of (3,3) — rule 2 satisfied.
      { x: 2, y: 3, status: CellStatus.NUMBER_0 }, // cascade seed
      { x: 2, y: 4, status: CellStatus.NUMBER_1 },
      { x: 3, y: 2, status: CellStatus.NUMBER_1 },
      { x: 3, y: 4, status: CellStatus.NUMBER_1 },
      { x: 4, y: 2, status: CellStatus.NUMBER_1 },
      { x: 4, y: 3, status: CellStatus.NUMBER_1 },
      { x: 4, y: 4, status: CellStatus.NUMBER_1 },
      // Cascade chain reachable from (2,3) — connected through target's
      // neighbourhood, so the single component contains a target-adjacent
      // cell (rule 3 satisfied).
      { x: 1, y: 3, status: CellStatus.NUMBER_0 },
      { x: 1, y: 4, status: CellStatus.NUMBER_1 },
    ];
    expect(validateChordUpdate(wideState, chordOp(3, 3), cells)).toEqual({
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

  it("routes `flag` to the flag validator (FR-014 accept path)", () => {
    const state = makeState(3, 3);
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.FLAG },
    ];
    expect(validateUpdate(state, flagOp(1, 1), cells)).toEqual({ ok: true });
  });

  it("routes `flag` to the flag validator (FR-014 reject path)", () => {
    const state = makeState(3, 3);
    // Non-FLAG target status — the flag validator must reject this.
    const cells: CellUpdate[] = [
      { x: 1, y: 1, status: CellStatus.NUMBER_1 },
    ];
    const result = validateUpdate(state, flagOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("INITIAL↔FLAG");
    }
  });

  it("routes `chord_click` to the chord validator (FR-015 accept path)", () => {
    // Target (1,1) is a "2" with 2 adjacent flags — satisfied. Update
    // every non-flag neighbour (the 6 INITIAL cells around (1,1)) so the
    // post-chord shape passes rule 2 (other neighbours updated).
    const state = makeState(3, 3);
    setCell(state, 1, 1, CellStatus.NUMBER_2);
    setCell(state, 0, 0, CellStatus.FLAG);
    setCell(state, 2, 2, CellStatus.FLAG);
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 0, y: 2, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 1, y: 2, status: CellStatus.NUMBER_1 },
      { x: 2, y: 0, status: CellStatus.NUMBER_1 },
      { x: 2, y: 1, status: CellStatus.NUMBER_1 },
    ];
    expect(validateUpdate(state, chordOp(1, 1), cells)).toEqual({ ok: true });
  });

  it("routes `chord_click` to the chord validator (FR-015 reject path)", () => {
    // Same setup, but the update mutates a target-adjacent flag — illegal.
    const state = makeState(3, 3);
    setCell(state, 1, 1, CellStatus.NUMBER_2);
    setCell(state, 0, 0, CellStatus.FLAG);
    setCell(state, 2, 0, CellStatus.FLAG);
    const cells: CellUpdate[] = [
      { x: 0, y: 1, status: CellStatus.NUMBER_1 },
      { x: 1, y: 0, status: CellStatus.NUMBER_1 },
      { x: 0, y: 0, status: CellStatus.NUMBER_1 }, // FLAG neighbour changed
    ];
    const result = validateUpdate(state, chordOp(1, 1), cells);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.reason).toContain("FLAG");
    }
  });
});
