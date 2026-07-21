# Tasks: Saolei MCP for Grid-Based Minesweeper Operation

**Input**: Design documents from `/specs/018-saolei-mcp/` — `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/proto-operation-contract.md`, `contracts/mcp-tool-contract.md`, `quickstart.md`.

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/ (all read by default as feature docs per Constitution §V).

**Tests**: Compile (`bazel build //...`) + unit tests (`bazel test //...` on the relevant target) are part of EVERY code task (Constitution Principle IV) — they are NOT broken out as separate tasks. Large tests are a separate acceptance phase (Principle VI).

**Organization**: Tasks grouped by user story (spec.md US1–US5) plus a Setup phase, a blocking Foundational (proto) phase, a parallel-capable desktop-execution phase, and a final large-test acceptance phase.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task in the same phase).
- **[Story]**: User-story phase tasks are labelled `[US1]`..`[US5]`. Setup / Foundational / desktop-execution / Polish tasks have NO story label.
- Exact file paths are included in every task description.

## Per-Phase Document List (Constitution Principle V — mandatory format)

Every phase below declares its document list under three categories — **代码规范文档** (repo `style/` docs + the external code specs they reference), **官方文档** (third-party official docs / GitHub READMEs), **技术文章** (other external references). A category with no relevant docs is explicitly marked **无**. The feature's own docs (`plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`) and `AGENTS.md` are read by default and are NOT repeated here. The planner has read each listed document and confirmed it contains the needed content.

---

## Phase 1: Setup (Dependencies)

**Purpose**: Add the MCP runtime + client dependencies.

### 文档清单

- **代码规范文档**: 无（仅依赖变更；依赖管理命令见 `AGENTS.md`，默认必读）。
- **官方文档**:
  - LangChain MCP adapters — https://docs.langchain.com/oss/javascript/langchain/mcp
  - MCP TypeScript SDK — https://github.com/modelcontextprotocol/typescript-sdk
- **技术文章**: 无。

### Tasks

- [ ] T001 Add MCP dependencies to `projects/game/agent/package.json` (`@langchain/mcp-adapters`, `@modelcontextprotocol/sdk`, `express`, `@types/express`) via the root `pnpm-workspace.yaml` catalog; run `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/agent up`, then `bazel run //:gazelle projects/game/agent`, then `bazel mod tidy`. Gate: `bazel build //projects/game/agent/...` passes.

**Checkpoint**: Dependencies resolvable; agent still builds.

---

## Phase 2: Foundational (Proto Operation Extensions) — BLOCKS all agent + desktop story work

**Purpose**: Extend `game.proto` with the generic operation Parts + `MouseInputMethod` enum (the contract `contracts/proto-operation-contract.md`). All agent and desktop code depends on the regenerated types.

**⚠️ CRITICAL**: No story work can begin until this phase is complete.

### 文档清单

- **代码规范文档**:
  - `style/api.md`
  - AIP-126 Enumerations — https://google.aip.dev/126
  - AIP-180 Backwards compatibility — https://google.aip.dev/180
  - AIP-140 Field names — https://google.aip.dev/140
- **官方文档**:
  - Protocol Buffers Proto3 Language Guide (Enumerations / Oneof / Updating a message type) — https://protobuf.dev/programming-guides/proto3/#enum
- **技术文章**: 无。

### Tasks

- [ ] T002 Extend `projects/game/game.proto` per `specs/018-saolei-mcp/contracts/proto-operation-contract.md`: add `enum MouseInputMethod` (`_UNSPECIFIED=0`, `_SIMULATED=1`, `_WINDOW_MESSAGE=2`), `enum KeyboardKey` (`_UNSPECIFIED=0`, `_F2=1`), `message KeyboardPressPart`, `message MouseMoveAndClickPart`; add `MouseInputMethod method` to `MouseMovePart` and `MouseClickPart`; add `KeyboardPressPart keyboard_press = 7` and `MouseMoveAndClickPart mouse_move_and_click = 8` to the `Part.kind` oneof. Gate: proto compiles (`bazel build //projects/game:game_proto`).
- [ ] T003 Regenerate consumed types: `bazel run //:gazelle projects/game/agent projects/game/desktop`, then `bazel build //projects/game/agent:game_types //projects/game/desktop/...`. Verify the generated `KeyboardPressPart`, `MouseMoveAndClickPart`, `MouseInputMethod`, `KeyboardKey` types exist on both TS (`bazel-bin/.../game_types/`) and Go sides.

