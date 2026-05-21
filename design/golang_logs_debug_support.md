# Golang logs debug 级别与 OTel scope name 优化方案

## 目标

本方案用于为 `common/gopkg/logs` 增加按部署环境类型选择日志级别的能力，并收敛 OTel logs reporter 的构造职责，目标是：

* dev / test 类型部署环境默认启用 `debug` 级别日志，便于排查测试环境和开发环境问题。
* prod 类型部署环境默认使用 `info` 级别日志，避免生产环境默认输出过多 debug 日志。
* 用户在 artifact env 中显式配置 `LOG_LEVEL` 时，保留用户配置并覆盖平台默认值。
* `common/gopkg/otel` 不再直接构造 `otelslog.NewHandler`，只调用 `common/gopkg/logs` 提供的 reporter 构造方法。
* OTel logger 的 `scope.name` 使用服务包名，例如 `dominion/projects/game/gateway`，而不是 `cmd` 入口包或 `common/gopkg/logs` 包名，便于在 SigNoz 中按服务代码定位日志来源。

本方案延续 [Golang logs Event 与 OTel 生命周期重构方案](./golang_logs_event_lifecycle.md) 中的 `InstallReporter` 生命周期模型，并延续 [Deploy artifact 环境变量支持方案](./deploy_artifact_env_support.md) 中 artifact env 作为用户运行参数的语义。

## 范围

本方案覆盖：

* `common/gopkg/logs`：日志级别解析、默认 console handler 级别、OTel reporter 构造方法、OTel handler 级别过滤。
* `common/gopkg/otel`：新增 logger name 配置项，并改为调用 `logs` 包创建 reporter。
* `projects/infra/deploy/runtime/k8s`：根据 deploy environment type 为 workload 注入默认 `LOG_LEVEL`。
* 服务入口：注册 OTel component 时传入服务包名。
* 相关单元测试、BUILD 文件和 Bazel 验证。

本方案不覆盖：

* 修改业务日志字段规范。
* 修改 SigNoz Collector、OTLP pipeline 或 Kubernetes metadata enrichment 配置。
* 引入运行时动态调整日志级别的控制面接口。
* 将 `LOG_LEVEL` 设为平台保留环境变量。`LOG_LEVEL` 仍允许用户配置。

## 当前问题

### Debug 方法已有但默认不可见

`common/gopkg/logs` 已提供：

```go
func Debug(ctx context.Context, msg string, events ...event.Event)
```

但默认 console handler 固定使用 `slog.LevelInfo`，因此未安装 reporter 或 reporter 也按 info 过滤时，debug 记录不会输出。

### deploy runtime 不向服务传递环境类型

deploy 控制面中存在 `domain.EnvironmentTypeProd`、`domain.EnvironmentTypeTest`、`domain.EnvironmentTypeDev`，但运行中服务目前只收到：

* `SERVICE_APP`
* `DOMINION_ENVIRONMENT`
* `POD_NAMESPACE`

其中 `DOMINION_ENVIRONMENT` 是环境名称，不是环境类型。运行时代码不应通过环境名称猜测 dev / test / prod。

### reporter 构造泄漏在 otel 包中

当前 deploy OTel 初始化在 `common/gopkg/otel` 中直接构造 reporter：

```go
logs.InstallReporter(slog.New(otelslog.NewHandler("dominion/common/gopkg/logs")))
```

这让 `otel` 包知道 `logs` reporter 的构造细节，也把 OTel `scope.name` 固定成 `dominion/common/gopkg/logs`，不利于在 SigNoz 中按实际服务定位日志。

## 最终模型

### 运行时日志级别模型

运行时日志级别由 `LOG_LEVEL` 环境变量决定：

| `LOG_LEVEL` | 生效级别 |
| --- | --- |
| `debug` | `slog.LevelDebug` |
| `info` | `slog.LevelInfo` |
| 空值 / 未设置 | `slog.LevelInfo` |
| 其他值 | `slog.LevelInfo` |

解析规则：

* 大小写不敏感，解析前 `strings.TrimSpace`。
* 非法值不 panic，回退到 `info`。
* 本阶段只支持 `debug` 和 `info`，不引入 `warn` / `error` 级别配置，避免扩大行为面。

### deploy 默认注入模型

deploy runtime 根据 `domain.EnvironmentType` 为 artifact workload 注入默认 `LOG_LEVEL`：

| EnvironmentType | 默认注入 |
| --- | --- |
| `EnvironmentTypeProd` | `LOG_LEVEL=info` |
| `EnvironmentTypeTest` | `LOG_LEVEL=debug` |
| `EnvironmentTypeDev` | `LOG_LEVEL=debug` |
| `EnvironmentTypeUnspecified` | `LOG_LEVEL=info` |

