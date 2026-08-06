# Tasks: Queued Input Mid-Turn Injection & Bubble Continuity

**Input**: Design documents from `specs/038-queue-input-mid-turn/`

**Prerequisites**: plan.md, spec.md (v2 — 2026-08-06 revision), research.md, data-model.md, contracts/, quickstart.md

**Tests**: Unit tests are part of each code-change task (Constitution IV — not separately assigned). Large test is a separate task (Constitution IV — MAY be assigned as acceptance; Constitution VI — MUST execute via testplan skill, all cases pass).

**Organization**: Tasks grouped by user story. US3 (fallback) requires no code change — covered by the turn-end drain unit tests in `projects/game/agent/src/turn-loop.test.ts` (the FR-004 fallback code path; the large test covers only the FR-001 tool-turn mid-turn injection). Spec v2 defines the "mid-turn delivery point" as the turn's first reasoning step + each reasoning step following a tool-result boundary (FR-001/FR-004); the `queueDrain` middleware fires before every model call, covering both.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2)
- Include exact file paths in descriptions

---

## Phase 1: Foundational (Blocking Prerequisite)

**Purpose**: The `TurnLoop.drainQueue()` method is the blocking prerequisite for US1 (the middleware calls it) and the large test (it verifies the behavior). MUST complete before Phase 2.

### 文档清单

- **代码规范文档**: `style/javascript.md`（TS 规范 + 测试约定：`vitest_test` 宏、DI/mock 约定）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` §引用 的基准规范）；`specs/019-js-test-reliability/`（`style/javascript.md` 引用的测试执行模型背景：`run-vitest-shim` 退出码契约，仅背景参考，非必读）
- **官方文档**: 无（`drainQueue` 复用同文件已有的 `combineAll` 私有函数与 `queueSignalFrame` 方法；API 行为由 `specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md` 定义，属 spec 相关文件自动必读）
- **技术文章**: 无

### Implementation

- [X] T001 Add `drainQueue(): TurnContent | null` public method to the `TurnLoop` class in `projects/game/agent/src/turn-loop.ts`. The method: if `this.buffer.length === 0` → return `null` (no-op, no emission); otherwise → call existing `combineAll(this.buffer)` to merge + clear the buffer, emit `this.queueSignalFrame(0)`, return the combined `TurnContent`. MUST NOT change `this.running` or call any finish method — it only touches the buffer + emits the depth signal. Add unit tests in `projects/game/agent/src/turn-loop.test.ts`: (a) `drainQueue()` on empty buffer returns `null` and emits no `QueueSignal`; (b) `drainQueue()` on non-empty buffer returns combined content, emits `QueueSignal(0)`, and buffer is cleared; (c) after a `drainQueue()` call, the turn-end drain (`runLoop` buffer check at `turn-loop.ts:356`) sees an empty buffer — no double-drain. Verify with `bazel test //projects/game/agent/src:lib_test` (or the package's `vitest_test` target).

**Checkpoint**: `drainQueue()` is functional and unit-tested. US1 implementation can begin.

---

## Phase 2: User Story 1 — Mid-Turn Injection at the Earliest Mid-Turn Boundary (Priority: P1) 🎯 MVP

**Goal**: A message queued during the turn is delivered at the earliest mid-turn delivery point — the turn's first reasoning step (messages queued before the first reasoning step) or the reasoning step immediately following a tool-result boundary (messages queued during tool execution) — not delayed until the full turn completes (FR-001, spec v2).

**Independent Test**: Start an agent turn with tool calls. Queue a message during tool execution. Verify the agent's next reasoning step after the tool result addresses the queued message (before the turn ends). See `specs/038-queue-input-mid-turn/quickstart.md` Scenario 1.

### 文档清单

- **代码规范文档**: `style/javascript.md`（TS 规范 + 测试约定）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: 无（`beforeModel` 中间件 API 由 `specs/038-queue-input-mid-turn/contracts/injection-seam-contract.md` 精确定义，属 spec 相关文件自动必读；langchain 类型定义位于 `projects/game/agent/node_modules/langchain/dist/agents/middleware/types.d.ts` 供参考：`BeforeModelHandler` 签名 `(state, runtime) => MiddlewareResult`、`Runtime.configurable` 访问、`MiddlewareResult` 返回 `{ messages: [...] }` 语义）
- **技术文章**: 无

### Implementation

