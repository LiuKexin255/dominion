# Game Desktop session 展示与 trace 日志修复方案

本文档作为 `design/game/game-agent-step2.md` 的补充修复方案，针对当前 desktop 实现中的两个问题：

1. Session 信息框中 `Name` 有值但 `Session ID` 为空，导致 Create/Get Agent 操作可能使用空 session id。
2. Desktop 日志框没有展示 tracing id，用户无法复制 trace id 到 SigNoz 查询服务端链路。

## 目标

完成本方案后应达到以下效果：

1. Desktop Session 详情只展示非空 `sessionId`，不再展示 `name`。
2. Create/Get/Delete/Connect Agent 操作始终使用真实 session id，不再向 gateway 发送空 session id 路径。
3. Desktop 每次 HTTP 操作日志都展示本次请求的 `trace_id`，成功和失败日志都能定位同一次请求。
4. Desktop WebSocket Connect Agent 日志展示 `trace_id`，并通过 `websocket.DialOptions.HTTPClient` 注入 `traceparent`。
5. `internal/api` client 不负责创建 trace context，也不返回 trace id；trace context 由 Wails App 操作层创建并传入。

## 非目标

本方案不包含以下内容：

1. 不改变 `projects/game/game.proto` 协议。
2. 不改变 gateway、session、proxy、agent 服务端路由和 tracing 装配。
3. 不让 desktop 上报日志或 span；desktop 仍只负责本地日志展示和 trace context 传播。
4. 不引入跨多个用户操作共享同一个 trace id 的会话级 trace；每个用户操作使用独立 trace id。
5. 不修改 `AGENTS.md` 或既有 step2 方案原文。

## 问题分析

### Session ID 为空

当前 desktop Go 后端直接把 Go proto 对象返回给 Wails。Go proto 生成结构体使用标准 `encoding/json` tag，例如：

```go
SessionId string `protobuf:"bytes,2,opt,name=session_id,json=sessionId,proto3" json:"session_id,omitempty"`
```

Wails 返回给前端时字段名是 `session_id`，但前端类型和页面逻辑读取的是 `sessionId`。因此 UI 中 `name` 可以显示，`sessionId` 为空；随后 Create/Get Agent 使用 `selectedSession.sessionId` 发起请求时可能传入空字符串。

### 日志没有 trace id

当前 `projects/game/desktop/internal/trace` 已提供 trace transport，但 trace context 是在 HTTP transport 的 `RoundTrip` 中临时 `Ensure` 的。App 日志层没有创建 trace context，也没有调用 `TraceIDFromContext`，因此日志 entry fields 中没有 `trace_id`。

WebSocket 侧当前直接 `websocket.Dial(context.Background(), ...)`，没有通过带 trace transport 的 HTTP client 注入 `traceparent`。

## 模型设计

### Wails View Model

在 Wails 边界新增 desktop/frontend 专用只读 view model。HTTP/WebSocket 对外通信仍使用 proto 类型和 protojson；view model 只用于 Wails IPC 与 UI 展示。

建议放置在 `projects/game/desktop/app.go` 或拆分到 `projects/game/desktop/view_model.go`：

```go
type SessionView struct {
    SessionID  string `json:"sessionId"`
    CreateTime string `json:"createTime,omitempty"`
}

type ListSessionsView struct {
    Sessions      []SessionView `json:"sessions"`
    NextPageToken string        `json:"nextPageToken,omitempty"`
}

type AgentView struct {
    SessionID  string `json:"sessionId"`
    OwnerIndex int32  `json:"ownerIndex"`
    Owner      string `json:"owner,omitempty"`
    CreateTime string `json:"createTime,omitempty"`
}
```

决策：Session 详情页只展示 session id，因此 `SessionView` 不需要暴露 `name`。如后续 UI 需要资源名，可再增加只读字段，但不得替代 `sessionId` 作为操作参数。

### Trace Context 所有权

Trace context 由 Wails App 操作层创建，`internal/api` 只消费调用方传入的 `context.Context`。

