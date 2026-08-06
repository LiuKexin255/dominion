# Contract: Mid-Turn Injection Seam (configurable callback)

**Feature**: `specs/038-queue-input-mid-turn` | **Date**: 2026-08-06
**Scope**: The internal injection seam between `SessionTeam.runTeamTurn` (provider) and the player `createAgent` `beforeModel` middleware (consumer), carried through the LangGraph `streamEvents` `configurable` object. Constitution §III (Interface-First).

This contract follows the same `configurable`-injection pattern as the existing `emitChannelFrame` callback (`session-team.ts:301-328`, `specs/037-saolei-team-optimize`).

## 1. The callback

```text
type DrainQueuedInput = () => TurnContent | null
```

- **Returns**: the combined queued content (all buffered messages, FIFO order) if the TurnLoop buffer was non-empty; `null` if the buffer was empty.
- **Side effect**: when non-null, the TurnLoop buffer is cleared and `QueueSignal(0)` is emitted (see `turn-loop-drain-contract.md`).
- **Synchronous**: the callback returns immediately (no async, no promise). The TurnLoop's `drainQueue()` is a synchronous array operation.

## 2. Provider: `SessionTeam.runTeamTurn`

`runTeamTurn` (`session-team.ts:286-466`) passes the `streamEvents` config. The `configurable` object gains a new key alongside `thread_id` and `emitChannelFrame`:

```text
configurable: {
  thread_id: this.sessionId,
  emitChannelFrame: (...),     // existing (spec 037)
  drainQueuedInput: () => this.turnLoop?.drainQueue() ?? null,   // NEW
}
```

- `this.turnLoop` is set in `SessionTeam.submit()` before `runTeamTurn` is called (the runner closure captures `this`; `turnLoop` is assigned once per session and never reassigned).
- If `turnLoop` is null (defensive — should not happen during a running turn), the callback returns `null` (no-op).

## 3. Consumer: player `queueDrain` `beforeModel` middleware

The player's `createAgent` middleware array (`player.ts:148-170`) gains a second entry:

```text
{
  name: "queueDrain",
  beforeModel: {
    hook: (_state, runtime) => {
      const drain = runtime.configurable?.drainQueuedInput
      if (typeof drain !== "function") return       // no callback → no-op
      const drained = drain()
      if (!drained) return                           // empty buffer → no-op
      return {
        messages: [new HumanMessage({ content: buildContentBlocks(drained) })],
      }
    },
  },
}
```

### Hook semantics

| Condition | Return value | Effect |
|-----------|-------------|--------|
| No `drainQueuedInput` in configurable | `void` (undefined) | No-op; model proceeds with current messages |
| `drain()` returns `null` (empty buffer) | `void` (undefined) | No-op; model proceeds with current messages |
| `drain()` returns `TurnContent` | `{ messages: [HumanMessage] }` | HumanMessage appended to conversation state via `messagesStateReducer` before model call |

### When the hook fires

The `beforeModel` hook fires before **every** model invocation within the `createAgent` internal loop:
1. **First model call** (turn start): the buffer may contain messages submitted after the turn started (messages 2, 3, ...). Draining here injects them into the first LLM call — the agent sees all queued messages from the start.
2. **Subsequent model calls** (after tool results): the buffer may contain messages submitted during tool execution. Draining here injects them alongside the tool results — mid-turn delivery (FR-001).

### `buildContentBlocks` reuse

The `buildContentBlocks(content: TurnContent)` function (`llm.ts`) converts a `TurnContent` to LangChain content blocks (text parts + image parts). It is already used by `runTeamTurn` to build the initial `HumanMessage` (`session-team.ts:293`). The middleware reuses the same function — the injected message has the identical content-block shape as the turn-start message.

## 4. Scope limitation

- **Player only**: the `queueDrain` middleware is added to the player's `createAgent`, NOT the planner's. The planner does not accept user input (spec 031 FR-031); its `createAgent` has no `drainQueuedInput` in its configurable, and the middleware is not registered (D6).
- **Single drain per model call**: the hook calls `drain()` once per invocation. If new messages arrive during the model call (between `beforeModel` and `afterModel`), they are caught by the next `beforeModel` or the turn-end drain.

## 5. Compatibility

- **No proto change**: the injection is entirely within the agent's TypeScript layer. No new wire-format field.
- **No graph-structure change**: the middleware is added to the existing `createAgent` middleware array; the team graph's nodes and edges are unchanged.
- **Backward compatible with existing middleware**: the `gameEndGuard` middleware (`player.ts:157-169`) is unaffected — LangGraph runs middleware hooks in registration order; `queueDrain` runs alongside (or after) `gameEndGuard` without interference (different concerns: one stops the loop on game-end, the other injects queued messages).
