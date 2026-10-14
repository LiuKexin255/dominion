/**
 * `@dominion/common-js-memory` — the memory capability library (no cordis
 * plugin form: the package exports no `name`/`inject`/`apply` and is never
 * mounted as a composition row).
 *
 * Export surface (specs/076-agent-v2-optimize/contracts/
 * memory-library-layering.md):
 *
 * - the functional-unit layer ({@link buildMemoryUnit}, {@link buildWikiUnit})
 *   and the composition layer ({@link buildMemoryWikiComposition}), plus the
 *   `CapabilityUnit` they both produce;
 * - the primitives: {@link createMemoryFace}, {@link createWikiFace}, the
 *   snapshot renderer/section, and the structured call surfaces consumed by
 *   the adapter-provided clients (including the injected {@link UnaryPolicy}
 *   seam);
 * - the four tool definitions, the two guidance texts and the domain
 *   operation cores.
 *
 * The library is domain-agnostic: no scenario vocabulary, no scope
 * self-derivation, no assembly flow, no deployment layout knowledge.
 */

export {
  buildMemoryUnit,
  buildWikiUnit,
} from "./units.js";
export type {
  CapabilityUnit,
  FaceRegistration,
  MemoryUnitDeps,
  WikiUnitDeps,
} from "./units.js";

export { buildMemoryWikiComposition } from "./capabilities.js";
export type { MemoryWikiCompositionDeps } from "./capabilities.js";

export { createMemoryFace, createWikiFace } from "./face.js";
export type { MemoryFace, MemoryScope, WikiFace } from "./face.js";

export {
  MEMORY_SNAPSHOT_SECTION_NAME,
  MEMORY_SNAPSHOT_SECTION_ORDER,
  SNAPSHOT_ENTRY_LIMIT,
  SNAPSHOT_ORDER_BY,
  memorySnapshotSection,
  renderMemorySnapshot,
} from "./snapshot.js";

export {
  MemoryClient,
  WikiClient,
  memoryName,
  parseMemoryName,
  parseWikiName,
  wikiName,
} from "./store.js";
export type {
  ListMemoriesOptions,
  ListWikisOptions,
  ListWikisPage,
  MemoryEntry,
  MemoryServiceCalls,
  MemoryStore,
  MemoryStoreRpcs,
  SearchWikisOptions,
  SearchWikisPage,
  UnaryCall,
  UnaryClient,
  UnaryInvoke,
  UnaryPolicy,
  Wiki,
  WikiServiceCalls,
  WikiStore,
  WikiSummary,
  WikiUpdate,
} from "./store.js";

export {
  MEMORY_ACTIONS,
  MEMORY_CONTENT_MAX_RUNES,
  applyMemoryCall,
  generateMemoryId,
  matchBySubstring,
} from "./operations.js";
export type { MemoryAction, MemoryOp, MemoryToolArgs } from "./operations.js";

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

export {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";

export {
  MEMORY_TOOL_DESCRIPTION,
  MEMORY_TOOL_NAME,
  MEMORY_TOOL_PARAMETERS,
  createMemoryToolDefinition,
  executeMemoryTool,
  resolveMemoryFace,
} from "./tool.js";

export {
  WIKI_GET_TOOL_DESCRIPTION,
  WIKI_GET_TOOL_NAME,
  WIKI_GET_TOOL_PARAMETERS,
  WIKI_SEARCH_TOOL_DESCRIPTION,
  WIKI_SEARCH_TOOL_NAME,
  WIKI_SEARCH_TOOL_PARAMETERS,
  WIKI_WRITE_TOOL_DESCRIPTION,
  WIKI_WRITE_TOOL_NAME,
  WIKI_WRITE_TOOL_PARAMETERS,
  createWikiGetToolDefinition,
  createWikiSearchToolDefinition,
  createWikiWriteToolDefinition,
  executeWikiGetTool,
  executeWikiSearchTool,
  executeWikiWriteTool,
  resolveWikiFace,
} from "./wiki-tools.js";

export { describeErr } from "./errors.js";
