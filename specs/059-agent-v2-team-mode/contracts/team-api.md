# Contract: Team API（AgentService 演进为 team 模型）

> 对外契约：`projects/game/agent_v2.proto` 的会话面（原 AgentService → TeamService 语义，服务名与包演进为实现细节，本契约为行为规范）。传输路径不变：会话面经 `gateway → proxy（owner 亲和）→ agent-v2`，REST 挂 `/api/v2`。
> 资源模型见 [data-model.md](../data-model.md)；决策依据见 [research.md](../research.md) R8。

## 1. RPC 面

| RPC | HTTP | 语义 |
|---|---|---|
| `UpdateTeam(UpdateTeamRequest) returns Team` | `PATCH /api/v2/{team.name=templates/*/sessions/*/team}` body: `team` | AIP-134 create-or-update 单例：未物化=物化（启动工作流），已物化=刷新（终止在途回合、排队作废、清空全部成员短期记忆、按新配置重建、create_time 保留） |
| `GetTeam(GetTeamRequest) returns Team` | `GET /api/v2/{name=templates/*/sessions/*/team}` | AIP-131；未物化 NOT_FOUND（前端据此呈现引导态）；返回成员状态与 desktop_connected |
| `GetTeamMember(GetTeamMemberRequest) returns TeamMember` | `GET /api/v2/{name=templates/*/sessions/*/team/members/*}` | AIP-131；返回实例状态与 output-only `system_prompt` |
| `Send(SendRequest) returns stream ChatEvent` | `POST /api/v2/{session=templates/*/sessions/*}:send` | AIP-136 自定义方法，server-streaming（gateway 呈现 chunked NDJSON，同现状）；无懒创建 |
| `ListTeamMessages(ListTeamMessagesRequest) returns ListTeamMessagesResponse` | `GET /api/v2/{parent=templates/*/sessions/*/team}/messages` | AIP-132：团队视图历史（归并序列） |
| `ListMemberMessages(ListMemberMessagesRequest) returns ListMemberMessagesResponse` | `GET /api/v2/{parent=templates/*/sessions/*/team/members/*}/messages` | AIP-132：成员视角历史（替代原 ListAgentMessages） |
| `Cancel(CancelRequest) returns CancelResponse` | `POST /api/v2/{name=templates/*/sessions/*/team}:cancel` | 幂等；team 取消语义见 §4 |

## 2. UpdateTeam 校验与物化（fail-fast，无半物化）

1. `team.name` 资源名合法（template ∈ KNOWN_TEMPLATES=saolei）。
2. `player_preset`/`planner_preset` 必填；preset 存在且 `role` 匹配（PLAYER/PLANNER 对应）→ 否则 `INVALID_ARGUMENT`。
3. `player_model`/`planner_model`（可选）必须在模型目录（与 ListModels 同源）→ 否则 `INVALID_ARGUMENT`。
4. 物化编排：逐成员创建（preset mount + player 侧 saoleiGame 注册 / planner 侧 memory 预取 fail-loud）→ team 注册 → 自动驱动 planner 产出开局策略。任一步失败：整体回滚（无半物化），上游可重试。

## 3. Send 与实时事件（ChatEvent 扩展）

- 请求不变：`{session, text}`。
- 行为：未物化 → `FAILED_PRECONDITION`（引导物化）；当前激活成员回合中 → 用户消息入 team 排队队列，返回 `queued{position}` 帧（team 级、无 member 字段）；回合空闲 → 消息进团队消息流广播，由当前激活成员处理，其回合事件实时流出。
- **ChatEvent 每帧新增 `member` 字段**（PLAYER/PLANNER）：`turn_start`/`block_start`/`delta`/`block_end`/`tool_result`/`turn_end` 标注产出成员；`queued` 为 team 级不设。消费端（web）按 member 归并到团队视图与对应成员视角视图。
- 一个 Send 流的生命周期：从发起至**当前处理用户消息的成员回合结束**（排队消息被后续回合消化时，其事件属于该后续回合的 Send 流；回填经 ListTeamMessages/ListMemberMessages 保证一致性）。

## 4. Cancel（team 语义，FR-017）

终止在途回合（无论哪个成员被驱动，`turn_end{CANCELED}`）+ 暂停编排层自动续驱 + 排队消息落地为历史且不触发新驱动；幂等；team 立即可再次 Send（恢复续驱：消息由当前激活成员处理）。对已暂停状态再次 Cancel 为 no-op 成功。

## 5. 历史读取

- `ListTeamMessages`：返回按 `seq` 单调归并的 TeamMessage 序列（member ∈ USER/PLAYER/PLANNER，message 为原生输出/输入）；刷新 team 后为新生命周期（清空后重建）；分页语义与现状一致（整体返回、next_page_token 恒空的协议兼容位）。
- `ListMemberMessages`：返回该成员视角 MemberViewMessage 序列（message.role ∈ USER/AGENT；sender ∈ USER/PLAYER/PLANNER 标注来源——USER=用户输入，PLAYER/PLANNER=team 广播注入，前端渲染 `user: [sender]...`）。
- 两者的同一原生消息**正文一致**（SC-003）。

## 6. 错误语义

沿用 AIP-193 与现状映射：资源名非法/preset 缺失或 role 不匹配/model 不在目录 → `INVALID_ARGUMENT`；未物化 Send/Cancel 目标不存在 → `FAILED_PRECONDITION` / `NOT_FOUND`；内部错误 → `INTERNAL`（`cause` 链保留）。gRPC status 与前端提示语对齐现状单 agent 时期的错误呈现惯例。

## 7. 不变的面

- `DesktopBridgeService.Connect`（WebSocket `/api/v2/templates/{t}/sessions/{s}/connect`，session 单位、新连接接管、UserFrame/TeamFrame）零变更——player 独占使用（FR-012）。
- `PresetService`/`ListModels` 配置面路由（gateway 直连）不变，Preset 消息扩展见 [preset-api.md](preset-api.md)。
- `/api/v1` SessionService 路由不变。
