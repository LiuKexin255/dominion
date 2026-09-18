# 调研：dsh 官方 agent-loop 与基础插件前置条件（saolei-loop 迁移前置）

> **状态**：调研完成。**决策状态（2026-09-08 更新）**：本文 2026-08-28 确认的 ①"单 agent 双角色"拓扑与②随之推导的"agent-scoped 游戏状态"**已被取代**——`survey/deepseek-harness-team-mode.md` 头部决策 ③ 拍板 **loop 持有多 agent**（player/planner 双顶层 agent + team 层群聊模型），游戏状态归属改由该文 §8 待定项 5 承接；③ 桥接形态取**插件桥接**仍然有效，桥接面统一与否待定（§5.4、§7.3）。**术语变更**：saolei-loop 的定义已从 agent loop（本文所调研的自研替换层）变为更高层次的 team loop，agent 驱动沿用传统 agent loop（官方 `dsh-agent-loop` 行保留）——见该文头部"术语变更"与 §9.3；本文中"saolei-loop"一词按其旧含义（agent loop 层）阅读，本文的机制事实（依赖面、异常处理、能力提供、message 无需专职插件）不受影响，§4.7/§4.6 的自研重写清单随驱动保留官方行而消解。§5.6（单 agent 双角色表达）与 §5.5 的 agent-scoped 形态判定按上述取代关系阅读。
> **日期**：2026-08-28（头部决策状态 2026-09-08 更新）
> **前置调研**：`survey/deepseek-harness-framework.md`（框架总体架构）、`survey/deepseek-harness-preset.md`（preset/系统提示词）、`survey/deepseek-harness-b1-plugin-packaging.md`（B1 嵌入）、`survey/deepseek-harness-b1-bazel-packaging.md`（B1 打包）、`specs/047-dsh-chat-demo/research.md`（B1 实证 D1–D10）
> **范围**：官方 `dsh-agent-loop`（0.1.1-rc.2，与 `third_party/dsh/core` 同线）的依赖面与冗余性、异常处理机制（优雅终止/断点继续/工具/MCP/LLM 流）、插件间能力提供机制（自研基础插件的机制前提）、message 传递是否需要独立插件、单 agent 双角色的机制表达。**不含** saolei-loop 具体设计。
> **说明**：本文为调研材料（源码级事实与机制结论）+ 头部所列讨论确认的决策记录；除已标注的确认项外，不含采用决策、迁移方案或未来方向设计。
> **后续调研**：`survey/deepseek-harness-team-mode.md`（2026-09-08）验证双 agent 拓扑的机制前提并拍板 **loop 持有多 agent**（player/planner 双顶层 agent + team 层群聊模型，取代本文 §5.6 决策）——preset 池组织与角色差异承载（编辑期固定、物化零定制）、registry 之上 team 层、1:1 sender-only 广播消息格式均有结论与设计基线；该文 §6/§8 为后续 spec 输入。

---

## 1. 背景与调研问题

agent 迁移 dsh 后计划自研 saolei-loop（原生实现 player/planner 双 agent 与局后复盘机制，替代当前 LangChain 中间件补丁方案）。自研 loop 应参考官方 agent-loop 实现。本次调研回答四个前置问题：

1. 官方 agent-loop 的依赖插件有哪些？对扫雷场景是否冗余？
2. 官方 agent-loop 的异常处理（优雅终止、断点继续、工具/MCP 异常处理等）是否完备？自研 loop 应继承哪些？
3. 自研基础插件的机制前提：一个插件能否为其他插件提供能力（grpc-js 桥接插件、扫雷游戏插件都要作为能力底座）？
4. message 传递是否需要第三个插件？（预判：不需要，message 生成方式固定）

信息源（全部为直接源码/官方文档实证）：

- 本地物化的 npm 包（0.1.1-rc.2 线，pnpm store）：
  - `@deepseek-ai/dsh-agent-loop`（node_modules/.pnpm 内，1318 行实现）
  - `@deepseek-ai/dsh-agent`（`experimental/dsh/demo/agent/node_modules`，Agent 接口/Inbox/registry）
  - `@deepseek-ai/dsh-agent-spine-demo`（spine 组装件源码）
  - `@deepseek-ai/dsh-tools`、`dsh-llm-retry`、`dsh-session`、`dsh-session-persistence`、`dsh-scope`
- dsh 官方仓库（master）：
  - [packages/mcp/mcp-client/README.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md)
  - [MCP client auto-reconnect Agent Note（2026-08-06）](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/feature/2026-08-06-mcp-client-auto-reconnect.md)
  - [docs/user/develop/basic/index.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/basic/index.md)（插件形态）
  - [docs/user/develop/framework/service.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/service.md)（服务与依赖）
  - [docs/user/develop/framework/events.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/events.md)（事件系统）
- 仓库内实证样板：`experimental/dsh/demo/agent/`（B1 宿主桥接 + session/event 收集 + get-or-create）

---

## 2. 官方 agent-loop 解剖

### 2.1 包与替换机制

`@deepseek-ai/dsh-agent-loop`（描述原文 "The concrete agent loop plugin for the DeepSeek Harness"）导出两个入口：主入口（`AgentLoop` 服务插件）与 `./invariant`（request-reconstruction 不变量伴生）。

