# Tasks: agent_v2 优化——wiki 注入收敛、team 成员流程标准化与 announcer 成员化

**Input**: Design documents from `/specs/076-agent-v2-optimize/`

**Prerequisites**: plan.md（批次交付计划）、spec.md（US1–US3 与 FR-001–FR-206）、research.md（D1–D10）、data-model.md、contracts/（flow-orchestration / memory-library-layering / announcer-member）、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task，任务完成即要求 `bazel build` / `bazel test` 相关 target 通过）；大型测试在每批末尾单独分配验收任务（testplan skill 实际执行，部署→测试→清理闭环，全部用例通过）。mock 约定按 `style/javascript.md`：DI / `vi.fn` test-double（禁止新增 `vi.mock` 模块拦截）。

**Organization**: 按用户裁定的交付批次排序（research.md D2，2026-10-14）：**Phase 3 = US2（批1）→ Phase 4 = US3（批2）→ Phase 5 = US1（批3）**——与 spec 优先级（US1=P1）不同序的原因：FR-206 要求 announcer 名册放宽先行或同批于 flow 重构；风险递增排序使 `orchestrator.test.ts` 行为保持基准最干净。三批相互独立可交付（批1/批2 无代码耦合；批2 必须先于批3 合入）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US3）；Setup/Foundational/Polish phase 无 story 标签
- 每个任务含精确文件路径（仓库根相对路径）

## Path Conventions

仓库根相对路径；公共库 `common/js/memory/`，dsh 插件 `common/js/dsh-plugins/`，agent_v2 `projects/game/agent_v2/`，web 前端 `projects/game/web/frontend/src/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更，无新包/新第三方依赖（flow 为包内自研模块，research.md D1；策略注入消费既有 grpc-runtime 服务面，D10）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（各实现 phase 自带文档清单）

---

## Phase 2: Foundational

**无任务**：本特性无跨批共享阻塞件（无 proto 消息形状变更、无 schema 迁移、无新契约服务面——三批各自携带完整的契约与测试基准）。批间顺序约束见 Dependencies。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无（本 phase 无任务；各实现 phase 自带文档清单）

---

## Phase 3: User Story 2 - wiki 注入移除 + memory 库两层 + unary 策略注入（批1）(Priority: P2) 🎯 MVP（"MVP" = D2 首批交付（可独立交付的最小批次），非 spec 最高优先级——spec 优先级仍为 US1=P1，排序依据见本文件 Organization）

**Goal**: `@dominion/common-js-memory` 分为功能单元层（memory 单元 / wiki 单元，各自独立可用）与组合层（memory+wiki 等价组合）；unary 调用策略副本删除、改经组装 seam 注入（`ctx.grpc.callUnary`）；`@dominion/dsh-saolei-memory` 只注入 memory（planner 能力面零 wiki 痕迹）；074 契约修订。wiki 能力/基建/服务零改动。

**Independent Test**: `bazel test //common/js/memory/... //common/js/dsh-plugins/saolei-memory/...` 全绿（单元独立挂载、组合等价、无策略副本、memory-only 注入断言——库的仓库内消费方仅 saolei-memory，saolei-loop 不受本批影响）；wiki 大型测试零改动回归 + conversation 回归通过（T006）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（ESM 书写规则——相对导入带 `.js` 扩展名、`export type` 显式；`vitest_test` 宏 data 规则；DI mock 约定；含其引用的 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）；`style/large_test.md`（T006 大型测试执行——testplan 组织与反模式；含其强制引用的 `style/golang.md`——T006 只执行既有 Go 测试、不改 Go 代码，作参考）与 `style/golang.md` 引用的 [Google Go Style: Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)
  - 未列入说明（原则 V）：`style/javascript.md` 另引用 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（测试执行 shim 退出码/空套件契约）、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（OTel 插桩时序）、`specs/048-js-esm-migration/research.md` R5（CJS 具名导入逐包核实表）与 `specs/019-js-test-reliability/`（js_test 执行模型/mock 根因背景）——本批不触碰测试执行模型、插桩面与 CJS 具名导入核实，与本 phase 任务不相关
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)（`vi.fn` DI doubles）；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（`style/javascript.md` 引用的"禁止 `vi.mock` 模块拦截"约定依据）
- **技术文章/技术参考文档**：
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（**全文**——`style/javascript.md` 引用的包级 ESM 契约：package.json/tsconfig/.swcrc 锁步与书写规则）
  - `specs/076-agent-v2-optimize/contracts/memory-library-layering.md`（**全文**——两层导出面/独立挂载义务/策略注入 seam/组合等价锚点/验收面）
  - `specs/076-agent-v2-optimize/research.md`（D4 两层设计含 guidance 裁定、D5 注入移除面验证、D10 策略去冗事实与裁定）
  - `specs/076-agent-v2-optimize/data-model.md` §1（单元/组合结构表）
  - `specs/076-agent-v2-optimize/quickstart.md` 批1 小节（批次验证场景与通过标准——T006 收口与 Checkpoint 依据）
  - `specs/074-dsh-grpc-runtime/contracts/memory-library.md`（**全文**——T005 的修订对象：§1 原子单元工厂、§2 能力原语、§3 结构化调用面与"两处各持一份"表述）
  - `specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md` §2（`ctx.grpc.client` / `callUnary` 服务面——策略注入来源）
  - **源码基准**：`common/js/memory/src/store.ts`（私有策略副本 :357-431——含 `UnaryInvoke`、`callUnary`（:377-403）、`probeChannel`（:416-431），删除对象；`MemoryServiceCalls`/`WikiServiceCalls` 结构类型）、`common/js/memory/src/face.ts`（wiki 方法在 MemoryFace 上的现状）、`common/js/memory/src/capabilities.ts`（现状组合工厂）、`common/js/memory/src/{guidance,tool,wiki-tools,snapshot,operations,wiki-operations}.ts`、`common/js/memory/src/index.ts`（导出面）、`common/js/dsh-plugins/saolei-memory/src/index.ts`（wiki client 构造 :121-126——删除对象）、`common/js/dsh-plugins/grpc-runtime/src/{client.ts,unary.ts}`（策略实现唯一归属——零改动参照）
  - `specs/069-wiki-template-web/contracts/wiki-service.md` §1（wiki scope = template 半边——`buildWikiUnit` 参数依据）
  - `specs/071-memory-preset-scope/contracts/dsh-plugins.md` §1/§4（memory scope = (template, preset) 全对）
  - `specs/068-wiki-round-pause/data-model.md` §3/§6/§9（wiki 检索语义 / 模型可见工具面 schema / prompt section 常量——`contracts/memory-library-layering.md` §1 义务 4 并列举出的领域语义冻结面，T002/T003 依据）与 `specs/068-wiki-round-pause/contracts/wiki-service.md`（wiki 服务面契约——同一义务 4 并列举出）

