# Plan Supplement: handler.go 迁移到 codegen name 解析

> **⚠️ SUPERSEDED**: 本文档已被 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) 替代。
> 新方案将迁移范围从仅 `parseParent` 扩展到 handler.go 中**所有** name 解析（6 个迁移点），
> 并消除了 `parseParent` 辅助函数、移除了死代码。本文档保留作为历史参考。

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Status**: Supplement to plan.md / tasks.md
**Spec**: [spec.md](spec.md)

## 背景

在 Phase 1（T001-T006）实现完成后，review 阶段用户提出反馈：**handler.go 中 `parseParent` 函数使用 `strings.CutPrefix` 手写解析 parent 路径，应该改为使用 codegen 生成的解析方法**，与仓库中 `projects/game` 的惯例一致（所有 handler 使用 codegen 生成的 `ParseXxxName()` 方法解析资源名，而非手写字符串处理）。

本补充方案明确以下决策：

1. proto 变更方案（声明 Scope 资源）
2. 迁移范围（仅 parseParent，还是包括全部 name 解析）
3. codegen 方法与 domain 层业务校验的关系
4. FR-015 约束影响评估
5. 需要更新的 spec/plan/tasks 文件及具体内容

## 调研依据

### codegen 插件行为验证

**插件版本**：`protoc-contrib/protoc-gen-go-aip` v0.1.3（go.mod `tool` 指令锁定）

**插件源码确认**（`internal/generator/resource/generator.go`）：

| 机制 | 源码位置 | 行为 |
|------|----------|------|
| `Parent()` 生成条件 | `lookupParent()` (line 346-382) + `parentPattern()` (line 662-667) | 子资源 pattern 去掉尾部 `collection/{variable}` 后，若前缀匹配同一编译单元内已声明的资源 pattern，则生成 `Parent()` 方法。当前 deploy.proto 未声明 Scope 资源 → `EnvironmentName.Parent()` **不生成**。 |
| `ParseScopeName()` 生成条件 | `emitPatternStruct()` (line 401-505) | 消息级 `google.api.resource` 注解声明资源后即生成。 |
| `child_type` 字段跳过 | `fieldReference()` (line 835-868) | 仅 `child_type`（无 `type`）的字段**静默跳过**，不生成 `ParseParent()`。`ListEnvironmentsRequest.parent` 仅 `child_type` → 无 `ParseParent()`。 |
| `ContainsWildcard()` | `emitContainsWildcard()` (line 563-588) | 每个资源均生成。`ScopeName.ContainsWildcard()` 返回 `n.ScopeID == "-"`。 |
| 通配符 `-` 的解析 | `ParseScopeName` 生成体 (line 421-448) | `strings.Split("deploy/scopes/-", "/")` → 3 段 → 校验非空 → `ScopeID = "-"`，**解析成功**。 |

**现有 codegen 输出**（`bazel-bin/projects/infra/deploy/deploy_go_proto_/.../deploy_aip.pb.resource.go`）：

- 已生成：`EnvironmentName`、`ParseEnvironmentName()`、`ServiceEndpointsName`、`ParseServiceEndpointsName()`
- 已生成：`GetEnvironmentRequest.ParseName()`、`DeleteEnvironmentRequest.ParseName()`
- 未生成：`ScopeName`、`ParseScopeName()`（因 Scope 资源未声明）
- 未生成：`EnvironmentName.Parent()`（因 Scope 资源未声明）

**仓库 precedent**：`projects/game/game.proto:176-185` 的 `Template` 资源——一个无标准方法（no CRUD RPCs）的资源，仅作为 codegen 的 pattern 声明存在。game handler（`projects/game/session/handler/handler.go:47`）通过 `game.ParseTemplateName(req.GetParent())` 解析 parent，而非手写字符串处理。

### game handler 叠加模式

game handler 展示了 codegen + 业务校验叠加的标准模式：

```go
// codegen 解析（结构校验）
tplName, err := game.ParseTemplateName(req.GetParent())
if err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
// 业务校验叠加在 codegen 之上
if err := gameconst.ValidateTemplateName(tplName); err != nil {
    return nil, status.Error(codes.InvalidArgument, err.Error())
}
```

codegen 只做结构校验（非空、无 `/`、段数正确），业务约束（如已知模板集合校验）由 domain 辅助层完成。

## 决策

### D1: 在 deploy.proto 声明 Scope 资源

**Decision**: 新增 `Scope` 消息，声明 `google.api.resource`，pattern `deploy/scopes/{scope}`。

