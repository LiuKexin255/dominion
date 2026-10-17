# Research: wiki 提升为 template 级知识库、memory 单条长度上限与 web wiki 管理

**Feature**: specs/069-wiki-template-web/spec.md | **Date**: 2026-09-20

**调研对象**：068 已交付的 wiki 全链路（`projects/game/game.proto`、`projects/game/memory/`、`common/js/dsh-plugins/memory{,-service}/`、`projects/game/testplan/wiki_test.go`）、web 前端（`projects/game/web/frontend/`）、AIP codegen 能力面、仓库契约修订惯例。

## 1. 现状盘点（调研结论）

- **资源形态**：Wiki 现为 `templates/{template}/sessions/{session}/wikis/{wiki}`（`projects/game/game.proto` Wiki resource option），六 RPC（Create/Get/Update/Delete/List/Search）经 gateway 暴露于 `/api/v1`；handler 以 `game.ParseSessionName(parent)` + `gameconst.ValidateTemplateName` 做父校验（**不做 session 存在性校验**，仅 template 已知集校验——`projects/game/pkg/gameconst/const.go` knownTemplateIDs 当前仅 `saolei`）。
- **存储**：`game_memory.wikis` 集合，文档含 `session_id`；三个索引均带 session 维度（unique `(template, session_id, wiki_id)`、unique `(template, session_id, title_norm)`、multikey `(template, session_id, title_grams)`），在 `NewWikiRepository` 构造时创建（`projects/game/memory/runtime/mongo/wiki_repository.go`）。
- **JS 工具面**：`WikiStore` 接口与 `wikiSearch/wikiGet/wikiWrite` 操作核均携带 `(template, session)` 双参（`common/js/dsh-plugins/memory-service/src/wiki-client.ts`、`wiki-operations.ts`）；`PlannerMemoryService.load` 绑定 `(template, session)`，三个 wiki 方法转发双参（`service.ts`）。模型面三工具定义不含作用域参数（作用域由绑定解析）。
- **memory 校验现状**：`Memory.content` **无任何长度校验**（`projects/game/memory/domain/model.go` 仅校验 memory_id 字符集）；快照注入最近更新 top-10（`common/js/dsh-plugins/memory-service/src/snapshot.ts` `SNAPSHOT_ENTRY_LIMIT = 10`）。
- **memory 批量机制**：`operations.ts` 的 `applyBatch` 已是两段式——preflight 对全量操作做零写入校验（空 content 等在此拒绝、全批不写），commit 逐条执行；单条路径 `applyAdd/applyReplace` 有 inline 空 content 检查并返回领域文本（如 `EMPTY_CONTENT_TEXT`）。
- **web 前端**：vite + React + `@deepseek-ai/dsh-client-ui-primitives`（可用：Button/Input/Modal/Pill/StateDot/RiskConfirmation 等；无专用 Textarea——`PresetsView.tsx` 对 persona 用原生 `<textarea>` 配主题样式）。视图切换为侧栏 state（`App.tsx` `AppView = 'sessions' | 'presets'`）；`PresetsView` 是非 session 管理视图的既有模式（独占编辑表单 + inline 二步删除确认 + loading/error/空态）。API 客户端模式：`src/api/*.ts` 相对路径 fetch + `requestJson`/`ApiError`（protojson camelCase）；PATCH 的 `update_mask` 经 query 参数传递（`updatePreset` 先例：`?update_mask=persona`）。
- **codegen 能力**：template 级子资源有直接先例——session 本身即 template 子资源（session handler 以 `game.ParseTemplateName(req.GetParent())` 解析 `templates/{template}` parent），agent_v2.proto 的 Preset 为 `templates/{template}/presets/{preset}`。protoc-gen-go-aip（`com_github_protoc_contrib_protoc_gen_go_aip`）对 `templates/{template}/wikis/{wiki}` 会生成 `WikiName`（`ParseWikiName`、`Parent() → TemplateName`）。

## 2. 决策 R1：Wiki 资源形态——template 级子资源

- **Decision**: 资源 pattern 改为 `templates/{template}/wikis/{wiki}`（type `game.liukexin.com/Wiki` 不变），HTTP Prefix `/api/v1/templates/{template}/wikis`；`CreateWikiRequest.parent` 注解 `{parent=templates/*}`；handler 父校验改为 `game.ParseTemplateName(parent)` + `gameconst.ValidateTemplateName`（与 session handler 既有父校验同构）；name 解析后 `name.Parent()` 直接是 TemplateName（原 `name.Parent().Parent()` 两跳取消）。六 RPC 语义（校验、唯一性、分页、排序、错误映射）不变。
- **Rationale**: template 已是资源层级中的一等 parent（session 的 parent 即 template）；无 session 存在性校验的既有事实使作用域提升不引入新的父校验负担。
- **Alternatives**: 保留 session 级资源 + 服务端聚合视图（否决——资源模型与"template 级共享"需求直接冲突）；新开 service（否决——同域同存储，068 单服务线延续）。

