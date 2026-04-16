# deploy：基于 Kubernetes 运行状态更新 Environment Ready 语义

## 目标

将 `projects/infra/deploy/` 中 `Environment.status.state=READY` 的判定条件，从“资源已成功 apply 到集群”调整为“该环境下所有期望的 Deployment 型 workload 都已在 Kubernetes 中完成 rollout，并被判定为可用”。

预期效果：

1. `deploy apply` 返回 `READY` 时，环境内所有容器都已经正常启动完成，而不是仅仅完成了资源创建/更新。
2. `Environment.status` 更准确地表达环境的**观察状态**，符合 `projects/infra/deploy/README.md` 中“对外提供环境当前观察状态”的职责定义。
3. 保持当前 deploy service 的整体架构不变，不引入额外 observer、调度器或自定义健康检查机制。

## 范围

本方案只收敛到 Kubernetes 原生运行状态，不包含：

1. 自定义业务健康检查。
2. HTTPRoute、Service、PVC、Secret 的额外运行态判定。
3. 新的异步 observer/reconciler 架构。
4. 自动回滚。

## 当前问题

当前状态流转中，`projects/infra/deploy/domain/worker.go` 的 `handleApply()` 在 `runtime.Apply()` 返回 nil 后立即调用 `env.MarkReady()`。

而 `projects/infra/deploy/runtime/k8s/executor.go` 中的 `K8sRuntime.Apply()` 只负责：

1. 将 `Environment` 转换为 Kubernetes 资源。
2. 对 Deployment/Service/HTTPRoute/PVC/Secret 执行 create/update。
3. prune 已不再属于期望状态的资源。

它并不会等待 Deployment rollout 完成，因此当前 `READY` 的真实含义是“apply 成功”，不是“容器已启动完成”。

## 模型设计

### Environment 状态语义

沿用现有状态机，不新增状态：

1. `RECONCILING`：环境正在 reconcile，包括资源 apply 与 rollout 等待阶段。
2. `READY`：环境下所有期望的 Deployment 型 workload 均已 rollout 完成。
3. `FAILED`：资源 apply 失败，或 rollout 进入失败终态，或等待超时。

也就是说，本方案不改变 `EnvironmentState` 模型本身，只改变 `READY` 的判定语义。

### rollout-ready 判定模型

对当前环境推导出的每一个期望 Deployment，满足以下条件才算 ready：

1. Deployment 存在。
2. `status.observedGeneration >= metadata.generation`。
3. `status.updatedReplicas == spec.replicas`。
4. `status.availableReplicas == spec.replicas`。
5. `status.unavailableReplicas == 0`。

该模型表达的是：

* 当前 spec 已被 Deployment controller 观察到。
* 期望副本已经全部更新到最新版本。
* 所有期望副本都已经达到 Kubernetes 的“available”判定。

对 `spec.replicas=0` 的 Deployment，可视为满足 rollout-ready，只要 generation 已被 controller 观察到。

### 失败模型

以下情况判定为失败：

1. `Apply()` 过程中任意资源 create/update/prune 失败。
2. 期望的 Deployment 在 apply 成功后仍然不存在。
3. Deployment 条件表明 rollout 失败，例如 `ProgressDeadlineExceeded`。
4. 等待 rollout 超过约定超时时间。

### 进行中模型

以下情况保持 `RECONCILING`：

1. apply 已成功。
2. 但至少一个 Deployment 仍未满足 rollout-ready 条件。
3. 且当前没有出现明确失败信号。

## 代码分层

### 领域层：`projects/infra/deploy/domain/`

领域层继续只表达“runtime 是否成功让环境达到目标状态”，不直接理解 Kubernetes 细节。

保留现有职责：

* `worker.go`：驱动 reconcile 流程，并把 runtime 的结果映射为 `READY/FAILED`。
* `environment.go`：维护状态转换、时间戳与 message。

本方案下，领域层不新增新的 deploy 状态，也不引入新的调度模型。

### 运行时层：`projects/infra/deploy/runtime/k8s/`

Kubernetes runtime 负责：

1. apply/prune 资源。
2. 基于 Kubernetes Deployment 状态等待 rollout 完成。
3. 识别 rollout 的成功、进行中与失败。

因此，`runtime.Apply()` 的语义从“成功下发资源”提升为“成功下发资源并等待运行态达到 ready”。

这是本方案最核心的分层决策：

* **worker 不理解 Kubernetes rollout 细节**。
* **runtime 对 Kubernetes readiness 负责到底**。

## 关键细节

### 1. 检查对象范围

只检查当前环境下**有 Pod/容器语义**的 Deployment 型 workload：

1. 普通 artifact 生成的 Deployment。
2. MongoDB infra 生成的 Deployment。

不把以下对象纳入 `READY` 门槛：

1. Service。
2. HTTPRoute。
3. Secret。
4. PVC。

原因是本方案目标是“所有容器正常启动”。这些对象要么是静态资源，要么是依附 Deployment 的配套资源，不直接表达容器是否已运行完成。

### 2. 检查时机

