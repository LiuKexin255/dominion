# Contract: planner 长期记忆快照——memory 服务 ListMemories 服务端排序注入最近 10 条

**Feature**: specs/065-agent-v2-team-refine/spec.md（FR-007，US3）
**服务端锚点**: `projects/game/game.proto`（ListMemoriesRequest）、`projects/game/memory/{domain,handler,runtime/mongo}`
**客户端锚点**: `common/js/dsh-plugins/memory-service/src/{client,snapshot,service}.ts`
**基线契约**: `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照固定与 section——注入策略增量修订，冻结时机与写路径零改动）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 基线——本契约增补 `order_by` 面）。

## §1 ListMemories 排序接口（memory 服务端）

1. **proto**：`ListMemoriesRequest` 增 `string order_by = 4`（AIP-132 Ordering: https://google.aip.dev/132）。支持值恰两个：
   - **缺省（空串）**：现行为——`memory_id` 升序（唯一键全序），游标为 raw `memory_id`；既有消费面（写路径全量拉取、既有分页断言）零破坏。
   - **`update_time desc`**（空白不敏感——按空白切分归一后等值比较，AIP-132 "redundant space characters are insignificant"）：`update_time` 降序，并列以 `memory_id` 升序打破——确定全序，不因分页/请求时点漂移（Mongo BSON date 毫秒精度，同毫秒并列由此规则消解）。
2. **错误语义**（AIP-193: https://google.aip.dev/193）：`order_by` 为其他任何值（含 `update_time`、`update_time asc`、`memory_id`、多字段列表、未知字段）→ `INVALID_ARGUMENT`，错误信息列出受支持值；ordered 模式下 `page_token` 解码失败 → `INVALID_ARGUMENT`。
3. **游标**（AIP-158: https://google.aip.dev/158）：ordered 模式 `next_page_token` 为复合游标 `(update_time, memory_id)` 的 base64url（NoPadding）JSON 编码（`update_time` 为 UTC RFC3339Nano）——镜像 session 服务先例 `projects/game/session/domain/pagination.go`。续页过滤 `$or: [{update_time: {$lt: T}}, {update_time: T, memory_id: {$gt: M}}]`（`T`/`M` 为游标值）。token 不透明（AIP-158 Opacity）；**分页时所有其他参数（含 `order_by`）MUST 与发放 token 的调用一致**（AIP-158 "must match"）——两种排序模式的 token 形态不互通，跨模式重放属非法使用（ordered 模式收到非编码 token 通常在 base64/JSON 解码即失败）。
4. **分层**：
   - `domain`：`ListMemoriesOrder`（两值：memory_id 升序 / update_time 降序）、`MemoryPageCursor{UpdateTime, MemoryID}`、`EncodeMemoryPageToken`/`DecodeMemoryPageToken`（解码失败返回 error）、`ErrInvalidPageToken`；`MemoryRepository.ListMemories` 签名增 `order ListMemoriesOrder` 参数。
   - `handler`：`order_by` 字符串解析与校验（归一空白 + 等值匹配 → order 枚举）；`toStatusError` 增 `ErrInvalidPageToken → INVALID_ARGUMENT`。
   - `runtime/mongo`：ordered 分支 sort spec `{update_time: -1, memory_id: 1}` + 复合游标过滤 + limit `pageSize+1`（既有 limit+1 模式）；缺省分支行为原样。游标解码归仓储所有（缺省模式的 raw `memory_id` 游标本就是仓储内部细节，两模式游标知识单点收敛于仓储）。
5. **索引**：`NewRepository` 启动时增建非唯一复合索引 `{template: 1, session_id: 1, update_time: -1, memory_id: 1}`（等值前缀 + 索引序扫描，镜像 session 仓储启动建索引模式 `projects/game/session/runtime/mongo/repository.go` NewSessionRepository）；既有唯一索引 `(template, session_id, memory_id)` 不变。
6. **HTTP 面**：grpc-gateway GET 的非路径字段自动映射查询参数（同 `page_size`/`page_token` 机制），`order_by` 无需额外注解。

## §2 快照装载与渲染（JS 客户端）

1. **`MemoryStore.listMemories` 签名**：增可选参数 `options?: { orderBy?: string; pageSize?: number }`：
   - `pageSize` 给定 → **单页即止**：发起一次请求（wire `{parent, pageSize, orderBy}`，undefined 字段不发送），返回首页条目，不续翻——AIP-158 page_size 为该页上限，快照装载只需首页。
   - 未给定 → 现行为：逐页累积至 `next_page_token` 为空（wire `{parent, pageToken}`；写路径全量语义，`operations.ts` 的 2 参调用零改动）。
2. **快照装载**（`service.ts` 的 `load`）：`client.listMemories(template, session, {orderBy: SNAPSHOT_ORDER_BY, pageSize: SNAPSHOT_ENTRY_LIMIT})`——`SNAPSHOT_ORDER_BY = "update_time desc"`、`SNAPSHOT_ENTRY_LIMIT = 10`（注入策略常量由 `snapshot.ts` 所有并导出）。首页即"最近更新的 ≤10 条、服务端有序"；失败 throw（fail-loud 物化回滚，064 §2 既有语义）。
3. **快照渲染**（`renderMemorySnapshot(entries)`）：**纯透传**——按入参顺序逐条一行渲染 `entry.content`；`长期记忆：` 头；空集空串（section 不渲染，既有语义）；`memory_id` 永不渲染（既有义务）。排序与截断由 §1 服务端排序 + 装载查询形态共同承担；客户端不做任何排序/截断/归一化，`MemoryEntry` 仅 `{memory_id, content}`（`updateTime` 字段及其 Timestamp 归一化逻辑不存在——排序知识单点收敛于服务端）。
4. **冻结时机不变**：`load` 预取时渲染一次并冻结（实例生命周期固定，运行中写入待下次物化生效——064 §2 既有语义）；`applyCall` 写路径与 `old_text` 定位面向全量存储，不传 `orderBy`、全页累积——现行为零变化。

## §3 边界

- 条目数 > 10：装载查询 `page_size=10` 首页即最近 10 条（服务端 page_size 上限语义保证返回不超过请求条数）；被截断条目仍可经 memory 工具操作（存储全量）。
- 全部条目 `update_time` 并列（如一次批量导入）：`memory_id` 升序的前 10 条（§1 并列规则）。
- 缺省模式消费面：`operations.ts` 写路径、既有 AIP-158 分页断言（`projects/game/testplan/memory_test.go`）继续走 `memory_id` 升序 + raw 游标——零破坏。
- 前端零改动：web 前端无 ListMemories 消费面；proto 增字段向后兼容。

## §4 验收面（对应 spec FR-007、SC-003）

Go 单测（memory 服务）：

- domain（`pagination_test.go`）：游标 codec round-trip；坏 token（空/坏 base64/坏 JSON/缺字段/坏时间）表驱动报错——镜像 `projects/game/session/domain/pagination_test.go`。
- 仓储（`runtime/mongo/repository_test.go`）：ordered 模式 `update_time` 降序、并列 `memory_id` 升序；limit+1 续页跨页全量一次；坏 token → `ErrInvalidPageToken`；缺省模式排序/游标回归。
- handler（`handler_test.go`）：合法 `order_by` 透传 order 枚举；非法 `order_by`（未知字段/升序/多字段）→ INVALID_ARGUMENT；仓储 `ErrInvalidPageToken` → INVALID_ARGUMENT。

JS 单测（memory-service）：

- client：`pageSize` 给定单页即止（不续翻、请求 wire 携带 `pageSize`/`orderBy`）；未给定时全页累积回归；条目仅 `{memory_id, content}`。
- service：`load` 发起 `{orderBy, pageSize: 10}` 单页调用且快照按返回序渲染；写路径 2 参调用回归；load 冻结、写后快照不变（064 既有用例）回归。

testplan（`projects/game/testplan/memory_test.go`，经网关 `?order_by=update_time%20desc`）：

- 有序列表端到端：排序断言（含 PATCH 更新后该条目浮至首位）；复合游标续页全量一次；非法 `order_by` → 400 INVALID_ARGUMENT；既有缺省模式分页断言回归。
- 既有 `TestAgentV2TeamGameStatsPromptFaces`（12 条夹具、间隔 3ms 互异）零改动通过：服务端排序结果与注入断言一致。
