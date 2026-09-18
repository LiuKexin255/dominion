# Data Model: Agent v2 Team 模式迁移

> 实体与资源模型（Phase 1）。资源命名与 RPC 契约详见 [contracts/team-api.md](contracts/team-api.md) 与 [contracts/preset-api.md](contracts/preset-api.md)；存储实现选型依据见 [research.md](research.md) R3/R4。

## 1. 资源层级

```text
templates/{template}                        # template（现有，KNOWN_TEMPLATES 仅 saolei）
├── sessions/{session}                      # Session（现有，/api/v1 管理，不变）
│   ├── team                                # Team —— session 的单例资源（AIP-156，替代原 agent 单例）
│   │   └── members/{member}                # TeamMember —— member id 即 role 字符串（saolei 物化后恰两个：members/player 与 members/planner，§4 服务端场景校验）
│   │       ├── (system_prompt)             # output-only 字段（GetTeamMember 返回）
│   │       └── messages                    # 成员视角历史（ListMemberMessages）
│   └── (connect)                           # DesktopBridgeService.Connect（现有，session 单位，不变）
└── presets/{preset}                        # Preset（现有路径，增加 role 字段分池）
```

member 资源名以 role 字符串为 id（saolei 物化后即 `members/player`、`members/planner`）——成员由 team 物化创建、无独立生命周期，id 即角色（场景词汇），避免引入可变成员 id；proto 层不约束 member 数量与 role 值域（场景无关原语），saolei 场景恰两成员由服务端物化校验保证（§4）。

## 2. 实体定义

### Team

session 的团队单例（FR-004/FR-005）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/sessions/{session}/team` |
| members | TeamMember[] | 物化输入与运行态输出**同形**。输入（caller-supplied 成员配置）：每成员 `role`（非空场景词汇）+ `preset`（preset 资源名）+ `model`（可空=部署默认）；`name`/`system_prompt` 输入侧不设。输出：物化后的成员快照（`name` 由服务端按 role 构造、`model` 为生效值、`system_prompt` 仅 GetTeamMember 填充）。proto 层不约束成员数量与 role 值域（场景无关原语）——saolei 场景约束由服务端校验承载（§4） |
| desktop_connected | bool | output-only：session 级桌面连接状态（player 独占使用） |
| create_time / update_time | Timestamp | create_time 跨刷新保留、update_time 刷新更新（对齐现状 Agent 语义） |

**生命周期**：未物化（无 team 记录，Send 拒绝并引导）→ 已物化（UpdateTeam 创建）→ 刷新（UpdateTeam 再次调用：终止在途回合、排队作废、清空全部成员短期记忆、按新配置重建；create_time 保留）→ 随进程内存态丢失（重启后回到未物化，既有语义）。

### TeamMember

team 的成员（物化配置与运行状态同形载体；无独立 CRUD，随 team 物化/刷新生灭）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/sessions/{session}/team/members/{member}`——`{member}` 即 role 字符串（saolei 物化后 members/player、members/planner）；输出侧由服务端按 role 构造 |
| role | string | 成员 role——场景词汇（saolei 下 `"player"`/`"planner"`）；物化输入必填（非空），输出与输入同值 |
| preset | string | 引用的 preset 资源名（物化输入必填；输出为物化时快照） |
| model | string | 模型 id（输入可空=部署默认，与 ListModels 目录同源校验；输出为物化生效值） |
| system_prompt | string | output-only，仅 GetTeamMember 返回：该实例当前生效的完整系统提示词装配结果（persona + team section + 工具守则 + [planner] 记忆快照） |

### Preset

