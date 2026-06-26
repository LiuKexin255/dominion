/**
 * mouse-tool.ts — LangChain tools that dispatch mouse operations through the
 * session-scoped OperationBridge.
 *
 * Feature 015 splits the single mouse tool into two purpose-specific tools:
 *   - mouse_move: reposition the cursor at screenshot-relative coordinates.
 *   - mouse_click: perform a button action at the current cursor position.
 *
 * Each tool is created per session by SessionAgent via createMouseMoveTool /
 * createMouseClickTool, binding the bridge in a closure.
 */

import type { StructuredToolInterface } from "@langchain/core/tools";
import { tool } from "langchain";
import { z } from "zod";

import type { OperationBridge, OperationResult } from "./operation-bridge";
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";

// ─── mouse_move tool ───────────────────────────────────────────────────────

const mouseMoveSchema = z.object({
  x_px: z.number().describe("X coordinate in pixels, image-relative"),
  y_px: z.number().describe("Y coordinate in pixels, image-relative"),
});

// ─── mouse_click tool ──────────────────────────────────────────────────────

const CLICK_TYPES = [
  "LEFT_CLICK",
  "LEFT_DOUBLE_CLICK",
  "RIGHT_CLICK",
  "RIGHT_DOUBLE_CLICK",
  "LEFT_RIGHT_PRESS",
] as const;

const CLICK_TYPE_TO_PROTO = {
  LEFT_CLICK: "AGENT_MOUSE_ACTION_LEFT_CLICK",
  LEFT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK",
  RIGHT_CLICK: "AGENT_MOUSE_ACTION_RIGHT_CLICK",
  RIGHT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK",
  LEFT_RIGHT_PRESS: "AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS",
} as const;

const mouseClickSchema = z.object({
  click_type: z
    .enum(CLICK_TYPES)
    .describe("Click type to perform at the current cursor position"),
});

/**
 * A content block returned to LangChain. Mirrors the subset of the LangChain
 * multimodal content shape consumed by `_formatToolOutput`: an array whose
 * elements each carry a `type` discriminator is passed through verbatim as the
 * `ToolMessage.content`, so an `image_url` block reaches the model as a real
 * image rather than a stringified blob.
 */
type MouseContentBlock =
  | { type: "text"; text: string }
  | { type: "image_url"; image_url: { url: string } };

/**
 * Create the "mouse_move" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds an AgentOperationFrame carrying a MOVE operation at the given
 *      screenshot-relative pixel coordinates.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseMoveTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px }): Promise<MouseContentBlock[]> => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: "AGENT_MOUSE_ACTION_MOVE",
          xPx: x_px,
          yPx: y_px,
        },
      };
      const result = await bridge.dispatch(frame);
      return buildResultBlocks(result);
    },
    {
      name: "mouse_move",
      description:
        "Move the mouse cursor to the given image-relative pixel coordinates " +
        "without clicking. Use this to position the cursor before a click. " +
        "When a window is bound, the result includes a post-action screenshot " +
        "showing the cursor at its new position.",
      schema: mouseMoveSchema,
    },
  );
}

/**
 * Create the "mouse_click" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds an AgentOperationFrame carrying the requested click action with
 *      coordinates fixed at (0, 0); the desktop ignores them and clicks at the
 *      cursor's current position.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseClickTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ click_type }): Promise<MouseContentBlock[]> => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: CLICK_TYPE_TO_PROTO[click_type],
          xPx: 0,
          yPx: 0,
        },
      };
      const result = await bridge.dispatch(frame);
      return buildResultBlocks(result);
    },
    {
      name: "mouse_click",
      description:
        "Perform a mouse click at the current cursor position. Use mouse_move " +
        "first to position the cursor. Click types: left click, left " +
        "double-click, right click, right double-click, simultaneous " +
        "left+right press. When a window is bound, the result includes a " +
        "post-action screenshot showing the cursor at the click position.",
      schema: mouseClickSchema,
    },
  );
}

// ─── shared result-block builder ───────────────────────────────────────────

/**
 * Build the LangChain content-block array for an operation result.
 *
 * Emits the status text always, and — when the desktop captured a screenshot —
 * the image plus a pixel-dimension annotation so the model can re-estimate
 * coordinates against the correct pixel space.
 */
function buildResultBlocks(result: OperationResult): MouseContentBlock[] {
  const blocks: MouseContentBlock[] = [
    { type: "text", text: result.message },
  ];
  if (result.screenshot) {
    blocks.push({
      type: "image_url",
      image_url: {
        url: `data:image/png;base64,${result.screenshot.data}`,
      },
    });
    blocks.push({
      type: "text",
      text: `[图片像素尺寸：${result.screenshot.widthPx}×${result.screenshot.heightPx}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`,
    });
  }
  return blocks;
}