用户覆盖规则：

* 如果 artifact env 已显式配置 `LOG_LEVEL`，runtime 保留用户值，不再写入平台默认值。
* 如果 artifact env 未配置 `LOG_LEVEL`，runtime 追加平台默认值。
* `LOG_LEVEL` 不进入 runtime 保留变量集合，不影响现有 artifact env 校验语义。

这样可以同时满足：平台为不同环境提供合理默认值；用户需要临时打开或关闭 debug 时，仍可通过 deploy 配置显式覆盖。

### OTel reporter 构造模型

`common/gopkg/logs` 新增 OTel reporter 构造方法：

```go
func NewOTelReporter(name string) *slog.Logger
```

职责：

* 使用传入的 `name` 创建 `otelslog.NewHandler(name)`。
* 按 `LOG_LEVEL` 包装最小级别过滤 handler。
* 返回可传给 `InstallReporter` 的 `*slog.Logger`。

`common/gopkg/otel` 只负责安装：

```go
uninstallLogs = logs.InstallReporter(logs.NewOTelReporter(cfg.loggerName))
```

`common/gopkg/otel` 不再直接 import `log/slog` 或 `otelslog` 来创建 reporter。

### OTel scope name 模型

`otelslog.NewHandler(name)` 的 `name` 会成为 OTel instrumentation scope name，即 SigNoz 中可见的 `scope.name`。

因此 `common/gopkg/otel` 增加配置项：

```go
func WithLoggerName(name string) Option
```

服务注册 OTel component 时传服务包名，而不是 `cmd` 入口包名：

```go
otel.Component(otel.WithLoggerName("dominion/projects/game/gateway"))
otel.Component(otel.WithLoggerName("dominion/projects/game/session"))
otel.Component(otel.WithLoggerName("dominion/projects/infra/deploy"))
```

默认值建议为 `dominion/common/gopkg/otel`，用于未显式配置的实验代码或旧调用点；正式服务应显式传服务包名。

## 代码分层

### `common/gopkg/logs`

职责：

* 提供 `Debug` / `Info` / `Warn` / `Error` 日志门面。
* 读取并解析 `LOG_LEVEL`。
* 创建 console handler 和 OTel reporter handler。
* 实现 slog handler 级别过滤，保证 console 与 OTel reporter 使用一致的级别语义。
* 安装 / 卸载 active reporter。

不负责：

* 判断当前 deploy environment type。
* 初始化 OTel provider、exporter 或 global LoggerProvider。
* 修改用户 artifact env。

### `common/gopkg/otel`

职责：

* 初始化 trace、metric、log provider。
* 设置 OTel global LoggerProvider。
* 接收 logger name 配置。
* 调用 `logs.NewOTelReporter(cfg.loggerName)` 并安装 reporter。

不负责：

* 解析 `LOG_LEVEL`。
* 直接构造 `otelslog.NewHandler`。
* 决定 dev / test / prod 默认日志级别。

### `projects/infra/deploy/runtime/k8s`

职责：

* 从 deploy domain environment type 映射默认日志级别。
* 为 Deployment / StatefulSet workload 注入默认 `LOG_LEVEL`。
* 在用户 env 已配置 `LOG_LEVEL` 时保留用户配置。

不负责：

* 校验 `LOG_LEVEL` 的值是否合法。
* 将 `LOG_LEVEL` 设为保留变量。
* 控制 OTel reporter 的实际过滤行为。

## 关键细节

### 级别过滤 handler

`otelslog.NewHandler` 本身没有 `WithLevel` 选项。OTel SDK `BatchProcessor.Enabled` 会处理所有记录，因此需要在 `logs` 包内包装一个标准 `slog.Handler`：

```go
type levelHandler struct {
    inner slog.Handler
    level slog.Leveler
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
    return level >= h.level.Level() && h.inner.Enabled(ctx, level)
}
```

`Handle`、`WithAttrs`、`WithGroup` 只委托给 inner，并保留同一个 level。

`Enabled` 同时检查本地最小级别和 inner handler 自身的 enabled 状态，避免绕过 OTel handler 的内部判断。

### console 与 reporter 使用同一套级别解析

默认 console handler 从：

```go
slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
```

调整为：

```go
slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevelFromEnv()})
```

OTel reporter 也使用 `logLevelFromEnv()` 创建 level wrapper。这样 deploy 环境和 non-deploy 环境的 `LOG_LEVEL` 行为一致。

### 用户覆盖默认 LOG_LEVEL

当前 artifact env 已支持 `LOG_LEVEL`，例如已有测试覆盖用户 env 排序场景。新增平台默认值时，必须避免覆盖用户配置。

建议实现 helper：

