# Contract: Agent Adapter Abort Signal

**Feature**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

This contract defines the interface changes to `AgentAdapter` and
`OperationBridge` that propagate an `AbortSignal` from the gRPC handler
to the LangChain runtime and the tool-dispatch layer.

**Authoritative source for the cancellation primitive**:
[`RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal)
— `AbortSignal | undefined` on `RunnableConfig`, inherited by
`PregelOptions` (see
[langgraphjs `pregel/types.ts` L80](https://github.com/langchain-ai/langgraphjs/blob/aa6deb60c7bd0575f6d8629ed9023b944cedbc7b/libs/langgraph-core/src/pregel/types.ts#L80)).

---

## `AgentAdapter.generateTurn` — signal parameter

### Current signature

```typescript
generateTurn(
  threadId: string,
  content: TurnContent,
): AsyncIterable<ContentBlock>;
```

### New signature

```typescript
generateTurn(
  threadId: string,
  content: TurnContent,
  signal?: AbortSignal,
): AsyncIterable<ContentBlock>;
```

### Contract

| Aspect | Specification |
|---|---|
| `signal` parameter | Optional. When omitted, behavior is identical to today (no cancellation). |
| Signal propagation | The implementation MUST pass `signal` into the `agent.streamEvents(input, options)` options object as `options.signal`. This is the official LangChain cancellation entry point. |
| Abort behavior | When `signal` is aborted, the `AsyncIterable` MUST stop yielding within a bounded window (LLM HTTP cancelled, stream iterator exits). The iterable MUST NOT throw `AbortError` to the consumer — `streamEvents` exits cleanly per [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900). |
| Normal completion | When the signal is not aborted, the iterable MUST yield all blocks and complete identically to today. |
| Checkpoint | The caller does not need to handle checkpoint cleanup — LangGraph's per-superstep checkpointing guarantees that completed supersteps survive an abort. |

### Callers

| Caller | Current | After change |
|---|---|---|
| `handler.ts` `Connect` (L287) | `adapter.generateTurn(sessionId, turnContent)` | `adapter.generateTurn(sessionId, turnContent, controller.signal)` |
| Test fakes in `llm.test.ts`, `session-agent.test.ts` | Omit `signal` | Omit `signal` (optional param, backward-compatible) |

---

## `OperationBridge.dispatch` — signal parameter + no-throw

### Current signature

```typescript
async dispatch(part: Part): Promise<OperationResult>
```

Throws `DesktopDisconnectedError` when `this.sink === null`.

### New signature

```typescript
async dispatch(part: Part, signal?: AbortSignal): Promise<OperationResult>
```

### Contract

| Aspect | Specification |
|---|---|
| `signal` parameter | Optional. When omitted, behavior matches today minus the throw. |
| No sink registered (`this.sink === null`) | MUST NOT throw. MUST return `{ status: "TOOL_RESULT_STATUS_FAILED", message: "desktop disconnected" }`. |
| Signal aborted before dispatch starts | MUST return `{ status: "TOOL_RESULT_STATUS_FAILED", message: "aborted" }` without writing to the sink. |
| Signal aborted during pending dispatch | MUST resolve the pending Promise with `{ status: "TOOL_RESULT_STATUS_FAILED", message: "aborted" }`, clear the timer, and delete the pending map entry. |
| Normal tool result arrives first | MUST resolve with the result (unchanged). |
| Timeout fires first (5s) | MUST resolve with `{ status: "TOOL_RESULT_STATUS_FAILED", message: "operation timed out" }` (unchanged). |
| Resolution semantics | The Promise MUST always **resolve** (never reject) for the abort and no-sink cases, consistent with the existing timeout and invalid-part paths. This prevents the abort from surfacing as an unhandled tool error inside LangGraph. |
| Race safety | The timer, the signal-abort listener, and the `handleResult` callback are mutually exclusive: whichever fires first wins, the others are cleaned up. |

### Removed symbols

| Symbol | Reason |
|---|---|
| `DesktopDisconnectedError` class | No throw sites remain after this contract takes effect. |
| `hasSink()` method | Sole caller (`ToolAbortOnDisconnect` middleware) is deleted. |

### Callers

| Caller | Current | After change |
|---|---|---|
| `mouse-tool.ts` `createMouseMoveTool` (L86) | `bridge.dispatch(part)` | `bridge.dispatch(part, signal)` — signal obtained from the LangChain tool context. |
| `mouse-tool.ts` `createMouseClickTool` (L122) | `bridge.dispatch(part)` | `bridge.dispatch(part, signal)` — same. |
| Test fakes | `bridge.dispatch(part)` | `bridge.dispatch(part)` or `bridge.dispatch(part, signal)` — optional param. |

---

## `handler.ts` `Connect` — per-turn AbortController lifecycle

### Contract

| Aspect | Specification |
|---|---|
| Controller creation | A new `AbortController` MUST be created for each turn, after `acquireMutex(sessionId)` succeeds and before `adapter.generateTurn(...)` is called. |
| Tracking | The controller MUST be stored in a stream-scoped `Map<string, AbortController>` (keyed by `sessionId`) for the duration of the turn. |
| Abort trigger | `stream.on("end")` and `stream.on("error")` MUST call `controller.abort()` for every entry in the map, then clear the map. This runs alongside the existing `cleanupSinks()`. |
| Turn cleanup | The turn's `finally` block MUST delete the session's entry from the map (in addition to the existing `releaseMutex(sessionId)`). |
| Normal path | When the desktop stays connected, the controller is never aborted; it is discarded after normal turn completion. |
| No-sink dispatch | If `dispatch()` returns FAILED with `"desktop disconnected"` or `"aborted"`, the handler MUST NOT treat it as a fatal error — the turn continues to unwind via the signal path. |

---

## References

- [`RunnableConfig.signal` — LangChain JS reference](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal)
- [`Pregel.streamEvents()` source — `combineAbortSignals`](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946)
- [PR #9900 — stream returns early on abort, invoke throws](https://github.com/langchain-ai/langchainjs/pull/9900)
