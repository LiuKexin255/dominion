# Tasks: Team Template Mode 缺陷修复

**Input**: Design documents from `/specs/036-team-mode-bugfix/`

**Prerequisites**: [plan.md](./plan.md) (required), [spec.md](./spec.md) (required for user stories), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: 本特性包含测试任务——031-spec 已建立 fake-model + fake-tool DI 测试模式（`projects/game/agent/src/team/graph.test.ts`），缺陷修复 MUST 新增覆盖各 Issue 的测试用例（宪法原则 IV）。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Backend (agent)**: `projects/game/agent/src/team/`
- **Frontend (desktop)**: `projects/game/desktop/frontend/src/components/`

---

## Phase 1: Setup

**Purpose**: 无项目初始化——本特性为 bugfix，仅修改现有文件。本 phase 确认编译基线。

### 文档清单

- **代码规范文档**: 无（本 phase 仅执行编译命令确认基线，无代码编辑）
- **官方文档**: 无
- **技术文章**: 无

---

- [X] T001 确认当前代码基线编译通过（基线门禁：仅确认变更前编译状态，非新增代码的编译 task）：执行 `bazel build //projects/game/agent/src/team:lib //projects/game/desktop/frontend/src/components:lib`（或 `bazel build //...`），确认无编译错误

**验证门禁**: 现有代码编译通过

---

## Phase 2: Foundational (team-sink gameLog 数据层)

**Purpose**: 扩展 `EphemeralGameBuffer` 新增 `gameLog` 字段与 `GameLogEntry` 类型，修改 sink 回调累积日志。此为 US2（planner 复盘输入）的数据层前提，MUST 在 Phase 4 之前完成。

**⚠️ CRITICAL**: Phase 4（US2 planner 复盘）依赖此 phase 完成

### 文档清单

- **代码规范文档**: `style/javascript.md`（§测试：DI over `vi.mock`，`projects/game/agent/src/team/` 代码规范）
- **官方文档**: 无
- **技术文章**: 无

---

- [X] T002 [P] 在 `projects/game/agent/src/team/team-sink.ts` 中新增 `GameLogEntry` interface（字段：`tool: string`、`x?: number`、`y?: number`、`state: GameState`、`status: "won" | "lost" | "playing"`），并在 `EphemeralGameBuffer` interface 中新增 `gameLog: GameLogEntry[]` 字段。同时更新 `createEphemeralGameBuffer()` 返回值初始化 `gameLog: []`。参考 [`data-model.md`](./data-model.md) §1-2

- [X] T003 在 `projects/game/agent/src/team/team-sink.ts` 的 `createTeamSink(buffer)` 中修改三个 sink 回调以累积 gameLog：
  - `onGameStart(state)`：在现有 `buffer.gameState = state` 之后，**清空** `buffer.gameLog = []` 并 push 初始条目 `{ tool: "saolei_init", state, status: "playing" }`（每局重置——planner 仅复盘当前局）
  - `onMove(tool, x, y, state)`：在现有 `buffer.gameState = state` 之后，push 操作条目 `{ tool, x, y, state, status: <computed> }`。status 的计算：import `isTerminalState` from `"../mcp/saolei/saolei-mcp"`、`isWin` from `"@dominion/game-saolei-board"`，计算逻辑 `isTerminalState(state) ? "lost" : isWin(state) ? "won" : "playing"`
  - `onGameEnd(state, status)`：在现有 `buffer.gameEvent = {...}` + `buffer.gameState = state` 之后，push 终局条目 `{ tool: "(game-end)", state, status }`
  - 参考 [`data-model.md`](./data-model.md) §2 写入规则表与 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §2.1

- [X] T004 在 `projects/game/agent/src/team/team-sink.test.ts` 中新增 gameLog 相关测试用例：
  - `onGameStart` 清空并写入初始条目：先写入一条 onMove 旧数据，再调 onGameStart，断言 `buffer.gameLog.length === 1` 且 `gameLog[0].tool === "saolei_init"`
  - `onMove` 累积操作条目：调 onMove 两次，断言 `buffer.gameLog` 有 2 条条目，含正确 `tool`/`x`/`y`/`status`
  - `onGameEnd` 累积终局条目：调 onGameEnd，断言 `buffer.gameLog` 末尾条目 `tool === "(game-end)"` 且 `status` 为传入的 won/lost
  - `onGameStart` 清空跨局数据：模拟第一局（onGameStart → onMove → onGameEnd）后第二局 onGameStart，断言 gameLog 仅含第二局初始条目
  - 复用现有 `makeState()` helper。参考现有 `createTeamSink` describe block 的测试模式

