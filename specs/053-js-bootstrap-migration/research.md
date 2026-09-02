# Research: JS bootstrap 组件与 experimental 目录统一为 js

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-01

依据：`common/gopkg/bootstrap/`（Go 参照实现全量阅读）、`style/javascript.md`、`specs/048-js-esm-migration/contracts/{esm-package-conventions,otel-instrumentation-esm-contract}.md`、`specs/052-deploy-health-probe/contracts/{bootstrap-health,verification-testplan}.md`、`specs/005-js-runtime-idioms/{research.md,contracts/logs-api.md}`、`common/js/{logs,otel,resolver}` 源码（仓库 JS API 风格样本）、`experimental/ts/*` 全部源码与 testplan。

用户方案指令（本文件所有决策的顶层约束）：**"迁移 bootstrap 注意遵循 js 风格和规范，不要过度迁移 golang 风格"**。

## D1: 包落位与命名 — `common/js/bootstrap` / `@dominion/common-js-bootstrap`

**Decision**: 公共包落位 `common/js/bootstrap`，包名 `@dominion/common-js-bootstrap`，BUILD 形态复制 `common/js/logs/BUILD.bazel` 三件套（`ts_project :lib` + `js_runtime_library :runtime_pkg` + `vitest_test :lib_test`）。

**Rationale**: 与 `common/js/{config,logs,otel,resolver}` 同层同构；`js_runtime_library` 是 `artifact_pkg_js` 运行时闭包发现的必要 provider（`tools/release/js_runtime_library.bzl` 文档）；catalog 依赖管理遵循 `pnpm-workspace.yaml` 约定。

**Alternatives considered**:
- 嵌套 `common/js/grpc/bootstrap`：混淆"应用 bootstrap"与"gRPC 域"，弃。
- 拆多包（core + grpc 适配器包）：包样板 ×2 且仓库无此先例，D2 已论证单包可行，弃。

## D2: gRPC 适配器的 grpc-js 隔离 — 单包 + 仅 `import type`

**Decision**: gRPC server/client 适配器与核心同包，适配器对 `@grpc/grpc-js` **仅使用 `import type`**（类型编译期擦除）；`@grpc/grpc-js` 只进 `devDependencies` 与 `:lib` 的 bazel 类型依赖，**不进** `package.json` dependencies、不进 `:runtime_pkg` 的 `npm_deps`。包运行时静态图为 grpc-js-free。

**Rationale**: `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` §1/§5 要求服务 bootstrap 静态图 MUST NOT 含 `@grpc/grpc-js`（IITM hook 在 `init()` 内注册，早于 server 动态导入）。共享 bootstrap 包位于每个服务的 bootstrap 静态图中——只要包运行时不加载 grpc-js，服务侧"init → 动态 import server.js（内部才加载 grpc-js）"的时序对被插桩包依然成立。`import type` 是 TS 标准能力（`style/javascript.md` 要求类型再导出显式 `export type`，同一机制），适配器只调用传入实例的方法（`bindAsync`/`tryShutdown`/`forceShutdown`/`close`），无任何 grpc-js 运行时取值。

**Alternatives considered**:
- subpath export（`@dominion/common-js-bootstrap/grpc`，由 server 侧动态图导入）：结构隔离更硬，但仓库零先例（所有包仅 `.` 导出），bazel `npm_link_all_packages` + tsc exports-map 解析引入新接线风险；且约束已在 048 契约 §5 覆盖（"bootstrap 静态图 MUST NOT 含 grpc-js"按传递闭包解释），tasks 阶段在 053 契约中显式登记该约束并加入审计命令（`contracts/bootstrap-js-api.md` §7），弃。
- 适配器自定义结构化最小接口（duck-typing，零依赖）：无 devDep 但类型表达力差、与真实 grpc-js API 漂移不可见，弃。

## D3: API 形态 — JS 惯用语对照表（用户指令核心落点）

