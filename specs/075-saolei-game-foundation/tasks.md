# Tasks: saolei 游戏基础与智能体工具解耦（dsh-saolei / dsh-saolei-tools）

**Input**: Design documents from `/specs/075-saolei-game-foundation/`

**Prerequisites**: plan.md、spec.md（用户故事 US1–US4、M1–M14 移动清单）、research.md（D1–D10）、data-model.md、contracts/（saolei-foundation / saolei-tools-plugin / team-loop-assembly / cutover-contracts）、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task，任务完成即要求 `bazel build` / `bazel test` 相关 target 通过）；大型测试（四 suite + 新增显式断言）在原子切换 phase 单独分配验收任务。mock 约定按 `style/javascript.md`：DI / `vi.fn` test-double（禁止新增 `vi.mock` 模块拦截）。

**Organization**: 迁移分步（research.md D5：P1 基础双面期 → P2 工具包 → P3 原子切换 → P4 终态文档）承载 spec 用户故事：Phase 3 = US1（dsh-saolei 双面期改造，旧世界不动）、Phase 4 = US2（新工具包，无消费方）、**Phase 5 = 原子切换单元**（US3 编排消费与所有权切换 + US4 组合切换——T009–T016 必须同批合入：dsh-saolei 移除 saoleiTools 面 ↔ saolei-loop 必须已改读 tools 包，saolei-loop 删 `src/game/` ↔ 必须已消费基础面，config.plugins 换单 ↔ 双 provide 冲突；该 phase 不允许部分合入）、Phase 6 = US4 终态（纯文档与静态复查，不重跑四 suite）。**Phase 3 先于 Phase 4 为类型依赖所决定**：工具包的 `OperateInput`/`SaoleiGame`/`ToolOutcome` 从 `@dominion/dsh-saolei` 导入，该导出面在 Phase 3 产出。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US4）；Setup/Foundational phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；JS 插件 `common/js/dsh-plugins/`，agent_v2 `projects/game/agent_v2/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更。新包目录 `common/js/dsh-plugins/saolei-tools/` 在 `pnpm-workspace.yaml` 既有 glob（`common/js/**`）覆盖内；全部第三方依赖已在 catalog（`@deepseek-ai/cordis`、`@deepseek-ai/dsh-tools`、`@deepseek-ai/dsh-system-prompt` 等），无新依赖引入、无 catalog 变更。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（各实现 phase 自带文档清单）

---

## Phase 2: Foundational

**无任务**：本特性无跨 US 的共享阻塞件（无 proto 变更、无 schema 迁移）。Phase 3（US1 基础双面期）即结构基座——游戏类型单一出口与基础服务面在其内产出，Phase 4/5 消费。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（本 phase 无任务；各实现 phase 自带文档清单）

---

## Phase 3: User Story 1 - dsh-saolei 游戏基础（双面期）(Priority: P1) 🎯 结构基座

**Goal**: dsh-saolei 增量改造为游戏基础：game/ 等价迁入（含形态改造——Service 移除/纯领域工厂）、游戏类型单一出口、基础服务面（`ctx.provide("saolei")`）、玩法 section——**保留旧 saoleiTools provide（双面兼容）**；saolei-loop/cordis.yml 零改动，全仓可构建，组合行为零变化。

**Independent Test**: dsh-saolei 全部单测通过（game 迁移等价用例行为断言零改动 + 基础面断言 + 既有 saoleiTools 断言保留）；`bazel build //...` 与全仓 `bazel test //...` 通过（旧世界 saolei-loop 未动）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（ESM 书写规则——相对导入带 `.js` 扩展名、`export type` 显式、CJS 依赖导入约定；`vitest_test` 宏 data 规则；DI mock 约定；含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（包契约全文——声明/tsconfig 与 .swcrc 锁步/源码规则/测试 data 规则；其引用的 `specs/048-js-esm-migration/research.md` §R5 一并阅读——CJS 具名导入逐包核实表，用于核定迁移后导入方式；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` 不涉及——本特性不改 OTel 装配）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（shim 退出码契约、data 双实例不变量）；`specs/075-saolei-game-foundation/contracts/saolei-foundation.md`（全文——面定义/导出面/依赖声明/禁止事项/测试义务）；`specs/075-saolei-game-foundation/research.md`（D1 面形态、D4 包内布局与导出面、D10 所有权与形态改造论证）；`specs/075-saolei-game-foundation/data-model.md`（§1–§2 实体归属与面）；**迁移源与行为契约权威**：`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts`（440 行——迁移比对基准）、`common/js/dsh-plugins/saolei-loop/src/game/{board,geometry,text}.ts`、`common/js/dsh-plugins/saolei-loop/src/game/{runtime.test.ts,text.test.ts}`（用例零改动基准）；`specs/051-agent-v2-dsh-migration/data-model.md` §2.5（游戏运行时行为契约权威）；`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.1/§2.2（现状注册形态与 GameRuntime 行为——改造比对）；`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`（§1 ToolOutcome/concludesTurn 契约——迁移语义不变项）；`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1（gameSection 名/order 50/文本与内容边界基线——T004/T005 逐字核对；间接引用显式展开）；`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` §1（remain 结果体现行权威——051 §2.5、051 saolei-plugins §2.2、060 §1 均显式让位于本修订；T002/T003 迁移比对基准；间接引用显式展开）；`specs/065-agent-v2-team-refine/data-model.md` §3（gameStatsText 统计消息文本模板权威——T002/T003 迁移语义不变项）；`specs/059-agent-v2-team-mode/data-model.md` §3（sessionName = GAME 会话资源名桥接键语义——T002 工厂参数语义核对）；`@deepseek-ai/dsh-system-prompt` 分发 README（`common/js/dsh-plugins/saolei/node_modules/@deepseek-ai/dsh-system-prompt/README.md`——section/order 频带与 agent-scoped shadow 语义；T004/T005 的 `PromptSection` 类型与 section 注册语义核实）；`specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md` §1（顶级基础行先例参照——本 phase 不读全文仅参照行形态，见 `projects/game/agent_v2/cordis.yml:67` 既有行）

- [X] T001 [US1] `common/js/dsh-plugins/saolei/` 依赖增补：`package.json` dependencies 增 `"@dominion/game-saolei-board": "workspace:*"`、peerDependencies 增 `"@dominion/dsh-desktop-bridge": "workspace:*"`、**不新增 `"@deepseek-ai/dsh-agent"`**（迁移后 `runtime.ts` 无 `Agent` 引用——`createGameRuntime` 不再收 agent 参数；契约 `specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §4 已同步收缩）；`tsconfig.json` paths 增 `"@dominion/game-saolei-board": ["../../../projects/game/pkg/saolei-board/src/core/index.js"]` 与 `"@dominion/dsh-desktop-bridge": ["../desktop-bridge/src/index.js"]`；`BUILD.bazel`——`lib` deps 增 `:node_modules/@dominion/game-saolei-board`（board.ts/text.ts 运行时+类型）与 `:node_modules/@dominion/dsh-desktop-bridge`（runtime.ts 类型 import）、`runtime_pkg` runtime_deps 增 `//projects/game/pkg/saolei-board:runtime_pkg`、`lib_test` data 增 `:node_modules/@dominion/game-saolei-board` 与 `:node_modules/@dominion/dsh-desktop-bridge`；执行 `bazel run @pnpm -- --dir /mnt/code/dominion up` 更新 lockfile、`bazel run //:gazelle common/js/dsh-plugins/saolei`；`bazel build //common/js/dsh-plugins/saolei/...` 通过
- [X] T002 [US1] game/ 领域文件迁入 `common/js/dsh-plugins/saolei/src/game/`（依赖 T001 的依赖声明与 paths）：`runtime.ts`/`board.ts`/`geometry.ts`/`text.ts` 自 `common/js/dsh-plugins/saolei-loop/src/game/` 文件级复制（逻辑零改动），其中 `runtime.ts` 迁移内含形态改造（按 `specs/075-saolei-game-foundation/research.md` D10）：① `GameRuntimeService extends Service` 改为普通实现类 `GameRuntime implements GameRuntime`——构造器仅收 `private readonly deps: GameRuntimeDeps`（删除 `ctx`/`name` 参数与 `super(ctx, name)`），类头注释改述终态（编排持有实例、清理责任在编排，不再表述 scope 注册）；② `createAgentGameRuntime(agent, desktopBridge, sessionName)` 改造为 `createGameRuntime(sessionName: string, desktopBridge: DesktopBridgeService): GameRuntime`——删除 agent 参数与默认值、删除 `isolate` 注册、保留 dispatch 预绑定（`desktopBridge.dispatch(sessionName, part, signal)`）与 `createDefaultBoardApi()` 组装、注释改述纯领域工厂（desktop-bridge 以 GAME 会话资源名为键的既有说明随迁）；③ 文件内 import 路径适配（`./board.js` 等相对引用不变；`Service` import 删除）；其余行为零变化（`specs/051-agent-v2-dsh-migration/data-model.md` §2.5 契约逐条保持）
- [X] T003 [US1] game 测试迁入（依赖 T002 的类形态）：`runtime.test.ts`（1,097 行）与 `text.test.ts` 自 saolei-loop 文件级复制，`runtime.test.ts` 仅构造形态适配（`new GameRuntimeService(ctx, name, deps)` 的构造点改 `new GameRuntime(deps)`——builder/fixture 形态同步，行为断言零改动）；`bazel test //common/js/dsh-plugins/saolei/...` 全绿
- [X] T004 [US1] `common/js/dsh-plugins/saolei/src/index.ts` 双面期改造（依赖 T002）：保留既有 `apply` 的 `ctx.provide(SAOLEI_TOOLS_SERVICE, buildSaoleiToolsUnit())` 与全部工具/guidance 主体（**零改动**——Phase 5 才移除）；新增：`SAOLEI_FOUNDATION_SERVICE = "saolei"`、`SaoleiFoundation` 接口（`{ readonly createGameRuntime: typeof createGameRuntime; readonly gameSection: PromptSection }`，契约 `specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §2）、`SAOLEI_GAME_SECTION`（`{ name: "saolei:game", order: 50, text: SAOLEI_GAME_RULES }`——文本常量原位不动）、apply 内追加 `ctx.provide(SAOLEI_FOUNDATION_SERVICE, { createGameRuntime, gameSection: SAOLEI_GAME_SECTION })`、`declare module "@deepseek-ai/cordis"` 的 `Context.saolei?: SaoleiFoundation` 与 `Context.saoleiGame?: SaoleiGame` **两项**增强（**后者自 `common/js/dsh-plugins/saolei-loop/src/index.ts:183-191` 迁入**——M8 句柄解析类型面；`dsh-saolei-tools` 的 `exec.agent.ctx.saoleiGame` 解析依赖此声明；双面期 saolei-loop 的同名增强保留至 T010，两处 `Context.saoleiGame` 声明合并**仅在两包 `SaoleiGame` 接口结构完全一致时合法**——迁移必须保持该接口逐字段等价，每步以 `bazel build //...` 验证；若结构出现分歧，则把该增强迁出并入 Phase 5 原子批）、game 导出汇总（`export { createGameRuntime, gameStatsText } from "./game/runtime.js"` 等值导出 + `export type { SaoleiGame, GameRuntime, GameRuntimeDeps, GameEventRecord, GameLogEntry, GameStats, OperationType, OperateInput, CellOperation, ToolOutcome } from "./game/runtime.js"` 与 `gameStatsText` from `"./game/text.js"`——对照 `specs/075-saolei-game-foundation/data-model.md` §1 实体表逐项核对无遗漏）；文件头注释改述双面期终态（游戏基础 + 兼容期 saoleiTools，注明 Phase 5 移除）
- [X] T005 [US1] `common/js/dsh-plugins/saolei/src/index.test.ts` 双面期扩展（依赖 T004）：既有 saoleiTools 断言全部保留；新增基础面断言组——`ctx.provide("saolei")` 面存在且字段完整（`createGameRuntime` 为函数、`gameSection.name === "saolei:game"`、`gameSection.order === 50`、`gameSection.text === SAOLEI_GAME_RULES` 逐字）；`gameSection` 文本首行断言（`## 扫雷玩法与可用操作`）；无全局注册副作用断言扩展（stub ctx 断言 apply 未调用 `ctx.systemPrompt.section`/`ctx.tools.register`——基础面纯 provide）；`bazel test //common/js/dsh-plugins/saolei/... //...`（全仓）通过

**Checkpoint**: 游戏基础就绪（类型单一出口 + 基础面 + game 迁移等价）；旧世界零影响（saolei-loop/cordis.yml 未动，组合行为零变化）。

---

## Phase 4: User Story 2 - dsh-saolei-tools 工具插件（并行包） (Priority: P1)

**Goal**: 新建 `@dominion/dsh-saolei-tools`（`common/js/dsh-plugins/saolei-tools/`）：三工具 + 双形式校验 + saolei:guidance 自 dsh-saolei 等价迁入，类型从 `@dominion/dsh-saolei` 导入（Phase 3 已就绪）；以 saoleiTools 静态单元提供方形态就绪但无消费方（cordis.yml 不含该行）。

**Independent Test**: 新包单测全绿（单元提供面内容逐字断言 + 收窄断言 + 行为用例等价迁移）；全仓 `bazel build //...` 通过；组合行为零变化（无消费方）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；ESM/测试/DI 约定）
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新包契约全文；其引用的 `specs/048-js-esm-migration/research.md` §R5 一并阅读——CJS 具名导入逐包核实表，用于新包导入方式核定；`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md` 不涉及——本特性不改 OTel 装配）；`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（测试 shim 契约）；`specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md`（全文——单元定义收窄/工具资产冻结值/依赖/测试义务）；`specs/075-saolei-game-foundation/research.md`（D4 包布局、D6 测试映射）；**新包骨架先例（逐字段参照）**：`common/js/dsh-plugins/saolei-memory/package.json` + `tsconfig.json` + `.swcrc` + `BUILD.bazel`（074 新包先例——deps 走 catalog/workspace、vitest_test 宏 data 镜像 :lib node_modules）；**迁移源与冻结值权威**：`common/js/dsh-plugins/saolei/src/index.ts`（420 行——工具/校验/guidance 迁移比对基准）、`common/js/dsh-plugins/saolei/src/index.test.ts`（441 行——测试迁移源）；`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §3（三工具契约——schema/参数/exec 语义）；`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（§2 工具条目与拒绝文本、§3 玩法表述、§4 guidance 表述——冻结值权威）；`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §2（guidance 内容边界——迁入时逐字保持）

- [X] T006 [US2] 创建包 `common/js/dsh-plugins/saolei-tools/` 骨架：`package.json`（name `@dominion/dsh-saolei-tools`、`"type": "module"`、`exports["."]` types+default 双条件指向 `./src/index.js`；dependencies `"@dominion/dsh-saolei": "workspace:*"`；peerDependencies `@deepseek-ai/cordis`/`@deepseek-ai/dsh-tools`/`@deepseek-ai/dsh-system-prompt` 均 catalog；devDependencies `@types/node`/`typescript`/`vitest` catalog——形态逐字段对齐 `common/js/dsh-plugins/saolei-memory/package.json` 先例）；`tsconfig.json`（`module: nodenext`、`target: ES2020`、paths `"@dominion/dsh-saolei": ["../saolei/src/index.js"]`）与 `.swcrc`（`{"module":{"type":"es6","preserveImportMeta":true}}`、`jsc.target: es2020`）锁步；执行 `bazel run @pnpm -- --dir /mnt/code/dominion up`、`bazel run //:gazelle common/js/dsh-plugins/saolei-tools`；BUILD.bazel 增 `vitest_test` 宏（`data = glob(["src/**/*.ts"]) + :lib 的全部 :node_modules/* deps` 镜像，禁止手写裸 genrule）
- [X] T007 [US2] `common/js/dsh-plugins/saolei-tools/src/index.ts`（依赖 T006）：自 `common/js/dsh-plugins/saolei/src/index.ts` 等价迁入工具主体——`export const name = "saolei-tools"`、`inject: string[] = []`、`SAOLEI_TOOLS_SERVICE = "saoleiTools"`、`SaoleiToolsUnit` 接口（sections 类型不变）、`AMBIGUOUS_ARGS_TEXT`/`MISSING_ARGS_TEXT`/`INCOMPLETE_ARGS_TEXT` 字面量、`normalizeOperateArgs`、`resolveRuntime`（`exec.agent.ctx.saoleiGame` 解析与 fail-loud 文案逐字不变）、`executeOutcome`（concludesTurn/isError 语义逐字不变，`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md` §2）、`RESULT_OUTPUT_SCHEMA`/`resultOutput()`、三工具 `defineTool` 定义（schema/描述/文案逐字不变）、`SAOLEI_GUIDANCE`（文本逐字不变）、`buildSaoleiToolsUnit()` **sections 收窄为 `[{ name: "saolei:guidance", order: 100, text: SAOLEI_GUIDANCE }]`**（无 saolei:game——契约 `specs/075-saolei-game-foundation/contracts/saolei-tools-plugin.md` §2）、`apply` 内 `ctx.provide(SAOLEI_TOOLS_SERVICE, ...)`；类型 `OperateInput`/`SaoleiGame`/`ToolOutcome` 改 `import type from "@dominion/dsh-saolei"`；**不迁入** `SAOLEI_GAME_RULES`（留 dsh-saolei）；文件头注释改述终态（工具插件经游戏基础 API 操作游戏）
- [X] T008 [US2] `common/js/dsh-plugins/saolei-tools/src/index.test.ts`（依赖 T007）：自 `common/js/dsh-plugins/saolei/src/index.test.ts` 迁移改写——保留：三工具 schema/描述逐字断言、`saolei:guidance` 名/order 100/text 逐字断言、无全局注册副作用断言（stub ctx）、双形式校验三拒绝文本逐字断言、`exec.agent` 缺失与 scope 无 `saoleiGame` 的 fail-loud 文案断言、concludesTurn 转发与 isError 抛错断言（`specs/062-team-game-end-handoff/data-model.md` §1.2 工具层行的既有覆盖）；**新增**：单元 sections 仅含 saolei:guidance 断言（`unit.sections.map(s => s.name)` equals `["saolei:guidance"]`——收窄断言）；`SaoleiGame` fake 类型改从 `@dominion/dsh-saolei` 导入；`bazel build //... && bazel test //common/js/dsh-plugins/saolei-tools/...` 通过

