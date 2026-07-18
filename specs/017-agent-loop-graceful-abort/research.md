# Research: Agent Loop Graceful Abort on Desktop Disconnect

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-13

This document consolidates the research that grounds the implementation
plan. It covers (A) the official LangChain JavaScript cancellation
contract, (B) the current throw-based termination mechanism in the agent
service, and (C) the design decisions that follow.

---

## A. LangChain JavaScript Cancellation Contract

### A.1 The cancellation option

The official LangChain JS cancellation primitive is the `signal` property
on [`RunnableConfig`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal),
typed as `AbortSignal | undefined`. It is inherited by every runnable
invocation path — `invoke()`, `stream()`, and `streamEvents()` — and
thus by the compiled graph returned by
[`createAgent`](https://docs.langchain.com/oss/javascript/langchain/agents#streaming)
(which returns a `ReactAgent` backed by a `CompiledStateGraph` extending
`Pregel`).

Exact type definition from
[`libs/langchain-core/src/runnables/types.ts` L101–107](https://github.com/langchain-ai/langchainjs/blob/cc39f564a9df18655ee4ae92de3f32e251021db8/libs/langchain-core/src/runnables/types.ts#L101-L107):

```typescript
export interface RunnableConfig<...> extends BaseCallbackConfig {
  signal?: AbortSignal;
}
```

`PregelOptions` (the options shape accepted by `agent.streamEvents()`)
[extends `RunnableConfig`](https://github.com/langchain-ai/langgraphjs/blob/aa6deb60c7bd0575f6d8629ed9023b944cedbc7b/libs/langgraph-core/src/pregel/types.ts#L80),
so `signal` is accepted directly in the second argument of the
`streamEvents` call the agent service already makes.

### A.2 When the signal must be supplied

**At call time** — i.e. in the options object passed to
`streamEvents(input, options)`. `createAgent({...})` does not accept a
`signal` parameter; each invocation gets a fresh `AbortController` so
each turn can be cancelled independently. This matches the per-turn
semantics the agent service needs.

### A.3 Documented behavior on abort

| Scenario | Behavior | Source |
|---|---|---|
| `invoke()` with aborted signal | `AbortError` propagates to the caller. | [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900) |
| `stream()` / `streamEvents()` with aborted signal | The async iterator **stops cleanly**; the stream closes via `controller.close()`. **No error is thrown to the consumer** of the `for await` loop. | [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900); [`IterableReadableStreamWithAbortSignal`](https://github.com/langchain-ai/langgraphjs/blob/main/libs/langgraph-core/src/pregel/stream.ts) |
| LLM HTTP request in flight | The `AbortSignal` propagates to the provider HTTP client (OpenAI / Anthropic). The actual HTTP request is cancelled in-process. | [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900) |
| Checkpointer state | LangGraph writes a checkpoint **after each completed superstep**. An aborted in-progress superstep is abandoned; all previously-completed supersteps retain their checkpoints intact. Resuming with the same `thread_id` continues from the last consistent superstep boundary. | [LangGraph fault-tolerance docs](https://docs.langchain.com/oss/javascript/langgraph/fault-tolerance#graceful-shutdown) |

The `streamEvents` path combining the caller-provided signal with
LangGraph's internal abort controller is implemented in
[`pregel/index.ts` via `combineAbortSignals()`](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946):
either signal aborting is sufficient.

### A.4 Orchestrator-level fine-tuning (optional)

`@langchain/core` (since ~0.3.x, via
[PR #7917](https://github.com/langchain-ai/langchainjs/pull/7917)) exposes
an `orchestratorAbortBehavior` option with three modes:

| Mode | Behavior |
|---|---|
| `"throw_immediately"` (default) | Abandon active async tasks and throw immediately on abort. |
| `"complete_pending"` | Do not abandon active tasks; only block starting new ones. |
| `"passthrough"` | Do not check signal at orchestration level; delegate entirely to tasks. |

**Decision**: leave the default (`"throw_immediately"`). We want the
turn to stop as promptly as possible on disconnect.

### A.5 Version pins (catalog)

The agent service's
[`package.json`](../../../projects/game/agent/package.json) resolves
LangChain packages from the root
[`pnpm-workspace.yaml`](../../../pnpm-workspace.yaml) catalog:

| Package | Catalog version | Cancellation feature |
|---|---|---|
| `@langchain/core` | `^1.2.0` | `RunnableConfig.signal` — stable since 0.1.x. `orchestratorAbortBehavior` since ~0.3.x. |
| `@langchain/langgraph` | `^1.4.4` | `Pregel.stream()` / `streamEvents()` signal combining — stable. `RunControl.requestDrain()` since 1.4.0 (not used here). |
| `langchain` | `^1.5.0` | `createAgent` API (v1 stable). |

All three are well above the versions where `signal` on `streamEvents`
stabilized. No version bump is required for this feature.

---

## B. Current Throw-Based Termination Mechanism

### B.1 Two independent throw sites

1. **Proactive — `ToolAbortOnDisconnect` middleware**
   ([`projects/game/agent/src/llm.ts:177-187`](../../../projects/game/agent/src/llm.ts)):
   wraps every tool call via `wrapToolCall`. Before each tool invocation,
   checks `bridge.hasSink()`. If `false` (desktop disconnected), throws
   `DesktopDisconnectedError`. Wired into `createAgent` at
   [`llm.ts:198`](../../../projects/game/agent/src/llm.ts).

2. **Reactive — `OperationBridge.dispatch()` throw**
   ([`projects/game/agent/src/operation-bridge.ts:148-161`](../../../projects/game/agent/src/operation-bridge.ts)):
   when a tool calls `bridge.dispatch(part)`, the method reads the sink
   locally and throws `DesktopDisconnectedError` at line 158 if `!sink`.
   Catches the race between the middleware check and the actual dispatch.

Both throws propagate up through `generateTurn` into the `for await` loop
in
[`handler.ts:287-317`](../../../projects/game/agent/src/handler.ts), where
the generic `catch (err)` block at
[`handler.ts:332-351`](../../../projects/game/agent/src/handler.ts) converts
them into `warn` + `wait` frames addressed to the desktop.

### B.2 Stream lifecycle cleanup

On stream `error` or `end`
([`handler.ts:357-365`](../../../projects/game/agent/src/handler.ts) and
[`handler.ts:367-370`](../../../projects/game/agent/src/handler.ts)), the
`cleanupSinks()` function
([`handler.ts:170-179`](../../../projects/game/agent/src/handler.ts))
iterates the stream-scoped `activeSessions` set
([`handler.ts:169`](../../../projects/game/agent/src/handler.ts)) and calls
`sa.getBridge().unregisterSink()` for each session. This clears the sink
but does **not** stop any in-flight LLM stream — it only arms the
middleware/dispatch throw for the *next* tool boundary.

### B.3 Symbols that become dead code if the throw path is removed

| Symbol | File:Line | Reason |
|---|---|---|
| `DesktopDisconnectedError` class | `operation-bridge.ts:70-75` | No throwers remain. |
| `DesktopDisconnectedError` import | `llm.ts:18` | Sole runtime import; class is dead. |
| `ToolAbortOnDisconnect` middleware | `llm.ts:177-187` | Entire `createMiddleware` body. |
| `hasSink()` method | `operation-bridge.ts:131-133` | Only caller is the middleware at `llm.ts:180`. |
| `throw new DesktopDisconnectedError` in dispatch | `operation-bridge.ts:158-160` | The throw inside `dispatch()` when `!sink`. |
| `DesktopDisconnectedError` import in test | `operation-bridge.test.ts:19` | Test import for a dead class. |

### B.4 Tests covering the disconnect path

| File:Line | Test | Covers |
|---|---|---|
| `operation-bridge.test.ts:80-86` | `"no sink registered → dispatch → throws DesktopDisconnectedError"` | Directly tests the dispatch throw. |
| `operation-bridge.test.ts:115-121` | `"hasSink() reflects sink registration state"` | Tests `hasSink()` lifecycle. |
| `operation-bridge.test.ts:91-109` | `"unregister mid-dispatch → timeout → FAILED"` | Timeout path when sink removed mid-dispatch. |
| `handler.test.ts:590-607` | `"registers bridge sink on user content frame"` | Sink registration on user content. |
| `handler.test.ts:656-675` | `"unregisters bridge sink on stream end for active sessions"` | `cleanupSinks` on stream end. |
| `handler.test.ts:677-696` | `"unregisters bridge sink on stream error"` | `cleanupSinks` on stream error. |
| `handler.test.ts:698-719` | `"sink callback writes content envelope to stream"` | Bridge → stream integration. |
| `handler.test.ts:782-818` | `"emits warn frame on LLM error and keeps stream open"` | Generic catch block (not disconnect-specific). |

**Test gaps** (no coverage today):
- `llm.test.ts` has no test for `ToolAbortOnDisconnect` middleware.
- `mouse-tool.test.ts` has no test for `dispatch()` throwing.

### B.5 Callers of `DesktopDisconnectedError` outside `projects/game/agent/`

**None.** The class is entirely internal to the agent package. No runtime
code outside `projects/game/agent/src/` references it. Safe to remove.

---

## C. Design Decisions

### C.1 Cancellation primitive: `AbortSignal` on `streamEvents`

**Decision**: use `signal: AbortSignal` in the `streamEvents` options
object, as documented in
[LangChain JS — `RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal).

**Rationale**: this is the official LangChain JS cancellation contract.
It propagates to the LLM HTTP client (cancelling the actual request),
stops the `streamEvents` async iterator cleanly (no error thrown to the
consumer), and leaves completed-superstep checkpoints intact for
reconnect-resume. It replaces both throw sites with a single, uniform
mechanism that engages at any turn phase — not just at tool boundaries.

**Alternatives considered**:

- **Keep the throw-based middleware but also pass a signal**: rejected.
  Two termination mechanisms for the same event is the kind of parallel
  code path §IV forbids — the middleware would become a redundant
  belt-and-suspenders that obscures the real cancellation path.
- **Use `RunControl.requestDrain()`** (LangGraph ≥ 1.4.0): rejected for
  this use case. `requestDrain()` is cooperative shutdown between
  supersteps — it does not cancel in-flight LLM HTTP requests. The
  [fault-tolerance docs](https://docs.langchain.com/oss/javascript/langgraph/fault-tolerance#graceful-shutdown)
  explicitly say: "requestDrain() does not cancel ongoing async work.
  For hard limits, combine drain with an AbortSignal." Disconnect is a
  hard limit, not a graceful drain.
- **Cancel the HTTP request directly** (bypass LangChain): rejected.
  Non-idiomatic, fragile against provider client internals, and wouldn't
  stop the graph orchestration layer.

### C.2 AbortController lifecycle: per-turn, stream-scoped

**Decision**: create one `AbortController` per turn, tracked in a
stream-scoped `Map<sessionId, AbortController>`. On stream `end` or
`error`, iterate the map and call `controller.abort()` for every
in-flight turn, then run the existing `cleanupSinks()`.

**Rationale**: the per-session mutex
([`handler.ts:264`](../../../projects/game/agent/src/handler.ts)) already
guarantees at most one turn per session; a per-turn controller matches
that granularity. A stream can carry multiple sessions, so the map
handles the multi-session case. Stream-scoped tracking ensures all
in-flight turns abort the instant the bidi stream tears down, regardless
of which phase (LLM thinking, text streaming, tool dispatch) each turn
is in.

### C.3 Signal propagation: through the adapter interface

**Decision**: extend `AgentAdapter.generateTurn(threadId, content)` to
`generateTurn(threadId, content, signal?: AbortSignal)`. The signal
flows through `AgentAdapterImpl.streamFromAgent` into the
`agent.streamEvents(input, { ..., signal })` options object.

**Rationale**: the adapter interface is the boundary between the gRPC
handler (which owns the stream lifecycle) and the LangChain runtime
(which owns the cancellation contract). Passing the signal through the
interface keeps the cancellation primitive visible at the seam, rather
than hiding it inside the adapter. The signal is optional so existing
callers (including tests) are unaffected unless they opt in.

**Existing-design verdict (§IV)**: the `AgentAdapter` interface
([`llm.ts:108-136`](../../../projects/game/agent/src/llm.ts)) was
designed as a per-turn streaming boundary with `threadId` and `content`
as the only variable inputs. Adding `signal` is a natural extension of
that design — it is another per-turn input that controls the turn's
lifecycle. The interface's shape and intent remain coherent.

### C.4 Remove the throw path entirely

**Decision**: delete `ToolAbortOnDisconnect`, `DesktopDisconnectedError`,
and `hasSink()`; change `OperationBridge.dispatch()` to return a FAILED
`OperationResult` when no sink is registered, instead of throwing.

**Rationale**: with the signal aborting the entire `streamEvents` run,
no new tool calls will start after disconnect, and any in-flight LLM
stream is cancelled. The throw-based path becomes redundant. Leaving it
would mean two termination mechanisms for the same event — exactly the
"parallel code paths" §IV prohibits. Removing it also eliminates the
false-positive `warn` + `wait` frames the current catch block emits for
a disconnected peer (spec FR-004).

**Existing-design verdict (§IV)**: the `OperationBridge` was designed as
a sink-dispatch correlation layer
([`operation-bridge.ts:1-18`](../../../projects/game/agent/src/operation-bridge.ts)).
The `DesktopDisconnectedError` throw and `hasSink()` were bolted on as
a disconnect-detection side channel. With cancellation moved to the
signal, the bridge's design returns to its original purpose: correlate
tool requests with results via `tool_id`. The throw and `hasSink()` are
outdated design elements that this change removes, bringing the bridge
back into coherence.

### C.5 In-flight tool dispatch on abort

**Decision**: `OperationBridge.dispatch()` gains an optional
`signal?: AbortSignal` parameter. When the signal aborts, the pending
dispatch resolves with a FAILED result (message `"aborted"`) and cleans
up its timer and map entry. The existing 5 s timeout remains as a
fallback.

**Rationale**: when the stream disconnects, the signal aborts and
LangGraph stops orchestrating new work, but a tool already awaiting a
desktop result via `dispatch()` holds a pending Promise. Without signal
awareness, that Promise lingers until the 5 s timeout. Wiring the
signal into `dispatch()` makes cleanup immediate and deterministic.

**Alternatives considered**:

- **Rely on the timeout alone**: rejected. 5 s of wasted work per
  in-flight dispatch is not "prompt termination" (spec FR-001,
  SC-001).
- **Reject the dispatch Promise on abort**: rejected. A rejection would
  surface as a tool error inside LangGraph, which the orchestrator
  might try to handle (feeding a ToolMessage back to the LLM). A FAILED
  resolution is consistent with the existing timeout/invalid-part
  paths and lets the signal-driven orchestration shutdown proceed
  without extra error handling.

---

## References

### Official Documentation

- [LangChain JS — `RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal) — the official cancellation option reference.
- [LangChain JS — Agents (streaming)](https://docs.langchain.com/oss/javascript/langchain/agents#streaming) — `createAgent` + `streamEvents` usage.
- [LangGraph JS — Fault Tolerance](https://docs.langchain.com/oss/javascript/langgraph/fault-tolerance#graceful-shutdown) — `RunControl.requestDrain()` vs `AbortSignal` semantics.
- [LangChain v1 release notes](https://docs.langchain.com/oss/javascript/releases/langchain-v1) — `createAgent` as the v1 standard API.

### Repositories

- [`RunnableConfig` type — langchainjs `runnables/types.ts` L101-107](https://github.com/langchain-ai/langchainjs/blob/cc39f564a9df18655ee4ae92de3f32e251021db8/libs/langchain-core/src/runnables/types.ts#L101-L107) — `signal?: AbortSignal` definition.
- [`PregelOptions` — langgraphjs `pregel/types.ts` L80](https://github.com/langchain-ai/langgraphjs/blob/aa6deb60c7bd0575f6d8629ed9023b944cedbc7b/libs/langgraph-core/src/pregel/types.ts#L80) — extends `RunnableConfig`, inheriting `signal`.
- [`Pregel.streamEvents()` — langgraphjs `pregel/index.ts` L1922-1946](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946) — `combineAbortSignals(options?.signal, ...)`.
- [PR #7917 — `orchestratorAbortBehavior`](https://github.com/langchain-ai/langchainjs/pull/7917) — three abort modes; default is `"throw_immediately"`.
- [PR #9900 — provider call/stream abort signal handling](https://github.com/langchain-ai/langchainjs/pull/9900) — stream returns early, invoke throws.

### Articles & RFCs

- No external articles or RFCs cited.
