# Game Desktop Agent 连接、日志与 Session UX 修复方案

本文档作为 `design/game/game-agent-step2.md` 与 `design/game/desktop-screenshot-refactor.md` 的补充方案，针对 desktop 在发送截图时暴露出的 agent 连接假成功、错误日志不可读、trace/correlation 信息不足，以及 session/agent 操作流程不顺畅等问题进行收敛设计。

## 背景

当前 desktop 已具备 session 列表、agent 操作、WebSocket 连接、窗口绑定、截图和发送截图能力。但实际使用中出现以下现象：

1. 发送截图时 desktop 显示 `Receive screenshot ack failed`，服务端日志中难以定位对应错误。
2. SigNoz trace 显示真实错误为 `agent owner not found`，但 desktop 只看到 gateway WebSocket close frame：`StatusInternalError` / `internal error`。
3. desktop UI 显示 WebSocket 已连接，但真正的 agent owner 校验发生在首个 `AgentFrame` 到达 proxy 后，导致连接状态与业务可用状态不一致。
4. 日志面板会截断长错误，关键错误原因、trace id 或 JSON 字段不可见。
5. session 列表点击后直接进入详情页，详情页没有删除 session；返回列表又清空选中态，导致删除当前 session 的路径不顺畅。
6. 启动 desktop 后需要用户手动刷新 session；进入 session 详情后需要手动 Get Agent，这些应改为自动加载。

## 目标

完成本方案后应达到以下效果：

1. desktop 启动后自动刷新 session 列表，用户无需先点击 Refresh 才能看到已有 session。
2. 进入 session agent 页面后自动获取当前 session 的 agent 信息；移除显式 `GetSession` / `GetAgent` 操作入口，将查询动作作为页面加载和刷新行为。
3. Session 详情页提供刷新按钮，用于重新拉取当前 session 的 agent 信息和连接状态相关展示。
4. `Connect Agent` 不再只代表 WebSocket 握手成功，而必须完成一次应用层 frame round-trip，确认 proxy 能找到 agent owner 且 owner agent stream 可用后，desktop 才显示 Connected。
5. 当 agent owner 不存在、agent stream 建立失败或转发失败时，desktop 能在连接阶段或首次操作阶段展示明确错误，并在本地日志中提供可用于 SigNoz 查询的 correlation 信息。
6. gateway/proxy 在 `ConnectAgent` 和 WebSocket bind 关键失败路径记录结构化日志，不再只给客户端返回泛化的 `internal error`。
7. desktop 日志面板可以查看完整日志内容，长错误、trace id、capture id 和 frame id 不被截断。
8. session 删除路径清晰：用户在详情页可以删除当前 session，删除后回到 session 列表并刷新。

## 非目标

本方案不包含以下内容：

1. 不改变截图实现、截图坐标语义或 `AgentScreenshotFrame` 字段；相关内容继续遵循 `design/game/desktop-screenshot-refactor.md`。
2. 不引入连续截图流、chunk 传输或图片压缩协商。
3. 不实现 agent 对鼠标、键盘的真实操作命令执行。
4. 不引入用户认证、权限、多租户或审计系统。
5. 不要求 desktop 日志远程上报；desktop 仍只保留本地 UI 日志，但日志内必须包含可复制的 trace/correlation 信息。
6. 不要求 gateway/proxy 向客户端暴露内部敏感错误详情；客户端错误可以做安全化展示，但服务端日志必须保留原始错误。

## 现状问题

### 日志显示截断

`projects/game/desktop/frontend/src/components/LogPanel.svelte` 当前使用单行布局：

```css
.log-line {
  white-space: nowrap;
}

.log-msg {
  overflow: hidden;
  text-overflow: ellipsis;
}
```

这会隐藏长错误内容。截图发送失败时，`reason = "internal error"`、trace id 或 JSON fields 都可能不可见。

### trace/correlation 信息不完整

