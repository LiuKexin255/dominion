# Contract: saolei 插件三件 + dsh 组合清单（agent-v2 游戏能力）

**Feature**: [spec.md](spec.md) FR-010/011/012/013/014/015 | **决策**: [research.md](../research.md) D3/D5/D6/D7/D11 | **数据模型**: [data-model.md](../data-model.md) §2.5/§2.8

三个 workspace 包（`common/js/dsh-plugins/`，`common/js/**` glob 覆盖；包契约形态对齐 049 [glm-llm-plugin.md](../../049-agent-v2-dsh-init/contracts/glm-llm-plugin.md) §1）+ 组合清单重写。**dsh 版本线 0.1.1-rc.2 精确 pin**（A8）；对官方 agent-loop 是"抄设计"而非继承代码（调研 §7.2 风险 1）。

## 1. @dominion/dsh-desktop-bridge

见 [desktop-bridge.md](desktop-bridge.md) §2（服务接口与行为契约）。包：`name: "desktop-bridge"`，无 inject，无运行时 deps（grpc handler 类型经 agent_v2 生成类型对齐或本地最小接口声明）。

## 2. @dominion/dsh-saolei-loop

```ts
// common/js/dsh-plugins/saolei-loop/src/index.ts
export const name = "saolei-loop";
export const inject = ["agents", "sessions", "llm", "tools", "systemPrompt", "desktopBridge"];
// + export const Config = z.object({ maxParallelToolCalls: z.number().default(10) });

// AgentOptions 声明合并扩展（persona 注入缝，research D3）
declare module "@deepseek-ai/dsh-agent" {
  interface AgentOptions { persona?: string }
}

export const DEFAULT_PLAYER_BASE = "<v1 player base 提示词，源 projects/game/agent/src/team/player.ts:79-85>";

// 服务面（agent-scoped）：工厂在 prepare() 内、agent 发布前，把 GameRuntime 以
// Service class 形态注册为该 agent scope 的 "saoleiGame" 服务——cordis Service 契约
// 保证随 agent scope 卸载自动注销（无手动清理路径）。
export interface SaoleiGame extends GameRuntime {}
declare module "@deepseek-ai/cordis" {
  interface Context {
    /** 仅在 agent.ctx 及其派生 scope 上可见；host/root 上下文上为 undefined。 */
    saoleiGame?: SaoleiGame;
  }
}
export interface GameRuntime {
  init(signal?: AbortSignal): Promise<ToolOutcome>;
  operate(input: OperateInput, signal?: AbortSignal): Promise<ToolOutcome>;
  remain(): ToolOutcome;
  /** 终局事件只读视图（统计/复盘用途预留；本 feature 无 planner 消费）。 */
  peekGameEvent(): GameEventRecord | null;
}
export type ToolOutcome = { isError: false; text: string; concludesTurn?: true } | { isError: true; error: { message: string } };
// 终局收束扩展（062）：成功结果的识别棋盘为终局（won/lost）时携带 concludesTurn: true。
// 契约与判定矩阵见 specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md §1/§2。
```

### 2.1 工厂与驱动（FR-010）

