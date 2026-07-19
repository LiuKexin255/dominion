/**
 * mouse-click.ts — LangChain `mouse_click` tool that dispatches a MouseClickPart
 * through the session-scoped OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a Part carrying a MouseClickPart with the requested click
 *      action; the desktop clicks at the cursor's current position.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * Part-model contract: a tool emits a Part (MouseClickPart) which the bridge
 * stamps with a tool_id and dispatches as a content frame. The bridge resolves
 * the dispatch from the matching ToolResultPart and buildResultBlock renders
 * it back to LangChain content blocks.
 *
 * Relocated from `src/mouse-tool.ts` (Feature 020 — per-tool-name directory
 * layout, see `specs/020-agent-resources-layout/contracts/directory-layout.md`).
 * The tool body is verbatim; only the import paths and the new
 * `extras: { standalone: true }` field differ from the pre-refactor source
 * (FR-011 — preserve today's desktop-display behavior).
 */

import type { StructuredToolInterface } from "@langchain/core/tools";
import { tool } from "langchain";
import { z } from "zod";

import type { OperationBridge } from "../../operation-bridge";
import type { Part } from "../../../game_types/projects/game/Part";
import type { MouseContentBlock } from "../shared/result-blocks";
import { buildResultBlocks } from "../shared/result-blocks";
import type { StandaloneExtras } from "../types";

const CLICK_TYPES = [
  "LEFT_CLICK",
  "LEFT_DOUBLE_CLICK",
  "RIGHT_CLICK",
  "RIGHT_DOUBLE_CLICK",
  "LEFT_RIGHT_PRESS",
] as const;

const CLICK_TYPE_TO_PROTO = {
  LEFT_CLICK: "MOUSE_CLICK_ACTION_LEFT_CLICK",
  LEFT_DOUBLE_CLICK: "MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK",
  RIGHT_CLICK: "MOUSE_CLICK_ACTION_RIGHT_CLICK",
  RIGHT_DOUBLE_CLICK: "MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK",
  LEFT_RIGHT_PRESS: "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS",
} as const;

const mouseClickSchema = z.object({
  click_type: z
    .enum(CLICK_TYPES)
    .describe("Click type to perform at the current cursor position"),
});

/**
 * Create the "mouse_click" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a Part carrying a MouseClickPart with the requested click
 *      action; the desktop clicks at the cursor's current position.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseClickTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ click_type }, config): Promise<MouseContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const part: Part = {
        mouseClick: {
          click: CLICK_TYPE_TO_PROTO[click_type],
        },
      };
      const result = await bridge.dispatch(part, signal);
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
      extras: { standalone: true } satisfies StandaloneExtras,
    },
  );
}
