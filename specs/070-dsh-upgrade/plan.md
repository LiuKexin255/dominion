# Implementation Plan: dsh 升级到 0.1.7 线（core 底座 + agent_v2 官方插件 + preset 模型重构）

**Branch**: `070-dsh-upgrade` | **Date**: 2026-09-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/070-dsh-upgrade/spec.md`

## Summary

全仓库 dsh 闭包从 `0.1.1-rc.2` 线升级到 `0.1.7-rc.2` 线（catalog 一个版本单元：cordis 4.0.4/loader 1.0.5/include 1.0.9/group 1.0.4/timer 1.1.6/node-addon 0.1.6 互锁），`third_party/dsh/core` 底座声明面同线升级（核心 11 包清单不变），agent_v2/demo/workspace 插件适配。核心适配工作为 **preset 模型重构**：目录式 `dsh-agent-presets`（roots/trust/includeUserRoot、mountPreset）→ 声明式 `dsh-agent-preset-registry`（`ctx.agentPresets.register`，eager 激活 + 修订保留）；`preset-authoring` 服务面冻结、内部改走 register 物化，Mongo 保持唯一事实源；模板内联为 preset-authoring `templates` 快照。逐符号 API diff 结论：全部源码消费符号在 0.1.7 存活（[research.md](research.md) D7），适配集中在组合清单、模板数据、preset-authoring 与断言重写。Mongo 旧 preset 数据迁移按用户要求置于**最后一个 phase**（API 导出→升级→回灌/删后重建）。可选新能力（compaction/skill/subagent/session-query/goal/workspace）全部 defer；dsh agent team 与 Dominion team 模式独立（用户已调研，D10）。

## Technical Context

**Language/Version**: TypeScript（Node ESM，NodeNext）；Go（仅 testplan 用例，不涉 dsh）

**Primary Dependencies**: `@deepseek-ai/dsh-*@0.1.7-rc.2`（lockstep 精确 pin）、`@deepseek-ai/cordis@~4.0.4`、cordis-plugin-loader@1.0.5 / include@1.0.9 / group@1.0.4 / timer@1.1.6、node-addon-require-builtin@0.1.6、`@deepseek-ai/schemastery@^3.18.4`；新增 preset 面包 `dsh-agent-preset-registry`；安装级 seam 包（settings/typert-protocol/util-values/brand/session-persistence/sandbox/sandbox-policy/ptc-runtime/user-approval）；前端 `dsh-client-ui-primitives@0.1.7-rc.2` + 重取 `dsh-client-ui-theme@0.1.7-rc.2`（theme 为 vendored 源、非 catalog 依赖，按 registry/unpkg 包文件重取）

**Storage**: Mongo `game_agent_v2.presets`（preset-authoring store，唯一事实源；迁移范围仅此 collection）

**Testing**: bazel build/test（vitest 单测）；testplan skill `guitar run`（game 四拓扑 system_test + demo interface_test + closure audit）

**Target Platform**: Linux 服务器（agent_v2/demo 服务镜像、bazel artifact_pkg_js 闭包）；web 前端（Vite）

**Project Type**: 服务型应用依赖升级 + 插件重构（web-service）

**Performance Goals**: 与升级前一致（无性能目标变化；行为不回归为硬约束）

**Constraints**: catalog 单一版本单元（不拆命名 catalog）；dsh 精确 pin、cordis 家族锁线内值；pnpm 官方流程刷 lockfile；旧 `dsh-agent-presets` 全声明面移除

**Scale/Scope**: 消费方 = third_party/dsh/core、projects/game/agent_v2、experimental/dsh/demo/agent（+agent-plugins/demo-echo）、common/js/dsh-plugins/*（9 插件）、projects/game/web/frontend；不改 Go 侧与桌面前端

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁 | 状态 | 说明 |
|---|---|---|
| 1 文档阅读（原则 V） | ✅ | 规划期已实读：registry/preset/arch-note 上游源与 README、unpkg 类型面、仓库全部消费面；tasks.md 将按三分类显式列出各 phase 文档 |
| 2 实现门禁（原则 II/III） | ✅ | preset 重构按新模型映射而非打补丁（目录面/ fs seam 删除，D3/D4）；接口先行：contracts/ 三份（组合清单与声明面、preset-authoring 映射、迁移 runbook）先于实现 |
| 3 编译+单测门禁（原则 IV） | ✅ | 编译+单测作为各开发任务内置门禁（tasks.md Phase 划分）；不单列 task——closure audit 与大型测试作为独立验收环节（原则 IV/VI） |
| 4 引用/图表/终态门禁（原则 I/VII/VIII） | ✅ | 全部文档带仓库相对路径/完整 URL 引用；Mermaid 图（data-model §3.5/§5、migration runbook）；文档只表述终态 |
| 5 大型测试验收（原则 VI） | ✅ | tasks.md Phase 8 实际执行 `guitar run projects/game/testplan/system_test.yaml`（四拓扑）+ `experimental/dsh/demo/testplan/interface_test.yaml` + closure audit，全部用例通过为验收；Mongo 迁移（Phase 8 T027）含执行证据记录 |

## Project Structure

### Documentation (this feature)

```text
specs/070-dsh-upgrade/
├── plan.md              # This file
├── research.md          # Phase 0 output（D1–D14 决策 + 逐符号 API diff + 复用调研）
├── data-model.md        # Phase 1 output（版本线/声明面/preset 实体/迁移数据）
├── quickstart.md        # Phase 1 output（六场景验证指南）
├── contracts/
│   ├── composition-manifest.md      # catalog/cordis.yml/BUILD 声明面契约
│   ├── preset-model-mapping.md      # preset-authoring 服务面冻结 + register 映射
│   └── mongo-preset-migration.md    # 最后 phase 迁移 runbook
└── tasks.md             # Phase 2 output (/speckit.tasks - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
third_party/dsh/core/                  # 底座：package.json/BUILD.bazel/version.ts 升线（11 包清单不变）
pnpm-workspace.yaml                    # catalog 切线 + 新包条目 - agent-presets
projects/game/agent_v2/
├── cordis.yml                         # 新行集合（+session-projection/registry −agent-presets；模板经 preset-authoring templates 快照）
├── package.json / BUILD.bazel         # 声明面扩展
├── preset-templates/                  # 移除（模板内联为 preset-authoring templates 快照）
└── src/                               # dsh.test.ts 断言重写；其余源码符号面不变（D7）
experimental/dsh/demo/agent/           # 同构迁移（cordis.yml/package.json/BUILD/测试 + presets-templates 移除）
common/js/dsh-plugins/
├── preset-authoring/                  # 重构：derive→register、fs seam 删除、templates 快照注入
└── (其余 8 插件)                       # 版本随 catalog；BUILD 声明面微调
projects/game/web/frontend/            # ui-primitives 0.1.7 + src/dsh-theme/ 重取
experimental/dsh/demo/testplan/        # closure audit（代码零改动，声明驱动）
```

**Structure Decision**: 不新增目录/包；preset-authoring 保持单插件内重构（模板声明注入经其 Config 扩展，见 contracts/preset-model-mapping.md §2.3）；demo-echo 不变。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违反项。（新增安装级 seam 包为 0.1.7 peer 义务，非复杂度自选——research D6 义务矩阵。）