**Rationale**:
- 直接 precedent：`projects/game/game.proto:176-185` Template 资源——无 CRUD RPCs 的纯声明性资源
- 声明后 codegen 生成 `ParseScopeName()`，替代手写 `strings.CutPrefix` 逻辑
- 声明后 codegen 生成 `EnvironmentName.Parent() ScopeName`，增强类型安全
- 符合宪法原则 II（重构式变更）：消除手写解析代码，统一为 codegen 方式

**proto 语法**（消息级 `google.api.resource`，插入在 `message Environment` 之前）：

```proto
// Scope is the collection-level resource that groups environments. It has no
// standard methods (no CRUD RPCs): it exists for resource-hierarchy typing
// (google.api.resource_reference) and to drive codegen of ParseScopeName, so
// parent parsing is fully codegen-driven — no hand-written string prefix
// stripping. Mirrors the Template resource pattern in projects/game.
message Scope {
  option (google.api.resource) = {
    type: "deploy.infra.liukexin.com/Scope"
    pattern: "deploy/scopes/{scope}"
    singular: "scope"
    plural: "scopes"
  };

  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
}
```

**codegen 将生成的方法**（声明 Scope 后）：

| 方法 | 说明 |
|------|------|
| `ScopeName{ScopeID string}` | Scope 资源名结构体 |
| `ParseScopeName(s string) (ScopeName, error)` | 解析 `deploy/scopes/{scope}`（3 段） |
| `ParseFullScopeName(s string) (ScopeName, error)` | 解析全限定名（`//deploy.infra.liukexin.com/` 前缀） |
| `ScopeName.String()` | 返回 `deploy/scopes/{scope}` |
| `ScopeName.FullName()` | 返回 `//deploy.infra.liukexin.com/deploy/scopes/{scope}` |
| `ScopeName.Validate()` | 结构校验（非空、无 `/`） |
| `ScopeName.ContainsWildcard()` | 返回 `n.ScopeID == "-"` |
| `ScopeName.Type()` / `.Pattern()` | 资源类型与 pattern 字符串 |
| `EnvironmentName.Parent() ScopeName` | **新增**——从 Environment 提取 parent Scope |
| `ScopeName.EnvironmentName(envNameID string) EnvironmentName` | **新增**——parent 构造子 |

### D2: 迁移范围——仅 parseParent

**Decision**: 本 feature（033）仅迁移 `parseParent` 函数到 codegen。handler.go 中其他 name 解析（`domain.ParseResourceName`、`domain.ParseServiceEndpointsName`）保持不变。

**Rationale**:
- 用户反馈明确指向 `parseParent`（"`strings.CutPrefix` 应该是 codegen 之前的代码"）
- `parseParent` 是唯一使用 `strings.CutPrefix` 手写解析的函数
- 其他 name 解析（`domain.ParseResourceName` 等）使用 domain 层 regex 校验，有独立的业务约束（`^[a-z][a-z0-9]{0,7}$`），迁移需要同步改造 domain 层类型——这超出 feature 033 的范围
- 声明 Scope 资源后，codegen 会额外生成 `EnvironmentName.Parent()` 和 `GetEnvironmentRequest.ParseName()` 等方法，为将来全面迁移铺路，但本 feature 不强制使用它们

**不迁移的函数清单**（保持 domain 层调用不变）：
- `GetEnvironment`（handler.go:57）：`domain.ParseResourceName(req.GetName())`
- `GetServiceEndpoints`（handler.go:72）：`domain.ParseServiceEndpointsName(req.GetName())`
- `UpdateEnvironment`（handler.go:267）：`domain.ParseResourceName(req.GetEnvironment().GetName())`
- `DeleteEnvironment`（handler.go:293）：`domain.ParseResourceName(req.GetName())`
- `fromProtoEnvironment`（handler.go:331）：`domain.ParseResourceName(env.GetName())`

### D3: codegen + domain 层叠加模式

**Decision**: `parseParent` 改为 codegen `ParseScopeName` 解析 + domain 层业务校验（scope regex），AIP-159 通配符 `-` 用 `ContainsWildcard()` 识别后跳过业务校验。

**迁移后的 `parseParent` 伪代码**：

```go
func parseParent(parent string) (string, error) {
    // codegen 结构校验
    scopeName, err := ParseScopeName(parent)
    if err != nil {
        return "", domain.ErrInvalidName
    }

    // AIP-159 通配符：跳过业务校验，直接传递
    if scopeName.ContainsWildcard() {
        return scopeName.ScopeID, nil // returns "-"
    }

    // domain 层业务校验（scope regex ^[a-z][a-z0-9]{0,7}$）
    envName, err := domain.NewEnvironmentName(scopeName.ScopeID, "env")
    if err != nil {
        return "", err
    }

    return envName.Scope(), nil
}
```

**行为等价性验证**：

