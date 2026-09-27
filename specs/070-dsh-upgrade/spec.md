# Feature Specification: dsh 升级到 0.1.7 线（core 底座 + agent_v2 官方插件 + preset 模型重构）

**Feature Branch**: `070-dsh-upgrade`

**Created**: 2026-09-27

**Status**: Draft

**Input**: User description: "为 @third_party/dsh/core/ 升级到 dsh 最新版本，另外对 @game/agent_v2/ 当中使用的 dsh 官方插件也进行升级"（经澄清：目标线取 registry 最新 next 线 `0.1.7-rc.2`；0.1.7 重构了 preset 模型并更换了新包，game 的 preset 部分按 0.1.7 框架重构适配；消费者范围为全仓库同线迁移）

## Clarifications

### Session 2026-09-27

- Q: 升级到 0.1.7 声明式 preset 模型后，用户自建 preset 的持久化事实源（source of truth）应该保留在哪里？ → A: 保留 Mongo store 为唯一事实源：preset-authoring 服务面（CRUD/模板规则/派生）契约不变，物化时映射为新模型声明。
- Q: 升级与 preset 重构上线后，存量数据（旧会话中记录的 preset 身份引用、Mongo 中按旧模型写入的记录）需要兼容到什么程度？ → A: 会话无持久化，无需考虑旧会话兼容；不要求运行时兼容旧数据，Mongo 中既有 preset 记录执行一次性迁移到新模型形态。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 框架核心底座升级到 0.1.7-rc.2 版本线 (Priority: P1)

维护者将 `third_party/dsh/core`（框架核心底座，11 包闭包清单 workspace 包，`third_party/dsh/core/package.json`）所 pin 的 dsh 版本线从 `0.1.1-rc.2` 升级到 `0.1.7-rc.2`，底座声明面（package.json 精确 pin、`third_party/dsh/core/BUILD.bazel` 的 `js_runtime_library.npm_deps` 枚举、`third_party/dsh/core/version.ts` 快照标识）同步更新，且底座保持"零插件"约束（`experimental/dsh/demo/testplan/closure_audit_test.go` 断言①）。cordis 家族与原生 addon 依赖按 0.1.7 线 peer 互锁值同线升级。

**Why this priority**: 底座是全部 dsh 服务的 runtime 闭包基座（`runtime_deps` 一行引用模式），版本线必须最先落定，其余消费者才能跟随。

**Independent Test**: `bazel build //third_party/dsh/core:runtime_pkg` 通过；lockfile 中全部 `@deepseek-ai/*` 解析到 0.1.7-rc.2 线；closure audit 断言①（plugin-free 枚举）通过。

**Acceptance Scenarios**:

1. **Given** 0.1.7-rc.2 线已锁定，**When** 更新底座声明面并刷新 lockfile，**Then** 底座闭包内每个 dsh 包与 cordis 互锁包（cordis 4.0.4 线、loader 1.0.5、include 1.0.9、group 1.0.4、timer 1.1.6、node-addon-require-builtin 0.1.6）都解析到线内版本，无跨线混装。
2. **Given** 升级后的底座，**When** 运行底座闭包审计（closure audit 断言①），**Then** 核心声明面不含任何插件包（仅框架核心清单），审计通过。
3. **Given** 升级后的底座，**When** 依赖底座的服务完成构建，**Then** 服务产物 runtime 闭包中的核心包全部来自 0.1.7-rc.2 线。

---

### User Story 2 - agent_v2 官方插件升级且行为不回归 (Priority: P1)

`projects/game/agent_v2` 服务声明的 dsh 官方插件（`projects/game/agent_v2/package.json`：`dsh-agent`、`dsh-agent-loop`、`dsh-app-boot`、`dsh-llm`、`dsh-llm-retry`、`dsh-persona`、`dsh-scope`、`dsh-session`、`dsh-system-prompt`、`dsh-tools`；preset 相关见 US3）升级到 0.1.7-rc.2 线；源码与组合清单（`projects/game/agent_v2/cordis.yml`）对升级引入的 API/行为变更完成适配，服务对外行为与升级前一致。

**Why this priority**: agent_v2 是生产服务，插件升级是用户点名的核心诉求；行为不回归是升级的硬约束。

