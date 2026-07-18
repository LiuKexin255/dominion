# Quickstart: Agent Loop Graceful Abort on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This guide documents the runnable validation scenarios that prove the
graceful-abort feature works end-to-end. It is a validation/run guide,
not an implementation reference — code lives in the source tree and
`tasks.md`.

---

## Prerequisites

- Bazel workspace at repo root (`/mnt/code/dominion`).
- The agent service builds: `bazel build //projects/game/agent/...`.
- Test runner available: `bazel run //projects/game/agent:vitest` (or
  equivalent via
  [`projects/game/agent/run_vitest.mjs`](../../../projects/game/agent/run_vitest.mjs)).
- A way to simulate a desktop bidi-stream disconnect (existing tests in
  `handler.test.ts` already do this by ending the gRPC call stream).

---

## Scenario 1: Disconnect mid-LLM-stream aborts the turn

**What it proves**: spec FR-001, FR-004, FR-008, SC-001, SC-002 — the
turn stops promptly on disconnect, no error frames emitted, all turn
phases covered.

### Steps

1. Start a `Connect` bidi stream with a user content frame.
2. While the LLM is mid-stream (between reasoning/text blocks), end the
   gRPC call stream (simulate desktop disconnect).
3. Observe:
   - The `for await` loop in `handler.ts` exits without throwing.
   - No `warn` frame is written for the disconnected session.
   - The per-session mutex is released (a new turn can start on
     reconnect).

### Expected outcome

- Turn stops within seconds of the stream `end`/`error` event.
- Zero `warn`/`error` frames emitted by the agent service for the dead
  session.
- LLM token billing stops (the HTTP request is cancelled by the abort
  signal propagation).

### Where to look

- `handler.test.ts` — add test: "stream end aborts in-flight turn via
  AbortController" (referenced in [plan.md change #12](./plan.md)).
- `llm.test.ts` — add test: "generateTurn respects AbortSignal — stream
  stops on abort" (referenced in [plan.md change #14](./plan.md)).

---

## Scenario 2: Disconnect mid-tool-dispatch resolves cleanly

**What it proves**: spec FR-001, FR-008 — a tool call awaiting a desktop
result is cleaned up on disconnect, not left to time out.

### Steps

1. Start a `Connect` bidi stream; send a user content frame that
   triggers a mouse-tool call.
2. The tool calls `bridge.dispatch(part)` and is awaiting the desktop
   result.
3. End the gRPC call stream (disconnect).
4. Observe:
   - `dispatch()` resolves with `{ status: FAILED, message: "aborted"
     }` (not a throw, not a 5s timeout wait).
   - The pending-dispatch map entry is cleaned up.
   - The turn unwinds via the abort signal.

### Expected outcome

- `dispatch()` returns a FAILED result promptly (not after the 5s
  timeout).
- No `DesktopDisconnectedError` is thrown anywhere (the class is
  deleted).

### Where to look

- `operation-bridge.test.ts` — add test: "dispatch with aborted signal →
  FAILED" (referenced in [plan.md change #13](./plan.md)).
- `mouse-tool.test.ts` — add test: "dispatch signal abort propagates to
  tool result" (referenced in [plan.md change #15](./plan.md)).

---

## Scenario 3: Reconnect resumes from checkpoint

**What it proves**: spec FR-006, SC-003 — the checkpoint survives an
aborted turn; reconnect can start a new turn from the last consistent
state.

### Steps

1. Run a turn to completion (user says "click the button", tool
   succeeds, LLM responds). Checkpoint written.
2. Start a second turn; disconnect mid-stream (abort).
3. Reconnect with the same session id; call `ListMessages`.
4. Observe:
   - All messages from the completed first turn are present.
   - The aborted second turn's user message may or may not be present
     depending on whether the abort happened before or after the first
     superstep checkpoint — but the state is consistent (no partial LLM
     message, no orphaned tool message).
   - A new turn can be started and runs correctly.

### Expected outcome

- 100% of pre-disconnect completed messages preserved.
- No partial/corrupt messages in the checkpoint.
- New turn after reconnect works normally.

### Where to look

- `handler.ts` `ListMessages` (unchanged — it reads from the
  checkpointer, whose semantics are preserved by LangGraph).

---

## Scenario 4: Normal turn path unchanged

**What it proves**: spec FR-007, SC-004 — the happy path is
byte-for-byte identical to today.

### Steps

1. Run a full turn with a stable desktop connection: text-only, then
   image+text, then a tool-using turn.
2. Observe:
   - Same frames streamed in the same order.
   - Closing `wait` frame sent.
   - Mutex released.

### Expected outcome

- No behavioral regression on any turn type.

### Where to look

- Existing tests in `handler.test.ts` (register/unregister sink,
  LLM-error handling) should pass unchanged after the refactor — they
  exercise the normal path.

---

## Scenario 5: No-sink dispatch returns FAILED (not throw)

**What it proves**: spec FR-003 — the throw-on-no-sink path in
`OperationBridge.dispatch()` is removed; replaced with a FAILED return.

### Steps

1. Call `bridge.dispatch(part)` when no sink is registered.
2. Observe: returns `{ status: FAILED, message: "desktop disconnected"
   }` — does not throw.

### Expected outcome

- No `DesktopDisconnectedError` thrown.
- The `DesktopDisconnectedError` class no longer exists in the codebase.

### Where to look

- `operation-bridge.test.ts` — replace the existing "throws
  DesktopDisconnectedError" test with "returns FAILED" (referenced in
  [plan.md change #13](./plan.md)).

---

## Full validation run

After implementation, run the complete agent test suite:

```bash
bazel test //projects/game/agent/...
```

All existing tests (minus the deleted `DesktopDisconnectedError` /
`hasSink()` tests) must pass, plus the new abort-coverage tests added
per the plan.
