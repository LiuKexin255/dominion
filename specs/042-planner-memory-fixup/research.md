# Research: planner 记忆校准实现修复

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](./spec.md)

> Phase 0 设计决策。本特性是对 [`specs/039-planner-memory-calibration/`](../../039-planner-memory-calibration/) 已落地实现的三个 bugfix，设计决策聚焦于"以何种方式与既有架构对齐"而非"选择哪种新架构"。所有引用附相对路径（仓库内）。

---

## D1. saolei_operate 停止行格式：序号 → 操作参数

### 问题

`saolei_operate`（[`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`](../../039-planner-memory-calibration/contracts/saolei-operate-contract.md)）在批量操作中途停止时，`operateResultText`（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:660-680`）的停止行格式为：

```text
saolei_operate → stopped at op K (reason)
```

K 是操作在 operations 列表中的 1-indexed 序号。对于 LLM 而言，序号本身缺乏语义——它需要回溯自己的 operations 列表才能定位是哪个操作触发了停止。

### 决策

停止行中的序号 K 替换为**导致停止的具体操作的参数**，格式为 `{type}({x},{y})`（与 gameLog 渲染格式一致）：

```text
saolei_operate → stopped at click(4,4) (game_over)
saolei_operate → stopped at flag(5,6) (out_of_bounds)
saolei_operate → stopped at chord(3,3) (won)
```

**格式选择依据**：`{type}({x},{y})` 已用于 `buildReviewInput`（`projects/game/agent/src/team/planner.ts:172-174`）渲染 gameLog 中的操作列表：

```text
saolei_operate(click(4,4), flag(5,5), chord(3,3))
```

停止行复用同一格式使 player LLM 在停止行与 gameLog 中看到一致的操作表示，无需在两种格式间转换。单次形态（普通参数 `type`/`x`/`y`）的操作对象与批量形态的元素结构相同（`CellOperation = {type, x, y}`），格式天然一致。

### 实现方案

1. **`operateResultText` 签名变更**（`saolei-mcp.ts`）：
   - 参数 `stoppedAt: number | null` → `stoppedOp: CellOperation | null`。
   - 停止行模板：`stopped at op ${stoppedAt}` → `stopped at ${stoppedOp.type}(${stoppedOp.x},${stoppedOp.y})`。
   - 非停止行（`executed N ops` / `skipped S no-op ops`）不变。

2. **调用点变更**（`saolei-mcp.ts` `saolei_operate` handler 内的 batch loop）：
   - `stoppedAt = i + 1` → `stoppedOp = operations[i]`（在 `game end` 和 `stop` 两个 break 分支）。
   - `operateResultText(executed, skipped, finalState, stoppedAt, stoppedReason)` → `operateResultText(executed, skipped, finalState, stoppedOp, stoppedReason)`。

3. **影响面同步**（格式字符串一致性，宪法原则 II 架构/工具面变更同步）：
   - `projects/game/agent/src/skill/saolei/SKILL.md`：多处引用 `stopped at op K (reason)` 的描述行更新为 `stopped at type(x,y) (reason)`。
   - `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`：停止行断言更新。
   - `projects/game/testplan/agent_saolei_test.go`：停止行断言更新。
   - `projects/game/testplan/saolei_team_test.go`：停止行引用更新。
   - `projects/game/fake-llm/service/testdata/sample_saolei_structural_stop.yaml`：停止行注释/期望更新（如含）。

### Rationale

1. **与 gameLog 格式一致**（宪法原则 II 对齐）：gameLog 已用 `{type}({x},{y})` 渲染操作，停止行复用同一格式避免引入第二种操作表示。
2. **单次/批量统一**：`CellOperation` 结构对两种形态一致，无需额外分支逻辑。
3. **LLM 可读性**：`click(4,4)` 比 `op 3` 更直接——LLM 无需回溯列表即可判断后续策略。

### Alternatives considered

- ❌ 保留序号并追加操作参数（`stopped at op 3 click(4,4) (reason)`）：冗余，序号无用。
- ❌ 使用 JSON 格式（`stopped at {"type":"click","x":4,"y":4} (reason)`）：冗长，与 gameLog 格式不一致。

---

## D2. review 指令实时显示帧：复用 instruction-node 模式

### 问题

039 实现了两种 planner→player 指令投递场景：

| 场景 | 节点 | 实时显示帧 | 当前行为 |
|---|---|---|---|
| team 初始化 / 压缩后 | `instruction-node.ts`（`createInstructionNode`） | ✅ 发射 3 帧（planner request / planner tool-call response / **player write-back**） | 指令在 desktop player 对话列表**实时可见** |
| 正常游戏结束复盘 | `planner.ts`（`createPlannerNode`） | ❌ 仅发射 review input 帧，**不发射指令 write-back 帧** | 指令在 checkpoint 中存在、player LLM 能读到，但 desktop player 页面**不实时显示**（仅重新加载后出现） |

根因：`planner.ts` review 节点（`projects/game/agent/src/team/planner.ts:389-419`）在 `instruct_player` 暂存的 instruction 不为 null 时，直接在 return 中写 `{playerMessages: [new HumanMessage(instruction)]}`——但没有像 `instruction-node.ts`（`projects/game/agent/src/team/instruction-node.ts:299-319`）那样发射 `emitChannelFrame(PRIMARY_AGENT_NAME, instruction, ensureMessageId(writeBack), "MESSAGE_ROLE_USER")` 帧。

### 决策

review 节点在写入指令到 `playerMessages` 时，复用 instruction-node.ts 的同一帧发射模式：

```ts
// planner.ts review 节点 return 前（instruction !== null 分支）
const writeBack = new HumanMessage(instruction);
ensureMessageId(writeBack);  // 保证 frameId == 持久化消息 id（dedup）
emitChannelFrame(
    PRIMARY_AGENT_NAME,
    instruction,
    writeBack.id ?? undefined,
    "MESSAGE_ROLE_USER",
);
// 然后在 return 中写 {playerMessages: [writeBack]}
```

具体实现要点：
1. **`ensureMessageId` 复用**：`instruction-node.ts:138-141` 的 `ensureMessageId` 函数（`if (!msg.id) msg._updateId(randomUUID())`）——保证发射帧的 `frameId` 与 LangGraph checkpoint 持久化后的消息 id 一致（041 dedup 机制，`specs/041-realtime-init-push/contracts/realtime-channel-contract.md` §4）。planner.ts 当前不导入此函数；需要导入或提取到共享位置。
2. **`PRIMARY_AGENT_NAME` 导入**：planner.ts 已导入 `ChannelFrameEmitter` 类型（`projects/game/agent/src/session-team.ts`），但未导入 `PRIMARY_AGENT_NAME`（= `"player"`）。需要追加导入。
3. **`emitChannelFrame` 可用性**：planner.ts review 节点已在作用域内持有 `emitChannelFrame`（lines 321-323 读自 `config?.configurable?.emitChannelFrame`）——无需新增 configurable 读取。
4. **消息对象复用**：当前 return 写的是 `new HumanMessage(instruction)`——改为先创建 `writeBack` 变量、`ensureMessageId(writeBack)`、发射帧、return 中用 `writeBack`。这样发射帧与 checkpoint 持久化的是**同一个消息对象**（同 id），避免实时帧与 reloaded history 重复。

### Rationale

1. **与 init/compact 行为对齐**（宪法原则 II）：instruction-node.ts 已验证此模式有效（init 指令可实时显示），review 场景复用同一模式是最小、最一致的修复。
2. **帧与 checkpoint 一致**（041 dedup）：`ensureMessageId` 保证 `frameId == msg.id`，desktop 的 `renderedMessageIds` 按 `frameId` 去重，实时帧与 reloaded history 不重复。
3. **零新机制**：`emitChannelFrame` / `ensureMessageId` / `PRIMARY_AGENT_NAME` 均为既有构件，review 节点仅追加一次帧发射调用。

### `ensureMessageId` 的位置决策

`ensureMessageId` 当前定义于 `instruction-node.ts:138-141`（模块私有函数）。planner.ts 需要用它，有两种方案：

- **方案 A**：planner.ts 从 instruction-node.ts 导入 `ensureMessageId`（当前为模块私有，需 export）。
- **方案 B**：提取 `ensureMessageId` 到共享位置（如 `instruction-tool.ts` 或新工具模块）。

**选择方案 A**（最小改动）：`ensureMessageId` 的语义（"确保 BaseMessage 有稳定 id 用于 frameId dedup"）与 instruction 场景紧密耦合，且当前仅 instruction-node.ts 和 planner.ts 两个调用点。从 instruction-node.ts export 并在 planner.ts 导入，改动最小且不引入新文件。

### Alternatives considered

- ❌ 在 review 节点使用不同的帧发射方式：违反一致性（宪法原则 II），且增加维护成本。
- ❌ 不发射帧、仅靠 checkpoint 持久化（在 ListMessages 时可见）：这正是当前 buggy 行为——desktop player 页面不实时显示。

---

## D3. RefreshTeam 后初始指令触发：复用 runInitTurn + 提取公共方法

### 问题

039 的 RefreshTeam（`projects/game/agent/src/session-team.ts:316-318`）清空短期消息通道（`playerMessages`/`plannerMessages`）+ 重置 `gameCounter`，但**不触发**新的指令产出。这导致 RefreshTeam 后 player 处于与 team 初始化完全相同的"无指令历史"状态，却得不到初始指令引导。

team 初始化时（`SessionTeamStore.update` 首次物化），`triggerInitInstruction()`（`session-team.ts:340-349`）被调用，异步执行 `runInitTurn()`——一次图 `invoke` 携带 `runInitInstruction: true` configurable 标记，START 条件边路由到 `initInstruction` 节点 → END，planner 产出无游戏历史指令写入 `playerMessages`。但 `triggerInitInstruction` 有一次性守卫 `if (this.initTurn) return;`，无法重复触发。

### 决策

RefreshTeam 在清空通道后触发一次新的指令产出，复用 `runInitTurn()` 图执行逻辑。通过**提取公共方法 `startInstructionTurn()`** 实现（宪法原则 II 重构式变更——提取公共逻辑而非复制）：

```ts
// session-team.ts — 新提取的私有方法
private startInstructionTurn(): void {
    this.initInFlight = true;
    this.initTurn = this.runInitTurn().finally(() => {
        this.initInFlight = false;
    });
}

