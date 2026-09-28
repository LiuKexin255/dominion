# Tasks: dsh 升级到 0.1.7 线（core 底座 + agent_v2 官方插件 + preset 模型重构）

**Input**: Design documents from `/specs/070-dsh-upgrade/`

**Prerequisites**: plan.md（必需）、spec.md（用户故事）、research.md、data-model.md、contracts/、quickstart.md

**Tests**: 编译与单测为各任务内置门禁（宪法 IV：作为开发任务的一部分，不单列 task）；大型测试（`guitar run`）与 Mongo 迁移在 Phase 8 作为独立验收环节执行（宪法 VI、用户要求迁移最后）。closure audit（系统级审计，非改动代码的单测）作为 US1/US4 的独立验收证据保留。

**Organization**: 按用户故事组织（US1 底座 / US2 agent_v2 插件 / US3 preset 重构 / US4 生态同线 / US5 前端）；Phase 1 声明面切换与 Phase 2 preset-authoring 引擎为公共前置。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1–US5；Setup/Foundational/收尾无标签）
- 所有任务带精确文件路径

---

## Phase 1: Setup（版本线与声明面切换 — 共享地基）

**Purpose**: catalog 全量切到 0.1.7-rc.2 线 + 全部消费方 package.json 依赖集合更新 + lockfile/gazelle 落地。本 phase 只做**声明面**（安装级），不改源码与组合清单。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`（ESM 声明面与依赖清单约定）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` §引用 声明的仓库基准）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§1 包声明——package.json 编辑不得破坏 `"type": "module"`；style/javascript.md 的间接引用）
- 官方文档: 无
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/research.md`（D1 互锁矩阵、D6 声明面清单、D13 流程）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§1 catalog 契约、§4 声明面扩展表）；`survey/deepseek-harness-b1-bazel-packaging.md`（§7 风险 2：node-addon optionalDep）；`specs/070-dsh-upgrade/quickstart.md`（场景 1 lockfile 检查命令）

- [X] T001 更新根 `pnpm-workspace.yaml` catalog（完整新增清单以 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §1/§4 与 `specs/070-dsh-upgrade/research.md` D6 为准）：全部 `@deepseek-ai/dsh-*` 改精确 `0.1.7-rc.2`；新增条目 agent_v2 侧 `dsh-agent-preset-registry`/`dsh-agent-preset`/`dsh-session-projection`/`dsh-settings`/`dsh-typert-protocol`/`dsh-util-values`/`dsh-brand`/`dsh-session-persistence`/`dsh-sandbox`/`dsh-sandbox-policy`/`dsh-ptc-runtime`/`dsh-user-approval`，demo 侧 llm-deepseek peer 面 `dsh-fs`/`dsh-credentials`/`dsh-attachment`/`dsh-anonymous-user-id`/`dsh-deepseek-llm-api-extensions`（全 `0.1.7-rc.2`；`dsh-workspace` 为 optional peer，不引入——research D6）；互锁值 `"@deepseek-ai/cordis": "~4.0.4"`、loader `1.0.5`、include `1.0.9`、group `1.0.4`、timer `1.1.6`、`node-addon-require-builtin: "0.1.6"`、`"@deepseek-ai/schemastery": "^3.18.4"`、`"@deepseek-ai/dsh-client-ui-primitives": "0.1.7-rc.2"`；删除 `@deepseek-ai/dsh-agent-presets` 条目
- [X] T002 更新 `projects/game/agent_v2/package.json` 依赖集合：删 `@deepseek-ai/dsh-agent-presets`；增 Phase1 清单中除 ui 相关外的全部新包（`catalog:` 协议；peer 义务见 research D6 表）
- [X] T003 [P] 更新 `experimental/dsh/demo/agent/package.json` 依赖集合：删 `@deepseek-ai/dsh-agent-presets`；增 D6 demo 列（含 llm-deepseek 0.1.7 peer 面：`dsh-fs`/`dsh-credentials`/`dsh-attachment`/`dsh-anonymous-user-id`/`dsh-deepseek-llm-api-extensions`/`dsh-settings`/`dsh-timeout` 及通用 seam 包；上述新条目已由 T001 显式写入 catalog）
- [X] T004 [P] 更新 `common/js/dsh-plugins/preset-authoring/package.json`：dependencies 删 `@deepseek-ai/dsh-agent-presets`，增 `@deepseek-ai/dsh-agent-preset-registry: "catalog:"`（schemastery/js-yaml/mongodb 不动）
- [X] T005 执行 `bazel run @pnpm -- --dir /mnt/code/dominion up` 刷新 `pnpm-lock.yaml` 并验证（不得手改 lockfile）：全部 `@deepseek-ai/*` 解析 `0.1.7-rc.2`；`@deepseek-ai/dsh-agent-presets` 零出现；cordis 仅一份物理版本（4.0.4）；`node-addon-require-builtin@0.1.6` 的 `linux-x64-gnu` optionalDep 在场（quickstart 场景 1 命令）
- [X] T006 在 `third_party/dsh/core`、`projects/game/agent_v2`、`experimental/dsh/demo`、`common/js/dsh-plugins` 目录下执行 `bazel run //:gazelle <dir>` 刷新 `BUILD.bazel` 的 node_modules link targets；执行 `bazel mod tidy`；`bazel build //third_party/dsh/core:version_lib` 通过（此阶段不要求服务 target 编译通过）

**Checkpoint**: 声明面同线、install 成功；服务源码尚未适配（后续 phase 修复）。

---

## Phase 2: Foundational（preset-authoring 新模型引擎 — 阻塞 US2/US3/US4）

**Purpose**: `@dominion/dsh-preset-authoring` 按 `ctx.agentPresets.register` 物化模型重构，服务面契约冻结。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`（vitest_test data 镜像规则、DI mock 约定）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准：命名导出/import type/箭头函数等语言规范）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则：相对导入 `.js` 扩展名、`export type` 显式、禁 `require()`；§5 测试 data 只含 raw `.ts` 源——style/javascript.md 的间接引用）；`specs/019-js-test-reliability/spec.md`（测试执行模型与 mock 根因背景——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 与 `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §5 的间接引用）
- 官方文档: https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/architecture/2026-09-18-declarative-agent-presets.md （模型语义）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset-registry/README.md （registry README：Config/生命周期/Remote 面）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset-registry/src/index.ts （register/disposer/错误语义源码）
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/contracts/preset-model-mapping.md`（全部——本 phase 的实现契约）；`specs/070-dsh-upgrade/data-model.md`（§3.2–§3.5 实体与物化流）；`specs/070-dsh-upgrade/research.md`（D2/D3）

- [X] T007 重构 `common/js/dsh-plugins/preset-authoring/src/derive.ts`：删除 `DeriveFs`/`nodeDeriveFs`/`COMPOSITION_FILE_NAME`/临时目录链路；`deriveComposition(record, template)` 改为纯内存构造 plugins rows（模板声明行快照 + persona 子行 `config.prefix = record.persona`；persona 行定位沿用 `PERSONA_ROW_NAME`）；返回值从 `AgentPreset {trust}` 改为 registry `register` 输入（`{id, plugins, name?, description?}`）
- [X] T008 重构 `common/js/dsh-plugins/preset-authoring/src/index.ts`：`mountPreset`/`AgentPreset`（dsh-agent-presets）import 换为经 ctx 注入的 `ctx.agentPresets.register/unregister` 生命周期（create→register、update→unregister+register、remove→unregister、boot 时 `list()` 全量物化；disposer 由插件 effect 持有）；`PresetAuthoringConfig` 增 `templates` 键（player/planner 模板声明快照，contracts §2.3）；store 写与 register 同步（store 为唯一事实源）；`inject = ["agentPresets"]` 不变；`PresetAuthoringService` 六方法签名与 `ComposeResult.agentPreset` 语义冻结不动
- [X] T009 更新 `common/js/dsh-plugins/preset-authoring/BUILD.bazel`：`@deepseek-ai/dsh-agent-presets` 相关 deps/npm_deps/data 换为 `@deepseek-ai/dsh-agent-preset-registry`（gazelle 后核对）
- [X] T010 重写 `common/js/dsh-plugins/preset-authoring/src/index.test.ts` 与 `src/derive.test.ts`：fake registry（DI seam）断言 create/update/remove 的 register/unregister 调用序列与 rows 内容（persona `prefix`）；templateRules 拒绝路径（INVALID_ARGUMENT、不落产物）保留；新增两条语义用例——激活失败定义留 roster 拒新绑定、update 后运行中引用不受损（修订保留）；内置门禁：`bazel test //common/js/dsh-plugins/preset-authoring/...` 全绿

**Checkpoint**: preset-authoring 单测全绿；`store.test.ts`/`store.mongo.test.ts` 不改即绿（store 冻结证明）。

---

## Phase 3: User Story 1 — 框架核心底座升级（Priority: P1）🎯 MVP 核心

**Goal**: `third_party/dsh/core` 底座声明面与快照标识同线，断言①（plugin-free）通过。

**Independent Test**: `bazel build //third_party/dsh/core:runtime_pkg` 通过；`bazel test //experimental/dsh/demo/testplan:closure_audit_test --test_filter=TestCoreBaselinePluginFree`（断言①）通过；lockfile 终态满足 SC-001。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`（version.ts 为 TS 源）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则——version.ts 快照注释随附；style/javascript.md 的间接引用）
- 官方文档: 无
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/research.md`（D1/D12）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§5 closure audit 契约）；`specs/070-dsh-upgrade/quickstart.md`（场景 1）

- [X] T011 [US1] 更新 `third_party/dsh/core/version.ts`：`DSH_CORE_SNAPSHOT = 'dsh-core@0.1.7-rc.2'`；注释改写为 0.1.7 线互锁事实（11 包清单不变；cordis ~4.0.4 / loader 1.0.5 / include 1.0.9 / group 1.0.4 / timer 1.1.6 / node-addon 0.1.6 互锁；依据 `specs/070-dsh-upgrade/research.md` D1——终态表述，不保留 0.1.1 演进叙述）；内置编译门禁：`bazel build //third_party/dsh/core:runtime_pkg` 通过
- [X] T012 [US1] 更新 `third_party/dsh/core/BUILD.bazel` 中 `runtime_pkg` 的 npm_deps 上方注释：闭包说明同步 0.1.7 线互锁事实并引用 `specs/070-dsh-upgrade/research.md`（D1/D12）作为依据（`npm_deps` 11 条列表本身不变）；内置编译门禁：`bazel build //third_party/dsh/core:runtime_pkg` 通过
- [X] T013 [US1] US1 验收（SC-001/SC-004）（拆分执行：SC-001 lockfile 终检已于 Phase 3 完成并复核；closure audit 断言①运行经用户 2026-09-27 裁定推迟至 T021 后执行，届时闭环）：closure audit 断言① 通过（`bazel test //experimental/dsh/demo/testplan:closure_audit_test --test_filter=TestCoreBaselinePluginFree`；`d6CorePackages` 口径不变、代码零改动），②③ 断言依赖 demo 声明面随 T023 验证；SC-001 终检并记录证据——`pnpm-lock.yaml` 内 `@deepseek-ai/dsh-agent-presets` 零出现、每个 `@deepseek-ai/*` 仅一个版本、cordis 物理副本唯一（quickstart 场景 1 命令；结果记入 PR 描述）

**Checkpoint**: US1 完成——底座可被服务 `runtime_deps` 引用且同线（MVP：Phase 1–3 交付可独立验证的版本线地基）。

---

## Phase 4: User Story 3 — game preset 面按新模型迁移（Priority: P1）

**Goal**: agent_v2 组合清单 preset 面切换到 registry + 声明式模板；目录/env 装配面移除；preset 语义五项在新模型下成立。

**Independent Test**: preset 相关单测（`presets.test.ts`/`server.test.ts`/`session.test.ts` preset 路径）全绿；D5 门禁用例（无名解析 not-found）通过。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则——style/javascript.md 的间接引用）；`specs/019-js-test-reliability/spec.md`（测试执行模型与 mock 根因背景——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）
- 官方文档: https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset-registry/README.md （Config `default`/`selectedDefault`、声明即行）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset/README.md （声明 Config：id/plugins/name/description/order）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/persona/README.md （persona `prefix`/`suffix`）
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/contracts/preset-model-mapping.md`（§2–§5）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 agent_v2 行集合 YAML）；`specs/070-dsh-upgrade/data-model.md`（§2.2 persona 迁移、§3 实体）；`specs/070-dsh-upgrade/research.md`（D4/D5）

- [X] T014 [US3] 迁移 `projects/game/agent_v2/cordis.yml` preset 面：删 `agent-presets` 行；按 contracts/composition-manifest.md §2 目标态新增 `agent-preset-registry` 行（`config: { default: '' }`）与 `preset-player`/`preset-planner` 两行 `@deepseek-ai/dsh-agent-preset` 声明（persona 文案自 `projects/game/agent_v2/preset-templates/{player,planner}/*/agent.cordis.yml` 的 `text` 迁移为 `prefix`；`preset.yml` 的 name/description 并入行 config；player 子行含 `@dominion/dsh-saolei`、planner 含 `@dominion/dsh-memory`）；`preset-authoring` 行 config 增 `templates` 快照键（与声明行同构数据）。内置门禁：`presets.test.ts`/`session.test.ts`/`server.test.ts` 的 preset 路径用例通过（`dsh.test.ts` 全清单断言在 T018 完成后联跑）
- [X] T015 [US3] 移除模板目录装配面：删除 `projects/game/agent_v2/preset-templates/` 目录；`projects/game/agent_v2/src/dsh.ts` 删除 `PRESET_TEMPLATES_ROOT` 解析链（`PRESET_TEMPLATES_ROOT_ENV`/`PRESET_TEMPLATES_DIR_NAME`/`resolvePresetTemplatesRoot` 及 env 注入）；`projects/game/agent_v2/BUILD.bazel` 删除 `preset-templates/**` artifact glob；`common/js/dsh-plugins/memory/src/index.ts:4` 注释中的模板路径引用同步改写；`projects/game/agent_v2/README.md` preset 面文档同步终态；全仓 grep `PRESET_TEMPLATES_ROOT|preset-templates`（agent_v2 与 dsh-plugins 面）零残留（demo 面属 Phase 6 T020/T021）
- [X] T016 [US3] 在 `projects/game/agent_v2/src/dsh.test.ts` 增加 D5 门禁用例"keeps preset selection fail-loud: the registry default points at no preset"（registry 行 `config` 恰为 `{ default: '' }`，声明 id 集合无空串 → 无名解析 not-found）。**裁定门禁实测**：`''` 通过 registry Config schema（`AgentPresetRegistry.Config` 校验 `{default:''}` 接受、`{}` 拒绝），采用 `''`，不启用 sentinel 回退；结论已写入 `specs/070-dsh-upgrade/research.md` D5 终态结论小节

**Checkpoint**: preset 语义（双角色池/CRUD/派生/fail-loud/team 隔离的服务侧输入）在新模型组合下成立。

---

## Phase 5: User Story 2 — agent_v2 官方插件升级与断言重写（Priority: P1）

**Goal**: agent_v2 组合清单终态（含 session-projection 新行）+ 全部单测绿；源码适配（预计极薄，research D7）。

**Independent Test**: `bazel build //projects/game/agent_v2/... && bazel test //projects/game/agent_v2/...` 全绿。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`（vitest_test `data` 镜像 `:lib` node_modules deps——dsh.test.ts 改动后 data 列表核对）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则、§5 测试 data 约定——style/javascript.md 的间接引用）；`specs/019-js-test-reliability/spec.md`（测试执行模型与 mock 根因背景——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）
- 官方文档: https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/session/session-projection/README.md （sessionProjections 服务与 inject 要求）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/agent-loop/src/index.ts （`static inject` 含 `sessionProjections` 的源码事实）
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/research.md`（D6 peer 义务矩阵、D7 符号存活表、D8 断言重写）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2/§4）；`specs/070-dsh-upgrade/data-model.md`（§2.1 行集合）；`specs/070-dsh-upgrade/quickstart.md`（场景 2）

- [X] T017 [US2] `projects/game/agent_v2/cordis.yml` 增 `{ id: session-projection, name: '@deepseek-ai/dsh-session-projection' }` 行（位置在 session 行后，contracts §2 顺序）；`projects/game/agent_v2/BUILD.bazel` 的 `artifact_pkg_js.server_pkg.npm_deps` 与 `ts_project`/vitest data 补全部新包 link targets（`:node_modules/@deepseek-ai/dsh-session-projection`、`:node_modules/@deepseek-ai/dsh-agent-preset-registry`、`:node_modules/@deepseek-ai/dsh-agent-preset` 等，对齐 package.json；删除 agent-presets 条目）
- [X] T018 [US2] 重写 `projects/game/agent_v2/src/dsh.test.ts` 组合清单断言：按 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 的目标行集合做绝对精确断言（行 id 顺序全量，不依赖运行时基线行数）；registry 行 `default: ''`、模板声明行 config（id/plugins/name/description）、persona 子行 `prefix`；保留"无 `@deepseek-ai/dsh-agent-spine-demo`"等既有负断言；"无 persistence/settings 行"断言更新为精确行集合断言（settings/session-persistence 确不进行）
- [X] T019 [US2] 源适配编译修复：`projects/game/agent_v2/src/{dsh,session,system-prompt,history,server}.ts` 按 0.1.7 类型面修复编译（research D7 全符号存活，预期仅注释/env 链路与类型微调；如遇未预见 API 变更，以 unpkg `@deepseek-ai/<pkg>@0.1.7-rc.2/lib/types/` 实测为准并在 PR 记录）。内置门禁：`bazel test //projects/game/agent_v2/...` 全绿（含 T014 preset 路径用例与 T016 门禁用例联跑；无用例删除/跳过，SC-002）

**Checkpoint**: agent_v2 单元面完整——US2+US3 在单测层达成，待 Phase 8 大型测试行为验收。

---

## Phase 6: User Story 4 — demo 与其余 dsh 插件同线迁移（Priority: P2）

**Goal**: demo（含 preset 演示、llm-deepseek peer 面）与其余 workspace 插件在 0.1.7 线全绿；closure audit ②③ 通过。

**Independent Test**: `bazel test //experimental/dsh/demo/... //common/js/dsh-plugins/...` 全绿；closure audit 全部断言通过。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3/§5——JS 侧适配）；`specs/019-js-test-reliability/spec.md`（测试执行模型与 mock 根因背景——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）；`style/golang.md`（§单元测试——preset_test.go 适配时遵守）、https://google.github.io/styleguide/go/guide 、https://google.github.io/styleguide/go/decisions 、https://google.github.io/styleguide/go/best-practices （Google Go Style Guide 三份——`style/golang.md` §引用 声明的外部基准）；`style/large_test.md`（不新建测试计划 YAML、用例按模块归位）
- 官方文档: https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset-registry/README.md （registry Config：`default`/`selectedDefault`、声明即行）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/agent-preset/README.md （声明 Config：id/plugins/name/description/order）；https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/preset/persona/README.md （demo persona 行 prefix 迁移）
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/contracts/composition-manifest.md`（§3 demo 契约、§5 audit 契约）；`specs/070-dsh-upgrade/research.md`（D6 demo 列、D8 断言面）；`specs/070-dsh-upgrade/quickstart.md`（场景 4）

- [X] T020 [US4] 迁移 `experimental/dsh/demo/agent/cordis.yml`：删 `agent-presets` 行；增 `session-projection`、`agent-preset-registry`（demo `default` 指向 `demo-standard`，demo 允许默认回落）与 `preset-demo-standard`/`preset-demo-tools` 声明行（persona `text`→`prefix`；demo-tools 含 `@dominion/dsh-demo-echo` 子行）；`system-prompt` 行 `persona` config 键按 0.1.7 Config 校验迁移；删除 `presets-templates/` 目录与 `experimental/dsh/demo/testplan/deploy.yaml` 中 roster roots 相关装配；`src/composition.test.ts` 断言同步
- [X] T021 [US4] 适配 demo 源与测试：`experimental/dsh/demo/agent/src/{dsh,session,server}.ts` 与 `dsh.test.ts`/`composition.test.ts`/`server.test.ts`/`session.test.ts`；`experimental/dsh/demo/testplan/preset_test.go` 16 用例按新模型语义适配（行为等价：preset 组合/创作生命周期；表驱动与 given/when/then 结构保持，遵守 `style/golang.md` §单元测试）；`experimental/dsh/demo/agent/BUILD.bazel` npm_deps/data 对齐 package.json 新集合。内置门禁：`bazel test //experimental/dsh/demo/... //common/js/dsh-plugins/...` 全绿
- [X] T022 [P] [US4] 核对其余 8 个 workspace 插件 BUILD 面（`common/js/dsh-plugins/{llm-glm,llm-opencode-go,memory,memory-service,saolei,saolei-loop,team,desktop-bridge}/BUILD.bazel`）：符号面 research D7 已验证存活，预期仅 node_modules link target 刷新与 vitest data 核对；如编译报错按 D7 表排查并记录
- [X] T023 [US4] US4 验收（SC-004）：closure audit 全部断言（①plugin-free ②声明可追溯 ③版本唯一）通过——`bazel test //experimental/dsh/demo/testplan:closure_audit_test`；expected set 由声明面数据驱动重算，测试代码零改动为预期

**Checkpoint**: 全仓 dsh 消费面同线且单测绿；SC-004 的 audit 面达成。

---

## Phase 7: User Story 5 — web 前端 UI 原语对齐（Priority: P3）

**Goal**: `dsh-client-ui-primitives@0.1.7-rc.2` 编译适配 + vendored theme 重取；前端测试绿。

**Independent Test**: `bazel build //projects/game/web/frontend/... && bazel test //projects/game/web/frontend/...` 全绿。

**文档清单（必读）**:
- 代码规范文档: `style/javascript.md`；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` 引用的仓库基准；注意前端包为 bundler 体系，`specs/048.../esm-package-conventions.md` 的 Node ESM 审计范围不含 `*/frontend` 包，其 `.js` 扩展名导入等规则以前端既有书写为准）；`specs/019-js-test-reliability/spec.md`（测试执行模型与 mock 根因背景——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）
- 官方文档: https://unpkg.com/@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2/lib/types/index.d.ts （0.1.7 组件类型面——Button/Menu/MarkdownText/JsonBlock/StateDot/DisclosureRow/Input 等已确认在场；适配时以此为准）；https://unpkg.com/@deepseek-ai/dsh-client-ui-theme@0.1.7-rc.2/ （vendored theme 包文件面：`src/dsh-theme/` 逐文件比对基准）
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/research.md`（D9）；`specs/070-dsh-upgrade/quickstart.md`（场景 5）

