/**
 * cordis plugin entry for the memory plugin's model-facing face — the row the
 * planner preset's composition mounts
 * (`projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml`).
 * Applying it registers the `memory` tool, the three wiki tools
 * (`wiki_search`/`wiki_get`/`wiki_write` —
 * specs/068-wiki-round-pause/spec.md FR-006) and the function-form snapshot
 * section in the SAME apply, so the row's tool face is all-or-nothing
 * (line-level selection — specs/064-memory-split-fold-remain/contracts/dsh-plugins.md
 * §1/§2). The wiki tools ride this row, so they mount only where the row does
 * (planner; specs/068-wiki-round-pause/spec.md FR-010).
 *
 * The section reads the snapshot through the host service face, keyed by the
 * assembly's `context.scope` (the agent — dsh-agent's `assembleContextFor`
 * sets `scope: agent`); the host row's `load` fills that cache during the
 * materialization setup, before the first assembly. An unbound/empty snapshot
 * renders as the empty string, which the registry drops from the prompt. The
 * wiki tools inject NOTHING into the prompt — a wiki entry never enters the
 * system prompt (specs/068-wiki-round-pause/spec.md Edge Cases「wiki 不注入快照」).
 */

import type { Context } from "@deepseek-ai/cordis";

import {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
} from "@dominion/dsh-memory-service";
import { createMemoryToolDefinition } from "./tool.js";
import {
  createWikiGetToolDefinition,
  createWikiSearchToolDefinition,
  createWikiWriteToolDefinition,
} from "./wiki-tools.js";

export const name = "memory-row";

/** Scope-only row: the tool face plus the host memory service it reads. */
export const inject = ["plannerMemory", "tools", "systemPrompt"];

export function apply(ctx: Context): void {
  // The tools bind the host service from THIS context: the row declares
  // `plannerMemory` in its inject, while the agent scope's isolate boundary
  // makes `exec.agent.ctx.plannerMemory` unreachable (T023).
  ctx.tools.register(createMemoryToolDefinition(ctx.plannerMemory));
  ctx.tools.register(createWikiSearchToolDefinition(ctx.plannerMemory));
  ctx.tools.register(createWikiGetToolDefinition(ctx.plannerMemory));
  ctx.tools.register(createWikiWriteToolDefinition(ctx.plannerMemory));

  ctx.systemPrompt.section({
    name: MEMORY_SNAPSHOT_SECTION_NAME,
    order: MEMORY_SNAPSHOT_SECTION_ORDER,
    text: (context) => ctx.plannerMemory.snapshot(context.scope) ?? "",
  });
}
