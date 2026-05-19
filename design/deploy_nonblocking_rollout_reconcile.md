# Deploy service 非阻塞 rollout 状态推进方案

## 目标

本方案用于调整 `projects/infra/deploy/` 中 deploy worker 的状态推进与 Kubernetes rollout 等待逻辑，目标是：

* 将当前 `Apply` 内部阻塞等待 rollout 的行为拆成独立的持久化状态，让 worker 不再长时间占用一次调度。
* 让 worker 每次只推进一个状态步骤，状态变更后通过队列重新调度，支持多步连续推进。
* 让 apply/delete 的异常继续通过 retry/backoff 收敛，而 `Failed` 仅表示 Kubernetes 明确报告 rollout 失败。
* 引入 service 层承载过程逻辑，使 handler、domain、worker 和 runtime 的职责边界更清晰。
* 通过带前置条件的状态更新避免旧 generation 或旧状态结果覆盖新目标。

## 范围

本方案覆盖：

* `projects/infra/deploy/domain` 状态模型、队列模型、worker 调度模型。
* `projects/infra/deploy/runtime/k8s` apply 与 rollout 查询拆分。
* `projects/infra/deploy/storage` 恢复查询与带状态前置条件的持久化更新。
* `projects/infra/deploy/handler.go` CRUD 过程逻辑向 service 层迁移。
* `projects/infra/deploy/deploy.proto` 状态枚举追加。
* 相关单元测试调整。

本方案不覆盖：

* 持久化队列实现。队列仍可为内存队列，进程重启后依赖 repository recovery 恢复。
* `GetServiceEndpoints` fallback 逻辑重构。该逻辑可在后续 service 层整理中迁移。
* 完整 ETag / FieldMask 乐观锁设计。

## 当前问题

当前实现中，`runtime/k8s` 的 `Apply` 在资源 apply/prune 后会调用 `waitForRollout`，该函数在一次 worker 处理内循环查询 Deployment / StatefulSet 直到 ready、失败或超时。这带来以下问题：

* worker 在等待 rollout 期间无法处理其他环境。
* rollout 等待不是持久化状态，进程重启后只能依赖粗粒度恢复重新进入当前流程。
* `domain.Worker.processPresent` 一次处理内会完成 `Pending -> Reconciling -> Apply -> Ready/Failed` 多个步骤，不利于表达“已提交资源，等待 Kubernetes 推进”。
* 当前 `Failed` 会被 `ListNeedingReconcile` 恢复重试，不符合“Failed 是 Kubernetes 明确失败终态”的语义。
* 当前 Mongo `Save` 对 stale write 静默返回 nil，worker 可能误认为旧状态推进已持久化成功。
* 当前 queue 在持锁状态下向 buffered channel 发送，channel 满时存在生产者持锁阻塞、消费者等待同一把锁的死锁风险。

## 最终模型

### 状态模型

在现有状态上 append-only 增加：

```go
StateWaitingRollout EnvironmentState = 6
```

`deploy.proto` 同步追加：

```proto
ENVIRONMENT_STATE_WAITING_ROLLOUT = 6;
```

禁止重排已有枚举值，因为状态已作为 int 持久化，并已通过 proto 对外暴露。

### 状态语义

* `Pending`：目标已接受，尚未开始处理当前 generation。
* `Reconciling`：正在提交 Kubernetes 资源，即 apply/prune 阶段。
* `WaitingRollout`：资源已提交，等待 Kubernetes controller 将 Deployment / StatefulSet 推进到 ready。
* `Ready`：当前 generation 已完成 rollout。
* `Failed`：当前 generation 的 Kubernetes rollout 已明确失败，是终态，不自动重试。
* `Deleting`：正在删除 runtime 资源。

### 状态流

存在目标：

```text
Pending / Ready(stale) / Failed(new generation)
  -> Reconciling
  -> WaitingRollout
  -> Ready
```

apply 异常：

```text
Reconciling apply 失败
  -> 状态不变或仅更新错误 message
  -> 返回 retry error
  -> worker 延迟重试
```

rollout 未就绪：

```text
WaitingRollout
  -> 状态不变
  -> message 变化时持久化
  -> 延迟 poll
```

rollout 明确失败：

```text
WaitingRollout
  -> Failed
```

删除：

```text
Pending / Reconciling / WaitingRollout / Ready / Failed
  -> Deleting
  -> repo.Delete
```

## Runtime 设计

将当前 `EnvironmentRuntime.Apply` 拆分为：

