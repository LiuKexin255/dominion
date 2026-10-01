/**
 * `@dominion/common-js-memory` — the memory capability library (no cordis
 * plugin form: the package exports no `name`/`inject`/`apply` and is never
 * mounted as a composition row).
 *
 * Export surface (specs/074-dsh-grpc-runtime/contracts/memory-library.md):
 *
 * - the atomic capability unit factory ({@link buildMemoryCapabilities}) and
 *   its types;
 * - the primitives: {@link createMemoryStore}, {@link createMemoryFace}, the
 *   snapshot renderer/section, and the structured call surfaces consumed by
 *   the adapter-provided clients;
 * - the four tool definitions, the two guidance sections and the domain
 *   operation cores.
 *
 * The library is domain-agnostic: no scenario vocabulary, no scope
 * self-derivation, no assembly flow, no deployment layout knowledge.
 */

export {
  buildMemoryCapabilities,
} from "./capabilities.js";
export type {
  MemoryCapabilities,
  MemoryCapabilitiesDeps,
  MemoryFaceRegistration,
} from "./capabilities.js";

export { createMemoryFace, createMemoryStore } from "./face.js";
export type { MemoryFace, MemoryScope } from "./face.js";

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
} from "./wiki-tools.js";

export { describeErr } from "./errors.js";
