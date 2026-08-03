---

description: "Task list for 032-guitar-deploy-failure-state"
---

# Tasks: Guitar Deploy Failure Environment Diagnostics

**Input**: Design documents from `/specs/032-guitar-deploy-failure-state/`（plan.md、spec.md、research.md、data-model.md、contracts/、quickstart.md）

**Prerequisites**: plan.md（必读）、spec.md（必读，用户故事来源）、research.md、data-model.md、contracts/、quickstart.md

**Tests**: 依宪法原则 IV，编译与单测是代码开发任务的一部分、**不单独分配 task**——每个实现 task 自带单测编写与 `bazel test` 验证。下文不再单列测试 task。

**Organization**: 本特性仅 1 个用户故事（US1，P1：部署失败时打印环境状态）。实现拆为两段：① 基础（deploy `describe` 命令，作为数据源契约）；② US1（guitar 消费 describe、在部署失败时打印诊断）。MVP = 基础 + US1 两段合计。

> **初版 Phase 1–3（T001–T008）已实现并提交**（依赖环境级 `message`，实测 5s 超时 `message` 为空、无法指认服务，详见 [research.md](./research.md) 背景）。
> **修订范围（Phase 4–6，T009–T018，2026-08-03 规划）**：为 deploy service 新增结构化 per-service 状态（proto→domain→runtime→reconcile→storage→handler），并修订 `deploy describe` 以 per-service 状态为主线；guitar **无代码改动**（shell-out 消费 describe，输出增强自动生效）。代码现状已核实：初版 T001–T008 实现均在代码中（`apply.go:248` formatState、`describe.go`、`main.go` 注册、`reporter.go:83`、`run.go:175` diagnoseDeployFailure）；per-service 状态尚未实现（proto/domain/runtime 均无 `Services`）。

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 仅用户故事阶段标注（US1）；基础/Polish 阶段不标
- 描述含精确文件路径

## 文档阅读门禁（宪法原则 V）

每个 phase 起始处的「文档清单」为强制阅读门禁，编码前 MUST 完整阅读。清单按「代码规范文档 / 官方文档 / 技术文章」三分类组织。`AGENTS.md` 与本 feature 的 spec/plan/contracts/data-model/research/quickstart 为代码开发必读内容，不在此重复列出。

---

## Phase 1: Foundational — deploy `describe` 命令（阻塞前置）

**Purpose**: 实现 `deploy describe` CLI 契约（数据源），US1 依赖之。同时扩展 `formatState` 覆盖 `WAITING_ROLLOUT`。

**⚠️ CRITICAL**: US1（guitar 集成）必须等本 phase 完成（describe 命令可用 + formatState 扩展）方可开始。

### 文档清单

