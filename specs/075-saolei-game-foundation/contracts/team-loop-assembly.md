# Contract: saolei-loop 编排消费与成员注册（游戏基础接入）

**Feature**: [spec.md](../spec.md) FR-010–FR-013 | **决策**: [research.md](../research.md) D2/D3/D4 | **数据模型**: [data-model.md](../data-model.md) §4/§6

saolei-loop 的终态契约变化：编排建立在游戏基础之上（正向依赖），成员 setup 的注册面增加 planner 的 saolei:game。074 组装契约（[team-assembly.md](../../074-dsh-grpc-runtime/contracts/team-assembly.md) §2/§4/§6）中 memory 相关条款全部继续有效；本契约只记录 saolei 侧的变化。

## 1. 正向依赖声明

```ts
// common/js/dsh-plugins/saolei-loop/src/index.ts
export const inject = ["saolei"];   // 游戏基础面（顶级 saolei 行提供）——组合装载期 fail-loud
```

- 顶级 `saolei` 行缺失 → saolei-loop 插件加载失败（cordis inject 解析），组合启动 fail-loud（不静默降级为无游戏运行时）。
- `config.plugins` 两能力面（`saoleiTools`/`memoryMount`）的 apply 期 fail-loud 检查**不变**（074 §4；能力面无 inject 声明，读取点检查）。
- 依赖声明：package.json 增 `"@dominion/dsh-saolei": "workspace:*"`（类型 + gameStatsText import）。

## 2. 编排对基础面的消费与游戏实例持有

- **游戏实例持有（M12，D10）**：TeamOrchestrator 私有字段 `private game: GameRuntime | null`——materialize 时经 `this.ctx.saolei.createGameRuntime(sessionName, desktopBridge)` 构造并保存（desktopBridge 运行期解析：`deps.desktopBridge ?? ctx.get("desktopBridge")`，缺失 fail-loud——保持现状物化期解析时序）；物化回滚与 dispose 丢弃引用（`this.game = null`；实例无外部资源，无显式清理路径）；refresh（宿主重建 TeamOrchestrator）→ 新实例（与现状行为等价——游戏局从不跨 refresh 存留）。
- **棋盘变动事件接线（hooks 能力面在场时）**：`createGameRuntime` 的**可选第三参**承载扩展 deps。编排在物化**早期（`createMember("player")` 之前——player setup 需要 `onBoardChange`）**经 `saoleiHooks` 面构建 hooks 能力单元（`mount.buildHooksUnit({ announce })`；`announce` 为惰性闭包，调用时读编排持有的 announcer），player setup 以 `createGameRuntime(sessionName, desktopBridge, { onBoardChange: unit.onBoardChange })` 构造运行时（同一实例，句柄注册与终局读取语义不变）；hooks 行缺席（能力面 undefined）→ 不传第三参，行为与现状完全一致。接线与异常边界见 `specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md` §5、`specs/078-saolei-js-hooks/contracts/board-change-event.md` §2/§3（需求来源 `specs/078-saolei-js-hooks/spec.md` FR-017）。
- **句柄注册（M13）**：player setup 将编排持有的实例注册进 player agent ctx——`agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game)`（memory face 同型，074 registerMemory 模式）；工具 execute 的 `exec.agent.ctx.saoleiGame` 属性解析与 fail-loud 文案**零变化**。
- **终局读取（M14）**：`nextStep` 的 gameEnded 判定直读 `this.game?.peekGameEvent()`（同一实例、同一读取时点——player turn settle 后）；`MemberRuntime.game` 字段与 `MountPlayerRuntime` seam（orchestrator deps + 默认实现 + session.ts 透传）删除——编排测试的运行时注入改经基础面 stub（`ctx.provide("saolei", { createGameRuntime: fake })`）。
- **类型**：`GameRuntime`/`GameEventRecord` 等自 `@dominion/dsh-saolei` 导入（saolei-loop 已编译依赖 dsh-saolei——gameStatsText import——直接 import 即可，无需结构重复声明）。
- **播报路径（orchestrator `announceStats()`）**：`gameStatsText` import（orchestrator.ts 唯一 import 点）自 `"./game/text.js"` 改 `@dominion/dsh-saolei`；announcer 仅提供 `announce(text)`、播报文本由编排渲染后传入（播报协议零变化，specs/065 contracts/game-stats-broadcast.md）。

## 3. 成员 setup 注册序（终态）

