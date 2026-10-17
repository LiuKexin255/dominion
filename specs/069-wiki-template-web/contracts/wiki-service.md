# Contract: WikiService——template 级作用域的 wiki 资源与检索 API

> **Feature**: specs/069-wiki-template-web/spec.md（FR-001~004）
> **基线修订**: `specs/068-wiki-round-pause/contracts/wiki-service.md`（本文生效后其全文由本契约取代：资源 pattern/parent/索引/存储文档去 session 维度；六 RPC 语义、校验、检索、分页、错误映射不变）
> **服务端锚点**: `projects/game/game.proto`（WikiService + Wiki + 请求消息）、`projects/game/memory/{handler,domain,runtime/mongo}`、`projects/game/gateway/cmd/main.go`
> **AIP**: [121](https://google.aip.dev/121)/[131](https://google.aip.dev/131)/[132](https://google.aip.dev/132)/[133](https://google.aip.dev/133)/[134](https://google.aip.dev/134)/[135](https://google.aip.dev/135)/[136](https://google.aip.dev/136)/[158](https://google.aip.dev/158)/[193](https://google.aip.dev/193)

## 1. 资源与标识

- 资源：`Wiki { name, title, content, create_time, update_time }`，pattern **`templates/{template}/wikis/{wiki}`**，type `game.liukexin.com/Wiki`。
- 标识形态（068 FR-017 延续）：资源消息无独立 `wiki_id` 字段——id 由 name 的 `{wiki}` 段承载；Create 请求携带 `wiki_id`（REQUIRED，agent/web 侧生成）。
- 标识分层：agent/工具面一律以 title 寻址；后端面（含 web）以 name 寻址。
- **作用域：`template`（单键）**——同一 template 的全部 session 共享同一 wiki 集合；集合 `game_memory.wikis`（同服务同库）。

## 2. RPC 面（六方法，语义不变、parent 变更）

| RPC | AIP | HTTP | 要点 |
|---|---|---|---|
| `CreateWiki` | [133](https://google.aip.dev/133) | `POST /api/v1/{parent=templates/*}/wikis`，body=wiki | 请求 `{parent, wiki_id(REQUIRED), wiki}`；`wiki_id` 生成侧派生 `sha256(title_norm)[:32]`；title 规范化重复（template 作用域）→ ALREADY_EXISTS；返回完整 Wiki |
| `GetWiki` | [131](https://google.aip.dev/131) | `GET /api/v1/{name=templates/*/wikis/*}` | 返回完整资源（唯一返回 content 的标准读路径） |
| `UpdateWiki` | [134](https://google.aip.dev/134) | `PATCH /api/v1/{wiki.name=templates/*/wikis/*}`，body=wiki，`update_mask` 经 query 传递 | title/content 可变；title 变更重生成 grams 并保持 template 作用域唯一；不存在 → NOT_FOUND |
| `DeleteWiki` | [135](https://google.aip.dev/135) | `DELETE /api/v1/{name=...}` | 目标不存在 → NOT_FOUND（无 `allow_missing`） |
| `ListWikis` | [132](https://google.aip.dev/132) | `GET /api/v1/{parent=templates/*}/wikis` | 全量获取语义（含 content）；无 order_by，固定 `wiki_id asc`；AIP-158 游标分页，`page_size` 默认 20、上限 100 |
| `SearchWikis` | [136](https://google.aip.dev/136) | `GET /api/v1/{parent=templates/*}/wikis:search` | `query` 参数；轻量响应 `{name, title}`（无 content）；§4 |

父校验：`parent` 解析为 `TemplateName`（`game.ParseTemplateName`）+ `gameconst.ValidateTemplateName`（已知 template 集）——与 session handler 既有 template 父校验同构；**无 session 存在性校验**（与 068 行为一致，仅层级上移）。

## 3. 校验与错误（AIP-193）

| 情形 | 状态 |
|---|---|
| `wiki_id` 不匹配 `[a-z0-9_-]+`；title 空 / > 128 rune；content 空 / > 64KB；Search 空 query；Search 续页 query 与 token 不一致；坏 page token | INVALID_ARGUMENT |
| title 规范化后在 **template 作用域**内重复（Create 或 Update 改 title，无论条目源自哪个 session） | ALREADY_EXISTS |
| Get/Update/Delete 目标不存在 | NOT_FOUND |
| parent 非已知 template | INVALID_ARGUMENT |

## 4. Search 契约（不变，作用域随 parent）

1. 匹配语义：规范化子串匹配（NFKC + 小写）；大小写/全半角不敏感、任意位置、支持中文。
2. 排序：精确相等 > 前缀 > 子串；同分按 `title_norm` 升序（UTF-8 码点序）。
3. 响应：`{ wikis: [{name, title}], next_page_token }`。
4. 分页：token = base64url `{q, offset}`；续页 query 规范化后须与 token 内 `q` 一致，否则 INVALID_ARGUMENT；`page_size` 默认 20、上限 100。
5. 实现策略（非契约）：bigram `$all` 预过滤 + Go 侧 `strings.Contains` 验证 + 评分；< 2 rune query 回退 regex。

## 5. 存储与索引

见 [data-model.md](../data-model.md) §2（文档无 `session_id`；unique `(template, wiki_id)`、unique `(template, title_norm)`、`(template, title_grams)` multikey；`NewWikiRepository` 启动幂等清理旧 session 维度索引与旧作用域文档——clean break，2026-09-20 用户裁定）。

## 6. 部署面

- 同 memory 服务二进制，gateway 仍于 `gameconst.MemoryTarget` 注册 WikiService REST 面（注册代码零改动，路由随 proto 注解变更）。
- 存量处置：无迁移（§5 启动清理）。

## 7. 验收锚点

- Go 单测：domain 校验 case 表（template 作用域 title 唯一）、fake collection 新索引/启动清理断言、handler 错误映射（parent 为 template 级）。
- testplan（`projects/game/testplan/wiki_test.go` 修订）：template 级路由的 Create/Get/Update/Delete、title 唯一冲突、List 固定序 + 分页、Search 子串/中文/大小写/短查询/续页一致性、超限拒绝。
