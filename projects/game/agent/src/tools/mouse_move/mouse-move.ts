/**
 * mouse-move.ts — LangChain `mouse_move` tool that dispatches a MouseMovePart
 * through the session-scoped OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a Part carrying a MouseMovePart at the given screenshot-relative
 *      pixel coordinates.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * Part-model contract: a tool emits a Part (MouseMovePart) which the bridge
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

const mouseMoveSchema = z.object({
  x_px: z.number().describe("X coordinate in pixels, image-relative"),
  y_px: z.number().describe("Y coordinate in pixels, image-relative"),
});

/**
 * Create the "mouse_move" LangChain tool bound to a session's OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a Part carrying a MouseMovePart at the given screenshot-relative
 *      pixel coordinates.
 *   2. Dispatches it through the bridge and awaits the desktop result.
 *   3. Returns a content-block array to LangChain via buildResultBlocks.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseMoveTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px }, config): Promise<MouseContentBlock[]> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const part: Part = {
        mouseMove: { xPx: x_px, yPx: y_px },
      };
      const result = await bridge.dispatch(part, signal);
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
      extras: { standalone: true } satisfies StandaloneExtras,
    },
  );
}
