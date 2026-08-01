# Data Model: Team Template Mode (StateGraph 升级)

**Feature**: `031-team-template-mode` | **Date**: 2026-07-29 | **Spec**: [`spec.md`](./spec.md) | **Plan**: [`plan.md`](./plan.md)

> 实体与状态模型。Proto 资源/消息的精确契约见 [`contracts/api-contract.md`](./contracts/api-contract.md)；team graph 状态/节点/边见 [`contracts/team-graph-contract.md`](./contracts/team-graph-contract.md)。本文件聚焦实体语义、字段、关系、存储与生命周期。

---

## 1. API 资源实体（proto 层）

### 1.1 Template（路径段，无资源消息）

| 属性 | 类型 | 说明 |
|---|---|---|
| 枚举值 | `enum Template { TEMPLATE_UNSPECIFIED=0; TEMPLATE_SAOLEI=1; }` | typed 枚举，仅作路径段与字段值（D2）。当前仅 `saolei`。无 CRUD/List RPC。 |

- **身份/唯一性**：枚举常量，非资源 id。
- **关系**：是 API 资源层级的根；Session、TeamProfile 挂在其下。
- **生命周期**：无（代码常量，随发布变更）。

### 1.2 Session

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string (IDENTIFIER) | `templates/{template}/sessions/{session}` |
| `template` | `Template` (typed enum) | 该 session 所属模板 |
| `session_id` | string (OUTPUT_ONLY) | 服务端生成 |
| `create_time` | Timestamp (OUTPUT_ONLY) | |

- **关系**：属于一个 Template；内含一个 Team。
- **生命周期**：Create → Get/List → Delete（移除顶层 `sessions/*`，clean break）。
- **存储**：session 服务既有存储（本特性不改其存储，仅改资源路径/层级）。

### 1.3 Team（取代 Agent）

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string (IDENTIFIER) | `templates/{template}/sessions/{session}/team` |
| `agents` | `repeated TeamAgent` | team 内 agent 描述（来自模板 graph schema，typed） |
| `create_time` | Timestamp (OUTPUT_ONLY) | |

**TeamAgent**:

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string | agent 名称（如 `player`/`planner`），模板 schema 已知 |
| `accepts_user_input` | bool | 是否接受用户输入（FR-031）；saolei: player=true, planner=false |

- **关系**：属于一个 Session；含若干 TeamAgent；TeamAgent 非独立资源（消息分区/frame 归位维度）。
- **生命周期**：Team 随 Session 连接（Connect）而存在；不可独立创建。
- **来源**：`agents` 由 agent 服务从模板定义（code）导出。

### 1.4 Message（按 agent 分区）

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string (IDENTIFIER) | `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}` |
| `message_id` | string (OUTPUT_ONLY) | |
| `sender` | `FrameSender` | USER/AGENT/SYSTEM |
| `agent` | string | 该消息所属 team 内 agent 名称（与路径 `{agent}` 一致） |
| `create_time` | Timestamp | |
| `content` | `MessageParts` | 显示块（display only），与 live frame 同形 |

- **关系**：属于一个 Session 内某 agent（路径段 `{agent}`）。
- **唯一性**：会话级隔离（以 session id 为作用域，FR-005）；按 agent 二级分区。
- **生命周期**：由 checkpoint state 重建（历史）；`RefreshTeam` 清空对应短期记忆（不直接删 Message 资源记录，而是清空 checkpointer 通道，详见 §2）。

### 1.5 TeamProfile（取代 AgentProfile；oneof 特化）

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string (IDENTIFIER) | `templates/{template}/profiles/{profile}` |
| `template` | `Template` (typed enum) | 与 oneof 变体一致（handler 校验） |
| `create_time` / `update_time` | Timestamp | |
| `spec` | `oneof` | typed 模板特化配置（D1） |

**SaoleiProfile**（`spec.saolei`）:

| 属性 | 类型 | 说明 |
|---|---|---|
| `player_model` | string | player LLM 模型选择（{provider}/{model}） |
| `planner_model` | string | planner LLM 模型选择 |

- **关系**：属于一个 Template。
- **约束**：saolei 的 TeamProfile **仅**含 player/planner 模型；不含 tools/mcp/skill 字段（FR-027，由模板固定装配 FR-028）。
- **存储**：MongoDB `team_profiles` 集合（prompt 服务 mongo 仓储）。

### 1.6 AgentFrame（传输信封，字段变更）

| 属性 | 类型 | 说明 |
|---|---|---|
| `session_id` | string (REQUIRED) | |
| `frame_id` | string | |
| `create_time` | Timestamp | |
| `sender` | `FrameSender` | |
| `agent` | string | **取代 `agent_profile_name`**（D12）；team 内 agent 名称 |
| `payload` | `oneof` | `message_parts`（display）/ `flow_parts`（control），不变 |

---

## 2. 运行时记忆与状态实体（非 proto 资源）

### 2.1 Strategy（策略，长期记忆）

| 属性 | 类型 | 说明 |
|---|---|---|
| `session_id` | string | 键 = session id（teamMemoryId，FR-013） |
| `content` | string | 策略文本（planner 经 `update_strategy` 写入；player 作为"当前态势"读取）；**初始无记录 → `get` 返回 `""`**（#3） |
| `update_time` | Timestamp | 最近更新时间 |

- **存储**：MongoDB `strategies` 集合，**由 agent 服务自身持久化**（mongo-backed memory store，D4 修订 #5）；**不经 prompt 服务**。
- **访问**：agent 经 `StrategyStore` 接口（`get`/`put`，DI）直连 mongo。agent 内部 memory，非公开 REST 资源、无 gRPC 中转。
- **初始值**：无记录时 `get` 返回空字符串 `""`（#3）；策略内容由 planner 首次 `update_strategy` 写入（无"模板内嵌初始策略"）。
- **生命周期**：跨局累积、跨 turn 持久；`RefreshTeam` **不影响**策略（FR-018）。session 删除**暂不**级联清理（#7）。
- **mongo 文档形状**（BSON）：`{ _id, session_id (unique), content, update_time, create_time }`。

### 2.2 Short-term messages（短期记忆）

| 属性 | 类型 | 说明 |
|---|---|---|
| `playerMessages` | `MessagesValue` | player 的对话/工具消息（team state 通道，D5） |
| `plannerMessages` | `MessagesValue` | planner 的复盘消息 |

- **存储**：内存 checkpointer（`MemorySaver`，per-session thread）。
- **生命周期**：`RefreshTeam`（FR-018）经 `REMOVE_ALL_MESSAGES` 清空**两个通道**；策略（§2.1）不受影响。
- **与 Message 资源关系**：Message 资源（§1.4）历史重建从这两个通道按 agent 读取。

### 2.3 GameState / GameEvent buffer（ephemeral，非"记忆"）

| 属性 | 类型 | 说明 |
|---|---|---|
| `gameState` | `GameState` | 最新识别棋盘状态（sink `onMove`/`onGameEnd` 写入） |
| `gameEvent` | `{ state, status: "won"|"lost", endedAt, consumed }` | 最近局结束事件（sink `onGameEnd` 写入） |

- **存储**：进程内 per-session ephemeral buffer（普通对象/Map，D7）。**非** LangGraph Store、**非** mongo、**非** checkpointer。
- **生命周期**：一个 team turn 内有效（player 落子→sink 写→player wrapper 读→planner 读→标记 consumed）。下次落子覆盖。`RefreshTeam` 不刻意清空（非"记忆"，语义上由后续落子覆盖）。
- **生产者**：team sink 实现（D9）。
- **消费者**：player 节点 wrapper（读 gameEvent → 写 `TeamState.gameEnded`）；planner 节点（读 gameState 复盘）。

