# session service 与 game gateway 正式连接方案

## 背景

当前 `projects/game/session` 与 `projects/game/gateway` 均已实现，但 `session service` 返回的 gateway 连接地址仍是临时格式：

```text
wss://{gateway_id}.{gateway_domain}/connect?token=...
```

而 `game gateway` 代码中已经落地的正式 WebSocket 入口是：

```text
GET /v1/sessions/{session}/game/connect?token=...
```

本方案以现有代码为准，设计文档仅作为意图参考。目标是让 `session service` 返回的连接 URL 能直接连接到被分配的有状态 `game gateway` 实例，并保持 token 与 gateway 校验逻辑一致。

## 目标

* `session service` 使用正式 gateway WebSocket 路径生成连接 URL。
* gateway 实例选择基于 `pkg/solver.DeployStatefulResolver`，不再依赖临时的手工 gateway ID 列表。
* token 中的 `gateway_id` 与 gateway 进程看到的 `HOSTNAME` 保持一致。
* 对外连接域名按 deploy 为有状态服务实例生成的固定域名规则拼接。
* 保持现有 API 字段 `agent_connect_url` 不变，客户端将其视为 opaque URL。

## 当前代码契约

### gateway 侧

已实现契约如下：

* WebSocket 路径：`/v1/sessions/{session}/game/connect`
  * 代码位置：`projects/game/gateway/ws.go`
* gateway ID 来源：进程环境变量 `HOSTNAME`
  * 代码位置：`projects/game/gateway/app/cmd/main.go`
* token 校验字段：
  * `session_id`
  * `gateway_id`
  * `exp`
  * `reconnect_generation`
  * 代码位置：`projects/game/pkg/token/token.go`
* gateway 建连时校验：
  * token 签名与过期时间合法
  * token 中 `gateway_id` 等于当前 gateway 的 `HOSTNAME`
  * token 中 `session_id` 等于 URL path 中的 `{session}`

### session 侧

当前临时代码如下：

* gateway 选择：`session/runtime/gateway.StaticRegistry` 从静态 ID 列表随机选择。
* URL 生成：`projects/game/session/service/service.go` 中 `buildConnectURL` 拼接 `/connect`。
* 配置来源：`GAME_GATEWAY_IDS` 与 `GAME_GATEWAY_DOMAIN`。

这些都应收敛为正式实现。

## 正式方案

### gateway 实例发现

`session service` 启动后创建 `pkg/solver.DeployStatefulResolver`：

```go
resolver, err := solver.NewDeployStatefulResolver()
target, err := solver.ParseTarget("game/gateway:http")
instances, err := resolver.Resolve(ctx, target)
```

`DeployStatefulResolver` 依赖环境变量：

```text
DOMINION_ENVIRONMENT={scope}.{envName}
```

解析结果中的 `StatefulInstance` 包含：

* `Index`：有状态服务实例序号。
* `Hostname`：Pod hostname，与 gateway 进程内 `HOSTNAME` 一致。
* `Endpoints`：ready endpoint，格式为 `ip:port`。

本方案中：

* `StatefulInstance.Hostname` 用作 session 中保存的 `gateway_id`。
* `StatefulInstance.Hostname` 也写入 token 的 `gateway_id` claim。
* `StatefulInstance.Index` 用于拼接 deploy 为每个 gateway 实例配置的外部域名。
* `StatefulInstance.Endpoints` 只作为实例 ready 判断和必要时的诊断信息，不直接作为浏览器/agent 连接 URL。

### 外部域名生成

deploy 部署工具会为有状态服务的每个节点配置固定外部域名。session service 不再使用 `ip:port` 生成连接 URL，而是按实例序号拼接外部域名。

建议新增配置：

```text
GAME_GATEWAY_PUBLIC_HOST_PATTERN=gateway-%d-game.liukexin.com
```

其中 `%d` 使用 `StatefulInstance.Index` 替换。

示例：

| Index | Hostname | Public host |
|---|---|---|
| `0` | `game-gateway-0` | `gateway-0-game.liukexin.com` |
| `1` | `game-gateway-1` | `gateway-1-game.liukexin.com` |

这样可以同时满足：

* token 鉴权绑定 gateway 内部身份：`game-gateway-0`
* 客户端连接外部域名：`gateway-0-game.liukexin.com`

### 连接 URL 生成

创建 session 或 reconnect 时，流程如下：

1. 通过 resolver 获取 gateway 实例列表。
2. 随机选择一个 ready 实例；reconnect 时优先排除当前 `gateway_id`。
3. 将 `instance.Hostname` 写入 session 的 `gateway_id`。
4. 用 `token.Issuer.Issue(sessionID, instance.Hostname, reconnectGeneration)` 签发 token。
5. 使用 `instance.Index` 生成 public host。
6. 返回完整连接 URL。

URL 格式：

```text
wss://{public_host}/v1/sessions/{session_id}/game/connect?token={escaped_token}
```

