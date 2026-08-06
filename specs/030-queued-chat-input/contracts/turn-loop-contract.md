# Contract: TurnLoop (internal module API)

**Feature**: `specs/030-queued-chat-input` | **Date**: 2026-07-29
**Scope**: Internal interface between the gRPC handler (`projects/game/agent/src/handler.ts`) and the LangChain agent adapter, implemented by the new `projects/game/agent/src/turn-loop.ts`. Constitution §III (Interface-First).

This contract defines the **LangGraph-native queue + single-flight loop** that replaces the per-frame `acquireMutex → generateTurn → releaseMutex` path. Conversation continuity is provided by the existing `MemorySaver` checkpointer (repeated `streamEvents` on the same `thread_id`); the loop only owns transient buffer + lifecycle.

## Module: `TurnLoop`

Owned by `SessionAgent` (one instance per `sessionId`). Constructed with:
- `sessionId: string` (the LangGraph `thread_id`).
- `adapterProvider: () => Promise<AgentAdapter>` (lazy; resolves the bound adapter per turn — reuses `SessionAgent.getOrCreateAdapter`).
- `emit: (frame: AgentFrame) => void` (the sink the handler registered on the stream; used to push blocks, `wait`, `warn`, and `QueueSignal`).
- `profileName: string` (the effective profile for emitted frames).

### Methods

| Method | Signature | Semantics |
|--------|-----------|-----------|
| `submit` | `(content: TurnContent) => void` | **Non-blocking.** If the loop is IDLE: start it with `content` (transition IDLE→RUNNING). If RUNNING: append `content` to the FIFO buffer and push `QueueSignal(buffer.length)`. Never disturbs the in-flight `generateTurn` (FR-002). Returns immediately. |
| `isRunning` | `() => boolean` | True iff a turn is in flight or the loop is draining queued work. Feeds `deriveStatusSignal(isInFlight = isRunning(), …)` — the **only** status source (replaces `isMutexHeld`). |
| `queueDepth` | `() => number` | Current `buffer.length`. (Used by tests; not strictly required by callers.) |
| `abort` | `() => void` | Abort the in-flight turn (`controller.abort()`) **and clear the buffer** (FR-011). Transitions RUNNING→IDLE and emits `wait` so the desktop returns to ready. No-op if IDLE. |
| `drainQueue` | `() => TurnContent \| null` | **Synchronous.** If the buffer is non-empty: merge ALL buffered `TurnContent`s into one aggregated `TurnContent` via `combineAll` (FIFO order), clear the buffer, emit `QueueSignal(0)`, and return the combined content. If the buffer is empty: return `null` (no-op, no emission). Does NOT change `running` state or transition the loop — it only touches the buffer + emits the depth signal. (Added by feature 038; authoritative definition: `specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md`.) |

### Loop body (RUNNING)

```
current = initialContent
while true:
  blocks = adapter.generateTurn(threadId, current, controller.signal)
  for block of blocks: emit(displayFrame(block))           // text/thinking/tool_call/tool_result (unchanged framing)
  // turn completed (stream exhausted; await stream.output inside generateTurn)
  if controller.signal.aborted: break                       // abort path (feature 017)
  if buffer non-empty:
     current = combineAll(buffer)                            // ONE aggregated HumanMessage (multi content blocks), FIFO (D3)
     buffer.clear()
     emit(QueueSignal(0))
     continue                                                // next turn, SAME thread_id → checkpointer continues (LangGraph native)
  else:
     emit(wait)                                              // idle (FR-006)
     break
running = false
```

**Mid-turn drain (feature 038)**: `drainQueue` MAY be called in the middle of a turn by the player's `queueDrain` `beforeModel` middleware (`projects/game/agent/src/team/player.ts:184-208`), which fires before every model call within the agent loop. When it runs, it clears the buffer **before** the turn-end check in the loop body above: the `buffer non-empty` test then sees 0 (unless new messages arrived after the last `drainQueue` call, e.g. during the final tool execution — the turn-end drain catches those, unchanged). There is no double-drain: `drainQueue` clears the buffer atomically, so the turn-end check sees whatever is left (0 or new arrivals). Authoritative definition: `specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md`.

### Errors

- If `generateTurn` throws **and** the controller is **not** aborted: emit `warn` (existing error-frame path, `projects/game/agent/src/handler.ts:511-538`) and **retain** the buffer (FR-015); the loop terminates (RUNNING→IDLE) and emits `wait`. Retry strategy (auto vs manual) is implementation-defined and out of this contract's scope.

### Guarantees (mapped to spec FRs)

- FR-002: `submit` while RUNNING only touches the buffer; the in-flight `generateTurn` is isolated (LangGraph property, research.md D2).
- FR-004: FIFO buffer; `combineAll` preserves submission order.
- FR-005: `combineAll` yields exactly one aggregated `HumanMessage`.
- FR-006: `wait` emitted iff buffer empty at boundary; never an empty turn.
- FR-011: `abort` clears buffer + emits `wait`.
- FR-015: non-abort errors retain the buffer.

## Adapter change (supporting)

`AgentAdapter.generateTurn`/`streamFromAgent` (`projects/game/agent/src/llm.ts`) is generalized to accept a **multi-part** input (N text parts + M image parts) so `combineAll` can build one aggregated `HumanMessage`. The single-message shape is the N=1/M∈{0,1} case (backward compatible). See `queue-channel-contract.md` for the wire view and research.md D3 for the merge representation.
