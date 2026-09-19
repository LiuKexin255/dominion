---
description: "Task list for feature 067-redis-infra-support"
---

# Tasks: Redis 基础设施支持与实例凭证注入改造

**Input**: Design documents from `/specs/067-redis-infra-support/`

**Prerequisites**: `specs/067-redis-infra-support/plan.md`（必需）、`specs/067-redis-infra-support/spec.md`（用户故事）、`specs/067-redis-infra-support/research.md`、`specs/067-redis-infra-support/data-model.md`、`specs/067-redis-infra-support/contracts/`、`specs/067-redis-infra-support/quickstart.md`（均已存在）

**Tests**: 单测按宪章 IV 折入各实现任务（`bazel build` + `bazel test` 为每次代码变更的一部分，不单列 task）；大型测试按宪章 VI 作为独立验收任务（T022）。

**Organization**: 按用户故事分阶段（spec.md US1–US5 优先级序）。**特殊约束：deploy service 需用户手动部署——所有代码与单测完成后，在 Phase 7 T021 停止点等待用户部署确认，方可执行任何真实部署/大型测试。**

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story the task belongs to (US1–US5)
- Include exact file paths in descriptions

---

## Phase 1: Setup（依赖引入）

**Purpose**: 新增三方依赖声明（唯一阻塞所有故事的公共前置）

**文档清单**：
- 代码规范文档：无（仅依赖声明变更）
- 官方文档：无
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D1/D2 选型与版本依据）

- [X] T001 [P] 在 `go.mod` 增加 `github.com/redis/go-redis/v9`（当前 v9 最新补丁），按 `AGENTS.md` Golang 依赖更新步骤执行（`bazel run //:go -- get` → `mod tidy -v` → 目标目录 `gazelle` → `bazel mod tidy`）
- [X] T002 [P] 在 `pnpm-workspace.yaml` catalog 增加 `redis: "^5"`（node-redis v5，见 `specs/067-redis-infra-support/research.md` D2）

**验证门禁**：`bazel build //...` 通过；`bazel mod tidy` 无 diff 残留。

---

## Phase 2: User Story 1 - deploy 配置支持 redis infra (Priority: P1) 🎯 MVP

**Goal**: deploy.yaml 可声明 `resource: redis` 的 standalone 实例；deploy service 生成 Deployment/Service/PVC（凭证以环境变量直接值注入，无 Secret）；CLI schema 放行 redis。

**Independent Test**: 单测覆盖 workload/builder/converter/executor/schema；真实部署验证在 Phase 7 门禁后进行（`specs/067-redis-infra-support/quickstart.md` 场景 2）。

