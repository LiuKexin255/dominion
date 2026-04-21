# Deploy artifact 环境变量支持方案

## 目标

本方案用于在现有 `deploy` CLI 与 `deploy service` 中为 `deploy.yaml` 的 `artifact` 增加环境变量能力，目标是：

* 让同一个构建产物可以在不同部署环境下显式注入不同的运行参数，而不必修改 `service.yaml`。
* 让环境变量成为 deploy 平台的**期望状态**一部分，随环境更新一起生效、回滚和删除。
* 让 deploy service 在保存环境前，结合 runtime 提供的保留环境变量集合统一校验 artifact env，避免静默覆盖系统注入值。
* 让 stateless / stateful 两类 artifact workload 在 env 语义上保持一致。
* 让最终生成的容器 env 顺序稳定，便于测试与对象比对。

## 范围

本方案仅覆盖 artifact workload 的明文环境变量支持：

* `deploy.yaml` 中 `artifact.env` 配置模型
* CLI schema / config / compiler 调整
* `deploy.proto` / domain / storage 模型调整
* deploy service 中与现有参数校验同层的 env 校验
* runtime/k8s 中 Deployment / StatefulSet 的 env 注入与排序
* 相关单测调整

本方案不包括：

* secret / 密文 / 引用外部密钥系统的能力
* `infra` 资源（如 MongoDB）自定义 env 能力
* CLI 侧的额外前置冲突校验
* 运行时按容器、按端口或按实例差异化 env 注入

## 当前问题

当前仓库实现中：

* `tools/deploy/pkg/config/config.go` 中的 `DeployArtifact` 只有 `path`、`name`、`replicas`，没有 env 字段。
* `tools/deploy/pkg/schema/deploy.schema.json` 也没有 `artifact.env` 配置项。
* `tools/deploy/v2/compiler/compiler.go` 会将 deploy 配置编译成 `projects/infra/deploy/deploy.proto` 中的 `ArtifactSpec`，但该模型当前不承载 env。
* `projects/infra/deploy/runtime/k8s/builder.go` 会为 artifact workload 注入平台保留环境变量，但当前没有用户自定义 env，也没有重名检查。
* `projects/infra/deploy/domain/environment.go` 中 `Environment.Validate()` 已承担 artifact / infra 参数与引用关系校验，但当前没有 env 专项校验入口。
* `projects/infra/deploy/domain/worker.go` 中 `EnvironmentRuntime` 当前只承载 apply / delete / query 能力，没有暴露 runtime 保留环境变量集合。

已确认当前 k8s runtime 为 artifact workload 注入的保留环境变量包括：

* `SERVICE_APP`
* `DOMINION_ENVIRONMENT`
* `POD_NAMESPACE`
* `TLS_CERT_FILE`
* `TLS_KEY_FILE`
* `TLS_CA_FILE`
* `TLS_SERVER_NAME`

因此，如果要支持 `artifact.env`，不能只在 CLI 或 runtime 单点拼接，而必须把 env 纳入 artifact desired state，并把 env 校验并入现有环境参数校验链路。

## 最终模型

## 配置模型

在 `deploy.yaml` 中为 `artifact` 增加可选字段：

```yaml
services:
  - artifact:
      path: //projects/foo/service.yaml
      name: api
      replicas: 2
      env:
        LOG_LEVEL: debug
        FEATURE_FLAG_X: "true"
        TIMEOUT_MS: "1500"
```

字段语义：

* `artifact.env` 类型为 `map[string]string`。
* key 表示容器环境变量名。
* value 表示明文环境变量值。
* 未配置时视为 `nil` / 空集合。

### 更新语义

`artifact.env` 采用与当前 deploy 一致的**全量替换**语义：

* 新配置中存在的 key 会成为最新期望状态。
* 新配置中删除的 key 会从目标环境中移除。
* 不提供 merge patch 或局部追加语义。

### 明文语义

本方案明确将 `artifact.env` 视为**明文配置**：

* 值会进入 proto、domain、存储与环境读取链路。
* 该能力仅用于运行参数调节，不承担 secret 存储职责。

