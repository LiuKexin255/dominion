# Contract: Directory Layout

**Feature**: 020-agent-resources-layout
**Date**: 2026-07-18
**Status**: Phase 1 contract — MUST be satisfied by implementation.

This contract pins where every kind of agent resource lives under `projects/game/agent/src/`. It is the authoritative reference for the three READMEs that ship with this feature.

## Authority

- Spec: `specs/020-agent-resources-layout/spec.md` FR-001, FR-002, FR-007.
- Clarification Q1 (2026-07-18): resource directories live UNDER `src/` (not at the agent project root).
- Constitution Principle III (Interface-First Design): this contract is settled BEFORE any code is written.

## Top-level layout

```text
projects/game/agent/src/
├── (existing flat files: llm.ts, handler.ts, server.ts, …)   ← unchanged
│
├── tools/                ← per-tool factories + shared helpers
├── mcp/                  ← MCP integrations (empty in this feature)
└── skill/                ← built-in skills (empty in this feature)
```

All three sibling directories live under `projects/game/agent/src/` so they inherit the existing `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])` BUILD.bazel glob at `projects/game/agent/BUILD.bazel:19-22`. No glob edits required.

## `tools/` — per-tool-name folder contract

```text
src/tools/
├── README.md                       # file-format contract + brief framework context (FR-006)
├── types.ts                        # StandaloneExtras type + isStandalone helper (see contracts/tool-definition.md)
├── types.test.ts
├── shared/                         # cross-tool helpers (NOT a tool itself)
│   └── <kebab-case-name>.ts
└── {tool_name}/                    # one folder per LangChain tool, named EXACTLY the LangChain name
    ├── <kebab-case-name>.ts        # exports create{ToolName}Tool(bridge): StructuredToolInterface
    └── <kebab-case-name>.test.ts   # side-by-side test (repo convention)
```

### Rules

1. **One folder per tool.** Folder name === the tool's LangChain `name` field verbatim (e.g. tool name `mouse_move` → folder `src/tools/mouse_move/`). This deliberate underscore-in-folder-name divergence from kebab-case exists so the folder path === the value users put in a profile's `tool_names` array.
2. **One factory file per folder.** Named in kebab-case matching repo convention (e.g. `mouse-move.ts` inside `mouse_move/`). Exports `create{ToolName}Tool`.
3. **Tests side-by-side.** `<kebab-case-name>.test.ts` next to the source file (repo convention; verified at `projects/game/agent/src/mouse-tool.ts` + `projects/game/agent/src/mouse-tool.test.ts`).
4. **Shared helpers** live in `src/tools/shared/`. Use when code is referenced by 2+ tool folders. Files in `shared/` are kebab-case (e.g. `result-blocks.ts`).
5. **Every tool MUST set `extras.standalone`** (see `contracts/tool-definition.md`).

### Adding a new tool (the README will document this)

1. Create `src/tools/{tool_name}/` where `{tool_name}` is the exact LangChain name.
2. Add `<kebab-case-name>.ts` exporting `create{ToolName}Tool(bridge): StructuredToolInterface` using the `tool()` factory.
3. Set `extras: { standalone: <bool> } satisfies StandaloneExtras` in the options bag.
4. Add `<kebab-case-name>.test.ts` next to it.
5. Wire into `buildTools` at `projects/game/agent/src/llm.ts:57` (if/else if chain).
6. Run `bazel run //:gazelle projects/game/agent` to regenerate `BUILD.bazel`.

### What this feature moves

| Pre-refactor path | Post-refactor path |
|---|---|
| `src/mouse-tool.ts` (factories + schemas + shared helpers, all in one file) | SPLIT into 3 files (see below) |
| `src/mouse-tool.test.ts` (unified tests) | SPLIT into 3 test files (assertions preserved verbatim) |
| `src/llm.ts` import: `from "./mouse-tool"` | `from "./tools/mouse_move/mouse-move"` and `from "./tools/mouse_click/mouse-click"` |

