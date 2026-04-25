# LLM Agent 玩扫雷 milestone 1 windows agent 详细技术方案

## 目标

本方案用于单独描述 milestone 1 中 `windows agent` 的职责、边界、技术选型、构建发布方式和与 `game gateway` 的协议对接方式，目标是：

* 让 `windows agent` 成为部署在目标 Windows 主机上的本地执行端。
* 让其稳定承接单窗口画面采集、H.264 fMP4 视频编码上传和鼠标输入执行。
* 让其通过当前 `game gateway` WebSocket 协议接入系统，而不直接对 `web` 暴露。
* 让 Windows 应用制品可以在 Linux CI 上稳定、可复现地构建，并发布到指定 S3 目录。

本方案希望达成的效果是：开发者在 Linux CI 中可以通过 Bazel 可重复地产出 `windows_agent` Windows 便携制品；Windows 主机运行该 agent 后，可以按 `session service` 返回的 `agent_connect_url` 接入指定 gateway，绑定一个目标窗口，上传低延迟 H.264 fMP4 视频流，并执行来自 web 的完整鼠标调试动作。

## 范围

本方案覆盖：

* Wails + Go + Web 前端技术栈选型。
* 单窗口绑定模型。
* 基于随包 `ffmpeg.exe` 的窗口捕获、H.264 fMP4 编码和分片上传。
* 独立 `input-helper.exe` 鼠标输入执行。
* 与 `game gateway` 的 WebSocket protojson 协议对接。
* 使用 Bazel 自定义 rule 构建 Windows 应用制品。
* 将 Wails CLI、ffmpeg 和 helper 都作为 Bazel 依赖/工具管理，避免依赖本地机器环境。
* 制品发布到 S3 指定目录。

本方案不包括：

* `session service` 生命周期管理。
* `game gateway` 的媒体缓存与对外 REST 接口。
* Web 操作页面实现，本次保持 pending。
* 键盘输入实现。
* Windows Graphics Capture / Media Foundation / NVENC 的原生深度集成。
* 不依赖 ffmpeg 的自研编码与 fMP4 mux 链路。
* 自动更新机制的完整实现。
* 完整替代 web 端的远程观察与控制体验。

## 现有协议与实现约束

本方案必须对齐当前代码中已经落地的 `session` 与 `gateway` 实现：

* `session service` 创建或重连 session 后返回完整 `agent_connect_url`。
* agent 使用该 URL 连接：`wss://{publicHost}/v1/sessions/{sessionID}/game/connect?token=...`。
* `game gateway` WebSocket 入口由 `projects/game/gateway/ws.go` 承接。
* WebSocket 业务消息使用 `projects/game/gateway/gateway.proto` 中的 `GameWebSocketEnvelope`，当前线上格式为 protojson 文本消息。
* 连接成功后第一条业务消息必须是 `hello`，且 `role = GAME_CLIENT_ROLE_WINDOWS_AGENT`。
* 媒体上传使用 `media_init` 与 `media_segment`：
  * `media_init` 携带 fMP4 initialization segment。
  * `media_segment` 携带 fMP4 media segment。
  * `segment` 字段是 protobuf `bytes`，在 protojson 文本帧中按 base64 表达。
  * 单个 init/segment 的原始二进制大小不得超过 `domain.MaxSegmentSize = 1MiB`。
  * `key_frame = true` 必须标记可供 web 追帧的关键帧边界。
* 控制请求由 web 经 gateway 转发到 agent，agent 需要按 `control_request -> control_ack -> control_result` 顺序处理。
* gateway 当前从视频缓存中按需生成 JPEG snapshot；agent 不需要常态上传 JPEG snapshot。

当前实现里还存在一个需要先处理的协议差异：`gateway.proto` 的 `GameMouseAction` 包含 `from_x/from_y/to_x/to_y`，但 gateway domain 层 `ControlRequestPayload` 当前只保留了 `x/y/duration_ms`。由于 gateway 会先把 web 的 `control_request` 转成 domain payload 再转发给 agent，drag 起止坐标会在转发前丢失。完整 `mouse_drag` 需要先补齐 gateway domain/control 映射；如果不修复该缺口，则 milestone 1 的 drag 只能按当前 `x/y/duration_ms` 能表达的降级语义执行。

## 总体技术选型

milestone 1 采用：

