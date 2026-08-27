# Data Model: vite + React Bazel 打包支持与验证 Demo

**Feature**: [050-vite-react-bazel](spec.md) | **Date**: 2026-08-27

本 feature 为构建基建，无运行时持久化数据实体。本文档建模两类静态结构：**构建产物的结构契约**（dist tree artifact）与**构建图的依赖关系**（catalog 依赖 → demo 包 → bazel targets → 产物/测试）。

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
  ├─ BUILD.bazel
  │   ├─ npm_link_all_packages("node_modules")
  │   ├─ vite_build(":dist")  ────────▶ Dist Tree Artifact（§1）
  │   ├─ vitest_test(":lib_test")      # data = src glob + 镜像 node_modules 条目
  │   └─ sh_test(":dist_assert_test")  # data = [":dist"]，断言 §1 约束
  └─ src/{main.tsx, App.tsx, App.test.tsx}
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

不涉及数据库、配置文件或跨服务数据交换；无状态迁移。
