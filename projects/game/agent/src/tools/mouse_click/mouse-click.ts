/**
 * mouse-click.ts — LangChain `mouse_click` tool that dispatches a MouseClickPart
 * through the session-scoped OperationBridge.
 *
 * On invoke the tool:
 *   1. Builds a FlowPart carrying a MouseClickPart with the requested click
 *      action; the desktop clicks at the cursor's current position.
 *   2. Reads the LangChain tool_call id from `config.toolCall.id`
 *      (contracts/tool-dispatch-contract.md §2 / research.md D2) and passes it
 *      to dispatch so the FlowPart operation, the tool_call MessagePart, and the
 *      later tool_result MessagePart share one tool_id (spec 023 FR-008).
 *   3. Dispatches it through the bridge and awaits the desktop result.
 *   4. Returns a `ToolMessage` via `buildToolResultMessage` carrying the REAL
 *      `ToolResultStatus` in `additional_kwargs.toolResultStatus`
 *      (contracts/tool-dispatch-contract.md §3 / data-model.md §6 — spec 023
 *      FR-012/FR-013). The ToolNode passes a returned ToolMessage through, so
 *      the status survives the MemorySaver checkpoint and `ListMessages`
 *      reconstructs the actual outcome (no text inference, no default-FAILED).
 *
 * Part-model contract: a tool emits a FlowPart (MouseClickPart) which the bridge
 * stamps with a tool_id and dispatches as a control frame. The bridge resolves
 * the dispatch from the matching ToolResultPart and buildToolResultMessage
 * renders it back to LangChain as a ToolMessage.
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

import type { OperationBridge } from "../../operation-bridge";
import type { FlowPart } from "../../../game_types/projects/game/FlowPart";
import { buildToolResultMessage } from "../shared/result-blocks";
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
 * @param bridge - The session-scoped OperationBridge (owned by SessionAgent).
 */
export function createMouseClickTool(
  bridge: OperationBridge,
): StructuredToolInterface {
  return tool(
    async ({ click_type }, config): Promise<ToolMessage> => {
      const signal = (config as { signal?: AbortSignal } | undefined)?.signal;
      const toolCallId = (config as { toolCall?: { id?: string } } | undefined)?.toolCall?.id;
      const part: FlowPart = {
        mouseClick: {
          click: CLICK_TYPE_TO_PROTO[click_type],
        },
      };
      const result = await bridge.dispatch(part, toolCallId, signal);
      return buildToolResultMessage(result, toolCallId, "mouse_click");
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
