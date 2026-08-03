# Feature Specification: Deploy Scope Removal

**Feature Branch**: `033-deploy-scope-cleanup`

**Created**: 2026-08-03

**Status**: Draft

**Input**: User description: "对 @tools/release/deploy/ 工具进行调整，将 `scope` 仅作为 env name 格式内容，不在作为设计的对象。移除所有跟 scope 有关的命令和逻辑，仅保留作为 {scope}.{env_name} 的格式。"

## Background & Context

### 当前状态

deploy CLI 工具（`tools/release/deploy/v3/`）将 "scope" 作为一等概念：
- `deploy scope` 命令用于查看/设置默认 scope（存储于本地 `.env/cli.json`）
- `--scope` 标志存在于所有命令（apply、del、describe、list、scope）
- 短名（short name）展开逻辑：短名 + 默认 scope / `--scope` 组合成完整环境名
- `apply` 命令使用 `deploy.yaml` 的 `name` 字段（如 `alice.dev`）与 `--scope`/默认 scope 组合

后端 deploy service 的资源模式为 `deploy/scopes/{scope}/environments/{env_name}`（`projects/infra/deploy/deploy.proto:92`）。`ListEnvironments` API 要求 parent 指定具体 scope（`parseParent` 函数 `handler.go:777-789`，通过 `domain.NewEnvironmentName` 校验 scope 须匹配 `^[a-z][a-z0-9]{0,7}$`）。

### 目标与差异

**移除 scope 作为独立设计对象**：scope 仅作为 `{scope}.{env_name}` 环境名格式的前缀部分存在，不再作为单独的命令、标志或配置项。具体：

1. 移除 `scope` 命令和所有默认 scope 配置逻辑
2. 移除 apply、del、describe 命令的 `--scope` 标志——这些命令直接使用完整环境名
3. `list` 命令保留 `--scope` 作为可选过滤参数（list 本质上按 scope 维度操作），不指定时列出所有 scope 的环境
4. 后端扩展支持 AIP-159 的 `-` 通配符模式，使 `list` 不指定 scope 时能跨 scope 列出所有环境

**核心设计原则——显式完整名，无推测无回退**：移除默认 scope 后，所有直接使用环境名的命令（apply、del、describe）都要求用户显式提供完整 `{scope}.{env_name}` 环境名。CLI 不做任何推测或静默回退——不存在自动补全 scope、自动拼接、猜测默认值等行为。不完整或格式错误的名称必须报错而非静默修正。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Remove the scope command and default scope config (Priority: P1)

开发者不再需要在部署前配置或查询 "默认 scope"。`deploy scope` 命令被完全移除，本地 `.env/cli.json` 配置文件不再被读取或写入。CLI 中不存在任何 "默认 scope" 概念。

**Why this priority**: 这是变更的核心——消除 scope 作为独立设计对象。

**Independent Test**: 执行 `deploy scope` 验证返回 "unknown command" 错误；验证任何命令都不会创建或读取 `.env/cli.json`。

**Acceptance Scenarios**:

1. **Given** deploy CLI 已安装，**When** 用户执行 `deploy scope`，**Then** CLI 返回错误表明 "scope" 不是有效命令。
2. **Given** deploy CLI 已安装，**When** 用户执行任意命令（apply、del、describe、list），**Then** CLI 不读取或写入 `.env/cli.json`。

---

### User Story 2 - Remove the --scope flag from apply/del/describe (Priority: P1)

`--scope` 标志从 apply、del、describe 命令中移除。这些命令直接使用完整的 `{scope}.{env_name}` 格式环境名。用户无法通过标志传递独立的 scope 值。

**Why this priority**: `--scope` 标志是 scope 作为设计对象的主要暴露途径。

**Independent Test**: 对 apply/del/describe 执行带 `--scope=team` 的命令，验证返回标志解析错误。

**Acceptance Scenarios**:

1. **Given** deploy CLI 已安装，**When** 用户执行 `deploy del --scope=team alice.dev`，**Then** CLI 返回标志解析错误，表明 `--scope` 不是有效标志。
2. **Given** deploy CLI 已安装，**When** 用户执行 `deploy describe alice.dev`，**Then** CLI 使用完整名 `alice.dev` 解析环境，无需 scope 参数。

---

### User Story 3 - Require full environment names for del/describe (Priority: P1)

del 和 describe 命令要求完整的 `{scope}.{env_name}` 格式。短名（如 `dev`）被拒绝并给出明确错误——CLI 不做任何推测或静默回退（不自动补全 scope、不猜测默认值）。`apply` 命令直接使用 `deploy.yaml` 的 `name` 字段作为完整环境名（`{{run}}` 占位符替换后），不再与 scope 组合。

**Why this priority**: 移除默认 scope 和 `--scope` 后，短名展开逻辑被彻底消除。要求完整名是直接后果，确保环境标识无歧义。所有环境名必须显式、完整，系统不做任何隐式推断。

