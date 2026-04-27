# Windows agent 前端 Bazel 化与 Wails 构建方案

## 目标

本方案用于将 `projects/game/windows_agent/frontend` 从“手工构建并提交 `dist` 产物”的模式迁移到 Bazel 管理的可复现构建模式，并保持 Wails v2 运行时对嵌入式前端资源的支持。

本方案希望达成的效果是：

* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 能完整构建 Windows agent，包括 Svelte/Vite 前端、Go/Wails 二进制、ffmpeg/input helper 资源和 portable zip。
* `bazel test //projects/game/windows_agent/...` 能覆盖 Go 单测和前端单测，不需要开发者或 CI 在 Bazel 外直接执行 `pnpm build`。
* 仓库不再保存 `frontend/dist` 这类前端构建产物，避免过期产物、hash 文件冲突和不可复现构建。
* 保留 Wails 官方推荐的 `frontend/` 与 `wails.json` 认知模型，让本项目仍然容易被熟悉 Wails 的开发者理解。

本方案是在 `design/llm_agent_play_game_milestone1_windows_agent.md` 的 Windows agent 方案基础上，对前端构建和 Wails asset embedding 交接方式进行补充。

## 范围

本方案覆盖：

* `projects/game/windows_agent/frontend` 的 Bazel build/test target 设计。
* Vite/Svelte 构建产物到 Go `//go:embed` 的交接方式。
* `projects/game/windows_agent/assets` 包的职责调整。
* `wails.json` 与 Wails 官方目录结构的取舍。
* `projects/game/windows_agent/README.md` 中生产构建和本地开发入口的说明。
* 需要删除或忽略的生成产物。
* 验收目标和迁移步骤。

本方案不包括：

* Windows agent 业务功能、协议、采集、编码和输入控制逻辑改造。
* S3 发布工具语义改造；发布工具设计见 `design/s3_artifact_release_tool.md`。
* NSIS installer、WebView2 fixed runtime、自动更新和多平台桌面包。
* Wails CLI 作为 Bazel 工具依赖的完整封装；该能力属于后续 `tools/wails` 通用规则建设。
* 前端 UI/UX 重设计。

## 当前问题

当前 `projects/game/windows_agent` 的前端链路如下：

```text
frontend/src + frontend/index.html
  -- 手工 pnpm build / vite build --> frontend/dist
  -- assets/frontend_dist symlink --> assets/frontend_dist
  -- go:embed --> assets.FrontendDist
  -- Wails AssetServer --> 桌面窗口资源
```

对应文件：

* `projects/game/windows_agent/frontend/package.json` 只定义 `dev` 和 `build`，其中 `build` 为 `vite build`。
* `projects/game/windows_agent/frontend/dist` 已存在构建产物。
* `projects/game/windows_agent/assets/frontend_dist` 是指向 `../frontend/dist` 的符号链接。
* `projects/game/windows_agent/assets/assets.go` 使用 `//go:embed all:frontend_dist`。
* `projects/game/windows_agent/assets/BUILD.bazel` 使用 `embedsrcs = glob(["frontend_dist/**"])`。
* `projects/game/windows_agent/wails.json` 的 `frontend:install`、`frontend:build`、`frontend:dev:watcher`、`frontend:dev:serverUrl` 均为空。

这带来以下问题：

1. **前端没有由 Bazel 编译**：仓库规范要求 TS/JS 项目使用 Bazel 编译，而不是 CI 或开发者直接运行 `pnpm build`。
2. **构建产物入库**：`frontend/dist` 是 Vite 生成目录，包含 hash 后的 JS/CSS 文件，提交后容易产生过期产物、合并冲突和 review 噪音。
3. **Go embed 交接脆弱**：当前依赖 `assets/frontend_dist -> ../frontend/dist` 符号链接。该链接规避了 Go `//go:embed` 不能使用 `..` 的限制，但不是受 Bazel action 管理的 declared output。
4. **Bazel target 依赖不完整**：`windows_agent_windows` 只依赖 `assets` 包，而 `assets` 包只读取当前工作区已有的 `frontend_dist/**` 文件；Bazel 不知道需要先构建 Vite 前端。
5. **前端无单测入口**：目前没有 `*.test.*` / `*.spec.*` 文件、Vitest 配置或 Bazel `js_test` 目标。
6. **Wails CLI 工作流不明确**：Wails 官方通常通过 `wails.json` 的 `frontend:build` 构建前端；当前配置为空，说明项目实际以 Bazel 为权威构建入口，但没有文档化开发工作流。

