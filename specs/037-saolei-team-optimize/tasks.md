# Tasks: saolei Team 模板优化

**Input**: Design documents from `specs/037-saolei-team-optimize/`

**Prerequisites**: `plan.md` (required), `spec.md` (required), `research.md`, `data-model.md`, `contracts/compression-contract.md`, `contracts/game-stats-contract.md`, `quickstart.md`

**Organization**: Tasks are grouped by user story (US1=P1, US2=P1, US3=P2, US4=P2, US5=P2). US1 establishes the `emitChannelFrame` mechanism that US2 reuses. US4 (desktop FIFO) is fully independent.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to
- All file paths are relative to repository root

## 关键设计决策（tasks.md 实现时遵循）

1. **`emitChannelFrame` 通过 LangGraph `configurable` 传递**（非 TeamGraphDeps 注入）：`runTeamTurn` 在 `streamEvents` 的 config 中设置 `configurable.emitChannelFrame`（类型 `(agent: string, content: string) => void`），planner/compress 节点从 `config?.configurable?.emitChannelFrame` 读取。这避免了修改 TeamGraphDeps / PlannerNodeDeps / SessionTeam 构造器 / server.ts factory（宪法原则 II：简化）。
2. **编译 + 单测** 作为每个代码变更的一部分执行（宪法原则 IV），不单列 task。每个 phase 结束时运行 `bazel build //projects/game/agent/... && bazel test //projects/game/agent/src/team:lib_test`（及相关测试 target）。
3. **大型测试** 单独分配 task（宪法原则 VI / 原则 IV）。

---

## Phase 1: Setup

**Purpose**: 基线验证，确认现有代码可编译可测试。

**文档清单**:
- 代码规范文档: 无
- 官方文档: 无
- 技术文章: 无

- [X] T001 Verify baseline build and tests pass: `bazel build //projects/game/agent/... && bazel test //projects/game/agent/src/team:lib_test && bazel test //projects/game/agent/src/mcp/saolei:lib_test`

**Checkpoint**: 基线绿色，可以开始变更。

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 共享基础设施——gameCounter 状态字段 + emitChannelFrame 传递机制。MUST 完成后才能开始 US1/US2。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、DI 测试约定、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）

- [X] T002 [P] Extend `TeamStateValue` interface in `projects/game/agent/src/team/state.ts` — add `gameCounter: number` field (integer, per-session game counter)
- [X] T003 Extend `TeamState` schema in `projects/game/agent/src/team/graph.ts` — add `gameCounter: Annotation<number>({ reducer: (_p: number, n: number) => n, default: () => 0 })` to the `Annotation.Root` (last-write-wins reducer, same pattern as `gameEnded`); do NOT change routing yet (routing changes are in Phase 4/US2)
- [X] T004 Define `ChannelFrameEmitter` type and pass `emitChannelFrame` through `configurable` in `projects/game/agent/src/session-team.ts` — in `runTeamTurn`, add `emitChannelFrame` to the `streamEvents` config's `configurable` object; the emitter constructs a `TeamFrame` via `buildTeamFrame(this.sessionId, this.template, { agent, messageParts: { parts: [{ text: { content } }] } })` and calls `this.turnLoopEmit?.(frame)`; export the type `type ChannelFrameEmitter = (agent: string, content: string) => void` from this module
- [X] T005 [P] Extend `refreshTeamChannels` in `projects/game/agent/src/context-middleware.ts` — add `gameCounter: 0` to the `graph.updateState` call alongside the existing channel clears (FR-014: RefreshTeam resets gameCounter with short-term memory)

**Checkpoint**: State schema extended, emitChannelFrame wired. `bazel build //projects/game/agent/... && bazel test //projects/game/agent/src/team:lib_test` passes (existing tests should still pass — gameCounter defaults to 0, emitChannelFrame is additive).

---

## Phase 3: User Story 1 — planner 游戏历史消息实时可见 (Priority: P1) MVP

**Goal**: planner 的复盘输入（游戏历史消息）在 desktop planner 标签页实时显示，无需重新进入 session。

**Independent Test**: 构建 team graph，注入 `emitChannelFrame` 录制回调；驱动一局游戏结束触发 planner 复盘；断言 emitChannelFrame 被调用且携带 `agent="planner"` + 完整游戏历史内容。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、DI 测试约定、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）、`specs/031-team-template-mode/bug-analysis.md` Issue 2 "复盘输入可见性"（bug 根因：streamEvents 不产出 createAgent 输入 HumanMessage）、`specs/036-team-mode-bugfix/contracts/team-graph-fix-contract.md` §2.2（reviewInput 内容=完整 gameLog）

