# Golang 服务 Bootstrap Daemon 与业务服务迁移方案

## 目标

本方案是 `design/golang_service_bootstrap_framework.md` 的后续方案，用于在现有 `common/gopkg/bootstrap` 生命周期框架上补充 daemon 管理能力，并迁移以下业务服务：

* `projects/infra/deploy/`
* `projects/game/session/`
* `projects/game/gateway/`

目标是：

* 让后台 daemon / worker 由 bootstrap 统一启动、停止、失败重启和最终失败上报。
* 让 daemon 每次重启都能构建新的 worker 实例，避免复用已关闭的 queue、timer、watcher、连接或其他运行期状态。
* 让 HTTP/gRPC-gateway server、Mongo client、deploy worker、OpenTelemetry 等 runtime 生命周期都进入统一的 stage 顺序。
* 保持当前 `app + cmd` 架构：`cmd` 只负责配置解析与外部依赖创建，`app` 固化服务组成，测试替换 fake dependency 时不会绕开真实服务装配方式。
* 消除三个服务中重复的 signal、server shutdown、client disconnect 和 worker goroutine 管理逻辑。

## 范围

本方案覆盖：

* `common/gopkg/bootstrap` 新增 daemon / worker supervisor 模型。
* bootstrap 退出监控从 server-only 扩展为通用 exit watcher。
* `projects/infra/deploy` worker/queue 生命周期调整与迁移方式。
* `projects/game/session` HTTP/gRPC-gateway server 与 Mongo client 生命周期迁移方式。
* `projects/game/gateway` HTTP/gRPC-gateway/WebSocket 入口生命周期迁移方式。
* 单元测试、构建验证和已有 testplan 的验收范围。

本方案不覆盖：

* 修改 deploy 业务 reconcile 语义、重试策略或 Kubernetes runtime 实现。
* 修改 session/gateway 的业务接口、协议或 WebSocket 消息模型。
* 引入第三方 supervisor 依赖。外部库只作为设计参考，最终在 `common/gopkg/bootstrap` 中实现窄边界能力。
* 动态注册或运行时增删 bootstrap component。

## 当前问题

现有 bootstrap 已能管理 foundation、client、server 三类长期运行组件，但业务服务仍有以下问题：

* `projects/infra/deploy/app/bootstrap.go` 直接 `go b.Worker.Run()`，worker panic 或意外退出后不会被统一重启。
* deploy `Worker.Run()` 不接收 context，停止依赖 `Queue.Stop()`；而 `Queue.Stop()` 当前直接 `close(q.done)`，不是幂等，也不适合 worker 重启后复用。
* `projects/infra/deploy/app/server.go`、`projects/game/session/app/server.go`、`projects/game/gateway/app/server.go` 都手写 HTTP/gRPC-gateway shutdown。
* `projects/game/session/app/cmd/main.go` 手写 Mongo client `defer Disconnect`，不在 bootstrap 的 stage 顺序内。
* `cmd` 中存在 signal 等进程生命周期逻辑，和 bootstrap 的统一 `Run(ctx)` 模型重复。

## 模型设计

### Component 与 Worker 的关系

`Component` 仍然是 bootstrap 管理的进程生命周期单元。实际 worker 不直接注册为 component，而是由 daemon component 内部管理。

```text
Bootstrap
  └── Daemon Component
        ├── WorkerBuilder
        ├── current Worker instance
        ├── restart policy
        ├── backoff
        ├── panic recovery
        └── Stop/cleanup coordination
```

规则：

* `Daemon` 实现 `Component`。
* 每次 `WorkerBuilder` 构建出的 `Worker` 是 daemon 的一次运行实例。
* `Worker` 不进入 bootstrap component 注册表。
* bootstrap 只关心 daemon component 的启动、停止和最终失败。
* worker 的重启、清理、backoff、panic recover 和错误分类都由 daemon 内部负责。

这样可以保持 bootstrap “启动前注册、运行后冻结”的模型，不需要运行期动态增删 component。

### Stage

在现有 stage 基础上增加 `StageDaemon`：

```go
type Stage int

const (
	StageFoundation Stage = 100
	StageClient     Stage = 200
	StageDaemon     Stage = 250
	StageServer     Stage = 300
)
```

含义：

* `StageFoundation`：进程级基础设施，例如 OTel。
* `StageClient`：外部 client 或连接，例如 Mongo client、gRPC client connection。
* `StageDaemon`：后台长期运行任务，例如 deploy reconcile worker。
* `StageServer`：入口服务，例如 HTTP server、gRPC server、grpc-gateway HTTP server。