## 官方 Wails 结构对比

Wails v2 官方项目通常包含：

```text
.
├── build/
├── frontend/
├── go.mod
├── go.sum
├── main.go
└── wails.json
```

关键约定：

* `frontend/` 可以是任意前端项目，例如 Svelte、React、Vue 或普通 HTML。
* 生产资源通常通过 Go `//go:embed all:frontend/dist` 进入 `embed.FS`，然后传给 `assetserver.Options{Assets: ...}`。
* `wails.json` 通过 `frontend:install`、`frontend:build`、`frontend:dev:watcher`、`frontend:dev:serverUrl` 描述 Wails CLI 工作流。
* `//go:embed` 的路径必须位于声明它的 Go 文件所在目录或子目录下，不能使用 `..`、绝对路径或 `bazel-out` 路径。

本项目当前与官方结构的关系：

* 保留了 `frontend/` 和 `wails.json`，符合 Wails 开发者的认知入口。
* `main.go` 放在 `cmd/windows_agent/`，不是官方模板根目录 `main.go`，但符合本仓库 Go 项目布局和 Bazel `go_binary` 习惯，不影响 Wails 运行时。
* 使用独立 `assets` 包向 Wails 提供 `embed.FS`，属于合理变体。
* 当前问题不是 Wails 运行时结构，而是前端 build output 没有被 Bazel 声明式管理。

因此，本方案不要求把 `cmd/windows_agent/main.go` 移回项目根目录。保留 `frontend/` 与 `wails.json`，将 Bazel 作为生产构建入口，是本仓库更合适的结构。

## 总体方案

采用 Bazel-owned asset handoff：

```text
frontend 源码
  -> Bazel Vite build target
  -> Bazel declared dist tree/file set
  -> Bazel copy/re-root 到 assets 包逻辑路径 frontend_dist/**
  -> assets.go //go:embed all:frontend_dist
  -> cmd/windows_agent/main.go Wails AssetServer
  -> windows_agent_package portable zip
```

核心原则：

1. 前端编译必须是 Bazel target 的一部分。
2. Go `//go:embed` 只能看到 package-relative 逻辑路径 `frontend_dist/**`，不能指向 `../frontend/dist` 或 `bazel-out/...`。
3. `frontend/dist` 不入库；本地开发需要生成时也应被 `.gitignore` 忽略。
4. `assets/frontend_dist` 不再作为手工维护的源码 symlink 入库。
5. Wails 运行时仍然接收 `assets.FrontendDist`，不要求资源实际位于 `frontend/dist`。

## 模型设计

### BUILD 模型

新增 `projects/game/windows_agent/frontend/BUILD.bazel`，定义前端构建和测试目标：

```starlark
load("@aspect_rules_js//js:defs.bzl", "js_binary", "js_test")

filegroup(
    name = "srcs",
    srcs = glob([
        "index.html",
        "package.json",
        "tsconfig.json",
        "vite.config.ts",
        "src/**",
    ]),
)

# 具体实现可以是 js_binary 包装 vite，也可以是自定义 Starlark rule。
# 成功标准是输出 Bazel declared dist，而不是写入源码目录 frontend/dist。
js_binary(
    name = "vite_build",
    data = [
        ":srcs",
        "@npm//vite",
        "@npm//@sveltejs/vite-plugin-svelte",
        "@npm//svelte",
        "@npm//typescript",
    ],
    entry_point = "@npm//vite/bin:vite.js",
)
```

最终应提供一个可被其他 Bazel target 依赖的产物，例如：

```starlark
frontend_dist(
    name = "dist",
    srcs = [":srcs"],
    vite = ":vite_build",
    out = "dist",
)
```

`frontend_dist` 可以先作为项目内私有 rule 实现；若后续多个项目需要 Vite/Svelte，则再抽象到 `tools/frontend`。

### Asset handoff 模型

`projects/game/windows_agent/assets/BUILD.bazel` 不再 glob 源码树中的 symlink，而是依赖前端 build target：

```starlark
load("@rules_go//go:def.bzl", "go_library")
load("//projects/game/windows_agent/assets:defs.bzl", "frontend_embed_assets")

frontend_embed_assets(
    name = "frontend_dist",
    src = "//projects/game/windows_agent/frontend:dist",
    out = "frontend_dist",
)

go_library(
    name = "assets",
    srcs = ["assets.go"],
    embedsrcs = [":frontend_dist"],
    importpath = "dominion/projects/game/windows_agent/assets",
    visibility = ["//visibility:public"],
)
```

