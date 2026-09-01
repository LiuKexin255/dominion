# Tasks: Deploy Health 探针支持

**Input**: Design documents from `/specs/052-deploy-health-probe/`

**Prerequisites**: plan.md（必需）、spec.md（用户故事）、research.md、data-model.md、contracts/、quickstart.md — 均位于 `specs/052-deploy-health-probe/`。

**Tests**: 单测内嵌于各开发任务（宪法 IV：编译+单测为代码变更的一部分，不单列）；大型测试为独立验收任务（宪法 VI：必须实际执行 `guitar run`，全部 case 通过）。

**Organization**: 按 user story 分组。US1=deploy 探针（P1）、US2=bootstrap health（P2）、US3=deploy 行为保持验证（P3）。Go bootstrap health 同时服务 US1 验证与 US2 交付，置于 Foundational phase（规则：服务多个 story 的基础实体放最前）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story
- 仓库无项目初始化需求（现有代码库、无新依赖），无独立 Setup phase。

---

## Phase 1: Foundational — Go bootstrap health 核心

**Purpose**: 所有 Go 服务共享的 health 生命周期基础（阻塞 US1 验证与 US2 交付）。

**文档清单**（编码前 MUST 完整阅读）：

- **代码规范文档**：
  - `style/golang.md`
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` §引用 间接引用，规范基准）
- **官方文档**：
  - [Go net/http package](https://pkg.go.dev/net/http)（Server/Shutdown/Handler 用法）
- **技术文章/技术参考文档**：
  - `specs/052-deploy-health-probe/contracts/bootstrap-health.md`（§1 共同约定、§2 Go 编排序列）
  - `specs/052-deploy-health-probe/data-model.md`（HealthServerLifecycle 状态机）
  - `specs/052-deploy-health-probe/research.md`（D5 决策与备选项）

Tasks:

- [X] T001 在 `common/gopkg/bootstrap/health.go`（新文件）实现 health 服务器：包内私有类型，`Start`（`net.Listen` `:38080`，mux 注册 `/healthz` 返回 200 `ok\n`，Serve 于 goroutine，失败返回含原因错误）与 `Stop`（`Shutdown` 释放端口）；日志用包内既有 `logs.Info` 风格。实现风格参照 `common/gopkg/bootstrap/http.go` 的 `HTTPServer`（两阶段关闭、done 语义可简化——health 无优雅排空需求）。单测（`common/gopkg/bootstrap/health_test.go`，表驱动 given/when/then，遵守 `style/golang.md` §单元测试：`/healthz` 200、其余路径 404、`Stop` 后端口释放、重复 `Start` 报错）。完成后 `bazel run //:gazelle common/gopkg/bootstrap`、`bazel test //common/gopkg/bootstrap/...`
- [X] T002 在 `common/gopkg/bootstrap/bootstrap.go` 的 `RunSignal` 集成 health 生命周期（依赖 T001）：组件启动循环成功后、进入等待前调用 health `Start`，失败则执行 `b.shutdown(started)` 回滚并返回错误（日志语义与组件启动失败一致，FR-010）；关闭路径（`stopOnce.Do` 内）先调用 health `Stop` 再执行 `b.shutdown(started)`（先进先出，FR-005）。单测（`common/gopkg/bootstrap/bootstrap_test.go`，用记录启停顺序的 stub 组件断言：health 启动晚于全部组件、停止早于全部组件；占住 38080 后 `RunSignal` 返回错误且已启动组件均被停止）。完成后 `bazel test //common/gopkg/bootstrap/...`、`bazel build //...`

**Checkpoint**: 全部 Go bootstrap 使用方（`projects/` 与 `experimental/` Go 服务）零改动自动获得 health 端点；`bazel test //common/gopkg/bootstrap/...` 通过。

---

## Phase 2: User Story 1 — deploy 探针 (Priority: P1) 🎯 MVP

**Goal**: deploy 生成的服务工作负载（Deployment/StatefulSet）无条件携带指向 38080/healthz 的 startupProbe（300s 预算）与 livenessProbe（FR-001/FR-002）。

**Independent Test**: builder 单测断言探针字段与参数（SC-001）——不依赖部署即可验证。

**文档清单**（编码前 MUST 完整阅读）：

