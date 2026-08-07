# Research: Queued Input Mid-Turn Injection & Bubble Continuity

**Feature**: `specs/038-queue-input-mid-turn` | **Date**: 2026-08-06

## D1. Mid-Turn Injection Mechanism: `beforeModel` Middleware

**Decision**: Use `langchain`'s `createAgent` `beforeModel` middleware to drain the TurnLoop queue and inject queued messages as `HumanMessage`(s) before each LLM call within the agent's internal loop.

**Rationale**:
- The `beforeModel` hook fires before **every** model invocation within the `createAgent` loop (`langchain/dist/agents/middleware/types.d.ts:169-185`). This is exactly the "tool-result boundary" — the moment after tools have completed and the agent is about to reason again.
- The hook can return `{ messages: [...] }` as a `MiddlewareResult` state update (`types.d.ts:69-71`). The agent's `messages` channel uses `messagesStateReducer`, so returned messages are **appended** to the conversation state before the model call — this is how HumanMessages get injected.
- The mechanism is already proven in the codebase: the player's `gameEndGuard` middleware (`player.ts:157-169`) uses `beforeModel` with `canJumpTo: ["end"]` / `jumpTo` to stop the loop. Adding a second middleware for queue drain follows the same pattern.
- The `runtime.configurable` object (`runtime.d.ts:55-60`) carries arbitrary keys from the `streamEvents` config, so a `drainQueuedInput` callback can be passed through the same path as the existing `emitChannelFrame` (`session-team.ts:301-328`).

