# LLM Agent 玩扫雷 milestone 1 windows agent 详细技术方案

## 目标

本方案用于单独描述 milestone 1 中 `windows agent` 的职责、边界、技术选型、构建发布方式和与 `game gateway` 的协议对接方式，目标是：

* 让 `windows agent` 成为部署在目标 Windows 主机上的本地执行端。
* 让其稳定承接单窗口画面采集、H.264 fMP4 视频编码上传和鼠标输入执行。
* 让其通过当前 `game gateway` WebSocket 协议接入系统，而不直接对 `web` 暴露。
* 让 Windows 应用制品可以在 Linux CI 上稳定构建，并发布到指定 S3 目录。

本方案希望达成的效果是：开发者在 Linux CI 中可以可重复地产出 `windows_agent` Windows 安装/便携制品；Windows 主机运行该 agent 后，可以按 `session service` 返回的 `agent_connect_url` 接入指定 gateway，上传低延迟视频流，并执行来自 web 的完整鼠标调试动作。

## 范围

本方案覆盖：

* Electron + React + TypeScript 技术栈选型
* 单窗口绑定模型
* 视频采集、随包 `ffmpeg.exe` 编码与 fMP4 分片上传
* 鼠标输入执行
* 与 `game gateway` 的 WebSocket 协议对接
* Linux 构建 Windows 应用制品
* 制品发布到 S3 指定目录

本方案不包括：

* `session service` 生命周期管理
* `game gateway` 的媒体缓存与对外 REST 接口
* Web 操作页面实现，本次保持 pending
* 键盘输入实现
* Windows Graphics Capture / Media Foundation / NVENC 的原生深度集成
* 自动更新机制的完整实现
* 完整替代 web 端的远程观察与控制体验

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
  * 单个 init/segment 不得超过 `domain.MaxSegmentSize = 1MiB`。
  * `key_frame = true` 必须标记可供 web 追帧的关键帧边界。
* 控制请求由 web 经 gateway 转发到 agent，agent 需要按 `control_request -> control_ack -> control_result` 顺序处理。
* gateway 当前从视频缓存中按需生成 JPEG snapshot；agent 不需要常态上传 JPEG snapshot。

当前实现里还存在一个需要先处理的协议差异：`gateway.proto` 的 `GameMouseAction` 包含 `from_x/from_y/to_x/to_y`，但 gateway domain 层 `ControlRequestPayload` 当前只保留了 `x/y/duration_ms`。由于 gateway 会先把 web 的 `control_request` 转成 domain payload 再转发给 agent，drag 起止坐标会在转发前丢失。完整 `mouse_drag` 需要先补齐 gateway domain/control 映射；如果不修复该缺口，则 milestone 1 的 drag 只能按当前 `x/y/duration_ms` 能表达的降级语义执行。

## 总体技术选型

milestone 1 采用：

* 主应用：`Electron + React + TypeScript`
* 打包工具：`electron-builder`
* Linux 构建 Windows 制品：`electronuserland/builder:wine` 或等价 Linux + Wine 构建环境
* WebSocket：Electron main process 中使用 Node.js WebSocket 客户端
* Protobuf / protojson：使用 TypeScript protobuf 生成物维护 `GameWebSocketEnvelope` 类型，并以 protojson 形态收发
* 视频编码：随包分发固定版本的 Windows `ffmpeg.exe`
* 输入执行：优先选择不要求 Linux 编译 Windows native addon 的方案；必要时使用独立 Windows helper 可执行文件承接输入动作

UI 形态采用 **agent 本机操作台**：Electron renderer 提供最小可用的连接、绑定、预览、状态与日志界面。milestone 1 不实现 web 操作页面，也不把 web 浏览器完整合并进 agent；gateway 的 web 连接能力保留为后续入口。后续如果确认需要远程浏览器或本机完整操作台，再单独设计 web 操作页面，或让 agent renderer 复用 web 的播放/控制组件。

### 选型原则

