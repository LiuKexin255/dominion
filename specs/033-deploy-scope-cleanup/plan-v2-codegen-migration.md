# Plan V2: handler.go 全面迁移到 codegen name 解析

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Status**: Replaces plan-supplement-codegen.md
**Spec**: [spec.md](spec.md)

## 背景

### 问题回顾

之前 `plan-supplement-codegen.md` 的迁移范围太保守——仅迁移 `parseParent`。用户 review 指出：

1. **NewEnvironmentName 副作用校验**：`parseParent` 用 `domain.NewEnvironmentName(scope, "env")` 触发 scope regex 校验，但 "env" 是 dummy 值——这是利用构造函数副作用的校验，不是优雅用法
2. **domain.ParseResourceName 仍被大范围使用**：handler.go 中 GetEnvironment、DeleteEnvironment、UpdateEnvironment、GetServiceEndpoints、fromProtoEnvironment 共 5 处仍用 domain 手写解析
3. **parseParent 无真正抽象**：它只是 parse + 副作用校验的组合
4. **不符合重构式变更**：宪法原则 II 要求将手写的 name 解析统一迁移到 codegen

### 用户决策

| 决策项 | 选择 |
|--------|------|
| domain 值对象 | **保留**——handler 边界用 codegen，domain 类型保持内部类型安全 |
| parseParent | **消除**——内联到 ListEnvironments 和 CreateEnvironment |
| 死代码 | **全部移除**——fromProtoEnvironment、domain.ParseResourceName、domain.ParseServiceEndpointsName |

## 调研依据

### codegen 已生成的完整 API

确认于 `bazel-bin/projects/infra/deploy/deploy_go_proto_/dominion/projects/infra/deploy/deploy_aip.pb.resource.go`：

**类型**（codegen package `deploy`）：
- `ScopeName{ScopeID string}`
- `EnvironmentName{ScopeID, EnvNameID string}`
- `ServiceEndpointsName{ScopeID, EnvNameID, AppID, ServiceID string}`

**解析函数**：
- `ParseScopeName(s) (ScopeName, error)` — 3 段结构校验
- `ParseEnvironmentName(s) (EnvironmentName, error)` — 5 段结构校验
- `ParseServiceEndpointsName(s) (ServiceEndpointsName, error)` — 10 段结构校验

**请求级 ParseName()**：
- `(x *GetEnvironmentRequest) ParseName() (EnvironmentName, error)` — `projects/infra/deploy/deploy_aip.pb.resource.go:370`
- `(x *GetServiceEndpointsRequest) ParseName() (ServiceEndpointsName, error)` — `:375`
- `(x *DeleteEnvironmentRequest) ParseName() (EnvironmentName, error)` — `:380`
- `(x *Environment) ParseName() (EnvironmentName, error)` — `:350`（UpdateEnvironment 使用）

**辅助方法**：
- `ScopeName.ContainsWildcard() bool` — `n.ScopeID == "-"`
- `EnvironmentName.Parent() ScopeName` — `:194`
- `ScopeName.EnvironmentName(envNameID) EnvironmentName` — `:201`
- `ScopeName.String()` → `"deploy/scopes/{scope}"`

**未生成 ParseName 的请求**：
- `ListEnvironmentsRequest` — parent 字段用 `child_type` reference（codegen 不生成 ParseName，直接用 `ParseScopeName`）
- `CreateEnvironmentRequest` — parent 字段用 `child_type` reference
- `UpdateEnvironmentRequest` — name 嵌套在 `Environment environment = 1` 中，用 `req.GetEnvironment().ParseName()`

### game 范式参照

game handler（`projects/game/session/handler/handler.go`）的标准模式：

```go
// game handler:47-53 — codegen parse + business validation 叠加
tplName, err := game.ParseTemplateName(req.GetParent())
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
if err := gameconst.ValidateTemplateName(tplName); err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
```

关键特征：
- **codegen 做结构校验**（段数、字面量、非空）
- **业务校验叠加在 codegen 之上**（`gameconst.ValidateTemplateName` 检查已知模板集合）
- **裸 string 传给下游**（`tplName.TemplateID`、`name.SessionID`）
- **不经过 toStatusError**——codegen 错误直接 `status.Error(codes.InvalidArgument, ...)`
- **无 parseParent helper**——每个 handler 方法内联解析

### codegen vs domain 校验差异

