# pkg/solver / pkg/grpc 有状态服务发现方案

## 目标

本方案用于在现有 `deploy service`、`pkg/solver` 和 `pkg/grpc/solver` 之上增加有状态服务发现能力，目标是：

* 让 `pkg/solver` 在**不破坏现有无状态发现接口**的前提下，显式提供“发现有状态服务全部实例”的能力。
* 让 `pkg/grpc/solver` 可以通过统一入口 `solver.URI("app/service:port", solver.WithInstance(i))` 表达“连接有状态服务第 i 个实例”的意图。
* 让 `deploy service` 对外返回的 `ServiceEndpoints` 资源同时承载：
  * 无状态服务的聚合 `endpoints`
  * 有状态服务的 `stateful_instances`
* 让“实例不存在”“服务不是有状态服务”“实例存在但当前无 ready endpoints”三类错误在协议和调用行为上被明确区分，避免静默回退。

本方案要达成的效果不是单纯“多加几个字段”，而是让仓库内的服务发现语义清晰分层：

* 无状态服务继续使用现有逻辑服务发现路径。
* 有状态服务显式进入实例级发现路径。
* gRPC 调用方无需了解 deploy 内部命名细节，只需要按实例编号访问。

## 范围

本方案只覆盖以下内容：

* `projects/infra/deploy` 的 `ServiceEndpoints` 返回模型扩展
* `projects/infra/deploy/runtime/k8s` 的服务发现查询语义扩展
* `pkg/solver` 的有状态服务发现接口与基于 deploy 的实现
* `pkg/grpc/solver` 的 stateful schema、Builder 和 URI option 设计

本方案不包括：

* 基于 `pkg/solver/k8s.go` 的有状态 resolver 实现
* leader / follower、读写分离、角色路由
* 按 Pod 名称、实例标签或业务角色发现
* 业务层协议改造

关于 StatefulSet 资源命名、per-instance Service、per-instance HTTPRoute 的部署约束，直接复用已有方案：

* `design/deploy_stateful_workload_support.md`

本方案不重复定义那份文档已经明确的资源生成规则，只讨论**如何消费这些资源进行服务发现**。

## 当前问题

### 1. `pkg/solver` 只有逻辑服务发现接口

当前 `pkg/solver` 只有：

```go
type Resolver interface {
    Resolve(ctx context.Context, target *Target) ([]string, error)
}
```

这个接口表达的是“给定逻辑服务，返回一组可连接地址”。它适合无状态服务，也适合“对实例不敏感”的聚合访问，但不适合“枚举有状态服务实例并按实例选择”的场景。

### 2. 默认 resolver 链路是 deploy-backed，而不是 k8s-backed

`pkg/grpc/solver` 默认通过 `solver.NewDeployResolver()` 取地址，而不是通过 Kubernetes API 直接查询。因此如果只扩展 `pkg/solver/k8s.go`，默认 gRPC 链路仍然无法获得有状态实例信息。

### 3. `ServiceEndpoints` 当前只能表达聚合 endpoints

当前 `projects/infra/deploy/deploy.proto` 中的 `ServiceEndpoints` 只有：

* `endpoints`
* `ports`

它没有实例级字段，因此 deploy-backed resolver 无法知道：

* 某个服务是否为有状态服务
* 该服务有哪些实例
* 第 `i` 个实例对应哪些 endpoints

### 4. 现有 runtime 查询逻辑对有状态服务不成立

当前 `projects/infra/deploy/runtime/k8s/executor.go` 中的 `QueryServiceEndpoints(...)` 要求标签选择结果**只能命中一个 Service**。但根据已有 stateful workload 方案，有状态服务会同时生成：

* governing headless Service
* 多个 per-instance Service

因此如果继续沿用“命中多个 Service 就报冲突”的逻辑，就无法对有状态服务提供发现结果。

## 最终模型

## ServiceEndpoints 资源模型

### 无状态服务

无状态服务保持现有语义：

* `endpoints`：逻辑服务的聚合地址
* `stateful_instances`：为空

### 有状态服务

有状态服务返回两层视图：

* `endpoints`：**聚合视图**，表示整个逻辑服务当前所有 ready 地址的聚合结果
* `stateful_instances`：实例级视图，每个实例一个条目

这样做的原因是：

* 已有“按逻辑服务访问”的客户端仍可以继续使用 `endpoints`
* 新的实例级客户端使用 `stateful_instances`
* 不把“服务是 stateful”变成对旧客户端的破坏性变化

### stateful_instances 结构

第一版保持最小结构：

* `index`
* `endpoints`

不在协议中暴露以下信息：

* Pod 名称
* per-instance Service 名称
* Route 名称

原因是这些细节属于部署内部实现，不应成为发现协议的稳定契约。

### 排序约束

`stateful_instances` 必须按 `index` **升序返回**。

这样做有三个作用：

* 协议输出稳定，便于测试
* 调试和日志比对更直接
* 调用方即使不依赖顺序，也能获得确定性结果

## pkg/solver 模型设计

### 原接口保持不变

