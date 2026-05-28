# Agent 玩游戏 step1 实现收敛重构方案

本文档基于 `design/game-agent-step1-improved.md` 的实现评审结论，进一步收敛当前 `projects/game` 下 `gateway`、`session`、`proxy`、`agent` 四个服务的实现结构。目标不是改变 step1 的产品范围，而是修正实现中已经暴露的边界不清、生命周期不一致、常量扩散、stream 绑定重复和 domain 模型污染问题。

## 目标

本次重构完成后应达到以下效果：

1. gRPC 连接生命周期清晰：服务统一复用 `*grpc.ClientConn`，不把轻量 client stub 当作需要长期管理的资源。
2. `projects/game` 内公共 target、资源名和日志字段集中维护，减少硬编码扩散。
3. WebSocket 与 gRPC stream 转发使用同一套 bind 抽象，`gateway` 和 `proxy` 的转发实现风格一致。
4. `proxy` 的 agent client 后台刷新符合 `bootstrap.Daemon` 风格，不再由 manager 自己伪装成 daemon-stage component。
5. `session` domain/runtime 只处理 `session_id`，`name = sessions/{session_id}` 只在 API/handler 边界生成。
6. `agent` handler 不直接把 gRPC stream 丢给 runtime，handler 负责协议转换，runtime 只暴露符合 domain 语义的方法。

## 非目标

本次重构不引入以下内容：

1. 真实游戏策略、LLM、DeepAgent、LangGraph 集成。
2. 复杂负载调度、容量管理或心跳机制。
3. 鉴权、限流、多租户。
4. proto API 语义调整。
5. grpc-gateway 对 bidirectional streaming 的转换。

## 现状问题

### gateway

当前 `gateway/cmd/main.go` 的 WebSocket handler 已经通过 `common/gopkg/http.Handler` 包装在 HTTP server 外层，因此 HTTP upgrade 请求可以由 `otelhttp` 提取或创建 OTel 上下文，并通过 `r.Context()` 传给后端 gRPC stream。这里不应使用 `common/gopkg/otel/tracecontext`，该包主要用于 CLI/tools 场景。

需要修正的是 WebSocket 与 gRPC stream 双向转发的生命周期：当前只有 WebSocket 到 gRPC 方向运行在 goroutine 中，另一个方向在主 goroutine 中阻塞，缺少统一的 bind/等待/错误映射模型。

### proxy

`proxy/runtime/stream` 已有通用的 `AgentFrameStream` 和 `Binder`，但它只放在 proxy runtime 下，gateway 无法复用。`proxy/service/connect_agenter.go` 中的 `grpcServerStream` 和 `grpcClientStream` 也是重复适配，因为 generated gRPC stream 已经具备：

```go
Recv() (*game.AgentFrame, error)
Send(*game.AgentFrame) error
```

因此这些 generated stream 已经隐式实现 `AgentFrameStream`。

`proxy/runtime/agentclient.Manager` 当前直接实现 `bootstrap.Component` 并返回 `bootstrap.StageDaemon`，但仓库内后台 worker 更一致的表达是 `bootstrap.Daemon(name, WorkerBuilder, ...)`。另外 `manager.newClient` 是只为测试替身存在的 struct 字段，会干扰 manager 的业务结构理解，应改成包级测试替身变量。

### session

`session/domain.Session` 当前仍包含 `Name` 字段，导致 domain、runtime、handler 多处都要处理 `sessions/{session_id}` 与 `session_id` 的转换。按资源模型设计，`name` 是 API 层可派生字段，不应进入 domain/runtime。

### agent

当前 agent 只是空壳服务，但 handler 仍不应直接把 gRPC stream 交给 runtime。handler 包的职责是把 gRPC 请求转换为 domain 设计的方法调用；runtime 不应感知 gRPC stream 形态。

## 包与模型设计

### 公共常量包

新增：

```text
projects/game/pkg/gameconst
```

不使用 `pkg/const`，因为 `const` 是 Go 关键字。

建议内容：

```go
const (
    SessionTarget = "game/session:grpc"
    ProxyTarget   = "game/proxy:grpc"
    AgentTarget   = "game/agent:grpc"

    SessionNamePrefix = "sessions/"

    LogFieldName       = "name"
    LogFieldSessionID  = "session_id"
    LogFieldOwner      = "owner"
    LogFieldAgentIndex = "agent_index"
)

func SessionName(sessionID string) string
func AgentName(sessionID string) string
```

使用范围：

