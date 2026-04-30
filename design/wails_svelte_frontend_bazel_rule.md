# Wails Svelte 前端 Bazel 构建宏方案

## 目标

本方案用于将 `projects/game/windows_agent/frontend` 中已经可工作的 Svelte/Vite Bazel 构建模式抽象为仓库内可复用的 Wails 前端构建宏，减少项目侧 `BUILD.bazel` 的样板代码，同时保持 Bazel target 的可见性和 Wails asset handoff 的可验证性。

本方案希望达成的效果是：

* `projects/game/windows_agent/frontend/BUILD.bazel` 只表达“这个包有一个 Wails Svelte 生产构建目标”，不再手写 Vite entry shim、`js_binary` 和 `js_run_binary` 组合。
* 生产构建目标仍然是显式 Bazel target，例如 `//projects/game/windows_agent/frontend:dist`，并继续作为 `wails_asset_library(src = ...)` 的输入。
* 前端测试 target 不隐藏在构建宏内部，继续作为 `//projects/game/windows_agent/frontend:vitest_test` 这样的显式 target 暴露给开发者、CI 和 `bazel query`。
* `package.json`、`tsconfig.json`、`vite.config.ts`、`index.html` 等特殊文件作为宏的强类型属性传入，不依赖 `srcs` 中是否恰好包含同名文件这种弱约束。
* 最终依赖链仍然可查询：`windows_agent_win_zip -> windows_agent_app -> windows_agent_assets_provider -> frontend:dist`。

本方案是 `design/windows_agent_frontend_bazel_wails_build.md` 和 `design/wails_bazel_terminal_build.md` 的后续抽象方案。前者描述 Windows agent 前端 Bazel 化的目标链路，后者描述 Wails asset、Go binary 和 app 的终态构建模型；本文只设计 Svelte/Vite 前端构建宏。

## 范围

本方案覆盖：

* 新宏的职责边界、API 和展开模型。
* 新宏在 `tools/release/wails` 中的代码分层。
* `projects/game/windows_agent/frontend/BUILD.bazel` 的目标形态。
* 与 `wails_asset_library`、`wails_app`、pnpm workspace 和 `aspect_rules_js` 的关系。
* 验收标准、迁移步骤和风险规避。

本方案不包括：

* Wails CLI 的 `wails build` 或 `wails dev` 封装。
* Svelte/Vite dev server、热重载和 browser preview target。
* 前端 UI/UX、业务逻辑或测试用例内容调整。
* Go/Wails binary、Windows resource、portable zip 的规则改造。
* `wails_asset_library` 的 asset staging 行为改造。
* 通用非 Wails 前端规则集建设；如果未来出现多个非 Wails Vite 项目，再单独抽象到 `tools/dev/frontend`。

## 当前状态

`projects/game/windows_agent/frontend/BUILD.bazel` 当前已能完成 Bazel 构建，核心结构如下：

```starlark
load("@aspect_rules_js//js:defs.bzl", "js_binary", "js_run_binary", "js_test")
load("@npm//:defs.bzl", "npm_link_all_packages")

npm_link_all_packages()

filegroup(
    name = "srcs",
    srcs = [
        "index.html",
        "package.json",
        "tsconfig.json",
        "vite.config.ts",
    ] + glob(["src/**"]),
)

genrule(
    name = "vite_build_entry",
    outs = ["vite_build_entry.mjs"],
    cmd = "printf '%s\\n' \"import './node_modules/vite/bin/vite.js';\" > $@",
)

js_binary(
    name = "vite_build",
    data = [":node_modules"],
    entry_point = ":vite_build_entry",
)

js_run_binary(
    name = "dist",
    srcs = [
        ":node_modules",
        ":srcs",
    ],
    args = ["build"],
    chdir = package_name(),
    out_dirs = ["dist"],
    tool = ":vite_build",
)
```

该模式可以工作，但存在以下问题：

