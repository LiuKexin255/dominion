# Data Model: Game Agent v2 — dsh 迁移 Step 2：游戏 agent 迁移与 desktop 退化

**Feature**: [spec.md](spec.md) | **Phase**: 1（设计） | **依据**: [research.md](research.md) D1–D16

本文定义本 feature 引入/演进的实体、字段、关系、校验规则与状态迁移。存量实体仅消费不修改：Game Session（`projects/game/game.proto` SessionService，Mongo `game_session`）、template 固定集合 `{saolei}`（`projects/game/pkg/gameconst/const.go`）、帧类型 `UserFrame`/`TeamFrame`/`FlowPart`/`FlowResultPart`（game.proto，desktop 桥接复用）。

---

## 1. 实体总览

```text
┌────────────────────────────────────────────────────────────────────┐
│ 持久层（Mongo）                                                      │
│  game_session.sessions（存量）   game_memory（存量，不动）            │
│  game_agent_v2.presets（新，FR-005）                                 │
└──────────────┬─────────────────────────────────────────────────────┘
               │ preset CRUD + ListModels（gateway 直连，PresetService）
               │ owner 亲和（agent 会话面，经 proxy）
┌──────────────▼─────────────────────────────────────────────────────┐
│ proxy：AgentHandler + DesktopBridgeHandler（agent_v2_owners 亲和；    │
│        仅承载会话面——配置面 PresetService 由 gateway 直连，不经 proxy）│
└──────────────┬─────────────────────────────────────────────────────┘
               │ gRPC（AgentService / PresetService / DesktopBridgeService
               │      同宿主 agent-v2 @50051）
┌──────────────▼─────────────────────────────────────────────────────┐
│ agent-v2 stateful 实例（dsh 组合：直组核心件 + 3 自研插件）            │
│  PresetStore(Mongo)   ModelCatalog(ctx.llm)   AgentMaterializer     │
│  AgentSession = dsh Agent + SessionHistory + FIFO queue             │
│  GameRuntime(session)（saolei-loop 注册于 agent.ctx，agent-scoped）│
│  DesktopConnection(session) ← ctx.desktopBridge（desktop-bridge）   │
└──────────────▲───────────────────────────────┬──────────────────────┘
               │ NDJSON /api/v2（对话流）        │ WS /api/v2/.../connect（flow 流，独立）
┌──────────────┴───────────┐   ┌───────────────▼─────────────────────┐
│ web（React）              │   │ desktop（退化：flow 控制终端）        │
│  侧栏 + 对话页 + preset   │   │  只读 session 选择 + 绑定 + 执行     │
│  管理 + agent 物化面板    │   └─────────────────────────────────────┘
└──────────────────────────┘
```

## 2. 资源与实体定义

### 2.1 Preset（preset 资源，持久化）

| 字段 | 类型 | 校验/说明 |
|---|---|---|
| `name` | string | `templates/{template}/presets/{preset}`；`{template}` ∈ 固定集合 {saolei}；`{preset}` 非空、不含 `/`、caller-supplied（AIP-133）；主键 |
| `player_prompt` | string | player 提示词；**空 = 物化时回退 `DEFAULT_PLAYER_BASE`**（v1 FR-034 语义）；**MUST NOT 含模型字段**（Q2 裁定） |
| `create_time` / `update_time` | Timestamp | 服务端维护 |

- **存储**：Mongo db `game_agent_v2`、collection `presets`；`_id` 由数据库自动生成（`style/mongo.md` 对象定义），`name` 字段建**唯一索引**（v1 prompt 服务 `team_profiles` 同构先例，`projects/game/prompt/runtime/mongo/repository.go:99-102`；[research.md](research.md) D2）。
- **生命周期**：CRUD 全标准方法（AIP-133/131/132/134/135）；删除不联动任何 agent（已物化 agent 的 persona 是创建期快照，Edge"preset 被删除时仍有 agent 引用"）；**不预置默认 preset**（Q2 裁定：使用前需先创建）。
- **并发**：同名 Create 二次 → `ALREADY_EXISTS`；Update 以 `update_mask`（`player_prompt`）应用，AIP-134。

### 2.2 Agent（session 的 agent 单例资源，AIP-156）

| 字段 | 类型 | 校验/说明 |
|---|---|---|
| `name` | string | `templates/{template}/sessions/{session}/agent`（单例，无 Create/Delete RPC） |
| `preset` | string | **必填**；物化时校验存在（`NOT_FOUND` fail-fast）；引用完整 preset 资源名 |
| `model` | string | 可选；空 = 进程默认（`GLM_MODEL \|\| "glm-5.2"`，与目录同源，[research.md](research.md) D4）；非空时校验 ∈ 模型目录（`INVALID_ARGUMENT`） |
| `create_time` / `update_time` | Timestamp | 服务端维护（update_time 随每次重新物化刷新） |