**Alternatives considered**:
1. **Restructure the turn loop** (break `createAgent` internal loop into TurnLoop-driven per-LLM-call turns, like opencode's provider-turn loop): Rejected — massive refactor of the team graph architecture. The `createAgent` is a compiled LangGraph subgraph that handles its own model↔tool loop internally; exposing each step to the TurnLoop would require replacing `createAgent` with a hand-built graph, losing all of its built-in middleware/tool-streaming/error-handling.
2. **LangGraph `interrupt()`**: Rejected — `interrupt()` pauses for REQUIRED input and blocks the graph until resumed. Queued input is OPTIONAL and must never pause the agent (documented in `turn-loop.ts:28-31`). `interrupt()` is the wrong semantics.
3. **`graph.updateState()` mid-invoke**: Rejected — calling `updateState` while the graph is running is unsafe and can corrupt the checkpoint. LangGraph's checkpoint model is designed for sequential super-steps, not concurrent mutations.

**Verification of `beforeModel` return semantics**: The `MiddlewareResult<TState>` type is `(TState & { jumpTo? }) | void` (`types.d.ts:69-71`). The `beforeModel` hook returns `PromiseOrValue<MiddlewareResult<Partial<InferSchemaUpdateType<TSchema>>>>` (`types.d.ts:176`). For the built-in state (`AgentBuiltInState`, `runtime.d.ts:10-37`), the update type includes `{ messages?: BaseMessage[] }`. Returning `{ messages: [humanMsg] }` is a valid partial state update; the agent applies it through the `messagesStateReducer`, appending the message. This is the standard LangGraph middleware pattern.

## D2. TurnLoop `drainQueue()` Method Design

**Decision**: Add a synchronous `drainQueue(): TurnContent | null` method to `TurnLoop` that atomically clears the buffer, emits `QueueSignal(0)`, and returns the combined content.

**Rationale**:
- The existing `combineAll(buffer)` function (`turn-loop.ts:173-184`) already merges all buffered `TurnContent`s into one aggregated `{ parts }` and clears the buffer. `drainQueue` wraps it with the QueueSignal emission.
- Emitting `QueueSignal(0)` on drain is required by `specs/030-queued-chat-input/contracts/queue-channel-contract.md §2` ("Turn completes and buffer drained → QueueSignal(0)"). The mid-turn drain is a new trigger for the same signal — the frontend uses it to transition pending messages to normal (FR-008).
- The method is synchronous: JavaScript is single-threaded, so `submit` (buffer push) and `drainQueue` (buffer read+clear) cannot interleave. A message is either in the buffer when drain runs or it isn't — both are safe.

**Interaction with the existing turn-end drain**: The `runLoop` body (`turn-loop.ts:356-360`) checks `this.buffer.length > 0` after the graph invoke completes. If `drainQueue` already cleared the buffer mid-turn, this check sees 0 and the loop emits `wait` (idle). If new messages arrived after the last `beforeModel` (e.g., during the final tool execution), the turn-end drain catches them. The two drains are complementary, not redundant.

**First `beforeModel` call**: On the first model call of a turn, the buffer may already contain messages submitted after the turn started (messages 2, 3, ... while message 1 started the turn). Draining here injects them into the very first LLM call — the agent sees all queued messages from the start. This is the desired behavior: no reason to wait for a tool call to deliver messages that arrived before the model started thinking. The spec's "earliest tool-result boundary" intent is satisfied — the first model call is even earlier than the first tool boundary.

## D3. Frontend Bubble Continuity: Backward-Scan Merge

**Decision**: Modify `App.svelte:handleMessageParts` (lines 727-745) to search backwards past interleaved USER entries when finding the merge target for agent text/thinking chunks.

**Rationale**:
- The current merge logic checks only `list[list.length - 1]`. When a USER entry is interleaved (optimistic insertion in `handleSendChatText:886-895`), the last entry is USER, the merge fails, and a new agent bubble is created.
- The fix: iterate backwards from the end of the list, skipping USER entries, to find the last AGENT entry with matching `agent` and `mergeKind`. Stop at any non-USER, non-matching entry. If found, merge into that entry (append content to its trailing part).
- Visual result: the agent's continuous bubble stays in its original position (before the user message); the queued user message remains below it. The bubble grows upward as more chunks arrive. This matches the user's confirmed preference ("Agent bubble above, user below").

**Edge case — mixed roles between merge target and current**: If the list is `[agent-thinking, user, warn, <new-thinking-chunk>]`, the backward scan hits `warn` (role AGENT but `mergeKind !== 'thinking'`) and stops. The new chunk starts a fresh entry. This is correct — a warn entry is a control-signal bubble that legitimately breaks the streaming chain.

## D4. opencode Steer vs Queue Reference

**Decision**: dominion's mid-turn injection is the behavioral equivalent of opencode's "steer" delivery. dominion does NOT adopt opencode's dual-mode (steer + queue) distinction — all queued messages are injected at the earliest opportunity (steer semantics), with the turn-end drain as the fallback.

**Rationale**:
- opencode distinguishes "steer" (mid-turn injection at provider-turn boundaries) from "queue" (idle-only delivery). The user queues a message and it's tagged as one or the other.
- dominion's architecture is simpler: there is one FIFO buffer (spec 030), and the user has no way to choose delivery mode. The user's intent is always "deliver ASAP". The mid-turn drain (via `beforeModel`) delivers at the earliest boundary; the turn-end drain delivers at the latest. There is no need for a user-facing delivery-mode distinction.
- opencode source reference: `packages/core/src/session/input.ts` (`promoteSteers` — promotes all steer messages with `admitted_seq <= cutoff` at turn start), `packages/core/src/session/runner/llm.ts` (the `run` loop sets `promotion = "steer"` after each provider turn, line ~`promotion = "steer"`). The cutoff concept maps to dominion's `beforeModel` boundary (each model call is a "cutoff" point).

**Source verified**: `https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/core/src/session/input.ts` (`promoteSteers`, `promoteNextQueued`), `https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/core/src/session/runner/llm.ts` (run loop). The repo's default branch is `dev`, not `main`.

## D5. QueueSignal Emission Timing

**Decision**: `drainQueue()` emits `QueueSignal(0)` immediately on drain, before the injected message reaches the model. The frontend decrements its pending indicator at this signal.

**Rationale**:
- The `QueueSignal` is the contract-defined mechanism for queue-depth feedback (`queue-channel-contract.md §2`). The mid-turn drain is a new trigger for `QueueSignal(0)` — the same signal the turn-end drain emits.
- The frontend (`App.svelte:816-818`) shifts `pendingMessageIds` when `queueCount` drops. With `QueueSignal(0)` emitted mid-turn, the pending messages transition to normal display immediately, while `processing` stays true (no `wait` signal). This is correct — the message has been consumed; the agent is still working.
- The `queue-channel-contract.md` emission table needs an additive row for the mid-turn drain trigger. See `contracts/turn-loop-drain-contract.md`.

## D6. Scope Limitation: Planner Agent

**Decision**: The `queueDrain` middleware is added ONLY to the player's `createAgent`, not the planner's.

**Rationale**:
- The planner does not accept user input (spec 031 FR-031/FR-032: `accepts_user_input = false`). User messages are never routed to the planner.
- The TurnLoop's queue is per-session and serves the primary input-accepting agent (`player`). Messages queued during a planner run are drained at the next player tool boundary or at turn end.
- Adding the middleware to the planner would be dead code (the drain callback would always return null for the planner).
