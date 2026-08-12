# Tasks: LLM Stream Stall Recovery

**Input**: Design documents from `/specs/043-llm-stream-stall-recovery/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/stall-recovery-contract.md](contracts/stall-recovery-contract.md), [quickstart.md](quickstart.md)

**Cross-feature specs referenced by tasks** (read the relevant one when a task cites its FR): [Feature 030 — Queued Chat Input](../030-queued-chat-input/spec.md) (buffer drain/clear semantics, FR-011/FR-015), [Feature 036 — Team Mode Bugfix](../036-team-mode-bugfix/spec.md) (player node error swallowing, FR-002), [Feature 038 — Queued Input Mid-Turn](../038-queue-input-mid-turn/spec.md) (mid-turn drain), [Feature 039 — Planner Memory Calibration](../039-planner-memory-calibration/spec.md) (init instruction turn, US3/FR-015).

**Organization**: Tasks grouped by user story. Each story is independently testable. US1–US3 are co-dependent (same timeout mechanism) and delivered together as the MVP; US4 is independent.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Add timeout configuration constants consumed by all subsequent phases.

### 文档清单

- **代码规范文档**: `style/javascript.md`（引用 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) 作为 TS 规范基准，需同时阅读）；本 phase 无涉及测试，javascript.md 的测试章节非必读
- **官方文档**: 无
- **技术文章**: 无

### Tasks

- [X] T001 Add `STREAM_IDLE_TIMEOUT_MS` and `INIT_TURN_TIMEOUT_MS` constants (env-var-backed via `GAME_STREAM_IDLE_TIMEOUT_MS` / `GAME_INIT_TURN_TIMEOUT_MS`, defaults 30000 / 120000) and `TOOL_HEARTBEAT_INTERVAL_MS` (default 10000, MUST be < `STREAM_IDLE_TIMEOUT_MS` per research.md R7) to `projects/game/agent/src/llm.ts`, exported for reuse by `graph.ts`, `session-team.ts`, and `operation-bridge.ts`. Read the existing constant patterns (e.g., `RECURSION_LIMIT` at line 32) for convention. Note: FR-001 requires the idle period to be at least 15s — the default (30000) satisfies this; document the constraint in a comment.

**Checkpoint**: Constants available for Phase 2+.

---

## Phase 2: Foundational (Node-Level Idle Timeout)

**Purpose**: Configure LangGraph's built-in `TimeoutPolicy.idleTimeout` on the team graph's `player` and `planner` nodes **individually** (per contract §1.1 — per-node `addNode` options, NOT `setNodeDefaults`). This is the core stall-detection mechanism — no code change needed in `turn-loop.ts` (the existing `finishError` terminal already retains the buffer).

**Scope note (F2)**: The timeout is intentionally applied to `player` and `planner` ONLY. `initInstruction`/`postCompactInstruction` are covered by the init-turn total timeout (FR-009); `compress` is out of scope — applying the timeout via `setNodeDefaults` would extend it to nodes whose event patterns were not analyzed (see plan.md Summary).

**⚠️ CRITICAL**: US1, US2, and US3 all depend on this phase. Without it, stalled model calls hang indefinitely.

### 文档清单

- **代码规范文档**: `style/javascript.md`（引用 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) 作为 TS 规范基准，需同时阅读）；本 phase 无涉及测试，javascript.md 的测试章节非必读
- **官方文档**: [LangGraph `TimeoutPolicy` type definition](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts) — installed at `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/pregel/utils/timeout.d.ts`; [LangGraph `StateGraph.addNode` timeout option](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/graph/state.ts) — installed at `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/graph/state.d.ts` (addNode options / `NodeSpec`); [LangGraph `NodeTimeoutError` and `isNodeTimeoutError`](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/errors.ts) — installed at `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/errors.d.ts` and `errors.js` lines 146–193
- **技术文章**: 无

### Tasks

- [X] T002 Configure per-node `idleTimeout` on the `player` and `planner` nodes in `projects/game/agent/src/team/graph.ts`. The current code at line 365 is `const graph = new StateGraph(TeamState)` followed by chained `.addNode(...)` calls. Add the `timeout` option to the existing `addNode("player", playerNode)` and `addNode("planner", plannerNode)` calls: `{ timeout: { idleTimeout: STREAM_IDLE_TIMEOUT_MS, refreshOn: "auto" } }` (per contract §1.1; verify the option signature via the installed `.d.ts`). Import `STREAM_IDLE_TIMEOUT_MS` from `../llm`. Do NOT use `setNodeDefaults` — it would apply the timeout to ALL nodes (initInstruction, postCompactInstruction, compress), which is out of scope (plan.md Summary / contract §1.1). Per the contract §1.2 + research.md R7, `refreshOn: "auto"` refreshes on LangChain callback events (model tokens + tool start/end) at tool boundaries; the mid-tool gap is covered by the client-side MCP heartbeat wrapper added in T008b-r (applied in `buildSaoleiMcpTools`).

**Checkpoint**: Node-level timeout active on player/planner. Any model call that stalls for >30s triggers `NodeTimeoutError`. Verify with `bazel build //projects/game/agent/src:graph` and `bazel test //projects/game/agent/src:graph_test`.

