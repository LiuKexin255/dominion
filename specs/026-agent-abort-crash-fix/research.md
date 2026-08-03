# Research: Fix Agent Service Crash on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-27

This document consolidates the technical investigation that grounds the
fix plan. It covers (A) the LangGraph v1.4.8 `GraphRunStream` abort
behavior discovered by reading the installed source, (B) the `@grpc/grpc-js`
bidi stream `write()` behavior on a closed stream, (C) the `mergeIterables`
flush-then-throw pattern, and (D) the design decisions that follow.

---

## A. LangGraph v1.4.8 GraphRunStream Abort Behavior

### A.1 Methodology

Read the installed `@langchain/langgraph@1.4.8` source directly:

- `dist/stream/run-stream.js` — `GraphRunStream` class definition
- `dist/stream/mux.js` — `StreamMux` + `pump` function
- `dist/stream/stream-channel.js` — `StreamChannel` class
- `dist/stream/transformers/messages.js` — `createMessagesTransformer`

This is authoritative for the pinned version
(`@langchain/langgraph` `^1.4.8` per `pnpm-workspace.yaml` catalog).

### A.2 `stream.output` — already has a `.catch()` inside LangGraph

`GraphRunStream` constructor (`run-stream.js` lines 94-98):

```javascript
this.#valuesDone = new Promise((resolve, reject) => {
    this.#resolveValuesFn = resolve;
    this.#rejectValuesFn = reject;
});
this.#valuesDone.catch(() => {});
```

`stream.output` is a getter returning `this.#valuesDone` (line 216-218).

**Conclusion**: `stream.output`'s rejection is **already consumed** by
LangGraph internally. Even if `await stream.output` is skipped (because
`yield* mergeIterables(...)` throws before it), the Promise has a
synchronously-attached rejection handler, so Node.js will **not** fire an
`unhandledRejection` event for `stream.output`.

> **Revision of the initial analysis**: spec.md's Motivation section
> hypothesized that `stream.output`'s dangling rejection is the crash
> cause. Source inspection disproves this for v1.4.8. The crash vector
> is more subtle (see §C and §D below).

### A.3 `pump(source, mux)` — also has a `.catch()`

`createGraphRunStream` line 428:

```javascript
pump(source, mux).catch((err) => {});
```

`pump` drains the raw `graph.stream()` source into the mux. On abort the
source rejects; `pump` catches it and calls `mux.fail(err)`. The
`.catch((err) => {})` swallows any rejection from `pump` itself.

**Conclusion**: `pump` cannot produce an unhandled rejection.

### A.4 `StreamChannel.fail(err)` — iterators THROW on subsequent `.next()`

`StreamChannel.iterate()` (`stream-channel.js` lines 91-106):

```javascript
iterate(startAt = 0) {
    let cursor = startAt;
    return { next: async () => {
        while (true) {
            if (cursor < this.#items.length) return { value: ..., done: false };
            if (this.#done) {
                if (this.#error) throw this.#error;   // ← THROWS
                return { value: void 0, done: true };
            }
            await new Promise((resolve) => this.#waiters.push(resolve));
        }
    } };
}
```

When `mux.fail(err)` is called, it invokes:
1. `transformer.fail?.(err)` for every transformer
2. `channel._fail(err)` for every wired channel
3. `this._events.fail(err)` — the main event log
4. `stream[REJECT_VALUES](err)` for every stream handle

`StreamChannel.fail(err)` sets `#error = err` and `#done = true`, then
wakes all pending waiters. The next `iterate().next()` call checks
`#done && #error` → **throws the error**.

### A.5 ALL stream projections throw on abort — including `stream.messages`

`createMessagesTransformer.fail(err)` (`messages.js`):

```javascript
fail(err) {
    for (const [key, stream] of active) {
        stream.source.fail(err);
        active.delete(key);
    }
    ignored.clear();
    log.fail(err);   // ← the messages StreamChannel is also failed
}
```

`messages` projection is `log.toAsyncIterable()` (line 30). When
`log.fail(err)` sets `#error`, subsequent `stream.messages` iteration
**throws the error**.

**This contradicts 017's research.md §A.3** which stated:
> `stream()` / `streamEvents()` with aborted signal: The async iterator
> **stops cleanly**; the stream closes via `controller.close()`. **No
> error is thrown to the consumer**.