**物化记录（agent-v2 内存态）**——API 资源的可观测投影：

| 字段 | 类型 | 说明 |
|---|---|---|
| `config` | `{preset, model}` | 最近一次 UpdateAgent 生效配置 |
| `persona` | string | 物化时从 preset 读出的 player_prompt（空 → `DEFAULT_PLAYER_BASE`）——**固化快照**，此后 preset 编辑不影响本 agent（刷新 = 再次 Update） |
| `agent`/`handle` | dsh `Agent`/`AgentHandle` | `ctx.agents.create({sessionId: <session 资源名>, agentOptions: {provider: "glm-responses", model, persona}})` 产物（persona 经 D3 扩展传入） |
| `history` | SessionHistory | 内存对话记录（049 §2.5 延续 + D10 工具块回填） |
| `queue` | QueuedMessage[] | 每会话 FIFO（049 FR-012 延续） |
| `runtime` | GameRuntime | saolei-loop 在 `agent.ctx` 注册的 agent-scoped `saoleiGame` 服务实例（§2.5），随 agent dispose 自动注销 |

**UpdateAgent 统一语义**（FR-006，refresh 并入）：

```text
[absent] ──UpdateAgent(preset=P, model=M)──▶ [materialized(P,M)]
   │  1) 校验 P 存在、M ∈ 目录（fail-fast，不产生半物化）
   │  2) 创建 dsh agent（persona=P 当前内容，空回退 base）+ GameRuntime + history
   ▼
[materialized(P,M)] ──UpdateAgent(P',M')──▶ [materialized(P',M')]（刷新）
      1) 同上校验
      2) 终止在途回合（流收 turn_end{ABORTED}、排队作废）→ dispose 旧 agent
         （history/queue/GameRuntime 随之清空——A2/FR-006"清空短期记忆"）
      3) 按新配置重新物化
   幂等：重复 Update 相同配置 → 相同配置的干净 agent（状态等价）
```

**并发规则**：同 session 的 Update 与在途回合并发 → 先终止后物化（上图的既定终止语义，Edge"Update 与在途回合并发"）；不同 session 互不阻塞。

**重启语义**（A2）：物化记录整体内存态，重启丢失；owner 记录（proxy Mongo）仍在 → 重启后 `Send` 得 `FAILED_PRECONDITION`（提示先 Update），`GetAgent` 得 `NOT_FOUND`；仅 preset 数据存活。

### 2.3 Message（agent 子资源，标准 List）

`templates/{template}/sessions/{session}/agent/messages/{message}`——`ListAgentMessages`（AIP-132，`page_size`/`page_token`/`next_page_token` 标准分页字段；内存态全量按序返回，分页为协议合规字段）。消息模型延续 049 §2.5（`HistoryMessage{message_id, role(USER|AGENT), create_time, blocks[]}`；Text/Think/ToolCall 块），**ToolCallBlock 语义升级**（D10）：

| 字段 | 说明 |
|---|---|
| `tool_id`/`name`/`args_json` | 不变（049） |
| `status` | `RUNNING`（assistant 消息终局时工具未完成）→ 终态 `SUCCEEDED \| FAILED`（tool/result 到达后按 tool_id 回填） |
| `result` | 工具结果渲染文本（棋盘文本等）；失败时为错误文本 |

多 step 回合：每 step 的 `assistant/message` 记一条 AGENT 消息；工具结果回填到**本 session 历史中最近的同 tool_id 未终态块**（turn 内唯一）。

### 2.4 ChatEvent（Send 流事件，D10 扩展）

049 §2.4 事件集延续（queued/turn_start/block_start/delta/block_end/turn_end），**新增**：

| 事件 | 字段 | 语义 |
|---|---|---|
| `tool_result` | `tool_id, status(SUCCEEDED\|FAILED), result` | loop 的 `tool/result` session 事件映射；web 按 tool_id 关联 ToolCallBlock 终态化 |

**index 不变量强化**：`block_start/delta/block_end` 的 `index` 为**回合内全局单调**（TurnCollector 对 per-step index 重映射，多 step 工具回合不重叠）；其余 049 §3 事件序不变量全部延续（单一终止 turn_end 最后、queued 先于 turn_start 等）。

### 2.5 GameRuntime（saolei-loop 持有，每 session）

