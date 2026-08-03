# Tasks: Deploy Scope Removal

**Input**: Design documents from `/specs/033-deploy-scope-cleanup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/deploy-cli.md

**Tests**: Tests are NOT separately assigned as tasks — compile + unit tests are part of each code change task per constitution principle IV. Test updates are included within the relevant implementation tasks.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

- CLI 工具：`tools/release/deploy/v3/`
- 后端 service：`projects/infra/deploy/`
- 文档：`tools/release/deploy/README.md`

## Phase 1: Foundational (Blocking Prerequisites)

**Purpose**: 后端 ListEnvironments AIP-159 通配符支持。此 phase 是 US4（list 跨 scope）的前置依赖，必须先完成。

**⚠️ CRITICAL**: US4 的 CLI list 跨 scope 功能依赖此后端能力。

**文档清单**：
- **代码规范文档**：`style/golang.md`、`style/api.md`
- **官方文档**：[AIP-159: Reading across collections](https://google.aip.dev/159)（通配符模式）、[AIP-123: Resource types](https://google.aip.dev/123)（Scope 资源声明）、[AIP-122: Resource names](https://google.aip.dev/122)（资源名校验）、[grpc-gateway path templates](https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/grpc_apidocs_path_templates/)（通配符路由）
- **补充方案**：[plan-v2-codegen-migration.md](plan-v2-codegen-migration.md)（handler.go 全面 codegen 迁移的决策与验证）
- **技术文章**：无

- [ ] T000 在 `projects/infra/deploy/deploy.proto` 的 `message Environment`（:89）之前新增 `Scope` 消息声明。该资源无标准方法（no CRUD RPCs），仅用于 codegen 生成 `ParseScopeName()` 和 `EnvironmentName.Parent()`，参照 `projects/game/game.proto:176-185` 的 Template 资源模式。具体 proto 语法：
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
  声明后执行 `bazel build //projects/infra/deploy:go_default_library` 确认 codegen 生成 `ParseScopeName` / `ScopeName` / `EnvironmentName.Parent()` 等方法。不修改任何 `google.api.http` 注解（FR-015 约束不受影响，`google.api.resource` 是消息级注解，不是 HTTP 注解）。参考 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D1、[AIP-123](https://google.aip.dev/123) 和 `projects/game/game.proto:176-185`。

