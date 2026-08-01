# Contract: saolei Team StateGraph

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](../spec.md) | **Research**: D5/D6/D7/D8/D10/D11

> saolei 模板的 LangGraph StateGraph 契约：节点、边、状态、策略/记忆流、planner 触发、RefreshTeam。实现于 `projects/game/agent/src/team/`（D11）。用原生 StateGraph + 自定义条件边（不依赖 swarm/supervisor 包，survey §3.1⚠️）。

---

## 1. State schema（`TeamState`）

语义字段（实现须用 **`Annotation.Root`** 定义，**不要用 `new StateSchema`+zod**——spike D14 实测 zod schema 不满足 `SerializableSchema`）：

```ts
// 语义（实现见 experimental/ts/team_graph_spike/src/team-graph.ts）
playerMessages:  Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] });
plannerMessages: Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] });
gameEnded:       Annotation<"won"|"lost"|null>({ reducer: overwrite, default: () => null });
```

- per-agent 私有消息通道（D5），与 Message 资源按 agent 分区（FR-005）一一对应。
- 策略**不在** state（在 `StrategyStore`，代码层注入 prompt，§3）。
- gameState/gameEvent**不在** state（在进程内 ephemeral buffer，`SaoleiEventSink` 写入，§4）。
- checkpointer：**单一外层 `MemorySaver`**（compile 外层图时传入；per-session thread = session id；`teamMemoryId` = session id，FR-013）。spike D14 A3 实测：`getState().values` 一次取回全部通道，per-agent 历史从单一 checkpointer 重建。
- **createAgent 不带自身 checkpointer**（spike D14 A2）：player/planner 的 createAgent 编译时不传 checkpointer，单次 `.invoke()` 跑完整 agent loop；消息持久化由外层 `MemorySaver` 统一承载。

**实现范本**：`experimental/ts/team_graph_spike/src/team-graph.ts`（spike 已验证可行的最小骨架）。**TS2883 坑**：导出 `Annotation.Root`/`StateSchema` 常量或 `CompiledStateGraph` 返回类型会触发 TS2883——`TeamState` 保持模块私有、仅导出 `typeof TeamState.State`（见 D14 注意事项 2）。

---

## 2. 节点与边

```text
START → [player] ──条件边(读 state.gameEnded)──→ [planner] ──→ [player] ...
                  │  gameEnded=null → 回 player（或 turn 结束 emit wait）
                  │  gameEnded≠null → planner
```

### 2.1 player 节点（入口；独占桌面控制；createAgent 全 loop）

- **形态**：player 节点为 **`createAgent`（内部 agent loop，跑到 LLM 自行决定停下为止）**——一次 player 节点运行 = 一次完整对局（LLM 落子直至停止）。graph **不**在 player loop 内中途移交。
- **输入**：`playerMessages`（含用户输入）。
- **工具**：saolei MCP 工具（`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_remain` 等，FR-010）。仅 player 持有。
- **策略注入**：节点进入时由代码层读 `StrategyStore.get(sessionId)`（无记录返回 `""`，D4/#3），作为"当前态势"注入 player prompt（FR-015；player 无读取工具）。
- **后处理（createAgent 返回后执行一次）**：读 ephemeral buffer 的 `gameEvent` → 若未 consumed，写 `TeamState.gameEnded = status`（D6 步骤 4）。
- **流式输出**：`ContentBlock` → `AgentFrame`（`agent="player"`）。
- **是否接受用户输入**：`true`（FR-031）。

### 2.2 planner 节点（每局结束触发一次；不控制桌面）