**Independent Test**: agent_v2 全部单元测试（含组合清单行断言、team 流程）通过；`projects/game/testplan/` 下 agent_v2 相关大型测试计划实际执行并通过（部署→测试→清理闭环）。

**Acceptance Scenarios**:

1. **Given** agent_v2 的 dsh 依赖已升级到 0.1.7-rc.2 线，**When** 服务以组合清单 boot，**Then** 全部插件行（timer/llm/session/system-prompt/tools/agents/agent-loop/invariants/invariant-* 子路径行/llm-retry/persona 等）加载成功，无 peer 冲突或模块缺失。
2. **Given** 升级后的服务，**When** 执行既有 agent_v2 系统测试（对话、team 群聊、planner memory、LLM 路由与流停滞看护），**Then** 全部用例通过与升级前一致的结果。
3. **Given** 升级引入的 API 变更，**When** 完成源码适配，**Then** 不存在绕过、跳过或删除既有测试用例来"通过"验收的情况。

---

### User Story 3 - game preset 部分按 0.1.7 新模型重构 (Priority: P1)

0.1.7 以声明式 preset 模型取代旧的目录式 `@deepseek-ai/dsh-agent-presets`：新包 `@deepseek-ai/dsh-agent-preset-registry`（`ctx.agentPresets`，负责选择、运行时修订保留与 profile 编辑）与 `@deepseek-ai/dsh-agent-preset`（以普通 Cordis 声明定义 preset：身份、展示元数据、子插件清单），preset 定义不再有独立目录/roots 概念（https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-09-18-declarative-agent-presets.md ；https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/preset ）。game 的 preset 能力面在该新模型下重构并保持既有产品语义。

**必须保持的 preset 产品语义**（来自 `projects/game/agent_v2/cordis.yml` agent-presets 行、`common/js/dsh-plugins/preset-authoring`、specs/058/059/060 契约面）：

1. 双角色池：player / planner 各自的系统级模板 preset（工具插件行隔离——player 配 `@dominion/dsh-saolei`、禁 `@dominion/dsh-memory`；planner 反之），模板不可被普通用户写删。
2. preset CRUD 服务面：创建/编辑/删除用户 preset（preset-authoring 插件 + Mongo store，事实源保持不变），创建时模板规则校验失败 → `INVALID_ARGUMENT`，不落任何产物。
3. 用户 preset 派生：用户 preset 的组合在物化时从 store 记录派生，不维护可写副本。
4. 显式选择 fail-loud：preset 选择在本服务是必选语义（UpdateAgent/物化必须显式给 preset），未知 preset id 报错而非静默回落。
5. team 集成：player/planner 池 preset 经模板插件行挂载工具，工具可见性由挂载层隔离。

**Why this priority**: preset roster 是 agent_v2 team 模式的核心功能面；0.1.7 线上旧包已死（最高 0.1.5-rc.3，与 0.1.7 线 peer 互斥），不重构则 US2 无法成立——这是 0.1.7 升级的最大适配工作项。

**Independent Test**: preset 相关单元测试（roster、模板规则校验、派生、fail-loud 选择）在新模型下全部通过；agent_v2 大型测试中 preset 创建/派生/team 全流程用例通过。

**Acceptance Scenarios**:

1. **Given** 新模型（registry + 声明式 preset），**When** 定义 player/planner 模板 preset，**Then** 模板以普通 Cordis 声明表达（子插件清单 + 元数据），且具备系统级信任（普通用户不可写删）。
2. **Given** 用户请求创建 preset，**When** 其插件行违反角色模板规则（required/forbidden），**Then** 请求被拒绝（`INVALID_ARGUMENT`）且无产物；**When** 合法，**Then** 用户 preset 可创建、编辑、删除，并在 agent 物化时生效。
3. **Given** 物化/更新 agent 时给出未知 preset id，**When** 解析，**Then** fail-loud 报错，不静默回落任何默认。
4. **Given** team 会话中不同角色 agent，**When** 各自按池模板 preset 挂载工具，**Then** 工具可见性按角色隔离（player 不可见 memory 工具，planner 不可见 saolei 工具）。
5. **Given** preset 定义激活失败（如插件行损坏），**When** 服务 boot，**Then** 失败定义留在 roster 中拒绝新绑定，但不阻止应用启动（新模型 eager-activation 语义）。
6. **Given** Mongo 中存在按旧模型写入的既有 preset 记录，**When** 执行一次性迁移，**Then** 记录在字段 schema 不变的前提下对齐到声明式模板 id（`template` 值可被新声明的模板解析、persona 物化映射可用），迁移后 CRUD/派生/team 流程正常工作；服务端校验拒绝旧形态时按 runbook 删后重建。

