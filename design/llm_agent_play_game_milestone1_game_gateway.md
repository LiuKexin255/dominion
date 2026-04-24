# LLM Agent 玩扫雷 milestone 1 game gateway 详细技术方案

## 目标

本方案用于落定 milestone 1 中 `game gateway` 的详细实现，目标是：

* 让 `game gateway` 成为 milestone 1 中唯一承接媒体热路径与调试操作热路径的有状态服务。
* 让 `web` 与 `windows agent` 通过统一的 gateway 协议接入被分配的实例。
* 让实时链路与查询链路分层明确：实时链路使用 `WebSocket`，查询链路使用 `RESTful HTTP`。
* 让截图能力改为按需模式，避免周期性截图上传带来的额外开销。
* 让操作协议只暴露调试所需的完整鼠标动作，避免将低级输入原语直接暴露给 `web`。

本方案希望达成的效果是：`session service -> game gateway -> windows agent / web` 的职责和协议边界清晰稳定，`web` 可以稳定查看视频、按需获取截图，并发起完整鼠标调试动作。

## 范围

本方案基于以下文档继续细化，不重复展开其中已确认的背景：

* `ideas/llm_agent_play_game/saolei.md`
* `design/llm_agent_play_game_milestone1_session_service.md`
* `design/deploy_stateful_workload_support.md`
* `design/llm_agent_play_game_milestone1_windows_agent.md`

本方案覆盖：

* `game gateway` 的职责边界与实例内 runtime
* `WebSocket` 主协议与 `RESTful` 辅助接口
* 视频上传、分片、缓存与下发策略
* 按需截图读取模型
* `web -> game gateway -> windows agent` 的调试操作模型
* 与当前 token 模型一致的鉴权方式

本方案不包括：

* `session service` 生命周期模型调整
* 新 token 字段设计
* `windows agent` 的具体采集/输入实现细节（见独立文档）
* WebRTC 媒体链路

## 组件定位

`game gateway` 是有状态服务，负责：

* 持有 `windows agent` 与 `web` 的长连接
* 接收 `windows agent` 上传的视频编码流
* 维护视频初始化段、近期媒体分片缓存和最新截图元信息
* 向 `web` 推送视频媒体分片
* 处理 `web` 发起的鼠标调试操作，并转发给 `windows agent`
* 维护实例内 `session runtime`
* 提供按需截图读取和运行态查询接口

`game gateway` 不负责：

* 创建、删除、重建 `session`
* 决定 `session` 绑定到哪个 gateway
* 持久化长期媒体数据
* 透明迁移跨实例连接
* 执行本地 Windows 采集和输入注入

## 总体协议收敛

### 协议分层

milestone 1 采用两类协议：

1. **主路径：WebSocket**
2. **辅助路径：RESTful HTTP**

划分原则：

* 需要双向实时、低延迟、承载二进制媒体的链路走 `WebSocket`
* 需要按需读取、查询状态的链路走 `RESTful HTTP`

### 为什么主路径不采用 gRPC streaming

原因：

* 当前仓库实际落地模式是 `unary gRPC + grpc-gateway REST`，没有 streaming RPC 基础设施
* `grpc-gateway` 不适合作为浏览器友好的双向实时二进制热路径
* `WebSocket` 更适合统一承接视频媒体与控制消息

### 协议实现选型

`game gateway` 服务端建议使用：

* `net/http`：承载 RESTful HTTP 与 WebSocket upgrade 入口
* `nhooyr.io/websocket`：承载 `/connect` WebSocket 协议

其中：

* `snapshot` / `runtime` 接口走当前仓库既有的 `unary gRPC + grpc-gateway REST` 模式
* `connect` WebSocket 接口不走 `gRPC`，由独立的 HTTP upgrade 入口直接承接

原因：

* 标准库适合 RESTful HTTP 接口
* 标准库本身不提供完整可用的 WebSocket 实现
* `nhooyr.io/websocket` 与 `context`、`net/http` 协作自然，适合本项目的一体化 HTTP + WebSocket 服务模型

## 接口路径

所有 `game gateway` 对外路径统一收敛到：

* `GET /v1/sessions/{session}/game/connect?token=...`：WebSocket upgrade 入口
* `GET /v1/sessions/{session}/game/snapshot`：按需读取截图
* `GET /v1/sessions/{session}/game/runtime`：读取 gateway 运行态摘要

路径设计原则：

* `session` 是主资源
* `game` 是该 session 在 gateway 上的子资源域
* `connect` / `snapshot` / `runtime` 都挂在同一资源前缀下，避免协议面分散

## 鉴权模型

本期显式沿用当前代码中的 token claims，不新增 `client_type`。`game gateway` 只校验：

