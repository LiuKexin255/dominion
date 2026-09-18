# 调研：dsh 的 turn/step 语义与业界 agent 框架 turn 边界对照

> **状态**：调研完成。**核实结论（2026-09-11）**：以下三个命题经源码级与官方文档级独立核实——
> **命题 1（确认）**：dsh 的原生词汇是**控制焦点视角**——turn = 控制权的一次完整移交，step = dsh-agent-loop 循环体的一次迭代（一次模型调用 + 其工具执行）；官方原文定义见 [dsh architecture.md Turn flow](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md#turn-flow)："A **step** is one model request plus the tools it calls. A **turn** is zero or more steps: it opens before its first input is claimed and closes once nothing is owed."
> **命题 2（确认 + 两处细化修正）**：多数常见 agent 框架的 turn 是**LLM 调用边界视角**——OpenAI Agents SDK（"A turn is defined as one AI invocation"）与 Anthropic Agent SDK（"A turn is one round trip inside the loop"）逐项确认，该单位 ≈ dsh 的 **step**。修正：① LangGraph 的 turn **不是**模型调用边界——其 turn 指用户对话轮（一次 invoke），模型/图执行的核心词是 Pregel **super-step**，LangGraph 反而是业界中与 dsh turn 语义同向的例证；② opencode 无 turn 词汇——其控制边界（一次 prompt → 一整条 assistant message）与模型迭代分段（step-start part）的两级结构与 dsh 的 turn/step **同构**，是 dsh 形态的先例而非命题 2 的例证。
> **命题 3（确认，一处表述收紧）**：工程理由成立——dsh 把工具循环收进 dsh-agent-loop 内部，取消/编排调度/终态语义/usage 呈现/live 渲染分组都挂靠在 turn 上，step 对外只承担流式渲染分段。表述收紧：模型调用次数对**控制面**（编排器/UI 的驱动与取消）是内部事务，但 dsh 不隐藏模型调用这一**数据面**事实（step/\*、assistant/chunk 全量落 session log，token 计费与审计完整可见）。
> **日期**：2026-09-11
> **前置调研**：`survey/deepseek-harness-team-mode.md`（team 模式与群聊驱动模型）、`survey/deepseek-harness-agent-loop-prereq.md`（dsh-agent-loop 解剖）
> **范围**：dsh（本地物化 0.1.1-rc.2）turn/step 语义的源码级核实；OpenAI Agents SDK、Anthropic（Messages API + Agent SDK）、LangGraph、Vercel AI SDK、opencode 五方官方文档的 turn 边界定义对照；工程理由分析与 multi-agent/编排场景的可组合性说明。**不含**任何方案设计或代码变更。
> **说明**：本文为调研材料（源码/官方文档实证 + 对照结论）；讨论源起 `specs/060-agent-v2-team-optimize` 实时流讨论中形成的对比结论，本文为其独立核实记录。

---

## 1. 背景与调研问题

`specs/060-agent-v2-team-optimize` 的实时流讨论中形成了一组关于 "turn" 定义的对比结论，涉及 dsh 与业界框架的词汇差异。由于该结论会影响后续流事件设计中对 turn/step 的理解（agent_v2 的 ChatEvent 词表直接镜像 dsh 的 turn/block/chunk 结构），需要独立核实而非照抄。本文回答三个问题：

1. dsh 的 turn/step 官方语义究竟是什么？（源码 + 官方文档实证）
2. 主流 agent 框架的 turn 是什么边界？与 dsh 哪个概念对应？（逐项官方文档核实）
3. dsh 为什么把控制焦点单位（turn）与模型调用单位（step）分开？这个工程理由是否站得住？对 multi-agent/编排场景意味着什么？

信息源：

- 本仓库消费侧证据：`projects/game/agent_v2/src/history.ts`、`projects/game/agent_v2.proto`、`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`
- 本地物化 dsh 源码（0.1.1-rc.2 线，pnpm store；两个 hash 实例的 `lib/index.js` 经 diff 确认逐字节一致）：
  - `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（1318 行驱动实现）
  - `node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-session/lib/types/types.d.ts`（SessionEventMap 权威词表）
  - `node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/README.md`（Agent 面/inbox/cancel 语义）
- dsh 官方仓库（master）：[architecture.md Turn flow](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md#turn-flow)
- 业界官方文档：OpenAI Agents SDK（run.py docstring / Sandbox guide）、Anthropic（Messages API tool use / Agent SDK agent loop）、LangGraph（Graph API / Checkpointers）、Vercel AI SDK（lifecycle callbacks / tool calling）、opencode（SDK docs / 源码）——全部 URL 见 §7。

---

## 2. 术语对照总表

两级结构的对照：**控制焦点边界**（谁持有驱动权的一次完整往返）与**模型调用边界**（一次模型请求 + 其工具执行）。

| 框架 | 控制焦点边界（≈ dsh **turn**） | 模型调用边界（= dsh **step**） | 框架自己的 "turn" 词义 |
|---|---|---|---|
| **dsh** | **turn**（`turn/start` → N×step → `turn/end`；打开于 input claim 之前，关闭于"nothing is owed"） | **step**（`step/start` → 一次模型请求 + 其工具执行 → `step/end`） | **turn = 控制焦点** |
| **OpenAI Agents SDK** | **run**（一次 `Runner.run`：input → loop → final output，§4.1） | **turn**（"one AI invocation (including any tool calls that might occur)"；`max_turns` 计数单位） | turn = 模型调用边界 |
| **Anthropic Agent SDK**（Claude Code 同款 loop） | 一次 query 循环（到 `ResultMessage` 终结） | **turn**（"one round trip inside the loop"；`maxTurns`/`num_turns` 计数单位） | turn = 模型调用边界 |
| **Anthropic Messages API** | agentic loop（客户端 `while stop_reason == "tool_use"`） | 一次 Messages 响应（一条 assistant message）；server-tool 的逻辑 turn 可跨多次请求（`pause_turn` 续传） | turn ≈ assistant 回合（`end_turn`/`pause_turn` 的 stop_reason 词） |
| **LangGraph** | thread 上一次 invoke（用户轮；文档称 "multi-turn conversations"） | 无专门词汇；节点执行落在 **super-step**（Pregel tick）内 | turn = 用户对话轮（与 dsh 同向）；核心词是 **super-step** |
| **Vercel AI SDK** | 一次 `generateText`/`streamText`（多 step 到 stopWhen 停止） | **step**（"Each model call is a step"） | 无 turn 词；step = 模型调用边界 |
| **opencode** | **一条 assistant message**（`session.prompt` 一次 → 返回一条 AssistantMessage，内含整个 loop 的产出；`session.abort` 的中止单位） | **step-start part** 边界（一条 assistant message 内可有多个 step-start 分段，每段 = 一次模型迭代） | 无 turn 词 |

两点横向观察：

- 业界 "turn" 的主流义（OpenAI/Anthropic）= dsh 的 step——dsh 若直接沿用业界 turn 词义做外露单位，会把编排器需要的控制边界切碎。
- 存在第三种形态：控制边界与模型迭代显式两级分层（opencode 的 message/part-step、Vercel AI SDK 的 run/step、LangGraph 的 invoke/super-step）——与 dsh 的 turn/step 同构。dsh 不是孤例，而是这个"两级分层"家族中把控制边界命名为 turn 的实现。

---

## 3. dsh 侧证据（源码级）

### 3.1 官方定义原文

dsh 官方仓库 [architecture.md 的 Turn flow 节](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md#turn-flow)（master；其 turn/step 定义与本地 0.1.1-rc.2 线一致，但 master 已演进超出 0.1.1-rc.2——本地 SessionEventMap 尚无 `assistant/attempt`、`system/message` 事件，引用时以本地源码为准）：

> A **step** is one model request plus the tools it calls. A **turn** is zero or more steps: it opens before its first input is claimed and closes once nothing is owed.

其 turn flow 图（官方原文，缩进层级即事件嵌套关系）：

```text
turn/start
  claim next-step input plus one queued message
  assemble prompt sections + tool schemas; project runtime context
  -> agent/pre-step                   reject | enter(messages, ...)
     reject, or a first enter rewritten empty -> close the turn with no step
     step/start
     agent/request -> prepareCall
     append entered messages as user/message; log request/header ...
     derive and freeze model history from the log
     stream ... assistant/message
     tool/call* -> tools/pre-execute -> tools/execute -> tools/post-execute -> tool/result*
     step/end
     tools owe another request, or next-step input arrived -> claim -> next step
  -> agent/turn-stopping
turn/end
```

本地 0.1.1-rc.2 的权威词表（`dsh-session` `lib/types/types.d.ts` SessionEventMap）逐字定义：

- `'step/start'`（types.d.ts L245）：**"Opens step `step` of turn `turn` — one model call plus the tool executions it requested."**
- `'turn/start'`（L225-229）：**"Opens turn `turn` before the loop claims queued input or runs pre-step."**——turn **先于**输入 claim 打开；"Rejection, empty input, cancellation, or failure may close it with no step"（turn 可含零个 step）。
- `'turn/end'`（L234-239）：带 `TurnEndReason`（`completed/blocked/aborted/error/max-tokens/interrupted` 的 merge-extensible sum type，L135-169）。
- `'assistant/chunk'`（L263-268）：载荷就是 `{turn, step, chunk}` 三级索引——**chunk 索引是 step 内局部的**（每次模型请求从 0 重启，消费侧需按 step 重置映射表，§3.3）。
- `'assistant/message'`（L269-285）："Assembled assistant message for one step (derived history uses this). Carries the step's `usage` ... there is no separate usage record"——**dsh 的 usage 记账是 step 级、随 assistant/message 落日志**；取消中断的流前缀以 `interrupted: true` 落同一事件。
- `'tool/call'` / `'tool/result'`（L286-314）：均携带 `{turn, step}` 坐标，`callId` 配对。

### 3.2 驱动实现（dsh-agent-loop 0.1.1-rc.2 源码）

`lib/index.js`（下述行号取自 `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`，两实例一致）：

**turn 主循环 `turn()`（L516-605）**：

- L521-523：`turn = phase.turn + 1`，`session.append("turn/start", { turn })`——turn 编号驱动内单调，durable（resume 时 `findLast(turn/start)` 续编号，L371）。
- L529-537：循环体首步 target 为 `"next-turn"`；`preStep` 里 `this.inbox.claim(target, position.turn)`——**turn 边界的 claim = 全部 next-step 输入 + 恰好一条排队的 next-turn 消息**（README L58 原文："At a turn boundary the driver opens the durable turn, then atomically claims pending next-step input plus one queued prompt; between steps it claims only next-step input."）。
- L538-546：`agent/pre-step` reject 或空输入 → turn 以 `blocked`/`completed` 关闭且**无 step**（turn 可为 0 step 的空壳）。
- L548-561：`step/start {turn, step}` → 落 `user/message`（claim 到的消息进日志）→ `step()` → `step/end {turn, step}`。
- L564-572：`turnEnds` 非空且 next-step 无新输入 → `agent/turn-stopping` serial 事件（listener 可 steer 续跑）→ break；否则 `target = "next-step"` 进入下一 step。**一个 turn 横跨任意多次模型调用**。
- L574-598：错误/abort → `turnEnds` 记 `aborted`/`error`；finally 中 `turn/end {turn, reason: turnEnds}` **总是落日志**（turn 终态兜底）。

**step 执行 `step()`（L606-688）**——即"循环体一次迭代"：

- L613：`buildRequest(turn, step, ...)`，消息边界取 `this.session.deriveMessages()`（请求从日志派生）。
- L617-627：`llm.stream(request)`，逐 chunk `session.append("assistant/chunk", {turn, step, chunk})`——三级索引在此产生。
- L630-647：abort 中断流时，已生成前缀以 `assistant/message {interrupted: true, usage}` 落日志（保真回放）。
- L651-663：流 finish 为 error → `agent/request-error` waterfall（listener 返回 `{kind:"retry"}` 接管恢复，否则 terminal）。
- L665-681：正常完成 → `assistant/message {turn, step, message, usage}` 落日志。
- L683-686：过滤 tool-call 块 → `executeToolCalls(...)`；无工具调用 → `{kind:"completed"}`（step 即 turn 尾步）；`concludesTurn: true` 的工具结果也可结束 turn。

**工具扇出时序（`executeToolCalls` L117-144 + `runGroup` L162+ + `appendToolCall`/`appendToolResult` L292-318）**：`tool/call` 事件在 dispatch 时落（`appendToolCall`，L191），`tool/result` 在按 model order 提交时落（`appendToolResult`，L182），两者均带 `{turn, step}` 坐标且 result 以 `sourceEventSeqs: [callSeq]` 引用 call。**扇出顺序确认为：assistant/message（L673）先于同 step 的 tool/call → tool/result（L685 调用链）**；abort 时未启动的调用补合成 `ABORTED_BEFORE_DISPATCH` 结果对（L274-290），维持"每个 tool_call 必有 tool_result"的 wire 不变量。

**驱动外层面**（dsh-agent README）：

- inbox 三入口：`followup` 排 `next-turn` FIFO（**每条独占一个 turn**）、`steer` 排 `next-step` + 唤醒、`inject` 排 `next-step` 不唤醒（README L68-69）。同一 turn 边界的 claim 恰好消费"一批 next-step + 一条 next-turn"——这是编排器折叠消息进单 turn 的机制基础（§3.3）。
- `cancel(cause, {keepInbox})`："aborts the in-flight turn plus queued and steering work"；已知限制明示 **"There is still no step-only abort that keeps the in-flight turn running"**——**取消的最小单位是 turn**（README L119）。
- `agent/status` 的 `running`："describes a driver-wide drain interval, not proof that a turn is still open; it can cover turn close, the durability checkpoint, and consecutive queued turns"（README L75）——**idle = 整个 drain interval 静默**，这是编排器可依赖的切换锚点。
- `dsh-agent-loop` README L134 已知限制："**No built-in turn budget** — tool calls or steering continue the current turn; a policy that bounds runaway turns must cancel from an existing lifecycle extension point such as `agent/turn-stopping`"——turn 没有内建长度上限，预算策略是插件/编排层职责。

### 3.3 本仓库消费侧证据

- **`projects/game/agent_v2/src/history.ts`**：
  - L49-51："`assistant/chunk` is `{turn, step, chunk}` with chunk a raw StreamChunk"——三级索引的结构化读取面；事件词表锚点指向 `dsh-session` SessionEventMap（L28-30）。
  - L676-685：**step 边界重置**——"dsh chunk indexes restart at 0 on every model request, so a new step number drops the step-local table and the pending interrupted-prefix blocks; the turn-global counter keeps monotonic across steps"。chunk 索引的 step 局部性是 dsh 每次模型请求重启索引的直接后果。
  - L591-613、L764-805：MemberCollector 以 `agent/status` running→idle 为 turn 边界锚（`ensureActive` 铸 turnId → 收集 → idle 时结算 `turn_end{status, error, usage}`）。
  - L671-674：`usage` chunk "Folded into turn_end; never a standalone frame"——usage 呈现挂靠 turn 终局帧（dsh 侧记账本是 step 级，见 §3.1）。
- **`projects/game/agent_v2.proto`**：
  - L559-567：ChatEvent 信封 `turn_id`（"constant across one member turn's events"）、"block index is globally monotonic and **step is monotonic within that member turn**"。
  - L635-639（BlockStartEvent.step）：**"Model output step this block belongs to ...; blocks sharing a step form one display segment"**——step 在对外 API 上的语义就是**渲染分段维度**，不是控制单位。
  - L677-700：`TurnStatus`（COMPLETED/ERROR/ABORTED/CANCELED）+ `TurnError` + `TurnUsage` 挂在 `TurnEndEvent` 上——终态、错误、usage 都以 turn 为信封。
  - L716-721：`HistoryMessage.interrupted`（"this assistant step's content is an interrupted prefix"）——interrupted 是 step 内容级的标记，服务于"失败回合不折叠"的呈现语义。
- **`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`**：
  - L28-36（文件头注释）：**"a drive batches its message set into ONE turn (`inject` all but the last, `followup` the last — the pre-step claim consumes next-step input plus one queued turn)"**，随后 "awaits the member's `agent/status` idle transition ... idle means the whole drain interval settled"。
  - L810-842（`drive()` 实现）：`inject` 除最后一条外的全部消息 + `followup` 最后一条 → 构成成员的**一个** turn；等待 idle 后返回。编排器的一次驱动弧 = 成员的一个 dsh turn，**成员内部走了多少 step 对编排器完全透明**。
  - L470-487（`cancel()`）：`agent.cancel({kind:"user"})` → 成员 turn 终止 + 排队清空——编排层的取消操作面就是 turn 级。

---

## 4. 业界框架逐项核实（官方文档摘录）

### 4.1 OpenAI Agents SDK：turn = 一次 AI 调用（确认）

[openai-agents-python `src/agents/run.py` docstring](https://github.com/openai/openai-agents-python/blob/main/src/agents/run.py)（`max_turns` 参数定义，原文）：

> "max_turns: The maximum number of turns to run the agent for. **A turn is defined as one AI invocation (including any tool calls that might occur).** Pass ``None`` to disable the turn limit."

[Sandbox Agents 指南](https://developers.openai.com/api/docs/guides/agents/sandboxes)（OpenAI 官方 API 文档，原文）：

> "Sandbox agents also don't change what a turn means. **A turn is still a model step**, not a single shell command or sandbox action. ... The agent runtime consumes another turn only when it needs another model response after sandbox work has happened."

判定：**turn = 一次模型调用（含其工具调用）**，`max_turns` 累计的是 model responses。该单位精确对应 dsh 的 step。控制焦点边界在 OpenAI 词汇里叫 **run**（"The agent will run in a loop until a final output is generated"，同 docstring）——一次 run ≈ dsh 的一个 turn（输入 → 循环 → 最终产出）。

### 4.2 Anthropic：两层词汇，Agent SDK 的 turn = loop 内一次往返（确认）

**Agent SDK**（[Agent loop 文档](https://code.claude.com/docs/en/agent-sdk/agent-loop)，Claude Code 同款执行循环，原文）：

> "A **turn is one round trip inside the loop**: Claude produces output that includes tool calls, the SDK executes those tools, and the results feed back to Claude automatically. This happens **without yielding control back to your code**. Turns continue until Claude produces output with no tool calls, at which point the loop ends and the final result is delivered."

`maxTurns` 超限产生 `error_max_turns` 终态、ResultMessage 携带 `num_turns`（同文档）。判定：**turn = 一次模型输出 + 其工具执行** ≈ dsh 的 step；"不向调用方交还控制权"的整个 loop（一次 query 到 ResultMessage）才是控制焦点边界 ≈ dsh 的 turn。

**Messages API 层**（[How tool use works](https://platform.claude.com/docs/en/agents-and-tools/tool-use/how-tool-use-works)）：客户端工具循环的惯用名是 **agentic loop**——"The canonical shape is a `while` loop keyed on `stop_reason`"，循环退出条件是 `stop_reason` 脱离 `"tool_use"`（`end_turn`/`max_tokens`/...）。API 的 turn 词义附着在 stop_reason 上：`end_turn` = 模型结束其 assistant 回合；[server tools 场景](https://platform.claude.com/docs/en/agents-and-tools/tool-use/server-tools) 的 `pause_turn` 表明 server-tool 的逻辑 turn 可跨多次 API 请求续传（"A paused turn means the work isn't finished; re-send the conversation ... to let the model continue where it left off"）。即 **API 层的 turn ≈ 一次（或经 pause 续传的一串）assistant 响应**，仍是模型输出边界视角。

### 4.3 LangGraph：核心词是 super-step，turn 指用户对话轮（修正）

[Graph API 文档](https://docs.langchain.com/oss/python/langgraph/graph-api)（原文）：

> "Inspired by Google's Pregel system, the program proceeds in discrete **'super-steps.'** A super-step can be considered a single iteration over the graph nodes. **Nodes that run in parallel are part of the same super-step**, while nodes that run sequentially belong to separate super-steps."

[Checkpointers 文档](https://docs.langchain.com/oss/python/langgraph/checkpointers)（原文）：

> "A **checkpoint** is a snapshot of the graph state saved at each super-step ... A super-step is a single 'tick' of the graph where all nodes scheduled for that step execute (potentially in parallel)."

`recursion_limit` 限制的是 super-steps 数（同 Graph API 文档）。而 LangGraph 文档中的 "turn" 用于**用户多轮对话**："To continue a conversation on an existing thread, pass a plain input dict ... Do not use `Command(update=...)` alone as input to continue **multi-turn conversations**"（同 Graph API 文档）——一次 invoke（一轮用户输入）= 一个对话 turn。

判定与修正：

- LangGraph **没有**把 turn 用作模型调用边界；模型/图执行的核心词是 **super-step**（Pregel 调度 tick）。
- super-step 与"一次模型调用"**不严格同义**：并行节点同属一个 super-step（其中可含多次模型调用），一个节点的执行又整体落在一个 super-step 内。在简单的 agent 环图（agent 节点 → tools 节点交替）中，一个 super-step ≈ 一次模型调用或一批工具执行，但这是图的形状决定的，不是词汇语义。
- LangGraph 的 turn（用户轮，一次 invoke）与 dsh 的 turn（控制焦点）**语义同向**。因此命题 2 的"多数常见框架"不应把 LangGraph 计入"turn = LLM 调用边界"一侧——它属于第三类（invoke/super-step 两级分层）。

### 4.4 Vercel AI SDK：step = 一次模型调用（opencode 的词汇上游）

[Lifecycle callbacks 文档](https://ai-sdk.dev/docs/ai-sdk-core/lifecycle-callbacks)（原文）：

> "When you use tools with `generateText` or `streamText`, a single user request can involve multiple model calls. **Each model call is a step.**"

[Tool calling 文档](https://ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling)：`stopWhen` 开启 multi-step——"the AI SDK will trigger a new generation passing in the tool result until there are no further tool calls or the stopping condition is met"。判定：**step = 模型调用边界**（= dsh 的 step），一次 generateText/streamText（run）= 控制焦点边界。无 turn 词汇。

### 4.5 opencode：message/part 两级，与 dsh 同构（修正）

[opencode SDK 文档](https://opencode.ai/docs/sdk/)：

- `session.prompt({ path, body })` — "Send prompt message ... Default returns **`AssistantMessage` with AI response**"：一次 prompt 的整条 agent loop（含全部工具往返）收敛为**一条** assistant message。
- `session.abort({ path })` — "Abort a **running session**"：中止单位是 prompt 运行整体（session 粒度的运行态），不是某次模型调用。
- Message/Part 类型："`session.messages({ path })` — List messages in a session. Returns `{ info: Message, parts: Part[] }[]`"。

[opencode 源码 `packages/opencode/src/session/message-v2.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/session/message-v2.ts)：assistant message 的 parts 包含 `step-start` 类型，且一条 message 内可有**多个** step-start 分段——源码注释原文："Anthropic adaptive thinking can persist assistant turns like: **step-start, reasoning(signature), text(""), step-start, reasoning(signature)**"（step-start 词汇承自 Vercel AI SDK UIMessage part 类型，其 step 语义见 §4.4）。

