# Contract: 压缩节点与上下文治理

**Feature**: `037-saolei-team-optimize` | **Spec**: [`../spec.md`](../spec.md) | **Data Model**: [`../data-model.md`](../data-model.md)

> team graph 新增压缩节点、gameCounter 状态字段与条件路由。压缩 = 各通道全量消息 → LLM 生成 → 替换为一条摘要 AIMessage。压缩后 player 停下（路由 END）。

---

## 1. State 字段扩展

**文件**: `projects/game/agent/src/team/state.ts` + `projects/game/agent/src/team/graph.ts`

```ts
// state.ts — TeamStateValue 扩展
export interface TeamStateValue {
  playerMessages: BaseMessage[];
  plannerMessages: BaseMessage[];
  gameEnded: GameEnded;
  gameCounter: number;  // NEW
}

// graph.ts — TeamState schema 扩展（module-private Annotation.Root）
const TeamState = Annotation.Root({
  playerMessages: Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] }),
  plannerMessages: Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] }),
  gameEnded: Annotation<GameEnded>({ reducer: (_p, n) => n, default: () => null }),
  gameCounter: Annotation<number>({ reducer: (_p: number, n: number) => n, default: () => 0 }),  // NEW
});
```

- `gameCounter` reducer = last-write-wins（与 `gameEnded` 一致）。
- 初始值 `0`；planner 节点返回时递增。

---

## 2. Graph 结构变更

**文件**: `projects/game/agent/src/team/graph.ts`

### 当前结构

```text
START → player ──条件(gameEnded≠null)──→ planner ──→ player
              └──(gameEnded=null)──→ END
```

### 变更后结构

```text
START → player ──条件(gameEnded≠null)──→ planner ──条件(gameCounter%5===0)──→ compress ──→ END
              └──(gameEnded=null)──→ END          └──(else)──→ player
```

### 路由函数

```ts
// planner 返回后的条件边
function routeAfterPlanner(state: TeamStateValue): "compress" | "player" {
  return state.gameCounter > 0 && state.gameCounter % 5 === 0 ? "compress" : "player";
}
```

### compile 变更

```ts
const graph = new StateGraph(TeamState)
  .addNode("player", playerNode)
  .addNode("planner", plannerNode)
  .addNode("compress", compressNode)               // NEW
  .addEdge(START, "player")
  .addConditionalEdges("player", routeAfterPlayer)
  .addConditionalEdges("planner", routeAfterPlanner) // CHANGED: planner → compress | player
  .addEdge("compress", END)                          // NEW: compress → END（player 停下）
  .compile({ checkpointer });
```

---

## 3. 压缩节点（Compress Node）

**文件**: `projects/game/agent/src/team/compress.ts`（新增文件）

### 节点签名

```ts
export function createCompressNode(
  deps: CompressNodeDeps,
): (state: TeamStateValue, config?: RunnableConfig) => Promise<Partial<TeamStateValue>>;

interface CompressNodeDeps {
  playerModel: ChatModel;
  plannerModel: ChatModel;
}
// 摘要帧经 config?.configurable?.emitChannelFrame 发射（tasks.md 决策 #1，
// 不通过 deps 注入；ChannelFrameEmitter 类型从 session-team.ts 导出）
```

### 节点行为

1. 对 `playerMessages` 通道：
   - 若通道为空（长度 0）→ 空操作（FR-015），跳过。
   - 否则：将通道消息序列化为文本，用 `playerModel.invoke()` 生成摘要 AIMessage。
   - 摘要 AIMessage 的 `id` = `randomUUID()`（同时作为 frameId）。
2. 对 `plannerMessages` 通道：同上，用 `plannerModel`。
3. 若任一摘要生成失败（LLM 调用抛错）→ **re-throw**（不捕获、不降级）。异常传播到 TurnLoop → abort 连接（FR-013）。
4. 摘要生成成功后：
   - 构造 channel update：`[RemoveMessage(REMOVE_ALL_MESSAGES), summaryAIMessage]`。
   - 读取 `config?.configurable?.emitChannelFrame`（`ChannelFrameEmitter | undefined`），对每个非空通道调用 `emitChannelFrame(agent, summaryContent)` 发射摘要帧（FR-011，实时可见）。
5. 返回 `{ playerMessages: [...], plannerMessages: [...] }`。

### 返回值

```ts
// 两个通道都有消息的常规情况
return {
  playerMessages: [
    new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
    playerSummary,
  ],
  plannerMessages: [
    new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
    plannerSummary,
  ],
};

// 某通道为空的空操作情况：该通道不包含在返回值中（不写入 = 不变）
return {
  playerMessages: [
    new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
    playerSummary,
  ],
  // plannerMessages 省略（空通道，无操作）
};
```

### 摘要提示词

```
// Player 通道摘要
你是扫雷游戏的 player agent。以下是你之前若干局游戏的对话历史。
请概括关键信息，包括：已玩局数、胜负记录、使用过的策略与效果、关键决策与经验教训。
保持简洁但信息完整，使你在下一局游戏中能据此继续。

对话历史：
{serialized messages}

请输出摘要：
```

