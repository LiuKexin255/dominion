# Research: LLM 请求发送可靠性修复与 opencode-go 模型接入

**Feature**: [spec.md](spec.md) | **Date**: 2026-09-12 | **分支**: `063-llm-reliability-opencode-go`

研究基础：spec 撰写/clarify 阶段已完成的源码级调研（spec.md Motivation 与 Clarifications），本文档固化设计决策。证据源：本地物化 dsh 家族 0.1.1-rc.2（`node_modules/.pnpm/`）、agent_v2/saolei-loop/llm-glm 仓库源码、fake-llm 与 testplan 设施探索、opencode-go 官方文档（https://opencode.ai/docs/zh-cn/go/ ）。

## D1: llm-glm 失败码迁移到 dsh 共享失败码分类学

**Decision**: llm-glm 适配器抛出的 `LlmError` 与带内失败全部采用 dsh 共享分类学码：`TRANSPORT`（连接建立/流读取失败）、`AUTH`（401/403）、`RATE_LIMIT`（429）、`SERVER`（5xx）、`INVALID_REQUEST`（400 且非超上下文）、`CONTEXT_WINDOW_EXCEEDED`（400 + 错误体措辞分类）、`QUOTA`（任意状态 + 错误体措辞分类）、`HTTP_<status>`（其余 4xx）、`EMPTY_RESPONSE`（响应无 body）、`STREAM_CLOSED`（流结束无终局事件）、`MALFORMED_RESPONSE`（SSE 载荷非法 JSON）。`GLM_*` 前缀码完全移除（宪章 VII 终态）。带内 provider 失败（`response.failed`/`error` 事件）保持 provider 原码（经 `finish{error}` 传递，llm-retry 按码路由）。

**Rationale**: 默认可重试集合 `[EMPTY_RESPONSE, RATE_LIMIT, SERVER, TIMEOUT, TRANSPORT]` 以共享码为键（`node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-llm/lib/index.js:356-365`）；官方 deepseek 适配器即此映射（同文件 deepseek `lib/index.js:1308-1324`）。码对齐后 llm-retry 零配置生效。

**Alternatives**: 保留 `GLM_*` 码并在 `providerRetryPolicy.retryableCodes` 里声明（拒绝：双分类学并存，日志/重试/呈现消费方分裂，且 `GLM_HTTP_429` 这类细码无法表达"限流可重试、认证不可"的类别语义）。

## D2: 重试经既有 llm-retry 扩展点执行，插件声明 providerRetryPolicy

**Decision**: 不新建重试机制。llm-glm 与新插件 `Config` 增加可选 `retryPolicy`（dsh `RetryPolicySchema` 透传），适配器 override `providerRetryPolicy()` 返回解析结果（官方 deepseek 模式，`node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_*/lib/index.js:1346-1348`）；未配置时回退 dsh 默认（normal、5 次、500ms→10s、抖动 0.1）。重试的 durable 事件（`llm/retry`/`llm/retry-started`）由 llm-retry 插件既有产出（`node_modules/.pnpm/@deepseek-ai+dsh-llm-retry@0.1.1-rc.2_*/lib/index.js:97-115`）。

**Rationale**: llm-retry 已组合（`projects/game/agent_v2/cordis.yml:40`）且实现 turn 语义内恢复（fresh numbered turn、cancellable backoff、Retry-After 尊重，`:118-155`）；根因只是码不命中（spec Motivation §1）。

**Alternatives**: 适配器内部自建 fetch 重试（拒绝：绕过 durable retry chain，用户可见语义与 session 事件不一致，重复造轮子）。

## D3: 429 的 Retry-After 解析

**Decision**: 非 2xx 响应先读 `Retry-After` 头（秒数或 HTTP 日期，官方 `providerRetryAfterMs` 同算法，deepseek `lib/index.js:1289-1297`），以 `LlmError` options `providerRetryAfterMs` 携带；llm-retry 已按其退避（不超过 `maxDelayMs` 则用服务端值，超出且 normal 模式则放弃重试，retry `lib/index.js:142-150`）。