判定：opencode **无 turn 词汇**；其两级结构为——**一条 assistant message（一次 prompt 运行的全部产出）≈ dsh 的 turn；message 内的 step-start 分段边界 ≈ dsh 的 step**。这是业界中与 dsh turn/step 结构最同形的实现，属命题 2 的修正项（它不构成"turn = LLM 调用边界"的例证，反而是两级分层的先例）。

---

## 5. 命题核实结论

### 5.1 命题 1（dsh 视角）——确认

- **turn = 控制权的一次完整移交**：官方定义"opens before its first input is claimed and closes once nothing is owed"（§3.1）；turn 边界的 claim 恰好是"一批 next-step 输入 + 一条排队消息"（§3.2）。单 agent 形态：`followup` 每条独占一个 turn，turn 即用户↔agent 的一次往返（§3.2 inbox 三入口）。team 形态：orchestrator 的一次 `drive()` = inject N-1 条 + followup 1 条 = 成员的**一个** turn，等待 idle（drain interval）即驱动弧闭合（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` L28-36、L810-842）。
- **step = dsh-agent-loop 的一次循环体**：官方定义"one model call plus the tool executions it requested"（§3.1 `step/start`）；实现上 `step()` = 一次模型流（chunk → assistant/message）→ 工具执行 → 继续/停止（§3.2）。
- 补充事实（讨论结论未提及但成立）：turn 可含**零**个 step（reject/空输入时 turn 仍开合，`blocked`/`completed` 关闭）；turn 编号 durable（resume 续编号）；interrupted 标记是 step 内容级（`assistant/message {interrupted}`），turn 终态由 `turn/end.reason` 与 agent_v2 的 `turn_end.status` 承载。

### 5.2 命题 2（业界 turn = LLM 调用边界 ≈ dsh step）——确认，附两处修正

逐项核实结果：

| 框架 | turn 词义 | 与结论相符性 |
|---|---|---|
| OpenAI Agents SDK | "one AI invocation (including any tool calls)"；`max_turns` 计 model responses | ✅ 相符 |
| Anthropic Agent SDK | "one round trip inside the loop"；`maxTurns`/`num_turns` | ✅ 相符 |
| Anthropic Messages API | assistant 回合（`end_turn`/`pause_turn`）；server-tool turn 可跨请求 | ✅ 相符（模型输出边界视角） |
| LangGraph | 用户对话轮（一次 invoke）；核心词 super-step | ❌ **修正**：不属本命题；其 turn 反与 dsh turn 同向 |
| opencode | 无 turn 词；assistant message / step-start part 两级 | ❌ **修正**：不属本命题；与 dsh turn/step 同构 |
| Vercel AI SDK | 无 turn 词；step = "each model call is a step" | ➖ 佐证：其 step 语义与 dsh step 精确一致 |

结论：**"多数常见 agent 框架的 turn 是 LLM 调用边界视角、该单位 ≈ dsh 的 step"成立**（OpenAI/Anthropic 两大直接证据 + Vercel AI SDK 的 step 同义佐证），但"多数"应限定为 **OpenAI Agents SDK 与 Anthropic 系**；LangGraph 与 opencode 属于"控制边界/模型迭代两级分层"家族（LangGraph 的 turn 另有专义）。跨框架迁移词汇时的实务规则：**先问"这个框架的 turn 对应 dsh 的 turn 还是 step"，不能按词面等同**——OpenAI/Anthropic 语境下的 "max_turns 预算" 折到 dsh 是 **step 预算**（dsh 无内建 turn budget，需在 `agent/turn-stopping` 或编排层实现，§3.2）。

### 5.3 命题 3（工程理由）——成立，一处表述收紧

"dsh 把工具循环收进 agent 内部（dsh-agent-loop），模型调用次数成为内部事务；控制焦点边界才是调用方需要的外露单位"——核实如下，逐项锚定：

1. **工具循环内置于 loop**：`dsh-agent-loop` 自述"The only package in the harness that contains concrete loop logic"（README L7）；调用方（UI/编排器）面向的是 `Agent` 面（`followup/steer/inject/cancel`，全部 turn 粒度入口）与 `session/event`/`agent/status` 事件面，不直接驱动模型调用。
2. **取消挂靠 turn**：`cancel` 的最小单位是 in-flight turn，官方明示"no step-only abort"（§3.2）；agent_v2 的 Cancel → `turn_end{CANCELED}` + 排队作废（`orchestrator.ts` L470-487）。
3. **编排调度挂靠 turn**：turn 边界 claim 的原子性（一批 next-step + 一条 next-turn）使编排器能把任意一批消息折叠进一个 turn（`orchestrator.ts` inject+followup 模式）；`agent/status` 的 running 覆盖 drain interval、idle 即切换锚——编排器以 turn 为最小调度/切换/互斥单位（"at most one member driven at a time" 以单个 in-flight turn 表达）。dsh-subagent 的 parent→child 也是 followup FIFO turn（`survey/deepseek-harness-team-mode.md` §2.4）——**turn 是 dsh 生态统一的编排原子**。
4. **interrupted / 终态 / usage 呈现挂靠 turn 信封**：dsh 侧 interrupted 是 step 级内容标记（保真回放需要），但对外终态面（agent_v2 `TurnEndEvent{status,error,usage}`）以 turn 为信封；usage 记账 dsh 是 step 级（随 assistant/message），agent_v2 折叠进 `turn_end` 不单独发帧（§3.3）。
5. **live 渲染分组挂靠 turn，step 只做分段**：agent_v2 前端按 `(member, turn_id)` 分组渲染，step 是"blocks sharing a step form one display segment"的分段维度（`agent_v2.proto` L635-639）；上游 dsh-web 的 turn process folding 设计（[2026-08-14-web-turn-process-folding note](https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/archived/feature/2026-08-14-web-turn-process-folding.md)，`specs/060-agent-v2-team-optimize/spec.md` Clarifications 已裁定引用）同样以"Turn 展开期间不折叠、关闭后折叠为'思考过程（N 步骤 · M 次工具调用）'"为渲染单位——**step 在对外契约中的唯一职责是流式渲染分段**，与讨论结论一致。
6. **表述收紧**："模型调用次数成为内部事务"应限定为**控制面**（编排器/UI 的驱动、取消、切换不以 step 为单位）；**数据面**上 dsh 不隐藏模型调用事实——`step/start`/`step/end` 是 durable session 事件、`assistant/chunk` 全量落日志、usage 按 step 记账，token 审计与回放完整。即：**step 不是控制单位，但是一等数据事实**；这也是 step 仍需要出现在 `session/event` 词表与对外流（渲染分段）里的原因。

---

## 6. 对 multi-agent / 编排场景的可组合性

1. **turn 作为编排原子的通用性**：dsh 的编排面（无论 saolei-loop 的交替驱动、dsh-subagent 的 continuation manager，还是实验性 Agent Teams）都建立在同一不变量上——**驱动 = 注入消息 + 等待 idle**，一个成员 turn 是不可分割的调度量子。成员内部 step 数（驱动弧长度）对编排器自适应：复杂局面多走几个 step、简单局面一个 step 收尾，编排逻辑无需感知。
2. **消息折叠的语义边界**：一批消息折叠进一个 turn 依赖 inbox claim 的原子性（"next-step input plus one queued prompt"，§3.2）。等价物对照：OpenAI run 的 input list（一次 run 吃进全部输入）、LangGraph 的 `Command(update)` + invoke 输入——跨框架移植"折叠"语义时要对准**控制边界**而非 step。
3. **预算与取消的挂靠差异是跨框架最大的坑**：OpenAI/Anthropic 的 `max_turns`/`maxTurns` 是**模型调用数预算**（≈ dsh step 预算），dsh 无内建 turn budget（§3.2 已知限制）——从 OpenAI 语义迁移"预算"到 dsh 时，正确落点是 step 计数策略（挂 `agent/pre-step`/`agent/turn-stopping`）或编排层驱动弧上限，而不是给 turn 计数。反之，dsh 的取消（turn 粒度）移植到 OpenAI 形态需要取消整个 run（SDK 无单 turn 取消概念）。
4. **多 agent 拓扑中的单位选择**：dsh team 模式（本仓库）以"顶层对等 agent + 编排器驱动 turn"组织（`survey/deepseek-harness-team-mode.md` §4）；对照业界——OpenAI 的 handoff 发生在 run 内部（turn 语义不变）、Anthropic 的委派走 tool 边界（subagent 结果以 tool result + notice 回流）、LangGraph 以图拓扑天然表达并行（super-step 内并行节点）。**turn 作为控制边界的价值在多 agent 下放大**：编排器的驱动/取消/超时/记账面全部统一到成员 turn 上，不需要理解每个成员内部的模型调用拓扑。
5. **UI 消费的可移植结论**：两级分组（turn 分组 + step 分段）是 dsh-web 与 opencode 共同收敛的渲染形态（前者 Turn folding，后者 assistant message + step-start part 分段）；agent_v2 的 ChatEvent 词表（`turn_id` 信封 + 块帧携带 step）与该形态一一对应。

---

## 7. 引用来源汇总

仓库内（本地物化源码，0.1.1-rc.2 线）：

- `projects/game/agent_v2/src/history.ts`（session/event 读取面、step 边界重置、turn 结算）
- `projects/game/agent_v2.proto`（ChatEvent 信封、TurnEndEvent、BlockStart.step 注释）
- `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`（drive 折叠/切换锚/cancel）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js`（turn()/step()/executeToolCalls 实现；两 hash 实例 diff 一致）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent-loop/README.md`（claim 原子性、turn budget 已知限制）
- `node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-session/lib/types/types.d.ts`（SessionEventMap：turn/step/chunk/message/tool 事件定义与注释）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/README.md`（followup/steer/inject、cancel、running/idle 语义）
- 前置调研：`survey/deepseek-harness-team-mode.md`、`survey/deepseek-harness-agent-loop-prereq.md`；需求背景：`specs/060-agent-v2-team-optimize/spec.md`

