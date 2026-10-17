# Feature Specification: 成员统一性补缺（announcer 同权 + team 统一消息流 + 在途广播快路）

**Feature Branch**: `077-announcer-member-unification`

**Created**: 2026-10-16

**Status**: Draft

**Input**: User description: "先行处理 @survey/saolei-hook-scripts.md 中 K4 的缺口，以及其他的 announcer优化，为 hook 功能做准备。"

## 背景与调研基线

本 feature 是 hook 功能（planner 经工具固化 JS 策略脚本、player 每次棋盘批量操作后执行并播报）的**前置分层补缺**，调研基线为 `survey/saolei-hook-scripts.md`（2026-10-16 十轮裁定/澄清）。范围经用户确认（2026-10-16）：K4 两项缺口 + 在途广播快路，无其他特定项。三项工作：

1. **K4 缺口②（team 统一成员消息流）**：loop 层与 team 层已有统一成员抽象（`Member`/`MemberExecution`，`common/js/dsh-plugins/saolei-loop/src/member.ts`；`TeamMemberSource`，`common/js/dsh-plugins/team/src/team.ts`），但宿主投影层绕过两套抽象——两个 `MemberCollector`（`projects/game/agent_v2/src/history.ts`）在 dsh 根上下文逐成员自行订阅四类事件（`session/event`/`agent/assistant-stream`/`agent/status`/`agent/error`，按 session id 过滤），announcer 再加一条独立 source 订阅（`projects/game/agent_v2/src/session.ts` 的 `announceOff`）——共三条互不相干的订阅路径。目标（survey §2.4）：team 插件提供统一的 team/member 消息流，宿主从该面单路消费成员产出。
2. **K4 缺口①（announcer 宿主统一 member 化）**：宿主未把 announcer 当作统一 member 处理的特例代码（`projects/game/agent_v2/src/session.ts`：`TeamEntry.members` 只含 agent 角色并另设 `announceOff` 旁路字段、独立播报订阅、`announcerMemberView` 平行构造、`toTeamView` 名册拼装特判、`GetTeamMember` announcer 提前 return 分支、`isMemberRole`/`isAgentMemberRole`/`activeAgentMember` 角色字符串判定辅助）。076 已完成呈现面契约（名册/active_member/成员读取，`specs/076-agent-v2-optimize/contracts/announcer-member.md`），本 feature 清理实现侧特例：成员视图与名册按成员属性派生（如 `consumes: false` ⇒ 无成员视角投影），而非按角色字符串特判。
3. **在途广播快路（survey §2.2.4/§2.2.5 已裁定形态）**：插入式产出（发送方为非激活成员的 message 产出，现状唯一生产者是 announcer 的终局统计播报）目前只进 pending 列表、在成员**下次驱动装配时**才 drain 可见（`common/js/dsh-plugins/team/src/team.ts` relay/pending）；快路使其以正常广播消息形态（同锚点、同 drain 构造路径）经不唤醒投递进入**当前在途成员**的 next-step inbox，下一 step 边界认领——这是 hook 播报"player 同回合下一 step 可见"（survey D7=b）的投递基建。

**D8 裁定落地**：survey 中 D8（分层补缺范围与时机，pending）由本 feature 落地为**独立 feature 先行**——本 feature 先于 hook feature 交付，hook feature 只剩 hook 引擎本身（游戏基础事件 seam、hooks 插件、沙箱与工具面）。

**快路的已裁定语义**（survey §2.2.4/§2.2.5）以需求条目承接：投递规则与接收方唯一性、投递面归属与成员侧统一策略、与 pending 的叠加关系（恰好一次双向自愈）、发言级顺序一致性与驱动/flow 零影响，分别见 FR-007–FR-011（本节不复述）。

## Clarifications

### Session 2026-10-16

- Q: 本 feature 的大型测试验收，是否只需既有 agent_v2 testplan 套件全部回归通过（快路行为由 bazel 单测覆盖），还是必须新增一个可构造的快路大型测试场景？ → A: 只需既有套件全部回归通过；快路行为由 bazel 单测覆盖，不新增快路专用大型测试场景（hook 落地前大型测试无法经正常链路触达快路；真实端到端快路场景由后续 hook feature 携带）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - team 统一成员消息流与宿主投影收敛 (Priority: P1)