分池的角色配置模板（FR-006，现有资源演进）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/presets/{preset}`（同 template 内 id 全局唯一，跨池不重复） |
| role | string | 场景词汇（saolei 下 `"player"`/`"planner"`）——create 必填（经 `CreatePresetRequest.role`）、不可变；决定所属池与绑定的工具插件行 |
| persona | string | 唯一用户可编辑字段；第一人称角色身份 + 人格 + 职责（R2 边界：不含团队级事实）；空值回退角色默认 base |
| create_time / update_time | Timestamp | 现有语义 |

**存储**：Mongo `game_agent_v2.presets`（roster authoring Store 的 Mongo 实现，记录含 role/persona/时间戳）；preset 组合文件（含角色工具插件行）由 copy-then-patch 创作（模板 preset → 副本 → patch persona），副本可从 store 记录重建（进程内存态语义）。
**角色锁定**：`"player"` 池模板内置 `@dominion/dsh-saolei` 工具插件行；`"planner"` 池模板内置 `@dominion/dsh-memory` 插件行——行级绑定，工具与配套守则同生共死（FR-006）。

### TeamMessage（团队视图条目）

`ListTeamMessages` 返回的归并序列元素（FR-014）。

| 字段 | 类型 | 说明 |
|---|---|---|
| member | string | 消息产出者：保留值 `"user"`（用户输入）或成员 role（场景词汇，saolei 下 `"player"`/`"planner"`）；空字符串=未设置 |
| message | HistoryMessage | 原生输出（正文/思考/工具调用块，与现有 HistoryMessage 同构）；member=`"user"` 时为用户输入 |
| seq | int64 | 归并序（跨成员时间排序锚，单调） |

条目追加归并序列时经 team 流的 `team_message` 帧扇出（见下"ChatEvent 扩展"），帧载荷与本条目同构、seq 同源同值（[contracts/team-api.md](contracts/team-api.md) §3.2）。

### MemberViewMessage（成员视角历史条目）

`ListMemberMessages` 返回的成员视角序列元素（FR-015）。

| 字段 | 类型 | 说明 |
|---|---|---|
| message | HistoryMessage | 该成员视角的消息（HistoryMessage.role 枚举不变：USER 或 AGENT） |
| sender | string | 消息来源标注：保留值 `"user"`（用户输入）或成员 role（team-broadcast 注入，渲染为 `user: [sender] 正文`——sender 为 role 字符串原值，saolei 下如 `user: [player] …`）；空字符串=未设置 |

HistoryMessage 自身结构沿用现有（role/blocks/create_time/interrupted），本 feature 不改其形态。

### ChatEvent 扩展（team 流）

现有 ChatEvent 词汇（queued/turn_start/block_start/delta/block_end/tool_result/turn_end）保持。帧分两级（[contracts/team-api.md](contracts/team-api.md) §3.2）：

**成员事件帧**（`turn_start`/`block_start`/`delta`/`block_end`/`tool_result`/`turn_end`）增加：

| 字段 | 类型 | 说明 |
|---|---|---|
| member | string | 该事件的产出成员 role（场景词汇，saolei 下 `"player"`/`"planner"`）——仅成员事件帧设值，team 级帧（queued/team_message）不设（空字符串=未设置）；block index 全局单调与 step 语义以成员回合为单位（team 流跨多回合，turn_id 每回合独立铸造） |

**team 级帧**（不设 `member` 外层字段）：`queued`（既有，排队回执）与新增 `team_message`：

| 字段 | 类型 | 说明 |
|---|---|---|
| member | string | 归并条目产出者（与 TeamMessage.member 同型同值）：保留值 `"user"`=用户消息、成员 role=成员产出 |
| message | HistoryMessage | 原生消息（与 TeamMessage.message 同构） |
| seq | int64 | 归并序锚——与 `ListTeamMessages` 返回元素的 seq 同源同值（同一归并序列分配） |

### SystemPrompt（实例化结果）

非独立资源——TeamMember 的 output-only 字段（FR-016）。装配来源与所有权（R2 决策边界）：

| 内容 | owner |
|---|---|
| 第一人称角色身份 + 人格（persona 行） | preset |
| 团队目标 + 成员名册（第三人称一句话职责）+ 广播格式约定 | team 插件（team section，order 1–49） |
| 工具守则（saolei guidance / memory 无 guidance） | 工具插件（order 100–199） |
| 长期记忆快照（仅 planner，order 200+，实例生命周期固定） | memory 插件 |

### Memory 条目（既有，零变更）

沿用 memory 服务资源模型 `templates/{template}/sessions/{session}/memories/{memory}`（`projects/game/game.proto` MemoryService + Mongo `game_memory`）；scope 键 (template, session)。

## 3. 进程内模型（非对外资源，实现锚点）

| 模型 | 持有者 | 说明 |
|---|---|---|
| 成员 session log | dsh（官方 agent-loop 驱动） | 每 agent 一份 append-only 日志；成员视角历史/团队视图/广播条目的唯一事实源（"team-visible means logged"） |
| team buffer | team 插件 | per-member **待消费引用列表**（messageId/callId 锚点，无消息内容副本）；drain 时 team 内部按锚点读成员 log 构造注入就绪消息（索引不对外暴露）；消费锚点 = receiver log 中 team-broadcast `messageId` 集合；非事实源、不持久化（重启重物化）；无全局消息副本队列（中转条目在投递完成后即移除） |
| 编排状态机 | saolei-loop | 交替激活（current member）、续驱标志（暂停/运行）、排队用户消息队列、游戏事件流（gameEnded 事实） |
| team 事件流扇出 | agent_v2 宿主（会话面） | session 的活跃 team 流集合（Send 建立、持续至 team 静止，[contracts/team-api.md](contracts/team-api.md) §3）；编排与成员事件的订阅面/扇出面——流断开不影响编排（取消编排仅经 Cancel RPC），无活跃流时事件照常固化入历史投影；`team_message` 帧与 List 面共用同一归并序列投影（seq 同源） |
| GameRuntime | saolei-loop 插件（game 模块，agent-scoped `saoleiGame`，player 侧） | 棋盘/规则/操作执行/胜负判定，desktop 派发绑定 sessionName（沿用 051 归属） |
| memory 快照缓存 | memory 插件 | `Map<ScopeKey, string>`，物化 setup 预取填充，section 求值读取 |

## 4. 校验规则（从需求导出）

- UpdateTeam（服务端两层校验，fail-fast，无半物化）：结构校验（场景无关）——members 非空、每成员 role 非空、preset 为合法 preset 资源名、model 可空；saolei 场景校验（agent_v2 为场景宿主，KNOWN_TEMPLATES 机制不变）——members 恰 2 且 role 集合恰为 `{"player", "planner"}`、preset 存在且 preset.role 与成员 role **字符串相等**、model 非空时在 ListModels 目录。任一失败 → `INVALID_ARGUMENT`（错误为场景校验表述，不含"proto 限制"色彩）；结构细节见 [contracts/team-api.md](contracts/team-api.md) §2。
- Send：未物化 team → FAILED_PRECONDITION（引导物化）。
- preset：id 语法 `[a-z0-9][a-z0-9-]*`、同 template 唯一（跨池）；role 为场景词汇字符串（saolei 下 `"player"`/`"planner"`）——create 必填（非空且为已知场景词汇：决定 copy-then-patch 的拷贝源池模板，非法值 INVALID_ARGUMENT）、create 后不可变；update_mask 仅 persona。
- 删除 preset：无 fan-out（已物化 team 不受影响；再次物化引用 → INVALID_ARGUMENT/NOT_FOUND）。
- 成员数量与 role 值域在 proto 层无约束（场景无关原语）；saolei 场景恰 2 成员（player + planner）由服务端物化校验承载，无成员增删 API。

## 5. 状态转移（编排状态机，实现语义）

```text
未物化 ──UpdateTeam──▶ 物化中 ──成功──▶ [静止等待: planner 激活] ──用户首条消息──▶ [驱动 planner: 开局策略] ──回合结束──▶ [驱动 player: 执行]
   ▲                                                                                             │
   │                                                                      排队消息? ──是──▶ [当前成员消化] ──┐
   │                                                                                             │ 否              │
   │                                                                      gameEnded? ──是──▶ [驱动 planner: 复盘] ──回合结束──┐
   │                                                                                             │ 否                              │
   │                                                                      player 回合结束 ──结构性续驱 player（下一轮）◄──────┘──┘
   │              （静止等待态：物化成功后停留于此（初始激活 = planner），等待用户首条消息；Cancel 暂停续驱后停留，等待用户再次 Send；运行中无未消费输入时同样静止在当前激活成员）
   └──刷新（UpdateTeam）：任意状态 → 终止在途回合 → 清空记忆 → 重新物化
