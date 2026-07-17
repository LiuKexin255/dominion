---

description: "Task list for feature 018-saolei-mcp implementation"
---

# Tasks: Saolei (Minesweeper) MCP, Agent Capability Reorganization & Profile MCP/Skill Selection

**Input**: Design documents from `/specs/018-saolei-mcp/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, quickstart.md

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story. US1 and US2 are co-equal P1 stories that share the MCP core (board state machine + tool factories) and are implemented together in one phase (Phase 3); each task carries a `[US1]`/`[US2]` label so both stories' acceptance criteria stay traceable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## Constitution Check

*GATE: Must pass before implementation begins.*

- **Citation Provenance (§I)**: every task that references an external library, tool, command, pattern, or inherited design decision MUST include an inline `[description](URL)` link or explicitly cite the parent `plan.md`/`contracts/` source. A matching entry MUST appear in the `## References` section at the end of this file. Decisions D-1..D-6 and the proto/contract shapes are inherited from [plan.md](./plan.md) and [contracts/](./contracts/) and are referenced rather than restated.
- **External Dependency Research (§II)**: this feature introduces NO new runtime dependency (D-1 plain LangChain tools, D-2 system-prompt skill assembly — see [research.md](./research.md)). No task adds a new dependency; any task that would MUST perform the same documentation research and cite findings before implementation.
- **Refactoring-Oriented Changes (§III)**: tasks touching existing units (`llm.ts` `buildTools`/`AdapterFactory`/`AgentAdapterImpl`, `session-agent.ts` `ProfileData`, `prompt-client.ts`, `operation-bridge.ts` `dispatch`, `game.proto` mouse parts/`Part.kind`, `app.go` create mapping, `view_model.go`, `ProfileManagement.svelte`, `api.ts`, mouse-tool relocation) are refactors of those units — natural extensions of still-coherent designs — with existing-design verdicts recorded in [plan.md](./plan.md) Changes. "Out of scope" MUST NOT carry an outdated design forward.
- **Interface Design Coverage (§IV)**: tasks touching externally callable boundaries inherit their interface design from [plan.md](./plan.md) and reference [contracts/input-delivery.md](./contracts/input-delivery.md) (proto `InputDelivery`/`KeyPart`/`PartBlock`) and [contracts/profile-api.md](./contracts/profile-api.md) (PromptService profile create/update) rather than restating shapes. The implemented interface MUST comply with [style/api.md](../../style/api.md).
- **Documentation First (§V)**: each phase section below carries its own Required Reading declaration (规范文档 / 官方文档 / 技术文章). Implementation of any task within a phase MUST NOT start before that phase's declaration is read. There is no single global declaration and no cross-phase inheritance — a document relevant to several phases is repeated in each.
- **Test Verification Granularity (§VI)**: every code-changing task MUST materialize the per-change ladder inherited from [plan.md](./plan.md) — build is the first gate; after a successful build the colocated unit test covering the changed unit MUST pass. Verification proceeds build → unit tests → large tests. Unit tests accompany the code change that exercises them (there are NO separate unit-test tasks); the large test (T026) is the feature-level checkpoint, not a per-change gate. The concrete build/test tooling is Bazel per [AGENTS.md](../../AGENTS.md).

**Per-change verification (applies to EVERY code-changing task)**: write/update the colocated unit test (`*.test.ts` for the agent service, `*_test.go` for the desktop) for the unit changed by the task, then run `bazel build <affected packages>` followed by `bazel test <affected packages>`. The task is not complete until both pass.

## Path Conventions

This feature spans multiple languages in one repo (not a single `src/` project):

- **Agent service (TypeScript)**: `projects/game/agent/src/` — tests colocated as `*.test.ts`.
- **Desktop (Go / Wails)**: `projects/game/desktop/` — tests colocated as `*_test.go`; input executors under `projects/game/desktop/internal/operation/`.
- **Frontend (Svelte 5 + TS)**: `projects/game/desktop/frontend/src/`.
- **Proto**: `projects/game/game.proto`.
- **Large tests**: `testplan/` (per `style/large_test.md`).

