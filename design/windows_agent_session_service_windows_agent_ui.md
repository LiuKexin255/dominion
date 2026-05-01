# Windows Agent session UI 与传输控制方案

## 目标

本方案用于落定 `projects/game/windows_agent` 的界面和本地运行时改造，目标是：

* 将当前单页左右栏界面升级为上方主工作区和下方日志区。
* 在主工作区提供 `连接`、`窗口捕获`、`调试` 三个 tab。
* 在下方提供日志 tab，集中展示 info、warn、error 日志。
* 让用户通过 `session service` 的 session 列表选择、创建、连接和删除 session。
* 让窗口捕获和视频传输由用户显式操作，保留 `开始传输` 与 `停止传输` 按钮。
* 为调试提供本地截图按钮和截图预览。

本方案希望达成的效果是：操作者打开 `windows_agent` 后，可以清楚完成“选择 session -> 连接 gateway -> 选择窗口 -> 开始传输 -> 查看日志/调试截图”的完整流程，并能从 UI 与日志中判断 session、gateway、窗口捕获、ffmpeg、input-helper 和媒体传输是否正常。

## 范围

本方案仅覆盖 `projects/game/windows_agent`：

* Wails 前端布局与组件设计。
* Wails app binding 设计。
* session-service HTTP client。
* gateway 连接与 reconnect fallback。
* 窗口选择、清除、开始传输、停止传输。
* 本地截图调试。
* UI 内存日志与监控日志事件。

本方案依赖 `session service` 的接口调整，见 `design/windows_agent_session_service_sessions_ui_support.md`。

本方案不包括：

* session 服务的 `ListSessions` 实现。
* web 远程操作页面。
* 日志落盘。
* gateway snapshot 验证按钮。
* 视频 parser 的完整流式重构。

## 界面设计

### 总体布局

界面分为上下两部分：

* 上方主区域：承载主要操作 tab。
* 下方次要区域：承载日志 tab。

主区域 tab：

* `连接`
* `窗口捕获`
* `调试`

下方 tab：

* `日志`

视觉风格延续当前深色 slate 工具台风格，重点改进信息结构而非重新设计品牌视觉。

### 连接 tab

职责：管理 session 与 gateway 连接。

展示内容：

* session service host：固定 `https://game.liukexin.com`。
* 当前 agent 状态：Disconnected、Connected、Bound、Streaming、Error。
* 当前连接 session：name、type、status、gateway id、reconnect generation。
* session 列表：name、type、status、gateway id、create time、update time。

操作：

* 刷新 session 列表。
* 新增 session。
* 连接 session。
* 删除 session。
* 断开当前连接。

新增 session：

* UI 必须提供 session type 选择。
* 第一版选项来自当前 proto：`SAOLEI`。
* 创建成功后刷新列表，并将新 session 高亮；不自动连接。

连接 session：

1. 用户点击某个 session 的连接按钮。
2. `windows_agent` 优先使用该 session 的 `agent_connect_url` 连接 gateway。
3. 如果连接失败，自动调用 `ReconnectSession(name)`。
4. 使用返回的 `session.agent_connect_url` 再连接一次。
5. 第二次失败才展示错误并写日志。

删除 session：

* 如果删除的是当前连接 session：
  1. 如果正在传输，先停止传输。
  2. 断开 gateway。
  3. 断开成功后调用 `DeleteSession`。
  4. 断开失败则不删除，并展示错误。
* 如果删除的是非当前连接 session：直接调用 `DeleteSession`。

### 窗口捕获 tab

职责：选择、清除捕获窗口，并手动控制传输。

展示内容：

* 当前捕获窗口卡片：title、HWND、class name、process id、rect。
* 当前传输状态：未选择、已选择、已连接未传输、传输中、错误。
* 可捕获窗口列表。

操作：

* 刷新窗口列表。
* 选择捕获窗口。
* 清除捕获窗口。
* 开始传输。
* 停止传输。

按钮规则：

* `开始传输`：仅在已连接 session、已选择窗口、当前未传输时启用。
* `停止传输`：仅在 `Streaming` 时启用。
* `清除捕获窗口`：如果正在传输，先停止传输，再清除绑定。

选择窗口后不自动传输，必须由用户点击 `开始传输`。

### 调试 tab

职责：验证 gateway snapshot 是否能从当前 session 的视频流中生成并返回，用来确认 agent 到 gateway 的视频传输链路正常。

