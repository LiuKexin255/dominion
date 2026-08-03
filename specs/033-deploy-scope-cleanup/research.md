# Research: Deploy Scope Removal

**Feature**: 033-deploy-scope-cleanup
**Date**: 2026-08-03
**Spec**: [spec.md](spec.md)

## R1: AIP-159 跨集合读取模式在 ListEnvironments 中的适用性

### Decision

采用 [AIP-159: Reading across collections](https://google.aip.dev/159) 的 `-` 通配符模式，使 `ListEnvironments` 支持 parent = `deploy/scopes/-` 时返回所有 scope 下的环境。

### Rationale

- AIP-159 是 Google API 改进提案中定义的**标准跨集合读取模式**，与本仓库的 `style/api.md`（要求遵循 [Google AIP 规范](https://google.aip.dev/general)）完全契合。
- 现有 proto HTTP 注解 `{parent=deploy/scopes/*}/environments` 的 `*` 通配符在 grpc-gateway 中匹配**单个非空路径段**，`-` 是合法的单字符路径段值，因此 `GET /v1/deploy/scopes/-/environments` 能正确路由到 `ListEnvironments` RPC。**无需修改 proto 文件**（FR-015）。
- AIP-159 明确规定：URI pattern 必须用 `*` 而非硬编码 `-`；响应中的资源名必须使用 canonical name（实际 scope，而非 `-`）。现有实现通过 `toProtoEnvironment(env)` 使用 `env.Name().String()` 返回 canonical name，天然满足此要求。
- grpc-gateway 的路径模板 `*` 语义：匹配一个非空路径段。`-` 是单字符段，被正确匹配。已通过 grpc-gateway 文档和社区实践验证（[grpc-gateway path templates](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/grpc_apidoc_path_templates/)）。

### Alternatives considered

1. **新增 `ListAllEnvironments` RPC**：违反 AIP 标准方法的资源导向设计原则（AIP-132），增加 API 表面积。拒绝。
2. **修改 `ListByScope` 支持空 scope（`""`）**：空 scope 与"不指定 scope"语义不明确；现有存储层用 scope 精确匹配，空字符串匹配不到任何文档。需要额外语义约定。不如 AIP-159 的 `-` 通配符语义清晰。拒绝。
3. **CLI 端发现所有 scope 再逐个 list**：需要额外的"list scopes"能力，后端无此 RPC；且产生 N 次请求，效率低。拒绝。

### 实现要点

- **handler 层** `parseParent` 函数（`projects/infra/deploy/handler.go:777-789`）：当 scope 为 `-` 时跳过 `domain.NewEnvironmentName` 校验（`-` 不匹配 `^[a-z][a-z0-9]{0,7}$` regex），直接返回 `-`。
- **存储层** `ListByScope` 函数（`projects/infra/deploy/storage/mongo.go:350-397`）：当 scope 为 `-` 时，使用空 filter（`bson.M{}`）而非 `{mongoFieldScope: "-"}`，匹配所有文档。
- **fake repository** `ListByScope`（`projects/infra/deploy/repository_fake_test.go:136`）：当 scope 为 `-` 时，跳过前缀过滤返回所有环境。
- 响应中环境名始终使用 canonical name（`toProtoEnvironment` 已保证），满足 AIP-159。

## R2: identity.go 环境名校验函数的精简策略

### Decision

保留 `ValidateFullEnvName` 和 `ParseFullEnvName` 函数（del/describe/apply 命令仍需校验和解析完整环境名），移除 `NewFullEnvName`、`IsFullEnvName`、`ValidateScope`、`validateEnvName` 函数（这些是 scope 组合逻辑的组成部分）。

### Rationale

- `ValidateFullEnvName(name string) error`：校验完整环境名格式 `^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`。del 和 describe 命令使用它拒绝短名和格式错误。
- `ParseFullEnvName(name string) (scope, envName string, err error)`：解析完整名为 scope 和 envName 两部分，用于构造后端资源名。所有命令使用它。
- `NewFullEnvName(scope, name string) (string, error)`：scope + name 组合逻辑的核心，移除后不再有调用者。
- `IsFullEnvName(name string) bool`：检测是否为完整名（含 `.`），是 `NewFullEnvName` 的辅助函数，移除。
- `ValidateScope(scope string) error`：独立 scope 校验，移除后不再有调用者（`validateOptions` 中的 scope 校验也会移除）。
- `validateEnvName(name string) error`：短名校验，是 `NewFullEnvName` 的辅助函数，移除。

保留的函数仍需要 `envPartRegexp`（用于 fullEnvRegexp 的组成部分）和 `fullEnvRegexp`。`errInvalidScope` 和 `errNoDefaultScope` 哨兵错误移除，`errInvalidFullEnvName` 保留。

### Alternatives considered

1. **将校验逻辑内联到各命令中**：违反 DRY 原则，三处重复相同的校验代码。拒绝。
2. **完全移除 identity.go**：`ValidateFullEnvName` 和 `ParseFullEnvName` 有明确复用价值，不属于 scope 设计对象。拒绝。

## R3: list.go 的 scope 可选参数处理

### Decision

`list` 命令保留 `--scope` 作为可选 flag。指定时校验并使用该 scope；不指定时使用字符串 `"-"` 作为 scope 通配符，构造 parent `deploy/scopes/-` 调用 `ListEnvironments` API。

### Rationale

- `list` 命令的 flag 列表中保留 `flagScope`，其余命令（apply、del、describe）从 flag 列表中移除 `flagScope`。
- `options` 结构体保留 `scope` 字段（list 命令仍使用）；apply/del/describe 命令不再读取该字段。
- `validateOptions` 中移除对 `opts.scope` 的全局校验（`ValidateScope(opts.scope)`），改为在 `validateListOptions` 中局部校验 `--scope` 值。
- `listCommand` 输出环境名时，从后端响应中解析 canonical resource name 获取实际 scope（`parseEnvironmentResourceName`），而非使用用户输入的 scope 或 `-`。这满足 FR-008 和 AIP-159 规定。
- `listCommand` 不再加载 `.env/cli.json` 配置，也不需要 `workspace.MustRoot()`（除非其他逻辑需要——检查后确认 `loadConfig` 是 list 中唯一需要 workspace 的调用，移除后 list 不再依赖 workspace）。

### Alternatives considered

1. **list 位置参数接受完整环境名**：与 spec User Story 4 的设计一致但被用户否决——用户选择保留 `--scope` flag 形式。
2. **list 不指定 scope 时报错**：用户明确要求"不填则返回所有 scope 的环境"。

## R4: main.go 命令注册表的精简

### Decision

从 `commandExecTable`、`commandValidatorTable`、`commandFlagTable` 中移除 `commandScope` 条目。从 `flagSpecs` 中保留 `flagScope`（list 仍用）。从 `options` 结构体保留 `scope` 字段（list 仍用）。移除 `flagScope` 的 flagSpec usage 更新为仅 list 命令使用。

### Rationale

- `commandScope` 常量移除。`parseOptions` 中的 "must provide command" 错误消息更新为不含 scope。
- `validateOptions` 中移除 `opts.scope` 的全局 `ValidateScope` 校验，因为非 list 命令不再有 scope 字段值（即使残留也不应校验）。
- `validateScopeOptions` 函数移除。
- `usageText()` 更新为反映新的命令集。
- `commandFlagTable` 中 apply/del/describe 的 flag 列表移除 `flagScope`，仅 list 保留。

### Alternatives considered

无实质替代方案。这是移除 scope 命令的直接后果。

## R5: `--scope` flag 值校验策略

### Decision

list 命令的 `--scope` 值在 `validateListOptions` 中使用 `envPartRegexp` 校验格式（`^[a-z][a-z0-9]{0,7}$`），校验失败返回明确错误。移除 `validateOptions` 中的全局 scope 校验。

### Rationale

- list 的 `--scope` 是唯一保留 scope 输入的入口，校验应在该命令的 validator 中完成。
- 使用 `envPartRegexp` 直接校验，而非调用已移除的 `ValidateScope` 函数。由于 `envPartRegexp` 在 identity.go 中保留（fullEnvRegexp 的组成部分），可直接使用。

## R6: scope.go 文件删除后的依赖清理

### Decision

完全删除 `tools/release/deploy/v3/scope.go` 文件。该文件包含 `cliConfig` 结构体、`cliConfigDir`/`cliConfigFile` 常量、`loadConfig`/`saveConfig` 函数、`scopeCommand` 函数。

### Rationale

- `loadConfig` 被 del.go、list.go、describe.go 调用用于读取默认 scope——移除 scope 概念后不再需要。
- `saveConfig` 仅被 `scopeCommand` 调用——随 `scopeCommand` 一起移除。
- `workspace` 包的 `MustRoot` 在 del/describe 中仅用于 `loadConfig`，移除后这两个命令不再依赖 workspace 包（apply 仍需 workspace 用于路径解析）。
- `scope_test.go` 文件（测试 `loadConfig`/`saveConfig`/`scopeCommand`）一并删除。

### 影响分析

- `import "dominion/tools/release/deploy/pkg/workspace"` 从 del.go 和 describe.go 中移除（list.go 中也会移除）。
- BUILD.bazel 中 `scope.go` 从 `srcs` 移除，`scope_test.go` 从测试 `srcs` 移除。通过 `bazel run //:gazelle` 自动更新。

## R7: handler.go 全面迁移到 codegen name 解析

### Decision

handler.go 中**所有** name 解析统一使用 codegen 生成的方法（`req.ParseName()`、`ParseScopeName`），不保留任何 domain 手写解析（`domain.ParseResourceName`、`domain.ParseServiceEndpointsName`）。`parseParent` 辅助函数消除，逻辑内联到 ListEnvironments / CreateEnvironment 调用点。死代码 `fromProtoEnvironment` 移除。domain 值对象保留。

### Rationale

- 用户 review 反馈：`domain.ParseResourceName` 仍被大范围使用（handler.go 5 处），`parseParent` 用 `domain.NewEnvironmentName(scope, "env")` 的 dummy 值触发 scope regex 校验是副作用利用，不符合宪法原则 II 的重构式变更要求。
- codegen 已生成完整 API（确认于 `deploy_aip.pb.resource.go`）：请求级 `ParseName()`（GetEnvironmentRequest、GetServiceEndpointsRequest、DeleteEnvironmentRequest）、消息级 `Environment.ParseName()`、`ParseScopeName` / `ParseEnvironmentName` / `ParseServiceEndpointsName`。
- **业务校验非副作用**：codegen 结构校验后，`domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)` 构造真实 domain 类型——regex 校验是构造的一部分，不是副作用（与 parseParent 用 dummy "env" 值有本质区别）。
- **scope 独立校验**：新增 `domain.ValidateScope(s string) error` 函数，替代 parseParent 中 `NewEnvironmentName(scope, "env")` 的副作用校验。
- **parseParent 消除**：参照 game handler（无 parseParent helper，每个方法内联 codegen 解析）。parseParent 仅被 ListEnvironments 和 CreateEnvironment 共用，消除后两处各自内联，行为等价（见 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D4 验证表）。
- **CreateEnvironment 通配符**：旧行为 `deploy/scopes/-` → parseParent 返回 "-" → 下游 `NewEnvironmentName("-", env)` 失败。新行为入口 `ContainsWildcard()` 显式拒绝。两者均返回 InvalidArgument，新行为更明确。
- **domain 值对象保留**：`domain.EnvironmentName` 在 repository 接口、storage、service、runtime、queue map key（47 处引用）中深度使用。彻底移除需改动 20+ 文件接口签名，远超 feature 033 范围。handler 边界用 codegen，内部保留 domain 类型——类型转换通过 `domain.NewXxxName` 构造函数完成。
- **game 范式参照**：game handler 在边界用 `game.ParseTemplateName` + `gameconst.ValidateTemplateName`，传裸 string 给下游（`tplName.TemplateID`）。deploy 采用类似模式——边界用 codegen `req.ParseName()` + `domain.NewXxxName` 构造，传 domain 类型给下游（保留类型安全）。

### Alternatives considered

1. **仅迁移 parseParent**（之前的 plan-supplement-codegen.md）：保留大量 domain 手写解析，不符合宪法原则 II 重构式变更。
2. **彻底移除 domain 值对象（完整 game 范式）**：所有层改用裸 string。影响 20+ 文件、大量接口签名变更，回归风险高，远超 feature 033 的 scope removal 目标。domain 值对象提供 queue map key 的可比较性、aggregate 封装、`.Label()` / `.String()` 格式化等价值。
3. **保留 parseParent 但正确实现**：用户明确选择消除 parseParent、内联到调用点。parseParent 被消除后，ListEnvironments 允许通配符、CreateEnvironment 显式拒绝通配符——两处行为差异使内联比共用 helper 更清晰。

详见 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md)。
