# Game Desktop 截图重构方案

本文档作为 `design/game/game-agent-step2.md` 的补充重构方案，重新定义 desktop 截图实现：改用 `github.com/kbinani/screenshot` 捕获整个窗口，坐标基于完整窗口原始像素，desktop 层不做坐标换算，并在截图前校验窗口可捕获状态，不可捕获时直接中止。

## 背景

`design/game/game-agent-step2.md` 原方案要求 desktop 绑定普通 Windows 窗口后截取窗口 client area，截图不包含标题栏和窗口边框，底层实现建议使用 `PrintWindow`，失败后 fallback 到 `BitBlt`。当前实现也基本遵循该路径：`projects/game/desktop/internal/capture` 自行调用 Win32/GDI API 完成截图。

实际使用中出现部分窗口截图不正确的问题：窗口内容只出现在截图左上角，其余区域为黑色。该问题与手写 Win32/GDI 截图路径中的 client area 定位、DPI 虚拟化、窗口渲染方式和 GDI bitmap 拷贝都相关。为降低截图实现复杂度，本方案将截图模块重构为基于 `github.com/kbinani/screenshot` 的屏幕矩形捕获。

同时，截图范围从 client area 改为完整窗口。后续 agent 输出操作坐标时，直接使用基于完整窗口截图的相对像素坐标，desktop 不再把坐标换算为 client area 坐标。

## 目标

完成本方案后应达到以下效果：

1. desktop 截图模块不再自行实现 `PrintWindow`、`BitBlt`、`GetDIBits` 等截图逻辑。
2. desktop 使用 `github.com/kbinani/screenshot` 截取绑定窗口在屏幕上的完整窗口矩形。
3. 截图 PNG 包含完整窗口，包括标题栏和窗口边框。
4. `AgentScreenshotFrame.width_px` / `height_px` 表示实际 PNG 解码后的完整窗口像素尺寸。
5. agent 后续操作坐标以完整窗口截图左上角为原点，desktop 不做 client area 坐标换算。
6. 截图前检查绑定窗口仍存在、可见、未最小化、未 cloaked、窗口矩形有效，并且目标矩形可以被截图；检查失败时返回明确错误并中止发送。
7. 服务端、gateway、proxy、agent 的 WebSocket/gRPC 转发模型保持不变。

## 非目标

本方案不包含以下内容：

1. 不实现连续截图流或高帧率视频流。
2. 不实现鼠标、键盘真实操作命令执行。
3. 不实现 desktop 到 client area 或屏幕绝对坐标的操作换算。
4. 不修改 `AgentFrame` oneof 结构。
5. 不恢复 `client_x_px` / `client_y_px` 等截图偏移字段。
6. 不改变 session、gateway、proxy、agent 的资源模型和路由职责。
7. 不保证被遮挡窗口可以得到无遮挡内容；本方案捕获的是当前屏幕矩形中的可见像素。

## 已确认决策

1. 坐标改为完整窗口坐标。agent 后续直接给出基于完整窗口截图的相对像素坐标。
2. desktop 不做坐标换算，不再维护 client area 原点偏移。
3. 截图前进行可捕获性检查。当前绑定窗口无法被截图时，返回错误并中止本次截图或发送。
4. 采用 `github.com/kbinani/screenshot` 替换自研 Win32/GDI 截图实现。
5. 截图仍固定编码为 PNG。

## 技术选型

| 模块 | 选型 | 说明 |
|---|---|---|
| 截图库 | `github.com/kbinani/screenshot` | 使用 `CaptureRect(image.Rectangle)` 截取桌面坐标系中的窗口矩形。 |
| 窗口枚举 | Win32 `EnumWindows` | 继续复用现有窗口发现、可见性、最小化和 cloaked 过滤逻辑。 |
| 窗口边界 | `DwmGetWindowAttribute(DWMWA_EXTENDED_FRAME_BOUNDS)`，失败时 fallback `GetWindowRect` | 优先获取用户实际可见窗口外框；fallback 保证普通窗口仍可截图。 |
| 图片编码 | Go 标准库 `image/png` | 保持 step2 固定 PNG 的协议约束。 |

## 模型设计

### WindowRef

`WindowRef` 表示可绑定的完整窗口，不再表达 client area 尺寸。

