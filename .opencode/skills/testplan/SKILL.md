---
name: testplan
description: Execute large test plans in this repository using guitar CLI to validate YAML testplans, deploy the SUT, run Bazel cases, and clean up the environment.
compatibility: opencode
metadata:
  audience: dominion
  scope: large-test
---

# testplan

使用这个 skill 执行仓库里的大型测试计划。

## 何时使用

- 你刚完成了 `grpc` 或 `http` 服务相关实现，需要按测试计划执行大型测试。
- 用户/开发计划要求"执行 testplan / 测试计划"。
- 你发现仓库规范要求编写 YAML 格式的测试计划并通过 `guitar` 执行。

不要在下面场景使用：

- 只是运行普通单元测试：直接使用 `bazel test`。
- 还没有测试计划文件：先帮助用户创建 YAML 测试计划。

## 必须先读的仓库约定

在执行前先读取：

1. `styles/large_test.md`
2. 对应服务目录下的 `README.md`（如果存在）
3. 测试计划 YAML 文件本身

仓库约定要点：

- 大型测试计划使用 YAML 格式，放在 `testplan` 目录。
- 使用 `guitar` 工具执行测试计划，完成"校验 → 部署 → 测试 → 清理"闭环。
- 测试代码通过 `pkg/testtool` 读取环境变量，不再直接使用 `SUT_HOST_URL`。
- 环境变量由 guitar 自动注入，包括 `TESTTOOL_ENV` 和 `TESTTOOL_ENDPOINT_<PROTOCOL>_<NAME>`。

## 测试计划格式

YAML 格式，参考 `design/guitar_yaml_testplan.md`：

```yaml
name: game-session-large-test
description: game-session HTTP REST 接口测试

suites:
  - name: default
    env: game.lt
    deploy: //projects/game/testplan/test_deploy.yaml
    endpoint:
      http:
        public: https://game.liukexin.com
    cases:
      - //projects/game/testplan:testplan_test
```

字段说明：
- `name`：测试计划名称
- `description`：测试计划描述（可选）
- `suites[].env`：测试环境标识，格式为 `scope.env`（如 `game.lt`）
- `suites[].deploy`：被测系统部署配置路径（`//` 前缀为 workspace 相对路径）
- `suites[].endpoint`：测试入口 URL 映射（可选），格式为 `protocol: name: url`
- `suites[].cases`：Bazel test target 列表

## 执行流程

### 1. 安装工具

确保 `guitar` 和 `deploy` 已安装：

```bash
bazel run //:deploy_install
bazel run //:guitar_install
```

### 2. 校验测试计划

执行静态校验，确认测试计划配置正确：

```bash
guitar validate <plan.yaml>
```

如果校验失败，根据错误信息修复测试计划 YAML 文件。

### 3. 执行测试计划

```bash
guitar run <plan.yaml>
```

`guitar run` 会自动完成：
1. 校验测试计划配置
2. 部署被测系统（`deploy apply`）
3. 执行测试用例（`bazel test --config=largetest`）
4. 清理环境（`deploy del`）

如果执行失败：
- 检查错误信息判断失败阶段（校验/部署/测试/清理）
- 如果是被测系统问题，修复后重新执行
- 如果是测试代码问题，修复后重新执行
- 最多重试 3 次

### 4. 查看结果

`guitar run` 会输出：
- 部署状态
- 每个测试 case 的结果
- 清理状态

如果失败，根据输出定位失败阶段。

## 编写测试计划

如果需要创建新的测试计划：

1. 在服务目录下创建 `testplan` 目录
2. 创建 `<test_name>.yaml` 文件，按上述 YAML 格式编写
3. 创建 `test_deploy.yaml` 部署配置（参考现有 deploy 配置）
4. 编写测试代码，使用 `pkg/testtool` 读取环境变量
5. 使用 `guitar validate` 校验测试计划

## 输出要求

向用户汇报时，按下面结构输出：

1. 使用的测试计划路径
2. 校验结果
3. 部署结果
4. 每个测试 case 的结果
5. 清理结果
6. 如果失败，指出失败发生在：校验 / 部署 / 测试执行 / 清理 中的哪一阶段

## 强约束

- 不要跳过清理。
- 不要伪造测试入口 URL。
- 不要把大型测试降级为"只跑单测"然后宣称完成。
- 不要修改测试计划文件，除非用户明确要求你修它。
- 不要把部署后短暂未就绪直接报成最终失败；guitar 内部已有超时和重试机制。
- 不要无限等待或无限重试。
- 不要提交代码。

## 仓库内可直接参考的文件

- `styles/large_test.md`
- `design/guitar_yaml_testplan.md`
- `tools/test/guitar/README.md`
- `tools/release/deploy/README.md`
- `projects/game/testplan/interface_test.yaml`