- [X] T024 [US5] 前端升级适配：`projects/game/web/frontend` 对 `@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2` 编译修复（消费文件清单见 `src/App.tsx`、`src/components/{ChatView,SessionList,ReasoningRow,ToolCard,WikiView,PresetsView,SystemPromptOverlay,TeamSettingsPanel}.tsx`；如组件 API 变更按类型面适配）；`src/dsh-theme/` 从 `@deepseek-ai/dsh-client-ui-theme@0.1.7-rc.2` 重新 vendoring（theme 非 catalog 依赖，按 registry/unpkg 0.1.7-rc.2 包文件重取，保留 vendored 形态）；`src/theme.css` 的 `lib/markdown/CodeBlock.module.css` 引用路径/类名契约重验。内置门禁：`bazel test //projects/game/web/frontend/...` 全绿（含 `dsh-theme.test.ts`/`ChatView.test.tsx`；无用例删除，SC-002）

**Checkpoint**: 全部用户故事完成；进入验收与迁移。

---

## Phase 8: 验收、迁移与收尾（Polish & Cross-cutting）

**Purpose**: 大型测试实际执行（宪法 VI）→ Mongo 迁移（用户要求：最后，全部验证后）→ 残留清理与证据汇总。

**文档清单（必读）**:
- 代码规范文档: `style/large_test.md`（执行规范：每系统一份计划、guitar run 闭环）；`style/golang.md`、https://google.github.io/styleguide/go/guide 、https://google.github.io/styleguide/go/decisions 、https://google.github.io/styleguide/go/best-practices （若修复 Go 测试用例；后三者为 `style/golang.md` §引用 声明的外部基准）；`style/javascript.md`、https://google.github.io/styleguide/tsguide.html 、`specs/048-js-esm-migration/contracts/esm-package-conventions.md` 、`specs/019-js-test-reliability/contracts/run-vitest-shim.md` （修复循环若触及 TS 源码/测试时的仓库基准、ESM 契约与 shim 退出码契约）；`style/mongo.md`（T027 删/重建 `game_agent_v2.presets` collection 的库表约定）
- 官方文档: 无
- 技术文章/技术参考文档: `specs/070-dsh-upgrade/contracts/mongo-preset-migration.md`（runbook 全文）；`projects/game/agent_v2.proto`（迁移契约 PresetService API 定义——`contracts/mongo-preset-migration.md` §1 的引用）；`specs/070-dsh-upgrade/quickstart.md`（全部六场景）

