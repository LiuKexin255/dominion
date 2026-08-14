# Quickstart: Real-Time Init Instruction Delivery

**Feature**: `041-realtime-init-push` | **Date**: 2026-08-09 | **Spec**: [spec.md](./spec.md)

Runnable validation scenarios that prove the feature works end-to-end. Each scenario lists prerequisites, commands, and the expected outcome. Implementation details belong in `tasks.md` (Phase 2); this is a validation/run guide.

For the channel contract governing these behaviors, see [`contracts/realtime-channel-contract.md`](./contracts/realtime-channel-contract.md). For entities, frame shapes, and dedup rules, see [`data-model.md`](./data-model.md).

## Prerequisites

- A running game deployment reachable by the desktop (agent + gateway). Deploy via the testplan skill for large tests (see "Large-test acceptance" below); for local checks, the unit-test suites suffice.
- A `TeamProfile` configured for the `saolei` template (planner model reachable — the gRPC keepalive fix must be deployed, spec Assumption line 105).
- The desktop build (`bazel build //projects/game/desktop:desktop` or equivalent).

## Unit-level checks (fast feedback — run during development)

These belong to the dev tasks (Constitution Principle IV — compile + unit test are part of coding, not separate tasks) but are listed here as the per-change verification gate.

### A1. Agent: stream sink bind/clear + init emission

**Covers**: contract §1 (sink lifecycle), §2 (init frames), §4 (dedup anchor); FR-001, FR-004, FR-005, FR-006, FR-010.

**Commands**:
```bash
bazel test //projects/game/agent:lib_test          # session-team/handler/instruction-node unit tests
```

**Expected**: new/updated cases assert —
- `bindStreamSink` then `emitChannelFrame` writes through the bound sink; before bind / after `clearStreamSink`, emit is a no-op.
- The init turn, with a bound sink, emits exactly the three frames (planner request `USER`, planner response `toolCall AGENT`, player write-back `USER`), each `frameId == <message id>`.
- Compare-and-delete: `clearStreamSink(handle)` does NOT clear a sink bound by a different (newer) handle.
- Degrade: planner failure → no frame emitted; promise resolves.

### A2. Desktop: continuous reader lifecycle

**Covers**: contract §3 (continuous reader); FR-002, FR-008, FR-009, FR-011, FR-012.

**Commands**:
```bash
bazel test //projects/game/desktop:desktop_test      # app_test.go + view_model_test.go
```

**Expected**: new/updated cases (imitate the existing `recvLoop` test pattern, `app_test.go:517-602`) assert —
- The reader started at `Connect` (after the probe) receives background `messageParts` frames with **no** user turn submitted (US3 / SC-003).
- A `wait` FlowPart is forwarded to the chatstream but the reader does **not** exit (FR-008); the next frame is still received.
- `SendUserTurn` does **not** start a second reader (FR-012); a turn's response frames are read by the already-running reader.
- Operation `FlowPart`s are executed and their `FlowResultPart`s sent back (FR-009).
- On `RecvFrame` error the reader synthesizes a `wait` and exits, closing `recvDone`; `CloseAgent` returns cleanly (FR-010).
- Reconnect: `Connect` waits on the prior `recvDone` before starting a new reader.

## Large-test acceptance (mandatory — Constitution Principle VI)

**This is a service-type real-time-channel change: large-test execution is mandatory acceptance, NOT satisfied by `bazel build`/`bazel test` alone.** Run via the **testplan** skill (`tools/test/guitar`); read `style/large_test.md` first.

The existing `projects/game/testplan/saolei_team_test.go` covers the `Connect` lifecycle (`TestTeamConnectLifecycle`, `TestTeamConnectExclusiveEmit`). The acceptance plan MUST add/extend cases for the scenarios below, then run the full deploy→test→cleanup loop:

**Large-test scope note**: B5 is service-side (second `Connect`, no duplicate) and MUST be added to the large test (tasks T010). B6's reader-continuity and B7's desktop-close aspects are client (desktop) behaviors covered by Go unit tests (task T008); B7's reconnect sink-rebind is covered by agent unit tests (task T009). The large test's obligations for B6/B7 are limited to: existing turn-level and re-Connect lifecycle cases keep passing (no regression) — see T010.

```bash
# Load the testplan skill, then:
guitar run projects/game/testplan/<plan.yaml>   # full deploy → test → cleanup
```

**Pass criterion**: ALL test cases pass. Any failed/flaky case = acceptance NOT met (fix and re-run).

