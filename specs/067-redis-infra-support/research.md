# Research: Redis 基础设施支持与实例凭证注入改造

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-19

本文件记录 `/speckit.plan` Phase 0 的调研结论。所有决策均给出依据与被否决的备选方案。已与用户确认的决策标注【用户确认】。

## 代码现状调研

### deploy 工具链（mongo infra 全链路）

| 环节 | 位置 | 现状 |
|---|---|---|
| 配置 schema | `tools/release/deploy/pkg/schema/deploy.schema.json` | `infra.resource` 枚举仅 `["mongodb"]`；CLI 加载 deploy.yaml 时做 JSON Schema 校验 |
| CLI 配置解析 | `tools/release/deploy/pkg/config/config.go` | `DeployInfra` 透传 resource/profile/name/app/persistence；仅校验持久化容量（上限 1Ti） |
| proto 定义 | `projects/infra/deploy/deploy.proto` | `InfraSpec.resource` 为自由 string，无枚举约束 |
| 域模型 | `projects/infra/deploy/domain/environment.go` | 不校验 resource 取值 |
| workload 模型 | `projects/infra/deploy/runtime/k8s/model.go` | `MongoDBWorkload{ServiceName, EnvironmentName, App, ProfileName, Persistence}`；资源名 `mongo-{env}-{name}-{hash}` / `svc-{env}-{name}-{hash}` / `secret-{env}-{name}-{hash}` / `pvc-{env}-{name}-{hash}`（`naming.go` `newObjectName`，前缀常量 `WorkloadKind*`，63 字符上限） |
| 转换器 | `projects/infra/deploy/runtime/k8s/converter.go` | `convertInfraToMongoWorkload`：`case infraResourceMongoDB`，default 报错"不支持的 infra resource 类型" |
| 构建器 | `projects/infra/deploy/runtime/k8s/builder.go` | `BuildMongoDBDeployment`（Deployment、Recreate、单副本、init 容器建用户、TCP 探针）、`BuildMongoDBService`、`BuildMongoDBPVC`、`BuildMongoDBSecret`（HMAC 密码写入 Secret）、`buildMongoDBContainerEnv`（`MONGO_INITDB_ROOT_USERNAME/PASSWORD` 经 `SecretKeyRef` 引用） |
| 执行器 | `projects/infra/deploy/runtime/k8s/executor.go` | apply 顺序：PVC→Secret→Deployment→Service；`pruneResources` 按 `dominion.io/environment` + `app.kubernetes.io/managed-by`（值 `dominion.io`）标签删除不在期望集合内的资源；`Delete` 按同标签删除全部 |
| 静态配置 | `projects/infra/deploy/runtime/k8s/config/static_config.yaml` | `mongodb.dev-single` profile：`mongo:7.0`、端口 27017、admin 用户、storage（truenas-iscsi-v2 / 4Gi / ReadWriteOncePod / Filesystem）；`go:embed` 内嵌，改动需重新部署 deploy service |
| 凭证派生 | `builder.go` `generateStablePassword` | HMAC-SHA256，key `dominion-mongo-stable-password`，输入 `(app, envName, serviceName)`，`\x00` join，字母表 `[a-zA-Z0-9]`，minLen 24 |

### 公共库现状

| 位置 | 现状 |
|---|---|
| `common/gopkg/mongo` | `NewClient(target, opts...)`：解析 `app/name`（默认端口 27017）→ `solver.NewDeployResolver` 解析端点 → 派生密码 → 拼 URI → 连接；`WithK8sResolver()` 测试注入点；`credentials.go` 内嵌同一 HMAC 算法 |
| `common/js` | 有 bootstrap（含 health 组件）、config、constants、grpc（resolver/otel）、logs、otel、resolver；**无 mongo、无 redis 包** |
| `projects/game/agent_v2/src/presets.ts` | 手写 HMAC 派生（注释自述 byte-for-byte 复制 Go 算法）+ 端点解析 + URI 拼接；`MONGO_URI` 直连优先；结果经环境变量注入 preset-authoring 插件 |
| `common/js/dsh-plugins/preset-authoring/src/store.mongo.ts` | 连接由宿主注入，插件本身部署无关（本次不动，见 spec Clarifications Q2） |

