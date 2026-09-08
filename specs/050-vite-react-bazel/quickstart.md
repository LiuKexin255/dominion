# Quickstart: vite + React Bazel 打包支持验证指南

**Feature**: [050-vite-react-bazel](spec.md) | **Date**: 2026-08-27

端到端验证本 feature 的可运行场景。实现完成后按以下步骤逐条验证（每步含预期结果）；这些场景同时构成验收演示脚本。

## 前置条件

- 仓库工作区就绪（node_modules 已按 `AGENTS.md` 的 pnpm 流程安装：`bazel run @pnpm -- --dir /mnt/code/dominion`）。
- demo 与 catalog 依赖已按 `plan.md` Project Structure 落地。
- 场景 7 额外前置：deploy 工具已安装（`bazel run //:deploy_install`，`tools/release/deploy/README.md` §安装）且 `registry.liukexin.com` 推送凭证可用。

## 场景 1：构建 React demo（US1 / FR-001）

```bash
bazel build //experimental/js/vite_react_demo:dist
```

**预期**：
- 构建成功，输出 tree artifact（`bazel-bin/experimental/js/vite_react_demo/dist/`）。
- 产物含 `index.html` 与 `assets/` 下内容哈希命名的 JS（结构契约见 [data-model.md §1](data-model.md)）。
- 手工复核（可选）：浏览器直接打开产物 `index.html`，页面渲染 demo 组件（含 `dominion-vite-react-demo` 特征文本与 counter 交互）。
- US1 验收场景 3（下游以相同方式消费 tree artifact）的实证消费方为 demo 部署载体：`experimental/js/vite_react_demo/server/assets/BUILD.bazel` 的 `wails_asset_library(src = "//experimental/js/vite_react_demo:dist")` 与 desktop 消费方式同构（先例 `projects/game/desktop/assets/BUILD.bazel`；消费契约见 [contracts/static-server-deploy.md](contracts/static-server-deploy.md)）。

## 场景 2：产物断言测试（US2 / FR-004a）

```bash
bazel test //experimental/js/vite_react_demo:dist_assert_test --test_output=all
```

**预期**：PASSED；输出逐条 `PASS A1`–`PASS A4`（断言语义见 [contracts/dist-artifact-assertions.md](contracts/dist-artifact-assertions.md)）。任何一条 FAIL 即验收不通过。

## 场景 3：组件单测（US2 / FR-004b）

```bash
bazel test //experimental/js/vite_react_demo:lib_test --test_output=all
```

**预期**：PASSED；App.test.tsx 的用例全部通过（RTL render + 交互断言，jsdom 环境）。

## 场景 4：存量零回归（US3 / FR-005、SC-003）

```bash
bazel build //projects/game/desktop/frontend:dist
bazel test //projects/game/desktop/frontend:lib_test
```

**预期**：两者均成功，行为与本 feature 合入前一致。

## 场景 5：可重复构建（SC-001）

```bash
bazel clean && bazel build //experimental/js/vite_react_demo:dist && \
bazel test //experimental/js/vite_react_demo:all
```

**预期**：干净环境重建后，场景 1–3 结果一致（全部成功）。

## 场景 6：依赖治理合规（SC-004）

```bash
grep -n '"react\|"@types/react\|"@vitejs/plugin-react\|"@testing-library\|"jsdom' \
  experimental/js/vite_react_demo/package.json
```

**预期**：所有命中行的版本字段均为 `catalog:`，无直接版本号。

## 场景 7：部署与人工访问验证（US4 / FR-007、FR-008、SC-006）

部署能力验收：实际执行部署 → 浏览器人工访问 → 清理闭环（部署入口为 deploy CLI；guitar 为测试编排闭环，不承载"仅部署供人工访问"形态——`research.md` D8、[contracts/static-server-deploy.md](contracts/static-server-deploy.md) §2.3）。

```bash
# 1. 构建 + 服务单测（部署声明的镜像 target 可构建）
bazel build //experimental/js/vite_react_demo/server:cmd_image
bazel test //experimental/js/vite_react_demo/server:server_test --test_output=all

# 2. 部署（自动构建推送镜像 → 提交环境 → 等待就绪；部署后保持运行）
deploy apply //experimental/js/vite_react_demo/deploy.yaml
```

**预期**：
- `cmd_image` 构建成功；`server_test` PASSED（`/` 返回入口 HTML、产物资产可服务、未知路径 404）。
- `deploy apply` 输出 `环境 vite.demo 已应用，状态: 就绪`（滚动等待期间输出"等待滚动发布"属正常中间态）。

**访问验证（人工 + curl 等价断言）**：
- 浏览器打开 `https://vite-react-demo.liukexin.com/`：页面渲染 demo 组件（`dominion-vite-react-demo` 特征文本 + counter 交互），DevTools Network 中 `assets/` 资源全部 200。
- curl 等价：`curl -s https://vite-react-demo.liukexin.com/` 返回入口 HTML（含 `/assets/` 资源引用）；对引用的任一 `assets/*.js` 路径 `curl -s` 返回 200 且内容含 `dominion-vite-react-demo`（与场景 2 断言 A4 同源）。

```bash
# 3. 清理（人工验证完成后；保持页面可访问时可暂缓执行）
deploy del vite.demo
```

**预期**：环境删除成功，入口 URL 不再可访问。

## 收尾

- 交付物中 demo README（`experimental/js/vite_react_demo/README.md`）包含宪法 VI 说明（终态表述）：demo 交付静态页面部署载体（服务型，server 单测齐备），部署能力已交付并以场景 7 实际执行验收；web E2E 大型测试暂缓为用户决策（2026-08-27，依据 [spec.md](spec.md) FR-009），依据宪法 VI 豁免条款作 README 说明。
- 全部场景通过后，本 feature 即为 `specs/049-agent-v2-dsh-init` 的 web 前端与 web 服务提供就绪基建（消费方式见 [contracts/vite-build-target.md](contracts/vite-build-target.md) 与 [contracts/static-server-deploy.md](contracts/static-server-deploy.md)）。
