# Research: Game Agent v2 — dsh 迁移 Step 2

**Feature**: [spec.md](spec.md) | **日期**: 2026-08-31 | **阶段**: Phase 0

本文解决 spec Technical Context 的全部待定项与实现前技术决策。每项含 **Decision / Rationale / Alternatives considered**。信息源：spec 及其裁定（2026-08-31）、前置调研 `survey/deepseek-harness-agent-loop-prereq.md`（§ 引用均指该文）、049 交付物（`specs/049-agent-v2-dsh-init/`）、dsh 0.1.1-rc.2 物化源码（node_modules/.pnpm）、仓库现状代码（引用带 file:line）。

前置事实（调研已实证，本文直接引用）：

- loop 可替换：`AgentFactory` 在 `@deepseek-ai/dsh-agent` 公共面，`ctx.agents.setFactory` 替换，消费者零改动（调研 §2.1）。
- 官方 `dsh-agent-spine-demo` 硬挂载 `AgentLoop` 且 `AgentRegistry.setFactory` 不允许二次注册（spec Motivation）→ 直组核心件为必经路径。
- 插件可为插件提供能力（Service + inject，调研 §5.1）；事件四模式 + scope 过滤（§5.2）；per-session 状态以 agent-scoped 注册为 §5.5 判定的吻合形态（host Map 是跨会话统计场景下的可选退化）。
- dsh 的 preset（会话级组合内容）不含模型路由；模型是 per-agent 创建期选项（`AgentOptions {provider, model, maxTokens}`，`dsh-agent` `lib/types/runtime-types.d.ts`；注释原文 "Persona belongs to system-prompt sections"）。

---

## D1: 服务与资源命名（spec A3 落定）

**Decision**：
- `ConversationService` 更名 **`AgentService`**（package `projects.game.v2` 不变，`projects/game/agent_v2.proto` 重塑）。
- 资源与 RPC：
  - **Agent 单例**（AIP-156）：`templates/{template}/sessions/{session}/agent`；`UpdateAgent`（AIP-134 create-or-update，`allow_missing=true`，PATCH `/api/v2/{agent.name=templates/*/sessions/*/agent}`）、`GetAgent`（GET）。无 Create/Delete RPC。
  - **消息子资源标准 List**（AIP-132，049 FR-014 语义延续）：`ListAgentMessages`，`GET /api/v2/{parent=templates/*/sessions/*/agent}/messages`（替代 `ListHistory` 的 `:history` 自定义方法）。
  - **Send**：保留自定义方法（AIP-136）`POST /api/v2/{session=templates/*/sessions/*}:send`（server-streaming，事件模型见 D10）。
  - **Preset**（AIP-133/131/132/134/135 全标准方法）：`templates/{template}/presets/{preset}`，REST 在 `/api/v2/{parent=templates/*}/presets` 与 `/api/v2/{name=templates/*/presets/*}`。
  - **模型目录**：只读 `ListModels`，`GET /api/v2/models`（无父集合的部署级只读目录；输出 `Model{id, context_window}`）。
  - **`Dispose` 移除**、**`:refresh` 不引入**（spec Q2 追加裁定）。
- **DesktopBridgeService**（新，独立服务）：bidi `Connect(stream UserFrame) returns (stream TeamFrame)`，**帧类型复用 `projects/game/game.proto` 的 `UserFrame`/`TeamFrame`**（agent_v2.proto import game.proto）。无 REST 绑定（WS 由 gateway 自定义路径承载，对齐 v1 Connect 形态）。

**Rationale**：命名对齐 spec 提示（"如 AgentService"）；AIP 方法形状由 Q2 裁定直接推导；帧复用使 desktop 的 WS 客户端二进制 proto 编解码（`projects/game/desktop/internal/api/websocket.go:74-123`）与 gateway 的 wsStream 适配器（`projects/game/gateway/cmd/main.go:208-245`，含 URL→template/session 注入）几乎零改动——desktop 只改 URL 前缀（D8）；game.proto 保持不动（v1 代码保留仓库、session/memory 服务仍在用）。

**Alternatives**：v2 自有帧类型（`BridgeFrame`）——语义更纯但迫使 desktop/gateway 双端重写编解码，无收益；preset 建模为 template 无关的全局资源——与 v1 TeamProfile 的 template 父结构不对称，且 web 下拉按 template 过滤自然。

## D2: preset 持久化 = MongoDB

**Decision**：新 db **`game_agent_v2`**、collection **`presets`**；`_id` 由数据库自动生成（`style/mongo.md`：_id 不能被对象属性覆盖），`name`（完整资源名）字段建唯一索引——v1 prompt 服务 `team_profiles` 同构先例（`projects/game/prompt/runtime/mongo/repository.go:99-102`）。agent-v2 经 `mongodb` npm 驱动（catalog 已有 `^7.5.0`）连接，端点发现复用 `@dominion/common-js-resolver`（目标 `dominion:///game/mongo:27017`，与 Go 服务 `mongo.NewClient("game/mongo")` 同源），env `MONGO_URI` 可直连覆盖（测试/本地）。存储字段：`name`、`player_prompt`、`create_time`、`update_time`。