保留现有：

```go
type Resolver interface {
    Resolve(ctx context.Context, target *Target) ([]string, error)
}
```

它继续承担无状态服务、以及有状态服务的聚合访问。

### 新增有状态 resolver 接口

新增独立接口，例如：

```go
type StatefulResolver interface {
    ResolveStateful(ctx context.Context, target *Target) ([]StatefulInstance, error)
}

type StatefulInstance struct {
    Index     int
    Endpoints []string
}
```

设计原则：

* 不污染原 `Resolver` 语义
* 不要求所有现有 resolver 立刻支持有状态语义
* 让调用方显式选择实例级发现路径

### 第一版只实现 deploy-backed StatefulResolver

第一版只新增 deploy-backed 实现，例如：

* `NewDeployStatefulResolver()`

它通过 deploy service 的 `ServiceEndpoints` 返回值读取 `stateful_instances`。

本阶段不实现：

* `NewK8sStatefulResolver()`

这样可以把改动控制在已经被默认 gRPC 链路使用的路径上，避免一次性扩到两套后端。

## pkg/grpc/solver 模型设计

## Scheme 分层

保留现有 scheme：

* `dominion`

新增 stateful scheme：

* `dominion-stateful`

设计原则：

* 普通服务发现与实例级发现走不同 resolver schema
* 不在同一个 builder 中隐式混入两套完全不同的解析语义
* 老客户端 URI 不变

## Builder 分层

保留现有：

* 普通 `Builder`

新增：

* `StatefulBuilder`

职责分工：

* 普通 `Builder`：依赖 `solver.Resolver`
* `StatefulBuilder`：依赖 `solver.StatefulResolver`

这比“给一个 Builder 加多个模式分支”更清晰，也便于测试。

## URI option 设计

统一入口采用：

```go
solver.URI("app/service:grpc")
solver.URI("app/service:grpc", solver.WithInstance(1))
```

而不是链式对象 API。

原因是：

* 保持 `solver.URI(...)` 返回 `string` 的调用习惯
* 避免把原本简单的工具函数升级成 builder 对象，扩大兼容性影响面

### URI 编码方式

当使用 `WithInstance(i)` 时：

* 自动切换到 `dominion-stateful` scheme
* 用 query 参数编码实例编号

例如：

* 普通：`dominion:///app/service:grpc`
* 有状态：`dominion-stateful:///app/service:grpc?instance=1`

这样做的原因是：

* 不改动现有 `app/service:port` 主体格式
* 避免发明 `service[1]` 之类的新语法
* 解析简单，和现有 target 模型边界清楚

### 实例编号语义

实例编号统一采用 **0-based**，与现有 StatefulSet ordinal 对齐。

例如：

* `WithInstance(0)` 表示第一个实例
* `WithInstance(1)` 表示第二个实例

不引入 1-based 包装，避免与 runtime/k8s 现有实例命名和选择逻辑错位。

## deploy service 代码分层

## domain / proto

需要扩展：

* `projects/infra/deploy/domain/ServiceQueryResult`
* `projects/infra/deploy/deploy.proto` 中的 `ServiceEndpoints`

新增实例级结构，例如：

* `StatefulServiceInstance`
  * `index`
  * `endpoints`

`ServiceQueryResult` 与 proto 结构保持同构，避免 handler 再做额外语义转换。

## runtime/k8s

`QueryServiceEndpoints(...)` 需要从“单一 Service 视图”调整为“逻辑服务视图”。

对于无状态服务：

* 继续按现有方式返回 `endpoints`

对于有状态服务：

* 从 governing Service 生成聚合 `endpoints`
* 从 per-instance Service 生成 `stateful_instances`

这里不重新定义 StatefulSet 的资源生成规则，直接复用 `design/deploy_stateful_workload_support.md` 中已经确定的命名和 selector 模式。

## handler

`GetServiceEndpoints(...)` 的资源名和 fallback 行为保持不变，仍然对外暴露同一个 `ServiceEndpoints` 资源。

变化仅在于：

* 返回模型中新增 `stateful_instances`
* `newServiceEndpointsResponse(...)` 需要复制该字段

这使得对外 API 仍然是同一个资源，只是表达能力更强。

## 关键细节

### 1. 如何识别 stateful 服务

在 `runtime/k8s` 查询阶段，不通过额外配置开关推断，而是依据已有资源形态识别：

* governing Service：headless (`ClusterIP=None`)
* per-instance Service：selector 含 `statefulset.kubernetes.io/pod-name`

这样可以直接复用现有部署结果，不要求额外维护一份“服务类型映射表”。

### 2. 聚合 endpoints 的语义

即使服务是有状态服务，`endpoints` 也保留，并表示：

* 所有 ready 实例地址的聚合结果

这保证了已有基于逻辑服务的访问路径仍然可用。

### 3. 实例存在但无 ready endpoint 的处理

若某个实例存在，但当前没有 ready endpoint，则该实例仍然要出现在 `stateful_instances` 中：

* `index` 存在
* `endpoints` 为空

这样调用方可以区分：