---

### User Story 4 - 共享 dsh 生态包同线迁移 (Priority: P2)

与 dsh 版本线耦合的其余仓库消费者同线迁移到 0.1.7-rc.2：`experimental/dsh/demo/agent`（demo 服务与 closure audit 断言②声明面，含其 preset roster demo 迁移到新模型）、`common/js/dsh-plugins/*`（workspace 插件；其中 `preset-authoring` 按 US3 新模型重构，`llm-glm`/`llm-opencode-go` 的 `dsh-timeout` 同线升级）、以及 0.1.7 线新增 peer 包在 catalog 与消费 package.json 的声明补齐。全部构建、测试通过。

**Why this priority**: catalog 是全局唯一版本面；dsh 闭包整体即一个版本单元，这些消费者不同线迁移则 US1/US2 无法以统一 catalog 达成。

**Independent Test**: `bazel build` + `bazel test` 覆盖 `//experimental/dsh/demo/...`、`//common/js/dsh-plugins/...` 全绿；demo 大型测试计划 `experimental/dsh/demo/testplan/interface_test.yaml` 实际执行通过；closure audit 断言②（声明可追溯）通过。

**Acceptance Scenarios**:

1. **Given** catalog 已切到 0.1.7-rc.2 线，**When** 刷新 lockfile 并构建 demo 与全部 workspace 插件，**Then** 无 peer 依赖冲突（不存在同仓库两份不同物理副本的 cordis），构建与单测全绿。
2. **Given** 0.1.7 线引入的新 peer 包（dsh-settings、dsh-typert-protocol、dsh-session-projection、dsh-session-persistence、dsh-util-values、dsh-brand、dsh-sandbox 系、dsh-user-approval、dsh-ptc-runtime 等），**When** 服务闭包物化，**Then** 每个随行 dsh 包都能追溯到某 package.json 的显式声明（closure audit 断言②），不存在幽灵包；`dsh-workspace` 为 dsh-agent 的 optional peer，本特性不引入（无工作区概念，`specs/070-dsh-upgrade/research.md` D6），不属于随行包。
3. **Given** demo 的 preset roster 演示（persona 行 + 插件行组合），**When** 迁移到新模型，**Then** demo 组合清单以新包表达且 demo 大型测试（含 preset 用例）实际执行通过。

---

### User Story 5 - 前端 UI 原语包对齐版本线 (Priority: P3)

`@deepseek-ai/dsh-client-ui-primitives`（`projects/game/web/frontend/package.json`）作为 dsh 家族同线发布包，对齐到 0.1.7-rc.2 线（该包存在 0.1.7-rc.2 版本），同时将 vendored 的 `@deepseek-ai/dsh-client-ui-theme` 源（`projects/game/web/frontend/src/dsh-theme/`，非 catalog 依赖，按 registry/unpkg 0.1.7-rc.2 包文件重取）重新 vendoring，web 前端构建与既有页面行为不受影响。

**Why this priority**: 该包是浏览器侧独立 UI 原语库，不参与 cordis peer 闭包，风险低，但对齐家族版本线可避免版本认知分裂。

**Independent Test**: web 前端既有构建与测试通过，页面渲染无回归。

**Acceptance Scenarios**:

1. **Given** `dsh-client-ui-primitives` 升到 0.1.7-rc.2 且 vendored theme 重取为 0.1.7-rc.2，**When** 构建并运行前端既有测试，**Then** 构建通过、用例全绿。

---

### Edge Cases