#### Source split

| Source symbol | Destination |
|---|---|
| `createMouseMoveTool`, `mouseMoveSchema` | `src/tools/mouse_move/mouse-move.ts` |
| `createMouseClickTool`, `mouseClickSchema`, `CLICK_TYPES`, `CLICK_TYPE_TO_PROTO` | `src/tools/mouse_click/mouse-click.ts` |
| `MouseContentBlock` (type), `buildResultBlocks` (helper) | `src/tools/shared/result-blocks.ts` |

#### Test split

The existing `src/mouse-tool.test.ts` has tests for `createMouseMoveTool`, `createMouseClickTool`, AND `buildResultBlocks`. It relocates-and-splits into:

| Test concern | Destination |
|---|---|
| `createMouseMoveTool` invocations | `src/tools/mouse_move/mouse-move.test.ts` |
| `createMouseClickTool` invocations (each click type) | `src/tools/mouse_click/mouse-click.test.ts` |
| `buildResultBlocks` output shape (with/without screenshot) | `src/tools/shared/result-blocks.test.ts` |

Assertions and test logic are preserved verbatim — only the file boundary and the import paths change.

## `mcp/` — file-format-only contract

```text
src/mcp/
├── README.md                       # file-format contract ONLY (FR-005)
└── {mcp_name}/                     # one folder per MCP integration
    └── <files>                     # format TBD when first MCP is authored
```

### Rules (what the README documents)

1. One folder per MCP integration, named `{mcp_name}` (lowercase, kebab-case).
2. No MCP is authored in this feature — only the directory and its README.
3. The README is **file-format only** per spec FR-005: folder naming, file naming, file-format expectations. NO framework-integration content, NO runtime content, NO RPC plumbing references.

## `skill/` — built-in skill contract

```text
src/skill/
├── README.md                       # file-format contract ONLY (FR-005); pins SKILL.md format
└── {skill_name}/                   # one folder per built-in skill
    └── SKILL.md                    # the skill itself (see contracts/skill-md-format.md)
```

### Rules (what the README documents)

1. One folder per built-in skill, named `{skill_name}` (lowercase, hyphenated, matches the SKILL.md frontmatter `name`).
2. Each built-in skill is exactly one file: `SKILL.md` inside its folder. The filename `SKILL.md` is fixed — do not use `skill.md`, `Skill.md`, or `{skill_name}.md`.
3. The SKILL.md content follows the agentskills.io convention — see `contracts/skill-md-format.md`.
4. The README is **file-format only** per spec FR-005: folder naming, the fixed `SKILL.md` filename, and the SKILL.md file-format contract pointer. NO framework-integration content, NO runtime content, NO PromptService / `Skill` proto resource references (those are user-created skills, out of scope per FR-013).
5. No built-in skill is authored in this feature — only the directory and its README.

## What this feature does NOT change

- The flat top-level files in `src/` (`llm.ts`, `handler.ts`, `server.ts`, `prompt-client.ts`, `session-agent.ts`, `operation-bridge.ts`, `model-provider.ts`, `resolver-provider.ts`, `context-middleware.ts`, `secrets.ts`, `bootstrap.ts`, `bootstrap-test.ts`). All stay where they are.
- `src/llm-tools.test.ts` — tests `AgentAdapterImpl` wiring (in `llm.ts`), not individual tools. Stays at `src/llm-tools.test.ts`.
- The `buildTools` if/else if dispatch body — stays as-is; only the two import paths change.
- `BUILD.bazel` glob — already covers `src/**/*.ts`; no edit needed. Only `gazelle` regen required.

## Out of scope for this contract

- User-created skills (proto `Skill` resource at `projects/game/game.proto:438-454`).
- Desktop `TOOL_OPTIONS` filtering by `standalone` (future feature).
- Tool registry refactor (current if/else if dispatch preserved).
