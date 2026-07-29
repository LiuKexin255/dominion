# Tasks: Queued Chat Input During Agent Run

**Input**: Design documents from `/specs/030-queued-chat-input/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Unit/contract tests are folded into each implementation task (Constitution §IV — compile+unit-test are part of the dev task, not separate tasks). The large test is a separate acceptance task (Constitution §IV/§VI).

**Organization**: Tasks are grouped by user story. US1 and US2 are combined into the MVP phase because the spec marks them co-equal P1 ("the two halves only deliver value together") — neither is independently valuable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

This feature lives in the existing desktop-app + service layout (no new project):
- Agent service (TypeScript): `projects/game/agent/src/`
- Desktop backend (Go): `projects/game/desktop/`
- Desktop frontend (Svelte 5): `projects/game/desktop/frontend/src/`
- Proto: `projects/game/game.proto`
- Large tests (Go): `projects/game/testplan/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: The one additive, self-contained proto change. It lands first because proto type regeneration is a global mechanical step and the `QueueSignal` type must exist before US4 wires it. It changes no behavior.

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/api.md`；[AIP-140 Field names](https://google.aip.dev/140)（新增字段 `queued_count` 命名规范）
- **官方文档**: [Protocol Buffers Language Guide (proto3) — Oneof](https://protobuf.dev/programming-guides/proto3/#oneof)
- **技术文章**: `specs/030-queued-chat-input/contracts/queue-channel-contract.md`（QueueSignal 字段与发射规则定义）

- [ ] T001 Add additive `QueueSignal` message and `FlowPart` oneof field `QueueSignal queue = 9` (next free tag after `flow_result = 8`) to `projects/game/game.proto` per `specs/030-queued-chat-input/contracts/queue-channel-contract.md`; regenerate proto types via `bazel build //projects/game/game:game_proto` (or the repo's proto target) and run `bazel run //:gazelle projects/game` to refresh `BUILD.bazel`; verify `bazel build //projects/game/...` is green. No behavior wired yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The LangGraph-native queue backbone — the per-session `TurnLoop` (buffer + single-message drain) that replaces the per-frame `acquireMutex→generateTurn→releaseMutex` path. This is the single-flight mechanism ALL of US1/US2/US3 build on.