- [ ] T001 [P] handler.go 全面迁移到 codegen name 解析（替代之前的 parseParent-only 迁移）。参照 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D1-D6 和 game handler 模式（`projects/game/session/handler/handler.go:47-53`）。具体变更：

  **(A) 新增 `domain.ValidateScope`**：在 `projects/infra/deploy/domain/environment_name.go` 新增导出函数 `ValidateScope(s string) error`，使用现有 `environmentNameRegexp`（`:16`）校验 scope 格式（`^[a-z][a-z0-9]{0,7}$`），失败返回 `ErrInvalidName`。该函数供 handler 中 scope 独立校验使用（parseParent 消除后 ListEnvironments / CreateEnvironment 需要）。

  **(B) 迁移 4 个 handler EnvironmentName / ServiceEndpointsName 解析到 codegen**：
  - `GetEnvironment`（`handler.go:55`）: `domain.ParseResourceName(req.GetName())` → `req.ParseName()`（codegen `GetEnvironmentRequest.ParseName()`，见 `deploy_aip.pb.resource.go:370`）→ `domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)`。codegen parse 错误用 `status.Error(codes.InvalidArgument, err.Error())`（参照 game handler:49），不走 toStatusError。domain 构造错误走 toStatusError。
  - `GetServiceEndpoints`（`handler.go:70`）: `domain.ParseServiceEndpointsName(req.GetName())` → `req.ParseName()`（codegen `GetServiceEndpointsRequest.ParseName()`，见 `:375`）→ `domain.NewServiceEndpointsName(cgName.ScopeID, cgName.EnvNameID, cgName.AppID, cgName.ServiceID)`。变量 `name` 仍是 `domain.ServiceEndpointsName`，下游 `.App()`、`.Service()`、`.EnvLabel()`、`.EnvironmentName()` 调用不变。
  - `UpdateEnvironment`（`handler.go:265`）: `domain.ParseResourceName(req.GetEnvironment().GetName())` → `req.GetEnvironment().ParseName()`（codegen `Environment.ParseName()`，见 `:350`）→ `domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)`。
  - `DeleteEnvironment`（`handler.go:291`）: `domain.ParseResourceName(req.GetName())` → `req.ParseName()`（codegen `DeleteEnvironmentRequest.ParseName()`，见 `:380`）→ `domain.NewEnvironmentName(name.ScopeID, name.EnvNameID)`。

  **(C) 消除 `parseParent`，内联到 2 个调用点**（参照 game 无 parseParent helper 模式）：
  - `ListEnvironments`（`handler.go:197`）: 替换 `parseParent(req.GetParent())` 调用为内联——`scopeName, err := ParseScopeName(req.GetParent())` → codegen 错误用 `status.Error(codes.InvalidArgument, err.Error())` → `scope := scopeName.ScopeID` → 若 `!scopeName.ContainsWildcard()` 则 `domain.ValidateScope(scope)`（错误走 toStatusError）→ `h.repo.ListByScope(ctx, scope, ...)`。
  - `CreateEnvironment`（`handler.go:225`）: 替换 `parseParent(req.GetParent())` 调用为内联——`ParseScopeName(req.GetParent())` → 若 `scopeName.ContainsWildcard()` 则显式拒绝 `status.Error(codes.InvalidArgument, "scope wildcard '-' is not allowed for create")` → `domain.ValidateScope(scopeName.ScopeID)` → `domain.NewEnvironmentName(scopeName.ScopeID, req.GetEnvName())`。行为等价性：旧行为依赖下游 `NewEnvironmentName("-", env)` 失败，新行为入口显式拒绝，两者均返回 InvalidArgument（见 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D4 验证表）。

  **(D) 移除死代码**：
  - 删除 `fromProtoEnvironment` 函数（`handler.go:324-340`，全仓库无调用者，grep 确认）。
  - 删除 `parseParent` 函数（`handler.go:775-797`，已内联到 ListEnvironments / CreateEnvironment）。

  **(E) 移除 domain 手写解析函数**：
  - 删除 `domain.ParseResourceName`（`environment_name.go:27-34`）。
  - 删除 `domain.ParseServiceEndpointsName`（`service_endpoints_name.go:53-60`）。
  - 保留 `domain.NewEnvironmentName` / `domain.NewServiceEndpointsName`（构造函数仍被 handler、storage toDomain、测试使用）。

  **(F) 更新测试**：
  - `handler_test.go:456`: `domain.ParseResourceName("deploy/scopes/dev/environments/alpha")` → `domain.NewEnvironmentName("dev", "alpha")`。
  - 删除 `domain/environment_name_test.go` 中的 `TestParseResourceName`。
  - 删除 `domain/service_endpoints_name_test.go` 中的 `TestParseServiceEndpointsName`。
  - T005 中 CreateEnvironment parent `deploy/scopes/-` 测试：断言 `wantCode` 仍为 `codes.InvalidArgument`（行为等价）。

  **依赖 T000**（Scope 资源声明已完成，codegen 已生成全部所需方法）。参考 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D1-D6、[AIP-159](https://google.aip.dev/159)、[AIP-122](https://google.aip.dev/122)。

- [ ] T002 [P] 扩展 `ListByScope` 支持 `-` scope 时空过滤在 `projects/infra/deploy/storage/mongo.go`。当 scope 参数为 `"-"` 时，使用空 filter（`bson.M{}`）替代 `{mongoFieldScope: scope}` 精确匹配，匹配所有文档。现有逻辑在 `mongo.go:361`（`filter := bson.M{mongoFieldScope: scope}`）——改为条件构建 filter：`var filter bson.M; if scope == "-" { filter = bson.M{} } else { filter = bson.M{mongoFieldScope: scope} }`。参考研究决策 [research.md R1](./research.md)。

- [ ] T003 [P] 扩展 `fakeRepository.ListByScope` 支持 `-` scope 返回所有环境在 `projects/infra/deploy/repository_fake_test.go`。现有逻辑在 `:136-159` 使用 `scopePrefix(scope)` 前缀匹配——当 scope 为 `"-"` 时跳过前缀过滤，返回所有环境（仅排序，不筛选）。参考 [research.md R1](./research.md)。

- [ ] T004 更新 `ListByScope` 接口注释说明 `-` 通配语义在 `projects/infra/deploy/domain/repository.go:22-24`。当前注释为 `// ListByScope lists environments under a scope with pagination.`——追加说明 scope 为 `"-"` 时匹配所有 scope（AIP-159 跨集合读取模式）。同时检查并更新另外 3 个 fake repository 的 `ListByScope` 方法签名注释（`service/command_test.go:108`、`service/reconcile_test.go:176`、`handler_test.go:1949`）——这些 fake 的实现为空或无过滤逻辑，无需修改实现，仅注释一致性检查。

- [ ] T005 [P] 新增 handler 层 `-` 通配符测试用例在 `projects/infra/deploy/handler_test.go` 的 `TestHandler_ListEnvironments` 函数。新增测试用例：seed 包含多个 scope 的环境（如 dev/alpha、prod/beta），request parent 为 `deploy/scopes/-`，验证返回所有 scope 的环境且 name 使用 canonical resource name（如 `deploy/scopes/dev/environments/alpha`、`deploy/scopes/prod/environments/beta`）。**依赖 T003**（fakeRepository.ListByScope 的 `-` 支持，先完成 T003 再执行本用例）。另新增一个用例：`CreateEnvironment` 的 parent 为 `deploy/scopes/-` 时返回 InvalidArgument（验证 T001 中 CreateEnvironment 对通配符的显式拒绝——旧行为依赖下游 `NewEnvironmentName("-", env)` 失败，新行为在入口 `ContainsWildcard()` 检查时拒绝，两者均返回 InvalidArgument，见 [plan-v2-codegen-migration.md](plan-v2-codegen-migration.md) D4 验证表）。参考现有测试用例结构（`:208-284`）和 [quickstart.md 步骤 7](./quickstart.md)。

- [ ] T006 [P] 新增存储层 `-` 通配符测试用例在 `projects/infra/deploy/storage/mongo_test.go` 的 `TestMongoRepository_ListByScope` 函数（或新测试函数）。新增测试：seed 包含 dev 和 prod 两个 scope 的环境，调用 `ListByScope(ctx, "-", pageSize, "")`，验证返回所有环境。**依赖 T002**（mongo ListByScope 的 `-` 空过滤，先完成 T002 再执行本用例）。参考现有测试结构（`:844-984`）。

**Phase 1 验证门禁**：
```bash
bazel build //projects/infra/deploy:go_default_library
bazel test //projects/infra/deploy:go_default_test
```

**Checkpoint**: 后端 ListEnvironments 支持跨 scope 查询。US4 CLI 部分可开始实现。

---

## Phase 2: User Story 1+2+3 — Remove scope command, --scope flag, and require full env names (Priority: P1) 🎯 MVP

**Goal**: 移除 CLI 中 scope 作为独立设计对象的全部逻辑：删除 scope 命令/配置、从 apply/del/describe 移除 --scope flag、del/describe/apply 改为直接使用完整环境名。这三个 story 紧密耦合（同一组文件、同一组函数），合并为一个 phase 实现。

**Independent Test**: 
- `deploy scope` 返回 "unknown command" 错误（US1）
- `deploy del --scope=team alice.dev` 返回 flag 解析错误（US2）
- `deploy del dev` 返回完整格式错误（US3）

**Why merged**: US1/US2/US3 的变更涉及相同的文件集（`main.go`、`identity.go`、`scope.go`、`del.go`、`describe.go`、`apply.go` 及其测试），拆分会导致中间态编译失败。按宪法原则 II（重构式变更），架构调整与功能变更作为同一变更交付。

### Implementation for User Story 1+2+3

**文档清单**：
- **代码规范文档**：`style/golang.md`
- **官方文档**：无
- **技术文章**：无

- [ ] T007 删除 `tools/release/deploy/v3/scope.go` 文件。该文件包含 `cliConfig` 结构体、`cliConfigDir`/`cliConfigFile` 常量、`loadConfig`/`saveConfig` 函数、`scopeCommand` 函数。删除整个文件。参考研究决策 [research.md R6](./research.md)。

- [ ] T008 删除 `tools/release/deploy/v3/scope_test.go` 文件。该文件测试 `loadConfig`/`saveConfig`/`scopeCommand`，随 scope.go 一起移除。

- [ ] T009 [P] [US1] 精简 `tools/release/deploy/v3/identity.go`：移除 `NewFullEnvName`、`IsFullEnvName`、`ValidateScope`、`validateEnvName` 函数，移除 `errNoDefaultScope`、`errInvalidScope` 哨兵错误变量；`errInvalidEnvName` 随 `validateEnvName` 一并移除（无其他调用者，避免死代码）。保留 `ValidateFullEnvName`、`ParseFullEnvName` 函数，保留 `errInvalidFullEnvName`、`envPartRegexp`、`fullEnvRegexp`（`envPartRegexp` 被 apply.go 的 `resolvePlaceholders` 使用，`fullEnvRegexp` 被 `ValidateFullEnvName` 使用）。`errInvalidFullEnvName` 的错误文本须更新为包含格式要求（如 `errors.New("非法完整环境名，须使用 {scope}.{env_name} 格式")`），使 del/describe/apply 的短名错误信息满足 spec.md FR-004/FR-005 与 US3 验收场景（spec.md:76）。`import "strings"` 保留（`ParseFullEnvName` 使用 `strings.Cut`）。参考研究决策 [research.md R2](./research.md)。

- [ ] T010 [US1][US2] 更新 `tools/release/deploy/v3/main.go` 命令注册表和 flag 定义。具体变更：(1) 移除 `commandScope` 常量（:22）；(2) 从 `commandExecTable`（:55-61）移除 `commandScope` 条目；(3) 从 `commandValidatorTable`（:63-69）移除 `commandScope` 条目；(4) 从 `commandFlagTable`（:114-120）的 apply、del、describe 行移除 `flagScope`，保留 list 行的 `flagScope`，移除 `commandScope` 行；(5) 移除 `validateScopeOptions` 函数（:257-262）；(6) 从 `validateOptions`（:209-227）移除 `opts.scope` 的 `ValidateScope` 校验块（:216-220）；从 `parseOptions`（:156-188）移除 `opts.scope = strings.TrimSpace(opts.scope)` 行（:180）；(7) 更新 `parseOptions` 中 "must provide command" 错误消息（:158）移除 scope 引用；(8) 保留 `flagScope` 常量和 `flagSpecs` 中的 `flagScope` 条目（list 仍使用），更新 usage 为 `"scope filter for list command"`；(9) 保留 `options` 结构体的 `scope` 字段。参考研究决策 [research.md R4](./research.md)。

- [ ] T011 [US2] 更新 `tools/release/deploy/v3/main.go` 的 `usageText()` 函数（:277-291）。更新命令列表为：移除 `scope` 行；apply 行移除 `[--scope=name]`；del 行移除 `[--scope=name]`；describe 行移除 `[--scope=name]`；list 行保留 `[--scope=name]`。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md) 的命令格式。

