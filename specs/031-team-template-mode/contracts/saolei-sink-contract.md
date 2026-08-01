# Contract: saolei MCP 旁路事件 Sink

**Feature**: `031-team-template-mode` | **Spec**: [`spec.md`](../spec.md) | **Research**: D9

> saolei MCP 提供的可选旁路事件 sink 注册接口。MCP 仅定义事件形状，**不耦合 team mode**（不引用 team/strategy/store/teamMemoryId）。默认无 sink 行为零变化（FR-020）。team 侧注册实现将事件写入进程内 ephemeral buffer（驱动 planner 触发，D6/D7）。

---

## 1. 接口（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`）

```ts
export interface SaoleiEventSink {
  /** saolei_init 后（新局开始）。 */
  onGameStart(state: GameState): void | Promise<void>;
  /** 每次落子工具（click/flag）后，携带最新 state。 */
  onMove(tool: CellTool, x: number, y: number, state: GameState): void | Promise<void>;
  /** gameStatus 变为 won/lost 时触发；status 为结构化枚举（非文本）。 */
  onGameEnd(state: GameState, status: "won" | "lost"): void | Promise<void>;
}
```

- `GameState` / `CellTool` 复用 `@dominion/game-saolei-board` 既有类型。
- 接口**仅描述事件形状**；不引用 team/strategy/store/teamMemoryId（FR-019）。

## 2. 注册点（`createSaoleiMcpServer`）

```ts
export function createSaoleiMcpServer(
  bridge: OperationBridge,
  boardApi: SaoleiBoardApi = createDefaultBoardApi(),
  sink?: SaoleiEventSink,   // 可选；默认 undefined（零行为变化，FR-020）
): McpServer;
```

- `sink` 可选第三参数（向后兼容）。
- MCP handler 在 `recognize()` 成功后、`gameStatus(state)`（`saolei-mcp.ts:253`）变化时调 `sink?.onXxx()`。
- 信号源 = MCP 内部第一手 `gameStatus()` 计算（FR-017），**非** tool result 文本解析。

## 3. 调用时机

| 事件 | 触发点 | 携带 |
|---|---|---|
| `onGameStart` | `saolei_init` recognize 成功后 | 初始 `state` |
| `onMove` | `saolei_click`/`saolei_flag` recognize 成功后 | `tool, x, y, state` |
| `onGameEnd` | 任一落子后 `gameStatus` ∈ {won, lost} | `state, status`（结构化） |

> `onGameEnd` 与 `onMove` 在终局落子上可先后都触发（先 onMove 更新 state，再 onGameEnd 报结束）。`saolei_remain`（只读）不触发 onMove/onGameEnd。

## 4. team 侧 sink 实现（消费者；`projects/game/agent/src/team/`）

```ts
const teamSink: SaoleiEventSink = {
  onGameStart: async (_state) => { /* 可选：重置 buffer */ },
  onMove: async (_t, _x, _y, state) => { buffer.gameState = state; },
  onGameEnd: async (state, status) => {
    buffer.gameEvent = { state, status, endedAt: Date.now(), consumed: false };
    buffer.gameState = state;
  },
};
// createSaoleiMcpServer(bridge, undefined, teamSink);
```

- 写入 per-session **ephemeral buffer**（D7，进程内，非持久、非"记忆"）。
- player wrapper 读 `gameEvent` → `TeamState.gameEnded`；planner 读 `gameState` 复盘（D6）。

## 5. 错误隔离

- sink 回调抛错**不得**影响 MCP 工具主流程（游戏操作仍正常返回，edge case）。
- 具体隔离（try/catch + 日志）由 `tasks.md` 实现；约束：工具返回值不受 sink 异常影响。

## 6. mcp-host 适配（`projects/game/agent/src/mcp-host.ts`）

- `SessionBridgeLookup` 扩展：除 `bridge` 外，传递 team sink（由 SessionTeam 提供，绑定其 ephemeral buffer）。
- 修正 `mcp-host.ts:9` 过时注释（spec 025 后 `recognized` 状态已存在；现又增 sink）。

## 7. 验证要点

- 未传 sink：所有 saolei 工具行为与升级前一致（FR-020）。
- 传记录型 sink：onGameStart/onMove/onGameEnd 按时机回调；onGameEnd 的 `status` 为 `"won"|"lost"` 枚举（非文本）。
- 接口类型审查：无 team/strategy/store/teamMemoryId 引用（FR-019）。
