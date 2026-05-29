# Agent 玩游戏 step1 改进方案

本文档是 `design/game-agent-step1.md` 的改进版，保留原 step1 的目标：交付可部署、可测试、可连通的 `gateway`、`session`、`proxy`、`agent` 四服务空壳系统；同时修正实现评审中发现的兼容性、命名、runtime 分层、连接生命周期和删除传播问题。

## 改进目标

在原方案基础上新增以下要求：

1. `gateway` WebSocket 使用 `github.com/coder/websocket`，并使用 `protojson.UnmarshalOptions{DiscardUnknown: true}` 解析 `AgentFrame`。
2. WebSocket 转 gRPC stream 时显式保留 trace context，并将请求上下文传递到 `proxy.ConnectAgent`。
3. `AgentService.InitAgent` 改为 `AgentService.CreateAgent`，采用标准资源创建命名。
4. `proxy` 不在每个请求中创建/关闭 agent gRPC client；改由 runtime 统一管理 agent clients，并定期刷新。
5. `proxy` 的 owner 选择基于 runtime 中当前可用 agent client 数量计算，不在每次创建 agent 时重复运行 resolver。
6. `proxy` 的 runtime 按具体依赖拆分子目录，例如 `runtime/mongo`、`runtime/agentclient`、`runtime/stream`。
7. `proxy.ConnectAgent` handler 不承载双向 stream 绑定细节；runtime 提供两个 stream 实例绑定能力。
8. `ConnectAgent` 的 `session_id` 可以从首帧获取，但首帧不应在 handler 中被单独处理或单独转发；首帧读取、路由和继续转发由 stream binder 统一封装。
9. `DeleteAgent` 必须向 owner agent 传播删除，清理 agent runtime 状态后再删除 proxy owner 记录。
10. `session` 的 Mongo 实现下沉到 `runtime/mongo`。
11. session 存储模型只保存 `SessionID` 和必要时间字段，不额外保存可派生的 `name`。
12. `DeleteSession` 必须向下传播删除 agent，避免 proxy owner 记录和 agent runtime 状态孤儿化。

## 非目标

仍然不包含：

1. 真实 agent 游戏策略、LLM、DeepAgent 或 LangGraph 集成。
2. 复杂负载调度、心跳、容量管理。
3. 多租户、鉴权、限流。
4. MongoDB 持久化卷。
5. grpc-gateway 对 stream 的转换。

## 服务职责调整

### gateway

`gateway` 继续作为统一 HTTP/WebSocket 入口。

改进要求：

1. 普通 unary HTTP/JSON 请求继续由 grpc-gateway 转为 gRPC。
2. WebSocket 请求由 `github.com/coder/websocket` 处理。
3. WebSocket frame 与 `AgentFrame` 使用 protojson 直接转换，不引入额外 DTO 或 fallback 映射。
4. 反序列化使用：

```go
protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(payload, frame)
```

5. 反序列化失败视为协议错误，关闭连接或返回明确错误，不将未知 JSON 静默包装成 echo frame。
6. 创建 `proxy.ConnectAgent` stream 时使用 HTTP 请求上下文携带的 trace context；如上下文中没有有效 trace context，应通过仓库 trace context 工具确保生成并传递。

### session

`session` 继续负责 session 控制面。

改进要求：

1. `CreateSession` 持久化最小 session 数据。
2. 存储层只保存 `session_id` 和必要时间字段。
3. `name = sessions/{session_id}` 只在 API/proto 转换层生成，不作为 Mongo 持久化字段。
4. `DeleteSession` 删除 session 前或删除过程中必须向 `proxy.DeleteAgent` 传播删除。
5. 如果 session 下没有 agent，`proxy.DeleteAgent` 的 NotFound 可按幂等删除处理，不阻塞 session 删除。

### proxy

`proxy` 继续负责 agent 资源控制面、owner 路由和 stream 转发。

改进要求：

1. `CreateAgent` 不直接调用 resolver；从 runtime agent client manager 获取当前 agent client 列表或数量。
2. owner 选择使用 `hash(session_id) % len(agent_clients)`，再映射到对应 owner index。
3. agent gRPC client 由 runtime 管理，按 agent instance 缓存和复用。
4. runtime 定期刷新 agent 实例列表，并增删对应 client。
5. `ConnectAgent` handler 只做协议入口和错误映射，具体 owner 查询、agent stream 创建、首帧读取、首帧路由和双向绑定由 runtime/service 封装。
6. `DeleteAgent` 必须先读取 owner，调用 owner agent 的删除接口，再删除 owner store。若 agent 删除返回 NotFound，可视为已清理。

### agent

`agent` 继续作为 stateful 空壳服务。

改进要求：

1. 将 `InitAgent` 改为 `CreateAgent`。
2. 新增 `DeleteAgent`，用于清理该 agent 实例内指定 session 的 runtime 状态。
3. `GetAgentStatus` 和 `Connect` 保持不变。
4. `Connect` 仍只支持 step1 的 status/echo 连通验证。

