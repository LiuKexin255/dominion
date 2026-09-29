---

description: "Task list for dsh 0.2.0 line upgrade (core baseline + official plugins + web webUI)"

---

# Tasks: dsh 升级到 0.2.0 线（core 底座 + 官方插件 + web 前端 webUI）

**Input**: Design documents from `/specs/073-dsh-0.2-upgrade/`

**Prerequisites**: plan.md（必需）、spec.md（用户故事 US1–US4）、research.md（D1–D8）、data-model.md、contracts/（declaration-surface / theme-vendoring / shimmer-adoption）、quickstart.md

**Tests**: 编译与单测为各任务内置门禁（宪法 IV：作为开发任务的一部分，不单列 task）；测试代码交付物（断言重写、shimmer 组件测试）与失败恢复覆盖为 spec 显式要求（FR-006/FR-007/US3 场景 3），内嵌于对应 story；大型测试在 Phase 6 作为独立验收 task（宪法 VI）。

**Organization**: Phase 1 声明面切换为全部 story 的共享地基（US 无标签）；Phase 2–5 按用户故事（US1→US2→US3→US4，P1 先于 P2）；Phase 6 收尾验收。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件/目录、无未完成依赖）
- **[Story]**: 所属用户故事（US1–US4；Setup/收尾无标签）
- 所有任务带精确文件路径

---

## Phase 1: Setup（版本线声明面切换 — 共享地基）

**Purpose**: catalog 切值 + packageExtensions 版本键与嵌套 `@deepseek-ai/*` 值 + lockfile/gazelle 落地。本 phase 只做**声明面**（安装级），消费 package.json 的 dsh 依赖键集合零变化（spec FR-005）。

**文档清单（必读）**:

- **代码规范文档**: `style/javascript.md`（§模块系统 (ESM)——**仅包级声明契约层面**，其契约正文为 `specs/048-js-esm-migration/contracts/esm-package-conventions.md` §1〔已列〕；该节内 §CJS 第三方依赖导入约定（依据 `specs/048-js-esm-migration/research.md` R5 白名单）、§OTel 插桩与 loader hook（指向 `specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`）、§测试 三面不涉本 phase 的声明面改动，不属本 phase 阅读面）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` §引用 声明的仓库基准）
- **官方文档**: 无
- **技术文章/技术参考文档**: `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§1 包声明——package.json/catalog 编辑不得破坏 `"type": "module"` 等包级契约；`style/javascript.md` 的间接引用）；`specs/073-dsh-0.2-upgrade/data-model.md`（§1.1 dsh 版本线与 cordis 唯一副本、§2 依赖序——本 phase 切值与 lockfile 刷新的校验规则口径）；`specs/070-dsh-upgrade/tasks.md`（Phase 1 声明面流程先例——`specs/073-dsh-0.2-upgrade/research.md` D3 指明的复用来源）；`specs/073-dsh-0.2-upgrade/contracts/declaration-surface.md`（§1–§5 全部——本 phase 的实现契约）；`specs/073-dsh-0.2-upgrade/research.md`（D1 互锁矩阵、D2 28 包 diff 结论、D3 变更清单）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 1 lockfile 验证命令）；`survey/deepseek-harness-b1-bazel-packaging.md`（§7 风险 2——native 平台包 optionalDep 形态）

