# Golang common/gopkg 日志补全方案

## 目标

本方案用于在 `common/gopkg` 中补全公共运行库的结构化日志，目标是：

* 让服务生命周期、服务发现、客户端初始化等公共运行路径在出现问题时有兜底日志可定位。
* 让日志集中在顶层入口和关键边界，避免在底层细节中散落重复日志。
* 让 `common/gopkg` 内新增日志按初始化边界选择合适入口：普通运行库走结构化日志，`bootstrap` 与 `otel` 初始化链路走本地控制台日志。
* 让 resolver、daemon 等周期性路径只在状态变化或异常时输出高价值日志，避免稳定状态下制造噪音。

日志底座沿用 [Golang OpenTelemetry 公共库方案](./golang_observability_common_library.md) 中的 `common/gopkg/logs` 与 `common/gopkg/otel` 模型；服务生命周期模型沿用 [Golang 服务 Bootstrap 框架方案](./golang_service_bootstrap_framework.md)。

## 范围

本方案覆盖：

* `common/gopkg/logs`：补齐日志级别 API 与测试注入能力。
* `common/gopkg/bootstrap`：使用标准库本地日志补齐进程生命周期、组件启动关闭、daemon 监督日志。
* `common/gopkg/grpc/solver`：补齐 gRPC resolver 构建、刷新、状态变化、错误日志。
* `common/gopkg/solver`：补齐 deploy/k8s 服务发现外部边界日志。
* `common/gopkg/mongo`、`common/gopkg/s3`、`common/gopkg/grpc/tls`：补齐客户端初始化和安全配置边界日志。
* 单元测试和 Bazel 构建依赖更新。

本方案不覆盖：

* 业务服务请求级 access log 体系。
* 对所有历史 `log.Printf` 调用的一次性迁移。
* Debug 日志运行时开关配置。
* SigNoz Collector、OpenTelemetry pipeline 或 Kubernetes metadata enrichment 配置。

## 当前问题

当前实现中：

* `common/gopkg/logs` 已提供 `Default`、`FromContext`、`InfoContext`、`ErrorContext`、`With`，但公共运行库几乎没有使用它。
* `common/gopkg/bootstrap` 已承担组件启动、回滚、关闭、daemon 重启以及 OTel 初始化编排等顶层职责，但关键事件只通过返回错误表达，缺少运行态兜底日志。
* `common/gopkg/grpc/solver` 每 30 秒刷新服务发现状态，刷新失败只调用 `ReportError`，缺少上下文日志；稳定状态如果直接补日志又容易形成噪音。
* `common/gopkg/solver`、`mongo`、`s3`、`tls` 连接外部系统或读取运行环境，失败时错误链较完整，但缺少统一结构化字段。
* `common/gopkg/bootstrap` 包含 `common/gopkg/otel` 的初始化入口，`common/gopkg/otel` 又是日志后端初始化方；这两个包如果依赖 `common/gopkg/logs` 会形成维护和初始化顺序成本。

## 模型设计

### 日志入口模型

除 `common/gopkg/bootstrap` 与 `common/gopkg/otel` 外，`common/gopkg` 内新增运行日志统一通过 `common/gopkg/logs` 输出：

```go
logs.InfoContext(ctx, "resolver addresses changed",
	"target", target.String(),
	"address_count", len(addresses),
)
```

`common/gopkg/bootstrap` 与 `common/gopkg/otel` 是例外：如果这两个包需要记录自身初始化或生命周期过程，统一使用标准库 `log/slog` 直接输出到本地，不使用 `common/gopkg/logs`，避免 `bootstrap -> logs -> otel` 或 `otel -> logs -> otel` 的依赖和初始化混乱。

### 日志级别模型

`common/gopkg/logs` 补齐以下 API：

```go
func DebugContext(ctx context.Context, msg string, args ...any)
func WarnContext(ctx context.Context, msg string, args ...any)
```

规则：

* `Info`：正常生命周期关键事件、状态变化、初始化完成。
* `Warn`：可恢复异常、降级、周期性 resolve 失败、配置不完整但未立即中止的情况。
* `Error`：当前操作失败、组件启动/关闭失败、外部调用失败、daemon fatal。
* `Debug`：刷新细节、无状态变化的周期性信息。第一阶段先补 API 和调用点；默认 handler 仍为 `Info` 级别，因此 debug 日志默认不输出。

### 字段模型

公共字段优先使用以下命名：

| 字段 | 含义 |
| --- | --- |
| `component` | bootstrap component 名称 |
| `stage` | bootstrap stage 名称 |
| `target` | 用户输入或解析后的目标描述 |
| `app` | dominion app |
| `service` | dominion service/component |
| `namespace` | Kubernetes namespace |
| `selector` | Kubernetes label selector |
| `endpoint` | 外部服务 endpoint 或 host |
| `address_count` | resolver 输出地址数量 |
| `endpoint_count` | 原始 endpoint 数量 |
| `attempt` | daemon 或重试次数 |
| `backoff` | 重启退避时长 |
| `status` | HTTP status 或状态名称 |
| `error` | 错误对象 |