## API 设计

### ProxyService

```proto
service ProxyService {
  rpc CreateAgent(CreateAgentRequest) returns (Agent);
  rpc GetAgent(GetAgentRequest) returns (Agent);
  rpc DeleteAgent(DeleteAgentRequest) returns (google.protobuf.Empty);
  rpc ConnectAgent(stream AgentFrame) returns (stream AgentFrame);
}
```

### AgentService

```proto
service AgentService {
  rpc CreateAgent(CreateAgentRequest) returns (AgentStatus);
  rpc DeleteAgent(DeleteAgentRequest) returns (google.protobuf.Empty);
  rpc GetAgentStatus(GetAgentStatusRequest) returns (AgentStatus);
  rpc Connect(stream AgentFrame) returns (stream AgentFrame);
}
```

说明：

1. `ProxyService.CreateAgent` 是对外资源编排接口，负责 owner 选择、agent runtime 创建和 owner 持久化。
2. `AgentService.CreateAgent` 是面向具体 owner instance 的 runtime 创建接口。
3. 两个方法同名但处于不同 service namespace，符合标准资源命名。
4. `AgentService.DeleteAgent` 是 runtime cleanup 接口，供 `ProxyService.DeleteAgent` 调用。

## 数据模型

### Session proto

proto 对外模型保持：

```proto
message Session {
  string name = 1;
  string session_id = 2;
  google.protobuf.Timestamp create_time = 3;
}
```

### Session Mongo document

Mongo 持久化模型调整为：

```go
type sessionDocument struct {
    SessionID  string    `bson:"session_id"`
    CreateTime time.Time `bson:"create_time"`
}
```

`name` 不入库，由转换层根据 `SessionID` 生成。

### Agent owner document

proxy owner 记录保持最小 owner 信息：

```go
type agentOwnerDocument struct {
    SessionID  string    `bson:"session_id"`
    OwnerIndex int       `bson:"owner_index"`
    Owner      string    `bson:"owner"`
    CreateTime time.Time `bson:"create_time"`
}
```

建议为 `session_id` 建唯一索引，避免并发 `CreateAgent` 下先查后插的竞态。

## 目录结构

```text
projects/game/
  game.proto
  gateway/
    cmd/
      main.go
    service.yaml
  session/
    cmd/
      main.go
    handler/
    domain/
    runtime/
      mongo/
        repository.go
        model.go
    service.yaml
  proxy/
    cmd/
      main.go
    handler/
    domain/
    runtime/
      mongo/
        owner_store.go
        model.go
      agentclient/
        manager.go
        client.go
      stream/
        binder.go
      picker/
        hash_picker.go
    service.yaml
  agent/
    cmd/
      main.go
    handler/
    domain/
    runtime/
      simple_runtime.go
    service.yaml
  testplan/
```

## Runtime 设计

### proxy runtime/agentclient

提供 agent client manager：

```go
type Manager interface {
    Get(ctx context.Context, ownerIndex int) (Client, error)
    List(ctx context.Context) ([]ClientRef, error)
    Close() error
}

type ClientRef struct {
    OwnerIndex int
    Owner      string
    Client     Client
}

type Client interface {
    CreateAgent(ctx context.Context, req *game.CreateAgentRequest) (*game.AgentStatus, error)
    DeleteAgent(ctx context.Context, req *game.DeleteAgentRequest) (*emptypb.Empty, error)
    GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error)
    Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error)
}
```

manager 负责：

1. 定期调用 resolver 获取 stateful agent 实例列表。
2. 为每个 instance 维护长期复用的 gRPC client/connection。
3. 移除不存在 instance 的 client。
4. 暴露当前 client 列表给 owner picker 使用。

### proxy runtime/picker

owner picker 不直接依赖 resolver：

```go
type OwnerPicker interface {
    Pick(ctx context.Context, sessionID string, clients []agentclient.ClientRef) (agentclient.ClientRef, error)
}
```

选择策略：

```text
owner = clients[hash(session_id) % len(clients)]
```

### proxy runtime/stream

stream binder 封装两个 stream 的绑定：

```go
type AgentFrameStream interface {
    Recv() (*game.AgentFrame, error)
    Send(*game.AgentFrame) error
}

type Binder interface {
    Bind(ctx context.Context, left AgentFrameStream, right AgentFrameStream) error
}
```

`ConnectAgent` 的 `session_id` 来源规则：

1. 可以从 gateway 发来的首帧 `AgentFrame.session_id` 获取。
2. handler 不单独读取首帧、不单独写首帧转发逻辑。
3. runtime/service 封装首帧读取：读取首帧后确定 owner，打开 agent stream，并把该首帧作为普通 stream 数据进入统一 binder 流程。
4. 首帧之后的所有 frame 由同一个 binder 逻辑处理。

## 请求链路

### 创建 agent

