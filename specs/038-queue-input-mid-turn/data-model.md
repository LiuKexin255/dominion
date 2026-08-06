# Data Model: Queued Input Mid-Turn Injection & Bubble Continuity

**Feature**: `specs/038-queue-input-mid-turn` | **Date**: 2026-08-06

This feature adds no new persistent entities or proto messages. All changes are to transient in-memory state (TurnLoop buffer, createAgent middleware) and frontend rendering logic. This document describes the state-machine and data-flow changes.

## 1. TurnLoop State Machine (Amendment)

The TurnLoop state machine (`specs/030-queued-chat-input/data-model.md`) is unchanged in its top-level transitions (IDLE → RUNNING → IDLE). The change is a **new drain trigger within the RUNNING state**.

### Existing transitions (unchanged)

| From | Event | To | Action |
|------|-------|----|--------|
| IDLE | `submit(content)` | RUNNING | start `runLoop(content)` |
| RUNNING | `submit(content)` | RUNNING | buffer.push; emit `QueueSignal(buffer.length)` |
| RUNNING | turn done, buffer non-empty | RUNNING | `combineAll(buffer)`; emit `QueueSignal(0)`; next turn |
| RUNNING | turn done, buffer empty | IDLE | emit `wait` |
| RUNNING | abort | IDLE | clear buffer; emit `QueueSignal(0)`; emit `wait` |
| RUNNING | non-abort error | IDLE | retain buffer; emit `warn`; emit `wait` |

### NEW transition (mid-turn drain)

| From | Event | To | Action |
|------|-------|----|--------|
| RUNNING | `beforeModel` hook fires (inside createAgent loop) | RUNNING | `drainQueue()`: if buffer non-empty → `combineAll(buffer)`, emit `QueueSignal(0)`, return combined content for injection; if empty → no-op |

The `drainQueue()` method is callable from outside `runLoop` (by the `beforeModel` middleware via the `configurable.drainQueuedInput` callback). It does NOT change `this.running` or transition the loop — it only touches the buffer and emits the depth signal. The loop's own drain check (`runLoop:356`) still runs at turn-end; if `drainQueue` already emptied the buffer, the loop sees 0 and goes idle.

## 2. `drainQueue()` Method Signature

```text
TurnLoop.drainQueue(): TurnContent | null
```

- **Returns**: the combined `TurnContent` (all buffered messages merged via `combineAll`, FIFO order) if the buffer was non-empty; `null` if the buffer was empty (no-op).
- **Side effect**: emits `QueueSignal(0)` via the loop's `emit` sink when the buffer was non-empty.
- **Atomicity**: synchronous; the buffer is cleared and the content returned in one step. No partial drain.
- **Idempotency on empty**: calling `drainQueue()` when the buffer is empty returns `null` and emits nothing.

## 3. Injection Seam: `configurable.drainQueuedInput`

The `runTeamTurn` method (`session-team.ts:286-466`) passes its `streamEvents` config with a `configurable` object. A new key is added:

```text
configurable.drainQueuedInput: (() => TurnContent | null) | undefined
```

- **Provider**: `SessionTeam.runTeamTurn` sets it to `() => this.turnLoop?.drainQueue() ?? null`.
- **Consumer**: the player's `queueDrain` `beforeModel` middleware reads `runtime.configurable?.drainQueuedInput` and calls it.
- **Scope**: the player node's `createAgent` only. The planner node does not read it (D6).

## 4. `beforeModel` Middleware: `queueDrain`

A new middleware added to the player's `createAgent` middleware array (alongside the existing `gameEndGuard`):

| Aspect | Value |
|--------|-------|
| Name | `"queueDrain"` |
| Hook | `beforeModel` |
| `canJumpTo` | not set (no jump — injection only) |
| Hook logic | read `runtime.configurable?.drainQueuedInput`; call it; if non-null, return `{ messages: [new HumanMessage({ content: buildContentBlocks(drained) })] }`; if null, return `void` (no-op) |
| Fires | before every model call within the createAgent loop (including the first call of the turn) |

The returned `{ messages: [...] }` is applied by the createAgent's state update mechanism through `messagesStateReducer` — the HumanMessage is appended to the conversation before the model sees it.

## 5. Frontend Merge State (Amendment)

### Current `ChatEntry` merge logic

`App.svelte:handleMessageParts` flattens streaming agent frames into `ChatEntry` items. The `mergeKind` field (`'text' | 'thinking' | 'mixed'`) tracks whether consecutive agent frames of the same kind should fold into the preceding entry.

### Change: backward-scan merge target

When an agent text/thinking frame arrives and the immediate last entry is a USER message (optimistic insertion), the merge logic now scans **backwards** past USER entries to find the last AGENT entry with matching `agent` and `mergeKind`. A matching AGENT entry whose `parts` array is **empty** is skipped — the scan continues backward — so it is neither chosen as the merge target (appending to an entry with no trailing part would be a no-op) nor does it break the chain (bubble continuity is preserved). Implemented as `findMergeTarget` in `projects/game/desktop/frontend/src/stream-merge.ts:48-62`:

```text
for i from list.length-1 downto 0:
  entry = list[i]
  if entry.role == AGENT and entry.agent == agent and entry.mergeKind == kind:
    if entry.parts.length > 0:
      → merge target found at i; append content to trailing part
      → break
    else:
      → continue   // matches but parts empty — skip, keep scanning backward
                   // for an earlier populated entry (does not break the chain)
  if entry.role != USER:
    → break (non-USER entry that doesn't match breaks the chain)
  // else: skip USER entry, continue backward scan
```

If no merge target is found, a new entry is created (the existing behavior for non-merge cases).

### Data flow impact

- No change to the `ChatEntry` type or the `chatMessages` bucket structure.
- No change to the `renderItems` derivation in `ChatView.svelte`.
- The only change is in how the trailing agent entry is located for content concatenation.
