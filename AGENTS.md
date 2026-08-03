# AGENTS.md

本文档面向在本仓库内工作的智能体编码代理（agentic coding agents）。

## 需求&方案规范

* 本仓库开发采用 `SDD` 架构，使用 `speckit` 作为 `SDD` 框架。
* 所有 Agent 在进行**需求规划**、**方案设计**、**计划制定**与**代码开发**时，应当阅读并遵守 `.specify/memory/constitution.md` 规范。

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

1. 参阅 `style` 目录下的文档，**任何**编辑代码前应当先阅读代码规范要求。
2. **注意**规范文档通常会引用为外部文档作为规范一部分，阅读和规划任务必读文档时**不要忽略掉引用外部的规范文档**。

## 调试与大型测试

* 大型测试保存在 `testplan` 目录下。使用 `testplan` SKILL 执行大型测试的测试计划。
* 遇到测试失败或需要排查运行时问题，可以使用 `signoz` skill 查询日志与 traces。
* 编写/执行大型测试的测试计划之前，要求阅读 `style/large_test.md`。

## 注释

* Do not add comments that restate what the code already expresses. Only add comments when they explain why (design decisions, workarounds) or when code is complex and requires additional context. 
* 如果注释要引用其他文件，需要写明引用链接，避免发生歧义。例如引用 specs 内容需要注明链接，不能只写 `FR-xx` 这类，避免不同的需求导致的歧义。

## 依赖

* 持续的、及时的升级依赖版本，避免版本差异太大导致与最新文档不一致，也避免版本差异过大导致未来升级困难。

## 信息检索

充分使用各种工具和 sub-agent 获取所需的信息，避免即兴设计、盲目开发。

* 使用 `webfetch` 工具读取 web URL 内容。
* 使用 `websearch` 工具在网络上检索信息。
* 加载 `context7-mcp` SKILL，使用 `context7` MCP 检索某个项目或者代码库的文档。
* 使用 `grep.app` MCP 检索 `github` 代码。
* 使用 `explore` subagent 探索代码仓库。
* 使用 `general` subagent 研究复杂问题。

## 其他

* 对服务进行问题排查时，应当优先查看 tracing 和 log 确认实际情况，特别是提供 tracing id 的情况。
* **Read 工具无传递性阅读能力**，例如 Read 工具阅读文件 `style/api.md` 文件，并不会阅读其中引用的 AIP 规范链接的内容。外部链接需要 agent 自己使用 web 工具阅读文档，无法通过 Read 其他文件间接获取。
* 获取 `bazel` 产物和测试报告等信息使用仓库下的 `bazel-*` 等目录，而不是 `~/.cache/bazel` 目录
* 对同一文件编辑建议**串行**进行，避免并发编辑导致更新丢失。

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at `specs/018-saolei-mcp/plan.md`
<!-- SPECKIT END -->
