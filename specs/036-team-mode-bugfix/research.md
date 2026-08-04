# Research: Team Template Mode 缺陷修复

**Feature**: `036-team-mode-bugfix` | **Date**: 2026-08-04 | **Spec**: [`spec.md`](./spec.md)

> Phase 0 调研输出。四个缺陷的根因、修复技术方案与验证依据。Issue 1-3 的根因分析见 `specs/031-team-template-mode/bug-analysis.md`；Issue 4 为 plan 阶段新增分析。本文件补充 bug-analysis.md 未覆盖的技术验证细节。

---

## D1 — Issue 1: `beforeModel` middleware 停止 createAgent loop（源码级确认）

### 背景

bug-analysis.md 提出"用 `beforeModel` middleware 在游戏结束时暂停 createAgent loop"的修复方向。本节对该方案进行源码级验证。

### 源码验证（`langchain@1.5.4` / `@langchain/langgraph@1.4.8`）

**1. middleware hook 面确认**（`langchain/dist/agents/middleware/types.d.ts:254-419`）：

| Hook | 类型 | 行号 |
|---|---|---|
| `beforeAgent` | `BeforeAgentHook` | 391 |
| `beforeModel` | `BeforeModelHook` | 400 |
| `wrapModelCall` | `WrapModelCallHook` | 382 |
| `afterModel` | `AfterModelHook` | 409 |
| `wrapToolCall` | `WrapToolCallHook` | 353 |
| `afterAgent` | `AfterAgentHook` | 418 |

**2. `beforeModel` + `canJumpTo` 类型签名**（`types.d.ts:182-185`）：

```ts
type BeforeModelHook<TSchema, TContext> =
  | BeforeModelHandler<TSchema, TContext>
  | { hook: BeforeModelHandler<TSchema, TContext>; canJumpTo?: JumpToTarget[] };
```

`JumpToTarget`（`constants.d.ts:5-6`）：`readonly ["model", "tools", "end"]`。

`MiddlewareResult<TState>`（`types.d.ts:69-71`）：`(TState & { jumpTo?: JumpToTarget }) | void`。

**3. `canJumpTo` 校验逻辑**（`nodes/middleware.js:79-92`）：node 读取 `getHookConstraint(this.middleware.beforeModel)` 获取 `canJumpTo` 数组；返回的 `jumpTo` 不在该数组中则抛 `Invalid jump target`。设置 `canJumpTo: ["end"]` 即白名单允许返回 `{ jumpTo: "end" }`。

**4. `jumpTo: "end"` 的路由效果**（`ReactAgent.js:537-554`，`#createBeforeModelRouter`）：

```js
return (state) => {
  if (!builtInState.jumpTo) return nextDefault;
  const destination = parseJumpToTarget(builtInState.jumpTo);
  if (destination === END) return END;   // ← routes to END, clean stop
};
```

路由到 `END` 是 LangGraph 的**正常成功终止路径**。`invoke()` 返回最终 state，**不抛异常**。这与递归超限（抛 `GraphRecursionError`）不同。

**5. 官方范本**——随包发布的 `middleware/modelCallLimit.js:112-132`：

```js
beforeModel: {
  canJumpTo: ["end"],
  hook: (state, runtime) => {
    if (exitBehavior === "end") return {
      jumpTo: "end",
      messages: [new AIMessage(error.message)]
    };
  }
}
```

`toolCallLimit.js:220,339` 展示了同样的 `canJumpTo: ["end"]` 模式（在 `afterModel` 上）。

### Decision

采用 bug-analysis.md 提出的方案：player 的 createAgent 添加 `beforeModel` middleware（`canJumpTo: ["end"]`），当 buffer 中存在未消费的游戏结束事件时返回 `{ jumpTo: "end" }` 停止 loop。

### 时序确认

```
迭代 N:
  beforeModel  → buffer 无未消费事件 → 继续
  model        → LLM 决定调用 saolei_click(3,4)
  tools        → MCP handler 执行 → sink.onMove(同步写 buffer.gameState)
                 → gameStatus = "lost" → sink.onGameEnd(同步写 buffer.gameEvent)
                 → return ToolMessage
迭代 N+1:
  beforeModel  → 检查 buffer: gameEvent 存在且 !consumed
                 → return { jumpTo: "end" }   ← 停止 loop
  (invoke 正常返回)
```

sink 回调在 MCP handler 内是 `await` 的（`saolei-mcp.ts:790-793`），team sink 的 `onGameEnd` 是纯同步赋值（`team-sink.ts:79-87`）。执行链严格顺序：**sink 写 buffer → handler 返回 → ToolNode 完成 → 循环 → beforeModel 检查 buffer**。不存在竞态。

### 暂停-恢复语义

