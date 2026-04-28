# 通用 Wails 项目 Bazel 构建方案

## 目标

本方案用于将 `tools/wails` 从“服务单个 Windows agent 的 Wails 辅助工具链”升级为仓库内通用的 Wails-on-Bazel 构建框架，使仓库内 Wails 项目可以通过 Bazel 完成可复现、可缓存、可声明输入输出的生产构建。

本方案希望达成的效果是：

* 提供通用 `wails_app` 构建入口，项目侧只声明 Wails app 的业务输入，不手写 Wails 生产构建细节。
* 生产构建由 Bazel 拆阶段完成：前端构建、资源交接、bindings、Windows resource、Go binary、项目打包均是 declared outputs。
* Wails CLI 由 Bazel 固定版本管理，但不把 `wails build` 作为生产构建入口。
* 优先使用正常 Go module 外部依赖 `github.com/wailsapp/wails/v2`，如外部依赖可满足 Bazel 构建，则移除当前本地化的 Wails 源码仓库。
* `projects/game/windows_agent` 迁移为该通用规则的首个落地项目，`bazel build //projects/game/windows_agent:windows_agent_win_zip` 仍产出 Windows portable zip。
* 当前只要求支持 Windows 目标平台；模型保留平台扩展点，但不实现 Linux/macOS app 构建支持。

本方案替代并收敛 `design/tools_wails_bazel_toolchain.md` 中关于 `tools/wails` 的后续建设目标；`design/windows_agent_frontend_bazel_wails_build.md` 中关于 Windows agent 前端 Bazel 化和 Go embed 交接的内容仍作为项目侧前端资源交接参考。

## 范围

本方案覆盖：

* 通用 `tools/wails` Bazel API、provider、rule/macro 分层。
* Wails CLI 在 Bazel 中的定位与使用边界。
* Wails Go runtime 依赖优先回归外部 Go module 的迁移策略。
* 移除本地化 Wails 源码前需要处理的 Gazelle import 重定向问题。
* Windows-only 生产构建的 tags、linkopts、resource、frontend embed 模型。
* `projects/game/windows_agent` 需要如何调整以适配通用构建方案。
* 验收标准与迁移步骤。

本方案不包括：

* macOS `.app`、codesign、notarization、universal binary。
* Linux desktop package、GTK/WebKit 运行环境管理。
* NSIS installer、自动更新、WebView2 fixed runtime 分发包。
* 完整替代 `wails dev` 的热重载体验。
* Windows agent 业务功能、UI/UX、协议或采集输入逻辑调整。

## 当前问题

当前仓库中 Wails 构建链路存在以下问题：

1. **`wails_app` 不是最终构建入口**：`projects/game/windows_agent:windows_agent_app` 只聚合部分 Wails 阶段输出，`WailsAppInfo.binary` 为空；最终 zip 仍直接依赖 `cmd/windows_agent:windows_agent_windows`。
2. **Wails 构建语义分散在项目侧**：Windows `go_binary` 的 `goos/goarch`、Wails production tags、linkopts、resource `.syso` 接入没有由 `tools/wails` 统一封装。
3. **本地化 Wails 源码维护成本高**：当前 `//third_party/github.com/wailsapp/wails/v2` 同时承担 Wails CLI 来源和 runtime 依赖来源。若不需要 patch Wails 源码或从源码构建 CLI，则本地化会增加 BUILD 文件维护和 Gazelle 重定向复杂度。
4. **Wails CLI 职责容易混淆**：CLI 对 bindings、diagnostics 有价值，但 `wails build` 是面向工作区的黑盒编排命令，不适合作为 Bazel 生产 action。
5. **Windows 目标平台与 CGo 认知混乱**：Wails v2 Windows runtime 本身不需要 CGo；当前目标平台仅 Windows，因此不应为了 Linux/macOS Wails CGo 场景引入 Windows C++ cross toolchain 作为默认要求。

## 总体方案

采用 “Bazel 拆阶段 Wails 构建框架” 模型：