* 主应用：`Wails + Go + Web 前端`。
* 本机 UI：Wails WebView2 窗口，前端用于连接、窗口选择、预览、状态和日志展示。
* 后端运行时：Go backend 负责所有系统能力、gateway 协议、窗口枚举、捕获进程、媒体分片和输入 helper 管理。
* WebSocket：Go WebSocket 客户端。
* Protobuf / protojson：Go protobuf 类型维护 `GameWebSocketEnvelope`，以 protojson 文本帧收发。
* 视频捕获与编码：Bazel 管理的固定版本 Windows `ffmpeg.exe`，运行时由 Go backend 通过 `os/exec` 启动。
* 输入执行：独立 `input-helper.exe`，由 Bazel 使用 rules_go 交叉编译，并随 agent 制品分发。
* 构建封装：在 `tools/wails/defs.bzl` 提供 Wails Bazel rule，不使用 `genrule` 作为正式封装。
* 依赖管理：Wails CLI、ffmpeg、input helper 均是 Bazel 声明式依赖或工具，固定版本和 SHA256，构建不依赖本地 PATH 中的临时安装。

UI 形态采用 **agent 本机操作台**：Wails 前端提供最小可用的连接、绑定、预览、状态与日志界面。milestone 1 不实现 web 操作页面，也不把 web 浏览器完整合并进 agent；gateway 的 web 连接能力保留为后续入口。后续如果确认需要本机完整操作台，可让 Wails 前端复用 web 的播放/控制组件。

### 选型原则

核心约束是 **Linux 稳定构建 Windows 应用制品** 和 **端到端视频链路优先跑通**。因此 milestone 1 的选型原则为：

* 不把主链路建立在 Linux CI 必须编译大型 Windows C/C++ 编码库之上。
* 不要求目标 Windows 主机预装 ffmpeg，也不从系统 PATH 回退到未知版本 ffmpeg。
* 不要求开发者或 CI 预装 Wails CLI；Wails CLI 由 Bazel 作为工具依赖获取。
* 不要求运行时从网络下载 ffmpeg 或 helper。
* 优先使用 Go 标准库、Windows 系统 DLL 和可由 Bazel 明确声明的预构建二进制。
* 保持 gateway 协议不变，避免 milestone 1 同时修改 agent 和 gateway 的媒体传输协议。

可接受的运行时依赖包括：

* Windows WebView2 Runtime。M1 使用 Wails 的 WebView2 bootstrapper 策略，离线固定 runtime 作为后续发布策略评估。
* Bazel 下载并校验的 Windows `ffmpeg.exe`。
* Bazel 交叉编译的 `input-helper.exe`。

不作为 milestone 1 主路径的能力包括：

* 自研 H.264 编码器或 Media Foundation in-process 编码。
* Windows Graphics Capture / Direct3D / DXGI 的原生深度集成。
* GStreamer 或其他大型多媒体 runtime。
* 二进制 WebSocket 媒体帧协议。
* 运行时下载 ffmpeg、Wails CLI 或其他构建工具。

## 组件定位

`windows agent` 负责：

* 提供本地 UI，用于输入或展示 session connect URL、连接状态、窗口绑定状态、媒体状态和错误日志。
* 提供本机预览和本地诊断能力，帮助操作者确认窗口绑定、采集和 gateway 连接是否正常。
* 枚举 Windows 可绑定窗口，并绑定一个目标窗口。
* 使用随包 `ffmpeg.exe` 捕获该窗口画面并编码为 H.264 fragmented MP4。
* 将 fMP4 init/segment 通过 WebSocket protojson 上传到 `game gateway`。
* 执行来自 `game gateway` 的完整鼠标动作。
* 回传 `control_ack`、`control_result`、`error`、`pong`。
* 在断线或退出时重置输入状态并停止捕获/编码/helper 子进程。

`windows agent` 不负责：

* 直接服务浏览器。
* 决定连接哪个 gateway。
* 生成或解析 session token。
* 保存长期 session 状态。
* 向 gateway 常态推送 JPEG snapshot。
* 持久化媒体数据。
* 在 milestone 1 中实现 web 操作页面或完整取代 web 端远程 UI。

## 应用模型

### Wails 应用划分

Wails 应用由 Go backend 与 Web 前端组成。

Go backend 负责所有系统能力和外部协议：

* Wails 生命周期、窗口创建、单实例锁和退出处理。
* 读取本地配置。
* 管理 WebSocket 连接。
* 管理窗口枚举、窗口绑定和窗口状态探测。
* 启动、监控和停止 `ffmpeg.exe`。
* 将 ffmpeg stdout 输出切分为 `media_init` / `media_segment`。
* 管理 `input-helper.exe` 进程并执行鼠标输入。
* 通过 Wails bindings/events 向前端上报状态和日志。

Web 前端只负责 UI：

* 输入或粘贴 `agent_connect_url`。
* 展示 session、gateway、连接和媒体状态。
* 展示可绑定窗口列表。
* 展示本机采集预览，用于确认绑定窗口正确。
* 展示最近错误与诊断日志。
* 提供启动、停止、重连、重新绑定窗口等本地操作入口。

agent UI 与 web UI 的边界：