- `invoke()` 正常返回（不抛异常），`result.messages` 包含含 game-over tool result 的完整消息
- player 节点写回 `playerMessages` → `consumeGameEvent` → `gameEnded` 设置 → 条件边路由到 planner
- planner 返回后，player 恢复：从 `playerMessages`（含 game-over tool result）重建 input
- createAgent 第一次迭代 `beforeModel`：`gameEvent.consumed = true` → 不跳转 → model 正常调用

### 注意事项

- `beforeModel` 跳转到 END 时**跳过** `afterModel`/`afterAgent` 钩子（`ReactAgent.js:542`），但不影响 invoke 的正常返回。
- 防御性保障：player 节点添加 try/finally，确保 `consumeGameEvent` 即使在 invoke 异常时也能执行（FR-002）。

---

## D2 — Issue 4: config 传递机制（源码级确认）

### 背景

player 节点（`player.ts:152`）与 planner 节点（`planner.ts:137`）调用 `createAgent.invoke({ messages })` 时不传 config。内部 createAgent 使用默认 `recursionLimit: 25`，不继承外层 graph 的 1000。

### 源码验证

**1. 默认 recursionLimit**（`@langchain/langgraph/dist/pregel/utils/config.js:36`）：

```js
const DEFAULT_RECURSION_LIMIT = 25;
```

在 `ensureLangGraphConfig`（`config.js:134-141`）中作为默认值应用。

**2. createAgent.invoke 接受 config**（`ReactAgent.js:613-617`）：

```js
async invoke(state, config) {
  const mergedConfig = mergeConfigs(this.#defaultConfig, config);
  const initializedState = await this.#initializeMiddlewareStates(state, mergedConfig);
  return this.#graph.invoke(initializedState, mergedConfig);
}
```

config 被合并后直接传给内部 compiled graph 的 `invoke()`。`recursionLimit` 是顶层 config key（不在 `configurable` 下），在 `CONFIG_KEYS`（`config.js`）和 `GRAPH_DEFAULT_CONFIG_KEYS`（`agents/utils.js:397-404`）列表中。

**3. LangGraph 节点函数签名**：`(state, config?)`。当 `.addNode("player", playerNode)` 注册时，LangGraph 执行该节点时传入 `(state, config)`，其中 config 包含外层 graph 的 `recursionLimit`、`configurable`、`signal` 等。

### Decision

- player 节点函数改为 `(state: TeamStateValue, config?: RunnableConfig): Promise<Partial<TeamStateValue>>`
- planner 节点函数改为 `(state: TeamStateValue, config?: RunnableConfig): Promise<Partial<TeamStateValue>>`
- 内部 `invoke()` 调用传入 config：`playerAgent.invoke({ messages: input }, config)`
- planner 的 `invokeWithRetry` 也接受并传递 config

### 注意事项

- config 传递是**附加性**的，不改变现有 invoke 的消息处理逻辑。
- 现有测试中 graph.invoke 传递 `recursionLimit: 50`，内部 createAgent 将继承 50 而非默认 25。但现有测试的 fake-model 步数远小于 50，不受影响。
- `signal`（AbortSignal）也随 config 传递，使内部 createAgent 能响应 abort。

---

## D3 — Issue 2: GameLog 数据结构与 planner 复盘输入

### 背景

ephemeral buffer 只存最新 `gameState` 快照。planner 仅从 `peekGameState(buffer)` 获取终局棋盘。需扩展 buffer 累积完整游戏日志。

### Decision

扩展 `EphemeralGameBuffer`，新增 `gameLog: GameLogEntry[]`。sink 回调累积日志条目：

```ts
export interface GameLogEntry {
  tool: string;           // "saolei_init" | "saolei_click" | ...
  x?: number;
  y?: number;
  state: GameState;       // 操作后的棋盘状态
  status: "won" | "lost" | "playing";
}

export interface EphemeralGameBuffer {
  gameState: GameState | null;
  gameEvent: GameEventRecord | null;
  gameLog: GameLogEntry[];  // 新增
}
```

sink 回调变更：
- `onGameStart`：**清空** `gameLog`（新局重置），push 初始条目 `{ tool: "saolei_init", state, status: "playing" }`
- `onMove`：push 操作条目 `{ tool, x, y, state, status: gameStatus(state) }`
- `onGameEnd`：push 终局条目 `{ tool: "(game-end)", state, status }`（status = won/lost）

planner `buildReviewInput` 改为渲染完整 `gameLog`（而非仅 `gameState` 快照）。

### 多局日志重置

`onGameStart` 清空 `gameLog` 确保每局独立。这是关键：bug-analysis.md 指出 planner 应复盘"本局"而非跨局累积。

### planner tab 可见性

复盘输入 `HumanMessage` 已被写入 `plannerMessages` 通道（planner 节点返回值 `result.messages` 包含它，且不被 strategy-id 过滤）。ListMessages 将 `human` → `MESSAGE_ROLE_USER` 并提取 text 内容，因此在 planner tab 中**已经可见**（历史加载时）。