- **peer 版本互锁冲突**：0.1.7-rc.2 线各包 peer 以 tilde 互锁（cordis `~4.0.4`、loader `~1.0.5`、include `~1.0.9`、group `~1.0.4`、dsh 家族互相精确 `0.1.7-rc.2`）。catalog 解析必须落在线内值（cordis 4.0.4、loader 1.0.5、include 1.0.9、group 1.0.4、timer 1.1.6、node-addon-require-builtin 0.1.6）；若解析漂移到线外（如 cordis 4.0.5+），同仓库可能物化两份 cordis，DI 框架双实例导致插件注册/类型识别运行时故障。
- **旧 presets 包不可同线混装**：`@deepseek-ai/dsh-agent-presets` 最高仅 0.1.5-rc.3，其精确 peer（cordis 4.0.2）与 0.1.7 线（~4.0.4）互斥；升级后该包 MUST 从 catalog 与全部 package.json/BUILD 声明面彻底移除，替换为新模型包（`dsh-agent-preset-registry` + `dsh-agent-preset`）。
- **preset 模型迁移语义差**：目录 roots/trust/`includeUserRoot`/`default` 配置面在新模型中不存在，改为声明式定义 + registry 的选择/修订保留；新语义包括 eager activation（激活失败留在 roster、拒绝新绑定、不阻止启动）、revision 保留（编辑时旧修订对运行中 agent 保持可用直至释放）、`selectedDefault`（无名新会话的解析目标）、会话数据记录 preset 身份且重启时按当前配置解析（缺失定义则拒绝）。既有"空 default + 显式必选"的产品语义（fail-loud）需在新模型上重新表达。存量数据不做运行时兼容：agent_v2 会话无持久化（无旧会话身份解析场景），Mongo 既有 preset 记录以一次性迁移承载（记录 schema 不变，`template`/persona 映射对齐声明式模型；FR-012）。
- **用户 preset 持久化映射**：事实源保持 preset-authoring 的 Mongo store（2026-09-27 澄清决策）；store 记录映射/物化为 0.1.7 新模型声明的机制（激活时机、声明形态、registry revision 生命周期与 store 记录的一致性）是 plan 阶段设计决策，spec 约束 US3 产品语义不变。新模型原生用户编辑走"活动 profile 的 user patch"（见架构注记），本服务不采用该面承载用户 preset。
- **新增 peer/依赖包**：0.1.7 线引入的新 peer 包（US4 场景 2 所列）与新 runtime 依赖（dsh-atomic-write、dsh-package-manifest、schemastery `^3.18.4` 等）必须纳入声明面与审计预期集，否则 closure audit 失败或产物缺包（完整清单以 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §1/§4 为唯一权威）。
- **原生 addon 可选依赖**：`node-addon-require-builtin` 0.1.6 的 `linux-x64-gnu` optionalDep 必须在 lockfile 中在场（`survey/deepseek-harness-b1-bazel-packaging.md` §7 风险 2），否则 bare-name 插件解析在部署环境失败。
- **API 破坏性变更**：上游处于 developer preview，官方声明"会有兼容性破坏"（https://github.com/deepseek-ai/deepseek-harness README）；0.1.1-rc.2 → 0.1.7-rc.2 跨 0.1.2/0.1.3/0.1.5/0.1.6/0.1.7 多条线，boot API、组合清单 schema、agent/session/loop 类型面、invariant 子路径导出都可能变化，适配工作量在 plan 阶段需按包逐一 diff 评估（registry 实测 tarball + 上游源码仓库比对）。
- **升级后发现不兼容**：若 0.1.7-rc.2 在仓库场景存在无法适配的破坏（如必需 API 被移除且无替代），允许回退到"上一个可用同线版本"（如 0.1.7-rc.1 或 0.1.5-rc.3 并放弃本次 preset 重构）并在 PR 记录差异（先例：`specs/047-dsh-chat-demo/tasks.md` T001 取线规则）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 版本线统一：catalog（根 `pnpm-workspace.yaml`）中全部 dsh 家族条目及 cordis 家族互锁包 MUST 统一 pin 到 `0.1.7-rc.2` 线的线内版本（dsh 闭包整体一个版本单元；dist-tag 不可信，dsh 家族精确版本 pin、cordis 家族锁在线内值，先例 `specs/049-agent-v2-dsh-init/spec.md` 版本线决策）。
- **FR-002**: 目标版本线为 `0.1.7-rc.2`（registry `next` 线；2026-09-27 实测），cordis 家族互锁值：cordis `4.0.4`、cordis-plugin-loader `1.0.5`、cordis-plugin-include `1.0.9`、cordis-plugin-group `1.0.4`、cordis-plugin-timer `1.1.6`、node-addon-require-builtin `0.1.6`。
- **FR-003**: 消费者范围为全仓库同线迁移：`third_party/dsh/core`、`projects/game/agent_v2`、`experimental/dsh/demo/agent`、`common/js/dsh-plugins/*`、`projects/game/web/frontend`（ui-primitives 依赖 + vendored theme 重取）。
- **FR-004**: `third_party/dsh/core` 底座声明面（package.json 依赖清单、`BUILD.bazel` npm_deps 枚举、`version.ts` 快照标识）MUST 同步 0.1.7-rc.2 线；底座保持零插件（仅框架核心清单，`experimental/dsh/demo/testplan/closure_audit_test.go` d6CorePackages 口径）。
- **FR-005**: 0.1.7 线要求的全部 peer 包 MUST 在消费服务的 package.json 与 catalog 中显式声明（pnpm 隐式 auto-install 不构成声明面；closure audit 断言②要求每个随行包可追溯）。完整新增清单以 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §1/§4 为唯一权威（US4 场景 2、Edge Cases 与 `research.md` D6 均为其投影）。
- **FR-006**: `projects/game/agent_v2` 源码与 `cordis.yml` 组合清单 MUST 完成 0.1.7-rc.2 线的 API 适配，既有单元测试全部通过，不得删除或跳过用例来达成通过。
- **FR-007**: 升级后 agent_v2 对外服务行为 MUST 与升级前一致（对话流程、team 群聊、planner memory、LLM 路由与流停滞看护等既有行为面），以既有大型测试计划实际执行全部通过为验收。
- **FR-008**: game preset 部分 MUST 按 0.1.7 新模型重构：以 `@deepseek-ai/dsh-agent-preset-registry` + `@deepseek-ai/dsh-agent-preset`（+ 既有 `dsh-persona`）表达，`@deepseek-ai/dsh-agent-presets` 从全部声明面移除；US3 所列五项产品语义（双角色池/CRUD 服务面/用户派生/显式 fail-loud 选择/team 工具隔离）MUST 在新模型下保持。用户 preset 事实源 MUST 保持 Mongo store（preset-authoring 服务面契约不变），0.1.7 新模型声明在物化/激活时由 store 记录映射生成。
- **FR-009**: `experimental/dsh/demo`（含其 preset 演示迁移到新模型）与 `common/js/dsh-plugins/*`（含 preset-authoring 重构）MUST 在 0.1.7-rc.2 线完成适配，demo 大型测试计划与 closure audit 全部通过。
- **FR-010**: lockfile MUST 经 pnpm 官方流程刷新（不得手改 `pnpm-lock.yaml`），且刷新后 lockfile 内全部 `@deepseek-ai/*` 同 0.1.7-rc.2 线、`node-addon-require-builtin` 平台 optionalDep 在场。
- **FR-011**: 升级涉及的行为面决策（选线依据、peer 互锁 pin、preset 模型映射设计）MUST 在交付文档（plan/research/contracts）中记录来源与依据（版本事实以 registry 实测为准，preset 语义以上游架构注记为准）。
- **FR-012**: Mongo 中既有 preset 记录 MUST 经一次性迁移到新模型可物化形态：`PresetRecord` 字段 schema 不变（`data-model.md` §3.4 冻结），迁移的实际语义是确认/归一化 `template` 值对应声明式模板 id、persona 子行映射（`prefix`）在新模型物化路径下可用（无运行时双格式兼容）；会话无持久化，不涉及旧会话 preset 身份解析兼容。

