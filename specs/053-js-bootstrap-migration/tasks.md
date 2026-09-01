# Tasks: JS bootstrap 组件与 experimental 目录统一为 js

**Input**: Design documents from `specs/053-js-bootstrap-migration/`（plan.md / research.md / data-model.md / contracts/ / quickstart.md）

**Prerequisites**: plan.md（required）、spec.md（required for user stories）、research.md、data-model.md、contracts/

**Global Rules**（适用于所有 phase，不再逐 task 重复）：

- **原则 IV**：每个代码变更 task 自带 `bazel build`（相关 target）+ `bazel test`（相关 target）验证，作为任务的一部分，不单列 test task；phase 门禁为该 phase 汇总验证。
- **原则 V**：每个 phase 开始前 MUST 完整阅读该 phase 声明的全部文档（三分类清单见各 phase）；AGENTS.md 与 spec 文件为必读背景，不重复列出。
- **原则 I / VII**：代码注释引用契约时使用仓库相对路径（如 `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §7`）；交付物只表述终态。
- 提交策略：迁移 phase（Phase 1）目录移动与内容更名分开提交（research.md D10）；其余按 task 或逻辑分组提交。
- Go 代码门禁（Phase 1 涉及 `interface_test.go`/`health_test.go`/`gateway/main.go` 编辑）：`bazel run //:go -- fmt [files]`。

## Phase 1: 目录迁移与标识符统一（US2）

**Goal**: `experimental/ts/` 三个子项目迁移至 `experimental/js/`，全部 `ts` 标识符更名（M1-M15），仓库构建/测试全绿，审计零命中。本 phase 不接入共享 bootstrap（纯迁移，行为不变）。

**文档清单**:

- **代码规范文档**:
  - `style/javascript.md`（TS/JSON/YAML 配置编辑）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准规范；TS 文件编辑：server.ts/bootstrap.ts 路径字符串、注释）
  - `style/golang.md`（Go 文件编辑：testplan 用例常量与 gateway import）
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的基准规范；Go 文件编辑）
- **官方文档**: 无（proto/BUILD 变更均为仓库内既有模式复制；bazel/pnpm 操作命令见 `AGENTS.md`，按原则 V 不重复列出）
- **技术文章/技术参考文档**:
  - `specs/053-js-bootstrap-migration/contracts/migration-rename-map.md`（权威映射 M1-M15 + 受影响文件清单 + 审计命令，本 phase 的唯一执行依据）
  - `specs/053-js-bootstrap-migration/research.md`（D10 迁移机制）
  - `specs/053-js-bootstrap-migration/quickstart.md`（§1 审计、§6 行为不变性核对）

**Tasks**:

- [ ] T001 目录移动与全局配置（M1/M2/M3/M13/M14）：`git mv experimental/ts/grpc_hello_world experimental/ts/hello_world experimental/ts/team_graph_spike experimental/js/`；`pnpm-workspace.yaml` 删除 `- experimental/ts/*` 条目；`.bazelignore` 将三条 `experimental/ts/*/node_modules` 更新为 `experimental/js/...`。提交点：纯移动+配置，不含内容更名。
- [ ] T002 [P] grpc_hello_world 核心标识符（M4-M9）：`experimental/js/grpc_hello_world/greeter.proto`（`package experimental.js.grpc_hello_world;`、http annotation `/experimental/js/grpc-hello-world/say-hello`）；`BUILD.bazel`（`go_proto_library.importpath` 与 `go_library.importpath` → `dominion/experimental/js/grpc_hello_world`；`artifact_pkg_js.app` 与 `artifact_image.app` → `grpc-hello-world-js`）；`src/server.ts`（生成类型导入路径 `../greeter_types/experimental/js/grpc_hello_world/Greeter.js`、proto 运行时路径 `experimental/js/grpc_hello_world/greeter.proto`）；`src/bootstrap.ts`（reporter 名 `grpc-hello-world-js/service`，M10）。
- [ ] T003 [P] 服务与网关清单（M6/M7/M11）：`service.yaml` 与 `testplan/gateway/service.yaml` 的 `app: grpc-hello-world-js`；`testplan/gateway/main.go`（import `dominion/experimental/js/grpc_hello_world`、solver URI `grpc-hello-world-js/service:grpc`、日志前缀字符串）；`testplan/gateway/BUILD.bazel`（importpath → `dominion/experimental/js/grpc_hello_world/testplan/gateway`、deps → `//experimental/js/grpc_hello_world:grpc_hello_world_go`）；`smoke_test.sh` 内路径/app 名引用。
- [ ] T004 [P] testplan 配置与用例（M1/M5/M7/M15）：`testplan/deploy.yaml`、`testplan/deploy_selfheal.yaml`（artifact `path: //experimental/js/grpc_hello_world/...`；gateway `http.matches[].path.value` 前缀 → `/experimental/js/grpc-hello-world`（M15）；**保留** `GREETING_SUFFIX: ts-suffix` 测试数据）；`testplan/BUILD.bazel`（`binary` label → `//experimental/js/grpc_hello_world/testplan/gateway:gateway`、`app: grpc-hello-world-ts` ×2 → `grpc-hello-world-js`）；`testplan/interface_test.yaml`（`deploy:`/`cases:` label）；`testplan/interface_test.go` 与 `testplan/health_test.go` 的 `pathPrefix`/`healthPathPrefix` 常量 → `/experimental/js/grpc-hello-world/say-hello`。
- [ ] T005 [P] 外围引用（M1/M3/M12）：`tools/dev/js/BUILD.bazel`（load label `@npm//experimental/js/grpc_hello_world:...`）；`projects/game/agent/src/context-middleware.ts` 与 `projects/game/agent/src/team/graph.test.ts` 注释内路径；`experimental/js/vite_react_demo/BUILD.bazel` 与 `dist_assert.sh` 注释内参考路径；`experimental/js/team_graph_spike/FINDINGS.md` 路径引用。
- [ ] T006 再生成与 phase 门禁：刷新 pnpm lockfile（workspace 路径变化，按 `AGENTS.md` pnpm 流程）；`bazel run //:gazelle experimental/js`（或按目录逐个）；`bazel mod tidy`；`bazel build //...` 与 `bazel test //experimental/js/... //tools/dev/js/...` 全绿；执行 `contracts/migration-rename-map.md` §3 四条审计命令零命中。

**Phase 1 Gate**: build/test 全绿 + 审计零命中 + `test ! -d experimental/ts`。此时仓库处于"迁移完成、服务仍是手写 bootstrap"的可验证中间态（宪法 VII：此中间态是 phase 验收点，不是交付残留）。

---

## Phase 2: bootstrap 包核心——编排与健康端点（US1）

**Goal**: `@dominion/common-js-bootstrap` 包骨架 + Component/Stage 契约 + Bootstrap 编排器（排序/回滚/信号/统一预算）+ 内置健康端点，单测覆盖核心语义。

**文档清单**:

- **代码规范文档**:
  - `style/javascript.md`（ESM 书写、tsconfig/.swcrc 锁步、vitest_test 宏、DI mock 约定）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的基准规范：命名/导出/import type/类成员可见性等）