这里的关键不是具体 rule 名称，而是 Bazel 产物对 Go embed 的逻辑路径必须表现为：

```text
projects/game/windows_agent/assets/frontend_dist/index.html
projects/game/windows_agent/assets/frontend_dist/assets/*.js
projects/game/windows_agent/assets/frontend_dist/assets/*.css
```

这样 `assets.go` 可以保持：

```go
//go:embed all:frontend_dist
var FrontendDist embed.FS
```

### Package 模型

顶层 `windows_agent_package` 不需要理解前端细节。依赖链应变为：

```text
//projects/game/windows_agent:windows_agent_win_zip
  -> //projects/game/windows_agent/cmd/windows_agent:windows_agent_windows
    -> //projects/game/windows_agent/assets:assets
      -> //projects/game/windows_agent/assets:frontend_dist
        -> //projects/game/windows_agent/frontend:dist
```

这样 portable zip 的构建自然触发前端构建。

## 代码分层

### `frontend/`

职责：

* 保存 Svelte/Vite 源码、`package.json`、`tsconfig.json`、`vite.config.ts`。
* 提供 Bazel 前端 build/test target。
* 不保存 `dist/` 构建产物。

建议结构：

```text
projects/game/windows_agent/frontend/
  ├── BUILD.bazel
  ├── package.json
  ├── tsconfig.json
  ├── vite.config.ts
  ├── index.html
  ├── src/
  └── .gitignore
```

### `assets/`

职责：

* 只提供 Wails `AssetServer` 需要的 `embed.FS`。
* 声明 Bazel 生成的 `frontend_dist` 为 `embedsrcs`。
* 不维护指向 `../frontend/dist` 的源码 symlink。

建议结构：

```text
projects/game/windows_agent/assets/
  ├── BUILD.bazel
  ├── assets.go
  └── defs.bzl        # 可选，若先在项目内实现 copy/re-root rule
```

### `cmd/windows_agent/`

职责保持不变：

* 构造 Wails app。
* 将 `assets.FrontendDist` 传给 `assetserver.Options`。
* 不直接关心前端构建方式。

### `release/`

职责保持不变：

* 打包 Windows Go binary、ffmpeg、input helper 和 icon。
* 不直接处理 frontend/dist，因为前端已经嵌入 Go binary。

### `README.md`

职责：

* 明确 Windows agent 的权威生产构建入口是 Bazel，而不是直接 `wails build` 或 `pnpm build`。
* 说明 `wails.json` 保留用于 Wails 项目识别和后续 dev/bindings 兼容，不代表 CI 使用 Wails CLI 构建。
* 说明前端本地开发命令必须通过 Bazel-managed pnpm 执行，避免开发者使用系统 pnpm 产生不可复现依赖。
* 记录 `frontend/dist` 和 `assets/frontend_dist` 不应入库。

## 关键细节

### Vite 输出目录

Vite 默认输出 `dist`。在 Bazel action 中不能写回源码目录 `projects/game/windows_agent/frontend/dist`，而应写入 action 输出目录。

可选实现方式：

1. 在 Vite wrapper 中传入 `--outDir "$TMPDIR/dist"`，构建后复制到 declared output directory。
2. 使用自定义 Starlark rule 创建 tree artifact 作为 `dist`。
3. 若 `rules_js` 目标直接支持目录输出，则优先使用目录输出，并在 asset handoff rule 中重根到 `frontend_dist`。

无论采用哪种方式，禁止让 Bazel action 写入源码目录。

### Go embed 与 Bazel 输出路径

不能这样做：

```go
//go:embed all:../frontend/dist
```

也不能让 Go 源码引用：

```go
//go:embed all:bazel-out/.../dist
```

正确做法是让 Bazel 将生成产物作为 `assets` 包的 `embedsrcs`，并让其逻辑路径匹配 `frontend_dist/**`。Go 源码只看到 package-relative 路径。

### 前端测试

建议采用 Vitest，原因：

* 与 Vite/Svelte 生态匹配。
* 可复用 Vite 配置和 ESM 模型。
* 可通过 Bazel `js_test` 或自定义 `vitest_test` 宏运行。

