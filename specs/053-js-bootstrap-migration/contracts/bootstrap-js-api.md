# Contract: @dominion/common-js-bootstrap 公共 API（bootstrap-js-api）

**Feature**: [spec.md](../spec.md) | **Date**: 2026-09-01 | 依据: [research.md](../research.md) D1-D9

本契约为 `common/js/bootstrap`（`@dominion/common-js-bootstrap`）的**实现前接口定义**（宪法原则 III）。实现 MUST 满足本契约；签名细节（参数顺序等）允许在实现 review 中微调，语义不得偏离。

## 1. 包与形态

- 落位 `common/js/bootstrap`，包名 `@dominion/common-js-bootstrap`，ESM（`"type": "module"`，遵循 `specs/048-js-esm-migration/contracts/esm-package-conventions.md`）。
- 导出面：`index.ts` barrel——值导出（工厂函数、`Stage` 常量对象、`Bootstrap` 类）具名导出；类型再导出 MUST 显式 `export type`。
- BUILD：`ts_project :lib` + `js_runtime_library :runtime_pkg`（`package_name = "@dominion/common-js-bootstrap"`）+ `vitest_test :lib_test`（形态对照 `common/js/logs/BUILD.bazel`）。

## 2. 核心类型

```typescript
/** 生命周期阶段。数值与 Go bootstrap（common/gopkg/bootstrap/stage.go）一致。 */
export const Stage = {
  Foundation: 100,
  Client: 200,
  Daemon: 250,
  Server: 300,
} as const;
export type Stage = (typeof Stage)[keyof typeof Stage];

/** bootstrap 管理的基本单元。失败以 rejection 表达（throw），不返回 error 值。 */
export interface Component {
  readonly name: string;
  readonly stage: Stage;
  start(signal: AbortSignal): Promise<void>;
  stop(signal: AbortSignal): Promise<void>;
}

/** 可选的意外退出信号：server 类组件意外退出时 resolve 携带 Error；
 *  实现 exitWatcher 语义（research.md D3/D7）。 */
export interface ExitWatchable {
  readonly exited: Promise<Error | undefined>;
}
```

## 3. Bootstrap 编排器

```typescript
export interface BootstrapOptions {
  /** 统一关停预算（毫秒），health 与全部组件停止共享同一超时信号。默认 5000。 */
  shutdownTimeoutMs?: number;
  /** @internal 测试缝隙：替换内置健康端点实现。生产代码 MUST NOT 传递。 */
  healthServerFactory?: () => HealthService;
}

export interface RunOptions {
  /** 外部取消信号（与 SIGTERM/SIGINT 等价触发优雅停止）。 */
  signal?: AbortSignal;
  /** 触发退出的进程信号。默认 ["SIGTERM", "SIGINT"]。 */
  signals?: NodeJS.Signals[];
}

export class Bootstrap {
  constructor(options?: BootstrapOptions);
  /** 注册组件。重名或 run 已开始 → throw（Error，message 含组件名/原因）。 */
  register(component: Component): void;
  /** 运行至退出。干净信号退出 → resolve；启动失败/意外退出/停止失败 → reject（多错为 AggregateError）。再次调用 → throw。 */
  run(options?: RunOptions): Promise<void>;
  /** 健康端点句柄（故障注入缝隙）。未运行/已停止 → undefined。 */
  get health(): HealthHandle | undefined;
}
```

### 3.1 run 编排序列（与 Go `RunSignal` 语义对齐）

```text
1. 组件快照 → stage 升序、name 升序稳定排序 → 依次 await start(signal)
   任一失败 → 逆序回滚已启动组件 → reject(AggregateError[启动错误, ...回滚错误])
2. 启动 health（内部，38080/healthz）
   失败 → 同上回滚语义（spec FR-004）→ reject
3. 等待退出触发：进程信号 | RunOptions.signal abort | 任一 ExitWatchable.exited resolve 携带 Error
4. 停止序列：health 首位 → 组件严格逆序；共享一个超时 AbortSignal（shutdownTimeoutMs）
   预算内未完成的 stop 记为失败并聚合
5. 干净信号退出 → resolve；其余 → reject（触发原因 + 聚合停止错误）
```

## 4. 健康端点（内部）

