# Data Model: Agent Loop Graceful Abort on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This feature introduces no new persistent entities, database tables, or
protobuf messages. It is a behavior refactor of the in-process turn
lifecycle. This document models the **runtime lifecycle states** of the
two entities whose behavior changes: the **per-turn AbortController** and
the **Agent Turn**.

---

## Entity: Per-Turn AbortController

A standard Web API `AbortController` created once per agent turn, scoped
to the bidi stream that drives the turn. Not persisted; lives only in
the `Connect` handler's closure.

### Fields

| Field | Type | Source |
|---|---|---|
| `signal` | `AbortSignal` | Standard Web API. Passed to `adapter.generateTurn(threadId, content, signal)` and onward to `agent.streamEvents(input, { signal })`. |
| `sessionId` | `string` (implicit, via map key) | The session this controller belongs to. Keyed in `activeTurns: Map<string, AbortController>`. |

### State transitions

```text
                    ┌─────────┐
    turn starts     │         │  stream.on("end")
  ┌──────────────►  │ ACTIVE  │  or stream.on("error")
  │                 │         │  ──────────────────────┐
  │                 └────┬────┘                        │
  │                      │                             ▼
  │                      │ controller.abort()
  │                      │ (stream end/error)
  │                      ▼
  │                 ┌─────────┐
  │                 │         │
  │                 │ ABORTED │
  │                 │         │
  │                 └────┬────┘
  │                      │
  │                      │ for-await loop exits
  │                      │ finally: activeTurns.delete(sid)
  │                      ▼
  │                 ┌─────────┐
  │                 │         │
  └─────────────────┤  GONE   │  (controller discarded; map entry removed)
                    │         │
                    └─────────┘
```

| Transition | Trigger | Effect |
|---|---|---|
| `→ ACTIVE` | `acquireMutex(sessionId)` succeeds; controller created and inserted into `activeTurns`. | Signal is live; `generateTurn` receives it and passes to `streamEvents`. |
| `ACTIVE → ABORTED` | `stream.on("end")` or `stream.on("error")` fires; `abortAllTurns()` calls `controller.abort()`. | LangGraph cancels the LLM HTTP request; the `streamEvents` async iterator stops; the `for await` loop in `streamFromAgent` exits; the `for await` loop in `handler.ts` exits. |
| `ABORTED → GONE` | The turn's `finally` block runs: `activeTurns.delete(sessionId)` + `releaseMutex(sessionId)`. | Controller is eligible for GC. |
| `ACTIVE → GONE` (normal completion) | Turn completes without abort; `finally` block runs. | Controller is never aborted; discarded after normal completion. |

### Invariants

- **At most one ACTIVE controller per session** — enforced by the
  per-session mutex (`acquireMutex` / `releaseMutex` in `handler.ts`).
- **A controller is aborted at most once** — `AbortController.abort()`
  is idempotent per the Web API spec; calling it on an already-aborted
  controller is a no-op.
- **`activeTurns` is stream-scoped** — the map lives in the `Connect`
  closure; a new stream gets a fresh map. Reconnecting with the same
  session id creates a new controller for the new turn.

---

## Entity: Agent Turn

The conversational pass driven by a single user message. Its lifecycle
is governed by the existing per-session mutex and the `generateTurn`
async iterable. This feature adds the abort path.

### State transitions (updated)

```text
    ┌──────────┐   user content frame    ┌──────────┐
    │          │ ──────────────────────► │          │
    │   IDLE   │   acquireMutex          │ BINDING  │
    │          │                         │ (if adapter needs build)
    └──────────┘                         └────┬─────┘
        ▲                                      │ adapter ready
        │                                      ▼
        │                                 ┌──────────┐
        │                            ┌──► │          │
        │                            │    │ RUNNING  │ ◄─── stream.on("end"/"error")
        │                            │    │          │      │
        │                            │    └──┬───┬───┘      │ abort signal fires
        │                            │       │   │          ▼
        │                      next block   │   │   ┌──────────┐
        │                       yields      │   │   │          │
        │                            │      │   │   │ ABORTING │
        │                            └──────┘   │   │          │
        │                                       │   └────┬─────┘
        │                                  turn  │        │ for-await exits
        │                                  done  │        ▼
        │                                       │   ┌──────────┐
        │                                       │   │          │
        └───────────────────────────────────────┘   │ CLEANUP  │
                  releaseMutex                       │          │
                                                    └────┬─────┘
                                                         │ finally
                                                         ▼
                                                    return to IDLE
```

| State | Entry condition | Exit condition | Abort-aware? |
|---|---|---|---|
| `IDLE` | No turn in flight for the session. | User content frame arrives; `acquireMutex`. | N/A |
| `BINDING` | Adapter not yet cached for the profile; `getOrCreateAdapter` running. | Adapter returned. | No — abort does not interrupt adapter bind (see [spec edge case: Disconnect during adapter bind](./spec.md#edge-cases)). |
| `RUNNING` | `adapter.generateTurn(...)` called; `streamEvents` iteration active. | All blocks yielded (normal) **or** abort signal fires. | **Yes** — `signal` passed to `streamEvents`. |
| `ABORTING` | Abort signal fired during `RUNNING`. | `for await` loop exits; control returns to handler. | **Yes** — LangGraph stops the LLM HTTP + stream. |
| `CLEANUP` | `for await` exited (normal or abort). | `finally` block completes: `activeTurns.delete(sessionId)` + `releaseMutex(sessionId)`. | N/A. |

### What the abort does NOT change

- **`IDLE → BINDING → RUNNING`** is unchanged for the normal path.
- **The mutex** is still acquired before the turn and released in the
  `finally` block — abort does not skip cleanup.
- **The checkpoint** — completed supersteps are preserved by LangGraph's
  per-superstep checkpointing; the in-progress superstep at abort time
  is discarded. Resuming with the same `thread_id` continues from the
  last consistent boundary.
- **Sink registration** — `registerSink()` / `unregisterSink()` lifecycle
  on `OperationBridge` is unchanged. `cleanupSinks()` still runs on
  stream end/error alongside the new `abortAllTurns()`.

---

## Relationship: AbortController ↔ OperationBridge.dispatch

When a tool dispatch is in flight (awaiting a desktop result) and the
abort signal fires:

```text
dispatch(part, signal)
  │
  ├── sink registered?
  │     ├── no  → return { FAILED, "desktop disconnected" }   (no throw)
  │     └── yes → write to sink, set up pending entry + timer
  │                │
  │                ├── signal aborts first  → resolve { FAILED, "aborted" }, cleanup
  │                ├── tool result arrives   → resolve with result, cleanup
  │                └── 5s timeout fires      → resolve { FAILED, "operation timed out" }, cleanup
  │
  └── return OperationResult
```

The signal is a third race participant alongside the existing tool-result
and timeout paths. All three resolve (never reject) and clean up the
pending map entry and timer.
