# 调研：Planner 内部长期记忆与 Agent 间通信机制

> **状态**：调研完成，待审查立项进入方案设计
> **日期**：2026-08-07
> **目标服务**：`projects/game/agent/`
> **范围**：031 team-template-mode 落地后，改进 planner 的长期记忆机制与 planner→player 的通信方式
> **说明**：本文为调研材料，供后续制定方案（spec / plan）时使用；非 SDD spec 文档。所有外部引用附完整 URL，仓库内引用附相对路径。

---

## 1. 背景与目标

### 1.1 起因

`specs/031-team-template-mode/` 实现后，发现 **"策略直接替换上下文" 效果不好**：

- 当前 planner 每局结束调 `update_strategy` **整体替换**一段策略文本（`projects/game/agent/src/team/update-strategy.ts`，`store.put(sessionId, content)` upsert），语义上是 LangChain 的 "Profile" 单文档整体替换模式。
- player 每次进入节点读一次 `StrategyStore.get()` 注入为固定 id 的 SystemMessage（"当前态势"），写回时过滤掉不进短期记忆（`projects/game/agent/src/team/player.ts`）。
- 整体替换的策略，LLM 每次要从零重写全文，既贵又易丢失前几局累积的洞察；且 strategy 作为外部 system 注入，未进入 player 的对话流，player 引用困难。

### 1.2 改进方向（经多轮讨论收敛）

1. **废弃 StrategyStore**（player/planner 共享的独立长期记忆）。
2. **planner 游戏结束后向 player 发送策略指示**，作为 player 对话历史的一部分（累积、可引用）。
3. **planner 自身**拥有内部长期记忆（跨多局对 player 的累积校准、自身复盘经验的演化）——这是本调研的核心问题。

### 1.3 调研聚焦的五个问题

| # | 问题 | 结论章节 |
|---|---|---|
| Q1 | 长期记忆的更新发生在什么时候？使用什么方式（单独 tool vs 框架抽取）？ | §3 |
| Q2 | 长期记忆更新后，如何刷新当前 agent 的上下文？ | §3 |
| Q3 | 架构改动（废弃 StrategyStore 后的 super turn 流程） | §5（决策） |
| Q4 | 压缩上下文时如何处理长期记忆？需不需要压缩后更新 system prompt 的长期记忆？ | §4 |
| Q5 | 不同 agent 之间的通信方法（sender tool? receiver HumanMessage?） | §6 |

主要参考生态：**LangChain / LangGraph**（dominion 使用的框架）与 **hermes-agent**（NousResearch）。Q5 额外参考 openclaw、opencode。

---

## 2. 当前架构现状（031 落地后）

### 2.1 player / planner 节点与 StrategyStore

| 组件 | 文件 | 现状 |
|---|---|---|
| player 节点 | `projects/game/agent/src/team/player.ts` | `createAgent` 全 loop；入口读 `strategyStore.get(sessionId)` 注入固定 id SystemMessage（"当前态势"），写回时过滤；createAgent 不带自身 checkpointer |
| planner 节点 | `projects/game/agent/src/team/planner.ts` | 每局结束触发一次；入口读 `strategyStore.get` 注入 system；持 `update_strategy` tool |
| `update_strategy` tool | `projects/game/agent/src/team/update-strategy.ts` | **整体替换**："the new strategy replaces the previous one entirely"；`store.put(sessionId, content)` upsert |
| StrategyStore 契约 | `specs/031-team-template-mode/contracts/strategy-store-contract.md` | `get(sessionId)` 无记录返回 `""`；`put(sessionId, content)`；mongo 持久；以 session id 为键 |
| team graph 契约 | `specs/031-team-template-mode/contracts/team-graph-contract.md` | per-agent 私有通道 `playerMessages`/`plannerMessages`；策略不在 state（代码层注入）；`MemorySaver` 单一 checkpointer |

### 2.2 037 的压缩机制（已落地，与本调研强相关）

`specs/037-saolei-team-optimize/research.md` D1 已实现：每 5 局触发 compress 节点，对 playerMessages / plannerMessages 通道全量压缩为一条摘要 AIMessage（`RemoveMessage(REMOVE_ALL_MESSAGES)` + 摘要）。压缩作用域仅短期通道，**当前不影响** StrategyStore。

---

## 3. 调研：长期记忆机制（Q1 + Q2）

### 3.1 LangChain / LangGraph（官方概念文档）

