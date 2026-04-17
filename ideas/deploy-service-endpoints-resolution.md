# deploy：中心化 service endpoints 解析能力

## 目标

在 `projects/infra/deploy/` 中增加一组面向 `app/service` 的中心化 endpoints 解析能力，用 deploy 作为环境权威控制面，并由服务端在查询时结合 Kubernetes `Service` / `EndpointSlice` 生成最终 `ip:port` 结果，替代当前 `pkg/solver` 在业务进程内直接扫描 k8s 的方式。

预期效果：

1. 服务发现从“每个 client 自己解析 k8s”收敛为“统一向 deploy 查询”，减少解析逻辑散落在业务侧。
2. deploy 可以在一个中心位置稳定实现“同环境优先、prod 环境缺失时降级”的解析策略，而不是让每个调用方自行拼装规则。
3. `pkg/solver` 从 k8s 解析器收敛为 deploy name-service client，业务调用方仍然保留 `app/service[:port_selector]` 这一轻量目标表示，并在 client 侧把 `port_selector` 解析为实际端口。
4. 解析结果除了 endpoints，还能明确暴露最终命中的 `scope` / `environment` / `resolution_mode`，便于调试、审计和后续权限治理接入。

## 范围

本方案只包含：

1. 在 `DeployService` 上新增一个 Get 风格的 `ServiceEndpoints` 资源读取接口。
2. 将 endpoints 视为 deploy 暴露的逻辑资源对象，而不是可独立增删改的注册资源。
3. 服务端在查询时实时读取 deploy 环境信息与 Kubernetes `Service` / `EndpointSlice`，生成最终 endpoints。
4. 定义同环境优先、prod 缺失时 fallback 的解析规则。
5. 将 `pkg/solver` 的目标格式从 `app/service[:port]` 收敛为 `app/service[:port_selector]`，其中 `port_selector` 支持 numeric port 与 named port 二选一；deploy 服务本身不理解 `port_selector`，只返回 service 端口映射。
6. 成功响应支持受限的 `view` 参数；失败响应通过 gRPC status details 返回详细信息。

本方案不包含：

1. 服务自行上报或注册 `ip:port`。
2. 新的 endpoint 持久化表或异步 registry 投影。
3. 权限模型本身的设计与落地。
4. 服务健康检查策略改造；是否 ready 仍以 k8s `Service` / `EndpointSlice` 观测结果为准。
5. 对现有 deploy 环境模型之外的全局流量治理、服务网格或跨集群调度。

## 当前问题

当前 `pkg/solver` 直接在业务进程内读取运行时环境变量，并通过以下三类标签在当前 namespace 内定位目标 `Service`：

1. `app.kubernetes.io/name`
2. `app.kubernetes.io/component`
3. `dominion.io/environment`

这一模式的问题是：

1. 解析策略固定写在 client 侧，无法由 deploy 控制面统一升级。
2. 当前只支持“同环境精确匹配”，缺少中心化的 prod fallback 规则。
3. 失败时只返回本地字符串错误，无法对外表达结构化解析原因。
4. `pkg/solver` 的端口参数目前只支持数字，无法直接引用 `service.yaml` 中声明的 named port。

与此同时，`projects/infra/deploy/runtime/k8s/` 已经在生成 k8s 对象时稳定写入上述标签，并且 deploy 已经是 `Environment` 的权威模型。因此，将解析能力并入 deploy，是比继续强化 `pkg/solver` 的 k8s 直连逻辑更合适的收敛方向。

## 模型设计

### ServiceEndpoints 资源模型

新增逻辑资源：`ServiceEndpoints`。

资源名：

`deploy/scopes/{scope}/environments/{env_name}/apps/{app}/services/{service}/endpoints`

它表示的是：

* “从指定环境视角出发，对某个逻辑 `{app, service}` 做一次解析后得到的最终 endpoints 结果。”

该资源不是一个可独立持久化管理的实体，而是一个由 deploy 在读取时动态计算出来的资源对象。

### port_selector 模型

调用目标格式从：

* `app/service[:port]`

收敛为：

* `app/service[:port_selector]`

其中 `port_selector` 表示：

1. numeric port，例如 `50051`
2. named port，例如 `grpc`

二者二选一。

该命名的目的，是避免继续把“命名端口”和“数字端口”都塞进 `Port int` 语义里，减少接口歧义。

该概念属于 `pkg/solver` client 侧目标语义，不进入 deploy 服务接口。

