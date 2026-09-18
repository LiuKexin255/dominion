# Research: DesktopBridgeService.Connect 拆分实现设计

> **上游输入**: [survey/desktop-bridge-connect-split.md](../../../survey/desktop-bridge-connect-split.md)（可行性调研 + 决策 D1–D7，已拍板）。本文承接其结论，落定**实现层**设计决策（D8 起），并对仓库代码逐点核验（所有锚点均已在当前代码树复核属实）。
> **用户补充指令**（2026-09-18）: 本需求为重构，原有基于 ws + bidi 框架的代码应当移除；框架基于新框架设计，保持干净，不遗留旧框架内容。

## 1. 代码核验摘要（对 survey 的增量确认）

| 核验点 | 结论 |
|---|---|
| proto 生成单元 | `agent_v2.proto` 与 `game.proto` 同属一个 Go 生成单元 `game_go_proto`（`projects/game/BUILD.bazel:6-19`，importpath `dominion/projects/game`）——给 `DesktopBridgeService` 加 HTTP 注解后，`RegisterDesktopBridgeServiceHandler` 网关注册函数**自动生成**，无需新 target；TS 面经 `agent_v2_proto` ts_proto_library → `projects/game/agent_v2/agent_v2_types/`（`server.ts:54` 消费生成的 `DesktopBridgeServiceHandlers`） |
| grpc-gateway v2.27.6 `ProtoMarshaller` | `runtime/marshal_proto.go`：`ContentType() = "application/octet-stream"`；`Marshal/Unmarshal` 仅接受 `proto.Message`。默认 marshaler 注册表只有通配 JSON（`runtime/marshaler_registry.go:98-104`），**必须显式** `runtime.WithMarshalerOption("application/octet-stream", &runtime.ProtoMarshaller{})` |
| 网关选项来源 | `runtime.NewServeMux(pgrpc.GatewayDefault()...)`（`common/gopkg/grpc/default.go:41`，当前仅 OTel tracing）——octet-stream 选项在 gateway 调用点 append，不动共享库 |
| 既有 server-streaming 转发范式 | proxy `AgentHandler.Send`：lookup owner → `agentV2Conn` → `newAgentClient(conn).Send(ctx, req)` → `binder.BindServerStream(stream, upstream)`（`projects/game/proxy/handler/agent.go:272-329`）；错误映射：status 承载拒绝保留原码，非 status 传输故障 → `UNAVAILABLE` |
| 既有 unary 转发范式 | proxy `AgentHandler.Cancel`：parse → `lookupAgentOwner`（lookup-only，无 owner → `NOT_FOUND`）→ 转发 → `propagateAgentError`（`agent.go:230-262`） |
| get-or-create 先例 | proxy `DesktopBridgeHandler.Connect` 用 `assignAgentOwner`（`projects/game/proxy/handler/bridge.go:81`）——desktop 可先于 team 物化连接，`Stream` 必须保持 |
| 插件连接注册表 | `DesktopBridge`：connections（session→stream face）+ pending（session→tool_id→waiter）；接管 = end 旧流 + 覆盖注册；晚到断开 compare-and-delete no-op；dispatch 失败路径齐备（`common/js/dsh-plugins/desktop-bridge/src/bridge.ts:141-421`）。拆分后**注册表/pending/dispatch 管线原样保留**，仅换写入面与入站来源 |
| desktop 探针 | `App.Connect`：WS dial → 发 ACTIVE status 探针 → 10s 等 echo → 存连接 + 起 readLoop，status 枚举名返给前端（`projects/game/desktop/app.go:921-1077`）；fake-desktop 同构（`projects/game/fake-desktop/service/session.go:87-167`）。**探针发送整体消失**，"连接确认 + 首帧超时"保留为首帧等待 |
| testplan WS 直连 | 共享 flow 客户端 helper 位于 `agent_v2_helpers_test.go`（`connectAgentV2Flow`/`sendFlowProbe`/`sendFlowReceipt*`/`readFlowTeamFrame*`/`serveTeamFlowScript` 等，被 `desktop_flow_test.go`、`agent_v2_game_test.go`、`agent_v2_conversation_test.go`、`agent_v2_preset_test.go` 复用）；`desktop_flow_test.go` 另自带 gorilla/websocket 客户端 dial `/connect`（probe echo / 接管 / 派发回执 / 在途失败四用例）——全部需整体重写为 HTTP 客户端 |
| 错误响应编码 | grpc-gateway v2.27.6 按请求头选 marshaler：Accept 仅对 mimeMap **精确键**命中（无通配回退；默认注册表只有 `*` 通配 JSONPb，本仓库另注册 `application/octet-stream`——故仅设 `Accept: application/json` 也无法命中），无 Accept 命中时 **outbound = inbound**（`runtime/marshaler_registry.go` `MarshalerForRequest`）。由此 `POST :sendFrame`（octet-stream、不设 Accept）的**全部错误响应**（网关解码失败——含 `MaxBytesReader` 超限、上游 gRPC 错误）由 `DefaultHTTPErrorHandler` 以 `ProtoMarshaller` 编码 `status.Convert(err).Proto()`：gRPC code 的 HTTP 映射状态码 + **二进制 `google.rpc.Status` 体**（`Content-Type: application/octet-stream`；`runtime/errors.go`，生成 handler 全部错误路径均传 outboundMarshaler）。`GET :stream`（无 body/Content-Type）inbound/outbound 均落通配 JSONPb——预流错误为 JSON 状态信封、流中异常终止为 NDJSON `{"error":...}` 尾行。D9.4 的 413 预检中间件在 gwmux 之前经 `http.Error` 返回 `text/plain`（不经 marshaler） |
| 上行顺序依赖 | 逐帧核验：desktop 侧每个操作独立执行、回执按 `flow_result.tool_id` 关联（fake-desktop executor 逐 part 独立出回执；插件 `handleResult` 按 tool_id 查 waiter）；wait/warn 为旁路日志信号；**无任何跨帧顺序依赖**——独立 unary 上行安全（survey §5.3 遗留检查项就此关闭） |
| env header 透传 | desktop 既有 REST 客户端已带 env header 经 gwmux→proxy→agent_v2 工作（`projects/game/desktop/internal/api/client.go:44-49`；grpc-gateway 默认 header matcher 放行非保留头）——`Stream`/`SendFrame` 走同一 gwmux 路径，透传由构造保证，无需新机制 |
| agent_v2 grpc-js server 上限 | grpc-js server 默认收包 4MiB、发包不限（`@grpc/grpc-js` `build/src/constants.js`）——Go 段 8MB 配置不传导到 TS 段，须在 `server.ts` server 构造显式配置（D17）；上行链路五段（gateway HTTP 守卫 / gateway 客户端 / proxy server / agentclient / agent_v2 server）自此全部 8MiB |