**Rationale**: 头部不含凭据，零泄漏约束不受影响；服务端指示优于本地指数退避。

**Alternatives**: 仅本地退避（拒绝：SC 场景 4 明确要求尊重服务端指示）。

## D4: 错误体仅用于分类、绝不回显

**Decision**: 非 2xx 时解析响应体 JSON 的 `error.{code,type,message}`，仅作为 `isQuotaExceededError`/`isContextWindowExceededError`（`@deepseek-ai/dsh-llm` 导出）分类输入；`LlmError.message` 保持现状式稳定文本（如 `GLM endpoint returned HTTP 429`）+ 状态码，不包含任何响应体文本。原始体仅进 `cause`（Error 链，不进用户可见消息/日志正文）。

**Rationale**: `specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md` §3 义务与 `specs/063-llm-reliability-opencode-go/spec.md` SC-003 / `specs/049-agent-v2-dsh-init/spec.md` SC-004 零泄漏：中间代理可能在错误体中反射请求头（含 Authorization）。分类需要（QUOTA 不可重试、CONTEXT_WINDOW 不可重试，spec SC-005 要求配额零重试）与零泄漏通过"只读不回显"同时满足；官方 deepseek 亦将原始体放 `cause` 而非 message（`lib/index.js:1559-1560`）。

**Alternatives**: 完全不读体（拒绝：429-配额与 429-限流无法区分，SC-005"配额同类重试 0 次"不可达成）。

## D5: 流停滞看护为 adapter 级 idleWatchdog

**Decision**: llm-glm 与新插件引入 `@deepseek-ai/dsh-timeout` 的 `idleWatchdog`：以消费信号包裹整个"fetch + body 读取"迭代，任一次 read 超过 `streamIdleTimeoutMs` 未完成 → 终止流并抛 `LlmError(..., "TIMEOUT")`（可重试）。`streamIdleTimeoutMs` 进插件 Config，默认 300000（官方同值，deepseek `lib/index.js:1150-1151, 1403`）；SSE comment 帧作为活动 pulse——wire 的 eventsource-parser 构造传入 `onComment` 回调（`common/js/dsh-plugins/llm-glm/src/wire.ts:195-196` 的 `createParser` 构造现未透传，需加）。cordis 行以 `!!js process.env.GLM_STREAM_IDLE_TIMEOUT_MS || 300000` 形式暴露给测试。

**Rationale**: 官方先例即 adapter 级（每 read 界限 + comment pulse，`deepseek lib/index.js:1386-1434`）；agent_v2 v2 栈当前无任何停滞检测（043/044 机制随 v1 移除）。默认 300s 而非 043 的 30s：044 校准的教训（推理模型正常深思考静默可 >65s，30s 为全行业最激进值，`specs/044-llm-stall-recovery-fix/spec.md` Problem 1）。

**Alternatives**: 编排器/会话级看护（拒绝：停滞发生在 agent-loop 内部流，上层只能看到"未 idle"，无法区分停滞与长思考，且要重建 043 的整套误判防线）；默认 120s（拒绝：官方先例 300s 已含推理容忍，且更保守）。

## D6: 空补全分类为 EMPTY_RESPONSE

**Decision**: 终局事件到达但零内容块（Responses `response.completed`/`incomplete` 无任何 block-start；chat `[DONE]` 无任何块）→ `finish{kind:"error", failure:{code:"EMPTY_RESPONSE", message:...}}`，不产出成功空回合。官方 deepseek 同语义（`deepseek lib/index.js:997-1007`：`stop` 且零块 → EMPTY_RESPONSE error finish）。

**Rationale**: `EMPTY_RESPONSE` 在默认可重试集合内（`dsh-llm/lib/index.js:246-253` 注释：degenerate completion，attempt produced nothing durable，safe to repeat）；成功空回合会静默结束 turn，用户与 loop 均无物可行动（spec FR-007）。

