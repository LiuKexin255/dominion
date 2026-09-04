# @dominion/game-web (`projects/game/web/frontend`)

game 网页前端（vite + React SPA）：session 管理与流式对话页面，构建产物（`:dist`）由
Go 静态服务 `//projects/game/web/server` 以 embed 方式托管。行为基线为 desktop 前端的
SessionList/ChatView 组件，风格与实现基线为 dsh Web UI，组件级复用
`@deepseek-ai/dsh-client-ui-primitives`。整体设计见
`specs/049-agent-v2-dsh-init/plan.md`（D4/D8）与
`specs/049-agent-v2-dsh-init/contracts/web-frontend.md`。

## 构建与测试

Bazel target（按 `specs/050-vite-react-bazel/contracts/vite-build-target.md` 契约声明）：

- `//projects/game/web/frontend:dist` — `vite_build`，产出 dist tree artifact
- `//projects/game/web/frontend:lib_test` — `vitest_test`，组件单测入口

## 依赖

全部依赖（含 `@deepseek-ai/dsh-client-ui-primitives`，dsh 0.1.1-rc.2 线——
`third_party/dsh/core` 同线，dist-tag 不可信，依据 `specs/049-agent-v2-dsh-init/research.md` D8）
统一经根 `pnpm-workspace.yaml` catalog 管理。`@deepseek-ai/dsh-client-ui-theme`
不作为依赖引入——其 token sheets 以源码形态 vendored 于 `src/dsh-theme/`
（见 Attribution 与 `src/dsh-theme/README.md`）。

## Attribution

本包参照 [deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)（dsh-web）
的部分组件源码改造实现，上游采用 BSD-3-Clause 许可证：

- `ReasoningRow`（think 折叠行）参照
  [packages/client/ui-chat/src/client/chat/ReasoningRow.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-chat/src/client/chat/ReasoningRow.tsx)
- `ToolCard`（工具调用卡片）参照
  [packages/client/ui-tool/src/client/tool/ToolCallTree.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/ToolCallTree.tsx)，
  卡内布局另参照
  [packages/client/ui-tool/src/client/tool/toolviews/GenericToolCard.tsx](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/ui-tool/src/client/tool/toolviews/GenericToolCard.tsx)
- markdown/代码块等渲染组件直接复用 npm 包
  [@deepseek-ai/dsh-client-ui-primitives](https://cdn.jsdelivr.net/npm/@deepseek-ai/dsh-client-ui-primitives@0.1.1-rc.2/README.md)
- `src/dsh-theme/`（官方 token sheets，以源码形态 vendored）来自 npm 包
  [@deepseek-ai/dsh-client-ui-theme](https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-theme)（MIT，
  上游
  [packages/client/ui-theme](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/client/ui-theme)）；
  溯源与人工升级流程见 `src/dsh-theme/README.md`
