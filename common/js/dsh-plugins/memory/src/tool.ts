/**
 * The `memory` tool — the single model-facing tool of the memory plugin's
 * preset row. Parameter schema and domain semantics follow spec 039 T015:
 * the flat `action`/`content`/`old_text` single-operation form XOR the
 * `operations[]`
 * batch form is validated in the operation core and answered with TEXT, no
 * read action exists, and every failure is a text result (never a thrown
 * tool error — 031 C15 neutral status; specs/059-agent-v2-team-mode/spec.md
 * FR-007). The description is rewritten for the v2 snapshot semantics: the
 * system-prompt snapshot is fixed at agent start and is NOT refreshed by
 * writes (survey/deepseek-harness-memory-plugin.md §4.1/decision ③).
 *
 * Storage access goes through the host service face at execution time
 * (`exec.agent.ctx.plannerMemory`, the fiber/ctx property walk used by the
 * saolei tools' `saoleiGame` resolution — dsh-tools `ToolExecution.agent`
 * carries the calling agent), which carries the agent's bound
 * `(template, session)` scope; the tool never closes over a scope.
 */

import { defineTool } from "@deepseek-ai/dsh-tools";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";

import { MEMORY_ACTIONS } from "@dominion/dsh-memory-service";
import type {
  MemoryToolArgs,
  PlannerMemoryService,
} from "@dominion/dsh-memory-service";

/** The tool name (kept from v1: the memory service has no other tool face). */
export const MEMORY_TOOL_NAME = "memory";

/**
 * Model-facing description: v1's wording with the two stale references
 * removed — the compression-boundary refresh (v2 has no compression) and the
 * memory skill (not migrated); replaced by the v2 snapshot semantics.
 */
export const MEMORY_TOOL_DESCRIPTION =
  "Manage the planner's long-term review memory (hermes-style single tool). " +
  "action=add records a new entry (content); action=replace/remove locate an " +
  "existing entry by a SHORT old_text substring and update (content) or " +
  "delete it; operations applies a batch atomically (all-or-nothing). " +
  "Substring matching is case-sensitive; a 0/multiple match returns the " +
  "current entries to pick a more specific old_text. Changes persist " +
  "immediately; the snapshot in your system prompt is fixed at agent start.";

/**
 * The parameter schema (v1 `memory-mcp.ts:427-471`): all fields optional at
 * the root — the single form XOR the batch form is a runtime contract
 * answered with text (a schema-level oneOf would turn the model's
 * recoverable mistake into an INVALID_ARGS failure). `operations` items
 * require `action`, matching v1's `z.enum(MEMORY_ACTIONS)` item schema, and
 * deliberately reject unknown keys (`additionalProperties: false`): v1's
 * zod object silently stripped them, and a typo'd batch field silently
 * applying a different operation is worse than a loud schema rejection the
 * model can correct.
 */
export const MEMORY_TOOL_PARAMETERS = {
  action: {
    type: "string",
    enum: MEMORY_ACTIONS,
    description:
      "single-operation form: add/replace/remove (mutually exclusive with operations)",
  },
  content: {
    type: "string",
    description: "entry body (add/replace)",
  },
  old_text: {
    type: "string",
    description: "short substring locating an existing entry (replace/remove)",
  },
  operations: {
    type: "array",
    description:
      "batch form: ordered memory operations (mutually exclusive with action)",
    items: {
      type: "object",
      additionalProperties: false,
      properties: {
        action: {
          type: "string",
          enum: MEMORY_ACTIONS,
          required: true,
        },
        content: { type: "string" },
        old_text: { type: "string" },
      },
    },
  },
} as const;

/** The canonical `{result: string}` output (saolei tools' declaration shape). */
const MEMORY_RESULT_OUTPUT_SCHEMA = {
  type: "object",
  properties: {
    result: {
      type: "string",
      required: true,
      description:
        "The memory result text: a success line, the domain feedback (0/multiple matches), or `memory failed: …`",
    },
  },
  additionalProperties: false,
} as const;

/**
 * The execution-time service face: the row's `apply` captures
 * `ctx.plannerMemory` from ITS OWN context and binds it here. Resolving
 * through `exec.agent.ctx.plannerMemory` does NOT work: the host service
 * lives in the host realm, and the agent scope's isolate boundary rejects the
 * property walk ("cannot get property … without inject" — T023 caught it in
 * the deployed topology; the row ctx is the composition point that declares
 * the dependency).
 */
export type MemoryToolService = Pick<PlannerMemoryService, "applyCall">;

function describeErr(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Execute one `memory` call. Any failure — missing agent caller, storage
 * outage — becomes a `memory failed: …` TEXT result, so the model can react
 * and the conversation is never interrupted (v1 memory-mcp.ts:472-482).
 */
export async function executeMemoryTool(
  service: MemoryToolService,
  args: MemoryToolArgs,
  exec: ToolRunContext,
): Promise<{ result: string }> {
  try {
    const agent = exec.agent;
    if (agent === undefined) {
      throw new Error(
        "memory tool requires an agent caller: no agent on the tool execution",
      );
    }
    return { result: await service.applyCall(agent, args) };
  } catch (err) {
    return { result: `memory failed: ${describeErr(err)}` };
  }
}

/** Build the registry-ready `memory` tool definition (no guidance section:
 * a single tool has no cross-call coordination need —
 * survey/deepseek-harness-memory-plugin.md §4.2). The host service is bound
 * by the caller (the preset row's `apply`) so the tool never has to cross the
 * agent scope's isolate boundary. */
export function createMemoryToolDefinition(service: MemoryToolService): ToolDefinition {
  return defineTool({
    name: MEMORY_TOOL_NAME,
    description: MEMORY_TOOL_DESCRIPTION,
    parameters: MEMORY_TOOL_PARAMETERS,
    output: {
      schema: MEMORY_RESULT_OUTPUT_SCHEMA,
      render: (_args, value) => [{ type: "text", text: value.result }],
    },
    execute: (args, exec) => executeMemoryTool(service, args, exec),
  });
}
