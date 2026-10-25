# Contract: 主子会话的 face 语义映射（survey S2 关闭）

> Feature: [spec.md](../spec.md)（FR-006/FR-007）| 数据模型：[data-model.md](../data-model.md) §4-§5 | 决策：[research.md](../research.md) D3/D4/D6
> 本契约定义 agent-v2 对话面（AgentService，`projects/game/agent_v2.proto`）对**主子形态场景**的语义映射。前置裁定：子代理可见性 = **名册固定**（spec 决策基线 5）。proto 零变更（FR-006）——场景无关 team 原语承载全部映射。

## §1 UpdateTeam：单主 agent 物化

| 项 | 语义 |
|---|---|
| 物化输入 | `members = [{role: "main", preset, model?}]`——恰单成员，role 必须等于场景 roster（sts2 = `main`）；`preset` 为 `templates/sts2/presets/{preset}` 且其 `role = "main"`；测试/验收建的 preset persona MUST 为空（保留 main 池模板 prefix base 与锚串；非空 persona 覆盖 prefix，见 `common/js/dsh-plugins/preset-authoring/src/derive.ts`）；`model` 可空 = 部署默认，非空时复合标识在 ListModels 目录 |
| 校验 | 两层 fail-fast（结构层 + 场景层），错误码 `INVALID_ARGUMENT`，时序先于任何 teardown——与 saolei 同构（[scene-registry.md](scene-registry.md) §4） |
| 物化产物 | 名册 = 场景成员投影（`[{main, agent}]`，**无服务端附加成员**——无 announcer）；物化成功后静止等待，`active_member = "main"`（activation） |
| 刷新 | 再次 UpdateTeam：终止在途回合（`turn_end{ABORTED}`）、未消费输入作废、释放 main agent（含历史）、按新配置重建；`create_time` 保留、重复 Update 幂等——与 [specs/059-agent-v2-team-mode/contracts/team-api.md](../../059-agent-v2-team-mode/contracts/team-api.md) §2 同构 |
| 失败 | 无半物化（失败整体回滚，`GetTeam` NOT_FOUND，可重试）；同 session 并发物化串行 |

未物化面：`Send`/`Cancel` = `FAILED_PRECONDITION`；`GetTeam`/`GetTeamMember`/List 面 = `NOT_FOUND`（不变）。

## §2 名册固定（子代理可见性裁定）

**物化名册在物化时固定，仅含场景 roster 成员**（主子场景 = 单 `main`）：

| face | 名册内角色（`main`） | 名册外角色（未来子代理，如房间角色） |
|---|---|---|
| `GetTeam.members` / `UpdateTeam` 输出 | ✅ 成员资源（name/role/preset/model） | ❌ 永不出现 |
| `GetTeamMember` | ✅（含 output-only `system_prompt`，实例装配面读取） | ❌ `NOT_FOUND` |
| `ListMemberMessages` | ✅ 成员视角 | ❌ `NOT_FOUND`（无成员视角——子代理不接收用户输入） |
| `ListTeamMessages` / `team_message` 帧 | ✅ 归并序列条目 | ✅ **成员标注条目可见**（member 标签 = 信封 role） |
| Send 流成员事件帧 | ✅ | ✅ **成员标注帧可见**（`turn_start`/`delta`/… 外层 member 标注） |

- 名册来源：场景注册的 `faceMembers`（仅成员名）与编排 `members()` 投影一致；host 的成员名预检/roster 错误文案由前者派生，不保留 announcer 字面量（[scene-registry.md](scene-registry.md) §1/§4）。
- 机制：host 对统一成员事件流的名册外信封按 **`(role, memberId)`** 动态投影（[scene-registry.md](scene-registry.md) §3）——viewless collector 产出帧与归并条目、无资源/无成员视角；条目销毁/刷新不跨代保留。
- 本 feature 仅落地机制（无子代理生产者；单测以合成信封验证）；子代理事件的**生产**由后续 sts2-loop feature（survey §5 S3）承载，本表即其接口约束。
- 被否决案（记录于 spec 决策基线 5）：动态名册（成员资源命名/名册涨缩/刷新窗口交互复杂度过高）、完全不可见（与 S5 呈现目标相悖）。

## §3 active_member 与静止语义

| 状态 | `active_member` |
|---|---|
| main 回合在途 | `"main"`（驱动成员） |
| 静止 | `"main"`（activation；取消/冻结不改变归属——单成员下 activation 恒 `main`） |
| 未物化 | 不呈现（现状不变） |

- 流收束：静止点（无在途回合且无待消化输入）结束 Send 流；自然收敛与 Cancel 后冻结静止都属静止点（不变）。
- `Cancel`：终止在途回合（`turn_end{CANCELED}`）+ 冻结 + 未消费用户输入落地为已固化历史；幂等；再次 Send 即恢复（不变）。
- 排队：在途时到达的用户消息 steer 入队，本流首帧 `queued{position}`（`position` = steered 计数 + 1）；单成员下 inbox 无广播成分，`queued` 即 steered-in-flight 计数。

## §4 错误码与文案

- 校验类：`INVALID_ARGUMENT`（roster 形状、preset 存在性/role 匹配、model 目录、复合标识格式——文案由 template + 场景表字段构造，机制与 saolei 一致）。
- 驱动失败：成员回合失败经 `turn_end{ERROR}` 干净收束（引擎消息随帧）；编排层失败以流错误收尾（`INTERNAL` + 六码用户动作文案，`LOOP_ERROR_TEXT` 机制不变）。
- 不新增错误码；`TeamSessionErrorCode` 不变。

## §5 proto 零变更声明

`projects/game/agent_v2.proto` 的 AgentService 是场景无关 team 原语（无 role 枚举、无场景字段）；单成员 `members` 列表与字符串 role 天然承载主子物化，无任何字段/方法/注释变更。gateway 路由、proxy owner 亲和、web 消费面均不感知本映射（web 本 feature 固定 saolei，零改动——spec Assumptions）。
