# Contract: `deploy describe` CLI

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02
**Owner tool**: `tools/release/deploy/v3`（`deploy` 二进制）

## 用途

打印单个部署环境的详细状态（状态、失败说明、服务列表、调和时间），供人工排查与工具（guitar）消费。填补现有 `list`（仅 scope 级概览）与无单环境详情视图之间的缺口。

## 命令契约

```
deploy describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>
```

- **位置参数 `<env>`**：必填。环境名，接受完整名（`scope.env`，如 `game.lt3x8q2`）或简版名（`env`，需配合默认 scope，解析规则与 `del` 一致，见 `tools/release/deploy/v3/del.go:31` 的 `NewFullEnvName`）。
- **`--scope`**：可选，显式 scope；与 `del`/`list` 行为一致（`main.go:86` flagSpecs）。
- **`--endpoint`**：可选，默认 `http://infra.liukexin.com`（`main.go:29`）。
- **`--timeout`**：可选，单次查询超时，默认 `5m`（`main.go:30`）。**无轮询**——一次 `GetEnvironment`。
- **`-v/--verbose`**：可选，打印 trace ID（与既有命令一致）。

## 行为契约

1. 解析 scope（`opts.scope` → 默认 scope → 报错 `没有默认 scope`，同 `list.go:18-24`）。
2. `NewFullEnvName` → `ParseFullEnvName` → `environmentResourceName(scope, envName)`（复用 `apply.go:220-226`）。
3. 调用 `opts.apiClient.GetEnvironment(ctx, resourceName)`（`client.go:94`）。
4. 格式化输出（见 [data-model.md](../data-model.md) 的"`deploy describe` 输出模型"）。

## 退出码与错误语义

| 场景 | stdout | 退出码 |
|------|--------|--------|
| 正常（环境存在） | 详情文本 | 0 |
| 环境不存在（`ErrNotFound`，`client.go:30`） | `环境 {fullEnvName} 不存在` | 1（非零，供 guitar 识别降级） |
| scope 缺失 | （无）stderr 报错 | 1 |
| deploy service 不可达/超时 | （无）stderr 报错 | 1 |

## 输出契约（确定性字段顺序）

```
环境 {fullEnvName}
状态: {状态中文 | 未知}
说明: {message}                        ← 仅 message 非空时输出此行
服务:
  - {artifact.name} (app={artifact.app}) [artifact]    ← 每个 artifact 一行
  - {infra.name} (app={infra.app}) [infra: {infra.resource}]  ← 每个 infra 一行
最近调和: {RFC3339 | -}
最近成功: {RFC3339 | -}
```

- 服务列表为空（artifacts 与 infras 均空）时输出 `服务: （无）`。
- 时间戳采用 RFC3339 UTC（`ts.AsTime().UTC().Format(time.RFC3339)`）；nil → `-`。
- 输出**不含 ANSI 颜色码**（与 `list` 一致，保证日志/管道可读）。

## 注册点（实现须改 `main.go`）

- `commandDescribe = "describe"` 常量（`main.go:17-22` 同区）。
- `commandExecTable`（`main.go:54`）新增 `describe → describeCommand`。
- `commandValidatorTable`（`main.go:61`）新增 `describe → validateDescribeOptions`（校验：target 非空，语义同 `validateDelOptions`，`main.go:232`）。
- `commandFlagTable`（`main.go:111`）新增 `describe: {flagEndpoint, flagTimeout, flagScope, flagVerbose}`（与 `del`/`list` 同集）。
- `usageText()`（`main.go:266`）新增 `describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>` 行。
- BUILD：`describe.go` 加入 `tools/release/deploy/v3/BUILD.bazel` 的 `deploy_lib.srcs`（gazelle 生成）；`describe_test.go` 加入 `deploy_test.srcs`。

## 参考

- 镜像模式：`tools/release/deploy/v3/del.go`、`tools/release/deploy/v3/list.go`
- 数据源：`tools/release/deploy/v2/client/client.go:94`（`GetEnvironment`）
- 状态映射：`tools/release/deploy/v3/apply.go:236`（`formatState`，须扩展 `WAITING_ROLLOUT→"等待滚动发布"`；`list.go` 复用该函数，故 `del_list_test.go` 的 `TestListCommand` 须同步增补 WAITING_ROLLOUT 断言——见决策 7）
- proto：`projects/infra/deploy/deploy.proto:89`（`Environment`）
- 测试桩模式：`tools/release/deploy/v3/del_list_test.go`（`httptest` + `clientpkg.NewClient`）
