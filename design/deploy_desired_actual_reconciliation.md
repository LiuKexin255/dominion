# Deploy service 环境期望/实际状态分离方案

## 目标

本方案用于调整 `projects/infra/deploy/` 中 `Environment` 的生命周期建模方式，目标是：

* 让 deploy service 能同时表达环境的**期望生命周期目标**与**当前实际阶段**，避免单一 `status.state` 在请求入口、worker 和恢复流程中被反复覆盖。
* 让 handler 只负责接受并持久化目标，worker 负责将环境从当前实际状态推进到期望状态，形成稳定的一致性收敛模型。
* 让运行中被更新的环境在当前流程结束后仍能继续执行最新目标，而不是被旧 worker 结果覆盖或遗漏。
* 让 recovery、repository 查询和 worker 调度都围绕“是否仍需收敛”这一统一语义工作，而不是依赖一组分散的中间状态约定。
* 让运行中可重试错误由 worker 统一执行有限次退避重试，不阻塞主循环，也不把重试细节污染到环境领域模型中。

## 范围

本方案仅覆盖 `deploy service` 内部模型与实现：

* `projects/infra/deploy/domain` 生命周期模型调整
* `projects/infra/deploy/handler.go` 请求接受语义调整
* `projects/infra/deploy/domain/worker.go` 收敛逻辑与退避重试调整
* `projects/infra/deploy/domain/queue.go` 单队列与工作项模型调整
* `projects/infra/deploy/domain/recovery.go` 恢复逻辑调整
* `projects/infra/deploy/domain/repository.go` 与 `projects/infra/deploy/storage/mongo.go` 持久化与查询接口调整
* 相关测试调整

本方案不包括：

* `deploy.proto` 对外协议调整
* CLI 行为变更
* 历史数据兼容与迁移策略

## 当前问题

当前实现中，`Environment` 已经有一套 deploy 内容上的 `desired_state`，但生命周期上只有一个 `status.state`，它同时承担了两类职责：

* **目标职责**：Create / Update / Delete 入口通过 `MarkReconciling()`、`MarkDeleting()` 直接把环境推到目标流程态。
* **实际职责**：worker 通过 `MarkReady()`、`MarkFailed()`、`MarkDeleting()` 报告实际处理结果。

这带来以下问题：

* handler 会提前写入“实际状态”，使 `status.state` 既是命令，又是观察结果。
* worker 当前只按 `status.state` 决定 apply / delete 路径，无法表达“目标变了但实际还未推进”。
* recovery 当前只查 `StateReconciling` / `StateDeleting`，无法统一表达“仍需继续收敛”的对象。
* 运行中若用户再次更新，旧 worker 可能在处理结束时写回旧结果，覆盖用户后来的目标。
* 当前 queue 只存 `EnvironmentName`，没有办法统一表达运行中 retry、目标变化重入队和重复任务合并语义。
* 当前 `Worker.Run()` 只要收到 `process()` 的 error 就退出，而上层启动逻辑会 panic，无法承载进程内退避重试。

因此，本次方案的核心不是再增加一个新流程态，而是将**期望**与**实际**拆开，并引入版本对齐语义、工作项队列语义和 worker 统一重试语义。

## 最终模型

## 生命周期模型

`Environment` 保持现有 deploy 内容上的 `desiredState`，新增生命周期目标字段放入 `status`：

* `desiredState`：环境的 deploy 内容目标（artifacts / infras），保持现状。
* `status.desired`：环境生命周期目标。
* `status.state`：环境当前实际阶段。
* `generation`：当前目标版本号。
* `status.observedGeneration`：worker 已处理完成的目标版本号。

### `status.desired`

新增内部枚举 `EnvironmentDesired`：

* `DesiredUnspecified = 0`
* `DesiredPresent = 1`
* `DesiredAbsent = 2`

本次仅使用两态：

* `DesiredPresent`：目标是环境存在并收敛到最新 deploy 内容。
* `DesiredAbsent`：目标是环境被删除。

保留 `DesiredUnspecified` 仅用于防御性检查，不作为正常运行态。

### `status.state`

