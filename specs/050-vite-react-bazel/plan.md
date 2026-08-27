# Implementation Plan: vite + React Bazel 打包支持与验证 Demo

**Branch**: `050-vite-react-bazel` | **Date**: 2026-08-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/050-vite-react-bazel/spec.md`

## Summary

让仓库标准构建链路（pnpm workspace + bazel）支持构建 vite + React 前端项目：React 工具链依赖纳入 catalog 统一管理；现有 `vite_build` 规则（`tools/dev/js/vite.bzl`）经调研确认框架中立、**零改动**即可用于 React（research.md D1）；在新建的 `experimental/js/vite_react_demo/` 放一个最小但真实的 React demo（交互组件 + RTL/jsdom 组件单测 + sh_test 产物断言）实证整条链路，并交付**静态页面部署载体**——demo 包内 Go 静态服务器（`server/`，dist 经 `wails_asset_library` 构建期 embed）以仓库标准服务形态（`service.yaml` + `artifact_image` + `deploy.yaml`）经 deploy 工具部署，提供浏览器免 header 直连入口 `https://vite-react-demo.liukexin.com/` 供端到端人工验证（research.md D8）；web E2E 用例暂缓（用户决策，spec FR-009）。存量 Svelte 前端（desktop）构建零回归。作为 `specs/049-agent-v2-dsh-init` web 前端与 web 服务的基建前置。

## Technical Context

**Language/Version**: TypeScript（catalog `typescript ^6.0.3` 线）+ React 18.x（`react ^18.3.1`，锚定 049 组件库 peer `^18.2.0`，research.md D3）；服务载体 Go（`go.mod` `go 1.26.2` 线，标准库 `embed`/`net/http`）

**Primary Dependencies**: `vite ^6.4.2`（catalog 现有线）、`@vitejs/plugin-react ^5.0.0`（peer 实测兼容 vite 6，research.md D2）、`react`/`react-dom ^18.3.1`、`@types/react(-dom)` 18 线、`@testing-library/react ^16` + `@testing-library/dom ^10`、`jsdom`（vitest 环境）；服务载体仅依赖仓库基建包（`common/gopkg/{bootstrap,http,otel,logs}`），零第三方运行时依赖

**Storage**: N/A（构建基建 + 静态托管载体，无持久化数据；部署环境不设置 persistence）

**Testing**: `bazel test`——`vitest_test` 宏（组件单测，jsdom per-file docblock，research.md D4）+ `sh_test`（产物断言，grpc_hello_world smoke_test 先例，research.md D5）+ `go_test`（server 静态托管行为单测，表驱动 httptest）；构建本身 `bazel build`（`vite_build` / `artifact_image` targets）

**Target Platform**: bazel 构建图成员（Linux 开发/CI 环境）；产物为浏览器静态资源（tree artifact）内嵌于 distroless Go 服务镜像；部署经 deploy 工具（`tools/release/deploy`）至 K8s + Gateway 环境（hostname 直连）

**Project Type**: 构建规则消费样板 + demo + 静态页面部署载体（服务型交付：仅静态托管，无 API/持久化）

**Performance Goals**: N/A（无运行时性能目标；构建确定性/可重复性由 bazel 保证）

**Constraints**: React 依赖全部经 catalog（FR-002/SC-004）；`vite_build` 规则零改动（research.md D1）；desktop 前端构建零回归（FR-005）；不做 dev server/HMR/SSR/UI 库/业务 API（FR-006）；部署载体仅静态托管（FR-007）；浏览器免 header 直连入口（FR-008）；web E2E 暂缓、宪法 VI README 豁免（FR-009）

