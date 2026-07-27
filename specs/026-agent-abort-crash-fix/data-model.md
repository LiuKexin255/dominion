# Data Model: Fix Agent Service Crash on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-27

This feature introduces no new data entities (no proto changes, no
storage changes). The data model here documents the **behavioral
contract** of the two new code constructs: the `safeWrite` helper and
the global `unhandledRejection` handler.

---

## 1. `safeWrite` — Stream-Write Error Containment

### Purpose

Wraps `stream.write(frame)` in a try-catch so that a synchronous throw
from writing to a closed/destroyed gRPC bidi stream is contained and
logged, never propagated as an unhandled rejection.

### Signature

```text
safeWrite(stream: ServerWritableStream, frame: AgentFrame, sessionId: string): void
```

### Behavior model

| Stream state | `stream.write()` outcome | `safeWrite` outcome |
|---|---|---|
| Open (healthy) | Returns `true` (or `false` for backpressure) | Returns normally; frame delivered |
| Closing / closed (peer gone) | Throws `ERR_STREAM_DESTROYED` / `ERR_STREAM_WRITE_AFTER_END` | Catches the throw; logs at `warn` level: `"stream write failed (peer disconnected?)"` with `{ sessionId, error }`; returns normally |
| Already errored | Throws or emits `'error'` event | Catches the throw; logs at `warn`; returns normally |

### Invariants

- **Never throws**: `safeWrite` MUST NOT propagate any exception to its
  caller. This is its core contract — it is the error-containment
  boundary.
- **Idempotent on failure**: if the stream is closed, repeated
  `safeWrite` calls are no-ops (each logs a warn).
- **No frame buffering**: `safeWrite` does not queue frames for later
  delivery. If the stream is closed, the frame is dropped (the peer is
  gone; per 017 FR-004, no frames should be emitted to a dead peer
  anyway).
- **Log level**: `warn` (not `error`) — a write failure during
  disconnect is an expected operational event, not an anomaly.

### Placement

Private function in `projects/game/agent/src/handler.ts`. Not exported.
Used only within the `Connect` handler's `stream.on("data", ...)`
callback.

---

## 2. Global `unhandledRejection` Handler — Defense-in-Depth

### Purpose

Catches any unhandled promise rejection at the process level so that a
single unexpected rejection does not terminate the multi-session agent
service.

### Registration

In `projects/game/agent/src/bootstrap.ts`, after OTel/reporter
initialization (so structured logging is available), before
`startServer()`:

```text
process.on("unhandledRejection", (reason) => {
    error("unhandled promise rejection", { reason: String(reason) });
});
```

### Behavior model

| Event | Handler action |
|---|---|
| Unhandled promise rejection | Log at `error` level with `reason`; **do not** call `process.exit` |
| Normal operation | No-op (handler never fires) |

### Invariants

- **Never exits the process**: the handler MUST NOT call
  `process.exit()`. A multi-session gRPC server must survive a single
  unexpected rejection.
- **Always logs**: the rejection reason MUST be logged so it is
  observable for diagnosis (via OTel reporter / signoz).
- **Does not suppress `rejectionHandled`**: if a rejection is later
  handled (e.g., by a `.catch()` attached after the `unhandledRejection`
  event), Node.js fires `rejectionHandled` automatically — the handler
  does not interfere with this.

---

## 3. What is NOT in the data model

- No new proto messages, enums, or fields.
- No new persistence entities.
- No changes to `MemorySaver` checkpoint semantics.
- No changes to `AgentFrame`, `MessagePart`, `FlowPart`, or any
  transport types.
- No changes to `AbortController` / `AbortSignal` lifecycle (017's
  per-turn controller, `activeTurns` map, `abortAllTurns()` — all
  preserved).

The fix is purely in the error-handling perimeter: `safeWrite` guards
existing `stream.write()` calls, and the global handler is a
process-level safety net. Neither introduces new data.