## 2. 实现决策（D8 起）

### D8 — proto 契约形状

**Decision**:

```proto
service DesktopBridgeService {
  rpc Stream(StreamRequest) returns (stream projects.game.TeamFrame) {
    option (google.api.http) = {
      get: "/api/v2/{session=templates/*/sessions/*}:stream"
    };
  }
  rpc SendFrame(SendFrameRequest) returns (SendFrameResponse) {
    option (google.api.http) = {
      post: "/api/v2/{session=templates/*/sessions/*}:sendFrame"
      body: "*"
    };
  }
}

// SendFrameResponse is intentionally empty: receipt correlation rides on
// flow_result.tool_id; the synchronous response carries no information.
message SendFrameResponse {}

message StreamRequest {
  // Session resource name: templates/{template}/sessions/{session}.
  string session = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Session"
    }
  ];
}

message SendFrameRequest {
  // Session resource name; overwritten by the URL path (grpc-gateway
  // assigns path parameters after body decoding).
  string session = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {
      type: "game.liukexin.com/Session"
    }
  ];
  // The desktop frame to deliver; body payload under octet-stream.
  projects.game.UserFrame frame = 2 [(google.api.field_behavior) = REQUIRED];
}
```

`Connect` RPC 及其注释整体删除（干净切换，用户指令）。

