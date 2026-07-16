/**
 * validation.test.ts — Tests for the Saolei legality rules (T007).
 *
 * Covers FR-016..023 from data-model.md §8: pre-init, awaiting-update block,
 * click non-block, chord non-number, flag target, out-of-bounds, illegal
 * saolei_update transition (atomic batch), and terminal-after-boom. Each rule
 * returns a machine-readable `{ reason }`; legal operations return `null`.
 */

import { describe, expect, it } from "vitest";

import { BoardState } from "./board";
import type { CellUpdate } from "./board";
import {
  validateChordTarget,
  validateClickTarget,
  validateFlagTarget,
  validatePositionalOperation,
  validateUpdateBatch,
} from "./validation";

function readyBoard(width = 3, height = 3): BoardState {
  const board = new BoardState();
  board.init(width, height);
  return board;
}

function awaitingBoard(width = 3, height = 3): BoardState {
  const board = readyBoard(width, height);
  board.enterAwaitingUpdate();
  return board;
}

describe("validatePositionalOperation", () => {
  it("rejects any operation before saolei_init (FR-016)", () => {
    const board = new BoardState();
    const got = validatePositionalOperation(board, 0, 0);
    expect(got).toEqual({ reason: "not-initialized" });
  });

  it("rejects an operation while awaiting-update (FR-017)", () => {
    const board = awaitingBoard();
    const got = validatePositionalOperation(board, 1, 1);
    expect(got).toEqual({ reason: "awaiting-update" });
  });

  it("rejects an operation after boom/terminal (FR-023)", () => {
    const board = readyBoard();
    board.enterAwaitingUpdate();
    board.commitUpdate([{ x: 0, y: 0, state: "boom" }]);
    const got = validatePositionalOperation(board, 1, 1);
    expect(got).toEqual({ reason: "terminal" });
  });

  it("rejects an out-of-bounds coordinate (FR-021)", () => {
    const board = readyBoard(3, 3);
    const got = validatePositionalOperation(board, 3, 0);
    expect(got).toEqual({ reason: "out-of-bounds" });
  });

  it("accepts a ready, in-bounds coordinate", () => {
    const board = readyBoard(3, 3);
    const got = validatePositionalOperation(board, 2, 2);
    expect(got).toBeNull();
  });
});

describe("validateClickTarget (FR-018)", () => {
  it("accepts a block cell", () => {
    const board = readyBoard();
    expect(validateClickTarget(board, 0, 0)).toBeNull();
  });

  it("rejects a revealed number cell", () => {
    const board = readyBoard();
    board.grid[0][0] = "one";
    const got = validateClickTarget(board, 0, 0);
    expect(got).toEqual({ reason: "cell-not-block" });
  });

  it("rejects a flagged cell", () => {
    const board = readyBoard();
    board.grid[1][1] = "flag";
    const got = validateClickTarget(board, 1, 1);
    expect(got).toEqual({ reason: "cell-not-block" });
  });
});

describe("validateFlagTarget", () => {
  it("accepts a block cell", () => {
    const board = readyBoard();
    expect(validateFlagTarget(board, 0, 0)).toBeNull();
  });

  it("accepts an already-flagged cell (toggle off)", () => {
    const board = readyBoard();
    board.grid[0][0] = "flag";
    expect(validateFlagTarget(board, 0, 0)).toBeNull();
  });

  it("rejects a revealed number cell", () => {
    const board = readyBoard();
    board.grid[0][0] = "three";
    const got = validateFlagTarget(board, 0, 0);
    expect(got).toEqual({ reason: "cell-not-block-and-not-flag" });
  });

  it("rejects a boom cell", () => {
    const board = readyBoard();
    board.grid[0][0] = "boom";
    const got = validateFlagTarget(board, 0, 0);
    expect(got).toEqual({ reason: "cell-not-block-and-not-flag" });
  });
});