### 参考实现

* **双语言 gRPC demo + testplan**：`experimental/grpc_chain/`（Go gateway → JS mid → Go backend）。proto 注解路径自带完整前缀（`/experimental/grpc-chain/echo/{message}`），HTTPRoute `PathPrefix` 匹配同前缀，网关 mux 直接服务注解路径、无需剥前缀。testplan BUILD 用 `go_largetest`。
* **Go 单服务 + mongo infra + grpc-gateway**：`experimental/golang/mongo_demo/`（`deploy.yaml` 含 `infra.resource: mongodb`）。
* **JS 服务打包**：`experimental/grpc_chain/mid/BUILD.bazel`（`ts_proto_library` 生成 TS 类型、`artifact_pkg_js` + `runtime_deps`（common/js 包的 `runtime_pkg` target）、npm catalog 依赖）。
* **JS 服务健康端点**：`common/js/bootstrap/src/health.ts`（38080/`/healthz` 组件）；grpc_chain mid 自行实现了等价逻辑。

## 技术决策

### D1 — Go redis 客户端：`github.com/redis/go-redis/v9`

- **Decision**: Go 官方客户端 go-redis v9，`redis.NewClient(&redis.Options{Addr, Password})`；未命中返回哨兵错误 `redis.Nil`。
- **Rationale**: Redis 官方维护（https://github.com/redis/go-redis ）、Go 生态事实标准、纯 Go 无 cgo（bazel/gazelle 直接可用）、支持 standalone/cluster/sentinel。仓库尚无 redis 依赖（`go.mod` 无 redis 条目），为全新引入。
- **Alternatives**: redigo（低层、需手工拼命令、无上下文语义）——否决；不引入客户端直接 TCP 实现——明显不合理，否决。

### D2 — JS redis 客户端：node-redis v5（`redis` npm 包）【用户确认】

- **Decision**: node-redis v5。`createClient({ socket: { host, port }, password })` + `await client.connect()`；promise-first；原生 ESM 双格式产物。
- **Rationale**: Redis 官方 Node 客户端（https://github.com/redis/node-redis ）；原生 ESM 意味着 `style/javascript.md` 的 CJS 导入约定（cjs-module-lexer 核实）不适用，无 interop 风险；显式 `connect()/disconnect()` 生命周期与服务 bootstrap 组件模式（启动建连、退出断连）契合。
- **Alternatives**: ioredis v5（社区流行、实例化即自动连接、Cluster/Sentinel 全）——CJS 产物（ESM 支持未正式落地），与仓库 ESM-only 方向有 interop 成本，且自动连接语义与显式生命周期管理相悖，否决。
- 依赖加入根 `pnpm-workspace.yaml` catalog（`redis: "^5"`），包内以 `catalog:` 引用（`AGENTS.md` TS/JS 依赖管理约束）。

### D3 — Redis 实例镜像与密码注入方式

- **Decision**: 官方镜像 `redis:8.4`（profile `dev-single`，端口 6379）。容器命令 `["/bin/sh", "-ec", "exec redis-server --requirepass \"$REDIS_PASSWORD\" [--appendonly yes]"]`，密码经环境变量 `REDIS_PASSWORD` 以**直接值**注入（与 mongo 凭证注入方式一致，spec FR-005/006）。
- **Rationale**:
  1. 官方镜像**不支持** `REDIS_PASSWORD`/`REDIS_ARGS` 环境变量：`REDIS_PASSWORD` 支持仍是 Open 状态的实验 PR（https://github.com/redis/docker-library-redis/pull/492 ）；`REDIS_ARGS` 是 redis-stack 镜像约定，官方 `docker.io/library/redis` 完全不读取（实证：https://github.com/redis/redis-vl-python/issues/658 ）。
  2. `sh -c 'exec redis-server --requirepass "$REDIS_PASSWORD"'` 是 docker-library 维护者给出的官方推荐做法（https://github.com/docker-library/redis/issues/46#issuecomment-335326234 ）。`exec` 保证 sh 退化为 redis-server 进程、信号（SIGTERM）正确传递。
  3. 版本选 8.4（当前 8.4.x 补丁线）：非 latest 的当前稳定线；8.x entrypoint 对 `/data` 挂载权限处理更完善（8.0.2+ 引入 `SKIP_FIX_PERMS`，见 https://hub.docker.com/_/redis ）；7.4 线已接近 OSS 维护尾声。与 mongo 选 7.0（落后一代）相比略激进，但 redis 为新增组件无迁移负担。