禁止记录：

* Mongo URI、密码、认证串。
* S3 access key、secret key。
* TLS private key、证书内容、CA 内容。
* 外部 HTTP response body 的完整内容。

### 测试注入模型

`common/gopkg/logs` 增加 context logger 注入能力，例如：

```go
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context
```

用途：

* 单元测试中向 context 注入 buffer logger，验证使用 `common/gopkg/logs` 的普通运行库关键事件和字段。
* 业务或普通公共库在进入某个子流程前追加字段，不依赖全局 logger。

测试不强绑定完整日志文案，只断言关键事件或结构化字段，避免文案调整导致测试脆弱。

## 代码分层

### `common/gopkg/logs`

职责：

* 提供统一日志门面。
* 隐藏 deploy 与非 deploy handler 差异。
* 提供 `DebugContext`、`WarnContext`、`WithLogger`。

不负责：

* 决定业务日志语义。
* 配置 debug 日志开关。该能力留到未来规划。

### `common/gopkg/bootstrap`

职责：

* 在进程生命周期顶层输出本地控制台兜底日志。
* 记录组件 start/stop、rollback、shutdown、server exit、daemon restart/fatal。
* 避免依赖 `common/gopkg/logs`，保持 OTel 初始化前后的日志路径清晰。

建议日志点：

* `RunSignal` 开始：组件数量、信号列表。
* 每个组件 `Start` 前后：`component`、`stage`。
* `Start` 失败：`component`、`stage`、`error`，随后记录 rollback。
* `shutdown` 开始和结束：组件数量、timeout。
* 每个组件 `Stop` 失败或 panic：`component`、`error`。
* `monitorExitWatchers` 收到异常退出：`component`、`error`。
* daemon build/start 失败、restart、backoff、restart exhausted、fatal。

### `common/gopkg/grpc/solver`

职责：

* 记录 resolver 生命周期和服务发现状态变化。

建议日志点：

* resolver build 成功：scheme、target、refresh interval。
* 初次 resolve 失败：`Error`。
* 周期 refresh 失败：`Warn` 或 `Error`，带 `target`、`error`。
* 地址集变化：`Info`，带 `address_count`。
* 地址集无变化：最多 `Debug`，默认不输出。
* resolver close：`Info` 或 `Debug`。

### `common/gopkg/solver`

职责：

* 记录 deploy service 与 Kubernetes API 访问边界。

建议日志点：

* deploy HTTP request 创建和响应异常。
* deploy 返回非 200/404 状态。
* k8s service list / EndpointSlice list 失败。
* resolve 到 0 个 ready endpoint。
* stateful instance 未找到或无 ready endpoint。

### `common/gopkg/mongo`、`s3`、`grpc/tls`

职责：

* 记录客户端初始化和安全配置边界。

建议日志点：

* Mongo：target 解析成功、resolver 类型、选中 address、创建 client 失败。
* S3：endpoint、region、client 创建失败。
* TLS：是否启用、使用的 CA/cert/key 文件路径、credentials 构建失败。

## 关键细节

### 避免重复打印错误

公共库中的日志应当靠近逻辑入口或外部边界，避免同一个错误在底层和顶层重复打印。

规则：

* `bootstrap` 作为生命周期顶层，需要使用本地控制台日志记录组件失败和 shutdown 失败。
* resolver 内部可以记录周期性失败，因为 gRPC 调用方未必能直接看到完整 resolve 背景。
* 纯转换函数、解析函数、filter 函数通常只返回 error，不打印日志。

### 控制周期性日志噪音

`grpc/solver` 的刷新循环默认 30 秒执行一次：

* 状态无变化：不输出 `Info`。
* 状态变化：输出 `Info`。
* 失败：输出 `Warn` 或 `Error`。
* 调试细节：使用 `Debug`，默认不输出。

### Bootstrap 与 OTel 初始化包的例外处理

`common/gopkg/bootstrap` 与 `common/gopkg/otel` 不使用 `common/gopkg/logs`。

如果需要输出生命周期、初始化模式或 exporter 创建失败日志：

* 统一使用标准库 `log/slog`。
* 只输出本地日志，不依赖 OTel logs pipeline。
* 不把这两个包纳入 `logs` facade 的依赖链。

### BUILD.bazel 更新

新增跨包日志依赖后，应通过 gazelle 更新 BUILD 文件：

```bash
bazel run //:gazelle common/gopkg
```

如果 Go module 或 Bazel module 依赖发生变化，再执行：

```bash
bazel run @rules_go//go -- mod tidy -v
bazel mod tidy
```

