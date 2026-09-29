# Feature Specification: dsh 升级到 0.2.0 线（core 底座 + 官方插件 + web 前端 webUI）

**Feature Branch**: `073-dsh-0.2-upgrade`

**Created**: 2026-09-29

**Status**: Draft

**Input**: User description: "1. 将 dsh/core 的 dsh 版本升级到 0.2.0；2. 将 game/web 参考的 dsh webui 也升级到 0.2.0"（语境：延续 `specs/070-dsh-upgrade/` 完成的 0.1.7-rc.2 迁移，将 `third_party/dsh/core/` 框架核心、其官方插件消费者、以及 `projects/game/web/` 前端参考的 dsh webUI 组件与设计升级到 0.2.0 版本线）

## Clarifications

### Session 2026-09-29

- Q: game/web 的 webUI 升级到 0.2.0，是指仅同步所参考的官方组件与设计 token 资产（页面视觉零变化），还是同时把 0.2.0 的新设计能力应用到 game/web 页面上？ → A: 资产对齐 + 设计跟进：在版本线对齐基础上，将 0.2.0 新设计能力应用到 game/web 页面。
- Q: 设计跟进应采用哪些 0.2.0 新设计能力——仅自然挂接既有页面运行态的项，还是为全部新能力创造承载面？ → A: 仅应用目前 game/web 有对应承载的功能，没有的先不引入（无承载面的能力 defer 记录）。


## User Scenarios & Testing *(mandatory)*

### User Story 1 - 框架核心底座升级到 0.2.0 线 (Priority: P1)

维护者将 `third_party/dsh/core`（框架核心底座，11 包闭包清单 workspace 包，`third_party/dsh/core/package.json`）所 pin 的 dsh 版本线从 `0.1.7-rc.2` 升级到 `0.2.0-rc.2`（registry `next` 线，2026-09-29 实测；0.2.0 GA 尚未发布，rc 形态取法沿 070 先例 `specs/070-dsh-upgrade/spec.md`），底座声明面（`package.json` 依赖清单经 catalog `catalog:` 协议切值（文件本身与键集合零改动）、`third_party/dsh/core/BUILD.bazel` 的 `js_runtime_library.npm_deps` 枚举、`third_party/dsh/core/version.ts` 快照标识）同步更新，且底座保持"零插件"约束（`experimental/dsh/demo/testplan/closure_audit_test.go` 断言①）。cordis 家族与 node-addon-require-builtin 在 0.2.0 线的互锁值与 0.1.7 线完全相同（上游未发布新版本，registry 实测），保持现值不动。

**Why this priority**: 底座是全部 dsh 服务的 runtime 闭包基座（`runtime_deps` 一行引用模式），版本线必须最先落定，其余消费者才能跟随；用户点名诉求第 1 条。

**Independent Test**: `bazel build //third_party/dsh/core:runtime_pkg` 通过；lockfile 中全部 `@deepseek-ai/*` 解析到 0.2.0-rc.2 线；closure audit 断言①（plugin-free 枚举）通过。

**Acceptance Scenarios**:

1. **Given** 0.2.0-rc.2 线已锁定，**When** 更新底座声明面并刷新 lockfile，**Then** 底座闭包内每个 dsh 包都解析到 `0.2.0-rc.2`，cordis 互锁包保持线内值（cordis 4.0.4、loader 1.0.5、include 1.0.9、group 1.0.4、timer 1.1.6、node-addon-require-builtin 0.1.6），无跨线混装。
2. **Given** 升级后的底座，**When** 运行底座闭包审计（closure audit 断言①），**Then** 核心声明面不含任何插件包（仅框架核心清单），审计通过。
3. **Given** 升级后的底座，**When** 依赖底座的服务完成构建，**Then** 服务产物 runtime 闭包中的核心包全部来自 0.2.0-rc.2 线。

---

### User Story 2 - 官方插件与消费服务同线升级且行为不回归 (Priority: P1)