**loop 可替换是接口/实现分离的显式设计**：`AgentFactory` 接口定义在 `dsh-agent` 包（`ctx.agents` registry 一侧），注释原文："The agent-creation factory the loop implementation provides to the registry via `AgentRegistry.setFactory`. Kept on the `dsh-agent` interface so consumers (e.g. the ACP bridge) program against `ctx.agents` without depending on the concrete `dsh-agent-loop` package."——即消费者（宿主、桥接插件）只面向 `ctx.agents.create()/resume()`，loop 实现经 `setFactory` 可整体替换。`AgentFactory` 仅两个方法：

- `createAgent(ownerCtx, options): Promise<AgentHandle>`——建 session + agent，rollback 覆盖的发布序列（setup → 注册 session/agent → `agent/session-start` → 启动 loop）。
- `resume(ownerCtx, options): Promise<AgentHandle>`——经 `ctx.sessionPersistence.prepare` 加载持久化会话后同序发布。

自研 saolei-loop 的替换路径因此明确：**组合清单不挂 `dsh-agent-loop` 行，挂自己的行并 `ctx.agents.setFactory(saoleiFactory)`**；宿主/桥接代码零改动（demo 的 `AgentSessions` 即面向 `ctx.agents` 编程的实例，`experimental/dsh/demo/agent/src/session.ts:151`）。

### 2.2 服务与驱动两层

**AgentLoop 服务**（`super(ctx, "agentLoop")`，即 `ctx.agentLoop`）：

- `static inject = ["agents", "sessions", "llm", "tools", "systemPrompt"]`——五个必需服务。
- `static Config`：`maxParallelToolCalls`（默认 10）+ `agents[]` 声明式配置（id/sessionId/provider/model/maxTokens/cwd/resumeSessionId；sessionId 与 resumeSessionId 互斥、exact identity 查重）。
- 构造时 `ctx.agents.setFactory(this)`；注册 system-prompt 模板变量（`provider`/`model`/`cwd`）；对 config 声明的 agent 在启动时 create/resume（失败经 `agent-loop/config-start-failed` 事件上报，不 crash 进程）。
- `FactoryOwnership`：工厂级所有权——live agent teardown 集合、startup task 集合、teardown signal（"agent loop is not active"）；插件卸载时 `dispose()` abort 全部 agent + 等待全部 startup/settlement 完成。

**ReactLoopAgent 驱动**（实现 `dsh-agent` 的 `Agent` 接口）：每 agent 一个实例，驱动 turn/step 边界。公开面：

| 成员 | 语义 |
|---|---|
| `followup(msg)` | 排队 next-turn + 唤醒（普通用户回合消息） |
| `steer(msg)` | 排队 next-step + 唤醒（运行中在下一 step 边界生效；idle 则开新 turn） |
| `inject(msg)` | 排队 next-step **不唤醒**（model-facing context：文件变更通知、技能内容等） |
| `cancel(cause, {keepInbox})` | 默认清空 inbox + abort 当前活动；`keepInbox` 保留待处理工作 |
| `runMaintenance(job)` | idle 态独占运行维护任务（status 保持 idle，唤醒输入滞留 inbox） |
| `whenIdle()` | 等待整个 agent 静默（含替换工作） |

Inbox（`dsh-agent` 提供）是**双队列投影**：`next-turn`（每条独占一个 turn）与 `next-step`（攒到下一 step 边界）；所有 mutation 是 durable session 事件（`agent/inbox/spliced`），replay-once 重放恢复。

**turn/step 主循环**（`turn()` → `preStep()` → `step()`）：一个 step = 一次模型请求 + 其工具调用；一个 turn = 零或多个 step。`agent/pre-step` waterfall 决定进入或拒绝（reject 则 turn 以 `blocked` 关闭，claimed 消息不丢弃、不重发）；`agent/turn-stopping` serial 事件允许 listener 在 turn 关闭前 steer 续跑（数据决定，监听顺序不影响结果）；工具结果 `concludesTurn: true` 可在 step 处结束 turn。runtime-context 投影（`RuntimeContextProjection`）把动态上下文差异作为合成 user message 落日志（"Model-visible means logged" 不变量）。

---

## 3. 调研问题 1：依赖插件与冗余性

### 3.1 依赖三层视图

**层 1 — 包级 peers（`dsh-agent-loop` package.json，10 项）**：