来源：[LangChain JS Memory 概念文档](https://docs.langchain.com/oss/javascript/concepts/memory)、[LangGraph Store 文档](https://docs.langchain.com/oss/javascript/langgraph/stores)、[LangGraph add-memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory)。

#### 3.1.1 记忆类型三分（参考人类记忆，映射 [CoALA paper](https://arxiv.org/pdf/2309.02427)）

| 类型 | 存储内容 | Agent 示例 |
|---|---|---|
| Semantic（语义） | 事实 | 关于用户/环境的事实 |
| Episodic（情景） | 经历 | 过去的 agent 动作（few-shot 示例） |
| Procedural（程序性） | 指令 | agent 的 system prompt（agent 修改自己的指令） |

#### 3.1.2 Q1 答案：长期记忆更新有两条路径

LangChain 官方明确二分（[概念文档 "Writing memories"](https://docs.langchain.com/oss/javascript/concepts/memory#writing-memories)），并各给参考实现：

| 维度 | **Hot path（热路径）** | **Background（后台）** |
|---|---|---|
| **方式** | agent 持有**单独的 memory tool**，自己决定何时记什么 | **独立的后台任务**重放/审查对话，抽取记忆 |
| **时机** | agent 运行中，响应前 | 主任务完成后异步/分离触发 |
| **典型例子** | ChatGPT 的 `save_memories` tool（upsert content string）→ [langchain-ai/memory-agent](https://github.com/langchain-ai/memory-agent) 模板 | [langchain-ai/memory-template](https://github.com/langchain-ai/memory-template)（memory-service） |
| **优点** | 实时（新记忆立即可用）、透明 | 无主路径延迟、应用逻辑与记忆管理解耦、可换便宜模型 |
| **缺点** | 多一个 tool 增复杂度、推理"记什么"增延迟、多任务分心影响记忆质量 | 写入频率/触发时机需精心设计 |

**两者并不互斥，是两层。** dominion 的 planner 复盘本质是 Background 语义（局结束触发，领域事件），planner 复盘内部用 hot-path tool 写记忆。

#### 3.1.3 语义记忆的两种管理形式

- **Profile**：单个持续更新的"档案"文档（整体替换）。随文档变大会出错、整体重生成易丢信息。**→ dominion 当前 StrategyStore 就是这个模式（整体替换）。**
- **Collection**：文档集合，持续追加/扩展，每条记忆更窄、更易生成、不易丢信息；但更新复杂（需 delete/update 现有条目）、搜索复杂。

#### 3.1.4 Q2 答案：LangGraph 每次读 store 都是 live 的

LangGraph 标准模式是 **prompt function / `dynamic_prompt` middleware 读 store**（[概念文档 procedural memory 伪代码](https://docs.langchain.com/oss/javascript/concepts/memory#procedural-memory)）：

```ts
const callModel = async (state, store) => {
  const instructions = await store.get(["agent_instructions"], "agent_a"); // 每次读，live
  const prompt = promptTemplate.format({ instructions: instructions[0].value.instructions });
  ...
};
```

刷新粒度取决于 prompt 挂载点：挂在 `create_react_agent` 的 `prompt` 参数 → 每次 invoke 入口读一次；挂在 `dynamic_prompt` / `beforeModel` middleware → **每次 model call 前重读**（真正 live）。

> **LangGraph 没有"冻结快照"概念**——每次读 store 都反映最新值。这与 hermes 形成鲜明对比（见 3.2.3）。

### 3.2 hermes-agent（NousResearch）

来源：[Persistent Memory 文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)、[Skills 文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)、[`tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py)。

#### 3.2.1 记忆系统分层

| 系统 | 类型 | 机制 |
|---|---|---|
| MEMORY.md（agent 个人笔记，2200 chars）+ USER.md（用户画像，1375 chars） | semantic | 有界快照，注入 system prompt |
| `skill_manage`（SKILL.md，progressive disclosure） | procedural | on-demand 加载，`skills_list()`(~3k tokens) → `skill_view(name)` → `skill_view(name, path)` |
| `session_search`（FTS5 全文检索过去会话） | episodic | 按需检索，不在常驻上下文，~20ms 查询 |

#### 3.2.2 Q1 答案：hermes 两条路径都用

**(a) Hot path：`memory` tool（前台主动）**
- 动作只有 `add` / `replace` / `remove`，**没有 `read`**（记忆自动注入 system prompt，见 Q2）。
- **substring matching**：replace/remove 用短子串定位条目，不需要全文。
- 严格字符上限，**满了不自动压缩**——tool 返回错误 + 当前条目列表，让 agent **在同一个 turn 内**自己 `replace` 合并或 `remove` 删除后再重试。把"何时更新"的决定权显式交给 LLM。

**(b) Background：self-improvement review（后台自改进审查）**
- **每个 turn 之后**异步跑一个后台 review，重放对话，判断是否该存记忆或更新 skill。
- **可换更便宜的模型**跑（`auxiliary.background_review`，重放"recent turns 原文 + 旧 turn 摘要"的 digest，约省 3–5×）。
- skill 创建时机：5+ 工具调用的复杂任务成功后 / 遇到错误死路找到正路后 / 用户纠正其方法后。

#### 3.2.3 Q2 答案：hermes 冻结快照（frozen snapshot）—— 刻意牺牲实时性换 prefix cache

> 来源：[memory 文档 "How Memory Appears in the System Prompt"](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory#how-memory-appears-in-the-system-prompt)。
>
> **Frozen snapshot pattern**：system prompt 注入在**会话开始时捕获一次，mid-session 永不改变**——这是**有意的**，为了保持 LLM 的 prefix cache。agent 在会话中 add/remove 记忆时，**变更立即持久化到磁盘，但不会出现在 system prompt 直到下一个会话开始**。Tool responses 始终显示 live state。

设计权衡：**性能（prefix cache 命中）优先于 mid-session 实时性**。记忆更新的"生效"延迟到下一个会话边界；tool 调用结果仍能反映 live 状态。

**这与 LangGraph 的 live-read 形成两种对立哲学。**

---

## 4. 调研：压缩与长期记忆（Q4）

> 用户决策：planner 长期记忆采用 hermes 式冻结快照，刷新边界定在"每 5 局压缩时"（同 hermes）。因此 hermes 压缩如何处理记忆直接相关。

### 4.1 hermes 压缩时刻意刷新 system prompt 的长期记忆块

来源：[`agent/conversation_compression.py`](https://github.com/NousResearch/hermes-agent/blob/main/agent/conversation_compression.py)、[Context Compression and Caching 文档](https://hermes-agent.nousresearch.com/docs/developer-guide/context-compression-and-caching)。

**hermes 的 memory 在会话期间是冻结快照**（§3.2.3）。那么会话期间用 memory tool 写的记忆什么时候才能被 agent 看到？**答案就是：压缩时**——压缩是 hermes **mid-session 刷新冻结 memory 快照的唯一契机**。

压缩触发时的执行链（`run_agent.py` + `conversation_compression.py`）：
1. 压缩前 flush，把待写 memory 落盘；
2. `_invalidate_system_prompt()` → **`load_from_disk()`**（重新从磁盘加载 memory）→ `_build_system_prompt()`（用最新 memory **重建 system prompt**）；
3. 中间对话历史换成摘要，system prompt 用重建后的版本。

### 4.2 该设计是反复迭代来的（说明坑在哪）

| 阶段 | 问题 | 修复 |
|---|---|---|
| 初版 | 压缩无条件重建 system prompt → KV cache 全失效 → 本地 MoE 模型分钟级卡顿（[issue #4319](https://github.com/NousResearch/hermes-agent/issues/4319)） | — |
| 中期 | system prompt 里 "Conversation started" 时间戳每次压缩都变 → prefix 变 → cache miss（[issue #8687](https://github.com/NousResearch/hermes-agent/issues/8687)） | 时间戳改为 session_start 稳定值，压缩信息单独成 "Last context compaction" 行（PR #27675） |
| **最新（[PR #67916](https://github.com/NousResearch/hermes-agent/pull/67916)）** | 即使 memory 没变也重建，白丢 cache | **引入 retain-vs-rebuild 决策**：压缩后用 containment check（`_cached_prompt_reflects_builtin_memory`）判断 cached prompt 是否已含当前 memory——**memory 没变就保留 cached prompt 不重建，变了才重建** |

**反面坑（[issue #17251](https://github.com/NousResearch/hermes-agent/issues/17251)）**：压缩 handoff 摘要把 memory 标成 "background reference, NOT active instructions"，导致 agent 字面遵循而忽略自己的 memory。结论：**memory 在压缩后必须仍是 active context，不能被降级为背景参考**。

### 4.3 prompt 分层（为保 cache）

hermes 把 system prompt 按 cache 稳定性分层：`[STABLE]` identity/skills/context-files/tools → `[VOLATILE]` memory 快照 / 压缩备注 / 时间戳。volatile 放尾部，即使 memory 变化，stable prefix 仍可被 KV cache 复用。

### 4.4 对照 dominion 的结论

dominion 的 planner 与 hermes 架构不同，但用户已选定"冻结快照 + 压缩刷新"，所以 hermes 的实践**直接适用**：

- planner 长期记忆烘焙进 system prompt 后冻结，跨多局复盘复用同一 system prompt；
- memory tool（add/replace/remove）改记忆只落盘，system prompt 不变；
- **每 5 局压缩时刷新**（同 hermes 的压缩刷新边界）：重读长期记忆，重新烘焙 system prompt；
- 可借鉴 hermes 的 **retain-vs-rebuild 优化**（记忆没变就跳过重建）；
- 压缩摘要提示词要明确"长期记忆是权威 active 上下文"，避免 issue #17251 的贬低坑。

---

## 5. 决策汇总（用户已拍板）

以下为多轮讨论后用户确认的设计决策，供后续 spec/plan 落实：

### D1. planner 内部长期记忆：hot path tool（add/replace/remove），hermes 风格

- planner 持有 memory tool（`add` / `replace` / `remove`，无 `read`），把对 player 的长期观察/复盘经验写成有界多 entry。
- planner **不额外 background 循环**——planner 自身（每局结束触发）就是一个 background。
- 参考：[hermes memory tool](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory#memory-tool-actions)。

### D2. planner 长期记忆注入：冻结快照（hermes 模式），不每次 invoke 刷新

- planner 的长期记忆注入 system prompt，**跨多次复盘 invoke 保持冻结**（同 hermes frozen snapshot，§3.2.3）。
- memory tool 修改记忆只落盘（保存修改记录），**不替换/不刷新 system prompt**。
- **澄清**：不是"每次 invoke 入口刷新"，而是冻结——刷新靠压缩边界（D4）。

### D3. 废弃 StrategyStore，super turn 改 planner→player 指示循环

- 不再有 player/planner 共享的 StrategyStore。
- super turn（一个完整 team turn）改为循环：
  1. planner 先向 player 发送策略指示；
  2. player 开始游戏；
  3. 游戏结束 → planner 复盘 → 发新指令给 player；
  4. 继续下一轮，不断循环。

### D4. 冻结快照刷新边界：每 5 局压缩时刷新长期记忆（同 hermes）

- 复用 037 已有的 compress 节点（每 5 局）作为刷新边界。
- 压缩时重读 planner 长期记忆，重新烘焙 system prompt（同 hermes 压缩刷新，§4.1）。
- 可借鉴 hermes retain-vs-rebuild（§4.2）。

### D5. 重建 planner systemPrompt 的实现方式：方案 b

- 长期记忆**不烘焙进 `createAgent` 的 `systemPrompt` 参数**（避免重建 createAgent 实例）。
- 而是作为 **input SystemMessage 注入**，但内容用一个**"冻结缓存"**持有（冻结期间不重读 store）；刷新边界（压缩）时清缓存，下次 invoke 重新读 store 烘焙新值。
- 改动更小，无需重建 createAgent 实例。

### D6. planner→player 通信：HumanMessage 写 playerMessages channel（openclaw 模式 + LangGraph channel）

- 详见 §6 调研结论。planner 调 tool 把指示作为 HumanMessage 写进 playerMessages channel，player 从自己的 channel 读。

---

## 6. 调研：Agent 间通信机制（Q5）

> 用户问题："是否大部分采用 sender 通过 tool 发送，接收者通过 HumanMessage 接收？"

### 6.1 答案：不是统一做法——三种模式并存，仅 openclaw 用 HumanMessage

调研覆盖 hermes、openclaw、opencode 三个 agent harness，以及 dominion 所用的 LangGraph。

**关键认知**：这三个 harness 都是 **"一次性委派"（delegation）范式**（parent 调 tool 委派子任务，child 跑完返回 summary 就结束）；dominion 的 player/planner 是 **LangGraph message passing 范式**（持续多局协作）。两者不同。

### 6.2 四种通信机制对比

| 项目 | sender→receiver 发起 | receiver 接收形式 | receiver→sender 返回 | 范式 |
|---|---|---|---|---|
| **hermes** | parent 调 `delegate_task` tool（传 goal+context） | **goal 烘焙进 child 的全新 system prompt**（[`_build_child_system_prompt`](https://github.com/NousResearch/hermes-agent/blob/main/tools/delegate_tool.py)），child 拿全新对话无 parent 历史 | child 输出 summary，作为 delegate tool 返回值（parent 看 tool result） | 委派（一次性） |
| **openclaw** | parent 调 spawn tool | **task 作为 first HumanMessage**（[`buildSubagentInitialUserMessage`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-initial-user-message.ts)），system prompt 只放运行时规则 | child 完成后 [`deliverSubagentAnnouncement`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-announce.ts) 投递进 parent 会话 | 委派（一次性，支持 persistent follow-up） |
| **opencode** | parent 调 Task tool（`@general` subagent） | subagent 独立上下文 | Task tool 返回值 | 委派（一次性） |
| **LangGraph**（dominion 框架） | agent tool 返回 `Command(goto=..., update={messages:[...]}, graph=PARENT)` | **共享 messages channel**，receiver 从 channel 读 | 写回共享 channel | message passing（持续协作） |

### 6.3 重点：openclaw 为什么选 HumanMessage（最贴近 dominion 需求）

来源：[`src/agents/subagent-initial-user-message.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-initial-user-message.ts)。

openclaw **刻意把 task 从 system prompt 挪到 first user turn（HumanMessage）**，源码注释给出三个理由：

```ts
// Keep the delegated task transcript-visible and single-sourced here.
// The system prompt owns runtime/subagent rules; this user turn owns
// the actual task envelope so delivery is easy to audit without
// duplicating tokens.
```

1. **transcript 可见**——task 在对话流里可见，不在隐藏的 system prompt；
2. **single-sourced，不重复占 token**——避免 system prompt 和 user turn 各放一份；
3. **审计友好**——投递链路好审计。

而 hermes 把 goal 烘焙进 system prompt（[`_build_child_system_prompt`](https://github.com/NousResearch/hermes-agent/blob/main/tools/delegate_tool.py)），openclaw 的注释正是对这种做法的改进。hermes 的 `delegate_tool.py` 自承 "modeled on OpenClaw's buildSubagentSystemPrompt"——openclaw 在先，hermes 借鉴但走了不同分支。

openclaw 还有一个对 dominion 有用的概念：**persistent session + follow-up messages**（`persistentSession` 参数 + [`subagent-control.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-control.ts) 的 send-message 操作）——parent 可给运行中/持久的 child 反复发 follow-up 消息。对应 dominion 的"planner 每局给 player 发新指示"。

### 6.4 对照 dominion 的结论（→ D6）

dominion 新架构"planner 发指示给 player"最接近 **openclaw 的 HumanMessage 模式 + LangGraph 的共享 channel**：

- **像 openclaw**：指示作为 HumanMessage 进入 player 的对话（playerMessages），而非烘焙进 player 的 system prompt。理由同 openclaw——transcript 可见（desktop player tab 能看到 planner 指示）、累积可引用、不与 player 的 base system prompt 重复。
- **像 LangGraph message passing**：planner 通过 tool（或节点返回值）把 HumanMessage 写进 playerMessages channel，player 节点入口从自己的 channel 读。复用 031 已有的 per-agent channel 架构（`specs/031-team-template-mode/contracts/team-graph-contract.md` §1，D5）。

**与一次性委派 harness 的关键区别**：dominion 是**每局反复投递**（planner 每局发一条新指示进 playerMessages），player session 持久、指示累积——更像 openclaw 的 persistent follow-up，而非一次性 delegate。

### 6.5 需要注意的细节（留给 plan）

planner 指示作为 HumanMessage 进 playerMessages 后，会随局数累积膨胀——037 的 compress 节点（每 5 局压 playerMessages）会把这些指示摘要吞掉。因此：

- planner 的**真正长期演化**必须放在 planner 自己的内部长期记忆（D1，add/replace/remove memory），不能依赖 playerMessages 里的指示文本留存。
- 两者分工：
  - **planner 内部长期记忆**（冻结快照，压缩时刷新）= planner 跨局的认知演化；
  - **planner→player 的 HumanMessage 指示**（进 playerMessages，会被压缩）= 对 player 单局/近期的校准。

---

## 7. 待定项（留待 spec 立项时决策）

1. **产出形态**：开新 spec 目录（如 `specs/0xx-planner-memory-and-calibration/`）走完整 SDD 流程，还是作为 031 的后续修订（031 目录下加 research/plan 增量）。倾向前者——本次是对 031 核心契约（StrategyStore、player/planner 共享长期记忆、FR-013/014/015、super turn 流程）的破坏性变更，clean break 更清晰。
2. **planner 长期记忆的 entry schema 与上限策略**：参考 hermes（字符上限 + 满了报错让 agent 自己 consolidate），具体上限值与 entry 结构留 spec 决定。
3. **planner→player 指示的消息形式细节**：HumanMessage 的具体内容结构、是否标注来源（"来自 planner 的指示"）、desktop planner tab 是否需要可见该指示，留 spec 决定。
4. **方案 b 的冻结缓存实现细节**：缓存持有/清空的时机、与 createAgent 生命周期的关系，留 plan 决定。

---

## 8. 参考来源索引

### LangChain / LangGraph
- [LangChain JS Memory 概念文档](https://docs.langchain.com/oss/javascript/concepts/memory)
- [LangGraph Store 文档](https://docs.langchain.com/oss/javascript/langgraph/stores)
- [LangGraph add-memory](https://docs.langchain.com/oss/javascript/langgraph/add-memory)
- [langchain-ai/memory-agent 模板（hot path 参考）](https://github.com/langchain-ai/memory-agent)
- [langchain-ai/memory-template（background 参考）](https://github.com/langchain-ai/memory-template)
- [CoALA paper（记忆类型映射）](https://arxiv.org/pdf/2309.02427)

### hermes-agent（NousResearch）
- [Persistent Memory 文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- [Skills System 文档](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- [Architecture](https://hermes-agent.nousresearch.com/docs/developer-guide/architecture)
- [Context Compression and Caching](https://hermes-agent.nousresearch.com/docs/developer-guide/context-compression-and-caching)
- [`tools/memory_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py)
- [`tools/delegate_tool.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/delegate_tool.py)
- [`agent/conversation_compression.py`](https://github.com/NousResearch/hermes-agent/blob/main/agent/conversation_compression.py)
- [issue #4319 KV cache invalidation on compression](https://github.com/NousResearch/hermes-agent/issues/4319)
- [issue #8687 timestamp changes after compression](https://github.com/NousResearch/hermes-agent/issues/8687)
- [issue #10880 /model switch memory stale](https://github.com/nousresearch/hermes-agent/issues/10880)
- [issue #17251 Compaction Demotes Memory to Background Reference](https://github.com/NousResearch/hermes-agent/issues/17251)
- [PR #67916 retain prompt cache when memory unchanged](https://github.com/NousResearch/hermes-agent/pull/67916)

### openclaw
- [`src/agents/subagent-initial-user-message.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-initial-user-message.ts)
- [`src/agents/subagent-system-prompt.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-system-prompt.ts)
- [`src/agents/subagent-announce.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-announce.ts)
- [`src/agents/subagent-control.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/subagent-control.ts)

### opencode
- [sst/opencode（anomalyco fork）README](https://github.com/sst/opencode) — build/plan agent + `@general` subagent + Task tool

### dominion 仓库内
- `specs/031-team-template-mode/spec.md`（FR-013/014/015 策略为共享长期记忆；FR-031/032 agent 输入属性）
- `specs/031-team-template-mode/contracts/team-graph-contract.md`（§1 per-agent channel；§2 节点与边；§3 策略/记忆流）
- `specs/031-team-template-mode/contracts/strategy-store-contract.md`（get/put 语义，待废弃）
- `specs/031-team-template-mode/research.md`（D4 策略持久化；D5 per-agent channel；D6 planner 触发）
- `specs/031-team-template-mode/bug-analysis.md`（实现期问题与修复方向）
- `specs/037-saolei-team-optimize/research.md`（D1 压缩方案；D7 压缩节点与路由）
- `projects/game/agent/src/team/player.ts`（player 节点现状）
- `projects/game/agent/src/team/planner.ts`（planner 节点现状）
- `projects/game/agent/src/team/update-strategy.ts`（整体替换语义，待废弃）
- `projects/game/agent/src/strategy-store.ts`（StrategyStore 接口，待废弃）
