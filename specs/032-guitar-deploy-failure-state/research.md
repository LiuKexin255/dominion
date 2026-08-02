# Research: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02

## 背景与现状（来自代码勘察）

- `guitar run` 编排链路：validate → deploy apply → bazel test → deploy del（清理）。部署步骤在 `tools/test/guitar/pkg/run/run.go:151` 通过 `runCommand(ctx, deployBinary, deployApplyCommand, "--run", runID, deployPath)` 调用外部 `deploy` 二进制；失败时仅 `return fmt.Errorf("deploy apply %s: %w", suite.Deploy, applyErr)`（run.go:152）。
- 部署失败常见形态：环境达到 `FAILED` 终态（某 service rollout 失败）、或部署超时（环境仍处于 `RECONCILING`/`WAITING_ROLLOUT`，对应 `context.DeadlineExceeded`）。后者尤其缺乏现场信息——错误只是一句"超时"。
- `deploy` 二进制即 v3（`//:deploy_install` → `//tools/release/deploy/v3:install` → `deploy_v3`）。现有命令：`apply`/`del`/`list`/`scope`。
- deploy service 的 `GetEnvironment`（`tools/release/deploy/v2/client/client.go:94`）返回完整 `Environment`，含：
  - `Status{State, Message, LastReconcileTime, LastSuccessTime}`（`projects/infra/deploy/deploy.proto:158`）
  - `DesiredState{Artifacts[], Infras[]}`（`deploy.proto:152`）——即本次部署所含服务集合
  - `FAILED` 态的 `Message` 字段携带失败原因（`tools/release/deploy/v2/client/poll.go:31` 将其并入错误）。
- `deploy list`（`tools/release/deploy/v3/list.go`）仅输出 `scope.env\t<状态中文>`，**不输出 message、不输出服务列表**。因此当前无任何 CLI 途径打印单环境详情。
- guitar 已 import deploy 的内部包 `tools/release/deploy/pkg/config`、`pkg/workspace`（run.go:13-14），但**不持有 deploy service endpoint**（endpoint 由 `deploy` 二进制自身的 `--endpoint`/默认值管理）。
- guitar 的 `runCommand`（run.go:182）将子进程 stdout/stderr 直接接到 guitar 的 stdout/stderr，并注入 trace 上下文。
- guitar 清理步骤已使用 `context.WithoutCancel(ctx)`（run.go:138），确保即使部署因 ctx 取消而失败，清理仍能执行——诊断调用须沿用同一模式。

## 关键决策

### 决策 1：在 deploy CLI 新增 `describe` 子命令，guitar shell-out 调用

**Decision**: 在 `deploy`（v3）新增 `describe <env>` 命令（镜像 `del.go` 的 scope 解析与 `GetEnvironment` 调用），打印单环境详情（状态、失败说明、服务列表、调和时间）。guitar 在部署失败分支用 `context.WithoutCancel(ctx)` 调用 `deploy describe <fullEnvName>`，其输出直接流入控制台。

**Rationale**:
1. **契合 guitar 既有架构**：guitar 是"shell-out 编排器"，apply/del 均通过外部 `deploy` 二进制执行；诊断沿用同一模式，零新依赖、零 endpoint 配置透传。
2. **填补 deploy CLI 真实缺口**（原则 II）：当前无 CLI 途径查看单环境详情，`describe` 是对所有人（含人工排查）可复用的正当扩展，而非仅为 guitar 打的补丁。
3. **接口优先**（原则 III）：`describe` 是清晰的 CLI 契约，guitar 只消费该契约，二者解耦、可独立演进与测试。
4. **endpoint 归属正确**：endpoint 解析留在 `deploy` 二进制内（与 apply/del/list 一致），无需在 guitar 重复引入 `--endpoint` 配置面。
5. **数据完备**：`GetEnvironment` 一次调用即得 status（state+message）+ desired_state（服务），满足 spec FR-002/FR-003/FR-004，且反映"实际部署"而非"本地文件"。

**Alternatives considered**:

- **备选 A：guitar 直接 import deploy client 调 GetEnvironment**。
  - 优点：无需改 deploy 工具；guitar 已依赖 deploy 内部包（config/workspace），增量耦合小；可得结构化数据自行排版。
  - 否决理由：① guitar 需新增 deploy service endpoint 的配置面（`--endpoint` 透传或硬编码默认值），引入不必要的配置复杂度；② 把"描述环境"的语义散落到编排器中，违背工具职责分离；③ guitar 当前对 deploy 的运行时交互全部经 CLI 契约（apply/del），引入直接 API 调用打破该一致性。结论：耦合与配置成本高于新增一个对称子命令。
- **备选 B：扩展 `deploy list` 输出 message/服务，guitar 调 `list`**。
  - 否决理由：`list` 语义是"列 scope 下所有环境概览"，强行塞入单环境详情会破坏其输出契约（已被 `del_list_test.go` 固定为 `scope.env\t状态`），且解析 list 文本去定位单个失败环境脆弱。属于"打补丁"。