**Rationale**：(1) FR-005 要求重启不丢；(2) agent-v2 为 stateful 多实例部署（owner 亲和，049 data-model §2.9）——preset 是**非 session 作用域**的全局资源，若存本地文件/内存，经不同实例的 CRUD 会分裂，Mongo 是唯一满足多实例一致性的既有设施；(3) 部署已有 mongo 基础设施（`projects/game/deploy.yaml:6-12`），v1 prompt 服务同构先例（`game_prompt.team_profiles`，`projects/game/prompt/runtime/mongo/repository.go:17-22`）。

**Alternatives**：本地 JSON 文件——多实例分裂 + stateful 挂卷成本；复用 `game_prompt` db——v1 服务将下线，库归属应随服务走，避免跨服务共享 db。

## D3: preset persona → agent 的注入路径（dsh prompt 系统）

**Decision**：
- **`AgentOptions` 声明合并扩展 `persona?: string`**（dsh-agent 注释明示 "Merge-extensible agent creation options"）。宿主物化时经 `ctx.agents.create({sessionId, agentOptions: {provider, model, persona}})` 传入。
- **saolei-loop 的工厂**在创建 agent 时于 **`agent.ctx` 注册 agent-scoped `systemPrompt.section({name: "deployment:persona", order: 0, text})`**——agent-scoped 同名 section **shadow 全局**（dsh-system-prompt README "an agent-scoped persona shadows the global default"；`node_modules/.pnpm/@deepseek-ai+dsh-system-prompt@0.1.1-rc.2_*/…/README.md` Public API §`ctx.systemPrompt.section`）。text = persona 非空 ? persona : `DEFAULT_PLAYER_BASE`（v1 空值回退语义，源 `projects/game/agent/src/team/player.ts:79-85` 的中文 base 提示词，迁入 saolei-loop 常量）。
- **组合清单移除全局 `persona` 配置**（049 cordis.yml 的 spine persona 行随 spine 一起消失）；`includeHarnessIdentity: false`、`includeRuntimeContext: false` 延续。

**Rationale**：dsh 明确 "Persona belongs to system-prompt sections"——AgentOptions 无 persona 字段是设计而非疏漏；agent-scoped shadow 是官方提供的唯一 per-agent persona 机制；preset 内容在物化时固化进 agent（Edge case：preset 删除不影响已物化 agent——天然成立，因为 section text 是创建期快照）。**注意**：`UpdateAgent` 重新物化 = 销毁旧 agent + 创建新 agent（新 section），不依赖任何运行时改写 prompt 的机制。

**Alternatives**：宿主在 `handle.agent.ctx` 上自行注册 section——可行但把 fallback 语义散到宿主；`system-prompt/assemble` waterfall 改写——过度设计。

## D4: 模型目录的来源与同源校验

**Decision**：
- **`@dominion/dsh-llm-glm` 适配器实现 `listModels()` override**：返回 `config.models`（静态目录，无端点调用；`LlmAdapter.listModels` 基线 "advertise no models"，override 是官方预留的 selector-metadata 缝，dsh-llm README "Adapters" 节）。
- agent-v2 的 `ListModels` RPC 调 `ctx.llm.listModels("glm-responses")` → 映射 `Model{id, context_window}`。
- **默认模型** = `process.env.GLM_MODEL || "glm-5.2"`——与 cordis.yml 的 `models[0].id` **同一表达式同源**（`projects/game/agent_v2/cordis.yml:22`），目录必含默认值。
- `UpdateAgent` 物化校验：model 字段非空时必须在目录内，否则 `INVALID_ARGUMENT`（fail-fast，US2 场景 7）。

