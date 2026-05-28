# Game Desktop 客户端方案

## 背景

`ideas/llm_agent_play_game/README.md` 规划了一个 agent 玩游戏并持续总结策略的系统。当前 `/projects/game` 已完成 `gateway`、`session`、`proxy`、`agent` 后台服务的基础连通能力，详见 `design/game-agent-step1.md`。

本方案在该后台系统之上增加一个 Windows 桌面客户端。客户端后续会作为用户界面、游戏捕获和游戏操作 agent 的承载入口；本阶段只实现空壳程序和后台接口连通性验证。

## 目标

在 `projects/game/desktop` 下实现一个 Windows Wails 桌面客户端，达成以下效果：

1. 客户端可以通过仓库 Bazel 规则构建为 `windows/amd64` 可执行文件，不使用 `wails` 命令行作为构建入口。
2. 客户端默认连接 `https://game.liukexin.com`，运行时由开发者输入 `env`，不保存本地配置。
3. 客户端接入现有 `gateway` 对外暴露的 session 和 agent 接口。
4. 客户端提供一个最小 Svelte UI，可执行完整连通性检查：创建 session、创建 agent、查询资源、建立 WebSocket、发送 status/echo frame、删除资源。
5. 客户端包含日志框，并提供前后端统一使用的日志打印库，所有操作、请求、响应和错误都能显示在日志框中。
6. BUILD 文件包含 Windows 客户端构建 target 和 S3 推送 target。

## 非目标

本阶段不包含以下内容：

1. 游戏窗口捕获、图像识别、按键/鼠标操作。
2. LLM、DeepAgent、LangChain/LangGraph 集成。
3. 用户认证、账号体系、权限控制。
4. 本地配置持久化、历史 session 管理。
5. 自动执行客户端 UI 验收；实际运行由开发者手动执行。
6. 多平台桌面产物；本阶段只支持 `windows/amd64`。

## 技术选型

| 模块 | 选型 | 说明 |
|---|---|---|
| 桌面框架 | Wails v2 | 仓库已有 `github.com/wailsapp/wails/v2` 和 Bazel Wails 规则 |
| 前端 | Svelte + TypeScript + Vite | 空壳 UI 足够轻量，后续扩展 UI 也方便 |
| 后端 | Go | Wails 后端直接复用仓库 Go/Bazel 体系 |
| 接口访问 | Go HTTP client + WebSocket client | 前端只调用 Wails binding，不直接访问远端接口 |
| 构建 | `//tools/release/wails:defs.bzl` | 使用仓库内 Wails Bazel rule，不使用 `wails build` |
| 发布 | `//tools/release:defs.bzl` 的 `s3_artifacts_push` | 推送到固定 S3 bucket `artifacts` |

## 外部接口

客户端不直连内部 gRPC 服务，只访问 `gateway` 的 HTTP/JSON 和 WebSocket 入口。

默认域名：

```text
https://game.liukexin.com
```

运行时请求头：

```text
env: <开发者输入>
```

### Session 接口

```text
POST   /api/v1/sessions
GET    /api/v1/sessions/{session_id}
DELETE /api/v1/sessions/{session_id}
```

创建 session 请求：

```json
{"session_id":"client-test-001"}
```

### Agent 接口

```text
POST   /api/v1/sessions/{session_id}/agent
GET    /api/v1/sessions/{session_id}/agent
DELETE /api/v1/sessions/{session_id}/agent
```

创建 agent 请求：

```json
{"agent":{}}
```

### Agent WebSocket 接口

```text
GET /api/v1/sessions/{session_id}/agent/connect
```

`https` 域名转换为 `wss`：

```text
wss://game.liukexin.com/api/v1/sessions/{session_id}/agent/connect
```

WebSocket 使用 `AgentFrame` 的 protojson 形态：

```json
{
  "session_id": "client-test-001",
  "type": "status",
  "payload": ""
}
```

`payload` 是 bytes 的 base64 字符串。当前后台空壳行为：

1. 发送 `type=status`，预期返回 `type=status` 且 `payload=aW5pdGlhbGl6ZWQ=`，解码为 `initialized`。
2. 发送 `type=echo`，预期返回 `type=echo` 且 payload 原样回显。

## 模型设计

### Config

运行时配置只保存在内存中。

```go
type Config struct {
    GatewayURL string `json:"gateway_url"`
    Env        string `json:"env"`
}
```

默认值：

```go
Config{GatewayURL: "https://game.liukexin.com"}
```

`Env` 不设置默认值，由开发者在 UI 中输入。

### Session

对应 gateway 返回的 session JSON。

```go
type Session struct {
    Name       string `json:"name"`
    SessionID  string `json:"session_id"`
    CreateTime string `json:"create_time"`
}
```

### Agent

对应 gateway 返回的 agent JSON。

```go
type Agent struct {
    Name       string `json:"name"`
    SessionID  string `json:"session_id"`
    OwnerIndex int32  `json:"owner_index"`
    Owner      string `json:"owner"`
    CreateTime string `json:"create_time"`
}
```

### AgentFrame

