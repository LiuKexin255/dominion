# Wails Bazel 终态构建方案

## 目标

本方案用于从 0 重新设计仓库内 Wails 应用的 Bazel 构建能力，使 `projects/game/windows_agent` 能通过 Bazel 完成可复现、可缓存、可声明输入输出的 Windows 生产构建。

本方案希望达成的效果是：

* Bazel 成为 Wails 应用生产构建的唯一入口，`bazel build //projects/game/windows_agent:windows_agent_win_zip` 能产出 Windows portable zip。
* `tools/wails` 表达 Wails 应用的构建语义，而不是封装 `wails build`。
* Wails runtime/library 使用 `go.mod` 管理的外部 Go module；不在 `MODULE.bazel` 绕过 `go.mod` 引入 Go 依赖。
* 不 patch 外部依赖。只使用外部 Wails repo 中可原样使用的 runtime/library targets；CLI、构建、资源、assets 交接等能力由本仓库本地 Bazel rules/helpers 实现。
* 所有生产产物都是 Bazel declared outputs，不写源码目录，不依赖开发者本机 `PATH` 中的工具。
* frontend dist、Go embed、Windows exe、Windows resource、portable zip 形成可查询、可验证的 Bazel 依赖闭环。

## 前置条件

实施本方案前，必须先清理当前仓库内所有已失效、缺失或会导致 Bazel analysis 失败的构建声明。

必须移除或重建：

* 不存在 package 的 `load(...)`、`register_toolchains(...)`、target 依赖。
* 已删除实现留下的 `//tools/wails:*`、`//tools/cc_windows_launcher_toolchain:*` 等坏引用。
* 直接依赖旧手写 Windows `go_binary` 的生产 package 路径。
* 指向本地 Wails 源码、外部 Wails repo、Wails CLI 的混杂依赖图。
* 会迫使 Bazel patch 外部 Go module 的 BUILD metadata 或源码的设计。

本方案不以现有实现作为参考或兼容对象。现有实现只能作为失败背景；终态实现应从空的 `tools/wails` 重新建设。

## 范围

本方案覆盖：

* `tools/wails` 的终态 API、provider、rule/macro 分层。
* Wails runtime/library 依赖来源。
* 不依赖 Wails CLI 的 Bazel-native Wails 构建模型。
* frontend assets 到 Go `//go:embed` 的权威交接模型。
* Windows Wails binary 的 `rules_go` 构建语义。
* Windows resource `.syso` declared output 模型。
* bindings 生成的可选 declared-output 模型。
* `projects/game/windows_agent` 的目标 BUILD 形态和验收标准。

本方案不包括：

* macOS `.app`、codesign、notarization。
* Linux desktop package、GTK/WebKit 运行环境管理。
* NSIS installer、自动更新、WebView2 fixed runtime 分发包。
* `wails dev` 热重载体验。
* Windows agent 业务功能、UI/UX、协议或采集输入逻辑调整。

## 调研结论

### 外部 Wails runtime 可作为终态依赖来源

仓库通过：

```starlark
go_deps.from_file(go_mod = "//:go.mod")
```

从根 `go.mod` 暴露 `com_github_wailsapp_wails_v2`。已确认关键 runtime/library labels 存在：

```text
@com_github_wailsapp_wails_v2//:wails
@com_github_wailsapp_wails_v2//pkg/options:options
@com_github_wailsapp_wails_v2//pkg/options/assetserver:assetserver
@com_github_wailsapp_wails_v2//pkg/runtime:runtime
```

这些 targets 对应 Windows agent 当前需要的 Wails runtime imports。方案终态应使用这些外部 labels，并移除本地 Wails runtime label 依赖，避免外部/本地双源。

实施时仍需在清理坏 Bazel 注册后执行硬验证：

```bash
bazel build \
  @com_github_wailsapp_wails_v2//:wails \
  @com_github_wailsapp_wails_v2//pkg/options:options \
  @com_github_wailsapp_wails_v2//pkg/options/assetserver:assetserver \
  @com_github_wailsapp_wails_v2//pkg/runtime:runtime
```

