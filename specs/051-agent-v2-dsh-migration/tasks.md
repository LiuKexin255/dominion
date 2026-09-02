# Tasks: 051-agent-v2-dsh-migration — Game Agent v2 — dsh 迁移 Step 2

**Input**: Design documents from `/specs/051-agent-v2-dsh-migration/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/ (all present)

**Tests**: 本 feature 明确要求测试（spec FR-020/SC-001..003 与各 contract 的测试义务）。按 constitution 原则 IV：**编译 + 单测/组件测试内嵌于每个实现任务**（`bazel build` + `bazel test` 相关 target，不单列 task）；**大型测试单列验收任务**（principle VI：必须实际 `guitar run` 执行且全部用例通过）。

**Organization**: 按 user story 组织（US1 游戏闭环 P1 MVP / US2 preset 与物化 P1 / US3 desktop 退化 P2 / US4 web 侧栏 P2）；跨故事共享的阻塞前置归 Foundational；v1 处置与全量验收为横切收尾 phase；Phase 3R 为 Directive 2026-09-01（用户修改意见 1/3/4）的 Phase 3 返工 phase（[revisions/directive-2026-09-01.md](revisions/directive-2026-09-01.md)）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属 user story（US1–US4）；Setup/Foundational/横切 phase 无 story 标签
- 所有任务含精确文件路径；bazel/pnpm/go 操作按 `AGENTS.md` 的包装命令执行（`bazel run //:gazelle`、`bazel run @pnpm -- --dir <abs path>`、`bazel run //:go -- fmt`）

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 三个新 dsh 插件 workspace 包骨架与依赖接线。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（style/javascript.md 引用基准）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（style/javascript.md 引用的 ESM 包契约：新包 tsconfig/.swcrc/package.json 契约）、`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §1（workspace 插件包契约先例：package.json/exports/peers 形态）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §1–§5（三包的名称/inject/依赖定义）

- [X] T001 创建 `@dominion/dsh-desktop-bridge` 包骨架：`common/js/dsh-plugins/desktop-bridge/`（package.json `"type": "module"` + exports、tsconfig.json（nodenext + paths 指向 dsh peers 源码）、`.swcrc`、src/index.ts 占位导出 `export const name = "desktop-bridge"`、BUILD.bazel `js_library`）——镜像 `common/js/dsh-plugins/llm-glm/` 形态；执行 `bazel run @pnpm -- --dir /mnt/code/dominion`（install）+ 目标目录下 `bazel run //:gazelle common/js/dsh-plugins/desktop-bridge`，`bazel build //common/js/dsh-plugins/desktop-bridge` 通过
- [X] T002 [P] 创建 `@dominion/dsh-saolei-loop` 包骨架：`common/js/dsh-plugins/saolei-loop/`（同 T001 形态；package.json 依赖声明 `@dominion/game-saolei-board`（workspace）+ dsh 0.1.1-rc.2 peers（dsh-agent/dsh-session/dsh-llm/dsh-system-prompt/dsh-tools/dsh-scope/cordis）），构建门禁同 T001
- [X] T003 [P] 创建 `@dominion/dsh-saolei` 包骨架：`common/js/dsh-plugins/saolei/`（同 T001 形态；依赖 `@deepseek-ai/dsh-tools`、`@deepseek-ai/dsh-system-prompt`、`@dominion/dsh-saolei-loop`（workspace，消费 saoleiGame 服务类型）），构建门禁同 T001

**Checkpoint**: 三个空插件包可编译、可被组合清单按名引用。

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 全部 P1 故事共同依赖的契约面与共享模块——proto 重塑、codegen、GLM 适配器扩展、preset 存储。**US1/US2/US3 的任何任务不得在本 phase 完成前开始**。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/api.md`（Service/Method 注释含 Prefix Path；REST/gRPC 注解）及其引用的 [AIP-122 Resource names](https://google.aip.dev/122)、[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-131 Get](https://google.aip.dev/131)、[AIP-132 List](https://google.aip.dev/132)、[AIP-133 Create](https://google.aip.dev/133)、[AIP-134 Update](https://google.aip.dev/134)、[AIP-135 Delete](https://google.aip.dev/135)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-156 Singleton resources](https://google.aip.dev/156)、[AIP-158 Pagination](https://google.aip.dev/158)；`style/javascript.md`（glm TS 改动）+ [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；`style/mongo.md`（_id 自动生成、库表隔离）
- **官方文档**：`node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-llm/README.md`（LlmAdapter.listModels 契约）；[OpenAI Responses API input items（openai-openapi）](https://github.com/openai/openai-openapi)（function_call/function_call_output item 形状）；[dsh cookbook: adding-an-llm-adapter](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md)（049 glm 合同引用的适配器约定）
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/contracts/agent-api.md`（§1 proto 全量 + §2 方法语义）、`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §1（DesktopBridgeService 面）、`specs/051-agent-v2-dsh-migration/research.md` D1/D2/D4/D11、`specs/051-agent-v2-dsh-migration/data-model.md` §2.1/§2.7、`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`（§3 协议义务/§4 序列化表/§7 未做扩展点——本 phase 解除其中两处 UNSUPPORTED_CONTENT）、`projects/game/game.proto`（UserFrame/TeamFrame/FlowPart 帧类型——T004 import 源）

- [X] T004 重塑 `projects/game/agent_v2.proto`：按 `specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §1——`ConversationService` 更名 `AgentService`（UpdateAgent/GetAgent/ListAgentMessages/Send + preset CRUD + ListModels，含 google.api.http 注解与 Prefix Path 注释）；新增 `DesktopBridgeService`（`Connect(stream projects.game.UserFrame) returns (stream projects.game.TeamFrame)`，import `projects/game/game.proto`）；新消息 `Agent`/`Preset`/`Model`/各 Request/Response；`ChatEvent` 新增 `ToolResultEvent tool_result = 16`；移除 `Dispose`/`ListHistory` 及其请求响应消息
- [X] T005 更新 codegen 与构建面：`projects/game/BUILD.bazel`（agent_v2_proto 增加 game.proto 依赖；agent_v2_go_proto/grpc-gateway/ts_proto_library 重生成）、`projects/game/agent_v2/src/server.ts` 与 `projects/game/proxy/`、`projects/game/gateway/` 中引用旧服务/消息名的编译修复（最小占位实现允许，后续 phase 替换为真实逻辑）；`bazel build //projects/game/...` 通过、`bazel run //:gazelle` 更新受影响 BUILD
- [X] T006 [P] 实现 GLM 模型目录：`common/js/dsh-plugins/llm-glm/src/adapter.ts` 增加 `listModels()` override 返回 `config.models`（静态、无端点调用，research D4），`src/adapter.test.ts` 补断言（config.models → LlmModelInfo 映射）；`bazel test //common/js/dsh-plugins/llm-glm` 通过
- [X] T007 [P] 实现 GLM 工具序列化：`common/js/dsh-plugins/llm-glm/src/serialize.ts` 增加 assistant `tool-call` 块 → `{type:"function_call", call_id, name, arguments}` 与 `tool-result` 消息 → `{type:"function_call_output", call_id, output}` 映射（解除 049 合同 §4 两处 UNSUPPORTED_CONTENT，research D11）；`src/serialize.test.ts` 与 `src/wire.test.ts` 补往返/交错用例；`bazel test //common/js/dsh-plugins/llm-glm` 通过
- [X] T008 实现 preset 存储模块：`projects/game/agent_v2/src/presets.ts`——`PresetStore` 接口（create/get/list/update/delete，AIP 语义错误：ALREADY_EXISTS/NOT_FOUND）+ Mongo 实现（`mongodb` catalog 依赖加入 `projects/game/agent_v2/package.json`；db `game_agent_v2`、collection `presets`、`_id` 自动生成、`name` 唯一索引、keyset 分页按 name 排序——`style/mongo.md` 与 v1 先例 `projects/game/prompt/runtime/mongo/repository.go`）；连接解析（`MONGO_URI` 直连 > `@dominion/common-js-resolver` 解析 `dominion:///game/mongo:27017`）；`presets.test.ts` 以注入 collection double（DI seam，`style/javascript.md` Mock 约定）断言写入目标与错误映射；`bazel test //projects/game/agent_v2` 通过