```go
type AgentFrame struct {
    SessionID string `json:"session_id"`
    Type      string `json:"type"`
    Payload   string `json:"payload"`
}
```

### LogEntry

日志同时服务后端调试和 UI 展示。

```go
type LogEntry struct {
    Time    string         `json:"time"`
    Level   string         `json:"level"`
    Source  string         `json:"source"`
    Message string         `json:"message"`
    Fields  map[string]any `json:"fields,omitempty"`
}
```

`source` 取值：

```text
frontend
backend
```

## 代码分层

目标目录：

```text
projects/game/desktop/
  BUILD.bazel
  README.md
  release.yaml
  main.go
  app.go
  internal/
    api/
      client.go
      models.go
      websocket.go
    applog/
      logger.go
  assets/
    BUILD.bazel
  frontend/
    BUILD.bazel
    package.json
    tsconfig.json
    vite.config.ts
    index.html
    src/
      App.svelte
      main.ts
      logger.ts
      api.ts
      style.css
```

说明：

| 路径 | 职责 |
|---|---|
| `main.go` | Wails 启动入口，绑定 App，加载前端 assets |
| `app.go` | 暴露给 Svelte 前端调用的 Wails 方法 |
| `internal/api` | 封装 gateway HTTP/JSON 和 WebSocket 访问 |
| `internal/applog` | 后端日志库，负责内存日志和 Wails event 推送 |
| `frontend/src/App.svelte` | 主界面、表单、按钮、日志框 |
| `frontend/src/api.ts` | Wails binding 调用封装 |
| `frontend/src/logger.ts` | 前端日志库 |
| `release.yaml` | S3 发布 manifest |

## Wails 后端 API

`App` 暴露给前端的方法：

```go
func (a *App) GetConfig(ctx context.Context) Config
func (a *App) SetConfig(ctx context.Context, cfg Config) error

func (a *App) CreateSession(ctx context.Context, sessionID string) (*api.Session, error)
func (a *App) GetSession(ctx context.Context, sessionID string) (*api.Session, error)
func (a *App) DeleteSession(ctx context.Context, sessionID string) error

func (a *App) CreateAgent(ctx context.Context, sessionID string) (*api.Agent, error)
func (a *App) GetAgent(ctx context.Context, sessionID string) (*api.Agent, error)
func (a *App) DeleteAgent(ctx context.Context, sessionID string) error

func (a *App) ConnectAgent(ctx context.Context, sessionID string) error
func (a *App) SendAgentFrame(ctx context.Context, frame api.AgentFrame) (*api.AgentFrame, error)
func (a *App) CloseAgent(ctx context.Context) error

func (a *App) RunConnectivityCheck(ctx context.Context, sessionID string) (*api.CheckResult, error)
func (a *App) Logs(ctx context.Context) []applog.Entry
```

`RunConnectivityCheck` 按以下顺序执行：

```text
CreateSession
CreateAgent
GetSession
GetAgent
ConnectAgent
Send status frame
校验 status payload == initialized
Send echo frame
校验 echo payload 原样返回
DeleteAgent
DeleteSession
```

## UI 设计

界面只做空壳和连通性验证，分为三块。

### 连接配置区

字段：

1. Gateway URL，默认 `https://game.liukexin.com`。
2. Env，开发者输入。
3. Session ID，默认生成 `desktop-{timestamp}`，允许手动修改。

配置仅保存在进程内存中，不写本地文件。

### 操作区

按钮：

1. Apply Config
2. Create Session
3. Get Session
4. Create Agent
5. Get Agent
6. Connect WebSocket
7. Send Status
8. Send Echo
9. Delete Agent
10. Delete Session
11. Full Connectivity Check

每个按钮执行前后都写日志。失败时日志框展示错误信息，不吞掉后端错误。

### 日志区

日志框要求：

1. 显示时间、level、source、message。
2. 支持展示 JSON fields。
3. 新日志自动滚动到底部。
4. 支持 Clear Logs。

## 日志库设计

后端 `internal/applog` 负责：

1. 追加内存日志。
2. 在 Wails runtime 可用后通过事件推送日志：

```text
game:log
```

3. 提供 `Entries()` 给前端初始化时拉取已有日志。

前端 `logger.ts` 负责：

1. 监听 `game:log`。
2. 前端本地操作也通过同一日志格式写入 UI。
3. 必要时调用 Wails runtime 的 log 方法，方便开发调试。

## Bazel 构建方案

使用仓库内 Wails 规则：

```bzl
load("//tools/release/wails:defs.bzl", "wails_app", "wails_asset_library")
```

### 前端 assets

Svelte/Vite 构建输出 `frontend/dist`，再通过 `wails_asset_library` 生成 Go embed library。

示意：

```bzl
wails_asset_library(
    name = "assets",
    src = ":frontend_dist_src",
    importpath = "dominion/projects/game/desktop/assets",
    visibility = ["//visibility:public"],
)
```

### Windows 应用

```bzl
wails_app(
    name = "desktop",
    assets = ":assets_provider",
    binary_name = "game-desktop",
    go_library = ":desktop_lib",
    platform = "windows/amd64",
    visibility = ["//visibility:public"],
)
```

`wails_app` 内部使用：

