# Data Model: wiki 提升为 template 级知识库、memory 单条长度上限与 web wiki 管理

**Feature**: specs/069-wiki-template-web/spec.md | **基线**: specs/068-wiki-round-pause/data-model.md（本文生效后其 §1 Wiki 作用域、§2 存储/索引、§5 前的 name 形态由本特性修订）

## 1. 资源与校验

### 1.1 Wiki（作用域修订：template 级）

```text
Wiki {
  name        string    // templates/{template}/wikis/{wiki}（id 由 {wiki} 段承载，无独立 id 字段——`specs/068-wiki-round-pause/spec.md` FR-017 延续）
  title       string    // 模型侧唯一地址；非空且 ≤ 128 rune
  content     string    // 非空且 ≤ 64KB
  create_time Timestamp // output-only
  update_time Timestamp // output-only
}
作用域键：template（唯一作用域参数；session 不再参与）
```

- 校验（domain 层，违反 → INVALID_ARGUMENT）：`wiki_id` 匹配 `[a-z0-9_-]+`；title 非空且 ≤ 128 rune；content 非空且 ≤ 64KB；Search query 非空；page_size 默认 20、上限 100（AIP-158 coerce）。
- 唯一性（违反 → ALREADY_EXISTS）：`title_norm`（NFKC + 小写，068 §2.1 纯函数不变）在 **template 作用域**内唯一——Create 与 Update 改 title 两条写路径都强制。
- title/content 上限、规范化、bigram、检索排序（精确 > 前缀 > 子串，同分 title_norm 升序）、List 固定 `wiki_id asc` 与游标、Search 偏移 token——全部沿用 `specs/068-wiki-round-pause/contracts/wiki-service.md` 契约，仅作用域变更。

### 1.2 Memory（新增写入校验）

```text
Memory { ... }  // 结构不变：name=templates/{t}/sessions/{s}/memories/{m}、content、时间戳；作用域仍 (template, session)
新增校验：content ≤ 1024 rune（MaxMemoryContentRunes；utf8.RuneCountInString 度量）
```

- 执行点：服务端 Create/Update（INVALID_ARGUMENT，错误信息含上限与实际 rune 数）+ JS 工具预检（单条路径与批量 preflight，超限返回领域文本、全批不写）。
- 仅约束写入：既有条目读取、top-10 快照注入、删除不受追溯影响。`old_text`（定位子串）无独立上限（天然受既有条目长度约束）。

## 2. 存储（mongo `game_memory` 库）

### 2.1 wikis 集合文档（终态）

```json
{
  "template":    "saolei",
  "wiki_id":     "sha256(title_norm)[:32]",
  "title":       "原始标题",
  "title_norm":  "NFKC + 小写",
  "title_grams": ["bi", "ig", ...],
  "content":     "知识正文",
  "create_time": ISODate(...),
  "update_time": ISODate(...)
}
```

`session_id` 字段**不再存在**（文档与查询全部去 session 维度）。

### 2.2 索引（启动时创建，替换 `specs/068-wiki-round-pause/data-model.md` §2.3 的三个 session 维度索引）

| 索引 | 用途 |
|---|---|
| unique `(template, wiki_id)` | 身份；List 固定 `wiki_id asc` 排序键 |
| unique `(template, title_norm)` | template 作用域 title 唯一性 |
| `(template, title_grams)` multikey | Search `$all` bigram 预过滤 |

### 2.3 启动清理（clean break 落地，幂等）

`NewWikiRepository` 构造时：

1. `Indexes().DropOne`：`template_1_session_id_1_wiki_id_1`、`template_1_session_id_1_title_norm_1`、`template_1_session_id_1_title_grams_1`（旧索引名按 mongo 默认命名规则）。
2. `DeleteMany({session_id: {$exists: true}})`：清除旧作用域文档（新文档无该字段，清理完成后为 no-op）。
3. 创建 §2.2 三个新索引。

错误处理沿用既有约定（索引操作忽略重复错误；清理失败按启动失败处理——污染 template 集合的风险大于启动失败）。

### 2.4 memories 集合

无 schema 变更、无索引变更；仅写入路径新增 1024 rune 校验（§1.2）。

## 3. JS 工具面 schema（签名收敛，模型面不变）

签名与文本的唯一权威在契约，本文不重复（避免多处漂移）：

- `WikiStore` / wiki 操作核（template 单参）签名：[contracts/dsh-plugins.md](contracts/dsh-plugins.md) §3
- `PlannerMemoryService` 作用域转发（wiki 方法仅转发 `scope.template`）：[contracts/dsh-plugins.md](contracts/dsh-plugins.md) §2
- memory content 预检常量（`MEMORY_CONTENT_MAX_RUNES = 1024`）与超限文本：[contracts/memory-content-limit.md](contracts/memory-content-limit.md) §3

三个工具的参数 schema、描述文本、`{result: string}` 结果形态、`wiki failed: …` 基建失败文本——均不变（`specs/068-wiki-round-pause/spec.md` FR-006~011）。

## 4. web wiki API 投影（protojson，camelCase）

```text
Wiki          { name, title, content, createTime?, updateTime? }   // RFC 3339 字符串
ListResponse  { wikis?: Wiki[], nextPageToken? }
SearchResponse{ wikis?: { name, title }[], nextPageToken? }        // 轻量条目，无 content

REST（相对路径，零 CORS）：
  POST   /api/v1/templates/{template}/wikis?wiki_id={id}   body=wiki        → Wiki
  GET    /api/v1/templates/{template}/wikis?page_size=&page_token=          → ListResponse
  GET    /api/v1/templates/{template}/wikis:search?query=&page_size=&page_token= → SearchResponse
  GET    /api/v1/{name}                                                      → Wiki
  PATCH  /api/v1/{wiki.name=templates/*/wikis/*}?update_mask=<改动字段>   body=wiki → Wiki
  DELETE /api/v1/{name}                                                      → empty
```

- wiki_id 生成（web 创建路径）：复用 `sha256(title_norm)[:32]` 派生规则（`api/wiki.ts` 内实现 JS 侧NormalizeTitle 的既有孪生，见 `common/js/dsh-plugins/memory-service/src/wiki-operations.ts`）——web 无独立 id 语义，与服务端/工具面同源。
- 错误消费：`ApiError`（grpc-gateway gRPC→HTTP 映射，ALREADY_EXISTS→409、INVALID_ARGUMENT→400、NOT_FOUND→404），UI 呈现 message。

## 5. 状态机

无新增状态机：Wiki 为无状态 CRUD 资源；memory 上限是写入校验而非状态转移；web 视图为纯前端 state（搜索词、分页 token、表单模式、删除确认——PresetsView 同构）。