* agent UI 面向部署在 Windows 主机旁的操作者，解决“连接哪个 session、绑定哪个窗口、采集是否正常、ffmpeg 是否正常、输入 helper 是否正常”等本机问题。
* web 操作页面本次保持 pending；gateway 协议仍保留 web 观察者/控制者模型，但不交付浏览器页面。
* milestone 1 的 agent UI 提供本机预览和本地日志，用于部署、绑定与诊断，不在预览画面上实现完整点击/拖动操作页面。

### 建议代码分层

建议按如下层次实现：

* `cmd/windows_agent`：Wails 应用入口、frontend embed、窗口配置和生命周期。
* `app`：Wails bindings/events、状态订阅、UI 调用入口。
* `runtime`：agent 本地状态机、session 信息、重连与清理。
* `transport`：gateway WebSocket 连接、protojson envelope 编解码、ping/pong、控制消息分发。
* `window`：Win32 窗口枚举、窗口信息、绑定状态、坐标转换辅助。
* `capture`：ffmpeg gdigrab 参数生成、窗口捕获策略、fallback 策略。
* `encoder`：`ffmpeg.exe` 路径解析、版本校验、进程管理、stdout/stderr 处理。
* `media`：MP4 box 解析、`media_init` / `media_segment` 组包、大小限制、关键帧标记。
* `input`：`input-helper.exe` 管理、stdin/stdout IPC、超时和断线清理。
* `release`：构建版本、manifest、S3 发布脚本。
* `tools/wails`：Wails Bazel rule、staging/package helper、固定工具依赖入口。

## 窗口绑定模型

milestone 1 收敛为：

* 一次 session 只绑定 **一个窗口**。
* 所有输入坐标均使用 **窗口相对坐标**。
* UI 中由用户从窗口列表中选择目标窗口。
* agent 内部保存窗口句柄、标题、进程 ID、class name 和最近一次窗口矩形。

窗口枚举由 Go backend 调用 Win32 API 完成：

* `EnumWindows` 枚举顶层窗口。
* `IsWindowVisible` 过滤不可见窗口。
* `GetWindowText` 获取窗口标题。
* `GetClassName` 获取窗口 class name。
* `GetWindowThreadProcessId` 获取进程 ID。
* `GetWindowRect` 获取屏幕坐标矩形。

窗口状态处理：

* 正常可见窗口：持续采集并上传视频。
* 失焦或被遮挡：优先继续采集；如果当前捕获方式只能得到遮挡后的画面，则如实上传，不伪造帧。
* 最小化或窗口消失：暂停视频上传，保持 WebSocket 控制链路可用，并上报错误或状态。
* 窗口标题变化：优先使用绑定时保存的 HWND；标题仅作为 fallback 和 UI 展示。
* 窗口重新出现：允许用户重新绑定，或在同一 HWND 仍有效时自动恢复采集。

## 视频采集与编码

### milestone 1 主链路

视频主链路采用 Wails Go backend 调度随包 ffmpeg 的单窗口捕获和 fMP4 输出：

```text
绑定窗口
  -> Go backend 保存 HWND / title / rect
  -> 启动 Bazel 打包的 ffmpeg.exe
  -> ffmpeg gdigrab 捕获目标窗口
  -> ffmpeg 编码为 H.264 fragmented MP4
  -> Go backend 读取 fMP4 stdout
  -> MP4 box parser 拆分 media_init / media_segment
  -> Go WebSocket protojson 上传到 game gateway
```

首版优先验证端到端可用性与 gateway 协议兼容性。若 `gdigrab hwnd` 在目标环境不稳定，再按 fallback 策略退到标题捕获或桌面捕获加 crop；Windows Graphics Capture 不作为 M1 主路径。

### ffmpeg 作为 Bazel 依赖

`ffmpeg.exe` 不保存到仓库中。它必须由 Bazel 作为外部依赖或工具获取，并满足：

* 固定下载 URL。
* 固定 SHA256。
* 固定版本 metadata。
* 构建时由 Bazel 拉取并作为 declared input 传入 Wails package rule。
* 打包时复制到 `resources/bin/ffmpeg.exe`。
* 运行时只使用随包路径，不回退到系统 PATH。

这样做的原因：

* 仓库不保存大型第三方二进制。
* Linux CI 不需要编译 Windows 原生编码库。
* Bazel remote cache 可以以固定 digest 缓存 action 输入和输出。
* 发布 manifest 可以完整记录 ffmpeg 来源、版本和 SHA256。
* agent 不依赖用户机器预装 ffmpeg，也不依赖 PATH 中的未知版本。

运行时路径应从 agent 可执行文件所在目录解析，例如：

```text
<agent-dir>/resources/bin/ffmpeg.exe
```

如果随包 ffmpeg 不存在、版本不匹配或 SHA256 校验失败，agent 应停止媒体链路并报告 `encoder_unavailable`，而不是回退到系统 PATH。

### 窗口捕获策略

主捕获方式：

```text
ffmpeg.exe -f gdigrab -i hwnd=<HWND>
```

要求：

