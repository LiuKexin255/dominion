# Contract: saolei_operate 停止行格式（序号 → 操作参数）

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](../spec.md) | **Research**: D1

> `saolei_operate`（[`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`](../../039-planner-memory-calibration/contracts/saolei-operate-contract.md)）批量操作中途停止时，结果行从 `stopped at op K (reason)`（K = 1-indexed 序号）改为 `stopped at {type}({x},{y}) (reason)`（具体操作参数）。格式与 gameLog 渲染一致。实现于 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `operateResultText`。

---

## 1. 签名变更

### 1.1 `operateResultText` 参数变更

```ts
// 变更前
function operateResultText(
    executed: number,
    skipped: number,
    state: GameState,
    stoppedAt: number | null,
    stoppedReason: MoveRejection | "won" | "lost" | null,
): string

// 变更后
function operateResultText(
    executed: number,
    skipped: number,
    state: GameState,
    stoppedOp: CellOperation | null,
    stoppedReason: MoveRejection | "won" | "lost" | null,
): string
```

- `stoppedAt: number | null`（1-indexed 序号）→ `stoppedOp: CellOperation | null`（导致停止的操作对象）。
- `CellOperation` = `{type: OperationType, x: number, y: number}`（既有类型，`saolei-mcp.ts:157-161`）。
- `null` 语义不变：`null` = 无停止（正常完成或全跳过），停止行不存在。

### 1.2 停止行模板变更

```text
变更前: saolei_operate → stopped at op ${stoppedAt} (${stoppedReason})
变更后: saolei_operate → stopped at ${stoppedOp.type}(${stoppedOp.x},${stoppedOp.y}) (${stoppedReason})
```

示例：
```text
saolei_operate → stopped at click(4,4) (game_over)
saolei_operate → stopped at flag(5,6) (out_of_bounds)
saolei_operate → stopped at chord(3,3) (won)
saolei_operate → stopped at click(0,0) (lost)
```

### 1.3 非停止行不变

```text
正常完成（无跳过）: saolei_operate → executed ${executed} ops
正常完成（含跳过）:   saolei_operate → executed ${executed} ops, skipped ${skipped} no-op ops
```

---

## 2. 调用点变更

### 2.1 batch loop（`saolei_operate` handler 内）

`saolei-mcp.ts` 的 `saolei_operate` handler batch loop（约 lines 1058-1130），将 `stoppedAt: number` 追踪改为 `stoppedOp: CellOperation | null` 追踪：

```ts
// 变更前
let stoppedAt: number | null = null;
// ...
for (let i = 0; i < operations.length; i += 1) {
    const result = await executeOperation(operations[i], extra);
    if (result.kind === "ok") {
        executed += 1;
        if (result.status !== "playing") {
            endedStatus = result.status;
            stoppedAt = i + 1;           // ← 1-indexed 序号
            stoppedReason = result.status;
            break;
        }
    } else if (result.kind === "stop") {
        stoppedAt = i + 1;               // ← 1-indexed 序号
        stoppedReason = result.reason;
        break;
    }
    // ...
}
// ...
return textResult(operateResultText(executed, skipped, finalState, stoppedAt, stoppedReason));

// 变更后
let stoppedOp: CellOperation | null = null;
// ...
for (let i = 0; i < operations.length; i += 1) {
    const result = await executeOperation(operations[i], extra);
    if (result.kind === "ok") {
        executed += 1;
        if (result.status !== "playing") {
            endedStatus = result.status;
            stoppedOp = operations[i];   // ← 导致停止的操作对象
            stoppedReason = result.status;
            break;
        }
    } else if (result.kind === "stop") {
        stoppedOp = operations[i];       // ← 导致停止的操作对象
        stoppedReason = result.reason;
        break;
    }
    // ...
}
// ...
return textResult(operateResultText(executed, skipped, finalState, stoppedOp, stoppedReason));
```

### 2.2 单次形态与批量形态的一致性

单次形态（普通参数 `type`/`x`/`y`）在入口归一化为 `operations: CellOperation[]`（既有逻辑，contract §3），因此 `operations[i]` 对单次形态也有效——单次操作的 `stoppedOp` 即为该单次操作参数。格式与批量形态一致（FR-002）。