仓库外（官方文档/源码，均访问于 2026-09-11）：

- dsh：https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md#turn-flow （Turn flow 权威定义；master 已超出 0.1.1-rc.2，见 §3.1 版本注记）
- dsh-web：https://github.com/deepseek-ai/deepseek-harness/blob/master/.agents/notes/archived/feature/2026-08-14-web-turn-process-folding.md （Turn 渲染折叠设计）
- OpenAI Agents SDK：https://github.com/openai/openai-agents-python/blob/main/src/agents/run.py （turn/max_turns 定义）；https://developers.openai.com/api/docs/guides/agents/sandboxes （"a turn is still a model step"）
- Anthropic：https://code.claude.com/docs/en/agent-sdk/agent-loop （Agent SDK turn 定义、maxTurns/num_turns）；https://platform.claude.com/docs/en/agents-and-tools/tool-use/how-tool-use-works （agentic loop / stop_reason）；https://platform.claude.com/docs/en/agents-and-tools/tool-use/server-tools （pause_turn、跨请求续传的 server-tool turn）
- LangGraph：https://docs.langchain.com/oss/python/langgraph/graph-api （super-step、recursion_limit、multi-turn conversations）；https://docs.langchain.com/oss/python/langgraph/checkpointers （checkpoint = super-step 快照）
- Vercel AI SDK：https://ai-sdk.dev/docs/ai-sdk-core/lifecycle-callbacks （"Each model call is a step"）；https://ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling （stopWhen multi-step）
- opencode：https://opencode.ai/docs/sdk/ （session.prompt/abort、Message/Part）；https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/session/message-v2.ts （step-start part、一条 assistant message 多 step）
