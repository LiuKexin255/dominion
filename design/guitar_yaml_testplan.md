# Guitar YAML 大型测试方案

## 目标

本方案用于将仓库内大型测试计划从 `md` 形式收敛为 `yaml` 形式，并实现 `tools/guitar` 作为统一 CLI 执行器，目标是：

* 让大型测试计划具备稳定、结构化、可校验的配置模型，避免继续依赖 agent 对 markdown 的语义解析。
* 让大型测试的执行流程收敛到 `guitar` CLI 中，形成统一的“校验 → 部署 → 执行 → 清理”闭环。
* 让测试代码通过 `pkg/testtool` 统一读取测试环境与入口信息，避免各个大型测试重复定义环境变量读取逻辑。
* 在不扩大本期范围的前提下，保留现有 Bazel 大型测试 target、`deploy` 工具链与测试代码组织方式。

## 范围

本方案仅覆盖以下内容：

* `testplan` 从 markdown 迁移到 YAML 的配置模型
* `tools/guitar` CLI 的 `validate` / `run` 语义
* `pkg/testtool` 的职责、API 和环境变量约定
* `guitar` 对 `deploy` 配置的静态校验与执行流程
* 存量大型测试计划迁移所需的约束与规则

本方案不包括：

* `.opencode/skills/testplan` 的改造
* `deploy` 工具对环境名模型的调整
* 自动从 `deploy` 配置推导测试 `env`
* 更复杂的 suite 依赖、并行执行、重试策略配置化
* 测试 SDK 扩展（如统一 HTTP client、请求 helper、断言 helper）

## 当前问题

当前仓库的大型测试存在以下问题：

* `styles/large_test.md` 规定的测试计划格式是 markdown，执行依赖 agent 按语义提取 `name` / `deploy` / `cases`。
* `.opencode/skills/testplan/SKILL.md` 承担了实际执行流程，但该流程不是代码实现，难以在本地、CI、agent 之间稳定复用。
* 现有大型测试代码（如 `projects/game/testplan/interface_test.go`）各自读取 `SUT_HOST_URL` / `SUT_ENV_NAME`，缺少统一 helper。
* 当前 `tools/guitar/` 只有说明文件和示例 YAML，尚无真正执行逻辑。
* markdown testplan 中既包含执行信息，也包含说明文字；如果直接替换成极简 YAML，会丢失“测试计划”的可读性与意图表达。

因此，本次调整不能只做“文件格式替换”，而需要同时收敛**计划模型**、**执行入口**与**测试运行时读取方式**。

## 总体方案

本方案的总体收敛方向如下：

1. 使用 YAML 作为 testplan 的唯一配置格式。
2. 使用 `guitar` 作为 testplan 的唯一 CLI 执行器。
3. `guitar run` 固定执行“先校验，再执行”的闭环，不向用户暴露复杂执行策略。
4. 测试代码通过 `pkg/testtool` 读取 `env` 与 `endpoint`，不再直接约定旧的 `SUT_*` 变量名。
5. suite 执行模型固定为：**串行、fail-fast、suite 间独立、无依赖、每个 suite 独立清理**。

## 最终模型

## YAML 配置模型

### 顶层结构

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

### 字段说明

#### 顶层字段

* `name`：测试计划名
* `description`：测试计划说明，用于保留原 markdown 中的测试目标、范围或补充说明
* `suites`：测试套件列表

#### `suite` 字段

* `name`：suite 名称
* `deploy`：部署配置路径，支持相对路径或 `//` 开头的 workspace 完整路径
* `endpoint`：测试入口集合，当前先支持 `http`
* `cases`：大型测试 Bazel target 列表，保留顺序执行

### `endpoint` 模型

当前 `endpoint` 采用分协议结构：

```yaml
endpoint:
  http:
    public: https://game.liukexin.com
    admin: https://admin.game.liukexin.com
```

设计原则：

* `endpoint` 不设置“主入口”概念。
* 测试代码必须显式按协议和名称读取入口。
* 某个入口缺失时，测试代码直接返回错误，不做猜测或回退。
* 当前先支持 `http`；后续如需 `grpc`，在相同模型下扩展。

### endpoint 名称约束

`endpoint.<protocol>.<NAME>` 中的 `<NAME>` 必须满足：

* 仅允许字母和数字
* 必须以字母开头

也即使用如下约束：

* 合法示例：`public`、`admin2`、`internal1`
* 非法示例：`2admin`、`admin-api`、`admin_api`

这样可以避免特殊字符参与环境变量展开，降低 CLI 与测试代码间协议复杂度。

## 执行语义

## `guitar validate`

`guitar validate <plan.yaml>` 只做静态检查，不执行部署和测试。

校验目标：

