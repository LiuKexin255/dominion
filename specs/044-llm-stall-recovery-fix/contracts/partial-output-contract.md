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
| `text` blocks | AIMessage `content[]` `{ type:"text", text:<concatenated, stream order> }` | Carry `additional_kwargs:{ interrupted:true }` ONLY when the **overall-last** streamed block is a `text` block (a content block was mid-stream at the stall) — see "Marking rule" below. |
| `reasoning` blocks | AIMessage `content[]` `{ type:"reasoning", reasoning:<concatenated> }` | Same; marked ONLY when the overall-last streamed block is `reasoning`. |
| `tool_call` with a retained `tool_result` | AIMessage `tool_calls[]` `{ id, name, args }` | Complete call retained (history shows call + result linked by `tool_id`). |
| `tool_call` without a `tool_result` | — (dropped) | Mid-flight partial call — cannot dispatch, corrupts tool history (spec FR-006). |
| `tool_result` blocks | standalone `ToolMessage` each | Side effect already executed on the desktop → retained (`tool_call_id`, message/screenshot content, `additional_kwargs.toolResultStatus`). |

**Resulting AIMessage shape** round-trips through `ListMessages` identically to a normal AI reply (reconstruction `handler.ts:668-717` reads `content` array → reasoning/text parts + `tool_calls`).

### Marking rule (which block carries `interrupted`)

The flag is placed on the merged content block (text or reasoning) **only when a content block was mid-stream at the stall** — determined by inspecting the **overall-last streamed `TurnBlock`** (after filtering to the stalled node, in stream order). Content is streamed sequentially, so the block being produced when the idle timer elapsed is necessarily the last streamed block:

| Overall-last streamed block | Mid-stream content block? | Marking |
|---|---|---|
| `text` | yes (it was being streamed when the stall hit) | merged text block → `additional_kwargs.interrupted = true` |
| `reasoning` | yes | merged reasoning block → `additional_kwargs.interrupted = true` |
| `tool_call` | no (a call was dispatched but no content was streaming) | **no content block marked** |
| `tool_result` | no (a tool completed; the next model invocation emitted nothing before the stall — "tool-gap") | **no content block marked** |

This is the precise operationalization of spec FR-005 ("the specific content block that was mid-stream … earlier fully-streamed blocks MUST NOT be marked"). The previous design (research.md R6) assumed the partial is at most `[reasoning…][text…]` (single model invocation) and tracked "last-seen text/reasoning kind" (`tailKind` updated inside the accumulation loop); that under-specifies the tool-gap case — it would mark a fully-streamed pre-tool text block, violating FR-005. The overall-last-block rule corrects this. (research.md R6 is a point-in-time research record; this contract supersedes its merge/marking assumption for implementation.)

**Known imprecision (accepted for v1)**: the merge concatenates ALL text into one block and ALL reasoning into one block, ignoring tool-call segment boundaries. In a multi-step partial like `[text1, tool_call, tool_result, reasoning2, text2(truncated)]`, the marked merged-text block (text1+text2) includes fully-streamed text1 alongside truncated text2. This is accepted because: (a) the merged block DOES contain the truncated content (it is not a false positive — contrast the tool-gap case, which is handled correctly by marking nothing); (b) `ListMessages` reconstruction (`handler.ts:674-695`) merges all text content blocks into a single text `MessagePart` regardless (`textBlocks.map(...).join("")`), so segment-aware merging would not improve display precision; (c) the model-visible continuity purpose (FR-007) does not require segment distinction. Segment-aware merge is a possible future refinement if the display ever gains per-segment granularity.

## §4 "Interrupted" flag (per-content-block)

The content block that was mid-stream at the stall (typically the last block of the partial) carries `additional_kwargs.interrupted = true` (exact key finalized in tasks.md; `additional_kwargs` is consistent with `toolResultStatus` carriage — `llm.ts:428-435`). **Only that block is marked** — earlier completed blocks in the same partial are unmarked (spec FR-005, Session 2026-08-12 clarification). The precise rule for determining "mid-stream" (including the tool-gap case) is defined in §3 "Marking rule".

**Tool-gap case (no content block was mid-stream)**: when the stall occurs after a tool completed but before the next model invocation emitted any content (the overall-last streamed block is a `tool_call` or `tool_result`), NO content block carries the flag — there was no truncated text/reasoning to mark, and marking the earlier fully-streamed text would violate FR-005 ("earlier fully-streamed blocks MUST NOT be marked"). The persisted partial reflects a complete tool round-trip (a coherent conversation state); the user was already notified in real-time by the ⚠ `warn` bubble (FR-012). A turn-level/message-level interrupted signal was explicitly rejected by the user (research.md R5), so no additional marker is fabricated. SC-003 ("the specific content block that was mid-stream … is visibly marked") is scoped to turns where a content block WAS mid-stream; a tool-gap turn has no such block and is therefore outside that criterion's subject.

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
- Unit: `mergePartialBlocks` — text+reasoning (last=text → text marked); reasoning-only (marked); multi-step `[text1, tool_call, tool_result, text2]` (last=text2 → merged text marked, accepted imprecision); **tool-gap `[text, tool_call, tool_result]` (last=tool_result → NO content block marked — the fully-streamed text stays unmarked)**; tool_call+result kept; tool_call without result dropped; empty → no-op.
- Unit (`handler.test.ts`): `ListMessages` returns the partial AIMessage with the interrupted marker on the flagged part; normal AIMessage unmarked.
- Large test: stall mid-reply → reconnect → `ListMessages` returns the partial output (spec SC-002/SC-003).
