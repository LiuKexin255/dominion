# Contract: Stream-Write Error Containment & Global Rejection Handler

**Feature**: [spec.md](../spec.md) | **Date**: 2026-07-27

This contract defines the behavioral interface of the two new constructs
introduced by this fix:

1. `safeWrite` — a private helper in `handler.ts` that wraps
   `stream.write()` in error containment.
2. The global `unhandledRejection` handler registered in `bootstrap.ts`.

Both are internal to the agent service (no external API surface).

---

## §1. `safeWrite`

### Signature

```typescript
function safeWrite(
  stream: grpc.ServerWritableStream<AgentFrame, AgentFrame>,
  frame: AgentFrame,
  sessionId: string,
): void
```

### Contract

1. **MUST NOT throw**. Any exception from `stream.write(frame)` is
   caught, logged at `warn` level, and swallowed.
2. **MUST log** on failure with:
   - Message: `"stream write failed (peer disconnected?)"`
   - Fields: `{ sessionId, error: String(err) }`
3. **MUST NOT buffer or retry**. If the write fails, the frame is
   dropped. (The peer is gone; per
   [017 FR-004](../../017-agent-loop-graceful-abort/spec.md), no frames
   should be emitted to a dead peer.)
4. **MUST be a no-op on success**. When `stream.write()` succeeds,
   `safeWrite` returns normally with no side effects beyond the write
   itself.

### Usage rule

Every `stream.write(frame)` call inside the `Connect` handler's
`stream.on("data", async (frame) => { ... })` callback MUST be replaced
with `safeWrite(stream, frame, sessionId)`. This includes:

- Status response frame (handler.ts line ~268)
- Profile-mismatch warn/wait frames (~348, ~357, ~372, ~386)
- Reasoning/text/toolCall/toolResult frames in the `for await` loop
  (~434, ~446, ~466, ~490)
- Post-loop wait frame (~509)
- **Catch-block warn/wait frames** (~527, ~537) — the crash vector

The sink callback registered via `registerSink` also calls
`stream.write(contentEnvelope)` (line ~395). This write is inside the
`try` block but originates from the bridge, not from the data callback
directly. It SHOULD also be guarded, but since the bridge's sink is
unregistered on stream end/error (via `cleanupSinks`), the window is
narrower. Guard it for consistency.

---

## §2. Global `unhandledRejection` Handler

### Registration site

`projects/game/agent/src/bootstrap.ts`, in the `main()` async function,
after `installReporter(...)` and before `startServer()`.

### Contract

1. **MUST log** every unhandled rejection at `error` level:
   - Message: `"unhandled promise rejection"`
   - Fields: `{ reason: String(reason) }`
2. **MUST NOT call `process.exit()`**. The handler is a safety net; the
   service must survive a single unexpected rejection.
3. **MUST be registered exactly once**. `bootstrap.ts` is the sole
   entrypoint; registering in other modules would duplicate the handler.
4. **MUST NOT interfere with `SIGTERM`/`SIGINT` shutdown**. The handler
   only logs; the existing signal handlers control graceful shutdown.

### Rationale

Node.js ≥15 defaults to `--unhandled-rejections=throw`, which terminates
the process on any unhandled promise rejection. For a long-running
multi-session gRPC server, this is operationally dangerous: a single
unexpected rejection (from any code path, not just the one fixed by
`safeWrite`) kills all active sessions.

The handler changes the default from "terminate" to "log and continue"
for this service class. The tradeoff is that a genuinely corrupted state
might continue running after a rejection — but the structured log
ensures the rejection is observable for diagnosis, and the `safeWrite`
fix closes the known crash vector so the handler should rarely fire.

---

## §3. Interaction between `safeWrite` and the global handler

`safeWrite` is the **primary fix** — it closes the known crash vector
(unguarded writes in the catch block). The global handler is
**defense-in-depth** — it catches any *other* unhandled rejection that
might slip through in the future.

After the fix:
- `safeWrite` prevents writes-to-closed-stream from throwing → the
  known crash path is closed.
- If some *other* code path produces an unhandled rejection, the global
  handler logs it and the service survives → the service is resilient
  to the entire category of bug, not just this one instance.

The two are complementary, not redundant.
