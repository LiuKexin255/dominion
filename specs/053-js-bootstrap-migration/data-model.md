# Data Model: JS bootstrap 组件与 experimental 目录统一为 js

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-01

实体分为两组：**公共包 API 实体**（`@dominion/common-js-bootstrap` 对外契约的形状，接口签名细节见 [contracts/bootstrap-js-api.md](contracts/bootstrap-js-api.md)）与**迁移映射实体**（old→new 标识符权威映射，完整清单见 [contracts/migration-rename-map.md](contracts/migration-rename-map.md)）。本特性无持久化数据。

## 1. Component（组件）

| 字段 | 类型 | 约束 |
|--------|--------|------|
| `name` | `string`（readonly） | 同一 Bootstrap 实例内唯一；重复注册 → throw |
| `stage` | `Stage`（readonly） | 启动排序第一键（升序） |
| `start(signal)` | `(signal: AbortSignal) => Promise<void>` | 失败以 rejection 表达；抛出即触发回滚 |
| `exited` | `Promise<Error \| undefined>`（可选） | 意外退出监测信号；未实现者不参与退出监听。HTTP server 适配器与 Daemon 提供；gRPC server 适配器不提供（research.md D7） |

排序规则：`stage` 升序，同 stage 按 `name` 升序（稳定排序）。停止恒为启动的严格逆序。

## 2. Stage（生命周期阶段）

| 常量 | 值 | 语义 |
|--------|-----|------|
| `Foundation` | 100 | 基础层（日志、配置、指标） |
| `Client` | 200 | 客户端连接层（DB、缓存、gRPC client） |
| `Daemon` | 250 | 后台任务层 |
| `Server` | 300 | 服务监听层（HTTP、gRPC server） |

类型形态：`const Stage = { ... } as const` + 字面量联合类型；数值与 Go `common/gopkg/bootstrap/stage.go` 一致（跨语言排序语义对齐）。

## 3. Bootstrap（编排器）

**配置（`BootstrapOptions`，单 options object）**：

| 字段 | 类型 | 默认 | 约束 |
|--------|------|------|------|
| `shutdownTimeoutMs` | `number` | `5000` | 统一关停预算（health + 全部组件共享一个超时 AbortSignal）；与 Go `WithShutdownTimeout` 默认 5s 对齐 |
| `healthServerFactory` | `() => HealthService` | 内置 38080 实现 | **@internal** 测试缝隙：生产代码 MUST NOT 传递（research.md D5） |

**运行（`run(options?)`）**：可选注入 `signal`（外部 AbortSignal）与 `signals`（默认 `["SIGTERM", "SIGINT"]`）。

**状态机**（run 生命周期，单次使用；重复 run → throw，对齐 Go）：

```text
created ──run()──▶ starting ──全部组件 start 成功──▶ running ──退出触发──▶ stopping ──停止序列完成──▶ stopped
                     │                                            │
                     │ 组件/health 启动失败                        │ 预算内未完成的 stop
                     ▼                                            ▼
                 rolling-back ──逆序停止已启动组件──▶ failed    stopped（错误聚合于 run 的 rejection）
```

- `created`/`starting`/`running` 期间 `register`：仅 `created` 允许（running 后注册 → throw；对齐 Go "Run 后不可注册"）。
- `health` 句柄：`running` 期间可访问（`HealthHandle.stop()` 幂等），`stopped` 后为 undefined（research.md D6）。

## 4. HealthService / HealthHandle（健康端点）

| 实体 | 字段/方法 | 约束 |
|--------|--------|------|
| 内置实现 | `:38080` 全接口监听、`GET /healthz` → 200 `ok\n`、他路径 404 | specs/052-deploy-health-probe/contracts/bootstrap-health.md §1；无配置项、无开关、无端口校验 |
| 生命周期 | 所有组件启动成功后启动；停止序列首位（先进先出） | 回滚场景下从未启动，无残留监听 |
| `HealthHandle` | `{ stop(): Promise<void> }` | 唯一公开缝隙：自愈故障注入（FR-014）；不提供 start/restart |

## 5. Daemon（监督器）

| 实体 | 字段 | 约束 |
|--------|--------|------|
| `Worker` | `run(signal): Promise<void>` | 阻塞至正常完成或取消；失败 = rejection |
| `buildWorker` | `(signal) => Worker \| Promise<Worker>` | 每次启动/重启前调用（对齐 Go WorkerBuilder.Build） |
| `DaemonOptions.initialBackoffMs` | 默认 1000 | 指数退避起点 |
| `DaemonOptions.maxBackoffMs` | 默认 30000 | 退避上限（`min(cur*2, max)`） |
| `DaemonOptions.maxRestarts` | 默认 5；负数 = 不限 | 耗尽 → 致命（经 `exited` 上报，触发全局关停） |
| `DaemonOptions.classifyError` | `(err) => "restart" \| "stop" \| "fatal"` | 默认分类：signal 取消的 AbortError → `"stop"`；无错正常完成 → `"stop"`；其余 → `"restart"`（对齐 Go defaultErrorClassifier） |
| 产出 | `Component`（stage=Daemon，name=入参） | 经 `exited` 参与意外退出监测 |

## 6. 适配器（既有对象 → Component）

| 工厂 | 输入 | stage | start | stop | exited |
|--------|--------|--------|--------|--------|--------|
| `createHttpServerComponent` | `name, http.Server, { port, host? }` | Server | `listen`（错误事件 → reject） | `close()` 排空，预算 abort → `closeAllConnections()` | ✅ 意外 close/error |
| `createGrpcServerComponent` | `name, { server, address, credentials }` | Server | `bindAsync` + `start` | `tryShutdown` 竞速预算 → `forceShutdown` | ❌（grpc-js 无 serve-loop 退出信号，research.md D7） |
| `createGrpcConnComponent` | `name, grpc.Client` | Client | no-op | `client.close()` | ❌（客户端无退出语义） |

gRPC 适配器对 `@grpc/grpc-js` 仅 `import type`（包运行时 grpc-js-free，research.md D2）。

## 7. 服务入口实体（experimental 服务终态形态）

- **grpc_hello_world**：`bootstrap.ts`（OTel init → reporter → 动态 import `buildServer()` → 注册 grpc server 组件 → `run()` → `HEALTH_STOP_AFTER_MS` 钩子经 `bootstrap.health` 注入 → run 完成后 otel `shutdown()` → exit）；`server.ts` 导出 `buildServer()`（proto 加载 + handler 注册 + 返回未 bind 的 Server 与 credentials；不再自行 bind/start）。
- **team_graph_spike**：`bootstrap.ts`（init → reporter → 动态 import server 构建 → 注册 http server 组件 → run → shutdown → exit）；接入后自动获得 38080/healthz（spec Clarification 2026-09-01 第 4 条：预期新行为）。
- **hello_world**：纯目录迁移，无代码形态变化。

## 8. 迁移映射（概要）

完整权威映射与审计命令见 [contracts/migration-rename-map.md](contracts/migration-rename-map.md)。数据不变量：**数据值不改**（service.yaml greeting 值 `"hello from ts config"`、`GREETING_SUFFIX: ts-suffix` 为 interface 断言依赖的测试数据）；**标识符全改**（目录、proto 包名、HTTP 路径、Go importpath、工件名 `grpc-hello-world-ts`→`grpc-hello-world-js`、构建/工作区/忽略清单/注释中的路径）。