**Checkpoint**: 契约面（proto/codegen）与共享模块（glm 目录/序列化、preset 存储）就绪；US1/US2/US3 可开始。

---

## Phase 3: User Story 1 - 在网页上驱动一局完整的扫雷游戏 (Priority: P1) 🎯 MVP

**Goal**: dsh 组合替换为 saolei-loop（直组核心件）+ 桥接/工具插件，agent 面具备物化与工具事件流，desktop flow 流经 gateway `/api/v2` WS → proxy → 桥接插件，fake LLM + fake desktop 驱动端到端游戏闭环（含异常分支与两流独立性）。

**Independent Test**: `guitar run projects/game/testplan/system_test.yaml --suite agent-v2-game`（及 `desktop-flow`）全绿——fake 拓扑下经 `/api/v2` 完成 preset 创建（CRUD）→ UpdateAgent 物化 → Send 游戏指令 → 工具块可见 → fake desktop 收到操作 → 棋盘文本回传 → 终局与终局后拒绝 → 异常分支 → 多 session 隔离 → 两流独立。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（插件/宿主/web TS）；`style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/)（proxy/gateway/fake-desktop/fake-llm Go，含其引用的 Style Decisions）；`style/large_test.md`（测试用例按模块拆分/表驱动/反模式清单）；`style/api.md` 及其引用的 [AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-193 Errors](https://google.aip.dev/193)（proxy/gateway 错误映射与 Send 自定义方法）
- **官方文档**（0.1.1-rc.2 线物化源码，本仓库 node_modules）：`node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（ReactLoopAgent/AgentLoop 全量——"抄设计"基准）；`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（Agent/AgentHandle/AgentRegistry/AgentOptions）；`node_modules/.pnpm/@deepseek-ai+dsh-tools@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-tools/README.md`（register/defineTool/executionMode/ToolExecution.agent）；`node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-system-prompt/README.md`（section/order 频带/agent-scoped shadow）；[dsh service.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/service.md) 与 [dsh events.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/events.md)（插件服务/事件四模式）
- **技术文章/技术参考文档**：`survey/deepseek-harness-agent-loop-prereq.md`（§2 替换机制/§3 依赖面/§4 异常处理继承清单/§5 能力提供与状态形态）；`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`（三插件全契约 + §5 组合清单 + §6 宿主演进 + §7 测试义务）、`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md`（桥接面/gateway WS/proxy 转发/验收锚点）、`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2（UpdateAgent/Send 语义）、`specs/051-agent-v2-dsh-migration/data-model.md` §2.2/§2.4/§2.5/§2.6/§2.8/§2.9；v1 游戏语义源：`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（工具契约/拒绝码/文本构造器）、`projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`（用例基线）、`projects/game/agent/src/mcp/saolei/geometry.ts`（坐标常量）、`projects/game/agent/src/skill/saolei/SKILL.md`（提示词内容源）、`projects/game/agent/src/team/team-sink.ts`（游戏历史语义）、`projects/game/agent/src/team/player.ts`（DEFAULT_PLAYER_BASE，:79-85）、`projects/game/agent/src/operation-bridge.ts`（桥接语义）、`projects/game/agent/src/handler.ts:101-280`（UpdateAgent/GetAgent 错误映射先例）；v1 路由/桥接移植源：`projects/game/gateway/cmd/main.go:178-350`（路径匹配/wsStream/handleWebSocketConnect/关闭分类）、`projects/game/proxy/handler/handler.go:179-226`、`projects/game/proxy/pkg/bind/`（WithFirstFrame/Binder）、`projects/game/game.proto`（UserFrame/TeamFrame/FlowPart/FlowResultPart/StatusSignal 帧类型）；`projects/game/pkg/saolei-board/`（SaoleiBoard API 与坐标几何）；`specs/049-agent-v2-dsh-init/contracts/conversation-api.md`（§3 事件序/§4 映射——零回归基线）

### Implementation for User Story 1

- [X] T009 [P] [US1] 实现 desktop-bridge 插件：`common/js/dsh-plugins/desktop-bridge/src/index.ts`（cordis Service，`ctx.desktopBridge`：`attach(sessionName, stream)` 首帧绑定/新连接接管/断连清理、`dispatch(sessionName, part, signal)`（UUID tool_id 盖入、20min 超时 backstop、无连接 FAILED "desktop disconnected"、abort FAILED "aborted"、stale 回执忽略）、`handlers()` 返回 DesktopBridgeService gRPC handler 面；OperationResult/截图类型随迁 v1 语义）——契约 `specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §2；`src/bridge.test.ts` 以注入 stream double 断言接管/超时/abort/uuid/stale（v1 `projects/game/agent/src/operation-bridge.test.ts` 基线迁移）；`bazel test //common/js/dsh-plugins/desktop-bridge` 通过
- [X] T010 [P] [US1] 实现 saolei-loop 驱动器与工厂：`common/js/dsh-plugins/saolei-loop/src/index.ts` + `src/driver.ts`——`SaoleiLoopAgent implements Agent`（turn/step 状态机、abort 检查点、interrupted 落日志、wakingAfterAbort/wake latch、四决策点 waterfall/serial、driver containment、FactoryOwnership——调研 §4.7 八条逐条对照官方源码实现）；`ctx.agents.setFactory(this)`；`AgentOptions` 声明合并扩展 `persona?: string` + agent-scoped `deployment:persona` section（空回退 `DEFAULT_PLAYER_BASE`，源 `projects/game/agent/src/team/player.ts:79-85`）；`resume()` fail-loud；注册 `model`/`cwd` 模板变量；`src/driver.test.ts`（fake llm/tools 注入，覆盖 abort/排队/事件面）；`bazel test //common/js/dsh-plugins/saolei-loop` 通过
- [X] T011 [US1] 实现 GameRuntime：`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts`（+ `board.ts`/`geometry.ts`/`text.ts`）——GameRuntime 以 **Service class 形态注册为 `agent.ctx` 的 `saoleiGame` 服务**（agent-scoped，工厂 prepare() 内注册、随 scope 卸载自动注销；无 host 级 Map/注册表；生产 builder 经 `SaoleiLoopPluginOptions.createRuntime` 注入）；init/operate/remain API 返回 v1 契约文本（outcome 行 + `game status:` 行 + 标尺棋盘 + `valid range:`；拒绝码三元组与 SKIP/STOP triage；counter-informed win；识别失败置 null；游戏历史 gameLog/gameEvent/统计）；操作经 `ctx.desktopBridge.dispatch` 下发（client 空间坐标 center 公式与 WINDOW_MESSAGE 随迁 `geometry.ts`）；识别经 `@dominion/game-saolei-board`；`src/game/runtime.test.ts` 迁移 v1 `saolei-mcp.test.ts` 用例基线（fake bridge + fake boardApi，DI）+ scope 生命周期断言（agent dispose 后服务不可达、root ctx 恒不可达）；`bazel test //common/js/dsh-plugins/saolei-loop` 通过
- [X] T012 [US1] 实现 saolei 插件：`common/js/dsh-plugins/saolei/src/index.ts`——`inject = ["tools", "systemPrompt"]`（`saoleiGame` 为 agent-scoped 服务，不能静态 inject）；全局注册 `saolei_init`/`saolei_operate`（双形式参数与互斥校验文本 = v1 MISSING/AMBIGUOUS/INCOMPLETE 字面量）/`saolei_remain`（`defineTool`，output `{result: string}`，exec 经 **`exec.agent.ctx`** 解析该 agent scope 的 `saoleiGame` 服务转发（缺失时 fail-loud），`ToolOutcome.isError` → 抛错）；注册 prompt section `saolei:guidance`（order 100，内容迁移 `projects/game/agent/src/skill/saolei/SKILL.md` 并适配插件语境）；`src/index.test.ts`（fake saoleiGame 注册于 agent scope 断言转发/参数校验/section 注册/exec.agent 缺失 fail-loud）；`bazel test //common/js/dsh-plugins/saolei` 通过
- [X] T013 [US1] 重写组合清单：`projects/game/agent_v2/cordis.yml` 按 `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §5 直组 15 行（timer/llm/session/system-prompt/tools/agents/invariants + 三伴生/llm-retry/llm-glm/desktop-bridge/saolei-loop/saolei；移除 spine 行与全局 persona；不挂 persistence/settings）；invariant 伴生行先试 subpath 行名（`@deepseek-ai/dsh-session/invariant` 等），Loader 拒绝则建本地 wrapper 包 `@dominion/dsh-core-invariants`（apply 内 `ctx.plugin()` 三伴生，research D5 双案）；更新 `projects/game/agent_v2/src/dsh.test.ts` 断言新清单；`bazel test //projects/game/agent_v2` 通过
- [X] T014 [US1] 宿主服务面接线：`projects/game/agent_v2/src/server.ts`——注册 `AgentService`（handlers：UpdateAgent/GetAgent/ListAgentMessages/Send + preset CRUD + ListModels，按 `contracts/agent-api.md` §2 语义：校验先行 fail-fast、`ctx.llm.listModels("glm-responses")` 同源校验）与 `DesktopBridgeService`（`ctx.desktopBridge.handlers()`）；`src/bootstrap.ts` 增加 mongo 客户端生命周期（graceful 顺序 server → agents → fiber → mongo → OTel）；`src/server.test.ts` 更新（gRPC 状态映射：FAILED_PRECONDITION/NOT_FOUND 等）；`bazel test //projects/game/agent_v2` 通过
- [X] T015 [US1] 物化管理：`projects/game/agent_v2/src/session.ts`——`AgentSessions` 演进：`materialize(session, {preset, model, persona})`（终止在途回合 turn_end{ABORTED}、dispose 旧 agent、`ctx.agents.create({agentOptions: {provider: "glm-responses", model, persona}})` 重建）；`send` 前置物化校验（未物化 FAILED_PRECONDITION，懒物化废止）；`dispose` 公共方法仅保留给进程 shutdown；`src/session.test.ts` 更新（049 用例零回归 + 物化/拒绝/幂等新用例）；`bazel test //projects/game/agent_v2` 通过
- [X] T016 [P] [US1] 工具事件映射：`projects/game/agent_v2/src/history.ts`——TurnCollector 处理 `tool/call`/`tool/result` session 事件（`tool_result` ChatEvent 帧 + 历史 ToolCallBlock 按 tool_id 终态回填）；**回合全局 block index 重映射**（step 边界重置 step-local 表，data-model §2.4）；`src/history.test.ts` 补多 step 工具回合用例（index 单调、tool_id 关联、找不到忽略）；`bazel test //projects/game/agent_v2` 通过
- [X] T017 [P] [US1] web 适配与工具结果呈现：`projects/game/web/frontend/src/api/conversation.ts`（sendStream 不变；`listHistory` → `GET /api/v2/{parent=templates/*/sessions/*/agent}/messages`；移除 `disposeSession` 导出）、`src/store/chat.ts`（reducer 新增 `tool_result` 分支按 tool_id 终态化）、`src/components/ToolCard.tsx`（result 文本与终态呈现）；更新 `src/store/chat.test.ts`/`ToolCard.test.tsx`/`App.test.tsx`（049 用例路径适配零回归 + 新分支用例）；`bazel test //projects/game/web/frontend` 通过
- [X] T018 [US1] proxy 转发面：`projects/game/proxy/handler/conversation.go` → `agent.go` 演进 + 新增 `bridge.go`——`AgentHandler implements gamev2.AgentServiceServer`（UpdateAgent get-or-create owner 分配、GetAgent/ListAgentMessages/Send 只查不分配（无 owner → NOT_FOUND）、preset CRUD/ListModels 无亲和稳定哈希选实例、错误映射沿用 propagateAgentError）；`DesktopBridgeHandler implements gamev2.DesktopBridgeServiceServer`（首帧身份 → get-or-create owner → `bind.WithFirstFrame` bidi pump，v1 `projects/game/proxy/handler/handler.go:179-226` 模式）；`projects/game/proxy/cmd/main.go` 注册两服务；更新 `handler` 与 `cmd` 的 Go 测试；`bazel test //projects/game/proxy/...` 通过
- [X] T019 [US1] gateway WS 入口：`projects/game/gateway/cmd/main.go`——`/api/v2/templates/{template}/sessions/{session}/connect` 路径分支（v1 `isWebSocketConnectPath`/`wsStream`/`handleWebSocketConnect` 移植：URL→template/session 注入、binary proto 帧、关闭分类）+ `gamev2.NewDesktopBridgeServiceClient(teamConn).Connect` bidi；`cmd/main_test.go` 增加 `/api/v2` WS 路由用例；`bazel test //projects/game/gateway` 通过
- [X] T020 [P] [US1] fake-desktop 测试设施：`projects/game/fake-desktop/`（Go 服务：`cmd/main.go` + `service/`，WS 客户端连接 gateway `/api/v2/.../connect`（testtool 端点注入）；确定性棋盘模型——init(F2) → 初始棋盘、click/flag/chord 更新模型；按模型返回预生成可识别 PNG 截图（图集映射，复用 `projects/game/testplan/saolei_fixtures_test.go` 嵌入图思路）+ SUCCEEDED 回执；故障注入（配置化：指定操作后断连/不回截图/FAILED）；`service.yaml`（stateless，仅 testplan 引用）+ BUILD.bazel）；Go 单测（棋盘模型确定性/回执形状）；`bazel build //projects/game/fake-desktop/...` + `bazel test //projects/game/fake-desktop/...` 通过
- [X] T021 [P] [US1] fake-llm 游戏模板：`projects/game/fake-llm/service/testdata/agent_v2_saolei.yaml`（responses_only 模板：user 关键词"开始一局扫雷"→ `tool_call: saolei_init`；`tools:` 规则按 `saolei_init` 结果含 "new game started" → 回文本；`saolei_operate` 批量调用链 → 匹配棋盘结果继续；终局模板命中 `game status: won` → 总结文本——多 step 链按 `ToolConfig.match_result_contains` 机制，参照既有 `agent_v2.yaml`/`saolei_tools.yaml`）；补模板匹配单测；`bazel test //projects/game/fake-llm/...` 通过
- [X] T022 [US1] 大型测试套件：`projects/game/testplan/agent_v2_game_test.go`（按模块聚焦：工具链路/棋盘文本契约/终局与终局后拒绝/desktop 缺席与断连分支/多 session 隔离/两流独立性——表驱动，`go_largetest`，helper 复用/扩展 `agent_v2_helpers_test.go`）+ `desktop_flow_test.go`（fake-desktop 视角：连接/探测/接管/操作回执）；`deploy_agent_v2.yaml` 扩展（+ mongo + memory + fake-desktop）；`system_test.yaml` 新增 suites `agent-v2-game`/`desktop-flow`；`bazel build --config=largetest` 相关 target 通过
- [X] T023 [US1] US1 验收执行（Phase 3R 返工后重跑）：`guitar validate projects/game/testplan/system_test.yaml` + `guitar run projects/game/testplan/system_test.yaml --suite agent-v2-game`、`--suite agent-v2-game-disconnect` 与 `--suite desktop-flow`——部署→测试→清理闭环、全部用例通过（失败用 signoz skill 查 tracing/log 排查修复后重跑至 green；constitution 原则 VI；前次验收因 Directive 2026-09-01 返工失效，覆盖范围增 disconnect 套件）