### 2.4 TeamState（graph state schema）

| 通道 | 类型 | reducer | 说明 |
|---|---|---|---|
| `playerMessages` | `MessagesValue` | `messagesStateReducer`（含 REMOVE_ALL） | D5 |
| `plannerMessages` | `MessagesValue` | `messagesStateReducer` | D5 |
| `gameEnded` | `"won" \| "lost" \| null` | 覆盖 | 结构化局结束标志（D6）；planner 处理后置 null |

- 策略**不在** graph state（在 StrategyStore，§2.1，代码层注入 prompt）。
- gameState/gameEvent**不在** graph state（在 ephemeral buffer，§2.3）。

---

## 3. 组件实体

### 3.1 SaoleiEventSink（MCP 旁路事件接口）

| 方法 | 签名 | 触发时机 |
|---|---|---|
| `onGameStart(state)` | `(GameState) => void\|Promise<void>` | `saolei_init` 后 |
| `onMove(tool, x, y, state)` | `(CellTool, number, number, GameState) => ...` | 每次落子工具后 |
| `onGameEnd(state, status)` | `(GameState, "won"\|"lost") => ...` | `gameStatus` 变为 won/lost 时（结构化） |

- **契约**：仅定义事件形状；不引用 team/strategy/store/teamMemoryId（FR-019）。
- **默认**：`sink?` 可选，未传时行为零变化（FR-020）。
- **生产者**：saolei MCP handler（recognize 后调）。
- **消费者**：team 侧 sink 实现（写入 §2.3 buffer）。
- 详见 [`contracts/saolei-sink-contract.md`](./contracts/saolei-sink-contract.md)。

### 3.2 StrategyStore（agent 侧接口，DI）

| 方法 | 签名 | 说明 |
|---|---|---|
| `get(sessionId)` | `=> Promise<string>` | 取策略；无记录返回空字符串 `""`（#3） |
| `put(sessionId, content)` | `(string) => Promise<void>` | 写策略（planner `update_strategy` 调用） |

- **生产 impl**：agent 直连 mongo（mongo-backed memory store，D4 修订 #5）。team graph 依赖此接口（DI，便于测试用 fake）。
- 详见 [`contracts/strategy-store-contract.md`](./contracts/strategy-store-contract.md)。

---

## 4. 实体关系总览

```text
Template (enum, path segment)
  ├── Session: templates/{template}/sessions/{session}
  │     └── Team: .../team  { agents: [TeamAgent{name, accepts_user_input}] }
  │           ├── player (accepts_user_input=true)  ── 独占 saolei MCP / 桌面控制
  │           └── planner (accepts_user_input=false) ── 每局结束触发 / update_strategy
  ├── Message: .../team/agents/{agent}/messages/{message}   (按 agent 分区)
  └── TeamProfile: templates/{template}/profiles/{profile}  { oneof spec: SaoleiProfile{...} }

运行时（非资源）:
  Strategy ── MongoDB (strategies, key=session_id) ── player 读(当前态势) / planner 写(update_strategy)
  Short-term messages ── MemorySaver checkpointer (playerMessages/plannerMessages) ── RefreshTeam 清空
  GameState/GameEvent buffer ── 进程内 ephemeral (per session) ── sink 写 / player+planner 读
```

## 5. 移除的实体（clean break）

- `Agent` 资源（`sessions/{session}/agent`）→ 由 Team 取代。
- `AgentProfile` 资源（`prompts/agentProfiles/*`）+ 其 CRUD RPC → 由 TeamProfile（oneof）取代。
- `Skill` 资源（`prompts/skills/*`，API 管理的自定义 skill）+ 其 RPC → 移除。MCP 配套内置 skill（`projects/game/agent/src/skill/`）**保留不受影响**（FR-007）。
- `RefreshAgent` → `RefreshTeam`。
- `AgentFrame.agent_profile_name` → `agent`。
