# Research: saolei Team 模板优化

**Feature**: `037-saolei-team-optimize` | **Spec**: [`spec.md`](./spec.md)

---

## D1. LLM 上下文压缩方案调研（opencode / openclaw / hermes / LangChain 生态）

### 背景

用户要求参考 opencode（`https://github.com/sst/opencode`，即 `anomalyco/opencode` fork）、openclaw（`https://github.com/openclaw/openclaw`）、hermes（`https://github.com/nousresearch/hermes-agent`）三个项目的上下文压缩方案。本特性（spec FR-006~FR-015）需要在 saolei team graph 中实现"每 5 局触发 player/planner 通道全量压缩为一条摘要"。

### 调研结论

**本特性的压缩方案比通用 agent harness 简单得多**，无需引入复杂的 token 计数、head+tail 选择或工具结果修剪。原因：

1. **触发器是领域事件（每 5 局）而非 token 阈值**——不需要 token 估算器。
2. **压缩语义是全量替换**——不需要 head+tail 拆分或滑动窗口。
3. **每次压缩是全新摘要**（无 running summary）——因为全量替换后通道中仅剩摘要一条消息，下一次压缩时只有上一条摘要 + 新增消息。
4. **压缩失败 = 直接 abort**——不需要降级/回退策略。

### 各项目方案对比

| 维度 | opencode (sst/opencode) | LangChain SummarizationMiddleware | Claude Code | Deep Agents SDK | **本特性 (037)** |
|---|---|---|---|---|---|
| 触发 | token 阈值（context limit − 20K buffer） | 可配（messages/tokens/fraction） | ~95% context window | 85% context window | **每 5 局游戏**（领域事件） |
| 压缩范围 | head（旧消息→摘要）+ tail（保留近期） | summarize 旧的 + keep 近 N 条 | 全量→结构化摘要 | 全量→摘要 + filesystem 保存原始 | **全量替换**（通道全部→一条摘要） |
| 摘要模型 | 专用 compaction agent（可用更便宜模型） | 可指定单独 summarization model | 同一模型 | 同一模型 | **复用各自 agent 模型** |
| running summary | 是（"Update the anchored summary"） | 是（running_summary 传播） | 否（每次全新） | 否 | **否**（全量替换后仅一条摘要，下次压缩时自然包含） |
| 工具结果修剪 | 是（prune 旧 tool outputs，保留 metadata） | 否 | 是（pre-query optimization） | 是（offload 到 filesystem） | **否**（全量替换，无需单独修剪） |
| 失败处理 | 降级/重试/unrecoverable ContextOverflowError | 静默跳过 | 确定性 head-drop fallback | N/A | **直接 abort**（致命错误，不降级不重试） |
| 压缩后行为 | replay 最后一条 user message | 继续 agent loop | 重新附加最近 5 个文件 | 继续 agent loop | **player 停下、turn 结束、等用户输入** |

**来源引用**：
- opencode compaction 源码: `https://github.com/sst/opencode/blob/main/packages/opencode/src/session/compaction.ts`
- opencode DeepWiki 文档: `https://deepwiki.com/sst/opencode/2.4-context-management-and-compaction`
- LangChain SummarizationMiddleware: `https://reference.langchain.com/python/langchain/agents/middleware/summarization/SummarizationMiddleware`
- LangChain short-term memory docs: `https://docs.langchain.com/oss/javascript/langchain/short-term-memory`
- LangGraph JS add-summary how-to: `https://github.com/langchain-ai/langgraphjs/blob/main/examples/how-tos/add-summary-conversation-history.ipynb`
- Deep Agents context management blog: `https://www.langchain.com/blog/context-management-for-deepagents`
- Agent harness context management 对比: `https://arize.com/blog/context-management-in-agent-harnesses/`

### 决策：采用 LangGraph 原生模式

