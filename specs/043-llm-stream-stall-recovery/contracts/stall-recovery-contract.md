# Contract: Stream Stall Recovery

**Feature**: 043-llm-stream-stall-recovery | **Date**: 2026-08-11

**Spec**: [spec.md](../spec.md) | **Data Model**: [data-model.md](../data-model.md)

## §1. Node-Level Idle Timeout Configuration

### §1.1 Timeout Application

The team graph's `player` and `planner` nodes MUST be configured with LangGraph's `TimeoutPolicy`:

```typescript
graph.addNode("player", createPlayerNode(deps), {
    timeout: { idleTimeout: STREAM_IDLE_TIMEOUT_MS, refreshOn: "auto" },
});
```

- `STREAM_IDLE_TIMEOUT_MS`: read from `process.env.GAME_STREAM_IDLE_TIMEOUT_MS`, default `30000` (30s).
- `refreshOn: "auto"`: the idle timer refreshes on LangChain callback events (model streaming tokens, tool start/end), state writes, and child-task scheduling. This is the only supported mode. **Tool-execution coverage additionally requires the client-side MCP heartbeat wrapper (research.md R7, §1.2 below)** — tool start/end events alone only refresh at tool boundaries, not during a long tool wait.
- The timeout applies per node-attempt. If the player node has a RetryPolicy (it currently does not), the timer resets for each retry attempt.

### §1.2 What Refreshes the Idle Timer

With `refreshOn: "auto"`, the following signals refresh the `idleTimeout` (per LangGraph `TimeoutPolicy` docs, verified against installed 1.4.8 source):

| Signal | Source | During Model Streaming | During Tool Execution |
|--------|--------|----------------------|----------------------|
| LangChain callback events (onLLMNewToken, onToolStart, onToolEnd) | Model/tool invocations under the node's run | ✅ Token deltas refresh | ⚠️ Tool start/end refresh at **boundaries ONLY** — no events fire during a long tool await |
| State writes | Node return values / channel updates | — | ✅ (at tool completion) |
| Stream writer calls | `runtime.writer` / custom stream | — | — |
| Child-task scheduling | `Send` / subgraph dispatch | — | — |
| Explicit `config.heartbeat()` | **NEW: client-side MCP heartbeat wrapper (research.md R7.2)** | — | ✅ Refreshes unconditionally (not gated on `refreshOn`) — covers the mid-tool gap across the MCP HTTP boundary |

**Invariant**: During normal agent operation, the idle timer is continuously refreshed: model streaming via token deltas, tool execution via the client-side heartbeat wrapper (`config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS`, default 10s < 30s idle). A stall (model hangs, no tokens, no tool events, no heartbeat) is the ONLY scenario where the timer elapses.

**⚠️ Without the client-side heartbeat wrapper, the idle timer fires DURING any MCP tool execution longer than `idleTimeout`** (the watchdog is a wall-clock `Promise.race`; no LangChain events fire between `handleToolStart` and `handleToolEnd` — verified in installed `dist/pregel/timeout.js`). The wrapper is therefore a REQUIRED part of FR-003, not an optimization.

**Contract for the client-side heartbeat wrapper**:

The production saolei tools (`saolei_init`, `saolei_operate`, `saolei_remain`) are MCP client tools built by `buildSaoleiMcpTools` (`llm.ts`), served by the session-scoped MCP HTTP host (`mcp-host.ts`). The tool execution crosses an MCP HTTP boundary: the MCP server's `bridge.dispatch` runs in a different async context and has NO access to `config.heartbeat` (R7.1). Therefore the heartbeat MUST be driven from the client side.

