# Research: memory 作用域从 session 迁移至 preset、web memory 管理与侧栏二级导航

**Feature**: specs/071-memory-preset-scope/spec.md | **先例基线**: specs/069-wiki-template-web/research.md（wiki session→template 作用域迁移同型裁定的直接参照）

## 1. 现状盘点（调研结论）

- **服务端**：MemoryService（`projects/game/game.proto`）由独立 Go 服务 `projects/game/memory` 承载（`handler/handler.go` + `domain` + `runtime/mongo`），经 gateway `/api/v1` 暴露；作用域键 `(template, session_id)`，唯一索引 `(template, session_id, memory_id)`，排序索引 `(template, session_id, update_time desc, memory_id asc)`；parent 解析 `game.ParseSessionName` + `gameconst.ValidateTemplateName`，**不校验 session 存在性**。
- **工具面**：JS 宿主插件 `@dominion/dsh-memory-service`（`common/js/dsh-plugins/memory-service/`）以 `PlannerMemoryScope {template, session}` 绑定 agent 与集合；快照 = 一次 `listMemories(order_by=update_time desc, page_size=10)` 冻结缓存；写路径 `operations.ts` 以 `generateMemoryId = sha256(content).hex[:32]` 派生 id、ALREADY_EXISTS 即同内容去重成功。工具插件 `@dominion/dsh-memory`（`common/js/dsh-plugins/memory/`）挂载于 planner 角色池模板（player 被 templateRules 禁挂）。
- **物化装配**：`agent_v2/src/session.ts` `doMaterialize` 以 session 两半构建 `memoryScope` 传给 `saolei-loop` orchestrator，planner member 的 `setup` 钩子 `loadPlannerMemory`；同函数已解析 `planner.preset` 的 id（`presetId()`，供 `compose`）。wiki 面（069）已收敛为 template 单参——`service.ts` 中 wiki 调用仅转发 `scope.template`。
- **preset 面**：PresetService（`agent_v2.proto`）为 agent_v2 自有状态（mongo `game_agent_v2.presets`），gateway 直连（无 proxy 亲和）；preset pattern `templates/{template}/presets/{preset}` 与 game.proto 同 proto package、同 Go codegen 单元（`projects/game/BUILD.bazel`：两 proto 一个 `go_proto_library`，protoc-gen-go-aip 为每个资源生成 `Parse*Name`——Preset 的解析函数已在 `dominion/projects/game` 包内可用，如 `game.ParsePresetName`；template 级父解析先例见 `handler/wiki_handler.go` 的 `game.ParseTemplateName`）。
- **web**：`App.tsx` 侧栏底部 `view-switch` 平铺 sessions/presets/wiki 三 tab；管理视图模式先例 `PresetsView`/`WikiView`（presets-* 样式类、独占表单、inline 二步删除）；`api/agent.ts` 已有 `listPresets`；`api/wiki.ts` 是 REST 客户端模式先例（protojson 投影、mask query、`sha256(title_norm)[:32]` id 派生）。
- **测试**：Go 单测（handler/domain/mongo fake）、JS 单测（vi.fn DI）、vitest（web）、testplan（`system_test.yaml` `game-system` suite 含 `memory_test.go` 与 `agent_v2_preset_test.go`；`game-memory-down` suite 验证物化 fail-loud）。
- **场景化文案**：`WikiView.tsx` 空态含 "planner 也能检索到"（L404）、创建表单占位 "如 开局-左上角"（L311）；工具描述与指引文本（LLM 面）经检查无场景措辞（`tool.ts`/`guidance.ts` 的 MEMORY_TOOL_DESCRIPTION scope-agnostic；指引中的 saolei 字样仅存在于代码注释的 band 排布说明，非模型可见文本）。

## 2. 决策 R1：Memory 资源形态——preset 级子资源，留在 game.proto v1 面

