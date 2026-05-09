# deploy、game gateway、game session tracing 与日志补全方案

## 目标

本方案用于为以下三个服务补充 tracing 上报与结构化日志：

* `projects/infra/deploy/`
* `projects/game/gateway/`
* `projects/game/session/`

目标是：

* 让三个服务的 HTTP/gRPC/WebSocket 入口都能生成可上报的 OpenTelemetry trace。
* 让一次用户请求、一次 WebSocket 会话、一次 deploy reconcile、一次 session 分配或 gateway control 操作，能通过 trace id 关联到关键业务日志。
* 让日志集中在顶层入口、异步 worker、外部系统边界和状态变化处，避免低价值重复日志散落在底层 helper 中。
* 让 deploy、gateway、session 复用仓库已有 `common/gopkg/otel`、`common/gopkg/logs`、`common/gopkg/grpc` 公共能力，不在业务服务内重复建设可观测底座。

本方案只覆盖 tracing 与 log。metrics 不在本轮范围内，后续如需要按独立方案设计。

## 参考方案

公共可观测底座沿用以下方案和实现：

* [Golang OpenTelemetry 公共库方案](./golang_observability_common_library.md)
* [Golang common/gopkg 日志补全方案](./golang_common_gopkg_logging_completion.md)
* [Golang 服务 Bootstrap 框架方案](./golang_service_bootstrap_framework.md)

现有公共能力包括：

* `common/gopkg/otel`：初始化 OpenTelemetry provider、OTLP exporter、trace id 工具。
* `common/gopkg/bootstrap.OTel()`：将 OTel 初始化纳入服务生命周期。
* `common/gopkg/logs`：结构化日志门面，deploy 环境桥接 OTel Logs，非 deploy 环境输出到控制台。
* `common/gopkg/grpc.ServiceDefault()`：gRPC server 默认接入 OTel instrumentation。
* `common/gopkg/grpc.ClientDefault()`：gRPC client 默认接入 OTel instrumentation。
* `common/gopkg/grpc.GatewayDefault()`：grpc-gateway 默认补充 HTTP route 与 RPC method trace 属性。

## 当前问题

### 通用问题

三个服务目前都有以下缺口：

* app 入口没有注册 `bootstrap.OTel()`，即使运行环境具备 Collector，也不会启动 trace/log provider。
* HTTP server handler 未统一包裹 `otelhttp.NewHandler`，HTTP 入口 span 不完整。
* grpc-gateway 入口多处直接使用 `runtime.NewServeMux()`，未复用 `grpc.GatewayDefault()` 补充低基数 route 信息。
* 业务层几乎没有结构化日志，失败排查主要依赖错误返回，缺少运行态上下文。
* 异步 worker、WebSocket、deploy reconcile 这类跨 goroutine/长连接链路缺少业务 span。

### deploy 服务问题

`projects/infra/deploy/` 的核心职责是维护 `Environment` 权威状态，并异步 reconcile 到 Kubernetes。当前：

* handler 能返回明确错误，但没有记录 create/update/delete/enqueue 等控制面操作。
* `domain.Worker` 执行 apply/delete/retry/max retry 时没有日志或业务 span。
* `runtime/k8s` 执行 Kubernetes apply/delete 时，失败错误链较完整，但缺少资源数量、资源类型、namespace 等可观测上下文。

### gateway 服务问题

`projects/game/gateway/` 同时提供 grpc-gateway HTTP 接口与 WebSocket 实时通道。当前：

* WebSocket connect、hello、disconnect、message routing 没有 trace/log。
* control request、ack、result、timeout、agent disconnect 只通过内存状态推进，排查时难以关联 requester、operation 与 session。
* `routingWorker` 与 `CompletionWorker` 是异步链路，缺少每条 completion/routed message 的边界观测。

### session 服务问题

`projects/game/session/` 负责 session 生命周期与 gateway 分配。当前：

* create/get/list/delete/reconnect 缺少业务日志。
* session 分配 gateway、查询 deploy stateful endpoint、生成 connect URL 等关键边界缺少 span/log。
* token 与 connect URL 涉及敏感信息，需要明确日志脱敏边界。

## 模型设计

### 入口 trace 模型

每个服务进程启动时注册 `bootstrap.OTel()`，由 `common/gopkg/otel` 根据运行环境决定是否远程上报。

