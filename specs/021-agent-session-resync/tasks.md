# Tasks: Agent Session Resync & Adapter Simplification

**Input**: Design documents from `/specs/021-agent-session-resync/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required for user stories), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/)

**Tests**: Unit tests are included inline with each code task (Constitution §IV — compile + unit run as part of each code task, not separately tasked). Large tests are a dedicated acceptance phase (Constitution §VI).

**Organization**: Tasks are grouped by user story (P1 → P2) so each story can be implemented and tested independently. The agent is the service under large-test acceptance; the desktop changes are verified by unit/manual (the desktop app is not part of the testplan SUT).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks in the same phase)
- **[Story]**: Which user story this task belongs to (US1..US4); Setup/Polish/Large-test phases have NO story label
- Exact file paths are included in every description

## Conventions (apply to EVERY code task)

- Before coding, read the phase's **必读文档** list (Constitution §V) and the relevant contract section cited in the task.
- Every code task MUST run, as part of itself: `bazel build` + `bazel test` for the affected targets; after adding/removing files or deps run `bazel run //:gazelle` (and `bazel mod tidy` / `pnpm up` / `go mod` steps per `AGENTS.md` as applicable). Do not commit on red.
- Mocking convention for agent TS tests: dependency-injected `vi.fn()` test-doubles (NOT module-level `vi.mock`); assert the mock is exercised (`style/javascript.md`).
- Large tests are organized BY MODULE in existing files/suites (`style/large_test.md`); never by spec/story number.

---

## Phase 1: Setup

**Purpose**: Establish a green baseline before changes.

- [ ] T001 Verify baseline build + tests are green: run `bazel build //...` and `bazel test //...` from repo root; record any pre-existing failure (note: the generated `game_types` import errors visible in the editor are build-artifact gaps resolved by `bazel`, not source defects) and confirm the agent + desktop targets build.

---

## Phase 2: User Story 1 — Agent Status Re-syncs on Session Re-entry (Priority: P1) 🎯 MVP

**Goal**: On session (re-)entry the agent reports its real working state and the desktop reconciles its "Agent is typing…" indicator, fixing the stuck-typing-after-reconnect defect (spec US1; [research.md](research.md) D1/D2; [contracts/agent-desktop-channel-contract.md](contracts/agent-desktop-channel-contract.md) §1; [data-model.md](data-model.md) §1).

**Independent Test**: Unit-test the status derivation (ACTIVE/IDLE/UNSPECIFIED); then verify, on session re-entry, the typing indicator clears when the agent is idle.

