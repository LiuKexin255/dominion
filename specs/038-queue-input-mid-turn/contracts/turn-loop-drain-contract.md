# Contract: TurnLoop Drain Queue (Internal Module API)

**Feature**: `specs/038-queue-input-mid-turn` | **Date**: 2026-08-06
**Scope**: Amendment to the TurnLoop internal interface (`specs/030-queued-chat-input/contracts/turn-loop-contract.md`). Adds a `drainQueue` method for mid-turn queue drain. Constitution §III (Interface-First).

## Amendment to `specs/030-queued-chat-input/contracts/turn-loop-contract.md`

### New method on `TurnLoop`

| Method | Signature | Semantics |
|--------|-----------|-----------|
| `drainQueue` | `() => TurnContent \| null` | **Synchronous.** If the buffer is non-empty: merge ALL buffered `TurnContent`s into one aggregated `TurnContent` via `combineAll` (FIFO order), clear the buffer, emit `QueueSignal(0)`, and return the combined content. If the buffer is empty: return `null` (no-op, no emission). Does NOT change `running` state or transition the loop — it only touches the buffer + emits the depth signal. |

### Callers

| Caller | When | Purpose |
|--------|------|---------|
| Player `createAgent` `queueDrain` `beforeModel` middleware | Before every model call within the agent loop | Inject queued messages as `HumanMessage`(s) alongside the next reasoning step (mid-turn delivery, FR-001) |
| `runLoop` turn-end check (existing, unchanged) | After the graph invoke completes | Fallback drain for messages arriving after the last `beforeModel` or during no-tool turns (FR-004) |

### Interaction between the two drains

The `drainQueue()` mid-turn drain and the `runLoop` turn-end drain operate on the **same buffer**. They are complementary:

1. `drainQueue()` is called by the middleware during the turn (0..N times depending on tool-call count).
2. After the graph invoke completes, `runLoop` checks `this.buffer.length > 0`. If `drainQueue` already emptied the buffer, this is 0 → loop emits `wait` (idle). If new messages arrived after the last `drainQueue` call (e.g., during the final tool execution), the turn-end drain catches them → next turn.

There is **no double-drain risk**: `drainQueue` clears the buffer atomically; the turn-end check sees whatever is left (0 or new arrivals).

### QueueSignal emission rules (amendment to queue-channel-contract.md §2)

| Trigger | Emitted frame | `queued_count` | Source |
|----------|---------------|----------------|--------|
| `submit` while RUNNING (buffer grows) | `FlowParts{ [QueueSignal] }` | new `buffer.length` | spec 030 (unchanged) |
| **Mid-turn `drainQueue` clears the buffer** | `FlowParts{ [QueueSignal] }` | `0` | **NEW (this feature)** |
| Turn completes and buffer drained into the next turn | `FlowParts{ [QueueSignal] }` | `0` | spec 030 (unchanged) |
| Loop reaches idle (emits `wait`) | — | (depth is already 0) | spec 030 (unchanged) |
| Abort clears the buffer | `FlowParts{ [QueueSignal] }` then `wait` | `0` | spec 030 (unchanged) |

The mid-turn `QueueSignal(0)` is the signal that transitions pending messages to normal display (FR-008) **before** the turn completes. The frontend decrements `pendingMessageIds` at this signal; `processing` stays true (no `wait`).

### Thread safety

JavaScript is single-threaded. `submit` (synchronous buffer push + QueueSignal emit) and `drainQueue` (synchronous buffer read + clear + QueueSignal emit) cannot interleave. A message submitted between two `drainQueue` calls is either seen by the next call or left for the turn-end drain — both are correct.
