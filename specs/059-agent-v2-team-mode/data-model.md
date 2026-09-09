# Data Model: Agent v2 Team 模式迁移

> 实体与资源模型（Phase 1）。资源命名与 RPC 契约详见 [contracts/team-api.md](contracts/team-api.md) 与 [contracts/preset-api.md](contracts/preset-api.md)；存储实现选型依据见 [research.md](research.md) R3/R4。

## 1. 资源层级

```text
templates/{template}                        # template（现有，KNOWN_TEMPLATES 仅 saolei）
├── sessions/{session}                      # Session（现有，/api/v1 管理，不变）
│   ├── team                                # Team —— session 的单例资源（AIP-156，替代原 agent 单例）
│   │   └── members/{member}                # TeamMember —— 固定两个：members/player 与 members/planner（role 即 id）
│   │       ├── (system_prompt)             # output-only 字段（GetTeamMember 返回）
│   │       └── messages                    # 成员视角历史（ListMemberMessages）
│   └── (connect)                           # DesktopBridgeService.Connect（现有，session 单位，不变）
└── presets/{preset}                        # Preset（现有路径，增加 role 字段分池）
```

member 资源名以 role 为 id（`members/player`、`members/planner`）——成员由 team 物化创建、无独立生命周期，id 即角色，避免引入可变成员 id。

## 2. 实体定义

### Team

session 的团队单例（FR-004/FR-005）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/sessions/{session}/team` |
| player_preset | string | player 成员引用的 preset 资源名（必填，池必须为 PLAYER） |
| planner_preset | string | planner 成员引用的 preset 资源名（必填，池必须为 PLANNER） |
| player_model / planner_model | string | 各成员模型 id（可选，缺省部署默认；与 ListModels 目录同源校验） |
| members | TeamMember[] | output-only：两个成员的运行时状态 |
| desktop_connected | bool | output-only：session 级桌面连接状态（player 独占使用） |
| create_time / update_time | Timestamp | create_time 跨刷新保留、update_time 刷新更新（对齐现状 Agent 语义） |

**生命周期**：未物化（无 team 记录，Send 拒绝并引导）→ 已物化（UpdateTeam 创建）→ 刷新（UpdateTeam 再次调用：终止在途回合、排队作废、清空全部成员短期记忆、按新配置重建；create_time 保留）→ 随进程内存态丢失（重启后回到未物化，既有语义）。

### TeamMember

team 的固定成员（无独立 CRUD；随 team 物化/刷新生灭）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/sessions/{session}/team/members/{player\|planner}` |
| role | enum | PLAYER / PLANNER |
| preset | string | 物化时引用的 preset（output-only） |
| model | string | 物化时模型（output-only） |
| system_prompt | string | output-only，仅 GetTeamMember 返回：该实例当前生效的完整系统提示词装配结果（persona + team section + 工具守则 + [planner] 记忆快照） |

### Preset

分池的角色配置模板（FR-006，现有资源演进）。

| 字段 | 类型 | 说明 |
|---|---|---|
| name | string | `templates/{template}/presets/{preset}`（同 template 内 id 全局唯一，跨池不重复） |
| role | enum | PLAYER / PLANNER——create 必填、不可变；决定所属池与绑定的工具插件行 |
| persona | string | 唯一用户可编辑字段；第一人称角色身份 + 人格 + 职责（R2 边界：不含团队级事实）；空值回退角色默认 base |
| create_time / update_time | Timestamp | 现有语义 |

**存储**：Mongo `game_agent_v2.presets`（roster authoring Store 的 Mongo 实现，记录含 role/persona/时间戳）；preset 组合文件（含角色工具插件行）由 copy-then-patch 创作（模板 preset → 副本 → patch persona），副本可从 store 记录重建（进程内存态语义）。
**角色锁定**：PLAYER 池模板内置 `@dominion/dsh-saolei` 工具插件行；PLANNER 池模板内置 `@dominion/dsh-memory` 插件行——行级绑定，工具与配套守则同生共死（FR-006）。

### TeamMessage（团队视图条目）

`ListTeamMessages` 返回的归并序列元素（FR-014）。

| 字段 | 类型 | 说明 |
|---|---|---|
| member | enum | USER / PLAYER / PLANNER——消息产出者 |
| message | HistoryMessage | 原生输出（正文/思考/工具调用块，与现有 HistoryMessage 同构）；USER 时为用户输入 |
| seq | int64 | 归并序（跨成员时间排序锚，单调） |

### MemberViewMessage（成员视角历史条目）

`ListMemberMessages` 返回的成员视角序列元素（FR-015）。

| 字段 | 类型 | 说明 |
|---|---|---|
| message | HistoryMessage | 该成员视角的消息（role: USER 或 AGENT） |
| sender | enum | 消息来源标注：USER（用户输入）/ PLAYER / PLANNER（team-broadcast 注入，渲染为 `user: [sender]...`） |

HistoryMessage 自身结构沿用现有（role/blocks/create_time/interrupted），本 feature 不改其形态。

### ChatEvent 扩展（实时流）

现有 ChatEvent 词汇（queued/turn_start/block_start/delta/block_end/tool_result/turn_end）保持，**每帧增加**：

| 字段 | 类型 | 说明 |
|---|---|---|
| member | enum | PLAYER / PLANNER——该事件的产出成员（user 消息入流时的排队帧不设 member，属 team 级） |

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
| GameRuntime | saolei 插件（agent-scoped `saoleiGame`，player 侧） | 棋盘/规则/操作执行/胜负判定，desktop 派发绑定 sessionName（沿用 051 归属） |
| memory 快照缓存 | memory 插件 | `Map<ScopeKey, string>`，物化 setup 预取填充，section 求值读取 |

## 4. 校验规则（从需求导出）

- UpdateTeam：preset 必填且 role 匹配（player_preset.role=PLAYER、planner_preset.role=PLANNER）→ 否则 INVALID_ARGUMENT；model 必须在 ListModels 目录 → 否则 INVALID_ARGUMENT；任一失败不产生半物化（fail-fast）。
- Send：未物化 team → FAILED_PRECONDITION（引导物化）。
- preset：id 语法 `[a-z0-9][a-z0-9-]*`、同 template 唯一（跨池）；role create 后不可变；update_mask 仅 persona。
- 删除 preset：无 fan-out（已物化 team 不受影响；再次物化引用 → INVALID_ARGUMENT/NOT_FOUND）。
- 成员数恰为 2（player + planner），无成员增删 API。

## 5. 状态转移（编排状态机，实现语义）

```text
未物化 ──UpdateTeam──▶ 物化中 ──成功──▶ [驱动 planner: 开局策略] ──回合结束──▶ [驱动 player: 执行]
   ▲                                                                            │
   │                                                     排队消息? ──是──▶ [当前成员消化] ──┐
   │                                                                            │ 否              │
   │                                                     gameEnded? ──是──▶ [驱动 planner: 复盘] ──回合结束──┐
   │                                                                            │ 否                              │
   │                                                     player 回合结束 ──结构性续驱 player（下一轮）◄──────┘──┘
   │                    （续驱暂停态：Cancel 后停留，等待用户 Send 恢复）
   └──刷新（UpdateTeam）：任意状态 → 终止在途回合 → 清空记忆 → 重新物化
```
