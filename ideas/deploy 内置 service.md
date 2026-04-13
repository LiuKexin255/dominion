## 目标

给 `deploy` 配置增加一类内置的 infra service。用户在 `deploy.yaml` 中声明后，由 `deploy` 工具直接生成对应的 Kubernetes 资源并部署到环境中，无需再单独编写 `service.yaml`。

这类内置 infra 主要面向固定形态、独立部署、接入方式标准化的资源，例如 `mysql`、`redis`。应用侧通过仓库内提供的 helper 接入，不直接感知底层资源模板、镜像版本、鉴权细节和服务发现实现。

本方案只整理**本次要实现的部分**。未来扩展方向单独放到文末章节，不混入本次实现范围。

## 本次实现范围

### 目标边界

本次实现只覆盖以下范围：

1. 在 `deploy` 配置中支持声明内置 infra service。
2. `deploy` 根据配置直接生成并下发对应的 Kubernetes 资源。
3. 内置 infra 资源归属于环境，删除环境时删除运行资源，不删除数据。
4. 当资源启用持久化时，同一环境下再次部署同一逻辑资源，可复用已有 PVC。
5. 应用侧通过 helper 访问资源，不直接暴露部署细节。

### 非目标

本次不解决以下问题：

1. 生产级高可用、备份恢复、跨环境迁移。
2. 数据内容级兼容性检查。
3. 某个具体资源类型的最终 Secret 生命周期策略。
4. 某个具体资源类型的完整可变更字段矩阵。

## 配置形态

实现后的 `deploy.yaml` 形态如下：

```yaml
app: grpc-hello-world
template: deploy
desc: "开发环境"
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
  - artifact:
      path: //experimental/grpc_hello_world/gateway/service.yaml
      name: gateway
    http:
      hostnames:
        - hello.liukexin.com
      matches:
        - backend: http
          path:
            type: PathPrefix
            value: /v1
  - infra:
      resource: mysql
      profile: dev-single
      name: my-mysql
      persistence:
        enabled: true
```

### 字段说明

#### `resource`

内置资源类型，例如 `mysql`、`redis`。

#### `profile`

资源预设类型。该字段用于选择平台内置模板和默认参数，不再使用原先的 `type: small` 语义。

#### `name`

逻辑资源名，用于在同一环境中区分多个同类型资源。

这里显式使用 `name`，不再使用 `uri`：

1. 仓库现有 `config.URI` 语义是“配置文件定位 URI”。
2. 内置 infra 这里需要的是“逻辑资源名”，而不是配置定位信息。
3. 使用 `name` 可以避免和现有 `DeployConfig.URI` / `ServiceConfig.URI` 语义冲突。

#### `persistence`

本次先设计为对象，当前仅保留一个字段：

```yaml
persistence:
  enabled: true
```

保留为对象是为了后续扩展存储类、容量等字段时不破坏配置结构。

## 资源生成与模板管理

### 模板管理方式

内置资源模板继续使用 **Go 代码构建**，不引入独立的 YAML 模板体系。

原因：

1. 仓库当前 `tools/deploy/pkg/k8s` 已经采用 Go 代码构建 `Deployment`、`Service`、`HTTPRoute`。
2. 继续沿用同一条路径，更容易保持类型安全、测试方式和重构一致性。
3. 平台静态默认参数可继续放在 `static_config.yaml`，但资源结构模板本身由代码生成。

### 本次新增的模型方向

本次实现中，`deploy` 将从现有的两类对象：

1. 引用外部 `service.yaml` 的 `artifact service`
2. 直接在 `deploy.yaml` 中声明的 `infra service`

统一进入 `deploy -> workload -> k8s object` 生成链路。

## identity 与命名规则

### appInstance

`appInstance` 沿用现有 `tools/deploy/pkg/k8s/workload.go` 中的 `shortNameHash(app, dominionApp)` 逻辑。

只要 `app` 与 `dominionApp` 不变，`appInstance` 就不变。

### 规范化规则

所有参与资源 identity 和资源名生成的字段，与 `newObjectName` 的标准化规则保持一致：

1. 先 `TrimSpace`。
2. 转换为小写。
3. 将所有非 `[a-z0-9-]` 的连续字符替换为 `-`。
4. 去掉首尾 `-`。
5. 若标准化后为空，则该片段视为无效。
6. 最终资源名超过 63 个字符时，直接报错，中止流程，不做截断或漂移。

### 资源标识

本次暂定统一资源标识为：

`{resource}-{dominionEnv}-{name}-{appInstance}`