- [X] T001 更新根 `pnpm-workspace.yaml`：catalog 中 26 个 `@deepseek-ai/dsh-*` 条目值 `0.1.7-rc.2` → `0.2.0-rc.2`（互锁条目零改动：`@deepseek-ai/cordis` `~4.0.4`、cordis-plugin-loader `1.0.5`、include `1.0.9`、group `1.0.4`、timer `1.1.6`、`node-addon-require-builtin` `0.1.6`、`@deepseek-ai/schemastery` `^3.18.4`——0.2.0 线 peer 互锁值与 0.1.7 线相同，research.md D1）；`packageExtensions` 两处版本键 `@deepseek-ai/dsh-client-ui-primitives@0.1.7-rc.2` 与 `@deepseek-ai/dsh-client-store@0.1.7-rc.2` → `@0.2.0-rc.2`，**并且** primitives 键内三处嵌套 `@deepseek-ai/*` 值（`@deepseek-ai/dsh-client-store`、`@deepseek-ai/dsh-util-code-language`、`@deepseek-ai/dsh-util-workspace-path`）同步为 `0.2.0-rc.2`——这三处是 primitives devDependencies 的镜像，留旧线会导入旧线副本、破坏版本唯一（SC-001/FR-010）；键集合与其余依赖集合不变（contracts/declaration-surface.md §2）。同时更新同文件内两处说明注释中的 `0.1.7-rc.2` 版本串（primitives 运行时导入说明注释、`@deepseek-ai/*` 随 catalog 同线的说明注释）为 `0.2.0-rc.2`（SC-006）
- [X] T002 执行 `bazel run @pnpm -- --dir /mnt/code/dominion up` 刷新 `pnpm-lock.yaml`（不得手改 lockfile）并按 `specs/073-dsh-0.2-upgrade/quickstart.md` 场景 1 验证：全部 `@deepseek-ai/*` 解析 `0.2.0-rc.2`、`0.1.7-rc.2` 零残留（含 `pnpm-workspace.yaml` 的 packageExtensions 嵌套值）、`@deepseek-ai/cordis` 全仓库唯一物理副本、`node-addon-require-builtin@0.1.6` 的 `linux-x64-gnu` optionalDep 在场
- [X] T003 在 `third_party/dsh/core`、`projects/game/agent_v2`、`experimental/dsh/demo`、`common/js/dsh-plugins`、`projects/game/web/frontend` 目录分别执行 `bazel run //:gazelle <dir>` 刷新 `BUILD.bazel` 的 node_modules link targets；执行 `bazel mod tidy`

**Checkpoint**: 声明面同线、install 成功；源码/组合清单未动（预期零适配，后续 phase 验证）。

---

## Phase 2: User Story 1 — 框架核心底座升级（Priority: P1）🎯 MVP

**Goal**: 底座声明面与快照标识同步 0.2.0-rc.2 线，断言①（plugin-free）通过。

**Independent Test**: `bazel build //third_party/dsh/core:runtime_pkg` 通过；`bazel test //experimental/dsh/demo/testplan:closure_audit_test` 断言①通过；lockfile 同线（Phase 1 产出）。

**文档清单（必读）**:

- **代码规范文档**: `style/javascript.md`（§模块系统 (ESM) §源码书写规则——`version.ts` 编辑的相对导入 `.js` 扩展名等书写规则；`esm-package-conventions.md` 为该节的包级契约正文〔技术参考桶已列〕）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style——`style/javascript.md` §引用 的仓库基准）
- **官方文档**: 无
- **技术文章/技术参考文档**: `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则——`version.ts` 属 Node ESM 包源码）；`specs/073-dsh-0.2-upgrade/data-model.md`（§1.2 框架核心底座三面互一致与 11 包口径——T004 校验规则）；`specs/073-dsh-0.2-upgrade/research.md`（D1 互锁矩阵——注释事实的依据）；`specs/073-dsh-0.2-upgrade/contracts/declaration-surface.md`（§3 框架核心底座、§4 刷新验证）；`specs/070-dsh-upgrade/tasks.md`（T012——`runtime_pkg` 闭包注释先例）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§5 closure audit 契约——T004 审计断言②③口径）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 2）

- [X] T004 [US1] 更新 `third_party/dsh/core/version.ts`：`DSH_CORE_SNAPSHOT = 'dsh-core@0.2.0-rc.2'`；头注释按 0.2.0-rc.2 线事实更新——dsh 家族 pin `0.2.0-rc.2`，cordis 家族互锁值（cordis 4.0.4、loader 1.0.5、include 1.0.9、group 1.0.4、timer 1.1.6）与 `node-addon-require-builtin` 0.1.6 与 0.1.7 线相同（互锁事实引用 `specs/073-dsh-0.2-upgrade/research.md` D1，替换现注释对 specs/070 的引用）。同时更新 `third_party/dsh/core/BUILD.bazel` 中 `runtime_pkg` 上方闭包注释：11 包闭包说明改为 `0.2.0-rc.2` 线事实并改引 `specs/073-dsh-0.2-upgrade/research.md` D1（该注释不随 gazelle 变化，先例 `specs/070-dsh-upgrade/tasks.md` T012；SC-006 覆盖 BUILD.bazel 声明与注释）。门禁：`bazel build //third_party/dsh/core:runtime_pkg`（含 `:version_lib`）通过；`bazel test //experimental/dsh/demo/testplan:closure_audit_test` 断言① plugin-free 通过（断言②③此时应随声明面自动成立，若失败先核对 demo 声明面再继续；**该 target 的 ②③ 在此仅作早期探测信号，正式验收为 Phase 5 的 T013**）