**Alternatives**: 保持现状成功空回合（拒绝：spec FR-007 明令禁止）。

## D7: 消费方停止时的传输清理

**Decision**: 适配器 `stream()` 以独立 `AbortController` 贯穿 fetch 与 body 读取；生成器 `finally`（消费方 `return()`/提前退出/异常）执行 `controller.abort()` + `reader.cancel()`，再 `releaseLock()`（官方 consumer-abort 先例，`deepseek lib/index.js:1402, 1422-1427`）。正常耗尽路径同样走 finally，abort 幂等。

**Rationale**: 现状仅 `releaseLock`（`common/js/dsh-plugins/llm-glm/src/adapter.ts:189-191`），body 未取消则连接滞留池中；FR-008 要求。

**Alternatives**: 仅 `reader.cancel()`（不 abort fetch）（次选：undici 下 cancel body 通常足够，但 abort 语义完整且官方先例如此，成本相同）。

## D8: 编排器经成员 ctx 订阅 agent/error 观察 turn 失败

**Decision**: `createMember` 在既有 `agent/status` 订阅旁（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:614-618`）增加成员 ctx 的 `agent/error` 订阅：payload `{turn, step, error}`（dsh-agent-loop `lib/index.js:465-473`，失败 turn 的活跃边界，**先于 idle**），记录到 `MemberRuntime.lastTurnFailure = {code, message} | null`（`error` 为 `LlmError` 时取 `error.code`，否则 `UNKNOWN`）。`drive()` 在 idle 等待解除后检查 `lastTurnFailure`：失败 → 返回失败结果；成功/正常 → 清空标记。取消路径不触发（abort 不 emit `agent/error`，turn 以 `{kind:"aborted"}` 收束——spec US2 场景 6 的语义基础）。

**Rationale**: 事件面与 idle 订阅同源（成员自身 ctx dispatch），最小接线；`agent/error` 恰好只在 turn 错误收束时 emit（step 错误经 recovery waterfall 未被吸收才 throw → throwError emit，`lib/index.js:574-599`），llm-retry 吸收的重试不产生该事件——即"最终失败"信号。

**Alternatives**: 订阅 durable `turn/end` session 事件（拒绝：需经 session 事件订阅面且要过滤 turn 归属，接线更重；`agent/error` 已在成员 ctx 事件面且顺序保证先于 idle）；`drive()` 侵入 agent-loop 内部（拒绝：跨层）。

## D9: 成员保持 = 复用既有 fail()/paused 暂停语义

**Decision**: `runPump` 在 `drive()` 返回失败结果时调用既有 `fail()` 通道（`orchestrator.ts:729-742`：`lastError` + `paused=true` + logger error），但**不改变 `this.current`**——激活成员天然保持；下一次 `submit()` 既有逻辑解除 pause 并重驱 `current`（spec Assumption "与既有 fail 暂停/再驱动语义对齐"）。`pendingReview` 驱动失败时保持 `pendingReview` 不消费（既有重试语义，`orchestrator.ts:710-714` 仅在成功 settle 后清除）。`fail()` 的 `lastError` 增加稳定失败码字段；日志结构化字段：`{session, phase, member, code, error}`（FR-012）。

**Rationale**: 059 spec 已定义"成员驱动失败…错误对用户可见…可再次驱动"（`specs/059-agent-v2-team-mode/spec.md:151`）；`fail()`/paused 是既有实现，本决策只是把"throw 才触发"扩展为"turn 失败也触发"，并在触发点保持 `current` 不变（现状 throw 路径同样不切换 current——切换发生在 `nextStep()` 的成功评估里）。

**Alternatives**: 新增独立"retention"状态机概念（拒绝：spec Assumption 明示不新增用户可见状态机概念）。

## D10: FR-012 结构化日志落点为编排器 logger 依赖

**Decision**: 失败日志经 `TeamOrchestrator` 既有 `deps.logger`（宿主注入，console 兜底，`orchestrator.ts:744-747`）产出，severity error、字段 `{session, phase, member, code, error}`；agent_v2 宿主侧确认 `TeamSessions` 物化时传入服务 logger（若无则补接，使日志进 OTel 通道——生产实证该通道存在：SigNoz `game/agent-v2` 应用日志）。

**Rationale**: 单一落点（编排器是"至多驱动一个成员"不变量的执行者，天然知道 member/phase/session）；避免适配器/收集器多点重复记录同一失败。

**Alternatives**: 适配器内直接 logger（拒绝：插件无宿主 logger 依赖且会与编排器日志重复）；history collector 落日志（拒绝：那是呈现面，非编排事实面）。

## D11: 模型选择复合标识 provider/model-id，proto 零字段变更

**Decision**: `provider/model-id` 复合标识作为模型选择面的唯一标识单位：`PresetService.ListModels` 的 `Model.id` 与 `AgentService` 的 `TeamMember.model` 直接承载复合标识（`projects/game/agent_v2.proto` **不改**）。服务侧单一解析点：`session.ts` 将复合标识按首个 `/` 切分为 `(provider, model)`——`DEFAULT_MODEL` 变为 `glm-responses/${process.env.GLM_MODEL || "glm-5.3"}`（`GLM_MODEL` env 语义不变）；`validateModel` 按切分结果对所选 provider 目录做成员校验；`orchestrator.materialize` 的成员输入携带切分后的 `{provider, model}`（`TeamMemberOptions` 增 `provider`，`createMember` 的 `agentOptions.provider` 取 `member.provider ?? deps.provider ?? TEAM_PROVIDER`，`orchestrator.ts:586-591`）。`ListModels` handler 从 `ctx.llm.listProviders()` 联合遍历各 provider 目录（`server.ts:943-948, 972-980` 改造），条目 id 为复合标识。web UI：下拉扁平渲染复合标识（`TeamSettingsPanel.tsx:216-247` 的 option 值即复合标识，"默认"空值语义不变）；级联呈现为纯前端可选演进，不在本范围。裸 id（无 `/`）的输入 → `INVALID_ARGUMENT`（提示复合形态）。

**Rationale**: 用户裁定 Option C（spec Clarifications Session 2026-09-12）；dsh core 路由本就是 `(provider, model)` 结构对（`GenerateOptions.provider`/`model` 分立，`node_modules/.pnpm/@deepseek-ai+dsh-llm@0.1.1-rc.2_*/lib/types/types.d.ts:332-335`），复合标识只在选择面缝合、单一解析点切分，两网关同名 `glm-5.3` 天然消歧。proto 不动 = 契约面最小变更（gateway/proxy 透传零改动）。

**Alternatives**: proto 增加 `Model.provider`/`TeamMember.provider` 字段（拒绝：UI 与调用方需理解两字段组合，proto churn 与代码gen成本大，且用户已裁定单标识形态）；部署级单选 Option A（spec 阶段已裁定否决）。

## D12: opencode-go 插件形态与 Chat Completions wire

**Decision**: 新 workspace 包 `common/js/dsh-plugins/llm-opencode-go/`（`@dominion/dsh-llm-opencode-go`，cordis `name='llm-opencode-go'`、`inject=['llm']`），provider route **`opencode-go`**，注册 `POST {baseURL}/chat/completions`（默认 `https://opencode.ai/zen/go/v1`）。结构镜像 llm-glm（`serialize.ts`/`wire.ts`/`adapter.ts`/`index.ts` + vitest），序列化/翻译以官方 deepseek 适配器为线协议先例（chat completions 全套：system 首消息、`stream:true` + `stream_options.include_usage`、tools 平铺 `type:'function'`、`reasoning_content` delta 接受、`finish_reason` 映射、usage 映射、`[DONE]` 哨兵）。差异决策：(a) reasoning 块**不回传**历史（对齐 llm-glm 049 决策，简化上下文）；(b) 请求头 = 条件 `Authorization: Bearer`（`apiKeyEnv`，默认 `OPENCODE_API_KEY`）+ `attributionHeaders()` + `x-opencode-session: <options.sessionId>`（稳定会话标识，网关官方建议）+ 自定义 `User-Agent`（客户端可识别）；(c) 失败分类/重试声明/停滞看护/空补全/传输清理义务与 llm-glm 完全同套（D1-D7）。默认模型目录 = 官方 Chat Completions 路由全部 16 个模型（`glm-5.3`、`glm-5.3-flash`、`glm-5.2`、`glm-5.1`、`kimi-k3`、`kimi-k2.7-code`、`kimi-k2.6`、`longcat-2.0`、`deepseek-v4.1-flash`、`deepseek-v4-pro`、`deepseek-v4-flash`、`deepseek-v4-flash-vision-exp`、`mimo-v2.5`、`mimo-v2.5-pro`、`hy4-preview`、`hy3`；官方文档模型表 https://opencode.ai/docs/zh-cn/go/ ；id/`contextWindow` 取 models.dev `opencode-go` 快照 https://models.dev/api.json ，网关 `/models` 未文档化 id 不收录），`OPENCODE_MODEL` env 覆盖首项。