作为 agent_v2 宿主与 team 插件的维护者，我要 team 插件成为宿主消费的**成员消息流权威**：宿主的全部成员投影（web ChatEvent 帧、归并历史、成员视角、turn 生命周期收束）从 team 提供的统一成员事件流**单路**消费——成员产出以成员标注的事件到达，announcer 与 agent 成员端到端同权，不再有逐成员下探 dsh 根事件的三路互不相干订阅。

**Why this priority**: K4 缺口②是分层缺口的结构核心：`TeamMemberSource.subscribe` 已对 agent 成员与 announce-only 成员统一产出通知，但宿主投影（流式 delta、工具帧、成员视角、turn 收束）不消费它而自行下探。hook 功能将 announcer 流量从"每局一条"放大到"每次操作 × N hooks"，特例路径承压放大——按宪法 II（重构式变更：架构调整与功能变更同步交付），分层补缺在 hook 功能前先行（用户 D8 裁定）。

**Independent Test**: 可独立开发与验证——投影行为等价验收：既有投影单测（`projects/game/agent_v2/src/session.test.ts` 全量、history 投影断言）与 agent_v2 大型测试回归全绿；代码面上三条订阅路径（两 MemberCollector 根事件订阅 + announcer 独立订阅）收敛为经 team 统一面的单路。

**Acceptance Scenarios**:

1. **Given** 物化后的 team，**When** 任一 agent 成员产出（发言、工具调用/结果、流式增量、turn 状态迁移），**Then** 宿主投影（web 帧、归并历史、成员视角、turn 收束帧）经 team 统一成员事件流到达，内容与帧序与现状等价（投影对外行为零变化）。
2. **Given** announcer 播报一条消息，**When** 播报发生，**Then** 归并条目 + `team_message{member="announcer"}` 帧经统一流到达（与 `ListTeamMessages` 同源同值），宿主不再存在 announcer 专属的独立订阅路径。
3. **Given** 刷新（UpdateTeam）触发旧 entry teardown，**When** teardown 执行，**Then** 统一流订阅随团队注册释放退订，旧生命周期的任何产出（含旧 announcer 播报）不进入新 entry 的投影（无帧泄漏，既有刷新测试锚点保持）。
4. **Given** 维护者审查宿主投影入口，**When** 检查成员事件的消费方式，**Then** 成员事件只有 team 统一面一个消费入口，无按角色字符串或 dsh session id 的散布过滤逻辑。

---

### User Story 2 - announcer 宿主统一 member 化（特例代码消失） (Priority: P2)

作为 web 最终用户与宿主维护者，announcer 在宿主层的全部呈现与生命周期**按成员属性统一派生**：成员名册、成员读取、成员视角的有无、entry teardown 都由"它是注册成员 + announce-only 属性（`consumes: false`、无 agent runtime）"导出——`announcerMemberView` 特例构造、`GetTeamMember` 提前 return 分支、`announceOff` 旁路字段、角色字符串判定辅助等特例代码消失，对外行为保持不变。

**Why this priority**: K4 缺口①的实现侧收尾——076 已建立 announcer 呈现契约（三成员名册、`active_member` 可为 announcer、成员读取同权），但实现以角色字符串特判支撑；成员属性派生使后续新增非 LLM 成员（如 hook 相关成员形态）零特例成本。优先级低于 US1（依赖统一流收敛后 teardown/持有面才可完全统一）。

**Independent Test**: 可独立于 US3 开发、与 US1 部分耦合（订阅面收敛属 US1）——076 announcer 契约验收面回归（`specs/076-agent-v2-optimize/contracts/announcer-member.md` §6：三成员名册、GetTeamMember 服务 announcer、请求侧校验拒绝 caller-supplied announcer、web 名册/激活渲染）+ 特例代码消失的代码面检查。

**Acceptance Scenarios**:

1. **Given** 已物化的 team，**When** 调用 GetTeam，**Then** 名册返回三成员（player/planner/announcer），由注册成员集派生（announcer 项空 preset/model 形态保持）。
2. **Given** 成员读取面，**When** 以 `members/announcer` 资源名访问，**Then** 与 LLM 成员一致的访问方式可用（output-only `system_prompt` 空值），不经角色提前 return 特例分支。
3. **Given** 成员视角查询，**When** 访问 announcer 的 member view，**Then** 保持 NOT_FOUND——由成员属性派生（announce-only ⇒ 无成员视角投影），而非角色字符串硬编码。
4. **Given** entry teardown（刷新/关闭），**When** teardown 执行，**Then** 成员释放按统一成员列表进行，announcer 不再以独立字段/独立退订函数旁路存在。
5. **Given** 调用方物化/刷新 team，**When** 提交 UpdateTeam，**Then** 请求侧校验保持严格（恰含 player/planner 两 LLM 成员配置，announcer 不可由调用方配置——076 FR-203 口径不变）。