| 输入 | 当前行为（T001） | 迁移后行为 | 一致？ |
|------|------------------|------------|--------|
| `deploy/scopes/dev` | CutPrefix → "dev" → regex pass → "dev" | ParseScopeName → ScopeID="dev" → regex pass → "dev" | ✅ |
| `deploy/scopes/-` | CutPrefix → "-" → wildcard branch → "-" | ParseScopeName → ScopeID="-" → ContainsWildcard → "-" | ✅ |
| `deploy/scopes/INVALID` | CutPrefix → "INVALID" → regex fail → InvalidArgument | ParseScopeName → ScopeID="INVALID" → regex fail → InvalidArgument | ✅ |
| `bad-parent` | CutPrefix → not ok → InvalidArgument | ParseScopeName → split → len≠3 → error → InvalidArgument | ✅ |
| `deploy/scopes/` (empty) | CutPrefix → "" → InvalidArgument | ParseScopeName → empty segment → error → InvalidArgument | ✅ |
| `deploy/scopes/dev/extra` | CutPrefix → "dev/extra" → Contains("/") → InvalidArgument | ParseScopeName → len≠3 → error → InvalidArgument | ✅ |

**附加清理**：`deployParentPrefix` 常量（handler.go:25）仅被 `parseParent` 使用，迁移后可移除。

### D4: FR-015 约束无影响

**Decision**: 在 deploy.proto 新增 `Scope` 消息声明**不属于 FR-015 约束范围**。

**分析**：
- FR-015（spec.md:133）原文："proto 文件 `projects/infra/deploy/deploy.proto` 的 `ListEnvironments` **HTTP 注解**不得修改"
- `google.api.http` 注解是 **RPC 方法级**注解（service 定义内），定义 HTTP 路由
- `google.api.resource` 注解是**资源消息级**注解（message 定义内），定义资源类型元数据
- 新增 `Scope` 消息（含 `google.api.resource`）不修改任何 RPC 的 `google.api.http` 注解
- 所有 HTTP 路由（`{parent=deploy/scopes/*}/environments` 等）保持不变

**无需更新 spec FR-015**。但建议在 plan.md Constraints 中补充说明"新增资源声明不属于 HTTP 注解变更"。

## 需要更新的文档

### spec.md

**FR-016**（handler 层 parseParent 处理）：更新为反映 codegen 方式。

当前（spec.md:134）：
> handler 层 `parseParent` 函数须特殊处理 `-` scope 值：跳过 `domain.NewEnvironmentName` 校验...

更新为：
> handler 层 `parseParent` 函数须使用 codegen 生成的 `ParseScopeName` 解析 parent（而非手写字符串处理），并特殊处理 `-` scope 值：通过 `ScopeName.ContainsWildcard()` 识别通配符后跳过 `domain.NewEnvironmentName` 业务校验，直接传递 `-` 给查询层。

### plan.md

**Constraints**（line 27）：补充 Scope 资源声明说明。

当前：
> **Constraints**: proto HTTP 注解不得修改（FR-015）；deploy.yaml schema 不得修改...

更新为：
> **Constraints**: proto HTTP 注解（`google.api.http`）不得修改（FR-015）；新增资源声明（`google.api.resource`）不属于此约束范围。在 deploy.proto 新增 `Scope` 消息声明以驱动 codegen 生成 `ParseScopeName`（参照 `projects/game/game.proto` Template 资源模式）。deploy.yaml schema 不得修改...

**Scale/Scope**（line 29）：补充 proto 变更。

当前：
> CLI 工具 13 个文件...后端涉及 handler.go、storage/mongo.go...

更新为：
> CLI 工具 13 个文件...后端涉及 deploy.proto（新增 Scope 资源声明）、handler.go（parseParent 迁移到 codegen + 移除 deployParentPrefix 常量）、storage/mongo.go...

### research.md

新增 **R7: handler.go parseParent 迁移到 codegen**：

```markdown
## R7: handler.go parseParent 迁移到 codegen

### Decision

在 deploy.proto 声明 Scope 资源（`google.api.resource`，pattern `deploy/scopes/{scope}`），
驱动 codegen 生成 `ParseScopeName()`，替代 handler.go 中手写的 `strings.CutPrefix` 解析。

### Rationale

- 仓库 precedent：projects/game/game.proto Template 资源（无 CRUD RPCs，纯 codegen 声明），
  game handler 通过 game.ParseTemplateName() 解析 parent。
- codegen 插件（protoc-gen-go-aip v0.1.3）对 child_type 字段不生成 ParseParent()，
  需声明 Scope 资源才能生成 ParseScopeName()（插件源码 fieldReference() line 843-845）。
- codegen 做结构校验（非空、段数、字面量匹配），domain 层做业务校验（scope regex），
  两者叠加，与 game handler 的 gameconst.ValidateTemplateName 模式一致。
- AIP-159 通配符 "-" 与 codegen 完全兼容：ParseScopeName("deploy/scopes/-") 解析成功，
  ScopeName.ContainsWildcard() 返回 true。

### Alternatives considered

1. **保留手写 parseParent**：与仓库惯例不一致（game 全部使用 codegen），违反宪法原则 II。
2. **迁移所有 name 解析到 codegen**：需要同步改造 domain 层 EnvironmentName / ServiceEndpointsName
   类型和 regex 校验逻辑，超出 feature 033 范围。本 feature 仅迁移 parseParent。
```

