/**
 * cordis plugin entry for the memory service host face — the row composed at
 * the agent_v2 host layer that provides `ctx.plannerMemory`. The service owns
 * the storage client (gRPC to the memory service,
 * `dominion:///game/memory:50051`), the per-agent snapshot cache and the
 * `(template, session)` bindings. The model-facing tool + snapshot section
 * live in `@dominion/dsh-memory` and reach this service by name through their
 * row's inject, so the connection infrastructure never appears as an
 * agent-dimension plugin row
 * (specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §1).
 *
 * Contract: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §1
 * (package and mount faces) / §2 (host service semantics); the storage
 * follows the existing memory service (survey/
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
