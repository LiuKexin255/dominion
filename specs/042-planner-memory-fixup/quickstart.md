# Quickstart: planner 记忆校准实现修复

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](./spec.md)

> 端到端验证指南。三个场景对应三个用户故事（US1/US2/US3），每个场景可通过单测（快速反馈）与大型测试（端到端验收）两层验证。大型测试经 testplan skill（`tools/test/guitar`）执行，宪法原则 VI 强制。

---

## 前置条件

- 仓库 bazel 构建环境可用（`bazel build //...` / `bazel test //...`）。
- 既有 `projects/game/testplan/system_test.yaml`（大型测试计划 YAML）+ `deploy_agent.yaml`（测试部署拓扑，已含 memory 服务条目）。
- 既有 `projects/game/fake-llm/`（脚本化 LLM fixture）+ `helpers_test.go`（fixture/helper 常量 lockstep）。

---

## 场景 1: saolei_operate 停止行含具体操作参数（US1）

### 验证目标

`saolei_operate` 批量操作中途停止（游戏结束/结构性拒绝）时，返回的结果行包含导致停止的**具体操作参数**（`type(x,y)`）而非序号。

### 单测验证

**文件**: `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`

```bash
bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test
```

**验证要点**（参照 [`contracts/saolei-operate-stop-format.md`](./contracts/saolei-operate-stop-format.md) §3.2）：
- 批量操作第 K 个操作触发 `game_over` → 结果行含 `stopped at click(x,y) (game_over)`（用实际坐标）。
- 批量操作第 K 个操作触发 `out_of_bounds` → 结果行含 `stopped at {type}(x,y) (out_of_bounds)`。
- 单次操作触发停止 → 停止行同样含操作参数（与批量一致）。
- 正常完成（无停止）→ 结果行 `executed N ops`（不变）。

### 大型测试验证

**文件**: `projects/game/testplan/agent_saolei_test.go`（既有文件，更新断言）

```bash
# 经 testplan SKILL 执行
guitar run projects/game/testplan/system_test.yaml
```

**验证要点**（参照 [`contracts/saolei-operate-stop-format.md`](./contracts/saolei-operate-stop-format.md) §3.3-3.4）：
- `agent_saolei_test.go` 既有 saolei_operate 停止用例的断言更新为含操作参数的格式。
- 既有 fixture `sample_saolei_structural_stop.yaml` 的注释/期望同步。

---

## 场景 2: 正常复盘指令在 player 对话列表实时可见（US2）

### 验证目标

planner 在正常游戏结束复盘后经 `instruct_player` 发送的校准指令，在 desktop player 页面对话列表中**实时显示**（无需重新加载），行为与 init/compact 场景一致。

### 单测验证

**文件**: `projects/game/agent/src/team/graph.test.ts`（review 节点既有测试所在文件）

```bash
bazel test //projects/game/agent/src/team:planner_test
# 或
bazel test //projects/game/agent/src/team:graph_test
```

**验证要点**（参照 [`contracts/review-instruction-display.md`](./contracts/review-instruction-display.md) §5）：
- review 节点 instruction 不为 null 时，发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, writeBack.id, "MESSAGE_ROLE_USER")` 帧。
- 发射帧的 `frameId`（`writeBack.id`）非 undefined（`ensureMessageId` 保证）。
- `writeBack` 消息对象与 `update.playerMessages` 中的是同一对象（同 id）。
- instruction 为 null 时不发射帧（既有行为不变）。

### 大型测试验证

**文件**: `projects/game/testplan/saolei_team_test.go`（既有文件，新增/更新用例）

```bash
guitar run projects/game/testplan/system_test.yaml
```

**验证要点**：
- planner 完成正常复盘后调用 `instruct_player` → player partition 的 `ListMessages` 在不重新加载的情况下含指令消息。
- 消息顺序：指令紧跟 game-ending tool_result 之后、player 下一条 output 之前。
- 对比 init 指令的显示行为——两者一致（均在发送后实时出现在 player 对话列表中）。

---

## 场景 3: RefreshTeam 后初始指令产出（US3）

### 验证目标

RefreshTeam 清空短期消息通道后，触发一次无游戏历史初始指令产出（与 team 初始化的 init instruction 行为一致），指令进入 `playerMessages` 通道并在 player 首次激活时可见。

### 单测验证

**文件**: `projects/game/agent/src/session-team.test.ts`

```bash
bazel test //projects/game/agent/src:session-team_test
```

**验证要点**（参照 [`contracts/refresh-instruction-trigger.md`](./contracts/refresh-instruction-trigger.md) §6）：
- `refreshTeam()` 清通道后调用 `startInstructionTurn()`（触发指令产出）。
- post-refresh 指令产出期间 `isBusy()` 为 true（守卫生效）。
- post-refresh 指令产出期间 `isRunning()` 为 false（typing indicator 不驱动）。
- 连续两次 `refreshTeam()`（每次完成后）每次均触发指令（FR-013）。
- post-refresh 指令产出失败时降级（不阻断 RefreshTeam 完成）。

### handler 单测

**文件**: `projects/game/agent/src/handler.test.ts`

```bash
bazel test //projects/game/agent/src:handler_test
```

**验证要点**：
- RefreshTeam 后指令产出期间再次调用 RefreshTeam → FAILED_PRECONDITION（既有 `isBusy()` 守卫）。
- RefreshTeam 完成后（指令产出也完成）再次 RefreshTeam → 成功并触发新指令。

### 大型测试验证

**文件**: `projects/game/testplan/saolei_team_test.go`（既有文件，新增用例）

```bash
guitar run projects/game/testplan/system_test.yaml
```

**验证要点**：
- 对已有对话历史的 session 执行 RefreshTeam → player `playerMessages` 被清空 → 随后出现新的无游戏历史初始指令。
- 指令产出期间 user message 排在指令之后（player 首次激活时先读指令、再处理 user message）。
- 连续 RefreshTeam 每次均产出新指令。

---

## 大型测试执行（宪法原则 VI 强制验收）

```bash
# 经 testplan SKILL 完整执行（部署→测试→清理闭环）
# 禁止仅 bazel build 替代验收
guitar run projects/game/testplan/system_test.yaml
```

**验收标准**：所有测试用例全部通过（failed/flaky 即未通过，修复重跑至全绿）。

**覆盖范围**（FR-014）：
- US1: saolei_operate 停止行含操作参数（游戏结束停止 / 结构性拒绝停止）。
- US2: 正常复盘指令在 player 对话列表实时可见（对比 init 指令一致）。
- US3: RefreshTeam 后初始指令产出（指令进入 playerMessages、user message 排序、降级、连续 refresh）。

---

## 影响面同步检查

停止行格式变更（US1）影响多处格式字符串引用，须确认全部同步（`rg "stopped at op"` 零命中旧格式）：

```bash
# 旧格式应零命中（全部已更新为 stopped at type(x,y)）
rg "stopped at op " projects/game/ specs/
```

**预期**：仅 `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` 中的示意描述 MAY 保留旧格式（非本特性 scope），其余全部更新。