**必读文档**:
- 代码规范文档: `style/javascript.md`; [Google TypeScript Style (tsguide)](https://google.github.io/styleguide/tsguide.html); `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/)
- 官方文档: 无
- 技术文章: 无

- [ ] T002 [P] [US1] Extract a pure status-derivation helper `deriveStatusSignal(isInFlight: boolean, isBound: boolean): StatusSignalStatus` into a new `projects/game/agent/src/status-signal.ts` (returns `ACTIVE` when `isInFlight`, else `IDLE` when `isBound`, else `UNSPECIFIED`), with side-by-side unit tests `projects/game/agent/src/status-signal.test.ts` covering all three branches (quickstart Scenario 1).
- [ ] T003 [US1] Wire the helper into the inbound `status` branch of `projects/game/agent/src/handler.ts` `Connect` (compute `isInFlight` from `this.isMutexHeld(sessionId)` and `isBound` from `sessionAgent.getAdapterState().isBound`; respond with the derived value). Run `bazel test` on the agent target.
- [ ] T004 [P] [US1] Change `projects/game/desktop/app.go` `ConnectAgent` to capture the probe response's `StatusSignalStatus` (currently discarded at the "Accept any response" comment) and return it from the method (signature `ConnectAgent(sessionID string) (string, error)` — the status enum name string), mirroring how `SendAgentFrame` already returns a value through the Wails binding.
- [ ] T005 [US1] Update `projects/game/desktop/frontend/src/api.ts` `connectAgent` to return `Promise<string>` (the status), and `projects/game/desktop/frontend/src/App.svelte`: (a) `resetPlayPageState` resets `processing = false`; (b) `handleConnectAgent` reconciles — `ACTIVE` ⇒ `processing = true`, else (`IDLE`/`UNSPECIFIED`) ⇒ `processing = false` and `playState = 'chat_ready'`. Run `bazel build` on the desktop target.

**Checkpoint**: After re-entering a session whose agent is idle, the typing indicator clears; status ping-pong is functional end-to-end.

---

## Phase 3: User Story 2 — Saolei MCP Tools Dispatch Reliably After Reconnect (Priority: P1)

**Goal**: Fix the "all MCP tools fail after reconnect" defect by making the `OperationBridge` sink lifecycle stream-scoped (compare-and-delete), so a stale stream close cannot clobber a fresh reconnect's sink (spec US2; [research.md](research.md) D3; [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md) §1; [data-model.md](data-model.md) §2).

**Independent Test**: Unit-test the bridge compare-and-delete (stale `unregisterSink` is a no-op; genuine disconnect still stops dispatch); then a reconnect→dispatch large test (Phase 6).

**必读文档**:
- 代码规范文档: `style/javascript.md`; [Google TypeScript Style (tsguide)](https://google.github.io/styleguide/tsguide.html)
- 官方文档: 无
- 技术文章: 无

- [ ] T006 [US2] Change `projects/game/agent/src/operation-bridge.ts`: make `registerSink(writeFn)` record the installed sink's identity and return a `SinkHandle`; make `unregisterSink(handle?)` clear `this.sink` **only** when `handle` identifies the currently-registered sink (compare-and-delete; a stale handle is a no-op); keep `dispatch`/`handleResult` behavior. Add unit tests `projects/game/agent/src/operation-bridge.test.ts` covering: A-supersedes-B then stale-unregister-A leaves sink=B; unregister-current clears; `dispatch` then resolves `FAILED "desktop disconnected"` (quickstart Scenario 2). Use injected sink functions (DI), not module mocks.
- [ ] T007 [US2] Update `projects/game/agent/src/handler.ts` `Connect`: record the handle returned by `registerSink` per session (replace the bare `activeSessions.add` with a per-session handle map), and have `cleanupSinks` pass that handle to `unregisterSink` so only this stream's sink is cleared. Run `bazel test` on the agent target.

**Checkpoint**: A reconnect followed by a saolei operation turn dispatches successfully (verified end-to-end in Phase 6; confirm via tracing that no `dispatch` resolves `FAILED "desktop disconnected"` on the live post-reconnect stream).

---

## Phase 4: User Story 3 — Agent-Internal Tool Results Are Visible on the Desktop (Priority: P2)

**Goal**: `saolei_update` forwards a display-only `ToolResultPart` (SUCCEEDED on accept, FAILED on rejection) so the desktop renders it without executing anything (spec US3; [research.md](research.md) D4/D5; [contracts/agent-desktop-channel-contract.md](contracts/agent-desktop-channel-contract.md) §2; [data-model.md](data-model.md) §3).

**Independent Test**: Unit-test `pushResult` writes a content frame to the sink (no `pending` entry); an integration/large check that `saolei_update` emits the part and the desktop renders it without an input action.

**必读文档**:
- 代码规范文档: `style/javascript.md`; [Google TypeScript Style (tsguide)](https://google.github.io/styleguide/tsguide.html)
- 官方文档: 无
- 技术文章: 无

- [ ] T008 [US3] Add a display-only `pushResult(toolResult: ToolResultPart): void` method to `projects/game/agent/src/operation-bridge.ts` that wraps the part in a content `AgentFrame` (`sender = SYSTEM`, fresh `frameId`) and writes it to the current sink (no-op if no sink; MUST NOT create a `pending` entry or await a result). Extend `operation-bridge.test.ts` to assert the sink receives exactly one content frame and `pending` stays empty.
- [ ] T009 [US3] In `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `saolei_update` handler: after resolving (acceptance or validation rejection), call `bridge.pushResult(...)` with `status = SUCCEEDED` on acceptance / `FAILED` on rejection (spec C3/D5) and a self-descriptive `message` (e.g. `saolei_update: state updated` / `saolei_update rejected: <reason>`); `toolId` is display-only. Assert via the existing saolei test harness that a `ToolResultPart` is emitted (quickstart Scenario 5).

**Checkpoint**: A `saolei_update` call surfaces a result card on the desktop and triggers no input action.

---

## Phase 5: User Story 4 — Adapter Lifecycle Simplified to Refresh-Only With Profile Guard (Priority: P2)

**Goal**: Remove the implicit per-turn adapter switch; add a profile-name guard that rejects a mismatched turn (warn+wait, non-fatal) before it runs; Refresh is the sole rebuild path (spec US4; [research.md](research.md) D6/D7; [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md) §2/§3; [data-model.md](data-model.md) §4/§5).

**Independent Test**: Unit-test `getOrCreateAdapter` returns cached (no rebuild on mismatch) and rebuilds after `invalidateAdapter`; unit-test the guard rejects mismatched (warn+wait, no mutex) and accepts matching/unbound.

**必读文档**:
- 代码规范文档: `style/javascript.md`; [Google TypeScript Style (tsguide)](https://google.github.io/styleguide/tsguide.html)
- 官方文档: 无
- 技术文章: 无

- [ ] T010 [P] [US4] Simplify `projects/game/agent/src/session-agent.ts` `getOrCreateAdapter`: return the cached adapter when one exists; otherwise build once via `serializeBind` for the supplied profile. Remove the `activeProfileName !== profileName ⇒ rebuild` branch (the auto-switch). Add/extend `session-agent.test.ts`: cached adapter returned for a differing profile (no rebuild); new adapter built after `invalidateAdapter()` (quickstart Scenario 4). Use an injected fake `AdapterFactory`.
- [ ] T011 [US4] Extract a pure helper `shouldRejectProfile(activeProfileName: string | null, isBound: boolean, effectiveProfileName: string): boolean` (true only when `isBound && activeProfileName && activeProfileName !== effectiveProfileName`) — e.g. into `projects/game/agent/src/profile-guard.ts` — with unit tests. Then wire it into `projects/game/agent/src/handler.ts` `Connect` content branch AFTER `effectiveProfileName` resolution and BEFORE `acquireMutex`: on reject, write a `WarnSignal` (naming bound vs received) and a `WaitSignal`, then `return` (no mutex, no adapter invocation) (quickstart Scenario 3).

**Checkpoint**: A mismatched-profile turn is rejected with a warning and the desktop returns to ready; Refresh then rebuilds for the new profile; matching/unbound turns are unaffected.

---

## Phase 6: Large-Test Acceptance (Agent Service — Constitution §VI)

**Purpose**: End-to-end validation of the agent-service behaviors. Tests are added to EXISTING module files and suites (`style/large_test.md`); no new test-plan YAML and no spec/story-named files.

**必读文档**:
- 代码规范文档: `style/large_test.md`; `style/golang.md`; [Google Go Style Guide](https://google.github.io/styleguide/go/)
- 官方文档: 无
- 技术文章: 无

- [ ] T012 Extend `projects/game/testplan/helpers_test.go` with shared helpers used by the new cases: a `refreshAgent(t, sutHostURL, sutEnvName, sessionID)` helper (HTTP `POST .../sessions/{id}/agent:refresh`, mirroring the desktop's call) and a `sendStatusFrame(t, conn, sessionID, status)` helper. Add only if not already present; reuse existing `connectAgentWS`/`sendTextWithProfile`/`drainWSFrame`.
- [ ] T013 Update `TestProfileSwitchMidConnection` in `projects/game/testplan/agent_lifecycle_test.go`: the turn under profile B WITHOUT Refresh MUST now be rejected (assert a `WarnSignal` then `WaitSignal`); then call `refreshAgent`, send the profile-B turn, and assert it succeeds with B's response. Also add a `TestProfileGuardRejectsMismatch` case (mismatch rejected, subsequent matching turn accepted). These cover spec US4 acceptance (quickstart Scenario 7).
- [ ] T014 Add status-ping-pong and reconnect-dispatch cases to `projects/game/testplan/agent_lifecycle_test.go`: (a) `TestStatusPingPong` — send a `status` frame, assert the response is `IDLE` when idle and `ACTIVE` while a turn is in-flight; (b) `TestReconnectDispatchReliability` — connect, run a turn, disconnect, reconnect, run a turn whose operation/tool dispatches, assert it returns `SUCCEEDED` (not `FAILED`). These cover spec US1/US2 acceptance (quickstart Scenario 6).
- [ ] T015 [P] Add a `saolei_update` display-result case to `projects/game/testplan/agent_saolei_test.go`: in an init→click→update flow, assert the agent emits a `ToolResultPart` for `saolei_update` on the stream (SUCCEEDED on acceptance) and that no input action is performed for it. This covers spec US3 acceptance (quickstart Scenario 5).
- [ ] T016 Confirm `projects/game/testplan/system_test.yaml` needs no new suite (the new functions live in the already-referenced `agent_lifecycle_test` and `agent_saolei_test` binaries); run the affected suites via the `testplan` skill and ensure green.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Whole-repo verification and cleanup.

- [ ] T017 Run `bazel build //...` and `bazel test //...` from repo root; regenerate with `bazel run //:gazelle` (and `bazel mod tidy` if module deps moved); ensure no regressions versus the Phase 1 baseline.
- [ ] T018 Run the [quickstart.md](quickstart.md) validation scenarios end-to-end (unit scenarios via `bazel test`; Scenarios 6–7 via the `testplan` skill) and confirm all acceptance criteria (SC-001..SC-005) pass.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately.
- **US1 (Phase 2)**: depends on Phase 1. MVP.
- **US2 (Phase 3)**: depends on Phase 1; edits `operation-bridge.ts` and `handler.ts` (sequenced after US1's `handler.ts` status edit to avoid same-file contention).
- **US3 (Phase 4)**: depends on Phase 1; edits `operation-bridge.ts` (builds on US2's bridge changes) — sequence after Phase 3.
- **US4 (Phase 5)**: depends on Phase 1; edits `handler.ts` (builds on prior handler edits) — sequence after Phase 2/3.
- **Large-Test Acceptance (Phase 6)**: depends on Phases 2–5 (all agent behaviors implemented).
- **Polish (Phase 7)**: depends on Phase 6.

### Within-Phase Ordering (same-file tasks are sequential, NOT [P])

- Phase 2: T002 → T003 (both `handler.ts`-area; T002 extracts the helper T003 wires); T004 → T005 (frontend depends on Go return type). T002 and T004 touch different files but T004/T005 are Go/frontend vs TS — they MAY run in parallel with T002 only if the agent-status contract is fixed; otherwise sequence T002/T003 first.
- Phase 3: T006 → T007 (T007 wires T006's handle).
- Phase 4: T008 → T009 (T009 uses T008's `pushResult`).
- Phase 5: T010 ∥ T011 are different files (`session-agent.ts` vs `profile-guard.ts`+`handler.ts`) — T010 is [P]-eligible; T011's helper extraction precedes its handler wiring.

### Parallel Opportunities

- T010 (session-agent.ts) can run in parallel with T011's `profile-guard.ts` helper extraction (different files).
- Phase 6 large-test tasks T013/T014/T015 touch different test files and are [P]-eligible once T012 helpers exist.
- Different user stories MAY be developed in parallel by different developers ONLY if same-file edits (`handler.ts`, `operation-bridge.ts`) are merged carefully; the recommended path is sequential (US1→US2→US3→US4) to avoid same-file conflicts.

---

## Parallel Example: Phase 6 (after T012)

```bash
# These edit different test files and can run in parallel:
Task: T013 update TestProfileSwitchMidConnection + TestProfileGuardRejectsMismatch in agent_lifecycle_test.go
Task: T015 add saolei_update display case in agent_saolei_test.go
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Phase 1 (baseline green).
2. Phase 2 (US1: status re-sync) — independently testable; delivers the most visible reconnect fix.
3. **STOP and VALIDATE**: re-enter an idle session and confirm the typing indicator clears.
4. (Recommended next: Phase 3 / US2 pairs naturally — the two P1 reconnect fixes together deliver real reconnect resilience.)

### Incremental Delivery

1. Setup → US1 (typing fix) → validate.
2. + US2 (dispatch-after-reconnect) → validate reconnect resilience together.
3. + US3 (saolei_update visible) → validate display.
4. + US4 (adapter simplification + guard) → validate lifecycle.
5. Large-test acceptance (Phase 6) → Polish (Phase 7).

---

## Notes

- Every code task includes its own `bazel build` + `bazel test` for affected targets (Constitution §IV); not separately tasked.
- Desktop (Go + Svelte) changes are verified by build + the agent large tests (the desktop app is not part of the testplan SUT); the agent is the service under large-test acceptance (Constitution §VI).
- The profile guard's `WaitSignal` after the `WarnSignal` also fixes the latent gap where the existing "agent_profile_name required" warn path omits the `WaitSignal` (see [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md) §3).
- Large tests are organized BY MODULE in existing files/suites per `style/large_test.md`; the single `system_test.yaml` is reused (no per-feature plan).
