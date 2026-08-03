# Tasks: Agent Resources Layout

**Input**: Design documents from `specs/020-agent-resources-layout/`

**Prerequisites**: `plan.md` (required), `spec.md` (required for user stories), `research.md`, `data-model.md`, `contracts/{directory-layout,tool-definition,skill-md-format}.md`, `quickstart.md`.

**Tests**: Build (`bazel build //projects/game/agent/...`) + unit tests (`bazel test //projects/game/agent/...`) run inside each implementation phase per Constitution Principle IV — they are NOT separate tasks. The large-test acceptance gate (Constitution Principle VI) is Phase 3.

**Organization**: Phases are ordered by risk — Phase 1 lays down scaffolding (new files only, zero relocation risk); Phase 2 does the behavior-preserving relocation of the mouse tools; Phase 3 is the large-test acceptance gate. User-story coverage:

| User Story | Phase |
|---|---|
| US1 — Discover Agent Resources by Category | Phase 1 |
| US2 — Existing Tools Relocate Without Behavior Change | Phase 2 |
| US3 — Built-in Skills Follow the SKILL.md Convention | Phase 1 |
| US4 — Tools Carry a Standalone Visibility Attribute | Phase 1 (contract + helper) + Phase 2 (apply to existing tools) |

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies between them within the phase).
- **[Story]**: Which user story this task belongs to.

## Path Conventions

All paths are repo-relative. Source root for this feature: `projects/game/agent/src/`. The existing `BUILD.bazel` glob `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])` at `projects/game/agent/BUILD.bazel:19-22` and the `vitest_test` glob at `projects/game/agent/BUILD.bazel:136-139` already sweep the entire `src/` tree, so new files under `src/tools/`, `src/mcp/`, `src/skill/` are auto-discovered with no glob edits — only a `gazelle` regen for hygiene.

---

## Phase 1: Resource directory scaffolding (US1 + US3 + US4 contract/helper)

**Goal**: Establish the three resource directories with their READMEs, plus the typed `standalone` helper that Phase 2 will consume. Zero relocation risk — every file in this phase is brand-new; no existing file is touched.

**Independent Test**: `ls` the three new directories and their READMEs; build + unit-test the package (the new `types.test.ts` must pass in isolation); confirm `mcp/README.md` and `skill/README.md` contain no framework terms per spec FR-005 / SC-005.

### 文档清单 (Required Reading — Constitution Principle V)

**代码规范文档** (仓库内 `style/` 规范 + 其引用的本 phase 需读的外部代码规范):
- `style/javascript.md` — TypeScript 测试约定（mock 模式、`vitest_test` 宏用法、`vi.mock` 禁用规则），用于 T004 编写 `types.test.ts` 时遵守 "Reliable pattern"。
- [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) — `style/javascript.md` 第 5 行声明作为本仓库 TS 规范基准，本 phase 新增的 `types.ts` / `types.test.ts` / 三个 README 须遵守。