**Checkpoint**: fake 拓扑下"web 驱动 + dsh agent + saolei 工具 + desktop（fake）执行"端到端闭环可演示；049 对话用例经更名 API 零回归。

---

## Phase 3R: Directive 2026-09-01 返工（用户修改意见 1/2/3/4）

**Purpose**: 落地用户 2026-09-01 修改意见 1/3/4（设计权威：[revisions/directive-2026-09-01.md](revisions/directive-2026-09-01.md)）——意见 1：fake-desktop 单服务化 + suite/deploy 重组；意见 3：agent_v2 proto 补 `google.api.resource` 注解、resource name 解析改 codegen（仓库已接入 `//:go_gen_aip`，缺注解是唯一缺口）；意见 4：`PresetService` 拆分（preset CRUD + ListModels）、gateway 直连 agent-v2、移除 proxy 无状态转发面。意见 2（部署窗口 503）已由用户上游修复处置（052 deploy health 探针 + 053 JS bootstrap 组件 + guitar postDeploySettle；directive §8），其 agent_v2 侧前置迁移为 T014b——不迁移则 agent-v2 pod 无法通过无条件附加的 startupProbe，T023 重跑部署失败（`specs/052-deploy-health-probe/contracts/deploy-probe.md` 未适配服务条目）。T004a 先行（proto + 契约面 + codegen），T014a/T018a/T019a 消费其产物（T014a → T014b 同文件 serial）；T022a/T022b 线与之无文件交集可并行；T023 重跑验收收口（前置 T014b）。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/api.md` 及其引用的 [AIP-122 Resource names](https://google.aip.dev/122)、[AIP-123 Resource types](https://google.aip.dev/123)（resource 注解与 `{service_name}/{Type}` type 命名）；`style/large_test.md`（suite/deploy 组织、反模式清单——平行测试计划/按交付物维度）；`style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/)（proxy/gateway/testplan Go 改动，含其引用的 Style Decisions）；`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（agent_v2 server.ts/bootstrap.ts 改动）
- **官方文档**：[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)（`google.api.resource`/`resource_reference` → Go 解析器生成的插件契约，仓库 pin v0.1.3）
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md`（本 phase 设计权威：§1 suite/deploy 重组与用例映射、§2 注解方案/工具链查证/手写解析处置/**§2.6 裁定 D 同包化**、§3 PresetService 拆分/ListModels 归属/gateway 直连拓扑/049 D4 修订、§4 冲突检查与实施顺序、§8 意见 2 处置终态与 T014b 迁移要点）；`experimental/golang/aip_codegen/FINDINGS.md`（`//:go_gen_aip` 接线实证：gazelle 指令防剥离、生成 API 形状、AIP-123 type 命名约束、跨文件 parent 行为）；T004a 交付后的修订版契约：`specs/051-agent-v2-dsh-migration/contracts/agent-api.md`（§1 双服务 proto + 注解、§3 错误表、§4 路由拓扑）、`specs/051-agent-v2-dsh-migration/data-model.md` §2.9、`specs/051-agent-v2-dsh-migration/research.md` D9；v1 注解先例：`projects/game/game.proto`（Template/Session/Team/TeamProfile 的 `google.api.resource` 与请求字段 `resource_reference` 用法）；**T014b 必读**：`specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md`（§3 Bootstrap 编排序列、§6 grpc-server 适配器、§8 两段式接入形态）、`specs/052-deploy-health-probe/contracts/deploy-probe.md`（探针无条件附加/未适配服务 rollout 不 READY/38080 不进 containerPorts）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §6（graceful 顺序不变量 server → agents → fiber → mongo）、迁移样板 `experimental/js/grpc_hello_world/src/bootstrap.ts` 与其 `BUILD.bazel`（053 T016 接入先例）、迁移对象现状 `projects/game/agent_v2/src/bootstrap.ts`；返工对象现状：`projects/game/proxy/handler/agent.go`、`projects/game/proxy/handler/agent_test.go`、`projects/game/proxy/cmd/main.go` 与 `cmd/main_test.go`、`projects/game/gateway/cmd/main.go`、`projects/game/agent_v2/src/server.ts` 与 `src/server.test.ts`、`projects/game/fake-desktop/`（service.yaml/BUILD.bazel/cmd/main.go）、`projects/game/testplan/deploy_agent_v2.yaml`、`projects/game/testplan/system_test.yaml`、`projects/game/testplan/agent_v2_game_test.go`

### Implementation for Directive 2026-09-01

- [X] T004a proto 增量修订与契约面同步（意见 3+4 同一次 proto 变更 + 裁定 D 同包化，directive §2.2/§2.6/§3.2/§4.1）：`projects/game/agent_v2.proto` —— (a) 意见 3：`Agent`/`Preset` 增 `option (google.api.resource)`（`game.liukexin.com/Agent`、pattern `templates/{template}/sessions/{session}/agent`、singular/plural；`game.liukexin.com/Preset`、pattern `templates/{template}/presets/{preset}`），name 字段加 `IDENTIFIER`，按 directive §2.2 表增补 `resource_reference`（`Agent.preset`、`SendRequest.session`→v1 `game.liukexin.com/Session`、GetAgent/ListAgentMessages/Get/Delete/parent 等请求字段），新增 `import "google/api/resource.proto"`；(b) 意见 4：preset CRUD 五方法与 `ListModels` 自 `AgentService` 移入新 `service PresetService`（REST 注解逐字保留、路径不变），`AgentService` 终态 = UpdateAgent/GetAgent/ListAgentMessages/Send，两服务的 Service/Method 注释按 `style/api.md` 重写（含 Prefix Path 与双面路由说明）；(c) 裁定 D（directive §2.6，`protoc-gen-go-aip` v0.1.3 跨 Go 包父构造器缺陷——`emitParentConstructor` 裸名 receiver 不可编译，上游无修复）：`option go_package`（`:13`）`dominion/projects/game/v2` → `dominion/projects/game`；`projects/game/BUILD.bazel` 生成单元合并——`agent_v2.proto` 并入 `game_proto` srcs 与 `game_go_proto`（grpc-gateway 双服务 handler 与 AIP 解析器随同 importpath 产出），**撤销 `agent_v2_go_proto` target 与 `agent_v2` go_library**（同 importpath 双 target 触发链接器 "multiple copies of package" 拒绝），`agent_v2_proto`（proto_library）保留供 TS `ts_proto_library`/`runtime_protos` 消费（与 `game_proto` 共享 .proto 文件合法，protoc 按 target 独立执行）且 deps += `@googleapis//google/api:resource_proto`；12 处 Go 导入面机械改名（`gamev2 "dominion/projects/game/v2"` → 并入 `dominion/projects/game` 导入、限定符 `gamev2.` → `game.`）：proxy `cmd/main.go`、`handler/{agent,bridge}.go` + `handler/{agent,bridge}_test.go`、gateway `cmd/main.go` + `cmd/main_test.go`、testplan `agent_v2_{conversation,game,helpers}_test.go`/`desktop_flow_test.go`/`web_test.go`；TS（ts_proto）消费保持不变；同步修订设计文档（directive §5 清单，含 `contracts/agent-api.md` §1 的 go_package 表述）：`contracts/agent-api.md` §1/§3/§4、`research.md` D9、`data-model.md` §1/§2.9、`plan.md` Constraints、`contracts/desktop-bridge.md` §3、`contracts/web-frontend.md` §2/§3、`quickstart.md` §2 套件表；codegen 重生成（`agent_v2_aip.pb.resource.go` 由 stub 变解析器且生成于 `dominion/projects/game` 包、TS 类型、grpc-gateway 双服务 handler 面）；`bazel build //projects/game/...` + 受影响单测（`//projects/game:game_test`、proxy/gateway/agent_v2 编译修复）通过——中间态 preset 路径暂无 gateway handler，由 T019a 闭环（directive §4.3）
- [X] T014a [P] agent_v2 宿主 PresetService 注册拆分（directive §3.7）：`projects/game/agent_v2/src/server.ts` —— preset CRUD 与 ListModels handlers 自 `buildAgentHandlers` 移出新 `buildPresetHandlers(deps: {presets, listModels})`（TS 版 `parsePresetResource`/`parseTemplateParent` 随迁），`startServer` 三服务注册（AgentService/PresetService/DesktopBridgeService，50051 单 server）；`AgentServiceDeps` 保留 UpdateAgent 校验所需 `presets`/`listModels` 注入；`src/server.test.ts` preset CRUD/模型目录/同源校验用例迁 `buildPresetHandlers` 断言面；`bazel test //projects/game/agent_v2` 通过
- [X] T014b agent_v2 接入 053 JS bootstrap 组件（意见 2 处置的前置迁移，directive §8；**与 T014a 同文件 serial：T014a 先**）：`projects/game/agent_v2/src/bootstrap.ts` 按 `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md` §8 两段式重写（样板 `experimental/js/grpc_hello_world/src/bootstrap.ts`）——静态导入仅 `@dominion/common-js-{otel,grpc-otel,logs,bootstrap}` → `await init({ instrumentations: [createGrpcInstrumentation()] })` → `installReporter(createOTelReporter("game/agent-v2"))`（`unhandledRejection` 安全网保留）→ 动态 import 构建组件 → `new Bootstrap()` 注册三组件：preset Mongo 存储（start = `connect` + `ensureIndexes` fail-loud、stop = `close`）、dsh composition（start = `bootDsh()` fail-loud、stop = `sessions.shutdown()`——dispose 全部 session（在途回合 abort）→ root fiber）、gRPC server（`createGrpcServerComponent`，`0.0.0.0:50051`）→ `await run()` → `uninstallReporter()` + `await shutdown()` → `process.exit`；**生命周期不变量**（`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §6）：启动 mongo → composition → server、停止严格逆序（server → agents/fiber → mongo），Stage/name 按 053 API 选择以满足该不变量；health（38080/healthz，全组件启动后才 200）由 Bootstrap 内置——agent_v2 满足 052 startupProbe 门控（当前无 /healthz，探针无条件附加会导致 rollout 不 READY、T023 部署失败）；`projects/game/agent_v2/src/server.ts` 的 `startServer` 拆出 `buildServer`（构造 sessions/deps/服务注册，返回未 bind 的 `grpc.Server` + sessions，bind 交组件）；`projects/game/agent_v2/package.json` + `"@dominion/common-js-bootstrap": "workspace:*"`；`projects/game/agent_v2/BUILD.bazel` `ts_project` deps += `:node_modules/@dominion/common-js-bootstrap`、`artifact_pkg_js.runtime_deps` += `//common/js/bootstrap:runtime_pkg`（workspace 包仅经 runtime_deps，不进 npm_deps）；`src/server.test.ts` 适配 `buildServer` 形状（handlers 断言面不变）；T014 原文的 bootstrap.ts mongo 生命周期部分由本任务以组件形态承载；`service.yaml` 零端口变更（052：38080 不进 containerPorts）；`bazel test //projects/game/agent_v2` 通过
- [X] T018a [P] proxy 移除 preset/models 转发面（directive §3.6/§2.4；生成 API 已随 T004a (c) 并入 `dominion/projects/game` 包，限定符 `game.`）：`projects/game/proxy/handler/agent.go` —— 删除 CreatePreset/ListPresets/GetPreset/UpdatePreset/DeletePreset/ListModels 六转发方法、`affinityFreeConn`、`listModelsPickKey`、`parsePresetName`、`parseTemplateParent`；`parseAgentResourceName` 改为 `game.ParseAgentName` + `gameconst.IsKnownTemplateID(parsed.TemplateID)` 薄 wrapper（返回 `game.AgentName`，调用点字段兼容）；`AgentHandler` struct 与文件头注释改纯 owner 亲和自述；`agent_test.go` 删除 4 个 `TestAgentHandler_AffinityFreeRPCs_*` 用例、INVALID_ARGUMENT 用例核对生成解析错误码不变（仅断 status code）；`projects/game/proxy/cmd/main.go` 注释同步；`bazel test //projects/game/proxy/...` 通过
- [X] T019a [P] gateway PresetService 直连注册（directive §3.4/§3.5；生成 API 已随 T004a (c) 并入 `dominion/projects/game` 包）：`projects/game/gateway/cmd/main.go` —— 新 `presetConn = grpc.NewClient(solver.URI(gameconst.AgentV2Target), clientOpts)`（unary 默认 keepalive），`game.RegisterPresetServiceHandler(ctx, gwmux, presetConn)`，bootstrap 注册 `GRPCConn("agent-v2", presetConn)`，包注释路由描述更新（会话面两跳 / 配置面直连）；`cmd/main_test.go` 增 preset 路径（`/api/v2/models`、`/api/v2/templates/saolei/presets` 等）经直连 handler 的路由用例；`bazel test //projects/game/gateway` 通过
- [X] T022a [P] fake-desktop 单服务化（directive §1.2）：`projects/game/fake-desktop/service.yaml` —— `name: fake-desktop`、单一 artifact `{name: fake-desktop, target: :cmd_image}`、desc 与注释终态化（删除双 manifest 形态描述）；删除 `service-drop.yaml`；`BUILD.bazel` 收敛单 `service_pkg`（`service = "fake-desktop"`）+ `cmd_image`，删除 `service_pkg_drop`/`cmd_image_drop`；`bazel build //projects/game/fake-desktop/...` + `bazel test //projects/game/fake-desktop/...` 通过
- [X] T022b suite/deploy 重组（directive §1.3–1.6）：`TestAgentV2GameDisconnectMidGameThenRecover`（含 `gameFlowReconnectWait` 常量与注释）自 `projects/game/testplan/agent_v2_game_test.go` 移入新 `agent_v2_game_disconnect_test.go`；`projects/game/testplan/BUILD.bazel` 新 `go_largetest` target `agent_v2_game_disconnect_test`（srcs = 新文件 + `agent_v2_helpers_test.go`/`helpers_test.go`/`saolei_fixtures_test.go`，deps/embedsrcs 镜像 `agent_v2_game_test`，size = medium）；新 `projects/game/testplan/deploy_agent_v2_drop.yaml`（同 `deploy_agent_v2.yaml` 拓扑，fake-desktop 单实例 env = `FAKE_DESKTOP_SESSION: desktop-e2e-drop`/`FAKE_DESKTOP_SCENARIO: progressive`/`FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS: "3"`）；`deploy_agent_v2.yaml` fake-desktop 收敛单实例（env = `desktop-e2e-won`/`won`）并更新 "proxy is a mandatory hop" 注释为意见 4 分工描述；`system_test.yaml`：`agent-v2-game` description 修剪为 won 拓扑用例集（won 链/缺席/隔离/两流独立），新增 suite `agent-v2-game-disconnect`（deploy = `deploy_agent_v2_drop.yaml`、cases = `agent_v2_game_disconnect_test`，description 按 `style/large_test.md` 惯例写明关注点与拓扑差异），顶部 description 块同步修剪；`bazel build --config=largetest //projects/game/testplan:agent_v2_game_test //projects/game/testplan:agent_v2_game_disconnect_test //projects/game/testplan:desktop_flow_test` 通过；随后执行 T023 重跑验收

**Checkpoint**: 单 fake-desktop 服务（单 manifest/单镜像）；won/drop 行为由 suite 级 deploy env 表达；resource name 解析全 codegen（`game.ParseAgentName`/`ParsePresetName`，生成面并入 `dominion/projects/game` 包——裁定 D）；preset/models 面经 gateway 直连，proxy 仅剩 owner 亲和面；agent_v2 经 053 bootstrap 提供 /healthz:38080（052 探针门控）；三套件（game/disconnect/desktop-flow）验收 green。

---

## Phase 4: User Story 2 - preset 由 agent-v2 管理、在 web 编辑，agent 按单例资源物化 (Priority: P1)

**Goal**: web 侧 preset 管理/agent 物化面板/未物化引导完整可用；`agent-v2-preset` 套件验证 US2 全场景。

**Independent Test**: `guitar run projects/game/testplan/system_test.yaml --suite agent-v2-preset` 全绿（CRUD/物化与模型选择/Update 刷新清记忆/未物化 Send 拒绝/未知模型拒绝/空 prompt 回退——US2 场景 1–7）；组件级：`bazel test //projects/game/web/frontend` 通过。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；`style/large_test.md`（preset 套件组织）
- **官方文档**：`node_modules/.pnpm/@deepseek-ai+dsh-client-ui-primitives@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-client-ui-primitives/`（组件面：Button/Input/DisclosureRow 等既有用法）
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md`（§2 preset 视图/§3 物化面板/§4 事件/§6 测试义务）、`specs/051-agent-v2-dsh-migration/contracts/agent-api.md` §2（web 消费的方法语义与错误）、`specs/051-agent-v2-dsh-migration/data-model.md` §2.1/§2.2/§2.10；`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`（§3 组件契约/§5 API 客户端表——延续基线）；`specs/051-agent-v2-dsh-migration/quickstart.md` §2（preset 套件断言要点）；现状实现源（T024–T027 修改对象）：`projects/game/web/frontend/src/api/conversation.ts`、`projects/game/web/frontend/src/api/sessions.ts`、`projects/game/web/frontend/src/App.tsx`、`projects/game/web/frontend/src/store/chat.ts`

- [ ] T024 [US2] web agent API 客户端：`projects/game/web/frontend/src/api/agent.ts`（`listPresets`/`createPreset`/`getPreset`/`updatePreset`（`update_mask: ["player_prompt"]`）/`deletePreset`/`listModels`/`updateAgent`（PATCH `/api/v2/{agent.name=templates/*/sessions/*/agent}`，body `{agent:{name,preset,model}}`）/`getAgent`，ApiError 复用 `api/conversation.ts` 模式）+ `api/agent.test.ts`；`bazel test //projects/game/web/frontend` 通过
- [ ] T025 [US2] preset 管理视图：`projects/game/web/frontend/src/components/PresetsView.tsx`（列表 + 新建/编辑表单（名称 + player_prompt 多行文本）+ 删除确认 + 空态引导）+ `src/App.tsx` 侧栏视图切换（`sessions | presets` 单页 state）+ `src/theme.css` 视图样式 + `PresetsView.test.tsx`（fetch mock 断言 CRUD 调用形状与空态）；`bazel test //projects/game/web/frontend` 通过
- [ ] T026 [US2] agent 物化面板与引导：`projects/game/web/frontend/src/components/AgentSettingsPanel.tsx`（preset 下拉必选 + model 下拉（listModels + "默认"项）+ Apply=updateAgent）+ ChatPanel 集成（"设置 agent"入口；GetAgent 404 或 Send FAILED_PRECONDITION/NOT_FOUND → 未物化引导态，US2 场景 5；已物化再 Apply 提示刷新语义）+ `AgentSettingsPanel.test.tsx`（数据源/必选校验/请求形状/引导态流转）；`bazel test //projects/game/web/frontend` 通过
- [ ] T027 [US2] 删除编排简化：`projects/game/web/frontend/src/App.tsx` `onDelete` 移除 dispose 跳（仅 `DELETE /api/v1/{name}`（name=`templates/{template}/sessions/{session}` 完整 session 资源名）→ 本地移除）+ `api/conversation.ts` 清理 dispose 残留引用 + `App.test.tsx` 删除编排用例更新（断言无 dispose 调用）；`bazel test //projects/game/web/frontend` 通过
- [ ] T028 [US2] preset 大型测试套件：`projects/game/testplan/agent_v2_preset_test.go`（模块聚焦：preset CRUD 经 gateway 往返/创建编辑后 Get/List 往返一致（Mongo 落库持久化）/物化配置 GetAgent 一致/Update 刷新后历史清空（ListAgentMessages 空）/未物化 Send 400 FAILED_PRECONDITION/未知模型 400/空 prompt 物化后 persona 生效（经 fake-llm 回显断言）；重启级持久化（FR-005 重启不丢）由 T037 全量验收覆盖）；`system_test.yaml` 新增 suite `agent-v2-preset`（复用 deploy_agent_v2.yaml）；`guitar run projects/game/testplan/system_test.yaml --suite agent-v2-preset` 全 green

**Checkpoint**: US1 + US2 均可独立验证（游戏闭环 + 配置闭环）。

---

## Phase 5: User Story 3 - desktop 退化为 flow 控制终端 (Priority: P2)

**Goal**: desktop 移除会话管理/preset 编辑/对话展示（含 chatstream），保留连接（`/api/v2`）/绑定/执行/确认，session 选择只读。

**Independent Test**: `bazel build //projects/game/desktop/...` + `bazel test //projects/game/desktop/...` 通过（保留面用例 green，移除面用例已删）；界面断言：无 session 管理/Profile/对话入口（Go 绑定面与前端组件清单核对 `contracts/web-frontend.md` §5）；真机冒烟记录可选（README）。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/)；`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（desktop 前端 TS 编辑：`projects/game/desktop/frontend/src/api.ts` 等）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §5（移除/保留清单）、`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §5（desktop 侧改向与保留语义）、`specs/051-agent-v2-dsh-migration/data-model.md` §2.6、`specs/051-agent-v2-dsh-migration/research.md` D12（移除依据：chatstream 唯一消费者等）；保留语义源：`projects/game/desktop/app.go`（:1619-1821 Connect/CloseAgent、:638-777 readLoop/handleInboundOperation）、`projects/game/desktop/internal/api/websocket.go`（连接实现——改向对象）

- [ ] T029 [US3] desktop 能力移除：`projects/game/desktop/frontend/src/components/` 删除 `ChatView.svelte`/`ChatMessage.svelte`/`ScreenshotModal.svelte`/`ProfileManagement.svelte`/`ProfileSelectDialog.svelte` 及 `chat-stream.ts`/`stream-merge.ts`/`chat-fifo.ts`；删除 `projects/game/desktop/internal/chatstream/`；`main.go` 移除 chatstream 注册；`app.go` 移除 team/profile/message 绑定（GetTeam/UpdateTeam/RefreshTeam/ListMessages/*TeamProfile*/CreateSession/DeleteSession，:1039-1471 区段）与 chatstream 绑定（:1840-1885）；`view_model.go` 移除对应类型与转换器；`internal/api/client.go`/`api.ts` 移除对应 REST 封装；修剪 `app_test.go`/`view_model_test.go` 对应用例；`bazel build //projects/game/desktop/...` + `bazel test //projects/game/desktop/...` 通过
- [ ] T030 [US3] desktop 改向与只读选择：`projects/game/desktop/internal/api/websocket.go` URL 模板 `/api/v1/.../connect` → `/api/v2/templates/{template}/sessions/{session}/connect`（GatewayURL/探测/readLoop 语义零改动）；`SessionList.svelte` 修剪为只读列表 + 选择（移除新建/删除/刷新按钮，数据源保留 `ListSessions`）；`App.svelte` 移除 session 管理与 Profile 分支流程（保留连接/绑定/执行/确认抽屉/日志视图）；更新对应测试；`bazel test //projects/game/desktop/...` 通过
- [ ] T031 [US3] desktop 冒烟与说明：`projects/game/desktop/README.md` 更新（能力清单=连接/绑定/执行/确认/配置/日志；连接目标 `/api/v2` flow 流；真 desktop 大测豁免说明——宪法 VI 豁免登记：desktop GUI 无法进 CI 大测，链路由 `desktop-flow` 套件以 fake-desktop 覆盖）；如具备 Windows 环境，按 `specs/051-agent-v2-dsh-migration/quickstart.md` §3 记录一次真机冒烟结果