* 绑定窗口时保存 HWND，并在启动 ffmpeg 前确认 HWND 仍有效。
* 文档和构建 metadata 记录 ffmpeg 最小版本要求，确保 `gdigrab hwnd` 能力可用。
* 启动前读取窗口矩形，并按最大分辨率策略限制输出尺寸。
* 如果 HWND 捕获失败，停止媒体链路并报告明确错误；M1 可以提供手动 fallback 入口。

fallback 顺序：

1. `gdigrab` 按唯一窗口标题捕获。
2. `gdigrab desktop` 加窗口矩形 crop。
3. 后续版本评估 Windows Graphics Capture。

fallback 不应悄悄改变坐标语义。只要进入 fallback，agent UI 必须展示当前捕获模式和风险，例如遮挡、DPI、窗口移动导致的裁剪偏移。

### ffmpeg 输出格式

gateway 当前要求 agent 上传 H.264 fMP4，因此 ffmpeg 输出必须满足：

* codec：H.264。
* container：fragmented MP4。
* 输出：stdout pipe，供 Go backend 实时读取。
* GOP：按关键帧边界切分。
* segment：单个 WebSocket payload 的原始二进制大小不超过 1MiB。
* 低延迟：避免 B-frames，优先低延迟 preset/tune。

参数方向如下，实际实现可根据目标窗口大小调整：

```bash
ffmpeg.exe \
  -hide_banner \
  -loglevel info \
  -f gdigrab \
  -framerate 30 \
  -i hwnd=<HWND> \
  -an \
  -c:v libx264 \
  -preset ultrafast \
  -tune zerolatency \
  -pix_fmt yuv420p \
  -g 30 \
  -keyint_min 30 \
  -sc_threshold 0 \
  -b:v <bitrate> \
  -maxrate <bitrate> \
  -bufsize <buffer-size> \
  -movflags frag_keyframe+empty_moov+default_base_moof \
  -frag_duration 500000 \
  -f mp4 pipe:1
```

默认参数建议：

* 帧率：30fps。
* GOP：30 帧，即约 1 秒一个关键帧。
* fragment duration：约 500ms。
* 最大输出尺寸：优先保持窗口原始尺寸；超过 1280x720 时等比缩放到 1280x720。
* 码率：按输出尺寸配置，默认 720p 使用 1Mbps 左右起步，确保 fragment 明显低于 1MiB。

如果 fragment 超过 1MiB，agent 不应强行发送超限消息，而应：

1. 停止当前 ffmpeg 进程。
2. 下调码率、帧率或分辨率。
3. 重新启动编码器并重新发送 `media_init`。
4. 在 UI 和日志中报告参数调整。

### fMP4 切分与上传

agent 需要把 ffmpeg stdout 中的 MP4 box 流解析为：

* `ftyp + moov`：作为 `media_init` 发送。
* 每组 `moof + mdat`：作为一个 `media_segment` 发送。

要求：

* `media_init.mime_type` 使用 `video/mp4; codecs="avc1..."` 格式。
* 每个 segment 生成递增且唯一的 `segment_id`。
* 每个从关键帧开始的 fragment 设置 `key_frame = true`。
* 每个 init/segment 在发送前检查原始二进制大小不超过 1MiB。
* ffmpeg 退出或 stderr 出现不可恢复错误时，停止媒体上传并保留 WebSocket 控制链路。
* 编码器重启后必须重新发送新的 `media_init`，再继续发送新的 `media_segment`。
* WebSocket 仍使用 protojson 文本帧；bytes 字段由 protojson 自动 base64。

### 截图策略

milestone 1 中 JPEG snapshot 的对外读取由 `game gateway` 从视频缓存按需生成：

* `GET /v1/sessions/{session}/game/snapshot` 由 gateway 处理。
* `control_request.flash_snapshot = true` 时，gateway 在收到 `control_result` 后刷新 snapshot。
* agent 不需要常态上传独立 JPEG snapshot。

agent 本地 UI 可以为了调试展示预览画面，但该预览不作为 gateway 协议的一部分。

## 输入执行

### 鼠标动作集合

agent 需要执行的完整鼠标动作：

* `mouse_click`
* `mouse_double_click`
* `mouse_drag`
* `mouse_hover`
* `mouse_hold(duration_ms)`

### 输入注入路径

milestone 1 的输入实现继续采用 **独立 `input-helper.exe` 主路径**：

* Wails Go backend 通过 stdin/stdout、本地 pipe 或等价 IPC 向 helper 下发完整鼠标动作。
* `input-helper.exe` 负责窗口相对坐标转换、SendInput/PostMessage 调用、执行结果返回和状态清理。
* helper 作为独立 Windows 可执行文件随 Wails 应用一起分发，放入 `resources/bin/`。
* helper 使用纯 Go 或 Windows syscall 实现，由 Bazel/rules_go 交叉编译为 Windows amd64。