desktop Go 后端已经在部分 HTTP 操作中使用 `tracecontext.Ensure` 并打印 `trace_id`，例如创建 session、列 session、创建 agent、连接 agent。但以下路径仍缺少统一字段：

1. `ListWindows`、`BindWindow`、`CaptureScreenshot` 没有 trace/correlation 字段。
2. `SendScreenshot` 只记录 `capture_id`、`frame_id`、`size`，失败日志没有同步记录 `trace_id`、`session_id`、`capture_id`、`frame_id`。
3. frontend 自己的 `log()` 调用只写 message，不带字段。
4. gateway `handleWebSocketConnect` 使用 `log.Printf`，且 `b.Bind` 失败时不记录原始错误。
5. proxy `connectAgenter.Connect` 的 owner lookup 失败和 inner bind 失败缺少日志。

### ConnectAgent 假连接

desktop 当前连接流程为：

```text
frontend handleConnectAgent
  -> Wails App.ConnectAgent
  -> WSClient.Connect
  -> websocket.Dial 成功
  -> frontend wsConnected = true
```

但服务端 owner 校验流程为：

```text
gateway handleWebSocketConnect
  -> websocket.Accept 成功
  -> proxyClient.ConnectAgent 建立 gRPC stream
  -> bind 等待第一帧
  -> proxy connectAgenter.Connect 读取第一帧
  -> ownerStore.Get(session_id)
```

因此 WebSocket upgrade 成功不代表 agent 可用。`agent owner not found` 会延迟到第一帧（例如 screenshot frame）发送后才暴露。

### Session 操作路径不顺畅

当前 session 列表页的 Delete 按钮依赖 `selectedSessionId`。点击 session row 会进入详情页；详情页没有 Delete Session。返回列表时 `handleBackToSessions` 会清空 `selectedSession`，导致 Delete 按钮禁用。用户无法自然地删除当前详情页中的 session。

### 自动加载缺失

当前 desktop 启动后不会自动执行 `ListSessions`；进入 session 详情后也不会自动执行 `GetAgent`。用户需要手动 Refresh 和 Get Agent，导致页面状态容易过期，也增加了进入 play 前的操作步骤。

## 已确认决策

1. `wsConnected` 的语义改为“应用层 agent stream 已验证可用”，不再表示 WebSocket TCP/HTTP upgrade 成功。
2. `ConnectAgent` 成功条件必须包含一次初始 `AgentFrame` round-trip。建议使用 `status` payload 作为 ping，收到 `status` 或明确 ack 后才视为连接成功。
3. gateway/proxy 仍可以保持“首帧决定 owner 路由”的架构，但 desktop 必须在连接阶段主动发送首帧来触发校验，而不是等到发送截图时才触发。
4. 服务端日志记录真实错误；客户端可展示安全化错误，但本地日志要包含 trace/correlation id 以便查询服务端 trace。
5. 移除 UI 中显式 `GetSession` / `GetAgent` 操作。查询动作由页面进入、刷新按钮和关键状态变更自动触发。
6. Session 详情页必须提供 Delete Session；删除成功后返回列表并刷新。

## 模型设计

### ConnectionState

frontend 不再只使用 boolean 表达连接状态。建议使用显式状态模型：

```ts
type ConnectionState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'error'
```

字段含义：

1. `disconnected`：没有可用 WS client，或用户已关闭/切换 session。
2. `connecting`：WebSocket 已开始连接或正在执行初始 round-trip。
3. `connected`：初始 round-trip 成功，agent owner 和 owner agent stream 可用。
4. `error`：连接或验证失败，错误详情展示在页面和日志中。

如短期保持 `wsConnected: boolean`，也必须保证它只在 round-trip 成功后设为 `true`。

### LogEntry

desktop 日志条目继续保留现有字段，并扩大 `fields` 的类型和使用范围：

```ts
interface LogEntry {
  time: string
  level: string
  source: string
  message: string
  fields?: Record<string, unknown>
}
```

建议所有网络、连接、截图相关日志统一使用以下字段：

