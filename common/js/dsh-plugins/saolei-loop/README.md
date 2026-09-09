# @dominion/dsh-saolei-loop (`common/js/dsh-plugins/saolei-loop`)

扫雷 team loop dsh 插件（cordis 插件名 `saolei-loop`）：saolei team 的编排层
（驱动权唯一归属——游戏阶段机、成员驱动时机、物化编排）。agent 驱动回归官方
`dsh-agent-loop` 行（本插件不 `setFactory`、不含 turn/step 驱动状态机）；
GameRuntime 以 agent-scoped `saoleiGame` 服务注册，注册点在宿主
（`projects/game/agent_v2/src/session.ts`）经 `ctx.presetAuthoring.compose()`
返回的 agent 创建 setup hook 内（`createAgentGameRuntime` 为生产构造器）。
编排契约见 `specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2；
GameRuntime 行为契约见
`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2。

## 依赖 pin 决策

- dsh 家族 peer（dsh-agent）按精确版本 `0.1.1-rc.2` pin（无前缀）：dsh 家族
  按 0.1.1-rc.2 线锁定是仓库既定决策
  （`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`，A8）。
- `@deepseek-ai/cordis` peer `^4.0.1`（插件框架，与 dsh 版本线解耦）。
- `@dominion/game-saolei-board` 经 `workspace:*` 依赖：棋盘识别
  （`SaoleiBoard.init/updateFromScreenshot`）与坐标几何复用（FR-015）。
- `@dominion/dsh-desktop-bridge` peer（类型面）：GameRuntime 的 desktop 派发
  绑定消费其服务面类型。
