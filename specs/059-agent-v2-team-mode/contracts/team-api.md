# Contract: Team API（AgentService 演进为 team 模型）

> 对外契约：`projects/game/agent_v2.proto` 的会话面（原 AgentService → TeamService 语义，服务名与包演进为实现细节，本契约为行为规范）。传输路径不变：会话面经 `gateway → proxy（owner 亲和）→ agent-v2`，REST 挂 `/api/v2`。
> 资源模型见 [data-model.md](../data-model.md)；决策依据见 [research.md](../research.md) R8。
> 会话面 proto 为**场景无关 team 原语**（2026-09-10 用户裁定）：role/member/sender 均为字符串——保留值 `"user"` 标注用户消息、成员 role 为场景词汇（saolei 下 `"player"`/`"planner"`）、空字符串=未设置；`Team` 物化输入为 `members` 列表。saolei 场景约束由 agent_v2 服务端校验承载（§2）。

## 1. RPC 面

| RPC | HTTP | 语义 |
|---|---|---|
| `UpdateTeam(UpdateTeamRequest) returns Team` | `PATCH /api/v2/{team.name=templates/*/sessions/*/team}` body: `team` | AIP-134 create-or-update 单例：物化输入 = `team.members` 列表（每成员 `{role, preset, model?}`，场景无关原语，校验见 §2）；未物化=物化（静止等待，工作流由用户首条消息触发），已物化=刷新（终止在途回合、排队作废、清空全部成员短期记忆、按新配置重建、create_time 保留） |
| `GetTeam(GetTeamRequest) returns Team` | `GET /api/v2/{name=templates/*/sessions/*/team}` | AIP-131；未物化 NOT_FOUND（前端据此呈现引导态）；返回成员状态与 desktop_connected |
| `GetTeamMember(GetTeamMemberRequest) returns TeamMember` | `GET /api/v2/{name=templates/*/sessions/*/team/members/*}` | AIP-131；返回实例状态与 output-only `system_prompt` |
| `Send(SendRequest) returns stream ChatEvent` | `POST /api/v2/{session=templates/*/sessions/*}:send` | AIP-136 自定义方法，server-streaming **team 流**（gateway 呈现 chunked NDJSON，同现状；流持续至 team 静止，见 §3）；无懒创建 |
| `ListTeamMessages(ListTeamMessagesRequest) returns ListTeamMessagesResponse` | `GET /api/v2/{parent=templates/*/sessions/*/team}/messages` | AIP-132：团队视图历史（归并序列） |
| `ListMemberMessages(ListMemberMessagesRequest) returns ListMemberMessagesResponse` | `GET /api/v2/{parent=templates/*/sessions/*/team/members/*}/messages` | AIP-132：成员视角历史（替代原 ListAgentMessages） |
| `Cancel(CancelRequest) returns CancelResponse` | `POST /api/v2/{name=templates/*/sessions/*/team}:cancel` | 幂等；team 取消语义见 §4 |

## 2. UpdateTeam 校验与物化（fail-fast，无半物化）

会话面 proto 为**场景无关 team 原语**：物化输入是 `team.members` 列表（每成员 `{role, preset, model?}`，caller-supplied 成员配置），字段层不编码 saolei 词汇、成员数量或 role 值域。saolei 场景约束由 **agent_v2 服务端校验**承载（saolei 场景宿主，KNOWN_TEMPLATES 机制不变），校验失败均为 `INVALID_ARGUMENT`（错误为场景校验表述，不含"proto 限制"色彩）：

1. `team.name` 资源名合法（template ∈ KNOWN_TEMPLATES=saolei）。
2. 结构校验（场景无关）：`members` 非空；每成员 `role` 非空（空字符串=未设置）、`preset` 为合法 preset 资源名；`model` 可空（空=部署默认）。
3. saolei 场景校验：`members` 恰 2 项且 role 集合恰为 `{"player", "planner"}`（无重复/遗漏/多余）；每成员 preset 存在且 `preset.role` 与成员 `role` **字符串相等**；`model` 非空时在模型目录（与 ListModels 同源）。
4. 物化编排：逐成员创建（preset mount + 按成员 `role` 分派场景 setup：player 侧 saoleiGame 注册 / planner 侧 memory 预取 fail-loud）→ team 注册 → 静止等待用户消息（初始激活成员 = planner，不自动驱动任何成员；首条用户消息驱动 planner 产出开局策略）。任一步失败：整体回滚（无半物化），上游可重试。

`update_mask` 省略 = 替换全部可变字段；可命名路径仅 `members`（列表整体替换，无逐成员 patch）；`allow_missing=true` 维持 create-or-update 语义（AIP-134）。

## 3. Send 与实时事件（team 流）

- 请求不变：`{session, text}`。
- 行为：未物化 → `FAILED_PRECONDITION`（引导物化）；当前激活成员回合中 → 用户消息入 team 排队队列，流首帧返回 `queued{position}`（team 级、无 member 字段）；回合空闲 → 消息直接进入团队消息流，由当前激活成员处理。用户消息在 Send 被接受时即固化入归并序列（enqueue 即固化，现状语义），并以 `team_message{member="user"}` 帧扇出。

### 3.1 team 流生命周期（team turn 持续流）

