/**
 * mouse-tool.test.ts — Tests for the mouse LangChain tool.
 *
 * Covers the required scenarios:
 *   1. invoke → bridge.dispatch called with correct frame (action, x_px,
 *      y_px, screenshot_id from bridge).
 *   2. tool returns the result message string.
 *   3. bridge.getCurrentScreenshotId() is called at dispatch time, NOT
 *      passed as a tool parameter (screenshot_id is absent from the schema).
 *
 * Plus coverage for each of the 5 action enum values and per-turn screenshot
 * dynamism (two invocations with different screenshot_ids).
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { createMouseTool } from "./mouse-tool";

import type { OperationBridge } from "./operation-bridge";
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";

const STATUS_SUCCEEDED = "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "AGENT_OPERATION_RESULT_STATUS_FAILED";

/**
 * Build a mock OperationBridge with vitest spied methods.  Only the two
 * methods used by the mouse tool are stubbed: getCurrentScreenshotId and
 * dispatch.  The partial object is double-cast to OperationBridge so it can
 * be passed to createMouseTool while still exposing the mock interface.
 */
type MockBridge = OperationBridge & {
  dispatch: ReturnType<typeof vi.fn>;
  getCurrentScreenshotId: ReturnType<typeof vi.fn>;
};

function makeMockBridge(screenshotId = "shot-default"): MockBridge {
  return {
    getCurrentScreenshotId: vi.fn(() => screenshotId),
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

  // ------------------------------------------------------------------
  // Required scenario 1: dispatch called with correct frame
  // ------------------------------------------------------------------
  it("invokes bridge.dispatch with the correct AgentOperationFrame", async () => {
    const mouseTool = createMouseTool(bridge);

    await mouseTool.invoke({
      x_px: 100,
      y_px: 200,
      action: "LEFT_CLICK",
    });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const frame = bridge.dispatch.mock.calls[0][0] as AgentOperationFrame;
    expect(frame.screenshotId).toBe("shot-default");
    expect(frame.mouse).toBeDefined();
    expect(frame.mouse!.action).toBe("AGENT_MOUSE_ACTION_LEFT_CLICK");
    expect(frame.mouse!.xPx).toBe(100);
    expect(frame.mouse!.yPx).toBe(200);
  });

  // ------------------------------------------------------------------
  // Required scenario 2: tool returns the result message
  // ------------------------------------------------------------------
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

  // ------------------------------------------------------------------
  // Required scenario 3: screenshot_id from bridge, not parameter
  // ------------------------------------------------------------------
  it("reads screenshot_id from bridge.getCurrentScreenshotId at dispatch time", async () => {
    const mouseTool = createMouseTool(bridge);

    await mouseTool.invoke({
      x_px: 50,
      y_px: 60,
      action: "LEFT_CLICK",
    });

    expect(bridge.getCurrentScreenshotId).toHaveBeenCalledTimes(1);
  });

  it("screenshot_id is NOT a parameter in the tool schema", () => {
    const mouseTool = createMouseTool(bridge);

    // The LangChain tool exposes its schema; screenshot_id must not appear.
    const schemaKeys = Object.keys(
      (mouseTool as unknown as { schema?: { shape?: Record<string, unknown> } })
        .schema?.shape ?? {},
    );
    expect(schemaKeys).toEqual(
      expect.arrayContaining(["x_px", "y_px", "action"]),
    );
    expect(schemaKeys).not.toContain("screenshot_id");
    expect(schemaKeys).not.toContain("screenshotId");
  });

  // ------------------------------------------------------------------
  // Per-turn screenshot dynamism
  // ------------------------------------------------------------------
  it("uses a different screenshot_id per invocation (dynamic, per-turn)", async () => {
    const mouseTool = createMouseTool(bridge);

    bridge.getCurrentScreenshotId.mockReturnValueOnce("shot-turn-1");
    await mouseTool.invoke({ x_px: 1, y_px: 1, action: "LEFT_CLICK" });

    bridge.getCurrentScreenshotId.mockReturnValueOnce("shot-turn-2");
    await mouseTool.invoke({ x_px: 2, y_px: 2, action: "LEFT_CLICK" });

    expect(bridge.dispatch).toHaveBeenCalledTimes(2);
    expect(
      (bridge.dispatch.mock.calls[0][0] as AgentOperationFrame).screenshotId,
    ).toBe("shot-turn-1");
    expect(
      (bridge.dispatch.mock.calls[1][0] as AgentOperationFrame).screenshotId,
    ).toBe("shot-turn-2");
  });

  // ------------------------------------------------------------------
  // All 5 action enum values map to the correct proto string
  // ------------------------------------------------------------------
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

  // ------------------------------------------------------------------
  // Tool metadata
  // ------------------------------------------------------------------
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