---

## Phase 3: User Story 1 — Stall Detection & Recovery (Priority: P1) 🎯 MVP

**Goal**: When the LLM stream stalls mid-turn, `NodeTimeoutError` propagates from the model-holding nodes through the graph to `runTeamTurn` → `runLoop` → `finishError` (emit `warn` + `wait`, retain buffer, return to idle). Without this phase, the player and planner nodes SWALLOW `NodeTimeoutError` via their existing catch/finally patterns, defeating the stall recovery.

**Independent Test**: Inject a fake `createAgentFn` that throws `NodeTimeoutError` into the player node. Assert the node RE-THROWS it (not swallows). Inject the same into the planner node. Assert it re-throws. Then verify the full path: a stalled model call → `warn` frame + `wait` frame emitted → session returns to idle. See [quickstart.md](quickstart.md) Scenario 1.

### 文档清单

- **代码规范文档**: `style/javascript.md`（引用 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) 作为 TS 规范基准，需同时阅读；本 phase 涉及编写测试，同时需阅读 javascript.md 的测试章节：`vitest_test` 宏声明规则 lines 22–52、Mock 约定 lines 54–86；javascript.md 引用的 `specs/019-js-test-reliability/`（测试执行模型背景）与 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls) 一并阅读）
- **官方文档**: [LangGraph `isNodeTimeoutError` type guard](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/errors.ts) — `node_modules/.pnpm/@langchain+langgraph@1.4.8_*/node_modules/@langchain/langgraph/dist/errors.js` lines 192–194; the `NodeTimeoutError` class at lines 153–189 (fields: `name="NodeTimeoutError"`, `node`, `elapsed`, `kind`, `runTimeout`, `idleTimeout`)
- **技术文章**: 无

### Tasks

- [X] T003 [P] [US1] Restructure the player node's error handling in `projects/game/agent/src/team/player.ts` (lines 217–232). The current code uses `try { invoke } finally { return }` which swallows ALL exceptions (Feature 036 FR-002). Convert to `try/catch`: (1) in the `catch` block, call `consumeGameEvent(buffer)` as before (game-end event consumed on ALL paths); (2) if `isNodeTimeoutError(err)` is true, **re-throw** the error (do NOT swallow — it must propagate for stall recovery); (3) for all other errors, return the existing state update (swallow — FR-036 compatibility). Import `isNodeTimeoutError` from `@langchain/langgraph`. See contract §2.2 for the pseudocode.

- [X] T004 [P] [US1] Add `NodeTimeoutError` re-throw to the planner node's catch block in `projects/game/agent/src/team/planner.ts` (lines 363–379). The current catch degrades on ALL errors (logs warning, clears buffer, returns normally). Add a check at the TOP of the catch: if `isNodeTimeoutError(err)`, **re-throw** it before the degrade logic. Import `isNodeTimeoutError` from `@langchain/langgraph`. See contract §2.4.