* `session_id`
* `gateway_id`
* `exp`
* `reconnect_generation`

校验规则：

* token 签名合法
* token 未过期
* token 中 `gateway_id` 必须与当前实例一致
* 路径中的 `{session}` 必须与 token 中的 `session_id` 一致
* WebSocket 握手消息中的 `session_id` 也必须与 token 一致
* 若 runtime 中记录的 `reconnect_generation` 更高，则拒绝旧连接的关键写操作

客户端角色区分不依赖 token，而依赖 WebSocket 建连后的 `hello.role`：

* `windows_agent`
* `web`

## session runtime 模型

每个 `game gateway` 实例内维护 `session runtime`，最小字段如下：

* `session_id`
* `gateway_id`
* `reconnect_generation`
* `agent_connection`
* `web_connections[]`
* `stream_state`
* `latest_snapshot`
* `inflight_operation`
* `last_media_time`
* `last_snapshot_time`
* `last_error`

设计原则：

* `agent_connection` 至多一个
* `web_connections[]` 可有多个观察者和调试者
* `inflight_operation` 同一时刻只允许一个
* `latest_snapshot` 只保留最近一张可用截图及其元信息

## WebSocket 协议设计

### 建连入口

客户端连接：

* `GET /v1/sessions/{session}/game/connect?token=...`

由 HTTP upgrade 到 WebSocket 后，第一条业务消息必须是：

```json
{
  "type": "hello",
  "session_id": "sessions/xxx 或 xxx",
  "message_id": "msg-1",
  "payload": {
    "role": "windows_agent | web"
  }
}
```

处理规则：

* token 不合法：关闭连接
* `{session}` 与 token 不一致：关闭连接
* `hello.session_id` 与 token 不一致：关闭连接
* `role = windows_agent` 且已有活跃 agent 连接：拒绝新连接
* `role = web`：加入观察者列表

### 顶层 envelope

统一使用 JSON envelope：

* `type`
* `session_id`
* `message_id`
* `payload`

与当前 proto 的对应关系如下：

* 顶层对象对应 `GameWebSocketEnvelope`
* `session_id` 对应 `GameWebSocketEnvelope.session_id`
* `message_id` 对应 `GameWebSocketEnvelope.message_id`
* `payload` 对应 `GameWebSocketEnvelope` 的 `oneof payload`
* `type` 用于表达当前命中的 `oneof payload` 分支：
  * `hello` -> `GameHello`
  * `media_init` -> `GameMediaInit`
  * `media_segment` -> `GameMediaSegment`
  * `control_request` -> `GameControlRequest`
  * `control_ack` -> `GameControlAck`
  * `control_result` -> `GameControlResult`
  * `error` -> `GameError`
  * `ping` -> `GamePing`
  * `pong` -> `GamePong`

本期最小消息集合：

* `hello`
* `media_init`
* `media_segment`
* `control_request`
* `control_ack`
* `control_result`
* `error`
* `ping`
* `pong`

### 对外暴露的控制动作

本期 **不对 web 暴露低级鼠标原语**。`web` 只允许发送完整动作：

* `mouse_click`
* `mouse_double_click`
* `mouse_drag`
* `mouse_hover`
* `mouse_hold`

其中：

* `mouse_hold` 必须带 `duration_ms`
* `mouse_hold` 最大时长限制为 `30s`

这样做的原因：

* 避免将一次动作拆成多次网络交互
* 降低网络抖动导致的“半执行状态”风险
* 更符合 milestone 1 的调试型需求

### `control_request`

示例：

```json
{
  "type": "control_request",
  "session_id": "sessions/sess-1",
  "message_id": "req-1",
  "payload": {
    "operation_id": "op-1",
    "kind": "mouse_drag",
    "flash_snapshot": true,
    "mouse": {
      "button": "left",
      "from_x": 100,
      "from_y": 120,
      "to_x": 280,
      "to_y": 320,
      "duration_ms": 800
    }
  }
}
```

其中：

* `flash_snapshot = true` 表示该操作完成后，gateway 需要刷新当前 session 的最新截图缓存
* `flash_snapshot` 只影响截图缓存刷新，不改变 `control_result` 的返回字段

### `control_ack`

示例：

```json
{
  "type": "control_ack",
  "session_id": "sessions/sess-1",
  "message_id": "ack-1",
  "payload": {
    "operation_id": "op-1"
  }
}
```

### `control_result`

示例：

```json
{
  "type": "control_result",
  "session_id": "sessions/sess-1",
  "message_id": "result-1",
  "payload": {
    "operation_id": "op-1",
    "status": "succeeded | failed | timed_out",
    "error_message": ""
  }
}
```