- [X] T025 大型测试验收（game）：经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`——四拓扑全部用例通过（game-system：对话/preset CRUD+物化/team 群聊与工具隔离/planner memory；game-disconnect：desktop 断开恢复；game-memory-down：team 物化 fail-loud；game-stall：LLM 路由流停滞看护）（部署→测试→清理闭环；失败则修复相关代码/用例后重跑直至全绿，禁止跳过；用例修复遵守 `style/golang.md`，不新建测试计划 YAML）；执行证据记入 PR
- [ ] T026 大型测试验收（demo）：`guitar run experimental/dsh/demo/testplan/interface_test.yaml`——`testplan_test`/`multiturn_test`/`preset_test` 全部通过；同 T025 修复闭环要求；执行证据记入 PR
- [ ] T027 Mongo preset 迁移（**最后执行**，前置=T025/T026 全绿）：按 `specs/070-dsh-upgrade/contracts/mongo-preset-migration.md` 执行——旧版本 `ListPresets` 导出存档（API：`projects/game/agent_v2.proto` PresetService）→ 新版本部署 → 试插探测 → 回灌（4a）或删后重建（4b，服务端校验拒绝旧形态时）→ `ListPresets` 全量 diff + team 物化冒烟（player/planner 各一）；存档与校验和保留至发布窗口关闭；执行证据记入 PR
- [ ] T028 收尾检查（SC-005/SC-006 + FR-011）：按 SC-006 界定范围 grep 旧线残留（`0.1.1-rc.2`、`dsh-agent-presets`、`PRESET_TEMPLATES_ROOT`、`preset-templates`/`presets-templates` 目录名——范围 = catalog/package.json/BUILD 声明面、源码、注释、版本快照标识、模板目录配置；豁免版本控制引用、fake-llm testdata 中已确认项，以及 research/data-model/mongo runbook 中迁移所必需的旧→新映射记录）零残留；`specs/070-dsh-upgrade/` 交付文档终态复核（引用链接有效且来源可追溯——FR-011、Mermaid 渲染、无演进叙述）；quickstart.md 六场景全部复核通过；汇总各 phase 证据完成 PR

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无依赖，立即开始；**阻塞全部后续 phase**（声明面/lockfile 是一切编译的前提）
- **Phase 2（Foundational）**: 依赖 Phase 1；**阻塞 Phase 4/5/6**（preset-authoring 是 agent_v2 与 demo 组合的引擎件）
- **Phase 3（US1）**: 依赖 Phase 1（与 Phase 2 可并行——不同文件）
- **Phase 4（US3）**: 依赖 Phase 2
- **Phase 5（US2）**: 依赖 Phase 4（同一文件 `cordis.yml`/`dsh.test.ts` 串行，且全清单断言需 preset 行先落）
- **Phase 6（US4）**: 依赖 Phase 2（demo 组合用 preset-authoring）；T022 可与 T020/T021 并行
- **Phase 7（US5）**: 依赖 Phase 1（仅 catalog；与其他故事可并行）
- **Phase 8**: 依赖 Phase 3–7 全部完成；T027 依赖 T025+T026（迁移最后，用户要求）

### User Story Dependencies

- **US1（P1）**: Phase 1 后即可（MVP 核心）
- **US3（P1）**: Phase 2 后；与 US1 无文件冲突
- **US2（P1）**: 依赖 US3 的组合清单与断言基础（`projects/game/agent_v2/cordis.yml`、`src/dsh.test.ts` 同文件串行）
- **US4（P2）**: 依赖 Foundational；不依赖 US2/US3 完成（但 closure audit ② 需要 demo 声明面就绪，即本 phase 内闭环）
- **US5（P3）**: 仅依赖 Phase 1，可全程并行
- **迁移（Phase 8 T027）**: 依赖全部验证（T025/T026）

### Within Each User Story

- 声明面 → 组合清单/源码 → 断言/源码内置门禁（每任务完成即跑相关 `bazel build`/`bazel test`，宪法 IV）
- 同文件任务严格串行（Phase 4→5 的 cordis.yml/dsh.test.ts）

### Parallel Opportunities

- Phase 1: T003 ∥ T004（不同 package.json；T002 因与 T001 同为 agent_v2 依赖面建议随 T001 顺序执行）
- Phase 2 与 Phase 3 完全并行（preset-authoring vs third_party/dsh/core，不同目录）
- Phase 6: T022 ∥ T020/T021（8 插件 BUILD 与 demo 目录互斥）
- Phase 7 与 Phase 3–6 全程并行（前端目录互斥）
- Phase 8: T025 与 T026 可并行（不同测试计划，资源允许时）

---

## Parallel Example: Phase 2 ∥ Phase 3

```bash
# 开发者 A（或串行第一段）:
Task: "T007 重构 common/js/dsh-plugins/preset-authoring/src/derive.ts ..."
Task: "T008 重构 common/js/dsh-plugins/preset-authoring/src/index.ts ..."

