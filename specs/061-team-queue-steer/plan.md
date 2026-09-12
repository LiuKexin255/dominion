# Implementation Plan: team 排队消息 step 边界进入与 turn 语义表述修正

**Branch**: `061-team-queue-steer` | **Date**: 2026-09-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/061-team-queue-steer/spec.md`

## Summary

恢复 v1（038）建立、v2 迁移中丢失的排队消息 mid-turn 进入语义：team 当前激活成员 turn 在途时，用户消息经 dsh 原生 `steer` 于**下一个 step 边界随工具结果之后**作为独立普通用户消息进入成员上下文（消费后无排队特殊语义）；turn 结束前未被 claim 的消息由 steer-wake 自愈为同成员新回合（消化优先于切换经 drain interval 自然保持）；静止路径、relay 边界、编排优先级不变。同步修正 059 家族文档与编排器注释中的 turn 冗余表述（删除不存在的"工具调用引发的后续 turn"触发源枚举、"team turn 持续流"命名、049/059 supersession 注记）。

技术路线（[research.md](research.md) R1–R8）：**全部基于 dsh 原生能力**——`agent.steer`（next-step + 唤醒）、claim 批语义（step 边界取全部 next-step）、steer-wake 自愈（idle 同步开 turn）、`agent.cancel` 默认清 inbox；proto **零变更**（消费信号复用 060 交付的 `member_view{sender:"user"}` 帧）；fake-llm 服务**零改动**（`keywords` 匹配最后一条 user 消息天然承载 step 级断言分叉）。

## Technical Context

**Language/Version**: TypeScript（Node ESM，agent_v2 + dsh 插件）+ React（web 前端）；Bazel 构建。

**Primary Dependencies**: `@deepseek-ai/dsh-agent@0.1.1-rc.2`（`Agent.steer`/`agent.inbox`/`cancel{keepInbox}`、`agent/inbox/*` 通知）；自研 `@dominion/dsh-saolei-loop`（编排层）+ agent_v2 宿主；web 前端 store。无新增依赖。

**Storage**: 无新增（team 会话内存态 + dsh durable session log，现状不变）。

**Testing**: vitest + `bazel test`（单测/组件测）；大型测试经 testplan skill（`guitar run`，fake-llm + fake-desktop）——验收必须全量通过（constitution VI）。

**Target Platform**: Linux 服务（deploy 平台）；web 前端。

**Project Type**: web-service（gRPC）+ web UI（行为修正，无新资源面）。

**Performance Goals / Constraints / Scale**: 沿用现状（单实例 stateful、低并发）；dsh 0.1.1-rc.2 精确 pin 不变；组合清单零变更。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

对照 `.specify/memory/constitution.md`（v1.4.0）：

| 原则/门禁 | 承载 |
|---|---|
| I 引用溯源 | research/data-model/contracts 全部结论附仓库内相对路径或仓库外 URL（dsh 源码、opencode 源码、survey、既有 spec 行号） |
| II 重构式变更 | 排队输入路径收敛为 dsh 原生 inbox（消解 v2 迁移时降级为编排 FIFO 的补丁形态）；FR-002 回退由 dsh 自愈承担而非新增编排消化逻辑——净简化（编排 FIFO 职责收缩为静止路径） |
| III 接口优先设计 | [contracts/orchestrator-input.md](contracts/orchestrator-input.md)（编排输入面）与 [contracts/web-queue-ui.md](contracts/web-queue-ui.md)（前端 chip 生命周期）先于 tasks/实现；对外 proto 零变更 |
| IV 测试颗粒度 | 编译+单测随各实现变更（不单列 task）；大型测试单独验收（quickstart V1–V5） |
| V 编码前阅读文档 | 由 `/speckit.tasks` 在 tasks.md 按三分类清单声明 |
| VI 大型测试验收 | quickstart 定义 V1–V5；验收 = 实际 `guitar run` 全量通过（构建通过不构成验收） |
| VII 终态表述 | 059/049 仅增补注记/修正表述、不保留迭代过程叙述；被否决备选仅在 research.md 记录 |

无违规项 → Complexity Tracking 不适用。

## Project Structure

### Documentation (this feature)

```text
specs/061-team-queue-steer/
├── spec.md                        # 需求（含 Clarifications 语义基准/opencode 对齐/dsh inbox 机制确认）
├── plan.md                        # 本文件
├── research.md                    # R1–R8 技术决策与依据
├── data-model.md                  # 消息生命周期/编排状态扩展/静止判定/术语终态
├── quickstart.md                  # V1–V5 验证场景
├── contracts/
│   ├── orchestrator-input.md      # saolei-loop 编排输入契约（steer/计数/取消/静止）
│   └── web-queue-ui.md            # 前端排队指示与消费消除契约
└── tasks.md                       # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/dsh-plugins/saolei-loop/src/
├── orchestrator.ts                # submit 在途分支 steer + steeredPending 计数 + 静止判定 + 头注释收敛
└── orchestrator.test.ts           # 单测：两路径/计数闭环/cancel/静止兜底

projects/game/agent_v2/src/
├── session.ts                     # watchQuiescence 静止条件扩展（inbox 兜底）；send 的 position 语义随 snapshot 自然生效
└── session.test.ts                # 单测补充

projects/game/web/frontend/src/
└── store/chat.ts (+ chat.test.ts) # chip 消除改挂 member_view{sender:"user"}（文本首匹配）；turn_start 出队退役
                                   # （chat.test.ts:247 既有用例随语义更新）

projects/game/fake-llm/service/testdata/
└── team_player.yaml                # 就地扩展：多步工具 + steer 探针/收尾/落地引用模板（服务零改动）

projects/game/testplan/
└── agent_v2_game_test.go（既有 team 用例扩展）+ system_test.yaml 挂载  # V1–V3 用例

# 文档/注释修正（research.md R8 清单，11 条；命名点含 059 research.md:80/:91）
specs/059-agent-v2-team-mode/{spec.md,data-model.md,research.md,contracts/dsh-plugins.md,contracts/team-api.md}
specs/049-agent-v2-dsh-init/{spec.md,research.md}
common/js/dsh-plugins/saolei-loop/src/orchestrator.ts（头注释）
```

**Structure Decision**: 全部原地修改，无新增包/目录/依赖/组合行；proto 与 gateway/proxy 零改动。

## Implementation Phases（供 /speckit.tasks 细化）

| Phase | 内容 | 验证门禁 |
|---|---|---|
| A | 编排层：submit 在途 steer、steeredPending 计数闭环、landed 落地收编与搭车投递（cancel 收编 / 下次 Send inject 搭车）、静止判定扩展、cancel 注释与头注释收敛（`orchestrator.ts` + 单测） | `bazel test //common/js/dsh-plugins/saolei-loop` |
| B | 宿主：`watchQuiescence` 静止条件（`session.ts` + 单测） | `bazel test //projects/game/agent_v2` |
| C | 前端：chip 消费消除（`chat.ts` + 组件/store 测试，既有 turn_start 用例更新） | `bazel test //projects/game/web/...` |
| D | 文档修正：R8 清单 11 条（059×8 含契约 steer 路径同步与 research 命名点、049×2 注记、orchestrator 注释随 A 完成） | SC-004 文本检索（rg 断言） |
| E | 大型测试：fake-llm testdata 模板组 + testplan 用例（V1–V3 + V6〔062 终局收束交互〕）+ 全量执行 | `guitar run` 部署→测试→清理闭环，V1–V5 全部通过（V5 = SC-004 检索，Phase D 承载） |

**前置 feature**：`specs/062-team-game-end-handoff/` 先行落地——两 feature 无实现耦合（062 编排器零改动，本 feature 的编排层变更干净叠加）；唯一交互面（终局收束 × 在途 steer：pending steered 消息使终局 turn **同 turn 延展**消费、复盘交接在后——与 062 Session 2026-09-12 裁定二的优先级语义一致）由本 feature 定义并测试（spec Edge Cases、research R3a、contracts/orchestrator-input.md §6、quickstart V6/T012）；机制依据：dsh turn 循环停止条件 `turnEnds && inbox.nextStep.length === 0` 与 turnEnds kind 无关（`dsh-agent-loop lib/index.js:564-571`）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规项，不适用。
