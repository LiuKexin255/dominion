# Implementation Plan: JS bootstrap 组件与 experimental 目录统一为 js

**Branch**: `053-js-bootstrap-migration` | **Date**: 2026-09-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/053-js-bootstrap-migration/spec.md`；用户方案指令："迁移 bootstrap 注意遵循 js 风格和规范，不要过度迁移 golang 风格"。

## Summary

交付共享 JS bootstrap 公共包 `@dominion/common-js-bootstrap`（`common/js/bootstrap/`），与 Go bootstrap（`common/gopkg/bootstrap/`）行为语义全量对齐（组件编排、健康端点、信号优雅退出、关停预算、gRPC/HTTP 适配器、Daemon 监督、意外退出监测），但 API 形态采用 JS 惯用语而非 Go 移植（AbortSignal、async/await + throw、AggregateError、options object、字符串字面量联合、`create*` 工厂函数；依据 `specs/005-js-runtime-idioms/research.md` 确立的"去除 Go 惯用法"先例与用户指令）。

同时将 `experimental/ts/`（grpc_hello_world、hello_world、team_graph_spike）迁移至 `experimental/js/`，所有携带 `ts` 的标识符（proto 包名、HTTP 路径、Go importpath、服务工件名 `grpc-hello-world-ts`）一并更名为 `js`；两个 experimental 服务接入共享 bootstrap 并删除手写样板。行为锚点：specs/052-deploy-health-probe 已确立的健康端点约定（38080/healthz、先进先出、启动失败即退出）与 048 的 OTel 插桩时序契约（bootstrap 两段式、静态图禁入 `@grpc/grpc-js`）。

## Technical Context

**Language/Version**: TypeScript（`target: ES2020`、`module: nodenext`，swc `es6` + `preserveImportMeta` 锁步，见 `style/javascript.md`）；Node ESM 运行时。

**Primary Dependencies**:
- 运行时：Node 内置（`node:http`、`node:module`、`node:events`）；`@dominion/common-js-logs`（结构化日志，仓库既有包）。
- 类型（devDependency，编译期擦除）：`@grpc/grpc-js`（gRPC 适配器仅 `import type`，包运行时零 grpc-js 依赖——OTel 插桩时序约束，见 research.md D2）。
- 不引入任何新的第三方运行时依赖。

**Storage**: N/A（无持久化）。

**Testing**: 单测 = vitest（`tools/dev/js/vitest_test.bzl` 宏 + DI mock 约定，见 `style/javascript.md`）；构建门禁 = bazel；大型测试 = guitar testplan（`style/large_test.md`），复用 grpc_hello_world 既有 interface_test.yaml（default + selfheal 两个 suite）。

**Target Platform**: Linux 服务器（k8s 部署的 JS 服务）与本地开发环境。

**Project Type**: 共享库包（workspace package）+ 仓库内 experimental 服务迁移与接入。

**Performance Goals**: N/A（生命周期管理器，非热路径）。

**Constraints**:
- OTel 插桩 ESM 契约（`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`）：服务 bootstrap 静态图 MUST NOT 加载 `@grpc/grpc-js`；两段式入口形态 MUST 保持。
- 健康端点约定（`specs/052-deploy-health-probe/contracts/bootstrap-health.md` §1）：38080/healthz、先进先出、启动失败视同组件失败、无配置项无开关无校验。
- ESM 包契约（`specs/048-js-esm-migration/contracts/esm-package-conventions.md`）：`"type": "module"`、相对导入带 `.js`、`export type` 显式、CJS 依赖 default import（grpc-js 例外允许具名导入）。

**Scale/Scope**: 新增 1 个公共包（约 8 个源文件）；迁移 3 个子项目；仓库内约 20 个文件含 `experimental/ts` / `grpc-hello-world-ts` 引用需更新。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|--------|--------|------|
| I. 引用溯源 | ✅ | 本方案所有引用均为仓库相对路径或完整 URL；下游工件（research/contracts/data-model/quickstart）同标准 |
| II. 重构式变更 | ✅ | 以共享组件替代两处手写样板是设计层收敛而非补丁；迁移属于命名统一的结构调整，与功能变更同批交付 |
| III. 接口优先设计 | ✅ | Phase 1 产出 `contracts/bootstrap-js-api.md`（公共 API 契约）与 `contracts/migration-rename-map.md`（更名映射契约），实现前定契 |
| IV. 测试颗粒度 | ✅ | 编译+单测在每次代码变更任务内执行（不单列 task）；大型测试单列验收 task（guitar run） |
| V. 编码前阅读文档 | ✅ | tasks 阶段按 phase 声明三分类文档清单（本方案工件已实际阅读） |
| VI. 大型测试验收 | ✅ | 验收 task 通过 testplan skill 实际执行 `guitar run experimental/js/grpc_hello_world/testplan/interface_test.yaml`（部署→测试→清理闭环，全部用例通过） |
| VII. 终态表述 | ✅ | 迁移完成后仓库不含 `experimental/ts` 残留；契约/注释只表述终态（gRPC 适配器与 Go 的 serve-loop 差异按当前形态记录，不记录演进过程） |

无门禁违例，无需 Complexity Tracking 豁免（新增 1 个包遵循 `common/js/*` 既有结构，无过度设计）。

## Project Structure

### Documentation (this feature)

```text
specs/053-js-bootstrap-migration/
├── plan.md                       # 本文件
├── research.md                   # Phase 0：技术决策与依据
├── data-model.md                 # Phase 1：实体与状态机、更名映射
├── quickstart.md                 # Phase 1：端到端验证指南
├── contracts/
│   ├── bootstrap-js-api.md       # @dominion/common-js-bootstrap 公共 API 契约
│   └── migration-rename-map.md   # ts→js 更名映射与审计判据
└── tasks.md                      # /speckit.tasks 产出（本命令不创建）
```

### Source Code (repository root)

```text
common/js/bootstrap/                     # 新增：@dominion/common-js-bootstrap
├── package.json                         # "type": "module"；grpc-js 仅 devDependencies
├── tsconfig.json / .swcrc               # 锁步（esm-package-conventions §2）
├── BUILD.bazel                          # gazelle 生成 + js_runtime_library :runtime_pkg + vitest_test :lib_test
└── src/
    ├── index.ts                         # barrel：类型 export type、工厂具名导出
    ├── component.ts                     # Component / Stage 契约类型
    ├── bootstrap.ts                     # createBootstrap：编排、回滚、信号、关停预算
    ├── health.ts                        # 内部健康端点（38080/healthz）
    ├── daemon.ts                        # createDaemon：worker 监督器
    ├── http-server.ts                   # node:http Server 适配器
    ├── grpc-server.ts                   # grpc-js Server 适配器（仅 import type）
    ├── grpc-conn.ts                     # grpc-js Client 适配器（仅 import type）
    └── *.test.ts                        # 单测（DI mock 约定）

experimental/js/                         # 迁移后终态
├── grpc_hello_world/                    # 自 experimental/ts/ 迁入 + 标识符更名 + 接入共享 bootstrap
│   └── testplan/                        # deploy/interface/selfheal 路径同步更名
├── hello_world/                         # 自 experimental/ts/ 迁入（纯迁移）
└── team_graph_spike/                    # 自 experimental/ts/ 迁入 + 接入共享 bootstrap
```

**Structure Decision**: 公共包落位 `common/js/bootstrap`，与 `common/js/{config,logs,otel,resolver}` 同层，包名 `@dominion/common-js-bootstrap`（目录、包名、BUILD 形态均沿用既有约定，参考 `common/js/logs/BUILD.bazel` 的 `ts_project + js_runtime_library + vitest_test` 三件套）。experimental 目录迁移后与既有 `experimental/js/vite_react_demo` 合并。

## Complexity Tracking

> 无 Constitution Check 违例，无需填写。