### B1. Init instruction visible on first entry (US1 / SC-001) — P1

**Setup**: fresh session, no team materialized. `UpdateTeam(allowMissing=true)` with a `TeamProfile`, then `Connect`.

**Steps**:
1. `UpdateTeam` (materialize + trigger init fire-and-forget).
2. `Connect` (opens stream; status probe → expect `IDLE`).
3. **Without sending any user message**, read frames from the stream until the init turn completes (~planner response time, ≤ 10 s).

**Expected**: the stream delivers, in order —
- planner request frame (`agent=planner`, `role=USER`, `messageParts.text`),
- planner response frame (`agent=planner`, `role=AGENT`, `messageParts.toolCall` instruct_player),
- player write-back frame (`agent=player`, `role=USER`, `messageParts.text`).

Each `frameId` equals the persisted message id (verified via a subsequent `ListMessages` for both `planner` and `player`). No re-entry needed.

### B2. Typing indicator OFF on entry during init (US2 / SC-002) — P2

**Setup**: fresh session; `UpdateTeam` then `Connect` **while the init turn is still in flight**.

**Expected**: the status probe response is `STATUS_SIGNAL_STATUS_IDLE` (init excluded from `isRunning`). Send a user message → frames flow → terminal `wait` clears `processing`. The init turn emits no `wait`/`status`.

### B3. Background delivery without user interaction (US3 / SC-003) — P3

**Setup**: `Connect` to a session whose init turn is still running.

**Expected**: with **no** `SendUserTurn` call, the init frames (B1) arrive through the connection. (Equivalent to B1's step 3, framed independently — proves the continuous channel, not a per-turn read.)

### B4. Destructive ops rejected during init (FR-007 / SC-005)

**Setup**: session with the init turn in flight (`isBusy()` true, `isRunning()` false).

**Steps**: issue `RefreshTeam` and a profile-change rebuild (`UpdateTeam` with a different profile) during the init.

**Expected**: both return `FAILED_PRECONDITION`. After the init completes, the same operations succeed.

### B5. No duplicate on re-entry (FR-004 / US1 acceptance scenario 2) — edge case 5

**Setup**: session where the init already completed. `Connect`, receive the seed + the real-time push is a no-op (sink was unbound when init completed).

**Steps**: re-enter the session (second `Connect`).

**Expected**: the instruction appears exactly once (from history/seed). No duplicate rendering (`renderedMessageIds` dedup by `frameId == messageId`). Then verify US1 acceptance scenario 2: re-entry shows the instruction immediately from history with no delay.

### B6. Continuous reader handles a full user turn (regression — SC-004)

**Setup**: connected session (continuous reader running).

**Steps**: `SendUserTurn` with a user message; the agent runs a turn producing text + an operation (desktop automation) + a terminal `wait`.

**Expected**: all turn response frames (text, tool calls, operation request → executed, `FlowResultPart` returned, terminal `wait`) are delivered through the **same** continuous reader — no per-turn reader is started. The reader continues after `wait` (FR-008) and is ready for the next turn. No regression vs the previous per-turn model (existing turn-level large-test cases must still pass).

### B7. Clean close / reconnect (FR-010)

**Setup**: connected session with the continuous reader running and the init sink bound.

**Steps**: close the connection (`CloseAgent`); reconnect (`Connect`).

**Expected**: `CloseAgent` returns cleanly (reader exited, sink cleared, no write to a dead connection). Reconnect binds a fresh sink; a still-in-flight init (if any) pushes to the new connection; a completed init is delivered from history. No goroutine leak (the prior `recvDone` was awaited).

## References

- Channel behaviors: [`contracts/realtime-channel-contract.md`](./contracts/realtime-channel-contract.md) (§1 sink, §2 init frames, §3 reader, §4 dedup, §5 unchanged).
- Entities & frame model: [`data-model.md`](./data-model.md) (§1 runtime constructs, §2 init turn, §3 frame shapes, §4 dedup rules).
- Design rationale: [`research.md`](./research.md) (D1–D9).
- Status probe contract (unchanged): [`specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md`](../021-agent-session-resync/contracts/agent-desktop-channel-contract.md) §1.
- Turn `wait`/queue semantics (unchanged): [`specs/030-queued-chat-input/contracts/turn-loop-contract.md`](../030-queued-chat-input/contracts/turn-loop-contract.md).
- Large-test workflow: `style/large_test.md` + the testplan skill (`tools/test/guitar`).