| peer | 用途 | saolei-loop 必要性 |
|---|---|---|
| `@deepseek-ai/dsh-agent` | Agent 接口、Inbox、agentEvents、registry | **必需**（实现其接口/复用其基础设施） |
| `@deepseek-ai/dsh-session` | Session 事件日志、SessionId、header 折叠 | **必需** |
| `@deepseek-ai/dsh-llm` | 消息构造、BlockAssembler、LlmError | **必需** |
| `@deepseek-ai/dsh-system-prompt` | prompt 组装/渲染 | **必需** |
| `@deepseek-ai/dsh-tools` | `TOOL_RUNTIME_SCHEDULER` 工具调度面 | **必需**（若复用官方工具管线——saolei 工具推荐走 `ctx.tools` 注册） |
| `@deepseek-ai/dsh-scope` | agent-scoped 注册边界（createScope） | **必需** |
| `@deepseek-ai/dsh-invariants` | invariant 伴生注册 | 建议（一致性防护，见 §4.6） |
| `@deepseek-ai/cordis` | 框架 | **必需** |
| `@deepseek-ai/dsh-session-persistence` | resume（断点继续） | **可选**（`ctx.get("sessionPersistence")` 可选注入；无则 resume 抛错、create 正常） |
| `@deepseek-ai/dsh-settings` | `installSettingsSection`（maxParallelToolCalls 用户设置节） | **冗余可去**（自研 loop 可不暴露用户设置节；仅库调用，无行为依赖） |

运行时 dependencies 仅 `@deepseek-ai/schemastery`（schema 库）。

**层 2 — 服务级注入**：必需 5 个（agents/sessions/llm/tools/systemPrompt）+ 可选 `sessionPersistence`（`ctx.get` 查询式）。无隐藏依赖。

**层 3 — spine 组装面（对照组，`dsh-agent-spine-demo` 静态挂载 17 项）**：Timer、LlmRuntime、SessionStore、SessionTitleService、SystemPrompt、ToolRuntime、SkillRegistry + SkillFileSystem（可 config 关）、AgentRegistry、llmRetry、Goal 三件套（默认不挂）、JobsLocal、InvariantRegistry + 4 个 invariant 伴生、shellEnv + toolBash（默认挂、config 可关）、workspaceContext（默认挂、可关）、toolSkill、toolJobs、AgentLoop。**spine 的冗余是 spine 作为"通用 coding spine"的组装冗余，不是 loop 本体的依赖**：session-title（标题生成）、skill 生态、jobs（后台任务）、bash/shell 工具、goal 驱动、agent-instructions（workspace 注入）都不在 agent-loop 的 peers 里。

### 3.2 结论

- **官方 agent-loop 本体依赖面对扫雷场景几乎无冗余**：10 peers 中 7 项是 loop 机制必需，2 项可选（persistence/settings），裁剪面很小。
- **冗余集中在 spine 组装层**——这正是 B1 打包调研已锁定的"按需添加插件、不用 spine 也可以直接组核心件"决策的依据（`survey/deepseek-harness-b1-bazel-packaging.md` §5.4 记录"绕过 spine 直组核心件无官方先例"的 PoC 探索项；自研 saolei-loop 本身就是"直组核心件 + 自己的 loop 行"形态，同样属于该探索项）。
- saolei-loop 的依赖最小集 = 上表 7 项必需 + 按需（persistence/llm-retry）。**推荐直接复用而不重写的周边**：`dsh-llm-retry`（LLM 请求重试，见 §4.4）、`dsh-tools` 工具管线（saolei 工具注册进 `ctx.tools` 即获得并发调度/异常处理/pre-post 钩子全套）。

---

## 4. 调研问题 2：异常处理机制（自研 loop 应继承的清单）

官方 agent-loop 的异常处理**总体完备**，且大多数机制位于 loop 可复用的基础设施层（dsh-agent/dsh-session/dsh-tools），自研 loop 只要复用这些层就自动继承；需要 saolei-loop 自身在驱动逻辑里重写的部分在 §4.7 汇总。

### 4.1 优雅终止（cancel/abort/dispose）

事实（`dsh-agent-loop/lib/index.js` ReactLoopAgent + AgentLoop）：

- **cancel 语义**：`cancel(cause, {keepInbox})` 默认清空 inbox 并 abort 当前活动；first-cause-wins；无活动时是 no-op。`AgentCancelCause` 是稳定语义类型（非自由字符串）。
- **abort 贯穿**：turn/step/stream 逐 chunk/工具调度各处 `signal.throwIfAborted()`；流中断时已生成的部分内容以 `assistant/message {interrupted: true}` 落日志（保真回放），未派发 tool call 不出现在消息里。
- **abort 后唤醒**：`wakingAfterAbort` 把 abort 期间的 wakeup 重定向到 next-turn（防止 abort 竞态吞消息）；maintenance/abort 期间到达的唤醒 latch 在 `wakeRequested`，idle 后重放。
- **dispose 链**（agent 级）：`cancel({kind:"disposed"})` → `whenIdle()`（等待驱动静默）→ `scope.dispose()`（agent-scoped 注册回滚）→ detach agent/session 注册。setup 中途卸载同样回滚（teardown 注册先于 publish）。
- **工厂级 teardown**：`FactoryOwnership.dispose()` 拒绝新工作（"agent loop is not active"）+ abort 全部 live agent + 等待 startup task 集合静默——插件卸载不会留下孤儿 driver。
- **事件面**：`agent/status`（idle ⇄ running）每次翻转广播；047 D3 已实证 idle 是回合终止信号。

### 4.2 断点继续（resume）

事实：