`frontend/package.json` 建议增加：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run"
  },
  "devDependencies": {
    "vitest": "catalog:"
  }
}
```

同时在根 `pnpm-workspace.yaml` 的 catalog 中增加 `vitest` 版本，并使用仓库要求的 Bazel-managed pnpm 更新 lockfile。

### Wails CLI 工作流

生产构建以 Bazel 为准。`wails.json` 可以保持 frontend 命令为空，或设置为调用 Bazel 包装脚本。

推荐短期策略：

* `wails.json` 保留空 frontend build 字段，表示 Wails CLI 不作为生产构建入口。
* 新增或更新 `projects/game/windows_agent/README.md` 说明：
  * 生产构建使用 `bazel build //projects/game/windows_agent:windows_agent_win_zip`。
  * 验证使用 `bazel test //projects/game/windows_agent/...`。
  * 前端开发可使用 `bazel run @pnpm -- --dir /abs/path/to/projects/game/windows_agent/frontend dev` 或后续封装的 Bazel dev target。
  * 不直接运行本机 `pnpm build` 作为提交或 CI 前置步骤。

推荐长期策略：

* 引入 `tools/wails` 通用 Bazel rule 后，让 `wails.json` 的生产字段仅作为 Wails CLI 兼容说明。
* 若必须支持 `wails dev`，可让 `frontend:dev:watcher` 调用 Bazel 管理的 pnpm/vite dev server，而不是直接依赖本机 pnpm。

### 生成文件与 ignore

需要移除：

```text
projects/game/windows_agent/frontend/dist/**
projects/game/windows_agent/assets/frontend_dist
```

需要新增或更新 ignore：

```text
projects/game/windows_agent/frontend/.gitignore
  /dist
```

如果保留本地 Wails CLI 开发模式生成的临时文件，也应明确 ignore，不能进入 Git。

## 决策详情

### 决策 1：不提交 `frontend/dist`

原因：

* `dist` 是 Vite 生成产物，包含 hash 文件名。
* 提交后无法保证与源码一致。
* 与 Bazel 可复现构建目标冲突。

### 决策 2：不继续使用 symlink handoff

原因：

* symlink 不是 Bazel 声明式构建产物。
* 依赖源码树中已有 `frontend/dist`。
* 容易在干净 checkout 或 CI 中产生空白 Wails 窗口。

### 决策 3：保留 `assets` 包

原因：

* Go `//go:embed` 对路径有严格限制。
* `cmd/windows_agent/main.go` 位于 `cmd/`，直接 embed `frontend/dist` 不方便。
* 独立 `assets` 包可以作为 Bazel generated assets 与 Wails runtime 的稳定边界。

### 决策 4：不强制迁移到 Wails 官方根 `main.go` 布局

原因：

* 本仓库 Go 项目使用 `cmd/<binary>` 是合理布局。
* Wails 运行时只需要 `assetserver.Options{Assets: fs}`，不要求 main.go 必须在根目录。
* 迁移 main.go 会扩大改动范围，但不能解决前端 Bazel 化核心问题。

### 决策 5：前端测试纳入 Bazel

原因：

* 仓库以 `bazel test` 作为验证标准。
* 只提供 `pnpm test` 不满足统一验证入口。
* 前端 build target 与 test target 应使用相同依赖锁定和 Node toolchain。

## 迁移步骤

### Step 1：新增前端 Bazel build target

1. 在 `projects/game/windows_agent/frontend/BUILD.bazel` 中声明 frontend sources。
2. 使用 Bazel-managed Node/pnpm/npm dependency 执行 Vite build。
3. 输出 declared `dist`，不写源码目录。
4. 验证 `bazel build //projects/game/windows_agent/frontend:dist` 成功。

### Step 2：新增 asset handoff target

1. 在 `projects/game/windows_agent/assets` 中实现 copy/re-root 逻辑。
2. 将 `//frontend:dist` 重根为 `frontend_dist/**`。
3. 更新 `go_library.embedsrcs` 依赖生成目标。
4. 删除 `embedsrcs = glob(["frontend_dist/**"])`。

### Step 3：删除生成产物和 symlink

1. 删除 `projects/game/windows_agent/frontend/dist/**`。
2. 删除 `projects/game/windows_agent/assets/frontend_dist` symlink。
3. 新增 `projects/game/windows_agent/frontend/.gitignore`，忽略 `/dist`。
4. 确认干净 checkout 后无需手工生成 dist 即可 Bazel build。

### Step 4：新增前端测试

1. 引入 Vitest 依赖和 catalog 版本。
2. 添加至少一个最小 smoke test，例如验证核心状态映射或组件工具函数。
3. 用 Bazel `js_test` 或 `vitest_test` 宏执行。
4. 将该 target 纳入 `bazel test //projects/game/windows_agent/...`。

### Step 5：更新 Windows agent README

