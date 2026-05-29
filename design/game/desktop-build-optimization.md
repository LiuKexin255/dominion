# Game Desktop 构建规则优化方案

## 背景

`design/game/desktop-client.md` 定义了 `projects/game/desktop` 的 Wails 桌面客户端。当前实现已经将前端构建、Wails assets embed 和 Windows 可执行文件串入 Bazel 构建图，但构建规则存在以下问题：

1. `projects/game/desktop/frontend/vite_build.bzl` 是项目内临时规则，不利于其他前端复用。
2. `wails_asset_library(name = "assets")` 同时生成 Go embed library 和 `:assets_provider`，调用方需要知道内部后缀。
3. `WailsAssetsInfo.assets_dir` 当前没有实际消费方，暴露了 staged assets 的内部实现细节。
4. `projects/game/desktop/assets/BUILD.bazel` 使用 `filegroup` 包装 `//projects/game/desktop/frontend:dist`，但包装没有提供额外语义。
5. Vite 构建会在源码目录生成 `frontend/dist/`，不符合 Bazel 管理产物的要求。
6. `frontend/` 下出现 `bazel-out/`，说明构建 action 已污染源码目录或把 Bazel 执行环境泄漏到源码目录。

本方案只优化构建规则和 target API，不改变桌面客户端的业务功能、UI 行为和 gateway 接口访问方式。

## 目标

1. 在 `tools/dev/js` 提供通用 Vite Bazel rule，供 `projects/game/desktop/frontend` 和后续前端项目复用。
2. Vite 构建产物必须是 Bazel declared directory，不在源码目录生成 `dist/`。
3. Vite rule 的关键输入通过显式参数声明，避免把配置文件和源码混在一个大的 `srcs` 队列中。
4. 拆分 Wails assets API：
   - `wails_asset_library` 只生成 Go embed library。
   - `wails_asset_provider` 只提供 `WailsAssetsInfo` 给 `wails_app`。
5. 移除 `WailsAssetsInfo.assets_dir`，避免外部调用方传递未被消费的内部 stage target。
6. `wails_app` 的 `assets` 参数引用与语义一致的 provider target，例如 `:assets`，不再引用 `:assets_provider`。
7. `wails_asset_library` 可以直接引用 frontend 的 `:dist` target，不需要额外 `filegroup` 包装。
8. 清理 `projects/game/desktop/frontend/dist/` 和 `projects/game/desktop/frontend/bazel-out/`，确保生成产物不进入源码树。

## 非目标

1. 不移除 Svelte 或 Vite 技术选型。
2. 不改造 Wails runtime、Go 后端 API 或前端 UI 交互逻辑。
3. 不引入新的 JS 包管理器或替换现有 pnpm workspace 管理方式。
4. 不实现 Wails binding 自动生成。
5. 不改变 `//projects/game/desktop:desktop` 的 Windows 构建平台要求。

## 代码分层

### 通用 JS/Vite 构建规则

新增：

```text
tools/dev/js/vite.bzl
```

职责：

1. 声明 `vite_build` rule。
2. 使用 `ctx.actions.declare_directory` 生成 Bazel 管理的 dist tree artifact。
3. 在 action 中执行 Vite，并强制 `--outDir` 指向 declared directory。
4. 不在源码目录创建或复制 `dist/`。

### Wails assets 规则

修改：

```text
tools/release/wails/private/assets.bzl
tools/release/wails/private/providers.bzl
tools/release/wails/defs.bzl
```

职责拆分：

1. `wails_asset_library`：生成可被 Go 代码 import 的 embed library。
2. `wails_asset_provider`：生成 `WailsAssetsInfo` provider，供 `wails_app(assets = ...)` 消费。

### Game Desktop 调用方

修改：

```text
projects/game/desktop/frontend/BUILD.bazel
projects/game/desktop/assets/BUILD.bazel
projects/game/desktop/BUILD.bazel
```

职责：

1. `frontend:dist` 使用通用 Vite rule 产出 Bazel tree artifact。
2. `assets:assets_lib` 生成 Go embed library。
3. `assets:assets` 提供 Wails assets provider。
4. `desktop_lib` 依赖 `assets:assets_lib`。
5. `wails_app` 依赖 `assets:assets`。

## 模型设计

