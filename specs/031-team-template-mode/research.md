# Research: Team Template Mode (StateGraph 升级)

**Feature**: `031-team-template-mode` | **Date**: 2026-07-29 | **Spec**: [`spec.md`](./spec.md) | **Survey**: `survey/agent-team-mode.md`

> 本文件为 Phase 0 调研与设计决策，resolve spec/plan 中所有 NEEDS CLARIFICATION 与关键架构选型。每项给出 **Decision / Rationale / Alternatives**。需求方三条 directive（① 大规模重构移除旧码；② proto/desktop 优先通用+typed、禁 blob/禁潜规则；③ 策略存 mongo 用当前 mongo 服务）为决策纲领。

---

## D1. 模板特化配置的 proto 表达：typed `oneof`（非 blob）

**Decision**: `TeamProfile` 用 **typed `oneof spec`** 表达模板特化配置，每个模板一个具名 message 变体：

```proto
message TeamProfile {
  string name = 1;              // templates/{template}/profiles/{profile}
  Template template = 2;        // typed 枚举，与 oneof 变体一致（校验）
  google.protobuf.Timestamp create_time = 3;
  google.protobuf.Timestamp update_time = 4;
  oneof spec {
    SaoleiProfile saolei = 10;  // 模板特化，typed 字段
  }
}
message SaoleiProfile {
  string player_model = 1;
  string planner_model = 2;
  string player_prompt = 3;   // 可选；空字符串=未设置=回退模板默认 base（FR-034）
  string planner_prompt = 4;  // 可选；空字符串=未设置=回退模板默认 base（FR-034）
}
```

**Rationale**: 需求方 directive ② 明确"不要用 bytes/string 等"非格式化方式实现"通用"，"不要为通用引入潜规则"。`oneof` 是**通用容器机制**（一个 profile 资源），每个变体是**具名 typed 特化**（FR-027 saolei 含 player/planner 模型选择与各自可选 base 提示词 `player_prompt`/`planner_prompt`，空值回退模板默认 base，FR-034）。新增模板 = 新增一个 `oneof` 变体 + 一个 message，类型安全、无 blob、无隐式约定。`template` 字段与激活的 oneof 变体必须一致（handler 校验，避免"潜规则"）。

> **修订说明（2026-08-02，需求方确认语义 A）**：SaoleiProfile 增加可选 `player_prompt`/`planner_prompt`（base 提示词）——原设计"prompt 由模板固定装配"修订为"**base 提示词可由 profile 配置**（空值回退模板默认 base；player 的 saolei skill body 始终由模板追加）"，见 [`spec.md`](../spec.md) FR-034。tools/mcp/skill 仍不可经 profile 配置（FR-027/FR-028）。

**Alternatives**:
- ❌ `string json_config` / `bytes config_payload` + 各端自行反序列化：被 directive ② 明确禁止（非格式化、潜规则）。
- ❌ 每模板独立资源（`templates/saolei/profiles/...` vs `templates/foo/profiles/...` 各自消息类型）：破坏"TeamProfile 是统一资源"的通用性，且 List/校验需潜规则区分。

来源：directive ②；AIP-149（oneof as variant）；`projects/game/game.proto` 既有 oneof 用法（`MessagePart.kind`、`FlowPart.kind`）。

---

## D2. Template 的 proto 表示：typed 枚举（仅路径段，无 CRUD 资源）

**Decision**: Template 为 **typed `enum`**（`TEMPLATE_UNSPECIFIED=0; TEMPLATE_SAOLEI=1`），仅作路径段与字段值；**不定义 `Template` 资源消息、不提供 List/Get/Create/Update/Delete RPC**（FR-001）。Session/TeamProfile 等消息用 typed `Template` 字段而非裸 `string`。

**Rationale**: FR-001 约束无模板列表 API、desktop 用枚举常量。用 typed enum（而非 string）满足 directive ②"优先通用+typed、禁潜规则"——所有引用模板处都用同一枚举，避免字符串拼写"潜规则"。Template 无独立资源消息（无 CRUD 即无资源生命周期）。

