/**
 * mouse-tool.test.ts — Tests for the mouse LangChain tool.
 *
 * Covers the core scenarios:
 *   1. invoke → bridge.dispatch called with correct frame (action, x_px, y_px).
 *   2. tool returns the result message string.
 *
 * Plus coverage for each of the 5 action enum values.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { createMouseTool } from "./mouse-tool";

import type { OperationBridge } from "./operation-bridge";
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";

const STATUS_SUCCEEDED = "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "AGENT_OPERATION_RESULT_STATUS_FAILED";

type MockBridge = OperationBridge & {
  dispatch: ReturnType<typeof vi.fn>;
};

function makeMockBridge(): MockBridge {
  return {
    dispatch: vi.fn(async () => ({
      status: STATUS_SUCCEEDED,
      message: "ok",
    })),
  } as unknown as MockBridge;
}

describe("createMouseTool", () => {
  let bridge: ReturnType<typeof makeMockBridge>;

  beforeEach(() => {
    bridge = makeMockBridge();
  });

  it("invokes bridge.dispatch with the correct AgentOperationFrame", async () => {
    const mouseTool = createMouseTool(bridge);

    await mouseTool.invoke({
      x_px: 100,
      y_px: 200,
      action: "LEFT_CLICK",
    });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const frame = bridge.dispatch.mock.calls[0][0] as AgentOperationFrame;
    expect(frame.mouse).toBeDefined();
    expect(frame.mouse!.action).toBe("AGENT_MOUSE_ACTION_LEFT_CLICK");
    expect(frame.mouse!.xPx).toBe(100);
    expect(frame.mouse!.yPx).toBe(200);
  });

  it("returns the dispatch result message to LangChain", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
    });

    const mouseTool = createMouseTool(bridge);
    const result = await mouseTool.invoke({
      x_px: 10,
      y_px: 20,
      action: "LEFT_CLICK",
    });

    expect(result).toBe("click registered");
  });

  it("returns failure message when dispatch fails", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_FAILED,
      message: "operation timed out",
    });

    const mouseTool = createMouseTool(bridge);
    const result = await mouseTool.invoke({
      x_px: 1,
      y_px: 2,
      action: "RIGHT_CLICK",
    });

    expect(result).toBe("operation timed out");
  });

  it("tool schema exposes only x_px, y_px, action", () => {
    const mouseTool = createMouseTool(bridge);

    const schemaKeys = Object.keys(
      (mouseTool as unknown as { schema?: { shape?: Record<string, unknown> } })
        .schema?.shape ?? {},
    );
    expect(schemaKeys.sort()).toEqual(["action", "x_px", "y_px"]);
  });

  it.each([
    ["LEFT_CLICK", "AGENT_MOUSE_ACTION_LEFT_CLICK"],
    ["LEFT_DOUBLE_CLICK", "AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK"],
    ["RIGHT_CLICK", "AGENT_MOUSE_ACTION_RIGHT_CLICK"],
    ["RIGHT_DOUBLE_CLICK", "AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK"],
    ["LEFT_RIGHT_PRESS", "AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS"],
  ] as const)(
    "maps action %s to proto value %s",
    async (action, protoValue) => {
      const mouseTool = createMouseTool(bridge);

      await mouseTool.invoke({ x_px: 0, y_px: 0, action });

      const frame = bridge.dispatch.mock.calls[0][0] as AgentOperationFrame;
      expect(frame.mouse!.action).toBe(protoValue);
    },
  );

  it("has name 'mouse' for profile.tool_names matching", () => {
    const mouseTool = createMouseTool(bridge);
    expect(mouseTool.name).toBe("mouse");
  });

  it("has a non-empty description", () => {
    const mouseTool = createMouseTool(bridge);
    expect(mouseTool.description).toBeTruthy();
    expect(mouseTool.description.length).toBeGreaterThan(10);
  });
});
