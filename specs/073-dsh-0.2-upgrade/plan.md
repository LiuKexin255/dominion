# Implementation Plan: dsh 升级到 0.2.0 线（core 底座 + 官方插件 + web 前端 webUI）

**Branch**: `073-dsh-0.2-upgrade` | **Date**: 2026-09-29 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/073-dsh-0.2-upgrade/spec.md`

## Summary

将全仓库 dsh 闭包从 `0.1.7-rc.2` 线同线升级到 `0.2.0-rc.2`（registry `next` 线；cordis 家族互锁值与 0.1.7 线相同、保持不动）。调研实测（`specs/073-dsh-0.2-upgrade/research.md` D2）本次升级为纯增量：28 个 diff 包中 20 个逐字节相同，变更包全部向后兼容、无新增 peer 义务、无 exports 变化——组合清单与消费源码预期零适配；唯一框架行为变化是 `dsh-agent-loop`/`dsh-session` 的工具调度失败恢复（失败 step 补记保守恢复结果，对话不再卡死）。前端面在资产对齐（ui-primitives 升级 + vendored theme 重取，`design-platform.css` 新增 8 token）基础上，按澄清裁定（spec Clarifications 2026-09-29）仅采用有承载面的新设计能力：ReasoningRow 流式运行态 shimmer 动画（TextShimmer + `--dsw-alias-label-shimmer`）与 DisclosureRow 头部动画。

## Technical Context

**Language/Version**: TypeScript（dsh 消费面：agent_v2/demo/workspace 插件/web 前端）；Go 侧（gateway/web server）不依赖 dsh、不在迁移面。

**Primary Dependencies**: `@deepseek-ai/dsh-*` 家族 26 个 catalog 条目 `0.1.7-rc.2` → `0.2.0-rc.2`（精确 pin）；互锁包保持现值：cordis `4.0.4`、cordis-plugin-loader `1.0.5`、cordis-plugin-include `1.0.9`、cordis-plugin-group `1.0.4`、cordis-plugin-timer `1.1.6`、node-addon-require-builtin `0.1.6`、`@deepseek-ai/schemastery` `^3.18.4`（research.md D1 互锁矩阵）。

**Storage**: N/A（无持久化变化；Mongo preset 记录零迁移——preset 模型 0.2.0 线稳定，research.md D6）。

**Testing**: `bazel test`（JS 侧 vitest 经 bazel 门禁；前端类型面额外包内 tsc 手动核验——先例 `specs/070-dsh-upgrade/research.md` D9、本特性遗留验证点 3）；大型测试经 testplan skill（`guitar run`）：`projects/game/testplan/system_test.yaml`（agent_v2 四拓扑）与 `experimental/dsh/demo/testplan/interface_test.yaml`（demo 接口面）；closure audit（`experimental/dsh/demo/testplan/closure_audit_test.go` 三断言）为独立 `go_unittest`，不属 `interface_test.yaml` 的用例。

**Target Platform**: Linux 服务端部署（agent_v2/demo/gateway）+ 浏览器前端（game web）。

**Project Type**: monorepo 依赖版本线迁移（catalog/声明面驱动）+ 前端设计能力采用（单组件改动）。

**Performance Goals**: N/A（无性能面变化；shimmer 动画开销由上游组件实现承载，组件测试覆盖行为正确性）。

**Constraints**: dsh 闭包整体一个版本单元（零跨线混装、cordis 全仓库唯一一份）；lockfile 只经 `pnpm up` 官方流程刷新；底座保持零插件；不得删除/跳过既有用例达成通过。

**Scale/Scope**: catalog 26 条切值 + `packageExtensions` 2 处版本键及 3 处嵌套 `@deepseek-ai/*` 值 + 底座声明面（`package.json` 经 catalog / `BUILD.bazel` 声明与注释 / `version.ts`）+ vendored theme 1 文件重取（`design-platform.css`）+ 前端 1 组件采用点（ReasoningRow）+ README/源码/测试注释的 `0.1.7` 版本串与版本线表述清理（SC-006，含 `pnpm-workspace.yaml` 说明注释与 `BUILD.bazel` 注释）+ 全量回归（单测 + 2 个大型测试计划）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪法原则） | 状态 | 依据 |
|---|---|---|
| 1. 文档阅读门禁（V） | ✅ 规划期已读并落实 | research.md 全部结论基于 registry 实测与 tarball diff（非引用传递）；tasks 阶段每 phase 文档清单由 `/speckit.tasks` 按 V 的三分类格式落 |
| 2. 实现门禁（II 重构式 / III 接口优先） | ✅ | 版本线切换在声明面（catalog/闭包清单）一次完成，非逐点 patch（II）；对外接口零变化（组合清单行集合、服务 API、前端页面结构），对内契约以 contracts/ 三份文档先行（III） |
| 3. 编译 + 单测门禁（IV） | ✅ | 每次变更经 `bazel build`/`bazel test` 相关 target；作为开发任务内置，不单列 task |
| 4. 引用、图表与终态门禁（I/VII/VIII） | ✅ | 全部结论带 registry/GitHub 引用（I）；spec 已按终态精化（ChatView 采用点收敛，VII）；本文档无图表，如后续文档需要图表用 Mermaid（VIII） |
| 5. 大型测试验收门禁（VI） | ✅ | agent_v2 `system_test.yaml` 与 demo `interface_test.yaml` 实际执行（部署→测试→清理闭环）全部通过作为验收；禁止 build-only 替代 |

**Post-Phase-1 复检**: contracts 三份（declaration-surface / theme-vendoring / shimmer-adoption）均以可验证契约表达，无接口预留过度设计；无违例项 → Complexity Tracking 无需填写。

## Project Structure

### Documentation (this feature)

```text
specs/073-dsh-0.2-upgrade/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── declaration-surface.md   # 声明面变更契约（catalog/core/BUILD/version.ts/packageExtensions）
│   ├── theme-vendoring.md       # vendored theme 重取契约 + FR-022 断言语义
│   └── shimmer-adoption.md      # ReasoningRow shimmer 采用 UX 契约
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
pnpm-workspace.yaml                     # catalog 26 条 dsh 条目切值 + packageExtensions 2 键及嵌套 @deepseek-ai/* 值
third_party/dsh/core/
├── package.json                        # 依赖全为 catalog: 协议，值随 catalog 切 0.2.0-rc.2（文件本身零改动；cordis 家族/node-addon 6 条值不变，共 11 包闭包）
├── BUILD.bazel                         # npm_deps 枚举（gazelle）+ 闭包注释随线更新（0.2.0-rc.2 事实）
└── version.ts                          # 快照标识 dsh-core@0.2.0-rc.2（注释互锁值不变，引用 specs/073 research D1）
projects/game/agent_v2/                 # 依赖经 catalog 同线；cordis.yml 行集合不变（预期零改动）
experimental/dsh/demo/agent/            # 同上
common/js/dsh-plugins/*                 # workspace 插件，dsh-* 依赖经 catalog 同线（预期零改动；README/src 注释版本串随线）
# README/源码/测试注释的 0.1.7 版本串与版本线表述随线（SC-006）
projects/game/{web,web/frontend,agent_v2}/README.md
projects/game/agent_v2/{BUILD.bazel,src/{system-prompt.ts,dsh.test.ts,history.ts,history.test.ts,session.test.ts}}
# 失败恢复用例：优先并入 history.test.ts/session.test.ts；无自然归属时新增 src/tool-recovery.test.ts（lib_test 的 data glob 自动收集）
experimental/dsh/demo/{README.md,agent/BUILD.bazel,agent/src/composition.test.ts}
common/js/dsh-plugins/{llm-glm,llm-opencode-go,saolei,saolei-loop,desktop-bridge}/README.md
common/js/dsh-plugins/{llm-glm,llm-opencode-go,team,saolei-loop,memory-service}/src/*.ts
projects/game/web/frontend/
├── package.json                        # ui-primitives 经 catalog 同线
├── src/dsh-theme/
│   ├── design-platform.css             # 重取 0.2.0-rc.2（唯一变化 sheet，+8 token）
│   └── README.md                       # 版本记录更新
└── src/components/ReasoningRow.tsx     # shimmer 采用点（TextShimmer + DisclosureRow running prop）
                                        # 及 ReasoningRow.test.tsx 组件测试
```

**Structure Decision**: 仓库既有 monorepo 结构不动；本特性全部改动落在上述既有路径，无新目录、无新包。version.ts 注释中的 cordis 互锁值描述与现状一致（0.2.0 线互锁值未变），仅 dsh 版本串更新。

## Complexity Tracking

> 无宪法违例，无需填写。