启动顺序为：

```text
Foundation -> Client -> Daemon -> Server
```

关闭顺序为：

```text
Server -> Daemon -> Client -> Foundation
```

这样可以保证入口服务开始接流量前，后台 worker 已经可用；退出时先停止入口，再停止后台任务，最后关闭外部 client 和 OTel。

### WorkerBuilder

daemon 不直接接收单个 `func(ctx) error`，而是接收可重建 worker 的 builder：

```go
type WorkerBuilder interface {
	Build(ctx context.Context) (Worker, error)
}

type WorkerBuilderFunc func(ctx context.Context) (Worker, error)
```

`Build(ctx)` 规则：

* 每次 daemon 首次启动或重启时调用一次。
* 返回一个全新的 worker 实例。
* 可以在其中创建 queue、watcher、timer、subscription 等本轮 worker 私有状态。
* 可以执行本轮启动前恢复逻辑，例如 deploy `domain.Recover(ctx, repo, queue)`。
* `Build` 失败参与 daemon restart policy。

### Worker

`Worker` 是 daemon 的一次运行实例：

```go
type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

`Start(ctx)` 规则：

* 对长期 worker，`Start` 必须阻塞直到本轮 worker 退出。
* `Start` 返回表示本轮 worker 已结束，daemon 将根据返回错误和 restart policy 决定是否重启。
* `Start` 应监听传入 context；daemon shutdown 时该 context 会被取消。

`Stop(ctx)` 规则：

* `Stop` 用于请求本轮 worker 退出并清理资源。
* `Stop` 可以关闭 queue、取消 watcher、flush buffer、释放 subscription 等。
* `Stop` 必须幂等，或由 worker 内部保证不会重复关闭不可重复资源。
* `Stop` 不负责决定是否重启。

### Daemon adapter

对外 API：

```go
func Daemon(name string, builder WorkerBuilder, opts ...DaemonOption) Component
```

daemon component 行为：

* `Name()` 返回传入 name。
* `Stage()` 返回 `StageDaemon`。
* `Start(ctx)` 启动 daemon supervisor goroutine 并立即返回。
* `Stop(ctx)` cancel 当前 worker context，调用当前 worker `Stop(ctx)`，等待 supervisor goroutine 退出。
* worker panic 会被 recover 成错误并交给 restart policy。
* 非 shutdown 状态下 worker 退出，由错误分类和 restart policy 决定 restart、stop 或 fatal。
* fatal 或 restart policy exhausted 会通过 exit watcher 上报 bootstrap，触发全局 shutdown。

### Daemon restart policy

daemon 默认应支持有限重启，避免 crash loop：

```go
type DaemonDecision int

const (
	DaemonRestart DaemonDecision = iota
	DaemonStop
	DaemonFatal
)
```

建议选项：

```go
func WithDaemonRestartBackoff(backoff time.Duration) DaemonOption
func WithDaemonMaxRestartBackoff(backoff time.Duration) DaemonOption
func WithDaemonMaxRestarts(max int) DaemonOption
func WithDaemonErrorClassifier(classifier func(error) DaemonDecision) DaemonOption
```

默认语义：

* daemon context 已取消后，`context.Canceled` / `context.DeadlineExceeded` 视为正常停止。
* 非 shutdown 状态下 `Build` 或 `Start` 返回错误，默认 `DaemonRestart`。
* panic 默认 `DaemonRestart`。
* restart 次数超过上限后转为 terminal error，上报 bootstrap。
* `DaemonStop` 表示该 daemon 本轮自然结束，不再重启，也不触发全局 shutdown。
* `DaemonFatal` 表示立即上报 bootstrap 并触发全局 shutdown。

### Exit watcher

现有 bootstrap 只监控 server component 的 `ServerDone()`。本方案将其泛化为内部 exit watcher：

```go
type exitWatcher interface {
	Done() <-chan error
}
```

规则：

* HTTP/gRPC server adapter 和 daemon adapter 都可以实现该接口。
* bootstrap 监控所有已启动 component 中的 exit watcher，不再只检查 `StageServer`。
* exit watcher 返回 nil 表示该 component 已自然结束，不触发全局 shutdown。
* exit watcher 返回非 nil error 表示该 component terminal failure，bootstrap 进入 shutdown 并返回该错误与 shutdown 错误的组合。

## 代码分层

### `common/gopkg/bootstrap`

新增或调整：

* `StageDaemon`。
* `WorkerBuilder`、`WorkerBuilderFunc`、`Worker`。
* `Daemon(name string, builder WorkerBuilder, opts ...DaemonOption) Component`。
* daemon restart policy、错误分类、panic recover、backoff。
* 将 server-only monitoring 泛化为 exit watcher monitoring。
* 新增明确的 Mongo client adapter：

```go
func MongoClient(name string, client *mongo.Client) Component
```

Mongo adapter 固定为 `StageClient`，`Start(ctx)` 不做额外初始化，`Stop(ctx)` 调用 `client.Disconnect(ctx)`。本方案不再引入通用 `ShutdownFunc`，避免 runtime 生命周期退化成匿名 shutdown hook。

### `projects/infra/deploy/app`

`app` 继续作为 deploy 服务 composition root，负责固化：

* repository
* runtime implementation
* queue / worker builder
* handler
* HTTP/gRPC-gateway server component
* bootstrap component 列表

建议将当前 `Bootstrap` 调整为：

```go
type Bootstrap struct {
	Repo    domain.Repository
	Runtime domain.EnvironmentRuntime
	Handler *deploy.Handler
}
```

deploy worker 的 queue 不再作为长期固定字段暴露，而是由 worker builder 每轮重建。

worker builder 示例：

```go
type WorkerBuilder struct {
	Repo    domain.Repository
	Runtime domain.EnvironmentRuntime
}

