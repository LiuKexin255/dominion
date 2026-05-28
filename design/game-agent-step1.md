# Agent 玩游戏 step1 方案

## 背景

原始目标见 `ideas/llm_agent_play_game/README.md`。step1 只交付 4 个服务的基本架构、部署与请求连通能力，不实现真实 agent、游戏策略、LLM 调用或 DeepAgent 集成。

## 目标

在 `/projects/game` 下实现一个可部署、可测试、可连通的空壳系统，达成以下效果：

1. `gateway`、`session`、`proxy`、`agent` 4 个服务均可独立构建和部署。
2. 普通请求通过 HTTP 进入 `gateway`，由 grpc-gateway 转为 gRPC 请求。
3. WebSocket 请求由 `gateway` 独立处理，并转换为后端 gRPC stream。
4. `proxy` 能为 agent 资源选择 owner，并将后续请求稳定路由到同一个 owner agent 实例。
5. `session` 和 `proxy` 使用同一个 MongoDB 实例、不同 database 持久化最小数据。
6. `agent` 作为 stateful 服务部署，只提供最小初始化、状态查询和 stream 连通能力。
7. 单服务接口测试和系统测试通过 testplan 编排。

## 非目标

step1 不包含以下内容：

1. LangChain、DeepAgent、LangGraph、Python/FastAPI 服务。
2. LLM 调用、游戏策略、策略总结、策略更新。
3. agent 真实业务状态机。
4. 复杂调度策略、负载感知、心跳或容量管理。
5. MongoDB 持久化卷。
6. 鉴权、限流、配额和多租户隔离。
7. grpc-gateway 对 stream 的转换。

## 技术选型

| 模块 | 选型 | 说明 |
|---|---|---|
| 语言 | Go | step1 全部服务使用 Go，后续可用同一 gRPC contract 替换 agent 语言 |
| RPC | gRPC | 除 WebSocket 入口外，服务间通信使用 gRPC |
| HTTP | grpc-gateway v2 | 只处理 unary HTTP/JSON 请求 |
| Stream | gRPC bidirectional stream | `proxy` 和 `agent` 提供 stream 接口 |
| WebSocket | gateway 原生 HTTP handler | `gateway` 将 WS frame 与 gRPC stream frame 互转 |
| MongoDB | 仓库现有 Mongo 封装 + mongo-driver | `session` 和 `proxy` 使用同实例不同 database |
| 部署 | 仓库 deploy tool | `service.yaml` 放在各服务目录下 |
| 测试 | testplan/guitar | 单服务接口测试和系统测试统一编排 |

## 服务职责

### gateway

`gateway` 是统一对外入口。

职责：

1. 暴露 `game.liukexin.com` 下的 HTTP API。
2. 使用 grpc-gateway 将普通 HTTP/JSON 请求转为 gRPC。
3. 暴露 `/api/v1/sessions/{session_id}/agent/connect` WebSocket 入口。
4. 对 WebSocket 请求建立到 `proxy` 的 gRPC stream，并在 WS frame 和 gRPC frame 之间转发。

`gateway` 不保存业务状态，不直接决定 agent owner。

### session

`session` 是会话控制面。

职责：

1. 创建、查询、删除 session。
2. 将最小 session 资源写入 MongoDB。
3. 通过 gRPC 服务对内提供接口，由 `gateway` 通过 grpc-gateway 对外暴露。

MongoDB 使用：

```text
database: game_session
collection: sessions
```

### proxy

`proxy` 是无状态路由服务。

职责：

1. 处理 agent 资源的 Create/Get/Delete 等控制面请求。
2. CreateAgent 时为 session 选择 owner agent instance。
3. 将 owner 信息写入 MongoDB。
4. 后续请求根据 owner 信息路由到指定 agent instance。
5. 为 `gateway` 提供 gRPC stream，承接 WebSocket 转换后的双向数据流。
6. 与 owner agent 建立 gRPC stream，并在 `gateway` 与 `agent` 之间转发 frame。

MongoDB 使用：

```text
database: game_proxy
collection: agent_owners
```

### agent

`agent` 是 stateful 空壳服务。

职责：

1. 暴露最小 gRPC 接口：初始化 agent、查询状态、建立 stream。
2. 不实现真实 agent 逻辑。
3. stream 接口仅返回 echo/status，用于证明请求连通和 owner 路由正确。
4. 部署为 stateful workload，owner 路由使用仓库 resolver 能力定位指定 instance。

