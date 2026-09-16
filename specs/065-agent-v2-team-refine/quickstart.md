# Quickstart: agent-v2-team-refine 验证指南

**Feature**: specs/065-agent-v2-team-refine/spec.md
**用途**: 端到端验证三块增量（终局统计播报 / 记忆快照近因注入 / team 提示词增量）的运行指南；实现细节见 [plan.md](plan.md) 与 [contracts/](contracts/)。

## 1. 前置条件

- 仓库 bazel 环境可用（`bazel` / `bazel run //:go`）；大型测试按 `style/large_test.md` 规范经 testplan skill（`tools/test/guitar`）执行。
- 无新增外部依赖、无 secret 变更、无前端改动；memory 服务有 `order_by` 通用排序增量（[memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) §1——AIP-132 `{field} [desc]` 语法 + 白名单 `memory_id`/`update_time`（任意键位合法）+ 指定 tie-breaker"完全不含 `memory_id` 才填补" + 通用游标（指针签名，nil = 首页）；proto 增字段向后兼容，缺省 `memory_id` 升序零破坏）——既有部署拓扑（`projects/game/testplan/deploy_agent_v2.yaml`，fake-llm + fake-desktop 零外网）直接复用。

## 2. 单元/集成级验证（每次代码变更随行，constitution 原则 IV）

```bash
# team 插件（成员消息源接口 + section 增量）
bazel test //common/js/dsh-plugins/team/...
# saolei-loop（分项计数 + 触发/guard + 消息模板）
bazel test //common/js/dsh-plugins/saolei-loop/...
# memory-service（listMemories 单页语义 + 快照透传渲染 + load 单页装载）
bazel test //common/js/dsh-plugins/memory-service/...
# memory 服务（order_by 通用解析/通用游标 codec（解码+键匹配）/仓储直译排序）
bazel test //projects/game/memory/...
# 宿主（announcer 订阅 + appendAnnouncement + 历史投影）
bazel test //projects/game/agent_v2/...
```

预期：全部通过。关键断言面：

- [team-member-source.md](contracts/team-member-source.md) §5——适配器等价（agent 成员行为回归）、announce-only 能力位（不建 pending/不被 relay/drain throw/roster 含其行）、非 agent source 派生同权（`assistant/message` 事件 → 发言单元 → `<saolei-message>` 渲染 → 注入 → 消费闭包）、section 增量措辞。
- [game-stats-broadcast.md](contracts/game-stats-broadcast.md) §5——分项计数口径（[data-model.md §1.3](data-model.md)）、announce 先于 drain、exactly-once、跳局不补报、`GetTeam` 面不含 `saolei`。
- [memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) §4——`order_by` 通用语法解析（表驱动：空白早返回 `[memory_id asc]`、白名单拒绝/重复拒绝/完全不含 `memory_id` 才追加、**中间位 `memory_id` 合法**（`memory_id, update_time desc` 原样不追加）、非法后缀）、通用游标 codec（round-trip + JSON 对象形态 pin `{"entries":[...]}`（`typed` 不出现在序列化输出）+ 坏 token（含顶层非对象/entries 缺失或空）/键不匹配（跨序重放）/时间不可解析拒绝 + 等价拼写互通 + decode 产物 `entry.Typed()` 为 typed 值 + 指针语义（Decode 成功恒非 nil、Encode(nil) → 空 token））、仓储直译排序（**nil cursor = 首页**（filter 无 `$or`）、`update_time` 降序 + 并列 `memory_id` 升序、`memory_id desc` 方向翻转、limit+1 续页、通用 OR 阶梯 seek O(n) 增量构造（任意键数直测 + 单键 = 单子句 `$or` + **typed 直达 bson**——fake 断言 filter 时间值为 `time.Time` 实例、仓储无转换路径）、缺省回归）、client 单页即止、快照透传渲染（`memory_id` 不渲染）。

## 3. 大型测试（验收门禁，constitution 原则 VI——实际执行 deploy→test→cleanup 闭环）

```bash
# 经 testplan skill 执行主 suite（game-system 覆盖 agent_v2 对话/游戏面）
guitar run projects/game/testplan/system_test.yaml
```

本 feature 的增量断言（落在 `projects/game/testplan/agent_v2_game_test.go` 与排队场景所属的 `agent_v2_conversation_test.go`，对照 SC-001/SC-002）：

1. **多局链路**：每局交接后团队归并序列恰有一条 `member="saolei"` 的统计消息；正文 = [data-model.md §3](data-model.md) 模板（结果行 + 总数/分项行）；数值与该局 fake-desktop 实际收到的成功派发序列一致（含一次批量多操作的对照局）；planner 复盘 turn 的模型输入含统计消息（fake-llm review 规则 keywords 命中模板关键行）；player 成员视图含 `user: [saolei]` 注入条目。
2. **排队跳局**：终局 player 回合收束时存在排队用户消息 → player 先消化并开新局 → 被跳过局无统计消息；新局交接时恰有一条新局统计（对照：终局后无排队消息的局统计即时播报）。
3. **提示词增量**：物化后 `GetTeamMember` 的 `system_prompt` 含 roster 的 saolei 行与"自身输出不使用广播标签"表述；planner 的记忆快照 ≤10 条且按更新时间倒序（fake-llm/夹具注入 >10 条记忆的会话对照——服务端 `order_by` 排序后经单页装载注入）。
4. **memory 有序列表**：`GET /api/v1/.../memories?order_by=update_time%20desc`（经网关）按 `update_time` 降序、并列 `memory_id` 升序；复合游标 `next_page_token` 续页全量一次；通用语法正路径（`update_time` 裸字段升序、`update_time desc, memory_id` 多字段）合法且序正确；非法 `order_by`（`foo`/`update_time asc`/`content`）→ 400 INVALID_ARGUMENT；缺省模式（无 `order_by`）分页断言回归。
5. **回归**：既有 agent_v2 大型测试全量通过（多局闭环、排队消化、取消、刷新重建、双视图回填）。

**通过标准**：所有用例全部通过（任何 failed/flaky 即验收未通过，修复后重跑至全绿）。

## 4. 手动观察路径（可选，不构成验收）

- 物化 team 后发送首条消息驱动游戏；局终态后在 webUI 团队视图观察 `saolei` 标签的统计消息（普通成员消息样式），planner 复盘输出随后出现。
- 对话页成员清单 → `GetTeamMember` 只读浮层核对 system prompt 增量（roster 广播成员行 + 标签格式说明 + planner 快照 ≤10 条）。
