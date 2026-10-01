/**
 * The `memory` tool — the capability unit's single memory tool. Parameter
 * schema and domain semantics follow spec 039 T015: the flat
 * `action`/`content`/`old_text` single-operation form XOR the `operations[]`
 * batch form is validated in the operation core and answered with TEXT, no
 * read action exists, and every failure is a text result (never a thrown
 * tool error — 031 C15 neutral status; specs/059-agent-v2-team-mode/spec.md
 * FR-007). The description is a generic, scenario-free statement of the
 * tool's own function (specs/068-wiki-round-pause/spec.md FR-012): writes
 * persist to the full stored memory, while the system-prompt snapshot holds
 * only the 10 most recently updated entries and is not refreshed by writes.
 *
 * Storage access is resolved lazily at execution time through the calling
 * agent's context property walk (`exec.agent.ctx.memory`) — the agent-scoped
 * face the orchestration registers with `agentCtx.isolate("memory", face)`
 * (saolei `saoleiGame` pattern; specs/074-dsh-grpc-runtime/contracts/
 * memory-library.md §5). A missing agent or missing face fails loud — never a
 * silent success; the failure text names the missing face and the registering
 * party.
 */

import { defineTool } from "@deepseek-ai/dsh-tools";
import type { ToolDefinition, ToolRunContext } from "@deepseek-ai/dsh-tools";

import type { MemoryFace } from "./face.js";
import { MEMORY_ACTIONS } from "./operations.js";
import type { MemoryToolArgs } from "./operations.js";

/** The tool name (kept from v1: the memory service has no other tool face). */
export const MEMORY_TOOL_NAME = "memory";

/**
 * Model-facing description: a generic statement of the tool's own function
 * (no deployment-specific scenario nouns, no third-party framework terms)
 * plus the snapshot recency semantics (specs/068-wiki-round-pause/spec.md
 * FR-012). The top-10 bound and the full-storage write semantics mirror the
 * snapshot loader in
 * specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §2.
 */
export const MEMORY_TOOL_DESCRIPTION =
  "Manage long-term memory entries. " +
  "action=add records a new entry (content); action=replace/remove locate an " +
  "existing entry by a SHORT old_text substring and update (content) or " +
  "delete it; operations applies a batch of actions in one call. " +
  "Substring matching is case-sensitive; a 0/multiple match returns the " +
  "current entries to pick a more specific old_text. Writes persist " +
  "immediately and act on the full stored memory. The memory snapshot in " +
  "your system prompt contains only the 10 most recently updated entries " +
  "and is not refreshed by writes.";

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
 * Resolve the calling agent's memory face. Failure is fail-loud: a missing
 * `exec.agent` (non-loop-driven dispatch) or an unregistered `memory` face
 * (the orchestration did not mount the capability unit) throws, and the
 * pipeline turns the throw into a model-visible error result — never a
 * fabricated success.
 *
 * Resolution reads the `memory` context PROPERTY (the proxy's fiber-walking
 * lookup), not `ctx.get`: the orchestration registers the face under an
 * isolation label (`agentCtx.isolate("memory", face)`), which the property
 * walk resolves from the shared agent fiber while `get`'s label-keyed store
 * lookup would not (saolei `saoleiGame` precedent).
 *
 * @param toolName The calling tool's name, so the missing-agent text names
 *   the tool that actually ran (`memory`/`wiki_search`/`wiki_get`/`wiki_write`).
 */
export function resolveMemoryFace(
  exec: ToolRunContext,
  toolName = MEMORY_TOOL_NAME,
): MemoryFace {
  if (exec.agent === undefined) {
    throw new Error(
      `${toolName} tool requires an agent caller: no agent on the tool execution`,
    );
  }
  const face = (exec.agent.ctx as { memory?: MemoryFace }).memory;
  if (face === undefined) {
    throw new Error(
      `memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none\n` +
        "(the team-layer materialization setup registers it from the memory capability mount)",
    );
  }
  return face;
}

/**
 * Execute one `memory` call. Any failure — missing agent caller, unregistered
 * face, storage outage — becomes a `memory failed: …` TEXT result, so the
 * model can react and the conversation is never interrupted (v1
 * memory-mcp.ts:472-482).
 */
export async function executeMemoryTool(
  args: MemoryToolArgs,
  exec: ToolRunContext,
): Promise<{ result: string }> {
  try {
    return {
      result: await resolveMemoryFace(exec, MEMORY_TOOL_NAME).applyCall(args),
    };
  } catch (err) {
    return { result: `memory failed: ${describeErr(err)}` };
  }
}

function describeErr(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Build the registry-ready `memory` tool definition. The cross-tool
 * coordination guidance is a separate prompt section owned by the capability
 * unit (`./capabilities.js`) and never part of this tool's own description
 * (specs/068-wiki-round-pause/contracts/dsh-plugins.md §3). Storage access is
 * resolved per call through `exec.agent.ctx.memory`. */
export function createMemoryToolDefinition(): ToolDefinition {
  return defineTool({
    name: MEMORY_TOOL_NAME,
    description: MEMORY_TOOL_DESCRIPTION,
    parameters: MEMORY_TOOL_PARAMETERS,
    output: {
      schema: MEMORY_RESULT_OUTPUT_SCHEMA,
      render: (_args, value) => [{ type: "text", text: value.result }],
    },
    execute: (args, exec) => executeMemoryTool(args, exec),
  });
}
