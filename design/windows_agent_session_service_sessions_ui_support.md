# Windows Agent session 服务 UI 支持方案

## 目标

本方案用于落定 `windows_agent` 新版连接 UI 所依赖的 `session service` 能力，目标是：

* 让 `windows_agent` 可以从 `session service` 获取当前可连接 session 列表。
* 让 session 资源本身携带最新 `agent_connect_url`，客户端不再从 response 外层读取连接 URL。
* 让 `windows_agent` 在连接失败后可以通过 `ReconnectSession` 获得新的 session 资源与连接 URL。
* 让用户可以在 UI 中按显式选择的 session 类型创建 session。

本方案希望达成的效果是：`session service` 提供完整的 session 列表、创建、重连和删除控制面能力，`windows_agent` 只消费 session 资源模型即可完成连接管理，不需要自行拼接 gateway URL、token 或 gateway host。

## 范围

本方案仅覆盖 `projects/game/session` 的接口与模型调整：

* `Session` proto 模型调整。
* 新增 `ListSessions` 接口。
* 调整 `CreateSessionResponse` 与 `ReconnectSessionResponse`。
* repository/service/handler 分层变更。
* 与 `windows_agent` 连接管理相关的契约。

本方案不包括：

* `windows_agent` UI 和 Wails binding 实现，见 `design/windows_agent_session_service_windows_agent_ui.md`。
* `game gateway` WebSocket 媒体和控制协议调整。
* session 权限、鉴权和多租户隔离设计。
* session 分页、搜索、排序的完整产品化能力。

## 模型设计

### Session

在 `projects/game/session/session.proto` 的 `Session` 中新增输出字段：

* `agent_connect_url`：当前 session 对应的 windows agent gateway 连接 URL。

字段语义：

* 由 `session service` 生成。
* 包含 gateway host、session ID 和短期 token。
* 客户端拿到后可以直接连接 gateway。
* token 过期或 gateway 不可用时，客户端应调用 `ReconnectSession` 获取新的 `Session.agent_connect_url`。

注意事项：

* `agent_connect_url` 包含连接 token，日志中禁止输出完整值。
* 允许在 UI 中展示 session name、gateway id 和状态，但不展示完整 token URL。
* `Session` 持久化模型可以保存 `agent_connect_url`，也可以只保存生成连接 URL 所需的 token/material 后在查询时组装；第一版建议在 service 层生成并随 proto 输出，避免 repository 存储敏感完整 URL。

### ListSessions

新增接口：

```proto
rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse) {
  option (google.api.http) = {
    get: "/v1/sessions"
  };
}

message ListSessionsRequest {}

message ListSessionsResponse {
  repeated Session sessions = 1;
}
```

列表范围：

* 只返回未结束/可连接 session。
* 第一版排除 `SESSION_STATUS_ENDED`。
* `PENDING`、`ACTIVE`、`DISCONNECTED`、`FAILED` 均返回，由 UI 展示状态并允许用户按需连接或删除。

不在第一版提供：

* 分页。
* 排序参数。
* 状态过滤参数。
* 按游戏类型过滤。

### CreateSessionResponse

调整为只返回 session：

```proto
message CreateSessionResponse {
  Session session = 1 [(google.api.field_behavior) = REQUIRED];
}
```

连接 URL 从 `response.session.agent_connect_url` 获取。

### ReconnectSessionResponse

调整为只返回 session：

```proto
message ReconnectSessionResponse {
  Session session = 1 [(google.api.field_behavior) = REQUIRED];
}
```

连接 URL 从 `response.session.agent_connect_url` 获取。

### SessionType

创建 session 时仍由请求显式传入 `type`。

第一版 UI 使用当前已有类型：

* `SESSION_TYPE_SAOLEI`

后续新增游戏类型时，只扩展 proto enum 和 UI select 选项，不改变创建接口形态。

## 代码分层

建议按现有 `projects/game/session` 分层扩展：