```go
type WindowRef struct {
    Handle      uintptr `json:"handle"`
    Title       string  `json:"title"`
    ProcessID   uint32  `json:"processID"`
    WidthPx     int     `json:"widthPx"`
    HeightPx    int     `json:"heightPx"`
    ScaleFactor float64 `json:"scaleFactor"`
}
```

字段说明：

1. `WidthPx` / `HeightPx` 是完整窗口外框的原始像素尺寸。
2. `ScaleFactor` 保留，用于记录当前窗口所在显示环境的缩放信息；step2 不基于该字段做坐标换算。
3. frontend 中对应字段同步使用 `widthPx` / `heightPx`，不再使用 `clientWidthPx` / `clientHeightPx`。

### WindowBounds

capture 包内部使用完整窗口边界，避免在截图、日志和错误信息中重复传递四个裸整数。

```go
type WindowBounds struct {
    Left   int
    Top    int
    Right  int
    Bottom int
}

func (b WindowBounds) Width() int  { return b.Right - b.Left }
func (b WindowBounds) Height() int { return b.Bottom - b.Top }
```

该类型仅作为 desktop 内部实现细节，不暴露到 `AgentScreenshotFrame`。

### CapturedImage

`CapturedImage` 保持现有字段形态，但注释语义改为完整窗口截图。

```go
type CapturedImage struct {
    Data     []byte `json:"data"`
    WidthPx  int    `json:"widthPx"`
    HeightPx int    `json:"heightPx"`
    Encoding string `json:"encoding"`
}
```

### AgentScreenshotFrame

proto 不新增字段。

```proto
message AgentScreenshotFrame {
  string capture_id = 1;
  ImageEncoding encoding = 2;
  bytes data = 3;
  int32 width_px = 4;
  int32 height_px = 5;
  double scale_factor = 6;
  string window_title = 7;
  google.protobuf.Timestamp capture_time = 8;
}
```

语义调整为：

1. `width_px` / `height_px` 必须等于完整窗口 PNG 解码尺寸。
2. agent 看到的像素坐标以该截图左上角为 `(0,0)`。
3. desktop 不向 agent 发送 client area 偏移，也不对 agent 坐标做 client area 换算。

## 代码分层

目标结构沿用现有 desktop 分层，只替换 `internal/capture` 的截图实现。

```text
projects/game/desktop/
  app.go
  internal/
    capture/
      window.go          # WindowRef、ListWindows、绑定窗口重新校验
      windows.go         # Windows 窗口枚举、DWM/window rect syscall wrapper
      capture.go         # CaptureWindow，调用 kbinani/screenshot.CaptureRect
      png.go             # PNG encode/decode/validate
      stub_nonwindows.go # 非 Windows 平台错误返回
  frontend/src/
    api.ts
    components/PlayView.svelte
```

职责说明：

| 路径 | 职责 |
|---|---|
| `app.go` | 保持 Wails 方法入口；记录截图日志；构造 `AgentScreenshotFrame`。 |
| `internal/capture/window.go` | 枚举可绑定窗口，返回完整窗口尺寸；截图前重新校验窗口状态。 |
| `internal/capture/windows.go` | 封装 Win32/DWM 获取窗口状态和完整窗口边界的调用。 |
| `internal/capture/capture.go` | 将窗口边界转换为 `image.Rectangle`，调用 `screenshot.CaptureRect`，编码 PNG。 |
| `internal/capture/png.go` | 保持图片编码和尺寸校验工具。 |
| `frontend/src/api.ts` | 同步 `WindowRef` 字段名。 |
| `frontend/src/components/PlayView.svelte` | 展示完整窗口尺寸和截图预览。 |

## 关键细节

### 截图前检查

`CaptureWindow(ctx, hwnd)` 在调用 `screenshot.CaptureRect` 前必须完成以下检查：

1. `ctx` 未取消。
2. `hwnd != 0`。
3. 窗口仍可见。
4. 窗口未最小化。
5. 窗口未被 DWM cloaked。
6. 窗口标题可以为空或非空不作为截图阻断条件；列表展示仍可过滤空标题窗口。
7. 完整窗口边界宽高大于 0。
8. `screenshot.CaptureRect` 对该矩形返回成功。
9. PNG 编码后可解码，且解码尺寸等于 capture metadata 中的 `widthPx` / `heightPx`。

