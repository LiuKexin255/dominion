# Research: Planner 长期记忆与校准指令

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](./spec.md)

> Phase 0 设计决策。核心调研依据为 `survey/planner-memory-and-agent-communication.md`（下称"调研"），本文聚焦落地决策与对既有代码的影响。所有外部引用附完整 URL，仓库内引用附相对路径。

---

## D1. memory 服务架构（grpc-go，独立数据库，仿 prompt 服务）

### 决策

新建 grpc-go `MemoryService`，目录结构与脚手架完全复刻 `projects/game/prompt/`（`cmd/main.go` + `handler/` + `domain/` + `runtime/mongo/`，`style/golang.md` §目录结构）。连接既有 `game/mongo` 实例（`mongo.NewClient("game/mongo")`），使用**独立数据库 `game_memory`**（`style/mongo.md`：每服务独立数据库，MUST NOT 与 agent 的 `game_prompt` 或 prompt 的库混用）。

### Rationale

1. **复用成熟脚手架**：prompt 服务已验证 `bootstrap`/`grpc`/`mongo`/`otel` 组装方式（`projects/game/prompt/cmd/main.go`），memory 服务同款依赖、同款生命周期，零新依赖模式。
2. **独立数据库**：`style/mongo.md` 明确"每个服务应当有自己的数据库存放本服务的集合"。memory 服务的 `memories` 集合放入 `game_memory`，与 agent 的 `game_prompt.strategies`（即将移除）、prompt 的 `game_prompt.team_profiles` 隔离。
3. **grpc-go 而非 TS**：需求方 directive（FR-006）。Go 服务与 prompt/session 一致，复用 `protoc-gen-go-aip` codegen 与 `dominion/common/gopkg` 体系。

### Alternatives considered

- ❌ memory 逻辑内嵌 agent（TS）：违反需求方 directive（独立 grpc-go 服务），且 agent 是 TS、memory 需独立伸缩/重启。
- ❌ 共用 agent 的 `game_prompt` 库：违反 `style/mongo.md` 服务隔离。

### 部署

`projects/game/deploy.yaml` 新增 `memory` 服务条目（artifact `//projects/game/memory/service.yaml`），与 session/proxy/prompt/agent 平级。gameconst 新增 `MemoryTarget = "game/memory:grpc"`（`projects/game/pkg/gameconst/const.go`，仿 `PromptTarget`）。TS memory-client 目标 `dominion:///game/memory:50051`（仿 `prompt-client.ts` `PROMPT_SERVICE_TARGET`）。

---

## D2. MemoryService proto 契约（资源 / RPC / 字段）

### 决策

在 `projects/game/game.proto` 新增 `Memory` 资源消息 + `MemoryService`（AIP 风格，`style/api.md`）：

- 资源 pattern：`templates/{template}/sessions/{session}/memories/{memory}`（FR-012，集合段复数 `memories`）。`google.api.resource` 注解驱动 `protoc-gen-go-aip` codegen（`ParseMemoryName` 等，无需手写 parent 解析，同 031 §5）。
- `Memory { string name; string memory_id; string content; google.protobuf.Timestamp create_time; google.protobuf.Timestamp update_time; }`。
- RPC（标准方法，AIP-133/134/135/132）：
  - `CreateMemory`（add；请求带 parent=session 资源名 + `memory_id` + `content`；`memory_id` 已存在 → `ALREADY_EXISTS`，FR-008 冲突拒绝）。
  - `UpdateMemory`（update；带 `name` + `content`；不存在 → `NOT_FOUND`）。
  - `DeleteMemory`（remove；带 `name`；不存在 → `NOT_FOUND`）。
  - `ListMemories`（带 parent=session；返回该 session 全部 memory，供 agent 烘焙冻结快照；非 LLM 工具，agent 内部调用）。

详见 [`contracts/memory-service-contract.md`](./contracts/memory-service-contract.md)。

### Rationale

1. **memory_id 为资源 id**：`{memory}` 段即 LLM 提供的 `memory_id`（FR-008）。CreateMemory 以 parent+memory_id 为键，重复 → ALREADY_EXISTS，使 add 与 update 语义区分（add=新建、update=改既有，FR-008 Assumptions）。
2. **ListMemories 供快照烘焙**：冻结快照需一次读全部条目（调研 D5），ListMemories 是其数据源（agent 内部读，非 LLM 工具）。
3. **AIP 标准方法**：Create/Update/Delete/List 覆盖 add/update/remove + 快照读，无需 custom method（AIP-136）。

