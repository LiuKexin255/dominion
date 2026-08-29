# Data Model: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Feature**: [spec.md](spec.md) | **Phase**: 1（设计；2026-08-29 修订 proxy 路由） | **依据**: [research.md](research.md) D1–D12

本文档定义本 feature 引入/消费的实体、字段、关系、校验规则与状态迁移。存量实体（Game Session 元数据、TeamProfile 等，`projects/game/game.proto`）仅消费不修改。

---

## 1. 实体总览

```text
┌─────────────────────────────────────────────────────────────┐
│ 现有 game 域（零改动，仅消费）                                  │
│  Game Session (Mongo, session 服务) ── /api/v1 CRUD           │
│  template 固定集合 {saolei} (gameconst)                        │
└──────────────┬──────────────────────────────────────────────┘
               │ 资源名直映射（1:1，get-or-create）
               ▼
┌─────────────────────────────────────────────────────────────┐
│ agent_v2 有状态实例（kind: stateful，内存态随进程重启丢失）        │
│  AgentSession ─1:1─ dsh Agent (sessionId = 资源名)            │
│      ├─ FIFO 队列 [QueuedMessage]                             │
│      ├─ TurnRunner（当前 ChatTurn 状态机）                     │
│      └─ ConversationHistory [HistoryMessage{blocks}]          │
└──────────────▲──────────────────────────────────────────────┘
               │ owner 亲和定向（AgentV2Owner，§2.9）
┌──────────────┴──────────────────────────────────────────────┐
│ proxy（ConversationService 转发面 + Mongo AgentV2Owner）        │
└──────────────▲──────────────────────────────────────────────┘
               │ gRPC（/api/v2 三 RPC）
┌──────────────┴──────────────────────────────────────────────┐
│ gateway（HTTP 出口：/api/v1 → session 等存量；/api/v2 → proxy） │
└──────────────▲──────────────────────────────────────────────┘
               │ 相对路径（/api/v1、/api/v2）
┌──────────────┴──────────────────────────────────────────────┐
│ web 前端（浏览器内存态）                                        │
│  SessionListView ← /api/v1    ChatView ← /api/v2 流 + history │
└─────────────────────────────────────────────────────────────┘
```

## 2. 实体定义

### 2.1 Game Session（存量，消费）

- 资源名 `templates/{template}/sessions/{session}`，本阶段新建固定 `template=saolei`（spec 澄清）。
- 元数据（`create_time` 等）由现有 session 服务持久化（Mongo）；管理面 = 既有 `/api/v1` REST（`projects/game/game.proto` SessionService）。
- **映射规则**：一个 game session 资源名 ↔ agent_v2 内一个 `AgentSession` 条目 ↔ 一个 dsh agent（`sessionId` 即资源名字符串，宿主自选，047 D5）。

### 2.2 AgentSession（agent_v2 实例内存态，owner 亲和）

| 字段 | 类型 | 说明 |
|---|---|---|
| `session` | string | 完整资源名（主键） |
| `agent` / `handle` | dsh `Agent` / `AgentHandle` | `ctx.agents.create({sessionId, agentOptions:{provider:'glm-responses', model}})` 的产物 |
| `queue` | `QueuedMessage[]` | 每会话 FIFO（FR-012） |
| `runner` | TurnRunner | 会话内回合串行驱动器（chain 模式） |
| `history` | `HistoryMessage[]` | 内存对话记录（FR-014），保序追加 |

**归属实例语义（owner 亲和，[research.md](research.md) D4）**：AgentSession 的生命周期与其所在 agent_v2 实例绑定——归属实例由 proxy 侧 AgentV2Owner 映射（§2.9）在**首次 Send** 时选定并持久；此后该 `(template, session)` 的全部 ConversationService 调用（Send/ListHistory/Dispose）经 proxy 定向同实例。`AgentSessions` 注册表是**每实例局部**的：同一资源名在不同实例上是互不相干的两个会话，owner 映射保证调用不会跨实例漂移（否则内存会话/历史被撕裂）。agent_v2 实例重启后 owner 映射仍指向同序号实例（StatefulSet 序号稳定，`specs/006-grpc-js-service-discovery` FR-009），内存历史丢失（spec Assumptions）。请求在 proxy 侧的路由短路（无 owner）见 [contracts/conversation-api.md](contracts/conversation-api.md) §2：ListHistory → 空列表、Dispose → 幂等 Empty（读路径不分配 owner）。

**校验**：资源名必须匹配 `templates/{template}/sessions/{session}` 形状（`{session}` 非空、`{template}` ∈ gameconst 固定集合），否则 `INVALID_ARGUMENT`——proxy 与 agent_v2 两级同规则校验（proxy 先拦，agent_v2 兜底；对齐 gateway 的 identity 注入模式，`projects/game/gateway/cmd/main.go` extractConnectIdentity 注释）。