- [X] T005 [US1] Add unit test to `projects/game/agent/src/team/player.test.ts`: inject a fake `createAgentFn` whose `invoke` rejects with a `NodeTimeoutError`-shaped error (`{ name: "NodeTimeoutError", node: "player", kind: "idle", idleTimeout: 30000, elapsed: 30001 }`). Assert the player node function THROWS (does not return a state update). Then inject a generic `Error` and assert the player node RETURNS normally (swallows the error, FR-036 compatibility). Also assert `consumeGameEvent` was called on both paths (verify the buffer's game event is consumed).

- [X] T006 [US1] Add unit test to `projects/game/agent/src/team/graph.test.ts` (where planner tests live — verified: graph.test.ts covers the planner path): verify the planner node re-throws `NodeTimeoutError` but degrades on other errors. Inject a fake `createAgentFn` that rejects with `NodeTimeoutError` → assert re-throw. Inject one that rejects with a generic `Error` → assert normal return (degrade).

**Checkpoint**: US1 complete. A stalled model call raises `NodeTimeoutError` → propagates through player/planner nodes → reaches `runLoop` catch → `finishError` (emit `warn` + `wait`, retain buffer, idle). Verify with `bazel test //projects/game/agent/src:player_test` (or `graph_test`) and `bazel test //projects/game/agent/src:graph_test`.

---

## Phase 4: User Story 2 — Queued Messages Survive Stall (Priority: P1)

**Goal**: When the turn terminates due to a stall (`NodeTimeoutError` → `finishError`), the user's queued messages are RETAINED in the buffer and auto-drained on the next turn. This is an EMERGENT PROPERTY of the existing `finishError` terminal (which already retains the buffer per Feature 030 FR-015) — no production code change is needed. This phase adds TESTS that verify the property holds for the new stall-trigger path.

**Independent Test**: Queue a message during a turn, trigger a stall (fake model that hangs), verify the message is retained after `finishError` and delivered as the next turn's input. See [quickstart.md](quickstart.md) Scenario 5.

### 文档清单

- **代码规范文档**: `style/javascript.md` 的测试章节（`vitest_test` 宏声明规则 lines 22–52、Mock 约定 lines 54–86；javascript.md 引用的 `specs/019-js-test-reliability/` 与 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls) 一并阅读）；本 phase 仅编写测试，无需阅读 Google TS Style Guide
- **官方文档**: 无
- **技术文章**: 无

### Tasks

- [X] T007 [US2] Add integration test to `projects/game/agent/src/session-team.test.ts` (or `turn-loop.test.ts`): build a `TurnLoop` with a fake `TurnRunner` that yields one block then throws a `NodeTimeoutError`-shaped error. Queue a message via `submit()` before the error. Assert the emitted frames include `warn` + `wait`, the buffer depth is unchanged after `finishError` (message retained), and a subsequent `submit()` with the retained buffer content starts a new turn that processes it. This verifies FR-006/FR-007 without modifying `turn-loop.ts` (the existing `finishError` path is reused).

**Checkpoint**: US2 complete. Verified that stall recovery retains the buffer and auto-drains on the next turn.

---

## Phase 5: User Story 3 — Tool Execution Not Falsely Detected (Priority: P1)

**Goal**: Verify that the `idleTimeout` with `refreshOn: "auto"` does NOT fire during legitimate tool execution (which can take up to 20 minutes via `bridge.dispatch` in the MCP server). Tool start/end events refresh the timer at tool **boundaries**, but during the MCP HTTP roundtrip + dispatch await no LangChain events fire — so a **client-side heartbeat wrapper is REQUIRED** (research.md R7.2): `withIdleHeartbeat(tool)` wraps each MCP client tool; during the tool's invoke it calls `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS`, keeping the idle timer alive for the full tool wait. This is applied in `buildSaoleiMcpTools` / `buildMemoryMcpTools` — the production choke points.

**⚠️ Rework notice**: The original T008b wired heartbeat into `OperationBridge.dispatch(part, signal, heartbeat)` and the mouse tools (`mouse_click`/`mouse_move`). This was ineffective in production because (a) the mouse tools are dead code since feature 031 (production uses `buildSaoleiMcpTools`), and (b) the MCP server's `bridge.dispatch` cannot access `config.heartbeat` (it lives in the LangGraph run on the other side of the MCP HTTP boundary — R7.1). T008b-r and T008c-r below revert the old approach and implement the client-side wrapper instead. See research.md R7 for the full MCP boundary analysis.