`projects/game/agent_v2` 与 `experimental/dsh/demo/agent` 声明的 dsh 官方插件（dsh-agent、dsh-agent-loop、dsh-agent-preset-registry、dsh-llm、dsh-llm-retry、dsh-persona、dsh-scope、dsh-session、dsh-system-prompt、dsh-tools 等）升级到 0.2.0-rc.2 线。与 0.1.7-rc.2 相比，0.2.0 线官方插件包的类型面为向后兼容增量（registry tarball 逐字节 diff 实测：调研覆盖 28 包——26 个 catalog dsh 条目 + client-store/client-theme——其中 20 个逐字节相同；变更的框架/插件包仅 `dsh-session`（新增 `ToolCallRecovery` 导出）、`dsh-agent-loop`（失败 step 恢复行为）、`dsh-app-boot`（可选 bundle 清单）、`dsh-util-values`（内部修复）、`dsh-llm`/`dsh-agent-preset-registry`（仅协议快照），均为增量），组合清单行集合与源码预期零适配。服务对外行为与升级前一致；"工具调度失败后对话仍可继续"作为升级自动获得的健壮性增强（上游修复，见 https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/.agents/notes/implemented/bug-fix/2026-09-19-failed-step-tool-results.md ）。

**Why this priority**: agent_v2 是生产服务，官方插件同线升级是 dsh 版本单元的物理约束（catalog 全局唯一版本面，混装导致 peer 冲突），且用户点名"官方插件"升级；行为不回归是硬约束。

**Independent Test**: agent_v2 全部单元测试（含组合清单行断言、team 流程）通过；`projects/game/testplan/system_test.yaml` 中 agent_v2 相关大型测试计划实际执行并通过（部署→测试→清理闭环）。

**Acceptance Scenarios**:

1. **Given** 依赖已升级到 0.2.0-rc.2 线，**When** 服务以组合清单 boot，**Then** 全部插件行加载成功，无 peer 冲突或模块缺失（组合清单行集合与升级前相同）。
2. **Given** 升级后的服务，**When** 执行既有 agent_v2 系统测试四拓扑（game-system：对话/preset/team 群聊与工具隔离/planner memory；game-disconnect：desktop 断开恢复；game-memory-down：team 物化 fail-loud；game-stall：LLM 路由流停滞看护），**Then** 全部用例通过与升级前一致的结果。
3. **Given** 会话中某 step 的工具调度在结果记录前失败，**When** 该 step 关闭，**Then** 未配对的工具调用获得保守恢复结果（结果未知的调用标记为 `TOOL_OUTCOME_UNKNOWN`〔不可盲目重试〕、未启动的调用标记为 `TOOL_NOT_STARTED`），后续对话可继续——0.2.0 新增健壮性行为，有对应测试覆盖。
4. **Given** 既有单测对失败路径事件序列的精确断言，**When** 0.2.0 行为变化使其失配，**Then** 断言按新事实重写且语义守恒，不存在删除、跳过或放松用例来"通过"验收的情况。

---

### User Story 3 - game/web webUI 组件与设计对齐 0.2.0 并采用流式 shimmer 动画 (Priority: P1)

`projects/game/web/frontend` 组件级复用的 `@deepseek-ai/dsh-client-ui-primitives`（`projects/game/web/frontend/package.json`）从 0.1.7-rc.2 升级到 0.2.0-rc.2（同线发布），vendored 的 `@deepseek-ai/dsh-client-ui-theme` 源（`projects/game/web/frontend/src/dsh-theme/`，非 catalog 依赖，按 registry tarball 重取，流程见 `projects/game/web/frontend/src/dsh-theme/README.md`）从 0.2.0-rc.2 重新获取。在资产对齐基础上，**设计跟进仅采用既有页面有承载面的新能力**（2026-09-29 澄清）——采用范围、采用点与 defer 边界以 FR-013 为规范源（调研结论见 `specs/073-dsh-0.2-upgrade/research.md` D4/D5/D7）：流式运行态 shimmer 文字动画挂接 ReasoningRow（ChatView 流式回合 THINK 尾块的渲染组件）；随包升级自动生效的视觉增强（如 DisclosureRow running 态头部动画的 CSS 演进）无需代码即获得。资产对齐面变化全部向后兼容：本应用消费的全部组件符号存活且 props 兼容（Icon 集合逐字节相同；Input 转 forwardRef、Tooltip/DisclosureRow/TextShimmer 为可选 prop 增量），既有组件源码除 shimmer 采用点外预期零改动；设计 token 新增 8 个（明暗双份）零删除、零既有值变更，sheet 集合与顺序不变。