**Decision**: Memory pattern 迁移为 `templates/{template}/presets/{preset}/memories/{memory}`，MemoryService 定义与 `/api/v1` 前缀不变；parent 解析改用 `game.ParsePresetName`（同包 codegen 产物），template 校验沿用 `gameconst.ValidateTemplateName(presetName.Parent())`。

**Rationale**: 服务端承载在 game/memory Go 服务，与 agent_v2（preset 的 owner）无部署耦合；069 将 wiki 提升为 template 级时同样留在 game.proto v1 面，仅变更 parent 段形态。Preset 资源虽声明于 agent_v2.proto，但两 proto 同属一个 codegen 单元（`projects/game/BUILD.bazel`），`ParsePresetName` 已在包内可用，无跨文件依赖问题（pattern 是字符串，resource_reference 用 child_type 自包含，不引用 Preset 类型——避免 game.proto→agent_v2.proto 的循环 import）。

**Alternatives considered**: ① 迁到 agent_v2.proto / v2 路径——rejected：引入服务归属与版本面双改动，且 agent_v2.proto 无法反向被 game.proto 引用（循环）；② 保持 v1 路径但自写 parent 解析——rejected：仓库规范为 codegen 驱动解析（`specs/035-proto-contract-refine/data-model.md` §1.1），无手写解析先例。

## 3. 决策 R2：存储终态与一次性清理——旧索引/旧文档清除不进服务代码

**Decision**: `memories` 文档以 `preset_id` 替换 `session_id`；索引重建为 unique `(template, preset_id, memory_id)` + `(template, preset_id, update_time desc, memory_id asc)`，由服务启动创建（`runtime/mongo/repository.go`）；legacy session 维度索引与含 `session_id` 旧文档的清除是新服务首次启动前单独执行的一次性运维清理（mongosh，步骤与顺序约束见 `specs/071-memory-preset-scope/quickstart.md` 场景 7 / `specs/071-memory-preset-scope/data-model.md` §2.3），服务代码不承载清理逻辑——`runtime/mongo/wiki_repository.go`（069）既有的启动清理代码同批移除（用户裁定：一次性清理项不放到代码里，等部署完单独清理）。

**Rationale**: spec FR-004 裁定 clean break 弃用（session→preset 无可靠归属映射，迁移不可定义）——弃用语义不变；清理本身是一次性事项，驻留服务代码即把一次性动作永久化进每次启动路径。清理须先于新服务首次启动执行：unique `(template, preset_id, memory_id)` 索引于启动时创建，legacy 文档缺 `preset_id` 按 null 参与唯一性，旧数据尚存会使索引构建失败（E11000）且被启动代码按已存在忽略（顺序约束见 `specs/071-memory-preset-scope/quickstart.md` 场景 7）。

**Alternatives considered**: 服务启动幂等清理（drop legacy 索引 + `DeleteMany({session_id: {$exists: true}})`，镜像 wiki_repository 机制）——rejected（用户裁定：一次性清理项不进代码）；保留旧文档不清理——rejected：残文档携带已废除的 `session_id` 字段，违反终态原则 VII 且污染集合；字段重命名 + 数据搬运——rejected：映射不可定义（见 spec Assumptions）。

## 4. 决策 R3：服务端不做 preset 存在性校验

**Decision**: 与现行 session 口径一致——仅解析 preset 资源名 + 校验 template 合法性；不调用 agent_v2 校验 preset 存在。web 管理面的 preset 选择器天然只列出真实 preset，被删 preset 的集合不再可达（数据保留，spec FR-010/FR-012）。

**Rationale**: 服务间按资源名寻址、保持零耦合（memory 服务对 agent_v2 无任何依赖方向）；名称寻址下"写入不存在的 parent"等价于为未来重建的 preset 预写集合，与 FR-010"同 id 重建后重新可用"自洽。

**Alternatives considered**: memory 服务调用 preset 面做存在性校验——rejected：引入跨服务运行时依赖与故障耦合（memory-down 拓扑外再添 preset-down 分支），收益仅是拒绝无主写入，而该写入本身无害。

## 5. 决策 R4：JS 作用域模型——`PlannerMemoryScope {template, preset}`，装配权留在 agent_v2