* testplan YAML 结构合法
* 必填字段完整
* `cases` 非空
* `deploy` 路径可解析
* `deploy` 配置合法
* `deploy.type == test`
* `endpoint.http.*` 中声明的 host 能在 `deploy` 配置的 `http.hostnames` 中找到

### deploy 弱校验边界

本期只对 `http.hostnames` 做弱校验：

* `endpoint.http.<NAME>` 是完整 URL
* 取其 host 部分
* 若该 host 不在 `deploy` 中声明的 `services[].http.hostnames` 集合内，则报错

本期不校验：

* path 是否匹配
* backend 是否匹配
* 是否存在默认入口
* `env` 字段是否存在（已移除，若存在则校验失败）

## `guitar run`

`guitar run <plan.yaml>` 固定执行以下流程：

1. 先执行与 `validate` 相同的校验
2. 按 `suites` 顺序逐个执行
3. 对每个 suite：
   1. `deploy apply <deploy-path> --run <run-name>`
   2. 为当前 suite 的每个 case 注入运行时环境变量
   3. 顺序执行 `bazel test --config=largetest <target>`
   4. 无论成功还是失败，都执行环境清理
4. 任一 suite 失败后立即停止后续 suite 执行

### 固定执行策略

本期执行策略不暴露给 testplan 使用者配置：

* suites 串行执行
* fail-fast
* suite 间独立、无依赖
* case 顺序执行
* 清理始终执行

### 失败语义

* deploy 失败：suite 立即失败，`guitar run` 返回非 0
* case 失败：suite 立即失败，`guitar run` 返回非 0
* cleanup 失败：仍保留原主结果，同时单独报告 cleanup 失败
* 某个 suite 成功后，才进入下一个 suite

## 环境变量协议

## 统一命名

`guitar` 与 `pkg/testtool` 共享同一套环境变量展开规则。

建议使用以下固定前缀：

* `TESTTOOL_ENV`
* `TESTTOOL_ENDPOINT_<PROTOCOL>_<NAME>`

其中：

* `<PROTOCOL>` 使用大写协议名，如 `HTTP`
* `<NAME>` 使用大写入口名

例如，以下 endpoint 配置：

```yaml
endpoint:
  http:
    public: https://game.liukexin.com
    admin2: https://admin.game.liukexin.com
```

其中 `TESTTOOL_ENV` 的值由 `guitar run` 为每个 suite 自动生成并注入，不再在 YAML 中声明。

当 `guitar run` 为 suite 生成环境名 `game.lt3x8q2` 时，展开为：

```bash
TESTTOOL_ENV=game.lt3x8q2
TESTTOOL_ENDPOINT_HTTP_PUBLIC=https://game.liukexin.com
TESTTOOL_ENDPOINT_HTTP_ADMIN2=https://admin.game.liukexin.com
```

### 展开规则

由于 `<NAME>` 已限制为“字母开头，且仅包含字母和数字”，因此环境变量展开不需要额外字符转换规则：

* `public` → `PUBLIC`
* `admin2` → `ADMIN2`

## 模型设计

## `pkg/testtool`

`pkg/testtool` 的职责收敛为：**读取并校验大型测试运行时上下文**。

本期不承担：

* HTTP client 创建
* 请求 header 拼装
* deploy 配置推导
* endpoint 默认值选择
* 业务级断言 helper

### API 设计

建议提供以下 API：

```go
func Env() (string, error)
func MustEnv() string
func Endpoint(protocol, name string) (string, error)
func MustEndpoint(protocol, name string) string
```

设计原则：

* `Env` / `Endpoint` 返回显式错误，便于测试代码自行处理
* `MustEnv` / `MustEndpoint` 直接 panic，便于在测试初始化阶段快速失败
* `protocol` 与 `name` 都由调用方显式指定，不提供默认入口

### 实现风格

参考仓库现有 `pkg/solver/env.go` 风格：

* 使用包级 `lookupEnv = os.LookupEnv`
* 提供 required-env helper
* 对缺失或空白值直接报错
* 测试通过替换 `lookupEnv` 注入假数据

这样可以保证：

* API 小而稳定
* 单测容易覆盖
* 与仓库现有 env helper 风格保持一致

## `guitar`

### 代码分层

建议 `tools/guitar` 按如下职责分层：

* `cmd`：CLI 入口，负责参数解析与退出码控制
* `pkg/config`：解析 YAML testplan
* `pkg/validate`：执行静态校验，包括 deploy.type / endpoint hostname 校验
* `pkg/run`：组织 validate → deploy → bazel test → cleanup 的执行流程
* `pkg/env` 或直接复用 `pkg/testtool` 的环境变量展开逻辑：将 suite 配置转成测试进程环境变量

### 为什么允许 `guitar` 依赖 `pkg/testtool`

`guitar` 与测试代码需要共享同一套环境变量协议。将协议逻辑收敛到 `pkg/testtool` 内可以避免：