All build/test entry points go through Bazel; BUILD files are regenerated with `bazel run //:gazelle` (per `AGENTS.md`).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish a known-green starting point before feature work.

Required Reading:
- 规范文档: `style/README.md` (style index)
- 官方文档: None
- 技术文章: None

- [X] T001 Establish green baseline by running `bazel build //projects/game/agent/... //projects/game/desktop/...` and `bazel test //projects/game/agent/... //projects/game/desktop/...` from repo root; record any pre-existing failures before changes begin

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared proto interface contract that US1's input-delivery path and the desktop executor both depend on. MUST complete before Phase 3.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

Required Reading:
- 规范文档: `style/api.md` (proto/interface conventions) + the AIPs it cites — [AIP-131](https://google.aip.dev/131), [AIP-132](https://google.aip.dev/132), [AIP-133](https://google.aip.dev/133), [AIP-134](https://google.aip.dev/134), [AIP-156](https://google.aip.dev/156)
- 官方文档: None
- 技术文章: None

- [X] T002 Add `InputDelivery` enum (`INPUT_DELIVERY_UNSPECIFIED`/`SIMULATE`/`WINDOW_MESSAGE`) and `InputDelivery delivery` field on `MouseMovePart` (field 4) and `MouseClickPart` (field 3) in `projects/game/game.proto` per [contracts/input-delivery.md](./contracts/input-delivery.md) §1 and [data-model.md](./data-model.md) §5a (default `SIMULATE` → backward compatible)
- [X] T003 Add `KeyAction` enum (`KEY_ACTION_UNSPECIFIED`/`KEY_ACTION_F2`) and `KeyPart` message (`tool_id`,`key`) plus the `key_press` member (field 7) in the `Part.kind` oneof in `projects/game/game.proto` per [contracts/input-delivery.md](./contracts/input-delivery.md) §2 and [data-model.md](./data-model.md) §5b
- [X] T004 Regenerate Go and TypeScript proto bindings and update BUILD files via `bazel run //:gazelle`, then `bazel build //projects/game/...` to confirm both bindings compile against the new `InputDelivery`/`KeyPart`

**Checkpoint**: proto contract landed and compiles — Phase 3 may begin.

---

## Phase 3: User Story 1 + 2 — Saolei MCP Core (Priority: P1) 🎯 MVP

**Goal**: An agent plays Minesweeper accurately through the saolei MCP (US1) while the MCP maintains full board state and enforces the operate→update protocol with legality validation (US2). The agent service is the in-process MCP server (D-1); input is delivered as window messages (no cursor occlusion); rejections are structured success, not thrown errors (D-5).

**Independent Test**: Construct an adapter for a profile with `mcp_names: ["saolei"]`, bind a (stub) window, and drive `saolei_init(9,9)` → `saolei_click(4,4)` → `saolei_update(...)`. Assert: the cell-centre coordinate is computed from `TOP_OFFSET=200`/`LEFT_OFFSET=24`/`BLOCK_LENGTH=32` (US1); the dispatch is a `PartBlock` of window-message parts with no physical cursor move (US1); the lifecycle goes `ready → awaiting-update → ready` (US2); and a second operation before `saolei_update`, an out-of-bounds coordinate, a click on a non-`block` cell, and a chord on a non-number cell are all returned as `status:"rejected"` with no window input (US2).

### Implementation for User Story 1 + 2

Required Reading:
- 规范文档: `style/javascript.md` (TS conventions) + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html); `style/golang.md` (desktop Go executor conventions — execute_v2/windows); `style/api.md` + the AIPs it cites — [AIP-131](https://google.aip.dev/131), [AIP-132](https://google.aip.dev/132), [AIP-133](https://google.aip.dev/133), [AIP-134](https://google.aip.dev/134), [AIP-156](https://google.aip.dev/156) (inherited proto `Part`/`PartBlock` contract); `style/README.md` (style index)
- 官方文档: [LangChain.js — agents](https://docs.langchain.com/oss/javascript/langchain/agents) + [overview](https://docs.langchain.com/oss/javascript/langchain/overview) (`createAgent`/`tool()` + zod, D-1; catalog pins `langchain ^1.5.0`, `@langchain/core ^1.2.0`, `@langchain/langgraph ^1.4.4`); [Model Context Protocol — architecture](https://modelcontextprotocol.io/docs/concepts/architecture) (agent service as MCP server, D-1); [Wails v2](https://wails.io/) (Win32 `PostMessage` access for window-message delivery)
- 技术文章: [Minesweeper keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — F2 "new game" convention used by `saolei_init`

- [X] T005 [P] [US2] Create the cell-state enumeration and board state machine in `projects/game/agent/src/mcp/saolei/board.ts` — `CellState` (`block`,`zero`..`eight`,`flag`,`boom`), `BoardState { width, height, grid, lifecycle }`, lifecycle `uninitialized`/`ready`/`awaiting-update`/`terminal`, and the transitions per [data-model.md](./data-model.md) §1-2 (init→all-`block`+ready; click/flag/double_click→awaiting-update; update→ready or terminal on `boom`; no auto-timeout per FR-011a)
- [X] T006 [P] [US1] Create coordinate computation in `projects/game/agent/src/mcp/saolei/geometry.ts` — module constants `TOP_OFFSET=200`, `LEFT_OFFSET=24`, `BLOCK_LENGTH=32` (px), `cellCentre(x,y)` → window-client-relative pixel, and an in-bounds check `0<=x<width && 0<=y<height` per [data-model.md](./data-model.md) §4 (D-6, FR-013/015/021)
- [X] T007 [US2] Create validation rules in `projects/game/agent/src/mcp/saolei/validation.ts` covering FR-016..023 (pre-init, awaiting-update, click non-`block`, chord non-number, only-flag-produces-flag, out-of-bounds, illegal `saolei_update` transition, terminal-after-boom) returning machine-readable reasons per [data-model.md](./data-model.md) §8 (depends on T005)
- [X] T008 [US1] Honor `InputDelivery` in the desktop move/click executor in `projects/game/desktop/internal/operation/execute_v2.go` and `execute_v2_logic.go` — `SIMULATE` keeps the existing physical-cursor path; `WINDOW_MESSAGE` defers to the message-based path (depends on T002; per [contracts/input-delivery.md](./contracts/input-delivery.md) §4)
- [X] T009 [US1] Add the `WINDOW_MESSAGE` Win32 `PostMessage` path (no `SetCursorPos`/`SendInput`) and `KeyPart` `WM_KEYDOWN`/`WM_KEYUP` handling in `projects/game/desktop/internal/operation/execute_windows.go` per [contracts/input-delivery.md](./contracts/input-delivery.md) §4 (depends on T002, T003; SC-003 occlusion-free)
- [X] T010 [US1] Process a multi-part `PartBlock` (move+click combo as one operation) and dispatch `KeyPart` in `projects/game/desktop/app.go` `executeAgentOperation`, reading a `WINDOW_MESSAGE` click's coordinate from the companion `MouseMovePart` in the same block (depends on T008, T009; per [data-model.md](./data-model.md) §5c/§5d)
- [X] T011 [US1] Generalize `OperationBridge.dispatch` in `projects/game/agent/src/operation-bridge.ts` from a single `Part` to a `PartBlock` (one or more parts) awaiting one `ToolResultPart`, correlating by `tool_id` + 5s timeout; the single-part path becomes the one-element block (refactor per [plan.md](./plan.md) Changes; depends on T002)
- [X] T012 [US1, US2] Create the per-session `SaoleiMcp` instance in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` owning the `BoardState`, created lazily when the profile declares the `saolei` mcp, isolated per session, and discarded on adapter rebuild (FR-025a/b/c; depends on T005, T006, T007)
- [X] T013 [US1, US2] Create the five saolei LangChain tool factories in `projects/game/agent/src/mcp/saolei/saolei-tools.ts` — `saolei_init`/`saolei_click`/`saolei_flag`/`saolei_double_click`/`saolei_update` built with the `tool()` + `zod` pattern (D-1), dispatching the `PartBlock`s in [data-model.md](./data-model.md) §5d, enforcing validation (T007), and returning structured `SaoleiToolResult { status:"ok"|"rejected", reason?, board? }` (D-5, FR-024a) per [contracts/saolei-mcp-tools.md](./contracts/saolei-mcp-tools.md) (depends on T011, T012)
- [X] T014 [US1] Extend `ProfileData` with `mcpNames` in `projects/game/agent/src/session-agent.ts` and refactor `buildTools` in `projects/game/agent/src/llm.ts` from the `if/else if` chain into a name→factory registry that also resolves `mcpNames` → the saolei tool set; extend the `AdapterFactory`/`AgentAdapterImpl` signatures and update all call sites (`projects/game/agent/src/server.ts`, `projects/game/agent/src/bootstrap-test.ts`) (refactor verdict in [plan.md](./plan.md) Changes; depends on T013)
- [X] T015 [US1] Have `SessionAgent` own the per-session `SaoleiMcp` instance and pass `mcpNames` to the adapter factory in `projects/game/agent/src/session-agent.ts` (depends on T012, T014)

**Checkpoint**: US1 + US2 fully functional — a profile declaring `mcp_names: ["saolei"]` can play a full, validated turn through the MCP.

---

## Phase 4: User Story 4 — Companion Saolei Skill (Priority: P2)

**Goal**: A companion skill document teaches the agent the MCP usage protocol and is injected into the system prompt at agent creation (D-2).

**Independent Test**: Build an agent for a profile with `skill_names: ["saolei"]` and assert the saolei skill body is present in the effective system prompt; read the skill document and confirm it documents all five tools, the `init → operate → update → repeat` protocol, the cell-state enumeration, the legality rules, and an example play flow (FR-026).

### Implementation for User Story 4

Required Reading:
- 规范文档: `style/javascript.md` (TS conventions) + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html); `style/README.md` (style index)
- 官方文档: [Agent Skills specification](https://agentskills.io/specification) (`SKILL.md` format, D-2); [LangChain.js — skills](https://docs.langchain.com/oss/javascript/langchain/multi-agent/skills) + [agents](https://docs.langchain.com/oss/javascript/langchain/agents) (skill/middleware + system-prompt pattern)
- 技术文章: [deepagents `SkillsMiddleware`](https://github.com/langchain-ai/deepagents/blob/46e10640caf78a84f9715cb8807882ea1b825d6a/libs/deepagents/deepagents/middleware/skills.py) (commit `46e10640c`) — skill-injection-by-system-prompt reference (D-2)

- [ ] T016 [P] [US4] Author the companion skill document in `projects/game/agent/src/skill/saolei/saolei.skill.md` — SKILL.md format (YAML frontmatter `name`/`description` + markdown body) covering the five tools, the mandatory update-after-operation protocol, the cell-state enumeration, the legality rules, and at least one example play flow (FR-026)
- [ ] T017 [US4] Fetch skill contents for the profile's `skill_names` via the existing `GetSkill` RPC in `projects/game/agent/src/prompt-client.ts`, and extend `ProfileResult`/`ProfileData` with `skillNames` and `skillContents` (refactor verdict in [plan.md](./plan.md) Changes)
- [ ] T018 [US4] Assemble resolved `skillContents` into the system prompt in `AgentAdapterImpl` (`effectiveSystemPrompt = systemPrompt + SEPARATOR + skillContents`) in `projects/game/agent/src/llm.ts`, extend the `AdapterFactory`/`AgentAdapterImpl` signatures with `skillNames`/`skillContents`, and update call sites (`server.ts`, `bootstrap-test.ts`, `session-agent.ts`) (D-2; depends on T017)

**Checkpoint**: US4 complete — the saolei skill is selectable and injected at agent creation.

---

## Phase 5: User Story 3 — Profile MCP & Skill Selection (Priority: P2)

**Goal**: An operator can select MCPs and skills when editing an agent profile in the desktop UI, and the selections persist end-to-end (FR-031..035), closing the dormant create-path gap (FR-033).

**Independent Test**: Create a profile toggling the saolei MCP + skill chips, list it back, edit the selections off and on, and assert each change round-trips through create/edit/list with the expected `mcp_names`/`skill_names` (SC-005, <30s).

### Implementation for User Story 3

Required Reading:
- 规范文档: `style/api.md` + the AIPs it cites — [AIP-131](https://google.aip.dev/131), [AIP-132](https://google.aip.dev/132), [AIP-133](https://google.aip.dev/133), [AIP-134](https://google.aip.dev/134), [AIP-156](https://google.aip.dev/156) (profile create/update AIP-compliant nested shape); `style/golang.md` (view_model.go/app.go); `style/javascript.md` (frontend TS/Svelte); `style/README.md` (style index)
- 官方文档: [Wails v2](https://wails.io/) (Wails view models + binding)
- 技术文章: None

- [ ] T019 [US3] Add `SkillNames`/`McpNames` to `CreateAgentProfileView` in `projects/game/desktop/view_model.go` (currently omitted — the dormant gap FR-033 closes) per [contracts/profile-api.md](./contracts/profile-api.md)
- [ ] T020 [US3] Carry `SkillNames`/`McpNames` onto the nested `AgentProfile` in the create mapping `projects/game/desktop/app.go` `CreateAgentProfile` (the update path already sets them) per [contracts/profile-api.md](./contracts/profile-api.md) (depends on T019)
- [ ] T021 [US3] Add MCP + skill selection chips to the create and edit forms in `projects/game/desktop/frontend/src/components/ProfileManagement.svelte`, mirroring the existing `toolNames` chip pattern, including `mcp_names`/`skill_names` in the update mask (FR-032) per [contracts/profile-api.md](./contracts/profile-api.md)
- [ ] T022 [US3] Wire `mcp_names`/`skill_names` through the create payload and update mask in `projects/game/desktop/frontend/src/api.ts`, reconciling the hand-maintained `CreateAgentProfileRequest` TS interface to the AIP-133 nested shape (or keeping the flat view-shape and letting `app.go` assemble) per [contracts/profile-api.md](./contracts/profile-api.md) (depends on T021)

**Checkpoint**: US3 complete — MCP/skill selection round-trips through the desktop UI.

---

## Phase 6: User Story 5 — Agent Service Reorganization (Priority: P3)

**Goal**: The agent service has `tools/`, `mcp/`, `skill/` capability directories each self-describing via a README, with the mouse tool relocated under `tools/mouse/` (FR-028..030).

**Independent Test**: List the agent source tree and confirm the three capability directories each contain a README, the mouse tool lives under `tools/mouse/` and still builds and passes, and the saolei MCP/skill live under `mcp/saolei/`/`skill/saolei/` (SC-006).

### Implementation for User Story 5

Required Reading:
- 规范文档: `style/javascript.md` (TS — mouse-tool relocation); `style/README.md` (style index)
- 官方文档: None
- 技术文章: None

- [ ] T023 [P] [US5] Relocate `projects/game/agent/src/mouse-tool.ts` and `mouse-tool.test.ts` into `projects/game/agent/src/tools/mouse/` and update all imports (the unit itself is unchanged — a move)
- [ ] T024 [P] [US5] Author `projects/game/agent/src/tools/README.md`, `projects/game/agent/src/mcp/README.md`, and `projects/game/agent/src/skill/README.md`, each describing its directory's purpose, conventions, and how to add a new entry (FR-028)
- [ ] T025 [US5] Regenerate BUILD files via `bazel run //:gazelle` and run `bazel build //projects/game/agent/...` and `bazel test //projects/game/agent/...` to confirm the mouse tool relocation builds and passes (FR-029) (depends on T023)

**Checkpoint**: US5 complete — the capability layout is self-describing and the mouse tool is relocated.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Feature-level acceptance and whole-repo hygiene.

Required Reading:
- 规范文档: `style/large_test.md` (large-test authoring — which transitively requires `style/golang.md`, since large tests are Go and follow its unit-test rules); `style/README.md` (style index)
- 官方文档: None
- 技术文章: [Minesweeper keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — F2/cell-coordinate scenario asserted by the large test

- [ ] T026 [P] Author the large-test testplan for accurate Minesweeper play (quickstart scenario 10) in `testplan/` per `style/large_test.md` (read it first per `AGENTS.md`): bind the Minesweeper window, create a profile with `mcp_names:["saolei"]` + `skill_names:["saolei"]`, drive `saolei_init(9,9)` → `saolei_click(4,4)` → `saolei_update(...)`, and assert the click lands inside cell (4,4) with no OS cursor marker over the target (SC-001/SC-003/SC-004). This is the feature-level large-test checkpoint (§VI), not a per-change gate.
- [ ] T027 Run `bazel build //...`, `bazel run //:gazelle`, and `bazel mod tidy` from repo root to confirm the whole workspace builds and BUILD/dependency files are regenerated and consistent after all feature changes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS Phase 3 (proto contract required by US1's input path).
- **US1+US2 (Phase 3)**: Depends on Phase 2. This is the MVP.
- **US4 (Phase 4)**: Depends on Phase 2 only (skill injection is independent of the MCP internals); may run in parallel with Phase 3.
- **US3 (Phase 5)**: Depends on Phase 2 only (desktop UI is independent of the agent service); may run in parallel with Phase 3.
- **US5 (Phase 6)**: No hard dependency on other stories (the `tools/mcp/skill` dirs already exist); may run in parallel with Phases 3-5.
- **Polish (Phase 7)**: Depends on all desired user stories being complete; T026 (large test) validates Phases 3-6 acceptance.

### User Story Dependencies

- **US1 + US2 (P1)**: Start after Phase 2. Implemented together (shared board/tools code). No dependency on other stories.
- **US4 (P2)**: Start after Phase 2. Independent of US1/US2/US3/US5.
- **US3 (P2)**: Start after Phase 2. Independent of US1/US2/US4/US5.
- **US5 (P3)**: Start any time after Phase 1. Independent of US1/US2/US3/US4.

### Within Phase 3 (US1+US2)

- T005 (board) and T006 (geometry) first (parallel).
- T007 (validation) after T005.
- T008/T009/T010 (desktop executor) after T002/T003.
- T011 (bridge) after T002.
- T012 (SaoleiMcp) after T005/T006/T007.
- T013 (tools) after T011/T012.
- T014 (buildTools wiring) after T013; T015 after T012/T014.

### Parallel Opportunities

- T005 ∥ T006 (board ∥ geometry — different files).
- T016 ∥ T024 ∥ T023 (skill doc ∥ READMEs ∥ mouse relocation — different files).
- After Phase 2, Phases 3, 4, 5, and 6 may proceed in parallel if staffed (different files/directories).
- All tasks marked `[P]` within a phase can run in parallel.

---

## Parallel Example: Phase 3 (US1+US2)

```bash
# Launch the two independent foundational units together:
Task: "T005 board state machine in projects/game/agent/src/mcp/saolei/board.ts"
Task: "T006 coordinate computation in projects/game/agent/src/mcp/saolei/geometry.ts"

# After Phase 2, Phase 3/4/5/6 can be staffed in parallel:
Developer A: Phase 3 (US1+US2) — MCP core
Developer B: Phase 4 (US4) — skill
Developer C: Phase 5 (US3) — profile UI
Developer D: Phase 6 (US5) — reorganization
```

---

## Implementation Strategy

### MVP First (US1 + US2 only)

1. Complete Phase 1 (Setup baseline).
2. Complete Phase 2 (Foundational proto contract) — CRITICAL, blocks the MCP.
3. Complete Phase 3 (US1 + US2).
4. **STOP and VALIDATE**: run the Phase 3 independent test + quickstart unit scenarios.
5. The agent can now play Minesweeper accurately through the validated MCP — demo if ready.

### Incremental Delivery

1. Setup + Foundational → contract ready.
2. Add Phase 3 (US1+US2) → validate → **MVP**.
3. Add Phase 4 (US4) → skill injected → validate.
4. Add Phase 5 (US3) → operator UI selection → validate.
5. Add Phase 6 (US5) → self-describing layout → validate.
6. Phase 7 → large-test acceptance (T026) + whole-repo build (T027).

### Parallel Team Strategy

1. Team completes Setup + Foundational together.
2. Once Foundational is done, Phases 3/4/5/6 proceed in parallel by different developers.
3. Phase 7 (large test + repo build) runs last against the integrated feature.

---

## Notes

- `[P]` tasks = different files, no dependencies on incomplete tasks.
- `[Story]` labels map tasks to user stories for traceability; US1+US2 share Phase 3 by design.
- Every code-changing task carries build + unit-test per-change verification (§VI); there are no separate unit-test tasks.
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
- Run `bazel run //:gazelle` after adding/moving source files so BUILD files stay current (per `AGENTS.md`).

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangChain.js — overview](https://docs.langchain.com/oss/javascript/langchain/overview) + [agents](https://docs.langchain.com/oss/javascript/langchain/agents) — LangChain v1; `createAgent`/`tool()` used by the agent service (catalog pins `langchain ^1.5.0`, `@langchain/core ^1.2.0`, `@langchain/langgraph ^1.4.4`).
- [Model Context Protocol — architecture](https://modelcontextprotocol.io/docs/concepts/architecture) — MCP host/client/server topology; the agent service embeds the server role (D-1).
- [Agent Skills specification](https://agentskills.io/specification) — `SKILL.md` format adopted for the saolei skill (D-2).
- [Wails v2](https://wails.io/) — desktop shell providing Win32 window access for `PostMessage` delivery.
- [AIP-131](https://google.aip.dev/131), [AIP-132](https://google.aip.dev/132), [AIP-133](https://google.aip.dev/133), [AIP-134](https://google.aip.dev/134), [AIP-156](https://google.aip.dev/156) — API Improvement Proposals the inherited `game.proto` profile create/update shapes conform to (per [contracts/profile-api.md](./contracts/profile-api.md)).
- [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) — the TS convention cited by `style/javascript.md`.

### Repositories

- [`langchain-ai/deepagents` — `SkillsMiddleware`](https://github.com/langchain-ai/deepagents/blob/46e10640caf78a84f9715cb8807882ea1b825d6a/libs/deepagents/deepagents/middleware/skills.py) (commit `46e10640c`) — skill injection as system-prompt assembly (reference only, not adopted; D-2).

### Articles & RFCs

- [Minesweeper (video game) — keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — F2 "new game" convention used by `saolei_init`.

---

## Phase 8: Convergence

**Purpose**: Bridge the implementation gap introduced by the intervening `019-js-test-reliability` feature. Feature 018 was interrupted mid-Phase-3 (now complete) because the JS test runner was unreliable; 019 has since (a) replaced every `js_test` target with the single `vitest_test` macro driving one canonical `tools/dev/js/run_vitest.mjs`, (b) switched test `data` from the pre-compiled `:lib` to raw package source (Fix B — single module instance), (c) hardened module-level mocks to dependency-injection seams, and (d) returned the whole TS suite to green. This phase records the post-019 green baseline and adapts the *remaining* TS-authoring phases (Phase 4 onward) to the new conventions. It does **not** duplicate the unchecked Phase 4-7 work (T016-T027), which remains the primary remaining scope.

**State at convergence (recorded, not re-performed by T028)**: `bazel test //projects/game/agent:lib_test` reports **275 passed | 1 skipped (276)** — all saolei MCP tests green (`board` 19, `validation` 29, `geometry` 12, `saolei-mcp` 3, `saolei-tools` 21, plus `session-agent` saolei-ownership, `build-tools`, `operation-bridge`). The saolei tests already use the reliable DI/`vi.fn()` pattern (no module-level `vi.mock`), so Phase 3 (US1+US2) conforms to the post-019 conventions and needs no rework.

Required Reading:
- 规范文档: `style/javascript.md` §测试 (the 019-added Testing section — `vitest_test` macro usage, reliable-vs-fragile mocking, source-based `data`); `style/README.md` (style index)
- 官方文档: [vitest — Mocking guide](https://vitest.dev/guide/mocking.html) (DI/`vi.fn()` test-double seam rationale)
- 技术文章: [specs/019-js-test-reliability/plan.md](../../specs/019-js-test-reliability/plan.md) — Architecture Revision (why `vitest_test` macro) and Module-Identity Revision (Fix B — test against source, drop `:lib`)

- [ ] T028 Re-confirm and record the post-019 green baseline before resuming Phase 4-7 (supersedes the pre-019 baseline recorded in T001): run `bazel build //projects/game/agent/... //projects/game/desktop/...` and `bazel test //projects/game/agent:lib_test //common/js/logs:lib_test //common/js/resolver:lib_test //common/js/otel:lib_test //common/js/grpc/otel:lib_test //common/js/grpc/resolver:lib_test` from repo root; confirm every target PASSED and that the saolei MCP tests (Phase 3, T005-T015) are green, so resumption starts from a known-green state rather than the pre-019 red baseline. Record the pass counts in the commit message. (partial — T001 / Constitution §IV)
- [ ] T029 Execute the remaining TS-authoring tasks (Phase 4 T017 `projects/game/agent/src/prompt-client.ts` skill fetch + `projects/game/agent/src/prompt-client.test.ts`; Phase 4 T018 `projects/game/agent/src/llm.ts` skill assembly + `projects/game/agent/src/llm.test.ts` / `projects/game/agent/src/build-tools.test.ts`) under the post-019 test conventions: (1) `projects/game/agent/BUILD.bazel` declares `lib_test` via the `vitest_test` macro with `data = glob(["src/**/*.ts"], allow_empty = True) + [...node_modules...]` (source-based, no `:lib`) so new `.ts` files under `skill/` are auto-included — add NO manual `js_test`/`entry_point`/`:lib`-in-data wiring, only `bazel run //:gazelle` if a new file is created outside the glob; (2) mocks MUST use the reliable dependency-injection / `vi.fn()` test-double seam (the saolei tests and the 019-refactored `prompt-client.test.ts` already do) and NOT module-level `vi.mock("external-dep")` against external packages, per `style/javascript.md` §测试; every mock MUST be asserted-called (FR-010 of 019); (3) each remaining phase's §V Required Reading MUST include `style/javascript.md` §测试 and [specs/019-js-test-reliability/plan.md](../../specs/019-js-test-reliability/plan.md) Module-Identity Revision. Verify each change with `bazel test //projects/game/agent:lib_test`. (partial — plan §VI / Constitution §V; specs/019-js-test-reliability/plan.md)

**Checkpoint**: post-019 baseline recorded green; remaining TS phases adapted to the `vitest_test` + source-based-data + DI-mock conventions. Phases 4-7 (T016-T027) may resume on this foundation.
