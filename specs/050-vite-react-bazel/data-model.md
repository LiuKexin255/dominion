# Data Model: vite + React Bazel 打包支持与验证 Demo

**Feature**: [050-vite-react-bazel](spec.md) | **Date**: 2026-08-27

本 feature 交付构建基建与静态页面部署载体，无运行时持久化数据实体。本文档建模三类静态结构：**构建产物的结构契约**（dist tree artifact）、**构建图的依赖关系**（catalog 依赖 → demo 包 → bazel targets → 产物/测试/镜像）与**部署链路的数据流**（deploy 声明 → 环境 → 访问入口）。

## 1. Dist Tree Artifact（静态产物目录）

`vite_build` target 的输出，下游以目录整体消费（同现有 desktop 前端产物形态）。

```text
<target-out>/              # tree artifact 根
├── index.html             # 入口 HTML（必有）
└── assets/
    ├── index-<hash>.js    # bundle（必有；<hash> = rollup 内容哈希 url-safe base64 段 [0-9A-Za-z_-]，默认 8 位，见 contracts/dist-artifact-assertions.md A3）
    └── index-<hash>.css   # 样式（demo 无独立样式文件时可缺省）
```

**结构约束**（对应 `contracts/dist-artifact-assertions.md`）：

| 字段/成员 | 规则 | 校验点 |
|-----------|------|--------|
| `index.html` | 必须存在于产物根 | sh_test 断言 A1 |
| 资源引用 | `index.html` 引用的每个本地资源路径（`<script src>` / `<link href>`）必须解析到产物目录内的既有文件 | sh_test 断言 A2 |
| 资源命名 | JS/CSS 以内容哈希命名（`*-<hash>.js|css` 形态；hash 字符集与默认长度见 `contracts/dist-artifact-assertions.md` A3） | sh_test 断言 A3 |
| bundle 内容 | demo 组件特征字符串（`dominion-vite-react-demo`）必须在某 `.js` 资源内容中出现 | sh_test 断言 A4 |
| React 运行时 | bundle 内含 react-dom 产物的 `@license` banner 字面量（`react-dom.production.min.js`；vite 默认 `esbuild.legalComments: "eof"` 保留 license 注释） | sh_test 断言 A4（与特征字符串共同检出） |

**不变式**：同一源码 + 同一依赖锁的重复构建产物确定性一致（bazel 构建 guarantee，SC-001 可重复性）。

## 2. 构建图依赖关系

```text
pnpm-workspace.yaml (catalog)
  ├─ react ^18.3.1 ──────────────┐
  ├─ react-dom ^18.3.1 ──────────┤
  ├─ @types/react (18 线) ────────┤ dependencies/devDependencies
  ├─ @types/react-dom (18 线) ────┤   （全部 catalog:，零直接版本）
  ├─ @vitejs/plugin-react ^5.0.0 ┤
  ├─ @testing-library/react ^16 ─┤
  ├─ @testing-library/dom ^10 ───┤
  └─ jsdom（稳定线）──────────────┘
           │
           ▼
experimental/js/vite_react_demo/        # pnpm workspace 成员（packages += "experimental/js/*"）
  ├─ package.json                       # @dominion/experimental-vite-react-demo
  ├─ deploy.yaml                        # 部署声明（环境 vite.demo，§4）
  ├─ BUILD.bazel
  │   ├─ npm_link_all_packages("node_modules")
  │   ├─ vite_build(":dist")  ────────────────────┬─▶ Dist Tree Artifact（§1）
  │   ├─ vitest_test(":lib_test")                 │   # data = src glob + 镜像 node_modules 条目
  │   └─ sh_test(":dist_assert_test")             │   # data = [":dist"]，断言 §1 约束
  │                                               │
  └─ server/（静态页面部署载体）                    │
      ├─ assets/BUILD.bazel                       │
      │   └─ wails_asset_library(":assets") ──────┘   # stage + go:embed all:frontend_dist
      ├─ main.go / main_test.go
      ├─ service.yaml                             # 服务/端口/产物声明（§4）
      └─ BUILD.bazel
          ├─ # gazelle:resolve（embed 库 importpath → :assets 映射，消费方 BUILD 顶部）
          ├─ go_library/go_binary(":server")      # embed dist，静态托管 :8080
          ├─ go_unittest(":server_test")          # 表驱动断言托管行为（§3；go_test 仓库 wrapper）
          ├─ artifact_pkg_go(":server_pkg")       # tar 层 /dominion/vite-react-demo/server/bin/server
          └─ artifact_image(":cmd_image")         # OCI 镜像 → registry.liukexin.com/vite-react-demo/server
```

**依赖治理规则**（SC-004，来源 `AGENTS.md` TS/JS 依赖规则）：

- demo `package.json` 的 React 工具链依赖 MUST 声明为 `catalog:`；
- MUST NOT 在 manifest 中出现任何直接版本号（React 相关条目）；
- catalog 版本组合的兼容性依据见 `research.md` D2/D3（plugin-react 5.x ↔ vite 6 peer 实测；react 18.3.x ↔ dsh 组件库 peer ^18.2.0）。

## 3. 测试数据流

| 测试 target | 输入 | 断言对象 | 环境 |
|-------------|------|----------|------|
| `:lib_test`（vitest） | `src/**/*.tsx` 原始源码 + 镜像 `:node_modules/*` | 组件渲染与交互行为（RTL） | jsdom（per-file docblock `// @vitest-environment jsdom`） |
| `:dist_assert_test`（sh_test） | `:dist` tree artifact | §1 结构约束 A1–A4 | bash（runfiles） |
| `server:server_test`（go_unittest） | embed 内的 dist 内容（经 `server/assets` 库随二进制编译期嵌入） | 静态托管行为：`/` 返回入口 HTML（引用 `assets/` 资源）、产物内资源可服务、未知路径 404 | go test（httptest，无外部依赖） |

不涉及数据库、配置文件或跨服务数据交换；无状态迁移。

## 4. 部署链路数据流

部署载体只读消费构建产物，环境无持久化（`deploy.yaml` 不设置 `persistence`）。

```text
deploy.yaml（环境声明：vite.demo / type prod / hostname vite-react-demo.liukexin.com / PathPrefix /）
  └─ deploy apply（tools/release/deploy CLI）
      ├─ service.yaml 解析 → artifact 解析（//experimental/js/vite_react_demo/server:cmd_image）
      ├─ bazel 构建镜像并推送 registry.liukexin.com/vite-react-demo/server
      └─ deploy service 提交环境（K8s Deployment + Service + HTTPRoute）
            └─ 浏览器访问 https://vite-react-demo.liukexin.com/ （免 header 直连，prod 型路由）
deploy del vite.demo → 环境与路由清理
```

| 数据 | 内容 | 声明位置 |
|------|------|----------|
| 环境名 | `vite.demo`（固定名，`{scope}.{env}` 各段 `^[a-z][a-z0-9]{0,7}$`） | `deploy.yaml` `name` |
| 访问入口 | `https://vite-react-demo.liukexin.com/`（hostname 直连 + `PathPrefix /`） | `deploy.yaml` `services[].http` |
| 服务端口 | `http`/8080（`deploy.yaml` `matches.backend: http` 引用端口名） | `server/service.yaml` `ports` |
| 环境类型 | `prod`（仅表示免 header 直连路由模式；`test`/`dev` 型强制 `env` header，浏览器不可达） | `deploy.yaml` `type`，语义见 `projects/infra/deploy/runtime/k8s/builder.go:669-676` |