注意：

* `snapshot` 已完全从 WebSocket 常态推送中移除
* `control_result` 不返回 `snapshot_id`、`has_fresh_snapshot` 等截图结果字段
* 若调试方需要读取最新静态截图，统一走 `GET /v1/sessions/{session}/game/snapshot`

## RESTful 接口设计

### `GET /v1/sessions/{session}/game/snapshot`

用途：按需获取静态截图。

本期选择“按需优先的混合模式”：

* 请求会触发截图流程
* 若已有**足够新**的截图，可直接返回最近一张
* 同步或随后刷新最新截图缓存

建议“足够新”阈值：

* `1s`

这样做的原因：

* 保持“按需截图”为主语义
* 避免频繁重复截图导致 agent 压力过高
* 让调试读取体验更稳定

### `GET /v1/sessions/{session}/game/runtime`

用途：查询 gateway 运行态摘要。

milestone 1 最小返回字段建议如下：

* `session_id`
* `gateway_id`
* `agent_connected`
* `web_connection_count`
* `stream_status`（`active | paused | unavailable`）
* `last_media_time`
* `last_snapshot_time`
* `inflight_operation`
* `last_error`

该接口只作为调试/排障读模型，不作为强一致控制面资源。

## 视频传输、格式与缓存

### 主链路

milestone 1 选择：

* 编码：`H.264`
* 浏览器播放封装：`fMP4`
* 浏览器播放方式：`MSE`

### 分片策略

视频分片策略收敛为：

* **按 GOP / 关键帧边界切片**
* gateway 缓存最近 **2~3 秒** 媒体分片
* 新连接从最近关键帧开始追帧

原因：

* 比更细粒度切片更易实现和维护
* 比更大粒度切片更适合调试观看与快速追帧
* 更符合 milestone 1 的稳定性目标

### 缓存分层

gateway 内缓存分为三层：

1. `init segment` 缓存
2. 近期媒体分片环形缓存
3. 最新截图缓存

建议目标：

* `init segment`：1 份
* 媒体分片缓存：最近 2~3 秒
* 截图缓存：最近 1 张

### 新连接追帧策略

`web` 新连接成功后，gateway 的媒体推送顺序：

1. `media_init`
2. 从最近关键帧开始的缓存分片
3. 实时 `media_segment`

静态截图不再由 WebSocket 主动推送，前端如需静态兜底画面，走 `snapshot` REST 接口。

## 控制操作与超时

### 串行化规则

同一 `session` 下：

* 允许多个 `web` 同时连接
* 允许多个 `web` 发起调试写操作
* gateway 必须按 `session` 严格串行执行控制动作
* 同一时刻只允许一个 `inflight_operation`

### 超时规则

* `mouse_click` / `mouse_double_click` / `mouse_hover`：`1s`
* `mouse_drag`：`30s`
* `mouse_hold`：由请求给出时长，且总时长不得超过 `30s`
* `windows agent` 无响应：`60s`

超时处理：

* 当前操作标记为 `timed_out`
* 清理鼠标按住态
* 若 agent 已断线，则在新连接建立后的初始化阶段执行输入状态重置

## 部署与接入

`game gateway` 继续使用 `StatefulSet` 部署，并与 `design/deploy_stateful_workload_support.md` 保持一致。

接入方式：

* `game-gateway-0.gw.example.com`
* `game-gateway-1.gw.example.com`
* `game-gateway-2.gw.example.com`

`session service` 返回完整 URL 后：

* `windows agent` 直连对应实例
* `web` 直连对应实例

gateway 失效时：

* 客户端优先重连原 URL
* 若原 URL 不可用，则回到 `session service` 发起重建
* gateway 不做透明迁移

## 中型测试方案

### 测试定位

`game gateway` 的接口测试定义为**中型测试**，属于大型测试的一种，但范围只面向单个服务。

中型测试目标是验证：

* WebSocket 主协议是否符合对外契约
* `snapshot` / `runtime` RESTful 接口是否符合对外契约
* fake agent 接入后的视频、截图、控制链路是否走通
* 串行化、超时、断线清理等运行时行为是否符合预期

### 编排方式

中型测试统一使用 `testplan` 编排，并使用单独的 deploy 配置部署测试物料。

推荐目录：

* `projects/game/game_gateway/testplan/`

推荐包含：

* `interface_test.yaml`
* `test_deploy.yaml`
* 中型测试 case target

### 被测系统与依赖

中型测试中：

* SUT：`game-gateway` 真实服务产物
* 依赖：`fake-agent` fake 服务产物
* `web` 不需要 fake 服务，直接由测试 case 发起 REST / WebSocket 请求

原因：

