# Implementation Plan: Deploy Config Support

**Branch**: `045-deploy-config` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/045-deploy-config/spec.md`

## Summary

为 deploy 工具新增 **config（非敏感配置数据）** 支持：service.yaml 顶层声明配置块池（含 json/yaml 类型化数据条目），deploy.yaml artifact 选择配置块名，控制面创建 ConfigMap 并投影为容器内 `{block}/{key}` 文件，平台注入 `DOMINION_CONFIG_DIR` 发现变量；提供 Go（`common/gopkg/config`）与 JS（`common/js/config`）SDK，以各自语言惯用法读取配置并深度合并到调用方默认值之上。技术方案详见 [research.md](research.md)、[data-model.md](data-model.md)、[contracts/](contracts/)。

## Technical Context

**Language/Version**: Go 1.26.2（deploy 工具、控制面、Go SDK）；TypeScript（JS SDK、experimental TS 服务）

**Primary Dependencies**:
- Go：`gopkg.in/yaml.v3`（已有，go.mod）、Kubernetes client-go（控制面已有）
- JS：`js-yaml`（**新增**，加入 `pnpm-workspace.yaml` catalog）、`@types/js-yaml`（dev）
- SDD 框架：speckit（本仓库 `.specify/`）

**Storage**: 
- 期望状态持久化：MongoDB（`projects/infra/deploy/storage/mongo.go`，新增 `config_entries` BSON 字段）
- 运行时配置载体：Kubernetes ConfigMap（控制面创建，按 workload 命名 `{workload}-config`）

**Testing**: 
- 单测：`bazel test`（Go `go_test`、JS `vitest`）
- 大型测试：testplan skill（`tools/test/guitar`），以 `experimental/golang/grpc_hello_world` 与 `experimental/ts/grpc_hello_world` 为被测对象

**Target Platform**: Linux server（Kubernetes 集群内运行的服务）

**Project Type**: 多语言（Go CLI 工具 + Go 控制面服务 + Go SDK 库 + JS SDK 库）

**Performance Goals**: 配置读取仅启动期一次性，无热路径性能要求；ConfigMap 受 K8s 1MB 单对象限制（配置数据须在此内）

**Constraints**: 
- config 不传递敏感数据（FR-017，敏感数据用 secret）
- 与环境变量参数互不影响（FR-016）
- 向后兼容：不含 config 的现有配置行为不变（FR-020）

**Scale/Scope**: 跨 3 个 Go 包（deploy CLI、deploy 控制面、Go SDK）+ 1 个 JS 包（JS SDK）+ 2 个 experimental 示例改造；约 12 处既有文件修改（仿 secret 全链路模式）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

宪章文件：`.specify/memory/constitution.md`（v1.3.0）。逐原则检查：

| 原则 | 状态 | 说明 |
|------|------|------|
| **I. 引用溯源** | ✅ 通过 | 所有设计文档引用仓库内路径与外部 URL；contracts/data-model 引用具体代码行号与 002 spec。实现阶段须在代码注释中引用 spec/contracts 路径。 |
| **II. 重构式变更** | ✅ 通过 | config 复用既有"per-artifact 字段贯穿全链路"架构（仿 secret），不引入新架构范式；控制面新增 ConfigMap apply 路径是对现有 apply/prune 模式的自然扩展，非打补丁。无过度设计。 |
| **III. 接口优先设计** | ✅ 通过 | 接口契约先行：[yaml-schema.md](contracts/yaml-schema.md)（service/deploy YAML schema）、[proto.md](contracts/proto.md)（proto message）、[sdk-go.md](contracts/sdk-go.md)/[sdk-js.md](contracts/sdk-js.md)（SDK 公共 API）、[runtime-contract.md](contracts/runtime-contract.md)（K8s 运行时约定）均已在 Phase 1 定义。 |
| **IV. 测试颗粒度** | ✅ 通过 | 编译+单测在每次代码变更时执行（开发任务内）；大型测试（experimental testplan）作为功能验收单独分配 task。 |
| **V. 编码前阅读文档** | ✅ 通过 | tasks.md 将为每个 phase 显式声明需阅读的文档清单（三分类：代码规范/官方文档/技术文章），含间接引用文档。 |
| **VI. 服务型应用大型测试** | ✅ 通过（附说明） | deploy 控制面自身无法自举大型测试（README 可声明豁免自身）；但 config 特性通过部署 experimental 服务经真实控制面端到端验证，大型测试切实执行（`guitar run`），所有用例须通过。 |

**Gate 结论**：无违规需豁免。Complexity Tracking 无需填写。

## Project Structure

### Documentation (this feature)

```text
specs/045-deploy-config/
├── plan.md              # 本文件
├── spec.md              # 需求规格
├── research.md          # Phase 0 调研决策
├── data-model.md        # Phase 1 数据模型
├── quickstart.md        # Phase 1 验证指南
├── checklists/
│   └── requirements.md  # 规格质量检查清单
└── contracts/           # Phase 1 接口契约
    ├── yaml-schema.md
    ├── proto.md
    ├── runtime-contract.md
    ├── sdk-go.md
    └── sdk-js.md
```

### Source Code (repository root)

```text
# === Go SDK（新增） ===
common/gopkg/config/
├── config.go            # Read[T any](block, key, defaults) (T, error) + 深度合并
├── config_test.go       # 单测：合并矩阵、错误情况、defaults 不变
└── BUILD.bazel          # gazelle 生成；含 runtime_pkg target

