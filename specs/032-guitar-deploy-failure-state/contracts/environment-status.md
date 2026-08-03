# Contract: Environment per-service status (proto / domain)

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-03
**Owner**: `projects/infra/deploy`（deploy service 控制面）

## 用途

为 `Environment.status` 新增结构化的 per-service rollout 状态，使消费者（`deploy describe`、未来其他工具）能稳定地获知环境内**每个**服务（artifact 与 infra）的观测状态与原因，取代初版依赖的、不稳定的环境级自由文本 `message`。本契约定义 proto/ domain 的字段、枚举、语义与写入/读取规则。

## proto 变更（`projects/infra/deploy/deploy.proto`）

### 新增 message：`ServiceStatus`（紧跟 `EnvironmentStatus` 之后定义）

```proto
// ServiceStatus 描述环境内单个服务（artifact 或 infra）的观测 rollout 状态。
message ServiceStatus {
  // name 是服务名，对应 ArtifactSpec.name 或 InfraSpec.name。
  string name = 1;

  // app 是该服务所属应用。
  string app = 2;

  // kind 标识服务来源（artifact 或 infra）。
  ServiceKind kind = 3;

  // state 是观测到的 rollout 状态。
  ServiceRolloutState state = 4;

  // message 提供该状态的补充详情（如 "可用副本不足（available: 0/1）"）。
  string message = 5;
}
```

### 新增 enum：`ServiceKind`

```proto
enum ServiceKind {
  SERVICE_KIND_UNSPECIFIED = 0;
  SERVICE_KIND_ARTIFACT = 1;
  SERVICE_KIND_INFRA = 2;
}
```

### 新增 enum：`ServiceRolloutState`

```proto
enum ServiceRolloutState {
  SERVICE_ROLLOUT_STATE_UNSPECIFIED = 0;
  // PENDING：资源已提交到运行时，尚未进行首次 rollout 观测。
  SERVICE_ROLLOUT_STATE_PENDING = 1;
  SERVICE_ROLLOUT_STATE_READY = 2;
  SERVICE_ROLLOUT_STATE_WAITING = 3;
  SERVICE_ROLLOUT_STATE_FAILED = 4;
}
```

### 扩展 `EnvironmentStatus`（`deploy.proto:158-166`）

新增第 5 字段（向后兼容，proto3 additive）：

```proto
message EnvironmentStatus {
  EnvironmentState state = 1 [(google.api.field_behavior) = OUTPUT_ONLY];
  string message = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp last_reconcile_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp last_success_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];
  // services 是环境内每个服务的观测 rollout 状态列表（OUTPUT_ONLY，由服务端写入）。
  repeated ServiceStatus services = 5 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

### codegen

由 bazel `go_proto_library`（`projects/infra/deploy/BUILD.bazel:58-75`）驱动，compilers `go_grpc_v2` + `go_proto` + `go_gen_grpc_gateway` + `go_gen_aip`。编辑 proto 后 `bazel build //projects/infra/deploy:deploy` / `bazel test //projects/infra/deploy/...` 自动重新生成 gRPC stub、grpc-gateway、AIP 辅助代码——**无手动 codegen 步骤**。生成代码落入包 `dominion/projects/infra/deploy`（`option go_package`）。

## domain 变更（`projects/infra/deploy/domain/`）

### 新增类型（`reconcile_types.go`）

`ServiceKind`（int 枚举：`Unspecified`/`Artifact`/`Infra`）、`ServiceRolloutState`（int 枚举：`Unspecified`/`Pending`/`Ready`/`Waiting`/`Failed`）、`ServiceStatus{Name, App string; Kind ServiceKind; State ServiceRolloutState; Message string}`（值类型结构体，字段无内嵌指针）。枚举须提供 `String()` 方法（便于日志/测试可读）。

### `EnvironmentStatus` 扩展（`environment.go:46-53`）

新增 `Services []*ServiceStatus` 字段（指针 slice，依 `style/golang.md:49-55` 容器元素规则，与既有 `[]*ArtifactSpec`/`[]*InfraSpec` 一致；亦与 proto 生成的 `[]*ServiceStatus` 对齐）。`cloneStatus`（`environment.go:369`）改为对 `Services` 深拷贝（决策 R7）。

### `RolloutStatus` 扩展（`reconcile_types.go:18`）

新增 `Services []*ServiceStatus` 字段，供 `CheckRollout` 回传 per-service。`EnvironmentRuntime.CheckRollout`（`worker.go:31`）签名不变。

### 新增 helper：`buildInitialServiceStatuses`

domain 层纯函数（建议放 `reconcile_types.go` 或 `environment.go`）：
```go
// buildInitialServiceStatuses 据 desired_state 为每个服务构造 PENDING 初始状态。
func buildInitialServiceStatuses(ds *DesiredState) []*ServiceStatus
```
- artifacts → `{Name, App, Kind: ServiceKindArtifact, State: ServiceRolloutStatePending, Message: "资源已提交，等待观测"}`
- infras → `{Name, App, Kind: ServiceKindInfra, State: ServiceRolloutStatePending, Message: "资源已提交，等待观测"}`
- 顺序：先 artifacts 后 infras（与 desired_state 一致）。
- `ds == nil` 或无服务 → 返回 nil。

## 写入语义（reconciler）

`service/reconcile.go` 各 `repo.TransitionStatus` 调用点构造的 `EnvironmentStatus.Services`（决策 R4/R6，详见 [../research.md](../research.md) 决策 R6 表格）：

