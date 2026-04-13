# Deploy Service 异步 K8s 重构方案

## 目标

重构 `projects/infra/deploy`，使 `deploy service` 从“仅维护环境权威状态”的最小实现，演进为一个**异步推进 Kubernetes 期望状态**的控制面服务。

本方案的核心目标是：

1. `CreateEnvironment` / `UpdateEnvironment` 成功返回，只表示**期望状态已被接受并持久化**。
2. 真实的 Kubernetes apply / delete 行为由**异步执行器**推进，不在请求链路中同步执行。
3. `DeleteEnvironment` 改为**异步删除**：先删除环境拥有的运行资源，再删除环境记录。
4. 接口测试使用**独立可部署的 test-only binary/image**，通过 fake runtime 屏蔽真实 k8s 交互。

## 重要前提

当前代码中的 `projects/infra/deploy/reconciler.go` 仅是临时占位实现，**不作为本次设计参考**。

本次重构应当按本方案重新设计异步执行模型、状态推进语义、删除语义与测试边界，而不是在现有 reconciler 逻辑上做增量修补。

## 本次实现范围

### 目标边界

本次只覆盖以下内容：

1. 为 `deploy service` 设计并落地新的异步执行模型。
2. 将环境状态推进改为：请求链路只持久化 desired state，异步逻辑推进 observed state。
3. 引入 service 自己的 k8s 运行时边界，不引用 `tools/deploy` 中的代码。
4. 允许复制 `tools/deploy/pkg/k8s` 中的模型、命名规则、标签规则与必要函数实现，并在 `projects/infra/deploy` 内独立维护。
5. 删除环境时，按确定顺序异步删除资源；删除成功后再删除环境数据。
6. 服务启动时扫描处于处理中状态的环境，并重新入队。
7. 新增接口测试专用的 `main`、target、image 与部署配置，且正式代码不包含测试实现。

### 非目标

本次不解决以下问题：

1. 自动重试策略、退避策略、卡住环境恢复机制。
2. ownerReferences / finalizers 驱动的删除模型。
3. 与 `tools/deploy` 运行语义的自动一致性或代码复用。
4. 生产级复杂调度能力（优先级队列、多副本协同抢占、分布式锁等）。
5. 现有临时 reconciler 的兼容保留。

## 已确认决策

### 1. 请求成功仅表示“已接受并持久化”

`CreateEnvironment` / `UpdateEnvironment` 的成功返回不代表环境已部署完成，只代表：

1. 输入已通过校验。
2. desired state 已写入 repository。
3. 环境已进入异步处理生命周期。

环境是否部署成功，由后续异步执行完成后写回 `Environment.status`。

### 2. create / update 成功后统一写入 `reconciling`

为避免“落库成功但未入队时进程崩溃”导致环境永久停在 `pending`，本方案固定：

1. 新建环境成功后，直接持久化为 `reconciling`。
2. 更新 desired state 成功后，直接持久化为 `reconciling`。
3. 启动恢复扫描只需扫描 `reconciling` 与 `deleting`。

因此，本次实现中 `pending` 不作为长期停留态使用。

### 3. 异步执行器的最小语义

异步执行器固定采用最小模型：

1. 队列元素只存 `envName`。
2. 不在队列中区分 reconcile/delete 任务类型。
3. worker 每次处理时重新从 repository 读取最新环境快照。
4. 同一个 environment 同时只允许一个执行流。
5. delete 优先级高于普通 reconcile。

worker 的判定逻辑为：

1. 读不到环境记录：视为幂等完成。
2. 环境状态为 `deleting`：进入删除流程。
3. 环境状态为 `reconciling`：进入 apply/reconcile 流程。

### 4. 启动恢复扫描

服务启动后，必须扫描 repository 中处于处理中状态的环境，并重新入队。

扫描范围固定为：

1. `reconciling`
2. `deleting`

这样可以覆盖：

1. 服务在处理中崩溃后的恢复。
2. 删除流程尚未完成时的恢复。