**状态迁移**：

```text
[absent] --Send/ListHistory(get-or-create)--> [live]
[live] --Dispose（或进程退出）--> [disposed：条目移除、handle.dispose()、
        在途流收 turn_end{ABORTED}、排队作废、历史不可查询]
[live] --同资源名再次访问--> 全新会话（无残留状态，FR-015）
```

### 2.3 QueuedMessage（排队消息，FR-012）

| 字段 | 类型 | 说明 |
|---|---|---|
| `text` | string | 用户消息文本（非空校验在入队前） |
| `stream` | 事件接收端 | 该消息 `Send` 调用对应的服务端流（保持打开直至本消息回合 turn_end） |
| `enqueuedAt` | 序号 | 队内位置（1 起），用于 `queued{position}` 事件 |

**不变式**：同一会话内同一时刻至多一个回合在跑；队列仅在回合进行中非空；`turn_end` 后 runner 立即取队首开新回合（自动按序发送）。

### 2.4 ChatTurn（对话轮次）与流事件（ChatEvent）

**回合状态机**（`turn_id` 为服务端铸造的 UUID，贯穿一个回合的全部事件）：

```text
[排队中] --轮到--> [running: turn_start] --chunk 流--> [turn_end{COMPLETED|ERROR|ABORTED}]
```

**ChatEvent payload**（proto oneof，与 dsh StreamChunk 同构，映射规则见 [contracts/conversation-api.md](contracts/conversation-api.md) §4）：

| 事件 | 字段 | 语义 |
|---|---|---|
| `queued` | `position:int32` | 消息入队位置（FR-012 排队指示；入队时发一次） |
| `turn_start` | — | 本消息回合开始 |
| `block_start` | `index, type(TEXT\|THINK\|TOOL_CALL), tool_id?, name?` | 内容块开始（index 按流中首现顺序分配） |
| `delta` | `index, text` | 增量文本（语义由所属 block 的 type 决定：TEXT=正文/THINK=思考/TOOL_CALL=参数 JSON 片段） |
| `block_end` | `index, block(ContentBlock 终态)` | 内容块终局（TOOL_CALL 携带完整 `args_json`） |
| `turn_end` | `status(COMPLETED\|ERROR\|ABORTED), error?, usage?` | 回合终止；流随之关闭 |

### 2.5 ContentBlock（内容块，行为基线 = `projects/game/game.proto` MessagePart 语义）

| 块 | 字段 | 校验/说明 |
|---|---|---|
| `TextBlock` | `content:string` | 正文；非空序列化为块（空文本块不产生） |
| `ThinkBlock` | `content:string` | 思考；与正文**分类不混排**（US2）；无思考内容的回合不产生（US2 场景 2） |
| `ToolCallBlock` | `tool_id, name, args_json, status, result` | `status: RUNNING\|SUCCEEDED\|FAILED`（RUNNING=执行中状态，US3 场景 1）；`result:string?` 与调用**关联展示于同一块**（US3 场景 2，对齐 desktop 的 tool_id 合并渲染基线）。本阶段 agent 零工具（FR-006），该块仅由构造数据在页面/接口层验证；真实产生留待后续工具 step |

**历史消息**：`HistoryMessage{message_id(服务端序号), role(USER|AGENT), create_time, blocks[]}`；用户消息入队时记录（blocks=[TextBlock]），agent 回复在 `assistant/message` 终局时记录（blocks=终局 ContentBlock 序列，保序保分类，FR-014）。

### 2.6 dsh Composition Manifest（cordis.yml，[research.md](research.md) D3）

启用面的唯一事实源，两行（值来源标注）：

```yaml
- id: agent-spine
  name: '@deepseek-ai/dsh-agent-spine-demo'
  config:
    persona: '<game 对话助手人设>'
    workspaceContext: false        # 唯一必填键
    includeRuntimeContext: false
    includeHarnessIdentity: false
    skills: { enabled: false }
    toolBash: false                # 零工具（FR-006）
    toolJobs: false
- id: llm-glm
  name: '@dominion/dsh-llm-glm'
  config:
    apiKeyEnv: GLM_API_KEY                      # bootstrap 由 secret 文件注入（D9）
    baseURL: !!js process.env.GLM_BASE_URL      # bootstrap 解析/默认（D9）
    models:
      - id: !!js process.env.GLM_MODEL || 'glm-5.2'
        contextWindow: 1000000
```

### 2.7 Model Endpoint（模型端点）