```text
Wails project BUILD
  -> wails_config_check
  -> frontend build target          # Bazel JS rules 或项目自定义 frontend target
  -> wails_frontend_assets          # re-root / stage to Go embed path
  -> wails_bindings                 # optional, Bazel-managed Wails CLI
  -> wails_windows_resources        # optional .syso declared output
  -> wails_go_binary                # rules_go with Wails production settings
  -> wails_app                      # DefaultInfo + WailsAppInfo(binary, ...)
  -> project package/release rule   # 例如 windows_agent_package
```

生产路径由 Bazel rule/macro 组成，不调用 `wails build`：

```text
允许：Bazel action -> wails generate module / helper / rules_go go_binary
禁止：Bazel action -> wails build -> frontend/dist / frontend/wailsjs / build/bin
```

Wails CLI 作为 Bazel-managed tool，服务于：

* `wails_version_test`。
* `wails_doctor`，仅 manual/local。
* `wails_bindings`，在受控 sandbox 中输出 declared directory。
* 兼容性对照 target，必须 manual/local/no-remote-cache，且不能被生产 target 依赖。

最终 Windows exe 由 `rules_go` 生成，而不是由 `wails build` 生成。

## 模型设计

### 对外 API

`tools/wails/defs.bzl` 对外暴露：

```starlark
wails_app
wails_config_check
wails_frontend_assets
wails_bindings
wails_windows_resources
wails_version_test
wails_doctor
```

首选项目侧入口是 `wails_app`：

```starlark
wails_app(
    name = "my_app",
    binary_name = "my-app",
    platform = "windows/amd64",
    wails_json = "wails.json",

    go_library = "//path/to/cmd/app:app_lib",
    frontend = "//path/to/frontend:dist",

    bindings = False,
    bindings_srcs = [],

    icon = "resources/icon.ico",
    manifest = "build/windows/wails.exe.manifest",
    info = "build/windows/info.json",
    webview2 = "embed",

    visibility = ["//visibility:public"],
)
```

### Provider 模型

```starlark
WailsToolchainInfo = provider(
    fields = {
        "wails": "Wails CLI executable File",
        "version": "Pinned Wails CLI version string",
        "runfiles": "Runfiles required by Wails CLI",
    },
)

WailsAppInfo = provider(
    fields = {
        "binary": "Produced Wails application binary File",
        "frontend_assets": "Declared frontend assets directory used by embed",
        "bindings": "Declared Wails bindings directory, if generated",
        "resources": "Declared platform resource output, if generated",
        "platform": "Target platform string, e.g. windows/amd64",
    },
)
```

`wails_app` 的 `DefaultInfo.files` 默认导出最终 binary。这样下游 package rule 可以直接把 `:my_app` 当作 binary 输入。

### Windows 平台构建语义

当前支持的生产平台：`windows/amd64`。

`wails_app(platform = "windows/amd64")` 内部生成或包装：

```starlark
go_binary(
    name = "my_app_windows_binary",
    embed = [go_library],
    srcs = [resources_syso],  # optional
    goos = "windows",
    goarch = "amd64",
    pure = "on",
    gotags = [
        "desktop",
        "production",
        "wv2runtime.embed",
        "native_webview2loader",
    ],
    gc_linkopts = [
        "-w",
        "-s",
        "-H",
        "windowsgui",
    ],
    out = "my-app.exe",
)
```

说明：

* `pure = "on"` 是 Windows-only 当前目标下的默认选择。Wails v2 Windows 端不需要 CGo，避免引入 Windows C++ cross toolchain。
* `webview2` 暴露为属性，可取 `download`、`embed`、`browser`、`error`，映射为 `wv2runtime.<value>` build tag。
* `native_webview2loader` 作为当前 Wails v2 Windows 默认 loader tag 保留。
* 如果未来某项目自身引入 `import "C"`，该项目不能使用默认 pure Windows 路径，应单独设计 CGo Windows toolchain 支持。

### Frontend assets 模型

