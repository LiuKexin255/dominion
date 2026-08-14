# Research: LLM Stream Stall Recovery

**Feature**: 043-llm-stream-stall-recovery | **Date**: 2026-08-11

## R1. LangGraph Built-in TimeoutPolicy (PRIMARY FINDING)

### Decision: Use LangGraph's `TimeoutPolicy.idleTimeout` on team graph nodes

**LangGraph 1.4.8** (`@langchain/langgraph` ^1.4.8, installed) provides a built-in per-node timeout policy that is the exact chunk-idle-watchdog mechanism needed:

**Source**: `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/pregel/utils/timeout.d.ts`

```typescript
type TimeoutPolicy = {
  /** Hard wall-clock cap (ms), never refreshed. */
  runTimeout?: number;
  /** Max idle time (ms) without observable progress.
   *  Refreshed by: writes, stream writer calls, child-task scheduling,
   *  LangChain callback events (model tokens, tool start/end), heartbeat. */
  idleTimeout?: number;
  /** "auto" (default): refresh on standard graph progress + heartbeat.
   *  "heartbeat": refresh only on explicit runtime.heartbeat(). */
  refreshOn?: "auto" | "heartbeat";
};
```

**Key behaviors** (from `errors.js` + `timeout.d.ts` docs):
- Raises **`NodeTimeoutError`** with fields `{ node, elapsed, kind: "idle"|"run", runTimeout, idleTimeout }` and message like `Node "player" exceeded its idle timeout of 30000ms without making progress`.
- Uses **cooperative cancellation**: aborts the node's `AbortSignal` and discards its buffered writes.
- `isNodeTimeoutError(e)` type guard available for classification.
- Configurable per-node via `addNode(name, fn, { timeout: { idleTimeout: 30000 } })` or globally via `StateGraph` constructor `defaults: { timeout: ... }`.
- Also overridable per-task via `Send(name, input, { timeout: { idleTimeout: N } })`.

**Why `idleTimeout`（`refreshOn: "auto"` 为既有配置，非本 feature 改动）** **+ periodic heartbeat during tool execution** **solves the tool-execution problem (FR-003)**: The `idleTimeout` refreshes on "LangChain callback events emitted under the node's run." During model streaming, token delta events (`onLLMNewToken`) refresh it continuously. Tool **start/end** events (`onToolStart`/`onToolEnd`) refresh it at the tool's **boundaries** — but **NOT during** the tool's execution: while OperationBridge `dispatch()` awaits the desktop result (up to 20 min), no LangChain callback events fire (verified against the installed `dist/pregel/timeout.js` — the `IdleProgressCallbackHandler` only observes callback events; the watchdog is a wall-clock `Promise.race` on `lastProgress + idleMs`).

**Therefore a heartbeat is REQUIRED during long tool execution** (this corrects the earlier "no manual pause/resume logic needed" claim — that was only true at tool boundaries): a periodic `config.heartbeat?.()` call while a tool is pending keeps the idle timer alive. LangGraph's `wrapConfig` installs `wrapped.heartbeat = () => scope.touch()` which refreshes `lastProgress` **unconditionally** (unlike `autoTouch`, it is not gated on `refreshOn`), so a heartbeat every N seconds (N < idleTimeout, e.g. 10s < 30s default) keeps the idle timer alive for the full tool wait. Model-streaming phases need no heartbeat (`refreshOn: "auto"` covers token events). A stall (model hangs mid-stream, no tokens, no tool events, no heartbeat) still causes the timer to elapse.

**Implementation note**: since feature 031 the production saolei tools are MCP client tools (`buildSaoleiMcpTools`), not the raw mouse tools. The MCP HTTP boundary prevents `config.heartbeat` from reaching the MCP server's `bridge.dispatch` (R7.1), so the heartbeat is driven by a **client-side wrapper** (`withIdleHeartbeat`) applied to each MCP tool in `buildSaoleiMcpTools`. `config.heartbeat` is available on the MCP client tool's invoke config because `wrapConfig` installs it on the node-attempt config and the ToolNode spreads `...config` into the tool's invoke config (verified in installed `dist/agents/nodes/ToolNode.js`). See R7 for the full MCP boundary analysis and wrapper design.

### Rationale