**Checkpoint**: 工具插件就绪（无消费方，组合行为零变化）；Phase 5 的 config.plugins 换单对象已备。

---

## Phase 5: 原子切换（US3 编排消费与所有权 + US4 组合切换）⚠️ 单一合入单元

**Goal**: 一次合入完成：saolei-loop 编排切换（基础面消费 + 游戏实例编排持有 + planner 玩法注册 + seam 删除 + 删 `src/game/`）、dsh-saolei 净化（移除 saoleiTools 双面与上层依赖）、宿主清理、组合清单终态（22 行）、manifest 断言、testplan 显式断言；四 suite 大型测试全绿 = 特性行为面交付。

**Independent Test**: `bazel build //...` + 全量单测通过；`guitar run projects/game/testplan/system_test.yaml` 四 suite 全部用例通过 + 三个新增显式 prompt 断言通过（quickstart.md §1/§3）。

**⚠️ CRITICAL**: T009–T016 必须同批合入（research.md D5 原子性依据：dsh-saolei 移除 saoleiTools 面 ↔ 编排必须已改读 tools 包；saolei-loop 删 `src/game/` ↔ 必须已消费基础面；config.plugins 换单 ↔ 同列表双 provide 冲突）；T017 验收在全部任务完成后执行。中断恢复判据：全仓 build/test 全绿 + cordis.yml 处于旧态（无顶级 saolei 行、config.plugins 含 `@dominion/dsh-saolei`）或新态（无混合态）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；本 phase 编辑 TS 代码与测试）；`style/large_test.md`（T016/T017 测试断言与执行——:53 大型测试 Go 代码遵循 `style/golang.md`、:83 FOR Agent 指引、反模式条款）；`style/golang.md`（§单元测试 :218-244——T016 Go 断言书写规范：测试状态而非交互、禁用例塞断言、helper 断言单一概念）
- **官方文档**：无（testplan 经 skill 执行；cordis inject/provide 语义经类型定义核实——见下）
- **技术文章/技术参考文档**：`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md`（全文——inject/持有与句柄/注册序/移除面/行为差异表/不变量/测试义务）；`specs/075-saolei-game-foundation/contracts/cutover-contracts.md`（全文——原子批次/组合终态/断言落点/静态复查）；`specs/075-saolei-game-foundation/contracts/saolei-foundation.md`（§2 面定义——编排消费对象）；`specs/075-saolei-game-foundation/research.md`（D2 inject 裁定、D3 注册序、D10 所有权与生命周期等价论证、D8 显式断言与背景风险）；`specs/075-saolei-game-foundation/data-model.md`（§4 注册矩阵、§6 状态迁移图）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`（§2/§4/§6——config.plugins 装配与 fail-loud 语义延续基线）；`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md`（§4 组装顺序——注册序依据）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（§2 编排行为契约——零变化边界）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 行集合——T014 锁步修订对象）；**cordis inject 语义核实**：`common/js/dsh-plugins/saolei-loop/node_modules/@deepseek-ai/cordis/lib/types/registry.d.ts`（inject 数组声明服务依赖、装载期 fail——saolei-memory `inject: ["grpc"]` 同型先例 `common/js/dsh-plugins/saolei-memory/src/index.ts`）；**编辑对象现状**：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（:216-226 GameEventSource/MountPlayerRuntime、:294-311 deps、:454-476 MemberRuntime.game、:767-838 createMember、:840-946 mountPlayerRuntime/saoleiTools/registerMemory、:1131 终局读取——改造基线）、`common/js/dsh-plugins/saolei-loop/src/index.ts`、`common/js/dsh-plugins/saolei-loop/src/{orchestrator,index}.test.ts`、`common/js/dsh-plugins/saolei-loop/package.json` + `tsconfig.json` + `BUILD.bazel`（依赖声明现状基线——T010 增 dsh-saolei / 删 game-saolei-board）、`projects/game/agent_v2/src/session.ts`（:48-56/:172/:822-824 透传）、`projects/game/agent_v2/src/session.test.ts`（:91/:299/:345/:359/:554 mountPlayerRuntime stub）、`projects/game/agent_v2/src/dsh.test.ts`（:27 头部「21-row set」注释、:506-518 capability rows fixture、:602-652 21 行断言、:787-798 `mounts no host-level saolei tool row` 反向断言与 :802「exact 21-row set」注释——终态须改写/移除）、`projects/game/agent_v2/cordis.yml`（现行清单全文）、`projects/game/agent_v2/package.json`（:40-42 saolei 三包声明）与 `projects/game/agent_v2/BUILD.bazel`（:83/:176-178/:249 runtime_pkg 与 node_modules）；**断言落点**：`projects/game/testplan/agent_v2_game_test.go`（:829-903 TestAgentV2TeamGameStatsPromptFaces——GetSystemPrompt 读面先例）；**夹具核实对象**：`projects/game/fake-llm/service/testdata/team_planner.yaml`（team-planner-opening 的 `扫雷玩法` system_keyword——零改动核实）

- [X] T009 [US3] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 编排切换（核心任务，契约 `specs/075-saolei-game-foundation/contracts/team-loop-assembly.md` §2/§3）：① 增私有字段 `private game: GameRuntime | null = null`（类型自 `@dominion/dsh-saolei` 导入）；② `materialize`：构造时点在 player `createMember` 的 setup 内经基础面构造（`this.game = this.ctx.saolei.createGameRuntime(options.session, desktopBridge)`——desktopBridge 获取 `this.deps.desktopBridge ?? this.ctx.get("desktopBridge")` 缺失 throw，保持物化期解析时序）；materialize catch（回滚路径）与 `dispose` 中 `this.game = null`；③ `createMember` player 分支 setup：`registerSaoleiTools(agentCtx)`（不变）→ `agentCtx.systemPrompt.section(foundation.gameSection)` → 句柄注册 `agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game)`（memory face 同型）；删除 `let game` 局部与 `runtime.game` 赋值、`MemberRuntime.game` 字段与类型；④ planner 分支 setup：`agentCtx.systemPrompt.section(foundation.gameSection)` 置于 `registerMemory` 之前（D3 注册序）；⑤ `nextStep` 终局判定改 `this.game?.peekGameEvent() ?? null`（删除 `members.player.game` 路径）；⑥ 删除 `MountPlayerRuntime` 类型、`TeamOrchestratorDeps.mountPlayerRuntime`、`mountPlayerRuntime()` 私有方法（desktopBridge 获取逻辑并入②）；⑦ `GameEventRecord`/`GameEventSource`/`createAgentGameRuntime` 等 game import 全部切换 `@dominion/dsh-saolei`（`GameEventSource` 接口可保留为本地结构类型或直接用 `GameRuntime["peekGameEvent"]`——按实现最小改动核定）；⑧ `saoleiFoundation()` 读取方法（`this.ctx.saolei` 防御性 undefined 检查仅类型收窄，缺失由 inject 保证 fail-loud）
- [X] T010 [US3] `common/js/dsh-plugins/saolei-loop/src/index.ts` 与 `src/orchestrator.ts` 的 `gameStatsText` import（依赖 T009 同批）：index.ts——`inject` 改 `["saolei"]`、删除全部 game/ 再导出（`createAgentGameRuntime`/`GameRuntimeService` 值导出与 game 类型导出块）、删除 `MountPlayerRuntime` 类型导出与 `declare module "@deepseek-ai/cordis"` 的 `saoleiGame?: SaoleiGame` 增强（T004 已在 dsh-saolei 提供同名增强——本步移除双面期残留；两处声明 `SaoleiGame` 结构一致时无冲突，见 T004 注）、文件头注释终态化（编排建立在游戏基础之上）；orchestrator.ts——`gameStatsText` import 自 `"./game/text.js"` 改 `"@dominion/dsh-saolei"`（:97 唯一 import 点、`announceStats()` :1161 消费——announcer.ts 无该 import，仅提供 `announce(text)`，播报文本由编排渲染后传入）；**同批更新 saolei-loop 依赖声明**：`package.json` dependencies 增 `"@dominion/dsh-saolei": "workspace:*"`、移除迁出后无引用的 `"@dominion/game-saolei-board": "workspace:*"`；`tsconfig.json` paths 增 `"@dominion/dsh-saolei": ["../saolei/src/index.js"]`、删 game-saolei-board path；`BUILD.bazel`——`lib` deps 增 `:node_modules/@dominion/dsh-saolei`、删 `:node_modules/@dominion/game-saolei-board`，`runtime_pkg` runtime_deps 增 `//common/js/dsh-plugins/saolei:runtime_pkg`、删 `//projects/game/pkg/saolei-board:runtime_pkg`，`lib_test` data 同步；执行 `bazel run @pnpm -- --dir /mnt/code/dominion up` + `bazel run //:gazelle common/js/dsh-plugins/saolei-loop`
- [X] T011 [US3] 编排测试更新（依赖 T009/T010）：`orchestrator.test.ts`——harness 增 `ctx.provide("saolei", { createGameRuntime: fake 工厂, gameSection: fakeSection("saolei:game", 50) })`（fake 工厂返回带 `peekGameEvent` 的实例并记录调用）；删除 `mountPlayerRuntime` stub/断言（:416 区域 11 处触点）；新增断言：materialize 调用基础面工厂（fake 被调用、构造参数含 session）、编排持有实例（终局读取 `this.game`——经 fake 实例的 peekGameEvent 断言，不再经成员）、player 分支（saoleiTools 单元三工具+仅 guidance、gameSection 注册、句柄 isolate 注册的实例 === fake 工厂返回实例、注册序 tools → sections → gameSection → 句柄）、planner 分支（gameSection 注册 + memory 原序 faces → tools → sections → ready、无工具无 guidance 无 saoleiGame 句柄）、materialize 失败/dispose 后 `this.game === null`（经下次 materialize 的工厂再调用断言或暴露面核定）；`index.test.ts`——inject 语义（面缺失时插件加载失败）与 apply 双能力面 fail-loud 既有用例保留；`projects/game/agent_v2/src/session.test.ts`——删除 `mountPlayerRuntime` stub 与断言（:91/:299/:345/:359/:554）；`GameEventRecord` import 拆出改自 `@dominion/dsh-saolei`（`SaoleiSystemMember` 仍自 `@dominion/dsh-saolei-loop`，:13 拆为两行）；fake ctx 增 `saolei` 基础面（`createGameRuntime` fake 工厂，供 materialize 调用；属性/`get()` 解析路径同步）；`saoleiTools` fixture sections 收窄为仅 `saolei:guidance`（:276-277 删 `saolei:game` 项，与收窄后单元一致）
- [X] T012 [US4] `common/js/dsh-plugins/saolei/src/index.ts` 净化（同批）：移除 saoleiTools 双面——删除 `SAOLEI_TOOLS_SERVICE`、`SaoleiToolsUnit`、`buildSaoleiToolsUnit`、三工具定义、`normalizeOperateArgs`、三拒绝字面量、`resolveRuntime`、`executeOutcome`、`RESULT_OUTPUT_SCHEMA`/`resultOutput`、`SAOLEI_GUIDANCE`、`defineTool` import（工具资产全量消失，归属 `@dominion/dsh-saolei-tools`）；apply 仅 `ctx.provide(SAOLEI_FOUNDATION_SERVICE, ...)`；`package.json` 移除 `"@dominion/dsh-saolei-loop": "workspace:*"` 依赖（tsconfig paths 同步）+ `@deepseek-ai/dsh-tools` peer 移除（无工具定义后）；`BUILD.bazel`——`lib` deps 删 `:node_modules/@dominion/dsh-saolei-loop`、`runtime_pkg` runtime_deps 删 `//common/js/dsh-plugins/saolei-loop:runtime_pkg`（saolei-board runtime_dep 已由 T001 加入，保留）、`runtime_pkg` npm_deps 删 `:node_modules/@deepseek-ai/dsh-tools`（无工具定义后，按迁移后运行时 import 终态核定）、`lib_test` data 删 `:node_modules/@dominion/dsh-saolei-loop`；`index.test.ts` 重写为纯基础面测试（契约 `specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §6——面内容断言、`createGameRuntime` 工厂纯度（静态签名仅收 deps）、gameSection 逐字、**无工具断言**（导出面无 ToolDefinition 值）、无全局注册副作用）；`bazel run @pnpm -- --dir /mnt/code/dominion up` + `bazel run //:gazelle common/js/dsh-plugins/saolei`
- [X] T013 [US4] `projects/game/agent_v2/src/session.ts` 宿主清理（同批）：删除 `mountPlayerRuntime` deps 字段（:172）、透传分支（:822-824）与 `MountPlayerRuntime` 类型 import（:52）；其余零改动（已核实无其他游戏类型消费）
- [X] T014 [US4] 组合清单与依赖终态（同批）：`projects/game/agent_v2/cordis.yml`——增顶级行 `- { id: saolei, name: '@dominion/dsh-saolei' }`（置于 `@dominion/*` 分区，grpc-runtime 行之后；行注释：游戏基础服务 `ctx.saolei`——`createGameRuntime` 工厂 + 玩法 section，saolei-loop 经 inject 消费）；saolei-loop 行 `config.plugins` 首项 `- name: '@dominion/dsh-saolei'` 改 `- name: '@dominion/dsh-saolei-tools'`；头部注释终态化（saolei-loop 能力依赖行注释改述：`@dominion/dsh-saolei-tools` 提供 saoleiTools 单元）；`projects/game/agent_v2/package.json` 增 `"@dominion/dsh-saolei-tools": "workspace:*"`；`projects/game/agent_v2/BUILD.bazel` 相应 `runtime_pkg` 与 `node_modules` 增补（对齐 :176-178 既有三包模式）；`bazel run @pnpm -- --dir /mnt/code/dominion up` + `bazel run //:gazelle projects/game/agent_v2`
- [X] T015 [US4] `projects/game/agent_v2/src/dsh.test.ts` 断言终态化（同批，契约锁步）：行集合断言（:602-652）21 → **22 行**（id/name 两表在 `grpc-runtime` 后插入 `saolei`/`@dominion/dsh-saolei`）；capability-dependency rows fixture（:506-518）首项 `{ name: "@dominion/dsh-saolei" }` 改 `{ name: "@dominion/dsh-saolei-tools" }`；新增断言：顶级 saolei 行无 config；**删除/改写** `mounts no host-level saolei tool row` 用例（:796-798——其 `rows.find((row) => row.id === "saolei")).toBeUndefined()` 与 `names` 不含 `@dominion/dsh-saolei` 的断言与终态顶级 saolei 行直接冲突），改写为「顶级 saolei 行存在（id `saolei` / name `@dominion/dsh-saolei`）且无 config」；同步将 :27 与 :802 的「21-row set」注释更新为 22 行；**同批锁步修订** `specs/070-dsh-upgrade/contracts/composition-manifest.md` §2 行集合口径为 22 行（074 T005 契约/代码锁步先例；其余 070 修订点归 Phase 6 T018）
- [X] T016 [US4] `projects/game/testplan/agent_v2_game_test.go` 三显式断言（同批，`style/golang.md` §单元测试规范；断言只增不改既有用例语义）：`TestAgentV2TeamGameStatsPromptFaces`（:829-903）内新增——① planner prompt 断言：`plannerPrompt` 含 `"## 扫雷玩法与可用操作"`（`specs/075-saolei-game-foundation/spec.md` FR-018 断言 1）；② player prompt 断言：player prompt 兼含玩法标题与 `"## saolei (Minesweeper tools)"`（断言 2）；③ planner 边界断言：planner prompt 不含 guidance 标题（断言 3）；断言常量按既有 fixture 常量风格定义（`agent_v2_helpers_test.go` 常量区）；`bazel build //projects/game/testplan:agent_v2_game_test` 通过
- [X] T017 [US3] 大型测试验收（宪法 VI；依赖 T009–T016 全部完成）：通过 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（部署→测试→清理闭环），四 suite（game-system / game-disconnect / game-memory-down / game-stall）全部用例通过——含 T016 三个显式断言通过（SC-002 主锚）与 player 游戏回路零回归（SC-001/SC-006）；**夹具核实项**（非验收依据）：`projects/game/fake-llm/service/testdata/team_planner.yaml` 零改动、team-planner-opening 的 `扫雷玩法` system_keyword 恢复命中（planner 开局回合由该模板应答）——执行记录中核实；若出现 planner 模板匹配异常，以显式断言的 GetSystemPrompt 实况为准排查（`specs/075-saolei-game-foundation/research.md` D8 背景风险）；任何失败修复后重跑至全绿（测试计划 YAML 零改动；Go 用例仅 T016 新增断言）