**Rationale**：spec 裁定"物化校验与目录同源（杜绝下拉可选、提交被拒分裂）"——单一来源（插件 config.models）经单一查询面（ctx.llm.listModels）导出，天然同源。049 合同将模型发现列为未做扩展点（`specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §7），本决策是对该缝的最小填充（静态返回，不实现 `discoverModels` 端点发现）。

**Alternatives**：宿主直接读 env 拼目录——绕过插件所有权，cordis.yml 与代码两处维护；实现真实 `discoverModels`——需要端点支持，超出需求。

## D5: 组合清单（FR-012：spine 单行 → 直组核心件 + saolei-loop）

**Decision**：`projects/game/agent_v2/cordis.yml` 重写为以下行（顺序仅表依赖分层，cordis 按 inject 等待，加载顺序无关紧要——spine 源码注释）：

| 行 | 包 | 角色 |
|---|---|---|
| timer | `@deepseek-ai/cordis-plugin-timer` | 纤维安全计时器（spine 首行镜像；llm-retry 等待依赖面） |
| llm | `@deepseek-ai/dsh-llm` | LlmRuntime（`ctx.llm`） |
| session | `@deepseek-ai/dsh-session` | SessionStore（事件日志） |
| system-prompt | `@deepseek-ai/dsh-system-prompt` | prompt 组装（config: `includeHarnessIdentity: false`、`includeRuntimeContext: false`） |
| tools | `@deepseek-ai/dsh-tools` | ToolRuntime（`ctx.tools`，native 模式默认） |
| agents | `@deepseek-ai/dsh-agent` | AgentRegistry（`ctx.agents`） |
| invariants | `@deepseek-ai/dsh-invariants` | 不变量注册表 |
| invariant-session / invariant-agent / invariant-scope | `@deepseek-ai/dsh-session/invariant` 等三个 subpath 导出 | 一致性防护伴生（见下） |
| llm-retry | `@deepseek-ai/dsh-llm-retry` | `agent/request-error` 恢复（调研 §4.4："挂官方 llm-retry 行即可"） |
| llm-glm | `@dominion/dsh-llm-glm` | GLM Responses 适配器（models 目录） |
| desktop-bridge | `@dominion/dsh-desktop-bridge` | flow 桥接（D8） |
| saolei-loop | `@dominion/dsh-saolei-loop` | loop + 游戏状态（D6） |
| saolei | `@dominion/dsh-saolei` | 工具 + prompt section（D7） |

**移除**（相对 spine 17 项）：SessionTitleService、SkillRegistry + SkillFileSystem、JobsLocal、shellEnv + toolBash、workspaceContext（agent-instructions）、toolSkill、toolJobs、Goal 三件套、**AgentLoop（官方 loop）**、agent-loop invariant 伴生。**不引入** dsh-session-persistence（A2：无 resume，冷恢复亦不需要）。

**invariant 伴生挂载方式**：三个伴生是包的 subpath export（`@deepseek-ai/dsh-session/invariant` 等，spine 源码 `import * as sessionInvariant from ...` 后 `ctx.plugin()`，`experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent-spine-demo/lib/index.js:1-24`）。cordis.yml 行名能否直接写 subpath 由 Loader（`@deepseek-ai/cordis-plugin-loader`，dsh-app-boot `lib/index.js:8`）解析决定——**实现期先试 subpath 行名**；若 Loader 拒绝，回退为一个本地 wrapper 包（`@dominion/dsh-core-invariants`，apply 内 `ctx.plugin()` 三个伴生）。两者均不改变行为。

**Rationale**：裁剪依据 = 调研 §3（loop 本体依赖面 7 必需 + spine 组装冗余分析）+ 本 feature 零 bash/skill/goal/jobs 需求；保留 invariants 是因为自研 loop 复用 session/agent/scope 基础设施，fail-loud 一致性断言在重写 loop 的高风险面有直接防护价值（调研 §4.6）。

**Alternatives**：保留 spine + 尝试禁用其 AgentLoop——spine 无禁用配置且 factory 禁止二次注册（spec Motivation 实证），不可行；完全不挂 invariants——省一行但失去重写区的免费防护。

## D6: saolei-loop 设计（FR-010/FR-011）

**Decision**：包 `@dominion/dsh-saolei-loop`（`common/js/dsh-plugins/saolei-loop/`）：

1. **Service 插件** `name: "saolei-loop"`，`inject = ["agents", "sessions", "llm", "tools", "systemPrompt", "desktopBridge"]`（官方 AgentLoop 的 5 必需 + 桥接服务；对齐调研 §3.1 层 2）。构造时 `ctx.agents.setFactory(this)`。
2. **`AgentOptions` 扩展 `persona`**（D3）。
3. **驱动器 `SaoleiLoopAgent implements Agent`**（dsh-agent `Agent` 接口：followup/steer/inject/cancel/whenIdle/runMaintenance/status/inbox/options）——turn/step 状态机**按官方 `ReactLoopAgent` 模式重写**（"抄设计"非继承代码，A8；物化源码 `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/…/lib/index.js`），必继承调研 §4.7 八条：phase 状态机与广播、abort 检查点布局（每 chunk/step/turn）、中断流部分内容 `interrupted: true` 落日志、abort 后唤醒重定向（wakingAfterAbort + wake latch）、四个决策点 waterfall/serial 语义（`agent/pre-step`/`agent/request`/`agent/request-error`/`agent/turn-stopping`）、driver containment（kick catch-all + `agent/error` 先发后抛）、工厂所有权（插件卸载全量 abort + 静默等待）、事件面/Inbox 复用（dsh-agent 提供，durable splice 自动获得）。
4. **游戏状态 = agent-scoped 服务注册**（调研 §5.5 判定的吻合形态）：工厂 `prepare()` 在 agent 发布前，将 GameRuntime 以 **Service class 形态注册为该 agent scope 的 `saoleiGame` 服务**（`new ...(agent.ctx, "saoleiGame")`；cordis `Service` 契约："Register this instance as `name` in the current context … the service is unregistered automatically when the owning fiber unloads"——**随 agent scope 卸载自动注销，无手动清理路径**）。**不采用 host 级 `Map<SessionId, GameRuntime>` 注册表**：host Map 把 per-agent 状态放进全局可达面（宿主任意代码都能按 agent id 反查任何 runtime）、需要手动维护 scope 事件与表项的一致性（`agent/disposed`/`session/disposed` 监听删除）；host 级注册表仅当未来需要跨会话统计（胜率/局数）时再引入（调研 §5.5"host Map 退化为可选优化"）。
5. **GameRuntime**（每 session）：持有 `recognized`/`initState`/`operationCount`（游戏状态）与 `gameLog`/`gameEvent`（游戏历史，v1 `EphemeralGameBuffer`/`SaoleiEventSink` 语义，`projects/game/agent/src/team/team-sink.ts:92-169`）；API `init(signal)` / `operate(ops, signal)` / `remain()` → 返回 v1 契约文本（outcome 行 + `game status:` 行 + 标尺棋盘，源契约 `projects/game/agent/src/skill/saolei/SKILL.md` §Tool-result body shape 与 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:601-757` 的文本构造器——**迁移语义不重写契约**）；操作下发经 `ctx.desktopBridge.dispatch(sessionName, part, signal)`；识别经 `@dominion/game-saolei-board`（FR-015：`SaoleiBoard.init/updateFromScreenshot`，截图空间识别 + client 空间下发的坐标纪律，`projects/game/agent/src/mcp/saolei/geometry.ts` 常量随迁）。拒绝码三元组/批量 triage（SKIP/STOP）/counter-informed win 判定全部按 v1 `saolei-mcp.ts:241-541` 语义移植。
6. **物化配置校验**：工厂对 `agentOptions.provider/model` 不做目录校验（目录校验在宿主 UpdateAgent，D4）；persona 空回退 DEFAULT_PLAYER_BASE（D3）。

