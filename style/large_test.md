# 大型测试

## 测试对象

以下对象需要编写大型测试：

* `grpc` 服务
* `http` 服务

大型测试按覆盖范围可分为：

* 中型测试：面向单个服务，一般放在对应服务目录下
* 大型测试：面向整个系统，一般放在系统目录下

中型测试也属于大型测试的一种，统一使用 `testplan` 进行编排，结构和执行方式保持一致。

## 测试计划

大型测试通过`测试计划`进行编排。测试计划通常与其使用物料（例如部署配置和测试用例）**一起**放在 `testplan` 目录，使用 YAML 格式定义，通过 `guitar` 工具执行。

测试计划包含以下内容：

* name：测试计划名
* suites：测试套件列表，每个 suite 包含：
  * env：测试环境标识（格式：scope.env，如 game.lt）
  * deploy：被测系统部署配置，通过 `deploy(//tools/release/deploy)` 工具进行部署，测试完成后移除
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

### 测试计划数量

* **每个被测系统只维护一份测试计划 YAML**，通过多个 `suite` 组织不同的部署拓扑或测试范围。
* 不要为单个需求、迭代步骤或验收场景新建独立的测试计划 YAML。新功能验证应当作为新 `suite` 或新 `case` 加入既有计划。
* 仅当被测系统的部署拓扑发生根本性变化（例如引入与既有 SUT 完全隔离的全新子系统）时，才允许新建独立的测试计划，并在该计划旁附说明。

## 测试用例

* 大型测试代码使用 `golang` 编写，**必须遵守 `style/golang.md` 中的单元测试规范**（命名风格、表驱动、helper 使用、`given/when/then` 结构、禁止在用例中塞断言等）。本文件仅补充大型测试特有的约定。
* 大型测试的 `target` 使用专用的 `bazel rule`，例如 `golang` 使用 `go_largetest(//tools/dev/go:defs.bzl)`。
* 同一目录下若存在多个大型测试 `target`，**至少有一个**必须使用 `gazelle` 生成的默认名称（`{package_name}_test`），以防止 gazelle 重复生成 `go_unittest`。
* 测试用例需要根据实际情况设置 `size`。
* 使用 `guitar run <plan.yaml>` 执行测试计划，自动完成部署、测试、清理闭环。
* 测试代码通过 `pkg/testtool` 读取环境变量获取 SUT 信息。
* 测试用例中发送 HTTP 请求，需要用 `common/gopkg/otel/tracecontext` 设置并打印 `trace_id` 便于日志排查。

### 测试组织

测试用例（`*_test.go` 文件、`*_test` binary、`suite`、`case`）的拆分**必须以被测模块/功能为维度**，不得以需求序号、迭代步骤、spec 编号或验收场景序号等交付物维度组织。

**按模块拆分**：

* 一个 `*_test.go` 文件聚焦一个被测模块；跨模块的端到端验证应拆分为多个聚焦的测试，分别归入对应模块文件。
* 不要为了"凑齐一组验收场景"把多个模块的测试堆在同一个文件。
* 共享的构造/断言代码放入共享 helper 文件（如 `helpers_test.go`），避免在多个文件中重复定义。

**按 suite 编排**：

* 一个 `suite` 对应一组部署拓扑与一组聚焦用例。
* 不同 `suite` 可以复用同一个测试 binary，通过不同的测试函数筛选关注点。

**何时新增测试**：

* 新增功能 → 在**对应模块**的既有测试文件中追加用例；若该模块尚无测试文件，再新建以模块命名的文件，并在既有测试计划中加入对应 `case` 或 `suite`。
* 修改既有功能 → 更新对应模块的测试；不要为了"不破坏旧测试"而新建平行文件。
* 验收文档中的"场景"只是测试覆盖的输入清单，**不是**测试代码的组织结构。翻译为测试时按模块归位，不要按场景编号成文件。

> FOR Agent: 使用 `testplan` SKILL 来执行大型测试。

## SUT

* 被测系统默认不进行持久化（`deploy` 配置里不设置 `persistence`）
* 根据模块合理拆分 SUT，避免将整个系统堆在一个 SUT 当中。

### GRPC & HTTP

* 使用 `http` 接口对 `grpc` 系统进行测试。
* 可以使用 `grpc-gateway` 组件为 `grpc` 服务提供 `http` 服务。
* 如果被测服务是纯 `grpc` 服务，则在 `testplan` 中增加一个 `grpc-gateway` 服务作为 adapter，用于将 `grpc->http`。这类 `http` 统一使用 `apitest.liukexin.com` 域名，并且通过 prefix 确保唯一，例如 `apitest.liukexin.com/{app}/{service}`。然后在这个 adapter 服务当中进行路径转换。

### 环境变量

使用 `guitar run` 执行测试计划时，环境变量由 guitar 自动注入，测试代码通过 `pkg/testtool` 读取：

* `TESTTOOL_ENV`：测试环境标识
* `TESTTOOL_ENDPOINT_<PROTOCOL>_<NAME>`：测试入口 URL（如 `TESTTOOL_ENDPOINT_HTTP_PUBLIC`）

```go
import "dominion/common/gopkg/testtool"

sutHostURL := testtool.MustEndpoint("http", "public")
envName := testtool.MustEnv()
```

旧的 `SUT_HOST_URL` / `SUT_ENV_NAME` 环境变量已废弃。

## 反模式

以下做法虽然能让测试"跑起来"，但会破坏可维护性，**禁止**采用：

1. **按交付物维度组织测试**：以需求序号、迭代步骤或 spec 编号命名测试文件（如 `stepN_test.go`、`<spec-id>_test.go`）；或为单个需求新建独立测试计划 YAML，与既有系统测试计划并存。
2. **一个文件装多模块**：把多个被测模块的逻辑堆在同一个 `*_test.go` 中，仅因它们恰好出现在同一份验收文档里。
3. **复制 helper 而非复用**：在新测试文件里重新定义既有共享 helper 已有的构造器、常量或断言逻辑。
4. **平行测试计划**：为单个需求新建一份与系统测试计划并存的 YAML，且引用相同 deploy 配置——应改为在既有计划中新增 `suite` 或 `case`。