**Decision**: 行为语义对齐 Go bootstrap，API 形态按 JS 惯用语重写：

| Go（`common/gopkg/bootstrap/`） | JS（`@dominion/common-js-bootstrap`） | 依据 |
|---|---|---|
| `Component{Name() Stage() Start(ctx) error Stop(ctx) error}` 接口方法 | `Component{name stage start(signal) stop(signal)}` plain-object 属性 + async 方法，失败 throw/reject | 仓库 JS 风格（plain objects、工厂函数），`specs/005-js-runtime-idioms/research.md`"Go 事件构造器/哨兵是 Go 惯用语，JS 用 plain objects"先例 |
| `context.Context` 取消传播 | `AbortSignal` / `AbortController` | Node 原生异步取消标准（https://developer.mozilla.org/docs/Web/API/AbortSignal ） |
| 返回 `error` 值 | `throw` / Promise rejection | JS 异常惯例 |
| `errors.Join` 错误聚合 | `AggregateError`（ES2021，target ES2020 之上可用） | https://developer.mozilla.org/docs/Web/JavaScript/Reference/Global_Objects/AggregateError |
| 变长 `Option` 函数（`WithShutdownTimeout`） | 单个 options object（`{ shutdownTimeoutMs }`） | 仓库 `createResolver(config)` / `init(config)` 风格 |
| `Stage` int 常量 + `String()` | `const Stage = { Foundation: 100, Client: 200, Daemon: 250, Server: 300 } as const` + 字面量联合类型（保留数值以对齐 Go 排序语义） | 数字排序语义跨语言一致；`as const` 对象是 TS 标准枚举替代 |
| `DaemonDecision` int 枚举 | `"restart" \| "stop" \| "fatal"` 字符串字面量联合 | TS 惯用联合类型 |
| `exitWatcher.Done() <-chan error` | 组件可选属性 `exited?: Promise<Error \| undefined>`，bootstrap `Promise.race` 汇聚 | channel → Promise 的标准映射 |
| `signal.NotifyContext` | `process.on("SIGTERM"/"SIGINT")` + AbortController；`run(options)` 可注入外部 AbortSignal/信号列表 | 注入 signal 使单测无需真实进程信号（Go 测试经 `syscall` 通道，JS 注入更简） |
| panic recover（Stop/Build/Start） | `try/catch`（async 函数内 await 天然可捕获） | 语言原生 |
| `New() *Bootstrap` | `new Bootstrap(options)` 或 `createBootstrap(options)`（以 tasks 实现时自然形态定，契约只约束调用面） | 两者均合仓库风格（`class Logger` 直出 vs `createResolver` 工厂） |

**Rationale**: 用户指令 + 仓库既有先例（005 已因 Go 风格移除 `logs/event` 包）。"对齐"指**行为语义**（顺序、回滚、预算、FIFO、失败分类），非 API 形状复刻。

**Alternatives considered**: 逐方法复刻 Go 签名（返回 error 对象、方法式接口）——与指令直接冲突，弃。

## D4: OTel 生命周期承载 — 服务入口胶水（非组件化）

**Decision**: `init()`（含 IITM hook 注册）与 `shutdown()` 保持在服务入口（bootstrap.ts 胶水）：

```text
服务 bootstrap.ts（终态形态）
├── 静态导入：common-js-otel / common-js-logs / common-js-bootstrap（均 grpc-js-free）
└── main()
    ├── await init({ instrumentations })            // 两段式契约第一步
    ├── installReporter(createOTelReporter(...))
    ├── const { buildServer } = await import("./server.js")   // 动态导入（契约第二步）
    ├── new Bootstrap(...)、register(...)、run()
    └── await run 完成后：uninstallReporter() → await shutdown() → process.exit
```