1. `gateway/cmd/main.go` 中的 backend target。
2. `session/cmd/main.go` 中的 proxy target。
3. `proxy/cmd/main.go` 和 `proxy/runtime/agentclient/client.go` 中的 agent target。
4. handler/repository 中的 session name prefix 与 agent name 拼接。
5. agent/proxy/session 中公共日志字段。

### 公共 bind 包

新增：

```text
projects/game/pkg/bind
```

该包承载 `AgentFrame` 双向转发的通用抽象。建议 API：

```go
type AgentFrameStream interface {
    Recv() (*game.AgentFrame, error)
    Send(*game.AgentFrame) error
}

type Binder interface {
    Bind(ctx context.Context, left AgentFrameStream, right AgentFrameStream) error
}

func NewBinder() Binder
func WithFirstFrame(inner AgentFrameStream, first *game.AgentFrame) AgentFrameStream
```

职责划分：

1. `Binder` 只负责两个 `AgentFrameStream` 之间的双向复制和生命周期等待。
2. generated gRPC stream 直接作为 `AgentFrameStream` 使用，不再创建额外 gRPC wrapper。
3. `bind` 包不包含任何具体传输实现，不直接依赖 WebSocket、gRPC handler 或具体 service 包。
4. WebSocket 需要 adapter，因为它的消息是 JSON text frame，不是 `AgentFrame` stream；该 adapter 应放在 gateway 边界内实现。
5. `WithFirstFrame` 封装“首帧已读取但仍需作为普通 frame 交给 binder 转发”的场景。

错误策略：

1. `io.EOF`、`context.Canceled`、正常 close 视为清理结束。
2. WebSocket 非法 JSON 返回 typed/protocol error，由 gateway handler 映射为 `websocket.StatusInvalidFramePayloadData`。
3. bind 包不直接决定 HTTP/WebSocket close policy，也不定义 WebSocket 错误类型，避免公共包耦合入口策略。

### agentclient manager 与 daemon

`agentclient.Manager` 保持业务接口：

```go
type Manager interface {
    Get(ctx context.Context, ownerIndex int) (Client, error)
    List(ctx context.Context) ([]ClientRef, error)
    Close() error
}
```

manager 不再实现 `bootstrap.Component`，而是拆分为：

1. manager：维护当前 agent client 缓存、刷新逻辑、查询逻辑。
2. refresher worker：执行初始 refresh 和周期 refresh。
3. builder：为 `bootstrap.Daemon` 构造 worker。

建议形态：

```go
type RefresherBuilder struct {
    Manager  *manager
    Interval time.Duration
}

func (b *RefresherBuilder) Build(ctx context.Context) (bootstrap.Worker, error)
func NewDaemon(m Manager, interval time.Duration) bootstrap.Component
```

错误策略：

1. 初始 refresh 失败：返回 error，让 `bootstrap.Daemon` 按 restart policy 重试。
2. 周期 refresh 失败：记录日志，保留旧 client 列表继续运行，避免短暂 resolver 故障导致服务整体退出。
3. Stop/Close：关闭 refresh worker，并关闭 manager 中所有 cached client/conn。

测试替身：

```go
var newAgentClient = func(ctx context.Context, instanceIndex int) (Client, error) {
    return NewAgentClient(ctx, instanceIndex)
}
```

测试中使用 save/restore + `t.Cleanup`，不再把 `newClient` 放进 manager struct。

### session domain 模型

调整为：

```go
type Session struct {
    SessionID  string
    CreateTime time.Time
}
```

转换规则：

1. handler 接收 API resource name 时解析出 `session_id`。
2. domain/repository/runtime 只接收和返回 `session_id`。
3. handler 返回 proto 时通过 `gameconst.SessionName(sessionID)` 生成 `name`。
4. Mongo document 继续只保存 `session_id` 和必要时间字段。

### agent handler/runtime 边界

移除 `agent/handler` 中的 `grpcAgentStream`，但不把 generated gRPC stream 直接传给 runtime。

收敛方向：

1. handler 负责读取 `game.AgentFrame`，识别 `session_id` 和消息语义。
2. runtime 暴露 domain 语义方法，例如 status/echo 当前 step1 所需能力。
3. handler 将 runtime 返回值转换为 `game.AgentFrame` 并通过 gRPC stream 发送。

这样即使当前 agent 是空壳，包边界仍保持：handler 处理协议，runtime 处理 domain 状态和行为。

## 分层调整

调整后建议目录：