- **resume 是"冷恢复"而非 mid-turn 续跑**：中断的 turn 以 `turn/end {reason: {kind: "aborted"}}` 关闭；`ctx.agents.resume({resumeSessionId, ...})` 从 persistence 加载完整事件日志重建 Session，ReactLoopAgent 构造时 `lastTurn = findLast(turn/start)?.turn ?? 0` 续编号，`request/header` 以 `reason: "resume"` 重新锚定。**进行中的 turn 不会被续跑**——恢复后从下一个 turn 开始，"如何重新驱动（例如注入游戏状态快照）"是 loop 侧策略。
- **前提服务**：`sessionPersistence`（`dsh-session-persistence` Service Definition：`append` 持久化后才 resolve、`load` 平衡中断尾、`prepare` 返回 SessionPreparation）；后端插件 `dsh-session-persistence-jsonl` / `-sqlite`（rc.8 起 SQLite 数据结构有不兼容变更记录，见 `survey/deepseek-harness-b1-plugin-packaging.md` §4.4）。
- **模型上下文恢复**：`deriveMessages()` 从日志投影 model history（"Model-visible means logged" 不变量 + 运行时断言：请求 messages 必须与日志派生一致，否则 fail——见 §4.6 invariant）。
- **同 id 重挂**：`restoreOrCreateConfigured` 等 registry 排空后再挂（`agent/disposed`/`session/disposed` 监听），防同 id 竞态。

### 4.3 工具异常处理

事实（`dsh-agent-loop` tool-calls 模块 + `dsh-tools` ToolRuntime）：

- **调度管线**：`TOOL_RUNTIME_SCHEDULER` 暴露 `prepare → dispatch → finalize/finish` 分阶段面（专供 loop 的并行调度器）；工具有 `executionMode`（exclusive 屏障 / parallel 有界滚动池，上限 `maxParallelToolCalls`）；结果与上下文按 model order 提交。
- **abort 时保 replay**：未启动的调用记录合成错误结果（`TOOL_ABORTED_BEFORE_DISPATCH`，"tool call aborted before dispatch"）——模型见到 tool_call 必有对应 tool_result 的 wire 不变量在取消路径也成立；已启动的调用 drain 并按序提交。
- **调度器内部失败**：停止补充新调用、drain in-flight、抛第一个失败；**不伪造工具结果**（保留已记录的 `tool/call` 事件）。
- **工具体契约**：必须观察 `exec.signal`；registry 不 abandon promise、不 hard-kill 同进程代码（协作式取消）。工具抛错 → `isError: true` 的 ToolExecutionFailure（模型可见失败，不是假成功）；输出 schema 违规 → `ToolOutputError`。
- **扩展钩子**（全部 scope-filtered，saolei 侧可挂策略）：`tools/pre-execute`（allow/deny/ask 审批）、`tools/execute`（around-dispatch：超时/重试/指标）、`tools/post-execute`（accept/replace/block + additionalContexts）、`tools/result`（观察）、参数解析容错（invalid JSON 保留原文）。

### 4.4 LLM 流异常处理

事实：

- **恢复扩展点**：`agent/request-error` waterfall——listener 返回 `{kind: "retry"}` 即接管恢复（不 retry 的默认失败是 terminal）。官方 `dsh-llm-retry` 插件（spine 携带、独立包）就挂在此点：provider-routed 重试策略 + 持久化重试记录（"Each scheduled retry is durable before its cancellable wait"）。**自研 loop 不需要实现重试逻辑，挂官方 llm-retry 行即可**。
- **错误归一化**：`LlmError`（code + serializable failure facts）在 adapter 边界归一；非 LLM 错误 → `{message, code: "UNKNOWN"}`。
- **turn 边界兜底**：`turn/end` 在 finally 中总是记录，`TurnEndReason` 区分 `completed/blocked/aborted/error/max-tokens`；`agent/error` 事件先 emit 再 throw（driver containment：`kick()` catch 所有驱动异常，单 agent 故障不影响 registry 其他 agent）。
- **流 stall**：dsh 侧流式读取是 `for await`（无内建 stall 计时器）；现有 agent 的 stall 恢复实践见 `survey/llm-stream-stall-recovery-revision.md`（LangChain 侧），dsh 迁移后 stall 治理挂点在 `tools/execute` 式 around 层或 adapter `stream()` 内部实现（自研 GLM Responses 适配器，`specs/049-agent-v2-dsh-init/spec.md` FR-007）。

### 4.5 MCP 异常处理（`dsh-mcp-client`，独立插件、默认不启用）

事实（[README](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md) + [auto-reconnect note](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/feature/2026-08-06-mcp-client-auto-reconnect.md)）：

- **自动重连**：per-instance connection supervisor；bounded backoff（500ms 起翻倍至 30s 上限）+ per-outage attempt budget（默认 10 次，约 2.5 分钟）+ 稳定窗口（uptime > maxDelayMs 重置预算——区分偶发崩溃与 crash loop）；预算耗尽注销该 server 工具并停止（error 日志），防重启风暴。
- **generation 原子交换**：每次重连建新 Client；全部 sync（初始/list_changed/重连）过单队列串行化，失败保留上一代工具集（不部分注册）；tool list 冲突整体回滚。
- **调用层**：per-call timeout（`toolCallTimeoutMs` 默认 60s）+ abort signal 透传；server 报 `isError` → 调用可见失败（模型不看到假成功）。
- **启动策略**：默认 fail-soft（连接失败仅日志、无该 server 工具、进程继续）；`failOnStartupError: true` 可变 fail-loud。
- **quiescent disposal**：dispose 翻 fence → 取消 pending timer → 关 client → 等待 in-flight attempt + sync 队列静默再注销；重连 timer unref 不拖住进程退出。
- **已知限制**：仅桥接 Tools capability；Streamable HTTP 的请求失败由 SDK transport 自恢复（supervisor 只管 stdio child 退出）；启动/discovery 超时继承 MCP SDK 60s 默认。