**验证门禁**: `bazel test //projects/game/agent/src/team:lib_test` 通过；`gameLog` 字段正确累积日志

---

## Phase 3: User Story 1 — 游戏结束时立即停止 player loop 并触发 planner (Priority: P1) 🎯 MVP

**Goal**: 当游戏以 won/lost 结束时，player 的 createAgent 内部 loop 立即停止（不再继续），使后处理消费 gameEvent → 设置 gameEnded → 条件边路由到 planner。同时为 player 节点添加 config 传递（US4 的 player 部分），使内部 createAgent 继承外层 graph 的 recursionLimit。

**Independent Test**: 通过 fake-model + fake-tool 测试验证：构造"落子即输局"场景（sink 写入 lost 事件），验证 player 节点返回后 `gameEnded === "lost"` 且条件边路由到 planner；构造"输局后 LLM 尝试重开"场景，验证 player loop 停止不执行重开操作。

### 文档清单

- **代码规范文档**: `style/javascript.md`（§测试：DI over `vi.mock`）
- **官方文档**: [LangChain createAgent middleware — beforeModel hook](https://js.langchain.com/docs/how_to/agent_executor/middleware/)；[LangGraph RunnableConfig](https://v2.docs.langchain.com/oss/javascript/langgraph/configuration)
- **技术文章**: 无

---

- [X] T005 [US1] 在 `projects/game/agent/src/team/player.ts` 的 `createPlayerNode` 中，为 `createAgentFn({...})` 调用新增 `middleware` 参数，添加 `gameEndGuard` middleware。middleware 结构：`{ name: "gameEndGuard", beforeModel: { canJumpTo: ["end"], hook: () => { if (buffer.gameEvent && !buffer.gameEvent.consumed) return { jumpTo: "end" }; } } }`。参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §1.1 和 [`research.md`](./research.md) D1。同时新增 `import type { RunnableConfig } from "@langchain/core/runnables"` 以支持 US4 的 config 参数

- [X] T006 [US1] 在 `projects/game/agent/src/team/player.ts` 中，将节点函数签名从 `(state: TeamStateValue)` 改为 `(state: TeamStateValue, config?: RunnableConfig)`（US4 player 部分）。在内部 `playerAgent.invoke({ messages: input })` 调用中传入 config 作为第二参数：`playerAgent.invoke({ messages: input }, config)`。参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §3.1 和 [`research.md`](./research.md) D2

- [X] T007 [US1] 在 `projects/game/agent/src/team/player.ts` 中，将节点函数体内的 `invoke` 调用与后处理重构为 try/finally 结构，确保 `consumeGameEvent(buffer)` 在 invoke 正常返回或异常时都能执行。结构参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §1.4：try 块内执行 `invoke` 并处理 `result.messages`；finally 块内执行 `consumeGameEvent(buffer)` 并组装返回值 `{ playerMessages, ...(gameEvent ? { gameEnded: gameEvent.status } : {}) }`。注意：invoke 正常返回时也需在 finally 中 consume（因 middleware 停止 loop 是正常返回，consumeGameEvent 在正常和异常路径都执行）。**异常语义**：finally 中组装返回值会吞掉 invoke 抛出的所有异常（节点正常返回、`gameEnded` 被设置并路由到 planner）——这是有意设计（US1 acceptance #5：异常终止时游戏结束事件仍被正确消费；与 spec.md edge case "player 持续落子但游戏不结束" 的递归超限场景一致），实现时无需在 finally 中重新抛出

- [X] T008 [US1] 在 `projects/game/agent/src/team/graph.test.ts` 中新增测试用例覆盖 Issue 1：
  - **lost 场景 → planner 触发**：构造 fake player tool 在调用时 `sink.onGameEnd(makeState(), "lost")`；fake player model 先调用该 tool → 再尝试调用 `saolei_init`（重开）。验证 `gameEnded` 被清除（planner 清除）、`plannerMessages` 非空（planner 运行了）、且 `saolei_init` tool 未被调用（middleware 停止了 loop——可通过 tool call count 断言）。复用 `buildTestGraph` helper，参考现有 `buildGameEndingPlayerTool` 改为 lost
  - **invoke 异常 → 后处理执行**：构造一个会抛异常的 `createAgentFn`（DI spy），先通过 sink 写入一个 gameEvent，验证即使 invoke 抛异常，`gameEnded` 仍被设置（try/finally 保障）。参考 [`research.md`](./research.md) D5 测试策略
  - **won 场景仍触发 planner**：现有 `playOneGamePlayerModel` 测试已覆盖 won——但注意该测试依赖旧 loop 行为的断言需按下一条 bullet 更新（loop 行为变化），更新后验证 won 场景下 planner 仍被触发。参考现有 `"routes player→planner on game end"` 测试
  - **现有测试更新（loop 语义变化）**：middleware 停止 loop 后，依赖旧 loop 行为的现有断言必须同步更新（源码依据 `graph.test.ts`，已确认会失败）：
    - `playOneGamePlayerModel`（`graph.test.ts:68`）移除 "stop" 响应（middleware 在游戏结束事件未消费时跳过下一次 model 调用），响应队列变为 [move, idle]，注释同步更新
    - D6 测试 `"routes player→planner on game end"`（`graph.test.ts:129`）的 `playerModel.calls` 断言 `toHaveLength(3)` → `toHaveLength(2)`，注释同步更新
    - 多局游戏测试 `"plays two games in one turn"`（`graph.test.ts:276`）模型队列重构为 [move, move, idle]（每局 game end 后 loop 停止、planner 在局间运行），`playerModel.calls` 断言 `toHaveLength(5)` → `toHaveLength(3)`，注释同步更新（该测试的 reviewRequests 文本断言在 T012 中更新）

**验证门禁**: `bazel test //projects/game/agent/src/team:lib_test` 通过；lost 场景下 planner 被触发；invoke 异常时后处理仍执行

**Checkpoint**: User Story 1 完全功能化，player loop 在游戏结束时停止、planner 被触发。US4 的 player config 传递部分也已完成。

---

## Phase 4: User Story 2 — planner 复盘能看到完整游戏过程 (Priority: P2)

**Goal**: planner 的复盘输入从仅显示终局棋盘快照改为渲染完整 gameLog（每步操作的工具名、坐标、操作后棋盘状态）。同时为 planner 节点添加 config 传递（US4 的 planner 部分）。

**Independent Test**: 构造一个包含多次落子操作的 buffer，验证 planner 的复盘输入 HumanMessage 文本包含每步操作的工具名、坐标与棋盘文本渲染；验证空 gameLog 时复盘输入为说明性消息。

**依赖**: Phase 2（team-sink gameLog）MUST 完成；T017（Issue 4 测试）另依赖 T006/T011（config 传递实现）完成

### 文档清单

- **代码规范文档**: `style/javascript.md`（§测试：DI over `vi.mock`）
- **官方文档**: 无
- **技术文章**: 无

---

- [X] T009 [US2] 在 `projects/game/agent/src/team/planner.ts` 中，修改 `buildReviewInput` 函数签名从 `(gameState: GameState | null)` 改为 `(buffer: EphemeralGameBuffer)`，改为读取 `buffer.gameLog` 渲染完整游戏过程。渲染逻辑：gameLog 为空时返回 `new HumanMessage("请复盘本局游戏（无可用游戏记录）。")`；非空时按顺序拼接每条 `GameLogEntry` 的 `序号. tool(coord) → status` + `renderBoardText(entry.state)`，末尾追加复盘指令。需要 import `renderBoardText` from `"@dominion/game-saolei-board"`（已 import）和 `EphemeralGameBuffer` type。参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §2.2 和 [`data-model.md`](./data-model.md) §2 渲染示例

- [X] T010 [US2] 在 `projects/game/agent/src/team/planner.ts` 的 `createPlannerNode` 返回的节点函数中，将 `buildReviewInput(peekGameState(buffer))` 调用改为 `buildReviewInput(buffer)`（传递整个 buffer 而非仅 gameState）。移除不再需要的 `peekGameState` import（如果 planner.ts 中不再使用）。注意 `EphemeralGameBuffer` import 已存在

- [X] T011 [US2] 在 `projects/game/agent/src/team/planner.ts` 中，将节点函数签名从 `(state: TeamStateValue)` 改为 `(state: TeamStateValue, config?: RunnableConfig)`（US4 planner 部分）。在 `invokeWithRetry` 函数签名中新增 `config?: RunnableConfig` 参数，并在 `agent.invoke({ messages: input })` 调用中传入 config。节点函数调用 `invokeWithRetry(plannerAgent, input, config)`。新增 `import type { RunnableConfig } from "@langchain/core/runnables"`。参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §3.2

- [X] T012 [US2] 在 `projects/game/agent/src/team/graph.test.ts` 中新增测试用例覆盖 Issue 2：
  - **多步操作的复盘输入**：构造 fake player tool 先 `sink.onMove("saolei_click", 3, 4, state1)` → `sink.onMove("saolei_click", 5, 2, state2)` → `sink.onGameEnd(state2, "lost")`。验证 planner 的 `plannerMessages` 中包含复盘输入 HumanMessage，其文本内容含 `saolei_click(3, 4)`、`saolei_click(5, 2)`、`lost` 以及 `renderBoardText` 输出。可通过检查 `plannerMessages` 中 `_getType() === "human"` 的消息 content 断言
  - **空 gameLog 场景**：构造 buffer.gameLog 为空（不调用任何 sink 回调），触发 planner，验证复盘输入为 `"请复盘本局游戏（无可用游戏记录）。"`
  - **现有测试文本断言更新**：`buildReviewInput` 输出文本变更后（"本局已结束…" → "本局游戏过程：…请复盘本局游戏表现…"），更新依赖旧文本的现有断言（源码依据 `graph.test.ts`，已确认会失败）：
    - D5 partition 测试（`graph.test.ts:202/206`：playerTexts 不含 / plannerTexts 含 "本局已结束"）→ 改为新文本（如 "请复盘本局游戏表现"）
    - planner strategy 注入测试（`graph.test.ts:254`：firstCall 含 "本局已结束"）→ 改为新文本
    - 多局游戏测试 reviewRequests filter（`graph.test.ts:310`：含 "本局已结束"）→ 改为新文本
  - **说明（planner tab 可见性）**：US2 acceptance #5 / SC-004 的 ListMessages 可见性依赖现有基础设施（`contracts/team-graph-fix-contract.md` §2.3：human → MESSAGE_ROLE_USER），本 task 仅验证 `plannerMessages` 通道内容；端到端可见性由 T016 大型测试覆盖

- [X] T017 [US4] 在 `projects/game/agent/src/team/graph.test.ts` 中新增 Issue 4 测试用例：
  - **>25 步不触发 GraphRecursionError（player）**：构造持续调用 tool 但不触发游戏结束的 fake player tool（不调用 sink 回调）+ 连续 26+ 次 tool 调用响应的 fake model（如 `respondWithTools` 链），`graph.invoke` 以 `recursionLimit: 1000` 调用，断言不抛出 `GraphRecursionError`（内部 createAgent 继承外层 recursionLimit 而非默认 25；对应 US4 acceptance #1 / SC-006）。参考 [`research.md`](./research.md) D2
  - **planner 继承 recursionLimit**：以相同方式验证 planner 节点的 createAgent 继承外层 recursionLimit（对应 US4 acceptance #2，可与上一条合并为参数化场景）
  - **abort 信号传播**：构造 `AbortController`，`graph.invoke` 传入 `{ signal }`，通过 `buildTestGraph` 的 `createAgentFn` DI 参数捕获内部 createAgent 的 `invoke` 调用，断言收到的 config 携带该 signal（对应 US4 acceptance #3）
  - 参考 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) §3