- **Alternatives**: 
  - bitnami/redis 镜像（原生 `REDIS_PASSWORD` env）——bitnami 免费镜像目录 2025 年起收缩迁移（https://github.com/bitnami/containers ），供应链风险，否决。
  - ConfigMap 挂 redis.conf（`requirepass` 写配置文件）——引入额外被管理资源类型，且密码进 ConfigMap 比 env 更糟，否决。
  - `REDIS_ARGS` 直传——官方镜像忽略该变量，功能失效，否决。
- **注意**: 密码经命令行展开后出现在容器进程 argv 中（集群内可见）；与"密码作为 Deployment env 直接值"的既定决策同级可见性（spec User Story 4 已接受该权衡）。

### D4 — Redis 密码派生算法

- **Decision**: 与 mongo 相同的确定性算法，仅 HMAC key 与资源种类相关：
  - 输入：`(app, environmentName, serviceName)`（与 mongo 同序），各输入 trim 后以 `\x00` join。
  - HMAC-SHA256，key = `dominion-redis-stable-password`。
  - 摘要字节逐一映射到字母表 `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`（`byte % 62`），长度不足 24 循环扩展。
- **Rationale**: spec 约束"密码生成方式不变"（算法复用）；key 按资源种类区分保证 mongo 与 redis 实例即便同名（同 app/env/service）也得到不同密码。算法在 deploy service（实例侧）与 gopkg/common-js（客户端侧）三处实现，行为必须逐字节一致（与 mongo 现状相同的同步维护方式）。
- **Alternatives**: 复用 mongo 的 HMAC key（同 key 不同输入前缀）——一旦输入规则调整即产生跨资源种类的密码碰撞面，否决；实例侧随机生成存 Secret——与"不再部署 Secret"的决策冲突，否决。

### D5 — Mongo 凭证注入改造与存量 Secret 清理

- **Decision**: 
  1. `buildMongoDBContainerEnv` 中 `MONGO_INITDB_ROOT_USERNAME`/`MONGO_INITDB_ROOT_PASSWORD` 改为**直接值**（用户名来自 profile `admin_username`，密码来自 `generateStablePassword`），删除 `SecretKeyRef` 引用。
  2. 删除 `BuildMongoDBSecret`、`applySecret` 调用及 `expectedApplyResources` 中 mongo Secret 的登记；`MongoDBWorkload.SecretResourceName()` 一并移除。
  3. 存量清理**零额外代码**：`pruneSecrets` 按 `dominion.io/environment` + `app.kubernetes.io/managed-by` 标签删除不在期望集合内的 Secret（`executor.go` prune 语义），mongo 停止登记后，旧环境下次 apply 时其实例 Secret 自动被删；环境删除（`Delete`）本就按标签删除全部 Secret。用户自建的业务 Secret（`deploy.yaml` secrets 绑定）不带 `app.kubernetes.io/managed-by: dominion.io` 标签，不受影响。
- **Rationale**: 变量名 `MONGO_INITDB_ROOT_USERNAME/PASSWORD` 是 mongo 镜像原生约定（init 容器脚本依赖），保持不变使 init 容器零改动；prune 机制天然承担存量迁移。
- **Alternatives**: 一次性清理脚本——多余，prune 已覆盖，否决；保留 Secret 兼容期——与"不再部署 Secret"目标矛盾，否决。

### D6 — RedisWorkload 资源设计（镜像 MongoDBWorkload 模式）

