# Contract: ListMemories page token 显式携带 order_by 上下文

> **Feature**: specs/068-wiki-round-pause/spec.md（FR-017 执行期用户裁定，2026-09-19）
> **锚点**: `projects/game/game.proto`（ListMemoriesRequest.order_by 注释）、`projects/game/memory/domain/{pagination,sort}.go`、`projects/game/memory/runtime/mongo/repository.go`、`projects/game/testplan/memory_test.go`
> **基线修订**: `specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（本文生效后其 §1 item 4 的 token 一致性语义由本文修订：order_by 一致性不再由「游标 entries 字段序列恰好等于最终排序键」隐式承载，改由 token 显式携带的 order_by 上下文独立校验并纳入方向；entries 载荷的校验从 token 契约条文中移出，归属游标领域内部逻辑——游标消费 token 携带内容时自行校验其与当前游标实现一致。其 §1 其余条目与 §4 其余验收面不变）
> **依据**: AIP-158（https://google.aip.dev/158 —"The user is expected to keep all other arguments to the RPC the same; if any arguments are different, the API **should** send an `INVALID_ARGUMENT` error"；Opacity 节认可内部结构化消息 + base64 的不透明编码）＋ AIP-132（https://google.aip.dev/132 — Ordering：等价拼写空白不敏感）＋ 仓库先例：SearchWikis token 显式携带规范化 query `q` 并于续页比对（specs/068-wiki-round-pause/contracts/wiki-service.md §4.4）

## 1. 所有权模型与变更定义（clean break，无兼容过渡）

校验分两层，各自校验自己拥有的内容、互不寄生：

- **page token 契约层**：token 显式携带定义结果序列的请求上下文——order_by 解析后的最终排序键。解码时与续页请求 `ParseMemoryOrderBy` 产出的最终排序键**逐项比对**（长度 + 每项拼写——字段名与方向合一）。这是 order_by 一致性的**唯一** token 契约条文。
- **游标领域内部逻辑**：entries 是 seek 载荷，其结构/对齐/值解析校验（每 sort term 恰一 entry、entry field 标签对齐、值按 term kind 可解析）归属游标内部——游标消费 token 携带内容时，自行校验其与**当前游标实现**一致。载荷形态与其校验是游标实现细节，不构成 token 契约内容；游标实现演进不影响 token 契约。

| 面 | 终态 |
|---|---|
| token wire 形态 | `base64url(NoPadding)(JSON {"sort":[...],"entries":[...]})`——`sort` 为 **order_by 上下文**：最终排序键的规范化拼写数组，每项 `"{field}"` 或 `"{field} desc"`（AIP-132 规范化 term：升序省略后缀）；`entries` 仍为 seek 载荷 `{field, value}`（065 原样） |
| 铸造 | `MemoryPageCursor` 增 `Sort []string`（json tag `sort`）——struct 加字段即 065 §1 item 4 预告的演进机制，`Encode`/`Decode`/handler/仓储签名不变；唯一铸造入口 `NewMemoryPageCursor(sort []*MemorySortTerm, values []any) *MemoryPageCursor`：逐 term `CursorEntry(values[i])` 构造 entries、逐 term 规范化拼写填充 Sort（拼写单一知识源为 domain 包内私有 `memorySortTermWire`，铸造与比对共用同一函数——无解析、无第二拼写源）；`len(values) != len(sort)` panic fail-fast（编程期不变量违约，同 `CursorEntry` kind 不匹配 panic 先例） |
| 上下文一致性（token 层） | token 内 `sort` 上下文与请求最终排序键逐项比对；不一致 → `ErrInvalidPageToken` → INVALID_ARGUMENT（错误映射不变）。**方向参与比较**：`update_time` 与 `update_time desc`（字段序列相同、方向不同）的 token 不再互通——续页翻转方向 → INVALID_ARGUMENT（065 实现的真实缺口，本次闭合） |
| 载荷校验归属（游标层） | 065 §1 item 4 原「entries 字段序列与请求最终排序键不匹配」校验**保留但归属重划**：entry field 标签与位序 term 的对齐、载荷良构、值解析归属游标构造/校验函数（游标领域内部逻辑），不再是 token 契约条文。拒绝集相对 065 **严格扩大**（新增：方向不一致、上下文缺失/伪造），065 已拒绝的形态仍全部拒绝 |
| 代码分层 | `DecodeMemoryPageToken(token, sort)` 公开签名不变，内部两段：信封解码 + 上下文比对（page token 层，函数本体职责）→ 委托包内私有游标构造/校验函数（如 `newMemoryCursor(sort, entries) (*MemoryPageCursor, error)`）完成载荷对齐/标签/值解析与 typed 单次转换（游标领域内部逻辑）。全部失败 wrap `ErrInvalidPageToken` |
| 仓储 | `memoryNextPageToken` 改经 `NewMemoryPageCursor` 铸造（上下文与 entries 成套单点） |
| handler | 零改动——签名、调用流、错误映射不变 |
| JS / wiki 面 | JS `listMemories` 零改动（orderBy/token 原样透传，同次调用内最终键恒一致）；ListWikis 无 order_by（固定 `wiki_id asc`）无请求上下文可校验；SearchWikis 即本形态先例——两者无需对齐改动 |

## 2. 解码校验顺序（按所有权归属标注；全部 wrap `ErrInvalidPageToken` → INVALID_ARGUMENT）

- **page token 层（信封 + 上下文）**：空 token → 坏 base64 → 坏 JSON（含顶层非对象）→ `entries` 缺失/非数组（信封结构）→ 上下文长度 ≠ 最终排序键长度（无 `sort` 键的旧形态 token 自然落此，fail-closed）→ 上下文逐项拼写不符（涵盖跨序重放、方向翻转、伪造上下文）。
- **游标层（携带内容与当前游标实现一致）**：`entries` 为空 → 每 sort term 恰一 entry（长度对齐）→ 元素为 null / 缺 `field`/`value`（空串）→ entry field 标签与位序 term 对齐 → 值按位序 term 的 kind 可解析（时间 RFC3339Nano），经 `sort[i].CursorValue(value)` 一次完成校验与 typed 转换（065 原样）。
- `EncodeMemoryPageToken` 空 token 条件：nil cursor / 空 Entries / **空 Sort**（手铸半成品 cursor 编码为空 token，fail-closed 而非铸造必死 token）。

## 3. 不变量

- 等价拼写互通：`update_time desc` 与 `update_time desc, memory_id` 最终键相同 → token 互通（上下文存最终键规范形——含隐式追加的 tie-breaker，非原始 order_by 串）。
- 065 §1 其余全部不变：白名单、tie-break 收尾判据、缺省 `[memory_id asc]`、OR 阶梯 seek、typed 单次转换、nil = 首页唯一判据。
- **page_size 与 parent 不纳入 token（定案）**：AIP-158 明文允许续页变更 page_size（"If the user changes the page_size in a request for subsequent pages, the service **must** honor the new page size"），故 page_size 不属定义结果序列的上下文；parent 不入 token 是 memory List / wiki List / wiki Search 三者一致的既有边界（SearchWikis 亦仅携 `q`）——两者均不作为 token 携带内容，亦不校验。
- entries 仍是「结果空间中的位置」；方向不进 seek 载荷——seek 比较算子由续页请求 term 的方向推导（Firestore `startAfter` 语义），上下文只用于校验、不用于查询构造。
- token 对外不透明、全消费面只透传；无跨版本持久 token 消费面（写路径单次调用内翻页、测试从首页开始）——内部 JSON 形态 clean break 零破坏。

## 4. proto 注释终态

见 `projects/game/game.proto` `ListMemoriesRequest.order_by` 注释第三段（token 携带 order_by 上下文并逐项校验含方向、载荷处理属游标内部、不一致 INVALID_ARGUMENT、引用本契约与 065 基线）。

## 5. 验收面（对 065 §4 的增量修订；用例按两层归属组织）

- `domain/pagination_test.go`：
  - **上下文一致性（token 层）**：方向翻转拒绝（`update_time desc` 上下文 token 配 `update_time` 序，及反向）；跨序重放拒绝（**带上下文铸造**，走到上下文比对路径）；上下文伪造拒绝（改一项拼写）；无上下文拒绝（手造 `{"entries":[...]}` 旧形态）；上下文长度不符拒绝；等价拼写互通（`update_time desc` 与 `update_time desc, memory_id` 的 token 互相可解）。
  - **信封（token 层）**：坏 token 表（空/坏 base64/坏 JSON/顶层非对象/`entries` 缺失/非数组）行为不变。
  - **游标内部校验（游标层；用例一律「上下文正确 + 载荷破坏」铸造，确保走到游标校验路径）**：`entries` 空/元素 null/缺 field/缺 value 拒绝；长度对齐（双 entry 载荷配单键序）；field 标签对齐（含未知 field 标签）拒绝；值不可按 kind 解析（时间非 RFC3339Nano）；decode 产物 `Typed()` 为 typed 值（单次转换行为面）。
  - **铸造与编码**：JSON 形态 pin（顶层恰两键 `{sort, entries}`、sort 拼写逐项、`typed` 不出现）；round-trip 经 `NewMemoryPageCursor` 铸造；`NewMemoryPageCursor` 单测（Sort 规范形 + entries typed 就绪 + len 失配 panic）；Encode nil/空 Entries/空 Sort → 空 token；指针语义回归。
- `domain/sort_test.go`：`memorySortTermWire` 真值表（asc → `"{field}"`、desc → `"{field} desc"`）。
- `runtime/mongo/repository_test.go`：经 Decode 的手铸 token（`TestMemoryListSeekValuesAreTyped` 等）改 `domain.NewMemoryPageCursor` 铸造；`memorySeekClauses` 直测用例（绕过 Decode）零改动；既有翻页 walk（repo 铸造 token 自动携上下文）回归。
- `handler/handler_test.go`：`TestMemoryService_ListMemoriesPageToken` 增方向翻转 → InvalidArgument、等价拼写（`update_time desc, memory_id`）配 desc token → 透传成功；既有坏 token/空 token → nil cursor 用例行为不变。
- testplan `projects/game/testplan/memory_test.go`：`?order_by=update_time desc` 首页 token 配 `order_by=update_time`（翻转方向）→ 400 INVALID_ARGUMENT；desc token 配等价拼写 `update_time desc, memory_id` 续页 → 200 序保持；缺省 token 配显式 `order_by=memory_id` 续页 → 200（等价拼写端到端）；既有缺省/desc 翻页 walk 回归。
