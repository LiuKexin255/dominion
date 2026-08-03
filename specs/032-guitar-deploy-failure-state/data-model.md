# Data Model: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02（初版）, 2026-08-03（修订：per-service 状态）

本特性的持久化实体变更为 deploy service 的 `EnvironmentStatus` 新增 per-service 状态子结构；CLI 侧聚焦 `deploy describe` 的**输出模型**（从既有 `Environment` proto 投影）。本文件定义：①新增 `ServiceStatus` 实体；②`EnvironmentStatus` 扩展；③`RolloutStatus` 扩展；④describe 输出模型；⑤reconcile per-service 状态流转；⑥验证规则。

## 新增实体：ServiceStatus（proto + domain）

### proto（`projects/infra/deploy/deploy.proto`，新增）

`ServiceStatus`（新 message）：

| 字段 | proto 类型 | domain 类型 | 含义 | 来源 |
|------|-----------|-------------|------|------|
| `name` | `string = 1` | `string` | 服务名 | `ArtifactSpec.name` 或 `InfraSpec.name` |
| `app` | `string = 2` | `string` | 所属应用 | `ArtifactSpec.app` / `InfraSpec.app` |
| `kind` | `ServiceKind = 3` | `ServiceKind` | 服务来源 | artifact→`ARTIFACT`；infra→`INFRA` |
| `state` | `ServiceRolloutState = 4` | `ServiceRolloutState` | 观测 rollout 状态 | reconciler 写入 |
| `message` | `string = 5` | `string` | 详情（可读原因） | k8s rollout 提取 / 初始固定文案 |

`ServiceKind`（新 enum）：`SERVICE_KIND_UNSPECIFIED=0`、`SERVICE_KIND_ARTIFACT=1`、`SERVICE_KIND_INFRA=2`。

`ServiceRolloutState`（新 enum）：

| 值 | 名称 | 含义 | 何时写入 |
|----|------|------|----------|
| 0 | `UNSPECIFIED` | 未指定 | 默认零值（不主动写） |
| 1 | `PENDING` | 资源已提交，等待首次 rollout 观测 | `applyAndWait` 进入 WAITING_ROLLOUT（决策 R4） |
| 2 | `READY` | rollout 完成 | `checkRollout` 判定该 workload ready |
| 3 | `WAITING` | rollout 进行中 | `checkRollout` 判定该 workload not-ready 且未 failed |
| 4 | `FAILED` | rollout 失败 | `checkRollout` 判定该 workload 有失败 condition |

### domain（`projects/infra/deploy/domain/reconcile_types.go`，新增）

```go
type ServiceKind int
const (
    ServiceKindUnspecified ServiceKind = iota
    ServiceKindArtifact
    ServiceKindInfra
)

type ServiceRolloutState int
const (
    ServiceRolloutStateUnspecified ServiceRolloutState = iota
    ServiceRolloutStatePending
    ServiceRolloutStateReady
    ServiceRolloutStateWaiting
    ServiceRolloutStateFailed
)

type ServiceStatus struct {
    Name    string
    App     string
    Kind    ServiceKind
    State   ServiceRolloutState
    Message string
}
```

对象语义（观测状态快照，按实例身份区分）；容器元素遵循 `style/golang.md:49-55`「容器元素强制指针」用 `[]*ServiceStatus`（与既有 `[]*ArtifactSpec`/`[]*InfraSpec` 一致，亦与 proto 生成的 `[]*ServiceStatus` 对齐）。domain `ServiceStatus` 与 proto `ServiceStatus` 经 handler 的 `toProtoServices` 双向映射（含 `ServiceKind`/`ServiceRolloutState` 的枚举映射函数）。

### 校验规则

- `name` 非空、`app` 非空（与 desired_state 中 ArtifactSpec/InfraSpec 的校验一致，`domain/spec.go:120-128,182-184`）。
- `kind` ∈ {Artifact, Infra}（UNSPECIFIED 仅用于零值占位，不主动构造）。
- `state` ∈ {Pending, Ready, Waiting, Failed}（UNSPECIFIED 仅零值占位）。
- reconciler 写入时保证 `name`+`app`+`kind` 三元组与 `env.DesiredState()` 中的服务一一对应。

## 实体扩展：EnvironmentStatus

### proto（`projects/infra/deploy/deploy.proto:158-166`，扩展）

新增字段（向后兼容，proto3 additive）：
```proto
message EnvironmentStatus {
  EnvironmentState state = 1 [(...OUTPUT_ONLY)];
  string message = 2 [(...OUTPUT_ONLY)];
  google.protobuf.Timestamp last_reconcile_time = 3 [(...OUTPUT_ONLY)];
  google.protobuf.Timestamp last_success_time = 4 [(...OUTPUT_ONLY)];
  repeated ServiceStatus services = 5 [(google.api.field_behavior) = OUTPUT_ONLY];  // 新增
}
```

### domain（`projects/infra/deploy/domain/environment.go:46-53`，扩展）

