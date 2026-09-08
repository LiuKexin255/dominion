# Tasks: vite + React Bazel 打包支持与验证 Demo

**Input**: Design documents from `/specs/050-vite-react-bazel/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md（均位于 `specs/050-vite-react-bazel/`）

**Organization**: 按 user story 组织（spec.md US1/US2/US3/US4）。构建与测试验证按宪法 IV 内联于各任务（不单列 task）；US3/US4 为验证型 story，其验收作为 story 验收 task。

**Format**: `- [ ] [ID] [P?] [Story?] Description`（P = 可并行：不同文件且无未完成依赖）

---

## Phase 1: Setup（catalog 依赖与 demo 包骨架）

**Purpose**: React 工具链进入 catalog、demo 包成为 workspace 成员、manifests 就绪。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：
  - `style/javascript.md`（依赖治理与测试约定；注意：其"模块系统"节的 nodenext/`.js` 扩展名规则是**服务侧 Node ESM 包**约束，vite 前端包沿 `projects/game/desktop/frontend/tsconfig.json` 的 bundler 模式，不受该节约束）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 声明的 JS/TS 规范基准，TS/TSX 代码命名与书写约定）
- **官方文档**：
  - [@vitejs/plugin-react README](https://github.com/vitejs/vite-plugin-react/tree/main/packages/plugin-react)（`vite.config.ts` 的 `react()` 用法；peer 兼容结论见 research.md D2）
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/research.md`（D2/D3 版本决策、D6/D7 布局与内容形态）
  - `specs/050-vite-react-bazel/plan.md`（Project Structure 文件树）
  - `specs/050-vite-react-bazel/contracts/vite-build-target.md`（包级前置条件：catalog/workspace/node_modules）
  - `projects/game/desktop/frontend/package.json`、`projects/game/desktop/frontend/tsconfig.json`（manifest/tsconfig 样板）

- [X] T001 更新根 `pnpm-workspace.yaml`：`packages` 列表新增 `experimental/js/*`；`catalog` 新增 8 条目——`react: ^18.3.1`、`react-dom: ^18.3.1`、`@types/react: ^18.3.0`、`@types/react-dom: ^18.3.0`、`@vitejs/plugin-react: ^5.0.0`、`@testing-library/react: ^16.0.0`、`@testing-library/dom: ^10.0.0`、`jsdom: ^26.0.0`（版本依据 `specs/050-vite-react-bazel/research.md` D2/D3：plugin-react 5.x peer 覆盖 catalog vite ^6.4.2；react 18.3.x 对齐 049 组件库 peer ^18.2.0；条目按字母序插入现有 catalog）
- [X] T002 [P] 创建 `experimental/js/vite_react_demo/package.json`（name `@dominion/experimental-vite-react-demo`、private、`type: module`；dependencies：`react`、`react-dom` 全 `catalog:`；devDependencies：`@vitejs/plugin-react`、`@types/react`、`@types/react-dom`、`@testing-library/react`、`@testing-library/dom`、`jsdom`、`vite`、`vitest`、`typescript` 全 `catalog:`——零直接版本，SC-004）+ `experimental/js/vite_react_demo/tsconfig.json`（样板 `projects/game/desktop/frontend/tsconfig.json`：`module/moduleResolution: ESNext/bundler`、`noEmit`、strict，新增 `"jsx": "react-jsx"`、`lib: ["ESNext", "DOM", "DOM.Iterable"]`；include `src/**/*.ts`、`src/**/*.tsx`）+ `experimental/js/vite_react_demo/vite.config.ts`（仅 `plugins: [react()]`）+ `experimental/js/vite_react_demo/index.html`（`<div id="root">` + `<script type="module" src="/src/main.tsx">`）
- [X] T003 [P] 创建最小源码 `experimental/js/vite_react_demo/src/main.tsx`（`createRoot(document.getElementById("root")!).render(<App />)`，import App 无扩展名——bundler 模式）与 `experimental/js/vite_react_demo/src/App.tsx`（占位函数组件，渲染特征字符串 `dominion-vite-react-demo`）
- [X] T004 更新 lockfile：`bazel run @pnpm -- --dir /mnt/code/dominion up`；验证 `pnpm-lock.yaml` 新增 `experimental/js/vite_react_demo` importer、`@vitejs/plugin-react` 解析到 5.x 且 peer 无 vite 6 冲突、react 解析到 18.3.x（`specs/050-vite-react-bazel/research.md` 版本汇总表逐项核对）；不手动编辑 lockfile（AGENTS.md）