### Alternatives considered

- ❌ 单一 `UpsertMemory`（add+update 合并）：与需求方"add/update/remove 三独立工具 + memory_id"相悖，且 add 冲突须拒绝（FR-008）。
- ❌ 快照读走单独 batch RPC：标准 ListMemories（AIP-132）已够，无需 custom。

---

## D3. memory mcp server（agent 上，path 闭包注入，转发不直连）

### 决策

在 agent 新建 memory mcp server（`projects/game/agent/src/mcp/memory/memory-mcp.ts`），向 planner 暴露三个工具：`memory_add`/`memory_update`/`memory_remove`（均含 `memory_id` + `content`）。**template 与 session 经独立 mcp server 的 path 闭包注入**（同 saolei mcp 既有做法，FR-012）——即 `createMemoryMcpServer(memoryClient, template, session)` 闭包绑定，工具参数不含 template/session。工具实现经 `memory-client.ts`（仿 `prompt-client.ts`，`dominion:///game/memory:50051`）转发到 memory 服务。**mcp server 不直连 memory 服务**（FR-007）：mcp server → memory-client（agent 进程内 gRPC client）→ memory 服务。

memory mcp 与 saolei mcp 由 mcp-host（`projects/game/agent/src/mcp-host.ts`）按**每 mcp 独立 path** 提供（R3 修正——不共用单 path）；path **须含 `template` 字段**（template-scoped），形如 `/internal/mcp/{template}/{session}/saolei`（player）与 `/internal/mcp/{template}/{session}/memory`（planner），各为独立 McpServer 实例。装配：planner 的 tools = memory mcp 工具 + 指令发送工具（D6）；player 的 tools = saolei mcp 工具（saolei_operate 等）。

### Rationale

1. **path 闭包注入 template/session**：与 saolei mcp 一致（FR-012），工具签名干净（只需 memory_id/content），且 per-session 隔离。
2. **mcp 在 agent、转发到 memory 服务**：需求方 directive（FR-007）。agent 持 gRPC client（复用 `prompt-client.ts` 成熟模式：keepalive/round_robin/TLS），memory 服务无外部暴露面。
3. **每 mcp 独立 path（template-scoped）**：planner/player 工具集不同（FR-009/FR-010），每 mcp 一个 path（含 template 段），mcp host 按 (template, session, mcpKind) 懒创建独立 McpServer（R3）。既有 saolei mcp path（`/internal/mcp/{sessionId}`）同步迁移到 template-scoped 多 path 方案（clean break，tasks 落实）。

详见 [`contracts/memory-mcp-contract.md`](./contracts/memory-mcp-contract.md)。

### Alternatives considered

- ❌ mcp server 直连 memory 服务：违反 FR-007（mcp 不直连），且 mcp server 在 agent 进程、本就该经 agent 转发。
- ❌ memory 工具直接写在 planner 节点（不经 mcp）：需求方明确"memory mcp"（FR-007），且 mcp 化使工具描述自动注入 LLM。

---

## D4. planner 冻结记忆快照实现（调研 D2/D5）

### 决策

planner 长期记忆以**冻结快照**注入 planner 系统提示词（调研 D2 hermes frozen snapshot）：

- **不烘焙进 `createAgent` 的 `systemPrompt` 参数**（避免重建 createAgent 实例）——改为作为 **input SystemMessage 注入**，内容由一个**冻结缓存**持有（`projects/game/agent/src/team/memory-snapshot.ts`，调研 D5 方案 b）。
- **冻结期间不重读** memory 服务；刷新边界 = 压缩节点（调研 D4，同 037 每 5 局压缩）+ team 初始化（首次烘焙）。
- 刷新时调 `memory-client.ListMemories(session)` → 重新烘焙快照（每条 `memory_id: 内容`，FR-011）→ 更新冻结缓存。
- 可借鉴 hermes retain-vs-rebuild（调研 §4.2）：若 ListMemories 结果与缓存一致则跳过重建——是否落地留 plan（优化项，非阻塞）。
- memory 工具（add/update/remove）只改 memory 服务存储，**不刷新快照**（冻结语义）；变更在下一个刷新边界（压缩）才进入快照。

### Rationale