`EnvironmentStatus` 增字段：
```go
type EnvironmentStatus struct {
    Desired            EnvironmentDesired
    State              EnvironmentState
    ObservedGeneration int64
    Message            string
    LastReconcileTime  time.Time
    LastSuccessTime    time.Time
    Services           []*ServiceStatus  // 新增（指针 slice，依 style/golang.md 容器元素规则）
}
```

**深拷贝要求（决策 R7）**：`cloneStatus`（`environment.go:369-376`）由浅拷贝改为对 `Services` 深拷贝（指针 slice，逐元素拷值后取址，镜像 `cloneArtifacts` 的 `spec := *artifact; cloned[i] = &spec` 模式）：
```go
func cloneStatus(status *EnvironmentStatus) *EnvironmentStatus {
    if status == nil { return nil }
    cloned := *status
    if len(status.Services) > 0 {
        cloned.Services = make([]*ServiceStatus, len(status.Services))
        for i, s := range status.Services {
            cp := *s
            cloned.Services[i] = &cp
        }
    }
    return &cloned
}
```

### storage（`projects/infra/deploy/storage/mongo.go`，扩展）

`mongoStatus`（`mongo.go:196-203`）增字段（**bson tag 无 `omitempty`**，确保 nil → 空数组以支持清空语义）：
```go
type mongoStatus struct {
    // ...既有...
    Services []mongoServiceStatus `bson:"services"`
}
type mongoServiceStatus struct {
    Name    string `bson:"name"`
    App     string `bson:"app"`
    Kind    int    `bson:"kind"`
    State   int    `bson:"state"`
    Message string `bson:"message"`
}
```

`TransitionStatus`（`mongo.go:426-473`）对 services **无条件写入**（决策 R6）：在 `setFields` 中恒置 `mongoFieldStatusServices: servicesToMongo(toStatus.Services)`（nil → 空 slice → 清空 stale）。`statusToMongo`/`statusFromMongo`（`mongo.go:633-645,797-808`）增 `Services` 映射。

> MongoDB schemaless：旧文档无 `services` 字段时解码为零值 slice，**无需 migration**。

## 实体扩展：RolloutStatus（domain 内部回传）

`domain.RolloutStatus`（`reconcile_types.go:18-23`）增字段，供 `CheckRollout` 回传 per-service：
```go
type RolloutStatus struct {
    State    RolloutState
    Message  string
    Services []*ServiceStatus  // 新增
}
```

`domain.EnvironmentRuntime.CheckRollout`（`worker.go:31`）**签名不变**（返回 `*RolloutStatus`），仅结构体增字段——所有 fake runtime 零改动（新字段默认 nil）。

## describe 输出模型（人类可读文本，修订）

guitar 调用 `deploy describe` 前，由 `Reporter.DeployDiagnostics` 先打印醒目头部（初版，不变）：
```
  --- 环境状态 (env=game.lt3x8q2) ---
```
随后 `deploy describe` 输出**顶格**。修订后示例（WAITING_ROLLOUT，含 per-service 状态）：

```
环境 game.lt3x8q2
状态: 等待滚动发布
服务:
  - service (app=game) [artifact] 就绪
  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）
  - mongo (app=game) [infra: mongodb] 已提交，等待观测
最近调和: 2026-08-03T10:30:05Z
最近成功: -
```

FAILED 场景示例：
```
环境 game.lt3x8q2
状态: 失败
服务:
  - gateway (app=game) [artifact] 失败: Deployment rollout 超过进度截止时间
  - service (app=game) [artifact] 等待发布: 可用副本不足（available: 1/2）
最近调和: 2026-08-03T10:30:10Z
最近成功: -
```

非 rollout 原因（apply 阶段失败，无 per-service 数据）示例：
```
环境 game.lt3x8q2
状态: 失败
说明: retry count exhausted
服务:
  - gateway (app=game) [artifact]
  - service (app=game) [artifact]
最近调和: 2026-08-03T10:30:10Z
最近成功: -
```

### 字段规则（修订）

| 输出行 | 规则 | 校验/边界 |
|--------|------|-----------|
| `环境 {fullEnvName}` | 恒输出 | fullEnvName = `{scope}.{env}` |
| `状态: {state中文}` | 恒输出；UNSPECIFIED → `未知` | 复用 `formatState`（已含 WAITING_ROLLOUT） |
| `说明: {message}` | **仅当 `status.services` 为空且 `status.message` 非空时输出**（决策 R5：避免与 per-service 重复；保留对 apply 失败/retry-exhausted 的表达） | — |
| `服务:` + 列表项 | desired_state 非空时输出；空则 `服务: （无）` | 每项内联 per-service 状态（见下） |
| `最近调和: {ts}` | 恒输出；nil → `-` | RFC3339 UTC |
| `最近成功: {ts}` | 恒输出；nil → `-` | RFC3339 UTC |

### 服务列表项格式（修订，内联 per-service 状态）

每项基础格式沿用初版：`  - {name} (app={app}) [{kind-tag}]`，其中 kind-tag：artifact → `artifact`；infra → `infra: {resource}`。

