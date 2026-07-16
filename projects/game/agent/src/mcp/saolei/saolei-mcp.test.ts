/**
 * saolei-mcp.test.ts — Tests for the per-session SaoleiMcp instance (T012).
 *
 * Covers FR-025a/b/c: the instance owns a board that starts uninitialized,
 * and each instance is independent (per-session isolation).
 */

import { describe, expect, it } from "vitest";

import { SaoleiMcp } from "./saolei-mcp";

describe("SaoleiMcp", () => {
  it("exposes a board that starts uninitialized", () => {
    const mcp = new SaoleiMcp();
    const board = mcp.getBoard();
    expect(board.lifecycle).toBe("uninitialized");
  });

  it("the board is the same instance across getBoard calls (stateful)", () => {
    const mcp = new SaoleiMcp();
    const board = mcp.getBoard();
    board.init(3, 3);

    expect(mcp.getBoard()).toBe(board);
    expect(mcp.getBoard().width).toBe(3);
  });

  it("two instances hold independent boards (per-session isolation, FR-025b)", () => {
    const a = new SaoleiMcp();
    const b = new SaoleiMcp();
    a.getBoard().init(9, 9);

    expect(b.getBoard().lifecycle).toBe("uninitialized");
    expect(b.getBoard().width).toBe(0);
    expect(a.getBoard()).not.toBe(b.getBoard());
  });
});
