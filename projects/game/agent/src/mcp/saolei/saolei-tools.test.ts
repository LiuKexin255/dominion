/**
 * saolei-tools.test.ts — Tests for the five saolei LangChain tools (T013).
 *
 * Covers the Phase 3 independent test: init→click→update lifecycle
 * (ready→awaiting-update→ready), the WINDOW_MESSAGE PartBlock shape with the
 * cell-centre coordinate (US1), and the game-rule rejections that must NOT
 * dispatch any window input (US2): second operation before update, out-of-
 * bounds, click on a non-block cell, chord on a non-number cell. Plus the
 * infrastructure-failure throw on a FAILED dispatch (D-5).
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import { SaoleiMcp } from "./saolei-mcp";
import { createSaoleiTools } from "./saolei-tools";

import type { Part } from "../../../game_types/projects/game/Part";

const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

type MockBridge = OperationBridge & {
  dispatch: ReturnType<typeof vi.fn>;
};

function makeMockBridge(result: OperationResult = {
  status: STATUS_SUCCEEDED,
  message: "ok",
}): MockBridge {
  return {
    dispatch: vi.fn(async () => result),
  } as unknown as MockBridge;
}

type ContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

/** Parse the SaoleiToolResult JSON carried by the tool's first text block. */
function parseResult(value: unknown): {
  status: "ok" | "rejected";
  reason?: string;
  board?: { width: number; height: number; lifecycle: string };
} {
  const blocks = value as ContentBlock[];
  const first = blocks[0];
  if (first.type !== "text") {
    throw new Error(`expected first block to be text, got ${first.type}`);
  }
  return JSON.parse(first.text);
}

function blocks(value: unknown): ContentBlock[] {
  return value as ContentBlock[];
}

function tools(
  mcp: SaoleiMcp,
  bridge: MockBridge,
): ReturnType<typeof createSaoleiTools> {
  return createSaoleiTools(mcp, bridge);
}

function byName(
  list: ReturnType<typeof createSaoleiTools>,
  name: string,
) {
  const found = list.find((t) => t.name === name);
  if (!found) throw new Error(`tool ${name} not found`);
  return found;
}

describe("createSaoleiTools", () => {
  it("creates exactly five tools with the canonical names", () => {
    const mcp = new SaoleiMcp();
    const list = tools(mcp, makeMockBridge());
    expect(list.map((t) => t.name).sort()).toEqual([
      "saolei_click",
      "saolei_double_click",
      "saolei_flag",
      "saolei_init",
      "saolei_update",
    ]);
  });
});

describe("saolei_init", () => {
  let mcp: SaoleiMcp;
  let bridge: MockBridge;

  beforeEach(() => {
    mcp = new SaoleiMcp();
    bridge = makeMockBridge();
  });

  it("dispatches [KeyPart{F2}] and resets the board to ready (US1)", async () => {
    const init = byName(tools(mcp, bridge), "saolei_init");

    await init.invoke({ x: 9, y: 9 });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const block = bridge.dispatch.mock.calls[0][0] as Part[];
    expect(block).toHaveLength(1);
    expect(block[0].keyPress).toBeDefined();
    expect(block[0].keyPress!.key).toBe("KEY_ACTION_F2");
  });

  it("resets the board to x×y block cells in ready (FR-009)", async () => {
    const init = byName(tools(mcp, bridge), "saolei_init");
    const board = mcp.getBoard();

    await init.invoke({ x: 9, y: 9 });

    expect(board.width).toBe(9);
    expect(board.height).toBe(9);
    expect(board.lifecycle).toBe("ready");
    expect(board.cellAt(0, 0)).toBe("block");
  });

  it("returns ok with the board summary", async () => {
    const init = byName(tools(mcp, bridge), "saolei_init");

    const result = parseResult(await init.invoke({ x: 9, y: 9 }));

    expect(result.status).toBe("ok");
    expect(result.board).toEqual({ width: 9, height: 9, lifecycle: "ready" });
  });

  it("throws on infrastructure failure (FAILED dispatch, D-5)", async () => {
    bridge = makeMockBridge({ status: STATUS_FAILED, message: "no window bound" });
    const init = byName(tools(mcp, bridge), "saolei_init");

    await expect(init.invoke({ x: 9, y: 9 })).rejects.toThrow(
      /saolei dispatch failed/,
    );
    // board must NOT have been reset on a failed dispatch.
    expect(mcp.getBoard().lifecycle).toBe("uninitialized");
  });
});