**Checkpoint**: desktop 为纯 flow 控制终端；US1–US3 全部可独立验证。

---

## Phase 6: User Story 4 - web session 侧栏交互优化 (Priority: P2)

**Goal**: 侧栏四项交互（标题单行/图标按钮/`···` 删除菜单/长名虚化 + 悬停滚动）经组件级测试验证。

**Independent Test**: `bazel test //projects/game/web/frontend`（SessionList 用例：标题不换行/图标可辨识可点/`···` 菜单删除可用/长名虚化与悬停滚动行为断言）。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：`node_modules/.pnpm/@deepseek-ai+dsh-client-ui-primitives@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-client-ui-primitives/`（图标/按钮组件面）
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md` §1（四项契约与 testid 约定）、`specs/049-agent-v2-dsh-init/contracts/web-frontend.md` §3.2（SessionList 基线）；`projects/game/web/frontend/src/theme.css` 与 `src/components/SessionList.tsx`（改造对象——阅读现状实现）

- [ ] T032 [P] [US4] 侧栏样式：`projects/game/web/frontend/src/theme.css`——`.sidebar-title` `white-space: nowrap`（FR-001）；session 名称容器右侧渐隐遮罩（无滚动条不换行）+ 悬停横向滚动（滚动条隐藏）+ 移出复位类（FR-004 的 CSS 面）
- [ ] T033 [US4] SessionList 交互：`projects/game/web/frontend/src/components/SessionList.tsx`——新建/刷新改图标按钮（加号/圆环箭头 + aria-label/tooltip，保留 `data-testid`，loading 禁用延续，FR-002）；每条目右侧 `···` 按钮弹出菜单含"删除"（不依赖选中态，FR-003）；长名悬停滚动 + `scrollLeft` 复位（FR-004 的行为面）；`SessionList.test.tsx` 扩展四项断言；`bazel test //projects/game/web/frontend` 通过