1. **冻结保 prefix cache**（调研 §3.2.3/§4.3）：跨多次复盘复用同一 system prompt 前缀，LLM provider 的 KV cache 命中。
2. **方案 b（input SystemMessage + 冻结缓存）**改动最小：无需重建 createAgent（planner 的 `systemPrompt` 仍为 base + 工具描述，不含记忆）；记忆作为每次 invoke 的 input SystemMessage，内容来自冻结缓存。
3. **压缩边界刷新**：复用 037 压缩节点（每 5 局）作为刷新契机；team 初始化时首次烘焙（无记忆则空快照）。

### Alternatives considered

- ❌ 每次 planner 激活重读 memory 服务刷新 system prompt：违反冻结语义（调研 D2），且增延迟、破坏 cache。
- ❌ 记忆烘焙进 createAgent systemPrompt：需在刷新时重建 createAgent 实例（调研 D5 已否决）。

---

## D5. 两场景节点拆分（review + init/compact，FR-019）

### 决策

team graph 拆分两个 planner 相关节点（FR-019，需求方 supplement 3 建议）：

```text
START → [initInstruction]（仅 team 初始化时）→ player
 player ──条件(gameEnded)──→ [review]（复盘 + 可选指令）──条件(gameCounter%5===0)──→ [compress] → END
                            │                                              └─ review 后非压缩 → player
                            └──(gameEnded=null)──→ END/player
 compress 后：[postCompactInstruction]（无历史、prompt 引导指令）→ END
```

- **review 节点**（正常游戏结束，`projects/game/agent/src/team/planner.ts` 改）：planner 复盘（携带 gameLog），按 prompt"必要时才调用"**可选**调用指令发送工具（D6）；不调用则不产生指令；graph 路由回 player（同 turn 继续，FR-017 消息顺序）。记忆冻结快照不变（不在 review 刷新）。
- **initInstruction / postCompactInstruction 节点**（新建，`team/instruction-node.ts`）：**无游戏历史**，prompt **要求** planner 给 player 指令（LLM 最终决定是否调用工具，R4 修正——无强制检验）；不触发 player invoke——init 时指令入 pending 槽随首次 player 激活注入（**异步**执行，`UpdateTeam(allow_missing=true)` 物化路径（graph 首建）后不等 LLM，R2 修正——原 CreateTeam 触发点被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede）；postCompact 时 turn 已结束（compress→END 前），指令入 pending 槽随下次激活注入（FR-015/FR-016）。
- 节点拆分使两场景的 prompt（带/不带 gameLog、prompt 强度"必要时 vs 要求"）、player invoke 语义（同 turn/不激活）清晰隔离。

### Rationale

1. **语义清晰**：两场景行为差异大（游戏历史有无、prompt 强度、是否激活 player），拆分节点避免单节点内多分支复杂度（宪法原则 II 简化）。
2. **prompt 驱动而非强制**：两场景产出指令与否最终均由 LLM 决定（R4 修正）；差异在 prompt 措辞（init/compact 要求给指令；review 必要时才调用）。节点不做"是否调用工具"的强制检验。
3. **复用 037 compress 节点**：compress 仍每 5 局触发、压缩后路由 END（037 既有）；postCompactInstruction 在 compress 之后、END 之前插入。

### Alternatives considered

- ❌ 单一 planner 节点处理三情形（review/init/compact）：分支多、prompt 拼接复杂。
- 注：init 节点在 team 创建时（`SessionTeam` 构造后）**异步**触发一次（见 D10，R2 修正）。

详见 [`contracts/team-graph-contract.md`](./contracts/team-graph-contract.md)。

---

## D6. 指令发送工具 + 消息顺序（FR-014/FR-017）

### 决策

新建指令发送工具 `instruct_player`（`projects/game/agent/src/team/instruction-tool.ts`，功能类似原 `update_strategy`，`projects/game/agent/src/team/update-strategy.ts`）：