**Rationale**: FR-015 裁定 v1 仅 Chat Completions；官方 deepseek 适配器是该 wire 的完整参考实现（本地物化可直接对照）；fake-llm 已有 `/v1/chat/completions` 端点（`projects/game/fake-llm/cmd/main.go:34-37`、`service/handler.go:225-317`）可作测试端点。

**Alternatives**: 并入 llm-glm 包做双 wire（拒绝：provider route/依赖/token 各自独立，混包耦合两个网关的生命周期）；Anthropic Messages wire（spec FR-015 已排除 v1）。

## D13: 部署/env 布线（OPENCODE_* 三元组 + 看护超时）

**Decision**: 镜像 GLM 布线全链：`dsh.ts` 增加 `OPENCODE_BASE_URL`/`OPENCODE_LLM_TARGET`（dominion resolver）解析与 `OPENCODE_API_KEY` 三级解析（env → `$DOMINION_SECRET_DIR/opencode-api-token` → 缺省告警）；`cordis.yml` 增加 `llm-opencode-go` 行（对齐 GLM 行 env 注入：`OPENCODE_BASE_URL`/`OPENCODE_MODEL`/`OPENCODE_STREAM_IDLE_TIMEOUT_MS`）；`service.yaml` secrets += `opencode-api-token`；`projects/game/deploy.yaml` 增加 secret 绑定（`llm-secrets` 家族新 key）；三个既有 testplan deploy YAML 增加 `OPENCODE_LLM_TARGET: dominion:///game/fake-llm:8080` 与合成 `OPENCODE_API_KEY`（非真实 secret，端到端覆盖条件携带与零泄漏），并新增 `deploy_agent_v2_stall.yaml`（SC-004a 独立拓扑，携带 `GLM_STREAM_IDLE_TIMEOUT_MS: "2000"`；主拓扑 3s/4s 正常 chunk 间隔不可共用）。两插件行均暴露 `streamIdleTimeoutMs`（`GLM_STREAM_IDLE_TIMEOUT_MS`/`OPENCODE_STREAM_IDLE_TIMEOUT_MS` env 兜底 300000）。