**Checkpoint**: 全部 user story 完成；进入 v1 处置与全量验收。

---

## Phase 7: v1 处置与 testplan 重组 (Cross-Cutting)

**Purpose**: FR-019/SC-005——v1 链路自部署与路由面移除（代码保留仓库），testplan 套件终态化。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/golang.md`；`style/large_test.md`（**反模式清单**：禁止按交付物维度组织测试、平行测试计划——本 phase 重组必须遵守）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/research.md` D14（处置决策）、`specs/051-agent-v2-dsh-migration/spec.md` FR-019（范围与排除）、`projects/game/testplan/system_test.yaml` 与 `projects/game/deploy.yaml`（重组对象——阅读现状 suites/deploy 绑定）

- [ ] T034 v1 路由面移除：`projects/game/gateway/cmd/main.go`——移除 TeamService/PromptService handler 注册、`promptConn`、`/api/v1` WS connect 分支与路径匹配（teamConn 保留承载 v2 面）；`projects/game/proxy/`——删除 `handler/handler.go`（TeamHandler）、v1 agentclient manager 与 `agent_owners` 使用（`cmd/main.go`/`runtime/`）；更新 `gateway/cmd/main_test.go` 与 proxy 测试（`/api/v1` team/prompt 路由 404 断言、session/memory 路由回归）；`bazel test //projects/game/gateway //projects/game/proxy/...` 通过
- [ ] T035 部署修剪：`projects/game/deploy.yaml` 移除 `prompt` 与 `agent`（v1）条目及 v1 secret 引用（memory/session/proxy/agent-v2/web/gateway/mongo 保留）；`projects/game/deploy.yaml` 注释同步（终态表述，不残留迁移过程描述）
- [ ] T036 testplan 终态化：`projects/game/testplan/system_test.yaml`——移除 v1 suites（agent-dialog/agent-queue/checkpoint-resume/concurrent-serialization/agent-multimodal/agent-operation/agent-saolei/saolei-team/agent-stall）；`session` 与 `memory` 套件迁至 `deploy_agent_v2.yaml`（memory 套件仅保留 gateway 路由 CRUD 用例，v1 agent 驱动的 planner/memory 工具流用例删除）；`agent-v2-conversation` suite 描述更新为 051 面（更名 API + 工具块）；删除 `deploy_agent.yaml`/`deploy_agent_stall.yaml` 与 v1 专属 case binaries（agent_dialog/agent_checkpoint/agent_multimodal/agent_operation/agent_saolei/agent_stall/saolei_team 等 `*_test.go` 与 BUILD targets）、`helpers_test.go` 中仅 v1 使用的 helper（team/profile/WS 面板）；受影响文件按 `style/golang.md` 与 `style/large_test.md` 修订；`bazel build //projects/game/testplan/...` + `bazel test //projects/game/testplan`（中型）通过