## 3. 决策 R2：存储索引与存量处置——clean break 的确定性落地

- **Decision**: 集合 `game_memory.wikis` 名不变；文档**去除 `session_id` 字段**；三个索引改为 unique `(template, wiki_id)`、unique `(template, title_norm)`、multikey `(template, title_grams)`。`NewWikiRepository` 启动时幂等清理：`Indexes().DropOne` 三个旧 session 维度索引（`template_1_session_id_1_wiki_id_1` 等）+ `DeleteMany({session_id: {$exists: true}})` 清除旧作用域文档。
- **Rationale**: 用户裁定存量丢弃（spec Clarifications 2026-09-20）；旧文档携带 `template/title_grams`，若残留会被新作用域查询命中、污染 template 级集合——必须代码内确定性清除，不能依赖运维手动步骤；启动清理为幂等 no-op（清理完成后零开销），是"清空"而非"转换"，与"不写迁移"裁定相容。旧索引 dropping 保证存储终态无演进残留（宪法 VII）。
- **Alternatives**: 运维手动 drop 集合（否决——易遗忘、残留即污染）；集合改名如 `template_wikis`（否决——旧集合残留 + 名字携带演进痕迹）；迁移合并（用户已否决）。

## 4. 决策 R3：memory 1024 rune 上限的双层执行

- **Decision**: 服务端 `domain.MaxMemoryContentRunes = 1024` + `ValidateMemoryContent`（`utf8.RuneCountInString`，对齐 wiki title 的 rune 度量），Create/Update handler 调用，超限 INVALID_ARGUMENT（错误信息含上限与实际长度）；JS 工具层预检——`applyAdd`/`applyReplace` 单条路径与 `applyBatch` preflight 增加长度检查，超限返回领域文本结果（文本以 `specs/069-wiki-template-web/contracts/memory-content-limit.md` §3 为准：`memory: content exceeds 1024 characters (got N). Keep entries short; store longer knowledge with wiki_write.`），批量场景全批不写。
- **Rationale**: FR-008~010。批量原子性依赖预检（服务端中途拒绝会留下部分写入，破坏 068 修订后的两段式语义）；FR-009 的"含上限说明 + wiki 引导"文本由 JS 预检产出（服务端 gRPC 错误消息不含 wiki 引导）；服务端校验兜底 REST 直写客户端。wiki 预检先例：`WIKI_CONTENT_MAX_BYTES` 于 `wiki-operations.ts` preflight。
- **Alternatives**: 仅服务端校验（否决——批量原子破坏 + 文本无引导）；仅 JS 预检（否决——REST 直写绕过上限）。

## 5. 决策 R4：JS wiki 面作用域签名收敛

- **Decision**: `WikiStore` 接口、`wiki-client.ts`（name 构造/解析）、`wiki-operations.ts`（search/get/write 核）全部去 session 参数（template 为唯一作用域参数）；`PlannerMemoryService.load` 绑定保持 `(template, session)`（memory 消费 session），三个 wiki 方法仅转发 `scope.template`；三个工具定义（参数 schema、模型面描述）不变。
- **Rationale**: FR-003——模型面零变化，底层作用域收敛为 template；scope 绑定的 fail-loud 语义（未 load 即调用报错）对 wiki 继续生效（经 memory 绑定间接持有 template）。
- **Alternatives**: wiki 独立 scope 绑定（否决——引入冗余状态面）；工具面增加 template 参数（否决——违反模型面不变约定）。

## 6. 决策 R5：web wiki 视图——沿用 PresetsView 模式

