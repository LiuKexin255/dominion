# Contract: announcer 成员更名与成员化呈现（名册 / active_member / 成员读取 / 校验放宽）

**Feature**: [../spec.md](../spec.md)（FR-201–FR-206）| **结构定义**: [../data-model.md](../data-model.md) §4 | **设计依据**: `survey/saolei-member-switch-flow.md` 裁定 23 与 §12.2
**实现锚点**: `common/js/dsh-plugins/saolei-loop/src/announcer.ts`、`projects/game/agent_v2/src/{session,history}.ts`、`projects/game/agent_v2.proto`（注释）、`projects/game/web/frontend/src/`
**基线契约修订**: `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3 item 4 / §4（"proto 面零改动/GetTeam 不含 saolei"条款推翻——本契约即修订内容）；`specs/059-agent-v2-team-mode/contracts/team-api.md` 与 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §1（active_member/名册语义增量勘误）

## 1. role 名（终态；旧名不留别名、无迁移）

| 项 | 终态 |
|---|---|
| role 常量 | `ANNOUNCER_MEMBER_ROLE = "announcer"` |
| 名册 summary | `ANNOUNCER_MEMBER_SUMMARY`（announcer 语义文案，如「终局播报对局结果与操作统计」） |
| 类名 | `AnnouncerMember` |
| source id 前缀 | `${session}/announcer` |

波及面：`projects/game/agent_v2/src/session.ts` 的订阅投影（`appendAnnouncement(ANNOUNCER_MEMBER_ROLE, ...)`）、`history.ts` 成员标签、`orchestrator.ts` 的注册/默认成员摘要（`DEFAULT_MEMBER_SUMMARIES` 键）、`common/js/dsh-plugins/saolei-loop/src/index.ts` 导出面、全部相关测试断言、065 契约文档、`projects/game/agent_v2/README.md`。旧 role 值与旧导出名一律不留（宪法原则 VII 终态表述；实施期旧名→新名对照见 [../tasks.md](../tasks.md) T007/T012）。会话状态为进程内存态（stateful + owner 亲和），无持久化迁移。

## 2. proto 面（消息形状零变更，注释修订）

`Team.members`（repeated TeamMember）、`Team.active_member`（string）、`TeamMember`（name/role/preset/model/system_prompt）消息字段全部不变——announcer 进入名册是**宿主行为**。proto 注释修订（消息字段零变更）：`projects/game/agent_v2.proto` 中"player/planner members"（:27-28）、"GetTeamMember returns one fixed member (members/player or members/planner)"（:69-71）、`TeamMember.role` 注释 "saolei: player / planner"（:301-304），以及其余 role 词汇注释——`TeamMessage.member`（:404）、`sender`（:422）、成员事件帧 `member`（:648）、`MemberViewEvent.member`（:677）——统一改为三成员/announcer 表述。

## 3. 宿主呈现契约（`projects/game/agent_v2/src/session.ts`）

1. **名册**：`toTeamView` 返回三成员——`[player, planner, announcer]`；announcer 项 = `name: .../team/members/announcer`、`role: "announcer"`、`preset: ""`、`model: ""`、`systemPrompt: ""`（非 LLM 成员名册形态；数据模型 §4）。
2. **active_member**：单一合并值语义保持（`active ?? activation`，060 §1）；announcer 播报期间激活权归属 announcer，`active_member` 可为 `"announcer"`——无呈现投影特例（不跳过 consumes=false 成员）。呈现可达性受同步执行窗口影响（微任务级激活窗口），是成员自身特性，不作框架保证。
3. **成员读取**（GetTeamMember 路径）：以与 LLM 成员一致的资源名访问方式服务 `members/announcer`——放宽现状硬编码 `player`/`planner` 的 roster guard 后返回 announcer 成员项（output-only `system_prompt` 为空值）；announcer 无 `AgentHandle`/`Agent`，不经 `orchestrator.member(role)` 与 `readMemberSystemPrompt`（LLM 成员的 generation 校验/重读语义保持）。
4. **校验边界**：`validateSaoleiMembers` 的**请求侧**校验保持（UpdateTeam 请求必须恰含 player/planner 两个 LLM 成员配置，preset 必填规则不变）——announcer 不可由调用方配置，由服务端进入名册；名册/读取面不因 announcer 项的存在判为非法（放宽点在视图构造与成员查找，不在请求校验）。裁定口径：`survey/saolei-member-switch-flow.md` §12.2 表述为「`validateSaoleiMembers` 两成员校验放宽」，本契约按 spec FR-203 收窄——放宽只落在名册构造与成员查找面，请求侧校验保持严格。
5. **类型**：宿主 `MemberRole` 与 saolei-loop `TeamRole` 纳入 `"announcer"`。

## 4. web 前端契约

1. 名册渲染（`App.tsx` 的 `team?.members?.map`、`TeamSettingsPanel.tsx` 的成员列表）对 announcer 项不报错、不忽略：以无 preset/model 形态展示（空值呈现）。
2. preset/model 下拉与提交（`TeamSettingsPanel` 按角色过滤 player/planner）不受 announcer 名册项影响。
3. 激活指示与团队消息成员标签：wire 角色串直接渲染（现状透传路径），announcer 播报消息标签呈现为 `announcer`。

## 5. 顺序约束（spec FR-206）

announcer 名册放宽**先行或同批**于 flow 标准化重构（批2 先于批3，research.md D2）：重构落地后激活权真实短暂归属 announcer，名册未放宽期间不得作为长期状态。

## 6. 验收面（对应 spec SC-006）

单测：三成员名册（GetTeam/UpdateTeam 视图）；GetTeamMember 服务 announcer（system_prompt 空值）；请求侧校验拒绝 caller-supplied announcer 配置；web 名册/激活渲染。大型测试：conversation 断言更新（`member="announcer"` 统计消息、`GetTeam` 名册含 announcer）。
