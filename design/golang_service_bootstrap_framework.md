# Golang 服务 Bootstrap 框架方案

## 目标

本方案用于在 `common/gopkg/bootstrap` 中建设 Golang 服务进程的标准生命周期管理框架，目标是：

* 让服务进程通过统一入口启动、等待退出信号、执行优雅关闭。
* 让长期运行组件按照稳定顺序启动和关闭，避免生命周期顺序依赖业务代码注册顺序。
* 让 OpenTelemetry、外部 client、HTTP/gRPC 服务等 runtime 组件获得可靠关闭保证。
* 先在 `experimental/golang/grpc_hello_world/` 中完成实践，验证无问题后再迁移业务服务。

本框架只管理服务内各类 long-time runtime 的生命周期，不负责业务初始化、依赖构造或配置解析。

## 范围

本方案覆盖：

* `common/gopkg/bootstrap` 生命周期编排核心。
* OpenTelemetry、gRPC client connection、gRPC server、HTTP server 的 adapter。
* 默认退出信号处理与统一 shutdown timeout。
* `experimental/golang/grpc_hello_world/service` 与 `experimental/golang/grpc_hello_world/gateway` 的第一阶段实践。

本方案不覆盖：

* repository、handler、token signer、业务 service、runtime client 等业务对象的初始化。
* deploy worker / daemon 的重启和恢复策略。
* 一次性迁移 `projects/infra/deploy/`、`projects/game/session/`、`projects/game/gateway/`。
* OpenTelemetry provider 自身实现细节；该部分沿用 `common/gopkg/otel`，详见 `design/golang_observability_common_library.md`。

daemon 管理在第一阶段暂不实现，待迁移 `projects/infra/deploy` worker 时再单独设计。

## 当前问题

当前服务启动逻辑存在以下重复和风险：

* `experimental/golang/grpc_hello_world/service/main.go` 手动初始化 OTel、创建 listener、启动 gRPC server、监听 signal、执行 `GracefulStop` 和 OTel shutdown。
* `experimental/golang/grpc_hello_world/gateway/main.go` 手动初始化 OTel、创建 gRPC client、启动 HTTP server、监听 signal、执行 HTTP shutdown、关闭 gRPC client、执行 OTel shutdown。
* `projects/infra/deploy/app/server.go`、`projects/game/session/app/server.go`、`projects/game/gateway/app/server.go` 重复实现了类似 HTTP/gRPC-gateway shutdown 逻辑。
* OTel、client、server 的关闭顺序依赖手写代码顺序，后续服务增加 runtime 组件时容易遗漏或顺序错误。
* 退出 timeout 没有统一模型，部分代码直接使用 `context.Background()` 关闭，可能导致退出阻塞。

## 模型设计

### Component

`Component` 是 bootstrap 管理的最小生命周期单元：

```go
type Component interface {
	Name() string
	Stage() Stage
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

规则：

* `Name()` 必须在同一个 bootstrap 内唯一。
* `Stage()` 决定组件跨类别的启动和关闭顺序。
* `Start(ctx)` 只负责启动该 runtime 组件。
* `Stop(ctx)` 只负责停止该 runtime 组件。
* 组件之间存在明确顺序依赖时，应通过不同 `Stage` 表达，不依赖同 stage 的 name 排序。

### Stage

第一阶段只定义三个 stage：

```go
type Stage int

const (
	StageFoundation Stage = 100
	StageClient     Stage = 200
	StageServer     Stage = 300
)
```

含义：

* `StageFoundation`：进程级基础设施，例如 OTel。
* `StageClient`：外部 client 或连接，例如 gRPC client connection，后续包括 Mongo client。
* `StageServer`：入口服务，例如 gRPC server、HTTP server、grpc-gateway HTTP server。

启动顺序为：

```text
Foundation -> Client -> Server
```

关闭顺序为：

```text
Server -> Client -> Foundation
```

同 stage 内按 `Name()` 字典序排序。该排序只用于保证确定性，不承载业务依赖语义。

### Bootstrap

`Bootstrap` 管理一组 components：

```go
type Bootstrap struct {
	// internal fields
}