* 实例不存在
* 实例存在但暂时不可用

### 4. stateful_instances 缺失实例与空实例的区别

协议语义明确约束：

* 缺少 `index=N` 的条目：表示该实例不存在
* 存在 `index=N` 但 `endpoints=[]`：表示该实例存在但无 ready endpoint

这为 `pkg/solver` 与 `pkg/grpc/solver` 的错误处理提供稳定依据。

## 错误语义

## `pkg/solver` stateful resolver

需要明确区分三类错误：

### 1. 服务不是有状态服务

当调用方进入 stateful 发现路径，但 deploy 返回中没有 `stateful_instances` 且该服务语义上不是 stateful 服务时，返回明确错误：

* `service is not stateful`

该错误用于表达“调用意图与服务类型不匹配”，不能静默回退到聚合 `endpoints`。

### 2. 指定实例不存在

当调用方请求 `WithInstance(i)`，但 `stateful_instances` 中没有 `index=i` 时，返回明确错误：

* `stateful instance i not found`

### 3. 指定实例存在但无 ready endpoint

当存在 `index=i`，但该实例 `endpoints` 为空时，返回明确错误：

* `stateful instance i has no ready endpoints`

## `pkg/grpc/solver`

`StatefulBuilder` 读取 URI 中的 `instance` query 参数后，调用 `StatefulResolver.ResolveStateful(...)`，再根据上述三类情况将错误原样上抛给 gRPC resolver。

这样 gRPC 客户端看到的是清晰的解析失败，而不是空地址静默重试。

## 决策详情

### 决策 1：不修改原 `Resolver` 接口

原因：

* 现有接口已经稳定服务于无状态发现
* 有状态发现需要的是“实例列表”，不是简单的地址列表
* 强行把实例语义塞进原接口只会让调用方分支更多

### 决策 2：第一版只做 deploy-backed StatefulResolver

原因：

* 默认 gRPC 路径当前就依赖 deploy-backed resolver
* 先做 deploy-backed 才能真正支撑调用方落地
* 可以避免同时维护两套 stateful resolver 实现和测试

### 决策 3：保留 stateful 服务的聚合 endpoints

原因：

* 兼容已有逻辑服务访问方式
* 避免“服务切成 stateful 后老客户端突然失效”
* 同时兼容聚合访问和实例访问两种模式

### 决策 4：`WithInstance` 不允许用于无状态服务

原因：

* 调用方已经明确表达“我要连接某个 stateful 实例”
* 如果目标服务不是 stateful，说明调用意图与目标不符
* 静默回退会掩盖配置问题

### 决策 5：实例编号采用 0-based

原因：

* 与 StatefulSet ordinal 对齐
* 与现有 runtime/k8s per-instance Service / Route 生成规则一致
* 不引入额外编号换算

### 决策 6：URI 中用 query 参数承载实例编号

原因：

* 不破坏现有 target 主格式
* 解析边界清晰
* 比 path 扩展语法更简单、更稳定

## 实现落点

建议实现落点如下：

* `projects/infra/deploy/deploy.proto`
  * 为 `ServiceEndpoints` 增加 `stateful_instances`
  * 新增 `StatefulServiceInstance` message
* `projects/infra/deploy/domain/service_endpoints_name.go`
  * 扩展 `ServiceQueryResult`
* `projects/infra/deploy/runtime/k8s/executor.go`
  * 扩展 `QueryServiceEndpoints(...)` 的有状态查询语义
* `projects/infra/deploy/runtime/k8s/service_query_test.go`
  * 增加有状态服务查询测试
* `projects/infra/deploy/handler.go`
  * 扩展 `newServiceEndpointsResponse(...)`
* `projects/infra/deploy/handler_test.go`
  * 增加 `stateful_instances` 响应测试
* `pkg/solver`
  * 新增 `StatefulResolver` / `StatefulInstance`
  * 新增 deploy-backed stateful resolver 及测试
* `pkg/grpc/solver`
  * 新增 stateful scheme
  * 新增 `StatefulBuilder`
  * 扩展 `solver.URI(..., opts...)`
  * 增加 `WithInstance(...)` option 与测试

## 验收标准

完成后应满足：

* 无状态服务的既有 `solver.URI("app/service:port")` 调用行为不变
* 有状态服务的 `ServiceEndpoints` 返回：
  * 聚合 `endpoints`
  * 升序 `stateful_instances`
* `solver.URI("app/service:port", solver.WithInstance(i))` 能生成 stateful schema URI
* `pkg/grpc/solver` 能基于指定实例建立连接
* 以下错误被明确区分并可测试验证：
  * 服务不是有状态服务
  * 指定实例不存在
  * 指定实例存在但无 ready endpoints

## 未来规划

本方案刻意不覆盖以下扩展项，可在后续独立设计：

* `pkg/solver/k8s.go` 的 stateful resolver 实现
* 按角色（leader/follower）而非纯 ordinal 发现
* 在协议中暴露更多实例元信息
* 非 gRPC 客户端的统一 stateful 发现接入
