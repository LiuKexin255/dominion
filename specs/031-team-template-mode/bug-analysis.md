# Bug Analysis: Team Template Mode 实现问题

**Date**: 2026-08-04 | **Branch**: `031-team-template-mode`

本文档分析用户报告的三个问题，定位根因并给出修复方向。

---

## Issue 1: player 游戏失败时不触发 planner

### 设计意图（用户澄清）

游戏结束（won/lost）时应**立即**触发 planner，而不是等 createAgent 跑完整个 loop 后才检查。游戏失败→触发 planner→planner 返回后→继续 player 下一步。当前实现违背了这一意图。

### 根因

**player 节点的 `createAgent` 内部 loop 在游戏结束后不会自动停止。** loop 跑到 LLM 自行决定停为止，可能重开多局耗尽递归上限，后处理 `consumeGameEvent` 不可达。

#### 详细链路

1. **内部 createAgent 默认 recursionLimit = 25**（LangGraph 源码 `Pregel.stream()` 注释确认）。`player.ts:152` 调用 `.invoke()` 未传 config，不继承外层 graph 的 1000。

2. **游戏结束后 createAgent 不停止**：player 的 system prompt 说"每局结束后你可以自行决定是否开新局"。输局后 LLM 调用 `saolei_init` 重开新局 → 多局累积步数超过 25 → `GraphRecursionError`。

3. **后处理不可达**（`player.ts:152-166`，无 try/catch）：

   ```ts
   const result = (await playerAgent.invoke({ messages: input })) as { ... };
   const gameEvent = consumeGameEvent(buffer);  // ← 递归超限时不可达
   return { playerMessages: ..., ...(gameEvent ? { gameEnded: ... } : {}) };
   ```

   `gameEnded` 不被设置 → 条件边返回 END 而非 planner。

4. **赢局不受影响**：赢局后 LLM 通常停止（任务完成），单局步数在 25 步限制内。

5. **"2 步就失败"的解释**：那 2 步只是 createAgent 内多次重开中的某一局。createAgent 不会在输局后自动停止——它继续重开新局。

### 修复方向：用 `beforeModel` middleware 在游戏结束时暂停 createAgent loop

**核心思路**：在 createAgent 内部 loop 中，每次 model 调用前检查 buffer，若游戏已结束则跳过 model 调用并停止 loop，使控制权返回外层 graph 节点。

**LangChain middleware 能力确认**（源码 `ReactAgent.js` + `nodes/middleware.cjs` + `toolCallLimit.js`）：

- createAgent 内部 loop 结构：`beforeModel → model → afterModel → tools → 循环回到 beforeModel`
- `beforeAgent`/`afterAgent`：整个 agent 执行的首尾各一次（cleanup 用途）
- `beforeModel`/`afterModel`：**每次** model 调用的前后
- `beforeModel` 可声明 `canJumpTo: ["end"]` 并返回 `{ jumpTo: "end" }` 跳过 model 调用、停止 loop
- `toolCallLimitMiddleware` 证实此模式（它用 `afterModel` 拦截，`afterAgent` 做清理）

**时序追踪**（无异步问题）：

```
迭代 N:
  beforeModel  → buffer 无未消费事件 → 继续
  model        → LLM 决定调用 saolei_click(3,4)
  tools        → MCP handler 执行:
                   bridge.dispatch → 截图 → recognize → GameState
                   await runSink("onMove")    → buffer.gameState = state  (同步)
                   gameStatus = "lost"
                   await runSink("onGameEnd") → buffer.gameEvent = {…}    (同步)
                   return ToolMessage("…game status: lost…")
                 ToolNode 完成

迭代 N+1:
  beforeModel  → 检查 buffer: gameEvent 存在且 !consumed
                 → return { jumpTo: "end" }   ← 暂停：跳过 model 调用
  (loop 结束，invoke 正常返回)
```

sink 回调在 MCP handler 内是 `await` 的（`saolei-mcp.ts:790-793`），team sink 的 `onGameEnd` 是纯同步赋值（`team-sink.ts:79-87`）。执行链严格顺序：**sink 写 buffer → handler 返回 → ToolNode 完成 → 循环 → beforeModel 检查 buffer**。不存在竞态。

**暂停-恢复语义**：
- `invoke()` 正常返回（不抛异常），`result.messages` 包含含 game-over tool result 的完整消息
- player 节点写回 `playerMessages` → `consumeGameEvent` → `gameEnded` 设置 → 条件边路由到 planner
- planner 返回后，player 恢复：从 `playerMessages`（含 game-over tool result）重建 input，`strategyStore.get()` 读取新策略
- createAgent 第一次迭代 `beforeModel`：`gameEvent.consumed = true` → 不跳转 → model 正常调用
- LLM 直接看到 game-over tool result，**中间无插入任何 message**，仅 strategy SystemMessage 透明更新

**实现方案**：

```ts
// player.ts — 为 createAgent 添加 gameEndGuard middleware
const playerAgent = createAgentFn({
  model: deps.model,
  tools: deps.tools,
  systemPrompt,
  middleware: [{
    name: "gameEndGuard",
    beforeModel: {
      canJumpTo: ["end"],
      hook: () => {
        // 上一次迭代的 tools 已执行完毕（sink 同步写入了 buffer）
        // 如果游戏已结束且未被消费，跳过 model 调用，停止 loop
        if (buffer.gameEvent && !buffer.gameEvent.consumed) {
          return { jumpTo: "end" };
        }
      },
    },
  }],
});
```

**额外保障**（防御性）：player 节点添加 try/finally，确保 `consumeGameEvent` 即使在 invoke 异常时也能执行

