# Research: vite + React Bazel 打包支持与验证 Demo

**Feature**: [050-vite-react-bazel](spec.md) | **Date**: 2026-08-27

本文档记录 plan 阶段的调研结论。所有决策面向 `specs/050-vite-react-bazel/spec.md` 的 FR 约束。

## 仓库现状盘点

| 维度 | 现状 | 来源 |
|------|------|------|
| vite bazel 规则 | `tools/dev/js/vite.bzl` 的 `vite_build`：`package_json` 定位包目录 → 本地跑 `./node_modules/.bin/vite build --outDir <tree artifact>`；`svelte_config` 为**可选**属性，规则本身框架中立 | `tools/dev/js/vite.bzl` |
| 唯一存量消费方 | `projects/game/desktop/frontend`（Svelte）：`npm_link_all_packages` + `vite_build` + `vitest_test` 三件套，dist 经 `assets/BUILD.bazel` 打包给 Go embed | `projects/game/desktop/frontend/BUILD.bazel` |
| React 依赖 | catalog（`pnpm-workspace.yaml`）无 react/react-dom/@types/react/React 插件/testing-library/jsdom；仓库无 React 项目先例 | `pnpm-workspace.yaml` |
| workspace 成员 | `experimental/js/` 目录不存在（现有 `experimental/ts/` 为服务侧 TS 实验） | `pnpm-workspace.yaml` packages 列表 |
| JS 测试设施 | `vitest_test` 宏（`tools/dev/js/vitest_test.bzl`）：genrule 拷贝 canonical shim 进消费包 + `js_test`；shim 走 vitest 3 `startVitest`，退出码 fail-closed | `specs/019-js-test-reliability/contracts/run-vitest-shim.md` |
| sh_test 先例 | `experimental/ts/grpc_hello_world`：`sh_test` + `$(location)` 传产物路径 + bash 断言脚本（rules_shell 0.6.1） | `experimental/ts/grpc_hello_world/BUILD.bazel:115` |
| tsconfig 前端样板 | desktop frontend：`module: ESNext`、`moduleResolution: bundler`、`noEmit`、strict | `projects/game/desktop/frontend/tsconfig.json` |

## D1: vite_build 规则零改动（React 支持无需修改规则）

**Decision**: 不修改 `tools/dev/js/vite.bzl`；React 项目以与 Svelte 前端完全相同的属性集使用现有规则（`package_json`/`index_html`/`config`/`tsconfig`/`srcs`，不传 `svelte_config`）。