**Checkpoint**: Proto extended; types regenerated; backward compatible (existing mouse tools default to `SIMULATED`).

---

## Phase 3: Desktop Execution of New Parts (FR-004a..d)

**Purpose**: The desktop gains the generic keyboard + window-message-mouse execution primitives. Depends on Phase 2 only — **these tasks MAY run in parallel with Phases 4–8** (different files; no agent-code dependency). Required for the large-test acceptance gate (Phase 9).

### 文档清单

- **代码规范文档**:
  - `style/golang.md`
  - Google Go Style Guide — https://google.github.io/styleguide/go/guide ; Style Decisions — https://google.github.io/styleguide/go/decisions ; Best Practices — https://google.github.io/styleguide/go/best-practices
- **官方文档**:
  - WM_MOUSEMOVE (lParam client-coordinate packing) — https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-mousemove
  - Mouse Input Notifications (WM_LBUTTONDOWN/WM_RBUTTONDOWN/...) — https://learn.microsoft.com/en-us/windows/win32/inputdev/mouse-input-notifications
  - PostMessage function — https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagea
  - Keyboard Input Notifications (WM_KEYDOWN/WM_KEYUP) — https://learn.microsoft.com/en-us/windows/win32/inputdev/keyboard-input-notifications
- **技术文章**: 无。

### Tasks

- [ ] T004 [P] Implement window-message mouse path: create `projects/game/desktop/internal/operation/window_message_windows.go` (PostMessage `WM_LBUTTONDOWN`/`WM_LBUTTONUP`/`WM_RBUTTONDOWN`/`WM_RBUTTONUP` to the bound HWND with client coords packed into `lParam` via `MAKELPARAM`; no OS cursor movement) and a non-Windows stub in `projects/game/desktop/internal/operation/execute_other.go`.
- [ ] T005 [P] Implement real keyboard press: replace the `ExecuteKeyPress` stub in `projects/game/desktop/internal/operation/execute_windows.go` with a real F2 implementation (PostMessage `WM_KEYDOWN`/`WM_KEYUP` with `VK_F2 = 0x71`, or `SendInput` `KEYBDINPUT`).
- [ ] T006 Wire the new Parts into `projects/game/desktop/app.go` `executeAgentOperation`: handle `KeyboardPressPart` and `MouseMoveAndClickPart`; honour `MouseInputMethod` (`SIMULATED` → existing `ScreenshotToScreenCoords` + `SetCursorPos`/`SendInput` path; `WINDOW_MESSAGE` → client coords direct, no screen offset, no cursor move); `UNSPECIFIED` treated as `SIMULATED`. Add Go table-driven unit tests (per `style/golang.md`). Gate: `bazel test //projects/game/desktop/internal/operation/...`.

**Checkpoint**: Desktop executes F2 + window-message mouse generically; existing `mouse_move`/`mouse_click` behavior unchanged.

---

## Phase 4: User Story 1 — Configure Saolei MCP Profile & Expose Tools (Priority: P1) 🎯 MVP

**Goal**: A saolei-enabled profile exposes the per-session MCP endpoint; an MCP client lists exactly the five saolei tools; mouse tools are not exposed; `saolei_init(width, height)` works.

**Independent Test** (quickstart.md Scenarios 1 & 2): configure a saolei profile (mcp_names round-trips), start a session, connect a loopback MCP client to `/internal/mcp/{session_id}`, list tools → exactly `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_update`; mouse tools absent; `saolei_init(9,9)` dispatches a `KeyboardPressPart{F2}` and initialises a 9×9 grid; unknown session_id → 404.

### 文档清单

- **代码规范文档**:
  - `style/javascript.md`
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**:
  - LangChain MCP adapters (`MultiServerMCPClient`, `getTools`) — https://docs.langchain.com/oss/javascript/langchain/mcp
  - langchain-mcp-adapters README (transports, error handling) — https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/README.md
  - MCP TypeScript SDK (Streamable HTTP server) — https://github.com/modelcontextprotocol/typescript-sdk
  - MCP specification (transport) — https://modelcontextprotocol.io/specification
- **技术文章**: 无。

### Tasks