**Rationale**: 与 `AgentService.Send`（`post .../sessions/*}:send` body `*`）同族；`body: "*"` + octet-stream 下，desktop `proto.Marshal(SendFrameRequest{frame})` 即请求体，path 参数在 body 解码后覆盖 `session`（生成代码语义，survey §3.1.2 已源码验证）——身份绑定不可被 body 值左右。`StreamRequest` 仅承载 path 身份。响应用与 RPC 同名的空 `SendFrameResponse`（AIP-136 "response message matching the RPC name, with a Response suffix"；与 `Cancel(CancelRequest) returns (CancelResponse)` 命名族一致）——回执关联靠 `tool_id`，同步响应无信息量。两个 `session` 字段的 `(google.api.resource_reference)`（type `game.liukexin.com/Session`，注解形态见 AIP-124 https://google.aip.dev/124 ）对齐同文件全部请求消息资源字段的既有惯例（直系先例 `SendRequest.session`——同资源类型、同 path 模式）；Session 类型声明于 `game.proto`、与 `agent_v2.proto` 同属 `game_go_proto` 生成单元，引用可解析；仅作用于文档/lint 工具，无 wire 与运行时影响。

**Alternatives**: (a) `body: "frame"` 嵌套映射——octet-stream 下无收益且生成路径更复杂，弃；(b) SendFrame 直接以 `UserFrame` 为请求——path 参数无处落（`session` 字段与 `UserFrame.session_id` 不同名不同义），弃；(c) 保留 Connect 并标记 deprecated——违背"干净重构、不遗留旧框架"指令，弃。

### D9 — 网关装配

**Decision**:
1. `gwmux` 选项：`runtime.NewServeMux(append(pgrpc.GatewayDefault(), runtime.WithMarshalerOption("application/octet-stream", &runtime.ProtoMarshaller{}))...)`。
2. 注册：`game.RegisterDesktopBridgeServiceHandler(ctx, gwmux, teamConn)`（与 AgentService 同连接——owner 亲和经 proxy；长流 keepalive 与 8MB 已配对）。
3. 删除：`newRootMux`、`isWebSocketConnectPath*`、`extractConnectIdentity*`、`wsStream`、`handleDesktopBridgeConnect`、`pumpWebSocketConnect`、`streamOpener` 及 `bind`/`websocket`/`proto` 等仅为其服务的 import；`phttp.Handler(gwmux, "game-gateway")` 直接作为 handler（WS 分支消失后 rootMux 无存在必要）。
4. body 上限中间件：包一层仅命中 sendFrame POST 路径的 `http.MaxBytesReader(w, r.Body, 8MiB)`；`Content-Length` 预检超限直接 413。8MiB 与 gRPC hop 上限对齐（5MiB 截图 + 信封余量充足）。

**Rationale**: "注册一行、零自研"是本重构核心收益；8MiB guard 使 HTTP 腿超限获得清晰归因（413 或 gateway 解码失败时 message 含 "request body too large"），gRPC hop 8MB 退化为传输兜底而非首报错点（spec 025 §4 守卫意图在 HTTP 腿的落点）。gRPC hop 全段 8MiB 的完整链条（含 agent_v2 TS 段）见 D17。

**Alternatives**: guard 放 gRPC hop 独扛——错误表现为 `ResourceExhausted` 且归因层级深，弃；全站 MaxBytesReader——会误伤其他端点，弃。

### D10 — proxy 转发

