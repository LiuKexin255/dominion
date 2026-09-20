# Tasks: wiki 提升为 template 级知识库、memory 单条长度上限与 web wiki 管理

**Input**: Design documents from `/specs/069-wiki-template-web/`

**Prerequisites**: plan.md、spec.md（用户故事 US1–US3）、research.md、data-model.md、contracts/、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task）；大型测试（testplan）单独分配任务作为验收环节。各任务完成即要求 `bazel build`/`bazel test`（相关 target）通过。

**Organization**: 按 user story 分 phase（US1 wiki 作用域提升 → US2 web 管理视图 → US3 memory 上限），每个 story 独立可测。US3 与 US1/US2 无文件交集，可在 Phase 2 后并行。US2 依赖 US1 的 REST 面（运行时验证），编码面仅依赖 Phase 2 的 proto 形态。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US3）；Setup/Foundational/Polish phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；Go 服务 `projects/game/memory/`，JS 插件 `common/js/dsh-plugins/`，web 前端 `projects/game/web/frontend/src/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更，无新包、无新依赖（web 前端 react/vitest 与插件面 node:crypto 均为既有依赖）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: Foundational（proto 契约变更）⚠️ 阻塞 US1/US2

**Purpose**: 一次 proto 编辑会话完成 Wiki template 级形态与 Memory 上限注释，产出 codegen 供服务端与 JS/web 消费。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/api.md`（REST/grpc 注解规则与 AIP 索引）；[AIP-121 Resource-oriented design](https://google.aip.dev/121)、[AIP-122 Resource names](https://google.aip.dev/122)、[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-132 Standard methods: List](https://google.aip.dev/132)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-158 Pagination](https://google.aip.dev/158)、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)（`google.api.resource` 注解驱动 `ParseWikiName`/parent 解析 codegen 的插件契约；template 级子资源先例见本仓库 session（parent `templates/{template}`）与 Preset（`templates/{template}/presets/{preset}`））
- **技术文章/技术参考文档**：`specs/069-wiki-template-web/contracts/wiki-service.md`（§1-3）、`specs/069-wiki-template-web/contracts/memory-content-limit.md`（§1-2）、`specs/069-wiki-template-web/data-model.md`（§1）、`specs/069-wiki-template-web/research.md`（R1/R3）

- [X] T001 在 `projects/game/game.proto` 将 Wiki 资源与 WikiService 全面改为 template 级形态（严格按 `specs/069-wiki-template-web/contracts/wiki-service.md` §1-2）：`Wiki` 资源注解 pattern `templates/{template}/wikis/{wiki}`；`CreateWikiRequest.parent` 注解 `{parent=templates/*}`（HTTP `POST /api/v1/{parent=templates/*}/wikis`）；Get/Update/Delete 的 name 模式 `templates/*/wikis/*`（Update 保持既有 `{wiki.name=...}` HTTP 绑定，仅路径模板去 session 段）；List/SearchWikis 的 parent `templates/*`（`GET /api/v1/{parent=templates/*}/wikis` 与 `.../wikis:search`）；全部请求消息的 parent/name 格式注释终态化（`templates/{template}/wikis/{wiki}`）；服务头注释（Prefix Path、作用域表述）同步更新；六 RPC 字段集与语义注释不变（校验/唯一性/分页/排序描述原样保留）
- [X] T002 在 `projects/game/game.proto` 为 `Memory.content` 字段及其 Create/Update 请求注释补充长度上限说明：content ≤ 1024 rune（超限 INVALID_ARGUMENT），引用 `specs/069-wiki-template-web/contracts/memory-content-limit.md`（同文件连续编辑，承接 T001）
- [X] T003 执行 `bazel run //:gazelle projects/game` 更新生成，`bazel build //projects/game/...` 验证 codegen（`ParseWikiName` 产物的 `WikiName` 无 SessionID 字段、parent 解析为 `TemplateName`）；`projects/game/memory/handler` 此时如有编译错误（`sessName.SessionID` 等引用），仅做最小机械修正，语义改造留给 Phase 3

**Checkpoint**: codegen 通过；Wiki 资源面为 template 级；Memory 上限进入 proto 注释。

---

## Phase 3: User Story 1 - wiki 作用域提升为 template 级（Priority: P1）🎯 MVP

**Goal**: Wiki 资源/存储/服务端/工具面全部收敛到 template 作用域（clean break 落地），跨 session 知识共享可用，testplan 大型测试全绿。

**Independent Test**: quickstart.md 场景 1/3/5——gateway HTTP 完成 template 级 CRUD/List/Search 全场景、JS 工具面跨 session 语义单测、启动清理落地。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/golang.md`、`style/mongo.md`、`style/api.md`、`style/large_test.md`、`style/javascript.md`；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)、[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[AIP-121 Resource-oriented design](https://google.aip.dev/121)、[AIP-122 Resource names](https://google.aip.dev/122)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-132 Standard methods: List](https://google.aip.dev/132)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-158 Pagination](https://google.aip.dev/158)、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：[go.mongodb.org/mongo-driver](https://pkg.go.dev/go.mongodb.org/mongo-driver)（`mongo/options`、bson、`Collection.Indexes().DropOne/CreateOne`、`DeleteMany`）；[MongoDB: $all query operator](https://www.mongodb.com/docs/manual/reference/operator/query/all/)、[MongoDB: Multikey Indexes](https://www.mongodb.com/docs/manual/core/index-multikey/)、[MongoDB: db.collection.dropIndex()](https://www.mongodb.com/docs/manual/reference/method/db.collection.dropIndex/)（旧索引默认命名 `field_1_...` 规则）；[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)（`codes`/`status` 错误映射）；[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)
- **技术文章/技术参考文档**：`specs/069-wiki-template-web/contracts/wiki-service.md`（全文）、`specs/069-wiki-template-web/contracts/dsh-plugins.md`（§2-3、§6）、`specs/069-wiki-template-web/data-model.md`（§1.1/§2/§3）、`specs/069-wiki-template-web/quickstart.md`（场景 1/3/5）、`specs/069-wiki-template-web/research.md`（R1-R4）、`survey/game-wiki-knowledge-base.md`（§4-5 检索与 API 基线）、`specs/068-wiki-round-pause/data-model.md`（§2 存储/索引基线——对照终态差异）、`specs/068-wiki-round-pause/contracts/wiki-service.md`（全文——被本特性取代的语义基线，支撑"六 RPC 语义不变"的零回归核对）、`specs/068-wiki-round-pause/contracts/dsh-plugins.md`（§4 服务面基线——WikiStore/操作核签名）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则与包级契约）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）、`projects/game/testplan/system_test.yaml`（既有 case 归属核对——T008）

- [X] T004 [P] [US1] 在 `projects/game/memory/domain/` 完成 wiki 领域层作用域收敛：`wiki.go` 的 `Wiki` 结构删除 `SessionID` 字段、作用域注释终态化（scope key = template；引用 `specs/069-wiki-template-web/data-model.md` §1.1）；`wiki_repository.go` 接口全部方法签名去 session 维度（`CreateWiki/GetWiki/UpdateWiki/DeleteWiki/ListWikis/SearchWikis(ctx, template, ...)`）；`wiki.go`/`wiki_test.go`/`wiki_repository.go` 相关注释与单测同步（校验函数 `ValidateWiki*` 与常量不变）
- [X] T005 [US1] 在 `projects/game/memory/runtime/mongo/` 完成 wikis 仓储 template 级改造（依赖 T004）：`wiki_model.go` 文档结构去 `session_id` 字段；`wiki_repository.go`——启动时幂等清理（`Indexes().DropOne` 旧索引 `template_1_session_id_1_wiki_id_1`、`template_1_session_id_1_title_norm_1`、`template_1_session_id_1_title_grams_1` + `DeleteMany(bson.M{"session_id": bson.M{"$exists": true}})` 清除旧作用域文档——`specs/069-wiki-template-web/data-model.md` §2.3）、创建三个新索引（unique `(template, wiki_id)`、unique `(template, title_norm)`、`(template, title_grams)` multikey）、全部 CRUD/List/Search 查询去 session 维度（`$all` 预过滤与 <2 rune regex 回退逻辑不变）；`wiki_repository_test.go` fake collection 单测更新（新索引创建断言、清理调用断言、filter 无 session 维度断言）
- [X] T006 [US1] 在 `projects/game/memory/handler/wiki_handler.go` 完成六 RPC 作用域收敛（依赖 T005）：Create/List/Search 的 parent 解析改 `game.ParseTemplateName(req.GetParent())` + `gameconst.ValidateTemplateName`；Get/Update/Delete 的 template 取值改 `name.Parent()` 一跳（原 `name.Parent().Parent()`）；`domain.Wiki` 装配去 SessionID；`wiki_handler_test.go` 更新（template 级 parent/name 构造、作用域内 title 唯一冲突 → ALREADY_EXISTS 用例、错误映射表不变）
- [X] T007 [P] [US1] 在 `common/js/dsh-plugins/memory-service/src/` 完成 JS 工具面作用域收敛（依赖 Phase 2 的 proto 形态；与 T004-T006 不同语言面可并行）：`wiki-client.ts`——`wikiName`/`parseWikiName` 构造 `templates/{template}/wikis/{wikiId}`、`WikiStore` 接口与实现全部方法去 session 参数；`wiki-operations.ts`——`wikiSearch/wikiGet/wikiWrite(store, template, ...)` 签名收敛（预检/提交两段式、`sha256(title_norm)[:32]` id 派生、128 rune/64KB 预检常量不变）；`service.ts`——三个 wiki 方法仅转发 `scope.template`（`load` 的 `(template, session)` 绑定与 fail-loud 语义不变）；`wiki-client.test.ts`/`wiki-operations.test.ts`/`service.test.ts` 更新——含跨 session 语义用例（同 template 不同 session 的两个 scope 驱动 `wikiWrite`/`wikiSearch`，断言 fake store 收到相同 template 寻址、无 session 维度——`specs/069-wiki-template-web/contracts/dsh-plugins.md` §6）
- [X] T008 [US1] 大型测试修订：`projects/game/testplan/wiki_test.go` 全面改为 template 级路由（helpers 与全部用例：Create `POST /api/v1/templates/{t}/wikis?wiki_id=`、Get/PATCH/DELETE `/api/v1/templates/{t}/wikis/{id}`、List/Search 同 parent；title 唯一冲突 409、List 固定序 + 分页、Search 中文/大小写/短查询/续页一致性、空/超限 400——用例语义不变仅作用域变更）；确认 `projects/game/testplan/system_test.yaml` 既有 case 归属无需拆分；`bazel run //:gazelle projects/game/testplan`（依赖 T006）
- [X] T009 [US1] 大型测试验收（宪法 VI）：通过 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml`（部署→测试→清理闭环），wiki 与 memory 全部用例通过；任何失败修复后重跑至全绿（依赖 T008）

**Checkpoint**: template 级 wiki 服务端 + 工具面可用、跨 session 共享语义单测通过、大型测试全绿 = **MVP 交付**。

---

## Phase 4: User Story 2 - web wiki 管理视图（Priority: P1）

**Goal**: web 侧栏第三视图完成 wiki 的搜索、分页列表、创建、编辑、删除闭环，错误可见、零 CORS。

**Independent Test**: quickstart.md 场景 4——vitest 组件/API 客户端测试全绿（`api/wiki.test.ts` + `WikiView.test.tsx` + `App.test.tsx`）即为 phase 验收标准；部署后浏览器 walkthrough 属 quickstart 指引性人工确认，不作为验收门禁（web 无浏览器执行面——research.md R8；运行时依赖 US1 部署面）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[React 官方文档](https://react.dev/learn)（组件/state/effect）；[Vitest](https://vitest.dev/)（`vi.fn`/`vi.stubGlobal` 测试 doubles）；[Testing Library: React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)（组件查询与断言）
- **技术文章/技术参考文档**：`specs/069-wiki-template-web/contracts/web-wiki-view.md`（全文）、`specs/069-wiki-template-web/contracts/wiki-service.md`（§2 REST 面）、`specs/069-wiki-template-web/data-model.md`（§4 protojson 投影与 id 派生）、`specs/069-wiki-template-web/research.md`（R5）、`specs/054-agent-v2-bugfixes/contracts/web-ui.md`（§6 独占编辑表单/二步删除/错误呈现语汇基线）、`specs/051-agent-v2-dsh-migration/contracts/web-frontend.md`（侧栏视图切换与 API 客户端模式基线）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）；既有模式参考：`projects/game/web/frontend/src/components/PresetsView.tsx`（独占表单/二步删除/textarea 先例）、`projects/game/web/frontend/src/api/agent.ts`（`updatePreset` 的 `?update_mask=` PATCH 先例）、`projects/game/web/frontend/src/api/sessions.ts`（REST 客户端形态）、`projects/game/web/frontend/src/api/agent.test.ts`（API 客户端 fetch mock 断言先例——T010 风格基线）、`projects/game/web/frontend/src/components/PresetsView.test.tsx`（组件测试风格基线——T011）、`projects/game/web/frontend/src/App.test.tsx`（视图切换断言基线——T012）

- [X] T010 [P] [US2] 新增 `projects/game/web/frontend/src/api/wiki.ts` 与 `api/wiki.test.ts`：六方法（`createWiki(template, wikiId, title, content)`、`listWikis(template, {pageSize, pageToken})`、`searchWikis(template, query, {pageSize, pageToken})`、`getWiki(name)`、`updateWiki(name, {title?, content?})`、`deleteWiki(name)`）——相对路径 `/api/v1/...`、`requestJson`/`ApiError` 复用、PATCH 经 `?update_mask=` query 传递（按实际修改字段构造 title/content 子集）、protojson camelCase 投影（`Wiki`/轻量搜索条目类型按 `specs/069-wiki-template-web/data-model.md` §4）；`wikiId` 派生 `sha256(title_norm)[:32]`（NormalizeTitle JS 孪生，参照 `common/js/dsh-plugins/memory-service/src/wiki-operations.ts` 既有实现）；测试为 fetch mock 请求形状断言（URL/method/body/query 与响应投影，风格对齐 `api/agent.test.ts`）
- [X] T011 [US2] 新增 `projects/game/web/frontend/src/components/WikiView.tsx` 与 `WikiView.test.tsx`（依赖 T010）：按 `specs/069-wiki-template-web/contracts/web-wiki-view.md` §3-4 实现搜索框（非空 → searchWikis、空 → listWikis 分页）、列表条目（title + update_time + 查看/编辑/删除）、独占创建/编辑表单（title=`Input`、content=原生 `<textarea>` 主题样式）、inline 二步删除确认、loading/error/空态、错误呈现（409 重名/404 并发删除/400 超限——`ApiError.message`）；组件测试覆盖搜索/空关键词/创建成功与重名错误/编辑保存/二步删除/空态与错误态（风格对齐 `PresetsView.test.tsx`）
- [X] T012 [US2] 在 `projects/game/web/frontend/src/App.tsx` 接入第三视图：`AppView` 类型增加 `'wiki'`、侧栏视图切换增加 wiki 按钮（sessions | presets | wiki 三态）、主区条件渲染 `<WikiView template={TEMPLATE_SAOLEI} />`（复用既有常量）；`App.test.tsx` 增加三视图切换断言（依赖 T011）

**Checkpoint**: web wiki 管理闭环组件级可测（vitest 全绿）；部署后可按 quickstart 场景 4 做人工 walkthrough 确认。

---

## Phase 5: User Story 3 - memory 单条长度上限（Priority: P2）

**Goal**: 每条 memory content ≤ 1024 rune 的双层执行（服务端校验 + 工具预检）与指引预先说明落地。

**Independent Test**: quickstart.md 场景 2——Go/JS 单测（校验 case 表、预检文本、批量原子、guidance 措辞）+ REST 超限 400 大型测试用例。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/golang.md`、`style/javascript.md`、`style/large_test.md`（T016）、`style/api.md`（AIP 索引与 REST/注解规则来源）；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)、[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)；[AIP-193 Errors](https://google.aip.dev/193)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)（输入校验节）
- **官方文档**：[unicode/utf8](https://pkg.go.dev/unicode/utf8)（`RuneCountInString` rune 计数语义）；[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)（`codes.InvalidArgument`）；[Vitest](https://vitest.dev/)
- **技术文章/技术参考文档**：`specs/069-wiki-template-web/contracts/memory-content-limit.md`（全文）、`specs/069-wiki-template-web/contracts/dsh-plugins.md`（§4-5）、`specs/069-wiki-template-web/data-model.md`（§1.2/§3）、`specs/069-wiki-template-web/quickstart.md`（场景 2）、`specs/069-wiki-template-web/research.md`（R3/R6）；`specs/068-wiki-round-pause/contracts/dsh-plugins.md`（§2 item 1/§3 被修订基线——两段式批量与指引段分工）、`specs/039-planner-memory-calibration/contracts/memory-service-contract.md`（§1/§2/§6 Memory 资源与 CRUD 基线——写入校验增补面）、`specs/048-js-esm-migration/contracts/esm-package-conventions.md`（TS 源码书写规则）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（`vitest_test` shim 退出码契约）

- [X] T013 [P] [US3] 在 `projects/game/memory/domain/model.go` 新增 `MaxMemoryContentRunes = 1024` 常量与 `ValidateMemoryContent(content string) error`（`utf8.RuneCountInString` 度量，超限错误消息含上限与实际 rune 数——`specs/069-wiki-template-web/contracts/memory-content-limit.md` §2）；`projects/game/memory/handler/handler.go` 的 `CreateMemory`（body content 提取后）与 `UpdateMemory`（`applyMemoryMask` 之后）调用并映射 INVALID_ARGUMENT；`model_test.go`/`handler_test.go` case 表（空/边界 1024/超限 1025/多字节中文 rune 计数/Create 与 Update 双路径）
- [X] T014 [P] [US3] 在 `common/js/dsh-plugins/memory-service/src/operations.ts` 新增 memory 预检：`MEMORY_CONTENT_MAX_RUNES = 1024` 常量；`applyAdd`/`applyReplace` 单条路径（与既有空 content 检查同位）与 `applyBatch` preflight（add/replace 分支）增加长度检查；超限领域文本按 `specs/069-wiki-template-web/contracts/memory-content-limit.md` §3（`memory: content exceeds 1024 characters (got N). Keep entries short; store longer knowledge with wiki_write.`，批量走既有 `batchError` 格式全批不写）；`operations.test.ts` 更新（单条超限文本含上限与 wiki_write 引导、批量含一条超限 → store 零写入断言、1024 边界通过）
- [X] T015 [P] [US3] 在 `common/js/dsh-plugins/memory/src/guidance.ts` 修订 `MEMORY_GUIDANCE`（分工差异段，英文）：memory 侧新增单条长度上限说明（约数表述 ~1000 字符量级，更长知识写入 wiki）；wiki 侧措辞明确跨会话持久（persists across sessions——template 作用域的模型可感知语义）；`WIKI_GUIDANCE` 与各工具描述不动；`guidance.test.ts` 增加措辞断言（分工段含上限句与跨会话措辞、用法段不含上限说明）
- [X] T016 [US3] 大型测试增例与验收：`projects/game/testplan/memory_test.go` 增例——REST Create 以 > 1024 rune content（含多字节字符）→ 400 且消息含上限；通过 testplan skill 实际执行 `guitar run projects/game/testplan/system_test.yaml` 全部用例通过（可与 T009 合并为同一次执行轮次，本 task 在 T013 后进行）（依赖 T013）

**Checkpoint**: 超限写入 100% 被拒且模型收到含引导的文本结果；合法写入零回归。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 文档终态、全仓回归与一致性复核。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/large_test.md`（验收复核执行面）
- **官方文档**：无
- **技术文章/技术参考文档**：`specs/069-wiki-template-web/quickstart.md`（全场景）、`specs/069-wiki-template-web/contracts/wiki-service.md`、`specs/069-wiki-template-web/contracts/memory-content-limit.md`、`specs/069-wiki-template-web/contracts/dsh-plugins.md`、`specs/069-wiki-template-web/contracts/web-wiki-view.md`（四份——实现一致性对照）、`projects/game/web/README.md`、`projects/game/agent_v2/README.md`（文档终态核对与更新目标——T017）

- [X] T017 [P] 文档终态更新：`projects/game/web/README.md` 页面清单增加 wiki 管理视图（结构节 `src/api/`、`src/components/` 描述同步）；检查 `projects/game/agent_v2/README.md` 与 `common/js/dsh-plugins/memory{,-service}/README.md`（如存在）中 wiki 作用域表述并终态化（template 级，无 session 字样残留——宪法 VII）；各新增/修订源文件的注释引用核对（指向 `specs/069-wiki-template-web/` 契约而非 068 被修订条目）；`projects/game/gateway/cmd/main.go` 两处注释终态化（memoryConn 注释：memories/wikis 双前缀全写（`/api/v1/templates/{template}/sessions/{session}/memories` 与 `/api/v1/templates/{template}/wikis`）、引用改指 `specs/069-wiki-template-web/contracts/wiki-service.md` §2/§6、provenance 改 `specs/069-wiki-template-web/spec.md` FR-001；RegisterWikiServiceHandler 注册处注释：引用 068 §6 改 069 §6）；Phase 3 review 反馈回落——`projects/game/memory/runtime/mongo/wiki_repository_test.go` 断言 helper 别名语义错位修正（`assertStringsEqual` 转发 helper 中性化直用）、`projects/game/testplan/BUILD.bazel` wiki_test 注释与 wiki_test.go 文件头表述对齐（并非全部 case 均 reset）、JS 面 `wiki-operations.ts`/`wiki-client.ts` 中 068 引用统一重指 069 契约（069 已重述的条目）、`projects/game/memory/cmd/main.go` 头注释 `spec 039-planner-memory-calibration FR-006` 裸引用归一为仓库相对路径
- [ ] T018 全仓验证与终态门禁复核：`bazel build //...` 与 `bazel test //...` 全绿；按 `specs/069-wiki-template-web/quickstart.md` 复核场景 1-5（大型测试已由 T009/T016 执行，此处核对场景覆盖与结果记录）；契约-data-model-实现三方一致性抽查（作用域、上限值 1024、路由形态）；确认无 068 被修订语义的残留引用（旧路由/旧索引/`session_id` 维度）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无任务，直接进入 Phase 2
- **Foundational (Phase 2)**: proto 契约变更——**阻塞 US1/US2**（US3 的代码不依赖 proto codegen 形状；T002 仅为 proto 注释对齐，顺路同批交付，不构成 US3 代码依赖）
- **US1 (Phase 3)**: 依赖 Phase 2；内部 T004 → T005 → T006 → T008 → T009 串行，T007（JS 面）在 Phase 2 后可与 T004-T006 并行
- **US2 (Phase 4)**: 编码依赖 Phase 2 proto 形态；运行时验证（walkthrough/testplan 环境）依赖 US1 完成；内部 T010 → T011 → T012 串行
- **US3 (Phase 5)**: 代码不依赖 Phase 2 产物（仅 T002 的 proto 注释随 Phase 2 同批提交）；T013/T014/T015 三任务不同文件可并行；T016 依赖 T013；**与 US1/US2 无文件交集，整 phase 可并行推进**
- **Polish (Phase 6)**: 依赖全部 story 完成

### User Story Dependencies

- **US1 (P1)**: Phase 2 后即可开始——MVP 主体
- **US2 (P1)**: 依赖 US1 的 template 级 REST 面（spec US2 "依赖 US1 的 template 级作用域"）；vitest 编码面可先行，部署验证在 US1 之后
- **US3 (P2)**: 独立于 US1/US2，可随时并行

### Parallel Opportunities

- T004（Go domain）与 T007（JS 面）在 Phase 2 后并行
- T013/T014/T015（US3 三任务，不同包不同文件）并行
- T017 与 T018 无文件交集可并行（T018 的全量验证建议在 T017 后收尾）
- US3 整个 phase 可与 US1/US2 由不同执行者并行

---

## Parallel Example: User Story 3

```bash
# 三个独立文件面，Phase 2 完成后同时启动：
Task: "T013 Go 校验 MaxMemoryContentRunes + ValidateMemoryContent（projects/game/memory/domain/model.go + handler/handler.go）"
Task: "T014 JS 预检 MEMORY_CONTENT_MAX_RUNES（common/js/dsh-plugins/memory-service/src/operations.ts）"
Task: "T015 guidance 分工段上限句与跨会话措辞（common/js/dsh-plugins/memory/src/guidance.ts）"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 2: proto 契约变更
2. Complete Phase 3: US1（服务端 + 工具面 + 大型测试全绿）
3. **STOP and VALIDATE**: quickstart 场景 1/3/5——template 级 REST 闭环 + 跨 session 工具语义 + 启动清理
4. Deploy if ready（存量 clean break 随部署生效）

### Incremental Delivery

1. Phase 2 → Phase 3（US1，MVP）→ 验证
2. Phase 4（US2 web 视图）→ vitest + walkthrough 验证
3. Phase 5（US3 上限，可穿插并行）→ 场景 2 验证
4. Phase 6（文档终态 + 全仓回归）

### Parallel Team Strategy

- 执行者 A：US1（Go 面串行链 + JS 面）
- 执行者 B：US3（三文件并行链）——Phase 2 后即可开始
- 执行者 C：US2 编码面（T010-T012），US1 合入后做部署验证

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖；同文件任务必须串行（如 T001 → T002 同为 game.proto）
- 编译 + 单测（宪法 IV）内嵌于每个任务，不单列；大型测试验收（宪法 VI）单独成 task（T009/T016/T018 复核）
- 每个任务完成即提交；checkpoint 处可停下独立验证
- T009 与 T016 可合并为同一次 `guitar run` 轮次（两 suite 同计划）；T018 收尾再跑一次全量确认
- 避免：跨 story 的文件耦合（US1 改 `wiki_handler.go`/`wiki-*.ts`，US3 改 `handler.go`/`operations.ts`/`guidance.ts`，US2 仅 web 前端——无交集）