- **Decision**: 
  - `RedisWorkload{ServiceName, EnvironmentName, App, ProfileName, Persistence}`；命名前缀 `WorkloadKindRedis = "redis"`（Deployment `redis-{env}-{name}-{hash}`、Service `svc-{env}-{name}-{hash}`、PVC `pvc-{env}-{name}-{hash}`）。
  - Deployment：`Recreate` 策略、单副本、单容器（无 init 容器）、TCP 探针（端口名 `redis`）挂 liveness+readiness。
  - 持久化：`persistence.enabled` → PVC 挂载 `/data` + 命令追加 `--appendonly yes`（AOF 落盘，重部署数据保留）；未启用 → `emptyDir`（与 mongo 的 `buildMongoDBDataVolumeSource` 行为一致）。
  - Service：单端口 `redis`（profile.port）。
  - `static_config.yaml` 新增 `redis.dev-single`：`{image: "redis", version: "8.4", port: 6379, storage: {同 mongodb.dev-single}}`；`RedisProfileConfig`（无 `admin_username`——redis 密码认证无用户名）。共用存储类型 `MongoStorageConfig` 更名为 `StorageConfig`（mongo/redis profile 共用，避免以 mongo 命名承载 redis 配置）。
  - `convertInfraToMongoWorkload` 泛化为按 resource 分发（`mongodb` → MongoDBWorkload，`redis` → RedisWorkload），`DeployObjects` 增加 `RedisWorkloads`；executor apply/prune/rollout 观测同步纳入。
- **Rationale**: standalone 单实例与 mongo 形态完全同构，最大化复用既有校验（profile 存在性、容量、命名长度）、prune、rollout 观测逻辑；redis 无需 init 容器（`--requirepass` 即完成认证初始化，mongo 的 init 容器源于其"先起裸库再建用户"的镜像行为）。
- **Alternatives**: StatefulSet——单副本无意义且增加 governing service 复杂度，否决；抽象通用 `InfraWorkload` 接口统一 mongo/redis——两 workload 差异（init 容器、认证变量、探针语义）大于共性，过早抽象（违反宪章 II 简化原则），否决。

### D7 — 公共库 API 设计（Go 风格照旧，JS 风格遵循 JS 惯例）【用户确认：JS 不照搬 Go 风格】

- **Decision**:
  - Go `common/gopkg/redis`（对齐 `common/gopkg/mongo` 既有模式）：`NewClient(rawTarget string, opts ...ClientOption) (*redis.Client, error)`；target 为 `app/name`（默认端口 6379）；`WithK8sResolver()` 注入点；`credentials.go` 内嵌 D4 算法。不主动 Ping（与 mongo 库惰性连接行为一致，连接错误在首次命令时暴露）。
  - JS `common/js/mongo`（`@dominion/common-js-mongo`，新包）：
    - `createMongoClient(target: string, options?: MongoClientOptions): Promise<MongoClient>`——解析、派生、建连一步完成；
    - `resolveMongoUri(target: string, options?: MongoClientOptions): Promise<string>`——仅产出凭据 URI，供"宿主注入连接串"模式（agent_v2 场景）；
    - `MongoClientOptions = { resolver?: EndpointResolver, env?: Record<string, string | undefined> }`——依赖注入（测试传 test-double，遵守 `style/javascript.md` Mock 约定）；密码派生函数为模块内部实现，不导出（API 面最小）。
    - target 语法接受 `app/name`（默认端口 27017）与 `dominion:///app/service:port` 两种形式，归一化为 `app/service:port` 后交由 JS resolver（JS resolver 要求带端口；`dominion:///` 为既有 JS 调用方惯例，agent_v2 `MONGO_TARGET` 即此形式）。与 Go `common/gopkg/mongo`（仅 `app/name`、solver 内补默认端口）相比为输入语法超集，端点解析与凭证结果一致（`contracts/common-db-libs.md` §3）。
  - JS `common/js/redis`（`@dominion/common-js-redis`，新包）：
    - `createRedisClient(target: string, options?: RedisClientOptions): Promise<RedisClient>`——内部 `resolve` 端点（`app/name` → `app/name:6379`）→ 派生密码 → node-redis `createClient({ socket: { host, port }, password })` → `await connect()` 返回已连接客户端；
    - `RedisClientOptions` 同上（resolver/env 注入）；错误为普通 `Error`（含 target 与阶段上下文），不引入 Go 风格的 sentinel error 体系。
  - agent_v2 替换：`projects/game/agent_v2/src/presets.ts` 删除本地 HMAC/解析实现，改调 `@dominion/common-js-mongo` 的 URI 产出口；保留 `MONGO_URI` 直连优先与 `PRESET_DATABASE/PRESET_COLLECTION_NAME` 常量；`bootstrap.ts` 装配点不变。
