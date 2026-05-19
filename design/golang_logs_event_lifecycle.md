# Golang logs Event 与 OTel 生命周期重构方案

## 目标

本方案用于继续重构 `common/gopkg/logs`，目标是：

* 移除 `logs` 包内按写入时机动态选择 console / OTel 后端的实现，改为由 OTel 初始化生命周期显式安装和卸载上报后端。
* 将日志字段从 `args ...any` 调整为类型明确的事件结构，避免 key / value 参数错位。
* 缩短日志输出方法名，调用方使用 `logs.Info`、`logs.Warn`、`logs.Error`、`logs.Debug`。
* 将事件包装器放在 `common/gopkg/logs/event` 子包，避免 `event.String`、`event.Err` 等包装器与 `logs.Info` 等输出方法混在同一个包内。
* 让 `common/gopkg/bootstrap`、`common/gopkg/otel` 和普通运行库统一使用 `common/gopkg/logs` 输出结构化日志。

本方案替换 [Golang logs 动态切换 OTel 后端方案](./golang_logs_dynamic_otel_switch.md) 中的动态 handler 模型，并延续 [Golang OpenTelemetry 公共库方案](./golang_observability_common_library.md) 的 deploy / non-deploy 环境语义。

## 范围

本方案覆盖：

* `common/gopkg/logs`：日志门面、默认 console logger、reporter 安装/卸载、context logger、测试注入能力。
* `common/gopkg/logs/event`：日志字段事件类型与通用包装器。
* `common/gopkg/otel`：deploy OTel Logs reporter 安装/卸载、non-deploy trace / metric 行为调整。
* `common/gopkg/bootstrap`：直接 `log/slog` 日志迁移到 `common/gopkg/logs`。
* 当前仓库内 `logs.InfoContext`、`logs.WarnContext`、`logs.ErrorContext`、`logs.DebugContext` 调用迁移。
* 相关单元测试、BUILD 文件和 Bazel 验证。

本方案不覆盖：

* 业务日志字段命名规范扩展。
* 将已有字段字面量或 `logFieldX` 常量统一抽象为领域 helper。
* SigNoz Collector、OTLP pipeline、Kubernetes metadata enrichment 配置。
* OTel Log SDK 对 error 的未来专用 `SetErr` 语义适配。

## 当前问题

### 动态 handler 引入反向依赖

当前 `common/gopkg/logs` 通过 `dynamicHandler` 在写日志时调用 `otel.IsLoggerProviderSet()` 判断后端：

```go
func (h *dynamicHandler) active() slog.Handler {
	if isInDeployMode() {
		return h.otel
	}
	return h.console
}
```

该模型让 `logs` 依赖 `common/gopkg/otel`，而新目标要求 `common/gopkg/otel` 在 provider 初始化完成后安装日志上报实现，因此需要把依赖方向调整为 `otel -> logs`。

### `args ...any` 容易错位

当前调用形态为：

```go
logs.InfoContext(ctx, "mongo endpoints resolved", "address_count", len(addresses))
```

`args ...any` 无法在编译期表达 key / value 成对关系，迁移时应改为明确的事件结构。

### 直接 `log/slog` 与统一日志门面不一致

旧方案允许 `common/gopkg/bootstrap` 和 `common/gopkg/otel` 直接使用标准库 `log/slog`，用于避免旧依赖图下的循环依赖。新方案移除 `logs -> otel` 后，可以统一迁移这些 direct slog 调用。

## 最终模型

### 包分层

目标依赖方向：

```text
common/gopkg/logs/event
        ↑
common/gopkg/logs
        ↑              ↑
common/gopkg/bootstrap common/gopkg/otel
                         ↑
                         └── OTel Logs SDK / otelslog
```

规则：

* `common/gopkg/logs/event` 不依赖仓库内其他包。
* `common/gopkg/logs` 不依赖 `common/gopkg/otel`，也不依赖 `otelslog`。
* `common/gopkg/otel` 负责创建 OTel LoggerProvider，并用 `otelslog.NewHandler` 创建 reporter logger 后安装到 `logs`。
* `common/gopkg/bootstrap` 使用 `logs` 输出自身生命周期日志。

