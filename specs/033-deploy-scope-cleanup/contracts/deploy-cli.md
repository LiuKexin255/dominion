# Contract: `deploy` CLI Commands

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Spec**: [spec.md](../spec.md)

## 用途

定义 deploy CLI 变更后的命令集、参数语义与行为契约。取代 `specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md` 中引用的旧版 `--scope` 和默认 scope 语义。

## 全局参数

- `--endpoint`：deploy service 地址，默认 `http://infra.liukexin.com`。
- `--timeout`：操作超时，默认 `5m`。
- `-v, --verbose`：打印 trace ID 等隐藏信息。

## 命令集

变更后 CLI 支持 4 个命令（移除 `scope` 命令）。

### `apply`

```
deploy apply [-v] [--endpoint=url] [--timeout=5m] [--run=id] <deploy.yaml>
```

**位置参数**（必填）：deploy.yaml 路径。

**Flags**：`--endpoint`、`--timeout`、`--run`（test 类型 `{{run}}` 占位符替换）、`-v`。

**行为**：
1. 从 deploy.yaml 读取 `name` 字段（已是 `{scope}.{env_name}` 格式）。
2. `{{run}}` 占位符替换（仅 test 类型）。
3. 解析完整环境名为 scope + envName，构造后端资源路径。
4. **不做 scope 组合**——直接使用 deploy.yaml 的 name 作为完整环境名。
5. 推送镜像、提交 desired state、轮询至就绪。

**变更**：移除 `--scope` flag。name 不再与 scope 组合。

### `del`

```
deploy del [-v] [--endpoint=url] [--timeout=5m] <env>
```

**位置参数**（必填）：完整环境名（`{scope}.{env_name}` 格式，如 `alice.dev`）。

**Flags**：`--endpoint`、`--timeout`、`-v`。

**行为**：
1. 校验完整环境名格式（`^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`）。
2. **不含点号的名称（短名）被拒绝**，返回错误说明需要完整格式。无推测、无静默回退。
3. 解析为 scope + envName，构造资源路径。
4. 调用 DeleteEnvironment，轮询至删除完成。

**变更**：移除 `--scope` flag 和默认 scope 加载。短名不再被接受。

### `describe`

```
deploy describe [-v] [--endpoint=url] [--timeout=5m] <env>
```

**位置参数**（必填）：完整环境名（`{scope}.{env_name}` 格式）。

**Flags**：`--endpoint`、`--timeout`、`-v`。

**行为**：
1. 校验完整环境名格式。
2. **不含点号的名称被拒绝**，无推测、无静默回退。
3. 调用 GetEnvironment，打印详情。

**输出契约**：不变，沿用 `specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md` 的 per-service 状态输出格式。

**变更**：移除 `--scope` flag 和默认 scope 加载。短名不再被接受。

### `list`

```
deploy list [-v] [--endpoint=url] [--timeout=5m] [--scope=name]
```

**位置参数**：无（不接受位置参数）。

**Flags**：`--endpoint`、`--timeout`、`--scope`（可选）、`-v`。

**`--scope`（可选）**：
- 指定时：列出该 scope 下所有环境。值须匹配 `^[a-z][a-z0-9]{0,7}$`。
- 不指定时：列出**所有 scope** 的所有环境（CLI 发送 `deploy/scopes/-` 作为 parent）。

**行为**：
1. 确定 scope：`opts.scope`（如指定）或 `"-"`（通配符）。
2. 调用 ListEnvironments，parent 为 `deploy/scopes/{scope}`。
3. 输出每行使用响应中的实际完整环境名（从 resource name 解析），格式 `{scope}.{env_name}` + tab + 状态。

**输出格式**：
```
{scope}.{env_name}\t{状态中文}
```

每行环境名使用响应中的 canonical scope（如 `alice.dev`），而非用户输入的 scope 或 `-`。

**变更**：移除默认 scope 加载。不指定 `--scope` 时从报错改为跨 scope 列出所有环境。

## 退出码与错误语义

| 场景 | stdout/stderr | 退出码 |
|------|---------------|--------|
| 命令成功 | stdout 输出结果 | 0 |
| 未知命令（如 `deploy scope`） | stderr: `unknown command: scope` | 1 |
| apply/del/describe 传 `--scope` | stderr: flag parse error | 1 |
| del/describe 传短名 | stderr: 完整格式错误信息 | 1 |
| 环境不存在（del/describe） | stdout: `环境 {name} 不存在` | 1 |
| deploy service 不可达/超时 | stderr: 错误信息 | 1 |

## 移除的内容

以下功能/概念从 CLI 中完全移除：

| 移除项 | 原位置 | 说明 |
|--------|--------|------|
| `scope` 命令 | `scope.go:59-89` | 查看/设置默认 scope |
| `--scope` flag（apply/del/describe） | `main.go:115-117` | 独立 scope 参数 |
| `.env/cli.json` 配置 | `scope.go:14-57` | 本地默认 scope 存储 |
| `cliConfig` 结构体 | `scope.go:20-22` | 配置数据模型 |
| `loadConfig`/`saveConfig` | `scope.go:24-57` | 配置读写 |
| `NewFullEnvName` | `identity.go:20-39` | scope + name 组合 |
| `ValidateScope` | `identity.go:45-50` | 独立 scope 校验 |
| `IsFullEnvName` | `identity.go:41-43` | 短名/完整名检测 |
| `validateEnvName` | `identity.go:68-73` | 短名校验 |
| `errNoDefaultScope` | `identity.go:11` | 无默认 scope 哨兵错误 |
| `errInvalidScope` | `identity.go:12` | 非法 scope 哨兵错误 |

## 后端 ListEnvironments 通配符扩展

### 请求

```
GET /v1/deploy/scopes/-/environments
```

- parent = `deploy/scopes/-`
- proto HTTP 注解不修改（`{parent=deploy/scopes/*}/environments` 已允许 `-` 值）

### 响应

返回所有 scope 下的所有环境，每个环境的 `name` 字段使用 canonical 资源名（如 `deploy/scopes/alice/environments/dev`），遵循 [AIP-159](https://google.aip.dev/159)。

### handler 层变更

`parseParent`（`projects/infra/deploy/handler.go:777-789`）：当 scope 为 `-` 时跳过 `domain.NewEnvironmentName` 校验，直接返回 `-`。

### 存储层变更

`ListByScope`（`projects/infra/deploy/storage/mongo.go:350-397`）：当 scope 为 `-` 时使用空 filter（`bson.M{}`），匹配所有文档。

## 参考

- AIP-159: [Reading across collections](https://google.aip.dev/159)
- AIP-132: [Standard methods: List](https://google.aip.dev/132)
- 旧版 describe 契约: [specs/032-guitar-deploy-failure-state/contracts/deploy-describe.md](../../032-guitar-deploy-failure-state/contracts/deploy-describe.md)
- API 规范: `style/api.md`
- Go 代码规范: `style/golang.md`