**Checkpoint**: 终态通路生效 = **特性行为面交付**（SC-001/SC-002/SC-003/SC-005/SC-006 全部达成）。

---

## Phase 6: User Story 4 - 终态清理与契约修订 (Priority: P2)

**Goal**: 受影响契约修订至终态、README/注释终态化、引用清零复查——纯文档与静态验证，无代码变更（四 suite 结果持续有效）。

**Independent Test**: quickstart.md §2 全量通过（静态检索零命中 + 目录核对 + 组合清单核对）；契约 diff 审阅通过。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无（纯文档修订）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/075-saolei-game-foundation/contracts/cutover-contracts.md`（§3 契约修订清单、§5 静态复查命令）；`specs/075-saolei-game-foundation/quickstart.md`（全文——T020 验证步骤）；修订对象原文（`specs/075-saolei-game-foundation/research.md` D7 表逐份）：`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md`、`specs/074-dsh-grpc-runtime/contracts/team-assembly.md`、`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§2 已由 T015 锁步行集合，其余修订点本 phase）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`、`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`、`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`、`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（八份全文——按 D7 表修订，行为条款零改动）；编辑对象：`projects/game/agent_v2/README.md` 与 `common/js/dsh-plugins/{saolei,saolei-tools,saolei-loop}/README.md`（旧形态表述改写基线）