**Rationale**: server 实例必须先于组件 Start 存在（组件持有已构造的 Server），而 server.js 的加载又必须晚于 `init()`——这强制"入口先 init 再动态导入"的两段式次序（`otel-instrumentation-esm-contract.md` §2），该契约同时规定此形态 MUST 保持。Go 的 `otel.Component()`（Foundation 组件）模式在此处不可移植：组件 Start 发生在 server.js 导入**之后**（需要实例），时序不成立。OTel shutdown 后置于全部组件停止之后，停机期日志仍可上报，语义更优。

**Alternatives considered**:
- grpc 组件接受异步工厂（`() => Promise<Server>`），otelize Foundation 组件化、Server-stage Start 内动态导入 server.js：组件模型完全统一（Go 心智），但适配器 API 因工厂间接性复杂化、与"不过度迁移 golang 风格"指令相悖，弃（记录于 `contracts/bootstrap-js-api.md` §6）。

## D5: 健康端点实现与测试缝隙 — DI 工厂注入（@internal）

**Decision**: `health.ts` 内部实现 38080/healthz（`node:http`，监听所有接口，200 `ok\n`、他路径 404）；`BootstrapOptions.healthServerFactory?: () => HealthService` 作为 **@internal DI 缝隙**（生产代码 MUST NOT 传），单测注入 `vi.fn()` 桩替代真实端口绑定——遵循 `style/javascript.md` Mock 约定（"生产代码以参数接收协作者，测试传入 vi.fn()"，仓库 blessed 模式）。真实端点行为（200/404/停机释放端口）由健康模块自身的单测以真实 38080 绑定验证（与 Go `health_test.go` 同策略；单文件内串行无冲突）。

**Rationale**: Go 侧顺序测试经 `newHealthServer` 包变量注桩（`common/gopkg/bootstrap/bootstrap_test.go:248`）；JS 的等价惯用语是构造器参数注入而非模块可变状态（ESM live binding 对导入方只读，setter 导出属反模式）。

**Alternatives considered**:
- 全部测试真实绑定 38080：vitest 多测试文件并行 worker，编排测试与健康测试并行时 EADDRINUSE 假红，弃。
- 模块级 setter（`setHealthServerFactoryForTest`）：ESM 导入只读，需导出 setter 破坏封装，弃。

## D6: 自愈钩子缝隙 — `bootstrap.health` 只读句柄

**Decision**: Bootstrap 实例暴露 `get health(): HealthHandle | undefined`（启动前/停止后为 undefined；`HealthHandle = { stop(): Promise<void> }`）。grpc_hello_world 的 `HEALTH_STOP_AFTER_MS` 钩子（FR-014，spec Assumption）在服务入口 `setTimeout(() => { void bootstrap.health?.stop(); }, delay)` 实现故障注入；钩子逻辑仅存在于实验服务。

**Rationale**: FR-002 健康端点为 bootstrap 内部自动行为（052 契约"无配置项无开关"），服务侧无法经配置关闭；但自愈大型测试（`specs/052-deploy-health-probe/contracts/verification-testplan.md` §2）需要"进程存活而健康端点失效"的注入能力，唯一最小面就是受控 stop 句柄。不加开关、不加事件系统（EventEmitter 对单一用途过度）。

**Alternatives considered**:
- `run()` 返回 handle：`run()` 语义是"运行至退出"的阻塞 Promise，混入返回值破坏 `await run()` 直觉，弃。
- 可注入 health 实现（服务自造可停的 health）：与 052"health 由 bootstrap 自动提供、实现收敛"冲突，弃。

## D7: HTTP/gRPC 适配器形态与 JS/Go 差异

**Decision**:
- `createHttpServerComponent(name, server, { port, host? })`：start 执行 `listen`（`error` 事件 → reject，EADDRINUSE 即启动失败）；stop 两阶段——`close()` 等待 keep-alive 排空，关停预算 abort 触发 `closeAllConnections()`（Node ≥18.2，https://nodejs.org/api/http.html#servercloseallconnections ）。暴露 `exited`（意外 `close`/`error` → resolve 错误）。
- `createGrpcServerComponent(name, { server, address, credentials })`：start 执行 `bindAsync + start`；stop 两阶段——`tryShutdown` 与预算 abort 竞速，超时 `forceShutdown`（对齐 Go GRPCServer 的 GracefulStop→Stop 两阶段）。
- `createGrpcConnComponent(name, client)`：start no-op、stop `client.close()`，Stage=Client（对齐 Go GRPCConn）。