```go
func appendDefaultLogLevel(envs []corev1.EnvVar, env map[string]string, envType domain.EnvironmentType) []corev1.EnvVar
```

或在构造 `containerEnv` 前处理 map：

```go
containerEnv := buildSortedUserEnv(workload.Env)
if _, ok := workload.Env[envLogLevel]; !ok {
    containerEnv = append(containerEnv, corev1.EnvVar{Name: envLogLevel, Value: defaultLogLevel(workload.EnvType)})
}
```

注意：`workload.Env` 可能为 nil。

默认 `LOG_LEVEL` 属于平台补充 env，排序位置建议放在用户 env 之后、`SERVICE_APP` 等平台运行时 env 之前，保持已有“用户 env 在前，平台 env 在后”的模型。

### Deployment / StatefulSet 都需要环境类型

目前 `HTTPRouteWorkload` 已携带 `EnvType` 用于 dev / test header route，但 `DeploymentWorkload` 和 `StatefulWorkload` 没有 `EnvType`。本方案需要把 `env.Type()` 从 `ConvertToWorkloads` 传入两类 workload：

```go
DeploymentWorkload{EnvType: envType, ...}
StatefulWorkload{EnvType: envType, ...}
```

这样 default `LOG_LEVEL` 的判断仍基于 deploy 控制面的类型模型，而不是运行时环境名称。

### 服务包名列表

当前正式服务入口建议配置：

| 服务 | logger name |
| --- | --- |
| game gateway | `dominion/projects/game/gateway` |
| game session | `dominion/projects/game/session` |
| infra deploy | `dominion/projects/infra/deploy` |

实验目录可暂不迁移，或使用默认 logger name；如果实验服务需要在 SigNoz 中定位，再单独显式配置。

## 决策详情

### 决策 1：使用 `LOG_LEVEL` 而不是 `DOMINION_ENVIRONMENT_TYPE`

选择：deploy runtime 直接按 environment type 注入默认 `LOG_LEVEL`。

原因：

* `logs` 包只需要日志级别，不需要理解 deploy domain enum。
* 避免把 `projects/infra/deploy/domain.EnvironmentType` 概念传播到所有运行时服务。
* 用户已有通过 artifact env 配置 `LOG_LEVEL` 的需求和测试场景，保留该入口最直接。

### 决策 2：允许用户 env 覆盖平台默认 `LOG_LEVEL`

选择：用户显式配置优先。

原因：

* `LOG_LEVEL` 是运行参数，不是平台身份变量。
* prod 临时排障可能需要显式打开 debug，dev/test 也可能需要降低为 info。
* 这与 artifact env 的“用户运行参数”定位一致。

### 决策 3：OTel scope name 使用服务包名

选择：`WithLoggerName` 传 `dominion/projects/<app>/<service>` 这类服务包名。

原因：

* SigNoz 中显示为 `scope.name` 时，服务包名比 `cmd` 入口包更稳定、更有定位价值。
* `cmd` 入口通常只是组装依赖，不代表日志所属业务服务。
* 不再使用 `dominion/common/gopkg/logs`，避免所有服务日志在 scope 上混到公共库名下。

### 决策 4：非法 `LOG_LEVEL` 回退 info

选择：不 panic，不返回初始化错误，回退 `info`。

原因：

* 日志系统是基础设施，配置错误不应导致服务无法启动。
* `info` 是生产安全默认值。
* 如需暴露配置错误，可未来增加一次性 warn 日志，但本方案不扩大行为。

## 实施计划

### Phase 1：logs 包级别解析与 reporter 构造

修改：

* `common/gopkg/logs/logs.go`
* `common/gopkg/logs/logs_test.go`
* `common/gopkg/logs/BUILD.bazel`

内容：

* 新增 `envLogLevel = "LOG_LEVEL"`。
* 新增 `logLevelFromEnv()`。
* 默认 console handler 使用 `logLevelFromEnv()`。
* 新增 `NewOTelReporter(name string) *slog.Logger`。
* 新增 level handler 包装 `otelslog.NewHandler(name)`。
* `BUILD.bazel` 将 `@io_opentelemetry_go_contrib_bridges_otelslog//:otelslog` 依赖迁移到 logs 包。

验收：

* `LOG_LEVEL=debug` 时 `logs.Debug` 输出。
* `LOG_LEVEL=info`、空值、非法值时 `logs.Debug` 被过滤。
* `NewOTelReporter` 使用传入 name 构造 reporter，并按 level 过滤。

### Phase 2：otel 包 logger name 配置与 reporter 调用收敛

修改：

* `common/gopkg/otel/options.go`
* `common/gopkg/otel/otel.go`
* `common/gopkg/otel/otel_test.go`
* `common/gopkg/otel/BUILD.bazel`

