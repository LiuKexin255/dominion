# Contract: team 插件——在途广播快路（插入式产出 → 在途成员下一 step）

**Feature**: [../spec.md](../spec.md)（FR-007–FR-012；US3）| **决策依据**: [../research.md](../research.md) D4–D6 | **设计语义**: `survey/saolei-hook-scripts.md` §2.2.4/§2.2.5（用户裁定形态）
**实现锚点**: `common/js/dsh-plugins/team/src/{team,broadcast}.ts`（relay 扩展 + source 投递面）、`common/js/dsh-plugins/saolei-loop/src/{orchestrator,agent-member}.ts`（投递面供给与策略）
**基线契约修订**: 本契约落地时同步修订 `specs/065-agent-v2-team-refine/contracts/team-member-source.md`（§2 增补：投递面与快路投递规则）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1（行为规范第 3 条增补投递时机）、`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`（§3 两类输入分层与 §7 宿主联动的"广播投递时机"增量）——修订为终态表述，引用本 feature spec。

## 1. 触发定义（发送方视角）

**插入式产出（insertion production）**：发送方不是当前在途 turn 执行者、却输出了 message（`assistant/message`，非空正文）。现状唯一生产者是 announcer 的播报；未来任何场景的非激活成员 message 产出（如 hook 播报）复用同一路径，无需新机制。

- 触发粒度 = **message 单元**（`BroadcastUnit.kind === "message"`）；tool 单元不触发（其发送者即在途成员本人，relay 已排除 sender，触发面恒空）。
- **接收方 = 当前在途成员，且仅此一个**（"至多一个成员在途驱动"不变量保证）。非在途成员不是快路接收方——静止成员感知不到插入消息，等下次驱动时经 pending drain 消费（既有语义，正是其接收广播的正确方式）。
- 启用条件是**接收方存在性**（存在当前在途成员），非发送方身份：终局统计播报时无在途成员 → 无快路、pending drain，现状不变。

## 2. 机械契约

### 2.1 投递面（成员种类的结构性属性）

```ts
interface TeamMemberSource {
  /** 在途投递面（可选）：与 consumes 同级的结构性属性——类型可缺省、语义非配置项。
   * agent 成员适配器统一供给（无 per-member 配置维度）；announce-only 成员结构性缺席。 */
  readonly deliverInFlight?: (message: UserMessage) => void;
}
```

agent 成员策略（单一实现，所有 LLM 成员共用）：

```text
AgentMember.deliverInFlight(message: UserMessage): void   // loop 侧成员适配器的投递面方法
  适配器在途（busy）→ agent.inject(message)   // 不唤醒驱动；下一 step 边界认领
  适配器静止        → no-op                    // "我不是接收方"
  inject 抛出       → 捕获 + warn 日志；回落 pending 基线（不向 announce 路径传播）
```

供给点：orchestrator `createMember` 组合装配（source 与 AgentMember 适配器同域）——注册的 source 对象携带 `deliverInFlight` 闭包**委托**该适配器方法（announcer 注册项不带）；team relay 时对每个**带投递面的 consuming 接收方**调用——机械遍历与语义单一接收方不矛盾（策略过滤后实际生效的只有当前在途成员）。

**投递面归属**（`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §1 行为规范第 7 条「team 只投递不驱动」保持成立）：team 只调用成员供给的 `deliverInFlight` 闭包，**不直接调用** `agent.inject`——实际 inject 发生在成员适配器（loop 侧）内，team 对 agent 细节保持不感知。

### 2.2 relay 的快路扩展（叠加，不改基线）

```mermaid
sequenceDiagram
    participant S as 插入式生产者（announcer/未来 hook）
    participant T as team relay
    participant P as pending（基线，不变）
    participant F as 投递面（agent 成员策略）
    participant R as 在途成员 next-step inbox

    S->>S: 产出落 sender log（锚点确立）
    S->>T: source.subscribe（log 事件）
    T->>P: 锚点追加进各 consuming 接收方 pending（照常）
    T->>T: buildMessage(unit)（与 drain 完全相同的构造路径：同锚点、同 team-broadcast source、同渲染）
    T->>F: deliverInFlight(广播消息)（仅 message 单元）
    F->>R: 在途 → agent.inject（不唤醒）；静止 → no-op
    Note over R: 下一 step 边界认领 → 落接收方 log（source.messageId = 锚点）→ 下一 step 模型请求携带
    Note over P: reconcile 重建时锚点已消费 → pending 副本剔除（恰好一次）