**Independent Test**: 执行 `deploy del dev`（短名）验证报错且不做静默回退；执行 `deploy del alice.dev` 验证正确构造后端资源路径。

**Acceptance Scenarios**:

1. **Given** 用户要删除环境，**When** 执行 `deploy del dev`（短名），**Then** CLI 返回错误，说明需要完整的 `{scope}.{env_name}` 格式。CLI 不尝试推测 scope 或静默拼接。
2. **Given** 用户要查看环境，**When** 执行 `deploy describe alice.dev`，**Then** CLI 正确解析完整名查询环境。
3. **Given** 用户执行 `deploy apply`，**When** deploy.yaml 的 `name` 为 `alice.dev`，**Then** CLI 直接使用 `alice.dev` 作为完整环境名，不与任何 scope 组合。

---

### User Story 4 - List with optional --scope and cross-scope listing (Priority: P2)

`list` 命令保留 `--scope` 作为可选参数：
- 指定 `--scope` 时：列出该 scope 下所有环境（行为与当前一致）
- 不指定 `--scope` 时：列出**所有 scope** 的环境

跨 scope 列出能力通过后端 AIP-159 通配符实现——CLI 发送 `deploy/scopes/-` 作为 parent。后端 deploy service 扩展支持 `-` 作为通配 scope 值（[AIP-159](https://google.aip.dev/159) 标准模式）。

**Why this priority**: `list` 本质上按 scope 维度操作（后端 `ListEnvironments` 要求 parent scope 资源路径），需要 scope 信息。保留 `--scope` 作为过滤参数使 scope 仅作为 list 的操作参数，而非全局设计对象。

**Independent Test**: 执行 `deploy list` 验证列出所有 scope 的环境；执行 `deploy list --scope=alice` 验证只列出 alice scope 的环境。

**Acceptance Scenarios**:

1. **Given** 环境 `alice.dev`、`alice.test`、`bob.prod` 存在，**When** 用户执行 `deploy list`，**Then** CLI 列出 `alice.dev`、`alice.test`、`bob.prod`（所有 scope 的所有环境），每行使用实际完整环境名。
2. **Given** 环境 `alice.dev`、`alice.test`、`bob.prod` 存在，**When** 用户执行 `deploy list --scope=alice`，**Then** CLI 列出 `alice.dev` 和 `alice.test`。
3. **Given** 用户执行 `deploy list`，**When** 后端返回环境列表，**Then** 输出中的每行环境名使用响应中的实际 scope（而非 `-`），遵循 AIP-159 规定。

---

### Edge Cases

- 环境名包含多个点号（如 `a.b.c`）时，格式校验拒绝它。
- 环境名为空时，CLI 返回校验错误。
- 旧版本遗留的 `.env/cli.json` 文件被忽略（不读取），无需迁移或清理。
- `deploy.yaml` 的 `name` 字段已通过 JSON schema 强制 `{scope}.{env_name}` 格式，无需额外校验。
- `{{run}}` 占位符机制不受影响——占位符在 `{scope}.{env_name}` 格式内解析。

## Requirements *(mandatory)*

### Functional Requirements

#### CLI 工具（`tools/release/deploy/v3/`）

- **FR-001**: CLI 不得包含 `scope` 命令。执行 `deploy scope` 须返回 "unknown command" 错误。
- **FR-002**: `--scope` 标志须从 apply、del、describe 命令中移除。对这些命令传递 `--scope` 须返回标志解析错误。
- **FR-003**: CLI 不得读取、写入或引用 `.env/cli.json` 本地配置文件或任何 "默认 scope" 概念。相关的 `cliConfig` 结构体、`loadConfig`/`saveConfig` 函数、`scopeCommand` 函数须被移除。
- **FR-004**: `del` 命令须要求完整环境名（`{scope}.{env_name}` 格式）。不含点号的名称（短名）须被拒绝，返回明确错误信息说明需要完整格式。CLI 不得做任何推测或静默回退（不自动补全 scope、不猜测默认值）。
- **FR-005**: `describe` 命令须要求完整环境名（`{scope}.{env_name}` 格式）。不含点号的名称须被拒绝。CLI 不得做任何推测或静默回退。
- **FR-006**: `apply` 命令须直接使用 `deploy.yaml` 的 `name` 字段作为完整环境名（`{{run}}` 占位符替换后），不得与任何 scope 值组合。
- **FR-007**: `list` 命令须保留 `--scope` 作为可选参数。指定时列出该 scope 的环境；不指定时 CLI 须向后端发送 `-` 作为 scope 通配符，列出所有 scope 的环境。
- **FR-008**: `list` 命令输出须使用响应中的实际完整环境名（`{scope}.{env_name}`），而非用户输入的 scope 或 `-`。
- **FR-009**: 环境名校验逻辑须移除 scope 组合与短名展开（`NewFullEnvName`、`ValidateScope`、短名检测），改为直接校验完整环境名格式（`^[a-z][a-z0-9]{0,7}\.[a-z][a-z0-9]{0,7}$`）。
- **FR-010**: 跨命令一致性约束——apply、del、describe 三个命令的环境名处理必须遵循统一的「显式完整名、无推测、无回退」原则，具体行为要求见 FR-004、FR-005、FR-006 与上文「核心设计原则」小节。本条不新增独立行为，仅约束三个命令的实现保持一致，避免个别命令残留 scope 组合或静默回退逻辑。
- **FR-011**: `deploy_v3 --help` 输出须反映更新后的命令集（无 `scope` 命令、apply/del/describe 无 `--scope`、`list` 可选 `--scope`）。
- **FR-012**: `tools/release/deploy/README.md` 须更新，移除所有对 `scope` 命令、默认 scope 配置、短名展开的引用。环境名格式部分须说明完整 `{scope}.{env_name}` 名始终为必需（list 的 `--scope` 为可选过滤）。

#### 后端 deploy service（`projects/infra/deploy/`）

- **FR-013**: 后端 `ListEnvironments` API 须支持 `-` 作为 parent scope 的通配符值（parent = `deploy/scopes/-`），遵循 [AIP-159](https://google.aip.dev/159) 的跨集合读取模式。
- **FR-014**: 当 scope 为 `-` 时，后端须返回所有 scope 下的所有环境，响应中每个环境的 `name` 字段须使用实际的 canonical 资源名（`deploy/scopes/{actual_scope}/environments/{env_name}`），而非 `-`。
- **FR-015**: proto 文件 `projects/infra/deploy/deploy.proto` 的 `ListEnvironments` HTTP 注解**不得修改**——现有 `{parent=deploy/scopes/*}/environments` 模式已满足 AIP-159 要求（通配符 `*` 允许 `-` 值，AIP-159 规定 URI pattern 必须用 `*` 而非硬编码 `-`）。
- **FR-016**: handler 层 `parseParent` 函数须特殊处理 `-` scope 值：跳过 `domain.NewEnvironmentName` 校验（`-` 不匹配 scope regex），直接传递 `-` 给查询层。
- **FR-017**: 存储层须支持跨 scope 查询：当 scope 为 `-` 时，使用空过滤条件（匹配所有文档）而非 scope 精确匹配。

### Key Entities *(include if feature involves data)*

- **Environment Name**: 部署环境的唯一标识，始终为 `{scope}.{env_name}` 格式（如 `alice.dev`）。两部分均须匹配 `^[a-z][a-z0-9]{0,7}$`。点号为完整名的分隔符。CLI 始终要求用户显式提供完整名，不做推测或静默回退。
- **Backend Resource Name**: API 级资源路径 `deploy/scopes/{scope}/environments/{env_name}`，通过解析环境名获得。此契约不变。对于 list 跨 scope 场景，parent 使用 `deploy/scopes/-`。
- **Scope Wildcard (`-`)**: AIP-159 定义的标准跨集合通配符，用于 `ListEnvironments` 的 parent 参数。值为 `deploy/scopes/-`。仅用于 list 跨 scope 场景。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 用户可仅使用完整环境名执行所有部署操作（apply、del、describe），无需单独配置或指定 scope。所有环境名均由用户显式提供，CLI 不做任何推测或静默回退。
- **SC-002**: deploy CLI 用户界面中无 `scope` 命令、无默认 scope 配置、无短名展开逻辑（`list --scope` 为可选过滤参数除外）。
- **SC-003**: `list` 不指定 `--scope` 时能列出所有 scope 的所有环境，每行使用实际完整环境名。
- **SC-004**: 后端 `ListEnvironments` 接受 `deploy/scopes/-` parent，返回所有 scope 的环境，遵循 AIP-159。
- **SC-005**: 所有现有单元测试通过（更新后移除 scope 相关 setup、使用完整环境名、list 跨 scope 场景新增测试）。
- **SC-006**: CLI help 文本和 README 准确反映简化后的命令集。

## Assumptions

- 后端 deploy service 的 proto HTTP 注解（`{parent=deploy/scopes/*}/environments`）无需修改，AIP-159 通配符 `*` 已允许 `-` 值。
- `deploy.yaml` 的 `name` 字段 schema 已强制 `{scope}.{env_name}` 格式，apply 命令无需 schema 变更。
- 旧版本遗留的 `.env/cli.json` 文件安全忽略，无需迁移。
- `{{run}}` 占位符机制不受此变更影响——占位符在 `{scope}.{env_name}` 格式内解析。
- 跨 scope list 不支持 `order_by`（遵循 AIP-159 对跨父级请求的排序建议）。
- AIP-159 关于不可达父级（AIP-217）的指引不适用于本系统（单一服务、无跨区域），无需实现 partial failure 指示。
