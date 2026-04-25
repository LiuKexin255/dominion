# Guitar 动态测试环境名设计

## 目标

本方案用于为 `tools/guitar` 与 `tools/deploy` 增加大型测试运行时动态环境名能力，目标是：

* 每次执行 `guitar run` 时，为测试部署生成唯一的临时测试环境名，避免多次大型测试互相覆盖同一个环境。
* 从 `testplan.yaml` 中移除手工维护的环境名配置，避免 `suite.env` 与 `deploy.name` 不一致。
* 让测试代码继续通过 `pkg/testtool` 读取完整测试环境名，不感知环境名是静态配置还是动态生成。
* 将 deploy 配置中的固定环境 scope 与 guitar 运行时生成的 run 标识组合成完整环境名。

最终效果：大型测试计划只声明“要部署哪个 test deploy”，实际测试环境由 `guitar run` 在执行时生成、部署、注入测试进程并清理。

## 范围

本方案覆盖以下内容：

* `test` 类型 `deploy.yaml` 的动态环境名格式。
* `deploy apply` 对动态环境名占位符的运行时赋值参数。
* `guitar validate` 对动态测试环境名的静态校验。
* `guitar run` 的 run 标识生成、部署、测试环境变量注入和清理流程。
* `testplan.yaml` 删除 suite 级 `env` 字段后的模型调整。

本方案不包括：

* 修改 deploy service 的环境名存储模型。
* 支持多个占位符或任意模板表达式。
* 支持 suite 并行执行、重试策略或共享测试环境。
* 变更 `pkg/testtool` 的环境变量协议。

## 当前问题

现有 `design/guitar_yaml_testplan.md` 中，suite 需要显式配置：

```yaml
suites:
  - name: default
    env: game.lt
    deploy: //projects/game/testplan/test_deploy.yaml
```

同时 `deploy.yaml` 中也有完整环境名：

```yaml
name: game.lt
type: test
```

这种模型存在以下问题：

* `suite.env` 与 `deploy.name` 需要使用者手工保持一致，容易漂移。
* 多次大型测试使用同一个 `test` 环境名时会互相覆盖。
* 测试失败后如果清理不完整，下一次测试可能复用脏环境。
* 测试计划承担了运行时环境实例选择职责，和“测试计划描述要测什么”的定位不一致。

## 总体方案

将测试环境名拆成两部分：

* 固定 scope：写在 `test` 类型的 `deploy.yaml` 中，由项目或服务决定。
* 动态 run：由 `guitar run` 在每个 suite 执行时随机生成。

`test` 类型 deploy 的 `name` 使用固定格式：

```yaml
name: {scope}.{{run}}
type: test
```

例如：

```yaml
name: game.{{run}}
type: test
```

`{{run}}` 是唯一支持的占位符，表示本次 suite 执行的动态 run 标识。`guitar run` 生成 run 值后调用：

```bash
deploy apply --run lt3x8q2 //projects/game/testplan/test_deploy.yaml
```

deploy 工具在解析 deploy 配置后将 `{{run}}` 替换为 `lt3x8q2`，最终完整环境名为：

```text
game.lt3x8q2
```

随后 guitar 将完整环境名注入测试进程：

```bash
TESTTOOL_ENV=game.lt3x8q2
```

测试结束后 guitar 使用完整环境名清理：

```bash
deploy del game.lt3x8q2
```

## 模型设计

### deploy.yaml

`test` 类型 deploy 支持动态环境名：

```yaml
name: game.{{run}}
desc: game 大型测试环境
type: test
services:
  - artifact:
      path: //projects/game/service.yaml
      name: gateway
    http:
      hostnames:
        - game.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /v1
```

约束：

* 只有 `type: test` 允许使用 `{{run}}`。
* `prod` 和 `dev` 类型禁止使用 `{{run}}`。
* `test` 类型 deploy 推荐使用 `name: {scope}.{{run}}`。
* 固定 scope 必须满足现有 scope 命名规则：`^[a-z][a-z0-9]{0,7}$`。
* `{{run}}` 替换后的值必须满足现有 env name 命名规则：`^[a-z][a-z0-9]{0,7}$`。
* 不支持多个 `{{run}}`，不支持其他占位符。

### testplan.yaml

suite 删除 `env` 字段：

```yaml
name: game-session-large-test
description: game-session HTTP REST 接口测试

suites:
  - name: default
    deploy: //projects/game/testplan/test_deploy.yaml
    endpoint:
      http:
        public: https://game.liukexin.com
    cases:
      - //projects/game/testplan:testplan_test
```

`env` 不再由测试计划配置，而是由 guitar 在运行时生成并注入。

### run 标识

run 标识由 guitar 按 suite 生成，每个 suite 一个独立值。

推荐格式：

```text
lt + 6 位 lowercase base36 随机串
```

示例：

```text
lt3x8q2
```

设计理由：

* 以字母开头，满足 deploy env name 命名约束。
* 总长度 8，满足现有 `^[a-z][a-z0-9]{0,7}$` 约束。
* 带 `lt` 前缀，便于从 deploy list 或日志中识别大型测试临时环境。
* 每个 suite 独立生成，符合现有 guitar “suite 间独立、逐个清理”的执行模型。

## 代码分层

### tools/deploy

deploy 负责动态环境名模板的解析、校验和替换。

建议职责划分：

* deploy 配置解析层：读取 `deploy.yaml`，保留原始 `name`。
* deploy 校验层：校验 `type` 与 `name` 中 `{{run}}` 的组合是否合法。
* deploy apply 命令层：接收 `--run` 参数，并在发送 desired state 前生成最终完整环境名。

`deploy apply` 参数：