**Rationale**：spec FR-010/011/013 的直接映射；"loop 即游戏控制点"由 GameRuntime 归属 loop 插件实现；宿主 `AgentSessions`（`projects/game/agent_v2/src/session.ts`）面向 `ctx.agents` 编程零改动（调研 §2.1 承诺）——049 对话行为零回归的结构保证。

**Alternatives**：保留官方 loop + 独立游戏插件持有状态——被 spec FR-010 否决（且工具经 loop 下发的控制点要求状态与 loop 同生命周期）；host 级服务 + `Map<SessionId, GameRuntime>`（`for(agent)` 按 id 查找 + dispose 监听删除）——状态同样按 session 隔离，但注册表面全局、生命周期手动维护，仅在需要 host 级跨会话统计时有价值。

## D7: saolei 工具插件的注册形态（FR-013/FR-014）

**Decision**：包 `@dominion/dsh-saolei`（`common/js/dsh-plugins/saolei/`）：`name: "saolei"`，`inject = ["tools", "systemPrompt"]`——`saoleiGame` 是 agent-scoped 服务（插件加载时不存在任何 agent），**不能静态 inject**，工具执行期经调用者 agent 的 scope 惰性解析（见下）。

1. **全局注册三工具**（`ctx.tools.register(defineTool(...))`）：`saolei_init`（无参）、`saolei_operate`（双形式参数：single `type/x/y` 或 `operations[]`，互斥校验文本按 v1 `AMBIGUOUS_ARGS_TEXT` 等字面量）、`saolei_remain`（无参）。**exec 体内经 `exec.agent.ctx` 解析该 agent scope 内注册的 `saoleiGame` 服务（GameRuntime 实例）**（`ToolExecution` 携带 `agent?`——"the pending call (name, parsed arguments, caller agent)"，dsh-tools `lib/types/index.d.ts:192-200`；`Agent.ctx` 是公开的 agent-scoped context，服务经声明合并按名访问、cordis reflect 按作用域链解析——宿主/根上下文上看不到任何 `saoleiGame`；`exec.agent` 缺失 = 非 loop 驱动的调用，fail-loud），转发到 `runtime.init/operate/remain`。工具自身无状态（FR-013"工具是无状态面向模型的人口"）。
2. **output 声明**：`{result: string}` JSON schema（render 即文本棋盘结果）；工具失败（desktop 缺席等）按 dsh 语义抛错 → `isError: true` 的模型可见失败（不伪造成功，调研 §4.3）——v1 的"拒绝是正常结果、桥接失败是 FAILED 结果"语义映射：游戏规则拒绝（no_active_game 等）是**正常结果文本**（`rejected: <reason>` 行）；desktop 缺席/断开是 runtime 返回的 FAILED `OperationResult` → 转为**错误结果**（US1 场景 4"明确的可读错误结果"）。两者区分见 data-model §GameRuntime。
3. **prompt section**：`ctx.systemPrompt.section({name: "saolei:guidance", order: 100, text})`——内容迁移 `projects/game/agent/src/skill/saolei/SKILL.md` 的工具使用守则（符号表/坐标约定/三层结果体/校验规则/示例流/禁用项），措辞从 "MCP 工具" 调整为插件工具语义；order 落在官方 tool-guidance 频带 100–199（dsh-system-prompt README "Order bands"）。**不保留 skill 文件形态**（FR-014：插件自注册 prompt section，随插件启停生效）。

**Rationale**：工具**目录**全局注册（一次注册、全体 agent 可见——官方 tool 插件形态）+ **状态解析走调用者 agent scope**（saolei-loop 在该 scope 注册的服务）——"saolei 建立在 saolei-loop 之上"的机制表达 = workspace 包依赖（类型）+ 服务名契约 + 作用域解析；prompt section 与工具同插件交付满足 dsh 所有权原则（"tool packages own their cross-call guidance"）。

