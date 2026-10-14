/**
 * The functional-unit layer: one atomic capability unit per storage domain,
 * each constructible, mountable and testable on its own
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1).
 *
 * A unit owns exactly one face key — `memory` for the memory tools, `wiki` for
 * the wiki tools — one tool set and the sections that belong to those tools
 * alone: `memory:snapshot` for memory, `wiki:guidance` (the three-tool
 * workflow) for wiki. The cross-store division of labour is not a property of
 * either unit and lives in the composition layer
 * (`./capabilities.js`;
 * specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 3/§2).
 *
 * The factories are pure functions over injected collaborators (the service
 * client, the composition-level unary policy, the scope): no I/O and no
 * registration happen here. The orchestration registers `faces` → `tools` →
 * `sections` into the member context and then awaits `ready()`.
 */

import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";

import { createMemoryFace, createWikiFace } from "./face.js";
import type { MemoryScope } from "./face.js";
import {
  WIKI_GUIDANCE,
  WIKI_GUIDANCE_SECTION_NAME,
  WIKI_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";
import { memorySnapshotSection } from "./snapshot.js";
import { MemoryClient, WikiClient } from "./store.js";
import type {
  MemoryServiceCalls,
  UnaryPolicy,
  WikiServiceCalls,
} from "./store.js";
import { createMemoryToolDefinition } from "./tool.js";
import {
  createWikiGetToolDefinition,
  createWikiSearchToolDefinition,
  createWikiWriteToolDefinition,
} from "./wiki-tools.js";

/** One agent-scoped registration: the isolate label plus the face value. */
export interface FaceRegistration {
  readonly key: string;
  readonly value: unknown;
}

/**
 * The atomic capability unit: faces/tools/sections/ready are produced together
 * and consumed together (a unit is mounted whole or not at all).
 */
export interface CapabilityUnit {
  /** The agent-scoped face registrations of this unit's own domains. */
  readonly faces: readonly FaceRegistration[];
  /** The tool definitions this unit contributes. */
  readonly tools: readonly ToolDefinition[];
  /** The prompt sections this unit contributes (name and order are frozen). */
  readonly sections: readonly PromptSection[];
  /**
   * The unit's readiness hook, awaited inside the member setup: the memory
   * unit's fail-loud prefetch, the wiki unit's no-op (a wiki has no prefetch
   * obligation).
   */
  readonly ready: () => Promise<void>;
}

/** Collaborators of {@link buildMemoryUnit}. */
export interface MemoryUnitDeps {
  /** The materialization source scope: `{template, preset}`. */
  readonly scope: MemoryScope;
  /** The MemoryService call surface built by `ctx.grpc.client<...>`. */
  readonly memoryClient: MemoryServiceCalls;
  /** The composition-level unary policy every RPC goes through. */
  readonly callUnary: UnaryPolicy;
}

/** Collaborators of {@link buildWikiUnit}. */
export interface WikiUnitDeps {
  /** The wiki collection's single scope half (one wiki set per template). */
  readonly template: string;
  /** The WikiService call surface built by `ctx.grpc.client<...>`. */
  readonly wikiClient: WikiServiceCalls;
  /** The composition-level unary policy every RPC goes through. */
  readonly callUnary: UnaryPolicy;
}

/**
 * Build the memory unit: the `memory` tool, the `memory:snapshot` section and
 * the fail-loud prefetch. It carries no guidance section — the single tool's
 * usage and snapshot semantics are already complete in
 * `MEMORY_TOOL_DESCRIPTION`
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 3).
 */
export function buildMemoryUnit(deps: MemoryUnitDeps): CapabilityUnit {
  const face = createMemoryFace(
    new MemoryClient(deps.memoryClient, deps.callUnary),
    deps.scope,
  );
  return {
    faces: [{ key: "memory", value: face }],
    tools: [createMemoryToolDefinition()],
    sections: [memorySnapshotSection(() => face.snapshotText())],
    ready: () => face.prefetch(),
  };
}

/**
 * Build the wiki unit: the three wiki tools plus `wiki:guidance` (their
 * discover → retrieve → persist workflow, which no single tool description can
 * carry). The wiki store has nothing to prefetch, so `ready` is a no-op.
 */
export function buildWikiUnit(deps: WikiUnitDeps): CapabilityUnit {
  const face = createWikiFace(
    new WikiClient(deps.wikiClient, deps.callUnary),
    deps.template,
  );
  return {
    faces: [{ key: "wiki", value: face }],
    tools: [
      createWikiSearchToolDefinition(),
      createWikiGetToolDefinition(),
      createWikiWriteToolDefinition(),
    ],
    sections: [
      {
        name: WIKI_GUIDANCE_SECTION_NAME,
        order: WIKI_GUIDANCE_SECTION_ORDER,
        text: WIKI_GUIDANCE,
      },
    ],
    ready: () => Promise.resolve(),
  };
}