- [X] T018 [US4] 契约修订（`specs/075-saolei-game-foundation/research.md` D7 表逐份）：`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1（注册表述：saolei-loop apply 全局注册 → 内容归 `@dominion/dsh-saolei`、编排按角色注册 player+planner）、§4（顺序表注册点注记）、§5（断言语义改述为显式 prompt 断言 + 夹具锚点核实项）；`specs/074-dsh-grpc-runtime/contracts/team-assembly.md` §3.1（saoleiTools 提供方改 `@dominion/dsh-saolei-tools`、sections 收窄为 [saolei:guidance]）、§5 行为差异②补正（saolei:game 注册路径）、§6（`saoleiGame` 运行时挂载引用改编排持有句柄、路径改 dsh-saolei）；`specs/070-dsh-upgrade/contracts/composition-manifest.md`（§4 声明面表增 dsh-saolei-tools 与顶级 saolei 行；§2 除 T015 已锁步的行集合外其余口径）；`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2/§3（包边界终态化：dsh-saolei = 游戏基础面、dsh-saolei-tools = 工具插件、GameRuntime 普通类与编排持有——074 后已过时的"全局注册"表述一并终态化；游戏行为条款零改动）；`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（工具/玩法条款包归属改述，冻结值条款零改动）；`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`（§1/§2 实现归属路径改 `@dominion/dsh-saolei` / `@dominion/dsh-saolei-tools`，语义条款零改动）；`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`（gameStatsText 归属改 `@dominion/dsh-saolei` 导出、编排 `announceStats()` 消费——announcer 播报，播报协议零改动）；`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（`createAgentGameRuntime` → `createGameRuntime` 等 game 路径引用终态化，编排行为条款零改动）
- [X] T019 [P] [US4] 终态清理：`projects/game/agent_v2/README.md` 与 `common/js/dsh-plugins/{saolei,saolei-tools,saolei-loop}/README.md`（saolei README 改述游戏基础形态、saolei-loop README 移除 game/ 内嵌表述、新增 saolei-tools README）；全仓引用清零复查——`specs/075-saolei-game-foundation/quickstart.md` §2 命令组：① `grep -rnE "dsh-saolei-loop|dsh-saolei-tools|dsh-saolei-memory" common/js/dsh-plugins/saolei/src/ --include="*.ts" | grep -v ".test.ts"`（依赖方向；排除 .test.ts 中的类型 import 核定项）零命中；② saolei-loop 内 `gameStatsText|createGameRuntime` 的本地 import 清零；③ `common/js/dsh-plugins/saolei-loop/src/game/` 目录不存在、`common/js/dsh-plugins/saolei-tools/src/` 与 `common/js/dsh-plugins/saolei/src/game/` 存在；④ 旧符号清零：`createAgentGameRuntime|GameRuntimeService|MountPlayerRuntime` 在 common/ projects/ 的代码引用（import/类型位置/标识符使用）清零——机械判据与命令 = `specs/075-saolei-game-foundation/contracts/cutover-contracts.md` §5 第四组（`grep -vF` 行排除 saolei-tools `resolveRuntime` 冻结 fail-loud 文案中的 `via createAgentGameRuntime` 字样——T007 逐字迁入的运行时行为值，属字符串字面量提及而非代码引用，豁免依据见该 §5 判定说明；契约/specs 修订文本除外——specs/ 不在检索路径）；各包注释复查——只表述终态（宪章原则 VII）
- [X] T020 [US4] quickstart 验证收尾：按 `specs/075-saolei-game-foundation/quickstart.md` §1–§3 执行（`bazel build //...` + §1 全部 target 单测复跑、§2 静态检索与目录/清单核对、§3 四 suite 结果确认——T017 已完成且 T018/T019 无代码变更，不需重跑；若意外触及代码，重跑受影响验证）；核对 §4 行为面抽查要点（planner prompt 含玩法、player 面等价、gameStatsText 播报不变）

