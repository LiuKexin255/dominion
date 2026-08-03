# Research: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02（初版）, 2026-08-03（修订：per-service 状态）

## 背景与现状（来自代码勘察）

### 初版已落地的部分（保留）

- `guitar run` 编排链路：validate → deploy apply → bazel test → deploy del（清理）。部署步骤在 `tools/test/guitar/pkg/run/run.go:151` 通过 `runCommand(ctx, deployBinary, deployApplyCommand, ...)` 调用外部 `deploy` 二进制；失败时仅 `return fmt.Errorf("deploy apply %s: %w", ...)`。
- 初版已新增 `deploy describe <env>` 子命令（`tools/release/deploy/v3/describe.go`）调用 `GetEnvironment` 打印环境详情；guitar 在部署失败分支经 `context.WithoutCancel(ctx)` shell-out 调 `deploy describe --timeout=10s`（`run.go:175-180` `diagnoseDeployFailure`），失败降级不掩盖原始错误。该链路**已实现且正确**，本次修订**不改动 guitar**。

### 初版暴露的问题：数据源 `message` 不稳定

初版 describe 依赖 `EnvironmentStatus.message`（自由文本）承载失败/等待原因。实测 `guitar run ... --timeout=5s`（环境进入 `WAITING_ROLLOUT` 但首个 `checkRollout` 轮询尚未完成）时 `message` 为空 → describe 无法指认哪个 service。根因（均已核实，带出处）：

1. **数据结构层**：`EnvironmentStatus`（`projects/infra/deploy/deploy.proto:158-166`）只有环境级 `{state, message, last_reconcile_time, last_success_time}`，**无 per-service 状态**。`DesiredState` 的 `ArtifactSpec`/`InfraSpec` 全是期望配置（无状态字段）。全仓库仅此一份 proto，无 events/rollout 子资源。
2. **折叠层**：`runtime/k8s/rollout.go:263-336` 的 `CheckRollout` 逐 workload 计算 ready/failed/waiting + 可读原因（`deploymentNotReadyMessage` 等，含副本数 `available: x/y`），但随后 `strings.Join(waitingMessages, "; ")`（`rollout.go:335`）折叠为**单一环境级字符串**写入 `message`。per-service 结构信息被丢弃。且遇到首个 failed deployment 即 early-return（`rollout.go:306`），其余服务状态不上报。
3. **填充时序层**：`message` 仅由 `checkRollout`（`service/reconcile.go:147`，每 `rolloutPollInterval=5s` 轮询一次）经 `retainWaitingRollout`（`reconcile.go:195-209`）/`markFailedFromRollout`（`reconcile.go:180`）写入。进入 `WAITING_ROLLOUT`（`applyAndWait`，`reconcile.go:134`）时**不写 message**。故从进入 `WAITING_ROLLOUT` 到首个 `checkRollout` 完成之间存在~5s 空窗，期间 `message` 为空。
4. **时序竞争**：guitar 的 `--timeout=5s` 与该~5s 轮询窗口竞争，若在空窗内杀掉 deploy-apply 并立即 describe，`message` 仍为空。

**结论**：`message` 既无 per-service 结构，又有时序空窗，不适合作为"判断哪个 service 失败/超时"的稳定数据源。

### deploy service 的大型测试约束（关键发现）

`projects/infra/deploy/README.md:24` 与 `:28` 明确：deploy service **禁止使用 `deploy` 工具部署**（无法自举——它是 deploy/guitar/testplan 部署链路的后端），故**不进行大型测试**。全仓库各 `testplan` 目录（`experimental/*/testplan`、`projects/game/testplan` 等）无 deploy service 用例（已核实）。这是仓库既定的、基于技术约束的例外，Constitution 原则 VI 对 deploy service 的适用以此例外为准。

### 服务→workload 的身份映射（用于 per-service 状态归位）

`runtime/k8s/converter.go:31-65` `ConvertToWorkloads` 将 desired_state 映射为 k8s workload 对象，每个对象携带服务身份：
- `DeploymentWorkload`（stateless artifact）：`ServiceName=artifact.Name`, `App=artifact.App`（`converter.go:83-97`），k8s 名 `WorkloadName()`（`model.go:34`）。
- `StatefulWorkload`（stateful artifact）：`ServiceName=artifact.Name`, `App=artifact.App`（`converter.go:67-81`），k8s 名 `WorkloadName()`（`model.go:107`）。
- `MongoDBWorkload`（infra）：`ServiceName=infra.Name`, `App=infra.App`（`converter.go:99-116`），k8s 名 `ResourceName()`（`model.go:266`）。

