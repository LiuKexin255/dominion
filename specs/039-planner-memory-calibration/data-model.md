# Data Model: Planner 长期记忆与校准指令

**Feature**: `039-planner-memory-calibration` | **Date**: 2026-08-07 | **Spec**: [`spec.md`](./spec.md) | **Plan**: [`plan.md`](./plan.md)

> 实体与状态模型。Proto 资源/消息精确契约见 [`contracts/memory-service-contract.md`](./contracts/memory-service-contract.md)；team graph 状态/节点见 [`contracts/team-graph-contract.md`](./contracts/team-graph-contract.md)；saolei_operate 见 [`contracts/saolei-operate-contract.md`](./contracts/saolei-operate-contract.md)。本文件聚焦实体语义、字段、关系、存储与生命周期。

---

## 1. API 资源实体（proto 层，新增）

### 1.1 Memory（planner 长期记忆条目，新增）

| 属性 | 类型 | 说明 |
|---|---|---|
| `name` | string (IDENTIFIER) | `templates/{template}/sessions/{session}/memories/{memory}` |
| `memory_id` | string (OUTPUT_ONLY) | `{memory}` 段，LLM 提供（FR-008） |
| `content` | string | 记忆内容文本 |
| `create_time` | Timestamp (OUTPUT_ONLY) | |
| `update_time` | Timestamp (OUTPUT_ONLY) | |

- **身份/唯一性**：资源名（`memories/{memory_id}` 在 session 作用域内唯一）。唯一键 `(template, session, memory_id)`。
- **关系**：属于一个 Session（`templates/{template}/sessions/{session}`）；为该 session 的 planner 长期记忆。
- **存储**：MongoDB `game_memory` 数据库（`style/mongo.md`，独立于 agent/prompt 的库），`memories` 集合，由新建 memory 服务（grpc-go）管理。
- **生命周期**：经 `CreateMemory`（memory_add）/`UpdateMemory`（memory_update）/`DeleteMemory`（memory_remove）增删改；跨进程重启持久。`ListMemories` 供 agent 烘焙冻结快照。session 删除暂不级联清理（与 031 strategy 决策对齐）。

---

## 2. 运行时记忆与状态实体（非 proto 资源）

### 2.1 planner 冻结记忆快照（frozen snapshot，调研 D2/D5）

| 属性 | 类型 | 说明 |
|---|---|---|
| `entries` | `{ memory_id: string, content: string }[]` | 冻结的记忆条目（来自 `ListMemories`） |
| `bakedAt` | `number`（ms） | 最近一次烘焙时间戳（retain-vs-rebuild 优化用，调研 §4.2） |

- **存储**：进程内（`projects/game/agent/src/team/memory-snapshot.ts` 冻结缓存），非持久。
- **生命周期**：team 初始化时首次烘焙；冻结期间 memory 工具改存储不刷新快照；压缩边界（037 每 5 局）刷新（重读 `ListMemories` → 重新烘焙）。
- **注入形式**：作为 planner 每次 invoke 的 input SystemMessage（内容来自冻结缓存，每条 `memory_id: 内容`，FR-011），**不烘焙进 createAgent systemPrompt**（调研 D5 方案 b）。

### 2.2 Calibration Instruction（planner→player 校准指令）

| 属性 | 类型 | 说明 |
|---|---|---|
| `content` | string | 指令文本（planner LLM 产出） |
| `scenario` | `review` \| `init` \| `compact` | 产出场景（FR-019） |

- **投递形态**：`HumanMessage` 进 `playerMessages` 通道（调研 D6）。
- **生命周期（两场景）**：
  - **review 场景**：planner 经 `instruct_player` 工具**可选**产出（携带游戏历史）；同 turn 内立即追加到 playerMessages（紧跟游戏结束 tool_result），player 继续。
  - **init/compact 场景**：planner 经 prompt 引导产出（无游戏历史，仅依冻结快照；LLM 决定是否调用 instruct_player，R4）；写入 `TeamState.pendingInstruction` 槽（§2.4），随 player 下次激活注入；不触发 player invoke。
- **压缩作用域**：review 指令在 playerMessages 内，会被 037 compress 节点压缩；init/compact 指令经 pending 槽注入，注入后同样进入 playerMessages 压缩作用域。planner 真正长期演化在冻结记忆快照（§2.1），不依赖 playerMessages 留存。

### 2.3 GameState / GameEvent buffer（ephemeral，不变+扩展）

031 既有 ephemeral buffer（`gameState`/`gameEvent`）。本特性扩展 sink：`onMove` → `onOperate(operations, finalState, ...)`，gameLog 以 `saolei_operate`（含全部 operations）为单位记录一条（FR-004）。其余不变。

### 2.4 TeamState（graph state schema，更新）

| 通道 | 类型 | reducer | 说明 |
|---|---|---|---|
| `playerMessages` | `MessagesValue` | `messagesStateReducer` | 031 既有；指令 HumanMessage 经此追加 |
| `plannerMessages` | `MessagesValue` | `messagesStateReducer` | 031 既有；复盘输出在此（对 player 不可见） |
| `gameEnded` | `"won"\|"lost"\|null` | 覆盖 | 031 既有 |
| `gameCounter` | `number` | 覆盖 | 037 既有（每 5 局触发 compress） |
| `pendingInstruction` | `string \| null` | 覆盖 | **新增**：init/compact 场景的待注入指令（D10） |