- [ ] T007 [P] [US1] Desktop profile MCP chip: add an "MCP: saolei" selectable chip to the create + edit forms and a badge to the profile card in `projects/game/desktop/frontend/src/components/ProfileManagement.svelte`; include `mcp_names` in the create request and in the update-mask array; validate via the existing profile CRUD flow.
- [ ] T008 [P] [US1] Agent profile `mcpNames` plumbing: extend `ProfileResult` in `projects/game/agent/src/prompt-client.ts` (`getProfile` reads `response.mcpNames`) and `ProfileData` in `projects/game/agent/src/session-agent.ts` to carry `mcpNames: string[]`.
- [ ] T009 [US1] Agent MCP host: create `projects/game/agent/src/mcp-host.ts` — a localhost HTTP server (Express) routing `/internal/mcp/{session_id}` to a lazily-created, session-bound `McpServer` (look up `SessionAgent` via `SessionAgentStore`, obtain its `OperationBridge`); unknown `{session_id}` → 404 `Session not found`; start it alongside the gRPC server in `projects/game/agent/src/server.ts` (port via env, e.g. `MCP_PORT`).
- [ ] T010 [US1] Agent saolei integration skeleton + `saolei_init`: create `projects/game/agent/src/mcp/saolei/game-state.ts` (`CellStatus`, `GameState` with `width`/`height`/`grid`/`pendingUpdate`/`lastOp`/`initialized` per data-model.md §1-3), `projects/game/agent/src/mcp/saolei/geometry.ts` (fixed constants `BOARD_ORIGIN_X_PX=24`, `BOARD_ORIGIN_Y_PX=200`, `CELL_SIZE_PX=32` + `center(x,y)` formula per data-model.md §5), and `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (register all 5 tools with schemas per `contracts/mcp-tool-contract.md`; fully implement `saolei_init(width,height)` = dispatch `KeyboardPressPart{KEY_F2}` via `OperationBridge` + init/reset `GameState`; register `saolei_click`/`saolei_flag`/`saolei_chord_click`/`saolei_update` with schemas and placeholder handlers returning "not yet implemented" — filled by US2/US3).
- [ ] T011 [US1] Agent adapter integration: in `projects/game/agent/src/llm.ts` extend `AdapterFactory` to receive `mcpNames`; for a saolei profile build the tools via `MultiServerMCPClient({ saolei: { transport: "http", url: "http://localhost:${MCP_PORT}/internal/mcp/${sessionId}" } })` → `getTools()` (FR-002b) and feed to the existing `createAgent({ model, tools })`; exclude `mouse_move`/`mouse_click` for saolei profiles (FR-012); thread `mcpNames` through `session-agent.ts` `serializeBind`. Add TS unit tests (DI per `style/javascript.md` — inject a fake bridge + fake MCP host). Gate: `bazel test //projects/game/agent:lib_test`.

**Checkpoint**: US1 independently testable — profile config round-trips; MCP endpoint serves the 5 tools to a loopback client; `saolei_init` dispatches F2 + inits state; mouse tools excluded.

---

## Phase 5: User Story 2 — Left-Click Reveal With Manual State Update (Priority: P1)

**Goal**: `saolei_click` reveals a cell and `saolei_update` applies a connected, rule-consistent batch; the operate-then-update alternation is enforced.

**Independent Test** (quickstart.md Scenario 3 + 4): `init` → `click` → `update` cycle; a second operation before `update` is rejected; a validation-rejected click does not lock (immediate retry allowed).

### 文档清单

- **代码规范文档**:
  - `style/javascript.md`
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**: 无。
- **技术文章**:
  - Minesweeper rules (cascade reveal, connectivity) — https://en.wikipedia.org/wiki/Minesweeper_(video_game)
  - minesweeper.now gameplay — https://minesweeper.now/help/gameplay

### Tasks

