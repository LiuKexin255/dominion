# Research: Queued Chat Input During Agent Run (LangGraph-Native)

**Feature**: `specs/030-queued-chat-input` | **Date**: 2026-07-29

**Directive**: Implement message queuing using **LangChain native capabilities** (no bespoke frontend queue, no custom mutex-patch queue). This research resolves the mechanism, the rejected alternatives, and the integration decisions.

---

## D1 — Chosen mechanism: a checkpointer-backed agent loop that buffers and drains pending `HumanMessage`s

**Decision**: The queue is a per-session **agent loop** that (a) buffers incoming user content while a turn is in flight, (b) runs the turn via the existing `AgentAdapter.generateTurn` (= LangGraph `streamEvents(v3)`), and (c) on turn completion, drains the buffer — combining all pending messages into one aggregated turn (FR-005) — and re-invokes `generateTurn` on the **same `thread_id`**, continuing until the buffer is empty, then emits `wait`.

**Rationale / evidence**:
- LangGraph's documented multi-turn model is *exactly* repeated `streamEvents`/`invoke` on the same `thread_id` with a `MemorySaver` checkpointer — "each call picks up where the last one left off" ([LangGraph — Add memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory); [Use functional API — multi-turn chatbot](https://docs.langchain.com/oss/javascript/langgraph/use-functional-api) shows `stream1` then `stream2` on the same config continuing the conversation). The dominion agent already relies on this across turns: history is reconstructed from the checkpointer (`projects/game/agent/src/llm.ts`, `session-agent.ts` `MemorySaver`).
- The documented streaming loop pattern is `while { stream = await graph.streamEvents(..., {version:"v3"}); for await (...); if (!stream.interrupted) { await stream.output; break; } ... }` ([LangGraph — interrupts: Stream with HITL](https://docs.langchain.com/oss/javascript/langgraph/interrupts)). Our loop follows this shape, minus the interrupt branch (see D2).
- The current single-turn driver (`projects/game/agent/src/handler.ts:413-510` → `adapter.generateTurn` → emit blocks → `await stream.output` at `projects/game/agent/src/llm.ts:526`) already terminates a turn on stream completion. The loop wraps this and adds the drain step.

**What makes it "LangChain native"**: conversation continuity, ordering, and history are provided by the LangGraph checkpointer + `streamEvents`; the "queue" is just a transient FIFO of `HumanMessage`s awaiting the next `streamEvents` invocation. No custom persistence, no custom event bus.

**Alternatives considered**:
- *Frontend queue that dispatches on `wait`* — rejected by the user directive (not LangChain-native) and weaker under reconnect/race (the frontend cannot atomically combine or guarantee server-side order).
- *Backend array + dispatcher bolted onto the mutex* — a custom queue; rejected for the same reason (not native).

---

## D2 — Why NOT `interrupt()` / `Command({ resume })`

**Decision**: Do **not** use LangGraph `interrupt()` for queueing.

**Rationale**: `interrupt()` *pauses* the graph to **await required** human input at a specific node, then resumes via `Command({ resume })` ([LangGraph — interrupts](https://docs.langchain.com/oss/javascript/langgraph/interrupts)). Our requirement is the opposite: input is **optional** and must **not** pause the agent (spec FR-002 — queued input does not alter the in-flight turn). Pausing would break continuous tool/reasoning runs. The buffer+drain loop (D1) satisfies "optional, non-interrupting" cleanly.

**Also confirmed (D1 support)**: an in-flight `streamEvents` invocation does **not** absorb externally-buffered messages — LangGraph processes one super-step from the checkpoint it loaded; an external `updateState` would fork, not retroactively alter the running step ([LangGraph — time-travel / forking](https://docs.langchain.com/oss/javascript/langgraph/use-time-travel)). This is precisely the isolation FR-002 requires: buffering messages outside the running graph guarantees they never disturb it.

---

## D3 — Combining multiple queued messages into one aggregated turn (FR-005)

**Decision**: When the buffer has ≥2 messages at a turn boundary, merge them into a **single `HumanMessage`** with **multiple content blocks**, in FIFO order: each message's text becomes a `{type:"text"}` block (joined so they remain ordered and distinct), and each screenshot becomes an `{type:"image_url"}` block immediately followed by its pixel-size annotation block (the existing convention at `projects/game/agent/src/llm.ts:464-484`).

**Rationale**:
- A single `HumanMessage` with multiple content blocks is **model-safe**: it avoids back-to-back human messages (which some providers handle inconsistently) and constitutes exactly one LLM-facing turn, matching "combined into the next single agent turn's input" (spec FR-005/Clarifications).
- This requires generalizing `TurnContent` / `streamFromAgent` to build content blocks from **N text parts + M image parts** rather than the current single-text/single-image shape (`projects/game/agent/src/llm.ts:455-484`). The existing size-annotation convention is preserved per image.

**Alternatives considered**:
- *Pass an array of `HumanMessage`s to one `streamEvents` call* — the messages reducer would append them, producing consecutive human messages; provider behavior is inconsistent. Rejected.
- *One separate turn per queued message* — explicitly rejected by the user in `/speckit.clarify` (chose "combined into one turn").

**Order guarantee**: the buffer is FIFO; merge preserves submission order (spec FR-004).

---

## D4 — Queue-state feedback to the frontend (FR-008/FR-009)

**Decision**: Add a minimal, **additive** `QueueSignal` `FlowPart` to `projects/game/game.proto` that the agent **pushes** whenever the per-session queue depth changes (on buffer +1, on drain to 0). The frontend renders the pending queue from this depth and from the messages it already sent optimistically; items transition from "pending" to "normal" as depth decreases and the combined turn streams.

```proto
message QueueSignal {
  int32 queued_count = 1;
}
// added to FlowPart.kind oneof:  QueueSignal queue = 9;
```

**Rationale**:
- The frontend cannot otherwise detect that an **auto-continued** turn has begun (the turn boundary between queued turns emits no `wait` — see D5), so it cannot satisfy FR-009 ("consumed messages transition out of pending") reliably without a backend signal.
- Pushing on depth change is event-driven and consistent with how the agent already pushes flow frames mid-turn (`wait`/`warn`/status, `projects/game/agent/src/handler.ts:501-509`); it does not alter the spec-021 pull-based `StatusSignal` semantics.
- `wait` is emitted **only** when the loop fully drains (queue empty → idle), so the desktop's `processing` indicator stays `true` across queued-turn boundaries and clears exactly once at the end.

**Alternatives considered**:
- *Frontend-optimistic-only (no proto change)* — rejected: cannot satisfy FR-009 (no reliable consume signal); also drifts under reconnect.
- *Extend `StatusSignal` with a count and make it push-on-change* — rejected: `StatusSignal` is pull-based by design (spec 021); overloading it risks regressing status reconciliation.

**Open item for `tasks.md`**: confirm whether `queued_count` or a richer "consumed message ids" payload is preferred; the plan recommends the minimal count (sufficient for FR-008/FR-009 given the frontend already tracks the messages it sent).

---

## D5 — Mutex → loop reconciliation; `wait` and status semantics

**Decision**: Replace the per-frame `acquireMutex → generateTurn → releaseMutex` serialization (`projects/game/agent/src/handler.ts:60-99,390-542`) with the per-session **TurnLoop** (D1), which is itself the single-flight mechanism (`running` flag + buffer). Consequences:

- **`wait` semantics**: `wait` is emitted by the loop **only when the buffer is empty at a turn boundary** (idle). Between auto-continued queued turns the loop emits the `QueueSignal` (depth → 0) and continues **without** `wait`, so the desktop's `processing` indicator remains active until the queue is fully drained.
- **Status reconciliation (spec 021)**: `status-signal.ts` `deriveStatusSignal(isInFlight, isBound)` is a **pure function** (`projects/game/agent/src/status-signal.ts:38-49`) and is **unchanged**. It is fed `isInFlight = turnLoop.isRunning()` instead of `isMutexHeld`. `STATUS_ACTIVE` thus correctly covers "a turn is in flight **or** the loop is draining queued work"; `STATUS_IDLE` when the loop is idle. The handler's status-probe call site (`projects/game/agent/src/handler.ts:258-261`) is updated to read loop state.
- **Profile-name guard** (`projects/game/agent/src/handler.ts:320-359`) runs **before** content reaches the loop, unchanged.
- **Adapter bind** (`session-agent.ts` `getOrCreateAdapter`/`serializeBind`) is unchanged; the loop obtains the adapter per turn as today.

**Rationale (Constitution §II)**: the existing mutex exists *only* to serialize turns; with queueing the serialization unit is the loop, so keeping the mutex would be a redundant second lock. Removing it and making the loop the single-flight owner is a simplification, not added complexity. The FIFO ordering guarantee (the old mutex's purpose) is preserved by the FIFO buffer.

**Migration note**: the existing handler FIFO test (`projects/game/agent/src/handler.test.ts` "serializes concurrent user content frames on same session (FIFO)") is recast to assert loop behavior: concurrent submits are combined/ordered, never concurrent.

---

## D6 — Abort interaction (spec FR-011)

**Decision**: An explicit abort (stop control / stream `end`/`error`, feature 017) aborts the in-flight turn **and clears the queue**. The loop's per-session `AbortController` (the existing `activeTurns` map, `projects/game/agent/src/handler.ts:204-215`) is reused; on abort the loop breaks, the buffer is cleared, and `wait` is emitted to return the desktop to ready.

**Rationale**: abort signals "halt" (spec FR-011, edge case "User aborts/stops…"); auto-continuing into queued work after an abort would contradict the user's intent. This matches the user's `/speckit.clarify` resolution that only **normal** completion triggers queue hand-off.

---

## D7 — Delivery failure handling (spec FR-015)

**Decision**: If a turn's `generateTurn` stream errors at the hand-off moment, the queued messages are **retained** in the buffer, a visible error is surfaced (existing `warn` frame path, `projects/game/agent/src/handler.ts:511-538`), and the loop does **not** drop the buffer. The exact retry strategy (automatic with bounded backoff vs. a manual retry affordance) is a **tasks.md/implementation** detail; the plan only fixes "retain + surface error" (FR-015).

**Rationale**: dropping authored messages on a transient failure loses work and breaks the "queue is reliable" expectation (`/speckit.clarify` Q2). Retaining + surfacing lets recovery occur without retyping.

---

## D8 — Frontend input enable + queue UI (spec FR-001/FR-008)

**Decision**:
- Remove `disabled={processing}` from the chat input and Send control (`projects/game/desktop/frontend/src/components/ChatView.svelte:358,364`); the input is always editable/submittable.
- On submit **while `processing`**: the frontend sends the frame immediately (the backend buffers it — D1), shows the user message in a **pending** visual state, and increments the queue indicator.
- The `queueCount`/`.queue-indicator` affordance already exists (`projects/game/desktop/frontend/src/components/ChatView.svelte:335-339`, `projects/game/desktop/frontend/src/App.svelte:78`); it is wired to the `QueueSignal` depth (D4) rather than the current vestigial in-flight counter (`App.svelte:629,634,651`).
- `SendUserTurn` in the Go desktop backend (`projects/game/desktop/app.go:701`) stays **non-blocking** and unchanged (feature 015 contract preserved); queuing is purely an agent-service concern.

**Rationale**: keeps the frontend dumb (render + send) and the queue logic native/LangGraph-side, matching the directive.

---

## External references (read during planning; to be re-listed in `tasks.md` per Constitution §V)

- [LangGraph — Add memory (MemorySaver, multi-turn)](https://docs.langchain.com/oss/javascript/langgraph/add-memory)
- [LangGraph — Use functional API (multi-turn chatbot, repeated streamEvents)](https://docs.langchain.com/oss/javascript/langgraph/use-functional-api)
- [LangGraph — Interrupts (Stream with HITL loop; why interrupt is wrong primitive — D2)](https://docs.langchain.com/oss/javascript/langgraph/interrupts)
- [LangGraph — Time travel / forking (in-flight isolation — D2 support)](https://docs.langchain.com/oss/javascript/langgraph/use-time-travel)
- [opencode — input-while-running queue behavior (behavioral reference)](https://github.com/anomalyco/opencode)

## In-repository sources (verified during planning)

- `projects/game/agent/src/llm.ts` — `createAgent` compile (`:325`), `generateTurn`/`streamFromAgent` (`:430-527`), single-text/single-image content blocks (`:455-484`), `await stream.output` turn completion (`:526`).
- `projects/game/agent/src/handler.ts` — user-content path (`:282-543`), per-frame mutex (`:60-99`), `acquireMutex`/`releaseMutex` (`:390,541`), `wait` emission (`:501-509`), `activeTurns`/`abortAllTurns` (`:204-215`), status probe (`:258-261`), warn-on-error (`:511-538`).
- `projects/game/agent/src/session-agent.ts` — `SessionAgent`/`SessionAgentStore`, adapter bind, `MemorySaver` ownership.
- `projects/game/agent/src/status-signal.ts` — pure `deriveStatusSignal` (unchanged seam).
- `projects/game/desktop/frontend/src/components/ChatView.svelte:335-368` — `disabled={processing}`, `queueCount` indicator.
- `projects/game/desktop/frontend/src/App.svelte:78,627-655` — `processing`/`queueCount` state and send path.
- `projects/game/desktop/app.go:701` — non-blocking `SendUserTurn`.
- `projects/game/game.proto:90,316-329,340-342,448-450` — `Connect`, `FlowPart`, `FlowParts`, `WaitSignal`.

All `NEEDS CLARIFICATION` items are resolved: the queue mechanism (D1), the combination semantics (D3, from `/speckit.clarify`), and the LangChain-native directive (user input) are settled. No outstanding blockers for Phase 1.
