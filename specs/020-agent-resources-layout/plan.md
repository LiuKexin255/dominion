# Implementation Plan: Agent Resources Layout

**Branch**: `020-agent-resources-layout` | **Date**: 2026-07-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/020-agent-resources-layout/spec.md`

## Summary

Reorganize the game agent's source tree into three peer resource directories under `projects/game/agent/src/` — `mcp/`, `tools/`, `skill/` — each with a README documenting its file-format contract; relocate the existing mouse tools into the new `tools/{tool_name}/` layout behavior-preservingly; add a `standalone` boolean attribute to every tool definition via LangChain's `extras` field so future desktop profile-config UI can hide collection-only tools. Built-in skills follow the agentskills.io SKILL.md convention; user-created skills (the `Skill` proto resource at `projects/game/game.proto:438`) are explicitly out of scope. No MCP or built-in skill is authored in this feature — only the directories and their file-format READMEs.

## Technical Context

**Language/Version**: TypeScript (catalog-managed via `pnpm-workspace.yaml`; runtime = Node.js). See `projects/game/agent/package.json`.

**Primary Dependencies**:
- `langchain` (catalog) — provides the `tool()` factory and `createAgent` (see `projects/game/agent/src/llm.ts:15`).
- `@langchain/core` (catalog) — provides `StructuredToolInterface` (the static tool type referenced in `projects/game/agent/src/llm.ts:14` and `projects/game/agent/src/mouse-tool.ts:18`).
- `zod` (catalog) — schema definitions on tools.
- Bazel + `aspect_rules_swc` / `aspect_rules_ts` / `aspect_rules_js` — build/test entry per `AGENTS.md`. `gazelle` regenerates `BUILD.bazel` from the file tree.

**Storage**: N/A (no persistence added; this is a refactor + READMEs).

**Testing**: `vitest` (catalog) via the `vitest_test` Bazel rule at `tools/dev/js/vitest_test.bzl`. Default `args = ["run", "src/"]` sweeps the whole `src/` tree, so test files in any subdirectory under `src/` are auto-discovered. Large tests via `testplan` skill (`tools/test/guitar`).

**Target Platform**: Linux container (gRPC server), per `projects/game/agent/src/bootstrap.ts` and `service.yaml`.

**Project Type**: gRPC service (the game agent).

**Performance Goals**: N/A (refactor preserves behavior; no new hot paths).

**Constraints**:
- Relocation MUST be behavior-preserving — `bazel build //projects/game/agent/...` and `bazel test //projects/game/agent/...` MUST pass identically to the pre-refactor baseline.
- The `extras` field is the LangChain-blessed extension point for provider-specific / arbitrary tool fields (see research.md §LangChain); `metadata` was rejected because it propagates to every emitted `ToolMessage` and LangSmith trace (semantic overloading).
- The `BUILD.bazel` glob `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])` at `projects/game/agent/BUILD.bazel:19-22` already covers `src/tools/**`, so moving the mouse tools under `src/tools/` requires no glob edit — only a `gazelle` regen.

**Scale/Scope**: One service (`projects/game/agent/`); 1 file pair to relocate (`mouse-tool.ts` + tests split by tool); 3 READMEs to author; one new typed helper (`isStandalone`); no new RPCs, no proto changes, no desktop changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|---|---|---|
| **I. Citation & Provenance** | PASS | All cross-references in this plan use repo-relative paths or full URLs (e.g. `projects/game/agent/src/llm.ts:57`, `https://agentskills.io/specification`). |
| **II. Refactoring Over Patching** | PASS | The directory reorganization IS the architectural change (collapsing flat `src/` into resource categories); the `standalone` attribute is added at the tool-definition layer (architectural), not patched onto a downstream consumer. No "patches" layered on top of an unchanged architecture. |
| **III. Interface-First Design** | PASS | Phase 1 produces `contracts/` artifacts declaring: (a) the `ToolDefinition` shape and `isStandalone` helper signature, (b) the directory-layout contract, (c) the SKILL.md file-format contract. The READMEs themselves are part of the contract surface. No code is written before the contracts are pinned. |
| **IV. Test Granularity & Cadence** | PASS | Unit tests (vitest) move with the source and run on every code change as part of the implementation tasks; no separate "test" task. The existing testplan large test is the acceptance gate, called out as a discrete validation step. |
| **V. Read Before Code** | PASS | tasks.md (next phase) MUST list every doc a coding agent will read. The research artifacts below (esp. the agentskills.io spec URL, LangChain `extras` source URL) are already read and cited here so they can be carried forward verbatim. |
| **VI. Large Test Acceptance for Services** | PASS | This is a service (`projects/game/agent/`). The existing testplan at `projects/game/testplan/` exercises mouse dispatch end-to-end; quickstart.md makes running it the final acceptance gate. |