- **移除**：策略相关字段——策略不在 graph state（031 既有，本特性彻底移除 StrategyStore）。
- player 节点入口读 `pendingInstruction`：非空则注入 playerMessages（作为 HumanMessage）后清空（FR-015/FR-016 的"与下次激活一同注入"实现）。

---

## 3. 工具实体

### 3.1 memory mcp 工具（planner 专属，FR-008/FR-009）

| 工具 | 参数 | 对应 MemoryService RPC | 说明 |
|---|---|---|---|
| `memory_add` | `memory_id`, `content` | `CreateMemory` | 新建；memory_id 已存在 → 错误反馈 LLM |
| `memory_update` | `memory_id`, `content` | `UpdateMemory` | 改既有；不存在 → 错误 |
| `memory_remove` | `memory_id` | `DeleteMemory` | 删既有；不存在 → 错误 |

- template/session 经 memory mcp server path 闭包注入（工具参数不含，FR-012）。
- 仅 planner 持有；player 不持有（FR-009）。
- 工具改存储**不刷新冻结快照**（§2.1）。

### 3.2 instruct_player 工具（planner 专属，FR-014/FR-017）

| 工具 | 参数 | 行为 |
|---|---|---|
| `instruct_player` | `content: string` | 将 content 作为 HumanMessage 追加到 playerMessages |

- review 场景 planner 按 prompt"必要时才调用"（可选）；init/compact 场景经 prompt 引导产出（LLM 决定，R4）。
- 仅 planner 持有。

### 3.3 saolei_operate 工具（player 专属，FR-001/FR-002）

| 工具 | 参数 | 行为 |
|---|---|---|
| `saolei_operate` | `operations: [{type: click\|flag\|chord, x, y}]` | 按序校验执行，单次返回最终棋盘+状态；失败按原因细分（§saolei-operate-contract） |

- 合并原 `saolei_click`/`saolei_flag`/`saolei_chord_click`。`saolei_init`/`saolei_remain` 不变。仅 player 持有。

---

## 4. 服务实体（新增）

### 4.1 MemoryService（grpc-go，新建）

- **职责**：承载 planner 长期记忆条目的持久化与读写（Memory 资源 CRUD）。
- **存储**：MongoDB `game_memory` 数据库，`memories` 集合（独立，`style/mongo.md`）。
- **访问**：agent 经 `memory-client.ts`（gRPC，`dominion:///game/memory:50051`）访问；memory mcp server 经此转发（mcp 不直连，FR-007）。
- **结构**：仿 `projects/game/prompt/`（cmd/handler/domain/runtime/mongo）。
- 详见 [`contracts/memory-service-contract.md`](./contracts/memory-service-contract.md)。

### 4.2 memory mcp server（agent 上，新建）

- **职责**：向 planner 暴露 memory_add/update/remove 工具，转发到 memory 服务。
- **装配**：与 saolei mcp 同一 per-session mcp host，独立 McpServer 实例 + 独立 path/namespace。
- **path 闭包**：`createMemoryMcpServer(memoryClient, template, session)` 绑定 template/session（FR-012）。
- 详见 [`contracts/memory-mcp-contract.md`](./contracts/memory-mcp-contract.md)。

---

## 5. 实体关系总览

```text
Template (resource message, path segment) — 031 既有
  ├── Session: templates/{template}/sessions/{session} — 031 既有
  │     └── Team: .../team { agents: [player, planner] } — 031 既有
  │           ├── player (saolei_operate/init/remain; 不持有记忆/策略工具)
  │           └── planner (memory_add/update/remove + instruct_player; 持冻结记忆快照)
  └── Memory: .../sessions/{session}/memories/{memory} — 【新增，memory 服务 grpc-go, db game_memory】

运行时（非资源）:
  planner 冻结记忆快照 ── 进程内冻结缓存（memory-snapshot.ts）── 压缩/初始化边界刷新（ListMemories）
  Calibration Instruction ── HumanMessage 进 playerMessages（review 同 turn / init-compact 经 pendingInstruction 槽）
  Short-term messages ── MemorySaver checkpointer（playerMessages/plannerMessages）── RefreshTeam/压缩清空
  GameState/GameEvent buffer ── 进程内 ephemeral ── sink（onOperate）写 / player+planner 读

被移除（clean break）:
  Strategy / StrategyStore / strategies 集合（game_prompt 库）/ update_strategy 工具 / player"当前态势"注入 / planner system 策略注入
```

## 6. 移除的实体（clean break，FR-013）

- **Strategy**（共享策略长期记忆）→ 由 planner 冻结记忆快照（§2.1）+ Calibration Instruction（§2.2）取代。
- **StrategyStore**（`get`/`put` 接口 + `MongoStrategyStore`，`projects/game/agent/src/strategy-store.ts`）→ 删除。
- **`strategies` 集合**（agent `game_prompt` 库）→ 废弃（不迁移）。
- **`update_strategy` 工具**（`projects/game/agent/src/team/update-strategy.ts`）→ 由 `instruct_player` 取代。
- **player"当前态势"注入**（`buildStrategyMessage`/`STRATEGY_MESSAGE_ID`）→ 删除（player 不再读策略）。
- **planner system 策略注入**（`buildStrategyMessage`）→ 由冻结记忆快照（input SystemMessage）取代。