对 saolei 的含义：若 saolei/memory 维持 MCP server 形态，`dsh-mcp-client` 直接可用（`mcp__<server>__<tool>` 命名注册进 `ctx.tools`）；其 supervisor 模式（bounded backoff + budget + 原子代交换 + quiescent disposal）也是自研扫雷游戏插件对外部依赖（如 desktop 游戏引擎连接）异常处理的参考范式。

### 4.6 不变量（invariants）与一致性防护

`dsh-agent-loop/invariant` 伴生：在 `llm/stream`（prepend、global）断言 loop 构造的请求必须 frozen、携带 live session id、messages 与 `session.deriveMessages()` 逐字节一致（log-reconstruction desync 检测）、config/system/tools 与折叠的 request/header 一致。注册进 `dsh-invariants` 的 invariant registry（fail-loud 断言面）。同类伴生还有 session/agent/scope 三个（spine 全部挂载）。自研 saolei-loop 若改动了"请求从日志派生"的任何环节，应提供自己的 invariant 伴生。

### 4.7 saolei-loop 应继承清单（结论）

**自动继承（复用基础设施即得）**：abort signal 贯穿模式、inbox durable splice、`agent/*` 全部事件面、turn/end 兜底与 TurnEndReason、工具调度与取消语义、llm-retry、MCP client 全套。

**需在 saolei-loop 驱动逻辑中重写/保留的模式**（参考 ReactLoopAgent 实现逐条对照）：

1. turn/step 状态机骨架：phase（idle/running/maintenance）+ `setPhase` 状态广播 + `activityDone` 静默追踪。
2. `signal.throwIfAborted()` 检查点布局（每 chunk、每 step 边界、每 turn 边界）。
3. 中断流的部分内容落日志（`interrupted: true`）。
4. abort 后唤醒重定向（`wakingAfterAbort`）与 wake latch。
5. `agent/pre-step` → `agent/request` → `agent/request-error` → `agent/turn-stopping` 四个决策点的 waterfall/serial 语义（复盘/策略注入的挂点，见 §5.3）。
6. driver containment（kick catch-all + `agent/error` 事件先发后抛）。
7. 工厂所有权（FactoryOwnership 等价物：插件卸载时全量 abort + 静默等待）。
8. resume 冷恢复语义（lastTurn 续编号 + request/header reason: resume + deriveMessages 投影）。

---

## 5. 调研问题 3：插件间能力提供机制（基础插件的机制前提）

### 5.1 服务机制：完全支持"插件为插件提供能力"

（[service.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/service.md)）

- **提供**：class form 插件 `extends Service`、`super(ctx, 'serviceName')`；TS 声明合并给 `ctx.serviceName` 类型。**消费**：`export const inject = ['serviceName']`（必需——未就绪则插件等待，不运行）或 `ctx.get('serviceName')`（可选）。
- **依赖生命周期**：required 服务消失（provider 卸载）→ 依赖插件自动 dispose、服务回来后重新加载——桥接插件与游戏插件的启停联动由框架负责。
- **服务隔离**：`cordis-plugin-group` 的 `isolate` realm 可让不同插件组看到同服务名的不同实例（preset 调研 §2.4 记录 preset 侧约束：会话级服务必须 isolate，防止第二 session 挂同 preset 时碰撞）。
- **注册即 effect**：插件注册的服务/事件监听/工具随插件卸载自动回滚（无手动 cleanup）。

dsh 自身就是范例：`ctx.tools`/`ctx.llm`/`ctx.agents`/`ctx.agentLoop`/`ctx.sessionPersistence` 全部是插件提供的普通服务（framework 调研 §2.3）。**grpc 桥接插件与扫雷游戏插件提供自有服务（如 `ctx.saoleiGame`、`ctx.desktopBridge`）在机制上与官方服务完全同构，无特权差异。**

### 5.2 事件机制：进程内四种模式 + scope 过滤

（[events.md](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/events.md)）

- 四模式：`emit`（广播）、`bail`（短路取值）、`serial`（有序等待，首个非空停止）、`waterfall`（管线，listener 必须调 `next()`）。
- 类型化事件经 `declare module` 声明合并；**事件监听是 effect**（插件卸载自动移除）。
- **scope-filtered dispatch**（`dsh-scope`）：`agent/*`、`tools/*` 事件按 agent scope 路由——agent-scoped listener 只收到自己 agent 的事件。扫雷游戏插件若在 agent scope 内注册监听，天然获得"每会话只看到自己事件"的隔离。
- 跨进程语义区分：Cordis 事件是进程内即时通信；**durable 事实走 session 事件**（append 进日志 + `session/event` 广播）。`turn/*`、`tool/call`、`tool/result` 等是 session 事件，观察它们要监听 `session/event` 判断 `event.type`。

