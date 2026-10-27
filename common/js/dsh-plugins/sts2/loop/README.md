# @dominion/dsh-sts2-loop (`common/js/dsh-plugins/sts2/loop`)

sts2 场景的**最小编排**包（cordis 插件名 `sts2-loop`）：官方 agent-loop 直驱
单主 agent，作为主子形态的 host 侧验收载体与后续 sts2-loop 能力的落地板。公开面
与行为契约见
`specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md`。

## 包定位与边界

- 本包是 sts2 场景的编排落位（`common/js/dsh-plugins/sts2/loop/`），与
  `saolei/loop/` 同层级；目录与包名映射见
  `specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §1/§2。
- 编排实例由 host 经场景注册按会话构造（非组合单例），与 saolei-loop 的
  所有权模型一致（`specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md`
  §1）。
- 一个会话恰一个主 agent（角色 `main`，dsh 会话 id `${session}/main`）；无 team
  注册、无 announcer、无 memory 挂载、无 hooks、无游戏运行时、无 desktop flow、
  无子代理派发（同上契约 §3；`specs/080-host-main-sub-model/spec.md` FR-010）。
- 本包只结构化满足 host 的编排 seam，不 import host；状态错误以同构 `code`
  （`UNMATERIALIZED`/`ALREADY_MATERIALIZED`/`DISPOSED`）供 host 结构化映射
  （`specs/080-host-main-sub-model/contracts/scene-registry.md` §2）。
- 本包随 agent_v2 部署闭包交付：workspace 依赖 + `:runtime_pkg` 进
  `server_pkg.runtime_deps`，不进 npm registry 依赖通道；运行时 import 仅为
  `@deepseek-ai/dsh-llm` 的用户消息构造
  （`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §4）。

## 公开面（`src/index.ts`）

```ts
export class Sts2Orchestrator { /* 场景中立编排 seam 的单成员实现 */ }
export class Sts2StateError extends Error { /* code: UNMATERIALIZED | ALREADY_MATERIALIZED | DISPOSED */ }
export interface Sts2OrchestratorDeps {
  readonly compose: ComposePreset;          // preset 派生（host 注入 ctx.presetAuthoring）
  readonly agents?: AgentCreationSeam;      // 默认 ctx.agents
  readonly logger?: OrchestratorLogger;     // 失败上报
  readonly provider?: string;               // 默认部署 provider
}
export const name = "sts2-loop";            // 组合行（空 apply）
export const inject: string[];              // []
```

其余导出为类型（`Sts2MaterializeInput`/`Sts2Member`/`Sts2MemberConfig`/
`Sts2Snapshot`/`Sts2Failure`/`Sts2FailureDetail`/`Sts2StateErrorCode`/
`Sts2OrchestrationFailureContext` 与 seam 窄类型 `ComposePreset`/`ComposedPreset`/
`AgentCreationSeam`/`OrchestratorLogger`、驱动错误码 `MainDriveErrorCode`）。

`Sts2Orchestrator` 实现 `materialize`/`members`/`member`/`teamEvents`/`snapshot`/
`submit`/`cancel`/`whenQuiescent`/`dispose`：

- `materialize({session, template, members})`：members 恰
  `[{role: "main", preset, provider?, model?}]`；流程 `compose(preset)` →
  `ctx.agents.create`（绑定 preset 声明与 model 路由、在 creation setup 内执行
  `preset.setup`）→ 订阅成员事件信封；失败整体回滚并保持未物化可重试。
- `snapshot()`：`{materialized, active, activation: "main", paused, queued,
  failed, lastError}`——`queued` 为在途 steering 计数，`lastError.origin` 恒为
  `"member-turn"`，无 phase 字段。
- `submit(text)`：静止开回合、在途 steer 入队；`cancel()` 取消并冻结，下次投递
  重放持有输入；`dispose()` 幂等，在途回合收敛后释放 agent 与全部订阅。

## 模块构成（`src/`）

| 模块 | 职责 |
|---|---|
| `orchestrator.ts` | 物化/回滚、seam 方法、快照投影、释放 |
| `drive.ts` | 单 agent 驱动原语（`inject`+`followup` 装配、`steer` 队列、cancel 冻结重放、六码 `mapEngineCode`） |
| `events.ts` | main agent 会话/live 事件 → `TeamMemberEvent` 信封（`role: "main"`） |
| `index.ts` | 公开面与组合行（`sts2-loop`，空 `apply`） |

## 演进入口

sts2 场景的完整能力（房间子代理派发、主/子能力与 section 分配、房间类型
preset 池模板）由后续 sts2-loop feature 在本包内填充，调研结论见
`survey/sts2-agent-model.md` §5。
