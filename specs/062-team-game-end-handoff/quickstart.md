# Quickstart: 062-team-game-end-handoff 验证指南

**目的**: 端到端证明终局收束语义生效——终局工具结果收束 player turn（无痕）、复盘即时触发、多局闭环与既有行为零回归。
**契约引用**: [contracts/saolei-turn-conclude.md](contracts/saolei-turn-conclude.md)（映射与判定矩阵）、[data-model.md](data-model.md) §1.2/§3（断言面）。
**规范引用**: [spec.md](spec.md) SC-001~005。

## 前置条件

- bazel 环境可用（仓库根目录执行）。
- 大型测试经 testplan skill 执行（`tools/test/guitar`；规范 `style/large_test.md`）——本 feature 的验收标准是**全部用例通过**（constitution 原则 VI），构建检查不替代执行。

## V1. 单测：收束标记映射矩阵（SC-004）

```bash
bazel test //common/js/dsh-plugins/saolei/... //common/js/dsh-plugins/saolei-loop/...
```

**预期**:
- `runtime.test.ts`：判定矩阵 runtime 侧 10 行逐行断言——致终局 operate / init 即终局棋盘 / 终局棋盘结构性拒绝 / 终局棋盘空操作列表 → `concludesTurn: true`；playing / `no_active_game` / `unable to recognize board` → 不携带；`remain` → 永不携带；dispatch FAILED → isError（无标记）。
- `index.test.ts`：`fakeExec` 的 `concludeTurn` spy——置位 outcome → 恰好调用一次；不置位 outcome → 零调用；isError → 抛错且零调用；参数组合拒绝（判定矩阵第 11 行，工具层、不进 runtime）→ 零调用。
- 既有终局棋盘用例的 outcome 断言含新字段（随可选字段同步更新），playing 用例断言不变。

## V2. 大型测试：终局收束与即时复盘主线（SC-001）

```bash
# testplan skill（tools/test/guitar），完整部署→测试→清理闭环
# 全量计划（3 suites：game-system / game-disconnect / game-memory-down）：
guitar run projects/game/testplan/system_test.yaml
# 聚焦 game 面（US1 主线，agent_v2_game_test 全部用例）：
guitar run projects/game/testplan/system_test.yaml --suite game-system
# 断开分支套件（agent_v2_game_disconnect_test）：
guitar run projects/game/testplan/system_test.yaml --suite game-disconnect
```

**预期**（`TestAgentV2TeamGameTerminalWonAndReviewContinues` / `...TerminalLostAndReviewStops` 更新后形态）:
- game 1：init（playing）→ operate 返回终局棋盘 → **该 turn 以终局 tool_result 为最后输出块收束**——fake-llm 终局链规则（`agent-v2-saolei-operate-won/lost` 的总结文本步骤）已脚本化但**零执行**（无该文本的模型输出）。
- 紧接 planner 复盘 turn（4-turn 链：planner 开局 → player 局 1 → planner 复盘 → player 局 2/停止确认），单流覆盖、除首条用户消息外零用户输入。
- game 2（continue 场景）：init 即识别胜利棋盘 → turn 在 init 后收束（tool_result 数 = 1，脚本中的 operate 批步骤零执行）。
- lost 场景：复盘后 player 停止确认 turn 无任何工具调用（不开局）。

## V3. 无痕终态（SC-002）

**预期**（同批大型测试断言）:
- 终局 player turn 的 turn_end 帧 status = `TURN_STATUS_COMPLETED`（Go 断言面 `game.TurnStatus_TURN_STATUS_COMPLETED`）。
- 该 turn 最后输出块 = 终局工具调用块（结果已 settle）；session log/视图/List 回填无 `interrupted`、无 "tool call aborted" 类合成结果、无消息缺失。
- Cancel 回归用例（`TestAgentV2TeamGameActiveMemberTransitions` 的 cancel 段）保持 CANCELED 终态——取消语义零改动。

## V4. 交接可见性（SC-003）

**预期**（同批大型测试断言）:
- planner 复盘 turn 的模型输入含 `<player-tool-call>` 终局单元，result 内终局 status 全文（"game status: won/lost"）；断言面：planner 成员视图 + fake-llm review 规则的 `history_keywords` 命中（命中即证明复盘输入含该单元）。
- 复盘后 player turn 的模型输入含其自身终局 tool call+result 与复盘 relay。
- planner 视图 relay 形态：标签对开头、无头行、不截断（060 contracts/team-api.md §4 既有断言回归）。

## V5. 回归面（SC-005）

**预期**:
- 既有 team 大型测试全量通过（多局闭环、排队消化、取消、刷新重建、回填/断开收敛/多流去重、多会话隔离）。
- `TestAgentV2TeamGameWonChainOnExecutor` 新形态：init 即终局 → turn 在 init 后收束（1 个 tool_result）、无第二次模型输出、无复盘（init 不写终局记录）、链路静止于 player 激活。
- desktop-absent 各用例（`agentV2NodesktopSummary` 断言）**不变**——dispatch 失败为 isError 不收束，模型正常输出失败总结（FR-001 失败不收束面的天然回归）。
- `agent_v2_conversation_test.go` / `agent_v2_game_disconnect_test.go` / `agent_v2_preset_test.go` 核对后零改动（仅 nodesktop 语义）。

## 收束判定速查

实现与断言时对齐 [data-model.md](data-model.md) §1.2 的 11 行矩阵；任何"是否收束"的疑问先查矩阵行，再查 [spec.md](spec.md) 终局处理流程总览图。
