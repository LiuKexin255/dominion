# Data Model: Agent Resources Layout

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Phase 1 design output

This feature has minimal traditional data — it is a directory-layout refactor plus one new boolean attribute on an existing entity. The "data model" is the contract for *where things live* and *what shape the new attribute has*.

## Entities

### 1. Tool Definition

The code unit that implements one LangChain tool. After this feature it carries a `standalone` attribute (via `extras`) and lives under `src/tools/{tool_name}/`.

**Shape (post-feature, per `extras` decision in research.md §2):**

```typescript
interface ToolDefinitionShape {
  // LangChain-standard fields (set via tool() options bag):
  name: string;                  // e.g. "mouse_move" — also the folder name
  description: string;
  schema: ZodObject;
  // Dominion-specific extension (set via tool() options bag):
  extras: {
    standalone: boolean;         // REQUIRED; default-when-omitted = true
  };
}
```

**Identity**: tool name (the LangChain `name` field). Folder name MUST equal the tool name verbatim (e.g. `mouse_move` tool → `src/tools/mouse_move/` folder).

**Lifecycle**: stateless; created per-session by `buildTools` at `projects/game/agent/src/llm.ts:57`. No persistence, no lifecycle transitions.

**Validation rules** (enforced by review; no automated check ships in this feature):
- Folder name === LangChain `name` field.
- `extras.standalone` MUST be set explicitly on every tool definition. The `isStandalone` helper (`src/tools/types.ts`) defaults to `true` when omitted, but contributors MUST set it (the README documents this).
- File at `src/tools/{tool_name}/{kebab-case-name}.ts` exports a factory function `create{ToolName}Tool(bridge: OperationBridge): StructuredToolInterface`.

**Relationships**:
- Many Tool Definitions → one `buildTools` dispatcher (`projects/game/agent/src/llm.ts:57`).
- Many Tool Definitions may share helpers from `src/tools/shared/`.
- One Tool Definition ↔ many profile `tool_names` entries (a tool can be referenced by any number of profiles; the `standalone` value is independent of which profiles reference it).

### 2. `standalone` attribute

A boolean riding on each Tool Definition via LangChain's `extras` field (research.md §2).

**Semantics**:
- `true`: tool is individually selectable on the desktop's agent-profile configuration page (today's behavior for `mouse_move`, `mouse_click`).
- `false`: tool is hidden on that page because it is intended for collection-only use in a future toolset feature. The agent still instantiates and dispatches it normally when a profile's `tool_names` references it (spec FR-012).

**Default**: `true` when omitted (spec Assumption). The `isStandalone` helper enforces this default at read time.

**Scope of effect**: desktop UI visibility ONLY. Never affects runtime behavior, tool dispatch, profile binding, or agent composition. The desktop consumer is a future feature; this feature only establishes the contract on the agent side.

**Existing values after this feature ships**:
| Tool | `standalone` | Reason |
|---|---|---|
| `mouse_move` | `true` | Spec FR-011 / Q2 clarification — preserve today's desktop display |
| `mouse_click` | `true` | Spec FR-011 / Q2 clarification — preserve today's desktop display |

### 3. Built-in Skill (file-based)

A skill authored as a `SKILL.md` file inside `src/skill/{skill_name}/`. Distinct from user-created skills (the `Skill` proto resource at `projects/game/game.proto:438-454`), which are out of scope.

**Shape** (per `contracts/skill-md-format.md`):
```yaml
---
name: <lowercase-hyphenated>      # required, 1-64 chars, MUST match folder name
description: <string>             # required, 1-1024 chars
compatibility: <string>           # optional, recommended (e.g. "opencode")
metadata:                         # optional, recommended
  audience: <string>              # repo convention sub-key
  scope: <string>                 # repo convention sub-key
license: <string>                 # optional
---

# Free-form markdown body (purpose, when-to-use, how-to-use, examples)
```

**Identity**: skill name (frontmatter `name`). Folder name MUST equal the skill name verbatim.

**Lifecycle**: static files; no runtime lifecycle in this feature. No built-in skill is authored here — only the directory and its README.

**Relationships**:
- One Built-in Skill ↔ one `skill/{skill_name}/` folder ↔ one `SKILL.md` file.
- Built-in Skills are NOT referenced by `AgentProfile.skill_names` (that field at `projects/game/game.proto:429` references user-created skills via the `Skill` proto resource). Built-in skill discovery is a future feature.

### 4. MCP Resource (file-based, future)

A Model Context Protocol integration authored as files inside `src/mcp/{mcp_name}/`. No MCP exists today.

**Shape**: TBD — this feature only creates the directory and its file-format-only README. The README documents the folder-naming convention (`mcp/{mcp_name}/`) and reserves the entry-point file naming for when the first MCP is authored. No MCP content is added in this feature.

**Identity**: mcp name (folder name).

**Lifecycle**: N/A in this feature.

**Relationships**: One MCP Resource ↔ one `mcp/{mcp_name}/` folder. `AgentProfile.mcp_names` at `projects/game/game.proto:430` references MCPs by name; consumption is a future feature.

## Filesystem Entities (the directory contract)

These are the "data" the feature primarily delivers. Detailed in `contracts/directory-layout.md`.

| Entity | Path | Contents (this feature) |
|---|---|---|
| Tools root | `src/tools/` | `README.md`, `types.ts`, `types.test.ts`, `shared/`, `mouse_move/`, `mouse_click/` |
| Tools shared | `src/tools/shared/` | `result-blocks.ts`, `result-blocks.test.ts` |
| Mouse move tool | `src/tools/mouse_move/` | `mouse-move.ts`, `mouse-move.test.ts` |
| Mouse click tool | `src/tools/mouse_click/` | `mouse-click.ts`, `mouse-click.test.ts` |
| MCP root | `src/mcp/` | `README.md` only |
| Skill root | `src/skill/` | `README.md` only |

## Validation Rules Summary

- `bazel build //projects/game/agent/...` MUST pass (TS compiles after relocation).
- `bazel test //projects/game/agent/...` MUST pass (unit tests pass at new locations; assertions unchanged from pre-refactor baseline).
- `bazel run //:gazelle projects/game/agent` regenerates `BUILD.bazel` cleanly.
- Every Tool Definition file in `src/tools/{tool_name}/` sets `extras.standalone` explicitly (verifiable by `rg "standalone:" src/tools/`).
- The two existing tools both have `standalone: true` (verifiable by reading the two tool files).
- The three READMEs exist and contain the file-format contracts per spec FR-002 through FR-006 (verifiable by reading).
- The testplan large test for mouse dispatch still passes end-to-end (the acceptance gate per spec US2 acceptance #3).

## State Transitions

None. The feature is purely structural — no entity changes state at runtime.

## Out of Scope (per spec FR-013)

- User-created skills (`Skill` proto resource at `projects/game/game.proto:438-454`, `PromptService` RPCs at `projects/game/game.proto:140-167`).
- Desktop-side filtering of `TOOL_OPTIONS` (see spec Assumption — separate follow-up feature).
- Tool registry / `buildTools` refactor to a map (current if/else if dispatch preserved).
- Any new RPC exposing tool metadata to the desktop (future feature).
- Authoring any actual MCP integration or any actual built-in skill.