```text
HTTP POST /api/v1/sessions/{session_id}/agent
  -> gateway grpc-gateway
  -> proxy.CreateAgent
  -> proxy agentclient.Manager.List
  -> proxy picker.Pick(session_id, clients)
  -> proxy 调用 owner agent.CreateAgent
  -> proxy 写入 Mongo game_proxy.agent_owners
```

### WebSocket 连接

```text
WS /api/v1/sessions/{session_id}/agent/connect
  -> gateway coder/websocket handler
  -> gateway 使用 protojson DiscardUnknown 解析 AgentFrame
  -> gateway 建立 proxy.ConnectAgent gRPC stream，并传递 trace context
  -> proxy runtime/service 从首帧读取 session_id
  -> proxy 查询 owner
  -> proxy agentclient.Manager.Get(owner_index)
  -> proxy 建立 owner agent.Connect gRPC stream
  -> proxy stream binder 统一绑定 gateway stream 与 agent stream
```

### 删除 agent

```text
HTTP DELETE /api/v1/sessions/{session_id}/agent
  -> gateway grpc-gateway
  -> proxy.DeleteAgent
  -> proxy 查询 owner
  -> proxy 调用 owner agent.DeleteAgent
  -> proxy 删除 Mongo owner 记录
```

错误处理建议：

1. owner 不存在：返回 NotFound。
2. agent runtime 删除 NotFound：可视为成功清理，继续删除 owner。
3. agent 调用失败且不是 NotFound：返回错误，不删除 owner，保留后续重试定位能力。

### 删除 session

```text
HTTP DELETE /api/v1/sessions/{session_id}
  -> gateway grpc-gateway
  -> session.DeleteSession
  -> session 调用 proxy.DeleteAgent
  -> proxy 调用 owner agent.DeleteAgent
  -> proxy 删除 owner 记录
  -> session 删除 Mongo session 记录
```

错误处理建议：

1. proxy.DeleteAgent 返回 NotFound：视为 session 下无 agent，继续删除 session。
2. proxy.DeleteAgent 其他错误：阻止 session 删除，避免 session 删除后留下不可追踪的 agent 状态。
3. session Mongo 删除 NotFound：返回 NotFound。

## Gateway WebSocket 细节

1. 使用 `coder/websocket.Accept` 接受连接。
2. 每次读取 frame 后使用 `protojson.UnmarshalOptions{DiscardUnknown: true}` 解析为 `game.AgentFrame`。
3. 不支持非 `AgentFrame` JSON 的 echo fallback。
4. 写回时直接 `protojson.Marshal` `AgentFrame`。
5. 使用请求 context 创建 gRPC stream，保证 HTTP trace context 传递到 proxy。
6. 连接关闭、context canceled、io EOF 作为正常结束处理。

## 测试方案

### 单元测试

至少覆盖：

1. gateway WebSocket handler 对未知 JSON 字段不报错。
2. gateway WebSocket handler 对非法 JSON 返回协议错误，不 fallback。
3. proxy owner picker 基于 client list 数量选择 owner，不调用 resolver。
4. agentclient manager 刷新 instance 后复用/关闭 client。
5. proxy `CreateAgent` 调用 owner `AgentService.CreateAgent` 并持久化 owner。
6. proxy `DeleteAgent` 先调用 owner `AgentService.DeleteAgent` 再删除 owner store。
7. proxy `ConnectAgent` handler 不包含首帧转发细节，stream binder 负责首帧和后续帧统一转发。
8. session Mongo document 不保存 `name`，proto 返回时正确生成 `sessions/{session_id}`。
9. session `DeleteSession` 调用 proxy `DeleteAgent`，并正确处理 NotFound 幂等语义。

### 系统测试

沿用原 testplan，并新增/强化：

1. WebSocket frame 携带未知字段仍可连接并收到响应。
2. WebSocket 非法 JSON 不被包装为 echo。
3. 删除 session 后，agent 查询返回 NotFound 或预期错误。
4. 删除 session 后，proxy owner store 不保留该 session 的 owner 记录。
5. 删除 agent 后，owner agent runtime 状态被清理。

## 验收标准

1. `bazel build //projects/game/...` 成功。
2. `bazel test //projects/game/...` 成功。
3. 相关 testplan 系统测试通过。
4. WebSocket 连接在真实部署环境中可完成 status/echo 往返。
5. DeleteAgent 和 DeleteSession 不留下 proxy owner 或 agent runtime 孤儿状态。
6. gateway 可以接受包含未知字段的 `AgentFrame` JSON。
7. proxy 在高频 Create/Get/Connect 下不会每请求创建新的 agent gRPC connection。

## 待决策项

无。以下决策已确认：

1. `AgentService.InitAgent` 改为 `CreateAgent`。
2. `DeleteAgent` 和 `DeleteSession` 都需要向下传播删除。
3. proxy 引入 runtime 级 agent client manager 并定期刷新。
4. `ConnectAgent` 的 `session_id` 可以从首帧获取，但首帧处理必须封装，不在 handler 中单独转发。
5. session Mongo 存储只保存 `SessionID`，不保存派生字段 `name`。
6. gateway 取消非法 JSON echo fallback。