因此 `CheckRollout` 在遍历 workload 时，可凭 workload 对象的 `ServiceName`/`App` 将 k8s rollout 状态归位到具体服务——身份信息已现成，无需额外查询。

### v2 client 与 proto codegen 链路

- proto codegen 由 bazel `go_proto_library`（`projects/infra/deploy/BUILD.bazel:58-75`）驱动，compilers 含 `go_grpc_v2`/`go_proto`/`go_gen_grpc_gateway`/`go_gen_aip`。编辑 `deploy.proto` 后 `bazel build`/`bazel test` 自动重新生成全部桩代码，**无需手动 codegen 步骤**。
- v2 HTTP client（`tools/release/deploy/v2/client/client.go:94`）`GetEnvironment` 直接返回生成的 proto `*deploy.Environment`（`protojson.Unmarshal`，`DiscardUnknown: true`）。**proto 新增字段自动流经 client，无需改 client 代码**。describe（`v3/describe.go:39`）直接消费该 proto 类型。

## 关键决策

### 决策 R1：为 EnvironmentStatus 新增结构化 ServiceStatus，不再依赖 message

**Decision**：在 proto `EnvironmentStatus` 与 domain `EnvironmentStatus` 新增 `repeated ServiceStatus services` 字段，reconciler 逐服务持久化 rollout 状态（state + 可读原因 + 身份），describe 以 per-service 列表为主诊断线；环境级 `message` 降级为次要补充。

**Rationale**:
1. **根因对症**（原则 II）：问题根因是"折叠 + 时序空窗"，而非 describe 展示问题。在数据模型层引入结构化 per-service 状态是对根因的重构式处理。
2. **稳定**：per-service 状态由 reconciler 在 `applyAndWait`（资源已提交即写入初始 `PENDING`）与每次 `checkRollout` 写入，从进入 `WAITING_ROLLOUT` 起即有数据，消除初版的~5s 空窗。
3. **可读**：每个服务一行，直接回答"哪个 service 失败/超时"，优于解析拼接字符串。
4. **向后兼容**：proto3 additive（新增字段+枚举），旧 client 忽略新字段；`message` 保留，非 rollout 原因（apply 失败、retry-exhausted）仍可经其表达。

**Alternatives considered**:
- **备选 R1-A：仅解析现有 message 文本**。否决：message 是非结构化拼接串，解析脆弱；且空窗期 message 为空，解析无源可解。
- **备选 R1-B：新增独立 `ServiceRollout` 顶级资源（REST 子资源）**。否决：过度设计——本需求只在 describe 一个消费点，嵌入 `EnvironmentStatus.services` 复用既有 `GetEnvironment` 一次调用即可，无需新 RPC/资源。

### 决策 R2：proto / domain 字段设计

**Decision**：

proto 新增（`deploy.proto`）：
```proto
message EnvironmentStatus {
  // ...既有 state(1)/message(2)/last_reconcile_time(3)/last_success_time(4)...
  repeated ServiceStatus services = 5 [(google.api.field_behavior) = OUTPUT_ONLY];
}

// ServiceStatus 描述环境内单个服务（artifact 或 infra）的观测 rollout 状态。
message ServiceStatus {
  string name = 1;                     // 服务名（ArtifactSpec.name 或 InfraSpec.name）
  string app = 2;                      // 所属应用
  ServiceKind kind = 3;                // artifact 或 infra
  ServiceRolloutState state = 4;       // 观测到的 rollout 状态
  string message = 5;                  // 详情（如 "可用副本不足（available: 0/1）"）
}

enum ServiceKind {
  SERVICE_KIND_UNSPECIFIED = 0;
  SERVICE_KIND_ARTIFACT = 1;
  SERVICE_KIND_INFRA = 2;
}

enum ServiceRolloutState {
  SERVICE_ROLLOUT_STATE_UNSPECIFIED = 0;
  SERVICE_ROLLOUT_STATE_PENDING = 1;   // 资源已提交，等待首次 rollout 观测
  SERVICE_ROLLOUT_STATE_READY = 2;
  SERVICE_ROLLOUT_STATE_WAITING = 3;
  SERVICE_ROLLOUT_STATE_FAILED = 4;
}
```