```text
projects/game/
  pkg/
    gameconst/
      const.go
    bind/
      binder.go
      first_frame.go
  gateway/
    cmd/
      main.go
  session/
    handler/
    domain/
    runtime/mongo/
  proxy/
    handler/
    service/
    runtime/
      mongo/
      agentclient/
      picker/
  agent/
    handler/
    domain/
    runtime/
```

`proxy/runtime/stream` 迁移到 `projects/game/pkg/bind` 后删除。

## 关键链路设计

### gateway WebSocket 连接

```text
HTTP WS /api/v1/sessions/{session_id}/agent/connect
  -> phttp.Handler/otelhttp 提取或创建 OTel context
  -> websocket.Accept
  -> game.NewProxyServiceClient(proxyConn).ConnectAgent(r.Context())
  -> gateway 内部 WebSocket AgentFrameStream adapter
  -> bind.Binder.Bind(ctx, wsStream, proxyGrpcStream)
```

说明：

1. 不使用 `tracecontext` 包。
2. OTel 上下文来自外层 HTTP handler。
3. gRPC client interceptor 负责将 context 传播到 proxy。
4. gateway handler 负责 WebSocket JSON frame 与 `AgentFrame` 的转换，转换时使用 `protojson.UnmarshalOptions{DiscardUnknown: true}`。
5. WebSocket 和 gRPC stream 生命周期由同一个 binder 管理。

### proxy ConnectAgent

```text
ProxyService.ConnectAgent stream
  -> service 读取首帧获取 session_id
  -> owner store 查询 owner
  -> agentclient.Manager.Get(owner_index)
  -> owner agent.Connect(ctx)
  -> bind.WithFirstFrame(proxyStream, firstFrame)
  -> bind.Binder.Bind(ctx, prefixedProxyStream, agentGrpcStream)
```

说明：

1. 首帧读取仍由 service 封装，不放到 handler。
2. 首帧不由 service 手写转发，而是通过 `WithFirstFrame` 重新进入统一 binder。
3. 不再需要 `grpcServerStream` 或 `grpcClientStream`。

### agent Connect

```text
AgentService.Connect stream
  -> handler Recv AgentFrame
  -> handler 根据 frame 调用 runtime domain 方法
  -> handler 将 runtime 结果转换为 AgentFrame
  -> handler Send AgentFrame
```

说明：

1. handler 不再用一层 gRPC adapter 把 stream 原样丢给 runtime。
2. runtime 不依赖 gRPC stream 类型，也不处理协议传输细节。
3. 当前 step1 只需支持 status/echo 连通验证。

### agent client 刷新

```text
bootstrap.Daemon("agentclient-manager", builder)
  -> worker Start
  -> initial manager.refresh(ctx)
  -> ticker periodic refresh
  -> resolver.Resolve(agent target)
  -> add new clients / close removed clients
  -> manager.Get/List serve proxy requests
```

## 实施步骤

### Step 1: 新增公共常量包

1. 创建 `projects/game/pkg/gameconst`。
2. 迁移 target、resource name、log field 常量。
3. 更新各服务引用。

验收：硬编码 target 字符串和公共日志字段不再散落在各服务中。

### Step 2: 新增公共 bind 包

1. 将 `proxy/runtime/stream` 的 binder 迁移到 `projects/game/pkg/bind`。
2. 增加 `WithFirstFrame`。
3. 保持 `bind` 包只包含 stream 抽象、binder 和首帧回放，不包含 WebSocket 或其他具体 stream 实现。
4. 迁移原 binder 单测。

验收：bind 包单测覆盖 clean close、错误传播、context cancel 和首帧回放。

### Step 3: 改造 gateway WebSocket handler

1. handler 保留路径解析和 `websocket.Accept`。
2. 在 gateway 内部实现 WebSocket 到 `bind.AgentFrameStream` 的 adapter。
3. 使用 `bind.NewBinder().Bind(...)` 连接 WebSocket stream 与 proxy gRPC stream。
4. 按 bind 返回错误映射 WebSocket close code。

验收：WebSocket unknown field、非法 JSON、status/echo 往返测试通过，且 handler 不再手写两个方向的转发循环。

### Step 4: 改造 proxy ConnectAgent

1. 引用 `projects/game/pkg/bind`。
2. 删除 `grpcServerStream`、`grpcClientStream`。
3. 使用 generated stream 直接作为 `bind.AgentFrameStream`。
4. 首帧通过 `bind.WithFirstFrame` 回放。