| 字段 | 说明 |
|---|---|
| `trace_id` | HTTP/WS upgrade 可在 SigNoz 查询的 trace id。 |
| `correlation_id` | 对本地流程稳定的排查 id；当没有真实 trace 时仍可关联多条本地日志。 |
| `session_id` | 当前 session id。 |
| `agent_name` | 如已知，记录 `sessions/{session_id}/agent`。 |
| `capture_id` | screenshot payload 的 capture id。 |
| `frame_id` | `AgentFrame.frame_id`。 |
| `size` | screenshot PNG 原始 byte 数。 |
| `error` | 原始错误字符串。 |

### AgentRefreshResult

进入 session 详情页和点击刷新按钮时，frontend 需要拉取 agent 信息。UI 可使用以下本地状态表达结果：

```ts
type AgentLoadState = 'idle' | 'loading' | 'loaded' | 'not_found' | 'error'
```

`not_found` 不应作为全局错误阻塞详情页，而应在 agent 区域提示“当前 session 尚未创建 agent”，并保留 Create Agent 按钮。

### ConnectProbe Frame

连接验证使用现有 `AgentFrame.status` payload，不新增 proto 字段：

```json
{
  "sessionId": "...",
  "frameId": "connect-probe-...",
  "status": { "status": "ping" }
}
```

预期响应为 `AgentFrame.status`，其内容来自 agent runtime 当前状态。只要完成一次 send/recv round-trip，即表示 gateway -> proxy -> owner agent stream 可用。

## 代码分层

### desktop frontend

涉及文件：

1. `projects/game/desktop/frontend/src/App.svelte`
2. `projects/game/desktop/frontend/src/components/SessionList.svelte`
3. `projects/game/desktop/frontend/src/components/SessionDetail.svelte`
4. `projects/game/desktop/frontend/src/components/PlayView.svelte`
5. `projects/game/desktop/frontend/src/components/LogPanel.svelte`
6. `projects/game/desktop/frontend/src/logger.ts`

职责调整：

1. `App.svelte` 管理页面状态、自动加载、连接验证和刷新动作。
2. `SessionList` 只负责展示列表、创建、刷新、选择；删除可以保留但不作为唯一删除路径。
3. `SessionDetail` 展示 session + agent 状态，提供 Create Agent、Delete Agent、Delete Session、Refresh、Connect Agent、Enter Play。
4. `LogPanel` 负责完整日志展示、展开/折叠和复制完整内容。

### desktop Go 后端

涉及文件：

1. `projects/game/desktop/app.go`
2. `projects/game/desktop/internal/api/websocket.go`
3. `projects/game/desktop/internal/applog/logger.go`
4. `projects/game/desktop/internal/trace/transport.go`

职责调整：

1. `App.ConnectAgent` 连接 WebSocket 后立即发送 connect probe，并等待响应；失败时关闭 WS 并返回错误。
2. `App.SendScreenshot` 记录完整 correlation 字段。
3. `WSClient` 提供带 context 的 `SendFrame` / `RecvFrame` 变体，避免使用 `context.Background()` 导致取消和超时不可控。
4. `applog.Entry.Fields` 保持 `map[string]any`，frontend 类型同步为 `Record<string, unknown>`。

### gateway

涉及文件：

1. `projects/game/gateway/cmd/main.go`

职责调整：

1. `handleWebSocketConnect` 在 `b.Bind(ws, stream)` 返回非 clean/protocol 错误时记录原始错误。
2. 日志字段包含 `session_id`、HTTP request trace id、close code 和 error。
3. 继续向客户端返回安全化 close reason；是否暴露原始错误由单独决策控制。

### proxy

涉及文件：

1. `projects/game/proxy/service/connect_agenter.go`
2. `projects/game/proxy/handler/handler.go`

职责调整：

