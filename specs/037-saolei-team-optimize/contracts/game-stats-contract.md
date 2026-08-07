# Contract: SaoleiEventSink 游戏统计数据扩展

**Feature**: `037-saolei-team-optimize` | **Spec**: [`../spec.md`](../spec.md) | **Data Model**: [`../data-model.md`](../data-model.md)

> 扩展 `SaoleiEventSink.onGameEnd` 携带游戏统计数据。统计数据由 MCP 内部第一手计算（operationCount + correctFlags + avgOpsPerMine），经 sink → ephemeral buffer → planner 复盘输入流转。接口仅描述事件形状，不耦合 team mode（FR-019 不变）。

---

## 1. GameStats 类型

**文件**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（或独立 types 文件）

```ts
/**
 * Per-game quantitative statistics, computed first-hand by the MCP at game end.
 * Carried by onGameEnd → ephemeral buffer → planner review input.
 */
export interface GameStats {
  /** Count of successful cell operations this game (onMove trigger count).
   *  Excludes init, remain, rejected moves, and LLM call count. */
  operationCount: number;
  /** Number of correctly flagged mines this game.
   *  null = init mineCounter undecodable (totalMines unknown). */
  correctFlags: number | null;
  /** operationCount / correctFlags, rounded to 2 decimals.
   *  "N/A" = correctFlags is 0 or null (division by zero / unknown). */
  avgOpsPerMine: number | "N/A";
}
```

---

## 2. SaoleiEventSink 接口扩展

