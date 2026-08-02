# Contract: guitar 部署失败诊断集成

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02
**Owner tool**: `tools/test/guitar/pkg/run`

## 用途

`guitar run` 在某 suite 的**部署步骤不成功**时，于上报错误前打印目标环境的当前状态（shell-out 调用 `deploy describe`），输出流入控制台，便于判断哪个 service 失败或超时。

## 触发条件（精确边界）

- **触发**：`runSuite` 中 `deploy apply` 步骤返回错误（`tools/test/guitar/pkg/run/run.go:151` 的 `runCommand(... deployApplyCommand ...)` 返回非 nil）。
- **不触发**：validation 失败、test（bazel test）失败、run id 生成失败、部署成功路径。（对齐 spec Assumptions：test 阶段失败不在范围。）

## 调用契约

```
r.DeployDiagnostics(fullEnvName)                              ← Reporter 醒目头部
runCommand(context.WithoutCancel(ctx), deployBinary,
           deployDescribeCommand, "--timeout=10s", fullEnvName)  ← shell-out + 短超时
```

- **常量**：`deployDescribeCommand = "describe"`（与既有 `deployApplyCommand`/`deployDeleteCommand` 同区，`run.go:21-28`）。
- **位置参数**：`fullEnvName`（`{scope}.{runID}`，run.go:133 已计算），与 cleanup 调用 `deploy del` 传入的实参一致（run.go:138）。
- **`--timeout=10s`（决策 5）**：`context.WithoutCancel(ctx)` 只剥离取消信号、不带 deadline，故必须显式给 describe 一个远小于部署超时的自身超时（单次 HTTP GET，10s 足够），避免部署已超时后再卡最长 5m（describe 默认 `--timeout`）。
- **上下文**：`context.WithoutCancel(ctx)`——部署失败常为 ctx 已取消（超时），必须用脱离取消的 ctx，与 cleanup（run.go:138）同理。
- **输出**：经 `defaultRunCommand`（run.go:182）将子进程 stdout/stderr 接到 guitar 的 stdout/stderr；`deploy describe` 文本**顶格**直接呈现，guitar 不解析、不逐行缩进（决策 6）。

## 失败降级契约（FR-005/FR-006）

- `deploy describe` 返回错误（含环境不存在 `ErrNotFound`、service 不可达、超时）时：guitar 向 `stderr` 打一行 warning（如 `warning: 获取环境 %s 状态失败: %v`），**不**改写 `runSuite` 的返回错误。
- `SuiteResult.Err`（run.go:108）保持为原始 `deploy apply` 错误。
- 降级不得 panic、不得中断后续 cleanup。

## 时序契约（单测可断言）

部署失败场景下，外部命令调用顺序为：

1. `deploy apply --run {runID} {deployPath}`（失败）
2. `deploy describe --timeout=10s {fullEnvName}`（诊断；用非取消 ctx）
3. `deploy del {fullEnvName}`（cleanup；既有 defer，用非取消 ctx）

控制台输出顺序：`--- Suite: {name} ---` 头 → `  Deploy` → `  --- 环境状态 (env=...) ---` 醒目头部 + 顶格 describe 文本 → `  Cleanup` → `suite {name}: failure, error: <原始 apply 错误>` → Summary。

### 测试断言手法（决策 D）

guitar 的 `runCommand` stub 签名为 `func(context.Context, string, ...string) error`，需如下设计以验证"非取消上下文"与时序：

- **顺序**：stub 记录每次调用的 `(name, args)`，断言 `calls[0]=apply`、`calls[1]=describe`（含 `--timeout=10s` 与 `fullEnvName`）、`calls[2]=del`。
- **非取消上下文**：测试构造一个**可取消的外层 ctx**（`context.WithCancel`），runCommand stub 在 apply 调用时触发 `cancel()` 并返回 `context.DeadlineExceeded`；describe 调用时捕获传入的 `ctx`，断言 `ctx.Err() == nil`（证明用了 `WithoutCancel`），而原外层 ctx 此时 `Err() != nil`。
- **错误保留**：断言 `Run` 返回的错误仍含 "deploy apply"（原始错误未被 describe 失败改写）。

## Reporter 契约

新增方法 `(*Reporter) DeployDiagnostics(envName string)`：打印一行**醒目分隔头部**（决策 6），例如：

```
  --- 环境状态 (env=game.lt3x8q2) ---
```

- 2 空格缩进 + `--- ... ---` 分隔线包裹，使随后顶格的 describe 输出在多 suite 场景下边界清晰可辨。
- **不着色**（与 `Step`/`SuiteHeader` 一致，结构化文本不带 ANSI 码）；随后流式呈现的 `deploy describe` 输出本身亦无颜色（见 deploy-describe 契约）。
- 满足 FR-009：非 TTY/管道下无颜色码（既有 `checkTerminal` 策略天然覆盖，因本行本就不着色）。

## 不变量

- 成功路径（apply 成功）不进入诊断分支（FR-008/SC-005）——既有输出零变化。
- cleanup 行为不变（FR-007/SC-004）。
- 诊断不引入与部署超时同量级的等待（单次 `GetEnvironment`，秒级）。

## 注册点（实现须改 guitar）

- `tools/test/guitar/pkg/run/run.go`：新增 `deployDescribeCommand` 常量；在 `runSuite` 的 apply 错误分支调用诊断 helper（封装头部 + shell-out + 降级）。
- `tools/test/guitar/pkg/run/reporter.go`：新增 `DeployDiagnostics(envName string)` 方法。
- BUILD：无新增 target（既有 `pkg/run` go_library）；gazelle 无需改动（无新源文件，仅改既有文件）。
- 测试：`tools/test/guitar/pkg/run/run_test.go` 的 "deploy failure" 用例扩展——断言 describe 调用存在、位于 apply 与 del 之间、使用非取消 ctx；新增 Reporter 头部行断言。

## 参考

- 编排主体：`tools/test/guitar/pkg/run/run.go:118`（`runSuite`）、run.go:136-148（cleanup defer）、run.go:150-153（apply 分支）
- 命令执行：`tools/test/guitar/pkg/run/run.go:182`（`defaultRunCommand`）、run.go:30-39（可替换 `runCommand`/`stdout`/`stderr`）
- Reporter：`tools/test/guitar/pkg/run/reporter.go:29`（`Reporter`）、reporter.go:75（`Step`）
- 消费契约：[deploy-describe.md](./deploy-describe.md)
- 测试模式：`tools/test/guitar/pkg/run/run_test.go:34`（"deploy failure" 用例）