**Decision**: `memory-service` 与 `saolei-loop` 的 `PlannerMemoryScope` 收敛为 `{template, preset}`；`TeamMaterializeOptions.memoryScope` 选项保留（saolei-loop 保持场景无关 team 原语，不知道"planner=记忆角色"以外的场景绑定），`agent_v2/src/session.ts` `doMaterialize` 改为 `memoryScope: { template: parsedSession.template, preset: this.presetId(planner.preset) }` 构建。client 的 `memoryName`/parent 构造、`operations.ts` 全部签名随迁（`session` 参数→`preset`）；wiki 转发不变（仅 `scope.template`）。

**Rationale**: 物化输入里 planner preset id 已被解析（`presetId()`），scope 两半在同一处可得；saolei-loop 不引入 preset 语义（它只接受装配好的 scope），改动面最小且层次不变。team 刷新换 planner preset 时 `UpdateTeam` 走同一 `doMaterialize` 路径，FR-009 自然成立。

**Alternatives considered**: ① orchestrator 内部从 `options.planner.preset` 派生 memoryScope、删除独立选项——rejected：把"记忆绑定在 planner 角色"的场景知识下沉进 team 原语，违背 059 的 scene-agnostic 分层；② scope 传完整 preset 资源名——rejected：两半（业务 id）形态是 T023 既有裁定（传全名会双重前缀），仅替换 session 半即可。

## 6. 决策 R5：模型面文本——分工段补 memory 跨局持久，工具描述不动

**Decision**: `guidance.ts` `MEMORY_GUIDANCE` 分工段修订：memory 补充"跨局持久（同一 preset 的所有对局共享，写错会持续影响后续每一局）"属性，与 wiki 的跨会话持久表述对齐分工边界（memory=每轮都要用的近期要点、wiki=偶尔深查的大块知识）；`MEMORY_TOOL_DESCRIPTION` 不改（已 scope-agnostic："full stored memory"）。

**Rationale**: 作用域迁移改变了模型可见的行为事实（记忆不再随局丢弃），模型面必须知情——否则模型会按"本局工作笔记"心智写长期无价值的条目或漏用；工具描述本身无 session 字样，零改动即终态。

**Alternatives considered**: 全部模型面文本不动——rejected：分工段明确写着 memory 是 "recent working notes"、跨会话持久只归于 wiki，与 preset 级持久的事实矛盾（终态原则 VII 要求表述与现状一致）。

## 7. 决策 R6：web 创建 id 派生——前端 WebCrypto 复刻工具路径摘要

**Decision**: `api/memory.ts` `createMemory(template, preset, content)` 以 `crypto.subtle.digest('SHA-256', utf8(content))` → hex → `slice(0, 32)` 派生 memory_id（与 `memory-service/src/operations.ts` `generateMemoryId` 的 node `createHash` 同算法同输入同值域）；表单仅 content 字段；同内容重复创建命中 409 呈现"已存在"提示（与工具路径 dedupe 语义对偶，spec clarify Q1 裁定）。

**Rationale**: SHA-256 对同字节输入在两种实现下产生相同摘要，摘要值即契约；web 与 agent 两条写入路径对同一内容生成同一 id，天然保证"同内容同条目"的全局一致性。

**Alternatives considered**: ① 用户手输 id——rejected（clarify 已裁）；② 服务端补自动生成语义——rejected：CreateMemoryRequest.memory_id 为调用方提供（AIP-133 user-specified id），新增服务端生成与既有契约冲突。

## 8. 决策 R7：web memory 视图——WikiView/PresetsView 模式 + preset 选择器

**Decision**: `MemoryView` 复用管理视图交互语汇（presets-* 样式类、独占创建/编辑表单、inline 二步删除、loading/error/空态）；顶部 preset 选择器数据来自既有 `api/agent.ts` `listPresets`（不按 role 过滤——场景无关）；列表默认 `order_by=update_time desc`、`page_size=20`（clarify Q3 裁定 + 与 wiki 视图页宽一致；服务端默认 100/上限 1000 不受影响）；条目详情直接取列表数据（Memory 无单条 Get RPC，List 返回全量资源）；无搜索框（memory 无 title 字段）。文案全部中性（"agent/助手"指代消费方，示例中性）。