### Event 子包

新增 `common/gopkg/logs/event`：

```go
package event

type Event struct {
	Key   string
	Value any
}

func String(key string, value string) Event
func Int(key string, value int) Event
func Int64(key string, value int64) Event
func Bool(key string, value bool) Event
func Any(key string, value any) Event
func Err(err error) Event
```

调用方使用：

```go
logs.Info(ctx, "mongo endpoints resolved", event.Int("address_count", len(addresses)))
logs.Error(ctx, "mongo client failed", event.String("target", rawTarget), event.Err(err))
```

`event.Err(nil)` 返回零值 `event.Event{}`。`logs` 在转换时跳过 key 为空且 value 为 nil 的 event，不输出 `error=nil`。

### logs 包 API

`common/gopkg/logs` 保留以下公开 API：

```go
func Default() *slog.Logger
func FromContext(ctx context.Context) *slog.Logger
func With(ctx context.Context, events ...event.Event) context.Context
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context
func SetDefault(logger *slog.Logger)

func Debug(ctx context.Context, msg string, events ...event.Event)
func Info(ctx context.Context, msg string, events ...event.Event)
func Warn(ctx context.Context, msg string, events ...event.Event)
func Error(ctx context.Context, msg string, events ...event.Event)

func InstallReporter(logger *slog.Logger) func()
```

兼容策略：

* `Default`、`FromContext`、`SetDefault` 继续公开，不改可见性和名称。
* `WithLogger` 继续用于测试或少量需要注入 logger 的调用链。
* 移除 `InfoContext`、`WarnContext`、`ErrorContext`、`DebugContext`。
* 没有可用 context 的迁移点使用 `context.Background()`。

### 默认 logger 与 reporter 模型

`logs` 默认使用 console logger：

```go
slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
```

`InstallReporter` 安装 OTel reporter logger：

```go
uninstall := logs.InstallReporter(slog.New(otelslog.NewHandler("dominion/common/gopkg/logs")))
```

行为规则：

* 未安装 reporter：输出到 console。
* 安装 reporter 后：包级 `logs.Info`、`logs.Warn`、`logs.Error`、`logs.Debug` 使用 reporter。
* `InstallReporter` 返回的 uninstall 只卸载自己安装的 reporter，避免 shutdown 时误清掉后续安装的新 reporter。
* uninstall 后恢复 console。
* `SetDefault` 用于测试注入，调用后默认 logger 固定为传入 logger。

### Event 到 slog.Attr 的转换

`logs` 输出方法内部将 `event.Event` 转为 `slog.Attr`，并使用 `LogAttrs`：

```go
FromContext(ctx).LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
```

转换规则：

* `event.Event{}` 跳过。
* `event.Err(nil)` 跳过。
* `event.Err(err)` 输出 key 为 `error` 的字段。
* 其他事件保持调用方给出的 key 和 value。

本阶段不使用 OTel Log SDK 的专用 `SetErr` 路径，避免与 `error` 字段产生重复异常属性。未来如 `otelslog` 稳定支持 error 专用语义，可单独设计迁移。

## OTel 生命周期模型

### deploy 环境

deploy 环境中，`common/gopkg/otel.initDeploy` 保持创建 trace、metric、log exporter 和 provider。LoggerProvider 创建并设置到 OTel global 后，安装 logs reporter：

```go
logglobal.SetLoggerProvider(lp)
uninstallLogs := logs.InstallReporter(slog.New(otelslog.NewHandler("dominion/common/gopkg/logs")))
```

shutdown 顺序：

1. 先调用 `uninstallLogs()`，让后续日志回到 console，避免继续写入即将关闭的 OTel LoggerProvider。
2. shutdown trace provider。
3. shutdown metric provider。
4. shutdown log provider。

### non-deploy 环境

non-deploy 环境保持以下语义：

* 不创建任何 OTLP exporter。
* 不解析、不访问 collector endpoint。
* 日志输出到 console。
* 保留 trace provider，使用 `sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))` 生成真实 trace id，保证本地测试和日志关联可用。
* 不再显式设置 metric provider，使用默认 noop meter provider。
* 保留 `otel.SetTextMapPropagator`，确保 trace context propagation 可用。

