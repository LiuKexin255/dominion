# Contract: RefreshTeam 后初始指令触发（复用 runInitTurn + 提取公共方法）

**Feature**: `042-planner-memory-fixup` | **Spec**: [`spec.md`](../spec.md) | **Research**: D3

> RefreshTeam（`session-team.ts`）清空短期消息通道后，触发一次无游戏历史初始指令产出——与 team 初始化的 init instruction 行为一致。通过提取公共方法 `startInstructionTurn()` 实现（复用 `runInitTurn()` 图执行逻辑，去除 `triggerInitInstruction` 的一次性守卫使 refresh 可重复触发）。实现于 `projects/game/agent/src/session-team.ts`。

---

## 1. 当前实现

### 1.1 `refreshTeam`（仅清通道，无指令触发）

```ts
// session-team.ts:316-318 — 当前
async refreshTeam(): Promise<void> {
    await refreshTeamChannels(this.graphHandle.graph, this.sessionId);
}
```

### 1.2 `triggerInitInstruction`（一次性守卫）

```ts
// session-team.ts:340-349 — 当前
triggerInitInstruction(): void {
    if (this.initTurn) return;   // ← 一次性守卫：仅 team 初始化首建触发
    this.initInFlight = true;
    this.initTurn = this.runInitTurn().finally(() => {
        this.initInFlight = false;
    });
}
```

### 1.3 `runInitTurn`（图执行逻辑）

```ts
// session-team.ts:386-425 — 当前
private async runInitTurn(): Promise<void> {
    try {
        await this.graphHandle.graph.invoke(
            {},
            {
                configurable: {
                    thread_id: this.sessionId,
                    runInitInstruction: true,       // START 条件边 → initInstruction 节点
                    instructionBuffer: { content: null },  // R1 外部 buffer
                    emitChannelFrame: (...) => this.emitChannelFrame(...),  // 实时帧
                },
                metadata: { session_id: this.sessionId },
                recursionLimit: RECURSION_LIMIT,
            },
        );
    } catch (err) {
        // 降级：记日志、跳过指令（contract §6）
        warn("init instruction turn failed; skipping initial instruction", { ... });
    }
}
```

---

## 2. 修复方案

### 2.1 提取 `startInstructionTurn()` 公共方法

```ts
// session-team.ts — 新提取的私有方法
/**
 * Start (or re-start) an instruction turn: run the graph with the
 * `runInitInstruction` flag and track its in-flight state for the
 * `isBusy()` guard. Used by BOTH team-init (one-shot, via
 * triggerInitInstruction) and RefreshTeam (repeatable).
 */
private startInstructionTurn(): void {
    this.initInFlight = true;
    this.initTurn = this.runInitTurn().finally(() => {
        this.initInFlight = false;
    });
}
```

### 2.2 `triggerInitInstruction` 调用公共方法（保留守卫）

```ts
// session-team.ts — team 初始化一次性触发
triggerInitInstruction(): void {
    if (this.initTurn) return;   // 一次性守卫：仅 team 初始化首建
    this.startInstructionTurn();
}
```

### 2.3 `refreshTeam` 清通道后调用公共方法（无守卫）

```ts
// session-team.ts — refresh 后触发指令
async refreshTeam(): Promise<void> {
    await refreshTeamChannels(this.graphHandle.graph, this.sessionId);
    // 042 US3 (FR-008/FR-009): post-refresh instruction trigger — same
    // runInitTurn logic as team-init (no-game-history, prompt-guided,
    // LLM-decided). No one-shot guard: each refresh triggers a new
    // instruction (FR-013). The isBusy() gate (initInFlight) blocks a
    // second refresh/rebuild while the instruction is in-flight (FR-012).
    this.startInstructionTurn();
}
```

---

## 3. 关键设计分析

### 3.1 `this.initTurn` 覆写安全性

| 调用时机 | `this.initTurn` 状态 | 覆写安全性 |
|---|---|---|
| Team 初始化（`triggerInitInstruction`） | `null`（首次） | 安全：新建 Promise |
| RefreshTeam（`startInstructionTurn`） | 已 resolved Promise（init 已完成） | 安全：`isBusy()` 为 false 保证无 turn 正在 await；覆写后新 `runTeamTurn` await 新 Promise |
| RefreshTeam 期间再次 RefreshTeam | pending Promise（`initInFlight = true`） | 不可能：`isBusy()` 为 true → handler 拒绝（FAILED_PRECONDITION） |

### 3.2 `isBusy()` / `isRunning()` 守卫语义（不变）