```go
ApplyResources(ctx context.Context, env *Environment) error
CheckRollout(ctx context.Context, env *Environment) (*RolloutStatus, error)
```

`ApplyResources` 只负责：

* 将 `Environment` 转换为 Kubernetes workloads。
* apply Deployment / StatefulSet / Service / HTTPRoute / PVC / Secret 等资源。
* prune 不再期望存在的资源。
* 不等待 rollout。

`CheckRollout` 只做一次 Kubernetes 状态查询，返回三态结果：

```text
Ready
Waiting(message)
Failed(message)
```

失败判定原则：

* Deployment 可使用 `ProgressDeadlineExceeded`、`ReplicaFailure=True` 等明确失败条件。
* observedGeneration 滞后、副本未更新、available 不足、仍有 unavailable replicas 都属于 `Waiting`。
* StatefulSet 当前若没有可靠失败信号，则只能返回 `Ready` 或 `Waiting`，不能为了超时而标记 `Failed`。

## Service 层设计

新增 service 层，承载跨 repo / runtime / queue 的过程逻辑。

### ReconcileService

负责单个环境的一步推进：

```go
ProcessOne(ctx context.Context, envName EnvironmentName) (ProcessResult, error)
```

`error` 是错误或不符合预期的唯一表达；`ProcessResult` 只表达成功处理后的调度意图，不与 error 语义重合。

建议结构：

```go
type ProcessResult struct {
    Changed      bool
    Terminal     bool
    RequeueAfter time.Duration
    Source       WorkItemSource
}
```

约定：

* `error != nil`：worker 按错误类型处理 retry/fatal。
* `error == nil`：worker 只根据 `ProcessResult` 决定是否重新入队。
* 每次 `ProcessOne` 最多推进一次持久化状态。

### EnvironmentCommandService

负责 Create / Update / Delete 的过程逻辑：

* proto 已转换后的领域输入校验。
* reserved env vars 查询与冲突校验。
* 创建或更新 `Environment`。
* 保存后入队。

handler 保留：

* proto 请求解析。
* gRPC status error 转换。
* proto/domain 转换。

## 持久化设计

### 状态更新必须带前置条件

状态推进不应继续依赖通用 `Save` 静默覆盖。新增 repository 方法或 service 内部使用 compare-and-update 语义：

```text
TransitionStatus(
  envName,
  expectedGeneration,
  expectedDesired,
  fromState,
  toStatus,
)
```

必须校验：

* generation 未变。
* desired 未变。
* 当前 state 等于 fromState。

如果不满足，返回明确错误，例如：

```go
ErrStaleState
ErrStaleGeneration
```

service 收到 stale 错误后重新读取最新环境并重新决策，不能当成功。

### 恢复查询

`ListNeedingReconcile` 调整为：

* `DesiredAbsent`：全部恢复。
* `DesiredPresent` 且 state 为 `Pending / Reconciling / WaitingRollout`：恢复。
* `DesiredPresent` 且 `observed_generation < generation`：恢复。
* `DesiredPresent` 且 state 为 `Failed` 且 `observed_generation == generation`：不恢复。

`Failed` 仅在新 generation 到来后通过 `SetDesiredPresent` 回到 `Pending`。

### message 与时间字段

* `WaitingRollout` 的 message 仅在内容变化时保存，避免每次 poll 都写 Mongo。
* `LastReconcileTime` 表示“一轮 reconcile 开始时间”，进入 `Reconciling` 时更新，poll 不更新。
* `LastSuccessTime` 仅在进入 `Ready` 时更新。

## Queue 设计

### 统一延迟入队

在 `Queue` 增加统一延迟入队能力，retry backoff 与 rollout poll 都使用同一个入口，例如：

```go
EnqueueAfter(ctx context.Context, item *WorkItem, delay time.Duration) error
```

worker 不再各自启动 goroutine 实现 retry/poll。

### WorkItem 来源

`WorkItem` 保留 source，用于优先级与 retry count 语义：

* `user`：用户 create/update/delete 触发，优先级最高，重置 retry count。
* `retry`：apply/delete 失败后的 counted retry，增加 retry count。
* `poll`：rollout waiting 的普通延迟检查，不增加 retry count。

去重规则：

* user 覆盖 retry / poll。
* retry 覆盖 poll。
* poll 不覆盖 user / retry。
* in-flight 环境的后续 work item 存入 follow-up，当前处理完成后再入队。

### 修复 channel 死锁风险

当前 queue 不应在持有 mutex 时向 `pendingCh` 发送。调整方向二选一：