**Checkpoint**: 交付物只表述终态（SC-004/SC-007 复核）= 特性完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1/2（Setup/Foundational）**: 无任务，直接进入 Phase 3
- **Phase 3（US1 基础双面期）**: 结构基座——T001 → T002 → T003 串行（T003 的构造形态适配依赖 T002 的类形态）；T002 完成后 T003 与 T004 可并行（不同文件域）；T005 依赖 T004
- **Phase 4（US2 工具包）**: 依赖 Phase 3 的类型导出（T007 import `@dominion/dsh-saolei` 的 `OperateInput`/`SaoleiGame`/`ToolOutcome`）；T006 → T007 → T008 串行（同包文件依赖）
- **Phase 5（原子切换）**: 依赖 Phase 3/4 全部完成；**T009–T016 单一合入单元**（不得拆分，见 CRITICAL）；T017 最后
- **Phase 6（US4 终态）**: 依赖 Phase 5 验收通过；纯文档

### User Story Dependencies

- **US1（P1）**: 无依赖——结构基座（游戏类型出口 + 基础面 + game 迁移等价）
- **US2（P1）**: 依赖 US1 的类型导出（编译依赖；工具行为测试独立可测）
- **US3（P1）**: 依赖 US1（基础面）+ US2（saoleiTools 提供方）——Phase 5 激活
- **US4（P2）**: 组合切换部分随 Phase 5 原子落地；契约/文档终态在 Phase 6（依赖 Phase 5 全绿）

