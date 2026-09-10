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

## 编排服务（TeamOrchestrator）

插件导出 per-team 编排状态机 `TeamOrchestrator`（`src/orchestrator.ts`，契约 §2；
状态图 `specs/059-agent-v2-team-mode/data-model.md` §5）：宿主为每个 team 构造一个
实例（`deps.compose` 接 `ctx.presetAuthoring.compose`；`ctx.agents`/`ctx.team`/
`ctx.desktopBridge` 默认从组合上下文解析；测试经 `TeamOrchestratorDeps` 注入
double，无模块拦截）。公开面：

- `materialize(options)`：逐成员 compose + `ctx.agents.create`（roster mount、
  player 侧 `createAgentGameRuntime` 注册、planner 侧 memory load DI seam），
  `ctx.team.register`，随后静止等待（初始激活 = planner，不发起任何驱动）；任一
  步失败回滚全部已建成员，无半物化。
- `submit(text)`：用户消息入口——当前成员回合中入 team FIFO（返回
  `queued`/`position`），空闲时立即由当前激活成员处理（物化后的首条 Send 即首驱，
  由 planner 处理）；取消暂停后再次 Send 恢复。
- `cancel()`：终止在途回合 + 暂停自动续驱 + 作废排队（返回未投递消息，落地为
  历史但不触发驱动）；幂等。
- `dispose()`：终止在途回合、作废排队、释放 team 注册与全部成员（宿主刷新/
  销毁路径，幂等）。
- `member(role)` / `snapshot()` / `whenQuiescent()`：成员句柄读取面（system prompt
  查看等）与状态读取面；`snapshot()` 含 `failed`/`lastError`
  （`{message, member, phase}`）失败面——泵步失败（drain 读取、交替不变量、
  followup/inject 异常）会暂停续驱并经注入的 `logger`
  （`TeamOrchestratorDeps.logger`，缺省 console fallback）上报，宿主据此映射
  INTERNAL/turn error；下一次成功驱动清除失败态。

编排层不合成任何驱动消息（FR-010）：每次驱动输入 = 成员未消费的团队广播
（`ctx.team.drain`）+ 排队用户消息；成员回合结束评估无排队消息、无待复盘终局、
drain 为空时静止在当前激活成员等待新输入。驱动经成员 `agent/status` idle 收敛判定
回合结束（含工具调用引发的连续 turn）；drain 的广播批次以 `inject`（除末条）+
`followup`（末条）在同一次 pre-step claim 注入单 turn。

## 依赖 pin 决策

- dsh 家族 peer（dsh-agent）按精确版本 `0.1.1-rc.2` pin（无前缀）：dsh 家族
  按 0.1.1-rc.2 线锁定是仓库既定决策
  （`specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md`，A8）。
- `@deepseek-ai/cordis` peer `^4.0.1`（插件框架，与 dsh 版本线解耦）。
- `@dominion/game-saolei-board` 经 `workspace:*` 依赖：棋盘识别
  （`SaoleiBoard.init/updateFromScreenshot`）与坐标几何复用（FR-015）。
- `@deepseek-ai/dsh-llm` peer：编排层构造用户消息（`createUserMessage`）。
- `@dominion/dsh-desktop-bridge` peer（类型面）：GameRuntime 的 desktop 派发
  绑定消费其服务面类型。
- `@dominion/dsh-team` peer（类型面）：`ctx.team` 注册/消费面的类型引用；运行时
  服务经组合上下文注入。
