# @dominion/dsh-saolei-loop (`common/js/dsh-plugins/saolei/loop`)

扫雷 team loop dsh 插件（cordis 插件名 `saolei-loop`）：saolei team 的编排层
（驱动权唯一归属：flow 层流程决策 + 执行层驱动时机与物化编排 + 成员适配器）。
agent 驱动回归官方 `dsh-agent-loop` 行（本插件不 `setFactory`、不含 turn/step 驱动
状态机）。游戏
领域是其**正向依赖**（`inject: ["saolei"]`）：编排在物化期经基础面工厂
`createGameRuntime` 构造并**持有**游戏实例，player 成员 ctx 上只注册 agent-scoped
句柄 `saoleiGame`（工具经 `exec.agent.ctx` 解析到同一实例）；游戏运行时实现
（`src/game/`）不驻留本包，归游戏基础 `@dominion/dsh-saolei`，`gameStatsText`
亦自该包导入用于播报。编排契约见
`specs/076-agent-v2-optimize/contracts/flow-orchestration.md`（分层与指令 / 成员接口 /
投递 / outcome 与冻结 / 操作路径；结构定义见
`specs/076-agent-v2-optimize/data-model.md` §2/§3），编排行为基线见
`specs/059-agent-v2-team-mode/contracts/dsh-plugins.md` §2（驱动时机：至多一个成员
在途驱动、不合成驱动消息）；游戏基础接入契约见
`specs/075-saolei-game-foundation/contracts/team-loop-assembly.md`；游戏运行时
行为契约见 `specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md` §2.2。

## 编排服务（TeamOrchestrator）

插件导出 per-team 编排 `TeamOrchestrator`（`src/orchestrator.ts`，契约
`specs/076-agent-v2-optimize/contracts/flow-orchestration.md` §1/§6/§7；结构见
`specs/076-agent-v2-optimize/data-model.md` §2/§3）：编排 = flow 层（`src/flow.ts`
——F-Moore 活动图、`start(activity)` 唯一指令、outcome 四态、冻结恢复链）+ 执行层
（本类——激活权落地、序列化泵、取消与释放）+ 成员适配器（`src/member.ts` 的
`drive`/`deliver` 接口，`src/agent-member.ts` 与 `src/announcer.ts` 两个实现）。
宿主为每个 team 构造一个实例（`deps.compose` 接 `ctx.presetAuthoring.compose`；
`ctx.agents`/`ctx.team`/`ctx.desktopBridge` 默认从组合上下文解析；测试经
`TeamOrchestratorDeps` 注入 double，无模块拦截）。公开面：

- `materialize(options)`：逐成员 compose + `ctx.agents.create`（roster mount、
  player 侧 saoleiTools 静态单元（三工具 + `saolei:guidance`）注册 + `saolei:game`
  玩法 section 注册 + 编排持有的游戏实例构造与句柄注册，planner 侧
  `saolei:game` 注册 + `memoryMount.build(scope)` 单元注册与 fail-loud 预取），
  构造 announce-only 的 announcer 成员，`ctx.team.register` 三成员名册
  （player/planner/announcer），随后静止在初始 `plan` 活动（激活权 = planner，
  不发起任何驱动）；任一步失败回滚全部已建成员（含丢弃持有的游戏实例引用），
  无半物化。
- `submit(text)`：用户消息入口——经 `flow.activeMember()` 只读接触点投递到当前
  激活成员的域（`member.deliver`，不经 flow 决策）：成员回合在途时 steer 入 inbox
  （`SubmitResult` 返回 `queued`/`position`），空闲时立即装配驱动开 turn（物化后的
  首条 Send 即首驱，由 planner 处理）；冻结期间投递的静止装配驱动即恢复动作。
- `cancel()`：终止在途回合（`agent.cancel({kind:"user"})`）+ 流程位置冻结
  （facts 不消费）+ inbox 未消费收编 landed（返回 `dropped`，落地为历史但不触发
  驱动）；已冻结时 no-op；幂等；不产生失败呈现。
- `dispose()`：终止在途回合（aborted，不转交）、丢弃 landed 集、释放 team 注册与
  全部成员（allSettled 释放、首个失败上报）、流程状态随引用清除（refresh 不保留
  流程事实）；宿主刷新/销毁路径，幂等。
- `member(role)` / `announcer` / `snapshot()` / `whenQuiescent()`：agent 成员
  （player/planner）句柄读取面（system prompt 查看等）、announcer 读取面（宿主订阅
  其播报日志）、状态读取面与静止等待面；`snapshot()` 含 `failed`/`lastError`
  （`{message, member, phase, code, origin, detail}`）失败面——成员回合失败与驱动
  装配异常都进冻结通道，并经注入的 `logger`
  （`TeamOrchestratorDeps.logger`，缺省 console fallback）上报，宿主据此映射
  INTERNAL/turn error；下一次未失败的驱动清除失败态。

编排层不合成任何驱动消息：flow 发起的驱动输入 = 成员未消费的团队广播
（`ctx.team.drain`，拉动式装配）+ landed flush（`specs/059-agent-v2-team-mode/spec.md`
FR-010 保持，见 `specs/076-agent-v2-optimize/spec.md` FR-105）；用户消息自成推进式
路径（`deliver`：在途 steer / 静止装配 `[drain, landed, message]` 开 turn），不进入
flow 发起的驱动输入构造。成员适配器以 `agent.whenIdle()` 判定完成（whole-agent
quiescence：turn 已结束且 inbox pending 为空，含经 deliver 投递未消费消息）；
`drive()` 返回 `settled`/`resting`/`failed`/`aborted` 四态——`settled` 触发流程事实
写入与边判定，`resting`（无可执行内容）静止在当前活动，`failed`/`aborted` 冻结
（`failed` 记录失败事实，`aborted` 不写），冻结期间唯一驱动源是 `deliver` 的静止
装配驱动（恢复动作），随后 flow 重启当前活动并重放适配器持有的输入集；团队广播
批次以 `inject`（除末条）+ `followup`（末条）在同一次 pre-step claim 注入单 turn。

## 依赖 pin 决策

- dsh 家族 peer（dsh-agent）经 catalog 解析到精确版本 `0.2.0-rc.2`：dsh 家族
  按 0.2.0-rc.2 线锁定是仓库既定决策（`third_party/dsh/core` 同线），依据
  `specs/073-dsh-0.2-upgrade/research.md` D1。
- `@deepseek-ai/cordis` peer 经 catalog 解析到 `~4.0.4`（线内互锁：解析必须
  落线内值，否则可能物化两份 cordis；依据 `specs/073-dsh-0.2-upgrade/research.md` D1）。
- `@dominion/dsh-saolei` 经 `workspace:*` 依赖：游戏基础面消费
  （`inject: ["saolei"]`）与游戏类型（`GameRuntime`/`GameEventRecord`/
  `SaoleiFoundation`）、`gameStatsText` 导入（`specs/075-saolei-game-foundation/
  contracts/team-loop-assembly.md` §1/§2）。
- `@deepseek-ai/dsh-llm` peer：编排层构造用户消息（`createUserMessage`）。
- `@dominion/dsh-desktop-bridge` peer（类型面）：编排在物化期解析 desktopBridge，
  经基础面工厂构造游戏实例。
- `@dominion/dsh-team` peer：`ctx.team` 注册/消费面的类型引用与
  `agentMemberSource` 成员消息源适配器的值引用（物化时包装成员句柄）；运行时
  服务经组合上下文注入。
