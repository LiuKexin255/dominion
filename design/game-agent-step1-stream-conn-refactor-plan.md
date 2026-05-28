# Agent 玩游戏 step1 stream 与 conn 生命周期修正方案

本文档承接 `design/game-agent-step1-refactor-plan.md`，只收敛当前实现中仍未满足目标的三个问题：`gateway` WebSocket stream 生命周期、`gameconst` session name 反向解析、`proxy` agentclient manager 的连接资源管理。本文不重新描述 step1 的产品范围、服务职责和其他已完成重构项。

## 目标

完成本方案后应达到以下效果：

1. `gateway` WebSocket 与后端 gRPC stream 使用同一个请求 `context.Context` 管理生命周期；上游连接关闭或 ctx 取消后，下游 stream 通过同一 ctx 自然退出，不再把 WebSocket 侧中断状态扩散到 gRPC stream wrapper。
2. `bind.Binder` 只负责两个 `AgentFrameStream` 的双向转发和首个错误返回，不再接收或管理 ctx；具体 stream 自己负责把 ctx 应用于底层 I/O。
3. `gameconst` 同时提供 `session_id -> session name` 和 `session name -> session_id` 的集中转换，避免 resource name 解析规则散落在 handler 中。
4. `proxy/runtime/agentclient.Manager` 管理长期资源 `*grpc.ClientConn`，不再把轻量 client stub 作为缓存资源；业务调用点按需从 conn 创建 client stub。

## 非目标

本方案不引入以下内容：

1. 不修改 proto API。
2. 不改变 WebSocket JSON frame 的格式或 gateway 路由。
3. 不引入新的负载调度、健康检查、心跳或重连机制。
4. 不改变 agent 当前 step1 的 status/echo 行为。
5. 不重写 `bootstrap.Daemon` 机制，只调整 agentclient manager 的资源模型。

## 现状问题

### gateway WebSocket stream 生命周期

当前 `projects/game/gateway/cmd/main.go` 中 `wsStream` 的 `Recv` 和 `Send` 使用 `context.Background()` 调用 WebSocket 读写。为让 WebSocket 侧错误影响 gRPC 侧，又额外引入了 `done chan error` 和 `gRPCStream` wrapper。

该实现的问题是：

1. WebSocket adapter 没有持有请求 ctx，底层 I/O 生命周期不由请求 ctx 直接控制。
2. WebSocket 侧中断状态通过 `gRPCStream` 扩散到 gRPC stream，破坏了 stream adapter 边界。
3. `bind.Bind(ctx, ...)` 虽然接收 ctx，但 `forward` 中没有实际使用 ctx；ctx 管理责任位置不清。

### gameconst 缺少 session name 反向解析

当前 `projects/game/pkg/gameconst` 已提供 `SessionName(sessionID string)`，但没有集中提供从 `sessions/{session_id}` 解析出 `session_id` 的 helper。调用点仍通过 `strings.TrimPrefix` 自行解析，导致规则扩散。

### agentclient manager 仍在管理 client

当前 `projects/game/proxy/runtime/agentclient.Manager` 的缓存条目同时持有 `*grpc.ClientConn` 和 `Client`，但接口 `Get/List` 返回的是 `Client` / `ClientRef`，关闭资源也通过 `client.Close()` 间接执行。

这仍然把轻量 client stub 当作长期管理资源，不符合 `game-agent-step1-refactor-plan.md` 中“服务统一复用 `*grpc.ClientConn`”的目标。

## 模型设计

### bind 包

调整后的接口：

```go
type AgentFrameStream interface {
    Recv() (*game.AgentFrame, error)
    Send(*game.AgentFrame) error
}

type Binder interface {
    Bind(left AgentFrameStream, right AgentFrameStream) error
}
```

`Binder` 不再接收 ctx。ctx 是具体 stream 的一部分，由 stream 在自己的 `Recv` / `Send` 中使用。

`Bind` 使用 2 个 channel 和 4 个 goroutine：

```text
left.Recv()  -> leftToRight chan -> right.Send(frame)
right.Recv() -> rightToLeft chan -> left.Send(frame)
```

生命周期规则：

