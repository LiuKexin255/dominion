# Data Model: Team Template Mode 缺陷修复

**Feature**: `036-team-mode-bugfix` | **Spec**: [`spec.md`](./spec.md) | **Research**: D1-D5

> 本特性为 `specs/031-team-template-mode` 的缺陷修复。数据模型变更仅涉及 Issue 2（ephemeral buffer 扩展）与 Issue 1（player middleware）。无 proto / API 契约变更。

---

## 1. EphemeralGameBuffer（扩展）

### 现有结构（`projects/game/agent/src/team/team-sink.ts`）

```ts
export interface EphemeralGameBuffer {
  gameState: GameState | null;
  gameEvent: GameEventRecord | null;
}
```

### 变更后结构

```ts
export interface EphemeralGameBuffer {
  gameState: GameState | null;
  gameEvent: GameEventRecord | null;
  gameLog: GameLogEntry[];   // 新增：完整操作序列（每局重置）
}
```

**初始化变更**：`createEphemeralGameBuffer()` 返回 `{ gameState: null, gameEvent: null, gameLog: [] }`。

---

## 2. GameLogEntry（新增）

```ts
export interface GameLogEntry {
  /** 触发本步操作的工具名称（如 "saolei_init"、"saolei_click"）。
   *  游戏结束事件用字面量 "(game-end)"。 */
  tool: string;
  /** 操作的 x 坐标（click/flag 适用；init 无）。 */
  x?: number;
  /** 操作的 y 坐标（click/flag 适用；init 无）。 */
  y?: number;
  /** 操作后的棋盘状态（文本渲染供 planner 复盘）。 */
  state: GameState;
  /** 操作后的游戏状态枚举。 */
  status: "won" | "lost" | "playing";
}
```

### 写入规则（sink 回调）

| sink 回调 | gameLog 变更 |
|---|---|
| `onGameStart(state)` | **清空** `gameLog = []`，push `{ tool: "saolei_init", state, status: "playing" }` |
| `onMove(tool, x, y, state)` | push `{ tool, x, y, state, status: gameStatus(state) }` |
| `onGameEnd(state, status)` | push `{ tool: "(game-end)", state, status }`（status = won/lost） |

**关键**：`onGameStart` 清空 gameLog 确保每局独立（planner 仅复盘当前局）。

### 读取方

planner 节点的 `buildReviewInput(buffer)` 读取 `buffer.gameLog`，渲染为文本：

```
本局游戏过程：
1. saolei_init → playing
   [棋盘文本]
2. saolei_click(3,4) → playing
   [棋盘文本]
3. saolei_click(5,2) → lost
   [棋盘文本]
4. (game-end) → lost
   [棋盘文本]

请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy。
```

空 gameLog 时渲染为：`"请复盘本局游戏（无可用游戏记录）。"`

---

## 3. 状态计算（gameStatus 逻辑）

`gameStatus(state)` 返回 `"won" | "lost" | "playing"`，判定逻辑为 `isTerminalState(state) ? "lost" : isWin(state) ? "won" : "playing"`。该函数是 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:285` 的**私有函数（未导出）**，team-sink 无法直接调用。team-sink 的 `onMove` 回调通过 import `isTerminalState`（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:255`）与 `isWin`（`@dominion/game-saolei-board`，`projects/game/pkg/saolei-board/src/core/index.ts:53`）自行计算，判定顺序与 MCP 侧一致（实现见 `tasks.md` T003）。

---

## 4. 无 proto 变更

本特性不修改任何 proto 定义：
- TeamState schema 不变（`playerMessages`/`plannerMessages`/`gameEnded`）。
- gameLog 是进程内 ephemeral buffer 的字段，不在 LangGraph state 中，不经过 checkpointer。
- AgentFrame / MessagePart proto 不变。
- 不新增 RPC。
