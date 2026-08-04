# Quickstart: Team Template Mode 缺陷修复

**Feature**: `036-team-mode-bugfix` | **Spec**: [`spec.md`](./spec.md)

> 验证指南：确认四个缺陷修复在单测与端到端层面正确生效。

---

## 前置条件

- 仓库已构建：`bazel build //...`
- saolei team graph 代码已按 [`contracts/team-graph-fix-contract.md`](./contracts/team-graph-fix-contract.md) 修改
- desktop 前端已按 [`contracts/desktop-alignment-fix.md`](./contracts/desktop-alignment-fix.md) 修改

---

## 1. 单元/集成测试（`bazel test`）

### 1.1 team graph 测试

```bash
bazel test //projects/game/agent/src/team:lib_test
```

**期望覆盖**：

| 测试场景 | 验证 Issue | 关键断言 |
|---|---|---|
| 游戏失败（lost）→ planner 触发 | Issue 1 | `plannerMessages` 非空；`gameEnded` 被 planner 清除 |
| 输局后 LLM 尝试重开 → loop 停止 | Issue 1 | `saolei_init` tool 未被调用（middleware 停止了 loop） |
| invoke 异常 → 后处理仍执行 | Issue 1 | `consumeGameEvent` 被调用（try/finally） |
| 多步操作的 planner 复盘输入 | Issue 2 | 复盘输入 HumanMessage 包含每步操作 + 棋盘 |
| 空日志的 planner 复盘输入 | Issue 2 | 复盘输入为说明性消息（非空内容、不崩溃） |
| config 传递到内部 createAgent | Issue 4 | createAgentFn DI spy 断言传入的 config 含 recursionLimit |

### 1.2 team-sink 测试

```bash
bazel test //projects/game/agent/src/team:lib_test  # 含 team-sink.test.ts
```

**期望覆盖**：

| 测试场景 | 验证 Issue | 关键断言 |
|---|---|---|
| onGameStart 清空 gameLog | Issue 2 | 第二局 onGameStart 后 gameLog 仅含本局条目 |
| onMove/onGameEnd 累积 gameLog | Issue 2 | gameLog 含正确顺序的操作条目 |

### 1.3 desktop 前端测试

```bash
# 如果存在前端组件测试
bazel test //projects/game/desktop/frontend/...
```

**期望覆盖**：

| 测试场景 | 验证 Issue | 关键断言 |
|---|---|---|
| 用户消息气泡右对齐 | Issue 3 | 用户消息容器 `justify-content: flex-end` 生效 |

---

## 2. 大型测试（testplan skill）

### 2.1 执行

使用 testplan skill 执行端到端测试（宪法原则 VI 强制）：

```
testplan skill → guitar run <plan.yaml>
```

测试计划位于 `projects/game/testplan/`（031-spec 已建立）。

**期望覆盖**：

| 测试场景 | 验证 Issue | 关键断言 |
|---|---|---|
| 游戏失败 → planner 触发 → 策略更新 | Issue 1, 2 | planner 被触发（plannerMessages 非空）；策略被写入 |
| 多局连续游戏 | Issue 1, 4 | 每局结束 planner 各触发一次；单局不触发 GraphRecursionError |
| desktop 对话页面用户气泡右对齐 | Issue 3 | 用户消息在 UI 中靠右显示 |

### 2.2 验收标准

- **所有测试用例全部通过**（宪法原则 VI）。
- 存在任何 failed/flaky 用例即视为验收未通过。

---

## 3. 手动验证（desktop）

1. 启动 desktop，创建 saolei 会话。
2. 发送用户消息 → 确认用户消息气泡靠右对齐（Issue 3）。
3. 触发一局游戏，故意让 player 输局 → 确认 planner tab 中出现复盘消息（Issue 1 触发 + Issue 2 内容完整）。
4. 查看 planner tab 的历史消息 → 确认复盘输入包含完整的游戏过程（Issue 2 可见性）。
