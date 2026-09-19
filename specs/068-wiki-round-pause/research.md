# Research: planner wiki 知识库与 saolei-loop 每五轮自然停

> **Feature**: specs/068-wiki-round-pause/spec.md
> **状态**: Phase 0 完成（2026-09-19）
> **基础**: `survey/game-wiki-knowledge-base.md`（前期调研，决策 D1–D10 已与用户确认）承载检索技术、分页与存储选型的完整论证；本文只记录 plan 阶段新增/细化的决策与验证，不重复调研内容。

## 决策总览

| # | 决策 | 状态 |
|---|---|---|
| R1 | 资源标识形态：资源消息不带独立 id 字段，id 由 name 末段承载；**Memory.memory_id 随本特性移除（clean break）** | 用户确认（2026-09-19） |
| R2 | Wiki 资源与标识分层：agent 面 title 寻址，后端面 name 寻址；Search 响应条目 `{name, title}` | 用户指示（plan 输入） |
| R3 | wiki 工具形态：三独立工具（`wiki_search` 仅 query / `wiki_get` 批量 / `wiki_write` 单条 XOR 批量 upsert） | 用户指示（已回写 spec） |
| R4 | JS 服务面挂载：扩展 `PlannerMemoryService`（同一 `ctx.plannerMemory` 面、同一 scope 绑定），零编排层改动 | 本文 §4 |
| R5 | `wiki_id` 生成：规范化 title 的确定性摘要（sha256 前 32 hex） | 本文 §2.3 |
| R6 | 检索缓存：redis 快照缓存（067 基建已落地），纯优化、可整体降级 | 沿用调研 D7 + §5 现状注记 |
| R7 | 自然停实现锚点：orchestrator 内 `完成轮数` 计数 + reviewing 分支停点判定 | 本文 §6 |
| R8 | 验证面：wiki → testplan 大型测试（gateway HTTP）；自然停 → orchestrator 单测为主 | 本文 §7 |
| R9 | ListMemories page token 显式携带 order_by 最终排序键上下文（含方向）独立校验；游标载荷校验归属游标领域内部逻辑；实现任务并入 Phase 3 主任务流；page_size/parent 不纳入 token | 用户裁定（2026-09-19 执行期） |

---

## 1. R1：资源标识形态与 Memory.memory_id 移除

### 1.1 规范考证（原文引证）