`K8sRuntime.Apply()` 在完成所有资源 apply/prune 后，立即开始轮询 Deployment rollout 状态，直到：

1. 全部 ready，返回 nil。
2. 明确失败，返回 error。
3. 超时，返回 error。

### 3. 轮询方式

使用固定轮询间隔查询 Deployment 状态，不通过“再次入队再检查”的方式实现。

原因：

1. 当前 queue 是内存队列，且没有 delay/backoff 机制。
2. 当前 bootstrap 只启动一个 worker。
3. 如果通过反复 re-enqueue 进行 readiness 检查，容易形成热循环。

### 4. context 取消语义

如果等待过程中收到 `context.Canceled` 或服务关闭，不应把环境标记为 `FAILED`。

应保持环境处于 `RECONCILING`，由 `projects/infra/deploy/domain/recovery.go` 在服务恢复后继续 requeue。

### 5. 状态消息

失败时，`status.message` 记录 Kubernetes rollout 的失败原因。

可选地，在保持 `RECONCILING` 时更新 message，表达当前阻塞点，例如：

* 某 Deployment 尚未达到 available replicas。
* 某 Deployment 尚未 observed 最新 generation。

若不扩展中间进度消息，本方案也成立；但保留该能力更利于排障。

## 决策详情

### 决策一：不新增 observer

选择：**不新增 observer / scheduler / persisted watch loop**。

原因：

1. 当前问题本质上是 `READY` 语义过早，而不是架构缺少新的角色。
2. 现有 `worker + runtime + recovery` 模型已经能承载“等待直到 ready/failed”这一流程。
3. 引入 observer 会额外引入新的状态机、重试与调度语义，超出本次目标。

### 决策二：不新增新的 EnvironmentState

选择：**沿用 `RECONCILING/READY/FAILED`**。

原因：

1. 当前状态模型已足以表达 apply 中、等待中、完成、失败。
2. rollout 等待本质上仍属于 reconcile 的一部分。

### 决策三：将 readiness 判断放在 runtime，而不是 worker

选择：**Kubernetes runtime 负责 readiness 判断**。

原因：

1. rollout 判定依赖 Deployment 的 Kubernetes 语义。
2. worker 不应承担 Kubernetes 特定知识。
3. 这样可以保持领域层稳定，减少跨层泄漏。

### 决策四：READY 只看 Deployment rollout，不看 Endpoint/Route

选择：**本阶段仅基于 Deployment rollout 判定 READY**。

原因：

1. 用户目标是“所有容器都正常启动”。
2. Deployment 的 rollout 状态已经能覆盖“容器是否已被 K8s 视为可用”。
3. 如果再把 EndpointSlice、Gateway/HTTPRoute attachment 等纳入门槛，会扩大范围，偏离本次目标。

### 决策五：不引入自定义健康检查

选择：**不修改 proto，不增加 artifact 自定义 readiness 配置**。

原因：

1. 当前阶段只需要收敛到 Kubernetes 自身运行状态。
2. 额外的 probe 配置会把方案从“修正 READY 语义”扩大为“引入新的健康检查能力”。

## 预计改动范围

核心改动文件：

1. `projects/infra/deploy/runtime/k8s/executor.go`
   * 在 apply/prune 完成后新增 rollout wait 逻辑。
2. `projects/infra/deploy/runtime/k8s/client.go`
   * 复用现有 typed client 查询 Deployment。
3. `projects/infra/deploy/domain/worker.go`
   * 逻辑大体不变，但需要明确处理 runtime 因取消退出时不转 `FAILED`。
4. `projects/infra/deploy/runtime/k8s/executor_test.go`
   * 增加 rollout 成功、进行中、失败、超时等测试。
5. `projects/infra/deploy/domain/worker_test.go`
   * 更新 `READY` 语义相关测试。

## 验收标准

满足以下条件时，认为方案落地成功：

1. `deploy apply` 在 Deployment 仅被创建、但尚未 rollout 完成时，不会返回 `READY`。
2. 环境内所有期望 Deployment 均 rollout 完成后，`Environment.status.state` 才变为 `READY`。
3. rollout 明确失败或等待超时后，环境进入 `FAILED`，且 `status.message` 可用于定位问题。
4. deploy service 重启或 context 取消时，进行中的环境不会被误标记为 `FAILED`，而是能被 `Recover()` 恢复继续处理。

## 风险与约束

1. 当前 deploy service 只有单 worker，等待 rollout 会降低整体吞吐。
2. 若单次 rollout 经常持续很久，会导致后续环境 reconcile 排队等待。
3. 本方案优先修正语义正确性，而不是优化并发处理能力。

## 未来规划

若后续出现以下情况，再考虑演进：

1. rollout 等待时间较长，单 worker 吞吐成为明显瓶颈。
2. 需要把“容器可用”进一步提升到“业务接口健康”。
3. 需要把中间进度结构化输出，而不是仅依赖 `status.message`。

到那时，可再评估：

1. 持久化 observer/requeue-with-backoff 机制。
2. 自定义 readiness/health 配置。
3. 更细粒度的环境观察状态模型。