| 方法 | 包含 `initInFlight`？ | 用途 |
|---|---|---|
| `isRunning()` | ❌ 不包含 | Connect status probe / desktop typing indicator |
| `isBusy()` | ✅ 包含 | RefreshTeam / profile-change rebuild 守卫 |

post-refresh 指令产出期间 `initInFlight = true`：
- `isBusy()` = true → handler 拒绝此期间的 RefreshTeam / rebuild（FR-012）。
- `isRunning()` = false → desktop 不显示 typing indicator（FR-011，与 team init 一致）。
- desktop 通过 `emitChannelFrame` 实时帧看到 planner 活动（request / tool-call response / player write-back 帧）。

### 3.3 `runInitTurn` 复用正确性

`runInitTurn` 执行 `graph.invoke({}, {configurable: {runInitInstruction: true, ...}})`。RefreshTeam 后调用它：

| 条件 | RefreshTeam 后的状态 | 正确性 |
|---|---|---|
| `playerMessages` | 已清空（`refreshTeamChannels` 清除） | ✅ initInstruction 节点写入新指令到空通道 |
| `plannerMessages` | 已清空 | ✅ initInstruction 节点读到空 plannerMessages（无游戏历史） |
| `frozenSnapshot` | 不受 RefreshTeam 影响（039 contract §7） | ✅ initInstruction 节点读到既有冻结快照 |
| `gameCounter` | 已重置为 0 | ✅ 不影响 initInstruction（它不读 gameCounter） |
| `emitChannelFrame` | streamSink 已绑定（用户通过 desktop 触发了 refresh） | ✅ 帧发射到达 desktop |
| `instructionBuffer` | 新建 `{ content: null }` | ✅ R1 外部 buffer 全新 |

### 3.4 `runInitTurn` 命名（可选优化）

`runInitTurn` 现服务于 team 初始化与 post-refresh 两场景。方法名 "Init" 仍准确（两场景均产出"初始"指令）。建议可选重命名为 `runInstructionTurn`（更准确表达可复用性），但非阻塞——tasks 落实时决定。

---

## 4. handler.ts 无需变更

`handler.ts` 的 RefreshTeam handler（`projects/game/agent/src/handler.ts:222-280`）调用 `team.refreshTeam()` 并 await 其完成。`refreshTeam` 内部触发指令后立即返回（fire-and-forget，`startInstructionTurn` 不被 await）——handler 在通道清除完成后即 respond `callback(null, {})`。这符合 FR-009："RefreshTeam 清空通道后即返回，不等 LLM"。

handler 的 `isBusy()` 守卫（`handler.ts:258`）已覆盖 post-refresh 指令产出期间的二次 refresh 拒绝——无需变更 handler。

---

## 5. 不受影响的部分

- **`refreshTeamChannels`**（`context-middleware.ts`）：不变（仅清除通道 + 重置 gameCounter）。
- **`initInstruction` 节点**（`instruction-node.ts`）：不变（已被 `runInitTurn` 的 `runInitInstruction: true` 正确路由触发）。
- **`routeAfterStart` 条件边**（`graph.ts`）：不变（读 `runInitInstruction` configurable 标记路由到 initInstruction）。
- **`triggerInitInstruction` 的一次性守卫**：保留不变（仅 team 初始化首建触发，profile 变更重建不重跑）。
- **039 contract §6（SessionTeam 初始化触发 initInstruction）**：不变（`triggerInitInstruction` 行为不变）。
- **039 contract §7（RefreshTeam）**：本特性增补"清通道后触发初始指令"行为——是 039 contract §7 的补充完善，不与之矛盾。

---

## 6. 验证要点

- `refreshTeam()` 清通道后调用 `startInstructionTurn()`（触发指令产出）。
- `startInstructionTurn()` 被 `triggerInitInstruction`（保留守卫）与 `refreshTeam`（无守卫）共用。
- post-refresh 指令产出复用 `runInitTurn()` 图执行逻辑（`runInitInstruction: true` configurable 标记）。
- `initInFlight` 在指令产出期间为 true（`isBusy()` 守卫生效）。
- post-refresh 期间 `isRunning()` 为 false（desktop typing indicator 不驱动）。
- post-refresh 期间 `emitChannelFrame` 帧发射到达 desktop（streamSink 已绑定）。
- 连续 RefreshTeam（每次完成后）每次均触发指令（FR-013）。
- post-refresh 指令产出失败降级（记日志、跳过指令，不阻断 RefreshTeam，FR-010）。
- handler.ts 无需变更（既有 `isBusy()` 守卫覆盖 post-refresh 期间的二次 refresh 拒绝）。