**Why this priority**: 用户点名诉求第 2 条；webUI 组件与设计是 game/web 前端的视觉与交互基础，设计跟进改善流式对话的进行中状态表达。

**Independent Test**: web 前端既有构建（`bazel build //projects/game/web/frontend/...` 含 `:dist`）与组件测试（`//projects/game/web/frontend:lib_test`）通过（shimmer 采用点含新增组件测试）；FR-022 消费覆盖断言（`FR-022` 定义见 `specs/054-agent-v2-bugfixes/spec.md`；`projects/game/web/frontend/src/dsh-theme.test.ts`）通过。

**Acceptance Scenarios**:

1. **Given** primitives 升级到 0.2.0-rc.2 且 vendored theme 重取为 0.2.0-rc.2，**When** 构建并运行前端既有测试，**Then** 构建通过、用例全绿，页面渲染无回归（类型面以包内 tsc 手动核验，070 门禁先例 `specs/070-dsh-upgrade/research.md` D9）。
2. **Given** primitives 0.2.0 新消费的设计 token（shimmer 文字、菜单分组头、开关拇指），**When** FR-022 消费覆盖断言运行，**Then** 全部 token 由重取后的 vendored sheets 定义（既有 3 项豁免继续有效）。
3. **Given** 对话回合进行中（ChatView 流式回合的 THINK 尾块，即 ReasoningRow running 态），**When** 运行态渲染，**Then** 折叠摘要以 shimmer 动画表达进行中状态（消费 `--dsw-alias-label-shimmer`），回合完结后动画停止且文字保持可选中；进行中语义不回退（既有"正在生成"无障碍语义保留），有组件测试覆盖。
4. **Given** 无承载面的 0.2.0 新能力（菜单分组、Tooltip 点击钉住、轮次跳转/开关/文档选区 token、深潜水标签 token），**When** 本特性交付，**Then** 不存在为它们新增的承载面改动，defer 清单与理由记录于交付文档。
5. **Given** primitives 与 theme 重取完成，**When** 检查 `src/dsh-theme/README.md` 版本记录与 packageExtensions 声明，**Then** 版本记录更新为 0.2.0-rc.2，两处扩展键与其嵌套 `@deepseek-ai/*` 值均与 0.2.0 线一致。

---

### User Story 4 - 共享生态同线迁移与闭包审计 (Priority: P2)

catalog（根 `pnpm-workspace.yaml`）切线带动的其余消费者同线迁移到 0.2.0-rc.2：`common/js/dsh-plugins/*`（workspace 插件，`dsh-*` 依赖经 catalog 同线）、`experimental/dsh/demo`（demo 服务）、以及 `pnpm-workspace.yaml` `packageExtensions` 的版本键与键内嵌套值（`@deepseek-ai/dsh-client-ui-primitives` 与 `@deepseek-ai/dsh-client-store` 的精确版本键及其镜像的 `@deepseek-ai/dsh-{client-store,util-code-language,util-workspace-path}` 值均随线更新；依赖键集合不变——两包 devDependencies 键集合 0.1.7→0.2.0 零变化，registry 实测）。closure audit 断言②（声明可追溯：产物中每个随行 dsh 包可追溯到显式声明）与断言③（版本唯一：同包名无第二版本）通过。

**Why this priority**: catalog 是全局唯一版本面，dsh 闭包整体即一个版本单元；这些消费者不同线迁移则 US1/US2/US3 无法以统一 catalog 达成。随动性质故 P2。

