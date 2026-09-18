# Research: team 排队消息 step 边界进入与 turn 语义表述修正

> **状态**：调研完成（2026-09-11）。全部决策依据已在 spec Clarifications（dsh inbox 机制确认 / opencode 对齐确认）与本文件核实。
> **范围**：mid-turn 进入的机制选型、回退路径论证、指示/取消/前端信号、大型测试断言面、文档修正落点。语义基准见 [spec.md](spec.md) Clarifications。

## R1 — mid-turn 投递机制：turn 在途时立即 `steer`（决策）

**Decision**：`TeamOrchestrator.submit` 检测到当前激活成员 turn 在途（`drivingMember !== null`）时，**立即** `member.agent.steer(message)`——消息直接进入该成员 dsh inbox 的 next-step 队列，**不再进入编排层 FIFO**；由 dsh agent-loop 在下一个 step 边界 claim（与工具结果同批、作为独立 `user/message` 落成员 log）。

依据（全部 dsh 原生，无自研投递逻辑）：

1. `steer` 语义精确匹配 FR-001：`@deepseek-ai/dsh-agent` `lib/types/runtime-types.d.ts`——"Submit steering for the nearest step. An idle driver starts a turn; …"；step 边界 claim 取走**全部** next-step 输入（`lib/index.js` `claim()`：`mutate("next-step", 0, length)`）→ 消息作为独立用户消息块位于工具结果之后（顺序由日志追加序决定），多条同边界消息按 admit 序一并进入（FR-001 的批量语义与提交顺序保持均免费获得）。
2. 消费后即普通消息：claim 时消息以 `user/message`（`source.kind === "user"`）落成员 session log——与最初驱动消息完全同构，此后无任何排队特殊语义（spec Clarifications"消费后无特殊语义"裁定）。
3. 成员视角实时呈现与前端消费信号**零新增**：`MemberCollector.onSessionEvent` 的 `user/message` 分支（`projects/game/agent_v2/src/history.ts:591-597`）→ `appendMemberViewUser`（:404-423，060 交付）→ `member_view{member, sender:"user", message}` 帧实时扇出。mid-turn 路径与回合开始路径共用同一投影。

**Alternatives**（否决）：

- *编排层持有队列、感知 step 边界后投递*：编排层只有 `agent/status` idle 信号，无法感知 step 边界；轮询成员 inbox 状态属补丁式设计（违反 constitution 原则 II）。否决。
- *统一 `inject`（不唤醒）+ 编排层唤醒*：inject 用于 drive 折叠（idle 成员上 inject N-1 + followup 1）；turn 在途时注入需要唤醒语义（消息到达即应在下一 step 生效），inject 的"不唤醒"使 turn 恰好结束的消息悬挂 inbox 直至下次驱动，语义倒退。否决。
- *迁移 opencode 的 SessionInput/promotion 体系*：spec Assumptions"实现优先 dsh 原生能力"裁定明确排除。否决。

## R2 — 静止路径保持编排驱动（FIFO 保留，职责收缩）

**Decision**：`drivingMember === null`（静止/取消恢复/泵间隙）时 submit 走**现状路径**：入编排 FIFO → `requestPump` → `nextStep` → `drive()`（drain relays + inject N-1 + followup 1 = 一个成员 turn）。

依据：静止路径的驱动输入必须含该成员未消费 relay（`ctx.team.drain`）——直接 `steer` 只投用户消息会绕过 relay（FR-005 relay 仅在编排驱动边界消费）。FIFO 在新设计下只承载静止路径的瞬时消息（submit → pump 立即消费），在途排队全部由成员 inbox 承担。

## R3 — FR-002 回退路径由 dsh 原生延展/自愈承担（关键论证）

**Decision**：steer 后 turn 在 claim 前结束的情形**不设编排层消化逻辑**——由 dsh turn 循环的原生语义承担。机制核心（源码复核 `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js:564-571`）：turn 循环在每个 step 结束后的停止条件是 `turnEnds && this.inbox.nextStep.length === 0`——**与 turnEnds 的 kind 无关**。pending 的 steered 输入使循环不 break：要么**同 turn 延展**下一 step claim（检查点前），要么经 **wake 开新 turn** 消费（检查点后，turn/end 已落）。

语义等价性论证（对照 FR-002/FR-003）：

- "turn 结束后由当前激活成员以新回合消化" ✓——两种形态下消费均属**同一成员**（steer 目标即当前激活成员；同 turn 延展则根本未换 turn）。
- "消化优先于切换" ✓——延展形态发生在 `drive()` 的 idle 等待期内（turn 未结束）；新 turn 形态使 `agent/status` running 持续（覆盖 consecutive turns，dsh-agent README §status），drive 的 `waitForIdle` 直到消费完成才返回 → 编排层 `nextStep` 的切换评估自然排在其后。
- 消息被消费（claim 落 log）后即普通用户消息，不触发额外消化 ✓。

