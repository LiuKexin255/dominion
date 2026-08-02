---

description: "Task list for 032-guitar-deploy-failure-state"
---

# Tasks: Guitar Deploy Failure Environment Diagnostics

**Input**: Design documents from `/specs/032-guitar-deploy-failure-state/`（plan.md、spec.md、research.md、data-model.md、contracts/、quickstart.md）

**Prerequisites**: plan.md（必读）、spec.md（必读，用户故事来源）、research.md、data-model.md、contracts/、quickstart.md

**Tests**: 依宪法原则 IV，编译与单测是代码开发任务的一部分、**不单独分配 task**——每个实现 task 自带单测编写与 `bazel test` 验证。下文不再单列测试 task。

**Organization**: 本特性仅 1 个用户故事（US1，P1：部署失败时打印环境状态）。实现拆为两段：① 基础（deploy `describe` 命令，作为数据源契约）；② US1（guitar 消费 describe、在部署失败时打印诊断）。MVP = 基础 + US1 两段合计。

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

- [ ] T001 扩展 `formatState` 在 `tools/release/deploy/v3/apply.go:236`：新增 `case deploy.EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT: return "等待滚动发布"`；并在 `tools/release/deploy/v3/del_list_test.go` 的 `TestListCommand` 增补一条 WAITING_ROLLOUT 环境断言（输出 `\t等待滚动发布`）。验证：`bazel test //tools/release/deploy/v3:deploy_test`。（依 data-model.md 决策 7；该扩展波及 list 输出，须同步更新其测试）
- [ ] T002 新增 `tools/release/deploy/v3/describe.go`：实现 `describeCommand(ctx, opts)`——镜像 `del.go`（`workspace.MustRoot`→`loadConfig` 取默认 scope→`NewFullEnvName`→`ParseFullEnvName`→`environmentResourceName`），调 `opts.apiClient.GetEnvironment(ctx, name)`（`tools/release/deploy/v2/client/client.go:94`），按 [data-model.md](./data-model.md)「输出模型」与 [contracts/deploy-describe.md](./contracts/deploy-describe.md) 打印（环境名/状态 via `formatState`——返回空串（UNSPECIFIED）时输出 `未知`/说明/服务列表来自 `desired_state.artifacts[]`+`infras[]`/最近调和/最近成功）；`ErrNotFound` 时打印 `环境 {fullEnvName} 不存在` 并返回非零错误。同时新增 `tools/release/deploy/v3/describe_test.go`：httptest 桩（镜像 `del_list_test.go` 的 `writeDelListJSONResponse`/`clientpkg.NewClient`），表驱动覆盖 FAILED（含 message）、READY、PENDING、WAITING_ROLLOUT、DELETING（→ `删除中`）、UNSPECIFIED（→ `未知`）、服务列表（artifact+infra）、时间戳 nil/非 nil、`ErrNotFound`。验证：`bazel test //tools/release/deploy/v3:deploy_test`。（依赖 T001 的 formatState）
- [ ] T003 在 `tools/release/deploy/v3/main.go` 注册 `describe` 命令：新增 `commandDescribe="describe"` 常量；`commandExecTable`（main.go:54）加 `describe→describeCommand`；`commandValidatorTable`（main.go:61）加 `describe→validateDescribeOptions`（target 非空，镜像 `validateDelOptions` main.go:232）；`commandFlagTable`（main.go:111）加 `describe:{flagEndpoint,flagTimeout,flagScope,flagVerbose}`；`usageText()`（main.go:266）增补 `describe` 行。运行 `bazel run //:gazelle tools/release/deploy/v3`（将 `describe.go` 加入 `deploy_lib.srcs`、`describe_test.go` 加入 `deploy_test.srcs`）。验证：`bazel build //tools/release/deploy/v3:deploy_v3` 与 `bazel test //tools/release/deploy/v3:deploy_test`，并手跑 `bazel run //tools/release/deploy/v3:deploy_v3 -- describe --help` 确认 usage 含 describe。（依赖 T002）

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