// 既有一次性触发（team 初始化）——保留守卫
triggerInitInstruction(): void {
    if (this.initTurn) return;  // 一次性守卫：仅 team 初始化
    this.startInstructionTurn();
}

// refreshTeam 追加指令触发（可重复）
async refreshTeam(): Promise<void> {
    await refreshTeamChannels(this.graphHandle.graph, this.sessionId);
    this.startInstructionTurn();  // 无守卫：每次 refresh 均触发
}
```

### 关键设计分析

#### 1. `this.initTurn` 的覆写安全性

`triggerInitInstruction` 的守卫 `if (this.initTurn) return;` 阻止重复触发。但 `refreshTeam` 在调用时：
- `isBusy()` 必为 false（handler.ts 在调用 `refreshTeam` 前检查 `isBusy()`）。
- 此时 `this.initTurn` 是一个已 resolved 的 Promise（team 初始化的 init turn 已完成）。
- `startInstructionTurn()` 直接覆写 `this.initTurn` 为新的 pending Promise——已 resolved 的旧 Promise 不影响任何已 await 它的代码（它们早已通过 await 点）。
- 后续 `runTeamTurn` 中的 `if (this.initTurn) { await this.initTurn; }` 会 await 新的 pending Promise——正确行为（用户消息排在 post-refresh 指令之后）。

#### 2. `isBusy()` / `isRunning()` 的守卫语义

- `isBusy()` = `this.initInFlight || this.isRunning()`——post-refresh 指令产出期间 `initInFlight = true`，所以 `isBusy()` 为 true，handler 会拒绝此期间的 RefreshTeam / profile-change rebuild（FR-012）。
- `isRunning()` = `this.turnLoop?.isRunning() ?? false`——**排除** `initInFlight`。post-refresh 指令不驱动 desktop typing indicator（与 team 初始化的 init instruction 一致，FR-011）。desktop 通过 channel frames 看到 planner 活动（request / tool-call response / player write-back 帧），而非 typing indicator。

#### 3. `runInitTurn` 的复用正确性

`runInitTurn()`（`session-team.ts:386-425`）执行 `graph.invoke({}, {configurable: {thread_id, runInitInstruction: true, instructionBuffer, emitChannelFrame}})`。RefreshTeam 后调用它：
- `playerMessages`/`plannerMessages` 刚被清空 → initInstruction 节点读到空的 `plannerMessages`（正确：无游戏历史）。
- `frozenSnapshot` 不受 RefreshTeam 影响（039 contract §7）→ initInstruction 节点读到既有冻结快照（正确：planner 依冻结记忆产出指令）。
- START 条件边 `routeAfterStart` 读 `runInitInstruction: true` → 路由到 `initInstruction` → END（player 不被 invoke，FR-015）。
- 指令写入 `playerMessages` → 随 player 首次激活注入（player 首次激活 = 下一条 user message → player invoke）。

#### 4. `runInitTurn` 方法名

`runInitTurn` 现服务于 team 初始化与 post-refresh 两个场景。方法名中的 "Init" 仍准确——两场景均产出"初始"指令（无游戏历史）。但为表达"可复用于 post-refresh"，建议**可选重命名**为 `runInstructionTurn`（非阻塞——内部实现不变，仅命名更准确）。plan 建议 tasks 落实时决定是否重命名。

#### 5. desktop 状态同步

post-refresh 指令产出期间，desktop 的行为与 team 初始化的 init instruction 完全一致：
- `isRunning()` = false → desktop 不显示 typing indicator。
- `emitChannelFrame` 通过 `streamSink` 发射帧 → desktop 实时看到 planner 的 request / tool-call response / player write-back 帧（streamSink 在用户连接时已绑定——用户通过 desktop 触发了 RefreshTeam）。
- 指令完成后，desktop player 对话列表出现指令帧（与 init 指令显示行为一致）。

### Rationale

1. **与 team 初始化行为对齐**（FR-008）：post-refresh 语义 = "清除短期记忆后重新开始"，与 team 初始化一致——需初始指令引导。
2. **提取公共方法**（宪法原则 II）：`triggerInitInstruction` 和 `refreshTeam` 共享 `runInitTurn + initInFlight 管理`逻辑，提取 `startInstructionTurn()` 消除重复。
3. **零新机制**：`runInitTurn`、`initInFlight`、`isBusy`、`initInstruction` 节点、`emitChannelFrame` 均为既有构件。

### Alternatives considered

- ❌ 在 `refreshTeam` 中复制 `runInitTurn` 逻辑：违反 DRY，且两份逻辑需同步维护。
- ❌ 新建 `postRefreshInstruction` 节点（独立于 `initInstruction`）：两节点行为完全相同（无游戏历史、prompt 引导、LLM 决定），复制无意义——`initInstruction` 节点已满足需求。
- ❌ RefreshTeam 后不触发指令（当前 buggy 行为）：player 在无引导状态下开始，体验退化（spec US3 已论证）。

### 待 plan/tasks 细化

- `runInitTurn` 是否重命名为 `runInstructionTurn`（非阻塞优化，建议但不强制）。
- `ensureMessageId` 从 instruction-node.ts export 的精确方式（方案 A，直接 export 函数）。

---

## 未解决的问题

所有设计未知项已在 D1–D3 决策。以下留 tasks 细化（非阻塞）：

- 停止行中操作参数的精确措辞确认（D1 已定 `{type}({x},{y})`，与 gameLog 一致）。
- `ensureMessageId` export 方式（D2 已定方案 A）。
- `runInitTurn` 是否重命名（D3 建议 `runInstructionTurn`，非阻塞）。
- SKILL.md / 测试断言中 `stopped at op K` → `stopped at type(x,y)` 的精确更新清单（grep 已列出全部引用点，tasks 逐文件落实）。

---

## 风险与缓解

| # | 风险 | 处置 |
|---|---|---|
| **R1** | 停止行格式变更影响面广（SKILL.md / 单测 / 大型测试 / fixture 均有断言） | grep 已列出全部引用点（`projects/game/` 内 43 处命中，`specs/039/` 内 1 处）。tasks 须逐文件更新，编译+单测+大型测试验证无遗漏。 |
| **R2** | review 指令帧发射可能造成实时帧与 checkpoint 持久化消息重复 | `ensureMessageId` 保证 `frameId == msg.id`（041 dedup 机制），desktop `renderedMessageIds` 按帧去重——与 init/compact 场景同一模式，已验证无重复。 |
| **R3** | RefreshTeam 后 `this.initTurn` 覆写可能与正在 await 的 `runTeamTurn` 冲突 | RefreshTeam 调用前 `isBusy()` 必为 false（无 turn 运行），无正在 await `initTurn` 的 `runTeamTurn`。覆写后新的 `runTeamTurn` await 新 Promise——正确排序。 |