如果这些 runtime targets 原样不可构建，不能 patch 外部 repo；必须在方案调研阶段整体改为本地 Wails 源码管理，而不是在开发中 fallback。

### 外部 Wails CLI 不作为终态依赖

外部 target `@com_github_wailsapp_wails_v2//cmd/wails:wails` 存在，但 generated BUILD 对 CLI/template 相关 `go:embed` 资源不完整，直接把它作为工具链会把方案推向 patch 外部 repo。

因此终态设计明确：

* `tools/wails` 不以 Wails CLI 为中心。
* 生产构建不调用 `wails build`。
* 不依赖外部 Wails CLI target 完成 app 构建、bindings、resource 或 frontend 交接。
* 如未来必须提供完整官方 Wails CLI，应在调研阶段选择本地化 Wails 源码并维护完整 BUILD metadata；不得 patch external repo。

## 总体方案

采用 “外部 Wails runtime + 本地 Bazel Wails 小工具” 模型：

```text
frontend build target
  -> wails_asset_library
  -> app go_library
  -> wails_go_binary
  -> wails_app
  -> project package rule
```

生产路径只由 Bazel rules/macros/helpers 组成：

```text
允许：Bazel action -> local helper -> declared output
允许：Bazel analysis -> rules_go go_binary -> Windows exe
禁止：Bazel action -> wails build
禁止：Bazel action -> developer PATH wails/go/npm/pnpm
禁止：patch external Wails/rules_go/Bazel repository
```

`tools/wails` 是仓库本地小工具，不是 Wails CLI wrapper。它负责把 Wails 应用生产构建中需要的稳定语义转换为 Bazel target graph。

## 模型设计

### 对外 API

`tools/wails/defs.bzl` 对外暴露：

```starlark
wails_asset_library
wails_go_binary
wails_app
wails_config_check
wails_windows_resources
wails_bindings
```

其中：

* `wails_asset_library`：frontend dist 到 Go embed assets package 的权威入口。
* `wails_go_binary`：封装 Windows Wails production binary 的 `rules_go` 参数。
* `wails_app`：聚合最终 exe，返回 `DefaultInfo` 与 `WailsAppInfo`。
* `wails_config_check`：校验 `wails.json` 不重新引入非 Bazel 生产命令。
* `wails_windows_resources`：生成 optional `.syso` declared output。
* `wails_bindings`：生成 optional bindings declared directory。

### Provider 模型

```starlark
WailsAssetsInfo = provider(
    fields = {
        "library": "Go assets library target consumed by app go_library",
        "assets_dir": "Declared frontend_dist tree artifact",
        "importpath": "Go importpath for the generated or declared assets package",
    },
)

WailsAppInfo = provider(
    fields = {
        "binary": "Produced Windows exe File",
        "assets": "WailsAssetsInfo or equivalent assets provider",
        "bindings": "Declared bindings directory, if generated",
        "resources": "Declared .syso resource file, if generated",
        "platform": "Target platform string, e.g. windows/amd64",
    },
)
```

`wails_app` 的 `DefaultInfo.files` 必须默认导出最终 `.exe`，让下游 package rule 可以直接使用 `binary = ":windows_agent_app"`。

### `wails_asset_library`

用途：统一管理 frontend dist 到 Go `//go:embed` 的交接，避免项目侧生成旁路 dist。

示例：

```starlark
wails_asset_library(
    name = "windows_agent_assets",
    src = "//projects/game/windows_agent/frontend:dist",
    importpath = "dominion/projects/game/windows_agent/assets",
    package_name = "assets",
    variable_name = "FrontendDist",
    out = "frontend_dist",
    visibility = ["//visibility:public"],
)
```

展开后的逻辑结果：