**Scale/Scope**: 1 个 demo 包（JS + server 载体）、catalog 新增 8 个依赖条目、1 个 workspace 通配条目、2 个部署声明文件（`server/service.yaml`、`deploy.yaml`）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|------|------|------|
| I. 引用溯源 | ✅ | research.md/plan.md/contracts 全部结论附仓库相对路径或官方文档/registry 来源 |
| II. 重构式变更 | ✅ | 现有 `vite_build` 规则经评估无需变更（research.md D1），不堆叠新规则、不引入第二套构建机制；部署载体全部复用既有机制（`wails_asset_library` embed、`artifact_pkg_go`/`artifact_image`、deploy 工具），零新建构建规则（research.md D8） |
| III. 接口优先 | ✅ | 对外接口 = vite_build 消费契约（`contracts/vite-build-target.md`）+ 产物断言契约（`contracts/dist-artifact-assertions.md`）+ 静态服务载体与部署契约（`contracts/static-server-deploy.md`：target 形态、service.yaml/deploy.yaml 字段、部署/访问/清理语义），先于实现固定 |
| IV. 测试颗粒度 | ✅ | build + 组件单测 + 产物断言 + server 静态托管单测均为 bazel target，验证执行内联于各开发任务（不单列 build/test task）；US3 为纯验证 story，其回归验证作为 story 验收 task；US4 部署验收（实际 deploy apply + 访问 + del）作为 story 验收 task |
| V. 编码前阅读 | ✅ | tasks 阶段按三分类列文档清单（`style/javascript.md`、`style/golang.md`、Go embed 官方文档、deploy README 等） |
| VI. 大型测试 | ✅（豁免—README 说明） | 交付静态页面部署载体（服务型）：server 单测齐备（go_test）；web E2E 大型测试暂缓——用户决策 2026-08-27（spec FR-009），部署能力以 quickstart 部署场景实际 `deploy apply` + 浏览器人工访问 + `deploy del` 验收（SC-006）；豁免说明随 demo README 交付（宪法 VI 豁免条款） |
| VII. 终态表述 | ✅ | 交付物只含最终形态；被否决选项仅 research.md 记录必要理由；用户决策（web E2E 暂缓）作为设计依据记录于 spec FR-009 |

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
│   ├── dist-artifact-assertions.md
│   └── static-server-deploy.md
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
    ├── deploy.yaml                # 部署声明：环境 vite.demo / type prod / hostname 直连（contracts/static-server-deploy.md §2.2）
    ├── README.md                  # demo 说明（构建/测试/部署命令、访问入口、宪法 VI 豁免说明）
    ├── src/
    │   ├── main.tsx               # createRoot 挂载
    │   ├── App.tsx                # 交互组件（counter，特征字符串）
    │   └── App.test.tsx           # RTL + jsdom docblock 组件单测
    └── server/                    # 静态页面部署载体（Go）
        ├── BUILD.bazel            # go_library/go_binary(:server) + go_test + artifact_pkg_go(:server_pkg) + artifact_image(:cmd_image)
        ├── main.go                # fs.Sub(embed) → http.FileServerFS；bootstrap + otel，:8080
        ├── main_test.go           # 静态托管行为单测（表驱动 httptest）
        ├── service.yaml           # 服务产物声明（app vite-react-demo / port http 8080）
        └── assets/
            └── BUILD.bazel        # wails_asset_library(:assets，src = //experimental/js/vite_react_demo:dist)
```

**Structure Decision**: 单 demo 项目结构（无 Option 保留）。`experimental/js/` 为用户新指定的前端实验区根目录，与 `experimental/ts/`（服务侧 TS 实验）对称；demo 命名沿仓库 snake_case 惯例（research.md D6）。构建规则文件零新增零修改（复用 `tools/dev/js/{vite.bzl,vitest_test.bzl}`）。部署载体为 demo 包内 `server/` 子目录（demo 自包含"前端 + 部署载体"，049 web 服务可按同一形态复制）：embed 库独立子目录 `server/assets/`（`//go:embed` 禁止 `..`；消费方 BUILD 顶部 `# gazelle:resolve` 映射 embed 库——research.md D8）；`deploy.yaml` 位于 demo 根（固定环境名项目根 deploy.yaml 先例 `experimental/golang/grpc_hello_world/deploy.yaml`）；服务样板对齐 fake-llm（`experimental/dsh/demo/fake-llm/`）。

## Complexity Tracking

> 无宪法违例，无需填写。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
