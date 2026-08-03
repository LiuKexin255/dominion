# Data Model: Deploy Scope Removal

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Spec**: [spec.md](spec.md)

## 概述

本变更不新增数据实体，而是**简化**现有环境名模型和 CLI 配置模型。以下描述变更前后的数据模型差异。

## 实体

### Environment Name（环境名）

环境唯一标识，格式为 `{scope}.{env_name}`（如 `alice.dev`）。

**格式规则（不变）**：
- 完整名正则：`^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`（`identity.go:17` 的 `fullEnvRegexp`）
- 两部分均须匹配 `^[a-z][a-z0-9]{0,7}$`（`identity.go:16` 的 `envPartRegexp`）
- 点号 `.` 为完整名分隔符

**变更前**：
```
NewFullEnvName(scope, name) → "scope.name"   # scope + name 组合
NewFullEnvName("", "dev")   → "没有默认 scope"  # 短名需 scope 补全
```

**变更后**：
- `NewFullEnvName` 函数移除。环境名始终由用户显式提供完整格式。
- `ValidateFullEnvName(name)` 直接校验完整格式（拒绝短名）。
- `ParseFullEnvName(name)` 解析为 scope + envName（不变）。

### CLI 配置（移除）

**变更前**：`cliConfig` 结构体（`scope.go:20-22`）存储于 `.env/cli.json`，包含 `DefaultScope` 字段。

**变更后**：`cliConfig` 结构体、`loadConfig`/`saveConfig` 函数、`.env/cli.json` 文件全部移除。CLI 不再有本地配置。

### Backend Resource Name（后端资源名，不变）

API 级资源路径 `deploy/scopes/{scope}/environments/{env_name}`，通过 `environmentResourceName(scope, envName)`（`apply.go:224-226`）构造。

对于 list 跨 scope 场景，parent 使用 `deploy/scopes/-`（AIP-159 通配符）。

### Scope 资源声明（新增 proto 声明，无运行时行为变更）

在 `deploy.proto` 新增 `Scope` 消息，声明 `google.api.resource`（pattern `deploy/scopes/{scope}`），用于驱动 codegen 生成 `ParseScopeName()` 和请求级 `ParseName()`。该资源无标准方法（no CRUD RPCs），仅作 codegen 声明存在——参照 `projects/game/game.proto:176-185` 的 `Template` 资源模式。

codegen 生成的方法（确认于 `deploy_aip.pb.resource.go`）：
- `ScopeName{ScopeID}`、`ParseScopeName()`、`ScopeName.ContainsWildcard()`（AIP-159 通配符）
- `EnvironmentName{ScopeID, EnvNameID}`、`ParseEnvironmentName()`、`EnvironmentName.Parent() ScopeName`
- `ServiceEndpointsName{ScopeID, EnvNameID, AppID, ServiceID}`、`ParseServiceEndpointsName()`
- 请求级 `ParseName()`：`GetEnvironmentRequest`、`GetServiceEndpointsRequest`、`DeleteEnvironmentRequest`
- 消息级 `ParseName()`：`Environment.ParseName()`、`ServiceEndpoints.ParseName()`

详见 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md)。

### Scope Wildcard `-`（新增概念）

AIP-159 定义的跨集合通配符，仅用于 `ListEnvironments` 的 parent 参数。

- 值：`-`（单字符）
- Parent 格式：`deploy/scopes/-`
- 语义：匹配所有 scope 下的所有环境
- 仅在 list 命令不指定 `--scope` 时由 CLI 自动使用
- 用户不直接输入 `-`；后端 handler 的 `parseParent` 函数识别它

## CLI options 结构体变更

**变更前**（`main.go:34-43`）：
```go
type options struct {
    command   string
    target    string
    endpoint  string
    timeout   time.Duration
    scope     string        // 全局 flag
    run       string
    verbose   bool
    apiClient *client.Client
}
```

**变更后**：
```go
type options struct {
    command   string
    target    string
    endpoint  string
    timeout   time.Duration
    scope     string        // 仅 list 命令使用
    run       string
    verbose   bool
    apiClient *client.Client
}
```

`scope` 字段保留（list 仍用），但 apply/del/describe 命令不再读取它。全局 `validateOptions` 不再校验 `scope`。

## 命令 flag 映射变更

**变更前**（`main.go:114-120`）：
| 命令 | flags |
|------|-------|
| apply | endpoint, timeout, scope, run, verbose |
| del | endpoint, timeout, scope, verbose |
| describe | endpoint, timeout, scope, verbose |
| list | endpoint, timeout, scope, verbose |
| scope | endpoint, timeout, scope, verbose |

**变更后**：
| 命令 | flags |
|------|-------|
| apply | endpoint, timeout, run, verbose |
| del | endpoint, timeout, verbose |
| describe | endpoint, timeout, verbose |
| list | endpoint, timeout, scope, verbose |

`scope` 命令行从注册表中完全移除。

## 后端 ListByScope 语义扩展

**变更前**（`storage/mongo.go:361`）：
```go
filter := bson.M{mongoFieldScope: scope}  // scope 精确匹配
```

**变更后**：
```go
var filter bson.M
if scope == "-" {
    filter = bson.M{}  // 跨 scope：匹配所有
} else {
    filter = bson.M{mongoFieldScope: scope}  // 精确匹配
}
```

`fakeRepository.ListByScope`（`repository_fake_test.go:136`）同步扩展：scope 为 `-` 时跳过前缀过滤。
