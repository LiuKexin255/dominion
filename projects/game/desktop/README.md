# projects/game/desktop

本目录包含 Dominion 游戏桌面的 Wails 应用（Go 后端 + Svelte/Vite 前端）。

## 构建方式

**必须使用 Bazel 构建，禁止直接调用前端构建工具生成 `dist/`。**

正确命令：

```bash
bazel build //projects/game/desktop/frontend:dist
bazel build //projects/game/desktop
```

## 禁止手动生成 `frontend/dist/`

### 出现原因

`frontend/package.json` 中声明了 `"build": "vite build"` 脚本。Vite 默认将构建产物输出到 `frontend/dist/`，且会对 JS/CSS 文件名加入内容哈希（例如 `index-DFjPDZE6.js`）。

当开发者直接在 `frontend/` 目录下执行 `pnpm build`、`npm run build` 或 `vite build` 时，这些带哈希的文件会被写入源码树。由于每次构建产生的哈希不同，会导致：

- `git status` 中出现大量未跟踪文件；
- 已跟踪的 `dist/` 文件被标记为删除或修改；
- 源码与构建产物耦合，污染 diff。

### 正确做法

前端构建由 Bazel 的 `vite_build` 规则接管（见 `frontend/BUILD.bazel`）：

```bash
bazel build //projects/game/desktop/frontend:dist
```

产物会输出到 Bazel 的输出目录（`bazel-bin/...`），由 `wails_app` 规则消费，不会污染源码树。`wails.json` 中的 `frontend:build` 也已置空，避免 Wails CLI 触发额外的 npm 构建。

### 禁止事项

- 禁止在 `frontend/` 内手动运行 `pnpm build`、`npm run build` 或 `vite build`；
- 禁止将 `frontend/dist/` 下的任何文件提交到 Git；
- 禁止修改 `vite.config.ts` 将 `outDir` 指回源码树。

### 已生成的 `dist/` 如何处理

如果工作区中已经存在 `frontend/dist/`，请删除并确保不再提交：

```bash
rm -rf projects/game/desktop/frontend/dist
# 若文件曾被 Git 跟踪，则使用：
git rm -r projects/game/desktop/frontend/dist
```

`frontend/.gitignore` 已配置忽略 `dist/`，防止再次误提交。