017's research cited [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900)
as the source. That PR predates the v3 `GraphRunStream` / `StreamMux`
architecture (which was introduced in `@langchain/langgraph` 1.x). The
v1.4.8 installed source shows that abort propagates as a **throw**,
not a clean close.

**Implication**: both 017's single-consumer path AND 023's concurrent
path receive a throw on abort in v1.4.8. The throw is caught by
handler.ts's catch block in both cases (the catch block checks
`controller.signal.aborted`). The crash is NOT caused by the throw
itself, but by what happens in the catch block's else-branch when
`stream.write()` hits a closed stream (see §D).

### A.6 Abort timing: `controller.abort()` is sync, `mux.fail` is async

1. `stream.on("end"/"error")` fires → `abortAllTurns()` → `controller.abort()` — **synchronous**: `controller.signal.aborted` is `true` immediately.
2. The abort signal propagates into LangGraph's `combineAbortSignals` → the raw `graph.stream()` source rejects — **async** (next microtask or later).
3. `pump` catches → `mux.fail(err)` → `StreamChannel.fail(err)` — **async**.
4. Consumers' `next()` throws — **async**.

Between step 1 and step 4, `controller.signal.aborted` is already `true`.
So by the time handler.ts's catch block runs, `controller.signal.aborted`
should be `true`, and the catch block enters the safe `if` branch.