**JS/Go 差异（终态记录，非缺陷）**：grpc-js 的 Server 无 serve-loop 返回值，`bindAsync` 成功后不存在"Serve 返回错误"的意外退出信号——gRPC server 适配器**不提供 `exited`**；意外退出监测由 HTTP server 适配器与 Daemon 承载。Go 侧 `httpServerComponent` 监听 `ListenAndServe` 返回错误的语义在 JS 侧由 `exited` Promise 等价承接。

**Rationale**: 各适配器仅调用传入实例的方法 + `import type` 类型（D2）；grpc-js API（https://grpc.github.io/grpc-node/grpc-js/classes/Server.html ）的 bindAsync/tryShutdown/forceShutdown 即对应 Go listener+Serve/GracefulStop/Stop 的最小映射。

## D8: Daemon 监督器形态

**Decision**: `createDaemon(name, buildWorker, options?)` 返回 Stage=Daemon 的组件；`Worker = { run(signal): Promise<void> }`（run 阻塞至退出/取消——对齐 Go Worker 的阻塞 Start，取消经 signal 而非单独 Stop 方法）；`DaemonOptions = { initialBackoffMs?, maxBackoffMs?, maxRestarts?, classifyError? }`，默认值对齐 Go（1s/30s/5，负 maxRestarts 禁用上限）；致命错误经组件 `exited` promise 上报 → bootstrap 全局关停。默认错误分类：signal 已取消的 AbortError → `"stop"`；worker 正常完成（无错）→ `"stop"`；其余 → `"restart"`（对齐 `common/gopkg/bootstrap/daemon.go` defaultErrorClassifier 语义）。

**Rationale**: 指数退避 `min(cur*2, max)`、重启上限耗尽→致命、分类器可插拔均与 Go 行为一致；字符串决策联合是 D3 的枚举映射。

## D9: 编排核心语义（与 Go `RunSignal` 逐条对齐）

**Decision**: `bootstrap.run(options?)`：
1. 注册期拒绝（重名 / run 后再 register）→ throw；
2. 组件快照后按 `stage` 升序、`name` 升序稳定排序，依次 `await start(signal)`——任一失败：逆序回滚已启动组件（错误聚合为 `AggregateError`）并 reject；
3. 全部成功 → 启动 health（失败 = 组件启动失败语义，回滚 + reject，FR-004）；
4. 等待退出触发：SIGTERM/SIGINT（可注入外部 AbortSignal）或 任一组件 `exited` 带 error；
5. 停止序列：health 首位 → 组件逆序；**统一关停预算**（默认 5000ms，`shutdownTimeoutMs` 可配）：一个共享 AbortSignal（超时 abort）传入每个 stop；预算内未完成按失败聚合；
6. 干净信号退出 resolve（undefined），其余 reject（含聚合停止错误）；进程退出码由服务入口依据 run 结果决定。

**Rationale**: 与 `common/gopkg/bootstrap/bootstrap.go` RunSignal 步骤 1-9 及 052 契约 `bootstrap-health.md` §1 逐条对应；`sort` 稳定（ES2019+ 规范保证，https://developer.mozilla.org/docs/Web/JavaScript/Reference/Global_Objects/Array/sort ）。

## D10: 迁移机制 — git mv 与引用同步清单

