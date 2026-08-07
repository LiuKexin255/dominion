# Data Model: saolei Team 模板优化

**Feature**: `037-saolei-team-optimize` | **Spec**: [`spec.md`](./spec.md) | **Research**: [`research.md`](./research.md)

---

## 1. 实体概览

本特性在 031-team-template-mode 已有数据模型基础上新增/扩展以下实体：

| 实体 | 类型 | 生命周期 | 存储位置 |
|---|---|---|---|
| **游戏计数器（Game Counter）** | 新增 state 字段 | per-session, in-process | LangGraph state（`gameCounter`） |
| **压缩摘要消息（Compression Summary Message）** | 新增消息类型 | per-session 短期消息 | LangGraph channel（`playerMessages`/`plannerMessages`） |
| **通道消息实时帧（Channel-Message Live Frame）** | 新增机制产物 | 瞬时（帧发射后由 desktop 持有） | TurnLoop emit → desktop `chatMessages` |
| **游戏统计数据（Game Stats）** | 新增数据结构 | per-game, ephemeral | MCP closure → `onGameEnd` → ephemeral buffer |
| **工具描述清单（Tool Description Inventory）** | 新增静态文本 | per-team-build（不变） | planner systemPrompt |

---

## 2. 游戏计数器（Game Counter）

### State 字段

```ts
// projects/game/agent/src/team/state.ts — TeamStateValue 扩展

interface TeamStateValue {
  playerMessages: BaseMessage[];   // 不变
  plannerMessages: BaseMessage[];  // 不变
  gameEnded: GameEnded;            // 不变
  gameCounter: number;             // NEW — 已完成（且 planner 复盘过）的游戏局数
}
```

### 字段语义

| 属性 | 值 |
|---|---|
| 类型 | `number`（整数） |
| 初始值 | `0` |
| Reducer | last-write-wins：`(_prev: number, next: number) => next` |
| 递增时机 | planner 节点返回时（`gameEnded` 被 clear 为 null 的同时，`gameCounter += 1`） |
| 压缩触发 | `gameCounter > 0 && gameCounter % 5 === 0` |
| 重置 | `RefreshTeam` 时一并清零（与短期消息一起重置） |

### 状态转换

```text
gameCounter: 0
  → [game1 ends, planner returns] → gameCounter: 1
  → [game2 ends, planner returns] → gameCounter: 2
  → [game3 ends, planner returns] → gameCounter: 3
  → [game4 ends, planner returns] → gameCounter: 4
  → [game5 ends, planner returns] → gameCounter: 5 → 压缩触发
  → [compression done, turn ends, user input, game6 starts...]
  → [game6 ends, planner returns] → gameCounter: 6
  → ...
  → [game10 ends, planner returns] → gameCounter: 10 → 压缩再次触发
```

---

## 3. 压缩摘要消息（Compression Summary Message）

### 数据结构

压缩摘要是标准的 `AIMessage`，写入对应通道后成为该通道的唯一消息。

```ts
// 压缩产物：一条 AIMessage，概括压缩前通道的全部短期消息
const summaryMessage: AIMessage = new AIMessage({
  id: randomUUID(),         // 同时作为 frameId（去重锚点）
  content: summaryText,     // LLM 生成的摘要文本
});
```

### 通道状态转换

```text
压缩前：
  playerMessages: [msg1, msg2, ..., msgN]   // N 条短期消息
  plannerMessages: [msg1, msg2, ..., msgM]  // M 条短期消息

压缩后：
  playerMessages: [playerSummary]           // 仅 1 条摘要 AIMessage
  plannerMessages: [plannerSummary]         // 仅 1 条摘要 AIMessage
```

### 压缩写入机制

```ts
// 复用 messagesStateReducer 的 RemoveMessage 支持（RefreshTeam 已验证）
return {
  playerMessages: [
    new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),  // 清空全部
    playerSummary,                                     // 写入摘要
  ],
  plannerMessages: [
    new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
    plannerSummary,
  ],
};
```