展示内容：

* 验证截图按钮。
* gateway snapshot 预览。
* snapshot 元信息：时间、尺寸、session、gateway、错误信息。

第一版调试截图以 gateway snapshot 为准，不再只做本地截图预览。

截图实现建议：

* Wails binding 调用 gateway REST 接口：`GET /v1/sessions/{id}/game/snapshot`。
* 后端从 gateway response 中取图片内容并返回 `data:image/jpeg;base64,...` 或等价可展示结果。
* 前端使用 `<img>` 展示 gateway 返回的 snapshot。
* 验证截图要求当前 session 已连接，且最好已经开始传输；未传输时允许请求但应提示可能没有可用 snapshot。

### 日志 tab

职责：展示本次运行的结构化日志。

日志级别：

* info
* warn
* error

功能：

* 自动滚动。
* 清空日志。
* level filter。
* 保留最近 500 到 1000 条。

第一版只做 UI 内存日志，不落盘。

## 模型设计

### 前端 Session 模型

前端使用 session service 返回的 `Session`：

* `name`
* `type`
* `status`
* `gatewayId`
* `createTime`
* `updateTime`
* `reconnectGeneration`
* `lastError`
* `agentConnectUrl`

UI 不展示完整 `agentConnectUrl`，只在连接时传给 Go app 层。

### AgentStatus

现有 `AgentStatus` 需要继续保留并补齐实际更新：

* `state`
* `sessionId`
* `boundWindow`
* `mediaSegCount`
* `lastError`
* `ffmpegRunning`
* `helperRunning`
* `connectedAt`

建议新增：

* `sessionName`
* `sessionType`
* `gatewayId`
* `streamingStartedAt`

### LogEntry

后端向前端发送结构化日志：

* `timestamp`
* `level`
* `module`
* `message`
* `fields`

日志中禁止包含完整 `agent_connect_url` 或 token。

## 代码分层

建议按现有目录扩展：

* `frontend/src/App.svelte`：改为主 tab + 下方日志布局。
* `frontend/src/lib/ConnectionPanel.svelte`：改为 session 列表与连接管理。
* `frontend/src/lib/WindowPanel.svelte`：改为窗口捕获和传输控制。
* `frontend/src/lib/DebugPanel.svelte`：新增截图调试 tab。
* `frontend/src/lib/LogPanel.svelte`：接入下方日志区域并增加过滤/清空。
* `frontend/src/lib/wails.d.ts`：补充 session、截图、capture bindings 类型。
* `internal/app`：新增 Wails bindings、状态更新和日志事件。
* `internal/sessionclient`：新增 session service REST client。
* `internal/runtime`：补齐 StartCapture/StopCapture/ClearWindow/ReadLoop 消费。
* `internal/capture`：复用现有窗口捕获策略，增加本地截图能力或截图 helper。
* `internal/encoder`：确保默认 runtime 注入 ffmpeg encoder。
* `internal/input`：确保默认 runtime 注入 input manager。

## Wails Binding 设计

建议新增或调整以下方法：

```go
ListSessions() ([]Session, error)
CreateSession(sessionType string) (Session, error)
ConnectSession(session Session) error
Disconnect() error
DeleteSession(name string) error

EnumerateWindows() ([]window.WindowInfo, error)
BindWindow(hwnd uintptr) error
ClearWindow() error
StartCapture() error
StopCapture() error

TakeScreenshot() (ScreenshotResult, error)
GetStatus() AgentStatus
```

说明：

* `ConnectSession` 内部执行“先用 session.agent_connect_url，失败后 reconnect 再重试”的策略。
* `DeleteSession` 内部处理当前连接 session 的停止传输和断开逻辑。
* `StartCapture` 必须检查已连接 session 和已绑定窗口。

## 关键细节

### session host

`session service` host 固定：

* `https://game.liukexin.com`

gateway URL 不由 `windows_agent` 拼接，完全使用 session service 返回的 `Session.agent_connect_url`。

### runtime 必须补齐的闭环

当前代码中已有部分能力，但还未完整串起来。实施 UI 前必须补齐：

* `Runtime.StartCapture` 暴露到 app 层。
* `Runtime.StopCapture` 暴露到 app 层。
* `ClearWindow` 清除绑定窗口。
* 默认 runtime 初始化 `encoder.NewEncoder(ffmpegPath)`。
* 默认 runtime 初始化 `input.NewManager()`。
* 启动 gateway `ReadLoop` 并消费消息。
* 将 `control_request` 路由到 `handleControlRequest`。
* 将 `ping` 路由到 `SendPong`。
* 将 gateway error 写入状态和日志。