- [ ] T012 [P] [US2] Agent validation module: create `projects/game/agent/src/mcp/saolei/validation.ts` — 8-connectivity helper + the click validator (FR-013: target must be INITIAL pre-dispatch; post-update target changed + all updated number cells connected through the batch) and the general range check (FR-016: coordinates within `[0,width)×[0,height)`). Pure functions, table-tested.
- [ ] T013 [US2] Agent `saolei_click` + `saolei_update` handlers in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`: `saolei_click(x,y)` validates target INITIAL (reject pre-dispatch, no `pendingUpdate` set on reject per Clarification Q3), dispatches `MouseMoveAndClickPart{ click: LEFT_CLICK, method: WINDOW_MESSAGE }` at `center(x,y)` via `OperationBridge`, sets `pendingUpdate=true`/`lastOp={kind:"click",...}` (FR-007); `saolei_update(cells)` requires `pendingUpdate`, runs the click validator (FR-013) + alternation (FR-011) + range (FR-016), applies on accept (clears `pendingUpdate`), rejects with state unchanged on failure. Results returned as normal MCP text (research.md D8). Gate: `bazel test //projects/game/agent:lib_test`.

**Checkpoint**: US2 independently testable — the core init→click→update loop with click validation + alternation.

---

## Phase 6: User Story 3 — Flag Toggle & Chord Click With Validation (Priority: P2)

**Goal**: `saolei_flag` toggles a flag (INITIAL↔FLAG only); `saolei_chord_click` performs a single simultaneous left+right press on a satisfied number cell; each tool's post-update shape is validated.

**Independent Test** (quickstart.md Scenario 3 flag/chord + 4): flag toggle accept/reject; chord on a satisfied number dispatches `LEFT_RIGHT_PRESS`; post-chord update preserves target-adjacent flags and updates other neighbors (mine-hit excepted) with per-component connectivity.

### 文档清单

- **代码规范文档**:
  - `style/javascript.md`
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**: 无。
- **技术文章**:
  - Minesweeper chording technique — https://rarepike.com/minesweeper/chord-technique/
  - Minesweeper rules — https://en.wikipedia.org/wiki/Minesweeper_(video_game)

### Tasks

- [ ] T014 [US3] Agent flag + chord validators in `projects/game/agent/src/mcp/saolei/validation.ts`: FR-014 (flag target INITIAL; post-update target transitions only INITIAL↔FLAG, no other cell changes); FR-015 (chord target is a non-0 number with adjacent FLAG count == number; post-update target-adjacent FLAG cells unchanged, other target-adjacent non-number cells updated except mine-hit, each connected component of updated number cells contains a target-adjacent cell). Table-tested.
- [ ] T015 [US3] Agent `saolei_flag` + `saolei_chord_click` handlers in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`: `saolei_flag(x,y)` validates INITIAL, dispatches `MouseMoveAndClickPart{ click: RIGHT_CLICK, method: WINDOW_MESSAGE }`, sets `lastOp={kind:"flag",...}` (FR-008); `saolei_chord_click(x,y)` validates the satisfied-number precondition, dispatches `MouseMoveAndClickPart{ click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }` (a single simultaneous left+right press — FR-009), sets `lastOp={kind:"chord_click",...}`; each `saolei_update` routes to the matching validator. Gate: `bazel test //projects/game/agent:lib_test`.

**Checkpoint**: US3 independently testable — flag + chord with their validation rules.

---

## Phase 7: User Story 4 — Validation Rejects Illegal Operations & Updates (Priority: P2)

**Goal**: Comprehensive reject paths across FR-013..FR-016 + mine-state semantics (FR-018/019); every rejection leaves state unchanged.

**Independent Test** (quickstart.md Scenario 4): each illegal operation/update (click flagged cell, flag an opened cell, chord unsatisfied number, disconnected update, out-of-bounds update, inconsistent-with-lastOp update, chord that changes a target-adjacent flag) is rejected with state unchanged.

### 文档清单

- **代码规范文档**:
  - `style/javascript.md`
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**: 无。
- **技术文章**:
  - Minesweeper rules — https://en.wikipedia.org/wiki/Minesweeper_(video_game)
  - minesweeper.now gameplay — https://minesweeper.now/help/gameplay

### Tasks

- [ ] T016 [US4] Harden validation + mine-state semantics in `projects/game/agent/src/mcp/saolei/validation.ts`: cover every reject path (FR-013..FR-016) including `HIT_MINE` (mine directly triggered by this op) vs `MINE` (mines shown at game end, not triggered by this op — Clarification D6) and the chord mine-hit exception (FR-015/FR-019); assert rejected updates mutate nothing. Add a focused table-driven test file `projects/game/agent/src/mcp/saolei/validation.test.ts` enumerating all accept/reject cases. Gate: `bazel test //projects/game/agent:lib_test`.

