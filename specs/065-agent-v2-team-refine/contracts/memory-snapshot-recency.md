# Contract: planner 长期记忆快照——memory 服务 ListMemories 通用排序与游标分页

**Feature**: specs/065-agent-v2-team-refine/spec.md（FR-007，US3）
**服务端锚点**: `projects/game/game.proto`（ListMemoriesRequest）、`projects/game/memory/{domain,handler,runtime/mongo}`
**客户端锚点**: `common/js/dsh-plugins/memory-service/src/{client,snapshot,service}.ts`
**基线契约**: `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照固定与 section——注入策略增量修订，冻结时机与写路径零改动）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 基线——本契约定义 `order_by` 面）。

## §1 ListMemories 通用排序接口（memory 服务端）

排序是一套**通用机制**：`order_by` 语法解析 → 字段白名单映射 → 唯一键收尾 → 通用游标 → 单路径仓储查询。不为任何单个排序需求定制方法、字面量或游标形态。

1. **`order_by` 语法**（AIP-132 Ordering: https://google.aip.dev/132）：
   - 值为逗号分隔的字段列表，每项为 `{field}` 或 `{field} desc`；升序通过**省略**后缀表达（AIP-132 只定义 `desc` 后缀，`asc` 不是合法 token）；空白不敏感（项内与项间任意空白等价）；`desc` 小写敏感。
   - **字段白名单**（API 字段 → Mongo 字段的单一事实源映射表，domain 所有）：`memory_id`（string、唯一）、`update_time`（time、非唯一）——首期集合从实际消费出发，机制不为该集合写死。
   - 非法值 → `INVALID_ARGUMENT`（AIP-193: https://google.aip.dev/193），错误信息列受支持字段与语法：未知字段（含子字段路径，如 `content.foo`）、非法后缀（`asc`、`DESC`、多余 token）、重复字段。
2. **唯一键收尾规则（通用 tie-breaker）**：排序键未以唯一字段（白名单 `Unique: true`，当前即 `memory_id`）收尾时，自动追加 `memory_id asc`——排序成为确定全序，游标才能稳定翻页（并列键不以唯一键收尾会在页边界静默跳行/重行，见 research.md R0 社区论证；Firestore Standard edition 对 `orderBy` 缺省自动追加 `__name__` 是同一规则的工业先例）。推论：`memory_id` 只能作为键列表末位出现（出现于非末位时追加必然冲突）→ `INVALID_ARGUMENT`。**缺省（空 `order_by`）经同一机制表达**：空键列表 + 收尾规则 = `[memory_id asc]`，无缺省特设分支。
3. **通用游标**（AIP-158 Pagination: https://google.aip.dev/158）：
   - `next_page_token` 编码**当前页最后一条文档在最终排序键（含 tie-breaker）上的全部键值**——所有排序形态共用一套机制。编码形态：base64url（NoPadding）JSON 数组，每元素 `{"field": "<API 字段名>", "value": "<字符串值>"}`；时间值 UTC RFC3339Nano。值类型由白名单映射表的字段类型声明解释（类型知识单点收敛于映射表，随 field 名携带类型信息）。
   - **不编码方向**：token 是结果空间中的**位置**；方向由续页请求自身的 `order_by` 重新推导（Firestore `startAfter` 语义：游标 = 排序键值位置，序由查询定义）。
   - 解码校验（坏 token → `ErrInvalidPageToken` → `INVALID_ARGUMENT`）：空 token、坏 base64、坏 JSON、非数组、元素缺 `field`/`value`、未知 field、值与字段声明类型不符（时间不可解析 RFC3339Nano）。
   - **键匹配校验**（AIP-158 "all other parameters must match" 的服务端强制）：解码出的字段序列（名与数量）必须与当前请求经收尾规则归一化后的最终排序键完全一致，否则 `ErrInvalidPageToken`——跨序重放（如 `update_time desc` 的 token 配缺省序请求）被显式拒绝。等价拼写的 `order_by`（如 `update_time desc` 与 `update_time desc, memory_id`，最终排序键相同）token 互通。token 不透明（AIP-158 Opacity），客户端只透传。
4. **单一仓储路径**：`MemoryRepository.ListMemories(ctx, template, session, sort []domain.MemorySortTerm, pageSize, pageToken)` 一套流程，无 per-order 方法：
   - `sort` → Mongo sort spec（白名单映射 Mongo 字段 × 方向）；
   - `pageToken` 非空 → 解码 → 键匹配校验 → **通用 OR 阶梯续页过滤**：对最终键 `k_0..k_{n-1}` 与游标值 `v_0..v_{n-1}`，`$or: [{k_0: op_0 v_0}, {k_0: v_0, k_1: op_1 v_1}, …, {k_0: v_0, …, k_{n-2}: v_{n-2}, k_{n-1}: op_{n-1} v_{n-1}}]`，`op_i` 升序为 `$gt`、降序为 `$lt`（seek method 的展开形态，方向随比较翻转——见 research.md R0）；
   - `SetLimit(pageSize + 1)`（既有 limit+1 模式）；页满时按最终键从页末条目提取键值 → 编码 `next_page_token`。
5. **分层**：
   - `domain`：`MemorySortTerm{Field, Descending}`（最终排序键元素）、`MemorySortFieldSpec{Field, MongoField, Kind, Unique}` 与白名单映射表（单一事实源）、`ParseMemoryOrderBy(orderBy) ([]MemorySortTerm, error)`（语法 + 白名单 + 重复校验 + 唯一键收尾，一步产出最终排序键）、`MemoryPageCursor` + `EncodeMemoryPageToken`/`DecodeMemoryPageToken`（通用 codec）、`ErrInvalidPageToken`。
   - `handler`：`order_by` → `domain.ParseMemoryOrderBy`，解析错误与 `ErrInvalidPageToken` → `INVALID_ARGUMENT`；无排序知识。
   - `runtime/mongo`：单路径查询（sort spec / 键匹配 / OR 阶梯 / limit+1 / 游标构造），排序与游标知识单点收敛于 domain 映射表 + 仓储的通用函数。
6. **索引**：唯一索引 `(template, session_id, memory_id)` 支撑缺省 `[memory_id asc]`（等值前缀 + 末键排序）；非唯一复合索引 `{template: 1, session_id: 1, update_time: -1, memory_id: 1}`（既有）支撑 `[update_time desc, memory_id asc]`（键序与方向精确匹配，ESR：等值前缀在前、排序键随后——https://www.mongodb.com/docs/manual/tutorial/equality-sort-range-guideline/）。**索引义务约定**：新增一个可排序字段 = 白名单加一行 + Mongo 文档 accessor 一行 + 按实际消费方向建 `(template, session_id, <field>[, memory_id])` 复合索引。未消费方向的组合（如 `update_time asc`）语法合法，由既有索引扫描 + 内存排序兜底（复合索引只支撑同序与逆序 sort：https://www.mongodb.com/docs/v7.0/tutorial/sort-results-with-indexes/）；记忆条目典型 <100/会话，规模下无碍；出现实际消费方向时再补建对应索引。
7. **HTTP 面**：grpc-gateway GET 的非路径字段自动映射查询参数（同 `page_size`/`page_token` 机制），`order_by` 无需额外注解。

## §2 快照装载与渲染（JS 客户端）

1. **`MemoryStore.listMemories` 签名**：增可选参数 `options?: { orderBy?: string; pageSize?: number }`：
   - `pageSize` 给定 → **单页即止**：发起一次请求（wire `{parent, pageSize, orderBy}`，undefined 字段不发送），返回首页条目，不续翻——AIP-158 page_size 为该页上限，快照装载只需首页。
   - 未给定 → 现行为：逐页累积至 `next_page_token` 为空（wire `{parent, pageToken}`；写路径全量语义，`operations.ts` 的 2 参调用零改动）。
2. **快照装载**（`service.ts` 的 `load`）：`client.listMemories(template, session, {orderBy: SNAPSHOT_ORDER_BY, pageSize: SNAPSHOT_ENTRY_LIMIT})`——`SNAPSHOT_ORDER_BY = "update_time desc"`、`SNAPSHOT_ENTRY_LIMIT = 10`（注入策略常量由 `snapshot.ts` 所有并导出）。首页即"最近更新的 ≤10 条、服务端有序"（`update_time desc` 在通用机制下归一化为 `[update_time desc, memory_id asc]`，排序形态与契约 §1 一致）；失败 throw（fail-loud 物化回滚，064 §2 既有语义）。
3. **快照渲染**（`renderMemorySnapshot(entries)`）：**纯透传**——按入参顺序逐条一行渲染 `entry.content`；`长期记忆：` 头；空集空串（section 不渲染，既有语义）；`memory_id` 永不渲染（既有义务）。排序与截断由 §1 服务端排序 + 装载查询形态共同承担；客户端不做任何排序/截断/归一化，`MemoryEntry` 仅 `{memory_id, content}`（排序知识单点收敛于服务端）。
4. **冻结时机不变**：`load` 预取时渲染一次并冻结（实例生命周期固定，运行中写入待下次物化生效——064 §2 既有语义）；`applyCall` 写路径与 `old_text` 定位面向全量存储，不传 `orderBy`、全页累积——现行为零变化。

## §3 边界

- 条目数 > 10：装载查询 `page_size=10` 首页即最近 10 条（服务端 page_size 上限语义保证返回不超过请求条数）；被截断条目仍可经 memory 工具操作（存储全量）。
- 全部条目 `update_time` 并列（如一次批量导入）：`memory_id` 升序的前 10 条（§1 收尾规则）。
- 缺省模式消费面：`operations.ts` 写路径、既有 AIP-158 分页断言（`projects/game/testplan/memory_test.go`）继续走 `memory_id` 升序——排序结果与翻页语义零变化；`next_page_token` 形态为通用编码（对客户端不透明、只透传，AIP-158 Opacity），无跨服务版本的持久 token 消费面（写路径翻页循环在单次调用内完成、测试每次从首页开始）。
- 前端零改动：web 前端无 ListMemories 消费面；proto 字段面向后兼容。

## §4 验收面（对应 spec FR-007、SC-003）

Go 单测（memory 服务）：

- 排序解析（`domain/sort_test.go`，表驱动）：`""` → `[memory_id asc]`（缺省经收尾规则推导）；`"update_time desc"` → `[update_time desc, memory_id asc]`（追加）；空白归一（多空格/制表符/项间空白）；`"update_time desc, memory_id"` → 不追加（幂等）；`"memory_id desc"` → `[memory_id desc]`（唯一键 desc 收尾）；`"update_time"` → `[update_time asc, memory_id asc]`（升序合法——通用语法下裸字段即升序）；拒绝：未知字段（`foo`/`content`/`content.foo`）、`"update_time asc"`（非法后缀）、`"update_time desc desc"`、重复字段（`"update_time, update_time"`）、`"memory_id desc, update_time"`（`memory_id` 非末位）；错误信息列受支持字段与语法。
- 游标 codec（`domain/pagination_test.go`）：多键 round-trip（含 RFC3339Nano 纳秒精度、字符串与时间混合键）；坏 token 表驱动（空/坏 base64/坏 JSON/非数组/缺 field/缺 value/未知 field/时间不可解析）。
- 仓储（`runtime/mongo/repository_test.go`）：缺省 `[memory_id asc]` 回归（序 + limit+1 翻页全量一次）；`update_time desc` 降序、并列 `memory_id` 升序、tie 组跨页边界全序保持；`memory_id desc` 序与翻页（方向翻转的 OR 阶梯）；显式多字段 `"update_time desc, memory_id"` 与 `"update_time desc"` 结果一致；键不匹配 token（跨序重放：desc 序 token 配缺省序、双键 token 配单键序）→ `ErrInvalidPageToken`；坏 token → `ErrInvalidPageToken`；fake `Find` 的 `$or` 求值通用化（前缀相等 + 方向感知比较）与任意键序排序。
- handler（`handler_test.go`）：合法/非法 `order_by` 表驱动（合法 → 仓储收到归一化最终排序键；非法 → InvalidArgument）；仓储 `ErrInvalidPageToken` → INVALID_ARGUMENT。

JS 单测（memory-service，零改动回归）：

- client：`pageSize` 给定单页即止（不续翻、请求 wire 携带 `pageSize`/`orderBy`）；未给定时全页累积回归；条目仅 `{memory_id, content}`。
- service：`load` 发起 `{orderBy, pageSize: 10}` 单页调用且快照按返回序渲染；写路径 2 参调用回归；load 冻结、写后快照不变（064 既有用例）回归。

testplan（`projects/game/testplan/memory_test.go`，经网关查询参数）：

- 有序列表端到端：`?order_by=update_time%20desc` 行为——PATCH 更新后该条目浮至首位、同毫秒并列组内 `memory_id` 升序、`page_size=2` 复合游标续页全量一次且序保持。
- 通用语法正路径：`update_time`（裸字段升序）与 `update_time desc, memory_id`（多字段显式 tie-breaker）合法且序正确——与 `update_time desc` 单页序一致的对照。
- 非法 `order_by` → 400 INVALID_ARGUMENT：`foo`（未知字段）、`update_time asc`（非法后缀）、`content`（未开放字段）。
- 既有缺省模式分页断言（`memory_id` 升序、翻页全量）回归。
- 既有 `TestAgentV2TeamGameStatsPromptFaces`（12 条夹具、间隔 3ms 互异）零改动通过：服务端排序结果与注入断言一致。
