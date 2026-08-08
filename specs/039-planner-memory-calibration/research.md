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

在 agent 新建 memory mcp server（`projects/game/agent/src/mcp/memory/memory-mcp.ts`）。> ⚠️ **Session 2026-08-08 修订**：原"暴露三个工具 `memory_add`/`memory_update`/`memory_remove`（均含 `memory_id`）"已改为**单一 hermes 风格 `memory` 工具**（`action`/`content`/`old_text`/`operations`，无 `memory_id`/无 `target`），详见 D9/D11。下文 path 闭包 / mcp 不直连的拓扑结论不变。**template 与 session 经独立 mcp server 的 path 闭包注入**（同 saolei mcp 既有做法，FR-012）——即 `createMemoryMcpServer(memoryClient, template, session)` 闭包绑定，工具参数不含 template/session。工具实现经 `memory-client.ts`（仿 `prompt-client.ts`，`dominion:///game/memory:50051`）转发到 memory 服务。**mcp server 不直连 memory 服务**（FR-007）：mcp server → memory-client（agent 进程内 gRPC client）→ memory 服务。

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
- 刷新时调 `memory-client.ListMemories(session)` → 重新烘焙快照（**纯内容**，每条仅 `content`，FR-011/Session 2026-08-08）→ 更新冻结缓存。
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

## D9. memory 工具 → MemoryService 转换映射（Session 2026-08-08 修订：hermes 式单工具 + agent 转换）

> **修订**：原 D9（3 独立工具 `memory_add/update/remove` + LLM 提供 `memory_id` + 冲突拒绝）已被 Session 2026-08-08 推翻。memory 工具改为 hermes 式单一 `memory` 工具（`action`/`content`/`old_text`/`operations`，无 `memory_id`/无 `target`）；memory 服务存储 API 不变；agent 负责转换。调研依据 `research.md` D11（hermes `MEMORY_SCHEMA`）。

### 决策

planner 持有**单一** hermes 风格 `memory` 工具（参数 `action`∈{add/replace/remove}、`content`、`old_text`、`operations`）。**memory 服务存储 API 不变**（`MemoryService` Create/Update/Delete/List，资源 `templates/{template}/sessions/{session}/memories/{memory}`，`{memory}`=内部 memory_id）。agent 侧 `memory` 工具实现负责将 hermes 式调用转换为服务的 memory_id 式 RPC：

| `memory` 工具调用 | agent 转换 | MemoryService RPC | 匹配/冲突语义 |
|---|---|---|---|
| `action=add, content` | agent 内部生成 memory_id（非 LLM 提供） | `CreateMemory(parent=session, memory_id=<gen>, content)` | 等价 content 已存在 → 成功（去重，同 hermes "no duplicate added"） |
| `action=replace, old_text, content` | agent `ListMemories` → 按 `old_text` 子串匹配定位唯一条目 → 得其 memory_id | `UpdateMemory(name=.../memories/{memory_id}, content)` | 0 命中 → 错误文本（含当前条目助重选）；多不同条目命中 → 错误（要求更具体子串）；全相同 → 作用首条 |
| `action=remove, old_text` | 同上定位 memory_id | `DeleteMemory(name=...)` | 同上 0/多命中语义 |
| `operations=[...]`（批量） | 数组每项按上述单 op 转换，**原子**应用（全成功才提交，同 hermes apply_batch） | 多 RPC（事务/补偿由 plan 落实） | 任一 op 失败 → 整批不提交，返回错误 + 当前条目 |

- **无 `memory_id` 参数**：`memory_id` 退化为 memory 服务内部存储键，agent 在 `add` 时生成（如 slug/UUID，plan 落实生成策略），对 LLM 不可见。`replace`/`remove` 经 `old_text` 子串定位（ListMemories + substring match）。
- **无 `target` 参数**：dominion planner 仅单一记忆存储（无 hermes 的 memory/user 双存储），`target` 无意义。
- **注入耦合**：planner 系统提示词冻结快照为**纯内容**（无 `memory_id`，FR-011/D11），使 LLM 据内容用 `old_text` 子串定位——工具参数与注入格式自洽（D11 第三节）。
- 工具返回单一 MCP 文本（成功/错误文本，LLM 据此决策；错误非异常，031 C15 neutral status）。
- 改存储**不刷新冻结快照**（FR-010 冻结语义）。

