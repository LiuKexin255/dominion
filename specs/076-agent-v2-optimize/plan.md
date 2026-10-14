# Implementation Plan: agent_v2 优化——wiki 注入收敛、team 成员流程标准化与 announcer 成员化

**Branch**: `076-agent-v2-optimize` | **Date**: 2026-10-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/076-agent-v2-optimize/spec.md`

## Summary

三项优化按三批递进交付（[research.md](research.md) D2）：**批1** wiki 注入移除 + memory 库两层分层（单元/组合）+ unary 策略注入去冗（FR-006，D10）；**批2** announcer 更名与成员化呈现（名册/active_member/成员读取统一，FR-206 先行约束）；**批3** team 成员流程标准化——依据 `survey/saolei-member-switch-flow.md` 裁定 1–24 的 flow 流程控制重构（F-Moore 活动图 + `start(activity)` 指令 + outcome 四态/冻结 + 成员适配器下沉 + 投递成员域化，061 语义经此落地并 supersession）。

技术路线：flow 层**自研 minimal runtime**（D1，零新依赖）；LLM 成员适配器直接使用 dsh-agent 0.2.0-rc.2 原生 `steer()`/`whenIdle()`/`inbox` 底座（D3）；行为保持以 `orchestrator.test.ts` 全量用例为验收基准、两点显式调整（FR-113/D9）。

## Technical Context

**Language/Version**: TypeScript（strict；ESM；vitest；bazel 构建）

**Primary Dependencies**: `@deepseek-ai/cordis`（插件运行时）、`@deepseek-ai/dsh-agent` 0.2.0-rc.2（steer/whenIdle/inbox/cancel(keepInbox)——research.md D3 已验证）、`@grpc/grpc-js`、`@deepseek-ai/dsh-tools` 与 `@deepseek-ai/dsh-system-prompt`（type-only peer）、`@dominion/dsh-grpc-runtime`（`ctx.grpc` client/callUnary 面，零代码改动）

**Storage**: N/A（编排状态进程内存态；memory/wiki 经 gRPC 服务持久，服务零改动）

**Testing**: vitest（`bazel test //...`，宪法 IV 随变更执行）+ testplan skill 大型测试（`guitar run projects/game/testplan/system_test.yaml --suite <name>`，宪法 VI 每批验收；`deploy_agent_v2*.yaml` 为部署配置、不可作为执行入口）

**Target Platform**: Linux server（agent_v2 服务、gateway、web 前端）

**Project Type**: 组合型——公共库（`common/js/memory`）+ cordis 插件（`common/js/dsh-plugins/*`）+ gRPC 服务宿主（`projects/game/agent_v2`）+ web 前端（`projects/game/web`）

**Performance Goals**: 行为保持型重构，无新增性能目标；unary 缺省 10s deadline 等既有策略语义不变（经注入路径唯一化，D10）

**Constraints**: 依赖方向（纯库 MUST NOT import 插件包）；wiki 能力/基建/服务零改动（FR-005）；announcer 名册放宽先行或同批于 flow 重构（FR-206）；061 supersession 处置（FR-114）

**Scale/Scope**: 1 个 flow（5 活动/8 边）、2 个能力单元、3 成员 team、`orchestrator.test.ts` 全量行为基准用例（当前 43 例 / 9 describe）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.5.0）五门禁评估——**全部通过，无违规**：

| 门禁 | 评估 |
|---|---|
| 1. 文档阅读门禁（原则 V） | PASS——本 plan/research 已实际阅读全部引用文档（survey、074/059/060/061/063/065/068 契约、源码锚点均经本次会话核读）；tasks.md 阶段将按三分类格式显式列出各 phase 文档清单 |
| 2. 实现门禁（原则 II / III） | PASS——重构式变更（flow 标准化重构与需求同批交付，非补丁堆叠）；接口先行（三份契约 [contracts/](contracts/) 先于实现定稿） |
| 3. 编译 + 单测门禁（原则 IV） | PASS——每批代码变更附 `bazel build` + `bazel test`，不单列 task |
| 4. 引用、图表与终态门禁（原则 I / VII / VIII） | PASS——仓库内引用用相对路径、仓库外用完整 URL；流程/状态转移图一律 Mermaid（data-model §2.1/§2.4、contracts/flow-orchestration.md §1/§3；文档中的目录树/布局清单为文件结构列表，非流程图）；文档只表述终态（更名不留旧别名；被否决方案仅 research.md 记录防踩坑） |
| 5. 大型测试验收门禁（原则 VI） | PASS——每批经 testplan skill 实际执行（quickstart.md 列明计划与通过标准；任何失败/flaky 即验收未通过） |