- `withIdleHeartbeat(tool: StructuredToolInterface): StructuredToolInterface` — wraps a tool so that during its invoke, `config.heartbeat` (read from the tool's invoke config — present because the ToolNode spreads `...config` from the node-attempt config) is called immediately, then every `TOOL_HEARTBEAT_INTERVAL_MS` (default 10_000, MUST be < `STREAM_IDLE_TIMEOUT_MS`). The interval is cleared in a `finally` block on resolve/reject/abort. If `config.heartbeat` is absent (non-LangGraph invocation, tests), the wrapper degrades to a direct passthrough.
- Applied in `buildSaoleiMcpTools` (`llm.ts`) to each tool returned by `client.getTools()` — the single production choke point. `buildMemoryMcpTools` applies the same wrapper for the planner's memory MCP tools (defense-in-depth; the memory tool is expected to be fast but the wrapper overhead is negligible).
- `OperationBridge.dispatch(part, signal?)` — NO heartbeat parameter (the parameter added in the original T008b is removed: it was unreachable in production because the MCP server cannot access `config.heartbeat`). The `bridge.dispatch` await in `saolei_init`/`saolei_operate` (`saolei-mcp.ts`) stays as `dispatch(part, extra.signal)` — unchanged from pre-043.

### §1.3 What Happens When the Timer Fires

1. LangGraph creates a `NodeTimeoutError` with `{ node: "player", kind: "idle", idleTimeout: 30000, elapsed: <wall-clock> }`.
2. The node's `AbortSignal` is aborted → the in-flight model HTTP fetch is cancelled.
3. The `NodeTimeoutError` propagates out of the node function.
4. The error propagates through `graph.streamEvents` → `runTeamTurn`'s `for await` throws.

## §2. Player Node Error Classification

### §2.1 Current Behavior (Feature 036 — UNCHANGED for non-timeout errors)

The player node (`team/player.ts:218-232`) uses `try { invoke } finally { return }` which swallows ALL exceptions. This ensures `consumeGameEvent` runs on every path (per [Feature 036](../036-team-mode-bugfix/spec.md) FR-002). For non-timeout errors (GraphRecursionError, model errors, tool errors), this behavior is PRESERVED.

### §2.2 New Behavior for NodeTimeoutError

The player node MUST be restructured from `try/finally { return }` to `try/catch`:

```typescript
// PSEUDOCODE — actual implementation in tasks.md
let result;
try {
    result = await playerAgent.invoke({ messages: input }, config);
} catch (err) {
    // Consume game event on ALL error paths (FR-036 Issue 1 preserved)
    const gameEvent = consumeGameEvent(buffer);
    // NodeTimeoutError MUST propagate (Feature 043 stall recovery)
    if (isNodeTimeoutError(err)) throw err;
    // Other errors: swallow, return with game event (FR-036 FR-002 preserved)
    return {
        playerMessages: result?.messages ?? [],
        ...(gameEvent ? { gameEnded: gameEvent.status } : {}),
    };
}
// Success path
const gameEvent = consumeGameEvent(buffer);
return {
    playerMessages: result.messages,
    ...(gameEvent ? { gameEnded: gameEvent.status } : {}),
};
```

### §2.3 Invariants

- `consumeGameEvent(buffer)` MUST be called on ALL paths (success + error + timeout) — the game-end event is never lost (FR-036 FR-002).
- `NodeTimeoutError` MUST be re-thrown — it must NOT be swallowed.
- All other errors MUST be swallowed (return normally) — FR-036 compatibility.
- The `isNodeTimeoutError(e)` type guard from `@langchain/langgraph` is the canonical detection mechanism.

### §2.4 Planner Node

The planner node (`team/planner.ts`) also invokes a model and could stall. If the planner uses a `try/finally { return }` pattern similar to the player node, the same restructure applies. If the planner does not swallow errors, no change is needed (the `NodeTimeoutError` already propagates). This is determined during implementation.

## §3. TurnLoop Error Terminal Classification

### §3.1 Existing Logic (turn-loop.ts:352-358 — UNCHANGED)

```typescript
} catch (err: unknown) {
    if (this.controller.signal.aborted || this.aborting) {
        this.finishAbort();
    } else {
        this.finishError(err);
    }
    return;
}
```

### §3.2 Classification for NodeTimeoutError

When `NodeTimeoutError` propagates from `runTeamTurn` to `runLoop`'s catch:

- `this.controller.signal.aborted` is **`false`** — the TurnLoop's own AbortController was NOT aborted. The timeout fires on LangGraph's internal signal (managed by the `TimeoutPolicy`), not the TurnLoop's caller signal.
- `this.aborting` is **`false`** — no user abort was requested.
- Therefore: **`finishError(err)` is called** → retain buffer, emit `warn` + `wait`.

This is the correct behavior: the user's queued messages are retained (FR-006), and a visible `warn` is emitted (FR-005).

### §3.3 No TurnLoop Code Change Required

The existing catch logic correctly classifies `NodeTimeoutError` as a non-abort error. No modification to `turn-loop.ts` is needed. The `finishError` path already:
- Retains the buffer (FR-006).
- Emits `warn` (FR-005).
- Emits `wait` (returns to idle).
- Sets `running = false`.

## §4. Init Instruction Turn Timeout

### §4.1 Configuration

`runInitTurn` (`session-team.ts:439-478`) MUST add a total timeout to its `graph.invoke` config:

```typescript
await this.graphHandle.graph.invoke(
    {},
    {
        configurable: { ... },
        signal: AbortSignal.timeout(INIT_TURN_TIMEOUT_MS),
        // ...
    },
);
```

- `INIT_TURN_TIMEOUT_MS`: read from `process.env.GAME_INIT_TURN_TIMEOUT_MS`, default `120000` (120s).
- `AbortSignal.timeout()` is Node.js-native (available since Node 17, project uses Node 24).

### §4.2 Degrade Behavior (UNCHANGED)

The existing catch in `runInitTurn` (lines 469-477) already handles any error from the invoke:
- Logs a warning (`"init instruction turn failed; skipping initial instruction"`).
- Resolves the promise (degrade — skip instruction, don't block).

The `AbortSignal.timeout` expiry produces an `AbortError`/timeout error from the invoke, which is caught by the existing catch. No change to the degrade path.

### §4.3 Post-Refresh Instruction Turns

The post-refresh instruction turn (Feature 042) uses the same `runInitTurn` / `startInstructionTurn` code path. The timeout applies equally to both team-init and post-refresh instruction turns.

## §5. Scope Boundaries

- This contract does NOT modify `turn-loop.ts` — the existing `finishError` terminal is reused as-is.
- This contract does NOT modify the abort semantics (per [Feature 030](../030-queued-chat-input/spec.md) FR-011 — abort clears the buffer; this feature's FR-012 — abort semantics unchanged) — user abort and connection-drop abort continue to clear the buffer via `finishAbort`.
- This contract does NOT add retry logic (FR-013) — on stall recovery, the agent returns to idle with buffer retained; the next turn drains the buffer.
- This contract does NOT add a total turn-level timeout — the `idleTimeout` (per-model-call progress check) + `RECURSION_LIMIT=1000` (super-step bound) cover the observed failure mode.