### 验证规则

- FR-012: 摘要 MUST 是有意义的 agent message（非空、非占位）。
- FR-015: 空通道压缩 = 空操作（不产生空摘要）。
- FR-009: 策略（StrategyStore）不受压缩影响。

---

## 4. 通道消息实时帧（Channel-Message Live Frame）

### 机制描述

由 US1 建立的通用机制——将"非模型产出的通道消息"在写入通道的同时，作为一帧实时发射到对应 agent 的标签页。

### 帧构造

帧在 `SessionTeam` 闭包内构造（`runTeamTurn` 注入的 `configurable.emitChannelFrame`，类型 `ChannelFrameEmitter = (agent: string, content: string) => void`；tasks.md 决策 #1）；节点仅调用 `emitChannelFrame(agent, content)`，不直接持有 TeamFrame 构造依赖。

```ts
// session-team.ts runTeamTurn — emitter 闭包（复用 turn-loop.ts buildTeamFrame）
const frame: TeamFrame = buildTeamFrame(this.sessionId, this.template, {
  agent: "planner",  // 或 "player"
  messageParts: {
    parts: [{ text: { content: messageText } }],
  },
});
// frame.frameId === message.id（去重锚点）
```

### 使用场景

| 场景 | agent | 内容来源 | FR |
|---|---|---|---|
| planner 复盘输入 | `"planner"` | `buildReviewInput(buffer)` 的 HumanMessage 内容 | FR-001 |
| 压缩摘要（player 通道） | `"player"` | player model 生成的摘要文本 | FR-011 |
| 压缩摘要（planner 通道） | `"planner"` | planner model 生成的摘要文本 | FR-011 |

### 去重规则

- 实时帧 `frameId` 与通道消息 `id` 一致。
- desktop `renderedMessageIds` 集合在收到实时帧时 `add(frameId)`。
- 重载（ListMessages）时，相同 messageId 的消息被跳过（`renderedMessageIds.has(mid)`）。

---

## 5. 游戏统计数据（Game Stats）

### 数据结构

```ts
// 新增类型（projects/game/agent/src/mcp/saolei/saolei-mcp.ts 或独立文件）

interface GameStats {
  /** 本局成功的格子操作次数（onMove 触发次数）。 */
  operationCount: number;
  /** 本局正确标记的地雷数。null = counter 不可解码。 */
  correctFlags: number | null;
  /** 每雷平均操作数（operationCount / correctFlags），保留两位小数。"N/A" = 不可计算。 */
  avgOpsPerMine: number | "N/A";
}
```

### 计算逻辑

| 指标 | 计算 | 数据源 | 降级 |
|---|---|---|---|
| `operationCount` | MCP closure 计数器，每次 `registerCellTool` 成功识别后 `++`，`onGameStart` 时重置 0 | MCP 内部第一手 | 不适用（始终可计） |
| `correctFlags` | `totalMines − MINE格数 − HIT_MINE格数`；`totalMines` 取自 `initState.mineCounter.value` | MCP 终局 `state.grid` + 开局 `initState.mineCounter` | `initState.mineCounter` 不可解码 → `null` |
| `avgOpsPerMine` | `Math.round(operationCount / correctFlags * 100) / 100` | `operationCount` / `correctFlags` | `correctFlags === 0` 或 `null` → `"N/A"` |

### 存储与流转

```text
MCP closure（per-session）
  ├── initState: GameState | null     // onGameStart 时保存
  ├── operationCount: number          // onGameStart 重置, registerCellTool 递增
  └── onGameEnd 触发时计算 GameStats
        ↓
SaoleiEventSink.onGameEnd(state, status, stats)
        ↓
EphemeralGameBuffer.gameEvent.stats   // 随 gameEvent 存储
        ↓
planner buildReviewInput(buffer)      // 渲染到复盘 message
```