```text
frontend click
  -> App.CreateAgent()
    -> ctx := tracecontext.Ensure(a.ctx)
    -> traceID := desktoptrace.TraceIDFromContext(ctx)
    -> log fields include trace_id
    -> api.Client.CreateAgent(ctx, sessionID)
      -> http.NewRequestWithContext(ctx, ...)
      -> trace transport injects traceparent
```

这样 App 层可以在请求前后记录同一个 `trace_id`，client 层不需要把 trace id 放进返回值。

## 代码分层

### `projects/game/desktop/app.go`

职责：

1. 创建每次用户操作的 trace context。
2. 从 context 提取 trace id，并写入 desktop 本地日志 fields。
3. 将 trace context 传给 `internal/api` HTTP/WebSocket client。
4. 将 proto 返回值转换为 Wails view model。
5. 在 Agent 操作前校验 `sessionId` 非空。

### `projects/game/desktop/internal/api/client.go`

职责：

1. 所有 HTTP 方法接收 `ctx context.Context` 参数。
2. 使用传入 ctx 创建 HTTP request。
3. 继续使用 protojson 作为 HTTP body/response 编解码模型。
4. 不创建 trace context，不提取 trace id，不返回 trace id。

建议方法签名：

```go
func (c *Client) CreateSession(ctx context.Context) (*game.Session, error)
func (c *Client) ListSessions(ctx context.Context, pageSize int32, pageToken string) (*game.ListSessionsResponse, error)
func (c *Client) GetSession(ctx context.Context, sessionID string) (*game.Session, error)
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error
func (c *Client) CreateAgent(ctx context.Context, sessionID string) (*game.Agent, error)
func (c *Client) GetAgent(ctx context.Context, sessionID string) (*game.Agent, error)
func (c *Client) DeleteAgent(ctx context.Context, sessionID string) error
func (c *Client) GetAgentStatus(ctx context.Context, sessionID string) (*game.AgentStatus, error)
```

### `projects/game/desktop/internal/api/websocket.go`

职责：

1. `WSClient.Connect` 接收调用方传入的 `context.Context`。
2. 使用 `websocket.DialOptions.HTTPClient` 配置带 trace transport 的 HTTP client。
3. `env` 仍通过 `DialOptions.HTTPHeader` 设置。
4. 不手写 `traceparent` header。

建议方法签名：

```go
func (w *WSClient) Connect(ctx context.Context, gatewayURL, sessionID, env string) error
```

示意：

```go
conn, _, err := websocket.Dial(ctx, fullURL, &websocket.DialOptions{
    HTTPClient: &http.Client{Transport: trace.NewHTTPTransport()},
    HTTPHeader: header,
})
```

## 关键细节

### 1. App 层创建 trace context

每个 Wails 入口方法中，凡是会发出 HTTP 或 WebSocket 请求的操作，都应先创建 trace context：

```go
ctx := tracecontext.Ensure(a.ctx)
traceID := desktoptrace.TraceIDFromContext(ctx)
```

日志 fields 统一使用 `trace_id`：

```go
a.logger.Info("backend", "Creating agent", map[string]any{
    "session_id": sessionID,
    "trace_id": traceID,
})
```

失败日志也必须带同一个 `trace_id`：

```go
a.logger.Error("backend", "Create agent failed", map[string]any{
    "session_id": sessionID,
    "trace_id": traceID,
    "error": err.Error(),
})
```

### 2. Client 只使用调用方 ctx

HTTP client 不得在方法内部使用 `context.Background()` 创建请求。所有请求必须使用调用方传入 ctx：

```go
req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
```

`trace.NewHTTPTransport()` 仍可在 `NewClient` 中配置；它负责从 request context 注入 traceparent，但不再拥有 trace context 创建职责。

### 3. WebSocket 使用 HTTPClient 注入 traceparent

WebSocket Connect 不手写 `traceparent` header。调用方传入的 ctx 携带 trace context，`websocket.DialOptions.HTTPClient` 使用 trace transport，由 transport 注入 header。

### 4. View model 转换

App 层应显式转换 proto 到 view model：

