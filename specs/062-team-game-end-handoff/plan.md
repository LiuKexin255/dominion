# Implementation Plan: 062-team-game-end-handoff

**Branch**: `062-team-game-end-handoff` | **Date**: 2026-09-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/062-team-game-end-handoff/spec.md`

## Summary

真实 LLM player 在终局后同 turn 连续开局，059 的复盘锚点（player turn idle）永不到达——复盘永不触发（生产实证见 spec Motivation）。本 feature 以 dsh 工具层官方收束 seam 修复：**任何返回终局棋盘（won/lost）的 saolei 工具成功结果无条件标记 `concludesTurn`**（`ToolRunContext.concludeTurn()`，纯内容驱动、不感知排队/编排状态），经 dsh-agent-loop 聚合后 turn 以 `completed` 无痕收束（无 abort 标记、`concludesTurn` 不落 durable 事件）→ 既有 idle 锚点即刻到达 → 编排器零改动接管复盘（FR-004）。切换判定对排队消息零特判：编排 FIFO 在 idle 分支按既有优先序消化（消化优先于复盘）（spec Clarifications Session 2026-09-12 裁定；与 061 的交互语义由 061 需求文档承载，本 feature 仅保证无冲突）。

变更面收敛为两处生产代码：`GameRuntime`（计算收束标记并经 `ToolOutcome` 携带）与 saolei 工具层（`executeOutcome` 映射 `exec.concludeTurn()`）；编排器/team/relay/webUI 零改动。测试面：单测新增收束标记映射断言（SC-004），大型测试移除"脚本停手"假设（终局总结文本不再存在，fake-llm 既有后续步骤成为"零执行"断言面，SC-001/002/005），`WonChainOnExecutor` / `TerminalWonAndReviewContinues`（game 2）/ `ActiveMemberTransitions`（game 2）三个用例拓态调整（init 即终局 → turn 在 init 后收束）。

## Technical Context

**Language/Version**: TypeScript（ESM，`common/js/dsh-plugins/` 两包）；大型测试 Go（`projects/game/testplan/`）

**Primary Dependencies**: `@deepseek-ai/dsh-tools@0.1.1-rc.2`（收束 seam：`ToolRunContext.concludeTurn()`，`lib/types/index.d.ts:299`；仅 `ToolExecutionSuccess` 可携带 `concludesTurn`，`:388-399`）、`@deepseek-ai/dsh-agent-loop@0.1.1-rc.2`（消费侧：`runGroup.commitReady` 聚合 `lib/index.js:176-187`、`step()` 返回 completed `:685-686`、turn/end reason `:590-598`；**turn 循环以本 step 结果覆盖 `turnEnds`** `:556`、next-step inbox 检查 `:564-571`）、`@deepseek-ai/cordis`（Service 形态不变）。零新增依赖。

**Storage**: N/A（会话历史为进程内存态，059 既有设计；`concludesTurn` 不落 durable 事件——`appendToolResult` 只持久化 content/isError/error/meta，`lib/index.js:302-318`）

**Testing**: 单测 vitest（`common/js/dsh-plugins/saolei/src/index.test.ts`、`common/js/dsh-plugins/saolei-loop/src/game/runtime.test.ts` 既有模式：fakeExec / fake bridge+boardApi doubles）；大型测试 `projects/game/testplan/`（guitar testplan skill 执行，`style/large_test.md`）；fake-llm 脚本 `projects/game/fake-llm/service/testdata/`（`agent_v2_saolei_tools.yaml` / `team_player.yaml` / `team_planner.yaml`，与 `agent_v2_helpers_test.go` 常量及 `message_store_test.go` 索引 lockstep）

**Target Platform**: Linux server（agent_v2 服务，bazel 构建）

**Project Type**: web-service（dsh 插件 + 宿主；本 feature 不动宿主）

**Performance Goals**: N/A（事件驱动本地机制，无吞吐/延迟目标）

**Constraints**: ①无痕性——turn/end reason `completed`、无 interrupted 标记、无合成错误结果（FR-003）；②编排器/team/relay/webUI 零改动（FR-004/FR-005 由既有机制导出）；③收束判定无状态、纯结果内容驱动，工具层不感知编排/复盘/排队状态（FR-001/FR-002，Session 裁定）；④`saolei_remain` 不收束；失败结果（isError）不收束

**Scale/Scope**: 两包生产代码 ~数十行；测试断言/夹具更新五处 Go 用例（WonChain / TerminalWon / TerminalLost / ConversationStream / ActiveMemberTransitions）+ 若干 YAML 注释/常量；契约文档两份（062 新契约 + 051 基线契约 §2/§3 同步为终态）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md` v1.5.0 五门禁逐项评估：

