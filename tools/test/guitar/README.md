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
guitar run <plan.yaml> --suite <suite-name>
```

执行完整测试计划：校验 → 部署 → 测试 → 清理。suites 按 YAML 中的顺序串行执行，任一 suite 失败后立即停止。

`--suite <suite-name>` 可选参数，指定只执行测试计划中的单个套件（通过 `name` 匹配）。未指定时执行全部套件。

### 测试计划格式

详见 `design/guitar_yaml_testplan.md`。

每个 suite 支持以下字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 套件名称，用于标识和 `--suite` 匹配 |
| `deploy` | string | 是 | 部署配置路径（相对目录或 `//` 开头的项目路径） |
| `cases` | string[] | 是 | 大型测试用例 target 列表 |
| `timeout` | int | 否 | 套件级别超时（秒）。`0` 或省略表示未设置，回退到全局 `--timeout`；`>0` 覆盖全局超时 |
| `endpoint` | object | 否 | endpoint 映射 |

### 输出格式

执行时按套件分组输出，格式如下：

```
--- Suite: {name} ---
run={runID} env={envName} deploy={deployPath}
  Deploy
  ...
  Test
    ...
  Cleanup
  ...
```

- 每个 suite 输出以 `--- Suite: {name} ---` 标题开头
- 第二行显示 runID、自动生成的环境名、deploy 路径
- 步骤（Deploy / Test / Cleanup）缩进 2 个空格
- 状态颜色：成功为绿色，失败为红色，运行中为黄色
- TTY 模式下自动启用颜色，非 TTY 或管道模式下自动禁用
- 每个 suite 执行受其 `timeout` 或全局 `--timeout` 限制，超时则终止该 suite

当 suite 的部署步骤不成功时，`  Deploy` 之后会附加「环境状态」诊断输出，格式如下：

```
  --- 环境状态 (env=game.lt3x8q2) ---
环境 game.lt3x8q2
状态: 失败
说明: service "gateway" rollout failed: ImagePullBackOff
服务:
  - gateway (app=game) [artifact]
  - mongo (app=game) [infra: mongodb]
最近调和: 2026-08-02T10:30:00Z
最近成功: -
```

- 第一行为醒目分隔头部（2 空格缩进，`--- ... ---` 包裹，不着色）
- 紧随其后为 `deploy describe` 的顶格文本（环境名/状态/失败说明/服务列表/最近调和与成功时间），不做逐行缩进
- 若 `deploy describe` 自身失败（如环境不存在、deploy service 不可达），向 stderr 输出 warning 降级，不影响原始部署错误上报与后续清理
- 部署成功时不输出该诊断（与既有行为一致）

诊断触发条件与降级语义见 `../../../specs/032-guitar-deploy-failure-state/contracts/guitar-integration.md`；describe 输出格式见 `../../../specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md`。

执行结束后输出一个 Summary，列出每个 suite 的结果与总计统计，方便用 `tail` 查看执行结果：

```
--- Summary ---
total: 2, passed: 1, failed: 1
  suite-a: success
  suite-b: failure, error: <error>
```

注意：suite 中不再包含 `env` 字段。环境名由 `guitar run` 为每个 suite 自动生成（lt + 6 位 base36 随机串），并通过环境变量注入测试进程。
