/**
 * mouse-tool.ts — LangChain tool that dispatches mouse operations through
 * the session-scoped OperationBridge.
 *
 * The tool is created per session by SessionAgent via createMouseTool(bridge),
 * binding the bridge in a closure.  screenshot_id is NOT part of the tool's
 * Zod schema — it is read dynamically from the bridge at dispatch time so each
 * turn uses the most recent screenshot without the LLM having to track it.
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

/**
 * Zod schema for the mouse tool input.
 *
 * screenshot_id is deliberately absent — it is injected from the bridge at
 * dispatch time (per-turn dynamic context), not modelled as an LLM-facing
 * parameter.
 */
const mouseSchema = z.object({
  x_px: z
    .number()
    .describe("X coordinate in pixels, screenshot-relative"),
  y_px: z
    .number()
    .describe("Y coordinate in pixels, screenshot-relative"),
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
 *   1. Reads the current screenshot_id from the bridge.
 *   2. Builds an AgentOperationFrame carrying the mouse operation.
 *   3. Dispatches it through the bridge and awaits the desktop result.
 *   4. Returns the result message string to LangChain.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px, action }) => {
      const screenshotId = bridge.getCurrentScreenshotId();

      const frame: AgentOperationFrame = {
        screenshotId,
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
        "simultaneous left+right press) at the given screenshot-relative " +
        "pixel coordinates. The current screenshot context is injected " +
        "automatically each turn.",
      schema: mouseSchema,
    },
  );
}
