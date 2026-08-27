# Implementation Plan: vite + React Bazel 打包支持与验证 Demo

**Branch**: `050-vite-react-bazel` | **Date**: 2026-08-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/050-vite-react-bazel/spec.md`

## Summary

让仓库标准构建链路（pnpm workspace + bazel）支持构建 vite + React 前端项目：React 工具链依赖纳入 catalog 统一管理；现有 `vite_build` 规则（`tools/dev/js/vite.bzl`）经调研确认框架中立、**零改动**即可用于 React（research.md D1）；在新建的 `experimental/js/vite_react_demo/` 放一个最小但真实的 React demo（交互组件 + RTL/jsdom 组件单测 + sh_test 产物断言）实证整条链路，作为 `specs/049-agent-v2-dsh-init` web 前端的基建前置。存量 Svelte 前端（desktop）构建零回归。

## Technical Context

**Language/Version**: TypeScript（catalog `typescript ^6.0.3` 线）+ React 18.x（`react ^18.3.1`，锚定 049 组件库 peer `^18.2.0`，research.md D3）

**Primary Dependencies**: `vite ^6.4.2`（catalog 现有线）、`@vitejs/plugin-react ^5.0.0`（peer 实测兼容 vite 6，research.md D2）、`react`/`react-dom ^18.3.1`、`@types/react(-dom)` 18 线、`@testing-library/react ^16` + `@testing-library/dom ^10`、`jsdom`（vitest 环境）

**Storage**: N/A（构建基建，无持久化数据）

**Testing**: `bazel test`——`vitest_test` 宏（组件单测，jsdom per-file docblock，research.md D4）+ `sh_test`（产物断言，grpc_hello_world smoke_test 先例，research.md D5）；构建本身 `bazel build`（`vite_build` target）

**Target Platform**: bazel 构建图成员（Linux 开发/CI 环境）；产物为浏览器静态资源（tree artifact）

**Project Type**: 构建规则消费样板 + demo（非服务型交付）

**Performance Goals**: N/A（无运行时性能目标；构建确定性/可重复性由 bazel 保证）

**Constraints**: React 依赖全部经 catalog（FR-002/SC-004）；`vite_build` 规则零改动（research.md D1）；desktop 前端构建零回归（FR-005）；不做 dev server/HMR/SSR/UI 库（FR-006）

**Scale/Scope**: 1 个 demo 包、catalog 新增 8 个依赖条目、1 个 workspace 通配条目

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|------|------|------|
| I. 引用溯源 | ✅ | research.md/plan.md 全部结论附仓库相对路径或 npm registry 来源 |
| II. 重构式变更 | ✅ | 现有 `vite_build` 规则经评估无需变更（research.md D1），不堆叠新规则、不引入第二套构建机制 |
| III. 接口优先 | ✅ | 对外接口 = vite_build 消费契约（`contracts/vite-build-target.md`）+ 产物断言契约（`contracts/dist-artifact-assertions.md`），先于实现固定 |
| IV. 测试颗粒度 | ✅ | build + 组件单测 + 产物断言均为 bazel target，验证执行内联于各开发任务（不单列 build/test task）；US3 为纯验证 story，其回归验证作为 story 验收 task |
| V. 编码前阅读 | ✅ | tasks 阶段按三分类列文档清单（`style/javascript.md`、shim 契约、vite 官方文档等） |
| VI. 大型测试 | ✅（豁免） | 非服务型交付（构建基建），无对外服务面；豁免说明随 demo README 交付（spec Assumptions 已记录） |
| VII. 终态表述 | ✅ | 交付物只含最终形态；被否决选项仅 research.md 记录必要理由 |

## Project Structure

### Documentation (this feature)

```text
specs/050-vite-react-bazel/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── vite-build-target.md
│   └── dist-artifact-assertions.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
pnpm-workspace.yaml                # packages += "experimental/js/*"；catalog += react 工具链条目
experimental/js/
└── vite_react_demo/
    ├── BUILD.bazel                # npm_link_all_packages + vite_build(:dist) + vitest_test(:lib_test) + sh_test(:dist_assert_test)
    ├── package.json               # @dominion/experimental-vite-react-demo，依赖全 catalog:
    ├── tsconfig.json              # desktop frontend 形态 + jsx: react-jsx
    ├── vite.config.ts             # plugins: [react()]
    ├── index.html                 # 入口
    ├── dist_assert.sh             # 产物断言脚本（sh_test 消费）
    ├── README.md                  # demo 说明 + 宪法 VI 豁免声明
    └── src/
        ├── main.tsx               # createRoot 挂载
        ├── App.tsx                # 交互组件（counter，特征字符串）
        └── App.test.tsx           # RTL + jsdom docblock 组件单测
```

**Structure Decision**: 单 demo 项目结构（无 Option 保留）。`experimental/js/` 为用户新指定的前端实验区根目录，与 `experimental/ts/`（服务侧 TS 实验）对称；demo 命名沿仓库 snake_case 惯例（research.md D6）。构建规则文件零新增零修改（复用 `tools/dev/js/{vite.bzl,vitest_test.bzl}`）。

## Complexity Tracking

> 无宪法违例，无需填写。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