**Decision**:
1. `Stream(req, stream)`：`parseAgentSession` → `assignAgentOwner`（**get-or-create**，保持"desktop 先于 team 物化连接"语义）→ `agentV2Conn` → `newBridgeClient(conn).Stream(ctx, req)` → `h.binder.BindServerStream(stream, upstream)`（`bind.ServerStreamBinder[game.TeamFrame]`）。错误映射与今日 Connect 一致：status 承载拒绝保留原码、非 status → `UNAVAILABLE`。
2. `SendFrame(ctx, req)`：`parseAgentSession` → `lookupAgentOwner`（**lookup-only**，与 Send/Cancel 同族：回执永不分配 owner；无 owner → `NOT_FOUND`）→ 转发 unary → `propagateAgentError`。
3. `DesktopBridgeHandler` 的 binder 字段从 `bind.Binder` 换为 `bind.ServerStreamBinder[game.TeamFrame]`；`bind.WithFirstFrame` 引用删除。

**Rationale**: 两个转发范式均为仓库现成生产代码（Send/Cancel），零新机制；首帧身份解析机制消失（身份在 path 参数中，gRPC 调用即绑定）。

**Alternatives**: SendFrame 也 get-or-create——一个回执触发 owner 分配是错误的副作用方向（且该 session 必然因 Stream/对话已存在 owner），弃。

### D11 — bind 包收缩

**Decision**: 删除 `binder.go` + `binder_test.go`、`first_frame.go` + `first_frame_test.go`；保留 `server_stream.go` + `server_stream_test.go`（`AgentService.Send` 与新 `Stream` 共用）。已 grep 验证 `bind.Binder`/`bind.WithFirstFrame` 生产使用点仅 gateway 与 proxy bridge 两处，均随本重构消失。

**Rationale**: 死代码即旧框架残留，违反用户指令与原则 VII。

### D12 — agent_v2 插件接口与心跳

**Decision**:
1. `wire.ts`：`BidiStream` 替换为两个结构面——下行 `StreamCall`（grpc-js `ServerWritableStream` 的结构超集：`request: { session?: string }`、`write`/`end`、`on("error"|"close"|"cancelled")`）与上行 `SendFrameCall`（`request: { session?: string; frame?: BridgeUserFrame }` + callback）。`DesktopBridgeServiceHandlers` 改为 `Stream(call)` / `SendFrame(call, callback)` 两方法。删除 `deriveSessionName` 导出（首帧身份解析机制消失）。
2. `bridge.ts`：
   - `Stream(call)`：`sessionName = call.request.session`（path 注入）；空值 → warn + `call.end()`——零帧 OK 结束，调用方立即得 EOF（grpc-js 的 server-streaming handler 正常返回不发送任何 status，须显式 `end()` 收口，否则 RPC 悬空至客户端超时取消；该分支生产不可达——gateway path 注入非空、proxy `parseAgentSession` 前置拦截——故 OK 收口即可，`end()` 已在 `StreamCall` 结构面内，零扩面）；`attach(sessionName, call)` = 接管（end 旧流 + 覆盖注册）→ 注册 → **立即写一帧 status（`STATUS_SIGNAL_STATUS_ACTIVE`）TeamFrame** → 启动心跳 `setInterval`（10s，写同型 status 帧）→ 挂 disconnect 事件。
   - Connection 对象 = stream face + heartbeat timer（接管/断开时 clearInterval）。
   - `SendFrame(call, callback)`：`sessionName` 取 `call.request.session`；`connections` 无该会话 → `callback({ code: FAILED_PRECONDITION, message: "desktop stream not active" })`；有则 `routeFrame`（flowResult → `handleResult`；wait/warn → 既有日志；其余 part 记 warn 忽略）后 `callback(null, {})`。
   - 删除 `respondProbe`（探针 echo 机制消失；status 帧改由服务端主动推：开流首帧 + 周期心跳，值固定 ACTIVE——desktop 前端继续展示同枚举名，行为面不变）。
   - `dispatch`/`handleResult`/`disconnect`（compare-and-delete + 在途 settle "desktop disconnected"）/ pending 结构**原样保留**。
3. `server.ts` `buildDesktopBridgeHandlers`：`Stream: (call) => bridge.Stream(call)`、`SendFrame: (call, cb) => bridge.SendFrame(call, cb)`（生成类型随 `agent_v2_proto` 重新生成）。