# 同时（不同目录）:
Task: "T011 [US1] 更新 third_party/dsh/core/version.ts ..."
Task: "T012 [US1] 更新 third_party/dsh/core/BUILD.bazel 注释 ..."
```

---

## Implementation Strategy

### MVP First（版本线地基 = Phase 1–3）

1. Phase 1: catalog/声明面/lockfile 落地（可独立验证：install 成功、同线）
2. Phase 2 与 Phase 3 并行推进
3. **STOP and VALIDATE**: US1 验收（T013）+ preset-authoring 单测（T010）独立验证
4. 此时仓库处于"声明面同线、引擎就绪、服务面待适配"的可回滚检查点

### Incremental Delivery

1. Setup + Foundational + US1 → 版本地基（可验证：audit ①、SC-001）
2. + US3 → preset 面新模型（可验证：preset 路径单测、D5 门禁）
3. + US2 → agent_v2 完整（可验证：`bazel test //projects/game/agent_v2/...` 全绿）
4. + US4 → 生态同线（可验证：demo/插件单测 + closure audit 全断言）
5. + US5 → 前端对齐（可验证：前端测试绿）
6. + Phase 8 → 大型测试四拓扑 + demo 接口全绿 → **最后** Mongo 迁移 → 收尾（SC-001–SC-006 全达成）