func (b *WorkerBuilder) Build(ctx context.Context) (bootstrap.Worker, error) {
	return domain.NewWorkerRunner(ctx, b.Repo, b.Runtime)
}
```

`domain.NewWorkerRunner` 返回值只需要结构性满足 `bootstrap.Worker` 接口，不需要 domain package 反向依赖 `common/gopkg/bootstrap`。

domain 内部 worker runner 示例：

```go
type WorkerRunner struct {
	queue  *Queue
	worker *Worker
}

func NewWorkerRunner(ctx context.Context, repo Repository, runtime EnvironmentRuntime) (*WorkerRunner, error) {
	queue := NewQueue()
	if err := Recover(ctx, repo, queue); err != nil {
		return nil, err
	}
	worker := NewWorker(repo, queue, runtime)
	return &WorkerRunner{queue: queue, worker: worker}, nil
}

func (w *WorkerRunner) Start(ctx context.Context) error {
	return w.worker.Run(ctx)
}

func (w *WorkerRunner) Stop(ctx context.Context) error {
	w.queue.stop()
	return nil
}
```

`queue.stop()` 是 deploy domain 内部清理细节，不作为公开 API 暴露。优先让 `domain.Worker.Run(ctx)` 通过 context 退出，queue 的停止能力只用于 `WorkerRunner.Stop(ctx)` 内部唤醒阻塞的 dequeue。

### `projects/infra/deploy/domain`

需要调整：

* `Worker.Run()` 改为 `Run(ctx context.Context) error`。
* `Worker.Run(ctx)` 调用 `queue.Dequeue(ctx)`，不再使用 `context.Background()`。
* `scheduleRetry` 使用 daemon/worker context，shutdown 后不再继续 enqueue retry。
* `Queue.Stop()` 不再作为导出的公开 API；如仍需唤醒阻塞的 `Dequeue`，改为 worker 内部私有方法并由 worker `Stop(ctx)` 调用。
* queue 私有停止方法必须幂等，避免重复 close panic。

deploy 当前 `domain.ErrWorkerFatal` 已表达 worker 不应吞掉的错误。迁移时 daemon error classifier 将该错误分类为 `DaemonFatal`，立即触发整体 shutdown。后续如识别出临时依赖错误，应新增更精确的错误类型，而不是复用 `ErrWorkerFatal`。

### `projects/infra/deploy/app/cmd`

`cmd` 只保留：

* flag/env 解析。
* Mongo client / repository 创建。
* K8s runtime client 创建。
* 调用 `app.NewBootstrap(...)`。
* 创建 `bootstrap.New(...)` 并注册 `app` 提供的 components。
* `b.Run(context.Background())`。

移除：

* 手写 `signal.NotifyContext`。
* 手写 `errCh`。
* 手写 `app.Serve(ctx, ...)` 阻塞生命周期。
* 手写 worker goroutine 启动。

### `projects/game/session/app`

`app.NewBootstrap(repo, tokenIssuer, gatewayReg)` 保持作为服务组成入口。

迁移方式：

* `app/server.go` 不再提供阻塞式 `Serve(ctx, ...)`。
* 改为构造 HTTP server / bootstrap component。
* grpc-gateway mux、handler 注册、HTTP handler 组合仍留在 `app`。
* Mongo client 不进入 `app.NewBootstrap`，但其关闭应由 bootstrap `StageClient` component 管理。

### `projects/game/session/app/cmd`

`cmd` 保留：

* token secret / TTL / HTTP addr / Mongo target 解析。
* Mongo client 创建。
* repository 创建。
* deploy stateful resolver 与 gateway registry 创建。
* token issuer 创建。

移除：

* 手写 signal context。
* 手写 Mongo client `defer Disconnect`。
* 手写 `app.Serve(ctx, ...)`。

Mongo client 通过 `bootstrap.MongoClient("mongo", client)` 注册到 `StageClient`，由 bootstrap 统一关闭。

### `projects/game/gateway/app`

`app.NewBootstrap(tokenSecret, gatewayID)` 保持作为服务组成入口。

迁移方式：

* 保留现有 `gatewayRouter`，继续将 WebSocket 路径路由给 `WSHandler`，其他路径给 grpc-gateway mux。
* `app/server.go` 改为构造 HTTP server / bootstrap component，不再阻塞运行。
* 目前无 Mongo client、daemon 或外部 client 生命周期需要额外迁移。

### `projects/game/gateway/app/cmd`

`cmd` 保留：

* HTTP addr、token secret、gateway id 解析。
* `app.NewBootstrap(...)`。
* 创建并运行 bootstrap。

移除：

* 手写 signal context。
* 手写 `app.Serve(ctx, ...)`。

## 迁移顺序

建议按以下顺序实施，降低风险：

1. 扩展 `common/gopkg/bootstrap`：`StageDaemon`、daemon model、exit watcher、必要 adapter 和单元测试。
2. 调整 `projects/infra/deploy/domain`：`Worker.Run(ctx)`、retry context、queue 私有停止语义。
3. 迁移 `projects/game/gateway`：只有入口 server 生命周期，改动最小。
4. 迁移 `projects/game/session`：入口 server + Mongo client shutdown。
5. 迁移 `projects/infra/deploy`：入口 server + deploy daemon worker。

deploy 放在最后，是因为它同时依赖 bootstrap daemon 能力和 domain worker/queue 语义调整。

## 验收标准

### bootstrap

应验证：

* `bazel build //common/gopkg/bootstrap/...`
* `bazel test //common/gopkg/bootstrap/...`