## 保留环境变量规则

本方案不再在领域模型或设计文档中硬编码 artifact 的保留环境变量集合，而是由 `EnvironmentRuntime` 暴露统一接口返回。

建议新增接口：

```go
ReservedEnvironmentVariableNames(ctx context.Context) ([]string, error)
```

规则如下：

* runtime 返回当前部署实现下所有需要保留的环境变量名。
* env 冲突判断按**完全相等**执行，区分大小写。
* env 校验逻辑不直接依赖 `runtime/k8s/builder.go` 中的常量集合，而是依赖 `EnvironmentRuntime` 的公开契约。
* `runtime/k8s` 实现需要保证其返回集合与实际注入的保留环境变量一致。

## 校验语义

### 失败时机

本方案将 env 校验放在 deploy service 的**保存前校验阶段**执行，并与其他参数校验放在同一条校验链路中。

具体要求：

* env 校验属于部署逻辑，不外溢到 CLI 的独立逻辑中。
* `handler` 在保存环境前，先获取 runtime 提供的保留环境变量集合，再调用统一校验。
* `Environment.Validate()` 扩展为支持 env 合法性与保留变量冲突校验，而不是把该逻辑放到 workload 转换后的失败路径里。
* 一旦发现用户 env 非法或与保留变量重名，本次部署在**保存前**失败，不写入新的环境期望状态。

这样既满足“校验属于部署逻辑”，又避免把必然失败的 desired state 落库。

### 合法性要求

除保留变量冲突外，还需约束：

* env key 必须是合法的环境变量名。
* env value 允许为空字符串。

本方案不在此处扩展更复杂的 value source 或 secret 语义。

## 模型设计

### proto / domain

`projects/infra/deploy/deploy.proto` 中的 `ArtifactSpec` 增加 env 字段，用于承接 CLI 编译结果。

`projects/infra/deploy/domain/spec.go` 中的 `ArtifactSpec` 同步增加 env 字段，并将其视为 artifact desired state 的组成部分。

`projects/infra/deploy/domain/environment.go` 中的 `Environment.Validate()` 需要扩展为接收 runtime 保留环境变量集合，将 env 校验与现有 artifact / infra 参数校验放在同一条校验链路中。

### EnvironmentRuntime

`projects/infra/deploy/domain/worker.go` 中的 `EnvironmentRuntime` 增加查询保留环境变量的接口，例如：

```go
type EnvironmentRuntime interface {
    Apply(ctx context.Context, env *Environment, progress func(msg string)) error
    Delete(ctx context.Context, envName EnvironmentName) error
    QueryServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
    QueryStatefulServiceEndpoints(ctx context.Context, envLabel string, app string, service string) (*ServiceQueryResult, error)
    ReservedEnvironmentVariableNames(ctx context.Context) ([]string, error)
}
```

这样 runtime 可以成为保留环境变量集合的唯一来源，避免设计与实现再次分叉。

### storage

`projects/infra/deploy/storage/mongo.go` 的 artifact 持久化结构需要同步增加 env 字段，保证：

* 环境读取时能完整返回当前期望 env
* 更新环境时 env 跟随 desired state 一起替换

### runtime workload

`projects/infra/deploy/runtime/k8s/model.go` 中的 artifact workload 结构需要承载 env，并为以下两条路径共用：

* `DeploymentWorkload`
* `StatefulWorkload`

这样可以保证 stateless / stateful 语义一致，不引入分叉模型。

## 代码分层

## CLI

CLI 层职责如下：

* `tools/deploy/pkg/schema`：声明 `artifact.env` 的配置结构
* `tools/deploy/pkg/config`：读取 `artifact.env`
* `tools/deploy/v2/compiler`：将 env 编译进 `ArtifactSpec`

CLI 不承担保留变量冲突校验职责。

## deploy service

deploy service 层职责如下：

