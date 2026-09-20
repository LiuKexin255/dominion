# Contract: memory 插件修订（wiki 作用域收敛 + memory 上限预检 + 指引措辞）

> **Feature**: specs/069-wiki-template-web/spec.md（FR-003、FR-008~011）
> **基线修订**: `specs/068-wiki-round-pause/contracts/dsh-plugins.md`（本文生效后：§4 服务面 WikiStore/操作核签名去 session 参数；§3 `memory:guidance` 措辞扩展；其余——包结构、挂载面、工具参数 schema、结果文本约定——不变）
> **锚点**: `common/js/dsh-plugins/memory/`、`common/js/dsh-plugins/memory-service/`、`projects/game/agent_v2/preset-templates/planner/planner/agent.cordis.yml`

## 1. 包与挂载面（不变）

`@dominion/dsh-memory-service`（provide `ctx.plannerMemory`）与 `@dominion/dsh-memory`（工具行）双包结构、planner 池模板行挂载、player 禁用规则——全部不变（068 §1）。三个 wiki 工具仍随 memory 行仅挂 planner。

## 2. 服务面（`PlannerMemoryService`）——作用域转发修订

```text
load(agentCtx, scope {template, session})     // 绑定不变（memory 消费 session；wiki 经绑定间接持有 template）
applyCall(agent, args)                        // memory 工具核，不变 + content 预检（§4）
wikiSearch(agent, query)                      // 转发 scope.template（不再转发 session）
wikiGet(agent, titles)                        // 同上
wikiWrite(agent, input)                       // 同上
snapshot(scopeKey)                            // 不变
```

未 load 即调用的 fail-loud 语义对 wiki 方法继续生效（经 memory scope 绑定解析 template）。

## 3. WikiStore 与操作核——template 单参签名

```text
WikiStore（wiki-client.ts）：
  searchWikis(template, query, opts?)
  getWiki(template, wikiId)
  createWiki(template, wikiId, title, content)
  updateWiki(template, wikiId, wiki)
  deleteWiki(template, wikiId)
  listWikis(template, opts?)

wiki-operations.ts：
  wikiSearch(store, template, query)
  wikiGet(store, template, titles)
  wikiWrite(store, template, input)
```

- name 构造/解析：`templates/{template}/wikis/{wikiId}`（`wikiName`/`parseWikiName` 去 session 段）。
- 语义不变项：title 寻址（NFKC + 小写规范化孪生）、`wiki_write` 按规范化 title 的 upsert（`wiki_id = sha256(title_norm)[:32]`）、两段式预检/提交、title ≤ 128 rune / content ≤ 64KB 预检常量、`wiki_search` page_size 100 折叠取尽、结果文本（命中/未命中引导/created-updated 明细）、`wiki failed: …` 基建失败文本、模型可见文本不含 `name`/`wiki_id`。
- 跨 session 语义：同 template 的所有 agent（不同 session 的 planner）经同一 template 参数读写同一集合——后写覆盖先写（upsert 预期）。

## 4. memory 工具核——content 长度预检（新增）

- `MEMORY_CONTENT_MAX_RUNES = 1024`；单条（`applyAdd`/`applyReplace`）与批量 preflight 增加检查，超限返回领域文本（含上限与 `wiki_write` 引导，全文见 [memory-content-limit.md](memory-content-limit.md) §3），批量全批不写。
- 既有行为不变：dedupe、substring 匹配、批量两段式、中断失败如实列出已应用部分。

## 5. prompt 文本修订（英文）

- **`memory:guidance`（分工差异段，order 110）**：
  - memory 侧新增一句：条目保持简短——每条有长度上限（约 1000 字符的约数表述），更长的知识写入 wiki（FR-011，2026-09-20 clarify 裁定"指引预先说明"）。
  - wiki 侧措辞微调：明确知识**跨会话持久**（persists across sessions）——template 作用域的模型可感知语义（US1）。
  - 职责边界不变：不重复工具用法/参数；不引入第三方框架术语。
- **`wiki:guidance`（用法段，order 120）与三个 wiki 工具描述**：不变。
- **memory 工具描述**：不变（top-10 快照说明等 068 FR-012 内容延续；上限说明由分工段承载）。

## 6. 验收锚点

- JS 单测：wiki ops 以 template 单参寻址（fake store 断言 URL/请求形状）；跨 session 语义以两个 scope（同 template 不同 session）驱动 wikiWrite/wikiSearch 断言同一集合；`MEMORY_GUIDANCE` 含上限句与跨会话措辞；memory 预检见 [memory-content-limit.md](memory-content-limit.md) §5。