保留现有观察态枚举：

* `StatePending`
* `StateReconciling`
* `StateReady`
* `StateFailed`
* `StateDeleting`

语义调整如下：

* `Pending`：目标已变更，但 worker 尚未开始按该目标推进。
* `Reconciling`：worker 正在向 `DesiredPresent` 收敛。
* `Ready`：worker 已将环境推进到 `DesiredPresent` 对应的最新 generation。
* `Failed`：worker 处理过当前 generation，但本次收敛失败。
* `Deleting`：worker 正在向 `DesiredAbsent` 收敛。

### generation / observedGeneration

本方案引入内部版本语义：

* `generation`：每次用户修改生命周期目标或 deploy 内容目标时递增。
* `status.observedGeneration`：worker 成功或失败处理到哪个 generation。

判定语义：

* `status.observedGeneration < generation`：说明最新目标尚未被处理完成。
* `status.observedGeneration == generation`：说明当前目标已被处理过；若此时 `state=Failed`，表示当前目标处理失败。

## 数据结构设计

建议将 `projects/infra/deploy/domain/environment.go` 中的结构调整为：

```go
type Environment struct {
    name         EnvironmentName
    envType      EnvironmentType
    description  string
    desiredState *DesiredState
    status       *EnvironmentStatus
    generation   int64
    createTime   time.Time
    updateTime   time.Time
    etag         string
}

type EnvironmentStatus struct {
    Desired            EnvironmentDesired
    State              EnvironmentState
    ObservedGeneration int64
    Message            string
    LastReconcileTime  time.Time
    LastSuccessTime    time.Time
}
```

`EnvironmentSnapshot`、mongo 文档结构、fake repository 和测试构造器都需要同步带上 `generation` 与 `status.desired` / `status.observed_generation`。

## 代码分层

## handler

`handler` 只负责：

* 接收请求
* 修改环境目标
* 保存环境
* 将环境入队

handler 不再直接决定环境已经进入 `Reconciling` 或 `Deleting` 的实际阶段。

## domain

`domain` 层负责：

* 生命周期目标与实际阶段的建模
* generation 递增与 observedGeneration 对齐语义
* worker 执行时的阶段推进
* queue 工作项语义与 worker retry 分类
* recovery 需要继续收敛对象的定义

## storage

`storage` 负责：

* 持久化 `generation`
* 持久化 `status.desired`
* 持久化 `status.observed_generation`
* 支持 `ListNeedingReconcile` 查询

## worker

`worker` 负责：

* 读取环境快照
* 根据 `status.desired` 决定走 apply 还是 delete
* 推进 `status.state`
* 完成后写入 `status.observedGeneration`
* 发现处理期间目标版本已变化时再次入队
* 对可重试错误执行有限次退避重试

## queue

`queue` 负责：

* 存储待执行 `WorkItem`
* 对同一环境做去重与覆盖合并
* 区分 `queued` 与 `inFlight`，防止同一环境被并行执行
* 为 worker 提供“用户新任务覆盖旧 retry 任务”的统一语义

## 模型设计

## handler 语义

### CreateEnvironment

创建新环境时：

* `desiredState` 使用请求中的 deploy 内容。
* `status.desired = DesiredPresent`
* `status.state = StatePending`
* `generation = 1`
* `status.observedGeneration = 0`
* 保存后普通入队。

不再在 handler 中调用 `MarkReconciling()`。

### UpdateEnvironment

更新环境时：

* 替换 `desiredState`
* `status.desired = DesiredPresent`
* `status.state = StatePending`
* 清空 `status.message`
* 保留 `status.lastSuccessTime`
* `generation++`
* 保存后普通入队。

这样可以明确表达“新目标已接受，但尚未开始处理”。

### DeleteEnvironment

删除环境时：

* 保留原 `desiredState` 内容不动
* `status.desired = DesiredAbsent`
* `status.state = StatePending`
* 清空 `status.message`
* `generation++`
* 保存后普通入队。

删除请求被接受后，环境不会立刻显示 `Deleting`，而是由 worker 在真正开始删除流程时推进到 `Deleting`。

## aggregate 行为