**Alternatives**:
- ❌ 裸 `string template`：易拼写错、潜规则（directive ② 禁）。
- ❌ 定义 `Template` 资源 + Get RPC：与 FR-001"无 List/CRUD"冲突，过度设计（宪法原则 II）。

来源：FR-001；AIP-126（枚举）。

---

## D3. Team 内 agent 元数据：后端 typed 暴露，desktop 通用渲染（不硬编码）

**Decision**: team 的 agent 列表及其属性由**后端 typed 暴露**（`Team.agents: repeated TeamAgent`，`TeamAgent{name; accepts_user_input}`，FR-031），desktop **从该描述通用渲染** tab（不按模板硬编码 agent 名）。agent 的"是否接受用户输入"为 typed bool 字段。

```proto
message TeamAgent { string name = 1; bool accepts_user_input = 2; }
message Team {
  string name = 1;                       // templates/{template}/sessions/{session}/team
  repeated TeamAgent agents = 2;         // 来自模板 graph schema
  google.protobuf.Timestamp create_time = 3;
}
```

**Rationale**: directive ②"优先通用设计"。desktop 仅持有 **Template 枚举常量**（模板集合，FR-024），但**不硬编码各模板的 agent 名**——agent 列表与输入能力从 `Team.agents`（typed）读取，使 desktop 的多 tab 渲染对所有模板通用。这避免"if saolei then player/planner"式的潜规则。agent 服务从 saolei 模板定义（code）导出 `TeamAgent` 描述。

**Alternatives**:
- ❌ desktop 按模板硬编码 agent 列表：违反 directive ②（潜规则、非通用）。
- 注：desktop 仍持有 Template 枚举常量（模板集合本身是固定的，FR-024），这与"agent 列表通用渲染"不矛盾——前者是模板集合，后者是单模板内的 agent。

来源：FR-024/FR-031/FR-032；directive ②。

---

## D4. 策略长期记忆持久化：agent 服务 mongo-backed memory store（不经 prompt 服务）

**Decision**: Strategy（长期策略记忆）由 **agent 服务自身**持久化到 MongoDB（当前 mongo 实例），agent 实现 mongo-backed memory store；team graph 经 `StrategyStore` 接口（`get(sessionId)` / `put(sessionId, content)`，DI）访问。**不经 prompt 服务**——prompt 服务仅管 TeamProfile 静态配置。agent 由此新增 mongo 客户端依赖（连接当前 mongo 实例，连接配置经 secrets，类同 prompt 服务连法）。

**初始值（需求方决策 #3）**：`get(sessionId)` 无记录时返回 **空字符串 `""`**；策略内容由 planner 首次 `update_strategy` 写入。planner 的 system 上下文 = [复盘指令] + [当前策略，初始 `""`]；player 的"当前态势"注入同理（初始为空）。

**Rationale**: 需求方 directive ③ + 修订 #5"agent 服务通过 memory 存储在数据库中"。strategy 是 agent 运行时记忆，由 agent 自治持久化，与 prompt 服务（静态配置）解耦——消除"prompt 服务承载运行时 strategy"的语义过载（原评审 #5）。复用 LangGraph 长期记忆概念（survey §7.1 的 memory/Store 抽象），impl 为 mongo-backed（持久，满足 directive ③"存数据库"）。`StrategyStore` 接口隔离存储 impl，便于测试用 fake。

策略为 agent 内部 memory（非公开 REST 资源、不经任何 gRPC 中转）；mongo 文档形状见 `data-model.md` §2.1。session 删除**暂不**级联清理 strategy（需求方决策 #7，strategy 管理后续优化）。

**Alternatives**:
- ❌ 经 prompt 服务 gRPC 中转（原 D4 初版）：语义过载 prompt 服务（评审 #5），需求方修订 #5 已否决。
- 注：mongo-backed store 是否实现为完整 LangGraph `BaseStore`（`runtime.store` 访问）还是自定义 `StrategyStore` 接口，属 impl 细节；契约仅约束 get/put 语义 + mongo 持久 + 初始 `""`。建议 impl 时优先对齐 LangGraph memory 抽象。