即使 Wails backend 和 helper 都是 Go，仍保持 helper 独立进程，原因是：

* 输入注入是有状态能力，`mouse_hold` 与 `mouse_drag` 可能保持鼠标按下态；独立进程可以在退出、信号、超时时集中 `ReleaseAll()`。
* 输入执行失败不应拖垮 WebSocket、媒体上传和 UI 主进程；agent 可以 kill 并重启 helper。
* 长动作可以用进程级 kill 作为最后兜底，取消语义比单纯 goroutine 更强。
* helper 可独立测试、独立替换，后续切换 SendInput/PostMessage/UIAutomation 不影响主 agent 架构。
* 如果后续输入需要单独权限、签名、DPI awareness 或兼容 manifest，独立 exe 更灵活。

输入执行要求：

* 将窗口相对坐标转换为屏幕坐标或目标窗口消息坐标。
* 严格检查输入执行结果。
* `mouse_hold` 必须带时长，且最大 `30s`。
* `mouse_drag` 按请求给定轨迹和时长一次性执行，不拆成多次网络往返。
* 断线、退出、执行失败时必须释放可能按住的鼠标按钮。

### 超时规则

agent 应按 gateway 当前约束响应：

* 收到 `control_request` 后尽快发送 `control_ack`。
* `mouse_click` / `mouse_double_click` / `mouse_hover`：应在 `1s` 内完成。
* `mouse_drag`：应在 `30s` 内完成。
* `mouse_hold`：按 `duration_ms` 执行，最大 `30s`。
* agent 无响应：由 gateway 超时并向 web 返回失败或 timed out。

### 断线与状态重置

若连接断开：

* agent 应立即停止读取新控制请求。
* agent 应释放所有本地记录的按住态。
* agent 应停止或重启媒体采集/编码链路，避免孤儿 ffmpeg 进程。
* agent 应停止或重启 input helper，避免遗留鼠标按住态。
* 新连接建立后只恢复媒体与控制服务能力，不恢复旧操作。

## 与 gateway 的交互

### 连接

agent 使用 `session service` 返回的完整 `agent_connect_url`，不自行拼接 gateway 地址，不解析 token 语义。

连接流程：

1. 用户或本地配置提供 `agent_connect_url`。
2. Go backend 建立 WebSocket。
3. WebSocket upgrade 成功后，在 `10s` 内发送 `hello`。
4. `hello.role = GAME_CLIENT_ROLE_WINDOWS_AGENT`。
5. `hello` 之后启动媒体上传与控制消息读取。

### 上行消息

agent 主要上行：

* `hello`
* `media_init`
* `media_segment`
* `control_ack`
* `control_result`
* `error`
* `pong`

所有上行消息均为 `GameWebSocketEnvelope` protojson 文本帧；每条消息必须设置 `session_id` 和唯一 `message_id`。JSON 中 bytes 字段按 protojson 规则使用 base64 表达，enum 使用 protojson 可接受的枚举表达方式。agent 不发送自定义 `{type, payload}` envelope，也不发送二进制 WebSocket 媒体帧。

### 下行消息

agent 需要处理：

* `control_request`
* `ping`
* `error`

其他面向 web 的媒体消息如果被错误路由到 agent，agent 应忽略并记录调试日志，不应崩溃。

## Bazel 构建 Windows 应用

### 构建目标

Linux CI 需要产出至少一种可运行制品：

* Windows x64 portable zip：用于测试、手工部署和快速分发。

可选产出：

* NSIS installer：用于正式安装。
* release metadata：用于后续自动更新或 latest 指针。

### 构建原则

本仓库标准编译工具是 Bazel。Wails 应用的构建、打包和发布入口都应由 Bazel target 封装，避免每个开发者或 CI 直接拼接临时脚本。

关键原则：

* 不使用 `genrule` 作为 Wails 正式封装。
* 在 `tools/wails/defs.bzl` 定义 Starlark rule，例如 `wails_windows_package`。
* Wails CLI 作为 Bazel 工具依赖，固定版本和 checksum，不依赖本地机器安装。
* ffmpeg 作为 Bazel 外部依赖，固定版本和 SHA256，不保存到仓库。
* input helper 由 Bazel/rules_go 构建，作为 package rule 的 declared input。
* 产物输出路径、文件名、zip 内容和 metadata 应稳定，便于 remote cache。
* 构建 action 不应读取本地 PATH 中的 Wails、ffmpeg、zip、node 或临时脚本。

### Wails Bazel rule

建议新增：

```text
tools/wails/
  ├── BUILD.bazel
  ├── defs.bzl
  └── package_helper.go 或 package_helper.sh
```

`tools/wails/BUILD.bazel` 导出 `defs.bzl`，并可定义用于 staging/package 的 helper 工具。`defs.bzl` 提供类似：