### 5.3 与 saolei-loop 的集成挂点（机制已备）

saolei 场景的"开始策略、局后复盘+新策略指令"在事件面上有直接对应物：

| saolei 需求 | 机制挂点 |
|---|---|
| 回合前改写/注入模型所见 | `agent/pre-step` waterfall（reject / 替换 messages） |
| 局后复盘触发新一轮 | `agent.followup()`（planner 产出作为下一 turn 消息）或工具结果 `additionalContexts`（下 step 生效） |
| 回合中转向 | `agent.steer()`（nearest step） |
| 不唤醒的上下文注入（策略快照） | `agent.inject()`（下 step 边界 claim） |
| turn 结束前否决/续跑 | `agent/turn-stopping` serial（listener steer 则续跑） |
| 提前结束 turn | 工具结果 `concludesTurn: true` |
| 游戏状态变化通知 loop | 自定义 Cordis 事件（emit）或扩展 SessionEventMap（merge-extensible，先例：`dsh-agent` 的 `agent/inbox/spliced`） |
| planner↔player 委派 | 双 agent 并存（`ctx.agents.create` 多次，demo D5 实证）或 subagent seam（父子委派，framework 调研 §2.7：continuable child session + FIFO followup；subagent provider 是独立包，spine 不含） |

### 5.4 桥接形态的两个实证先例

- **宿主直连**（`experimental/dsh/demo/agent/`，047 已实证）：bootstrap `boot()` 拿 ctx → 宿主 TS 代码（server.ts/session.ts）直接 `ctx.agents.create` + `ctx.on("session/event")` + grpc-js 对外服务。桥接逻辑在组合外。
- **插件桥接**（官方先例：`dsh-sdk-jsonrpc-server` 行——jsonrpc-agent 组合把它当插件挂载，stdio JSON-RPC 面由插件提供；ACP demo 同理）。桥接逻辑在组合内，享受：组合清单声明式启停、effect 生命周期、可向其他插件提供服务（`ctx.xxx`）、scoped 注册。

两种形态机制均成立；插件形态的差异点是把 grpc server 的启停从 bootstrap 移入 `apply()` + `ctx.effect()`（demo 的 `AgentSessions.shutdown()` 顺序——逐 agent dispose 后 `ctx.fiber.dispose()`——对应插件形态下插件自身 dispose 语义）。

**扫雷场景判定（2026-08-28 确认）**：能力方向是决策输入——下行指令（desktop/web → `agent.followup/cancel/create`）与上行消息（session/event → 转发）两种形态都能承载，但扫雷存在第三个方向：游戏插件要把棋盘变化/agent 操作流推送给 desktop 渲染（**插件 → 桥接**）。该方向只有插件桥接可原生承载（游戏插件 `inject: ["grpcBridge"]` 消费桥接服务的推送 API）；宿主直连形态需宿主代转（游戏插件 emit 事件 → 宿主订阅 → 宿主转发给 grpc），桥接被拆成两半、边界模糊。故桥接取**插件桥接**形态。关联待定项：049 已定的 web 对话面（gateway `/api/v2` → 宿主直连 gRPC）与 desktop 游戏面（插件桥接）是否统一为一个桥接插件（一个 gRPC 面、gateway 与 desktop 同为客户端）——统一更收敛（一面、一套生命周期），但需 049 宿主直连代码演进迁移；留待后续 spec 澄清（§7.3 待定项 1）。

### 5.5 per-session 状态（游戏插件的状态维护前提）

- ReactLoopAgent 每 agent 一个 `createScope` 边界：agent-scoped 注册随 agent dispose 自动 unwind（`agent.ctx` 上注册的工具/服务/监听都是 agent-local）。
- 游戏插件的两种状态形态均可行：host 级服务 + `Map<SessionId, GameState>`（监听 `agent/disposed`/`session/disposed` 清理），或 agent-scoped 注册（监听 `agent/created` 在 `agent.ctx` 上挂 per-agent 游戏服务；preset 的 isolate-realm 约束同样适用于自研 per-agent 服务行）。
- "Model-visible means logged" 约束：游戏状态若要进模型上下文，必须经 user/message（source 标注）或工具结果落日志——ReactLoopAgent 的 `RuntimeContextProjection`（合成 user message 快照 + 替换语义）是官方同型实现。

**拓扑确认后的形态判定（2026-08-28）**：player/planner 拓扑已确认为单 agent 双角色（一局游戏 = 一个 dsh session，见 §5.6），游戏状态与 session 同生命周期，agent-scoped 形态从"可能错位"（若双 agent 各持 scope 则一局游戏横跨两个 scope）变为吻合——Game 实例挂 `agent.ctx`（`agent/created` 时接线），agent dispose 时随 scope 自动 unwind，游戏事件 scope-filtered 天然按会话路由。host Map 形态退化为可选优化：仅当需要跨会话统计（胜率/局数）时在 host 层另留轻量注册表，棋盘本体仍走 agent-scoped。由此两个推论：