---

### User Story 3 - 在途广播快路（插入式产出 → 在途成员下一 step） (Priority: P3)

作为即将到来的 hook 功能（announcer 在 player turn 内高频播报 hook 分析）的基建消费者：插入式产出（发送方为非激活成员的 message 产出）送达**当前在途成员的下一个 step**——player 同一回合内即可看到播报内容并据此调整后续行为；静止成员照旧 pending drain；每成员恰好一次；成员间发言级顺序一致；驱动与 flow 决策零影响。

**Why this priority**: hook 功能的功能性基建（survey D7=b：hook 结果"下一 step 可见"经统一播报快路承担）；本 feature 交付后 hook feature 无需触碰投递机制。注意：hook 落地前快路在生产行为上惰性（现状唯一插入式生产者 announcer 只在 player 收束后播报终局统计，彼时无在途成员），机制以 bazel 单测验证——大型测试验收为既有套件全量回归、无新增快路专用场景（2026-10-16 范围确认与澄清，见 Clarifications）。

**Independent Test**: 可独立于 US1/US2 开发与验证——team 单测（在途成员投递、锚点闭包、静止降级）+ agent 成员适配器单测（next-step 注入不唤醒）；大型测试以既有套件现状行为不变回归验收（快路惰性，无新增快路专用场景——真实端到端快路场景由后续 hook feature 携带）。

**Acceptance Scenarios**:

1. **Given** player turn 在途（多 step 执行中）、announcer 产生插入式播报，**When** 播报发生，**Then** 该广播以正常广播消息形态（与 drain 完全相同的构造路径、同一锚点、`team-broadcast` source、`<announcer-message>` 渲染）经不唤醒投递进入 player 的 next-step inbox，下一 step 边界认领进 session log，下一 step 的模型请求自动携带；relay 副本被锚点闭包从 pending 剔除（恰好一次）。
2. **Given** planner 静止，**When** 同一播报发生，**Then** planner 照旧 pending → 下次驱动装配时 drain 消费（既有语义不变）。
3. **Given** 终局统计播报（flow announce 活动，player 已收束非在途），**When** 播报执行，**Then** 无在途接收方、无快路，走 pending drain，现状行为完全不变。
4. **Given** 在途成员的 next-step 边界同时有待消费的用户 steer 消息与快路广播，**When** step 认领，**Then** 两者按插入序 FIFO 认领，投影分离（steer 渲染 `user`；快路广播渲染 `user: [announcer] ...`，均为既有投影种类，零新增投影规则）。
5. **Given** cancel 打断在途成员且快路消息残留 inbox，**When** cancel 执行，**Then** 快路副本不进入 landed（landUnclaimed 只收编用户消息，`team-broadcast` 残留放行给 `agent.cancel` 清 inbox）；锚点未消费 → pending 基线在该成员下次驱动装配时经 drain 重投恰一份——不丢失、不重复（自愈重投单一路径，research.md D10）。
6. **Given** 多条插入式播报在同一 step 窗口内连续发生，**When** 在途成员推进 step，**Then** 各锚点独立闭包、FIFO 认领，无重排与重复。

---

### Edge Cases