- [X] T006 [US1] Implement reviewInput real-time frame emission in `projects/game/agent/src/team/planner.ts` — in `createPlannerNode`'s returned node function, after constructing `reviewInput` via `buildReviewInput(buffer)` and before calling `invokeWithRetry`, read `config?.configurable?.emitChannelFrame` as `ChannelFrameEmitter | undefined`; if present and reviewInput has non-empty string content, call `emitChannelFrame(PLANNER_AGENT_NAME, reviewContent)` (FR-001); the frameId/messageId dedup is handled by desktop's `renderedMessageIds` (FR-003); ensure the "no game record" message (gameLog empty) is also emitted (FR-004)
- [X] T007 [US1] Add integration test in `projects/game/agent/src/team/graph.test.ts` — build team graph with a `emitChannelFrame` recording callback in configurable; drive one game to completion (fake tool triggers onGameEnd); assert: emitChannelFrame was called with `agent="planner"` and content containing the full game process (tool names, coordinates, board states); assert the emitted content matches what buildReviewInput produces

**Checkpoint**: planner 游戏历史消息实时可见（SC-001）。`bazel test //projects/game/agent/src/team:lib_test` passes. 桌面级验证（手动，`quickstart.md` 场景 1）：planner tab 实时出现复盘输入、重载不重复（FR-003 / US1 AS1/AS3）；desktop 组件测试基础设施就绪前以手动验证为准。

---

## Phase 4: User Story 2 — 每 5 局触发 player/planner 历史上下文压缩 (Priority: P1)

**Goal**: 每 5 局游戏结束后触发 player/planner 通道全量压缩为一条摘要 AIMessage；压缩后 player 停下等待用户输入。

