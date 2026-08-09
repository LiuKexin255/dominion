# Tasks: Real-Time Init Instruction Delivery

**Input**: Design documents from `/specs/041-realtime-init-push/` — [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md), [quickstart.md](./quickstart.md).

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/.

**Tests**: Per Constitution Principle IV (`.specify/memory/constitution.md`), compile + unit tests are **part of every implementation task** (not separate tasks) — each implementation task below includes updating/adding the corresponding unit test and running `bazel build` + `bazel test` on the relevant target. Per Principle VI, the large test is a separate acceptance task (Phase 6).

**文档清单约定**: 本仓库内设计文档（plan.md、research.md、data-model.md、contracts/、quickstart.md）本属"spec 相关文件"（宪法 V：无需重复列出即为必读），但为满足"不做引用传递 / 规划即阅读"并精确到 section 级阅读范围，仍按惯例显式列于各 phase 的「技术文章」分类下（该分类名义上为仓库外资料，此处为展示位置约定）；「代码规范文档」只列仓库内 `style/` 规范及其引用的外部规范，「官方文档」只列仓库外完整 URL。

**Organization**: Tasks grouped by user story. US2 (typing indicator) and US3 (continuous channel) are largely delivered by the Foundational phase + US1; their phases are primarily verification/regression (the `isRunning`/`isBusy` split and the SSE receive pipeline are already implemented in the working tree). Frontend (`projects/game/desktop/frontend/`) and proto (`projects/game/game.proto`) are intentionally **unchanged** — see plan.md.

**Test-framework rules** (mandatory):
- Agent TS tests: `style/javascript.md` — DI + `vi.fn()` test-doubles (no new module-level `vi.mock`); `vitest_test` macro in BUILD.bazel; assert mocks are exercised.
- Desktop Go tests: `style/golang.md` — table-driven, `given/when/then`, no assertions inside cases, `TestFuncName`/`Test_funcName` naming; the existing `app_test.go` `recvLoop` test pattern (`TestRecvLoop_AppendsToChatStream`, `app_test.go:517-602`) is the model.
- Large tests: `style/large_test.md` — add cases to the **existing** module file + plan; do NOT create per-requirement files/plans.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 (user-story phases only)
- Exact file paths in every description

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: establish the feature branch from the 040 branch base (HEAD `868c49b`), which already contains the prerequisite `isRunning`/`isBusy` split (spec Assumption line 106) and the 039/040 changes this feature builds on.

**Documents to read before this phase**:
- 代码规范文档: 无
- 官方文档: 无
- 技术文章: [plan.md](./plan.md) (Technical Context, Project Structure); [research.md](./research.md) ("Context: why the init instruction is invisible today")

- [ ] T001 Create/switch to feature branch `041-realtime-init-push` from the current branch base (`040-team-singleton-conformance`, HEAD `868c49b`, which already contains the committed `isRunning`/`isBusy` split in `projects/game/agent/src/session-team.ts` + `handler.ts` and the 039/040 baseline). Verify the base compiles: `bazel build //projects/game/agent:lib //projects/game/desktop:desktop_lib` and the existing tests pass: `bazel test //projects/game/agent:lib_test //projects/game/desktop:desktop_test`. If the split is not present, stop and surface the gap (it is a hard prerequisite for US2).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the two shared mechanisms that BOTH US1 (init visible) and US3 (continuous channel) depend on — the agent-side stream display sink (research.md D1) and the desktop-side continuous reader (research.md D4). These are refactorings over the existing per-turn model (Constitution Principle II).

**⚠️ CRITICAL**: No user-story work can begin until T002–T004 are complete.

### Documents to read before this phase

