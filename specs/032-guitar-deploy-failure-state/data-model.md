# Data Model: Guitar Deploy Failure Environment Diagnostics

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02

本特性无新建持久化实体。数据模型聚焦 `deploy describe` 的**输出模型**（从既有 `Environment` proto 投影）与 guitar 诊断调用的**流程状态**。

## 既有实体（来自 `projects/infra/deploy/deploy.proto`，本特性只读消费）

### Environment（`deploy.proto:89`）

| 字段 | 类型 | 来源 | describe 是否展示 |
|------|------|------|------------------|
| `name` | string | 资源名 `deploy/scopes/{scope}/environments/{env}` | 展示为完整环境名 `{scope}.{env}` |
| `status.state` | `EnvironmentState` enum | `deploy.proto:168` | 展示（中文映射） |
| `status.message` | string | 失败/异常原因说明 | 展示（非空时） |
| `status.last_reconcile_time` | Timestamp | 最近调和时间 | 展示 |
| `status.last_success_time` | Timestamp | 最近成功时间 | 展示（无则 `-`） |
| `desired_state.artifacts[]` | `ArtifactSpec` (`deploy.proto:197`) | 应用服务集合 | 展示（服务列表） |
| `desired_state.infras[]` | `InfraSpec` (`deploy.proto:243`) | 基础设施集合 | 展示（服务列表） |

> `type`/`create_time`/`update_time`/`etag` 不在诊断关注范围，不展示。

### ArtifactSpec 关键字段（`deploy.proto:197`）

`name`（产物名）、`app`（应用名）、`image`、`replicas`、`workload_kind`。describe 服务列表项格式：`{name} (app={app}) [artifact]`。

### InfraSpec 关键字段（`deploy.proto:243`）

`resource`（如 `mongodb`）、`profile`、`name`、`app`。describe 服务列表项格式：`{name} (app={app}) [infra: {resource}]`。

## 状态枚举中文映射（复用并扩展 `formatState`）

`formatState`（`tools/release/deploy/v3/apply.go:236`）当前映射：

| EnvironmentState | 中文 |
|------------------|------|
| `ENVIRONMENT_STATE_PENDING` | 等待中 |
| `ENVIRONMENT_STATE_RECONCILING` | 部署中 |
| `ENVIRONMENT_STATE_READY` | 就绪 |
| `ENVIRONMENT_STATE_FAILED` | 失败 |
| `ENVIRONMENT_STATE_DELETING` | 删除中 |
| `ENVIRONMENT_STATE_UNSPECIFIED` | （空串） |

**扩展点（决策 7，已确认）**：`ENVIRONMENT_STATE_WAITING_ROLLOUT`（`deploy.proto:175`，值 6）当前未映射（返回空串）。超时诊断正是需要区分"等待滚动发布"的关键场景，故 `formatState` 须新增映射 `→ 等待滚动发布`。

**影响面（须一并处理）**：`list.go` 复用 `formatState`（`tools/release/deploy/v3/list.go:42`），扩展后 list 对该态环境的输出由"无状态"变为 `\t等待滚动发布`——属正向改进（此前信息缺失），但改变了 list 输出契约。须同步更新 `tools/release/deploy/v3/del_list_test.go` 的 `TestListCommand`，增补一条 WAITING_ROLLOUT 环境的断言用例。对 apply 成功路径无实际影响（`PollUntilReady` 仅在 READY 返回，apply 成功时状态必为"就绪"）。

## `deploy describe` 输出模型（人类可读文本）

guitar 在调用 `deploy describe` 前，由 `Reporter.DeployDiagnostics` 先打印一行**醒目头部**（决策 6）：

```
  --- 环境状态 (env=game.lt3x8q2) ---
```

随后的 `deploy describe` 输出**顶格**（不逐行缩进），示例（FAILED 场景）：

```
环境 game.lt3x8q2
状态: 失败
说明: service "gateway" rollout failed: ImagePullBackOff
服务:
  - gateway (app=game) [artifact]
  - mongo (app=game) [infra: mongodb]
最近调和: 2026-08-02T10:30:00Z
最近成功: -
```

字段规则：

| 输出行 | 规则 | 校验/边界 |
|--------|------|-----------|
| `环境 {fullEnvName}` | 恒输出 | fullEnvName = `{scope}.{env}` |
| `状态: {state中文}` | 恒输出；UNSPECIFIED 时显示 `未知` | 复用 `formatState` |
| `说明: {message}` | 仅 `status.message` 非空时输出 | message 可能为空（如超时未到终态） |
| `服务:` + 列表项 | 至少有 artifacts/infras 时输出；空则输出 `服务: （无）` | 来自 `desired_state`；若响应 desired_state 为空，降级为 `（无）` |
| `最近调和: {ts}` | 恒输出；nil → `-` | RFC3339（UTC） |
| `最近成功: {ts}` | 恒输出；nil → `-` | RFC3339（UTC） |

**环境不存在**：`GetEnvironment` 返回 `ErrNotFound`（`client.go:30`）时，describe 输出友好提示（如 `环境 {fullEnvName} 不存在`）并以非零退出码返回，供 guitar 侧识别。

## guitar 诊断流程状态机（时序）

```
runSuite:
  Step("Deploy")
  applyErr = runCommand(ctx, deploy apply ...)
  if applyErr != nil:                      ← 仅部署失败路径
      r.DeployDiagnostics(fullEnvName)     ← 醒目头部行（见输出模型）
      runCommand(WithoutCancel(ctx), deploy describe,
                 "--timeout=10s", fullEnvName)   ← 决策 5：显式短超时
          ├─ 成功：stdout 顶格流入控制台
          └─ 失败：stderr 打 warning，不影响 err
      return fmt.Errorf("deploy apply %s: %w", ...)   ← 原始错误保留
  (defer) Step("Cleanup") → deploy del    ← 始终执行，行为不变
```

**不变量**：
- 诊断仅在 `apply` 失败分支触发（test 失败、validation 失败不触发——对齐 spec Assumptions）。
- 诊断与 cleanup 均用 `context.WithoutCancel(ctx)`。
- 诊断失败不改 `err`（满足 FR-006）；`SuiteResult.Err` 仍为原始部署错误。
- 调用顺序（单测可断言）：`apply` → `describe` → `del`。

## 验证规则（来自 spec FR）

- **FR-005/FR-006（同一降级路径）**：`describe` 返回错误时（环境不存在/状态不可读/describe 自身失败如 deploy service 不可达）→ guitar 降级（warning），不抛异常、不掩盖原始错误。
- **FR-007/SC-004**：cleanup 仍执行（既有 defer 不变）。
- **FR-008/SC-005**：成功路径不触发诊断（apply 成功不进错误分支）。
- **FR-009**：诊断头部行遵循 Reporter 既有颜色降级（`checkTerminal` 判定，非 TTY 无 ANSI 码）；describe 自身输出为 deploy 工具文本（与 `list` 一致，不带颜色，保持日志可读）。