- 工具签名：`instruct_player(content: string)`。
- 实现：将 content 作为 `HumanMessage` 追加到 **playerMessages 通道**（调研 D6 openclaw 式 HumanMessage）。在 review 节点中，planner 的 createAgent 持有该工具。**跨通道写入机制（R1 已决）**：planner 接收 gameLog 也是 HumanMessage（输入注入），指令发送与之对称——若 createAgent 子图内 tool 无法直接写外层 `playerMessages`，则经**外部 buffer 中转**（同 037 `emitChannelFrame` 的 configurable 暂存模式）：工具把指令暂存到 configurable 提供的槽 → planner 节点在 createAgent.invoke 返回后读暂存 → 由**节点返回值**写 `{playerMessages:[HumanMessage]}`（外层图通道）。
- **消息顺序保证**（FR-017）：review 节点在 planner 复盘后、graph 路由回 player 前执行；指令 HumanMessage 经 `messagesStateReducer` 追加到 playerMessages 末尾（紧随游戏结束 tool_result）。player 下次激活读 playerMessages 时顺序为 `tool_calling → tool_result → planner 指令 → player 下一条 output`。
- **planner 复盘对 player 不可见**：复盘输出在 plannerMessages（per-agent channel，031 §1），不进 playerMessages。
- review 节点中指令按 prompt"必要时才调用"（可选，LLM 决定）；init/compact 节点中 prompt 要求给指令（LLM 决定，R4——无强制检验，见 D5）。

### Rationale

1. **HumanMessage 进 playerMessages**（调研 D6）：transcript 可见（desktop player tab 能看到指令）、累积可引用、不与 player base system prompt 重复。
2. **工具化（非节点自动写）**：需求方 supplement 2（planner 决定是否发送，类似 update_strategy）。两场景均 LLM 决定；差异在 prompt 措辞（R4）。
3. **消息顺序由 reducer 自然保证**：messagesStateReducer 追加语义 + review→player 路由时序。
4. **外部 buffer 中转（R1）**：规避 LangGraph 子图 tool 无法直写外层通道的不确定性；与既有 gameLog HumanMessage 注入、037 emitChannelFrame 同一模式，零新机制风险。

### Alternatives considered

- ❌ 指令烘焙进 player system prompt：调研 §6.3 已否决（不可见、不可累积、与 base 重复）。
- ❌ review 自动每次写指令（非工具）：违反 supplement 2（可选）。

---

## D7. saolei_operate 批量工具 + 失败细分（FR-001/FR-002）

### 决策

合并 `saolei_click`/`saolei_flag`/`saolei_chord_click` 为 `saolei_operate(operations: [{type, x, y}])`（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`）：

- `type` ∈ {`click`/`flag`/`chord`}（枚举，非裸 string）。
- 按列表顺序依次 `validateMove` + dispatch（复用既有 `registerCellTool` 逻辑提取为按单 op 处理的内部函数）。
- **失败细分**（FR-002，澄清 Session 2026-08-07）：
  - 游戏结束（某 op 后 won/lost）：在该 op 停止，剩余不执行。
  - 无害空操作拒绝（`MoveRejection` 中 `cell_already_revealed`/`cell_is_flagged`/`cannot_flag_revealed`/`chord_requires_number`/`chord_no_unrevealed_neighbor`——不改变棋盘的 cell 状态拒绝）：跳过该 op，继续剩余。
  - 结构性/上下文拒绝（`out_of_bounds`/`no_active_game`）：在该 op 停止，剩余不执行。
- **单次返回**：一个 MCP 文本块，反映处理后的最终棋盘 + 游戏状态 + （若有）跳过/停止原因。
- `saolei_init`/`saolei_remain` 不变（FR-003）。
- **gameLog 以 operate 为单位**（FR-004）：sink 的 `onMove` 改为 `onOperate(operations, finalState, ...)`，gameLog 记一条含全部 operations 的项。planner 工具描述（`buildToolDescriptionSection`）改为描述 `saolei_operate` + click/flag/chord（FR-005）。

### Rationale

1. **减少往返**：LLM 批量落子只一次调用一次返回（需求方观察）。
2. **失败细分保实用性**：无害空操作（如在数字格 click）不中断批量，结构性错误停止避免级联（澄清 Session 2026-08-07）。
3. **MoveRejection 既有分类**：`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `MoveRejection` 已有完整原因码，映射到三类即可（FR-002 由 plan 落实精确映射）。

详见 [`contracts/saolei-operate-contract.md`](./contracts/saolei-operate-contract.md)。

### Alternatives considered

- ❌ 批量失败一律停止（不区分无害空操作）：澄清已否决（无害空操作应跳过继续）。
- ❌ 保留三个独立工具：违反批量化目标。

---

## D8. 移除 StrategyStore 的影响面（FR-013）

### 决策