核心约束是 **Linux 稳定构建 Windows 应用**。因此 milestone 1 禁止把主链路建立在必须由 Linux CI 编译 Windows C/C++ native addon 的能力上。

可接受的运行时依赖包括：

* Electron / Chromium / Node.js 自带能力。
* 已经预编译且可校验的 Windows 二进制，例如随包 `ffmpeg.exe`。
* 可以由 Linux 稳定交叉编译的独立 helper，例如纯 Go `GOOS=windows GOARCH=amd64` helper。

不作为 milestone 1 主路径的能力包括：

* C++/WinRT Windows Graphics Capture 绑定。
* Media Foundation in-process 编码。
* 需要 Windows SDK + node-gyp 在 Linux 上交叉编译的 Node native addon。
* 直接链接 x264 / NVENC SDK / AMF SDK 的进程内编码器。

## 组件定位

`windows agent` 负责：

* 提供本地 UI，用于输入或展示 session connect URL、连接状态、窗口绑定状态、媒体状态和错误日志。
* 提供本机预览和本地诊断能力，帮助操作者确认窗口绑定、采集和 gateway 连接是否正常。
* 绑定单个目标窗口。
* 采集该窗口画面。
* 调度随包 `ffmpeg.exe` 编码为 H.264 fragmented MP4。
* 将 fMP4 init/segment 通过 WebSocket 上传到 `game gateway`。
* 执行来自 `game gateway` 的完整鼠标动作。
* 回传 `control_ack`、`control_result`、`error`、`pong`。
* 在断线或退出时重置输入状态并停止采集/编码子进程。

`windows agent` 不负责：

* 直接服务浏览器。
* 决定连接哪个 gateway。
* 生成或解析 session token。
* 保存长期 session 状态。
* 向 gateway 常态推送 JPEG snapshot。
* 持久化媒体数据。
* 在 milestone 1 中实现 web 操作页面或完整取代 web 端远程 UI。

## 应用模型

### Electron 进程划分

`main process` 负责所有系统能力和外部协议：

* 读取本地配置。
* 管理 WebSocket 连接。
* 管理窗口枚举与绑定。
* 管理画面采集会话。
* 启动、监控和停止 `ffmpeg.exe`。
* 将 ffmpeg 输出切分为 `media_init` / `media_segment`。
* 执行鼠标输入。
* 通过 IPC 向 renderer 上报状态和日志。

`renderer process` 只负责 UI：

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
* 如果后续决定不要独立 web 浏览器，应优先复用 web 播放与控制组件到 Electron renderer，而不是在 agent 中重写第二套 UI。

`preload` 负责暴露受控 IPC API，renderer 不直接访问 Node.js、文件系统、子进程或 WebSocket。

### 建议代码分层

建议按如下层次实现：

* `app`：Electron 生命周期、窗口创建、托盘和退出处理。
* `ui`：React renderer、状态页、窗口选择和日志面板。
* `runtime`：agent 本地状态机、session 信息、重连与清理。
* `transport`：gateway WebSocket 连接、protojson envelope 编解码、ping/pong。
* `capture`：窗口枚举、窗口绑定、画面采集。
* `encoder`：`ffmpeg.exe` 路径解析、参数生成、进程管理、stdout fMP4 解析。
* `media`：`media_init` / `media_segment` 组包、大小限制、关键帧标记。
* `input`：鼠标动作执行、坐标转换、断线状态重置。
* `release`：构建版本、manifest、S3 发布脚本。

## 窗口绑定模型

milestone 1 收敛为：

* 一次 session 只绑定 **一个窗口**。
* 所有输入坐标均使用 **窗口相对坐标**。
* UI 中由用户从窗口列表中选择目标窗口。
* agent 内部保存窗口标识、标题、进程信息和最近一次窗口矩形。

窗口状态处理：

* 正常可见窗口：持续采集并上传视频。
* 失焦或被遮挡：优先继续采集；如果当前采集方式只能得到遮挡后的画面，则如实上传，不伪造帧。
* 最小化或窗口消失：暂停视频上传，保持 WebSocket 控制链路可用，并上报错误或状态。
* 窗口重新出现：允许用户重新绑定，或在同一窗口标识可恢复时自动恢复采集。

