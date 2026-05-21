# game gateway 与 runtime 架构迁移方案

## 背景

当前 game 系统已经将 gateway 服务改为 `aggregate` 暴露模式，目标是让 game 系统共同使用一个公网域名：

```text
game.liukexin.com
```

但当前 `session` 与 `gateway` 服务仍各自提供 HTTP 入口，各自承担一部分路由与接口暴露职责：

* `session` 通过 HTTP grpc-gateway 暴露 `/v1/sessions/**`。
* `gateway` 同时承担 WebSocket、`GameGatewayService`、`GameRuntimeService`、owner proxy 与游戏运行态。
* 系统级大型测试分散在 `projects/game/testplan`，gateway 自身大型测试在 `projects/game/gateway/testplan`。

这会导致统一域名下的职责边界不清晰：公网入口、session 控制面、game runtime 运行态、owner routing 混在不同服务中，后续难以扩展和测试。

本方案以“公网 edge gateway + 内部 session + 有状态 runtime”的边界重新收敛架构。

相关基础能力参考：

* `design/deploy_stateful_aggregate_exposure.md`
* `design/stateful_service_discovery.md`
* `design/guitar_yaml_testplan.md`

## 目标

本次迁移要达成的效果：

* game 系统只通过 `game.liukexin.com` 暴露公网入口。
* `gateway` 成为纯 edge 服务，负责 grpc-gateway 聚合、WebSocket 入口和 owner proxy，不持有业务 proto 与游戏运行态。
* `runtime` 成为新的有状态游戏运行态服务，负责 WebSocket 实际处理、游戏 runtime 内存状态、media/control/snapshot 能力。
* `session` 彻底内部化，只提供 gRPC 服务，不再提供 HTTP 入口。
* owner 字段从 `owner_gateway_id` 重命名为 `owner_runtime_id`，owner 语义明确指向 runtime 实例。
* 系统级大型测试统一由 `projects/game/gateway/testplan` 承担。
* 不保留新旧双跑过渡路径，迁移后旧 gateway 运行态代码从 gateway 目录移出。

## 非目标

本方案不覆盖：

* token audience 拆分。本次先继续使用单一 audience 语义，避免引入额外权限面迁移。
* 新 gateway 自有业务 proto。gateway 不定义业务 RPC，只引用 session/runtime proto 做聚合与路由。
* runtime 共享存储。runtime 的 session runtime、media cache、inflight operation 仍为 owner runtime 本地内存。
* owner 主动迁移、热接管、旧 owner drain。
* service mesh、mTLS、NetworkPolicy。
* 向后兼容旧 `owner_gateway_id` 字段或旧 gateway proto。

## 最终服务模型

### `projects/game/gateway`

`gateway` 是公网 edge 服务。

职责：

* 暴露 `game.liukexin.com`。
* 注册 session proto 的 grpc-gateway HTTP handler。
* 注册 runtime proto 中 `GameGatewayService` 的 grpc-gateway HTTP handler。
* 处理 WebSocket 公网入口：

  ```text
  GET /v1/sessions/{session}/game/connect?token=...
  ```

* 校验 token，读取 `owner_runtime_id`。
* 将 WebSocket、runtime read API 请求转发到 owner runtime。
* 不持有 `gateway.proto`，不定义业务 proto，避免聚合层与后端服务契约不一致。
* 不持有 game runtime 内存状态、media cache、control executor。

部署建议：

```yaml
version: "3.0"
name: gateway
app: game
kind: stateless
artifacts:
  - name: cmd
    ports:
      - name: http
        port: 8080
```

`gateway` 可以横向扩容，任意实例都只承担协议入口与转发职责。

### `projects/game/runtime`

`runtime` 是新的有状态游戏运行态服务。

职责：

* 管理每个 session 的本地 `SessionRuntime`。
* 维护 agent/web WebSocket 实际连接。
* 管理 media cache、snapshot、control executor、async routing worker。
* 提供 runtime proto 定义的 3 类入口：
  * WebSocket 内部入口。
  * `GameGatewayService`：对 gateway 暴露 runtime read API。
  * `GameRuntimeService`：对 session 暴露 runtime lifecycle RPC。
* 使用 `HOSTNAME` 作为 `runtime_id`，写入 token 的 `owner_runtime_id`。

部署建议：

```yaml
version: "3.0"
name: runtime
app: game
kind: stateful
exposure: aggregate
artifacts:
  - name: cmd
    ports:
      - name: http
        port: 8080
      - name: grpc
        port: 8082
```

