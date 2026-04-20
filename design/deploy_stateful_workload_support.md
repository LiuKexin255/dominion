# Deploy service / CLI stateful workload 支持方案

## 目标

本方案用于在现有 `deploy service` 与 `deploy` CLI 中增加 `stateful` workload 支持，目标是：

* 让 deploy 配置可以显式表达 `stateless` 与 `stateful` 两类运行时工作负载。
* 让 `stateful` workload 在保留现有环境隔离规则的前提下，生成实例级域名与路由。
* 让 `deploy service` 在模型、校验、runtime 资源生成上显式区分 `Deployment` 与 `StatefulSet` 路径。
* 让 CLI 在配置解析、编译和用户错误提示上清晰表达两类 workload 的差异，不引入静默忽略语义。

## 范围

本方案仅覆盖 `deploy service` 与 `deploy` CLI 对 `stateful` workload 的支持：

* 配置模型调整
* proto / domain 模型调整
* runtime/k8s 资源生成调整
* CLI schema / config / compiler 调整
* `stateful` 的 hostname 展开与缩容语义

本方案不包括：

* 业务侧 `session service` / `game gateway` 的协议设计
* 更高阶段的统一前门路由
* leader 选举、读写分离、按实例角色路由
* 存储 lifecycle 的高级策略

## 当前问题

当前仓库实现存在以下约束：

* `tools/deploy/pkg/schema/service.schema.json` 已允许 `artifacts[].type = stateful`，但 `tools/deploy/pkg/config/config.go` 仍只接受 `deployment`。
* `projects/infra/deploy/deploy.proto` 与 `projects/infra/deploy/domain/spec.go` 中的 `ArtifactSpec` 没有 workload 类型字段。
* `projects/infra/deploy/runtime/k8s/converter.go` 默认将所有 artifact 都转成 `DeploymentWorkload`。
* `projects/infra/deploy/domain/spec.go` 中 `ArtifactHTTPSpec.Validate()` 当前强制要求 `hostnames` 和 `matches` 同时非空。
* `projects/infra/deploy/runtime/k8s/converter.go` 当前只有在 `http.matches` 非空时才会生成 `HTTPRoute`。

因此，若要支持 `stateful` workload，不能只在解释层“忽略 match”，必须同步调整配置模型、校验规则、proto/domain 和 runtime 生成路径。

## 最终模型

## 配置模型

### service 级 workload 类型

将运行时类型从 `artifacts[].type` 提升到 `service` 级别，建议新增：

```yaml
workload:
  kind: stateless | stateful
```

设计原则：

* `artifact` 继续表示构建产物。
* `workload.kind` 表示运行时部署形态。
* `stateful/stateless` 是 Kubernetes controller 选择问题，不应继续绑定在 artifact 上。

### artifact 的定位

`artifacts[]` 保留为构建与镜像产物配置，例如：

* `name`
* `target`
* `tls`
* `ports`

不再让 `artifacts[].type` 决定部署形态。

### 非兼容调整原则

本方案明确**不考虑兼容性问题**。

要求如下：

* 新配置统一使用 `service.workload.kind`
* `artifacts[].type` 直接视为无效旧字段，不再兼容读取
* CLI、schema、proto、domain、runtime 全部按新模型收敛
* 文档与示例全部以新字段为准

## HTTP 语义

### stateless

`stateless` 保持当前语义：

* `http.hostnames`：可用
* `http.matches`：可用
* 生成 `Deployment + Service + HTTPRoute`

### stateful

`stateful` 复用 `http.hostnames`，但语义调整为：

* `http.hostnames` 表示**基础域名**
* `http.matches` **非法，直接报错**
* 生成 `StatefulSet + Service(s) + per-instance HTTPRoute`

这里不采用“忽略 `match`”的行为，原因是：

* 静默忽略会让用户误以为 path/backend match 生效
* 不利于 schema、CLI 和文档表达
* 会扩大未来模型歧义

## stateful hostname 展开规则

若配置：

```yaml
workload:
  kind: stateful
http:
  hostnames:
    - gateway.example.com
```

且 `service.name = game-gateway`、`replicas = 3`，则实例名使用简短形式：

* `game-gateway-0`
* `game-gateway-1`
* `game-gateway-2`

最终实例域名自动展开为：

* `game-gateway-0.gateway.example.com`
* `game-gateway-1.gateway.example.com`
* `game-gateway-2.gateway.example.com`

### 多 hostname 规则

若 `http.hostnames` 配置多个基础域名，则全部展开。

例如：

