/**
 * types.ts — Shared types and helpers for the agent's tool definitions.
 *
 * `StandaloneExtras` is the per-tool `extras` payload that every tool factory
 * in `src/tools/{tool_name}/` MUST set on the `tool()` options bag. The
 * `isStandalone` helper reads it back, defaulting to `true` when the payload
 * is omitted so today's desktop-display behavior is preserved (spec
 * Assumption "default value of `standalone`").
 *
 * Authoritative contract:
 * specs/020-agent-resources-layout/contracts/tool-definition.md.
 */

import type { StructuredToolInterface } from "@langchain/core/tools";

/**
 * Shape of the per-tool `extras` payload that every tool definition MUST set.
 *
 * LangChain types `extras` as `Record<string, unknown> | undefined` on
 * `StructuredToolInterface`
 * (https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/types.ts),
 * so callers set it via `extras: { standalone: <bool> } satisfies
 * StandaloneExtras` in the `tool()` options bag and readers cast it back to
 * this shape.
 */
export interface StandaloneExtras {
  /**
   * `true` = tool is individually selectable on the desktop profile-config
   * page; `false` = hidden because intended for future toolset/collection use
   * only. Scope of effect is desktop UI visibility ONLY — never runtime
   * dispatch (spec FR-012).
   */
  standalone: boolean;
}

/**
 * Read the `standalone` flag off any tool. Returns `true` when the tool omits
 * `extras.standalone` (preserves today's desktop-display behavior — spec
 * Assumption "default value of `standalone`").
 *
 * Returns `false` ONLY when `extras.standalone === false` (explicit). Returns
 * `true` for `extras.standalone === true`, `extras.standalone === undefined`,
 * `extras === undefined`, or any other shape. This makes the default-true
 * behavior explicit at the read site.
 */
export function isStandalone(tool: StructuredToolInterface): boolean {
  const extras = tool.extras as Partial<StandaloneExtras> | undefined;
  return extras?.standalone !== false;
}