移除 `projects/game/agent/src/strategy-store.ts`（`StrategyStore` 接口 + `MongoStrategyStore`）及全部引用：

- `projects/game/agent/src/server.ts`：移除 `createMongoClient`（strategy 用）/`MongoStrategyStore`/`strategies` 集合 wiring（注：agent 仍可能需 mongo client 用于其他？——本特性后 agent 不再直连 mongo，strategy 库 `game_prompt.strategies` 废弃）。
- `projects/game/agent/src/team/{player,planner}.ts`：移除 `strategyStore.get`/`buildStrategyMessage`/`STRATEGY_MESSAGE_ID` 注入与过滤。
- `projects/game/agent/src/team/update-strategy.ts`：删除（被 instruction-tool 取代）。
- `projects/game/agent/src/team/graph.ts`：移除 strategyStore 注入。
- 037 compress 节点：不受 strategy 影响（compress 只压 playerMessages/plannerMessages，不涉 strategy），无需改。
- 既有 `strategies` 集合数据：clean break 不迁移（FR-013 Assumptions）。

### Rationale

宪法原则 II（重构式）：strategy 共享记忆设计被 memory 服务 + 指令工具取代，须完整移除旧路径，不留孤儿引用（SC-005 代码审查验证）。

---

## D9. memory 工具 → MemoryService RPC 映射 + memory_id 冲突（FR-008）

### 决策

| mcp 工具 | MemoryService RPC | memory_id 语义 | 冲突/缺失 |
|---|---|---|---|
| `memory_add(memory_id, content)` | `CreateMemory(parent=session, memory_id, content)` | 新条目的资源 id（LLM 提供） | `memory_id` 已存在 → ALREADY_EXISTS（FR-008） |
| `memory_update(memory_id, content)` | `UpdateMemory(name=session/.../memories/{memory_id}, content)` | 定位既有条目 | 不存在 → NOT_FOUND |
| `memory_remove(memory_id)` | `DeleteMemory(name=...)` | 定位既有条目 | 不存在 → NOT_FOUND |

- 工具返回：memory 服务的 RPC 响应（成功/错误文本），LLM 据此决策。
- `memory_id` 由 LLM 提供（FR-008 Assumptions）；格式约束（如 `[a-z0-9_-]+`）由 plan 落实（AIP-122 资源 id 字符集）。

### Rationale

add/update/remove 三工具与 Create/Update/Delete 一一对应；memory_id 为资源 id 使定位明确（无需 hermes 的 substring 匹配，调研 §3.2.2）。冲突拒绝保证 add/update 语义区分。

---

## D10. SessionTeam 初始化触发 init 节点（FR-015）

### 决策

`SessionTeam` 物化（`SessionTeamStore.update`，040：`UpdateTeam(allow_missing=true)` 物化路径——原 `SessionTeamStore.create`（AIP-133 CreateTeam）触发点被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede）时，在 team graph **首建**后**异步**触发一次 **initInstruction 节点**（D5，R2 修正——`UpdateTeam(allow_missing=true)` 物化后即返回、不等 LLM）：planner 仅依冻结记忆快照（首次为空或既有），经 prompt 要求产出初始指令（LLM 决定是否调用 instruct_player，R4），写入 pending 指令槽（`TeamState.pendingInstruction`）。该指令随 player 首次激活（首次 user message → 首次 player invoke）一同注入 playerMessages（FR-015）。init 不触发 player invoke（UpdateTeam 响应不含 player 输出）。**profile 变更重建（040 FR-005）复用既有 checkpointer 重建 graph，不重跑 initInstruction（仅首建触发）。**

**与 desktop 状态同步（R2 关键问题，须 tasks 解决）**：
1. **agent typing 状态**：initInstruction 异步运行时，desktop 须正确进入 "agent typing"（planner 工作中）状态——经既有 channel-frame（`emitChannelFrame`，agent=planner）或 flow 状态下发；须协调 init 异步产出与 desktop Connect 时序（Connect 可能在 init 完成前/后建立）。
2. **user message 排序**：initInstruction 异步产出期间若 desktop 收到 user message，该 user message 须正确排在 planner 指令**之后**（player 首次激活时先注入 pending 指令、再处理 user message）——复用 030/038 TurnLoop 队列语义 + pendingInstruction 优先注入。

### Rationale