如果不补齐 `ReadLoop`，web 发来的鼠标控制不会生效。如果不暴露 `StartCapture`，UI 的开始传输按钮无法真正工作。

### 传输控制

传输状态由用户显式控制：

* 选择窗口不自动开始传输。
* 连接 session 不自动开始传输。
* 只有点击 `开始传输` 才启动 ffmpeg 和媒体发送。
* 断开 session 前如正在传输，先停止传输。

### 日志与监控

必须记录的日志事件：

* app startup/shutdown。
* session list/create/delete/reconnect。
* gateway connect/disconnect/connect retry。
* window selected/cleared。
* capture start/stop/failure。
* ffmpeg start、stderr 关键错误、退出。
* media init sent。
* media segment count 周期性汇总。
* input-helper start/stop/failure。
* control request ack/result。
* ping/pong 或 websocket read loop error。

日志展示到 UI，同时可写 Go/Wails runtime log。第一版不落盘。

### 截图调试

第一版截图用于验证 gateway snapshot：

* 必须有当前连接 session。
* 建议在已开始传输后点击验证截图。
* 点击后由 Wails backend 请求 gateway `GetGameSnapshot` REST 接口。
* 成功后显示 gateway 返回的 snapshot 图片和元信息。
* 如果 gateway 还没有缓存到视频帧，应显示“暂无 snapshot”并写 warn 日志。
* 请求失败时写 error 日志并显示错误。

## 决策详情

### 决策 1：用户手动连接 session

选择：启动后只自动刷新 session 列表，不自动连接。

原因：

* 用户需要明确选择目标 session。
* 避免启动 app 后误连接或误占用 gateway agent 连接。
* 与删除、重连、窗口选择的操作语义更清晰。

### 决策 2：session 类型由 UI 选择

选择：新建 session 时 UI 必须选择 session type。

原因：

* session type 是 session 资源的一部分。
* 后续扩展其他游戏类型时不需要改交互模型。
* 避免 agent 隐式假设固定游戏。

### 决策 3：保留开始传输按钮

选择：连接 session 和选择窗口后仍需用户点击 `开始传输`。

原因：

* 传输会启动 ffmpeg 和占用资源，应由用户显式触发。
* 用户可以先确认窗口选择正确，再开始推流。
* 调试问题时可以单独验证连接和窗口捕获。

### 决策 4：删除当前 session 前先断开

选择：删除当前连接 session 时，先停止传输并断开 gateway，断开成功后才删除 session。

原因：

* 避免本地 agent 仍持有已删除 session 的 gateway 连接。
* 避免 session 服务状态和 agent 本地状态不一致。
* 出错路径更容易向用户解释和恢复。

### 决策 5：连接失败后自动 ReconnectSession

选择：优先使用列表中的 `agent_connect_url`，失败后自动 reconnect 并重试一次。

原因：

* 快速路径简单，避免每次连接都重新分配 gateway。
* reconnect 能处理 token 过期、gateway 不可用或旧连接信息失效。
* 用户只点击一次连接，agent 内部完成一次可靠恢复尝试。

## 验收标准

* App 启动后可以刷新并展示 session 列表。
* 新建 session 时 UI 可以选择 session type。
* 点击连接 session 时，优先使用 `session.agent_connect_url`；失败后自动 `ReconnectSession` 并重试。
* 用户可以选择窗口、清除窗口。
* `开始传输` 仅在已连接且已选择窗口时可用。
* 点击 `开始传输` 后 ffmpeg 开始采集并向 gateway 发送 media init/segment。
* 点击 `停止传输` 后 ffmpeg 停止，gateway 连接保持。
* 删除当前 session 前会先停止传输并断开 gateway，失败则不删除。
* 调试 tab 可以请求 gateway snapshot 并展示返回截图。
* 日志 tab 能显示 info、warn、error，并包含连接、重连、捕获、传输和错误事件。
* Wails 前端测试和 Go 单元测试通过。

## 未来规划

未来可按需要扩展：

* 日志落盘与导出。
* session 列表分页、过滤和搜索。
* 视频 parser 真正流式化。
* 更准确的关键帧识别。
* Windows Graphics Capture 或 DXGI 捕获后端。
