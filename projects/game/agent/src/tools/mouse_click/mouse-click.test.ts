/**
 * mouse-click.test.ts — Tests for the `mouse_click` LangChain tool.
 *
 * Coverage:
 *   - mouse_click invokes bridge.dispatch with each mapped click proto value.
 *   - Result blocks: single text block when no screenshot, [text, image_url,
 *     text] when a screenshot is present, single text block on dispatch
 *     failure.
 *   - Schema exposes only click_type; name is 'mouse_click' for profile
 *     tool_names matching; description is non-empty.
 *   - Signal wiring: signal from tool context is forwarded.
 *
 * Relocated verbatim from `src/mouse-tool.test.ts` (Feature 020 — per-tool-name
 * directory layout, see
 * `specs/020-agent-resources-layout/contracts/directory-layout.md`).
 * Assertions, `it(...)` titles, mock setup, and the `it.each(...)` click-type
 * → proto parameterization are preserved verbatim (spec Assumption / SC-002).
 * Only the imports at the top of the file are adjusted to the new locations.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { createMouseClickTool } from "./mouse-click.js";
import { OperationBridge } from "../../operation-bridge.js";

import type { FlowPart } from "../../../game_types/projects/game/FlowPart.js";
import type { ToolMessage } from "@langchain/core/messages";

const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

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
 * Content-block shape carried inside the returned ToolMessage's `.content`.
 * The tool now returns a ToolMessage (T020 — buildToolResultMessage); tests
 * read `.content` / `.additional_kwargs.toolResultStatus` / `.tool_call_id` /
 * `.name` directly off the message.
 */
type ContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

function readContent(msg: ToolMessage): ContentBlock[] {
  return msg.content as ContentBlock[];
}

function readSchemaShape(
  t: unknown,
): Record<string, unknown> | undefined {
  return (t as { schema?: { shape?: Record<string, unknown> } }).schema?.shape;
}

// ─── mouse_click ───────────────────────────────────────────────────────────