1. 任意一个 `Recv` 或 `Send` 返回错误时，记录首个错误，设置退出标记，`Bind` 立刻返回。
2. `Bind` 不等待 4 个 goroutine 全部退出。
3. goroutine 在 stream 底层连接关闭或 ctx 取消后自然退出。
4. 两个 Recv goroutine 在向 channel 写入前检查退出标记：
   1. 如果退出标记为 true，关闭当前方向 channel 后退出。
   2. 如果退出标记为 false，将 frame 写入 channel。
5. Send goroutine 在对应 channel 关闭后退出。
6. `io.EOF`、`context.Canceled` 视为正常关闭，`Bind` 返回 nil。
7. 非 clean close 的首个错误原样返回给调用方，由入口层决定 close policy 或 status mapping。

该设计要求 `Bind` 不承担关闭 transport 的职责。调用方仍负责 `defer conn.CloseNow()`、gRPC stream 生命周期和请求 ctx 生命周期。

### gateway wsStream

`wsStream` 持有请求 ctx：

```go
type wsStream struct {
    ctx       context.Context
    conn      *websocket.Conn
    sessionID string
}
```

`Recv` 使用：

```go
s.conn.Read(s.ctx)
```

`Send` 使用：

```go
s.conn.Write(s.ctx, websocket.MessageText, data)
```

gateway handler 连接链路：

```text
ctx := r.Context()
websocket.Accept
proxyClient.ConnectAgent(ctx)
wsStream{ctx: ctx, conn: conn, sessionID: sessionID}
bind.NewBinder().Bind(wsStream, proxyGrpcStream)
```

删除 gateway 内部的 `gRPCStream` wrapper 和 `done chan error`。WebSocket 侧错误不再通过额外 wrapper 注入 gRPC stream；上下游退出依赖同一个 ctx 和底层 stream 关闭。

### gameconst session 资源名转换

保留现有前向 helper：

```go
func SessionName(sessionID string) string
```

新增反向 helper 与 sentinel error：

```go
var ErrInvalidSessionName = errors.New("invalid session name")

func SessionID(name string) (string, error)
```

规则：

1. `name` 必须以 `SessionNamePrefix` 开头。
2. prefix 后的 session id 不能为空。
3. 不在该 helper 中解析 agent name；agent resource 如需解析，另行定义对应 helper。
4. handler 捕获 `ErrInvalidSessionName` 后映射为 API 边界错误：gRPC `InvalidArgument` 或 HTTP `BadRequest`。

### agentclient manager

manager 的缓存资源改为 `*grpc.ClientConn`。

建议接口：

```go
type Manager interface {
    Get(ctx context.Context, ownerIndex int) (*ConnRef, error)
    List(ctx context.Context) ([]*ConnRef, error)
    Close() error
}

type ConnRef struct {
    OwnerIndex int
    Owner      string
    Conn       *grpc.ClientConn
}
```

注意：`Get` 按本次决策返回 `*ConnRef`，`List` 返回指针数组 `[]*ConnRef`。

缓存条目：

```go
type connEntry struct {
    conn       *grpc.ClientConn
    ownerIndex int
    ownerName  string
}
```

连接创建替身变量：

```go
var newAgentConn = func(ctx context.Context, instanceIndex int) (*grpc.ClientConn, error) {
    uri := solver.URI(gameconst.AgentTarget, solver.WithInstance(instanceIndex))
    return grpc.NewClient(uri, pgrpc.ClientDefault()...)
}
```

client stub 按需从 conn 创建：

```go
func NewAgentClient(conn *grpc.ClientConn) Client {
    return &AgentClient{client: game.NewAgentServiceClient(conn)}
}
```

`AgentClient` 不再持有 conn，也不提供关闭 conn 的语义。长期连接只由 manager 的 `Close`、refresh stale removal 和 daemon stop 管理。

## 分层调整

### gateway

变更范围：

1. `projects/game/gateway/cmd/main.go`
   - `wsStream` 增加 `ctx context.Context` 字段。
   - `Recv/Send` 使用 `s.ctx`。
   - 删除 `done` 字段。
   - 删除 `gRPCStream` 类型。
   - 调用 `b.Bind(ws, stream)`。

### bind

变更范围：

1. `projects/game/pkg/bind/binder.go`
   - `Binder.Bind` 移除 ctx 参数。
   - 使用 2 channel + 4 goroutine 实现双向转发。
   - 任意错误后记录首个错误并立即返回。
2. `projects/game/pkg/bind/binder_test.go`
   - 更新调用签名。
   - 增加 Recv 错误、Send 错误、clean close、退出标记检查相关测试。