```

team 流终点（[contracts/team-api.md](contracts/team-api.md) §3.1）：Send 建立的实时流在编排状态机到达静止态（无在途回合且无待消化输入）时结束——含自然收敛与 Cancel 后的暂停静止。

切换锚点（player/planner 互切的精确条件）：

- **驱动输入与静止**（执行期用户裁定，2026-09-10）：物化成功后编排层静止等待——初始激活成员 = planner（初始相位 = planning），不自动驱动任何成员；游戏的首次驱动由用户第一条消息触发（首驱输入 = 用户消息本身，此时 drain(planner) 为空）。此后一切驱动（结构性续驱、gameEnded 复盘驱动、排队消化）的输入 = 目标成员 drain 返回的未消费团队消息 + 排队用户消息，编排层不合成任何驱动消息（FR-009/FR-010 完全落实）；无未消费团队消息且无排队消息时，编排层保持静止在当前激活成员（不合成消息、不空转）——"结构性续驱"只在存在未消费输入时发生；取消暂停后再次 Send 恢复（FR-017：再次 Send 即有输入即驱动）。
- **player → planner**：切换发生在游戏结束后。gameEnded 事实的确立来源是 saolei 工具返回游戏终局结果（won/lost，工具结果层面；即 `saolei_operate` 的终局结果——终局记录由 GameRuntime 在该次工具执行内写入，`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts`），编排层经 `peekGameEvent` 读取判定；player 回合结束时的切换评估按上图优先级执行（排队消息先消化 → gameEnded? → 结构性续驱）。
- **planner → player**：切换节点是 planner 完成静止——turn 结束并且不会再触发新的 turn（两种新 turn 触发源都不存在：工具调用引发的后续 turn、待消化排队消息）。上图中 planner（开局策略/复盘）的"回合结束"均指此静止条件；存在待消化排队消息时先由 planner 消化（消化优先于切换），确认静止后才续驱 player。
