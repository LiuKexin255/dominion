# Agent 玩游戏 step2 方案

本文档承接 `ideas/llm_agent_play_game/README.md` 中 step2 的目标，以及 `design/game/desktop-client.md`、`design/game/game-agent-step1-stream-conn-refactor-plan.md` 已确定的 step1 架构。step2 的核心目标是把 desktop 从连通性验证工具推进为正式的 session/agent 操作入口，并支持绑定普通 Windows 窗口、截取一张不含标题栏的 PNG 图片，通过 agent WebSocket 链路传递给 agent。

## 目标

完成 step2 后应达到以下效果：

1. desktop UI 从当前调试按钮页改为正式的 session 列表、session 操作页和 play 页面。
2. session 由服务端生成 `session_id`，desktop 不再在创建请求中指定 session id。
3. desktop 可以列出 session，选择一个 session 后进入 play 状态。
4. desktop 可以枚举并绑定一个普通 Windows 应用窗口。
5. desktop 可以截取绑定窗口的 client area 图片，截图不包含标题栏和窗口边框。
6. 截图编码固定为 PNG，传输时显式携带编码格式和原始像素尺寸。
7. agent 可以通过现有 WebSocket/gRPC stream 链路接收 screenshot frame，并返回明确 ack/status。
8. `AgentFrame` payload 在 proto 中显式定义，不再使用无约束 `bytes payload` + `type string` 表示业务语义。
9. desktop 与服务通信使用 `projects/game/game.proto` 中定义的 Go proto 类型，不再维护独立请求/响应 DTO 作为通信模型。
10. desktop 发出的 HTTP/WebSocket 请求设置 trace context，便于在服务端追踪请求链路；desktop 日志不接入远端上报。
11. desktop 日志显示改为一条日志一行，避免当前表格字段拆成多行造成阅读困难。

## 非目标

step2 不包含以下内容：

1. 不实现连续截图流或高帧率视频流；本阶段只要求手动发送单张截图。
2. 不支持无法通过普通 Win32 截图方式捕获的 GPU/DirectX 游戏窗口；后续遇到具体不支持的游戏时再评估 Windows Graphics Capture。
3. 不实现 agent 对鼠标、键盘的真实操作命令执行。
4. 不实现 LLM、DeepAgent、LangChain/LangGraph 或策略总结。
5. 不引入用户认证、权限、多租户、限流。
6. 不改变 gateway/proxy 的 owner 路由职责；gateway/proxy 继续只负责 frame 转发。
7. 不做图片压缩策略协商；step2 固定 PNG。

## 已确认决策

1. 真实游戏窗口截图能力：step2 先支持普通窗口。若后续某些游戏无法截图，再替换或补充 Windows Graphics Capture。
2. WebSocket 大图片传输上限：step2 目标为单张图片，不设计连续流和 chunk 协议。
3. 图片编码格式：固定为 PNG。
4. desktop 使用 proto 的方式：desktop Go 后端直接使用 Go proto 类型。
5. 窗口坐标语义：截图和后续坐标均基于窗口 client area 原始像素坐标，不包含标题栏。

## 总体架构

step2 继续沿用 step1 链路：

```text
desktop Wails
  ├─ HTTP/JSON unary
  │   -> gateway grpc-gateway
  │   -> session/proxy gRPC
  └─ WebSocket AgentFrame protojson
      -> gateway
      -> proxy ConnectAgent gRPC stream
      -> owner agent Connect gRPC stream
```

desktop 新增本地窗口能力：

```text
Svelte Play 页面
  -> Wails App methods
  -> desktop/internal/capture
  -> Win32 EnumWindows / PrintWindow / BitBlt
  -> image/png
  -> game.AgentFrame{screenshot}
  -> WebSocket
```

gateway 和 proxy 不理解 screenshot 业务内容，只转发 `AgentFrame`。agent 作为业务接收端识别 screenshot payload，并返回 ack/status。

## Proto 模型设计

### SessionService

新增 `ListSessions`，并将创建 session 改为服务端生成 id。

```proto
service SessionService {
  rpc CreateSession(CreateSessionRequest) returns (Session);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  rpc DeleteSession(DeleteSessionRequest) returns (google.protobuf.Empty);
}
```

建议模型：

```proto
message CreateSessionRequest {
}

message ListSessionsRequest {
  int32 page_size = 1;
  string page_token = 2;
}

message ListSessionsResponse {
  repeated Session sessions = 1;
  string next_page_token = 2;
}
```

