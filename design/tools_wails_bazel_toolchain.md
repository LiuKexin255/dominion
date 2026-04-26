# tools/wails Bazel toolchain 方案

## 目标

本方案用于在 `tools/wails` 中引入 Wails CLI 的 Bazel-managed toolchain，并为仓库内 Wails 桌面应用提供可复现、可 remote cache 的构建基础。

本方案希望达成的效果是：

* Wails CLI 版本由 Bazel 固定，不依赖开发者本机 `PATH` 中的 `wails`。
* 生产构建禁止调用 `wails build`，避免 Wails CLI 在 Bazel 外部模型中写源码目录、执行任意前端命令或产生不可声明输出。
* Wails 相关能力拆成 Bazel 可声明、可缓存的阶段：bindings 生成、前端资源交接、Windows resource/syso 生成、Go binary 构建参数、诊断工具。
* 所有生产产物都是 Bazel declared outputs，输出路径、文件名、环境变量和输入集合稳定，可被 remote cache 命中。
* `projects/game/windows_agent` 后续可在不改变业务代码的前提下迁移到 `tools/wails` 提供的通用规则。

本方案与 `design/windows_agent_frontend_bazel_wails_build.md` 互补：后者解决 Windows agent 前端 Bazel 化和 Go embed 交接；本方案解决 Wails CLI 和 Wails 平台资源能力如何进入 Bazel toolchain。

## 范围

本方案覆盖：

* `tools/wails` 的目录结构。
* Wails CLI toolchain provider、toolchain type 和注册方式。
* Wails CLI 的构建/获取方式。
* 禁用 `wails build` 的策略。
* bindings、frontend assets、Windows resources、app 聚合 rule 的模型。
* remote cache 稳定性约束。
* `projects/game/windows_agent` 的迁移路径。

本方案不包括：

* 立即实现 macOS `.app`、codesign、notarization 或 universal binary。
* 立即实现 Linux desktop package。
* 完整替代 Wails CLI 的 `wails dev` 热重载体验。
* Windows installer/NSIS；Windows agent portable zip 仍由项目或 release 工具负责。
* 前端 UI/UX 和业务功能调整。

## 当前问题

Wails CLI 官方 `wails build` 是一个面向开发者工作区的编排命令。它通常会：

1. 读取 `wails.json`。
2. 创建缺失的 embed 目录。
3. 生成 bindings 到 `frontend/wailsjs`。
4. 按 `wails.json` 执行 `frontend:install` 和 `frontend:build`。
5. 生成 Windows icon、manifest、`.syso` 等资源。
6. 调用 `go build` 输出到 `build/bin`。
7. 可选执行 hooks、压缩、installer 相关逻辑。

这些行为不适合作为 Bazel 正式生产 action 的黑盒：

* 可能写入源码目录，例如 `frontend/dist`、`frontend/wailsjs`、`build/bin`、临时 `.syso`。
* 会执行 `wails.json` 中的命令字符串，容易绕过 Bazel-managed pnpm/npm 和 Node toolchain。
* 内部阶段对 Bazel 不透明，难以做到精确输入声明、增量构建和 remote cache 命中。
* 输出路径和临时文件可能包含宿主机状态或工作区绝对路径。
* `wails build` 同时做 bindings、frontend、resource、go build 和 package，任何一步变化都会破坏细粒度缓存。

因此，本方案明确：**生产构建禁止调用 `wails build`**。Wails CLI 可以进入 Bazel toolchain，但只能用于受控子命令或诊断，不作为最终产物构建入口。

## 总体方案

采用 “Wails CLI hermetic tool + Bazel 拆阶段规则” 模型：

```text
tools/wails toolchain
  ├── 提供固定版本 wails CLI
  ├── 提供 wails_bindings rule
  ├── 提供 wails_frontend_assets / embed handoff rule
  ├── 提供 wails_windows_resources rule
  ├── 提供 wails_doctor / wails_version 诊断 target
  └── 提供 wails_app 聚合 macro

生产路径：
  frontend build/test     -> Bazel JS rules
  bindings generation     -> Bazel declared output
  frontend embed handoff  -> Bazel declared output
  windows resources/syso  -> Bazel declared output
  Go binary               -> rules_go
  portable zip/release    -> release/package rules

禁止路径：
  Bazel action -> wails build -> build/bin / frontend/dist / frontend/wailsjs
```