```go
func sessionViewFromProto(s *game.Session) SessionView {
    if s == nil {
        return SessionView{}
    }
    return SessionView{
        SessionID:  s.GetSessionId(),
        CreateTime: timestampString(s.GetCreateTime()),
    }
}
```

`timestampString` 应复用现有时间格式风格，优先使用 RFC3339。若 proto timestamp 为空，可返回空字符串。

### 5. Agent 操作非空校验

App 层在调用 `CreateAgent`、`GetAgent`、`DeleteAgent`、`ConnectAgent` 前校验 session id：

```go
if sessionID == "" {
    return nil, fmt.Errorf("session_id is required")
}
```

这样即使前端状态异常，也不会向 gateway 发 `/api/v1/sessions//agent` 这类请求。

### 6. 前端展示

`SessionDetail.svelte` 删除 Name 行，只展示 Session ID：

```svelte
<span class="info-key">Session ID</span>
<span class="info-value">{session.sessionId}</span>
```

前端类型继续使用 camelCase，不引入 `session_id` 字段。

## 决策详情

### 为什么不直接让前端读取 `session_id`

`session_id` 是 Go proto 结构体经 `encoding/json` 暴露出来的实现细节。desktop 设计中 HTTP/WebSocket 通信模型应使用 proto，但 Wails IPC 是 UI 边界，允许使用只读 view model。使用 view model 可以避免 UI 依赖 Go proto JSON tag，也能保持前端代码符合常见 TypeScript camelCase 风格。

### 为什么不在 `internal/api` 返回 trace id

trace id 是一次用户操作的观测字段，不是 game API 的业务返回值。由 App 层创建 ctx 后，App 层天然能拿到 trace id 并记录日志；client 层只负责协议通信，避免污染 API 返回值。

### 为什么 WebSocket 不手写 traceparent header

仓库已有 trace transport 能从 context 注入 W3C traceparent。`websocket.DialOptions.HTTPClient` 可以复用该能力，避免重复实现 header 注入逻辑，也能与 HTTP client 行为保持一致。

## 测试方案

### 单元测试

建议覆盖：

1. View model 转换：`game.Session{SessionId:"s1"}` 转为 `SessionView{SessionID:"s1"}`，JSON 字段为 `sessionId`。
2. `ListSessions` view model 转换保留 `nextPageToken`。
3. Agent view model 转换保留 `sessionId`、`ownerIndex`、`owner`。
4. App 层 Agent 操作在 session id 为空时返回本地错误，不调用 HTTP client。
5. `internal/api.Client` 各 HTTP 方法使用传入 ctx 创建请求，并仍注入 `traceparent`。
6. `WSClient.Connect(ctx, ...)` 通过 `DialOptions.HTTPClient` 注入 `traceparent`，测试服务端握手 request header 能收到 `traceparent`。

### 手动验收

1. 启动 desktop。
2. 点击 Create Session。
3. 进入 Session Detail，页面只显示非空 Session ID，不显示 Name。
4. 点击 Create Agent，日志框显示 `session_id` 和 `trace_id`。
5. 点击 Get Agent，日志框显示 `session_id` 和 `trace_id`，且不再出现空 session id 导致的 404。
6. 点击 Connect Agent，日志框显示 `trace_id`。
7. 使用日志框中的 `trace_id` 在 SigNoz 查询对应 gateway/server span；若本地服务非 deploy 模式，预期仍可能查不到远端 span，但日志中必须有 trace id。

### Bazel 验证

建议执行：

```bash
bazel run //:go -- fmt projects/game/desktop
bazel test //projects/game/desktop/...
bazel build //projects/game/desktop:desktop
```

如修改 BUILD 文件或新增 Go 文件后 target 缺失，按仓库规范执行：

```bash
bazel run //:gazelle projects/game/desktop
```

## 后续规划

1. 如需要跨 Create Session、Create Agent、Connect Agent 多个操作共享同一条 trace，可在 App 层增加显式“操作链路 trace”状态；本方案暂不引入。
2. 如需要 desktop 自身 span 出现在 SigNoz，需要引入 desktop OTel exporter 配置；这会改变 desktop 观测边界，本方案不处理。
