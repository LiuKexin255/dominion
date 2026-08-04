# Implementation Plan: Team Template Mode 缺陷修复

**Branch**: `036-team-mode-bugfix` | **Date**: 2026-08-04 | **Spec**: [`spec.md`](./spec.md)

**Input**: Feature specification from `/specs/036-team-mode-bugfix/spec.md`

## Summary

修复 `specs/031-team-template-mode`（Team Template Mode）已实现行为的四个缺陷：

1. **Issue 1（高）**：player 的 createAgent 内部 loop 在游戏结束后不自动停止（LLM 重开新局→递归超限→后处理不可达），导致 lost 场景下 planner 永远不被触发。修复：为 createAgent 添加 `beforeModel` middleware（`canJumpTo: ["end"]`），游戏结束时停止 loop；添加 try/finally 保障后处理可靠执行。
2. **Issue 2（中）**：planner 复盘仅看到终局棋盘快照，缺失完整游戏过程。修复：扩展 ephemeral buffer 新增 `gameLog`，sink 回调累积完整操作序列；planner `buildReviewInput` 渲染完整 gameLog。
3. **Issue 3（低）**：用户消息气泡未右对齐。修复：将 ChatView 中 ChatMessage 外层的 `.msg-row` wrapper 改为 `.msg-pending-wrapper`（不设 `display: flex`，保留 pending 样式）。
4. **Issue 4（中）**：player/planner 节点的 createAgent `invoke()` 未传递外层 graph 的 config（含 `recursionLimit`），内部 createAgent 使用默认 25 而非外层的 1000。修复：节点函数接受 `config` 参数并传递给内部 `invoke()`。

## Technical Context

**Language/Version**: TypeScript（agent team graph 节点）；Svelte（desktop 前端）。

**Primary Dependencies**:
- LangChain `langchain@1.5.4` / `@langchain/langgraph@1.4.8` / `@langchain/core@1.2.3`（createAgent middleware、StateGraph 节点函数、RunnableConfig）。版本统一于 `pnpm-workspace.yaml` catalog。

**Storage**: 无新增存储。`gameLog` 为进程内 ephemeral buffer 字段（非持久化）。

**Testing**: `bazel test`（TS `js_test`，fake-model + fake-tool DI 模式）；大型测试经 testplan skill（宪法原则 VI 强制）。

**Target Platform**: Linux（agent 服务）+ Windows（desktop）。

**Project Type**: 多服务 web/desktop 应用（gRPC 微服务 + 桌面客户端）。本特性仅涉及 agent TS 代码与 desktop Svelte 前端。

**Constraints**: 无 proto / API / RPC 变更。无新增依赖。config 传递为附加性变更（不改变现有 invoke 消息处理逻辑）。

**Scale/Scope**: 修复 3 个 TS 文件 + 1 个 Svelte 文件 + 2 个测试文件。无架构变更。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）核心原则与开发流程门禁评估本特性：

| 原则/门禁 | 评估 | 结论 |
|---|---|---|
| **I. 引用溯源** | 所有引用包含仓库内相对路径（如 `player.ts:152`）与外部来源（LangChain 源码路径）。research.md 含完整源码级验证。 | ✅ 合规 |
| **II. 重构式变更** | Issue 1 的 middleware 方案是对现有架构的扩展（createAgent 已支持 middleware，spike A4 确认），非打补丁绕过；Issue 4 的 config 传递是对节点函数签名的正确补全（LangGraph 节点函数本应接受 config）。 | ✅ 合规 |
| **III. 接口优先设计** | 本特性为 bugfix，无新增 RPC/HTTP 接口。内部模块变更（player/planner/sink 节点行为）已在 contracts 中定义行为契约。 | ✅ 合规 |
| **IV. 测试颗粒度** | 编译+单测属各代码变更 task 内（不单列）；大型测试单列验收 task。 | ✅ 合规 |
| **V. 编码前阅读文档** | `tasks.md` 须为每 phase 声明三分类文档清单（代码规范/官方文档/技术文章），含间接引用。 | ✅ 由 `/speckit.tasks` 落实 |
| **VI. 大型测试验收** | 服务型应用，SC-007 强制大型测试，须经 testplan skill 完整执行（部署→测试→清理），全部用例通过。 | ✅ 合规（见 [`quickstart.md`](./quickstart.md)） |