- `apply()` 构造时 `ctx.agents.setFactory(this)`（官方 AgentLoop 同型）。
- `createAgent(ownerCtx, options)`：发布序列对齐官方（setup → 注册 session/agent → `agent/session-start` → 启动驱动；teardown 注册先于 publish）；创建 GameRuntime 并以 Service class 形态注册为 **`agent.ctx` 的 `saoleiGame` 服务**（agent-scoped，随 scope 卸载自动注销）；于 `agent.ctx` 注册 agent-scoped `systemPrompt.section({name: "deployment:persona", order: 0, text: options.agentOptions.persona || DEFAULT_PLAYER_BASE})`（shadow 全局同名 section，research D3）；注册 loop 模板变量（`model`/`cwd`，官方行为对齐）。
- `resume(ownerCtx, options)`：**抛错**（A2 无 persistence——不挂 dsh-session-persistence，resume 不可达；fail-loud 优于静默）。
- 驱动器 `SaoleiLoopAgent implements Agent`（dsh-agent `Agent` 全接口：id/options/session/inbox/status/ctx、cancel/whenIdle/runMaintenance/send/followup/steer）——**必继承调研 §4.7 八条**：
  1. turn/step 状态机骨架（phase idle/running/maintenance + `setPhase` 广播 + activityDone 静默追踪）；
  2. `signal.throwIfAborted()` 检查点布局（每 chunk、每 step 边界、每 turn 边界）；
  3. 中断流部分内容以 `assistant/message {interrupted: true}` 落日志；
  4. abort 后唤醒重定向（wakingAfterAbort）与 wake latch；
  5. `agent/pre-step` → `agent/request` → `agent/request-error` → `agent/turn-stopping` 四决策点 waterfall/serial 语义；
  6. driver containment（kick catch-all + `agent/error` 先发后抛）；
  7. 工厂所有权（插件 dispose：拒绝新工作 + 全量 abort + 等待 startup/settlement 静默）；
  8. Inbox/事件面复用 dsh-agent（durable splice、`agent/*` 事件、turn/end 兜底 + TurnEndReason 区分）。
- 工具调度：经 `ctx.tools` 官方管线（`TOOL_RUNTIME_SCHEDULER` 语义：prepare→dispatch→finalize、exclusive/parallel 有界、abort 未启动调用合成错误结果——"tool_call 必有 tool_result" wire 不变量）；`maxParallelToolCalls` 默认 10。
- **GameRuntime 生命周期挂接**：注册于 `agent.ctx`（Service class 形态），随 agent scope 卸载**自动注销**——无手动清理监听（调研 §5.5 判定的吻合形态；测试注入经 `SaoleiLoopPluginOptions.createRuntime` seam）。

### 2.2 GameRuntime 行为（FR-011，v1 契约语义迁移）

- 状态/历史/状态机/操作管线/结果文本契约：[data-model.md](../data-model.md) §2.5（逐条对应 v1 源）。
- 下发：`ctx.desktopBridge.dispatch(sessionName, part, signal)`——loop 即游戏控制点（工具经 runtime 间接到达桥接）。
- 识别：`SaoleiBoard.init/updateFromScreenshot`（`@dominion/game-saolei-board`，FR-015 复用；`BoardStateIncompatibleError` 等异常 → 识别失败路径）；截图空间识别 / client 空间下发坐标常量随迁（`projects/game/agent/src/mcp/saolei/geometry.ts`）。
- 统计：`computeGameStats`（operationCount / correctFlags / avgOpsPerMine）语义随迁（v1 `saolei-mcp.ts:396-426`）。
- 游戏历史：gameLog/gameEvent 语义 = v1 `EphemeralGameBuffer`/`createTeamSink`（`projects/game/agent/src/team/team-sink.ts`）——init 重置 gameLog、一次 operate 一条（含完整操作列表）、终局一条 + gameEvent 终局记录。

## 3. @dominion/dsh-saolei

```ts
// common/js/dsh-plugins/saolei/src/index.ts
export const name = "saolei";
// saoleiGame 是 agent-scoped 服务（插件加载时无任何 agent 存在），不能静态 inject；
// 工具执行期经 exec.agent.ctx 惰性解析。
export const inject = ["tools", "systemPrompt"];
```

- **三工具全局注册**（`ctx.tools.register(defineTool(...))`，dsh-tools）：
  - `saolei_init`（无参）：`runtime.init(exec.signal)` → outcome；
  - `saolei_operate`（参数双形式：single `{type: click|flag|chord, x: int≥0, y: int≥0}` 或 `{operations: [{type,x,y}...]}`，互斥校验文本 = v1 `MISSING_ARGS_TEXT`/`AMBIGUOUS_ARGS_TEXT`/`INCOMPLETE_ARGS_TEXT` 字面量）：`runtime.operate(input, exec.signal)`；
  - `saolei_remain`（无参）：`runtime.remain()`。
  - exec 体：经 **`exec.agent.ctx`** 解析该 agent scope 内注册的 `saoleiGame` 服务（声明合并类型；`ToolExecution.agent` 携带调用者，dsh-tools `lib/types/index.d.ts:192-200`；`exec.agent` 缺失或服务不在 scope = 非 loop 驱动的调用/注册前窗口，**fail-loud 抛错**）；工具自身无状态（FR-013）。
  - output 声明：`{result: string}` canonical JSON（render = 棋盘文本）；`ToolOutcome.concludesTurn === true` 时先调用 `exec.concludeTurn()` 再返回（终局收束，[062 契约](../../062-team-game-end-handoff/contracts/saolei-turn-conclude.md) §2）；`ToolOutcome.isError` → 工具抛错（模型可见失败，不伪造成功）。