1. owner lookup 失败时记录结构化日志，字段包含 `session_id` 和 error。
2. agent stream 建立成功继续记录 `agent stream connected`。
3. inner `c.binder.Bind` 返回错误时记录结构化日志，字段包含 `session_id`、`agent_index`、error。

## 关键细节

### 启动自动刷新 session

desktop frontend 在 mount 后自动调用 `handleRefresh()`：

1. 初始页面展示 loading 或空列表提示。
2. 自动刷新失败时显示错误，并在日志中记录 `source=sessions`。
3. 用户仍可点击 Refresh 重新加载。

如果 Svelte 组件使用 `$effect`，必须避免依赖变化导致循环刷新；应使用一次性初始化标记。

### 进入 session 详情自动获取 agent

`handleSelectSession(session)` 不应只设置 `page='detail'`，还应触发 agent 加载：

```text
select session
  -> selectedSession = session
  -> page = detail
  -> agentLoadState = loading
  -> getAgent(session.sessionId)
     -> success: agent = result, loaded
     -> 404 NotFound: agent = null, not_found
     -> other error: error
```

`Get Agent` 按钮移除，改为 `Refresh Agent` 或详情页统一 `Refresh` 按钮。

### 移除 GetSession / GetAgent 操作

UI 不再提供手动 `GetSession` / `GetAgent` 按钮：

1. session 信息来自列表项和自动刷新。
2. agent 信息进入详情自动获取。
3. 详情页 Refresh 重新执行 `GetAgent`，必要时也可刷新当前 session 基本信息。

### Connect Agent 应用层握手

`App.ConnectAgent(sessionID)` 的新语义：

```text
WS Dial
  -> build AgentFrame{status: ping, frame_id: connect-probe-*}
  -> SendFrame(ctx, frame)
  -> RecvFrame(ctx)
  -> expect status frame or explicit ack
  -> success: store ws/sessionID
  -> failure: close ws, return error
```

这样可以立即触发 proxy owner lookup：

1. owner 不存在时，`ConnectAgent` 直接失败，UI 不进入 Connected。
2. owner agent 不可达时，`ConnectAgent` 直接失败。
3. 只有真实 stream 可用时，用户才能进入 Play。

### 错误展示

本地错误展示分两层：

1. 页面 error bar：展示面向用户的简洁错误，例如“连接 agent 失败：agent owner not found”。
2. LogPanel：展示完整错误链和 fields，例如 trace_id、session_id、frame_id、close_code。

LogPanel 至少满足：

1. 默认可换行显示完整 message。
2. fields 以 JSON 展示时可完整复制。
3. 单条日志可展开/折叠；如果暂不实现展开，必须取消 `text-overflow: ellipsis`。

### Session 删除

Session 详情页新增 Delete Session：

```text
Click Delete Session
  -> call deleteSession(selectedSession.sessionId)
  -> success: selectedSession = null, agent = null, wsConnected = false, close WS if needed
  -> page = sessions
  -> refresh sessions
```

如果当前 WS 已连接，删除 session 前应先关闭本地 WS，避免 UI 保持旧连接状态。

### trace 与 correlation

desktop 每次 HTTP/WS 操作继续使用 `tracecontext.Ensure` 生成或复用 trace。截图发送还应生成本地 `correlation_id`，用于连接以下日志：

1. capture start / capture success / capture failed
2. screenshot frame build
3. send frame
4. receive ack / receive failed

`SendScreenshot` 的失败日志必须包含与发送日志相同的 `capture_id`、`frame_id`、`session_id` 和 `correlation_id`。

### gateway/proxy 日志

gateway 对以下事件记录日志：

1. WebSocket accepted：debug 或 info，包含 `session_id`。
2. `proxyClient.ConnectAgent` 创建 stream 失败：error。
3. `b.Bind` 返回非 clean/protocol error：error，包含原始 error 和关闭给客户端的 close code。
4. protocol error：warn，包含 `session_id`。

proxy 对以下事件记录日志：

