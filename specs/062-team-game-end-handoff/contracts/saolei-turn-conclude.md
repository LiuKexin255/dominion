# Contract: saolei 终局收束——ToolOutcome 扩展与工具层映射

**Feature**: [spec.md](../spec.md) FR-001/FR-002/FR-006 | **决策**: [research.md](../research.md) D1/D2 | **数据模型**: [data-model.md](../data-model.md) §1/§2

**基线契约**: `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2（`ToolOutcome`/`GameRuntime` 类型）与 §3（三工具 exec 体映射）——本契约是其终局收束语义的扩展声明；本 feature 同步将 051 §2 类型行与 §3 映射更新为终态（非增量 patch），两份契约保持一致。

## 1. ToolOutcome 扩展（@dominion/dsh-saolei-loop → @dominion/dsh-saolei 契约）

```ts
// common/js/dsh-plugins/saolei-loop/src/game/runtime.ts
export type ToolOutcome =
  | { isError: false; text: string; concludesTurn?: true }
  | { isError: true; error: { message: string } };
```

- `concludesTurn?: true`：**仅成功分支**、可选、仅在置位时携带。语义：该结果的识别棋盘为终局（`gameStatus(state) ∈ {won, lost}`）。
- **置位规则（无条件、纯内容驱动）**：判定只依赖本次返回的结果棋盘。置位/不置位的逐路径矩阵见 [data-model.md](../data-model.md) §1.2（11 行 = runtime 10 行 + 工具层参数拒绝 1 行，SC-004 断言面；runtime 侧断言在 `runtime.test.ts`，工具层在 `index.test.ts`）。
- **MUST NOT**：不因排队消息（如编排 FIFO）存在与否变化；不读不写 `gameEvent`/`reviewedGameEvent`/编排状态；错误分支不可携带（类型保证，对齐 dsh `ToolExecutionFailure.concludesTurn?: never`）。
- `saolei_remain` 永不置位；无棋盘结果（`no_active_game`/`unable to recognize board`/参数组合拒绝）不置位。

## 2. 工具层映射（@dominion/dsh-saolei executeOutcome 扩展）

`common/js/dsh-plugins/saolei/src/index.ts` `executeOutcome`（三工具唯一 outcome 消费点）：

```ts
const outcome = await run(resolveRuntime(exec));
if (outcome.isError) {
  throw new Error(outcome.error.message);      // 既有：模型可见失败
}
if (outcome.concludesTurn === true) {
  exec.concludeTurn();                          // 新增：dsh 官方收束 seam
}
return { result: outcome.text };
```

- `exec.concludeTurn()`（dsh-tools `ToolRunContext`，`lib/types/index.d.ts:299`）MUST 在返回成功结果前调用；同 step 内位于终局调用之后的其他工具调用组**不受影响**（dsh `executeToolCalls` 循环不因 concluded 中断，agent-loop `lib/index.js:130-143`）。
- 三工具（init/operate/remain）共用该映射，无逐工具分支（矩阵差异全部由 runtime 的 `ToolOutcome` 表达）。

## 3. 消费语义（dsh-agent-loop 拥有，本 feature 依赖并固化）

- 成功结果的 `concludesTurn: true` 经 `runGroup.commitReady` 在 durable `tool/result` 提交**之后**聚合为 `concluded`（agent-loop `lib/index.js:176-187`）→ `step()` 返回 `{kind:"completed"}`（`:685-686`）→ turn/end reason `{kind:"completed"}`（`:590-598`）。
- **无痕性**：`concludesTurn` 不落 durable 事件（`appendToolResult` 只持久化 content/isError/error/info/meta，`:302-318`）——session log 与自然停手不可区分（FR-003）。
- **收束语义精确为"该 step 之后不再发起下一次模型调用"**：同 step 内其他工具调用组正常执行并提交；next-step inbox 非空时的延展行为属 dsh 原生消费语义，与输入机制（如 061）的交互由该机制的需求文档定义——本契约的标记无条件性不受其影响。

## 4. 编排边界（零改动声明）

- 收束后的 player→planner 切换完全经既有 idle 锚点与 `nextStep()` 评估接管（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:763-808`）：FIFO 消化（`:772-779`，优先）→ pendingReview 重试 → phase 续驱 → gameEnded 评估（`:796-805`，`peekGameEvent() !== reviewedGameEvent` 引用比较去重）→ 结构性续驱。
- 收束与切换判定 MUST NOT 因排队输入做任何特判：不抑制标记、不快照/排队终局记录、不在后续 step 重标记（FR-004 条款）。
- 终局工具单元的 relay 完整性（call+result 配对 → `<player-tool-call>` 全文）由既有 team 机制保证（FR-005，`common/js/dsh-plugins/team/src/team.ts:315-360`；`broadcast.ts:121-155`）。

## 5. 兼容性

- `ToolOutcome` 消费方全量核查为 4 处（saolei 工具层、saolei-loop index 再导出、runtime 自身、两包测试）——可选字段向后兼容，playing 路径行为不变。
- 既有"无 saoleiGame 服务" fail-loud 边界（`saolei/src/index.ts:91-104`）零改动：planner 误调 saolei 工具仍为模型可见错误，不收束（FR-006）。