* CLI 和测试代码各自维护一套变量名拼接规则
* 将来调整协议时出现一边修改、一边遗漏

因此，本期允许：

* `pkg/testtool` 提供环境变量 key 生成与读取逻辑
* `guitar` 直接引用这些规则做注入

## 关键细节

## 与现有 Bazel 大测规则的关系

本方案**不改变**现有大型测试 target 组织方式：

* 继续使用 `go_largetest`
* 继续使用 `bazel test --config=largetest <target>` 执行
* `BUILD.bazel` 中不再要求显式维护旧的 `SUT_HOST_URL` / `SUT_ENV_NAME`
* 测试代码改为从 `pkg/testtool` 读取 `env` 与 `endpoint`

## 与现有 deploy 工具链的关系

本方案**不替代** `deploy`，而是复用现有工具链：

* 部署仍使用 `deploy apply`
* 清理仍使用 `deploy del`
* `guitar` 只负责 orchestration，不重新实现 deploy 功能

## description 字段的作用

`description` 用于承接原 markdown testplan 中的说明性内容，避免 YAML 退化为“只剩执行清单”。

本期不要求把 markdown 中所有层级结构原样映射到 YAML，但至少要求：

* testplan 仍能表达测试目标
* testplan 仍能表达测试范围或补充说明

## 决策详情

### 决策 1：Guitar 是 CLI 执行器，不处理 skill 集成

选择：本期只设计并实现 CLI 执行器，不把 skill 作为方案范围的一部分。

原因：

* 避免把“工具执行模型”与“agent 调用方式”耦合在一起
* 先让本地、CI、agent 共享同一执行器
* skill 后续可以作为 CLI 的上层调用入口单独处理

### 决策 2：YAML 只增加 `description`、`endpoint` 等已确认字段，不扩展执行策略字段

选择：不新增 retry、parallel、depends_on、cleanup_policy 等字段。

原因：

* 本期执行语义已固定为串行、fail-fast、独立清理
* 不应把当前明确不开放给使用者的行为暴露成 schema
* 保持 testplan 配置简单，避免一次性引入太多策略语义

### 决策 3：endpoint 使用分协议 map，而不是单字符串

选择：

```yaml
endpoint:
  http:
    public: https://...
```

原因：

* 允许一个 suite 同时声明多个入口
* 允许未来按协议扩展到 `grpc`
* 与测试代码“显式按 key 选择入口”的风格一致

### 决策 4：不设置默认入口

选择：测试代码必须显式调用 `Endpoint(protocol, name)`。

原因：

* 避免“默认 public / default”这类隐式约定
* 避免随着入口增多产生歧义
* 缺失时直接报错，比回退更安全

### 决策 5：endpoint 校验先收敛到 hostname 级别

选择：本期只校验 endpoint host 是否出现在 deploy `http.hostnames` 中。

原因：

* 这是当前 deploy 配置中最稳定、最容易静态校验的信息
* path、backend 等更细粒度语义不属于本期必要约束
* 能在低复杂度下提前发现明显配置错误

### 决策 6：`env` 由 `guitar run` 自动生成

选择：`env` 不再作为 suite 字段，由 `guitar run` 为每个 suite 自动生成并注入测试进程。

原因：

* 避免使用者在 YAML 中重复声明环境名，减少配置冗余
* 确保测试进程拿到的环境名与 deploy 实际使用的运行名一致
* 将环境名管理收敛到 `guitar run` 入口，便于统一管控

## 迁移规则

现有 `testplan/*.md` 迁移到 YAML 时，遵循以下规则：

1. 顶层 `name` 迁移为 YAML `name`
2. markdown 中的说明、Scope 或补充描述，收敛到 `description`
3. `deploy` 原样迁移到 suite `deploy`
4. `cases` 原样迁移到 suite `cases`
5. 原先由 `BUILD.bazel` 维护的 `SUT_HOST_URL` / `SUT_ENV_NAME`，改为：
   * 环境名由 `guitar run` 自动生成，通过 `TESTTOOL_ENV` 注入
   * suite 内定义 `endpoint.http.<NAME>`
   * 测试代码改用 `pkg/testtool`

## 验收标准

当本方案落地后，应满足以下结果：

* 存在结构化 YAML testplan，并可由 `guitar validate` 校验
* `guitar run` 能执行单个或多个 suite 的完整闭环
* 任一 suite 失败时，后续 suite 不继续执行
* 当前 suite 在失败时仍会清理部署环境
* 测试代码可通过 `pkg/testtool` 读取 `env` 与命名入口
* endpoint 命名和环境变量展开规则在 `guitar` 与测试代码中保持一致

## 后续规划

以下内容不在本期范围内，可在后续单独设计：

* `endpoint.grpc` 的具体落地
* suite 级并行执行
* retry / wait / cleanup 策略配置化
* skill 对 `guitar` 的调用集成