```text
frontend:dist
  -> declared tree frontend_dist/**
  -> generated assets.go with //go:embed all:frontend_dist
  -> go_library(name = "windows_agent_assets", embedsrcs = [frontend_dist])
```

要求：

* `src` 必须是 Bazel frontend build target 的输出，不能是源码树 `frontend/dist`。
* 输出目录名稳定，默认 `frontend_dist`。
* helper 只写 declared directory 与 declared generated Go file。
* `//go:embed` 文件必须位于同一个 Go package 的逻辑目录下。
* `WailsAssetsInfo.library` 必须被 app 的 `go_library` 依赖消费，不能只作为 provider 旁路存在。

### `wails_go_binary`

用途：封装 Windows Wails production binary 的 `rules_go` 参数。它应是 macro，内部调用 `go_binary(...)`，不得在 rule implementation 中动态实例化 `go_binary`。

示例：

```starlark
wails_go_binary(
    name = "windows_agent_app_binary",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    embed = ["//projects/game/windows_agent/cmd/windows_agent:windows_agent_lib"],
    resources = ":windows_resources",  # optional
    webview2 = "embed",
)
```

Windows amd64 终态语义：

```starlark
go_binary(
    name = "windows_agent_app_binary",
    embed = [app_go_library],
    srcs = [resources_syso],  # optional
    goos = "windows",
    goarch = "amd64",
    pure = "on",
    gotags = [
        "desktop",
        "production",
        "wv2runtime.embed",
    ],
    gc_linkopts = [
        "-w",
        "-s",
        "-H",
        "windowsgui",
    ],
    out = "windows-agent.exe",
)
```

约束：

* 当前只支持 `platform = "windows/amd64"`，其他平台显式 `fail(...)`。
* `webview2` 可取 `download`、`embed`、`browser`、`error`，映射为 `wv2runtime.<value>` tag。
* 默认不添加 `native_webview2loader`。如未来确实需要，必须作为显式属性并用 Windows smoke test 验证。
* `pure = "on"` 只表示 Wails Windows runtime 不走 CGo；它不应被描述为 Bazel Windows launcher/toolchain 问题的通用解法。

### `wails_app`

用途：项目侧首选入口，聚合 config check、assets、resources、binary，并导出最终 exe。

示例：

```starlark
wails_app(
    name = "windows_agent_app",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    wails_json = "wails.json",
    go_library = "//projects/game/windows_agent/cmd/windows_agent:windows_agent_lib",
    assets = "//projects/game/windows_agent:windows_agent_assets",
    resources = ":windows_resources",  # optional
    bindings = ":bindings",            # optional
    webview2 = "embed",
    visibility = ["//visibility:public"],
)
```

要求：

* `wails_app` 必须导出真实 Windows exe。
* `WailsAppInfo.binary` 与 `DefaultInfo.files` 中的 exe 必须一致。
* `assets` 必须进入 `go_library` 依赖闭环；不能只在 `wails_app` 里声明但不被最终 binary embed。
* `wails_app` 不调用 `wails build`，不依赖 Wails CLI。

### `wails_config_check`

用途：校验 `wails.json` 只保留项目语义，不重新引入 Bazel 外生产构建命令。

检查项：

* `frontend:install`、`frontend:build`、hooks 不能配置为 CI/生产会执行的非 Bazel 命令。
* `outputfilename`、`frontend:dir`、`build:dir` 等字段如果与 Bazel rule 属性冲突，应 fail。
* `wails.json` 可以保留用于 IDE、Wails 项目识别和开发者认知，但不是生产构建入口。

### `wails_windows_resources`

用途：生成 Windows `.syso` declared output。

示例：

```starlark
wails_windows_resources(
    name = "windows_resources",
    icon = "resources/icon.ico",
    manifest = "build/windows/wails.exe.manifest",
    info = "build/windows/info.json",
    arch = "amd64",
    out = "windows-agent-res.syso",
)
```

要求：