```starlark
wails_windows_package(
    name = "windows_agent_win_zip",
    project = ":wails_project",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    wails_cli = "@wails_cli_windows_agent//:wails",
    ffmpeg = "@ffmpeg_windows_amd64//file",
    input_helper = "//projects/game/windows_agent_helper/input:input_helper",
    resources = [
        "resources/icon.ico",
        "resources/bin/ffmpeg-metadata.json",
    ],
    out = "windows-agent-win.zip",
)
```

rule 职责：

1. 声明 Wails CLI、ffmpeg、input helper、前端资源、Go 源码、图标和 metadata 为 inputs。
2. 使用 `ctx.actions.run` 调用固定版本 Wails CLI 或受控 package helper。
3. 将 Wails 构建输出写入 declared directory。
4. 将 `ffmpeg.exe`、`input-helper.exe`、metadata 复制到稳定的 `resources/bin/` 布局。
5. 使用 Bazel 管理的 zipper 或 `rules_pkg` 生成 portable zip。
6. 输出 `DefaultInfo(files = depset([zip]))`，供发布 target 依赖。

rule 不应：

* 从本地 PATH 寻找 Wails CLI。
* 从本地 PATH 寻找 ffmpeg。
* 在 action 中访问网络下载依赖。
* 写入源码目录。
* 生成非声明输出。

### Wails CLI 依赖管理

Wails CLI 与 ffmpeg 一样属于构建输入，必须由 Bazel 管理：

* 在 Bazel module / repository rule 中固定 Wails CLI 版本。
* 通过预构建二进制或 Go tool 构建方式提供执行平台可运行的 `wails`。
* 若使用 Go tool 构建 Wails CLI，应由 Bazel action 构建，不依赖开发者 `$GOPATH/bin/wails`。
* 版本号写入 release manifest。
* CI 镜像只提供基础 OS 依赖，不提供隐式 Wails 版本。

这样做可以保证：

* 构建输入 digest 稳定。
* 产物可复现。
* remote cache 命中不受开发者机器影响。
* 后续升级 Wails CLI 是显式代码变更，而不是环境漂移。

### portable zip 内容

建议 portable zip 形态：

```text
windows-agent-0.1.0-windows-amd64.zip
  ├── windows-agent.exe
  ├── resources/
  │   ├── icon.ico
  │   └── bin/
  │       ├── ffmpeg.exe
  │       ├── input-helper.exe
  │       ├── ffmpeg.exe.sha256
  │       └── ffmpeg-metadata.json
  ├── manifest.json
  └── SHA256SUMS
```

`manifest.json` 和 `SHA256SUMS` 可由 release 工具在发布前生成，也可由 package rule 生成基础 metadata 后由 release 工具补齐 S3 相关信息。无论哪种方式，最终发布目录必须包含二者。

### WebView2 策略

M1 默认采用 Wails WebView2 bootstrapper 策略：

* 适合在线 Windows 环境。
* 制品尺寸小。
* Windows 11 通常已有 WebView2 Runtime。

如果目标 Windows 主机可能离线或不能安装 runtime，需要后续评估 fixed WebView2 runtime 随包策略。fixed runtime 会显著增加制品大小，不作为 M1 默认路径。

### native 依赖约束

构建必须满足：

* 不要求开发者本地安装 Wails CLI。
* 不要求开发者本地安装 ffmpeg。
* 不要求运行时下载 ffmpeg。
* 不要求 Linux CI 编译 ffmpeg。
* 不要求 Linux CI 编译大型 Windows C/C++ 多媒体库。
* input helper 使用 rules_go 交叉编译或等价 Bazel Go target 构建。

## S3 制品发布

### 发布目标

构建完成后，制品发布到指定 S3 目录。目录由 CI 参数显式指定，例如：

```text
s3://<bucket>/<prefix>/windows-agent/<version>/
```

其中 `<prefix>` 不在代码中硬编码，由 CI 环境变量提供。

### 发布内容

每次发布至少包含：

* Windows agent portable zip。
* 可选 NSIS installer。
* `manifest.json`。
* `SHA256SUMS`。

建议目录形态：

```text
s3://<bucket>/<prefix>/windows-agent/0.1.0/
  ├── windows-agent-0.1.0-windows-amd64.zip
  ├── windows-agent-0.1.0-windows-amd64-installer.exe
  ├── manifest.json
  └── SHA256SUMS
```

`manifest.json` 至少记录：

* agent 版本。
* git commit。
* 构建时间。
* 平台与架构。
* Wails 版本。
* Wails CLI 来源与 SHA256。
* WebView2 策略。
* ffmpeg 版本。
* ffmpeg 来源。
* ffmpeg SHA256。
* input helper 版本或构建 commit。
* 每个发布制品的文件名、大小和 SHA256。

### S3 上传方式