竞态窗口分析（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:810-842` `drive()`）：

| 窗口 | 行为 |
|---|---|
| step 结束、stopping 检查点时 next-step 已有 pending steer | 不 break → **同 turn 延展**下一 step claim（FR-001 主路径；含终局收束 step——turnEnds kind 无关，R9） |
| break 已发生（turn/end 落日志、driver 收尾/已 idle）后 steer | wake → **新 turn**（idle 时同步开 turn，README §steer）→ 新 turn 首 step 的 turn 边界 claim 消费（FR-002 自愈形态） |
| `drivingMember` 置 null 后（drive 已返回）submit | 走 R2 静止路径（FIFO → drive） |
| drive 返回与 submit 之间的微任务间隙 | `drivingMember === null` 判定为静止 → R2 路径；即使成员 inbox 因极窄窗口残留 pending，静止判定的 inbox 检查（R4）兜底 |

**测试义务**：大型测试必须覆盖"消息在最后一个 step 执行中到达 → turn 结束 → 同成员新回合消化 → 消化完成后才切换"（SC-002），实证两窗口语义与编排切换的正确交互。

## R3a — 与 062 终局收束的交互（062 先行落地，零特判）

**结论**：语义自然一致，两 feature 均零特判。关键机制（同 R3 源码）：`concludesTurn` 聚合产生的 `turnEnds{completed}` 与纯文本自然停手同路径——终局 step 置位 `concludesTurn` 时若存在 pending 的 steered 消息，turn 循环**不 break**（`inbox.nextStep.length > 0`）→ **同 turn 延展一步**：消息与终局工具结果同批进入（FR-001 原文路径——"下一个 step 开始时随工具结果一起"）；延展步后 turn 自然结束 → 既有 gameEnded 评估接管复盘。

- **优先级一致性**：与 062 Session 2026-09-12 裁定二（编排 FIFO 消化优先于复盘）语义一致——用户消息由当前激活成员消费先于复盘交接；复盘对象为消费结束时最新的终局记录（若延展步 init 新局并玩到终局，终局记录被覆盖——与 062 对消化 turn 的既有接受语义相同）。仅机制不同（inbox 延展 vs 消化 drive）。
- **委托闭环**：062 契约 §3（`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`）将"next-step inbox 非空时的延展行为"委托本 feature 定义——本节即该定义；062 的标记无条件性（其 FR-001）与本 feature 的零特判（FR-005/契约不变量）互相保持。
- **测试义务**（quickstart V6）：player 终局 step 执行中排队消息 → 延展步消费 → turn 结束 → 复盘交接事件序（player 延展 turn 的 turn_end 先于 planner 复盘 turn_start）；steer 探针模板的响应 MUST 为纯文本（无工具调用）以保证延展 turn 收束、断言确定。

## R4 — 排队指示 position 与 `snapshot.queued` 语义扩展

**Decision**：`snapshot.queued` = 编排 FIFO 长度 + **steered-unclaimed 计数**（该成员 turn 在途期间经 steer 投递、尚未被 claim 落 log 的消息数）。宿主 `send` 的 `queued{position}`（`projects/game/agent_v2/src/session.ts:242-246`，`before.queued + 1`）随之覆盖两路径。

steered-unclaimed 计数实现：steer 时记录 `message.id` 进入 per-drive 集合；`user/message`（`source.kind === "user"`）事件到达时按 `messageId` 移出（MemberCollector 已订阅 `session/event`，编排层经成员事件面共享同一事实源，不新增订阅）。取消/终局兜底：drive 结束（idle）时集合清空；若 idle 时成员 `inbox.hasPending` 为真（极窄残留），静止判定将其视为未静止。

**静止判定（quiescence）扩展**：`session.ts watchQuiescence` 与编排 `whenQuiescent` 的静止条件在"无在途回合且无待消化输入"外，增加"被驱动成员 inbox 无 pending"检查——堵住 R3 竞态表的最后一格（成员技术上 idle 但 inbox 有 steer 残留时，team 不误判静止/结束流）。依据：`Inbox.hasPending`（dsh-agent `lib/types/inbox.d.ts`）。

## R5 — 取消语义：默认清 inbox + 落地消息下次 Send 搭车进入 LLM

**Decision（两部分）**：

1. `orchestrator.cancel` 保持现状 `agent.cancel({ kind: "user" })`——dsh cancel 默认 durable 清空全部 pending（steered-unclaimed 随之取消，`agent/inbox/discarded`）。已消费消息 = 普通 user message（随在途回合终止呈现，log 不回滚）；未消费消息保留为已固化历史（Send 接受时已入归并序列）且不触发新驱动。`keepInbox` 备选否决：保留 inbox 残留需编排层二次清理，复杂度无收益。
2. **落地消息（landed，Session 2026-09-11 裁定——恢复 054 FR-017 第三个子句的 team 形态；作为解耦层实现）**：取消时全部未消费消息（编排 FIFO + steered-unclaimed）收编入编排层 `landed: UserMessage[]`——Cancel 侧只写 landed，不触碰 Send 逻辑；Send 侧只有**统一 flush 规则（来源无关）**：任何驱动装配（drive messages 拼接，静止路径）与任何 steer 之前（在途路径）先 flush landed——`inject`（非唤醒 next-step 上下文，idle 时挂起直到唤醒，`runtime-types.d.ts`）承载"进上下文但不驱动"。US1/US2（FR-001/FR-002）对 Cancel 后情况零定制。
   **可达性论证（在途+landed 组合非死代码）**：正常时序下 Cancel 后下次 Send 必走静止路径（成员 idle、landed 非空）；但 Send 对在途/静止的判定是弱耦合瞬时快照——Cancel 收敛窗口（`agent.cancel` 已调用、abort 传播/durability checkpoint 未完、`drivingMember` 未清空）内到达的 Send 仍判定在途 → steer 路径的 flush 生效。统一规则以不变应竞态；为 Cancel 后状态定制分支反而要求 Send 感知 Cancel 状态机阶段（复杂度更高，Session 2026-09-11 裁定否决）。

关键约束：

- **不单独触发处理**：landed 不进 `nextStep` 的驱动判定（`queue.length > 0` 检查不含 landed）——仅搭车于新消息触发的回合（FR-004）。
- **多次取消安全**：landed 在编排层累积；后续活跃取消清 inbox 不丢失 landed（它们此时在编排层而非 inbox）；消费闭环与 steeredPending 共用（`user/message` 按 messageId 移出）。
- **静止判定不受阻**：landed 不计入 `snapshot.queued`（无排队指示——chip 已随终态清除）；取消后的暂停静止（流终点）语义不变——paused 短路（`session.ts watchQuiescence` 现状）。

## R6 — 前端 chip 清除信号：`member_view{sender:"user"}` 单一规则

**Decision**：排队 chip 的消除改挂 `member_view` 帧（消费信号）——reduce 收到 `member_view` 且 `sender === "user"` 时，按**文本首匹配**移除 queue 中对应项；`turn_start` 分支的"队首出队"逻辑（`projects/game/web/frontend/src/store/chat.ts:542-550`，注释"turn_start 消费队首"）**退役**。

依据：

1. FR-004 要求"消费时消除"——`member_view{sender:"user"}` 在 claim 落 log 的同一写内扇出（`history.ts:399-402` 注释明示该帧即"consumption"信号），mid-turn（step 边界）与回合开始两条路径统一；`turn_start` 是回合开始信号，先于 claim，且 mid-turn 路径没有属于自己的 turn_start（turn 早已开始）——继续挂 turn_start 会使 mid-turn 消息的 chip 悬挂到 turn 结束。
2. 双信号并存会 double-remove（idle 路径 turn_start 与 member_view 都触发）——收敛为 member_view 单一规则消除该缺陷。
3. 文本匹配的边界：同文本多条排队消息时 member_view 按消费序到达，first-match 移除即按序等价；重复文本仅影响 UI 瞬时形态，不影响正确性（组件测试覆盖）。

**Alternatives**（否决）：queued 帧携带服务端消息 id + member_view 按 id 匹配——member_view 的 message id 在 claim 时才铸（`newMessage` 服务端赋 id），Send 时不可知；为 UI 瞬时态引入跨帧 id 协议不成比例。proto 维持零改动。

## R7 — 大型测试断言面：fake-llm 匹配天然支持 step 级分叉

**Decision**：fake-llm 服务**零改动**。step 级断言由既有匹配语义承载（`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2/§3）：`keywords` 匹配**最后一条 user 消息**——steered 消息被 claim 后，它就是该 step 请求的最后一条 user 消息；steer 前的 step 最后一条 user 消息仍是原驱动消息。