# === JS SDK（新增） ===
common/js/config/
├── package.json         # @dominion/common-js-config，依赖 js-yaml
├── src/
│   ├── index.ts         # readConfig<T>(block, key, defaults): T + 深度合并
│   └── merge.ts         # 递归深合并 + 原型污染防护
├── test/
│   └── config.test.ts   # vitest 单测
├── tsconfig.json, .swcrc
└── BUILD.bazel          # gazelle 生成；含 runtime_pkg target

# === deploy 工具（CLI）修改 ===
tools/release/deploy/
├── pkg/config/config.go             # +Configs/ConfigBlock/ConfigEntry 结构；+DeployArtifact.Config；ParseServiceConfig 校验
├── pkg/config/v3.go                 # 无需代码改动（ParseV3ServiceConfig 委托 ParseServiceConfig，config 校验自动生效）；补版本门禁单测（R8）
├── pkg/schema/service.schema.json   # +顶层 configs 字段
├── pkg/schema/deploy.schema.json    # +artifact.configs 字段
├── pkg/schema/schema.go             # （如有）schema 校验适配
├── pkg/config/testdata/             # +含 config 的 service/deploy 测试 fixture
├── v2/compiler/compiler.go          # +config 选择校验 + 编译为 ArtifactSpec.ConfigEntries
└── README.md                        # +config 用法说明 + 保留变量 DOMINION_CONFIG_DIR

# === deploy 控制面（projects/infra/deploy）修改 ===
projects/infra/deploy/
├── deploy.proto                     # +ConfigEntry message；+ArtifactSpec.config_entries=12
├── domain/spec.go                   # +ConfigEntry 类型 + Validate；+ArtifactSpec.ConfigEntries
├── domain/environment.go            # +cloneArtifacts 深拷贝 ConfigEntries
├── handler.go                       # +to/fromProtoConfigEntries
├── storage/mongo.go                 # +mongoConfigEntry + 三处映射
├── runtime/k8s/model.go             # +DeploymentWorkload/StatefulWorkload.ConfigEntries
├── runtime/k8s/converter.go         # +两处 converter 透传 ConfigEntries
├── runtime/k8s/builder.go           # +configVolumeName/configMountPath/envConfigDir 常量；+BuildConfigMap；+BuildDeployment/BuildStatefulSet 投影段
└── runtime/k8s/executor.go          # +ConfigMap apply/prune 路径；+DOMINION_CONFIG_DIR 保留变量

# === experimental 被测对象改造 ===
experimental/golang/grpc_hello_world/
├── service/service.yaml             # +configs 配置块声明
├── service/main.go                  # +config.Read 读取问候语
├── service/BUILD.bazel              # +common/gopkg/config runtime_dep
├── testplan/deploy.yaml             # +artifact.configs 选择
└── testplan/interface_test.go       # +断言 config 覆盖生效
experimental/ts/grpc_hello_world/
├── service.yaml                     # +configs 配置块声明
├── src/server.ts                    # +readConfig 读取问候语
├── package.json                     # +@dominion/common-js-config
├── BUILD.bazel                      # +common/js/config runtime_dep
├── testplan/deploy.yaml             # +artifact.configs 选择
└── testplan/interface_test.go       # +断言 config 覆盖生效

# === 依赖管理 ===
pnpm-workspace.yaml                  # +catalog: js-yaml, @types/js-yaml
go.mod                               # （无新增，yaml.v3 已有）
```

**Structure Decision**: config 特性横跨 3 层（声明/选择/校验在 deploy CLI；期望状态/reconcile/挂载在 deploy 控制面；运行时读取在两个共享 SDK 包）。各层修改点严格遵循既有 secret 特性（002）建立的全链路模式，无新增顶层目录（除两个 SDK 包，归入既有 `common/{gopkg,js}/` 共享库惯例位置）。

## Phase 概览（供 tasks.md 规划参考）

> 具体 phase 划分与文档清单由 `/speckit.tasks` 生成。以下为建议的实施顺序与依赖，便于分步实施或中断恢复：

1. **SDK 层先行**（Go `common/gopkg/config` + JS `common/js/config`）：独立可测，不依赖 deploy 改动；单测验证合并矩阵。
2. **deploy CLI schema + 解析**：service/deploy JSON Schema + Go 结构 + ParseServiceConfig 校验（FR-003/004）+ testdata fixture。
3. **deploy CLI compiler**：config 选择校验（FR-007）+ 编译为 ConfigEntries。
4. **deploy 控制面 proto/domain/storage/handler/converter/model**：全链路字段贯通（仿 secret）。
5. **deploy 控制面 builder + executor**：ConfigMap 创建/投影/apply/prune + `DOMINION_CONFIG_DIR` 保留变量。
6. **experimental 改造 + 大型测试**：Go/TS grpc_hello_world 接入 config，testplan 端到端验收。

**验证门禁**：
- 每个 phase：`bazel build` + `bazel test`（受影响 target）通过（宪章门禁 3）。
- 最终：testplan skill 执行 experimental testplan，所有用例通过（宪章门禁 5 / 原则 VI）。
- 所有代码注释引用 spec/contracts 路径（宪章门禁 4 / 原则 I）。

## Complexity Tracking

> 无宪章违规需豁免，本表留空。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
