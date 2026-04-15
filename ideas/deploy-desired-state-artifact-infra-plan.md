# 基于 Artifact/Infra 平铺 DesiredState 的改造方案

## 目标

基于当前 `projects/infra/deploy/deploy.proto` 的新定义，将 deploy service 的 `EnvironmentDesiredState` 从“编译后的 service/http/infra 结果”重构为更接近服务端输入边界的结构：

- `artifacts[]`：上传**已解析完成、可直接部署**的 Artifact 描述；`http` 归属到对应 Artifact。
- `infras[]`：上传基础设施实例描述。
- 不再保留独立的 `services[]` / `http_routes[]`。
- 不再使用额外包裹层来同时承载 artifact / infra。

本方案只覆盖**为适配新 proto 所需的代码改动设计**，不展开未来能力扩展。

## 背景判断

当前代码中存在两个不一致的边界：

1. `tools/deploy/pkg/config` 面向的是高层 deploy 配置：`artifact` / `infra` / `http`。
2. `tools/deploy/v2/compiler/compiler.go` 会把它编译为旧 proto 中的：
   - `ServiceSpec`
   - `InfraSpec`
   - `HTTPRouteSpec`

这导致服务端接收的是“拆散后的运行时输入”，而不是“按部署单元组织的 deploy 输入”。proto 改造后的正确方向是：

- `ArtifactSpec` 承载 artifact 的部署输入：`name/app/image/ports/replicas/tls_enabled/http`
- `InfraSpec` 承载 infra 的部署输入：`resource/profile/name/app/persistence`

## 关键决策

### 决策 1：服务端领域模型要跟随 proto，改为 Artifact/Infra 平铺

`projects/infra/deploy/domain.DesiredState` 当前仍是：

- `Services []*ServiceSpec`
- `Infras []*InfraSpec`
- `HTTPRoutes []*HTTPRouteSpec`

此结构必须改为：

- `Artifacts []*ArtifactSpec`
- `Infras []*InfraSpec`

原因：

- 否则 handler 只能做 proto 到旧模型的“回填拆分”，新边界会在入口处被抹平。
- `http` 已经明确归属到 artifact，继续维持 `HTTPRoutes` 独立集合会让模型重新退回旧结构。

### 决策 2：runtime/k8s 转换时再从 Artifact 派生 Deployment/Service/HTTPRoute

`projects/infra/deploy/runtime/k8s/converter.go` 不应再假设输入是独立的 service 与 http route 集合，而应：

- 对每个 `ArtifactSpec` 生成一个 `DeploymentWorkload`
- 若 `artifact.http != nil` 且有路由配置，则再生成一个 `HTTPRouteWorkload`
- 对每个 `InfraSpec` 生成一个 `MongoDBWorkload`

这样 runtime/k8s 仍然保留“工作负载对象是 K8s 侧内部模型”的职责。

### 决策 3：CLI compiler 改为直接产出 ArtifactSpec / InfraSpec

`tools/deploy/v2/compiler/compiler.go` 已经拥有构建 `ArtifactSpec` 所需的全部数据：

- `artifact.Name`
- `serviceConfig.App`
- `imageRef`
- `artifact.TLS`
- `artifact.Ports`
- `defaultServiceReplicas`
- `deployService.HTTP`

因此 compiler 的职责不是再把 artifact 与 http 拆成两个 proto 集合，而是直接拼出一个 `deploy.ArtifactSpec`。

## 实现锚点

### 1. CLI 编译层

#### 改造目标

把 `tools/deploy/v2/compiler/compiler.go` 从：

- 产出 `desiredState.Services`
- 产出 `desiredState.HttpRoutes`

改为：

- 产出 `desiredState.Artifacts`
- 产出 `desiredState.Infras`

#### 具体锚点

- `tools/deploy/v2/compiler/compiler.go`
  - `Compile`
  - `compileHTTPRoute`
- `tools/deploy/v2/compiler/compiler_test.go`
- `tools/deploy/v2/apply_test.go`
- `tools/deploy/v2/client/client_test.go`

#### 设计细节

- 将 `compileHTTPRoute` 重命名或重构为 `compileArtifactHTTP`，返回 `*deploy.ArtifactHTTPSpec`。
- 在 `Compile` 内构造：
  - `deploy.ArtifactSpec`
  - `deploy.InfraSpec`
- artifact 路由校验仍保留在 compiler 侧，因为 backend 端口名依赖 artifact 的 port 定义。

### 2. 服务端 proto 映射层（handler）

#### 改造目标

`projects/infra/deploy/handler.go` 要从旧的 proto↔domain 三段映射：

- services
- infras
- http_routes

切换为：

- artifacts
- infras

#### 具体锚点

- `projects/infra/deploy/handler.go`
  - `toProtoDesiredState`
  - `fromProtoDesiredState`
  - `toProtoServices` / `fromProtoServices`（删除或替换）
  - `toProtoHTTPRoutes` / `fromProtoHTTPRoutes`（删除或替换）
- `projects/infra/deploy/handler_test.go`

#### 设计细节

- 新增：
  - `toProtoArtifacts`
  - `fromProtoArtifacts`
  - `toProtoArtifactPorts`
  - `fromProtoArtifactPorts`
  - `toProtoArtifactHTTP`
  - `fromProtoArtifactHTTP`
