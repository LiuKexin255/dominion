# Implementation Plan: Guitar Deploy Failure Environment Diagnostics

**Branch**: `032-guitar-deploy-failure-state` | **Date**: 2026-08-02 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/032-guitar-deploy-failure-state/spec.md`

## Summary

When `guitar run` 执行某 suite 的部署步骤不成功时，当前仅返回一句 `deploy apply <path>: <error>`，无法直观看出环境最终状态、失败原因（哪个 service 失败）或涉及哪些服务。

技术方案（详见 [research.md](./research.md)）：在 `deploy` CLI（v3）中新增 `describe` 子命令，调用既有 `GetEnvironment` API 打印单个环境的详细状态（状态、失败说明、服务列表、调和时间）；`guitar` 在部署失败分支中通过非取消上下文调用 `deploy describe <env>`，使其输出直接流入控制台，并对 describe 自身失败做降级处理。该方案契合 `guitar` 既有"shell-out 编排器"架构（已通过外部 `deploy` 二进制执行 apply/del），并填补了 deploy CLI 缺少单环境详情视图的真实缺口。

## Technical Context

**Language/Version**: Go（仓库 toolchain；`guitar` 与 `deploy` 均为 Go CLI）

**Primary Dependencies**: `github.com/spf13/pflag`（CLI 解析，两工具均用）、deploy v2 HTTP client（`tools/release/deploy/v2/client`，已提供 `GetEnvironment`/`ListEnvironments`）、deploy proto（`projects/infra/deploy`，已生成 `Environment`/`EnvironmentStatus`/`EnvironmentDesiredState`）

**Storage**: N/A（CLI 工具，无本地存储；状态数据来自 deploy service）

**Testing**: `bazel test`（Go 单测）。deploy v3 用 `httptest` 桩部署 service；guitar `run` 用可替换的 `runCommand` 包级变量桩外部命令

**Target Platform**: Linux/macOS 开发机（CLI 工具）

**Project Type**: CLI 工具（编排器 + 部署客户端 CLI）

**Performance Goals**: 失败诊断调用远小于部署超时——guitar shell-out 显式传 `--timeout=10s`（单次 HTTP GET，无轮询）

**Constraints**: 诊断调用必须使用非取消上下文（部署失败常为 `context.DeadlineExceeded`，原 ctx 已取消，与 cleanup 用 `context.WithoutCancel` 同理）；诊断失败不得掩盖原始部署错误

**Scale/Scope**: 新增 1 个 deploy 子命令（`describe.go`）+ guitar 失败分支诊断钩子 + 配套单测；不改动 deploy service、不改 deploy apply/del 行为

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

依据 `.specify/memory/constitution.md`（v1.3.0）：

- **原则 I（引用溯源）**：本 plan 及产物中的仓库内引用使用相对路径，外部引用使用完整 URL。✅ 合规。
- **原则 II（重构式变更）**：现有 deploy CLI 无任何方式打印单环境详情（`list` 仅输出 `scope.env\tstate`，无 message/服务）。新增 `describe` 是对真实缺口的功能扩展（非补丁）；guitar 失败分支从"仅返回错误"改为"打印结构化诊断"，是设计层面的恰当处理。✅ 合规。
- **原则 III（接口优先）**：先定 `deploy describe` 的 CLI 契约与 guitar 集成契约（见 [contracts/](./contracts/)），再实现。✅ 合规。
- **原则 IV（测试颗粒度）**：编译 + 单测作为开发任务的一部分，不单列 task。✅ 合规。
- **原则 V（编码前阅读文档）**：由后续 `/speckit.tasks` 在 tasks.md 中按 phase 声明文档清单。✅ 预留。
- **原则 VI（大型测试验收）**：本特性改的是 CLI 工具（guitar 编排器、deploy CLI 客户端），非服务型应用；deploy service 自身不在改动范围。故大型测试不作强制验收门禁，端到端验证见 [quickstart.md](./quickstart.md)。✅ 合规（不适用服务型应用条款）。

**结论**：无违规，Complexity Tracking 留空。

## Project Structure

### Documentation (this feature)

```text
specs/032-guitar-deploy-failure-state/
├── plan.md              # 本文件
├── research.md          # Phase 0：技术决策（describe 命令方案）与备选
├── data-model.md        # Phase 1：describe 输出模型与实体
├── quickstart.md        # Phase 1：端到端验证指南
├── contracts/
│   ├── deploy-describe.md   # deploy describe CLI 契约
│   └── guitar-integration.md # guitar 集成契约
└── tasks.md             # Phase 2（/speckit.tasks 生成，非本命令产物；其内部实现 phase（Phase 1/2/3）编号独立于本文档的规划 phase 编号）
```

### Source Code (repository root)

```text
tools/release/deploy/v3/
├── main.go            # 注册 describe 命令（commandExecTable/commandValidatorTable/commandFlagTable/usageText）
├── describe.go        # 新增：describeCommand，调用 GetEnvironment 打印详情（镜像 del.go 的 scope 解析）
├── apply.go           # 扩展 formatState：新增 WAITING_ROLLOUT→"等待滚动发布"（决策 7）
├── describe_test.go   # 新增：单测（httptest 桩 + 输出断言）
└── del_list_test.go   # 更新：TestListCommand 增补 WAITING_ROLLOUT 断言（formatState 扩展波及 list）

tools/test/guitar/pkg/run/
├── run.go             # 新增 deployDescribeCommand 常量；runSuite 部署失败分支调用诊断 helper（传 --timeout=10s）
├── reporter.go        # 新增 Reporter.DeployDiagnostics(envName)：醒目头部行 "--- 环境状态 (env=...) ---"
└── run_test.go        # 扩展：deploy failure 场景断言 apply→describe(--timeout=10s)→del 顺序、非取消 ctx、错误保留
```

**Structure Decision**: 沿用两工具既有目录布局。deploy 侧遵循 v3 现有"一命令一文件 + main.go 注册表"模式（参考 `del.go`/`list.go`）；guitar 侧改动集中在 `pkg/run`（编排逻辑）与 `reporter.go`（输出）。两工具均通过既有 `bazel` target 编译，gazelle 生成 BUILD 变更。
