# Contract: vite_build 在 React 前端包上的使用契约

**Feature**: [050-vite-react-bazel](../spec.md) | 宪法 §III（接口优先设计）

这是本 feature 对外的核心接口：**任意 vite + React 前端包接入仓库 bazel 构建的消费契约**。`specs/049-agent-v2-dsh-init` 的 web 前端是第一个计划中的后续消费方；demo（`experimental/js/vite_react_demo/`）是该契约的活样板。

规则本体：`tools/dev/js/vite.bzl`（`vite_build`，本 feature **零修改**——`research.md` D1）。本契约固定消费方式，非规则实现。

## 接口：`vite_build` target 声明（React 项目形态）

```python
load("@npm//:defs.bzl", "npm_link_all_packages")
load("//tools/dev/js:vite.bzl", "vite_build")

npm_link_all_packages(name = "node_modules")

vite_build(
    name = "dist",
    srcs = glob(["src/**"]),          # .tsx/.ts/.css 等全部源文件
    config = "vite.config.ts",        # 内含 plugins: [react()]
    index_html = "index.html",
    package_json = "package.json",
    tsconfig = "tsconfig.json",       # jsx: "react-jsx"
    visibility = ["//visibility:public"],
)
```

**属性约定**：

| 属性 | React 项目取值 | 说明 |
|------|----------------|------|
| `srcs` | `glob(["src/**"])` | 必须覆盖全部参与构建的源（.tsx/.css 等）；遗漏即沙盒内缺文件失败 |
| `config` | `vite.config.ts` | React 差异全部承载于此（`@vitejs/plugin-react` 插件），规则属性不感知框架 |
| `svelte_config` | **不传** | Svelte 专属可选属性；React 项目省略 |
| `out` | 缺省（`dist`） | 产物 tree artifact 名 |

**前置条件**（包级）：

1. 包是 pnpm workspace 成员（`pnpm-workspace.yaml` packages 覆盖）；
2. 包内已声明 `npm_link_all_packages(name = "node_modules")`；
3. React 工具链依赖（react/react-dom/@vitejs/plugin-react/@types/*）在 `package.json` 中声明为 `catalog:`（`AGENTS.md` TS/JS 依赖规则；catalog 条目已由本 feature 落地，见 `research.md` D2/D3）。

**输出**：`DefaultInfo(files)` = 单个 tree artifact（dist 目录），结构与断言约束见 [dist-artifact-assertions.md](dist-artifact-assertions.md)；下游规则以 `:dist` label 整体消费（同 desktop 前端产物消费方式，`projects/game/desktop/assets/BUILD.bazel` 先例）。

**执行语义**（继承规则现状，非本 feature 变更）：本地执行（`execution_requirements = {"local": ""}`，依赖源码树 node_modules）；产物直接写入 bazel 声明的输出目录（`--outDir` + `--emptyOutDir`）。

## 接口：组件单测 target（React 项目形态）

沿用 `vitest_test` 宏（`tools/dev/js/vitest_test.bzl`），React 特有约定：

```python
vitest_test(
    name = "lib_test",
    data = glob(["src/**"]) + [
        "tsconfig.json",
        ":node_modules/react",
        ":node_modules/react-dom",
        ":node_modules/@testing-library/react",
        ":node_modules/@testing-library/dom",
        ":node_modules/jsdom",
        # ":node_modules/vitest" 由宏自动注入，勿手写
    ],
)
```

| 约定 | 内容 | 依据 |
|------|------|------|
| `data` 镜像 | 必须列入组件测试运行所需的全套 `:node_modules/*` 条目 | `style/javascript.md` §js_test 执行模型（丢失即 runfiles `Cannot find package`） |
| DOM 环境 | 需 DOM 的测试文件顶部声明 `// @vitest-environment jsdom`（per-file） | `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 不传环境选项，docblock 是唯一 per-file 机制） |
| 源码形态 | data 传原始 `.ts/.tsx` 源，不传任何预编译产物 | 同上契约（模块单实例不变式） |
| `tsconfig.json` 入 data | React 项目 MUST 将包内 `tsconfig.json` 一并列入 `data` | shim（`tools/dev/js/run_vitest.mjs`）不设 `root`，其驱动的 vite 实例 `root` 缺省为 `process.cwd()`（[vite `root` 默认值](https://v6.vite.dev/config/shared-options.html#root)）——js_test 进程 cwd 是 runfiles workspace 根，该层不存在任何项目 config，vitest 以零项目配置运行；`.tsx` 转译经 vite 的 esbuild 管线（[vite `esbuild` 选项](https://v6.vite.dev/config/shared-options.html#esbuild)），esbuild 从被转译文件的最近父目录发现 `tsconfig.json` 并读取 `"jsx": "react-jsx"`（automatic runtime；[esbuild tsconfig 字段与发现规则](https://esbuild.github.io/content-types/#tsconfig-json)）；缺失时按 classic runtime 发射且不自动引入 React（[esbuild JSX auto-import](https://esbuild.github.io/content-types/#auto-import-for-jsx)），运行期报 `ReferenceError: React is not defined`。实证记录：`experimental/js/vite_react_demo/BUILD.bazel` 注释 |

## 兼容性边界

- 存量 Svelte 消费方（`projects/game/desktop/frontend`）的声明与行为不受本契约影响（FR-005；契约只新增 React 形态约定，不改规则属性语义）。
- catalog 的 React 条目版本组合（plugin-react 5.x ↔ vite ^6.4.2、react 18.3.x）是本契约的隐式前提；catalog 升级须重验 peer 兼容（`research.md` 版本汇总表）。

## 消费方

- demo：`experimental/js/vite_react_demo/`（本 feature 交付，作为契约实证）
- 计划中：`specs/049-agent-v2-dsh-init` web 前端（按本契约新建包，SC-005）