- [ ] T012 [US1][US2][US3] 更新 `tools/release/deploy/v3/del.go` 的 `deleteCommand` 函数。移除 `workspace.MustRoot()` / `loadConfig` 调用（:20-24），移除 scope 解析逻辑（:26-29）。改为直接使用 `opts.target` 作为完整环境名：调用 `ValidateFullEnvName(strings.TrimSpace(opts.target))` 校验格式（短名/格式错误的错误信息由 T009 更新的 `errInvalidFullEnvName` 文本提供，说明须使用完整 `{scope}.{env_name}` 格式），调用 `ParseFullEnvName` 解析 scope+envName，构造 `environmentResourceName(scope, envName)`。`环境 %s 已删除` 输出（:49）中的 `fullEnvName` 改为 `strings.TrimSpace(opts.target)`。移除 `import "dominion/tools/release/deploy/pkg/workspace"`；`import "strings"` 保留（`strings.TrimSpace` 仍在使用）。参考 [contracts/deploy-cli.md del 命令](./contracts/deploy-cli.md) 和 [research.md R6](./research.md)。

- [ ] T013 [US1][US2][US3] 更新 `tools/release/deploy/v3/describe.go` 的 `describeCommand` 函数。移除 `workspace.MustRoot()` / `loadConfig` 调用（:17-22），移除 scope 解析逻辑（:24-27）。改为直接使用 `opts.target` 作为完整环境名：调用 `ValidateFullEnvName(strings.TrimSpace(opts.target))` 校验格式（错误信息含格式要求，见 T009），调用 `ParseFullEnvName` 解析 scope+envName，构造 resource name。`环境 %s 不存在`（:42）与 `printEnvironmentDetail(fullEnvName, …)`（:48）中的 `fullEnvName` 改为 `strings.TrimSpace(opts.target)`。移除 `import "dominion/tools/release/deploy/pkg/workspace"`；`import "strings"` 保留（`strings.TrimSpace` 仍在使用）。参考 [contracts/deploy-cli.md describe 命令](./contracts/deploy-cli.md)。