domain 新增（`domain/reconcile_types.go` 旁）：`ServiceStatus{Name,App,Kind,State,Message}`、`ServiceKind`（`Artifact`/`Infra`）、`ServiceRolloutState`（`Pending`/`Ready`/`Waiting`/`Failed`）。`domain.EnvironmentStatus` 增 `Services []*ServiceStatus`（指针 slice，依 `style/golang.md:49-55` 容器元素规则，与既有 `[]*ArtifactSpec` 一致）；`domain.RolloutStatus` 增 `Services []*ServiceStatus`（供 `CheckRollout` 回传 per-service）。

**Rationale**:
1. `name`+`app`+`kind` 三元组与 `DesiredState` 的 `ArtifactSpec`/`InfraSpec` 身份对齐，describe 可据其与 desired_state 服务列表归并展示。
2. `PENDING` 态专门表达"已 apply、未观测"，用于 `applyAndWait` 写入初始状态（决策 R4），使 describe 在空窗期也能列出服务。
3. `state` 用枚举（非自由文本），供 describe 稳定映射中文、供未来程序化消费。
4. 字段编号 5 为 `EnvironmentStatus` 下一个空闲号；新 message/enum 取全新字段空间，不冲突。

**Alternatives**: per-service 不带 `kind`（仅 name+app）——否决：infra（mongo）与 artifact 同为 Deployment，需 `kind` 区分服务来源以在 describe 标注 `[artifact]`/`[infra: mongodb]`（与初版服务列表格式一致）。

### 决策 R3：CheckRollout 重构——产出 per-service 列表，不再折叠/early-return

**Decision**：重构 `runtime/k8s/rollout.go:263` `CheckRollout`：
- 遍历 `objects.Deployments`（含 MongoDBWorkloads）与 `objects.StatefulWorkloads`，逐 workload 取 k8s 对象、判定 ready/waiting/failed + 可读原因，构造 `ServiceStatus{name=workload.ServiceName, app=workload.App, kind, state, message}`。
- **不再**遇到首个 failed 即 early-return——收集**全部** workload 的状态。
- 派生环境级 state：任一 failed → `RolloutFailed`；否则任一 waiting → `RolloutWaiting`；否则 `RolloutReady`。
- 派生环境级 message（向后兼容）：拼接非 ready 服务的 message（`"; "` 连接），供 `RolloutStatus.Message`；reconciler 的 `retainWaitingRollout`/`markFailedFromRollout` 现改为**主要持久化 `Services`**（message 仍写，作为次要）。
- 返回 `RolloutStatus{State, Message, Services}`。

**Rationale**:
1. 收集全部服务状态后，失败场景下 describe 能同时显示"哪个 failed"与"其余服务处于什么状态"，信息完整。
2. 派生 env-level state/message 维持 reconcile 状态机语义不变（`checkRollout` 的 switch 仍按 `RolloutReady`/`RolloutFailed`/default 分流，`reconcile.go:153-160`），降低对 reconcile.go 的侵入。
3. `kind` 归位：遍历 workload 对象（而非裸 name 列表）即得 `ServiceName`/`App`，并区分 artifact（Deployment/StatefulWorkload）vs infra（MongoDBWorkload）。

**Alternatives**:
- **备选 R3-A：保留 early-return，仅 failed 服务入 Services**。否决：失败时其余服务状态缺失，用户看不到全貌。
- **备选 R3-B：新增独立 `CheckRolloutDetailed` 方法返回新类型**。否决：双方法增加接口面与测试面；`RolloutStatus` 增字段（`CheckRollout` 签名不变）更简洁，所有 fake runtime（`reconcile_test.go:309`/`command_test.go:149`/`handler_test.go:1794`）零改动（新字段默认 nil）。

### 决策 R4：applyAndWait 写入初始 PENDING Services，消除时序空窗

**Decision**：reconciler `applyAndWait`（`reconcile.go:125-144`）在 `ApplyResources` 成功、转移至 `WAITING_ROLLOUT` 时，**同步写入初始 Services**：据 `env.DesiredState()` 构造每个服务（artifact→`{Kind:Artifact}`，infra→`{Kind:Infra}`）的 `ServiceStatus{State: Pending, Message: "资源已提交，等待观测"}`，随状态转移一并持久化。后续 `checkRollout` 用真实状态覆盖。

**Rationale**:
1. **直接消除初版空窗**：进入 `WAITING_ROLLOUT` 即有 per-service 数据（不再等首个~5s 轮询），describe 在 guitar 短超时场景也能列出环境内的服务集合及其"尚未观测"状态——比初版的空 message 信息量显著提升。
2. **语义诚实**：`ApplyResources` 成功意味着 workload 已提交到 k8s，"已提交、待观测"是准确描述。
3. **构造轻量**：纯 domain 映射（desired_state → Services），不依赖 k8s 客户端，新增一个 domain helper（`buildInitialServiceStatuses(*DesiredState) []*ServiceStatus`）即可。

