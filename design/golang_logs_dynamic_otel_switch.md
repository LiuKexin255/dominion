# Golang logs 动态切换 OTel 后端方案

## 目标

本方案用于重构 `common/gopkg/logs` 的默认日志后端选择机制，目标是：

* 让所有普通业务代码和公共运行库都可以统一使用 `common/gopkg/logs` 输出日志，不需要关心自身执行时 `common/gopkg/otel` 是否已经初始化。
* 在 OTel Logs provider 尚未初始化时，日志通过标准库 `log/slog` 输出到本地控制台，保证启动早期日志不丢失。
* 在 OTel Logs provider 初始化完成后，后续日志自动切换到 OTel Logs pipeline 上报，不需要调用方重新获取 logger 或区分初始化时机。
* 保持 `logs.With`、`logger.With`、`WithGroup` 等结构化字段语义在切换前后一致。

本方案延续 [Golang OpenTelemetry 公共库方案](./golang_observability_common_library.md) 中 `common/gopkg/otel` 与 `common/gopkg/logs` 的分层模型，并保留 [Golang common/gopkg 日志补全方案](./golang_common_gopkg_logging_completion.md) 中 `bootstrap` 与 `otel` 初始化链路直接使用标准库 `log/slog` 的例外规则。

## 范围

本方案覆盖：

* `common/gopkg/logs`：默认 logger 的后端选择机制、动态 handler、测试注入注释与测试覆盖。
* `common/gopkg/otel`：OTel Logs provider ready 状态的并发安全表达。
* 相关单元测试和 Bazel 验证。

本方案不覆盖：

* SigNoz Collector、OTLP pipeline、Kubernetes metadata enrichment 配置。
* 业务日志字段规范的扩展。
* 对历史 `log` / `slog` 调用的一次性迁移。
* OTel shutdown 后重新回退到控制台的运行时生命周期管理。

## 当前问题

当前 `common/gopkg/logs` 在第一次调用 `Default()` 时通过 `sync.Once` 固定默认 logger：

```go
if otel.IsLoggerProviderSet() {
	defaultLogger = slog.New(newDeployHandler("dominion/common/gopkg/logs"))
} else {
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
```

这会导致默认后端由第一次调用时机决定：

* 如果首次调用发生在 `otel.Init()` 前，后续即使 OTel Logs provider 初始化成功，`logs` 仍然使用控制台 handler。
* 如果首次调用发生在 `otel.Init()` 后，日志才会使用 OTel handler。

该行为把 `logs` 调用方与 OTel 启动时机耦合起来，不符合“普通运行库统一使用 `logs` 输出”的目标。

## 模型设计

### 默认 logger 模型

`logs.Default()` 仍返回一个进程内稳定的 `*slog.Logger`，但该 logger 不再在创建时决定最终输出后端，而是持有一个动态 `slog.Handler`。

示意结构：

```go
type dynamicHandler struct {
	console slog.Handler
	otel    slog.Handler
}
```

`Default()` 初始化一次：

```go
defaultLogger = slog.New(newDynamicHandler())
```

后续每条日志由 `dynamicHandler` 根据 OTel Logs provider ready 状态选择后端。

### 后端切换模型

`dynamicHandler` 在 `Enabled` 和 `Handle` 时读取 `otel.IsLoggerProviderSet()`：

```go
func (h *dynamicHandler) active() slog.Handler {
	if otel.IsLoggerProviderSet() {
		return h.otel
	}
	return h.console
}
```

行为规则：

* OTel Logs provider 未 ready：使用 `slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})` 输出到控制台。
* OTel Logs provider ready 后：使用 `otelslog.NewHandler("dominion/common/gopkg/logs")` 上报到 OTel。
* 切换后不双写控制台，避免 deploy 环境产生重复日志。
* OTel shutdown 后不回退到控制台；shutdown 属于进程退出路径，后端状态保持 ready。

`Enabled` 与 `Handle` 之间不增加额外锁。`bootstrap` 与 `otel` 初始化链路本身固定使用标准库 `log/slog`，普通 `logs` 调用通常发生在 OTel 初始化边界之外；即使状态刚好在单条日志的 `Enabled` 与 `Handle` 之间变化，也接受该瞬时边界语义，避免为初始化期低概率事件引入全局锁。