- [x] T001 [US2] `common/js/memory/src/store.ts` 拆分与策略注入（FR-006）：① 删除私有 `callUnary`（:377-403）与 `probeChannel`（:416-431）；② 新增导出结构类型 `UnaryPolicy`——与 `ctx.grpc.callUnary`（`common/js/dsh-plugins/grpc-runtime/src/client.ts:47-52`）结构兼容的调用策略函数类型，client 参数以结构最小类型声明（适配器只用到注入 client 本身，不引入 `grpc.Client` 具名依赖亦不使用 `unknown` 参数位——避免 strictFunctionTypes 反变不兼容，具体形参形态实现时以「saolei-memory 可直接传入 `ctx.grpc.callUnary` 且库测试可传 `vi.fn()` 替身」为准）；③ `MemoryClient` / `WikiClient` 构造器在注入 client 外增策略参数，全部 RPC 方法改经注入策略调用（现 label 文案 "memory client call"/"wiki client call" 保持）；④ `MemoryClient` 不再引用 `WikiServiceCalls`、`WikiClient` 不再引用 `MemoryServiceCalls`（store 类型面按服务分离，`MemoryStore extends MemoryStoreRpcs, WikiStore` 合并类型保留给组合层消费）；⑤ 同步更新 `common/js/memory/src/client.test.ts`——策略以 `vi.fn()` 替身注入（替身直通 callback 语义），断言适配器将调用委托给注入策略（正向断言 mock 生效，`style/javascript.md` Mock 规则）；`bazel test //common/js/memory/...` 通过
- [x] T002 [US2] `common/js/memory/src/face.ts` 拆分（依赖 T001 的 store 分离）：① `MemoryFace` 移除 `wikiSearch`/`wikiGet`/`wikiWrite` 三方法（memory face 只服务 memory 操作与快照）；② 新增 `WikiFace`（绑定 `template` 单参 scope；`wikiSearch(query)`/`wikiGet(titles)`/`wikiWrite(input)` 三方法，语义与现 `MemoryFace` 同名方法等价迁移——wiki 集合为模板级共享，`specs/069-wiki-template-web/contracts/wiki-service.md` §1）；③ `createMemoryFace` 收 `(memoryStore, scope)`、新增 `createWikiFace(wikiStore, template)`；④ `wiki-tools.ts` 的执行解析改经 `exec.agent.ctx.wiki`（`WikiFace`），缺面 fail-loud 文案与 memory 工具同型（指名 face 键 `"wiki"` 与注册方）；⑤ 更新 `face.test.ts` / `wiki-tools.test.ts` 断言；`bazel test //common/js/memory/...` 通过
- [x] T003 [US2] 单元层与组合层工厂（依赖 T001/T002）：① 新建 `common/js/memory/src/units.ts`——`buildMemoryUnit({ scope, memoryClient, callUnary })`：faces `[{key:"memory", value: MemoryFace}]`、tools `[memory]` ×1、sections `[memory:snapshot(200)]`（**无 guidance section**——契约 memory-library-layering §1 义务 3）、`ready = face.prefetch()`；`buildWikiUnit({ template, wikiClient, callUnary })`：faces `[{key:"wiki", value: WikiFace}]`、tools 三 wiki 工具、sections `[wiki:guidance(120)]`、`ready` no-op；② `capabilities.ts` 改造为组合层——`buildMemoryWikiComposition({ scope, memoryClient, wikiClient, callUnary })`：两单元合并 + 注入组合层自有分工 section `memory:guidance(110)`（文本 = 现 `MEMORY_GUIDANCE` 原文，归组合层引用）；删除 `buildMemoryCapabilities` 旧名与 `MemoryCapabilitiesDeps`（终态表述，宪法 VII——不留别名导出）；③ `index.ts` 导出面更新（新工厂/`UnaryPolicy`/`WikiFace` 类型；移除旧名）；④ 新增/更新单测（`index.test.ts` 或新 `units.test.ts`）：**单元独立挂载**（各自 faces/tools/sections/ready 可用、工具经对应 face 键解析、单独构造不引用另一服务类型）、**组合等价锚点**（组合产物工具集 ×4、section 名与 order `memory:guidance(110)/wiki:guidance(120)/memory:snapshot(200)`、组装文本与改造前逐项一致——对照 `specs/076-agent-v2-optimize/contracts/memory-library-layering.md` §2 逐项断言）、memory 单元 sections 不含任何 guidance；`bazel test //common/js/memory/...` 通过
- [x] T004 [US2] `common/js/dsh-plugins/saolei-memory/src/index.ts` memory-only 注入（依赖 T003）：① 删除 `WIKI_SERVICE_NAME` 导出与 WikiService client 构造（:121-126）及 `WikiServiceCalls` import；② `memoryMount.build(scope)` 改为产出 memory 单元（`buildMemoryUnit`），组装时把 `ctx.grpc.callUnary` 随 client 一起传入（GrpcClientSeam 接口面相应增加 `callUnary`——结构读取 `ctx.grpc` 的方式与 client 同型）；③ 头注释改述终态（memory-only 适配器 + 策略注入）；④ 更新 `index.test.ts`：断言不构造 wiki client、build 产物为 memory 单元（faces 单键 memory、tools 单工具、sections 仅 snapshot）、策略函数经组装传入；`bazel test //common/js/dsh-plugins/saolei-memory/...` 通过
- [x] T005 [P] [US2] `specs/074-dsh-grpc-runtime/contracts/memory-library.md` 修订（FR-006；依赖 T003 定稿的导出面，可与 T004 并行——纯文档）：① §3 撤销「两处各持一份是依赖方向的必然，非待收敛的重复」表述，改述 unary 策略注入 seam（策略实现唯一归属 grpc-runtime，库经组装 seam 消费，依据 `specs/076-agent-v2-optimize/research.md` D10）；② §1/§2 改述两层导出面（单元工厂/组合工厂/独立挂载/组合等价），细则指向 `specs/076-agent-v2-optimize/contracts/memory-library-layering.md`；③ §4/§5 领域语义与解析面条款核对（wiki 工具解析键改 `"wiki"` 的表述同步）
- [x] T006 [US2] 大型测试验收（依赖 T004，批1 收口）：经 testplan skill 执行 `guitar run projects/game/testplan/system_test.yaml --suite game-system`（`projects/game/testplan/system_test.yaml:38-52`；同一 suite 内覆盖 ① `//projects/game/testplan:wiki_test` 零改动回归——wiki gateway HTTP 面不变；② `//projects/game/testplan:agent_v2_conversation_test` conversation 回归——planner 能力面变化的端到端确认，已核实测试计划/fake-llm 规则无 wiki 工具驱动，预期零断言改动通过）；全部用例通过（宪法 VI：任何失败/flaky 即验收未通过，修复后重跑）

