# Wails v2

本目录包含 [Wails](https://wails.io/) v2 框架的本地源码副本，版本 `v2.12.0`。

## 为什么本地化

Wails 同时作为本项目 `tools/wails` 工具链的 CLI 二进制来源和
`projects/game/windows_agent` 的运行时依赖。将源码本地化可以确保 CLI 版本与
Go 依赖版本始终一致，避免单独管理 CLI 二进制版本和 go.mod 依赖版本。
