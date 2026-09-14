# Tasks: 064-memory-split-fold-remain

**Input**: Design documents from `/specs/064-memory-split-fold-remain/`

**Prerequisites**: plan.md（Technical Context/Project Structure）、spec.md（FR-001~008 / SC-001~005 / US1~US3）、research.md（D1~D6）、data-model.md（§1~§5）、contracts/（dsh-plugins.md / web-ui.md / saolei-plugins.md）、quickstart.md（场景 1~5）

**Tests**: 单测按宪章原则 IV 内嵌于代码变更任务（不单列）；大型测试验收单列（原则 VI，Phase 5 T009）。

**Organization**: 按 user story 分 phase（US1/US2 = P1，US3 = P2）；三 story 相互独立可并行（不同文件面）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story
- 每任务含精确文件路径；bazel build+test 为代码任务的组成部分（AGENTS.md 命令入口）

---

## Phase 1: Setup（基线验证）

**目的**: 确认变更前基线为绿，建立可回归的起点。

**文档清单**：
- 代码规范文档：无
- 官方文档：无
- 技术文章/技术参考文档：`specs/064-memory-split-fold-remain/plan.md`（Project Structure 一节——变更面清单）

- [X] T001 基线验证：`bazel test //common/js/dsh-plugins/memory/... //common/js/dsh-plugins/saolei/... //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/frontend/...` 全绿——记录基线结果供后续 phase 对照

---

## Phase 2: User Story 1 - memory 插件拆分：host 基建与 agent 工具各归其位 (Priority: P1) 🎯 MVP

**Goal**: `@dominion/dsh-memory` 双面拆为 `@dominion/dsh-memory-service`（host 基建）与 `@dominion/dsh-memory`（纯工具面）；preset 维度规则只引用工具插件（FR-001~FR-003 / SC-001 / SC-002）。

**Independent Test**: `bazel build //...` 全绿；`bazel test //common/js/dsh-plugins/memory-service/... //common/js/dsh-plugins/memory/... //projects/game/agent_v2/...` 通过；组合面五处终值（quickstart 场景 1）。

