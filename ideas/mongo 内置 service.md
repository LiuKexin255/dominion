## 说明

本方案基于 `idea/deploy 内置 service.md` 的总体方案，只补充 `mongo` 作为一种具体内置 infra 资源时的特有设计。

总体方案中已经明确的内容（例如配置入口、统一生成链路、环境归属、持久化复用总原则、adopt-or-abort 总原则等），本方案不再重复展开。

## 目标

在内置 infra 总体方案基础上，落地一个面向开发/测试环境的 **mongo 单体数据库** 方案，使用户在 `deploy.yaml` 中声明 `resource: mongo` 后，即可由 `deploy` 直接生成并部署所需 Kubernetes 资源，并通过仓库内 helper 完成接入。

本方案只覆盖 **mongo 单体数据库**，不包含副本集、分片、生产级高可用等能力。

## 本次实现范围

### 目标边界

本次实现只覆盖以下内容：

1. 定义 `mongo` 资源在内置 infra 体系下的默认落地形态。
2. 明确 `mongo` 所需 Kubernetes 对象集合与各对象职责。
3. 明确 `mongo` 的默认鉴权、初始化、持久化与复用策略。
4. 明确应用侧 helper 的接入方式与最小职责边界。
5. 明确 `mongo` 对应 profile 的默认参数来源与管理方式。

### 非目标

本次不解决以下问题：

1. Mongo 副本集、分片、自动选主与高可用。
2. 备份恢复、跨环境迁移、数据导入导出。
3. 数据内容级兼容性判断。
4. 更细粒度的权限模型，例如应用级用户、多用户隔离、密码轮转。
5. 用户侧自定义镜像、版本、Kubernetes 控制器类型或初始化脚本。

## 配置约束

`mongo` 沿用内置 infra 的统一配置形态：

```yaml
services:
  - infra:
      resource: mongo
      profile: dev-single
      name: my-mongo
      persistence:
        enabled: true
```

本方案不新增 `mongo` 专属配置字段。

原因：

1. 本次目标是先收敛一个固定形态的单体 Mongo 方案。
2. 部署细节由工具内置模板决定，不向用户暴露。
3. 后续若需要扩展 `mongo` 特定可配置项，应在不破坏当前配置形态的前提下增量设计。

## 资源形态

### 默认资源集合

对一个启用的 `mongo` 内置资源，`deploy` 默认生成以下对象：

1. `Deployment`
2. `Service`
3. `Secret`
4. `PersistentVolumeClaim`（当 `persistence.enabled=true` 时）

### 默认控制器选择

本次选择 **Deployment 单副本**，不使用 StatefulSet。

原因：

1. 本次目标是单体 Mongo，不需要 Pod ordinal 与稳定 Pod DNS。
2. 当前更重要的目标是“运行资源与数据资源分离”，而不是“控制器语义上的有状态编排”。
3. 使用独立 PVC 更符合“删除环境时删除运行资源、不删除数据”的总体约束。
4. 现有 `tools/deploy/pkg/k8s` 已经具备 Deployment 风格的对象生成与执行链路，本次扩展更自然。

### Service 形态

本次使用普通 `ClusterIP Service`，暴露 Mongo 默认端口。

本次不使用 headless service。

原因：

1. 应用侧通过 helper 接入，不需要直接依赖 Pod 身份。
2. 单体 Mongo 只需要稳定的集群内访问入口，不需要每 Pod 独立寻址能力。

## 持久化与复用

### PVC 策略

当 `persistence.enabled=true` 时，`deploy` 为该逻辑资源生成独立 PVC，并在后续同一逻辑资源再次部署时尝试复用该 PVC。

### 默认存储参数

Mongo 的默认存储参数放入 `tools/deploy/pkg/k8s/static_config.yaml` 中管理，至少包括：

1. `storageClassName`
2. 容量
3. `accessModes=ReadWriteOnce`
4. `volumeMode=Filesystem`

本次不向用户暴露上述字段。

### 复用时的处理原则

当复用已有 PVC 时：

1. 不重新初始化数据库数据目录。
2. 不假设初始化脚本会再次执行。
3. 只做最小必要校验，确认该卷仍可作为当前逻辑资源继续使用。

这里的“最小必要校验”除总体方案已有的 PVC 兼容性检查外，对 Mongo 额外要求：

1. Mongo 可以正常启动。
2. 当前约定的 admin 凭据可以成功连接。

若任一条件不满足，直接报错并中止，不做自动修复。

## 鉴权与初始化

### 鉴权模型

本次仅创建一个 admin 用户，作为 helper 访问 Mongo 的统一凭据。

本次不创建 app user，不做更细粒度权限拆分。

### 密码生成方式

admin 密码不作为长期持久资产保存，而是由工具根据逻辑资源 identity 的稳定输入生成固定密码。