## 视频采集与编码

### milestone 1 主链路

视频主链路首版采用最容易跑通的 Electron capture / MediaRecorder 到 ffmpeg 转码链路：

```text
绑定窗口
  -> Electron/Chromium 采集窗口画面
  -> MediaRecorder 产出浏览器可编码 chunk
  -> ffmpeg.exe 编码为 H.264 fragmented MP4
  -> Electron main process 读取 fMP4 stdout
  -> 拆分 media_init / media_segment
  -> WebSocket 上传到 game gateway
```

首版优先验证端到端可用性与 gateway 协议兼容性；如果延迟或 CPU 开销不可接受，再优化为 raw frame pipe、Windows Graphics Capture 或硬件编码路径。

### 为什么随包 `ffmpeg.exe`

随包 `ffmpeg.exe` 表示：`windows_agent` 发布包中携带一个固定版本的 Windows ffmpeg 可执行文件，运行时由 Electron main process 通过 `child_process.spawn` 启动。

这样做的原因：

* Linux CI 构建时只需要复制 `ffmpeg.exe`，不需要编译 Windows 原生编码库。
* 避免 Electron native addon、Media Foundation、x264、NVENC SDK 等编译链路成为发布阻塞。
* ffmpeg 对 H.264、fMP4、低延迟参数和硬件编码 fallback 的支持成熟。
* agent 不依赖用户机器预装 ffmpeg，也不依赖 PATH 中的未知版本。

运行时路径应从 Electron resources 目录解析，例如：

```text
resources/bin/ffmpeg.exe
```

如果随包 ffmpeg 不存在、版本不匹配或 SHA256 校验失败，agent 应停止媒体链路并报告 `encoder_unavailable`，而不是回退到系统 PATH。

### ffmpeg 输出格式

gateway 当前要求 agent 上传 H.264 fMP4，因此 ffmpeg 输出必须满足：

* codec：H.264。
* container：fragmented MP4。
* 输出：stdout pipe，供 main process 实时读取。
* GOP：按关键帧边界可切分。
* segment：单个 WebSocket payload 不超过 1MiB。
* 低延迟：避免 B-frames，优先低延迟 preset/tune。

实现前需要在该首版链路内落定 capture 到 ffmpeg 的具体交接格式，包括 MediaRecorder mime type、chunk 周期、帧率、分辨率、码率、GOP、fragment 时长，以及编码进程重启后如何重新发送 `media_init`。这是实现设计的一部分，不能只停留在“调用 ffmpeg”这一层描述。

参数方向如下，实际实现可根据输入源调整：

```bash
ffmpeg.exe \
  -i <capture-input> \
  -an \
  -c:v libx264 \
  -preset ultrafast \
  -tune zerolatency \
  -pix_fmt yuv420p \
  -movflags frag_keyframe+empty_moov+default_base_moof \
  -f mp4 pipe:1
```

如目标机器支持硬件编码，可在后续版本探测 `h264_nvenc`、`h264_qsv`、`h264_amf`，失败后回退到 `libx264`。milestone 1 不强依赖硬件编码。

### fMP4 切分与上传

agent 需要把 ffmpeg stdout 中的 MP4 box 流解析为：

* `ftyp + moov`：作为 `media_init` 发送。
* 每组 `moof + mdat`：作为一个 `media_segment` 发送。

要求：

* `media_init.mime_type` 使用 `video/mp4; codecs="avc1..."` 格式。
* 每个 segment 生成递增且唯一的 `segment_id`。
* 每个从关键帧开始的 fragment 设置 `key_frame = true`。
* 如果 fragment 超过 1MiB，需要调整 ffmpeg GOP、帧率、码率或 fragment 参数，而不是强行发送超限消息。
* ffmpeg 退出或 stderr 出现不可恢复错误时，停止媒体上传并保留 WebSocket 控制链路。
* 编码器重启后必须重新发送新的 `media_init`，再继续发送新的 `media_segment`。

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