### proxy service

变更范围：

1. `projects/game/proxy/service/connect_agenter.go`
   - `binder.Bind(ctx, prefixed, agentStream)` 改为 `binder.Bind(prefixed, agentStream)`。
   - `manager.Get` 返回 `*ConnRef` 后，通过 `agentclient.NewAgentClient(ref.Conn)` 创建临时 client。
2. 相关 mock binder 和测试同步更新签名。

### gameconst 与 session handler

变更范围：

1. `projects/game/pkg/gameconst/const.go`
   - 新增 `ErrInvalidSessionName`。
   - 新增 `SessionID(name string) (string, error)`。
2. `projects/game/session/handler/handler.go`
   - `GetSession` 和 `DeleteSession` 使用 `gameconst.SessionID`。
   - 删除本文件对 `strings.TrimPrefix` 的直接依赖。
3. 测试补充非法 name、空 session id 的错误映射。

### proxy agentclient

变更范围：

1. `projects/game/proxy/runtime/agentclient/client.go`
   - `NewAgentClient` 改为接收 `*grpc.ClientConn`。
   - `AgentClient` 不再保存 conn。
   - 移除 `Client.Close()`，或至少不再作为 manager 关闭路径使用。
2. `projects/game/proxy/runtime/agentclient/manager.go`
   - `ClientRef` 改为 `ConnRef`。
   - `Manager.Get` 返回 `*ConnRef`。
   - `Manager.List` 返回 `[]*ConnRef`。
   - refresh 新增实例时创建 conn。
   - refresh 删除 stale 实例时关闭 conn。
   - `Close` 关闭所有 cached conn。
3. `projects/game/proxy/runtime/picker/*`
   - `[]ClientRef` 改为 `[]*ConnRef`。
4. `projects/game/proxy/handler/*`
   - 从 picked `ConnRef.Conn` 创建临时 agent client。
5. `projects/game/proxy/service/*`
   - `Get` 返回 `*ConnRef` 后创建临时 agent client。

## 关键细节

### Bind 的错误收敛

`Bind` 返回策略按本次决策固定为：立刻返回首个错误，不等待 goroutine 全部退出。

推荐内部结构：

```go
type bindState struct {
    done atomic.Bool
    once sync.Once
    errCh chan error
}
```

`report(err)`：

1. 将 `io.EOF` / `context.Canceled` 归一化为 nil。
2. `once.Do` 设置 `done = true`。
3. 将首个结果写入 `errCh`。

Recv goroutine：

```text
for {
  frame, err := src.Recv()
  if err != nil { report(err); close(out); return }
  if done { close(out); return }
  out <- frame
}
```

Send goroutine：

```text
for frame := range in {
  if err := dst.Send(frame); err != nil { report(err); return }
}
```

Send goroutine 不主动检查退出标记，只在 channel 关闭后自然退出，或在 `Send` 返回错误时上报首个错误并退出。为了避免 `Bind` 永久等待，只有 `errCh` 是 `Bind` 的返回触发条件；goroutine 自然退出由 stream ctx/连接关闭保证。

### gateway close code 映射

gateway 仍沿用当前入口策略：

1. clean close 返回正常 WebSocket close。
2. JSON 协议错误映射为 `websocket.StatusInvalidFramePayloadData`。
3. 其他错误映射为 `websocket.StatusInternalError`。

区别是协议错误只从 `wsStream.Recv` 返回，不再通过 `gRPCStream.done` 传播。

### ConnRef 指针数组

`Manager.List` 返回 `[]*ConnRef` 的原因：

1. 与本次决策一致。
2. 避免后续 ConnRef 增加状态字段时出现大结构复制。
3. picker 只读取 `OwnerIndex` / `Owner`，不会修改 ConnRef。

实现时应在 `List` 中为每个 entry 创建新的 `ConnRef` 指针，不暴露 manager 内部 entry 指针。

## 测试计划

### 单元测试

至少覆盖：

1. `pkg/bind`
   - left -> right 转发。
   - right -> left 转发。
   - 双向同时转发。
   - `Recv` 返回 `io.EOF` 时 clean close。
   - `Recv` 返回 `context.Canceled` 时 clean close。
   - `Recv` 返回普通错误时 `Bind` 立刻返回该错误。
   - `Send` 返回普通错误时 `Bind` 立刻返回该错误。
   - Recv goroutine 在退出标记已设置时关闭当前 channel 并退出。