- [ ] T014 [US2][US3] 更新 `tools/release/deploy/v3/apply.go` 的 `applyCommand` 函数。移除 scope 组合逻辑：将 `NewFullEnvName(opts.scope, strings.TrimSpace(envName))`（:66）改为直接使用 `envName` 作为完整环境名——调用 `ParseFullEnvName(strings.TrimSpace(envName))` 解析 scope+envName（`ParseFullEnvName` 内部已调用 `ValidateFullEnvName` 校验，无需重复校验；格式错误信息由 T009 更新的 `errInvalidFullEnvName` 文本提供）。`scopeResourceName(scope)`（:75）保留（CreateEnvironment 的 parent 仍需要）。`import "strings"` 保留（apply.go 中 strings 还有其他使用）。参考 [contracts/deploy-cli.md apply 命令](./contracts/deploy-cli.md) 和 [research.md R2](./research.md)。

- [ ] T015 [US1][US2][US3] 更新 `tools/release/deploy/v3/main_test.go` 的 `Test_parseOptions` 和 `TestRun_Help`。移除或更新以下测试用例：(1) "apply with endpoint timeout and scope" — 移除 `--scope=team` 和期望的 `scope: "team"` 字段；(2) "apply with run flag" — 移除 `--scope=team`；(3) "list scope flag" — 保留（list 仍有 --scope）；(4) "scope target" 测试用例 — 移除（scope 命令不再存在）；(5) "scope invalid target" — 移除；(6) `TestRun_Help` 的断言更新（如需要）。确保剩余用例的 `options` 结构体期望值不再包含非 list 命令的 scope 字段。参考现有测试结构。