* `web` 侧主要承担接口调用与断言职责，更适合在测试代码中直接实现
* `fake-agent` 作为独立服务部署，更接近真实网络边界，也更容易模拟断线、超时、暂停媒体等行为

### fake-agent 角色

`fake-agent` 需要模拟 `windows agent` 的最小能力：

* 连接 `GET /v1/sessions/{session}/game/connect?token=...`
* 发送 `hello(role=windows_agent)`
* 上传 `media_init`
* 上传 `media_segment`
* 响应 `control_request`
* 配合 snapshot 请求返回截图

并支持以下测试场景：

* 正常返回
* 延迟返回
* 永不返回（超时）
* 主动断线
* 暂停视频上传
* snapshot 生成失败

### fake-agent 驱动方式

第一版中型测试中，`fake-agent` 使用**环境变量**驱动测试场景，而不是额外暴露控制接口。

原因：

* 配置简单，易于在 testplan/deploy 中表达
* 更适合稳定的接口测试
* 避免为了测试再引入一层 fake-agent 控制协议

### 核心覆盖场景

第一批中型测试建议至少覆盖：

* web 合法连接成功
* fake-agent 合法连接成功
* path session 与 token 不一致时被拒绝
* 第二个 agent 连接同一 session 时被拒绝
* `media_init` 与 `media_segment` 能成功送达 web
* 新连接从最近关键帧开始追帧
* `GET /snapshot` 在已有足够新截图时直接返回最近截图
* `GET /snapshot` 在无足够新截图时触发 fake-agent 生成并返回
* `GET /runtime` 返回最小字段集
* `control_request -> control_ack -> control_result` 走通
* `flash_snapshot = true` 时，操作完成后会刷新截图缓存；操作链路与按需截图链路可同时工作，且操作结果本身不返回截图结果字段
* 多 web 并发发起操作时，gateway 严格串行执行
* click / drag / agent 无响应超时语义正确
* fake-agent 断线时 inflight operation 被清理，鼠标状态重置逻辑被触发

### 测试物料分类

中型测试所需物料按来源分为两类：

#### 开发时需要产出的物料

* `game-gateway` 服务产物
* `fake-agent` fake 服务产物
* `testplan` YAML
* 中型测试 deploy YAML
* 中型测试 case target
* fake-agent 场景环境变量定义

#### 预先准备的物料

* **单个短视频测试素材**

当前方案中，除视频素材外，其余测试物料都属于开发时需要产出的内容。

### 视频测试物料策略

中型测试推荐准备：

* **单个短视频测试素材**

由 `fake-agent` 在运行时按需从该视频中切取：

* `media_init`
* 若干 `media_segment`
* 至少一个关键帧起始片段
* 对应时刻的截图

这样做的原因：

* 比维护大量离散分片文件更容易管理
* 更适合验证“最近关键帧追帧”和“缓存窗口”逻辑
* 视频与截图来源一致，便于测试结果比对

建议视频素材满足：

* 时长短（约 `2~5s`）
* 含多个 segment
* 含明确关键帧边界
* 内容稳定、可重复

## 决策详情

### 决策 1：主路径采用 WebSocket，辅助路径采用 RESTful HTTP

选择：实时链路用 WebSocket，查询链路用 REST。

原因：

* WebSocket 更适合双向实时与二进制媒体
* REST 更适合按需读取与调试查询
* 协议边界清晰，利于 web 与 agent 分层实现

### 决策 2：截图改为按需模式

选择：取消周期性截图上传，改为按需触发。

原因：

* 更符合调试场景
* 降低 agent 和 gateway 的额外负担
* 与 RESTful `snapshot` 读取模型天然匹配

### 决策 3：只对 web 暴露完整鼠标动作

选择：不向 web 暴露 `mouse_down` / `mouse_up` / `mouse_move` 等低级原语。

原因：

* 降低网络抖动下的半状态风险
* 简化 gateway 状态机
* 更适合 milestone 1 的调试需求

### 决策 4：视频按 GOP / 关键帧切片

选择：以 GOP 为单位切片，并保留 2~3 秒缓存。

原因：

* 在实现复杂度、追帧体验和调试稳定性之间更平衡

### 决策 5：沿用当前 token 模型

选择：不扩展 `client_type`。

原因：

* 与当前 `session service` 已实现代码保持一致
* 避免控制面模型在 milestone 1 中进一步发散

## 未来规划

后续若需要进一步降低直播延迟或丰富调试能力，可在不改变本方案基本边界的前提下演进：

* 媒体链路升级为 WebRTC
* 增加键盘输入操作
* 将 `snapshot` 扩展为更丰富的只读模型
* 为 `runtime` 接口补充更细粒度的诊断字段