* `gateway.example.com`
* `gateway.internal.example.com`

则 `game-gateway-0` 对应：

* `game-gateway-0.gateway.example.com`
* `game-gateway-0.gateway.internal.example.com`

### 缩容语义

若 `replicas` 缩小，被移除实例的域名立即失效。

deploy 需要同时删除对应的：

* per-instance Service
* per-instance HTTPRoute / route rule

不保留兼容窗口，也不做透明迁移。

## 环境隔离规则

沿用现有 deploy 平台行为：

* PROD：不追加 env header match
* TEST / DEV：自动追加 `env={scope}.{env_name}` header match

该逻辑是平台自动注入的环境隔离规则，不是用户配置的 `http.matches` 语义。

这条规则对 `stateless` 和 `stateful` 都生效。

## 模型设计

### proto / domain

### deploy proto

建议在 `projects/infra/deploy/deploy.proto` 中新增 workload kind 枚举，例如：

* `WORKLOAD_KIND_UNSPECIFIED`
* `WORKLOAD_KIND_STATELESS`
* `WORKLOAD_KIND_STATEFUL`

并让 `ArtifactSpec` 或其上层承载该信息。考虑当前 deploy service 只对外暴露 `Environment`，短期可在 `ArtifactSpec` 中新增字段承接编译结果，避免一次性大改 desired state 结构。

### domain

`projects/infra/deploy/domain/spec.go` 需要同步增加 workload kind 字段，并更新 `Validate()` 语义：

* `stateless`：若配置 `http`，则 `hostnames` 和 `matches` 都需要合法
* `stateful`：若配置 `http`，则 `hostnames` 必须非空，`matches` 必须为空

### 为什么短期仍可挂在 ArtifactSpec 上

对外配置模型已经改成 `service.workload.kind`。为控制本次改动范围，deploy service 内部可先在 `ArtifactSpec` 上承接 workload kind；这只是内部实现选择，不意味着继续兼容旧配置字段。

## 代码分层

## deploy service

建议按如下职责分层：

* `handler`：继续接收 `Environment` CRUD 和查询请求
* `domain`：维护 desired state 的 workload kind 与 HTTP 校验规则
* `storage`：持久化新增的 workload kind 字段
* `runtime/k8s/converter`：按 workload kind 分发到不同 workload 对象
* `runtime/k8s/builder`：分别生成 Deployment / StatefulSet 及其配套资源
* `runtime/k8s/executor`：增加 StatefulSet 相关 apply/prune 路径

## CLI

建议按如下职责分层：

* `pkg/schema`：声明 `service.workload.kind` 的 schema 约束
* `pkg/config`：解析并校验 service/workload/http 语义
* `v2/compiler`：将配置编译为 deploy proto desired state
* `README` / 示例：向用户说明 stateful 的 hostname 展开语义

## runtime/k8s 资源生成

## stateless 路径

保持现有路径：

* `Deployment`
* 共享 `Service`
* 可选 `HTTPRoute`

## stateful 路径

新增显式路径：

* `StatefulSet`
* governing/headless Service
* per-instance Service
* per-instance HTTPRoute / rule

### 为什么需要 per-instance Service

当前仓库的 `HTTPRoute -> backend` 指向的是 `Service`，不是 Pod。若要将：

* `game-gateway-0.gateway.example.com`

稳定路由到：

* `game-gateway-0` 实例

就必须有一个精确指向该实例的 backend Service，不能继续把所有实例 hostname 指向同一个共享 Service。

### per-instance Service 的实现方式

per-instance Service 建议通过 `selector label` 精确选中单个 StatefulSet Pod。

推荐做法：

* 保留一个 governing/headless Service，选择整组 StatefulSet Pod，用于稳定网络身份和 StatefulSet `serviceName`
* 为每个实例额外生成一个 per-instance Service，例如：
  * `game-gateway-0`
  * `game-gateway-1`
  * `game-gateway-2`
* 每个 per-instance Service 的 selector 不再只使用共享的 `app=...` 之类标签，而是使用该实例独有的稳定 label

推荐优先复用 StatefulSet Pod 的稳定标签，例如：

* `statefulset.kubernetes.io/pod-name=game-gateway-0`

这样：

* `game-gateway-0` 这个 Service 只会选中 `game-gateway-0` 这个 Pod
* Pod 重建后只要实例名不变，Service 仍然会继续选中新的 Pod
* 缩容时删除对应实例 Pod 后，同步删除该 per-instance Service 即可

这也是 per-instance HTTPRoute 能稳定落到指定实例的基础。