任一检查失败时，返回带上下文的错误，例如：

```text
capture window: hwnd 12345 is minimized
capture window: invalid window bounds left=10 top=10 right=10 bottom=20
capture window: capture rect failed: <library error>
```

`SendScreenshot` 遇到上述错误时不发送 WebSocket frame。

### 窗口边界选择

优先使用 DWM extended frame bounds：

```text
DwmGetWindowAttribute(hwnd, DWMWA_EXTENDED_FRAME_BOUNDS)
```

原因是它更接近用户看到的实际窗口外框。若 DWM 调用失败或返回无效矩形，fallback 到：

```text
GetWindowRect(hwnd)
```

两者都失败或得到无效矩形时，中止截图。

### 截图实现

核心流程：

```go
bounds, err := captureWindowBounds(hwnd)
if err != nil {
    return nil, err
}

img, err := screenshot.CaptureRect(image.Rect(
    bounds.Left,
    bounds.Top,
    bounds.Right,
    bounds.Bottom,
))
if err != nil {
    return nil, fmt.Errorf("capture rect: %w", err)
}

pngBytes, err := EncodePNG(img)
```

不再保留 `PrintWindow` fallback。若 `screenshot.CaptureRect` 失败，本次截图失败。

### 坐标语义

完整窗口截图的坐标系如下：

```text
(0,0) ----------------> x
  |
  |  完整窗口截图，包括标题栏、边框和内容区
  v
  y
```

agent 后续输出的点击坐标直接以该截图为参考。例如 agent 返回 `(100, 50)`，含义是完整窗口截图中距离左上角 100px、50px 的位置。step2 不实现将该坐标转换为 client area 坐标或屏幕绝对坐标。

### 遮挡行为

`github.com/kbinani/screenshot` 捕获的是桌面指定矩形中的当前可见像素。若目标窗口被其他窗口遮挡，截图可能包含遮挡窗口内容。该行为不是错误；只有库无法捕获目标矩形或窗口状态无效时才中止。

### DPI 与多显示器

截图矩形使用桌面坐标系，主显示器左上角为原点，多显示器可能出现负坐标。实现必须避免假设 `Left` / `Top` 非负。

desktop 已有 Windows DPI 初始化逻辑时应继续保留。若后续发现高 DPI 下窗口 bounds 与截图尺寸不一致，再单独补充 per-monitor DPI awareness 调整；本方案不引入坐标换算层。

### 日志

截图日志应包含：

1. `hwnd`
2. `window_title`
3. `bounds`
4. `width_px` / `height_px`
5. PNG bytes 大小
6. 错误原因

示例：

```text
12:34:56 INFO backend screenshot captured {"hwnd":12345,"title":"Game","bounds":{"left":100,"top":80,"right":1380,"bottom":800},"width_px":1280,"height_px":720,"encoding":"PNG","size":456789}
```

## 决策详情

### 为什么改用 `github.com/kbinani/screenshot`

当前自研 Win32/GDI 截图实现需要同时处理 DC 生命周期、bitmap 像素格式、client area 到屏幕坐标转换、DPI 缩放和不同窗口渲染方式。该复杂度已经导致截图结果不稳定。

`github.com/kbinani/screenshot` 将桌面矩形截图封装为稳定的 Go API，desktop 只需要负责获取窗口矩形并编码 PNG。这样可以删除大量容易出错的 GDI 代码，并降低后续维护成本。

### 为什么截图改为完整窗口

用户已确认后续坐标基于整个窗口。完整窗口截图可以让 agent 看到与用户桌面一致的窗口外观，包括标题栏和边框，避免 desktop 隐式改变坐标原点。

该决策会改变 `design/game/game-agent-step2.md` 中 client area 的原始语义。本方案作为补充设计，以本方案为准。

### 为什么 desktop 不做坐标换算

step2 不实现真实鼠标键盘操作。若此阶段加入 client area 或屏幕绝对坐标换算，会引入尚未使用的复杂逻辑，并且需要额外验证 DPI、多显示器、窗口边框和标题栏尺寸。