`Session.session_id` 继续保留在响应模型中，但不再由创建请求提供。session id 生成放在 session service 内部，建议使用随机不可预测 id，例如 UUID/ULID 风格字符串；具体实现应封装在 session domain/service 层，测试中可替换 id generator。

### AgentFrame

将 `AgentFrame` 改为显式 oneof payload。示意：

```proto
message AgentFrame {
  string session_id = 1 [(google.api.field_behavior) = REQUIRED];
  string frame_id = 2;
  google.protobuf.Timestamp create_time = 3;

  oneof payload {
    AgentStatusFrame status = 10;
    AgentEchoFrame echo = 11;
    AgentScreenshotFrame screenshot = 12;
    AgentAckFrame ack = 13;
  }
}

message AgentStatusFrame {
  string status = 1;
}

message AgentEchoFrame {
  bytes data = 1;
}

message AgentAckFrame {
  string ack_frame_id = 1;
  string message = 2;
}
```

### Screenshot payload

截图 payload 固定表达原始 client area 像素。示意：

```proto
enum ImageEncoding {
  IMAGE_ENCODING_UNSPECIFIED = 0;
  IMAGE_ENCODING_PNG = 1;
}

message AgentScreenshotFrame {
  string capture_id = 1;
  ImageEncoding encoding = 2;
  bytes data = 3;
  int32 width_px = 4;
  int32 height_px = 5;
  int32 client_x_px = 6;
  int32 client_y_px = 7;
  double scale_factor = 8;
  string window_title = 9;
  google.protobuf.Timestamp capture_time = 10;
}
```

字段说明：

1. `encoding` step2 必须为 `IMAGE_ENCODING_PNG`。
2. `data` 是 PNG 原始 bytes；通过 WebSocket protojson 传输时自然表现为 base64 字符串。
3. `width_px` / `height_px` 是实际 PNG 解码后的像素尺寸，必须与截图 client area 尺寸一致。
4. `client_x_px` / `client_y_px` 是截图左上角在目标窗口 client area 坐标系中的偏移。step2 截取完整 client area，因此通常为 `0,0`。
5. 后续 agent 返回鼠标操作坐标时，应以该 client area 原始像素坐标为准。

## 服务端设计

### session

职责调整：

1. `CreateSession` 内部生成 session id。
2. MongoDB 继续保存最小数据：`session_id`、`create_time`。
3. 新增 `ListSessions`，从 MongoDB 按创建时间或 `_id` 稳定排序返回。
4. `DeleteSession` 继续向下传播 `proxy.DeleteAgent`，保持 step1 删除清理语义。

建议分层：

```text
projects/game/session/
  domain/
    id_generator.go
    model.go
    repository.go
  handler/
    handler.go
  runtime/mongo/
    repository.go
```

`SessionRepository` 需要补充列表能力：

```go
type SessionRepository interface {
    Create(ctx context.Context, session *Session) (*Session, error)
    Get(ctx context.Context, sessionID string) (*Session, error)
    List(ctx context.Context, pageSize int, pageToken string) (*ListSessionsResult, error)
    Delete(ctx context.Context, sessionID string) error
}
```

### gateway

gateway 继续使用 `protojson.UnmarshalOptions{DiscardUnknown: true}` 解析 WebSocket text frame。改动点：

1. 跟随新的 `AgentFrame oneof` 更新测试和示例 frame。
2. 继续从 URL path 注入 `session_id`，覆盖客户端 frame 中的 session id。
3. 不在 gateway 解析 screenshot 内容，不做图片大小或编码校验。
4. 保持 `bind.Binder` 转发模型。

### proxy

proxy 不理解 screenshot payload。改动点仅限：

1. 跟随 proto 更新编译和测试。
2. `ConnectAgent` 继续从首帧获取 `session_id` 后路由到 owner agent。
3. owner 查询、agent stream 建立、`bind.WithFirstFrame` 行为保持不变。

### agent

agent 在 `Connect` 中识别新的 oneof payload：

1. `status`：返回当前 status。
2. `echo`：返回 echo。
3. `screenshot`：校验 encoding 为 PNG、`data` 非空、尺寸为正；记录已接收截图，并返回 `ack`。

step2 不要求 agent 解码图片或进行 LLM 分析，但建议在 handler/runtime 边界保留 domain 语义方法：

```go
type Runtime interface {
    Create(ctx context.Context, sessionID string) (*Status, error)
    Delete(ctx context.Context, sessionID string) error
    Status(ctx context.Context, sessionID string) (*Status, error)
    ReceiveScreenshot(ctx context.Context, input ScreenshotInput) (*ScreenshotReceipt, error)
}
```