### ViteBuild

建议 rule API：

```bzl
vite_build(
    name,
    package_json,
    index_html,
    config,
    tsconfig = None,
    svelte_config = None,
    srcs = [],
    out = "dist",
    visibility = None,
)
```

字段说明：

| 字段 | 说明 |
|---|---|
| `package_json` | 前端 package 描述文件，用于定位 package 目录和声明依赖输入。 |
| `index_html` | Vite HTML 入口，显式声明为关键入口文件。 |
| `config` | `vite.config.ts` 或 `vite.config.js`。 |
| `tsconfig` | TypeScript 配置文件，可选但 desktop 前端应显式传入。 |
| `svelte_config` | Svelte 配置文件，可选但 desktop 前端应显式传入。 |
| `srcs` | 业务源码集合，例如 `glob(["src/**"])`。 |
| `out` | declared directory 名称，默认 `dist`。 |

`srcs` 只承载业务源码，不承载 package/config/html 等关键文件。这样 BUILD 文件能直接表达哪些文件是构建入口和配置，减少因为大列表排序或注释约定产生的隐式规则。

### WailsAssetsInfo

调整后 provider：

```bzl
WailsAssetsInfo = provider(
    fields = {
        "library": "Go assets library target consumed by the app go_library",
        "importpath": "Go importpath for the generated assets package",
    },
)
```

移除：

```bzl
assets_dir
```

原因：当前 `wails_app` 不直接消费 staged assets directory，真正进入二进制的是 Go embed library。保留 `assets_dir` 会迫使调用方知道 `:assets_lib_stage` 这类内部 target，增加 API 噪声。

### wails_asset_library

建议 API：

```bzl
wails_asset_library(
    name = "assets_lib",
    src = "//projects/game/desktop/frontend:dist",
    importpath = "dominion/projects/game/desktop/assets",
)
```

职责：

1. 校验 `src` 提供且只提供一个目录 artifact。
2. stage frontend assets。
3. 生成 `assets.go`。
4. 生成 Go library target，供 Go 代码 import。

`src` 应直接引用 `//projects/game/desktop/frontend:dist`，不需要再通过 `filegroup` 包装。

`_stage_assets.src` 应使用普通 target configuration，不使用 `cfg = "exec"`。前端 assets 是应用输入，不是执行期工具；只有 helper 工具需要 `cfg = "exec"`。

### wails_asset_provider

建议 API：

```bzl
wails_asset_provider(
    name = "assets",
    library = ":assets_lib",
    importpath = "dominion/projects/game/desktop/assets",
)
```

职责：

1. 提供 `WailsAssetsInfo`。
2. 作为 `wails_app(assets = ":assets")` 的输入。
3. 不生成 Go library，不 stage assets。

## 目标 BUILD 形态

### `projects/game/desktop/frontend/BUILD.bazel`

```bzl
load("//tools/dev/js:vite.bzl", "vite_build")

vite_build(
    name = "dist",
    package_json = "package.json",
    index_html = "index.html",
    config = "vite.config.ts",
    tsconfig = "tsconfig.json",
    svelte_config = "svelte.config.js",
    srcs = glob(["src/**"]),
    visibility = ["//visibility:public"],
)
```

### `projects/game/desktop/assets/BUILD.bazel`

```bzl
load("//tools/release/wails:defs.bzl", "wails_asset_library", "wails_asset_provider")

wails_asset_library(
    name = "assets_lib",
    src = "//projects/game/desktop/frontend:dist",
    importpath = "dominion/projects/game/desktop/assets",
    visibility = ["//visibility:public"],
)

wails_asset_provider(
    name = "assets",
    library = ":assets_lib",
    importpath = "dominion/projects/game/desktop/assets",
    visibility = ["//visibility:public"],
)
```

### `projects/game/desktop/BUILD.bazel`

```bzl
go_library(
    name = "desktop_lib",
    deps = [
        "//projects/game/desktop/assets:assets_lib",
        ...
    ],
)

wails_app(
    name = "desktop",
    assets = "//projects/game/desktop/assets:assets",
    binary_name = "game-desktop",
    go_library = ":desktop_lib",
    platform = "windows/amd64",
)
```

## 关键细节

### Vite 输出目录

Vite action 必须将输出目录指向 Bazel declared directory：