### 5. deleting 为粘性状态

删除环境时：

1. handler 不直接删除 environment 数据。
2. handler 只负责把环境标记为 `deleting`，并触发异步执行。
3. 删除资源成功后，异步执行器再删除 environment 数据。

如果删除失败：

1. 环境保持 `deleting`。
2. 不自动转为 `failed`。
3. 错误信息写入 `status.message`。

本方案接受“删除失败后环境先卡住，后续再处理”的策略。

### 6. 删除机制固定采用 label-based manual delete

本方案不采用 ownerReferences / finalizers 作为第一阶段删除主机制。

第一阶段固定采用：

1. 通过稳定 labels 识别当前 environment 拥有的资源。
2. 按固定顺序逐类手工删除资源。
3. 全部删除成功后再删除 environment 数据。

这样做的原因：

1. 更符合“服务自己掌控删除生命周期”的目标。
2. 与本次已确定的删除顺序与 sticky deleting 语义一致。
3. 不引入 owner GC、propagation policy、blockOwnerDeletion 等额外复杂度。

### 7. 命名与标签规范一起复制

虽然 `projects/infra/deploy` 不引用 `tools/deploy` 代码，但本次允许直接复制其已验证的命名/标签规范与相关函数实现。

固定要求：

1. 资源命名规则与标签规则一起复制，不拆开另定。
2. ownership 判断依赖稳定 labels。
3. 删除逻辑围绕 ownership labels 构建，而不是只靠资源名猜测。

### 8. 删除顺序固定

删除环境时，资源删除顺序固定为：

1. `HTTPRoute`
2. 业务 `Service`
3. 业务 `Deployment`
4. infra 主资源
5. infra 附属资源
6. environment 数据删除

本顺序以当前目标为准，后续若引入新的 infra 类型，再在对应资源方案中扩展。

### 9. 并发语义固定

为避免 update / reconcile / delete 相互覆盖，本方案固定：

1. `deleting` 状态下禁止新的 desired state 更新。
2. 同一 environment 的 worker 串行执行。
3. worker 每轮执行前都读取最新状态，不使用旧快照直接落库。
4. 如果 worker 发现环境已进入 `deleting`，普通 reconcile 直接退出。

本次不引入复杂 CAS/重试体系，但实现时不得违反以上并发语义。

### 10. 接口测试边界固定

接口测试使用 fake `EnvironmentRuntime`，不直接使用真实 k8s client。

测试分层固定为：

1. **接口测试**：使用 fake runtime，验证 HTTP/gRPC 行为与生命周期语义。
2. **adapter 单测**：验证 domain 到 k8s 对象映射、apply/delete 顺序和资源生成逻辑。

### 11. `main_iface` 是独立可部署的 test-only 工件

接口测试专用入口固定为独立工件：

1. 独立 `go_binary`
2. 独立 `oci_image`
3. 独立 `service.yaml` 的 artifacts 以及独立的 `deploy.yaml`
4. 仅供接口测试部署使用，不进入正式发布链路

## 方案结构

### 包边界

`projects/infra/deploy` 应引入 service 自己的运行时边界，例如：

```text
projects/infra/deploy/
  runtime/
    interface.go
    fake.go            # test-only target 使用
    k8s/
      model.go
      naming.go
      labels.go
      builder.go
      executor.go
```

边界要求：

1. service 只依赖自己的 runtime interface。
2. k8s adapter 是 interface 的一个具体实现。
3. 不直接 import `tools/deploy/...`。
4. 如需复用已有逻辑，采用复制实现的方式落到 service 自己的目录内。

### 启动与 wiring 结构

建议将当前 `main.go` 中的启动逻辑抽到共享 bootstrap library，再提供两个 thin main：

```text
projects/infra/deploy/
  app/
    bootstrap.go
    server.go
    worker.go
    recovery.go
  main.go          # prod wiring
  main_iface.go    # test-only wiring
```

要求：