**Checkpoint**: 底座三面（package.json 经 catalog、BUILD.bazel 经 gazelle、version.ts）同线，审计①绿。

---

## Phase 3: User Story 2 — agent_v2 官方插件同线且行为不回归（Priority: P1）

**Goal**: agent_v2 在 0.2.0-rc.2 线构建/单测全绿；工具调度失败恢复行为（0.2.0 新增）有测试覆盖。

**Independent Test**: `bazel test //projects/game/agent_v2/... --nocache_test_results` 全绿。

**文档清单（必读）**:

- **代码规范文档**: `style/javascript.md`（§测试 (Testing)——vitest 执行模型与 mock 约定；§模块系统 (ESM) ——§源码书写规则（若涉源码编辑）、§CJS 第三方依赖导入约定、§OTel 插桩与 loader hook 三面均属本 phase 阅读面，其直接引用的仓库内文档见技术参考桶；`esm-package-conventions.md` 为该节的包级契约正文〔技术参考桶已列〕）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style）
- **官方文档**: https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls （mock 模块陷阱——`style/javascript.md` §Mock 约定的直接引用）；https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.0-rc.2/.agents/notes/implemented/bug-fix/2026-09-19-failed-step-tool-results.md （上游仓库官方注记：失败恢复行为语义——断言重写与新用例的权威依据：TOOL_OUTCOME_UNKNOWN/TOOL_NOT_STARTED 两级保守结果、已提交结果保留、按 assistant 调用顺序）
- **技术文章/技术参考文档**: `specs/019-js-test-reliability/spec.md`（mock 脆弱性的需求面：US2 mock 拦截失效、FR-010 mock 生效断言——`style/javascript.md` §测试 的间接引用；执行模型与根因分析正文见 `specs/019-js-test-reliability/research.md`）；`specs/019-js-test-reliability/research.md`（§1 测试执行模型、§2 mock 根因、§6 模块单例——`style/javascript.md` §测试 指向的目录内背景正文）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则——相对导入 `.js` 扩展名等；§5 测试 data 只含 raw `.ts` 源；`style/javascript.md` 的间接引用）；`specs/048-js-esm-migration/research.md`（R5 CJS 具名导入白名单——`style/javascript.md` §CJS 第三方依赖导入约定 的直接引用）；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（§2 bootstrap 两段式时序、§3 测试路径契约 `createRequire` 唯一豁免——`style/javascript.md` §OTel 插桩与 loader hook 与 §规则：`createRequire()` 的直接引用）；`specs/073-dsh-0.2-upgrade/data-model.md`（§3 冻结面：组合清单行集合——T005 组合清单断言的校验规则）；`specs/073-dsh-0.2-upgrade/research.md`（D2 逐字节 diff 结论、D6 行为变化与断言影响面）；`specs/070-dsh-upgrade/research.md`（D8——断言按新事实重写且语义守恒的先例）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 3）

- [X] T005 [US2] 运行 `bazel test //projects/game/agent_v2/... --nocache_test_results`：预期零适配全绿（组合清单 21 行断言、preset fail-loud、team 流程）；若失败路径事件序列断言失配（`dsh-agent-loop` 0.2.0 在失败 step 关闭前补记保守恢复 `tool/result` 事件——research.md D6），按新事实重写断言（语义守恒：断言事件序列含恢复结果而非删除断言；不得删除/跳过用例）；非 D6 原因的失败按 spec Edge Cases 回退条款评估并上报（暂停后续 phase）
- [X] T006 [US2] 确认工具调度失败恢复行为有仓库级测试覆盖（FR-007）：核对 T005 后既有失败路径用例是否已断言恢复结果与"失败后会话可继续"；**优先**在既有失败路径用例（`projects/game/agent_v2/src/history.test.ts`、`projects/game/agent_v2/src/session.test.ts`）中增补；若语义无自然归属则新增 `projects/game/agent_v2/src/tool-recovery.test.ts`（新增文件被 `lib_test` 的 `data = glob(["src/**/*.ts"])` 自动收集；因测试直接导入 `@deepseek-ai/dsh-session`（原不在 lib_test runfiles），`projects/game/agent_v2/BUILD.bazel` 的 `lib_test` data 增补该包链接，理由注释见该文件）。用例内容：模拟 step 内工具调度失败 → 断言事件流包含 `TOOL_OUTCOME_UNKNOWN`/`TOOL_NOT_STARTED` 恢复结果、已提交结果保留、会话可继续。门禁：相关 target 全绿