**文档清单**：
- 代码规范文档：`style/golang.md`；其引用的 [Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)（规范正文）、[Style Decisions](https://google.github.io/styleguide/go/decisions) 与 [Best Practices](https://google.github.io/styleguide/go/best-practices)
- 官方文档：[redis 官方镜像（requirepass/命令行配置、探针端口约定）](https://hub.docker.com/_/redis)；[Kubernetes Pod 探针（TCP 探针语义）](https://kubernetes.io/docs/concepts/workloads/pods/probes/)
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D3/D4/D6）、`specs/067-redis-infra-support/data-model.md`（§1–§4）、`specs/067-redis-infra-support/contracts/deploy-redis-infra.md`（§1–§5）、`tools/release/deploy/README.md`（§infra / §Secret 配置）；[docker-library/redis issue #46（sh -c requirepass 官方推荐模式）](https://github.com/docker-library/redis/issues/46)

- [X] T003 [US1] 在 `projects/infra/deploy/runtime/k8s/model.go` 新增 `RedisWorkload`（字段 `ServiceName/EnvironmentName/App/ProfileName/Persistence`；`Validate()` 对齐 `MongoDBWorkload`：四项非空、`ResourceName()` ≤63 字符）；在 `naming.go` 新增 `WorkloadKindRedis = "redis"`；`model_test.go` 增加表驱动用例
- [X] T004 [P] [US1] 在 `projects/infra/deploy/runtime/k8s/static_config.go` 新增 `RedisProfileConfig{Image,Version,Port,Storage}`（无 admin_username，`specs/067-redis-infra-support/data-model.md` §2.4）与 `K8sConfig.Redis` map 及 `RedisProfile(name)` 查询；将共用存储类型 `MongoStorageConfig` 更名为 `StorageConfig`（mongo/redis 共用，同步更新 `static_config.go`/`static_config_test.go`/`builder_test.go`/`executor_test.go` 引用）；在 `runtime/k8s/config/static_config.yaml` 增加 `redis.dev-single`（`image: "redis"`, `version: "8.4"`, `port: 6379`, storage 取值同 `mongodb.dev-single`）；`static_config_test.go` 与 `runtime/k8s/testdata/static_config.redis.valid.yaml`/`static_config.redis.invalid.yaml`（缺 storage / 端口非法等负例）用例
- [X] T005 [US1] 在 `projects/infra/deploy/runtime/k8s/builder.go` 实现 `BuildRedisDeployment/BuildRedisService/BuildRedisPVC`（契约 `specs/067-redis-infra-support/contracts/deploy-redis-infra.md` §2/§3.2：命令 `["/bin/sh","-ec","exec redis-server --requirepass \"$REDIS_PASSWORD\""]`（持久化启用时追加 ` --appendonly yes`）、env 直接值 `REDIS_PASSWORD`、`SERVICE_APP`/`DOMINION_ENVIRONMENT`/`POD_NAMESPACE(fieldRef)`、Recreate 单副本、卷 `redis-data`→PVC 或 emptyDir、liveness+readiness TCP 探针端口名 `redis`）；新增 redis 密码派生常量（HMAC key `dominion-redis-stable-password`，输入序 `(app, environmentName, serviceName)`，算法同 `generateStablePassword`，`specs/067-redis-infra-support/research.md` D4）；`builder_test.go` 增加三对象用例（含密码向量断言）
- [X] T006 [US1] 在 `projects/infra/deploy/runtime/k8s/converter.go` 将 infra 转换按 `resource` 分发（`mongodb`→MongoDBWorkload、`redis`→RedisWorkload、default 报错文案不变），`DeployObjects` 增加 `RedisWorkloads`；`converter_test.go` 更新（含 redis profile 不存在报错、同环境 mongodb+redis 混合转换用例）
- [X] T007 [US1] 在 `projects/infra/deploy/runtime/k8s/executor.go` 接入 redis apply（顺序 PVC(如启用)→Deployment→Service）与 `expectedApplyResources` 登记（deployments/services，不登记 secrets），并将 `deploymentCount`/`serviceCount` 聚合（`executor.go` 现以 `MongoDBWorkloads` 计数处）纳入 `RedisWorkloads`；`rollout.go` 将 `RedisWorkloads` 纳入 Deployment 观测；`executor_test.go`/`rollout_test.go` 更新（含 prune 移除 redis 服务用例）
- [X] T008 [P] [US1] 在 `tools/release/deploy/pkg/schema/deploy.schema.json` 将 `infra.resource` 枚举扩为 `["mongodb","redis"]`；`pkg/schema/schema_test.go` 中 unknown-resource 负例由 `redis` 改为 `mysql`；`pkg/config/testdata/deploy.infra.yaml` 增加 redis infra 样例并同步 `config_test.go` 期望
- [X] T009 [P] [US1] 更新 `tools/release/deploy/README.md`：§infra 增加 redis 用例与字段说明，§Secret 配置附近补充"infra 实例凭证以环境变量注入、不部署实例 Secret"

**Checkpoint**：`bazel build //projects/infra/deploy/... //tools/release/deploy/...` 与 `bazel test //projects/infra/deploy/... //tools/release/deploy/...` 全绿。**注意：本阶段不执行任何真实 deploy apply（deploy service 尚未更新部署）。**

---

## Phase 3: User Story 2 - Go/JS redis 公共库 (Priority: P1)

**Goal**: `common/gopkg/redis` 与 `common/js/redis` 两个连接库按契约交付（target 解析→端点解析→密码派生→建连）。

**Independent Test**: 各包单测（target 表驱动、端点选择、密码向量与 Go/实例侧一致、resolver/env 注入 mock 正向断言）。

**文档清单**：
- 代码规范文档：`style/golang.md` + 其引用的 [Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions) 与 [Best Practices](https://google.github.io/styleguide/go/best-practices)；`style/javascript.md` + 其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- 官方文档：[go-redis v9（NewClient/Options/redis.Nil）](https://pkg.go.dev/github.com/redis/go-redis/v9)；[node-redis v5（createClient/connect/ESM）](https://redis.io/docs/latest/develop/clients/nodejs/)
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D1/D2/D4/D7/D10）、`specs/067-redis-infra-support/contracts/common-db-libs.md`（§1/§2/§5）、`specs/067-redis-infra-support/data-model.md`（§4/§5）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新 ESM 包级契约）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest_test shim 退出码契约）

- [X] T010 [P] [US2] 新建 `common/gopkg/redis/`（布局对齐 `common/gopkg/mongo/`：`client.go` `NewClient(rawTarget string, opts ...ClientOption) (*redis.Client, error)` 与 `WithK8sResolver()`；`credentials.go` 内嵌 `specs/067-redis-infra-support/research.md` D4 redis 派生算法；`default.go` 端口 6379；不 Ping 惰性连接）；含单测（密码向量与 T005 实例侧向量一致）；目标目录执行 gazelle
- [X] T011 [P] [US2] 新建 `common/js/redis/`（包 `@dominion/common-js-redis`，布局对齐 `common/js/resolver/`：`package.json`（`redis: "catalog:"`）+ `tsconfig.json` + `BUILD.bazel`（lib/vitest_test/runtime_pkg）+ `src/`：`createRedisClient(target, options?)` 返回已连接 node-redis 客户端、`RedisClientOptions{resolver?, env?}` 注入、错误为普通 `Error`；**API 遵循 JS 惯例，不照搬 Go 风格**，见 `specs/067-redis-infra-support/contracts/common-db-libs.md` §2）；执行 `pnpm up`（按 `AGENTS.md` TS/JS 流程）；含单测（DI mock 正向断言、密码向量与 T010 一致）

**Checkpoint**：`bazel test //common/gopkg/redis/... //common/js/redis/...` 全绿；两包密码向量输出一致。

---

## Phase 4: User Story 3 - JS mongo 公共库与 agent_v2 替换 (Priority: P2)

**Goal**: `@dominion/common-js-mongo` 按 Go 侧行为交付；agent_v2 宿主侧 mongo 连接装配改用公共库，对外行为不变。

**Independent Test**: 新包单测 + agent_v2 全量既有测试通过（`MONGO_URI` 直连优先等行为保留）。

**文档清单**：
- 代码规范文档：`style/javascript.md` + 其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/mongo.md`
- 官方文档：[mongodb node driver v7（MongoClient/connect）](https://www.mongodb.com/docs/drivers/node/current/)
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D7）、`specs/067-redis-infra-support/contracts/common-db-libs.md`（§3/§4/§5）、`specs/067-redis-infra-support/data-model.md`（§5/§7）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新 ESM 包级契约）、`specs/048-js-esm-migration/research.md`（R5：`mongodb` 具名导入核实依据）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest_test shim 退出码契约）

- [X] T012 [P] [US3] 新建 `common/js/mongo/`（包 `@dominion/common-js-mongo`，布局对齐 `common/js/resolver/`：`createMongoClient(target, options?)` 返回已连接 `MongoClient`、`resolveMongoUri(target, options?)` 仅产出凭据 URI、`MongoClientOptions{resolver?, env?}` 注入；行为对齐 `common/gopkg/mongo`：默认端口 27017、用户名 `admin`、`authSource=admin`、`DOMINION_ENVIRONMENT` 缺省回退 `default`、端点取首个；target 接受 `app/name` 与 `dominion:///app/service:port` 两种形式，统一归一化为 `app/service:port` 后交由 JS resolver，见 `specs/067-redis-infra-support/contracts/common-db-libs.md` §3；密码派生与 Go 逐字节一致（key `dominion-mongo-stable-password`）；`mongodb` 具名类型导入允许，核实依据见 `style/javascript.md` CJS 白名单与 `specs/048-js-esm-migration/research.md` R5）；执行 `pnpm up`；含单测（向量断言 + 两种 target 形式归一化）
- [X] T013 [US3] 替换 `projects/game/agent_v2/src/presets.ts`：删除本地 HMAC 派生/端点解析/URI 拼接实现，改调 `@dominion/common-js-mongo` 的 URI 产出口；保留 `MONGO_URI` 直连优先、`MONGO_TARGET`/`PRESET_DATABASE`/`PRESET_COLLECTION_NAME` 常量与 `resolveMongoUri()` 对外签名（`bootstrap.ts` 零改动）；`package.json` 增加 `@dominion/common-js-mongo: "workspace:*"` 并 `pnpm up`；更新 `presets.test.ts`（用例语义不变：凭据向量、直连优先、空端点报错；mock 改注入 resolver/env，resolver 调用参数按契约归一化为 `app/service:port`，见 `specs/067-redis-infra-support/contracts/common-db-libs.md` §3）；`common/js/dsh-plugins/preset-authoring` 不改动

**Checkpoint**：`bazel test //common/js/mongo/... //projects/game/agent_v2/...` 全绿；`grep` 确认 `projects/game/agent_v2/src/` 无本地密码派生实现残留。

---

## Phase 5: User Story 4 - mongo 实例凭证改环境变量注入 (Priority: P2)

**Goal**: mongo 实例凭证以环境变量直接值注入；不再创建实例 Secret；存量 Secret 由 prune 自动清理。

**Independent Test**: builder/executor 单测断言 env 直接值、无 Secret 生成、apply 后旧 Secret 被删除；真实环境验证在 Phase 7（`specs/067-redis-infra-support/quickstart.md` 场景 2）。

**文档清单**：
- 代码规范文档：`style/golang.md` + 其引用的 [Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions) 与 [Best Practices](https://google.github.io/styleguide/go/best-practices)
- 官方文档：[mongo 官方镜像（MONGO_INITDB_ROOT_USERNAME/PASSWORD 语义）](https://hub.docker.com/_/mongo)
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D5）、`specs/067-redis-infra-support/contracts/deploy-redis-infra.md`（§3.1/§6）、`specs/067-redis-infra-support/data-model.md`（§2.2/§4）

- [X] T014 [US4] 在 `projects/infra/deploy/runtime/k8s/builder.go` 将 `buildMongoDBContainerEnv` 改为直接值（`MONGO_INITDB_ROOT_USERNAME`=profile `admin_username`、`MONGO_INITDB_ROOT_PASSWORD`=`generateStablePassword(app, env, service)`；函数不再接收 secretName）；删除 `BuildMongoDBSecret` 与 mongo secret 常量/键名（`mongoSecretUsernameKey` 等）；`builder_test.go` 断言更新（env 直接值、无 Secret）
- [X] T015 [US4] 在 `projects/infra/deploy/runtime/k8s/model.go` 删除 `SecretResourceName()`；`executor.go` 删除 mongo `applySecret` 调用与 `expectedApplyResources` 中 mongo secrets 登记（保留 `pruneSecrets`/`deleteSecrets` 本身——存量自动清理依赖 prune）；`model_test.go`/`executor_test.go` 更新，新增"已存在旧 Secret 的环境下 apply 后该 Secret 被删除"用例

**Checkpoint**：`bazel test //projects/infra/deploy/...` 全绿；仓库内检索无 `BuildMongoDBSecret` 残留。

---

## Phase 6: User Story 5 - redis demo（代码与测试计划） (Priority: P3)

**Goal**: `experimental/redis_demo/` 双语言 gRPC 服务 + 共享网关 + testplan 就绪（仅编码与单测/构建，**不执行真实部署**——留待 Phase 7 门禁后）。

**Independent Test**: `bazel build //experimental/redis_demo/...` 通过（大型测试执行属 Phase 7）。

**文档清单**：
- 代码规范文档：`style/api.md` + 其引用的 [AIP-122 Resource names](https://google.aip.dev/122)、[AIP-131 Get](https://google.aip.dev/131)、[AIP-135 Delete](https://google.aip.dev/135)、[AIP-136 Custom Methods](https://google.aip.dev/136)；`style/golang.md` + 其引用的 [Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions) 与 [Best Practices](https://google.github.io/styleguide/go/best-practices)；`style/javascript.md` + 其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；`style/large_test.md`
- 官方文档：[grpc-gateway v2（runtime.ServeMux）](https://grpc-ecosystem.github.io/grpc-gateway/)；[go-redis v9](https://pkg.go.dev/github.com/redis/go-redis/v9)；[node-redis v5](https://redis.io/docs/latest/develop/clients/nodejs/)
- 技术文章/技术参考文档：`specs/067-redis-infra-support/research.md`（D7/D8/D10）、`specs/067-redis-infra-support/contracts/common-db-libs.md`（§1/§2：go_service/js_service 消费的公共库 API）、`specs/067-redis-infra-support/contracts/redis-demo.md`、`specs/067-redis-infra-support/data-model.md`（§5/§6）、`specs/067-redis-infra-support/quickstart.md`（场景 3）、`tools/release/deploy/README.md`（§健康探针约定：js_service 38080/`/healthz` 与打包规范）、`specs/052-deploy-health-probe/contracts/bootstrap-health.md`、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（js_service 两段式 bootstrap）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`

- [X] T016 [US5] 新建 `experimental/redis_demo/redis_demo.proto`（`package experimental.redis_demo`；`KvService`：`GetKvEntry`/`SetKvEntry`/`DeleteKvEntry`，`KvEntry{name,value}`，注解路径 `Get`=`/v1/{name=apps/*/kvEntries/*}`、`Set`=`/v1/{name=apps/*/kvEntries/*}:set`（upsert）、`Delete`=`/v1/{name=apps/*/kvEntries/*}`——契约 `specs/067-redis-infra-support/contracts/redis-demo.md` §2）与 `experimental/redis_demo/BUILD.bazel`（proto_library + go_proto_library，compilers 对齐 `experimental/grpc_chain/BUILD.bazel`，含 gateway 与 `//:go_gen_aip` codegen）
- [X] T017 [P] [US5] 新建 `experimental/redis_demo/go_service/`（`main.go`：`common/gopkg/bootstrap` + grpc 服务端 + `redis.NewClient("redis-demo/redis")`，Get/Set/Delete 直通 redis，**redis key = 资源全名**；`service.yaml`：app `redis-demo`、gRPC 50051、`tls: true`；`BUILD.bazel`：go_binary + `artifact_pkg_go`/`artifact_image`）
- [X] T018 [P] [US5] 新建 `experimental/redis_demo/js_service/`（布局对齐 `experimental/grpc_chain/mid/`：`package.json`（workspace 依赖 `@dominion/common-js-redis` 等）、`tsconfig.json`、`BUILD.bazel`（`ts_proto_library` + `ts_project` + `artifact_pkg_js`（`runtime_deps` 含 `//common/js/redis:runtime_pkg`）+ `artifact_image`）、`src/bootstrap.ts`（两段式 OTel bootstrap，见 `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`）+ `src/server.ts`（grpc-js 服务端、`createRedisClient("redis-demo/redis")`、健康端点 38080/`/healthz`，见 `tools/release/deploy/README.md` §健康探针约定）；`service.yaml` 同 go_service 形态）；执行 `pnpm up`
- [X] T019 [US5] 新建 `experimental/redis_demo/testplan/gateway/`（`main.go`：两条 `grpc.NewClient`（`solver.URI("redis-demo/go-service:grpc")`、`redis-demo/js-service:grpc`），两个 `runtime.NewServeMux` 分别注册 handler，外层 `http.ServeMux` 以 `http.StripPrefix` 按 `/experimental/redis-demo/go/`、`/experimental/redis-demo/js/` 分发（契约 `specs/067-redis-infra-support/contracts/redis-demo.md` §1）；`service.yaml`：http 80、`tls: true`；BUILD 依托 T020 的 `artifact_pkg_go/artifact_image`）
- [X] T020 [US5] 新建 `experimental/redis_demo/testplan/` 编排（`deploy.yaml`：`name: redis.{{run}}`、type test，含 redis infra（app `redis-demo`、无持久化）+ go-service + js-service + gateway（HTTPRoute `apitest.liukexin.com` PathPrefix `/experimental/redis-demo`）；`interface_test.yaml`：单 suite、`endpoint.http.public: https://apitest.liukexin.com`、cases 指向 `testplan_test`；`interface_test.go`：5 用例（go 自环、js 自环、go set→js get、js set→go get、delete→404），经 `common/gopkg/testtool`（`MustEndpoint`/`MustEnv`）读取入口，遵循 `style/large_test.md` 测试组织与反模式；`BUILD.bazel`：gateway 的 `artifact_pkg_go/artifact_image` + `go_largetest(name="testplan_test")`）

**Checkpoint**：`bazel build //experimental/redis_demo/...` 与全仓 `bazel build //...`/`bazel test //...` 通过。

---

## Phase 7: 手动部署门禁与大型测试验收 (US5 验收)

**Purpose**: ⛔ **用户约束的强制停止点**——deploy service 需手动部署，验证前必须停下等待确认。

**文档清单**：
- 代码规范文档：`style/large_test.md`；`style/golang.md`（`style/large_test.md` 规定大型测试代码遵守其单测规范）+ 其引用的 [Google Go Style — Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions) 与 [Best Practices](https://google.github.io/styleguide/go/best-practices)
- 官方文档：无
- 技术文章/技术参考文档：`specs/067-redis-infra-support/quickstart.md`（场景 2/3）、`specs/067-redis-infra-support/contracts/redis-demo.md`（§4/§5）、`specs/067-redis-infra-support/research.md`（风险清单 1）

- [X] T021 [US5] ⛔ **停止点（MANDATORY PAUSE）**：全部代码构建+单测通过后**停止执行**，向用户报告"待手动部署项"：① 使用本分支产物部署新版 deploy service（`projects/infra/deploy`，static_config 含 `redis.dev-single`、converter 支持 redis、凭证注入已改造）；② 执行 `bazel run //:deploy_install` 更新本机 deploy CLI。**在用户明确确认部署完成前，禁止执行任何 `deploy apply`/`deploy describe`/`guitar run`**。收到确认后在 tasks.md 勾选本条并继续
- [X] T022 [US5] 大型测试闭环验收：通过 testplan skill 执行 `guitar run //experimental/redis_demo/testplan/interface_test.yaml`（部署→测试→清理闭环），5 个用例全部通过；同时按 `specs/067-redis-infra-support/quickstart.md` 场景 2 抽查：`deploy describe` 显示 `[infra: redis]` 就绪、集群无实例 Secret、存量 mongo 环境重新 apply 后旧 Secret 被自动删除。任何失败/flaky 用例须修复后完整重跑直至全绿（宪章 VI；禁止以构建检查替代）

**Checkpoint**：guitar 全绿 + 场景 2 抽查通过 → spec SC-001/SC-002/SC-003 达成。

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: 全场景复核与终态检查

**文档清单**：
- 代码规范文档：无
- 官方文档：无
- 技术文章/技术参考文档：`specs/067-redis-infra-support/quickstart.md`（场景 1–5）、`specs/067-redis-infra-support/spec.md`（Success Criteria）

- [ ] T023 复核 `specs/067-redis-infra-support/quickstart.md` 场景 1–5 全部通过；对照 `specs/067-redis-infra-support/spec.md` Success Criteria（SC-001–SC-005）逐项核对；终态检查：无中间产物残留、无被否决方案痕迹、文档引用完整（`.specify/memory/constitution.md` 原则 I/VII）、图表均为 Mermaid（原则 VIII）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无依赖；T001/T002 可并行。**BLOCKS 所有后续阶段**
- **Phase 2（US1）**: 依赖 Phase 1（T001 不直接需要，但统一前置）；T003→T005→T006→T007 串行（同文件链），T004/T008/T009 可与 T003–T007 并行
- **Phase 3（US2）**: 依赖 Phase 1；与 Phase 2 完全并行（不同目录）；T010/T011 互相并行
- **Phase 4（US3）**: 依赖 Phase 1；与 Phase 2/3 并行；T012→T013 串行
- **Phase 5（US4）**: 依赖 Phase 2 完成（改同一组文件：builder/model/executor 及其测试）；T014→T015 串行
- **Phase 6（US5）**: 依赖 Phase 2（infra 语义）与 Phase 3（公共库）；T016→(T017 ∥ T018)→T019→T020
- **Phase 7（验收）**: 依赖 Phase 2–6 全部完成；T021（⛔ 手动部署停止点）→T022
- **Phase 8（Polish）**: 依赖 Phase 7

### User Story Dependencies

- **US1（P1）**: Phase 1 后即可开始——MVP
- **US2（P1）**: 仅依赖 Phase 1，与 US1 并行
- **US3（P2）**: 仅依赖 Phase 1，与 US1/US2 并行
- **US4（P2）**: 依赖 US1（同文件先后编辑，避免并发冲突；见 `AGENTS.md` 同文件串行编辑约束）
- **US5（P3）**: 依赖 US1（redis infra）+ US2（redis 公共库）；验收（Phase 7）依赖 US4（无 Secret 语义并入验收抽查）

### Parallel Opportunities

- T001 ∥ T002；T004 ∥ T003→T005→…；T008 ∥ T009 ∥（T003–T007）
- T010 ∥ T011 ∥ Phase 2/Phase 4
- T012 ∥ T013 之前的任何任务（T013 依赖 T012）
- T017 ∥ T018（T016 完成后）
- **不可并行**：T003–T007 与 T014–T015（同文件链）；同一文件编辑一律串行

---

## Parallel Example: User Story 5

```bash
# T016 完成后，两个服务实现可并行推进：
Task: "T017 [P] [US5] go_service（Go gRPC + common/gopkg/redis）"
Task: "T018 [P] [US5] js_service（grpc-js + @dominion/common-js-redis）"
# 随后串行：T019 gateway（依赖 proto 与两服务 service.yaml）→ T020 testplan
```

---

## Implementation Strategy

### MVP First (User Story 1 + 2)

1. Phase 1 依赖引入 → Phase 2 redis infra（US1）+ Phase 3 公共库（US2）并行
2. **STOP and VALIDATE**：单测全绿即 MVP 代码完成；真实部署验证因 deploy service 手动部署约束统一延后至 Phase 7
3. Phase 4/5 收敛项 → Phase 6 demo → Phase 7 ⛔ 手动部署门禁 → guitar 闭环验收

### Incremental Delivery

1. US1+US2 → 单测可验证（MVP）
2. US3（JS mongo 库化，agent_v2 回归保障）
3. US4（凭证注入改造，与 US1 同链）
4. US5（demo 代码）→ **⛔ T021 用户手动部署 deploy service** → T022 大型测试全绿（唯一真实环境验收点）
5. Phase 8 终态复核

---

## Notes

- 单测/编译按宪章 IV 属各任务内嵌步骤（每任务完成即 `bazel build`/`bazel test` 相关 target），不单列
- **⛔ T021 为强制人工门禁**：deploy service 由用户手动部署；门禁前一切真实部署类验证（deploy apply/describe、guitar run）均禁止——CLI schema 虽可先行安装，但旧服务端会拒绝 `resource: redis`
- 大型测试验收标准为全部用例通过（含跨语言一致性），失败/flaky 须修复重跑（宪章 VI）
- 提交节奏：每任务或逻辑分组提交一次；Phase 7 验收通过为整体交付条件