agent-scoped 服务：工厂在 agent 发布前于 `agent.ctx` 注册 `saoleiGame` 服务（GameRuntime 实例，Service class 形态），随 agent scope 卸载自动注销（D6）；宿主/根上下文不可见（无全局注册表）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `recognized` | GameState \| null | 最近识别棋盘；null = 无活动局（`no_active_game`）；识别失败置 null |
| `initState` | GameState \| null | 本局初始棋盘（mineCounter 解码正确标记数来源） |
| `operationCount` | number | 本局成功下发操作数（stats 输入） |
| `gameLog` | GameLogEntry[] | 本局操作序列；init 时重置；一次 operate 调用记**一条**（含完整操作列表）；终局追加 `(game-end)` 条目 |
| `gameEvent` | GameEventRecord \| null | 终局记录 `{status: won\|lost, stats{operationCount, correctFlags, avgOpsPerMine}, endedAt}` |

**游戏状态机**（v1 契约语义，A6——识别/校验/胜负判定规则不变，仅迁移承载位置）：

```text
[no_active_game] ──init：F2 下发+识别成功──▶ [playing]
[playing] ──operate 触发终局（识别判定 won/lost）──▶ [won | lost]
[playing] ──识别失败──▶ [no_active_game]（后续单元格操作按 no_active_game 拒绝）
[won | lost] ──init──▶ [playing]（重开）；终局后单元格操作按 game_won/game_over 拒绝
```

**操作执行管线**（GameRuntime.operate 内部，per operation）：

```text
校验（v1 validateMove 语义：越界→out_of_bounds；终局→game_over/game_won；
      UNKNOWN 宽松；per-type 规则） ──拒绝──▶ 三元组分诊：
      SKIP（cell_already_revealed/cell_is_flagged/cannot_flag_revealed/
            chord_requires_number/chord_no_unrevealed_neighbor）→ 批内继续
      STOP（out_of_bounds/no_active_game/game_over/game_won）→ 批终止
通过 ──▶ desktopBridge.dispatch（client 空间坐标：center(x,y)
          = (24+x*32+16, 104+y*32+16)，WINDOW_MESSAGE 输入法）
      ──▶ OperationResult{status, screenshot} ──▶ 识别更新 recognized
      ──▶ 终局判定（loss-first：HIT_MINE/MINE → lost；counter-informed win → won）
```

**结果文本契约**（模型可见，FR-013——逐字延续 v1）：
- 三层体：outcome 行（`new game started` / `saolei_operate → executed N ops` / `... stopped at {type}({x},{y}) ({reason})` / `rejected: <reason>` / `unable to recognize board` / `saolei_remain → computed`）+ `game status: won|lost|playing` 行（无活动局且无状态时省略）+ 标尺棋盘（`board size <w>*<h>` + `col<N>`/`row<N>` 标尺网格，符号 `* 0-8 F X M ?`）+ 拒绝时 `valid range:` 行。
- `saolei_remain`：每已揭示数字格 `数字 − 相邻 F 数`（可为 0/负），其余 `-`，同标尺网格。
  > 本行表述已被 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` §1 修订（结果体网格前增 legend 语义标注行，主语义 = 剩余未标记雷数，2026-09-14）；现行表述以该修订为准。
- **错误结果 ≠ 拒绝**：游戏规则拒绝是**正常结果文本**（`rejected:` 行）；desktop 缺席/断连/超时是 `dispatch` 返回 FAILED 或错误 → 工具**错误结果**（`isError: true`，模型可见、回合存活，US1 场景 4/5）。

**双形式 operate 入参**（工具参数校验，v1 字面量）：single `{type,x,y}` 与 batch `{operations[]}` 互斥——两者皆无 `MISSING`、两者皆有 `AMBIGUOUS`、single 残缺 `INCOMPLETE`，精确文本随迁（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:621-633`）。

### 2.6 DesktopConnection（desktop-bridge 持有，每 session）

| 字段 | 类型 | 说明 |
|---|---|---|
| `sessionName` | string | 绑定键（首帧 template_id/session_id，gateway URL 注入） |
| `write` | (frame: TeamFrame) => void | 写回调（不持流引用——断线重连不丢 in-flight，v1 OperationBridge 语义） |
| `pending` | Map<tool_id, PendingDispatch> | 在途操作（超时 backstop 20min；abort/断线按 FAILED 错误结算） |

**状态迁移**：

```text
[absent] ──Connect（首帧绑定 session）──▶ [connected]
[connected] ──同 session 新 Connect──▶ [connected']（接管：旧连接关闭——v1 基线）
[connected] ──流断开/错误──▶ [absent]（在途 dispatch 结算 FAILED "desktop disconnected"）
任何态：dispatch 无连接 → 立即 FAILED 错误结果（不下发、无半执行）
```

**与对话流独立**（Q3/FR-009）：flow 流（本实体）与对话流（Send NDJSON）为独立连接/独立 gRPC 流，互不影响（构造性独立）。