**Checkpoint**: game 部署无 v1 agent/prompt；测试计划为终态（一份 YAML，v2 面套件）。

---

## Phase 8: 大型测试验收与收尾 (Polish & Acceptance)

**Purpose**: constitution 原则 VI 验收门禁 + 文档/安全收尾。

**文档清单（编码前必读）**：

- **代码规范文档**：`style/large_test.md`
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/051-agent-v2-dsh-migration/quickstart.md`（验收对照）、`tools/test/guitar/README.md`（guitar 用法）、`.opencode/skills/testplan/SKILL.md`（执行 skill 指引：安装、`guitar run`、失败排查 signoz 流程）

- [ ] T037 全量大型测试验收：`bazel run //:deploy_install` + `bazel run //:guitar_install` 后执行 `guitar validate projects/game/testplan/system_test.yaml` 与 `guitar run projects/game/testplan/system_test.yaml`——全部 suites（session/memory/agent-v2-conversation/agent-v2-preset/agent-v2-game/agent-v2-game-disconnect/desktop-flow）**所有用例通过**（零 failed、零 flaky；失败用 signoz skill 查 tracing/log 修复后重跑至全 green；constitution 原则 VI——构建检查不构成验收）；**FR-005 重启持久化**：验收环境内对 agent-v2 执行一次重启（deploy 工具重启/重建该服务）后经 preset Get/List 读回，断言重启前创建的 preset 数据仍存（US2 验收项）
- [ ] T038 手工冒烟与文档收尾：按 `specs/051-agent-v2-dsh-migration/quickstart.md` §3 执行可选项并记录（如具备环境）；更新 `projects/game/agent_v2/README.md`（组合清单/物化语义/环境变量 MONGO_URI）、`projects/game/fake-desktop/README.md`（新建设施说明）、`projects/game/testplan/README.md`（套件拓扑变更）
- [ ] T039 终态一致性检查：全交付物 grep 无明文 token（SC-006：`GLM_API_KEY` 值、secret 内容）；核对 spec FR-001..FR-020 与 SC-001..SC-006 逐条达成；确认无 v1 半迁移残留描述（constitution 原则 VII 终态表述）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，立即开始（T001 → T002/T003 可并行）
- **Foundational (Phase 2)**: 依赖 Phase 1（T006/T007 仅需 llm-glm 包，可与 Phase 1 并行；T004/T005/T008 依赖 T002 包骨架）——**阻塞全部 user story**
- **US1 (Phase 3)**: 依赖 Phase 2 完成；内部顺序：T009/T010/T017 可并行 → T011 → T012 → T013；T014 → T015（T016 并行）；T018/T019 在 T005 后可与插件线并行；T020/T021 独立并行；T022 汇总；T023 验收
- **Phase 3R (Directive 2026-09-01 返工)**: 依赖 Phase 3 交付物（在其之上返工）；内部顺序：T004a 先行（proto/契约/codegen）→ T014a ∥ T018a ∥ T019a（不同目录并行；agent_v2 线内 **T014a → T014b 同文件 serial**——server.ts 注册拆分先、bootstrap 接入后）；T022a → T022b（与 T004a 线无文件交集，可并行）；T023 重跑验收收口（**前置 T014b**：agent_v2 须提供 /healthz:38080 方可通过 052 startupProbe，directive §8）；**阻塞 Phase 4**（T024 消费拆分后的 PresetService API 面）
- **US2 (Phase 4)**: 依赖 Phase 3 的 T014/T015/T017/T018（AgentService 面与 web 适配就绪）+ Phase 3R 的 T004a/T014a/T019a（PresetService 面就绪）；T024 → T025 → T026 → T027 → T028（T025/T026 均集成于 `App.tsx`，串行执行避免同文件冲突）
- **US3 (Phase 5)**: 依赖 T019（gateway WS 入口）与 T022/T023（fake-desktop 桥接面验证）；T029 → T030 → T031
- **US4 (Phase 6)**: 仅依赖 Phase 2（T004/T005 的 API 形状）；T032 → T033；**可最早并行插入**（与任何 phase 无文件冲突）
- **Phase 7**: 依赖 US1–US3 完成（v2 面全部就绪后方可下线 v1）
- **Phase 8**: 依赖全部前序 phase