HTTP 入口统一使用：

```go
httpServer := &http.Server{
	Addr:    listenAddr,
	Handler: otelhttp.NewHandler(handler, "<service>-http"),
}
```

grpc-gateway mux 统一使用：

```go
httpMux := runtime.NewServeMux(grpc.GatewayDefault()...)
```

这样入口 span 由 `otelhttp` 创建，grpc-gateway 在路由匹配后补充：

* `http.route`
* `rpc.method_full`
* 低基数 span name，例如 `GET /v1/sessions/{id}`

### 业务 span 模型

入口 span 只表达协议层请求。跨外部系统、异步 worker、长连接消息处理需要业务 span 补充语义。

业务 span 命名采用稳定、低基数字符串：

* `deploy.worker.process`
* `deploy.worker.apply_present`
* `deploy.worker.delete_absent`
* `gateway.ws.connect`
* `gateway.ws.hello`
* `gateway.ws.control_request`
* `gateway.control.completion`
* `session.create`
* `session.reconnect`
* `session.gateway.pick_random`

span attribute 只记录低基数或必要定位字段，避免记录大 payload、token、完整 connect URL、媒体二进制内容。

### 日志模型

业务日志统一使用 `common/gopkg/logs`：

```go
logs.InfoContext(ctx, "session created",
	"session_id", sessionID,
	"gateway_id", gatewayID,
)
```

日志级别约定：

* `Info`：正常生命周期关键事件、状态变化、用户操作被接受、异步任务完成。
* `Warn`：可恢复异常、重试、超时、客户端断开、无可用 gateway 等业务可预期异常。
* `Error`：当前操作失败、外部系统调用失败、worker fatal、状态保存失败。
* `Debug`：高频细节。第一阶段默认不依赖 debug 输出定位问题。

日志字段名按约定统一，不做强制规范，也不抽象字段常量。三个服务内优先使用以下字段名：

| 字段 | 含义 |
| --- | --- |
| `operation` | 当前业务操作名 |
| `env_name` | deploy environment 名称 |
| `generation` | deploy environment generation |
| `desired_state` | deploy desired state |
| `retry_count` | deploy worker retry 次数 |
| `session_id` | game session ID |
| `gateway_id` | gateway 实例 ID |
| `conn_id` | WebSocket 连接 ID |
| `client_role` | WebSocket client role |
| `operation_id` | control operation/request ID |
| `operation_kind` | control operation kind |
| `message_kind` | gateway message payload 类型 |
| `target_conn_id` | routed message 目标连接 ID |
| `resource_kind` | Kubernetes 资源类型 |
| `resource_name` | Kubernetes 资源名 |
| `namespace` | Kubernetes namespace |
| `error` | error 对象 |

### 高频数据模型

gateway 的 media segment 是高频数据，不能逐条记录 payload，也不默认逐条创建业务 span。

规则：

* WebSocket connect、hello、disconnect、control request、control completion 创建 span 并记录日志。
* media init 可记录一次关键日志，因为它代表播放初始化状态变化。
* media segment 默认只在异常时记录，正常转发不逐条 trace/log。
* catch-up 可记录消息数量，不记录 segment bytes。

### 敏感数据模型

以下内容禁止进入日志和 span attribute：

* session token 原文。
* 完整 connect URL。
* WebSocket 消息 payload 原文。
* media segment 二进制内容。
* Kubernetes Secret 内容。

允许记录：

* `session_id`
* `gateway_id`
* public host，不包含 token query。
* message/control kind。
* resource kind/name/namespace。

## 代码分层

### app/cmd 层

负责进程级装配：

* 注册 `bootstrap.OTel()`。
* 使用 `grpc.GatewayDefault()` 创建 grpc-gateway mux。
* 使用 `otelhttp.NewHandler()` 包裹最终 HTTP handler。
* 保留 `log.Fatalf` 作为启动失败或进程退出兜底；运行期业务日志不使用标准库 `log`。

涉及文件：

* `projects/infra/deploy/app/cmd/main.go`
* `projects/game/gateway/app/cmd/main.go`
* `projects/game/session/app/cmd/main.go`

### handler 层

负责协议入口的业务语义日志，不重复做协议 access log。