- **终局统计播报（无在途成员）**：发送方为非激活产出但接收方不存在 → 无快路、pending drain，现状不变（快路启用条件 = 存在当前在途成员，非"发送方身份"）。
- **turn 收束边缘（`concludesTurn` / 模型停止）**：announce 在 turn 收束前发生 → 在途成员仍是接收方、快路成立；turn 随即收束 → 消息留存 inbox、由收束 turn 的延展步认领（循环停止条件 `turnEnds && inbox.nextStep.length === 0` 强制消化 next-step 残留）——时序等价 drain，锚点闭包防重。
- **turn 失败残留边缘**：turn 失败（非 cancel）不清 inbox（dsh-agent-loop 仅 cancel 清 pending work），快路副本残留 next-step；该成员下一次 turn 起点先把 inbox 中 `team-broadcast` 残留清扫（未认领 ⇒ 锚点未消费 ⇒ pending 派生重投恰一份），再装配新批——用户消息残留不在清扫范围（随 turn 边界照常认领，061 既有语义）。
- **cancel 边缘**：`orchestrator.cancel()` 的 landUnclaimed 先序只收编**用户**未消费消息为 landed；快路的 `team-broadcast` 残留不落地（放行给 `agent.cancel` 清 inbox），锚点未消费由 pending 重建自愈重投恰一份——见 US3 场景 5（landed 因此只承载用户输入，可靠性单一载体是 pending）。
- **快路注入异常**：投递面执行异常时回落 pending 基线（错误不破坏游戏流程；错误吞噬策略细节留 plan）。
- **刷新 teardown 中的在途快路**：entry teardown 终止在途回合（aborted），快路残留随 inbox 清理，锚点未消费不产生跨生命周期重投（旧团队 pending 已随 registration dispose 清空）。
- **announce-only 成员不是接收方**：`consumes: false` 非 relay 接收方，结构性无投递面（announcer 自己的播报不会回流给自己）。
- **顺序偏差披露（工具单元级）**：快路消息在接收方 log 中位于同 step 全部工具结果之后（step 边界认领），其他成员的生产序中它可能先于同 step 后继兄弟工具单元——偏差有界于单 step 窗口、发言级顺序不受影响（是否消除留 plan/测试阶段评估）。
- **多成员种类演进**：未来非 agent 消费成员（若有）结构性无投递面，不受快路影响；新插入式生产者（hook）复用同一路径，无需新机制。

## Requirements *(mandatory)*

### Functional Requirements

**A. team 统一成员消息流与宿主投影收敛（US1；K4 缺口②）**

- **FR-001**: team 插件 MUST 提供统一的成员事件流消费面，成为宿主消费成员产出的权威：以成员标注（成员身份 + 事件）覆盖宿主现有三类投影输入——终态产出（session 事件；agent 成员与 announce-only 成员同权）、流式增量（agent 成员独有）、turn 生命周期状态与错误（agent 成员独有）。agent-only 事件自然只从 agent 成员到来；team 保持"成员 = 消息源、不感知成员是 agent 还是系统角色"的依赖倒置（`specs/065-agent-v2-team-refine/contracts/team-member-source.md` §1）。
- **FR-002**: 宿主投影 MUST 单路化：现状三条互不相干的订阅路径（两个 `MemberCollector` 在 dsh 根上下文的逐成员订阅 + announcer 独立 source 订阅，`projects/game/agent_v2/src/history.ts`/`session.ts`）收敛为经 team 统一面的单路消费；announcer 播报的宿主投影（`appendAnnouncement` 归并条目 + `team_message` 帧）经统一流触发；entry teardown 的订阅释放单路化（`announceOff` 独立 teardown 消失）。
- **FR-003**: 投影对外行为 MUST 等价保持：web ChatEvent 帧序与内容、`ListTeamMessages`/`ListMemberMessages` 同源同值、成员视角消费时点、turn 收束帧、刷新/关闭 teardown 无帧泄漏——既有投影单测与大型测试回归全绿（行为等价是验收基准，非逐字实现保持）。

**B. announcer 宿主统一 member 化（US2；K4 缺口①）**

- **FR-004**: 宿主 entry 的成员持有 MUST 统一：`TeamEntry` 的成员集合容纳全部注册成员（player/planner/announcer），announcer 不再以独立字段（`announceOff`）或"members 只含 agent 角色"的旁路形态存在；成员 teardown 按统一成员列表进行。
- **FR-005**: 名册、成员读取与成员视图 MUST 按成员属性派生而非角色字符串特判：三成员名册与 `GetTeamMember` 的 announcer 服务形态（空 preset/model、output-only `system_prompt` 空值、不经 agent assembly）由注册成员集与成员属性导出；announce-only（`consumes: false`）⇒ 无成员视角投影（`ListMemberMessages` 对 announcer 保持 NOT_FOUND）；`announcerMemberView` 特例构造、`GetTeamMember` 的 announcer 提前 return 分支、`isAgentMemberRole`/`activeAgentMember` 类角色字符串判定辅助被属性/状态派生取代（类型层面的 agent/非 agent 成员区分允许保留，视图构造中的角色串特判不允许）。
- **FR-006**: 076 announcer 呈现契约 MUST 保持：三成员名册、`active_member` 可为 announcer、请求侧校验保持严格（UpdateTeam 恰含 player/planner 两 LLM 成员配置，announcer 由服务端进入名册、不可由调用方配置）——`specs/076-agent-v2-optimize/contracts/announcer-member.md` §3 验收面回归通过。