## 模型设计

### Session

step1 仅保留必要字段：

```proto
message Session {
  string name = 1;        // sessions/{session_id}
  string session_id = 2;
  google.protobuf.Timestamp create_time = 3;
}
```

### Agent

`Agent` 表示某个 session 下的 agent 资源和 owner 信息：

```proto
message Agent {
  string name = 1;        // sessions/{session_id}/agent
  string session_id = 2;
  int32 owner_index = 3;
  string owner = 4;
  google.protobuf.Timestamp create_time = 5;
}
```

### AgentFrame

`AgentFrame` 是 WebSocket 和 gRPC stream 之间的最小传输单元：

```proto
message AgentFrame {
  string session_id = 1;
  string type = 2;
  bytes payload = 3;
}
```

step1 中 `type` 仅需要支持 `status` 和 `echo`。`payload` 不定义游戏协议，后续扩展。

## API 设计

资源路径保持：

```text
/api/v1/sessions/{session_id}
/api/v1/sessions/{session_id}/agent
/api/v1/sessions/{session_id}/agent/connect
```

### SessionService

```proto
service SessionService {
  rpc CreateSession(CreateSessionRequest) returns (Session);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc DeleteSession(DeleteSessionRequest) returns (google.protobuf.Empty);
}
```

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
  rpc InitAgent(InitAgentRequest) returns (AgentStatus);
  rpc GetAgentStatus(GetAgentStatusRequest) returns (AgentStatus);
  rpc Connect(stream AgentFrame) returns (stream AgentFrame);
}
```

## 请求链路

### 创建 session

```text
HTTP POST /api/v1/sessions
  -> gateway grpc-gateway
  -> session.CreateSession
  -> Mongo game_session.sessions
```

### 创建 agent

```text
HTTP POST /api/v1/sessions/{session_id}/agent
  -> gateway grpc-gateway
  -> proxy.CreateAgent
  -> proxy 选择 owner_index
  -> proxy 调用 owner agent.InitAgent
  -> proxy 写入 Mongo game_proxy.agent_owners
```

### 查询 agent

```text
HTTP GET /api/v1/sessions/{session_id}/agent
  -> gateway grpc-gateway
  -> proxy.GetAgent
  -> proxy 查询 owner
  -> proxy 调用 owner agent.GetAgentStatus
  -> 返回 owner 信息
```

### WebSocket 连接

```text
WS /api/v1/sessions/{session_id}/agent/connect
  -> gateway WS handler
  -> gateway 建立 proxy.ConnectAgent gRPC stream
  -> proxy 查询 owner
  -> proxy 建立 owner agent.Connect gRPC stream
  -> gateway/proxy/agent 转发 AgentFrame
```

## owner 路由设计

CreateAgent 是无状态请求。`proxy` 负责选择 owner，并持久化 owner 映射。

step1 owner 选择策略：

```text
instances = stateful_resolver.Resolve(agent_target)
owner_index = instances[hash(session_id) % len(instances)].index
```

该策略可重复、易测试，不依赖负载和心跳。agent 实例数量不通过配置写死，而是在 CreateAgent 时通过仓库 resolver 服务发现能力获取当前 agent stateful 实例列表后计算。

后续请求通过 MongoDB 查询 owner：

```text
session_id -> owner_index
```

`proxy` 使用仓库 resolver/pkg/resolver 能力获取 agent stateful 实例列表，并路由到指定 `owner_index`。step1 不解析 pod hostname。

## 代码分层

系统目录：

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
    service.yaml
    handler/
    service/
    domain/
    runtime/
  proxy/
    cmd/
      main.go
    service.yaml
    handler/
    service/
    domain/
    runtime/
  agent/
    cmd/
      main.go
    service.yaml
    handler/
    service/
    domain/
    runtime/
  testplan/
    deploy.yaml
    system_test.yaml
    system_test.go
```

`service.yaml` 放在各服务目录下，不放在 `cmd` 目录。

各服务分层遵循 `styles/golang.md`：

