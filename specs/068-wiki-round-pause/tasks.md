# Tasks: planner wiki 知识库与 saolei-loop 每五轮自然停

**Input**: Design documents from `/specs/068-wiki-round-pause/`

**Prerequisites**: plan.md、spec.md（用户故事 US1–US4）、research.md、data-model.md、contracts/、quickstart.md（均已存在）

**Tests**: 按宪法原则 IV/VI：**编译 + 单测属于每个实现任务的一部分**（不单列 task）；大型测试（testplan）单独分配任务作为验收环节。各任务完成即要求 `bazel build`/`bazel test`（相关 target）通过。

**Organization**: 按 user story 分 phase（US1 wiki 服务端 → US2 wiki 工具面 → US3 描述与指引 → US4 自然停），每个 story 独立可测。US4 与所有 wiki 相关 phase 完全独立，可随时并行。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属 user story（US1–US4）；Setup/Foundational/Polish phase 无 story 标签
- 每个任务含精确文件路径

## Path Conventions

仓库根相对路径；Go 服务 `projects/game/memory/`，JS 插件 `common/js/dsh-plugins/`，测试计划 `projects/game/testplan/`（与 plan.md Project Structure 一致）。

---

## Phase 1: Setup

**无任务**：既有仓库增量变更，无新包、无新依赖（redis 依赖仅出现在可选 Phase 7；`golang.org/x/text` 已在 go.mod）。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：无

---

## Phase 2: Foundational（proto 契约与资源标识收敛）⚠️ 阻塞 US1/US2

**Purpose**: 一次 proto 编辑会话完成 Wiki 契约定义与 Memory 资源标识收敛（FR-017），产出 codegen 供服务端与 JS client 消费。

**文档清单**（编码前必读，宪法原则 V）：