- **Decision**: 新增 `projects/game/web/frontend/src/api/wiki.ts`——create（`POST /api/v1/templates/{t}/wikis?wiki_id=...`，body=wiki）、list（GET 分页）、search（`GET .../wikis:search?query=`）、get、update（`PATCH /api/v1/{wiki.name=templates/*/wikis/*}?update_mask=<改动字段>`，body=wiki——对齐 `updatePreset` 的 mask query 先例）、delete；错误经 `ApiError`（grpc-gateway HTTP 映射）。新增 `src/components/WikiView.tsx`——搜索框（Input）+ 分页列表（title + update_time + 操作）+ 独占创建/编辑表单（title=Input、content=原生 `<textarea>` 主题样式，PresetsView persona 先例）+ inline 二步删除确认 + loading/error/空态。`App.tsx`：`AppView` 增加 `'wiki'`、侧栏第三切换按钮、主区条件渲染 `<WikiView template={TEMPLATE_SAOLEI} />`。无搜索关键词时走 List（服务端固定 id 升序——spec Clarifications 裁定）。
- **Rationale**: FR-005~007；`/api/v1` 同源相对路径零 CORS（web README 既有拓扑）；PresetsView 恰是"非 session 的 template 级管理视图"既有形态，交互语汇（独占表单、二步删除、错误呈现）与仓库 051/054 web 契约一致。
- **Alternatives**: session 面板内嵌 wiki 管理（否决——作用域已 template 级，逐 session 翻找违背需求本意）；template 切换器（spec Assumptions 已排除——knownTemplateIDs 仅 saolei）。

## 7. 决策 R6：模型面文本修订——上限入分工段、跨会话措辞微调

- **Decision**: `MEMORY_GUIDANCE`（分工差异段）增加一句英文说明：memory 条目保持简短（每条约 1000 字符上限的约数表述），更长的知识写入 wiki；同段 wiki 侧措辞由 "worth keeping beyond the current moment" 微调为明确跨会话持久（如 "persists across sessions"）。工具描述与 `WIKI_GUIDANCE`（用法段）不动。
- **Rationale**: FR-011（clarify 2026-09-20 裁定 A——指引预先说明）；分工段正是 memory/wiki 边界的唯一载体，上限即边界的量化；跨会话持久是 template 作用域对模型的实质语义变化（US1），应在分工段可感知。约数表述避免模型依赖精确值（服务端仍是精确 1024）。
- **Alternatives**: 上限写入 memory 工具描述（否决——FR-013 工具描述仅述自身，且描述已承载 top-10 说明）；写入 `WIKI_GUIDANCE`（否决——那是跨工具用法段）。

## 8. 决策 R7：契约修订组织——069 自有契约 + 基线修订声明

- **Decision**: 本特性产出四份契约（`specs/069-wiki-template-web/contracts/`）：`wiki-service.md`（template 级 WikiService 全量契约，头部声明取代 068 同名契约）、`memory-content-limit.md`（新上限契约）、`dsh-plugins.md`（修订 068 dsh-plugins 契约：WikiStore 签名、guidance 措辞、预检扩展）、`web-wiki-view.md`（新 web 视图契约）。**不回改** 068/064 的契约文件。
- **Rationale**: 仓库先例——068 的 `contracts/dsh-plugins.md` 头部以"基线修订"声明推翻 064 §2 item 4，未改动 064 原文件；契约演进以"新契约声明取代"表达，特性目录保持交付时快照。
- **Alternatives**: 原地编辑 `specs/068-wiki-round-pause/contracts/*.md`（否决——无仓库先例，且模糊特性边界）。

## 9. 决策 R8：测试策略分层

- **Decision**: Go 单测——`ValidateMemoryContent` case 表（空/边界 1024/1025/多字节中文）、fake collection 的 wiki 仓储新索引与启动清理断言、handler 错误映射（超限 INVALID_ARGUMENT、template 作用域 ALREADY_EXISTS）；JS 单测——wiki ops 以 template 单参寻址（fake store 断言）、memory 预检文本（超限含上限与 wiki 引导、批量原子）、guidance 文本断言（分工段含上限句与跨会话措辞、用法段不含）；web vitest——`api/wiki.ts` fetch mock 请求形状、`WikiView` 组件闭环（搜索/创建/编辑/删除/错误呈现）、`App` 三视图切换；testplan——`wiki_test.go` 全面改 template 级路由（CRUD/List/Search/分页/错误用例），`memory_test.go` 增加 >1024 rune content 拒绝用例；`system_test.yaml` 的 web_test 不动（托管面 smoke 与本特性无关，REST 闭环由 wiki_test.go 覆盖、UI 逻辑由 vitest 覆盖）。
- **Rationale**: 宪法 IV（小颗粒高频）/VI（大型测试实际执行）；跨 session 可见性在 REST 层即 template 级 CRUD（同 parent），工具层跨 session 语义由 JS 单测以不同 scope 驱动验证。
- **Alternatives**: web UI 走 testplan HTTP 级闭环（否决——无浏览器面，API 闭环已由 wiki_test.go 覆盖，避免重复）；为上限增加独立 testplan 文件（否决——并入 memory_test.go 即可）。

## 10. 需用户裁定的事项

无。spec 澄清（上限 1024 rune、存量 clean break、浏览排序保持 List 语义、指引预先说明）已全部在 specify/clarify 阶段裁定；本文件各决策均为其技术落地，无新增歧义。
