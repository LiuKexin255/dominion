# Deploy artifact OSS 支持方案

## 目标

本方案用于在现有 `service.yaml`、`deploy` CLI 与 `deploy service` 中为 artifact 增加 OSS 访问能力，目标是：

* 让服务产物可以在 `service.yaml` 中显式声明“该 artifact 需要对象存储访问能力”，并由 deploy runtime 自动注入运行所需的 S3 认证信息。
* 让 OSS 能力与现有 `artifacts[].tls` 保持同层语义：都属于 artifact 的运行时能力声明，而不是环境级覆盖参数。
* 让 deploy service 在保存环境前，继续通过 runtime 暴露的保留环境变量集合，拒绝用户对平台注入的 S3 凭证环境变量进行覆盖。
* 让业务侧通过统一的 `pkg/s3` 公共库创建可用的 SeaweedFS S3 client，避免各服务重复处理 endpoint、认证和默认 region。
* 让 stateless / stateful 两类 artifact workload 在 OSS 语义上保持一致。

## 范围

本方案仅覆盖 artifact workload 的 OSS 访问支持：

* `service.yaml` 中 `artifacts[].oss` 配置模型
* CLI schema / config / compiler 调整
* `deploy.proto` / domain / storage 模型调整
* deploy service 与 runtime/k8s 中的 OSS 环境变量注入
* runtime 保留环境变量集合扩展
* `pkg/s3` 公共库
* 相关单测与文档调整

本方案不包括：

* `deploy.yaml` 中的环境级 OSS 覆盖配置
* bucket、region、endpoint 的 deploy 配置化
* bucket 生命周期管理、自动建桶、清桶或权限编排
* AWS S3 专有能力（如 replication、notification、select object）
* TLS 注入模型调整（TLS 继续保持现有文件挂载方案）

## 当前问题

当前仓库实现中：

* `tools/deploy/pkg/config/config.go` 中的 `ServiceArtifact` 只有 `name`、`target`、`tls`、`ports`，没有 `oss` 字段。
* `tools/deploy/pkg/schema/service.schema.json` 中也没有 `artifacts[].oss` 配置项。
* `tools/deploy/v2/compiler/compiler.go` 会将 `service.yaml` 中的 artifact 编译为 `projects/infra/deploy/deploy.proto` 中的 `ArtifactSpec`，但当前模型没有承载 OSS 开关。
* `projects/infra/deploy/runtime/k8s/builder.go` 当前只会为 artifact workload 注入平台保留环境变量与 TLS 文件路径环境变量，没有 S3 凭证注入能力。
* `projects/infra/deploy/runtime/k8s/static_config.go` 当前只有 `TLSConfig` 和 MongoDB profile，没有 OSS 静态配置模型。
* 仓库中尚无统一的 S3 client 公共库，业务代码无法复用统一的 endpoint 与凭证读取逻辑。

已确认当前 deploy 平台对 artifact env 冲突的统一处理方式已经在 `design/deploy_artifact_env_support.md` 中设计并落地：

* runtime 通过 `ReservedEnvironmentVariableNames` 暴露保留环境变量集合
* deploy service 在保存环境前执行 env 冲突校验
* runtime/k8s 在构造 Deployment / StatefulSet 时负责注入保留环境变量

因此，若要支持 OSS，应该复用这条既有链路，而不是在 CLI 或业务库中自行拼装环境变量约定。

## 最终模型

## 配置模型

在 `service.yaml` 中为 `artifacts[]` 增加可选布尔字段：

```yaml
name: session-service
app: game
desc: session service
artifacts:
  - name: service
    target: :service_image
    oss: true
    ports:
      - name: grpc
        port: 50051
```

字段语义：

* `artifacts[].oss` 类型为 `bool`。
* `true` 表示该 artifact 需要 deploy runtime 自动注入 S3 认证环境变量。
* `false` 或未配置表示不注入 OSS 相关环境变量。

本方案明确不将 OSS 配置做成对象，也不在 deploy 配置层承载 bucket / region / endpoint。原因是：

* endpoint 在当前场景下是平台级固定值，不是 artifact 配置差异。
* SeaweedFS 对 region 不承担 AWS S3 那样的路由语义。
* bucket 由业务在运行时动态决定，不属于 deploy desired state。

## 静态配置模型

在 `projects/infra/deploy/runtime/k8s/config/static_config.yaml` 中增加：

```yaml
oss:
  secret: "seaweedfs-s3-credentials"
  access_key: "accessKey"
  secret_key: "secretKey"
```

字段语义：

* `oss.secret`：Kubernetes Secret 名称。
* `oss.access_key`：Secret 中 access key 对应的键名。
* `oss.secret_key`：Secret 中 secret key 对应的键名。

对应 Go 模型建议新增：

```go
type OSSConfig struct {
    Secret    string `yaml:"secret"`
    AccessKey string `yaml:"access_key"`
    SecretKey string `yaml:"secret_key"`
}
```