**Checkpoint**: US4 independently testable — the validation contract is complete for the shipped rule set (FR-017: extensible).

---

## Phase 8: User Story 5 — Built-in Saolei Skill Auto-Injected (Priority: P3)

**Goal**: When `mcp_names` includes `saolei`, the built-in `SKILL.md` is loaded and appended to the system prompt; otherwise not injected.

**Independent Test** (quickstart.md Scenario 5): a saolei profile's assembled prompt contains the skill body; a non-saolei profile's does not.

### 文档清单

- **代码规范文档**:
  - `style/javascript.md`
  - Google TypeScript Style Guide — https://google.github.io/styleguide/tsguide.html
- **官方文档**:
  - agentskills.io SKILL.md open standard — https://agentskills.io/specification
  - OpenCode-recognized SKILL.md subset — https://opencode.ai/docs/skills/
- **技术文章**: 无。

### Tasks

- [ ] T017 [P] [US5] Author the built-in skill at `projects/game/agent/src/skill/saolei/SKILL.md` per `specs/020-agent-resources-layout/contracts/skill-md-format.md` (frontmatter `name: saolei`; body covers minesweeper rules, the `(x,y)` top-left-origin coordinate convention, the five tools, the operation→update alternation, the `CellStatus` enum, and the validation expectations).
- [ ] T018 [US5] Agent skill loader + injection: create `projects/game/agent/src/skill-loader.ts` (load `src/skill/{name}/SKILL.md` bodies; `mcp_name → built-in skill` registry mapping `saolei → saolei`); in the `AdapterFactory` (`projects/game/agent/src/llm.ts`), when `mcpNames` includes `saolei`, append the saolei skill body to the `systemPrompt` before constructing `AgentAdapterImpl` (FR-023/024/025). Add a unit test asserting injection on/off by `mcpNames`. Gate: `bazel test //projects/game/agent:lib_test`.

**Checkpoint**: US5 independently testable — skill auto-injection tied to the saolei MCP selection.

---

## Phase 9: Polish & Large-Test Acceptance

**Purpose**: Service large-test acceptance (Constitution Principle VI) + the desktop Windows integration validation.

### 文档清单

- **代码规范文档**:
  - `style/golang.md`
  - `style/large_test.md`
  - Google Go Style Guide — https://google.github.io/styleguide/go/guide ; Style Decisions — https://google.github.io/styleguide/go/decisions ; Best Practices — https://google.github.io/styleguide/go/best-practices
- **官方文档**: 无（testplan/guitar 通过 `testplan` SKILL 加载；`design/guitar_yaml_testplan.md` 为仓库内文档，默认必读）。
- **技术文章**: 无。

### Tasks

- [ ] T019 [P] Agent MCP large test: add a module-named test file `projects/game/testplan/agent_saolei_test.go` (per `style/large_test.md` — organise by MODULE not by scenario; reuse `helpers_test.go`) covering the deployed agent's MCP endpoint (tool listing + a `saolei_init`/`saolei_click`/`saolei_update` flow via a deployed SUT, HTTP through the existing gateway), and add its case to the existing `projects/game/testplan/system_test.yaml` (do NOT create a new plan YAML). Run via the `testplan` SKILL (`guitar run`).
- [ ] T020 Desktop Windows integration validation (quickstart.md Scenarios 6 & 7): on a Windows host with Minesweeper bound, validate F2 (`KeyboardPressPart`) restarts the game and `WINDOW_MESSAGE` mouse reveals a cell with no OS cursor visible; validate an end-to-end `init → click → update → flag → update → chord_click → update` sequence. Document results in the test record; if a Windows large-test environment is unavailable, record this as a manual integration gate. (Depends on Phase 3 + Phases 4–7.)
- [ ] T021 Run the full `specs/018-saolei-mcp/quickstart.md` validation (Scenarios 1–5 are covered by the unit/integration tests above; Scenarios 6–7 acceptance via T019/T020) and confirm every Success Criterion SC-001..SC-007 in `spec.md` is met.