**Rationale**: 注册表/pending/故障路径是与传输无关的游戏层语义（survey A 类），原样保留；心跳值 ACTIVE 保持前端展示语义（旧 echo 恒返回 desktop 自报的 ACTIVE）。10s 心跳覆盖 ingress 空闲超时（D5）并充当 liveness。

**Alternatives**: 心跳仅空闲时发（距上次写 <10s 则跳过）——省极少带宽引入状态跟踪，弃；SendFrame 对无流静默缓存重放——引入隐藏队列语义，违背 D4 拒绝语义，弃。

### D13 — desktop 桥客户端

**Decision**: 删除 `internal/api/websocket.go`（WSClient），新增 `internal/api/bridgeclient.go`：
1. `Connect(ctx, gatewayURL, template, sessionID, env)`：`GET {gw}/api/v2/templates/{t}/sessions/{s}:stream`（env header；不设 Accept——无请求 Content-Type 的 GET 使 inbound/outbound 均落通配 JSONPb，见 §1）。非 2xx → 读 JSON 状态信封取 message 返回错误；响应 200 后记录 body reader；**首帧行**（NDJSON `{"result": <TeamFrame>}`）在 10s 内读取并解析——超时/失败即关闭并报错（首帧超时判定，替代旧探针）。
2. `RecvFrame(ctx)`：`bufio.Reader.ReadBytes('\n')` 逐行（不用 Scanner——规避 token 上限；下行无截图但 MessageParts 理论可大）→ `protojson.UnmarshalOptions{DiscardUnknown: true}` 解 `{"result"}` 信封内 TeamFrame（对齐 `style/golang.md` 同仓 grpc proto 反序列化约定）；行含 `{"error": ...}` → 返回错误（流异常终止尾块）。
3. `SendFrame(ctx, frame)`：`POST .../sessions/{s}:sendFrame`，`Content-Type: application/octet-stream`，body = `proto.Marshal(&SendFrameRequest{Frame: frame})`；**不设 Accept**——outbound 随 inbound 落 `ProtoMarshaller`（§1 错误响应编码行），错误体为二进制 `google.rpc.Status`。非 2xx → 按响应 `Content-Type` 的 media type（`mime.ParseMediaType` 去参数）分派：`application/octet-stream` → `proto.Unmarshal` 为 `google.golang.org/genproto/googleapis/rpc/status` 的 `Status`、经 `status.FromProto` 取 gRPC code 与 message（grpc-go `status` 包同源类型，无新增依赖）；其余（413 预检的 `text/plain` 等）→ body 原文作 message。返回的错误统一携带 HTTP 状态码、gRPC code（可解出时）与 message。
4. `Close()`：取消请求 ctx（同时解除阻塞的行读取）。
5. `app.go`：`Connect` 流程 = 既有 reconnect handover（关旧连接 + 等 recvDone）→ `bridgeclient.Connect` → 等首帧取 status 枚举名返前端 → 存客户端 + 起 readLoop（循环体不变）。探针发送代码删除。`sendOperationResult` → `bridge.SendFrame` 包一层**有界重试**（3 次、间隔 1s，覆盖 D4 接管间隙：新 Connect 在途时旧流已断、SendFrame 暂时 FAILED_PRECONDITION；重试判定只认错误携带的 gRPC code（FAILED_PRECONDITION）——HTTP 400 与 INVALID_ARGUMENT 共码，不可凭 HTTP 状态判定；最终失败仅日志，与今日写失败行为对齐）。

**Rationale**: HTTP 客户端无 close handshake/读锁死锁类别问题（survey B 类 CloseNow/锁外读两个 workaround 连根消失）；`ctx cancel` 即解除阻塞读。有界重试是对 D4 拒绝语义的配套（用户拍板"接管间隙由 desktop 重试"）。

**Alternatives**: 无重试——接管间隙且回执必然落 20min backstop，违背 D4 拍板，弃；无限重试——掩盖永久失联，弃。