**Checkpoint**: agent_v2 单测全绿（含失败恢复覆盖），组合清单行集合与升级前相同。

---

## Phase 4: User Story 3 — webUI 组件与设计对齐 0.2.0 并采用流式 shimmer（Priority: P1）

**Goal**: ui-primitives 同线（Phase 1 已切值）+ vendored theme 重取（FR-022 闭合）+ ReasoningRow shimmer 采用。

**Independent Test**: `bazel build //projects/game/web/frontend/...`（含 `:dist`）+ `bazel test //projects/game/web/frontend/...` 全绿 + 包内 `tsc --noEmit` 零错误。

**文档清单（必读）**:

- **代码规范文档**: `style/javascript.md`（§测试 (Testing) §js_test 执行模型与 §Mock 约定；§模块系统 (ESM)——`.tsx` 编辑：**前端包为 bundler 体系，`specs/048-js-esm-migration/contracts/esm-package-conventions.md` 的 Node ESM 审计范围不含 `*/frontend` 包，其 `.js` 扩展名导入等规则以前端既有书写为准**〔070 Phase 7 先例〕；故该节 §CJS 白名单与 §OTel 小节不适用本 phase）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style）
- **官方文档**: https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls （mock 模块陷阱——`style/javascript.md` §Mock 约定的直接引用）；`@deepseek-ai/dsh-client-ui-*` 无独立官方文档站点（本 phase 所需权威面为第三方依赖官方发布产物——类型面与实现载体——列于技术参考桶）
- **技术文章/技术参考文档**: https://unpkg.com/@deepseek-ai/dsh-client-ui-primitives@0.2.0-rc.2/lib/types/index.d.ts （第三方依赖官方发布产物：`TextShimmer`/`DisclosureRow` 0.2.0 组件类型面——T009/T010 权威类型契约，`contracts/shimmer-adoption.md` 为其转录）；https://unpkg.com/@deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2/lib/client.js （第三方依赖官方发布产物：vendored theme 的 sheet 实际载体；tarball 主源 `https://registry.npmjs.org/@deepseek-ai/dsh-client-ui-theme/-/dsh-client-ui-theme-0.2.0-rc.2.tgz`，research.md D2 Sources——T007 逐文件比对基准）；`specs/019-js-test-reliability/spec.md`（mock 脆弱性的需求面——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/research.md`（§1 测试执行模型、§2 mock 根因、§6 模块单例——`style/javascript.md` §测试 指向的目录内背景正文）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§3 源码书写规则、§5 测试——vitest data 只含 raw `.ts` 源；`style/javascript.md` 的间接引用）；`specs/054-agent-v2-bugfixes/spec.md`（FR-022 消费覆盖断言的定义——T007 验收门禁口径）；`projects/game/web/frontend/src/dsh-theme/README.md`（「获取源实态」与「人工升级步骤」——T007 重取流程的原始依据，`contracts/theme-vendoring.md` §1 为其契约化）；`specs/073-dsh-0.2-upgrade/contracts/theme-vendoring.md`（§1 重取流程、§2 新 token 清单、§3 FR-022 断言语义——T007 全部依据）；`specs/073-dsh-0.2-upgrade/contracts/shimmer-adoption.md`（§1 采用点、§2 状态契约、§3 明确不采用、§4 测试断言——T009/T010 全部依据）；`specs/073-dsh-0.2-upgrade/data-model.md`（§1.3 vendored token sheets、§1.4 shimmer 状态实体——T007/T009/T010 的校验规则与状态契约口径）；`specs/073-dsh-0.2-upgrade/research.md`（D4 theme diff、D5 shimmer 设计、D7 defer 清单）；`specs/070-dsh-upgrade/research.md`（D9 终态结论——tsc 手动核验门禁先例）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 4）

