# Quickstart: Fix Agent Service Crash on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-27

Validation scenarios for the fix. Scenarios 1–3 are unit/integration
tests (vitest); Scenarios 4–5 are large-test cases (testplan skill).

---

## Prerequisites

- `bazel` available (build + test entry).
- `projects/game/agent/` compiles: `bazel build //projects/game/agent/...`.
- Existing tests pass: `bazel test //projects/game/agent/...`.

---

## Scenario 1 (Unit): `safeWrite` catches write error on closed stream

**Validates**: data-model.md §1 invariant "never throws"; contract §1.

1. In `handler.test.ts`, create a mock `stream` whose `write()` method
   throws `new Error("ERR_STREAM_DESTROYED")`.
2. Call `safeWrite(mockStream, frame, "test-session")`.
3. Assert: no exception thrown.
4. Assert: a `warn` log was emitted with message
   `"stream write failed (peer disconnected?)"` and
   `{ sessionId: "test-session", error: "Error: ERR_STREAM_DESTROYED" }`.

**Expected outcome**: `safeWrite` returns normally; the write error is
logged but not thrown.

---

## Scenario 2 (Unit): Catch-block write does not crash on closed stream

**Validates**: spec FR-001 (process does not crash); research.md §D
crash chain is broken.

1. In `handler.test.ts`, simulate the crash chain:
   - Set up a `Connect` handler with a mock bidi stream.
   - Feed a user-content frame to start a turn.
   - Make `generateTurn` throw a non-abort error (so the catch block
     enters the `else` branch).
   - Have the mock stream's `write()` throw on every call.
2. Assert: the data callback completes without throwing.
3. Assert: no unhandled rejection is produced (the test process does
   not crash).

**Expected outcome**: the catch block's `stream.write(warnFrame)` and
`stream.write(waitFrame)` calls are caught by `safeWrite`; the callback
returns normally.

---

## Scenario 3 (Unit): Disconnect during turn — no frames to dead peer

**Validates**: spec FR-003 / 017 FR-004 parity.

1. In `handler.test.ts`, simulate a turn in progress.
2. Emit `stream.on("end")` to trigger `abortAllTurns()`.
3. Assert: after abort, no further frames are written to the stream
   (the `for await` loop exits, the catch block enters the `if
   (controller.signal.aborted)` branch which only logs).

**Expected outcome**: zero `stream.write()` calls after the abort; the
dead peer receives no frames.

---

## Scenario 4 (Large Test): Disconnect does not crash the agent service

**Validates**: spec SC-001 / SC-002 (process survives; zero unhandled
rejections).

1. Deploy the agent service via `testplan` skill
   (`guitar run projects/game/testplan/system_test.yaml`).
2. In the `checkpoint-resume` suite (or a new case in the existing
   suite), connect a desktop, start a turn, then disconnect the desktop
   mid-turn.
3. Assert: the agent service process is still running after the
   disconnect (health check passes, or the test framework can make a
   second RPC to the same service instance).
4. Assert: no `unhandled promise rejection` log appears in the agent
   service logs during the disconnect.

**Expected outcome**: agent service PID unchanged; zero unhandled
rejection log entries.

---

## Scenario 5 (Large Test): Normal turn path unchanged

**Validates**: spec FR-006 / SC-004 (happy path regression).

1. In the existing `agent-dialog` or `agent-operation` suite, run a
   normal turn with a stable desktop connection.
2. Assert: streamed frames, closing wait frame, and mutex release are
   identical to pre-fix behavior.

**Expected outcome**: all existing large-test cases pass without
modification (the fix adds only error-containment guards that are
no-ops on the happy path).

---

## Running the validation

```bash
# Unit tests (Scenarios 1-3)
bazel test //projects/game/agent:handler_test

# Large tests (Scenarios 4-5) — via testplan skill
# Load the testplan skill, then:
guitar run projects/game/testplan/system_test.yaml
```

All cases MUST pass (Constitution §VI).