```
// Planner 通道摘要
你是扫雷团队的 planner（复盘规划者）。以下是你之前若干局游戏的复盘对话历史。
请概括关键信息，包括：已复盘局数、策略更新历史、关键观察与判断、策略效果评估。
保持简洁但信息完整，使你在下一次复盘中能据此继续。

对话历史：
{serialized messages}

请输出摘要：
```

### 消息序列化

将 `BaseMessage[]` 序列化为文本供摘要提示词使用：

```ts
function serializeMessages(messages: BaseMessage[]): string {
  return messages
    .map((m) => {
      const role = m._getType();
      const content = typeof m.content === "string"
        ? m.content
        : JSON.stringify(m.content);
      return `[${role}]: ${content}`;
    })
    .join("\n\n");
}
```

---

## 4. Planner 节点变更

**文件**: `projects/game/agent/src/team/planner.ts`

### gameCounter 递增

planner 节点返回时，除了将 `gameEnded` 置为 `null`，还需递增 `gameCounter`：

```ts
// 成功路径
return {
  plannerMessages: result.messages.filter(...),
  gameEnded: null,
  gameCounter: state.gameCounter + 1,  // NEW
};

// 降级路径（invoke 失败后）
return {
  gameEnded: null,
  gameCounter: state.gameCounter + 1,  // NEW — 即使降级也计数（一局已结束且 planner 已尝试复盘）
};
```

### reviewInput 实时帧发射（US1）

planner 节点在构造 `reviewInput` 后、调用 `createAgent.invoke` 之前，发射实时帧：

```ts
const reviewInput = buildReviewInput(buffer);

// US1: 发射复盘输入帧（实时可见，FR-001）
// 经 config?.configurable?.emitChannelFrame 读取（tasks.md 决策 #1）
const emitChannelFrame = config?.configurable?.emitChannelFrame as
  | ChannelFrameEmitter
  | undefined;
if (emitChannelFrame) {
  const content = typeof reviewInput.content === "string"
    ? reviewInput.content
    : "";
  if (content) {
    emitChannelFrame(PLANNER_AGENT_NAME, content);
  }
}

const input: BaseMessage[] = [buildStrategyMessage(strategy), ...state.plannerMessages, reviewInput];
```

### reviewInput 内容扩展（US5）

`buildReviewInput` 渲染游戏统计数据（见 data-model.md §6）。

### PlannerNodeDeps 扩展

```ts
interface PlannerNodeDeps {
  model: ChatModel;
  strategyStore: StrategyStore;
  buffer: EphemeralGameBuffer;
  sessionId: string;
  plannerBasePrompt: string;
  playerTools: StructuredToolInterface[];                        // NEW (US3)
  createAgentFn?: CreateAgentFn;
  // reviewInput 帧经 config?.configurable?.emitChannelFrame 发射（tasks.md 决策 #1，
  // 不通过 deps 注入 emitFrame；ChannelFrameEmitter 类型从 session-team.ts 导出）
}
```

### systemPrompt 工具描述注入（US3）

```ts
// 构造 systemPrompt 时追加工具描述段
function buildToolDescriptionSection(tools: StructuredToolInterface[]): string {
  if (tools.length === 0) return "";
  const lines = [
    "",
    "## Player 可用工具",
    "以下是 player 持有的工具（你不能调用这些工具，仅可参考其描述判断 player 是否充分利用）：",
  ];
  for (const tool of tools) {
    lines.push(`- ${tool.name}: ${tool.description}`);
  }
  return lines.join("\n");
}

const systemPrompt = basePrompt + buildToolDescriptionSection(deps.playerTools);
```

---

## 5. RefreshTeam 扩展

**文件**: `projects/game/agent/src/context-middleware.ts`

`refreshTeamChannels` 在清空短期消息通道的同时，将 `gameCounter` 重置为 0：

```ts
// 当前：仅清空 playerMessages + plannerMessages
// 变更后：同时重置 gameCounter
await graph.updateState(config, {
  playerMessages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })],
  plannerMessages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })],
  gameCounter: 0,  // NEW
});
```

---

## 6. 验证要点

- **压缩触发**：连续 5 局后 `gameCounter === 5`，路由到 compress 节点。
- **压缩效果**：压缩后 `playerMessages.length === 1`（仅摘要）、`plannerMessages.length === 1`。
- **player 停下**：压缩后 graph 路由到 END（不继续到 player）。
- **策略保留**：压缩后 `StrategyStore.get(sessionId)` 返回值不变。
- **压缩失败**：LLM 调用抛错 → 节点 re-throw → TurnLoop catch → abort。
- **空通道**：空通道压缩 = 空操作（通道不变）。
- **RefreshTeam**：刷新后 `gameCounter === 0`，通道为空。
- **实时帧**：compress 节点调用 `configurable.emitChannelFrame` 后，desktop 对应 tab 实时显示摘要。
- **gameCounter 计数**：won 和 lost 均计数；planner 降级也计数。