func New(opts ...Option) *Bootstrap
func (b *Bootstrap) Register(component Component) error
func (b *Bootstrap) Run(ctx context.Context) error
func (b *Bootstrap) RunSignal(ctx context.Context, signals ...os.Signal) error
```

`Run(ctx)` 使用默认退出信号：

```go
os.Interrupt, syscall.SIGTERM
```

`RunSignal(ctx, signals...)` 用于自定义退出信号。调用方仍然传入 `context.Context`，便于测试或由上层系统控制退出。

### 退出模型

退出可由以下原因触发：

* 收到默认或自定义 signal。
* 传入的 `ctx` 被取消。
* 某个 server component 自行退出。
* 某个 component 启动失败，触发 rollback。

任意原因触发退出后，bootstrap 创建统一 shutdown deadline。所有 component 的 `Stop(ctx)` 都共享该 deadline。

默认 timeout：

```go
const defaultShutdownTimeout = 5 * time.Second
```

通过 option 覆盖：

```go
func WithShutdownTimeout(timeout time.Duration) Option
```

### 错误语义

启动阶段：

* bootstrap 按 `Stage asc, Name asc` 启动 components。
* 任一 component 启动失败，停止所有已成功启动的 components。
* rollback 使用同一个 shutdown timeout。
* 返回启动错误与 rollback 错误的组合错误。

运行阶段：

* 服务仍在运行时不算错误。
* server 的 `Serve` / `ListenAndServe` 返回表示服务已退出。
* 如果退出发生在 bootstrap 已开始 shutdown 之后，则视为正常退出。
* 如果 server 自行退出且不是预期关闭，则触发整体 shutdown，并将该退出错误作为 bootstrap 返回错误的一部分。

关闭阶段：

* bootstrap 只停止已成功启动的 components。
* 停止顺序为启动顺序反向。
* 单个 component 停止超时或失败，不阻止后续 components 停止。
* 多个停止错误使用 `errors.Join` 汇总。

### OTel 关闭保证

OTel adapter 固定为 `StageFoundation`。因此：

* 启动时 OTel 最先初始化。
* 关闭时 OTel 最后停止。

这样可以保证触发退出的日志、关闭过程中的日志和 trace/metric/log pipeline 都先写入，再由 OTel shutdown flush 后退出。

## 代码分层

### `common/gopkg/bootstrap`

职责：

* 管理 component 注册、唯一 name 校验、排序。
* 提供 `Run(ctx)` 和 `RunSignal(ctx, signals...)`。
* 管理统一 shutdown timeout。
* 管理启动失败 rollback、运行期退出、反向关闭和错误聚合。
* 提供常用 adapter。

不负责：

* 业务对象构造。
* 业务配置解析。
* 业务服务注册到 gRPC server 或 HTTP mux。
* OpenTelemetry provider 内部实现。

### Adapter

#### OTel

```go
func OTel(opts ...otel.Option) Component
```

行为：

* `Start(ctx)` 调用 `otel.Init(ctx, opts...)`。
* 保存返回的 `otel.Shutdown`。
* `Stop(ctx)` 调用保存的 shutdown。
* `Stage()` 返回 `StageFoundation`。

OTel 初始化必须发生在 `Start(ctx)`，不能发生在 adapter 构造时，确保生命周期由 bootstrap 统一驱动。

#### gRPC Client Connection

```go
func GRPCConn(name string, conn *grpc.ClientConn) Component
```

行为：

* `Start(ctx)` 不做网络拨号，只标记 component 可用。
* `Stop(ctx)` 调用 `conn.Close()`。
* `Stage()` 返回 `StageClient`。

gRPC target、dial options 和 connection 创建仍由业务代码负责。

#### gRPC Server

```go
func GRPCServer(name string, server *grpc.Server, listener net.Listener) Component
```

行为：

* `Start(ctx)` 启动 `server.Serve(listener)`。
* `Stop(ctx)` 调用 `server.GracefulStop()`。
* 如果 `Stop(ctx)` 的 context 超时，fallback 到 `server.Stop()`。
* `Stage()` 返回 `StageServer`。

listener 由业务代码创建后传入 adapter。端口绑定失败应在业务装配阶段直接暴露。

#### HTTP Server

```go
func HTTPServer(name string, server *http.Server) Component
```

行为：

* `Start(ctx)` 启动 `server.ListenAndServe()`。
* `Stop(ctx)` 调用 `server.Shutdown(ctx)`。
* `http.ErrServerClosed` 在 bootstrap 已进入 shutdown 后视为正常。
* `Stage()` 返回 `StageServer`。

第一阶段不提供外部 listener 版本。后续如需要端口 `0` 测试或 socket 复用，再增加独立 adapter。

#### ShutdownFunc

```go
func ShutdownFunc(name string, stage Stage, fn func(context.Context) error) Component
```

该 adapter 作为兜底能力保留，但常用组件应优先使用明确 adapter，避免所有组件都退化成匿名 shutdown hook。

### 默认组件组合

第一阶段可以提供窄边界默认组件：

```go
func Standard() []Component
```

`Standard()` 只包含通用 foundation 能力，例如 OTel。它不构造 Mongo、gRPC client、handler、repository 或业务 service。

## hello_world 实践方案

### service

现有 `experimental/golang/grpc_hello_world/service/main.go` 的生命周期包括：

* `otel.Init(context.Background())`
* `net.Listen("tcp", ":"+*port)`
* `grpc.NewServer(pgrpc.ServiceDefault()...)`
* `grpcServer.Serve(listener)`
* signal wait
* `grpcServer.GracefulStop()`
* OTel shutdown

迁移后：

* 保留 flag、listener 创建、gRPC server 创建和业务 handler 注册。
* 注册 `bootstrap.OTel()`。
* 注册 `bootstrap.GRPCServer("grpc", grpcServer, listener)`。
* 调用 `b.Run(context.Background())`。

### gateway

现有 `experimental/golang/grpc_hello_world/gateway/main.go` 的生命周期包括：

* `otel.Init(context.Background())`
* `grpc.NewClient(...)`
* `runtime.NewServeMux(pgrpc.GatewayDefault()...)`
* `http.Server{Handler: phttp.Handler(mux)}`
* `srv.ListenAndServe()`
* signal wait
* `srv.Shutdown(context.Background())`
* `conn.Close()`
* OTel shutdown

迁移后：

* 保留 flag、gRPC client 创建、gateway mux 创建、handler 注册、HTTP server 创建。
* 注册 `bootstrap.OTel()`。
* 注册 `bootstrap.GRPCConn("backend", conn)`。
* 注册 `bootstrap.HTTPServer("http", srv)`。
* 调用 `b.Run(context.Background())`。

### 验收标准

第一阶段完成后应验证：

* `bazel build //common/gopkg/bootstrap/...`
* `bazel test //common/gopkg/bootstrap/...`
* `bazel build //experimental/golang/grpc_hello_world/...`
* `bazel test //experimental/golang/grpc_hello_world/...`
* 有可用测试环境时，执行 `experimental/golang/grpc_hello_world/testplan/interface_test.yaml`。