**Alternatives**：loop 工厂在 agent.ctx 逐 agent 注册工具——工具归属 loop，与 FR-013 的插件分工冲突；MCP server 形态（dsh-mcp-client 挂载）——引入 stdio/http 进程与 schema 转换，v1 的 MCP 层正是被本 feature 拆除的对象（spec 2.3）。

## D8: desktop 桥接插件与链路（FR-009，Q3 裁定 A）

**Decision**：包 `@dominion/dsh-desktop-bridge`（`common/js/dsh-plugins/desktop-bridge/`）：`name: "desktop-bridge"`，无 inject（自足）。

1. **gRPC 面**：插件提供 `DesktopBridgeService` 的 handler 实现（`Connect` bidi）；**宿主 `server.ts` 在既有 50051 单 gRPC server 上同时注册 `AgentService` 与 `DesktopBridgeService`**（service.yaml 单 grpc 端口不变，proxy 单端口可达双服务）。插件服务 `ctx.desktopBridge`：
   - `connect(sessionName, bidiStream)`：首个 `UserFrame` 的 `template_id/session_id` 为绑定键（gateway 从 URL 注入，v1 语义）；**新连接接管**（同 session 旧连接关闭，v1 基线）；StatusSignal 探测帧回应（desktop `App.Connect` 的应用层探测，`projects/game/desktop/app.go:1674-1710`）。
   - `dispatch(sessionName, part, signal): Promise<OperationResult>`：铸造 UUID `tool_id` 盖入 FlowPart（v1 OperationBridge 语义，`projects/game/agent/src/operation-bridge.ts:232-233`）、下行 `TeamFrame{flowParts}`、按 `tool_id` 匹配 `FlowResultPart` 回执；**无连接 → FAILED "desktop disconnected"**；超时 backstop 延续 v1 `DISPATCH_TIMEOUT_MS`（20 分钟，确认抽屉 15 分钟自动放行优先）；连接对象不持流引用只持写回调（断线重连不丢 in-flight，v1 `:10-12` 语义）。
   - **与对话流独立**：桥接是独立 WS 连接 + 独立 gRPC 流，任一断开不影响另一条（构造性独立，US1 场景 6）。
2. **链路**：desktop → gateway **新 WS 入口 `/api/v2/templates/{template}/sessions/{session}/connect`**（复用 v1 wsStream 适配器模式：binary proto 帧 + URL 身份注入，`projects/game/gateway/cmd/main.go:208-245` 移植）→ proxy（`DesktopBridgeService.Connect` 定向转发，owner 亲和 get-or-create）→ agent-v2 桥接插件。desktop `GatewayURL` 配置零改动（FR-009），仅 URL 路径前缀 `/api/v1` → `/api/v2`（`projects/game/desktop/internal/api/websocket.go:42-44`）。

**Rationale**：Q3 裁定 A 的完整复刻；帧/探测/接管/确认语义全部延续 v1（desktop 执行端零语义变化）；插件桥接形态由调研 §5.4 判定（"插件 → 桥接"方向只有插件桥接可原生承载）。

**Alternatives**：统一桥接（对话 + flow 一个 gRPC 面）——049 对话面是宿主直连 gRPC（server.ts），统一需把对话面迁入插件且 gateway 双协议耦合，调研 §7.3 待定项 1 的收敛方案被 spec FR-009 的"两流独立"要求反推为分离面更稳；desktop 直连 agent-v2——绕过 owner 亲和，多实例下无法定位 session 归属实例。

## D9: 路由分工与 owner 分配点变化（FR-007/FR-019；Directive 2026-09-01 意见 4 修订）

**Decision**：
- **移除**：`TeamHandler`（TeamService 转发面）、v1 agentclient manager、`agent_owners` collection 使用（代码与部署移除；mongo 旧集合不迁移不清理——v1 链路整体下线）。
- **`ConversationHandler` → `AgentHandler` implements `game.AgentServiceServer`**（`projects/game/proxy/handler/conversation.go` 演进）：proxy 只承载**会话面**（agent/队列/游戏状态在进程内存，必须 owner 亲和）：
  - **owner 亲和 RPC**（既有 `agent_v2_owners` 池，get-or-create 语义复用 `assignConversationOwner`）：`UpdateAgent`（**成为 owner 分配点**——物化即落 owner，desktop 先连/先物化均成立）、`GetAgent`、`ListAgentMessages`、`Send`（**不再分配**：无 owner → `NOT_FOUND`，对齐"未物化 Send 明确报错"；有 owner 但实例内未物化（如重启后）→ agent-v2 返回 `FAILED_PRECONDITION`，两级错误语义在 contracts/agent-api.md §错误分层 定义）。
  - `DesktopBridgeHandler` implements `game.DesktopBridgeServiceServer`：`Connect` bidi——首帧身份解析（gateway 注入）→ get-or-create owner → `bind.WithFirstFrame` + bidi pump（v1 `handler.go:179-226` 模式）。