1. Vite entry shim、`js_binary` 和 `js_run_binary` 的组合是 Wails/Svelte 前端构建的固定样板，项目侧重复暴露实现细节。
2. `filegroup(name = "srcs")` 把 `package.json`、`tsconfig.json`、`vite.config.ts`、`index.html` 和普通源码混在一起，无法从 BUILD API 上表达这些文件是构建边界的特殊输入。
3. 如果后续多个 Wails Svelte 前端复用该模式，容易产生细节漂移，例如 `chdir`、`out_dirs`、entry shim、visibility 或 node_modules 输入不一致。

当前端到 Wails app 的产物链已经正确：

```text
//projects/game/windows_agent/frontend:dist
  -> //projects/game/windows_agent/assets:windows_agent_assets_stage
  -> //projects/game/windows_agent/assets:windows_agent_assets_provider
  -> //projects/game/windows_agent:windows_agent_app
  -> //projects/game/windows_agent:windows_agent_win_zip
```

因此本方案只抽象 `frontend:dist` 的生成方式，不改变下游 asset handoff。

## 模型设计

### 职责模型

新宏命名为 `wails_svelte_frontend`，它只负责生产构建：

```text
Svelte/Vite source + config files + node_modules
  -> Vite build action
  -> declared tree artifact dist/**
```

它不负责：

```text
Vitest test target
dist -> frontend_dist re-root
Go //go:embed assets.go generation
Wails app binary
portable zip
```

对应职责边界：

| 层 | 目标 | 职责 |
| --- | --- | --- |
| frontend build | `wails_svelte_frontend(name = "dist", ...)` | 执行 `vite build`，产出 `dist` tree artifact |
| frontend test | 显式 `js_test(name = "vitest_test", ...)` | 执行 Vitest，测试内容在 BUILD 中可见 |
| asset handoff | `wails_asset_library` | 将 `dist` staged 为 Go embed 可用的 `frontend_dist` |
| Wails app | `wails_app` | 组合 Go library、assets provider 和 Windows binary |
| package | `windows_agent_package` | 打包 portable zip |

### API 模型

推荐 public macro API：

```starlark
wails_svelte_frontend(
    name,
    index,
    package_json,
    tsconfig,
    vite_config,
    srcs,
    node_modules,
    out_dir = "dist",
    args = ["build"],
    visibility = None,
    **kwargs
)
```

参数含义：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | 产物 target 名称，Windows agent 中为 `dist` |
| `index` | label/string | 是 | Vite HTML 入口，通常为 `index.html` |
| `package_json` | label/string | 是 | 前端 package manifest，影响依赖和 Vite 行为 |
| `tsconfig` | label/string | 是 | TypeScript 配置，作为构建输入显式建模 |
| `vite_config` | label/string | 是 | Vite 配置，作为构建输入显式建模 |
| `srcs` | list[label/string] | 是 | 普通前端源码，例如 `glob(["src/**"])` |
| `node_modules` | label | 是 | `npm_link_all_packages()` 生成的 node_modules target |
| `out_dir` | string | 否 | Vite 输出目录，默认 `dist` |
| `args` | list[string] | 否 | 传给 Vite binary 的参数，默认 `build` |
| `visibility` | list[string] | 否 | 应传递给最终产物 target |
| `**kwargs` | dict | 否 | 透传给最终 `js_run_binary` 的小范围扩展，例如 tags |

关键点：

* `index`、`package_json`、`tsconfig`、`vite_config` 必须是独立参数，不能要求调用方把它们放进 `srcs`。
* `srcs` 只表达普通源文件，不承担特殊文件约定。
* `node_modules` 由调用方显式传入。宏不调用 `npm_link_all_packages()`，避免在宏中隐式创建 repository-generated targets。
* `args` 默认只支持生产构建，不承担 dev server 语义。

### 展开模型

调用：

