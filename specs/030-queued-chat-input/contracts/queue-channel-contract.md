# Contract: Queue-State Channel (agent ↔ desktop)

**Feature**: `specs/030-queued-chat-input` | **Date**: 2026-07-29
**Scope**: The wire-level behavior between the agent service and the desktop over the existing `Connect(stream AgentFrame)` bidi stream (`projects/game/game.proto:90`), for queue-state feedback. Constitution §III (Interface-First).

The queue **logic** is server-side and LangGraph-native (see `turn-loop-contract.md`, `research.md` D1). This contract defines the one **additive** wire change that lets the desktop render pending messages and transition them to normal on consume (spec FR-008/FR-009), plus the re-scoped `wait` semantics.

## 1. Inbound (desktop → agent): unchanged

- The desktop sends user content exactly as today: an `AgentFrame` with `payload = messageParts`, `sender = FRAME_SENDER_USER` (`projects/game/desktop/app.go:701` `SendUserTurn`, non-blocking per feature 015).
- **New behavior**: the desktop MAY send such a frame while a turn is in progress (the input is no longer disabled). The agent treats a frame arriving during an in-flight turn as a **queued submission** (buffered; FR-002), not as a concurrent turn. No new inbound field is required.

## 2. Outbound (agent → desktop): additive `QueueSignal`

A new `FlowPart` pushed by the agent whenever the per-session queue depth changes. It rides the existing flow channel (`FlowParts`), exactly like `wait`/`warn`/`status`.

### Proto addition (`projects/game/game.proto`)

```proto
message QueueSignal {
  // Current number of user messages buffered in the per-session queue
  // (waiting to be combined into the next agent turn).
  int32 queued_count = 1;
}

message FlowPart {
  oneof kind {
    // ... existing fields (mouse_move, mouse_click, keyboard_press,
    //     mouse_move_and_click, wait, warn, status, flow_result) ...
    QueueSignal queue = 9;   // NEW — additive, field tag 9 (next free)
  }
}
```

- Additive only: existing `FlowPart` consumers that switch on `kind` and default/ignore unknown cases are unaffected. Field tag `9` is the next free oneof tag after `flow_result = 8` (`projects/game/game.proto:316-329`).

### Emission rules (agent side)

| Trigger | Emitted frame | `queued_count` |
|----------|---------------|----------------|
| `submit` while a turn is RUNNING (buffer grows) | `FlowParts{ [QueueSignal] }` | new `buffer.length` |
| Mid-turn `drainQueue` clears the buffer — **NEW (feature 038)** | `FlowParts{ [QueueSignal] }` | `0` |
| Turn completes and buffer drained into the next turn (buffer → 0) | `FlowParts{ [QueueSignal] }` | `0` |
| Loop reaches idle (emits `wait`) | — | (depth is already 0; no extra signal required) |
| Abort clears the buffer | `FlowParts{ [QueueSignal] }` then `wait` | `0` |

The mid-turn `drainQueue` row is added by feature 038: `drainQueue` clears the buffer mid-turn (before the turn-end check), so `queued_count` drops to `0` and pending messages transition to normal display before the turn completes. Authoritative definition: `specs/038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md` (QueueSignal emission rules).

- `QueueSignal` is **pushed on change** (event-driven), not polled. The spec-021 pull-based `StatusSignal` is unchanged.

## 3. `wait` semantics (re-scoped, no proto change)

- `WaitSignal` (`projects/game/game.proto:448-450`) keeps its existing meaning ("turn/loop is idle, ready for input").
- **New emission rule** (agent side): `wait` is emitted by the TurnLoop **only when the queue is empty at a turn boundary**. Between auto-continued queued turns, the loop emits `QueueSignal(0)` and continues **without** `wait`.
- **Desktop side**: consequently, the desktop's `processing` indicator (set on send, cleared on `wait`, `projects/game/desktop/frontend/src/App.svelte:627,572`) stays `true` across queued-turn boundaries and clears exactly once when the queue is fully drained.

## 4. Desktop rendering contract (frontend)

- **Input enabled during run** (FR-001): the chat input and Send control are no longer `disabled={processing}` (`projects/game/desktop/frontend/src/components/ChatView.svelte:358,364`).
- **Submit while `processing`** (FR-002/FR-008): the message is sent immediately (server buffers it) and rendered in a **pending** visual state; the `QueueSignal.queued_count` drives the queue indicator (the existing `queueCount`/`.queue-indicator` affordance, `projects/game/desktop/frontend/src/components/ChatView.svelte:335-339`).
- **Consume transition** (FR-009): when `queued_count` decreases (and the combined turn streams its response), the pending messages transition out of the pending representation into the normal conversation view.

## 5. Compatibility & versioning

- Purely additive (`QueueSignal` is a new oneof variant; `wait`/`warn`/`status`/`flow_result` untouched). No field renumbering, no breaking change to existing frames.
- An older desktop that does not understand `QueueSignal` will ignore it (proto-loader default); the feature's core value (input-while-running, queue+combine, auto-continue) still works server-side — only the precise pending-count UI degrades gracefully.

> **Resolved**: `queued_count` alone suffices for FR-008/FR-009. The frontend already tracks the messages it sent optimistically; the backend count signal confirms consumption and drives the indicator. A richer payload (consumed message ids) is unnecessary for the MVP and would add complexity with no user-facing benefit.