describe("validateChordTarget (FR-019)", () => {
  it.each([
    ["zero", "zero"],
    ["one", "one"],
    ["eight", "eight"],
  ] as const)("accepts number cell %s", (_label, state) => {
    const board = readyBoard();
    board.grid[1][1] = state;
    expect(validateChordTarget(board, 1, 1)).toBeNull();
  });

  it("rejects a block cell", () => {
    const board = readyBoard();
    const got = validateChordTarget(board, 0, 0);
    expect(got).toEqual({ reason: "cell-not-number" });
  });

  it("rejects a flag cell", () => {
    const board = readyBoard();
    board.grid[0][0] = "flag";
    const got = validateChordTarget(board, 0, 0);
    expect(got).toEqual({ reason: "cell-not-number" });
  });
});

describe("validateUpdateBatch", () => {
  it("rejects when the board is not awaiting-update", () => {
    const board = readyBoard();
    const cells: CellUpdate[] = [{ x: 0, y: 0, state: "one" }];
    const got = validateUpdateBatch(board, cells);
    expect(got).toEqual({ reason: "not-awaiting-update" });
  });

  it("accepts a legal reveal batch (block → number)", () => {
    const board = awaitingBoard();
    const cells: CellUpdate[] = [
      { x: 0, y: 0, state: "zero" },
      { x: 1, y: 1, state: "two" },
    ];
    expect(validateUpdateBatch(board, cells)).toBeNull();
  });

  it("accepts placing a flag (block → flag) and clearing one (flag → block)", () => {
    const board = awaitingBoard();
    board.grid[2][2] = "flag";
    const cells: CellUpdate[] = [
      { x: 0, y: 0, state: "flag" },
      { x: 2, y: 2, state: "block" },
    ];
    expect(validateUpdateBatch(board, cells)).toBeNull();
  });

  it("accepts reporting a boom (block → boom)", () => {
    const board = awaitingBoard();
    expect(
      validateUpdateBatch(board, [{ x: 0, y: 0, state: "boom" }]),
    ).toBeNull();
  });

  it("accepts idempotent re-reporting (cell to itself)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "five";
    expect(
      validateUpdateBatch(board, [{ x: 0, y: 0, state: "five" }]),
    ).toBeNull();
  });

  it("rejects a number transitioning back to block (FR-022)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "one";
    const got = validateUpdateBatch(board, [
      { x: 0, y: 0, state: "block" },
    ]);
    expect(got?.reason).toContain("terminal-cell-locked");
    expect(got?.reason).toContain("(0,0)");
  });

  it("rejects flagging a revealed number (FR-020)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "three";
    const got = validateUpdateBatch(board, [
      { x: 0, y: 0, state: "flag" },
    ]);
    expect(got?.reason).toContain("terminal-cell-locked");
  });

  it("rejects a flag transitioning to a number (FR-020)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "flag";
    const got = validateUpdateBatch(board, [
      { x: 0, y: 0, state: "one" },
    ]);
    expect(got?.reason).toContain("illegal-flag-transition");
  });

  it("rejects boom transitioning to anything (terminal cell)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "boom";
    const got = validateUpdateBatch(board, [
      { x: 0, y: 0, state: "block" },
    ]);
    expect(got?.reason).toContain("terminal-cell-locked");
  });

  it("rejects atomically when ANY cell in the batch is illegal (SC-007)", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "one"; // terminal — illegal to change
    const cells: CellUpdate[] = [
      { x: 1, y: 1, state: "two" }, // legal
      { x: 0, y: 0, state: "block" }, // illegal
    ];
    const got = validateUpdateBatch(board, cells);
    expect(got?.reason).toContain("illegal-transition");
    expect(got?.reason).toContain("(0,0)");
    // The legal cell is not separately called out, only offenders are listed.
  });

  it("rejects an out-of-bounds coordinate in the batch (FR-021)", () => {
    const board = awaitingBoard(3, 3);
    const got = validateUpdateBatch(board, [
      { x: 5, y: 0, state: "one" },
    ]);
    expect(got?.reason).toContain("out-of-bounds");
    expect(got?.reason).toContain("(5,0)");
  });

  it("lists multiple offending cells in a single rejection", () => {
    const board = awaitingBoard();
    board.grid[0][0] = "one";
    board.grid[1][1] = "two";
    const got = validateUpdateBatch(board, [
      { x: 0, y: 0, state: "block" },
      { x: 1, y: 1, state: "flag" },
    ]);
    expect(got?.reason).toContain("(0,0)");
    expect(got?.reason).toContain("(1,1)");
  });
});
