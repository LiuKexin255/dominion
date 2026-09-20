# Contract: memory 插件修订（三 wiki 工具 + prompt sections + 服务面扩展）

> **Feature**: specs/068-wiki-round-pause/spec.md（FR-006~013）
> **锚点**: `common/js/dsh-plugins/memory/`、`common/js/dsh-plugins/memory-service/`、`projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml`
> **基线修订**: `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（本文生效后其 §2 item 4 "不注册 guidance section" 由本特性推翻；§1 包与挂载面结构不变）

## 1. 包与挂载面（不变 + 增量）

| 插件 | 包 | cordis 名 | 变化 |
|---|---|---|---|
| 基建/服务面 | `@dominion/dsh-memory-service` | `memory` | provide `ctx.plannerMemory`——面扩展 wiki 方法（§4） |
| 工具行 | `@dominion/dsh-memory` | `memory-row` | inject 不变（`plannerMemory/tools/systemPrompt`）；注册内容扩展（§2） |

挂载面不变：host 行 `cordis.yml`（agent_v2）；planner 池模板行（`agent.cordis.yml` 的 `- { id: memory, name: '@dominion/dsh-memory' }`）；templateRules（player required saolei / forbidden memory；planner 反之）。**wiki 工具随 memory 行自动仅挂 planner**（FR-010）。

## 2. 工具行注册内容（一行注册的全部模型面）

1. `memory` 工具——描述修订（§3），参数不变（add/replace/remove + operations 批量）；批量提交中断行为随本特性修订（失败消息如实列出已应用部分，见 §5）。
2. `wiki_search` 工具——参数 `{ query }`（唯一参数）；执行 `plannerMemory.wikiSearch`：内部以 `page_size`=100 折叠 `next_page_token` 直至取全命中标题（会话规模上界见 `survey/game-wiki-knowledge-base.md` §5.1.1、research.md §5/R6）；结果 = 命中标题列表文本（无 content、无 name）；空结果给"未命中 + 建议换关键词"引导。
3. `wiki_get` 工具——参数 `{ titles: string[] }`；执行 `wikiGet`：每标题规范化精确解析（SearchWikis 结果中 `title_norm` 相等者）→ GetWiki；结果 = 命中条目完整 content + 未命中标题引导。
4. `wiki_write` 工具——参数 单条 `{ title, content }` XOR 批量 `{ entries: [{title, content}] }`（互斥为运行时契约，文本应答）；执行 `wikiWrite`：存在（精确解析命中）→ UpdateWiki；不存在 → CreateWiki（`wiki_id = sha256(title_norm)[:32]`）；批量：任一条目校验失败则全批不写（preflight 映射 memory `applyBatch` 两段式），提交阶段逐条执行、无事务回滚，基础设施故障中断时失败消息如实列出已写入条目；结果 = 每条目 created/updated；空 `entries` 数组 → 引导文本（不提交）。
5. `memory:snapshot` section（order 200，既有，不变）。
6. `memory:guidance` section（order 110，新）——memory vs wiki 分工差异（§3）。
7. `wiki:guidance` section（order 120，新）——wiki 三工具整体用法（§3）。

工具结果 schema/渲染：三者均为 `{result: string}` + 文本渲染（对齐 memory 工具）；基础设施故障 → `wiki failed: …` 文本，永不抛工具错误（FR-011）；任何模型可见文本不含 `name`/`wiki_id`（FR-009）。

## 3. prompt 文本（全部英文，FR-012/013）

- **memory 工具描述（修订）**：通用表述（无 `planner` 等场景名词、无 hermes 等外部措辞）；说明快照仅保留最近更新的 10 条、写操作作用于全量存储且不刷新快照；仅描述自身功能。
- **`memory:guidance`（分工差异段）**：仅描述两工具分工——memory 是常驻系统提示的少量近期工作记忆（快照按近期性截断 top-10），wiki 是不自动进入上下文的海量知识库（按需检索取回）；不重复任一工具用法/参数；不引入 memGPT/Letta 等第三方框架术语。
- **`wiki:guidance`（整体用法段）**：三工具配合流程——`wiki_search` 发现有什么 → `wiki_get` 取回所需内容 → `wiki_write` 沉淀/更新知识（upsert 语义）；不重复分工差异段内容。
- **三 wiki 工具描述**：各仅述自身功能（`wiki_search`：标题模糊检索返回标题列表；`wiki_get`：按标题批量取回内容；`wiki_write`：按标题批量写入或更新）。

## 4. 服务面扩展（`PlannerMemoryService`）

```text
load(agentCtx, scope)                              // 既有；绑定 (template, session)——wiki 共用
applyCall(agent, args)                             // 既有（memory 工具）
snapshot(scope)                                    // 既有
wikiSearch(agent, query): Promise<string>          // 新增
wikiGet(agent, titles[]): Promise<string>          // 新增
wikiWrite(agent, input): Promise<string>           // 新增（单条 XOR 批量）
```

- wiki 方法与 `applyCall` 同约束：未绑定 scope 的 agent 调用 → throw（工具面转 `wiki failed` 文本）；基础设施失败 reject、领域结果为文本。
- 存储 seam：`WikiStore { searchWikis, getWiki, createWiki, updateWiki }`（client 实现，gRPC `dominion:///game/memory:50051`，proto-loader/凭证/round_robin 复用 `MemoryClient` 通道形态——`common/js/dsh-plugins/memory-service/src/client.ts`）。
- 编排层零改动：orchestrator 的 `loadPlannerMemory` 注入不变（research §4）。

## 5. 不变量与修订声明

- 快照语义不变：wiki 不注入任何系统提示内容（无路标/标题索引注入，后续可选）。
- memory 工具的操作语义、snapshot section、064 §1 包结构/inject 面不变；`operations` 的批量提交中断失败消息修订见 §2 item 1。
- **064 §2 item 1 修订**：其对 `operations` 的「批量原子」措辞由本特性（068）裁定改为两段式表述——preflight 任一条目校验失败则全批不写；提交阶段逐条执行、无事务回滚，中断时失败消息如实列出已应用部分（`specs/068-wiki-round-pause/spec.md` Edge Cases「批量 write 的原子性」）。
- **064 §2 item 4 修订**：由"不注册 guidance section（单工具无跨调用协调需求）"改为"注册 `memory:guidance` 与 `wiki:guidance` 两个 section（memory 与 wiki 工具组间的跨工具协调需求）"。