建议将 `Environment` 的生命周期方法收敛为“改目标”和“推进实际”两类。

### 改目标

建议新增或重构以下方法：

* `SetDesiredPresent(newDesiredState *DesiredState) error`
* `SetDesiredAbsent() error`

其职责是：

* 修改 `status.desired`
* 将 `status.state` 置为 `Pending`
* 清理已失效的错误消息
* 递增 `generation`
* 更新时间戳

### 推进实际

以下方法保留，但语义改成仅由 worker 使用：

* `MarkReconciling() error`
* `MarkReady(processedGeneration int64) error`
* `MarkFailed(processedGeneration int64, msg string) error`
* `MarkDeleting() error`
* `SetReconcilingMessage(msg string) error`
* `SetStatusMessage(msg string) error`

关键约束：

* `MarkReady()` 和 `MarkFailed()` 需要写入 `status.observedGeneration = processedGeneration`
* `MarkDeleting()` 只表示“当前实际在执行删除”，不改变 `status.desired`
* `process()` 每次执行都负责将状态准确落库；是否需要重试由 `Run()` 决定

## worker 收敛逻辑

## Present 路径

当 `status.desired = DesiredPresent` 时，worker 目标是将环境推进到当前 `generation` 对应的 deploy 内容。

### 处理规则

* `state = Pending`：
  * 调用 `MarkReconciling()`
  * 保存一次进度
  * 调用 `runtime.Apply()`

* `state = Reconciling`：
  * 继续执行 `runtime.Apply()`

* `state = Ready`：
  * 若 `observedGeneration < generation`，说明用户在 ready 后又更新了目标，需要重新 `MarkReconciling()` 并执行 apply
  * 若 `observedGeneration == generation`，无需处理

* `state = Failed`：
  * 若 `observedGeneration < generation`，说明失败的是旧目标，新目标尚未处理，应重新 `MarkReconciling()` 并执行 apply
  * 若 `observedGeneration == generation`，说明当前目标已失败，是否继续在运行中重试由 `Run()` 根据错误分类和 retry count 决定

### apply 成功

* 调用 `MarkReady(processedGeneration)`
* 清空 message
* 更新时间
* 保存

### apply 失败

* 调用 `MarkFailed(processedGeneration, err.Error())`
* 保存
* 返回可重试或不可重试的分类错误，由 `Run()` 决定是否退避重试

### 处理期间目标变化

worker 开始处理时记录 `processedGeneration`。处理结束后再次从 repository 读取环境：

* 若环境不存在：直接结束
* 若环境的 `generation > processedGeneration`：说明处理期间目标已变化，需要再次入队
* 该路径作为“不增加 retry count 的重新执行”处理

这样可以保证“流程中被更新，流程后再次执行”。

## Absent 路径

当 `status.desired = DesiredAbsent` 时，worker 目标是删除环境。

### 处理规则

* `state = Pending`：
  * 调用 `MarkDeleting()`
  * 保存
  * 执行 `runtime.Delete()`

* `state = Deleting`：
  * 继续执行 `runtime.Delete()`

* `state = Ready / Reconciling / Failed`：
  * 统一先调用 `MarkDeleting()` 再进入删除路径

### delete 成功

* 直接 `repo.Delete()`

### delete 失败

* 保持 `status.desired = DesiredAbsent`
* 保持 `status.state = StateDeleting`
* 更新 message
* 保存
* 返回可重试或不可重试的分类错误，由 `Run()` 决定是否退避重试

## worker 退避重试设计

本方案将运行中重试统一收口到 `projects/infra/deploy/domain/worker.go` 中的 `Worker.Run()`。

原则如下：

* `process()` 负责分类错误，不直接决定重试策略。
* `Run()` 负责：
  * 判断是否需要重试
  * 是否增加 retry count
  * 计算 backoff
  * 在延迟后重新入队
  * 达到上限后停止重试
* `process()` 每次执行都要负责把当次执行结果落到环境状态中；`Run()` 只负责是否再次执行。

### 基础错误分类

