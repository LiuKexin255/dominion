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
    ])
    .describe("Mouse action to perform"),
});

/**
 * Create the "mouse" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds an AgentOperationFrame carrying the mouse operation.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns the result message string to LangChain.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px, action }) => {
      const frame: AgentOperationFrame = {
        mouse: {
          action: MOUSE_ACTION_TO_PROTO[action],
          xPx: x_px,
          yPx: y_px,
        },
      };

      const result = await bridge.dispatch(frame);
      return result.message;
    },
    {
      name: "mouse",
      description:
        "Perform a mouse operation (click, double-click, right-click, or " +
        "simultaneous left+right press) at the given image-relative " +
        "pixel coordinates.",
      schema: mouseSchema,
    },
  );
}