**⚠️ CRITICAL**: No user-story frontend/backend-queue work can begin until this phase is complete.

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用，作为本仓库 js/ts 规范基准）；[vitest — Mocking Modules Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（采用依赖注入而非 `vi.mock`，`style/javascript.md` §Mock 约定 引用）
- **官方文档**: [LangGraph — Add memory (MemorySaver, multi-turn)](https://docs.langchain.com/oss/javascript/langgraph/add-memory)；[LangGraph — Use functional API (repeated streamEvents on same thread_id)](https://docs.langchain.com/oss/javascript/langgraph/use-functional-api)；[LangGraph — Interrupts (why interrupt() is the wrong primitive)](https://docs.langchain.com/oss/javascript/langgraph/interrupts)；[LangGraph — Time travel / forking (in-flight isolation)](https://docs.langchain.com/oss/javascript/langgraph/use-time-travel)
- **技术文章**: `specs/030-queued-chat-input/contracts/turn-loop-contract.md`；`specs/030-queued-chat-input/contracts/queue-channel-contract.md`；`specs/030-queued-chat-input/data-model.md`；`specs/030-queued-chat-input/research.md`（D1/D2/D5/D6/D7）；`specs/030-queued-chat-input/quickstart.md`；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（声明 `js_test` target 的 `vitest_test` 宏与 `data` 规则，`style/javascript.md` §测试 引用）

- [ ] T002 [P] Create the `TurnLoop` class in `projects/game/agent/src/turn-loop.ts` implementing the LangGraph-native queue+loop per `specs/030-queued-chat-input/contracts/turn-loop-contract.md` and `specs/030-queued-chat-input/data-model.md`: FIFO `buffer`, `submit(content)` (start loop if idle, else buffer), `isRunning()`, `queueDepth()`, `abort()`; the loop drives `AgentAdapter.generateTurn` (= `streamEvents`) and, on turn completion, drains the next queued message as the next turn on the same `thread_id` (single-message path: one turn per drain) and emits `wait` only when the buffer is empty; abort clears the buffer + emits `wait` (FR-011); a non-abort turn error retains the buffer + emits `warn` (FR-015). Use dependency injection for the adapter provider and the `emit` sink (per `style/javascript.md` §Mock). Include `projects/game/agent/src/turn-loop.test.ts` (submit-while-idle starts loop; submit-while-running buffers and does not disturb the in-flight turn; empty-buffer ⇒ exactly one terminal `wait`; abort clears buffer + emits `wait`; non-abort error retains buffer) and run `bazel test //projects/game/agent/...`.
- [ ] T003 Wire `projects/game/agent/src/session-agent.ts` to own one `TurnLoop` per session (construct it in `getOrCreate`, reusing the existing `MemorySaver` checkpointer and adapter provider) and expose `isRunning()`/`submit()`/`abort()` per `specs/030-queued-chat-input/contracts/turn-loop-contract.md`. (Depends on T002.) Update `projects/game/agent/src/session-agent.test.ts` accordingly; run `bazel test`.
- [ ] T004 Refactor the user-content path in `projects/game/agent/src/handler.ts` (currently `acquireMutex→generateTurn→releaseMutex` at lines ~390-542): route inbound user frames to the per-session `TurnLoop` via `SessionAgent` instead of running `generateTurn` inline; remove the per-frame mutex acquire/release for user content (the loop is now the single-flight owner); have `wait` emitted solely by the loop on full drain; keep the profile-name guard (lines ~320-359) upstream of the loop; update the status-probe call site (~lines 258-261) to feed `deriveStatusSignal(isInFlight = sessionAgent.isRunning(), state.isBound)` — `projects/game/agent/src/status-signal.ts` is unchanged. Recast the existing FIFO-serialization test in `projects/game/agent/src/handler.test.ts` to assert loop behavior (no concurrent turns; a queued message becomes the next turn; status ACTIVE while running / IDLE when drained) per `specs/030-queued-chat-input/research.md` D5; run `bazel test //projects/game/agent/...`. (Depends on T002, T003.)

**Checkpoint**: The LangGraph-native queue backbone is unit-tested in isolation; a queued single message becomes the next turn and `wait` fires only on full drain. Frontend still has input disabled (US1) — next phase.

---

## Phase 3: User Story 1 & 2 — Input During Run + Auto Hand-off (Priority: P1) 🎯 MVP

**Goal**: The user can type and submit while the agent is running (FR-001/FR-002), and the queued message automatically becomes the next agent turn (FR-005/FR-006). Delivered value: a working queue for the single-message case.

**Independent Test**: `specs/030-queued-chat-input/quickstart.md` Scenario 1 (input editable/submittable during a run; submission does not disturb the in-flight turn) and Scenario 2 (the queued message becomes the next turn automatically, single terminal `wait`).

> Note: the pending indicator here is frontend-optimistic (frontend-tracked); US4 (Phase 5) upgrades it to backend-driven `QueueSignal`.

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用）
- **官方文档**: [Svelte 5 — What are Runes (`$state`)](https://svelte.dev/docs/svelte/what-are-runes)（前端用 `$state` 管理 `processing`/`queueCount`）
- **技术文章**: `specs/030-queued-chat-input/contracts/queue-channel-contract.md`（`wait` 仅在全排空时发射的语义）；`specs/030-queued-chat-input/quickstart.md`（Scenario 1-2）；[opencode session/prompt modules](https://github.com/anomalyco/opencode/tree/main/packages/opencode/src/session)（行为参考：input-while-running queue 交互模式）

- [ ] T005 [P] [US1] Remove the run-time input lock in `projects/game/desktop/frontend/src/components/ChatView.svelte`: delete `disabled={processing}` from the chat `<textarea>` (line ~358) and from the Send `<button>` (line ~364) so the input is editable/submittable while an agent turn is in progress (spec FR-001).
- [ ] T006 [US1] Update `projects/game/desktop/frontend/src/App.svelte` `handleSendChatText` (lines ~627-655): when submitting while `processing === true`, send the frame immediately (the backend `TurnLoop` buffers it — non-blocking `SendUserTurn` is unchanged) and render the user message in a pending visual state using the existing `queueCount` affordance (`projects/game/desktop/frontend/src/components/ChatView.svelte:335-339`); keep `processing` semantics so it stays `true` across the queued-turn boundary and clears only on the terminal `wait` (spec FR-002). (Depends on T005 and Phase 2.)
- [ ] T007 [US2] Verify end-to-end via quickstart walkthrough: manually execute `specs/030-queued-chat-input/quickstart.md` Scenarios 1 and 2 against a built desktop — confirm the input is editable during a run, a submitted message does not alter the in-flight turn, and on turn completion the queued message automatically becomes the next turn (single terminal `wait`). Also run `bazel build //projects/game/... && bazel test //projects/game/agent/...` and confirm green.

**Checkpoint**: MVP delivered — input-while-running + single-message auto hand-off works end-to-end.

---

## Phase 4: User Story 3 — Combine Multiple Queued Messages Into One Turn (Priority: P2)

**Goal**: When ≥2 messages are queued during a run, they are combined into ONE next turn's input in FIFO order (spec FR-004/FR-005; `/speckit.clarify` decision).

**Independent Test**: `specs/030-queued-chat-input/quickstart.md` Scenario 3 — submit A, B, C during a run; on completion exactly ONE next turn begins whose combined input contains A, B, C in order.

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: [LangChain — Messages concepts (HumanMessage, multimodal content blocks)](https://js.langchain.com/docs/concepts/messages/)（合并为单个 `HumanMessage` 多 content block 的模型安全表示）；[LangGraph — Use functional API (multi-turn)](https://docs.langchain.com/oss/javascript/langgraph/use-functional-api)
- **技术文章**: `specs/030-queued-chat-input/research.md`（D3 合并表示）；`specs/030-queued-chat-input/contracts/turn-loop-contract.md`（`combineAll`）；`specs/030-queued-chat-input/quickstart.md`（Scenario 3）

- [ ] T008 [P] [US3] Generalize `projects/game/agent/src/llm.ts` `TurnContent` / `streamFromAgent` (lines ~455-484) to build model content blocks from **N text parts + M image parts** (each image followed by its pixel-size annotation block), so an aggregated turn can carry multiple queued messages; the existing single-message shape is the N=1/M∈{0,1} case (backward compatible) per `specs/030-queued-chat-input/research.md` D3. Include multi-part content-block ordering assertions in `projects/game/agent/src/llm.test.ts`; run `bazel test`.
- [ ] T009 [US3] Implement `combineAll` in `projects/game/agent/src/turn-loop.ts`: change the drain step from "one turn per message" to "merge ALL pending into a single aggregated `HumanMessage` (FIFO)" and run it as exactly one next turn (per `specs/030-queued-chat-input/contracts/turn-loop-contract.md` loop body and `data-model.md`); the buffer is cleared and a depth-0 signal pushed on drain. Add multi-message combine + FIFO-order cases to `projects/game/agent/src/turn-loop.test.ts`; run `bazel test`. (Depends on T008.)

**Checkpoint**: Multiple queued messages combine into one aggregated turn, FIFO, with no loss of text or screenshots.

---

## Phase 5: User Story 4 — Queue Visibility & Control (Priority: P3)

**Goal**: The pending queue is visibly indicated (driven by the backend) and a queued message can be removed before it is consumed (spec FR-008/FR-009/FR-010).

**Independent Test**: `specs/030-queued-chat-input/quickstart.md` Scenario 4 — queue a message, see the indicator, remove it, confirm it is not delivered.

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；`style/api.md`；[AIP-140 Field names](https://google.aip.dev/140)
- **官方文档**: 无
- **技术文章**: `specs/030-queued-chat-input/contracts/queue-channel-contract.md`（`QueueSignal` 发射规则 + `wait` 语义）；`specs/030-queued-chat-input/quickstart.md`（Scenario 4）；[opencode session/prompt modules](https://github.com/anomalyco/opencode/tree/main/packages/opencode/src/session)（行为参考：pending/移除 UX 交互模式）

- [ ] T010 [P] [US4] Emit `QueueSignal` (proto field from T001) from `projects/game/agent/src/turn-loop.ts` on every per-session queue-depth change (submit ⇒ +1/new depth; drain-to-next-turn ⇒ 0; abort ⇒ 0) via the registered `emit` sink, per `specs/030-queued-chat-input/contracts/queue-channel-contract.md`. Add `QueueSignal` depth-change assertions to `projects/game/agent/src/turn-loop.test.ts`; run `bazel test`.
- [ ] T011 [US4] Drive the pending-queue rendering from `QueueSignal.queued_count` in `projects/game/desktop/frontend/src/App.svelte` and `projects/game/desktop/frontend/src/components/ChatView.svelte`: replace the Phase-3 optimistic `queueCount` source with the backend `QueueSignal` value; transition pending messages to normal when depth decreases and the combined turn streams (spec FR-009). (Depends on T010.)
- [ ] T012 [US4] Add a remove/cancel affordance for pending queued messages in `projects/game/desktop/frontend/src/components/ChatView.svelte` (spec FR-010): removing a pending message drops it before hand-off and the frontend reflects the decreased count. (Depends on T011.)

**Checkpoint**: The queue is transparent (backend-driven count) and removable.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Cross-cutting verification (abort/failure/status) and large-test acceptance (Constitution §VI).

### 需阅读文档 (Constitution §V)

- **代码规范文档**: `style/javascript.md`（前端/agent 交叉校验）；`style/golang.md`（大型测试 Go 用例规范，`style/large_test.md` 引用）；`style/large_test.md`（测试计划组织、按模块拆分、禁止按 spec/交付物维度组织）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（如新增 `js_test` target）
- **官方文档**: [Go testing stdlib](https://pkg.go.dev/testing)；testplan SKILL（`/mnt/code/dominion/.opencode/skills/testplan/SKILL.md`，`style/large_test.md` 引用 `guitar run`）
- **技术文章**: `specs/030-queued-chat-input/quickstart.md`（Scenario 5-7）；既有测试计划 `projects/game/testplan/system_test.yaml` 与模块用例 `projects/game/testplan/agent_dialog_test.go`（按 `style/large_test.md` 要求就近追加，不得新建按 spec 命名的文件或独立计划）

- [ ] T013 [P] Verify cross-cutting behavior against `specs/030-queued-chat-input/quickstart.md` Scenarios 5-7: abort discards the queue (FR-011), delivery failure retains the queue (FR-015), and status reconciliation is unchanged (spec 021 — `STATUS_ACTIVE` while running/draining, `STATUS_IDLE` when idle). Additionally verify two edge cases from spec.md: (a) desktop disconnects mid-turn with messages queued — queue MUST be retained on reconnect, and the indicator MUST reflect only genuinely pending messages (no stale count from a prior view); (b) session re-entry with a non-empty queue — indicator must show correct pending count. Ensure `projects/game/agent/src/turn-loop.test.ts` and `projects/game/agent/src/handler.test.ts` cover these; run `bazel test //projects/game/agent/...` green.
- [ ] T014 Add queue-while-running large-test cases to the existing module test file `projects/game/testplan/agent_dialog_test.go` (organized by module per `style/large_test.md` — NOT by spec/feature number), and register the new cases as a `suite`/`case` in the existing `projects/game/testplan/system_test.yaml` (do NOT create a new per-feature YAML — `style/large_test.md` 反模式 #1/#4). Cover Scenarios 1-4 over the deployed agent SUT; follow `style/golang.md` (given/when/then, table-driven, no asserts inside cases). Run `bazel test //projects/game/testplan/...` to confirm the targets build/run.
- [ ] T015 Execute the large test via the testplan skill: `guitar run projects/game/testplan/system_test.yaml` (full deploy→test→cleanup). Constitution §VI acceptance requires ALL cases to pass; fix and re-run until fully green. Build-only checks do NOT constitute acceptance.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately. (The proto field is additive; functionally it only blocks US4's wiring.)
- **Foundational (Phase 2)**: Start after T001 is safe to merge (independent of proto). BLOCKS all user-story backend/frontend work that touches the queue.
- **US1 & US2 (Phase 3, MVP)**: Depends on Phase 2 (the TurnLoop) — frontend enable + verification.
- **US3 (Phase 4)**: Depends on Phase 2 (builds `combineAll` on the TurnLoop + generalizes `llm.ts`).
- **US4 (Phase 5)**: Depends on T001 (proto) and Phase 2 (emission source).
- **Polish & Large Test (Phase 6)**: Depends on Phases 2-5 being complete.

### User Story Dependencies

- **US1 & US2 (P1, MVP)**: Depends on Phase 2 only. (US1 and US2 are co-dependent — implemented together.)
- **US3 (P2)**: Depends on Phase 2; independent of US1/US2 frontend.
- **US4 (P3)**: Depends on T001 (proto) + Phase 2; independent of US3.

### Within Each User Story

- Backend mechanism (Phase 2) before frontend enable (Phase 3).
- Single-message path (MVP) before multi-message combine (US3).
- Optimistic indicator (MVP) before backend-driven `QueueSignal` (US4).

### Parallel Opportunities

- T002 (turn-loop) is independently startable.
- T005 (ChatView input enable) ‖ T008 (llm multi-part) ‖ T010 (QueueSignal emission) — different files, no mutual dependency.
- US3 (Phase 4) and US4 (Phase 5) can proceed in parallel once Phase 2 is done (different concerns: combine vs visibility).
- T013 (cross-cutting verification) can run alongside T014 (large-test authoring).

---

## Parallel Example: After Phase 2

```bash
# These touch different files and have no mutual dependency:
Task: "T005 [US1] Remove disabled={processing} in ChatView.svelte"
Task: "T008 [US3] Generalize llm.ts content blocks for multi-part input"
Task: "T010 [US4] Emit QueueSignal from turn-loop.ts"
```

---

## Implementation Strategy

### MVP First (US1 & US2 only)

1. Complete Phase 1 (proto) and Phase 2 (TurnLoop backbone).
2. Complete Phase 3 (US1 & US2) — frontend input enable + single-message auto hand-off.
3. **STOP and VALIDATE**: run quickstart Scenarios 1-2.
4. Demo/deploy if ready — the core queue value is delivered.

### Incremental Delivery

1. Phase 1 + Phase 2 → backbone ready (unit-tested).
2. + Phase 3 (US1 & US2) → MVP (single-message queue, test Scenarios 1-2).
3. + Phase 4 (US3) → multi-message combine (Scenario 3).
4. + Phase 5 (US4) → backend-driven visibility + remove (Scenario 4).
5. + Phase 6 → cross-cutting hardening + large-test acceptance (Scenarios 5-7, all green).

### Notes

- Unit/contract tests are part of each implementation task (Constitution §IV) — run `bazel test` within the task; do not file separate unit-test tasks.
- The large test (T015) is the Constitution §VI acceptance gate — it MUST be executed via `guitar run`, not just built.
- Retry strategy for FR-015 (auto-with-backoff vs manual retry affordance) is intentionally left as an implementation choice within T013's scope; the spec only fixes "retain + surface error".

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks.
- [Story] label maps a task to its user story for traceability.
- US1 and US2 share a phase because they are co-equal P1 and only deliver value together (spec).
- Commit after each task or logical group; stop at any checkpoint to validate independently.