- [X] T007 [US3] vendored theme 重取：按 `specs/073-dsh-0.2-upgrade/contracts/theme-vendoring.md` §1，从 `@deepseek-ai/dsh-client-ui-theme@0.2.0-rc.2` tarball 的 `lib/client.js` region 锚点提取全部 8 张 sheet（JSON 反转义后按字节落盘），覆盖 `projects/game/web/frontend/src/dsh-theme/` 同名 css——预期仅 `design-platform.css` 变化（+8 token，明暗双份），其余 7 张逐字节不变（出现其他 sheet 变化即停下核对 tarball 版本）；`src/dsh-theme/README.md` 随线更新——「来源与版本」的 vendored 版本记录与「获取源实态」中的 `0.1.7-rc.2` STYLES 顺序表述均改为 `0.2.0-rc.2`（顺序不变，`index.css` 零改动；SC-006 要求该 README 全文零旧线残留）。门禁：`bazel test //projects/game/web/frontend:lib_test`——FR-022 断言通过（primitives 0.2.0 新消费的 `--dsw-alias-label-shimmer`/`--dsw-alias-menu-group-header-fill`/`--dsw-alias-switch-thumb` 由重取后的 `design-platform.css` 闭合；豁免集合 3 项不变、不得扩大）
- [X] T008 [US3] 前端资产对齐验证**与 FR-008 类型面适配**（本任务承载 FR-008 的适配职责，非纯验证任务——宪法 IV 的门禁即本任务内置）：`bazel build //projects/game/web/frontend/...`（含 `:dist` Vite 生产构建）+ `bazel test //projects/game/web/frontend/...` + 包内类型核验 `cd projects/game/web/frontend && ./node_modules/.bin/tsc --noEmit` 零错误（bazel 门禁不含 tsc）。预期组件源码零适配（FR-008：Icon 集合逐字节相同、props 变化全为可选增量）；若出现类型失配，按 0.2.0 类型面最小适配
- [X] T009 [US3] ReasoningRow shimmer 采用（`projects/game/web/frontend/src/components/ReasoningRow.tsx`，按 `specs/073-dsh-0.2-upgrade/contracts/shimmer-adoption.md` §1）：折叠摘要文本 `.reasoning-summary-text` 内容以 `<TextShimmer active={running}>` 包裹（`TextShimmer` 自 `@deepseek-ai/dsh-client-ui-primitives` 导入；不包裹 `.reasoning-separator` 与图标——组件契约 "Keep icons outside"；`data-follow-end` 跟尾机制保留）+ `DisclosureRow` 传 `running={running}`（0.2.0 头部动画）；既有 `{running && <span className="visually-hidden">正在生成</span>}` 保留（进行中语义不得仅由动画承载）
- [X] T010 [US3] 增补 `projects/game/web/frontend/src/components/ReasoningRow.test.tsx` shimmer 断言（`specs/073-dsh-0.2-upgrade/contracts/shimmer-adoption.md` §4）：running 态 TextShimmer active 语义与 visually-hidden「正在生成」在场；ok 态（完结/历史）文字完整呈现且状态切换不重挂载文本节点；既有用例（latestLine/firstLine 摘要选择、MarkdownText streaming、空文本不渲染）全部保持；running 态 DisclosureRow 以 TextShimmer 装饰层（inert）重渲染折叠内容致既有 `getByTestId('reasoning-summary')` 定位命中两份，`ChatView.test.tsx` 连带做排除装饰层的最小定位适配（语义不变）——jsdom 环境断言 DOM 结构/属性语义，不断言动画帧。门禁：`bazel test //projects/game/web/frontend/...` 全绿

**Checkpoint**: 前端构建/组件测试/类型面三绿，FR-022 闭合，shimmer 采用完成（US3 场景 1/2/3/5 满足；场景 4 defer 清单已由 research.md D7 落档）。

---

## Phase 5: User Story 4 — 共享生态同线迁移与闭包审计（Priority: P2）

**Goal**: demo 与全部 workspace 插件在 0.2.0-rc.2 线全绿，closure audit 三断言通过。

**Independent Test**: `bazel test //experimental/dsh/demo/... //common/js/dsh-plugins/... --nocache_test_results` 全绿；closure audit ①②③ 通过。

**文档清单（必读）**:

- **代码规范文档**: `style/javascript.md`（§测试 (Testing)——vitest 执行模型与 mock 约定；§模块系统 (ESM) §源码书写规则——插件源码编辑时的书写规则）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style）
- **官方文档**: https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls （mock 模块陷阱——`style/javascript.md` §Mock 约定的直接引用）
- **技术文章/技术参考文档**: `specs/019-js-test-reliability/spec.md`（mock 脆弱性的需求面——`style/javascript.md` §测试 的间接引用）；`specs/019-js-test-reliability/research.md`（§1 测试执行模型、§2 mock 根因、§6 模块单例——`style/javascript.md` §测试 指向的目录内背景正文）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（vitest shim 退出码契约——`style/javascript.md` §声明测试 target 的间接引用）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（§5 测试 data 约定；`style/javascript.md` 的间接引用）；`specs/048-js-esm-migration/research.md`（R5 CJS 具名导入白名单——`style/javascript.md` §CJS 第三方依赖导入约定 的直接引用）；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（§3 测试文件 `createRequire` 唯一豁免——`style/javascript.md` §规则：`createRequire()` 的直接引用）；`specs/073-dsh-0.2-upgrade/data-model.md`（§1.1 dsh 版本线与 cordis 唯一副本、§3 冻结面——demo/插件回归的校验规则口径）；`specs/073-dsh-0.2-upgrade/research.md`（D2 逐字节 diff 结论、D3 closure audit 数据驱动推导、D6 行为变化与断言影响面）；`specs/070-dsh-upgrade/research.md`（D12 closure audit 机制——T013 expected set 由声明面自动重算的依据）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§5 audit 契约——断言②③口径）；`specs/073-dsh-0.2-upgrade/contracts/declaration-surface.md`（§3 断言①口径、§4 刷新验证）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 2/3）

- [X] T011 [P] [US4] demo 回归：`bazel test //experimental/dsh/demo/... --nocache_test_results`（组合清单断言、preset demo）——预期零适配全绿；失败路径断言失配按 research.md D6 重写（语义守恒，同 T005 规则）
- [X] T012 [P] [US4] workspace 插件回归：`bazel test //common/js/dsh-plugins/... --nocache_test_results`（llm-glm、llm-opencode-go、team、memory-service、saolei、saolei-loop、preset-authoring 等）——预期零适配（research.md D2：消费符号面 0.1.7 → 0.2.0 零变化）；失败按（a）D6 行为断言重写（b）非 D6 失败走 spec Edge Cases 回退条款上报
- [X] T013 [US4] closure audit 终验：`bazel test //experimental/dsh/demo/testplan:closure_audit_test` 三断言全过（① 底座零插件、② 每个随行 dsh 包声明可追溯、③ 版本唯一——expected set 由 package.json 声明自动推导，`specs/070-dsh-upgrade/research.md` D12 机制零改动）

**Checkpoint**: 生态全绿 + 审计三断言通过（US4 场景 1/2/3 满足）。

---

## Phase 6: Polish & 验收（大型测试 + 残留清查）

**Purpose**: 服务型应用完整验收（宪法 VI）与终态确认（SC-006、FR-011）。

**文档清单（必读）**:

- **代码规范文档**: `style/large_test.md`（全部——大型测试执行规范，testplan 编写/执行约束）；`style/golang.md`（§单元测试——大型测试用例修复时遵守，`style/large_test.md` §测试用例 明文要求）与 https://google.github.io/styleguide/go/guide 、https://google.github.io/styleguide/go/decisions 、https://google.github.io/styleguide/go/best-practices （Google Go Style Guide 三份——`style/golang.md` §引用 声明的外部基准）；`style/javascript.md`（§模块系统 (ESM)——**仅注释/文档中的版本串与 ESM 书写表述背景**；该节 §CJS/§OTel/§测试 小节不涉本 phase 的注释与文档改动，不属本 phase 阅读面）；https://google.github.io/styleguide/tsguide.html （Google TypeScript Style）
- **官方文档**: 无
- **技术文章/技术参考文档**: `specs/073-dsh-0.2-upgrade/data-model.md`（§1.1 版本线校验规则与三处版本记录——T018 终态确认口径）；`specs/073-dsh-0.2-upgrade/quickstart.md`（场景 5 大型测试、场景 6 残留清查）；`specs/073-dsh-0.2-upgrade/research.md`（D6 行为变化与断言影响面——失败修复时的判据）