### service port map 模型

`ServiceEndpoints` 成功响应中包含 service 的端口映射：

* `map<string, int32> ports`

其语义与 `service.yaml` 中 `ports` 配置保持一致，即：

* key = 端口名，例如 `grpc`
* value = 对应数值端口，例如 `50051`

deploy 服务负责返回这份映射；`pkg/solver` 在拿到响应后，使用 `ports` 将 `port_selector` 解析成最终端口，并再与返回的 endpoints 结果匹配。

### resolution_mode 模型

成功解析结果需要显式返回最终解析模式：

1. `SAME_ENV`：命中请求环境内的目标 service。
2. `PROD_FALLBACK`：请求环境内不存在目标 service，转而命中某个 prod 环境。

该字段是结果语义的一部分，而不是仅供调试使用的附加信息。

### view 模型

`view` 只作用于成功响应，且保持最小化：

1. `BASIC`：返回 `endpoints`、`ports` 以及 client 正常解析所需的最小结果。
2. `RESOLUTION`：在 `BASIC` 基础上，额外返回 `resolved_scope`、`resolved_environment`、`resolution_mode` 等解析元数据。

错误响应不使用 `view` 扩展信息，而是统一通过 gRPC status details 返回结构化错误详情。

## 代码分层

### 协议层：`projects/infra/deploy/deploy.proto`

协议层负责：

1. 新增 `GetServiceEndpoints` RPC。
2. 定义 `ServiceEndpoints` 资源及其 `name`。
3. 定义 `ServiceEndpointsView`、`ResolutionMode` 等外部 API 枚举。
4. 定义 `GetServiceEndpointsRequest` 中的 `name`、`view` 等字段，以及 `ServiceEndpoints` 中的 `ports` 端口映射字段。

建议 HTTP 形态：

* `GET /v1/{name=deploy/scopes/*/environments/*/apps/*/services/*/endpoints}`

`view` 作为查询参数进入请求。

### 领域层：`projects/infra/deploy/domain/`

领域层负责：

1. 解析 `ServiceEndpoints` 资源名中的 `scope` / `env_name` / `app` / `service`。
2. 明确解析策略的领域规则：
   * 先查同环境。
   * 仅当 caller 环境类型为 `PROD` 且同环境 `Service` 不存在时，进入 fallback。
   * fallback 目标仍按 `target app + target service` 精确匹配。
3. 保持资源名仅表达 `{scope, env_name, app, service}`，不把 client 侧端口选择语义带入 deploy 服务接口。

领域层不负责直接访问 k8s，也不负责 EndpointSlice 细节。

### 应用/处理层：`projects/infra/deploy/handler.go`

处理层负责：

1. 组装 deploy repository、环境类型信息与 runtime 查询。
2. 按规则执行同环境查找与 fallback。
3. 在成功时构造 `ServiceEndpoints` 响应，包括 endpoints 与 `ports` 映射。
4. 在失败时构造带 gRPC status details 的结构化错误。

这是本方案的关键分层决策：

* **proto 表达对外资源和错误契约。**
* **domain 表达解析规则和目标语义。**
* **handler / runtime 负责读取 deploy + k8s 并产出最终结果。**

### 运行时层：`projects/infra/deploy/runtime/k8s/`

运行时层负责：

1. 基于 `{app, service, environment}` 查找 `Service`。
2. 读取 `Service.Spec.Ports`，提取稳定的 `name -> port` 映射。
3. 从 `EndpointSlice` 中提取 ready 且非 terminating 的 endpoint 地址。
4. 返回 endpoints 与端口映射。

运行时层不负责决定 fallback 策略，只负责在给定目标环境下完成一次真实解析。

## 关键细节

### 1. “不存在” 的精确定义

本方案中，“同环境不存在目标 service” 的定义固定为：

* 在当前环境中，不存在匹配 `{app, service, environment}` 的 Kubernetes `Service` 资源。

以下情况不属于“不存在”：

1. `Service` 存在但没有 ready endpoints。
2. `Service` 存在但后续 client 请求的 named port 不存在。
3. deploy 环境存在但尚未 ready。

这三种情况都不触发 fallback。

### 2. fallback 条件

进入 fallback 必须同时满足：

1. 请求环境类型为 `PROD`。
2. 同环境中不存在目标 `Service`。

fallback 候选域固定为：

* 所有 `prod` 环境里，存在匹配 `target app + target service` 的 `Service` 的环境。

说明：