**Alternatives**:
- **备选 R4-A：apply 后立即在同 cycle 跑一次 CheckRollout**（apply → check 合并）。否决：重构 reconcile 节奏（applyAndWait 语义变为"apply+check"），侵入大且改变 `ProcessResult` 的 RequeueAfter 语义；与"仅加 per-service 状态"的 scope 不符。
- **备选 R4-B：不写初始 Services，接受空窗**。否决：未解决初版根因（时序空窗），per-service 状态在短超时仍为空，违背"替换不稳定的 message"目标。

### 决策 R5：describe 以 per-service 为主线，message 降级为次要

**Decision**：`v3/describe.go` `printEnvironmentDetail` 输出改为：服务列表每项**内联其 rollout 状态/原因**（有 per-service 数据时），例如 `  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）`；环境级 `说明: {message}` 仅在**无 per-service 数据且 message 非空**时输出（避免与 per-service 重复；保留对 apply 失败/retry-exhausted 等非 rollout 原因的表达）。

**Rationale**:
1. per-service 状态是稳定主诊断（决策 R1/R4），应为主线；message 是脆弱的自由文本，降为兜底。
2. 内联到既有服务列表（而非新增独立 section）使"哪个 service 出问题"一目了然，且复用 desired_state 的权威服务集合（per-service 状态据 name+app+kind 归并到列表项）。
3. READY 时 per-service 全为 ready，服务列表显示各服务"就绪"，`说明:` 不再输出（READY 的 message 历来为 "ready"，与状态行冗余）。

**Alternatives**:
- **备选 R5-A：完全移除 message 输出行**。否决：apply 阶段失败（state 仍 RECONCILING，无 Services）、retry-exhausted（FAILED 无 rollout）等场景的 reason 仅存于 message，移除会丢信息。
- **备选 R5-B：新增独立"服务状态:" section，与服务列表分离**。否决：服务列表与状态分离需用户交叉比对 name，体验差；内联更直观。

### 决策 R6：TransitionStatus 对 Services 采用"无条件写入（nil 即清空）"语义

**Decision**：storage `TransitionStatus`（`storage/mongo.go:426-473`）对新增 `Services` 字段采用**与既有"非零才写"不同的专属语义**：每次转移**无条件 `$set` services** 为 `toStatus.Services` 的 BSON 表示（nil → 写空，即清空 stale）。其余既有字段维持"非零才写"。

reconcile 各调用点的 Services 意图（均已核对）：
| 调用点 | Services 意图 |
|--------|---------------|
| `transitionToReconciling`（→RECONCILING） | nil（清空，新一轮 reconcile 作废旧 per-service） |
| `applyAndWait`（→WAITING_ROLLOUT） | 初始 PENDING 列表（决策 R4） |
| `retainWaitingRollout`（→WAITING_ROLLOUT） | checkRollout 真实列表 |
| `markReadyFromRollout`（→READY） | 全部 READY 列表 |
| `markFailedFromRollout`（→FAILED） | checkRollout 真实列表（含失败服务） |
| `transitionToDeleting`（→DELETING） | nil（清空） |
| `MarkRetryExhausted`（→FAILED） | nil（apply 阶段失败，无 rollout 数据） |

**Rationale**:
1. Services 是新字段，无既有代码"依赖其不被触碰"，无条件写入安全。
2. "nil 即清空"语义使 stale per-service 状态（如 FAILED→RECONCILING 重新部署）被显式清除，避免 describe 显示上一轮残留。
3. 既有字段维持"非零才写"不变，零侵入既有行为。

**实现要点**：`mongoStatus` 增 `Services []mongoServiceStatus`（bson tag 无 `omitempty`，确保 nil 序列化为空数组/null 以清空）；`TransitionStatus` 中 `setFields[mongoFieldStatusServices] = servicesToMongo(toStatus.Services)` 无条件执行；`statusToMongo`/`statusFromMongo` 增映射。

**Alternatives**: 对 Services 也用"len>0 才写"——否决：无法清空 stale（FAILED→RECONCILING 时旧失败 Services 残留）。

### 决策 R7：cloneStatus 须深拷贝 Services

**Decision**：`domain/environment.go:369-376` `cloneStatus` 当前为浅拷贝（`cloned := *status`）。新增 `Services []*ServiceStatus` 后，浅拷贝共享底层数组——`cloneStatus` 改为对 `Services` 做深拷贝（指针 slice 逐元素拷值取址，镜像 `cloneArtifacts` 的 `spec := *artifact; cloned[i] = &spec` 模式）。