| 层 | 职责 |
|---|---|
| `cmd` | 进程入口、配置读取、bootstrap 组装 |
| `handler` | gRPC proto interface 实现 |
| `service` | 过程式业务逻辑 |
| `domain` | 领域模型和纯逻辑 |
| `runtime` | Mongo、下游 gRPC client、resolver 等外部依赖实现 |

## 关键扩展点

### proxy owner store

```go
type OwnerStore interface {
    Create(ctx context.Context, owner *AgentOwner) error
    Get(ctx context.Context, sessionID string) (*AgentOwner, error)
    Delete(ctx context.Context, sessionID string) error
}
```

### proxy owner picker

```go
type OwnerPicker interface {
    Pick(ctx context.Context, sessionID string) (int, error)
}
```

### agent runtime

```go
type Runtime interface {
    Init(ctx context.Context, sessionID string) (*Status, error)
    Status(ctx context.Context, sessionID string) (*Status, error)
    Connect(stream AgentStream) error
}
```

这些接口用于后续替换调度策略、存储实现和 agent runtime，不在 step1 引入额外复杂度。

## 部署设计

`gateway`、`session`、`proxy` 使用 stateless workload。

`agent` 使用 stateful workload。

MongoDB 在 deploy 配置中作为 infra 声明，step1 不启用持久化：

```yaml
services:
  - infra:
      resource: mongodb
      profile: dev-single
      name: mongo
      app: game
      persistence:
        enabled: false
```

系统域名：

```text
game.liukexin.com
```

HTTPRoute 指向 `gateway` 的 HTTP 端口。

## 测试方案

每个服务包含接口测试。整个系统包含 testplan 系统测试。

系统测试至少覆盖：

1. 创建 session 成功。
2. 创建 agent 成功。
3. MongoDB 中存在 session 和 owner 记录。
4. 查询 agent 返回固定 owner。
5. 多次查询同一 session 的 agent，owner 不变。
6. WebSocket 连接成功。
7. WebSocket 收到来自 owner agent 的 status/echo 响应。
8. 删除 agent/session 后，请求返回预期结果。
9. testplan 完成部署、测试和清理闭环。

测试代码使用 `common/gopkg/testtool` 读取 `TESTTOOL_ENV` 和 `TESTTOOL_ENDPOINT_*`，不使用旧的 `SUT_HOST_URL` / `SUT_ENV_NAME`。

## 决策详情

### 为什么 step1 全部使用 Go

DeepAgent 主要支持 Python，后续真实 agent runtime 很可能需要 Python。但 step1 的目标是服务架构、路由、Mongo 和 stream 连通，不需要真实 agent 能力。使用 Go 可以最大化复用仓库现有 Bazel、gRPC、grpc-gateway、Mongo、deploy 和 testplan 体系。

agent 服务通过 gRPC contract 隔离语言实现，后续可将 `agent` 替换为 Python、TypeScript 或其他语言实现，不影响 `gateway`、`session`、`proxy` 的协议边界。

### 为什么 stream 不走 grpc-gateway

grpc-gateway 对 stream 支持不适合作为本系统的 WebSocket 双向通道。step1 明确采用：

```text
gateway WebSocket <-> proxy gRPC stream <-> agent gRPC stream
```

grpc-gateway 只处理普通 unary HTTP/JSON 请求。

### 为什么 agent 使用 stateful workload

agent 连接具有 owner 语义。CreateAgent 后，后续请求必须回到同一个 agent instance。stateful workload 提供稳定实例身份，proxy 通过 resolver 路由到指定 owner instance。

### 为什么 owner 选择使用 hash

step1 不需要复杂调度。CreateAgent 时先通过 `common/gopkg/solver.StatefulResolver` 发现 agent stateful 实例列表，再使用 `hash(session_id) % len(instances)` 选择 owner。这样不需要配置写死副本数，同时选择结果可重复、可测试，足以验证 owner 路由链路。后续可在 `OwnerPicker` 中替换为负载感知策略。

## 未来规划

以下内容不属于 step1，但设计为后续扩展保留空间：

1. 将 `agent` 服务替换为 Python + DeepAgent/LangGraph 实现。
2. 引入真实游戏输入/输出协议。
3. 引入 agent 策略总结和策略更新。
4. 引入 agent 心跳、负载上报和容量感知调度。
5. 引入鉴权、限流、审计和多租户隔离。
6. 为 MongoDB 启用持久化存储。

## 待定项

无。