| 维度 | codegen (`ParseXxxName`) | domain (`ParseResourceName` / `NewXxxName`) |
|------|--------------------------|---------------------------------------------|
| 校验内容 | 段数、字面量、非空、不含 `/` | regex `^[a-z][a-z0-9]{0,7}$`（scope/envName）+ `^[a-z][a-z0-9-]{0,19}$`（app/service） |
| 错误类型 | `fmt.Errorf` 字符串 | sentinel `ErrInvalidName` |
| 字段访问 | 导出字段 `.ScopeID` / `.EnvNameID` / `.AppID` / `.ServiceID` | 未导出 + 访问器 `.Scope()` / `.EnvName()` / `.App()` / `.Service()` / `.Label()` / `.EnvLabel()` |
| 独有方法 | `.Parent()` / `.ContainsWildcard()` / `.Validate()` / `.FullName()` / `.Type()` / `.Pattern()` | `.Label()` / `.EnvLabel()` / `.EnvironmentName()`（ServiceEndpointsName） |

**关键结论**：codegen 提供**结构校验**，domain 构造函数 `NewEnvironmentName` / `NewServiceEndpointsName` 提供**业务校验**（regex）。两者叠加 = 原来单一 domain 解析的全部校验。

### domain 值对象深度依赖（保留的理由）

`domain.EnvironmentName` 在 service 内部的使用范围（grep 确认 47 处引用）：

| 层 | 用法 |
|----|------|
| **domain aggregate** | `Environment.name EnvironmentName`（`environment.go:28`）、`Environment.Name() EnvironmentName`（`:131`）、`EnvironmentSnapshot.Name EnvironmentName`（`:15`） |
| **domain queue** | `map[EnvironmentName]*WorkItem`、`map[EnvironmentName]bool`、`chan EnvironmentName`（`queue.go:36-40`）— 需要值类型可比较 |
| **repository 接口** | `Get(ctx, name EnvironmentName)`、`UpdateDesired(ctx, name EnvironmentName, ...)`、`Delete(ctx, name EnvironmentName)`、`TransitionStatus(ctx, name EnvironmentName, ...)`（`repository.go:14-44`） |
| **service 层** | `command.Create/Update/Delete(ctx, envName EnvironmentName, ...)`、`reconcile.ProcessOne/MarkRetryExhausted(ctx, envName EnvironmentName)` |
| **runtime 层** | `K8sRuntime.Delete(ctx, envName EnvironmentName)`、`EnvironmentRuntime.Delete(ctx, envName EnvironmentName)` |
| **storage 层** | `MongoRepository.Get/UpdateDesired/Delete/TransitionStatus(ctx, name EnvironmentName, ...)`；`toDomain()` 用 `domain.NewEnvironmentName(m.Scope, m.EnvName)` 重构 |
| **测试层** | 大量 `domain.NewEnvironmentName("scope", "env")` 构造与断言 |

彻底移除值对象需改动 20+ 文件的接口签名，远超 feature 033 的 scope removal 目标。保留值对象，仅在 handler 边界迁移到 codegen。

### fromProtoEnvironment 是死代码

`handler.go:324-340` 的 `fromProtoEnvironment` 函数全仓库无调用者（grep 确认仅有定义处）。该函数内部使用 `domain.ParseResourceName`。

## 设计决策

### D1: handler 边界统一使用 codegen 解析

**Decision**: handler.go 中**所有** name 解析使用 codegen 方法，不保留任何 domain 手写解析。

**6 个迁移点**：

| # | 函数 | 行号 | Before（domain） | After（codegen） |
|---|------|------|-------------------|-------------------|
| 1 | `GetEnvironment` | :55 | `domain.ParseResourceName(req.GetName())` | `req.ParseName()` → `domain.NewEnvironmentName` |
| 2 | `GetServiceEndpoints` | :70 | `domain.ParseServiceEndpointsName(req.GetName())` | `req.ParseName()` → `domain.NewServiceEndpointsName` |
| 3 | `UpdateEnvironment` | :265 | `domain.ParseResourceName(req.GetEnvironment().GetName())` | `req.GetEnvironment().ParseName()` → `domain.NewEnvironmentName` |
| 4 | `DeleteEnvironment` | :291 | `domain.ParseResourceName(req.GetName())` | `req.ParseName()` → `domain.NewEnvironmentName` |
| 5 | `ListEnvironments` | :197 | `parseParent(req.GetParent())` | 内联 `ParseScopeName` + 通配符 + `ValidateScope` |
| 6 | `CreateEnvironment` | :225 | `parseParent(req.GetParent())` | 内联 `ParseScopeName` + 通配符拒绝 + `ValidateScope` |