## 调用迁移规则

### 普通日志

迁移前：

```go
logs.InfoContext(ctx, "session created", logFieldSessionID, sessionID)
```

迁移后：

```go
logs.Info(ctx, "session created", event.String(logFieldSessionID, sessionID))
```

### 错误日志

迁移前：

```go
logs.ErrorContext(ctx, "failed to save session", logFieldSessionID, sessionID, logFieldError, err)
```

迁移后：

```go
logs.Error(ctx, "failed to save session", event.String(logFieldSessionID, sessionID), event.Err(err))
```

### 无 context 的日志

迁移前：

```go
slog.Info("http server started", "component", c.name)
```

迁移后：

```go
logs.Info(context.Background(), "http server started", event.String("component", c.name))
```

### `deploy_http_client.go` response body 错误字段

`common/gopkg/solver/deploy_http_client.go:60` 当前将 response body 字符串放到 `error` 字段：

```go
logs.WarnContext(ctx, "deploy http non-200 response", "status", resp.StatusCode, "error", string(body))
```

迁移时先转为 error，再使用 `event.Err`：

```go
logs.Warn(ctx, "deploy http non-200 response",
	event.Int("status", resp.StatusCode),
	event.Err(fmt.Errorf("response body: %s", string(body))),
)
```

`fmt` 已在该文件中使用，不需要新增 `errors` import。

### defer 日志

`projects/game/gateway/ws.go` 中的 defer 日志保持 defer 语义：

```go
defer logs.Info(ctx, "gateway: ws disconnect",
	event.String(logFieldSessionID, sessionID),
	event.String(logFieldConnID, wc.connID),
)
```

## 代码变更范围

### `common/gopkg/logs`

* 删除 `dynamicHandler`、`newDynamicHandler`、`newDeployHandler`、`isInDeployMode`。
* 删除 `common/gopkg/otel` import。
* 删除 `otelslog` import。
* 新增 reporter 安装状态和 `InstallReporter`。
* 将 `With` 参数改为 `events ...event.Event`。
* 将包级输出方法改为短名称和 `events ...event.Event`。
* 保留 `Default`、`FromContext`、`WithLogger`、`SetDefault`。

### `common/gopkg/logs/event`

* 新增 `Event` 类型。
* 新增 `String`、`Int`、`Int64`、`Bool`、`Any`、`Err` 包装器。
* `Err(nil)` 返回零值 event。

### `common/gopkg/otel`

* 增加 `common/gopkg/logs` 依赖。
* 增加 `otelslog` 依赖。
* deploy 初始化成功后安装 logs reporter。
* shutdown 时卸载 logs reporter。
* non-deploy 保留 trace provider，移除显式 meter provider 设置。
* 移除或停止使用 `IsLoggerProviderSet` / `loggerProviderSet`。如果为兼容暂时保留，也必须在 shutdown 或测试 reset 中避免表达过期 ready 状态。

### `common/gopkg/bootstrap`

* 将生产代码 direct `slog.Info`、`slog.Warn`、`slog.Error`、`slog.InfoContext`、`slog.WarnContext`、`slog.ErrorContext` 迁移为 `logs`。
* 无 ctx 的 goroutine 或 lifecycle 日志使用 `context.Background()`。

### 调用方

* 所有 `logs.InfoContext` 迁移为 `logs.Info`。
* 所有 `logs.WarnContext` 迁移为 `logs.Warn`。
* 所有 `logs.ErrorContext` 迁移为 `logs.Error`。
* 所有 `logs.DebugContext` 迁移为 `logs.Debug`。
* 字段字面量和 `logFieldX` 常量保留，只在调用处包一层 `event.String` / `event.Int` / `event.Any` / `event.Err`。

## BUILD 变更

### `common/gopkg/logs/BUILD.bazel`

* 移除 `//common/gopkg/otel`。
* 移除 `@io_opentelemetry_go_contrib_bridges_otelslog//:otelslog`。
* 增加 `//common/gopkg/logs/event`。

### `common/gopkg/logs/event/BUILD.bazel`

新增 `go_library`，importpath 为：