### runtime 对象建议

建议在 `projects/infra/deploy/runtime/k8s/model.go` 中新增：

* `StatefulWorkload`
* 可选的 `StatefulInstanceRouteWorkload`（或在 `HTTPRouteWorkload` 上扩展 instance 维度）

建议在 `converter.go` 中按 workload kind 显式分支，而不是在现有 Deployment 路径上塞条件逻辑。

## 校验规则

### service config 校验

对 `tools/deploy/pkg/config/config.go` 的建议：

* `service.workload.kind` 必填，或缺省为 `stateless`
* `stateful + http.matches` => 报错
* `stateful + http` 但无 `hostnames` => 报错
* `stateless + http` 但无 `matches` => 报错

### schema 约束

对 `tools/deploy/pkg/schema/service.schema.json` 的建议：

* 新增 `workload.kind`
* 标记 `artifacts[].type` 为弃用，后续移除
* 用条件 schema 表达：
  * `stateful` 不允许 `http.matches`
  * `stateless` 可以使用 `http.matches`

若 JSON Schema 难以完整表达，可在 Go 配置校验层补齐。

## CLI 改动

CLI 需要覆盖以下改动。

### 1. 配置解析

`tools/deploy/pkg/config/config.go`

* 新增 `ServiceWorkloadKind`
* 解析 `service.workload.kind`
* 拒绝 `artifacts[].type` 旧字段并提示使用 `service.workload.kind`
* 提供 stateful 相关的显式错误提示

### 2. schema

`tools/deploy/pkg/schema/service.schema.json`

* 更新 service schema
* 增加 workload 顶层结构
* 更新 testdata 和 schema 测试

### 3. compiler

`tools/deploy/v2/compiler/compiler.go`

* 按 workload kind 生成不同的 deploy proto 字段
* `replicas` 对 `stateful` 表示 StatefulSet replicas
* HTTP 编译分成两条路径：
  * `compileStatelessHTTP`
  * `compileStatefulHTTP`

对 `stateful`：

* 仅编译 `hostnames`
* 不编译 `matches`
* 若用户配置了 `matches`，在编译前就报错

### 4. 文档与示例

更新 `tools/deploy/README.md`：

* 增加 `service.workload.kind` 示例
* 增加 `stateful` 示例
* 明确 `http.hostnames` 在 `stateful` 下表示基础域名
* 明确展开后的实例域名规则

## deploy service 改动

deploy service 需要覆盖以下改动。

### 1. proto

* 新增 workload kind 枚举和字段
* 更新生成代码与相关转换逻辑

### 2. domain

* 更新 `ArtifactSpec` / 相关 spec 模型
* 更新 `Validate()` 逻辑
* 保持 test/dev 环境隔离逻辑不下沉到 domain 层

### 3. storage

* 持久化 workload kind
* 更新 mongo mapping 与测试

### 4. runtime/k8s

* 新增 StatefulSet workload 对象
* 新增 stateful service / route 生成逻辑
* 更新 executor apply / prune 顺序

## 关键决策

### 决策 1：workload 类型上移到 service 粒度

选择：使用 `service.workload.kind`，不再由 `artifacts[].type` 决定运行时形态。

原因：

* `stateful/stateless` 是 runtime concern，不是 artifact concern
* 降低 build metadata 和 deployment topology 的耦合
* 为后续 stateful runtime 扩展留出空间

### 决策 2：stateful 的 `http.matches` 非法

选择：stateful 下配置 `matches` 直接报错。

原因：

* 避免静默忽略
* 简化工具和文档表达
* 减少用户误判配置已生效的风险

### 决策 3：stateful 的 hostname 按实例展开

选择：`http.hostnames` 表示基础域名，实例域名使用简短实例名展开，例如：

* `{service_name}-0.{hostname}`
* `{service_name}-1.{hostname}`

原因：

* 贴合有状态服务实例级接入需求
* 与前面 `game gateway` 的接入模型一致
* 实例名简短、稳定，便于观察和排障

### 决策 4：缩容后实例域名立即失效

选择：缩容时删除对应实例路由与 Service，域名立即失效。

原因：

* 行为明确
* 避免保留无效实例入口
* 与客户端重建连接语义一致

## 未来规划

未来若 `stateful` workload 需要引入更复杂的网络行为，例如 leader 路由、按实例角色暴露、读写分离、统一前门确定性路由，应在 `workload` 之外增加独立的 `network/exposure` 模型，而不是继续向现有 `http` 结构中塞入更多语义。