### Within Each User Story

- 契约文档（075 contracts/ 四份）为唯一行为规范——实现分歧以契约为准
- 等价迁移任务（T002/T003/T007）的"零逻辑改动/逐字不变"以迁移源为比对基准——review 时逐字段 diff（T002 的形态改造点以 research.md D10 为唯一授权变更清单）
- 每个 phase 完成即 `bazel build`/`bazel test`（相关 target）通过（宪法 IV，不单列 task）

### Parallel Opportunities

- Phase 3: T002 → T003 串行（T003 的构造适配依赖 T002 的类形态）；T004 与 T003 可并行（不同文件域——index.ts vs game/*.test.ts，均依赖 T002 完成）
- Phase 5 内 T009–T016 为原子批次（同批编写、一次合入；无并行收益）
- Phase 6: T018 ∥ T019（文档域不同——契约 vs README/注释）

---

## Parallel Example: Phase 3

```text
T001（依赖增补） → T002（game 四文件迁入，含 runtime.ts 形态改造）
                     ├→ T003（game 测试迁入——构造形态适配）
                     └→ T004（index.ts 双面期改造——依赖 T002 的工厂/导出）
                            └→ T005（index.test.ts 面断言扩展——依赖 T004）
T003 与 T004 可并行（不同文件域，共同前置 T002）
```

---

## Implementation Strategy

### MVP First

本特性为重新划分型（行为零回归契约 + planner 玩法恢复），**用户可见价值在 Phase 5 原子切换落地时一次性兑现**——MVP 是完整链路：Phase 3 → 4 → 5 顺序交付，Phase 5 四 suite 全绿（含三显式断言）= MVP 达成（SC-001/SC-002）。Phase 3/4 为可独立 review 的安全中间态（每步全仓可构建、组合行为零变化）。

### Incremental Delivery

1. Phase 3（US1）: 基础双面期（旧世界并行存在）→ review/合入
2. Phase 4（US2）: 工具包（无消费方）→ review/合入
3. Phase 5（原子切换）: **一次合入 T009–T016 → T017 四 suite 验收** = 特性行为面交付 🎯
4. Phase 6（US4）: 契约修订 + 终态清理（纯文档）

### 中断恢复

每个 phase 是独立合入单元；Phase 5 未完成 T009–T016 全部八项前不得部分合入（双 provide/双 game 并存中间态不可用）——中断时以 `bazel build //... && bazel test //...` 全绿 + cordis.yml 处于旧态或新态（无混合态）为恢复判据。

---

## Notes

- [P] tasks = 不同文件、无未完成依赖
- 契约文档（specs/075-saolei-game-foundation/contracts/）与 research.md D1–D10 是实现分歧的唯一裁决源
- 每任务完成后 commit；Phase 5 为单一合入单元（squash 或同批 PR）
- 074 遗留问题的收口验证在 T016（显式断言）与 T018（060 契约修订），不以夹具匹配行为为依据（`specs/075-saolei-game-foundation/research.md` D8）
- 若 Phase 5 实施中发现 cordis `inject: ["saolei"]` 的装载语义与预期不符（如 provide 面不满足 inject 解析），回退讨论先于就地改道（对齐 074 tasks 的 cordis 版本复核纪律）