```text
dominion/common/gopkg/logs/event
```

### `common/gopkg/otel/BUILD.bazel`

* 增加 `//common/gopkg/logs`。
* 增加 `@io_opentelemetry_go_contrib_bridges_otelslog//:otelslog`。

### `common/gopkg/bootstrap/BUILD.bazel`

* 增加 `//common/gopkg/logs`。
* 增加 `//common/gopkg/logs/event`。

其他调用方 BUILD 文件由 Gazelle 更新。

## 测试计划

### 单元测试

更新或新增以下测试：

* `common/gopkg/logs/event`：
  * `String`、`Int`、`Int64`、`Bool`、`Any` 输出 key / value。
  * `Err(err)` 输出 key 为 `error`。
  * `Err(nil)` 返回零值 event。
* `common/gopkg/logs`：
  * 默认 console 输出。
  * `InstallReporter` 后输出进入 reporter。
  * uninstall 后恢复 console。
  * uninstall 只卸载自己安装的 reporter。
  * `With` 按 event 添加字段。
  * `SetDefault` 保持测试注入能力。
  * `WithLogger` 保持 context logger 注入能力。
* `common/gopkg/otel`：
  * deploy 初始化成功后安装 logs reporter。
  * shutdown 调用 logs reporter uninstall。
  * non-deploy 不创建 metric provider，不影响 `Tracer().Start` 生成 trace id。
  * non-deploy 不设置 logs reporter。
* `common/gopkg/bootstrap`：
  * 生命周期日志仍可被测试捕获。

### Bazel 验证

完成实现后执行：

```bash
bazel run @rules_go//go -- fmt <changed-go-files>
bazel run //:gazelle common/gopkg/logs common/gopkg/otel common/gopkg/bootstrap
bazel test //common/gopkg/logs/... //common/gopkg/otel/... //common/gopkg/bootstrap/...
bazel test //...
bazel build //...
```

如涉及依赖变更，再执行：

```bash
bazel mod tidy
```

## 决策详情

### 决策 1：Event 类型放在 `logs/event` 子包

决策：接受。

原因：

* `event.String`、`event.Err` 与 `logs.Info`、`logs.Error` 分层清晰。
* 避免 `logs.String` 与日志输出方法混杂在同一命名空间。
* 调用处可读性更强：`logs.Info(ctx, msg, event.String(...))`。

### 决策 2：`Err(nil)` 跳过

决策：接受。

原因：

* `error=nil` 对定位问题没有价值。
* Go `slog` 只有零值 `slog.Attr{}` 会被跳过，`slog.Any("error", nil)` 不会跳过，因此必须在 event 转换层显式过滤。

### 决策 3：保留 non-deploy trace provider

决策：接受。

原因：

* 既有方案要求 non-deploy 不远程上报，但仍保留真实 trace id。
* 当前测试依赖 `otel.Tracer().Start` 后 `TraceID(ctx)` 非空。
* 删除 trace provider 会退回 noop tracer，破坏本地 trace id 能力。

### 决策 4：non-deploy 使用默认 noop meter

决策：接受。

原因：

* non-deploy 不远程上报 metric。
* 默认 OTel meter provider 已是 noop，显式创建 `sdkmetric.NewMeterProvider()` 没有必要。
* 删除后 shutdown 逻辑更简单。

### 决策 5：保留 `Default`、`FromContext`、`SetDefault`

决策：接受。

原因：

* 保持公开 API 兼容。
* 测试仍依赖 `SetDefault` 注入 logger。
* `FromContext` 和 `WithLogger` 仍是 context logger 模型的基础。

### 决策 6：迁移 direct slog 到 `logs`

决策：接受。

原因：

* 新依赖方向消除了旧方案中的循环依赖顾虑。
* bootstrap、otel 和普通运行库日志行为统一。

## 未来规划

* 如 OTel Go `otelslog` 稳定支持 error 专用 `SetErr` 路径，可评估 `event.Err` 是否改为专用 error 语义。
* 后续可按业务领域增加字段 helper，但本方案不迁移字段字面量和 `logFieldX` 常量。
* 后续可设计日志 level、debug 开关或 console 输出格式配置。