**Independent Test**: `bazel build` + `bazel test` 覆盖 `//experimental/dsh/demo/...`、`//common/js/dsh-plugins/...` 全绿；demo 大型测试计划 `experimental/dsh/demo/testplan/interface_test.yaml` 实际执行通过；closure audit 断言②③通过。

**Acceptance Scenarios**:

1. **Given** catalog 已切到 0.2.0-rc.2 线，**When** 刷新 lockfile 并构建 demo 与全部 workspace 插件，**Then** 无 peer 依赖冲突（cordis 全仓库唯一一份物理副本），构建与单测全绿。
2. **Given** 服务闭包物化，**When** closure audit 运行，**Then** 每个随行 dsh 包都可追溯到某 package.json 的显式声明（断言②），同包名不存在第二版本（断言③）。
3. **Given** packageExtensions 版本键已随线更新，**When** 前端 Vite 构建与沙箱 vitest 运行，**Then** primitives/client-store 的运行时导入（发布在 devDependencies、由消费方提供）均可解析。

---

### Edge Cases

- **rc 形态与 GA 差异**：0.2.0 线 registry 仅有 0.2.0-rc.1（2026-09-28）与 0.2.0-rc.2（2026-09-29），GA 未发布。本特性 pin `0.2.0-rc.2`（dist-tag `next`）；若上游随后发布 0.2.0 GA 或 0.2.0-rc.3+，属后续例行小升级，不在本特性范围（070 先例：dist-tag 不可信、版本事实以 registry 实测为准）。
- **失败路径行为断言失配**：0.2.0 的工具失败恢复改变失败 step 的事件序列（补记恢复结果）——若 saolei-loop/team 等单测对失败路径有精确事件断言，按新事实重写断言（重写≠放松，070 D8 先例 `specs/070-dsh-upgrade/research.md`）。
- **FR-022 升级信号触发**：primitives 升级而 vendored theme 未重取时，FR-022 断言失败（primitives 0.2.0 新消费 3 个 token）——这是设计使然的升级信号，必须同步重取 theme 闭合，不得以扩大豁免集合替代重取。
- **packageExtensions 版本键漂移**：`packageExtensions` 以精确版本为键；键未随线更新则 pnpm 不应用扩展，primitives 运行时导入解析失败（构建期即暴露，非运行时隐患）。
- **升级后发现不兼容**：调研实测 0.2.0-rc.2 为纯增量（无 API 移除、无 peer 扩展），但若实现期发现无法适配的破坏，允许回退到上一可用线（0.1.7-rc.2）并在交付文档记录差异（先例 `specs/070-dsh-upgrade/spec.md` Edge Cases）。
- **lockfile 平台 optionalDep**：`node-addon-require-builtin@0.1.6` 的 `linux-x64-gnu` optionalDep 必须在 lockfile 在场（值不变，刷新后仍需验证；`survey/deepseek-harness-b1-bazel-packaging.md` §7 风险 2）。
- **shimmer 动画的无障碍与降级**：动画仅为进行中状态的装饰性表达，进行中语义不得仅由动画承载（既有 visually-hidden "正在生成" 语义保留）；动画不得改变文字可选中性与布局稳定性（回合完结动画停止后内容与完结前一致，无重挂载跳动）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 版本线统一：catalog（根 `pnpm-workspace.yaml`）中全部 `@deepseek-ai/dsh-*` 条目 MUST 统一 pin 到 `0.2.0-rc.2`（dsh 闭包整体一个版本单元；dsh 家族精确版本 pin，先例 `specs/049-agent-v2-dsh-init/spec.md` 版本线决策、`specs/070-dsh-upgrade/spec.md` FR-001）。
- **FR-002**: 目标版本线为 `0.2.0-rc.2`（registry `next` 线，2026-09-29 实测；与 FR-001 同一版本单元的互锁矩阵）；cordis 家族与互锁包值与 0.1.7 线相同且 MUST 保持：cordis `~4.0.4`（解析 4.0.4）、cordis-plugin-loader `1.0.5`、cordis-plugin-include `1.0.9`、cordis-plugin-group `1.0.4`、cordis-plugin-timer `1.1.6`、node-addon-require-builtin `0.1.6`、`@deepseek-ai/schemastery` `^3.18.4`。
- **FR-003**: 消费者范围为全仓库同线迁移：`third_party/dsh/core`、`projects/game/agent_v2`、`experimental/dsh/demo`、`common/js/dsh-plugins/*`、`projects/game/web/frontend`（ui-primitives 依赖 + vendored theme 重取）。
- **FR-004**: `third_party/dsh/core` 底座声明面（`package.json` 依赖清单——全部为 catalog `catalog:` 协议，键集合与文件内容零改动、值随 catalog 切换自动生效；`BUILD.bazel` npm_deps 枚举；`BUILD.bazel` 的 `runtime_pkg` 闭包注释；`version.ts` 快照标识 `dsh-core@0.2.0-rc.2`）MUST 同步 0.2.0-rc.2 线；底座保持零插件（仅框架核心 11 包清单，closure audit 断言①，口径见 FR-012）。
- **FR-005**: 0.2.0 线无新增 peer 义务（registry 实测：全部包 package.json 相对 0.1.7-rc.2 仅版本字符串变化）——各消费 package.json 的 dsh 依赖集合 MUST 保持不变（仅经 catalog 切值），不得引入 0.2.0 线不存在的新 dsh 包声明。
- **FR-006**: `projects/game/agent_v2` 与 `experimental/dsh/demo/agent` 的组合清单（`cordis.yml`）行集合 MUST 与升级前相同（0.2.0 无组合面变化）；既有单元测试全部通过，因 0.2.0 行为变化（工具失败恢复）失配的断言 MUST 按新事实重写且语义守恒，不得删除或跳过用例。
- **FR-007**: 升级后 agent_v2 对外服务行为 MUST 与升级前一致（既有四拓扑行为面：game-system 的对话流程/preset/team 群聊与工具隔离/planner memory、game-disconnect 的 desktop 断开恢复、game-memory-down 的 team 物化 fail-loud、game-stall 的 LLM 路由流停滞看护），以既有大型测试计划实际执行（部署→测试→清理闭环）全部通过为验收；工具调度失败恢复作为新增健壮性行为 MUST 有测试覆盖——恢复结果为两级保守语义（未配对调用标记 `TOOL_OUTCOME_UNKNOWN`〔结果未知、不可盲目重试〕、未启动调用标记 `TOOL_NOT_STARTED`），语义依据上游注记 https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/.agents/notes/implemented/bug-fix/2026-09-19-failed-step-tool-results.md 。
- **FR-008**: `projects/game/web/frontend` 的 `@deepseek-ai/dsh-client-ui-primitives` MUST 升级到 0.2.0-rc.2；pnpm `packageExtensions` 的两处版本键（primitives、client-store）MUST 同步为 `0.2.0-rc.2`，且两键内 `@deepseek-ai/*` 依赖值（client-store、util-code-language、util-workspace-path）MUST 随线更新为 `0.2.0-rc.2`（键集合与其余依赖集合不变——这些嵌套精确 pin 是 primitives devDependencies 的镜像，留旧线会导入旧线副本、破坏版本唯一）；除 FR-013 的设计跟进采用点外，前端组件源码仅在实际编译/测试暴露类型失配时按 0.2.0 类型面适配（调研预期零适配）。
- **FR-009**: `projects/game/web/frontend/src/dsh-theme/` MUST 从 `@deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2` tarball 重新 vendoring（按 `src/dsh-theme/README.md` 人工升级步骤：提取 region CSS、核对 STYLES 顺序、更新版本记录）；重取后 FR-022 消费覆盖断言 MUST 通过（primitives 0.2.0 新消费 token 全部由 sheets 定义），豁免集合不得扩大。
- **FR-010**: lockfile MUST 经 pnpm 官方流程刷新（`pnpm up`，不得手改 `pnpm-lock.yaml`），刷新后 lockfile 内全部 `@deepseek-ai/*` 解析 `0.2.0-rc.2`、cordis 全仓库唯一一份、`node-addon-require-builtin@0.1.6` 平台 optionalDep 在场。
- **FR-011**: 升级的版本事实与行为变化依据（registry 实测、tarball diff 结论、上游修复注记）MUST 在交付文档（research/plan）中记录来源与依据。
- **FR-012**: closure audit 三断言 MUST 通过：① 底座零插件、② 每个随行 dsh 包声明可追溯、③ 版本唯一；demo 大型测试计划实际执行全部通过。
- **FR-013**: 设计跟进采用范围 MUST 限定为既有页面有承载面的 0.2.0 新设计能力：流式运行态 shimmer 文字动画（ReasoningRow——即 ChatView 流式回合 THINK 尾块渲染组件——的折叠摘要 running 态经 TextShimmer 组件与 `--dsw-alias-label-shimmer` token 表达，DisclosureRow 头部经 `running` prop 启用 0.2.0 头部动画；回合完结后动画停止且文字保持可选中，"正在生成"无障碍语义保留）；随包升级自动生效的视觉增强随升级获得。无承载面的新能力（MenuGroup 菜单分组、Tooltip 点击钉住、轮次跳转/开关/文档选区 token、深潜水标签 token `--dsw-alias-label-deep-diving(-shimmer)`、异步问答模式 `user-question-reply`、MarkdownText 正文流的 shimmer）MUST NOT 引入承载面改动，defer 清单与理由 MUST 记录于交付文档。