并挂入 `K8sConfig`。

## 运行时环境变量模型

当 `artifacts[].oss = true` 时，deploy runtime 为容器直接注入：

* `S3_ACCESS_KEY`
* `S3_SECRET_KEY`

这两个环境变量均通过 `SecretKeyRef` 来自 `static_config.yaml` 指定的 Secret，而不是通过文件挂载。

### 为什么 OSS 使用 env 而不是文件挂载

与 TLS 不同，S3 认证只需要短字符串形式的 access key / secret key，不需要证书文件语义。

因此，直接使用 `EnvVarSource.SecretKeyRef`：

* 能减少 builder 与业务库的中间转换成本
* 更贴近 `minio-go/v7` 的认证输入形式
* 不需要额外维护 `_FILE` 环境变量约定

TLS 保持现有文件挂载方案，不在本次设计中调整。

## 业务公共库模型

新增 `pkg/s3` 公共库，用于屏蔽 SeaweedFS endpoint、默认 region 与认证读取逻辑。

对外 API：

```go
func NewS3Client(region string) (*minio.Client, error)
```

语义：

* `region` 允许为空。
* 当 `region == ""` 时，库内回退到固定默认值 `us-east-1`。
* endpoint 固定写在 `pkg/s3` 中，不支持按环境切换。
* access key / secret key 从 `S3_ACCESS_KEY` / `S3_SECRET_KEY` 读取。

本方案不在 `pkg/s3` 中绑定 bucket，也不包装为业务专用对象；bucket 仍由调用方在具体 API 调用时传入。这与 `minio-go/v7` 的原生职责边界保持一致。

## SeaweedFS 适配结论

本方案选用 `minio-go/v7` 作为 S3 client SDK。选型结论如下：

* SeaweedFS 提供兼容 S3 的核心 bucket / object API，满足当前业务所需的对象存取能力。
* 当前方案只依赖 access key / secret key + 固定 endpoint，不依赖 AWS 专有特性。
* region 在当前场景下只作为 client 初始化参数保留，不承担 deploy 或路由语义。

因此，本方案明确将 `pkg/s3` + `minio-go/v7` 作为 SeaweedFS 的统一接入方式。

## 模型设计

### CLI 配置与编译模型

建议按现有 `tls` 链路扩展：

* `tools/deploy/pkg/schema/service.schema.json`：为 `artifacts[]` 增加 `oss` 布尔字段
* `tools/deploy/pkg/config/config.go`：为 `ServiceArtifact` 增加 `OSS bool`
* `tools/deploy/v2/compiler/compiler.go`：将 `artifact.OSS` 编译进 `deploy.ArtifactSpec`

### proto / domain / storage

建议在 `projects/infra/deploy/deploy.proto` 的 `ArtifactSpec` 中新增 `oss_enabled` 字段，并在以下层同步透传：

* `projects/infra/deploy/domain/spec.go`
* `projects/infra/deploy/storage/mongo.go`
* `projects/infra/deploy/runtime/k8s/model.go`
* `projects/infra/deploy/runtime/k8s/converter.go`

该字段是 artifact desired state 的一部分，采用与 deploy 现有模型一致的全量替换语义。

### runtime/k8s 静态配置模型

在 `projects/infra/deploy/runtime/k8s/static_config.go` 中新增 `OSSConfig` 并加入 `K8sConfig`：

* `TLS` 继续表达 TLS 文件挂载所需的静态配置
* `OSS` 表达 S3 凭证注入所需的 Secret 名与键名映射

两者并存，但采用不同的注入方式。

## 代码分层

## CLI

CLI 层职责如下：

* `tools/deploy/pkg/schema`：声明 `artifacts[].oss` 的配置结构
* `tools/deploy/pkg/config`：读取 `artifacts[].oss`
* `tools/deploy/v2/compiler`：将 OSS 开关编译进 `ArtifactSpec`

CLI 不承担 S3 env 注入逻辑，也不承担 Secret 键名解释逻辑。

## deploy service

deploy service 层职责如下：

* `handler`：沿用现有保存前 env 冲突校验流程
* `domain`：承接 `oss_enabled` desired state 表达
* `storage`：持久化 `oss_enabled`
* `runtime`：对外暴露包括 S3 变量在内的保留环境变量集合
* `runtime/k8s/converter`：将 `oss_enabled` 透传到 workload
* `runtime/k8s/builder`：按开关注入 S3 认证环境变量

## runtime/k8s

runtime/k8s 层职责如下：

* `static_config.go`：加载并校验 `oss.secret` / `oss.access_key` / `oss.secret_key`
* `builder.go`：通过 `SecretKeyRef` 为 Deployment / StatefulSet 注入 `S3_ACCESS_KEY` / `S3_SECRET_KEY`
* `ReservedEnvironmentVariableNames`：将新的 S3 变量加入保留集合

