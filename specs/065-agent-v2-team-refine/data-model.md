# Data Model: agent-v2-team-refine

**Feature**: specs/065-agent-v2-team-refine/spec.md
**契约**: [contracts/team-member-source.md](contracts/team-member-source.md) · [contracts/game-stats-broadcast.md](contracts/game-stats-broadcast.md) · [contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md)
**现状基线**: `specs/059-agent-v2-team-mode/data-model.md`（team 编排/历史模型）、`specs/062-team-game-end-handoff/data-model.md`（终局收束）——本文件只记录增量实体与变更字段。

## 1. 实体（增量）

### 1.1 成员消息源（TeamMemberSource）— team 插件拥有的接口

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 稳定成员标识：team 映射键、广播 sender 键（agent 成员 = 其 dsh session id） |
| `events` | `readonly SessionEvent[]` | 成员产出 log（共享事件词汇表，只读快照语义） |
| `subscribe?` | `(onEvent) => () => void` | 产出实时通知（可选；drain 时派生是读权威，live 仅为优化与投影订阅面） |
| `sectionTarget?` | `{section(spec)}` | team section 注入目标（可选；缺省 = 无 prompt 注入） |
| `consumes?` | boolean（缺省 true） | `false` = announce-only：不接收 relay、不建 pending、不可 drain |

- **依赖倒置点**：team 只认"成员 = 消息源"；agent 与非 agent 成员（扫雷系统）都是该接口的实现者，产出经**完全相同**的派生/渲染/消费闭包路径。
- **agent 适配器**（team 导出）：`agentMemberSource(handle)` ——id/events/subscribe/sectionTarget/consumes 全部映射自 AgentHandle，现有语义零变化。
- **派生同权**：`deriveUnits`/`consumedAnchors`/`renderBroadcast` 改读 `source.events`，逻辑不变——`assistant/message` 形态产出即发言单元。

### 1.2 扫雷系统成员（SaoleiSystemMember）— saolei-loop 所有

| 字段/方法 | 类型 | 说明 |
|---|---|---|
| `source` | TeamMemberSource | `id = ${session}/saolei`（与 `${session}/player`、`${session}/planner` 命名同构；不是 dsh session，仅标识）；`events` = 内存 log；`subscribe` = 产出通知；无 `sectionTarget`；`consumes = false` |
| `announce(text)` | `(text: string) => void` | 向 log 追加一条 `assistant/message` 形态事件（构造形态见下方 log 事件形态）并同步通知订阅者；text 非空校验（fail-loud） |

- **log 事件形态**：`{type: "assistant/message", seq, time, data: {turn, step, message}}`——`message` 经 `createAssistantMessage` 构造（唯一 MessageId、单 text block；构造入参传合成 provider/model——入参不含 `kind`，构造后 `AssistantMessage.source` 为必填 `{kind: "model", provider, model}`）；`turn`/`step` 固定占位（派生/渲染只读 `id/content/time/seq`，两者不被读取）。词汇表兼容即派生同权（§1.1），不构造 turn/step 边界、不写 dsh session（构造同构于既有测试 `common/js/dsh-plugins/team/src/team.test.ts` 的 `data: {turn: 1, step: 1, message}`）。
- **生命周期**：进程内存态、随 team 物化（刷新 dispose 即清、重启即失）——与 agent 成员 log 实际行为对齐（2026-09-15 裁定，research.md D9）；`events` 由成员自持，未来加持久化 store 不动 team。
- **注册信息**（saolei-loop 常量并导出）：`role = "saolei"`、`SAOLEI_MEMBER_SUMMARY = "扫雷系统，终局播报对局结果与操作统计"`——以常规 announce-only 成员注册进 `members`，roster 自然渲染三行；不进 proto `TeamMember` 成员列表与 `active_member`。

### 1.3 每局分项操作统计 — `GameStats` 扩展

| 字段 | 类型 | 说明 |
|---|---|---|
| `operationsByType` | `Record<OperationType, number>` | click/flag/chord 各自的**成功派发**计数；分项之和恒等于 `operationCount` |

- **维护点**：`GameRuntimeService` 每局内态；`init` 清零（与 `operationCount`/`gameLog` 同点）；`executeOperation` 返回 `kind === "ok"` 时按 `op.type` 递增。
- **携带**：`computeGameStats` 扩展签名接收该表；随 `GameEventRecord.stats` 终局携带（`GameEventRecord` 形状不变）。
- **口径**（与 `operationCount` 一致）：SKIP（无害 no-op）与 STOP（结构性拒绝）不计；桌面派发失败（isError）不计；init/remain 调用不计；批量调用按其中实际成功执行的单个操作计。

### 1.4 统计消息（team 消息流的 wire 投影）— 宿主侧

| 字段 | 值 |
|---|---|
| merge `member` | `"saolei"`（wire 角色串，string） |
| `HistoryMessage.role` | `ROLE_AGENT` |
| blocks | 单个 text block（模板正文） |
| `seq` | 既有 `TeamHistory` 单调 seq（与 user/成员消息同源同值） |
| 帧扇出 | `team_message{member="saolei", message, seq}`（实时）；ListTeamMessages 回填同条目 |