- **Rationale**: 用户明确约束"js 参考 golang 代码时不要过度迁移 golang 风格"——JS 侧采用 async 工厂函数 + options 对象 + 依赖注入（与 `common/js/resolver` 的 `createResolver(config?)` 惯例一致），而非 `NewClient(target, ...opts)` 变参模式；显式返回已连接客户端符合 JS 生态对 async 资源获取的习惯。Go 侧与 `common/gopkg/mongo` 完全对齐（仓库内 Go 一致性优先）。
- **Alternatives**: JS 侧照抄 Go 的 `ClientOption` 变参与错误包装——用户明确否决；node-redis URL 形式（`redis://:pass@host:port`）传参——需对密码做 URL 编码，且 host/port/password 分字段传参更清晰，否决。

### D8 — demo：`experimental/redis_demo/`（grpc_chain 模式 + 共享网关）【用户确认】

- **Decision**:
  - 目录：`redis_demo.proto`（`package experimental.redis_demo`）+ `go_service/`（Go 纯 gRPC）+ `js_service/`（TS grpc-js 纯 gRPC）+ `testplan/`（`gateway/` + `deploy.yaml` + `interface_test.{go,yaml}` + BUILD）。
  - proto：`KvService`，资源 `KvEntry{name, value}`（`apps/{app}/kvEntries/{kv_entry_id}`）；方法 `GetKvEntry`（标准 Get）、`SetKvEntry`（custom method `:set`，upsert 语义，AIP-136）、`DeleteKvEntry`（标准 Delete）；`style/api.md` AIP 规范。**redis key = 资源全名**（如 `apps/demo/kvEntries/k1`），两语言实现一致（跨语言数据一致性的契约基础）。
  - 两个服务实现同一 proto，target 均为 `redis-demo/redis`（同一实例）；app `redis-demo`。
  - 网关（testplan 内 Go grpc-gateway 服务）：两条 grpc 连接（`solver.URI("redis-demo/go-service:grpc")`、`redis-demo/js-service:grpc`），两个 `runtime.ServeMux` 分别注册 `KvService` handler，外层 `http.ServeMux` 按 `/experimental/redis-demo/go/`、`/experimental/redis-demo/js/` 前缀挂载（`http.StripPrefix`）；proto 注解路径 `/v1/{name=apps/*/kvEntries/*}`。
  - testplan：`deploy.yaml`（`redis.{{run}}` test 环境：redis infra（无持久化，遵守 `style/large_test.md` SUT 默认不持久化）+ 2 服务 + 网关 + HTTPRoute `apitest.liukexin.com` PathPrefix `/experimental/redis-demo`）；`interface_test.yaml` 单 suite；用例含自环（go set/get、js set/get）与跨语言（go set → js get、js set → go get）及 delete。
- **Rationale**: 复用已验证的 grpc_chain 模式（gateway adapter 满足 `style/large_test.md`"纯 grpc 服务经网关转 HTTP、统一 apitest 域名 + 前缀唯一"约定）；单网关双 mux 前缀路由以最小拓扑覆盖跨语言一致性验证。
- **Alternatives**: Go 服务自带 grpc-gateway（mongo_demo 模式）+ JS 单配网关——两种暴露模式并存，拓扑不对称，否决（用户已确认）；两个独立网关 deployment——多一份镜像与部署，无额外收益，否决。

### D9 — deploy CLI schema 与测试修正

- **Decision**: `deploy.schema.json` `infra.resource` 枚举扩为 `["mongodb", "redis"]`；`schema_test.go` 中"rejects unknown resource"用例当前以 `resource: redis` 作为非法样例（`tools/release/deploy/pkg/schema/schema_test.go:135`），需改为其他非法值（如 `mysql`）；`pkg/config/testdata/deploy.infra.yaml` 增加 redis 样例；README（`tools/release/deploy/README.md` §infra）同步更新。
- **Rationale**: 枚举是 CLI 侧唯一的 resource 合法性校验点（domain/converter 之外）；redis 转正后原负例失效。
- **Alternatives**: 无。