### 2.7 ModelCatalog（只读目录）

`ListModels` → `Model{id, context_window}[]`；来源 = `ctx.llm.listModels("glm-responses")` = glm 插件 `config.models`（D4）；默认模型 = `GLM_MODEL || "glm-5.2"`（与 cordis.yml 同一表达式，目录必含）。部署级只读，无 CRUD。

### 2.8 dsh Composition Manifest（cordis.yml 重写）

启用面唯一事实源（[research.md](research.md) D5）：`timer`、`llm`、`session`、`system-prompt`（`includeHarnessIdentity: false`、`includeRuntimeContext: false`）、`tools`、`agents`、`invariants` + 三伴生、`llm-retry`、`llm-glm`（models 目录行延续 049）、`desktop-bridge`、`saolei-loop`、`saolei`。**无 spine 行、无官方 agent-loop 行**（FR-012）；全局 persona 配置移除（persona 全部来自 preset 物化，D3）。

### 2.9 路由（agent_v2_owners 语义演进；配置面直连，directive §3）

| RPC / 服务 | 路由 | 分配语义 |
|---|---|---|
| `UpdateAgent` | owner 亲和（经 proxy） | **get-or-create**（物化即落 owner——新分配点） |
| `GetAgent` / `ListAgentMessages` / `Send` | owner 亲和（经 proxy） | 只查不分配；无 owner → `NOT_FOUND`（Send 的未物化错误第一层） |
| `PresetService`（preset CRUD / `ListModels`） | **gateway 直连 agent-v2**（不经 proxy） | 任意活实例（gRPC 客户端 LB）；无实例 → `UNAVAILABLE` |
| `DesktopBridgeService.Connect` | owner 亲和（经 proxy） | **get-or-create**（desktop 可先于对话连接；与对话同实例——游戏状态所在） |

### 2.10 web 前端状态（演进）

- 侧栏：session 列表（不变）+ 新视图切换 `sessions | presets`；条目级 `···` 菜单（删除）。
- ChatPanel：+ `agentStatus`（`unmaterialized | materialized | unknown`——GetAgent 404 / Send 前置错误驱动）；物化面板（preset/model 下拉 + Apply）。
- store：ChatEvent `tool_result` → 按 tool_id 终态化 ToolCallBlock（live 与 history 回填同规则）；删除编排去 dispose 跳。

## 3. 校验与错误码汇总（agent-v2 请求级）

| 场景 | 错误 |
|---|---|
| 资源名不匹配 `templates/{t}/sessions/{s}` / `templates/{t}/presets/{p}` 形状；template ∉ {saolei} | `INVALID_ARGUMENT`（proxy 先拦、agent-v2 兜底，049 双级校验延续） |
| Send 空 text / CreatePreset 缺 preset_id / Update 缺 name | `INVALID_ARGUMENT` |
| Send 无 owner（proxy） | `NOT_FOUND` |
| Send 有 owner 但 agent 未物化（重启后/从未 Update） | `FAILED_PRECONDITION`（提示先 Update；grpc-gateway → 400） |
| UpdateAgent preset 不存在 / model 未知非空 | `NOT_FOUND` / `INVALID_ARGUMENT`（fail-fast，无半物化） |
| GetAgent 未物化 | `NOT_FOUND` |
| preset 重复创建 | `ALREADY_EXISTS` |
| 无 agent-v2 活实例 | `UNAVAILABLE`（会话面由 proxy 应答；配置面由 gateway 直连应答，[contracts/agent-api.md](contracts/agent-api.md) §3） |

## 4. 不变量

1. **模型可见即落日志**（dsh）：游戏事实只经工具结果进入模型上下文（FR-011"游戏状态 MUST NOT 经隐藏通道外泄"）——棋盘/状态经 tool/result session 事件，天然满足。
2. **tool_call 必有对应 tool_result**（wire 不变量）：loop 的调度器取消路径合成 `TOOL_ABORTED_BEFORE_DISPATCH` 等价物（调研 §4.3 继承）。
3. **回合串行**：同 session 至多一个回合在跑；队列仅在回合中非空；turn_end 后自动取队首（049 延续）。
4. **物化原子性**：UpdateAgent 要么完整物化新 agent、要么保持旧 agent 不变（校验先行，无中间态）。
5. **两流独立**：对话流与 flow 流任一断开/故障不影响另一条（US1 场景 6）。
6. **会话隔离**：多 session 的 history/queue/GameRuntime/DesktopConnection 互不串扰（Edge"并发多 session 游戏"）。
7. **token 零泄漏**：全部错误/日志/事件不含明文 token（SC-006，049 延续）。