### Parallel Team Strategy

- 开发者 A: Phase 1 → Phase 2（preset-authoring 引擎）
- 开发者 B: Phase 3（US1 底座）→ Phase 7（US5 前端）
- 汇合: Phase 4 → 5（agent_v2，同文件串行）→ Phase 6 → Phase 8

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖
- 每任务完成即执行相关 `bazel build`/`bazel test`（宪法 IV，不单列 task）；closure audit 与大型测试为独立验收环节（宪法 IV/VI）
- 大型测试（T025/T026）与迁移（T027）为独立验收任务（宪法 VI + 用户"迁移最后"要求）
- D5（default 空串）与前端符号契约（D9）为实现期门禁：实测结论如与 research 预设不符，按回退方案执行并回写 research.md
- 任何 task 中发现的 API 事实与 research D7 表不符：以 unpkg 0.1.7 类型面实测为准，并在 PR 记录差异
- **Phase 编号口径**：本文件 Phase 1–8 为唯一权威编号（plan.md/research.md 引用时以本文件为准）；与用户故事非一一对应（Phase 4 = US3、Phase 5 = US2），顺序由文件依赖（`cordis.yml`/`dsh.test.ts` 串行）决定，非优先级；`specs/070-dsh-upgrade/research.md` D14 的分组为建议顺序，其 P1–P7 与本文 Phase 的对应关系见该节映射说明