**Independent Test**: Verify the player/planner node specs include `refreshOn: "auto"` and `idleTimeout` is set (and that `setNodeDefaults` was NOT used). Verify the heartbeat wrapper: invoke a wrapped fake tool that hangs > idleTimeout with a heartbeat-providing config → heartbeat fires at TOOL_HEARTBEAT_INTERVAL_MS cadence → no `NodeTimeoutError`. For a full tool-execution-timeout test on the production MCP path, see the large test in Phase 7. See [quickstart.md](quickstart.md) Scenario 6.

### 文档清单

- **代码规范文档**: `style/javascript.md` 的测试章节（`vitest_test` 宏声明规则 lines 22–52、Mock 约定 lines 54–86；javascript.md 引用的 `specs/019-js-test-reliability/` 与 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls) 一并阅读）；本 phase 涉及生产代码修改（llm.ts、operation-bridge.ts），需阅读 Google TS Style Guide（[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**: [LangGraph `LangGraphRunnableConfig.heartbeat`](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts) — the `wrapConfig` heartbeat function (installed `dist/pregel/timeout.js`, unconditional `scope.touch()` refresh, research.md R7.2); [LangGraph `TimeoutPolicy`](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts) — installed `dist/pregel/utils/timeout.d.ts`; [`@langchain/mcp-adapters` `_callTool` — config propagation](https://github.com/langchain-ai/langchain/blob/main/libs/mcp-adapters/src/tools.ts) — installed `node_modules/.pnpm/@langchain+mcp-adapters@*/dist/tools.js` lines 351–420 (confirms `_callTool` reads `config.signal` but NOT `config.heartbeat`); [langchain `ToolNode.runTool` — config spread](https://github.com/langchain-ai/langchain/blob/main/libs/langchain/src/agents/nodes/ToolNode.ts) — installed `langchain/dist/agents/nodes/ToolNode.js` lines 229–241 (confirms `...config` is spread into tool invoke, so `heartbeat` is available on the MCP client tool's config)
- **技术文章**: 无

### Tasks

- [X] T008 [US3] Add unit test to `projects/game/agent/src/team/graph.test.ts`: build the team graph and verify the player and planner node specs include `timeout.idleTimeout` equal to `STREAM_IDLE_TIMEOUT_MS` and `timeout.refreshOn === "auto"`. This is a configuration verification test — it asserts the timeout is applied to player/planner (per contract §1.1) and that `setNodeDefaults` was NOT used (compress/initInstruction/postCompactInstruction must NOT carry the timeout). A full behavioral test (tool execution > idleTimeout without false positive) is covered by T008c-r + the large test in Phase 7 (T011).

- [X] T008b-r [US3] **Rework T008b**: replace the dispatch-side heartbeat with a client-side MCP wrapper. Three parts:
  1. **Revert `operation-bridge.ts`**: remove the `heartbeat?: () => void` third parameter from `dispatch()` and all associated interval/clearHeartbeat logic (lines 196–267 of the committed version). Restore the signature to `dispatch(part: FlowPart, signal?: AbortSignal): Promise<OperationResult>`. Remove the `TOOL_HEARTBEAT_INTERVAL_MS` import (it's now consumed by the wrapper in `llm.ts`, not by the bridge).
  2. **Revert `tools/mouse_click/mouse-click.ts` and `tools/mouse_move/mouse-move.ts`**: remove the heartbeat reading (`config as { heartbeat?: ... }`) and pass-through (`bridge.dispatch(part, signal, heartbeat)` → `bridge.dispatch(part, signal)`). These are dead code in production but the revert keeps them consistent with the new wrapper-based design (research.md R7.3).
  3. **Add `withIdleHeartbeat` to `llm.ts`**: implement a `withIdleHeartbeat(tool: StructuredToolInterface): StructuredToolInterface` wrapper function that (a) on each invoke, reads `config.heartbeat` from the tool's invoke config (present because `ToolNode.runTool` spreads `...config` — verified in `langchain/dist/agents/nodes/ToolNode.js:229-241`); (b) calls `heartbeat()` immediately, then starts `setInterval(heartbeat, TOOL_HEARTBEAT_INTERVAL_MS)`; (c) invokes the underlying tool's `.invoke(args, config)`; (d) in a `finally` block, clears the interval (no leaked timers); (e) if `config.heartbeat` is absent/undefined, degrades to a direct passthrough (no interval). Then modify `buildSaoleiMcpTools` and `buildMemoryMcpTools` to wrap each tool from `client.getTools()` with `withIdleHeartbeat` before returning. See contract §1.2 / research.md R7.2.

- [X] T008c-r [US3] **Rework T008c**: replace the dispatch heartbeat tests with wrapper unit tests. Add tests to `projects/game/agent/src/llm.test.ts` (or a new `tool-heartbeat.test.ts`): (1) wrap a fake tool whose invoke hangs for > `STREAM_IDLE_TIMEOUT_MS` (fake timers); invoke the wrapped tool with a config containing a `heartbeat: vi.fn()` → assert heartbeat is called immediately + at `TOOL_HEARTBEAT_INTERVAL_MS` cadence; resolve the fake tool → assert the interval is cleared (`vi.getTimerCount() === 0`); (2) invoke the wrapped tool with NO heartbeat in config → assert the fake tool is invoked directly (no interval set, `vi.getTimerCount()` unchanged except for the fake tool's own timers); (3) the fake tool rejects → assert the interval is still cleared (finally block). Remove the old dispatch-heartbeat tests from `operation-bridge.test.ts` (the `describe("OperationBridge dispatch heartbeat (043 US3, T008c)")` block).

**Checkpoint**: US3 complete. The timeout configuration is verified on player/planner only, and the client-side heartbeat wrapper keeps the idle timer alive during long MCP tool execution on the production path.

---

## Phase 6: User Story 4 — Init Instruction Turn Timeout (Priority: P2)

**Goal**: The async init instruction turn (`runInitTurn`) has a bounded total execution time (default 120s). If the planner LLM stalls during the init turn, the turn times out and degrades (skip instruction, log warning, resolve the init promise) within the bounded window, unblocking the first user turn.

**Independent Test**: Set `GAME_INIT_TURN_TIMEOUT_MS=1000`, inject a graph whose `invoke` never resolves, verify `runInitTurn` resolves (degrades) within ~1s. See [quickstart.md](quickstart.md) Scenario 4.

### 文档清单

- **代码规范文档**: `style/javascript.md`（引用 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) 作为 TS 规范基准，需同时阅读；本 phase 涉及编写测试，同时需阅读 javascript.md 的测试章节：`vitest_test` 宏声明规则 lines 22–52、Mock 约定 lines 54–86；javascript.md 引用的 `specs/019-js-test-reliability/` 与 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls) 一并阅读）
- **官方文档**: [Node.js `AbortSignal.timeout()`](https://nodejs.org/api/globals.html#abortsignaltimeoutdelay) — Node 24 native (project uses Node v24.14.1)
- **技术文章**: 无

### Tasks

- [X] T009 [US4] Add `signal: AbortSignal.timeout(INIT_TURN_TIMEOUT_MS)` to the `graph.invoke` config in `runInitTurn` at `projects/game/agent/src/session-team.ts` (lines 441–468). Import `INIT_TURN_TIMEOUT_MS` from `./llm`. The existing catch (lines 469–477) already handles any invoke error (logs warning, resolves the promise — degrade). The `AbortSignal.timeout` expiry produces a timeout error caught by this existing catch. No change to the degrade logic. See contract §4 and research.md R5.

- [X] T010 [US4] Add unit test to `projects/game/agent/src/session-team.test.ts`: set `GAME_INIT_TURN_TIMEOUT_MS=1000` (via `process.env` override or test-only injection), inject a fake graph whose `invoke` returns a never-resolving promise (simulating a stall). Assert `runInitTurn` resolves (not rejects) within ~1.5s and the degrade log message is emitted. Also verify that a subsequent `runTeamTurn` call does NOT block (the init promise resolved via degrade).

**Checkpoint**: US4 complete. Init instruction turn is bounded by a total timeout. Verify with `bazel test //projects/game/agent/src:session-team_test`.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Large test (testplan) for end-to-end stall recovery validation, and final build/test pass.

### 文档清单

- **代码规范文档**: `style/large_test.md`（大型测试规范，testplan/guitar 执行模型；large_test.md 要求大型测试代码使用 golang 编写并**必须遵守 `style/golang.md` 的单元测试规范** — 需同时阅读 `style/golang.md` 及其引用的 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md` 的测试章节（Mock 约定 lines 54–86，用于 fake-llm 的 DI 模式；javascript.md 引用的 `specs/019-js-test-reliability/` 与 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls) 一并阅读）
- **官方文档**: 无
- **技术文章**: 无

### Tasks

- [X] T011 Large test — create or update a testplan in `projects/game/testplan/` that validates end-to-end stall recovery: (1) deploy the agent with the new timeout config; (2) start a saolei session turn; (3) simulate an LLM stream stall (fake-llm that sends partial reasoning then stops, connection alive); (4) verify the desktop receives a `warn` frame + `wait` frame within ~30s (the configured idleTimeout); (5) verify queued messages are retained and delivered on the next turn. Also verify **saolei MCP tool execution >30s does NOT trigger a false stall**: have the LLM emit a `saolei_operate` tool call, delay the desktop's tool response by > 30s (longer than the `idleTimeout`), verify no `NodeTimeoutError`/false stall occurs — the idle timer is kept alive by the client-side heartbeat wrapper (`withIdleHeartbeat` calling `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS`, research.md R7.2). **This MUST exercise the production saolei MCP path** (`saolei_operate` via `buildSaoleiMcpTools` → MCP HTTP → `bridge.dispatch`), NOT the dead-code mouse tools. Use the `testplan` skill (`guitar run <plan.yaml>`) per `style/large_test.md`. See [quickstart.md](quickstart.md) Scenarios 5 & 6.

- [X] T012 Run `bazel build //projects/game/agent/...` and `bazel test //projects/game/agent/...` to verify all changes compile and pass. Then run `bazel run //:gazelle projects/game/agent` to update BUILD.bazel if new imports were added (e.g., `isNodeTimeoutError` from `@langchain/langgraph`). Run `bazel run //:go -- fmt` on any modified TypeScript files if applicable (formatting via the JS toolchain).

- [X] T013 [US1] [SC-006] Regression test — add to `projects/game/agent/src/turn-loop.test.ts`: verify the existing abort semantics are UNCHANGED by the stall feature. (1) Start a turn with a fake `TurnRunner` that blocks; call `abort()`; assert the buffer is CLEARED and `QueueSignal(0)` + `wait` are emitted (Feature 030 FR-011 / FR-012 — user abort clears buffer, unchanged). (2) Assert a connection-drop abort (stream close → abort path) also clears the buffer. This closes the SC-006 coverage gap (existing abort semantics exhibit zero regressions).

**Checkpoint**: All phases complete. Full build + unit tests + large test pass.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — can start immediately. ✅ DONE (T001 committed).
- **Phase 2 (Foundational)**: Depends on T001 (constants). BLOCKS US1/US2/US3. ✅ DONE (T002 committed).
- **Phase 3 (US1)**: Depends on Phase 2 (timeout must be configured) + T001. T003/T004 are parallel (different files). T005/T006 depend on T003/T004 respectively. ✅ DONE (T003–T006 committed).
- **Phase 4 (US2)**: Depends on Phase 3 (US1 — stall must propagate to finishError for the test to verify buffer retention). No production code change — test only. ✅ DONE (T007 committed).
- **Phase 5 (US3)**: Depends on Phase 2 (timeout config must be in place) + T001 (TOOL_HEARTBEAT_INTERVAL_MS). T008 (config test) ✅ DONE. **T008b-r and T008c-r are REWORK tasks**: they revert the original (ineffective) T008b/T008c and implement the client-side wrapper instead. T008c-r depends on T008b-r. Production code change: operation-bridge.ts (revert) + mouse tools (revert) + llm.ts (new wrapper).
- **Phase 6 (US4)**: Depends on T001 (constants). Independent of Phases 2–5. Can run in parallel. Developer completed; pending commit.
- **Phase 7 (Polish)**: Depends on all prior phases (including Phase 5 rework). T011 (large test) MUST exercise the production saolei MCP path with the client-side wrapper. T013 (SC-006 regression) depends on Phase 3 (abort path is baseline behavior; test asserts unchanged semantics).

### User Story Dependencies

- **US1 (P1)**: Depends on Phase 2 (node timeout). MVP core. ✅ Delivered.
- **US2 (P1)**: Emergent from US1 (finishError retains buffer). Test verifies the property. ✅ Delivered.
- **US3 (P1)**: Depends on Phase 2 (node timeout) + the client-side heartbeat wrapper (T008b-r) — the timer is only refreshed at tool boundaries without it, and the MCP HTTP boundary prevents dispatch-side heartbeat (research.md R7.1). **T008b/T008c committed but INEFFECTIVE in production — rework via T008b-r/T008c-r required before US3 is truly satisfied.**
- **US4 (P2)**: Independent of US1–US3. Can be developed in parallel.

### Parallel Opportunities

- T003 (player.ts) and T004 (planner.ts) are different files → parallel. ✅ Done.
- Phase 5 rework (T008b-r) and Phase 6 (US4) are independent → parallel.
- T005 and T006 are different test files → parallel (after their implementation tasks). ✅ Done.
- T008 (graph config test) is already done; T008b-r is the rework.

---

## Implementation Strategy

### MVP First (US1 + US2 + US3)

1. ~~Complete Phase 1: Setup (constants)~~ ✅ DONE
2. ~~Complete Phase 2: Foundational (node-level idleTimeout on player/planner)~~ ✅ DONE
3. ~~Complete Phase 3: US1 (player + planner error classification)~~ ✅ DONE
4. ~~STOP and VALIDATE~~ ✅ DONE — T005/T006 unit tests pass.
5. ~~Complete Phase 4: US2 (buffer retention test)~~ ✅ DONE
6. **REWORK Phase 5: US3** — revert the ineffective T008b/T008c (dispatch heartbeat + mouse tool wiring) and implement the client-side MCP heartbeat wrapper (T008b-r/T008c-r). The original T008 (graph config test) is kept.
7. **STOP and VALIDATE**: Run all unit tests including the reworked T008c-r. The MVP is complete only after the wrapper is verified.

### Incremental Delivery

1. ~~Setup + Foundational → timeout active~~ ✅ (stalls detected, but errors swallowed by player/planner)
2. ~~Add US1 → errors propagate~~ ✅ → full stall recovery (warn + wait + buffer retained)
3. ~~Add US2~~ ✅ → verify buffer retention
4. **REWORK US3** → client-side heartbeat wrapper for MCP tools (replaces ineffective dispatch heartbeat)
5. ~~Add US4 → init turn timeout~~ (developer completed, pending commit)
6. Large test → end-to-end validation on the production saolei MCP path

---

## Notes

- US2 requires NO production code changes — it is an emergent property of the existing `finishError` terminal, verified by tests. US3 requires the client-side heartbeat wrapper (T008b-r): LangGraph's `idleTimeout` refreshes on callback events at tool boundaries only; during a long saolei MCP tool execution (up to 20 min via `bridge.dispatch` in the MCP server) no events fire, and the MCP HTTP boundary prevents `config.heartbeat` from reaching the server-side dispatch (research.md R7.1). So the heartbeat is driven by a **client-side wrapper** (`withIdleHeartbeat`, applied in `buildSaoleiMcpTools`) calling `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS` during the MCP tool's invoke — this refreshes the idle timer unconditionally (research.md R7.2). This is by design (Constitution §II: the LangGraph `TimeoutPolicy` + client-side wrapper + existing `finishError` terminal).
- **Rework history**: the original T008b wired heartbeat into `OperationBridge.dispatch(part, signal, heartbeat)` and the mouse tools. This was committed but is INEFFECTIVE in production: the mouse tools (`mouse_click`/`mouse_move`) are dead code since feature 031 (production uses `buildSaoleiMcpTools`), and the MCP server's `bridge.dispatch` has no access to `config.heartbeat` (MCP HTTP boundary). T008b-r reverts the old approach and implements the client-side wrapper. The old T008c tests are replaced by T008c-r.
- `turn-loop.ts` is NOT modified by this feature — the existing `finishError` terminal already retains the buffer (Feature 030 FR-015). The `NodeTimeoutError` arrives with `controller.signal.aborted === false`, so the existing catch logic correctly routes to `finishError`.
- The timeout is applied per-node via `addNode(name, fn, { timeout: ... })` options for `player` and `planner` ONLY (contract §1.1). `setNodeDefaults` is intentionally NOT used — it would apply the timeout to `initInstruction`/`postCompactInstruction`/`compress`, whose event patterns were not analyzed (plan.md Summary, F2 scope decision). The `addNode` timeout option is verified in the installed LangGraph 1.4.8 type declarations (`dist/graph/state.d.ts` — `StateGraphAddNodeOptions` / `NodeSpec`).
