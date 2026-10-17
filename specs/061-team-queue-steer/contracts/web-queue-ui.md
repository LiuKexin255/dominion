# Contract: web 排队指示与消费消除（member_view 单一信号）

> 前端排队 chip 生命周期契约（`projects/game/web/frontend/src/store/chat.ts`）。proto **零变更**——消费信号复用既有 `member_view{sender:"user"}` 帧（060 交付，`projects/game/agent_v2/src/history.ts:399-423`：成员消费用户输入的同一写内扇出）。行为依据 [../research.md](../research.md) R6；FR-004。

## 1. chip 生命周期

| 事件 | 行为 |
|---|---|
| `queued{position}`（本流首帧，busy 时） | 追加 `QueuedMsg{text, position}`（现状不变，`store/chat.ts:486-489`） |
| `member_view` 且 `sender === "user"` | 按 `message` 首个 text 块内容对 `queue` 做**文本首匹配**移除（**新增**；消费时消除——mid-turn 路径在 step 边界消除，回合开始路径在 drive claim 时消除） |
| `member_view` 且 `sender !== "user"` | 不触碰 queue（relay 进入成员视角，与排队无关） |
| `turn_end{CANCELED}` | queue 全清（取消路径的兜底——服务端 cancel 同时作废 team 排队队列，`store/chat.ts:768`） |
| 其余 `turn_end` 终态 | **不**触碰 queue（`COMPLETED` 仅收束 live 回合、`ERROR` 保留已呈现内容与排队 chip、`ABORTED` 走会话删除的整体复位——`store/chat.ts:772-798`） |
| `turn_start` | **不再**触碰 queue（现状"队首出队"逻辑退役——`store/chat.ts:548-550`；mid-turn 消费没有属于自己的 turn_start，且与 member_view 双信号并存会 double-remove） |

> **勘误（2026-10-14，076 批3 实现核对）**：本表原将 turn_end 行写作"`turn_end`（任一终态）→ queue 全清（现状不变）"，与实现不符——实现（新旧一致，`store/chat.ts:768-798`）只在 `TURN_STATUS_CANCELED` 分支清空 queue；`COMPLETED` 与 `ERROR` 均保留 queue，`ABORTED` 的清空来自会话删除的整体复位而非 turn_end 分支。此处按实现的实际终态分类更正；076 批3（`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §7）沿用该帧语义，未改动行为。

## 2. 匹配规则（文本首匹配）

```text
onMemberViewUser(text):
  idx = queue.findIndex(q => q.text === text)
  if idx >= 0: queue = queue.remove(idx)
```

- member_view 用户帧按消费序到达（dsh claim 按 admit 序），first-match 移除即按序等价。
- 重复文本边界：同文本多条排队消息时逐帧逐条移除（每次移除最早一条），仅影响 UI 瞬时形态，不影响正确性（组件测试覆盖）。

## 3. 断言面（SC-003）

1. mid-turn 消费：player 多步工具回合中排队的消息，其 chip 在 `member_view{sender:"user"}` 帧到达时消除（不等待 turn_end）；成员视角同时呈现该输入（呈现于输入时 step 与下一 step 输出之间）。
2. 回合开始消费：静止→排队→消化路径，chip 在消化 turn 的 claim 帧消除（先于该 turn 的输出完成）。
3. 取消：`turn_end{CANCELED}` 后 chips 全清（现状回归）。
4. 重复文本：两条同文本排队消息的 chip 随两帧先后各自消除，无 double-remove、无悬挂。
5. 回填一致性：刷新/重连后 List 面（成员视角/归并序列）与实时呈现收敛（既有语义回归）。