- [ ] T016 [US1][US2][US3] 更新 `tools/release/deploy/v3/del_list_test.go`。对于 `TestDelCommand`：移除测试用例中的 `scope` 字段和 `saveConfig` setup（:94-98），改为直接使用完整环境名（如 `target: "dev.api"` 替代 `target: "api", scope: "dev"`），更新期望的 HTTP 路径。对于 `TestListCommand`：这部分在 US4 处理，此 task 仅处理 del 相关部分。该文件当前未引入 `workspace` 包，无需改动 import。**新增回归测试 `TestNoConfigFileAccess`（同文件，覆盖 del/describe/list 三个命令，describe 复用 describe_test.go 的桩）**：在 `newDelListWorkspace` 工作区中执行命令后断言 (1) 工作区不创建 `.env` 目录或 `cli.json` 文件；(2) 预置含 `default_scope` 的 `.env/cli.json` 时命令行为不受影响（配置被忽略）。满足 spec.md US1 验收场景 2（spec.md:47）。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md)。

- [ ] T017 [US1][US2][US3] 更新 `tools/release/deploy/v3/describe_test.go`。移除测试用例中的 `scope` 字段和 `defaultScope` 字段及对应的 `saveConfig` setup（:360-364），改为直接使用完整环境名 `target`（如 `target: "dev.api"` 替代 `target: "api", scope: "dev"`）。所有测试用例的 HTTP handler path 保持或更新为对应的完整环境名路径。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md)。