---

## Phase 2: User Story 1 — bazel 构建 React 项目 (Priority: P1) 🎯 MVP

**Goal**: demo 以仓库标准命令（bazel）构建成功，产出与 `vite_build` 既有形态兼容的 dist tree artifact。

**Independent Test**: `specs/050-vite-react-bazel/quickstart.md` 场景 1——`bazel build //experimental/js/vite_react_demo:dist` 成功且产物含入口 HTML 与 hash 命名资源。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：无（BUILD.bazel 编辑以契约与样板驱动，无专门 style 文档）
- **官方文档**：无（`vite_build` 用法以规则源码与契约为准）
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/contracts/vite-build-target.md`（target 声明契约：属性取值与前置条件）
  - `tools/dev/js/vite.bzl`（规则属性源码，确认零改动适用）
  - `projects/game/desktop/frontend/BUILD.bazel`（`vite_build` 消费样板）
  - `specs/050-vite-react-bazel/quickstart.md`（场景 1 验证步骤）

- [X] T005 [US1] 创建 `experimental/js/vite_react_demo/BUILD.bazel`：`load("@npm//:defs.bzl", "npm_link_all_packages")` + `load("//tools/dev/js:vite.bzl", "vite_build")` + `npm_link_all_packages(name = "node_modules")` + `vite_build(name = "dist", srcs = glob(["src/**"]), config = "vite.config.ts", index_html = "index.html", package_json = "package.json", tsconfig = "tsconfig.json", visibility = ["//visibility:public"])`（**不传 `svelte_config`**，属性逐项对齐 `specs/050-vite-react-bazel/contracts/vite-build-target.md`）；随后 `bazel run //:gazelle experimental/js/vite_react_demo`（gazelle 校正，如误生成多余 target 按契约形态修正——`AGENTS.md` BUILD 惯例）；构建验证（内联，宪法 IV）：`bazel build //experimental/js/vite_react_demo:dist` 成功；核对产物 tree artifact（`bazel-bin/experimental/js/vite_react_demo/dist/`）含 `index.html` 与 `assets/` 下 hash 命名的 `.js` 资源、入口引用无悬空（`specs/050-vite-react-bazel/quickstart.md` 场景 1）

**Checkpoint**: US1 完成——bazel 可构建 React 项目，产物形态与现有 `vite_build` 输出兼容（MVP 达成）。

---

## Phase 3: User Story 2 — demo 实证 React 真实参与构建 + 自动化验证 (Priority: P1)

**Goal**: demo 组件具备真实交互与特征字符串；组件单测（RTL/jsdom）与产物断言（sh_test）随 `bazel test` 执行并通过。

