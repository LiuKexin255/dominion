# 大型测试

## 测试对象

以下对象需要编写大型测试：

* `grpc` 服务
* `http` 服务

## 测试计划

大型测试通过`测试计划`进行编排。测试计划通常与其使用物料（例如部署配置和测试用例）**一起**放在 `testplan` 目录，使用 YAML 格式定义，通过 `guitar` 工具执行。

测试计划包含以下内容：

* name：测试计划名
* suites：测试套件列表，每个 suite 包含：
  * env：测试环境标识（格式：scope.env，如 game.lt）
  * deploy：被测系统部署配置，通过 `deploy(//tools/deploy)` 工具进行部署，测试完成后移除
  * endpoint：测试入口 URL 映射（可选）
  * cases：测试用例列表，在部署完成后执行

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

详见 `design/guitar_yaml_testplan.md`。

## 测试用例

* 大型测试的 `target` 使用专用的 `bazel rule`，例如 `golang` 使用 `go_largetest(//tools/go:defs.bzl)`。
* 大型测试的 `target` 名最好使用 `gazelle` 生成的默认名称（`{package_name}_test`），以防止重复生成 `go_unittest`。
* 测试用例需要根据实际情况设置 `size`。
* 使用 `guitar run <plan.yaml>` 执行测试计划，自动完成部署、测试、清理闭环。
* 测试代码通过 `pkg/testtool` 读取环境变量获取 SUT 信息。

> FOR Agent: 使用 `testplan` SKILL 来执行大型测试。

## SUT

* 被测系统默认不进行持久化（`deploy` 配置里不设置 `persistence`）

### GRPC & HTTP

* 使用 `http` 接口对 `grpc` 系统进行测试。
* 可以使用 `grpc-gateway` 组件为 `grpc` 服务提供 `http` 服务。

### 环境变量

使用 `guitar run` 执行测试计划时，环境变量由 guitar 自动注入，测试代码通过 `pkg/testtool` 读取：

* `TESTTOOL_ENV`：测试环境标识
* `TESTTOOL_ENDPOINT_<PROTOCOL>_<NAME>`：测试入口 URL（如 `TESTTOOL_ENDPOINT_HTTP_PUBLIC`）

```go
import "dominion/pkg/testtool"

sutHostURL := testtool.MustEndpoint("http", "public")
envName := testtool.MustEnv()
```

旧的 `SUT_HOST_URL` / `SUT_ENV_NAME` 环境变量已废弃。