**However**, `mergeIterables`'s flush-then-throw (§C) can yield buffered
blocks DURING step 4's window. Handler processes these blocks via
`stream.write()`. If the gRPC stream is already closed but the error/end
event hasn't been processed yet (step 1 hasn't fired), `stream.write()`
can throw synchronously. The throw is caught by the catch block, but
`controller.signal.aborted` may still be `false` at that instant (the
end/error event callback hasn't run yet). The catch block enters the
`else` branch and calls `stream.write()` again, which throws again —
this time from inside the catch block (see §D).

---

## B. `@grpc/grpc-js` Bidi Stream `write()` on Closed Stream

### B.1 Behavior

`ServerWritableStream` extends Node.js `Writable`. When the underlying
gRPC call has been cancelled or half-closed by the peer:

- `stream.write(chunk)` may throw synchronously
  (`ERR_STREAM_DESTROYED` / `ERR_STREAM_WRITE_AFTER_END`), or
- it may emit an `'error'` event on the stream.

The exact behavior depends on Node.js version and the stream's internal
state. In both cases, an unhandled throw from inside an async
EventEmitter listener produces an unhandled promise rejection.

### B.2 `captureRejections` is not set

Searched the entire `projects/game/` tree: `captureRejections` is not
set on any EventEmitter. Node.js default behavior applies: a rejected
promise from an async listener becomes an unhandled rejection, which
triggers `process.exit(1)` (Node.js ≥15 default
`--unhandled-rejections=throw`).

---

## C. `mergeIterables` Flush-Then-Throw Pattern

### C.1 The code path (`llm.ts` lines 758-779)

```javascript
while (pending > 0 && !hasError) {
    while (queue.length > 0) yield queue.shift() as T;
    if (pending > 0 && !hasError) {
        await new Promise(...);
    }
}
// Flush items buffered up to the failure
while (queue.length > 0) yield queue.shift() as T;
if (hasError) throw firstError;
```

When any sub-stream throws (abort → `StreamChannel.fail` → `next()`
throws), `hasError` becomes `true`. The main loop exits, then
**all buffered items are yielded** before `throw firstError`.

### C.2 Why this matters

During abort, multiple consumers may have pushed items into the queue
before the error propagated. `mergeIterables` yields ALL of them to the
caller (handler.ts's `for await` loop) before throwing. Handler calls
`stream.write(frame)` for each yielded block.

If the bidi stream is already closed (peer disconnected), these writes
can throw. The throw is caught by handler.ts's try/catch. But if
`controller.signal.aborted` is still `false` at that instant (the
end/error callback hasn't fired yet — §A.6), the catch block enters the
`else` branch and calls `stream.write()` again (§D).

### C.3 017 vs 023 behavioral difference

- **017**: single consumer (`stream.messages`). On abort in v1.4.8,
  `stream.messages` also throws (§A.5). But there is no
  `mergeIterables` flush — the throw propagates directly. Any buffered
  items in the `for await` loop are simply not yielded. Handler's catch
  block runs with `controller.signal.aborted === true` (because
  `stream.on("end")` fired the abort synchronously).
- **023**: `mergeIterables` flushes buffered items THEN throws. The
  flush introduces additional `stream.write()` calls in the window
  between stream-close-detection and abort-signal-propagation. This
  window is where the crash occurs.

---

## D. Handler.ts Catch Block — Unprotected `stream.write()` is the Crash Vector

### D.1 The code (`handler.ts` lines 511-542)

```typescript
} catch (err: unknown) {
    if (controller.signal.aborted) {
        info("turn aborted on desktop disconnect", { sessionId });
    } else {
        const message = err instanceof Error ? err.message : "Processing error";
        error("LLM processing failed", { sessionId, error: message });
        const warnFrame: AgentFrame = buildFrame(...);
        stream.write(warnFrame);    // ← UNPROTECTED: can throw on closed stream
        const waitFrame: AgentFrame = buildFrame(...);
        stream.write(waitFrame);    // ← UNPROTECTED: can throw on closed stream
    }
} finally {
    activeTurns.delete(sessionId);
    this.releaseMutex(sessionId);
}
```

### D.2 The crash chain

1. `mergeIterables` yields buffered blocks during abort flush (§C.2).
2. Handler's `for await` processes a block → `stream.write(frame)`.
3. gRPC stream is already closed (peer gone) → `stream.write()` throws
   synchronously (`ERR_STREAM_DESTROYED`).
4. Throw is caught by the catch block.
5. `controller.signal.aborted` is `false` (end/error callback hasn't
   fired yet — §A.6 race).
6. Catch block enters `else` branch.
7. `stream.write(warnFrame)` → throws again (stream still closed).
8. **Throw escapes the catch block** (it's thrown from inside the
   catch body, not inside the try body).
9. `finally` runs (`activeTurns.delete`, `releaseMutex`).
10. Throw propagates out of the `stream.on("data", async (frame) => {...})`
    async callback.
11. The async callback returns a rejected Promise.
12. `captureRejections` is `false` → unhandled rejection.
13. Node.js fires `unhandledRejection` event → `process.exit(1)`.
14. **Agent service restarts.**

### D.3 Why 017 didn't crash here (mostly)

In 017, there is no `mergeIterables` flush. When `stream.messages`
throws on abort, the throw goes directly to the catch block. There are
no extra `stream.write()` calls between stream-close and abort-signal.
The `controller.signal.aborted` check in the catch block is `true`
(because `stream.on("end")` fired synchronously before the throw
propagated). The catch block enters the safe `if` branch.

The 017 crash path is **theoretically possible** but requires a much
narrower race window (a `stream.write()` inside the `for await` loop
hitting a closed stream before the end/error callback fires). The 023
`mergeIterables` flush **widens this window** by forcing extra
`stream.write()` calls during the abort propagation delay.

### D.4 Same pattern exists in try-body `stream.write()` calls

All `stream.write()` calls inside the `for await` loop (lines 434, 446,
466, 490) and the post-loop `wait` frame (line 509) are in the `try`
block. If they throw, the catch block handles them. But the catch
block's `else`-branch writes (lines 527, 537) can re-throw — and THAT
throw is not caught.

---

## E. Design Decisions

### D1 — Guard `stream.write()` in the catch block's else-branch

**Decision**: Wrap the catch block's `stream.write()` calls in a
try-catch so that a write-failure on a closed stream is logged but
does not escape the catch block.

```typescript
} catch (err: unknown) {
    if (controller.signal.aborted) {
        info("turn aborted on desktop disconnect", { sessionId });
    } else {
        // ... log error ...
        safeWrite(stream, warnFrame);
        safeWrite(stream, waitFrame);
    }
}
```

Where `safeWrite` is a helper that catches write errors on a closed
stream.

**Rationale**: This directly closes the crash vector (§D.2 step 7-12).
Even if `controller.signal.aborted` is `false` and the catch block
enters the `else` branch, a failed write no longer escapes.

**Alternatives considered**:
- **Check `stream.writableEnded` / `stream.destroyed` before writing**:
  rejected as the sole defense — there is still a TOCTOU race between
  the check and the write. Use it as an additional optimization, not
  the primary guard.
- **Set `EventEmitter.captureRejections = true`**: rejected as the
  primary fix — it converts async-listener rejections into `'error'`
  events, but the handler's `stream.on("error")` callback would then
  fire redundantly during cleanup. Useful as defense-in-depth alongside
  D1.

### D2 — Guard all `stream.write()` calls in the data callback

**Decision**: Also guard the `stream.write()` calls inside the `for
await` loop (lines 434, 446, 466, 490) and the post-loop wait frame
(line 509). These are in the `try` block so their throws are caught,
but guarding them prevents noisy error logs for an expected disconnect.

**Rationale**: During the `mergeIterables` flush, writes to a closed
stream are expected (the peer is gone). Logging them as errors is
misleading; silently dropping them is correct.

### D3 — Guard `stream.write()` calls outside the try-catch

**Decision**: The data callback has `stream.write()` calls outside the
main try-catch (status response at line 268, profile-mismatch warn/wait
at lines 348/357/372/386). Wrap these in `safeWrite` too.

**Rationale**: If the bidi stream closes between receiving a frame and
writing the response, these unprotected writes can also throw and
become unhandled rejections.

### D4 — Global `unhandledRejection` handler as defense-in-depth

**Decision**: Register `process.on("unhandledRejection", ...)` in
`bootstrap.ts` that logs the rejection but does **not** call
`process.exit(1)`. This prevents future similar bugs from silently
crashing the service.

**Rationale**: The primary fix (D1/D2/D3) closes the known crash vector.
But the category of bug (async EventEmitter listener producing an
unhandled rejection) is subtle and could recur in other code paths. A
global handler ensures the service stays alive even if a regression
slips through, while still making the rejection observable via logs.

**Alternative considered**:
- **Keep `process.exit(1)` on unhandled rejection** (Node.js default):
  rejected. For a long-running multi-session gRPC server, a single
  unexpected rejection should not terminate all sessions. Logging +
  continuing is the safer operational posture for this service class.

### D5 — Preserve 023's concurrent stream consumption model

**Decision**: Do NOT change `mergeIterables`'s flush-then-throw
behavior or `consumeToolResults`'s direct stream iteration. These are
correct designs for the 023 content-model refactor (tool_call /
tool_result live rendering). The fix is entirely in the error-handling
perimeter (handler.ts write guards + bootstrap.ts global handler), not
in the stream-consumption core.

**Rationale**: Constitution §II (Refactoring Over Patching) — the
existing design (concurrent consumers + merge) serves the goal
(tool_call/tool_result streaming); the bug is in the error-handling
edge, not the architecture. Changing the consumption model to avoid
the bug would be a patch over the real gap (unguarded writes).

---

## References

### Official Documentation

- [Node.js — unhandled-rejections behavior](https://nodejs.org/api/process.html#event-unhandledrejection) — default `--unhandled-rejections=throw` since Node.js 15.
- [Node.js — EventEmitter.captureRejections](https://nodejs.org/api/events.html#capture-rejections-of-promises) — the `captureRejections` option that is NOT set in this codebase.

### Repositories (installed source)

- `@langchain/langgraph@1.4.8` `dist/stream/run-stream.js` — `GraphRunStream` class, `stream.output` getter, `.catch(() => {})` on `#valuesDone` (line 98).
- `@langchain/langgraph@1.4.8` `dist/stream/mux.js` — `StreamMux.fail()`, `pump()` function, `pump(source, mux).catch(() => {})` (line 428).
- `@langchain/langgraph@1.4.8` `dist/stream/stream-channel.js` — `StreamChannel.iterate()`, `fail(err)` sets `#error` → `next()` throws (lines 99-106).
- `@langchain/langgraph@1.4.8` `dist/stream/transformers/messages.js` — `createMessagesTransformer.fail(err)` calls `log.fail(err)` (line 71).

### Repository-Internal Sources

- `specs/017-agent-loop-graceful-abort/research.md` §A.3 — the assumption (based on PR #9900 docs) that `streamEvents` aborts cleanly; revised by this research (§A.5 above).
- `projects/game/agent/src/llm.ts` lines 486-527 (`streamFromAgent`), 635-663 (`consumeToolResults`), 716-780 (`mergeIterables`).
- `projects/game/agent/src/handler.ts` lines 511-542 (catch block with unprotected `stream.write()`).
- `projects/game/agent/src/bootstrap.ts` lines 40-41 (only SIGTERM/SIGINT, no `unhandledRejection`).