**Independent Test**: `specs/050-vite-react-bazel/quickstart.md` 场景 2/3——`:dist_assert_test` 与 `:lib_test` 全绿。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：
  - `style/javascript.md`（§测试：js_test 执行模型、`vitest_test` 宏的 data 规则、mock 约定；注意"模块系统"节的 nodenext/`.js` 扩展名为服务侧规则，前端 bundler 模式无扩展名导入沿 desktop 样板）
  - [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` 声明的 JS/TS 规范基准，TSX 组件与测试代码书写约定）
- **官方文档**：
  - [React Testing Library — Introduction](https://testing-library.com/docs/react-testing-library/intro/)（render/query 理念与安装面；peer `@testing-library/dom`）
  - [Vitest — Test Environment](https://vitest.dev/guide/environment)（`// @vitest-environment jsdom` per-file docblock 语法）
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/contracts/vite-build-target.md`（vitest_test 声明约定：data 镜像 + jsdom docblock）
  - `specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md`（A1–A4 断言清单与行为要求）
  - `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 契约：`startVitest` 仅传 `{watch: false}`、不传环境选项——per-file docblock 因此是唯一环境声明机制）
  - `experimental/ts/grpc_hello_world/smoke_test.sh` + `experimental/ts/grpc_hello_world/BUILD.bazel`（`sh_test` + `$(location)` + runfiles 先例）
  - `projects/game/desktop/frontend/BUILD.bazel`（`vitest_test` 用法样板）

- [X] T006 [US2] 完善 `experimental/js/vite_react_demo/src/App.tsx`：带状态交互组件（counter 按钮：`useState` 计数 + click 递增）+ 渲染特征字符串 `dominion-vite-react-demo`（`data-testid="demo-marker"` 容器）+ 计数展示 `data-testid="demo-count"`（`specs/050-vite-react-bazel/research.md` D7；特征字符串与 `dist_assert.sh` 单一来源约定见 contracts/dist-artifact-assertions.md 行为要求 4）
- [X] T007 [US2] 创建 `experimental/js/vite_react_demo/src/App.test.tsx`：首行 `// @vitest-environment jsdom` docblock；RTL `render(<App />)` 断言特征字符串可见（`getByTestId("demo-marker")`）+ `fireEvent.click` 计数递增断言（不引入 mock，纯渲染/交互断言——`style/javascript.md` mock 约定下无需 mock）
- [X] T008 [US2] 在 `experimental/js/vite_react_demo/BUILD.bazel` 追加 `load("//tools/dev/js:vitest_test.bzl", "vitest_test")` + `vitest_test(name = "lib_test", data = glob(["src/**"]) + ["tsconfig.json", ":node_modules/react", ":node_modules/react-dom", ":node_modules/@testing-library/react", ":node_modules/@testing-library/dom", ":node_modules/jsdom"], size = "small")`（data 镜像逐项对齐 `specs/050-vite-react-bazel/contracts/vite-build-target.md`，`tsconfig.json` 必入 data——esbuild 从被转译源文件最近父目录发现它以取 `"jsx": "react-jsx"`，缺省即 classic runtime 运行期崩溃，依据见契约约定表；`:node_modules/vitest` 由宏自动注入勿手写；`style/javascript.md` §js_test 执行模型）；`bazel test //experimental/js/vite_react_demo:lib_test` 通过（内联，宪法 IV）
- [X] T009 [P] [US2] 创建 `experimental/js/vite_react_demo/dist_assert.sh`：`set -euo pipefail`；argv 接收 dist 目录；逐条输出 `PASS/FAIL A1`–`A4`——A1 `index.html` 存在且非空、A2 HTML 内 `src=/href=` 引用的产物内相对资源全部存在（零悬空）、A3 `assets/` 下存在 `*-[hash].js`（hash 为 url-safe base64 段 `[0-9A-Za-z_-]`、默认 8 位——**非**十六进制）、A4 某 `.js` 资源内 grep 到特征字符串 `dominion-vite-react-demo` 与 React 运行时痕迹（react-dom 产物的 license banner 字面量 `react-dom.production.min.js`）（逐项对齐 `specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md`；仅 bash + coreutils，样板 `experimental/ts/grpc_hello_world/smoke_test.sh`）
- [X] T010 [US2] 在 `experimental/js/vite_react_demo/BUILD.bazel` 追加 `load("@rules_shell//shell:sh_test.bzl", "sh_test")` + `sh_test(name = "dist_assert_test", srcs = ["dist_assert.sh"], args = ["$(location :dist)"], data = [":dist"], deps = ["@bazel_tools//tools/bash/runfiles"], tags = ["local"])`（形态对齐 `experimental/ts/grpc_hello_world/BUILD.bazel:115`；`:dist` 为本地执行 target，沿 desktop 前端 `local` 惯例）；验证（内联，宪法 IV）：`bazel test //experimental/js/vite_react_demo:dist_assert_test //experimental/js/vite_react_demo:lib_test --test_output=all` 全绿，`dist_assert_test` 输出 `PASS A1`–`PASS A4`（`specs/050-vite-react-bazel/quickstart.md` 场景 2/3）；任一 FAIL 即修复后重跑至全绿（宪法 VI 全过标准）

**Checkpoint**: US1+US2 完成——React 真实参与构建且三类证据链（构建产物、断言测试、组件单测）自动化可重复。

---

## Phase 4: User Story 3 — 存量前端构建零回归 (Priority: P2)

**Goal**: desktop Svelte 前端的构建与测试在本变更后行为不变。**纯验证 phase：无代码编辑**；回归定位与修复回退至 Phase 1/2 任务。

**Independent Test**: `specs/050-vite-react-bazel/quickstart.md` 场景 4——desktop `:dist` 构建与 `:lib_test` 测试全绿。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：无（纯验证 phase，无代码编辑）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/quickstart.md`（场景 4 验证步骤与预期）

- [X] T011 [US3] 存量回归验证：`bazel build //projects/game/desktop/frontend:dist` 与 `bazel test //projects/game/desktop/frontend:lib_test` 全绿，产物结构与 feature 合入前一致（`specs/050-vite-react-bazel/quickstart.md` 场景 4；SC-003）。若出现回归，本 phase **不直接编辑修复**：定位至 Phase 1/2 的依赖或规则变更，回退至对应任务按该 phase 文档清单修复，再重跑本验证至全绿

**Checkpoint**: 全部 user story 完成（US3 为验证型 story，无新交付物）。

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 文档收尾与全场景终验。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/quickstart.md`（全部 6 场景终验脚本）
  - `specs/050-vite-react-bazel/contracts/vite-build-target.md`（README 中 049 消费指引的引用目标）
  - `projects/game/fake-llm/README.md`（宪法 VI 大型测试豁免声明先例）

- [X] T012 [P] 创建 `experimental/js/vite_react_demo/README.md`：demo 定位（vite + React bazel 构建实证）、构建/测试命令、前置条件说明（`bazel run @pnpm -- --dir /mnt/code/dominion` 安装 node_modules；缺少 node_modules 时构建失败的原因与解决方式——spec Edge Cases「本地执行环境差异」）、宪法 VI 大型测试豁免声明（非服务型构建基建，验收 = bazel build + bazel test，参照 `projects/game/fake-llm/README.md` 豁免先例）、049 web 前端的消费指引（链接 `specs/050-vite-react-bazel/contracts/vite-build-target.md`）
- [X] T013 终验：按 `specs/050-vite-react-bazel/quickstart.md` 场景 1–6 逐条执行——含 `bazel clean` 后可重复构建（场景 5）与 `grep` 依赖合规检查（场景 6：`experimental/js/vite_react_demo/package.json` 中 React 相关条目全部 `catalog:`）；全部通过后 feature 验收完成

---

## Phase 6: User Story 4 — 静态页面部署载体（服务实现）

**Purpose**: demo 交付可部署的静态页面服务：Go 静态服务器构建期内嵌 dist 产物，按仓库标准服务形态声明镜像与服务/部署配置。

**Goal**: `:server` 二进制内嵌 dist 并提供静态托管（go_test 覆盖）；`server/service.yaml` 与根 `deploy.yaml` 就绪（字段逐项对齐契约）。

**Independent Test**: `specs/050-vite-react-bazel/quickstart.md` 场景 7 前置——`bazel build //experimental/js/vite_react_demo/server:cmd_image` 成功、`bazel test //experimental/js/vite_react_demo/server:server_test` 通过。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：
  - `style/golang.md`（包引用三级分组、常量代替魔术字、单测表驱动与 given/when/then、非导出函数 `Test_funcName` 命名、gazelle 默认 target 名）
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` 引用的规范基石——clarity/simplicity/least mechanism 原则）
- **官方文档**：
  - [Go embed 包文档](https://pkg.go.dev/embed)（`//go:embed` 模式语义：相对包目录、禁止 `..`、目录递归嵌入、`all:` 前缀；embed.FS 实现 fs.FS 可直接服务 HTTP）
- **技术文章/技术参考文档**：
  - `specs/050-vite-react-bazel/contracts/static-server-deploy.md`（本 phase 核心契约：embed 库/服务 target/service.yaml/deploy.yaml 全部字段与行为约定）
  - `specs/050-vite-react-bazel/research.md`（D8 部署形态决策与机制依据）
  - `experimental/dsh/demo/fake-llm/cmd/main.go`（最小 Go HTTP 服务样板：flag → mux → `phttp.Handler` → `bootstrap.HTTPServer` + `otel.Component`）
  - `experimental/dsh/demo/fake-llm/cmd/BUILD.bazel` + `experimental/dsh/demo/fake-llm/BUILD.bazel`（go_binary + `artifact_pkg_go`/`artifact_image` 样板）
  - `experimental/dsh/demo/fake-llm/service.yaml`（service.yaml 字段样板：ports/artifacts/kind）
  - `tools/release/wails/private/assets.bzl`（`wails_asset_library` 机制：stage dist 进包 → 生成 `//go:embed all:frontend_dist` → go_library；默认 `out = "frontend_dist"`、`variable_name = "FrontendDist"`）
  - `projects/game/desktop/assets/BUILD.bazel`（`wails_asset_library(src = "//…:dist")` 消费先例）
  - `tools/release/deploy/README.md`（service.yaml/deploy.yaml schema、app/name 与 artifact_image 一致性校验、环境名格式）

- [X] T014 [US4] 创建部署载体：`experimental/js/vite_react_demo/server/assets/BUILD.bazel`——`load("//tools/release/wails:defs.bzl", "wails_asset_library")` + `wails_asset_library(name = "assets", src = "//experimental/js/vite_react_demo:dist", importpath = "dominion/experimental/js/vite_react_demo/server/assets", visibility = ["//experimental/js/vite_react_demo/server:__pkg__"])`（独立子目录与包级 visibility `:__pkg__` 语法依据见 research.md D8 与契约 §1.1）；`experimental/js/vite_react_demo/server/main.go`——包注释说明载体定位（静态托管 demo dist，`specs/050-vite-react-bazel/contracts/static-server-deploy.md`），`const embedDistDir = "frontend_dist"`（规则默认 out，`style/golang.md` 魔术字常量化），`newHandler() (http.Handler, error)`：`fs.Sub(assets.FrontendDist, embedDistDir)` → `http.FileServerFS` → `mux.Handle("/")`，main 仿 `experimental/dsh/demo/fake-llm/cmd/main.go`（`var port = flag.String("port", "8080", ...)` → `phttp.Handler(handler, "vite-react-demo")` → `bootstrap.HTTPServer("http", srv)` + `otel.Component()`）；`experimental/js/vite_react_demo/server/main_test.go`——`Test_newHandler` 表驱动 httptest（given：构造 handler + `fs.Glob` 取产物内首个 `assets/*.js` 路径；when/then：`GET /` 200 且 body 含 `/assets/` 引用、`GET /index.html` 200 且含 `<div id="root">`、`GET {资产路径}` 200、`GET /missing.js` 404；无 mock、无外部依赖，`style/golang.md` §单元测试）；`experimental/js/vite_react_demo/server/BUILD.bazel`——顶部声明 `# gazelle:resolve go dominion/experimental/js/vite_react_demo/server/assets //experimental/js/vite_react_demo/server/assets:assets`（gazelle 无法自动解析生成型 embed 库的 importpath，先例 `projects/game/desktop/BUILD.bazel:6`），`bazel run //:gazelle experimental/js/vite_react_demo/server` 生成 `go_library("server_lib")`/`go_binary("server")`，单测按仓库惯例声明 `load("//tools/dev/go:defs.bzl", "go_unittest")` + `go_unittest(name = "server_test", srcs = ["main_test.go"], embed = [":server_lib"], deps = ["//experimental/js/vite_react_demo/server/assets"])`（go_test wrapper：注入 `-test.v`、默认 `size = "small"`，`style/golang.md` §单元测试），再追加 `load("//tools/release:defs.bzl", "artifact_image", "artifact_pkg_go")` + `artifact_pkg_go(name = "server_pkg", app = "vite-react-demo", binary = ":server", service = "server")` + `artifact_image(name = "cmd_image", app = "vite-react-demo", pkg = ":server_pkg", service = "server")`（字段逐项对齐契约 §1）；验证（内联，宪法 IV）：`bazel build //experimental/js/vite_react_demo/server:cmd_image` 成功且 `bazel test //experimental/js/vite_react_demo/server:server_test --test_output=all` 全绿
- [X] T015 [US4] 创建部署声明：`experimental/js/vite_react_demo/server/service.yaml`（`version "3.0"`、`name: server`、`app: vite-react-demo`、`kind: stateless`、`ports: [{name: http, port: 8080}]`、`artifacts: [{name: server, target: :cmd_image}]`）与 `experimental/js/vite_react_demo/deploy.yaml`（`name: vite.demo`、`type: prod`、services 引用上述 service.yaml + `http.hostnames: [vite-react-demo.liukexin.com]` + `matches: [{backend: http, path: {type: PathPrefix, value: /}}]`；type 语义与固定环境名依据 research.md D8）——字段逐项对齐 `specs/050-vite-react-bazel/contracts/static-server-deploy.md` §2.1/§2.2

**Checkpoint**: US4 实现完成——静态页面服务镜像可构建、单测通过、部署声明就绪。

---

## Phase 7: User Story 4 — 部署验证与文档收尾

**Purpose**: 部署能力验收（实际 deploy apply → 浏览器人工访问 → deploy del）与 README/quickstart 终态化收尾。

**Goal**: quickstart 场景 7 实际执行通过；demo README 表述部署交付与宪法 VI 豁免终态。

**Independent Test**: `specs/050-vite-react-bazel/quickstart.md` 场景 7——`deploy apply` 环境就绪、入口 URL 返回 demo 页面（入口 HTML/资产/特征内容可访问）、`deploy del` 清理成功。

**文档清单（本 phase 编码前必读）**：

- **代码规范文档**：
  - `style/large_test.md`（部署/测试计划仓库约定：部署配置与测试计划的目录约定边界、testplan 编排语境——本 feature 不建测试计划、部署入口为 deploy CLI 的规范依据）
- **官方文档**：无
- **技术文章/技术参考文档**：
  - `tools/release/deploy/README.md`（`deploy apply`/`del` 命令、`--timeout`/`-v` 全局参数、`//` 路径规则）
  - `tools/test/guitar/README.md`（guitar run 闭环语义——部署入口为 deploy CLI 而非 guitar 的能力边界依据）
  - `specs/050-vite-react-bazel/quickstart.md`（场景 7 验证步骤与预期）
  - `specs/050-vite-react-bazel/contracts/static-server-deploy.md`（README 部署章节与豁免表述的引用目标）
  - `specs/050-vite-react-bazel/spec.md`（FR-009 用户决策记录——README 豁免表述的引用来源）

- [ ] T016 [P] [US4] 更新 `experimental/js/vite_react_demo/README.md` 为部署交付终态：Targets 表新增 `:server`/`:cmd_image`；新增 "Deploying the demo" 章节（`bazel run //:deploy_install` 前置 → `deploy apply //experimental/js/vite_react_demo/deploy.yaml` → 访问 `https://vite-react-demo.liukexin.com/` → 清理 `deploy del vite.demo`；`type: prod` 仅表示免 header 直连路由模式的说明，见契约 §2.3）；"Large-test exemption (Constitution VI)" 章节按终态重写——交付静态页面部署载体（服务型，server 单测齐备）、部署能力已交付（quickstart 场景 7 实际执行验收）、web E2E 大型测试暂缓为用户决策（2026-08-27，依据 `specs/050-vite-react-bazel/spec.md` FR-009），依据宪法 VI 豁免条款作 README 说明；049 消费指引补服务载体形态（链接 `specs/050-vite-react-bazel/contracts/static-server-deploy.md`）
- [ ] T017 [US4] 部署验收：按 `specs/050-vite-react-bazel/quickstart.md` 场景 7 实际执行——前置 `bazel run //:deploy_install`；`deploy apply //experimental/js/vite_react_demo/deploy.yaml` 至环境 `vite.demo` 就绪；浏览器访问 `https://vite-react-demo.liukexin.com/` 人工验证页面渲染（`dominion-vite-react-demo` 特征文本 + counter 交互、assets 资源加载）；curl 等价断言（入口 200 含 `/assets/` 引用、产物资产 200）；验证完成后 `deploy del vite.demo` 清理成功（SC-006）；失败时经 signoz skill 查询环境 `vite.demo` 服务日志定位（AGENTS.md 排查约定），修复后重跑至通过

**Checkpoint**: US4 完成——部署能力交付并可实际执行验证，文档为终态。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖，立即开始；T002/T003 可并行（不同文件），T004 依赖 T001–T003（lockfile 需 manifests 就绪）
- **Phase 2 (US1)**: 依赖 Phase 1 完成（T005 依赖 node_modules 链接；构建验证内联于 T005）
- **Phase 3 (US2)**: 依赖 Phase 2（T008/T010 编辑同一 BUILD 文件于 T005 之后；T007 依赖 T006 的组件 API；T009 独立可与 T006/T007 并行）
- **Phase 4 (US3)**: 依赖 Phase 1（依赖面变更是回归源；建议在 Phase 3 后一并执行以覆盖全部变更）
- **Phase 5 (Polish)**: 依赖 Phase 2–4 全部完成（T013 终验要求全场景绿）
- **Phase 6 (US4 实现)**: 依赖 Phase 2（`:dist` 是 embed 输入）；T014 → T015（`service.yaml` 引用 `:cmd_image`）
- **Phase 7 (US4 验收)**: 依赖 Phase 6 全部完成；T017 依赖 T016（README 命令以实际执行验证）

### User Story Dependencies

- **US1 (P1)**: Phase 1 后开始——MVP 主体
- **US2 (P1)**: 依赖 US1 的构建链路（同一 BUILD 文件追加）
- **US3 (P2)**: 依赖 Phase 1 的依赖变更；对 US1/US2 无交付依赖（纯验证）
- **US4 (P1)**: 依赖 US1 的 `:dist` 产物（embed 输入）；对 US2/US3 无交付依赖

### Parallel Opportunities

- Phase 1: T002 ∥ T003（不同文件）
- Phase 3: T009 ∥ (T006 → T007)（`dist_assert.sh` 与组件源码零交集）；T008/T010 串行（同 BUILD 文件，编辑冲突规避）
- Phase 6→7: T016 可与 T015 并行（README 内容由契约决定，不依赖 T015 执行）；T014→T015 串行

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 → Phase 2 → 停下验证（quickstart 场景 1）
2. MVP = bazel 能构建 React 项目（049 前端的最小前置）

### Incremental Delivery

1. Phase 1+2 → US1 验证（构建链路通）
2. Phase 3 → US2 验证（实证自动化）
3. Phase 4 → US3 验证（存量零回归）
4. Phase 5 → README + 全场景终验
5. Phase 6 → US4 实现（部署载体 + 声明）
6. Phase 7 → US4 验收（实际部署 + 人工访问 + 清理，README 终态）

---

## Notes

- 构建与测试验证内联于各任务（宪法 IV：小颗粒度测试随代码变更执行，不单列 task）；Phase 4 的 US3 与 Phase 7 的 T017 为验证型验收 task（T017 为部署能力验收，宪法 VI 部署闭环的实际执行）
- `tools/dev/js/vite.bzl` 与 `tools/dev/js/vitest_test.bzl` **零修改**（research.md D1/D4）——任何"需要改规则才能跑通"的情况都是方案偏离，回 plan 校准
- 部署载体全部复用既有机制：`wails_asset_library`（embed）、`artifact_pkg_go`/`artifact_image`（镜像）、deploy CLI（部署/清理）——零新建构建规则（research.md D8）
- 特征字符串 `dominion-vite-react-demo` 是组件源码与断言脚本的单一来源常量，修改时两处同步（contracts/dist-artifact-assertions.md 行为要求 4）
- demo 不引入路由/状态库/UI 组件库/dev server 配置/业务 API（spec FR-006 边界；服务载体仅静态托管）
- 部署入口为 deploy CLI 而非 guitar run：guitar 为测试编排闭环（用例必填 + 执行后强制清理），"仅部署供人工访问"由 `deploy apply`/`deploy del` 承载（research.md D8）；未来 web E2E 经 testplan 接入时不改服务形态（contracts/static-server-deploy.md §与 guitar 的关系）
- 每 task 完成后 commit；Phase checkpoint 处按 quickstart 对应场景独立验证