**Decision**: 采用 LangGraph JS 的 "separate summarize node + RemoveMessage" 模式（参考 [LangGraph JS add-summary how-to](https://github.com/langchain-ai/langgraphjs/blob/main/examples/how-tos/add-summary-conversation-history.ipynb)），在 team graph 中插入压缩节点。

**Rationale**:
1. **框架原生**：dominion 的 team graph 使用 `@langchain/langgraph` ^1.4.8，`messagesStateReducer` 已支持 `RemoveMessage({ id: REMOVE_ALL_MESSAGES })` 清空通道（`specs/031-team-template-mode/contracts/team-graph-contract.md` §5, spike A1）。压缩 = 先清空再写入摘要，完全复用现有机制。
2. **与 RefreshTeam 一致**：`context-middleware.ts` 的 `refreshTeamChannels` 已用相同模式清空通道（`RemoveMessage(REMOVE_ALL_MESSAGES)`），压缩节点复用同一清空 + 写入路径。
3. **无需引入外部依赖**：不需要 LangMem、不需要 token 计数器、不需要 summarizationMiddleware（该 middleware 是 `createAgent` 内部的 `beforeModel` hook，而我们的压缩发生在 graph 节点级别，不在 createAgent loop 内部）。
4. **比 opencode 简单**：opencode 的 compaction 涉及 head+tail 选择、token budget 估算、tool pruning、media stripping、overflow replay 等复杂逻辑——这些在"每 5 局全量替换"语义下全部不需要。

**Alternatives considered**:
- *LangChain `summarizationMiddleware`*：它是 `createAgent` 的 `beforeModel` middleware，在每次模型调用前检查并压缩。但我们的压缩触发点在 graph 级别（planner 返回后、player 之前），不在 createAgent loop 内部。不适合。
- *LangMem `SummarizationNode`*：Python 专用（langmem 是 Python 包），dominion 是 TypeScript。不适用。
- *opencode 的 compaction 独立 agent*：opencode 使用一个独立的 "compaction" agent + agent loop 来生成摘要。我们不需要 agent loop（压缩不需要工具调用），直接用 `model.invoke()` 生成摘要即可。

### 压缩节点设计要点

```text
压缩节点（"compress"）插入位置：
  planner → [compress（条件：gameCounter % 5 === 0）] → player/END

压缩节点逻辑：
  1. 对 playerMessages 通道：model.invoke(summarizePrompt + channel messages) → AIMessage
  2. 对 plannerMessages 通道：同上（用 planner model）
  3. 返回 {
       playerMessages: [RemoveMessage(REMOVE_ALL_MESSAGES), summaryAIMessage],
       plannerMessages: [RemoveMessage(REMOVE_ALL_MESSAGES), summaryAIMessage],
     }
  4. 压缩失败 → throw（graph 传播异常 → TurnLoop catch → abort）
```

- 压缩节点需要访问 player/planner 两个 model（通过 `TeamGraphDeps` 注入）。
- 压缩节点的摘要提示词需要包含角色上下文（player/planner 各自的职责）。
- 空通道压缩 = 空操作（FR-015）：通道长度为 0 时跳过，不产生空摘要。
- 压缩后路由到 END（不是 player），player 在下一 turn 以摘要上下文重建（FR-010）。

---

## D2. opencode go provider 会话 ID 参数问题（用户问题 #2）

### 问题

用户在 opencode go 管理页面观察到：agent 服务每次向 provider 发送的请求都没有"会话 ID"参数。

### 调研结论

**确认：当前实现中，LLM provider 调用不携带任何会话标识。**

**根因分析**（代码溯源）：

1. **ChatModel 实例是 per-model-name 单例，跨 session 共享**：
   - `projects/game/agent/src/model-provider.ts` `ModelProviderCache.getProvider(modelSpec)` 按 bare model name 缓存 ChatModel 实例（`this.cache.get(bareModel)`），同一 model 的所有 session 共用一个实例。
   - `initChatModel(bareModel, { modelProvider, apiKey, configuration: { baseURL } })` 仅传递 model name、provider format、API key 和 base URL——**无 session ID**。

2. **graph config 中的 `metadata: { session_id }` 不传递到 provider**：
   - `projects/game/agent/src/session-team.ts:257` 在 `streamEvents` config 中设置 `metadata: { session_id: this.sessionId }`。
   - 但该 metadata 是 **LangGraph runtime 内部元数据**（用于 checkpoint namespace 路由），**不会**被转发到 ChatModel 的 HTTP 请求。LangChain 的 ChatModel 调用不自动将 graph config metadata 注入到 provider HTTP headers/body 中。

3. **`createAgent` 的 invoke 不传递 session 上下文到 provider**：
   - `projects/game/agent/src/team/player.ts` `playerAgent.invoke({ messages: input }, config)` 将 graph config 传递给 agent，但 agent 内部的 ChatModel 调用仅消费 `signal`（AbortSignal）和 `recursionLimit`，不消费 `metadata.session_id`。

### 对本特性的影响

**压缩功能的 LLM 调用同样不携带会话 ID**。压缩节点通过 `model.invoke()` 生成摘要，该调用走相同的 ChatModel 单例，不传递 session ID 到 provider。

**这不阻塞压缩功能的实现**——压缩 LLM 调用的行为与现有 player/planner 模型调用一致。

**潜在改进方向**（不在本特性范围内，记录为后续 TODO）：
- 如果需要 provider 级别的 session 可观测性（在 opencode go 管理页面区分不同 session 的请求），可在 `ModelProviderCache` 或 ChatModel 调用处注入 session-scoped metadata（如 OpenAI 的 `user` 参数或自定义 header）。
- 这需要修改 model-provider.ts 和 server.ts 的 factory wiring，属于横切关注点，应单独规划。

---

## D3. 通道消息实时帧发射机制（US1 基础设施）

### 问题

US1 要求 planner 的复盘输入（HumanMessage）在 desktop 实时可见。当前 `streamEvents` 仅产出 createAgent 内部的 `messages`/`tools` 协议事件，不产出 createAgent 的**输入** HumanMessage（`projects/game/agent/src/session-team.ts` `runTeamTurn` 仅订阅 `messages` 的 `content-block-finish` 与 `tools` 的 `tool-started`/`tool-finished`）。

### 决策：在 graph 节点级别注入帧发射回调

**Decision**: 为 team graph 注入一个 `emitFrame` 回调（`TurnLoopEmit` 类型），使节点能在 `createAgent.invoke` 之外主动发射 `TeamFrame`。planner 节点在构造 reviewInput 后、调用 createAgent 之前，调用 `emitFrame` 发射复盘输入帧（携带 `agent="planner"`）。

**Rationale**:
1. **最小侵入**：不需要修改 LangGraph 的 streamEvents 机制或 createAgent 的 middleware。
2. **通用性**：US1 建立的机制（FR-005）可复用于 US2 的压缩摘要（压缩节点写入摘要后同样调用 emitFrame 发射摘要帧）。
3. **与现有帧格式一致**：复用 `turn-loop.ts` `buildTeamFrame` 构造帧，`agent` 字段标记归属 tab。
4. **DI 友好**：`emitFrame` 通过 `TeamGraphDeps` 注入，测试中传入录制数组（`style/javascript.md` §测试）。

**设计要点**:
- `TeamGraphDeps` 新增 `emitFrame?: (frame: TeamFrame) => void`（可选，默认 no-op）。
- planner 节点在 `buildReviewInput(buffer)` 后，将 reviewInput 内容转换为 `ContentBlock[]`（复用 `session-team.ts` `messageToContentBlocks` 或直接构造 TextPart），调用 `emitFrame(buildTeamFrame(...))`。
- 压缩节点同理：写入摘要 AIMessage 后，调用 `emitFrame` 发射摘要帧。
- 帧去重：实时发射的帧与重载时 ListMessages 返回的同一条消息（相同 messageId/frameId）MUST 去重（复用 desktop 的 `renderedMessageIds` 机制，`projects/game/desktop/frontend/src/App.svelte`）。

**Alternatives considered**:
- *createAgent middleware `afterModel`/`beforeModel`*：middleware 可以拦截模型调用前后的状态，但复盘输入是 createAgent 的**输入参数**（不在 model call 循环中），middleware 无法捕获。
- *自定义 LangGraph stream mode*：可以实现自定义事件类型，但侵入性大且 LangGraph v1.4.8 的 streamEvents v3 不支持自定义 event method。
- *在 `runTeamTurn` 中检测 channel 变化并补发帧*：需要在 streamEvents 之外额外监测 channel 写入，复杂且时序不确定。

---

## D4. 游戏统计数据计算方案（US5）

### 问题

需要在 `onGameEnd` 事件中增加三项统计数据（operationCount、correctFlags、avgOpsPerMine），且 MUST 由 MCP 内部第一手计算。

### 决策：MCP 内部维护 per-game 操作计数器 + 终局推导 correctFlags

**operationCount**:
- 在 MCP server closure 中维护 `let operationCount = 0`。
- `onGameStart`（`saolei_init` 成功后）重置为 0。
- `registerCellTool` handler 中，在 `runSink("onMove", ...)` 调用前（即 `recognize` 成功后）`operationCount++`。
- 被拒落子（`validateMove` 返回 `ok:false`）在 `operationCount++` 之前 `return`，不计入。
- `saolei_init` 和 `saolei_remain` 不经过 `registerCellTool`，天然不计入。

**correctFlags**:
- 终局时由 GameState 第一手推导：`correctFlags = totalMines − MINE格数 − HIT_MINE格数`。
- `totalMines` 取自开局识别状态 `initState.mineCounter`：开局 flags=0 时 counter value = 总地雷数（`projects/game/pkg/saolei-board/src/core/counter.ts` `decodeMineCounter` 返回 `{ decoded: true, value }`，value = mines − flags = mines − 0 = mines）。
- 终局 `MINE`/`HIT_MINE` 格数：遍历终局 `state.grid` 统计。
- 需在 MCP closure 中保存 `initState`（`onGameStart` 时 `initState = state`）。

**avgOpsPerMine**:
- `avgOpsPerMine = operationCount / correctFlags`，保留两位小数（`(Math.round(x / y * 100) / 100)`）。
- `correctFlags === 0` → 以 `"N/A"` 字符串表示（明确不可计算语义，不产生 NaN/Infinity）。
- `initState.mineCounter` 不可解码（`decoded: false` 或 `undefined`）→ correctFlags 标记为 `null`，avgOpsPerMine 标记为 `"N/A"`。

**Sink 接口扩展**:
- `SaoleiEventSink.onGameEnd` 参数扩展：从 `(state, status)` → `(state, status, stats?)`。
- `stats` 为可选参数（向后兼容，FR-019 不变：接口仅描述事件形状）。
- team sink 将 stats 写入 ephemeral buffer（随 gameEvent 一并存储）。
- planner 的 `buildReviewInput` 读取 buffer 中的 stats，渲染到复盘 message 中。

**数据结构**:
```ts
interface GameStats {
  operationCount: number;
  correctFlags: number | null;  // null = counter 不可解码
  avgOpsPerMine: number | "N/A";  // "N/A" = correctFlags 为 0 或 null
}
```

### 来源验证
- CellStatus 包含 `"MINE"` 和 `"HIT_MINE"`（`projects/game/pkg/saolei-board/src/core/types.ts:12-26`）。
- `isWin` 返回 true 时 MINE/HIT_MINE 均为 0（`projects/game/pkg/saolei-board/src/core/win.ts:51-56` `NON_WIN_CELLS` 包含 MINE/HIT_MINE）。
- `MineCounter` 类型为 `{ decoded: true; value: number } | { decoded: false }`（`projects/game/pkg/saolei-board/src/core/types.ts:39-41`）。
- `onMove` 仅在 `registerCellTool` 内成功识别后触发（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:790`）。

---

## D5. desktop FIFO 消息上限实现方案（US4）

### 问题

desktop 每个 agent tab 需要消息数量上限，超出按 FIFO 移除最旧消息。

### 决策：在 App.svelte chatMessages 状态更新处统一截断

**Decision**: 定义命名常量 `MAX_CHAT_ENTRIES_PER_AGENT`（建议值 200），在所有写入 `chatMessages[agent]` 的位置统一应用截断逻辑。

**Rationale**:
1. **纯前端改动**：不依赖后端改动，仅修改 `projects/game/desktop/frontend/src/App.svelte`。
2. **统一截断点**：`chatMessages[agent]` 的所有写入处（`handleMessageParts`、`loadAgentHistories`、warn 处理、optimistic user turn）都应用同一截断函数 `trimFifo(entries, max)`。
3. **FIFO 语义**：截断函数保留最新的 N 条，移除最旧的（`entries.slice(-max)`）。

**写入位置清单**（App.svelte 中所有 `chatMessages = {...}` 赋值处）:
- `handleMessageParts`（实时帧 + 流式合并）：line 737, 742-752
- `loadAgentHistories`（历史加载）：line 509-512
- `handleAgentFrame` warn 处理：line 785-796
- `handleSendChatText` optimistic user turn：line 880-889

**截断函数**:
```ts
const MAX_CHAT_ENTRIES_PER_AGENT = 200;
function trimFifo<T>(entries: T[], max: number = MAX_CHAT_ENTRIES_PER_AGENT): T[] {
  return entries.length > max ? entries.slice(-max) : entries;
}
```

每个写入处替换为 `trimFifo([...list, newEntry])` 或在赋值后截断。

---

## D6. planner 系统提示词工具描述注入方案（US3）

### 问题

planner 的系统提示词需要包含 player 工具的名称与描述（静态文本），但 planner 的实际工具集仍仅 `update_strategy`。

### 决策：在 createPlannerNode 中拼接工具描述文本段

**Decision**: 在 `createPlannerNode` 构造 systemPrompt 时，从 `deps` 获取 player 工具列表，提取每个工具的 `name` 和 `description`，拼接成文本段追加到 planner 的 systemPrompt。

**Rationale**:
1. **静态计算**：工具集由模板固定装配（`specs/031-team-template-mode/spec.md` FR-028），在 team 构建时（`buildTeamGraph`）一次性计算，不引入运行时查询。
2. **DI 友好**：player 工具列表已在 `TeamGraphDeps.playerTools` 中，传递给 planner node 即可。
3. **工具描述来源**：`StructuredToolInterface` 的 `description` 字段（LangChain 工具接口）。saolei MCP 工具的 description 在 `saolei-mcp.ts` `registerTool` 时定义。

**实现要点**:
- `PlannerNodeDeps` 新增 `playerTools: StructuredToolInterface[]`（从 `TeamGraphDeps` 传入）。
- 构造 systemPrompt 时追加一段：
  ```
  ## Player 可用工具
  以下是 player 持有的工具（你不能调用这些工具，仅可参考其描述判断 player 是否充分利用）：
  - saolei_init: <description>
  - saolei_click: <description>
  ...
  ```
- planner 的 `tools` 仍仅 `[buildUpdateStrategyTool(strategyStore, sessionId)]`（FR-018 不变）。

---

## D7. 压缩节点与 graph 路由的交互

### 问题

压缩节点插入后，graph 结构从 `planner → player` 变为 `planner → [compress?] → player/END`。需要在条件边中判断是否触发压缩。

### 决策：新增 gameCounter 状态字段 + 条件路由

**Decision**: 在 `TeamState` 中新增 `gameCounter: number` 字段（last-write-wins reducer，default 0）。planner 节点返回时递增 `gameCounter`。条件边读取 `gameCounter % 5 === 0` 判断是否路由到 compress 节点。

**Graph 结构变更**:
```text
当前：
  START → player ──条件(gameEnded)──→ planner → player ...
                └──(null)──→ END

变更后：
  START → player ──条件(gameEnded)──→ planner ──条件(gameCounter%5===0)──→ compress → END
                                              └──(else)──→ player ...
                └──(null)──→ END
```

**关键约束**:
- compress 节点路由到 **END**（不是 player），实现 FR-010（player 停下等待用户输入）。
- 非压缩路径：planner → player（保持原有连续游戏行为，FR-009）。
- `gameCounter` 在 `RefreshTeam` 时一并清零（与短期消息一起重置，保持一致语义）。

**State schema 变更**:
```ts
const TeamState = Annotation.Root({
  playerMessages: Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] }),
  plannerMessages: Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] }),
  gameEnded: Annotation<GameEnded>({ reducer: (_p, n) => n, default: () => null }),
  gameCounter: Annotation<number>({ reducer: (_p: number, n: number) => n, default: () => 0 }),  // NEW
});
```

`TeamStateValue` 接口相应新增 `gameCounter: number`。

---

## D8. 压缩摘要提示词设计

### 问题

压缩摘要需要概括 player/planner 各自通道的全部短期消息要点，使 agent 能据此继续。

### 决策：角色化摘要提示词

**Player 通道摘要提示词**:
```
你是扫雷游戏的 player agent。以下是你之前若干局游戏的对话历史摘要。
请概括关键信息，包括：已玩局数、胜负记录、使用过的策略与效果、关键决策与经验教训。
保持简洁但信息完整，使你在下一局游戏中能据此继续。

