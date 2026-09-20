# Contract: memory 单条 content 长度上限（1024 rune）

> **Feature**: specs/069-wiki-template-web/spec.md（FR-008~011）
> **基线**: `specs/039-planner-memory-calibration/contracts/memory-service-contract.md`（Memory 资源与 CRUD 契约；本契约为其增加写入校验，其余不变）
> **锚点**: `projects/game/memory/domain/model.go`（校验）、`projects/game/memory/handler/handler.go`（Create/Update 调用点）、`common/js/dsh-plugins/memory-service/src/operations.ts`（工具预检）、`common/js/dsh-plugins/memory/src/guidance.ts`（指引预先说明）

## 1. 上限语义

- 每条 memory 的 `content` 上限 **1024 rune**（`MaxMemoryContentRunes`；`utf8.RuneCountInString` 度量——与 wiki title 的 rune 口径一致，对中英文公平；spec Clarifications 2026-09-20 裁定）。
- 仅约束**写入路径**：Create、Update（含工具面 replace）。既有条目的读取、top-10 快照注入、删除不受追溯影响。
- `old_text`（replace/remove 的定位子串）不设独立上限——其长度天然受既有条目内容（≤ 1024 rune）约束。
- 空 content 语义维持现状（工具面既有领域文本），不属于本契约。

## 2. 服务端执行（最后防线）

- `domain.ValidateMemoryContent(content)`：rune 数 > 1024 → 错误（消息含上限与实际 rune 数）。
- handler `CreateMemory` / `UpdateMemory`（`applyMemoryMask` 之后）调用；违反 → `INVALID_ARGUMENT`。
- REST/gRPC 直写客户端（非工具面）依赖此层。

## 3. 工具面执行（JS 预检，主要防线）

- 常量：`MEMORY_CONTENT_MAX_RUNES = 1024`（与服务端对齐）。
- 单条路径（`applyAdd` / `applyReplace`，与既有空 content 检查同位）：超限返回领域文本结果（非错误、不写入）。
- 批量路径（`applyBatch` preflight）：任一 op 的 content 超限 → `batchError`（全批不写，含既有"当前条目列表"格式）。
- 超限文本（FR-009：含上限说明 + wiki 引导）：

```text
memory: content exceeds 1024 characters (got N). Keep entries short; store longer knowledge with wiki_write.
```

- 基建失败仍走 `memory failed: …`（不变）；超限是领域结果，永不作为工具错误抛出。

## 4. 模型面预先说明（FR-011）

- `memory:guidance`（分工差异段）增加一句（英文，约数表述）：memory 条目保持简短（每条约 1000 字符量级），更长的知识写入 wiki。
- 工具描述与 `wiki:guidance`（用法段）不承载上限说明（068 FR-013 职责边界延续）。

## 5. 验收锚点

- Go 单测：`ValidateMemoryContent` case 表（空 / 1024 边界 / 1025 / 多字节中文 rune 计数）；handler Create/Update 超限 → INVALID_ARGUMENT 且消息含数值。
- JS 单测：单条 add/replace 超限文本（含上限与 wiki_write 引导）；批量含一条超限 → 全批不写（store 零调用断言）；`MEMORY_GUIDANCE` 文本含上限句。
- testplan（`projects/game/testplan/memory_test.go` 增例）：REST Create content > 1024 rune → 400。
