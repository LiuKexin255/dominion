# Tasks: memory 作用域从 session 迁移至 preset、web memory 管理与侧栏二级导航

**Input**: Design documents from `/specs/071-memory-preset-scope/`

**Prerequisites**: plan.md、spec.md（用户故事 US1–US6）、research.md、data-model.md、contracts/、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task）；大型测试（testplan）单独分配任务作为验收环节。各任务完成即要求 `bazel build`/`bazel test`（相关 target）通过。

**Organization**: 按 user story 分 phase（US1 服务端作用域迁移+JS 装配 → US2 跨 session 共享 → US3 边界语义 → US5 导航收纳 → US4 memory 视图 → US6 文案清理）。US2–US3 依赖 US1；US4 与 US5 同为 P2 但共享 `App.tsx`，**US5 先行**（导航结构是 memory 视图的入驻前提，避免临时第四平铺 tab 违反 FR-015）；US6 为前端文案面（P3），独立于服务端变更，其扫描 helper 复用 T019 产出的共享文件（依赖 T019，T019 已在 US4 phase 交付）。JS 装配（T007）归入 US1 phase——服务端 preset 化后 agent_v2 全套件要恢复全绿必须同步随迁（先例：`specs/069-wiki-template-web/tasks.md` T007 同款裁定）。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US6）；Setup/Foundational/Polish phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；Go 服务 `projects/game/memory/`，proto `projects/game/game.proto`，JS 插件 `common/js/dsh-plugins/`，agent_v2 `projects/game/agent_v2/`，web 前端 `projects/game/web/frontend/src/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更，无新包、无新依赖（WebCrypto `crypto.subtle` 为浏览器原生 API，无依赖引入）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: Foundational（proto 契约变更）⚠️ 阻塞全部 US

**Purpose**: 一次 proto 编辑会话完成 Memory 资源与 MemoryService 的 preset 级形态，产出 codegen 供服务端与 JS/web 消费。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/api.md`（REST/grpc 注解规则、Service 注释需含 Prefix Path、AIP 索引）；`style/golang.md`（含 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)；T002 codegen 编译修正）；[AIP-122 Resource names](https://google.aip.dev/122)、[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-132 Standard methods: List](https://google.aip.dev/132)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-158 Pagination](https://google.aip.dev/158)、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)（`google.api.resource` 注解驱动 `ParseMemoryName`/parent 解析 codegen 的插件契约；preset 级子资源先例见本仓库 Preset（parent `templates/{template}`）；两 proto 同包单 codegen 单元见 `projects/game/BUILD.bazel` 头注释）
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/memory-service.md`（§1-2 寻址与四 RPC）、`specs/071-memory-preset-scope/data-model.md`（§1.1/§5 资源形态与 name 总表）、`specs/071-memory-preset-scope/research.md`（R1 资源形态裁定）；基线契约（作用域外条款核对）：`specs/039-planner-memory-calibration/contracts/memory-service-contract.md`（创立契约——CRUD/校验语义零改动基线）

- [X] T001 在 `projects/game/game.proto` 将 Memory 资源与 MemoryService 全面改为 preset 级形态（严格按 `specs/071-memory-preset-scope/contracts/memory-service.md` §1-2）：`Memory` 资源注解 pattern `templates/{template}/presets/{preset}/memories/{memory}`；`CreateMemory` HTTP `POST /api/v1/{parent=templates/*/presets/*}/memories`；`UpdateMemory` `PATCH /api/v1/{memory.name=templates/*/presets/*/memories/*}`；`DeleteMemory` `DELETE /api/v1/{name=templates/*/presets/*/memories/*}`；`ListMemories` `GET /api/v1/{parent=templates/*/presets/*}/memories`；全部请求消息的 parent/name 格式注释终态化（`templates/{template}/presets/{preset}`）；MemoryService 头注释（Prefix Path、作用域表述——planner 长期记忆挂载于物化来源 preset）与 Memory/CreateMemoryRequest/UpdateMemoryRequest 注释中的 039 作用域引用更新为 071 契约引用；`ListMemoriesRequest.order_by` 注释中"作用域键"表述随迁、排序/token 语义注释**原样保留**（`specs/068-wiki-round-pause/contracts/memory-page-token-context.md` 条款不变）；字段集与 `method_signature` 不变
- [X] T002 执行 `bazel run //:gazelle projects/game` 更新生成，`bazel build //projects/game/...` 验证 codegen（`ParseMemoryName` 产物无 SessionID、parent 解析为 PresetName 形态）；`projects/game/memory/handler` 此时如有编译错误（`sessName.SessionID` 等引用），仅做**标识符层面的最小机械修正**（改名/引用对齐使 codegen 可编译），MUST NOT 改动 parent 解析或寻址语义——`ParseSessionName`→`ParsePresetName`、过滤维度等语义改造全部留待 Phase 3 T005，避免与 T005 重复编辑同一路径

**Checkpoint**: codegen 通过；Memory 资源面为 preset 级。

---

## Phase 3: User Story 1 - Memory 资源作用域迁移至 preset（Priority: P1）🎯 MVP

**Goal**: 服务端/存储/JS 工具面/物化装配全部收敛到 (template, preset) 作用域（clean break 落地——旧索引/旧文档清除为新服务首启前的一次性运维动作），preset 级 REST 全场景与既有 agent_v2 套件全绿。

**Independent Test**: quickstart.md 场景 1/3/7——gateway HTTP 完成 preset 级 CRUD/List（409/404/400 全错误面）、JS 单测（scope 寻址/装配传递）、旧作用域数据一次性清理与核验（新服务首启前运维动作）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/golang.md`（含指针/单测规范与 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)）、`style/mongo.md`（库表/对象定义）、`style/api.md`（[AIP-122](https://google.aip.dev/122)、[AIP-132](https://google.aip.dev/132)、[AIP-133](https://google.aip.dev/133)、[AIP-134](https://google.aip.dev/134)、[AIP-135](https://google.aip.dev/135)、[AIP-158](https://google.aip.dev/158)、[AIP-193](https://google.aip.dev/193)）、`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）、`style/large_test.md`（T008）
- **官方文档**：[go.mongodb.org/mongo-driver](https://pkg.go.dev/go.mongodb.org/mongo-driver)（`mongo/options`、`Collection.Indexes().CreateOne`）；[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)（`codes`/`status` 错误映射）；[Vitest](https://vitest.dev/)（T007 JS 单测 `vi.fn` DI doubles）
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/memory-service.md`（全文）、`specs/071-memory-preset-scope/contracts/dsh-plugins.md`（§1-4）、`specs/071-memory-preset-scope/data-model.md`（§1-3/§5）、`specs/071-memory-preset-scope/quickstart.md`（场景 1/3/7）、`specs/071-memory-preset-scope/research.md`（R1-R4/R9-R10）；基线契约（不变条款核对）：`specs/068-wiki-round-pause/contracts/memory-page-token-context.md`（全文——order_by/token 语义零改动基线）、`specs/069-wiki-template-web/contracts/memory-content-limit.md`（§1-2——单条 1024 rune 上限零改动基线）、`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1——排序/游标机制零改动基线）、`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（§2——绑定/fail-loud 语义基线）、`specs/039-planner-memory-calibration/contracts/memory-service-contract.md`（CRUD/校验语义——作用域外零改动基线）；clean break 清理口径见本清单已列的 `specs/071-memory-preset-scope/data-model.md` §2.3 与 `specs/071-memory-preset-scope/quickstart.md` 场景 7（新服务首启前一次性运维动作，非服务代码）；template 级父解析先例：`projects/game/memory/handler/wiki_handler.go`；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）、`projects/game/testplan/system_test.yaml`（case 归属核对——T008/T009）、`specs/059-agent-v2-team-mode/tasks.md`（T023——T007 中 `(T023 wiring fix)` 注释的出处）、`specs/069-wiki-template-web/tasks.md`（T007——JS 面与服务端同批随迁的先例裁定）

- [X] T003 [P] [US1] 在 `projects/game/memory/domain/model.go` 完成领域层作用域收敛：`Memory` 结构 `SessionID` 字段更名 `PresetID string`（bson/注释随迁，scope key = (template, preset)，注释引用 `specs/071-memory-preset-scope/data-model.md` §1.1 终态化——删除 039 session 作用域表述）；`MemoryRepository` 接口全部方法签名的 session 参数更名 preset（`CreateMemory/UpdateMemory/DeleteMemory/ListMemories(ctx, template, preset, ...)`）；`ValidateMemoryID`/`ValidateMemoryContent`/`MaxMemoryContentRunes`/分页常量不变；`domain/model_test.go`/`repository.go` 相关注释与单测同步
- [X] T004 [US1] 在 `projects/game/memory/runtime/mongo/` 完成 memories 仓储 preset 级改造（依赖 T003）：`model.go` 文档结构 `session_id`→`preset_id` 字段（`fieldSessionID`→`fieldPresetID`，doc 注释终态化）；`repository.go`——启动仅创建两个新索引（unique `(template, preset_id, memory_id)`、`(template, preset_id, update_time desc, memory_id asc)`；`memoryIndexModels` 返回 `[]*mongo.IndexModel` 指针数组）、`memoryFilter` 与全部 CRUD/List 查询以 `preset_id` 过滤（seek/游标逻辑零改动），**不含**旧索引 drop 与旧文档 `DeleteMany` 清理代码（一次性清理项不进代码——`specs/071-memory-preset-scope/data-model.md` §2.3，运维清理步骤（新服务首启前执行）见 quickstart.md 场景 7）；`wiki_repository.go` 同批移除其启动旧索引 drop 与旧文档清理代码（同口径，仅保留索引创建）；`repository_test.go` 更新（新索引创建断言、filter 无 session 维度断言、preset 维度 CRUD/游标用例随迁——无清理调用断言）、`wiki_repository_test.go` 移除 legacy 清理断言与清理失败用例
- [X] T005 [US1] 在 `projects/game/memory/handler/handler.go` 完成 MemoryService 作用域收敛（依赖 T004）：`CreateMemory`/`ListMemories` 的 parent 解析改 `game.ParsePresetName(req.GetParent())` + `gameconst.ValidateTemplateName(presetName.Parent())`；`UpdateMemory`/`DeleteMemory` 经再生成的 `ParseMemoryName` 取 `Template`/`PresetID`；`domain.Memory` 装配用 PresetID；错误映射表不变；`handler_test.go` 更新（preset 级 parent/name 构造、同集合重复 id → ALREADY_EXISTS、跨 preset 同 id 各自成功、order_by/token 用例随迁）
- [X] T006 [P] [US1] 在 `projects/game/gateway/cmd/main_test.go` 更新 `TestRootMuxAPIv1SessionMemoryFallback`：memories 路由断言改 preset 形态（`/api/v1/templates/saolei/presets/route-test/memories` → 503 proxy 失败仍成立）；新增断言旧 session 形态路由（`/api/v1/templates/saolei/sessions/x/memories`）→ 404（注册代码零改动，仅测试与注释终态化）
- [X] T007 [P] [US1] JS 工具面与装配作用域随迁（依赖 Phase 2 proto 形态；与 T003-T006 不同语言面可并行）：`common/js/dsh-plugins/memory-service/src/client.ts`——`memoryName`/`parseMemoryName` 构造/解析 `templates/{template}/presets/{preset}/memories/{memoryId}`、`MemoryStore` 接口与实现全部方法 `session` 参数更名 `preset`（parent `templates/{template}/presets/{preset}`）；`operations.ts`——`generateMemoryId` 与 dedupe/batch 语义不变、签名 `session`→`preset`；`service.ts`——`PlannerMemoryScope` 形态 `{template, preset}`、`load` 的 parent 构造与绑定/wiki 转发（仅 `scope.template`）随迁、文件头注释终态化（引用 `specs/071-memory-preset-scope/contracts/dsh-plugins.md`）；`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`——`PlannerMemoryScope` 同型声明随迁、`TeamMaterializeOptions.memoryScope` 注释更新；`projects/game/agent_v2/src/session.ts`——`doMaterialize` 的 `memoryScope` 改 `{ template: parsedSession.template, preset: this.presetId(planner.preset) }`（替换其中 `(T023 wiring fix)` 注释为终态表述——该 wiring 出自 `specs/059-agent-v2-team-mode/tasks.md` T023；scope 两半为业务 template 与物化来源 planner preset id）；全部对应 `*.test.ts` 随迁（scope 形态断言、client parent 寻址断言、装配传递断言）
- [X] T008 [US1] 大型测试修订：`projects/game/testplan/memory_test.go` 全面改为 preset 级路由（helpers 与全部用例：Create `POST /api/v1/templates/{t}/presets/{p}/memories?memory_id=`、PATCH/DELETE `/api/v1/templates/{t}/presets/{p}/memories/{id}`、List 同 parent；409 重复 id、404 不存在、400 非法 id/超限/order_by/token 用例语义不变仅作用域变更——preset 名用合法形态即可，服务端不校验存在性）；确认 `projects/game/testplan/system_test.yaml` case 归属无需拆分；`bazel run //:gazelle projects/game/testplan`（依赖 T005）
- [X] T009 [US1] 大型测试验收（宪法 VI）：随迁两处仍以 sessionID 作 helper preset 实参的既有 agent_v2 夹具（T008 后 helper 已 preset 级路由，实参不换则运行期必红；先例口径同 Organization 节 T007 归入 US1 的裁定——服务端 preset 化后 agent_v2 全套件要恢复全绿必须同步随迁）：`projects/game/testplan/agent_v2_game_test.go` `TestAgentV2TeamGameStatsPromptFaces` 的 12 条快照预写 `createMemory` 实参改传 `plannerPreset` 资源名的 preset id 段（planner 快照预取自物化来源 planner preset 集合，T007 装配），`projects/game/testplan/agent_v2_preset_test.go` `TestAgentV2TeamMemoryReviewPersistsAndSnapshotReloads` 的 `listMemories` 实参改传 `teamMemberPreset(team, "planner")` 资源名的 preset id 段（review 写入经 T007 落 planner preset 集合）——两处仅换集合归属实参、用例断言与语义零改动；随后通过 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（部署→测试→清理闭环），memory 与 agent_v2 全部用例通过（agent_v2 物化/对话用例依赖 T007 装配随迁）；任何失败修复后重跑至全绿（依赖 T007、T008）

**Checkpoint**: preset 级服务端 + 工具面 + 装配可用、REST 全场景与既有套件全绿 = **MVP 交付**。

---

## Phase 4: User Story 2 - 同 preset 物化的 agent 共享 memory 集合（Priority: P1）

**Goal**: 跨 session 持久语义可验证：同 planner preset 两 session 快照共享、模型面知情（guidance 跨局持久措辞）。

**Independent Test**: quickstart.md 场景 2——新跨 session 大型用例 + guidance 组件单测全绿。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）、`style/golang.md`（含 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)；T011 大型测试用例规范）、`style/large_test.md`（测试组织按模块、helper 复用）
- **官方文档**：[Vitest](https://vitest.dev/)（`vi.fn` DI doubles）
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/dsh-plugins.md`（§5 模型面文本、§6 验收锚点）、`specs/071-memory-preset-scope/quickstart.md`（场景 2）、`specs/071-memory-preset-scope/spec.md`（US2 验收场景）；`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（§2 快照 section 基线）；终局回顾落库基线：`specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md`（US2 场景 3）；既有用例参考：`projects/game/testplan/agent_v2_preset_test.go`（memory review/快照用例现状——T011 新增用例的落点；既有用例随迁归属 T009）、`projects/game/testplan/agent_v2_helpers_test.go`（共享 helper）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`

- [X] T010 [P] [US2] 在 `common/js/dsh-plugins/memory/src/guidance.ts` 修订 `MEMORY_GUIDANCE`（分工段，英文）：memory 侧补充跨局持久属性——同 preset 的所有对局共享记忆、错误条目会持续影响后续每一局（persists across games for the same persona）；wiki 侧跨会话持久表述与单条 ~1000 字符体量预期不变；`MEMORY_TOOL_DESCRIPTION` 与 section 名/order 不动；`guidance.test.ts` 增加措辞断言（分工段含跨局持久句、无 session 作用域措辞、工具描述逐字不变）
- [X] T011 [US2] 大型与装配用例（既有 memory 用例的 preset 作用域随迁归属 T009——Phase 3 既有套件全绿的前提，本 task 仅做新增与核对）：`projects/game/testplan/agent_v2_preset_test.go` 新增跨 session 共享用例（按 `specs/071-memory-preset-scope/quickstart.md` 场景 2）：同一 planner preset 在 session A 物化写入 memory → session B 以同 preset 物化 → `GetTeamMember`（planner）`system_prompt` 的 memory 快照段命中该条目；对照组：换另一 planner preset 物化 → 快照为空；`projects/game/agent_v2/src/session.test.ts` 补装配断言（`memoryScope` 两半 = parsedSession.template 与 planner preset id）；终局回顾落库（US2 场景 3）以 T009 随迁后的既有用例 `TestAgentV2TeamMemoryReviewPersistsAndSnapshotReloads` 为覆盖载体（review 写入断言落在物化来源 planner preset 集合、refresh 后快照重载），核对其在 preset 作用域下成立即可，无需另立重复新用例（依赖 T007、T009）
- [X] T012 [US2] 大型测试验收（宪法 VI）：testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`，跨 session 共享用例与全部既有用例通过，失败修复后重跑至全绿（依赖 T010、T011）

**Checkpoint**: 跨 session 持久语义端到端可验证（SC-001/SC-004）。

---

## Phase 5: User Story 3 - 作用域隔离与生命周期边界（Priority: P2）

**Goal**: 隔离、刷新换 preset、session/preset 删除边界全部有用例锁定。

**Independent Test**: quickstart.md 场景 1/2——隔离与生命周期 REST 用例、刷新换 preset 用例全绿。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/golang.md`（含 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)）、`style/large_test.md`（测试组织按模块）、`style/javascript.md`（含 [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；T014 `session.test.ts` 装配断言）
- **官方文档**：[Vitest](https://vitest.dev/)（T014 `session.test.ts` 的 `vi.fn` 装配断言）
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/memory-service.md`（§3 生命周期与边界）、`specs/071-memory-preset-scope/contracts/dsh-plugins.md`（§4 物化装配——T014 refresh 装配）、`specs/071-memory-preset-scope/spec.md`（US3 验收场景 1-4）、`specs/071-memory-preset-scope/quickstart.md`（场景 1/2）；`projects/game/testplan/memory_test.go`（T008 改造后形态——本 phase 增例落点）、`projects/game/testplan/agent_v2_preset_test.go`（refresh 用例现状）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）

- [ ] T013 [US3] 在 `projects/game/testplan/memory_test.go` 增加隔离与生命周期用例：跨 preset 零泄漏（P1 建条目后 P2 List 不含）；session 删除不影响集合（建 session → 以 preset 写入 → DeleteSession（v1）→ List 仍在）；preset 删除不级联（v2 `DELETE /api/v2/templates/{t}/presets/{p}` → v1 List memories 仍在）；无容量上限（FR-018）：写入超过单页 `page_size` 的条数后 List 分页可完整取回，无淘汰、无超限拒绝；每页大小/断言风格沿用既有 helpers
- [ ] T014 [US3] 刷新换 preset 用例：`projects/game/testplan/agent_v2_preset_test.go` 增例——已物化 team（planner=P1）经 UpdateTeam 刷新换 planner=P2 → planner `system_prompt` 快照来自 P2 集合、P1 集合数据不受影响（可用 v1 REST 前置写入两集合各一条对照断言）；`projects/game/agent_v2/src/session.test.ts` 补刷新装配断言（依赖 T007）
- [ ] T015 [US3] 大型测试验收（宪法 VI）：testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`，本 phase 用例与全部既有用例通过（依赖 T013、T014）

**Checkpoint**: 边界语义全部锁定（SC-002）。

---

## Phase 6: User Story 5 - 侧栏两级导航（浮出菜单）（Priority: P2，先于 US4 执行）

**Goal**: 侧栏一级收敛为 sessions/presets + 知识入口，wiki 经浮出菜单可达，底部行数恒定。

**Independent Test**: quickstart.md 场景 5——App.test.tsx 两级结构/浮层行为/互斥渲染断言全绿（无服务端依赖）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[React 官方文档](https://react.dev/learn)（组件/state）；[Vitest](https://vitest.dev/)、[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/web-nav.md`（全文——结构/行为/状态/样式/验收锚点）、`specs/071-memory-preset-scope/data-model.md`（§4 App 导航状态）、`specs/071-memory-preset-scope/spec.md`（US5 验收场景 1-5）；`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md`（§2 侧栏视图切换基线）；既有代码参考：`projects/game/web/frontend/src/App.tsx`（view-switch 现状）、`projects/game/web/frontend/src/App.test.tsx`（视图切换断言基线）、`projects/game/web/frontend/src/theme.css`（presets-* 管理视图样式形态）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`

- [ ] T016 [P] [US5] 在 `projects/game/web/frontend/src/theme.css` 补充浮出菜单样式（浮层定位/层级/菜单项形态，沿用 dsh-theme 变量与管理视图形态；`data-testid` 命名按 `contracts/web-nav.md` §4）
- [ ] T017 [US5] 在 `projects/game/web/frontend/src/App.tsx` 改造侧栏视图导航为两级（依赖 T016；严格按 `specs/071-memory-preset-scope/contracts/web-nav.md` §1-3）：一级 = sessions/presets 直达按钮 + 固定知识入口按钮（wiki/memory 收纳、**本 task 仅挂 wiki 项**，memory 项由 US4 接入）；浮层 `knowledgeMenuOpen` 瞬态（toggle 开关、点选切换视图并关闭、点浮层外部仅关闭不改视图、Esc 关闭）；入口按钮呈现当前选中视图；`AppView` 保持既有 `'wiki'` 值、新增 `'memory'`；主区互斥渲染与会话面板常驻挂载语义不变；`App.test.tsx` 更新（一级仅 2 直达 + 1 入口、浮层开关/外点关闭/选中态保持、互斥渲染、既有直达零回归——`contracts/web-nav.md` §5 锚点 1/2/3）

**Checkpoint**: 导航两级化（wiki 入口迁移完成），侧栏底部行数恒定。

---

## Phase 7: User Story 4 - web memory 管理视图（Priority: P2）

**Goal**: preset 选择 + 分页列表/查看/创建/编辑/二步删除闭环，错误可见、文案中性。

**Independent Test**: quickstart.md 场景 4/6——`api/memory.test.ts` + `MemoryView.test.tsx` + `App.test.tsx` 全绿；REST 闭环已由 Phase 3-5 大型测试覆盖。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[MDN: SubtleCrypto.digest()](https://developer.mozilla.org/en-US/docs/Web/API/SubtleCrypto/digest)（T018 摘要 id 派生——`crypto.subtle.digest('SHA-256')` 返回 ArrayBuffer → hex 编码）；[React 官方文档](https://react.dev/learn)；[Vitest](https://vitest.dev/)、[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/contracts/web-memory-view.md`（全文）、`specs/069-wiki-template-web/contracts/web-wiki-view.md`（视图组织/交互语汇/API 客户端形态模式先例）、`specs/071-memory-preset-scope/contracts/memory-service.md`（§2 REST 面）、`specs/071-memory-preset-scope/data-model.md`（§4 视图状态模型）、`specs/071-memory-preset-scope/spec.md`（US4 验收场景 1-5、FR-016）；`specs/054-agent-v2-bugfixes/contracts/web-ui.md`（§6 独占编辑表单/二步删除/错误呈现语汇基线）；既有代码参考：`projects/game/web/frontend/src/api/wiki.ts`（REST 客户端/protojson 投影/mask query 先例）、`projects/game/web/frontend/src/api/wiki.test.ts`（fetch mock 断言基线）、`projects/game/web/frontend/src/components/WikiView.tsx`（管理视图形态先例）、`projects/game/web/frontend/src/components/PresetsView.test.tsx`（组件测试风格基线）、`projects/game/web/frontend/src/api/agent.ts`（`listPresets` 复用面）、`common/js/dsh-plugins/memory-service/src/operations.ts`（`generateMemoryId` 摘要值域——T018 web 派生 id 的对齐依据）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`

- [ ] T018 [P] [US4] 新增 `projects/game/web/frontend/src/api/memory.ts` 与 `api/memory.test.ts`：四方法（`listMemories(template, preset, {pageSize?, pageToken?, orderBy?})`、`createMemory(template, preset, content)`、`updateMemory(name, content)`、`deleteMemory(name)`）——相对路径 `/api/v1/templates/{t}/presets/{p}/memories`、`requestJson`/`ApiError` 复用、PATCH 经 `?update_mask=content`；`createMemory` 以 `crypto.subtle.digest('SHA-256', utf8(content))` → hex → `slice(0, 32)` 派生 memory_id（与 `common/js/dsh-plugins/memory-service/src/operations.ts` `generateMemoryId` 同值域——`contracts/web-memory-view.md` §2）；protojson camelCase 投影（`Memory`/`MemoryPage` 类型按 data-model §4）；测试为 fetch mock 请求形状断言 + 摘要派生断言（已知输入向量对照 node 路径摘要值）
- [ ] T019 [US4] 新增 `projects/game/web/frontend/src/components/MemoryView.tsx` 与 `MemoryView.test.tsx`（依赖 T018；严格按 `specs/071-memory-preset-scope/contracts/web-memory-view.md` §3-4）：preset 选择器（`listPresets(template)` 全角色、切换重置列表/分页/表单态）、分页列表（默认 `orderBy="update_time desc"`、`pageSize=20`，行 = content + updateTime）、独占创建表单（仅 content，id 派生不暴露）、独占编辑表单（mask content）、inline 二步删除、loading/error/空态（空态中性文案 + 创建入口）、错误呈现（409 已存在/404/400 超限）；组件测试覆盖选择/列表序/创建成功与重复内容 409 提示/编辑保存/二步删除/空态错误态/**场景词扫描断言**（渲染输出不含 planner/player/saolei/扫雷——`contracts/web-memory-view.md` §5 锚点 2；扫描 helper 提取为共享文件 `projects/game/web/frontend/src/scene-words.ts` 供 T021 复用，禁止在各测试文件重复定义）
- [ ] T020 [US4] 在 `projects/game/web/frontend/src/App.tsx` 接入 memory 视图（依赖 T017、T019）：`AppView` 落定 `'memory'` 值、浮层菜单增加 memory 项（`<MemoryView template={TEMPLATE_SAOLEI} />` 主区条件渲染）；`App.test.tsx` 增加浮层内 memory 切换断言

**Checkpoint**: web memory 管理闭环组件级可测（vitest 全绿）。

---

## Phase 8: User Story 6 - wiki/memory 管理页面文案场景无关化（Priority: P3）

**Goal**: wiki 视图既有两处场景化文案修正，两视图文案零场景词有断言锁定。

**Independent Test**: quickstart.md 场景 6——WikiView.test.tsx 文案断言全绿；preset 视图/对话页场景数据呈现零改动。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`（[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)）
- **官方文档**：[Vitest](https://vitest.dev/)、[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/spec.md`（FR-016/FR-017、US6 验收场景 1-3）、`specs/071-memory-preset-scope/contracts/web-memory-view.md`（§4 文案约束）；既有代码参考：`projects/game/web/frontend/src/components/WikiView.tsx`（L404 空态 "…planner 也能检索到"、L311 占位 "如 开局-左上角" 两处修正点）、`projects/game/web/frontend/src/components/WikiView.test.tsx`（断言落点）；`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`

- [ ] T021 [US6] 在 `projects/game/web/frontend/src/components/WikiView.tsx` 修正两处场景化文案：空态句改为中性表述（消费方以"agent/助手"指代，如"新建一条把知识沉淀下来，agent 也能检索到"）、创建表单 title 占位示例改为中性内容（去扫雷局面示例）；`WikiView.test.tsx` 增加断言——两处文案中性化 + **场景词扫描断言**（复用 T019 产出的共享 helper `projects/game/web/frontend/src/scene-words.ts`，覆盖 WikiView 全部用户可见文案）；确认 `PresetsView`/对话页场景词汇数据呈现零改动（FR-017，不触碰）

**Checkpoint**: 基础能力呈现零场景污染（SC-007）。

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: 文档终态、全仓回归与最终验收。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/large_test.md`（T024 大型测试执行前必读：测试计划执行、SUT/环境变量、测试组织与反模式）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/071-memory-preset-scope/quickstart.md`（全文——回归核对清单）、`projects/game/agent_v2/README.md`（L58-61 `/api/v1/` 路由描述 + L295 `scope 键 (template, session)`——两处 session 作用域表述现状）

- [ ] T022 [P] 在 `projects/game/agent_v2/README.md` 将 memory 服务描述终态化（preset 级路由 `/api/v1/.../presets/.../memories`、planner 长期记忆挂载于物化来源 preset 的表述）：覆盖 L58-61 的 `/api/v1/` 路由描述**与 L295 的 `scope 键 (template, session)`**；全文扫描并清除 session 作用域旧表述（终态原则 VII）
- [ ] T023 全仓回归：`bazel test //projects/game/... //common/js/dsh-plugins/...`（含未直接改动面的零回归验证——session 服务、wiki 面、preset 面、team 对话面；并显式核对 FR-013：player 角色仍禁挂 `@dominion/dsh-memory`（agent_v2 templateRules 未变）、wiki 工具保持 template 级作用域——由既有 templateRules/scope 测试覆盖并通过）
- [ ] T024 最终全量大型测试验收（宪法 VI）：testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（含 game-system/game-disconnect/game-memory-down/game-stall 全 suite），全部用例通过；按 `specs/071-memory-preset-scope/quickstart.md` 逐场景核对。场景 4/5 的浏览器手工 walkthrough 为**人工抽查项**、不纳入自动验收（web 无浏览器执行面；UI 逻辑由 vitest 组件测试覆盖、REST 闭环由本 suite 的 `memory_test.go` 覆盖——豁免口径见 plan.md Constitution Check 门禁 5，对齐 `specs/069-wiki-template-web/research.md` R8 裁断）；任何失败修复后重跑至全绿

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务，直接开始
- **Phase 2（Foundational）**: proto 契约——**阻塞全部 US**（服务端/JS/web 都消费 codegen 与注解形态）
- **Phase 3（US1）**: 依赖 Phase 2；T003→T004→T005 串行（domain→repo→handler），T006/T007 可与 T003-T005 并行（不同文件）；T008 依赖 T005；T009 收口依赖 T007+T008，并含 `agent_v2_game_test.go`/`agent_v2_preset_test.go` 两处既有夹具的 preset 实参随迁（MVP「既有套件全绿」前提）
- **Phase 4（US2）**: 依赖 Phase 3（T011 依赖 T007 装配与 T009 的既有用例随迁产物）；T010 与 T011 可并行
- **Phase 5（US3）**: 依赖 Phase 3；T013/T014 可并行（不同测试文件）；与 Phase 4 可并行（无文件交集——T014 与 T011 同文件 `agent_v2_preset_test.go`，若并行须串行该文件编辑）
- **Phase 6（US5）**: 仅依赖 Phase 1；与 Phase 3-5 完全并行（纯前端）
- **Phase 7（US4）**: T018 与 Phase 6 并行可行；T020 依赖 T017+T019
- **Phase 8（US6）**: 仅 WikiView 文案与测试，独立于服务端变更；T021 复用 T019 产出的共享扫描 helper（依赖 T019，T019 已在 Phase 7 交付）
- **Phase 9（Polish）**: 依赖全部 US 完成

### User Story Dependencies

- **US1（P1）**: Phase 2 后即可开始——MVP 基座
- **US2（P1）**: 依赖 US1 的作用域终态（T007/T009）
- **US3（P2）**: 依赖 US1；与 US2 可交错
- **US5（P2）**: 独立（纯前端导航）；**先于 US4**（`App.tsx` 同文件，导航结构为 memory 入驻前提）
- **US4（P2）**: 依赖 US5（T020）与 Phase 2（REST 形态）；T018/T019 可提前并行
- **US6（P3）**: 独立于服务端变更；扫描 helper 复用 T019 产出的共享文件 `projects/game/web/frontend/src/scene-words.ts`（依赖 T019，T019 已在 Phase 7 交付）

### Parallel Opportunities

- Phase 2 完成后：US1 的 Go 面（T003-T006）、JS 面（T007）、前端导航（T016-T017）、memory API 客户端（T018）、文案清理可按文件分派并行
- T010（guidance）与 T011（testplan）不同文件可并行
- T013（memory_test.go）与 T014（agent_v2_preset_test.go + session.test.ts）不同文件可并行

---

## Parallel Example: Phase 3（US1）

```text
并行轨道 A（Go 服务端）: T003 → T004 → T005 → T008
并行轨道 B（JS 面）:     T007
并行轨道 C（gateway 测试）: T006
收口: T009（大型测试验收，等 A+B 完成）
```

---

## Implementation Strategy

### MVP First（US1 Only）

1. Phase 2（proto + codegen）
2. Phase 3（US1 服务端 + JS 装配 + REST 大型测试全绿）
3. **STOP and VALIDATE**: preset 级 REST 全场景 + agent_v2 既有套件全绿 = MVP（跨 session 能力已在服务端就绪，agent 面由 T007 装配）

### Incremental Delivery

1. MVP（US1）→ 2. US2（跨 session 验证 + 模型面知情）→ 3. US3（边界锁定）→ 4. US5（导航）→ 5. US4（memory 视图）→ 6. US6（文案清理）→ 7. Polish（回归 + 最终验收）

每步交付独立可验证，前序全绿后推进。

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖；同文件任务（如 `App.tsx` 的 T017/T020、`agent_v2_preset_test.go` 的 T011/T014）必须串行
- 每任务完成即 `bazel build`/`bazel test`（相关 target）通过（宪法 IV，不单列）
- 大型测试验收任务（T009/T012/T015/T024）必须经 testplan skill 实际执行 `guitar run`，禁止以构建检查替代（宪法 VI）
- T009/T012/T015 为各 user story 的服务端验收门禁（宪法 VI），T024 为最终全量验收——均执行完整 `system_test.yaml`；同一套件被多次实际执行是**有意为之**（每个 story checkpoint 需独立可验证、可交付），非遗漏
- 停在任一 checkpoint 均可独立验证该 story