- [ ] T018 [US2][US3] 更新 `tools/release/deploy/v3/apply_test.go`。移除测试中的 `scope: "team"` opts 字段（:81, :106, :135, :168），因为 apply 不再接受 scope。deploy.yaml 中的 `name: team.dev` 保持不变（已是完整名，apply 直接使用）。验证 HTTP 路径 `/v1/deploy/scopes/team/environments/dev` 保持正确（从 deploy.yaml name 解析 scope=team, envName=dev）。

- [ ] T019 [P] [US1] 更新 `tools/release/deploy/v3/BUILD.bazel`：从 `go_library` 的 `srcs` 移除 `"scope.go"`，从 `go_unittest` 的 `srcs` 移除 `"scope_test.go"`。执行 `bazel run //:gazelle tools/release/deploy/v3` 自动更新。也可手动编辑后验证。

**Phase 2 验证门禁**：
```bash
bazel run //:gazelle tools/release/deploy/v3
bazel build //tools/release/deploy/v3:deploy_v3
bazel test //tools/release/deploy/v3:deploy_test
```

**Checkpoint**: CLI 的 scope 命令、--scope flag（apply/del/describe）、默认 scope 配置全部移除。del/describe/apply 使用完整环境名。scope 命令返回 unknown command，--scope 返回 flag 解析错误，短名返回格式错误。

---

## Phase 3: User Story 4 — List with optional --scope and cross-scope listing (Priority: P2)

**Goal**: list 命令保留 `--scope` 作为可选过滤参数；不指定时 CLI 发送 `-` 通配符列出所有 scope 的环境。输出使用响应中的实际完整环境名。

**Independent Test**: `deploy list` 列出所有 scope 的环境；`deploy list --scope=alice` 只列出 alice scope 的环境。

**Prerequisites**: Phase 1 完成（后端 `-` 通配符支持）。Phase 2 完成（main.go 中 scope 校验已移除）。

### Implementation for User Story 4

