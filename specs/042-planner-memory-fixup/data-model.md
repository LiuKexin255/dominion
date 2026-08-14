# Data Model: planner 记忆校准实现修复

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](./spec.md)

> 本特性为 bugfix，不新增任何数据实体或状态字段。以下记录三处修复涉及的既有状态/行为模型的**变更点**（变更前 → 变更后），供 tasks 落实参考。

---

## 1. TeamState（不变）

`projects/game/agent/src/team/state.ts` / `graph.ts` 的 `TeamState`（`Annotation.Root`）**不受本特性影响**：

| 字段 | 类型 | 说明 |
|---|---|---|
| `playerMessages` | `BaseMessage[]`（`messagesStateReducer`） | 不变 |
| `plannerMessages` | `BaseMessage[]`（`messagesStateReducer`） | 不变 |
| `gameEnded` | `"won" \| "lost" \| null`（overwrite） | 不变 |
| `gameCounter` | `number`（overwrite） | 不变 |

- 039 US3（T019）曾新增 `pendingInstruction` 字段，但后续设计改为指令直写 `playerMessages`（contract §1："无独立指令槽"），故 `TeamState` 无 pendingInstruction 字段。
- 本特性不新增/删除任何 TeamState 字段。

---

## 2. 行为模型变更

### 2.1 `saolei_operate` 停止行（US1）

| 维度 | 变更前 | 变更后 |
|---|---|---|
| `operateResultText` 停止参数 | `stoppedAt: number \| null`（1-indexed 序号） | `stoppedOp: CellOperation \| null`（操作对象） |
| 停止行格式 | `stopped at op K (reason)` | `stopped at {type}({x},{y}) (reason)` |
| batch loop 追踪 | `stoppedAt = i + 1` | `stoppedOp = operations[i]` |
| 非停止行格式 | `executed N ops` / `skipped S no-op ops` | **不变** |

### 2.2 review 指令实时显示帧（US2）

| 维度 | 变更前 | 变更后 |
|---|---|---|
| review 节点指令写回 | `return { playerMessages: [new HumanMessage(instruction)] }` | 创建 `writeBack` → `ensureMessageId(writeBack)` → 发射 `emitChannelFrame` 帧 → `update.playerMessages = [writeBack]` |
| 实时显示帧 | ❌ 不发射 | ✅ 发射（`PRIMARY_AGENT_NAME` + `MESSAGE_ROLE_USER` + `writeBack.id`） |
| init/compact 帧发射 | ✅ 已有（不变） | ✅ 不变 |
| `ensureMessageId` 可见性 | `instruction-node.ts` 模块私有 | `instruction-node.ts` export（planner.ts 导入） |

### 2.3 RefreshTeam 后初始指令触发（US3）

| 维度 | 变更前 | 变更后 |
|---|---|---|
| `refreshTeam` 行为 | 仅清通道（`refreshTeamChannels`） | 清通道 + 触发指令（`startInstructionTurn`） |
| 指令触发方式 | `triggerInitInstruction`（一次性守卫） | 提取 `startInstructionTurn` 公共方法；`triggerInitInstruction` 调用它（保留守卫）；`refreshTeam` 调用它（无守卫） |
| `runInitTurn` 复用 | 仅 `triggerInitInstruction` 调用 | `triggerInitInstruction` + `refreshTeam` 均调用（经 `startInstructionTurn`） |
| `this.initTurn` 覆写 | 不覆写（一次性） | refresh 后覆写为新 Promise（可重复） |
| `isBusy()` / `isRunning()` | 不变 | 不变（`initInFlight` 机制既有） |
| handler.ts | 不变 | 不变 |

---

## 3. 不新增的实体

- ❌ 无新 proto 消息（039 已定义 Memory + MemoryService）。
- ❌ 无新 TeamState 字段。
- ❌ 无新 MCP 工具/工具参数。
- ❌ 无新服务/服务依赖。
- ❌ 无新 SKILL.md（仅更新既有 saolei SKILL.md 的格式字符串）。

---

## 4. 实体生命周期（不变）

| 实体 | 生命周期 | 本特性影响 |
|---|---|---|
| `Memory`（planner 长期记忆条目） | memory 服务持久化，压缩边界刷新冻结快照 | 不变 |
| Calibration Instruction（planner→player 指令） | HumanMessage 进 `playerMessages`；RefreshTeam/压缩清除 | 不变（review 指令实时显示为行为修复，非生命周期变更） |
| `FrozenMemorySnapshot` | 进程内冻结缓存，压缩/init 边界刷新 | 不变 |
| `TeamFrame`（实时显示帧） | 经 streamSink 发射，desktop 去重渲染 | review 场景新增帧发射（与 init/compact 对齐） |

---

## 5. 总结

本特性为纯 bugfix：无新数据实体、无新状态字段、无新消息类型。三处修复均为既有构件的行为对齐（格式一致化 / 帧发射对齐 / 方法提取），数据模型层面零增量。