Wails CLI 在该模型中是工具，不是构建系统。构建系统仍然是 Bazel。

## 模型设计

### 目录结构

建议新增：

```text
tools/wails/
  ├── BUILD.bazel
  ├── defs.bzl
  ├── toolchain.bzl
  ├── repositories.bzl
  ├── private/
  │   ├── providers.bzl
  │   ├── toolchain.bzl
  │   ├── bindings.bzl
  │   ├── frontend_assets.bzl
  │   ├── windows_resources.bzl
  │   ├── app.bzl
  │   └── diagnostics.bzl
  └── helpers/
      ├── BUILD.bazel
      ├── stage_frontend.go
      ├── generate_winres.go
      ├── inspect_wails_config.go
      └── stable_zip.go       # 可选，若后续由 tools/wails 负责桌面包归档
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
        "binary": "Produced application binary",
        "frontend_assets": "Declared frontend assets used by embed",
        "bindings": "Declared bindings output, if generated",
        "resources": "Platform resources, if generated",
    },
)
```

### Toolchain type

```starlark
toolchain_type(
    name = "toolchain_type",
)
```

Toolchain implementation：

```starlark
wails_cli_toolchain(
    name = "linux_amd64_toolchain_impl",
    wails = "//tools/wails:wails_cli",
    version = "v2.x.y",
)

toolchain(
    name = "linux_amd64_toolchain",
    toolchain = ":linux_amd64_toolchain_impl",
    toolchain_type = "//tools/wails:toolchain_type",
    exec_compatible_with = [
        "@platforms//os:linux",
        "@platforms//cpu:x86_64",
    ],
)
```

项目 rule 使用：

```starlark
toolchains = ["//tools/wails:toolchain_type"]
```

### Wails CLI 来源

首选：使用 `rules_go` 从已锁定 Go module 构建 Wails CLI。

当前 `MODULE.bazel` 已通过 `go_deps.from_file(go_mod = "//:go.mod")` 引入 `com_github_wailsapp_wails_v2`。因此 `tools/wails` 应优先提供一个仓库内 `go_binary` 包装：

```starlark
go_binary(
    name = "wails_cli",
    embed = ["@com_github_wailsapp_wails_v2//cmd/wails"],
    visibility = ["//visibility:public"],
)
```

如果外部 repo 没有可直接引用的 Bazel target，则在 `tools/wails/repositories.bzl` 中提供 module extension 或 repository rule，固定 Wails source archive、sha256 和生成的 BUILD 文件。

不推荐直接下载开发者机器上的 `wails` 或使用 `go install ...@latest`，因为这会造成版本漂移。

### 禁用 `wails build`

`tools/wails` 不提供生产可见的 `wails_build` rule。

允许提供受限诊断 target：

```starlark
wails_version_test(
    name = "wails_version_test",
)

wails_doctor(
    name = "wails_doctor",
    tags = ["manual"],
)
```

如果确实需要对比官方 `wails build` 行为，只能提供 `manual`、`local`、非 CI 的兼容 target，并且命名必须带有 `compat` 或 `manual`：

```starlark
wails_build_compat(
    name = "wails_build_compat_manual",
    tags = ["manual", "local", "no-remote-cache"],
)
```

该 target 不得被 `wails_app`、release target、CI target 或 `bazel test //...` 依赖。

正式约束：

* `wails_app` 不调用 `wails build`。
* `windows_agent_package` 不依赖任何 `wails_build_compat*` target。
* CI 不运行 `wails build`。
* README 明确 `wails build` 不是生产构建入口。

## Rule 设计

### `wails_bindings`

用途：生成前端 JS/TS bindings，输出到 Bazel declared directory。

示例：

```starlark
wails_bindings(
    name = "bindings",
    wails_json = "wails.json",
    go_srcs = [
        "//projects/game/windows_agent/cmd/windows_agent:windows_agent_lib",
        "//projects/game/windows_agent/internal/app:app",
    ],
    out = "wailsjs",
)
```