### D14 — fake-desktop

**Decision**: `service/session.go` 重写连接层（executor/scenarios/board 不动）：
1. `serveOnce`：`GET :stream`（首帧 10s 超时判定）→ 循环逐行读 TeamFrame → 逐 part `executor.Execute` → 回执 `POST :sendFrame`（octet-stream）。
2. 重连循环（`reconnectDelay` 2s）与故障注入点（`Disconnected()` 判定）保持；**`disconnectDrainPause` 删除**——unary POST 完成即回执已抵达 agent_v2（unary 响应在 `handleResult` 之后返回），WS 时代"回执在途即断连"的竞态类别消失（survey B 类）。
3. `session_test.go` 的 WS server 双打改为 HTTP 双打（`httptest` 流式响应 + 记录 POST）。

**Rationale**: 测试设施与新协议同构；断连注入语义（第 N 回执后断流）不变，disconnect 大型测试断言不动。

### D15 — testplan 重写面

**Decision**:
1. `agent_v2_helpers_test.go`：共享 flow 客户端 helper（`connectAgentV2Flow`/`sendFlowUserFrame`/`dialAgentV2FlowProbed`/`sendFlowProbe`/`sendFlowReceipt*`/`replyFlowReceipt`/`readFlowTeamFrame*`/`serveTeamFlowScript`）→ HTTP helper（`openDesktopStream` 返回行读取器 + cancel；`postUserFrame` 发送回执），并迁移全部调用方（`desktop_flow_test.go`、`agent_v2_game_test.go`、`agent_v2_conversation_test.go`、`agent_v2_preset_test.go`，含 `agent_v2_game_test.go` 的 probe-echo 断言 → 流存活/心跳语义）。`desktop_flow_test.go` 用例语义迁移：probe-echo 用例 → **首帧 status 用例**（开流后 10s 内收到 ACTIVE status 帧，无上行探针）；接管用例（第二流打开后旧流结束）；派发→回执→识别链路用例；在途断连失败用例（关流后 dispatch 收 "desktop disconnected"）；新增空闲存活（SC-004）与超限归因（SC-003）用例。
2. `agent_v2_game_disconnect_test.go` 等经 fake-desktop 的用例：**业务断言零变化**（仅注释中 "dial + probe" 措辞更新为 HTTP 首帧语义）；直接使用共享 flow helper 的断言按第 1 条迁移。
3. 拓扑与 deploy.yaml 零改动（纯路径前缀路由）。

**Rationale**: 大型测试是 D6 直接切换的兜底验收（原则 VI：实际执行、全部通过）。

### D16 — 扫雷统计口径（输入 1）

**Decision**: 无代码变更。核验结论：`operationCount`/`operationsByType` 仅在"派发成功且识别成功"后递增（`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:385-388`）；SKIP/STOP/派发失败/init/remain 不计（`runtime.test.ts:594-630`；[specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md](../065-agent-v2-team-refine/contracts/game-stats-broadcast.md) §1.2 已成文）。唯一边缘（派发成功但识别失败）使该局状态失效、不可能产生终局统计，不影响口径。既有单测作为本重构的回归面保留。

**Rationale**: 用户指令"如果已经是了则忽略"。

### D17 — agent_v2 gRPC server 消息上限对齐 8MiB

**Decision**: `projects/game/agent_v2/src/server.ts` `buildServer` 的 server 构造显式传入 `"grpc.max_receive_message_length": 8 * 1024 * 1024` 与 `"grpc.max_send_message_length": 8 * 1024 * 1024`（server 级，作用于该进程全部服务面）。上行链路五段上限自此全部 8MiB：gateway HTTP 预检/MaxBytesReader（D9.4）→ gateway→proxy gRPC 客户端（`projects/game/gateway/cmd/main.go` clientOpts）→ proxy server（`projects/game/proxy/cmd/main.go` MaxRecv/MaxSendMsgSize）→ proxy→agent_v2 agentclient（`projects/game/proxy/runtime/agentclient/manager.go` MaxCallRecv/MaxCallSendMsgSize）→ agent_v2 grpc-js server（本决策）。