来源：directive ③；需求方修订 #3/#5/#7；FR-013/FR-014；LangGraph memory/Store 抽象 `survey/agent-team-mode.md` §7.1。

---

## D5. team graph 消息通道：per-agent 私有通道（非共享 scratchpad）

**Decision**: team state 采用 **per-agent 私有消息通道**（`playerMessages`、`plannerMessages`，各为 `MessagesValue`），与 FR-005"消息按 team 内 agent 分区"及 desktop 多 tab 一一对应。player 与 planner 之间**不共享消息历史**，仅共享**策略**（StrategyStore，长期）与 **graph 控制状态**（`gameEnded` 等）。

```ts
interface TeamState {
  playerMessages: MessagesValue;   // 短期：player 的对话/工具消息
  plannerMessages: MessagesValue;  // 短期：planner 的复盘消息
  gameEnded: "won" | "lost" | null; // 结构化局结束标志（survey Q5）
}
```

**Rationale**: FR-005 将 Message 资源按 agent 分区，desktop 每 tab 显示一个 agent 的消息——per-agent 私有通道与之直接对应，历史重建直接读对应通道（无需"共享后按 agent 过滤"的潜规则，directive ②）。语义上 player 不需要 planner 的推理文本、planner 不需要 replay player 全部落子（planner 读 sink 的 gameState），故不需共享消息；策略是显式共享机制（非消息历史）。

**spike 实测确认（D14 A3）**：单一外层 `MemorySaver` 即可序列化 `playerMessages`+`plannerMessages`+`gameEnded`（`getState().values` 一次取回），per-agent 历史从单一 checkpointer 重建——架构 (i) 成立，无需分离 createAgent（架构 ii 更重且非必要）。

**Alternatives**:
- ⚠️ 单一共享 `messages` 通道 + 按 agent 标签过滤显示（survey §3.1(1) shared scratchpad）：需"显示时过滤"的潜规则，且 LLM 上下文跨 agent 串扰。per-agent 通道更清晰、无潜规则。

来源：FR-005；FR-013（策略为共享机制）；directive ②；`survey/agent-team-mode.md` §3.1(1)、§6.1。

---

## D6. planner 触发与 gameEnded 生命周期（resolve survey Q5）

**Decision（需求方修订 #1/#6）**：player 节点为 **`createAgent`（内部 agent loop，跑到 LLM 自行决定停下为止，不逐步中断）**。结构化 hook 信号触发 planner，**每局结束进入 planner 恰好一次**；`update_strategy` 的重试由 **planner 节点内部自行处理**，graph 调度层面**不**做重试。精确数据流：

