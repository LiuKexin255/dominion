# AGENTS.md

本文档面向在本仓库内工作的智能体编码代理（agentic coding agents）。

## 操作命令参考

### Bazel / Gazelle

* 使用 `bazel` 作为编译与测试入口。
* 在代码中引用新的依赖后，使用 `bazel run //:gazelle` 生成/更新 `BUILD.bazel` 文件。由于设置了 `-lazy` 参数，最好在需要更新的目录下执行 `gazelle`，或者指定目录：`bazel run //:gazelle some/subdir`。
* `BUILD.bazel` 文件通常只由 `gazelle` 生成/更新；如需添加 `target`（如 `oci_image`）应在 gazelle` 生成后添加。
* 使用 `bazel mod tidy` 更新 Bazel 依赖。

### Golang

* 使用 `bazel run //:go` 执行 Go 命令。
* 代码格式化：`bazel run //:go -- fmt [变更文件]`。
* 依赖更新：`bazel run //:go -- mod tidy -v`。

Golang 代码格式化与依赖更新步骤：

1. 使用 `fmt` 对代码进行格式化。
2. 使用 `mod` 命令更新 `go.mod`。
3. 使用 `gazelle` 更新 `BUILD.bazel` 文件。
4. 使用 `bazel mod tidy` 更新 Bazel 依赖。

### TypeScript / JavaScript

* 执行 `pnpm` 命令时使用：`bazel run @pnpm -- --dir {project_path}`，`--dir` 参数必须使用绝对路径。
* TypeScript/JavaScript 依赖版本必须统一在根目录 `pnpm-workspace.yaml`的 catalog 中管理；只有明确记录为特殊例外的依赖才可以在 package manifest 中声明直接版本。
* 修改 `package.json` 后，使用 `pnpm up` 更新依赖；不要手动修改 `pnpm-lock.yaml`。

### Python

* 使用 `bazel run //:python -- [python 参数]` 执行 Python 命令，例如：
  `bazel run //:python -- --version` 或
  `bazel run //:python -- -m compileall [目录]`。

Python 依赖更新步骤：

1. 修改 `requirements_lock.txt`，添加、删除或更新 Python 依赖版本。
2. 使用 `bazel mod tidy` 更新 Bazel 模块锁文件与依赖解析结果。
3. 使用 `bazel run //:gazelle` 生成/更新 Python 相关 `BUILD.bazel` 文件。
4. 使用 `bazel build //...` 和 `bazel test //...` 验证仓库构建与测试。

## 代码规范 

参阅 `style` 目录下的文档，**任何**编辑代码前应当先阅读代码规范要求。

## 调试与大型测试

* 遇到测试失败或需要排查运行时问题，可以使用 `signoz` skill 查询日志与 traces。
* 需要执行大型测试计划时，使用 `testplan` skill。

## 注释

Do not add comments that restate what the code already expresses. Only add comments when they explain why (design decisions, workarounds) or when code is complex and requires additional context. 

## 其他

* 对服务进行问题排查时，应当优先查看 tracing 和 log 确认实际情况，特别是提供 tracing id 的情况。

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
<!-- SPECKIT END -->
