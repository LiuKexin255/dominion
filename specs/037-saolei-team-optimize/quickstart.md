# Quickstart: saolei Team 模板优化

**Feature**: `037-saolei-team-optimize` | **Spec**: [`spec.md`](./spec.md) | **Contracts**: [`contracts/`](./contracts/)

> 本文档描述如何验证 037 特性的五项变更（实时可见性修复、上下文压缩、工具描述注入、消息上限、游戏统计）端到端工作。详细实现步骤见 `tasks.md`（由 `/speckit.tasks` 生成）。

---

## 前置条件

- bazel 构建环境就绪（`bazel build //...` 通过）
- 已有 saolei team 测试基础设施（fake-model + fake-tool DI 模式，`projects/game/agent/src/team/graph.test.ts`）
- desktop 前端开发环境（`projects/game/desktop/frontend/`）

---

## 验证场景

### 场景 1: planner 游戏历史消息实时可见（US1 / SC-001）

**验证目标**: planner 复盘输入在 desktop planner tab 实时显示（无需重载）。

**单元/集成测试验证**（`projects/game/agent/src/team/`）:

1. 构建 team graph，注入 `emitFrame` 录制数组。
2. 构造 fake tool 产生游戏结束事件（sink 写入 gameLog + onGameEnd）。
3. 触发 planner 节点运行。
4. 断言：`emitFrame` 被调用，帧携带 `agent="planner"`，内容包含完整游戏过程（每步操作的 tool/coord/status）。

**desktop 验证**:

1. 启动 desktop，进入 saolei session，选择窗口。
2. 发送用户输入，让 player 完成一局游戏。
3. 观察 planner tab：游戏结束后，planner 的复盘输入消息**立即出现**（无需重新进入 session）。
4. 重新进入 session（重载历史）：复盘输入消息仍在，且不重复。

**通过标准**: 实时帧发射 + 重载去重 + 内容一致。

---

### 场景 2: 每 5 局触发上下文压缩（US2 / SC-002 / SC-003 / SC-004）

**验证目标**: 连续 5 局后触发压缩，通道收缩为摘要，player 停下，策略保留。

**集成测试验证**（`projects/game/agent/src/team/graph.test.ts`）:

1. 构建 team graph，注入 fake model（player + planner + 压缩均使用同一 fake）。
2. 连续驱动 5 局游戏结束（每局 planner 触发一次）。
3. 断言第 5 局 planner 返回后：
   - `gameCounter === 5`。
   - graph 路由到 compress 节点（而非 player）。
   - 压缩后 `playerMessages.length === 1`（仅摘要 AIMessage）。
   - 压缩后 `plannerMessages.length === 1`。
   - `StrategyStore.get(sessionId)` 返回值不变（策略保留）。
   - turn 结束（路由 END，player 不继续）。
4. 发送第 6 局用户输入：
   - player 以摘要上下文重建（通道中仅摘要一条消息 + 新 HumanMessage）。
   - 游戏正常进行。

**压缩失败验证**:

1. 使 fake model 在压缩调用时抛错。
2. 断言：异常传播到 TurnLoop → abort 信号触发 → 连接终止。

**空通道验证**:

1. 构造 plannerMessages 为空（planner 降级路径不写消息）的场景。
2. 触发压缩。
3. 断言：plannerMessages 不变（空通道 = 空操作），playerMessages 正常压缩。

**通过标准**: 5 局触发压缩 + 通道收缩为 1 + 策略保留 + player 停下 + 压缩失败 abort。

---

### 场景 3: planner 系统提示词注入 player 工具描述（US3 / SC-005）

**验证目标**: planner 提示词包含工具描述，但 planner 工具集仍仅 `update_strategy`。

**单元测试验证**:

1. 构建 team graph，捕获 planner 的 `createAgent` 调用参数（通过 `createAgentFn` DI spy）。
2. 断言：`systemPrompt` 包含每个 player 工具的 name 和 description（`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_chord_click`/`saolei_remain`）。
3. 断言：`tools` 参数仅包含 `update_strategy`（长度为 1，不含 player 工具）。