### EphemeralGameBuffer 扩展

```ts
// projects/game/agent/src/team/team-sink.ts — GameEventRecord 扩展

interface GameEventRecord {
  state: GameState;
  status: "won" | "lost";
  endedAt: number;
  consumed: boolean;
  stats?: GameStats;  // NEW — 游戏统计数据（可选，向后兼容）
}
```

### SaoleiEventSink 接口扩展

```ts
// projects/game/agent/src/mcp/saolei/saolei-mcp.ts — onGameEnd 参数扩展

interface SaoleiEventSink {
  onGameStart(state: GameState): void | Promise<void>;
  onMove(tool: CellTool, x: number, y: number, state: GameState): void | Promise<void>;
  // 扩展：第三参数 stats 为可选（向后兼容，FR-019 不变）
  onGameEnd(
    state: GameState,
    status: "won" | "lost",
    stats?: GameStats,
  ): void | Promise<void>;
}
```

---

## 6. planner 复盘输入扩展（buildReviewInput）

### 当前结构

```ts
// projects/game/agent/src/team/planner.ts — buildReviewInput 当前输出
function buildReviewInput(buffer: EphemeralGameBuffer): BaseMessage {
  const log = buffer.gameLog;
  if (log.length === 0) {
    return new HumanMessage("请复盘本局游戏（无可用游戏记录）。");
  }
  const lines: string[] = ["本局游戏过程："];
  for (let i = 0; i < log.length; i += 1) {
    // ... 每步操作的 tool, coord, status, board
  }
  lines.push("请复盘本局游戏表现...");
  return new HumanMessage(lines.join("\n"));
}
```

### 扩展后结构

```ts
function buildReviewInput(buffer: EphemeralGameBuffer): BaseMessage {
  const log = buffer.gameLog;
  if (log.length === 0) {
    return new HumanMessage("请复盘本局游戏（无可用游戏记录）。");
  }
  const lines: string[] = ["本局游戏过程："];
  for (let i = 0; i < log.length; i += 1) {
    // ... 每步操作（不变）
  }

  // NEW: 游戏统计数据
  const stats = buffer.gameEvent?.stats;
  if (stats) {
    lines.push("");
    lines.push("本局统计数据：");
    lines.push(`- 操作次数：${stats.operationCount}`);
    lines.push(`- 正确标记地雷数：${stats.correctFlags ?? "不可用"}`);
    lines.push(`- 每雷平均操作数：${stats.avgOpsPerMine}`);
  }

  lines.push("");
  lines.push("请复盘本局游戏表现...");
  return new HumanMessage(lines.join("\n"));
}
```

---

## 7. desktop 消息上限（FIFO）

### 数据结构

```ts
// projects/game/desktop/frontend/src/App.svelte

// 命名常量
const MAX_CHAT_ENTRIES_PER_AGENT = 200;

// 截断函数（FIFO：保留最新 N 条）
function trimFifo<T>(entries: T[], max: number = MAX_CHAT_ENTRIES_PER_AGENT): T[] {
  return entries.length > max ? entries.slice(-max) : entries;
}
```

### 应用点

| 写入位置 | 函数 | 当前行（App.svelte） | 截断方式 |
|---|---|---|---|
| 实时帧（流式合并） | `handleMessageParts` | ~739, ~744 | `trimFifo([...list])` after merge / new entry |
| 历史加载 | `loadAgentHistories` | ~511-513 | `trimFifo([...existing, ...entries])` |
| warn 控制帧 | `handleAgentFrame` warn 分支 | ~787-789 | `trimFifo([...list, warnEntry])` |
| optimistic user turn | `handleSendChatText` | ~882-884 | `trimFifo([...list, userEntry])` |

### 行为约束

- FR-020: 每个 agent tab 独立计数。
- FR-021: 超出上限时移除最旧（FIFO）。
- FR-022: 不同 tab 相互独立。
- FR-023: 压缩时不显式清理旧消息（自然 FIFO 滚动）。
- FR-024: ListMessages 返回超过上限时截断。
- FR-025: 实时流与历史加载统一 FIFO。

