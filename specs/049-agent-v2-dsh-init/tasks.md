# Tasks: Game Agent v2 — dsh 迁移 Step 1：session 对话页面与模型接入

**Input**: Design documents from `/specs/049-agent-v2-dsh-init/`

**Prerequisites**: plan.md（已读）、spec.md（已读）、research.md、data-model.md、contracts/、quickstart.md（全部已读，本文件引用处均标注路径）

**Tests**: 单测不单列 task——编译+单测（`bazel build` + `bazel test`）是每个实现 task 的内嵌完成门禁（`.specify/memory/constitution.md` 原则 IV）；大型测试单独成 phase 作为验收（原则 VI 允许）。

**Organization**: 按 user story 分 phase（US1/US2/US3/US4 对应 spec.md 优先级 P1/P1/P2/P2）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US4）；Setup/Foundational/大型测试 phase 无 story 标签
- 所有路径为仓库相对路径

## 通用约定（所有 phase 生效）

- 新 TS 包全部原生 ESM：`"type": "module"` + tsconfig `module: nodenext`（相对导入带 `.js` 后缀）+ `.swcrc` `{"module":{"type":"es6","preserveImportMeta":true},"jsc.target":"es2020"}`（`specs/048-js-esm-migration/contracts/esm-package-conventions.md`，样板文件 `experimental/dsh/demo/agent/.swcrc`）。
- 依赖声明：catalog 统一管理；dsh 家族（`@deepseek-ai/*`）与 `@dominion/dsh-llm-glm` 消费按精确版本/`workspace:*`（先例 `third_party/dsh/core/package.json`）。
- 每次新增依赖后：目标目录 `bazel run //:gazelle <dir>` 更新 BUILD；`pnpm up` 更新 lockfile（`AGENTS.md`）。
- 注释引用带完整路径/URL（constitution 原则 I）；只表述终态（原则 VII）。

---

## Phase 1: Setup（共享骨架与 workspace 注册）

**Purpose**: 四个新包的骨架、workspace 注册、catalog/常量增量——不含业务逻辑。

### 文档清单（本 phase 必读）

- **代码规范**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：无（包骨架无第三方 API 调用）
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（包级 ESM 契约）；`specs/050-vite-react-bazel/contracts/vite-build-target.md`（React 前端包构建契约）；`specs/002-deploy-secret-config/contracts/secret-config.md`（service/deploy secret 契约，T003 对照 §1）；样板源码：`pnpm-workspace.yaml`（workspace/catalog 活样板）、`third_party/dsh/core/package.json`（dsh 依赖 pin 先例）、`experimental/dsh/demo/agent/package.json` 与 `experimental/dsh/demo/agent/.swcrc`（服务包骨架对照）、`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm-deepseek/package.json`（eventsource-parser 版本对照）、`projects/game/session/service.yaml`（service.yaml secret 声明对照）、`experimental/js/vite_react_demo/package.json`（前端包 devDeps 对照）、`experimental/js/vite_react_demo/server/BUILD.bazel` 与 `experimental/js/vite_react_demo/server/assets/BUILD.bazel`（Go 静态服务打包样板）

- [X] T001 在 `pnpm-workspace.yaml` 的 `packages` 增加 `projects/game/agent_v2` 与 `projects/game/web/frontend`（`common/js/dsh-plugins/*` 已被 `common/js/**` 覆盖，无需条目），在 `catalog` 增加 `eventsource-parser: ^3.1.0`（对齐官方 `dsh-llm-deepseek` deps 声明，`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm-deepseek/package.json`）；执行 `pnpm up` 更新 lockfile
- [X] T002 [P] 创建插件包骨架 `common/js/dsh-plugins/llm-glm/`：`package.json`（name `@dominion/dsh-llm-glm`、`private`、`"type":"module"`、deps：`eventsource-parser: catalog:` + `@deepseek-ai/schemastery: ^3.18.1`；peers：`@deepseek-ai/dsh-llm: 0.1.1-rc.2` + `@deepseek-ai/cordis: ^4.0.1`）、`tsconfig.json`（nodenext，paths 指向 `@deepseek-ai/dsh-llm` 物化路径按 gazelle 生成）、`.swcrc`（es6+preserveImportMeta）、`BUILD.bazel`（`npm_link_all_packages` + 空 `ts_project` 占位，gazelle 后补全）
- [X] T003 [P] 创建 agent_v2 服务包骨架 `projects/game/agent_v2/`：`package.json`（name `@dominion/game-agent-v2`、deps：`@deepseek-ai/dsh-agent`/`dsh-agent-spine-demo`/`dsh-app-boot`/`dsh-llm` 精确 pin `0.1.1-rc.2`、`@dominion/dsh-llm-glm: workspace:*`、`@dominion/common-js-{otel,logs,resolver,grpc-otel,grpc-resolver}: workspace:*`、`@grpc/grpc-js`/`@grpc/proto-loader: catalog:`——对照 `experimental/dsh/demo/agent/package.json`）、`tsconfig.json`、`.swcrc`、`service.yaml`（app `game`、name `agent_v2`、grpc 50051、`tls: true`、artifacts secrets `["glm-api-token"]`——对照 `projects/game/session/service.yaml` + `specs/002-deploy-secret-config/contracts/secret-config.md` §1）、`BUILD.bazel` 基础（`npm_link_all_packages` + `ts_config`）
- [X] T004 [P] 创建 web 前端包骨架 `projects/game/web/frontend/`：`package.json`（name `@dominion/game-web`、deps：`react`/`react-dom: catalog:` + `@deepseek-ai/dsh-client-ui-primitives: 0.1.1-rc.2`（精确 pin，catalog 例外记录于包 README）；devDeps：`@vitejs/plugin-react`/`@types/react`/`@types/react-dom`/`@testing-library/react`/`@testing-library/dom`/`jsdom`/`vite`/`vitest`/`typescript: catalog:`——对照 `experimental/js/vite_react_demo/package.json`）、`vite.config.ts`（`plugins:[react()]`）、`index.html`、`tsconfig.json`（`jsx: "react-jsx"`）、`src/` 空入口、`BUILD.bazel`（`npm_link_all_packages` + `vite_build` name `dist` + `vitest_test` name `lib_test`，按 `specs/050-vite-react-bazel/contracts/vite-build-target.md` 属性表）
- [X] T005 [P] 创建 web 服务骨架 `projects/game/web/server/`：`main.go`（`embed` 静态托管骨架，占位 404 handler）+ `assets/BUILD.bazel`（`wails_asset_library` name `assets`，src 先占位指向 `//projects/game/web/frontend:dist`）+ `BUILD.bazel`（go_library/go_binary/`go_unittest`/`artifact_pkg_go`/`artifact_image`，app `game`，对照 `experimental/js/vite_react_demo/server/BUILD.bazel` 与 `server/assets/BUILD.bazel`——注意 `//go:embed` 不支持 `..`，故 assets 独立 BUILD 包）+ `service.yaml`（app `game`、name `web`、http 8080）
- [X] T006 [P] 在 `projects/game/pkg/gameconst/const.go` 增加 `AgentV2Target = "game/agent_v2:grpc"`（含注释：agent_v2 服务发现目标，spec FR-013）；`bazel test //projects/game/pkg/gameconst/...`
- [X] T007 [P] 在 `projects/game/web/frontend/README.md` 与 `common/js/dsh-plugins/llm-glm/README.md` 写包说明：用途、依赖 pin 决策依据（`specs/049-agent-v2-dsh-init/research.md` D1/D8）、ui-primitives BSD-3-Clause attribution 与参照源码链接