### 结构化字段模型

`WithAttrs` 与 `WithGroup` 必须同时作用到两个后端，不能只作用于当前 active handler。

示意实现：

```go
func (h *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dynamicHandler{
		console: h.console.WithAttrs(attrs),
		otel:    h.otel.WithAttrs(attrs),
	}
}

func (h *dynamicHandler) WithGroup(name string) slog.Handler {
	return &dynamicHandler{
		console: h.console.WithGroup(name),
		otel:    h.otel.WithGroup(name),
	}
}
```

这样可以保证以下场景在切换后仍保留字段：

```go
logger := logs.Default().With("request_id", requestID)
logger.InfoContext(ctx, "request started")

ctx = logs.With(ctx, "component", "resolver")
logs.WarnContext(ctx, "resolve failed", "error", err)
```

### OTel ready 状态模型

`common/gopkg/otel` 不再使用普通可变 `bool` 表示 LoggerProvider 是否已设置。该状态需要支持日志 goroutine 并发读取和 `otel.Init()` 写入。

建议模型：

```go
var loggerProviderSet atomic.Bool

func IsLoggerProviderSet() bool {
	return loggerProviderSet.Load()
}
```

deploy 初始化成功路径中，顺序必须是：

```go
logglobal.SetLoggerProvider(lp)
loggerProviderSet.Store(true)
```

规则：

* 只有 OTel log `LoggerProvider` 成功设置到 global 后，才能标记 ready。
* deploy 初始化失败时保持 false，`logs` 继续输出到控制台。
* non-deploy 初始化不设置 log provider ready，`logs` 继续输出到控制台。
* OTel shutdown 不把状态改回 false。

## 代码分层

### `common/gopkg/logs`

职责：

* 暴露统一业务日志门面。
* 创建默认动态 handler。
* 在每条日志写入时选择 console 或 OTel 后端。
* 保证 `WithAttrs`、`WithGroup` 在切换前后字段一致。
* 保留测试注入能力。

不负责：

* 初始化 OTel provider。
* 管理 OTel exporter 生命周期。
* 判断 deploy / non-deploy 环境。
* 在 OTel shutdown 后重建或回退日志 pipeline。

### `common/gopkg/otel`

职责：

* 初始化 trace、metric、log provider。
* 设置全局 OTel log provider。
* 维护并发安全的 log provider ready 状态。
* 提供 `IsLoggerProviderSet()` 给 `logs` 查询。

不负责：

* 输出普通业务日志。
* 依赖 `common/gopkg/logs`。

### `common/gopkg/bootstrap`

职责保持不变：

* 编排 OTel 初始化组件。
* 记录自身生命周期日志时继续直接使用标准库 `log/slog`。
* 不依赖 `common/gopkg/logs`，避免 `bootstrap -> logs -> otel` 初始化链路混乱。

## 关键细节

### 不依赖 `otelslog` 的 global delegate 作为切换机制

`otelslog.NewHandler()` 在构造时会基于当前 global logger provider 创建 OTel logger。OpenTelemetry global provider 对初始化前创建的 logger 有 delegate 机制，但不能作为本方案的主切换机制，原因是：

* OTel provider ready 前的日志不会自动 fallback 到控制台，而是可能被 noop provider 丢弃。
* 行为依赖 OTel global 内部代理语义，不如 `logs` 显式选择 console / OTel 后端清晰。

因此 `logs` 应自己实现动态 handler，并显式决定每条日志使用哪个后端。

### `SetDefault` 语义

`SetDefault(logger)` 保留为测试注入能力。调用后默认 logger 固定为传入 logger，不再参与 console / OTel 自动切换。

需要在注释中明确：

* `SetDefault` 仅用于测试。
* 生产代码应依赖 `Default()` 的自动切换机制。
* 调用 `SetDefault` 会绕过动态 handler。

### 测试注入

`logs` 测试需要能稳定验证两个后端。建议保留或调整包内 handler factory，例如：

```go
var newDeployHandler = func(name string) slog.Handler {
	return otelslog.NewHandler(name)
}

var newConsoleHandler = func() slog.Handler {
	return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
}
```

这些变量只用于包内测试替换，不作为生产扩展点。

### 日志级别与输出格式

控制台 fallback 保持当前行为：