要求：

* 使用 toolchain 中的 `wails`。
* 输出只能写入 `ctx.actions.declare_directory("wailsjs")`。
* 不写 `frontend/wailsjs` 源码目录。
* bindings 生成需要 host 可执行工具时，必须为 exec platform 构建。
* 所有影响 bindings 的 Go 源文件、`wails.json`、生成配置都必须作为 inputs。

### `wails_frontend_assets`

用途：将前端构建产物重根为 Go embed 可见路径。

示例：

```starlark
wails_frontend_assets(
    name = "frontend_dist",
    src = "//projects/game/windows_agent/frontend:dist",
    out = "frontend_dist",
)
```

要求：

* `src` 必须是 Bazel frontend build target 输出，不得是源码树 `frontend/dist`。
* 输出目录内部路径稳定，例如：

```text
frontend_dist/index.html
frontend_dist/assets/index-<contenthash>.js
frontend_dist/assets/index-<contenthash>.css
```

* 如需 hash 文件名，必须由内容决定，不包含时间戳、绝对路径或随机数。

### `wails_windows_resources`

用途：生成 Windows `.syso` 或其他 Windows resource declared output。

示例：

```starlark
wails_windows_resources(
    name = "windows_resources",
    icon = "resources/icon.ico",
    manifest = "build/windows/wails.exe.manifest",
    info = "build/windows/info.json",
    out = "windows-agent-res.syso",
)
```

要求：

* 不在项目根目录临时写 `.syso`。
* 输出文件名稳定。
* 版本、manifest、icon 都来自 declared inputs。
* 若 rules_go 对 `.syso` 的接入需要特定布局，应由 rule 或 macro 负责 staging 到 execroot 内的稳定路径，而不是写源码目录。

### `wails_app`

用途：项目侧聚合 macro。它负责把 Wails 概念转换为 Bazel target 依赖图，而不是执行 `wails build`。

示例：

```starlark
wails_app(
    name = "windows_agent_app",
    binary_name = "windows-agent",
    platform = "windows/amd64",
    wails_json = "wails.json",
    frontend = "//projects/game/windows_agent/frontend:dist",
    go_binary = "//projects/game/windows_agent/cmd/windows_agent:windows_agent_windows",
    icon = "resources/icon.ico",
    visibility = ["//visibility:public"],
)
```

展开后的依赖关系：

```text
wails_app
  -> wails_bindings            # 可选，若前端需要 generated bindings
  -> frontend:dist             # Bazel JS/Vite build
  -> wails_frontend_assets     # 重根到 frontend_dist/**
  -> wails_windows_resources   # 可选，Windows .syso/resource
  -> rules_go go_binary        # desktop,production tags/linkopts
```

### `wails_config_check`

用途：校验 `wails.json` 不会引入漂移。

示例：

```starlark
wails_config_check(
    name = "wails_config_check",
    wails_json = "wails.json",
)
```

检查项：

* 生产项目不得配置会被 CI 执行的 `frontend:install` / `frontend:build` 命令，除非命令是明确的 Bazel wrapper。
* 不允许 hooks 在生产构建中执行非 Bazel 声明工具。
* `outputfilename`、`frontend:dir`、`build:dir` 等字段若被 Bazel rule 使用，必须与 BUILD 属性一致。

## Remote cache 稳定性要求

所有 `tools/wails` 生产 rule 必须满足：

1. **输入完整声明**：Go 源文件、frontend 源文件、`wails.json`、icon、manifest、info、pnpm lock、package.json、tool binary 都必须作为 inputs。
2. **输出只写 declared outputs**：不得写源码目录、用户 home、全局 cache 或 `build/bin`。
3. **稳定工作目录**：action 在 sandbox 临时目录中运行；输出不包含 execroot 绝对路径。
4. **稳定环境变量**：显式设置 `HOME`、`TMPDIR`、`CI`、`NO_COLOR` 等必要环境，禁止读取用户 shell 环境。
5. **禁用网络**：生产 action 不下载依赖；Wails CLI、Node deps、Go deps 均由 Bazel 管理。
6. **无时间戳产物**：zip、manifest、bindings、resource 输出不得包含当前时间；如格式要求时间戳，必须固定或由 declared input 提供。
7. **无随机输出**：禁止随机文件名、随机 ID；hash 文件名只能来自内容 hash。
8. **平台显式**：目标平台通过 rule 属性或 Bazel platform 表达，不能由宿主机隐式推断。
9. **执行平台与目标平台分离**：bindings 生成等需要运行的工具面向 exec platform；最终 Go binary 面向 target platform。
10. **不使用 `wails build`**：生产路径不调用该黑盒编排命令。

