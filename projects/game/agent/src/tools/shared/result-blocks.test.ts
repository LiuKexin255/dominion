/**
 * result-blocks.test.ts — Unit tests for the now-exported `buildResultBlocks`
 * helper.
 *
 * Previously this helper was file-private inside `src/mouse-tool.ts` and only
 * exercised indirectly through the mouse tools. After Feature 020 split it
 * lives at `src/tools/shared/result-blocks.ts` and is testable in isolation.
 *
 * Covers the three contract behaviors previously asserted through the tools at
 * `src/mouse-tool.test.ts:85-142`:
 *   1. `OperationResult` with no `screenshot` → single text block.
 *   2. `OperationResult` with `screenshot` → [text, image_url, text] with the
 *      pixel-dimension annotation template.
 *   3. `OperationResult` with `status: TOOL_RESULT_STATUS_FAILED` → still a
 *      single text block carrying the failure message (buildResultBlocks does
 *      not branch on status).
 *
 * Per `style/javascript.md` §Mock 约定 "Reliable pattern": no `vi.mock`, no
 * tool invocation — pure function tests with plain object-literal inputs.
 */

import { describe, expect, it } from "vitest";

import type { OperationResult } from "../../operation-bridge.js";
import { buildResultBlocks, buildToolResultMessage } from "./result-blocks.js";

const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

describe("buildResultBlocks", () => {
  it("returns a single text content block when no screenshot is present", () => {
    const result: OperationResult = {
      status: STATUS_SUCCEEDED,
      message: "moved",
    };

    const blocks = buildResultBlocks(result);

    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toEqual({ type: "text", text: "moved" });
  });

  it("returns [text, image_url, text] with the pixel-dimension annotation when a screenshot is present", () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
    const result: OperationResult = {
      status: STATUS_SUCCEEDED,
      message: "moved",
      screenshot: { data: pngBase64, widthPx: 1920, heightPx: 1080 },
    };

    const blocks = buildResultBlocks(result);

    expect(blocks).toHaveLength(3);
    expect(blocks[0]).toEqual({ type: "text", text: "moved" });
    expect(blocks[1]).toEqual({
      type: "image_url",
      image_url: { url: `data:image/png;base64,${pngBase64}` },
    });
    expect(blocks[2]).toEqual({
      type: "text",
      text: "[图片像素尺寸：1920×1080（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
    });
  });

  it("returns a single text block carrying the failure message on dispatch failure", () => {
    const result: OperationResult = {
      status: STATUS_FAILED,
      message: "operation timed out",
    };

    const blocks = buildResultBlocks(result);

    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toEqual({
      type: "text",
      text: "operation timed out",
    });
  });
});

// T018 (contracts/tool-dispatch-contract.md §3 / data-model.md §6): the real
// ToolResultStatus rides in additional_kwargs.toolResultStatus so it survives
// MemorySaver and ListMessages reads the actual outcome (FR-012..FR-015).
describe("buildToolResultMessage", () => {
  it("carries SUCCEEDED status in additional_kwargs and propagates tool_call_id + name", () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC";
    const result: OperationResult = {
      status: STATUS_SUCCEEDED,
      message: "moved",
      screenshot: { data: pngBase64, widthPx: 1920, heightPx: 1080 },
    };

    const msg = buildToolResultMessage(result, "call_abc", "mouse_move");

    // The status text + screenshot blocks are reused verbatim (contract §8).
    expect(msg.content).toEqual(buildResultBlocks(result));
    // tool_call_id is propagated so LangChain links result ↔ AIMessage.tool_calls[i].
    expect(msg.tool_call_id).toBe("call_abc");
    expect(msg.name).toBe("mouse_move");
    // The REAL status rides in additional_kwargs — the history fix (FR-012).
    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_SUCCEEDED);
  });

  it("carries FAILED status in additional_kwargs (the fix does not mask real failures)", () => {
    // spec US2 acceptance 2 / FR-013: a genuine failure still reads FAILED.
    const result: OperationResult = {
      status: STATUS_FAILED,
      message: "operation timed out",
    };

    const msg = buildToolResultMessage(result, "call_fail", "mouse_click");

    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_FAILED);
    expect(msg.tool_call_id).toBe("call_fail");
    expect(msg.name).toBe("mouse_click");
    expect(msg.content).toEqual([{ type: "text", text: "operation timed out" }]);
  });

  it("defaults tool_call_id to empty string when toolCallId is undefined (non-agent path)", () => {
    // contract §3: empty string when the id is unknown (e.g. a direct-invoke
    // test path with no config.toolCall.id).
    const result: OperationResult = {
      status: STATUS_SUCCEEDED,
      message: "ok",
    };

    const msg = buildToolResultMessage(result, undefined, "mouse_move");

    expect(msg.tool_call_id).toBe("");
    expect(msg.additional_kwargs?.toolResultStatus).toBe(STATUS_SUCCEEDED);
  });
});