### Rationale

1. **定位机制 ↔ 注入格式耦合**（D11 第三节）：hermes 注入纯内容→子串定位；dominion 既已选纯内容注入（Session 2026-08-08），工具定位必须用 `old_text` 子串，二者自洽。若保留 `memory_id` 参数而注入纯内容，LLM 无法获得 memory_id 来调用工具——自相矛盾。
2. **memory 服务 API 稳定**：变更仅限 MCP 工具面与 agent 转换层，memory 服务（proto/仓储/handler）零改动——降低本次修订的实现面（仅 agent 侧 memory-mcp.ts + memory-snapshot.ts 注入格式）。
3. **hermes 实践验证**：substring 匹配 + 错误回传 current_entries 的自纠错闭环在 hermes 已验证有效（调研 §3.2.2，[`memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py) `replace`/`remove`）。

### Alternatives considered

- ❌（原 D9）3 独立工具 + LLM 提供 memory_id + 冲突拒绝：已被 Session 2026-08-08 推翻（与纯内容注入不自洽——LLM 看不到 memory_id 却要用它定位）。
- ❌ 单工具但仍用 memory_id 定位（无 old_text）：需注入 `memory_id: 内容`，但 Session 2026-08-08 已定纯内容注入，矛盾。

### 待 plan 细化

- agent 生成 memory_id 的策略（UUID vs content-derived slug；slug 需处理冲突/长度，AIP-122 字符集）。
- `operations` 批量的原子性实现（memory 服务无跨 RPC 事务 → agent 侧补偿/先全验证后提交；dominion v1 无硬上限，批量动机弱，是否实现 operations 路径留 plan——单 op 路径已满足核心需求）。
- substring 匹配的精确语义（大小写敏感、`old_text` 最小长度、与 hermes 一致即 `old_text in entry` 子串包含）。

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

## D11. memory 工具的使用引导（hermes 调研 → dominion memory skill）

### 调研问题

hermes 的长期记忆工具（`memory` tool，actions = add/replace/remove）是否有配套的 skill 或提示词，向 LLM 说明**如何使用**以及**何时使用**？若有，参考其为 dominion 的 memory mcp 编写配套 skill。

### 调研结论：hermes **没有**独立的 memory skill；引导内嵌于工具描述 + 错误反馈

来源：[`tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py)（`MEMORY_SCHEMA`）、[Persistent Memory 文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)。模块 docstring 明示设计哲学（第 22 行）：*"Behavioral guidance lives in the tool schema description."*。

hermes 把记忆工具的全部 LLM 引导放在**两个面**，均非独立 SKILL.md：

1. **工具 schema description**（`MEMORY_SCHEMA["description"]`，OpenAI function-calling schema 内）——主引导面。是一段多段式富文本，含结构化 HOW / WHEN / IF FULL / TARGETS / SKIP 段落（见下"引导内容摘录"）。
2. **运行时错误字符串**——当记忆满或操作失败时，错误响应本身携带整合指令（如 `"Consolidate now: use 'replace' to merge overlapping entries into shorter ones or 'remove' stale or less important entries (see current_entries below), then retry this add — all in this turn."`）。
3. **系统提示词冻结记忆块头部**——显示容量占用（如 `MEMORY (your personal notes) [67% — 1,474/2,200 chars]`），让 LLM 感知容量。

> 注：hermes 的 [memory 文档页](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory) 的 "What to Save vs Skip" / "Capacity Management" 段是对 schema description 的人类向阐释，**不注入 LLM**——LLM 只看到 schema description + 错误串 + 冻结块头部。

### hermes 工具 schema description 引导内容摘录（verbatim，`MEMORY_SCHEMA["description"]`）