若需要派生 PVC 相关对象，可在末尾追加 `-pvc`。

该标识用于：

1. 生成资源名。
2. 作为复用和兼容性判断的基础。
3. 区分同一环境中的多个同类型内置资源。

### 逻辑资源等价判定

满足以下条件时，视为同一环境中的同一逻辑资源再次部署：

1. 同一 `app`
2. 同一 `dominionApp`
3. 同一 `resource`
4. 同一 `name`

若上述任一项变化，则视为不同资源，不复用已有持久化资源。

## 生命周期与删除语义

### 环境归属

内置 infra 资源归属于环境。

删除环境时：

1. 删除该环境下的运行资源。
2. 不删除数据。

这里的“数据”当前至少包括 PVC，对未来其他保留类资源不在本次展开。

### 持久化资源复用

当 `persistence.enabled=true` 时：

1. 同一逻辑资源再次部署，允许复用旧 PVC。
2. 若旧 PVC 状态不符合最低兼容条件，则直接报错，中止流程。

### 旧 PVC 的最低检查边界

本次只做最低程度检查，目标是保证 PVC 可以正常挂载，不尝试判断更高层数据内容是否兼容。

最低检查包括：

1. PVC 存在。
2. PVC 归属正确。
3. `storageClassName` 兼容。
4. `accessModes` 兼容。
5. `volumeMode` 兼容。
6. 容量满足本次申请要求。

若上述任一条件不满足，直接报错，中止流程。

## adopt-or-abort 规则

### 基本原则

只允许复用或更新由 Dominion 管理且 identity 匹配的对象；否则直接中止，不自动接管。

### 环境级 identity

环境级归属由以下标签共同确定：

1. `app.kubernetes.io/managed-by`
2. `dominion.io/app`
3. `dominion.io/environment`

### 资源级 identity

在环境级 identity 基础上，再加：

1. `app.kubernetes.io/component`

也就是说：

1. 环境级 identity 用于界定“这个对象是否属于当前环境”。
2. 资源级 identity 用于界定“这个对象是否属于当前逻辑资源实例”。

### adopt-or-abort 判定

只有当对象满足以下条件时，才允许复用或更新：

1. 由 Dominion 管理。
2. 环境级 identity 匹配。
3. 资源级 identity 匹配。

若发现预存对象同名但不满足上述条件，则直接 abort。

## 服务发现与接入方式

不同类型的内置资源也需要服务发现。

本次方向为：

1. 将 `pkg/grpc/solver` 中与环境相关的发现逻辑抽象到 `pkg/solver`。
2. `pkg/grpc/solver` 保留 gRPC 自身包装。
3. 各类内置 infra helper 再基于通用 solver 封装各自接入逻辑。

应用侧通过 helper 接入资源，不直接暴露底层资源模板、部署细节和鉴权细节。

## 关键细节锚点

以下内容作为本方案的关键锚点，后续具体实现不能偏离：

3. `appInstance` 复用现有 `shortNameHash(app, dominionApp)` 规则。
4. identity 规范化规则与 `newObjectName` 保持一致。
5. 资源名超长时直接报错，不做名称漂移。
6. 同一逻辑资源再次部署时，持久化资源可复用；不兼容直接 fail fast。
7. 删除环境只删除运行资源，不删除数据。
8. adopt-or-abort 只允许处理 Dominion 管理且 identity 匹配的对象。
9. 内置资源模板使用 Go 代码构建，不引入独立 YAML 模板体系。
10. 应用侧通过 helper 访问内置 infra，不直接暴露部署细节。

## 涉及具体资源落地时的约束

以下内容不在本次统一方案中展开，但当引入某个具体资源类型时，必须逐项明确：

1. 该资源类型的最终对象集合是什么，例如是否包含 StatefulSet、Service、Secret、ConfigMap 等。
2. 哪些字段允许更新，哪些字段属于不兼容变更。
3. 是否由实例级资源，删除和复用策略是什么。
4. helper 最终暴露给应用侧的接入接口是什么。
5. 该资源类型需要补充哪些 preflight 校验。
6. 该资源类型的默认参数和 profile 具体如何映射。

该章节作为后续具体资源方案的统一约束。

## 未来计划

未来扩展方向不属于本次实现范围，单独记录如下：

1. 支持更多内置 infra 资源类型。
2. 扩展 `persistence` 对象，增加更多存储相关配置。
3. 完善不同资源类型的兼容性与更新策略。
4. 逐步补齐更高层的数据内容级检查能力。
5. 视需要评估更复杂的生产级资源能力。
