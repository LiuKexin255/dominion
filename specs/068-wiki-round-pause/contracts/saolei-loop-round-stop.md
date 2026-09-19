# Contract: saolei-loop 每五轮自然停

> **Feature**: specs/068-wiki-round-pause/spec.md（FR-014~016）
> **锚点**: `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（`nextStep`/`runPump`）、`projects/game/agent_v2/src/session.ts`（`watchQuiescence`，零改动）
> **基线**: `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2（编排行为契约）；数据模型见 [data-model.md](../data-model.md) §8 状态图。

## 1. 游戏轮定义

一**轮** = 一局从开始到达游戏终态（won/lost，player 侧 `peekGameEvent` 产生新终态记录）+ 该局的 planner 复盘 turn **成功结束**。中途重开的局不产生终态记录、不进入复盘，因此**不计数、不清零**（既有 `reviewedGameEvent` 记录身份防重复消费的机制天然排除重开局）。

## 2. 计数与停点

1. **计数点**：`runPump` 中 review drive 成功 settle（`step.review !== undefined` 分支，`reviewedGameEvent`/`pendingReview` 消费处）→ `completedRounds += 1`。失败的复盘走 `fail()` 挂起，不计数；按既有失败恢复语义重试，成功 settle 后才计数。
2. **停点判定**：`nextStep` 的 `phase === "planning" || phase === "reviewing"` 分支——该分支的到达本身保证队列已空（队列优先分支在前：排队消息由当期 activation 的 planner 消化完），满足"先让当期 member 处理完再停"。
3. **停点动作**（`completedRounds >= ROUND_STOP_INTERVAL`，常量 = 5）：执行与正常切换**相同**的状态转移（`phase = "playing"`、`current = "player"`），随后**返回 null（不产生驱动步）**，`completedRounds = 0`。player 的未消费 relays **不 drain**（保留在 team 服务中，下次驱动一并投递，不得丢失）。
4. 计数为编排器内存态，随 materialize/re-materialize 重置（与 phase/queue 等编排态同生命周期）。

## 3. 停下后的行为

- 循环静息：pump 无步（`active === null && queued === 0`）→ session 层 `watchQuiescence` 按既有静息语义收尾 Send 流（`projects/game/agent_v2/src/session.ts`，**零改动**）。
- **不是失败/取消**：不写 `lastError`、不置 `paused`、无日志错误；对外快照**不新增字段**（用户决策 2026-09-19）。
- **恢复**：用户 `submit` → `requestPump` → `nextStep` 队列分支：`drain(player)`（planner 复盘广播随行）+ 用户消息 → 驱动 player。与"不停时结构性续驱"的投递内容一致，仅时机合并到用户消息。

## 4. 与既有机制的交互

| 机制 | 交互 |
|---|---|
| 排队消息 | 停点判定在队列消化之后（§2.2）；第 5 轮计数后到达的排队消息由 planner 正常消化，消化完仍停 |
| cancel/paused | 独立机制：cancel 保留 activation 并暂停（用户消息恢复同一 member）；自然停是"切换后静息"。第 5 轮计数后发生 cancel → 恢复并消化完队列后同样到达停点 |
| 失败（fail） | 复盘失败不计数；停点永不掩盖失败（失败路径在 drive 返回处先行返回） |
| Dispose | 既有清理语义不变 |
| Send 流 | 静息收尾语义复用，零改动（§3） |

## 5. 验收锚点

orchestrator 单测（注入 fake 成员/终态记录，`orchestrator.test.ts` 扩展）：

1. 轮次 1–4 完成后照常自动驱动 player（含 relays 投递）。
2. 第 5 轮复盘 settle 且队列空 → 无 player 驱动；snapshot 的 `activation === "player"`、`phase === "playing"`；无 `lastError`/`paused`。
3. 第 5 轮复盘后有排队消息 → planner 消化完再停。
4. 停下后 `submit` → player 被驱动且收到 planner 未消费广播。
5. 中途重开的局（无终态记录）不计数。
6. 复盘失败 → 不计数；重试成功 → 计数并按规则停。
7. 停下后新一轮从 1 计数（第 5+1 轮完成后不停，第 10 轮才再次停）。
8. player relays 在停点不被消费（下一次驱动可取回）。

E2E 不设独立 testplan（真实 5 局成本/波动不可控，裁断记录于 research.md §7）；既有 agent_v2 testplan 作零回归。
