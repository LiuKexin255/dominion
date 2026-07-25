/**
 * mouse-move.test.ts — Tests for the `mouse_move` LangChain tool.
 *
 * Coverage:
 *   - mouse_move invokes bridge.dispatch with the correct Part + coords.
 *   - Result blocks: single text block when no screenshot, [text, image_url,
 *     text] when a screenshot is present, single text block on dispatch
 *     failure.
 *   - Schema exposes only x_px and y_px; name is 'mouse_move' for profile
 *     tool_names matching; description is non-empty.
 *   - Signal wiring: signal from tool context is forwarded; undefined when no
 *     config is provided.
 *
 * The `mouse tool abort signal` describe block uses a real OperationBridge so
 * the abort path runs through the actual dispatch logic.
 *
 * Relocated verbatim from `src/mouse-tool.test.ts` (Feature 020 — per-tool-name
 * directory layout, see
 * `specs/020-agent-resources-layout/contracts/directory-layout.md`).
 * Assertions, `it(...)` titles, mock setup, and the skipped test's comment are
 * preserved verbatim (spec Assumption / SC-002). Only the imports at the top
 * of the file are adjusted to the new locations.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { createMouseMoveTool } from "./mouse-move";
import { OperationBridge } from "../../operation-bridge";

import type { FlowPart } from "../../../game_types/projects/game/FlowPart";
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
 * The tool now returns a ToolMessage (T019 — buildToolResultMessage); tests
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

// ─── mouse_move ────────────────────────────────────────────────────────────

describe("createMouseMoveTool", () => {
  let bridge: ReturnType<typeof makeMockBridge>;

  beforeEach(() => {
    bridge = makeMockBridge();
  });

  it("invokes bridge.dispatch with a MouseMovePart at the given coordinates", async () => {
    const mouseTool = createMouseMoveTool(bridge);

    await mouseTool.invoke({ x_px: 100, y_px: 200 });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    const part = bridge.dispatch.mock.calls[0][0] as FlowPart;
    expect(part.mouseMove).toBeDefined();
    expect(part.mouseMove!.xPx).toBe(100);
    expect(part.mouseMove!.yPx).toBe(200);
    // A click part is never emitted by the move tool.
    expect(part.mouseClick).toBeUndefined();
  });

  it("returns a ToolMessage whose content is a single text block when no screenshot is present", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "moved",
    });

    const mouseTool = createMouseMoveTool(bridge);
    const msg = await mouseTool.invoke({ x_px: 10, y_px: 20 });

    expect(readContent(msg)).toHaveLength(1);
    expect(readContent(msg)[0]).toEqual({ type: "text", text: "moved" });
  });

  it("returns a ToolMessage carrying FAILED status in additional_kwargs on dispatch failure", async () => {
    // spec US2 acceptance 2 / FR-013: a genuine failure still reads FAILED
    // (the fix does not mask real failures).
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_FAILED,
      message: "operation timed out",
    });

    const mouseTool = createMouseMoveTool(bridge);
    const msg = await mouseTool.invoke({ x_px: 1, y_px: 2 });

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
      message: "moved",
      screenshot: { data: pngBase64, widthPx: 1920, heightPx: 1080 },
    });

    const mouseTool = createMouseMoveTool(bridge);
    const msg = await mouseTool.invoke({ x_px: 5, y_px: 6 });

    const blocks = readContent(msg);
    expect(blocks).toHaveLength(3);
    expect(blocks[0]).toEqual({ type: "text", text: "moved" });
    expect(blocks[1]).toEqual({
      type: "image_url",
      image_url: { url: `data:image/png;base64,${pngBase64}` },
    });
    // Annotation matches the llm.ts:226 template exactly.
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

    const mouseTool = createMouseMoveTool(bridge);
    const msg = await mouseTool.invoke({ x_px: 0, y_px: 0 });

    expect(readContent(msg)).toHaveLength(1);
    expect(readContent(msg)[0].type).toBe("text");
  });

  it("tool schema exposes only x_px and y_px", () => {
    const mouseTool = createMouseMoveTool(bridge);
    const schemaKeys = Object.keys(readSchemaShape(mouseTool) ?? {});
    expect(schemaKeys.sort()).toEqual(["x_px", "y_px"]);
  });

  it("has name 'mouse_move' for profile.tool_names matching", () => {
    const mouseTool = createMouseMoveTool(bridge);
    expect(mouseTool.name).toBe("mouse_move");
  });

  it("has a non-empty description", () => {
    const mouseTool = createMouseMoveTool(bridge);
    expect(mouseTool.description).toBeTruthy();
    expect(mouseTool.description.length).toBeGreaterThan(10);
  });

  it("passes signal from tool context to dispatch", async () => {
    const mouseTool = createMouseMoveTool(bridge);
    const controller = new AbortController();

    await mouseTool.invoke(
      { x_px: 100, y_px: 200 },
      { signal: controller.signal },
    );

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    // dispatch signature is (part, toolId, signal); signal is the 3rd arg.
    expect(bridge.dispatch.mock.calls[0][2]).toBe(controller.signal);
  });

  it("forwards undefined signal when no config is provided", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "ok",
    });
    const mouseTool = createMouseMoveTool(bridge);

    await mouseTool.invoke({ x_px: 1, y_px: 2 });

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    expect(bridge.dispatch.mock.calls[0][2]).toBeUndefined();
  });

  // T009 (contracts/tool-dispatch-contract.md §2): the tool reads
  // config.toolCall.id and passes it to dispatch so the FlowPart operation
  // shares the LangChain tool_call.id (spec 023 FR-008).
  it("passes config.toolCall.id as the dispatch toolId", async () => {
    const mouseTool = createMouseMoveTool(bridge);

    await mouseTool.invoke(
      { x_px: 7, y_px: 8 },
      { toolCall: { id: "call_xyz" } } as unknown as Record<string, unknown>,
    );

    expect(bridge.dispatch).toHaveBeenCalledTimes(1);
    expect(bridge.dispatch.mock.calls[0][1]).toBe("call_xyz");
  });

  // T019 (contracts/tool-dispatch-contract.md §3): the returned ToolMessage
  // carries the LangChain tool_call.id as tool_call_id and the real status in
  // additional_kwargs, so ListMessages reconstructs the actual outcome
  // (FR-012/FR-013) and the live path reads the same value (FR-009).
  it("returns a ToolMessage whose tool_call_id + name + status come from config.toolCall.id / tool name / dispatch outcome", async () => {
    bridge.dispatch.mockResolvedValueOnce({
      status: STATUS_SUCCEEDED,
      message: "moved",
    });
    const mouseTool = createMouseMoveTool(bridge);

    const msg = await mouseTool.invoke(
      { x_px: 7, y_px: 8 },
      { toolCall: { id: "call_xyz" } } as unknown as Record<string, unknown>,
    );

    expect(msg.tool_call_id).toBe("call_xyz");
    expect(msg.name).toBe("mouse_move");
    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_SUCCEEDED);
  });
});

// ─── signal abort wiring (real OperationBridge) ────────────────────────────

describe("mouse tool abort signal", () => {
  // SKIPPED (FR-014 / SC-004): langchain's DynamicStructuredTool.invoke(input,
  // { signal }) hangs — neither resolves nor rejects — when the signal is
  // already aborted before invoke (Runnable._callWithConfig's abort listener
  // fires but leaves the wrapping promise pending; reproduced & diagnosed in
  // Phase 5 triage against @langchain/core 1.2.0). The production dispatch
  // abort short-circuit (operation-bridge.ts:151) is correct and is covered
  // DIRECTLY — without the langchain tool.invoke layer — by operation-bridge
  // .test.ts ("signal already aborted → dispatch resolves FAILED 'aborted'").
  // This integration test cannot exercise the path until langchain's
  // pre-aborted-signal handling is addressed; tracked as an out-of-scope
  // dependency (langchain framework), not a defect in our dispatch code.
  it.skip("tool result is FAILED when signal is already aborted", async () => {
    // Use a real OperationBridge so the abort path runs through the actual
    // dispatch logic — sink registered so the no-sink check is bypassed and
    // the signal-abort short-circuit at operation-bridge.ts:151 is reached.
    //
    // NOTE: langchain's DynamicStructuredTool.invoke(input, { signal }) hangs
    // (neither resolves nor rejects) when the signal is already aborted before
    // invoke — the abort listener inside Runnable._callWithConfig fires but
    // leaves the wrapping promise pending. The production dispatch abort
    // short-circuit itself is correct and is covered directly by
    // operation-bridge.test.ts ("signal already aborted → dispatch resolves
    // FAILED 'aborted'"). This integration test is skipped pending a langchain
    // workaround; see specs/019-js-test-reliability (Phase 5 triage).
    const bridge = new OperationBridge();
    bridge.registerSink(() => {
      throw new Error("sink must not be called when signal is already aborted");
    });
    const controller = new AbortController();
    controller.abort();

    const mouseTool = createMouseMoveTool(bridge);
    const msg = await mouseTool.invoke(
      { x_px: 1, y_px: 2 },
      { signal: controller.signal },
    );
    const result = readContent(msg);

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ type: "text", text: "aborted" });
  });
});
