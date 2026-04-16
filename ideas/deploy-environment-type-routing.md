# deploy：基于 Environment.type 区分 prod/test/dev 路由入口

## 目标

将 `projects/infra/deploy/` 中环境用途从隐含约定收敛为显式模型，在 `Environment` 上增加 `type` 字段，取值为 `prod`、`test`、`dev`，并基于该字段控制 HTTPRoute 的匹配条件。

预期效果：

1. deploy 控制面能够明确表达一个环境的用途，而不是依赖命名约定推断。
2. 当同一个系统同时部署多套环境时，非正式环境可以通过 `env={scope}.{env_name}` 请求头区分入口，避免与正式环境共享主路由。
3. 正式环境保持无额外 header 条件的路由行为，作为默认入口承接方式。
4. 保持当前 deploy service 的整体分层不变，不引入新的路由控制面或额外调度机制。

## 范围

本方案只包含：

1. 在 `Environment` 上增加 `type` 枚举字段。
2. 在业务代码中校验 `type` 不允许为 `UNSPECIFIED`。
3. 约束 `type` 创建后不可变。
4. 在生成 HTTPRoute 时，对 `test/dev` 环境追加 `env={scope}.{env_name}` 的 header match。

本方案不包含：

1. 新的环境唯一性规则。
2. 基于 `type` 的额外资源配额、镜像、profile 或副本策略。
3. 新的入口网关、额外域名或路由拓扑改造。
4. 对已有 `scope` / `env_name` 命名规则的调整。

## 当前问题

当前 `projects/infra/deploy/deploy.proto` 中的 `Environment` 只有资源名、描述、期望状态和观察状态，没有表达“正式 / 测试 / 开发”用途的字段。

与此同时，`projects/infra/deploy/runtime/k8s/builder.go` 中的 `BuildHTTPRoute()` 生成的 Gateway API `HTTPRouteMatch` 只包含 path 匹配，不包含 header 条件。

因此，当同一个系统需要同时部署多套环境并共享同一个入口时，当前模型无法在 deploy 控制面中直接表达：

1. 哪套环境是正式环境。
2. 哪些环境需要依赖 header 进行流量区分。
3. 这些规则如何稳定地进入最终生成的 HTTPRoute。

## 模型设计

### Environment.type 模型

在 `Environment` 上新增 `type` 字段，并定义枚举：

1. `PROD`：正式环境。
2. `TEST`：测试环境。
3. `DEV`：开发环境。
4. `UNSPECIFIED`：仅作为 proto 零值保留，不允许业务创建时使用。

该字段表达的是环境用途，而不是环境运行状态。

也就是说：

* `EnvironmentState` 继续表达 `PENDING / RECONCILING / READY / FAILED / DELETING`。
* `Environment.type` 表达环境属于 `prod / test / dev` 中的哪一类。

### 不可变性模型

`Environment.type` 创建后不可变。

原因：

1. 该字段决定环境入口路由的匹配方式，属于环境身份的一部分。
2. 若运行中把 `prod` 改成 `test/dev`，会直接改变 HTTPRoute 匹配条件，造成入口行为跳变。
3. 当前 `UpdateEnvironment()` 语义本身就只允许更新 `desired_state`，与“type 为创建时确定的属性”一致。

因此，本方案要求：

* `CreateEnvironment` 允许写入 `type`。
* `UpdateEnvironment` 不允许修改 `type`。
* 若更新请求显式携带 `type`，返回业务错误，而不是静默忽略。

### HTTPRoute 匹配模型

路由生成规则收敛为：

1. `Environment.type = PROD`：保持当前行为，只生成 path 匹配。
2. `Environment.type = TEST/DEV`：若 artifact 配置了 HTTP，则在每个 `HTTPRouteMatch` 中追加一个 header 条件：
   * `name = env`
   * `value = {scope}.{env_name}`

其中 `{scope}.{env_name}` 直接复用 `projects/infra/deploy/domain/environment_name.go` 中 `EnvironmentName.Label()` 的现有语义，不新增第二套环境标识格式。

该模型表达的是：

* 正式环境占据默认入口。
* 非正式环境必须通过显式 header 才能命中对应路由。

## 代码分层

### 协议层：`projects/infra/deploy/deploy.proto`

协议层负责定义 `Environment.type` 的外部 API 语义。

建议新增：

1. `EnvironmentType` 枚举。
2. `Environment.type` 字段。

协议层只负责对外表达模型。

### 领域层：`projects/infra/deploy/domain/`

领域层负责：

1. 维护 `Environment.type` 的领域模型。
2. 校验创建时 `type != UNSPECIFIED`。
3. 明确 `type` 是创建后不可变的环境属性。

领域层不负责理解 Gateway API header match 细节，但可以提供“当前环境是否需要 header 路由约束”的判断基础。

### 存储层：`projects/infra/deploy/storage/`

存储层负责：

1. 持久化 `Environment.type`。
2. 保持 `Environment.type` 与领域模型之间的一致映射。

### 运行时层：`projects/infra/deploy/runtime/k8s/`

运行时层负责：

1. 接收环境类型信息。
2. 在生成 `HTTPRoute` 时，将 `TEST/DEV` 环境转换为带 header 条件的 Gateway API 匹配规则。

这是本方案最核心的分层决策：

* **domain 表达“环境类型是什么”。**
* **runtime 决定“这种类型如何映射为 Kubernetes HTTPRoute 规则”。**

## 关键细节

### 1. 最窄修改点

HTTPRoute header 条件的最窄落点在：

* `projects/infra/deploy/runtime/k8s/builder.go`
* `BuildHTTPRoute()` 中构造 `gatewayv1.HTTPRouteMatch` 的位置

