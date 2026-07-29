# Data Model: Queued Chat Input During Agent Run

**Feature**: `specs/030-queued-chat-input` | **Date**: 2026-07-29

This feature adds **transient, in-memory** per-session state (a queue + loop) on top of the existing LangGraph `MemorySaver` checkpointer. It introduces **no new persisted entity**: conversation history remains the checkpointer's responsibility (the LangChain-native multi-turn store). The new state is the **TurnLoop**'s runtime buffer and lifecycle.

## Entities

### QueuedMessage (transient, in-memory)

A user-authored submission received while a turn is in flight, held until the next turn boundary.

| Field | Type | Notes |
|-------|------|-------|
| `text` | `string` | The user's text. Required (desktop enforces non-empty on send). |
| `image` | `{ data: Uint8Array; mime: string; widthPx: number; heightPx: number } \| undefined` | Optional screenshot attachment (spec FR-012). |
| `submittedAt` | `number` | Monotonic timestamp; establishes FIFO ordering within the buffer (spec FR-004). |

**Relationship**: belongs to exactly one **InputQueue** (per session). Consumed in FIFO order; merged into a single aggregated `HumanMessage` (research.md D3) at the turn boundary, then discarded.

### InputQueue / TurnLoop (transient, in-memory, per session)

The LangGraph-native queue + single-flight loop, owned by `SessionAgent` (`projects/game/agent/src/session-agent.ts`). One instance per `sessionId`.

| Field | Type | Notes |
|-------|------|-------|
| `sessionId` | `string` | The thread id (= LangGraph `thread_id`). |
| `buffer` | `QueuedMessage[]` (FIFO) | Pending submissions awaiting the next turn. Empty at idle. |
| `running` | `boolean` | True while a turn is in flight **or** the loop is draining. Feeds `deriveStatusSignal(isInFlight=running, …)` (research.md D5). |
| `controller` | `AbortController \| undefined` | The in-flight turn's abort handle (reuses the existing per-session abort model, `projects/game/agent/src/handler.ts:204-215`). Present iff `running`. |

**Relationships**:
- Owns the `buffer` of `QueuedMessage`s.
- Drives `AgentAdapter.generateTurn` (= LangGraph `streamEvents`) once per turn; on completion drains `buffer` (combine → next turn) or emits `wait` (idle).
- Pushes `QueueSignal` (`queued_count = buffer.length`) on every depth change (research.md D4).

### Turn Boundary (`wait` signal) — unchanged semantics, re-scoped emission

The existing turn-complete `WaitSignal` (`projects/game/game.proto:448-450`), emitted at the end of `generateTurn`. **This feature changes when it is emitted, not what it means**: `wait` is emitted by the TurnLoop **only when the buffer is empty at a turn boundary** (idle). Between auto-continued queued turns the loop continues without `wait` (research.md D5).

## State Transitions — TurnLoop lifecycle

```text
                 submit(content) [idle]
        ┌─────────────────────────────────────┐
        ▼                                     │
   ┌─────────┐  submit(content) [running]  ┌─────────┐
   │  IDLE   │ ─────────────────────────▶  │ queued  │ (buffer.push; emit QueueSignal +1)
   └─────────┘                             └─────────┘
        │                                       │
        │ submit(content) starts loop           │
        ▼                                       │
   ┌──────────────────────────────────────────────────┐
   │                    RUNNING                        │
   │  current = content                                │
   │  loop:                                            │
   │    generateTurn(threadId, current) → stream blocks│
   │    on completion:                                 │
   │      if buffer non-empty:                         │
   │        current = combineAll(buffer) (FIFO)        │
   │        buffer.clear(); emit QueueSignal 0         │
   │        continue loop  (next turn, same thread_id) │
   │      else:                                        │
   │        emit wait  → IDLE                          │
   └──────────────────────────────────────────────────┘
        │ abort() / stream end·error (feature 017)
        ▼
   buffer.clear(); emit wait → IDLE   (FR-011: abort discards queue)
```

**Transitions** (spec-aligned):

| From | Event | To | Side effects |
|------|-------|----|--------------|
| IDLE | `submit(content)` | RUNNING | `running=true`; start loop with `content`; `QueueSignal` unchanged (0). |
| RUNNING | `submit(content)` | RUNNING | `buffer.push(content)` (FIFO); `QueueSignal +1`. (In-flight turn is **not** disturbed — FR-002.) |
| RUNNING | turn completes, `buffer` non-empty | RUNNING | `current = combineAll(buffer)` (one aggregated `HumanMessage`, D3); `buffer.clear()`; `QueueSignal 0`; next `generateTurn` on same `thread_id`. |
| RUNNING | turn completes, `buffer` empty | IDLE | emit `wait`; `running=false`. |
| RUNNING | abort / stream end·error | IDLE | `controller.abort()`; `buffer.clear()` (FR-011); emit `wait`; `running=false`. |
| RUNNING | turn errors (non-abort) | RUNNING (retain) | `buffer` **retained**; emit `warn` (FR-015); retry strategy is implementation-defined. |

## Validation rules (from requirements)

- **FR-002**: a `submit` while RUNNING never reaches the in-flight `generateTurn` (buffered only). Enforced structurally by the loop.
- **FR-004**: `buffer` is FIFO; `combineAll` preserves `submittedAt` order.
- **FR-005**: `combineAll` produces exactly **one** aggregated `HumanMessage` (multiple content blocks) per drain.
- **FR-006**: `wait` is emitted iff `buffer` is empty at a turn boundary; never an empty turn.
- **FR-011**: abort transitions RUNNING→IDLE and clears `buffer`.
- **FR-015**: a turn error retains `buffer` (no drop).
- **Status (spec 021)**: `STATUS_ACTIVE` ⇔ `running`; `STATUS_IDLE` ⇔ `!running && adapter bound`. Derived by the unchanged pure `deriveStatusSignal` (`projects/game/agent/src/status-signal.ts`).
