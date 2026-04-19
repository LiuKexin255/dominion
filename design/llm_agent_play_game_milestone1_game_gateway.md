# LLM Agent 玩扫雷 milestone 1 game gateway 方案

## 目标

本方案用于落定 milestone 1 中 `game gateway` 的职责和接入方式，目标是：

* 让 `game gateway` 成为有状态的媒体与操作传输层。
* 让 `windows agent` 与 `web` 均直连分配到的 `game gateway`。
* 让每个 `game gateway` 实例拥有固定接入域名，能稳定承载长连接。
* 让实例失效后由客户端回到 `session service` 触发重建，而不是在 gateway 层做透明迁移。

## 范围

本方案基于 `ideas/llm_agent_play_game/saolei.md` 中 milestone 1 的目标收敛，不重复展开该文档中已确认的背景与里程碑定义。

本方案仅覆盖 `game gateway`：

* 连接与媒体热路径职责
* 实例内代码分层
* Kubernetes 中的 StatefulSet 部署方式
* 每实例固定域名接入方案
* 连接校验与断线后的处理边界

本方案不包括：

* `session` 生命周期管理
* gateway 分配策略
* `session` 持久化模型
* 更高阶段的统一前门路由

## 组件定位

`game gateway` 是有状态服务，负责：

* 持有 `windows agent` 与 `web` 的长连接
* 接收和转发视频流
* 接收截图结果并提供读取
* 接收 `web` 发起的调试操作并转发给 `windows agent`
* 在进程内维护连接状态与媒体缓存

`game gateway` 不负责：

* 创建或管理 `session`
* 选择 gateway 分配策略
* 持久化会话生命周期信息
* 为客户端生成连接 URL

## 模型设计

### gateway 身份模型

`game gateway` 实例需要稳定逻辑身份，建议使用 `StatefulSet` ordinal 作为 `gateway_id` 来源，例如：

* `game-gateway-0`
* `game-gateway-1`
* `game-gateway-2`

设计原则：

* 客户端绑定的是 `gateway_id` 对应的实例，而不是某个节点。
* `gateway_id` 是 token 校验和连接归属的依据。

### 连接校验模型

`game gateway` 在接受连接时，需校验 token 中至少包含的：

* `session_id`
* `gateway_id`
* `client_type`
* `exp`
* `reconnect_generation`

只有当 token 中的 `gateway_id` 与当前实例一致时，连接才被接受。

## 代码分层

建议按如下层次实现：

* `connect`：管理 `web` / `windows agent` 长连接
* `media`：处理视频流与截图缓存
* `control`：处理调试操作与结果回传
* `auth`：校验 token 与 session 绑定关系
* `session runtime`：维护本实例上活跃 session 的内存状态

## Kubernetes 部署与接入

### 部署方式

`game gateway` 使用 `StatefulSet` 部署。

原因：

* 需要稳定实例身份
* 需要给每个实例配置固定接入域名
* 需要明确表达“session 连接到哪个实例”

### 接入方式

每个 `game gateway` 实例有一个固定域名作为接入点，例如：

* `gw-0.example.com`
* `gw-1.example.com`
* `gw-2.example.com`

客户端拿到 `session service` 返回的完整 URL 后，直接连接对应域名。

### 为什么不使用普通 Service / HTTPRoute 做实例定向

Kubernetes 普通 `Service` 与 `HTTPRoute` 适合将流量送到某个服务，不适合表达“将该 session 的连接送到某个指定 gateway 实例”。

因此本方案不将以下机制作为实例定向的主方案：

* 普通 `Service` 的默认负载均衡
* `HTTPRoute -> Service` 的通用路由
* 按节点 IP 作为接入契约

实例定向由 `session service` 决定，`game gateway` 只负责承接已分配到本实例的连接。

## 关键细节

### 媒体与操作热路径

`game gateway` 是唯一承载以下热路径的组件：

* `windows agent -> game gateway` 的视频流与截图结果上传
* `web -> game gateway` 的视频/截图读取
* `web -> game gateway -> windows agent` 的调试操作转发

### 连接失败处理边界

`game gateway` 不负责在自身失效时将连接透明迁移到其他实例。

规则如下：

* 客户端优先尝试重连原 URL
* 若连接不上，则回到 `session service` 发起重建请求
* 新 gateway 分配完成后，再由客户端连接新 URL

### token 与实例绑定

旧 token 在 `session` 重建后应失效。

这样可以保证：

* 客户端不能继续使用旧 gateway 信息
* 连接归属始终与最新 `gateway_id` 一致

## 决策详情

### 决策 1：`game gateway` 作为有状态服务单独存在

选择：将媒体与操作热路径收敛到独立有状态服务。

原因：

* 长连接、媒体缓存、操作转发天然具有实例内状态
* 与 `session service` 的无状态控制面职责形成清晰分层
* 更符合后续整体架构演进方向

### 决策 2：每实例固定域名接入

选择：每个 `game gateway` 实例单独固定域名。

原因：

* 可以直接表达“连接到指定实例”的需求
* 对 WebSocket 和长连接更直接、行为更稳定
* 避免依赖通用 Service/HTTPRoute 的二次负载均衡

### 决策 3：不在 gateway 层做透明迁移

选择：实例失效后由客户端回到 `session service` 发起重建请求。

原因：

* 避免在 gateway 层引入复杂的跨实例会话迁移
* 恢复路径清晰且符合 milestone 1 目标
* 有利于控制面与热路径边界稳定

## 未来规划

未来若 `game gateway` 实例数量增长，导致单实例域名与证书管理成本升高，可在不改变“客户端使用 `session service` 返回的完整 URL 直连 gateway”这一契约的前提下，引入统一入口层，并按 token 中的 `gateway_id` 做确定性路由。