- [ ] T004 [P] [US1] 在 `tools/test/guitar/pkg/run/reporter.go` 新增方法 `(*Reporter) DeployDiagnostics(envName string)`：打印单行 `  --- 环境状态 (env=%s) ---`（2 空格缩进 + `---` 分隔包裹，**不着色**，与既有 `Step`/`SuiteHeader` 一致）；在 `tools/test/guitar/pkg/run/run_test.go` 的 `TestReporter` 增补该方法的输出断言。验证：`bazel test //tools/test/guitar/pkg/run:run_test`。（依 [contracts/guitar-integration.md](./contracts/guitar-integration.md) Reporter 契约 + 决策 6）
- [ ] T005 [US1] 在 `tools/test/guitar/pkg/run/run.go` 新增常量 `deployDescribeCommand = "describe"`（与 `deployApplyCommand`/`deployDeleteCommand` 同区，run.go:21-28）；新增诊断 helper（封装 `r.DeployDiagnostics(fullEnvName)` + `runCommand(context.WithoutCancel(ctx), deployBinary, deployDescribeCommand, "--timeout=10s", fullEnvName)`，describe 失败时 `fmt.Fprintf(stderr, "warning: 获取环境 %s 状态失败: %v\n", ...)` 不改返回错误）；在 `runSuite`（run.go:150-153）的 deploy apply 错误分支调用该 helper（return 之前，cleanup defer 之前）。在 `run_test.go` 扩展 "deploy failure" 用例：用可取消外层 ctx，apply 调用时 `cancel()` 并返回 `context.DeadlineExceeded`；断言调用顺序 `apply→describe（含 --timeout=10s 与 fullEnvName）→del`、describe 收到的 `ctx.Err()==nil`（非取消）、`Run` 返回错误含 "deploy apply"（原始错误保留）。诊断触发点在 `runSuite` 内，spec.md Edge Cases 的 `--suite` 过滤场景由同一代码路径天然覆盖，无需额外用例。验证：`bazel test //tools/test/guitar/pkg/run:run_test`。（依赖 T004；依 [contracts/guitar-integration.md](./contracts/guitar-integration.md) 调用契约+测试断言手法+决策 5）

**Checkpoint**: `guitar run` 部署失败打印环境状态诊断，成功路径无变化，`run_test` 全绿——用户故事 1 独立可验证。

---

## Phase 3: Polish & 交叉关注

**Purpose**: 文档同步与全量验证。

### 文档清单

- **代码规范文档**：无
- **官方文档**：无
- **技术文章**：无

### Tasks

- [ ] T006 [P] 更新 `tools/release/deploy/README.md`：在「命令」小节（apply/del/list/scope 旁）增补 `deploy describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>` 用法与说明。
- [ ] T007 [P] 更新 `tools/test/guitar/README.md`：在「输出格式」小节说明部署不成功时附加的「环境状态」诊断输出（醒目头部 + `deploy describe` 顶格文本），并备注 describe 失败降级。
- [ ] T008 全量验证门禁：运行 `bazel build //tools/release/deploy/v3:deploy_v3 //tools/test/guitar/cmd:guitar` 与 `bazel test //tools/release/deploy/v3:deploy_test //tools/test/guitar/pkg/run:run_test` 全绿；随后按 [quickstart.md](./quickstart.md) 执行端到端验证 1（`deploy describe <env>` 单命令，需可访问 deploy service）、验证 2（失败路径核心场景，**必做**——优先采用构造成本最低的场景 A：suite `timeout` 或全局 `--timeout` 设极短（如 `--timeout=20s`）触发超时；仅当环境确无法构造失败场景时方可豁免，且须在任务完成记录中说明原因）、验证 3（成功路径回归），并执行「边界验证」三例（非 TTY 无 ANSI 码、环境创建前失败、describe 自身失败降级），确认失败路径诊断输出符合预期。

---

## Dependencies & Execution Order

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

## Implementation Strategy

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

## Notes

- 依宪法原则 IV，编译 + 单测内嵌于各实现 task（T001/T002/T005 自带单测与 `bazel test`），无独立测试 task
- [P] task = 不同文件、无未完成依赖
- 每个实现 task 完成后即 `bazel build`+`bazel test` 对应 target（小颗粒度反馈环）
- 依宪法原则 VI，本特性为 CLI 工具改动（非服务型应用），大型测试不作强制门禁；端到端验证见 quickstart.md（需 deploy service 访问）
- 改动不涉及 deploy service、不改 deploy apply/del 既有行为（仅扩展 formatState 的 list 副作用已由 T001 测试覆盖）
