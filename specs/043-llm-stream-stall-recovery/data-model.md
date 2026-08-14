# Data Model: LLM Stream Stall Recovery

**Feature**: 043-llm-stream-stall-recovery | **Date**: 2026-08-11

## 1. Timeout Configuration

### Entity: `StreamIdleTimeoutConfig`

The chunk-idle timeout applied to team graph nodes that perform LLM model calls.

| Field | Type | Default | Source |
|-------|------|---------|--------|
| `idleTimeout` | `number` (ms) | `30000` | `process.env.GAME_STREAM_IDLE_TIMEOUT_MS` |
| `refreshOn` | `"auto"` | `"auto"` | Fixed — refreshes on model tokens + tool events (tool-execution coverage completed by the client-side MCP heartbeat wrapper, research.md R7.2) |

**Applied to nodes**: `player`, `planner` ONLY (per contract §1.1 — `initInstruction`/`postCompactInstruction` are covered by the init-turn total timeout FR-009; `compress` is out of scope; `setNodeDefaults` is NOT used because it would extend the timeout to all nodes).

**Configuration mechanism**: `graph.addNode(name, fn, { timeout: { idleTimeout: STREAM_IDLE_TIMEOUT_MS, refreshOn: "auto" } })` in `team/graph.ts`.

**Constraints**:
- MUST be ≥ 15000 (15s) per FR-001.
- `refreshOn: "auto"` is the only supported mode. **Tool-execution coverage requires the client-side MCP heartbeat wrapper** (`config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS` during MCP tool invocation — research.md R7.2): tool start/end events refresh only at boundaries; a 20-min saolei tool wait (`saolei_operate` batch via `bridge.dispatch`) with no events would otherwise trip the 30s idle timer. The wrapper is applied in `buildSaoleiMcpTools` (`llm.ts`) because the production saolei tools cross the MCP HTTP boundary — `config.heartbeat` is available on the MCP client tool's invoke config (ToolNode spreads `...config`) but cannot reach the MCP server's `bridge.dispatch` (R7.1).

### Entity: `InitTurnTimeoutConfig`

The total execution timeout for the async init instruction turn.

| Field | Type | Default | Source |
|-------|------|---------|--------|
| `timeoutMs` | `number` (ms) | `120000` | `process.env.GAME_INIT_TURN_TIMEOUT_MS` |

**Applied to**: `runInitTurn` (`session-team.ts:439-478`) via `AbortSignal.timeout(timeoutMs)`.

**Constraints**: MUST be long enough for a normal init instruction (model call + instruction tool + write-back), but short enough to not block the first user turn excessively (default 120s).

## 2. Error Classification

### `NodeTimeoutError` (from LangGraph)

LangGraph raises this when the `idleTimeout` or `runTimeout` elapses. Fields:

| Field | Type | Description |
|-------|------|-------------|
| `name` | `"NodeTimeoutError"` | Type discriminator |
| `node` | `string` | The node that timed out (e.g., `"player"`) |
| `elapsed` | `number` (ms) | Wall-clock time elapsed |
| `kind` | `"idle" \| "run"` | Which timeout fired |
| `runTimeout` | `number \| undefined` | The configured runTimeout (if `kind="run"`) |
| `idleTimeout` | `number \| undefined` | The configured idleTimeout (if `kind="idle"`) |

**Detection**: `isNodeTimeoutError(e)` from `@langchain/langgraph`.

### Classification Rules

| Error Type | Player Node Behavior | TurnLoop Terminal | Buffer | Emitted Frames |
|------------|---------------------|-------------------|--------|----------------|
| `NodeTimeoutError` | **Re-throw** (propagate) | `finishError` | **Retained** | `warn` + `wait` |
| Other errors (model, tool, GraphRecursionError) | **Swallow** (return normally, per [Feature 036](../036-team-mode-bugfix/spec.md) FR-002) | N/A (node returns) | N/A | N/A (graph continues) |
| User abort (caller signal) | N/A (signal aborts) | `finishAbort` | **Cleared** (per [Feature 030](../030-queued-chat-input/spec.md) FR-011) | `QueueSignal(0)` + `wait` |