当前代码只设置了 `Path`，没有设置 `Headers`。因此只需把环境类型透传到 `HTTPRouteWorkload`，即可在这里条件性追加：

* `PROD`：不加 `Headers`
* `TEST/DEV`：加 `Headers: [{name: env, value: scope.env}]`

### 2. header 值来源

header 值不重新拼接、不依赖调用方传入，而是统一使用：

* `EnvironmentName.Label()`

这样可以保证：

1. label、命名、路由条件使用同一套环境标识。
2. 不会出现某层用 `scope-env`，另一层用 `scope.env` 的格式分裂。

### 3. 更新行为

`UpdateEnvironment()` 当前只处理 `desired_state` 更新。

本方案下应进一步明确：

1. 更新请求如果未携带 `type`，按当前流程仅更新 `desired_state`。
2. 更新请求如果显式携带 `type`，直接返回错误，说明该字段不可变。

这样可以避免“调用方以为改成功，服务端却默默忽略”的接口歧义。

### 4. 入口区分前提

本方案能成立的前提是：

* 当同一个系统部署两套或多套环境时，请求入口能够为非正式环境注入 `env={scope}.{env_name}` header。

如果上游没有注入该 header，则 `TEST/DEV` 环境的 HTTPRoute 不会命中。

也就是说，本方案解决的是“deploy 控制面如何把环境用途映射到入口规则”，不负责解决“谁来注入 header”。

## 决策详情

### 决策一：使用 `type`，不继续使用 `is_default`

选择：**使用 `Environment.type` 枚举表达环境用途**。

原因：

1. 本次目标是区分正式环境与非正式环境入口，不是维护某个“默认环境”概念。
2. `prod/test/dev` 比 `default/non-default` 更稳定，也更接近实际业务语义。
3. 可以避免“是否允许多个默认环境”“删除默认环境后怎么办”等额外规则。

### 决策二：`type` 创建后不可变

选择：**`Environment.type` 只能在创建时确定**。

原因：

1. 该字段会直接影响生成的 HTTPRoute 匹配规则。
2. 运行中修改该值的代价太高，且容易引发流量入口切换。
3. 与当前 `UpdateEnvironment` 只更新 `desired_state` 的模型一致。

### 决策三：业务代码拒绝 `UNSPECIFIED`

选择：**协议保留 `UNSPECIFIED`，业务创建时拒绝该值**。

原因：

1. proto 枚举需要零值。
2. 业务上不应允许一个“用途未定义”的新环境进入系统。
3. 这样既符合 protobuf 习惯，也保持业务语义明确。

### 决策四：只有 `TEST/DEV` 才增加 header match

选择：**`PROD` 保持原样，`TEST/DEV` 增加 `env={scope}.{env_name}` header 条件**。

原因：

1. 正式环境应继续承接默认入口。
2. 非正式环境本就需要显式区分入口，header 条件正好承担这个作用。
3. 该规则直接服务于“同一个系统部署两套时可以通过 header 区分入口”的目标。

## 预计改动范围

核心改动文件：

1. `projects/infra/deploy/deploy.proto`
   * 新增 `EnvironmentType` 枚举与 `Environment.type` 字段。
2. `projects/infra/deploy/domain/environment.go`
   * 给 `Environment` 与 `EnvironmentSnapshot` 增加类型字段及相关校验。
3. `projects/infra/deploy/handler.go`
   * 增加 proto/domain 的 type 转换，并在更新路径中拒绝修改 type。
4. `projects/infra/deploy/storage/mongo.go`
   * 持久化 `env_type` 并完成 domain/storage 的双向映射。
5. `projects/infra/deploy/runtime/k8s/model.go`
   * 让 `HTTPRouteWorkload` 携带环境类型信息。
6. `projects/infra/deploy/runtime/k8s/converter.go`
   * 将环境类型透传到 HTTPRoute workload。
7. `projects/infra/deploy/runtime/k8s/builder.go`
   * 在生成 HTTPRoute 时对 `TEST/DEV` 追加 header match。
8. 测试文件
   * `handler_test.go`
   * `domain/environment_test.go`
   * `domain/worker_test.go`
   * `storage/mongo_test.go`
   * `runtime/k8s/converter_test.go`
   * `runtime/k8s/builder_test.go`

## 验收标准

满足以下条件时，认为方案落地成功：

1. 新建环境时必须显式指定 `type`，且 `UNSPECIFIED` 会被业务校验拒绝。
2. `UpdateEnvironment` 不允许修改 `type`，调用方能得到明确错误。
3. `PROD` 环境生成的 HTTPRoute 与当前行为一致，不包含额外 `env` header 条件。
4. `TEST/DEV` 环境生成的每个 HTTPRoute match 都包含 `env={scope}.{env_name}` header 条件。
5. 当同一个系统部署多套环境时，只要入口注入正确 header，即可命中对应非正式环境路由。

## 风险与约束

1. 本方案依赖入口层为 `TEST/DEV` 环境请求注入 `env` header，否则非正式环境路由不会命中。
2. 若 `PROD` 与 `TEST/DEV` 共用 hostname，本方案本质上依赖 header 作为入口区分条件，因此需要网关、调用链和调试手段都理解这一规则。

## 未来规划

若后续出现以下需求，再单独扩展：

1. 基于 `type` 控制不同的资源配额、profile、默认副本或发布策略。
2. 对 `prod/test/dev` 引入更细粒度的入口策略，例如不同域名、不同网关或不同认证要求。
3. 在 API 中增加更丰富的环境元数据，而不仅仅是用途枚举。