通用方案不规定具体前端框架，只要求 `frontend` 属性指向一个 Bazel target，该 target 输出前端 dist tree artifact 或文件集合。

`wails_frontend_assets` 负责把该输出重根到 Go embed 可见路径，例如：

```text
frontend_dist/index.html
frontend_dist/assets/index-<contenthash>.js
frontend_dist/assets/index-<contenthash>.css
```

Go 代码可以继续采用独立 assets 包：

```go
//go:embed all:frontend_dist
var FrontendDist embed.FS
```

对于已有项目，`wails_app` 不强制移动 Go 文件目录；它应能接入项目已有 assets 包或由项目显式提供 asset library。

### Bindings 模型

`wails_bindings` 是可选阶段。通用接口建议：

```starlark
wails_bindings(
    name = "bindings",
    app_package = "//path/to/cmd/app:app_lib",
    wails_json = "wails.json",
    out = "wailsjs",
    tags = [],
)
```

要求：

* 使用 Wails CLI toolchain 中固定版本 `wails`。
* 输出到 declared directory，不写 `frontend/wailsjs`。
* bindings 生成使用 exec platform，不继承 Windows target platform。
* 如 Wails CLI 对 `go.mod`/工作区布局有假设，应由 helper 在 sandbox 内 materialize 最小 Go workspace，再运行 `wails generate module`。

Windows agent 当前已有手写 TypeScript declarations，可先以 `bindings = False` 迁移主构建链路，再单独补 bindings 生成。

### Windows resources 模型

`wails_windows_resources` 负责生成 `.syso`：

```starlark
wails_windows_resources(
    name = "windows_resources",
    icon = "resources/icon.ico",
    manifest = "build/windows/wails.exe.manifest",
    info = "build/windows/info.json",
    arch = "amd64",
    out = "my-app-res.syso",
)
```

要求：

* `.syso` 是 Bazel declared file。
* 不写源码目录。
* 作为 `go_binary.srcs` 输入接入 rules_go。
* 没有真实 icon 时，项目可暂时不启用 resources 阶段，但 release zip 可继续包含外部 icon 文件。

## Wails 依赖来源设计

### 首选：正常外部 Go module 依赖

Wails runtime/library 依赖优先回归正常 Go module：

```go
require github.com/wailsapp/wails/v2 v2.12.0
```

Bazel 通过：

```starlark
go_deps.from_file(go_mod = "//:go.mod")
```

生成或使用 `@com_github_wailsapp_wails_v2` 外部 repository target。

优点：

* 不维护本地化 Wails 源码和大量 BUILD 文件。
* Wails runtime 版本由 `go.mod` 锁定，符合 Go 生态常规。
* 降低 `third_party/github.com/wailsapp/wails/v2` 与 upstream 漂移风险。

### Wails CLI 来源

Wails CLI 不要求来自本地化源码。可选来源：

1. **外部 Go module 构建 CLI**：从 `@com_github_wailsapp_wails_v2//cmd/wails` 构建。若 Gazelle 生成 target 可用，这是最一致的来源。
2. **固定预构建 CLI 二进制**：通过 repository rule 下载官方或内部镜像中的 Wails CLI，固定 version/sha256。适合外部 Go module CLI target 构建困难时。

禁止使用开发者本机 `PATH` 中的 `wails` 或 `go install ...@latest`。

### 本地化 Wails 源码移除策略

当前仓库存在本地化路径：

```text
third_party/github.com/wailsapp/wails/v2
```

在移除前必须先处理 Bazel import 重定向：

1. 检查本地化 Wails 顶层 `BUILD.bazel` 中的 Gazelle prefix / resolve 指令。
2. 检查仓库内所有 `//third_party/github.com/wailsapp/wails/v2...` label 引用。
3. 将项目 BUILD deps 改回外部 repo label，例如 `@com_github_wailsapp_wails_v2//...`，或使用 Gazelle 重新生成 deps。
4. 确认 `go_deps.use_repo` 中保留 `com_github_wailsapp_wails_v2`。
5. 运行 Gazelle，确保 import `github.com/wailsapp/wails/v2/...` 不再被重定向到本地 `//third_party/...`。
6. 验证外部 Wails runtime 依赖可构建 Windows app。
7. 验证 Wails CLI toolchain 可从外部 repo 或固定二进制获取。
8. 以上全部通过后，删除本地化 Wails 源码目录。

