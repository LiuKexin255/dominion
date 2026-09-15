# Quickstart: agent-v2-team-refine 验证指南

**Feature**: specs/065-agent-v2-team-refine/spec.md
**用途**: 端到端验证三块增量（终局统计播报 / 记忆快照近因注入 / team 提示词增量）的运行指南；实现细节见 [plan.md](plan.md) 与 [contracts/](contracts/)。

## 1. 前置条件

- 仓库 bazel 环境可用（`bazel` / `bazel run //:go`）；大型测试按 `style/large_test.md` 规范经 testplan skill（`tools/test/guitar`）执行。
- 无新增外部依赖、无 secret 变更、无 proto/Go 服务/前端改动——既有部署拓扑（`projects/game/testplan/deploy_agent_v2.yaml`，fake-llm + fake-desktop 零外网）直接复用。

## 2. 单元/集成级验证（每次代码变更随行，constitution 原则 IV）

```bash
# team 插件（成员消息源接口 + section 增量）
bazel test //common/js/dsh-plugins/team/...
# saolei-loop（分项计数 + 触发/guard + 消息模板）
bazel test //common/js/dsh-plugins/saolei-loop/...
# memory-service（updateTime 捕获 + 快照倒排截取）
bazel test //common/js/dsh-plugins/memory-service/...
# 宿主（announcer 订阅 + appendAnnouncement + 历史投影）
bazel test //projects/game/agent_v2/...
```

预期：全部通过。关键断言面：

- [team-member-source.md](contracts/team-member-source.md) §5——适配器等价（agent 成员行为回归）、announce-only 能力位（不建 pending/不被 relay/drain throw/roster 含其行）、非 agent source 派生同权（`assistant/message` 事件 → 发言单元 → `<saolei-message>` 渲染 → 注入 → 消费闭包）、section 增量措辞。
- [game-stats-broadcast.md](contracts/game-stats-broadcast.md) §5——分项计数口径（[data-model.md §1.3](data-model.md)）、announce 先于 drain、exactly-once、跳局不补报、`GetTeam` 面不含 `saolei`。
- [memory-snapshot-recency.md](contracts/memory-snapshot-recency.md) §4——Timestamp 归一化、倒排 + 前 10 条 + 并列确定、`memory_id` 不渲染。

## 3. 大型测试（验收门禁，constitution 原则 VI——实际执行 deploy→test→cleanup 闭环）

```bash
# 经 testplan skill 执行主 suite（game-system 覆盖 agent_v2 对话/游戏面）
guitar run projects/game/testplan/system_test.yaml
```

本 feature 的增量断言（落在 `projects/game/testplan/agent_v2_game_test.go` 与排队场景所属的 `agent_v2_conversation_test.go`，对照 SC-001/SC-002）：

1. **多局链路**：每局交接后团队归并序列恰有一条 `member="saolei"` 的统计消息；正文 = [data-model.md §3](data-model.md) 模板（结果行 + 总数/分项行）；数值与该局 fake-desktop 实际收到的成功派发序列一致（含一次批量多操作的对照局）；planner 复盘 turn 的模型输入含统计消息（fake-llm review 规则 keywords 命中模板关键行）；player 成员视图含 `user: [saolei]` 注入条目。
2. **排队跳局**：终局 player 回合收束时存在排队用户消息 → player 先消化并开新局 → 被跳过局无统计消息；新局交接时恰有一条新局统计（对照：终局后无排队消息的局统计即时播报）。
3. **提示词增量**：物化后 `GetTeamMember` 的 `system_prompt` 含 roster 的 saolei 行与"自身输出不使用广播标签"表述；planner 的记忆快照 ≤10 条且按更新时间倒序（fake-llm/夹具注入 >10 条记忆的会话对照）。
4. **回归**：既有 agent_v2 大型测试全量通过（多局闭环、排队消化、取消、刷新重建、双视图回填）。

**通过标准**：所有用例全部通过（任何 failed/flaky 即验收未通过，修复后重跑至全绿）。

## 4. 手动观察路径（可选，不构成验收）

- 物化 team 后发送首条消息驱动游戏；局终态后在 webUI 团队视图观察 `saolei` 标签的统计消息（普通成员消息样式），planner 复盘输出随后出现。
- 对话页成员清单 → `GetTeamMember` 只读浮层核对 system prompt 增量（roster 广播成员行 + 标签格式说明 + planner 快照 ≤10 条）。