测试覆盖至少包括：

* components 按 stage 和 name 启动。
* components 按反向顺序停止。
* duplicate name 注册失败。
* 启动失败触发已启动组件 rollback。
* Stop 超时或错误不会阻止后续组件 Stop，并通过 `errors.Join` 汇总。
* OTel 在关闭顺序中最后执行。
* HTTP server 在 bootstrap shutdown 后返回 `http.ErrServerClosed` 不算错误。
* gRPC server graceful stop 超时后 fallback 到 `Stop()`。

## 关键细节

### 为什么使用 Stage 而不是注册顺序

注册顺序容易随着业务代码重构变化，且错误不明显。`Stage` 将生命周期类别固化到 adapter，业务代码即使调整注册代码顺序，也不会改变基础设施、client、server 的启动和关闭顺序。

### 为什么同 Stage 使用 Name 排序

同 stage 内不应存在强顺序依赖。使用 name 排序只是为了让日志、测试和行为稳定。如果同 stage 内出现必须先后执行的组件，说明 stage 设计不够，应拆分 stage 或调整 adapter 分类。

### 为什么 OTel 是 Foundation

OTel 需要覆盖进程启动后的 server/client runtime，也需要在退出最后 flush 日志、trace 和 metric。因此 OTel 应最先启动、最后关闭。

### 为什么第一阶段不做 Daemon

`experimental/golang/grpc_hello_world` 只覆盖 OTel、client、server 三类生命周期，不包含 deploy worker 这类后台 daemon。daemon 涉及退出恢复、重试、backoff、异常分类等策略，应在迁移 `projects/infra/deploy` 时结合 worker 语义单独设计。

### 为什么 listener 由业务创建

端口、协议和服务注册属于业务装配。listener 创建失败应在进入 bootstrap 运行前直接返回，避免 adapter 同时承担配置解析和 runtime 生命周期管理。

## 决策详情

* Signal 由 bootstrap 管理；业务 goroutine 不负责进程生命周期。
* `Run(ctx)` 保留 context 参数，并使用默认退出信号。
* `RunSignal(ctx, signals...)` 用于自定义 signal。
* daemon 第一阶段移除。
* 同 stage 内按 name 排序。
* 组件 name 必须唯一。
* 退出开始后使用统一 shutdown timeout，不区分退出原因。
* 默认 shutdown timeout 为 5 秒，可通过 option 覆盖。
* 启动失败 rollback 也受同一 shutdown deadline 约束。
* server 仍在运行时不算错误；server 自行退出才触发 bootstrap shutdown。
* bootstrap 已开始 shutdown 后的 server 退出不算错误。
* OTel 总是最后停止。
* `ShutdownFunc` 保留为兜底 adapter，但常用 runtime 提供明确 adapter。

## 后续规划

完成 `experimental/golang/grpc_hello_world` 验证后，再按以下顺序迁移：

1. 迁移 `projects/game/session` 的 HTTP/grpc-gateway server 和 Mongo client 生命周期。
2. 迁移 `projects/game/gateway` 的 HTTP/grpc-gateway/WebSocket 入口生命周期。
3. 迁移 `projects/infra/deploy`，并在迁移前补充 daemon/worker 生命周期方案。
4. 根据真实服务迁移结果决定是否增加 Mongo adapter、HTTP listener adapter、daemon restart policy。