milestone 1 的输入实现采用 **独立 `input-helper.exe` 主路径**：

* Electron main process 通过 stdin/stdout、本地 pipe 或等价 IPC 向 helper 下发完整鼠标动作。
* `input-helper.exe` 负责窗口相对坐标转换、SendInput/PostMessage 调用、执行结果返回和状态清理。
* helper 作为独立 Windows 可执行文件随 Electron 应用一起分发，放入 `resources/bin/`。
* helper 优先使用 Linux 可稳定交叉编译的实现，例如纯 Go `GOOS=windows GOARCH=amd64` 构建。
* 不在 milestone 1 主链路中引入需要 Windows SDK + node-gyp 交叉编译的输入库。

选择独立 helper 的原因：

* 避免 Electron native addon 与 Electron ABI、node-gyp、Windows SDK 的耦合。
* 便于在断线、超时、进程退出时独立清理鼠标按住态。
* 便于后续替换输入实现，而不影响 Electron UI 和 WebSocket 主进程。

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
* 新连接建立后只恢复媒体与控制服务能力，不恢复旧操作。

## 与 gateway 的交互

### 连接

agent 使用 `session service` 返回的完整 `agent_connect_url`，不自行拼接 gateway 地址，不解析 token 语义。

连接流程：

1. 用户或本地配置提供 `agent_connect_url`。
2. Electron main process 建立 WebSocket。
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

所有上行消息均为 `GameWebSocketEnvelope` protojson 文本帧；每条消息必须设置 `session_id` 和唯一 `message_id`。JSON 中 bytes 字段按 protojson 规则使用 base64 表达，enum 使用 protojson 可接受的枚举表达方式。除非 gateway 增加适配层，agent 不应发送自定义 `{type, payload}` envelope。

### 下行消息

agent 需要处理：

* `control_request`
* `ping`
* `error`

其他面向 web 的媒体消息如果被错误路由到 agent，agent 应忽略并记录调试日志，不应崩溃。

## Linux 构建 Windows 应用

### 构建目标

Linux CI 需要产出至少一种可运行制品：

* Windows portable zip：用于测试、手工部署和快速分发。

可选产出：

* NSIS installer：用于正式安装。
* blockmap / latest metadata：用于后续自动更新。

### 构建工具

建议使用：

* `pnpm` 管理 TypeScript/Electron 依赖。
* Bazel 负责编译 TypeScript、收集资源、封装 Electron 打包动作和统一发布入口。
* `electron-builder` 打包 Windows 制品。
* `electronuserland/builder:wine` 作为 CI 容器或参考环境。

本仓库标准编译工具是 Bazel。Electron 应用可以继续使用 pnpm/electron-builder 完成 Electron 生态打包，但 TypeScript 编译、Electron 打包动作和发布入口都应由 Bazel target 封装，避免每个开发者或 CI 直接拼接临时脚本。

### TypeScript 编译

TypeScript 应使用 Bazel 规则编译，沿用仓库当前 `experimental/ts/hello_world/BUILD.bazel` 中的模式：

* `ts_project` 负责 TypeScript 编译和类型产物。
* `swc` 作为 transpiler。
* `js_binary` 可用于本地开发或工具入口。

Electron 项目建议拆分为多个 Bazel target：

* `main_lib`：Electron main process。
* `preload_lib`：preload 和 `contextBridge` API。
* `renderer_lib` 或 `renderer_bundle`：React renderer。
* `package_json` / `resources` / `runtime_bins`：打包所需元数据和资源。

这样可以在打包前通过 Bazel 明确验证 TS 编译、类型检查和资源依赖，而不是把 TS 编译隐式藏在 electron-builder 命令里。

### Electron 打包封装

Electron 打包仍使用 `electron-builder`，但应封装为 Bazel rule 或 macro，例如：