## 代码分层

### `tools/wails/BUILD.bazel`

职责：

* 导出 `defs.bzl`。
* 定义 Wails CLI binary target。
* 定义 toolchain implementation 和 toolchain target。

### `tools/wails/defs.bzl`

职责：

* 对外暴露稳定 API：`wails_app`、`wails_bindings`、`wails_frontend_assets`、`wails_windows_resources`、`wails_config_check`。
* 不放复杂实现逻辑；实现放在 `private/`。

### `tools/wails/private/*`

职责：

* 实现 Starlark rules。
* 管理 providers。
* 管理 action env、toolchains、outputs、runfiles。

### `tools/wails/helpers/*`

职责：

* 提供小型 Go helper，处理跨平台 staging、resource 生成、config 检查。
* helper 自身用 `go_unittest` 覆盖。
* helper 输出必须稳定。

## 决策详情

### 决策 1：禁用生产 `wails build`

原因：

* `wails build` 是黑盒编排命令，内部写目录和执行命令对 Bazel 不透明。
* 它会弱化 remote cache 命中能力。
* 它容易绕过 Bazel-managed pnpm、Go toolchain 和声明式 inputs。

### 决策 2：Wails CLI 仍作为 toolchain 引入

原因：

* Wails CLI 是官方能力入口，后续 bindings、doctor、兼容检查仍有价值。
* 固定版本后可以避免开发者本机 Wails 版本漂移。
* toolchain 让不同 exec platform 的 CLI 选择具备扩展空间。

### 决策 3：优先从 Go module 构建 Wails CLI

原因：

* 仓库已由 `go.mod`/`go_deps` 锁定 Wails 版本。
* 比下载预构建二进制更透明。
* 适合 Bazel remote cache 和统一 Go toolchain。

### 决策 4：`wails_app` 是 macro/rule 聚合，不是 `wails build` wrapper

原因：

* 聚合 Bazel target 可以保留细粒度缓存。
* 每个阶段可单独测试和复用。
* 项目侧仍获得类似 Wails app 的声明式入口。

### 决策 5：`wails.json` 保留但受校验

原因：

* 它是 Wails 项目语义入口，开发者和工具会预期存在。
* Bazel 生产构建不应盲目信任其中的命令字段。
* `wails_config_check` 可以防止 `frontend:build`、hooks 等字段重新引入漂移。

## 迁移步骤

### Step 1：建立 toolchain 骨架

1. 新增 `tools/wails/BUILD.bazel`、`defs.bzl`、`toolchain.bzl`、`private/providers.bzl`。
2. 定义 `WailsToolchainInfo` 和 `toolchain_type`。
3. 用 `rules_go` 构建固定版本 `wails_cli`。
4. 在 `MODULE.bazel` 或根 BUILD 配置中注册 toolchain。
5. 增加 `wails_version_test`，验证 Bazel 使用的是固定版本 CLI。

### Step 2：增加禁用 `wails build` 的 config check

1. 实现 `wails_config_check`。
2. 对 `projects/game/windows_agent/wails.json` 增加检查 target。
3. 检查 README 和 BUILD 中没有生产路径依赖 `wails build`。

### Step 3：接入 frontend asset handoff

1. 将 `design/windows_agent_frontend_bazel_wails_build.md` 中的 frontend:dist 作为输入。
2. 实现 `wails_frontend_assets`，输出 `frontend_dist/**`。
3. 更新 `assets` 包依赖生成输出，删除源码 symlink。

