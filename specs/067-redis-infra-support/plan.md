# Implementation Plan: Redis 基础设施支持与实例凭证注入改造

**Branch**: `067-redis-infra-support` | **Date**: 2026-09-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/067-redis-infra-support/spec.md`

## Summary

参考既有 mongodb infra 全链路，为 deploy 工具增加 redis（standalone）infra 支持，并为 Go/JS 公共库补齐 redis 与 JS mongo 连接库；同时将 mongo/redis 实例的凭证注入从"独立 Secret 对象 + SecretKeyRef"改为"环境变量直接值"（密码确定性派生方式不变，存量 Secret 由 prune 自动清理）。最终以 `experimental/redis_demo/`（Go + JS 双语言 gRPC 服务连同一 redis 实例，共享测试网关）经 testplan 闭环验收。

全部技术决策见 [research.md](research.md)（D1–D10）；数据结构见 [data-model.md](data-model.md)；接口契约见 [contracts/](contracts/)：

* [contracts/deploy-redis-infra.md](contracts/deploy-redis-infra.md) — deploy.yaml redis infra、实例资源清单、凭证注入契约
* [contracts/common-db-libs.md](contracts/common-db-libs.md) — Go/JS redis、JS mongo 公共库 API 契约（**JS 采用 JS 惯例 API，不照搬 Go 风格**）
* [contracts/redis-demo.md](contracts/redis-demo.md) — demo 拓扑、KvService API、testplan 结构

## Technical Context

**Language/Version**: Go（`go.mod`，新增 `github.com/redis/go-redis/v9`）+ TypeScript（原生 ESM，`style/javascript.md`；新增 node-redis v5 至 catalog）

**Primary Dependencies**: `github.com/redis/go-redis/v9`（Go redis 客户端，research.md D1）；`redis`（node-redis v5，JS redis 客户端，research.md D2）；`mongodb`（JS 驱动，catalog 既有 `^7.5.0`）；既有 `common/gopkg/solver`、`@dominion/common-js-resolver`（端点解析复用，不新造）

**Storage**: redis standalone 实例（官方 `redis:8.4` 镜像，可选 PVC 持久化）；mongodb 实例（既有 `mongo:7.0`，行为不变）

**Testing**: `bazel test`（Go 单测；JS `vitest_test` 宏）；大型测试经 testplan skill（`guitar run //experimental/redis_demo/testplan/interface_test.yaml`）完成部署→测试→清理闭环

**Target Platform**: Kubernetes（deploy service 管理的集群）+ Linux 容器（distroless）

**Project Type**: 平台工具（deploy CLI + deploy service）+ 公共库（gopkg / common-js）+ 实验 demo（grpc 服务）

**Performance Goals**: 无新增性能指标（infra 单实例、demo 规模；探针周期等沿用 mongo 实例参数）

**Constraints**: 凭证确定性派生三处实现（deploy service / gopkg / common-js）逐字节一致；JS 库 API 为 JS 惯例（用户约束）；JS mongo 库 target 语法接受 `app/name` 与 `dominion:///app/service:port` 两种形式（归一化后与 Go 语义一致，具体见 contracts/common-db-libs.md §3）；`static_config.yaml` 内嵌于 deploy service（profile 变更需发布服务）；TS/JS 依赖统一 catalog 管理

**Scale/Scope**: 1 种新 infra 资源（redis）；3 个新公共库包；1 个替换点（agent_v2 presets.ts）；1 个 demo（2 服务 + 1 网关 + testplan）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 门禁（宪章原则） | 状态 | 说明 |
|---|---|---|
| 1. 文档阅读（V） | ✅ | Plan 阶段已读：`style/javascript.md`、`style/golang.md`、`style/api.md`、`style/large_test.md`、`style/mongo.md`、`AGENTS.md`；外部来源见 research.md §外部参考。tasks 阶段按三分类清单执行 |
| 2. 实现（II 重构式 / III 接口优先） | ✅ | 接口契约先行（contracts/ 三份）；mongo 凭证注入改造与 redis 新增同步进行；`convertInfraToMongoWorkload` 泛化为按 resource 分发属架构随需求演化的重构（research.md D6 否决过早抽象，保留两个具体 workload 类型） |
| 3. 编译 + 单测（IV） | ✅ 规划内 | 每次代码变更执行 `bazel build`/`bazel test` 相关 target（不单列 task；tasks.md 各实现任务内嵌） |
| 4. 引用、图表与终态（I/VII/VIII） | ✅ | 本 plan 及 contracts/data-model 全部引用可溯源（仓库内相对路径、仓库外 URL）；图表均为 Mermaid；文档仅表述终态 |
| 5. 大型测试验收（VI） | ✅ 规划内 | demo 以 `guitar run` 完整闭环执行且全部用例通过为验收（quickstart 场景 3；contract redis-demo §5）；既有 `experimental/golang/mongo_demo` 回归由 SC-005 全仓构建/测试覆盖，本特性不重复执行其大型测试 |