- **AIP-122**（[Resource names](https://google.aip.dev/122)）："Resources **must** expose a `name` field that contains its resource name. Resources **may** provide the resource ID as a separate field (e.g. `book_id`). This field **must** apply the `OUTPUT_ONLY` field behavior classification."
  → `Memory.memory_id`（已标 OUTPUT_ONLY）**不违反 AIP-122**——独立 id 字段是被允许的可选项。
- **AIP-133**（[Standard methods: Create](https://google.aip.dev/133)，User-specified IDs 节）："The `{resource}_id` field **must** exist on the request message, **not the resource itself**."
  → id 的规范位置是 Create 请求消息（本仓库 `CreateMemoryRequest.memory_id` REQUIRED 即此形态，合规保留）；资源消息上的 id 字段是 AIP-122 允许但**冗余**的便利字段。
- **仓库先例**：`projects/game/game.proto` 的 `Session` 消息在 specs/035-proto-contract-refine 已移除 `template`/`session_id` 字段（reserved 2,3，注释注明 "carried by the name pattern path segments"）——本仓库的既定风格是 **id 只存在于 name 中**。

**结论**：`Memory.memory_id` 是 AIP 容许但与本仓库 Session 先例不一致的冗余字段；用户裁定随本特性 clean break 移除（对齐 FR-017）。

### 1.2 消费方影响清单（移除的改造面）

| 消费方 | 现状 | 改造 |
|---|---|---|
| `projects/game/game.proto` `Memory` 消息 | `string memory_id = 2 [OUTPUT_ONLY]` | 删除字段，`reserved 2; reserved "memory_id";`（对齐 Session 手法） |
| `projects/game/memory/handler/handler.go` | 响应构造 `MemoryId: m.MemoryID`（L244 附近） | 移除该行；domain/仓储内部的 `MemoryID` 字段与 mongo 文档不受影响（存储层继续存 id，仅 proto 响应面不再暴露） |
| `common/js/dsh-plugins/memory-service/src/client.ts` | `ListedMemory{memoryId}` 解码 → `MemoryEntry.memory_id` | 改为解码 `name` 并解析末段 id（新增 `parseMemoryName`，与既有 `memoryName()` 构造对称）；`MemoryEntry.memory_id` 作为 JS 内部派生值保留（operations.ts 寻址不变） |
| `projects/game/testplan/memory_test.go` | 18 处 `GetMemoryId()` 断言 + `memory_id` 字面量（路径参数/注释，共 26 处 `memory_id` 出现；如 `created.GetMemoryId() != "mem-1"`） | 改为断言 `GetName()`（如 `.../memories/mem-1` 后缀） |
| `ListMemoriesRequest.order_by` 白名单 | `memory_id`（unique）+ `update_time` | **白名单与 tie-break 机制不变**；proto 注释将 `memory_id` token 的语义更新为 "name 的 `{memory}` 段"（该 token 无活跃发送方：JS 只发 `update_time desc`，testplan 只测 `update_time` 与非法值拒绝——2026-09-19 grep 验证） |
| `CreateMemoryRequest.memory_id` | REQUIRED（AIP-133 用户指定 id） | 不变 |

消费方均为仓库内（无外部 REST 消费者），clean break 无兼容负担（与 035 的 Session 处理同型）。

## 2. R2/R5：Wiki 资源定义与标识分层

### 2.1 标识分层（用户指示）

- **agent 面（工具）**：一律以 title 寻址——search(query) 发现、get(titles) 取回、write 单条/批量 upsert；`name`/`wiki_id` 不进入模型可见文本（spec FR-009）。
- **后端面（服务 API）**：遵守 AIP——`name` 是唯一资源标识（Get/Update/Delete 按 name 寻址，AIP-131/134/135）；Create 请求带 `wiki_id`（REQUIRED，agent 侧生成，AIP-133）。

### 2.2 资源消息（不含独立 id 字段，FR-017）

`Wiki { name, title, content, create_time, update_time }`，pattern `templates/{template}/sessions/{session}/wikis/{wiki}`，服务 `WikiService`（沿用调研 D2，同 game.proto 单元、同 memory 服务二进制与 gateway target——调研 D4）。

### 2.3 wiki_id 生成（R5）

- 形态：`sha256(title_norm)` 前 32 hex（满足 `[a-z0-9_-]`，与 `generateMemoryId` 的 content-digest 同型——`common/js/dsh-plugins/memory-service/src/operations.ts`）。
- 确定性收益：同 title 重建（删除后重建、并发创建竞态）得到同 id；upsert 的 create 分支幂等。
- 生成位置：JS 工具层（agent 侧），与服务端 `wikiIDPattern` 校验（`[a-z0-9_-]+`，对齐 handler 的 memoryIDPattern）。

## 3. R3：三工具形态（已回写 spec，此处记录实现映射）

| 工具 | 参数 | 服务端映射 | 结果文本 |
|---|---|---|---|
| `wiki_search` | `{ query: string }`（仅此一个参数） | SearchWikis | 命中标题列表（无 content） |
| `wiki_get` | `{ titles: string[] }` | 每标题：SearchWikis 精确解析（规范化 title 相等）→ GetWiki | 每标题完整 content；未命中标题列出 + 引导 |
| `wiki_write` | 单条 `{ title, content }` XOR 批量 `{ entries: [{title, content}] }` | 每条目：精确解析存在 → UpdateWiki；不存在 → CreateWiki（wiki_id = §2.3 摘要） | 每条目 created/updated；批量原子（预检失败全批不写，映射 memory `applyBatch` 的 preflight/commit 两段式） |

批量原子性沿用 memory 工具的既有限制并如实反映：基础设施故障中断提交可能留下部分写入（spec 边界已记录）。

## 4. R4：JS 服务面挂载（扩展 plannerMemory 面）

- **方案**：`PlannerMemoryService`（`common/js/dsh-plugins/memory-service/src/service.ts`）新增 wiki 调用方法（search/get/write），复用**同一** per-agent scope 绑定——`load(agentCtx, scope)` 已经持有 `(template, session)`，wiki 与 memory 的作用域相同（调研 D1），绑定一次两用。
- **收益**：编排层（`saolei-loop` orchestrator 的 `loadPlannerMemory` 注入）**零改动**；preset 挂载面（planner 行 `@dominion/dsh-memory`）不变（FR-010 自动满足——wiki 工具与 memory 工具同行注册）。
- **备选否决**：独立 `ctx.plannerWiki` 服务面——需编排层新增 load 接线与第二个 inject 声明，收益仅是名字上的分离；`plannerMemory` 面名是内部 cordis 标识（非模型可见文本，不受 FR-012 通用化约束），保留名字、扩展能力即可。
- 存储访问：`memory-service` 包内新增 `WikiStore` seam（`searchWikis`/`getWiki`/`createWiki`/`updateWiki` 四方法的窄接口，DI 形态对齐 `MemoryStore`；Delete/List 为服务端 API 面，JS client 不消费——YAGNI）。

## 5. R6：检索缓存现状注记

- 调研 D7 的 redis 结果快照缓存**落地前提已满足**：067 已交付 redis 基建（deploy 资源、Go/JS 公共库 `common/gopkg/redis`、`common/js/redis`——git log `067 phase 3/6/7/8`，2026-09-19 验证）。
- 契约定位不变：缓存是纯服务端内部优化（miss/不可用 → 逐页重执行），API 契约（`{q, offset}` token、续页 query 匹配校验）对缓存与否不变。
- 缓存模块独立可拆（调研 §7 item 5），tasks 阶段可作为独立 phase 或裁剪项。

## 6. R7：自然停实现锚点（orchestrator）

代码锚点：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`。

- **计数点**：`runPump` 内 review drive 成功 settle 处（`step.review !== undefined` 分支，`reviewedGameEvent` 消费点）——`完成轮数 += 1`。该点天然满足"到达终态的局 + 复盘 turn 成功结束"（pendingReview 仅由终态记录触发；失败的复盘走 `fail()` 不计数，重试成功后才计）。
- **停点**：`nextStep()` 的 `phase === "planning" || phase === "reviewing"` 分支——进入该分支即队列已空（队列优先分支在前，满足"当期 member 处理完排队消息再停"）；若 `完成轮数 ≥ 5`（stop 常量 `5`）：执行与正常切换相同的状态转移（`phase = "playing"`、`current = "player"`），**返回 null 不驱动**，计数清零。player 的 relays 不 drain（保留至下次驱动一并投递）。
- **恢复**：循环静息（pump 空、`active === null && queued === 0`）→ session 层 `watchQuiescence` 按既有静息语义收尾 Send 流（`projects/game/agent_v2/src/session.ts` L784-808，无需改动）；用户 `submit` → `nextStep` 队列分支 `drain(player)` + 消息 → 驱动 player（含 planner 复盘广播）。
- **与既有机制的关系**：
  - 不复用 `paused`（cancel/failure 语义：保留 activation、无切换）；自然停是"切换后静息"，语义不同，独立布尔/计数状态即可，且**不新增对外快照字段**（用户决策）。
  - cancel 交叠：第 5 轮计数后 cancel → 恢复后队列消化完同样到达停点（nextStep 顺序保证）。
  - 计数为编排器内存态，与 phase/queue 一致随 re-materialize 重置（服务重启后重新计数，可接受——整个团队态都是易失的）。
- **中途重开**：重开不产生终态记录 → 无 pendingReview → 不计数不清零（`game/runtime.ts` 的 gameEvent 跨 restart-init 保留的是"最近一次终态"，`reviewedGameEvent` 记录身份防重复消费——既有机制天然排除重开局）。

## 7. R8：验证面

| 面 | 验证方式 | 依据 |
|---|---|---|
| wiki 服务端（Go） | fake collection 单测（filter 断言、token 编解码、纯函数 case 表——中文/全半角/大小写/emoji/短查询回退/评分排序）+ testplan 大型测试（gateway HTTP：CRUD/List 固定序分页/Search 子串与分页/错误映射），仿 `projects/game/testplan/memory_test.go` | 宪法 VI；调研 §7 item 7 |
| wiki 工具面（JS） | 单测：三工具注册与参数 schema、批量原子 preflight、title 精确解析（含规范化对齐）、结果文本不含 name/id | spec FR-006~011 |
| prompt/描述 | 单测断言 section 注册（名称/order/文本）与描述文本（无场景名词、top-10 说明、英文、无外部术语、各述自身） | spec FR-012/013 |
| memory_id 移除 | Go/JS 单测 + testplan 断言改 name 后全绿 | FR-017 |
| 自然停 | orchestrator 单测（注入 fake 成员/终态记录，覆盖：计数递增、第 5 轮停、队列消化后停、cancel 交叠、失败重试后计数、重开后清零、user submit 恢复驱动 player） | spec FR-014~016 |
| 自然停 E2E | 不设独立 testplan：真实驱动 5 局完整游戏成本与波动不可控；既有 agent_v2 testplan（game/conversation/stall 等）作零回归保障。编排层单测已覆盖状态机全部分支 | 宪法 VI 的验收由 wiki 大型测试承担；自然停以单测验收的裁断记录于此 |

## 8. R9：ListMemories page token 显式 order_by 上下文（2026-09-19 执行期裁定）

### 8.1 问题与三点裁定

065 交付的 `DecodeMemoryPageToken`（`projects/game/memory/domain/pagination.go`）以「游标 entries 字段序列恰好等于请求最终排序键」作为 order_by 一致性校验——隐式实现耦合：游标如何生成、如何编码是实现细节而非强制性质，游标实现一旦变化该校验静默失效；且比对只及字段名不及方向（`update_time desc` 的 token 配 `update_time` 请求可通过解码并产生错误 seek）。三点裁定：

1. **所有权分层**：page token 契约层唯一负责 order_by 上下文一致性——token 显式携带解析后最终排序键的规范化拼写数组，解码时与续页请求最终排序键逐项比对（长度/字段/方向）；entries 载荷的结构/对齐/值解析校验归属游标领域内部逻辑——游标消费 token 携带内容时自行校验其与当前游标实现一致。两层各自校验自己拥有的内容，互不寄生。
2. **任务归属**：实现任务并入 Phase 3 主任务流（与 search pagetoken 任务 T005 同 phase），不设独立增量小节。
3. **不纳入项定案**：page_size（AIP-158 明文允许续页变更）与 parent（memory List / wiki List / wiki Search 三者一致的既有边界）不作为 token 携带内容。

### 8.2 形态依据

- AIP-158（https://google.aip.dev/158）：续页参数一致性条款（"all other arguments ... must match"）+ Opacity（内部结构化消息 + base64 为认可的不透明编码）。
- AIP-132（https://google.aip.dev/132）Ordering：等价拼写空白不敏感——上下文存最终键规范形（含隐式追加的 tie-breaker），非原始 order_by 串。
- 仓库先例：SearchWikis token 显式携带规范化 query `q` 并于续页比对（specs/068-wiki-round-pause/contracts/wiki-service.md §4.4）——本裁定将同一形态推广到 ListMemories order_by 面。
- 契约：specs/068-wiki-round-pause/contracts/memory-page-token-context.md（修订 specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md §1 item 4）。

## 9. 参考文献

- 调研：`survey/game-wiki-knowledge-base.md`（D1–D10、n-gram 论证、分页社区实践、两级检索业界验证）
- [AIP-122 Resource names](https://google.aip.dev/122) / [AIP-133 Standard methods: Create](https://google.aip.dev/133) / [AIP-131](https://google.aip.dev/131) / [AIP-132](https://google.aip.dev/132) / [AIP-134](https://google.aip.dev/134) / [AIP-135](https://google.aip.dev/135) / [AIP-136](https://google.aip.dev/136) / [AIP-158](https://google.aip.dev/158) / [AIP-193](https://google.aip.dev/193)
- spec：`specs/068-wiki-round-pause/spec.md`（FR-001~017）
- 仓库锚点：`projects/game/game.proto`、`projects/game/memory/`（handler/domain/runtime/mongo）、`projects/game/gateway/cmd/main.go`、`projects/game/testplan/memory_test.go`、`common/js/dsh-plugins/memory/`、`common/js/dsh-plugins/memory-service/`、`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`、`projects/game/agent_v2/src/session.ts`
- 既有契约：`specs/064-memory-split-fold-remain/contracts/dsh-plugins.md`（本特性修订 §2 item 4）、`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`（order_by 面，注释语义更新）、`specs/035-proto-contract-refine/`（Session id 移除先例）