**文档清单**：
- 代码规范文档：`style/javascript.md`；其引用基准 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（冲突时以 `style/javascript.md` 优先）
- 官方文档：无（纯仓库内重组，无第三方 API 新面）
- 技术文章/技术参考文档：`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（拆分终态契约——实现的直接依据）、`specs/064-memory-split-fold-remain/data-model.md` §1~§2 与 §5（文件归属表 + 组合五处终值 + 不变实体零回归锚定）、`specs/064-memory-split-fold-remain/research.md` D1/D3（域核心归属与三面清单）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（新包 package.json/tsconfig/测试契约）、`specs/058-dsh-preset-roster-demo/contracts/composition-manifest.md` §4（BUILD 增删通道：npm_deps 仅 registry 包、workspace 包走 runtime_deps）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2/§3/§5（被拆分基线：物化编排与 `plannerMemory.load` seam + memory 插件行为语义 + 组合行清单——本 feature 修订其 memory 行；不变量重申）

- [ ] T002 [US1] 新建 `common/js/dsh-plugins/memory-service/` 包（`@dominion/dsh-memory-service`）：`git mv` 迁入 `common/js/dsh-plugins/memory/src/` 的 `client.ts`/`operations.ts`/`snapshot.ts`/`service.ts` 与对应 `client.test.ts`/`operations.test.ts`/`service.test.ts`；新 `src/index.ts` 承载 host 行（cordis 名 `memory`、`inject = []`、`provide("plannerMemory")`，文件头注释按 `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §1 重写）；`package.json`（`"type": "module"`、deps：grpc-js/proto-loader/grpc-resolver/logs + peer：cordis/dsh-agent/dsh-scope，对照 esm-package-conventions §1~§2）与 `tsconfig.json`（对照 `common/js/dsh-plugins/memory/tsconfig.json` 同型）、`.swcrc`（对照 memory 包同型——048 契约 §2 要求 tsconfig/.swcrc 锁步）；旧 `common/js/dsh-plugins/memory/` 包本任务保持双面形态但核心导入源切换——全部指向迁出文件的相对导入改为 `@dominion/dsh-memory-service` 跨包导入（`src/index.ts`：`./client.js`/`./service.js`；`src/tool.ts`：`./operations.js`/`./service.js`；`src/preset-row.ts` 与 `src/preset-row.test.ts`：`./snapshot.js`；`src/tool.test.ts`：`./service.js`），不留断链（re-export 面保持不变），`package.json` 增 `"@dominion/dsh-memory-service": "workspace:*"`；两个 BUILD.bazel 经 `bazel run //:gazelle common/js/dsh-plugins` 生成后为 memory-service 补 `js_runtime_library :runtime_pkg`（package_name `@dominion/dsh-memory-service`，npm_deps = grpc-js/proto-loader，runtime_deps = grpc resolver/logs，对照 `common/js/dsh-plugins/memory/BUILD.bazel` 现形态）、memory 包 deps 增 service 包 link target；`pnpm-lock.yaml` 经 `bazel run @pnpm -- --dir /mnt/code/dominion up` 刷新；验证 `bazel test //common/js/dsh-plugins/memory-service/... //common/js/dsh-plugins/memory/...` 全绿（此时 agent_v2 组合未动，全仓仍绿）
- [ ] T003 [US1] 拆分切换（三面原子，依赖 T002）：① `common/js/dsh-plugins/memory/src/preset-row.ts` 内容升为 `src/index.ts`（cordis 名 `memory-row`、`inject = ["plannerMemory","tools","systemPrompt"]`，文件头注释指向 `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §1），删除 `preset-row.ts`，`preset-row.test.ts` 改名 `index.test.ts` 随迁；`package.json` exports 仅 `.`、deps 收缩（去 grpc-resolver/logs，保留 dsh-tools + memory-service）；BUILD.bazel 随 gazelle 重生成并收缩（npm_deps 去 grpc 系，runtime_deps 改为 memory-service:runtime_pkg），并同步改写旧 BUILD 顶部 `host face and the ./preset-row subpath` 注释为拆分后终态描述。② `projects/game/agent_v2/package.json` dependencies 增 `"@dominion/dsh-memory-service": "workspace:*"`。③ `projects/game/agent_v2/cordis.yml`：host 行（:155）name 改 `'@dominion/dsh-memory-service'`；templateRules player.forbidden 改 `['@dominion/dsh-memory']`、planner.required 改 `['@dominion/dsh-memory']`；头部注释（:10-14）裸名解析说明同步。④ `projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml:6` 行名改 `'@dominion/dsh-memory'`。⑤ `projects/game/agent_v2/BUILD.bazel` runtime_deps 增 `"//common/js/dsh-plugins/memory-service:runtime_pkg"`（gazelle 后确认）。⑥ `projects/game/agent_v2/src/dsh.test.ts` 断言更新（:541-545 playerRows 不含 `@dominion/dsh-memory`、plannerRows 恰一个 `@dominion/dsh-memory`，并补 `playerRows`/`plannerRows` 均 `not.toContain("@dominion/dsh-memory-service")`；:701-725 authoring config `toEqual` fixture 的 templateRules 与 `byId.get("memory")` → `@dominion/dsh-memory-service`）。⑦ `projects/game/agent_v2/README.md:163` 模板守则描述更新（player forbidden 仅 memory 工具插件）；⑧ `common/js/dsh-plugins/preset-authoring/src/index.test.ts`（:354/:389/:548/:556/:571/:578）与 `src/derive.test.ts`（:179/:192/:216/:231）中作为不透明夹具字符串的 `@dominion/dsh-memory/preset-row` 一律对齐为 `@dominion/dsh-memory`（语义不变，使 T008 终态检查真零命中）；`pnpm up` + `bazel run //:gazelle projects/game/agent_v2` + `bazel mod tidy`；验证 `bazel build //...` 全绿 + `bazel test //common/js/dsh-plugins/memory/... //common/js/dsh-plugins/preset-authoring/... //projects/game/agent_v2/...` 通过

**Checkpoint**: SC-001/SC-002 达成——组合面五处终值、行为零回归（US1 可独立验证，即 MVP）。

---

## Phase 3: User Story 2 - 游戏结束的 player 回合折叠 (Priority: P1)

**Goal**: webUI 完成回合折叠规则扩展为三分类——终局收束回合以末步为锚折叠，与 planner 回合同型（FR-004~FR-006 / SC-003）。

