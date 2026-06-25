/**
 * mouse-tool.ts — LangChain tool that dispatches mouse operations through
 * the session-scoped OperationBridge.
 *
 * The tool is created per session by SessionAgent via createMouseTool(bridge),
 * binding the bridge in a closure.
 */

import type { StructuredToolInterface } from "@langchain/core/tools";
import { tool } from "langchain";
import { z } from "zod";

import type { OperationBridge } from "./operation-bridge";
import type { AgentOperationFrame } from "../game_types/projects/game/AgentOperationFrame";

/**
 * Map the short action names exposed to the LLM to the proto enum string
 * values expected by AgentMouseOperation.action.  Kept as a const record so
 * the indexed value preserves the literal type required by the proto field.
 */
const MOUSE_ACTION_TO_PROTO = {
  LEFT_CLICK: "AGENT_MOUSE_ACTION_LEFT_CLICK",
  LEFT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK",
  RIGHT_CLICK: "AGENT_MOUSE_ACTION_RIGHT_CLICK",
  RIGHT_DOUBLE_CLICK: "AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK",
  LEFT_RIGHT_PRESS: "AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS",
  MOVE: "AGENT_MOUSE_ACTION_MOVE",
} as const;

const mouseSchema = z.object({
  x_px: z
    .number()
    .describe("X coordinate in pixels, image-relative"),
  y_px: z
    .number()
    .describe("Y coordinate in pixels, image-relative"),
  action: z
    .enum([
      "LEFT_CLICK",
      "LEFT_DOUBLE_CLICK",
      "RIGHT_CLICK",
      "RIGHT_DOUBLE_CLICK",
      "LEFT_RIGHT_PRESS",
      "MOVE",
    ])
    .describe("Mouse action to perform"),
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
 * Create the "mouse" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds an AgentOperationFrame carrying the mouse operation.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain: the status text always,
 *      and — when the desktop captured a screenshot — the image plus a
 *      pixel-dimension annotation so the model can re-estimate coordinates
 *      against the correct pixel space.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px, action }): Promise<MouseContentBlock[]> => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: MOUSE_ACTION_TO_PROTO[action],
          xPx: x_px,
          yPx: y_px,
        },
      };

      const result = await bridge.dispatch(frame);

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
    },
    {
      name: "mouse",
      description:
        "Perform a mouse operation at the given image-relative pixel " +
        "coordinates. Available actions: left click, left double-click, " +
        "right click, right double-click, simultaneous left+right press, " +
        "and move (reposition cursor without clicking). When a window is " +
        "bound, the result includes a post-action screenshot with a red " +
        "marker ring at the operation coordinates and the image pixel " +
        "dimensions.",
      schema: mouseSchema,
    },
  );
}
