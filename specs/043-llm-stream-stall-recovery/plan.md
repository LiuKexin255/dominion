# Implementation Plan: LLM Stream Stall Recovery

**Branch**: `043-llm-stream-stall-recovery` | **Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/043-llm-stream-stall-recovery/spec.md`

## Summary

Three changes to the agent's turn-execution layer that detect and recover from LLM SSE stream stalls (TCP connection alive but no data arriving):

1. **Chunk-idle timeout via LangGraph's built-in `TimeoutPolicy.idleTimeout`** (FR-001–FR-003): Configure `idleTimeout` (default 30s, configurable) on the team graph's `player` and `planner` nodes **individually** (`addNode` options per contract §1.1 — NOT `setNodeDefaults`, which would apply the timeout to `initInstruction`/`postCompactInstruction`/`compress` too; those nodes are intentionally out of scope). LangGraph's `idleTimeout` with `refreshOn: "auto"` resets on LangChain callback events (model streaming tokens, tool start/end). When the model stream stalls, no callback events fire, the `idleTimeout` elapses, and LangGraph raises `NodeTimeoutError` via cooperative AbortSignal cancellation.

2. **Tool-execution heartbeat** (FR-003, REQUIRED — see research.md R7): the idle timer is a wall-clock watchdog and only refreshes on callback events; during a long saolei MCP tool dispatch (up to 20 min) no events fire, so a bare `idleTimeout` would raise false `NodeTimeoutError`s mid-tool. Since feature 031 the production saolei tools are MCP client tools (`buildSaoleiMcpTools`) — the MCP HTTP boundary prevents `config.heartbeat` from reaching the MCP server's `bridge.dispatch` (research.md R7.1). Fix: a **client-side heartbeat wrapper** (`withIdleHeartbeat`) applied to each MCP tool in `buildSaoleiMcpTools` / `buildMemoryMcpTools`; during the tool's invoke it calls `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS` (default 10s < 30s idle), keeping the idle timer alive for the full MCP roundtrip + `bridge.dispatch` await. The wrapper reads heartbeat from the tool's invoke config (present because the ToolNode spreads `...config` from the node-attempt config). The `OperationBridge.dispatch` heartbeat parameter added in the original T008b is REMOVED (it was unreachable in production — the MCP server cannot access `config.heartbeat`).

3. **Error classification + player node propagation** (FR-004–FR-008): The player node's current `finally { return }` pattern (per [Feature 036](../036-team-mode-bugfix/spec.md) FR-002) swallows ALL exceptions. Modify it to re-throw `NodeTimeoutError` specifically (letting it propagate to `runTeamTurn` → `runLoop` → `finishError`, which retains the buffer), while continuing to swallow other errors (GraphRecursionError, model/tool errors) for Feature 036 FR-002 compatibility. The init instruction turn (FR-009/FR-010) gets a total timeout via `AbortSignal.timeout()`.

**Key research finding**: LangGraph 1.4.8 (`@langchain/langgraph/dist/pregel/utils/timeout.d.ts`) provides `TimeoutPolicy` with `idleTimeout` — the exact chunk-idle-watchdog mechanism, built into the framework. This eliminates the need for a custom `StreamWatchdog` class + composite AbortSignal (Constitution §II — Refactoring Over Patching: leverage existing architecture). The tool-execution heartbeat (research.md R7) is a client-side MCP tool wrapper (`withIdleHeartbeat`) — a small addition because the production saolei tools cross the MCP HTTP boundary, and `config.heartbeat` cannot reach `bridge.dispatch` on the server side. See [research.md](research.md) for the full langchain/langgraph analysis, MCP boundary analysis, and alternatives evaluation.

## Technical Context

**Language/Version**: TypeScript (agent service)

**Primary Dependencies**:
- `@langchain/langgraph` ^1.4.8 — `TimeoutPolicy` (`idleTimeout`, `runTimeout`, `refreshOn`), `NodeTimeoutError`, `isNodeTimeoutError`, `StateGraph.addNode` options, `graph.streamEvents`
- `@langchain/core` ^1.2.3 — `RunnableConfig` (signal propagation)
- `langchain` ^1.x — `createAgent` (player/planner internal loop)

**Storage**: N/A (transient per-turn state — no persistence change)

**Testing**: vitest (unit, via `vitest_test` Bazel macro); testplan/guitar (large tests)

**Target Platform**: Linux server (agent gRPC service)

**Project Type**: web-service (agent backend, gRPC bidi streaming)

**Performance Goals**: The `idleTimeout` adds zero per-token overhead — LangGraph's timer reset is a lightweight timestamp comparison on callback events. No measurable impact on normal turn execution.

**Constraints**: The timeout MUST NOT fire during legitimate tool execution (saolei MCP tool dispatch via `bridge.dispatch`, up to 20 min). `refreshOn: "auto"` stays as the (unchanged) baseline — token deltas refresh during streaming, tool start/end events refresh at boundaries; the mid-tool gap is covered by the **new** client-side heartbeat wrapper (`withIdleHeartbeat`, applied in `buildSaoleiMcpTools`) — heartbeat refreshes the timer unconditionally, so the timer cannot elapse mid-tool. The timeout MUST NOT change existing abort semantics (FR-012).

**Scale/Scope**: Per-session, single-user. One team graph per session with `player` + `planner` nodes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| **I. Citation & Provenance** | ✅ Pass | All references use repo-relative paths or full URLs. LangGraph source paths reference the installed version's dist files. |
| **II. Refactoring Over Patching** | ✅ Pass | Uses LangGraph's built-in `TimeoutPolicy.idleTimeout` instead of building a custom watchdog (leverages existing framework architecture). The player node modification is a minimal, targeted change to its catch behavior — not a restructure. |
| **III. Interface-First Design** | ✅ Pass | Contracts defined in `contracts/stall-recovery-contract.md`: (1) timeout configuration interface (env vars → `TimeoutPolicy`), (2) error classification contract (`NodeTimeoutError` propagates vs. other errors swallowed), (3) init turn timeout interface. |
| **IV. Test Granularity & Cadence** | ✅ Pass | Unit tests (vitest) for player node error classification + timeout configuration. Large tests (testplan) for end-to-end stall recovery. |
| **V. Read Before Code** | ✅ Pass | Per-phase document lists (three-category format) declared in `tasks.md`, including indirect references (Google TS/Go style guides, vitest docs, specs/019, LangGraph source). |
| **VI. Large Test Acceptance** | ✅ Pass | Agent is a service-type application. Large tests via testplan skill covering stall detection → warn + wait → buffer retention → next-turn drain. |

**Gate result**: All gates pass. No violations requiring justification.

## Project Structure

### Documentation (this feature)

```text
specs/043-llm-stream-stall-recovery/
├── plan.md                              # This file
├── research.md                          # Phase 0: langchain/langgraph timeout research
├── data-model.md                        # Phase 1: timeout config, error classification, state transitions
├── quickstart.md                        # Phase 1: validation guide (unit + large test)
├── contracts/
│   └── stall-recovery-contract.md       # Timeout configuration + error classification contract
└── tasks.md                             # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
projects/game/agent/src/
├── team/
│   ├── graph.ts                         # MODIFIED: addNode timeout config for player/planner
│   ├── player.ts                        # MODIFIED: catch NodeTimeoutError → re-throw (vs swallow)
│   └── planner.ts                       # MODIFIED: catch NodeTimeoutError → re-throw (if applicable)
├── session-team.ts                      # MODIFIED: runInitTurn adds AbortSignal.timeout
├── llm.ts                               # MODIFIED: timeout config constants + withIdleHeartbeat wrapper + buildSaoleiMcpTools/buildMemoryMcpTools apply wrapper
├── operation-bridge.ts                  # REVERTED: dispatch heartbeat param REMOVED (revert to dispatch(part, signal?))
├── tools/mouse_click/mouse-click.ts     # REVERTED: heartbeat pass-through removed (dead code; dispatch no longer takes heartbeat)
├── tools/mouse_move/mouse-move.ts       # REVERTED: heartbeat pass-through removed (dead code; dispatch no longer takes heartbeat)
├── operation-bridge.test.ts             # MODIFIED: heartbeat interval tests replaced with wrapper unit tests
├── llm.test.ts (or tool-heartbeat.test.ts) # NEW: withIdleHeartbeat wrapper unit tests
└── team/graph.test.ts                   # MODIFIED: timeout configuration tests
```

**Structure Decision**: No structural change — the feature modifies existing files in `agent/src/team/` and `agent/src/`. No new modules. The timeout leverages LangGraph's built-in `TimeoutPolicy` (no new utility files needed).

## Complexity Tracking

> No constitution violations. Table intentionally empty.