用例形态（testdata 新增模板组 + testplan 用例，不改服务）：

1. player 多步工具模板（step 1 产 tool call → turn 继续）；
2. steer 关键词模板（如 `keywords:[steer-probe]` → 输出对消息有响应的正文）；
3. 断言面：SC-001（下一 step 输入含该消息——经 fake-llm 响应文本与成员 log 的 user/message 位置双重断言）、SC-002（尾段到达 → 自愈新回合 → 消化完成后切换——断言成员视角/归并序列事件序）、SC-003（member_view 实时帧与 chip 消除）、SC-005（既有全量回归）。

## R8 — 文档/注释修正落点清单（FR-006/FR-007 全量）

| # | 文件:位置 | 修正内容 |
|---|---|---|
| 1 | `specs/059-agent-v2-team-mode/spec.md:47` | 切换节点 Q&A ②：删"两种新 turn 触发源（工具调用引发的后续 turn…）"枚举，收敛为"turn 已结束（dsh 语义：不再有新的模型调用）且无待消化排队消息" |
| 2 | `specs/059-agent-v2-team-mode/spec.md:49`、`research.md:80`、`research.md:91` | "team turn 持续流"命名：改称"team 持续流（至 team 静止）"或同行加注"非 dsh turn"澄清（Session 2026-09-11 裁定，FR-006；#7 为 team-api 的另一处） |
| 3 | `specs/059-agent-v2-team-mode/data-model.md:156` | 切换锚点 planner→player 条目：同 #1 收敛 |
| 4 | `specs/059-agent-v2-team-mode/research.md:59` | R6 续驱规则的静止定义：同 #1 收敛 |
| 5 | `specs/059-agent-v2-team-mode/research.md:110` | 决策⑦：同 #1 收敛 |
| 6 | `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md:66` | 结构性续驱条目：同 #1 收敛；**顺带 §2 驱动契约更新**——补 steer 路径（turn 在途的用户消息投递）、steered 计数、静止判定 inbox 检查（本 feature 行为变更的契约同步，constitution 原则 III；同文件 :65 的 `followup()` 简写补 inject 折叠机制说明） |
| 7 | `specs/059-agent-v2-team-mode/contracts/team-api.md:35`（§3.1） | "team turn 持续流"命名：同 #2 |
| 8 | `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:28-36` | 文件头注释：idle = 切换锚点静止的表述收敛（删 "turn ended and no further turn is triggered" 的触发源式解释，改为 turn 本义 + 待消化输入）；补 steer 投递路径说明 |
| 9 | `specs/049-agent-v2-dsh-init/spec.md:153`（FR-012） | supersession 注记：mid-turn 注入是 038 核心行为而非"observe-only 等扩展"；team 形态排队语义以 `specs/061-team-queue-steer/spec.md` 为准（FR-007①） |
| 10 | `specs/049-agent-v2-dsh-init/research.md:199` | 同 #9 注记（该决策行原文保持，附注） |
| 11 | `specs/059-agent-v2-team-mode/spec.md:181`（FR-011） | supersession 注记："当前成员回合进行中到达的用户消息 MUST 排队，回合结束后…消化"子句由 061 FR-001/FR-002 supersede；其余子句（排队呈现、广播、切换延后）保持（FR-007②） |