* `.syso` 是 declared file。
* 不写源码目录。
* icon、manifest、info 都是 declared inputs。
* `.syso` 通过 `wails_go_binary(srcs = [...])` 进入最终 exe。
* 项目可不开启 resources；不开启时 exe 仍可构建，只是没有内嵌 icon/manifest/version metadata。

### `wails_bindings`

用途：可选生成 frontend bindings 到 declared directory。

终态要求：

* 不写 `frontend/wailsjs` 源码目录。
* 输出到 `ctx.actions.declare_directory(...)`。
* 不依赖 `wails build`。
* 不强制依赖外部 Wails CLI。
* 如复用 Wails library 中可构建的 bindings 包，应通过本地 helper 控制输入、工作目录、环境变量和输出目录。
* 如实现成本高，项目可关闭 bindings；工具 API 仍保留该能力边界。

## 代码分层

建议目录：

```text
tools/wails/
  BUILD.bazel
  defs.bzl
  private/
    providers.bzl
    assets.bzl
    app.bzl
    go_binary.bzl
    config_check.bzl
    windows_resources.bzl
    bindings.bzl
  helpers/
    BUILD.bazel
    stage_frontend.go
    generate_assets_go.go
    inspect_config.go
    generate_winres.go
    generate_bindings.go
```

职责：

* `defs.bzl`：只导出稳定 API。
* `private/providers.bzl`：定义 `WailsAssetsInfo`、`WailsAppInfo`。
* `private/assets.bzl`：实现 `wails_asset_library`。
* `private/go_binary.bzl`：实现 `wails_go_binary` macro。
* `private/app.bzl`：实现 `wails_app` 聚合 provider rule/macro。
* `private/config_check.bzl`：实现 `wails_config_check`。
* `private/windows_resources.bzl`：实现 `.syso` resource 规则。
* `private/bindings.bzl`：实现 bindings declared output 规则。
* `helpers/*`：小型 Go helper，负责稳定文件 staging、config 检查、resource 生成和 bindings 生成；helper 自身用 `go_unittest` 覆盖。

## `projects/game/windows_agent` 目标形态

顶层 BUILD 目标形态：

```starlark
load("//projects/game/windows_agent/release:defs.bzl", "windows_agent_package")
load("//tools/wails:defs.bzl", "wails_app", "wails_asset_library", "wails_config_check")

wails_config_check(
    name = "wails_config_check",
    wails_json = "wails.json",
)

wails_asset_library(
    name = "windows_agent_assets",
    src = "//projects/game/windows_agent/frontend:dist",
    importpath = "dominion/projects/game/windows_agent/assets",
    package_name = "assets",
    variable_name = "FrontendDist",
    out = "frontend_dist",
)

wails_app(
    name = "windows_agent_app",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    wails_json = "wails.json",
    go_library = "//projects/game/windows_agent/cmd/windows_agent:windows_agent_lib",
    assets = ":windows_agent_assets",
    webview2 = "embed",
    visibility = ["//visibility:public"],
)

windows_agent_package(
    name = "windows_agent_win_zip",
    binary = ":windows_agent_app",
    ffmpeg = "@ffmpeg_windows_amd64//:bin/ffmpeg.exe",
    input_helper = "//projects/game/windows_agent/helper/input:input_helper",
    icon = "resources/icon.ico",
    visibility = ["//visibility:public"],
)
```

`cmd/windows_agent` 的 app library 依赖应使用外部 Wails runtime labels：

```starlark
go_library(
    name = "windows_agent_lib",
    srcs = ["main.go"],
    importpath = "dominion/projects/game/windows_agent/cmd/windows_agent",
    deps = [
        "//projects/game/windows_agent:windows_agent_assets",
        "//projects/game/windows_agent/internal/app",
        "@com_github_wailsapp_wails_v2//:wails",
        "@com_github_wailsapp_wails_v2//pkg/options:options",
        "@com_github_wailsapp_wails_v2//pkg/options/assetserver:assetserver",
    ],
)
```