工具在部署时：

1. 计算 admin 密码。
2. 将用户名和密码写入本次生成的 Secret。

helper 在运行时：

1. 使用同样的规则推导 admin 密码。
2. 使用推导结果完成连接。

因此，本次 Secret 的职责是承载运行时注入，而不是成为唯一凭据来源。

### Secret 生命周期

本次 `Secret` 归属于运行资源。

删除环境时：

1. 删除 Mongo 运行资源。
2. 删除对应 Secret。
3. 不删除 PVC。

后续再次部署同一逻辑资源时，工具重新按相同规则生成密码并创建 Secret。

### 初始化原则

Mongo 初始化必须满足“可重入、可校验”，但本次不引入复杂的初始化编排框架。

具体规则如下：

#### 新卷场景

当数据卷为空时：

1. 启动 Mongo。
2. 创建 admin 用户。
3. 使用稳定规则生成的密码完成初始化。

#### 旧卷场景

当复用已有数据卷时：

1. 不重复创建 admin 用户。
2. 不依赖初始化脚本再次执行。
3. 仅校验当前 admin 凭据是否仍可用。

这里要求初始化逻辑具备“可重入”特性，主要是为了覆盖以下场景：

1. 首次部署过程中出现重试或容器重启。
2. `deploy apply` 重入。
3. 复用旧卷时初始化入口再次被触发。
4. 后续模板升级后仍需保持行为稳定。

## helper 接入方式

### 接口目标

应用侧通过统一 helper 接入 Mongo，目标接口形态如下：

```go
mongo.NewClient("{app}/{name}")
```

### helper 职责

`mongo` helper 至少负责以下内容：

1. 根据当前运行环境信息确定目标环境。
2. 解析 `{app}/{name}` 中的 app 与逻辑资源名。
3. 定位对应 Mongo 服务。
4. 按约定规则生成 admin 凭据。
5. 组装 Mongo 连接参数并返回 client。

应用侧不直接感知以下内容：

1. Service 名称
2. Secret 名称
3. 密码生成规则
4. 镜像与版本
5. Deployment 还是 StatefulSet

## profile 与静态配置

本次 `mongo` 至少提供一个 profile：`dev-single`。

该 profile 的默认参数放在 `static_config.yaml` 中统一维护，建议至少包含：

1. 镜像地址
2. 镜像版本
3. Mongo 默认端口
4. admin 用户名
5. 存储参数
6. 资源请求与限制
7. 探针参数

本次 profile 的含义是“平台定义的一组 Mongo 单体默认模板”，而不是用户可自由组合的参数集合。

## 升级策略

Mongo 的升级由工具模板负责。

含义是：

1. 若用户配置不变且平台模板不变，则不会发生升级。
2. 若平台更新了内置模板，则是否触发升级由工具实现决定。
3. 升级策略不向用户暴露为配置项。

本次只要求方案为后续升级预留清晰边界，不在本次展开具体版本升级流程。

## 关键锚点

以下内容作为本方案的关键锚点，后续实现不得偏离：

1. 本方案建立在 `deploy` 内置 infra 总体方案之上，不重复定义总体语义。
2. `mongo` 本次只支持单体数据库，不考虑副本集与分片。
3. `mongo` 默认使用单副本 `Deployment`，不使用 StatefulSet。
4. `mongo` 默认通过普通 `ClusterIP Service` 暴露服务。
5. 当 `persistence.enabled=true` 时，数据通过独立 PVC 承载。
6. 删除环境时删除运行资源与 Secret，不删除 PVC。
7. Mongo 默认存储参数统一放在 `static_config.yaml` 中管理，其中 `accessModes=ReadWriteOnce`、`volumeMode=Filesystem`。
8. 本次只创建 admin 用户，不创建 app user。
9. admin 密码由工具根据逻辑资源稳定规则生成；helper 使用相同规则推导密码。
10. Secret 不是唯一凭据来源，而是运行时注入载体。
11. 复用旧卷时不重复初始化，只校验当前 admin 凭据是否可用；失败直接中止。
12. 应用侧通过 `mongo.NewClient("{app}/{name}")` 访问资源，不直接感知底层部署细节。
13. profile 表示平台内置默认模板，不表示用户可直接控制的底层部署参数集合。
14. 升级策略由工具模板负责，不向用户暴露为配置项。

## 未来计划

以下内容不属于本次实现范围，单独记录如下：

1. 支持更多 Mongo profile，例如更高资源规格或不同存储策略。
2. 将 Mongo 相关发现能力沉淀到通用 `pkg/solver`，再由 `pkg/mongo` 基于其封装。
3. 支持更细粒度的用户模型，例如 app user、密码轮转或凭据治理。
4. 评估 Mongo 版本升级、数据兼容性校验与自动迁移策略。
5. 视需要评估副本集能力。