**Rationale**: 宿主注入模式是 049 D9 既定分歧的延续；布线文件清单来自探索报告（dsh.ts/cordis.yml/service.yaml/deploy.yaml/testplan×3/README）。

**Alternatives**: 无（既定模式复用）。

## D14: fake-llm 有状态 transient 故障注入 + Responses 停滞投影

**Decision**: fake-llm 模板 schema 增加可选 `transient` 块（per-template 有状态、mutex 保护计数器）：`{times: N, http_status?: int, retry_after?: int（秒）, error_message?: string, empty?: bool}`——匹配模板的**前 N 次**请求注入指定行为，之后按模板正常应答；`times` 缺省/0 视为 ∞（向后兼容既有 `failure` 语义仍恒定）。注入行为：`http_status`（Responses/chat 两 wire：直接返回该状态 + `Retry-After` 头，体为 `{"error":{"message":<error_message ‖ "injected http failure">,"type":"injected"}}`）；`empty`（200 + 终局事件但零内容块，两 wire）；既有 `failure`（带内失败）也可配 `times`。另将 chat wire 既有的 `stall`/`stall_after`（`service/handler.go:576-579`）投影到 Responses handler（移除 `responses.go:576-581` 的故意排除，注释同步终态化）。chat wire 的 `http_status` 注入同步支持（opencode-go 插件测试）。

