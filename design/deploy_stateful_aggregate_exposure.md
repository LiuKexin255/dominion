# Deploy stateful aggregate exposure 支持方案

## 目标

本方案用于调整 `deploy service` 与 `deploy` CLI 对 `stateful` workload 的默认暴露模型，目标是：

* 让应用型 `stateful` 服务默认以**服务整体**对外提供能力，而不是默认暴露每个实例。
* 保留 StatefulSet/headless Service 提供的稳定实例身份与内部实例发现能力。
* 将实例级外部暴露收敛为显式 escape hatch，避免 deploy 默认把业务路由语义暴露到部署层。
* 让 `GetServiceEndpoints` 对 stateful 服务始终提供稳定的聚合发现与实例发现语义，且不依赖 per-instance Service。

要达成的效果是：deploy 负责应用服务的基础拓扑与服务级入口，业务服务自己根据 session、index、token、registry 或其他应用语义完成内部路由。

## 范围

本方案覆盖：

* `service.yaml` root 层新增 `exposure` 配置。
* deploy proto / domain / storage / compiler / runtime 对 `exposure` 的支持。
* stateful aggregate 与 per-instance 两种暴露模式的 Kubernetes 资源生成规则。
* `QueryStatefulServiceEndpoints` 从 headless Service EndpointSlices 推导 endpoints 与 stateful instances。
* 服务端默认值归一化策略。
* 当前 gateway 服务的迁移窗口配置。

本方案不覆盖：

* game gateway 的内部 session 路由、proxy、共享状态或 token 语义改造。
* ClickHouse / Kafka / ZooKeeper 等底层组件的完整 operator 部署模型。
* leader/follower、读写分离、按实例角色路由。
* service mesh、Gateway controller 特定实现的高级流量策略。

已有 stateful workload 与 stateful discovery 背景参考：

* `design/deploy_stateful_workload_support.md`
* `design/stateful_service_discovery.md`

本方案以这两份文档的实现现状为基础，重新收敛 deploy 对应用型 stateful 服务的默认暴露语义。

## 当前问题

当前 stateful 支持的实现偏向实例级暴露：

* stateful workload 生成 governing/headless Service。
* 每个 replica 生成一个 per-instance Service。
* 每个 replica 生成一个 per-instance HTTPRoute。
* HTTP hostnames 会按实例展开。
* `QueryStatefulServiceEndpoints` 当前从 per-instance Services 查询 `stateful_instances`。

这对需要每个节点有独立 advertised address 的底层组件有价值，但对应用服务存在问题：

* 默认暴露 StatefulSet ordinal，外部 API 契约与副本数耦合。
* 资源数量随 replicas 增长，Service / HTTPRoute 为 `O(replicas)`。
* deploy 层承担了过多实例级路由语义。
* 扩缩容会改变外部域名集合。
* 应用服务作为整体对外提供能力的场景不够自然。

因此需要将默认模式改为 aggregate，并保留 per-instance 作为显式模式。

## 最终模型

## 配置模型

### service.yaml root 层 exposure

在 `service.yaml` root 层新增：

```yaml
kind: stateful
exposure: aggregate | per-instance
```

示例：

```yaml
version: "3.0"
name: gateway
app: game
desc: game gateway service
kind: stateful
exposure: aggregate
artifacts:
  - name: cmd
    target: app/cmd:cmd_image
    tls: true
    ports:
      - name: http
        port: 8080
```

`exposure` 的语义：

| 值 | 语义 |
| --- | --- |
| `aggregate` | stateful 服务作为整体暴露，生成服务级 ClusterIP Service 与服务级 HTTPRoute。 |
| `per-instance` | 保留实例级暴露，生成 per-pod Service 与 per-instance HTTPRoute。 |

约束：

* `exposure` 只允许用于 `kind: stateful`。
* `kind: stateless` 设置 `exposure` 直接报错。
* `exposure` 未设置时，由 deploy 服务端归一化为 `aggregate`。

## Kubernetes 资源模型

### aggregate 模式

aggregate 是 stateful 服务的默认暴露模式。

生成资源：