原则：

* 记录请求被接受、关键参数、返回业务结果摘要。
* 错误只在 handler 顶层或逻辑入口兜底记录，避免同一错误在 service/repository 多层重复打印。
* 不记录 token、payload 原文、connect URL。

涉及文件：

* `projects/infra/deploy/handler.go`
* `projects/game/gateway/handler.go`
* `projects/game/session/handler.go`

### service/domain worker 层

负责长流程、异步流程和状态变化的 span/log。

涉及文件：

* `projects/infra/deploy/domain/worker.go`
* `projects/infra/deploy/app/worker.go`
* `projects/game/gateway/ws.go`
* `projects/game/gateway/worker.go`
* `projects/game/gateway/service/gateway.go`
* `projects/game/gateway/service/control.go`
* `projects/game/gateway/service/worker.go`
* `projects/game/session/service/service.go`

### runtime/storage/external boundary 层

负责外部系统调用边界的 span/log。

涉及文件：

* `projects/infra/deploy/runtime/k8s/executor.go`
* `projects/infra/deploy/runtime/k8s/*query*.go`
* `projects/game/session/runtime/gateway/deploy.go`
* `projects/game/session/runtime/storage/mongo.go`

## 关键改造点

### deploy

#### app 入口

* 注册 `bootstrap.OTel()`。
* `runtime.NewServeMux()` 改为 `runtime.NewServeMux(grpc.GatewayDefault()...)`。
* HTTP handler 外层包 `otelhttp.NewHandler(httpMux, "deploy-http")`。

#### handler

建议在以下操作记录顶层日志：

* `CreateEnvironment`：记录 `env_name`、`generation`、desired state、enqueue 成功或失败。
* `UpdateEnvironment`：记录 `env_name`、`generation`、field mask 摘要、enqueue 结果。
* `DeleteEnvironment`：记录 `env_name`、desired absent、enqueue 结果。
* `GetServiceEndpoints`：记录 `env_name`、app、service、view、endpoint 数量；失败时记录 reason。

#### worker

建议增加业务 span：

* `deploy.worker.process`
* `deploy.worker.apply_present`
* `deploy.worker.delete_absent`

建议增加日志：

* dequeue 开始：`env_name`、`retry_count`、source。
* apply/delete 成功：`env_name`、`generation`。
* apply/delete 失败并重试：`env_name`、`retry_count`、error。
* max retry 放弃：`env_name`、`retry_count`、error。
* fatal：`env_name`、error。

#### k8s runtime

成功路径不逐资源打日志，只记录 reconcile 资源计数，例如 Deployment、Service、HTTPRoute、StatefulSet、PVC、Secret 数量。

失败路径记录失败资源明细：

* `resource_kind`
* `resource_name`
* `namespace`
* `error`

### gateway

#### app 入口

* 注册 `bootstrap.OTel()`。
* grpc-gateway mux 使用 `grpc.GatewayDefault()`。
* `app.Router` 外层包 `otelhttp.NewHandler(router, "gateway-http")`。

#### WebSocket

`projects/game/gateway/ws.go` 建议记录：

* connect accepted/rejected：`session_id`、`conn_id`、remote addr 摘要、error。
* hello success/timeout/invalid：`session_id`、`conn_id`、`client_role`。
* disconnect：`session_id`、`conn_id`、`client_role`、reason。
* route failure：`session_id`、`target_conn_id`、error。

建议创建 span：

* `gateway.ws.connect`
* `gateway.ws.hello`
* `gateway.ws.web_message`：仅 control 类消息。
* `gateway.ws.agent_message`：仅 ack/result/media init 或异常。

media segment 正常转发不逐条 trace/log。

#### control

`projects/game/gateway/service/control.go` 建议记录：

* submit success/failure：`session_id`、`operation_id`、`operation_kind`、`requester_conn_id`。
* ack/result：`session_id`、`operation_id`。
* timeout：`session_id`、`operation_id`、duration。
* agent disconnect completion：`session_id`、`operation_id`。

`projects/game/gateway/service/worker.go` 与 `projects/game/gateway/worker.go` 对 completion/routed message 建立异步边界 span，并记录 message kind、target conn、session。

### session

#### app 入口

