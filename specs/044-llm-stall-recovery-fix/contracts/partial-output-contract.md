# Contract: Partial Agent Output Persistence on Stall

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](../spec.md) FR-004/FR-005/FR-006/FR-007

This contract defines the interface by which the agent persists the already-streamed partial output of a stalled node to the checkpoint, so it survives session reconnection (`ListMessages` returns it). It compensates for LangGraph's `idleTimeout` discarding the stalled node's buffered writes (`task.writes.splice`, `@langchain/langgraph` `dist/pregel/timeout.js:200-211`).

## §1 When persistence fires

`projects/game/agent/src/session-team.ts:runTeamTurn` (the `async *` generator at `:725-912`) wraps its `for await (const event of stream)` loop and the trailing `await stream.output` (`:911`) in:

```typescript
try {
  for await (const event of stream) { /* yield blocks (accumulated) */ }
  await stream.output;
} catch (err) {
  if (isNodeTimeoutError(err) && err.kind === "idle") {
    await this.persistPartialOutput(err, partialBlocks);
  }
  throw err;   // MUST re-throw so runLoop → finishError (043 warn+wait, retain buffer) runs unchanged
}
```

**Guards**:
- ONLY on an idle `NodeTimeoutError` (`err.kind === "idle"`). Other errors (GraphRecursionError, model/tool errors) are NOT persisted — they re-throw directly and take the existing `finishError` path with no partial-write.
- ONLY for the `player`/`planner` nodes (the only nodes with `idleTimeout` configured — §3 of idle-timeout-contract). `initInstruction`/`postCompactInstruction`/`compress` stalls are not in scope (they use the init-turn total timeout).
- After persistence, the error is **always re-thrown** — persistence is compensation, not recovery. 043's `finishError` (warn + wait, retain queued buffer) runs unchanged (spec FR-010).

## §2 What is persisted (partitioned by stalled node)

`persistPartialOutput(err, partialBlocks)`:

1. `stalledAgent = agentFromNamespace(err.node)` — the stalled node name normalized to `"player"`/`"planner"` (`NodeTimeoutError.node` is the `addNode` name; if namespaced by a subgraph, normalized — research.md R7).
2. Filter `partialBlocks` to `b.agent === stalledAgent`. **Only the stalled node's streamed blocks are persisted** — prior completed nodes already committed their writes to the checkpoint; persisting their blocks would duplicate (research.md R7).
3. `channel = stalledAgent === "player" ? "playerMessages" : "plannerMessages"` (matches `AGENT_CHANNELS`, `handler.ts:82-83`).
4. `{ aiMessage, toolMessages } = mergePartialBlocks(filteredBlocks)` (§3).
5. If **both** are empty (nothing was streamed before the stall — e.g. stall at first byte), this is a **no-op** (spec US3.4): no empty/truncated artifact is fabricated.
6. Otherwise: `await this.graphHandle.graph.updateState({ configurable: { thread_id: this.sessionId } }, { [channel]: [aiMessage, ...toolMessages] })`.

`messagesStateReducer` appends the new messages (fresh `id`s) to the channel — no collision with prior messages.

## §3 Merge rules — `TurnBlock[]` → messages

`mergePartialBlocks(blocks: TurnBlock[]): { aiMessage: AIMessage; toolMessages: ToolMessage[] }` (new helper in `session-team.ts`):

| Input block(s) | Output | Rule |
|---|---|---|
| `text` blocks | AIMessage `content[]` `{ type:"text", text:<concatenated, stream order> }` | If this is the interrupted tail (last block overall), attach `additional_kwargs:{ interrupted:true }` (§4). |
| `reasoning` blocks | AIMessage `content[]` `{ type:"reasoning", reasoning:<concatenated> }` | Same interrupted-tail marking. |
| `tool_call` with a retained `tool_result` | AIMessage `tool_calls[]` `{ id, name, args }` | Complete call retained (history shows call + result linked by `tool_id`). |
| `tool_call` without a `tool_result` | — (dropped) | Mid-flight partial call — cannot dispatch, corrupts tool history (spec FR-006). |
| `tool_result` blocks | standalone `ToolMessage` each | Side effect already executed on the desktop → retained (`tool_call_id`, message/screenshot content, `additional_kwargs.toolResultStatus`). |

**Resulting AIMessage shape** round-trips through `ListMessages` identically to a normal AI reply (reconstruction `handler.ts:668-717` reads `content` array → reasoning/text parts + `tool_calls`).

## §4 "Interrupted" flag (per-content-block)

The content block that was mid-stream at the stall (typically the last block of the partial) carries `additional_kwargs.interrupted = true` (exact key finalized in tasks.md; `additional_kwargs` is consistent with `toolResultStatus` carriage — `llm.ts:428-435`). **Only that block is marked** — earlier completed blocks in the same partial are unmarked (spec FR-005, Session 2026-08-12 clarification).

`ListMessages` reconstruction (`handler.ts:668-717`) is extended minimally: when emitting a `MessagePart` for a content block carrying `additional_kwargs.interrupted`, it sets an `interrupted:true` marker on the emitted part. The desktop renders this (desktop-rendering-contract.md §2). A normal complete AIMessage carries no such marker.

## §5 Feasibility gate

`graph.updateState` is called AFTER the stall's AbortSignal fired. This MUST be confirmed by an empirical spike (research.md R4) in the first implementation task: build a graph with a `MemorySaver`, start `streamEvents`, trigger an idle `NodeTimeoutError`, call `updateState` in the catch, then `getState` — assert the values are present. Expected: succeeds (updateState is an independent checkpointer mutation, not bound to the aborted invocation). Contingency if it fails: write via a fresh graph interaction or direct checkpointer `.put` (documented in tasks.md).

## §6 Interaction with 043 (unchanged behaviors)

- **Queued-message buffer (043 FR-006/FR-007)**: retained and auto-drained on the next turn — unchanged. The partial agent output is an **additional** retention (in the message history), independent of the user-input buffer (spec Edge Cases).
- **warn + wait (043 FR-005/FR-008)**: emitted by `finishError` after the re-throw — unchanged.
- **Abort path (043 FR-011 / Feature 026)**: partial-output persistence applies ONLY to stall-induced termination, NOT abort (an abort is an intentional halt; spec FR-011). The `catch` only acts on `NodeTimeoutError`; an abort takes the `runLoop` abort branch (`turn-loop.ts:353-354`), which does not invoke `persistPartialOutput`.
- **Model-visible**: the persisted AIMessage lands in the per-agent channel the model reads, so the next turn's model sees its truncated reply and can continue (spec Clarifications Q1).

## §7 Verification

- Unit (`session-team.test.ts`): mock stream yields N blocks then rejects with idle `NodeTimeoutError` → `updateState` called with merged AIMessage in `err.node`'s channel; error re-thrown; `finishError` still warn+wait + retained buffer.
- Unit: multi-node turn (player complete → planner stall) → `updateState` writes ONLY `plannerMessages` (player not duplicated).
- Unit: `mergePartialBlocks` — text+reasoning; reasoning-only; tool_call+result kept; tool_call without result dropped; empty → no-op.
- Unit (`handler.test.ts`): `ListMessages` returns the partial AIMessage with the interrupted marker on the flagged part; normal AIMessage unmarked.
- Large test: stall mid-reply → reconnect → `ListMessages` returns the partial output (spec SC-002/SC-003).