* channel send 移到锁外。
* 改为内部 slice + condition/unbounded queue 模型。

同时 `Enqueue` / `EnqueueAfter` 应尊重 `ctx`，避免 shutdown 时阻塞。

## Retry 设计

apply/delete 的异常仍通过 error 表达。

* apply 失败不进入 `Failed`。
* delete 失败不进入 `Failed`。
* retry 使用 capped backoff。
* 不因达到最大 retry count 而最终 drop work item。
* `RetryCount` 可继续用于计算 delay，delay 到 `maxRetryDelay` 后封顶。

这样避免环境永久停在 `Reconciling` 或 `Deleting` 且无后续调度。

## Worker 设计

worker 职责收敛为：

* 从 queue 取 work item。
* 调用 `ReconcileService.ProcessOne`。
* 根据 error 分类执行 retry/fatal/ignore。
* 根据 `ProcessResult` 执行立即或延迟入队。
* 完成 queue item。

worker 不再直接操作 repository 或 runtime，不再承载状态机分支。

## 关键场景

### 新建环境

```text
CreateEnvironment
  -> DesiredPresent + Pending + generation=1
  -> user enqueue
  -> ProcessOne: Pending -> Reconciling
  -> requeue
  -> ProcessOne: ApplyResources, Reconciling -> WaitingRollout
  -> poll after 5s
  -> ProcessOne: CheckRollout waiting, state unchanged
  -> poll after 5s
  -> ProcessOne: CheckRollout ready, WaitingRollout -> Ready
```

### 等待 rollout 时更新

```text
WaitingRollout(generation=N)
  -> UpdateEnvironment
  -> generation=N+1, state=Pending
  -> user enqueue
```

旧 poll work item 执行时必须通过 expected generation/state 校验避免覆盖新状态。

### 等待 rollout 时删除

```text
WaitingRollout
  -> DeleteEnvironment
  -> DesiredAbsent, state=Pending 或直接 state=Deleting
  -> user enqueue
  -> ProcessOne: active state -> Deleting
  -> ProcessOne: runtime.Delete -> repo.Delete
```

建议 Delete 接口允许从 `WaitingRollout` 进入删除流程。

### rollout 明确失败

```text
WaitingRollout
  -> CheckRollout returns Failed(message)
  -> TransitionStatus WaitingRollout -> Failed
  -> terminal, no requeue
```

## 测试计划

### domain

* `StateWaitingRollout` 字符串与合法迁移。
* `Failed` 为终态，同 generation 不恢复。
* `WaitingRollout` 可进入 `Deleting`。
* `ProcessOne` 每次最多推进一个状态。
* apply 失败返回 retry error，不标记 `Failed`。
* rollout waiting 不改变状态，返回 poll result。
* rollout failed 标记 `Failed`。

### queue

* `EnqueueAfter` 延迟入队。
* user / retry / poll 优先级与覆盖规则。
* in-flight follow-up 行为。
* channel 满或 shutdown 时不死锁。
* retry count 只在 retry source 中增加，poll 不增加。

### storage

* `ListNeedingReconcile` 新规则。
* `TransitionStatus` expected generation / desired / fromState 校验。
* stale generation / stale state 返回明确错误。
* message 未变化时不重复写入。

### runtime/k8s

* `ApplyResources` 不调用 rollout wait。
* `CheckRollout` ready / waiting / failed 三态。
* Deployment 明确失败条件。
* StatefulSet 未 ready 时保持 waiting。

### handler/service

* Create/Update/Delete 调用 command service。
* Update 允许从 `WaitingRollout` 回到 `Pending`。
* Delete 允许从 `WaitingRollout` 进入删除流程。
* proto 状态映射包含 `WAITING_ROLLOUT`。

## 决策记录

* 接受新增 `StateWaitingRollout`。
* 接受 apply 与 rollout 查询拆分。
* 接受 `Failed` 为终态，且只表示 Kubernetes 明确 rollout 失败。
* 接受 apply/delete 失败无限 retry with capped backoff，不进入 `Failed`，不最终 drop。
* 接受 queue 增加统一延迟入队，retry 与 rollout poll 共用。
* 接受 `ProcessResult`，但其只表达成功后的调度意图，错误仍只通过 `error` 表达。
* 接受状态更新增加 generation / desired / fromState 前置条件。
* 暂缓 `GetServiceEndpoints` service 化。
* 暂缓完整 ETag / FieldMask 乐观锁。
* 暂缓持久化队列。