内容：

* `config` 增加 `loggerName string`。
* `defaultConfig()` 设置默认 logger name。
* 新增 `WithLoggerName(name string) Option`。
* `initDeploy` 改为 `logs.InstallReporter(logs.NewOTelReporter(cfg.loggerName))`。
* 移除 `otel` 包对 `otelslog` 和 `log/slog` reporter 构造的依赖。

验收：

* deploy 初始化成功后 reporter 仍被安装。
* `WithLoggerName` 能影响 reporter 构造入参。
* `common/gopkg/otel` 不再依赖 `otelslog` BUILD target。

### Phase 3：deploy runtime 注入默认 LOG_LEVEL

修改：

* `projects/infra/deploy/runtime/k8s/model.go`
* `projects/infra/deploy/runtime/k8s/converter.go`
* `projects/infra/deploy/runtime/k8s/builder.go`
* `projects/infra/deploy/runtime/k8s/builder_test.go`
* `projects/infra/deploy/runtime/k8s/converter_test.go`

内容：

* `DeploymentWorkload`、`StatefulWorkload` 增加 `EnvType domain.EnvironmentType`。
* `ConvertToWorkloads` 将 `env.Type()` 传入两类 workload。
* `BuildDeployment`、`BuildStatefulSet` 注入默认 `LOG_LEVEL`。
* 用户 env 已配置 `LOG_LEVEL` 时不覆盖。

验收：

* prod Deployment / StatefulSet 默认含 `LOG_LEVEL=info`。
* test / dev Deployment / StatefulSet 默认含 `LOG_LEVEL=debug`。
* 用户配置 `LOG_LEVEL` 时保留用户值。
* env 顺序稳定：用户 env、默认 `LOG_LEVEL`（仅未配置时）、平台身份 env、TLS / OSS env。

### Phase 4：服务入口配置 scope name

修改：

* `projects/game/gateway/app/cmd/main.go`
* `projects/game/session/app/cmd/main.go`
* `projects/infra/deploy/app/cmd/main.go`

内容：

* `otel.Component()` 改为传入服务包名。

示例：

```go
bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/gateway")))
```

验收：

* 服务启动后 OTel logs reporter 的 scope name 来自服务包名。

## 测试计划

### 单元测试

建议新增或调整：

* `common/gopkg/logs`
  * `TestDebug_WithDebugLevelEnv`
  * `TestDebug_WithInfoLevelEnv`
  * `TestDebug_WithInvalidLevelEnv`
  * `TestNewOTelReporter_LevelFiltering`
* `common/gopkg/otel`
  * `TestOptions_WithLoggerName`
  * `TestInit_Deploy_InstallsReporterWithConfiguredLoggerName`
* `projects/infra/deploy/runtime/k8s`
  * `TestBuildDeployment_DefaultLogLevelByEnvType`
  * `TestBuildDeployment_UserLogLevelOverridesDefault`
  * `TestBuildStatefulSet_DefaultLogLevelByEnvType`
  * `TestBuildStatefulSet_UserLogLevelOverridesDefault`
  * `TestConvertToWorkloads_DeploymentAndStatefulEnvTypePassthrough`

### Bazel 验证

最小验证：

```bash
bazel test //common/gopkg/logs:logs_test //common/gopkg/otel:otel_test //projects/infra/deploy/runtime/k8s:all
```

最终验证：

```bash
bazel test //...
```

### 手动 QA

服务级行为可通过以下方式验收：

1. 构造 dev 或 test 类型环境，未配置 `LOG_LEVEL`，部署后确认 Pod env 中为 `LOG_LEVEL=debug`。
2. 构造 prod 类型环境，未配置 `LOG_LEVEL`，部署后确认 Pod env 中为 `LOG_LEVEL=info`。
3. 在 prod artifact env 中显式配置 `LOG_LEVEL=debug`，部署后确认 Pod env 保留用户值。
4. 服务发出一条 `logs.Debug`，在 SigNoz 中确认 dev/test 可见、默认 prod 不可见。
5. 在 SigNoz log detail 中确认 `scope.name` 为服务包名，例如 `dominion/projects/game/gateway`。

## 未来规划

* 如后续需要支持 `warn` / `error` 运行时过滤，可扩展 `logLevelFromEnv()`，但应单独评估对现有 debug/info 语义的影响。
* 如需要运行中动态调级，应由控制面或配置中心设计独立机制，不应复用进程启动时的 `LOG_LEVEL` 环境变量。
* 如 SigNoz 侧需要更细粒度定位到子包，可在业务日志字段规范中另行设计 `component` 或 `module` 字段，而不是把 `scope.name` 改成每个调用点动态变化。