**消除的函数**：
- `parseParent`（:775-797）— 内联到 ListEnvironments / CreateEnvironment
- `fromProtoEnvironment`（:324-340）— 死代码移除

### D2: 业务校验通过 domain 构造函数（非副作用）

**Decision**: codegen 结构校验后，用 domain 构造函数 `NewEnvironmentName` / `NewServiceEndpointsName` 进行 regex 业务校验 + 类型构造。

**为什么这不是"副作用利用"**：

当前 parseParent 的问题是用 `domain.NewEnvironmentName(scope, "env")` 传入 **dummy "env"** 值，仅为触发 scope regex 校验——这是副作用利用。

迁移后的模式是传入 **真实解析值** `domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)`——构造一个真实的 domain 类型，regex 校验是构造的一部分，不是副作用。

### D3: scope 校验新增 domain.ValidateScope

**Decision**: 在 `domain/environment_name.go` 新增导出函数 `ValidateScope(s string) error`，用于 parseParent 消除后 ListEnvironments / CreateEnvironment 中对 scope 的独立校验。

**为什么不复用 NewEnvironmentName**：scope 校验只需校验单个字段，但 NewEnvironmentName 要求两个参数（scope + envName）。ListEnvironments 的 parent 只有 scope 没有 envName。

```go
// domain/environment_name.go 新增
// ValidateScope reports whether s conforms to the scope format rule
// (^[a-z][a-z0-9]{0,7}$). Used by handler-layer scope validation after
// codegen ParseScopeName (structural validation) and before cross-scope
// query dispatch.
func ValidateScope(s string) error {
    if !environmentNameRegexp.MatchString(s) {
        return ErrInvalidName
    }
    return nil
}
```

### D4: parseParent 消除——内联到调用点

**Decision**: 消除 parseParent 函数，将逻辑内联到 ListEnvironments 和 CreateEnvironment。参照 game 范式（无 parseParent helper，每个 handler 方法自包含解析）。

**ListEnvironments**（内联后）：

```go
func (h *Handler) ListEnvironments(ctx context.Context, req *ListEnvironmentsRequest) (*ListEnvironmentsResponse, error) {
    scopeName, err := ParseScopeName(req.GetParent())
    if err != nil {
        return nil, status.Error(codes.InvalidArgument, err.Error())
    }

    scope := scopeName.ScopeID
    if !scopeName.ContainsWildcard() {
        if err := domain.ValidateScope(scope); err != nil {
            return nil, toStatusError(err)
        }
    }

    envs, nextPageToken, err := h.repo.ListByScope(ctx, scope, req.GetPageSize(), req.GetPageToken())
    // ... (unchanged)
}
```

**CreateEnvironment**（内联后）：

```go
func (h *Handler) CreateEnvironment(ctx context.Context, req *CreateEnvironmentRequest) (*Environment, error) {
    if req.GetEnvironment() == nil {
        return nil, status.Error(codes.InvalidArgument, "environment is required")
    }

    scopeName, err := ParseScopeName(req.GetParent())
    if err != nil {
        return nil, status.Error(codes.InvalidArgument, err.Error())
    }
    if scopeName.ContainsWildcard() {
        return nil, status.Error(codes.InvalidArgument, "scope wildcard '-' is not allowed for create")
    }
    if err := domain.ValidateScope(scopeName.ScopeID); err != nil {
        return nil, toStatusError(err)
    }

    envName, err := domain.NewEnvironmentName(scopeName.ScopeID, req.GetEnvName())
    if err != nil {
        return nil, toStatusError(err)
    }
    // ... (unchanged)
}
```

**行为等价性验证**：