### Key Entities

- **dsh 版本线（version line）**: 同一次 lockstep 发布的 dsh 家族版本集合 + 互锁的 cordis 家族精确版本，整体作为唯一版本单元。
- **框架核心底座（core baseline）**: `third_party/dsh/core` 的 11 包闭包清单（框架核心，零插件）。
- **声明面（declaration surface）**: catalog + `packageExtensions` 键与键内嵌套值 + 消费 package.json 依赖 + `BUILD.bazel` npm_deps 枚举与闭包注释 + closure audit 预期集，必须与实际随行闭包一致。
- **vendored token sheets**: `projects/game/web/frontend/src/dsh-theme/` 内官方设计 token 的源码形态副本（`--dsw-*` token 唯一权威），随 ui-theme 同线重取。
- **工具失败恢复结果（failed-step tool-result recovery）**: 0.2.0 起，失败的 step 关闭前为未配对工具调用补记的保守恢复结果（结果未知/未启动两级），保证会话历史对 provider 始终合法、对话可继续。
- **流式 shimmer 动画（streaming shimmer）**: 0.2.0 设计跟进采用的进行中状态视觉表达——运行态文字以共享高光动画呈现，完结后文字保持可选中；消费 `--dsw-alias-label-shimmer` token。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 升级后，lockfile 中全部 dsh 家族包解析到 `0.2.0-rc.2`（FR-001/FR-010 的量化验收），零跨线混装、零重复物理副本（cordis 全仓库唯一一份）。
- **SC-002**: 仓库全部既有构建与单元测试（dsh 相关：core、agent_v2、demo、dsh-plugins、web 前端）100% 通过（覆盖 FR-006/FR-007 的用例保全要求），无用例被删除、跳过或标记豁免；因 0.2.0 行为变化重写的断言语义守恒。
- **SC-003**: agent_v2 既有大型测试计划实际执行（部署→测试→清理闭环）全部用例通过，服务对外行为与升级前一致（四拓扑全回归：game-system 的对话/preset/team/memory + game-disconnect + game-memory-down + game-stall）。
- **SC-004**: demo 大型测试与 closure audit 三断言（plugin-free、声明可追溯、版本唯一）通过。
- **SC-005**: web 前端构建与组件测试全绿，FR-022 消费覆盖断言通过（theme 与 primitives 同线闭合）；流式运行态以 shimmer 动画表达（有组件测试覆盖），既有页面其余视觉与交互无回归。
- **SC-006**: 升级后仓库内（`specs/` 历史文档与本特性交付文档中的版本线对比表述除外）不存在 `0.1.7` 版本线残留——判据为全仓扫描 `0.1.7` 版本串**零命中**（`0.1.7-rc.2` 为其子集），覆盖声明面（catalog、`packageExtensions` 两处键及其嵌套 `@deepseek-ai/*` 值、package.json、`BUILD.bazel` 声明与注释、`pnpm-workspace.yaml` 说明注释）、`version.ts` 快照标识、全部 README 与源码（含测试）注释中的版本串与版本化 URL，以及把当前 pin/peer/persona Config 归属到旧线的版本线表述；扫描面为 `*.json`/`*.yaml`/`*.ts`/`*.tsx`/`*.bazel`/`*.bzl`/`*.md`/`*.go`（命令见 `quickstart.md` 场景 6）。`pnpm-workspace.yaml`、`third_party/dsh/core/version.ts`、`projects/game/web/frontend/src/dsh-theme/README.md` 三处版本记录同步为 `0.2.0-rc.2`；历史行为叙述按原则 VII 改写为终态表述（不保留"从 0.1.7 演进到现在"的叙述），演进历史由版本控制系统承载。