```starlark
electron_builder_package(
    name = "windows_agent_win_zip",
    app_dir = ":app_dir",
    config = ":electron-builder.yml",
    platform = "win",
    targets = ["portable", "nsis"],
    outs = [
        "windows-agent-0.1.0-win32-x64.zip",
        "windows-agent-0.1.0-win32-x64.exe",
    ],
)
```

该 Bazel 封装层负责：

1. 依赖 TS 编译 target。
2. 准备 electron-builder 所需的 app 目录。
3. 复制 `ffmpeg.exe`、`input-helper.exe`、icon、license 等 resources。
4. 调用 `electron-builder --win portable nsis`。
5. 将输出产物声明为 Bazel outputs。

该 rule 不重写 Electron 打包逻辑，只把 electron-builder 作为底层工具调用，从而保留 Electron 生态兼容性，同时让仓库入口保持 Bazel 化。

### Linux 到 Windows 打包可行性与风险

Electron Windows 制品可以在 Linux 上打包。推荐 CI 使用 `electronuserland/builder:wine` 或等价包含 Wine、Node、pnpm 和 electron-builder 依赖的固定镜像。

Linux 打 Windows 的可行范围：

* portable Windows `.exe` / `.zip`：作为 milestone 1 主目标。
* NSIS installer：可选目标。
* Windows Electron runtime：由 electron-builder 下载和组装。

主要风险：

* **native addon 风险**：如果 npm 依赖需要 `node-gyp` 编译 Windows native addon，会引入 Windows SDK、Visual Studio Build Tools、Electron ABI rebuild 等问题。milestone 1 禁止这类依赖进入主链路。
* **代码签名风险**：普通 pfx/OV 证书可后续在 Linux 上处理；EV/HSM 证书和 SmartScreen 规避不作为 milestone 1 阻塞项。
* **electron-builder 下载与缓存风险**：electron-builder 会下载 Electron runtime、NSIS 等工具。milestone 1 可通过固定 CI 镜像和锁定依赖版本降低风险；后续再评估更 hermetic 的 Bazel repository 管理。
* **跨平台范围风险**：Linux 打 Windows 可行；Linux 打 macOS 不作为本项目目标。

因此 milestone 1 的构建原则是：**Bazel 编译 TS + Bazel 封装 electron-builder + Bazel run 发布 S3**。不要尝试绕开 electron-builder 自己实现 Electron 打包，也不要引入需要 Linux 编译 Windows native addon 的 npm 依赖。

本地开发构建命令方向：

```bash
pnpm install --frozen-lockfile
pnpm --filter @dominion/windows-agent build
pnpm --filter @dominion/windows-agent electron-builder --win portable nsis
```

正式发布入口方向：

```bash
bazel run //projects/game/windows_agent/release:publish_s3 -- \
  --version 0.1.0 \
  --s3-url s3://<bucket>/<prefix>/windows-agent/0.1.0/
```

`publish_s3` 目标负责：

1. 触发或校验 Electron Windows 产物已生成。
2. 收集 portable zip、可选 installer、`ffmpeg.exe` 版本信息和 helper 版本信息。
3. 生成 `manifest.json`。
4. 生成 `SHA256SUMS`。
5. 上传到指定 S3 URL。

仓库当前 `pnpm-workspace.yaml` 只包含 `experimental/ts/*`，新增 agent 工程时需要把 `projects/game/windows_agent` 或最终目录加入 workspace。

### native 依赖约束

构建必须满足：

* 不要求 Linux CI 编译 Windows Node native addon。
* 不要求安装 Windows SDK。
* 不要求安装 Visual Studio Build Tools。
* 不要求 MinGW 参与 Electron 主应用构建。

独立 `input-helper.exe` 应使用纯 Go 交叉编译或明确配置 Bazel/rules_go `windows_amd64` 构建，并由 Electron 打包流程复制到 `resources/bin/`，不影响 Electron 主应用打包。

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
  ├── windows-agent-0.1.0-win32-x64.zip
  ├── windows-agent-0.1.0-win32-x64.exe
  ├── manifest.json
  └── SHA256SUMS