需求方 supplement 3：team 初始化时 player 指令历史为空，需一次无游戏历史的初始校准。R2 修正：`UpdateTeam(allow_missing=true)` 物化不阻塞等 LLM（异步，原 CreateTeam 触发点被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede），避免 gRPC RPC 顶 deadline；pending 槽 + TurnLoop 队列保证指令不丢失、且排在期间到达的 user message 之前。

### 待 plan 细化

- pending 指令槽的 state 字段（`TeamState.pendingInstruction: string | null`，last-write-wins）与 player 节点消费逻辑（player 入口读 pendingInstruction → 注入 playerMessages → 清空）。
- init 异步与 desktop Connect 的 typing-state 时序协调（init 完成在 Connect 前如何补发 typing/指令帧）。
- user message 在 init 期间的排队与"排在指令之后"的精确实现（TurnLoop 队列 + pending 注入顺序）。
- init 异步失败（planner model 不可用）的降级（不阻断 team；后续压缩边界自然重试/补齐）。

---

## 未解决的问题

所有设计未知项已在 D1–D10 决策。以下留 plan/tasks 细化（非阻塞）：

- `MoveRejection` 原因码 → 三类的精确映射（D7，FR-002）。
- memory_id 字符集与长度约束（D9，AIP-122）。
- 记忆条目上限值与达上限策略（参考 hermes 报错让 LLM consolidate，调研 §3.2.2）。
- pending 指令槽 state 字段与消费时序（D10）。
- hermes retain-vs-rebuild 优化是否落地（D4，非阻塞优化）。
- session 删除时 memory 级联清理（暂不清理，与 031 strategy 决策对齐）。

---

## 风险与缓解（plan 评审，2026-08-07）

> 用户评审指出的脆弱点与处置。R1/R6 已闭环；R2/R3/R4 已决方向、细节留 tasks；R5 时序在节点编排中保证。

| # | 风险 | 处置 |
|---|---|---|
| **R1** | `instruct_player` 跨通道写外层 `playerMessages`（createAgent 子图 tool 能否直写外层通道） | **已闭环**：planner 接收 gameLog 本就是 HumanMessage 注入，指令发送对称——采用**外部 buffer 中转**（configurable 暂存 + 节点返回值写外层通道，同 037 `emitChannelFrame`）。无需 spike（D6/contract §4）。 |
| **R2** | `UpdateTeam(allow_missing=true)` 物化同步触发 initInstruction 顶 gRPC deadline（原 CreateTeam 触发点，040 supersede） | **已决**：改为**异步**（`UpdateTeam` 物化即返回，仅 graph 首建触发；profile 变更重建不重跑 init）。须 tasks 解决与 desktop 的状态同步——desktop 正确进入 agent typing；异步期间到达的 user message 须排在 planner 指令之后（TurnLoop 队列 + pendingInstruction 优先注入，D10）。 |
| **R3** | memory mcp 与 saolei mcp 共用单 path 不可行 | **已决**：每 mcp 独立 path，path 含 `template`（template-scoped，`/internal/mcp/{template}/{session}/{saolei\|memory}`）。既有 saolei path 同步迁移（clean break，D3/contract）。 |
| **R4** | init/compact "强制"指令依赖 LLM 合规（软保证） | **已决**：取消强制检验。两场景产出指令与否均由 LLM 决定；差异在 prompt 措辞（init/compact 要求给指令；review 必要时才调用）。spec/contract/data-model 已同步改"强制"→"prompt 引导"。 |
| **R5** | 压缩→快照刷新→postCompactInstruction 时序 | 节点编排显式保证 `review → compress（清通道+刷新快照）→ postCompactInstruction → END`；contract §2.4/§2.3 已约束顺序。tasks 实现时按此序连边。 |
| **R6** | ListMemories 缺分页（违反 AIP-158/`style/api.md`） | **已闭环**：ListMemories 加分页（`page_size`/`page_token`/`next_page_token`，AIP-158），对齐 prompt 服务 ListTeamProfiles 默认/上限。contract/research 已改。 |

**遗留（低/已接受）**：memory_add 的 LLM 提供 memory_id（R7，需求方接受）；session 删除不清理 memory（R8，对齐 031）；新增第 6 服务的运维面（R9，directive 驱动，agent 侧移除 mongo 直连净简化）；进程内冻结快照缓存 + in-memory checkpointer/SessionTeamStore 不抗重启（R10，继承 031）。