```starlark
wails_svelte_frontend(
    name = "dist",
    index = "index.html",
    package_json = "package.json",
    tsconfig = "tsconfig.json",
    vite_config = "vite.config.ts",
    srcs = glob(["src/**"]),
    node_modules = ":node_modules",
    visibility = [
        "//projects/game/windows_agent:__pkg__",
        "//projects/game/windows_agent/assets:__pkg__",
    ],
)
```

展开为等价内部 targets：

```starlark
genrule(
    name = "dist_vite_entry",
    outs = ["dist_vite_entry.mjs"],
    cmd = "printf '%s\\n' \"import './node_modules/vite/bin/vite.js';\" > $@",
)

js_binary(
    name = "dist_vite_build",
    data = [":node_modules"],
    entry_point = ":dist_vite_entry",
)

js_run_binary(
    name = "dist",
    srcs = [
        "index.html",
        "package.json",
        "tsconfig.json",
        "vite.config.ts",
        ":node_modules",
    ] + glob(["src/**"]),
    args = ["build"],
    chdir = package_name(),
    out_dirs = ["dist"],
    tool = ":dist_vite_build",
    visibility = [...],
)
```

实际实现中应使用 `native.package_name()`，因为宏文件不在调用包内。

### 测试模型

测试 target 不由 `wails_svelte_frontend` 生成。

`frontend/BUILD.bazel` 应继续显式声明 Vitest 相关 target，例如：

```starlark
genrule(
    name = "vitest_entry",
    outs = ["vitest_entry.mjs"],
    cmd = "printf '%s\\n' \"import './node_modules/vitest/vitest.mjs';\" > $@",
)

js_test(
    name = "vitest_test",
    args = ["run"],
    data = [
        "vitest.config.ts",
        ":node_modules",
        ":frontend_test_srcs",
    ],
    entry_point = ":vitest_entry",
)
```

原因：

1. 测试内容属于项目质量边界，不应隐藏在生产构建宏中。
2. 显式 target 便于 `bazel query //projects/game/windows_agent/frontend:all` 和 review 直接发现测试入口。
3. 不同前端项目可能使用不同测试框架、DOM 环境或 test data，生产构建宏不应固化测试策略。

如果未来需要减少 Vitest 样板，应单独设计 `vitest_test` 宏，而不是让 `wails_svelte_frontend` 顺手生成测试 target。

## 代码分层

### 规则位置

推荐放在已有 Wails rule set 下：

```text
tools/release/wails/
├── defs.bzl
└── private/
    └── frontend.bzl
```

原因：

* 当前用途是 Wails 前端生产资产，不是通用 Web app 构建工具。
* 该宏产物的主要消费者是 `wails_asset_library`。
* `tools/release/wails` 已经是 Wails 构建语义的公共 API 位置。
* 保持与 `wails_asset_library`、`wails_app`、`wails_go_binary` 相同的 public `defs.bzl` re-export 模式。

`tools/release/wails/defs.bzl` 增加：

```starlark
load("//tools/release/wails/private:frontend.bzl", _wails_svelte_frontend = "wails_svelte_frontend")

wails_svelte_frontend = _wails_svelte_frontend
```

`tools/release/wails/private/frontend.bzl` 加载：

```starlark
load("@aspect_rules_js//js:defs.bzl", "js_binary", "js_run_binary")
```

### 是否需要 provider

本方案不新增 provider。

`wails_svelte_frontend` 的最终产物仍是 `js_run_binary` 的 `DefaultInfo.files`，包含 declared directory `dist`。下游 `wails_asset_library(src = "//...:dist")` 已按 DefaultInfo 消费 directory artifact。

新增 provider 会让 `wails_asset_library` 需要支持新类型，扩大改动范围，但不能提升当前功能的可验证性。

### 是否需要 helper binary

本方案不新增 Go helper binary。

当前 Vite build 只需要 `js_binary` 和 `js_run_binary`。已有 `tools/release/wails/helpers` 负责 asset staging、`assets.go` 和 Windows resources；前端 build 不需要额外文件复制或重根逻辑。

## BUILD 目标形态

迁移后 `projects/game/windows_agent/frontend/BUILD.bazel` 推荐形态：