- **代码规范文档**：
  - `style/golang.md`
  - [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（`style/golang.md` §引用 间接引用）
- **官方文档**：
  - [Liveness, Readiness, and Startup Probes — Probe concepts](https://kubernetes.io/docs/concepts/workloads/pods/probes/)（startup 门控语义、字段默认值、HTTP 探针端口规则）
  - [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)（参数范例 30×10=300s、成功码区间）
- **技术文章/技术参考文档**：
  - `specs/052-deploy-health-probe/contracts/deploy-probe.md`（生成契约 §2 与零修改边界 §4——参数唯一权威来源）
  - `specs/052-deploy-health-probe/data-model.md`（ProbePair 实体）
  - `specs/052-deploy-health-probe/research.md`（D1/D2/D3/D8）

Tasks:

- [ ] T003 [US1] 在 `projects/infra/deploy/runtime/k8s/builder.go` 新增探针构造（如 `buildHealthProbes()`，参数严格按 `contracts/deploy-probe.md` §2：startup `periodSeconds:10`/`failureThreshold:30`，liveness `periodSeconds:10`/`failureThreshold:3`，httpGet `path:/healthz`/`port:38080`；不设置 initialDelaySeconds；不声明 containerPort），并在 `BuildDeployment`（容器字面量，约 `builder.go:273-279`）与 `BuildStatefulSet`（约 `builder.go:441-447`）的用户服务容器上附加 `StartupProbe`/`LivenessProbe`（参照同文件 Mongo 探针先例 `builder.go:764-768`、`builder.go:804-809`；infra 组件不动）。单测（`projects/infra/deploy/runtime/k8s/builder_test.go`：Deployment 与 StatefulSet 两类工作负载的容器均携带契约探针字段与参数，SC-001；表驱动）。完成后 `bazel test //projects/infra/deploy/runtime/k8s/...`
- [ ] T004 [P] [US1] 在 `tools/release/deploy/README.md` 打包规范章节之后新增"健康探针约定"章节（D8）：端口 38080、路径 /healthz、bootstrap 自动提供（Go）/自行实现（JS，统一公共库为后续工作）、未适配服务部署不就绪的风险、探针参数值（与 `contracts/deploy-probe.md` §2 一致）。纯文档变更，无代码

**Checkpoint**: `bazel test //projects/infra/deploy/...` 通过；README 约定章节可检索。US1 可独立交付（MVP 与 Phase 1 组合即覆盖全部 Go 服务）。

---

## Phase 3: User Story 2 — JS 验证服务自行实现 health (Priority: P2)

**Goal**: 用于验证的 `experimental/` JS 服务按同一约定自行实现 health 端点（FR-006/FR-009；统一 JS bootstrap 公共库为后续独立工作，本特性不交付 JS 公共库）。

**Independent Test**: 本地 `bazel run` 启动服务后 `curl http://localhost:38080/healthz` 返回 200；发送 SIGTERM 观察日志顺序为 health 先停。

**文档清单**（编码前 MUST 完整阅读）：

- **代码规范文档**：
  - `style/javascript.md`
  - [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（`style/javascript.md` §引用 间接引用，规范基准）
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` §模块系统 间接引用；相对导入带 `.js`、`import.meta` 等书写规则）
  - `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（`style/javascript.md` §OTel 插桩 间接引用；bootstrap 两段式时序——health 实现不得破坏"静态只导 OTel → await init() → 动态 import server.js"）
- **官方文档**：
  - [Node.js http module](https://nodejs.org/api/http.html)（`createServer`/`listen`/`server.close` 语义与 ESM 导入 `node:http`）
- **技术文章/技术参考文档**：
  - `specs/052-deploy-health-probe/contracts/bootstrap-health.md`（§1 共同约定、§3 JS 自行实现约定）
  - `specs/052-deploy-health-probe/contracts/verification-testplan.md`（§2 自愈路径的 HEALTH_STOP_AFTER_MS 设计——T006 实现）
  - `specs/052-deploy-health-probe/data-model.md`（JS 实现形态）
  - `specs/052-deploy-health-probe/research.md`（D6/D7）

Tasks:

- [ ] T005 [P] [US2] 在 `experimental/grpc_chain/mid/src/bootstrap.ts` 自行实现 health（遵守 `contracts/bootstrap-health.md` §1/§3：`node:http` 监听 `:38080`、`GET /healthz` → 200 `ok\n`、其余 404；`startServer` 成功后启动 health；SIGTERM/SIGINT 关闭链第一步停止 health（`server.close`）；启动失败记录原因并按启动失败退出——FR-010；保持既有 OTel 两段式 bootstrap 时序不变）。本地验证：`bazel run //experimental/grpc_chain/mid`（或包内启动方式）后 `curl -i http://localhost:38080/healthz` 得 200。完成后 `bazel build //experimental/grpc_chain/...`
- [ ] T006 [P] [US2] 在 `experimental/ts/grpc_hello_world/src/bootstrap.ts` 实现与 T005 相同约定的 health（可复制该实现，experimental 定位允许），并增加 test-only 行为：读取环境变量 `HEALTH_STOP_AFTER_MS`（未设置时不启用），到时后停止 health 服务（模拟进程挂死；`server.close` 后保持进程存活以便 liveness 判死）。该逻辑仅存在于本服务（research.md D7 清理约束）。完成后 `bazel build //experimental/ts/grpc_hello_world/...`

**Checkpoint**: 两个 experimental JS 服务本地均提供 `/healthz:38080`；US2 可独立验证（不依赖 US1 探针）。

---

## Phase 4: User Story 3 — deploy 行为保持与端到端验证 (Priority: P3)

**Goal**: deploy 状态检查逻辑零修改（FR-003/SC-005/SC-006），判定依据经探针生效的端到端证明（SC-002/SC-003/SC-007，宪法 VI 大型测试验收）。

**Independent Test**: `guitar run` 两个 experimental 测试计划全部 case 通过 + 零修改边界核查通过。

**文档清单**（执行前 MUST 完整阅读）：

- **代码规范文档**：
  - `style/golang.md`（大型测试 case 遵守其 §单元测试 规范——`style/large_test.md` §测试用例 要求）
  - `style/large_test.md`（测试计划数量：向既有 plan 增加 suite/case，禁止新建独立 plan YAML；case 按模块组织；`go_largetest` rule 与 gazelle 默认命名；`pkg/testtool` 读取 `TESTTOOL_ENV`/`TESTTOOL_ENDPOINT_HTTP_PUBLIC`；HTTP 请求带 `tracecontext`）
- **官方文档**：
  - 无
- **技术文章/技术参考文档**：
  - `specs/052-deploy-health-probe/contracts/verification-testplan.md`（两个 suite 的设计）
  - `specs/052-deploy-health-probe/quickstart.md`（§3/§4/§5 验证步骤与期望）
  - `tools/test/guitar/README.md`（`guitar validate`/`guitar run`/`--suite` 用法）
  - `.opencode/skills/testplan/SKILL.md`（`style/large_test.md` §测试计划 间接引用；testplan skill 加载与 guitar 执行流程）
  - `specs/052-deploy-health-probe/research.md`（D7/D9——已知范围外影响，验收不涉及 system_test）

Tasks:

- [ ] T007 [US3] 就绪路径大型测试（依赖 T002、T003、T005）：加载 testplan skill 执行 `guitar run experimental/grpc_chain/testplan/interface_test.yaml`——既有 plan/deploy/case 不改（部署 backend=Go 自动 health + mid=JS 自行实现 + gateway）。验收：部署达 READY（startupProbe 门控下双语言 health 生效，SC-002/SC-007）、既有 case 全部通过、清理成功。失败时用 signoz/deploy describe 排查
- [ ] T008 [US3] 自愈路径大型测试（依赖 T006；与 T007 可并行）：在 `experimental/ts/grpc_hello_world/testplan/` 新增 `deploy_selfheal.yaml`（基于既有 `deploy.yaml`，为 service 工件注入 `HEALTH_STOP_AFTER_MS`，参照 `projects/game/testplan/deploy_agent_stall.yaml` 的 per-suite 部署配置先例）；新增 `health_test.go`（`go_largetest`，遵守 `style/large_test.md`：`testtool.MustEndpoint("http","public")` 取端点、请求带 tracecontext；case 轮询公共端点观测"初始成功 → ≥1 次失败窗口（liveness 判死+重启）→ 恢复"，时间窗容忍调度抖动）；在**既有** `interface_test.yaml` 中新增 suite（deploy 指向 `deploy_selfheal.yaml`，cases 引用新 target）——不新建独立 plan YAML（反模式 #1）。执行 `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml --suite <新suite>` 验收全部 case 通过（SC-003），再整 plan 回归
- [ ] T009 [US3] 负向验证（`quickstart.md` §4，依赖 T003）：编写临时 deploy.yaml 引用未适配约定的服务工件（如 `//projects/game/agent/service.yaml`），`deploy apply --run negative-check` 后 `deploy describe` 确认该服务持续 WAITING、环境不 READY（Story 3 场景 2），随后 `deploy del` 清理；临时文件不提交。可选附加 `quickstart.md` §5 清单抽查（kubectl get deploy -o yaml 核对探针字段）
- [ ] T010 [US3] 零修改边界核查（SC-005/SC-006，依赖 T003）：`git diff` 确认以下路径零改动——`projects/infra/deploy/deploy.proto`、`tools/release/deploy/pkg/config/`、`tools/release/deploy/pkg/schema/`、`tools/release/deploy/v2/compiler/`、`projects/infra/deploy/runtime/k8s/rollout.go`、`projects/infra/deploy/runtime/k8s/converter.go`、`projects/infra/deploy/runtime/k8s/executor.go`、`tools/release/deploy/v3/`；`rg "38080"` 确认仓库内新增 38080 出现处仅限：builder 探针、bootstrap health、experimental JS 实现、README 约定章节、测试——无任何校验逻辑（FR-008）

**Checkpoint**: 全部 guitar case 通过 + 负向与零修改核查通过 → 宪法 VI 验收达成。

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 全仓回归与验收状态收敛。

**文档清单**（执行前 MUST 完整阅读）：

- **代码规范文档**：
  - 无（本 phase 无代码变更）
- **官方文档**：
  - 无
- **技术文章/技术参考文档**：
  - `specs/052-deploy-health-probe/quickstart.md`（完整验证清单）
  - `specs/052-deploy-health-probe/checklists/requirements.md`（spec 质量清单状态）

Tasks:

- [ ] T011 最终回归门禁：`bazel build //...` 与 `bazel test //...` 全仓通过；按 `quickstart.md` §1 复核单测清单；确认 `specs/052-deploy-health-probe/checklists/requirements.md` 各项与最终交付一致（如需勾选更新）。已知范围外影响（`system_test.yaml` 全红、静态服务不 READY）确认为记录状态而非缺陷（research.md D9）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Foundational）**: 无前置，立即开始；阻塞 Phase 2 的端到端验证与 US2 交付面
- **Phase 2（US1）**: T003 依赖 T001（builder 单测不需 bootstrap，但 MVP 语义上探针+health 同步交付）；T004 无依赖
- **Phase 3（US2）**: T005/T006 依赖 T002 后的约定稳定（实际仅依赖契约文档，可与 Phase 2 并行）
- **Phase 4（US3）**: T007 依赖 T002+T003+T005；T008 依赖 T006（+T003 探针生效）；T009/T010 依赖 T003
- **Phase 5（Polish）**: 依赖全部完成

### User Story Dependencies

- **US1（P1）**: 仅依赖 Phase 1；builder 单测独立可验
- **US2（P2）**: 仅依赖契约文档；本地 curl 独立可验
- **US3（P3）**: 集成验证，依赖 US1+US2 完成（story 间唯一依赖，spec 中已声明）

### Parallel Opportunities

- T003 ∥ T004（不同文件）
- T005 ∥ T006（不同服务目录）
- T007 ∥ T008（不同测试计划，guitar 串行执行亦可）
- Phase 2 与 Phase 3 可整体并行（不同语言区域）

---

## Implementation Strategy

### MVP First（Phase 1 + US1）

1. 完成 Phase 1（Go bootstrap health）
2. 完成 Phase 2（deploy 探针 + README）
3. **验证**：builder 单测（SC-001）+ 任一 Go 服务本地 curl /healthz —— 全部 Go 服务（生产 + experimental）即获得探针驱动就绪与自愈能力

### Incremental Delivery

1. Phase 1 → Phase 2（MVP：Go 全覆盖）→ 验证
2. Phase 3（JS experimental 验证载体）→ 本地 curl 验证
3. Phase 4（大型测试 + 负向 + 零修改核查）→ 宪法 VI 验收
4. Phase 5（全仓回归）→ 交付

---

## Notes

- 单测内嵌于 T001/T002/T003（宪法 IV），不单列
- 大型测试 T007/T008 必须实际执行 `guitar run`（部署→测试→清理闭环、全部 case 通过），禁止以 `bazel build` 测试 target 替代（宪法 VI）
- `experimental/` 之外的服务代码（含 `projects/game/agent`、`agent_v2`）零修改（FR-009）；`system_test.yaml` 全红为已知范围外后果（research.md D9）
- 每个任务完成后提交（commit after each task or logical group）
