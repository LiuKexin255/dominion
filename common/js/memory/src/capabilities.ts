/**
 * The atomic capability unit factory. `buildMemoryCapabilities` produces the
 * four fields together — the agent-scoped face, the four tool definitions,
 * the three prompt sections, and the fail-loud prefetch — as one indivisible
 * unit: the orchestration mounts all of it or none of it
 * (specs/074-dsh-grpc-runtime/contracts/memory-library.md §1; user constraint:
 * a capability unit is registered whole).
 *
 * The factory is a pure function over the injected clients and scope: it
 * performs no I/O and registers nothing. The orchestration registers
 * `faces` → `tools` → `sections` into the member context, then awaits
 * `ready()` (the fail-loud prefetch that rolls the member back on rejection).
 */

import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";

import { createMemoryFace, createMemoryStore } from "./face.js";
import type { MemoryFace, MemoryScope } from "./face.js";
import {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";
import { memorySnapshotSection } from "./snapshot.js";
import type { MemoryServiceCalls, WikiServiceCalls } from "./store.js";
import { createMemoryToolDefinition } from "./tool.js";
import {
  createWikiGetToolDefinition,
  createWikiSearchToolDefinition,
  createWikiWriteToolDefinition,
} from "./wiki-tools.js";

/** One agent-scoped registration: the isolate label plus the face value. */
export interface MemoryFaceRegistration {
  readonly key: string;
  readonly value: MemoryFace;
}

/**
 * The atomic capability unit: faces/tools/sections/ready are produced
 * together and consumed together.
 */
export interface MemoryCapabilities {
  /** The agent-scoped face registrations (`[{key: "memory", value: face}]`). */
  readonly faces: readonly MemoryFaceRegistration[];
  /** The four tool definitions (`memory` + the three wiki tools). */
  readonly tools: readonly ToolDefinition[];
  /**
   * The three prompt sections (`memory:guidance`, `wiki:guidance`,
   * `memory:snapshot`).
   */
  readonly sections: readonly PromptSection[];
  /** Fail-loud prefetch: await before publishing the member. */
  readonly ready: () => Promise<void>;
}

/** The factory dependencies. */
export interface MemoryCapabilitiesDeps {
  readonly scope: MemoryScope;
  readonly memoryClient: MemoryServiceCalls;
  readonly wikiClient: WikiServiceCalls;
}

/**
 * Build the atomic capability unit for one member scope. Section names,
 * orders and text are unchanged from the pre-migration plugin row, so the
 * assembled system prompt is byte-identical (specs/074-dsh-grpc-runtime/
 * contracts/memory-library.md §1).
 */
export function buildMemoryCapabilities(
  deps: MemoryCapabilitiesDeps,
): MemoryCapabilities {
  const store = createMemoryStore(deps.memoryClient, deps.wikiClient);
  const face = createMemoryFace(store, deps.scope);
  return {
    faces: [{ key: "memory", value: face }],
    tools: [
      createMemoryToolDefinition(),
      createWikiSearchToolDefinition(),
      createWikiGetToolDefinition(),
      createWikiWriteToolDefinition(),
    ],
    sections: [
      {
        name: MEMORY_GUIDANCE_SECTION_NAME,
        order: MEMORY_GUIDANCE_SECTION_ORDER,
        text: MEMORY_GUIDANCE,
      },
      {
        name: WIKI_GUIDANCE_SECTION_NAME,
        order: WIKI_GUIDANCE_SECTION_ORDER,
        text: WIKI_GUIDANCE,
      },
      memorySnapshotSection(() => face.snapshotText()),
    ],
    ready: () => face.prefetch(),
  };
}