这样后续替换为真实 DeepAgent runtime 时，不需要让 runtime 感知 gRPC stream 细节。

## Desktop 设计

### 分层

建议目录：

```text
projects/game/desktop/
  app.go
  internal/
    api/
      client.go
      websocket.go
      protojson.go
    capture/
      window.go
      windows.go
      capture.go
      png.go
    trace/
      transport.go
    applog/
      logger.go
  frontend/src/
    App.svelte
    api.ts
    components/
      SessionList.svelte
      SessionDetail.svelte
      PlayView.svelte
      LogPanel.svelte
```

### Proto 使用

desktop Go 后端直接使用 `dominion/projects/game` 生成的 Go proto 类型：

1. HTTP 请求体用 `protojson.Marshal` 生成。
2. HTTP 响应用 `protojson.Unmarshal` 解析。
3. WebSocket frame 用 `protojson.Marshal/Unmarshal`。
4. Wails 暴露给 Svelte 的方法可以返回 proto 类型可 JSON 化的结果；如 Wails 对 proto 类型的 JSON 表现不稳定，可在 Wails 边界做只读 view model，但 view model 不作为通信模型，也不用于 HTTP/WebSocket 序列化。

### UI

#### Session 列表页

功能：

1. Apply Config。
2. Refresh Sessions。
3. Create Session。
4. Delete Session。
5. 选择 session 后进入详情页。

#### Session 详情页

功能：

1. 展示 session 基本信息。
2. Create/Get/Delete Agent。
3. Connect Agent WebSocket。
4. Enter Play。

#### Play 页面

功能：

1. List Windows。
2. Bind Window。
3. Capture Screenshot。
4. Preview Screenshot。
5. Send Screenshot To Agent。
6. 展示 agent ack。

Play 状态机建议：

```text
idle
  -> session_selected
  -> agent_connected
  -> window_bound
  -> screenshot_captured
  -> screenshot_sent
```

### 窗口枚举与绑定

step2 只支持 Windows 普通窗口。新增 `internal/capture` 使用纯 Win32 syscall，不引入 CGO。

窗口模型：

```go
type WindowRef struct {
    Handle uintptr
    Title string
    ProcessID uint32
    ClientWidthPx int
    ClientHeightPx int
    ScaleFactor float64
}
```

枚举规则：

1. 使用 `EnumWindows` 枚举顶层窗口。
2. 过滤不可见窗口、空标题窗口、最小化窗口。
3. 使用 `DwmGetWindowAttribute(DWMWA_CLOAKED)` 过滤 cloaked 窗口。
4. 使用 `GetClientRect` 获取 client area 尺寸。
5. 过滤 client area 宽高为 0 的窗口。

绑定窗口时保存 `HWND` 和窗口标题；截图前重新校验窗口仍存在且 client area 尺寸有效。

### 截图

截图范围：目标窗口 client area，不含标题栏和边框。

实现策略：

1. 优先使用 `PrintWindow` 捕获窗口内容。
2. 如果 `PrintWindow` 失败或返回无效图片，fallback 到 `BitBlt` 捕获当前屏幕可见区域。
3. 使用 `GetClientRect` 和 `ClientToScreen` 确定 client area 的屏幕位置。
4. 输出 `image.RGBA` 或等价 image 类型。
5. 使用 Go 标准库 `image/png` 编码为 PNG bytes。
6. PNG 解码尺寸必须等于 capture metadata 中的 `width_px` / `height_px`。

风险说明：

1. 某些 GPU/DirectX 游戏窗口可能无法通过 `PrintWindow` 正确截图。
2. fallback `BitBlt` 只能捕获屏幕上当前可见像素，窗口被遮挡时可能不准确。
3. DPI awareness 必须尽早设置，否则高 DPI 多显示器下尺寸可能被缩放。

## Trace 与日志

### Trace

desktop 不上报日志，但请求应带 trace context：

1. HTTP client 使用 `common/gopkg/otel/tracecontext.HTTPTransport` 或等价逻辑注入 W3C `traceparent`。
2. WebSocket dial header 同样注入 `traceparent`。
3. desktop 本地日志记录 trace id，方便用户复制到 SigNoz 查询服务端链路。
4. gateway 已由 `phttp.Handler` 接入 OTel，WebSocket handler 使用 `r.Context()` 创建 proxy gRPC stream，继续保持 trace 传播。

### 日志 UI

desktop 日志改为一条日志一行：