1. player（createAgent）内部 loop 调 saolei 工具 → MCP handler 执行 → `recognize()` 更新状态 → 计算 `gameStatus(state)`（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:253`）。
2. 当 status 变为 `won`/`lost`，handler 调 `sink.onGameEnd(state, status)`（结构化枚举，非文本）。
3. team sink 实现：写**进程内 ephemeral state buffer**（per session）= `{state, status, endedAt, consumed}`（gameState/gameEvent）。
4. **player 节点后处理（createAgent 返回后执行一次，非 per-tool）**：读 state buffer → 若有未 consumed 结束事件，写入 `TeamState.gameEnded = status`。
5. **条件边**读 `state.gameEnded`：非 null → 路由 `planner`；null → 路由 `player`（或 turn 结束 emit wait）。
6. **planner 节点（每局触发一次）**：读 StrategyStore（当前策略为 system，初始 `""`）+ state buffer（gameState）→ LLM 复盘 → 调 `update_strategy`（写 StrategyStore；**重试在 planner 节点内部**）→ 节点返回后 **graph 清除 `TeamState.gameEnded = null`**（并标记 buffer 事件 consumed）→ 路由回 player（FR-009：是否开新局由 player LLM/用户驱动，非强制循环）。

**关键约束**：
- player = createAgent 全 loop（跑到 LLM 停下）；gameEnded 在 createAgent **返回后**由 player 节点后处理从 buffer 读入（一次），graph 不在 player loop 内中途移交。
- **每局结束进入 planner 恰好一次**：一次 player 运行对应一局，buffer 记最新结束事件；planner 处理后 graph 置 `gameEnded=null`，避免重复触发。
- **`update_strategy` 重试由 planner 内部处理**（节点内重试/降级），graph 调度不因 `update_strategy` 失败而重路由 planner；graph 在 planner 节点返回后无条件清 `gameEnded`（无论 update_strategy 成败）。

**Rationale**: 需求方修订 #1（createAgent 全 loop）/#6（planner 每局一次、重试内部、graph 不调度重试）。信号来自 MCP 内部第一手 `gameStatus()`（FR-017），经 sink 结构化输出，全程不解析 tool result 文本（survey §5.3）。

**遗留假设（需在实现/测试中确认）**：一次 player createAgent 运行 = 一局（LLM 在局结束后自行停止）。若 LLM 在一次运行内连玩多局，buffer 仅记最新结束事件（中间局不单独触发 planner）——此为接受的边界行为（FR-009 多局由 LLM/用户驱动）。

**Alternatives**:
- ❌ 手写逐步 loop（每 superstep 一次落子）+ 中途移交：控制更精确但放弃 createAgent 开箱能力、需重接 streaming/middleware，工作量与风险更高（需求方 #1 已选 createAgent 全 loop）。
- ❌ graph 调度层重试 update_strategy（重新路由 planner）：需求方 #6 已否决（重试归 planner 内部）。
- ❌ 字符串匹配 tool result 文本（survey §5.3 已废弃）：不稳定。

来源：FR-009/FR-011/FR-017；需求方修订 #1/#6；survey §5.3/§6.2；`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:253`。

---

## D7. gameState/gameEvent 缓冲：进程内 ephemeral（非持久、非"记忆"）

**Decision**: sink 写入的游戏状态/结束事件存于**进程内 per-session ephemeral buffer**（普通对象/Map，非 LangGraph Store、非 mongo、非 checkpointer）。player/planner 节点在同一 turn 内读取。

**Rationale**: gameState 是触发信号与 planner 复盘输入，生命周期在一个 turn 内（player 落子→sink 写→player wrapper 读→planner 读），无需持久化。它与"短期记忆"（messages，FR-016/FR-018）是不同概念——`RefreshTeam`（FR-018）清空短期记忆（messages），**不清空** state buffer（state buffer 由下次落子覆盖，语义上非"记忆"）。策略（D4，mongo）与 state buffer（D7，ephemeral）是两个独立存储，不混用。

**Alternatives**:
- ❌ 复用 LangGraph `InMemoryStore` 承载 gameState+strategy（survey §6.1 原议）：strategy 改 mongo（D4）后，gameState 不必再走 Store 抽象；ephemeral buffer 更轻。

来源：FR-016/FR-018；D4；survey §6.1。

---

## D8. RefreshTeam 清空短期记忆机制：`REMOVE_ALL_MESSAGES`（per-agent 通道）

**Decision**: `RefreshTeam`（FR-018）经 `context-middleware`（已预留扩展点，`projects/game/agent/src/context-middleware.ts`）对**两个 per-agent 通道**（`playerMessages`、`plannerMessages`，D5）各发 `RemoveMessage({id: REMOVE_ALL_MESSAGES})`（`@langchain/langgraph` 内置全量清空原语）。策略（StrategyStore，mongo）不受影响。

**Rationale**: survey §7.2 论证 `messagesStateReducer` 内置 `REMOVE_ALL_MESSAGES` 原语丢弃所有 prior messages。per-agent 通道各清一次即清空全部短期记忆。策略在独立 mongo 层（D4），与 checkpointer 解耦，天然不受影响（FR-013/FR-018）。落地于已预留的 `context-middleware`（宪法原则 II：落地预留扩展点，非新堆叠）。

**Alternatives**:
- ❌ 删除整个 checkpointer thread：影响控制状态（gameEnded 等），过粗。
- ❌ `summarizationMiddleware`：survey §7.2 指出其 checkpoint 增长 bug（deepagents#2876），且诉求是"清空"非"摘要"。

**实现结果（Phase 5 Batch 2，2026-07-30，用户已接受偏离）**：最终**未**落地于 middleware 钩子，而是 `context-middleware.ts` 的 `refreshTeamChannels` 经**外层 `graph.updateState({ configurable: { thread_id } }, clearChannel("playerMessages") + clearChannel("plannerMessages"))`** 直清两通道（per-channel `RemoveMessage({id: REMOVE_ALL_MESSAGES})`，spike A1 实测 per-channel 独立）。**根因**：player/planner 的 `createAgent` 不带自身 checkpointer（D14 A2），其 middleware 只见 createAgent 自身的 `{messages}` 通道，无法触达外层 `playerMessages`/`plannerMessages`（两通道持久化在外层单一 `MemorySaver`，D14 A3）；`graph.updateState` 的更新经各通道 `messagesStateReducer` 应用（checkpointer 语义），是架构上正确的落地（详见 `contracts/team-graph-contract.md` §5 偏离说明）。本 Decision 原结论保留作为历史记录。

来源：FR-018；survey §7.2；`projects/game/agent/src/context-middleware.ts`；`@langchain/langgraph` `REMOVE_ALL_MESSAGES`。

---

## D9. saolei MCP sink 接口：通用事件形状，不耦合 team mode

**Decision**: `createSaoleiMcpServer(bridge, boardApi?, sink?)` 增可选 `sink?: SaoleiEventSink` 参数。接口仅定义事件形状（`onGameStart`/`onMove`/`onGameEnd`，`onGameEnd` 携带结构化 `status: "won"|"lost"`），**不引用 team/strategy/store/teamMemoryId**。默认 `undefined` 时行为零变化（FR-020）。recognize 后、`gameStatus` 变化时调 `sink?.onXxx()`。同步修 `mcp-host.ts:9` 过时注释。

```ts
export interface SaoleiEventSink {
  onGameStart(state: GameState): void | Promise<void>;
  onMove(tool: CellTool, x: number, y: number, state: GameState): void | Promise<void>;
  onGameEnd(state: GameState, status: "won" | "lost"): void | Promise<void>;
}
```

**Rationale**: FR-019/FR-020/FR-022。MCP 仅提供事件接口，不实现旁路、不知 team mode（宪法原则 II：解耦非打补丁）。team 侧注册 sink 实现，将事件写入 D7 的 ephemeral buffer（D6 步骤 3）。sink 回调抛错由 MCP handler 捕获隔离（不影响工具主流程，edge case）。

**Alternatives**: 无（survey §7.3 已论证此为唯一解耦方案）。

来源：FR-019..FR-022；survey §7.3；`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:581`（createSaoleiMcpServer）、`:253`（gameStatus）；`projects/game/agent/src/mcp-host.ts:9`（过时注释）。

---

## D10. 既有 turn/队列基础设施：重构为承载 team graph（非扩展单 agent）

**Decision**: 既有 `SessionAgent`/`SessionAgentStore`/`TurnLoop`/`OperationBridge` 按"移除旧码、添加新码"（directive ①）**重构为 team 形态**：`SessionAgent`→`SessionTeam`（持有 team graph 实例 + per-session ephemeral state buffer + StrategyStore 引用）；`TurnLoop` 的单飞/队列语义保留并复用（team 的一个 turn = 一次 graph invoke）；`OperationBridge` 保留（player 独占使用）；`AdapterFactory`/`AgentAdapter` 单 agent 路径移除，由 team graph builder 取代。具体内部结构由 `tasks.md` 各 phase 决定，本 plan 仅约束"重构非补丁"。

**Rationale**: directive ① 大规模重构。单 agent AdapterFactory 直接绑定一个 profile+model；team 需绑定多 agent（player/planner 各自 model）+ 共享 graph + 策略注入——结构不同，扩展旧路径会变补丁（违反宪法原则 II）。TurnLoop 的单飞/队列（spec 030）与 team 的 turn 语义兼容（一个 user 输入 → 一次 team turn），故保留复用。`gameEnded` 等控制状态在 team turn 内由条件边处理，不破坏单飞。

**Alternatives**:
- ⚠️ 保留 SessionAgent 并在其上堆叠 team 逻辑：违反 directive ① 与宪法原则 II（补丁化）。

来源：directive ①；宪法原则 II；`projects/game/agent/src/{session-agent,turn-loop,operation-bridge,llm}.ts`；survey §9。

---

## D11. agent 模板组织：`src/team/` 目录，以模板为边界

**Decision**: agent 新增 `projects/game/agent/src/team/` 目录承载 saolei 模板：`TeamGraph` builder、`TeamState` schema、`player`/`planner` 节点、`update_strategy` 工具、`teamSink` 实现、模板定义（导出 `TeamAgent` 描述 + 内嵌初始策略）。模板选择由路径段 `{template}`（typed enum）路由到对应 builder（当前仅 saolei）。

**Rationale**: 以"模板"为代码组织边界，为未来多模板扩展留位（一个模板一个子结构）。模板定义集中：agent 列表（D3）、初始策略（D4）、graph schema、tools/mcp 装配（FR-028 模板固定装配）。模板选择用 typed enum 路由（非 string 潜规则，directive ②）。

来源：FR-009/FR-028；directive ②；`projects/game/agent/src/` 现状。

---

## D12. AgentFrame agent 名称字段：clean break 重命名

**Decision**: `AgentFrame` 字段 7 由 `agent_profile_name`（string）**重命名为 `agent`**（string，语义=team 内 agent 名称，如 `player`）。clean break（无历史数据兼容，需求方确认），proto 字段号 7 复用、字段名改。

**Rationale**: FR-023。clean break 下无需保留旧字段；复用字段号 7 改名为 `agent`，语义清晰（team 内 agent 名称，非 profile 名称）。desktop 现有按 `agentProfileName` 合并消息的逻辑（`projects/game/desktop/frontend/src/App.svelte` `handleMessageParts`）改按 `agent` 归位（US4 范围）。

来源：FR-023；需求方确认 clean break；`projects/game/desktop/frontend/src/App.svelte`。

---

## D13. 未决项与留待 tasks.md 的实现细节

以下为**实现细节**（非设计决策），由 `tasks.md` 各 phase 在编码前确定，本 plan 不固化：

- proto 各 message 的具体字段号分配（`contracts/api-contract.md` 给出语义与关键字段，字段号由实现时 gazelle/protoc 流程确定，保留 reserved 卫生）。
- team graph 节点/边的具体 TS 实现（`contracts/team-graph-contract.md` 给出契约，实现由 task 完成）。
- planner 节点内部 `update_strategy` 的重试/降级（需求方修订 #6：重试归 planner 内部，graph 不调度重试）。
- sink 回调抛错的隔离方式（约束：不影响工具主流程）。
- desktop 未知 agent 名称的归位策略（约束：契约字段存在；建议归入默认/丢弃由 task 定）。
- agent mongo 客户端的具体接入（连接配置来源、连接池）——agent 新增 mongo 依赖（D4 修订）。

### D14. LangGraph API 假设（评审 #2）——已 spike 实测确认（RESOLVED）

### D14. LangGraph API 假设——已 spike 实测确认（RESOLVED）

原为评审 #2 的 pending 验证项，已由 spike `experimental/ts/team_graph_spike/`（testplan + fake-llm 端到端 + vitest 确定性验证）实测确认。实测版本：`@langchain/langgraph` 1.4.8 / `langchain` 1.5.4 / `@langchain/core` 1.2.3 / `@langchain/openai` 1.5.5。逐条结论（证据见 `experimental/ts/team_graph_spike/FINDINGS.md`）：

- **A1 `REMOVE_ALL_MESSAGES` ✅ confirmed**：sentinel 真实导出；返回 `{ messages: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })] }` 清空该 channel 全部 prior messages；per-channel 独立（清 `playerMessages` 不影响 `plannerMessages`，结构性独立）。→ D8/FR-018 成立。
- **A2 createAgent 内嵌外层图节点 ✅ confirmed**：player 节点调 `createAgent.invoke()`，内部 tool loop 跑到 LLM 自停，返回后外层读 `gameEnded` 路由。**关键：createAgent 不带自身 checkpointer（`checkpointer?` 可选），单次 invoke 跑完整 agent loop；消息历史由外层单一 `MemorySaver` 持有。** → D6 "player=createAgent 全 loop" 成立。
- **A3 单 TeamState + per-agent 通道 ✅ confirmed（架构 i 成立）**：单一外层 `MemorySaver` 即可序列化 `playerMessages`+`plannerMessages`+`gameEnded`（`getState().values` 一次取回）；per-agent 历史从单一 checkpointer 重建，**无需分离 createAgent**（架构 ii 更重且非必要）。→ D5 维持。
- **A4 middleware 钩子面 ✅ confirmed**：6 个钩子 = `beforeAgent`/`beforeModel`/`wrapModelCall`/`afterModel`/`wrapToolCall`/`afterAgent`。**无独立 `afterTool`，`wrapToolCall` 包揽其职且更强**。middleware 可返回 `REMOVE_ALL_MESSAGES`。→ RefreshTeam 落地点 = 现有 `context-middleware.ts` 的 `beforeModel`（D8）。
  > **实现结果（2026-07-30，用户已接受偏离）**：RefreshTeam 最终**未**落地于 `beforeModel` 钩子，而是经外层 `graph.updateState` 直清两通道（`context-middleware.ts` 的 `refreshTeamChannels`，per-channel `RemoveMessage(REMOVE_ALL_MESSAGES)`，A1）。根因：createAgent 不带自身 checkpointer（A2），其 middleware 无法触达外层通道（A3）；`updateState` 走 channel reducer，是正确落地。详见 `contracts/team-graph-contract.md` §5 偏离说明。
- **A5 ChatOpenAI→fake-llm 端到端 ✅ confirmed**：`guitar run` 全绿，player/planner 各产出消息、strategy 写入。
- **A6 结构化 flag 条件路由 ✅ confirmed**：条件边读非 messages 字段 `gameEnded` 正确路由（非 null→planner、null→END）。

**实现期注意事项（须写入 `tasks.md` 对应 phase，避免踩坑）**：

1. **`TeamState` 必须用 `Annotation.Root`，不要用 `new StateSchema`+zod**：实测在 zod 3.25.76 + langgraph 1.4.8 下，`z.string().nullable().default(null)` 不满足 `SerializableSchema`（缺 `jsonSchema`）。正确写法：`Annotation<GameEnded>({ reducer: overwrite, default: () => null })`；消息通道 `Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] })`。
2. **declaration/TS2883 坑**：导出 `Annotation.Root`/`StateSchema` 常量或 `CompiledStateGraph` 返回类型会触发 TS2883（langgraph dual-package CJS 路径不可命名）。生产 `projects/game/agent` 包当前 emit declaration——采用 `Annotation.Root` 时须将 `TeamState` 保持模块私有、仅导出 `typeof TeamState.State`，或在导出边界加类型标注。
3. **`gameEnded` 终值**：planner 跑完会被清成 `null`（D6 step 6）。测试断言"局结束"应检查 planner 是否运行过（`plannerMessages` 非空），而非终态 `gameEnded`。
4. **createAgent 不带自身 checkpointer**：player/planner 的 createAgent 编译时不传 checkpointer；消息持久化由外层 `MemorySaver`（compile 外层图时传入）统一承载。

**实现范本**：`experimental/ts/team_graph_spike/src/team-graph.ts`（spike 已验证可行的最小 team graph 骨架，实现时参考其 `Annotation.Root` state、createAgent-as-node、条件边、middleware 写法）。

**Constitution 合规**：所有决策遵循原则 I（引用溯源）、II（重构非补丁）、III（接口优先）、V（编码前阅读——本节 API 假设已实测）、VI（大型测试验收）。