建议在 `projects/infra/deploy/domain/errors.go` 中新增三类基础错误，并通过 `fmt.Errorf("%w: ...", ErrXxx)` 包装具体来源错误：

* `ErrRetryCounted`
  * 表示当前错误可重试
  * 本次重试需要增加 `WorkItem.RetryCount`

* `ErrRetryNoCount`
  * 表示当前错误需要重新执行
  * 但不增加 `WorkItem.RetryCount`
  * 典型场景：处理期间目标版本变化

* `ErrWorkerFatal`
  * 表示 worker 不应吞掉该错误
  * `Run()` 应直接返回，由上层按当前进程模型处理

`context.Canceled` / `context.DeadlineExceeded` 继续按上下文语义处理，不作为普通重试错误。

### `process()` 与 `Run()` 的职责

建议语义如下：

* `process(ctx, item)`：
  * 执行一次环境处理
  * 落当次执行状态
  * 返回 `nil` 或被包装过的分类错误

* `Run(ctx)`：
  * `Dequeue()` 取出 `WorkItem`
  * 调用 `process()`
  * 使用 `error.Is()` 判定错误类型
  * 对 `ErrRetryCounted` / `ErrRetryNoCount` 执行退避或重入队
  * 仅对 `ErrWorkerFatal` 返回错误

### backoff 与重试上限

建议第一版采用固定默认值：

* `maxRetries = 5`
* `delay = min(1s * 2^RetryCount, 30s)`

即：

* 第 1 次 counted retry：1s
* 第 2 次 counted retry：2s
* 第 3 次 counted retry：4s
* 第 4 次 counted retry：8s
* 第 5 次 counted retry：16s
* 封顶 30s

`ErrRetryNoCount` 不增加 `RetryCount`。第一版可直接重入队；如实现上需要避免热循环，也可用一个很短的固定延迟。

### 达到重试上限后的行为

当 `WorkItem.RetryCount` 达到上限时：

* `Run()` 不再重试
* 直接结束当前 item 的执行周期
* 环境最终状态以 `process()` 本次已经写入 repository 的状态为准

这保证：

* worker 不会在进程内无限自旋
* 环境状态仍由领域逻辑准确表达

### 非阻塞延迟重试

退避等待不能阻塞 `Run()` 主循环。建议做法：

* `Run()` 识别到可重试错误后
* 使用 timer / goroutine 在延迟后执行重入队
* `Run()` 自身继续处理下一项

这样某一个环境的 backoff 不会拖住整个 deploy worker。

## queue 设计

本方案移除优先队列，改为单队列模型。删除请求不再依赖优先级表达“更高优先级”，而是依赖环境最新 `status.desired = DesiredAbsent` 的状态语义。

### `WorkItem`

建议将 `projects/infra/deploy/domain/queue.go` 中的队列元素升级为：

```go
type WorkItem struct {
    EnvName    EnvironmentName
    RetryCount int
}
```

第一版不在 `WorkItem` 中加入更多调度字段。重试相关的等待由 worker timer 负责，不让 queue 自身理解 backoff 时间。

### 队列状态

为避免同一环境被重复排队或并行执行，queue 需要同时维护：

* `queued`
  * 表示已经在队列中等待被取出的环境
* `inFlight`
  * 表示已经被 `Dequeue()` 取出、正在由 worker 处理的环境

语义如下：

* `Dequeue()` 时：
  * 从 `queued` 移除
  * 加入 `inFlight`

* 当前 item 处理完成时：
  * 从 `inFlight` 移除

这样可以保证同一环境不会同时存在两个正在执行的工作项。

### 用户入队与 retry 入队

建议将 queue 的写入分成两类：

* 用户入队（handler / recovery / 目标变化后重新调度）
* retry 入队（worker 自己的 counted / no-count 重试）

并采用以下合并规则：

#### 用户入队

用户新任务视为新目标：

* 若该环境不在 `queued` / `inFlight`：新增 `WorkItem{RetryCount: 0}`
* 若该环境已在 `queued`：覆盖旧待处理项，并将 `RetryCount` 重置为 `0`
* 若该环境在 `inFlight`：不启动第二个并行执行实例，而是登记一个“当前执行完成后需要再跑一次”的新任务，且 `RetryCount` 重置为 `0`