**Decision**: 两段提交：①`git mv experimental/ts/{grpc_hello_world,hello_world,team_graph_spike} experimental/js/`（纯移动，便于历史追溯）；②内容更名（标识符 + 引用 + 接入改造）。完整 old→new 映射（权威版本在 `contracts/migration-rename-map.md`）覆盖：目录、proto 包名 `experimental.ts.grpc_hello_world`→`experimental.js.grpc_hello_world`、HTTP 路径前缀 `/experimental/ts/grpc-hello-world`→`/experimental/js/grpc-hello-world`、Go importpath `dominion/experimental/ts/grpc_hello_world`→`dominion/experimental/js/grpc_hello_world`、**服务工件名 `grpc-hello-world-ts`→`grpc-hello-world-js`**（service.yaml `app` 字段 ×2、BUILD `app` 属性 ×2、gateway solver URI、OTel reporter 名、smoke_test.sh）、`pnpm-workspace.yaml`（移除 `experimental/ts/*`）、`.bazelignore`（3 条 node_modules 路径）、`tools/dev/js/BUILD.bazel`（`@npm//experimental/ts/grpc_hello_world` proto-loader label）、server.ts 内 proto 运行时路径与生成类型导入路径、testplan yamls/go 路径前缀、`projects/game/agent` ×2 与 `experimental/js/vite_react_demo` ×2 注释、team_graph_spike `FINDINGS.md`。

**Rationale**: `grpc-hello-world-ts` 属 FR-009"所有携带 ts 的标识符"（出现在 solver URI、部署 app 名、reporter service name——均为标识符而非数据值）；service.yaml 的 greeting 配置值（`"hello from ts config"`）与 `GREETING_SUFFIX: ts-suffix` 是**数据值**非标识符，不改（interface_test.go 断言依赖这些值）。移动后需 `pnpm` 刷新 lockfile（workspace 路径变化）+ `bazel run //:gazelle`（BUILD 路径）+ `bazel mod tidy`。

**Alternatives considered**: 目录移动与更名合并单次提交——git rename 检测在内容同变时退化，分开提交保历史可读（spec Assumption 亦如此约定）。

## D11: 验证策略

**Decision**:
- 单测（每次变更必跑，原则 IV）：bootstrap 包 `:lib_test`（编排顺序/回滚/预算/FIFO/daemon 分类与退避/适配器，DI mock）；迁移后三子项目既有 target（spike.test.ts、smoke_test 等）。
- 大型测试（原则 VI，单独验收 task）：`guitar run experimental/js/grpc_hello_world/testplan/interface_test.yaml`——default suite 证明接口契约 + 配置语义不回归，selfheal suite 证明探针就绪 + 自愈（路径已更名，验证的就是新路径）。team_graph_spike 无 testplan（未部署对象），其接入验证 = 构建 + 单测 + 本地启动冒烟（quickstart §4）。
- 静态审计：`contracts/migration-rename-map.md` §3 的 rg 命令集（`experimental/ts`、`experimental\.ts\.`、`grpc-hello-world-ts` 零命中，历史 spec 与 `specs/` 除外）。

**Rationale**: 大型测试复用既有计划（`style/large_test.md`"每个被测系统只维护一份测试计划 YAML"，新验证作为既有 suite 的路径更名延续，不新建计划）。

## 汇总：对 Go bootstrap 的"故意不对齐"清单（终态）

| 项 | Go | JS | 原因 |
|---|---|---|---|
| API 载体 | 接口方法 + error 返回 | plain object + async/throw | D3 用户指令 |
| 取消机制 | context.Context | AbortSignal | D3 |
| otel 生命周期 | `otel.Component()` Foundation 组件 | 服务入口胶水（init 前置、shutdown 后置） | D4，OTel loader-hook 契约强制 |
| grpc server 意外退出 | Serve 返回错误 → exitWatcher | 不可观测（grpc-js 无 serve-loop），适配器无 `exited` | D7，运行时能力差异 |
| http 停机排空 | `Shutdown(ctx)` 内建 | `close()` + 预算后 `closeAllConnections()` | D7 Node API 形态 |
| 健康桩注入 | 包变量 `newHealthServer` | `options.healthServerFactory`（@internal DI） | D5 仓库 mock 规范 |