**文档清单**：
- **代码规范文档**：`style/golang.md`
- **官方文档**：[AIP-159: Reading across collections](https://google.aip.dev/159)（list 输出使用 canonical 环境名的规范依据，FR-008）
- **技术文章**：无

- [ ] T020 [US4] 更新 `tools/release/deploy/v3/list.go` 的 `listCommand` 函数。移除 `workspace.MustRoot()` / `loadConfig` 调用（:12-16），移除默认 scope 回退逻辑和 `errNoDefaultScope` 检查（:18-24）。改为：scope 取 `opts.scope` 值（已 trim），如为空则设为 `"-"`（通配符）。移除 `ValidateScope(scope)` 调用（:25-27，校验已在 validateListOptions 中完成）。调用 `ListEnvironments(ctx, scopeResourceName(scope))`（不变）。输出行改为从 `environment.Name` 解析完整环境名：`scope, envName := parseEnvironmentResourceName(environment.Name); line := scope + "." + envName`（替代当前 `:39` 的 `line := scope + "." + envName`，因为 scope 变量在跨 scope 模式下为 `"-"`）。移除 `import "dominion/tools/release/deploy/pkg/workspace"`。参考 [contracts/deploy-cli.md list 命令](./contracts/deploy-cli.md) 和 [research.md R3](./research.md)。

- [ ] T021 [US4] 更新 `tools/release/deploy/v3/main.go` 的 `validateListOptions` 函数（:250-255）。保留"不接受位置参数"校验。新增 `--scope` 值的局部校验：当 `opts.scope != ""` 时，使用 `envPartRegexp.MatchString(opts.scope)` 校验格式，失败返回 `fmt.Errorf("非法 scope: %q", opts.scope)`。注意 `errInvalidScope` 已在 T009 中移除，需用 `fmt.Errorf` 直接构造错误。参考研究决策 [research.md R5](./research.md)。

- [ ] T022 [US4] 更新 `tools/release/deploy/v3/del_list_test.go` 的 `TestListCommand`。更新测试用例：(1) 移除 `saveConfig` setup（:183-186, :200-203）；(2) 现有 "success with environments" 等用例的 scope 改为通过 `opts.scope` 传入（而非 saveConfig），HTTP handler 保持校验 `/v1/deploy/scopes/dev/environments`；(3) 新增 "cross-scope listing" 测试用例：不传 scope（`opts.scope` 为空），mock server 校验 path 为 `/v1/deploy/scopes/-/environments`，返回多个 scope 的环境，验证输出包含实际完整环境名（如 `alice.dev`、`bob.prod`）而非 `-`；(4) 移除 "no scope error" 测试用例（:159-163，不再报错而是跨 scope 查询）；(5) 该文件当前未引入 `workspace` 包，无需改动 import。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md) 和 [spec.md US4 验收场景](./spec.md)。

**Phase 3 验证门禁**：
```bash
bazel build //tools/release/deploy/v3:deploy_v3
bazel test //tools/release/deploy/v3:deploy_test
```

**Checkpoint**: list 不指定 --scope 时跨 scope 列出所有环境，输出使用实际完整环境名。

---

## Phase 4: Polish & Documentation

**Purpose**: 更新文档和帮助文本，确保一致性。

**文档清单**：
- **代码规范文档**：`style/golang.md`
- **官方文档**：无
- **技术文章**：无

- [ ] T023 [P] 更新 `tools/release/deploy/README.md` 的"命令"小节（:237-320）。具体变更：(1) 移除"配置默认 scope"小节（:299-306）的 `deploy scope` 命令文档；(2) "删除环境"小节（:263-269）移除"支持简版名（`dev`，需配置默认 scope）"，改为说明须使用完整环境名 `{scope}.{env_name}`；(3) "查看环境详情"小节（:277-283）移除 `[--scope=name]`；(4) "部署/更新"小节（:255-261）确认无 `--scope`（当前已无，检查即可）；(5) 新增 `list` 命令的 `--scope` 可选说明和 `deploy list` 列出所有 scope 环境的说明（当前 :271-275 仅有 `deploy list`，补充 `--scope` 用法）。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md)。

- [ ] T024 [P] 更新 `tools/release/deploy/README.md` 的"环境名格式"小节（:314-320）。移除"输入含 `.` 视为完整环境名，否则视为简版名（需默认 scope）"，改为说明环境名始终使用完整 `{scope}.{env_name}` 格式。保留 `scope` 和 `env_name` 格式规则和 `{{run}}` 占位符说明。参考 [contracts/deploy-cli.md](./contracts/deploy-cli.md)。

- [ ] T025 运行 `bazel run //:gazelle` 确认所有 BUILD.bazel 文件为最新状态（Phase 2/3 代码变更后）。验证 `bazel build //tools/release/deploy/v3:deploy_v3` 和 `bazel build //projects/infra/deploy:go_default_library` 编译通过。

**Phase 4 验证门禁**：
```bash
bazel build //tools/release/deploy/v3:deploy_v3
bazel test //tools/release/deploy/v3:deploy_test
bazel test //projects/infra/deploy:go_default_test
```