describe("createMouseClickTool", () => {
  let bridge: ReturnType<typeof makeMockBridge>;

  beforeEach(() => {
    bridge = makeMockBridge();
  });

  it("invokes bridge.dispatch with a MouseClickPart for the mapped click action", async () => {
    const mouseTool = createMouseClickTool(bridge);

    await mouseTool.invoke({ click_type: "LEFT_CLICK" });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const part = bridge.dispatch.mock.calls[0][0] as FlowPart;
    expect(part.mouseClick).toBeDefined();
    expect(part.mouseClick!.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
    // A click part carries no coordinates — desktop clicks at the current
    // cursor position, and MOVE is structurally absent from MouseClickPart.
    expect((part.mouseClick as unknown as { xPx?: number }).xPx).toBeUndefined();
  });

  it("returns a ToolMessage whose content is a single text block when no screenshot is present", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
    });

    const mouseTool = createMouseClickTool(bridge);
    const msg = await mouseTool.invoke({ click_type: "LEFT_CLICK" });

    expect(readContent(msg)).toHaveLength(1);
    expect(readContent(msg)[0]).toEqual({ type: "text", text: "click registered" });
  });

  it("returns a ToolMessage carrying FAILED status in additional_kwargs on dispatch failure", async () => {
    // spec US2 acceptance 2 / FR-013: a genuine failure still reads FAILED
    // (the fix does not mask real failures).
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_FAILED,
      message: "operation timed out",
    });

    const mouseTool = createMouseClickTool(bridge);
    const msg = await mouseTool.invoke({ click_type: "RIGHT_CLICK" });

    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_FAILED);
    expect(readContent(msg)).toHaveLength(1);
    expect(readContent(msg)[0]).toEqual({
      type: "text",
      text: "operation timed out",
    });
  });

  it("returns a ToolMessage whose content is [text, image_url, text] with annotation when screenshot is present", async () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
      screenshot: { data: pngBase64, widthPx: 1920, heightPx: 1080 },
    });

    const mouseTool = createMouseClickTool(bridge);
    const msg = await mouseTool.invoke({ click_type: "LEFT_CLICK" });

    const blocks = readContent(msg);
    expect(blocks).toHaveLength(3);
    expect(blocks[0]).toEqual({ type: "text", text: "click registered" });
    expect(blocks[1]).toEqual({
      type: "image_url",
      image_url: { url: `data:image/png;base64,${pngBase64}` },
    });
    expect(blocks[2]).toEqual({
      type: "text",
      text: "[图片像素尺寸：1920×1080（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
    });
  });

  it("does not emit the annotation without a screenshot", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "ok",
    });

    const mouseTool = createMouseClickTool(bridge);
    const msg = await mouseTool.invoke({ click_type: "LEFT_CLICK" });

    expect(readContent(msg)).toHaveLength(1);
    expect(readContent(msg)[0].type).toBe("text");
  });

  it("tool schema exposes only click_type", () => {
    const mouseTool = createMouseClickTool(bridge);
    const schemaKeys = Object.keys(readSchemaShape(mouseTool) ?? {});
    expect(schemaKeys).toEqual(["click_type"]);
  });

  it.each([
    ["LEFT_CLICK", "MOUSE_CLICK_ACTION_LEFT_CLICK"],
    ["LEFT_DOUBLE_CLICK", "MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK"],
    ["RIGHT_CLICK", "MOUSE_CLICK_ACTION_RIGHT_CLICK"],
    ["RIGHT_DOUBLE_CLICK", "MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK"],
    ["LEFT_RIGHT_PRESS", "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS"],
  ] as const)("maps click_type %s to proto value %s", async (clickType, protoValue) => {
    const mouseTool = createMouseClickTool(bridge);

    await mouseTool.invoke({ click_type: clickType });

    const part = bridge.dispatch.mock.calls[0][0] as FlowPart;
    expect(part.mouseClick!.click).toBe(protoValue);
  });

  it("has name 'mouse_click' for profile.tool_names matching", () => {
    const mouseTool = createMouseClickTool(bridge);
    expect(mouseTool.name).toBe("mouse_click");
  });

  it("has a non-empty description", () => {
    const mouseTool = createMouseClickTool(bridge);
    expect(mouseTool.description).toBeTruthy();
    expect(mouseTool.description.length).toBeGreaterThan(10);
  });

  it("passes signal from tool context to dispatch", async () => {
    const mouseTool = createMouseClickTool(bridge);
    const controller = new AbortController();

    await mouseTool.invoke(
      { click_type: "LEFT_CLICK" },
      { signal: controller.signal },
    );

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    // dispatch signature is (part, signal); signal is the 2nd arg.
    expect(bridge.dispatch.mock.calls[0][1]).toBe(controller.signal);
  });

  // T027 (contracts/tool-dispatch-contract.md §1..§2 / research.md D10): the
  // tool does NOT pass config.toolCall.id to dispatch — the operation channel
  // uses a bridge-minted id (decoupled from the conversation tool_call.id).
  it("does not pass any toolId to dispatch (operation channel is decoupled)", async () => {
    const mouseTool = createMouseClickTool(bridge);

    await mouseTool.invoke(
      { click_type: "LEFT_CLICK" },
      { toolCall: { id: "call_abc" } } as unknown as Record<string, unknown>,
    );

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    // dispatch is (part, signal) — the second positional arg is the signal
    // (undefined here), NOT a toolId. The conversation tool_call.id never
    // reaches dispatch.
    expect(bridge.dispatch.mock.calls[0][1]).toBeUndefined();
  });

  // T020 (contracts/tool-dispatch-contract.md §3): the returned ToolMessage
  // carries the LangChain tool_call.id as tool_call_id and the real status in
  // additional_kwargs, so ListMessages reconstructs the actual outcome
  // (FR-012/FR-013) and the live path reads the same value (FR-009).
  it("returns a ToolMessage whose tool_call_id + name + status come from config.toolCall.id / tool name / dispatch outcome", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "click registered",
    });
    const mouseTool = createMouseClickTool(bridge);

    const msg = await mouseTool.invoke(
      { click_type: "LEFT_CLICK" },
      { toolCall: { id: "call_abc" } } as unknown as Record<string, unknown>,
    );

    // tool_call_id still mirrors config.toolCall.id (conversation grouping),
    // even though dispatch no longer takes the id.
    expect(msg.tool_call_id).toBe("call_abc");
    expect(msg.name).toBe("mouse_click");
    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_SUCCEEDED);
  });
});