**Checkpoint**: 批1 完成——memory 库两层就绪、策略副本消除、planner 零 wiki 痕迹（SC-002/003/004 达成）；wiki 基建零改动回归全绿。

---

## Phase 4: User Story 3 - announcer 更名与成员化呈现（批2）(Priority: P3)

**Goal**: 播报成员 role `saolei` → `announcer` 全量更名；announcer 作为正式成员进入 GetTeam 名册（三成员）、可为 active_member、GetTeamMember 以与 LLM 成员一致方式服务（无 preset/model 名册形态）；proto 注释修订与契约修订；web 兼容渲染。

**Independent Test**: `bazel test //common/js/dsh-plugins/saolei-loop/... //projects/game/agent_v2/... //projects/game/web/...` 全绿（三成员名册/announcer 成员读取/请求侧校验保持/导出面无 saolei 残留）；conversation 大型测试断言更新后全绿（T013）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）；`style/api.md` 及其引用的 [AIP-122 Resource names](https://google.aip.dev/122)（成员资源名 `members/{role}` 模式）、[AIP-131 Standard methods: Get](https://google.aip.dev/131)（GetTeamMember 语义）、[AIP-156 Singleton resources](https://google.aip.dev/156)（team 单例资源——UpdateTeam/GetTeam 面语义保持）；`style/golang.md`（含其引用的 [Google Go Style: Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)——T013 Go 断言更新）；`style/large_test.md`（T013 执行与组织——按模块追加用例，不按 spec 编号建文件）
  - 未列入说明（原则 V）：`style/javascript.md` 另引用 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（测试执行 shim 退出码/空套件契约）、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（OTel 插桩时序）、`specs/048-js-esm-migration/research.md` R5（CJS 具名导入逐包核实表）与 `specs/019-js-test-reliability/`（js_test 执行模型/mock 根因背景）——本批不触碰测试执行模型、插桩面与 CJS 具名导入核实，与本 phase 任务不相关
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（DI mock 约定依据）
- **技术文章/技术参考文档**：
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（**全文**——`style/javascript.md` 引用的包级 ESM 契约）
  - `specs/076-agent-v2-optimize/contracts/announcer-member.md`（**全文**——更名清单/proto 注释/宿主呈现契约/web 契约/顺序约束/验收面）
  - `specs/076-agent-v2-optimize/research.md` D6（更名面与渲染点验证事实）
  - `specs/076-agent-v2-optimize/data-model.md` §4（announcer 名册项字段值）
  - `specs/076-agent-v2-optimize/quickstart.md` 批2 小节（批次验证场景与通过标准——T013 手工 web 验证与 Checkpoint 依据）
  - `survey/saolei-member-switch-flow.md` 裁定 23（统一呈现的设计依据）
  - `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`（**全文**——T012 修订对象：§3 item 4 / §4 proto 面条款）；`specs/065-agent-v2-team-refine/contracts/team-member-source.md`（**member 契约检查基准**——announcer 实现符合度的核对面）；`specs/065-agent-v2-team-refine/data-model.md` §1.2（role 名与 `SaoleiSystemMember` 表述的同步更名对象——T012 ④）；`specs/065-agent-v2-team-refine/spec.md` FR-001 与背景澄清（role 标签 `saolei` 的更名与 supersession 标注对象——T012 ⑤）
  - `specs/059-agent-v2-team-mode/contracts/team-api.md` 与 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §1（active_member/名册语义——T012 勘误对象）
  - **源码基准**：`common/js/dsh-plugins/saolei-loop/src/announcer.ts`（:26 `SAOLEI_MEMBER_ROLE`、:29 summary、:47 类、:60 id 前缀——更名对象）、`common/js/dsh-plugins/saolei-loop/src/{orchestrator.ts,index.ts}`（导出与注册波及）、`projects/game/agent_v2/src/session.ts`（:880-894 `validateSaoleiMembers` 请求侧校验保持、:996-1021 `memberStateView`/`toTeamView` 名册、:563 订阅投影）、`projects/game/agent_v2/src/history.ts`（成员标签）、`projects/game/agent_v2.proto`（:27-75 服务注释、:284-320 TeamMember 注释、:404/:422/:648/:677 role 词汇注释）、`projects/game/web/frontend/src/App.tsx`（:381/:440 激活指示、:451 名册 map）、`projects/game/web/frontend/src/components/TeamSettingsPanel.tsx`（:48 memberOf、:70-74 preset 下拉、:173 成员列表）、`projects/game/testplan/agent_v2_conversation_test.go` 与 `projects/game/testplan/agent_v2_helpers_test.go`（`agentV2SaoleiMember` wire role 常量与 roster 行——断言更新点）

- [x] T007 [US3] `common/js/dsh-plugins/saolei-loop/src/announcer.ts` 更名（本批基座）：① `SAOLEI_MEMBER_ROLE` → `ANNOUNCER_MEMBER_ROLE`（值 `"announcer"`）、`SAOLEI_MEMBER_SUMMARY` → `ANNOUNCER_MEMBER_SUMMARY`（summary 文本改写为 announcer 语义，如「终局播报对局结果与操作统计」——去除"扫雷系统"场景名歧义）、类 `SaoleiSystemMember` → `AnnouncerMember`、source id 前缀 `` `${session}/saolei` `` → `` `${session}/announcer` ``（:60）；② 同步 `announcer.test.ts` 断言与 `src/index.ts` 导出面（不留旧别名，宪法 VII）；③ 头注释改述终态；④ 合成 model provenance 值（`ANNOUNCE_PROVIDER = "saolei"`、`ANNOUNCE_MODEL = "system"`）**明确不在更名范围**——二者是不路由、不呈现的合成值而非 role 名（FR-201 范围外），仅把注释改述为其非场景含义；`bazel test //common/js/dsh-plugins/saolei-loop/...` 通过
- [x] T008 [P] [US3] `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 更名波及与类型放宽（依赖 T007 常量）：① `TeamRole`（呈现面类型：快照 `member`/`active`/`activation`/`role` 等）扩为 `"player" | "planner" | "announcer"`、`DEFAULT_MEMBER_SUMMARIES` 键与注册摘要随新常量更新；**成员记录键收窄**：`this.members`/`drivingMember` 的 `Record<TeamRole, MemberRuntime>` 改为仅 LLM 成员的联合键（如 `type AgentTeamRole = "player" | "planner"`）——`MemberRuntime` 要求 `AgentHandle`+`Agent`，announce-only 成员无 agent，批2 不引入 announcer 的 `MemberRuntime`（announcer 保持独立 `saolei` 访问器字段，批3 再统一成员化重组）；announcer 注册路径与能力位 `consumes:false` 零行为改动；② `orchestrator.test.ts` 中 saolei 断言更新（role/summary/id）；`bazel test //common/js/dsh-plugins/saolei-loop/...` 通过
- [x] T009 [US3] `projects/game/agent_v2/src/session.ts` 三成员名册与成员读取（依赖 T007/T008/T010——`MemberRole` 定义在 `history.ts`，由 T010 放宽，session.ts 仅 import/re-export）：① 确认 session.ts 的 `MemberRole` re-export 面（:61/:64）随 T010 含 announcer，本文件不重复定义该类型；② `toTeamView`（:1009-1021）名册数组增第三项 announcer——`memberStateView` 读 `entry.members[role]`（键为 LLM 成员）不适用于 announcer，故 announcer 项由 role 直接构造：`name: ${entry.sessionName}/team/members/announcer`、`role: "announcer"`、`preset: ""`、`model: ""`、`systemPrompt: ""`（data-model §4 字段值逐项，字段形状与 `memberStateView` 一致）；③ **GetTeamMember 路径**（现状 :355-395，真实路径 = 硬编码 roster guard → `orchestrator.member(role)`（`AgentHandle`）→ `readMemberSystemPrompt(handle.agent)`）：放宽 guard（`member !== "player" && member !== "planner"` → NOT_FOUND，文案 "the roster is player/planner" 同步改三成员表述），并为 announcer 增加独立分支——announcer 无 `AgentHandle`/`Agent`，不经 `orchestrator.member(role)` 与 `readMemberSystemPrompt`，直接返回与 ② 同形的成员项（output-only `systemPrompt: ""`）；LLM 成员的 generation 校验/重读语义保持；④ `validateSaoleiMembers`（:880-894）**请求侧校验零改动**（仍恰含 player/planner 两 LLM 成员配置，preset 必填不变——announcer 不可由调用方配置）；⑤ 订阅投影（:563）改用 `ANNOUNCER_MEMBER_ROLE`；⑥ `session.test.ts` 增断言：GetTeam 三成员（announcer 空形态）、GetTeamMember("announcer") 可达且 system_prompt 空、UpdateTeam 请求携带 announcer 配置仍被拒；`bazel test //projects/game/agent_v2/...` 通过
- [x] T010 [P] [US3] `projects/game/agent_v2/src/history.ts` 成员标签、`MemberRole` 类型放宽与注释随更名（依赖 T007——不同文件可并行；**T009 依赖本任务**的 `MemberRole` 放宽）：① `MemberRole`（:203）扩为 `"player" | "planner" | "announcer"`——宿主成员角色类型的唯一归属（session.ts 只 import/re-export）；② 成员标签与相关注释中的 saolei 呈现改 announcer（:217-218 非 agent 系统成员注释、:419-425 `appendAnnouncement` 文档）；③ `history.test.ts` 若有 role 断言同步更新；`bazel test //projects/game/agent_v2/...` 通过
- [x] T011 [P] [US3] `projects/game/agent_v2.proto` 注释修订（依赖 T007）：:27-75 服务/方法注释、:284-320 `TeamMember` role 注释，以及 :404/:422/:648/:677 的 role 词汇注释（`TeamMessage.member` / `sender` / 成员事件帧 `member` / `MemberViewEvent.member`）的三成员/announcer 表述（**消息字段与注解零变更**——契约 announcer-member §2）；`bazel build //projects/game/...` 相关 target 通过
- [x] T012 [P] [US3] 契约与 README 修订（依赖 T007–T010 更名面定稿，纯文档）：① `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3 item 4 / §4——「proto 面零改动 / GetTeam 不含 saolei / UpdateTeam 两成员校验不变」条款按统一呈现修订（指向 `specs/076-agent-v2-optimize/contracts/announcer-member.md`）；② `specs/059-agent-v2-team-mode/contracts/team-api.md`、`specs/060-agent-v2-team-optimize/contracts/team-api.md` §1 active_member/名册语义增量勘误（announcer 可为 active_member、三成员名册）；③ `projects/game/agent_v2/README.md` 播报成员表述更名（"非物化的扫雷系统成员 role saolei"段落改述三成员名册终态）；④ `specs/065-agent-v2-team-refine/data-model.md` §1.2 与 `contracts/game-stats-broadcast.md` §3 正文中的 `SaoleiSystemMember`/role `saolei`/`SAOLEI_MEMBER_SUMMARY` 呈现同步更名（终态表述）；⑤ `specs/065-agent-v2-team-refine/spec.md` FR-001 与背景澄清（:29/:152）——role 标签 `saolei` 更名 `announcer`，并标注 supersession：其"不进入 proto `TeamMember` 成员列表与 `active_member`"条款由 `specs/076-agent-v2-optimize/spec.md` FR-202/FR-203 取代（对齐 061 supersession 的 T021 处置口径）；已完成的任务清单记录（`specs/**/tasks.md`）不属契约/spec 终态文档，不做更名改写
- [x] T013 [US3] web 兼容渲染与大型测试验收（依赖 T009）：① `projects/game/web/frontend/src/App.tsx` 名册渲染（:451）与 `TeamSettingsPanel.tsx`（:48/:70-74/:173）确认 announcer 空形态项不报错、preset/model 下拉不受影响（wire 角色串透传路径核对；如需空值呈现微调在此完成）+ 组件测试补充；② `projects/game/testplan/agent_v2_conversation_test.go` 与基座 `projects/game/testplan/agent_v2_helpers_test.go`（`agentV2SaoleiMember` wire role 常量与 roster 行文本 `- [saolei] …`）断言更新：`member="announcer"` 统计消息、`GetTeam` 名册含三成员（**按既有模块文件内追加/调整用例**，`style/large_test.md` 测试组织——不新建按 spec 编号的文件/计划）；③ 经 testplan skill 执行 `guitar run projects/game/testplan/system_test.yaml --suite game-system`（conversation 断言更新后回归）全部用例通过；④ 手工 web 验证（quickstart 批2 场景：名册/激活指示/播报标签 announcer）

**Checkpoint**: 批2 完成——announcer 更名无残留、三成员名册/active_member/成员读取统一呈现（SC-006 达成）；FR-206 顺序约束满足（名册放宽先于批3）。

---

## Phase 5: User Story 1 - team 成员流程与切换标准化（批3）(Priority: P1)

**Goal**: saolei-loop 编排重组为 flow 流程控制（自研 minimal runtime）：F-Moore 活动图（5 活动/8 边）+ `start(activity)` 指令 + outcome 四态/冻结 + LLM 成员适配器下沉（steer/whenIdle/inbox）+ 用户消息投递成员域化（编排 FIFO 移除，061 语义落地）+ 错误契约六码 + cancel/dispose 目标语义 + 宿主联动适配。行为保持基准：`orchestrator.test.ts` 全量 + 两点显式调整。

**Independent Test**: `bazel test //common/js/dsh-plugins/... //projects/game/agent_v2/... //projects/game/web/...` 全绿（行为保持基准 + flow 结构/冻结/投递/错误契约/扩展演示新测试）；conversation/stall/memory_down/disconnect 大型测试全部通过（T022）；SC-001/005/007/008 达成。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）；`style/large_test.md` 与 `style/golang.md`（含其引用的 [Google Go Style: Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)——T022 Go 断言追加）；`style/api.md` 不适用——本批宿主联动为内部行为对齐，proto/服务面零变更（`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §7）
  - 未列入说明（原则 V）：`style/javascript.md` 另引用 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`（测试执行 shim 退出码/空套件契约）、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`（OTel 插桩时序）、`specs/048-js-esm-migration/research.md` R5（CJS 具名导入逐包核实表）与 `specs/019-js-test-reliability/`（js_test 执行模型/mock 根因背景）——本批不触碰测试执行模型、插桩面与 CJS 具名导入核实，与本 phase 任务不相关
- **官方文档**：[Vitest `vi` API](https://vitest.dev/api/vi.html)；[Vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（DI mock 约定依据）；第三方依赖类型面（随包安装的 vendored 权威）：`common/js/dsh-plugins/saolei-loop/node_modules/@deepseek-ai/dsh-agent/lib/types/runtime-types.d.ts`（`steer`/`whenIdle`/`inbox`/`cancel(cause,{keepInbox})`/`inject`/`followup` 语义——成员适配器机械依据）、`common/js/dsh-plugins/llm-glm/node_modules/@deepseek-ai/dsh-llm/lib/types/error.d.ts`（`HarnessError.code` 判别——引擎错误码映射来源）
- **技术文章/技术参考文档**：
  - `specs/048-js-esm-migration/contracts/esm-package-conventions.md`（**全文**——`style/javascript.md` 引用的包级 ESM 契约）
  - `survey/saolei-member-switch-flow.md`（**全文**——裁定 1–24 与 §4 活动指令/§5 flow 层与执行层/§6 用户消息投递/§7 成员类型与接口实现/§8 异常路径/§9 场景映射表/§11 被否决方案——实现语义唯一权威）
  - `specs/076-agent-v2-optimize/contracts/flow-orchestration.md`（**全文**——分层与指令/边表/成员接口/投递/outcome 与冻结/错误契约/操作路径/宿主联动/验收基准）
  - `specs/076-agent-v2-optimize/data-model.md` §2/§3/§5（flow 结构/成员适配器/快照派生）
  - `specs/076-agent-v2-optimize/research.md` D1（自研 runtime 裁定）/D3（dsh-agent 底座 API 验证表）/D9（行为保持基准与两点调整）
  - `specs/061-team-queue-steer/research.md`（**R1–R5 定义处**——在途 steer（R1/R3）、静止编排驱动（R2，被取代）、`snapshot.queued` 与静止判定 inbox pending（R4）、取消清 inbox + landed（R5）；data-model §5 与 flow-orchestration.md §3 的 R 编号依据）+ `specs/061-team-queue-steer/spec.md`（**SC 方向与 FR-001..FR-005**——多消息时序基准调整依据）+ `specs/061-team-queue-steer/contracts/orchestrator-input.md`（在途投递语义沿用：消息形态/进入点/同边界多条/relay 边界/landed flush）+ `specs/061-team-queue-steer/contracts/web-queue-ui.md`（chip 生命周期——T022 实现契约）
  - `specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2（流收束 origin 二分保持）+ `specs/063-llm-reliability-opencode-go/data-model.md` §1（引擎错误码表——映射来源）
  - `specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md` §3（播报/复盘 exactly-once 守卫（对象恒等）与跳局不补发语义保持——FR-110 与 T015/T016 announce/review 活动实现依据）
  - **基线契约**（`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` 头部声明）：`specs/059-agent-v2-team-mode/spec.md` FR-009/FR-010（驱动时机与驱动输入——FR-105 明确保持）、`specs/059-agent-v2-team-mode/data-model.md` §5（编排状态机——切换锚点被 flow 契约取代，读以确认取代边界）、`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2（编排行为）
  - `specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md`（自然停语义保持——rest 活动映射）
  - `specs/068-wiki-round-pause/data-model.md` §8（自然停状态机——survey §6.x 引用处：Counting 不是活动而是 review 的 onSettle、Stopped 即 rest，T016 rest 活动与轮计数语义的实现依据）
  - `specs/062-team-game-end-handoff/spec.md`（concludesTurn 终局收束——idle 可达性上游条件，零改动基线）
  - **源码基准**：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（**全文 1,263 行**——重组对象：状态字段 :488-530、agent 信号订阅 :832-848、runPump :1022-1055、pendingReview 消费 :1042-1049、nextStep :1119-1174、drive :1210-1247、waitForIdle :1249-1262）、`common/js/dsh-plugins/saolei-loop/src/orchestrator.test.ts`（**全文——行为保持基准**，两点调整见 D9）、`common/js/dsh-plugins/saolei-loop/src/announcer.ts`（T007 后形态——announce-only 适配器基底）、`common/js/dsh-plugins/team/src/team.ts`（drain exactly-once 面——零改动核对）、`projects/game/agent_v2/src/session.ts`（watchQuiescence :777-801、send :285-308、toTeamView）、`projects/game/web/frontend/src/store/chat.ts`（:553-555 queued 帧、:622-624 turn_start 出队——061 要退役的路径、:768 turn_end 全清保留）
  - `specs/076-agent-v2-optimize/quickstart.md`（批3 验证场景）

- [ ] T014 [US1] 成员接口与 LLM agent 成员适配器（本批基座）：新建 `common/js/dsh-plugins/saolei-loop/src/member.ts`（`Member` 接口：`drive(input?)` / `deliver(message)`；`Outcome` 四态类型——`settled`/`resting`/`failed{code,detail}`/`aborted`；`LoopErrorCode` 六码类型）与 `common/js/dsh-plugins/saolei-loop/src/agent-member.ts`（player/planner 共用单一实现，机械自 `orchestrator.ts` 下沉）：① 输入构造 `drain`（空 → resting）；② 驱动 `inject(除末条)+followup(末条)` 与三重守卫保持；③ 完成等待 `agent.whenIdle()`（取代编排器 idleWaiters 机械）；④ 失败标记（`agent/error` 订阅先于 idle 读取并清零）→ 错误码映射表（引擎码 → loop 六码，按 data-model §3.2 表逐行实现）；⑤ `deliver` 分流：在途 `agent.steer(message)` / 静止装配 `[drain(自己)..., flushLanded(), message]` 开 turn；⑥ 失败持有输入集重放（`drive(input)` 显式覆盖路径）；⑦ `drive()` 不 throw 契约（内部失败转译 failed）。配套单测（新 `agent-member.test.ts`：DI 替身注入 agent seam——steer/whenIdle/inject/followup 双轨断言、错误码映射表驱动、完成定义含 deliver 未消费消息）；`bazel test //common/js/dsh-plugins/saolei-loop/...` 通过
- [ ] T015 [US1] announce-only 成员适配器（依赖 T014 接口）：`common/js/dsh-plugins/saolei-loop/src/announcer.ts` 增补 `Member` 实现——`drive()` 同步执行 `announce`（恒等守卫 `statsSentFor` 对象判等、时间戳严格递增语义保持）返回 settled、渲染/播报异常 → `failed{INTERNAL}`；`consumes=false`（无 deliver 语义）；`announcer.test.ts` 增补 drive 语义断言；`bazel test //common/js/dsh-plugins/saolei-loop/...` 通过
- [ ] T016 [US1] flow 层（依赖 T014 的类型）：新建 `common/js/dsh-plugins/saolei-loop/src/flow.ts`：① `Activity` 定义面（`id`/`member`/`onSettle?`/`next(facts)`）与 `Facts`（terminal/reviewed/rounds/statsSentFor——对象恒等判据）；② saolei F-Moore 图实例化——5 活动（plan/play/announce/review/rest）+ 8 边逐条落码（边表 = `specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §1，含 review 的 `onSettle: reviewed=terminal, rounds+1` 与 rest 的计数清零/planner 产出保留语义、跳局不补发 guard 对最新记录求值）；③ `FlowState`（position/lastOutcome/facts/frozenError）与决策入口分流（settled→onSettle+边判定 / resting→静止 / failed·aborted→冻结；冻结唤醒后 last-outcome 分流——settled 打断→边判定、failed→重启当前活动）；④ flow 无生命周期（纯状态对象）。配套单测（新 `flow.test.ts`：边表逐边判定、冻结分流、恢复链两次驱动顺序、扩展演示——**取 `survey/saolei-member-switch-flow.md` §7.3「工具/服务成员」扩展位（drive = 调用外部服务）新增一个示例活动/边定义，不改 flow 运行时即被编排**，SC-007）；`bazel test //common/js/dsh-plugins/saolei-loop/...` 通过
- [ ] T017 [US1] 执行层与 orchestrator 重组（依赖 T014–T016）：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 行为保持重构——① `start(activity)` 消费：激活权落地（呈现/日志/驱动权）+ `await member.drive()`（rest 活动不驱动）；② `runPump` 重组：queue gate/case 1 移除（编排 FIFO 消失）、`paused` 短路 → flow 冻结分支、决策入口 = 任何 drive() 闭环返回；③ `submit` → 外壳分发（`flow.activeMember()` 只读接触点 → `member.deliver(message)`）；④ `cancel`（终止在途 `agent.cancel({kind:"user"})` → aborted 冻结、inbox 未消费收编 landed、已冻结 no-op、幂等——**竞态修正**：cancel 打断 review 在途时记录不被消费，取代 :1042-1049 现状消费路径）；⑤ `dispose`（allSettled 释放、流程状态全清、幂等、submit 同步 throw DISPOSED 保持）；⑥ materialize 原子性保持；⑦ `nextStep`/`waitForIdle`/私有 drive 机械拆除（下沉产物消费）；⑧ 快照派生（`phase` ← 活动映射 plan→planning/review→reviewing/play·rest→playing、`active`/`activation`、`paused` ← frozen、`queued` ← inbox pending+steered-unclaimed）；⑨ `common/js/dsh-plugins/team/src/team.ts` drain 消费面核对（本轮不改造的交付面）——团队广播 relay 的 exactly-once 边界与 `specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §2 一致（装配拉动装配），零改动；如发现偏差在同任务内适配。`bazel build //...` 全仓通过、`bazel test //common/js/dsh-plugins/team/...` 通过
- [ ] T018 [US1] 行为保持基准与新增测试（依赖 T017）：① `orchestrator.test.ts` 全量用例通过，两点显式基准调整——多消息时序按 `specs/061-team-queue-steer/spec.md` SC 方向重写 queue-priority 相关断言（mid-turn 进入）；cancel 打断 review 在途竞态按 aborted 冻结语义修正断言（记录不被消费）；② 对照 `survey/saolei-member-switch-flow.md` §9 映射表逐场景核对 S1–S10 覆盖；③ 新增 describe：deliver 在途/静止双轨、冻结恢复链（failed 重启当前活动重放持有集）、错误契约六码上报与宿主透传、aborted 通道；`bazel test //common/js/dsh-plugins/saolei-loop/... //common/js/dsh-plugins/team/...` 通过
- [ ] T019 [US1] 宿主联动适配（依赖 T017，可与 T018 并行）：`projects/game/agent_v2/src/session.ts`：① `watchQuiescence` 静止判定改 `frozen || (active === null && inboxPending === 0)`（冻结短路取代 paused 短路；`specs/061-team-queue-steer/research.md` R4 inbox pending 扩展）；② 流收束 origin 二分保持（member-turn 干净 / orchestration INTERNAL——`specs/063-llm-reliability-opencode-go/contracts/orchestrator-turn-outcome.md` §2）；③ 错误码 → 用户动作文案映射（宿主维护六码文案表，文案取 `survey/saolei-member-switch-flow.md` §8.1 码表的"呈现指导"列；快照/失败帧携带 code 与文案——loop 层不产出文案；**验收判据**：六码各有非空文案，单测断言映射表覆盖全部六码）；④ Send 接入面核对（排队帧语义沿 061 帧）；`session.test.ts` 增断言；`bazel test //projects/game/agent_v2/...` 通过
- [ ] T020 [P] [US1] web 排队 chip 生命周期（依赖 T017 合入后的帧语义，可与 T019 并行）：`projects/game/web/frontend/src/store/chat.ts` 按 `specs/061-team-queue-steer/contracts/web-queue-ui.md` 全文实现——`member_view{sender:"user"}` 文本首匹配移除（新增）、`turn_start` 不再触碰 queue（**退役 :622-624 队首出队**）、`turn_end` 全清保留（:768）、`queued{position}` 追加保留（:553-555）；`store/chat.test.ts` 用例更新（mid-turn 消费消除/回合开始路径/重复文本逐条移除）；`bazel test //projects/game/web/...` 通过
- [ ] T021 [P] [US1] `specs/061-team-queue-steer/spec.md` supersession 标注（纯文档，可与 T019/T020 并行）：头部增补 supersession 注记——静止半边（编排 FIFO，`specs/061-team-queue-steer/research.md` R2）由 `specs/076-agent-v2-optimize/spec.md` FR-105 取代、在途半边（steer→inbox，`specs/061-team-queue-steer/research.md` R1/R3）经成员域 deliver 落地（语义等价引用其 `contracts/orchestrator-input.md`）、web 面契约整体沿用（`contracts/web-queue-ui.md`）；其 plan/tasks 不再单独执行
- [ ] T022 [US1] 大型测试验收（依赖 T018–T020，批3 收口）：① `projects/game/testplan/agent_v2_conversation_test.go` 追加 mid-turn 进入断言（用户在成员回合在途发送的消息于下一 step 边界进入——SC-005，判据：在途连续发送 2 条用户消息、均随下一 step 的工具结果进入该 step 模型输入且成员视角于消费时点实时呈现；按既有模块文件追加用例，`style/large_test.md` 组织规则）；② 经 testplan skill 执行：`guitar run projects/game/testplan/system_test.yaml --suite game-system`（conversation）、`guitar run projects/game/testplan/system_test.yaml --suite game-stall`（流停滞恢复）、`guitar run projects/game/testplan/system_test.yaml --suite game-memory-down`（memory 服务不可达 fail-loud）、`guitar run projects/game/testplan/system_test.yaml --suite game-disconnect`（桌面驱动链路的中途断连/恢复——批3 flow 与 cancel 语义改动的回归面；suite 名行号见 `projects/game/testplan/system_test.yaml:38/53/61/69`）；全部用例通过（宪法 VI）；③ 手工 web 验证（quickstart 批3 场景：切换/排队 chip 消除/取消恢复/自然停）

**Checkpoint**: 批3 完成——flow 标准化兑现（SC-001/005/007/008 达成）；061 supersession 落档。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Goal**: 终态门禁复查（宪法 I/VII/VIII）与全批收口。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`、`style/golang.md`（残留复查涉及双语言；含各自外部风格指南引用——[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)、[Google Go Style: Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）
  - 未列入说明（原则 V）：`style/javascript.md` 另引用 `specs/019-js-test-reliability/contracts/run-vitest-shim.md`、`specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md`、`specs/048-js-esm-migration/research.md` R5 与 `specs/019-js-test-reliability/`——本 phase 只做残留复查与文档终态更新，不触碰测试执行模型、插桩面与 CJS 具名导入核实
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/076-agent-v2-optimize/quickstart.md`（全流程走查）；`specs/076-agent-v2-optimize/contracts/`（三契约终态核对）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（`style/javascript.md` 引用的包级 ESM 契约——残留清理涉及 TS 时的书写依据）

- [ ] T023 交付面 `saolei` role 残留复查（宪法 VII 终态表述）：① **代码面**——`grep -rn "SAOLEI_MEMBER\|SaoleiSystemMember\|/saolei\b" common/ projects/ --include="*.ts" --include="*.go"`（排除 node_modules；排除 saolei 游戏域正当标识——`@dominion/dsh-saolei`/`saoleiTools`/`saolei:game`/`saolei_init` 等插件与玩法名）应为零命中；另显式登记正则不命中的豁免项：`common/js/dsh-plugins/saolei-loop/src/announcer.ts` 的 `ANNOUNCE_PROVIDER = "saolei"` / `ANNOUNCE_MODEL = "system"`（不路由不呈现的合成 model provenance，非 role 名——FR-201 范围外，见 T007 ④）。② **交付文档面**——按文件精确列举：**纳入复查/清理**：`projects/game/agent_v2/README.md`；`specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md`（T012 ①）、`specs/059-agent-v2-team-mode/contracts/team-api.md` 与 `specs/060-agent-v2-team-optimize/contracts/team-api.md` §1（T012 ②）、`specs/065-agent-v2-team-refine/data-model.md` §1.2（T012 ④）。**原则 VII 排除（正当保留旧名、不做改写）**：SDD 规划工件——`specs/**/spec.md`、`specs/**/research.md`、`specs/**/plan.md`、`specs/**/tasks.md`（含 `specs/065-agent-v2-team-refine/{spec,plan,research,tasks}.md`）与 `specs/076-agent-v2-optimize/contracts/`（076 契约已以 `announcer` 表述 role，"原 saolei"是更名对象的叙述）；以及 `survey/saolei-member-switch-flow.md`（设计沿革记录与实施期对照）。依据：原则 VII 约束**交付物**终态，而需求/沿革文档需以旧名叙述被更名对象与裁决依据。③ 发现纳入范围内的残留即清理
- [ ] T024 `projects/game/agent_v2/README.md` 终态更新：编排/成员/能力面描述对齐批1–批3 终态（memory-only 注入、三成员名册、flow 编排概述——只述终态不留演进史）；`specs/076-agent-v2-optimize/quickstart.md` 三批场景全流程走查一遍确认文档与实现一致

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1) / Foundational (Phase 2)**: 无任务——直接进入批1。
- **批1 (Phase 3) → 批2 (Phase 4) → 批3 (Phase 5)**: **串行硬顺序**（用户裁定 D2）：批2 ≤ 批3 为 FR-206 顺序约束（名册放宽先行，否则 flow 重构后 active_member 真实短暂归属 announcer 而名册未承接）；批1 先行为风险递增排序（每批独立过全门禁，中断可恢复）。批1 与批2 代码面无耦合，**如需并行开发**可同时开工，但**验收与合入仍按批序**。
- **Polish (Phase 6)**: 依赖三批全部完成。

### User Story Dependencies

- **US2（批1）**: 无前置——立即可开工（MVP）。
- **US3（批2）**: 与批1 可并行开工（代码面无耦合）；验收与合入序在批3 之前（`ANNOUNCER_MEMBER_*` 常量由本批 T007 引入，批1 不触及 saolei-loop）。
- **US1（批3）**: 依赖批2 合入（FR-206）。

### Within Each Batch

- 基座任务（批1 T001–T003 / 批2 T007 / 批3 T014–T016）先行，波及面任务随后，大型测试验收收口。
- 每任务完成即 `bazel build` + `bazel test` 相关 target（宪法 IV，内嵌非独立任务）。

### Parallel Opportunities

- 批1：T005（纯文档）可与 T004 并行。
- 批2：T008 / T010 / T011 互为不同文件，在 T007 常量就绪后**可并行**；**T009 依赖 T010**（`MemberRole` 由 T010 在 `history.ts` 放宽，T009 不再标 `[P]`）、**T012 依赖 T007–T010**（更名面定稿后收口），二者在 T009/T010 完成后执行。
- 批3：T019 / T020 / T021 互为不同文件，在 T017 合入后**三任务可并行**；T018 与 T019 可并行。

---

## Parallel Example: 批2（Phase 4）

```bash
# T007 常量更名合入后，以下三个任务并行（不同文件、无相互依赖）：
Task: "T008 orchestrator.ts 更名波及与 TeamRole 放宽"
Task: "T010 history.ts MemberRole 放宽 + 成员标签"
Task: "T011 agent_v2.proto 注释修订"

# T010 完成后：T009（依赖 MemberRole 放宽），随后 T012（依赖 T007–T010 定稿）
Task: "T009 session.ts 三成员名册与成员读取"
Task: "T012 契约与 README 修订"
```

---

## Implementation Strategy

### MVP First（批1 = US2）

1. 完成 Phase 3（批1）全部任务并过 T006 大型测试验收——**最小独立交付**：planner 零 wiki 痕迹、库两层就绪、策略副本消除。
2. **STOP and VALIDATE**: quickstart 批1 场景独立验证（SC-002/003/004）。

### Incremental Delivery（批次递进，用户裁定 D2）

1. 批1（US2）→ 独立验收交付（MVP）
2. 批2（US3）→ 独立验收交付（SC-006；解除批3 呈现耦合）
3. 批3（US1）→ 行为保持基准验收交付（SC-001/005/007/008）
4. Phase 6 Polish → 终态门禁复查

### 中断恢复

每批收口即仓库处于可交付态；批次内任务含明确依赖标注，恢复时从未完成的最小依赖闭包继续。

---

## Notes

- [P] 任务 = 不同文件且无未完成依赖；同文件任务串行（宪法 AGENTS.md「对同一文件编辑建议串行」）。
- 大型测试验收任务（T006/T013/T022）必须经 testplan skill 实际执行 `guitar run <plan.yaml>` 完成部署→测试→清理闭环，禁止以 `bazel build` 测试 target 替代（宪法 VI）。执行入口为**测试计划** YAML（`projects/game/testplan/system_test.yaml`，含 `suites`/`cases`），按需以 `--suite <name>` 选择套件；`projects/game/testplan/deploy_agent_v2*.yaml` 是**部署配置**（被 suite 的 `deploy:` 引用，`type: test` + `services`、无 `suites`），不可作为 `guitar run` 参数（`guitar` 会以 "config suites must not be empty" 校验失败；见 `tools/test/guitar/pkg/validate/validate.go:26`）。
- 测试断言更新遵循 `style/large_test.md` 组织规则：按模块归位追加，不为验收场景新建 spec 编号命名的文件或平行测试计划。
- 文档任务（T005/T012/T021/T024）与代码任务同批合入，保证契约与实现同步终态。