| 调用点 | Services | 说明 |
|--------|----------|------|
| `transitionToReconciling` | nil | 清空，新一轮 reconcile 作废旧 per-service |
| `applyAndWait` | `buildInitialServiceStatuses(env.DesiredState())` | 进入 WAITING_ROLLOUT 即写初始 PENDING |
| `retainWaitingRollout` | `status.Services`（来自 CheckRollout） | 更新各服务真实状态 |
| `markReadyFromRollout` | `status.Services`（CheckRollout 全 ready） | — |
| `markFailedFromRollout` | `status.Services`（CheckRollout 含失败） | — |
| `transitionToDeleting` | nil | 清空 |
| `MarkRetryExhausted` | nil | apply 阶段失败，无 rollout 数据 |

> **注意（决策 R11）**：`retainWaitingRollout` 既有"env-level message 相等即早退（不写库）"优化（`reconcile.go:196`）会跳过 `TransitionStatus`，导致 per-service 更新不落库。修订后：三个 rollout 转移函数（`markReadyFromRollout`/`markFailedFromRollout`/`retainWaitingRollout`）签名从 `message string` 改为整体 `status *domain.RolloutStatus`；`retainWaitingRollout` 早退条件改为 `message 相等 && servicesEqual(env.Status().Services, status.Services)`（新增 domain helper `servicesEqual`），二者均未变化才跳过写入。

### runtime `CheckRollout` 产出契约（`runtime/k8s/rollout.go:263`，重构）

输入：`env`（取 `DesiredState` 经 `ConvertToWorkloads` 得 workload 对象集合）。
处理：遍历**全部** workload（`objects.Deployments` + `objects.MongoDBWorkloads` + `objects.StatefulWorkloads`），逐个取 k8s 对象判定状态，构造 `ServiceStatus`：
- Deployment（artifact）：`name=workload.ServiceName, app=workload.App, kind=Artifact`；state 据 `isDeploymentReady`/`isDeploymentFailed`（ready→READY，failed→FAILED，余→WAITING）；message 据 `deploymentNotReadyMessage`/`deploymentFailureMessage`。
- MongoDB（infra）：同上，`kind=Infra`，k8s 名取 `ResourceName()`。
- StatefulSet（artifact）：`name=workload.ServiceName, app=workload.App, kind=Artifact`；state 据 `isStatefulSetReady`（ready→READY，余→WAITING，StatefulSet 无可靠 failed 信号）；message 据 `statefulSetNotReadyMessage`。
- **不再** early-return on first failed（决策 R3）。

派生：
- env-level `State`：任一 FAILED → `RolloutFailed`；否则任一 WAITING/PENDING → `RolloutWaiting`；否则 `RolloutReady`。
- env-level `Message`：拼接非 READY 服务的 message（`"; "` 连接），空则 ""。
- 返回 `RolloutStatus{State, Message, Services: <全部服务列表>}`。

> **注意**：`PENDING` 由 `buildInitialServiceStatuses`（applyAndWait）写入，`CheckRollout` 不会产出 PENDING（它只观测已 apply 的 workload，产出 READY/WAITING/FAILED）。

## 持久化语义（storage）

`storage/mongo.go`：
- `mongoStatus` 增 `Services []mongoServiceStatus`（**bson tag `services` 无 `omitempty`**）。
- `TransitionStatus` 对 `services` **无条件 `$set`**（nil → 空 slice → 清空 stale；决策 R6）。
- `statusToMongo`/`statusFromMongo` 增 `Services` 双向映射。
- MongoDB schemaless：**无 migration**；旧文档缺 `services` 解码为零值 slice。

## handler 映射（`projects/infra/deploy/handler.go`）

`toProtoStatus`（`handler.go:420-430`）增 `Services` 映射；新增 `toProtoServices([]*domain.ServiceStatus) []*ServiceStatus` 与 `serviceKindToProto`/`serviceRolloutStateToProto` 枚举映射（及反向，如需 `fromProtoStatus`——当前 handler 无 fromProtoStatus，status 为 OUTPUT_ONLY 不入参）。`GetEnvironment`/`ListEnvironments` 经 `toProtoEnvironment`→`toProtoStatus` 自动返回 services。

## 客户端影响

- **v2 HTTP client**（`tools/release/deploy/v2/client/client.go:94`）：直接返回 proto `*deploy.Environment`（`protojson.Unmarshal, DiscardUnknown:true`），**proto 新字段自动流经，无 client 改动**。
- **deploy CLI v3**：`describe.go` 消费 proto `Environment.Status.Services`（见 [deploy-describe.md](./deploy-describe.md)）。
- **guitar**：经 shell-out 消费 describe stdout，无 proto 接触（见 [guitar-integration.md](./guitar-integration.md)）。

## 向后兼容性

- proto3 additive：旧 client 忽略 `services`；旧服务端不返回 `services`（describe 回退到初版纯服务列表，见归并逻辑"无匹配则不追加"）。
- domain 枚举零值为 `Unspecified`，与既有 `EnvironmentState` 零值处理一致。
- 无 RPC 签名变更（`GetEnvironment`/`ListEnvironments` 不变）。

## 参考

- proto 现状：`projects/infra/deploy/deploy.proto:158-176`（`EnvironmentStatus`/`EnvironmentState`）
- reconcile 现状：`projects/infra/deploy/service/reconcile.go:125-209`（applyAndWait/checkRollout/retain/markReady/markFailed）
- runtime 现状：`projects/infra/deploy/runtime/k8s/rollout.go:263-336`（CheckRollout，待重构）
- workload 身份：`projects/infra/deploy/runtime/k8s/converter.go:31-65`、`model.go:34,107,266`
- storage 现状：`projects/infra/deploy/storage/mongo.go:196-203,426-473,633-645,797-808`
- handler 现状：`projects/infra/deploy/handler.go:309-324,420-430`
- 决策出处：[../research.md](../research.md) 决策 R1~R7、R10