- **官方文档**:
  - [Node.js http API](https://nodejs.org/api/http.html)（server.listen/close/closeAllConnections/close 事件——健康端点与 http 适配器依据）
  - [Node.js process signal events](https://nodejs.org/api/process.html#signal-events)（SIGTERM/SIGINT 监听）
  - [MDN AbortController](https://developer.mozilla.org/docs/Web/API/AbortController) 与 [MDN AbortSignal.any()](https://developer.mozilla.org/docs/Web/API/AbortSignal/any)（取消信号构造与合并）
  - [MDN AggregateError](https://developer.mozilla.org/docs/Web/JavaScript/Reference/Global_Objects/AggregateError)（错误聚合）
  - [MDN Array.prototype.sort](https://developer.mozilla.org/docs/Web/JavaScript/Reference/Global_Objects/Array/sort)（ES2019+ 稳定排序——同 stage 按 name 稳定排序的依据，research.md D9）
- **技术文章/技术参考文档**:
  - `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§1-§4：包形态/类型/编排器/健康端点契约，实现依据）
  - `specs/053-js-bootstrap-migration/data-model.md`（§1-§4 实体与状态机）
  - `specs/053-js-bootstrap-migration/research.md`（D3 惯用语对照、D5 测试缝隙、D9 编排语义）
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§2 tsconfig/.swcrc 锁步——T007 包骨架依据）
  - `specs/052-deploy-health-probe/contracts/bootstrap-health.md`（健康端点共同约定 §1——行为锚点）
  - `common/gopkg/bootstrap/bootstrap.go`、`common/gopkg/bootstrap/health.go`、`common/gopkg/bootstrap/stage.go`、`common/gopkg/bootstrap/bootstrap_test.go`（Go 行为参照与测试用例集；stage.go 为 Stage 数值来源）
  - `common/js/logs/BUILD.bazel`（包三件套样板：ts_project + js_runtime_library + vitest_test）

**Tasks**:

- [ ] T007 包骨架：创建 `common/js/bootstrap/`（`package.json`：`"type": "module"`、name `@dominion/common-js-bootstrap`、依赖 `@dominion/common-js-logs: workspace:*`、devDeps `@types/node`/`typescript`/`vitest`；`tsconfig.json` + `.swcrc` 按 `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §2 锁步；`BUILD.bazel` 三件套对照 `common/js/logs/BUILD.bazel`；空 `src/index.ts`）；`bazel run //:gazelle common/js/bootstrap` 后补齐 BUILD 手工 target；验证 `bazel build //common/js/bootstrap:lib`。
- [ ] T008 `src/component.ts`：`Stage` as const 常量对象（100/200/250/300）+ 字面量联合类型、`Component` 接口（name/stage/start/stop，AsyncSignal 签名见契约 §2）、`ExitWatchable` 接口（`exited: Promise<Error | undefined>`）。
- [ ] T009 `src/health.ts` + `src/health.test.ts`：内置健康端点（`:38080` 全接口、`GET /healthz` → 200 `ok\n`、其余 404、`HealthService` 内部接口、`HealthHandle { stop() }`）；测试以真实 38080 绑定（单文件内串行），用例对照 Go `common/gopkg/bootstrap/health_test.go`：healthz 200/ok、`/` 与未知路径与 `/healthz/extra` 404、Stop 释放端口（可重新绑定）。
- [ ] T010 `src/bootstrap.ts`：`Bootstrap` 类（`register` 重名/run 后 throw；`run(options?)` 按 `contracts/bootstrap-js-api.md` §3.1 五步编排：排序启动→失败逆序回滚（AggregateError）→health 启动（失败同回滚）→等待退出（信号/注入 signal/exited）→health 首位+组件逆序统一预算停止；`get health()` 句柄；`shutdownTimeoutMs` 默认 5000）。
- [ ] T011 `src/bootstrap.test.ts`：DI 桩（`healthServerFactory` 注入 `vi.fn()` 记录顺序；组件以记录型 fake 实现）：stage+name 排序、重名拒绝、run 后注册拒绝、两次 run 拒绝、启动失败回滚逆序、health FIFO（最后启动/最先停止）、health 启动失败回滚、预算超时聚合、注入 AbortSignal 干净退出 resolve、组件 exited 带错触发全局关停并 reject。所有 mock 断言遵循 `style/javascript.md`（对被拦截调用做正向断言）。

**Phase 2 Gate**: `bazel test //common/js/bootstrap:lib_test` 全绿；`bazel build //common/js/bootstrap:...` 通过。

---

## Phase 3: bootstrap 包适配器与 Daemon（US1）

**Goal**: HTTP/gRPC server、gRPC client 适配器与 Daemon 监督器及单测；barrel 导出与运行时隔离审计。

**文档清单**:

- **代码规范文档**:
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**:
  - [grpc-js Server API](https://grpc.github.io/grpc-node/grpc-js/classes/Server.html)（bindAsync/tryShutdown/forceShutdown 语义：tryShutdown 等待 pending calls 完成、forceShutdown 取消全部且与 tryShutdown 幂等互触发；与 research.md D7 引用一致）
  - [grpc-js Client API](https://grpc.github.io/grpc-node/grpc-js/classes/Client.html)（Client.close()）
  - [Node.js http API — server.closeAllConnections()](https://nodejs.org/api/http.html#servercloseallconnections)（停止排空两阶段依据；Phase 2 已读 http 页面的本 phase 细读小节）
- **技术文章/技术参考文档**:
  - `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§5 Daemon、§6 适配器、§7 运行时依赖隔离强约束与审计命令）
  - `specs/053-js-bootstrap-migration/data-model.md`（§5 Daemon 实体、§6 适配器矩阵）
  - `specs/053-js-bootstrap-migration/research.md`（D2 grpc-js 隔离、D7 适配器与 JS/Go 差异、D8 Daemon）
  - `common/gopkg/bootstrap/grpc.go`、`common/gopkg/bootstrap/http.go`、`common/gopkg/bootstrap/daemon.go`（Go 行为参照）
  - `common/js/logs/src/index.ts`（barrel 导出风格样板：值具名导出 + export type——T015 对照）

**Tasks**:

- [ ] T012 `src/http-server.ts` + 测试：`createHttpServerComponent(name, server, { port, host? })`（start=listen，error 事件 reject；stop=close 排空、预算 abort 后 `closeAllConnections()`；`exited` 承接意外 close/error）。测试：listen 成功/端口冲突 reject、stop 后端口释放、abort 触发 closeAllConnections、exited 意外错误 resolve。
- [ ] T013 `src/grpc-server.ts` + `src/grpc-conn.ts` + 测试：`createGrpcServerComponent(name, { server, address, credentials })`（start=bindAsync+start；stop=tryShutdown 与预算竞速→forceShutdown；**不提供 exited**，research.md D7）与 `createGrpcConnComponent(name, client)`（start no-op、stop=client.close()）。对 `@grpc/grpc-js` 仅 `import type`。测试用结构化 fake（实现 bindAsync/tryShutdown/forceShutdown/close 的 vi.fn 对象，DI 注入），覆盖两阶段停止与 forceShutdown 回退。
- [ ] T014 `src/daemon.ts` + 测试：`createDaemon(name, buildWorker, options?)`（监督循环、指数退避 `min(cur*2, max)`、maxRestarts 耗尽→fatal 经 `exited` 上报、默认分类：AbortError+信号取消→stop / 无错完成→stop / 其余→restart）。测试：build 失败重启、worker 错误退避序列（fake timers 或缩短 backoff）、耗尽→fatal、信号取消不重启、自定义 classifyError。
- [ ] T015 barrel 与隔离审计：`src/index.ts` 全量导出（值具名导出 + `export type` 显式类型再导出，对照 `common/js/logs/src/index.ts` 风格）；`package.json` devDeps 增加 `@grpc/grpc-js: catalog:`、BUILD `:lib`/`:lib_test` 增加对应 `:node_modules/@grpc/grpc-js` 类型依赖；执行 `contracts/bootstrap-js-api.md` §7 两条审计命令零命中（包内无 `@grpc/grpc-js` 运行时导入）。

**Phase 3 Gate**: `bazel test //common/js/bootstrap:lib_test` 全绿；§7 审计零命中；`bazel build //common/js/bootstrap:runtime_pkg` 通过。

---

## Phase 4: experimental 服务接入共享 bootstrap（US3）

**Goal**: grpc_hello_world 与 team_graph_spike 删除手写 bootstrap 样板、接入共享组件；外部行为符合 spec FR-013（既有契约不变 + 新增健康端点为预期行为）。

**文档清单**:

- **代码规范文档**:
  - `style/javascript.md`（两段式入口形态 §"OTel 插桩与 loader hook"）
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**: 无（grpc-js Server 构造与 ServerCredentials 在既有 `server.ts` 中已使用，无新 API；OTel init/shutdown 为仓库内包 `common/js/otel`）
- **技术文章/技术参考文档**:
  - `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§8 服务接入形态——入口次序与静态图约束）
  - `specs/053-js-bootstrap-migration/data-model.md`（§7 服务入口终态形态）
  - `specs/053-js-bootstrap-migration/research.md`（D4 OTel 入口胶水、D6 自愈钩子缝隙）
  - `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（§2 生产路径契约：静态图禁入 grpc-js、三步次序不可倒置；§5 禁止事项）
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§4 打包与运行：artifact_pkg_js runtime_deps）
  - `specs/053-js-bootstrap-migration/quickstart.md`（§3/§4 本地验证场景）

**Tasks**:

- [ ] T016 grpc_hello_world 接入：`src/server.ts` 重构——`startServer()` 改为 `buildServer()`：proto 加载、config 读取、handler 注册、credentials 构建保留，返回未 bind 的 `grpc.Server`（不再 bindAsync/start）；`src/bootstrap.ts` 重写——静态导入仅 `@dominion/common-js-{otel,grpc-otel,logs,bootstrap}` → `await init({ instrumentations: [createGrpcInstrumentation()] })` → `installReporter(createOTelReporter("grpc-hello-world-js/service"))` → `const { buildServer } = await import("./server.js")` → `new Bootstrap()` + `register(createGrpcServerComponent(...))`（address `0.0.0.0:50051`）→ `HEALTH_STOP_AFTER_MS` 钩子经 `bootstrap.health?.stop()`（保留 `specs/052-deploy-health-probe/contracts/verification-testplan.md` §2 语义与注释边界）→ `await run()` → `uninstallReporter()` + `await shutdown()` → 按结果 `process.exit(0/1)`；`BUILD.bazel`：deps 增加 `:node_modules/@dominion/common-js-bootstrap`、`artifact_pkg_js.runtime_deps` 增加 `//common/js/bootstrap:runtime_pkg`；package.json 增加 workspace 依赖。
- [ ] T017 [P] team_graph_spike 接入：`src/server.ts` 拆出 server 构建（`http.createServer(handler)` 不 listen）；`src/bootstrap.ts` 重写——init → reporter("team-graph-spike") → 动态 import → `register(createHttpServerComponent("http", server, { port: 8080 }))` → run → shutdown → exit；接入后自动获得 38080/healthz（spec Clarifications 第 4 条）；`BUILD.bazel`/`package.json` 依赖更新同 T016 模式。
- [ ] T018 本地验证与 phase 门禁：`bazel build //experimental/js/...` + `bazel test //experimental/js/...`；`bazel test //experimental/js/grpc_hello_world:smoke_test`；按 `quickstart.md` §3（grpc_hello_world：healthz 200/ok、SIGTERM health 先停、退出码 0）与 §4（team_graph_spike：38080 healthz、8080 /health 不变、优雅退出）执行本地运行验证；`bazel run //:gazelle` 同步。

**Phase 4 Gate**: build/test/smoke 全绿 + quickstart §3/§4 场景全部符合预期 + 契约 §8 静态图约束人工核对（bootstrap.ts 静态导入清单）。

---

## Phase 5: 大型测试验收与终态审计（US1/US2/US3 验收，宪法原则 VI）

**Goal**: 实际执行大型测试（部署→测试→清理闭环）全部用例通过；仓库终态审计与全量回归。

**文档清单**:

- **代码规范文档**:
  - `style/large_test.md`（testplan 编排与执行规范；guitar 用例编写规范本 phase 不新增用例，仅执行与必要修复）
- **官方文档**: 无（guitar 为仓库内工具，执行方式由 testplan skill 承载）
- **技术文章/技术参考文档**:
  - `specs/053-js-bootstrap-migration/quickstart.md`（§5 大型测试验收、§6 行为不变性核对）
  - `specs/053-js-bootstrap-migration/contracts/migration-rename-map.md`（§3 终态审计命令）
  - `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§7 隔离审计命令）
  - `specs/052-deploy-health-probe/contracts/verification-testplan.md`（§2 自愈 suite 时序约束：判死+重启全周期 > 60s 余量、窗口断言语义）

**Tasks**:

- [ ] T019 大型测试执行（原则 VI：实际执行，禁止以构建检查替代）：加载 testplan skill 后执行 `guitar run experimental/js/grpc_hello_world/testplan/interface_test.yaml`——default suite（新路径 `/experimental/js/grpc-hello-world/say-hello` 接口契约 + 配置语义 + 探针就绪）与 selfheal suite（HEALTH_STOP_AFTER_MS 注入 → 判死失败窗口 → 自动重启恢复）全部 case 通过且清理完成；任何失败/flaky：修复后**重新完整执行**直至全部通过。
- [ ] T020 终态审计与全量回归：执行 `contracts/migration-rename-map.md` §3 四条命令 + `contracts/bootstrap-js-api.md` §7 两条命令（零命中）；`bazel build //...` + `bazel test //...` 全仓库回归全绿；更新 `specs/053-js-bootstrap-migration/checklists/requirements.md` 复验状态。

**Phase 5 Gate**: guitar 全部 case 通过 + 全部审计零命中 + 全仓库 build/test 绿。

---

## Dependencies & Execution Order

```text
Phase 1（迁移） ──┐
                  ├──▶ Phase 4（接入）──▶ Phase 5（大型测试验收）
Phase 2（核心）──▶ Phase 3（适配器/Daemon）──┘
```

- Phase 1 与 Phase 2/3 相互独立，可并行推进（不同目录、无文件交叉）。
- Phase 4 依赖 Phase 1（终态路径）与 Phase 3（适配器可用）。
- Phase 5 依赖 Phase 4。
- Phase 内 [P] 任务（不同文件、无依赖）可并行：T002/T003/T004/T005；T017 与 T016。

## Notes

- 每完成一个 phase，建议在对应 task 勾选并按 phase gate 验证后再进入下一 phase（中断可恢复：phase gate 即恢复点）。
- 实现中若发现契约遗漏的 `experimental/ts`/`grpc-hello-world-ts` 引用，按 `contracts/migration-rename-map.md` 同规则处理并回填该契约 §2 清单。
