# Tasks: dsh Chat Demo — grpc-js 服务进程内嵌入 dsh

**Input**: Design documents from `specs/047-dsh-chat-demo/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/, quickstart.md

**Tests**: 单测按宪章原则 IV 随代码任务内联执行（`bazel build` + `bazel test` 是每个代码任务的一部分，不单列）；大型测试单独列为验收任务（原则 VI）。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

多服务 demo，路径以仓库根为基准（`third_party/dsh/`、`experimental/dsh/demo/`），结构见 `specs/047-dsh-chat-demo/plan.md` §Project Structure。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: workspace 注册、两个 TS 包骨架、应用 proto 契约落地——后续所有 phase 的物料基础。

**Independent Test**: `bazel build //experimental/dsh/demo:chat_proto` 通过；lockfile 更新后含新增 importer（见 Phase 2 T004）。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/javascript.md`（TS 包结构与 vitest 约定）
- [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的仓库 TS 规范基准；冲突时仓库文档优先）
- `style/api.md`（proto 注解规范入口）
- [AIP-136 Custom methods](https://google.aip.dev/136)（`:sendMessage` 自定义方法模式——`style/api.md` 引用的外部规范）
- [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)（`google.api.http` 注解语义——`style/api.md` 引用的外部规范）

**官方文档**：
- [rules_js: pnpm and rules_js](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md)（workspace/lockfile/link targets 机制）
- [Node.js Modules: Loading ECMAScript modules using require()](https://nodejs.org/api/modules.html#loading-ecmascript-modules-using-require)（require(esm) 语义、Node 22.12+ 默认启用——T002 CJS 方向依据，`specs/047-dsh-chat-demo/research.md` D8）
- [TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)（`module`/`moduleResolution` 编译选项语义——T002 tsconfig 依据）

**技术文章/技术参考文档**：
- `specs/047-dsh-chat-demo/plan.md`（§Technical Context、§Project Structure）
- `specs/047-dsh-chat-demo/contracts/chat-api.md`（proto 与 HTTP 契约的完整定义）
- `specs/047-dsh-chat-demo/research.md` D6/D9（底座包清单、app 命名）
- `common/js/otel/package.json`（workspace 包样板）
- `experimental/grpc_chain/echo.proto`（应用根 proto + 注解样板）
- `experimental/golang/grpc_hello_world/BUILD.bazel`（proto_library + go_proto_library + grpc-gateway/AIP 编译器样板）

### Tasks

- [X] T001 在根 `pnpm-workspace.yaml` 的 `packages` 中注册 `third_party/dsh/core` 与 `experimental/dsh/demo/agent`（Go 目录 fake-llm/gateway 不注册）；创建 `third_party/dsh/core/package.json`（name `@dominion/dsh-core`、private、精确 pin 0.1.1-rc.2 线的 11 个核心包：`@deepseek-ai/dsh-app-boot`、`@deepseek-ai/cordis`、`@deepseek-ai/cordis-plugin-loader`、`@deepseek-ai/cordis-plugin-include`、`@deepseek-ai/cordis-plugin-group`、`@deepseek-ai/cordis-plugin-timer`、`node-addon-require-builtin`、`@deepseek-ai/dsh-home-paths`、`@deepseek-ai/dsh-invariants`、`@deepseek-ai/dsh-system-prompt`、`@deepseek-ai/dsh-launch-environment`；**catalog 例外**，直接精确版本无前缀，全部精确 pin `0.1.1-rc.2`（`specs/047-dsh-chat-demo/research.md` D10 实测 API 线；若 registry 无该 patch 则取最近同线 patch 并在 PR 记录差异））+ `third_party/dsh/core/version.ts`（导出底座快照标识常量）+ `third_party/dsh/core/tsconfig.json`
- [X] T002 [P] 配置 `experimental/dsh/demo/agent/` 三件套为 CJS 方向（`specs/047-dsh-chat-demo/research.md` D8 修订版）：`package.json`（name `@dominion/dsh-demo-agent`、private、**无 `"type"` 字段——`.js` 默认 CJS**；deps：`@deepseek-ai/dsh-app-boot`、`@deepseek-ai/dsh-agent`、`@deepseek-ai/dsh-llm`、`@deepseek-ai/dsh-agent-spine-demo`、`@deepseek-ai/dsh-llm-deepseek` 精确 pin `0.1.1-rc.2`（与 T001 同线，`specs/047-dsh-chat-demo/research.md` D10） + `@grpc/grpc-js`、`@grpc/proto-loader` 用 `catalog:` + workspace 包 `@dominion/common-js-otel`、`@dominion/common-js-logs`、`@dominion/common-js-grpc-otel`、`@dominion/common-js-grpc-resolver`、`@dominion/common-js-resolver` 用 `workspace:*`；devDeps `typescript`、`@types/node` catalog）+ `experimental/dsh/demo/agent/tsconfig.json`（**`module: "commonjs"`、不设 `moduleResolution`——与 `common/js/otel/tsconfig.json` 同款**；既有 workspace 包 paths 映射保留）+ `experimental/dsh/demo/agent/.swcrc`（**`module.type: "commonjs"`**——CJS 入口）
- [X] T003 [P] 创建 `experimental/dsh/demo/chat.proto`（`package experimental.dsh.demo`、`option go_package = "dominion/experimental/dsh/demo"`；`Chat` service + `SendMessage` RPC + `google.api.http` 注解 `post: "/experimental/dsh-demo/{name=conversations/*}:sendMessage" body: "*"`，字段与注释按 `specs/047-dsh-chat-demo/contracts/chat-api.md` §2 逐字一致）+ `experimental/dsh/demo/BUILD.bazel`（`proto_library(name="chat_proto")` + `go_proto_library`（compilers 含 `go_grpc_v2`/`go_proto`/`@grpc_ecosystem_grpc_gateway//protoc-gen-grpc-gateway:go_gen_grpc_gateway`/`//:go_gen_aip`）+ `go_library` embed，样板照抄 `experimental/golang/grpc_hello_world/BUILD.bazel`）；`bazel build //experimental/dsh/demo:chat_proto //experimental/dsh/demo:chat_go_proto` 验证

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: `third_party/dsh/core` 框架核心底座（零插件）——agent 服务构建的硬前置，也是 US3 的交付物本体。

**⚠️ CRITICAL**: agent 服务（Phase 3）无法在此 phase 完成前构建。

**Independent Test**: `bazel build //third_party/dsh/core:runtime_pkg` 通过；lockfile 内全部 `@deepseek-ai/*` 同 0.1.1-rc.2 线。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/javascript.md`
- [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的仓库 TS 规范基准；冲突时仓库文档优先）

**官方文档**：
- [rules_js: pnpm and rules_js](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md)（`npm_link_all_packages` 与 `:node_modules/<pkg>` link targets）

**技术文章/技术参考文档**：
- `survey/deepseek-harness-b1-bazel-packaging.md` §3.3（link target 传递语义实证）、§5.1（闭包清单 workspace 包范式）
- `specs/047-dsh-chat-demo/data-model.md` §6（dsh 框架核心底座实体定义——T001/T005 枚举依据）
- `specs/047-dsh-chat-demo/research.md` D6
- `common/js/otel/BUILD.bazel`（`js_runtime_library` 完整范式——注意 `package_json` 为必传属性）
- `tools/release/js_runtime_library.bzl`（宏定义：`package_json`/`npm_deps`/`runtime_deps` 属性语义）

### Tasks

- [X] T004 更新 lockfile：`bazel run @pnpm -- --dir /mnt/code/dominion up`（AGENTS.md 流程），检查 `pnpm-lock.yaml` 新增 `third_party/dsh/core` 与 `experimental/dsh/demo/agent` 两个 importer、全部 `@deepseek-ai/*` 解析到同一 rc 线、`node-addon-require-builtin` 的 linux-x64-gnu optionalDep 在场（`survey/deepseek-harness-b1-bazel-packaging.md` §7 风险 2）；如有版本不一致，修正 T001/T002 的 pin 后重跑
- [X] T005 创建 `third_party/dsh/core/BUILD.bazel`：`npm_link_all_packages(name="node_modules")` + `ts_project(name="version_lib", srcs=["version.ts"], transpiler=swc, tsconfig=":tsconfig.json")` + `js_library(name="pkg", srcs=[":version_lib"])`（`npm_link_all_packages` 的 workspace:* 解析要求）+ `js_runtime_library(name="runtime_pkg", package_name="@dominion/dsh-core", lib=":version_lib", package_json="package.json", npm_deps=[11 个核心包的 `:node_modules/<pkg>` link targets 枚举], visibility=["//visibility:public"])`——**`package_json` 是 mandatory 属性**（`tools/release/js_runtime_library.bzl`）；随后 `bazel run //:gazelle third_party/dsh/core` 生成/校正 BUILD；`bazel build //third_party/dsh/core:runtime_pkg //third_party/dsh/core:pkg` 通过（构建验证内联于本任务，Constitution IV）
- [X] T006 Foundational 复核：对照 BUILD 确认 `npm_deps` 与 package.json 依赖一一对应（枚举完整性，`survey/deepseek-harness-b1-bazel-packaging.md` §3.3）——构建验证已在 T005 内联完成，本任务仅做枚举人工复核，不单独执行构建（Constitution IV）

---

## Phase 3: User Story 1 - 通过网关完成一次确定性聊天往返 (Priority: P1) 🎯 MVP

**Goal**: 三服务链路（gateway → agent → fake-llm）端到端跑通：命中模板的聊天消息经公共 HTTP 入口返回逐字一致的确定性回复。

**Independent Test**: `guitar run` 部署三服务后，`POST /experimental/dsh-demo/conversations/{id}:sendMessage` 返回模板逐字回复；重复请求同回复；未命中走兜底；空字段 400（`specs/047-dsh-chat-demo/contracts/chat-api.md` §4 US1/Edge 行）。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/golang.md`（fake-llm、gateway、testplan 用例的 Go 规范：表驱动/given-when-then/指针语义）
- [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石，normative+必读）
- [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（具体风格点决策——`style/golang.md` 引用）
- [Google Go Best Practices](https://google.github.io/styleguide/go/best-practices)（`style/golang.md` 引用，非规范但推荐）
- `style/javascript.md`（agent TS + vitest：DI mock 约定、vitest_test data 规则）
- [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 引用的仓库 TS 规范基准；冲突时仓库文档优先）
- [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` Mock 约定引用的规范依据）
- `style/api.md`（HTTP 侧行为与契约一致性）
- [AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)（`style/api.md` 引用；Phase 1 已列，本 phase HTTP 行为复核需要）
- `style/large_test.md`（testplan 组织、go_largetest、反模式——T018 起）

**官方文档**：
- [dsh-llm-deepseek README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/README.md)（适配器 config/SSE wire/错误码——agent 任务与 fake-llm wire 双向参照）
- [jsonrpc-agent minimal.cordis.yml](https://github.com/deepseek-ai/deepseek-harness/blob/master/examples/jsonrpc-agent/minimal.cordis.yml)（官方组合样例，对照 demo 两行清单）
- [agent-lifecycle.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/agent-lifecycle.md)（回合事件时序：followup→running→turn/start→assistant/message→turn/end→idle）
- [sdk/server/src/server.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts)（get-or-create 防抖、staleness 校验、shutdown 模式——T013 蓝本）
- [sdk/client/src/api.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/client/src/api.ts)（idle 终止判定与末条 assistant/message 提取——T013 蓝本）
- [llm/src/message.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm/src/message.ts)（`createUserMessage`/`ContentBlock` 形状）
- [agent-spine-demo/src/index.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/examples/agent-spine-demo/src/index.ts)（spine `Config` schema 与 `apply` 行为——T011 蓝本）

**技术文章/技术参考文档**：
- `specs/047-dsh-chat-demo/contracts/fake-llm-wire.md`（T008-T009 契约）
- `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md`（模板 schema 与匹配语义——T007/T008 契约）
- `specs/047-dsh-chat-demo/contracts/dsh-agent-service.md`（bootstrap/组合清单/驱动契约——T011-T015 契约）
- `specs/047-dsh-chat-demo/contracts/chat-api.md`（gRPC handler 行为——T014）
- `specs/047-dsh-chat-demo/research.md` D1-D5、D7-D9
- `specs/019-js-test-reliability/`（vitest 执行模型背景——`style/javascript.md` 引用；T013/T015 的 CLI/Bazel 一致性依据）
- `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim fail-closed 退出码契约——`style/javascript.md` 引用；T015 vitest_test 声明依据）
- `tools/dev/go/defs.bzl`（`go_largetest` rule 定义——`style/large_test.md` 引用；T019/T022 使用）
- 仓库样板（fake-llm 任务）：`projects/game/fake-llm/cmd/main.go`、`projects/game/fake-llm/service/handler.go`、`projects/game/fake-llm/service/message_store.go`、`projects/game/fake-llm/service/matcher.go`、`projects/game/fake-llm/service/handler_test.go`、`projects/game/fake-llm/BUILD.bazel`、`projects/game/fake-llm/service.yaml`
- 仓库样板（agent 任务）：`experimental/grpc_chain/mid/src/server.ts`、`experimental/grpc_chain/mid/src/bootstrap.ts`、`experimental/grpc_chain/mid/BUILD.bazel`、`experimental/grpc_chain/mid/service.yaml`、`projects/game/agent/src/resolver-provider.ts`
- 仓库样板（gateway/testplan 任务）：`experimental/grpc_chain/testplan/gateway/main.go`、`experimental/grpc_chain/testplan/gateway/BUILD.bazel`、`experimental/grpc_chain/testplan/gateway/service.yaml`、`experimental/grpc_chain/testplan/deploy.yaml`、`experimental/grpc_chain/testplan/interface_test.yaml`、`experimental/grpc_chain/testplan/interface_test.go`、`experimental/grpc_chain/testplan/BUILD.bazel`、`tools/test/guitar/README.md`（plan 格式与 run 语义；注意 suite 无 `env` 字段，环境名自动生成）

### Tasks

#### fake-llm（Go，可与 agent/gateway 并行；仅依赖 T003 无，实际零 proto 依赖，可最先动工）

- [X] T007 [P] [US1] 实现 `experimental/dsh/demo/fake-llm/service/message_types.go`（模板类型：name/keywords/history_keywords/min_turn/text/reasoning，字段与默认值按 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2）+ `message_store.go`（YAML/JSON `messages:` 列表加载，`go:embed` testdata，多文件合并）+ `matcher.go`（**单轮关键词匹配**：任一 keyword 子串命中最后一条 user 消息 + 确定性兜底选择）+ 表驱动单测（`style/golang.md` 单测规范；本 phase 不实现多轮条件，留 T021）
- [X] T008 [P] [US1] 实现 `experimental/dsh/demo/fake-llm/service/handler.go`（`POST /v1/chat/completions`：`stream:true` 走 SSE 帧序列、`stream:false` 单 JSON、`GET /health`；忽略 `model` 与 `authorization`/`x-deepseek-harness-*`/attribution header；SSE 不变量按 `specs/047-dsh-chat-demo/contracts/fake-llm-wire.md` §3：role 首帧→content delta→finish+usage 帧→`data: [DONE]` 终帧）+ `startup.go` + 单测（`httptest.Server` 真实 transport，样板 `projects/game/fake-llm/service/handler_test.go`；依赖 T007 的类型定义完成）
- [X] T009 [P] [US1] 创建 `experimental/dsh/demo/fake-llm/service/testdata/chat.yaml`（US1 模板组：`greeting`（keywords:[hello]）、`chat-only`（keywords:[chat]）、`farewell`（keywords:[]——**纯兜底模板**，按 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2/§3.3 的唯一空 keywords 非多轮模板，是 US1-3 兜底断言的确定性锚点）；内容沿用母本文案保证可读性）并纳入 embed（依赖 T007 的 schema 定义完成）
- [X] T010 [US1] 实现 `experimental/dsh/demo/fake-llm/cmd/main.go`（`common/gopkg/bootstrap` + otel + `phttp.Handler(mux, "fake-llm")`，样板 `projects/game/fake-llm/cmd/main.go`）+ `experimental/dsh/demo/fake-llm/BUILD.bazel`（`go_library`/`go_test` + `artifact_pkg_go` + `artifact_image`）+ `experimental/dsh/demo/fake-llm/service.yaml`（app `dsh-demo`、name `fake-llm`、kind stateless、port http 8080、无 tls）+ `experimental/dsh/demo/fake-llm/README.md`（Constitution VI 豁免声明，参照 `projects/game/fake-llm/README.md` §Large-test exemption）；`bazel run //:gazelle` + `bazel build`/`bazel test` 全绿

#### agent（grpc-js/TS，串行链，依赖 Phase 2 完成）

- [X] T011 [US1] 创建 `experimental/dsh/demo/agent/cordis.yml`（两行：`agent-spine` 五裁剪键 + `llm-deepseek` 的 `apiKeyEnv: FAKE_LLM_API_KEY`/`baseURL: !!js process.env.FAKE_LLM_BASE_URL`/`models: [{id: fake-chat-v1, contextWindow: 100000}]`——逐字按 `specs/047-dsh-chat-demo/contracts/dsh-agent-service.md` §2；CJS 方向下服务根无需任何 package.json data 文件，`specs/047-dsh-chat-demo/research.md` D8 修订版）
- [X] T012 [US1] 实现 `experimental/dsh/demo/agent/src/dsh.ts`：`bootDsh()` 封装——`createResolver().resolve("dominion:///dsh-demo/fake-llm:8080")` → `process.env.FAKE_LLM_BASE_URL = http://<endpoints[0]>/v1` → `boot("dsh-demo-agent", <产物内 cordis.yml 绝对路径——由 `__filename` 推导（CJS 全局，替代 ESM 的 `fileURLToPath(import.meta.url)`）>, undefined, undefined, pathToFileURL(__filename).href)`（`boot` 自 `@deepseek-ai/dsh-app-boot`；`pathToFileURL` 自 `node:url`——CJS 入口的 `bareModuleBaseUrl` 锚点，与 ESM `import.meta.url` 等价，`specs/047-dsh-chat-demo/research.md` D8 修订版）→ 返回 Context；boot 失败 catch 后打诊断日志并 `process.exit(1)`（fail-loud，FR-009）；单测 `dsh.test.ts`（`vi.spyOn(process, "exit")` 断言两条失败路径——boot 抛错、cordis.yml 引用未声明插件名（不在物化闭包）——均打诊断日志 + exit(1)，覆盖 FR-009 与 US3 验收场景 3）
- [X] T013 [US1] 实现 `experimental/dsh/demo/agent/src/session.ts`：`AgentSessions` 服务（构造注入 Context，依赖注入 seam 便于 `vi.fn()` mock——`style/javascript.md` Mock 约定）——`send(conversationId, text)`：get-or-create（`ctx.agents.get` 命中复用；未命中 `ctx.agents.create({sessionId, meta:{cwd}, agentOptions:{provider:"deepseek-official", model:"fake-chat-v1"}})`；并发同 id 经单一创建 promise 防抖 + staleness 复验，蓝本 [sdk/server.ts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/server/src/server.ts)）→ `agent.followup(createUserMessage({content:[{type:"text",text}],source:{kind:"user"}}))` → 回合终止（`agent/status`→idle 事件或 `await agent.whenIdle()`）→ reply = 本回合**末条** `assistant/message` 的 text blocks 拼接（蓝本 [api.ts finalResponse](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/client/src/api.ts)）；shutdown 时逐 agent `handle.dispose()` + `ctx.fiber.dispose()`；单测 `session.test.ts`（mock Agent/事件流覆盖：新建/复用/并发防抖/多 assistant 取末条/无 assistant 空串/**followup 失败（fake-llm 不可达）→ send 返回错误且会话仍可复用、后续请求正常——Edge Case 的 500 + 进程存活**）
- [X] T014 [US1] 实现 `experimental/dsh/demo/agent/src/server.ts`（grpc-js `Chat` 服务：protoLoader 加载 runtime proto（定位用 `path.dirname(__filename)`——CJS 全局直接可用，替代 ESM 的 `path.dirname(fileURLToPath(import.meta.url))`；样板 `experimental/grpc_chain/mid/src/server.ts` 的 `__dirname` 同款写法）+ `SendMessage` handler：从 `name` 提取 `conversations/` 后缀为会话 id、校验非空（空 → INVALID_ARGUMENT）→ `sessions.send()` → 回填 `SendMessageResponse`；TLS opportunistic `buildServerCredentials()`；单测：agent 错误 → `INTERNAL`（gRPC 状态码）映射，服务不退出）+ `experimental/dsh/demo/agent/src/bootstrap.ts`（otel init（`createGrpcInstrumentation`）→ `bootDsh()` → 动态 `import("./server.js")` → `startServer({ctx})` → SIGTERM/SIGINT 优雅退出链）
- [X] T015 [US1] 创建 `experimental/dsh/demo/agent/BUILD.bazel`：`npm_link_all_packages` + `ts_proto_library(name="chat_types", proto="//experimental/dsh/demo:chat_proto")` 包 `js_library(name="chat_types_lib")` + `ts_project(name="server_lib", srcs=[src/*.ts 排除 *.test.ts], transpiler=swc, tsconfig=":tsconfig.json", deps=[chat_types_lib + 全部 `:node_modules/*` 直接依赖])` + `vitest_test(name="lib_test", data=glob(src/**/*.ts)+镜像 node_modules deps)`（`style/javascript.md` data 规则）+ `artifact_pkg_js(name="server_pkg", app="dsh-demo", service="agent", ts_project=":server_lib", entrypoint="src/bootstrap.js", runtime_protos=["//experimental/dsh/demo:chat_proto"], runtime_deps=["//third_party/dsh/core:runtime_pkg", "//common/js/otel:runtime_pkg", "//common/js/logs:runtime_pkg", "//common/js/grpc/otel:runtime_pkg", "//common/js/grpc/resolver:runtime_pkg", "//common/js/resolver:runtime_pkg"], npm_deps=[app-boot/dsh-agent/dsh-llm/spine/llm-deepseek/grpc-js/proto-loader 的 link targets], data_files=["cordis.yml"])` + `artifact_image(name="cmd_image", ...)`；`bazel run //:gazelle experimental/dsh/demo/agent` 后校正；`bazel build //experimental/dsh/demo/agent:cmd_image` 通过（构建验证内联于本任务，Constitution IV）；创建 `experimental/dsh/demo/agent/service.yaml`（app `dsh-demo`、name `agent`、kind stateless、port grpc 50051、artifact tls）
- [X] T016 [US1] US1 汇聚复核门禁：核对三个镜像产物均已生成（fake-llm 由 T010、agent 由 T015、gateway 由 T017 各自内联构建验证）且 agent/fake-llm 单测全绿（T010/T013/T015 内联执行）——本任务仅做交叉复核，不单独执行构建/测试（Constitution IV）；发现问题回到对应任务修复

#### gateway（Go，独立并行；依赖 T003 proto）

- [X] T017 [P] [US1] 实现 `experimental/dsh/demo/gateway/main.go`（grpc-gateway v2：`solver.URI("dsh-demo/agent:grpc")` 拨号 + `runtime.NewServeMux(pgrpc.GatewayDefault()...)` + `RegisterChatHandler` + `phttp.Handler(mux, "dsh-demo-gateway")` + `bootstrap` 组件注册，逐段照抄 `experimental/grpc_chain/testplan/gateway/main.go` 样板）+ `experimental/dsh/demo/gateway/BUILD.bazel`（`go_library`/`go_binary` + `artifact_pkg_go` + `artifact_image`，deps 含 `//experimental/dsh/demo:chat`（go_library）与 `//common/gopkg/{bootstrap,grpc,grpc/solver,http,otel}`）+ `experimental/dsh/demo/gateway/service.yaml`（name `gateway`、port http 80、kind stateless、tls）；`bazel run //:gazelle` + build 全绿

#### testplan（US1 验收）

- [X] T018 [US1] 创建 `experimental/dsh/demo/testplan/deploy.yaml`（version 3.0、type test、name `dsh-demo.{{run}}`；services：fake-llm、agent、gateway + ingress `http.hostnames:[apitest.liukexin.com]` + `matches: PathPrefix /experimental/dsh-demo`；样板 `experimental/grpc_chain/testplan/deploy.yaml`）+ `experimental/dsh/demo/testplan/interface_test.yaml`（**单份计划**，suite `default`：deploy + endpoint `http.public: https://apitest.liukexin.com` + cases `[testplan_test]`——后续 US2 以追加 case 方式扩展，`style/large_test.md` 反模式 1/4）+ `experimental/dsh/demo/testplan/BUILD.bazel`（`go_largetest` targets；至少一个 target 用目录默认名 `testplan_test` 防 gazelle 重复生成）
- [X] T019 [US1] 实现 `experimental/dsh/demo/testplan/chat_test.go`（`go_largetest(name="testplan_test")`；`testtool.MustEndpoint("http","public")` + `MustEnv()` + `x-dominion-env` header + `tracecontext`（`style/large_test.md`）；用例：单轮命中模板逐字断言 / 同消息重复确定性 / 未命中走兜底（期望文本 = `farewell`.text，纯兜底模板，`specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3.3/§4）/ 空字段 400、坏资源名（不匹配 `conversations/*`/空会话 id）404（路由层拒绝）——按 `specs/047-dsh-chat-demo/contracts/chat-api.md` §4；given/when/then + 表驱动，`style/golang.md`。注：chat-api §4 Edge 行"fake-llm 不可达 → 500 且进程存活"由 T013/T014 单测覆盖（testplan 部署环境无法在计划内停服，`style/large_test.md` 单份计划原则））
- [X] T020 [US1] **US1 大型测试验收门禁**：经 testplan skill 执行 `guitar run experimental/dsh/demo/testplan/interface_test.yaml`（先 `bazel run //:guitar_install` / `//:deploy_install` 若未装），完成部署→测试→清理闭环，**全部用例通过**（零 failed/flaky）；失败时用 signoz skill 查 `dsh-demo/*` 日志/tracing 定位（重点：`FAKE_LLM_BASE_URL` 注入日志、native addon 加载、组合 fail-loud 诊断），修复后重跑至全绿

**Checkpoint**: User Story 1（MVP）端到端可验收——公共 HTTP 入口的确定性聊天往返全绿。

---

## Phase 4: User Story 2 - 多轮会话连续性 (Priority: P2)

**Goal**: 同一会话多轮上下文可见（多轮分支模板命中），跨会话隔离，并发会话无串扰。

**Independent Test**: 同会话第二轮命中 `greeting-again`（多轮分支）而新会话同消息命中 `greeting`（首轮分支）；两会话交错各自正确（`specs/047-dsh-chat-demo/contracts/chat-api.md` §4 US2 行）。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/golang.md`
- [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石，normative+必读）
- [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（具体风格点决策——`style/golang.md` 引用）
- `style/large_test.md`

**官方文档**：
- 无（本 phase 无新增第三方组件依赖）

**技术文章/技术参考文档**：
- `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §2-§4（多轮条件与优先级语义、验收场景↔模板映射）
- `specs/047-dsh-chat-demo/contracts/chat-api.md` §4
- `specs/046-fake-llm-think-chunking/contracts/template-config.md`（母本模板作者面契约参照）

### Tasks

- [ ] T021 [US2] 扩展 `experimental/dsh/demo/fake-llm/service/matcher.go`：多轮条件匹配（`history_keywords` 全部命中"除最后一条 user 消息外"的历史、`min_turn` 轮次下限；优先级：多轮条件模板 > 纯关键词 > 兜底，多轮并列取声明条件数多者再按 name 字典序——按 `specs/047-dsh-chat-demo/contracts/fake-llm-templates.md` §3 逐条实现）+ `experimental/dsh/demo/fake-llm/service/testdata/chat.yaml` 增加 `greeting-again` 模板（keywords:[hello] + history_keywords:[hello] + min_turn:2，text 见契约 §1 示例）+ 表驱动单测（首轮/二轮/隔离/条件不满足回退）
- [ ] T022 [US2] 实现 `experimental/dsh/demo/testplan/multiturn_test.go`（`go_largetest(name="multiturn_test")`，并把该 target **追加**进 `interface_test.yaml` 既有 suite 的 cases——不新建计划/套件）：多轮分支（同会话两轮 "hello" → 第二轮 `greeting-again`）、会话隔离（新会话同消息 → `greeting`）、并发交错（两会话交替各轮正确断言）
- [ ] T023 [US2] **US2 大型测试验收门禁**：`guitar run experimental/dsh/demo/testplan/interface_test.yaml` 全部用例（含 US1 回归）通过

**Checkpoint**: US1 + US2 均独立可验收。

---

## Phase 5: User Story 3 - dsh 依赖底座可复用且仅含框架核心 (Priority: P3)

**Goal**: 底座闭包审计通过：core 零插件包、服务闭包内每个 dsh 包可溯源、同名包版本唯一（SC-004）。

**Independent Test**: `bazel test //experimental/dsh/demo/testplan:closure_audit_test` 通过（三项审计断言全过）。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/golang.md`
- [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石，normative+必读）
- [Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)（具体风格点决策——`style/golang.md` 引用）

**官方文档**：
- 无

**技术文章/技术参考文档**：
- `specs/047-dsh-chat-demo/contracts/dsh-agent-service.md` §4（审计断言定义）
- `specs/047-dsh-chat-demo/data-model.md` §6（底座实体定义——审计断言②的期望集依据）
- `survey/deepseek-harness-b1-bazel-packaging.md` §3.2/§3.3（tar 物化实证、link target 语义）、§5.6.2（同名包多版本静默覆盖风险）
- `tools/release/defs.bzl`（`_collect_runtime_closure` L34/L383-411、Phase 3 npm 拍平 L446-473 与 L546 `cp -aL`、Phase 5 data_files——审计脚本的机制依据）
- `tools/release/js_runtime_library.bzl`

### Tasks

- [ ] T024 [US3] 实现 `experimental/dsh/demo/testplan/closure_audit_test.go`（普通 `go_test`，`data = [//experimental/dsh/demo/agent:server_pkg tar, //third_party/dsh/core:BUILD.bazel, //third_party/dsh/core:package.json]`）：① 解析 core BUILD 的 `npm_deps` 与 package.json——断言集合内无任何插件包（插件 = 不在 D6 核心清单内的 `@deepseek-ai/dsh-*`）；② 展开 agent server_pkg tar 的 `node_modules/`——断言每个 `@deepseek-ai/*` 与 `node-addon-require-builtin` 包 ∈ {核心 11 包 ∪ 服务 BUILD `npm_deps` 声明的 peer 闭包到不动点}（服务闭包期望集从 agent package.json 直接依赖 + 其 peers 递归生成，双向断言：tar 实际集合 ⊇ cordis.yml 启用行所需 且 ⊆ 核心∪声明的传递闭包）；③ tar 内同名包（从 store 路径 `name@version` 或 package.json version 提取）版本唯一；完成后 `bazel test //experimental/dsh/demo/testplan:closure_audit_test` 通过（测试执行内联于本任务，Constitution IV）
- [ ] T025 [US3] US3 验收复核：确认三项审计断言全绿（`closure_audit_test` 已在 T024 内联执行）——本任务仅做验收复核，不单独执行测试（Constitution IV）；若断言②失败说明闭包有第三来源，回查 T015 的 `npm_deps`/`runtime_deps` 声明后修复 T024 重跑

**Checkpoint**: 三个 user story 全部独立可验收。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: demo 顶层文档与最终全量验收。

### 文档清单（本 phase 必读）

**代码规范文档**：
- `style/large_test.md`

**官方文档**：
- 无

**技术文章/技术参考文档**：
- `specs/047-dsh-chat-demo/quickstart.md`（验收对照表）
- `projects/game/fake-llm/README.md`（Constitution VI 豁免先例——已在 T010 应用于 fake-llm README，此处对照复核）
- `tools/test/guitar/README.md`

### Tasks

- [ ] T026 [P] 创建 `experimental/dsh/demo/README.md`：拓扑图（gateway → agent → fake-llm）、三服务与底座 target 一览表（bazel target/端口/寻址）、构建/单测/审计/大型测试命令（对齐 `specs/047-dsh-chat-demo/quickstart.md`）、已知限制（无 compaction/无会话持久化/0.x-rc 漂移成本——`specs/047-dsh-chat-demo/spec.md` Assumptions）、fake-llm 大型测试豁免说明链接
- [ ] T027 **最终验收门禁**（FR-008 / Constitution VI）：`bazel build //experimental/dsh/... //third_party/dsh/core/...` + `bazel test //experimental/dsh/... //third_party/dsh/core/...` 全绿；`guitar run experimental/dsh/demo/testplan/interface_test.yaml` 全部用例通过（US1+US2 全回归 + 清理闭环）；`bazel test //experimental/dsh/demo/testplan:closure_audit_test` 通过——记录执行证据于 PR 描述

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，立即开始（T001∥T002∥T003 可并行）
- **Foundational (Phase 2)**: T004 依赖 T001+T002（lockfile 一次解析两个 importer）；T005 依赖 T004；T006 依赖 T005
- **fake-llm 组（T007-T010）**: 零 proto/底座依赖，**可与 Phase 1/2 全程并行**
- **gateway（T017）**: 仅依赖 T003（proto codegen）
- **agent 链（T011-T015）**: 硬依赖 Phase 2 完成；串行 T011→T012→T013→T014→T015
- **testplan（T018-T020）**: 依赖 T016/T017（镜像可构建）
- **US2（Phase 4）**: 依赖 T020 验收通过
- **US3（Phase 5）**: 依赖 T015（server_pkg tar 产物存在）
- **Polish (Phase 6)**: 依赖全部 story 完成

### User Story Dependencies

- **US1 (Phase 3)**: fake-llm 组（T007→T008∥T009→T010）∥ gateway（T017）∥ agent 链（T011→…→T015）→ T016 汇聚复核 → T018→T019→T020 大型测试验收
- **US2 (Phase 4)**: 依赖 US1 验收（T020）；T021→T022→T023
- **US3 (Phase 5)**: 依赖 T015；T024→T025；与 US2 无依赖、可并行
- **Polish**: 依赖全部

### Within Each User Story

- 先底层后上层：类型/store → handler/驱动 → 服务装配 → BUILD/打包 → 大型测试
- 每个 [US] 代码任务自带 `bazel build`/`bazel test`（Constitution IV，不单列）
- 大型测试验收任务（T020/T023/T027）是各 story 的完成判据；T006/T016/T025 为人工复核检查点（构建/测试已内联于实现任务，Constitution IV）

### Parallel Opportunities

- Phase 1：T001∥T002∥T003（不同文件）
- T007（类型/store）完成后：T008∥T009（handler 与 testdata 不同文件）
- 三条独立构建面：fake-llm 组 ∥ gateway（T017）∥ core baseline（Phase 2）∥（Phase 2 后）agent 链
- US3（T024-T025）与 US2（T021-T023）无相互依赖，可并行推进
- T026 可与 T024/T025 并行

---

## Parallel Example: User Story 1

```bash
# fake-llm 类型/store 就绪（T007）后，并行启动：
Task: "T008 handler + SSE 单测（experimental/dsh/demo/fake-llm/service/handler.go）"
Task: "T009 testdata chat.yaml（experimental/dsh/demo/fake-llm/service/testdata/）"

# 三条互不相干的构建面并行（不同目录、不同文件）：
Task: "T007-T010 fake-llm（Go）"
Task: "T017 gateway（Go，依赖 T003）"
Task: "T004-T006 core baseline（Phase 2）"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 Setup（T001-T003）→ Phase 2 Foundational（T004-T006）；fake-llm/gateway 并行推进
2. Phase 3 US1：三面汇聚 → T016 汇聚复核 → T020 大型测试全绿
3. **STOP and VALIDATE**: MVP 可演示（gateway 确定性聊天往返）

### Incremental Delivery

1. Setup + Foundational → 底座可用（本身已是 US3 交付物的一半）
2. + US1 → MVP 大型测试全绿
3. + US2 → 多轮/隔离/并发用例进既有计划并全绿
4. + US3 → 闭包审计通过（可与 US2 并行）
5. + Polish → 全量回归 + 文档，FR-008 最终验收

### Parallel Team Strategy

1. 团队共同完成 Setup + Foundational
2. 完成后：开发者 A → fake-llm；开发者 B → agent 链；开发者 C → gateway + testplan 骨架
3. T020 汇聚验收后，US2 与 US3 可由两人并行推进

---

## Notes

- [P] 标记前提是"不同文件且无未完成依赖"——任务描述中的 [P] 已按此判定，并列任务共享的前置完成即可并行
- dsh 0.x-rc 上游 API 可能漂移：T012-T014 以研究阶段实测的 0.1.1-rc.2 API 为准（`specs/047-dsh-chat-demo/research.md` D10）；若实际包类型签名不一致，以 node_modules 内 `.d.ts` 为最终依据并在 PR 记录差异
- 大型测试执行必须经 testplan skill（`guitar run`），禁止以 `bazel build` 测试 target 替代（Constitution VI）
- 单份测试计划原则：US2/后续功能一律以 suite/case 追加进 `interface_test.yaml`，不新建计划 YAML（`style/large_test.md`）
- **TLA 漂移风险（CJS 方向，`specs/047-dsh-chat-demo/research.md` D8 修订版）**：dsh 升级后若静态依赖引入 top-level await，require(esm) 启动将报 `ERR_REQUIRE_ASYNC_MODULE`——届时对该包局部改动态 `import()`；启动路径由 T020 大型测试覆盖
- **node10 subpath 盲区（CJS 方向，同 D8）**：`module: "commonjs"` 的默认解析不含 package.json `exports` subpath——未来若需导入 dsh 包的 subpath 导出，评估切 `module: nodenext` 或改用包顶层入口；当前 5 个静态依赖包仅用主入口，不受影响
