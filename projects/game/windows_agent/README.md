# Windows Agent

Windows Agent 是一个基于 Wails v2 的桌面应用，运行在 Windows 系统上，提供 AI 游戏操控的桌面代理能力。前端使用 Svelte + Vite，后台使用 Go/Wails，构建由 Bazel 统一管理。

## 目录结构

```
projects/game/windows_agent/
├── frontend/              # Svelte + Vite 前端源码
│   ├── src/               # 前端组件与逻辑
│   ├── index.html         # 入口 HTML
│   ├── vite.config.ts     # Vite 配置
│   ├── package.json       # 前端依赖声明
│   └── BUILD.bazel        # 前端 Bazel build/test target
├── assets/                # Wails AssetServer embed.FS 资源
│   ├── assets.go          # //go:embed all:frontend_dist
│   ├── defs.bzl           # frontend_embed_assets 自定义 rule
│   └── BUILD.bazel
├── cmd/windows_agent/     # Go 主程序入口
│   └── main.go            # Wails app 构造与启动
├── helper/                # 输入助手（input-helper.exe 源码）
│   └── input/             # 鼠标 IPC 命令解析与 Win32 执行
├── internal/              # 业务逻辑（采集、编码、输入控制等）
│   ├── app/
│   ├── capture/
│   ├── encoder/
│   ├── input/
│   ├── media/
│   ├── runtime/
│   ├── transport/
│   └── window/
├── release/               # 打包规则（zip 打包 Go binary + 资源）
├── resources/             # 应用图标等静态资源
├── wails.json             # Wails 项目识别，CI 不使用 wails build
└── BUILD.bazel            # 顶层打包 target
```

## 构建

生产构建使用 Bazel，打包为 Windows 便携 zip：

```bash
bazel build //projects/game/windows_agent:windows_agent_win_zip
```

产物包含：
- `windows-agent.exe`（前端资源已嵌入二进制）
- `resources/bin/ffmpeg.exe` + SHA256
- `resources/bin/input-helper.exe`
- `resources/icon.ico`

构建会自动触发：
1. Vite 前端构建（`//projects/game/windows_agent/frontend:dist`）
2. Asset handoff（`//projects/game/windows_agent/assets:frontend_dist`）
3. Go 编译与 Wails binary 链接
4. Portable zip 打包

## 测试

运行所有 Go 单测与前端 Vitest 测试：

```bash
bazel test //projects/game/windows_agent/...
```

包括：
- Go 单元测试（`internal/` 各包）
- 前端 Vitest smoke test（`frontend/`）

## 前端开发

启动 Vite dev server（热重载）：

```bash
bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/windows_agent/frontend dev
```

必须使用 Bazel 管理的 pnpm，不要使用系统全局安装的 pnpm，以确保依赖版本的一致性。

## 依赖管理

前端依赖通过 `pnpm-workspace.yaml` 的 catalog 统一管理。执行依赖操作必须使用 Bazel-managed pnpm：

```bash
bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/windows_agent/frontend add <package>
bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/windows_agent/frontend up
```

操作后需更新 `pnpm-lock.yaml` 并运行 `bazel mod tidy` 同步 Bazel 依赖。

## Wails CLI

`wails.json` 保留在项目根目录，用于 Wails 项目工具链识别（如 IDE 插件、代码生成等）。其 `frontend:build` 等字段为空字符串，因为 CI 和开发环境均以 Bazel 为权威构建入口，不使用 `wails build`。

## 不提交生成产物

以下目录由 Bazel 自动生成，**不得提交到 Git**：

- `frontend/dist/` — Vite 构建输出
- `assets/frontend_dist` — 由 Bazel action 生成的 asset handoff 产物

根目录 `.gitignore` 已配置忽略 `node_modules`，Bazel 生成产物位于 `bazel-*` 目录下，不会被提交。
