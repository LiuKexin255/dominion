# Contract: web 排队指示与消费消除（member_view 单一信号）

> 前端排队 chip 生命周期契约（`projects/game/web/frontend/src/store/chat.ts`）。proto **零变更**——消费信号复用既有 `member_view{sender:"user"}` 帧（060 交付，`projects/game/agent_v2/src/history.ts:399-423`：成员消费用户输入的同一写内扇出）。行为依据 [../research.md](../research.md) R6；FR-004。

## 1. chip 生命周期

| 事件 | 行为 |
|---|---|
| `queued{position}`（本流首帧，busy 时） | 追加 `QueuedMsg{text, position}`（现状不变，`store/chat.ts:486-489`） |
| `member_view` 且 `sender === "user"` | 按 `message` 首个 text 块内容对 `queue` 做**文本首匹配**移除（**新增**；消费时消除——mid-turn 路径在 step 边界消除，回合开始路径在 drive claim 时消除） |
| `member_view` 且 `sender !== "user"` | 不触碰 queue（relay 进入成员视角，与排队无关） |
| `turn_end`（任一终态） | queue 全清（现状不变——取消/终止路径的兜底，`store/chat.ts:691` 注释） |
| `turn_start` | **不再**触碰 queue（现状"队首出队"逻辑退役——`store/chat.ts:548-550`；mid-turn 消费没有属于自己的 turn_start，且与 member_view 双信号并存会 double-remove） |

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