```starlark
# keep
load("@aspect_rules_js//js:defs.bzl", "js_test")
load("@npm//:defs.bzl", "npm_link_all_packages")
load("//tools/release/wails:defs.bzl", "wails_svelte_frontend")

npm_link_all_packages()

wails_svelte_frontend(
    name = "dist",
    index = "index.html",
    node_modules = ":node_modules",
    package_json = "package.json",
    srcs = glob(["src/**"]),
    tsconfig = "tsconfig.json",
    vite_config = "vite.config.ts",
    visibility = [
        "//projects/game/windows_agent:__pkg__",
        "//projects/game/windows_agent/assets:__pkg__",
    ],
)

filegroup(
    name = "frontend_test_srcs",
    srcs = [
        "package.json",
        "tsconfig.json",
        "vite.config.ts",
        "vitest.config.ts",
    ] + glob(["src/**"]),
)

genrule(
    name = "vitest_entry",
    outs = ["vitest_entry.mjs"],
    cmd = "printf '%s\\n' \"import './node_modules/vitest/vitest.mjs';\" > $@",
)

js_test(
    name = "vitest_test",
    args = ["run"],
    data = [
        "vitest.config.ts",
        ":frontend_test_srcs",
        ":node_modules",
    ],
    entry_point = ":vitest_entry",
)
```

说明：

* `dist` 是生产构建 target。
* `vitest_test` 是测试 target，显式保留。
* `frontend_test_srcs` 可按测试需要包含 `vitest.config.ts`，与生产构建输入分开。
* 生产构建不把 `vitest.config.ts` 作为输入，避免测试配置变化导致生产构建 cache 失效。

## 关键细节

### 特殊文件强约束

宏 API 必须强制调用方传入：

```starlark
index = "index.html"
package_json = "package.json"
tsconfig = "tsconfig.json"
vite_config = "vite.config.ts"
```

这样 BUILD 文件能明确表达这些文件的角色：

* `index` 是 Vite HTML entry。
* `package_json` 是包边界和依赖声明。
* `tsconfig` 是 TypeScript/Svelte 编译上下文。
* `vite_config` 是 Vite 构建配置。

宏不应通过以下方式弱约束：

```starlark
srcs = glob(["**"])
```

或要求 `srcs` 中包含固定文件名。弱约束会让调用方遗漏特殊文件时只在运行时失败，且 review 无法看出构建边界。

### `node_modules` 显式传入

`npm_link_all_packages()` 由 `@npm//:defs.bzl` 生成，它在 BUILD 包中创建 `:node_modules` 及子 targets。

宏不应在内部调用：

```starlark
npm_link_all_packages()
```

原因：

* macro 内部隐藏 target 创建会降低 BUILD 文件可读性。
* 多个宏调用可能产生重复 target 名称。
* node_modules 作为构建依赖边界，应在项目 BUILD 中显式出现。

### `chdir` 固化为调用包

Vite 需要在前端包目录下找到 `index.html`、`vite.config.ts` 和 `package.json`。宏内部应设置：

```starlark
chdir = native.package_name()
```

这延续当前可工作的 `frontend:dist` 行为。

### `out_dirs` 固化为 declared directory

`js_run_binary(out_dirs = [out_dir])` 使用 declared directory 表达 Vite 输出。下游 `wails_asset_library` 已要求 `src` 提供 exactly one directory artifact。

宏不应把 Vite 输出写入源码目录 `frontend/dist`，也不应通过 `genrule` copy 回源码树。

### Vite entry shim

当前工作实现通过 generated entry shim 调用：

```javascript
import './node_modules/vite/bin/vite.js';
```

宏可以先保留该方式，最小化行为变化。后续若验证 `@npm//:vite/package_json.bzl` 提供的 `vite_binary` 在本仓库同样稳定，可再单独替换；不应在本次抽象中同时改变执行入口和 BUILD API。

### 不纳入 Vitest

生产构建宏不接受 `vitest_config`、`test_srcs`、`test_name` 等参数。