### Step 4：接入 bindings 生成

1. 实现 `wails_bindings` declared output。
2. 前端 build target 依赖 generated `wailsjs`。
3. 不再提交 `frontend/wailsjs` generated output。

### Step 5：接入 Windows resources

1. 实现 `wails_windows_resources`。
2. 将 `.syso` 或资源 archive 作为 Go binary 的 declared input。
3. 验证 Windows binary icon/manifest 稳定。

### Step 6：提供 `wails_app` 聚合 macro

1. 封装 frontend assets、bindings、resources、Go binary 参数。
2. 迁移 `projects/game/windows_agent` 使用 `wails_app`。
3. 保持 `windows_agent_package` 只处理最终 binary、ffmpeg、input helper、icon 和 zip。

## 验收标准

### Toolchain 验收

* `bazel build //tools/wails:wails_cli` 成功。
* `bazel test //tools/wails:...` 成功。
* `wails_version_test` 输出版本与仓库锁定版本一致。
* 构建日志中不出现本机 `PATH` 里的 `wails`。

### 禁用 `wails build` 验收

* `tools/wails` 没有对外暴露生产 `wails_build` rule。
* `wails_build_compat` 如存在，必须带 `manual`、`local`、`no-remote-cache` 标签。
* `bazel query 'somepath(//projects/game/windows_agent:windows_agent_win_zip, //tools/wails:任何_compat_build)'` 无路径。
* README 明确 CI/发布不使用 `wails build`。

### Remote cache 验收

* 清理工作区中 `frontend/dist`、`frontend/wailsjs`、`build/bin` 后，`bazel build //projects/game/windows_agent:windows_agent_win_zip` 仍成功。
* 重复构建相同输入时，Wails 相关 action 可命中 cache。
* Wails 相关 action 输出不包含绝对路径、当前时间或随机 ID。
* 所有 Wails 相关 outputs 位于 Bazel output tree。

### Windows agent 验收

* `bazel test //projects/game/windows_agent/...` 成功。
* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功。
* zip 内容稳定，仍包含：

```text
windows-agent.exe
resources/bin/ffmpeg.exe
resources/bin/ffmpeg.exe.sha256
resources/bin/input-helper.exe
resources/icon.ico
```

* 前端资源已嵌入 `windows-agent.exe`，不作为独立 `frontend/dist` 目录进入 zip。

## 风险与规避

### 风险 1：Wails CLI 源码构建依赖 CGO 或平台库

规避：

* 先验证 `cmd/wails` 作为 CLI tool 在 Linux exec platform 构建是否需要 GUI 平台库。
* 如构建 CLI 需要额外系统库，将 CLI toolchain 限定到 CI 已安装依赖的平台，或改用固定 sha256 的预构建 CLI 二进制。

### 风险 2：bindings 生成与 Bazel Go 包图不匹配

规避：

* 第一阶段可以只做 `wails_config_check` 和 toolchain，不急于生成 bindings。
* bindings rule 明确区分 exec platform 工具和 target platform Go 源码。
* 增加最小 fixture 测试覆盖 bindings 输出路径和内容稳定性。

### 风险 3：Windows `.syso` 难以接入 rules_go

规避：

* 先保留现有 icon-in-zip 方案。
* 将 `.syso` 作为后续阶段，不阻塞 frontend Bazel 化。
* 实现前先用小型 fixture 验证 rules_go 如何接收 generated `.syso`。

### 风险 4：开发者误用 `wails build`

规避：

* README 明确禁止生产使用。
* 不提供生产 wrapper。
* `wails_config_check` 防止 `wails.json` 中重新引入生产 build 命令漂移。

## 未来规划

* 支持 macOS `.app`、codesign 和 universal binary 的 Bazel 化。
* 支持 Linux desktop package。
* 将 Wails dev server 与 Bazel-managed pnpm/Vite dev target 结合，提供受控 `wails dev` 替代流程。
* 将 `tools/wails` 中通用的 frontend staging 能力与未来 `tools/frontend` 统一。
* 为 Wails app 增加大型测试模板，覆盖启动、首页资源加载、基础 IPC/bindings 调用。
