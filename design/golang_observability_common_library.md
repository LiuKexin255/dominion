# Golang OpenTelemetry 公共库方案

## 目标

本方案用于在 `common/gopkg` 与 `common/gopkg/grpc` 中建设 Golang 服务统一可观测公共库，目标是：

* 让仓库内 gRPC 服务通过统一入口接入 OpenTelemetry logs、traces、metrics。
* 让部署在 deploy 环境中的服务通过 OTLP/gRPC 上报到自建 SigNoz Collector。
* 让非 deploy 环境不远程上报 trace 和 metric，同时日志 fallback 到控制台输出，并保持 trace id 可用。
* 让日志、链路和指标都能按 `app`、`service`、`environment`、`container` 等维度定位问题。
* 让测试失败时可以直接获取 trace id，用于在 SigNoz 中查询对应链路和日志。
* 在 `experimental/golang/grpc_hello_world/` 中完成端到端实践，验证 gRPC server/client 默认接入效果。

## 范围

本方案覆盖：

* `common/gopkg/otel`：OpenTelemetry provider、exporter、resource、trace id 工具。
* `common/gopkg/logs`：业务日志门面，deploy 环境后端为 OTel Logs，非 deploy 环境后端为控制台。
* `common/gopkg/grpc`：在 `ServiceDefault()` 与 `ClientDefault()` 中接入 gRPC OTel instrumentation。
* `experimental/golang/grpc_hello_world/`：公共库实践与验证目标。
* 单元测试和 Bazel 构建依赖更新。

本方案不覆盖：

* SigNoz Collector 的部署实现。
* Kubernetes RBAC、Collector pipeline、processor 的代码实现。
* 业务服务自定义指标体系设计。
* 对所有历史 `log.Printf` 调用的一次性迁移。

SigNoz Collector 与 Kubernetes metadata enrichment 是本方案的前置依赖，详见 [SigNoz Collector 前置依赖](./signoz_collector_prerequisites.md)。

## 当前问题

当前仓库实现中：

* `common/gopkg/otel/` 目录已存在但为空，没有公共 OpenTelemetry 初始化能力。
* `common/gopkg/grpc/default.go` 只聚合 resolver、keepalive、TLS 配置，没有接入 OTel gRPC instrumentation。
* 服务代码主要使用 Go 标准库 `log`，没有统一结构化日志门面。
* `experimental/golang/grpc_hello_world` 已使用 `common/gopkg/grpc.ServiceDefault()` 与 `ClientDefault()`，适合作为端到端实践目标。
* deploy runtime 已为 workload 注入 `SERVICE_APP`、`DOMINION_ENVIRONMENT`、`POD_NAMESPACE`，并设置 `app.kubernetes.io/name`、`app.kubernetes.io/component`、`dominion.io/environment` 等 Pod label。

## 最终模型

## 运行环境模型

公共库按进程本地环境判断是否处于 deploy 环境。

deploy 环境判定条件为以下变量均存在且非空：

* `SERVICE_APP`
* `DOMINION_ENVIRONMENT`
* `POD_NAMESPACE`

任一变量缺失时视为非 deploy 环境。

### deploy 环境行为

deploy 环境中：

* traces 通过 OTLP/gRPC exporter 上报。
* metrics 通过 OTLP/gRPC exporter 上报。
* logs 通过 OTel Logs SDK 与 OTLP/gRPC exporter 上报。
* endpoint 默认使用 `dominion-opentelemetry-collector.kube-public.svc.cluster.local:4317`。
* OTLP/gRPC 使用 insecure 连接，匹配自建集群内 Collector 默认部署方式。
* shutdown 时统一 flush trace、metric、log pipeline。

### 非 deploy 环境行为

非 deploy 环境中：

* traces 不远程上报。
* metrics 不远程上报。
* logs fallback 到控制台输出。
* 不创建任何 OTLP exporter，不解析也不访问集群内 Collector 域名。
* trace provider 仍创建真实 trace id，保证本地测试和日志关联可用。
* `logs` API 与 deploy 环境一致，业务代码不感知后端差异。

## 资源维度模型

公共库和 Collector 最终需要保证以下维度可用于 logs、traces、metrics 查询：

| 维度 | 来源 | 最终属性 |
| --- | --- | --- |
| app | Pod label `app.kubernetes.io/name` | `app` |
| service | Pod label `app.kubernetes.io/component` | `service` |
| service unique id | `app` 与 `service` 组合 | `service.name = app/service` |
| environment | Pod label `dominion.io/environment` | `environment` |
| container | Kubernetes container metadata | `container` |
| namespace | Kubernetes namespace metadata | `k8s.namespace.name` |
| pod | Kubernetes pod metadata | `k8s.pod.name` |

`service.name` 使用 `app/service` 格式，作为 SigNoz 与 OTel 生态中的服务唯一标识。例如：

