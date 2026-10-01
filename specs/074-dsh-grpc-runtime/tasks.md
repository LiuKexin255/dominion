# Tasks: gRPC client 基建插件、memory 能力化与 preset 模板简化

**Input**: Design documents from `/specs/074-dsh-grpc-runtime/`

**Prerequisites**: plan.md、spec.md（用户故事 US1–US5）、research.md（D1–D14）、data-model.md、contracts/（grpc-runtime / memory-library / team-assembly / preset-templates）、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task，任务完成即要求 `bazel build` / `bazel test` 相关 target 通过）；大型测试（testplan 四 suite）在原子切换 phase 单独分配验收任务。mock 约定按 `style/javascript.md`：DI / `vi.fn` test-double（禁止新增 `vi.mock` 模块拦截）。

**Organization**: 按 user story 分 phase（US1 基建 → US2 memory 公共库 → US3 提供方与编排 → 原子切换（US2 收尾 + US3 激活 + US4 模板简化的同批落地）→ US5 终态）。依据 research.md D11 迁移分步：US1/US2/US3 三个准备 phase 各自独立可构建可测试（旧世界未动）；**Phase 6 为原子切换单元**——cordis.yml 终态（saolei-loop config.plugins 激活 + 模板工具行/templateRules 移除）与旧包删除必须同批合入（模板行与编排注册并存会双重注册），该 phase 不允许部分合入；四 suite 大型测试验收归属此 phase（宪法 VI）。US5 仅文档/契约修订与静态复查（无代码变更，不重跑四 suite）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US5）；Setup/Foundational phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；JS 插件 `common/js/dsh-plugins/`，公共库 `common/js/memory/`（新建），agent_v2 `projects/game/agent_v2/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更。新包目录均在 `pnpm-workspace.yaml` 既有 glob（`common/js/**`）覆盖内；全部第三方依赖已在 catalog（`@deepseek-ai/cordis`、`@grpc/grpc-js`、`@grpc/proto-loader`、`@deepseek-ai/dsh-tools` 等），无新依赖引入、无 catalog 变更。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（各实现 phase 自带文档清单）

---

## Phase 2: Foundational

**无任务**：本特性无跨 US 的共享阻塞件（无 proto 变更、无 schema 迁移）。US1（grpc-runtime 基建行）即结构基座，作为首个 P1 story phase 执行；US2/US3 依赖 US1，US4 随 Phase 6 原子切换落地，US5 最后。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（本 phase 无任务；各实现 phase 自带文档清单）

---

## Phase 3: User Story 1 - gRPC client 基建插件（Priority: P1）🎯 结构基座

**Goal**: 组合级 grpc-runtime 基建行落地（`ctx.grpc` 服务面：类型化 client 构造 + unary 辅助 + channel/packageDefinition 缓存共享 + canonical protoPath 解析 + 连接策略迁移），无消费方、行为零变化。

**Independent Test**: 基建包单测全绿（策略/缓存/共享/解析/unary/teardown/初始化顺序）；组合清单含 grpc-runtime 行且 agent_v2 既有单测不回归（quickstart.md §1 US1 部分）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（ESM 书写规则、CJS 依赖导入约定——`@grpc/grpc-js`/`@grpc/proto-loader` 具名导入已核实可用、`vitest_test` 宏 data 规则、DI mock 约定；含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[@grpc/grpc-js](https://github.com/grpc/grpc-node/blob/master/packages/grpc-js/README.md)（client/Channel API 与支持 channel options——含 `service_config`/`channelOverride`/退避/keepalive 键）；[@grpc/proto-loader](https://github.com/grpc/grpc-node/blob/master/packages/proto-loader/README.md)（`loadSync` options 表：`longs`/`enums`/`defaults`/`oneofs`/`includeDirs`）；[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）
- **技术文章/技术参考文档**：channel 共享维护者模式 [grpc-node#2214](https://github.com/grpc/grpc-node/issues/2214)、one client per service per process [grpc-node#2896](https://github.com/grpc/grpc-node/issues/2896)、隐式连接共享要求相同 credentials/options [grpc-node#158](https://github.com/grpc/grpc-node/issues/158)；组合清单管辖契约（T005 行断言现状）：`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 组合行集合与 `dsh.test.ts` 精确断言——迁移期 22 行口径与 T019 终态 21 行收口）；`specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md`（全文——API/义务/策略/初始化顺序契约）；`specs/074-dsh-grpc-runtime/research.md`（D1–D3 包形态与策略 why 表、D2 API 定稿、D13 机制依据）；`specs/074-dsh-grpc-runtime/data-model.md`（§1 基建实体）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新包契约全文——包声明/tsconfig 与 .swcrc 锁步/源码规则/测试）；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（OTel 初始化时序契约与禁止事项——§2 两段式 bootstrap、§3 `createRequire` 豁免登记；bootstrap 两段式插桩注册 + 插件 channel 惰性创建的依据）；`specs/048-js-esm-migration/research.md`（R5 逐包具名导入核实表——`@grpc/grpc-js`/`@grpc/proto-loader` 具名导入依据）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 退出码契约、data 双实例不变量）；`survey/dsh-grpc-runtime-client.md`（§6.1 服务面示意、R1 grpc-js 机制事实、"必须随代码迁移的策略"清单）；`tools/release/deploy/README.md` §runtime_protos（canonical protoPath 部署标准——:173 proto 按标准导入路径放置、:246 runtime_protos 物料）；迁移源现状：`common/js/dsh-plugins/memory-service/src/client.ts`（:120-172 TLS/退避/round_robin 策略、:210-249 callUnary、:455-473 probeChannel、:183-207 serviceRoot/protoOptions）；`common/js/grpc/otel/src/index.ts`（载入顺序约束与插桩工厂导出——无注册 API）、`common/js/grpc/otel/src/index.test.ts`（`createRequire` 豁免场景唯一先例——048 契约 §3 测试路径契约的仓库实例）；`common/js/grpc/resolver/src/grpc-js-resolver.ts`（registerDominionResolver 幂等语义）

- [X] T001 [US1] 创建包 `common/js/dsh-plugins/grpc-runtime/` 骨架：`package.json`（name `@dominion/dsh-grpc-runtime`、`"type": "module"`、`exports["."]` types+default 双条件指向 `./src/index.js`、deps 走 catalog：`@deepseek-ai/cordis`/`@grpc/grpc-js`/`@grpc/proto-loader`/`@dominion/common-js-grpc-resolver`/`@dominion/common-js-grpc-otel`/`@dominion/common-js-logs`）；`tsconfig.json`（`module: nodenext`、`target: ES2020`、workspace 依赖 `paths` 指向 `<dep>/src/index.js`）与 `.swcrc`（`{"module":{"type":"es6","preserveImportMeta":true}}`、`jsc.target: es2020`）锁步；执行 `bazel run @pnpm -- --dir /mnt/code/dominion up` 更新 lockfile、`bazel run //:gazelle common/js/dsh-plugins/grpc-runtime` 生成 BUILD.bazel
- [X] T002 [P] [US1] `common/js/dsh-plugins/grpc-runtime/src/channel.ts`：连接策略从 `common/js/dsh-plugins/memory-service/src/client.ts:120-172` 迁移并逐条带 why 注释（按 research.md D3 表：round_robin 覆盖 pick_first——滚动升级路由；MUST NOT app 级 keepalive——对齐 grpc-go ClientDefault、unary server enforcement 会 GOAWAY 多余 ping；重连退避 initial 1s / max 15s；TLS `/etc/tls/ca.crt` 存在则 `createSsl` + `TLS_SERVER_NAME` override 否则 insecure）；channel 缓存 `Map<string, ClientChannel>`（键 = target，凭据/channel 策略为基建统一常量故键退化——契约注明策略分化时必须扩键，`specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md` §2 O2）；`channel.test.ts`——策略断言（options 含 round_robin service config、无 keepalive 键、退避值、TLS 分支）与缓存共享断言（同 target 两次构造返回共享同一 channel 实例、不同 target 各自独立）
- [X] T003 [P] [US1] `common/js/dsh-plugins/grpc-runtime/src/unary.ts`：`callUnary<T>` 与 `probeChannel` 从 `common/js/dsh-plugins/memory-service/src/client.ts:210-249,455-473` **逐字段照搬**（`Metadata({waitForReady: true})`、deadline 缺省 10s、pre-call 连接探测防"Waiting for LB pick"无限排队、失败后 channel 状态日志），仅增可选 `deadlineMs` 参数（签名见 `specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md` §2）；`unary.test.ts`——callUnary 语义断言（deadline 默认与 `deadlineMs` 覆盖、waitForReady metadata、probe 先行、失败日志含 channel 状态、reject 传播）
- [X] T004 [US1] `common/js/dsh-plugins/grpc-runtime/src/client.ts` 与 `src/index.ts`（依赖 T002、T003）：`client<T>(options)` —— canonical protoPath 解析（service root 模块相对深溯，memory-service `serviceRoot()` 生产布局同型四段上溯；绝对路径逃逸出口；`includeDirs` 缺省解析出的 service root）、packageDefinition 缓存（键 = protoPath+includeDirs；`longs:String, enums:String, defaults:true, oneofs:true` 与 `ts_proto_library` 生成选项严格一致）、`loadPackageDefinition` 后按 `serviceName` 逐段解析构造器、以共享 channel 经 `channelOverride` 构造 service client、**单点 `as T` 泛型收口**（全仓唯一 proto 强转点）；插件入口 `src/index.ts`（`name = "grpc-runtime"`、`inject = []`、apply：幂等注册 resolver → 建立惰性 runtime（channel 唯一创建点 `ctx.grpc.client()`，apply 期不建 channel）→ `ctx.provide("grpc", {...})` → `ctx.effect` teardown 关闭全部缓存 channel；插桩注册归服务 bootstrap 两段式（`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` §2），插件 MUST NOT 自行注册；MUST NOT 模块顶层副作用初始化）+ `declare module "@deepseek-ai/cordis"` 的 `Context.grpc` 类型增强（`GrpcRuntime`/`GrpcClientOptions` 按契约 §2）；单测 `client.test.ts`/`index.test.ts`（DI/`vi.fn` 约定，正向断言 mock 生效）——canonical 相对路径与绝对路径解析、packageDefinition 缓存命中（同 protoPath 两次构造仅 loadSync 一次）、serviceName 逐段解析与错误（不存在的服务名 fail-loud）、teardown 关闭全部 channel、初始化顺序（结构性断言：apply 期不创建 channel、注册序 resolver→runtime——时序依据 `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` §2）；BUILD.bazel 增 `vitest_test` 宏（`data` = src glob + 镜像 `:lib` 的全部 `:node_modules/*` deps，禁止手写裸 genrule）
- [X] T005 [US1] 组合清单接线：`projects/game/agent_v2/cordis.yml` 增 `- { id: grpc-runtime, name: '@dominion/dsh-grpc-runtime' }` 行（无 config，置于 `@dominion/dsh-core` 基线行同级、`@dominion/*` 行分区）；`projects/game/agent_v2/package.json` 增依赖 `@dominion/dsh-grpc-runtime: workspace:*`、`BUILD.bazel` 相应 `runtime_deps` 增补（workspace 包不进 npm_deps，同 `@dominion/dsh-memory-service` 现行模式）；`projects/game/agent_v2/src/dsh.test.ts` manifest 断言更新（行集合含 grpc-runtime；本 phase 迁移期为 **22 行**——grpc-runtime 行已在、memory host 行仍在（Phase 6 删除后回落到 21 行）；**契约/代码锁步**：同批把管辖契约 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 的行集合与断言口径修订为迁移期 22 行，T019 再收口为终态 21 行）；`bazel run @pnpm -- --dir /mnt/code/dominion up`、`bazel run //:gazelle projects/game/agent_v2` 后 `bazel build //...` 与 `bazel test //common/js/dsh-plugins/grpc-runtime/... //projects/game/agent_v2/...` 通过（无消费方、行为零变化，不需四 suite）

**Checkpoint**: 基建行就绪（SC-003 的 grpc-runtime 部分、SC-004 机制具备）；`ctx.grpc` 可消费。

---

## Phase 4: User Story 2 - memory 公共库（Priority: P1）

**Goal**: `@dominion/common-js-memory`（`common/js/memory/`）落地：领域语义等价迁入 + 原子能力单元工厂/原语导出 + 工具 execute 改面解析 + 缺面 fail-loud；旧世界（memory-service、dsh-memory）未动、并行可构建。

**Independent Test**: 公共库单测全绿（迁移语义用例等价 + 单元工厂/face/缺面新用例）；`@dominion/dsh-memory-service` 与旧 `dsh-memory` 包不受影响（`bazel build //...` 全仓通过）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；ESM/测试/DI 约定）
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）；`@deepseek-ai/dsh-tools`（私有 harness 包，无公开文档 → 类型依据见「技术文章/技术参考文档」）
- **技术文章/技术参考文档**：`@deepseek-ai/dsh-tools` 类型定义（`common/js/dsh-plugins/memory/node_modules/@deepseek-ai/dsh-tools/lib/types/index.d.ts`——`defineTool`/`ToolDefinition`/`ToolRunContext` 依据；新包安装后经 `common/js/memory/node_modules/` 解析）；`@deepseek-ai/dsh-system-prompt` 类型定义（`common/js/dsh-plugins/memory/node_modules/@deepseek-ai/dsh-system-prompt/lib/types/index.d.ts:47`——`PromptSection` 注册规格类型依据）；`specs/074-dsh-grpc-runtime/contracts/memory-library.md`（全文——单元工厂/原语/结构化调用面/缺面文案契约）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（§3.1-§3.2 提供方对照——memory-library §6 的引用目标）；`specs/074-dsh-grpc-runtime/research.md`（D6 库化裁定与迁移映射、D7 缺面文案）；`specs/074-dsh-grpc-runtime/data-model.md`（§2 能力层实体）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新库包契约——声明/tsconfig 与 .swcrc 锁步/源码规则）；`specs/048-js-esm-migration/research.md`（R5 逐包具名导入核实表——CJS 依赖导入方式依据）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 退出码契约、data 双实例不变量）；`survey/deepseek-harness-memory-plugin.md`（memory 物化 setup 时序与快照冻结背景）、`survey/planner-memory-and-agent-communication.md`（快照冻结哲学——FR-009 冻结不变量背景）；**领域语义零改动基线契约**：`specs/071-memory-preset-scope/contracts/memory-service.md`（preset 作用域/快照 10 条降序冻结语义）、`specs/071-memory-preset-scope/contracts/dsh-plugins.md`（工具面绑定语义基线）、`specs/069-wiki-template-web/contracts/dsh-plugins.md` 与 `specs/069-wiki-template-web/contracts/memory-content-limit.md`（wiki 工具语义、单条 1024 rune 上限）、`specs/068-wiki-round-pause/contracts/memory-page-token-context.md`（order_by/token 语义）、`specs/068-wiki-round-pause/contracts/wiki-service.md`（wiki 语义基线）、`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（§2 绑定/fail-loud 基线）；迁移源现状：`common/js/dsh-plugins/memory-service/src/{operations,wiki-operations,snapshot,errors,service,client,wiki-client}.ts`、迁移/重组源测试：`common/js/dsh-plugins/memory-service/src/{operations.test.ts（629）、wiki-operations.test.ts（500）、client.test.ts（331）、wiki-client.test.ts（412）、service.test.ts（483）}`、`common/js/dsh-plugins/memory/src/{tool,wiki-tools,guidance}.ts`、**per-agent 面解析同型先例**：`common/js/dsh-plugins/saolei/src/index.ts`（:91-104 `exec.agent.ctx` 属性穿越与缺面 fail-loud 文案——T010 改造基线）

- [ ] T006 [US2] 创建库包 `common/js/memory/` 骨架：`package.json`（name `@dominion/common-js-memory`、`"type": "module"`、exports types+default → `./src/index.js`、deps catalog：`@deepseek-ai/dsh-tools`（`defineTool` 运行时依赖 + 类型）/`@dominion/common-js-logs`/`@grpc/grpc-js`（结构化调用面类型基础）、peerDependencies：`@deepseek-ai/dsh-system-prompt`（`PromptSection` 类型，type-only——契约 `memory-library.md` §2））；`tsconfig.json`/`.swcrc` 锁步（同 T001 模式）；`bazel run @pnpm -- --dir /mnt/code/dominion up` + `bazel run //:gazelle common/js/memory`；**纯库**——MUST NOT 导出 cordis 插件形态（name/inject/apply）
- [ ] T007 [P] [US2] 领域语义文件级迁入（零逻辑改动，仅 import 路径与注释引用终态化）：`common/js/dsh-plugins/memory-service/src/operations.ts`（478 行）、`wiki-operations.ts`（379 行）、`snapshot.ts`（66 行）、`errors.ts`（10 行）→ `common/js/memory/src/`；`operations.test.ts`（629 行）、`wiki-operations.test.ts`（500 行）同批文件级迁入（用例语义零改动）
- [ ] T008 [P] [US2] `common/js/memory/src/store.ts`：`MemoryClient`/`WikiClient` 适配层改造——**吸收迁移源 `common/js/dsh-plugins/memory-service/src/client.ts`（473 行：连接基建由 T002/T003/T004 承接后仅保留 client 适配与页游走语义）与 `wiki-client.ts`（377 行 RPC 适配：连接部分同归基建，保留页游走/name 解析）**；构造器仅收注入 client（删除自建分支：resolver 注册/loadSync/credentials/channel 构造，连接职责已归基建），实现 `MemoryStore`/`WikiStore`（页游走/AIP 语义零改动）；结构化调用面类型 `MemoryServiceCalls`/`WikiServiceCalls`（8 个 RPC 方法的 request/response 结构类型，复用既有手写消息类型——`specs/074-dsh-grpc-runtime/contracts/memory-library.md` §3；MUST NOT import projects/game 生成代码或 saolei-loop）；`client.test.ts`（331）/`wiki-client.test.ts`（412）迁入拆分：页游走/name 解析/DI 注入用例保留，连接策略用例删除（已由 T002/T003/T004 基建测试承接）
- [ ] T009 [US2] `common/js/memory/src/face.ts` 与 `capabilities.ts`（依赖 T007、T008）：`MemoryScope`（`{template, preset}`，库内场景无关命名；编排层自持的 `PlannerMemoryScope` 结构类型见 T013）；`createMemoryStore(memoryClient, wikiClient)` / `createMemoryFace(store, scope)`（face 创建即绑定 scope，`prefetch`/`applyCall`/`wikiSearch`/`wikiGet`/`wikiWrite`/`snapshotText`——`memory-service/src/service.ts:132-158` 的按 agent scope 解析 Maps 语义随 face 绑定消失）；`buildMemoryCapabilities(deps)` 原子单元（`faces: [{key: "memory", value: face}]`、`tools`×4、`sections`×3（`memory:guidance` 110 / `wiki:guidance` 120 / `memory:snapshot` 200 闭包 `() => face.snapshotText()`）、`ready: () => face.prefetch()`——faces/tools/sections/ready 为不可拆分单元，契约 `memory-library.md` §1）；face 单测（scope 绑定、prefetch 冻结快照文本、预取失败 reject 传播、单元原子性——四字段同时产出）
- [ ] T010 [US2] `common/js/memory/src/{tool,wiki-tools,guidance,index}.ts`：工具定义与 guidance 从 `common/js/dsh-plugins/memory/src/` 迁入（schema/描述/文案/section 名与 order 零变化）；`tool.ts`/`wiki-tools.ts` 的 execute 从闭包绑定改为 `exec.agent.ctx.memory` 属性穿越解析（saolei 同型 `common/js/dsh-plugins/saolei/src/index.ts:91-104`），缺面 fail-loud 文案按 research.md D7（四工具同型：`memory tools require the agent-scoped "memory" service; agent "${exec.agent.id}" has none (the team-layer materialization setup registers it from the memory capability mount)`）；`service.test.ts`（483 行）重写为 face/单元工厂测试（scope 绑定语义、wiki 转发、快照冻结、缺面错误断言）；`index.ts` 汇总导出（buildMemoryCapabilities/原语/类型/工具常量——`specs/074-dsh-grpc-runtime/contracts/memory-library.md` §1-§3）

**Checkpoint**: memory 公共库独立完备（SC-006 领域语义部分、SC-005 缺面用例具备）；旧世界未动。

---

## Phase 5: User Story 3 - 能力提供方与编排（Priority: P1）

**Goal**: 三提供方/编排件就绪但未激活：dsh-saolei 改造为 saoleiTools 静态单元提供方、薄 adapter（`@dominion/dsh-saolei-memory`）提供 memoryMount 工厂、saolei-loop 增依赖装配（config.plugins + ctx.plugin + reflect.get）与编排消费序——全部单测以 stub 提供方验证，cordis.yml 未含 config.plugins（不激活）。

**Independent Test**: 三包单测全绿（提供面内容/装配 fail-loud/注册序/分支/rollback 传播——stub 提供方注入）；全仓 `bazel build //...` 通过。本 phase 不启动真实 agent_v2 组合：cordis.yml 尚未含 config.plugins，真实组合下 saolei-loop apply 会因提供面缺失 fail-loud，故装配与编排分支在激活前仅由带 stub 提供方的单测覆盖（真实组合验证归 Phase 6 T014–T018）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；ESM/测试/DI 约定）
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）；cordis（私有插件运行时，无公开文档 → API 依据见「技术文章/技术参考文档」）
- **技术文章/技术参考文档**：cordis API 类型定义（`common/js/dsh-plugins/saolei-loop/node_modules/@deepseek-ai/cordis/lib/types/registry.d.ts`（`ctx.plugin` :120/:198、`ctx.inject` :111）与 `.../lib/types/reflect.d.ts`（`provide` :41 / `get` :7-12）——D13 已核验，实现期直接对照）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（全文——依赖声明/提供方契约/编排调用序/不变量/依赖方式边界）；`specs/074-dsh-grpc-runtime/contracts/memory-library.md`（§6 dsh-saolei 对照）；`specs/074-dsh-grpc-runtime/research.md`（D4 四轮终态、D5 config 化、D13 机制依据、D14 依赖方式边界）；`specs/074-dsh-grpc-runtime/data-model.md`（§3 组装层实体与时序图）；`specs/074-dsh-grpc-runtime/contracts/preset-templates.md`（§3 templateRules 移除与 §5 组合清单终态——T014 形态依据）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（adapter 新包契约）；`specs/048-js-esm-migration/research.md`（R5 逐包具名导入核实表——CJS 依赖导入方式依据）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（测试 shim 契约）；现状参考：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（:216-269 seam/loadPlannerMemory 现状、:731-799 createMember、:801-826 mountPlayerRuntime——改造基线）、`common/js/dsh-plugins/saolei/src/index.ts`（工具/sections 注册现状——T011 改造基线）、`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:430-440`（saoleiGame 挂载不变项）；`projects/game/agent_v2/cordis.yml`（invariant 行的裸说明符动态 import 注释——裸包名解析机制现状；依据 research.md D13 第 3 条）

- [ ] T011 [P] [US3] `common/js/dsh-plugins/saolei/src/index.ts` 改造为静态单元提供方：apply 改为 `ctx.provide("saoleiTools", { tools: [saolei_init, saolei_operate, saolei_remain 的定义], sections: [saolei:guidance, saolei:game 的 spec] })`——删除 `ctx.tools.register` / `ctx.systemPrompt.section` 全局注册（组合级挂载下会泄漏给全员），`inject` 改 `[]`；工具定义/schema/文案与 section 名/order/文本**零变化**；工具 execute 的 `exec.agent.ctx.saoleiGame` 解析不动；`index.test.ts` 重写（提供面内容断言——三工具 schema 逐字、两 sections 名/order/文本逐字；无全局注册副作用断言——stub ctx 断言 tools.register 未被调用）
- [ ] T012 [P] [US3] 创建 `common/js/dsh-plugins/saolei-memory/`（`@dominion/dsh-saolei-memory`，薄 adapter）：`package.json`（deps：`@deepseek-ai/cordis`/`@dominion/common-js-logs`/`@dominion/common-js-memory` workspace:*）+ tsconfig/.swcrc（同 T001 模式）+ gazelle；`src/index.ts`：cordis 插件（`name = "saolei-memory"`、`inject = ["grpc"]`、Config zod schema `{ target: string, protoPath: string, label?: string }`——非 canonical 绝对路径亦接受为逃逸出口）；apply 组合级一次性构造 `ctx.grpc.client<MemoryServiceCalls>` 与 `<WikiServiceCalls>`（config 驱动，含 `serviceName: "projects.game.MemoryService"` / `"projects.game.WikiService"`）→ `ctx.provide("memoryMount", { build(scope: MemoryScope) { return buildMemoryCapabilities({ scope, memoryClient, wikiClient }); } })`；依赖方向 MUST 为 adapter → 公共库 + grpc 面（MUST NOT 依赖 saolei-loop——scope 显式传参，`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §3.2）；单测（stub ctx.grpc —— `client` `vi.fn` 返回 DI 假 client：config 驱动构造参数断言、provide 的 memoryMount 面存在、`build(scope)` 返回原子单元且 scope 显式传入、grpc 面缺失 fail-loud）
- [ ] T013 [US3] `common/js/dsh-plugins/saolei-loop/src/` 组装改造（依赖 T011、T012 的提供面类型；文件：`orchestrator.ts` + `index.ts`）：① Config schema 增 `plugins`（zod 扁平 rows `{ name: string, config?: ... }`——无角色分组/白名单/校验，`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §2）；② apply 依赖装配：按裸包名动态 `import(row.name)`（Node ESM 经宿主闭包解析——机制见 `specs/074-dsh-grpc-runtime/research.md` D13 第 3 条，现状注释见 `projects/game/agent_v2/cordis.yml` invariant 行）→ `await ctx.plugin(entry, row.config)`（失败 reject = fail-loud）→ `reflect.get("saoleiTools")` / `reflect.get("memoryMount")`，任一缺失 → throw；③ createMember setup 编排（`orchestrator.ts:756-766` 区域）：player 分支注册 saoleiTools 单元（tools + sections → agentCtx）→ 既有 `mountPlayerRuntime`；planner 分支 `unit = memoryMount.build(options.memoryScope)` → 注册序固定 **faces → tools → sections**（agentCtx.isolate("memory", face) → tools.register×4 → systemPrompt.section×3）→ `await unit.ready()`（fail-loud 预取，reject → setup throw → 物化回滚）；④ 移除 `loadPlannerMemory` dep 与私有方法（`orchestrator.ts:262-269, 828-839`）及相关注释终态化（编排层自持 `PlannerMemoryScope` 结构类型——saolei-loop 无能力实现编译依赖，与公共库 `MemoryScope` 结构兼容，`build(scope)` 显式传参）；⑤ 单测更新：`orchestrator.test.ts` / `index.test.ts` 以 stub 提供方插件（挂载假 saoleiTools/memoryMount provide）覆盖——依赖装配（列表行挂载、提供缺失 apply throw、坏插件挂载 reject）、编排分支（player 注册三工具两 sections 且无 memory 痕迹；planner 注册四工具三 sections + isolate "memory"）、注册序断言、`ready()` reject → setup throw 传播（rollback 契约 `specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §4/§6）

**Checkpoint**: 组装通路代码就绪（未激活）；SC-008 组装完整性用例具备。

---

## Phase 6: 原子切换（US2 收尾 + US3 激活 + US4 模板简化）⚠️ 单一合入单元

**Goal**: cordis.yml 终态 + templateRules 移除 + 旧包删除 + 断言终态化，一次合入；四 suite 大型测试全绿 = 本特性行为面验收。

**Independent Test**: `bazel build //...` + 全量单测通过；`guitar run projects/game/testplan/system_test.yaml` 四 suite（game-system/game-disconnect/game-memory-down/game-stall）全部用例通过；quickstart.md §2 静态检索零命中。

**⚠️ CRITICAL**: T014–T017 必须同批合入（模板行与编排注册并存会双重注册；saolei-loop config.plugins 激活依赖模板工具行已移除）；T018 验收在本 phase 全部任务完成后执行。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；本 phase 编辑 session.ts/dsh.test.ts/preset-authoring 等 TS 代码）；`style/large_test.md`（T018 测试计划执行与用例组织——四 suite 归属、guitar 闭环；:53 指引大型测试 Go 代码遵循 `style/golang.md`）；`style/golang.md`（T018 若修正用例断言须遵循的 Go 单测/断言规范）
- **官方文档**：无（testplan 经 skill 执行——`style/large_test.md` :83 FOR Agent 指引）
- **技术文章/技术参考文档**：`specs/074-dsh-grpc-runtime/contracts/preset-templates.md`（§1-§5 全文——即插即用语义/分界判据/templateRules 移除/registry 边界/组合清单终态 diff）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（§2 config 形态、§5 行为差异三处）；`specs/074-dsh-grpc-runtime/research.md`（D8 移除清单、D10 行为差异、D11 P3 原子性依据）；`specs/074-dsh-grpc-runtime/data-model.md`（§4-§5 模板实体变更与消失实体清单）；`specs/074-dsh-grpc-runtime/quickstart.md`（§2-§3 验证步骤）；`tools/release/deploy/README.md`（§runtime_protos 部署物料标准——T018 四 suite 部署拓扑依据）；现行契约（修订对象现状，本 phase 仅按 074 契约改代码/配置、契约文本修订归 T019）：`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 断言契约——dsh.test.ts 迁移期 22 行断言现状，T019 收口为终态 21 行）、`specs/070-dsh-upgrade/contracts/preset-model-mapping.md`（§3 模板快照语义现状）、`specs/059-agent-v2-team-mode/contracts/preset-api.md`（§2 role 语义现状）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§3/§5 现状）；编辑对象：`projects/game/agent_v2/cordis.yml`（现行清单全文）、`projects/game/testplan/system_test.yaml`（四 suite 归属核对——本 phase 不改测试计划）、`common/js/dsh-plugins/preset-authoring/src/index.ts` 与 `src/derive.ts`（T015 移除 templateRules 的现状基线）、`projects/game/agent_v2/src/dsh.test.ts`（T017 断言终态化的现状基线）

- [ ] T014 [US3] `projects/game/agent_v2/cordis.yml` saolei-loop 行增 config（与 T015 同批）：`config.plugins` 扁平列表——`- name: '@dominion/dsh-saolei'`、`- name: '@dominion/dsh-saolei-memory'` + `config: { target: 'dominion:///game/memory:50051', protoPath: 'projects/game/game.proto' }`（形态按 `specs/074-dsh-grpc-runtime/contracts/preset-templates.md` §5 diff；saolei-loop 行注释终态化——依赖声明语义）；`projects/game/agent_v2/package.json`/`BUILD.bazel` 增 `@dominion/dsh-saolei-memory`（与既有 `@dominion/dsh-saolei` 同型闭包声明）
- [ ] T015 [US4] templateRules 机制移除与模板快照终态（research.md D8 清单逐点）：`common/js/dsh-plugins/preset-authoring/src/index.ts`——Config 类型 `templateRules?` 字段（:69）、zod schema 项（:99-106）、`validateTemplateRows` 的 import 与再导出、`TemplateRowRules` 的 import 与再导出（:26-31,:36-37）、deps 注入与捕获（:194,:258）、create 时校验调用（:383-386）、`createPresetAuthoring` 透传（:570）；`src/derive.ts`——`TemplateRowRules` 类型（:82-87）、`validateTemplateRows`（:157-180）；`src/index.test.ts`（:571-605）与 `src/derive.test.ts`（:109-159）相关 fixture 删除（derive 的 persona patch 用例保留）；`projects/game/agent_v2/cordis.yml` preset-authoring 行——`templates.player.plugins`/`templates.planner.plugins` 仅留 persona 行（`@dominion/dsh-saolei`/`@dominion/dsh-memory` 工具行删除）、`templateRules` 数据块删除（:156-170）、模板 `description` 改写为即插即用语义表述、`templates` 快照注释终态化（:136-139 引用随迁）
- [ ] T016 [US2] 旧世界删除：`common/js/dsh-plugins/memory-service/` 与 `common/js/dsh-plugins/memory/` 两包整目录删除（含测试与 BUILD）；`projects/game/agent_v2/package.json`/`BUILD.bazel` 移除 `@dominion/dsh-memory` 与 `@dominion/dsh-memory-service` 依赖声明；`projects/game/agent_v2/src/session.ts` 移除 `loadPlannerMemoryBinding()`（:860-874）、`LoadPlannerMemory` 的 import（:52）、deps 字段（:178）与绑定调用（:816）及注释（memoryScope 传递流 :518-537 不变）；`bazel run @pnpm -- --dir /mnt/code/dominion up` + `bazel run //:gazelle`（两删除目录与 agent_v2）后全仓 `bazel build //...` + `bazel test //...` 通过
- [ ] T017 [US4] `projects/game/agent_v2/src/dsh.test.ts` 断言终态化：manifest 行断言（行集合含 grpc-runtime 行、无 memory host 行、saolei-loop 行携带 config.plugins 扁平列表及内容；合约口径 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2）；authoring config 断言（templates 两模板 plugins 仅 persona 行、无 templateRules 键、description 新文案）；既有 fixture 终态化（`PLAYER_TEMPLATE`/`PLANNER_TEMPLATE` :482-499、runtime 列表 :611-633、authoring config `toEqual` :740-766、`byId.get("memory")` :773 等——按 `specs/074-dsh-grpc-runtime/contracts/preset-templates.md` §5 终态）
- [ ] T018 [US3] 大型测试验收（宪法 VI；依赖 T014–T017 全部完成）：通过 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（部署→测试→清理闭环），四 suite（game-system / game-disconnect / game-memory-down / game-stall）全部用例通过——覆盖 planner 记忆回路（快照注入/写入/review）、preset 闭环（CRUD/物化/模型目录）、player 回路与 memory 故障路径（预取 fail-loud/回滚在真实拓扑下成立）；任何失败（failed/flaky）修复后重跑至全绿；测试计划与用例零改动（本特性为通路迁移，行为零回归——若发现用例自身因形态断言失败，仅修正断言不改变用例语义、不新增/重排 Go 用例，故不引入 `pkg/testtool`/`common/gopkg/otel/tracecontext` 的新用法；`style/large_test.md` 反模式条款约束）

**Checkpoint**: 终态通路生效 = **特性行为面交付**（SC-001/SC-002/SC-003/SC-007/SC-008 全部达成）。

---

## Phase 7: User Story 5 - 终态清理与契约修订（Priority: P2）

**Goal**: 受影响契约修订至终态、README/注释终态化、引用清零复查——纯文档与静态验证，无代码变更（四 suite 结果持续有效）。

**Independent Test**: quickstart.md §1-§2 全量通过（build+单测复跑确认 + 三组静态检索零命中 + 组合清单终态核对 + 旧目录不存在核对）；契约 diff 审阅通过。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无（纯文档修订）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/074-dsh-grpc-runtime/contracts/preset-templates.md`（§6 修订清单——逐份修订点）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（§5 行为差异三处契约文本）；`specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md` 与 `memory-library.md`（新形态表述的参照源）；`specs/074-dsh-grpc-runtime/quickstart.md`（全文——T021 验证步骤）；修订对象原文：`specs/059-agent-v2-team-mode/contracts/preset-api.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`、`specs/070-dsh-upgrade/contracts/composition-manifest.md`、`specs/070-dsh-upgrade/contracts/preset-model-mapping.md`、`specs/071-memory-preset-scope/contracts/dsh-plugins.md`、`specs/071-memory-preset-scope/contracts/memory-service.md`（六份全文——按 074 §6 清单修订，领域行为条款零改动）；编辑对象：`projects/game/agent_v2/README.md`（T020 旧形态表述改写基线，:109/:137/:184-185/:306/:340 提及点）

- [ ] T019 [P] [US5] 契约修订（`specs/074-dsh-grpc-runtime/contracts/preset-templates.md` §6 清单逐份）：`specs/059-agent-v2-team-mode/contracts/preset-api.md` §2（"role 决定工具插件行"→ role 对应能力组装归 saolei-loop 编排代码、依赖经 config.plugins 声明；templateRules 引用删除）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3/§5（`ctx.plannerMemory` host 面 → memory 公共库 + adapter 表述；组合清单行变化）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 由 T005 落地的迁移期 22 行口径收口为终态 21 行——删除 memory host 行、saolei-loop config.plugins、grpc-runtime 行已在；§4 声明面表增删）；`specs/070-dsh-upgrade/contracts/preset-model-mapping.md` §3（templates 快照 = 即插即用 persona 行；templateRules 段删除）；`specs/071-memory-preset-scope/contracts/dsh-plugins.md` 与 `memory-service.md`（memory 挂载/预取/快照通路表述迁移至公共库 + adapter；`plannerMemory` 面名清零；行为差异三处按 `specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §5 文本）
- [ ] T020 [P] [US5] 终态清理：`projects/game/agent_v2/README.md`（memory-service/插件行/`ctx.plannerMemory` 表述 → 公共库 + adapter + 角色依赖列表新形态，:109,:137,:184-185,:306,:340 等提及点全量改写）；全仓引用清零复查（`specs/074-dsh-grpc-runtime/quickstart.md` §2 三组检索 + `@dominion/dsh-memory'` 引号形态 + 旧目录不存在 + `common/js/memory/` 存在）；`common/js/memory/` 与 `common/js/dsh-plugins/saolei-memory/`/`saolei-loop`/`grpc-runtime` 注释复查——只表述终态、不保留迁移过程表述（宪章原则 VII）
- [ ] T021 [US5] quickstart 验证收尾：按 `specs/074-dsh-grpc-runtime/quickstart.md` §1-§2 执行（`bazel build //...` + §1 全部 target 单测复跑、§2 静态检索与组合清单核对、§4 连接共享断言确认）；核对 `specs/074-dsh-grpc-runtime/contracts/preset-templates.md` §2 即插即用/组装依赖分界判据已成文（FR-024 契约落点）；T018 已完成四 suite 实际执行且 T019/T020 无代码变更——不需重跑（若意外触及代码，重跑受影响验证）

**Checkpoint**: 交付物只表述终态（SC-002/SC-003/SC-006 复核）= 特性完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1/2（Setup/Foundational）**: 无任务，直接进入 Phase 3
- **Phase 3（US1 基建）**: 结构基座——T004 依赖 T002/T003
- **Phase 4（US2 公共库）**: 依赖 Phase 3 完成（T008 起消费 `ctx.grpc` 类型语境；库本身不依赖基建运行时，但单测与后续 adapter 消费语境对齐）
- **Phase 5（US3 提供方与编排）**: 依赖 Phase 4（T012 adapter 消费公共库；T013 消费两提供面类型）
- **Phase 6（原子切换）**: 依赖 Phase 3/4/5 全部完成；**T014–T017 单一合入单元**（不得拆分）；T018 最后
- **Phase 7（US5 终态）**: 依赖 Phase 6 验收通过；纯文档

### User Story Dependencies

- **US1（P1）**: 无依赖——结构基座（MVP 起点但单独无用户可见价值，见 Implementation Strategy）
- **US2（P1）**: 依赖 US1（类型/基建语境；领域迁移本身独立可测）
- **US3（P1）**: 依赖 US2（adapter 消费公共库）；T011 与 T012 可并行后 T013 汇合
- **US4（P2）**: 随 Phase 6 原子切换落地（依赖 US3 通路就绪——模板工具行移除的前提）
- **US5（P2）**: 依赖 Phase 6 全绿

### Within Each User Story

- 契约文档（074 contracts）为唯一行为规范——实现分歧以契约为准
- 策略/语义迁移任务（T002/T003/T007/T008）零逻辑改动为硬约束，比对迁移源逐字段
- 每个 phase 完成即 `bazel build`/`bazel test`（相关 target）通过（宪法 IV，不单列 task）

### Parallel Opportunities

- T002 ∥ T003（不同文件）；T007 ∥ T008（不同文件域）；T011 ∥ T012（不同包）；T019 ∥ T020（文档域不同）
- Phase 4 与 Phase 5 的 T011 可提前（仅依赖现状代码）——但建议按序执行保持 review 粒度

---

## Parallel Example: User Story 3

```text
并行组 1（不同包，无依赖）：
Task: T011 改造 common/js/dsh-plugins/saolei/src/index.ts 为 saoleiTools 提供方
Task: T012 创建 common/js/dsh-plugins/saolei-memory/ adapter 包

汇合：
Task: T013 saolei-loop 依赖装配与编排消费（消费 T011/T012 提供面）
```

---

## Implementation Strategy

### MVP First

本特性为通路迁移（行为零回归契约），**用户可见价值在 Phase 6 原子切换落地时一次性兑现**——MVP 不是单一 story 而是完整链路：Phase 3 → 4 → 5 → 6 顺序交付，Phase 6 四 suite 全绿 = MVP 达成（SC-001）。Phase 3/4/5 为可独立 review 的安全中间态（每步全仓可构建）。

### Incremental Delivery

1. Phase 3（US1）: 基建行落地（无消费方，零行为变化）→ review/合入
2. Phase 4（US2）: memory 公共库（旧世界并行存在）→ review/合入
3. Phase 5（US3）: 提供方与编排（未激活）→ review/合入
4. Phase 6（原子切换）: **一次合入 T014–T017 → T018 四 suite 验收** = 特性行为面交付 🎯
5. Phase 7（US5）: 契约修订 + 终态清理（纯文档）

### 中断恢复

每个 phase 是独立合入单元；Phase 6 未完成 T014–T017 全部四项前不得部分合入（双重注册中间态）——中断时以 `bazel build //... && bazel test //...` 全绿 + cordis.yml 处于旧态或新态（无混合态）为恢复判据。

---

## Notes

- [P] tasks = 不同文件、无未完成依赖
- 契约文档（specs/074-dsh-grpc-runtime/contracts/）与 research.md D1–D14 是实现分歧的唯一裁决源
- 每任务完成后 commit；Phase 6 为单一合入单元（squash 或同批 PR）
- 官方 registry（`ctx.agentPresets`）在本终态方案中非依赖（research.md D13 备注）；如实现中发现 `ctx.plugin` 裸名解析障碍，回退讨论先于就地改道
- 迁移任务（T002/T003/T007/T008）的"零逻辑改动"以迁移源文件为比对基准——review 时逐字段 diff