milestone 1 建议使用 Bazel 可执行发布入口，而不是使用桌面框架自带发布 provider。原因是本仓库已有 Bazel 作为统一构建入口，且发布前需要生成 manifest、checksum，并收集 Wails app、`ffmpeg.exe` 与 `input-helper.exe` 的版本元信息。

发布入口可以先实现 `bazel run` 的发布工具目标，例如：

```bash
bazel run //projects/game/windows_agent/release:publish_s3 -- \
  --version 0.1.0 \
  --s3-url s3://<bucket>/<prefix>/windows-agent/0.1.0/ \
  --dist-dir <bazel-output-dist-dir>
```

`publish_s3` 目标负责：

1. 校验 Wails Windows 产物已生成。
2. 收集 portable zip、可选 installer、`ffmpeg.exe` 版本信息、Wails 版本信息和 helper 版本信息。
3. 生成 `manifest.json`。
4. 生成 `SHA256SUMS`。
5. 上传到指定 S3 URL。

上传使用的凭证遵循仓库已有 S3 约定：

* `S3_ACCESS_KEY`
* `S3_SECRET_KEY`

发布工具应优先复用仓库 `pkg/s3` 的 endpoint 与凭证约定；如果内部调用 AWS CLI，则由工具或 CI 将上述凭证映射到 AWS CLI 需要的环境变量。

## 建议仓库结构

由于主 agent 将从零搭建，建议删除旧 `projects/game/windows_agent/` 内容后重建为 Wails + Go 结构。helper 可在现有 `projects/game/windows_agent_helper/input/` 基础上演进。

建议结构：

```text
projects/game/windows_agent/
  ├── BUILD.bazel
  ├── wails.json
  ├── cmd/
  │   └── windows_agent/
  │       └── main.go
  ├── internal/
  │   ├── app/
  │   ├── runtime/
  │   ├── transport/
  │   ├── window/
  │   ├── capture/
  │   ├── encoder/
  │   ├── media/
  │   └── input/
  ├── frontend/
  │   ├── package.json
  │   ├── tsconfig.json
  │   ├── vite.config.ts
  │   └── src/
  ├── resources/
  │   ├── icon.ico
  │   └── bin/
  │       └── ffmpeg-metadata.json
  └── release/
      ├── BUILD.bazel
      └── *.go
```

共享构建工具：

```text
tools/wails/
  ├── BUILD.bazel
  ├── defs.bzl
  └── package_helper/ 或 scripts/
```

输入 helper：

```text
projects/game/windows_agent_helper/input/
  ├── BUILD.bazel
  ├── main.go
  ├── command.go
  ├── windows_executor.go
  ├── executor_other.go
  └── *_test.go
```

## 验收标准

milestone 1 的 windows agent 方案验收标准：

* Linux CI 可以通过 Bazel 构建 Windows x64 portable zip。
* Wails CLI 由 Bazel 固定版本管理，不依赖本地机器安装。
* ffmpeg 由 Bazel 固定版本和 SHA256 管理，不保存到仓库。
* 可以通过 `bazel run //projects/game/windows_agent/release:publish_s3 -- --s3-url ...` 发布制品。
* 发布包内包含可执行的 Wails agent、固定版本 `ffmpeg.exe` 和 `input-helper.exe`。
* 发布包上传到指定 S3 prefix，并包含 `manifest.json` 与 `SHA256SUMS`。
* agent 可使用 `session service` 返回的 `agent_connect_url` 连接 gateway。
* agent 可在 WebSocket 建连后发送 `hello(role=windows_agent)`。
* agent 可绑定一个 Windows 目标窗口，并通过 ffmpeg 捕获该窗口画面。
* agent 可发送 H.264 fMP4 `media_init` 与 `media_segment`，且 segment 原始大小不超过 1MiB。
* agent 可连续 60 秒上传视频，gateway 可从视频缓存生成 snapshot。
* agent 可处理 `ping` 并返回 `pong`。
* agent 可处理 `control_request`，返回 `control_ack` 与 `control_result`。
* agent UI 可展示本机预览、连接状态、绑定窗口、ffmpeg 状态、helper 状态和最近错误。
* agent 断线或退出时不会遗留 ffmpeg 子进程、input-helper 子进程或鼠标按住态。
* 重复构建在相同输入下产物稳定，适合 Bazel remote cache。

## 决策详情

### 决策 1：使用 Wails + Go + Web 前端

原因：

* Windows agent 需要本地 UI 展示连接、窗口、媒体和错误状态。
* Go backend 更适合承接 WebSocket、子进程、Win32 窗口枚举、媒体分片和输入 helper 管理。
* Wails 提供本机窗口与 Web 前端集成能力，适合构建轻量本机操作台。
* Go 代码更容易纳入仓库现有 Bazel/rules_go 体系。
* 主 agent 从零搭建可以避免迁就旧实现结构，直接按 Wails 模型组织代码。

### 决策 2：视频捕获与编码依赖 Bazel 管理的 `ffmpeg.exe`

