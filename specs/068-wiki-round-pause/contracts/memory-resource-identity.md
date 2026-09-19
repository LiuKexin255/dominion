# Contract: Memory 资源标识收敛——移除 Memory.memory_id

> **Feature**: specs/068-wiki-round-pause/spec.md（FR-017，plan 阶段用户指示）
> **锚点**: `projects/game/game.proto`、`projects/game/memory/handler/handler.go`、`common/js/dsh-plugins/memory-service/src/client.ts`、`projects/game/testplan/memory_test.go`
> **依据**: AIP-122（[独立 id 字段是允许项](https://google.aip.dev/122)）＋AIP-133（[`{resource}_id` 属于请求消息而非资源本身](https://google.aip.dev/133)）＋仓库先例（`Session` 于 specs/035-proto-contract-refine 移除 session_id 的 clean break）；论证见 [research.md](../research.md) §1。

## 1. 变更定义（clean break，无兼容过渡）

| 面 | 终态 |
|---|---|
| proto `Memory` 消息 | 删除 `string memory_id = 2 [OUTPUT_ONLY]`；`reserved 2; reserved "memory_id";`（对齐 Session 手法） |
| proto `CreateMemoryRequest.memory_id` | **不变**（REQUIRED——AIP-133 用户指定 id，agent 侧生成） |
| proto `ListMemoriesRequest.order_by` | 白名单（`memory_id`/`update_time`）与 tie-break 机制**不变**；注释将 `memory_id` token 语义更新为 "name 的 `{memory}` 段"（该 token 无活跃发送方——research §1.2） |
| Go handler | 响应构造不再设置 `MemoryId`（删除 `MemoryId: m.MemoryID` 行）；domain/仓储/存储文档的内部 `MemoryID` 不变 |
| JS client | `listMemories` 解码改从 `name` 解析：新增 `parseMemoryName(name)` 取 `/memories/` 末段（与 `memoryName()` 构造对称）；`MemoryEntry.memory_id` 保留为 JS 内部派生值（operations.ts 的 update/delete 寻址不变，快照渲染本就不含 id） |
| testplan `memory_test.go` | 响应断言从 `GetMemoryId()` 改为 `GetName()`（如后缀 `/memories/mem-1`） |

## 2. 不变量

- 资源寻址语义不变：Get/Update/Delete 按 name；Create 按请求 memory_id 生成 name。
- 存储层继续持有 id（mongo 文档 `_id` 之外的 `memory_id` 字段、排序键）——仅 proto 响应面收敛。
- `MemoryEntry.memory_id`（JS 派生）继续**不出现**在任何模型可见文本（specs/059 FR-008 既有约定）。
- 065 排序分页契约（`specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md`）的行为面零变化。

## 3. 消费方与验证

消费方均为仓库内（proto codegen、Go handler、JS client、testplan；无外部 REST 消费者）。验证：Go/JS 单测全绿 + `projects/game/testplan/memory_test.go` 改造后 testplan 实际执行全绿（quickstart.md 场景 3）。
