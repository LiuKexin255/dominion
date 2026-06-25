/**
 * mouse-tool.test.ts — Tests for the mouse LangChain tool.
 *
 * Covers the core scenarios:
 *   1. invoke → bridge.dispatch called with correct frame (action, x_px, y_px).
 *   2. tool returns a content-block array: single text block when no
 *      screenshot, [text, image_url, text] when a screenshot is present.
 *   3. annotation text matches the llm.ts:226 pixel-dimension template exactly.
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

/**
 * Expected content-block shape returned by the tool. The mouse tool's return
 * is consumed by LangChain `_formatToolOutput`, which passes any array whose
 * elements carry a `type` discriminator through verbatim as
 * `ToolMessage.content`. The cast below mirrors that shape for assertions.
 */
type ContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

function asBlocks(value: unknown): ContentBlock[] {
  return value as ContentBlock[];
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

  it("returns a single text content block when no screenshot is present", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
    });

    const mouseTool = createMouseTool(bridge);
    const result = asBlocks(
      await mouseTool.invoke({
        x_px: 10,
        y_px: 20,
        action: "LEFT_CLICK",
      }),
    );

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ type: "text", text: "click registered" });
  });

  it("returns a single text block carrying the failure message on dispatch failure", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_FAILED,
      message: "operation timed out",
    });

    const mouseTool = createMouseTool(bridge);
    const result = asBlocks(
      await mouseTool.invoke({
        x_px: 1,
        y_px: 2,
        action: "RIGHT_CLICK",
      }),
    );

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      type: "text",
      text: "operation timed out",
    });
  });

  it("returns [text, image_url, text] with annotation when screenshot is present", async () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
      screenshot: { data: pngBase64, widthPx: 1920, heightPx: 1080 },
    });

    const mouseTool = createMouseTool(bridge);
    const result = asBlocks(
      await mouseTool.invoke({
        x_px: 5,
        y_px: 6,
        action: "LEFT_CLICK",
      }),
    );

    expect(result).toHaveLength(3);
    // Block 0 — status text.
    expect(result[0]).toEqual({ type: "text", text: "click registered" });
    // Block 1 — image_url with data URL prefix.
    expect(result[1]).toEqual({
      type: "image_url",
      image_url: { url: `data:image/png;base64,${pngBase64}` },
    });
    // Block 2 — annotation matching the llm.ts:226 template exactly.
    expect(result[2]).toEqual({
      type: "text",
      text: "[图片像素尺寸：1920×1080（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
    });
  });

  it("does not emit the annotation without a screenshot", async () => {
    // Screenshot-absent path must produce exactly one text block; the
    // pixel-dimension annotation is paired with the image, never standalone.
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "ok",
    });

    const mouseTool = createMouseTool(bridge);
    const result = asBlocks(
      await mouseTool.invoke({
        x_px: 0,
        y_px: 0,
        action: "LEFT_CLICK",
      }),
    );

    expect(result).toHaveLength(1);
    expect(result[0].type).toBe("text");
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
    ["MOVE", "AGENT_MOUSE_ACTION_MOVE"],
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
