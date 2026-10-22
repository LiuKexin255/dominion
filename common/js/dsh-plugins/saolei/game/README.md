# @dominion/dsh-saolei (`common/js/dsh-plugins/saolei/game`)

扫雷**游戏基础** cordis 插件（cordis 插件名 `saolei`，`inject = []`）：组合清单的
**顶级基础行**（`- { id: saolei, name: '@dominion/dsh-saolei' }`，无 config——游戏
基础无部署参数）。插件持有 saolei 游戏领域的全部运行时与玩法内容，`apply` 内
`ctx.provide("saolei", { createGameRuntime, gameSection })` 暴露基础服务面
`SaoleiFoundation`：

- `createGameRuntime(sessionName, desktopBridge)`：**纯领域游戏运行时工厂**——返回
  普通实现类 `GameRuntime` 实例（构造器仅收 `GameRuntimeDeps`，无 agent 参数、无
  cordis scope 注册副作用）。实例所有权在调用方：saolei-loop 编排在物化期构造并
  持有，player 成员 ctx 上只注册 agent-scoped 句柄 `saoleiGame`（工具经
  `exec.agent.ctx.saoleiGame` 解析，解析路径与错误文案零变化）——
  `specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §2、
  `specs/075-saolei-game-foundation/contracts/team-loop-assembly.md` §2。
- `gameSection`：`{ name: "saolei:game", order: 50, text: SAOLEI_GAME_RULES }` 玩法
  section（经典扫雷规则 + 与三工具对齐的可用操作），由 saolei-loop 编排对 player
  与 planner 两个角色注册（`specs/075-saolei-game-foundation/contracts/
  team-loop-assembly.md` §3；内容边界与 persona 去重基线见
  `specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §1）。

面内容为静态值（无状态、无生命周期动作）；本插件不在组合作用域全局注册工具或
section。模型面向的 saolei 工具与 `saolei:guidance` 工具守则归工具插件
`@dominion/dsh-saolei-tools`。包契约见
`specs/075-saolei-game-foundation/contracts/saolei-foundation.md`。

## 包导出面（游戏类型单一出口）

全仓游戏类型/运行时 API 只从本包导出（saolei-loop 不承担 game 类型再导出职责，
`specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §3）：

- **值**：`createGameRuntime`、`gameStatsText`、`SAOLEI_GAME_RULES`（兼
  `SAOLEI_GAME_SECTION`）。
- **类型**：`SaoleiGame`、`GameRuntime`（接口与实现类）、`GameRuntimeDeps`、
  `GameEventRecord`、`GameLogEntry`、`GameStats`、`OperationType`、`OperateInput`、
  `CellOperation`、`ToolOutcome`、`SaoleiFoundation`。
- `src/game/`：运行时体系（`runtime.ts`/`board.ts`/`geometry.ts`/`text.ts`）。

`saolei_remain` 的三处语义表述（工具 description、`saolei:game` 玩法行、结果体
legend 行）以 `specs/064-memory-split-fold-remain/contracts/saolei-plugins.md` 为准：
主语义 = 每个数字格周围剩余未标记雷数（数字 − 相邻已标旗数，可为 0/负），显式排除
旗子数量读法；结果体 legend 行位于 `board size` 行与网格之间，并锚定列 = x、行 = y
（与 `saolei_operate` 参数一致）。`saolei:guidance` 的 remain 条目同语义改写；按 060
所有权边界（`specs/060-agent-v2-team-optimize/contracts/prompt-sections.md` §2）该
条目不含 `over-flagged` 玩法解释，保留 `may be 0 or NEGATIVE` 值域表述。

## 依赖 pin 决策

- dsh 家族 peer（dsh-system-prompt）经 catalog 解析到精确版本 `0.2.0-rc.2`：dsh 家族
  按 0.2.0-rc.2 线锁定是仓库既定决策（`third_party/dsh/core` 同线），依据
  `specs/073-dsh-0.2-upgrade/research.md` D1。
- `@deepseek-ai/cordis` peer 经 catalog 解析到 `~4.0.4`（线内互锁：解析必须落线内值，
  否则可能物化两份 cordis；依据 `specs/073-dsh-0.2-upgrade/research.md` D1）。
- `@dominion/game-saolei-board` 经 `workspace:*` 依赖：棋盘识别与坐标几何
  （`src/game/board.ts`/`runtime.ts`）。
- `@dominion/dsh-desktop-bridge` peer（类型面）：`src/game/runtime.ts` 的 desktop 派发
  绑定消费其服务面类型。
- 游戏基础不依赖任何上层：不声明 `@dominion/dsh-saolei-loop` /
  `@dominion/dsh-saolei-tools` / `@dominion/dsh-saolei-memory`
  （`specs/075-saolei-game-foundation/contracts/saolei-foundation.md` §4/§5）；本插件
  不持有工具定义，亦不声明 `@deepseek-ai/dsh-tools`。