### D10 — JS 公共库连接对 bootstrap 生命周期的态度

- **Decision**: 公共库只提供连接原语（async 工厂），不提供 bootstrap 组件封装；服务自行在启动阶段建连、退出阶段断连（demo 的 js_service 以 `createDaemon`/Bootstrap 组件或等价装配包裹）。
- **Rationale**: 现有 `common/js/bootstrap` 组件模式以显式装配组合为约定，数据库连接的启停策略（退出顺序、超时）属服务策略；库内固化会限制使用方式（宪章 II 避免过度设计）。Go 侧 `common/gopkg/mongo` 亦无 bootstrap 组件，行为对齐。
- **Alternatives**: 提供 `createRedisComponent()` bootstrap 组件——当前仅 demo 一个消费者，收益不足，否决（未来有多消费者时可再评估）。

## 外部参考

| 主题 | 来源 |
|---|---|
| go-redis v9 连接与认证 | https://github.com/redis/go-redis （`NewClient`/`Options{Addr, Username, Password}`/`redis.Nil`） |
| node-redis v5 连接与认证 | https://github.com/redis/node-redis （`createClient({url \| socket+password})`、`await connect()`、ESM） |
| 官方 redis 镜像不支持密码 env | https://github.com/redis/docker-library-redis/pull/492 （Open）、https://github.com/docker-library/redis/issues/46 （维护者推荐 `sh -c 'exec redis-server --requirepass "$REDIS_PASSWORD"'`） |
| REDIS_ARGS 为 redis-stack 约定 | https://github.com/redis/redis-vl-python/issues/658 |
| 镜像 tag 与 entrypoint 行为 | https://hub.docker.com/_/redis （8.4.x trixie 线、8.0.2+ `SKIP_FIX_PERMS`/`SKIP_DROP_PRIVS`、默认关闭 protected mode——**必须设密码**） |
| K8s 探针语义 | https://kubernetes.io/docs/concepts/workloads/pods/probes/ （TCP 探针；startup 成功前不执行 liveness——`specs/052-deploy-health-probe/research.md` 已引用） |
| 大型测试规范 | `style/large_test.md`（网关 adapter、apitest 域名前缀、SUT 不持久化、guitar 闭环） |
| JS/TS 规范 | `style/javascript.md`（ESM 约定、vitest_test 宏、DI mock 约定、catalog 依赖管理） |
| Go 规范 | `style/golang.md`（import 分组、常量优先、指针语义） |
| API 规范 | `style/api.md` + https://google.aip.dev/136 （custom method `:set`）、https://google.aip.dev/131 / https://google.aip.dev/135 （标准 Get/Delete） |

## 风险与注意点

1. **deploy service 需重新部署**：`static_config.yaml` 为 `go:embed` 内嵌（D3 profile 新增、schema 在 CLI 侧），实现完成后需发布新版 deploy service 与 deploy CLI（`bazel run //:deploy_install`）方可生效。
2. **redis 探针与密码**：官方镜像默认关闭 protected mode（容器互通友好但无认证即裸奔，见 https://hub.docker.com/_/redis ），`--requirepass` 必须始终生效——builder 中密码注入不可条件化。
3. **AOF 与 PVC**：`--appendonly yes` 仅在持久化启用时追加；AOF 文件写 `/data`（镜像 WORKDIR），与 PVC 挂载点一致。
4. **prune 时序**：mongo Secret 移除登记后，升级 deploy service 的首次 apply 即触发存量 Secret 删除——该操作幂等且不影响实例（实例进程已在运行，凭据不变）。
5. **node-redis catalog 版本**：以 `pnpm up` 解析到的 v5 最新补丁为准；demo 与公共库统一走 catalog。
6. **grpc-gateway 双 mux 前缀路由**：`http.StripPrefix` 后注解路径必须完全匹配（`/v1/...`），网关内路由错误只影响 demo 测试失败，不影响平台。