| 属性 | 值 |
|---|---|
| 协议 | OpenAI Responses（`POST {baseURL}/responses`，SSE 流） |
| baseURL | `GLM_BASE_URL` env；生产默认 `https://open.bigmodel.cn/api/v1`；测试经 `GLM_LLM_TARGET` 服务发现指向 fake（D9） |
| token | `$DOMINION_SECRET_DIR/glm-api-token` 文件 → `GLM_API_KEY` env（`specs/002-deploy-secret-config/contracts/secret-config.md` §5；k8s 绑定 `llm-secrets/glm-codingplan`） |
| model id | `GLM_MODEL` env，默认 `glm-5.2`（contextWindow 1000000） |
| 失败语义 | 端点不可达/无效 token → `turn_end{ERROR}`（进程存活、会话可恢复，spec Edge Cases）；token 缺失于启动期 fail-loud |

### 2.8 fake-llm Responses 端点（扩展实体，[contracts/fake-responses-wire.md](contracts/fake-responses-wire.md))

`projects/game/fake-llm` 新增 `POST /v1/responses`：消费 OpenAI Responses 请求（model 忽略、input items、`stream:true`），按模板匹配产出 `reasoning_summary_text.delta`（think）与 `output_text.delta`（text）事件序列；沿用既有模板/多轮条件/延迟设施。

### 2.9 AgentV2Owner（proxy 侧路由实体，[research.md](research.md) D4）

proxy 为 agent_v2 维护的 `(template, session) → 实例` 亲和映射，复用 v1 路由实体形态（`domain.AgentOwner`）与设施（`domain.OwnerStore` 接口、hash picker、agentclient manager）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `template_id` + `session_id` | string 复合主键 | 同 v1 AgentOwner 键形（同 template 不同 session、同 session 不同 template 均为不同键） |
| `owner_index` | int | agent_v2 StatefulSet 实例序号（分配时 `FNV32a(sessionID) % 实例数`） |
| `owner` | string | 实例名（resolver Hostname） |
| `create_time` | timestamp | 分配时间 |

- **存储**：Mongo `game_proxy.agent_v2_owners` collection——与 v1 `agent_owners` **不同 collection 隔离**：两个服务实例池独立（owner_index 语义互不通用），同一 game session 可同时存在 v1 team owner 与 v2 conversation owner 两条映射、互不干扰；owner store 构造函数语义化（`NewAgentOwnerStore(client)` / `NewAgentV2OwnerStore(client)`），db/collection 常量私有化于 `projects/game/proxy/runtime/mongo` 包内——collection 命名知识由 store 层单一持有，`cmd/main.go` 只做装配、不出现 collection 字符串（[research.md](research.md) D4）；v1 存储位置与行为零改动（调用点仅构造名变化）。
- **分配**：首次 `Send` get-or-create（并发竞态 `ErrOwnerAlreadyExists` → 重读胜者，复用 v1 `assignOwner` 模式 `projects/game/proxy/handler/handler.go`）；读路径（ListHistory/Dispose）不分配。
- **生命周期**：创建后不删除——dispose 不清 owner（映射是亲和锚点而非会话状态，与 v1 owner 生命周期一致）；同资源名再 Send 定向同实例，agent_v2 侧 get-or-create 全新会话（FR-015 无残留由 agent_v2 保证）。
- **状态迁移**：

```text
[absent] --首次 Send（get-or-create + 持久）--> [assigned{owner_index}]
[assigned] --实例缩容至 < owner_index --> 转发失败 UNAVAILABLE（503），
             与 v1 缩容语义一致（本阶段不做迁移）
```

## 3. 关系与一致性规则

1. **资源名即身份**：game session 资源名贯穿 `/api/v1`（元数据）与 `/api/v2`（对话）两面；两面独立生命周期（元数据持久、对话内存），删除编排（D6）保证"元数据删除成功 ⇒ 资源释放"。
2. **事件与历史一致**：同一回合内 `block_end` 终态块 ⊕ 历史追加块 = `assistant/message` 载荷（上游装配语义，`/tmp/opencode/dsh/packages/core/agent-loop/src/agent.ts` assembler）；刷新回填与流式呈现一致（FR-014）由此结构保证。
3. **分类保序**：text/think/tool-call 按发生顺序保序；块内增量按 index 归属；tool 结果按 `tool_id` 关联。
4. **隔离**：会话间状态（agent/queue/history）零共享；并发互不阻塞（US1 场景 3/Edge）。
5. **owner 亲和（2026-08-29 修订）**：同一 `(template, session)` 的全部对话调用经 proxy 定向唯一 agent_v2 实例（§2.9）；映射持久于 Mongo、实例重启不漂移；无 owner 的读请求在 proxy 短路（空 history/幂等 dispose），不产生分配副作用。

## 4. 非目标（本阶段明确不建模）

team/TeamProfile、桌面操作（mouse/keyboard FlowPart）、image 内容块、observe-only 扩展队列行为、跨客户端（desktop↔web）删除联动、多标签页实时推送、对话历史持久化（dsh persistence 插件）——均为后续 step 范围（spec FR-010/Assumptions）。