当 `status.services` 含该服务（按 `name`+`app`+`kind` 三元组归并）的 per-service 状态时，在项尾**追加状态文本**：

| ServiceRolloutState | 追加文本 | 示例 |
|---------------------|----------|------|
| `READY` | ` 就绪` | `  - service (app=game) [artifact] 就绪` |
| `WAITING` | ` 等待发布: {message}` | `  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）` |
| `FAILED` | ` 失败: {message}` | `  - gateway (app=game) [artifact] 失败: ImagePullBackOff` |
| `PENDING` | ` 已提交，等待观测` | `  - mongo (app=game) [infra: mongodb] 已提交，等待观测` |
| `UNSPECIFIED`/无匹配 | （无追加） | `  - service (app=game) [artifact]` |

**归并逻辑**：以 desired_state 的服务列表为权威顺序（分段遍历 artifacts 后 infras，遍历时 kind 已知），对每个服务在 `status.services` 中按 `name`+`app`+`kind` 三元组查找匹配项；匹配则追加其状态文本，不匹配则不追加（旧版服务端无 per-service 数据时，输出回退到初版纯列表）。用三元组而非 name+app 是因为 domain 校验仅保证 artifact name 唯一、infra name 唯一，不保证跨类唯一（`domain/environment.go:302-306`）。

**环境不存在**：`GetEnvironment` 返回 `ErrNotFound` 时输出 `环境 {fullEnvName} 不存在` 并以非零退出码返回（初版不变）。

## reconcile per-service 状态流转（时序）

```
applyAndWait（RECONCILING → WAITING_ROLLOUT）:
  runtime.ApplyResources(env)                         ← 提交 workload 到 k8s
  initial = buildInitialServiceStatuses(env.DesiredState())   ← 决策 R4：每个服务 PENDING
  repo.TransitionStatus(..., RECONCILING, &EnvironmentStatus{
      State: WAITING_ROLLOUT, ObservedGeneration: gen, Services: initial,   ← 写入
  })
  → RequeueAfter 5s（rolloutPollInterval）

checkRollout（WAITING_ROLLOUT，每 5s）:
  status = runtime.CheckRollout(env)                 ← 返回 {State, Message, Services}（决策 R3）
  switch status.State:
    READY    → markReadyFromRollout:  Services = 全部 READY（来自 status.Services）
    FAILED   → markFailedFromRollout: Services = status.Services（含失败服务）
    WAITING  → retainWaitingRollout:  Services = status.Services（更新各服务状态）

其他转移（transitionToReconciling / transitionToDeleting / MarkRetryExhausted）:
  Services = nil（清空 stale，决策 R6）
```

**不变量**：
- `buildInitialServiceStatuses` 与 `CheckRollout` 产出的 Services 数量 = desired_state 的服务总数（artifact + infra）。顺序不敏感（describe 按三元组查找归并，展示顺序以 desired_state 为准）。
- `CheckRollout` 不再 early-return on first failed（决策 R3）：全部 workload 均上报状态。
- env-level `State`/`Message` 由 per-service 派生（任一 failed→Failed；任一 waiting→Waiting；否则 Ready），reconcile 状态机 switch 语义不变。
- **早退条件（决策 R11）**：`retainWaitingRollout` 既有"message 相等即早退"优化（`reconcile.go:196`）会跳过 `TransitionStatus` 写入——须将早退条件改为 message 与 Services **同时**相等才跳过（新增 domain helper `servicesEqual`），否则 per-service 更新在 message 不变时会丢失（如某服务转 READY 而其余仍等待、拼接 message 相同）。

## 验证规则（来自 spec FR）

- **FR-003（失败原因）**：FAILED/WAITING 场景下 per-service `message` 携带具体原因（如 `可用副本不足（available: 0/1）`、`ImagePullBackOff`），describe 内联展示，满足"判断哪个 service"。
- **FR-004（服务集合）**：服务列表来自 desired_state（权威），per-service 状态归并其上。
- **FR-005/FR-006（降级）**：describe 失败（环境不存在/不可达）时 guitar 降级 warning、不改原始错误（初版不变）。
- **FR-007/SC-004（cleanup 不变）**：guitar 诊断与 cleanup 均用 `WithoutCancel(ctx)`，cleanup defer 不变。
- **FR-008/SC-005（成功路径）**：apply 成功不进诊断分支；READY 时 per-service 全 ready，describe 无 `说明:` 行。
- **FR-009（颜色降级）**：describe 输出无 ANSI 码；Reporter 头部行遵循既有 `checkTerminal`（初版不变）。
- **FR-010（per-service 稳定数据源）**：applyAndWait 写入初始 PENDING（决策 R4）；retainWaitingRollout 早退条件同时比较 Services（决策 R11），保证 per-service 状态随状态变化持续更新。
- **时序空窗消除（决策 R4）**：进入 WAITING_ROLLOUT 即有 PENDING per-service 数据，guitar 短超时场景 describe 亦能列出服务。