### data-model.md

在后端资源名小节（"Backend Resource Name"）后补充 Scope 资源声明说明。

### contracts/deploy-cli.md

"handler 层变更"小节（line 149-151）更新为 codegen 方式描述。

### tasks.md

#### Phase 1 变更

**新增 T000（前置 task）**：在 deploy.proto 声明 Scope 资源。

**修改 T001**：`parseParent` 从 `strings.CutPrefix` 改为 `ParseScopeName` + domain 业务校验叠加。

详见下文"tasks.md 具体更新"。

## tasks.md 具体更新

### 新增 T000：在 deploy.proto 声明 Scope 资源

```
- [ ] T000 在 `projects/infra/deploy/deploy.proto` 的 `message Environment`（:89）之前
  新增 `Scope` 消息声明。该资源无标准方法（no CRUD RPCs），仅用于 codegen 生成
  ParseScopeName() 和 EnvironmentName.Parent()，参照 `projects/game/game.proto:176-185`
  的 Template 资源模式。具体 proto 语法：
  message Scope {
    option (google.api.resource) = {
      type: "deploy.infra.liukexin.com/Scope"
      pattern: "deploy/scopes/{scope}"
      singular: "scope"
      plural: "scopes"
    };
    string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  }
  声明后执行 bazel build 确认 codegen 生成 ParseScopeName / ScopeName / Parent() 等方法。
  不修改任何 google.api.http 注解（FR-015 约束不受影响）。
  参考本补充方案 D1 和 AIP-123: https://google.aip.dev/123。
```

### 修改 T001：parseParent 迁移到 codegen

```
- [ ] T001 [P] 重写 `parseParent` 函数（`projects/infra/deploy/handler.go:777`），
  从手写 strings.CutPrefix 改为使用 codegen 生成的 ParseScopeName。
  新逻辑：(1) 调用 ParseScopeName(parent) 做结构校验（段数、字面量、非空）；
  (2) 若 name.ContainsWildcard() 为 true（scope 为 "-"），直接返回 "-" 跳过业务校验
  （AIP-159 通配符）；(3) 否则调用 domain.NewEnvironmentName(name.ScopeID, "env") 做
  scope regex 业务校验（^[a-z][a-z0-9]{0,7}$），返回 envName.Scope()。
  同时移除 deployParentPrefix 常量（handler.go:25，仅 parseParent 使用）。
  parseParent 同时被 ListEnvironments（:199）和 CreateEnvironment（:227）调用，
  行为等价性见 plan-supplement-codegen.md D3 验证表。
  参考本补充方案 D3 和 game handler 模式（projects/game/session/handler/handler.go:47）。
  **依赖 T000**（Scope 资源声明）。
```

### Phase 1 文档清单更新

Phase 1 文档清单新增：
- **官方文档**：[AIP-123: Resource types](https://google.aip.dev/123)（Scope 资源声明规范）

### Phase 1 验证门禁不变

```bash
bazel build //projects/infra/deploy:go_default_library
bazel test //projects/infra/deploy:go_default_test
```

### 对现有测试的影响

现有 T005 已实现的测试用例（handler_test.go）在迁移后行为不变（见 D3 验证表），无需修改测试期望值。codegen 生成的错误消息格式与 domain 层不同（`fmt.Errorf` vs `ErrInvalidName`），但 `toStatusError` 统一映射为 `codes.InvalidArgument`，测试断言 `wantCode: codes.InvalidArgument` 不受影响。

## 验证门禁

迁移完成后验证：

```bash
# 1. codegen 确认生成 ScopeName / ParseScopeName / EnvironmentName.Parent()
bazel build //projects/infra/deploy:go_default_library

# 2. handler 单测全部通过（包括 wildcard 和 invalid parent 用例）
bazel test //projects/infra/deploy:go_default_test

# 3. 确认 deployParentPrefix 已移除且无残留引用
grep -r "deployParentPrefix" projects/infra/deploy/ # 应无输出
```