`wails_app(assets = ...)` 负责校验和暴露 app 级 provider；真正的 Go import 闭环仍必须通过 app `go_library.deps` 显式依赖 `wails_asset_library` 生成的 assets library。

生产 zip 不得依赖 legacy `cmd/windows_agent:windows_agent_windows`。该 target 应删除，或保留为 manual/debug 且不被 release/package/CI 依赖。

## Windows / Bazel 9.1.0 baseline

仓库使用 Bazel 9.1.0。该版本包含 Windows launcher cross-build 相关修复，因此本方案不默认引入 Windows target C++ cross toolchain。

终态仍要求验证：

```bash
bazel build //tools/wails/testdata/minimal_windows_go_binary:minimal_windows_go_binary
bazel build //projects/game/windows_agent:windows_agent_app
```

如果仍失败于 `@@bazel_tools//src/tools/launcher:launcher` 或 `@@bazel_tools//tools/cpp:toolchain_type`，应作为 Bazel launcher/toolchain baseline 问题单独处理。可选解法是 hermetic LLVM-mingw、MinGW 或 Zig C++ toolchain，但只有实际验证仍失败时才引入。

真实 Windows CGo/C/C++ cross toolchain 只在项目代码或依赖确实包含 `import "C"`、C/C++ 源码或 C deps 时才进入方案。

## 关键细节

### 为什么不封装 Wails CLI

本方案目标是 Bazel 管理 Wails 构建，而不是封装 Wails CLI。`wails build` 是面向开发者工作区的黑盒编排命令，会写源码目录、执行 frontend 命令、生成临时资源并调用 Go build。这与 Bazel declared outputs、remote cache 和 sandbox 模型冲突。

Wails CLI 可以作为开发者本地工具存在，但不是生产构建依赖。

### 为什么 runtime 用外部依赖，CLI 不用

外部 Wails runtime targets 可由 `go.mod` + `go_deps` 原样提供，符合依赖管理约束。外部 CLI target 存在，但 generated BUILD 对模板 embed 资源不完整；修复它需要 patch 外部依赖，违反约束。

因此终态只使用外部 Wails runtime/library，CLI/构建能力由本地 `tools/wails` 实现。

### 为什么 `wails_asset_library` 管理 frontend embed

Go `//go:embed` 只能嵌入声明该 directive 的 Go package 目录下的文件。让 `wails_app` 单独接收 `frontend = "//...:dist"` 很容易生成一份没有被最终 binary 消费的旁路输出。

`wails_asset_library` 把 frontend dist、`frontend_dist/**`、generated `assets.go` 和 `go_library.embedsrcs` 绑定在一起，能从 Bazel query 上证明 frontend 进入真实 binary。

## 决策详情

### 决策 1：清理坏实现后从 0 开始

原因：当前缺失 package、坏 toolchain 注册、旧 Wails labels 和半迁移 target 会在 analysis 阶段阻塞任何有效验证。继续兼容这些实现会把方案带回补丁式修复。

### 决策 2：生产构建不依赖 Wails CLI

原因：目标是 Bazel 管理 Wails 构建语义。CLI 是开发工具，不是 Bazel production action driver。

### 决策 3：Wails runtime 使用外部 Go module

原因：runtime labels 可由 `go.mod` + `go_deps` 原样提供，不需要 patch；这符合 Go 依赖版本真相在 `go.mod` 的仓库约束。

### 决策 4：不 patch 外部依赖

原因：patch external repo 会让依赖行为脱离 `go.mod` 和 upstream 版本真相。如果确实需要修改 upstream 行为，应在调研阶段选择本地源码管理方案，而不是开发中 fallback。

### 决策 5：`wails_app` 必须导出最终 exe

原因：下游 package/release rule 应依赖 Wails app 抽象，而不是绕过它依赖手写 Windows binary。

### 决策 6：resources 与 bindings 是工具能力，但项目可选

