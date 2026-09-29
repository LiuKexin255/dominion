/**
 * cordis plugin entry for the memory service host face — the row composed at
 * the agent_v2 host layer that provides `ctx.plannerMemory`. The service owns
 * the storage clients (gRPC to the memory service,
 * `dominion:///game/memory:50051`: MemoryService and WikiService), the
 * per-agent snapshot cache and the `(template, preset)` bindings. The
 * model-facing tools + snapshot section live in `@dominion/dsh-memory` and
 * reach this service by name through their row's inject, so the connection
 * infrastructure never appears as an agent-dimension plugin row
 * (specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §1).
 *
 * Contract: specs/071-memory-preset-scope/contracts/dsh-plugins.md §1/§4
 * (preset scope + materialization wiring);
 * specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §1
 * (package and mount faces) / §2 (host service semantics);
 * specs/069-wiki-template-web/contracts/dsh-plugins.md §2 (wiki face); the
 * storage follows the existing memory service (survey/
 * deepseek-harness-memory-plugin.md decision ⑤/§7 option A).
 */

import type { Context } from "@deepseek-ai/cordis";

import { MemoryClient } from "./client.js";
import { createPlannerMemory } from "./service.js";
import { WikiClient } from "./wiki-client.js";

export const name = "memory";

/** Host-row plugin: no service requirements. */
export const inject: string[] = [];

export function apply(ctx: Context): void {
  const client = new MemoryClient();
  const wikiStore = new WikiClient();
  ctx.effect(() => () => client.close(), "planner-memory.client()");
  ctx.effect(() => () => wikiStore.close(), "planner-memory.wiki-client()");
  ctx.provide("plannerMemory", createPlannerMemory({ client, wikiStore }));
}

export {
  MemoryClient,
  MEMORY_SERVICE_TARGET,
  memoryName,
  parseMemoryName,
} from "./client.js";
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
  WikiClient,
  parseWikiName,
  wikiName,
} from "./wiki-client.js";
export type {
  ListWikisOptions,
  ListWikisPage,
  SearchWikisOptions,
  SearchWikisPage,
  Wiki,
  WikiStore,
  WikiSummary,
  WikiUpdate,
} from "./wiki-client.js";
export {
  WIKI_CONTENT_MAX_BYTES,
  WIKI_GET_EMPTY_TITLES_TEXT,
  WIKI_SEARCH_EMPTY_QUERY_TEXT,
  WIKI_SEARCH_PAGE_SIZE,
  WIKI_TITLE_MAX_RUNES,
  WIKI_WRITE_AMBIGUOUS_ARGS_TEXT,
  WIKI_WRITE_EMPTY_ENTRIES_TEXT,
  WIKI_WRITE_MISSING_ARGS_TEXT,
  generateWikiId,
  normalizeWikiTitle,
  wikiGet,
  wikiSearch,
  wikiWrite,
} from "./wiki-operations.js";
export type { WikiWriteArgs, WikiWriteEntry } from "./wiki-operations.js";