* `handler`：在保存前拉取 runtime 保留环境变量集合，并调用统一环境校验
* `domain`：承接 artifact env 的 desired state 表达
* `storage`：持久化 env
* `domain/environment.Validate`：将 env 校验与其他参数校验放到一起
* `runtime`：对外提供保留环境变量集合
* `runtime/k8s/builder`：将排序后的 env 注入 Deployment / StatefulSet 容器

## runtime/k8s

建议将 env 处理拆为两步：

1. **校验阶段**：在保存前检查 env key 合法性与保留变量冲突
2. **构造阶段**：将用户 env 与平台 env 合并，并输出稳定顺序的 `[]corev1.EnvVar`

排序规则建议为：

* 用户 env 按 key 字典序排序后输出
* 平台保留 env 继续按既有固定顺序追加

这样既不会影响系统注入语义，也能保证测试中的对象输出稳定。

## 关键细节

## 为什么 env 放在 artifact 而不是 service.yaml

`service.yaml` 更适合表达构建产物本身的通用属性；`artifact.env` 属于环境级部署参数。

将 env 放在 `deploy.yaml` 的 `artifact` 下，可以直接表达：

* 同一镜像在不同环境的不同参数
* 参数跟随 deploy desired state 一起替换和回滚

## 为什么不做 CLI 前置冲突校验

本方案明确不希望部署逻辑外溢。

因此：

* CLI 只负责配置读取与编译
* deploy service 的部署链路才是 env 冲突规则的唯一执行点

这样可以避免规则在 CLI / service 双处维护，也避免未来非 CLI 调用方绕过校验。

## 为什么通过 EnvironmentRuntime 暴露保留环境变量

保留环境变量集合本质上属于 runtime 契约，而不是单个校验函数的私有常量。

通过 `EnvironmentRuntime` 暴露该集合有两个好处：

* 领域校验可以与现有参数校验放在一起，不需要反向依赖具体 runtime builder 常量
* runtime 仍然是保留变量集合的唯一权威来源，避免设计和实现分叉

这样失败仍然表现为“请求被拒绝”，而不是“先保存再 reconcile 失败”。

## 为什么需要排序

Go 的 map 本身无稳定遍历顺序。如果直接把 `map[string]string` 转成 `[]EnvVar`：

* 单测会不稳定
* 生成对象 diff 会出现无意义波动

因此，本方案要求在实际构造容器 env 时对用户 env 按 key 排序。

## 决策详情

### 决策 1：env 为 artifact desired state 的一部分

原因：

* 与 deploy 当前 full desired-state replacement 模型一致
* 可以完整表达新增、修改、删除
* 不需要额外引入补丁式语义

### 决策 2：env 明文存储

原因：

* 当前目标是运行参数调整，不是 secret 管理
* 能显著降低本次设计与实现复杂度

约束：

* 文档和接口语义需明确该字段不适合保存敏感信息

### 决策 3：冲突校验仅在部署链路中执行

原因：

* 避免逻辑外溢到 CLI 或额外公共校验层
* 保持规则集中在 deploy service 的实际部署语义中

### 决策 4：保存前失败

原因：

* 避免把必然失败的 env 配置落库
* 对用户表现为请求直接失败，语义更清晰

### 决策 5：只拦截 artifact workload 的平台保留 env

原因：

* 保留环境变量集合由 runtime 统一提供，而不是在设计中单独硬编码
* env 校验与其他参数校验放在一起，可以在保存前拒绝非法配置

## 测试要求

本方案需要补齐以下测试：

* `deploy.yaml` 解析 `artifact.env` 的配置测试
* compiler 将 env 编译进 `ArtifactSpec` 的测试
* proto / domain / storage 的 env 往返测试
* DeploymentWorkload / StatefulWorkload 的 env 注入测试
* 保留变量冲突时报错测试
* 非法 env key 报错测试
* 排序后输出稳定的测试

## 未来规划

本方案不在本次范围内处理以下能力，后续如有需要另行设计：

* secret / valueFrom / ConfigMap / SecretRef 语义
* `infra` 资源自定义 env
* 更细粒度的 per-container 或 per-instance env 覆盖
