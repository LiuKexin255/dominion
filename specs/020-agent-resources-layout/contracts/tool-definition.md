# Contract: Tool Definition and `standalone` Attribute

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Phase 1 contract — MUST be satisfied by implementation.

This contract pins the shape of every Tool Definition in `projects/game/agent/src/tools/` and the semantics of the `standalone` attribute introduced by this feature. It is the authoritative reference for `tasks.md`.

## Authority

- Spec: `specs/020-agent-resources-layout/spec.md` FR-009 through FR-012.
- Research: `specs/020-agent-resources-layout/research.md` §2 (LangChain `extras` field — sources cited inline).
- Constitution Principle III (Interface-First Design): this contract is settled BEFORE any code is written.

## Tool Definition shape

Every tool factory in `src/tools/{tool_name}/` MUST return a `StructuredToolInterface` produced by the `tool()` factory from the `langchain` package, with these fields populated in the options bag:

| Field | Required | Type | Constraint |
|---|---|---|---|
| `name` | YES | string | The LangChain tool name. MUST equal the parent `{tool_name}` folder name verbatim (e.g. `mouse_move`). Used by `buildTools` at `projects/game/agent/src/llm.ts:57` to match profile `tool_names` entries. |
| `description` | YES | string | Human-readable description shown to the LLM. Non-empty. |
| `schema` | YES | `z.ZodObject` | The tool's input schema. |
| `extras.standalone` | YES | boolean | Dominion-specific visibility flag. See §`standalone` Semantics below. MUST be set explicitly on every tool definition. |

```typescript
// Canonical factory pattern (concrete example: mouse_move)
import { tool } from "langchain";
import { z } from "zod";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { OperationBridge } from "../../operation-bridge";
import type { StandaloneExtras } from "../types";

const mouseMoveSchema = z.object({
  x_px: z.number().describe("X coordinate in pixels, image-relative"),
  y_px: z.number().describe("Y coordinate in pixels, image-relative"),
});

export function createMouseMoveTool(bridge: OperationBridge): StructuredToolInterface {
  return tool(
    async ({ x_px, y_px }, config) => {
      // ... unchanged implementation
    },
    {
      name: "mouse_move",
      description: "Move the mouse cursor to the given image-relative pixel coordinates without clicking. ...",
      schema: mouseMoveSchema,
      extras: { standalone: true } satisfies StandaloneExtras,
    },
  );
}
```

### Why `extras` and not `metadata`

`extras` is declared on `StructuredToolInterface` and on `ToolWrapperParams` (the `tool()` options bag) in `@langchain/core/tools` — it is statically typed, end-to-end, no casts needed. `metadata` was rejected because it propagates to every emitted `ToolMessage` and to LangSmith traces (see research.md §2 "Rejected alternatives"). `extras` does not propagate — it stays on the tool instance.

Full source references in `research.md` §2.

## `standalone` Semantics

| Value | Desktop profile-config page behavior | Agent runtime behavior |
|---|---|---|
| `true` | Tool listed as an individually-selectable chip | Tool instantiated + dispatched normally when a profile's `tool_names` references it |
| `false` | Tool HIDDEN on the page (intended for collection-only use in a future toolset feature) | Tool STILL instantiated + dispatched normally when a profile's `tool_names` references it |

**Default when omitted**: `true` (preserves today's desktop-display behavior; spec Assumption). The `isStandalone` helper below enforces this default at read time. **Contributors MUST still set `extras.standalone` explicitly** on every tool definition — the default is a safety net, not a license to omit.

**Scope of effect**: desktop UI visibility ONLY. `standalone` NEVER affects runtime behavior — the agent dispatches any tool referenced by a profile regardless of the value (spec FR-012).

**Consumer**: the desktop is NOT updated in this feature. The contract is established on the agent side so a future desktop feature can consume it. See spec Assumption "scope of this feature is the agent project only".

## `isStandalone` helper contract

Lives at `src/tools/types.ts`. Signature:

```typescript
import type { StructuredToolInterface } from "@langchain/core/tools";

export interface StandaloneExtras {
  standalone: boolean;
}

/**
 * Read the `standalone` flag off any tool. Returns `true` when the tool omits
 * `extras.standalone` (preserves today's desktop-display behavior — spec
 * Assumption "default value of `standalone`").
 *
 * Implementation contract:
 *   - Reads `tool.extras` (statically typed on StructuredToolInterface).
 *   - Returns `false` ONLY when `extras.standalone === false` (explicit).
 *   - Returns `true` for: `extras.standalone === true`, `extras.standalone === undefined`,
 *     `extras === undefined`, or any other shape. This makes the default-true
 *     behavior explicit at the read site.
 */
export function isStandalone(tool: StructuredToolInterface): boolean;
```

The helper MUST be exercised by unit tests at `src/tools/types.test.ts` covering at minimum:
1. Tool with `extras.standalone === true` → returns `true`.
2. Tool with `extras.standalone === false` → returns `false`.
3. Tool with no `extras` set → returns `true` (the default-when-omitted case).

## File location contract (cross-reference)

See `contracts/directory-layout.md` for the per-tool folder naming rule (folder name === LangChain `name`).

## Existing tools — values after this feature

| Tool | New file path | `extras.standalone` |
|---|---|---|
| `mouse_move` | `src/tools/mouse_move/mouse-move.ts` | `true` |
| `mouse_click` | `src/tools/mouse_click/mouse-click.ts` | `true` |

Both values are `true` per spec FR-011 and Q2 clarification in `spec.md` §Clarifications.

## Out of scope for this contract

- Tool registry / map-based dispatch (current `buildTools` if/else if stays unchanged).
- RPC exposing tool metadata to the desktop (future feature).
- Any tool other than `mouse_move` and `mouse_click` (no new tools authored here).