> Save durable facts to persistent memory that survive across sessions. Memory is injected into every future turn, so keep entries compact and high-signal.
>
> **HOW**: make ALL your changes in ONE call via an 'operations' array ... batch applies atomically and the char limit is checked only on the FINAL result ...（dominion 注：hermes 单 tool + action param + batch；dominion 为 3 独立 tool，batch 段不适用）
>
> **WHEN**: save proactively when the user states a preference, correction, or personal detail, or you learn a stable fact about their environment, conventions, or workflow. Priority: user preferences & corrections > environment facts > procedures. The best memory stops the user repeating themselves.
>
> **IF FULL**: an add is rejected with the current entries shown. Reissue as ONE batch that removes or shortens enough stale entries and adds the new one together.
>
> **TARGETS**: 'user' = who the user is (name, role, preferences, style). 'memory' = your notes (environment, conventions, tool quirks, lessons).
>
> **SKIP**: trivial/obvious info, easily re-discovered facts, raw data dumps, task progress, completed-work logs, temporary TODO state (use session_search for those). Reusable procedures belong in a skill, not memory.

### dominion 的差异与缺口

> ⚠️ **Session 2026-08-08 修订**：本节原描述的工具形态（3 独立工具 + memory_id）已被推翻——dominion 现采用 hermes 式**单一 `memory` 工具**（`action`/`content`/`old_text`/`operations`，无 `memory_id`，详见 D9）。以下"缺口"项已由 Session 2026-08-08 + 本 D11 决策（memory skill）+ FR-020 闭合。

- ~~**工具形态**：dominion 为 3 个独立 MCP 工具（`memory_add`/`memory_update`/`memory_remove`，均含 `memory_id`）~~ → **已改**：单一 hermes 式 `memory` 工具（D9），引导适配 `old_text` 子串定位（0/多命中报错），与 hermes 一致。
- **现有缺口**：本特性契约的工具 `description` 已落实（`contracts/memory-mcp-contract.md` §1-2）；planner 系统提示词的记忆使用引导由 **memory skill**（FR-020，本 D11 决策）承载。
- **dominion 特有的非直觉语义**（hermes 无对应，需引导）：**冻结快照**——planner 经 `memory` 工具改存储后，变更**不立即**进入其系统提示词，要到压缩边界才刷新（FR-010/D4）。若 LLM 不理解这一点，会误以为"刚 add 的记忆已生效"或反复 add 同一洞察。此语义是 hermes frozen snapshot 的 dominion 化（刷新边界 = 压缩，非 hermes 的下一会话），**必须**在引导中显式说明（memory skill §3.4）。
- **域适配**：hermes 的 WHEN/SKIP 是面向通用助手（用户偏好/环境事实）。dominion 的 planner 是扫雷复盘规划者，其记忆语义是**跨局累积的对 player 校准经验与自身复盘认知演化**（spec US2）。WHEN/SKIP 需改写为复盘域（如：保存"player 重复犯的错误模式"、"被验证有效的开局策略"；跳过"单局偶发失误"、"棋盘具体坐标"）。

### 决策：新建 `memory` SKILL.md（planner 专属），与 saolei skill 对称

参考 hermes 的引导**内容**（HOW/WHEN/SKIP/IF FULL），但按 dominion 既有 SKILL.md 机制承载（非 hermes 式内嵌工具描述）。理由：

1. **复用既有机制**：dominion 已有 `src/skill/{name}/SKILL.md` + `skill-loader.ts` 按 `mcp_names` 注入 systemPrompt 的成熟模式（saolei 先例，`specs/020-agent-resources-layout/contracts/skill-md-format.md`）。memory skill 与 saolei skill 对称——saolei skill 注入 player（player 持 saolei 工具），memory skill 注入 planner（planner 持 memory 工具，FR-009）。
2. **工具描述 vs skill 的分工**（与 saolei 一致）：MCP 工具 `description`（per-tool，简短，聚焦参数/行为）+ SKILL.md（系统提示词级，富文本，聚焦**整体使用哲学、何时/何类记忆、冻结快照模型**）。两者互补，非重复。
3. **冻结快照语义必须显式引导**：这是 dominion 特有、且 LLM 不可能从工具描述推断的非直觉行为，最适合放在 SKILL.md。
4. **hermes 内容可适配复用**：HOW（改 memory_id 语义）、WHEN（改复盘域）、SKIP（改复盘域）、IF FULL（dominion v1 不设硬上限，D2 已决，故此段改为"无硬上限、保持精炼"）。

