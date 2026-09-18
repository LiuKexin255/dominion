# Tasks: desktop-bridge-split

**Input**: Design documents from `/specs/066-desktop-bridge-split/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: spec 的 Success Criteria 显式要求单测（SC-003/SC-005 的单测面）与大型测试（SC-001/SC-002/SC-004/SC-006，SC-003 另有端到端用例）——单测按 constitution 原则 IV 随实现任务交付；大型测试执行为独立验收任务（原则 VI）。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## 通用约定（每个代码任务随行，不单列 task）

- 每次代码变更随行 `bazel build` + `bazel test`（相关 target，constitution 原则 IV）；新增/删除源文件后在对应目录执行 `bazel run //:gazelle <dir>` 更新 BUILD.bazel（AGENTS.md 流程）。
- 对同一文件的编辑串行进行（AGENTS.md）；代码注释遵守 constitution 原则 I/VII（引用带仓库相对路径或完整 URL、只表述终态）。
- AGENTS.md 与本 feature 的 spec.md 为必读（spec.md 整篇必读，不整篇重复列入各 phase 文档清单；phase 仅按需标注需重点对照的 spec 章节）。
- **编译原子性与分阶段策略**：proto 新增/替换 RPC 都会立即改变生成的 handler 接口（Go server 接口与 TS `DesktopBridgeServiceHandlers` 的**必填**方法），故 Phase 1-5 采用"增量并存"推进（新增 RPC 与新实现逐层落地，`Connect`/WS 桥/`WSClient` 保留至 Phase 6 统一删除）；`agent_v2` 的 TS handler 适配必须与新 RPC 同期补齐（`Stream` 在 T004、`SendFrame` 在 T009），各 phase 的 Checkpoint 按此裁剪验证范围；最终状态 = 干净的新框架、零旧框架残留（用户指令 + spec FR-009；中间开发提交不入交付物，原则 VII）。

---

## Phase 1: Foundational — proto 契约（阻塞全部 US）

**Purpose**: `DesktopBridgeService` 落地两个带 HTTP 注解的新 RPC 与请求/响应消息；`Connect` 暂留（Phase 6 删除）。无独立 Setup 需求——既有服务/依赖零新增。

**文档清单**：