* 输出到 `os.Stdout`。
* 使用 `slog.TextHandler`。
* 默认级别为 `slog.LevelInfo`。

本方案不调整 Debug 运行时开关，不把控制台输出改为 JSON，也不改为 stderr。

## 实施计划

### Phase 1：重构 OTel ready 状态

修改：

* `common/gopkg/otel/otel.go`
* `common/gopkg/otel/otel_test.go`

内容：

* 移除或替换普通 `LoggerProviderSet bool`。
* 使用并发安全状态实现 `IsLoggerProviderSet()`。
* deploy 初始化成功时，在 `logglobal.SetLoggerProvider(lp)` 后标记 ready。
* non-deploy、deploy 初始化失败、partial failure 均保持 not ready。
* 更新测试，不再直接读写普通 bool。

验收：

* deploy 初始化成功后 `IsLoggerProviderSet()` 为 true。
* non-deploy 初始化后 `IsLoggerProviderSet()` 为 false。
* log exporter 创建失败等 partial failure 后仍为 false。

### Phase 2：实现 logs 动态 handler

修改：

* `common/gopkg/logs/logs.go`
* `common/gopkg/logs/logs_test.go`

内容：

* 新增 `dynamicHandler`。
* `Default()` 改为初始化持有动态 handler 的 logger。
* `Enabled`、`Handle` 通过 `otel.IsLoggerProviderSet()` 选择后端。
* `WithAttrs`、`WithGroup` 同时派生 console 和 OTel handler。
* 更新 `SetDefault` 注释，说明会绕过自动切换，仅用于测试。

验收：

* OTel not ready 时，`logs.InfoContext` 输出到控制台。
* 先调用 `logs.Default()`，再将 OTel 状态切到 ready，后续日志进入 OTel handler。
* 切换后不再写控制台。
* 切换前通过 `logger.With` 或 `logs.With` 绑定的字段，切换后仍存在。
* `SetDefault` 后使用注入 logger，不进行自动切换。

### Phase 3：格式化与 Bazel 验证

执行：

```bash
bazel run @rules_go//go -- fmt common/gopkg/otel/otel.go common/gopkg/otel/otel_test.go common/gopkg/logs/logs.go common/gopkg/logs/logs_test.go
bazel test //common/gopkg/otel/...
bazel test //common/gopkg/logs/...
bazel test //common/gopkg/...
```

如果 BUILD 文件因测试依赖变化需要更新，执行：

```bash
bazel run //:gazelle common/gopkg
```

## 验收标准

单元测试：

* OTel 未初始化时，`logs` 输出到控制台。
* OTel 初始化完成后，已有默认 logger 自动切换到 OTel handler。
* OTel 初始化后不双写控制台。
* `WithAttrs`、`WithGroup`、`logs.With` 的字段在切换前后保持一致。
* `SetDefault` 明确绕过自动切换，并有测试覆盖。
* `otel.IsLoggerProviderSet()` 无数据竞争风险。

Bazel 验证：

* `bazel test //common/gopkg/otel/...`
* `bazel test //common/gopkg/logs/...`
* `bazel test //common/gopkg/...`

行为验证：

* 服务启动早期、OTel 初始化前的普通 `logs` 调用可以在控制台看到。
* OTel 初始化完成后的普通 `logs` 调用可以进入 OTel Logs pipeline。
* 业务代码无需按 OTel 初始化时机区分日志入口。

## 决策详情

* 使用动态 `slog.Handler`，而不是动态替换 `*slog.Logger`。
* 每条日志写入时选择后端，避免第一次调用固定后端。
* OTel ready 后只上报 OTel，不双写控制台。
* OTel shutdown 后不回退控制台。
* `Enabled` 与 `Handle` 之间不加锁，接受初始化边界瞬时切换语义。
* `SetDefault` 保留测试用途，但调用后绕过自动切换。
* `bootstrap` 与 `otel` 包继续直接使用标准库 `log/slog`，不纳入 `logs` facade 的依赖链。

## 未来规划

* 如需 OTel pipeline 运行时重启，再单独设计 provider lifecycle 与日志后端回退策略。
* 如需 deploy 环境同时保留 stdout 兜底，再单独设计 tee handler 与去重规则。
* 如需 Debug 日志运行时开关，再扩展 handler level 配置模型。