| Criterion | LangGraph `TimeoutPolicy` | Custom `StreamWatchdog` class |
|-----------|--------------------------|------------------------------|
| Tool-execution handling | ✅ Automatic at tool **boundaries** (`onToolStart`/`onToolEnd` refresh); **requires a periodic `config.heartbeat()` during long tool waits** (MCP tools via the client-side wrapper — research.md R7.2) — heartbeat refreshes unconditionally (verified in `dist/pregel/timeout.js` `wrapConfig`) | ❌ Requires manual `pause()`/`resume()` on tool-started/finished events |
| Signal composition | ✅ Framework-managed (node's AbortSignal) | ❌ Requires custom `AbortSignal.any()` composition |
| Maintenance | ✅ Framework-maintained, version-tracked | ❌ Custom code to maintain + test |
| Error type | ✅ `NodeTimeoutError` (structured, type-guarded) | ❌ Custom `StreamStallError` class |
| Test surface | ✅ LangGraph tests the timer; we test config + classification | ❌ Must test timer, signal composition, pause/resume |
| Constitution §II | ✅ Leverages existing architecture | ❌ Builds parallel infrastructure |

### Alternatives Considered

1. **Custom `StreamWatchdog` + composite `AbortSignal.any()`** (original plan): Wrap `runTeamTurn`'s `for await` loop with a custom timer that resets on `messages` events and pauses on `tools` events. **Rejected**: LangGraph already provides this mechanism natively; building a parallel watchdog violates Constitution §II (simpler to use the framework's built-in).

