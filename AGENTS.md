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

1. 阅读目录结构和代码时，优先阅读根目录和需求相关目录，如有需要再阅读其他目录。

## 开发环境

### 编程语言
仓库使用编程语言与对应文件后缀。

1. Golang: `*.go`

### 编译工具

1. 使用 `bazel` 作为编译工具，使用语言对应 `rules` 为各个语言提供编译支持。
2. 使用 `bazel run //:gazelle` 命令生成/更新 `BUILD.bazel` 文件。**特别注意**：应当只使用 `gazelle` 生成 `BUILD.bazel` 文件，除非生成的文件无法编译。
3. 使用 `bazel mod tidy` 命令更新 `bazel` 依赖。

#### Golang

1. 使用 [`go_rules`](https://github.com/bazel-contrib/rules_go) 提供 golang 编译支持。
2. 代码格式化：使用 `bazel run @rules_go//go -- fmt [变更文件]` 命令对代码进行格式化；
3. 依赖更新：`bazel run @rules_go//go -- mod tidy -v` 更新 `go.mod`。
4. 为 `BUILD.bazel` 中的单元测试 target 设置 `size= "small"`。
5. 如果缺少 `proto` 相关依赖，可以尝试使用 `gazelle` 更新 `BUILD.bazel` 后是否解决问题。

###### 格式化与依赖更新

Golang 代码格式化与依赖更新步骤如下：

1. step 1: 使用 `fmt` 对代码进行格式化。
2. step 2: 使用 `mod` 命令更新 `go.mod`。
3. step 3：使用 `gazelle` 命令更新 `BUILD.bazel` 文件。
4. step 4：使用 `bazel mod tidy` 更新 `bazel` 依赖。

## 开发流程

### 代码生成流程
Agent 应按照规定迭代流程生成代码。以下为单个迭代流程，从上到下依次执行，且单次只执行该步骤中指定的动作，不得执行当前步骤中没有列出的动作。

1. step 1: 分析指令和需求，并列出代码变更计划。
1. step 2：根据需求/要求，更改代码文件。
1. step 3: 执行对应语言的代码格式与依赖更新。
2. step 5：进行编译检查。
3. step 6：运行单元测试。

较大代码变更应拆分为多次迭代进行变更，每次迭代代码变更行数最好小于 500 行。代码变更计划参考:

```
epoch 1: 
    step 1.1: 修改 `xxxx.go` 增加 `xxxx` 方法，...
    step 1.2: 新增 `xxxx.go` 文件，包含 .... 功能，...
epoch 2:
    step 2.1: ...
```