**通过标准**: 描述在提示词 + 工具集不变。

---

### 场景 4: desktop 消息上限 FIFO（US4 / SC-006）

**验证目标**: 每个 agent tab 消息受上限约束，超出 FIFO 移除。

**前端组件测试验证**（`projects/game/desktop/frontend/`）:

1. 向某 agent tab 注入超过 `MAX_CHAT_ENTRIES_PER_AGENT`（200）条消息。
2. 断言：tab 仅保留最新 200 条（最旧被移除）。
3. 向另一 agent tab 注入消息。
4. 断言：两 tab 计数独立（player tab 超限不影响 planner tab）。

**通过标准**: FIFO 移除 + 独立计数。

---

### 场景 5: 游戏统计数据（US5 / SC-008）

**验证目标**: MCP 在 onGameEnd 携带正确的统计数据，planner 复盘包含统计。

**单元测试验证**（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` 测试）:

1. 构造一局已知操作序列与已知地雷布局的 fake 游戏。
2. 验证 onGameEnd 携带的 `GameStats`:
   - `operationCount` = onMove 触发次数（不含 init/remain/被拒落子）。
   - `correctFlags` = totalMines − MINE格数 − HIT_MINE格数。
   - `avgOpsPerMine` = operationCount / correctFlags（2 位小数）。
3. won 局：MINE=0, HIT_MINE=0 → correctFlags = totalMines。
4. lost 局：correctFlags = totalMines − MINE − HIT_MINE。
5. y=0（开局踩雷）：avgOpsPerMine = "N/A"。
6. counter 不可解码：correctFlags = null, avgOpsPerMine = "N/A"。

**集成测试验证**:

1. 驱动一局游戏结束，验证 planner 的 `buildReviewInput` 输出包含三项统计数据文本。
2. 验证实时帧（US1 机制）与重载均显示统计数据。

**通过标准**: 统计正确 + 降级处理 + 复盘包含统计。

---

### 场景 6: 大型测试（FR-034 / SC-007）

**验证目标**: 端到端验证全部特性（经 testplan skill 执行）。

**测试计划**: 在 `testplan/` 目录下创建测试计划 YAML，覆盖：

1. **实时可见性**: 完成 1 局游戏 → planner tab 实时显示复盘输入。
2. **压缩触发**: 连续完成 5 局 → 压缩触发、通道收缩、player 停下。
3. **工具描述**: planner 提示词包含工具描述（日志/trace 验证）。
4. **游戏统计**: onGameEnd 携带统计数据、复盘输入包含统计（日志/trace 验证）。

**执行方式**:

```bash
# 加载 testplan skill
# 执行测试计划：guitar run <plan.yaml>
# 完整部署 → 测试 → 清理闭环
```

**通过标准**: 所有测试用例全部通过（宪法原则 VI）。

---

## 编译与单测命令

```bash
# Agent 包（TypeScript）
bazel test //projects/game/agent/src/team:lib_test
bazel test //projects/game/agent/src:mcp_saolei_test  # 或对应 target

# Desktop 前端（如需）
bazel test //projects/game/desktop/frontend:lib_test  # 或对应 target

# 全量编译
bazel build //...
bazel test //...
```

---

## 关键引用

| 文档 | 路径 |
|---|---|
| Spec | `specs/037-saolei-team-optimize/spec.md` |
| Research | `specs/037-saolei-team-optimize/research.md` |
| Data Model | `specs/037-saolei-team-optimize/data-model.md` |
| 压缩节点契约 | `specs/037-saolei-team-optimize/contracts/compression-contract.md` |
| 游戏统计契约 | `specs/037-saolei-team-optimize/contracts/game-stats-contract.md` |
| 031 team graph 契约 | `specs/031-team-template-mode/contracts/team-graph-contract.md` |
| 031 desktop 契约 | `specs/031-team-template-mode/contracts/desktop-contract.md` |
| 031 saolei sink 契约 | `specs/031-team-template-mode/contracts/saolei-sink-contract.md` |
| bug 根因 | `specs/031-team-template-mode/bug-analysis.md` Issue 2 |
| 大型测试规范 | `style/large_test.md` |