测试覆盖至少包括：

* `StageDaemon` 启动顺序位于 client 后、server 前。
* shutdown 顺序为 server、daemon、client、foundation。
* daemon `Start` 启动 supervisor goroutine并立即返回。
* daemon `Stop` cancel 当前 worker、调用 worker `Stop`、等待 supervisor 退出。
* worker panic 后按 policy restart。
* worker error 后按 backoff restart。
* `Build` error 参与 restart policy。
* fatal classifier 触发 bootstrap shutdown 并返回错误。
* restart exhausted 触发 bootstrap shutdown 并返回错误。
* 默认 restart 参数为初始 backoff `1s`、最大 backoff `30s`、连续失败上限 `5`。
* 第一阶段不启用 jitter。
* shutdown context 下的 worker 退出不触发 restart。
* exit watcher 可同时覆盖 server 和 daemon。
* Mongo adapter 属于 `StageClient`，Stop 调用 `client.Disconnect(ctx)`。

### `projects/game/gateway`

应验证：

* `bazel build //projects/game/gateway/...`
* `bazel test //projects/game/gateway/...`
* 有可用测试环境时，执行 `projects/game/gateway/testplan/interface_test.yaml`。

### `projects/game/session`

应验证：

* `bazel build //projects/game/session/...`
* `bazel test //projects/game/session/...`
* 有可用测试环境时，执行 `projects/game/session/testplan/interface_test.yaml`。

### `projects/infra/deploy`

应验证：

* `bazel build //projects/infra/deploy/...`
* `bazel test //projects/infra/deploy/...`

`projects/infra/deploy/README.md` 明确该服务因无法自举不进行大型测试，因此本迁移不要求 deploy testplan。

## 关键细节

### 为什么 Worker 不作为 Component 注册

worker 是 daemon 的一次运行实例，可能因失败被多次重建。component 是 bootstrap 的稳定生命周期单元，注册后在 `Run` 期间冻结。让 worker 直接参与 component 管理会要求 bootstrap 支持运行期动态增删 component，复杂度明显上升，也会让 restart 语义分散。