验收：proxy ConnectAgent 测试仍覆盖首帧读取、owner 查询、agent stream 创建、首帧和后续 frame 转发。

### Step 5: 改造 agent handler/runtime 边界

1. 删除 `grpcAgentStream`。
2. 调整 runtime/domain 接口，使 handler 调用 domain 语义方法。
3. handler 内完成 `AgentFrame` 与 runtime 输入/输出的转换。
4. 保持当前 status/echo 行为不变。

验收：agent runtime 不依赖 gRPC stream；Connect 行为测试仍覆盖 status/echo。

### Step 6: 改造 agentclient manager daemon

1. manager 移除 `Name/Stage/Start/Stop` component 实现。
2. 新增 refresher worker/builder 或 `NewDaemon` factory。
3. proxy main 注册 `bootstrap.Daemon` 返回的 component。
4. `newClient` 改包级 `newAgentClient`。
5. 更新 manager tests。

验收：manager 查询/刷新测试通过；daemon worker 初始 refresh、周期 refresh、Stop/Close 测试通过。

### Step 7: 移除 session domain Name

1. `domain.Session` 删除 `Name`。
2. repository `Create/Get/Delete` 只处理 session_id。
3. handler 负责解析/生成 resource name。
4. 更新 session handler 和 mongo repository tests。

验收：Mongo document 不保存 name；proto 返回仍包含正确 `sessions/{session_id}`。

## 测试计划

### 单元测试

至少覆盖：

1. `pkg/bind` 双向转发 clean close、错误传播、context cancel。
2. `pkg/bind` 首帧回放。
3. gateway WebSocket adapter：未知 JSON 字段可解析，非法 JSON 返回协议错误。
4. gateway WebSocket handler：status/echo 往返、非法 JSON close、unknown field 成功。
5. proxy ConnectAgent：首帧读取后统一 bind 转发。
6. agentclient manager：新增 client、复用 client、移除 stale client、Close 清理。
7. agentclient daemon worker：初始 refresh 失败触发 restart error，周期 refresh 失败保留旧 clients。
8. session：domain 无 Name，handler 生成 proto name，DeleteSession 继续向 proxy 删除 agent。
9. agent handler：Connect 不直接转交 gRPC stream，status/echo 行为不变。

### 构建验证

完成代码修改后执行：

```bash
bazel run @rules_go//go -- fmt <changed go files>
bazel run //:gazelle projects/game
bazel test //projects/game/...
bazel build //projects/game/...
```

### 系统测试

如进入服务级验收，执行 game testplan，并重点验证：

1. WebSocket status/echo 往返。
2. WebSocket frame 带未知字段仍可连接并收到响应。
3. 非法 JSON 不 fallback 为 echo，连接按协议错误关闭。
4. DeleteAgent 清理 owner agent runtime 状态和 proxy owner 记录。
5. DeleteSession 向下传播 DeleteAgent，不留下 orphan owner/runtime 状态。

## 风险与注意事项

1. `bootstrap.Daemon` 的 worker `Start` 是阻塞式方法，agentclient refresher worker 需要在 `Start` 内运行循环，不能再像 component `Start` 一样只启动 goroutine 后立即返回。
2. 周期 refresh 失败不应清空已有 clients，否则短暂 resolver 故障会导致所有请求失败。
3. WebSocket handler 不应绕过 `phttp.Handler`，否则 OTel HTTP span/context 无法进入 `r.Context()`。
4. bind 包不要直接依赖 gateway/proxy handler 包，避免公共包反向依赖服务入口。
5. bind 包不要包含 WebSocket adapter，具体 stream 实现留在对应服务边界内。
6. 移除 session domain `Name` 时要同步更新测试，否则容易出现 proto name 缺失但 repository 测试仍通过的假阳性。

## 决策记录

1. 公共常量包命名为 `gameconst`，不使用 `const`。
2. gateway 不引入 `tracecontext` 包，依赖 `phttp.Handler` 与 gRPC OTel interceptor 传播上下文。
3. generated gRPC stream 直接满足 `AgentFrameStream`，不再创建重复 wrapper。
4. WebSocket 需要 adapter，因为其传输格式是 protojson text frame；adapter 属于 gateway 边界，不属于 bind 包。
5. `agentclient.Manager` 使用 `bootstrap.Daemon` 风格注册后台刷新。
6. 初始 refresh 失败走 daemon restart；周期 refresh 失败记录日志并继续保留旧 clients。
7. agent handler 必须完成 gRPC 协议到 domain 方法的转换，runtime 不接收 gRPC stream。