**Rationale**: 规则实现只做"定位包目录 → 跑 `vite build` → 产物写进 tree artifact"，框架差异完全由包内的 `vite.config.ts` 插件承载。React 项目需要的全部输入（index.html、vite.config.ts、tsconfig.json、src/*.tsx）都在现有属性覆盖范围内。demo 落地本身就是适用性实证（FR-001），desktop 构建不受影响（FR-005）。

**Alternatives considered**:
- 为 React 增加专属规则/属性（如 `react_config`）：无需求支撑，违背原则 II（过度设计）。
- 换用 aspect_rules_js 的 `vite_prod` 类规则：引入第二套前端构建机制，与现有 `vite_build` 并存增加维护面；spec Assumptions 已排除。

## D2: React 插件选 `@vitejs/plugin-react` 5.x

**Decision**: catalog 新增 `@vitejs/plugin-react`，约束 `^5.0.0`（当前解析 5.2.0）。

**Rationale**: 版本兼容是硬约束——catalog vite 线为 `^6.4.2`，npm registry 实测 peer 范围：
- `@vitejs/plugin-react@5.x`: `vite ^4.2.0 || ^5.0.0 || ^6.0.0 || ^7.0.0 || ^8.0.0` ✅（且未来升 vite 7/8 无需换插件）
- `@vitejs/plugin-react@4.7.0`: `vite ^4.2.0 || ^5.0.0 || ^6.0.0 || ^7.0.0` ✅ 但上限低一档
- `@vitejs/plugin-react@6.1.0`（latest）: `vite ^8.0.0` ❌ 与 catalog vite 6 不兼容
- `@vitejs/plugin-react-swc@4.3.3`: `vite ^4 || ^5 || ^6 || ^7 || ^8` ✅ 但为非默认管线（SWC）

官方 babel 版（plugin-react）是 vite 生态默认选择、文档与社区实践最全；demo 无 dev server/HMR 需求（FR-006），babel 构建性能差异不构成选型因素。

**Alternatives considered**: `@vitejs/plugin-react-swc`（兼容但非官方默认管线）；不装插件（vite esbuild 可直接以 `jsx: react-jsx` 编译 .tsx，但 dev 模式缺 fast-refresh、非标准形态，FR-002 明确要求 React 插件入 catalog）。

## D3: React 版本锚 18.x 终版，测试栈 RTL 16 + jsdom

**Decision**（catalog 约束，具体版本 tasks 阶段 `pnpm up` 解析锁定）:
- `react` / `react-dom`: `^18.3.1`（18.x 线终版，满足 `specs/049-agent-v2-dsh-init` 将复用的 `@deepseek-ai/dsh-client-ui-primitives` peer `react ^18.2.0`）
- `@types/react` / `@types/react-dom`: 18 线（`^18.3.x`）
- `@testing-library/react`: `^16.0.0`（peer 支持 react 18；其 peer 依赖 `@testing-library/dom` 需显式同入 catalog `^10.0.0`）
- `jsdom`: 最新稳定线（vitest 3 的 `jsdom` 测试环境载体）

**Rationale**: 049 的组件库 peer 决定 React 主版本下限/上限（spec FR-002）；RTL 16 + jsdom 是 React 组件测试事实标准组合。jsdom 经 vitest 环境机制按需加载，不影响非组件测试。

**Alternatives considered**: React 19（超出 dsh peer 线，049 落地时要回退）；`happy-dom`（更轻但社区默认仍是 jsdom）；`react-test-renderer`（官方已标记废弃方向）。

## D4: 组件测试环境经 per-file docblock 声明，data 镜像 node_modules

**Decision**: demo 的组件测试文件顶部以 `// @vitest-environment jsdom` docblock 声明环境；`vitest_test` 的 `data` 按规范镜像所需 node_modules 条目（`:node_modules/react`、`:node_modules/react-dom`、`:node_modules/@testing-library/react`、`:node_modules/@testing-library/dom`、`:node_modules/jsdom`，`:node_modules/vitest` 宏自动注入）。

**Rationale**: shim 契约（`specs/019-js-test-reliability/contracts/run-vitest-shim.md`）固定以 `startVitest(mode, filters, {watch:false})` 启动，不传环境选项——per-file docblock 是 vitest 官方的 per-file 环境机制，与 shim 零耦合、无需改 shim 或加全局 config。`data` 镜像规则是 `style/javascript.md` §js_test 执行模型的硬性要求（丢失 `:node_modules/*` 会在 runfiles 中 `Cannot find package` 崩溃）。

**Alternatives considered**: 包内 `vitest.config.ts` 全局 `test.environment`（可行但多一个 config 面，且 vite.config.ts 已存在时 vitest 配置合并链更复杂）；改 shim 注入环境选项（违反 canonical shim 单一契约）。

## D5: 产物断言测试用 sh_test + bash 脚本

**Decision**: demo 附带 `dist_assert.sh`，以 `sh_test` 声明（`args = ["$(location :dist)"]`、`data = [":dist"]`、`deps = ["@bazel_tools//tools/bash/runfiles"]`），断言：(a) 入口 `index.html` 存在；(b) HTML 引用的每个 hash 命名资源在产物目录中存在；(c) bundle 内含 demo 组件的唯一特征字符串（minify 不改字符串字面量）。

**Rationale**: `experimental/ts/grpc_hello_world` 的 smoke_test 是仓库现成同构先例（`sh_test` + `$(location)` + bash 断言 + runfiles 定位）；对 tree artifact 的结构性断言用 shell 最直接，不引入新测试载体。

**Alternatives considered**: go_test 读 tree artifact（多一层语言与 target，无对应收益）；把断言塞进 vitest（vitest 面向源码单测，不应依赖 bazel 产物 target；shim 契约也不含产物 data）。

## D6: demo 布局与命名

**Decision**: 新建 `experimental/js/vite_react_demo/`（目录名沿仓库 snake_case 惯例：`hello_world`、`grpc_hello_world`、`team_graph_spike`）；`pnpm-workspace.yaml` packages 新增 `experimental/js/*` 通配。包名 `@dominion/experimental-vite-react-demo`（沿 `@dominion/game-desktop` 风格，private、`type: module`）。

**Rationale**: 用户指定根目录 `experimental/js/`；通配 `experimental/js/*` 与既有 `experimental/ts/*` 对称，后续前端实验零改动进入 workspace。

**Alternatives considered**: 放 `experimental/ts/`（该目录定位服务侧 TS 实验，前端混入语义模糊）；demo 直接放 049 的 web 目录（违背"基建与业务分离、demo 独立验证"的 feature 目标）。

## D7: demo 内容形态（最小但真实）

**Decision**:
- `index.html` + `src/main.tsx`（createRoot 挂载）+ `src/App.tsx`（含一个带状态交互组件，如 counter 按钮，渲染唯一特征字符串，如 `dominion-vite-react-demo`）
- `src/App.test.tsx`：RTL render + 断言交互（click 后计数变化）——验证 React 运行时 + JSX 转换 + jsdom 环境全链路
- `vite.config.ts`：仅 `plugins: [react()]`
- `tsconfig.json`：沿 desktop frontend 形态（`moduleResolution: bundler`、`noEmit`、strict），`jsx: "react-jsx"`
- 不含：路由、状态库、CSS 框架、dev server 配置、dsh 组件（FR-006 边界）

**Rationale**: "能构建"必须被证明为"React 真实参与构建"（US2）：交互组件 + 特征字符串 + RTL 行为断言三者共同构成证据链；其余一概最小化。

## 版本兼容性结论汇总

| 依赖 | catalog 约束 | 兼容依据 |
|------|--------------|----------|
| `@vitejs/plugin-react` | `^5.0.0` | npm 实测 peer `vite ^4.2‖^5‖^6‖^7‖^8`；latest 6.x 要求 vite ^8 不可用 |
| `react` / `react-dom` | `^18.3.1` | dsh `dsh-client-ui-primitives` peer `react ^18.2.0`（049 前置） |
| `@testing-library/react` | `^16.0.0` | peer react ^18/^19；需显式 `@testing-library/dom ^10` |
| `jsdom` | 最新稳定线 | vitest 3 `jsdom` 环境标准载体 |
| vite / vitest / typescript | 沿 catalog 现有线 | 不动（spec Assumptions：工具链大版本升级不在范围） |

## 风险与对策

- **pnpm 对 React 依赖的 install 脚本**：`pnpm-workspace.yaml` 有 `onlyBuiltDependencies: []`（禁止构建脚本），React 全家桶无 install 脚本需求，无影响。
- **vite 6 + plugin-react 5 的 peer 告警**：已实测兼容；如 `pnpm up` 解析出告警，以 lockfile 为准确认版本组合。
- **jsdom 体积/速度**：仅 demo 组件测试文件使用（docblock 按需加载），不影响其他包测试。
- **gazelle 对 `experimental/js/` 的误生成**：vite 项目与 `ts_project` 形态不同，BUILD 以 desktop frontend 为样板手工声明（gazelle 生成后按 `AGENTS.md` 惯例调整 target）。

## 参考

- `tools/dev/js/vite.bzl`（现有规则全文）
- `tools/dev/js/vitest_test.bzl` + `specs/019-js-test-reliability/contracts/run-vitest-shim.md`
- `projects/game/desktop/frontend/`（BUILD/package/tsconfig 三件套样板）
- `experimental/ts/grpc_hello_world/BUILD.bazel:115` + `smoke_test.sh`（sh_test 先例）
- `style/javascript.md` §js_test 执行模型
- npm registry: `@vitejs/plugin-react`、`@vitejs/plugin-react-swc` peer 元数据（2026-08-27 查询）