### Key Entities

本特性涉及的关键资产（依赖升级不引入新数据实体；preset 重构涉及模型变更）：

- **dsh 版本线（version line）**: 同一次 lockstep 发布的 dsh 家族版本集合 + 互锁的 cordis 家族精确版本，整体作为唯一版本单元。
- **框架核心底座（core baseline）**: `third_party/dsh/core` 的 11 包闭包清单（框架核心，零插件）。
- **声明面（declaration surface）**: package.json 依赖 + `BUILD.bazel` npm_deps 枚举 + closure audit 预期集，三者必须与实际随行闭包一致。
- **声明式 preset（declarative preset）**: 0.1.7 模型中由普通 Cordis 声明定义的 agent 能力集合（身份、元数据、子插件清单），由 registry（`ctx.agentPresets`）管理选择与运行时修订保留；取代旧目录式 roots/trust 模型。
- **角色模板规则（role template rules）**: game 侧 preset 语义——player/planner 池模板各自的 required/forbidden 插件行约束与系统信任级别（产品语义，模型无关）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 升级后，lockfile 中全部 dsh 家族包（含 cordis 家族互锁包）解析到 0.1.7-rc.2 线内版本，零跨线混装、零重复物理副本（cordis 全仓库唯一一份）；`@deepseek-ai/dsh-agent-presets` 不再出现于任何声明面。
- **SC-002**: 仓库全部既有构建与单元测试（dsh 相关：core、agent_v2、demo、dsh-plugins、web 前端）100% 通过，无用例被删除、跳过或标记 flaky 豁免；preset 相关用例在新模型下等价重写后通过。
- **SC-003**: agent_v2 既有大型测试计划实际执行（部署→测试→清理闭环）全部用例通过，服务对外行为与升级前一致（对话、preset、team、memory 全回归）。
- **SC-004**: demo 大型测试与 closure audit（断言① plugin-free、断言② 声明可追溯、断言③ 版本唯一）通过：产物中每个 dsh 包都可追溯到显式声明，底座不含插件，同包名无第二版本。
- **SC-005**: preset 产品语义五项（双角色池、CRUD+模板规则校验、用户派生、显式 fail-loud 选择、team 工具隔离）在新模型下全部有对应用例验证通过。
- **SC-006**: 升级后声明面、源码与注释中不存在残留的旧版本线与旧 preset 模型引用（catalog/package.json/BUILD 声明、`version.ts` 快照标识、代码注释、模板目录配置同步到新状态；演进历史由版本控制系统承载）。迁移所必需的旧→新映射记录（`research.md`/`data-model.md`/`contracts/mongo-preset-migration.md`）不计入残留判定（原则 VII"仅在必要时记录"）。

