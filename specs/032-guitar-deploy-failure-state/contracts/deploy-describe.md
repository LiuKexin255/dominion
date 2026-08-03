# Contract: `deploy describe` CLI

**Feature**: 032-guitar-deploy-failure-state
**Date**: 2026-08-02（初版）, 2026-08-03（修订：per-service 状态主线）

## 用途

打印单个部署环境的详细状态（状态、per-service rollout 状态与原因、服务列表、调和时间），供人工排查与工具（guitar）消费。修订后以 **per-service 状态为主线**（取代初版依赖的环境级自由文本 `message`），直接回答"哪个 service 失败/超时"。

## 命令契约

```
deploy describe [-v] [--endpoint=url] [--timeout=5m] [--scope=name] <env>
```

- **位置参数 `<env>`**：必填。环境名，接受完整名（`scope.env`）或简版名（`env`，需配合默认 scope，解析规则与 `del` 一致，见 `tools/release/deploy/v3/del.go:31` 的 `NewFullEnvName`）。
- **`--scope`**：可选，显式 scope；与 `del`/`list` 行为一致（`main.go:86` flagSpecs）。
- **`--endpoint`**：可选，默认 `http://infra.liukexin.com`（`main.go:29`）。
- **`--timeout`**：可选，单次查询超时，默认 `5m`（`main.go:30`）。**无轮询**——一次 `GetEnvironment`。
- **`-v/--verbose`**：可选，打印 trace ID（与既有命令一致）。

> 命令注册点（`main.go` 常量/exec/validator/flag/usage 表）已由初版实现，本次修订**不改注册**，仅改 `describe.go` 的输出格式化逻辑。

## 行为契约

1. 解析 scope（`opts.scope` → 默认 scope → 报错 `没有默认 scope`，同 `list.go:18-24`）。
2. `NewFullEnvName` → `ParseFullEnvName` → `environmentResourceName(scope, envName)`（复用 `apply.go:220-226`）。
3. 调用 `opts.apiClient.GetEnvironment(ctx, resourceName)`（`client.go:94`）。
4. 格式化输出（见下"输出契约"，与 [../data-model.md](../data-model.md) 的"describe 输出模型"一致）。

## 退出码与错误语义

| 场景 | stdout | 退出码 |
|------|--------|--------|
| 正常（环境存在） | 详情文本 | 0 |
| 环境不存在（`ErrNotFound`，`client.go:30`） | `环境 {fullEnvName} 不存在` | 1（非零，供 guitar 识别降级） |
| scope 缺失 | （无）stderr 报错 | 1 |
| deploy service 不可达/超时 | （无）stderr 报错 | 1 |

## 输出契约（修订：per-service 状态主线）

```
环境 {fullEnvName}
状态: {状态中文 | 未知}
说明: {message}                                            ← 仅当 services 为空且 message 非空
服务:
  - {name} (app={app}) [{kind-tag}] {per-service状态文本}  ← 内联 per-service 状态（决策 R5）
最近调和: {RFC3339 | -}
最近成功: {RFC3339 | -}
```

### 服务列表项

- 基础格式（沿用初版）：`  - {name} (app={app}) [{kind-tag}]`
  - artifact：`kind-tag = artifact`
  - infra：`kind-tag = infra: {resource}`
- **per-service 状态文本**（修订新增）：当 `status.services` 含按 `name`+`app`+`kind` 三元组匹配项时（describe 分段遍历 artifacts/infras 时 kind 已知：artifact 段匹配 `SERVICE_KIND_ARTIFACT`，infra 段匹配 `SERVICE_KIND_INFRA`），项尾追加：

| ServiceRolloutState | 追加文本 |
|---------------------|----------|
| `READY` | ` 就绪` |
| `WAITING` | ` 等待发布: {service.message}` |
| `FAILED` | ` 失败: {service.message}` |
| `PENDING` | ` 已提交，等待观测` |
| `UNSPECIFIED` / 无匹配 | （无追加） |

### `说明:` 行规则（修订）

仅当 `status.services` **为空**（旧版服务端无 per-service 数据，或非 rollout 原因）**且** `status.message` 非空时输出。有 per-service 数据时不输出 `说明:`（避免与 per-service message 重复；决策 R5）。

### 边界

- 服务列表为空（artifacts 与 infras 均空）时输出 `服务: （无）`。
- 时间戳 RFC3339 UTC（`ts.AsTime().UTC().Format(time.RFC3339)`）；nil → `-`。
- 输出**不含 ANSI 颜色码**（与 `list` 一致，保证日志/管道可读）。
- 归并以 desired_state 服务列表为权威顺序；`status.services` 中无对应项的服务不追加状态文本（兼容旧版服务端）。

## 输出示例

WAITING_ROLLOUT（per-service 主线，`说明:` 不输出）：
```
环境 game.lt3x8q2
状态: 等待滚动发布
服务:
  - service (app=game) [artifact] 就绪
  - gateway (app=game) [artifact] 等待发布: 可用副本不足（available: 0/1）
最近调和: 2026-08-03T10:30:05Z
最近成功: -
```

非 rollout 原因（services 为空，`说明:` 输出）：
```
环境 game.lt3x8q2
状态: 失败
说明: retry count exhausted
服务:
  - gateway (app=game) [artifact]
最近调和: 2026-08-03T10:30:10Z
最近成功: -
```

## 数据依赖

- per-service 状态来自 `Environment.status.services`（proto 新字段，见 [environment-status.md](./environment-status.md)）。
- 服务列表来自 `Environment.desired_state`（既有，初版已用）。
- 当服务端版本旧（无 `services` 字段）时，describe 回退到初版纯服务列表（无 per-service 文本，`说明:` 在 message 非空时输出）——向后兼容。

## 参考

- 实现：`tools/release/deploy/v3/describe.go`（`printEnvironmentDetail`，待修订输出格式化）
- 数据源：`tools/release/deploy/v2/client/client.go:94`（`GetEnvironment`，返回 proto `*deploy.Environment`）
- 状态映射：`tools/release/deploy/v3/apply.go:248`（`formatState`，已含 WAITING_ROLLOUT）
- proto 契约：[environment-status.md](./environment-status.md)
- 输出模型：[../data-model.md](../data-model.md) "describe 输出模型"