1. 新增或更新 `projects/game/windows_agent/README.md`。
2. 写明生产构建入口：`bazel build //projects/game/windows_agent:windows_agent_win_zip`。
3. 写明验证入口：`bazel test //projects/game/windows_agent/...`。
4. 写明前端开发入口必须使用 Bazel-managed pnpm 或后续 Bazel dev target。
5. 写明 `wails.json` 的保留目的和限制：保留 Wails 项目语义，但 CI 不以 `wails build` 为入口。
6. 写明 `frontend/dist` 与 `assets/frontend_dist` 不应提交。

### Step 6：验证完整构建链路

执行：

```bash
bazel test //projects/game/windows_agent/...
bazel build //projects/game/windows_agent:windows_agent_win_zip
```

并检查 zip 内容仍包含：

```text
windows-agent.exe
resources/bin/ffmpeg.exe
resources/bin/ffmpeg.exe.sha256
resources/bin/input-helper.exe
resources/icon.ico
```

前端资源不应作为 zip 中的独立目录出现，因为它们应已嵌入 `windows-agent.exe`。

## 验收标准

### 构建验收

* `bazel build //projects/game/windows_agent/frontend:dist` 成功。
* `bazel build //projects/game/windows_agent/cmd/windows_agent:windows_agent_windows` 成功，并触发前端构建。
* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功，无需预先运行 `pnpm build`。
* 从干净 checkout 删除 `frontend/dist` 后仍可构建成功。

### 测试验收

* `bazel test //projects/game/windows_agent/...` 包含前端测试 target。
* Go 现有单测全部通过。
* 前端至少有一个 Bazel 执行的 smoke/unit test。

### 仓库状态验收

* `git ls-files projects/game/windows_agent/frontend/dist` 无输出。
* `git ls-files projects/game/windows_agent/assets/frontend_dist` 无输出。
* `projects/game/windows_agent/frontend/.gitignore` 包含 `/dist`。
* 没有新增子包级 `pnpm-lock.yaml`。
* `pnpm-lock.yaml` 只通过 Bazel-managed pnpm 更新。

### Wails 兼容验收

* `cmd/windows_agent/main.go` 仍通过 `assetserver.Options{Assets: assets.FrontendDist}` 提供资源。
* `wails.json` 保留在 `projects/game/windows_agent/` 下。
* `frontend/` 仍是标准 Wails 前端目录。
* 不要求 `wails build` 作为 CI 构建入口；CI 以 Bazel target 为准。

### 文档验收

* `projects/game/windows_agent/README.md` 存在。
* README 明确生产构建入口、测试入口、前端开发入口和 Wails CLI 的定位。
* README 明确 `frontend/dist` 与 `assets/frontend_dist` 不应入库。

## 风险与规避

### 风险 1：Bazel 目录输出与 Go embedsrcs 路径不匹配

表现：Go 编译时提示 embed pattern 找不到文件，或运行时 Wails 找不到 `index.html`。

规避：

* asset handoff rule 必须输出 `frontend_dist/index.html`，而不是只输出 `dist/index.html`。
* 增加一个小型 Go 测试或 Bazel smoke test，读取 `assets.FrontendDist` 并确认存在 `frontend_dist/index.html`。

### 风险 2：Vite 依赖 action 中的当前工作目录

表现：Vite 找不到 `index.html` 或 `vite.config.ts`。

规避：

* Vite wrapper 在 action 中将前端源码复制到临时工作目录后执行。
* 或显式传入 `--config`、`--root`、`--outDir`。

### 风险 3：Vitest/Svelte 测试依赖 DOM 环境

表现：组件测试需要 jsdom/happy-dom。

规避：

* 初期只做纯函数或 smoke test。
* 若需要组件测试，再引入 `happy-dom` 或 `jsdom`，并纳入 catalog。

### 风险 4：开发者仍习惯直接运行 pnpm

表现：本地生成 `frontend/dist` 后误提交。

规避：

* `.gitignore` 忽略 `/dist`。
* 文档明确生产构建入口为 Bazel。
* 如需本地命令，使用 `bazel run @pnpm -- --dir <abs path> ...`。

## 未来规划

* 将 Vite/Svelte build/test 封装抽象到 `tools/frontend`，供后续其他前端项目复用。
* 建设 `tools/wails`，将 Wails CLI、Windows `.syso`、icon/manifest 生成和 package 流程统一成 Bazel rule。
* 支持 `wails dev` 的 Bazel-friendly 开发模式，包括 Vite dev server 和 Wails bindings 生成。
* 增加桌面 GUI 大型测试计划，例如启动 Windows agent 后通过自动化验证首页资源、连接状态和日志面板。