```text
StatefulSet
svc-{env}-{service}-{hash}       # headless Service
agsvc-{env}-{service}-{hash}     # aggregate ClusterIP Service
route-{env}-{service}-{hash}     # service-level HTTPRoute -> agsvc-*
```

其中：

* `svc-*` 是 StatefulSet governing/headless Service，`ClusterIP: None`。
* `agsvc-*` 是普通 ClusterIP Service，selector 覆盖整组 StatefulSet pods。
* `route-*` 是服务级 HTTPRoute，backend 指向 `agsvc-*`。
* `http.hostnames` 不展开，表示最终服务级域名。

访问路径：

```text
gateway.example.com
  -> route-*
  -> agsvc-*
  -> any ready pod
```

内部实例身份仍由 headless Service 提供：

```text
{pod-name}.svc-{env}-{service}-{hash}.{namespace}.svc
```

### per-instance 模式

per-instance 是 escape hatch，用于确实需要外部定向访问单个 StatefulSet 实例的服务。

生成资源：

```text
StatefulSet
svc-{env}-{service}-{hash}          # headless Service
sisvc-{env}-{service}-{hash}-{i}    # per-pod ClusterIP Service
sirt-{env}-{service}-{hash}-{i}     # per-instance HTTPRoute -> sisvc-*-{i}
```

其中：

* `sisvc-*` 通过 `statefulset.kubernetes.io/pod-name={statefulset-name}-{i}` 精确选择单个 pod。
* `sirt-*` 的 backend 指向对应的 `sisvc-*`。
* `http.hostnames` 按当前实现继续展开为 `{service}-{index}-{hostname}`。
* per-instance 模式不生成 `agsvc-*`。

per-instance 模式保留 `http.matches`，并应修复为支持全部 matches，而不是只使用第一个 match。

## HTTP 语义

### aggregate

配置：

```yaml
http:
  hostnames:
    - gateway.example.com
  matches:
    - backend: http
      path:
        type: PathPrefix
        value: /
```

生成一个服务级 HTTPRoute：

```text
gateway.example.com -> route-* -> agsvc-*
```

### per-instance

同一份 `http` 配置会生成每实例 HTTPRoute：

```text
gateway-0-gateway.example.com -> sirt-*-0 -> sisvc-*-0
gateway-1-gateway.example.com -> sirt-*-1 -> sisvc-*-1
```

`per-instance` 下必须支持全部 `matches`，与 stateless / aggregate 的 HTTPRoute 能力保持一致。

### 没有 HTTP 的 aggregate

`aggregate` 模式允许不配置 `http`。

即使没有 HTTPRoute，也仍然生成：

```text
StatefulSet
svc-*      # headless
agsvc-*    # aggregate ClusterIP Service
```

原因：

* `exposure: aggregate` 表示服务整体可作为一个逻辑服务被访问。
* `agsvc-*` 对集群内部调用也有价值。
* 资源语义稳定，不因是否配置 HTTPRoute 改变 Service 集合。

## ServiceEndpoints 发现语义

## 总体原则

`GetServiceEndpoints` 对 stateful 服务的 discovery 结果不受 `exposure` 影响。

无论 `aggregate` 还是 `per-instance`，都统一从 headless Service EndpointSlices 推导：

* `endpoints`
* `stateful_instances`

不查询：

* `agsvc-*`
* `sisvc-*`

这些 Service 是流量入口或 route backend，不是 stateful discovery 的权威来源。

## endpoints

对 stateful 服务：

```text
endpoints = headless Service 中所有 ready 且 non-terminating pod endpoints
```

语义：

* 表示整个 stateful 逻辑服务的聚合 ready endpoints。
* 与 `exposure` 无关。
* 作为旧聚合访问路径的兼容视图。

## stateful_instances

`stateful_instances` 从同一批 headless EndpointSlices 推导：

1. 遍历 headless Service EndpointSlices。
2. 读取 `discoveryv1.Endpoint.TargetRef.Name` 作为 pod name。
3. 若 `TargetRef.Name` 为空，可用 `Endpoint.Hostname` 兜底。
4. 从 pod name 解析 StatefulSet ordinal。
5. 按 ordinal 分组 endpoints。
6. 按 index 升序返回。