```

`manifest.json` 至少记录：

* agent 版本。
* git commit。
* 构建时间。
* 平台与架构。
* Electron 版本。
* ffmpeg 版本。
* ffmpeg 来源。
* ffmpeg SHA256。
* 每个发布制品的文件名、大小和 SHA256。

### S3 上传方式

milestone 1 建议使用 Bazel 可执行发布入口，而不是直接使用 `electron-builder` S3 provider。原因是本仓库已有 Bazel 作为统一构建入口，且发布前需要生成 manifest、checksum，并收集 Electron app、`ffmpeg.exe` 与 `input-helper.exe` 的版本元信息。

发布入口可以有两种实现方式：

1. 先实现 `bazel run` 的发布工具目标，例如 `//projects/game/windows_agent/release:publish_s3`。
2. 如果后续多个制品都需要发布到 S3，再抽象一个 Starlark rule 或 macro，例如 `s3_artifact_publish`，用于声明待上传文件、manifest 模板和目标 S3 prefix。

建议先做第 1 种，等复用需求明确后再沉淀通用 rule。

上传使用的凭证遵循仓库已有 S3 约定：

* `S3_ACCESS_KEY`
* `S3_SECRET_KEY`

发布工具应优先复用仓库 `pkg/s3` 的 endpoint 与凭证约定；如果内部调用 AWS CLI，则由工具或 CI 将上述凭证映射到 AWS CLI 需要的环境变量。

如果发布工具内部使用 AWS CLI 访问仓库当前 SeaweedFS S3，需要显式配置 endpoint：

```bash
AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY" \
AWS_SECRET_ACCESS_KEY="$S3_SECRET_KEY" \
aws --endpoint-url https://s3.liukexin.com s3 cp dist/ "s3://<bucket>/<prefix>/windows-agent/<version>/" --recursive
```

如不希望发布工具依赖 AWS CLI endpoint 细节，则实现 Go 发布工具，直接复用 `pkg/s3`。

### 可复用 Bazel rule 方向

如果后续需要沉淀通用 Bazel rule，可提供类似声明：

```starlark
s3_artifact_publish(
    name = "publish_s3",
    srcs = [
        ":windows_agent_zip",
        ":windows_agent_installer",
        ":manifest",
        ":sha256sums",
    ],
    s3_url = "$(S3_RELEASE_URL)",
)
```

该 rule 只负责发布已存在的构建产物，不隐式修改源文件或生成业务代码。`s3_url` 应支持通过命令行参数或环境变量注入，避免把发布目录硬编码到 BUILD 文件。

## 建议仓库结构

建议新增目录：

```text
projects/game/windows_agent/
  ├── package.json
  ├── electron-builder.yml
  ├── vite.config.ts
  ├── tsconfig.json
  ├── src/
  │   ├── main/
  │   │   ├── index.ts
  │   │   ├── runtime.ts
  │   │   ├── transport.ts
  │   │   ├── capture.ts
  │   │   ├── encoder.ts
  │   │   ├── media.ts
  │   │   └── input.ts
  │   ├── preload/
  │   │   └── index.ts
  │   └── renderer/
  │       ├── App.tsx
  │       └── components/
  ├── resources/
  │   ├── icon.ico
  │   └── bin/
  │       └── ffmpeg.exe
  └── scripts/
      ├── build-release.ts
      └── publish-s3.ts
  └── release/
      ├── BUILD.bazel
      └── publish_s3.go
```

如果采用独立输入 helper，可新增：

```text
projects/game/windows_agent_helper/input/
```

helper 的发布产物复制到 Electron `resources/bin/` 下。

## 验收标准

milestone 1 的 windows agent 方案验收标准：