2. **LangChain `ChatOpenAI.timeout`**: Set `timeout` on the ChatModel via `initChatModel`. **Rejected**: `@langchain/openai` 1.5.5 passes `timeout` to the openai npm client, which only applies it to the **initial HTTP response** (headers), not to SSE mid-stream stalls (confirmed in source: `chat_models/base.js:280` — `timeout: this.timeout` goes to the client constructor). This is the same gap identified by [opencode issue #13841](https://github.com/anomalyco/opencode/issues/13841).

3. **Vercel AI SDK `timeout.chunkMs`** (opencode's approach): Requires switching from langchain's `initChatModel` to Vercel AI SDK's `streamText`. **Rejected**: dominion uses LangGraph `createAgent` (not `streamText`); switching would be a major architecture change.

## R2. Player Node Error Handling — `NodeTimeoutError` Propagation

### Current behavior (Feature 036, `player.ts:218-232`)

The player node uses `try { ... } finally { return ... }` to ensure `consumeGameEvent` runs on ALL paths (success + error), and the `finally { return }` pattern **swallows all exceptions** (see [Feature 036 — Team Mode Bugfix](../036-team-mode-bugfix/spec.md) FR-002):

```typescript
try {
    result = (await playerAgent.invoke({ messages: input }, config));
} finally {
    const gameEvent = consumeGameEvent(buffer);
    return { playerMessages: result?.messages ?? [], ...(gameEvent ? { gameEnded: gameEvent.status } : {}) };
}
```

This means a `NodeTimeoutError` would be swallowed — the graph would continue (route to planner or back to player) with empty messages, defeating the stall recovery.

### Decision: Restructure to `try/catch` with selective re-throw

Convert the `try/finally { return }` to `try/catch`:
- On **`NodeTimeoutError`**: consume the game event (for FR-036 consistency), then **re-throw** — let it propagate to `runTeamTurn` → `runLoop` → `finishError` (retain buffer, emit `warn` + `wait`).
- On **other errors** (GraphRecursionError, model errors, tool errors): consume the game event, **swallow** (return normally) — FR-036 FR-002 compatibility.

### Rationale

The `finally { return }` pattern is a JS anti-pattern for selective error handling — `return` in `finally` suppresses ALL throws. The `try/catch` restructure is the minimal change that preserves FR-036 behavior while allowing `NodeTimeoutError` to propagate. The game event is consumed on both paths (the `catch` block also calls `consumeGameEvent` before deciding to re-throw or swallow).

## R3. Cross-Framework Comparison

| Framework | Mechanism | Equivalent in LangGraph |
|-----------|-----------|------------------------|
| **opencode** (PR #25575) | `streamText({ timeout: { chunkMs: chunkTimeout } })` — Vercel AI SDK built-in | `TimeoutPolicy.idleTimeout` — same concept, framework-native |
| **OpenClaw** (`proxy.ts`) | `readIdleTimeoutMs` + `buildProxyRequestAbort(callerSignal, idleTimeoutMs)` — manual composite signal | `TimeoutPolicy.idleTimeout` — framework manages signal composition |
| **Hermes** (`run_agent.py`) | `_interruptible_streaming_api_call()` — background thread + abort event | `TimeoutPolicy` + cooperative AbortSignal — same cooperative cancellation principle |
| **dominion (this feature)** | LangGraph `TimeoutPolicy.idleTimeout` on player/planner nodes | Framework-native, no custom code |

All frameworks converge on the same pattern: **per-chunk idle timeout that resets on streaming activity, cooperative abort when it fires.** LangGraph provides this natively.

## R4. Incident Re-Analysis — Desktop "typing → idle" Transition

### Observation (user report)

> "在发送消息前，明确观察到 desktop 的状态从 'agent is typing' 转换到 idle，并且发送消息时并没有 queue 的字样，并且过了一会又 desktop 又恢复到 idle"

### Root Cause

The "typing → idle" transition is NOT the turn completing normally. It is an **indirect cascade** caused by the LLM stream stall:

```
1. LLM stream stalls (TCP alive, no SSE data)
   → runTeamTurn's for-await blocked
   → running=true → desktop shows "typing"

2. No TeamFrames flow from agent → desktop
   → bidi WebSocket appears idle

3. ~1-4 min: WebSocket idle timeout fires → bidi stream drops

4. handler.ts stream.on("end") → abortLoops() → team.abort()
   → controller.abort() → LLM fetch interrupted
   → runLoop catch: controller.signal.aborted === true → finishAbort()
   → buffer cleared (per [Feature 030](../030-queued-chat-input/spec.md) FR-011), QueueSignal(0) + wait emitted (to dead stream, swallowed)

5. desktop detects WebSocket drop → reconnects
   → status probe → agent returns IDLE (isRunning=false after abort)
   → desktop shows idle

6. user sends message → no queue (IDLE) → new turn → LLM stalls again → cycle repeats
```

### Why the chunk-idle watchdog fixes this

With `idleTimeout: 30000` on the player node:
1. LLM stream stalls → no callback events for 30s
2. LangGraph raises `NodeTimeoutError` after 30s (NOT 1-4 min)
3. `NodeTimeoutError` propagates to `runLoop` → `finishError` (retain buffer, emit `warn` + `wait`)
4. The `warn` + `wait` are emitted through the **still-alive** bidi stream → desktop receives them immediately
5. Desktop shows the error + returns to idle within ~30s (not 1-4 min), and the user's queued messages are retained

## R5. Init Instruction Turn Timeout

### Decision: Use `AbortSignal.timeout(ms)` as the init turn's signal

The init instruction turn (`runInitTurn`, `session-team.ts:439-478`) uses `graph.invoke` (not streaming). LangGraph's `idleTimeout` on the `initInstruction` node would work, but `runInitTurn` already passes a config with `configurable` but no `signal`. The simplest approach: add `signal: AbortSignal.timeout(INIT_TURN_TIMEOUT_MS)` to the invoke config.

When the timeout fires:
- `AbortSignal.timeout` aborts → LangGraph propagates → the invoke throws
- `runInitTurn`'s existing catch (lines 469-477) catches it → logs warning → resolves (degrade)
- The init promise resolves → user turns awaiting it are unblocked (FR-010)

**Rationale**: `AbortSignal.timeout()` is Node.js-native (no dependencies), simple, and reuses the existing degrade path. Configuring `TimeoutPolicy` on the `initInstruction` node would also work but adds graph-construction complexity for a fire-and-forget turn.

## R6. Configuration Design

### Decision: Environment variables with constants fallback

```typescript
// llm.ts (new constants)
export const STREAM_IDLE_TIMEOUT_MS = Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) || 30_000;
export const INIT_TURN_TIMEOUT_MS = Number(process.env.GAME_INIT_TURN_TIMEOUT_MS) || 120_000;
```

Applied in:
- `team/graph.ts`: `graph.addNode("player", playerFn, { timeout: { idleTimeout: STREAM_IDLE_TIMEOUT_MS, refreshOn: "auto" } })` (same for the `planner` node)
- `session-team.ts`: `runInitTurn` uses `signal: AbortSignal.timeout(INIT_TURN_TIMEOUT_MS)`

**Rationale**: Environment variables are the simplest configuration mechanism for a server-side service (consistent with existing env-var patterns in the codebase). The defaults (30s idle, 120s init) match community consensus (opencode 15-30s, OpenClaw `readIdleTimeoutMs`).

## R7. Tool-Execution Heartbeat — Client-Side MCP Wrapper (REQUIRED)

### Problem: idle timer fires DURING long tool execution

Verified against the installed LangGraph 1.4.8 source (`dist/pregel/timeout.js`):

- The idle watchdog is a **wall-clock timer** (`Promise.race([nodeOutcome, watchdog])`; `checkIdle` re-arms at `lastProgress + idleMs`). It fires while the node function is still running — it is NOT suspended during tool execution.
- `lastProgress` refreshes only on: LangChain callback events (`IdleProgressCallbackHandler`), `Send` (child-task scheduling), stream writer calls, and explicit `heartbeat()`.
- A saolei tool dispatch (`OperationBridge.dispatch()` in the MCP server) awaits the desktop's `FlowResultPart` (up to `DISPATCH_TIMEOUT_MS` = 20 min). Between `handleToolStart` and `handleToolEnd` **no LangChain callback events fire** (the tool body is a single pending promise — no token/chain/custom events).

**Consequence**: with `idleTimeout: 30000` and no heartbeat, a tool wait >30s raises `NodeTimeoutError` → false stall → violates FR-003 / SC-003 / US3.

### R7.1 Architecture constraint: the MCP HTTP boundary (post-031)

**Since feature 031 (team template mode)**, the production player tools are the saolei **MCP** tools (`saolei_init`, `saolei_operate`, `saolei_remain`), built by `buildSaoleiMcpTools` (`llm.ts:300-313`) and served by the session-scoped MCP HTTP host (`mcp-host.ts`). The former raw mouse tools (`mouse_click`/`mouse_move`, `llm.ts:226-248`) are **dead code** in production — `server.ts:233-238` wires `buildSaoleiMcpTools`, NOT `buildTools`; only `llm.test.ts` still references the mouse tools.

The tool execution path crosses an **MCP HTTP boundary**:

```
LangGraph run (gRPC handler)                    MCP host (Express, same process)
┌────────────────────────────────┐              ┌─────────────────────────────────┐
│ player node attempt             │              │ POST /internal/mcp/:t/:s/saolei │
│  config.heartbeat = scope.touch │              │  MCP server tool handler        │
│  └─ createAgent → ToolNode      │              │   └─ bridge.dispatch(           │
│     └─ MCP client tool .invoke( │── HTTP ───> │         part, extra.signal)     │
│         args, { ...config,      │              │      ↑ extra = { signal } only  │
│           heartbeat: fn  ✓ }    │              │      NO config.heartbeat!       │
│       )                         │<── HTTP ─── │                                 │
└────────────────────────────────┘              └─────────────────────────────────┘
```

- `config.heartbeat` is installed by LangGraph's `wrapConfig` (`timeout.js:100-102`) on the **node-attempt config**. The ToolNode spreads `...config` into each tool's invoke config (`langchain/dist/agents/nodes/ToolNode.js:229-241`), so the **MCP client tool's invoke config HAS heartbeat**.
- BUT the mcp-adapters `_callTool` (`@langchain/mcp-adapters/dist/tools.js:351-420`) reads only `config.signal` and forwards it via the HTTP request. It does NOT read or forward `config.heartbeat`.
- The MCP server's tool handler (`saolei-mcp.ts:767`) receives only `extra: { signal: AbortSignal }` — the MCP protocol provides an AbortSignal for cancellation, but **no heartbeat / progress-from-client channel**.
- Therefore the MCP server's `bridge.dispatch(part, extra.signal)` (`saolei-mcp.ts:885,983`) has **no access to `config.heartbeat`** — it lives in a different async execution context (the LangGraph run) on the other side of the HTTP boundary.

**This is why the original T008b heartbeat design (passing heartbeat through `dispatch(part, signal, heartbeat)`) is ineffective in production**: T008b wired heartbeat into `mouse_click`/`mouse_move` (dead code) and the `OperationBridge.dispatch` third parameter; production `saolei_operate`/`saolei_init` never call dispatch with heartbeat.

### R7.2 Solution: client-side heartbeat wrapper on MCP tools

Since `config.heartbeat` IS available on the **MCP client side** (the LangGraph tool invocation context), the heartbeat must be driven **from there** — not passed through the MCP boundary to `bridge.dispatch`.

**Design**: wrap each MCP client tool (returned by `buildSaoleiMcpTools` / `buildMemoryMcpTools`) with a `withIdleHeartbeat(tool)` wrapper:

1. On each tool invoke, read `config.heartbeat` from the invoke config (present because the ToolNode spreads `...config`).
2. Call `heartbeat()` immediately (the first setInterval tick is `TOOL_HEARTBEAT_INTERVAL_MS` away), then start `setInterval(heartbeat, TOOL_HEARTBEAT_INTERVAL_MS)` (default 10_000, MUST be < `STREAM_IDLE_TIMEOUT_MS`).
3. Invoke the underlying MCP tool (which makes the HTTP request → MCP server → `bridge.dispatch` → desktop await).
4. In a `finally` block (resolve/reject/abort all reach it), clear the interval — no leaked timers.
5. If `config.heartbeat` is absent (non-LangGraph invocation, unit tests), degrade to a direct passthrough — no interval, no overhead.

LangGraph's `wrapConfig` installs `wrapped.heartbeat = () => scope.touch()` (`timeout.js:100-102`). `touch()` refreshes `lastProgress` **unconditionally** (not gated on `refreshOn` — verified in `dist/pregel/timeout.js:27-30`). The periodic heartbeat keeps the idle timer alive for the full duration of the MCP HTTP roundtrip + `bridge.dispatch` await (up to 20 min).

**Application point**: `buildSaoleiMcpTools` (`llm.ts:300-313`) wraps each tool from `client.getTools()` with `withIdleHeartbeat`. This is the single production choke point where ALL saolei MCP tools pass through. `buildMemoryMcpTools` applies the same wrapper for the planner's memory MCP tools (optional — the memory tool is expected to be fast, but the wrapper's overhead is negligible and provides defense-in-depth).

### R7.3 Disposition of the OperationBridge.dispatch heartbeat parameter

The `dispatch(part, signal?, heartbeat?)` third parameter and its interval logic are **removed** (revert to `dispatch(part, signal?)`):

- In the MCP path (production): heartbeat is driven client-side by the wrapper; `dispatch` never receives heartbeat.
- In the mouse tool path (dead code): the heartbeat pass-through added in T008b is reverted.
- Keeping a parallel heartbeat mechanism in `dispatch` would be Constitution §II violation (stacking code for a path that's unreachable in production).

The `TOOL_HEARTBEAT_INTERVAL_MS` constant stays in `llm.ts` — it's now consumed by the wrapper instead of `dispatch`.

### R7.4 Alternatives rejected

- **`refreshOn: "heartbeat"`** — would require instrumenting model streaming too (tokens would no longer auto-refresh), defeating the "zero per-token overhead" goal.
- **Custom `StreamWatchdog` pause/resume** — rejected in R1 (parallel infrastructure, violates Constitution §II).
- **Shared per-session heartbeat registry** (LangGraph run registers heartbeat → MCP server dispatch looks it up by sessionId) — introduces cross-context shared mutable state, much more complex than the client-side wrapper, with no benefit.
- **MCP progress notifications (server→client)** — the direction is wrong; we need client→server heartbeat (or rather, client-side self-refresh), not server-to-client progress.
- **`createAgent` `wrapToolCall` middleware** — viable alternative to the per-tool wrapper (would cover all tools automatically), but requires verifying that `request.runtime` carries `heartbeat`; the per-tool wrapper is simpler and guaranteed to work (the tool invoke config demonstrably has heartbeat — `mouse-click.ts:82` already reads it).

### R7.5 Test implications

US3's behavioral verification (tool execution > idleTimeout without false stall) requires the heartbeat wrapper. Unit test: a fake MCP tool whose invoke hangs > `STREAM_IDLE_TIMEOUT_MS`, wrapped with `withIdleHeartbeat` and invoked with a heartbeat-providing config → heartbeat fires at `TOOL_HEARTBEAT_INTERVAL_MS` cadence, interval cleared on resolve. Without heartbeat in config → no interval (passthrough). The config-only test (T008) is necessary but NOT sufficient. The large test (T011) verifies end-to-end on the production MCP path.