实例存在但未 ready 时，仍应返回实例条目，`endpoints` 为空：

```json
{
  "index": 1,
  "hostname": "sts-name-1",
  "endpoints": []
}
```

协议语义：

* 缺少 `index=N`：实例不存在。
* 存在 `index=N` 但 `endpoints=[]`：实例存在但当前无 ready endpoint。

## 返回示例

```json
{
  "is_stateful": true,
  "endpoints": [
    "10.0.0.10:8080",
    "10.0.0.11:8080"
  ],
  "stateful_instances": [
    {
      "index": 0,
      "hostname": "sts-game-gateway-0",
      "endpoints": ["10.0.0.10:8080"]
    },
    {
      "index": 1,
      "hostname": "sts-game-gateway-1",
      "endpoints": ["10.0.0.11:8080"]
    }
  ]
}
```

## 模型设计

## Proto

在 `projects/infra/deploy/deploy.proto` 中新增 `ExposureMode`：

```proto
enum ExposureMode {
  EXPOSURE_MODE_UNSPECIFIED = 0;
  EXPOSURE_MODE_AGGREGATE = 1;
  EXPOSURE_MODE_PER_INSTANCE = 2;
}
```

`ArtifactSpec` 新增字段：

```proto
message ArtifactSpec {
  // existing fields 1-10
  ExposureMode exposure = 11;
}
```

注意：

* proto 中不要把 `aggregate` 设为 0。
* `UNSPECIFIED` 只表达“客户端未指定”。
* 默认值由 deploy 服务端控制。

## Domain

在 `projects/infra/deploy/domain/spec.go` 中新增：

```go
type ExposureMode int

const (
    ExposureModeUnspecified ExposureMode = 0
    ExposureModeAggregate   ExposureMode = 1
    ExposureModePerInstance ExposureMode = 2
)
```

`ArtifactSpec` 新增：

```go
Exposure ExposureMode
```

校验规则：

* `WorkloadKindStateful + ExposureModeUnspecified` 在服务端 normalize 为 `ExposureModeAggregate` 后再校验。
* `WorkloadKindStateful + ExposureModeAggregate` 合法。
* `WorkloadKindStateful + ExposureModePerInstance` 合法。
* `WorkloadKindStateless + ExposureModeUnspecified` 合法。
* `WorkloadKindStateless + 任何非 unspecified exposure` 非法。

## 服务端默认值归一化

默认值归一化应发生在 deploy 服务端，而不是 CLI 编译阶段。

建议在 domain 层提供 desired state normalize，且在以下路径调用：

* `domain.NewEnvironment(...)`
* `(*Environment).SetDesiredPresent(...)`
* `domain.RehydrateEnvironment(...)` 可读取老数据时兼容 unspecified，但不应改变持久化语义；下一次 update 时会写回归一化结果。

最终默认策略：

```text
workload_kind unspecified -> stateless
replicas 0 -> 1
stateful exposure unspecified -> aggregate
```

说明：

* 当前 deploy.yaml schema 已要求 `replicas >= 1`，所以服务端把 `0` 归一为 `1` 符合用户配置语义。
* 如果未来需要支持 scale-to-zero，应改为 proto `optional int32 replicas` 或独立字段，不能复用 proto3 `0` 表示 both unset and zero。
* `version`、`uri`、target/path normalization 仍属于 CLI/config parser 职责，不移动到服务端。
* `LOG_LEVEL` 默认仍属于 k8s runtime builder 职责。
* Mongo profile 默认仍属于 runtime static config 职责。

## 代码分层

## CLI / config

涉及：

* `tools/release/deploy/pkg/config/config.go`
* `tools/release/deploy/pkg/schema/service.schema.json`
* `tools/release/deploy/v2/compiler/compiler.go`

职责：