- 成员视图：消费成员的 log 内为 `user/message`（`source {kind: "team-broadcast", role: "saolei", senderSessionId: "…/saolei", messageId: 发言锚}`）→ `MemberViewEntry {sender: "saolei"}` + `member_view` 帧（既有路径，非新通路）。
- **不变量**：每个被交接的局至多一条（编排 `statsSentFor` guard + MessageId 唯一）；`GetTeam.members`/`active_member` 不含 `saolei`。

### 1.5 记忆列表排序 — ListMemories `order_by`（memory 服务）+ 快照单页装载（JS 客户端）

契约：[contracts/memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) §1–§2。

| 实体 | 位置 | 说明 |
|---|---|---|
| `ListMemoriesRequest.order_by` | `projects/game/game.proto` | `string order_by = 4`（AIP-132）。缺省 = `memory_id` 升序（现行为零破坏）；`update_time desc`（空白不敏感）= `update_time` 降序 + `memory_id` 升序并列打破；其他值 INVALID_ARGUMENT |
| `ListMemoriesOrder` | `projects/game/memory/domain` | 两值枚举（memory_id 升序 / update_time 降序）；`MemoryRepository.ListMemories` 增 order 参数 |
| `MemoryPageCursor` | `projects/game/memory/domain` | 复合游标 `{UpdateTime time.Time, MemoryID string}`；`EncodeMemoryPageToken`/`DecodeMemoryPageToken` base64url(NoPadding)-JSON、`update_time` UTC RFC3339Nano（镜像 session 服务 `ListPageCursor` 先例 `projects/game/session/domain/pagination.go`）；解码失败 → `ErrInvalidPageToken` → handler 映射 INVALID_ARGUMENT |
| Mongo 排序/索引 | `projects/game/memory/runtime/mongo` | ordered：sort `{update_time: -1, memory_id: 1}` + 续页 `$or [{update_time < T}, {update_time = T, memory_id > M}]` + limit+1；启动建非唯一索引 `{template: 1, session_id: 1, update_time: -1, memory_id: 1}` |
| `listMemories` options | `common/js/dsh-plugins/memory-service/src/client.ts` | 可选 `{orderBy?, pageSize?}`；`pageSize` 给定 → 单页即止，未给定 → 全页累积（写路径现行为）。`MemoryEntry` 仅 `{memory_id, content}` |
| 快照装载/渲染 | `common/js/dsh-plugins/memory-service/src/{service,snapshot}.ts` | `load` 以 `{orderBy: "update_time desc", pageSize: 10}` 单页取最近 10 条（`SNAPSHOT_ORDER_BY`/`SNAPSHOT_ENTRY_LIMIT` 常量归 snapshot.ts）；`renderMemorySnapshot` 纯透传渲染（`memory_id` 永不渲染、空集空串——既有语义） |

- **快照序**：最近更新的 ≤10 条、最新在前、确定（同毫秒并列以 `memory_id` 升序打破）——由服务端排序保证，客户端不排序/不截断。
- **零变化面**：memory 工具写路径（`applyMemoryCall` 的 old_text 定位面向全量存储、不传 order_by）、快照冻结时机（物化预取、实例生命周期固定）、前端。

## 2. 状态转移（终局交接路径增量）

```mermaid
sequenceDiagram
    participant P as player(回合收束)
    participant O as orchestrator(nextStep)
    participant S as SaoleiSystemMember(内存 log)
    participant H as TeamSessions(宿主投影)
    participant L as planner(复盘)

    P->>O: idle（终局记录未复盘，队列空）
    O->>S: announce(gameStatsText(event))
    S->>S: log 追加 assistant/message 事件
    S-->>H: subscribe 通知（宿主物化后订阅）
    H->>H: appendAnnouncement → merge 入列 + team_message 帧
    O->>O: drain(planner)（派生拾取 saolei 发言单元，排末位）
    O->>L: 复盘驱动（播报消息居 followup 位）
    Note over O,S: 队列非空时 case 1 先行（player 消化）——播报顺延；<br/>新局覆盖终局记录则本局永不播报（跳局不补发）
```

关键转移规则（含既有不变量的增量影响）：

1. **播报条件**：case 4 命中 `peekGameEvent() !== null && !== reviewedGameEvent`，且 `statsSentFor !== 该记录`（对象恒等）。
2. **复盘保证启动**：播报后 planner 的 drain 必含 saolei 发言单元 → 原"`relays.length === 0` 不启动复盘、落入 player 续驱"的分支在存在未复盘终局记录时不再可达。
3. **重试安全**：复盘驱动失败 → `pendingReview` 持有含播报消息的输入集重试（case 2 短路 case 4）→ 无二次播报；log 与历史各一条。
4. **跳局**：排队消化驱动 player 开新局、终局记录被覆盖 → 旧记录的播报条件永不再满足 → 不补发；新局交接时对新记录播报。
5. **取消/刷新**：paused 短路 pump（播报顺延至恢复后交接）；dispose 清编排与成员 log（未交接局不再播报）。

## 3. 统计消息文本模板（终态）

`gameStatsText(record: GameEventRecord): string`（saolei-loop 所有，纯函数）：

```text
本局游戏结束：{胜利|失败}。
本局共执行 {operationCount} 个操作：click {click} 次、flag {flag} 次、chord {chord} 次。
```

- `{胜利|失败}` ← `record.status`（`won` → 胜利，`lost` → 失败）；计数 ← `record.stats.operationsByType` 与 `record.stats.operationCount`。
- 确定性纯文本（无时间戳/局号等不稳定字段）；模型与用户双可读。
