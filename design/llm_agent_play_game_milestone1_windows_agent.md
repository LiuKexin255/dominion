# LLM Agent 玩扫雷 milestone 1 windows agent 详细技术方案

## 目标

本方案用于单独描述 milestone 1 中 `windows agent` 的职责、边界和实现策略，目标是：

* 让 `windows agent` 成为部署在目标 Windows 主机上的本地执行端。
* 让其稳定承接单窗口画面采集、视频编码、按需截图与鼠标输入执行。
* 让其通过 `game gateway` 协议接入系统，而不直接对 `web` 暴露。

## 范围

本方案覆盖：

* 单窗口绑定模型
* 视频采集与编码
* 按需截图
* 鼠标输入执行
* 与 `game gateway` 的连接与回传行为

本方案不包括：

* `session service` 生命周期管理
* `game gateway` 的媒体缓存与对外 REST 接口
* 键盘输入实现

## 组件定位

`windows agent` 负责：

* 绑定单个目标窗口
* 采集该窗口内容
* 将画面编码为 H.264 并上传到 `game gateway`
* 在需要时生成 JPEG 截图
* 执行来自 `game gateway` 的完整鼠标动作
* 回传操作结果、错误和截图标识

`windows agent` 不负责：

* 直接服务浏览器
* 决定连接哪个 gateway
* 保存长期 session 状态

## 窗口绑定模型

milestone 1 收敛为：

* 一次 session 只绑定 **一个窗口**
* 所有输入坐标均使用 **窗口相对坐标**

窗口状态处理：

* 失焦、遮挡、最小化时，agent 应尽量继续采集绑定窗口内容
* 如果底层能力无法继续捕获，则：
  * 暂停视频上传
  * 保持控制链路可用
  * 将当前流状态回传给 gateway

## 代码分层

建议按如下层次实现：

* `runtime`：管理 session、本地状态和重连
* `capture`：窗口绑定与画面采集
* `encoder`：H.264 编码
* `snapshot`：按需截图生成
* `transport`：与 gateway 的 WebSocket 协议收发
* `input`：鼠标输入执行与状态重置

## 媒体采集与上传

### 视频

视频主链路：

* 从绑定窗口采集内容
* 编码为 `H.264`
* 按 GOP / 关键帧边界组织上传
* 通过 WebSocket 发送给 `game gateway`

约束：

* 优先低延迟编码配置
* 避免依赖 B-frames
* 若无法持续采集，则暂停视频上传，而不是发送伪造帧

### 截图

截图改为 **按需模式**。

触发来源：

* gateway 发起 snapshot 请求
* 控制操作要求 `flash_snapshot = true`

截图格式：

* `jpeg`

返回策略：

* 若本地已有足够新的截图，可优先返回最近一张
* 同步或随后刷新最近截图

建议“足够新”阈值：

* `1s`

## 输入执行

### 鼠标动作集合

agent 需要执行的完整鼠标动作：

* `mouse_click`
* `mouse_double_click`
* `mouse_drag`
* `mouse_hover`
* `mouse_hold(duration_ms)`

### 输入注入路径

本期建议使用：

* `SendInput`

实现要求：

* 将窗口相对坐标转换为系统输入坐标
* 严格检查调用返回值
* `mouse_hold` 必须带时长，且最大 `30s`
* `mouse_drag` 按请求给定轨迹和时长一次性执行，不拆成多次网络往返

### 超时规则

* `mouse_click` / `mouse_double_click` / `mouse_hover`：`1s`
* `mouse_drag`：`30s`
* `mouse_hold`：最大 `30s`
* agent 无响应：由 gateway 在 `60s` 后判超时

### 断线与状态重置

若连接断开：

* agent 应重置鼠标状态
* 不恢复历史按住态
* 新连接建立后只恢复媒体与控制服务能力，不恢复旧操作

## 与 gateway 的交互

### 连接

agent 使用：

* `GET /v1/sessions/{session}/game/connect?token=...`

完成 WebSocket upgrade 后发送 `hello`，`role = windows_agent`。

### 上行消息

agent 主要上行：

* `media_init`
* `media_segment`
* `control_result`
* `error`
* `pong`

截图不作为常态 WebSocket push，而是配合 snapshot 请求或操作结果流程产出。

## 决策详情

### 决策 1：agent 只绑定单窗口

原因：

* 简化坐标模型
* 简化采集和输入注入边界
* 更符合扫雷场景的阶段目标

### 决策 2：截图按需触发

原因：

* 调试场景不需要持续高频截图
* 可以显著降低本地资源消耗

### 决策 3：对外只接受完整动作

原因：

* 降低网络抖动导致的异常状态
* 让 agent 执行逻辑更接近“原子动作执行器”

### 决策 4：断线后重置鼠标状态

原因：

* 避免按钮卡住
* 让新连接从干净状态恢复