1. `goos = "windows"`
2. `goarch = "amd64"`
3. `gotags = ["production", "wv2runtime.embed"]`
4. `gc_linkopts = ["-w", "-s", "-H", "windowsgui"]`

## S3 发布方案

使用仓库已有规则：

```bzl
load("//tools/release:defs.bzl", "s3_artifacts_push")
```

`tools/release/s3push` 已固定：

1. S3 bucket: `artifacts`
2. object key: `{name}/{version}/{filename}`
3. 同时上传 artifact、`manifest.json`、`SHA256SUMS`

首版版本号接受默认值：

```text
0.1.0
```

`release.yaml`：

```yaml
name: game-desktop
version: 0.1.0
artifacts:
  - target: //projects/game/desktop:desktop
    filename: game-desktop-windows-amd64.exe
    platform: windows
    arch: amd64
```

推送 target：

```bzl
s3_artifacts_push(
    name = "desktop_push",
    manifest = ":release.yaml",
    artifacts = {
        "//projects/game/desktop:desktop": ":desktop",
    },
    visibility = ["//visibility:public"],
)
```

上传后的对象路径：

```text
artifacts/game-desktop/0.1.0/game-desktop-windows-amd64.exe
artifacts/game-desktop/0.1.0/manifest.json
artifacts/game-desktop/0.1.0/SHA256SUMS
```

## 测试方案

### Go 单元测试

建议覆盖：

1. `internal/api` HTTP client
   - method/path/body 正确。
   - `env` header 正确。
   - 非 2xx 响应返回可读错误。
2. `internal/api` WebSocket client
   - `https` 到 `wss` 转换正确。
   - status/echo frame 编解码正确。
3. `internal/applog`
   - 日志追加顺序正确。
   - `Entries()` 返回副本。
   - event sink 可被替换测试。

执行：

```bash
bazel test //projects/game/desktop/...
```

### 前端验证

前端至少要求 TypeScript/Svelte build 通过。命令由 Bazel target 封装，不直接使用裸 `pnpm` 作为交付入口。

### 构建验证

```bash
bazel build //projects/game/desktop:desktop
```

### 发布 target 验证

发布由开发者在具备 S3 网络和权限的环境执行：

```bash
bazel run //projects/game/desktop:desktop_push
```

### 手动运行验收

由开发者实际运行 Windows 客户端，验收步骤：

1. 启动 `game-desktop.exe`。
2. 确认 Gateway URL 默认为 `https://game.liukexin.com`。
3. 输入当前测试环境 `env`。
4. 使用默认生成的 session id 或手动输入。
5. 点击 `Full Connectivity Check`。
6. 日志框中应出现完整链路：

```text
config applied
session created
agent created
session fetched
agent fetched
websocket connected
status response initialized
echo response ok
agent deleted
session deleted
connectivity check passed
```

7. 单独点击每个操作按钮也应能得到对应日志和响应。

## 决策详情

### 为什么客户端访问 gateway 而不是内部 gRPC

`gateway` 是现有系统的对外边界，已经负责 grpc-gateway 和 WebSocket 转换。客户端接入 gateway 可以复用线上同一套 HTTP 路由、`env` header、WebSocket 行为和系统测试路径，避免桌面客户端绑定内部服务发现和 gRPC resolver。

### 为什么网络访问放在 Go 后端

Wails 前端运行在 WebView 中。将 HTTP/WebSocket 放在 Go 后端有三个好处：

1. 避免 WebView CORS、证书和浏览器 API 差异影响。
2. 后续游戏捕获、操作 agent、本地进程交互都更适合在 Go 后端统一编排。
3. 日志、超时、错误包装和测试更容易复用 Go/Bazel 体系。

### 为什么使用 Svelte

本阶段 UI 是空壳但后续会扩展为交互界面。Svelte 比 Vanilla TS 更适合维护状态和组件，同时比引入大型 UI 框架更轻量。仓库 `pnpm-workspace.yaml` 已有 Svelte/Vite/TypeScript 版本约束。

### 为什么不使用 `wails` 命令构建

仓库已有 `tools/release/wails` Bazel 规则，能够生成 Windows Wails production binary。使用 Bazel 可以把 Go、前端 assets、Windows 可执行文件和 S3 发布 target 串成统一构建图，符合仓库构建规范。

### 为什么首版版本号固定为 `0.1.0`

`s3push` manifest 要求 SemVer。当前是第一个 desktop 空壳版本，固定 `0.1.0` 简单明确；后续需要发布新版本时只更新 `release.yaml`，或再引入 stamping/自动版本规则。

## 未来规划

以下内容不属于本阶段，但目录和分层为后续保留扩展空间：

1. 本地保存 gateway/env/session 历史配置。
2. 增加 Windows 资源信息：icon、产品名、版本 metadata。
3. 增加游戏窗口发现、截图捕获、输入操作模块。
4. 增加 agent 实时消息视图和游戏状态面板。
5. 增加自动更新机制，读取 S3 `manifest.json` 和 `SHA256SUMS`。
6. 增加更多平台产物，如 `linux/amd64`、`darwin/arm64`。

## 待定项

无。