**Checkpoint**: `bazel build //projects/game/agent_v2/... //projects/game/web/... //common/js/dsh-plugins/...` 通过；`pnpm install` 无错误。

---

## Phase 2: Foundational（阻塞全部 user story 的公共底座）

**Purpose**: proto 契约代码生成、GLM 插件完整实现、fake Responses 端点——三者互相独立可并行。

### 文档清单（本 phase 必读）

- **代码规范**：`style/api.md`；`style/javascript.md`；`style/golang.md`；[AIP-122 Resource names](https://google.aip.dev/122)；[AIP-126 Enumerations](https://google.aip.dev/126)；[AIP-136 Custom methods](https://google.aip.dev/136)；[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：[dsh cookbook: adding an LLM adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)；[OpenAI OpenAPI openapi.yaml（Responses streaming events 定义）](https://github.com/openai/openai-openapi/blob/main/openapi.yaml)；[GLM Coding Plan 接入文档](https://docs.bigmodel.cn/cn/coding-plan/tool/others)
- **技术文章/技术参考文档**：`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`；`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`；`specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md`；`specs/047-dsh-chat-demo/research.md`（D1/D4 适配缝与组合清单实证）；`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/index.d.ts` 与 `lib/types/types.d.ts`（LlmAdapter/StreamChunk 契约原文，物化源码）；`experimental/dsh/demo/chat.proto` + `experimental/dsh/demo/BUILD.bazel`（ts_proto_library 样板）；`projects/game/BUILD.bazel`（go_proto_library 样板）；`projects/game/fake-llm/README.md` 与 `projects/game/fake-llm/service/`（既有 fake 设施）

- [X] T008 编写 proto（package `projects.game.v2`、`go_package "dominion/projects/game/v2"`、Service/Method 注释含 Prefix Path——内容逐字段照 `specs/049-agent-v2-dsh-init/contracts/conversation-api.md` §1 的完整 proto；proto 文件现位于 `projects/game/agent_v2.proto`——T039 归位，内容不变）+ `proto_library` + `go_proto_library`（compilers: go_grpc_v2/go_proto/grpc-gateway/go_gen_aip，对照 `projects/game/BUILD.bazel` 的 `game_go_proto`）+ `go_library` + `ts_proto_library` + `js_library`（对照 `experimental/dsh/demo/agent/BUILD.bazel` 的 `chat_types`/`chat_types_lib`；proto/go 目标现位于 `projects/game/BUILD.bazel`、ts 目标位于 `projects/game/agent_v2/BUILD.bazel`——T039 归位）；`bazel build //projects/game/...`
- [X] T009 [P] 实现插件 `common/js/dsh-plugins/llm-glm/src/wire.ts`：Responses SSE 事件类型定义 + `eventsource-parser` 解析 + 事件→StreamChunk 映射（映射表逐行照 `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §5，含 reasoning 双词汇容差与未知事件忽略）+ `wire.test.ts`（构造 SSE 帧序列断言 chunk 序：index 分配、usage-先-finish、completed/incomplete/failed 三终局）；`bazel test //common/js/dsh-plugins/llm-glm:lib_test`
- [X] T010 [P] 实现插件 `common/js/dsh-plugins/llm-glm/src/serialize.ts`：`GenerateOptions` → Responses 请求体（user/assistant 消息映射、system→instructions、reasoning 不回传、image/tool 内容 throw `UNSUPPORTED_CONTENT`——`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §4）+ `serialize.test.ts`（多轮往返断言）
- [X] T011 实现插件 `common/js/dsh-plugins/llm-glm/src/adapter.ts` + `src/index.ts`：`GlmResponsesAdapter extends LlmAdapter`（`stream()` 组装 serialize→fetch(SSE)→wire chunk 流；`options.signal` 透传；attribution headers；协议义务逐条落实 cookbook）与 cordis 导出（`name='llm-glm'`/`inject=['llm']`/schemastery `Config`/`apply` 注册 route `glm-responses`）+ `adapter.test.ts`（协议义务回归：finish 后零输出、delta index 复用、HTTP 非 200 throw LlmError、signal abort；DI seam mock fetch——`style/javascript.md` Mock 约定）；完善 `common/js/dsh-plugins/llm-glm/BUILD.bazel`（ts_project deps 含 `:node_modules/@deepseek-ai/dsh-llm` + vitest_test data 镜像）；`bazel build/test //common/js/dsh-plugins/llm-glm:lib_test`（gazelle 目录名映射按需 `# gazelle:resolve`）
- [X] T012 [P] 扩展 `projects/game/fake-llm/`：`service/` 新增 Responses 端点 handler（`POST /v1/responses`：input 解析只取 message 文本、model 忽略、SSE 事件发射照 `specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md` §2 事件序与不变式，复用既有 `matcher.go` 模板/多轮条件/延迟设施并按 §3 投影 think/text 分帧）+ `service/testdata/` 新增模板（think+text 含 history_keywords 变体、纯 text、长延迟、失败注入）+ Go 单测（`style/golang.md` §单元测试：表驱动、given/when/then）；注册路由于 `cmd/main.go` 或既有注册点；`bazel build/test //projects/game/fake-llm/...`；既有 chat-completions 用例零改动回归

**Checkpoint**: `bazel build //... && bazel test //common/js/dsh-plugins/... //projects/game/fake-llm/... //projects/game/agent_v2/...` 全绿；插件三模块（wire/serialize/adapter）与 fake 端点单测通过。

---

## Phase 3: User Story 1 — 网页新建 session 并完成一次模型对话 (Priority: P1) 🎯 MVP

**Goal**: agent_v2 宿主完整落地（boot/队列/历史/流式转发/gRPC 面），bind server-streaming 扩展，proxy ConversationService 转发面（owner 亲和），gateway `/api/v2` 增量（经 proxy 两跳），web 前端对话页（text 流式）与最小 session 创建，game 部署增量——端到端可对话。

**Independent Test**: 测试环境部署后经 gateway `POST /api/v2/templates/saolei/sessions/{s}:send` 发送消息，收到 `turn_start→TEXT delta 多帧→turn_end{COMPLETED}` 事件流；浏览器页面渐进呈现回复（详细步骤 `specs/049-agent-v2-dsh-init/quickstart.md` §2/§3）。

### 文档清单（本 phase 必读）

- **代码规范**：`style/javascript.md`；`style/golang.md`（gateway/proxy/bind Go 面）；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[dsh cookbook: adding an LLM adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)（宿主注入模式：`!!js process.env` env fallback）；[MDN ReadableStream](https://developer.mozilla.org/en-US/docs/Web/API/ReadableStream)（NDJSON 消费 reader API）；[React API Reference](https://react.dev/reference/react)（hooks/组件）；[dsh-client-ui-primitives README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)（组件清单与用法）；[grpc-go API Reference（ServerStream/ClientStream 形态）](https://pkg.go.dev/google.golang.org/grpc)（bind 扩展与 proxy 转发的生成接口契约）；[grpc-gateway runtime/errors.go @ v2.27.6](https://github.com/grpc-ecosystem/grpc-gateway/blob/v2.27.6/runtime/errors.go)（gRPC code→HTTP 映射，错误语义依据）
- **技术文章/技术参考文档**：`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`（§1 托管拓扑/事件映射 §4/错误映射与两跳语义 §2/§2.1）；`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`；`specs/049-agent-v2-dsh-init/data-model.md`（§2.2 owner 亲和/§2.6–2.9 实体与路由映射）；`specs/049-agent-v2-dsh-init/research.md`（D2/D3/D4/D9/D10/D11/D12）；`specs/047-dsh-chat-demo/research.md`（D2 resolve 时序/D3 idle 终止/D5 get-or-create）；`specs/002-deploy-secret-config/contracts/secret-config.md`（service/deploy secret 契约）；`tools/release/deploy/README.md`（`runtime_protos` 按标准导入路径物化语义，T016/T017）；样板源码：`experimental/dsh/demo/agent/src/{bootstrap,dsh,session,server}.ts`、`experimental/dsh/demo/agent/cordis.yml`、`experimental/dsh/demo/agent/BUILD.bazel`、`experimental/dsh/demo/testplan/deploy.yaml`（env 注入样例）、`projects/game/proxy/cmd/main.go`（路由设施装配样板）、`projects/game/proxy/handler/handler.go`（owner 解析/assignOwner/propagateAgentError 样板）、`projects/game/proxy/domain/interfaces.go`（OwnerStore/OwnerPicker 契约）、`projects/game/proxy/runtime/agentclient/{manager,client}.go`（stateful 实例连接管理）、`projects/game/proxy/runtime/mongo/owner_store.go`（owner 存储实现）、`projects/game/proxy/runtime/picker/hash_picker.go`、`projects/game/pkg/bind/{binder,first_frame}.go`（v1 泵语义，扩展基线）、`projects/game/BUILD.bazel`（根 proto 目标样板）、`projects/game/agent/BUILD.bazel`（ts_proto_library 跨目录引用先例）、`projects/game/gateway/cmd/main.go`、`experimental/js/vite_react_demo/server/main.go` 与 `main_test.go`（Go 静态托管逐行对照，T022）、`projects/game/game.proto`（Session 字段对照，T019）

- [X] T013 [US1] 实现 `projects/game/agent_v2/src/dsh.ts`：`bootDsh()` = boot 前配置注入（`GLM_BASE_URL` 已设直用；否则 `GLM_LLM_TARGET` 经 `createResolver().resolve()` 注入；否则默认 `https://open.bigmodel.cn/api/v1`；读 `$DOMINION_SECRET_DIR/glm-api-token` 文件→`GLM_API_KEY`，缺失/空 fail-loud 且错误不含 token 内容）+ `boot(binName, cordisConfigPath(), undefined, undefined, import.meta.url)`；DI seam（resolver/boot 可注入）+ `dsh.test.ts`（`specs/049-agent-v2-dsh-init/research.md` D9；对照 `experimental/dsh/demo/agent/src/dsh.ts`）
- [X] T014 [US1] 实现 `projects/game/agent_v2/src/session.ts`：`AgentSessions`（get-or-create 注册表 + single-flight 防抖 + 陈旧校验，对照 demo `session.ts`）扩展每会话 FIFO 队列与 `TurnRunner`（忙时入队发 `queued{position}`、回合结束自动取队首、会话间独立 chain）+ `dispose(session)`（幂等：移除条目、`handle.dispose()`、在途流转 `turn_end{ABORTED}`、排队作废、历史清空——`specs/049-agent-v2-dsh-init/data-model.md` §2.2/2.3）+ `session.test.ts`（mock ctx 驱动事件监听器——`style/javascript.md` Mock 约定；排队/续跑/dispose 用例）
- [X] T015 [US1] 实现 `projects/game/agent_v2/src/history.ts`：会话生命周期事件收集器（`assistant/chunk`→ChatEvent 映射转发给当前回合流；`assistant/message`→`HistoryMessage{role:AGENT}` 终局追加；用户消息入队时追加 `role:USER`；`agent/status idle`/`agent/error`/`turn/end` → 回合终止判定——映射表 `specs/049-agent-v2-dsh-init/contracts/conversation-api.md` §4）+ `history.test.ts`（事件序→ChatEvent 序断言，对照 `experimental/dsh/demo/agent/src/session.ts` 的 runRound 模式）
- [X] T039 [P] [US1] **（返工：proto 归位 app 根目录，`specs/049-agent-v2-dsh-init/research.md` D12；T008 交付物位置变更，内容零改动）** `git mv projects/game/agent_v2/agent_v2.proto projects/game/agent_v2.proto`；在 `projects/game/BUILD.bazel` 新增 `agent_v2_proto`（proto_library，srcs `agent_v2.proto`，deps 同既有 agent_v2 声明：empty/timestamp/annotations/field_behavior）+ `agent_v2_go_proto`（go_proto_library，compilers 对照 `game_go_proto`：go_grpc_v2/go_proto/grpc-gateway/go_gen_aip，importpath `dominion/projects/game/v2`，**deps 仅 `@googleapis//google/api:annotations_go_proto`** 并随迁 linker 冲突注释——annotations 与 field_behavior 的 go_proto 共享 importpath，同列触发 "multiple copies of package passed to linker"）+ `agent_v2`（go_library，embed go_proto）；从 `projects/game/agent_v2/BUILD.bazel` 移除上述 proto/go 目标；`projects/game/gateway/cmd/BUILD.bazel` 的 `# gazelle:resolve go dominion/projects/game/v2` 映射与 dep 改指 `//projects/game:agent_v2`；`bazel build //projects/game/...`
- [X] T036 [P] [US1] 实现 bind server-streaming 扩展 `projects/game/pkg/bind/server_stream.go` + `server_stream_test.go`：接口与签名逐字照 `specs/049-agent-v2-dsh-init/research.md` D11（`ServerSendStream[Resp]`/`UpstreamStream[Resp]`（Recv-only 已发送形态）/`ServerStreamBinder[Resp].BindServerStream(downstream, upstream) error`/`NewServerStreamBinder[Resp]()`）；泵语义 = `upstream.Recv()` 循环逐帧 `downstream.Send`，`io.EOF`/`context.Canceled` 归一化为 nil（对齐 v1 report 语义，`projects/game/pkg/bind/binder.go`）；**v1 `binder.go`/`first_frame.go` 零改动**；表驱动单测（干净收尾/上游错误透传/下游断开归一化/下游发送失败透传，fake stream 注入——`style/golang.md` §单元测试）；`bazel test //projects/game/pkg/bind/...`（含既有 v1 用例回归）
- [X] T016 [US1] **（返工：proto 加载路径）** `projects/game/agent_v2/src/server.ts` 的 `PROTO_PATH` 改为标准导入路径物化位置 `path.join(SERVICE_ROOT, "projects/game/agent_v2.proto")`（`runtime_protos` 物化语义见 `tools/release/deploy/README.md`；demo 根 proto 加载先例 `experimental/dsh/demo/agent/src/server.ts:27`；类型 import `../agent_v2_types/...` 不变——types 目标仍在 agent_v2 包内引用根 proto）；`server.test.ts` 同步路径断言；`bazel test //projects/game/agent_v2:lib_test`
- [X] T017 [US1] **（返工：BUILD proto 引用；cordis.yml 不变）** `projects/game/agent_v2/BUILD.bazel`：`ts_proto_library(name = "agent_v2_types", proto = "//projects/game:agent_v2_proto")`（agent v1 跨目录引用先例 `projects/game/agent/BUILD.bazel:20-24`）、`artifact_pkg_js.runtime_protos = ["//projects/game:agent_v2_proto"]`；`bazel build //projects/game/agent_v2:cmd_image`
- [X] T037 [US1] 实现 proxy ConversationService 转发面（`specs/049-agent-v2-dsh-init/research.md` D4）：(a) **（返工：构造 API 语义化——2026-08-29 用户指令，collection 命名归属 store 层；已按参数化实现的工作区代码按下述终态调整）** `projects/game/proxy/runtime/mongo/owner_store.go` 语义化构造函数：`NewAgentOwnerStore(client)`（v1 team owner → `game_proxy.agent_owners`）与 `NewAgentV2OwnerStore(client)`（→ `game_proxy.agent_v2_owners`），db/collection 常量私有化于包内（不导出，命名知识单一事实源在 store 层——`specs/049-agent-v2-dsh-init/research.md` D4）；移除参数化构造 `NewMongoOwnerStore(client, db, coll)` 与导出常量 `DefaultOwnerDatabase`/`DefaultOwnerCollection`；v1 调用点 `projects/game/proxy/cmd/main.go` 改 `NewAgentOwnerStore(mongoClient)`——构造名变化但存储位置/行为零改动（`owner_store_test.go` 经 `newStoreWithFakeCollection` 直构 struct，不受构造 API 影响，零改动）；(b) `projects/game/proxy/runtime/agentclient/manager.go` conn factory 参数化 target（`newAgentConn(ctx, target, instanceIndex)`，manager 传自身 target）与 daemon name 参数化（`NewDaemon(name, m, interval)`，v1 调用点传原名）——v1 行为零改动，`manager_test.go` 同步 mock factory 签名；(c) 新增 `projects/game/proxy/handler/conversation.go`：`ConversationHandler`（实现 `gamev2.ConversationServiceServer`；`Send` = 资源名校验 INVALID_ARGUMENT → assignOwner get-or-create（复用 `handler.go` assignOwner 竞态语义，store/picker/manager 注入 v2 实例）→ `gamev2.NewConversationServiceClient(connRef.Conn).Send(ctx, req)` 建流（建流调用即完成请求发送与半关闭——D11 已发送形态）→ `bind.NewServerStreamBinder[gamev2.ChatEvent]().BindServerStream(stream, upstream)` 转发（生成流 `grpc.ServerStreamingClient[ChatEvent]` 直接满足 `UpstreamStream[ChatEvent]`，无适配层），owner 实例连接缺失/建流失败 → UNAVAILABLE；`ListHistory` = lookupOwner，NotFound ⇒ 200 空响应短路（读路径不分配），存在 ⇒ 转发；`Dispose` = lookupOwner，NotFound ⇒ 幂等 Empty 短路，存在 ⇒ 转发；下游 status 原码透传——`propagateAgentError` 语义）+ `conversation_test.go`（owner 分配/短路/透传/泵转发分支）；(d) `projects/game/proxy/cmd/main.go` 装配 v2 owner store（`proxymongo.NewAgentV2OwnerStore(mongoClient)`——main.go 不出现 collection 字符串）、`agentclient.NewManager(statefulResolver, solver.MustParseTarget(gameconst.AgentV2Target), DefaultRefreshInterval)` + 第二 daemon + `gamev2.RegisterConversationServiceServer(grpcServer, conversationHandler)`；gazelle 同步 BUILD（proxy 依赖 `//projects/game:agent_v2`）；`bazel test //projects/game/proxy/...`
- [X] T018 [US1] **（返工·替换：gateway 经 proxy 注册，不做 agent_v2 直连——2026-08-29 用户架构指令）** `projects/game/gateway/cmd/main.go`：移除 `agentV2Conn` 直连块（dial/bootstrap 注册/注释），`gamev2.RegisterConversationServiceHandler(ctx, gwmux, teamConn)`（挂既有 proxy conn，与 TeamService 同连接；teamConn 注释更新为双服务托管）；保留 `rootMux.HandleFunc("/api/v2/", gwmux.ServeHTTP)` 与既有 `/api/v1/` 分支零改动；`main_test.go` 修订（移除 agentV2Conn 装配断言，ConversationService 注册断言改经 teamConn）；`bazel test //projects/game/gateway/...`
- [X] T038 [P] [US1] agent_v2 有状态声明：`projects/game/agent_v2/service.yaml` `kind: stateless` → `kind: stateful`（对齐 v1 agent `projects/game/agent/service.yaml`；proxy 经 `dominion-stateful` 按实例序号寻址，`specs/006-grpc-js-service-discovery` FR-009/FR-012）；`projects/game/pkg/gameconst/const.go` `AgentV2Target` 注释更新（消费方 = proxy 有状态实例解析，gateway 不直连）
- [X] T019 [P] [US1] 实现 `projects/game/web/frontend/src/api/`：`sessions.ts`（`/api/v1/templates/saolei/sessions` CRUD，相对路径 fetch，protojson 字段对照 `projects/game/game.proto` Session）+ `conversation.ts`（`sendStream` NDJSON 流读取——代码照 `specs/049-agent-v2-dsh-init/contracts/conversation-api.md` §6 样例；`listHistory`/`disposeSession`）+ `ndjson.test.ts`（分片半行/粘包重组）
- [X] T020 [P] [US1] 实现 `projects/game/web/frontend/src/store/chat.ts`：`ChatState` + 事件 reducer（归约不变式照 `specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §4：queued/turn_start/deltas/turn_end 各分支，ERROR/ABORTED 处理）+ `useSyncExternalStore` 绑定 + `chat.test.ts`（事件序归约、COMPLETED/ERROR/ABORTED 三分支、回填重建）
- [X] T021 [US1] 实现 `projects/game/web/frontend/src/`：`components/ChatView.tsx`（消息区 TEXT 块 `MessageText` 渐进渲染 + 发送输入 + 错误呈现 + 底部跟随滚动）+ `components/SessionList.tsx` 最小集（Create/选择，Delete/Refresh 留 US4）+ `App.tsx` 装配（侧栏+主区双栏布局，无路由库）+ `src/theme.css`（定义 11 个 `--dsw-*` token——清单见 `specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §3.3）+ `App.test.tsx`（发送→渐进呈现的组件测试，testing-library）
- [X] T022 [US1] 打通 `projects/game/web/server/` 静态托管：`main.go` 完整实现（`fs.Sub(assets.FrontendDist, "frontend_dist")` + `http.FileServerFS`——逐行对照 `experimental/js/vite_react_demo/server/main.go`）+ `main_test.go`（embed 内容 404/200 断言，对照 `experimental/js/vite_react_demo/server/main_test.go`）；`bazel build //projects/game/web/server:cmd_image`（依赖 T004 的 `:dist` 与 T005 的 assets 声明）
- [X] T023 [US1] 部署增量 `projects/game/deploy.yaml`：services 增加 agent_v2（`//projects/game/agent_v2/service.yaml`——`kind: stateful` 已由 T038 落于 service.yaml；secrets 绑定 `glm-api-token: {secret: llm-secrets, key: glm-codingplan}`——`specs/002-deploy-secret-config/contracts/secret-config.md` §2）与 web（`//projects/game/web/server/service.yaml`，http 块 hostnames `[game.liukexin.com]` + match `PathPrefix /`）；gateway 服务 http 块 matches 增加 `{backend: http, path: PathPrefix /api/v2/}`（既有 `/api/v1/` 原样——同主机名路径分流 `specs/049-agent-v2-dsh-init/research.md` D5）；proxy 已在 services 列表（增量注册无需部署声明改动）

**Checkpoint**: `bazel build //... && bazel test //projects/game/... //common/js/...` 全绿（含 `//projects/game/pkg/bind/...`、`//projects/game/proxy/...`、`//projects/game/gateway/...`）。MVP 可停点：以 `projects/game/testplan/deploy_agent_v2.yaml`（Phase 7 建）或手工部署验证 quickstart §2 用例 2–4、9；正式大型验收在 Phase 7 统一执行。

---

## Phase 4: User Story 2 — 思考过程（think）可见且与正文区分 (Priority: P1)

**Goal**: THINK 块端到端呈现：默认折叠可展开、流式渐进、与正文区分、无思考不出现空区域。

**Independent Test**: think 模板驱动的回合中，页面与接口层可分别获取 text 与 think；`ReasoningRow` 组件测试通过（`specs/049-agent-v2-dsh-init/quickstart.md` §2 用例 2）。

### 文档清单（本 phase 必读）

- **代码规范**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[React API Reference](https://react.dev/reference/react)；[dsh-client-ui-primitives README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)（DisclosureRow/MessageText 用法）
- **技术文章/技术参考文档**：`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（§3.2 ReasoningRow 契约）；[dsh ReasoningRow.tsx 参照源码](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)（折叠摘要 latest/first line、running 跟随、data-state）

- [ ] T024 [P] [US2] 实现 `projects/game/web/frontend/src/components/ReasoningRow.tsx`（自建：`DisclosureRow` 外壳 + `IconThinkOutline14` + 默认折叠 + 折叠摘要行（running=最新行/完成=首行）+ 展开全文 + `data-state="running|ok"`；参照源码改造并去 locale/slot 依赖）+ `ReasoningRow.test.tsx`（折叠默认、展开切换、无思考文本不渲染该组件）
- [ ] T025 [US2] 集成 `projects/game/web/frontend/src/components/ChatView.tsx`：THINK 块渲染分支（think 与 text 分类呈现不混排、流式渐进更新折叠摘要、纯 text 回合零 THINK 区域）+ `ChatView` 组件测试补 THINK 场景（含"无思考不出现空区域"——US2 场景 2）

**Checkpoint**: `bazel test //projects/game/web/frontend:lib_test` think 场景全绿。

---

## Phase 5: User Story 3 — 对话页工具调用（tools）渲染能力 (Priority: P2)

**Goal**: ToolCard 组件与 TOOL_CALL 块渲染能力就绪，以构造数据在页面/接口层验证（端到端验证推迟至后续第一个工具 step，spec FR-005）。

**Independent Test**: 构造 `HistoryMessage`/流事件（名称/参数/状态/结果、多工具混合）驱动组件，断言分类保序、关联展示（`specs/049-agent-v2-dsh-init/quickstart.md` §2 用例集说明）。

### 文档清单（本 phase 必读）

- **代码规范**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[React API Reference](https://react.dev/reference/react)；[dsh-client-ui-primitives README](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)（StateDot/JsonBlock 用法）
- **技术文章/技术参考文档**：`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（§3.2 ToolCard 契约）；[dsh ToolCallTree.tsx 参照源码](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx)（单卡形态，剥离 slot 系统）

- [ ] T026 [P] [US3] 实现 `projects/game/web/frontend/src/components/ToolCard.tsx`（自建：名称/参数（JsonBlock 折叠）/状态（StateDot：RUNNING/SUCCEEDED/FAILED）/result 关联同卡；props 照 `specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §3.2）+ `ToolCard.test.tsx`（US3 三场景：RUNNING 态、结果关联、失败态）
- [ ] T027 [US3] 集成 `projects/game/web/frontend/src/components/ChatView.tsx`：TOOL_CALL 块渲染分支 + 历史回填的 tool_call 块渲染（tool_id 关联展示）+ 混合保序测试（构造"正文+思考+多次工具调用"数据断言三类内容按序可区分呈现——US3 场景 3）

**Checkpoint**: `bazel test //projects/game/web/frontend:lib_test` US3 构造数据用例全绿（US3 端到端验证延后，此为页面/接口层验收）。

---

## Phase 6: User Story 4 — session 管理页面完整能力 (Priority: P2)

**Goal**: 列表（含创建时间）/新建/删除（编排 dispose）/切换完整闭环，多会话互不串扰。

**Independent Test**: 页面（或 web API）执行 新建→列表可见→进入对话→返回列表→删除 闭环；回合中删除触发 ABORTED 与资源释放（`specs/049-agent-v2-dsh-init/quickstart.md` §2 用例 1/7）。

### 文档清单（本 phase 必读）

- **代码规范**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[React API Reference](https://react.dev/reference/react)
- **技术文章/技术参考文档**：`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（§2/§5 删除编排与 API 面）；`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`（§1 Dispose 幂等语义）；行为基线源码：`projects/game/desktop/frontend/src/components/SessionList.svelte`（列表/时间/操作）、`projects/game/desktop/frontend/src/components/ChatView.svelte`（切换/隔离/pending 标记）

- [ ] T028 [US4] 完善 `projects/game/web/frontend/src/components/SessionList.tsx`：列表（名称+创建时间格式化）、Refresh、Delete 编排（`deleteSession`(/api/v1) 成功→`disposeSession`(/api/v2)，dispose 失败仅记录不阻断——`specs/049-agent-v2-dsh-init/research.md` D6）、选中态与切换回调 + 组件测试（含 dispose 失败容错分支）
- [ ] T029 [US4] 实现 `projects/game/web/frontend/src/App.tsx` 多会话隔离：每 session 独立 `ChatState`（切换不串扰、刷新经 `listHistory` 回填——FR-014 前端侧）、回合中切换/返回列表的输入态管理 + 组件测试（双会话切换隔离断言）

**Checkpoint**: `bazel test //projects/game/web/frontend:lib_test` US4 用例全绿；US1–US4 全部就绪。

---

## Phase 7: 大型测试验收与收尾（Cross-Cutting）

**Purpose**: FR-011 大型测试（经 testplan skill 实际执行，全部用例通过）、真实端点冒烟文档、存量零回归、token 零泄漏。

### 文档清单（本 phase 必读）

- **代码规范**：`style/large_test.md`（单测试计划/suite 组织/模块命名/go_largetest/反模式）；`style/golang.md`（§单元测试 规范）
- **官方文档**：无
- **技术文章/技术参考文档**：`.opencode/skills/testplan/SKILL.md`（guitar 操作入口，T033 执行依据）；`specs/049-agent-v2-dsh-init/quickstart.md`（§2 用例集/§3 冒烟/§4 回归）；`specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md`（§4 部署替换）；既有样板：`projects/game/testplan/system_test.yaml`、`projects/game/testplan/deploy_agent.yaml`、`projects/game/testplan/BUILD.bazel`（go_largetest 声明）、`projects/game/testplan/helpers_test.go`、`experimental/dsh/demo/testplan/deploy.yaml`（fake env 注入样例）

- [ ] T030 编写 `projects/game/testplan/deploy_agent_v2.yaml`（type test，services：mongo(dev-single)/session/fake-llm/**proxy**/agent_v2/web/gateway——proxy 为 `/api/v2` 两跳路由（gateway→proxy→agent_v2）的必经组件；agent_v2 env：`GLM_LLM_TARGET=dominion:///game/fake-llm:8080` + `GLM_API_KEY=dummy-key`；gateway http 块 matches `/api/v1/`+`/api/v2/`，web http 块 hostname `game.liukexin.com` match `/`——对照 `projects/game/testplan/deploy_agent.yaml` 与 `experimental/dsh/demo/testplan/deploy.yaml`）
- [ ] T031 编写大型测试用例 `projects/game/testplan/agent_v2_conversation_test.go`（模块=agent_v2 对话面；用例：流式事件序与多帧 delta、多轮连续性、会话隔离、排队 queued→自动续跑（长延迟模板窗口）、history 回填一致性、dispose ABORTED 与全新会话、模型故障 ERROR 恢复、空文本 400；helpers 优先复用/扩展 `helpers_test.go`，trace_id 经 `common/gopkg/otel/tracecontext`——`style/large_test.md` §测试用例）+ `projects/game/testplan/web_test.go`（模块=web 服务：`GET /` HTML、静态资源、页面经 `/api/v1`+`/api/v2` 的管理闭环 smoke）；`projects/game/testplan/BUILD.bazel` 增两个 `go_largetest` target（srcs/deps 对照既有声明，gazelle 同步）
- [ ] T032 在既有 `projects/game/testplan/system_test.yaml` 增加 suite（引用 `//projects/game/testplan/deploy_agent_v2.yaml` + T031 两个 case target；**不得新建独立测试计划 YAML**——`style/large_test.md` §测试计划数量）；suite 描述写明覆盖的 spec 场景锚点
- [ ] T033 经 testplan skill 实际执行验收：`guitar run projects/game/testplan/system_test.yaml`（新 suite；完成部署→测试→清理闭环），**全部用例通过**（failed/flaky 即修复重跑直至全绿——constitution 原则 VI；禁止以 `bazel build` 测试 target 替代执行）
- [ ] T034 真实 GLM 端点冒烟交付文档：`specs/049-agent-v2-dsh-init/quickstart.md` §3 复核（步骤可手工复验）；`projects/game/agent_v2/README.md` 与 `projects/game/web/README.md`（服务说明、`glm-codingplan` k8s secret key 运维预置说明、已知限制——多标签页无实时推送/desktop 删除不联动/历史随进程重启丢失，`specs/049-agent-v2-dsh-init/research.md` 已知限制节）
- [ ] T035 存量零回归与泄漏扫描：`bazel test //projects/game/gateway/... //projects/game/agent/... //projects/game/proxy/... //projects/game/pkg/bind/... //projects/game/fake-llm/... //projects/game/desktop/...` 全绿（SC-005——proxy/bind 为本 feature 增量修改点，其既有 v1 用例必须全绿）；`rg -i "glm-api-token|GLM_API_KEY" --glob '!*.md'` 复核交付物无明文 token 值（SC-004）；既有 game 大型测试抽跑一组（`guitar run projects/game/testplan/system_test.yaml` 既有 suite）确认 `/api/v1` 行为与 proxy v1 TeamService 链路不变

**Checkpoint**: `specs/049-agent-v2-dsh-init/quickstart.md` §5 交付核对清单全部勾选。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖，立即可开始。
- **Phase 2 (Foundational)**: 依赖 Phase 1（T008 需 T003 骨架；T009–T011 需 T002；T012 独立但同期）——**阻塞全部 user story**。
- **Phase 3 (US1, MVP)**: 依赖 Phase 2（T013–T015 依赖 T008/T011；T021 依赖 T019/T020；T022 依赖 T004/T005）。**Phase 3 内返工链（2026-08-29 方案变更）**：T039（proto 归位）先行，阻塞 T016/T017/T018/T037；T036（bind 扩展）阻塞 T037（proxy 泵消费方）；T038 独立。
- **Phase 4 (US2)**: 依赖 Phase 3（ChatView/THINK 流已就绪；插件 reasoning 与 fake think 模板已在 T009/T012 落地）。
- **Phase 5 (US3)**: 依赖 Phase 3（ChatView 集成点）；与 Phase 4 可并行。
- **Phase 6 (US4)**: 依赖 Phase 3（Dispose RPC 与 SessionList 最小集）；与 Phase 4/5 可并行。
- **Phase 7 (验收)**: 依赖 Phase 3–6 全部完成。

### User Story Dependencies

- **US1 (P1)**: Phase 2 后即可开始，无跨 story 依赖。
- **US2 (P2 优先级序中的 P1)**: 依赖 US1 的 ChatView/store 集成点；独立可测（组件级）。
- **US3 (P2)**: 依赖 US1；独立可测（构造数据）。
- **US4 (P2)**: 依赖 US1（Dispose RPC、SessionList 最小集）；独立可测。

### Within Each User Story

- 契约/模型（proto、wire、store）先于服务/端点；服务先于集成；单测内嵌每 task。
- 同文件任务串行（T014→T015→T016 共享注册表语义；T016→T017 共享 agent_v2 BUILD/server.ts；T021→T025→T027→T029 同为 ChatView.tsx 渐进编辑，串行执行）。

### Parallel Opportunities

- Phase 1: T002–T007 全部 [P]（不同包）。
- Phase 2: T008/T009+T010/T012 三线并行；T011 汇合 T009/T010。
- Phase 3: 返工线 T039 →（T016+T017 → T037 → T018）与 T036（bind 扩展，独立包）、T038（service.yaml 声明）、前端线 T019+T020 并行；T021 汇合。
- Phase 4 (T024) 与 Phase 5 (T026) 与 Phase 6 (T028) 三个组件文件互不相同，可并行（各自集成 task 串行于各自组件 task 后）。

---

## Parallel Example: User Story 1

```bash
# agent_v2 宿主线与前端线并行（Phase 3 内）：
Task T013: "实现 projects/game/agent_v2/src/dsh.ts（boot 前 env/secret 注入）"
Task T019: "实现 projects/game/web/frontend/src/api/conversation.ts（NDJSON 流客户端）"
Task T020: "实现 projects/game/web/frontend/src/store/chat.ts（事件 reducer）"

# 汇合后：
Task T021: "ChatView + App 装配（消费 api+store）"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 Setup + Phase 2 Foundational（CRITICAL——阻塞全部）。
2. Phase 3 US1 完成 → **STOP and VALIDATE**：`bazel build //... && bazel test //...` 全绿；可选用 T030 的 deploy 手工验证一次对话闭环。
3. 正式大型验收在 Phase 7 统一执行（FR-011 单测试计划约束，`style/large_test.md`）。

### Incremental Delivery

1. Setup + Foundational → 底座就绪。
2. +US1（text 流式对话）→ MVP。
3. +US2（think 折叠）→ +US3（tools 渲染）→ +US4（管理闭环）——三者除共享 ChatView 集成点串行外可并行推进。
4. Phase 7 大型测试全绿 + 冒烟/回归/泄漏扫描 → 交付。

### Notes

- 每完成一个 task 或逻辑组提交；Checkpoint 处停下独立验证。
- 对 `projects/game/web/frontend/src/components/ChatView.tsx` 的多 phase 渐进编辑（T021→T025→T027→T029）保持串行，避免并发编辑冲突。
- 禁止为验收新建独立测试计划 YAML（`style/large_test.md` 反模式 1/4）。
