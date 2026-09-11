# Data Model: team 排队消息 step 边界进入与 turn 语义表述修正

> 进程内状态模型（非对外资源）。行为契约见 [contracts/orchestrator-input.md](contracts/orchestrator-input.md) 与 [contracts/web-queue-ui.md](contracts/web-queue-ui.md)；对外 proto **零变更**（`queued`/`member_view`/`turn_*` 帧均已存在）。

## 1. 排队消息生命周期（终态）

```text
用户 Send（成员 turn 在途）
  → [accepted·已固化] 归并序列 appendUser（Send 接受时，不变）+ queued{position} 帧
  → [steered·待消费] 消息经 agent.steer 进入成员 inbox next-step（编排层记录 messageId 待消费集合）
  → [claimed·已消费] 下一个 step 边界 claim：user/message 落成员 log
       └─ 自此为普通用户消息，生命周期终止（排队特殊语义全部消失）
       └─ Cancel 于 claimed 前介入：→ [landed·落地]（编排层，见下）

用户 Send（team 静止）
  → [accepted·已固化] + queued 帧（无在途 turn 时不发，现状不变）
  → [fifo·待消费] 编排 FIFO（瞬时；pump 立即消费）
  → [claimed·已消费] drive 的 turn 边界 claim：user/message 落成员 log（同上终止）
       └─ Cancel 于 claimed 前介入：→ [landed·落地]

[landed·落地]（编排层 landed 列表）
  进入：Cancel 收编全部未消费消息（FIFO + steered-unclaimed）
  退出：统一 flush 规则（来源无关）——任何驱动装配（drive messages 拼接）与任何 steer 之前 flush
       → claim 落 log → 普通用户消息（终止）
  约束：不单独触发驱动（不进 nextStep 驱动判定）；不产生排队指示（chip 已随终态清除）
  解耦：Cancel 侧只写 landed；Send 侧不感知 landed 来源（US1/US2 对 Cancel 后情况零定制）
```

关键不变量：

- **消费即终止**：`claimed` 之后不存在任何"排队消息"状态——成员 log 中的 `user/message`（`source.kind === "user"`）与最初驱动消息同构，取消不回滚、切换评估不感知（FR-003/FR-004）。
- **可见性独立于消费**：team 归并序列位置固定于 Send 接受时刻（enqueue 即固化，不变）；消费时点只决定成员视角位置与 chip 消除时点。
- **回退自愈**：`steered` 消息在 turn 结束前未被 claim 时，经 steer-wake 由 dsh 开新 turn 消费（research.md R3）——状态机上仍是 `steered → claimed`，只是 claim 落在自愈 turn 的 turn 边界而非 step 边界。
- **落地必达**：`landed` 消息在下次 Send 触发的回合中必达 LLM 上下文（不单独触发处理，FR-004）。

## 2. 编排层状态扩展（`TeamOrchestrator`）

| 字段（现名/新增） | 类型 | 语义 |
|---|---|---|
| `queue`（既有，语义收缩） | `UserMessage[]` | 仅承载**静止路径**的待消费用户消息（submit 于 `drivingMember === null` 时入队，pump 立即消费） |
| `steeredPending`（新增，per drive） | `Set<MessageId>` | 当前成员 turn 在途期间经 steer 投递、尚未 claim 的消息 id；`user/message`（source user）事件按 id 移出；drive 收束（idle）清空；Cancel 时其消息对象收编入 `landed` |
| `landed`（新增） | `UserMessage[]` | 取消收编的未消费消息（FIFO + steered-unclaimed）；不进驱动判定、不计入 `snapshot.queued`；下次 Send 投递（静止→drive inject 批 / 在途→inject 后 steer）；消费闭环与 steeredPending 共用（messageId） |
| `snapshot.queued`（语义扩展） | `number` | `queue.length + steeredPending.size`——宿主 `queued{position}` 的单一事实源（landed 不计入） |
| `snapshot.active`（不变） | `TeamRole \| null` | 在途驱动成员 |

状态机交互（`nextStep` 优先级不变）：排队消化（FIFO）> pendingReview > planning/reviewing→player 切换 > gameEnded 复盘 > player 续驱。steered 消息不进入 `nextStep` 评估——它们在 drive 的 drain interval 内由 dsh 消费（含自愈 turn）。

## 3. 静止判定扩展（quiescence）

`session.ts watchQuiescence` 与编排 `whenQuiescent` 收敛的静止条件：

```text
静止 ⇔ 编排 pump 无待跑步骤（active === null ∧ queue 空 ∧ steeredPending 空〔含兜底：被驱动成员 inbox.hasPending === false〕）∧ 非 paused-有输入
```

- `steeredPending.size > 0` 时成员必为 running（turn 在途或自愈 turn 接续，dsh `agent/status` running 覆盖 consecutive turns）——常规路径下 `active !== null` 已蕴含；inbox pending 检查是极窄竞态窗口（drive 返回与状态转换之间）的兜底（research.md R4）。
- 流终点（team 持续流结束）沿用该静止判定，语义不变。

## 4. 前端排队态（client-local，`store/chat.ts`）

| 状态 | 进入 | 退出 |
|---|---|---|
| `queue: QueuedMsg{text, position}[]` | `queued` 帧（本流首帧，现状） | ① `member_view{sender:"user"}` 帧到达：按 text 首匹配移除（**新增**，消费时消除）；② `turn_end` 终态：全清（现状，取消/终止路径） |

`turn_start` 分支的队首出队逻辑移除（research.md R6）——单一消费信号规则。

## 5. 术语基准（修正后终态表述）

- **turn**：控制焦点的一次完整移交；**turn 已结束 = 成员本轮全部工作完成、不再有新的模型调用**（工具欠账延续同 turn 的下一个 step，不开新 turn）。切换锚点 = "turn 已结束 ∧ 无待消化输入"，不再枚举"新 turn 触发源"。
- **team 持续流**（原"team turn 持续流"）：Send 建立的流的生命周期——从发起持续输出至 team 静止，横跨多个成员 turn；非 dsh turn。
- 依据：`survey/deepseek-harness-turn-step-semantics.md`（源码级核实）。