**Rationale**: SC-001 需要"首尝试失败、重试成功"的单次注入——现有 handler 无状态、同请求恒同结果（探索报告 §1.2）；SC-004a 需要 Responses wire 停滞（看护验收）；有状态计数器是最小机制（每模板独立、无全局状态、并发安全）。

**Alternatives**: 独立 fault-proxy 服务（拒绝：部署拓扑与测试复杂度大增）；query-param 触发（拒绝：匹配语义漂移出模板体系）。

## D15: 大型测试验收场景设计（对应 SC-001..005）

**Decision**: 在 `projects/game/testplan/` 增加用例（复用既有 deploy 拓扑与 helpers）：

1. **SC-001**（单次瞬时失败恢复）：fixture 带 `transient: {times: 1, http_status: 503}`（planner 触发词），Send 后断言 turn 正常 COMPLETED、无 ERROR 帧、session log 含 `llm/retry` 事件 1 条。
2. **SC-002**（planner 失败保持）：`transient: {times: 6, http_status: 500}`（= 1 初始 + 默认 5 重试，恰耗尽单 turn 预算）→ 断言 turn_end ERROR、GetTeam activeMember/activation = planner、再次 Send（第 7 次匹配起模板耗尽）仍 planner 应答并完成规划切 player。
3. **SC-004a**（停滞看护）：Responses stall fixture + 独立 `deploy_agent_v2_stall.yaml`（`GLM_STREAM_IDLE_TIMEOUT_MS: "2000"`；主拓扑存在 3s/4s 正常 chunk 间隔，不可共用）→ 断言超时类失败（可重试）呈现且不无限挂起（测试整体超时兜底）。
4. **SC-005**（配额/认证不重试）：`http_status: 429` + `error_message: "insufficient quota"`（注入体携带该文案）→ 断言零 `llm/retry` 事件、恰一次 ERROR；`http_status: 401` → 断言零 `llm/retry` 事件、恰一次 ERROR（双用例）。
5. **SC-003**（opencode-go 全流程）：deploy env `OPENCODE_LLM_TARGET` → fake-llm；UpdateTeam 以 `opencode-go/<model>` 选择成员模型，完成含工具调用的多轮会话；断言目录联合（ListModels 含两 provider 复合标识、同名模型消歧）。

**Rationale**: 直接映射 spec SC；fake-llm 能力缺口由 D14 补齐；验收经 testplan skill 完整闭环（宪章 VI）。

**Alternatives**: 仅单测覆盖（拒绝：宪章 VI 服务型应用必须实际执行大型测试验收）。

## 遗留确认项（tasks 阶段处理）

- `TeamSessions` 物化时是否已向 orchestrator 注入服务 logger（D10 的补接点）——`session.ts:707-726` 物化参数核对。
- `system-prompt` 的 `{{model}}` 变量渲染：复合标识切分后传入 agent 的 `model` 为裸 id（agentOptions.model），system prompt 渲染不受影响（`system-prompt.test.ts:85-97` 现状以裸 id 断言）——tasks 中确认 agentOptions 侧始终裸 id。
- pnpm catalog 中 `@deepseek-ai/dsh-timeout` 版本对齐 0.1.1-rc.2 线（`bazel mod tidy` + gazelle 流程）。
