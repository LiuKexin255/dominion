/**
 * cordis plugin entry for the planner-memory plugin's host service face —
 * the row composed at the agent_v2 host layer that provides
 * `ctx.plannerMemory`. The service owns the storage client (gRPC to the
 * memory service, `dominion:///game/memory:50051`), the per-agent snapshot
 * cache and `(template, session)` bindings. The model-facing tool + snapshot
 * section are the `./preset-row` export (mounted by the planner pool's
 * template preset, so the tool is visible to planner members only).
 *
 * Contract: specs/059-agent-v2-team-mode/contracts/dsh-plugins.md §3; the
 * storage follows the existing memory service (survey/
 * deepseek-harness-memory-plugin.md decision ⑤/§7 option A).
 */

import type { Context } from "@deepseek-ai/cordis";

import { MemoryClient } from "./client.js";
import { createPlannerMemory } from "./service.js";

export const name = "memory";

/** Host-row plugin: no service requirements. */
export const inject: string[] = [];

export function apply(ctx: Context): void {
  const client = new MemoryClient();
  ctx.effect(() => () => client.close(), "planner-memory.client()");
  ctx.provide("plannerMemory", createPlannerMemory({ client }));
}

export { MemoryClient, MEMORY_SERVICE_TARGET, memoryName } from "./client.js";
export type { MemoryEntry, MemoryStore } from "./client.js";
export {
  MEMORY_ACTIONS,
  applyMemoryCall,
  generateMemoryId,
  matchBySubstring,
} from "./operations.js";
export type { MemoryAction, MemoryOp, MemoryToolArgs } from "./operations.js";
export { createPlannerMemory } from "./service.js";
export type { PlannerMemoryScope, PlannerMemoryService } from "./service.js";
export {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
  renderMemorySnapshot,
} from "./snapshot.js";
export {
  MEMORY_TOOL_DESCRIPTION,
  MEMORY_TOOL_NAME,
  MEMORY_TOOL_PARAMETERS,
  createMemoryToolDefinition,
} from "./tool.js";