- [X] T002 [US1] Add a `queueDrain` `beforeModel` middleware to the player's `createAgent` middleware array in `projects/game/agent/src/team/player.ts`. The middleware is added as a second entry alongside the existing `gameEndGuard` (lines 157-169). The `beforeModel.hook` signature is `(_state, runtime) => ...`: read `const drain = runtime.configurable?.drainQueuedInput`; if `typeof drain !== "function"` return (no-op); call `const drained = drain()`; if `!drained` return (empty buffer — no-op); return `{ messages: [new HumanMessage({ content: buildContentBlocks(drained) })] }`. Add imports: `HumanMessage` from `@langchain/core/messages` (alongside existing `SystemMessage` import on line 39), `buildContentBlocks` and type `TurnContent` from `../llm` (new import after line 44). The hook fires before **every** model call within the `createAgent` loop — the turn's first model call AND each model call following a tool result — covering both FR-001 delivery points (spec v2). Add a unit test verifying the middleware returns `{ messages: [HumanMessage] }` when the drain callback returns content, and returns `undefined` when the callback returns `null` or is absent. Verify with `bazel test`.

- [X] T003 [P] [US1] Wire the `drainQueuedInput` callback into the `streamEvents` `configurable` object in `SessionTeam.runTeamTurn` in `projects/game/agent/src/session-team.ts`. Add `drainQueuedInput: () => this.turnLoop?.drainQueue() ?? null` to the `configurable` object (lines 301-328, alongside the existing `thread_id` and `emitChannelFrame`). No new imports needed — `this.turnLoop` is already a field on `SessionTeam` and `drainQueue()` is added in T001. Verify with `bazel build //projects/game/agent/...`.

**Checkpoint**: US1 is functional. A message queued during tool execution is drained by the `queueDrain` middleware and injected as a `HumanMessage` before the agent's next model call; a message queued before the first reasoning step is injected into the turn's first model call. The `QueueSignal(0)` emission transitions the pending message to normal display mid-turn.

---

## Phase 3: User Story 2 — Agent Bubble Stays Continuous (Priority: P1)

**Goal**: Submitting a user message during the agent's streaming text/thinking output does NOT split the agent's bubble into two (FR-005/FR-006). The merge logic is extracted into a testable pure module with unit tests (Constitution IV).

**Independent Test**: Start an agent turn producing streaming thinking. Submit a queued message mid-stream. Verify the thinking content is ONE continuous bubble with the user message below it. See `specs/038-queue-input-mid-turn/quickstart.md` Scenario 2.

### 文档清单

- **代码规范文档**: `style/javascript.md`（TS 规范 + `vitest_test` 测试约定）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: 无（Svelte 5 组件沿用既有模式；纯函数抽取复用 `src/chat-fifo.ts` 先例）
- **技术文章**: 无

### Implementation