**官方文档** (第三方组件/依赖的官方文档或其 GitHub 仓库 README):
- [agentskills.io specification](https://agentskills.io/specification) — SKILL.md 文件格式权威标准，T002 编写 `skill/README.md` 时所需字段约束（`name` 正则、`description` 长度、frontmatter 规则）的原始来源。
- [OpenCode skills docs](https://opencode.ai/docs/skills/) — Dominion 运行时实际识别的 SKILL.md 子集，决定哪些字段可移植、哪些字段会被忽略（T002 README 中"非可移植字段"段落的依据）。
- [LangChain `StructuredToolInterface` source (`extras` field)](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/types.ts) — T003 设计 `StandaloneExtras` / `isStandalone` 时确认 `extras` 字段在 `StructuredToolInterface` 上有静态类型声明，并确认其基础类型为 `Record<string, unknown> | undefined`（决定 `isStandalone` 内部的 cast 写法）。

**技术文章**: 无

### Implementation

- [ ] T001 [P] [US1] Create directory `projects/game/agent/src/mcp/` and author `projects/game/agent/src/mcp/README.md` as a **file-format-only** contract per spec FR-002 + FR-005 + SC-005. Content MUST cover: (a) one folder per MCP integration named `{mcp_name}` (lowercase, kebab-case); (b) the entry-point file naming is reserved ("TBD when first MCP is authored"). Content MUST NOT mention LangChain, `createAgent`, `buildTools`, `AgentProfile`, `PromptService`, `grpc`, `proto`, or `GameService` (spec FR-005). Style model: `tools/templates/README.md` (minimal, 16 lines).
- [ ] T002 [P] [US1][US3] Create directory `projects/game/agent/src/skill/` and author `projects/game/agent/src/skill/README.md` as a **file-format-only** contract per spec FR-002 + FR-003 + FR-004 + FR-005. Content MUST cover: (a) one folder per built-in skill named `{skill_name}` (lowercase, hyphenated, matches the SKILL.md frontmatter `name`); (b) the filename is fixed `SKILL.md` (uppercase — do not use `skill.md`, `Skill.md`, or `{skill_name}.md`); (c) frontmatter field contract — required `name` (1–64 chars, regex `^[a-z0-9]+(-[a-z0-9]+)*$`, MUST equal folder name) and `description` (1–1024 chars); recommended-optional `license`, `compatibility`, `metadata`; (d) non-portable fields (`allowed-tools`, all Claude Code extension fields) MUST be documented as non-portable and MAY be omitted; (e) body is free-form markdown, recommended sections (purpose, when-to-use, how-to-use, examples, edge cases), soft limit <500 lines; (f) pointer to the authoritative contract at `specs/020-agent-resources-layout/contracts/skill-md-format.md` and the upstream standard at https://agentskills.io/specification. Content MUST NOT mention LangChain, `createAgent`, `buildTools`, `AgentProfile`, `PromptService`, `grpc`, `proto`, or `GameService` (spec FR-005). User-created skills (`Skill` proto resource at `projects/game/game.proto:438-454`) are out of scope (FR-013) — the README MUST explicitly state it covers built-in skills only.
- [ ] T003 [P] [US4] Create `projects/game/agent/src/tools/types.ts` exporting:
  - `StandaloneExtras` interface (shape `{ standalone: boolean }` — the per-tool `extras` payload every tool MUST set).
  - `isStandalone(tool: StructuredToolInterface): boolean` helper — returns `false` ONLY when `tool.extras?.standalone === false`; returns `true` for `standalone === true`, `standalone === undefined`, `extras === undefined`, or any other shape (the default-true behavior, per spec Assumption "default value of `standalone`" and `contracts/tool-definition.md` lines 88–96).
  - Import `StructuredToolInterface` from `@langchain/core/tools` (type-only import). Read `tool.extras` as `Partial<StandaloneExtras> | undefined` — the cast is required because `extras` is typed `Record<string, unknown> | undefined` upstream (source: https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/types.ts).
  - Verbatim pattern in `specs/020-agent-resources-layout/research.md` §2 lines 72–92 and `contracts/tool-definition.md` lines 78–97.
- [ ] T004 [P] [US4] Create `projects/game/agent/src/tools/types.test.ts` with a `describe("isStandalone", ...)` block covering the four contract cases at `contracts/tool-definition.md` lines 99–102:
  1. tool with `extras.standalone === true` → returns `true`.
  2. tool with `extras.standalone === false` → returns `false`.
  3. tool with no `extras` set → returns `true` (default-when-omitted).
  4. tool with `extras` present but `standalone` key absent → returns `true`.
  Build the mock tool via a small `makeTool(extras)` helper that casts `{ extras }` to `StructuredToolInterface` (same `as unknown as StructuredToolInterface` pattern used at `projects/game/agent/src/mouse-tool.test.ts:33-40`). No `vi.mock` — pure value-in/value-out (style/javascript.md §Mock 约定 "Reliable pattern").
- [ ] T005 [P] [US1] Create directory `projects/game/agent/src/tools/` (if not already created by T003/T004) and author `projects/game/agent/src/tools/README.md`. Per spec FR-006 and research.md §3 Q7, the tools README is **slightly richer** than the `mcp/` and `skill/` READMEs (FR-006 allows framework content for `tools/`). Content MUST cover:
  - **File-format contract (lead with this)**: one folder per tool, named EXACTLY the LangChain `name` field verbatim (e.g. tool name `mouse_move` → folder `src/tools/mouse_move/`). This deliberate underscore-in-folder-name divergence from kebab-case exists so the folder path === the value users put in a profile's `tool_names` array.
  - Files inside `{tool_name}/`: `<kebab-case-name>.ts` exporting `create{ToolName}Tool(bridge: OperationBridge): StructuredToolInterface` (kebab-case per repo convention), plus side-by-side `<kebab-case-name>.test.ts`.
  - `src/tools/shared/` for cross-tool helpers (referenced by 2+ tool folders). Files are kebab-case.
  - `src/tools/types.ts` for the `StandaloneExtras` type + `isStandalone` helper.
  - **Brief framework context (allowed by FR-006)**: how `buildTools` at `projects/game/agent/src/llm.ts:57` consumes a new tool (the if/else if dispatch on string names). The README MUST instruct a contributor adding a new tool to: (1) create `src/tools/{tool_name}/`, (2) author the factory using the `tool()` factory from `langchain`, (3) set `extras: { standalone: <bool> } satisfies StandaloneExtras` in the options bag, (4) add a side-by-side test, (5) wire into `buildTools` if/else if, (6) run `bazel run //:gazelle projects/game/agent` to regenerate `BUILD.bazel`.
  - The `standalone` attribute contract (cite `contracts/tool-definition.md`): `true` = individually selectable on desktop profile-config page; `false` = hidden (intended for collection-only use); default when omitted is `true` but contributors MUST set it explicitly.

### Verification gate (Constitution Principle IV)

- [ ] T006 Run `bazel build //projects/game/agent/...` — MUST succeed (TS compiles; `types.ts` is auto-included by the existing `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])` at `projects/game/agent/BUILD.bazel:19-22`).
- [ ] T007 Run `bazel test //projects/game/agent/...` — MUST succeed; the new `types.test.ts` is auto-discovered by the `vitest_test` glob at `projects/game/agent/BUILD.bazel:136-139`. Existing tests stay green (no existing file was touched).
- [ ] T008 Verify spec FR-005 / SC-005 mechanically: `rg -i 'langchain|createAgent|buildTools|AgentProfile|PromptService|grpc|proto|GameService' projects/game/agent/src/mcp/README.md projects/game/agent/src/skill/README.md` MUST produce zero matches.

**Checkpoint**: The three resource directories exist with conforming READMEs; the `standalone` helper and its type are in place and unit-tested; no behavior has changed (existing mouse tools still compile and pass at their pre-refactor locations). Ready for the riskier relocation in Phase 2.

---

## Phase 2: Relocate mouse tools and apply `standalone` (US2 + US4 application)

**Goal**: Move `mouse-tool.ts` and `mouse-tool.test.ts` from the flat `src/` root into the new `src/tools/` layout per the per-tool-name convention, split them by concern, attach `extras.standalone = true` to both tools per FR-011, and update `buildTools`'s import paths. All existing test assertions MUST pass with their logic unchanged (spec Assumption / SC-002).

**Independent Test**: `test ! -e` on the old files; `ls` the new files; `rg` for the updated import paths in `llm.ts`; `rg 'standalone:' projects/game/agent/src/tools/` returns ≥2 matches both setting `true`; `bazel build` + `bazel test` on the package pass identically to the pre-refactor baseline.

### 文档清单 (Required Reading — Constitution Principle V)

**代码规范文档** (仓库内 `style/` 规范 + 其引用的本 phase 需读的外部代码规范):
- `style/javascript.md` — TypeScript 测试约定（mock "Reliable pattern"、`vi.fn()` test-double seam）；T010 / T012 / T014 编写或迁移测试时遵守——禁止使用 `vi.mock` 拦截跨包外部依赖，使用依赖注入或直接构造对象字面量。
- [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) — `style/javascript.md` 第 5 行声明作为本仓库 TS 规范基准；本 phase 拆分、迁移、新增的 `.ts` 文件须遵守。

**官方文档** (第三方组件/依赖的官方文档或其 GitHub 仓库 README):
- [LangChain `tool()` factory and `DynamicStructuredTool` source](https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-core/src/tools/index.ts) — 确认 `tool()` 工厂返回的 `DynamicStructuredTool` 在构造时把 `extras` 作为真实可枚举属性存储，因此穿过 `createAgent({ tools })` 的往返后 `extras.standalone` 仍然存在、不被剥离（T011 / T013 在 options bag 设置 `extras: { standalone: true }` 的可行性依据；也是 `research.md` §2 决策的第 2 条依据）。

**技术文章**: 无

### Implementation — create new files (parallel; non-overlapping paths)

- [ ] T009 [P] [US2] Create `projects/game/agent/src/tools/shared/result-blocks.ts` with:
  - Exported type `MouseContentBlock` (the discriminated union `{ type: "text"; text: string } | { type: "image_url"; image_url: { url: string } }` — moved verbatim from `projects/game/agent/src/mouse-tool.ts:63-65`).
  - Exported function `buildResultBlocks(result: OperationResult): MouseContentBlock[]` (moved verbatim from `projects/game/agent/src/mouse-tool.ts:149-166`).
  - Import: `import type { OperationResult } from "../../operation-bridge";` (two levels up from `src/tools/shared/` to `src/`).
- [ ] T010 [P] [US2] Create `projects/game/agent/src/tools/shared/result-blocks.test.ts` — NEW direct unit tests for the now-exported `buildResultBlocks` helper (it was previously file-private and only tested indirectly through the tools). Cover the three contract behaviors observed at `projects/game/agent/src/mouse-tool.test.ts:85-142`:
  1. `OperationResult` with no `screenshot` → returns `[ { type: "text", text: <message> } ]`.
  2. `OperationResult` with `screenshot: { data, widthPx, heightPx }` → returns `[text, image_url, text]` and the third block's text matches the template `[图片像素尺寸：{w}×{h}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]` verbatim.
  3. `OperationResult` with `status: TOOL_RESULT_STATUS_FAILED` and a message → still returns a single text block carrying the failure message (buildResultBlocks does not branch on status).
  Import `buildResultBlocks` from `./result-blocks`. Construct the `OperationResult` input as a plain object literal typed `OperationResult` (import the type from `../../operation-bridge`). No `vi.mock`, no tool invocation — pure function tests (style/javascript.md §Mock 约定 "Reliable pattern").
- [ ] T011 [P] [US2][US4] Create `projects/game/agent/src/tools/mouse_move/mouse-move.ts` containing the `mouse_move` tool:
  - Move `mouseMoveSchema` (verbatim from `projects/game/agent/src/mouse-tool.ts:27-30`) and `createMouseMoveTool(bridge: OperationBridge): StructuredToolInterface` (verbatim from `projects/game/agent/src/mouse-tool.ts:78-100`).
  - Add `extras: { standalone: true } satisfies StandaloneExtras` to the `tool()` options bag (FR-011 — preserve today's desktop-display behavior).
  - Imports: `import { tool } from "langchain";`, `import { z } from "zod";`, `import type { StructuredToolInterface } from "@langchain/core/tools";`, `import type { OperationBridge } from "../../operation-bridge";` (two levels up), `import type { Part } from "../../../game_types/projects/game/Part";` (three levels up — same `game_types` resolution as today's `"../game_types/projects/game/Part"` at `projects/game/agent/src/mouse-tool.ts:23`, just one extra `../` per new directory level), `import type { MouseContentBlock } from "../shared/result-blocks";` and `import { buildResultBlocks } from "../shared/result-blocks";` (sibling directory), `import type { StandaloneExtras } from "../types";`.
  - The async tool body (`bridge.dispatch(part, signal)` + `return buildResultBlocks(result)`) stays verbatim — only the import paths and the new `extras` field differ from the pre-refactor source.
- [ ] T012 [P] [US2] Create `projects/game/agent/src/tools/mouse_move/mouse-move.test.ts` by **relocating verbatim** the `describe("createMouseMoveTool", ...)` block (lines 64–201) AND the `describe("mouse tool abort signal", ...)` block (lines 347–390, which uses `createMouseMoveTool`) from `projects/game/agent/src/mouse-tool.test.ts`. The third describe block is a `it.skip(...)` for a known langchain framework limitation — preserve the `skip` and its explanatory comment verbatim. Adjust only the imports at the top of the file:
  - `import { createMouseMoveTool } from "./mouse-move";` (was `from "./mouse-tool"`).
  - `import { OperationBridge } from "../../operation-bridge";` (was `from "./operation-bridge"`).
  - `import type { Part } from "../../../game_types/projects/game/Part";` (was `from "../game_types/projects/game/Part"`).
  - Assertions, `it(...)` titles, mock setup, and the skipped test's comment MUST NOT change (spec Assumption / SC-002).
- [ ] T013 [P] [US2][US4] Create `projects/game/agent/src/tools/mouse_click/mouse-click.ts` containing the `mouse_click` tool:
  - Move `CLICK_TYPES` const array (verbatim from `projects/game/agent/src/mouse-tool.ts:34-40`), `CLICK_TYPE_TO_PROTO` const map (verbatim from `projects/game/agent/src/mouse-tool.ts:42-48`), `mouseClickSchema` (verbatim from lines 50-54), and `createMouseClickTool(bridge: OperationBridge): StructuredToolInterface` (verbatim from lines 113-138).
  - Add `extras: { standalone: true } satisfies StandaloneExtras` to the `tool()` options bag (FR-011).
  - Imports: same pattern as T011, with `MouseContentBlock` / `buildResultBlocks` from `"../shared/result-blocks"`, `OperationBridge` from `"../../operation-bridge"`, `Part` from `"../../../game_types/projects/game/Part"`, `StandaloneExtras` from `"../types"`.
- [ ] T014 [P] [US2] Create `projects/game/agent/src/tools/mouse_click/mouse-click.test.ts` by **relocating verbatim** the `describe("createMouseClickTool", ...)` block (lines 205–343) from `projects/game/agent/src/mouse-tool.test.ts`. Adjust only the imports at the top of the file:
  - `import { createMouseClickTool } from "./mouse-click";` (was `from "./mouse-tool"`).
  - `import { OperationBridge } from "../../operation-bridge";` (was `from "./operation-bridge"`).
  - `import type { Part } from "../../../game_types/projects/game/Part";` (was `from "../game_types/projects/game/Part"`).
  - Assertions, `it(...)` titles, mock setup, and the `it.each(...)` click-type→proto parameterization MUST NOT change (spec Assumption / SC-002).

### Implementation — wire up and tear down (sequential; depends on T011 + T013)

- [ ] T015 [US2] Update `projects/game/agent/src/llm.ts` line 17 from `import { createMouseClickTool, createMouseMoveTool } from "./mouse-tool";` to:
  ```typescript
  import { createMouseClickTool } from "./tools/mouse_click/mouse-click";
  import { createMouseMoveTool } from "./tools/mouse_move/mouse-move";
  ```
  The `buildTools` body at lines 57-70 stays UNCHANGED — the if/else if dispatch on string tool names is preserved per spec edge case "What happens to indirect imports?" and `contracts/directory-layout.md` "What this feature does NOT change" line 123. No registry, no map-based refactor (out of scope per data-model.md §Out of Scope).
- [ ] T016 [US2] Delete `projects/game/agent/src/mouse-tool.ts` and `projects/game/agent/src/mouse-tool.test.ts` — every symbol and every test block has a new home in T009–T014. Confirm with `test ! -e projects/game/agent/src/mouse-tool.ts && test ! -e projects/game/agent/src/mouse-tool.test.ts`.
- [ ] T017 [US2] Regenerate `BUILD.bazel` for hygiene (per `AGENTS.md` and plan.md line 34) — run `bazel run //:gazelle projects/game/agent`. The existing `glob(["src/**/*.ts"], exclude = ["**/*.test.ts"])` and `vitest_test` data glob already cover the new and deleted files at load time, so this step is for keeping generated entries in sync with the file tree; no manual `BUILD.bazel` edit is expected.

### Verification gate (Constitution Principle IV; SC-002 behavior preservation)

- [ ] T018 Run `bazel build //projects/game/agent/...` — MUST succeed.
- [ ] T019 Run `bazel test //projects/game/agent/...` — MUST succeed with test logic unchanged from pre-refactor baseline. Specifically `//projects/game/agent:lib_test` covers the relocated `mouse-move.test.ts`, `mouse-click.test.ts`, the new `result-blocks.test.ts`, the Phase-1 `types.test.ts`, and the unchanged `llm-tools.test.ts` / `llm.test.ts` / `handler.test.ts` / etc.
- [ ] T020 Verify file-shape mechanically:
  ```bash
  test ! -e projects/game/agent/src/mouse-tool.ts
  test ! -e projects/game/agent/src/mouse-tool.test.ts
  rg 'from "./mouse-tool"' projects/game/agent/src/llm.ts           # zero matches
  rg 'from "./tools/mouse_move/mouse-move"' projects/game/agent/src/llm.ts   # one match
  rg 'from "./tools/mouse_click/mouse-click"' projects/game/agent/src/llm.ts # one match
  rg 'standalone:' projects/game/agent/src/tools/                   # ≥2 matches, all 'true'
  ```

**Checkpoint**: Existing mouse tools (`mouse_move`, `mouse_click`) now live under `src/tools/{tool_name}/` per FR-007, both carry `extras.standalone = true` per FR-011, and every pre-refactor unit test passes with its assertions verbatim per SC-002. The contract is fully wired; the only remaining gate is the end-to-end large test.

---

## Phase 3: Large-test acceptance gate (Constitution Principle VI; spec US2 acceptance #3)

**Goal**: Prove the relocation is end-to-end behavior-preserving by running the existing mouse-dispatch testplan, which exercises the full stack (desktop ↔ gateway ↔ agent ↔ LLM) through the tool dispatch path that this feature touched.

**Independent Test**: All testplan cases pass with no regression in profile binding, frame exchange, mouse dispatch, or ToolResult handling versus the pre-refactor baseline.

### 文档清单 (Required Reading — Constitution Principle V)

**代码规范文档** (仓库内 `style/` 规范 + 其引用的本 phase 需读的外部代码规范):
- `style/large_test.md` — 大型测试执行规范，明确"测试计划数量"规则（每个被测系统只维护一份测试计划 YAML；新功能验证作为新 `suite` 或 `case` 加入既有计划，**不要**为单个需求新建独立测试计划——T021 必须复用 `projects/game/testplan/` 既有计划），以及"反模式"清单（禁止按交付物维度组织测试、禁止平行测试计划）。

**官方文档**: 无

**技术文章**: 无

### Implementation

- [ ] T021 [US2] Load the `testplan` skill (`load skill testplan`) and execute the existing mouse-dispatch testplan at `projects/game/testplan/`. Per `style/large_test.md` "测试计划数量" the existing system testplan MUST be reused — DO NOT create a new YAML for this feature (spec FR-008 acceptance, Constitution Principle VI). Follow the skill's documented flow: install `guitar`/`deploy` if needed → `guitar validate` → `guitar run <plan.yaml>`. The testplan covers mouse dispatch through `buildTools` → `createMouseMoveTool` / `createMouseClickTool` → `OperationBridge.dispatch` → desktop round-trip → `buildResultBlocks` rendering — the entire chain the relocation touched.
  - Prerequisite reading BEFORE running: `style/large_test.md` (per AGENTS.md and Constitution Principle V).
  - If a case fails: load the `signoz` skill, query the failing service's logs/traces (provide the `trace_id` from the test output), localize, fix, and re-run. Up to 3 retries per the testplan skill's documented retry policy.
  - Report structure: testplan path → validate result → deploy result → per-case result → cleanup result → (if failed) failure stage and signoz query summary.

### Verification gate (Constitution Principle VI)

- [ ] T022 Confirm all testplan cases pass; mouse dispatch and ToolResult handling behave identically to the pre-refactor baseline.

**Checkpoint**: Feature complete — all seven scenarios in `quickstart.md` (the four file-shape checks Scenarios 1–5, the small-test gate Scenario 6, and this large-test gate Scenario 7) now pass.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1**: No dependencies — can start immediately. Creates only new files; touches no existing source.
- **Phase 2**: Depends on Phase 1 completion (specifically T003 `types.ts` must exist so T011/T013 can `import type { StandaloneExtras } from "../types";`, and the `tools/` directory must exist with its README). Phase 2 performs the risky relocation.
- **Phase 3**: Depends on Phase 2 completion (the relocated tools must be the ones under test). Large-test only — does not modify code.

### Within Phase 1

- T001–T005 are all `[P]` — non-overlapping file paths, no cross-deps. Can run in parallel.
- T006–T008 (verification) run after T001–T005 complete.

### Within Phase 2

- T009–T014 are all `[P]` — non-overlapping file paths. Can run in parallel.
- T015 (update `llm.ts` imports) depends on T011 and T013 (the new tool files must exist for the import paths to resolve).
- T016 (delete old files) depends on T015 (the `llm.ts` import must point to the new location before the old `mouse-tool.ts` is removed, otherwise the build breaks mid-refactor).
- T017 (gazelle regen) depends on T016.
- T018–T020 (verification) run after T017 completes.

### Within Phase 3

- T021 (run testplan) is the only implementation step.
- T022 is the verification gate.

### Parallel Opportunities

- All Phase 1 tasks (T001–T005) can run in parallel.
- All Phase 2 new-file tasks (T009–T014) can run in parallel.

---

## Notes

- Per `contracts/directory-layout.md` lines 119–124, the flat top-level files in `src/` (`llm.ts`, `handler.ts`, `server.ts`, `prompt-client.ts`, `session-agent.ts`, `operation-bridge.ts`, `model-provider.ts`, `resolver-provider.ts`, `context-middleware.ts`, `secrets.ts`, `bootstrap.ts`, `bootstrap-test.ts`) and `src/llm-tools.test.ts` stay where they are. Only `mouse-tool.ts` + `mouse-tool.test.ts` move, and only `llm.ts` line 17 (the import) is edited.
- Per `data-model.md` §Out of Scope, the `buildTools` if/else if dispatch is preserved — no registry, no map refactor. Only the two import paths change.
- Per `spec.md` FR-013, user-created skills (`Skill` proto at `projects/game/game.proto:438-454`, `PromptService` RPCs at `projects/game/game.proto:140-167`) are out of scope. The `skill/README.md` MUST explicitly state this scope boundary.
- Per `spec.md` Assumption, the desktop consumer of the `standalone` attribute is a separate follow-up feature — this feature only establishes the agent-side contract. No desktop change, no new RPC.
- The `BUILD.bazel` file at `projects/game/agent/BUILD.bazel` is NOT hand-edited in this feature — the existing `glob(["src/**/*.ts"], ...)` patterns cover all new and deleted files. T017 only runs `gazelle` for hygiene.