| 门禁 | 原则 | 评估 | 状态 |
|---|---|---|---|
| 1. 文档阅读 | V | 本 plan Phase 0/1 产出 research.md / data-model.md / contracts/ / quickstart.md；tasks.md（后续 `/speckit.tasks`）按三分类显式列出每 phase 文档清单（含间接引用：dsh 物化源码、051 基线契约、060 relay 契约） | PASS（tasks 阶段落实） |
| 2. 实现 | II 重构式 / III 接口优先 | 现有架构（工具层 → runtime → 编排器分层）满足需求：收束标记经 `ToolOutcome` 契约扩展（接口先行，见 [contracts/saolei-turn-conclude.md](contracts/saolei-turn-conclude.md)），无补丁式绕行；无需架构收缩或扩展 | PASS |
| 3. 编译+单测 | IV | 每次代码变更随 `bazel build` + `bazel test`（相关 target），不单列 task | PASS |
| 4. 引用、图表与终态 | I / VII / VIII | 全部结论带可追溯引用（源码行号/spec/契约路径）；spec/plan 图表统一 Mermaid（data-model 消费链与 tasks 依赖链同批迁移）；文档只表述终态（被否决的 cancel 路线仅以"防重复踩坑"必要度记录于 spec Clarifications 与 research.md） | PASS |
| 5. 大型测试验收 | VI | 功能完成后经 testplan skill 实际执行 `guitar run`（部署→测试→清理闭环），全部用例通过为验收标准；不以构建检查替代 | PASS（验收 task 落实） |

**Post-Phase-1 re-check**：设计产物（data-model/contracts/quickstart）与 spec FR-001~006 逐条对齐，无新增违规——见各产出文件。

## Project Structure

### Documentation (this feature)

```text
specs/062-team-game-end-handoff/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── saolei-turn-conclude.md
├── research-notes.md    # 调研草稿（clarify 阶段产出，research.md 的输入）
└── spec.md              # 需求（已终态，含三条 Session 裁定与流程图）
```

### Source Code (repository root)

```text
# 生产代码（仅两处，均已有 BUILD.bazel，无新目录）
common/js/dsh-plugins/saolei-loop/src/game/
├── runtime.ts           # ToolOutcome 契约扩展 + 终局收束标记计算（init/operate 成功路径）
├── runtime.test.ts      # SC-004 单测：收束标记映射矩阵
└── text.ts / board.ts   # 不改动（gameStatus 既有）
common/js/dsh-plugins/saolei/src/
├── index.ts             # executeOutcome：outcome.concludesTurn → exec.concludeTurn()
└── index.test.ts        # 工具层单测：fakeExec 增加 concludeTurn spy 断言

# 测试基建（夹具与断言随收束语义同批更新）
projects/game/fake-llm/service/testdata/
├── agent_v2_saolei_tools.yaml   # 终局链规则注释更新（成为"已脚本化不执行"的后续步骤）
├── team_player.yaml             # 头部行为脚本注释更新
└── team_planner.yaml            # 不变（review 锚定 history_keywords 工具结果文本，非总结文本）
projects/game/testplan/
├── agent_v2_helpers_test.go     # 终局总结文本常量移除（断言锚点改为工具结果文本）
├── agent_v2_game_test.go        # WonChainOnExecutor / TerminalWonAndReviewContinues /
│                                # TerminalLostAndReviewStops / ConversationStreamIndependentOfFlow / ActiveMemberTransitions
│                                # 断言与拓态更新 + 新增收束切面断言
└── agent_v2_game_disconnect_test.go / agent_v2_conversation_test.go / agent_v2_preset_test.go
                                 # nodesktop 总结文本不受影响（isError 不收束）——仅核对，预计零改动

# 零改动面（显式列出防误改）
common/js/dsh-plugins/saolei-loop/src/orchestrator.ts   # FR-004：编排器零改动
common/js/dsh-plugins/team/src/*                        # relay 既有机制（FR-005）
projects/game/agent_v2/src/history.ts / session.ts      # webUI COMPLETED 呈现既有导出（FR-003）
```

**Structure Decision**: 单仓库多 workspace 既有结构，无新增包/目录；变更收敛在 saolei/saolei-loop 两包与 game testplan。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规——不填。