**文件**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`

```ts
export interface SaoleiEventSink {
  onGameStart(state: GameState): void | Promise<void>;
  onMove(tool: CellTool, x: number, y: number, state: GameState): void | Promise<void>;
  /** 携带游戏统计（可选第三参数，向后兼容）。 */
  onGameEnd(
    state: GameState,
    status: "won" | "lost",
    stats?: GameStats,  // NEW — 可选，向后兼容
  ): void | Promise<void>;
}
```

- `stats` 为可选参数：未升级的 sink 实现不需要修改（向后兼容）。
- 接口仍**不引用** team/strategy/store 概念（FR-019 不变）。

---

## 3. MCP 内部计算逻辑

**文件**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — `createSaoleiMcpServer` closure

### 新增 closure 变量

```ts
let recognized: GameState | null = null;  // 已有
let initState: GameState | null = null;    // NEW — 保存开局识别状态（用于取 mineCounter）
let operationCount = 0;                    // NEW — 本局操作计数器
```

### onGameStart（saolei_init handler 内）

```ts
// saolei_init 成功后
initState = state;       // NEW — 保存开局状态
operationCount = 0;      // NEW — 重置操作计数
await runSink("onGameStart", (s) => s.onGameStart(state));
```

### registerCellTool handler 内

```ts
// 在 recognize 成功后、runSink("onMove") 调用处
operationCount++;  // NEW — 计数（仅在成功识别后，被拒落子在 validateMove 前 return）
await runSink("onMove", (s) => s.onMove(name, x, y, state));
const status = gameStatus(state);
if (status === "won" || status === "lost") {
  const stats = computeGameStats(initState, state, operationCount);  // NEW
  await runSink("onGameEnd", (s) => s.onGameEnd(state, status, stats));
}
```

### computeGameStats 纯函数

```ts
function computeGameStats(
  initState: GameState | null,
  finalState: GameState,
  operationCount: number,
): GameStats {
  // correctFlags = totalMines − MINE格数 − HIT_MINE格数
  // totalMines 取自 initState.mineCounter（开局 flags=0 时 value = mines）
  const counter = initState?.mineCounter;
  let correctFlags: number | null;

  if (counter?.decoded === true) {
    const totalMines = counter.value;  // 开局 flags=0, counter = mines
    let mineCells = 0;
    let hitMineCells = 0;
    for (const row of finalState.grid) {
      for (const cell of row) {
        if (cell === "MINE") mineCells++;
        if (cell === "HIT_MINE") hitMineCells++;
      }
    }
    correctFlags = totalMines - mineCells - hitMineCells;
  } else {
    correctFlags = null;  // counter 不可解码
  }

  // avgOpsPerMine
  let avgOpsPerMine: number | "N/A";
  if (correctFlags !== null && correctFlags > 0) {
    avgOpsPerMine = Math.round((operationCount / correctFlags) * 100) / 100;
  } else {
    avgOpsPerMine = "N/A";
  }

  return { operationCount, correctFlags, avgOpsPerMine };
}
```

---

## 4. EphemeralGameBuffer 扩展

**文件**: `projects/game/agent/src/team/team-sink.ts`

```ts
export interface GameEventRecord {
  state: GameState;
  status: "won" | "lost";
  endedAt: number;
  consumed: boolean;
  stats?: GameStats;  // NEW — 游戏统计数据
}
```

### createTeamSink onGameEnd 扩展

```ts
onGameEnd: (state: GameState, status: "won" | "lost", stats?: GameStats) => {
  buffer.gameEvent = {
    state,
    status,
    endedAt: Date.now(),
    consumed: false,
    stats,  // NEW
  };
  buffer.gameState = state;
  buffer.gameLog.push({ tool: "(game-end)", state, status });
},
```

---

## 5. planner buildReviewInput 扩展

**文件**: `projects/game/agent/src/team/planner.ts`

```ts
function buildReviewInput(buffer: EphemeralGameBuffer): BaseMessage {
  const log = buffer.gameLog;
  if (log.length === 0) {
    return new HumanMessage("请复盘本局游戏（无可用游戏记录）。");
  }
  const lines: string[] = ["本局游戏过程："];
  for (let i = 0; i < log.length; i += 1) {
    const entry = log[i];
    const coord = entry.x != null ? `(${entry.x}, ${entry.y})` : "";
    lines.push(`${i + 1}. ${entry.tool}${coord} → ${entry.status}`);
    lines.push(renderBoardText(entry.state));
    lines.push("");
  }

  // NEW: 游戏统计数据
  const stats = buffer.gameEvent?.stats;
  if (stats) {
    lines.push("本局统计数据：");
    lines.push(`- 操作次数：${stats.operationCount}`);
    lines.push(`- 正确标记地雷数：${stats.correctFlags ?? "不可用"}`);
    lines.push(`- 每雷平均操作数：${stats.avgOpsPerMine}`);
    lines.push("");
  }

  lines.push("请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy。");
  return new HumanMessage(lines.join("\n"));
}
```

---

## 6. 向后兼容

| 变更 | 兼容性 |
|---|---|
| `onGameEnd` 新增可选 `stats?` 参数 | 向后兼容——未升级的 sink 实现不受影响 |
| `GameEventRecord` 新增可选 `stats?` 字段 | 向后兼容——已有代码不读 `stats` 字段不受影响 |
| MCP closure 新增 `initState` / `operationCount` | 内部变更，无接口影响 |
| `computeGameStats` 纯函数 | 新增，无影响 |

---

## 7. 验证要点

- **operationCount 口径**：仅计 `registerCellTool` 内成功识别后的操作（click/flag/chord）；init/remain/被拒落子不计。
- **correctFlags won 局**：全部地雷被正确标记，MINE=0, HIT_MINE=0，correctFlags = totalMines。
- **correctFlags lost 局**：correctFlags = totalMines − MINE格数 − HIT_MINE格数。
- **correctFlags counter 不可解码**：`initState.mineCounter` 为 `{ decoded: false }` 或 `undefined` → correctFlags = null。
- **avgOpsPerMine y=0**：如开局即踩雷（correctFlags=0），avgOpsPerMine = "N/A"（不崩溃、不产生 NaN/Infinity）。
- **统计数据流转**：MCP → sink → buffer → planner buildReviewInput → 复盘 message 包含三项统计。
- **接口隔离**：`SaoleiEventSink` / `GameStats` 不引用 team/strategy/store 概念（FR-019）。