**门禁执行顺序**：文档阅读 → 实现（bugfix 式）→ 编译+单测 → 引用 → 大型测试验收。

## Project Structure

### Documentation (this feature)

```text
specs/036-team-mode-bugfix/
├── plan.md              # 本文件
├── research.md          # Phase 0 调研（D1-D5）
├── data-model.md        # EphemeralGameBuffer 扩展 + GameLogEntry
├── quickstart.md        # 验证指南
├── contracts/
│   ├── team-graph-fix-contract.md   # Issue 1/2/4 后端契约
│   └── desktop-alignment-fix.md     # Issue 3 前端契约
└── checklists/
    └── requirements.md  # spec 质量检查清单
```

### Source Code (变更范围)

```text
projects/game/agent/src/team/
├── player.ts            # Issue 1: gameEndGuard middleware + try/finally; Issue 4: config 传递
├── planner.ts           # Issue 2: buildReviewInput 渲染 gameLog; Issue 4: config 传递
├── team-sink.ts         # Issue 2: EphemeralGameBuffer + GameLogEntry + sink 回调累积日志
├── graph.test.ts        # 新增测试用例（Issue 1/2/4）
└── team-sink.test.ts    # 新增 gameLog 测试（Issue 2）

projects/game/desktop/frontend/src/components/
└── ChatView.svelte      # Issue 3: 移除外层 .msg-row wrapper
```

**Structure Decision**: 本特性为 bugfix，不新增文件/目录，仅修改现有文件。后端变更集中于 `projects/game/agent/src/team/`，前端变更集中于 `projects/game/desktop/frontend/src/components/ChatView.svelte`。

## 修复方案摘要

### Issue 1: gameEndGuard middleware

player 的 createAgent 添加 `beforeModel` middleware（`canJumpTo: ["end"]`）。当 buffer 中存在未消费的游戏结束事件时返回 `{ jumpTo: "end" }`，跳过 model 调用、停止 loop。`invoke()` 正常返回（不抛异常）。源码级确认见 [`research.md`](./research.md) D1。

同时添加 try/finally 保障后处理：即使 invoke 异常（如 `GraphRecursionError`），`consumeGameEvent` 仍被执行，`gameEnded` 被正确设置。

### Issue 2: GameLog 扩展

扩展 `EphemeralGameBuffer` 新增 `gameLog: GameLogEntry[]`。sink 回调累积日志：`onGameStart` 清空 gameLog 并 push 初始条目（每局重置）；`onMove` push 操作条目；`onGameEnd` push 终局条目。planner `buildReviewInput` 从渲染终局 `gameState` 改为渲染完整 `gameLog`。详见 [`data-model.md`](./data-model.md)。

### Issue 3: ChatView 对齐修复

ChatView 中 ChatMessage 外层的 `.msg-row` 改为 `.msg-pending-wrapper`（不设置 `display: flex`），让 ChatMessage 自身的 `.msg-row.msg-user`（`justify-content: flex-end`）生效。详见 [`contracts/desktop-alignment-fix.md`](./contracts/desktop-alignment-fix.md)。

### Issue 4: config 传递

player 与 planner 节点函数签名从 `(state)` 改为 `(state, config?)`，内部 `invoke()` 调用传入 `config`。使内部 createAgent 继承外层 graph 的 `recursionLimit`（1000）、`signal` 等配置。源码级确认见 [`research.md`](./research.md) D2。

## 复杂度追踪

无 Constitution Check 违规。
