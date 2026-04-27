# third_party

本目录存放本地化的第三方依赖源码。

## Golang 项目

### 依赖管理

- 本目录下的 Golang 项目**不得**保留独立的 `go.mod` 文件，所有依赖由仓库根目录 `go.mod` 统一管理。
- 传递依赖通过 `go mod tidy` + `bazel mod tidy` 自动同步到 `MODULE.bazel` 的 `use_repo`。
- 对应的外部仓库（如 `com_github_xxx`）需从 `use_repo` 中移除，使 Bazel 通过 `importpath` 解析到本地 `//third_party/...`。

### Gazelle 前缀设置

本地化的 Go 项目必须在顶层 `BUILD.bazel` 中声明 `# gazelle:prefix`，使其与原始 module path 一致：

```starlark
# gazelle:prefix github.com/example/project
```

这样 gazelle 为子包生成的 `importpath` 和 `deps` 才能正确指向本地 `//third_party/...` target，
而非外部仓库。