**Rationale**：`Environment.Status()` 经 `cloneStatus` 用于 `RehydrateEnvironment`（`environment.go:106`）与各处状态快照，共享 slice 会导致并发/后续修改污染。深拷贝维持聚合的不变性。

### 决策 R8：guitar 无代码改动

**Decision**：guitar 侧不改任何代码。初版已落地的 `diagnoseDeployFailure`（shell-out `deploy describe`）天然消费 describe stdout；describe 输出增强（per-service 主线）后，guitar 控制台自动呈现新内容。

**Rationale**：保持 guitar"shell-out 编排器"架构一致性（apply/del/describe 统一经 CLI 契约），避免 guitar 直接 import deploy service client（决策见初版决策 1 备选 A 的否决理由仍成立）。

### 决策 R9（初版保留，已落地）：命令名 describe、非取消 ctx、--timeout=10s、醒目头部、formatState 扩展

以下初版决策**仍有效且已实现**，本次修订不变更：
- 命令名 `describe`（kubectl 语义一致）。
- 诊断用 `context.WithoutCancel(ctx)` + 失败降级（FR-005/FR-006）。
- 诊断显式 `--timeout=10s`（远小于部署超时）。
- `Reporter.DeployDiagnostics` 醒目头部行 `  --- 环境状态 (env=...) ---` + describe 顶格输出。
- `formatState` 扩展 `WAITING_ROLLOUT → 等待滚动发布`（波及 list，已同步测试）。

### 决策 R10：deploy service 大型测试遵循既定例外

**Decision**：deploy service 的代码变更以**单测**为验收门禁（proto 映射、reconcile 各转移路径、storage 往返、CheckRollout per-service 产出、handler 返回、describe 输出）。**不**为 deploy service 新增 testplan——遵循 `projects/infra/deploy/README.md:28` 既定例外（无法自举，不进行大型测试）。端到端冒烟（per-service 在真实 describe 可见）依赖 deploy service 新版经其独立 k8s 部署流程上线 infra.liukexin.com，属后续独立步骤，超出本特性 scope（见 [quickstart.md](./quickstart.md)）。

### 决策 R11：retainWaitingRollout 早退条件须同时比较 Services

**Decision**：`retainWaitingRollout`（`reconcile.go:195-209`）既有"env-level message 相等即早退（不写库）"优化（`reconcile.go:196`）会跳过 `TransitionStatus`，导致 per-service 状态更新不落库。修订：三个 rollout 转移函数（`markReadyFromRollout`/`markFailedFromRollout`/`retainWaitingRollout`）签名从 `message string` 改为整体 `status *domain.RolloutStatus`；`retainWaitingRollout` 早退条件改为 `message 相等 && servicesEqual(env.Status().Services, status.Services)`（新增 domain helper `servicesEqual`，放 `reconcile_types.go`，比较 Name/App/Kind/State/Message），二者均未变化才跳过写入。

**Rationale**：
1. 保留"无变化不写库"的优化（避免每 5s 空写，维持既有 reconcile_test"message 不变不触发写入"语义），同时保证 per-service 状态在 message 文本不变但服务状态变化（如某服务转 READY、其余仍等待且拼接 message 相同）时仍持久化——兑现 plan"每次 checkRollout 用真实状态更新"的承诺与 data-model 不变量。
2. 签名统一传 `*domain.RolloutStatus` 使三个转移函数同时获得 `Message` 与 `Services`，`checkRollout` 的 switch 分流语义不变。

**Alternatives**：
- **备选 R11-A：无条件写入（移除 message 相等早退）**。否决：改变既有无变化不写行为，且既有"message 不变不触发写入"断言需删除；比较 Services 语义更精确。
- **备选 R11-B：仅比较 Services（忽略 message）**。否决：message 变化而 Services 未变（如 apply 阶段 message 更新）时也应写库，须同时比较二者。

## 结论

所有 NEEDS CLARIFICATION 已解决：数据模型（结构化 `ServiceStatus`，决策 R1/R2）、runtime 产出方式（CheckRollout 重构，R3）、时序空窗消除（applyAndWait 写初始 PENDING，R4）、describe 展示（per-service 主线 + message 次要，R5）、storage 写入语义（无条件写/nil 清空，R6）、深拷贝（R7）、guitar 无改动（R8）、deploy service 大型测试例外（R10）。可进入 Phase 1 设计实体模型与契约。