* fallback 可以跨 scope。
* 但 fallback 仍然不忽略 `target app`。
* “允许跨 app 调用”表达的是 caller app 不必等于 target app，不是 target app 可以被省略。

### 3. 选择算法

本方案选择“稳定挑一个”，而不是“稳定分摊”。

因此默认策略为：

1. 先筛出 viable candidates：
   * `Service` 存在。
   * `Service` 具备可返回的端口映射与 endpoints 结果。
2. 按固定 canonical key 排序候选，建议键为：
   * `{scope}/{env_name}`
3. 取排序后的第一个候选作为 fallback 目标。

这样做的原因是：

1. 比 hash 更容易解释和测试。
2. 不需要把候选集变化再包装成“稳定 hash”语义。
3. 能保证在候选集不变时，解析结果稳定可预测。

### 4. named port 的解析职责在 `pkg/solver`

若调用目标包含 named `port_selector`，则职责分工固定为：

1. deploy 服务负责选定目标环境与目标 `Service`。
2. deploy 服务返回该 `Service` 的 `ports` 映射与原始 endpoints 结果。
3. `pkg/solver` 在 client 侧将 named `port_selector` 解析为数值端口。
4. `pkg/solver` 使用该端口拼出最终 `ip:port` 地址列表。

这样做的原因是：

1. deploy 服务接口保持只表达资源解析结果，不额外承载 client 侧端口选择语义。
2. `pkg/solver` 继续承担 `app/service[:port_selector]` 这一调用目标格式的解释责任。
3. service 端口名的语义仍然依赖具体 `Service` 对象，因此 deploy 需要返回 `ports` 映射作为解析依据。

### 5. 成功响应必须暴露最终命中的 scope / environment

由于 fallback 可跨 scope，成功响应必须显式返回：

1. `resolved_scope`
2. `resolved_environment`
3. `resolution_mode`

否则：

1. 调试时无法知道最终命中了哪里。
2. 缓存层难以判断结果是否跨 scope。
3. 后续接入权限模型时，难以复用现有返回值进行审计。

### 6. 错误响应使用 gRPC status details

失败响应不通过 `view` 返回细节，而通过 gRPC status details 返回结构化信息。

建议最少规则：

1. 所有业务错误都附加 `google.rpc.ErrorInfo`。
2. 需要表达约束失败时，再附加 `google.rpc.PreconditionFailure`。

推荐码：

1. `InvalidArgument`：`name` / `view` 非法。
2. `NotFound`：同环境不存在，且 fallback 后也没有候选。
3. `FailedPrecondition`：deploy 能定位 `Service`，但其运行时端口模型不满足返回契约。

推荐 `ErrorInfo.reason`：

1. `SERVICE_ENDPOINTS_NOT_FOUND`
2. `SERVICE_PORT_MAP_UNAVAILABLE`
3. `INVALID_VIEW`

推荐 metadata：

1. `resource_name`
2. `app`
3. `service`
4. `environment`
5. `resolved_scope`（若已进入 fallback 选择阶段）
6. `resolved_environment`（若已进入 fallback 选择阶段）

## 决策详情

### 决策一：endpoints 作为 Get 风格资源，而不是注册表条目

选择：**把 endpoints 视为 deploy 暴露的逻辑资源对象，并使用 Get 接口读取。**

原因：

1. endpoint 不是业务主动维护的数据，而是 deploy 结合 k8s 运行时生成的结果。
2. 当前目标是统一解析入口，而不是引入第二套可写注册中心。
3. Get 风格更符合“读取一次解析结果”的实际语义。

### 决策二：不引入 endpoint 持久化

选择：**v1 直接查询 k8s，不新增 registry 表。**

原因：

1. 当前目标是统一解析规则，而不是建立新的投影系统。
2. 实时查询可以避免 deploy 状态与 endpoint 快照漂移。
3. 在规则还在收敛阶段时，先避免把问题扩展到异步一致性。

### 决策三：fallback 只在“Service 不存在”时触发

选择：**仅当同环境中不存在匹配 `Service` 时才允许 fallback。**

原因：

1. 这条规则能明确区分“环境缺失”和“服务故障”。
2. 避免把 fallback 演变成隐式容灾策略。
3. 与“主调和被调都在 prod 环境才允许降级”的目标一致。

### 决策四：fallback 候选可跨 scope，但仍精确匹配 target app + service

选择：**scope 不是 fallback 边界，但 target app + service 仍是解析键的一部分。**