注入顺序建议保持与现有 env 设计一致：

1. 用户 `artifact.env`（按 key 排序）
2. 平台基础保留环境变量
3. TLS 相关保留环境变量（若启用）
4. OSS 相关保留环境变量（若启用）

这样既保持输出稳定，也不会破坏现有 TLS 语义。

## 业务公共库

`pkg/s3` 的职责仅限于：

* 读取 `S3_ACCESS_KEY` / `S3_SECRET_KEY`
* 应用固定 endpoint
* 在 region 为空时应用默认 region
* 构造 `minio.Client`

它不承担：

* 自动建桶
* bucket 生命周期管理
* deploy 配置读取
* Secret / ConfigMap 解析

## 关键细节

## 为什么 OSS 放在 `service.artifacts[]`

`tls` 已经是 artifact 级运行时能力声明。OSS 与 TLS 在语义上同属“该构建产物在运行时需要的平台能力注入”，因此应放在同一层，而不是放到 `deploy.yaml` 的环境级配置中。

这样可以表达：

* 某个 artifact 天生依赖对象存储能力
* 同一个 artifact 在不同环境中保持相同运行时依赖声明

## 为什么不把 endpoint / bucket / region 放进 deploy 配置

当前业务约束已明确：

* endpoint 是平台级固定值
* SeaweedFS 的 region 不承担强语义
* bucket 由业务在运行时动态决定

因此，把这些字段做进 deploy 模型只会增加配置复杂度，并把不需要由 deploy 控制的运行参数混入 desired state。

## 为什么保留环境变量校验仍走既有 env 机制

`design/deploy_artifact_env_support.md` 已经定义了统一规则：

* runtime 是保留环境变量集合的唯一来源
* deploy service 在保存前执行冲突校验
* runtime builder 负责实际注入

OSS 只是新增一组平台保留环境变量，应复用这条链路，而不是再单独设计一套 OSS 冲突检查机制。

## 为什么 `pkg/s3` 直接返回 `*minio.Client`

本次目标是提供统一、最薄的 SeaweedFS S3 接入入口，而不是引入新的业务抽象层。

直接返回 `*minio.Client` 的好处是：

* 与 `minio-go/v7` 原生用法一致
* 不需要额外设计 bucket 绑定语义
* 调用方仍可自由使用 SDK 的标准 API

## 决策详情

### 决策 1：OSS 为 artifact 级布尔开关

原因：

* 与现有 `artifacts[].tls` 保持同层模型
* 当前只需要表达“是否注入对象存储认证能力”
* 避免过早引入对象化配置和多余字段

### 决策 2：OSS Secret 只存 access key / secret key

原因：

* endpoint 为平台级固定值，不属于 Secret
* region / bucket 为运行时动态参数，不属于 deploy 静态配置

### 决策 3：OSS 使用 direct env 注入，TLS 保持文件挂载

原因：

* S3 凭证适合直接作为短字符串 env 提供给 SDK
* TLS 证书链更适合继续保留文件语义
* 不为追求“统一注入方式”而破坏已有 TLS 使用模型

### 决策 4：公共库命名为 `pkg/s3`

原因：

* 代码面对的是 S3 API，而不是抽象“对象存储”概念
* 避免 `oss` 与特定厂商命名混淆

### 决策 5：`pkg/s3` 使用 `minio-go/v7`

原因：

* 更贴合 S3-compatible 场景
* 对固定 endpoint + access key / secret key 的使用方式更直接
* 足以覆盖 SeaweedFS 的当前使用场景

### 决策 6：`NewS3Client(region)` 中 region 允许为空

原因：

* SeaweedFS 对 region 不承担强语义
* 可以减少业务调用方的样板代码

约束：

* 当 region 为空时，回退到固定默认值 `us-east-1`

## 测试要求

本方案需要补齐以下测试：

* `service.yaml` 解析 `artifacts[].oss` 的配置测试
* compiler 将 `oss` 编译进 `ArtifactSpec` 的测试
* proto / domain / storage 的 `oss_enabled` 往返测试
* `K8sConfig` 解析 `oss.secret` / `access_key` / `secret_key` 的测试
* Deployment / StatefulSet 注入 `S3_ACCESS_KEY` / `S3_SECRET_KEY` 的测试
* OSS 启用后保留环境变量冲突校验测试
* `pkg/s3` 在 region 为空时应用默认值的测试
* `pkg/s3` 缺少 `S3_ACCESS_KEY` / `S3_SECRET_KEY` 时的报错测试

## 未来规划

本方案不在本次范围内处理以下能力，后续如有需要另行设计：

* endpoint 按环境切换
* bucket 级配置与自动建桶能力
* 通过 IRSA / STS 等非静态凭证方式接入对象存储
* 更高层的业务 wrapper（例如 bucket-aware helper）
