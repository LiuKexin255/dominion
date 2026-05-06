# Deploy Mongo 容量配置与 Recreate 重启策略方案

## 目标

为 `deploy.yaml` 中的 `mongodb` 基础设施部署增加实例级容量配置，并将由 deploy 工具生成的 MongoDB Deployment 更新策略固定为 `Recreate`。

完成后达到以下效果：

* 用户可以在单个 mongo infra 实例上声明 PVC 容量，不需要新增 profile 才能调整容量。
* 容量格式与 Kubernetes PVC `resources.requests.storage` 保持一致，最大不超过 `1Ti`。
* MongoDB 使用持久化卷时更新容器前会先停止旧 Pod，避免默认滚动更新过程中多个 Pod 同时挂载同一个数据卷。
* 旧配置保持兼容：未配置容量时继续使用静态 profile 中的默认容量。

## 用户配置模型

在 `infra.persistence` 下增加可选字段 `capacity`：

```yaml
services:
  - infra:
      resource: mongodb
      profile: dev-single
      name: mongo
      app: hello-world
      persistence:
        enabled: true
        capacity: 20Gi
```

字段语义：

* `persistence.enabled`：是否启用持久化，保持现有语义。
* `persistence.capacity`：可选，启用持久化时覆盖 profile 默认 PVC 容量。

约束：

* `capacity` 使用 Kubernetes `resource.Quantity` 解析，支持 `20Gi`、`500G`、`1Ti` 等 PVC storage quantity 格式。
* `capacity` 最大值为 `1Ti`，大于 `1Ti` 报错。
* `capacity` 为空时不覆盖 profile 默认值。
* `persistence.enabled: false` 时配置 `capacity` 报错，因为该字段只对 PVC 生效。

## 模型设计

### CLI 配置模型

`tools/release/deploy/pkg/config.DeployInfraPersistence` 增加容量字段：

```go
type DeployInfraPersistence struct {
    Enabled  bool   `yaml:"enabled"`
    Capacity string `yaml:"capacity,omitempty"`
}
```

`deploy.schema.json` 对 `infra.persistence.capacity` 放开字段，做字符串非空约束；精确 quantity 格式和 `1Ti` 上限由 Go 层校验，避免在 JSON Schema 中重复实现 Kubernetes quantity 规则。

### 协议与领域模型

`projects/infra/deploy/deploy.proto` 在 `InfraPersistenceSpec` 中增加字段：

```proto
message InfraPersistenceSpec {
  bool enabled = 1;
  string capacity = 2;
}
```

对应同步扩展：

* `projects/infra/deploy/domain.InfraPersistenceSpec`
* handler 中 proto/domain 双向转换
* `projects/infra/deploy/storage` 中 BSON 持久化模型

旧数据没有 `capacity` 时保持空字符串，运行时回退 profile 默认容量。

### Kubernetes workload 模型

`projects/infra/deploy/runtime/k8s.PersistenceConfig` 增加 `Capacity`，`MongoDBWorkload` 继续通过 `Persistence` 携带持久化设置。

转换规则：

* `domain.InfraSpec.Persistence.Capacity` 传入 `MongoDBWorkload.Persistence.Capacity`。
* 构建 PVC 时，若 workload capacity 非空则使用该值，否则使用 `MongoProfileConfig.Storage.Capacity`。
* PVC 兼容性校验使用同一套最终容量计算，避免创建和校验逻辑不一致。

## 代码分层

### 配置读取层

变更文件：

* `tools/release/deploy/pkg/schema/deploy.schema.json`
* `tools/release/deploy/pkg/config/config.go`

职责：

* schema 允许 `persistence.capacity`。
* `ParseDeployConfig` 后执行语义校验：
  * `enabled: false` 且 `capacity` 非空时报错。
  * `capacity` 非空时必须能被 `resource.ParseQuantity` 解析。
  * 解析后的 quantity 不得大于 `1Ti`。

### 编译层

变更文件：

* `tools/release/deploy/v2/compiler/compiler.go`

职责：

* 将 `DeployInfra.Persistence.Capacity` 编译到 proto `InfraPersistenceSpec.Capacity`。
* 保持当前逻辑：只有启用持久化时才设置 `Persistence`。

### Deploy Service 层

变更文件：

* `projects/infra/deploy/deploy.proto`
* `projects/infra/deploy/handler.go`
* `projects/infra/deploy/domain/spec.go`
* `projects/infra/deploy/storage/mongo.go`

职责：