如果宏提供 `test_name` 这类参数，会让测试 target 变成构建宏的副产物，违背“测试内容不要封装在 build 文件内，以防不知道有测试 target”的要求。

### 与 pnpm workspace 的关系

该宏不管理 npm 依赖版本。依赖仍由：

* 根 `pnpm-workspace.yaml` 的 `catalog` 管理版本。
* 前端 `package.json` 使用 `catalog:` 引用。
* `MODULE.bazel` 的 `npm_translate_lock(pnpm_lock = "//:pnpm-lock.yaml")` 暴露 `@npm`。

新增依赖时仍按仓库规范执行 Bazel-managed pnpm，而不是手动编辑 lockfile。

## 决策详情

### 决策 1：实现为 macro，不实现为原生 rule

原因：

* 需要组合 `genrule`、`js_binary`、`js_run_binary` 多个现有 rules。
* Bazel rule implementation 不能动态创建其他 rules。
* 现有 `wails_asset_library`、`wails_app`、`wails_go_binary` 也使用 public macro 组合规则。

### 决策 2：放在 `tools/release/wails`

原因：

* 该宏面向 Wails 生产前端资产，而不是所有 Vite 项目。
* 当前唯一消费者是 `wails_asset_library`。
* 与 Wails 规则 public API 保持统一，调用方只需 load `//tools/release/wails:defs.bzl`。

### 决策 3：不封装测试

原因：

* 测试 target 必须在 BUILD 中显式可见。
* 测试输入、DOM 环境、setup 文件和 matcher 可能随项目前端变化，不属于生产构建语义。
* 将 test target 隐藏在 build macro 中会降低 query、review 和 CI target 管理的透明度。

### 决策 4：特殊文件作为强属性

原因：

* `package.json`、`tsconfig.json`、`vite.config.ts`、`index.html` 是构建模型的一部分，不是普通源码。
* 强属性能让 BUILD 文件表达意图，也让遗漏在 analysis 或 action 输入层更容易定位。
* 避免通过文件名约定或 `glob` 隐式依赖这些文件。

### 决策 5：不改变下游 asset handoff

原因：

* `wails_asset_library` 已经能消费 `frontend:dist` directory artifact。
* 当前查询链路和 zip 构建已经验证通过。
* 本方案目标是减少前端 build target 样板，不重新设计 Go embed 交接。

## 迁移步骤

### Step 1：新增宏实现

1. 新增 `tools/release/wails/private/frontend.bzl`。
2. 实现 `wails_svelte_frontend` macro。
3. 在宏内部创建 `<name>_vite_entry`、`<name>_vite_build` 和最终 `<name>` target。
4. 对最终 `<name>` target 传递 `visibility` 和必要 `kwargs`。

### Step 2：导出 public API

1. 更新 `tools/release/wails/defs.bzl`。
2. re-export `wails_svelte_frontend`。

### Step 3：迁移 Windows agent frontend BUILD

1. 在 `projects/game/windows_agent/frontend/BUILD.bazel` 中加载 `wails_svelte_frontend`。
2. 用 `wails_svelte_frontend(name = "dist", ...)` 替换当前 Vite build 相关 `filegroup`、`genrule`、`js_binary`、`js_run_binary`。
3. 保留并显式维护 `vitest_test`。
4. 可将测试输入整理为 `frontend_test_srcs`，但不要由 `wails_svelte_frontend` 生成测试 target。

### Step 4：验证目标图

执行：

```bash
bazel query //projects/game/windows_agent/frontend:all
```

确认至少能看到：

```text
//projects/game/windows_agent/frontend:dist
//projects/game/windows_agent/frontend:vitest_test
```

执行：

```bash
bazel query 'somepath(//projects/game/windows_agent:windows_agent_win_zip, //projects/game/windows_agent/frontend:dist)'
```

确认最终 zip target 仍依赖 `frontend:dist`。

### Step 5：验证构建和测试

执行：