1. 首帧缺失或 session_id 为空。
2. owner lookup 失败。
3. owner agent connection 获取失败。
4. owner agent stream 建立失败。
5. inner bind 返回错误。

## 决策详情

### 为什么用应用层 round-trip 而不是仅依赖 WebSocket Dial

WebSocket Dial 只证明 gateway 接受了 HTTP upgrade。当前 owner 路由依赖首个 `AgentFrame.session_id`，因此业务连接可用性必须通过发送首帧才能验证。应用层 round-trip 能复用现有协议和路由，不需要 gateway 在 upgrade 前新增 owner 查询职责。

### 为什么不把真实错误直接放进 WebSocket close reason

close reason 会直接暴露给客户端，可能包含内部服务细节。更稳妥的做法是：服务端日志记录真实错误；客户端收到安全化错误和 correlation id。后续如需要更友好的客户端错误，可在 `AgentFrame` 中新增明确 error payload，本方案先不引入 proto 变更。

### 为什么移除 GetSession/GetAgent 按钮

Get 操作是数据加载行为，不是用户核心任务。进入页面自动获取并提供 Refresh 按钮，能减少用户操作、降低状态过期概率，也符合正式客户端而非调试工具的定位。

### 为什么详情页保留 Delete Agent 并新增 Delete Session

Agent 和 Session 是不同资源。用户在 session 详情页需要同时能清理 agent 或删除整个 session。删除 session 应是当前 session 的显式危险操作，不能依赖列表页选中态。

## 实施步骤

### Step 1: 修复日志面板完整展示

修改 `LogPanel.svelte`：

1. 移除 `.log-msg` 的 `overflow: hidden` / `text-overflow: ellipsis`。
2. 将日志行改为可换行或增加展开态。
3. fields 使用 `Record<string, unknown>`，展示完整 JSON。

验收：长错误完整可见或可展开查看，trace id 不被截断。

### Step 2: 补齐 desktop correlation 字段

修改 `app.go` 和 frontend logger：

1. `SendScreenshot` 的所有日志携带 `session_id`、`capture_id`、`frame_id`、`correlation_id`、`size`。
2. 失败日志复用相同字段。
3. `ListWindows`、`BindWindow`、`CaptureScreenshot` 至少携带 `correlation_id`；如使用 trace context，也记录 `trace_id`。
4. frontend 自发日志支持 fields。

验收：一次发送截图失败时，LogPanel 中所有相关日志能用同一个 `correlation_id` 串起来。

### Step 3: ConnectAgent 应用层验证

修改 `App.ConnectAgent` 与 `WSClient`：

1. `WSClient.SendFrame` / `RecvFrame` 支持传入 context。
2. `App.ConnectAgent` 在 Dial 成功后发送 `status: ping` frame。
3. 等待响应，成功后才保存 `a.ws` 和 `a.sessionID`。
4. 失败时关闭 WS 并返回错误。
5. frontend 只有 `ConnectAgent` 返回成功后才设置 connected 状态。

验收：对没有 agent owner 的 session 点击 Connect Agent 直接失败，不允许进入 Play。

### Step 4: Session/Agent 自动加载与 UX 修复

修改 `App.svelte` 和 `SessionDetail.svelte`：

1. desktop 启动后自动执行 `ListSessions`。
2. 选择 session 后自动执行 `GetAgent`。
3. 移除 Get Agent 操作按钮，增加 Refresh 按钮。
4. 增加 Delete Session 按钮。
5. 删除 session 后关闭本地 WS、清理状态、回到列表并刷新。

验收：启动即显示 session；进入详情自动显示 agent 或“未创建 agent”；详情页可删除当前 session。

### Step 5: gateway/proxy 可观测性修复

修改 `gateway/cmd/main.go` 和 `proxy/service/connect_agenter.go`：

1. gateway 记录 bind error 和 close code。
2. proxy 记录 owner lookup failure 和 bind failure。
3. 日志字段包含 `session_id`、error 和可用 trace/correlation 信息。