2. `gateway/cmd`
   - WebSocket unknown field 仍可解析。
   - 非法 JSON 返回协议错误 close code。
   - status/echo 往返通过。
   - client 关闭 WebSocket 后 handler 不泄露阻塞在 `context.Background()` 的读写。
3. `gameconst`
   - `SessionName("abc") == "sessions/abc"`。
   - `SessionID("sessions/abc") == "abc"`。
   - `SessionID("abc")` 返回 `ErrInvalidSessionName`。
   - `SessionID("sessions/")` 返回 `ErrInvalidSessionName`。
4. `session/handler`
   - Get/Delete 使用合法 name 成功解析。
   - 非法 name 映射为 `InvalidArgument`。
5. `agentclient/manager`
   - refresh 新增实例创建 conn。
   - refresh 移除 stale 实例关闭 conn。
   - `Get` 返回目标 instance 的 `*ConnRef`。
   - `List` 返回 `[]*ConnRef`，内容包含 owner metadata 和 conn。
   - `Close` 关闭全部 conn。
   - 初始 refresh 失败返回 error。
   - 周期 refresh 失败保留旧 conn。
6. `proxy/handler` 与 `proxy/service`
   - mock manager 返回 conn。
   - 调用点通过 `ConnRef.Conn` 按需创建 client stub。
   - ConnectAgent 首帧回放和后续 frame 转发行为保持不变。

### 构建验证

完成代码修改后执行：

```bash
bazel run @rules_go//go -- fmt \
  projects/game/pkg/bind/binder.go \
  projects/game/pkg/bind/binder_test.go \
  projects/game/gateway/cmd/main.go \
  projects/game/gateway/cmd/main_test.go \
  projects/game/pkg/gameconst/const.go \
  projects/game/session/handler/handler.go \
  projects/game/session/handler/handler_test.go \
  projects/game/proxy/runtime/agentclient/client.go \
  projects/game/proxy/runtime/agentclient/manager.go \
  projects/game/proxy/runtime/agentclient/manager_test.go \
  projects/game/proxy/runtime/picker/hash_picker.go \
  projects/game/proxy/runtime/picker/hash_picker_test.go \
  projects/game/proxy/handler/handler.go \
  projects/game/proxy/handler/handler_test.go \
  projects/game/proxy/service/connect_agenter.go \
  projects/game/proxy/service/connect_agenter_test.go
bazel run //:gazelle projects/game
bazel test //projects/game/...
bazel build //projects/game/...
```

## 决策记录

1. `SessionID(name)` 使用 sentinel error：`ErrInvalidSessionName`。
2. `agentclient.Manager.List` 返回 `[]*ConnRef`，不返回值数组。
3. `agentclient.Manager.Get` 返回 `*ConnRef`，不返回 client stub。
4. `Bind` 移除 ctx 参数；ctx 属于 stream，不属于 binder。
5. `Bind` 在任意 `Recv/Send` 出错后立刻返回首个错误，不等待 goroutine 全部退出。
6. `Bind` 内部 goroutine 依赖 stream 关闭或 stream ctx cancel 后自然退出。
7. gateway 删除 `gRPCStream` wrapper，WebSocket 错误不再通过额外 wrapper 传播到 gRPC stream。

## 验收标准

1. `gateway` 中不存在 `gRPCStream` 和 `wsStream.done`。
2. `wsStream.Recv/Send` 均使用创建 stream 时持有的请求 ctx。
3. `bind.Binder.Bind` 签名为 `Bind(left, right AgentFrameStream) error`。
4. 所有 `Bind` 调用点不再传 ctx。
5. `gameconst` 提供 `SessionID(name)` 和 `ErrInvalidSessionName`。
6. session handler 不再直接使用 `strings.TrimPrefix` 解析 session name。
7. `agentclient.Manager` 的缓存条目不包含长期 client stub。
8. `agentclient.Manager.Get` 返回 `*ConnRef`。
9. `agentclient.Manager.List` 返回 `[]*ConnRef`。
10. stale conn 和 manager close 都直接关闭 `*grpc.ClientConn`。
11. `bazel test //projects/game/...` 通过。
12. `bazel build //projects/game/...` 通过。