**Independent Test**: `bazel test //projects/game/web/frontend:lib_test`——新增终局折叠用例（回填 + 实时两路径）通过、既有 interrupted/最终答案用例零回归（quickstart 场景 2）。

**文档清单**：
- 代码规范文档：`style/javascript.md`；其引用基准 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（冲突时以 `style/javascript.md` 优先）
- 官方文档：`@deepseek-ai/dsh-client-ui-chat` README "Turn Process Folding" 章节（https://www.npmjs.com/package/@deepseek-ai/dsh-client-ui-chat ；源仓库 https://github.com/deepseek-ai/deepseek-harness ）——折叠规则的官方行为陈述（054 契约 §2.2 的行为依据）
- 技术文章/技术参考文档：`specs/064-memory-split-fold-remain/contracts/web-ui.md`（三分类规则——实现的直接依据）、`specs/064-memory-split-fold-remain/data-model.md` §3（回合分类纯函数）、`specs/064-memory-split-fold-remain/research.md` D2（判定推导与既有测试影响）、`specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.2（被修订基线——最终答案/无最终答案两条款语义）、`specs/054-agent-v2-bugfixes/revisions/phase4-failed-turn-folding.md`（interrupted 字段与失败回合展开的设计裁定——失败桶基线）、`specs/054-agent-v2-bugfixes/data-model.md` §1.5 与 §5.2（interrupted 字段语义与传递 + 回合终态处置投影）、`projects/game/web/frontend/src/store/chat.ts`（现状代码只读参照，非文档：turn_end 归约 :671-695 的 COMPLETED 无标记 / ERROR/CANCELED 尾步 interrupted 投影——零改动面）、`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md` §3（turn 收束消费语义——终局收束回合判定所依据的机制形态；062 spec 声明机制细节归其契约）、`specs/062-team-game-end-handoff/spec.md`（终局收束无痕性——被消费的派生形态背景）

- [ ] T004 [US2] 修改 `projects/game/web/frontend/src/components/ChatView.tsx` 的 `CompletedTurn`（:233-302）：折叠判定扩展三分类——先按现状找最终答案步（isFinalAnswer），未命中时若回合内全部消息均无 `interrupted` 标记则以末步为锚（`process = messages.slice(0, lastIndex)` 折叠、末步整步可见），存在 interrupted 标记则保持现状全展开；`isFinalAnswer`（:187-193）与文件头/函数注释同步指向 `specs/064-memory-split-fold-remain/contracts/web-ui.md` §1；`projects/game/web/frontend/src/components/ChatView.test.tsx` 同批：:504 与 :542 两个既有用例的 fixture 尾步补 `interrupted: true`（归入失败桶，断言不变——陈旧 RUNNING 中断推导语义保持），新增用例组：终局收束回合（多步、末步 THINK|TEXT|TOOL 且工具块 SUCCEEDED 带 result、无 interrupted）默认折叠（toggle 存在、标签计数 = 过程步数与过程内工具块数、末步锚含工具卡片可见）、点击展开/收起、单步终局回合无折叠控件、成员视角视图同型一例、实时路径一例（`ChatStore` + 流式 harness，参同文件 :965-1006 CANCELED 用例形态：`turn_end{COMPLETED}` 终局收束回合实时收束后即折叠，与回填形态一致——FR-006）；验证 `bazel test //projects/game/web/frontend/...` 全绿

**Checkpoint**: SC-003 达成——US2 与 US1 相互独立（不同文件面）。

---

## Phase 4: User Story 3 - saolei_remain 语义澄清 (Priority: P2)

**Goal**: remain 三处表述（工具 description / 玩法规则 prompt / 结果体 legend）主语义 = 每数字格周围剩余未标记雷数，显式排除旗数误读并锚定坐标读法（FR-007/FR-008 / SC-004）。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop/... //common/js/dsh-plugins/saolei/...`——legend 断言通过、既有前缀断言零破坏（quickstart 场景 3）。