* `session.proto`：新增 `ListSessions`，移动 `agent_connect_url` 到 `Session`，调整 response。
* `handler.go`：实现 `ListSessions` handler，并调整 create/reconnect response 填充。
* `service/service.go`：新增 `ListSessions` service 方法，统一为 session proto 输出准备 `agent_connect_url`。
* `domain/repository`：新增 list 能力，支持排除 ended session。
* `runtime/storage`：为 Mongo repository 实现 list 查询。
* `service/service_test.go` 与 `handler_test.go`：覆盖 list、create、reconnect 的新返回模型。

## 关键细节

### agent_connect_url 生成位置

建议在 service 层统一生成 `agent_connect_url`，原因：

* URL 生成依赖 gateway public host 与 token 签发，属于 service 编排职责。
* repository 不需要理解 token 或 gateway URL 语义。
* handler 只负责 domain/proto 转换，不承担业务生成逻辑。

### ListSessions 的 token 新鲜度

`ListSessions` 返回的 `agent_connect_url` 可能在用户点击连接时已经过期。

设计处理：

* `windows_agent` 先尝试使用 session 中的 `agent_connect_url`。
* 如果 gateway 连接失败，调用 `ReconnectSession`。
* 再使用新的 `session.agent_connect_url` 重试一次。
* 第二次失败才向用户报错。

这样可以避免列表接口每次都强制旋转 token，同时保留可靠恢复路径。

### DeleteSession

接口保持现状：

```http
DELETE /v1/sessions/{id}
```

客户端约定：

* 删除当前已连接 session 前，`windows_agent` 先停止传输并断开 gateway。
* 断开失败则不继续删除。
* 非当前连接 session 可直接删除。

### 敏感日志

所有 session service 日志应避免记录完整 `agent_connect_url`。

允许记录：

* session name。
* gateway id。
* reconnect generation。
* session status。

不允许记录：

* 完整 `agent_connect_url`。
* token 原文。

## 决策详情

### 决策 1：新增 ListSessions

选择：补充 `ListSessions` 接口，而不是让 `windows_agent` 维护本地 session 缓存。

原因：

* session 服务是 session 生命周期的系统事实来源。
* 多端创建、删除或重连后，agent 本地缓存无法保证准确。
* UI 需要展示当前可连接 session 列表，必须来自服务端。

### 决策 2：只返回未结束/可连接 session

选择：第一版列表排除 `ENDED` session。

原因：

* `windows_agent` 的连接 UI 面向可操作 session。
* 已结束 session 不可连接，展示会干扰操作。
* 后续如需要历史记录，可新增 showEnded 或独立历史页面。

### 决策 3：agent_connect_url 放入 Session

选择：`Session` 资源直接携带 `agent_connect_url`，`CreateSessionResponse` 和 `ReconnectSessionResponse` 不再单独返回 URL 字段。

原因：

* 资源模型自包含，`Get/List/Create/Reconnect` 返回形态一致。
* `windows_agent` 只需要处理 `Session`，降低前端和 app 层分支。
* 新旧连接逻辑都能统一读取 `session.agent_connect_url`。

### 决策 4：连接失败后再 ReconnectSession

选择：`windows_agent` 优先使用 session 中的连接 URL，失败后再调用 `ReconnectSession`。

原因：

* 避免每次连接都无条件重新分配 gateway。
* 保留快速连接路径。
* 在 token 过期、gateway 不可用或分配失效时仍能自动恢复。

## 验收标准

* `GET /v1/sessions` 返回未结束 session 列表。
* `Session` proto 输出包含 `agent_connect_url`。
* `CreateSessionResponse` 只返回 `session`，且 `session.agent_connect_url` 可用于 agent 连接。
* `ReconnectSessionResponse` 只返回 `session`，且 `session.agent_connect_url` 已更新。
* `DeleteSession` 保持可用。
* 单元测试覆盖 list、create、reconnect、delete 的关键路径。
* 日志不输出完整 `agent_connect_url` 或 token。

## 未来规划

未来可按需要扩展：

* `ListSessionsRequest` 增加状态过滤、分页和排序。
* session 历史列表或审计列表。
* 按用户、项目或租户隔离 session。
* 为不同客户端角色返回不同连接 URL，例如 web connect URL 与 agent connect URL。