* 解析 `service.yaml` root 层 `exposure`。
* schema 限定 `exposure` 取值为 `aggregate | per-instance`。
* CLI 不负责设置 exposure 默认值。
* CLI 可以做配置级错误提示：stateless 设置 exposure 报错。
* compiler 将显式 exposure 映射到 proto；未设置则保持 `EXPOSURE_MODE_UNSPECIFIED`。
* replicas 默认不再由 CLI compiler 单独负责，应由服务端兜底。

## deploy service / handler

涉及：

* `projects/infra/deploy/handler.go`
* `projects/infra/deploy/deploy.proto`

职责：

* proto <-> domain 转换新增 exposure 字段。
* 不在 handler 中散落复杂默认逻辑，尽量委托 domain normalize。
* `GetServiceEndpoints` 对 stateful 服务继续走 `QueryStatefulServiceEndpoints`，不按 exposure 分叉。

## domain / storage

涉及：

* `projects/infra/deploy/domain/spec.go`
* `projects/infra/deploy/domain/environment.go`
* `projects/infra/deploy/storage/mongo.go`

职责：

* 承载 `ExposureMode`。
* 归一化默认值。
* 校验 exposure 与 workload kind 的组合。
* 持久化 exposure。

## runtime/k8s converter

涉及：

* `projects/infra/deploy/runtime/k8s/converter.go`
* `projects/infra/deploy/runtime/k8s/model.go`

职责：

* `StatefulWorkload` 增加 exposure。
* `aggregate`：生成 StatefulWorkload、aggregate HTTPRoute，不生成 InstanceRoutes。
* `per-instance`：生成 StatefulWorkload、InstanceRoutes，不生成 aggregate HTTPRoute。
* HTTP hostnames 在 aggregate 下不展开，在 per-instance 下展开。

## runtime/k8s builder

涉及：

* `projects/infra/deploy/runtime/k8s/naming.go`
* `projects/infra/deploy/runtime/k8s/builder.go`

新增命名前缀：

```go
WorkloadKindAggregateService WorkloadKind = "agsvc"
```

新增 builder：

```go
BuildStatefulAggregateService(workload *StatefulWorkload, cfg *K8sConfig)
```

该 Service：

* 名称：`agsvc-{env}-{service}-{hash}`
* 类型：普通 ClusterIP Service
* selector：`app + service + dominion_environment`
* ports：与 headless Service 一致

per-instance HTTPRoute builder 需要修复为支持全部 matches。

## runtime/k8s executor

涉及：

* `projects/infra/deploy/runtime/k8s/executor.go`

apply 逻辑：

```text
stateful common:
  apply headless Service
  apply StatefulSet

aggregate:
  apply agsvc-* aggregate Service
  apply route-* service-level HTTPRoute if configured

per-instance:
  apply sisvc-* per-instance Services
  apply sirt-* per-instance HTTPRoutes if configured
```

expected/prune 逻辑：

* aggregate：保留 `svc-*`、`agsvc-*`、`route-*`，删除旧 `sisvc-*`、`sirt-*`。
* per-instance：保留 `svc-*`、`sisvc-*`、`sirt-*`，删除旧 `agsvc-*`、`route-*`。

## runtime/k8s endpoint query

`QueryStatefulServiceEndpoints` 调整为：

1. 找到唯一 headless Service `svc-*`。
2. 从 headless Service 读取 ports map。
3. 查询 headless Service EndpointSlices。
4. 用同一批 EndpointSlices 生成 `endpoints`。
5. 用同一批 EndpointSlices 按 pod ordinal 生成 `stateful_instances`。
6. 不查询 `agsvc-*` 或 `sisvc-*` EndpointSlices。

该逻辑与 `exposure` 无关。

## 迁移方案

## 默认行为迁移

新默认：

```text
kind: stateful + exposure unspecified -> aggregate
```

这会改变旧 stateful 服务的默认暴露行为，因此需要迁移窗口。

## gateway 迁移保护

当前 `projects/game/gateway/service.yaml` 仍依赖 per-instance 暴露，应显式增加：

```yaml
kind: stateful
exposure: per-instance
```

这样 deploy 支持 aggregate 后，gateway 仍保持旧行为，直到后续独立讨论并实现 gateway 内部路由方案。

## 切换模式的 prune 行为