```text
vite build --outDir <declared_directory> --emptyOutDir
```

如果 Vite 要求相对路径，应在 action 内计算 declared directory 的绝对路径后传给 `--outDir`。禁止先输出到源码目录 `dist/` 再复制。

### package 目录定位

`vite_build` 可以通过 `package_json` 定位前端 package 目录，但不能依赖 `srcs[0]` 的顺序。当前临时实现要求 `package.json MUST be first`，这是隐式约定，应移除。

### node_modules

当前 desktop 前端依赖源码目录下的 `node_modules/`。本方案不改变依赖安装机制，但 Vite rule 不应把 `node_modules` 当作 Bazel 输出，也不应在源码目录写入构建产物。后续如引入更完整的 JS rules，可再将 node_modules 依赖也封闭进 Bazel action。

### `frontend/bazel-out` 清理

`frontend/bazel-out/` 不应存在于源码目录。它的出现说明构建过程进入真实源码目录执行命令，并泄漏了 Bazel 执行环境。修复后应直接删除该目录，并通过新 Vite rule 防止再次生成。

### 测试数据同步

以下 testdata 应同步迁移，避免继续示范旧 API：

```text
tools/release/wails/testdata/app/BUILD.bazel
tools/release/wails/testdata/asset_library/BUILD.bazel
```

旧写法：

```bzl
wails_app(
    assets = ":assets_provider",
)
```

新写法：

```bzl
wails_app(
    assets = ":assets",
)
```

## 决策详情

### 为什么拆分 library 和 provider

Go embed library 和 Wails provider 是两个不同职责：

1. Go embed library 服务 Go 代码 import 和 `assetserver.Options{Assets: ...}`。
2. Wails provider 服务 Bazel 构建图中的 app 聚合与元数据传递。

拆分后，调用方能明确表达依赖关系：

```text
Go 代码 -> assets_lib
wails_app -> assets
```

不再需要知道宏内部生成了 `:assets_provider`。

### 为什么移除 `assets_dir`

`assets_dir` 当前没有被 `wails_app` 用来构建二进制。暴露它会让调用方传递 `:assets_lib_stage`，把内部实现细节变成公共 API。移除后 provider 只保留实际语义：Go library 和 importpath。

### 为什么 `wails_asset_library` 直接引用 frontend target

`//projects/game/desktop/frontend:dist` 已经是一个目录 artifact target。额外 `filegroup` 只是重命名，没有增加封装、校验或跨包复用价值。直接引用更短，也更清楚地表达资产来源。

### 为什么 Vite 配置文件要显式参数化

`package.json`、`index.html`、`vite.config.ts`、`tsconfig.json`、`svelte.config.js` 是构建入口或配置，不应和普通源码一起塞进 `srcs`。显式参数能让 BUILD 文件表达构建模型，减少规则对输入顺序的依赖。

### 为什么仍保留 Vite

`design/game/desktop-client.md` 选择了 Svelte + TypeScript + Vite。当前优化目标是让 Vite 构建被 Bazel 正确管理，而不是替换前端工具链。是否去 Vite 化属于另一项技术选型变更。

## 验证方案

完成实现后应执行：

```bash
bazel build //projects/game/desktop/frontend:dist
bazel build //projects/game/desktop/assets:assets_lib
bazel build //projects/game/desktop/assets:assets
bazel build //projects/game/desktop:desktop
bazel test //tools/release/wails/...
```

同时检查：

```bash
git status --short --ignored=matching projects/game/desktop/frontend
```

预期：

1. 不再出现未跟踪的 `projects/game/desktop/frontend/dist/`。
2. 不再出现 `projects/game/desktop/frontend/bazel-out/`。
3. `node_modules/` 仍可能作为本地依赖目录被忽略；本方案不处理依赖安装封闭性。

## 未来规划

1. 将 frontend 的 npm/pnpm 依赖也纳入 Bazel action 输入，减少对源码目录 `node_modules/` 的依赖。
2. 为 `vite_build` 增加可选 `mode`、`define`、`env` 输入，支持多环境前端构建。
3. 如后续 `wails_app` 需要自动连接 Go assets library，可让 `WailsAssetsInfo.library` 参与 app binary 依赖聚合，进一步减少调用方手写 Go deps。