* Linux CI 可以构建 Windows x64 portable zip。
* 可以通过 `bazel run //projects/game/windows_agent/release:publish_s3 -- --s3-url ...` 发布制品。
* 发布包内包含可执行的 Electron agent 和固定版本 `ffmpeg.exe`。
* 发布包内包含 `input-helper.exe`，并记录 helper 版本或构建 commit。
* 发布包上传到指定 S3 prefix，并包含 `manifest.json` 与 `SHA256SUMS`。
* agent 可使用 `session service` 返回的 `agent_connect_url` 连接 gateway。
* agent 可在 WebSocket 建连后发送 `hello(role=windows_agent)`。
* agent 可发送 H.264 fMP4 `media_init` 与 `media_segment`，且 segment 不超过 1MiB。
* agent 可处理 `ping` 并返回 `pong`。
* agent 可处理 `control_request`，返回 `control_ack` 与 `control_result`。
* agent UI 可展示本机预览、连接状态、绑定窗口、ffmpeg 状态和最近错误。
* agent 断线或退出时不会遗留 ffmpeg 子进程或鼠标按住态。

## 决策详情

### 决策 1：使用 Electron + React + TypeScript

原因：

* Windows agent 需要本地 UI 展示连接、窗口、媒体和错误状态。
* Electron 生态对 Linux 构建 Windows 安装/便携制品支持成熟。
* React + TypeScript 适合快速构建调试 UI 和受控状态面板。
* WebSocket、子进程、文件系统和本地配置可以集中在 main process，renderer 通过受控 IPC 访问。

### 决策 2：视频编码依赖随包 `ffmpeg.exe`

原因：

* 保证 Linux CI 构建时不需要编译 Windows 原生编码库。
* 避免 Media Foundation、x264、NVENC SDK 或 Node native addon 的构建复杂度。
* ffmpeg 已成熟支持 H.264、fMP4、低延迟参数和硬件编码 fallback。
* 固定随包版本后，运行行为更可控，也便于记录 SHA256 和来源。

### 决策 3：截图不由 agent 独立上传

原因：

* 当前 gateway 已有基于视频缓存的 snapshot REST 接口。
* `flash_snapshot` 在 gateway 收到 `control_result` 后触发刷新。
* 避免在 agent 与 gateway 间增加第二套图片上传协议。

### 决策 4：agent 只绑定单窗口

原因：

* 简化坐标模型。
* 简化采集和输入注入边界。
* 更符合扫雷场景的阶段目标。

### 决策 5：对外只接受完整鼠标动作

原因：

* 降低网络抖动导致的异常状态。
* 让 agent 执行逻辑更接近“原子动作执行器”。
* 断线时更容易清理本地输入状态。

### 决策 6：S3 发布使用版本化目录，并由 Bazel run 统一入口执行

原因：

* 每次构建产物可追溯。
* 便于回滚到指定版本。
* `manifest.json` 与 `SHA256SUMS` 可以完整描述本次发布。
* 不依赖 Electron auto-update 的目录约定，先满足指定目录发布要求。
* 使用 `bazel run` 作为发布入口，符合仓库构建工具收敛方向，也便于后续抽象通用 S3 artifact publish rule。

### 决策 7：web 操作页面 pending，agent 只提供本机操作台

原因：

* agent UI 必须解决本机部署、窗口绑定、采集预览和诊断问题。
* web 操作页面当前 pending，不应在 milestone 1 中被仓促实现或替代。
* gateway 仍保留 web 观察者/控制者协议模型，但本次不交付浏览器页面。
* 后续如果确认只需要本机体验，可以再把 web 播放/控制组件复用进 Electron renderer。

## 后续优化

以下能力不阻塞 milestone 1，可在后续版本评估：

* 使用 Windows Graphics Capture 进行更高质量的单窗口 GPU 采集。
* 使用 Media Foundation 或硬件编码 API 进行进程内 H.264 编码。
* 引入自动更新，并为 S3 增加固定 `latest.yml` 指针。
* 使用 EV 证书或云签名服务降低 Windows SmartScreen 风险。
* 增加键盘输入。
* 修复 gateway domain 层对 `mouse_drag` 起止坐标的表达缺口。
* 将 web 端播放/控制组件内嵌到 Electron renderer，形成单体本机 agent UI。
