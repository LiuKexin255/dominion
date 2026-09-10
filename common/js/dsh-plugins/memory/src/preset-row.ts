/**
 * cordis preset-row plugin for the planner-memory plugin's model-facing
 * face — the row the planner preset's composition mounts
 * (`projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml`).
 * Applying it registers the single `memory` tool plus the function-form
 * snapshot section in the SAME apply, so the row's tool/guidance pair is
 * all-or-nothing (line-level selection —
 * specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3 item 2). It
 * registers NO guidance section: a single tool has no cross-call coordination
 * need (survey/deepseek-harness-memory-plugin.md §4.2).
 *
 * The section reads the snapshot through the host service face, keyed by the
 * assembly's `context.scope` (the agent — dsh-agent's `assembleContextFor`
 * sets `scope: agent`); the host row's `load` fills that cache during the
 * materialization setup, before the first assembly. An unbound/empty snapshot
 * renders as the empty string, which the registry drops from the prompt.
 */

import type { Context } from "@deepseek-ai/cordis";

import {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
} from "./snapshot.js";
import { createMemoryToolDefinition } from "./tool.js";

export const name = "memory-row";

/** Scope-only row: the tool face plus the host memory service it reads. */
export const inject = ["plannerMemory", "tools", "systemPrompt"];

export function apply(ctx: Context): void {
  // The tool binds the host service from THIS context: the row declares
  // `plannerMemory` in its inject, while the agent scope's isolate boundary
  // makes `exec.agent.ctx.plannerMemory` unreachable (T023).
  ctx.tools.register(createMemoryToolDefinition(ctx.plannerMemory));

  ctx.systemPrompt.section({
    name: MEMORY_SNAPSHOT_SECTION_NAME,
    order: MEMORY_SNAPSHOT_SECTION_ORDER,
    text: (context) => ctx.plannerMemory.snapshot(context.scope) ?? "",
  });
}