* 注册 `bootstrap.OTel()`。
* grpc-gateway mux 使用 `grpc.GatewayDefault()`。
* HTTP handler 外层包 `otelhttp.NewHandler(httpMux, "session-http")`。

#### handler/service

建议创建 span：

* `session.create`
* `session.get`
* `session.list`
* `session.delete`
* `session.reconnect`
* `session.enrich_connect_url`

建议记录日志：

* create success/failure：`session_id`、`session_type`、`gateway_id`。
* reconnect success/failure：`session_id`、old/new `gateway_id`、`reconnect_generation`。
* delete success/failure：`session_id`。
* no gateway available：`session_id`、operation。

#### gateway registry

`projects/game/session/runtime/gateway/deploy.go` 建议创建 span：

* `session.gateway.pick_random`
* `session.gateway.pick_random_excluding`
* `session.gateway.public_host`

记录 resolver 返回实例数、ready 实例数、排除 gateway、最终 gateway。不要记录 token 或完整 connect URL。

## 决策详情

### 决策 1：本轮只做 tracing 与 log

仓库规范要求服务代码使用 OTel 上报 tracing、log、metrics。本次任务目标是 tracing 上报和 log，因此本方案不设计业务 metrics。

原因：

* tracing 与日志已经能覆盖本轮排障目标。
* metrics 需要单独定义指标名、label、采样语义和告警目标，不适合混入本次改造。

### 决策 2：日志字段名按约定统一，不做强制规范

三个服务内使用一致字段名，但不新增公共常量或强制校验层。

原因：

* 当前目标是补齐可观测缺口，不是建设日志 schema 框架。
* 字段常量会增加跨包依赖和抽象成本。
* 通过方案约定即可保证第一阶段查询体验一致。

### 决策 3：gateway 不对每条 media segment 建 span/log

WebSocket media segment 是高频路径。正常转发不逐条 trace/log，只在异常、media init、control 类消息、连接生命周期记录。

原因：

* 避免 trace/log 量随视频帧率或分片数线性暴涨。
* control 操作和连接生命周期才是第一阶段排障重点。

### 决策 4：deploy 成功路径记录资源计数，失败路径记录资源明细

deploy reconcile 成功时只记录资源计数；失败时记录具体资源 kind/name/namespace。

原因：

* 成功路径逐资源日志价值低且噪音大。
* 失败路径需要定位具体 Kubernetes 资源。

## 测试与验收

### 单元测试与构建

改造完成后执行：

```bash
bazel run @rules_go//go -- fmt <changed go files>
bazel run //:gazelle projects/infra/deploy projects/game/gateway projects/game/session
bazel test //projects/infra/deploy/... //projects/game/gateway/... //projects/game/session/...
bazel build //projects/infra/deploy/... //projects/game/gateway/... //projects/game/session/...
```

如新增或调整公共依赖，再执行：

```bash
bazel mod tidy
```

### 大型测试

* deploy：`projects/infra/deploy/README.md` 明确本服务不进行大型测试，因此不执行 testplan。
* gateway：执行 `projects/game/gateway/testplan/interface_test.yaml`。
* session：执行 `projects/game/session/testplan/interface_test.yaml`。

大型测试按仓库要求使用 `testplan` skill 执行。

### 手动 QA

至少完成以下表面验证：

* deploy：通过 HTTP/gRPC gateway 创建或更新一个 environment，确认请求成功，后台 worker reconcile 产生同一 trace 相关业务日志。
* gateway：建立 web 与 agent WebSocket 连接，发送 hello、control request、ack/result，确认 connect/control/completion 有 trace 和日志；media segment 正常转发时不产生逐条噪音日志。
* session：通过 HTTP 创建、查询、重连、删除 session，确认 session lifecycle、gateway pick、connect URL enrich 有 trace 和日志，且日志不包含 token 或完整 connect URL。
* 在 OTel 后端按 trace id 查询，确认同一次请求的 HTTP span、业务 span、结构化日志可以关联。

## 未来规划

以下内容不属于本轮范围：

* 为 deploy reconcile、gateway connection/control、session lifecycle 设计业务 metrics。
* 为日志字段建立机器校验或公共 schema。
* 为 trace sampling、日志级别、debug 开关增加运行时配置。
* 为 WebSocket media 路径设计聚合指标或低频采样 trace。
