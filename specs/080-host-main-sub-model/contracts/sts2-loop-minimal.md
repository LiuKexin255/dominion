# Contract: `@dominion/dsh-sts2-loop` 最小编排包

> Feature: [spec.md](../spec.md)（FR-009；FR-010 负向边界）| 数据模型：[data-model.md](../data-model.md) §4 | 决策：[research.md](../research.md) D5
> 本契约定义新建包 `common/js/dsh-plugins/sts2/loop/`（`@dominion/dsh-sts2-loop`）的定位、公开面与行为：官方 agent-loop 直驱单主 agent 的最小编排——主子形态的 host 侧验收载体与后续 sts2-loop feature（survey §5 S3/S4/S6）的落地板。

## §1 包定位与组合行

- 布局：`common/js/dsh-plugins/sts2/loop/`（对齐 `saolei/loop/` 分组先例，[specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md](../../079-sts2-template-scaffold/contracts/dsh-plugins-layout.md)）。
- 组合行：`projects/game/agent_v2/cordis.yml` 新增 `{ id: sts2-loop, name: '@dominion/dsh-sts2-loop' }`——**空 config 惰性行**：`apply` 空实现（不 provide 面、不注册工具/section）、`inject: []`；对齐 `sts2` 骨架行模式。后续 S3/S4 的房间能力依赖声明挂本行 `config.plugins` 生长（与 saolei-loop 同型演化路径）。`dsh.test.ts` 逐行断言同步。
- 编排实例由 host 经场景注册构造（每会话一个实例，非组合单例——与 saolei-loop 相同的所有权模型）。

## §2 公开面（`src/index.ts`）

```ts
export class Sts2Orchestrator implements SceneOrchestration { /* §3-§5 */ }
export interface Sts2OrchestratorDeps {
  readonly compose: ComposePreset;          // preset 派生（host 注入 ctx.presetAuthoring）
  readonly agents?: AgentCreationSeam;      // 默认 ctx.agents
  readonly logger?: OrchestratorLogger;     // 失败上报
  readonly provider?: string;               // 默认部署 provider
}
export const name = "sts2-loop";            // 插件行（空 apply）
```

seam 接口 = host 的 `SceneOrchestration`（[scene-registry.md](scene-registry.md) §2；包不 import host，TS 结构化满足 + 本契约锚定）。包内声明自己的结构化窄接口（`ComposePreset`/`AgentCreationSeam`/`OrchestratorLogger` 形状与 saolei-loop 同名 seam 一致）与同 `code` 的状态错误——跨场景插件包不互相依赖（`specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md` §2 不变量）。

## §3 物化与释放

- `materialize({session, template, members})`：members 恰 `[{role:"main", preset, provider?, model?}]`（host 已完成全部校验；编排侧对形状异常 fail-loud 抛状态/参数错误）。流程：`compose(preset)` → `ctx.agents.create`（sessionId = `${session}/main`，绑定 preset 声明与 model 路由）→ `preset.setup(agentCtx)` → 订阅事件（§5）。失败整体回滚（释放已创建 agent），保持未物化可重试。
- `members()` = `[{role: "main", kind: "agent"}]`（物化后）；`member("main")` = agent 句柄。
- `dispose()`：幂等；在途回合收敛（取消）后释放 agent 与全部订阅。
- **不做**（FR-010）：team 注册（`ctx.team`）、announcer、memory 挂载、hooks、游戏运行时、desktop flow、preset 池模板内容职责（池模板在 cordis.yml 快照）。

## §4 驱动（`src/drive.ts`）

原语 = dsh-agent 0.2.0-rc.2 基 API（`inject`/`followup`/`steer`/`whenIdle`；先例 `common/js/dsh-plugins/saolei/loop/src/agent-member.ts` 的单 agent 极简化）：

| 操作 | 行为 |
|---|---|
| 静止时 `submit(text)` | 装配输入（仅用户消息，无广播 drain）→ `inject`（前缀）/ 末条 `followup` → 等待 `whenIdle` 收束 → `turn_end{COMPLETED}`，回到静止 |
| 在途时 `submit(text)` | `steer(text)` 入 inbox（pending +1）；当前 turn 结束时重放持有输入集（驱动恢复链） |
| `cancel()` | 取消在途回合（`turn_end{CANCELED}`）+ 冻结（不消费持有输入）；投递即恢复 |
| 失败 | 引擎错误按六码分类（映射对齐 saolei `mapEngineCode`：`TRANSIENT`/`QUOTA`/`CREDENTIAL`/`CONFIG`/`INPUT_TOO_LARGE`/`INTERNAL`）——成员回合失败经 turn_end ERROR 干净收束；无自动重试/续驱（最小编排无自愈泵，`failed` 置位、冻结等投递） |

无 flow 状态机：单 agent 无成员切换、无相位（snapshot 无 `phase`）、无自动续驱——turn 收束即静止，驱动只由用户消息触发。

## §5 事件信封（`src/events.ts`）

订阅 main agent 的 session 事件（`session/event`）与 live 面（`agent/assistant-stream`/`agent/status`/`agent/error`），装入 `TeamMemberEvent` 信封（**类型导入** `@dominion/dsh-team`，无运行时依赖）：`{role: "main", memberId: <agent session id>, channel: "log"|"live", event}`。`teamEvents()` 透传该流（未物化/已释放抛状态错误）。host 的 `MemberCollector` 按信封消费（与 saolei 成员同一路径）。

## §6 快照

`snapshot() = {materialized, active: "main"|null, activation: "main", paused, queued, failed, lastError}`——`queued` = steered-in-flight 计数；`paused` = 冻结（cancel/失败后）；`lastError.origin` = `"member-turn"`（单 agent 无编排层失败源，engine 失败经成员通道）。host 的 quiescence watcher 与 TeamView 投影直接消费（[face-main-sub.md](face-main-sub.md) §3）。

## §7 测试义务

- 包内单测（`bazel test`）：物化/回滚、静止-在途-收束、steer 排队与重放、cancel 冻结与恢复、六码映射、事件信封形状（seam doubles 注入，`style/javascript.md` Mock 惯例）。
- 端到端验收归 `game-sts2` suite（[testplan-game-sts2.md](testplan-game-sts2.md)）——宪法 VI。