1. **共享游戏记录 = session log 本身**：planner 策略产出、player 每步操作、工具结果（棋盘反馈）交错落在同一个 append-only 日志，复盘所需的完整操作史就是 `deriveMessages()` 的自然投影，零额外机制。
2. **断点继续的棋盘重建**：resume 冷恢复只重建对话/工具历史（§4.2），棋盘状态在游戏插件内存。dsh 哲学给出的正路：**模型可见的棋盘事实（已揭示格子、数字、剩余雷数）本就以 tool/result 落日志，游戏插件从日志重放重建棋盘（近乎无状态，同 `deriveMessages` 的派生模式）**。隐藏信息（雷布局）例外——不得进入模型可见投影：要么作为"落日志但不投影模型"的自定义 SessionEventMap 事件（merge-extensible，§5.3），要么游戏插件自持小量持久化；属 saolei-loop spec 待明确点（§7.3 待定项 3）。

### 5.6 单 agent 双角色的机制表达（2026-08-28 确认拓扑）

角色不是两个 agent，而是 saolei-loop 内部的**阶段**——这正是自研 loop 相对 LangChain 中间件补丁的核心增量。阶段状态机（`new-game → planning → playing → win/lose → review → planning…`）由游戏插件持有（它拥有游戏规则与状态），loop 在每个 step 边界查询阶段，再以三个可组合的官方机制表达角色差异：

| 手段 | 机制 | planner/player 差异表达 |
|---|---|---|
| persona/prompt | `ctx.systemPrompt` 组装管线（`assembleContextFor(agent, signal)`），sections 可插拔 | planning/review 阶段注入 planner persona + 策略上下文；playing 阶段注入 player persona + 当前策略指令 |
| 工具可见性 | `dsh-tools` per-scope restriction（allow/deny，scoped 注册 shadow 全局） | planner 只见读状态类工具；player 见操作类工具（按阶段过滤工具目录） |
| model route | `agent/request` waterfall 替换 frozen config（provider/model/maxTokens） | planner/player 可差异化模型或参数 |

"每局结束后复盘 + 新策略指令"的现有补丁机制在此成为阶段状态机的自然转移；`agent/turn-stopping`（turn 收尾前否决续跑，listener steer 则续跑）与工具结果 `concludesTurn`（提前结束 turn）恰好覆盖"一局打完即止"的边界。是否启用差异化 model route、阶段转移规则在 loop 与游戏插件间的分权，属 saolei-loop spec 待定项（§7.3 待定项 2/4）。

---

## 6. 调研问题 4：message 传递不需要第三个插件

结论：**预判成立——不需要独立的 message 传递插件**。依据：

1. **message 的产生面固定**：全部 model-visible 消息由 loop 写入 session 日志（`user/message`、`assistant/chunk`、`assistant/message`、`tool/call`、`tool/result`、`turn/*`、`step/*`、`todo/write`、`request/header`——`dsh-session` SessionEventMap，merge-extensible）。不存在"多种生成方式需要统一抽象"的问题。
2. **message 的投递面固定**：`session/event` Cordis 事件广播（subject=session, payload=event）是唯一 durable 投递通道；官方消费者（Web UI/TUI/SDK client）全部是"订阅 + 投影渲染"（framework 调研 §3：三种 surface 共享同一事件流，互不感知）。
3. **桥接插件订阅即可**：grpc-js 桥接插件在 `apply()` 内 `ctx.on("session/event", ...)`，按 sessionId 路由转发给 desktop/web——demo 的 `AgentSessions.runRound`（`experimental/dsh/demo/agent/src/session.ts:182`）就是这个模式的宿主版实证（事件收集 + idle 终止 + 末条 assistant/message 提取，047 D3）。
4. **操作指令与消息的通道本就分离**：操作指令（下行：发消息/取消/管理）走桥接插件提供的 gRPC 面（调 `agent.followup/cancel`、`ctx.agents.create/resume`）；消息（上行：回合内容）走 session/event 订阅。一个桥接插件双向都覆盖，message 无需再分层。
5. 若未来出现"非 session 日志的瞬态通知"（如游戏引擎心跳），走自定义 Cordis emit 事件即可（§5.2），同样不需要专职插件。

---

## 7. 前置条件结论与风险

### 7.1 前置条件判定（全部满足）

| 前置条件 | 判定 | 依据 |
|---|---|---|
| loop 可替换（自研 saolei-loop 可行） | ✅ | AgentFactory 接口在 dsh-agent 公共面、setFactory 替换、消费者零改动（§2.1） |
| 官方 loop 依赖面无重大冗余拖累 | ✅ | 10 peers 中 7 必需 + 2 可选 + 1 可去（§3） |
| 异常处理可继承（避免重复开发） | ✅ | §4 全部机制；llm-retry/MCP client 可直接复用 |
| 插件可为插件提供能力 | ✅ | Service/inject + 声明合并 + isolate realm（§5.1） |
| 事件通知机制（游戏插件需要） | ✅ | 四模式 + scope 过滤 + session/event durable 广播（§5.2） |
| per-session 状态维护 | ✅ | agent-scoped 注册 / host Map + dispose 事件（§5.5） |
| message 无需专职插件 | ✅ | 产生/投递面均固定，桥接订阅即可（§6） |
| 单 agent 双角色表达（persona/工具可见性/model route 三手段） | ✅ | §5.6 |
| 桥接先例 | ✅ | 宿主直连（047 实证）+ 官方 sdk-jsonrpc-server 插件先例（§5.4） |

