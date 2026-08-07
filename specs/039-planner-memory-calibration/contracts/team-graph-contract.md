# Contract: saolei Team StateGraph（更新：两场景节点 + 指令工具 + 冻结快照 + 移除 strategy）

**Feature**: `039-planner-memory-calibration` | **Spec**: [`spec.md`](../spec.md) | **Research**: D4/D5/D6/D8/D10

> 在 031/037 已落地的 team StateGraph 基础上更新：拆分两场景节点（review + init/compact）、planner 持 memory 工具 + 指令发送工具 + 冻结记忆快照、移除 StrategyStore、player 消费 pending 指令。原生 StateGraph + 自定义条件边（不依赖 swarm/supervisor）。实现于 `projects/game/agent/src/team/`。

---

## 1. State schema（`TeamState`，更新）

```ts
const TeamState = Annotation.Root({
  playerMessages:     Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] });
  plannerMessages:    Annotation<BaseMessage[]>({ reducer: messagesStateReducer, default: () => [] });
  gameEnded:          Annotation<"won"|"lost"|null>({ reducer: overwrite, default: () => null });
  gameCounter:        Annotation<number>({ reducer: overwrite, default: () => 0 });   // 037 既有
  pendingInstruction: Annotation<string|null>({ reducer: overwrite, default: () => null }); // 新增
});
```

- per-agent 通道（031）、`gameEnded`/`gameCounter`（031/037）不变。
- **新增 `pendingInstruction`**：init/compact 场景的待注入指令（D10）；player 节点入口消费。
- **移除**：策略不在 state（031 既有）；本特性彻底移除 StrategyStore，无替代 state 字段（记忆在冻结快照，§3）。
- checkpointer：单一外层 `MemorySaver`（031 既有）；createAgent 不带自身 checkpointer（031 spike D14）。
- `Annotation.Root`（非 zod StateSchema，031 spike D14 TS2883 坑）。

---

## 2. 节点与边（更新）

```text
START → [initInstruction]（仅 team 初始化触发一次，D10）→ player
 player ──条件(gameEnded≠null)──→ [review] ──条件(gameCounter%5===0)──→ [compress] → [postCompactInstruction] → END
                │                                       └─ review 非压缩 ──→ player（同 turn 继续）
                └──(gameEnded=null)──→ END/player
```

### 2.1 player 节点（更新）

- 形态：createAgent 全 loop（031 既有）；gameEndGuard（036）不变。
- **移除**：`buildStrategyMessage`/"当前态势"注入（FR-013）。
- **新增**：入口读 `state.pendingInstruction`，非空则作为 HumanMessage 注入 playerMessages（实现 FR-015/FR-016 的"与下次激活一同注入"），并返回 `{ pendingInstruction: null }` 清空。
- 工具：saolei mcp 工具（saolei_operate 等，仅 player）。
- 后处理：consumeGameEvent → `gameEnded`（031 既有，不变）。

### 2.2 review 节点（正常游戏结束；更新 `planner.ts`）

- 触发：条件边 `gameEnded ≠ null`（031 既有，每局一次）。
- **输入**：`plannerMessages` + gameLog（`saolei_operate` 为单位，FR-004）+ 冻结记忆快照（input SystemMessage，每条 `memory_id: 内容`，§3）。
- **工具**：`memory_add`/`memory_update`/`memory_remove`（memory mcp）+ `instruct_player`（指令发送，§4）。**移除 `update_strategy`**。
- planner 复盘（plannerMessages，对 player 不可见）→ **可选**调用 `instruct_player`（FR-014 可选）：调用则指令 HumanMessage 经 graph state update 追加 playerMessages（FR-017 消息顺序）；不调用则不产生指令。
- 返回：`{ plannerMessages, gameEnded: null, gameCounter: +1 }`。
- **冻结快照不在 review 刷新**（FR-010）。
- 路由：`gameCounter % 5 === 0` → compress；else → player（同 turn）。

### 2.3 initInstruction / postCompactInstruction 节点（新建 `instruction-node.ts`）