- **触发**：仅由条件边在 `gameEnded ≠ null` 时路由进入；**每局结束恰好触发一次**（FR-011/D6）。
- **输入**：`plannerMessages`；system 上下文 = [复盘指令] + [当前策略（`StrategyStore.get`，初始 `""`，FR-014/#3）]；复盘输入 = ephemeral buffer 的 `gameState`（D6 步骤 6）。
- **工具**：仅 `update_strategy`（写 `StrategyStore`，FR-012）；无其他读取工具。
- **`update_strategy` 重试**：由 **planner 节点内部**自行处理（重试/降级）；graph 调度**不**因 `update_strategy` 失败而重路由 planner（需求方 #6）。
- **节点返回后（graph 执行）**：graph 无条件 **`TeamState.gameEnded = null`** + 标记 buffer `gameEvent.consumed=true`（D6 步骤 6；无论 update_strategy 成败）→ 路由回 player（FR-009：是否开新局由 player LLM/用户驱动）。
- **流式输出**：`ContentBlock` → `AgentFrame`（`agent="planner"`）。
- **是否接受用户输入**：`false`（FR-031；desktop 屏蔽其 tab 输入）。

### 2.3 条件边（player → planner | player/结束）

```ts
function route(state): "planner" | "player" {
  return state.gameEnded ? "planner" : "player";
}
```
- `gameEnded ≠ null`（player 一次运行内有局结束）→ planner；`null` → player（player 决定续局或 turn 结束 emit wait，FR-009 不强制循环）。

### 2.4 `update_strategy` 工具（planner 专属）

```ts
const updateStrategy = tool(async ({ content }) => {
  await strategyStore.put(sessionId, content);  // 写 mongo（agent mongo-backed store，D4）
  return { ok: true };
}, { name: "update_strategy", schema: { content: z.string() } });
```
- 仅 planner 持有（FR-012）；写 `StrategyStore`（mongo，D4）。

---

## 3. 策略 / 记忆流

```text
StrategyStore (mongo, key=session_id)
   ↑ put(update_strategy)           ↓ get(当前态势→player prompt; system→planner)
   planner                          player / planner

MemorySaver checkpointer (per session thread)
   playerMessages / plannerMessages  ← RefreshTeam 发 REMOVE_ALL_MESSAGES 清空两通道（FR-018, D8）

Ephemeral buffer (per session, 进程内)
   sink.onGameEnd → 写 gameEvent   → player 节点后处理(createAgent 返回后)读 → TeamState.gameEnded
                                   → planner 读 gameState 复盘 → consumed
```

- 策略与短期消息**解耦**：策略在 mongo（D4），短期消息在 checkpointer；`RefreshTeam` 清短期不影响策略（FR-013/FR-018）。

---

## 4. planner 触发数据流（结构化信号，非文本解析；D6）

1. player（createAgent 内部 loop）调 saolei 工具 → MCP handler `bridge.dispatch` → `recognize()` 更新状态 → `gameStatus(state)`（`saolei-mcp.ts:253`）。
2. status 变 won/lost → handler 调 `sink.onGameEnd(state, status)`（结构化枚举）。
3. team sink → 写 ephemeral buffer `gameEvent = {state, status, endedAt, consumed:false}`（+ 更新 gameState）。
4. **player 节点后处理（createAgent 返回后执行一次）** → 读 buffer → 写 `TeamState.gameEnded = status`。
5. 条件边 → `gameEnded≠null` → planner（每局一次）。
6. planner → 读策略(system, 初始 "")+gameState → LLM 复盘 → `update_strategy`（写 StrategyStore；**重试在 planner 内部**）→ 节点返回 → **graph 置 `gameEnded=null` + `consumed=true`**（无条件，无论 update_strategy 成败）→ 回 player。

**全程不解析 tool result 文本**（FR-017）。`gameEnded` 在 planner 节点返回后由 graph 清除（每局恰好触发一次，避免重复；需求方 #6）。

---

## 5. RefreshTeam（FR-018）

- 经 `context-middleware`（`projects/game/agent/src/context-middleware.ts`，已预留扩展点）的 **`beforeModel`** 钩子（spike D14 A4 实测 middleware 可返回 `REMOVE_ALL_MESSAGES`）。
- 对 `playerMessages` 与 `plannerMessages` 各发 `RemoveMessage({id: REMOVE_ALL_MESSAGES})`（`@langchain/langgraph` 全量清空原语，D8；spike D14 A1 实测 per-channel 独立清空）。
- 策略（StrategyStore/mongo）与 gameEnded 控制状态不在清空范围（策略保留；gameEnded 由 planner 正常清除）。
- **测试断言坑**（spike D14 注意事项 3）：`gameEnded` 终值为 `null`（planner 跑完即清）；断言"局结束"应检查 `plannerMessages` 非空，而非终态 `gameEnded`。

---

## 6. 与既有基础设施的关系（D10）

- `SessionTeam`（取代 `SessionAgent`）：持有 team graph 实例 + per-session ephemeral buffer + `StrategyStore` 引用 + `OperationBridge`（player 独占）。
- `TurnLoop`：单飞/队列语义保留复用（一个 user 输入 → 一次 team turn = 一次 graph invoke）；`gameEnded` 在 turn 内由条件边处理，不破坏单飞。
- `OperationBridge`：保留（player 独占使用，planner 不操作）。
- 模板选择：路径段 `{template}`（typed enum）→ 路由到 `src/team/` 对应 builder（当前仅 saolei，D11）。

---

## 7. 验证要点

- 仅 player 持有/调用 saolei 工具；planner 不发起桌面操作（FR-010）。
- planner 仅在 `gameEnded≠null` 触发，每局一次（FR-011）；planner 处理后清 `gameEnded`（不重复触发）。
- 策略经代码层注入（player 当前态势 / planner system）；player 无读取工具（FR-015）。
- `RefreshTeam` 清两通道短期消息，策略可读（FR-018）。
- 多局由 player LLM/用户驱动，graph 无强制循环（FR-009）。