**Checkpoint**: Feature accepted — service large test green (agent MCP) + desktop Windows validation recorded.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no dependencies — start immediately.
- **Phase 2 (Foundational proto)**: depends on Phase 1; **BLOCKS all agent + desktop story work** (Phases 4–8) and Phase 3.
- **Phase 3 (Desktop execution)**: depends on Phase 2 only; **MAY run in parallel with Phases 4–8** (different files; no agent-code dependency). Required only for Phase 9 acceptance.
- **Phases 4–8 (User Stories)**: each depends on Phase 2; sequenced by priority and by shared files in `projects/game/agent/src/mcp/saolei/` + `llm.ts` (see story dependencies below).
- **Phase 9 (Polish/acceptance)**: depends on Phase 3 + the relevant story phases (T019 needs Phases 4–7; T020 needs Phase 3 + Phases 4–7).

### User Story Dependencies

- **US1 (Phase 4)**: depends on Phase 2 (proto types). No dependency on other stories. (Delivers the MCP host + saolei skeleton + `saolei_init` + adapter integration — the foundation the other agent story phases extend.)
- **US2 (Phase 5)**: depends on US1 (extends `saolei-mcp.ts` handlers + uses the `GameState`/`geometry` modules).
- **US3 (Phase 6)**: depends on US1 (same files as US2; extends `validation.ts` + `saolei-mcp.ts`).
- **US4 (Phase 7)**: depends on US2 + US3 (hardens the validators they introduced).
- **US5 (Phase 8)**: depends on US1 (modifies the same `AdapterFactory` in `llm.ts`).

### Within Each Phase

- Compile (`bazel build //...`) + unit tests (`bazel test //...` on the relevant target) run as part of each code task — not as separate tasks (Constitution Principle IV).
- [P] tasks within a phase touch different files and may run concurrently.
- Commit after each task or logical group; stop at any checkpoint to validate independently.

### Parallel Opportunities

- T004, T005 (Phase 3) — different files, parallel.
- T007, T008 (Phase 4) — desktop UI vs agent plumbing, parallel.
- T012 (Phase 5) and T017 (Phase 8) — different files, parallel where their phases overlap.
- Phase 3 as a whole is parallel with Phases 4–8 (second contributor, Windows desktop focus).
- Within US1: T007/T008 (parallel) → T009/T010 (depend on T008) → T011 (depends on T009/T010).

---

## Parallel Example: User Story 1

```text
# Parallel pair 1 (different files):
T007  Desktop profile MCP chip         (projects/game/desktop/frontend/.../ProfileManagement.svelte)
T008  Agent profile mcpNames plumbing  (projects/game/agent/src/prompt-client.ts, session-agent.ts)

# Then (depend on T008):
T009  Agent MCP host                   (projects/game/agent/src/mcp-host.ts, server.ts)
T010  Agent saolei skeleton + init     (projects/game/agent/src/mcp/saolei/*)

# Then (depends on T009 + T010):
T011  Agent adapter integration        (projects/game/agent/src/llm.ts, session-agent.ts)
```

---

## Implementation Strategy

### MVP First (Phases 1 + 2 + 4 = US1)

1. Complete Phase 1 (Setup) and Phase 2 (Foundational proto) — **CRITICAL**, blocks everything.
2. Complete Phase 4 (US1) — configure a saolei profile, stand up the MCP endpoint, expose the 5 tools, make `saolei_init` work.
3. **STOP and VALIDATE**: US1 independent test (quickstart.md Scenarios 1 & 2). This is a demoable MVP — an operator can configure saolei and init a game through the MCP tools.

### Incremental Delivery

4. Add Phase 5 (US2) → core click+update loop → validate independently.
5. Add Phase 6 (US3) → flag + chord → validate independently.
6. Add Phase 7 (US4) → validation completeness → validate independently.
7. Add Phase 8 (US5) → skill auto-injection → validate independently.
8. Phase 3 (desktop execution) can be done in parallel from step 2 onward; it + Phase 9 close out the large-test acceptance.

Each story adds value without breaking previous stories; all story unit tests remain green throughout.

---

## Notes

- [P] = different files, no dependency on an incomplete same-phase task.
- [Story] maps a task to its user story for traceability.
- Every code task includes its compile + unit gate (bazel) as part of the task (Constitution Principle IV).
- The feature's own design docs (`plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`) and `AGENTS.md` are read by default before any code (Constitution Principle V); the per-phase 文档清单 lists the ADDITIONAL `style/` + external docs each phase requires.
- Large tests are organised by MODULE and added to the existing `system_test.yaml` (per `style/large_test.md` anti-patterns) — no new test-plan YAML, no spec/scenario-named files.