```bash
bazel build //projects/game/windows_agent/frontend:dist
bazel test //projects/game/windows_agent/frontend:vitest_test
bazel build //projects/game/windows_agent:windows_agent_win_zip
```

如果改动影响 `tools/release/wails` 的加载或宏展开，也应执行：

```bash
bazel query //tools/release/wails/...
```

## 验收标准

### BUILD API 验收

* `projects/game/windows_agent/frontend/BUILD.bazel` 使用 `wails_svelte_frontend(name = "dist", ...)` 表达生产构建。
* `index`、`package_json`、`tsconfig`、`vite_config` 均为显式参数。
* `srcs` 只承载普通源码，不依赖固定文件名隐式包含特殊文件。
* `npm_link_all_packages()` 和 `node_modules = ":node_modules"` 在 BUILD 中显式存在。
* `vitest_test` 仍作为显式 target 存在，不由 `wails_svelte_frontend` 生成。

### 构建验收

* `bazel build //projects/game/windows_agent/frontend:dist` 成功。
* `bazel build //projects/game/windows_agent:windows_agent_win_zip` 成功。
* `bazel query 'somepath(//projects/game/windows_agent:windows_agent_win_zip, //projects/game/windows_agent/frontend:dist)'` 能返回有效路径。

### 测试验收

* `bazel test //projects/game/windows_agent/frontend:vitest_test` 成功。
* `bazel test //projects/game/windows_agent/...` 仍包含前端测试 target。
* `bazel query //projects/game/windows_agent/frontend:all` 能直观看到 `vitest_test`。

### 仓库状态验收

* 不新增或提交 `projects/game/windows_agent/frontend/dist/`。
* 不新增或提交 `projects/game/windows_agent/assets/frontend_dist`。
* 不新增子包级 `pnpm-lock.yaml`。
* 不新增 npm 依赖；如未来新增，必须通过 Bazel-managed pnpm 更新。

## 风险与规避

### 风险 1：宏隐藏测试 target

表现：开发者只看到 `wails_svelte_frontend`，不知道是否存在前端测试。

规避：本方案明确禁止宏生成测试 target；测试 target 必须在 `BUILD.bazel` 中显式声明。

### 风险 2：特殊文件弱约束导致遗漏

表现：调用方只传 `srcs = glob(["src/**"])`，漏掉 `vite.config.ts` 或 `tsconfig.json`，构建在运行时才失败或 cache key 不完整。

规避：宏 API 强制 `index`、`package_json`、`tsconfig`、`vite_config` 独立传入。

### 风险 3：宏改变 Vite 工作目录

表现：Vite 找不到 `index.html` 或配置文件。

规避：宏固定 `chdir = native.package_name()`，保持当前已验证行为。

### 风险 4：out_dir 与 asset handoff 不匹配

表现：`wails_asset_library` 提示 src 未提供 exactly one directory artifact，或 Wails runtime 找不到 `index.html`。

规避：继续使用 `js_run_binary(out_dirs = ["dist"])`，并通过 `bazel build frontend:dist` 与 zip query 验证。

### 风险 5：抽象过度成为通用前端框架

表现：宏开始支持 dev server、测试、SSR、SvelteKit adapter、多个 bundler，导致 API 复杂且责任不清。

规避：本宏只支持 Wails Svelte/Vite production build。未来需求单独设计，不在本宏内扩张。

## 未来规划

* 如果出现第二个 Wails Svelte/Vite 前端，复用 `wails_svelte_frontend` 并根据实际差异调整参数。
* 如果出现非 Wails Vite 前端，再评估将通用 Vite build 抽象迁移到 `tools/dev/frontend`，由 `tools/release/wails` 只保留 Wails-specific wrapper。
* 如果前端测试样板在多个项目中重复，再单独设计显式 `vitest_test` 宏，仍保持测试 target 在 BUILD 文件中清晰可见。
* 如果需要支持 `wails dev`，另行设计 dev server/watch target，不纳入生产构建宏。