`runtime` 仍是 stateful，因为 owner runtime 的内存状态与连接状态绑定到具体实例。

### `projects/game/session`

`session` 是内部 control-plane 服务。

职责：

* 提供 `SessionService` gRPC 服务。
* 管理 session 持久化。
* 创建 session 时调用 runtime `GameRuntimeService.CreateGameRuntime`。
* reconnect 时调用 runtime `GameRuntimeService.RefreshGameRuntime`，必要时重新创建 runtime。
* 不再提供 HTTP grpc-gateway 入口。

部署建议：

```yaml
version: "3.0"
name: session
app: game
kind: stateless
artifacts:
  - name: cmd
    ports:
      - name: grpc
        port: 8081
```

公网 `/v1/sessions/**` 由 gateway 注册 session proto 的 grpc-gateway handler 后暴露。

## Proto 模型

### runtime proto

新建：

```text
projects/game/runtime/runtime.proto
```

建议 proto package：

```proto
package projects.game.runtime;

option go_package = "dominion/projects/game/runtime";
```

保留服务名：

```proto
service GameGatewayService {
  rpc GetGameSnapshot(GetGameSnapshotRequest) returns (GameSnapshot) {
    option (google.api.http) = {
      get: "/v1/{name=sessions/*/game/snapshot}"
    };
  }

  rpc GetGameRuntime(GetGameRuntimeRequest) returns (GameRuntime) {
    option (google.api.http) = {
      get: "/v1/{name=sessions/*/game/runtime}"
    };
  }
}

service GameRuntimeService {
  rpc CreateGameRuntime(CreateGameRuntimeRequest) returns (CreateGameRuntimeResponse);
  rpc RefreshGameRuntime(RefreshGameRuntimeRequest) returns (RefreshGameRuntimeResponse);
}
```

其中 `GameGatewayService` 表示 runtime 向 edge gateway 暴露的 read-side 能力，`GameRuntimeService` 表示 session 控制面对 runtime 的 lifecycle 能力。

### gateway proto

gateway 不新建业务 proto。

原因：

* gateway 是聚合层，不是业务契约所有者。
* session 与 runtime 的 proto 已分别定义资源与 RPC。
* gateway 引用其他服务 proto 注册 grpc-gateway handler，可以避免重复定义 HTTP contract 后出现不一致。

如果后续需要 gateway 自身健康检查或诊断接口，应单独设计非业务 proto，不与 session/runtime API 混合。

### session proto

`projects/game/session/session.proto` 保持为 session 资源契约，但服务实现从 HTTP grpc-gateway 改为内部 gRPC。

字段调整：

* 保留 `gateway_id` 需要重新评估命名，建议迁移为 `runtime_id` 或 `owner_runtime_id`。
* `agent_connect_url` 已标记 deprecated，目标态不再依赖该字段表达固定实例域名。
* `token` 继续作为 session 创建/reconnect 后返回的连接凭据。

## Token 与 owner 模型

### Claims

token claims 中 owner 字段统一改为：

```json
{
  "session_id": "session-1",
  "owner_runtime_id": "runtime-pod-0",
  "owner_epoch": 1,
  "aud": "game-runtime",
  "iat": 1710000000,
  "exp": 1710000900,
  "reconnect_generation": 0
}
```

本次不兼容旧字段：

* 不读取 `owner_gateway_id`。
* 不保留 `gateway_id` 旧 token 语义。
* owner epoch 仍要求大于等于 1。

### owner 决策边界

owner authority 只属于 runtime。

* session 负责请求 runtime 创建或刷新 runtime，并持久化返回的 `owner_runtime_id` 与 token。
* gateway 只根据 token 中的 `owner_runtime_id` 做转发，不创建 runtime，不修正 owner。
* runtime 收到本地请求后再次校验 token 与本实例 `HOSTNAME` 一致，防止错误转发或伪造请求。

这样避免 gateway 与 runtime 同时成为 owner 决策源。

## 路由模型

### 公网路由

所有公网请求进入 gateway：

```text
game.liukexin.com
  -> gateway
```

gateway 按路径和协议分发：

| 请求 | gateway 行为 | 后端 |
| --- | --- | --- |
| `POST /v1/sessions` | grpc-gateway 聚合 | session gRPC |
| `GET /v1/sessions/*` | grpc-gateway 聚合 | session gRPC |
| `POST /v1/sessions/*:reconnect` | grpc-gateway 聚合 | session gRPC |
| `DELETE /v1/sessions/*` | grpc-gateway 聚合 | session gRPC |
| `GET /v1/sessions/*/game/connect` | token owner proxy | runtime owner HTTP/WS |
| `GET /v1/sessions/*/game/runtime` | token owner proxy 或 runtime grpc-gateway handler | runtime owner |
| `GET /v1/sessions/*/game/snapshot` | token owner proxy 或 runtime grpc-gateway handler | runtime owner |

