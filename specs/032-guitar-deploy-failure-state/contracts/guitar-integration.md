# Contract: guitar 部署失败诊断集成

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02（初版，已实现）, 2026-08-03（修订：确认无 guitar 改动）

## 用途

`guitar run` 在某 suite 的**部署步骤不成功**时，于上报错误前打印目标环境的当前状态（shell-out 调用 `deploy describe`），输出流入控制台，便于判断哪个 service 失败或超时。

## 修订说明：guitar 无代码改动

本次修订为 deploy service 新增结构化 per-service 状态（见 [environment-status.md](./environment-status.md)）并增强 `deploy describe` 输出（见 [deploy-describe.md](./deploy-describe.md)）。guitar 经 shell-out 消费 describe stdout，**describe 输出增强后 guitar 自动呈现 per-service 状态，无需任何 guitar 代码改动**。初版已落地的 `diagnoseDeployFailure`（`run.go:175-180`）与 `Reporter.DeployDiagnostics`（`reporter.go:83-85`）保持不变。

以下契约内容为初版（已实现），本次仅复核其与 per-service 增强的兼容性——结论：完全兼容（guitar 不解析 describe 内容，仅透传 stdout）。

## 触发条件（精确边界，不变）

- **触发**：`runSuite` 中 `deploy apply` 步骤返回错误（`tools/test/guitar/pkg/run/run.go:151` 的 `runCommand(... deployApplyCommand ...)` 返回非 nil）。
- **不触发**：validation 失败、test（bazel test）失败、run id 生成失败、部署成功路径。（对齐 spec Assumptions：test 阶段失败不在范围。）

## 调用契约（不变）

```
r.DeployDiagnostics(fullEnvName)                              ← Reporter 醒目头部
runCommand(context.WithoutCancel(ctx), deployBinary,
           deployDescribeCommand, "--timeout=10s", fullEnvName)  ← shell-out + 短超时
```

- **常量**：`deployDescribeCommand = "describe"`（`run.go:27`）。
- **位置参数**：`fullEnvName`（`{scope}.{runID}`，run.go:133），与 cleanup 调用 `deploy del` 传入的实参一致（run.go:138）。
- **`--timeout=10s`**：单次 HTTP GET，远小于部署超时。
- **上下文**：`context.WithoutCancel(ctx)`——部署失败常为 ctx 已取消（超时），必须用脱离取消的 ctx，与 cleanup（run.go:138）同理。
- **输出**：经 `defaultRunCommand`（run.go:182）将子进程 stdout/stderr 接到 guitar 的 stdout/stderr；`deploy describe` 文本**顶格**直接呈现，guitar 不解析、不逐行缩进。

## 失败降级契约（FR-005/FR-006，不变）

- `deploy describe` 返回错误（含环境不存在 `ErrNotFound`、service 不可达、超时）时：guitar 向 `stderr` 打一行 warning（如 `warning: 获取环境 %s 状态失败: %v`），**不**改写 `runSuite` 的返回错误。
- `SuiteResult.Err`（run.go:108）保持为原始 `deploy apply` 错误。
- 降级不得 panic、不得中断后续 cleanup。

## 时序契约（单测可断言，不变）

部署失败场景下，外部命令调用顺序为：

1. `deploy apply --run {runID} {deployPath}`（失败）
2. `deploy describe --timeout=10s {fullEnvName}`（诊断；用非取消 ctx）
3. `deploy del {fullEnvName}`（cleanup；既有 defer，用非取消 ctx）

控制台输出顺序：`--- Suite: {name} ---` 头 → `  Deploy` → `  --- 环境状态 (env=...) ---` 醒目头部 + 顶格 describe 文本（修订后含 per-service 状态） → `  Cleanup` → `suite {name}: failure, error: <原始 apply 错误>` → Summary。

## Reporter 契约（不变）

`(*Reporter) DeployDiagnostics(envName string)`：打印 `  --- 环境状态 (env={fullEnvName}) ---`（2 空格缩进 + 分隔线，不着色）。随后流式呈现的 describe 输出本身亦无颜色（见 [deploy-describe.md](./deploy-describe.md)）。

## 不变量（不变 + 修订增益）

- 成功路径（apply 成功）不进入诊断分支（FR-008/SC-005）。
- cleanup 行为不变（FR-007/SC-004）。
- 诊断不引入与部署超时同量级的等待（单次 `GetEnvironment`，秒级）。
- **修订增益**：describe 输出增强后，guitar 控制台在部署失败时直接显示 per-service 状态（哪个服务等待/失败 + 原因），且因 deploy service 在 `applyAndWait` 即写入初始 PENDING per-service 数据（[environment-status.md](./environment-status.md) 决策 R4），guitar 短超时场景亦能列出服务——消除了初版的时序空窗。

## 参考

- 编排主体：`tools/test/guitar/pkg/run/run.go:118`（`runSuite`）、run.go:136-148（cleanup defer）、run.go:150-153（apply 分支）、run.go:175-180（`diagnoseDeployFailure`）
- 命令执行：`tools/test/guitar/pkg/run/run.go:182`（`defaultRunCommand`）
- Reporter：`tools/test/guitar/pkg/run/reporter.go:83`（`DeployDiagnostics`）
- 消费契约：[deploy-describe.md](./deploy-describe.md)
- 数据契约：[environment-status.md](./environment-status.md)