本方案将坐标语义收敛为：agent 与 desktop 都使用完整窗口截图坐标。后续真正执行操作时，再基于当时的操作模型设计屏幕绝对坐标转换。

### 为什么截图失败时中止而不是 fallback

原方案的 fallback 是为 `PrintWindow` 失败时补救，但 fallback 路径仍依赖自研 GDI 截图，并可能产出错误图片。新方案要求截图前检查可捕获性，失败时返回明确错误，避免向 agent 发送错误截图并污染后续策略判断。

### 为什么 proto 不变

`AgentScreenshotFrame` 当前字段已经能表达 PNG bytes、实际像素尺寸、窗口标题和截图时间。完整窗口坐标不需要额外字段，因为坐标原点固定为截图左上角。

继续不恢复 `client_x_px` / `client_y_px`，避免重新引入 client area 语义。

## 测试方案

### 单元测试

至少覆盖：

1. `WindowRef` 字段从 `ClientWidthPx` / `ClientHeightPx` 更新为 `WidthPx` / `HeightPx`。
2. `WindowBounds.Width()` / `Height()` 返回正确尺寸。
3. 无效 bounds 返回错误。
4. `CapturedImage` PNG 编码后解码尺寸与 metadata 一致。
5. non-Windows stub 继续返回 not supported 错误。

Windows 真实截图依赖桌面环境，不作为普通单元测试强制要求。

### Bazel 验证

实现后执行：

```bash
bazel run //:go -- fmt projects/game/desktop/internal/capture projects/game/desktop
bazel run //:go -- mod tidy -v
bazel run //:gazelle projects/game/desktop/internal/capture
bazel mod tidy
bazel test //projects/game/desktop/...
bazel build //projects/game/desktop:desktop
```

如 Gazelle 更新根级 Go 依赖 repo，还需确认 `MODULE.bazel` 的 `use_repo(go_deps, ...)` 同步包含新依赖。

### Windows 手动验收

1. 启动 desktop。
2. 创建 session，创建 agent，并连接 WebSocket。
3. 打开一个普通 Windows 窗口，例如记事本、浏览器或目标游戏窗口。
4. 在 desktop 中刷新窗口列表，选择该窗口并绑定。
5. 点击截图。
6. 预览显示完整窗口，包含标题栏和窗口边框。
7. PNG 宽高与完整窗口外框尺寸一致。
8. 发送截图后 agent 返回 ack。
9. 最小化窗口后再次截图，应返回明确错误且不发送 frame。
10. 关闭绑定窗口后再次截图，应返回明确错误且不发送 frame。

## 与原 step2 方案的差异

| 原 step2 方案 | 本方案 |
|---|---|
| 截取 client area，不含标题栏和边框 | 截取完整窗口，包含标题栏和边框 |
| 坐标基于 client area 原始像素 | 坐标基于完整窗口截图原始像素 |
| 使用 `PrintWindow`，失败 fallback `BitBlt` | 使用 `github.com/kbinani/screenshot.CaptureRect`，失败直接返回错误 |
| desktop 后续负责 client area / screen 坐标换算 | step2 desktop 不做坐标换算 |
| `WindowRef` 暴露 `ClientWidthPx` / `ClientHeightPx` | `WindowRef` 暴露 `WidthPx` / `HeightPx` |

## 风险与注意事项

1. 目标窗口被遮挡时，截图可能包含遮挡内容。
2. 高 DPI、多显示器和负坐标环境仍需 Windows 手动验收。
3. `github.com/kbinani/screenshot` 是新增 Go 依赖，需要同步 Go module、Gazelle 和 Bazel module lock。
4. 由于截图包含标题栏和边框，agent 视觉输入与原 client area 方案不同，后续策略提示词或操作模型需要明确坐标基准。
5. 如果某些游戏窗口仍无法通过屏幕矩形方式捕获，后续再评估 Windows Graphics Capture 或 DXGI Output Duplication。

## 未来规划

后续实现真实操作命令时，再新增单独方案设计：

1. agent 输出完整窗口截图坐标。
2. desktop 将完整窗口截图坐标转换为屏幕绝对坐标。
3. desktop 执行鼠标、键盘操作。
4. 针对 DPI、多显示器、窗口移动、窗口缩放补充验收。

## 待定项

无阻塞待定项。
