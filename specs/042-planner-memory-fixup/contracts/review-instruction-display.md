# Contract: review 指令实时显示帧（复用 instruction-node 模式）

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](../spec.md) | **Research**: D2

> 正常游戏结束复盘场景（review 节点，`planner.ts`）下 planner 经 `instruct_player` 工具发送的校准指令，在写入 `playerMessages` 通道时发射实时显示帧——与 init/compact 场景（`instruction-node.ts`）同一模式。使指令在 desktop player 页面对话列表中实时可见（无需重新加载）。实现于 `projects/game/agent/src/team/planner.ts`。

---

## 1. 问题定位

### 1.1 init/compact 场景（正确行为）

`instruction-node.ts`（`projects/game/agent/src/team/instruction-node.ts:299-319`）在将指令写入 `playerMessages` 时，发射实时显示帧：

```ts
// instruction-node.ts — 正确模式（3 帧发射）
if (instruction !== null) {
    const writeBack = new HumanMessage(instruction);
    update.playerMessages = [writeBack];
    if (emitChannelFrame) {
        emitChannelFrame(
            PRIMARY_AGENT_NAME,       // "player"
            instruction,
            ensureMessageId(writeBack),  // frameId == msg.id（dedup）
            "MESSAGE_ROLE_USER",
        );
    }
}
```

### 1.2 review 场景（当前缺陷）

`planner.ts`（`projects/game/agent/src/team/planner.ts:389-419`）在 instruction 不为 null 时直接 return `{playerMessages: [new HumanMessage(instruction)]}`——**不发射任何实时显示帧**：

```ts
// planner.ts — 当前缺陷（无帧发射）
const instruction = instructionBuffer?.content ?? null;
if (instructionBuffer) {
    instructionBuffer.content = null;
}
return {
    plannerMessages: result.messages.filter(...),
    ...(instruction !== null
        ? { playerMessages: [new HumanMessage(instruction)] }  // ← 仅写 checkpoint，无帧
        : {}),
    gameEnded: null,
    gameCounter: state.gameCounter + 1,
};
```

**结果**：指令被写入 checkpoint（player LLM 在下次激活时能读到——player 回复证实它看到了指令），但 desktop player 页面对话列表看不到它（仅重新加载后经 ListMessages 出现）。

---

## 2. 修复方案

### 2.1 帧发射模式（复用 instruction-node.ts）

review 节点在 instruction 不为 null 时，复用 instruction-node.ts 的同一帧发射模式：

```ts
// planner.ts review 节点 — 修复后
const instruction = instructionBuffer?.content ?? null;
if (instructionBuffer) {
    instructionBuffer.content = null;
}

const update: Partial<TeamStateValue> = {
    plannerMessages: result.messages.filter(
        (m: BaseMessage) => m.id !== PLANNER_MEMORY_SNAPSHOT_ID,
    ),
    gameEnded: null,
    gameCounter: state.gameCounter + 1,
};

if (instruction !== null) {
    const writeBack = new HumanMessage(instruction);
    ensureMessageId(writeBack);  // 保证 frameId == 持久化消息 id（041 dedup）
    update.playerMessages = [writeBack];
    if (emitChannelFrame) {
        emitChannelFrame(
            PRIMARY_AGENT_NAME,
            instruction,
            writeBack.id ?? undefined,
            "MESSAGE_ROLE_USER",
        );
    }
}

return update;
```

### 2.2 变更清单

1. **导入追加**（`planner.ts`）：
   - `PRIMARY_AGENT_NAME` from `"../session-team"`（当前已导入 `ChannelFrameEmitter` 类型，需追加 `PRIMARY_AGENT_NAME` 常量）。
   - `ensureMessageId` from `"./instruction-node"`（当前 instruction-node.ts 中为模块私有函数 `function ensureMessageId(msg: BaseMessage): string`，需 export）。

2. **`ensureMessageId` export**（`instruction-node.ts`）：
   - 将 `function ensureMessageId(msg: BaseMessage): string` 从模块私有改为 `export function ensureMessageId(msg: BaseMessage): string`。
   - 函数实现不变（`if (!msg.id) msg._updateId(randomUUID()); return msg.id as string;`）。

3. **review 节点 return 重构**（`planner.ts`）：
   - 从 spread return（`...(instruction !== null ? { playerMessages: [...] } : {})`）改为 build-update-then-return 模式。
   - 追加帧发射（instruction 不为 null 且 emitChannelFrame 可用时）。

### 2.3 消息对象复用

修复后，发射帧的 `writeBack` 消息对象与 return 中写入 `update.playerMessages` 的是**同一对象**（同 id）。LangGraph 的 `messagesStateReducer` 将其追加到 checkpoint。desktop 的实时帧 `frameId == msg.id`（经 `ensureMessageId` 保证），与 reloaded ListMessages 条目去重（041 `renderedMessageIds` 机制）——无重复显示。

---

## 3. `emitChannelFrame` 可用性

review 节点已在作用域内持有 `emitChannelFrame`（`planner.ts:321-323`）：

```ts
const emitChannelFrame = config?.configurable?.emitChannelFrame as
    | ChannelFrameEmitter
    | undefined;
```

此读取发生在 review input 帧发射之前（lines 324-344 发射 review input 帧）。指令写回帧发射在同一 `emitChannelFrame` 作用域内——无需新增 configurable 读取。

`emitChannelFrame` 在 user-turn runner（`runTeamTurn`）中安装（`session-team.ts:684-689`），因此 review 场景（game end 触发、user turn 内执行）中 `emitChannelFrame` 始终可用。`undefined` 防御仅用于节点在非 team-turn 上下文调用时（测试场景）。

---

## 4. 消息顺序保证（FR-017 不变）

review 指令的消息顺序由既有机制保证（不变）：
- review 节点在 planner createAgent.invoke 返回后执行——此时 player 的 game-ending tool_result 已在 `playerMessages` 中。
- 指令 HumanMessage 经 `messagesStateReducer` 追加到 `playerMessages` 末尾（紧随 game-ending tool_result）。
- graph 路由回 player 后，player 下次激活读 `playerMessages` 时顺序为 `tool_calling → tool_result → planner 指令 → player 下一条 output`。
- 实时显示帧的发射时序（节点 return 前）与 checkpoint 追加时序（graph reducer 处理 return 后）一致——帧先发射、checkpoint 随后追加，desktop 先看到帧、后续 ListMessages 确认（dedup 无重复）。

---

## 5. 验证要点

- review 节点在 instruction 不为 null 时发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, writeBack.id, "MESSAGE_ROLE_USER")` 帧。
- 发射帧的 `frameId`（`writeBack.id`）与 checkpoint 持久化后的消息 id 一致（`ensureMessageId` 保证）。
- `ensureMessageId` 从 `instruction-node.ts` export，`planner.ts` 导入复用。
- `PRIMARY_AGENT_NAME` 从 `session-team` 导入到 `planner.ts`。
- review 指令在 desktop player 页面对话列表实时可见（无需重新加载）——行为与 init/compact 一致。
- 消息顺序不变（FR-017：紧跟 tool_result 之后、player 下一条 output 之前）。
- instruction 为 null 时不发射帧、不写 playerMessages（既有行为不变）。