```bash
deploy apply [--run <run>] <deploy.yaml>
```

行为：

* `name` 不包含 `{{run}}` 时，不要求 `--run`。
* `name` 包含 `{{run}}` 且未传 `--run` 时，直接报错并中止。
* `--run` 不满足 env name 命名规则时，直接报错并中止。
* 替换后完整环境名继续复用现有完整环境名校验逻辑。

### tools/guitar

guitar 负责测试执行期的 run 标识生成和完整生命周期编排。

`guitar validate`：

* 校验 testplan YAML 结构合法。
* 校验 suite 不再配置 `env` 字段。
* 校验 deploy 路径可解析。
* 校验 deploy 配置 `type == test`。
* 校验 deploy `name` 使用 `{scope}.{{run}}` 格式。
* 校验 endpoint host 能在 deploy HTTP hostname 中找到。

`guitar run`：

1. 执行与 `validate` 相同的静态校验。
2. 按 suite 顺序执行。
3. 为当前 suite 生成 run 标识。
4. 根据 deploy `name` 计算完整环境名。
5. 执行 `deploy apply --run <run> <deploy-path>`。
6. 为测试 case 注入 `TESTTOOL_ENV=<scope>.<run>` 和 endpoint 环境变量。
7. 顺序执行 `bazel test --config=largetest <target>`。
8. 无论部署或测试是否成功，都执行 `deploy del <scope>.<run>`。
9. 任一 suite 失败后停止后续 suite。

### pkg/testtool

`pkg/testtool` 不需要感知动态环境名，只继续读取 guitar 注入的运行时变量：

```bash
TESTTOOL_ENV=<scope>.<run>
TESTTOOL_ENDPOINT_HTTP_PUBLIC=https://game.liukexin.com
```

## 关键细节

### 为什么占位符叫 `{{run}}`

`{{env}}` 容易和以下概念混淆：

* deploy 完整环境名 `{scope}.{env_name}`。
* HTTPRoute 访问时使用的 `env` header。
* 测试进程中的 `TESTTOOL_ENV`。
* artifact 环境变量 `artifact.env`。

`{{run}}` 表达的是“本次 suite 执行实例标识”，和 guitar 的运行行为直接对应，语义更窄、更稳定。

### 为什么每个 suite 生成一个 run

现有 guitar 执行模型是 suite 串行、suite 间独立、每个 suite 独立清理。每个 suite 生成独立 run 可以保证：

* suite 之间不会共享脏环境。
* cleanup 范围清晰，只删除当前 suite 的环境。
* 后续如果支持 suite 并行，环境名模型不需要重新设计。

### 为什么由 deploy 替换占位符

deploy 是环境名合法性的入口，也是最终发送 desired state 的工具。由 deploy 处理 `{{run}}` 可以保证：

* guitar 不需要改写 deploy YAML 文件。
* 其他执行器也能复用 `deploy apply --run`。
* deploy 对环境名的校验逻辑保持集中。

### 日志要求

`guitar run` 在每个 suite 开始时应输出：

* suite 名称。
* 生成的 run 标识。
* 最终完整环境名。
* deploy 路径。

示例：

```text
suite default: run=lt3x8q2 env=game.lt3x8q2 deploy=//projects/game/testplan/test_deploy.yaml
```

这样测试失败后可以直接定位残留环境并手工清理。

### 失败语义

* deploy 失败：当前 suite 失败，仍尝试清理完整环境名。
* case 失败：当前 suite 失败，仍尝试清理完整环境名。
* cleanup 失败：保留原始失败结果，同时报告 cleanup 失败。
* run 标识生成失败：当前 suite 失败，不执行 deploy。
* `deploy apply --run` 缺失或非法：deploy 直接失败，不发送 desired state。

## 兼容性与迁移

迁移前：

```yaml
# testplan.yaml
suites:
  - name: default
    env: game.lt
    deploy: //projects/game/testplan/test_deploy.yaml
```

```yaml
# test_deploy.yaml
name: game.lt
type: test
```

迁移后：

```yaml
# testplan.yaml
suites:
  - name: default
    deploy: //projects/game/testplan/test_deploy.yaml
```

```yaml
# test_deploy.yaml
name: game.{{run}}
type: test
```

迁移策略：

* 新的 `guitar validate` 应拒绝 suite 中继续配置 `env`。
* `test` 类型 deploy 在 guitar 场景下应使用 `{{run}}`。
* 非 guitar 场景仍可通过 `deploy apply --run <run>` 手工部署动态测试环境。

## 验收标准

实现完成后需要满足：

* `guitar validate` 接受删除 `env` 且 deploy 使用 `name: game.{{run}}` 的测试计划。
* `guitar validate` 拒绝 suite 继续配置 `env` 的测试计划。
* `guitar validate` 拒绝 `type: test` 但 deploy name 不是 `{scope}.{{run}}` 的 guitar 测试部署。
* `deploy apply` 在 deploy name 包含 `{{run}}` 但未传 `--run` 时失败。
* `deploy apply --run lt3x8q2` 能将 `game.{{run}}` 解析为 `game.lt3x8q2`。
* `guitar run` 每个 suite 生成独立 run，并向测试进程注入 `TESTTOOL_ENV=<scope>.<run>`。
* `guitar run` 无论 case 成功或失败，都调用 `deploy del <scope>.<run>` 清理环境。
* 相关 Go 单元测试通过 `bazel test` 执行。

## 未来规划

以下能力不纳入本次实现，可后续单独设计：

* 支持用户指定 run 前缀或固定 run 值，便于本地调试复现。
* 支持 suite 并行执行。
* 支持测试失败后按参数保留环境用于排查。
* 支持在 deploy service 中标记环境来源为 guitar large test run。
