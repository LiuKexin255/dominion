# Quickstart: vite + React Bazel 打包支持验证指南

**Feature**: [050-vite-react-bazel](spec.md) | **Date**: 2026-08-27

端到端验证本 feature 的可运行场景。实现完成后按以下步骤逐条验证（每步含预期结果）；这些场景同时构成验收演示脚本。

## 前置条件

- 仓库工作区就绪（node_modules 已按 `AGENTS.md` 的 pnpm 流程安装：`bazel run @pnpm -- --dir /mnt/code/dominion`）。
- demo 与 catalog 依赖已按 `plan.md` Project Structure 落地。

## 场景 1：构建 React demo（US1 / FR-001）

```bash
bazel build //experimental/js/vite_react_demo:dist
```

**预期**：
- 构建成功，输出 tree artifact（`bazel-bin/experimental/js/vite_react_demo/dist/`）。
- 产物含 `index.html` 与 `assets/` 下内容哈希命名的 JS（结构契约见 [data-model.md §1](data-model.md)）。
- 手工复核（可选）：浏览器直接打开产物 `index.html`，页面渲染 demo 组件（含 `dominion-vite-react-demo` 特征文本与 counter 交互）。
- US1 验收场景 3（下游以相同方式消费 tree artifact）由契约保证，无需 demo 自带消费方：`vite_build` 零改动（research.md D1）、输出即单个 tree artifact，与 desktop 消费方式同构（先例 `projects/game/desktop/assets/BUILD.bazel` 的 `wails_asset_library(src = "//projects/game/desktop/frontend:dist")`）。

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

## 收尾

- 交付物中 demo README（`experimental/js/vite_react_demo/README.md`）包含宪法 VI 大型测试豁免说明（非服务型交付，验收 = 场景 1–6）。
- 全部场景通过后，本 feature 即为 `specs/049-agent-v2-dsh-init` 的 web 前端提供就绪基建（消费方式见 [contracts/vite-build-target.md](contracts/vite-build-target.md)）。