一个 Send 建立的 NDJSON 流从发起持续输出，覆盖发起后的**全部成员回合**——包括当前激活成员处理该消息的回合，与编排自动驱动的成员回合（结构性续驱 player、gameEnded 复盘、排队消化、多局循环）——直到 **team 静止**（编排层无在途回合且无待消化输入，即编排状态机的静止态，[data-model.md](../data-model.md) §5）才由服务端结束流。静止包括自然收敛（player 不开局 / planner 无策略）与 Cancel 后的暂停静止。

### 3.2 帧词汇（双消息承载）

同一流承载两类帧：

| 帧类 | 帧 | 层级 | 说明 |
|---|---|---|---|
| 成员事件帧（agent message） | `turn_start` / `block_start` / `delta` / `block_end` / `tool_result` / `turn_end` | 成员级 | 现有 ChatEvent 词汇，外层 `member` 字段（string——产出成员的 role，场景词汇，saolei 下 `"player"`/`"planner"`；仅成员事件帧设值）标注产出成员；block index 全局单调与 `step` 语义保持**以成员回合为单位**（每回合一个 index 空间）；`turn_id` 照常为成员回合铸造、team 流内每回合独立——前端按 `(member, turn_id)` 分组增量渲染 |
| team message 帧（team message） | `team_message` | team 级 | **新增**：载荷 `{member, message, seq}`，与 `ListTeamMessages` 返回元素同构——member 为 string（保留值 `"user"`=用户消息；成员 role=成员产出）、message 为原生消息、seq 为归并序锚（与 List 面同源同值）；与 `queued` 同为 team 级帧、不设外层 `member` 字段（空字符串=未设置） |

`team_message` 帧在归并序列每次追加条目时扇出：用户消息在 Send 被接受时追加（含排队路径——enqueue 即固化，现状语义），成员消息在其固化入归并序列（回合内逐步落定）时追加。前端分工：成员事件帧驱动实时增量渲染；`team_message` 帧提供归并序列锚（seq），保证团队视图归并序与 List 回填跨视图一致（SC-003）。

### 3.3 流与编排解耦

team 流是编排事件的**订阅面**而非编排本身：

- 客户端断开 / 流取消 MUST NOT 终止编排循环；取消编排仅经 Cancel RPC（§4）。
- 服务端向该 session 的**全部活跃 team 流**扇出事件（成员事件帧 + `team_message` 帧）；`queued` 帧例外——它是该 Send 调用自身消息入队的回执，仅出现在其所响应的流（且为首帧）。
- 无活跃流时编排照常运行、事件照常固化入归并序列与成员历史（List 面可回填）。
- 流异常断开不构成任何编排语义：客户端经 ListTeamMessages/ListMemberMessages 回填补齐（既有回填一致性机制），team 流在下次用户 Send 时重新建立。

### 3.4 并发流

同 session 允许多个并发 Send 流（如流 A 存续期间用户再 Send 建流 B）：每个活跃流**完整接收** team 事件扇出，服务端不跨流去重、不合并流。帧应用幂等：成员事件帧按 `(member, turn_id)` + index/step 增量锚、`team_message` 帧按 seq 归并锚，多流重复帧由前端按锚去重。中途建立的流只接收订阅点之后的帧；错过的在途回合增量经该回合固化的 `team_message` 帧（完整条目）与 List 回填补齐。

## 4. Cancel（team 语义，FR-017）

终止在途回合（无论哪个成员被驱动，`turn_end{CANCELED}`）+ 暂停编排层自动续驱 + 排队消息保留为已固化历史（Send 接受时已入归并序列，§3）且不触发新驱动；幂等；活跃 team 流在 Cancel 后的静止点结束（§3.1）。team 立即可再次 Send（恢复续驱：消息由当前激活成员处理，建立新 team 流）。对已暂停状态再次 Cancel 为 no-op 成功。

## 5. 历史读取

- `ListTeamMessages`：返回按 `seq` 单调归并的 TeamMessage 序列（member 为 string——保留值 `"user"`=用户消息、成员 role=成员产出；message 为原生输出/输入）；刷新 team 后为新生命周期（清空后重建）；分页语义与现状一致（整体返回、next_page_token 恒空的协议兼容位）。
- `ListMemberMessages`：返回该成员视角 MemberViewMessage 序列（message.role ∈ USER/AGENT——HistoryMessage 自身 Role 枚举不变；sender 为 string 标注来源：`"user"`=用户输入、成员 role=team 广播注入，前端渲染 `user: [sender] 正文`——sender 为 role 字符串原值，saolei 下如 `user: [player] …`）。
- 两者的同一原生消息**正文一致**（SC-003）。

## 6. 错误语义

沿用 AIP-193 与现状映射：资源名非法/UpdateTeam 结构或 saolei 场景校验失败（§2——members 数量/role 值域/preset 缺失或 role 不匹配/model 不在目录）→ `INVALID_ARGUMENT`；未物化 Send/Cancel 目标不存在 → `FAILED_PRECONDITION` / `NOT_FOUND`；内部错误 → `INTERNAL`（`cause` 链保留）。gRPC status 与前端提示语对齐现状单 agent 时期的错误呈现惯例。

## 7. 不变的面

- `DesktopBridgeService.Connect`（WebSocket `/api/v2/templates/{t}/sessions/{s}/connect`，session 单位、新连接接管、UserFrame/TeamFrame）零变更——player 独占使用（FR-012）。
- `PresetService`/`ListModels` 配置面路由（gateway 直连）不变，Preset 消息扩展见 [preset-api.md](preset-api.md)。
- `/api/v1` SessionService 路由不变。