### Alternatives considered

- **在 TeamState 中新增 gameLog 通道**：过度设计——gameLog 是 ephemeral 的（每局重置），不需要 checkpoint 持久化。保持在 ephemeral buffer 中。
- **仅渲染 gameLog 的最后一步（终局）**：即当前行为，bug-analysis.md 确认为不满足需求。

---

## D4 — Issue 3: ChatView CSS 对齐修复

### 背景

`ChatView.svelte:269-276` 对用户文本外包了一层 `.msg-row`（无 `msg-user` 对齐类），ChatMessage 内部又渲染自己的 `.msg-row.msg-user`。外层 flex 容器 `justify-content` 默认 `flex-start`，内层 div 只占内容宽度，`flex-end` 无效。

### Decision

移除 ChatView 中 ChatMessage 外层的 `.msg-row` 类（改为不带 flex 布局的 wrapper），让 ChatMessage 自身的 `.msg-row.msg-user` 成为 `.chat-thread` 的直接 flex 子项。

```svelte
<!-- 改前 (ChatView.svelte:274) -->
<div class="msg-row" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>

<!-- 改后 -->
<div class="msg-pending-wrapper" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>
```

`.msg-pending-wrapper` 仅保留 `opacity` 样式（从 `.msg-pending` 继承），不设置 `display: flex`，让 ChatMessage 的根 `.msg-row` 直接控制对齐。

### 注意事项

- `.msg-pending` 的 `opacity: 0.65` 样式定义在 ChatView 的 `<style>` 中，新 wrapper 需引用它。
- ChatMessage 的 `.msg-row` 在 ChatMessage 自身的 `<style>` 中定义（scoped），ChatView 的 `.msg-pending-wrapper` 不影响其内部布局。
- 仅影响 `kind === 'text' || kind === 'thinking'` 分支（ChatMessage 渲染路径）；agent markdown 文本、image、tool 有各自独立的 `.msg-row` 渲染，不受影响。

---

## D5 — 测试策略

### 现有测试基础设施

031-spec 的测试使用 fake-model + fake-tool DI 模式（`graph.test.ts`）。现有 fake player tool 在每次调用时触发 `sink.onGameEnd("won")`。现有 fake-model 用 `fakeModel().respondWithTools(...).respond(...)` 链式构造。

### 需新增的测试用例

**Issue 1**：
- "输局后 LLM 尝试重开"场景：fake-model 先调用 tool（触发 lost）→ 再尝试调用 `saolei_init` → 验证 `saolei_init` 未执行（middleware 在游戏结束时停止了 loop）。
- "invoke 异常时后处理仍执行"场景：构造一个会抛异常的 createAgentFn，验证 try/finally 确保 `consumeGameEvent` 仍被调用。

**Issue 2**：
- 多步操作场景：fake tool 先 `onMove`（playing）→ `onMove`（playing）→ `onGameEnd`（lost）。验证 planner 的复盘输入包含所有步骤。
- 空日志场景：planner 被触发但 buffer.gameLog 为空。验证复盘输入为说明性消息。

**Issue 4**：
- 验证节点函数接受 config 并传递给 createAgent.invoke。可通过 createAgentFn DI spy 断言传入的 config 包含 recursionLimit。

### 大型测试

031-spec 的 testplan（`testplan/`）已有端到端测试。需新增覆盖"游戏失败→planner 触发"场景的用例（现有用例覆盖 won 场景）。

---

## 参考索引

| 关注点 | 来源 |
|---|---|
| bug-analysis（Issue 1-3） | `specs/031-team-template-mode/bug-analysis.md` |
| spike middleware 确认 | `experimental/ts/team_graph_spike/FINDINGS.md` A4 |
| LangChain middleware types | `langchain/dist/agents/middleware/types.d.ts` |
| modelCallLimit 范本 | `langchain/dist/agents/middleware/modelCallLimit.js` |
| ReactAgent invoke | `langchain/dist/agents/ReactAgent.js` |
| 默认 recursionLimit | `@langchain/langgraph/dist/pregel/utils/config.js` |
| player 节点 | `projects/game/agent/src/team/player.ts` |
| planner 节点 | `projects/game/agent/src/team/planner.ts` |
| team sink（buffer） | `projects/game/agent/src/team/team-sink.ts` |
| team graph | `projects/game/agent/src/team/graph.ts` |
| MCP sink 接线 | `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:780-797` |
| session-team turn runner | `projects/game/agent/src/session-team.ts:245-355` |
| ChatView 渲染 | `projects/game/desktop/frontend/src/components/ChatView.svelte:269-276` |
| ChatMessage 组件 | `projects/game/desktop/frontend/src/components/ChatMessage.svelte:96-103` |