### 7.2 风险与限制记录

1. **0.x-rc 破坏性变更**：developer preview 明示承诺（047 D10-5）；`AgentFactory`/`Agent` 接口虽在公共面包，但无稳定性承诺。saolei-loop 对 ReactLoopAgent 模式的复制是"抄设计"而非"继承代码"，升级时需逐 rc 对照。
2. **subagent 与 spine 的绕过无官方先例**：自研 loop 直组核心件（不用 spine、不用官方 loop）在官方组合中无先例（B1 打包调研 §5.4 已记录该探索属性）；风险面是内部 API 漂移，缓解是 0.1.1-rc.2 全家桶精确 pin（既有决策）。
3. **resume 是冷恢复**：mid-turn 断点续跑不存在；扫雷"进行中一局"的恢复语义需 saolei-loop 自行设计（游戏状态快照 + 恢复注入），不能指望框架续跑 turn。
4. **流 stall 治理无内建**：dsh loop 层无 stall 计时器；现有 043/044 的 stall 恢复需求在 dsh 侧要落在自研 LLM 适配器（GLM Responses）或 around 层（§4.4）。
5. **MCP HTTP transport 的 supervisor 覆盖有限制**（若 saolei/memory 走 streamable-http）：请求级失败靠 SDK transport 自恢复，supervisor 不接管（§4.5 已知限制）。
6. **event-driven 回复的时序依赖**：官方模式"事件流 + idle 终止"对"回合完成判定"依赖 `agent/status` 顺序（wire 不变量有官方测试锚定，047 D3）；自研 loop 若改变 status 语义需保持该不变量（桥接/SDK 消费者的共同假设）。

### 7.3 对后续设计的输入与剩余待定项

已确认决策（2026-08-28，用户确认，见头部）：单 agent 双角色拓扑、agent-scoped 游戏状态、插件桥接形态。

设计基线输入：

- saolei-loop 依赖最小集与"继承清单"（§3.2、§4.7）可直接作为设计基线。
- 双角色阶段表达三手段与复盘/策略挂点映射（§5.3、§5.6）供 saolei-loop 设计参考。
- 扫雷游戏插件对外部依赖的异常处理建议参考 MCP supervisor 范式（§4.5）。

剩余待定项（saolei-loop / 基础插件 spec 阶段澄清）：

1. **桥接面统一与否**：049 的 web 对话面（gateway `/api/v2` → 宿主直连 gRPC）与 desktop 游戏面（插件桥接）是否统一为一个桥接插件（§5.4）。
2. **阶段状态机分权**：转移规则全在游戏插件，还是 loop 管回合节奏、游戏插件管规则判定——即 saolei-loop 与游戏插件的接口契约，亦决定两个 spec 的先后关系。
3. **隐藏信息的持久化形态**：雷布局走"落日志不投影模型"的自定义事件还是游戏插件自持（§5.5 推论 2）。
4. **差异化 model route**：planner/player 是否用不同模型/参数（§5.6）。

---

## 8. 引用来源汇总

仓库内（本地物化源码，0.1.1-rc.2 线）：

- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/`（lib/index.js 全量、lib/types/*.d.ts、package.json peers）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/`（Agent 接口、AgentFactory、Inbox、agent/* 事件、registry API）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent-spine-demo/lib/index.js`（17 项组装清单）
- `node_modules/.pnpm/@deepseek-ai+dsh-tools@0.1.1-rc.2_*/`（ToolRuntime、TOOL_RUNTIME_SCHEDULER、tools/* 事件）
- `node_modules/.pnpm/@deepseek-ai+dsh-llm-retry@0.1.1-rc.2_*/`（request-error 恢复点消费方）
- `node_modules/.pnpm/@deepseek-ai+dsh-session-persistence@0.1.1-rc.2_*/`（SessionPersistence Service Definition）
- `node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_*/`（SessionEventMap）
- `node_modules/.pnpm/@deepseek-ai+dsh-scope@0.1.1-rc.2_*/`（scoped-context 原语）
- `experimental/dsh/demo/agent/src/dsh.ts`、`experimental/dsh/demo/agent/src/session.ts`（B1 宿主桥接实证）
- `third_party/dsh/core/package.json`（core baseline 11 包）
- 前置调研：`survey/deepseek-harness-framework.md`、`survey/deepseek-harness-preset.md`、`survey/deepseek-harness-b1-plugin-packaging.md`、`survey/deepseek-harness-b1-bazel-packaging.md`、`survey/llm-stream-stall-recovery-revision.md`、`specs/047-dsh-chat-demo/research.md`、`specs/049-agent-v2-dsh-init/spec.md`

仓库外（dsh 官方仓库文档，均 master）：

- https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md
- https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/implemented/feature/2026-08-06-mcp-client-auto-reconnect.md
- https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/basic/index.md
- https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/service.md
- https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/develop/framework/events.md