```text
service.name = game/session
app = game
service = session
environment = alice.dev
container = dp-alice-dev-session-1a2b3c4d
```

`app`、`service`、`environment`、`container` 使用简洁字段名，不额外使用 `app.name`、`service.name` 形式表达业务维度；`service.name` 仅保留为 OTel/SigNoz 标准服务标识。

## 日志模型

业务代码不直接使用 `go.opentelemetry.io/otel/log` 作为日常日志 API。

新增 `common/gopkg/logs`，对业务暴露 `slog` 风格接口：

```go
logs.InfoContext(ctx, "create room", "room", roomID)
logs.ErrorContext(ctx, "create room failed", "error", err)
logger := logs.FromContext(ctx)
```

`logs` 包职责：

* 提供统一日志入口，避免业务代码直接依赖 OTel Logs 底层 record 模型。
* 从 `context.Context` 中关联当前 span context。
* deploy 环境通过 OTel Logs bridge / handler 写入 OTel log pipeline。
* 非 deploy 环境通过控制台 handler 输出。
* 保持字段命名、错误字段、trace id 关联规则一致。

OTel Logs 是日志上报后端，不是业务层日志 API。

## Trace 模型

`common/gopkg/otel` 提供统一 tracer 与 trace id 工具：

```go
shutdown, err := otel.Init(ctx)
defer shutdown(context.Background())

ctx, span := otel.Tracer().Start(ctx, "operation")
defer span.End()

traceID := otel.TraceID(ctx)
```

规则如下：

* deploy 环境创建 OTLP trace exporter。
* 非 deploy 环境不创建 OTLP trace exporter，但仍创建 sampled span context，确保 `TraceID(ctx)` 非空。
* gRPC 自动 instrumentation 通过 context 传播 trace parent。
* 测试失败时使用 `t.Logf("trace_id=%s", otel.TraceID(ctx))` 输出查询入口。

## Metric 模型

`common/gopkg/otel` 提供统一 meter：

```go
meter := otel.Meter()
```

第一阶段只提供基础能力：

* deploy 环境初始化 OTLP metric exporter 和 periodic reader。
* 非 deploy 环境使用 noop meter provider。
* `common/gopkg/grpc` 接入 `otelgrpc` 后自动产生 gRPC client/server 指标。
* 业务自定义 metrics 后续按需通过 `otel.Meter()` 增加。

## 代码分层

## `common/gopkg/otel`

职责：

* 判断 deploy / non-deploy 环境。
* 创建 resource、trace provider、meter provider、log provider。
* 创建 OTLP/gRPC exporters。
* 设置全局 propagator。
* 暴露 `Tracer()`、`Meter()`、`TraceID(ctx)`。
* 返回统一 `Shutdown` 用于 flush。

建议 API：

```go
type Shutdown func(context.Context) error

func Init(ctx context.Context, opts ...Option) (Shutdown, error)
func Tracer() trace.Tracer
func Meter() metric.Meter
func TraceID(ctx context.Context) string
```

配置项：

```go
func WithCollectorEndpoint(endpoint string) Option
func WithServiceVersion(version string) Option
```

默认 Collector endpoint：

```text
dominion-opentelemetry-collector.kube-public.svc.cluster.local:4317
```

允许通过 option 或环境变量覆盖，方便测试、迁移和临时验证。该 endpoint 只在 deploy 环境创建 OTLP exporters 时使用；非 deploy 环境不得解析或访问该域名。

## `common/gopkg/logs`

职责：

* 提供业务日志门面。
* deploy 环境接入 OTel Logs。
* 非 deploy 环境 fallback 到控制台。
* 统一 `error` 字段、上下文 trace id 关联、默认 logger。

建议 API：

```go
func Default() *slog.Logger
func FromContext(ctx context.Context) *slog.Logger
func InfoContext(ctx context.Context, msg string, args ...any)
func ErrorContext(ctx context.Context, msg string, args ...any)
func With(ctx context.Context, args ...any) context.Context
```

## `common/gopkg/grpc`

职责：

* 保持 gRPC 默认装配入口。
* 在 `ServiceDefault()` 中追加 `grpc.StatsHandler(otelgrpc.NewServerHandler())`。
* 在 `ClientDefault()` 中追加 `grpc.WithStatsHandler(otelgrpc.NewClientHandler())`。
* 不在业务代码中重复装配 gRPC OTel handler。

## `experimental/golang/grpc_hello_world`

职责：

* 在 service 进程初始化 `common/gopkg/otel` 与 `common/gopkg/logs`。
* 在 gateway 进程初始化 `common/gopkg/otel` 与 `common/gopkg/logs`。
* 通过现有 `pgrpc.ServiceDefault()` 与 `pgrpc.ClientDefault()` 验证 gRPC trace propagation。
* 在 handler 中记录一条结构化日志，包含请求参数与 trace id。
* 在 testplan 或手动验证中确认 gateway -> service 链路可查询。

