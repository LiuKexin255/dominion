# AGENTS.md

本文档面向在本仓库内工作的智能体编码代理（agentic coding agents）。

禁止 Agent 修改本文件。

## 规则优先级
执行任务时按以下优先级决策：

1. 用户在当前会话中的直接指令。
2. 当前目录下的 `README.md` 文件。
3. 根目录下 `AGENTS.md`。
4. `style` 目录下的规范和风格指南。
5. 对应语言/生态系统的通用最佳实践。

### 其他

> FOR Agent:
> * 执行 `sub_agent` 或者 `category` 任务前，检查 `category` 和 `subagent_type` 是否只传递了一个（两个参数同时传递可能导致不符合预期的子任务执行），并且确认参数与任务分配相符。

## 开发环境

### 编程语言
仓库使用编程语言与对应文件后缀。

1. Golang: `*.go`

### 编译工具

1. 使用 `bazel` 作为编译工具，使用语言对应 `rules` 为各个语言提供编译支持。
2. 在代码中引用新的依赖后，使用 `bazel run //:gazelle` 命令生成/更新 `BUILD.bazel` 文件（因为设置了 `-lazy` 参数，所以最好在需要更新的目录下执行 `gazelle`，或者指定目录 `bazel run //:gazelle some/subdir`）。
3. `BUILD.bazel` 文件通常应**只由** `gazelle` 命令生成/更新，如需添加 `target`（如 `oci_image`）应在 `gazelle` 生成后添加。不要更改 `gazelle` 生成的内容，除非生成的 `BUILD.bazel` 文件无法编译。
4. 使用 `bazel mod tidy` 命令更新 `bazel` 依赖。
5. 【**重要**】使用 `bazel test` 作为单测验证标准。

#### Golang

1. 使用 [`go_rules`](https://github.com/bazel-contrib/rules_go) 提供 golang 编译支持。
2. 使用 `bazel run @rules_go//go` 来执行 `golang` 命令。
3. 代码格式化：使用 `bazel run @rules_go//go -- fmt [变更文件]` 命令对代码进行格式化；
4. 依赖更新：`bazel run @rules_go//go -- mod tidy -v` 更新 `go.mod`。
5. 涉及 `proto` 的代码，使用 `gazelle` 生成 `BUILD.bazel` 后，使用 `bazel` 进行测试和编译；**禁止**自己编写 `proto` 和 `grpc stub` 代码。
6. golang 大型测试的 `target` 使用 `go_largetest(//tools/go:defs.bzl)` 规则，单元测试使用 `go_unittest` （默认生成）。

##### 格式化与依赖更新

Golang 代码格式化与依赖更新步骤如下：

1. step 1: 使用 `fmt` 对代码进行格式化。
2. step 2: 使用 `mod` 命令更新 `go.mod`。
3. step 3：使用 `gazelle` 命令更新 `BUILD.bazel` 文件。
4. step 4：使用 `bazel mod tidy` 更新 `bazel` 依赖。

#### Typescript/Javascript

1. `npnm` 使用 `bazel run @pnpm --dir {project_path}` 来执行。
2. `ts/js` 项目使用 `npnm` monorepo 仓库来管理依赖，并且子包的依赖全部是 `workspace:*` 以保持和主包版本一致。

## 开发计划

开发计划应满足以下要求

* 有明确的目标与验收标准、预计变更范围。目标不仅包括代码，并且还要有交付的代码能**做什么**、**起到什么作用**。
* 开发计划以 TDD 驱动，交付结果可以被单测或者其他方式进行验证。如果 `RED Tests` 编写困难，**可改为输出测试计划和用例**，完成编码后编写测试代码。
* 服务代码需要大型测试作为验收标准之一（除非 `README.md` 中明确说明本服务不需要大型测试）。
* 代码修改应已**计划的最优实现**为首要目标，而不是“最小改动”。当有更好的代码实现时，应对已有代码进行**重构**，而不是迁就现有代码。
* 计划需明确要求**每次** `Agent/Sub-Agent/Executor` 改动代码前，先阅读仓库规范与风格文件。
* 禁止 `Agent` 提交代码，每次完成 Task 和 Fix 后改用 `**git add**` 暂存代码 。`git commit` 操作由开发者进行。

> FOR `Prometheus`: 
> 1. 计划明确要求  `@Atlas` 进行验收前，执行 `/compact` 命令压缩上下文。
> 2. 计划验收不仅要看有没有交付代码，还要看代码是否实现预期功能。
> 3. Code Quality Review 需要先阅读先阅读仓库规范与风格文件，并包含**测试代码评审**（测试代码是否符合开发目标，是否按测试计划实现）和**代码风格评审**（变更代码是否符合仓库规范）。
> 4. 验收问题即使**不是本次改动导致**，也应当修复。


## 规范与风格

代码规范与风格参考 `styles` 目录下的各个语言对应的参考文件。

## 测试

* 单元测试使用 `bazel test` 执行，且随 `bazel build` 一起作为编译验证的一部分。
* 服务代码需要进行大型测试。先编写测试计划并放到 `testplan` 目录，然后按计划部署服务、执行测试用例。
* 更多大型测试信息参阅 `style` 目录。

> FOR `Prometheus`: 
> * 使用 `testplan` SKILL 执行测试计划。
> * 如没有特别说明，有测试计划的时候需要**执行**测试计划。执行中遇到**部署**或**非本次变更引起的问题**，可跳过执行。