**文档清单**：
- 代码规范文档：`style/javascript.md`；其引用基准 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)（冲突时以 `style/javascript.md` 优先）
- 官方文档：无
- 技术文章/技术参考文档：`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（三处表述终态——实现的直接依据）、`specs/064-memory-split-fold-remain/data-model.md` §4（结果体文本契约）、`specs/064-memory-split-fold-remain/research.md` D4（legend 落点与测试影响）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2（结果文本契约索引——指向 data-model §2.5）、`specs/051-agent-v2-dsh-migration/data-model.md` §2.5（remain per-cell 语义——本 feature 修订其 remain 条目）、`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1/§2（`saolei:game` 玩法行与 `saolei:guidance` 工具条目的所有权分界——本 feature 修订两处 remain 表述）、`common/js/dsh-plugins/saolei/README.md`（现状参照——三工具注册形态）

- [ ] T005 [P] [US3] 修改 `common/js/dsh-plugins/saolei-loop/src/game/text.ts` 的 `remainText`（:101-108）：在 `board size` 行与网格之间插入 legend 行（英文单行，语义按 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` §1：每格值 = 该数字格周围剩余未标记雷数 = cell number − adjacent flags，可为 0 或负；NOT the count of flags；Columns are x and rows are y — the same (x, y) as saolei_operate），文件头注释补引 064 契约；`common/js/dsh-plugins/saolei-loop/src/game/runtime.test.ts` 同批（:573-587 remain 用例）：既有 `toContain("saolei_remain → computed\ngame status: playing")` 前缀断言保持，新增 legend 关键词断言（mines still unmarked / NOT the count of flags / Columns are x and rows are y）与 legend 位于 board size 行之后的顺序断言；验证 `bazel test //common/js/dsh-plugins/saolei-loop/...` 全绿
- [ ] T006 [P] [US3] 措辞同步两处（与 T005 不同文件，可并行）：① `common/js/dsh-plugins/saolei/src/index.ts`——`saolei_remain` 工具 description（:238-246）主语义前置为"剩余未标记雷数"并显式排除旗数读法（保留公式派生、`-` 哨兵、`no_active_game`、终局不阻断既有边界），`SAOLEI_GUIDANCE` 的 remain 条目（:317）同向改写；`common/js/dsh-plugins/saolei/src/index.test.ts` 与 `common/js/dsh-plugins/saolei/README.md` 同步。② `common/js/dsh-plugins/saolei-loop/src/index.ts`——`SAOLEI_GAME_RULES` remain 行（:145）改为"剩余未标记雷数"主语义 + 旗数排除，全局剩余雷数计数行（:139）补"顶部计数器"区分表述；`common/js/dsh-plugins/saolei-loop/src/index.test.ts` 措辞断言同步（:51-58 区域：新增 `剩余未标记雷数`、`不是旗子数量`、`顶部计数器` 断言，既有玩法关键词集保留）；验证 `bazel test //common/js/dsh-plugins/saolei/... //common/js/dsh-plugins/saolei-loop/...` 全绿（若与 T005 并行，合并后串行复跑 saolei-loop）

**Checkpoint**: SC-004 达成——US3 与 US1/US2 相互独立。

---

## Phase 5: Polish & 大型测试验收

**目的**: 契约修订注记、终态检查、大型测试闭环（宪法 VI）。

