本目录包含大型测试编排相关定义与工具

## Guitar 工具

通过 guitar 工具执行 YAML 格式的大型测试计划，实现"校验 → 部署 → 执行 → 清理"闭环。

### 安装

使用以下命令安装 guitar 工具：

```bash
bazel run //:guitar_install
```

- 默认安装路径为 `$HOME/.local/bin`。
- 可以通过 `--prefix` 参数指定安装路径。

### 前置条件

guitar run 依赖 deploy 工具，请先安装：

```bash
bazel run //:deploy_install
```

### 相关命令

1. 校验测试计划

```bash
guitar validate <plan.yaml>
```

静态校验测试计划配置：结构校验、必填字段检查、deploy 类型校验、endpoint hostname 校验。

2. 执行测试计划

```bash
guitar run <plan.yaml>
```

执行完整测试计划：校验 → 部署 → 测试 → 清理。suites 按 YAML 中的顺序串行执行，任一 suite 失败后立即停止。

### 测试计划格式

详见 `design/guitar_yaml_testplan.md`。

注意：suite 中不再包含 `env` 字段。环境名由 `guitar run` 为每个 suite 自动生成（lt + 6 位 base36 随机串），并通过环境变量注入测试进程。