从 `per-instance` 切到 `aggregate`：

* 创建/保留 `svc-*` headless。
* 创建 `agsvc-*`。
* 创建 `route-*`。
* prune 旧 `sisvc-*`。
* prune 旧 `sirt-*`。

从 `aggregate` 切到 `per-instance`：

* 保留 `svc-*` headless。
* 创建 `sisvc-*`。
* 创建 `sirt-*`。
* prune 旧 `agsvc-*`。
* prune 旧 `route-*`。

外部域名语义可能变化，文档必须提示用户提前协调 DNS / 客户端。

## 关键决策

### 决策 1：aggregate 是默认模式

原因：

* deploy 主要面向应用服务部署。
* 应用型 stateful 服务通常应作为整体对外提供能力。
* 不默认暴露 StatefulSet ordinal，避免副本拓扑成为外部 API 契约。

### 决策 2：per-instance 是显式 escape hatch

原因：

* 仍需支持少数必须外部定向访问实例的服务。
* 兼容当前 gateway 迁移窗口。
* 保留 ClickHouse/Kafka/ZooKeeper 类节点级暴露能力的最低支持，但不作为默认应用模型。

### 决策 3：aggregate Service 使用 `agsvc-*` 前缀

原因：

* 现有 `svc-*` 已用于 stateful headless Service。
* 避免 headless Service 与 ClusterIP Service 命名冲突。
* 资源含义清晰：`svc-*` 是 governing/headless，`agsvc-*` 是 aggregate traffic backend。

### 决策 4：stateful discovery 统一从 headless 推导

原因：

* headless Service 是 StatefulSet 实例身份的根。
* aggregate 与 per-instance 切换不应改变 discovery API。
* 避免 per-instance EndpointSlice 的 N+1 查询。
* 避免 discovery 依赖 routing backend Service。

### 决策 5：默认值由服务端控制

原因：

* deploy service 是 desired state 的系统事实来源。
* 直接调用 API 的非 CLI 客户端应获得同样默认行为。
* proto 中 `UNSPECIFIED = 0` 只表达未指定，不承载业务默认值。

### 决策 6：aggregate 没有 HTTP 也生成 `agsvc-*`

原因：

* aggregate exposure 表示服务整体可被访问。
* ClusterIP Service 对内部调用与 endpoint 对齐有价值。
* 避免 Service 资源集合随 HTTPRoute 配置变化。

### 决策 7：per-instance HTTPRoute 支持全部 matches

原因：

* 与 stateless / aggregate 的 HTTPRoute 能力一致。
* 避免当前只使用 `Matches[0]` 的隐藏限制。
* 避免用户配置多个 matches 时静默丢失语义。

## 验收标准

完成后应满足：

* `kind: stateful` 未设置 exposure 时，服务端归一化为 aggregate。
* `kind: stateless` 设置 exposure 报错。
* aggregate stateful 生成 `StatefulSet + svc-* + agsvc-*`，配置 HTTP 时额外生成 `route-* -> agsvc-*`。
* per-instance stateful 生成 `StatefulSet + svc-* + sisvc-* + sirt-*`，不生成 `agsvc-*`。
* `QueryStatefulServiceEndpoints` 不查询 `agsvc-*` / `sisvc-*`，只从 headless EndpointSlices 推导 `endpoints` 和 `stateful_instances`。
* `stateful_instances` 按 index 升序返回。
* 实例存在但未 ready 时，返回该 index 且 endpoints 为空。
* `replicas=0` 的 proto 输入在服务端归一化为 `1`。
* 当前 gateway service.yaml 显式配置 `exposure: per-instance`，迁移期间行为不变。

## 未来规划

后续可独立设计：

* gateway 在 aggregate 模式下的内部 session owner 路由 / proxy / redirect 方案。
* scale-to-zero 的 proto optional 表达与语义。
* 针对底层组件的更完整 node advertised address 模型。
* 按业务角色而非 ordinal 的 exposure 模型。
* 独立的部署诊断接口，用于检查 `route-* -> agsvc-*`、`sirt-* -> sisvc-*` 的 backend 健康状态。
