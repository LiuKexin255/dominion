/**
 * geometry.test.ts — Tests for Saolei coordinate geometry (T006).
 *
 * Asserts the pinned constants (TOP_OFFSET=200, LEFT_OFFSET=24,
 * BLOCK_LENGTH=32), the cell-centre formula from data-model.md §4, and the
 * in-bounds check (FR-021).
 */

import { describe, expect, it } from "vitest";

import {
  BLOCK_LENGTH,
  LEFT_OFFSET,
  TOP_OFFSET,
  cellCentre,
  inBounds,
} from "./geometry";

describe("geometry constants (data-model.md §4)", () => {
  it("pins TOP_OFFSET=200 px", () => {
    expect(TOP_OFFSET).toBe(200);
  });

  it("pins LEFT_OFFSET=24 px", () => {
    expect(LEFT_OFFSET).toBe(24);
  });

  it("pins BLOCK_LENGTH=32 px", () => {
    expect(BLOCK_LENGTH).toBe(32);
  });
});

describe("cellCentre", () => {
  it("computes (0,0) → (40,216) per the data-model example", () => {
    expect(cellCentre(0, 0)).toEqual({ x: 40, y: 216 });
  });

  it("computes (1,1) → (72,248) per the data-model example", () => {
    expect(cellCentre(1, 1)).toEqual({ x: 72, y: 248 });
  });

  it("matches the formula X = 24 + x*32 + 16, Y = 200 + y*32 + 16", () => {
    for (const { x, y } of [
      { x: 0, y: 0 },
      { x: 4, y: 4 },
      { x: 8, y: 8 },
      { x: 29, y: 15 },
    ]) {
      const got = cellCentre(x, y);
      expect(got.x).toBe(24 + x * 32 + 16);
      expect(got.y).toBe(200 + y * 32 + 16);
    }
  });

  it("every cell centre in a 9×9 board is within the board pixel area", () => {
    const width = 9;
    const height = 9;
    const boardRight = LEFT_OFFSET + width * BLOCK_LENGTH;
    const boardBottom = TOP_OFFSET + height * BLOCK_LENGTH;
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < width; x++) {
        const p = cellCentre(x, y);
        expect(p.x).toBeGreaterThan(LEFT_OFFSET);
        expect(p.x).toBeLessThan(boardRight);
        expect(p.y).toBeGreaterThan(TOP_OFFSET);
        expect(p.y).toBeLessThan(boardBottom);
      }
    }
  });
});

describe("inBounds (FR-021)", () => {
  it("accepts coordinates inside the board", () => {
    expect(inBounds(0, 0, 9, 9)).toBe(true);
    expect(inBounds(8, 8, 9, 9)).toBe(true);
    expect(inBounds(4, 4, 9, 9)).toBe(true);
  });

  it("rejects the top-left corner of an uninitialized board", () => {
    expect(inBounds(0, 0, 0, 0)).toBe(false);
  });

  it("rejects x at the width boundary (x == width)", () => {
    expect(inBounds(9, 0, 9, 9)).toBe(false);
  });

  it("rejects y at the height boundary (y == height)", () => {
    expect(inBounds(0, 9, 9, 9)).toBe(false);
  });

  it("rejects negative coordinates", () => {
    expect(inBounds(-1, 0, 9, 9)).toBe(false);
    expect(inBounds(0, -1, 9, 9)).toBe(false);
  });
});