---

## 8. TeamGraphDeps 扩展

### 当前结构

```ts
// projects/game/agent/src/team/graph.ts
interface TeamGraphDeps {
  playerModel: ChatModel;
  plannerModel: ChatModel;
  strategyStore: StrategyStore;
  buffer: EphemeralGameBuffer;
  sessionId: string;
  playerTools: StructuredToolInterface[];
  playerBasePrompt: string;
  plannerBasePrompt: string;
  createAgentFn?: CreateAgentFn;
}
```

### 帧发射传递机制（US1/US2）

`TeamGraphDeps` **无变更**（tasks.md 决策 #1：不通过 deps 注入帧发射回调，宪法原则 II 简化）。实时帧发射经 LangGraph `configurable` 传递：

```ts
// projects/game/agent/src/session-team.ts — runTeamTurn 内 streamEvents config
type ChannelFrameEmitter = (agent: string, content: string) => void; // 从此模块导出

const stream = await graphHandle.graph.streamEvents(input, {
  configurable: {
    thread_id: this.sessionId,
    emitChannelFrame: (agent: string, content: string) => {
      const frame: TeamFrame = buildTeamFrame(this.sessionId, this.template, {
        agent,
        messageParts: { parts: [{ text: { content } }] },
      });
      this.turnLoopEmit?.(frame);
    },
  },
});
```

- planner/compress 节点从 `config?.configurable?.emitChannelFrame` 读取（`ChannelFrameEmitter | undefined`）。
- `SessionTeam` 已持有 `sessionId` 与 `template`（既有字段，`session-team.ts` 构造器），无需传入 TeamGraphDeps。
- 测试中通过 `streamEvents` config 注入录制回调。

---

## 9. 实体关系图

```text
┌──────────────────────────────────────────────────────────────────┐
│                        TeamState (LangGraph)                     │
│                                                                  │
│  playerMessages ──── messagesStateReducer ──── [summary | msgs] │
│  plannerMessages ─── messagesStateReducer ──── [summary | msgs] │
│  gameEnded ───────── last-write-wins ───────── "won"|"lost"|null│
│  gameCounter ─────── last-write-wins ───────── number (NEW)     │
└──────────────────────────────────────────────────────────────────┘
         │                                          │
         │ write                                    │ read
         ▼                                          ▼
┌─────────────────────┐                   ┌──────────────────────┐
│  Planner Node       │                   │  Compress Node (NEW) │
│  gameCounter += 1   │──────────────────▶│  if counter % 5 == 0 │
│  gameEnded = null   │  conditional edge │  → summarize channels│
│  buildReviewInput   │                   │  → emitChannelFrame  │
│  emitChannelFrame   │                   │  → route to END      │
└─────────────────────┘                   └──────────────────────┘
         │
         │ read
         ▼
┌──────────────────────────────────────────────────────────────────┐
│                   EphemeralGameBuffer (per-session)              │
│                                                                  │
│  gameState: GameState | null                                    │
│  gameEvent: GameEventRecord | null                              │
│    └── stats?: GameStats (NEW)                                  │
│  gameLog: GameLogEntry[]                                        │
└──────────────────────────────────────────────────────────────────┘
         ▲
         │ write
┌─────────────────────┐
│  Team Sink          │
│  onGameEnd(stats)   │
└─────────────────────┘
         ▲
         │ invokes with stats
┌──────────────────────────────────────────────────────────────────┐
│              Saolei MCP Server (per-session closure)            │
│                                                                  │
│  initState: GameState | null (NEW)                              │
│  operationCount: number (NEW)                                   │
│  on onGameEnd: compute GameStats → sink.onGameEnd(s, st, stats) │
└──────────────────────────────────────────────────────────────────┘
```