如果外部 repo 构建失败，允许暂时保留本地化 Wails 源码，但必须记录具体阻塞原因，并限制本地 patch 范围。

## 代码分层

建议 `tools/wails` 目录：

```text
tools/wails/
  BUILD.bazel
  defs.bzl
  toolchain.bzl
  repositories.bzl            # 可选：固定预构建 Wails CLI
  private/
    providers.bzl
    config_check.bzl
    frontend_assets.bzl
    bindings.bzl
    windows_resources.bzl
    go_binary.bzl             # 新增：Wails Go binary 构建语义
    app.bzl                   # wails_app 聚合入口
    diagnostics.bzl
  helpers/
    BUILD.bazel
    inspect_config/
    generate_winres.go
    stage_frontend.go         # 可选：替代 shell cp，提高跨平台稳定性
    materialize_bindings_workspace.go
```

职责划分：

* `defs.bzl`：只导出稳定 API。
* `toolchain.bzl`：Wails CLI toolchain provider 与注册。
* `private/go_binary.bzl`：封装 Wails Windows binary 的 `go_binary` 参数。
* `private/app.bzl`：组合 config/frontend/bindings/resources/binary，返回 `WailsAppInfo`。
* `helpers/*`：需要复杂文件 staging、资源生成、workspace materialization 时使用 Go helper，helper 自身用 `go_unittest` 覆盖。

## `projects/game/windows_agent` 调整方案

### 目标形态

`projects/game/windows_agent` 应成为通用 `wails_app` 的使用方，而不是手写 Wails 构建细节。

顶层 BUILD 目标形态：

```starlark
wails_config_check(
    name = "wails_config_check",
    binary_name = "windows-agent",
    wails_json = "wails.json",
)

wails_app(
    name = "windows_agent_app",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    wails_json = "wails.json",
    go_library = "//projects/game/windows_agent/cmd/windows_agent:windows_agent_lib",
    frontend = "//projects/game/windows_agent/frontend:dist",
    bindings = False,
    icon = None,  # resources/icon.ico 仍为占位符时不启用 .syso
    webview2 = "embed",
    visibility = ["//visibility:public"],
)

windows_agent_package(
    name = "windows_agent_win_zip",
    binary = ":windows_agent_app",
    ffmpeg = "@ffmpeg_windows_amd64//:bin/ffmpeg.exe",
    icon = "resources/icon.ico",
    input_helper = "//projects/game/windows_agent/helper/input:input_helper",
    visibility = ["//visibility:public"],
)
```

### `cmd/windows_agent`

保留：

```starlark
go_library(
    name = "windows_agent_lib",
    srcs = ["main.go"],
    ...
)
```

调整：

* `windows_agent_windows` 不再作为 release zip 的权威入口。
* 可删除该 target，或保留为 manual/debug 兼容 target，但 Wails production tags/linkopts 应由 `tools/wails` 统一管理。

### `assets`

短期保持当前独立 assets 包：

```text
frontend:dist -> assets:frontend_dist -> assets:assets -> windows_agent_lib
```

`wails_app` 需要支持这种已有 assets 包模式，不强制改业务代码。

中长期可考虑让 `wails_app` 管理 asset handoff target，但不能破坏 Go `//go:embed` 必须位于 package 子目录的约束。

### `wails.json`

保持：

```json
"frontend:install": "",
"frontend:build": ""
```

`wails_config_check` 负责防止生产构建重新引入非 Bazel frontend 命令或 hooks。

## 关键细节

### 为什么生产构建不调用 `wails build`