#### retry 入队

worker 重试视为旧任务的继续执行：

* 若该环境不在 `queued` / `inFlight`：按当前 `WorkItem` 入队
* 若该环境已有用户新任务在 `queued`：保留用户新任务，丢弃 retry 项
* 若该环境在 `inFlight`：不生成第二个并行执行实例，只登记后续 retry 项

#### 目标变化重入队

处理期间版本变化对应 `ErrRetryNoCount`：

* 仍视为重新执行当前环境
* 不增加 `RetryCount`
* 若已有更新的用户任务，则保留用户任务

### retry 信息仅存在 queue item

本方案明确：

* `RetryCount` 只存在于 `WorkItem`
* 不写入 `Environment`
* 不持久化到 repository / Mongo

这意味着：

* 运行时 retry count 是单进程内执行信息
* 进程重启后 retry count 清零
* recovery 拉起对象时，开始一个新的 retry 周期

## recovery 设计

`projects/infra/deploy/domain/recovery.go` 不再拼 `StateReconciling` / `StateDeleting` 状态列表，而是统一调用：

* `repo.ListNeedingReconcile(ctx)`

recovery 的职责变成：

* 找出仍需继续收敛的环境
* 以“用户新任务”的方式重新入队

这样 recovery 只依赖“是否还需要 reconcile”的统一定义，而不再依赖一组脆弱的中间状态。

## Repository 设计

## 保留接口

保留现有：

* `ListByStates(ctx, ...EnvironmentState)`

它仍然用于“按当前实际状态筛选环境”。

## 新增接口

新增：

* `ListNeedingReconcile(ctx context.Context) ([]*Environment, error)`

命名含义明确：返回仍需继续收敛的环境。

### `ListNeedingReconcile` 判定规则

本方案将其定义为返回满足以下任一条件的环境：

* `status.desired = DesiredPresent` 且 `status.observedGeneration < generation`
* `status.desired = DesiredPresent` 且 `status.state = StateFailed` 且 `status.observedGeneration == generation`
* `status.desired = DesiredAbsent`

第二条虽然不会直接由 recovery 触发“运行中 retry”，但它仍然属于“需要 reconcile 的环境”，因此 recovery 会在进程重启后重新纳入待处理集合。

## Mongo 持久化

`projects/infra/deploy/storage/mongo.go` 需要增加：

* `generation`
* `status.desired`
* `status.observed_generation`

建议结构：

```go
type mongoEnvironment struct {
    ...
    DesiredState *mongoDesiredState `bson:"desired_state"`
    Status       *mongoStatus       `bson:"status"`
    Generation   int64              `bson:"generation"`
    ...
}

type mongoStatus struct {
    Desired            int       `bson:"desired"`
    State              int       `bson:"state"`
    ObservedGeneration int64     `bson:"observed_generation"`
    Message            string    `bson:"message"`
    LastReconcileTime  time.Time `bson:"last_reconcile_time"`
    LastSuccessTime    time.Time `bson:"last_success_time"`
}
```

`ListNeedingReconcile()` 在 Mongo 中可以按三类条件组合查询。第一版可以直接在 storage 层拼 BSON 条件，不需要额外抽象。

## 关键细节

## proto 不改

本方案明确不修改 `deploy.proto`。因此：

* `status.desired`
* `generation`
* `status.observedGeneration`

都只在 deploy service 内部使用，不对外暴露。

这意味着第一阶段解决的是**内部一致性与收敛语义**，而不是 API 表达力。

## `desiredState` 内容在 delete 时保持不变

删除请求只修改生命周期目标，不清空 deploy 内容目标。原因是：

* 改动范围最小
* 不需要为“空 desiredState”引入额外语义
* 删除流程只依赖 `status.desired = DesiredAbsent`

## `Failed` 与运行中重试

本方案明确：

