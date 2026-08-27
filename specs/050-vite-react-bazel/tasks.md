# Tasks: vite + React Bazel 打包支持与验证 Demo

**Input**: Design documents from `/specs/050-vite-react-bazel/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md（均位于 `specs/050-vite-react-bazel/`）

**Organization**: 按 user story 组织（spec.md US1/US2/US3）。构建与测试验证按宪法 IV 内联于各任务（不单列 task）；US3 为纯验证 story，其回归验证作为 story 验收 task。

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

- [ ] T001 更新根 `pnpm-workspace.yaml`：`packages` 列表新增 `experimental/js/*`；`catalog` 新增 8 条目——`react: ^18.3.1`、`react-dom: ^18.3.1`、`@types/react: ^18.3.0`、`@types/react-dom: ^18.3.0`、`@vitejs/plugin-react: ^5.0.0`、`@testing-library/react: ^16.0.0`、`@testing-library/dom: ^10.0.0`、`jsdom: ^26.0.0`（版本依据 `specs/050-vite-react-bazel/research.md` D2/D3：plugin-react 5.x peer 覆盖 catalog vite ^6.4.2；react 18.3.x 对齐 049 组件库 peer ^18.2.0；条目按字母序插入现有 catalog）
- [ ] T002 [P] 创建 `experimental/js/vite_react_demo/package.json`（name `@dominion/experimental-vite-react-demo`、private、`type: module`；dependencies：`react`、`react-dom` 全 `catalog:`；devDependencies：`@vitejs/plugin-react`、`@types/react`、`@types/react-dom`、`@testing-library/react`、`@testing-library/dom`、`jsdom`、`vite`、`vitest`、`typescript` 全 `catalog:`——零直接版本，SC-004）+ `experimental/js/vite_react_demo/tsconfig.json`（样板 `projects/game/desktop/frontend/tsconfig.json`：`module/moduleResolution: ESNext/bundler`、`noEmit`、strict，新增 `"jsx": "react-jsx"`、`lib: ["ESNext", "DOM", "DOM.Iterable"]`；include `src/**/*.ts`、`src/**/*.tsx`）+ `experimental/js/vite_react_demo/vite.config.ts`（仅 `plugins: [react()]`）+ `experimental/js/vite_react_demo/index.html`（`<div id="root">` + `<script type="module" src="/src/main.tsx">`）
- [ ] T003 [P] 创建最小源码 `experimental/js/vite_react_demo/src/main.tsx`（`createRoot(document.getElementById("root")!).render(<App />)`，import App 无扩展名——bundler 模式）与 `experimental/js/vite_react_demo/src/App.tsx`（占位函数组件，渲染特征字符串 `dominion-vite-react-demo`）
- [ ] T004 更新 lockfile：`bazel run @pnpm -- --dir /mnt/code/dominion up`；验证 `pnpm-lock.yaml` 新增 `experimental/js/vite_react_demo` importer、`@vitejs/plugin-react` 解析到 5.x 且 peer 无 vite 6 冲突、react 解析到 18.3.x（`specs/050-vite-react-bazel/research.md` 版本汇总表逐项核对）；不手动编辑 lockfile（AGENTS.md）

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

- [ ] T005 [US1] 创建 `experimental/js/vite_react_demo/BUILD.bazel`：`load("@npm//:defs.bzl", "npm_link_all_packages")` + `load("//tools/dev/js:vite.bzl", "vite_build")` + `npm_link_all_packages(name = "node_modules")` + `vite_build(name = "dist", srcs = glob(["src/**"]), config = "vite.config.ts", index_html = "index.html", package_json = "package.json", tsconfig = "tsconfig.json", visibility = ["//visibility:public"])`（**不传 `svelte_config`**，属性逐项对齐 `specs/050-vite-react-bazel/contracts/vite-build-target.md`）；随后 `bazel run //:gazelle experimental/js/vite_react_demo`（gazelle 校正，如误生成多余 target 按契约形态修正——`AGENTS.md` BUILD 惯例）；构建验证（内联，宪法 IV）：`bazel build //experimental/js/vite_react_demo:dist` 成功；核对产物 tree artifact（`bazel-bin/experimental/js/vite_react_demo/dist/`）含 `index.html` 与 `assets/` 下 hash 命名的 `.js` 资源、入口引用无悬空（`specs/050-vite-react-bazel/quickstart.md` 场景 1）

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