`wails build` 会执行面向工作区的编排：生成 bindings 到源码树、执行 frontend 命令、生成临时 `.syso`、调用 `go build` 输出 `build/bin`。这些行为对 Bazel 来说不是稳定 declared outputs，会降低 remote cache 与 sandbox 可控性。

因此生产构建必须拆成 Bazel 阶段；CLI 只作为工具用于受控子命令。

### 为什么优先移除本地化 Wails 源码

本方案不要求 patch Wails runtime 源码，也不要求从本地源码构建 CLI。正常 Go module 外部依赖更符合 Go/Bazel 依赖管理模型。只有在外部 repo 的 BUILD 生成或 CLI 构建无法满足需求时，才保留本地化源码。

### Windows C++ toolchain 处理

当前 Wails 目标平台仅 Windows，且 Wails v2 Windows runtime 不需要 CGo。默认 Windows binary 应使用 `pure = "on"`。

如果遇到 C++ toolchain 相关错误，需要先区分：

1. **Host C++ toolchain 缺失**：例如 Bazel/JS launcher 需要本机 Linux C++ toolchain，应通过 `rules_cc` 本机 auto configure 解决。
2. **Windows target C++ toolchain 缺失**：只有项目自身或依赖确实需要 Windows CGo/C/C++ 时才需要 MinGW/Zig CC 等 cross toolchain。本方案默认不引入。

### 平台扩展空间

`wails_app.platform` 保留字符串接口：

```text
windows/amd64
windows/arm64   # future
darwin/amd64    # future, not implemented
darwin/arm64    # future, not implemented
linux/amd64     # future, not implemented
```

当前实现只接受 `windows/amd64`。其他平台应显式 fail，并提示尚未支持，避免误以为存在生产支持。

## 决策详情

### 决策 1：`wails_app` 是生产入口，不是旁路聚合 target

原因：项目侧如果仍直接依赖手写 `go_binary`，Wails 构建语义会继续分散。`wails_app` 必须导出最终 binary，才能成为通用构建方案。

### 决策 2：Wails CLI 不作为最终 build driver

原因：`wails build` 的工作区写入和命令执行不适合 Bazel。CLI 保留为 toolchain tool，用于 version、doctor、bindings 和兼容对照。

### 决策 3：优先使用外部 Go module Wails 依赖

原因：降低本地化源码维护成本，避免 patch upstream BUILD 文件成为长期负担。移除本地化源码前必须先处理 Gazelle import 重定向。

### 决策 4：Windows-only 当前目标默认 pure Go

原因：Wails v2 Windows runtime 不需要 CGo。默认引入 Windows C++ cross toolchain会增加复杂度，并偏离当前目标。

### 决策 5：bindings 后置迁移

原因：bindings 生成涉及 Wails CLI 对 Go workspace 的假设，复杂度高。Windows agent 当前已有手写 declarations，可先迁移生产 binary 链路，再补齐 generated bindings。

## 迁移步骤

### Step 1：验证外部 Wails Go module 可用性

1. 恢复或确认 `go.mod` 中 `github.com/wailsapp/wails/v2` 版本。
2. 确认 `go_deps.use_repo` 中包含 `com_github_wailsapp_wails_v2`。
3. 暂时绕过本地 `//third_party/github.com/wailsapp/wails/v2` label，验证外部 repo label 可被 rules_go 构建。
4. 处理 Gazelle import 重定向，确保 `github.com/wailsapp/wails/v2/...` 不再被解析到本地 third_party。

### Step 2：实现 `wails_go_binary`

1. 新增 `tools/wails/private/go_binary.bzl`。
2. 封装 Windows production tags/linkopts/pure/goos/goarch/out。
3. 支持 optional `.syso` 输入。
4. 增加 fixture 测试，验证 `DefaultInfo.files` 包含 `.exe`。

### Step 3：升级 `wails_app`

1. `wails_app` 接收 `go_library`、`binary_name`、`platform`、`frontend`、`icon`、`bindings` 等属性。
2. 内部调用 config/frontend/resources/binary 阶段。
3. `WailsAppInfo.binary` 设置为最终 exe。
4. `DefaultInfo.files` 默认导出最终 exe。