**C. 在途广播快路（US3；survey §2.2.4/§2.2.5 裁定形态）**

- **FR-007**: 插入式广播 MUST 送达当前在途成员：发送方为非激活成员的插入式产出（现状唯一场景：announcer 在其他成员 turn 内的播报）发生时，该广播以**正常广播消息形态**——与 drain 完全相同的构造路径（同锚点、同 `team-broadcast` source 形态、同渲染）产出，无快路特制消息形态——经**不唤醒投递**进入当前在途成员的 next-step inbox，在其下一 step 边界认领进入 session log，下一 step 的模型请求自动携带。
- **FR-008**: 快路 MUST 为框架统一投递规则（无 per-member 配置维度）：接收方 = 当前在途成员且仅此一个（"至多一个成员在途驱动"不变量保证）；成员投递面是 agent 成员**种类**的结构性属性——agent 成员适配器统一注册、统一策略（在途 → 不唤醒注入；静止 → no-op，即"我不是接收方"），非注册时的选择项；非 agent 成员结构性无此面；team relay 时对每个 consuming 成员调用投递面，策略过滤后实际生效的只有当前在途成员（机械遍历与语义单一接收方不矛盾）。
- **FR-009**: exactly-once MUST 由既有锚点闭包保证：快路与 pending 是**叠加关系**——relay/pending 恒为可靠基线照常发生，快路是并发的提前投递；同一锚点每成员恰好一次（接收方 log 出现 `source.messageId === 锚点` 的注入即闭包）：快路被认领 → pending 重建自动剔除 relay 副本（自愈去重）；快路被取消/丢弃 → 锚点未消费 → pending 照常 drain（自愈重投）；快路任何失败自动回落基线语义。
- **FR-010**: 成员间顺序一致性 MUST 达到发言级生产序：快路接收方与其他成员看到的共享消息相对顺序相同（= 生产顺序，assistant message 级完全一致）；工具单元级偏差 MUST 有界于单个 step 窗口内（快路消息相对同 step 后继兄弟工具单元的位置），不得影响发言级顺序（是否消除该有界偏差留 plan/测试阶段评估）。
- **FR-011**: 快路对驱动与 flow MUST 零影响：不唤醒驱动（不开新 turn、不延长在途 turn）、不触发 flow 决策、不影响 `whenIdle`/完成判定；快路消息不计入驱动输入装配记账（steered/queued 快照不含快路消息）；"编排不合成驱动消息"不变量保持——快路投递的是成员的真实广播产出（同锚点），驱动输入构成（drain + landed + 用户消息）不变，变化的只是既有广播的投递时机。
- **FR-012**: 静止成员与终局统计播报 MUST 行为不变：静止成员照旧 pending → 下次驱动装配 drain；终局统计播报（player 非在途、flow announce 活动路径）无快路、走 pending drain，现状行为保持——hook 落地前快路在生产行为上惰性为已接受状态。

**D. 契约与文档修订**