- **代码规范文档**：`style/golang.md`（Go 风格：导入三级分组、指针语义、注释、表驱动测试 given/when/then、命名 `TestFuncName`/`Test_funcName`、单测禁外部依赖、test target 用 gazelle 默认名）；及其引用的 [Google Go Style Guide（入口）](https://google.github.io/styleguide/go/) 三份——[Style Guide](https://google.github.io/styleguide/go/guide)（golang.md 标注"必读"）、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**：无（describe 命令镜像 deploy v3 既有 `del.go`/`list.go`/`main.go` 模式与既有 `GetEnvironment` client，不引入新第三方 API）
- **技术文章**：无

### Tasks

- [X] T001 扩展 `formatState` 在 `tools/release/deploy/v3/apply.go:236`：新增 `case deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT: return "等待滚动发布"`；并在 `tools/release/deploy/v3/del_list_test.go` 的 `TestListCommand` 增补一条 WAITING_ROLLOUT 环境断言（输出 `\t等待滚动发布`）。验证：`bazel test //tools/release/deploy/v3:deploy_test`。（依 research.md 决策 R9；该扩展波及 list 输出，须同步更新其测试）
- [X] T002 新增 `tools/release/deploy/v3/describe.go`：实现 `describeCommand(ctx, opts)`——镜像 `del.go`（`workspace.MustRoot`→`loadConfig` 取默认 scope→`NewFullEnvName`→`ParseFullEnvName`→`environmentResourceName`），调 `opts.apiClient.GetEnvironment(ctx, name)`（`tools/release/deploy/v2/client/client.go:94`），按 [data-model.md](./data-model.md)「输出模型」与 [contracts/deploy-describe.md](./contracts/deploy-describe.md) 打印（环境名/状态 via `formatState`——返回空串（UNSPECIFIED）时输出 `未知`/说明/服务列表来自 `desired_state.artifacts[]`+`infras[]`/最近调和/最近成功）；`ErrNotFound` 时打印 `环境 {fullEnvName} 不存在` 并返回非零错误。同时新增 `tools/release/deploy/v3/describe_test.go`：httptest 桩（镜像 `del_list_test.go` 的 `writeDelListJSONResponse`/`clientpkg.NewClient`），表驱动覆盖 FAILED（含 message）、READY、PENDING、WAITING_ROLLOUT、DELETING（→ `删除中`）、UNSPECIFIED（→ `未知`）、服务列表（artifact+infra）、时间戳 nil/非 nil、`ErrNotFound`。验证：`bazel test //tools/release/deploy/v3:deploy_test`。（依赖 T001 的 formatState）
- [X] T003 在 `tools/release/deploy/v3/main.go` 注册 `describe` 命令：新增 `commandDescribe="describe"` 常量；`commandExecTable`（main.go:54）加 `describe→describeCommand`；`commandValidatorTable`（main.go:61）加 `describe→validateDescribeOptions`（target 非空，镜像 `validateDelOptions` main.go:232）；`commandFlagTable`（main.go:111）加 `describe:{flagEndpoint,flagTimeout,flagScope,flagVerbose}`；`usageText()`（main.go:266）增补 `describe` 行。运行 `bazel run //:gazelle tools/release/deploy/v3`（将 `describe.go` 加入 `deploy_lib.srcs`、`describe_test.go` 加入 `deploy_test.srcs`）。验证：`bazel build //tools/release/deploy/v3:deploy_v3` 与 `bazel test //tools/release/deploy/v3:deploy_test`，并手跑 `bazel run //tools/release/deploy/v3:deploy_v3 -- describe --help` 确认 usage 含 describe。（依赖 T002）

**Checkpoint**: `deploy describe <env>` 可用，`deploy_test` 全绿——US1 可开始消费该契约。

---

## Phase 2: User Story 1 — guitar 部署失败诊断集成 (Priority: P1) 🎯 MVP

**Goal**: `guitar run` 在 suite 部署不成功时，于上报错误前打印醒目头部 + 顶格 `deploy describe` 输出（环境状态/失败原因/服务列表），describe 失败降级、不改原始错误、不影响 cleanup。

**Independent Test**: 构造部署会失败的 suite（引用无法就绪的 service 或 `--timeout` 极短），运行 `guitar run <plan.yaml>`，观察部署失败后是否打印诊断信息；另构造健康 suite 确认成功路径无变化（见 [quickstart.md](./quickstart.md) 验证 2/3）。

### 文档清单

- **代码规范文档**：`style/golang.md`；及 [Google Go Style Guide — Style Guide](https://google.github.io/styleguide/go/guide)（必读）、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**：无（guitar 复用既有 `runCommand` 包级变量与 `Reporter` 模式，不引入新依赖）
- **技术文章**：无

### Tasks

- [X] T004 [P] [US1] 在 `tools/test/guitar/pkg/run/reporter.go` 新增方法 `(*Reporter) DeployDiagnostics(envName string)`：打印单行 `  --- 环境状态 (env=%s) ---`（2 空格缩进 + `---` 分隔包裹，**不着色**，与既有 `Step`/`SuiteHeader` 一致）；在 `tools/test/guitar/pkg/run/run_test.go` 的 `TestReporter` 增补该方法的输出断言。验证：`bazel test //tools/test/guitar/pkg/run:run_test`。（依 [contracts/guitar-integration.md](./contracts/guitar-integration.md) Reporter 契约 + 决策 6）
- [X] T005 [US1] 在 `tools/test/guitar/pkg/run/run.go` 新增常量 `deployDescribeCommand = "describe"`（与 `deployApplyCommand`/`deployDeleteCommand` 同区，run.go:21-28）；新增诊断 helper（封装 `r.DeployDiagnostics(fullEnvName)` + `runCommand(context.WithoutCancel(ctx), deployBinary, deployDescribeCommand, "--timeout=10s", fullEnvName)`，describe 失败时 `fmt.Fprintf(stderr, "warning: 获取环境 %s 状态失败: %v\n", ...)` 不改返回错误）；在 `runSuite`（run.go:150-153）的 deploy apply 错误分支调用该 helper（return 之前，cleanup defer 之前）。在 `run_test.go` 扩展 "deploy failure" 用例：用可取消外层 ctx，apply 调用时 `cancel()` 并返回 `context.DeadlineExceeded`；断言调用顺序 `apply→describe（含 --timeout=10s 与 fullEnvName）→del`、describe 收到的 `ctx.Err()==nil`（非取消）、`Run` 返回错误含 "deploy apply"（原始错误保留）。诊断触发点在 `runSuite` 内，spec.md Edge Cases 的 `--suite` 过滤场景由同一代码路径天然覆盖，无需额外用例。验证：`bazel test //tools/test/guitar/pkg/run:run_test`。（依赖 T004；依 [contracts/guitar-integration.md](./contracts/guitar-integration.md) 调用契约+测试断言手法+决策 5）

**Checkpoint**: `guitar run` 部署失败打印环境状态诊断，成功路径无变化，`run_test` 全绿——用户故事 1 独立可验证。

---

## Phase 3: Polish & 交叉关注

**Purpose**: 文档同步与全量验证。

### 文档清单

- **代码规范文档**：无
- **官方文档**：无
- **技术文章**：无

### Tasks

- [X] T006 [P] 更新 `tools/release/deploy/README.md`：在「命令」小节（apply/del/list/scope 旁）增补 `deploy describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>` 用法与说明。
- [X] T007 [P] 更新 `tools/test/guitar/README.md`：在「输出格式」小节说明部署不成功时附加的「环境状态」诊断输出（醒目头部 + `deploy describe` 顶格文本），并备注 describe 失败降级。
- [X] T008 全量验证门禁：运行 `bazel build //tools/release/deploy/v3:deploy_v3 //tools/test/guitar/cmd:guitar` 与 `bazel test //tools/release/deploy/v3:deploy_test //tools/test/guitar/pkg/run:run_test` 全绿；随后按 [quickstart.md](./quickstart.md) 执行端到端验证 1（`deploy describe <env>` 单命令，需可访问 deploy service）、验证 2（失败路径核心场景，**必做**——优先采用构造成本最低的场景 A：suite `timeout` 或全局 `--timeout` 设极短（如 `--timeout=20s`）触发超时；仅当环境确无法构造失败场景时方可豁免，且须在任务完成记录中说明原因）、验证 3（成功路径回归），并执行「边界验证」三例（非 TTY 无 ANSI 码、环境创建前失败、describe 自身失败降级），确认失败路径诊断输出符合预期。

---

## Dependencies & Execution Order（初版，已落地）

### Phase 依赖

- **Phase 1（Foundational）**：无前置依赖，可立即开始；**阻塞** Phase 2。
- **Phase 2（US1）**：依赖 Phase 1 完成（describe 命令 + formatState 扩展）。
- **Phase 3（Polish）**：依赖 Phase 1 + Phase 2 完成。

### Task 级依赖

- T001 → T002（describe 调 formatState）→ T003（注册依赖 describe.go）
- T004 → T005（run.go 调 DeployDiagnostics）
- T006 ∥ T007（不同 README，可并行）；T008 依赖全部实现完成

### 并行机会

- T006、T007 可并行（不同文件）
- T004 与 Phase 1 的 T001 理论上可并行（不同工具、不同文件），但因 Phase 1 是 Phase 2 前置，建议顺序执行

---

## 并行示例：Phase 3

```bash
# 两个 README 更新可同时启动：
Task: "T006 更新 tools/release/deploy/README.md 命令小节"
Task: "T007 更新 tools/test/guitar/README.md 输出格式小节"
```

---

## Implementation Strategy（初版，已落地）

### MVP First（Phase 1 + Phase 2）

1. 完成 Phase 1：deploy `describe` 命令 + formatState 扩展 → `deploy describe` 可独立工作
2. 完成 Phase 2：guitar 部署失败诊断集成 → 端到端闭环（部署失败→打印诊断→cleanup 不变）
3. **STOP and VALIDATE**：按 quickstart.md 验证 2（失败路径）与验证 3（成功路径回归）独立验证
4. Phase 3 文档同步与全量门禁

### 增量交付

- Phase 1 完成：`deploy describe` 即为可独立使用的能力（人工排查可直接受益）
- Phase 2 完成：用户故事 1 全量交付（guitar 自动诊断）
- Phase 3 完成：文档与全量回归验证闭环

---

## Notes（初版，已落地）

- 依宪法原则 IV，编译 + 单测内嵌于各实现 task（T001/T002/T005 自带单测与 `bazel test`），无独立测试 task
- [P] task = 不同文件、无未完成依赖
- 每个实现 task 完成后即 `bazel build`+`bazel test` 对应 target（小颗粒度反馈环）
- 依宪法原则 VI，本特性为 CLI 工具改动（非服务型应用），大型测试不作强制门禁；端到端验证见 quickstart.md（需 deploy service 访问）
- 改动不涉及 deploy service、不改 deploy apply/del 既有行为（仅扩展 formatState 的 list 副作用已由 T001 测试覆盖）

## T008 端到端验证执行记录（2026-08-02）

测试内容：`experimental/ts/grpc_hello_world/testplan/interface_test.yaml`（deploy.yaml 含 service + gateway 两个 artifact，env `liukexin.{{run}}`）。

- **编译 + 单测门禁**：`bazel build //tools/release/deploy/v3:deploy_v3 //tools/test/guitar/cmd:guitar` 通过；`bazel test //tools/release/deploy/v3:deploy_test //tools/test/guitar/pkg/run:run_test` 全绿（2/2 PASSED）。
- **验证 1（describe 单命令）**：`deploy describe liukexin.d032desc`（READY 实环境）输出 `状态: 就绪` / `说明: ready` / 服务列表 / `最近调和: -` / `最近成功: 2026-08-02T13:12:26Z`，退出码 0；`deploy describe liukexin.no032a` → `环境 liukexin.no032a 不存在`，退出码 1。
- **验证 2（失败路径核心场景，scenario A 短超时，必做）**：`guitar run ... --timeout=5s` → deploy apply 被杀（env 已创建、滚动发布中）→ 打印 `  --- 环境状态 (env=liukexin.ltdn8qn2) ---` 头部 + 顶格 describe（`状态: 等待滚动发布`、`服务:` 列出 service+gateway、时间戳 `-`）→ `Cleanup` 执行（env 已删除）→ 原始错误保留 `deploy apply ...: signal: killed`。满足 FR-001/FR-002/FR-004/FR-006/FR-007、SC-002/SC-003/SC-004。
- **验证 3（成功路径回归）**：`guitar run ... --timeout=20s` → deploy 成功（就绪）→ test PASS → cleanup，**无**诊断头部。满足 FR-008/SC-005。
- **边界 1（非 TTY 无 ANSI）**：验证 2 输出经 `| tee` 管道，grep `\x1b\[` 无命中。满足 FR-009。
- **边界 2（环境创建前失败）**：`guitar run ... --timeout=2s` → deploy apply 被杀（env 未创建）→ describe 返回 not-found（stdout `环境 ... 不存在`）+ stderr 降级 `warning: 获取环境 ... 状态失败`，原始错误保留、不崩溃、cleanup 仍尝试。满足 FR-005/FR-006。
- **边界 3（describe 自身失败降级）**：边界 2 已实测降级机制（describe 非零退出 → warning）；单测 `TestRun/deploy_failure_with_describe_degradation` 精确覆盖。
- **FAILED 终态渲染**（SC-001）未单独构造实环境（scenario B），由 `describe` 的 message 字段渲染已验证（READY 显示 `说明: ready`）+ 单测 `TestDescribeCommand/failed_with_message_and_services` 覆盖 FAILED+message 渲染。

---

# 修订范围：deploy service per-service 状态（2026-08-03 规划）

**背景**：初版依赖环境级 `message` 承载失败/等待原因；实测 `--timeout=5s` 场景 `message` 为空、describe 无法指认哪个 service（根因：`runtime/k8s/rollout.go:263` `CheckRollout` 将各 workload 状态 `strings.Join("; ")` 折叠为单一环境级字符串，且仅在每 5s 的 `checkRollout` 后填充，首次轮询前为空——详见 [research.md](./research.md) 背景）。修订方案：① deploy service 新增结构化 per-service 状态（reconciler 在 `applyAndWait` 即写入初始 PENDING、`checkRollout` 写真实状态），不再折叠；② `deploy describe` 以 per-service 状态为主线。决策见 [research.md](./research.md) R1~R11、契约见 [contracts/environment-status.md](./contracts/environment-status.md) / [contracts/deploy-describe.md](./contracts/deploy-describe.md)。

> 初版 Phase 1–3 Notes 中"改动不涉及 deploy service"仅适用于初版；以下 Phase 4–6 显式改动 deploy service。

---

## Phase 4: Foundational（修订）— deploy service per-service 状态（阻塞前置）

**Purpose**: 为 `Environment.status` 新增结构化 per-service rollout 状态（proto → domain → runtime → reconcile → storage → handler 全链路），使 Phase 5 的 describe 能稳定展示每个服务的状态与原因。

**⚠️ CRITICAL**: Phase 5（describe per-service 主线）须等本 phase 完成（proto 新字段 + handler 映射就绪）方可端到端验证。

### 文档清单

- **代码规范文档**：
  - `style/golang.md`（导入三级分组；**容器元素强制 `[]*T`**（line 49-55）、深拷贝仅在需要时、`new` 构造无初始化指针对象、注释、单测表驱动 given/when/then + 命名 + 禁外部依赖 + target 用 gazelle 默认名）；及其引用的 [Google Go Style Guide（入口）](https://google.github.io/styleguide/go/)——[Style Guide](https://google.github.io/styleguide/go/guide)（必读）、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
  - `style/api.md`（REST+gRPC、google.api 注解、proto 规范索引）；及其引用的相关 AIP——[AIP-126 Enumerations](https://google.aip.dev/126)（`UPPER_SNAKE_CASE`、`_UNSPECIFIED=0`、包级枚举值前缀）、[AIP-144 Repeated fields](https://google.aip.dev/144)（复数名、宜用 message）、[AIP-203 Field behavior](https://google.aip.dev/203)（`OUTPUT_ONLY`、新增字段向后兼容、nested 行为独立）——适用于 T009
  - `style/mongo.md`（field 为对象时定义具体模型**不用 `bson.M`**、`_id` 自动生成）——适用于 T013
- **官方文档**：无（proto 变更为 additive，镜像 `projects/infra/deploy/deploy.proto` 既有模式；runtime 复用既有 k8s client-go；storage 复用既有 mongo-driver——均不引入新第三方 API）
- **技术文章**：无

### Tasks

- [X] T009 [P] 在 `projects/infra/deploy/deploy.proto` 新增 per-service 状态定义（依 [contracts/environment-status.md](./contracts/environment-status.md) proto 契约）：① 在 `EnvironmentStatus`（proto:158-166）新增 `repeated ServiceStatus services = 5 [(google.api.field_behavior) = OUTPUT_ONLY];`；② 新增 `message ServiceStatus { string name=1; string app=2; ServiceKind kind=3; ServiceRolloutState state=4; string message=5; }`（字段注释，对齐既有 message 风格）；③ 新增包级 enum `ServiceKind`（`SERVICE_KIND_UNSPECIFIED=0`/`SERVICE_KIND_ARTIFACT=1`/`SERVICE_KIND_INFRA=2`）与 `ServiceRolloutState`（`SERVICE_ROLLOUT_STATE_UNSPECIFIED=0`/`..._PENDING=1`/`..._READY=2`/`..._WAITING=3`/`..._FAILED=4`），值名前缀加枚举名（AIP-126 包级规则）；布局对齐既有 `EnvironmentState`/`ArtifactSpec`。`ServiceStatus` 子字段不重复标 `field_behavior`（对齐 `ArtifactSpec`/`InfraSpec`，AIP-203 nested 独立）。完成后 `bazel build //projects/infra/deploy:deploy`（触发 `go_proto_library` 重生成）通过，确认生成 Go 包含 `ServiceStatus`/`ServiceKind`/`ServiceRolloutState`。
- [X] T010 [P] 在 `projects/infra/deploy/domain/` 新增 per-service domain 类型与扩展：① `domain/reconcile_types.go` 新增 `ServiceKind`（`Unspecified`/`Artifact`/`Infra`）、`ServiceRolloutState`（`Unspecified`/`Pending`/`Ready`/`Waiting`/`Failed`）int 枚举（各带 `String()`，对齐 `domain/state.go:53`）与 `ServiceStatus{Name,App string; Kind ServiceKind; State ServiceRolloutState; Message string}`；在 `RolloutStatus`（reconcile_types.go:18-23）新增 `Services []*ServiceStatus`。② `domain/environment.go`：`EnvironmentStatus`（environment.go:46-53）新增 `Services []*ServiceStatus`（指针 slice，`style/golang.md:49-55`）；`cloneStatus`（environment.go:369-376）改为对 `Services` 深拷贝（`make([]*ServiceStatus, len)` + 逐元素 `cp := *s; cloned[i] = &cp`，镜像 `cloneArtifacts` environment.go:383-411）；新增 `BuildInitialServiceStatuses(ds *DesiredState) []*ServiceStatus`（artifact→`{Kind:Artifact,State:Pending,Message:"资源已提交，等待观测"}`、infra→`{Kind:Infra,...}`，先 artifacts 后 infras，nil/空返回 nil）。③ 单测：`TestBuildInitialServiceStatuses`、`Test_cloneStatus_services_deep_copy`、枚举 `String()`。验证：`bazel test //projects/infra/deploy/domain:domain_test`。（与 T009 不同包、可并行）
- [X] T011 在 `projects/infra/deploy/runtime/k8s/rollout.go` 重构 `CheckRollout`（rollout.go:263-336，依 [contracts/environment-status.md](./contracts/environment-status.md) runtime 契约 + research.md 决策 R3）：遍历 `objects.Deployments`+`objects.MongoDBWorkloads`+`objects.StatefulWorkloads`（经 `ConvertToWorkloads`），逐 workload 取 k8s 对象判定状态，构造 `domain.ServiceStatus{name=workload.ServiceName, app=workload.App, kind, state, message}`——Deployment（artifact `kind=Artifact`）据 `isDeploymentReady`/`isDeploymentFailed` + `deploymentNotReadyMessage`/`deploymentFailureMessage`；MongoDB（infra `kind=Infra`，名取 `ResourceName()`）同 Deployment；StatefulSet（artifact）据 `isStatefulSetReady` + `statefulSetNotReadyMessage`。**不再** early-return on first failed（rollout.go:306 现状）——收集**全部**。派生 env-level `State`（任一 FAILED→Failed；否则任一非 READY→Waiting；否则 Ready）+ `Message`（拼接非 READY message）；返回 `RolloutStatus{State, Message, Services}`。更新 `runtime/k8s/rollout_test.go` `TestCheckRollout`（rollout_test.go:451）：断言 `got.Services` 全部服务 + 各 state/message；新增"一 failed + 一 waiting"用例（两者均在 `Services`，env-level=Failed）。验证：`bazel test //projects/infra/deploy/runtime/k8s:k8s_test`。（依赖 T010）
- [X] T012 在 `projects/infra/deploy/service/reconcile.go` 各 `repo.TransitionStatus` 调用点持久化 Services（research.md 决策 R4/R6/R11 表格 + [contracts/environment-status.md](./contracts/environment-status.md) 写入语义）：① `applyAndWait`（reconcile.go:125-144）RECONCILING→WAITING_ROLLOUT 增 `Services: domain.BuildInitialServiceStatuses(env.DesiredState())`（初始 PENDING，消除时序空窗）；②③④ **签名重构**：`markReadyFromRollout`（reconcile.go:164-177）/`markFailedFromRollout`（reconcile.go:180-191）/`retainWaitingRollout`（reconcile.go:195-209）的 `message string` 参数改为整体 `status *domain.RolloutStatus`，`checkRollout`（reconcile.go:153-160）switch 三分支随之传 `status`——markReady 写 `Message: "ready"`（既有硬编码）+ `Services: status.Services`（全 READY）；markFailed 写 `Message: status.Message` + `Services: status.Services`（含失败）；retainWaitingRollout 的**早退条件**由 `env.Status().Message == message` 改为 `env.Status().Message == status.Message && domain.ServicesEqual(env.Status().Services, status.Services)`（新增 domain helper `ServicesEqual`，放 `domain/reconcile_types.go`，比较 Name/App/Kind/State/Message；二者均未变化才跳过写入，避免 message 不变时 per-service 更新不落库，决策 R11）；⑤ `transitionToReconciling`/`transitionToDeleting`/`MarkRetryExhausted` 显式 `Services: nil`（清空 stale）。扩展 `service/reconcile_test.go`：`checkRolloutFn`（reconcile_test.go:290）返回携带 `Services`，断言 `fakeRepository.TransitionStatus` 收到的 `toStatus.Services` 一致；**新增用例**：message 不变但 Services 变化（如某服务 PENDING→READY 而其余等待、拼接 message 相同）时仍触发写入。验证：`bazel test //projects/infra/deploy/service:service_test`。（依赖 T010、T011）
- [X] T013 [P] 在 `projects/infra/deploy/storage/mongo.go` 持久化 Services（research.md 决策 R6 + style/mongo.md）：① 新增具体 struct `mongoServiceStatus{Name,App string; Kind,State int; Message string}`（bson tag，**不用 `bson.M`**）；`mongoStatus`（mongo.go:196-203）增 `Services []mongoServiceStatus`（bson `services`，**无 `omitempty`**）；② `TransitionStatus`（mongo.go:426-473）`setFields` 中**无条件**置 `mongoFieldStatusServices: servicesToMongo(toStatus.Services)`（nil→空→清空；新增常量 `mongoFieldStatusServices="status.services"`）；③ `statusToMongo`/`statusFromMongo`（mongo.go:633-645,797-808）增 `Services` 双向映射。④ 单测 `storage/mongo_test.go`：往返一致、清空语义、旧文档缺字段零值。验证：`bazel test //projects/infra/deploy/storage:storage_test`。（依赖 T010；MongoDB schemaless 无 migration）
- [X] T014 [P] 在 `projects/infra/deploy/handler.go` 增 proto↔domain Services 映射：① `toProtoStatus`（handler.go:420-430）增 `Services: toProtoServices(statusValue.Services)`；② 新增 `toProtoServices([]*domain.ServiceStatus) []*ServiceStatus`（镜像 `toProtoArtifacts` handler.go:432-455）+ 枚举映射 `serviceKindToProto`/`serviceRolloutStateToProto`（对齐 `toProtoState` handler.go:344-361）。③ 单测 `handler_test.go`：`GetEnvironment` 返回 `status.services` 与 domain 一致（枚举映射、透传、nil→空）。验证：`bazel test //projects/infra/deploy:deploy_test`。（依赖 T009 proto 类型、T010 domain 类型）

**Checkpoint**: deploy service 全链路支持 per-service 状态产出与持久化，`bazel test //projects/infra/deploy/...` 全绿——Phase 5 describe 可消费 `Environment.status.services`。

---

## Phase 5: User Story 1（修订）— describe per-service 状态主线 (Priority: P1)

**Goal**: `deploy describe` 输出以 per-service rollout 状态为主线（每服务一行内联 就绪/等待发布/失败/已提交 + 原因），环境级 `message` 降级为次要（仅当无 per-service 数据且 message 非空时输出）。guitar 经 shell-out 自动呈现、**无需改 guitar**。

**Independent Test**: 单测 `describe_test.go` 覆盖 per-service 文本内联与 message 降级规则（确定性，不依赖远端）；端到端冒烟（可选，需 deploy service 新版上线）见 [quickstart.md](./quickstart.md) 场景 A/B/C。

### 文档清单

- **代码规范文档**：`style/golang.md`（同 Phase 4 Go 风格要点）；及 [Google Go Style Guide — Style Guide](https://google.github.io/styleguide/go/guide)（必读）、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**：无（describe 复用既有 `GetEnvironment` proto 与 v2 client，仅改 `printEnvironmentDetail` 格式化）
- **技术文章**：无

### Tasks

- [X] T015 [US1] 修订 `tools/release/deploy/v3/describe.go` `printEnvironmentDetail`（describe.go:53-84，依 [contracts/deploy-describe.md](./contracts/deploy-describe.md) + research.md 决策 R5）：① 服务列表项内联 per-service 状态——遍历 desired_state 的 artifacts 段（kind=artifact）与 infras 段（kind=infra:resource）时，按 `name`+`app`+`kind` 三元组在 `environment.Status.Services` 查找匹配项，匹配则项尾追加：`READY`→` 就绪`、`WAITING`→` 等待发布: {service.message}`、`FAILED`→` 失败: {service.message}`、`PENDING`→` 已提交，等待观测`、`UNSPECIFIED`/无匹配→不追加（兼容旧版服务端）；② `说明:` 行改为**仅当 `len(Status.Services)==0` 且 `Message!=""`** 时输出。修订 `describe_test.go`：表驱动覆盖 READY（各 ` 就绪`、无 `说明:`）、WAITING（` 等待发布:...`、无 `说明:`）、FAILED（` 失败:...`、无 `说明:`）、PENDING（` 已提交，等待观测`）、无 per-service+message 非空（输出 `说明:`）、无 per-service+message 空（无 `说明:`）。验证：`bazel test //tools/release/deploy/v3:deploy_test`。（依赖 Phase 4：至少 T009 proto 类型；端到端需 T014 handler 映射）

**Checkpoint**: `deploy describe` 以 per-service 为主线，`deploy_test` 全绿——用户故事 1（判断哪个 service 失败/超时）独立可验证。

---

## Phase 6: Polish（修订）— 文档同步与全量验证

**Purpose**: 文档同步（describe/guitar 输出示例）与全量验证。

### 文档清单

- **代码规范文档**：无（README 文档更新 + 验证，无代码风格适用）
- **官方文档**：无
- **技术文章**：无

### Tasks

- [ ] T016 [P] 更新 `tools/release/deploy/README.md`：将初版 `describe` 小节输出示例改为 per-service 主线（服务项内联 就绪/等待发布/失败/已提交），说明 per-service 状态来自 `Environment.status.services`、`说明:` 仅在无 per-service 数据时输出。
- [ ] T017 [P] 更新 `tools/test/guitar/README.md`：在初版「部署失败环境状态诊断」小节补充诊断输出含 per-service 状态（哪个服务等待/失败 + 原因），备注 `applyAndWait` 即写初始 PENDING、短超时亦能列出服务（消除初版时序空窗）。
- [ ] T018 全量验证门禁：① `bazel build //projects/infra/deploy/... //tools/release/deploy/v3:deploy_v3 //tools/test/guitar/cmd:guitar` 与 `bazel test //projects/infra/deploy/domain:domain_test //projects/infra/deploy/service:service_test //projects/infra/deploy/storage:storage_test //projects/infra/deploy/runtime/k8s:k8s_test //projects/infra/deploy:deploy_test //tools/release/deploy/v3:deploy_test //tools/test/guitar/pkg/run:run_test` 全绿（deploy service 不进行大型测试，见 `projects/infra/deploy/README.md:28`，单测为权威验收）。② 按 [quickstart.md](./quickstart.md) 第二部分端到端冒烟（**可选/非阻塞**——依赖 deploy service 新版经独立 k8s 流程上线 infra.liukexin.com）：场景 A（`--timeout=5s` 确认 per-service 可见）、B（成功路径无诊断）、C（standalone describe READY 各 ` 就绪`）、D（环境不存在降级）；远端旧版则跳过并记录，单测（①）仍为通过门禁。

**Checkpoint**: 文档同步、全量单测全绿；端到端冒烟视部署前置决定执行或记录跳过。

---

## Dependencies & Execution Order（修订范围）

### Phase 依赖

- **Phase 4（Foundational 修订）**：无前置依赖（初版 Phase 1–3 已完成），可立即开始；**阻塞** Phase 5。
- **Phase 5（US1 修订）**：依赖 Phase 4 完成。
- **Phase 6（Polish 修订）**：依赖 Phase 4 + Phase 5 完成。

### Task 级依赖

- T009（proto） ∥ T010（domain）：不同包、无编译依赖，可并行
- T011（runtime/k8s） ← T010
- T012（reconcile） ← T010、T011
- T013（storage） ← T010
- T014（handler） ← T009、T010
- T015（describe） ← Phase 4（至少 T009；端到端需 T014）
- T016 ∥ T017（不同 README，可并行）；T016/T017 ← T015
- T018 ← 全部修订 task 完成

### 推荐执行序（单实现者，review 友好）

T009 ∥ T010 → T011 → T012 → T013 ∥ T014 → T015 → T016 ∥ T017 → T018

### 并行机会

- T009、T010 可并行（Phase 4 起始）
- T013、T014 可并行（均依赖 T010；T014 另依赖 T009）
- T016、T017 可并行（Phase 6，不同 README）

---

## Implementation Strategy（修订范围）

### MVP First（Phase 4 + Phase 5）

1. 完成 Phase 4：deploy service per-service 状态全链路 → `Environment.status.services` 可产出与持久化
2. 完成 Phase 5：describe per-service 主线 → describe 稳定展示"哪个 service 失败/超时"
3. **STOP and VALIDATE**：`bazel test //projects/infra/deploy/... //tools/release/deploy/v3:deploy_test` 全绿（单测为权威验收）；端到端冒烟视 deploy service 新版上线前置决定
4. Phase 6 文档同步与全量门禁

### 关于 deploy service 部署与端到端验证

deploy service 因无法自举（`projects/infra/deploy/README.md:24`）禁止用 `deploy`/guitar/testplan 部署自身，经其独立 k8s 流程上线。故 Phase 5/6 的端到端冒烟须在 deploy service 新版上线 infra.liukexin.com 后进行——属后续独立步骤，不阻塞本特性单测验收。

---

## Notes（修订范围补充）

- 依宪法原则 IV，编译 + 单测内嵌于各实现 task（T009~T015 自带单测与 `bazel test`），无独立测试 task
- 依宪法原则 VI + `projects/infra/deploy/README.md:28`，deploy service 不进行大型测试，单测为权威验收；端到端冒烟为可选非阻塞
- proto 变更为 proto3 additive（新增字段+枚举，不删改已有编号），向后兼容（AIP-180/AIP-203）；旧版服务端不返回 `services` 时 describe 回退初版纯服务列表
- v2 HTTP client（`tools/release/deploy/v2/client/client.go:94`）直接返回 proto `*deploy.Environment`，新字段自动流经、无需改 client
- guitar **无代码改动**（初版 shell-out 链路不变，describe 输出增强自动生效）
- 容器元素用指针 slice `[]*ServiceStatus`（`style/golang.md:49-55`，与既有 `[]*ArtifactSpec`/`[]*InfraSpec` 一致）