- **prompt section**（FR-014）：`ctx.systemPrompt.section({name: "saolei:guidance", order: 100, text})`——内容迁移 `projects/game/agent/src/skill/saolei/SKILL.md`（识别棋盘非截图/符号表/坐标标尺/三层结果体/坐标约定/三工具用法/校验 triage 表/示例流程/禁用项），措辞适配插件工具语境；**不保留 skill 文件**。

## 4. @dominion/dsh-llm-glm 扩展（research D4/D11）

- `listModels()` override：返回 `config.models`（静态，无端点调用）。
- 序列化扩展（解除 049 §4 表两处 `UNSUPPORTED_CONTENT`）：assistant `tool-call` 块 → `{type:"function_call", call_id, name, arguments}`；`tool-result` 消息 → `{type:"function_call_output", call_id, output}`（OpenAI Responses input item 形状，[openai-openapi](https://github.com/openai/openai-openapi)）。
- 其余协议义务（usage 先于 finish、index 分配、错误两路径、signal、条件 Authorization、token 零泄漏）不变（049 合同 §3）。

## 5. 组合清单（projects/game/agent_v2/cordis.yml 重写，FR-012）

```yaml
# 直组核心件 + 三自研插件（无 spine、无官方 agent-loop；research D5）
- { id: timer,           name: '@deepseek-ai/cordis-plugin-timer' }
- { id: llm,             name: '@deepseek-ai/dsh-llm' }
- { id: session,         name: '@deepseek-ai/dsh-session' }
- { id: system-prompt,   name: '@deepseek-ai/dsh-system-prompt',
    config: { includeHarnessIdentity: false, includeRuntimeContext: false } }
- { id: tools,           name: '@deepseek-ai/dsh-tools' }
- { id: agents,          name: '@deepseek-ai/dsh-agent' }
- { id: invariants,      name: '@deepseek-ai/dsh-invariants' }
# invariant 伴生三行：subpath 行名（@deepseek-ai/dsh-session/invariant 等）；
# 若 Loader 拒绝 subpath 行名 → 本地 wrapper 包 @dominion/dsh-core-invariants（research D5 双案）
- { id: invariant-session, name: '@deepseek-ai/dsh-session/invariant' }
- { id: invariant-agent,   name: '@deepseek-ai/dsh-agent/invariant' }
- { id: invariant-scope,   name: '@deepseek-ai/dsh-scope/invariant' }
- { id: llm-retry,       name: '@deepseek-ai/dsh-llm-retry' }
- id: llm-glm
  name: '@dominion/dsh-llm-glm'
  config: { apiKeyEnv: GLM_API_KEY, baseURL: !!js process.env.GLM_BASE_URL,
            models: [{ id: !!js process.env.GLM_MODEL || 'glm-5.2', contextWindow: 1000000 }] }
- { id: desktop-bridge,  name: '@dominion/dsh-desktop-bridge' }
- { id: saolei-loop,     name: '@dominion/dsh-saolei-loop' }
- { id: saolei,          name: '@dominion/dsh-saolei' }
```

**移除**：spine 行（含其 persona/零工具 config）、官方 agent-loop（被 saolei-loop 替换，不得残留——FR-012）。**不挂** dsh-session-persistence / dsh-settings（调研 §3.1 可选项裁定）。

## 6. 宿主演进（projects/game/agent_v2/src）

- `server.ts`：注册 `AgentService`（新 handlers：UpdateAgent/GetAgent/ListAgentMessages/Send + preset CRUD + ListModels）与 `DesktopBridgeService`（`ctx.desktopBridge.handlers()`）于同一 50051 server。
- `session.ts`：`AgentSessions` 演进——`getOrCreate` 懒物化废止；`materialize(session, {preset, model, persona})`（UpdateAgent 语义实现：校验先行、终止在途回合、dispose 旧、create 新）；`send` 前置物化校验（`FAILED_PRECONDITION`）；`dispose` RPC 面移除（进程 shutdown 内部清理保留）。
- `history.ts`：TurnCollector 扩展——`tool/call`/`tool/result` session 事件映射（`tool_result` ChatEvent + 历史 ToolCallBlock 终态回填，join 键 tool_id）；**回合全局 block index 重映射**（step 边界重置 step-local 表，research D10）；`SessionHistory.appendAssistant` 后 tool_id 索引维护。
- `presets.ts`（新）：Mongo 存取（`game_agent_v2.presets`；`MONGO_URI` 覆盖 / resolver 发现 `dominion:///game/mongo:27017`）。
- `bootstrap.ts`：mongo 客户端生命周期（graceful shutdown 顺序：server → agents → fiber → mongo → OTel）。

## 7. 测试义务（每包 vitest 随交付）

1. **saolei-loop**：驱动器状态机（turn/step/abort/排队语义，fake llm/tools 注入——对齐 049 session.test.ts 模式）；GameRuntime 全契约（fake bridge + fake boardApi——v1 `saolei-mcp.test.ts` 2015 行用例基线迁移：三工具/双形式/逐条拒绝/batch triage/chord 宽松/识别失败/信号透传/每调用一条 gameLog）；persona 回退；agent-scoped 生命周期（agent dispose 后其 scope 上 `saoleiGame` 不可达——scope 注销断言；root ctx 恒不可达——隔离断言）。
2. **desktop-bridge**：attach/接管/断连结算/超时/abort/uuid tool_id/stale 回执忽略（v1 `operation-bridge.test.ts` 基线迁移）。
3. **saolei**：工具 schema/参数互斥文本/exec 经 `exec.agent.ctx` 解析转发（fake saoleiGame 注册于 agent scope；`exec.agent` 缺失时 fail-loud 断言）；prompt section 注册生效。
4. **llm-glm**：listModels 返回 config.models；function_call/function_call_output 序列化往返；既有 049 用例零回归。
5. **agent_v2 宿主**：049 既有用例（send/queue/history/backfill/dispose-shutdown）零回归（接口更名后）；新增 UpdateAgent 语义/未物化 Send 拒绝/preset CRUD/模型校验用例。

## 8. 实现期必读（间接引用显式列出）

- 官方 loop 物化源码（抄设计基准，A8）：`node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（ReactLoopAgent + AgentLoop 全量）与 `lib/types/*.d.ts`
- dsh 公共面类型：`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/lib/types/*.d.ts`（Agent/AgentHandle/AgentRegistry/Inbox）
- dsh-tools / dsh-system-prompt README（本仓库 node_modules 内，工具注册与 section/order 频带语义）：`node_modules/.pnpm/@deepseek-ai+dsh-tools@0.1.1-rc.2_*/…/README.md`、`node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_*/…/README.md`
- 调研继承清单：`survey/deepseek-harness-agent-loop-prereq.md` §2–§5（逐节：包与替换机制/依赖面/异常处理/能力提供/状态形态）
- v1 语义源：`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（工具契约全量）、`projects/game/agent/src/skill/saolei/SKILL.md`（提示词内容源）、`projects/game/agent/src/team/team-sink.ts`（游戏历史语义）、`projects/game/agent/src/team/player.ts:79-85`（DEFAULT_PLAYER_BASE 源）
- 识别库：`projects/game/pkg/saolei-board/`（`SaoleiBoard.init/updateFromScreenshot`、坐标几何 `src/core/geometry.ts`）
- 049 glm 插件契约：`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md`