| 输入 parent | ListEnvironments 旧行为 | ListEnvironments 新行为 | CreateEnvironment 旧行为 | CreateEnvironment 新行为 |
|---|---|---|---|---|
| `deploy/scopes/dev` | parseParent→"dev"→校验→ListByScope("dev") | ParseScopeName→ScopeID="dev"→ValidateScope→ListByScope("dev") | parseParent→"dev"→NewEnvironmentName("dev",env) | ParseScopeName→ScopeID="dev"→ValidateScope→NewEnvironmentName("dev",env) |
| `deploy/scopes/-` | parseParent→"-"→wildcard→ListByScope("-") | ParseScopeName→ScopeID="-"→ContainsWildcard→ListByScope("-") | parseParent→"-"→NewEnvironmentName("-",env)→ErrInvalidName | ParseScopeName→ScopeID="-"→ContainsWildcard→**显式拒绝 InvalidArgument** |
| `deploy/scopes/INVALID` | parseParent→"INVALID"→regex fail→InvalidArgument | ParseScopeName→"INVALID"→ValidateScope fail→InvalidArgument | 同 ListEnvironments | 同 |
| `bad-parent` | parseParent→ParseScopeName fail→ErrInvalidName→InvalidArgument | ParseScopeName fail→status.InvalidArgument | 同 | 同 |
| `deploy/scopes/` (空) | parseParent→empty segment→InvalidArgument | ParseScopeName→empty→InvalidArgument | 同 | 同 |

**CreateEnvironment 通配符行为差异**：旧行为依赖下游 `NewEnvironmentName("-", envName)` 失败，新行为在入口显式拒绝。两者都返回 InvalidArgument，但新行为更明确——提前拦截而非延迟失败。T005 测试用例断言 `wantCode: codes.InvalidArgument` 不受影响。

### D5: 错误处理——codegen 错误直接映射

**Decision**: codegen parse 错误直接用 `status.Error(codes.InvalidArgument, err.Error())`，不经过 `toStatusError`。

**理由**：
- `toStatusError` 的 default 分支返回 `codes.Internal`——codegen 的 `fmt.Errorf` 不匹配任何 sentinel error，会被错误映射为 Internal
- game handler（`:49`）直接 `status.Error(codes.InvalidArgument, err.Error())`——仓库既有惯例
- codegen 错误消息已包含足够信息（`"parse %q: bad number of segments, want: 3, got: %d"`）

domain 构造函数/校验函数的错误（`ErrInvalidName` 等 sentinel）仍走 `toStatusError`。

### D6: 移除 domain 手写解析函数

**Decision**: 移除 `domain.ParseResourceName`（`environment_name.go:27-34`）和 `domain.ParseServiceEndpointsName`（`service_endpoints_name.go:53-60`），及对应测试。

**理由**：
- 迁移后无生产代码调用者（handler.go 5 处全部迁移）
- 仅 handler_test.go:456 使用 `domain.ParseResourceName`——改为 `domain.NewEnvironmentName("dev", "alpha")`
- 宪法原则 II：简化、消除死代码
- domain 层的职责是值对象定义和构造（`NewXxxName`），不是解析（解析是边界关注点，由 codegen 负责）

**保留的 domain 函数**：
- `NewEnvironmentName(scope, envName)` — 被 handler.go（CreateEnvironment）、storage/mongo.go toDomain、测试使用
- `NewServiceEndpointsName(scope, envName, app, service)` — 被 handler.go（GetServiceEndpoints）、测试使用
- `ValidateScope(s)` — 新增，被 handler.go（ListEnvironments / CreateEnvironment）使用

### D7: 迁移留在 feature 033

**Decision**: 本次 codegen 全面迁移作为 feature 033 的 Phase 1 扩展。

**理由**：
- T000（Scope 资源声明）已完成，是本次迁移的依赖
- parseParent 原本就在 Phase 1 中修改（T001）——扩展为全面迁移是自然的
- 拆分为独立 feature 会产生依赖问题（新 feature 依赖 T000 的 proto 变更）
- 用户 review 明确要求在当前 feature 中完成此迁移

## 逐函数迁移详细设计

### GetEnvironment（handler.go:54-66）

```go
// Before
envName, err := domain.ParseResourceName(req.GetName())

// After
name, err := req.ParseName()
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
envName, err := domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)
if err != nil {
    return nil, toStatusError(err)
}
```

### GetServiceEndpoints（handler.go:69-193）

```go
// Before
name, err := domain.ParseServiceEndpointsName(req.GetName())

// After
cgName, err := req.ParseName()
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
name, err := domain.NewServiceEndpointsName(cgName.ScopeID, cgName.EnvNameID, cgName.AppID, cgName.ServiceID)
if err != nil {
    return nil, toStatusError(err)
}
```

**说明**：变量 `name` 仍是 `domain.ServiceEndpointsName`，下游所有 `.App()`、`.Service()`、`.EnvLabel()`、`.EnvironmentName()` 调用不变。

### UpdateEnvironment（handler.go:256-287）

