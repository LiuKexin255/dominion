/**
 * board.test.ts — Tests for the Saolei board state machine (T005).
 *
 * Covers cell-state enumeration, the lifecycle transitions
 * (uninitialized→ready→awaiting-update→ready, terminal on boom), `init` reset
 * from any state, grid row-major indexing, and atomic commit semantics — per
 * data-model.md §1-2 and FR-009..012/023.
 */

import { describe, expect, it } from "vitest";

import {
  BoardState,
  CELL_STATES,
  NUMBER_CELL_STATES,
} from "./board";
import type { CellState } from "./board";

describe("CellState enumeration", () => {
  it("lists all twelve states in display order", () => {
    expect(CELL_STATES).toEqual([
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
    ]);
  });

  it("number states are zero through eight", () => {
    expect(NUMBER_CELL_STATES).toEqual([
      "zero",
      "one",
      "two",
      "three",
      "four",
      "five",
      "six",
      "seven",
      "eight",
    ]);
  });
});

describe("BoardState", () => {
  it("starts uninitialized with an empty grid", () => {
    const board = new BoardState();
    expect(board.lifecycle).toBe("uninitialized");
    expect(board.width).toBe(0);
    expect(board.height).toBe(0);
    expect(board.grid).toEqual([]);
  });

  describe("init", () => {
    it("populates a width×height grid of block cells and enters ready", () => {
      const board = new BoardState();
      board.init(3, 2);

      expect(board.width).toBe(3);
      expect(board.height).toBe(2);
      expect(board.lifecycle).toBe("ready");
      expect(board.grid).toHaveLength(2);
      expect(board.grid[0]).toEqual(["block", "block", "block"]);
      expect(board.grid[1]).toEqual(["block", "block", "block"]);
    });

    it("init from ready resets the whole grid to block", () => {
      const board = new BoardState();
      board.init(2, 2);
      board.enterAwaitingUpdate();
      board.commitUpdate([
        { x: 0, y: 0, state: "one" },
        { x: 1, y: 1, state: "flag" },
      ]);

      board.init(2, 2);

      expect(board.lifecycle).toBe("ready");
      expect(board.grid[0]).toEqual(["block", "block"]);
      expect(board.grid[1]).toEqual(["block", "block"]);
    });

    it("init from terminal returns to ready (FR-009 edge case)", () => {
      const board = new BoardState();
      board.init(2, 2);
      board.enterAwaitingUpdate();
      board.commitUpdate([{ x: 0, y: 0, state: "boom" }]);
      expect(board.lifecycle).toBe("terminal");

      board.init(2, 2);

      expect(board.lifecycle).toBe("ready");
      expect(board.cellAt(0, 0)).toBe("block");
    });

    it("resizes the grid on re-init", () => {
      const board = new BoardState();
      board.init(9, 9);
      board.init(3, 3);

      expect(board.width).toBe(3);
      expect(board.height).toBe(3);
      expect(board.grid).toHaveLength(3);
      expect(board.grid[0]).toHaveLength(3);
    });

    it.each([
      ["zero width", 0, 3],
      ["zero height", 3, 0],
      ["negative width", -1, 3],
      ["negative height", 3, -2],
      ["non-integer width", 2.5, 3],
    ])("rejects non-positive/non-integer dimensions: %s", (_label, w, h) => {
      const board = new BoardState();
      expect(() => board.init(w, h)).toThrow(/positive integer/);
      // A rejected init leaves the board untouched.
      expect(board.lifecycle).toBe("uninitialized");
    });
  });

  describe("lifecycle transitions", () => {
    it("ready → awaiting-update on enterAwaitingUpdate", () => {
      const board = new BoardState();
      board.init(2, 2);

      board.enterAwaitingUpdate();

      expect(board.lifecycle).toBe("awaiting-update");
    });

    it("awaiting-update → ready when update has no boom", () => {
      const board = new BoardState();
      board.init(2, 2);
      board.enterAwaitingUpdate();

      board.commitUpdate([{ x: 0, y: 0, state: "zero" }]);

      expect(board.lifecycle).toBe("ready");
    });

    it("awaiting-update → terminal when update contains a boom (FR-023)", () => {
      const board = new BoardState();
      board.init(2, 2);
      board.enterAwaitingUpdate();

      board.commitUpdate([
        { x: 0, y: 0, state: "three" },
        { x: 1, y: 1, state: "boom" },
      ]);

      expect(board.lifecycle).toBe("terminal");
      expect(board.cellAt(0, 0)).toBe("three");
      expect(board.cellAt(1, 1)).toBe("boom");
    });

    it("awaiting-update → ready even when flag is in the batch", () => {
      const board = new BoardState();
      board.init(2, 2);
      board.enterAwaitingUpdate();

      board.commitUpdate([{ x: 0, y: 0, state: "flag" }]);

      expect(board.lifecycle).toBe("ready");
      expect(board.cellAt(0, 0)).toBe("flag");
    });
  });

  describe("commitUpdate writes the grid", () => {
    it("writes each reported cell at grid[y][x] (row-major)", () => {
      const board = new BoardState();
      board.init(3, 3);
      board.enterAwaitingUpdate();

      board.commitUpdate([
        { x: 2, y: 0, state: "one" },
        { x: 0, y: 2, state: "five" },
      ]);

      expect(board.cellAt(2, 0)).toBe("one");
      expect(board.cellAt(0, 2)).toBe("five");
      expect(board.cellAt(1, 1)).toBe("block");
    });

    it("all terminal cell states can be written", () => {
      const states: CellState[] = [
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
      const board = new BoardState();
      board.init(states.length, 1);
      board.enterAwaitingUpdate();

      board.commitUpdate(states.map((state, x) => ({ x, y: 0, state })));

      for (let x = 0; x < states.length; x++) {
        expect(board.cellAt(x, 0)).toBe(states[x]);
      }
    });
  });

  describe("toSummary", () => {
    it("echoes width, height, and lifecycle", () => {
      const board = new BoardState();
      board.init(9, 9);
      board.enterAwaitingUpdate();

      expect(board.toSummary()).toEqual({
        width: 9,
        height: 9,
        lifecycle: "awaiting-update",
      });
    });
  });
});