**Post-Phase-1 re-check**: still PASS — Phase 1 design (data-model.md, contracts/, quickstart.md) introduces no new violation; the `extras`-field pattern is minimal and justified (Complexity Tracking not needed).

## Project Structure

### Documentation (this feature)

```text
specs/020-agent-resources-layout/
├── plan.md              # This file
├── spec.md              # /speckit.specify output
├── research.md          # Phase 0 output — SKILL.md convention, LangChain extras field, repo conventions
├── data-model.md        # Phase 1 output — Tool Definition entity, standalone attribute, file-layout entities
├── quickstart.md        # Phase 1 output — build + unit-test + large-test validation guide
├── contracts/           # Phase 1 output
│   ├── tool-definition.md     # ToolDefinition shape, isStandalone helper, extras schema
│   ├── directory-layout.md    # src/{mcp,tools,skill}/ contract
│   └── skill-md-format.md     # agentskills.io SKILL.md frontmatter + body contract
├── checklists/
│   └── requirements.md  # /speckit.specify output (already validated)
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
projects/game/agent/src/
├── llm.ts                       # Unchanged location; buildTools imports updated
├── llm.test.ts                  # Unchanged
├── llm-tools.test.ts            # Unchanged location (tests AgentAdapterImpl wiring, not tools)
├── handler.ts                   # Unchanged
├── session-agent.ts             # Unchanged
├── operation-bridge.ts          # Unchanged
├── prompt-client.ts             # Unchanged
├── server.ts                    # Unchanged
├── model-provider.ts            # Unchanged
├── resolver-provider.ts         # Unchanged
├── context-middleware.ts        # Unchanged
├── secrets.ts                   # Unchanged
├── bootstrap.ts                 # Unchanged
├── bootstrap-test.ts            # Unchanged
│
├── tools/                       # NEW — per-tool-name folders per FR-007
│   ├── README.md                # NEW — file format + brief framework context (FR-006 allows framework content)
│   ├── types.ts                 # NEW — StandaloneExtras type, isStandalone helper, ToolDefinition alias
│   ├── types.test.ts            # NEW — isStandalone helper tests
│   ├── shared/                  # NEW — cross-tool helpers (underscore-free; sibling of tool folders)
│   │   ├── result-blocks.ts     # MOVED from mouse-tool.ts (MouseContentBlock type + buildResultBlocks helper)
│   │   └── result-blocks.test.ts# MOVED+SPLIT from mouse-tool.test.ts (buildResultBlocks assertions)
│   ├── mouse_move/              # NEW — one folder per tool name (LangChain name)
│   │   ├── mouse-move.ts        # MOVED from mouse-tool.ts (createMouseMoveTool + mouseMoveSchema)
│   │   └── mouse-move.test.ts   # MOVED+SPLIT from mouse-tool.test.ts (mouse_move assertions)
│   └── mouse_click/             # NEW
│       ├── mouse-click.ts       # MOVED from mouse-tool.ts (createMouseClickTool + mouseClickSchema + CLICK_TYPES + CLICK_TYPE_TO_PROTO)
│       └── mouse-click.test.ts  # MOVED+SPLIT from mouse-tool.test.ts (mouse_click assertions)
│
├── mcp/                         # NEW — empty except README (FR-001, FR-005)
│   └── README.md                # NEW — file format only, no framework content
│
└── skill/                       # NEW — empty except README (FR-001, FR-003, FR-004, FR-005)
    └── README.md                # NEW — file format only, no framework content; pins SKILL.md contract
```

**Structure Decision**: Single-project layout (Option 1 in template). All TypeScript stays under `projects/game/agent/src/` per Q1 clarification — inherits the existing `src/**/*.ts` BUILD.bazel glob at `projects/game/agent/BUILD.bazel:19-22` with no glob edits required. Tool folders follow strict per-tool-name pattern (`tools/{tool_name}/`) per FR-007; tightly-coupled mouse helpers that are genuinely cross-tool (`MouseContentBlock`, `buildResultBlocks`) live in `tools/shared/` rather than being duplicated. Test files remain side-by-side with their source (repo convention — see research.md §Repo Survey Q1).

**Test-file split note**: the existing `mouse-tool.test.ts` exercises three concerns (mouse_move, mouse_click, buildResultBlocks). After the source split it is relocated-and-split into three test files — one per source file. Assertions and test logic are preserved verbatim; only the file boundary changes. This refines spec Assumption "test files move with their source files" from 1:1 (anticipating a single source file) to 1:N (matching the per-tool-name source split that FR-007 mandates). `llm-tools.test.ts` is unchanged because it tests `AgentAdapterImpl` wiring (in `llm.ts`), not individual tools.

## Complexity Tracking

Not needed — no Constitution Check violations to justify. The `extras`-field pattern is one extra typed property and a one-line reader helper; the `tools/shared/` folder is one extra directory level to avoid code duplication. Neither rises to "complexity requiring justification".