- **initInstruction**：team 初始化时**异步**触发一次（D10，R2——`UpdateTeam(allow_missing=true)` 物化路径（graph 首建）后即返回、不等 LLM；原 `SessionTeamStore.create`（AIP-133 CreateTeam）触发点被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede）。planner 仅依冻结记忆快照（首次烘焙，§3），经 prompt **要求**给 player 指令产出**无 gameLog**指令（LLM 决定是否调用 `instruct_player`，R4——无强制检验）；指令写入 `TeamState.pendingInstruction`（不触发 player invoke）。`UpdateTeam` 响应不含 player 输出。异步产出期间到达的 user message 须排在指令之后（player 首次激活时先注入 pending 指令）。**仅 graph 首建（物化）触发；profile 变更重建（040 FR-005）不重跑 initInstruction。**
- **postCompactInstruction**：compress 节点之后、END 之前。planner 依压缩刷新后的冻结快照，经 prompt 要求产出**无 gameLog**指令（因 player 指令历史已被压缩清理，FR-016；LLM 决定是否调用）；指令写入 `pendingInstruction`；turn 结束（END），随下次 player 激活注入。
- 两节点复用同一节点函数（参数区分 scenario）；prompt 措辞区分两场景与 review（init/compact 要求给指令；review 必要时才调用）。节点不做"是否调用工具"的强制检验（R4）。
- 不触发 player invoke（指令进 pending 槽，由 player 入口消费）。

### 2.4 compress 节点（037 既有，不变）

- 每 5 局（`gameCounter % 5 === 0`）触发，压 playerMessages/plannerMessages 为摘要（037 D1）。
- **新增**：压缩后触发冻结记忆快照刷新（review 之后、postCompactInstruction 之前）——重读 `listMemories` → 重新烘焙快照（§3，调研 D4）。

### 2.5 条件边

```ts
function routeAfterPlayer(state): "review" | "end" {
  return state.gameEnded ? "review" : "end";   // gameEnded=null → 结束 turn（等下次激活）
}
function routeAfterReview(state): "compress" | "player" {
  return state.gameCounter % 5 === 0 ? "compress" : "player";
}
```

> player 多局能力（FR-009）：正常 review 后路由回 player（同 turn 继续，是否开下一局由 LLM 决定）；仅 compress 后 turn 结束（与 037 一致）。

---

## 3. planner 冻结记忆快照（`memory-snapshot.ts`，调研 D5 方案 b）

```ts
class FrozenMemorySnapshot {
  private entries: { memory_id: string; content: string }[] = [];
  async refresh(memoryClient, template, session): Promise<void> {
    this.entries = await memoryClient.listMemories(template, session);  // 重读
  }
  toSystemMessage(): BaseMessage {
    const text = this.entries.map(e => `${e.memory_id}: ${e.content}`).join("\n");
    return new SystemMessage({ id: "planner-memory-snapshot", content: `长期记忆：\n${text}` });
  }
}
```

- **注入**：作为 planner 每次 invoke 的 input SystemMessage（`review`/`initInstruction`/`postCompactInstruction` 节点入口）；每条 `memory_id: 内容`（FR-011）。
- **不烘焙进 createAgent systemPrompt**（调研 D5）：planner 的 `systemPrompt` = base + 工具描述（不含记忆）。
- **冻结**：memory 工具改存储不刷新快照。
- **刷新边界**：team 初始化（首次 `refresh`）+ compress 节点（每 5 局 `refresh`，D4）。
- 过滤：invoke 写回时过滤掉 `planner-memory-snapshot` id（不进 plannerMessages 短期通道，同 031 strategy 过滤模式）。
- retain-vs-rebuild 优化（调研 §4.2）可选，plan 决定。

---

## 4. instruct_player 工具（`instruction-tool.ts`，FR-014/FR-017）

```ts
const instructPlayer = tool(async ({ content }) => {
  // 经 graph state update：追加 HumanMessage 到 playerMessages
  // review 场景：经 configurable 外部 buffer 中转（R1）——工具暂存 content，
  // 节点在 createAgent.invoke 返回后读暂存、由节点返回值写 playerMessages。
  return { ok: true };
}, { name: "instruct_player", schema: { content: z.string() } });
```