- `InfraSpec` 的映射逻辑保留，但字段改为 `persistence.enabled` 嵌套结构。

### 3. 领域模型与校验

#### 改造目标

`projects/infra/deploy/domain` 要以 Artifact/Infra 为新的权威 desired state。

#### 具体锚点

- `projects/infra/deploy/domain/spec.go`
- `projects/infra/deploy/domain/environment.go`
- `projects/infra/deploy/domain/spec_test.go`
- `projects/infra/deploy/domain/environment_test.go`
- `projects/infra/deploy/domain/worker_test.go`

#### 设计细节

- 删除或替换：
  - `ServiceSpec`
  - `ServicePortSpec`
  - `HTTPRouteSpec`
- 新增：
  - `ArtifactSpec`
  - `ArtifactPortSpec`
  - `ArtifactHTTPSpec`
- `HTTPRouteRule` / `HTTPPathRule` 可保留复用。
- `Environment.Validate` 需要从“route 必须引用已有 service/port”改为：
  - artifact 名称唯一
  - artifact 端口定义合法
  - `artifact.http.matches[].backend` 必须引用本 artifact 的 port name

### 4. 持久化层（Mongo Repository）

#### 改造目标

Mongo 存储结构要跟随 domain desired state 调整。

#### 具体锚点

- `projects/infra/deploy/storage/mongo.go`
- `projects/infra/deploy/storage/mongo_test.go`

#### 设计细节

- 文档结构从：
  - `services`
  - `infras`
  - `http_routes`

  改为：
  - `artifacts`
  - `infras`
- 删除旧的 `mongoServiceSpec` / `mongoHTTPRouteSpec`，合并为：
  - `mongoArtifactSpec`
  - `mongoArtifactPortSpec`
  - `mongoArtifactHTTPSpec`
- `serviceSpecsToMongo` / `httpRouteSpecsToMongo` 等函数改为 artifact 对应函数。

### 5. runtime/k8s 转换层

#### 改造目标

`projects/infra/deploy/runtime/k8s/converter.go` 要从新的 artifact/infra 输入恢复生成内部 K8s workload。

#### 具体锚点

- `projects/infra/deploy/runtime/k8s/converter.go`
- `projects/infra/deploy/runtime/k8s/converter_test.go`
- `projects/infra/deploy/runtime/k8s/executor_test.go`

#### 设计细节

- `ConvertToWorkloads` 的主循环改为：
  - 遍历 `desiredState.Artifacts`
    - 生成 `DeploymentWorkload`
    - 若存在 `artifact.http`，生成 `HTTPRouteWorkload`
  - 遍历 `desiredState.Infras`
    - 生成 `MongoDBWorkload`
- `convertHTTPRouteMatches` 改为接收 `ArtifactPortSpec` 与 `HTTPRouteRule`。
- 不再需要 `serviceSpecMap` 和独立 `desiredState.HTTPRoutes`。

### 6. 运行时执行层的期望状态测试

#### 改造目标

executor 逻辑本身未必需要大改，但测试构造的输入需要全面替换。

#### 具体锚点

- `projects/infra/deploy/runtime/k8s/executor_test.go`

#### 设计细节

- 现有辅助构造函数：
  - `newExecutorTestServiceSpec`
  - `newExecutorTestHTTPRouteSpec`

  要收敛成：
  - `newExecutorTestArtifactSpec`
  - `newExecutorTestArtifactHTTP`

## 推荐实施顺序

1. **domain 模型与校验**
   - 先让服务端内部模型与 proto 对齐。
2. **handler proto↔domain 映射**
   - 打通 API 入口与内部模型。
3. **storage 持久化结构**
   - 保证仓储层能存取新结构。
4. **runtime/k8s converter**
   - 从新模型重新生成 K8s workload。
5. **CLI compiler 与客户端测试**
   - 最后让请求生产端切换到新 proto。

这个顺序的好处是：服务端先形成稳定目标模型，CLI 最后再切换，便于逐层验证。

## 验收标准

满足以下条件视为改造完成：

1. `EnvironmentDesiredState` 在代码中只体现为：
   - `artifacts[]`
   - `infras[]`
2. 服务端 domain/storage/runtime 不再依赖：
   - 旧 `ServiceSpec`
   - 旧 `HTTPRouteSpec`
3. runtime/k8s 能从单个 `ArtifactSpec` 同时派生：
   - Deployment
   - Service
   - 可选 HTTPRoute
4. CLI compiler 直接产出：
   - `deploy.ArtifactSpec`
   - `deploy.InfraSpec`
5. 相关 Bazel 单测全部通过：
   - `//projects/infra/deploy/domain:domain_test`
   - `//projects/infra/deploy:deploy_test`
   - `//projects/infra/deploy/storage:storage_test`
   - `//projects/infra/deploy/runtime/k8s:k8s_test`
   - `//tools/deploy/v2/...` 对应测试目标

## 未来规划（本次不做）

- 将 `replicas` 从 compiler 常量默认值抽到 deploy 配置层。
- 进一步评估 image 是否也应由服务端解析/补全，而不是由 CLI 完成。
- 若未来支持更多 artifact 类型，再考虑把 `ArtifactSpec` 拆为带 `type` 的 oneof。当前阶段不需要预先设计。