**Key invariant**: `NodeTimeoutError` MUST be distinguishable from a user abort at the TurnLoop level. The TurnLoop's catch checks `controller.signal.aborted || aborting`:
- A `NodeTimeoutError` arrives with `controller.signal.aborted === false` (the timeout fires on a separate signal managed by LangGraph, not the TurnLoop's controller) → `finishError` (retain buffer).
- A user abort arrives with `controller.signal.aborted === true` → `finishAbort` (clear buffer).

## 3. State Transitions

### TurnLoop state machine (unchanged, with new trigger)

```
IDLE → submit → RUNNING (start runLoop)
RUNNING → model streams normally → ... → turn completes
    → buffer empty → finishIdle → IDLE (emit wait)
    → buffer non-empty → drain → next turn (same thread_id)
RUNNING → NodeTimeoutError (stall detected)
    → runLoop catch: controller.signal.aborted === false
    → finishError → IDLE (emit warn + wait, RETAIN buffer)
RUNNING → user abort (controller.abort)
    → runLoop catch: controller.signal.aborted === true
    → finishAbort → IDLE (emit QueueSignal(0) + wait, CLEAR buffer)
RUNNING → non-timeout error (model/tool error not caught by player node)
    → runLoop catch: controller.signal.aborted === false
    → finishError → IDLE (emit warn + wait, RETAIN buffer)
```

**New trigger**: `NodeTimeoutError` is a new trigger for the existing `finishError` terminal. The terminal's behavior (retain buffer, emit `warn` + `wait`) is unchanged from [Feature 030 FR-015](../030-queued-chat-input/spec.md).

### Init turn state machine (new timeout)

```
triggerInitInstruction → startInstructionTurn → runInitTurn
    → graph.invoke succeeds → initTurn resolves (instruction produced)
    → graph.invoke throws (planner LLM error) → catch → warn → resolve (degrade)
    → AbortSignal.timeout fires (NEW) → graph.invoke throws → catch → warn → resolve (degrade)
```

The timeout is a new trigger for the existing degrade path. The degrade behavior (skip instruction, resolve promise) is unchanged from [Feature 039 contract §6](../039-planner-memory-calibration/spec.md).

## 4. Data Flow: Stall Recovery Sequence

```
┌──────────────────────────────────────────────────────────────────┐
│ 1. User sends message → submit → RUNNING                         │
│ 2. graph.streamEvents → player node starts                       │
│ 3. createAgent.invoke → model streams tokens (callback events    │
│    refresh idleTimeout)                                          │
│ 4. LLM service stalls → no more callback events                  │
│ 5. 30s idleTimeout elapses → NodeTimeoutError raised             │
│ 6. NodeTimeoutError propagates through createAgent → player node │
│ 7. Player node catch: isNodeTimeoutError → re-throw              │
│ 8. graph.streamEvents → runTeamTurn for-await throws             │
│ 9. runLoop catch: signal.aborted === false → finishError         │
│10. finishError: RETAIN buffer, emit warn + wait                  │
│11. Desktop receives warn (error notice) + wait (idle)            │
│12. If buffer non-empty → next submit drains buffer (FR-007)      │
└──────────────────────────────────────────────────────────────────┘
```

## 5. Data Flow: Tool Execution With Heartbeat (FR-003 — No False Stall)

```
┌──────────────────────────────────────────────────────────────────┐
│ 1. User sends message → player node starts                       │
│ 2. Model streams tokens → emits a saolei_operate tool_call       │
│ 3. ToolNode invokes the wrapped MCP client tool:                 │
│    a. withIdleHeartbeat reads config.heartbeat                   │
│    b. Calls heartbeat() immediately, starts setInterval          │
│    c. Invokes the underlying MCP client tool                     │
│ 4. MCP client tool → HTTP POST → MCP server (mcp-host)           │
│ 5. MCP server handler → bridge.dispatch(part, extra.signal)      │
│    → awaits desktop result (up to 20 min)                        │
│ 6. MEANWHILE (client side): setInterval fires heartbeat() every  │
│    TOOL_HEARTBEAT_INTERVAL_MS → scope.touch() refreshes timer    │
│ 7. Desktop responds → bridge.dispatch resolves → HTTP response   │
│ 8. MCP client tool resolves → wrapper clears interval (finally)  │
│ 9. ToolNode returns ToolMessage → model resumes streaming        │
│ NO NodeTimeoutError fires during steps 5-8 (idle timer kept     │
│ alive by the wrapper's heartbeat, NOT by dispatch or MCP events) │
└──────────────────────────────────────────────────────────────────┘
```