- **`PresetService` 拆分 + gateway 直连**（2026-09-01 用户指令，[revisions/directive-2026-09-01.md](revisions/directive-2026-09-01.md) §3）：preset CRUD 与 `ListModels` 是**无状态配置面**（preset 状态在 Mongo `game_agent_v2.presets`，D2 已论证多实例一致性；模型目录为静态插件配置）——自 `AgentService` 拆出独立 gRPC 服务（同宿主 agent-v2 进程 50051），**不经 proxy**：gateway 以 `presetConn` 直连 agent-v2（`gameconst.AgentV2Target` 服务发现，resolver 返回全部 ready 端点，gRPC 客户端 LB 覆盖多实例；无 session 亲和诉求，任意活实例正确服务）。
- **gateway**：移除 TeamService/PromptService handler 注册与 v1 WS 分支；`/api/v1/` 子树只剩 session+memory 的 gwmux；`/api/v2/` 子树 = gwmux（teamConn 上的会话面 AgentService HTTP + presetConn 直连的 PresetService HTTP，路径集不相交）+ 新 WS connect 路径分支；teamConn 不变名只换注册面。

**Rationale**：Update 分配 owner 是 Send 去懒物化后的必然推论（否则首次 Send 无路由目标）；preset/models 面不设任何实例选择机制——按**状态归属**拆服务（directive §3.1）：有状态会话面经 proxy 亲和，无状态配置面直连，proxy 转发层对无状态 RPC 是纯开销；v1 owner 池保留给 memory？——否，memory 服务的 gateway 路由是 gateway→memory 直连（`memoryConn`，`projects/game/gateway/cmd/main.go:54-101`），不经 proxy，v1 owner 池随 TeamHandler 一起消失。

**Alternatives**：
- preset 也走 owner 分配（按 template 键）——为无状态 RPC 强造粘性，实例滚动时反而不必要地失效。
- preset/models 经 proxy 无亲和稳定哈希转发（Directive 2026-09-01 前的设计，**被否决**）——为无状态 RPC 强造 proxy 中转层：proxy 需维护 `affinityFreeConn`/请求派生键哈希整套机制只为绕过自己；gateway 直连后该面整体移除（directive §0 意见 4、§3.6）。

## D10: ChatEvent 事件模型扩展（US1 工具调用块的端到端兑现）

**Decision**：
1. **新增 `ToolResultEvent tool_result = 16`**（ChatEvent oneof 扩展）：`{tool_id, status(SUCCEEDED|FAILED), result(string)}`。来源：loop 追加的 `tool/result` session 事件 → TurnCollector 映射（`isError` → FAILED）。web store 按 `tool_id` 关联到既有 ToolCallBlock 更新状态与结果（A7"按 tool_id 关联呈现"）。
2. **回合全局 block index**：dsh 的 chunk `index` 是 **per-step**（每次模型请求从 0 递增）——带工具的 turn 含多个 step，index 会重叠。TurnCollector 维护 per-turn 的 step-local → 全局 index 映射（`step/start` session 事件或 `assistant/message` 边界重置 step-local 表），保证 ChatEvent 的 `index` 在**回合内全局单调**（049 contract §3"one block_start per block with aligned index"不变量在多 step 下仍成立）。
3. **历史投影**：`assistant/message`（每 step 一条）→ HistoryMessage{AGENT, blocks}；其中 ToolCallBlock 初始 RUNNING；`tool/result` 到达后按 `tool_id` 回填同块的 `status/result`（历史与回填一致，A7/FR-007）。

**Rationale**：049 的 ChatEvent/ToolCallBlock 模型预留了 tool 渲染（`TOOL_STATUS_RUNNING` 注释 "No terminal status source in this phase"，`projects/game/agent_v2/src/history.ts:117-128`）但无结果通道——本决策补上唯一缺口（一个事件 + 一个 join 键），事件序不变量（049 contracts/conversation-api.md §3）全部延续。

**Alternatives**：result 并入 block_end——工具执行在 assistant 消息**之后**，block_end 时无结果可用；web 轮询 GetAgent——流式语义倒退。

## D11: GLM 适配器的工具序列化扩展（US1 的模型面前提）

