# Contract: 游戏基础——棋盘变动事件 seam

**Feature**: [../spec.md](../spec.md)（FR-001/FR-002；US4）| **决策依据**: [../research.md](../research.md) D5 | **数据模型**: [../data-model.md](../data-model.md) §1
**实现锚点**: `common/js/dsh-plugins/saolei/src/game/runtime.ts`（`GameRuntimeDeps`/`operate`/`createGameRuntime`）
**基线契约修订**: 本契约落地时同步修订 `specs/075-saolei-game-foundation/contracts/saolei-foundation.md`（服务面的事件 seam 增量）与 `specs/075-saolei-game-foundation/contracts/team-loop-assembly.md`（§2 游戏运行时构造的事件接线），修订为终态表述并引用本 feature spec。

## 1. 语义（领域事实推送）

"棋盘批量操作变动"是游戏领域的既有事实（与 gameLog 同源同信息）；本 seam 将其由拉改推：

- **触发锚点**：`operate` 内 `saolei_operate` 条目落账（gameLog push）之后、`operate` 返回之前，**同步**回调一次。
- **触发条件**（三者合取）：
  1. `deps.onBoardChange` 存在（seam 接入）；
  2. `executed > 0`（实际变动——全 skip 批量不触发）；
  3. `gameStatus(finalState) === "playing"`（终局批量不触发——**游戏开始与结束都不触发 hook**）。
- **载荷**：该次落账的 `GameLogEntry`（`tool: "saolei_operate"`、`operations`、`state: GameState`、`status: "playing"`）——与类级 gameLog 读取面尾条同源同值（`GameRuntime.peekGameLog()` 不属 `SaoleiGame` 契约面，仅测试/未来消费方读取）。
- **载荷只读**：订阅方 MUST 只读该条目（MUST NOT 改写 `entry.state` 等领域对象）——hooks 引擎以 JSON 快照把 `board` 传入沙箱脚本（[saolei-hooks-plugin.md](saolei-hooks-plugin.md) §4）。
- **粒度**：一次 operate 批量调用至多一条事件；`saolei_init` 条目与 `(game-end)` 追加条目不产生事件。

## 2. 机械契约（DI 形态）

```ts
export interface GameRuntimeDeps {
  // ...既有三字段不变...
  /** 棋盘变动事件 seam：可选回调；缺省 = 无订阅方，行为与现状完全一致。 */
  readonly onBoardChange?: (entry: GameLogEntry) => void;
}
```

- `createGameRuntime(sessionName, desktopBridge, deps?)`：构造签名扩展为可选第三参（承载 `onBoardChange` 等扩展 deps）；既有两参调用语义不变。
- 既有 `SaoleiGame` 契约面（init/operate/remain/peekGameEvent）与 `ToolOutcome` 语义**零变化**——seam 是纯增量（spec FR-002）。
- 回调时序上界：同步、落账后、返回前——订阅方可依赖"回调返回时 gameLog 已含该条目"。

## 3. 异常边界（订阅方责任）

- 游戏基础侧**不做** try/catch：运行时不吞噬、不转换回调异常（§2 DI 形态：裸同步回调），无边界订阅方抛错时异常沿 `operate` 调用路径上抛——回调前已落账的领域事实不受影响。
- 因此异常边界是**订阅方责任**：订阅方 MUST 自持边界，在回调内处理自身异常（吞噬 + warn 日志 + 可选播报，见 [../data-model.md](../data-model.md) §3.3），使 `operate` 的返回值与控制流不因回调抛错而改变。
- 设计意图：领域事实推送失败是订阅方问题，不构成领域结果的一部分（survey §2.1 边界划分：hook 侧失败不进领域结果）。

## 4. 订阅方解耦

- seam 的存在不引入对任何具体订阅方（hooks 插件、saolei-tools、编排）的依赖——游戏基础只知"可选回调"。
- hooks 引擎与 saolei-tools 零耦合（spec FR-016）：两者只共享本上游（类型 + seam）。

## 5. 验收面（对应 spec US4/SC-005）

`@dominion/dsh-saolei` 单测（`runtime.test.ts` 增量）：

1. 触发条件合取：`executed > 0` 且 `playing` → 回调恰一次，载荷与 gameLog 尾条逐字段同值。
2. 全 skip 批量（`executed = 0`，有落账）→ 零回调。
3. 终局批量（成功操作导致 `won`/`lost`）→ 零回调；`(game-end)` 条目不触发。
4. `saolei_init` → 零回调。
5. 结构拒绝/识别失败/派发失败/空批量的提前返回 → 零回调（无落账路径）。
6. seam 缺省（不传 `onBoardChange`）→ 既有用例全量回归不变（US4 场景 4）。
7. 回调异常边界（US4 场景 5）：无 `onBoardChange` 时（seam 缺省）与订阅方自持边界、吞噬自身异常时，`operate` 返回值与 gameLog 一致；无边界订阅方抛错时运行时不吞噬、不转换（§3：游戏基础侧不做 try/catch），异常沿 `operate` 调用路径上抛，回调前已落账的领域事实不变。生产接线的订阅方是 hooks 引擎事件处理器（`common/js/dsh-plugins/saolei-hooks/src/handler.ts` 的整循环 try/catch 自持边界，[../data-model.md](../data-model.md) §3.3），该上抛路径不可达。