**Rationale**: grpc-js server 默认收包上限 4MiB、发包不限（`@grpc/grpc-js` 1.14.4 `build/src/constants.js` `DEFAULT_MAX_RECEIVE_MESSAGE_LENGTH`/`DEFAULT_MAX_SEND_MESSAGE_LENGTH`）——Go 段的 8MB 配置不传导到 TS 段。默认值下 ≤5MiB 的 SendFrame 回执在 agent_v2 入口即 `RESOURCE_EXHAUSTED`（HTTP 429 映射），FR-007「≤5MiB 必达、超限在 HTTP 边缘归因」不成立（SC-003 的 ≤5MiB 腿钉在该处）。收包 8MiB 与全段配对后，gateway 预检成为唯一首报错点、gRPC 各段退化为传输兜底（D9 rationale 的完整落地）。发包显式 8MiB 与 proxy agentclient 对端的 `MaxCallRecvMsgSize(8MB)` 成对——有效上限本就由对端存在，在发送端声明使潜在超帧在最早点归因，并保持仓库「每段 recv/send 成对配置」惯例（gateway/proxy/agentclient 三段均成对）。同进程其余服务面（对话/预设）载荷远小于 8MiB，server 级上提无副作用。

**Alternatives**: 收包设 -1（无限）——拆除传输兜底、超限涌向 gRPC 解码层，违背 D9 分层归因，弃；仅设收包不设发包——发包上限隐式依赖对端配置、不满足成对惯例，弃。

## 3. 风险与缓解

| 风险 | 缓解 |
|---|---|
| NDJSON 分块边界：chunked 传输不保证 1 chunk = 1 消息 | 客户端按**行**（`\n` delimiter，`ForwardResponseStream` 每消息追加）缓冲解析，不按 chunk（D13.2） |
| 首消息前无响应头（中间层挂起） | 开流即推 status 首帧（D12），Header 立即落盘 |
| 上行无序 | 帧自包含 + tool_id 关联已逐帧核验（§1）；无顺序依赖 |
| sendFrame 错误体非单一 JSON（gwmux 错误路径为二进制 `google.rpc.Status`、413 预检为纯文本），客户端误解析 | 客户端按响应 `Content-Type` 分派解析（D13.3）；重试判定只认解出的 gRPC code（D13.5） |
| takeover 间隙回执被拒 | FAILED_PRECONDITION + desktop 有界重试（D4/D13.5） |
| desktop 旧版二进制对旧端点的访问（同仓同发布） | 不存在外部旧客户端；testplan 兜底（D6） |
| 各 gRPC 段消息上限错配（TS 段默认收包 4MiB < Go 段 8MB——≤5MiB 上行在 agent_v2 入口 429 RESOURCE_EXHAUSTED） | 全段显式 8MiB（D17 + D9.4）；SC-003 端到端用例（≤5MiB 必达 + >8MiB 归因）钉死 |

## 4. 迁移约束承接（survey §8 的落地映射）

- **A 类保留**：octet-stream 延续 spec 025 §2 二进制直传；HTTP 显式上限（D9.4）承接 §3/§4 守卫意图；接管/no-op 清理、dispatch 故障路径、探测意图（开流首帧 + 首帧超时）、两流独立——全部落入 D12/D13。
- **B 类消失**：URL 身份逐帧注入、CloseNow、锁外读、WS 关闭码分类、首帧身份绑定 + WithFirstFrame、7 段路径手工匹配、OriginPatterns/307 细节、WS 32KiB 读限、drain pause——随代码删除，无对应迁移物。
- **C 类原样**：keepalive 配对、env header 透传（经 gwmux 同路径构造性保持）、gRPC hop 8MB（Go 三段为既有配置原样；agent_v2 grpc-js server 段按 D17 显式配齐——grpc-js 默认收包 4MiB，「既有 8MB」对 TS 段不成立）。