describe("saolei_click (US1 + US2)", () => {
  let mcp: SaoleiMcp;
  let bridge: MockBridge;

  beforeEach(async () => {
    mcp = new SaoleiMcp();
    bridge = makeMockBridge();
    await byName(tools(mcp, bridge), "saolei_init").invoke({ x: 9, y: 9 });
    bridge.dispatch.mockClear();
  });

  it("dispatches a WINDOW_MESSAGE move+click PartBlock at the cell centre (US1)", async () => {
    const click = byName(tools(mcp, bridge), "saolei_click");

    await click.invoke({ x: 4, y: 4 });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const block = bridge.dispatch.mock.calls[0][0] as Part[];
    expect(block).toHaveLength(2);

    // Move part carries the cell-centre coordinate (cellCentre(4,4) = 168,344).
    const move = block[0].mouseMove!;
    expect(move).toBeDefined();
    expect(move.xPx).toBe(24 + 4 * 32 + 16);
    expect(move.yPx).toBe(200 + 4 * 32 + 16);
    expect(move.delivery).toBe("INPUT_DELIVERY_WINDOW_MESSAGE");

    // Click part is a LEFT_CLICK delivered by window message (no cursor move).
    const clickPart = block[1].mouseClick!;
    expect(clickPart.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
    expect(clickPart.delivery).toBe("INPUT_DELIVERY_WINDOW_MESSAGE");
  });

  it("transitions lifecycle ready → awaiting-update on a dispatched click (US2)", async () => {
    const click = byName(tools(mcp, bridge), "saolei_click");
    const board = mcp.getBoard();
    expect(board.lifecycle).toBe("ready");

    await click.invoke({ x: 0, y: 0 });

    expect(board.lifecycle).toBe("awaiting-update");
    // A second operation while awaiting-update is rejected — covered
    // separately by "rejects a second operation before saolei_update".
  });

  it("rejects a second operation before saolei_update with NO dispatch (FR-017)", async () => {
    const click = byName(tools(mcp, bridge), "saolei_click");
    await click.invoke({ x: 0, y: 0 }); // → awaiting-update
    bridge.dispatch.mockClear();

    const result = parseResult(await click.invoke({ x: 1, y: 1 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("awaiting-update");
    expect(bridge.dispatch).not.toHaveBeenCalled();
  });

  it("rejects an out-of-bounds coordinate with NO dispatch (FR-021)", async () => {
    const click = byName(tools(mcp, bridge), "saolei_click");

    const result = parseResult(await click.invoke({ x: 9, y: 0 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("out-of-bounds");
    expect(bridge.dispatch).not.toHaveBeenCalled();
  });

  it("rejects a click on a non-block cell with NO dispatch (FR-018)", async () => {
    const click = byName(tools(mcp, bridge), "saolei_click");
    mcp.getBoard().grid[0][0] = "one";

    const result = parseResult(await click.invoke({ x: 0, y: 0 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("cell-not-block");
    expect(bridge.dispatch).not.toHaveBeenCalled();
  });

  it("rejects an operation before saolei_init with NO dispatch (FR-016)", async () => {
    const fresh = new SaoleiMcp();
    const click = byName(tools(fresh, makeMockBridge()), "saolei_click");

    const result = parseResult(await click.invoke({ x: 0, y: 0 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("not-initialized");
  });

  it("appends a screenshot content block when the dispatch carries one", async () => {
    const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
    bridge = makeMockBridge({
      status: STATUS_SUCCEEDED,
      message: "ok",
      screenshot: { data: png, widthPx: 256, heightPx: 256 },
    });
    // re-init on the new bridge so the board is ready.
    mcp = new SaoleiMcp();
    await byName(tools(mcp, bridge), "saolei_init").invoke({ x: 9, y: 9 });
    const click = byName(tools(mcp, bridge), "saolei_click");

    const result = blocks(await click.invoke({ x: 0, y: 0 }));

    expect(result).toHaveLength(3);
    expect(result[1].type).toBe("image_url");
    if (result[1].type === "image_url") {
      expect(result[1].image_url.url).toBe(`data:image/png;base64,${png}`);
    }
  });
});

describe("saolei_flag", () => {
  let mcp: SaoleiMcp;
  let bridge: MockBridge;

  beforeEach(async () => {
    mcp = new SaoleiMcp();
    bridge = makeMockBridge();
    await byName(tools(mcp, bridge), "saolei_init").invoke({ x: 9, y: 9 });
    bridge.dispatch.mockClear();
  });

  it("dispatches a RIGHT_CLICK WINDOW_MESSAGE block", async () => {
    const flag = byName(tools(mcp, bridge), "saolei_flag");

    await flag.invoke({ x: 2, y: 3 });

    const block = bridge.dispatch.mock.calls[0][0] as Part[];
    expect(block[1].mouseClick!.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
    expect(block[1].mouseClick!.delivery).toBe("INPUT_DELIVERY_WINDOW_MESSAGE");
  });

  it("rejects flagging a revealed number (only block/flag are flag targets)", async () => {
    const flag = byName(tools(mcp, bridge), "saolei_flag");
    mcp.getBoard().grid[1][1] = "two";

    const result = parseResult(await flag.invoke({ x: 1, y: 1 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("cell-not-block-and-not-flag");
    expect(bridge.dispatch).not.toHaveBeenCalled();
  });
});

describe("saolei_double_click", () => {
  let mcp: SaoleiMcp;
  let bridge: MockBridge;

  beforeEach(async () => {
    mcp = new SaoleiMcp();
    bridge = makeMockBridge();
    await byName(tools(mcp, bridge), "saolei_init").invoke({ x: 9, y: 9 });
    bridge.dispatch.mockClear();
  });

  it("dispatches a LEFT_RIGHT_PRESS (chord) WINDOW_MESSAGE block", async () => {
    const dc = byName(tools(mcp, bridge), "saolei_double_click");
    // make (1,1) a number so the chord target is valid.
    mcp.getBoard().grid[1][1] = "three";

    await dc.invoke({ x: 1, y: 1 });

    const block = bridge.dispatch.mock.calls[0][0] as Part[];
    expect(block[1].mouseClick!.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
  });

  it("rejects a chord on a non-number cell with NO dispatch (FR-019)", async () => {
    const dc = byName(tools(mcp, bridge), "saolei_double_click");

    const result = parseResult(await dc.invoke({ x: 0, y: 0 }));

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("cell-not-number");
    expect(bridge.dispatch).not.toHaveBeenCalled();
  });
});

describe("saolei_update (US2 lifecycle)", () => {
  let mcp: SaoleiMcp;
  let bridge: MockBridge;

  beforeEach(async () => {
    mcp = new SaoleiMcp();
    bridge = makeMockBridge();
    await byName(tools(mcp, bridge), "saolei_init").invoke({ x: 9, y: 9 });
    const click = byName(tools(mcp, bridge), "saolei_click");
    await click.invoke({ x: 0, y: 0 }); // → awaiting-update
  });

  it("transitions awaiting-update → ready on a legal batch and never dispatches", async () => {
    const update = byName(tools(mcp, bridge), "saolei_update");
    bridge.dispatch.mockClear();

    const result = parseResult(
      await update.invoke({
        cells: [{ x: 0, y: 0, state: "zero" }],
      }),
    );

    expect(result.status).toBe("ok");
    expect(result.board?.lifecycle).toBe("ready");
    expect(bridge.dispatch).not.toHaveBeenCalled();
    expect(mcp.getBoard().cellAt(0, 0)).toBe("zero");
  });

  it("transitions to terminal when the batch reports a boom (FR-023)", async () => {
    const update = byName(tools(mcp, bridge), "saolei_update");

    const result = parseResult(
      await update.invoke({
        cells: [{ x: 0, y: 0, state: "boom" }],
      }),
    );

    expect(result.board?.lifecycle).toBe("terminal");
    expect(mcp.getBoard().cellAt(0, 0)).toBe("boom");
  });

  it("rejects an illegal batch atomically and leaves the board unchanged (SC-007)", async () => {
    const update = byName(tools(mcp, bridge), "saolei_update");
    mcp.getBoard().grid[5][5] = "one"; // terminal — illegal to change

    const result = parseResult(
      await update.invoke({
        cells: [
          { x: 1, y: 1, state: "two" }, // legal
          { x: 5, y: 5, state: "block" }, // illegal
        ],
      }),
    );

    expect(result.status).toBe("rejected");
    expect(result.reason).toContain("illegal-transition");
    // No state changed: (1,1) is still block, board still awaiting-update.
    expect(mcp.getBoard().cellAt(1, 1)).toBe("block");
    expect(result.board?.lifecycle).toBe("awaiting-update");
  });

  it("rejects update when not awaiting-update (FR-016/017 guard)", async () => {
    // Drain the awaiting-update state with a valid update first.
    await byName(tools(mcp, bridge), "saolei_update").invoke({
      cells: [{ x: 0, y: 0, state: "zero" }],
    });
    const update = byName(tools(mcp, bridge), "saolei_update");

    const result = parseResult(
      await update.invoke({
        cells: [{ x: 1, y: 1, state: "one" }],
      }),
    );

    expect(result.status).toBe("rejected");
    expect(result.reason).toBe("not-awaiting-update");
  });
});

describe("full turn: init → click → update (Phase 3 independent test)", () => {
  it("drives ready → awaiting-update → ready through the tools", async () => {
    const mcp = new SaoleiMcp();
    const bridge = makeMockBridge();
    const list = tools(mcp, bridge);
    const board = mcp.getBoard();

    await byName(list, "saolei_init").invoke({ x: 9, y: 9 });
    expect(board.lifecycle).toBe("ready");

    await byName(list, "saolei_click").invoke({ x: 4, y: 4 });
    expect(board.lifecycle).toBe("awaiting-update");

    const r = parseResult(
      await byName(list, "saolei_update").invoke({
        cells: [{ x: 4, y: 4, state: "one" }],
      }),
    );
    expect(r.status).toBe("ok");
    expect(board.lifecycle).toBe("ready");
    expect(board.cellAt(4, 4)).toBe("one");
  });
});