**落地形态**：`projects/game/agent/src/skill/memory/SKILL.md`（遵循 `specs/020-agent-resources-layout/contracts/skill-md-format.md` frontmatter/body 契约）；`skill-loader.ts` 的 `BUILTIN_SKILL_NAMES` 注册 `"memory"`；`planner.ts` 组装 systemPrompt 时调 `appendSkillBodyToPrompt(base, ["memory"])`（当前 planner **不**调 appendSkillBodyToPrompt，T020 需补）。

详见新增契约 [`contracts/memory-skill-contract.md`](./contracts/memory-skill-contract.md)。

### Alternatives considered

- ❌ 照搬 hermes：把全部引导塞进 MCP 工具 description（不建 SKILL.md）。否决：dominion 既有 SKILL.md 机制就是为这类富引导设计的（saolei 先例），且冻结快照语义需要系统提示词级的连贯说明，分散在 3 个工具 description 里割裂、且无法解释"为何刚改的记忆没立刻生效"。
- ❌ 不加任何引导（仅工具 description 占位符）。否决：planner 无法理解冻结快照语义、不知该记什么，记忆质量退化；hermes 证实这类引导对记忆工具有效性关键。
- ❌ 把引导烘焙进冻结快照 SystemMessage。否决：冻结快照是**数据**（记忆条目），每局可能变；引导是**静态指令**，应随 systemPrompt 稳定（且保 prefix cache，D2）。

---

## D12. saolei_operate 双形态参数 + memory skill 注入装配（Session 2026-08-08）

### D12.1 saolei_operate 双形态参数（FR-001，augment）

#### 决策

`saolei_operate` 参数采用与 hermes `memory` 工具一致的双形态——调用方可传**普通参数**（单次操作 `type`∈{click/flag/chord} + `x`/`y`）**或** **`operations` 数组**（批量 `[{type,x,y},...]`）。两种形态二选一，语义等价（单次 = 长度 1 的 operations）。

```ts
saolei_operate(
  type?: OperationType,         // 单次形态
  x?: number, y?: number,       // 单次形态
  operations?: CellOperation[], // 批量形态
): MCPTextResult
```

#### Rationale

1. **与 hermes `memory` 工具形态对称**（Session 2026-08-08 directive）：hermes `memory` 工具为单 op（action/content/old_text）+ 批量（operations）双形态；saolei_operate 同构。统一两个工具的参数风格，降低 LLM 学习成本。
2. **单次操作更省**：player 大量场景是单次落子（揭示一格、标记一格），普通参数（3 字段）比包一层 `operations:[{...}]` 更轻；批量场景仍可用 operations。
3. **执行/返回语义不变**：无论单次或批量，均归一化为内部 operations 列表后按序执行、单次返回（FR-002 失败细分不变）。归一化在工具入口完成（单次 → `[{type,x,y}]`）。

#### Alternatives considered

- ❌ 仅保留 `operations` 数组（原 039 设计）：单次落子需包数组，冗余；且与 memory 工具形态不一致（Session 2026-08-08 要求对称）。
- ❌ 仅保留普通参数（单次）：失去批量化（本特性核心目标 SC-001）。

#### 待 plan 细化

- 同时提供 `type/x/y` 与 `operations`、或均不提供时的拒绝/优先规则（约束：二者语义等价；建议 `operations` 优先，或直接拒绝歧义调用——plan 决定）。
- 空 `operations` 列表行为（无操作返回当前状态 / 非法，spec Edge Case 约束不产生副作用）。

### D12.2 memory skill 注入装配（FR-020）

#### 决策

memory skill 经 dominion 既有 skill 机制注入 planner 系统提示词（与 saolei skill 注入 player 完全对称）：

