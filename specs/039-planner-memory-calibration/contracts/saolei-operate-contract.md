# Contract: saolei_operate（批量落子工具）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) | **Research**: D7

> 合并 `saolei_click`/`saolei_flag`/`saolei_chord_click` 为单个 `saolei_operate`（有序操作列表、保序执行、单次返回）。失败按拒绝原因细分（FR-002）。`saolei_init`/`saolei_remain` 不变（FR-003）。实现于 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`。sink 的 `onMove` → `onOperate`，gameLog 以 operate 为单位（FR-004/FR-005）。

---

## 1. 工具签名（MCP）

```ts
type OperationType = "click" | "flag" | "chord";
interface CellOperation { type: OperationType; x: number; y: number; }

saolei_operate(operations: CellOperation[]): MCPTextResult
```

- `operations`：有序列表，每个声明 `type`（枚举，非裸 string）+ 坐标 `(x, y)`。
- 顶层原点 `(0,0)`，x=列、y=行（既有约定）。
- 单一 MCP 文本内容块返回（FR-002）。

---

## 2. 执行语义（保序 + 失败细分，FR-001/FR-002）

按列表顺序依次处理每个 op（复用既有 `validateMove` + dispatch 逻辑，提取为按单 op 处理的内部函数）：

```text
for op in operations (顺序):
  verdict = validateMove(recognized, toCellTool(op.type), op.x, op.y)
  if verdict.ok:
    dispatch(op) → recognize → 更新 recognized
    operationCount++ (统计)
    sink.onOperate(...) 累积（见 §4，最后一次统一回调或按 op 回调——见下）
    if gameStatus(recognized) ∈ {won, lost}: STOP（游戏结束，剩余不执行）
  else if verdict.reason ∈ HARMLESS_NOOP_REASONS:
    SKIP op（不执行、不改棋盘），继续下一个
  else (verdict.reason ∈ STRUCTURAL_REASONS):
    STOP（结构性/上下文拒绝，剩余不执行）
返回单一结果：反映处理后的最终棋盘 + 游戏状态 + （若有）跳过/停止原因
```

### 拒绝原因分类（FR-002，澄清 Session 2026-08-07）

| 分类 | `MoveRejection` 原因码（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`） | 处理 |
|---|---|---|
| **无害空操作**（不改变棋盘） | `cell_already_revealed`、`cell_is_flagged`、`cannot_flag_revealed`、`chord_requires_number`、`chord_no_unrevealed_neighbor` | **SKIP**（跳过，继续剩余） |
| **结构性/上下文** | `out_of_bounds`、`no_active_game` | **STOP**（停止，剩余不执行） |
| **游戏结束**（非 validateMove 拒绝，是终局） | `gameStatus ∈ {won, lost}` | **STOP**（游戏结束，剩余不执行） |

> `HARMLESS_NOOP_REASONS` / `STRUCTURAL_REASONS` 为常量集合（`style/golang.md`/`style/javascript.md` 字面量常量），定义于 saolei-mcp.ts。

### 单次返回内容（FR-002）

- 正常完成全部 op（无停止）：结果行（如 `saolei_operate → executed N ops`）+ 游戏状态行 + 最终文本棋盘。
- 中途 STOP（游戏结束/结构性）：结果行（如 `saolei_operate → stopped at op K (reason)`）+ 游戏状态行 + 停止时棋盘。
- 中途 SKIP（无害空操作）：结果行标注跳过（如 `saolei_operate → executed M ops, skipped S no-op ops`）+ 游戏状态行 + 最终棋盘。
- `no_active_game`（初始无局）：复用既有 `rejectionText("no_active_game", null)` 引导调 `saolei_init`。
- recognize 失败：复用既有 `unrecognizableText()`。

---

## 3. 单元素等价（FR-002 验收 #3）

`saolei_operate([{type:"click",x,y}])` 等价于原 `saolei_click(x,y)`：同样校验、同样 dispatch、同样返回棋盘+状态行。

---

## 4. sink 与 gameLog（FR-004/FR-005）

### sink 接口扩展

```ts
export interface SaoleiEventSink {
  onGameStart(state: GameState): ...;                                      // 不变
  /** 批量操作后；operations 为本次 saolei_operate 的全部 op（含跳过/停止的） */
  onOperate(operations: CellOperation[], finalState: GameState, stats?: GameStats): ...;
  onGameEnd(state: GameState, status: "won"|"lost", stats?: GameStats): ...; // 不变
}
```

- `onMove(tool,x,y,state)` → 替换为 `onOperate(operations, finalState, stats?)`：一次 `saolei_operate` 触发一次 `onOperate`（携带全部 operations 列表 + 最终 state）。
- team sink 将 `onOperate` 写入 ephemeral buffer 的 **gameLog 为一条含全部 operations 的项**（FR-004，不再每 op 一条）。
- `onGameEnd` 不变（终局落子后触发）。

### gameLog 记录单位（FR-004）

- gameLog 每条：`{ tool: "saolei_operate", operations: CellOperation[], status, state }`（一条 = 一次批量调用）。
- planner 复盘输入（`buildReviewInput`）渲染 gameLog 时，每条展示 `saolei_operate(operations) → status` + 棋盘。

### planner 工具描述（FR-005）

`buildToolDescriptionSection`（`planner.ts`）改为描述 `saolei_operate`（而非三个独立工具）+ 其支持的 click/flag/chord 操作类型，与 gameLog 的 `saolei_operate` 操作类型一一对应。`saolei_init`/`saolei_remain` 描述按 037 D6 既有（game-visible 子集：init + operate；remain 不注入，037 FR-016 refine）。

---

## 5. 空操作列表（Edge Case）

`saolei_operate([])`（空列表）：不产生任何落子副作用；具体返回（视为无操作返回当前状态 / 视为非法）由 plan 决定（spec Edge Case 约束不产生副作用）。

---

## 6. 验证要点

- 落子工具面仅 `saolei_operate`（无 `saolei_click`/`saolei_flag`/`saolei_chord_click`）；`saolei_init`/`saolei_remain` 仍在。
- 批量按序执行、单次返回；单元素等价单次落子。
- 无害空操作拒绝（如在数字格 click）→ 跳过继续；结构性/上下文（越界、无活动局）→ 停止；游戏结束 → 停止。
- gameLog 以 `saolei_operate`（含全部 operations）为单位；planner 工具描述含 `saolei_operate` + click/flag/chord。
- sink `onOperate` 每批量调用触发一次，携带全部 operations。