- [X] T004 [US2] Extract the streaming merge logic in `handleMessageParts` (`projects/game/desktop/frontend/src/App.svelte:727-745`) into a NEW pure module `projects/game/desktop/frontend/src/stream-merge.ts` (precedent: `chat-fifo.ts` — pure functions, no Svelte imports). Define and export a structural `StreamEntry` interface (fields: `role: MessageRole; agent: string; mergeKind?: 'text' | 'thinking' | 'mixed'; parts: { text?: { content: string }; thinking?: { content: string } }[]` — structurally compatible with App.svelte's `ChatEntry` at line 68; import `MessageRole` from `./api` where it is exported at `src/api.ts:68`). Export `findMergeTarget(list: StreamEntry[], agent: string, kind: 'text' | 'thinking'): number | null` — backward scan: iterate `i` from `list.length - 1` downto `0`; at `list[i]`, if `role === MessageRole.AGENT && entry.agent === agent && entry.mergeKind === kind && entry.parts && entry.parts.length > 0` → return `i`; if `role !== MessageRole.USER` → break (return `null`); otherwise continue backward (skip USER entries). Export `appendToEntry(list, index, incomingParts, kind): StreamEntry[]` — returns a NEW array with the trailing part's `.text.content` (kind `'text'`) or `.thinking.content` (kind `'thinking'`) concatenated (join incoming part content as in current lines 733-741), then `trimFifo([...list])`. Rewire `handleMessageParts` in `App.svelte`: replace the single-entry `const last = list[list.length - 1]` check with `findMergeTarget(list, agent, kind)`; on hit, call `appendToEntry` and update `chatMessages`; on `null`, fall through to the existing new-entry creation (lines 748-758). No BUILD.bazel change — the existing `lib_test` vitest target globs `src/**/*.ts` (`BUILD.bazel:23-30`). Verify with `bazel build //projects/game/desktop/...`.

- [X] T005 [US2] Add unit tests in `projects/game/desktop/frontend/src/stream-merge.test.ts` (precedent: `chat-fifo.test.ts`; runs via the existing `lib_test` target): (a) agent text chunk merges into the trailing same-agent text entry (no USER interleaved); (b) agent thinking chunk after a USER entry merges PAST the USER into the earlier agent thinking entry — the defect case (bubble NOT split); (c) backward scan skips multiple consecutive USER entries and still merges; (d) chain broken by a non-USER non-matching entry (e.g. AGENT with a different `mergeKind`, like a warn entry) → returns `null` (new entry); (e) no matching agent entry → `null`; (f) an agent entry with `parts.length === 0` is skipped by the scan; (g) `appendToEntry` concatenates text/thinking content correctly and returns a new array (original untouched). Verify with `bazel test //projects/game/desktop/frontend:lib_test`.

**Checkpoint**: US2 is functional and unit-tested. The agent's streaming text/thinking bubble stays continuous when a user message is queued mid-stream (end-to-end visual verification via quickstart.md Scenario 2).

---

## Phase 4: Polish & Large Test Acceptance

**Purpose**: End-to-end validation (Constitution VI) and documentation amendment.

### 文档清单

- **代码规范文档**: `style/large_test.md`（大型测试规范：测试计划 YAML 结构、`guitar` 执行、部署→测试→清理闭环）；`style/golang.md`（大型测试代码使用 golang 编写，**必须遵守**其单元测试规范 — `style/large_test.md` §测试用例 显式要求）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` §引用 的规范基准）
- **官方文档**: 无
- **技术文章**: 无

### Implementation

- [X] T006 Add the mid-turn injection large test `TestAgentDialogQueueMidTurnInjection` to the EXISTING queue tests in `projects/game/testplan/agent_dialog_test.go` (module file exists — no new test file, per `style/large_test.md` 反模式 #1/#2; appended to the queue-while-running section, following the helpers `frameQueueSignal` (`helpers_test.go:993`) and `queueSignalDepths` (`helpers_test.go:1036`)). The test drives a real saolei_init tool dispatch (same trigger as `TestAgentOperationDispatchLoopSuccess`), submits "watch out for mines" while the tool is in-flight (blocked on the desktop reply), then asserts mid-turn delivery at the tool-result boundary (specs/038-queue-input-mid-turn/spec.md FR-001): `QueueSignal` depth sequence [1, 0] with the depth-0 signal BEFORE the terminal `wait` (drain at the beforeModel boundary, not turn end — specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md emission table), exactly ONE terminal wait (no second turn), no saolei_click dispatch after the init (the injected HumanMessage became the last message of the next model call), and the queued message persisted in ListMessages history (injection-seam-contract.md §3). DELETED the 3 pre-existing no-tool queue-while-running large tests — `TestAgentDialogQueueAutoHandoff`, `TestAgentDialogQueueMultipleCombine`, `TestAgentDialogQueueInputDoesNotDisturb` (plus the `collectTextContents` helper, which only they used) — whose FR-004 no-tool fallback / spec 030 handoff / combine / does-not-disturb end-to-end timing is inherently unreliable: fake-llm has no delay mechanism and a no-tool turn responds in milliseconds, so the fallback window between the first reasoning step and the turn-end drain cannot be synchronized end-to-end (designer evaluation — the T006a drain-thinking-frame timing did not make them reliable). Those behaviors are deterministically covered by the gate-controlled unit tests in `projects/game/agent/src/turn-loop.test.ts` (turn-loop.test.ts:242/398/526/665), which hold the turn in-flight via a gate and drain through the SAME turn-end `runLoop` code path the FR-004 fallback uses. Updated the queue-while-running section comment in `agent_dialog_test.go` and the `agent-queue` suite description in `projects/game/testplan/system_test.yaml` accordingly (the large-test layer now covers ONLY the FR-001 tool-turn mid-turn injection). Verify with `bazel build //projects/game/testplan:agent_dialog_test`; acceptance via the testplan skill: `guitar run <plan.yaml>` (`projects/game/testplan/system_test.yaml`, `agent-queue` suite) — full deploy→test→cleanup loop; ALL cases MUST pass (Constitution VI: any failed or flaky case = acceptance not met, fix and re-run until green).

- [X] T007 [P] Documentation amendment: (a) update `specs/030-queued-chat-input/contracts/turn-loop-contract.md`: add a `drainQueue` row to the Methods table; add a note to the loop body that `drainQueue` may be called mid-turn by the `beforeModel` middleware, clearing the buffer before the turn-end check; (b) update `specs/030-queued-chat-input/contracts/queue-channel-contract.md` §2: add a row to the emission-rules table for "Mid-turn `drainQueue` clears the buffer → `QueueSignal(0)`"; (c) verify the spec 030 supersession notes (FR-013 + the "next agent turn" assumption, both carrying `[SUPERSEDED — see Feature 038]`) are present in `specs/030-queued-chat-input/spec.md` (added during the spec v2 revision) and cross-reference `specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md` as the authoritative contract; (d) align `specs/038-queue-input-mid-turn/data-model.md` §5 merge pseudocode with the implemented condition (add `parts.length > 0` to the scan condition) and reference `projects/game/desktop/frontend/src/stream-merge.ts` as the implementation location.

**Checkpoint**: All acceptance criteria validated. Large test passes end-to-end (all cases green via `guitar run`).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Foundational)**: No dependencies — start immediately. Blocks Phase 2.
- **Phase 2 (US1)**: Depends on Phase 1 (T001 `drainQueue` must exist). T002 and T003 are independent of each other (different files) — can run in parallel.
- **Phase 3 (US2)**: No dependency on Phase 1 or 2 — can run in parallel with Phase 2 (different codebase: frontend vs agent). T005 depends on T004 (it tests the extracted module).
- **Phase 4 (Polish)**: T006 depends on Phases 1+2 (the feature must be implemented to test it; agent-side only — does not require Phase 3). T007 depends on nothing — can run in parallel with T006.