```text
12:34:56 INFO backend screenshot captured {"width_px":1280,"height_px":720,"encoding":"PNG"}
```

要求：

1. 时间、level、source、message 在同一行。
2. fields 以单行 JSON 展示。
3. 长字段可横向滚动或折叠，不把每个字段拆成独立行。
4. 继续支持 Clear Logs 和自动滚动到底部。

## 测试方案

### 单元测试

至少覆盖：

1. `game.proto` 新 `AgentFrame oneof` 的 protojson 编解码。
2. session `CreateSession` 不需要请求 session id，返回服务端生成 id。
3. session `ListSessions` 返回已创建 session，并按稳定顺序分页。
4. desktop HTTP client 使用 protojson 发送/解析 proto 模型。
5. desktop WebSocket client 使用 protojson 发送/解析 `AgentFrame`。
6. desktop trace transport 注入 `traceparent`。
7. desktop capture PNG 编码后解码尺寸与 metadata 一致。
8. agent `Connect` 接收 screenshot frame 后返回 ack。
9. gateway WebSocket 继续覆盖 URL 中的 session id。

### Windows 手动验收

由于窗口枚举和截图依赖 Windows 桌面环境，step2 需要手动验收：

1. 启动 desktop。
2. 创建 session。
3. 创建 agent 并连接 WebSocket。
4. 打开一个普通 Windows 窗口，例如记事本或浏览器。
5. 在 desktop 中刷新窗口列表，选择该窗口并绑定。
6. 点击截图，预览中显示窗口 client area，且不包含标题栏。
7. 发送截图，agent 返回 ack。
8. 日志中显示 PNG、宽高、trace id、ack 信息。

### Bazel 验证

完成实现后执行：

```bash
bazel run //:go -- fmt projects/game
bazel run //:gazelle projects/game
bazel test //projects/game/...
bazel build //projects/game/...
bazel build //projects/game/desktop:desktop
```

如修改 Go module 依赖，再按仓库规范执行：

```bash
bazel run //:go -- mod tidy -v
bazel mod tidy
```

## 系统测试建议

testplan 侧建议覆盖服务端可自动化部分：

1. 创建 session，不传 session id，返回非空 session id。
2. ListSessions 能看到新建 session。
3. 创建 agent。
4. WebSocket 发送 status frame，收到 status 响应。
5. WebSocket 发送 screenshot frame，payload 为小 PNG fixture，收到 ack。
6. 删除 session 后 agent owner 清理。

窗口枚举和真实截图不放入 testplan 强制执行，保留为 Windows desktop 手动验收。

## 实施步骤

1. 更新 `projects/game/game.proto`：session id 服务端生成、ListSessions、AgentFrame oneof、ScreenshotFrame。
2. 更新 session handler/domain/runtime/mongo，支持 id generator 和 ListSessions。
3. 更新 gateway/proxy/agent 以适配新 AgentFrame。
4. 更新服务端单测和 testplan 中的 frame 结构。
5. 更新 desktop `internal/api`，改用 Go proto 类型和 protojson。
6. 新增 desktop `internal/capture`，实现窗口枚举、绑定校验、client area 截图、PNG 编码。
7. 重构 desktop frontend 为 session 列表、session 详情、play 页面和单行日志面板。
8. 补充 traceparent 注入与日志展示。
9. 执行 Bazel 单测、构建和 Windows 手动验收。

## 风险与注意事项

1. `AgentFrame` oneof 是协议破坏性变更，desktop、gateway、proxy、agent、testplan 必须同步修改。
2. PNG 单张截图可能较大，step2 仅保证单张手动发送；不要把该协议直接扩展为高频截图流。
3. 高 DPI 和多显示器环境可能导致截图尺寸和窗口逻辑尺寸不一致，实现时必须以实际像素为准。
4. `PrintWindow` 对部分 GPU/DirectX 窗口可能黑屏；这是 step2 已接受的限制。
5. Wails IPC 与前端状态模型不强制直接使用 Go proto 对象，可在 Wails 边界使用适合 UI 展示的 view model；但 HTTP/WebSocket 等对外通信对象必须由 proto 模型构造，并使用 protojson 编解码。

## 待定项

无阻塞待定项。

以下问题已作为明确约束处理，不再待定：

1. 窗口截图先支持普通窗口。
2. WebSocket 只发送单张图片。
3. 图片固定 PNG。
4. desktop Go 后端直接使用 Go proto 类型通信。
5. 坐标基于不含标题栏的 client area 原始像素。
