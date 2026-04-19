# LLM Agent 玩扫雷 milestone 1 session service 方案

## 目标

本方案用于落定 milestone 1 中 `session service` 的职责和边界，目标是：

* 让 `session service` 成为无状态的控制面入口，负责 `session` 生命周期管理。
* 让 `session service` 负责随机分配一个可用的 `game gateway`。
* 让 `session service` 为 `web` 与 `windows agent` 签发连接 token，并直接返回完整可用的连接 URL。
* 让客户端在原连接失效后，可以向 `session service` 发起重建请求并获得新的 gateway 分配结果。

## 范围

本方案基于 `ideas/llm_agent_play_game/saolei.md` 中 milestone 1 的目标收敛，不重复展开该文档中已确认的背景与里程碑定义。

本方案仅覆盖 `session service`：

* `session` 生命周期管理
* `session` 最小模型设计
* `game gateway` 的随机分配
* token 签发与完整 URL 返回
* 断线后的重建流程

本方案不包括：

* 视频流传输与缓存
* 截图字节缓存与读取
* 调试操作的实时转发
* `game gateway` 的实例内运行时设计

## 组件定位

`session service` 是无状态服务，负责：

* 创建、查询、删除 `session`
* 维护 `session` 生命周期状态
* 随机选择一个健康的 `game gateway`
* 为 `web` 与 `windows agent` 签发连接 token
* 直接返回带 token 的完整连接 URL
* 在连接失效后处理重建请求并重新分配 gateway

`session service` 不负责：

* 持有任何媒体长连接
* 缓存视频帧或截图内容
* 代理视频和操作热路径
* 承担 `game gateway` 的实例内状态

## 模型设计

### session 模型

`session` 只保存生命周期状态与断线重连所必需的信息，不保存媒体运行态。

建议最小字段如下：

* `id`
* `type`：当前会话对应的游戏类型。milestone 1 先固定支持 `saolei`
* `status`：`pending | active | disconnected | ended | failed`
* `gateway_id`
* `created_at`
* `updated_at`
* `ended_at`
* `last_seen_at`
* `reconnect_generation`
* `last_error`（可选，短文本）

设计原则：

* `session` 只回答“当前是什么会话、是什么游戏类型、处于哪个生命周期、当前应连接哪个 gateway”。
* 不在 `session` 中保存视频流状态、截图内容、缓存运行态或长连接细节。

### Create 请求模型

创建 `session` 时，请求中需要显式指定 `type`。

milestone 1 先支持：

* `saolei`

设计原则：

* `session service` 不根据客户端上下文隐式推断游戏类型。
* `type` 是 `session` 资源模型的一部分，而不只是创建时的临时参数。
* 后续若支持其他游戏类型，可在不改变 `Session` 主资源模型的前提下扩展枚举值。

### token 模型

连接 token 使用短期有效的签名 token。建议至少包含：

* `session_id`
* `gateway_id`
* `client_type`（`web` / `windows_agent`）
* `exp`
* `reconnect_generation`

设计原则：

* token 绑定到指定 `gateway_id`，防止客户端跨 gateway 连接。
* `session service` 返回的 URL 直接带 token，客户端拿到后可直接使用。
* token 在 `session` 重建后失效，避免旧连接信息继续生效。

## 代码分层

建议按如下层次实现：

* `api/handler`：提供 `session` 管理与重建接口
* `service`：处理 session 生命周期、gateway 分配、URL 生成
* `repository`：持久化 `session`
* `token`：签发与校验 token
* `gateway registry`：维护当前可用 `game gateway` 列表与健康状态

## 关键细节

### gateway 分配策略

milestone 1 使用随机分配策略。

原因：

* 目标是优先打通完整链路
* 实现简单，能满足当前规模
* 后续可替换为更复杂的负载策略，但不影响客户端契约

### 完整 URL 返回

`session service` 直接返回：

* `agent_connect_url`

要求：

* URL 中直接带好 token
* 客户端不需要再拼参数
* 客户端不需要自己选择 gateway

### 重建流程

连接失败时，客户端先尝试使用原 URL 连接原 `game gateway`。

若连接不上，则：

1. 客户端向 `session service` 发起重建请求
2. `session service` 重新随机选择一个可用的 `game gateway`
3. 更新 `session.gateway_id` 与 `reconnect_generation`
4. 返回新的完整连接 URL

该流程是连接恢复的标准路径，不做透明迁移。

## 决策详情

### 决策 1：`session service` 保持无状态

选择：`session service` 不持有长连接与媒体缓存。

原因：

* 适合水平扩展
* 让控制面与热路径职责清晰分离
* 减少服务耦合和资源压力混合

### 决策 2：返回完整 URL 而非连接片段

选择：`session service` 直接返回完整可用 URL。

原因：

* 降低客户端复杂度
* 避免 `windows agent` 自行拼 token 或 gateway 信息
* 让连接入口保持单一、明确

### 决策 3：重建请求重新分配 gateway

选择：连接失败后由客户端向 `session service` 发起重建请求，再获得新的 gateway。

原因：

* 行为简单且可恢复
* 不依赖透明迁移或实例级恢复
* 与有状态 `game gateway` 的边界一致

## 未来规划

未来若 `game gateway` 的选择策略需要引入负载、地区或资源约束，可在不改变 `session service -> 返回完整 URL` 这一客户端契约的前提下扩展 gateway 选择逻辑。
