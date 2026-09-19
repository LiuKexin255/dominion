# Contract: WikiService——wiki 资源与检索 API

> **Feature**: specs/068-wiki-round-pause/spec.md（FR-001~005、FR-017）
> **服务端锚点**: `projects/game/game.proto`（WikiService + Wiki + 请求消息）、`projects/game/memory/{handler,domain,runtime/mongo}`、`projects/game/gateway/cmd/main.go`
> **基线**: `survey/game-wiki-knowledge-base.md` §5（决策 D1–D9）；AIP：[121](https://google.aip.dev/121)/[131](https://google.aip.dev/131)/[132](https://google.aip.dev/132)/[133](https://google.aip.dev/133)/[134](https://google.aip.dev/134)/[135](https://google.aip.dev/135)/[136](https://google.aip.dev/136)/[158](https://google.aip.dev/158)/[193](https://google.aip.dev/193)

## 1. 资源与标识

- 资源：`Wiki { name, title, content, create_time, update_time }`，pattern `templates/{template}/sessions/{session}/wikis/{wiki}`，type `game.liukexin.com/Wiki`。
- **标识形态（FR-017）**：资源消息无独立 `wiki_id` 字段——id 由 name 的 `{wiki}` 段承载（AIP-133：`{resource}_id` 属于 Create 请求；仓库先例：Session 于 specs/035 移除 session_id）。
- **标识分层**：agent/工具面一律以 title 寻址（见 [dsh-plugins.md](dsh-plugins.md) §3）；后端面以 name 寻址。
- 作用域：`(template, session)`，与 memory 相同；集合 `game_memory.wikis`（同服务同库）。

## 2. RPC 面（六方法）

| RPC | AIP | HTTP | 要点 |
|---|---|---|---|
| `CreateWiki` | [133](https://google.aip.dev/133) | `POST /api/v1/{parent=templates/*/sessions/*}/wikis`，body=wiki | 请求 `{parent, wiki_id(REQUIRED), wiki}`；`wiki_id` 由 agent 侧生成（sha256(title_norm)[:32]，见 data-model §5）；title 规范化重复 → ALREADY_EXISTS；返回完整 Wiki |
| `GetWiki` | [131](https://google.aip.dev/131) | `GET /api/v1/{name=templates/*/sessions/*/wikis/*}` | 返回完整资源（唯一返回 content 的标准读路径） |
| `UpdateWiki` | [134](https://google.aip.dev/134) | `PATCH /api/v1/{name=...}`，body=wiki | title/content 可变（`update_mask` 支持两者）；title 变更重生成 grams 并保持唯一；不存在 → NOT_FOUND |
| `DeleteWiki` | [135](https://google.aip.dev/135) | `DELETE /api/v1/{name=...}` | 按 name 删除；目标不存在 → NOT_FOUND（AIP-135 默认语义，无 `allow_missing`） |
| `ListWikis` | [132](https://google.aip.dev/132) | `GET /api/v1/{parent=...}/wikis` | **全量获取语义**：返回完整资源（含 content）；**无 order_by**，固定 `wiki_id asc`（唯一身份索引支撑）；AIP-158 游标分页（固定单键 seek），`page_size` 默认 20、上限 100 |
| `SearchWikis` | [136](https://google.aip.dev/136) 自定义方法 | `GET /api/v1/{parent=...}/wikis:search` | **检索**：`query` 参数；轻量响应（条目 `{name, title}`，无 content）；§4 分页 |

List 不提供 order_by 的依据：wiki_id 是 memory_id 的对标（name 末段身份），排序/游标机制极简化；仓库先例 `ListSessionsRequest` 亦无 order_by（调研 D8）。

## 3. 校验与错误（AIP-193）

| 情形 | 状态 |
|---|---|
| `wiki_id` 不匹配 `[a-z0-9_-]+`；title 空 / > 128 rune；content 空 / > 64KB；Search 空 query；Search 续页 query 与 token 不一致；坏 page token | INVALID_ARGUMENT |
| title 规范化后在作用域内重复（Create 或 Update 改 title） | ALREADY_EXISTS |
| Get/Update/Delete 目标不存在 | NOT_FOUND |
| 父资源 session 不存在 | NOT_FOUND（对齐 memory handler 既有父校验语义） |

## 4. Search 契约

1. **匹配语义**：规范化子串匹配——query 与 title 均经 `NFKC + 小写`（data-model §2.1）；大小写/全半角不敏感、任意位置、支持中文。
2. **排序**（确定性全序，声明于 proto 注释）：精确相等 > 前缀 > 子串；同分按 `title_norm` 升序（UTF-8 码点序，非拼音/locale 序）。
3. **响应**：`{ wikis: [{name, title}], next_page_token }`——仅此两字段的轻量条目（AIP-136 自定义响应形态；无 content）。
4. **分页**：token = base64url `{q: 规范化 query, offset}`；续页 query 规范化后必须与 token 内 `q` 一致（对齐 AIP-158 token 与请求上下文匹配的精神），否则 INVALID_ARGUMENT；`page_size` 默认 20、上限 100。与 List token 形态独立。
5. **实现策略**（非契约）：bigram `$all` 预过滤 + 服务端 `strings.Contains` 验证 + Go 侧评分；< 2 rune query 回退 QuoteMeta regex；redis 快照缓存纯优化、miss 降级逐页重执行（survey §5.1.1，research §5）。

## 5. 存储与索引

见 data-model.md §2（文档结构、规范化/bigram 纯函数、三个索引）。仓储 seam 保持 `collectionOps` 形态（无 Aggregate 依赖——`$all` 是 filter 非管道，survey §4.4）；fake collection 单测模式仿 `projects/game/memory/runtime/mongo/repository_test.go`。

## 6. 部署面

- 同 memory 服务二进制：cmd 注册 `WikiServiceServer`，mongo client/db 复用；gateway 于 `gameconst.MemoryTarget` 同 target 注册 WikiService REST 面。
- 无存量数据迁移（新集合）；索引启动时创建。

## 7. 验收锚点

- Go 单测：domain 纯函数 case 表（中文/全角/大小写/emoji/短查询）、token 编解码与 query 匹配校验、fake collection 的 `$all` filter 断言、handler 错误映射。
- testplan 大型测试（`projects/game/testplan/wiki_test.go`，仿 memory_test.go）：Create/Get/Update/Delete、title 唯一冲突、List 固定序 + 分页、Search 子串/中文/大小写/短查询/续页一致性、超限拒绝——quickstart.md 场景 1。
