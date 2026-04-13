# deploy service

本目录用于承载 deploy 控制面的协议与实现。当前模型以 `Environment` 作为唯一对外资源，通过 gRPC 暴露环境的增删改查接口；环境内部的 Kubernetes 资源不会直接对外暴露。

## 目标

`deploy service` 的职责是：

1. 维护部署环境的**权威状态**。
2. 接收客户端提交的环境**期望状态**。
3. 异步将期望状态 reconcile 到 Kubernetes 集群。
4. 对外提供环境当前的**观察状态**与错误信息。

本地 CLI 是薄客户端，只负责：

- 参数解析与用户交互。
- 组织 gRPC 请求并调用远端服务。
- 展示环境状态、错误信息和执行结果。

## 资源模型

当前对外只暴露一个资源：`Environment`。

- 资源类型：`infra.liukexin.com/deploy/Environment`
- 资源路径：`deploy/{scope}/environments/{env_name}`

这里的资源名已经完整表达原来的完整环境名：

- 原来的完整环境名：`{scope}.{env_name}`
- 现在的资源名：`deploy/{scope}/environments/{env_name}`

也就是说，协议层不再单独保存 `full_env_name`，资源 `name` 本身就是环境的唯一标识。

## 状态模型

`Environment` 分成两部分：

1. `desired_state`
   - 客户端提交的目标部署内容。
   - 表示“这个环境最终应该长成什么样”。

2. `status`
   - 服务端异步 reconcile 后写回的观察状态。
   - 表示“系统当前已经做到哪一步、是否成功、失败原因是什么”。

这是一个**异步部署模型**：

- `CreateEnvironment` / `UpdateEnvironment` 只负责写入新的期望状态。
- 服务端在后台执行实际的部署与更新。
- 调用方通过 `GetEnvironment` / `ListEnvironments` 观察进度和结果。

当前 `status.state` 包含以下状态：

- `PENDING`
- `RECONCILING`
- `READY`
- `FAILED`
- `DELETING`

## desired_state 结构

`Environment.desired_state` 当前承载三类部署内容：

### 1. `services`

用于描述需要部署的应用服务，字段包括：

- `name`
- `app`
- `image`
- `ports`
- `replicas`
- `tls_enabled`

注意这里的输入已经是**可直接部署的镜像引用**，而不是 Bazel target、构建定义或其他本地构建语义。

### 2. `infras`

用于描述环境内需要的基础设施资源，当前字段包括：

- `resource`
- `profile`
- `name`
- `app`
- `persistence_enabled`

### 3. `http_routes`

用于描述 HTTP 路由规则，当前字段包括：

- `hostnames`
- `matches`
- `path`

## 服务边界

### 服务端负责

- `Environment` 资源的权威存储。
- `desired_state` 的校验与归一化。
- 根据 `desired_state` 构建 Kubernetes 所需对象。
- 与 Kubernetes API 交互并执行异步 reconcile。
- 将部署过程中的结果、错误和时间点回写到 `status`。

### CLI 负责

- 把用户输入转换成 `Environment` 资源写请求。
- 提交 `desired_state`。
- 轮询或查询环境状态。
- 以用户可读方式展示结果。

### 不在协议中的内容

以下内容当前**不应进入** deploy service 的 proto：

- Bazel target。
- `deploy.yaml` / `service.yaml` 原始内容。
- 环境内部 Deployment / Service / HTTPRoute / Secret 等 Kubernetes 资源的对外资源化表示。
- 单独的 `Apply` RPC。

原因是当前协议要表达的是“控制面上的环境期望状态”，而不是“本地构建过程”或“集群内部对象明细”。

## RPC 设计

当前服务接口为标准资源式 CRUD：

- `GetEnvironment`
- `ListEnvironments`
- `CreateEnvironment`
- `UpdateEnvironment`
- `DeleteEnvironment`

其中：

- `CreateEnvironment`：创建环境并写入初始 `desired_state`
- `UpdateEnvironment`：更新已有环境的 `desired_state`
- `DeleteEnvironment`：删除环境，并触发服务端清理该环境关联的部署资源

`Create` 和 `Update` 自身不要求同步完成部署；它们的成功仅表示“期望状态已被接受并记录”。

## 设计原则

1. **资源单一**
   - 当前只暴露 `Environment`，不把环境内部资源拆成独立外部资源。

2. **期望状态驱动**
   - 客户端提交目标状态，服务端负责收敛。

3. **异步执行**
   - 部署过程和资源写操作解耦，避免接口被集群执行时长绑定。

4. **输入去构建化**
   - 协议层不关心 Bazel、构建脚本和产物解析，只接收可部署的 image 和相关部署规格。

5. **服务端权威**
   - 环境状态、部署进度和失败原因以服务端为准。

## 后续演进方向

后续如果需要扩展，可以继续沿着当前模型演进，例如：

- 为 `EnvironmentStatus` 增加更细粒度的 condition / reason。
- 增加乐观锁语义，例如更严格地使用 `etag`。
- 增加回滚、暂停、恢复等面向环境的控制能力。

但在当前阶段，不引入额外子资源，优先保持 `Environment` 模型简单稳定。