```go
// Before
envName, err := domain.ParseResourceName(req.GetEnvironment().GetName())

// After
name, err := req.GetEnvironment().ParseName()
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
envName, err := domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)
if err != nil {
    return nil, toStatusError(err)
}
```

### DeleteEnvironment（handler.go:290-305）

```go
// Before
envName, err := domain.ParseResourceName(req.GetName())

// After
name, err := req.ParseName()
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
envName, err := domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)
if err != nil {
    return nil, toStatusError(err)
}
```

### ListEnvironments（handler.go:196-217）

见 D4 设计——内联 `ParseScopeName` + 通配符 + `ValidateScope`。

### CreateEnvironment（handler.go:220-253）

见 D4 设计——内联 `ParseScopeName` + 通配符拒绝 + `ValidateScope` + `NewEnvironmentName`。

## 影响范围

### 变更文件清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `domain/environment_name.go` | 新增 + 删除 | 新增 `ValidateScope`；删除 `ParseResourceName` |
| `domain/service_endpoints_name.go` | 删除 | 删除 `ParseServiceEndpointsName` |
| `handler.go` | 重写 | 6 处 name 解析迁移 + 消除 parseParent + 移除 fromProtoEnvironment |
| `handler_test.go` | 修改 | :456 改用 `domain.NewEnvironmentName`；T005 通配符测试调整 |
| `domain/environment_name_test.go` | 删除 | 删除 `TestParseResourceName` |
| `domain/service_endpoints_name_test.go` | 删除 | 删除 `TestParseServiceEndpointsName` |

### 不变的文件

以下文件**不变**（domain 值对象保留）：

- `domain/environment.go`（aggregate root）
- `domain/repository.go`（接口签名）
- `domain/queue.go`（map key 类型）
- `domain/errors.go`
- `storage/mongo.go`（除 T002 ListByScope 通配符外不变）
- `service/command.go`、`service/reconcile.go`
- `runtime/k8s/executor.go`
- 所有 `*_test.go`（除上述明确修改的）

## 需要更新的文档

### spec.md

**FR-016** 更新——parseParent 消除，改为内联 codegen + 显式通配符处理。

当前（spec.md:134）：
> handler 层 `parseParent` 函数须使用 codegen 生成的 `ParseScopeName` 解析 parent...

更新为：
> handler 层 name 解析须全部使用 codegen 生成的方法（`req.ParseName()`、`ParseScopeName`），不保留任何 domain 手写解析。`ListEnvironments` 内联 `ParseScopeName` + `ContainsWildcard()` 通配符识别 + `domain.ValidateScope` 正则校验；`CreateEnvironment` 内联 `ParseScopeName` + 显式拒绝通配符 + `domain.ValidateScope`。其他 RPC（GetEnvironment、GetServiceEndpoints、UpdateEnvironment、DeleteEnvironment）使用请求级 `ParseName()` + `domain.NewXxxName` 构造。handler 中不保留 `parseParent` 辅助函数。

### plan.md

**Scale/Scope** 更新——补充全面迁移影响范围。

**Constraints** 已包含 Scope 资源声明说明，无需修改。

### research.md

**R7** 更新——parseParent 迁移改为全面迁移。

**新增 R8**：handler.go 全面 codegen 迁移决策（涵盖所有 6 个迁移点 + parseParent 消除 + 死代码移除 + domain 值对象保留理由）。

### data-model.md

"Scope 资源声明"小节补充——codegen 已生成请求级 ParseName 和 ScopeName 通配符方法。

### contracts/deploy-cli.md

"handler 层变更"小节更新——parseParent 消除，改为内联 codegen。

### tasks.md

Phase 1 重构——T001 替换为全面迁移 task。

## tasks.md Phase 1 具体更新

### 保留不变的 tasks

- **T000**（已完成）：Scope 资源声明
- **T002** [P]：ListByScope storage 通配符支持
- **T003** [P]：fakeRepository 通配符支持
- **T004**：ListByScope 接口注释
- **T005** [P]：Handler 通配符测试（依赖 T003）—— 需调整 CreateEnvironment 通配符测试的错误消息
- **T006** [P]：Storage 通配符测试（依赖 T002）

### 替换 T001