- **FR-013**: 受影响契约 MUST 修订：`specs/065-agent-v2-team-refine/contracts/team-member-source.md`（统一成员事件流面 + 成员投递面/快路投递规则增量）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（team 服务面：统一流与投递面；含"team 只投递不驱动"条目的投递面归属说明）、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`（广播投递时机增量表述 + §7 宿主成员事件消费改经统一流；loop 侧 `orchestrator.announcer` 读取面保留）、`specs/076-agent-v2-optimize/contracts/announcer-member.md`（§3 成员读取实现侧表述改为按成员 kind 属性派生）；`projects/game/agent_v2/README.md` 的成员投影与 announcer 描述同步更新。

### Key Entities *(include if feature involves data)*

- **统一成员事件流（unified member event stream）**: team 插件提供的成员标注事件流——宿主投影的单路输入，覆盖终态产出/流式增量/turn 生命周期三类事件；team 保持成员抽象依赖倒置。
- **成员投递面（member delivery face）**: agent 成员适配器结构性提供的 next-step 注入面（team relay 时调用）；类型上可缺省、语义上非配置项（成员种类的固有属性，与 `consumes` 同类）。
- **插入式产出（insertion production）**: 发送方视角的定义——非当前在途 turn 执行者的成员输出了 message（绕过正常 turn 时序的发言）；现状唯一生产者是 announcer，未来 hook 播报复用。
- **快路（in-flight fast-path）与 pending 基线**: 叠加关系——pending 恒为可靠基线，快路是并发的提前投递；锚点闭包保证恰好一次（认领去重/丢弃重投双向自愈）。
- **成员属性派生（attribute-derived projection）**: 宿主名册/读取/视图由注册成员集与成员属性（announce-only ⇒ 无 runtime/无视角/空形态）导出，取代角色字符串特判。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 宿主投影单路化达成且行为等价：`projects/game/agent_v2/src/` 中不再存在逐成员的 dsh 根事件订阅与 announcer 独立订阅（成员事件消费只有 team 统一面一个入口）；`projects/game/agent_v2/src/session.test.ts` 与 `projects/game/agent_v2/src/history.test.ts` 全量投影/记账断言（驱动面切换为统一流、断言不变）通过。
- **SC-002**: announcer 特例代码消失且呈现契约保持：`announcerMemberView` 特例构造、`GetTeamMember` announcer 提前 return 分支、`announceOff` 旁路字段在代码中不存在；076 announcer 契约验收面（三成员名册、成员读取、请求侧校验、web 渲染）回归通过。
- **SC-003**: 快路行为单测六项全过：①在途成员的下一 step 模型输入包含插入式广播（既有 `<announcer-message>` 广播格式渲染、零新增投影规则）；②同锚点恰好一次（快路认领后 pending 重建剔除）；③静止成员 pending drain 行为不变；④终局统计播报无快路、现状行为不变；⑤cancel 与 turn 失败后的恢复装配对同锚点恰一份（快路副本不落地 landed、inbox 残留被下一 turn 起点清扫——US3 场景 5）；⑥同 step 窗口多条播报 FIFO 投递序、各锚点独立闭包（US3 场景 6）。
- **SC-004**: 跨成员顺序一致性断言通过：快路接收方 log 中共享消息相对顺序与其他成员 drain 序一致（发言级 = 生产顺序）；工具单元级偏差断言有界于单 step 窗口。
- **SC-005**: 驱动零影响断言通过：快路注入不改变 turn 开闭与 `whenIdle` 完成判定、不计入 queued/steered 记账、不触发 flow 决策。
- **SC-006**: agent_v2 既有大型测试全套件（`projects/game/testplan/system_test.yaml`：game-system（含 conversation/game/wiki/web 各 module）、game-stall、game-memory-down、game-disconnect，经 testplan skill 实际执行部署→测试→清理闭环）全部用例通过。
- **SC-007**: 契约修订落盘且引用可追溯：065/059/076 契约增量与 agent_v2 README 更新完成（修订处以本 feature spec 为引用来源）。

## Assumptions

- **调研基线权威性**：`survey/saolei-hook-scripts.md` 的裁定为本 feature 设计语义基线——K4 需补上（用户裁定）、快路形态（§2.2.4 统一播报 + §2.2.5 九轮裁定：team 服务提供、成员侧统一策略、无 per-member 配置、接收方唯一、发言级顺序一致）；D8 由本 feature 落地为"独立 feature 先行"（用户 2026-10-16 范围确认：K4 + 快路，无其他特定项）。
- **快路生产行为惰性**：hook 落地前唯一插入式生产者是终局统计播报（player 非在途），快路在生产行为上不可观察——机制以 bazel 单测验证（2026-10-16 澄清：大型测试验收 = 既有套件全量回归，不新增快路专用场景）；后续 hook feature（planner 固化 JS 脚本 + operate 后播报）是首个真实受益者并携带真实端到端快路大型测试场景，其 spec 将引用本 feature 交付物。
- **对外 API 与 web 零变更**：统一流与快路是宿主/team 插件内部结构，proto 消息形状、RPC 语义、ChatEvent 帧形状不变；web 前端零改动（名册/激活/消息标签渲染沿用 076 交付面）。
- **请求侧校验口径不变**：UpdateTeam 仍恰含 player/planner 两 LLM 成员配置（076 FR-203 收窄口径保持）；本 feature 不放宽任何校验。
- **实现细节留 plan**：统一成员事件流的具体 API 形态（事件词汇、订阅/注册方式）、成员投递面回调签名与错误吞噬策略、工具单元级有界偏差是否消除、MemberCollector 内部机械的迁移映射——均留 plan 阶段裁定。
- **验收方式**：按宪法 VI，大型测试验收经 testplan skill 实际执行（部署→测试→清理闭环），全部用例通过为验收标准；编译+单测随每次代码变更执行（宪法 IV）。
