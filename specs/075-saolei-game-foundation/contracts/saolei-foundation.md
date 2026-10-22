# Contract: saolei 游戏基础插件（@dominion/dsh-saolei）

**Feature**: [spec.md](../spec.md) FR-001–FR-005 | **决策**: [research.md](../research.md) D1/D4/D5 | **数据模型**: [data-model.md](../data-model.md) §1–§2

dsh-saolei 的终态契约：saolei 游戏领域功能的唯一持有者与基础服务提供方（组合清单顶级基础行）。包契约形态对齐 048 [esm-package-conventions.md](../../048-js-esm-migration/contracts/esm-package-conventions.md)。

## 1. 插件形态

```ts
// common/js/dsh-plugins/saolei/src/index.ts
export const name = "saolei";
export const inject: string[] = [];            // 纯提供方（desktopBridge 经工厂参数，运行期注入）
export const SAOLEI_FOUNDATION_SERVICE = "saolei";
```

- 组合挂载：**顶级行** `- { id: saolei, name: '@dominion/dsh-saolei' }`，无 config（游戏基础无部署参数；mTLS/desktop-bridge 等部署关切由消费方组合持有）。
- MUST NOT 以 saolei-loop `config.plugins` 子插件形态挂载（spec FR-001）；MUST NOT 依赖 saolei-loop / dsh-saolei-tools / dsh-saolei-memory（spec FR-005）。

## 2. 基础服务面

```ts
export interface SaoleiFoundation {
  readonly createGameRuntime: typeof createGameRuntime;
  readonly gameSection: PromptSection;
}

export function apply(ctx: Context): void {
  ctx.provide(SAOLEI_FOUNDATION_SERVICE, { createGameRuntime, gameSection: SAOLEI_GAME_SECTION });
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    /** 组合级游戏基础面（顶级 saolei 行提供；agent 派生 scope 均可解析）。 */
    saolei?: SaoleiFoundation;
  }
}
```

- **`createGameRuntime(sessionName, desktopBridge, deps?): GameRuntime`**：**纯领域游戏工厂**——构造普通类实例（`GameRuntime`，构造器仅收 `GameRuntimeDeps`；无 agent 参数、无 cordis scope 注册副作用；`GameRuntimeService` 的 Service 继承已移除——实例的所有权与清理责任在调用方编排，[team-loop-assembly.md](team-loop-assembly.md) §2）。可选第三参承载扩展 deps（既有两参调用语义不变）。desktop-bridge 预绑定 dispatch 的组装语义与现状等价（`dispatch: (part, signal) => desktopBridge.dispatch(sessionName, part, signal)`；`sessionName` 为 **GAME 会话资源名** `templates/{template}/sessions/{session}`——桥接键，非成员 namespaced session id，specs/059 data-model §3）。游戏运行时行为契约零变化（specs/051 data-model §2.5、specs/062 turn-conclude、specs/064 remain 语义）。
- **棋盘变动事件 seam（`GameRuntimeDeps.onBoardChange?`）**：可选回调，载荷为落账的 `GameLogEntry`；`operate` 内 `saolei_operate` 条目 push 后、返回前同步回调一次，触发条件为三者合取——`onBoardChange` 存在 ∧ `executed > 0`（全 skip 批量不触发）∧ `gameStatus(finalState) === "playing"`（终局批量不触发；游戏开始与结束都不触发）。`saolei_init` 条目、`(game-end)` 追加条目与无落账路径（结构拒绝/识别失败/派发失败/空批量）均不触发。缺省（未接入订阅方）时行为与现状完全一致——seam 无副作用。订阅方异常边界：回调抛错**不得**改变 `operate` 返回值或控制流——游戏基础侧不做 try/catch，异常由订阅方（hooks 引擎事件处理器）内部处理。语义、机械契约与验收面见 `specs/078-saolei-js-hooks/contracts/board-change-event.md`，需求来源 `specs/078-saolei-js-hooks/spec.md` FR-001/FR-002。
- **`gameSection`**：`{ name: "saolei:game", order: 50, text: SAOLEI_GAME_RULES }`——冻结契约值（名/order/文本与 specs/060 contracts/prompt-sections.md §1 基线逐字一致；内容边界：玩法 + 可用操作，MUST NOT 混入工具调用形态）。编排对 player 与 planner 注册（[team-loop-assembly.md](team-loop-assembly.md) §3）。
- 面内容为静态值（无状态、无生命周期动作）；消费方缺失面 = 组合装载 fail-loud（`inject: ["saolei"]` 声明方——saolei-loop——加载失败）。

## 3. 包导出面（库边界）

- **类型**：`SaoleiGame`、`GameRuntime`（接口与实现类）、`GameRuntimeDeps`、`GameEventRecord`、`GameLogEntry`、`GameStats`、`OperationType`、`OperateInput`、`CellOperation`、`ToolOutcome`、`SaoleiFoundation`。
- **值**：`createGameRuntime`、`gameStatsText`、`SAOLEI_GAME_RULES`。
- 游戏类型的**单一出口**：全仓游戏类型/运行时 API 只从本包导出（saolei-loop 不再再导出，spec M8）。
- 行为契约权威不变：游戏运行时状态机与结果文本 = specs/051 data-model §2.5（含 064 修订）、`concludesTurn` = specs/062 contracts/saolei-turn-conclude.md §1/§2、`gameStatsText` 模板 = specs/065 data-model §3。

## 4. 依赖声明

```
dependencies:  { "@dominion/game-saolei-board": "workspace:*" }
peerDependencies: { "@deepseek-ai/cordis",
                    "@deepseek-ai/dsh-system-prompt", "@dominion/dsh-desktop-bridge" }
```

（dsh-system-prompt/desktop-bridge 为 game/runtime.ts 迁移随带的类型/运行时 peer；`@deepseek-ai/dsh-agent` 迁移后无 `Agent` 引用——`createGameRuntime` 不再收 agent 参数——MUST NOT 声明。）

## 5. 禁止事项

- MUST NOT 持有智能体工具、saolei:guidance 或任何模型面向的能力单元（归 dsh-saolei-tools）。
- MUST NOT 在 apply 中全局注册工具或 section（`ctx.systemPrompt.section` 不出现在本插件；玩法经编排注册）。
- MUST NOT 持有游戏实例或 per-team/per-agent 状态（工厂只构造，实例所有权在调用方——多 team 并发下组合级实例会冲突）；MUST NOT 出现场景词汇驱动的行为分支（player/planner 角色知识不在基础——注册与分配是编排职责）。

## 6. 测试义务（vitest 随交付）

1. `game/runtime.test.ts`（1,097 行）与 `text.test.ts` 文件级随迁等价全绿（比对基准 = 迁移源用例；构造形态适配点 `new GameRuntime(deps)` 同步，行为断言零改动）。
2. `index.test.ts` 重写：面内容断言（`ctx.provide("saolei")` 面存在、`createGameRuntime` 为工厂、`gameSection` 名/order 50/text 逐字等于 `SAOLEI_GAME_RULES`）；**无工具断言**（面/包导出无 ToolDefinition 值）；**工厂纯度断言**（构造仅收 deps、无 ctx/scope 参数——静态签名断言）；无全局注册副作用（stub ctx 断言 `tools.register`/`systemPrompt.section` 未被调用）。
