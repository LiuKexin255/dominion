# Implementation Plan: Agent v2 team 模式优化（部署配置收敛 / 常量库 / 实时流修复 / 广播净化 / 提示词分层）

**Branch**: `060-agent-v2-team-optimize` | **Date**: 2026-09-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/060-agent-v2-team-optimize/spec.md`（含执行期用户指令：US4 必须先定位引入根因、修复与根因一致，找不到根因则停下说明——**两个根因均已定位并通过代码与 git 历史实证**，见 [research.md](research.md) R5/R6）

## Summary

059 team 模式交付后的优化批次，六个切片：(1) deploy 平台新增产物位置保留环境变量 `DOMINION_ARTIFACT_DIR`，agent-v2 preset 模板根改由其推导，部署清单移除全部 preset 路径 env；(2) preset 唯一事实源化——用户 preset 的组合文件副本不再被维护，物化时从 Mongo store 记录派生**纯临时组合文件**经官方 `mountPreset` 挂载（用完即弃、幂等重建，`PRESET_WRITABLE_ROOT` 语义消亡）；(3) common 新增常量库（Go + JS）收录平台保留环境变量名，deploy 工具/服务与 `projects/game/` 使用方切换引用；(4) **根因修复**两个 webUI 实时缺陷——成员视角实时呈现被消费的用户输入（新增消费帧）、工具结果终态化补全三投影面（live 草稿早退路径不再跳过已固化条目）；(5) team 查询面暴露单一"当前激活成员"值 + 主界面 system prompt 入口 + 广播净化（移除 think、单一 XML 标注形态）；(6) 提示词三层所有权重构（玩法+操作进 saolei-loop 全员 section、工具守则仅剩用法、persona 去重）。

技术路线全部决策与依据见 [research.md](research.md)（R1–R12，US4 根因链见 R5/R6）；实体与契约见 [data-model.md](data-model.md) 与 [contracts/](contracts/)。

## Technical Context

**Language/Version**: TypeScript（Node ESM：agent_v2、dsh 插件族、web 前端 React 18）+ Go（deploy k8s builder/runtime、gateway）+ proto（`projects/game/agent_v2.proto`）；Bazel 构建。

**Primary Dependencies**: `@deepseek-ai/dsh` 全家桶 0.1.1-rc.2 精确 pin（本 feature 消费其 `dsh-agent-presets` 根导出的 `mountPreset`——直接挂载合成 AgentPreset，无需 root 扫描发现）；自研插件族 `@dominion/dsh-{team,saolei,saolei-loop,preset-authoring,memory,desktop-bridge,llm-glm}`；grpc-js；Mongo（preset store，`game_agent_v2.presets`）。

**Storage**: Mongo `game_agent_v2.presets`（唯一事实源地位强化：CRUD 只写 store）；无新增存储。

**Testing**: vitest（单测）+ `bazel test`；大型测试经 testplan skill（`guitar run`，fake-llm + fake-desktop，`projects/game/testplan/system_test.yaml` 三 suite）。

**Target Platform**: Linux 服务（deploy 平台部署）；web 浏览器。

**Project Type**: web-service（gRPC + REST gateway）+ web UI + 平台部署工具（k8s builder）。

**Performance Goals**: 沿用现状；无新增性能目标（广播去重与 think 移除净减少 token）。

**Constraints**: dsh 0.1.1-rc.2 精确 pin；`mountPreset` 官方约束——组合必须可作文件路径加载（内存组合无官方 API，R2）；部署 env 为纯字符串无插值（`tools/release/deploy/pkg/schema/deploy.schema.json`）。

**Scale/Scope**: 单实例低并发（既有语义）；preset 规模个人级。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

对照 `.specify/memory/constitution.md`（v1.4.0）七原则与五门禁，本 plan 的合规承载：

| 原则/门禁 | 承载 |
|---|---|
| I 引用溯源 | research/contracts/data-model 全部结论附仓库内相对路径或仓库外 URL 引用；US4 根因链引用具体 commit（2a3c4d4/d3fb732/4dff297）与代码行 |
| II 重构式变更 | preset 可写副本机制**收缩**为 store 派生临时文件（删除 copy-then-patch 双写维护面，非堆叠）；US4 修复对齐根因（补全 settle 三投影面 / 补消费帧），不改动回填收敛等不该改的面（R5/R6 修复边界） |
| III 接口优先设计 | Phase 1 产出 [contracts/](contracts/)（team-api 增量、preset-derivation、deploy-env、const-lib、prompt-sections）先于 tasks/实现 |
| IV 测试颗粒度 | 编译+单测归属各实现变更（不单列 task）；大型测试单独验收（quickstart.md + testplan） |
| V 编码前阅读文档 | 由后续 `/speckit.tasks` 在 tasks.md 中按三分类清单声明 |
| VI 大型测试验收 | quickstart.md 定义验证场景；验收必须实际 `guitar run` 全量通过 |
| VII 终态表述 | 文档与注释只表述终态；被否决方案（copy-then-patch 维护、头行摘要形态等）仅在 research.md 记录必要理由 |

无违规项 → Complexity Tracking 不适用。

## Project Structure

### Documentation (this feature)

```text
specs/060-agent-v2-team-optimize/
├── plan.md              # This file
├── research.md          # Phase 0 output — 全部技术决策、US4 根因链与修复边界
├── data-model.md        # Phase 1 output — 实体与状态模型（帧词汇/Team 视图/preset 派生/常量）
├── quickstart.md        # Phase 1 output — 端到端验证指南
├── contracts/           # Phase 1 output
│   ├── team-api.md      # 059 team-api 契约的增量修订（消费帧/激活成员/广播格式/实时性）
│   ├── preset-derivation.md  # preset store 唯一事实源 + 使用时派生挂载契约
│   ├── deploy-env.md    # 产物位置保留变量注入契约 + 部署清单收敛
│   ├── const-lib.md     # 常量库（Go+JS）契约与采用范围
│   └── prompt-sections.md    # 提示词三层所有权（loop 玩法 section/工具守则/persona）
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/infra/deploy/runtime/k8s/builder.go   # 注入 DOMINION_ARTIFACT_DIR（stateful/stateless 两处保留变量块）
projects/infra/deploy/runtime/k8s/executor.go  # ReservedEnvironmentVariableNames 追加 DOMINION_ARTIFACT_DIR（保留名校验面）
tools/release/deploy/README.md                 # 保留变量清单 + 产物位置变量文档
common/gopkg/constants/                        # 新增：Go 常量库（保留环境变量名全集）
common/js/constants/                           # 新增：JS 常量库 @dominion/common-js-constants
projects/game/deploy.yaml                      # agent-v2 env 块 preset 项移除
projects/game/testplan/deploy_agent_v2*.yaml   # 三份拓扑同步骤移除
projects/game/agent_v2/cordis.yml              # roster roots 收缩为两模板根；模板根路径改产物变量推导
projects/game/agent_v2/src/dsh.ts              # 模板根解析（DOMINION_ARTIFACT_DIR 派生 + fail-loud）
projects/game/agent_v2.proto                   # ChatEvent 消费帧、Team.active_member
projects/game/agent_v2/src/history.ts          # appendMemberViewUser 消费帧扇出
projects/game/agent_v2/src/session.ts / server.ts  # TeamView.activeMember、GetTeam 投影
projects/game/agent_v2/preset-templates/       # persona 瘦身（去玩法/操作描述）
common/js/dsh-plugins/preset-authoring/src/    # materialize 重构为 derive（store→临时组合文件）；CRUD store-only
common/js/dsh-plugins/team/src/broadcast.ts    # think 移除 + 单一 XML 标注形态
common/js/dsh-plugins/team/src/section.ts      # 广播格式约定同步
common/js/dsh-plugins/saolei/src/index.ts      # guidance 收缩为纯工具用法
common/js/dsh-plugins/saolei-loop/src/index.ts # 注册 saolei:game 玩法+操作 section（host 级、全员可见）
projects/game/web/frontend/src/store/chat.ts   # toolResult 三投影面 settle；消费帧归约
projects/game/web/frontend/src/App.tsx / components/  # 激活成员呈现、system prompt 主界面入口
projects/game/fake-llm/service/testdata/team_*.yaml  # 夹具锚点随格式/提示词同步
projects/game/testplan/*_test.go               # 断言增量（消费帧/激活成员/新格式/实时性）
```

**Structure Decision**: 沿用仓库既有分区，无新目录（常量库两个新包对齐 `common/gopkg/*`、`common/js/*` 既有形态）；契约以增量修订 059 既有契约文档的形式交付（[contracts/team-api.md](contracts/team-api.md) 为 059 同名契约的修订版语义）。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规项，不适用。