原因：

* ffmpeg 已成熟支持 `gdigrab`、H.264、fMP4、低延迟参数和硬件编码 fallback。
* Linux CI 构建时只需要下载和校验固定 Windows ffmpeg 二进制，不需要编译 Windows 原生编码库。
* 相比自研 Media Foundation/OpenH264/mp4 mux 链路，ffmpeg 方案更适合 milestone 1 先打通端到端视频传输。
* 不把 ffmpeg 二进制保存到仓库，避免大型第三方二进制污染源码。
* 由 Bazel 固定 URL/SHA256 管理后，构建输入稳定，适合 remote cache。

### 决策 3：WebSocket 媒体传输保持 protojson bytes/base64

原因：

* 当前 gateway 已经以 `GameWebSocketEnvelope` protojson 文本帧为线上格式。
* 保持协议不变可以避免 milestone 1 同时修改 agent、gateway、fake agent 和测试工具。
* base64 带来约 33% 额外开销，但通过码率、分辨率和 fragment duration 控制后，M1 可接受。
* protojson 更易调试，便于早期定位媒体 envelope、segment_id 和 key_frame 问题。
* 二进制 WebSocket 帧可在带宽或 CPU 成为瓶颈后作为协议优化单独设计。

### 决策 4：输入执行保留独立 `input-helper.exe`

原因：

* 输入注入有鼠标按住态，独立进程更容易在异常退出、断线或超时时集中释放。
* 输入执行失败不应影响媒体上传、WebSocket 连接和 Wails UI。
* 长时间动作可以通过进程级 kill 作为兜底取消手段。
* helper 可独立测试、独立替换、独立调整权限和 DPI 策略。
* 现有 helper 可在已有代码基础上修改，不需要主 agent 从零实现所有输入细节。

### 决策 5：Wails 封装为 `tools/wails` 下的 Bazel rule

原因：

* 构建和打包逻辑应成为可复用的仓库工具，而不是项目内临时 shell 拼接。
* 自定义 Starlark rule 可以显式声明 inputs/outputs/tools，适合 remote cache。
* `ctx.actions.run` 比 `genrule` 更容易管理 runfiles、工具依赖、树状输出和 downstream providers。
* Wails CLI 作为 Bazel 工具依赖后，构建不受开发者本地安装版本影响。
* 后续其他 Wails 桌面应用也可复用同一 rule。

### 决策 6：agent 只绑定单窗口

原因：

* 简化坐标模型。
* 简化采集和输入注入边界。
* 更符合扫雷场景的阶段目标。
* 便于控制 ffmpeg 参数、segment 大小和诊断状态。

### 决策 7：截图不由 agent 独立上传

原因：

* 当前 gateway 已有基于视频缓存的 snapshot REST 接口。
* `flash_snapshot` 在 gateway 收到 `control_result` 后触发刷新。
* 避免在 agent 与 gateway 间增加第二套图片上传协议。

### 决策 8：对外只接受完整鼠标动作

原因：

* 降低网络抖动导致的异常状态。
* 让 agent 执行逻辑更接近“原子动作执行器”。
* 断线时更容易清理本地输入状态。

### 决策 9：S3 发布使用版本化目录，并由 Bazel run 统一入口执行

原因：

* 每次构建产物可追溯。
* 便于回滚到指定版本。
* `manifest.json` 与 `SHA256SUMS` 可以完整描述本次发布。
* 不依赖桌面框架的自动更新目录约定，先满足指定目录发布要求。
* 使用 `bazel run` 作为发布入口，符合仓库构建工具收敛方向，也便于后续抽象通用 S3 artifact publish rule。

### 决策 10：web 操作页面 pending，agent 只提供本机操作台

原因：

* agent UI 必须解决本机部署、窗口绑定、采集预览和诊断问题。
* web 操作页面当前 pending，不应在 milestone 1 中被仓促实现或替代。
* gateway 仍保留 web 观察者/控制者协议模型，但本次不交付浏览器页面。
* 后续如果确认只需要本机体验，可以再把 web 播放与控制组件复用进 Wails 前端。

## 后续优化

以下能力不阻塞 milestone 1，可在后续版本评估：

* 使用 Windows Graphics Capture 进行更高质量的单窗口 GPU 采集。
* 使用 Media Foundation、OpenH264 或硬件编码 API 进行不依赖 ffmpeg 的编码链路。
* 为 WebSocket 媒体传输增加二进制帧模式，减少 base64 开销。
* 引入自动更新，并为 S3 增加固定 latest 指针。
* 使用 EV 证书或云签名服务降低 Windows SmartScreen 风险。
* 增加键盘输入。
* 修复 gateway domain 层对 `mouse_drag` 起止坐标的表达缺口。
* 将 web 端播放/控制组件内嵌到 Wails 前端，形成更完整的本机 agent UI。