示例：

```text
wss://gateway-0-game.liukexin.com/v1/sessions/session-123/game/connect?token=...
```

注意：

* `{session_id}` 使用裸 ID，例如 `session-123`，不是资源名 `sessions/session-123`。
* token 必须使用 `url.QueryEscape` 或 `net/url` 写入 query，避免裸字符串拼接。
* `agent_connect_url` 字段名暂不调整；gateway 当前通过 WebSocket `hello.role` 区分 `windows_agent` 与 `web`，所以该 URL 实际是 role-neutral connect URL。

## 代码改造范围

### `session/runtime/gateway`

建议将 gateway registry 从返回 `string` 改为返回实例分配结果：

```go
type Assignment struct {
    GatewayID  string
    Index      int
    PublicHost string
}

type Registry interface {
    PickRandom(ctx context.Context) (*Assignment, error)
    PickRandomExcluding(ctx context.Context, gatewayID string) (*Assignment, error)
}
```

正式实现：

* 内部持有 `solver.StatefulResolver`
* 内部持有 `*solver.Target`，固定为 `game/gateway:http`
* 内部持有 public host pattern
* `Resolve` 后只从有 ready endpoint 的实例中选择
* `PickRandomExcluding` 在只剩一个实例时允许回退到原实例，保持当前行为

单测继续保留一个 fake registry 或 fake resolver，避免 service 单测依赖真实 deploy service。

### `session/service`

调整点：

* `CreateSession` 使用 registry 返回的 `Assignment`。
* `ReconnectSession` 使用 registry 返回的 `Assignment`。
* session 中保存 `Assignment.GatewayID`。
* token 签发使用 `Assignment.GatewayID`。
* `buildConnectURL` 参数改为 `sessionID, publicHost, token`。
* URL path 改为 `/v1/sessions/{sessionID}/game/connect`。

### `session/app/cmd`

调整点：

* 引入 `solver.NewDeployStatefulResolver()`。
* 解析 `solver.ParseTarget("game/gateway:http")`。
* 新增 `GAME_GATEWAY_PUBLIC_HOST_PATTERN`。
* 废弃 `GAME_GATEWAY_IDS` 与 `GAME_GATEWAY_DOMAIN`，或仅作为本地开发 fallback。

本地开发 fallback 可以保留，但正式环境应使用 deploy resolver。

## 测试方案

### 单元测试

需要更新或新增：

* `session/runtime/gateway`：
  * resolver 返回多个实例时随机选择。
  * reconnect 排除当前 `gateway_id`。
  * 单实例时 reconnect 回退到原实例。
  * 无实例或无 ready endpoint 时返回 `ErrNoGatewayAvailable`。
  * public host pattern 正确使用 `Index` 拼接。
* `session/service`：
  * `CreateSession` 返回 URL path 为 `/v1/sessions/{id}/game/connect`。
  * token issuer 收到的 `gatewayID` 等于 `Assignment.GatewayID`。
  * URL host 等于 `Assignment.PublicHost`。
  * token query 被正确 escape。
* `session/handler`：
  * 不只检查 URL 包含 gateway/token，还应解析 URL 并校验 scheme、host、path、token query。

### 大型测试

建议增加 session 与 gateway 的联合契约测试：

1. 部署 session service、gateway service 和必要依赖。
2. 调用 `CreateSession`。
3. 解析返回的 `agent_connect_url`。
4. 使用该 URL 建立 WebSocket。
5. 发送 `hello(role=windows_agent)`。
6. 断言连接成功。

该测试用于证明：

* session 选择的 `gateway_id` 与 gateway `HOSTNAME` 一致。
* session 拼出的 public host 能路由到同一 gateway 实例。
* token 的 session/gateway claims 能通过 gateway 校验。

## 兼容与迁移

* 不修改 token claim 结构。
* 不新增 `client_type`。
* 不修改 gateway WebSocket 路径。
* 不修改 session API 响应字段名。
* 旧 `/connect` 临时 URL 不建议长期兼容；若已有外部客户端依赖，可只在短期内由 gateway 或 ingress 做兼容转发。

## 仍需确认的事项

当前方案基本闭合，剩余需要确认的是部署配置细节：

1. `GAME_GATEWAY_PUBLIC_HOST_PATTERN` 的正式值是否固定为 `gateway-%d-game.liukexin.com`。
2. 本地开发环境是否需要保留 `GAME_GATEWAY_IDS/GAME_GATEWAY_DOMAIN` fallback。
3. session service 无法访问 deploy service 时，是启动失败，还是降级到 fallback registry。

推荐默认决策：

* 正式环境必须配置 `DOMINION_ENVIRONMENT` 与 `GAME_GATEWAY_PUBLIC_HOST_PATTERN`。
* 正式环境 resolver 初始化失败时直接启动失败，避免返回不可用 URL。
* 本地开发可以保留静态 fallback，但测试需覆盖正式 resolver registry。