1. `main.go` 使用真实 runtime。
2. `main_iface.go` 使用 fake runtime。
3. 正式 bootstrap 不依赖 fake 实现。
4. fake 实现只进入 test-only target。

### BUILD / image 结构

建议形成：

1. 共享 `go_library`：承载 bootstrap / handler / worker / runtime interface
2. `go_binary`：生产 main
3. `go_binary`：interface-test main
4. 两套独立 `oci_image`
5. test-only deploy 配置引用 interface-test image

## 生命周期语义

### Create / Update

1. handler 验证输入。
2. 写入新的 desired state。
3. 环境状态持久化为 `reconciling`。
4. 将 `envName` 入队。
5. 返回成功响应。

异步 worker 负责：

1. 读取最新环境快照。
2. 将 desired state 映射为 k8s 资源。
3. 执行 apply。
4. 成功则写 `ready`。
5. 失败则写 `failed`，并记录 message。

### Delete

1. handler 验证输入。
2. 将环境持久化为 `deleting`。
3. 将 `envName` 入队。
4. 返回成功响应。

异步 worker 负责：

1. 读取最新环境快照。
2. 根据 ownership labels 查询资源。
3. 按固定顺序删除资源。
4. 如果删除全部成功，再删除 environment 数据。
5. 删除失败则保持 `deleting`，并写入 message。

## 测试策略

### 接口测试

接口测试关注生命周期契约，而不是“请求返回即完成部署”。

固定断言原则：

1. Create / Update 成功返回只表示 desired state 已记录。
2. 环境随后进入 `reconciling`。
3. 异步完成后进入 `ready` 或 `failed`。
4. Delete 成功返回后环境进入 `deleting`。
5. 删除完成后环境记录消失。
6. 删除失败时环境继续停留在 `deleting`，并暴露失败信息。

### Adapter 单测

adapter 单测负责验证：

1. domain -> k8s 资源映射是否正确。
2. 命名与 labels 是否符合复制后的规范。
3. 删除顺序是否正确。
4. 删除过程是否只影响当前 environment 拥有的资源。

## 当前无阻塞项

在以下决策已确认后，当前方案没有新的架构级阻塞项：

1. create / update 都直接写 `reconciling`
2. queue 只存 `envName`
3. deleting 为粘性状态
4. 删除机制采用 label-based manual delete
5. 命名/标签规范一起复制
6. 启动恢复扫描 `reconciling/deleting`
7. `main_iface` 为独立 test-only 可部署工件

因此，本方案可以直接进入实现设计。

## 关键细节锚点

以下内容为本方案不可偏离的关键锚点：

1. 当前代码中的 `reconciler.go` 仅为临时实现，本次重构不得以其生命周期逻辑作为参考基线。
2. 请求成功仅表示 desired state 已被接受并持久化，不表示环境已部署完成。
3. create / update 成功后统一写 `reconciling`，不保留长期 `pending`。
4. 队列元素只存 `envName`，worker 每轮读取最新快照。
5. 同一 environment 串行处理，delete 优先于普通 reconcile。
6. 删除失败保持 `deleting`，不自动转 `failed`。
7. 第一阶段删除机制固定为 label-based manual delete。
8. 命名、labels、ownership 识别规则一起复制到 service 自己的 k8s 代码中。
9. 删除顺序固定为：`HTTPRoute -> Service -> Deployment -> infra 主资源 -> infra 附属资源 -> environment 数据`。
10. 服务启动必须扫描 `reconciling/deleting` 并重新入队。
11. 接口测试使用 fake `EnvironmentRuntime`，正式代码不包含测试实现。
12. `main_iface` 是独立可部署的 test-only binary/image/manifest。

## 未来计划

以下内容不属于本次方案范围，统一放到后续阶段处理：

1. 自动重试与退避策略。
2. 卡住环境的人工恢复 / 管理接口。
3. ETag / compare-and-set 级别的并发控制。
4. ownerReferences / finalizers 方案评估。
5. 多优先级队列、事件去重优化、分布式执行器。
6. 与 `tools/deploy` 更高层语义统一或协议统一。