### WebSocket 路径

WebSocket path 保持不变：

```text
GET /v1/sessions/{session}/game/connect?token=...
```

保持该路径可以降低 Windows agent、web client 和大型测试迁移成本。

### runtime owner proxy

gateway 的 owner proxy 流程：

1. 从 query 中读取 token。
2. 验证 token 签名、过期时间与 owner epoch。
3. 读取 `owner_runtime_id`。
4. 使用 deploy stateful resolver 查询 `game/runtime:http` 的实例。
5. 如果 `owner_runtime_id` 对应本次请求应转发到某个 runtime 实例，则 reverse proxy 到该实例内部 endpoint。
6. WebSocket upgrade 请求必须保持 upgrade header 与连接双向转发语义。

runtime 再执行本地校验：

* token session 等于 path session。
* token `owner_runtime_id` 等于 runtime 进程 `HOSTNAME`。
* token owner epoch 合法。

## 代码分层

### gateway

建议分层：

```text
projects/game/gateway/
  app/
    cmd/
    router.go              # path/protocol dispatch
    grpc_gateway.go        # 注册 session/runtime grpc-gateway handlers
  runtime/
    owner/
      owner.go             # owner resolver interface / target decision
      deploy.go            # deploy stateful resolver adapter for runtime
      proxy.go             # reverse proxy construction
  config/
  service.yaml
```

gateway 可以引用：

* `projects/game/session`
* `projects/game/runtime`
* token 包
* solver/deploy resolver

gateway 不引用 runtime 的 domain/service 内部包。

### runtime

建议从当前 gateway 迁移分层：

```text
projects/game/runtime/
  runtime.proto
  app/
    cmd/
  config/
  domain/
  handler.go
  service/
  ws.go
  ws_action.go
  worker.go
  service.yaml
```

从当前 `projects/game/gateway` 迁移的主要内容：

* `domain/`
* `service/`
* `handler.go`
* `ws.go`
* `ws_action.go`
* `ws_validate.go`
* `worker.go`
* token 相关包，或抽到 game 公共包

不迁移到 runtime 的内容：

* edge owner reverse proxy
* gateway 聚合 router
* session grpc-gateway 注册逻辑

### session

建议调整：

```text
projects/game/session/
  app/cmd/main.go       # 启动 gRPC server，不再启动 HTTP server
  runtime/runtimeclient # 或 runtime/gateway 改名为 runtime/client
  service/
  domain/
  runtime/storage/
  service.yaml
```

session 的 runtime client 调用：

```text
game/runtime:grpc
```

不再调用旧 `game/gateway:internal-grpc`。

## 大型测试模型

系统级大型测试全部归属：

```text
projects/game/gateway/testplan/
```

### gateway 系统级 testplan

部署对象：

* gateway
* runtime，replicas >= 3
* session
* mongo

覆盖场景：

1. `CreateSession` 通过 `game.liukexin.com` 成功。
2. session 返回 token，token 中包含 `owner_runtime_id`。
3. Windows agent 使用统一域名 WebSocket 连接成功。
4. Web client 使用统一域名 WebSocket 连接成功。
5. media init/segment 从 agent 转发到 web。
6. `GetGameRuntime` 通过统一域名返回 runtime 状态。
7. `GetGameSnapshot` 通过统一域名返回 snapshot。
8. reconnect 后新 token 可用，旧 token 按 owner epoch/reconnect generation 规则失效。
9. path session 与 token session 不一致时被拒绝。
10. 请求落到非 owner runtime 时，gateway 能转发到 owner runtime。
11. runtime owner pod 不可用时返回明确 503/Unavailable。
12. 不依赖 per-instance public host。

### runtime 服务级 testplan

如需要保留服务级大型测试，可新建：

```text
projects/game/runtime/testplan/
```

覆盖 runtime 自身能力：

* WS protocol validation
* duplicate agent
* media cache / catchup
* control timeout
* snapshot refresh
* idle cleanup

### session testplan

`projects/game/session/testplan` 不再作为公网 HTTP 测试。

可选保留为内部 gRPC contract 测试，或将覆盖下沉到单元测试与 gateway 系统级大型测试。

## 迁移步骤