- [ ] T014 大型测试验收（宪法 VI，经 testplan skill 实际执行）：`projects/game/testplan/system_test.yaml`（agent_v2 四拓扑全回归：game-system 对话/preset/team/memory + game-disconnect + game-memory-down + game-stall）与 `experimental/dsh/demo/testplan/interface_test.yaml`（demo 接口面），完成部署→测试→清理闭环，**全部用例通过**（任何 failed/flaky 即验收未过，修复后重跑至全绿；禁止仅 build 检查替代）
- [X] T015 [P] agent_v2/demo 版本引用清理（SC-006）：`projects/game/agent_v2/README.md`、`projects/game/agent_v2/src/system-prompt.ts`（3 处 `unpkg.com/@deepseek-ai/...@0.1.7-rc.2` 版本化 URL）、`projects/game/agent_v2/src/dsh.test.ts`（preset-registry 版本注释及 0.1.7 peers/persona Config 版本线表述）、`projects/game/agent_v2/src/{history.ts,history.test.ts,session.test.ts}`（0.1.7 历史行为叙述按原则 VII 改写为终态事实）、`projects/game/agent_v2/BUILD.bazel`（约 line 118 的 "The 0.1.7 line's install-level peers" 版本线表述）、`experimental/dsh/demo/README.md`、`experimental/dsh/demo/agent/BUILD.bazel`（约 line 96 的 "the 0.1.7 install-level peers"）、`experimental/dsh/demo/agent/src/composition.test.ts`（约 line 183/376 的 "0.1.7 line"/"0.1.7 model"）的 `0.1.7` 版本串/版本线表述清除：能对应到 0.2.0 线的改为 `0.2.0-rc.2`，其余改写为不绑定版本线的终态表述；其中的依据引用按事实归属改指：版本线事实引 `specs/073-dsh-0.2-upgrade/research.md`，install-level 分类引 `specs/070-dsh-upgrade/contracts/composition-manifest.md` §4，类型面/事件面设计事实引 `specs/070-dsh-upgrade/research.md` D7 终态结论（引用路径不含 `0.1.7` 字面，不违 SC-006）。门禁：`bazel test //projects/game/agent_v2/... //experimental/dsh/demo/... --nocache_test_results` 全绿（仅注释/文档变化，用例零改动）
- [X] T016 [P] dsh-plugins 面版本引用清理（SC-006）：`common/js/dsh-plugins/{llm-glm,llm-opencode-go,saolei,saolei-loop,desktop-bridge}/README.md`（含 desktop-bridge 的 "0.1.7 线内互锁" 版本线表述）、`common/js/dsh-plugins/team/src/broadcast.ts`（2 处 `unpkg...dsh-llm@0.1.7-rc.2` URL）、`common/js/dsh-plugins/team/src/team.ts`、`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`、`common/js/dsh-plugins/saolei-loop/src/announcer.ts`（"the 0.1.7 envelope"）、`common/js/dsh-plugins/memory-service/src/service.ts`、`common/js/dsh-plugins/{llm-glm,llm-opencode-go}/src/{serialize.ts,serialize.test.ts}`（"0.1.7 loop-built request"/"0.1.7 model"/"0.1.7 tool-role message"）的 `0.1.7` 版本串与版本线表述清除：能对应到 0.2.0 线的改为 `0.2.0-rc.2`，其余改写为不绑定版本线的终态表述。门禁：`bazel test //common/js/dsh-plugins/... --nocache_test_results` 全绿
- [X] T017 [P] web 面 README 版本引用清理（SC-006）：`projects/game/web/README.md`、`projects/game/web/frontend/README.md`（含 attribution 的 `dsh-client-ui-primitives@0.1.7-rc.2` URL 与「0.1.7-rc.2 线」表述）改为 `0.2.0-rc.2`，与 catalog 的 ui-primitives 条目同线。门禁：`bazel build //projects/game/web/frontend/... && bazel test //projects/game/web/frontend/...` 全绿
- [ ] T018 残留清查与终态确认（SC-006/FR-011）：按 `specs/073-dsh-0.2-upgrade/quickstart.md` 场景 6 全仓扫描 `0.1.7` 版本串（命令含 `--include="*.json" --include="*.yaml" --include="*.ts" --include="*.tsx" --include="*.bazel" --include="*.bzl" --include="*.md" --include="*.go"`，扫描根文件（AGENTS.md/README.md/package.json/pnpm-workspace.yaml）与 `third_party/ projects/ experimental/ common/ tools/ survey/`，覆盖 catalog、`packageExtensions` 键与嵌套值、package.json、`pnpm-workspace.yaml` 说明注释、`BUILD.bazel` 声明与注释、`version.ts`、全部 README、源码与测试注释；豁免 `specs/` 下历史文档与本特性交付文档的版本线对比表述）预期零输出；确认 `pnpm-workspace.yaml`、`third_party/dsh/core/version.ts`、`projects/game/web/frontend/src/dsh-theme/README.md` 三处版本记录一致为 `0.2.0-rc.2`；复核 research/plan 中版本事实与行为变化依据的来源记录完整（FR-011：registry 实测、tarball diff 结论、上游修复注记）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，立即开始；T001→T002→T003 串行（同一声明面流程链）
- **US1 (Phase 2)**: 依赖 Phase 1（catalog 切值 + gazelle）
- **US2 (Phase 3)**: 依赖 Phase 1；与 Phase 2/4/5 无文件冲突，可并行
- **US3 (Phase 4)**: 依赖 Phase 1（primitives 已 0.2.0）；T007→T008→T009→T010 串行（T009 依赖 T007——FR-022 在 theme 未重取时必失败；T008 为资产对齐独立验证点）
- **US4 (Phase 5)**: 依赖 Phase 1；T011/T012 并行（不同目录），T013 在其后
- **Polish (Phase 6)**: 依赖 Phase 2–5 全部完成；T015/T016/T017 互不冲突可并行，T018 在其后