- 内置实现：`node:http` 监听 `:38080`（全接口——kubelet 经 Pod IP 探测），`GET /healthz` → 200 `ok\n`，其余 404。
- 约束完全继承 `specs/052-deploy-health-probe/contracts/bootstrap-health.md` §1：先进先出生命周期、启动失败视同组件失败、无配置项/开关/端口校验。
- `HealthHandle`：`{ stop(): Promise<void> }`——唯一公开缝隙，供实验服务的 liveness 故障注入（spec FR-014）；不提供 start/restart，不构成健康端点的开关。

## 5. Daemon 监督器

```typescript
export interface Worker {
  /** 阻塞运行至正常完成（resolve）或失败（reject）；signal 取消时应以 AbortError reject 或干净 resolve。 */
  run(signal: AbortSignal): Promise<void>;
}

export type DaemonDecision = "restart" | "stop" | "fatal";

export interface DaemonOptions {
  initialBackoffMs?: number;  // 默认 1000
  maxBackoffMs?: number;      // 默认 30000；退避 = min(cur*2, max)
  maxRestarts?: number;       // 默认 5；负数 = 不限；耗尽 → fatal
  classifyError?: (err: unknown) => DaemonDecision;
}

/** 创建 stage=Daemon 的组件。buildWorker 于每次启动/重启前调用。 */
export function createDaemon(
  name: string,
  buildWorker: (signal: AbortSignal) => Worker | Promise<Worker>,
  options?: DaemonOptions,
): Component & ExitWatchable;
```

默认错误分类（对齐 `common/gopkg/bootstrap/daemon.go` defaultErrorClassifier）：

- worker 无错正常完成 → `"stop"`；
- AbortError 且监督信号已取消 → `"stop"`（关停，不重启、不报致命）；
- 其余错误 → `"restart"`；重启次数耗尽 → fatal（经 `exited` resolve 携带错误 → bootstrap 全局关停）。

## 6. 适配器

```typescript
/** node:http Server → Server-stage 组件。start=listen（错误事件 reject）；stop=close 排空，预算 abort 后 closeAllConnections()。提供 exited。 */
export function createHttpServerComponent(
  name: string,
  server: http.Server,
  options: { port: number; host?: string },
): Component & ExitWatchable;

/** grpc-js Server → Server-stage 组件。start=bindAsync+start；stop=tryShutdown 竞速预算→forceShutdown。不提供 exited（research.md D7：grpc-js 无 serve-loop 退出信号）。 */
export function createGrpcServerComponent(
  name: string,
  options: { server: grpc.Server; address: string; credentials: grpc.ServerCredentials },
): Component;

/** grpc-js Client → Client-stage 组件。start=no-op；stop=client.close()。 */
export function createGrpcConnComponent(
  name: string,
  client: grpc.Client,
): Component;
```

## 7. 运行时依赖隔离（强约束）

- 包运行时依赖仅为 Node 内置模块与 `@dominion/common-js-logs`；**包的任何模块 MUST NOT 对 `@grpc/grpc-js` 产生运行时导入**（仅允许 `import type`）。
- 理由：本包位于每个服务的 bootstrap 静态图中；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` §5 禁止 bootstrap 静态图加载被插桩 CJS 包（IITM hook 于 `init()` 内注册，必须先于 grpc-js 加载）。`@grpc/grpc-js` 仅出现于 devDependencies 与 bazel 类型依赖，禁止列入 `:runtime_pkg` 的 `npm_deps`。
- 审计判据（终态零命中）：

```bash
rg '^\s*import\s+(?!type)' -g '*.ts' common/js/bootstrap/src --pcre2 | rg '@grpc/grpc-js'
rg 'from "@grpc/grpc-js"' -g '*.ts' common/js/bootstrap/src | rg -v 'import type|export type'
```

## 8. 服务接入形态（两段式保持）

服务入口（bootstrap.ts）静态图仅含 `@dominion/common-js-{otel,logs,bootstrap}`；次序固定：`await init({ instrumentations })` → `installReporter(...)` → `await import("./server.js")` 构建组件 → `register` → `run()` → run 完成后 `uninstallReporter()` + `await shutdown()` → `process.exit`。OTel 生命周期不组件化（research.md D4：server 实例必须晚于 init 产生、早于组件 Start 注入，时序与组件模型互斥）。