- 代码规范文档：`style/api.md`；[AIP-136 Custom methods](https://google.aip.dev/136)（custom method 命名/HTTP 动词/`:` 动词 URI/Request-Response 消息族——已通读全文）；[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)（`google.api.http` 注解、body 映射与 path 参数覆盖语义）；[AIP-192 Documentation](https://google.aip.dev/192)（Service/Method 注释要求）
- 官方文档：无
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/research.md` D8（proto 精确形状——含 `SendFrameResponse` 空响应裁定）；`specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md` §1（端点与线格式表）；`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §1（帧语义基线——UserFrame/TeamFrame 词汇表沿用依据）；`survey/desktop-bridge-connect-split.md` §7（D1–D3 决策原文）；`projects/game/game.proto`（帧与信号类型定义——新 RPC 请求/响应消息的帧词汇表；051 contract §7 间接引用）

**Tasks**:

- [X] T001 在 `projects/game/agent_v2.proto` 的 `DesktopBridgeService` 按 research.md D8 精确形状新增：`rpc Stream(StreamRequest) returns (stream projects.game.TeamFrame)`（`get: "/api/v2/{session=templates/*/sessions/*}:stream"`）与 `rpc SendFrame(SendFrameRequest) returns (SendFrameResponse)`（`post: "/api/v2/{session=templates/*/sessions/*}:sendFrame"`、`body: "*"`）；新增 `StreamRequest`（`string session = 1` REQUIRED——session 资源名，path 注入）与 `SendFrameRequest`（`string session = 1` REQUIRED + `projects.game.UserFrame frame = 2` REQUIRED——path 参数在 body 解码后覆盖 session）与空 `SendFrameResponse`；Service/Method 注释按 `style/api.md`（Service 注释含 Prefix Path、Method 注释含语义与 spec/契约引用）；`Connect` RPC 原样保留；执行 `bazel build //projects/game:game_go_proto //projects/game/agent_v2:agent_v2_types` 确认生成物（Go：`RegisterDesktopBridgeServiceHandler`、`DesktopBridgeService_StreamServer`/`SendFrame` client+server 类型；TS：`projects/game/agent_v2/agent_v2_types/projects/game/v2/DesktopBridgeService.js` 面更新；`agent_v2` 的 TS handler 适配在 T004/T009 补齐，见「通用约定」第 4 条）

**Checkpoint（验证门禁）**: `bazel build //projects/game:game_go_proto //projects/game/agent_v2:agent_v2_types` 全绿；生成的网关注册函数与新 RPC 类型可见（`agent_v2` TS handler 适配在 T004/T009 完成前不纳入本门禁——见「通用约定」第 4 条）。

---

## Phase 2: User Story 1 — 服务端下行流 (Priority: P1) 🎯 MVP

**Goal**: `:stream` 端到端：gateway 生成路由注册 → proxy `Stream` 转发（get-or-create owner + ServerStreamBinder）→ 插件 `Stream` 面（attach 接管 + 首帧 status 推送 + 10s 心跳 + 断连清理）。US3 的连接生命周期语义（接管 no-op、断连 settle）随 attach 在本 phase 交付。

**Independent Test**: proxy/插件单测绿——插件侧 fake StreamCall double 验证首帧推送、心跳启停、接管（旧流 end + 晚到断开 no-op）、断开（compare-and-delete + 在途 settle "desktop disconnected"）、`isDesktopConnected` 读取面（`agent_v2` 的 TS handler 因生成接口同时要求 `SendFrame` 而待 Phase 3 T009 补齐后恢复全绿）。

**文档清单**：

- 代码规范文档：`style/golang.md`（proxy/gateway）+ Google Go Style（[Guide](https://google.github.io/styleguide/go/guide) / [Decisions](https://google.github.io/styleguide/go/decisions) / [Best Practices](https://google.github.io/styleguide/go/best-practices)，style/golang.md 引用的外部规范）；`style/javascript.md`（插件）+ [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) + [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（style/javascript.md §Mock 约定引用的外部规范）
- 官方文档：[grpc-gateway: Customizing your gateway — Stream Error Handler](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/#stream-error-handler)（server streaming 的 newline-delimited JSON 信封与流异常终止语义）；[gRPC Node Basics — server streaming](https://grpc.io/docs/languages/node/basics/)（grpc-js 服务端流的 write()/end() 写入面）
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md` §2（连接语义 1-5 条）/§4（proxy 转发表）；`specs/066-desktop-bridge-split/data-model.md` §1.1/§1.3/§2（流状态迁移与注册表不变量）；`specs/066-desktop-bridge-split/research.md` D10/D12；`specs/066-desktop-bridge-split/spec.md` US1/US3 验收场景；`survey/deepseek-harness-agent-loop-prereq.md` §5.1/§5.4（dsh 插件 Service/inject 形态；051 contract §7 间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（style/javascript.md §执行模型引用的 vitest shim 退出码契约）

**Tasks**:

- [X] T002 [P] [US1] 在 `projects/game/proxy/handler/bridge.go` 新增 `Stream(req *game.StreamRequest, stream game.DesktopBridgeService_StreamServer) error`：`parseAgentSession(req.GetSession())`（复用 `agent.go` 同款校验）→ `assignAgentOwner`（get-or-create——desktop 可先于 team 物化连接）→ `agentV2Conn` → `newBridgeClient(connRef.Conn).Stream(ctx, req)` → `BindServerStream` 转发；错误映射与既有 `Connect` 一致（status 承载拒绝保留原码、非 status 传输故障 → `UNAVAILABLE`）；`DesktopBridgeHandler` 增 `streamBinder bind.ServerStreamBinder[game.TeamFrame]` 字段（`binder bind.Binder` 字段暂留供 `Connect`，Phase 6 删）；在 `projects/game/proxy/cmd/main.go` 装配处传入 `bind.NewServerStreamBinder[game.TeamFrame]()`；扩展 `projects/game/proxy/handler/mocks_test.go`（Stream server/upstream double）与 bridge 单测（owner get-or-create、错误映射、响应帧转发——参照 `agent.go` Send 的既有测试形态）
- [X] T003 [P] [US1] [US3] 在 `common/js/dsh-plugins/desktop-bridge/src/wire.ts` 新增 `StreamCall` 结构面（`request: { session?: string }`、`write(frame: BridgeTeamFrame)`、`end()`、`on("error" | "close" | "cancelled", ...)`——grpc-js `ServerWritableStream` 的结构超集；既有 `BidiStream` 结构上满足该面，`Connect` 暂可继续传入）并在 `DesktopBridgeServiceHandlers` 增 `Stream(call: StreamCall): void`；在 `common/js/dsh-plugins/desktop-bridge/src/bridge.ts` 实现 `Stream(call)`：`sessionName = call.request.session`（空值 warn + return）；`attach(sessionName, call)` 扩展为——接管（end 旧流 + 覆盖注册，既有语义）→ 注册 → **立即写一帧 status TeamFrame**（`STATUS_SIGNAL_STATUS_ACTIVE`，经 `buildFlowFrame` 全信封）→ 启动心跳 `setInterval`（10_000ms，写同型 status 帧）→ 挂 disconnect 事件（cancelled/error/close → 既有 `disconnect` compare-and-delete + settle 在途派发）；`Connection` 增心跳定时器字段（接管/断开时 `clearInterval`）；`dispatch`/`handleResult`/`disconnect`/pending 管线零改动；扩展 `common/js/dsh-plugins/desktop-bridge/src/bridge.test.ts`（首帧推送、心跳启停与清理、接管 no-op、断开 settle、`isDesktopConnected`——注入 fake `StreamCall` double，`style/javascript.md` Mock 约定）
- [X] T004 [US1] 在 `projects/game/agent_v2/src/server.ts` 的 `buildDesktopBridgeHandlers` 增 `Stream: (call) => bridge.Stream(call as unknown as StreamCall)`（生成类型 `DesktopBridgeServiceHandlers` 适配）；适配 `projects/game/agent_v2/src/server.test.ts`（handler 注册面；`agent_v2` 整体编译在 T009 补齐 `SendFrame` 后恢复——见「通用约定」第 4 条）
- [X] T005 [P] [US1] 在 `projects/game/gateway/cmd/main.go` 注册 `game.RegisterDesktopBridgeServiceHandler(ctx, gwmux, teamConn)`（与 `RegisterAgentServiceHandler` 相邻；teamConn 长流 keepalive 与 8MB hop 已配对）；WS 分支暂留（`:stream`/`:sendFrame` 路径不命中 WS 分支、自然落 gwmux）；此时 `SendFrame` 在 proxy 侧为 Unimplemented 占位（Phase 3 实现）——注册无消费方调用即无影响

**Checkpoint（验证门禁）**: `bazel test //projects/game/proxy/... //projects/game/gateway/... //common/js/dsh-plugins/desktop-bridge/...` 全绿；插件单测覆盖 attach 全语义（首帧/心跳/接管/断连）。（`agent_v2` 因生成的 handler 接口要求 `SendFrame` 而保持红，待 Phase 3 T009 补齐后恢复——见「通用约定」第 4 条。）

---

## Phase 3: User Story 2 — 服务端上行单发 (Priority: P1)

**Goal**: `:sendFrame` 服务端闭环：gateway octet-stream marshaler + 8MiB body 守卫 → proxy unary 转发（lookup-only owner）→ 插件 `SendFrame`（无活跃流 → `FAILED_PRECONDITION`；回执按 tool_id resolve）。

**Independent Test**: gateway 中间件单测（413/归因报错、≤5MiB 放行、路由无干扰）+ 插件单测（无流拒绝、回执 resolve、part 分派）+ proxy 单测（lookup-only、错误保码）全绿。

**文档清单**：

- 代码规范文档：`style/golang.md` + Google Go Style（[Guide](https://google.github.io/styleguide/go/guide) / [Decisions](https://google.github.io/styleguide/go/decisions) / [Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) + [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（style/javascript.md §Mock 约定引用的外部规范）
- 官方文档：[grpc-gateway: Customizing your gateway](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/)（`WithMarshalerOption` 注册自定义 marshaler——已通读全文）；[gRPC status codes](https://github.com/grpc/grpc/blob/master/doc/statuscodes.md)（`FAILED_PRECONDITION = 9`——插件侧错误码常量依据）
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md` §2（语义 6-9 条）/§3（网关装配——marshaler 与 8MiB 守卫）；`specs/066-desktop-bridge-split/research.md` D9/D12 与 §1（错误响应编码行——desktop 不设 Accept 的依据）；`specs/025-desktop-image-state-refine/contracts/image-transport-contract.md` §2/§4（二进制直传与"超限清晰归因、绝不静默截断"守卫意图的延续基线）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（style/javascript.md §执行模型引用的 vitest shim 退出码契约）

**Tasks**:

- [X] T006 [P] [US2] 在 `projects/game/gateway/cmd/main.go`：gwmux 选项改为 `runtime.NewServeMux(append(pgrpc.GatewayDefault(), runtime.WithMarshalerOption("application/octet-stream", &runtime.ProtoMarshaller{}))...)`；新增 body 上限中间件并包裹 gwmux（仅命中 sendFrame POST 路径：`Content-Length` 预检 > 8MiB（8*1024*1024，与 gRPC hop 对齐）直接 `413`；其余命中请求套 `http.MaxBytesReader(w, r.Body, 8MiB)`——超限读取经 gateway 解码报 "request body too large" 类清晰错误，永不静默截断）；中间件产物交由既有 `phttp.Handler(...)` 包装；在 `projects/game/gateway/cmd/main_test.go` 新增单测（413 预检归因、超限读取报错、≤5MiB body 正常放行不误伤、`:stream` GET 与其他 POST 路径不受影响、`Content-Type: application/json` 路由不受 octet-stream 注册影响）
- [X] T007 [P] [US2] 在 `projects/game/proxy/handler/bridge.go` 新增 `SendFrame(ctx context.Context, req *game.SendFrameRequest) (*game.SendFrameResponse, error)`：`parseAgentSession(req.GetSession())` → `lookupAgentOwner`（**lookup-only**，与 `agent.go` 的 `Cancel` 同族——回执永不分配 owner；无 owner → `NOT_FOUND`）→ `agentV2Conn` → `newBridgeClient(connRef.Conn).SendFrame(ctx, req)` → 错误经 `propagateAgentError` 保码透传（参照 `agent.go:253-261` 形态）；扩展 mocks/单测（lookup-only 断言、错误映射、请求原样转发）
- [X] T008 [P] [US2] 在 `common/js/dsh-plugins/desktop-bridge/src/wire.ts` 新增 `SendFrameCall` 结构面（`request: { session?: string; frame?: BridgeUserFrame }`、callback 面）并在 `DesktopBridgeServiceHandlers` 增 `SendFrame(call, callback)`；在 `common/js/dsh-plugins/desktop-bridge/src/bridge.ts` 实现 `SendFrame(call, callback)`：session 取 `call.request.session`（空 → `INVALID_ARGUMENT` 错误码 3）；`frame` 缺失或无 `flowParts` → `INVALID_ARGUMENT`；`connections` 无该 session → `callback({ code: 9, message: "desktop stream not active" })`（`FAILED_PRECONDITION`，常量注释引用 statuscodes.md，不引入 grpc-js 依赖）；有连接 → 复用 `routeFrame`（`flowResult` → `handleResult`；`wait`/`warn` → 既有日志；其余 part（含 `status`）warn 忽略）→ `callback(null, {})`；在 `common/js/dsh-plugins/desktop-bridge/src/index.ts` 的 `DesktopBridgeService` 公共接口面同步增 `Stream`/`SendFrame` 方法签名；扩展 `bridge.test.ts`（无流 FAILED_PRECONDITION、回执 resolve 在途派发、INVALID_ARGUMENT 两型、wait/warn 日志、迟到回执 resolve 旧 pending）
- [X] T009 [US2] 在 `projects/game/agent_v2/src/server.ts` 的 `buildDesktopBridgeHandlers` 增 `SendFrame: (call, cb) => bridge.SendFrame(call, cb)`（生成类型适配）；适配 `projects/game/agent_v2/src/server.test.ts`

**Checkpoint（验证门禁）**: Phase 2+3 单测全绿；服务端 `:stream` + `:sendFrame` 闭环可经插件单测双打全链路验证（fake StreamCall + SendFrame 调用）。

---

## Phase 4: User Story 1+2 — desktop 客户端切换 (Priority: P1)

**Goal**: 真实 desktop 以 HTTP 桥客户端工作：NDJSON 下行读取（首帧超时判定）+ octet-stream 上行 POST（有界重试）；`WSClient` 不再被引用（文件 Phase 6 删）。

**Independent Test**: `bridgeclient_test.go`（httptest 双打：首帧延迟/超时、NDJSON 逐行、error 尾块、octet-stream body、错误体分派解析）+ `app_test.go` 适配全绿。

**文档清单**：

- 代码规范文档：`style/golang.md` + Google Go Style（[Guide](https://google.github.io/styleguide/go/guide) / [Decisions](https://google.github.io/styleguide/go/decisions) / [Best Practices](https://google.github.io/styleguide/go/best-practices)）
- 官方文档：无
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md` §1.1（错误体线格式——Content-Type 分派依据）/§5（desktop 客户端行为 5 条——首帧 10s 超时、逐行解析、无 Accept、错误体分派、有界重试、ctx cancel 关闭）；`specs/066-desktop-bridge-split/data-model.md` §1.4（下行信封帧两形态）；`specs/066-desktop-bridge-split/research.md` D13 与 §1（错误响应编码行——marshaler 选择与错误体编码的源码核验）/§3（NDJSON 分块边界风险——按行不按 chunk）

**Tasks**:

- [X] T010 [P] [US1] 新增 `projects/game/desktop/internal/api/bridgeclient.go`：`BridgeClient`——`Connect(ctx, gatewayURL, template, sessionID, env)`（`GET {gw}/api/v2/templates/{t}/sessions/{s}:stream`，`env` 非空则设 header（复用 `client.go:44-49` 模式与 `trace.NewHTTPTransport()`），不设 Accept；非 2xx → 读 JSON 状态信封（契约 §1.1，可 `protojson` 解为 `google.rpc.Status`）取 message 返回错误；2xx 记录 body reader 与请求 ctx cancel）；`RecvFrame(ctx)`（`bufio.Reader.ReadBytes('\n')` 逐行——不假设 1 chunk = 1 消息；行 JSON 解析：`result` 字段 → `protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal` 为 `*game.TeamFrame`（FR-007 前向兼容），含 `error` 字段 → 返回携带其 message 的错误——流异常终止）；`WaitFirstStatus(ctx, timeout)`（10s 内读首行并提取 status 枚举名字符串，超时/失败关闭并报错——连接确认判定）；`SendFrame(ctx, frame *game.UserFrame)`（`POST .../sessions/{s}:sendFrame`，`Content-Type: application/octet-stream`，body = `proto.Marshal(&game.SendFrameRequest{Frame: frame})`，不设 Accept——错误体线格式见契约 §1.1；非 2xx 按响应 `Content-Type` 的 media type（`mime.ParseMediaType` 去参数）分派：`application/octet-stream` → `proto.Unmarshal` 为 `google.golang.org/genproto/googleapis/rpc/status` 的 `Status`、经 `status.FromProto` 取 gRPC code 与 message（grpc-go `status` 包同源类型，无新增依赖），其余（413 预检 `text/plain` 等）→ body 原文作 message；返回的错误统一携带 HTTP 状态码、gRPC code（可解出时）与 message）；`Close()`（cancel 请求 ctx + 关闭 body——解除阻塞的行读取）；新增 `projects/game/desktop/internal/api/bridgeclient_test.go`（`httptest` 双打：首帧即时/延迟/超时、单 chunk 多行与单行跨 chunk、`{"error":...}` 尾块、octet-stream 请求体字节断言（含近上限 ≤5MiB 逐字节断言）、错误体分派解析——413 纯文本、400 二进制 `google.rpc.Status`（断言 code=9 FAILED_PRECONDITION）、`:stream` 非 2xx JSON 信封）；`bazel run //:gazelle projects/game/desktop/internal/api`
- [X] T011 [US1] [US2] [US3] 在 `projects/game/desktop/app.go` 将 `WSClient` 切换为 `BridgeClient`（字段、构造与引用面）：`Connect(template, sessionID)` 流程——既有 reconnect handover（关旧连接 + `recvMu` 下等 `recvDone`，`app.go:946-962` 语义原样保留）→ `bridge.Connect` → `WaitFirstStatus`（10s）取 status 枚举名返回前端（展示语义保持）→ 存客户端 + `recvDone` 重置 + 起 `readLoop`（循环体/错误处理不变）；**删除探针帧构造与发送代码**（`app.go:976-1047` 的 probe 构造/发送/echo 解析——首帧改由服务端推送）；回执发送路径（`app.go:497/508` 一带的 `SendFrame` 调用）改为 `bridge.SendFrame` 并包**有界重试**（3 次、间隔 1s——判定只认 T010 错误携带的 gRPC code `FAILED_PRECONDITION`（HTTP 400 与 INVALID_ARGUMENT 共码，不可凭 HTTP 状态判定），覆盖接管间隙；最终失败记日志返回错误，与今日写失败行为对齐）；`CloseAgent` 适配（`Close()` 的 ctx cancel 即解除 readLoop 阻塞读——`recvDone` 等待保持）；适配 `projects/game/desktop/app_test.go` 与 `projects/game/desktop/view_model_test.go` 中相关 double/断言

**Checkpoint（验证门禁）**: `bazel test //projects/game/desktop/...` 全绿；`websocket.go` 无引用（编译产物不含其符号）。

---

## Phase 5: User Story 5 — fake-desktop 与 testplan 切换 (Priority: P2)

**Goal**: fake-desktop 以 HTTP 工作（含断连注入语义）；testplan 共享 flow 客户端 helper 与 desktop_flow 六用例改写为 HTTP；game 系（经 fake-desktop）用例断言零变化。

**Independent Test**: fake-desktop 单测（HTTP 双打）绿；testplan 编译绿（执行验收在 Phase 7）。

**文档清单**：

- 代码规范文档：`style/golang.md` + Google Go Style（[Guide](https://google.github.io/styleguide/go/guide) / [Decisions](https://google.github.io/styleguide/go/decisions) / [Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/large_test.md`（测试组织/反模式——按模块归位、复用 helper、禁止平行测试计划）
- 官方文档：无
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md` §5（fake-desktop 断连注入：第 N 笔回执 POST 完成后断流）/§6（验收锚点）；`specs/066-desktop-bridge-split/research.md` D14/D15；`specs/066-desktop-bridge-split/quickstart.md` §2（用例与预期表）；`specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md` §6（验收锚点基线——既有断言语义的迁移来源）；`projects/game/desktop/internal/api/websocket.go`（既有 WS 连接实现——HTTP 切换的对照源；051 contract §7 间接引用）；`projects/game/testplan/agent_v2_helpers_test.go`（共享 flow 客户端 helper——HTTP 重写对象）

**Tasks**:

- [ ] T012 [P] [US1] [US2] [US3] 在 `projects/game/fake-desktop/service/session.go` 重写连接层（`executor.go`/`scenarios.go`/`board.go` 零改动）：`serveOnce` —— `GET :stream`（首行 10s 超时判定，连接日志保留 session/scenario 字段）→ 循环逐行读 `TeamFrame` → 逐 part `executor.Execute` → 回执经 `POST :sendFrame`（octet-stream，同契约 §5 形态）；外层重连循环（`reconnectDelay` 2s）与 `executor.Disconnected()` 断连注入点保持——**回执 POST 完成即已抵达 agent_v2（unary 完成语义），删除 `disconnectDrainPause` 常量与等待逻辑**；`writeFrame`/`readFrame`/`convertToWS` 替换为 HTTP helper（或删除）；重写 `projects/game/fake-desktop/service/session_test.go`：WS server double → `httptest` NDJSON 流 double + POST 记录断言（首帧、回执体、断连注入时序）
- [ ] T013 [P] [US5] testplan HTTP 客户端迁移（消灭 `projects/game/testplan/` 全部 WS flow 引用）：(A) 重写 `agent_v2_helpers_test.go` 的共享 flow 客户端 helper（`connectAgentV2Flow`/`sendFlowUserFrame`/`dialAgentV2FlowProbed`/`sendFlowProbe`/`sendFlowReceipt*`/`replyFlowReceipt`/`readFlowTeamFrame*`/`serveTeamFlowScript`）为 `openDesktopStream`（`GET :stream`，env header，返回行读取器 + cancel func）与 `postUserFrame`（octet-stream POST），删除 `sendFlowProbe`/probe 相关 helper；(B) 重写 `desktop_flow_test.go` 用例为 HTTP——(1) `TestDesktopFlowFirstStatusFrame`（开流 10s 内收到 `STATUS_SIGNAL_STATUS_ACTIVE` status 帧，取代 probe-echo、无需上行探针）(2) 接管（第二流打开后旧流读取终止、新流持续收帧，既有断言语义保持）(3) 派发→回执→识别闭环（team 链路 + POST 回执 + 既有棋盘断言）(4) 在途断连（cancel 流后 dispatch 工具错误 "desktop disconnected"，既有断言语义保持）(5) 空闲存活（无操作请求期持续收到心跳帧、链路不被中间层断开——SC-004）(6) 超限归因（>8MiB 回执 POST 得到 413/清晰大小错误、≤5MiB body 逐字节等于 proto 载荷——SC-003）；(C) 迁移共享 helper 的全部调用方断言：`agent_v2_game_test.go`（含 :532-535 的 probe-echo 断言改为流存活/心跳语义）、`agent_v2_conversation_test.go`、`agent_v2_preset_test.go`；`agent_v2_game_disconnect_test.go` 注释中 "dial + probe" 措辞更新为 HTTP 首帧语义（断言零变化）；`projects/game/testplan/` 目录 gazelle（如有文件增删）

**Checkpoint（验证门禁）**: `bazel build //projects/game/fake-desktop/... //projects/game/testplan/...` + fake-desktop 单测绿；testplan 用例断言与 051 §6 验收锚点一一对应。

---

## Phase 6: User Story 4 — 旧框架移除与收口 (Priority: P2)

**Goal**: 删除全部 ws + bidi 框架代码与依赖；仓库终态 = 仅新框架（用户指令："保持干净，不要遗留旧框架内容"）。**注意**：T014 删除 `Connect` RPC 后、T015/T017 完成前，插件与 proxy 会短暂编译红（生成类型/接口面变化）——本 phase 以 T020 的全仓 build+test 绿为验证门禁，phase 内逐任务不要求独立全绿。

**Independent Test**: quickstart.md §4 no-residue 检索全零 + `bazel build //... && bazel test //...` 全绿。

**文档清单**：

- 代码规范文档：`style/golang.md` + Google Go Style（[Guide](https://google.github.io/styleguide/go/guide) / [Decisions](https://google.github.io/styleguide/go/decisions) / [Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html) + [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（style/javascript.md §Mock 约定引用的外部规范）
- 官方文档：无
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/research.md` §4（A/B/C 类迁移映射——B 类不迁移清单）+ D11；`specs/066-desktop-bridge-split/quickstart.md` §4（完成判定检索清单）；`specs/066-desktop-bridge-split/plan.md` Project Structure（删除面全景）；`survey/desktop-bridge-connect-split.md` §8（A/B/C 迁移分类原始裁定）；`projects/game/gateway/cmd/main.go`（WS 桥实现——删除范围）、`projects/game/proxy/handler/bridge.go`（bidi relay——删除范围）、`projects/game/pkg/bind/`（bidi 泵与首帧重放——删除范围）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（style/javascript.md §执行模型引用的 vitest shim 退出码契约）

**Tasks**:

- [ ] T014 [US4] 在 `projects/game/agent_v2.proto` 删除 `Connect` RPC 及其专属注释段，Service 注释更新为拆分后终态（Prefix Path 两条端点、无 WS/桥接描述）；在 `projects/game/game.proto` 将 `UserFrame`/`TeamFrame` 注释中 "exchanged over WebSocket and gRPC streams" 类 WS 措辞更新为 HTTP 流（chunked NDJSON）与 gRPC 的终态描述；`bazel build //projects/game/...` 再生成（TS handlers 面只剩 `Stream`/`SendFrame`，修正因此暴露的编译点）
- [ ] T015 [US4] 在 `common/js/dsh-plugins/desktop-bridge/src/`：`wire.ts` 删 `BidiStream` 与 `deriveSessionName`（`bridge.ts` 中同步删除其实现与引用）；`bridge.ts` 删 `handlers()` 的 `Connect` 分支与首帧身份绑定逻辑、删 `respondProbe` 及其调用；`index.ts` 公共面收口（`DesktopBridgeService` 接口只含 `Stream`/`SendFrame`/`dispatch`/`isDesktopConnected` 等终态方法，导出面同步）；`bridge.test.ts`/`index.test.ts` 删 Connect/probe/首帧身份用例
- [ ] T016 [US4] 在 `projects/game/gateway/cmd/main.go` 删除 WS 桥全部代码：`newRootMux`/`isWebSocketConnectPathV2`/`isWebSocketConnectPathIn`/`extractConnectIdentityV2`/`extractConnectIdentityIn`/`wsStream`/`handleDesktopBridgeConnect`/`pumpWebSocketConnect`/`streamOpener` 及孤儿 import（`bind`、`coder/websocket`、`proto` 等仅为其服务者）；`srv.Handler` 直接 `phttp.Handler(gwmux, "game-gateway")`；包级 doc 注释 Routes 段更新（无 WS 面）；在 `projects/game/gateway/cmd/main_test.go` 删除 WS 桥对应测试（路径匹配/身份提取/wsStream 编解码等）
- [ ] T017 [US4] 在 `projects/game/proxy/handler/bridge.go` 删除 `Connect` 方法、`binder bind.Binder` 字段与 `bind.WithFirstFrame` 引用（`streamBinder` 为唯一 binder），newBridgeClient 注释更新；在 `projects/game/proxy/cmd/main.go` 装配收口（单 `bind.NewServerStreamBinder[game.TeamFrame]()`）；`projects/game/proxy/handler/mocks_test.go` 清理 bidi/首帧 double
- [ ] T018 [US4] 删除 `projects/game/pkg/bind/binder.go`、`projects/game/pkg/bind/binder_test.go`、`projects/game/pkg/bind/first_frame.go`、`projects/game/pkg/bind/first_frame_test.go`；在 `projects/game/pkg/bind/server_stream.go` 将注释中对 `binder.go` 的历史引用改写为终态表述（io.EOF/Canceled 清洁关闭语义自述，不指向已删文件）；`bazel run //:gazelle projects/game/pkg/bind`
- [ ] T019 [US4] 删除 `projects/game/desktop/internal/api/websocket.go` 与 `projects/game/desktop/internal/api/websocket_test.go`（`WSClient`/`convertToWS`）；检查并清零 `projects/game/fake-desktop/` 与 `projects/game/testplan/` 残留的 websocket import；`bazel run //:gazelle projects/game/desktop/internal/api`
- [ ] T020 [US4] 依赖与残留收口：`bazel run //:go -- mod tidy -v`（`coder/websocket`/`gorilla/websocket` 无生产使用后从 go.mod 移除）→ 受影响目录 `bazel run //:gazelle` → `bazel mod tidy` → `bazel build //... && bazel test //...` 全绿；执行 `specs/066-desktop-bridge-split/quickstart.md` §4 检索清单确认零残留（`connect` WS 端点、`WSClient`、`bind.Binder`、`WithFirstFrame`、`wsStream`、`pumpWebSocketConnect`、`respondProbe`/`sendFlowProbe`、`deriveSessionName`、`disconnectDrainPause`、桥接链路 `coder/websocket`/`gorilla/websocket`）

**Checkpoint（验证门禁）**: no-residue 检索全零；全仓 build+test 绿。

---

## Phase 7: User Story 5 — 大型测试验收 (Priority: P2)

**Goal**: constitution 原则 VI 验收：实际执行 testplan（部署→测试→清理闭环），全部用例通过。

**Independent Test**: guitar run 全绿即验收（SC-001/SC-002/SC-003/SC-004/SC-006）。

**文档清单**：

- 代码规范文档：`style/large_test.md`（含 "FOR Agent: 使用 testplan SKILL" 指引）
- 官方文档：无
- 技术文章/技术参考文档：`specs/066-desktop-bridge-split/quickstart.md` §2（用例与预期表）/§3（手工冒烟）；`specs/066-desktop-bridge-split/spec.md` Success Criteria；`specs/066-desktop-bridge-split/research.md` D16（统计口径核验——仅计已执行操作）；`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §1.2（操作计数契约基线）

**Tasks**:

- [ ] T021 [US5] 通过 testplan SKILL 执行 `guitar run projects/game/testplan/system_test.yaml`（完整部署→测试→清理闭环）：desktop_flow 六用例（首帧 status/接管/派发回执识别/在途断连/空闲存活 SC-004/超限归因 SC-003）、game won 拓扑（多局 + 终局统计播报——统计口径回归）、game_disconnect drop 拓扑（三局序列）、conversation/stall/memory 等既有回归全部通过；任何失败/flaky 用例修复后重新执行直至全部通过（原则 VI——仅构建检查不构成验收）

**Checkpoint（验证门禁）**: guitar 全绿；spec SC-001..SC-006 逐条对照满足。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（proto）**: 无依赖——阻塞全部后续 phase（新 RPC 类型是所有消费方的编译前提）。
- **Phase 2/3（服务端 US1/US2）**: 依赖 Phase 1；两 phase 间建议按序（Phase 3 的 T008 扩展 `routeFrame`/`index.ts` 与 Phase 2 的 T003 同文件串行更稳）。
- **Phase 4（desktop 客户端）**: 依赖 Phase 2+3（服务端端点就绪）。
- **Phase 5（fake-desktop/testplan）**: 依赖 Phase 2+3；与 Phase 4 可并行（不同目录）。
- **Phase 6（移除）**: 依赖 Phase 4+5 全部完成（所有消费方已离开旧框架）。
- **Phase 7（大型验收）**: 依赖 Phase 6（终态代码）。

### User Story Dependencies

- **US1 (P1)**: Phase 1 → Phase 2（服务端）→ Phase 4（客户端读流）。
- **US2 (P1)**: Phase 1 → Phase 3（服务端）→ Phase 4（客户端上行）。
- **US3 (P2)**: 生命周期语义随 Phase 2 的 attach 交付；客户端断连/重连面在 Phase 4、注入验证在 Phase 5/7。
- **US4 (P2)**: Phase 6——必须在 US1/US2/US5 的代码切换完成后执行（编译原子性）。
- **US5 (P2)**: Phase 5（测试改写）+ Phase 7（验收执行）。

> **注意**：本 feature 为单链路协议重构，spec 的 US 之间不具备独立部署性（同一 proto/同一编译单元）——独立性体现为**可独立验证**（各 phase 的单测/检查点），而非可独立交付。

### Within Each User Story / Phase

- 先服务端后客户端；先实现后删旧（增量并存策略，见"通用约定"第 4 条）。
- 同文件任务串行（`server.ts` 在 Phase 2/3 各改一次；`bridge.ts` 在 Phase 2/3/6 各改一次）。

### Parallel Opportunities

- Phase 2: T002（proxy）/ T003（插件）/ T005（gateway）三任务不同文件可并行；T004 依赖 T003。
- Phase 4 与 Phase 5 整体可并行（desktop vs fake-desktop/testplan）。
- Phase 6: T015（插件）/T016（gateway）/T017（proxy）/T018（bind）/T019（desktop）不同文件可并行，T014（proto 再生成）先行、T020 收口最后。

---

## Parallel Example: Phase 2

```bash
# 三任务不同文件，可并行：
Task: "T002 [US1] proxy Stream 转发 in projects/game/proxy/handler/bridge.go"
Task: "T003 [US1] 插件 Stream 面（attach/首帧/心跳）in common/js/dsh-plugins/desktop-bridge/src/{wire,bridge}.ts"
Task: "T005 [US1] gateway 注册 in projects/game/gateway/cmd/main.go"
# T004（server.ts wiring）待 T003 类型面完成后串行。
```

---

## Implementation Strategy

### MVP First（US1 服务端闭环）

1. Phase 1：proto 契约落地。
2. Phase 2：`:stream` 服务端闭环 + 插件全语义单测。
3. **STOP and VALIDATE**：插件/proxy/agent_v2/gateway 单测绿——US1 服务端可独立验证。
   （端到端可演示需至 Phase 5 + Phase 7：fake-desktop 切换后经 testplan 验收。）

### Incremental Delivery（推荐执行序）

1. Phase 1 → Phase 2 → Phase 3（服务端两向闭环，单测绿）
2. Phase 4 ∥ Phase 5（真实 desktop 与 fake-desktop/testplan 切换）
3. Phase 6（旧框架全量移除 + no-residue 验证）
4. Phase 7（guitar 大型验收全绿——最终门禁）

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- 单测随实现任务交付（constitution 原则 IV——不单列 task）；大型测试执行单列 T021（原则 VI）
- 交付终态 = Phase 6 完成后（constitution 原则 VII）；"新旧并存"的编译原子性策略见「通用约定」第 4 条
- Commit after each task or logical group; 每个Checkpoint 处可中断/恢复（phase 间无隐式状态）