**Decision**：`@dominion/dsh-llm-glm` 序列化器（`common/js/dsh-plugins/llm-glm/src/serialize.ts`）扩展 Responses `input` 映射（049 合同 §4 表中 "UNSUPPORTED_CONTENT" 两行的解除）：
- assistant `tool-call` 块 → `{type: "function_call", call_id, name, arguments}` output item；
- `tool-result` 消息 → `{type: "function_call_output", call_id, output: <rendered text>}` item；
- 依据 OpenAI Responses 官方 input item 形状（[openai-openapi responses](https://github.com/openai/openai-openapi)）；流式侧（`function_call_arguments.delta` 等事件映射）049 wire.ts 已预留，无需变更。

**Rationale**：无工具回传则第二 step 请求无法携带工具结果——US1 的硬前提；`finish{kind:'tool-calls'}` 分支 049 已实现（glm-llm-plugin.md §5 表 `response.completed` 行）。

**Alternatives**：无（必须扩展；范围限定在既有 fail-fast 占位的解除）。

## D12: desktop 退化边界（FR-016/FR-017/FR-018，A4）

**Decision**：
- **移除**：SessionList 的管理操作（新建/删除/刷新按钮，保留**只读列表 + 选择**）、ProfileManagement.svelte、ProfileSelectDialog.svelte、ChatView/ChatMessage/ScreenshotModal 的对话展示、chatstream SSE 子系统（`internal/chatstream/` + `chat-stream.ts`/`stream-merge.ts`/`chat-fifo.ts`——仅为对话 UI 供给而存在）、Go 侧 team/profile/message 绑定（GetTeam/UpdateTeam/RefreshTeam/ListMessages/List/Create/Get/Delete/UpdateTeamProfile，`projects/game/desktop/app.go:1039-1471`）、session CRUD 写操作绑定（Create/Delete）。GetSession 绑定一并移除——其前端零消费，只读选择的数据源仅 ListSessions（保留面见 [web-frontend.md](web-frontend.md) §5）。
- **保留并延续**：WSClient + Connect 探测 + readLoop + `handleInboundOperation`/`executeAgentOperation` + hold/确认抽屉（`OperationConfirmDrawer.svelte`）+ debug 模式、ListWindows/SetSelectedWindow/截图、config（GatewayURL/Env/Template）+ 日志查看。
- **改向**：连接 URL `/api/v1/.../connect` → `/api/v2/.../connect`（D8）；session 选择为只读（A4：列表或输入，指定连接目标，无任何管理操作）。

**Rationale**：FR-016/017 逐条映射；chatstream 移除依据 = 其唯一消费者（对话 UI）被移除（`projects/game/desktop/main.go:30-55` 注册链）；session 只读列表的数据源 = gateway 保留的 `/api/v1` SessionService List（`projects/game/game.proto:31-33`）。

**Alternatives**：保留 chatstream 供"日志"用——desktop 的执行日志已有 applog/LogPanel，不依赖 chat 流。

## D13: web 结构（US2/US4 + FR-008）

**Decision**：
- **侧栏四项**（FR-001..004）：`.sidebar-title` 加 `white-space: nowrap`（+ header 布局收紧）；新建/刷新改 dsh primitives 图标按钮（加号/圆环箭头 + aria-label/tooltip，含 loading 禁用态延续）；删除入口 = 每 session 条目右侧 `···` 按钮弹出菜单（菜单含"删除"）；长名 = 右侧渐隐遮罩（`mask-image`/伪元素渐变）+ 悬停横向滚动（`overflow-x: auto` on hover、隐藏滚动条样式）+ 移出复位（`scrollLeft` 归零）。
- **删除编排**：去掉 dispose 跳（FR-007），仅 `DELETE /api/v1/{name}`（`projects/game/web/frontend/src/App.tsx:142-178` 简化）。
- **preset 管理视图**（新）：侧栏加 "Presets" 导航切换；列表 + 新建/编辑（表单：名称 + player_prompt 多行文本）/删除（带确认）。
- **agent 物化面板**（新）：ChatPanel 顶部"设置 agent"入口 → 面板（preset 下拉 = ListPresets、model 下拉 = ListModels + 默认项、Apply = UpdateAgent）；未物化 session 的对话页引导物化（Send 的 FAILED_PRECONDITION/NOT_FOUND 错误或 GetAgent 404 触发引导态，US2 场景 5）。
- **api/store**：`api/agent.ts`（preset CRUD/models/UpdateAgent/GetAgent）、`listHistory` → `GET /api/v2/{parent}/agent/messages`、ChatEvent `tool_result` 处理（store reducer 按 tool_id 更新 ToolCallBlock；回填同规则）。

**Rationale**：延续 049 单页视图切换架构（无 router，`projects/game/web/frontend/src/App.tsx:89-221`）；组件测试基线（SessionList.test.tsx 等）随之扩展。

**Alternatives**：引入 router——两视图规模不值得；物化入口放侧栏——物化是 session 级操作，入口应在对话上下文内。

## D14: v1 处置与 testplan 重组（FR-019，SC-005）

**Decision**：
- **deploy.yaml**：移除 `prompt` 与 `agent`（v1）条目及 v1 secret 引用；其余（mongo/session/proxy/memory/agent-v2/web/gateway）保留。
- **testplan suites**（`projects/game/testplan/system_test.yaml`，现状 session 与 planner-memory 均部署自 deploy_agent.yaml）：**移除** agent-dialog、agent-queue、checkpoint-resume、concurrent-serialization、agent-multimodal、agent-operation、agent-saolei、saolei-team、agent-stall（v1 驱动面）；**session 与 memory 套件迁至扩展后的 v2 测试部署**（session 服务与 `/api/v1` session/memory 路由仍在产线，需要回归面；memory 套件仅保留 gateway 路由 CRUD 用例，v1 agent 驱动的 planner/memory 工具流用例随 v1 下线移除）；**agent-v2-conversation 扩展**为 051 全量（见 D16）。
- **测试部署** `deploy_agent_v2.yaml`：+ mongo（preset 持久化）+ memory（memory 套件）+ fake-desktop（D15）；`deploy_agent.yaml`/`deploy_agent_stall.yaml` 及 v1 专属 case 二进制（agent_dialog_test 等）随 suites 一并移除（代码删除，非仅摘除引用——终态原则 VII）。

**Rationale**：SC-005 处置清单的测试面闭环；v1 用例在 v1 面移除后无法部署执行，保留即虚假覆盖。

**Alternatives**：保留 v1 suites 于独立 deploy——与"自 game 部署移除"裁定冲突，维护双拓扑成本无收益。

## D15: fake desktop 执行器（A9）

**Decision**：新 Go 测试设施 `projects/game/fake-desktop/`（与 fake-llm 同级同构：stateless http/ws 服务 + service.yaml）：
- 作为 WS 客户端连接 gateway `/api/v2/.../connect`（可配置 session/GatewayURL，读 testtool 端点注入）；
- 收 `FlowPart` 操作请求 → **确定性执行**：按操作类型回传 `FlowResultPart{status: SUCCEEDED}` + **可识别截图**——截图复用 `saolei_fixtures_test.go` 的嵌入式 PNG 思路（`projects/game/testplan/saolei_fixtures_test.go` + BUILD.bazel:16-26），按"当前局状态"返回对应棋盘图（内部维护确定性棋盘模型：init=F2 → 初始棋盘；click/flag/chord → 更新模型 → 渲染 PNG——用 `@dominion/game-saolei-board` 的对偶渲染或预生成图集映射）；
- 支持注入故障分支：断连（指定操作后关闭）、不回截图（识别失败路径）、FAILED 结果。
- 不进生产部署（service.yaml 仅被 testplan deploy 引用）。

**Rationale**：FR-020/A9 直接映射；确定性 = 大型测试零外部依赖 + 可断言终局（won/lost 由 fake 棋盘模型决定）。**实现取舍留给 tasks**：图集映射（有限状态枚举）优先于运行时渲染（简单、确定性更强）。

**Alternatives**：真 desktop 冒烟——Windows 依赖进 CI 不可行（spec 允许"或真 desktop 冒烟记录"，fake 为主）；fake 直接连 proxy gRPC——绕过 gateway WS 面，覆盖不全。

## D16: 大型测试拓扑与验收（FR-020）

**Decision**：`deploy_agent_v2.yaml` 扩展后拓扑：mongo + session + memory + fake-llm + fake-desktop + proxy + agent-v2-test（零 secret + `GLM_LLM_TARGET`）+ web + gateway（`/api/v1/` + `/api/v2/`）。suites：
- **agent-v2-conversation**（049 用例零回归：流式/多轮/排队/回填——经更名后 API；`web_test` 并入）；
- **agent-v2-preset**（新）：preset CRUD/持久化（重启 agent-v2 实例后 preset 仍在）/物化与模型选择/Update 刷新语义（记忆清空）/未物化 Send 拒绝/未知模型拒绝（US2 全场景）；
- **agent-v2-game**（新）：US1 全场景——工具链路（fake-llm 模板驱动 saolei_init/operate/remain 调用链）、棋盘反馈文本契约、终局与终局后拒绝、desktop 缺席/中途断连的异常分支、多 session 隔离、**两流独立性**（对话流与 flow 流任一断开另一条不受影响）；
- **desktop-flow**（新）：fake-desktop 视角——连接/探测/接管（二次连接关闭首个）/操作回执/确认语义（如 fake 实现确认协议则覆盖，否则记录冒烟豁免说明）；
- **session**、**memory**（迁移入）。
- fake-llm 新增 `agent_v2_saolei*.yaml` 模板：user 关键词触发 `tool_call: saolei_init` → `tools:` 规则匹配 `saolei_init` 结果（含 "new game started"）→ 回文本/下一批 tool_call（operate 批量）→ 终局文本（`game status: won`）——多 step 链（`ToolConfig.match_result_contains` 机制，`projects/game/fake-llm/service/message_types.go:130-148`）。
- **验收 = `guitar run projects/game/testplan/system_test.yaml` 全量通过**（部署→测试→清理闭环、全部用例 green、零 failed/flaky——constitution 原则 VI）。

**Rationale**：SC-001/002 的用例化；049 既有 agent_v2 测试基建（`agent_v2_helpers_test.go` 448 行）是扩展基线。

**Alternatives**：分多个 plan 文件——单 system_test.yaml 延续现状，套件串行即隔离。

---

## 汇总：NEEDS CLARIFICATION 清零核对

| spec 待定项 | 决策 |
|---|---|
| A3 服务/资源/proto 组织命名 | D1（AgentService + DesktopBridgeService，单 proto 文件延续） |
| preset 存储 | D2（Mongo `game_agent_v2.presets`） |
| persona 注入机制 | D3（AgentOptions 扩展 + agent-scoped section shadow） |
| 模型目录来源 | D4（glm 适配器 listModels → ctx.llm.listModels） |
| 组合清单构成 | D5（直组 13 行 + invariant 伴生挂载双案） |
| ChatEvent 工具结果通道 | D10（tool_result 事件 + 全局 index + tool_id join） |
| GLM 工具序列化 | D11（function_call/output 回传） |
| proxy 路由语义 | D9（会话面 owner 亲和——Update 分配 owner；配置面 gateway 直连；bridge get-or-create） |
| desktop 移除/保留边界 | D12 |
| web 结构 | D13 |
| v1/testplan 处置 | D14 |
| fake desktop | D15 |
| 大型测试 | D16 |

无遗留 NEEDS CLARIFICATION。