## Project Structure

### Documentation (this feature)

```text
specs/076-agent-v2-optimize/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── flow-orchestration.md         # flow/执行层/成员接口/outcome/错误/操作路径/宿主联动
│   ├── memory-library-layering.md    # memory 库两层导出面 + memory-only 注入 + 策略注入 seam
│   └── announcer-member.md           # announcer 更名 + 成员化呈现 + 基线契约修订点
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
common/js/memory/src/                      # 批1：功能单元层（memory/wiki 单元工厂）+ 组合层；
                                           #        store/face 拆分；unary 策略副本删除（seam 注入）
common/js/dsh-plugins/saolei-memory/src/   # 批1：memory-only 注入；callUnary 随 client 传入
common/js/dsh-plugins/grpc-runtime/        # 零改动（策略已导出于服务面，D10 仅消费）
common/js/dsh-plugins/saolei-loop/src/     # 批2：announcer.ts 更名（ANNOUNCER_MEMBER_ROLE 等）；
                                           # 批3：orchestrator 重组为 flow 层/执行层/成员适配器
                                           #       （自研 minimal runtime，无新文件外依赖）
common/js/dsh-plugins/team/src/            # 批3：drain 消费面核对（exactly-once 边界保持，T017 ⑨）
projects/game/agent_v2.proto               # 批2：注释修订（消息形状零变更）
projects/game/agent_v2/src/session.ts      # 批2：三成员名册/GetTeamMember(announcer)/类型；
                                           # 批3：宿主联动（快照派生/watchQuiescence/错误码文案）
projects/game/agent_v2/src/history.ts      # 批2：announcer 成员标签
projects/game/web/frontend/src/            # 批2：announcer 名册/激活渲染；
                                           # 批3：错误文案沿既有渲染路径
specs/061-team-queue-steer/spec.md         # 批3：supersession 标注（FR-114）
specs/074-dsh-grpc-runtime/contracts/memory-library.md  # 批1：§3 修订（策略注入 seam，D10）
specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md  # 批2：§3 item4/§4 修订（统一呈现）
```

**Structure Decision**: 沿用现有仓库布局，无新增顶层目录/包；flow 运行时作为 saolei-loop 包内模块（与 orchestrator 同域），成员适配器同包内独立文件——包边界不因内部分层变化。

## Batch Delivery Plan（供 /speckit.tasks 参考）

| 批 | 内容 | 契约 | 验收门禁 |
|---|---|---|---|
| 批1 | US2：memory 库两层 + memory-only 注入 + 策略注入去冗（FR-006）+ 074 契约修订 | memory-library-layering.md | 单测（独立挂载/组合等价/无 wiki 痕迹/无策略副本）+ wiki_test 回归（SC-002/003/004/009） |
| 批2 | US3：announcer 更名 + 三成员名册/active_member/成员读取 + web + 065 修订 | announcer-member.md | 单测 + conversation 大型测试（SC-006）+ 手工 web |
| 批3 | US1：flow 重构（活动图/指令/成员适配器/投递/错误契约/操作路径/宿主联动）+ 061 supersession | flow-orchestration.md | orchestrator 全量基准（两点调整）+ 结构/冻结/投递/错误契约测试 + conversation/stall/memory_down/disconnect 大型测试（SC-001/005/007/008） |

每批独立过全部五门禁；批间顺序硬约束：批2 ≤ 批3（FR-206）；批1 与批2/批3 无相互依赖（可并行开发，串行验收更稳）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无宪法违规项。主要复杂度决策及其简化论证：flow 自研（vs XState）——定制语义无框架损耗映射、图极小（research.md D1）；unary 策略注入（vs 包装 client）——与 client 注入同型、无动态包装魔法（research.md D10）。