原因：

1. caller app 与 target app 解耦，满足跨 app 调用诉求。
2. target app 仍保留在匹配键中，可以避免同名 service 在不同 app 下发生歧义。
3. deploy runtime 当前本就以 `app + service + environment` 写入标签，保持这一维度最稳妥。

### 决策五：`port_selector` 只保留在 `pkg/solver`

选择：**调用目标格式中的 `port` 收敛为 `port_selector`，但该概念不进入 deploy 服务接口。**

原因：

1. 新语义同时支持 numeric port 与 named port。
2. 使用 `port` 容易让调用方误以为只能传数字。
3. `port_selector` 能更准确表达“按端口名或端口号选择 endpoint”。
4. deploy 服务本身只需要返回 `Service` 的端口映射，不需要理解 client 侧 selector 语义。

### 决策六：成功响应的 `view` 与错误详情分离

选择：**`view` 仅控制成功响应；错误详情统一走 gRPC status details。**

原因：

1. 成功响应和失败响应应有清晰边界。
2. grpc-go 与 grpc-gateway 对 status details 已有稳定支持。
3. 可以避免在响应体里发明第二套错误元数据协议。

## 预计改动范围

核心改动文件：

1. `projects/infra/deploy/deploy.proto`
   * 新增 `ServiceEndpoints` 资源、`GetServiceEndpoints` RPC、`ServiceEndpointsView`、`ResolutionMode`，并在响应中加入 `ports` 映射。
2. `projects/infra/deploy/handler.go`
   * 增加 `GetServiceEndpoints` 入口、解析流程、status details 错误构造。
3. `projects/infra/deploy/domain/`
   * 新增 `ServiceEndpoints` 资源名解析与解析规则，不引入 `port_selector` 模型。
4. `projects/infra/deploy/runtime/k8s/`
   * 增加按 `{app, service, environment}` 查找 `Service`、返回端口映射、展开 `EndpointSlice` 的能力。
5. `pkg/solver/target.go`
   * 将 `app/service[:port]` 收敛为 `app/service[:port_selector]` 语义。
6. `pkg/solver`
   * 增加 deploy name-service client，并基于响应里的 `ports` 映射解析 `port_selector`。
7. 测试文件
   * `projects/infra/deploy/handler_test.go`
   * `projects/infra/deploy/runtime/k8s/*_test.go`
   * `pkg/solver/target_test.go`
   * `pkg/solver/*_test.go`

## 验收标准

满足以下条件时，认为方案落地成功：

1. `DeployService` 能通过 `GetServiceEndpoints` 返回指定 `{scope, env_name, app, service}` 的解析结果。
2. 同环境存在目标 `Service` 时，结果使用同环境 endpoints，不进入 fallback。
3. 仅当 caller 环境类型为 `PROD` 且同环境 `Service` 不存在时，才会进入 fallback。
4. fallback 可跨 scope，但最终命中的候选仍满足 `target app + target service` 精确匹配。
5. `DeployService` 成功响应中包含目标 `Service` 的 `ports` 映射，且与运行时 `Service.Spec.Ports` 一致。
6. `pkg/solver` 能基于 `ports` 映射将 named `port_selector` 解析成正确数值端口。
7. 若 `pkg/solver` 请求的 named `port_selector` 不在 `ports` 映射中，client 返回明确错误，不进入 fallback。
8. 成功响应至少能在 `RESOLUTION` 视图下返回 `resolved_scope`、`resolved_environment` 与 `resolution_mode`。
9. `pkg/solver` 可通过 deploy name-service client 获得与上述规则一致的解析结果。

## 风险与约束

1. 由于 fallback 可以跨 scope，若未来引入权限模型，需要在候选筛选阶段加入权限过滤，而不能在结果返回后再做补救。
2. 该方案默认实时查询 k8s，因此结果是观测时刻的一致性，而不是连接时刻的健康承诺。
3. 若未来 prod 环境数增加，固定排序取第一个候选可能带来流量集中；但这属于后续流量治理问题，不属于本方案目标。

## 未来规划

本方案落地后，可在后续单独评估以下扩展，但不属于当前目标：

1. 基于 deploy revision 的本地缓存与失效策略。
2. 基于权限模型的 fallback 候选过滤。
3. 对 prod 中多个环境部署相同 `app/service` 的告警。
4. 若未来确有需要，再评估从“固定排序取第一个候选”升级为可解释的稳定分摊策略。