### User Story Dependencies

- **US1 (P1)**: Foundational 后开始；不依赖其他故事（其独立测试所需的 preset CRUD/物化面已在 Foundational + US1 内交付）
- **US2 (P1)**: 消费 US1 交付的 AgentService 面；web 文件与 US1 的 T017 有先后（T017 先）但无冲突
- **US3 (P2)**: 依赖 US1 的桥接链路（T019/T022）；desktop 文件与其他故事零交集
- **US4 (P2)**: 完全独立（纯前端侧栏，仅 `theme.css`/`SessionList.tsx`/其测试）

### Parallel Opportunities

- Phase 1: T002/T003 并行（不同包目录）
- Phase 2: T006/T007 并行（llm-glm 包内不同文件+测试）；T004→T005 串行
- Phase 3: 三条并行线——插件线（T009/T010 → T011 → T012 → T013）、宿主线（T014 → T015 ∥ T016）、路由线（T018 ∥ T019）+ 测试设施线（T020 ∥ T021）+ web 线（T017）
- Phase 4: T024 → T025 → T026 → T027（web 文件链串行：api → 视图 → 面板 → 编排简化）→ T028
- 多人协作：Foundational 完成后，US4 可随时独立插入；US1 的四条线可分人

---

## Parallel Example: User Story 1