**Checkpoint**: 所有文档更新完成，编译和单测全部通过。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（后端通配符）**: 无依赖，可立即开始。BLOCKS Phase 3（US4 list 跨 scope 需要后端支持）。
- **Phase 2（US1+2+3 CLI 移除）**: 无依赖（不涉及后端）。可与 Phase 1 并行（不同代码库区域）。
- **Phase 3（US4 CLI list）**: 依赖 Phase 1（后端 `-` 支持）和 Phase 2（main.go scope 校验已移除）。
- **Phase 4（文档）**: 依赖 Phase 2 和 Phase 3 完成。

### User Story Dependencies

- **US1（移除 scope 命令）+ US2（移除 --scope flag）+ US3（完整名要求）**: 合并在 Phase 2 中实现，三者变更同一组文件，不可拆分。
- **US4（list 跨 scope）**: 依赖 US1+2+3 的 main.go 变更（scope 校验移除）和 Phase 1 的后端变更。

### Parallel Opportunities

- **Phase 1 内部**：T000 是 T001 的前置依赖（Scope 资源声明 → codegen 生成 → parseParent 迁移）。T000 完成后，T001/T002/T003 可并行（不同文件）。T005/T006 可并行（不同测试文件）。
- **Phase 1 与 Phase 2 可并行**：Phase 1 改后端（`projects/infra/deploy/`），Phase 2 改 CLI（`tools/release/deploy/v3/`），无文件冲突。
- **Phase 2 内部**：T009（identity.go）可与其他文件并行，但 T010-T014 有顺序依赖（main.go 变更影响其他文件的编译）。
- **Phase 4 内部**：T023/T024 可并行（同一文件不同小节，但需注意不冲突——建议顺序执行或合并）。

---

## Parallel Example: Phase 1 + Phase 2

```bash
# 后端变更（Phase 1）和 CLI 变更（Phase 2）可由不同开发者并行：
# Developer A: Phase 1 — 后端 AIP-159 通配符 + codegen 全面迁移
Task: "T000 在 deploy.proto 声明 Scope 资源"
Task: "T001 handler.go 全面迁移到 codegen name 解析（依赖 T000）"
Task: "T002 扩展 ListByScope 时空过滤在 mongo.go"

# Developer B: Phase 2 — CLI scope 移除
Task: "T007 删除 scope.go"
Task: "T008 删除 scope_test.go"
Task: "T009 精简 identity.go"
```

---

## Implementation Strategy

### MVP First (Phase 1 + Phase 2)

1. 完成 Phase 1：后端 ListEnvironments 支持跨 scope
2. 完成 Phase 2：CLI 移除 scope 命令/flag/配置，使用完整环境名
3. **STOP and VALIDATE**：
   - `deploy scope` → unknown command ✅
   - `deploy del --scope=team alice.dev` → flag error ✅
   - `deploy del dev` → 格式错误 ✅
   - `deploy describe alice.dev` → 正常工作 ✅
   - `deploy apply` → 直接使用 deploy.yaml name ✅

### Incremental Delivery

1. Phase 1 + Phase 2 → MVP（scope 移除 + 完整环境名）
2. Phase 3 → list 跨 scope 功能
3. Phase 4 → 文档更新

---

## Notes

- 编译 + 单测是每个 task 的一部分（宪法原则 IV），不单列 task。
- 每个 task 完成后执行 `bazel build` + `bazel test` 验证相关 target。
- `bazel run //:gazelle` 用于更新 BUILD.bazel（删除文件后必须执行）。
- 代码格式化使用 `bazel run //:go -- fmt [变更文件]`。
- 引用必须包含来源（宪法原则 I）——代码注释引用 spec 时使用完整路径（如 `specs/033-deploy-scope-cleanup/spec.md`）。
- T023/T024 虽然在不同小节但同一文件，建议合并执行以避免 edit 冲突。
