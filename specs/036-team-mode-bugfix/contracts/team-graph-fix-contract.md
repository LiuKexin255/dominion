# Contract: Team Graph 缺陷修复

**Feature**: `036-team-mode-bugfix` | **Spec**: [`spec.md`](../spec.md) | **Research**: D1-D5

> 本契约为 `specs/031-team-template-mode/contracts/team-graph-contract.md` 与 `contracts/saolei-sink-contract.md` 的 **amendment**（修订补丁），描述 Issue 1/2/4 修复后的行为变更。Issue 3（前端 CSS）无后端契约，见 [`desktop-alignment-fix.md`](./desktop-alignment-fix.md)。

---

## 1. Issue 1: player createAgent gameEndGuard middleware

### 1.1 middleware 定义

player 的 createAgent 新增一个 `beforeModel` middleware，在每次 model 调用前检查 ephemeral buffer 中是否存在未消费的游戏结束事件：

```ts
// player.ts
const playerAgent = createAgentFn({
  model: deps.model,
  tools: deps.tools,
  systemPrompt,
  middleware: [{
    name: "gameEndGuard",
    beforeModel: {
      canJumpTo: ["end"],
      hook: () => {
        if (buffer.gameEvent && !buffer.gameEvent.consumed) {
          return { jumpTo: "end" };
        }
      },
    },
  }],
});
```

### 1.2 行为约束

- 当 `buffer.gameEvent` 存在且 `!consumed` 时，`beforeModel` 返回 `{ jumpTo: "end" }`，跳过 model 调用、停止 createAgent 内部 loop。
- `invoke()` 正常返回（不抛异常），`result.messages` 包含含 game-over tool result 的完整消息。
- `canJumpTo: ["end"]` 是必须的——未声明时返回 `jumpTo` 会抛 `Invalid jump target`（源码 `nodes/middleware.js:79-92`）。
- 已消费的 `gameEvent`（`consumed = true`）不触发跳转 → model 正常调用（恢复后第一次迭代）。

### 1.3 时序保证

sink 回调在 MCP handler 内是 `await` 的（`saolei-mcp.ts:790-793`），team sink 回调是同步赋值。执行链严格顺序：

```
tools 执行 → sink.onMove(同步) → sink.onGameEnd(同步) → ToolMessage 返回
→ ToolNode 完成 → 循环回到 beforeModel → 检查 buffer → 跳转 or 继续
```

无竞态。

### 1.4 后处理防御（try/finally）

player 节点添加 try/finally 保障，确保 `consumeGameEvent` 即使在 invoke 异常时也能执行：

```ts
try {
  const result = (await playerAgent.invoke({ messages: input }, config)) as { ... };
  // ... 正常处理
} finally {
  const gameEvent = consumeGameEvent(buffer);
  // ... 设置 gameEnded
}
```

**异常语义**：finally 中组装返回值意味着 invoke 抛出的异常（含 `GraphRecursionError` 与模型/工具错误）均被吞掉——节点正常返回、`gameEnded` 被设置并路由到 planner（US1 acceptance #5：异常终止时游戏结束事件仍被正确消费、planner 仍被触发）。

---

## 2. Issue 2: GameLog 与 planner 复盘输入

### 2.1 EphemeralGameBuffer 扩展

见 [`data-model.md`](../data-model.md) §1-2。新增 `gameLog: GameLogEntry[]`，sink 回调累积日志（`onGameStart` 清空并 push 初始条目，`onMove` push 操作条目，`onGameEnd` push 终局条目）。

### 2.2 planner buildReviewInput 改为渲染完整 gameLog

```ts
// planner.ts — buildReviewInput 从 peekGameState(buffer) 改为读取 buffer.gameLog
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

### 2.3 planner tab 可见性

复盘输入 `HumanMessage` 写入 `plannerMessages` 通道（planner 节点返回值 `result.messages` 包含它，不被 strategy-id 过滤）。ListMessages 将 `human` → `MESSAGE_ROLE_USER` 并提取 text 内容，在 planner tab 中**已可见**（历史加载时）。

---

## 3. Issue 4: 节点函数 config 传递

### 3.1 player 节点签名变更

```ts
// 改前
return async (state: TeamStateValue): Promise<Partial<TeamStateValue>> => { ... };

// 改后
return async (
  state: TeamStateValue,
  config?: RunnableConfig,
): Promise<Partial<TeamStateValue>> => {
  // ...
  const result = (await playerAgent.invoke(
    { messages: input },
    config,  // ← 传递外层 graph 的 config（含 recursionLimit、signal）
  )) as { messages: BaseMessage[] };
  // ...
};
```

### 3.2 planner 节点签名变更

```ts
// 改前
return async (state: TeamStateValue): Promise<Partial<TeamStateValue>> => { ... };

// 改后
return async (
  state: TeamStateValue,
  config?: RunnableConfig,
): Promise<Partial<TeamStateValue>> => {
  // ...
  result = await invokeWithRetry(plannerAgent, input, config);
  // ...
};
```

`invokeWithRetry` 也接受并传递 config：`agent.invoke({ messages: input }, config)`。

### 3.3 效果

- 内部 createAgent 继承外层 graph 的 `recursionLimit`（当前为 `RECURSION_LIMIT = 1000`，`llm.ts:32`），不再使用默认 25。
- `AbortSignal` 随 config 传递，使内部 createAgent 能响应 abort。

---

## 4. 变更影响范围

| 文件 | 变更内容 | 关联 Issue |
|---|---|---|
| `projects/game/agent/src/team/player.ts` | 新增 gameEndGuard middleware；节点函数接受 config；try/finally 后处理；invoke 传 config | 1, 4 |
| `projects/game/agent/src/team/planner.ts` | buildReviewInput 渲染 gameLog；节点函数接受 config；invokeWithRetry 传 config | 2, 4 |
| `projects/game/agent/src/team/team-sink.ts` | EphemeralGameBuffer 新增 gameLog；GameLogEntry 类型；sink 回调累积日志 | 2 |
| `projects/game/agent/src/team/graph.test.ts` | 新增测试用例 | 1, 2, 4 |
| `projects/game/agent/src/team/team-sink.test.ts` | 新增 gameLog 相关测试 | 2 |
| `projects/game/desktop/frontend/src/components/ChatView.svelte` | 外层 .msg-row wrapper 改为 .msg-pending-wrapper | 3 |

无 proto / API / RPC 变更。无新增依赖。