验收：复现 `agent owner not found` 时，SigNoz logs 或 pod logs 中能直接看到 `session_id` 和原始错误。

## 测试方案

### 单元测试

1. `desktop/internal/api/websocket_test.go`：新增 connect probe round-trip 测试。
2. `desktop` Go 测试：`ConnectAgent` 在 probe 失败时关闭 WS 并返回错误。
3. `gateway/cmd/main_test.go`：gRPC stream NotFound / Internal error 时验证 close 行为不变，并覆盖日志 hook（如实现可测试日志）。
4. `proxy/service/connect_agenter_test.go`：owner not found 和 inner bind error 保持正确 status，并覆盖日志字段（如日志包支持测试 reporter）。
5. frontend 逻辑如有测试框架，覆盖启动自动刷新、选择 session 自动加载 agent、删除 session 后回到列表。

### 构建验证

```bash
bazel test //projects/game/desktop/...
bazel test //projects/game/gateway/...
bazel test //projects/game/proxy/...
bazel build //projects/game/desktop:desktop
```

### 手动验收

在 Windows desktop 客户端执行：

1. 启动客户端，session 列表自动加载。
2. 选择一个没有 agent 的 session，详情页显示 agent 未创建。
3. 点击 Connect Agent，预期失败并显示明确错误，不进入 Connected。
4. 点击 Create Agent 后，自动刷新或手动 Refresh 能显示 agent 信息。
5. 点击 Connect Agent，连接成功后才能 Enter Play。
6. 绑定窗口、截图、发送截图，成功时显示 ack；失败时日志包含完整错误和 correlation 字段。
7. 在详情页点击 Delete Session，成功后返回 session 列表并刷新，旧连接状态被清理。
8. LogPanel 中长错误完整可见，可复制 trace/correlation 信息。

## 验收标准

1. `wsConnected=true` 只在应用层 `AgentFrame` round-trip 成功后出现。
2. 没有 agent owner 的 session 不会被 UI 标记为 Connected。
3. desktop 启动自动加载 session 列表。
4. 进入 session 详情自动加载 agent 信息，且 UI 不再提供 Get Agent 调试按钮。
5. 详情页可以删除当前 session。
6. 截图发送失败时，本地日志完整展示错误，并包含 `session_id`、`capture_id`、`frame_id` 和 `correlation_id`。
7. gateway/proxy 对 `ConnectAgent` 关键失败路径有服务端日志，可通过 `session_id` 或 trace 查询。
8. 相关 Bazel 测试和 desktop 构建通过。

## 风险与注意事项

1. connect probe 会改变 `ConnectAgent` 的时序：连接时会立即产生一帧 `status` 请求。agent handler 当前支持 status payload，应保持兼容。
2. 如果 `ConnectAgent` 失败后没有正确关闭 WS，后续操作可能复用已坏连接；失败路径必须主动 `Close` 并清理 `a.ws`。
3. `tracecontext.Ensure(a.ctx)` 如果复用同一 Wails app context，可能导致多个操作共享同一 trace id。若需要每次操作独立 trace，应从 `context.Background()` 或派生新 context 生成新的 trace。该点实现时需明确选择。
4. 详情页自动 `GetAgent` 的 NotFound 应作为正常状态处理，不能让整个页面进入错误状态。
5. 删除 session 可能触发级联删除 agent；若服务端删除 agent 返回 NotFound，应遵循已有 session 删除幂等设计。

## 未来规划

1. 在 `AgentFrame` oneof 中新增显式 error payload，让 gateway/proxy/agent 可以向 desktop 返回结构化业务错误，而不是依赖 WebSocket close reason。
2. 将 desktop 日志支持导出为 JSON 文件，便于用户提交问题时附带完整本地诊断信息。
3. 为 session 列表增加搜索、分页加载更多和最近使用排序。
4. 将 service.name 从统一 `game` 细分为 `game/gateway`、`game/proxy`、`game/agent`、`game/session`，降低 SigNoz 查询成本。