**验证门禁**: `bazel test //projects/game/agent/src/team:lib_test` 通过；planner 复盘输入包含完整游戏过程

**Checkpoint**: User Stories 1、2 和 4 完全功能化。planner 复盘输入包含完整 gameLog；player 和 planner 的 createAgent 均继承外层 graph config。

---

## Phase 5: User Story 3 — 用户消息气泡右对齐 (Priority: P3)

**Goal**: desktop 对话页面中用户消息气泡靠右对齐，不被外层 `.msg-row` wrapper 的默认 `flex-start` 覆盖。

**Independent Test**: 检查 ChatView.svelte 中 ChatMessage 外层 wrapper 不再使用 `.msg-row` 类（不再设 `display: flex`），ChatMessage 内部的 `.msg-row.msg-user`（`justify-content: flex-end`）直接控制对齐。

### 文档清单

- **代码规范文档**: 无（前端 Svelte 组件，无 `style/` 下的专门 Svelte 规范）
- **官方文档**: [Svelte class: directive](https://svelte.dev/docs/svelte/class)
- **技术文章**: [MDN — justify-content](https://developer.mozilla.org/en-US/docs/Web/CSS/justify-content)

---

- [X] T013 [P] [US3] 在 `projects/game/desktop/frontend/src/components/ChatView.svelte` 中，将 `kind === 'text' || kind === 'thinking'` 分支（当前约 line 274）的外层 wrapper 从 `<div class="msg-row" class:msg-pending={item.pending}>` 改为 `<div class="msg-pending-wrapper" class:msg-pending={item.pending}>`。在 `<style>` 中新增 `.msg-pending-wrapper { padding: 2px 12px; }`（与原 `.msg-row` 的 padding 一致，不设 `display: flex`）。保留 `class:msg-pending` 引用（`.msg-pending` 的 `opacity: 0.65` 样式已存在）。参考 [`contracts/desktop-alignment-fix.md`](./contracts/desktop-alignment-fix.md) §2

**验证门禁**: desktop 前端编译通过（`bazel build //projects/game/desktop/frontend/...`）；用户消息气泡靠右对齐（ChatMessage 内部 `.msg-row.msg-user` 的 `flex-end` 生效）；`.msg-pending` 的 opacity 效果保留

**Checkpoint**: 所有四个 user story 均已完成。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 编译验证、Gazelle 更新、大型测试验收

### 文档清单

- **代码规范文档**: `style/large_test.md`（大型测试规范，T016 执行依据）
- **官方文档**: 无
- **技术文章**: 无

---

- [ ] T014 执行代码格式化与依赖检查：`bazel run //:go -- fmt`（如有 Go 变更——本特性无 Go 变更，可跳过）；对变更的 TS 文件确认格式正确。执行 `bazel run //:gazelle projects/game/agent/src/team` 和 `bazel run //:gazelle projects/game/desktop/frontend/src/components` 确认 BUILD.bazel 无需更新（无新增文件，但需确认 import 变更不引入新依赖——本特性 `RunnableConfig` 来自 `@langchain/core/runnables`，已在依赖中）

- [ ] T015 执行全量编译+单测验证（最终回归门禁，配合各变更 task 内已执行的编译+单测）：`bazel build //...` 和 `bazel test //projects/game/agent/src/team:lib_test`，确认所有编译和测试通过（含现有 031-spec 测试无回归）

- [ ] T016 大型测试验收（宪法原则 VI）：使用 testplan skill 执行端到端大型测试。测试计划位于 `projects/game/testplan/`（031-spec 已建立）。需确认覆盖以下场景，全部用例通过：
  - 游戏失败（lost）→ planner 被触发 → 策略被更新（Issue 1）
  - planner 复盘输入包含完整游戏过程（Issue 2）
  - 多局连续游戏不触发 GraphRecursionError（Issue 1 + Issue 4）
  - 如现有 testplan 未覆盖 lost 场景，MUST 扩展测试计划或新增测试用例
  
  参考执行方式：加载 `testplan` skill，按其指引执行 `guitar run <plan.yaml>`。验收标准：所有测试用例全部通过（failed/flaky = 验收未通过，MUST 修复后重新执行）。参考 `style/large_test.md`（大型测试规范）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖——确认编译基线
- **Phase 2 (Foundational)**: 无依赖——team-sink gameLog 数据层
- **Phase 3 (US1)**: 无依赖（player.ts 变更不依赖 team-sink gameLog）——可与 Phase 2 并行
- **Phase 4 (US2)**: **依赖 Phase 2**（planner 读取 gameLog，需 team-sink 先完成 gameLog 字段）
- **Phase 5 (US3)**: 无依赖——独立前端变更，可与 Phase 2/3/4 并行
- **Phase 6 (Polish)**: 依赖 Phase 2-5 全部完成

### User Story Dependencies

- **US1 (P1)**: 无前置依赖——player.ts middleware + try/finally + config 传递
- **US2 (P2)**: 依赖 Foundational（Phase 2 team-sink gameLog）——planner 读取 gameLog
- **US3 (P3)**: 无前置依赖——ChatView.svelte 独立变更
- **US4 (P2)**: 拆分融入 US1（player config，T006）和 US2（planner config，T011）——不单独成 phase，因与 US1/US2 修改相同文件

### 文件依赖矩阵（防冲突）

| 文件 | 涉及 Task | 冲突说明 |
|---|---|---|
| `team-sink.ts` | T002, T003 | Phase 2 内串行 |
| `team-sink.test.ts` | T004 | Phase 2 内，依赖 T002/T003 |
| `player.ts` | T005, T006, T007 | Phase 3 内串行（同文件） |
| `planner.ts` | T009, T010, T011 | Phase 4 内串行（同文件），依赖 Phase 2 完成 |
| `graph.test.ts` | T008, T012, T017 | Phase 3/4 分别新增测试，同 phase 内串行编辑 |
| `ChatView.svelte` | T013 | Phase 5 独立 |

### Parallel Opportunities

- **Phase 2（team-sink）与 Phase 3（player.ts）可并行**：不同文件，无依赖
- **Phase 5（ChatView.svelte）可与 Phase 2/3/4 并行**：完全独立的前端变更
- Phase 3 内 T005/T006/T007 串行（同一文件 `player.ts`）；T008 紧随其后（`graph.test.ts`）
- Phase 4 内 T009/T010/T011 串行（同一文件 `planner.ts`）；T012 → T017 串行（同一文件 `graph.test.ts`）

---

## Parallel Example: Phase 2 + Phase 3 + Phase 5

```bash
# These three can run in parallel (different files, no dependencies):
Task: "T002-T004: team-sink.ts gameLog data layer"
Task: "T005-T008: player.ts gameEndGuard middleware + config"
Task: "T013: ChatView.svelte alignment fix"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup（确认编译基线）
2. Complete Phase 3: User Story 1（player loop 停止 + planner 触发 + player config）
3. **STOP and VALIDATE**: 验证游戏失败时 planner 被触发
4. 此时 US4 的 player 部分也已完成

### Incremental Delivery

1. Setup → 确认基线
2. Phase 2（team-sink gameLog）→ 数据层就绪
3. Phase 3（US1: player gameEndGuard）→ 游戏结束时 planner 触发 ✅（MVP）
4. Phase 4（US2: planner gameLog rendering）→ planner 复盘完整 ✅
5. Phase 5（US3: ChatView alignment）→ 用户气泡右对齐 ✅
6. Phase 6（Polish）→ 大型测试验收 ✅

---

## Notes

- US4（config 传递）未单独成 phase，因其修改的文件（`player.ts`/`planner.ts`）与 US1/US2 重叠。US4 的 player 部分在 Phase 3（T006）完成，planner 部分在 Phase 4（T011）完成。
- 编译+单测属各代码变更 task 内（宪法原则 IV），不单列 task。
- `RunnableConfig` 来自 `@langchain/core/runnables`（已在 agent 包依赖中，无需新增依赖）。
- `isTerminalState` 已从 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:255` export。
- `isWin` 已从 `@dominion/game-saolei-board` export。
- `renderBoardText` 已从 `@dominion/game-saolei-board` import 于 planner.ts。