### Step 4：迁移 `projects/game/windows_agent`

1. 顶层 `wails_app` 增加 `go_library`、`binary_name`、`platform`。
2. `windows_agent_win_zip.binary` 改为 `:windows_agent_app`。
3. `cmd/windows_agent:windows_agent_windows` 改为 manual/debug 兼容 target 或删除。
4. 维持 `assets` 包和 frontend Bazel build 不变，降低业务代码改动。

### Step 5：移除本地化 Wails 源码

在以下条件全部满足后执行：

1. 外部 Wails runtime repo 可构建 Windows agent。
2. Wails CLI toolchain 可从外部 repo 或固定二进制提供。
3. 仓库内不再有 `//third_party/github.com/wailsapp/wails/v2...` label 引用。
4. Gazelle 不再把 Wails imports 重定向到本地路径。
5. `bazel test //tools/wails/...` 成功。
6. `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功。

### Step 6：补齐可选能力

1. 真 icon 到位后接入 `wails_windows_resources`。
2. 解决 bindings workspace materialization 后启用 `bindings = True`。
3. 增加 README，说明 `wails build` 不是生产入口。

## 验收标准

### 通用工具验收

* `bazel test //tools/wails/...` 成功。
* `wails_version_test` 使用 Bazel-managed Wails CLI，不依赖本机 PATH。
* `wails_app` fixture 能导出 `WailsAppInfo.binary`。
* 生产构建路径不依赖任何 `wails_build_compat*` target。

### Wails 外部依赖验收

* 仓库内 Wails runtime imports 由外部 `@com_github_wailsapp_wails_v2` 满足。
* 移除本地化 Wails 源码后，Gazelle 不重新生成到 `//third_party/github.com/wailsapp/wails/v2` 的依赖。
* Wails CLI 来源固定 version/sha256 或外部 Go module version。

### Windows agent 验收

* `bazel test //projects/game/windows_agent/...` 成功。
* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功。
* `bazel query 'somepath(//projects/game/windows_agent:windows_agent_win_zip, //projects/game/windows_agent:windows_agent_app)'` 有路径。
* zip 内容仍包含：

```text
windows-agent.exe
resources/bin/ffmpeg.exe
resources/bin/ffmpeg.exe.sha256
resources/bin/input-helper.exe
resources/icon.ico
```

* 前端资源嵌入 `windows-agent.exe`，不以独立 `frontend/dist` 目录进入 zip。

## 风险与规避

### 风险 1：外部 Wails repo 的 Gazelle BUILD 不满足构建

规避：先验证外部 repo；失败时记录具体 target/错误。可短期保留本地化源码，或改用固定 CLI 二进制 + 外部 runtime 依赖的混合方案。

### 风险 2：Gazelle import 重定向残留

规避：移除本地化源码前先清理所有 `//third_party/github.com/wailsapp/wails/v2...` label，并运行 Gazelle 验证不会回写。

### 风险 3：bindings 生成依赖 Go workspace 布局

规避：bindings 后置；先以 `bindings = False` 迁移主构建链路。后续用 helper materialize sandbox workspace。

### 风险 4：Windows resource `.syso` 与 rules_go 接入不稳定

规避：resources 阶段 optional；没有真实 icon 时不阻塞主构建。单独用 fixture 验证 `.syso` 进入 exe。

### 风险 5：误把 host C++ toolchain 与 Windows C++ cross toolchain 混淆

规避：默认 Windows Wails binary 使用 `pure = "on"`。只有项目出现真实 Windows CGo/C/C++ 需求时才引入 cross toolchain。

## 未来规划

* 支持 `windows/arm64`。
* 支持 macOS `.app`、codesign、notarization。
* 支持 Linux desktop package。
* 提供 Bazel-managed Wails dev workflow，替代部分 `wails dev` 体验。
* 为 Wails app 提供大型测试模板，覆盖启动、资源加载、基础 IPC/bindings 调用。