## 关键细节

## 为什么不新增 `SERVICE_NAME`

service 已由 deploy runtime 以 Pod label `app.kubernetes.io/component` 表达。

本方案不新增 `SERVICE_NAME`，避免同一身份在 env 与 label 中重复维护。Collector 通过 `k8sattributes` 提取该 label 后，统一生成：

```text
service = <app.kubernetes.io/component>
service.name = <app.kubernetes.io/name>/<app.kubernetes.io/component>
```

## 为什么不新增 `CONTAINER_NAME`

container name 是 Kubernetes 原生 metadata。Collector 通过 `k8sattributes` 可为 telemetry 补齐 `k8s.container.name`。

本方案将该值映射为简洁业务维度：

```text
container = <k8s.container.name>
```

因此不需要在 deploy runtime 中额外注入 `CONTAINER_NAME`。

## 为什么需要 `common/gopkg/logs`

OTel Logs 更适合作为 telemetry 后端，不适合作为业务日志 API 直接暴露。

引入 `common/gopkg/logs` 有以下好处：

* 业务代码保持稳定，即使 OTel Logs bridge 或 SDK API 后续变化，也只影响公共库内部。
* deploy 与 non-deploy 后端可以无缝切换。
* 日志字段、trace id 关联、错误字段命名可以统一治理。
* 未来如需适配 `zap`、`logr` 或第三方框架，只需在公共库内增加 adapter。

## 为什么非 deploy 仍创建 trace id

本地测试失败时，开发者仍需要从日志中拿到 trace id 进行定位。即使非 deploy 环境不远程上报 trace，进程内也要创建真实 span context，使日志能与同一请求链路关联。

非 deploy 环境的 trace id 不保证可在 SigNoz 查询到远程 trace 数据，但可用于关联本地控制台日志；当请求进入 deploy 服务后，deploy 侧会独立上报自己的链路与日志。

## gRPC A -> B 场景

每个进程独立判断是否处于 deploy 环境：

* A 和 B 都在 deploy：A、B 都上报。
* A 不在 deploy，B 在 deploy：A 不上报，B 上报。
* A 在 deploy，B 不在 deploy：A 上报，B 不上报。

trace context 仍通过 gRPC metadata 传播，是否 export 由每个进程自己的 provider 决定。

## 验收标准

单元测试：

* 非 deploy 环境 `otel.TraceID(ctx)` 非空。
* 非 deploy 环境不会创建 OTLP trace / metric exporters。
* 非 deploy 环境日志写入控制台后端。
* deploy 环境会创建 trace / metric / log OTLP/gRPC exporters。
* `logs.InfoContext(ctx, ...)` 可关联当前 span context。
* `ServiceDefault()` 和 `ClientDefault()` 包含 `otelgrpc` stats handler。

Bazel 验证：

* `bazel test //common/gopkg/otel/...`
* `bazel test //common/gopkg/logs/...`
* `bazel test //common/gopkg/grpc/...`
* `bazel test //experimental/golang/grpc_hello_world/...`

实践验证：

* 部署 `experimental/golang/grpc_hello_world`。
* 请求 gateway 后，在 SigNoz 中看到 `gateway -> service` 链路。
* logs 与 traces 可通过同一个 trace id 关联。
* logs、traces、metrics 均包含 `app`、`service`、`environment`、`container` 维度。
* `service.name` 为 `app/service` 格式。

## 决策详情

* 使用 OTLP/gRPC 上报到 `dominion-opentelemetry-collector.kube-public.svc.cluster.local:4317`。
* deploy 环境才上报；非 deploy 环境不远程上报 trace / metric。
* 非 deploy 环境日志 fallback 到控制台输出。
* 非 deploy 环境不创建 OTLP exporters，因此不会访问 `dominion-opentelemetry-collector.kube-public.svc.cluster.local`。
* 日志收集主链路使用 OTel Logs。
* 业务日志入口使用 `common/gopkg/logs`，不直接暴露 OTel Logs API 给业务代码。
* 不新增 `SERVICE_NAME`，service 从 `app.kubernetes.io/component` 提取。
* 不新增 `CONTAINER_NAME`，container 从 Kubernetes metadata 提取。
* `service.name` 使用 `app/service` 格式。
* `app`、`service`、`environment`、`container` 使用简洁字段名。
* 实践范围为 `experimental/golang/grpc_hello_world/`。
* Collector 前置依赖单独成文，不纳入代码实现。

## 未来规划

* 增加业务 metrics 命名规范。
* 增加采样率配置，例如 ratio sampler。
* 增加更多日志框架 adapter。
* 将更多已有服务从标准库 `log` 迁移到 `common/gopkg/logs`。