原因：`.syso` 和 bindings 对发布体验、前端类型体验有价值，但不应阻塞基础 app 构建。工具提供 declared-output 能力；项目是否启用由输入是否完整决定。

## 验收标准

### 清理验收

* 仓库不存在指向缺失 Bazel package 的 `load(...)`、`register_toolchains(...)` 或 target deps。
* `bazel query //projects/game/windows_agent:windows_agent_win_zip` 能完成 analysis。
* 生产构建图中不存在对 `wails build`、PATH `wails`、外部 Wails CLI target 的依赖。

### 外部 Wails runtime 验收

* 以下 target 原样 build 成功：

```bash
bazel build \
  @com_github_wailsapp_wails_v2//:wails \
  @com_github_wailsapp_wails_v2//pkg/options:options \
  @com_github_wailsapp_wails_v2//pkg/options/assetserver:assetserver \
  @com_github_wailsapp_wails_v2//pkg/runtime:runtime
```

* 仓库内 Wails runtime BUILD deps 不再指向 `//third_party/github.com/wailsapp/wails/v2...`。
* `MODULE.bazel` 不新增绕过 `go.mod` 的 Wails Go 依赖。
* 外部 Wails repo 没有 patch。

### `tools/wails` 验收

* `bazel test //tools/wails/...` 成功。
* `wails_asset_library` fixture 能证明 frontend dist 进入 generated Go assets library。
* `wails_go_binary` fixture 能产出 `.exe`，且文件名为 `<binary_name>.exe`。
* `wails_app` fixture 的 `DefaultInfo.files` 与 `WailsAppInfo.binary` 指向同一个 exe。
* `wails_config_check` 能拒绝非 Bazel 生产 frontend/hook 命令。

### Windows agent 验收

* `bazel test //projects/game/windows_agent/...` 成功。
* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功。
* `bazel query 'somepath(//projects/game/windows_agent:windows_agent_win_zip, //projects/game/windows_agent:windows_agent_app)'` 有路径。
* `bazel query` 能证明 frontend dist 通过 `wails_asset_library`、app `go_library` 进入 `windows_agent_app`。
* zip 内容稳定，包含：

```text
windows-agent.exe
resources/bin/ffmpeg.exe
resources/bin/ffmpeg.exe.sha256
resources/bin/input-helper.exe
resources/icon.ico
```

* 前端资源嵌入 `windows-agent.exe`，不作为独立 `frontend/dist` 目录进入 zip。

## 风险与规避

### 风险 1：外部 Wails runtime 实际 build 失败

规避：清理坏 Bazel 注册后第一时间执行外部 runtime targets build 验收。若失败且需要修改 upstream，方案应整体切换为本地 Wails 源码管理，不允许 patch external repo 或开发中 fallback。

### 风险 2：误把 Wails CLI 重新引入生产路径

规避：`tools/wails` 不提供生产 `wails_build` wrapper；query 验收生产图不依赖外部 CLI target 或 compat target。

### 风险 3：frontend assets 旁路

规避：`wails_asset_library` 是唯一 frontend embed 入口；验收必须用 query 证明 frontend dist 被 app `go_library` 消费。

### 风险 4：Bazel 9.1.0 Windows launcher 问题仍存在

规避：先用最小 Windows `go_binary` fixture 验证。若仍失败，再单独设计 hermetic Windows target C++ launcher toolchain。

### 风险 5：bindings 生成复杂度高

规避：bindings 是可选 declared-output 能力，不阻塞 `windows_agent_win_zip`。实现时优先本地 helper 和可声明 inputs，禁止写源码目录。

## 未来规划

* 支持 `windows/arm64`。
* 支持 macOS `.app`、codesign、notarization。
* 支持 Linux desktop package。
* 提供 Bazel-managed Wails dev workflow，但仍不把 `wails build` 作为生产入口。
* 为 Wails app 提供大型测试模板，覆盖启动、资源加载和基础 IPC/bindings 调用。