---

## Issue 2: planner 看不到完整的游戏过程

### 设计意图（用户澄清）

planner 应该看到**完整的游戏过程**——player 的每一步操作以及操作后的游戏状态——而不是仅终局棋盘快照。这些内容应该在 planner tab 中可见。

### 根因

**ephemeral buffer 只存最新 `gameState` 快照**（`team-sink.ts:79-87`），不记录历史操作序列。planner 的复盘输入仅从 `peekGameState(buffer)` 获取终局棋盘文本（`planner.ts:184`），缺失游戏过程。

### 修复方向：扩展 buffer 累积完整游戏日志

#### 1. 扩展 ephemeral buffer 记录游戏日志

```ts
// team-sink.ts — 新增游戏日志条目类型
export interface GameLogEntry {
  tool: string;           // "saolei_init" | "saolei_click" | ...
  x?: number;
  y?: number;
  state: GameState;       // 操作后的棋盘状态
  status: "won" | "lost" | "playing";  // 操作后的游戏状态
}

export interface EphemeralGameBuffer {
  gameState: GameState | null;
  gameEvent: GameEventRecord | null;
  gameLog: GameLogEntry[];  // 新增：完整操作序列
}
```

#### 2. sink 回调累积日志

- `onGameStart`：清空 `gameLog`，push 初始条目 `{ tool: "saolei_init", state, status: "playing" }`
- `onMove`：push 操作条目 `{ tool, x, y, state, status: gameStatus(state) }`
- `onGameEnd`：push 终局条目（带 won/lost status）

#### 3. planner 复盘输入渲染完整游戏过程

```ts
// planner.ts — buildReviewInput 改为渲染完整 gameLog
function buildReviewInput(buffer: EphemeralGameBuffer): BaseMessage {
  const log = buffer.gameLog;
  if (log.length === 0) {
    return new HumanMessage("请复盘本局游戏（无可用游戏记录）。");
  }
  const lines: string[] = ["本局游戏过程："];
  for (let i = 0; i < log.length; i++) {
    const entry = log[i];
    const coord = entry.x != null ? `(${entry.x}, ${entry.y})` : "";
    lines.push(`${i + 1}. ${entry.tool}${coord} → ${entry.status}`);
    lines.push(renderBoardText(entry.state));
    lines.push("");
  }
  lines.push("请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy。");
  return new HumanMessage(lines.join("\n"));
}
```

#### 4. planner tab 可见性

复盘输入 `HumanMessage` 已经被写入 `plannerMessages` 通道（planner 节点的返回值 `result.messages` 包含它，且它不被 strategy-id 过滤）。`ListMessages` 将 `human` → `MESSAGE_ROLE_USER` 并提取 text 内容，因此在 planner tab 中**已经可见**。

**注意**：实时流（`streamEvents`）只产生 AI/tool 事件，不产生 input HumanMessage 事件。复盘输入在**历史加载**（ListMessages）时可见，但不在**实时流**中推送。如果需要实时可见，可以在 planner 节点开始时通过 `config.writer` 发射一个自定义事件。

---

## Issue 3: 对话页面用户气泡未右对齐

### 现象

用户消息气泡没有靠右对齐，而是出现在左侧。

### 根因

`ChatView.svelte:269-276` 对用户文本外包了一层 `.msg-row`（无 `msg-user` 对齐类），ChatMessage 内部又渲染了自己的 `.msg-row.msg-user`。外层 flex 容器 `justify-content` 默认 `flex-start`，内层 div 只占内容宽度，`flex-end` 无效。

### 修复方向

移除 ChatView 中 ChatMessage 外层的 `.msg-row` 类（改为不带 flex 布局的 wrapper），让 ChatMessage 自身的 `.msg-row.msg-user` 成为 `.chat-thread` 的直接 flex 子项。

```svelte
<!-- 改前 -->
<div class="msg-row" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>

<!-- 改后 -->
<div class:msg-pending-wrapper" class:msg-pending={item.pending}>
  <ChatMessage part={item.part} role={item.role} timestamp={item.timestamp} />
</div>
```

`.msg-pending-wrapper` 仅保留 `opacity` 样式（从 `.msg-pending` 继承），不设置 `display: flex`，让 ChatMessage 的根 `.msg-row` 直接控制对齐。

---

## 修复优先级

| 问题 | 严重度 | 核心改动 |
|------|--------|----------|
| Issue 1 | **高** | player createAgent 添加 `afterAgent` middleware，游戏结束时 `jumpTo: "end"` |
| Issue 2 | **中** | buffer 扩展 gameLog；planner buildReviewInput 渲染完整日志 |
| Issue 3 | **低** | ChatView 移除外层 `.msg-row` wrapper |

## 参考代码索引

| 关注点 | 文件路径 |
|--------|----------|
| player 节点 | `projects/game/agent/src/team/player.ts` |
| planner 节点 | `projects/game/agent/src/team/planner.ts` |
| team graph（条件边） | `projects/game/agent/src/team/graph.ts:172-176` |
| team sink（buffer） | `projects/game/agent/src/team/team-sink.ts` |
| MCP sink 接线 | `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:780-797` |
| middleware 能力 | LangChain `langchain/dist/agents/middleware/toolCallLimit.js`（`jumpTo: "end"` 范本） |
| spike middleware 确认 | `experimental/ts/team_graph_spike/FINDINGS.md` A4 |
| ChatView 渲染 | `projects/game/desktop/frontend/src/components/ChatView.svelte:269-276` |
| ChatMessage 组件 | `projects/game/desktop/frontend/src/components/ChatMessage.svelte:96-103` |