1. 新建 `projects/game/agent/src/skill/memory/SKILL.md`（遵循 `specs/020-agent-resources-layout/contracts/skill-md-format.md` frontmatter/body 契约）。
2. `projects/game/agent/src/skill-loader.ts` 的 `BUILTIN_SKILL_NAMES` 注册 `"memory"`（当前仅 `"saolei"`）。
3. `projects/game/agent/src/team/planner.ts` 组装 systemPrompt 时调 `appendSkillBodyToPrompt(base, ["memory"])`（当前 planner **不**调 `appendSkillBodyToPrompt`——T020 需补；player 已调 `appendSkillBodyToPrompt(base, ["saolei"])` 作对称先例）。

#### Rationale

1. **复用既有机制**：`skill-loader.ts` 按 `mcp_names`/内置名注入 systemPrompt 的模式已由 saolei 验证（`specs/018-saolei-mcp` FR-023/024/025，`specs/020-agent-resources-layout`）。memory skill 仅扩展注册表 + planner 装配点，零新机制。
2. **对称性**：saolei skill → player（player 持 saolei 工具）；memory skill → planner（planner 持 memory 工具，FR-009）。player 不注入 memory skill（player 不持记忆工具）。
3. **静态注入保 prefix cache**：skill body 烘焙进 createAgent 的静态 systemPrompt（template-fixed，031 FR-028），与冻结快照（input SystemMessage，数据）分离——skill 是稳定指令，快照是可变数据（D11 Alternatives 已证）。

#### Alternatives considered

- ❌ 动态按 profile `mcp_names` 注入（如 player 的 saolei）：planner 的 memory 工具是本特性硬装配（非可选 profile 配置），且当前 planner prompt 装配不读 `mcp_names`；直接在 planner.ts 显式 `appendSkillBodyToPrompt(base, ["memory"])` 最简、与 player 的显式 `["saolei"]` 对称。
- ❌ 把 skill 内容写进 `DEFAULT_PLANNER_BASE`：base prompt 是复盘职责描述，skill 是工具使用引导，语义分层不同；且 skill-loader 的 SKILL_PROMPT_SEPARATOR 分隔 + 单独文件管理便于演进。

#### 待 plan 细化

- skill body 的精确措辞（中文/英文、篇幅 <5000 tokens，`specs/020-agent-resources-layout/contracts/skill-md-format.md`）；由 tasks 落实，参考 hermes HOW/WHEN/SKIP + dominion 冻结快照语义（D11）。
- `data_files` 声明：`artifact_pkg_js` target 须含 `src/skill/memory/SKILL.md`（同 saolei skill 的既有 data_files 模式，tasks 落实）。

---

## 未解决的问题

所有设计未知项已在 D1–D12 决策。以下留 plan/tasks 细化（非阻塞）：

- `MoveRejection` 原因码 → 三类的精确映射（D7，FR-002）。
- agent 生成 memory_id 的策略（UUID vs content-derived slug；AIP-122 字符集/长度，D9）。
- `memory` 工具 `operations` 批量路径是否实现 + 原子性机制（D9，v1 无硬上限，单 op 已满足核心需求）。
- `old_text` 子串匹配精确语义（大小写、最小长度，与 hermes 一致即子串包含，D9）。
- `saolei_operate` 双形态同时提供/均不提供的拒绝/优先规则 + 空 operations 行为（D12.1）。
- memory skill body 精确措辞（D12.2，tasks 落实）。
- 记忆条目上限值与达上限策略（v1 不设硬上限，D2；未来参考 hermes consolidate）。
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

**遗留（低/已接受）**：~~memory_add 的 LLM 提供 memory_id（R7）~~ → **Session 2026-08-08 已推翻**（memory_id 改由 agent 内部生成，LLM 用 old_text 子串定位，D9）；session 删除不清理 memory（R8，对齐 031）；新增第 6 服务的运维面（R9，directive 驱动，agent 侧移除 mongo 直连净简化）；进程内冻结快照缓存 + in-memory checkpointer/SessionTeamStore 不抗重启（R10，继承 031）。