- **代码规范文档**：`style/api.md`（REST/grpc 注解规则与 AIP 索引）；[AIP-121 Resource-oriented design](https://google.aip.dev/121)、[AIP-122 Resource names](https://google.aip.dev/122)、[AIP-123 Resource types](https://google.aip.dev/123)、[AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-132 Standard methods: List](https://google.aip.dev/132)、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-142 Time and duration](https://google.aip.dev/142)、[AIP-148 Standard fields](https://google.aip.dev/148)、[AIP-158 Pagination](https://google.aip.dev/158)、[AIP-203 Field behavior documentation](https://google.aip.dev/203)
- **官方文档**：[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)（`google.api.resource` 注解驱动 `ParseWikiName` 等资源名解析 codegen 的插件契约，仓库 pin v0.1.3）
- **技术文章/技术参考文档**：`specs/068-wiki-round-pause/contracts/wiki-service.md`（§1-2）、`specs/068-wiki-round-pause/contracts/memory-resource-identity.md`、`specs/068-wiki-round-pause/data-model.md`（§1/§4）、`specs/068-wiki-round-pause/research.md`（§1-2）、`survey/game-wiki-knowledge-base.md`（§5.1）

- [X] T001 在 `projects/game/game.proto` 移除 `Memory.memory_id` 字段并添加 `reserved 2; reserved "memory_id";`（对齐同文件 `Session` 消息的 035 先例手法）；同步更新 `ListMemoriesRequest.order_by` 注释——`memory_id` token 语义改为 "name 的 `{memory}` 段"（白名单与 tie-break 机制不变，`contracts/memory-resource-identity.md` §1）
- [X] T002 在 `projects/game/game.proto` 新增 `Wiki` 资源消息（`name/title/content/create_time/update_time`，无独立 id 字段，资源注解 `game.liukexin.com/Wiki` + pattern `templates/{template}/sessions/{session}/wikis/{wiki}`）与 `WikiService` 六 RPC（Create/Get/Update/Delete/List/SearchWikis）及全部请求/响应消息——字段、注解、HTTP 规则、注释严格按 `contracts/wiki-service.md` §1-2 与 `data-model.md` §1（含 `CreateWikiRequest.wiki_id` REQUIRED、`ListWikisRequest` 无 order_by、`SearchWikisRequest{parent,query,page_size,page_token}`、轻量条目 `{name,title}`）；参照同文件 `MemoryService` 的注解形态
- [X] T003 执行 `bazel run //:gazelle projects/game` 更新生成，`bazel build //projects/game/...` 验证 proto codegen（含 `ParseWikiName` 生成物、`Memory` 无 `MemoryId` 字段的编译面收敛——`projects/game/memory/handler` 此时如有编译错误，仅做最小机械修正（如移除 `MemoryId:` 赋值行），语义改造留给 T008）

**Checkpoint**: codegen 通过；`Wiki`/`WikiService` 产物可用；`Memory` 响应面不再有独立 id。

---

## Phase 3: User Story 1 - wiki 服务端（Priority: P1）🎯 MVP

**Goal**: memory 服务（Go）提供 WikiService 全部六方法与检索，gateway REST 可用，testplan 大型测试全绿（含 memory name 断言改造）。

**Independent Test**: quickstart.md 场景 1/3——gateway HTTP 完成 wiki CRUD/List/Search 全场景与 memory 零回归（`contracts/wiki-service.md` §7）。

**文档清单**：

- **代码规范文档**：`style/golang.md`、`style/mongo.md`、`style/api.md`、`style/large_test.md`；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)；[AIP-121 Resource-oriented design](https://google.aip.dev/121)、[AIP-122 Resource names](https://google.aip.dev/122)、[AIP-131 Standard methods: Get](https://google.aip.dev/131)、[AIP-132 Standard methods: List](https://google.aip.dev/132)（Ordering 节）、[AIP-133 Standard methods: Create](https://google.aip.dev/133)、[AIP-134 Standard methods: Update](https://google.aip.dev/134)、[AIP-135 Standard methods: Delete](https://google.aip.dev/135)、[AIP-136 Custom methods](https://google.aip.dev/136)、[AIP-148 Standard fields](https://google.aip.dev/148)、[AIP-158 Pagination](https://google.aip.dev/158)（参数一致性、Opacity、Expiring page tokens 节）、[AIP-193 Errors](https://google.aip.dev/193)
- **官方文档**：[golang.org/x/text/unicode/norm](https://pkg.go.dev/golang.org/x/text/unicode/norm)；[go.mongodb.org/mongo-driver](https://pkg.go.dev/go.mongodb.org/mongo-driver)（`mongo/options`、bson；`$all`/multikey/regex 查询构造）；[MongoDB: $all query operator](https://www.mongodb.com/docs/manual/reference/operator/query/all/)、[MongoDB: Multikey Indexes](https://www.mongodb.com/docs/manual/core/index-multikey/)；[google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)（`codes`/`status` 错误映射）；[protoc-gen-go-aip（protoc-contrib）README](https://github.com/protoc-contrib/protoc-gen-go-aip)（codegen `ParseWikiName`/parent 解析）；[encoding/json Unmarshal](https://pkg.go.dev/encoding/json#Unmarshal)（缺失 JSON 键解码为零值——旧形态 token 无 `sort` 键落为 nil 切片、类型不符报错的语义依据）
- **技术文章/技术参考文档**：`survey/game-wiki-knowledge-base.md`（§4 检索技术、§5.1 API 设计）、`specs/068-wiki-round-pause/contracts/wiki-service.md`、`specs/068-wiki-round-pause/contracts/memory-resource-identity.md`、`specs/068-wiki-round-pause/contracts/memory-page-token-context.md`、`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（§1 item 4/9——被修订基线）、`specs/068-wiki-round-pause/data-model.md`（§2-3）、`specs/068-wiki-round-pause/quickstart.md`（场景 1/3）

- [X] T004 [P] [US1] 在 `projects/game/memory/domain/` 新增 wiki 领域层：模型（`Wiki` 结构、作用域键）+ 校验（`wiki_id` 匹配 `[a-z0-9_-]+`、title 非空且 ≤ 128 rune、content 非空且 ≤ 64KB、title_norm 作用域唯一冲突错误）+ 纯函数 `NormalizeTitle`（NFKC + 小写，`golang.org/x/text/unicode/norm`）与 `TitleBigrams`（rune 级 2-gram 去重）+ 表驱动单测（中文/全角/大小写/emoji case，`data-model.md` §2.1-2.2；测试规范遵 `style/golang.md` 单元测试节）
- [X] T005 [P] [US1] 在 `projects/game/memory/domain/` 新增 wiki 分页 token：Search token（base64url `{q, offset}` 编解码 + 续页 query 规范化后与 token 内 `q` 一致性校验，不一致 `ErrInvalidPageToken`；空 query 拒绝）与 List 固定 `wiki_id asc` 单键 seek 游标 + 评分函数（精确 > 前缀 > 子串，同分 `title_norm` 升序）+ 表驱动单测（`data-model.md` §3）
- [X] T006 [US1] 在 `projects/game/memory/runtime/mongo/` 新增 wikis 仓储：文档模型（`{_id, template, session_id, wiki_id, title, title_norm, title_grams, content, create_time, update_time}`，`style/mongo.md` 对象定义）+ 启动建三索引（unique `(template,session_id,wiki_id)`、unique `(template,session_id,title_norm)`、`(template,session_id,title_grams)` multikey）+ CRUD/List（单键 seek）+ Search（≥2 rune 走 `title_grams $all` 投影查询，<2 rune 回退 `title_norm` regex `QuoteMeta`）——保持 `collectionOps` seam 不扩展 Aggregate；fake collection 单测（filter 断言模式，仿 `repository_test.go`）（依赖 T004/T005）
- [X] T007 [US1] 在 `projects/game/memory/handler/` 新增 wiki handler：六 RPC 实现（Create：parent 解析 + 校验 + ALREADY_EXISTS 映射；Get/Update/Delete：name 解析（codegen `ParseWikiName`）+ NOT_FOUND；Update title 变更重生成 grams；List：固定序 + 游标 + `page_size` 默认 20/上限 100；Search：query 规范化 + 空 query INVALID_ARGUMENT + 仓储预过滤 + Go 侧 `strings.Contains` 验证 + 评分排序 + offset 分页 + `page_size` 默认 20/上限 100/负数 INVALID_ARGUMENT（AIP-158 coerce 语义））+ 单测（错误映射表驱动，AIP-193 语义对齐既有 memory handler）（依赖 T006）
- [X] T025 [P] [US1] 契约与 proto 注释（接口先行，宪法 III）：新增 `specs/068-wiki-round-pause/contracts/memory-page-token-context.md`（按定稿全文——token 显式携带 order_by 最终排序键上下文并独立校验，游标载荷校验归属游标领域内部逻辑；修订 065 §1 item 4）；修订 `specs/068-wiki-round-pause/contracts/memory-resource-identity.md` §2 第四条为精确边界表述；`specs/068-wiki-round-pause/research.md` 决策总览增 R9 行 + 新增 R9 裁定记录节（原参考文献节顺延编号）；`specs/068-wiki-round-pause/plan.md` 目录树与 Constitution Check 契约清单补新契约文件；更新 `projects/game/game.proto` `ListMemoriesRequest.order_by` 注释第三段为终态表述（token 携带 order_by 上下文并逐项比对含方向、载荷处理属游标内部、不一致 INVALID_ARGUMENT、引用新契约与 065 基线——宪法 VII 只述终态）；`bazel run //:gazelle projects/game` + `bazel build //projects/game/...`（注释进入 codegen 产物）
- [X] T026 [US1] 在 `projects/game/memory/domain/` 按「page token 契约层 / 游标领域内部逻辑」两层归属实现 ListMemories token 显式 order_by 上下文（`specs/068-wiki-round-pause/contracts/memory-page-token-context.md` §1-2）：`pagination.go`——`MemoryPageCursor` 增 `Sort []string`（json tag `sort`，token wire 形态 `{"sort":[...],"entries":[...]}`）；**page token 层**：`DecodeMemoryPageToken(token, sort)` 签名不变，负责信封解码（空 token/坏 base64/坏 JSON 含顶层非对象/`entries` 缺失或非数组）与 **order_by 上下文一致性**（`sort` 上下文与请求最终排序键长度 + 逐项拼写比对，方向参与）；**游标层**：包内私有游标构造/校验函数（如 `newMemoryCursor(sort, entries) (*MemoryPageCursor, error)`）在游标消费 token 携带内容时校验其与当前游标实现一致——每 sort term 恰一 entry、entry field 标签与位序 term 对齐、值按 term kind 经 `CursorValue` 一次完成校验与 typed 转换（原内联校验保留、归属重划）；导出 `NewMemoryPageCursor(sort []*MemorySortTerm, values []any) *MemoryPageCursor` 为唯一铸造入口（entries 与上下文成套填充，上下文拼写经 `memorySortTermWire`；`len(values) != len(sort)` panic fail-fast）；`EncodeMemoryPageToken` 空 token 条件增「Sort 为空」；全部失败 wrap `ErrInvalidPageToken`；`sort.go` 增包内私有 `memorySortTermWire(term) string`（`"{field}"`/`"{field} desc"`——铸造与比对共用的单一拼写源）；按契约 §5 更新 `pagination_test.go`（上下文校验/信封/游标内部校验用例按两层归属组织）/`sort_test.go`（`memorySortTermWire` 真值表）；`bazel test //projects/game/memory/domain/...`（依赖 T025；与 T004/T005/T006 不同文件可并行）
- [X] T027 [US1] 仓储铸造与 handler 测试面：`projects/game/memory/runtime/mongo/repository.go` `memoryNextPageToken` 改经 `domain.NewMemoryPageCursor` 铸造（上下文与 entries 成套单点）；`handler.go` 零改动（`DecodeMemoryPageToken(token, sort)` 签名与调用流不变）；`repository_test.go` 经 Decode 的手铸 token（`TestMemoryListSeekValuesAreTyped` 等）改 `domain.NewMemoryPageCursor` 铸造（`memorySeekClauses` 直测用例绕过 Decode、零改动），`handler_test.go` `TestMemoryService_ListMemoriesPageToken` 增方向翻转拒绝与等价拼写（`update_time desc, memory_id`）透传用例；`bazel test //projects/game/memory/...`（依赖 T026；与 T006/T007 不同文件可并行，与 T008 同文件 `handler_test.go` 需串行——先 T027 后 T008）
- [X] T008 [P] [US1] 在 `projects/game/memory/handler/handler.go` 完成资源标识收敛语义改造：确认响应构造不再设置 `MemoryId`、相关注释终态化（引用 `contracts/memory-resource-identity.md`），更新 `handler_test.go` 中 memory_id 断言为 name 断言
- [X] T009 [US1] 服务注册：`projects/game/memory/cmd/main.go` 注册 `WikiServiceServer`（同 mongo client/db）；`projects/game/gateway/cmd/main.go` 在 `gameconst.MemoryTarget` 同 target 注册 WikiService grpc-gateway REST 面（依赖 T007）
- [X] T010 [US1] 大型测试编写：新增 `projects/game/testplan/wiki_test.go`（gateway HTTP：Create/Get/Update/Delete、title 唯一冲突 409、List 固定序 + 分页续页、Search 中文子串/大小写/全半角/短查询/续页 query 不一致 400、空 title/content/query 400、超限 400；helper 复用 `helpers_test.go` 既有构造器，遵 `style/large_test.md` 测试组织——按模块归位不按场景编号）；改造 `projects/game/testplan/memory_test.go` 全部 `GetMemoryId()` 断言（18 处）及 `memory_id` 字面量（order_by token / 路径参数）为 `GetName()`/name 形态；`bazel run //:gazelle projects/game/testplan` 更新生成，在既有测试计划 `projects/game/testplan/system_test.yaml`（`game-system` suite）加入 case（不新建平行计划）；增列 token order_by 上下文一致性用例（`update_time desc` 首页 token 配 `order_by=update_time` 翻转方向 → 400；desc token 配等价拼写 `update_time desc, memory_id` 续页 → 200 序保持；缺省 token 配显式 `order_by=memory_id` 续页 → 200——`specs/068-wiki-round-pause/contracts/memory-page-token-context.md` §5）
- [X] T011 [US1] 大型测试验收（宪法 VI）：通过 testplan skill 实际执行（`guitar run projects/game/testplan/system_test.yaml`，部署→测试→清理闭环），全部用例通过（含 memory 零回归）；任何失败修复后重跑至全绿（依赖 T010）

**Checkpoint**: wiki 服务端经 gateway HTTP 全链路可用且大型测试全绿 = **MVP 交付**。

---

## Phase 4: User Story 2 - wiki 工具面（Priority: P1）

**Goal**: memory 插件行注册三个 wiki 工具（`wiki_search` 仅 query / `wiki_get` 批量 / `wiki_write` 单条 XOR 批量 upsert），经扩展的 `plannerMemory` 服务面访问 WikiService；JS 侧完成 memory name 解析（FR-017）。

**Independent Test**: quickstart.md 场景 2 单测面——三工具注册与参数 schema、upsert/批量原子/精确解析/结果无内部标识断言全绿。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Node.js ESM: import.meta.dirname](https://nodejs.org/api/esm.html#importmetadirname)、[Node.js ESM: CommonJS namespaces](https://nodejs.org/api/esm.html#commonjs-namespaces)、[Node.js Packages: "type"](https://nodejs.org/api/packages.html#type)、[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/048-js-esm-migration/research.md`（§R5 CJS 第三方依赖互操作普查——具名导入可用包表）、`specs/019-js-test-reliability/research.md`（js_test 执行模型与 mock 根因）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（js_test 退出码契约）、`specs/068-wiki-round-pause/contracts/dsh-plugins.md`、`specs/068-wiki-round-pause/contracts/memory-resource-identity.md`（§1 JS 面）、`specs/068-wiki-round-pause/data-model.md`（§5-7）、`specs/068-wiki-round-pause/research.md`（§3-4）、`survey/game-wiki-knowledge-base.md`（§5.2）

- [X] T012 [P] [US2] 新增 `common/js/dsh-plugins/memory-service/src/wiki-client.ts`：`WikiStore` 接口（`searchWikis/getWiki/createWiki/updateWiki` 四方法）+ gRPC 实现（复用 `client.ts` 的 proto-loader/凭证/round_robin 通道形态，DI seam 同款）+ `wikiName()`/`parseWikiName()` + 单测（DI 注入 fake client）
- [X] T013 [P] [US2] 修改 `common/js/dsh-plugins/memory-service/src/client.ts`：新增 `parseMemoryName(name)`（取 `/memories/` 末段，与 `memoryName()` 对称），`listMemories` 解码从 `name` 派生 `MemoryEntry.memory_id`；更新 `client.test.ts`（FR-017 JS 面，`contracts/memory-resource-identity.md` §1）
- [X] T014 [US2] 新增 `common/js/dsh-plugins/memory-service/src/wiki-operations.ts`：wiki 操作核——`wikiSearch`（以 `page_size`=100 折叠 `next_page_token` 取全命中标题 + 渲染标题列表 + 空结果引导）、`wikiGet`（逐标题规范化精确解析（JS 侧 `normalize("NFKC").toLowerCase()` 与服务端对齐）→ GetWiki → 命中内容 + 未命中引导）、`wikiWrite`（单条 XOR 批量；逐条解析：命中 → update，未命中 → create（`wiki_id = sha256(title_norm)[:32]`）；批量 preflight 全过才 commit（映射 `operations.ts` 的 `applyBatch` 两段式）；逐条目 created/updated 文本）+ 单测（upsert/部分未命中/批量原子/超限/空批量与空输入文本引导）（依赖 T012）
- [X] T015 [US2] 修改 `common/js/dsh-plugins/memory-service/src/service.ts`：`PlannerMemoryService` 新增 `wikiSearch/wikiGet/wikiWrite`（复用 `load()` 的 per-agent scope 绑定；未绑定 scope 抛错、基础设施失败 reject）+ `src/index.ts` 导出面更新 + 单测（依赖 T014）
- [X] T016 [US2] 新增 `common/js/dsh-plugins/memory/src/wiki-tools.ts`：三工具定义（`wiki_search` 参数仅 `query`（内部以 `page_size`=100 折叠 `next_page_token` 取全命中标题）、`wiki_get` 参数 `titles: string[]`、`wiki_write` 单条 XOR `entries[]`（空 `entries` → 引导文本不提交），参数 schema 策略对齐 `tool.ts`——根级可选 + 项内 `additionalProperties: false`；`{result}` 输出 + 文本渲染；失败 → `wiki failed: …` 文本；结果不含 `name`/`wiki_id`；每工具描述仅述自身功能，英文）+ 单测（含 `wiki_search` 翻页聚合与空 `entries` 引导）（依赖 T015）
- [X] T017 [US2] 修改 `common/js/dsh-plugins/memory/src/index.ts`：行内注册三工具（经 `ctx.plannerMemory` 绑定，形态同既有 memory 工具注册）；更新 `index.test.ts` 断言（工具计数/名称；系统提示 section 集合与既有一致、wiki 不注入任何内容——spec 边界"wiki 不注入快照"）并断言 player 池模板物化组合不挂载任何 wiki 工具（复用 `projects/game/agent_v2/src/dsh.test.ts` 既有 templateRules 断言面，闭合 spec US2 验收场景 1）

**Checkpoint**: US1+US2 组合后 planner 具备 wiki 全工具链（单测面）。

---

## Phase 5: User Story 3 - memory 描述通用化与配套指引（Priority: P2）

**Goal**: memory 工具描述修订（通用化 + top-10 快照说明）+ 两个英文 prompt section（分工差异/wiki 整体用法）。

**Independent Test**: quickstart.md 场景 2 断言面——描述与 section 文本断言全绿（spec FR-012/013）。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[Node.js Packages: "type"](https://nodejs.org/api/packages.html#type)、[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/048-js-esm-migration/research.md`（§R5 CJS 第三方依赖互操作普查）、`specs/019-js-test-reliability/research.md`（js_test 执行模型与 mock 根因）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（js_test 退出码契约）、`specs/068-wiki-round-pause/spec.md`（FR-012/013）、`specs/068-wiki-round-pause/contracts/dsh-plugins.md`（§3）、`survey/game-wiki-knowledge-base.md`（§3.1/§3.2 记忆分层实践——分工段内容取材）、`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（修订基线）

- [ ] T018 [P] [US3] 修改 `common/js/dsh-plugins/memory/src/tool.ts`：重写 `MEMORY_TOOL_DESCRIPTION`——通用表述（去 `planner`/`hermes-style` 等场景与外部措辞）、新增"快照仅保留最近更新的 10 条、写操作作用于全量存储且不刷新快照"说明、仅述自身功能、英文（措辞要点见 `contracts/dsh-plugins.md` §3）；更新 `tool.test.ts` 断言（无场景名词、含 top-10 说明）
- [ ] T019 [P] [US3] 新增 `common/js/dsh-plugins/memory/src/guidance.ts`（两个 section 常量与文本）并修改 `common/js/dsh-plugins/memory/src/index.ts` 注册：`memory:guidance`（order 110——memory vs wiki 分工差异：常驻少量近期工作记忆 vs 按需检索海量知识库；不含用法/参数、无 memGPT/Letta 术语）与 `wiki:guidance`（order 120——`wiki_search` 发现 → `wiki_get` 取回 → `wiki_write` 沉淀/更新的配合用法；不重复分工）；文本风格参照 `common/js/dsh-plugins/saolei/src/index.ts` 的 `SAOLEI_GUIDANCE`（英文、markdown 小节）；新增/更新单测断言（section 名称/order/文本边界：分工段无用法、用法段无分工、均无第三方术语）

**Checkpoint**: 模型面文本（描述 × 4 + section × 2）全部就位且断言全绿。

---

## Phase 6: User Story 4 - saolei-loop 每五轮自然停（Priority: P2）

**Goal**: orchestrator 完成轮计数与停点：每第 5 个完成轮的 planner turn（含排队消息消化）结束后停止自动续驱 player，activation 切至 player、计数清零；用户消息恢复。

**Independent Test**: quickstart.md 场景 4——`contracts/saolei-loop-round-stop.md` §5 八条用例全绿。

**文档清单**：

- **代码规范文档**：`style/javascript.md`；[Google TypeScript Style](https://google.github.io/styleguide/tsguide.html)
- **官方文档**：[TypeScript Modules Reference](https://www.typescriptlang.org/docs/handbook/modules/reference.html)、[vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)
- **技术文章/技术参考文档**：`specs/048-js-esm-migration/contracts/esm-package-conventions.md`、`specs/019-js-test-reliability/research.md`（js_test 执行模型与 mock 根因）、`specs/019-js-test-reliability/contracts/run-vitest-shim.md`（js_test 退出码契约）、`specs/068-wiki-round-pause/contracts/saolei-loop-round-stop.md`、`specs/068-wiki-round-pause/data-model.md`（§8 状态图）、`specs/068-wiki-round-pause/spec.md`（FR-014~016）

- [ ] T020 [US4] 修改 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 并扩展 `orchestrator.test.ts`：新增 `completedRounds` 私有状态与 `ROUND_STOP_INTERVAL = 5` 常量；计数点在 `runPump` review settle 分支（`step.review !== undefined` 且 drive 成功）；停点在 `nextStep` 的 planning/reviewing 分支（队列分支之后）——`completedRounds >= 5` 时执行 `phase = "playing"`/`current = "player"` 转移但返回 null（不驱动、不 drain player relays）并清零计数；不写 `lastError`/不置 `paused`/快照零新增字段；注释引用契约（终态表述）；单测覆盖契约 §5 全部 8 条用例（fake 成员/终态记录注入，`style/javascript.md` Mock 约定）；复核 `projects/game/agent_v2/README.md` §大型测试既有 068 豁免段（本行为不单设大型测试的说明；宪法 VI：以编排层单测为验收、既有 agent_v2 testplan 作零回归），不一致处更新

**Checkpoint**: 自然停状态机独立交付（与 wiki 部分零耦合）。

---

## Phase 7:（可选，可整体裁剪）Search 结果缓存

**Purpose**: 调研 D7 的 redis 快照缓存——纯服务端优化，裁剪不影响任何契约（`contracts/wiki-service.md` §4.5、research.md §5）。

**文档清单**：

- **代码规范文档**：`style/golang.md`；[Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Google Go Style Decisions](https://google.github.io/styleguide/go/decisions)、[Google Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)
- **官方文档**：[go-redis v9 Guide](https://redis.uptrace.dev/guide/go-redis.html)（仓库内 `common/gopkg/redis` 为公共库封装，用法参照该包源码注释与 067 交付的 redis_demo）
- **技术文章/技术参考文档**：`survey/game-wiki-knowledge-base.md`（§5.1.1）、`specs/068-wiki-round-pause/contracts/wiki-service.md`（§4）、`specs/068-wiki-round-pause/data-model.md`（§3.3）、`specs/068-wiki-round-pause/research.md`（§5）

- [ ] T021 在 `projects/game/memory/` 接入 Search 缓存：redis client 装配（`common/gopkg/redis`，cmd 注入 domain/handler seam）+ 快照缓存（key `wiki:search:{template}:{session}:{gen}:{hash(q)}`、TTL ~60s）+ 写代际计数器（`wiki:searchgen:{template}:{session}`，wiki 写 INCR）+ miss/不可用降级为逐页重执行 + go.mod/bazel 依赖（`bazel run //:go -- mod tidy` + `bazel mod tidy` + gazelle）+ 单测（命中/失效/降级三路径，fake redis seam 注入）（依赖 Phase 3 完成）

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: 全仓验证与终态门禁复核。

**文档清单**：

- **代码规范文档**：无
- **官方文档**：无
- **技术文章/技术参考文档**：`.specify/memory/constitution.md`（门禁 4/5 复核依据）、`specs/068-wiki-round-pause/quickstart.md`、`specs/068-wiki-round-pause/checklists/requirements.md`、`projects/game/agent_v2/README.md`（§大型测试豁免记录面）

- [ ] T022 全量构建与测试：`bazel build //...` 与 `bazel test //...`（至少覆盖 `//projects/game/...` 与 `//common/js/dsh-plugins/...`）全绿
- [ ] T023 按 `specs/068-wiki-round-pause/quickstart.md` 逐场景核对验收状态（场景 1/3 已由 T011 执行——复核结果记录；场景 2/4 单测面确认；缺口补齐）
- [ ] T024 终态门禁复核（宪法 I/VII/VIII）：全部新增/修改代码与文档的引用可追溯（仓库相对路径/URL）、注释与文档只表述终态、图表为 Mermaid；`checklists/requirements.md` 全项复核更新

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**: 无任务
- **Phase 2（Foundational）**: 阻塞 US1（T007 依赖 codegen）与 US2（T012 依赖 proto 文件）；T001/T002 同文件串行
- **Phase 3（US1）**: 依赖 Phase 2；T004/T005 并行 → T006 → T007 → T009；T025（契约+proto 注释，无依赖可先行）→ T026 → T027（与 wiki 侧 T006/T007/T009 不同文件可双线并行）→ T010（依赖 T006、T027）→ T011；T008 在 T027 之后执行（同文件 handler_test.go 串行），其余独立
- **Phase 4（US2）**: 依赖 Phase 2（proto）；T012/T013 并行 → T014 → T015 → T016 → T017
- **Phase 5（US3）**: 依赖 Phase 4（工具与 section 描述对象存在）；T018/T019 并行
- **Phase 6（US4）**: **零依赖**——可在任何时刻与 Phase 2+ 并行开展（不同包、不同文件）
- **Phase 7（缓存）**: 依赖 Phase 3；整体可裁剪
- **Phase 8（Polish）**: 依赖全部保留 phase 完成

### User Story Dependencies

- **US1（P1）**: Phase 2 后即可开始，无其他 story 依赖 → **MVP**
- **US2（P1）**: 依赖 Phase 2；消费 US1 的服务端（联调），但单测面仅依赖契约+fake
- **US3（P2）**: 依赖 US2（文本描述三工具与 memory 工具的关系）
- **US4（P2）**: 独立

### Parallel Opportunities

```text
Phase 2 完成后：
  开发者 A: Phase 3 (US1 服务端)          开发者 B: Phase 6 (US4 自然停，零依赖)
Phase 3/6 完成后：
  Phase 4 (US2) → Phase 5 (US3)          Phase 7 (缓存，可选)
同 phase 内：T004∥T005、T025 可先行（无依赖）、T012∥T013、T018∥T019；T026→T027 依赖链与 wiki 侧（T006/T007/T009）可双线并行（T027 与 T008 同文件 handler_test.go 需串行）
```

---

## Implementation Strategy

### MVP First（US1 only）

1. Phase 2（proto 契约与标识收敛）→ 2. Phase 3（US1 服务端 + 大型测试验收）→ **STOP and VALIDATE**：gateway HTTP 全链路可用（quickstart 场景 1/3）

### Incremental Delivery

1. Phase 2 → Phase 3 = wiki 服务端 MVP
2. Phase 4 → planner 工具链可用（端到端价值闭环）
3. Phase 5 → 模型面指引完备
4. Phase 6 → 自然停（可任意提前，独立交付）
5. Phase 7（可选）→ 检索缓存优化
6. Phase 8 → 全仓复核收尾

---

## Notes

- 每个实现任务的完成标准含其单测与相关 target 的 `bazel build`/`bazel test`（宪法 IV，不单列）
- 大型测试仅 T011 实际执行一次闭环（部署→测试→清理），T024 仅复核记录
- AGENTS.md 是代码开发必读（宪法 V 免于在 tasks.md 重复列出）；本特性 spec/plan 与设计文档按各 phase 实际需要显式列于该 phase 的文档清单
- 同文件编辑串行（T001→T002；T006→T007 等按依赖图）
- 提交节奏：每 task 或逻辑组提交一次