Phase 1 后复查：设计产物（data-model / contracts / quickstart）与宪章无冲突；无 Complexity Tracking 违规项（见下）。

## Project Structure

### Documentation (this feature)

```text
specs/067-redis-infra-support/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── deploy-redis-infra.md
│   ├── common-db-libs.md
│   └── redis-demo.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
# deploy 工具（CLI 侧）
tools/release/deploy/
├── pkg/schema/deploy.schema.json        # infra.resource 枚举 + ["redis"]
├── pkg/schema/schema_test.go            # unknown-resource 负例改为 mysql 等
├── pkg/config/testdata/deploy.infra.yaml # 增加 redis 样例
└── README.md                             # §infra 文档更新（redis + 凭证注入说明）

# deploy service（服务端）
projects/infra/deploy/
├── runtime/k8s/
│   ├── model.go                # RedisWorkload；MongoDBWorkload 移除 SecretResourceName
│   ├── naming.go               # WorkloadKindRedis = "redis"
│   ├── builder.go              # BuildRedis{Deployment,Service,PVC}；mongo env 改直接值；删 BuildMongoDBSecret
│   ├── converter.go            # 按 resource 分发；DeployObjects 增加 RedisWorkloads
│   ├── executor.go             # redis apply/prune；移除 mongo applySecret 与 expected secrets 登记
│   ├── rollout.go              # redis Deployment 观测
│   ├── static_config.go        # RedisProfileConfig / K8sConfig.Redis
│   ├── config/static_config.yaml # redis.dev-single profile
│   └── builder_test.go / executor_test.go / converter_test.go / ...  # 对应测试更新
# （mongo Secret 相关测试与 testdata 同步移除）

# Go 公共库
common/gopkg/redis/              # 新包：client.go / credentials.go / default.go（对齐 common/gopkg/mongo 布局）

# JS 公共库（新包，均含 vitest 单测与 runtime_pkg target）
common/js/mongo/                 # @dominion/common-js-mongo：createMongoClient / resolveMongoUri
common/js/redis/                 # @dominion/common-js-redis：createRedisClient（node-redis v5）

# 存量替换
projects/game/agent_v2/
└── src/presets.ts               # 删本地派生/解析，改用 @dominion/common-js-mongo；行为保留
   （package.json 增依赖；bootstrap.ts 不变）

# demo（新目录，对齐 experimental/grpc_chain 布局）
experimental/redis_demo/
├── BUILD.bazel                  # proto_library + go_proto_library（含 gateway codegen）
├── redis_demo.proto             # KvService：Get/Set/Delete kvEntries（AIP）
├── go_service/                  # Go gRPC 服务（common/gopkg/redis）+ service.yaml
├── js_service/                  # TS grpc-js 服务（@dominion/common-js-redis）+ service.yaml
└── testplan/
    ├── BUILD.bazel              # gateway pkg/image + go_largetest testplan_test
    ├── deploy.yaml              # redis infra + 双服务 + 网关 + HTTPRoute（apitest 前缀）
    ├── gateway/                 # Go grpc-gateway 双 mux 前缀路由
    ├── interface_test.go        # 大型测试用例（自环 + 跨语言 + delete）
    └── interface_test.yaml      # 测试计划（guitar）

# 依赖管理
pnpm-workspace.yaml              # catalog 增加 redis: "^5"
go.mod / go.sum / MODULE.bazel.lock  # 增加 github.com/redis/go-redis/v9（bazel mod tidy）
```

**Structure Decision**: 全部沿用仓库既有布局约定——公共库对齐 `common/gopkg/mongo`、`common/js/resolver` 形态；demo 对齐 `experimental/grpc_chain`（proto 根置 + 双语言子目录 + testplan 含 gateway）布局；deploy 侧变更收敛在 `tools/release/deploy` 与 `projects/infra/deploy` 既有文件。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规项。复杂度说明：redis 未与 mongo 抽象统一 workload（research.md D6：差异大于共性，过早抽象违反宪章 II 简化要求）；JS 库未提供 bootstrap 组件封装（research.md D10：单消费者收益不足）。