- **备选 C：仅依赖 deploy apply 已有的 stdout 输出**。
  - 否决理由：apply 的轮询输出是交错的进度信息，超时场景下 `PollUntilReady` 只回 `context deadline exceeded`，无最终状态/失败原因的结构化呈现；用户仍需手动二次查询。不满足 spec FR-002/FR-003。

### 决策 2：诊断调用使用非取消上下文 + 失败降级

**Decision**: guitar 在部署失败分支用 `context.WithoutCancel(ctx)`（与 cleanup 同）调用 `deploy describe`；若 describe 自身失败，仅向 stderr 打 warning，不改变 suite 最终上报的错误。

**Rationale**: 部署失败高频原因是 ctx 超时（已取消），若用原 ctx 调 describe 会立即取消、拿不到任何状态。降级处理满足 spec FR-005/FR-006（不掩盖原始错误、状态获取失败不中断）。

### 决策 3：调用时序——apply 失败 → describe → cleanup(del)

**Decision**: 诊断插入在 deploy-apply 错误分支（return 之前）；既有 cleanup defer（run.go:136-148）不变，仍随后执行。

**Rationale**: 精确命中"部署不成功"场景（spec 将 test 阶段失败显式排除）；不干扰成功路径（FR-008）；cleanup 行为零改变（FR-007/SC-004）。

### 决策 4：命令名 `describe`

**Decision**: 新命令命名 `describe`（用户已确认）。

**Rationale**: kubectl `describe` 语义即"打印资源详情"（含 status/事件），与本场景"打印环境状态详情"高度一致，对用户直觉友好。与 deploy 现有 `apply`/`del`/`list`/`scope` 短动词风格相容。

**Alternatives**: `status`（侧重运行态）、`get`（对齐 REST `GetEnvironment`，但语义偏"取资源"且 ambiguous）、`show`/`info`（过于泛化）。已选定 `describe`。

### 决策 5：诊断调用显式约束短超时（`--timeout=10s`）

**Decision**: guitar shell-out 调 `deploy describe` 时显式传 `--timeout=10s`。

**Rationale**: `context.WithoutCancel(ctx)`（决策 2）只剥离取消信号、不带 deadline，故 describe 子进程仅受自身 `--timeout` 约束（默认 5m）。部署失败常已超时，若 describe 再卡最长 5m 才进 cleanup，体验很差，违背"诊断远小于部署超时"。describe 是单次 `GetEnvironment`（HTTP GET），10s 足够且远小于任何部署超时。

### 决策 6：诊断头部用醒目分隔标记，describe 输出保持顶格

**Decision**: guitar 侧 `Reporter.DeployDiagnostics(envName)` 打印醒目头部行 `  --- 环境状态 (env={fullEnvName}) ---`（2 空格缩进 + 分隔线包裹）；`deploy describe` 自身的多行输出保持顶格（不逐行缩进）。

**Rationale**: guitar 经 `runCommand` 将子进程 stdout 直连 os.Stdout，逐行加缩进需引入捕获+重打逻辑（备选 b），破坏"直接流入"简洁性。改用醒目分隔标记（备选 a）即可在多 suite 场景下让用户清晰辨识诊断块的归属与边界，且 describe 自身输出的 `环境/状态/服务/...` 结构辨识度足够。头部行不着色（与既有 `Step`/`SuiteHeader` 一致），满足 FR-009。

**Alternatives**: (b) guitar 捕获 describe stdout 逐行加 2 空格缩进重打（视觉最佳但增加捕获逻辑，否决）；(c) 给 describe 加 `--indent` 参数（过度设计，否决）。

### 决策 7：扩展 `formatState` 覆盖 `WAITING_ROLLOUT`，并同步更新 `list` 测试

**Decision**: 在 `tools/release/deploy/v3/apply.go:236` 的 `formatState` 新增 `ENVIRONMENT_STATE_WAITING_ROLLOUT → 等待滚动发布`；因 `list.go` 复用 `formatState`，该扩展会改变 list 对该态环境的输出（由无状态变为 `\t等待滚动发布`），需同步更新 `tools/release/deploy/v3/del_list_test.go` 的 list 用例（增补 WAITING_ROLLOUT 断言）。

**Rationale**: 超时诊断正是需区分"等待滚动发布"的关键场景，扩展是正向改进（list 此前对该态输出空，信息缺失）。选择"扩展共享函数 + 同步测试"而非"describe 用独立映射隔离"，是因为该态本就应被所有消费者正确显示，隔离反而制造不一致。扩展对 apply 成功路径无实际影响（`PollUntilReady` 仅在 READY 返回，apply 成功时状态必为"就绪"）。

## 结论

所有 NEEDS CLARIFICATION 已解决（含用户确认的 A~E）：技术路径（describe 子命令 + shell-out）、数据来源（GetEnvironment 返回 desired_state，已验证）、上下文处理（非取消 ctx）、超时（--timeout=10s）、失败降级、调用时序、输出层级（醒目标记 + 顶格）、formatState 扩展影响面（波及 list，同步更新测试）、命名（describe）均明确。可进入 Phase 1 设计 `describe` 输出模型与 CLI/集成契约。