**Rationale**: 069 web wiki 视图已验证该模式的组件结构与测试形态；preset 选择器复用 v2 面既有 API 零新增服务端面。

**Alternatives considered**: ① 列表外加独立详情请求——rejected：无 Get RPC，为详情加 RPC 属能力蔓延；② 按 role=planner 过滤选择器——rejected：把场景角色词引入 memory 视图，违反场景无关裁定（spec FR-016）。

## 9. 决策 R8：侧栏两级导航——浮出菜单（popover）

**Decision**: `App.tsx` 侧栏底部改造：一级 = sessions/presets 两个直达按钮 + 一个固定的「知识」入口按钮；点击入口弹出浮层菜单（wiki、memory 两项），点选后切换视图并关闭浮层，点击浮层外部仅关闭不改视图；入口按钮呈现当前选中视图；`AppView` 状态扩展为四视图（sessions/presets/wiki/memory）+ 浮层开闭瞬态。样式在 `theme.css` 补充（浮层定位/层级）。

**Rationale**: spec clarify Q2 用户裁定 popover——设计目标是侧栏底部行数恒定、不随视图增多变长（分组展开在展开时增加行数，被否决）；外点关闭与选中态保持是 popover 的标准语义补全，已落 FR-015/US5 场景。

**Alternatives considered**: 侧栏内分组展开——rejected（用户裁定，见 Clarifications Session 2026-09-28）；顶栏下拉——rejected：视图导航收敛于侧栏，迁移入口位置引入额外布局改动。

## 10. 决策 R9：测试策略分层

**Decision**: ① Go 单测：handler（preset 父解析/校验/错误映射）、domain（PresetID 模型）、mongo（preset 维度过滤/索引/游标）；② JS 单测：client（preset 寻址/parent 构造）、service（scope 绑定 {template,preset}、wiki 转发不变）、operations（preset 签名、digest 不变）、guidance（跨局持久措辞）、orchestrator/session（装配传递）；③ vitest：`api/memory.ts`（路由/摘要派生/mask）、`MemoryView`（选择器/列表序/表单/删除/场景词扫描）、`WikiView`（文案修正断言/场景词扫描）、`App`（两级导航/浮层行为/互斥渲染）；④ testplan：`memory_test.go` 路径改 preset parent（用合法形态 preset 名即可——无存在性校验）；`agent_v2_preset_test.go` 新增跨 session 共享用例（同 planner preset 两 session：A 写入 → B 物化快照命中，SC-001/SC-004）+ 既有 memory 用例随迁；`gateway/cmd/main_test.go` 路由断言路径更新。

**Rationale**: 分层对齐宪法 IV（小颗粒高频、大型低频验收）；跨 session 回路必须经大型测试（真实部署多服务拓扑），单测只能验证装配逻辑。

**Alternatives considered**: 跨 session 共享仅以 JS 单测双 scope 断言——rejected：不覆盖 gateway→memory 服务→mongo 的真实寻址链，宪法 VI 的端到端验收缺口。

## 11. 决策 R10：gateway 零代码改动，注释与路由测试同步

**Decision**: gateway 注册代码不动（路由随 proto 注解自动生成）；`main_test.go` `TestRootMuxAPIv1SessionMemoryFallback` 的 memories 路径断言改为 preset 形态，注释同步终态。

**Rationale**: 069 同款：gateway 是注解驱动，无手写路由。

**Alternatives considered**: 无（无可选方案）。

## 12. 需用户裁定的事项

无——spec/clarify 已裁定全部关键决策（id 派生、菜单形态、默认排序、容量口径、clean break、无存在性校验）；本 research 的 R1–R10 均为裁定之下的落地设计，且各有仓库先例支撑。