- **跨通道写入机制（R1 已决）**：planner 接收 gameLog 也是 HumanMessage（输入注入），指令发送与之对称。createAgent 子图内 tool 无法保证直写外层 `playerMessages` → 采用**外部 buffer 中转**（同 037 `emitChannelFrame` 的 configurable 暂存模式）：工具把指令 content 暂存到 configurable 提供的槽（如 `stageInstruction(content)`）→ planner 节点（review/init/compact）在 createAgent.invoke 返回后读暂存 → 由**节点返回值**写 `{playerMessages:[new HumanMessage(content)]}`（外层图通道，经 messagesStateReducer 追加）。
- **review 场景**：planner createAgent 持有；按 prompt"必要时才调用"（可选，LLM 决定）→ 暂存 → 节点返回值追加 playerMessages（紧跟游戏结束 tool_result，FR-017 顺序）。
- **init/compact 场景**：节点内 planner 经 prompt 要求给指令（LLM 决定，R4）→ 同样经外部 buffer 中转 → 节点写入 `pendingInstruction`（不直接进 playerMessages，由 player 入口消费）。
- **planner 复盘对 player 不可见**：复盘输出在 plannerMessages（per-agent channel）。

> R1 已决（外部 buffer 中转，规避子图 tool 无法直写外层通道的不确定性）——无需 plan spike 验证写入路径；与既有 gameLog HumanMessage 注入、037 emitChannelFrame 同模式。

---

## 5. 策略/记忆流（更新后）

```text
MemoryService (mongo, db game_memory, key=template+session+memory_id)
    ↑ Create/Update/Delete (memory mcp tools)        ↓ ListMemories (冻结快照刷新：init + compress)
    planner (memory_add/update/remove)               planner (FrozenMemorySnapshot → input SystemMessage)

MemorySaver checkpointer (per session thread)
    playerMessages  ← instruct_player（review 同 turn 追加 / init-compact 经 pendingInstruction 槽由 player 入口注入）
    plannerMessages ← planner 复盘输出（对 player 不可见）
    pendingInstruction ← initInstruction/postCompactInstruction 写入；player 入口消费清空

Ephemeral buffer (per session)
    sink.onOperate → gameLog（saolei_operate 为单位）→ review 节点复盘输入
                   → gameEvent → player 后处理 → gameEnded
```

- 记忆（冻结快照）与短期消息解耦：记忆在进程内冻结缓存（数据源 memory 服务），短期消息在 checkpointer；RefreshTeam/压缩清短期不影响冻结快照缓存（但压缩会触发快照从 memory 服务重读刷新）。

---

## 6. SessionTeam 初始化触发 initInstruction（D10，FR-015）

- `SessionTeamStore.update`（`UpdateTeam(allow_missing=true)` 物化路径——原 `SessionTeamStore.create`（AIP-133 CreateTeam）被 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede）在 team graph **首建**后**异步**执行一次 `initInstruction` 节点（R2，不等 LLM、`UpdateTeam` 物化即返回）：planner 经 prompt 要求产出初始指令（LLM 决定，R4）→ 写 `pendingInstruction`。须协调与 desktop Connect 的 typing-state 时序，及期间 user message 排在指令之后。**profile 变更重建（040 FR-005）不重跑 initInstruction（仅首建触发）。**
- player 首次激活（首次 user message → player invoke）时入口消费 `pendingInstruction` → 注入 playerMessages。
- `UpdateTeam` 响应不含 player 输出（init 不触发 player invoke）。
- 超时/错误降级（planner model 不可用）：plan 落实（如跳过 init 指令、记日志，不阻断 `UpdateTeam` 物化）。

---

## 7. RefreshTeam（031 既有，不变 + 清 pending）

- 经 `graph.updateState` 清 playerMessages/plannerMessages（031 §5 既有）。
- **新增**：同时清 `pendingInstruction`（避免过期指令残留）。
- 不影响冻结记忆快照（记忆在 memory 服务，快照缓存下次压缩边界自然刷新）。
- `gameCounter` 一并清零（037 既有）。

---

## 8. 验证要点

- review 节点 planner 持 memory_add/update/remove + instruct_player（无 update_strategy）；player 不持记忆/策略工具。
- 冻结记忆快照作为 input SystemMessage 注入（每条 `memory_id: 内容`），不烘焙进 createAgent systemPrompt；压缩/初始化边界刷新、review 不刷新。
- 正常 review 指令可选；消息顺序 `tool_calling → tool_result → planner 指令 → player output`；planner 复盘对 player 不可见。
- init/compact 经 prompt 引导产出无历史指令（LLM 决定是否调用，R4），进 pendingInstruction 槽，player 入口消费，不触发 player invoke。
- compress 后 turn 结束（与 037 一致）；review 非压缩路由回 player（FR-009）。
- StrategyStore 及全部引用移除（SC-005）。
- RefreshTeam 清两通道 + pendingInstruction + gameCounter；不影响冻结快照数据源。
