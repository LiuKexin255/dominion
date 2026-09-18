/**
 * mouse-move.ts — LangChain `mouse_move` tool that dispatches a MouseMovePart
 * through the session-scoped OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a FlowPart carrying a MouseMovePart at the given screenshot-
 *      relative pixel coordinates.
 *   2. Reads the LangChain tool_call id from `config.toolCall.id`
 *      (contracts/tool-dispatch-contract.md §2 / research.md D2). This id is
 *      used ONLY to set the returned `ToolMessage.tool_call_id` so the
 *      conversation channel groups the tool_call↔tool_result bubble (LangChain
 *      wires the bubble grouping). It is NOT passed to dispatch: per the
 *      decoupling revision (research.md D10 / contracts/tool-dispatch-contract.md
 *      §1) the operation channel uses a bridge-minted id that is independent of
 *      the conversation `tool_call.id`.
 *   3. Dispatches it through the bridge and awaits the desktop result.
 *   4. Returns a `ToolMessage` via `buildToolResultMessage` carrying the REAL
 *      `ToolResultStatus` in `additional_kwargs.toolResultStatus`
 *      (contracts/tool-dispatch-contract.md §3 / data-model.md §6 — spec 023
 *      FR-012/FR-013). The ToolNode passes a returned ToolMessage through, so
 *      the status survives the MemorySaver checkpoint and `ListMessages`
 *      reconstructs the actual outcome (no text inference, no default-FAILED).
 *
 * Part-model contract: a tool emits a FlowPart (MouseMovePart) which the bridge
 * stamps with a bridge-minted operation-channel id and dispatches as a control
 * frame. The bridge resolves the dispatch from the matching ToolResultPart and
 * buildToolResultMessage renders it back to LangChain as a ToolMessage.
 *
 * Relocated from `src/mouse-tool.ts` (Feature 020 — per-tool-name directory
 * layout, see `specs/020-agent-resources-layout/contracts/directory-layout.md`).
 * The `extras: { standalone: true }` field is preserved from the pre-refactor
 * source (FR-011 — preserve today's desktop-display behavior).
 */

import type { StructuredToolInterface } from "@langchain/core/tools";
import type { ToolMessage } from "@langchain/core/messages";
import { tool } from "langchain";
import { z } from "zod";

import type { OperationBridge } from "../../operation-bridge.js";
import type { FlowPart } from "../../../game_types/projects/game/FlowPart.js";
import { buildToolResultMessage } from "../shared/result-blocks.js";
import type { StandaloneExtras } from "../types.js";

const mouseMoveSchema = z.object({
  x_px: z.number().describe("X coordinate in pixels, image-relative"),
  y_px: z.number().describe("Y coordinate in pixels, image-relative"),
});

/**
 * Create the "mouse_move" LangChain tool bound to a session's OperationBridge.
 *
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseMoveTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px }, config): Promise<ToolMessage> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const toolCallId = (config as { toolCall?: { id?: string } } | undefined)?.toolCall?.id;
      const part: FlowPart = {
        mouseMove: { xPx: x_px, yPx: y_px },
      };
      const result = await bridge.dispatch(part, signal);
      return buildToolResultMessage(result, toolCallId, "mouse_move");
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