### User Story Dependencies

- **US1 (P1)**: Depends on T001 (Foundational). T002 + T003 within US1 are parallelizable.
- **US2 (P1)**: No dependencies — fully independent of US1.
- **US3 (P2)**: No code change. Covered by the gate-controlled unit tests in `projects/game/agent/src/turn-loop.test.ts` (turn-loop.test.ts:242/398/526/665), which hold the running window via a gate and drain through the turn-end `runLoop` code path — the exact FR-004 fallback path. NOT covered by a large test: fake-llm has no delay mechanism and a no-tool turn responds in milliseconds, so the no-tool fallback window cannot be synchronized end-to-end (designer evaluation — the 3 no-tool queue-while-running large tests were deleted in T006).

### Parallel Opportunities

- T002 and T003 (Phase 2) can run in parallel — different files (`player.ts` vs `session-team.ts`), no dependency on each other.
- Phase 3 (US2, T004 + T005) can run in parallel with Phase 2 (US1, T002 + T003) — frontend vs agent, no file conflicts.
- T006 and T007 (Phase 4) can run in parallel — test code vs documentation.

---

## Implementation Strategy

### MVP First (US1 + US2)

1. Complete Phase 1: Foundational (T001 — `drainQueue`).
2. Complete Phase 2: US1 (T002 + T003 — middleware + wiring).
3. Complete Phase 3: US2 (T004 + T005 — merge-module extraction + unit tests). Can be done in parallel with Phase 2.
4. **STOP and VALIDATE**: Test US1 (queue a message during tool execution — the agent addresses it mid-turn; rapid double-submit reaches the first model call) and US2 (queue during streaming — bubble stays continuous).
5. Complete Phase 4: Polish (T006 — large test acceptance, T007 — contract docs).

### Incremental Delivery

1. T001 → `drainQueue` ready (can be unit-tested in isolation).
2. T002 + T003 → US1 functional (mid-turn injection works end-to-end).
3. T004 + T005 → US2 functional + unit-tested (bubble continuity fixed).
4. T006 → Large test acceptance gate passed (Constitution VI).
5. T007 → Spec 030 contracts amended + data-model aligned (documentation complete).

---

## Notes

- US3 (fallback for no-tool turns) requires no code change — it is the existing turn-end drain behavior retained as-is. It is NOT large-tested: fake-llm has no delay mechanism and a no-tool turn responds in milliseconds, so the FR-004 fallback window between the first reasoning step and the turn-end drain cannot be synchronized end-to-end (designer evaluation). The behavior is deterministically covered by the gate-controlled unit tests in `projects/game/agent/src/turn-loop.test.ts` (turn-loop.test.ts:242/398/526/665), which drain through the same turn-end `runLoop` code path.
- Unit tests are part of each code-change task (Constitution IV — `bazel build` + `bazel test` on each change, not separately assigned). T005 restores unit-test coverage for the frontend merge logic.
- The large test (T006) is the acceptance gate per Constitution VI — MUST execute via the testplan skill (`guitar run <plan.yaml>`), all cases MUST pass (any failed or flaky case = acceptance not met).
- No new BUILD.bazel targets needed: the frontend `lib_test` vitest target globs `src/**/*.ts` (new `stream-merge.ts` / `stream-merge.test.ts` are auto-covered); large-test cases append to the existing `agent_dialog_test.go` (no gazelle run needed).
- The `configurable.drainQueuedInput` callback type is `(() => TurnContent | null) | undefined` — accessed via `runtime.configurable?.drainQueuedInput` with a `typeof !== "function"` guard (the configurable type is `{ [key: string]: unknown }`).
- Per-phase 文档清单 follow Constitution V (three mandatory categories; "无" stated explicitly where a category has no documents).