## Assumptions

- 0.2.0 GA 尚未发布（registry 仅 0.2.0-rc.1/rc.2，dist-tag `next` = 0.2.0-rc.2，2026-09-29 实测）；本特性 pin `0.2.0-rc.2`，与 070 取 `0.1.7-rc.2` 的先例同构。GA 发布后的升级属后续例行工作。
- 服务面（`projects/game/agent_v2`、`experimental/dsh/demo`、`common/js/dsh-plugins/*`）为纯版本线迁移，不引入新业务功能（070 Assumptions 先例）；前端面在版本线对齐基础上包含设计跟进，但仅采用既有页面有承载面的能力（流式 shimmer 动画，FR-013），无承载面的能力（菜单分组、Tooltip 点击钉住、轮次跳转、开关、文档选区、深潜水标签 token、异步问答协议快照）不在本特性引入，defer 清单记录于交付文档（070 D10 先例：复用决策显式记录避免重复调研）。
- 调研实测（registry tarball 逐字节 diff，2026-09-29）：覆盖 28 包（26 个 catalog dsh 条目 + client-store/client-theme），20 个逐字节相同；变更的 8 个包（dsh-session、dsh-agent-loop、dsh-app-boot、dsh-util-values、dsh-llm、dsh-agent-preset-registry、dsh-client-ui-primitives、dsh-client-ui-theme）均为向后兼容增量；全部包 package.json 无新增 peer 义务、无 exports 变化。据此预期组合清单与消费源码零适配——若实现期实测推翻（发现破坏性变更），按 Edge Cases 回退条款处理。
- cordis 家族与 node-addon-require-builtin 互锁值在 0.2.0 线与 0.1.7 线完全一致（上游未发布新版本），底座与 catalog 中这些条目值不变。
- preset 声明式模型在 0.2.0 线稳定（`dsh-agent-preset-registry` 仅协议快照更新），070 完成的 preset-authoring 重构成果与 Mongo preset 记录零迁移、零改动。
- 桌面（desktop）与 Go 侧（fake-llm、gateway、testplan Go 用例）不依赖 dsh 包，不在迁移面（070 Assumptions 先例）。
- `dsh-session` 新增导出 `ToolCallRecovery` 为 additive，无既有消费方受影响；其行为由 `dsh-agent-loop` 内部消费，本仓库不直接调用。
- `FR-022` 为本仓库既有需求编号（定义于 `specs/054-agent-v2-bugfixes/spec.md`：已渲染组件消费的 `--dsw-*` ⊆ vendored sheets 定义集 ∪ 冻结豁免集）；本特性沿用其作为 theme 重取的验收门禁，不重新定义。本文档其余 `FR-###`/`SC-###` 均为本特性自有编号。