```text
player:  composed.setup(persona)
       → registerSaoleiTools：saoleiTools 单元（tools×3 → sections×1[guidance]）     # 074 既有，单元内容收窄
       → agentCtx.systemPrompt.section(saoleiFoundation.gameSection)                # saolei:game（源自基础面）
       → 游戏句柄注册：agentCtx.isolate("saoleiGame").provide("saoleiGame", this.game)
         （this.game = materialize 期经 foundation.createGameRuntime 构造的编排持有实例）

planner: composed.setup(persona)
       → agentCtx.systemPrompt.section(saoleiFoundation.gameSection)                # saolei:game（新增——074 遗留修复）
       → registerMemory：faces → tools → sections → await ready()                   # 074 FR-016 原序不变
```

- 基础面读取：`this.ctx.saolei`（inject 声明保证存在；防御性 undefined 检查仅作类型收窄）。
- 注册序对最终 prompt 无影响（section 按 order 排序，specs/060 §4）；固定序为测试断言确定性服务。
- **planner 生效面（终态）**：persona + team:roster + **saolei:game** + memory:guidance + wiki:guidance + memory:snapshot——四工具面不变；MUST NOT 注册 saolei 工具、saolei:guidance 与游戏句柄。
- **player 生效面（终态）**：persona + team:roster + saolei:game + saolei:guidance + 三工具 + 游戏句柄（解析到编排持有的同一实例）——与改造前逐字等价（SC-006/FR-012）。

## 4. saolei-loop 的移除面

- `src/game/` 目录整体消失（M1/M2）；index.ts 移除全部 game 再导出与 `MountPlayerRuntime` 类型导出（单一出口 = dsh-saolei，M8）。
- `MemberRuntime.game` 字段与 `MountPlayerRuntime` seam（类型、deps、默认实现）删除；宿主 session.ts 的 deps 透传清理（M14）。
- 编排状态机行为零变化（specs/059 contracts/dsh-plugins.md §2 权威）：终局事件读取（`peekGameEvent`——读取对象从成员句柄改为自身持有实例，时序不变）、round-stop、queue-first digestion、播报时序、fail/回滚语义。

## 5. 行为差异表（相对 074 终态，契约化）

| # | 差异 | 074 终态 | 本特性终态 |
|---|---|---|---|
| ① | planner system prompt | 无 saolei:game | 含 saolei:game（order 50，文本 = 060 基线）——唯一预期行为变化（spec M11） |
| ② | saoleiTools 单元 sections | [guidance, game] | [guidance]（game 拆归基础面，编排对双角色注册） |
| ③ | 游戏运行时实现的包归属 | saolei-loop `src/game/` | dsh-saolei（编排经基础面消费；`GameRuntimeService` Service 形态移除、工厂改纯领域构造 `createGameRuntime`） |
| ④ | 游戏实例所有权 | player agent scope 的 cordis 服务（编排经 `members.player.game` 读终局） | **TeamOrchestrator 字段持有**（player agent ctx 仅句柄；编排直读自身字段）——工具解析/终局时序/回滚/dispose 行为等价（D10 论证） |
| ⑤ | saolei-loop inject | `[]` | `["saolei"]`（装载期 fail-loud） |

## 6. 不变量（回滚/fail-loud 语义延续）

- 物化回滚：memory 预取 `ready()` reject → setup throw → 整体回滚（074 team-assembly §6，不变）；游戏实例构造失败（desktopBridge 缺失等）→ setup/materialize throw → 回滚且编排丢弃实例引用；saolei:game 注册为静态注册（无失败路径），不改变回滚边界。
- `CreateAgentOptions` setup throw 即回滚的框架行为不变。
- 编排 dispose：成员 dispose + 游戏实例引用丢弃（`this.game = null`）+ 注册清理——无残留（现状靠成员 scope 注销的等价转移，D10）。
- 组合缺顶级 saolei 行 / config.plugins 漏 saolei-tools 行 / 漏 memory 行：三类缺失均组合启动 fail-loud（inject 解析失败 / apply 面检查失败）。

## 7. 测试义务（vitest 随交付）

1. `index.test.ts`：inject 语义（面缺失加载失败）；apply 双能力面 fail-loud（既有用例保留）。
2. `orchestrator.test.ts`：harness 增 `ctx.provide("saolei", { createGameRuntime: fake 工厂, gameSection: spec })`；
   - materialize 断言：编排构造并持有游戏实例（fake 工厂被调用、实例在编排字段——终局读取不经成员）；
   - player 分支：saoleiTools 单元注册（三工具 + 仅 guidance section）、`gameSection` 注册、**句柄注册（isolate 注册的实例 === 编排持有实例）**、注册序断言（tools → sections[单元] → gameSection → 句柄注册）；
   - planner 分支：`gameSection` 注册 + memory 单元原序、**无工具无 guidance 无句柄断言**；
   - 回滚/清理断言：materialize 失败 → 编排游戏实例引用丢弃；dispose → 同；
   - `MountPlayerRuntime` seam 用例删除（session.test 的 mountPlayerRuntime stub 断言同步移除）。
