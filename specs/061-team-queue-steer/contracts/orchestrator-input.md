# Contract: saolei-loop 编排输入契约（mid-turn steer / 计数 / 取消 / 静止）

> `@dominion/dsh-saolei-loop` `TeamOrchestrator` 的用户输入面。本 feature 变更集中在 `submit` 的在途分支与计数/静止/取消语义；`drive`/`nextStep` 优先级/交替激活不变。行为依据 [../research.md](../research.md) R1–R5；边界 [../spec.md](../spec.md) FR-001..FR-005。

## 1. `submit(text)` 行为

```text
submit(text) → SubmitResult
  if 静止（drivingMember === null）:
      createUserMessage → 入编排 FIFO → paused = false → requestPump
      （现状路径不变；R2——静止驱动必须含 drain relays）
      drive 输入装配（nextStep）：messages = [...relays, ...flushLanded(), 新消息]
  else（当前激活成员 turn 在途）:
      for m of flushLanded(): member.agent.inject(m)   // 统一 flush（非唤醒）
      createUserMessage → member.agent.steer(message)  // 唤醒
      steeredPending.add(message.id)
      paused = false
  return { queued: 是否排队呈现（在途=true；静止=false）,
           position?: FIFO.length + steeredPending.size（仅 queued 时）,
           messageId }
```

**landed flush 统一规则（来源无关）**：`flushLanded()` 取空并返回当前 landed 队列——挂在 Send 的两个既有输入出口（驱动装配 / steer 前），Send 不感知 landed 消息的来源（Cancel 是当前唯一生产者），对"Cancel 之后的情况"零定制（Session 2026-09-11 裁定；在途路径的 flush 非死代码——Cancel 收敛窗口内 Send 仍判定在途，统一规则以不变应竞态，research.md R5）。

义务：

1. **在途 steer 的消息形态**：与现状 `createUserMessage({ content: [{type:"text", text}], source: {kind:"user"} })` 完全一致——claim 后与驱动消息同构（普通用户消息，无特殊语义）。
2. **进入点**：steer 后消息由 dsh agent-loop 在下一个 step 边界 claim（与工具结果同批、独立 user/message）；turn 结束前未被 claim 时经 steer-wake 自愈为新 turn（R3），编排层**不**为 steered 消息安排消化 drive。
3. **同边界多条**：turn 在途期间先后 submit 的多条消息按 admit 序留在 inbox next-step，下一个 step 边界一并 claim（dsh `claim()` 批语义），提交顺序保持。
4. **relay 边界不变**：steer 仅投用户消息；团队广播仍仅在编排驱动边界经 `ctx.team.drain` 消费（FR-005）。
5. **landed flush 统一规则**：任何驱动装配与任何 steer 之前 flush 全部 landed——与新输入一同进入 LLM 上下文（普通用户消息形态、不单独触发处理，FR-004，Session 2026-09-11 裁定）；规则来源无关（Send 不感知 landed 从哪来），US1/US2 的排队逻辑不因 Cancel 定制。

## 2. 计数与快照

- `snapshot.queued === queue.length + steeredPending.size`——宿主 `queued{position}` 帧的唯一事实源（`projects/game/agent_v2/src/session.ts` send 的 `before.queued + 1` 语义随之扩展）。
- `steeredPending` 的**消费闭环**：成员 `user/message`（`source.kind === "user"`）事件按 `messageId` 移出——与 `member_view{sender:"user"}` 帧同一事实源（`projects/game/agent_v2/src/history.ts` appendMemberViewUser），不新增订阅面。
- **收束兜底**：drive 的 idle 等待返回时清空 `steeredPending`（成员已静止，未 claim 残留不可能存在——steer-wake 保证；清空为防御性不变量）。

## 3. 静止判定（quiescence）

`whenQuiescent()` / 宿主 `watchQuiescence` 的静止条件：

```text
active === null ∧ queue 空 ∧ steeredPending 空
（兜底：active 归 null 的过渡窗口内，被驱动成员 agent.inbox.hasPending === false）
```

team 持续流（Send 流生命周期）终点沿用该判定——无在途回合且无待消化输入（含 inbox）才结束流。

## 4. `cancel()`（语义保持 + 落地收编）

```text
cancel() → { dropped }
  dropped = queue.splice(0)              // 静止路径 FIFO 残留（现状）
  landed.push(...dropped, ...steeredPending 收编)   // 全部未消费消息落地
  paused = true
  active?.agent.cancel({ kind: "user" })
      └─ dsh 默认清 inbox：inbox 侧未消费残留随之 durable 取消
          （landed 已收编编排层，不受影响）
```

- 已消费（claim 落 log）的消息：普通用户消息，随在途回合终止呈现，不回滚（FR-004）。
- 未消费消息（landed）：不触发新驱动；已固化历史（Send 接受时入归并序列）保留；下次 Send 经统一 flush 规则（§1）进入 LLM 上下文。
- 再次 `submit` 恢复（现状不变）——该次 Send 走正常两路径之一，landed 经 §1 统一规则 flush。
- 多次取消：landed 在编排层累积；活跃取消清 inbox 不丢失 landed；消费闭环（`user/message` 按 messageId）与 steeredPending 共用。

## 5. 不变量

1. 任一时刻至多一个成员被驱动（现状；steer 不新增驱动路径，自愈 turn 属同一成员的 drain interval）。
2. 消息恰好消费一次：`steeredPending` 与 FIFO 互斥（在途/静止二择投递），`messageId` 唯一（dsh inbox pending 期间唯一性 + 编排 FIFO 不混入已 steer 消息）。
3. `nextStep` 优先级序（排队消化 > pendingReview > 切换 > gameEnded > 续驱）不变——steered 消息不经 `nextStep`，其"消化优先于切换"由 drain interval 语义承载（R3）。