* 接收 CLI 提交的 desired state。
* 在 domain 和 storage 中保留 capacity。
* 返回环境详情时不丢失 capacity。

### Kubernetes Runtime 层

变更文件：

* `projects/infra/deploy/runtime/k8s/model.go`
* `projects/infra/deploy/runtime/k8s/converter.go`
* `projects/infra/deploy/runtime/k8s/builder.go`

职责：

* 将 infra persistence capacity 转换到 MongoDB workload。
* 生成 PVC 时使用实例级容量覆盖 profile 容量。
* 校验已有 PVC 时继续禁止缩容，允许等于或扩容。
* 构建 MongoDB Deployment 时设置：

```go
Strategy: appsv1.DeploymentStrategy{
    Type: appsv1.RecreateDeploymentStrategyType,
},
```

## 关键细节

### 容量上限

上限使用 Kubernetes quantity 的比较结果判断：

```go
maxMongoPersistenceCapacity := resource.MustParse("1Ti")
```

用户输入解析后与 `1Ti` 比较，大于 `1Ti` 时报错。这样 `1Ti` 合法，`1025Gi`、`2Ti` 非法。

### 默认容量回退

新增一个内部辅助逻辑用于得到最终容量：

1. `strings.TrimSpace(workload.Persistence.Capacity)` 非空时使用实例配置。
2. 否则使用 `strings.TrimSpace(profile.Storage.Capacity)`。

该逻辑同时用于：

* `BuildMongoDBPVC`
* `CheckPVCCompatibility`

避免 PVC 创建和已有 PVC 校验使用不同容量来源。

### 持久化关闭时的 capacity

`capacity` 只对 PVC 有意义，因此 `persistence.enabled: false` 时配置 `capacity` 直接报错。这样可以避免用户误以为容量配置已生效。

### 已存在 PVC 的兼容性

保留当前规则：

* storage class、access modes、volume mode 必须与期望一致。
* existing capacity 小于或等于 desired capacity 时通过。
* existing capacity 大于 desired capacity 时报错，避免缩容。

本次只改变 desired capacity 的来源，不改变兼容性策略。

### MongoDB Deployment 更新策略

当前自举清单 `projects/infra/deploy/k8s.yaml` 中 MongoDB Deployment 已显式使用 `Recreate`，但生成逻辑需要同步设置，确保 deploy 工具生成的 mongo 资源也使用相同策略。

## 决策详情

* 容量字段放在 `persistence` 下，而不是新增 `storage` 顶层字段：容量只在启用 PVC 时生效，归属持久化配置更清晰。
* 容量字段可选：避免破坏旧配置，也保留 profile 作为默认配置来源。
* 上限使用 `1Ti`：符合本次需求，且与 Kubernetes quantity 的二进制单位表达一致。
* `enabled: false` 配置 `capacity` 报错：避免无效配置静默通过。
* MongoDB Deployment 使用 `Recreate`：MongoDB 单实例配合 PVC 时，不应使用默认滚动更新导致新旧 Pod 并存。

## 测试计划

### 单元测试

配置与 schema：

* `capacity: 20Gi` 解析成功。
* `capacity: 1Ti` 解析成功。
* `capacity: 1025Gi` 或 `2Ti` 解析失败。
* `capacity: bad-size` 解析失败。
* `enabled: false` 且配置 `capacity` 解析失败。
* 未配置 `capacity` 的旧配置解析成功。

编译与模型传递：

* deploy config 中的 `capacity` 能编译到 proto `InfraPersistenceSpec.Capacity`。
* handler proto/domain 转换不丢失 capacity。
* storage 保存和读取不丢失 capacity。

Kubernetes 构建：

* 配置实例级 `capacity` 时，生成 PVC 使用实例级容量。
* 未配置实例级 `capacity` 时，生成 PVC 使用 profile 默认容量。
* `CheckPVCCompatibility` 使用实例级容量判断兼容性。
* MongoDB Deployment 的 `spec.strategy.type` 为 `Recreate`。

### 验证命令

完成实现后执行：

```bash
bazel test //tools/release/deploy/...
bazel test //projects/infra/deploy/...
bazel build //...
```

`projects/infra/deploy/README.md` 已说明 deploy service 自身不进行大型测试，因此本方案不要求新增 deploy service 大型测试。

## 未来规划

本方案不扩展 storage class、access modes、volume mode 的实例级配置。如后续需要更细粒度的 PVC 配置，可以在 `persistence` 下继续增加字段，并复用本方案的配置校验与 workload 传递路径。