**文档清单**：
- 代码规范文档：`style/large_test.md`（testplan/guitar 执行规范）、`style/javascript.md`（T008 若修改 TS 注释时）
- 官方文档：无
- 技术文章/技术参考文档：`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`、`specs/064-memory-split-fold-remain/contracts/web-ui.md`、`specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（T007 注记指向对象与 T008 注释重指向核对基准）、`specs/064-memory-split-fold-remain/quickstart.md`（场景 4——大型测试验收面与命令）、`specs/064-memory-split-fold-remain/research.md` D5（契约修订策略与注记形态）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md`（注记对象 §3/§5）、`specs/054-agent-v2-bugfixes/contracts/web-ui.md`（注记对象 §2.2）、`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2 与 `specs/051-agent-v2-dsh-migration/data-model.md` §2.5（注记对象）、`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md`（注记对象 §1/§2）、`specs/060-agent-v2-team-optimize/contracts/team-api.md`（顶部增量修订注记的先例形态）

- [ ] T007 [P] 四份旧契约加修订注记（060 修订 059 的先例形态：顶部注记指向 064，不重写正文）：① `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §3 与 §5 顶部注记指向 `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（memory 插件双包拆分）；② `specs/054-agent-v2-bugfixes/contracts/web-ui.md` §2.2 表格上方注记指向 `specs/064-memory-split-fold-remain/contracts/web-ui.md`（无最终答案条款的终局收束例外）；③ `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2 的结果文本契约索引行与 `specs/051-agent-v2-dsh-migration/data-model.md` §2.5 的 remain 语义行旁注记指向 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（结果体 legend 与表述修订）；④ `specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1/§2 的玩法行/工具条目旁注记指向 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md`（remain 表述修订）
- [ ] T008 终态检查（宪法 VII）：① `rg -n "dsh-memory/preset-row" common/ projects/` 零命中（`specs/` 历史文档为版本记录、不在搜索面；preset-authoring 夹具已随 T003 对齐）；② `rg -n "\./preset-row" common/js/dsh-plugins/memory/` 零命中（导出面/源码/BUILD 注释均收敛——BUILD.bazel 无扩展名，检查需直搜目录而非 grep --include）；③ `rg -n "dsh-memory/preset-row" pnpm-lock.yaml` 零命中（T003 的 pnpm up 已刷新）；④ `rg -n "specs/059-agent-v2-team-mode/contracts/dsh-plugins.md" common/js/dsh-plugins/memory/ common/js/dsh-plugins/memory-service/` 命中的注释均已重指向 064 契约；不一致项修复后复跑 `bazel build //...`
- [ ] T009 大型测试验收（宪法 VI，依赖 T003/T004/T005/T006/T008）：经 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（仓库唯一的 agent_v2 计划，四个 suite：`game-system`（game 主线含终局收束/复盘链 + conversation 面的 cancel/backfill/失败中断等用例）、`game-disconnect`（断开收敛）、`game-memory-down`（memory 服务缺失的物化 fail-loud）、`game-stall`（流停滞看门狗）），完成部署→测试→清理闭环；全部用例通过（failed/flaky 均视为未通过，修复后重跑至全绿）；仅 `bazel build` 测试 target 不构成验收；SC-005 的 webUI 侧折叠呈现以 T004 组件测试等效锚定（guitar 用例面为服务端行为，不含 webUI 渲染断言）

**Checkpoint**: 全部 SC（SC-001~SC-005）达成，feature 验收完成。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（基线）**: 无依赖，立即开始。
- **Phase 2（US1）**: 依赖 Phase 1；内部串行 T002 → T003（绿-绿两步，中间态可审查）。
- **Phase 3（US2）**: 依赖 Phase 1；与 Phase 2/4 文件面不相交，可并行。
- **Phase 4（US3）**: 依赖 Phase 1；内部 T005 ∥ T006（不同文件），T006 自含两包措辞；与 Phase 2/3 可并行。
- **Phase 5（收尾）**: T007 可提前并行（纯文档）；T008 依赖 T003；T009 依赖全部实现 phase。

### Parallel Opportunities

- Phase 2（memory 包族）∥ Phase 3（web 前端）∥ Phase 4（saolei 措辞）——三个文件面零交叠，可三线并行。
- Phase 4 内部：T005（saolei-loop/src/game/text.ts）∥ T006（saolei/src + saolei-loop/src/index.ts）。
- Phase 5 内部：T007（契约注记）可与其他任何 phase 并行。

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 基线绿 → 2. Phase 2 拆分落地 → 3. 验证 US1（组合面五处终值 + 行为零回归）→ 可独立交付（架构修正价值自足）。

### Incremental Delivery

1. US1 拆分（MVP）→ 2. US2 折叠（用户直接可感）→ 3. US3 remain 措辞（模型推理正确性）→ 4. Phase 5 契约注记 + 终态检查 + 大型测试全量验收。每步独立可验证，可中断恢复（phase 间无半成品耦合）。

---

## Notes

- 每个 phase 开工前完整阅读该 phase 文档清单（宪法 V）；AGENTS.md 与 spec 相关文件为必读无需重复列出。
- 编译 + 单测内嵌各任务（宪法 IV）；大型测试为独立验收任务 T009（宪法 VI）。
- 代码注释引用更新（重指向 064 契约）随各实现任务落地（T002/T003/T004/T005/T006 内嵌），T008 做终态核对。
- 提交粒度：按任务或逻辑组提交；T003 为单一原子变更（组合三面不可拆分提交）。