### User Story Dependencies

- **US1 (P1)**: 仅依赖 Phase 1——无跨 story 依赖
- **US2 (P1)**: 仅依赖 Phase 1——独立可测（agent_v2 单测面）
- **US3 (P1)**: 仅依赖 Phase 1——独立可测（前端构建/测试面）
- **US4 (P2)**: 仅依赖 Phase 1——独立可测（demo/plugins/审计面）；T005 与 T011/T012 的 D6 断言重写规则同源，但文件不同、可并行

### Parallel Opportunities

- Phase 1 完成后：US1（T004）、US2（T005–T006）、US4 的 T011/T012 可四路并行（不同目录）
- T011 ∥ T012（demo ∥ dsh-plugins，不同目录）
- US3 内部 T008（验证）与 T009（shimmer 源码）理论不同文件，但 T008 是 T009 前的独立验证锚点，建议按序执行

---

## Parallel Example: Phase 1 完成后

```bash
# 四路并行（不同目录、无未完成依赖）：
Task: "T004 [US1] 更新 third_party/dsh/core/version.ts + 底座门禁"
Task: "T005 [US2] agent_v2 全量单测（零适配验证/断言重写）"
Task: "T011 [US4] demo 回归单测"
Task: "T012 [US4] workspace 插件回归单测"
```

---

## Implementation Strategy

### MVP First（Setup + US1）

1. 完成 Phase 1（声明面切换 + lockfile 验证）
2. 完成 Phase 2（底座快照标识 + runtime_pkg + 断言①）
3. **STOP and VALIDATE**: quickstart 场景 1/2 通过——版本线单元已落定，其余消费者可随时跟随

### Incremental Delivery

1. Setup + US1 → 版本线地基（MVP）
2. US2 → agent_v2 回归绿（服务面不回归证明）
3. US3 → 前端资产对齐 + shimmer（用户点名的 webUI 诉求闭环）
4. US4 → 生态与审计全绿
5. Phase 6 → 大型测试验收 + 终态确认（特性完成）

### 中断恢复

每 phase 有独立门禁（quickstart 场景映射）；中断后按「Phase 1 → 未完成的最早 phase」顺序恢复，已绿 phase 以 quickstart 场景复核即可。

---

## Notes

- [P] 任务 = 不同文件/目录、无未完成依赖
- **回退条款**：任一 phase 出现非 D6 原因且无法适配的破坏（research.md D2 结论被实测推翻），按 spec Edge Cases 整体回退 catalog 至 `0.1.7-rc.2` 并 `pnpm up`，差异记录于交付文档后终止特性
- D6 断言重写规则（T005/T011/T012 共用）：重写≠放松——按新事实断言事件序列（含补记的保守恢复结果），不得删除用例、不得跳过、不得标记 flaky 豁免（SC-002）
- FR-022 是 theme 重取的验收门禁而非自动同步（T007）；豁免集合冻结 3 项；`FR-022` 定义于 `specs/054-agent-v2-bugfixes/spec.md`
- SC-006 残留面包含 `packageExtensions` 嵌套 `@deepseek-ai/*` 值（T001 与 `pnpm-workspace.yaml` 说明注释）、`third_party/dsh/core/BUILD.bazel` 闭包注释（T004）、`agent_v2`/`demo` 的 `BUILD.bazel` 版本线注释与 README/源码/测试注释版本串（T015/T016/T017）
- 大型测试禁止 build-only 替代（T014，宪法 VI）