* worker 运行中的再次执行由 `Run()` 基于分类错误统一调度
* `process()` 每次执行都要把当次状态写入环境
* 当 `state=Failed` 且 `observedGeneration==generation` 时，表示当前目标已处理失败；若该失败路径返回 `ErrRetryCounted`，则由 `Run()` 在进程内执行退避重试
* 若达到重试上限，worker 停止继续重试，但对象仍可在后续进程重启后通过 recovery 再次进入执行周期

这样可以把运行中重试和重启恢复清晰拆开：

* 运行中 retry：由 worker + queue item 控制
* 重启后恢复：由 `ListNeedingReconcile()` 控制

## 对并发更新的处理方式

本方案不引入新的外部协议 CAS，但通过 `generation / observedGeneration` 保证以下语义：

* worker 可以知道自己处理的是哪一版目标
* worker 可以在处理结束后判断目标是否已变化
* 目标变化后会再次入队，不会因为旧流程结束而丢失新目标
* queue 通过 `queued / inFlight` 语义，避免因为重入队而并行执行同一环境

这已经能满足“流程中被更新，流程后再次执行”的要求。

## 决策详情

本方案最终采用以下决策：

* 生命周期目标字段放入 `status`，命名为 `desired`
* `desired` 仅做两态：`Present` / `Absent`
* delete 请求先写 `Pending`，worker 再推进到 `Deleting`
* `UpdateDesiredState()` 每次都将 `state` 置回 `Pending`
* `ListByStates()` 保留，新增 `ListNeedingReconcile`
* recovery 完全改为基于 `ListNeedingReconcile`
* queue 重构为单队列 `WorkItem` 模型，不再保留优先队列
* `WorkItem` 最小字段为 `EnvName` 与 `RetryCount`
* 运行中 retry 由 `Worker.Run()` 统一负责，`process()` 只做错误分类
* 分类错误通过基础 error + wrapping 表达，`Run()` 使用 `error.Is()` 判定
* 基础错误集合为：`ErrRetryCounted`、`ErrRetryNoCount`、`ErrWorkerFatal`
* 用户新任务覆盖旧 retry 任务并重置 retry count
* queue 同时维护 `queued` / `inFlight`，防止同一环境被并行执行
* retry 信息只保存在 queue item，不写入环境对象
* `maxRetries = 5`，backoff 采用指数退避并封顶 30s
* 保留 `DesiredUnspecified`
* proto 暂不调整

## 验收效果

完成本方案后，应能达到以下效果：

* Create / Update / Delete 请求只修改目标，不再把实际阶段写死。
* worker 能根据 `desired` 推进 `state`，而不是只靠当前 `state` 猜测下一步。
* 运行中目标变更后，旧流程结束不会吞掉新目标，环境会再次进入待收敛队列。
* worker 对可重试错误执行有限次退避重试，不阻塞主循环，也不会在进程内无限自旋。
* queue 能正确处理“用户新任务覆盖旧 retry 任务”和“任务已在执行中”的情况，不会让同一环境并行执行两次。
* recovery 能稳定找出所有仍需继续收敛的环境，而不是只关心少数中间状态。
* `Failed` 对象既可以在当前进程内按 retry 策略继续执行，也不会失去后续统一恢复能力。

## 预计改动范围

主要涉及以下文件：

* `projects/infra/deploy/domain/environment.go`
* `projects/infra/deploy/domain/state.go`
* `projects/infra/deploy/domain/repository.go`
* `projects/infra/deploy/domain/queue.go`
* `projects/infra/deploy/domain/worker.go`
* `projects/infra/deploy/domain/errors.go`
* `projects/infra/deploy/domain/recovery.go`
* `projects/infra/deploy/handler.go`
* `projects/infra/deploy/storage/mongo.go`
* `projects/infra/deploy/repository_fake_test.go`
* `projects/infra/deploy/domain/*_test.go`
* `projects/infra/deploy/handler_test.go`
* `projects/infra/deploy/storage/mongo_test.go`

## 未来规划

本次不实现，但后续可单独评估：

* 将 `generation / observedGeneration` 与 API 对外状态表达对齐
* 更细粒度的 condition / reason / message 设计
* 基于 optimistic concurrency 的更强写入保护
* 更复杂的 retry 策略（如 jitter、按错误类型配置退避参数）
