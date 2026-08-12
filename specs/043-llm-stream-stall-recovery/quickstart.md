# Quickstart: LLM Stream Stall Recovery

**Feature**: 043-llm-stream-stall-recovery | **Date**: 2026-08-11

**Spec**: [spec.md](spec.md) | **Contract**: [contracts/stall-recovery-contract.md](contracts/stall-recovery-contract.md)

## Prerequisites

- Bazel build system (`bazel build //...`, `bazel test //...`)
- The agent service's team graph with player/planner nodes (`projects/game/agent/src/team/graph.ts`)
- LangGraph 1.4.8+ (provides `TimeoutPolicy`, `NodeTimeoutError`, `isNodeTimeoutError`)
- Node.js 24+ (provides `AbortSignal.timeout()`, `AbortSignal.any()`)

## Unit Test Validation

### Scenario 1: NodeTimeoutError propagates from the player node

**What it validates**: FR-004/FR-008 — `NodeTimeoutError` is re-thrown by the player node (not swallowed by the `finally { return }` pattern).

**How to test**: In `team/player.test.ts` (or `graph.test.ts`), inject a fake `createAgentFn` whose `invoke` throws a `NodeTimeoutError`. Assert the player node function re-throws it (rather than returning a normal state update).

```typescript
// Pseudocode — actual test in tasks.md
const fakeNodeTimeoutError = Object.assign(new Error("idle timeout"), {
    name: "NodeTimeoutError",
});
const fakeCreateAgent = () => ({
    invoke: () => Promise.reject(fakeNodeTimeoutError),
});
// Assert: createPlayerNode({...}) throws NodeTimeoutError, not returns {}
```

### Scenario 2: Non-timeout errors are still swallowed (FR-036 compatibility)

**What it validates**: per [Feature 036](../036-team-mode-bugfix/spec.md) FR-002 — errors other than `NodeTimeoutError` are still swallowed by the player node, and the game event is consumed.

**How to test**: Inject a fake `createAgentFn` whose `invoke` throws a generic `Error`. Assert the player node returns normally (with `gameEnded` if applicable) instead of throwing.

### Scenario 3: Timeout configuration is applied to nodes

**What it validates**: FR-001/FR-011 — the team graph configures `idleTimeout` on the player and planner nodes.

**How to test**: In `graph.test.ts`, build the team graph and inspect the node specs' `timeout` configuration. Assert `player` and `planner` nodes have `timeout.idleTimeout` set to the configured value (default 30000 or the env-var override).

### Scenario 4: Init turn timeout fires

**What it validates**: FR-009/FR-010 — `runInitTurn` times out within the configured window.

**How to test**: In `session-team.test.ts`, set `GAME_INIT_TURN_TIMEOUT_MS` to a short value (e.g., 1000ms) and inject a graph whose `invoke` hangs (never resolves). Assert `runInitTurn` resolves (degrades) within ~1s + margin.

## Large Test Validation (testplan/guitar)

### Scenario 5: End-to-end stall recovery with buffer retention

**What it validates**: FR-001, FR-004–FR-007, SC-001, SC-002 — a real LLM stream stall is detected, the agent returns to idle with a visible notice, and queued messages are retained and delivered on the next turn.

**Prerequisites**: Deploy the agent service with the new timeout configuration. Use a fake-llm or a configurable LLM endpoint that can simulate a mid-stream stall (accept connection, send partial thinking, then stop sending data without closing the connection).

**Steps**:

1. Deploy the SUT via `guitar run <plan.yaml>`.
2. Connect to a saolei session, provision the team, start a turn.
3. Simulate the LLM stall: the fake-llm sends partial reasoning chunks, then stops (connection alive, no more data).
4. Verify: within ~30s (the configured `idleTimeout`), the desktop receives a `warn` frame (error notice) followed by a `wait` frame (idle).
5. Queue a user message during the stall (before the timeout fires). Verify the message enters the queue (QueueSignal with depth > 0).
6. After the timeout fires and the agent returns to idle, verify the queued message is automatically delivered as the next turn's input (the agent responds to it — the buffer was retained, not cleared).
7. Verify: the agent produces a normal response to the queued message on the next turn.

**Expected outcome**: The stall is detected in ~30s (not 1-4 minutes), the user sees a clear error notice, and the queued message is NOT lost.

### Scenario 6: Tool execution does not trigger false stall detection

**What it validates**: FR-003, SC-003 — saolei MCP tool execution (which can take up to 20 minutes via `bridge.dispatch` in the MCP server) does not trigger the idle timeout.

**Steps**:

1. Deploy the SUT.
2. Start a turn where the agent invokes a saolei MCP tool (e.g., `saolei_operate` dispatched to the desktop via the production MCP path: `buildSaoleiMcpTools` → MCP HTTP → `bridge.dispatch`).
3. Delay the desktop's tool response by > 30s (longer than the `idleTimeout`).
4. Verify: no `NodeTimeoutError` fires during the tool wait — the idle timer is kept alive by the **client-side heartbeat wrapper** (`withIdleHeartbeat` calling `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS`, research.md R7.2), not by dispatch-side events or MCP protocol features.
5. When the tool result arrives, verify the agent continues normally (model resumes streaming, turn completes).

## Configuration Override

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `GAME_STREAM_IDLE_TIMEOUT_MS` | `30000` | Chunk-idle timeout for player/planner model calls (FR-001; MUST be ≥ 15s per FR-001 — test overrides below are for graph_test only, which does not exercise a real stall) |
| `GAME_INIT_TURN_TIMEOUT_MS` | `120000` | Total timeout for the init instruction turn (FR-009) |

To test with shorter timeouts (faster test cycles — note FR-001's ≥15s minimum for the stream idle timeout, so the example uses 15000 for `GAME_STREAM_IDLE_TIMEOUT_MS`; `GAME_INIT_TURN_TIMEOUT_MS` has no minimum):
```bash
GAME_STREAM_IDLE_TIMEOUT_MS=15000 GAME_INIT_TURN_TIMEOUT_MS=10000 bazel test //projects/game/agent/src:graph_test
```

## Verification Checklist

- [ ] `NodeTimeoutError` propagates from the player node (unit test)
- [ ] Non-timeout errors are still swallowed by the player node (unit test, FR-036 compatibility)
- [ ] `idleTimeout` is configured on player and planner nodes ONLY — `setNodeDefaults` NOT used (unit test, contract §1.1)
- [ ] Client-side heartbeat wrapper keeps the idle timer alive during long MCP tool execution (unit test, research.md R7.2)
- [ ] Init turn timeout fires within the configured window (unit test)
- [ ] End-to-end stall detection → warn + wait → buffer retention (large test)
- [ ] Saolei MCP tool execution > idleTimeout does not trigger false stall — production MCP path (large test)
- [ ] Existing abort semantics unchanged (user abort still clears buffer — regression test, T013)