一致性义务（SC-004）：修正后文本检索"工具调用引发的后续 turn"零残留；"team turn" 零命中，或每条命中同行含"非 dsh turn"澄清语（改名/加注二择一，含 `research.md:80/:91`）；注记落于 049×2 处与 059 FR-011。

## 决策汇总

| # | 决策 | 依据原则 |
|---|---|---|
| R1 | turn 在途 submit 立即 `steer`（dsh 原生，消息直入成员 inbox next-step） | 原生能力优先 / FR-001 |
| R2 | 静止路径保持编排驱动（FIFO 收缩为静止路径专用） | FR-005 relay 边界 |
| R3 | FR-002 回退由 dsh 原生延展/自愈承担（同 turn 延展 / wake 新 turn 两窗口）；切换优先级经 drain interval 自然保持 | FR-002/FR-003 |
| R3a | 与 062 终局收束交互：零特判、语义一致（pending steer 使终局 turn 延展消费，复盘交接在后——与 062 FIFO 裁定优先级一致） | 062 契约 §3 委托 / FR-005 |
| R4 | `snapshot.queued` 含 steered-unclaimed 计数；静止判定加 inbox pending 检查 | FR-004 / 竞态封堵 |
| R5 | 取消沿用默认清 inbox（不用 keepInbox） | FR-004 / 原则 II 简化 |
| R6 | 前端 chip 消除改挂 `member_view{sender:"user"}`（文本首匹配），turn_start 出队退役 | FR-004 消费时消除 |
| R7 | fake-llm 零改动；keywords 最后一条 user 消息匹配承载 step 级分叉 | SC-001 测试可行性 |
| R8 | 文档修正 11 条落点（059×8 / 049×2 / orchestrator 注释×1；命名点含 059 research.md:80/:91） | FR-006/FR-007 |
