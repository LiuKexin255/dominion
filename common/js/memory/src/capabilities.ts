/**
 * The composition layer: merge functionally independent units into one
 * capability surface a member mounts whole
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §2).
 *
 * The composition is the frozen capability surface the equivalence anchor
 * defines (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §2):
 * the four tools, the section names and orders `memory:guidance`(110) /
 * `wiki:guidance`(120) / `memory:snapshot`(200), the assembled prompt text and
 * the memory prefetch (spec FR-004/SC-003).
 *
 * The `memory:guidance` section (the memory-vs-wiki division of labour) is
 * owned here, not by either unit: the division only means anything while both
 * stores are mounted, so no unit-bound prompt ever references a tool that is
 * not part of that mount
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 3).
 */

import type { PromptSection } from "@deepseek-ai/dsh-system-prompt";

import type { MemoryScope } from "./face.js";
import {
  MEMORY_GUIDANCE,
  MEMORY_GUIDANCE_SECTION_NAME,
  MEMORY_GUIDANCE_SECTION_ORDER,
} from "./guidance.js";
import type {
  MemoryServiceCalls,
  UnaryPolicy,
  WikiServiceCalls,
} from "./store.js";
import { buildMemoryUnit, buildWikiUnit } from "./units.js";
import type { CapabilityUnit } from "./units.js";

/** Collaborators of {@link buildMemoryWikiComposition}. */
export interface MemoryWikiCompositionDeps {
  /** The memory scope: `{template, preset}`; its template also scopes the wiki. */
  readonly scope: MemoryScope;
  /** The MemoryService call surface built by `ctx.grpc.client<...>`. */
  readonly memoryClient: MemoryServiceCalls;
  /** The WikiService call surface built by `ctx.grpc.client<...>`. */
  readonly wikiClient: WikiServiceCalls;
  /** The composition-level unary policy every RPC goes through. */
  readonly callUnary: UnaryPolicy;
}

/**
 * Compose the memory and wiki units into one mountable unit, adding the
 * composition-owned division-of-labour section. The memory unit's prefetch is
 * the composition's readiness hook (wiki has none).
 */
export function buildMemoryWikiComposition(
  deps: MemoryWikiCompositionDeps,
): CapabilityUnit {
  const memory = buildMemoryUnit({
    scope: deps.scope,
    memoryClient: deps.memoryClient,
    callUnary: deps.callUnary,
  });
  const wiki = buildWikiUnit({
    template: deps.scope.template,
    wikiClient: deps.wikiClient,
    callUnary: deps.callUnary,
  });
  // The division section leads the guidance band it shares with the wiki
  // workflow, so the section order is 110/120/200
  // (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §2).
  const division: PromptSection = {
    name: MEMORY_GUIDANCE_SECTION_NAME,
    order: MEMORY_GUIDANCE_SECTION_ORDER,
    text: MEMORY_GUIDANCE,
  };
  return {
    faces: [...memory.faces, ...wiki.faces],
    tools: [...memory.tools, ...wiki.tools],
    sections: [division, ...wiki.sections, ...memory.sections],
    ready: memory.ready,
  };
}