因此只有 daemon 是 component；worker 由 daemon 内部独占管理。

### 为什么使用 WorkerBuilder 而不是 DaemonFunc

单个 `DaemonFunc` 适合简单循环，但不适合需要重启后重建状态的 worker。deploy worker 当前包含 queue、retry goroutine、repository recovery 等运行期状态。重启时复用同一个函数容易误用旧状态，尤其是 `Queue.Stop()` close 后无法复用。

`WorkerBuilder` 明确表达：每次 restart 都创建新 worker 实例，旧 worker 通过 `Stop` 清理。

### 为什么 deploy queue 应随 worker 重建

deploy queue 当前通过 close channel 让 `Dequeue` 返回。关闭后的 queue 不适合再次用于新 worker。将 queue 放入 worker builder 内部，每轮 worker 获得新 queue，可以避免重启复用已关闭状态。

queue 的停止能力不再作为公开 API 暴露。bootstrap 管理 daemon component，daemon 管理 worker 实例，worker 的 `Stop(ctx)` 管理本轮 queue 清理。

### 为什么 domain.Recover 放在 Build 中

deploy worker 每次新建 queue 后，都需要重新从 repository 恢复未完成 reconcile 状态。将 `domain.Recover(ctx, repo, queue)` 放在 `Build` 中，可以保证首次启动和重启语义一致。

### 为什么 daemon fatal 才触发全局 shutdown

普通 worker error 应由 daemon supervisor 内部消化并重启，否则 worker 的临时失败会导致入口服务退出。只有 fatal error 或 restart policy exhausted 表示该 daemon 已无法恢复，此时才通过 exit watcher 上报 bootstrap，触发整体 shutdown。

### 为什么 app+cmd 架构必须保留

`app` 固化 handler、service、repository、runtime、worker、server 的组合关系。测试中替换 fake repository、fake runtime 或 fake Mongo 时，仍通过相同 `app.NewBootstrap` / component 构造路径装配服务，避免测试装配和生产装配漂移。

`cmd` 只处理进程边界输入：flag、env、外部 client、`bootstrap.Run`。

## 决策详情

* `Daemon` 是 bootstrap component，实际 worker 不是 component。
* daemon 接收 `WorkerBuilder`，不直接接收 `DaemonFunc`。
* `WorkerBuilder.Build(ctx)` 每次启动或重启都构建新 worker。
* `Worker.Start(ctx)` 必须阻塞直到本轮 worker 结束。
* `Worker.Stop(ctx)` 负责请求退出和清理本轮 worker 私有资源。
* `Build` 失败参与 restart policy。
* daemon 默认支持有限重启，初始 backoff 为 `1s`，最大 backoff 为 `30s`，连续失败上限为 `5`。
* daemon 第一阶段不加入 jitter。
* daemon panic 由 supervisor recover，并按 restart policy 处理。
* fatal 或 restart exhausted 才上报 bootstrap 并触发全局 shutdown。
* bootstrap exit monitoring 从 server-only 泛化为 exit watcher。
* 新增 `StageDaemon`，启动顺序在 client 后、server 前。
* deploy queue/worker/recover 收敛到 worker builder 内部。
* deploy `Worker.Run` 改为接收 context。
* deploy `Queue.Stop` 不再导出；queue 停止由 worker `Stop(ctx)` 内部管理。
* `domain.ErrWorkerFatal` 分类为 `DaemonFatal`。
* 新增明确 Mongo adapter，session Mongo client shutdown 进入 `StageClient`。
* gateway 当前只需要迁移 HTTP/gRPC-gateway/WebSocket 入口 server 生命周期。
* 三个服务都保留 `app + cmd` 架构。

## 待决策项目

当前无待决策项目。已确认：新增明确 Mongo adapter；deploy queue 停止不再导出，由 bootstrap daemon 管理 worker 生命周期；`domain.ErrWorkerFatal` 按 fatal 处理；daemon 默认 restart 参数为初始 backoff `1s`、最大 backoff `30s`、连续失败上限 `5`；第一阶段不加入 jitter。

## 后续规划

完成本方案后，可根据业务服务迁移结果继续评估：

* 是否增加 HTTP listener adapter，用于端口 `0` 测试或 socket 复用。
* 是否支持 periodic worker、one-shot worker 或动态 child worker。
* 是否为 daemon restart 事件接入结构化日志、metric 和 trace。
* 是否将 deploy worker 的错误类型进一步细分为 fatal、retryable runtime error 和 data corruption error。