[历史消息]
{channel messages serialized}
```

**Planner 通道摘要提示词**:
```
你是扫雷团队的 planner（复盘规划者）。以下是你之前若干局游戏的复盘对话历史摘要。
请概括关键信息，包括：已复盘局数、策略更新历史、关键观察与判断、策略效果评估。
保持简洁但信息完整，使你在下一次复盘中能据此继续。

[历史消息]
{channel messages serialized}
```

摘要通过 `model.invoke([{ type: "human", content: prompt }])` 生成，返回 `AIMessage`。

**消息序列化**: 将 BaseMessage[] 序列化为文本——遍历每条消息，输出 `[role]: content`。复用 `messageToContentBlocks` 的 role 映射逻辑（或直接读 `msg._getType()` + `msg.content`）。

---

## D9. 压缩实时帧发射与去重

### 问题

压缩摘要写入通道后需要实时可见（FR-011），复用 US1 的帧发射机制（FR-005）。

### 决策

压缩节点在写入摘要 AIMessage 后，调用 `emitFrame` 发射摘要帧：
- 帧携带 `agent="player"`（player 通道摘要）或 `agent="planner"`（planner 通道摘要）。
- 帧的 `messageParts.parts` 为 `[{ text: { content: summaryText } }]`。
- 帧的 `frameId` 与摘要 AIMessage 的 `id` 一致，使 desktop 去重（`renderedMessageIds`）在重载时正确去重。

**去重保证**:
- 实时帧：desktop `handleMessageParts` 收到帧 → `renderedMessageIds.add(frameId)`。
- 重载：desktop `loadAgentHistories` 从 ListMessages 获取同一条消息（相同 messageId）→ `renderedMessageIds.has(mid)` 跳过。
- 二者内容一致（均为摘要 AIMessage 的文本内容）。

---

## 未解决的问题（无）

所有 NEEDS CLARIFICATION 已在 spec.md Clarifications 和 Assumptions 中解决。本 plan 阶段无新增待定项。