```
- [ ] T001 [P] handler.go 全面迁移到 codegen name 解析。具体变更：

  (A) 新增 domain.ValidateScope：在 `projects/infra/deploy/domain/environment_name.go`
  新增导出函数 `ValidateScope(s string) error`，使用现有 `environmentNameRegexp`
  校验 scope 格式（`^[a-z][a-z0-9]{0,7}$`），失败返回 `ErrInvalidName`。

  (B) 迁移 5 个 handler name 解析到 codegen：
  - GetEnvironment（:55）: `domain.ParseResourceName(req.GetName())` →
    `req.ParseName()`（codegen）→ `domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)`
  - GetServiceEndpoints（:70）: `domain.ParseServiceEndpointsName(req.GetName())` →
    `req.ParseName()`（codegen）→ `domain.NewServiceEndpointsName(...)`
  - UpdateEnvironment（:265）: `domain.ParseResourceName(req.GetEnvironment().GetName())` →
    `req.GetEnvironment().ParseName()`（codegen）→ `domain.NewEnvironmentName(...)`
  - DeleteEnvironment（:291）: `domain.ParseResourceName(req.GetName())` →
    `req.ParseName()`（codegen）→ `domain.NewEnvironmentName(...)`

  codegen parse 错误用 `status.Error(codes.InvalidArgument, err.Error())`
  （参照 game handler:49），不走 toStatusError。
  domain 构造错误走 toStatusError（ErrInvalidName → InvalidArgument）。

  (C) 消除 parseParent，内联到 2 个调用点：
  - ListEnvironments（:197）: `ParseScopeName(req.GetParent())` → 若
    `ContainsWildcard()` 则跳过校验用 "-" → 否则 `domain.ValidateScope` → ListByScope
  - CreateEnvironment（:225）: `ParseScopeName(req.GetParent())` → 若
    `ContainsWildcard()` 则显式拒绝（InvalidArgument, "scope wildcard '-' is not
    allowed for create"）→ 否则 `domain.ValidateScope` → `NewEnvironmentName`

  (D) 移除死代码：
  - 删除 `fromProtoEnvironment` 函数（:324-340，全仓库无调用者）
  - 删除 `parseParent` 函数（:775-797，已内联）

  (E) 移除 domain 手写解析函数：
  - 删除 `domain.ParseResourceName`（environment_name.go:27-34）
  - 删除 `domain.ParseServiceEndpointsName`（service_endpoints_name.go:53-60）

  (F) 更新测试：
  - handler_test.go:456: `domain.ParseResourceName("...")` → `domain.NewEnvironmentName("dev", "alpha")`
  - handler_test.go T005 通配符测试: CreateEnvironment 的 parent "deploy/scopes/-"
    断言 wantCode 仍为 codes.InvalidArgument（行为等价，见 plan-v2 D4 验证表）
  - 删除 domain/environment_name_test.go 中的 TestParseResourceName
  - 删除 domain/service_endpoints_name_test.go 中的 TestParseServiceEndpointsName

  参照本方案 D1-D6、game handler 模式（projects/game/session/handler/handler.go:47-53）。
  参考 [AIP-159](https://google.aip.dev/159)、[AIP-122](https://google.aip.dev/122)。
```

### Phase 1 文档清单更新

```
- **代码规范文档**：`style/golang.md`、`style/api.md`
- **官方文档**：
  - [AIP-159: Reading across collections](https://google.aip.dev/159)（通配符模式）
  - [AIP-123: Resource types](https://google.aip.dev/123)（Scope 资源声明）
  - [AIP-122: Resource names](https://google.aip.dev/122)（资源名校验）
  - [grpc-gateway path templates](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/grpc_apidocs_path_templates/)（通配符路由）
- **补充方案**：[plan-v2-codegen-migration.md](plan-v2-codegen-migration.md)
- **技术文章**：无
```

### Phase 1 验证门禁不变

```bash
bazel build //projects/infra/deploy:go_default_library
bazel test //projects/infra/deploy:go_default_test
```

## 验证门禁

```bash
# 1. codegen 确认生成全部方法（ScopeName/EnvironmentName/ServiceEndpointsName + ParseName）
bazel build //projects/infra/deploy:go_default_library

# 2. handler 单测全部通过
bazel test //projects/infra/deploy:go_default_test

# 3. 确认 parseParent / fromProtoEnvironment / domain.ParseResourceName / domain.ParseServiceEndpointsName 已移除
grep -rn "parseParent\|fromProtoEnvironment\|domain\.ParseResourceName\|domain\.ParseServiceEndpointsName" projects/infra/deploy/ # 应仅在注释或历史引用中

# 4. 确认无 strings.CutPrefix 手写解析残留
grep -rn "CutPrefix" projects/infra/deploy/ # 应无输出
```