**Independent Test**: 连续驱动 5 局游戏结束；断言第 5 局后 gameCounter===5，graph 路由到 compress 节点，player/planner 通道各收缩为 1 条摘要，策略保留，turn 结束（路由 END）。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、DI 测试约定、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）、[LangGraph JS — How to add summary of conversation history](https://github.com/langchain-ai/langgraphjs/blob/main/examples/how-tos/add-summary-conversation-history.ipynb)（RemoveMessage + 摘要替换模式）、[LangChain — Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory)（消息删除/摘要策略）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）、`specs/037-saolei-team-optimize/research.md` D1（压缩方案对比与决策）、D7（graph 路由交互）、D8（摘要提示词设计）、D9（实时帧发射与去重）、`specs/031-team-template-mode/contracts/team-graph-contract.md` §1/§5（state schema + channel clearing 机制）、`specs/037-saolei-team-optimize/contracts/compression-contract.md`（完整压缩节点契约）

- [X] T008 [US2] Create compress node in `projects/game/agent/src/team/compress.ts` — export `createCompressNode(deps: CompressNodeDeps)` where `CompressNodeDeps = { playerModel: ChatModel; plannerModel: ChatModel }`; the node function `(state: TeamStateValue, config?: RunnableConfig) => Promise<Partial<TeamStateValue>>`: (1) for each non-empty channel, serialize messages to text, call `model.invoke([{ role: "human", content: summaryPrompt + serialized }])` to get an AIMessage summary (use playerModel for playerMessages, plannerModel for plannerMessages); validate summary content is non-blank (trim length > 0) — empty/whitespace summary → re-throw (FR-012/FR-013); (2) construct channel update `[new RemoveMessage({ id: REMOVE_ALL_MESSAGES }), summaryAIMessage]` per channel; (3) if LLM call throws → re-throw (FR-013: abort); (4) read `config?.configurable?.emitChannelFrame` and emit summary frames (FR-011); (5) empty channel = skip (FR-015); import `RemoveMessage` from `@langchain/core/messages`, `REMOVE_ALL_MESSAGES` from `@langchain/langgraph`; use the summary prompts from `specs/037-saolei-team-optimize/contracts/compression-contract.md` §3
- [X] T009 [US2] Add gameCounter increment to planner node in `projects/game/agent/src/team/planner.ts` — in the node return (both success and degrade paths), add `gameCounter: state.gameCounter + 1` alongside the existing `gameEnded: null` return
- [X] T010 [US2] Add compress node and conditional routing to `buildTeamGraph` in `projects/game/agent/src/team/graph.ts` — (1) add `routeAfterPlanner` function: `return state.gameCounter > 0 && state.gameCounter % 5 === 0 ? "compress" : "player"`; (2) construct compress node via `createCompressNode({ playerModel: deps.playerModel, plannerModel: deps.plannerModel })`; (3) `.addNode("compress", compressNode)`; (4) change `.addEdge("planner", "player")` to `.addConditionalEdges("planner", routeAfterPlanner)`; (5) `.addEdge("compress", END)` (player stops, FR-010)
- [X] T011 [US2] Add integration tests in `projects/game/agent/src/team/graph.test.ts` — (a) drive 5 consecutive game endings (each triggers planner; mix won and lost outcomes — both increment gameCounter, FR-006); assert after 5th: `gameCounter === 5`, `playerMessages.length === 1` (summary AIMessage with non-blank content, FR-012), `plannerMessages.length === 1`, strategy unchanged, graph routed to END (not player); (b) drive game 6 after compression: player resumes with summary context; (c) compression failure: make fake model throw on summary call → assert error propagates (abort); (d) empty channel: skip compression for empty channel; (e) gameCounter at non-5 multiples: no compression triggered; (f) compression summary frames: after compression in (a), assert `emitChannelFrame` was called twice with `agent="player"` and `agent="planner"`, each with non-empty content matching the summary model output (FR-011, SC-004); (g) RefreshTeam after compression: after (a), invoke `refreshTeamChannels` (graph.updateState with `RemoveMessage({ id: REMOVE_ALL_MESSAGES })` + `gameCounter: 0`); assert `playerMessages`/`plannerMessages` cleared (summary gone), `gameCounter === 0`, strategy unchanged (FR-014, US2 AS8)

**Checkpoint**: 每 5 局触发压缩、通道收缩、策略保留、player 停下、压缩失败 abort（SC-002/SC-003/SC-004）。`bazel test //projects/game/agent/src/team:lib_test` passes.

---

## Phase 5: User Story 3 — planner 系统提示词注入 player 工具描述 (Priority: P2)

**Goal**: planner 的系统提示词包含 player 工具的名称与描述清单（静态文本），但 planner 实际工具集仍仅 `update_strategy`。

**Independent Test**: 捕获 planner createAgent 的 systemPrompt，断言包含每个 player 工具的 name + description；断言 planner 工具集仍仅 `update_strategy`。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）、`specs/037-saolei-team-optimize/contracts/compression-contract.md` §4（工具描述注入设计：buildToolDescriptionSection 格式）、`specs/031-team-template-mode/spec.md` FR-028（模板固定装配工具集）

- [X] T012 [US3] Pass `playerTools` to planner node in `projects/game/agent/src/team/graph.ts` — add `playerTools: deps.playerTools` to the `createPlannerNode({...})` call in `buildTeamGraph` (the tools are already in `TeamGraphDeps`, just need to forward them)
- [X] T013 [US3] Implement tool description injection in `projects/game/agent/src/team/planner.ts` — (1) add `playerTools: StructuredToolInterface[]` to `PlannerNodeDeps`; (2) add a `buildToolDescriptionSection(tools)` helper that formats each tool's `name` and `description` into a markdown section (see `specs/037-saolei-team-optimize/contracts/compression-contract.md` §4 for the exact format); (3) append the section to `systemPrompt` (after the base prompt, before createAgent); (4) planner's actual `tools` array stays `[buildUpdateStrategyTool(strategyStore, sessionId)]` only (FR-018)
- [X] T014 [US3] Add unit test in `projects/game/agent/src/team/graph.test.ts` — capture planner's createAgent options via `createAgentFn` DI spy; assert `systemPrompt` contains each player tool name and description; assert `tools` array length is 1 (only update_strategy)

**Checkpoint**: planner 提示词包含工具描述、工具集不变（SC-005）。`bazel test //projects/game/agent/src/team:lib_test` passes.

---

## Phase 6: User Story 5 — saolei MCP game end 游戏统计数据 (Priority: P2)

**Goal**: saolei MCP 在 game end 事件中计算并携带三项游戏统计数据（operationCount / correctFlags / avgOpsPerMine），数据纳入 planner 复盘 message。

**Independent Test**: 构造已知操作序列与地雷布局的 fake 游戏，验证 onGameEnd 携带的 GameStats 数值正确；验证 buildReviewInput 包含统计文本；验证 y=0 与 counter 不可解码的降级处理。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、纯函数测试约定、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）、`specs/037-saolei-team-optimize/contracts/game-stats-contract.md`（完整统计计算契约）、`specs/037-saolei-team-optimize/data-model.md` §5（GameStats 数据结构与流转）、`specs/031-team-template-mode/contracts/saolei-sink-contract.md`（现有 sink 接口契约）

- [X] T015 [P] [US5] Define `GameStats` type and extend `SaoleiEventSink.onGameEnd` with optional `stats?` parameter in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — add `interface GameStats { operationCount: number; correctFlags: number | null; avgOpsPerMine: number | "N/A" }`; change `onGameEnd` signature to `(state: GameState, status: "won" | "lost", stats?: GameStats)` (backward compatible — optional param, FR-019 unchanged)
- [X] T016 [US5] Implement `computeGameStats` pure function and MCP closure tracking in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — (1) add closure variables `let initState: GameState | null = null` and `let operationCount = 0`; (2) in `saolei_init` handler: set `initState = state` and `operationCount = 0` after successful recognize; (3) in `registerCellTool` handler: `operationCount++` after successful recognize, before `runSink("onMove")`; (4) implement `computeGameStats(initState, finalState, operationCount): GameStats` — correctFlags = totalMines(initState.mineCounter.value) − MINE格数 − HIT_MINE格数; counter undecodable → correctFlags = null; avgOpsPerMine = correctFlags > 0 ? round(operationCount/correctFlags * 100)/100 : "N/A"; (5) in `onGameEnd` call: pass `computeGameStats(initState, state, operationCount)` as third arg
- [X] T017 [P] [US5] Extend `GameEventRecord` with optional `stats?` field and update `createTeamSink.onGameEnd` in `projects/game/agent/src/team/team-sink.ts` — add `stats?: GameStats` to `GameEventRecord` interface; update `onGameEnd` sink callback to accept and store `stats` into `buffer.gameEvent.stats`; import `GameStats` type from saolei-mcp module
- [X] T018 [US5] Extend `buildReviewInput` to render game stats in `projects/game/agent/src/team/planner.ts` — after the game log lines and before the review instruction line, read `buffer.gameEvent?.stats`; if present, append a "本局统计数据：" section with operationCount, correctFlags (or "不可用" if null), and avgOpsPerMine (see `specs/037-saolei-team-optimize/contracts/game-stats-contract.md` §5 for exact format)
- [X] T019 [US5] Add unit tests in `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` — (a) known game: assert operationCount, correctFlags, avgOpsPerMine match expected values; (b) won game: MINE=0, HIT_MINE=0, correctFlags = totalMines; (c) lost game: correctFlags = totalMines − MINE − HIT_MINE; (d) y=0 (instant loss): avgOpsPerMine = "N/A"; (e) counter undecodable: correctFlags = null, avgOpsPerMine = "N/A"; (f) rejected moves not counted in operationCount; (g) init/remain not counted
- [X] T020 [US5] Add team-sink + buildReviewInput tests — (a) in `projects/game/agent/src/team/team-sink.test.ts`: call `createTeamSink(...).onGameEnd(state, status, stats)`; assert `buffer.gameEvent.stats` stored (FR-031); (b) planner unit test (in `projects/game/agent/src/team/` test files): call `buildReviewInput(buffer)` with stats present → output contains "本局统计数据：" with operationCount / correctFlags / avgOpsPerMine; with stats absent → no stats section; with `correctFlags` null → renders "不可用" (FR-032, US5 Independent Test item b)

**Checkpoint**: 游戏统计数据计算正确、降级处理完善、复盘包含统计（SC-008）。`bazel test //projects/game/agent/src/mcp/saolei:lib_test && bazel test //projects/game/agent/src/team:lib_test` passes.

---

## Phase 7: User Story 4 — desktop 对话消息显示数量上限 FIFO (Priority: P2)

**Goal**: desktop 每个 agent 标签页的显示消息受数量上限约束，超出按 FIFO 移除最旧消息。

**Independent Test**: 向某 agent tab 注入超过上限的消息，验证最旧被移除、仅保留上限条数；验证不同 tab 独立计数。

**文档清单**:
- 代码规范文档: `style/javascript.md`（TS 规范、[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档: [Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（javascript.md §测试 引用的 DI-over-mock 依据）
- 技术文章: `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（javascript.md §Bazel 引用的 shim 退出码契约：fail-closed / 空套件 vacuous pass）、`specs/031-team-template-mode/contracts/desktop-contract.md`（desktop 多标签页契约）、`specs/037-saolei-team-optimize/data-model.md` §7（FIFO 实现方案 + 写入位置清单）

- [X] T021 [P] [US4] Implement `trimFifo` and `MAX_CHAT_ENTRIES_PER_AGENT` in a new pure module `projects/game/desktop/frontend/src/chat-fifo.ts` — `export const MAX_CHAT_ENTRIES_PER_AGENT = 200` and `export function trimFifo<T>(entries: T[], max: number = MAX_CHAT_ENTRIES_PER_AGENT): T[] { return entries.length > max ? entries.slice(-max) : entries }`; App.svelte imports from this module (pure module enables unit testing without a Svelte harness)
- [X] T022 [P] [US4] Add `projects/game/desktop/frontend/src/chat-fifo.test.ts` — (a) over-cap injection removes oldest (FIFO), keeps newest N; (b) at-cap/under-cap unchanged; (c) per-tab independence via separate arrays (FR-022); (d) history-load truncation (FR-024); (e) live+history mixed ordering (FR-025); bootstrap a vitest target for the frontend package (BUILD.bazel `vitest_test`/`js_test` macro per `style/javascript.md`) if none exists (FR-020..025, SC-006, US4 Independent Test)
- [X] T023 [US4] Apply `trimFifo` at all `chatMessages` write points in `projects/game/desktop/frontend/src/App.svelte` — wrap every `chatMessages = { ...chatMessages, [agent]: [...list, newEntry] }` assignment with `trimFifo(...)`: (1) `handleMessageParts` streaming merge path (~line 739) and new entry path (~line 744); (2) `loadAgentHistories` (~line 511); (3) `handleAgentFrame` warn branch (~line 787); (4) `handleSendChatText` optimistic user turn (~line 882); verify each write point applies trimFifo to the agent-specific array only (per-tab independent counting, FR-022)

**Checkpoint**: FIFO 生效、各 tab 独立计数（SC-006）。`bazel test //projects/game/desktop/frontend/...` passes（含 chat-fifo 单测）。FR-023（压缩时不显式清理旧消息）为 MUST-NOT 行为，由 code review 验证不存在显式清理代码路径。

---

## Phase 8: Polish & Large Test

**Purpose**: BUILD 更新、全量验证、大型测试验收。

**文档清单**:
- 代码规范文档: `style/large_test.md`（大型测试规范）、`style/golang.md`（大型测试代码用 Go，遵守单元测试规范：命名风格、表驱动、given/when/then 结构）、[Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)（golang.md 引用的 Go 风格规范基石，必读）
- 官方文档: 无
- 技术文章: `specs/037-saolei-team-optimize/quickstart.md`（验证场景清单）

- [X] T024 Run `bazel run //:gazelle` in `projects/game/agent/src/team/` to update BUILD.bazel for the new `compress.ts` file
- [X] T025 Run full build and test suite: `bazel build //... && bazel test //...` — fix any regressions
- [X] T026 Add large test cases to the existing saolei-team suite in `projects/game/testplan/` — add new test cases to the existing `system_test.yaml` suite `saolei-team` (or a new suite in the same file if topology differs); test cases (Go, `go_largetest` rule) covering: (1) planner game history real-time visibility; (2) 5-game compression trigger (channel shrink, player stops, strategy preserved); (3) planner tool description in systemPrompt (verify via log/trace); (4) game stats computation correctness (operationCount/correctFlags/avgOpsPerMine). Follow `style/large_test.md`: cases organized by tested module (not by spec number), use existing helpers, add to existing test plan YAML (not a new one)
- [ ] T027 Execute large test plan via testplan skill: `guitar run <plan.yaml>` — MUST complete full deploy→test→cleanup loop; all test cases MUST pass (宪法原则 VI); if any case fails, fix and re-run until fully green

---

## Dependencies & Execution Order

### Phase Dependencies

```
Phase 1 (Setup)
  └── Phase 2 (Foundational) — BLOCKS US1, US2
        ├── Phase 3 (US1) — emitChannelFrame usage
        │     └── Phase 4 (US2) — compress reuses emitChannelFrame (US1 dep)
        ├── Phase 5 (US3) — tool description (independent of US1/US2)
        ├── Phase 6 (US5) — game stats (independent of US1/US2/US3)
        ├── Phase 7 (US4) — desktop FIFO (fully independent, different project)
        └── ...
Phase 8 (Polish) — depends on all stories complete
```

### User Story Dependencies

| Story | Depends On | Reason |
|---|---|---|
| **US1 (P1)** | Phase 2 (Foundational) | Needs `emitChannelFrame` in configurable + gameCounter state field |
| **US2 (P1)** | US1 + Phase 2 | Compress node reuses `emitChannelFrame` mechanism; needs `gameCounter` for routing |
| **US3 (P2)** | Phase 2 | Needs graph.ts changes (pass playerTools to planner); no US1/US2 dependency |
| **US4 (P2)** | None | Pure frontend change (App.svelte); different project, fully independent |
| **US5 (P2)** | None | MCP sink extension + stats computation; independent (planner.ts change in T018 touches buildReviewInput which is also modified in US1's T006, but different code sections) |

### Within Each User Story

- State/interface changes before behavior changes
- Node implementation before graph wiring
- Unit/integration tests after implementation (same task as implementation)
- Compile + test after each task (宪法原则 IV)

### Parallel Opportunities

- **T002, T005** can run in parallel (different files: state.ts vs context-middleware.ts)
- **T015, T017** can run in parallel (different files: saolei-mcp.ts vs team-sink.ts)
- **T021, T022** can run in parallel with any Phase 3-6 task (completely different project: desktop frontend)
- **Phase 5 (US3), Phase 6 (US5), Phase 7 (US4)** can all run in parallel after Phase 2 completes (if team capacity allows), since they touch different files/concerns
- Within Phase 6: T015/T017 (interface definitions) can parallel before T016/T018 (implementations that depend on them)

### File Conflict Notes (Serialize These)

The following files are modified across multiple phases — edits to the SAME file MUST be serialized:

| File | Phases |
|---|---|
| `projects/game/agent/src/team/planner.ts` | Phase 3 (T006), Phase 4 (T009), Phase 5 (T013), Phase 6 (T018) |
| `projects/game/agent/src/team/graph.ts` | Phase 2 (T003), Phase 4 (T010), Phase 5 (T012) |

If working sequentially (recommended for single developer): Phase 3 → Phase 4 → Phase 5 → Phase 6 ensures planner.ts and graph.ts edits are applied in order.

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (baseline verification)
2. Complete Phase 2: Foundational (gameCounter + emitChannelFrame)
3. Complete Phase 3: US1 (planner reviewInput real-time visibility)
4. **STOP and VALIDATE**: Test US1 independently — planner game history visible in real-time
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 → Test (planner real-time visibility works) → **MVP demo**
3. US2 → Test (compression triggers at game 5) → Deploy/Demo
4. US3 → Test (planner prompt includes tool descriptions)
5. US5 → Test (game stats computed and in review input)
6. US4 → Test (desktop FIFO limits messages)
7. Phase 8 → Large test passes → Final validation

### Parallel Team Strategy

With multiple developers after Foundational:
- Developer A: US1 → US2 (sequential, shared mechanism)
- Developer B: US3 (planner.ts — coordinate with A for file conflicts)
- Developer C: US5 (MCP + team-sink — coordinate with A for planner.ts)
- Developer D: US4 (desktop frontend — fully independent, no coordination needed)

---

## Notes

- `emitChannelFrame` type is exported from `session-team.ts` for nodes to import
- `RemoveMessage` from `@langchain/core/messages`, `REMOVE_ALL_MESSAGES` from `@langchain/langgraph` (same imports as `context-middleware.ts`)
- Compression summary prompts are defined in `specs/037-saolei-team-optimize/contracts/compression-contract.md` §3
- GameStats computation logic is defined in `specs/037-saolei-team-optimize/contracts/game-stats-contract.md` §3
- Desktop FIFO write points are listed in `specs/037-saolei-team-optimize/data-model.md` §7
- Large test cases MUST be added to existing `projects/game/testplan/system_test.yaml` (not a new YAML), per `style/large_test.md` §测试计划数量