---

## 3. 影响面同步（格式字符串一致性）

停止行格式变更影响以下文件中的格式字符串引用（grep `stopped at op` 确认的完整清单）：

### 3.1 `projects/game/agent/src/skill/saolei/SKILL.md`（player skill）

多处描述 `stopped at op K (reason)` 的行需更新为 `stopped at type(x,y) (reason)`。具体位置（SKILL.md 行号近似）：
- Tool-result body shape / outcome 行章节：`stopped at op K (reason)` 描述行。
- Move validation 章节：structural/terminal stop 结果行描述。
- Reason code 表：各 `stopped at op K (...)` 引用。
- Example play flow：`stopped at op 1 (game_won)` 示例。
- Summary 段：`stopped at op K (reason)` 引用。

**MUST 保留** `skill-loader.test.ts` 既有断言标记（frontmatter `name: saolei`、body 含 `# saolei` 与 `saolei_init`）。

### 3.2 `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`（单测）

停止行断言更新（约 lines 823, 854, 946, 955, 959, 993, 1499, 1621, 1854, 1877, 1880）：
- `stopped at op 1 (out_of_bounds)` → `stopped at {type}({x},{y}) (out_of_bounds)`（用各测试用例的实际操作参数）。
- `stopped at op 1 (game_over)` → `stopped at {type}({x},{y}) (game_over)`。
- `stopped at op 1 (game_won)` → `stopped at {type}({x},{y}) (game_won)`。
- `stopped at op 2 (out_of_bounds)` → `stopped at {type}({x},{y}) (out_of_bounds)`。
- `stopped at op 1 (won)` → `stopped at {type}({x},{y}) (won)`。
- `stopped at op 1 (lost)` → `stopped at {type}({x},{y}) (lost)`。

### 3.3 `projects/game/testplan/agent_saolei_test.go`（大型测试）

停止行断言更新（约 lines 42, 47, 52, 406, 492-493, 511, 577, 598-599, 684, 705-706）：
- 注释/断言中 `stopped at op K (reason)` → `stopped at {type}({x},{y}) (reason)`。

### 3.4 `projects/game/testplan/saolei_team_test.go`（大型测试）

停止行引用更新（约 lines 1155, 1182, 1324, 1328, 1338）：
- 注释/断言中 `stopped at op 1 (...)` → `stopped at {type}({x},{y}) (...)`。

### 3.5 `projects/game/testplan/helpers_test.go`

停止行引用（约 line 1850）：如有 `stopped at op` 引用则同步。

### 3.6 `projects/game/fake-llm/service/testdata/sample_saolei_structural_stop.yaml`

停止行注释/期望（line 7）：`stopped at op 2 (out_of_bounds)` 注释同步。

---

## 4. 不受影响的部分

- **gameLog 记录格式**（FR-004）：不变。gameLog 以一次 `saolei_operate` 调用为单位记录一条含全部操作的历史项，不包含停止行文本。
- **planner review input 渲染**（`buildReviewInput`）：不变。review input 渲染 gameLog 每条为 `saolei_operate(operations) → status` + 棋盘，不含停止行文本。
- **`saolei_init` / `saolei_remain` 返回格式**：不变。
- **拒绝行格式**（`rejected: <reason>`）：不变。拒绝行用于 pre-dispatch 整体拒绝（如 `no_active_game`），非批量停止。
- **039 contract**（`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` §2）：contract 中的 `stopped at op K (reason)` 为示意格式（`如 ... stopped at op K (reason)`），本特性更新实现格式后，039 contract 的示意描述 MAY 在后续维护中更新但非本特性 scope（本特性不改 039 spec/contract）。

---

## 5. 验证要点

- `operateResultText` 接受 `CellOperation | null`（非 number）作为停止操作参数。
- 停止行格式为 `stopped at {type}({x},{y}) (reason)`（含操作类型 + 坐标）。
- 非停止行格式不变（`executed N ops` / `skipped S no-op ops`）。
- 单次形态与批量形态的停止行格式一致。
- SKILL.md、单测、大型测试、fixture 中所有 `stopped at op K` 引用已同步更新。
- gameLog 记录格式不受影响。