### Phase 1：新建 runtime proto 与服务目录

1. 新建 `projects/game/runtime/runtime.proto`。
2. 将旧 gateway proto 中 runtime 相关资源复制并重命名 owner 字段。
3. 使用 gazelle 更新 BUILD 文件。
4. 确认生成的 Go proto importpath 为 `dominion/projects/game/runtime`。

验收：

* `bazel build //projects/game/runtime/...` 通过。

### Phase 2：迁移旧 gateway 运行态到 runtime

1. 迁移 WebSocket handler、domain、service、worker。
2. 将 `owner_gateway_id` 全部改为 `owner_runtime_id`。
3. runtime 启动 HTTP/WS 与 gRPC 服务。
4. runtime `HOSTNAME` 作为 runtime id。

验收：

* runtime 单测通过。
* runtime 服务级接口测试可连接 WS、读 runtime、读 snapshot。

### Phase 3：session 改为 gRPC-only 并调用 runtime

1. session app 删除 HTTP server。
2. session app 增加 gRPC server。
3. session runtime client 改为调用 `projects.game.runtime.GameRuntimeService`。
4. session 持久化字段改为 runtime owner 语义。

验收：

* session 单测通过。
* session gRPC handler 可创建、查询、删除、reconnect session。

### Phase 4：gateway 改为 edge 聚合层

1. gateway 删除运行态 domain/service/ws 实现。
2. gateway 注册 session proto grpc-gateway handler。
3. gateway 注册 runtime `GameGatewayService` grpc-gateway handler，或对 runtime read path 做 token-aware owner proxy。
4. gateway 实现 WS owner proxy。
5. gateway owner resolver target 改为 `game/runtime:http`。

验收：

* `/v1/sessions/**` 通过 gateway 访问 session。
* `/v1/sessions/*/game/**` 通过 gateway 访问 owner runtime。
* gateway 不创建 runtime，不持有 runtime state。

### Phase 5：部署与大型测试迁移

1. gateway `service.yaml` 改为公网 stateless edge。
2. runtime 新增 stateful aggregate `service.yaml`。
3. session `service.yaml` 改为内部 gRPC。
4. 合并 `projects/game/testplan` 的系统级用例到 `projects/game/gateway/testplan`。
5. gateway testplan 部署 gateway + runtime + session + mongo。
6. 删除或降级旧 session HTTP testplan。

验收：

* `bazel test //projects/game/...` 通过。
* 使用 `testplan` skill 执行 gateway 系统级大型测试通过。

## 决策记录

| 决策 | 结论 |
| --- | --- |
| 新服务命名 | `runtime` |
| proto 复用 | 不复用旧 gateway proto，新建 runtime proto |
| owner 字段 | `owner_gateway_id` 重命名为 `owner_runtime_id` |
| session HTTP | 删除，不保留公网 HTTP 入口 |
| session RPC | 只提供 gRPC 服务 |
| gateway proto | 不新建业务 proto，只引用 session/runtime proto |
| gateway 类型 | grpc-gateway 聚合 + WS owner proxy |
| token audience | 暂不拆分 |
| 系统级大型测试归属 | `projects/game/gateway/testplan` |
| 迁移策略 | 不双跑，直接迁移到目标架构 |

## 关键风险与约束

### WebSocket reverse proxy

WebSocket 是最高风险部分。gateway 必须正确保留：

* upgrade header
* query token
* close frame
* 长连接生命周期
* backpressure 行为

大型测试必须真实通过公网 gateway WebSocket 入口验证，而不是只测 runtime 本地 handler。

### owner split-brain

gateway 不能成为 owner 决策源。gateway 只能读取 token 并转发，runtime 才是 owner runtime 状态权威。

### 不兼容迁移

本方案不双跑，且不兼容旧 proto 与旧 token 字段。因此开发分支在完成前可能无法部分部署。每个 phase 的单测和 build 必须保持通过，但系统级行为以最终 phase 为验收点。

### grpc-gateway 聚合错误语义

gateway 注册多个后端 proto handler 时，需要保持 session/runtime 的错误码与 HTTP 映射一致，避免 gateway 自己包装错误导致 API contract 改变。

## 未来规划

以下内容不属于本次迁移范围：

* token audience 拆分为 connect/internal 两类。
* owner runtime 主动 drain 与连接迁移。
* runtime 共享状态或跨 runtime 热接管。
* gateway 诊断 API，例如 owner route debug、runtime endpoint debug。
* session proto 字段进一步清理，例如删除 deprecated `agent_connect_url`。