```

义务：

1. **pending 恒为基线**：relay 的锚点追加无条件发生；快路是叠加的提前投递，任何失败（注入异常/取消丢弃）自动回落基线语义。
2. **单一构造路径**：快路消息经 `buildMessage`（→ `buildBroadcastMessage`，`common/js/dsh-plugins/team/src/broadcast.ts:271-289`）构建——与 drain 完全相同；**不存在快路特制消息形态**（无独有 source kind、无独有渲染）。
3. **回读语义**：构造时从 sender log 回读锚点内容——relay 时事件已在 log 中；读取失败按既有 fail-loud（corruption 同 drain 错误面，不新增错误种类）。

## 3. exactly-once（锚点闭包，零新记账）

- 消费闭包：接收方 log 出现 `source.messageId === 锚点` 的 `user/message` 注入即闭包（`consumedAnchors`，`broadcast.ts:160-173`）。
- **双向自愈**：快路被认领 → drain 前置 reconcile 重建 pending 时剔除 relay 副本（去重）；快路被取消/丢弃（cancel 清 inbox）→ 锚点未消费 → pending 照常 drain（重投）。
- `drained` 集合继续保护 drain 路径"已弹出未落 log"窗口；快路不使用 `drained`（不弹出 pending）。

## 4. 顺序一致性

- **发言级（assistant message）跨成员完全一致**：快路接收方与其他成员看到的共享消息相对顺序相同（= 生产顺序）。机制：drain 序按 `compareUnits` 生产坐标；快路消息落点 = 下一 step 边界认领，位于其生产时刻所属 step 窗口内。
- **工具单元级有界偏差（披露，不消除——用户裁定 2026-10-16）**：接收方 log 中快路消息位于同 step 全部工具结果之后（step 边界认领），其他成员的生产序中它可能先于同 step 后继兄弟工具单元（其 `tool/call` 锚点时间晚于 announce）。偏差被限制在单个 step 窗口内、仅涉及插入消息与兄弟工具单元的相对位置，发言级顺序不受影响。消除方案（工具单元锚点改 result 时间 / drain 按认领位置归并）会改变既有 drain 排序保证，不在本 feature 范围。

## 5. 与驱动 / flow / 投递排队的关系（零影响）

| 维度 | 契约 |
|---|---|
| 驱动 | `inject` 不唤醒驱动——不开新 turn、不延长在途 turn、不触发任何 flow 决策；在途 turn 的下一步（处理工具结果的那一步，本来就会发生）认领该消息 |
| 完成判定 | `whenIdle`/agent 静止语义不变（inject 非唤醒输入，turn 收束即 idle） |
| 输入记账 | 快路消息不计入 steered/queued 快照（呈现 chip 生命周期由 `member_view` 帧驱动，现状语义） |
| 驱动输入构成 | drain + landed + 用户消息不变；"编排不合成驱动消息"保持——快路投递的是成员真实广播产出（同锚点），非合成文本 |
| steer 共存 | 与用户 steer 消息共享 nextStep inbox 与 step 边界认领——同边界多条按插入序 FIFO；投影分离（steer → `user`；快路 → `team-broadcast`，均为 `appendMemberViewUser` 既有处理种类，零新增投影规则） |

## 6. 边缘路径

- **turn 收束边缘（`concludesTurn`/模型停止）**：announce 在收束前发生 → 在途成员仍是接收方、快路成立；turn 随即收束 → 消息留存 inbox、下一 turn 边界认领（"claims pending next-step input"，dsh-agent-loop step/turn 认领机）——时序等价 drain，锚点闭包防重。
- **cancel**：`orchestrator.cancel()` 既有顺序 `landUnclaimed`（收编 inbox 为 landed，下次 turn 开启 flush）先于 inbox 清除——不丢失；若被清除，锚点未消费 → pending 重建自愈重投（§3 双向自愈）。
- **刷新 teardown**：registration dispose 清空 pending；在途快路残留随 inbox 清理——无跨生命周期重投（旧团队已不存在）。
- **announce-only 接收方**：`consumes: false` 非 relay 接收方且无投递面——announcer 的播报不回流给自己。

## 7. 验收面（对应 spec SC-003/SC-004/SC-005）

- team 单测：①在途接收方收到快路广播（同锚点、同 drain 构造——消息逐字段等价断言）；②快路认领后 drain/pending 重建剔除副本（恰好一次）；③静止接收方无投递（no-op）、pending 照常；④tool 单元不触发快路；⑤投递面抛出 → warn 日志、pending 基线不受扰、relay 不上抛；⑥终局统计播报场景（announcer 生产、无在途接收方，spec SC-003 第④项）——各接收方投递面策略 no-op、pending 照常 drain（现状行为不变）。
- loop 单测（AgentMember 策略）：busy → inject（不唤醒断言：无新 turn、whenIdle 不变）；静止 → no-op；注入后 steered/queued 计数不变。
- 顺序断言：快路接收方 log 的发言级序 = 其他成员 drain 序（生产序）；工具单元级偏差仅出现在单 step 窗口内。
- 大型测试：既有套件全量回归（快路生产行为惰性——现状唯一插入式生产者 announcer 播报时无在途成员；spec Clarifications 2026-10-16 裁定：不新增快路专用大型场景，真实端到端场景由后续 hook feature 携带）。