本方案预期不引入新的外部依赖。

## 实施计划

### Phase 1：补齐 logs API

修改：

* `common/gopkg/logs/logs.go`
* `common/gopkg/logs/logs_test.go`

内容：

* 增加 `DebugContext`。
* 增加 `WarnContext`。
* 增加 `WithLogger`。
* 保持本地默认 handler 为 `Info` 级别。

验收：

```bash
bazel test //common/gopkg/logs:logs_test
```

### Phase 2：补齐 bootstrap 生命周期日志

修改：

* `common/gopkg/bootstrap/bootstrap.go`
* `common/gopkg/bootstrap/daemon.go`
* `common/gopkg/bootstrap/http.go`
* `common/gopkg/bootstrap/grpc.go`
* `common/gopkg/bootstrap/otel.go`

验收：

```bash
bazel test //common/gopkg/bootstrap:bootstrap_test
```

### Phase 3：补齐 resolver 与服务发现日志

修改：

* `common/gopkg/grpc/solver/resolver.go`
* `common/gopkg/solver/deploy_http_client.go`
* `common/gopkg/solver/deploy_resolver.go`
* `common/gopkg/solver/deploy_stateful_resolver.go`
* `common/gopkg/solver/k8s.go`
* 相关 `BUILD.bazel`

验收：

```bash
bazel test //common/gopkg/grpc/solver:solver_test //common/gopkg/solver:solver_test
```

### Phase 4：补齐客户端初始化日志

修改：

* `common/gopkg/mongo/client.go`
* `common/gopkg/s3/s3.go`
* `common/gopkg/grpc/tls/tls.go`
* 相关 `BUILD.bazel`

验收：

```bash
bazel test //common/gopkg/mongo:mongo_test //common/gopkg/s3:s3_test //common/gopkg/grpc/tls:tls_test
```

### 总体验收

```bash
bazel test //common/gopkg/...
bazel build //common/gopkg/...
```

## 决策详情

### 决策 1：普通运行库统一使用 `common/gopkg/logs`

决策：接受。

原因：

* `solver`、`grpc/solver`、`mongo`、`s3`、`grpc/tls` 等普通运行库统一 deploy 和非 deploy 行为。
* 统一结构化字段和 context logger 模型。
* 避免普通运行库直接选择不同日志后端。

例外：

* `common/gopkg/bootstrap` 使用标准库 `log/slog` 输出到控制台。
* `common/gopkg/otel` 使用标准库 `log/slog` 输出到控制台。

### 决策 2：先补 `DebugContext`，但默认不输出 debug 日志

决策：接受。

原因：

* 允许 resolver 等周期性路径先写入 debug 级别细节。
* 默认 handler 仍为 `Info`，不会增加当前日志噪音。
* Debug 开关作为未来能力单独设计。

### 决策 3：`common/gopkg/bootstrap` 与 `common/gopkg/otel` 不使用 `common/gopkg/logs`

决策：接受。

原因：

* `bootstrap` 包含 `otel` 初始化，属于日志后端建立前后的生命周期编排层。
* `otel` 是 `logs` deploy handler 的依赖方。
* 反向依赖会增加初始化顺序和维护成本，并造成启动阶段日志路径混乱。
* 初始化和生命周期阶段日志统一使用标准库 `log/slog` 本地输出即可。

### 决策 4：不做默认请求级 access log

决策：接受。

原因：

* HTTP/gRPC 请求级观测已有 OTel tracing 和 metrics 承担。
* 公共库默认每请求打 `Info` 容易形成高基数、高噪音日志。
* 业务 handler 可按业务语义自行记录关键事件。

### 决策 5：resolver 稳定状态不输出 Info

决策：接受。

原因：

* resolver 默认周期刷新，稳定状态反复输出没有定位价值。
* 地址变化和错误才是运行判断需要的高价值事件。

### 决策 6：敏感信息禁止入日志

决策：接受。

原因：

* Mongo、S3、TLS 均涉及凭据或密钥材料。
* 日志只记录定位需要的非敏感元数据。

### 决策 7：增加 `WithLogger` 支持测试注入

决策：接受。

原因：

* 便于单元测试捕获日志输出。
* 避免测试依赖全局 logger 或进程 stdout。
* 也支持调用方在 context 中追加已有 logger。

### 决策 8：测试不强绑定完整日志文案

决策：接受。

原因：

* 日志文案属于可维护文本，不应成为大量脆弱测试的稳定契约。
* 关键事件和结构化字段才是需要稳定验证的行为。

## 未来规划

后续可单独设计：

* Debug 日志运行时开关，例如环境变量或配置项。
* request access log 中间件，明确采样、高基数字段、脱敏策略后再落地。
* 业务服务从标准库 `log` 到 `common/gopkg/logs` 的迁移计划。
* 基于 trace id 的日志查询和大型测试失败诊断流程。