- [ ] T006 [US2] 完善 `experimental/js/vite_react_demo/src/App.tsx`：带状态交互组件（counter 按钮：`useState` 计数 + click 递增）+ 渲染特征字符串 `dominion-vite-react-demo`（`data-testid="demo-marker"` 容器）+ 计数展示 `data-testid="demo-count"`（`specs/050-vite-react-bazel/research.md` D7；特征字符串与 `dist_assert.sh` 单一来源约定见 contracts/dist-artifact-assertions.md 行为要求 4）
- [ ] T007 [US2] 创建 `experimental/js/vite_react_demo/src/App.test.tsx`：首行 `// @vitest-environment jsdom` docblock；RTL `render(<App />)` 断言特征字符串可见（`getByTestId("demo-marker")`）+ `fireEvent.click` 计数递增断言（不引入 mock，纯渲染/交互断言——`style/javascript.md` mock 约定下无需 mock）
- [ ] T008 [US2] 在 `experimental/js/vite_react_demo/BUILD.bazel` 追加 `load("//tools/dev/js:vitest_test.bzl", "vitest_test")` + `vitest_test(name = "lib_test", data = glob(["src/**"]) + [":node_modules/react", ":node_modules/react-dom", ":node_modules/@testing-library/react", ":node_modules/@testing-library/dom", ":node_modules/jsdom"], size = "small")`（data 镜像逐项对齐 `specs/050-vite-react-bazel/contracts/vite-build-target.md`；`:node_modules/vitest` 由宏自动注入勿手写；`style/javascript.md` §js_test 执行模型）；`bazel test //experimental/js/vite_react_demo:lib_test` 通过（内联，宪法 IV）
- [ ] T009 [P] [US2] 创建 `experimental/js/vite_react_demo/dist_assert.sh`：`set -euo pipefail`；argv 接收 dist 目录；逐条输出 `PASS/FAIL A1`–`A4`——A1 `index.html` 存在且非空、A2 HTML 内 `src=/href=` 引用的产物内相对资源全部存在（零悬空）、A3 `assets/` 下存在 `*-[hex-hash].js`、A4 某 `.js` 资源内 grep 到特征字符串 `dominion-vite-react-demo` 与 React 运行时痕迹（react-dom 产物的 license banner 字面量 `react-dom.production.min.js`）（逐项对齐 `specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md`；仅 bash + coreutils，样板 `experimental/ts/grpc_hello_world/smoke_test.sh`）
- [ ] T010 [US2] 在 `experimental/js/vite_react_demo/BUILD.bazel` 追加 `load("@rules_shell//shell:sh_test.bzl", "sh_test")` + `sh_test(name = "dist_assert_test", srcs = ["dist_assert.sh"], args = ["$(location :dist)"], data = [":dist"], deps = ["@bazel_tools//tools/bash/runfiles"], tags = ["local"])`（形态对齐 `experimental/ts/grpc_hello_world/BUILD.bazel:115`；`:dist` 为本地执行 target，沿 desktop 前端 `local` 惯例）；验证（内联，宪法 IV）：`bazel test //experimental/js/vite_react_demo:dist_assert_test //experimental/js/vite_react_demo:lib_test --test_output=all` 全绿，`dist_assert_test` 输出 `PASS A1`–`PASS A4`（`specs/050-vite-react-bazel/quickstart.md` 场景 2/3）；任一 FAIL 即修复后重跑至全绿（宪法 VI 全过标准）

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

- [ ] T011 [US3] 存量回归验证：`bazel build //projects/game/desktop/frontend:dist` 与 `bazel test //projects/game/desktop/frontend:lib_test` 全绿，产物结构与 feature 合入前一致（`specs/050-vite-react-bazel/quickstart.md` 场景 4；SC-003）。若出现回归，本 phase **不直接编辑修复**：定位至 Phase 1/2 的依赖或规则变更，回退至对应任务按该 phase 文档清单修复，再重跑本验证至全绿

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

- [ ] T012 [P] 创建 `experimental/js/vite_react_demo/README.md`：demo 定位（vite + React bazel 构建实证）、构建/测试命令、前置条件说明（`bazel run @pnpm -- --dir /mnt/code/dominion` 安装 node_modules；缺少 node_modules 时构建失败的原因与解决方式——spec Edge Cases「本地执行环境差异」）、宪法 VI 大型测试豁免声明（非服务型构建基建，验收 = bazel build + bazel test，参照 `projects/game/fake-llm/README.md` 豁免先例）、049 web 前端的消费指引（链接 `specs/050-vite-react-bazel/contracts/vite-build-target.md`）
- [ ] T013 终验：按 `specs/050-vite-react-bazel/quickstart.md` 场景 1–6 逐条执行——含 `bazel clean` 后可重复构建（场景 5）与 `grep` 依赖合规检查（场景 6：`experimental/js/vite_react_demo/package.json` 中 React 相关条目全部 `catalog:`）；全部通过后 feature 验收完成

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖，立即开始；T002/T003 可并行（不同文件），T004 依赖 T001–T003（lockfile 需 manifests 就绪）
- **Phase 2 (US1)**: 依赖 Phase 1 完成（T005 依赖 node_modules 链接；构建验证内联于 T005）
- **Phase 3 (US2)**: 依赖 Phase 2（T008/T010 编辑同一 BUILD 文件于 T005 之后；T007 依赖 T006 的组件 API；T009 独立可与 T006/T007 并行）
- **Phase 4 (US3)**: 依赖 Phase 1（依赖面变更是回归源；建议在 Phase 3 后一并执行以覆盖全部变更）
- **Phase 5 (Polish)**: 依赖 Phase 2–4 全部完成（T013 终验要求全场景绿）

### User Story Dependencies

- **US1 (P1)**: Phase 1 后开始——MVP 主体
- **US2 (P1)**: 依赖 US1 的构建链路（同一 BUILD 文件追加）
- **US3 (P2)**: 依赖 Phase 1 的依赖变更；对 US1/US2 无交付依赖（纯验证）

### Parallel Opportunities

- Phase 1: T002 ∥ T003（不同文件）
- Phase 3: T009 ∥ (T006 → T007)（`dist_assert.sh` 与组件源码零交集）；T008/T010 串行（同 BUILD 文件，编辑冲突规避）

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

---

## Notes

- 构建与测试验证内联于各任务（宪法 IV：小颗粒度测试随代码变更执行，不单列 task）；Phase 4 的 US3 为纯验证 story，其回归验证作为 story 验收 task（无代码变更）
- `tools/dev/js/vite.bzl` 与 `tools/dev/js/vitest_test.bzl` **零修改**（research.md D1/D4）——任何"需要改规则才能跑通"的情况都是方案偏离，回 plan 校准
- 特征字符串 `dominion-vite-react-demo` 是组件源码与断言脚本的单一来源常量，修改时两处同步（contracts/dist-artifact-assertions.md 行为要求 4）
- demo 不引入路由/状态库/UI 组件库/dev server 配置（spec FR-006 边界）
- 每 task 完成后 commit；Phase checkpoint 处按 quickstart 对应场景独立验证