## Assumptions

- 0.1.7-rc.2 线的全部所需包（含新的 `dsh-agent-preset-registry`、`dsh-agent-preset`、`dsh-workspace` 等 peer 面）在 registry 均有已发布版本（2026-09-27 实测确认：registry/agent-preset/persona/workspace/settings 等均存在 0.1.7-rc.2）。
- cordis 家族在 0.1.7 线采用线内精确值 pin（cordis `4.0.4`、loader `1.0.5`、include `1.0.9`、group `1.0.4`、timer `1.1.6`；node-addon-require-builtin `0.1.6`），保证"一个版本单元"的确定性；survey 的"cordis 保持 range"惯例（`survey/deepseek-harness-b1-bazel-packaging.md` §141）在线内精确 pin 前提下按线内值收敛。
- `@deepseek-ai/schemastery` 统一为 `^3.18.4`（新 preset 包依赖 `~3.18.4`，与仓库既有 range 兼容；已由 `specs/070-dsh-upgrade/plan.md` 与 `research.md` D1 定案）。
- 用户 preset 持久化事实源保持 Mongo store（preset-authoring 服务面契约不变），新模型声明在物化时映射生成；store 记录到新模型声明的映射机制（激活时机、revision 生命周期一致性）见 `specs/070-dsh-upgrade/contracts/preset-model-mapping.md`（宪法原则 III），spec 约束产品语义不变（FR-008）。
- `projects/game/agent_v2/preset-templates/` 模板数据（player/planner）迁移为组合清单 `@deepseek-ai/dsh-agent-preset` 声明式定义，原目录与 `PRESET_TEMPLATES_ROOT` env 移除（`specs/070-dsh-upgrade/research.md` D4）；系统信任与不可写删语义由"宿主组合行 = 部署所有权"保留。
- agent_v2 会话无持久化（内存态），升级不涉及旧会话数据的 preset 身份解析兼容；Mongo 既有 preset 记录按 FR-012 以一次性迁移处理。
- 桌面前端（desktop）与 Go 侧（fake-llm、gateway、testplan Go 用例）不依赖 dsh 包，不在迁移面内。
- 升级不引入新业务功能；所有变更为版本线迁移、API 适配、preset 模型重构与声明面维护。