- **代码规范文档**:
  - Agent (TS): `style/javascript.md`; [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
  - Desktop (Go): `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/guide); [Go Style Decisions](https://google.github.io/styleguide/go/decisions); [Go Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**:
  - Agent (TS): LangGraph.js — runtime values via `configurable`/context + `thread_id` scoping: [Tools](https://docs.langchain.com/oss/javascript/langchain/tools); persistence / checkpointer + `invoke`: [Persistence](https://docs.langchain.com/oss/javascript/langgraph/persistence); vitest — module-mocking pitfalls（DI 优于模块级 `vi.mock` 的官方依据）: [vitest Mocking Modules](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
  - Desktop (Go): 无（nhooyr.io/websocket 与 goroutine/channel 均为既有内部用法，已由 `style/golang.md` + Google Go Style 覆盖）
- **技术文章**:
  - Agent: [research.md](./research.md) D1; [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md) §1 (sink lifecycle)
  - Desktop: [research.md](./research.md) D4; [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md) §3 (continuous reader); [`specs/030-queued-chat-input/contracts/turn-loop-contract.md`](../030-queued-chat-input/contracts/turn-loop-contract.md) (existing `wait`/queue signal semantics the reader must preserve)

### Tasks

- [ ] T002 [P] Add the stream-bound display sink to `SessionTeam` in `projects/game/agent/src/session-team.ts`: replace the `turnLoopEmit`-only-at-`submit` model with a `streamSink: ((frame: TeamFrame) => void) | null` field + `bindStreamSink(sink, handle)` / `clearStreamSink(handle)` (compare-and-delete on `handle`). Refactor the `emitChannelFrame` closure (current `session-team.ts:530-555`) and `submit` (current `session-team.ts:378-390`) to read `this.streamSink?.(frame)` live (closure over `this`, so rebind/clear reflects on next emit). The sink is injected (DI seam) so tests pass a `vi.fn()` per `style/javascript.md`. Update `projects/game/agent/src/session-team.test.ts`: assert emit-before-bind and after-clear are no-ops; emit-while-bound writes through; compare-and-delete does not clear a newer handle. Run `bazel test //projects/game/agent:lib_test`.
- [ ] T003 Bind/clear the display sink in the `Connect` handler in `projects/game/agent/src/handler.ts`: on the first inbound frame for a session (the status probe arrives first — current `handler.ts:325-405`), call `team.bindStreamSink((frame) => safeWrite(stream, frame, sessionId), handle)` (track bound sessions in a per-stream set, mirroring `sessionSinkHandles`/`activeLoopSessions` at `handler.ts:291-323`); in the existing `cleanupSinks`/stream-`end`/`error` paths (`handler.ts:292-305`, `507-522`), call `team.clearStreamSink(handle)` (FR-010). Update `projects/game/agent/src/handler.test.ts`: assert the sink is bound on first per-session frame and cleared on stream end. Run `bazel test //projects/game/agent:lib_test`. (Depends on T002.)
- [ ] T004 [P] Replace the per-turn `recvLoop` with a continuous reader in `projects/game/desktop/app.go`: rename/repurpose `recvLoop` (`app.go:643-717`) into a continuous `readLoop` started at the end of `Connect` (`app.go:1629-1754`) **after** the one-shot probe `RecvFrame` (`app.go:1709`); remove the `wait`-terminates branch (`app.go:707-713`) so `wait` is forwarded to the chatstream but the reader continues (FR-008); remove the `recvMu`/`recvDone` check-and-start block from `SendUserTurn` (`app.go:605-621`) — `SendUserTurn` only sends (FR-012); in `Connect`, before starting a new reader, wait on the prior `recvDone` (under `recvMu`) for reconnect handover; keep the synthesized-`wait`-on-`RecvFrame`-error (`app.go:655-665`) and the `CloseAgent` wait-on-`recvDone` (`app.go:1791-1796`). Update `projects/game/desktop/app_test.go` (imitate the `recvLoop` test pattern at `app_test.go:517-602`): assert background `messageParts` received with no user turn; `wait` does not terminate; `SendUserTurn` starts no second reader; reconnect handover; clean close. Run `bazel test //projects/game/desktop:desktop_test`.

**Checkpoint**: Foundation ready — agent has a stream sink bound at `Connect`; desktop has a continuous reader. User-story work can begin.

---

## Phase 3: User Story 1 — Init Instruction Visible on First Entry (Priority: P1) 🎯 MVP

**Goal**: the planner's init instruction is delivered in real-time to both the planner tab and the player tab through the connection, without leaving/re-entering the session (FR-001, FR-004, FR-005, FR-006; SC-001).

**Independent Test**: with a bound stream sink (Phase 2) and a running init turn, assert the agent emits exactly three frames — planner request (`agent=planner`, `role=USER`, text), planner response (`agent=planner`, `role=AGENT`, `toolCall`), player write-back (`agent=player`, `role=USER`, text) — each `frameId == <message id>`, and degrade (planner failure) emits no frame ([quickstart.md](./quickstart.md) A1).

### Documents to read before this phase

- **代码规范文档**: `style/javascript.md`; [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: LangGraph.js — runtime values via `configurable`/context + `invoke` returning `{ messages }`: [Tools](https://docs.langchain.com/oss/javascript/langchain/tools); persistence/`thread_id`: [Persistence](https://docs.langchain.com/oss/javascript/langgraph/persistence); vitest — module-mocking pitfalls（DI 优于模块级 `vi.mock`）: [vitest Mocking Modules](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- **技术文章**: [research.md](./research.md) D1 (sink bound at Connect), D2 (install emitter in `runInitTurn`), D3 (faithful message→frame mirroring), D9 (best-effort/degrade); [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md) §2 (init frame shapes); [data-model.md](./data-model.md) §3 (frame model), §4 (dedup anchor); [quickstart.md](./quickstart.md) §A (A1 unit checks — phase Independent Test 依据)

### Implementation for User Story 1

- [ ] T005 [US1] Install `emitChannelFrame` in `runInitTurn`'s `configurable` in `projects/game/agent/src/session-team.ts` (current `session-team.ts:342-368`), bound to `this.streamSink` (the closure from T002). Remove/replace the "NO `emitChannelFrame` is installed here" scope note at `session-team.ts:322-337` with the new rationale (sink bound at `Connect`, init best-effort — research.md D1/D9). Update `projects/game/agent/src/session-team.test.ts`: with a bound sink + a fake planner producing an instruction, assert frames flow through the sink during the init turn; with sink unbound, emit is a no-op (degrade). Run `bazel test //projects/game/agent:lib_test`. (Depends on T002.)
- [ ] T006 [US1] Emit the init instruction frames from `projects/game/agent/src/team/instruction-node.ts` (current `instruction-node.ts:149-229`) after the planner invoke resolves: emit (a) the planner request (already emitted at `instruction-node.ts:164-178` when emitter present — keep), (b) the planner response as a `toolCall` `MessagePart` (faithful mirroring, research.md D3), (c) the player write-back as a `text` `MessagePart`. Each frame's `frameId` MUST equal the producing message's id (`HumanMessage.id` / `AIMessage.id`) for dedup (FR-004, data-model §4). Extend the emission type (`ChannelFrameEmitter`, `session-team.ts:157-162`) to accept pre-built `MessagePart[]` (clean generalization, Principle II) and reuse the TurnLoop's message→part logic (`projects/game/agent/src/turn-loop.ts:444-496`) for the `toolCall` part. Update `projects/game/agent/src/team/instruction-node.test.ts`: assert the three frames are emitted with correct `agent`/`role`/`frameId`; planner-failure degrade emits no response/write-back frame. Run `bazel test //projects/game/agent:lib_test`. (Depends on T005.)

**Checkpoint**: US1 complete — on first entry the init instruction appears in both tabs in real-time; re-entry shows it from history with no duplicate.

---

## Phase 4: User Story 2 — Typing Indicator Reflects Actual State (Priority: P2)

**Goal**: the typing indicator is OFF on entry when only the background init is in flight, and is driven only by real user turns (FR-003, FR-007; SC-002, SC-005).

**Independent Test**: status probe returns `IDLE` when only the init is in flight; `RefreshTeam`/profile-change rebuild return `FAILED_PRECONDITION` during the init; the continuous reader forwards `wait` (clears `processing`) but background `messageParts` do not drive a "typing" state.

**Note**: US2 requires **no new production code** — the `isRunning`/`isBusy` split is already implemented (`session-team.ts:392-427`, `handler.ts:366-395`) and the continuous reader's `wait`-forwarding is delivered by T004 (FR-008). This phase is verification/regression to prevent regressions.

### Documents to read before this phase

- **代码规范文档**: `style/javascript.md`; [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html); `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/guide); [Go Style Decisions](https://google.github.io/styleguide/go/decisions); [Go Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**: 无（`status-signal.ts`/probe 为本仓库内部既有实现）
- **技术文章**: [research.md](./research.md) D6 (typing indicator unchanged); [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md) §5 (unchanged behaviors); [`specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md`](../021-agent-session-resync/contracts/agent-desktop-channel-contract.md) §1 (status probe contract)

### Implementation for User Story 2

- [ ] T007 [US2] Add/strengthen regression tests for the typing-indicator contract (FR-003/FR-007). In `projects/game/agent/src/session-team.test.ts` and `projects/game/agent/src/handler.test.ts`: assert `isRunning()` excludes `initInFlight` and `isBusy()` includes it; the Connect status probe responds `STATUS_SIGNAL_STATUS_IDLE` when only the init is in flight; `RefreshTeam` and profile-change rebuild return `FAILED_PRECONDITION` while `isBusy()` (these tests largely exist at `session-team.test.ts:884-908` and `handler.test.ts:894-925` — verify they still pass under the Phase 2 refactor; add cases if gaps exist); assert the `UpdateTeam` RPC returns before the init turn completes (fire-and-forget retained — FR-005). In `projects/game/desktop/app_test.go`: assert the continuous reader (T004) forwards a `wait` to the chatstream (so the frontend clears `processing`) and does NOT synthesize any "typing"/ACTIVE signal for background `messageParts`. Run `bazel test //projects/game/agent:lib_test //projects/game/desktop:desktop_test`. (Depends on T002, T004.)

**Checkpoint**: US2 complete — the typing indicator stays correct end-to-end under the new continuous-reader + sink model.

---

## Phase 5: User Story 3 — Continuous Real-Time Channel (Priority: P3)

**Goal**: background agent activity reaches the desktop through the real-time channel without user interaction; the continuous reader is the sole reader and terminates cleanly on close (FR-002, FR-008, FR-009, FR-010, FR-011, FR-012; SC-003, SC-004).

**Independent Test**: with no user turn submitted, background frames arrive through the connection; a full user turn (text + operation + terminal `wait`) is handled by the same continuous reader with no regression; reconnect/close release resources cleanly (quickstart.md B3, B6, B7).

**Note**: US3's core mechanism (continuous reader + stream sink) is delivered by Phase 2; US1 (Phase 3) is the first concrete background producer. This phase verifies the general continuous-delivery behaviour + lifecycle edge cases.

### Documents to read before this phase

- **代码规范文档**: `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/guide); [Go Style Decisions](https://google.github.io/styleguide/go/decisions); [Go Best Practices](https://google.github.io/styleguide/go/best-practices); `style/javascript.md`; [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: 无
- **技术文章**: [research.md](./research.md) D1 (sink bound at Connect — T009 生命周期边界依据), D4 (continuous reader), D7 (dedup risk analysis); [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md) §1 (sink lifecycle), §3 (reader), §4 (dedup); [quickstart.md](./quickstart.md) §B (B3/B6/B7 — phase Independent Test 依据)

### Implementation for User Story 3

- [ ] T008 [P] [US3] Add desktop unit tests in `projects/game/desktop/app_test.go` for the continuous-reader general behaviours and lifecycle edge cases (FR-002/FR-009/FR-010/FR-011/FR-012): (a) a full user turn's frames — text, tool calls, an operation `FlowPart` (executed + `FlowResultPart` returned, not mirrored to chatstream), terminal `wait` — are all delivered by the already-running reader (SC-004 regression vs the previous per-turn model); (b) after `wait` the reader continues and receives a subsequent background frame; (c) on `RecvFrame` error a `wait` is synthesized and the reader exits, `recvDone` closes, `CloseAgent` returns cleanly (FR-010). Follow the existing `app_test.go` httptest+websocket pattern. Run `bazel test //projects/game/desktop:desktop_test`. (Depends on T004.)
- [ ] T009 [P] [US3] Add agent unit tests in `projects/game/agent/src/handler.test.ts` and `projects/game/agent/src/session-team.test.ts` for the sink-lifecycle edge cases of a continuous channel (FR-010): `clearStreamSink` on stream `end`/`error` prevents further writes (the in-flight init turn emits to `null` → no write to a dead connection); reconnect binds a fresh sink and a still-in-flight init pushes to the new connection (compare-and-delete keeps the newer sink); two concurrent background producers (init emitter + a compress/review-style emitter) interleaving through the same bound sink each deliver frames tagged with their own agent, no frame is lost (spec edge case 4). Run `bazel test //projects/game/agent:lib_test`. (Depends on T002, T003.)

**Checkpoint**: All user stories independently functional — US1 (init visible), US2 (typing correct), US3 (continuous channel) verified.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: mandatory large-test acceptance (Constitution Principle VI) + final regression.

**Note on scope**: the large test (T010/T011) covers the **service** (agent + gateway) — the agent-side stream sink + init emission over the real `Connect` stream. The desktop continuous reader is a client component (not deployed) and is covered by Go unit tests (T004/T007/T008), per Principle IV.

### Documents to read before this phase

- **代码规范文档**: `style/large_test.md` (test organization rules — by module, no per-requirement plan); `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/guide); [Go Style Decisions](https://google.github.io/styleguide/go/decisions); [Go Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**: 无
- **技术文章**: [quickstart.md](./quickstart.md) §A (A1/A2 unit checks — T012 执行依据), §B (large-test scenarios B1–B7); [research.md](./research.md); [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md)

### Tasks

- [ ] T010 Add large-test cases to the **existing** Connect-lifecycle module file `projects/game/testplan/saolei_team_test.go` (which already holds `TestTeamConnectLifecycle`, `TestTeamConnectExclusiveEmit`) covering real-time init delivery over the real `Connect` stream: (a) after `UpdateTeam` + `Connect` with no user turn, the init frames arrive through the stream tagged `planner`/`player` with `frameId == messageId` (quickstart B1/B3); (b) status probe returns `IDLE` during init (B2); (c) `RefreshTeam`/profile-change rebuild return `FAILED_PRECONDITION` during init (B4); (d) re-enter the session (second `Connect`) after the init completed: the instruction appears exactly once, no duplicate rendering (quickstart B5; FR-004, US1 acceptance scenario 2). Add the case/suite to the **existing** `projects/game/testplan/system_test.yaml` (do NOT create a new test-plan YAML — `style/large_test.md` anti-pattern #1/#4). Reuse existing helpers in `helpers_test.go` (anti-pattern #3). Use `go_largetest`; keep a gazelle-default `{package_name}_test` target. Run `bazel build` on the new target (build is NOT acceptance — see T011).
- [ ] T011 Execute the large-test acceptance via the **testplan** skill: `guitar run projects/game/testplan/system_test.yaml`. Per Constitution Principle VI, this MUST run the full deploy→test→cleanup loop and **all** cases MUST pass; build-only checks do NOT constitute acceptance. On any failed/flaky case, fix and re-run until fully green.
- [ ] T012 [P] Final regression + citation cleanup: run `bazel build //...` and `bazel test //...`; run the quickstart.md unit checks (A1 agent, A2 desktop); verify all new/changed code comments cite the spec FRs + file:line per Constitution Principle I (e.g. `session-team.ts` scope notes, contract references); remove any stale "init frame could never reach the desktop" comments superseded by this feature.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1, T001)**: no dependencies — establishes branch + verifies the `isRunning`/`isBusy` prerequisite.
- **Foundational (Phase 2, T002–T004)**: depends on T001; **blocks all user stories**.
- **US1 (Phase 3, T005–T006)**: depends on T002 (stream sink).
- **US2 (Phase 4, T007)**: depends on T002 + T004.
- **US3 (Phase 5, T008–T009)**: T008 depends on T004; T009 depends on T002 + T003.
- **Polish (Phase 6, T010–T012)**: T010/T011 depend on T002, T003, T005, T006 (service-side emission complete); T012 is final.

### Task-level Dependencies

```
T001 ─┬─ T002 (agent sink) ─┬─ T003 (handler bind) ──┐
      │                      ├─ T005 (US1 emitter) ── T006 (US1 frames) ──┐
      │                      ├─ T007 (US2 regression: +T004) ─────────────┤
      │                      └─ T009 (US3 agent edges: +T003) ─────────────┤
      └─ T004 (desktop reader) ─┬─ T007 (US2: +T002) ──────────────────────┤
                                 └─ T008 (US3 desktop edges) ───────────────┤
                                                                          ├─ T010 ── T011 ── T012
```

### Parallel Opportunities

- **T002 (agent, `session-team.ts`) ∥ T004 (desktop, `app.go`)**: different repos/files, no dependency — run concurrently.
- Within US3: **T008 (desktop `app_test.go`) ∥ T009 (agent `handler.test.ts`/`session-team.test.ts`)** after their deps are met.
- T012 is `[P]` with any earlier-finished work (it is a final sweep).

### Within Each User Story

- Foundational mechanisms (Phase 2) before story-specific behaviour.
- US1: install emitter (T005) before frame content (T006).
- Unit tests are written/updated **inside** each task (Constitution Principle IV) — no separate test tasks.
- Each story checkpoint is independently verifiable per its Independent Test.

---

## Parallel Example

```bash
# After T001, launch the two foundational tracks concurrently (different files):
Task: "T002 — stream sink on SessionTeam in projects/game/agent/src/session-team.ts"
Task: "T004 — continuous reader in projects/game/desktop/app.go"

# After T002 + T003 + T004, launch US2/US3 verification concurrently (different files):
Task: "T007 — typing-indicator regression (agent + desktop tests)"
Task: "T008 — US3 desktop reader edge tests in app_test.go"
Task: "T009 — US3 agent sink-lifecycle edge tests"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. **Phase 1** (T001): branch + verify `isRunning`/`isBusy` prerequisite.
2. **Phase 2** (T002–T004): foundational sink + continuous reader.
3. **Phase 3** (T005–T006): US1 — init instruction visible on first entry.
4. **STOP and VALIDATE**: run US1's Independent Test (agent emits the 3 frames with correct `agent`/`role`/`frameId`); run `bazel test //projects/game/agent:lib_test //projects/game/desktop:desktop_test`. The init instruction is now visible on first entry — demo-ready.

### Incremental Delivery

1. Setup + Foundational → sink + continuous reader in place.
2. + US1 → init visible on first entry (MVP — the primary production bug is fixed).
3. + US2 → typing-indicator regression-locked (no behaviour change, but protected).
4. + US3 → continuous-channel edge cases verified.
5. + Phase 6 → large-test acceptance green (mandatory for service change).

### Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[Story]` maps a task to its user story for traceability.
- Unit tests are part of each implementation task (Constitution Principle IV); the large test is the Phase 6 acceptance gate (Principle VI).
- Frontend (`projects/game/desktop/frontend/src/`) and proto (`projects/game/game.proto`) are intentionally unchanged — the existing SSE→`handleMessageParts`→`renderedMessageIds` pipeline + `frame.agent` routing already satisfy the receive side (research.md "Key finding").