```bash
# 插件线与宿主/路由/测试设施线并行推进：
Task: T009  "desktop-bridge 插件" (common/js/dsh-plugins/desktop-bridge/)
Task: T010  "saolei-loop 驱动器与工厂" (common/js/dsh-plugins/saolei-loop/src/driver.ts)
Task: T017  "web 适配与工具结果呈现" (projects/game/web/frontend/src/)
Task: T020  "fake-desktop 测试设施" (projects/game/fake-desktop/)

# 汇总前可并行：
Task: T018  "proxy 转发面" (projects/game/proxy/handler/)
Task: T019  "gateway WS 入口" (projects/game/gateway/cmd/main.go)
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Phase 1 + Phase 2 → 契约面与共享模块就绪
2. Phase 3 (US1) → `guitar run --suite agent-v2-game` green = **MVP 达成**（"常规 agent 可以进行游戏"）
3. STOP and VALIDATE：独立验证 US1（fake 拓扑端到端 + 049 零回归）

### Incremental Delivery

1. Setup + Foundational → 基座
2. US1 → 游戏闭环（MVP，可演示）
3. US2 → 配置闭环（preset/物化/模型选择）
4. US3 → desktop 终端化
5. US4 → 侧栏打磨（可随时插入）
6. Phase 7 → v1 下线（SC-005）
7. Phase 8 → 全量验收（SC-001..006 终判）

---

## Notes

- 每个实现任务内嵌其编译 + 单测门禁（constitution 原则 IV：`bazel build` + `bazel test` 相关 target，作为任务一部分，不单列）
- Go 代码格式化 `bazel run //:go -- fmt <files>`；TS 遵循各包既有 lint/格式；BUILD 变更后目标目录下 `bazel run //:gazelle <dir>`
- 大型测试执行必须走 testplan skill（`guitar run`，`.opencode/skills/testplan/SKILL.md`）；验收标准 = 全部用例通过（任何 failed/flaky 即未通过，修复重跑）
- v1 代码保留仓库不删除（FR-019：部署移除 ≠ 代码删除）；但 testplan 的 v1 case binaries/deploys/suites 属测试资产，随面下线删除（Phase 7）
- 注释与文档遵守 constitution 原则 I/VII：引用带路径/URL、只表述终态
