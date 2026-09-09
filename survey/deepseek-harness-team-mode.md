# 调研：dsh 支持 saolei team 模式（player + planner 双 agent）

> **状态**：调研完成。**已确认决策（2026-09-08，四轮）**：
> **第一轮**：① planner 是**顶层 agent**而非派发对象——planner 必须拿到原始游戏过程（游戏指令+结果），subagent/委派路径排除（child 输入由 player 的转述/指令决定，压缩失真且目标被 player 左右）。② 双 agent 不共享 history；每 agent 的 history/store 一致性 per-session 独立成立（§4.3）。
> **第二轮**：③ **拓扑拍板——loop 持有多 agent（player/planner 双顶层 agent）**，取代 2026-08-28 的"单 agent 双角色"决策（`survey/deepseek-harness-agent-loop-prereq.md` §5.6，理由：该拓扑与模型更契合）。④ 广播消息与原消息 **1:1**（一条产出消息对应一条广播消息，不聚合）；广播包装**只有发送者标注、无收件人定向**（群聊模型，面向特定成员时在 message 内容内指出，§5.3）。⑤ **team 层独立抽象**：把"群聊"抽为 team 插件，只负责群聊消息同步与团队 prompt（团队介绍/目标/成员职责），不含 saolei 特定逻辑；saolei-loop 经注册 API 注入 player/planner 分工；**agent persona 与团队 prompt 分开管理**（§4.5）。⑥ **角色差异全部由 preset 承载、与物化无关**：工具选择（player 的 saolei-*、planner 的 memory 工具）在**编辑 preset 时**固定，物化 agent 零定制（用户编辑面只有 persona，§3.3）。
> **第三轮**：⑦ **team 只投递不驱动**——广播机制只负责消息收集与投递；**agent 何时执行由 saolei-loop 决定**。投递目标是**成员 buffer**（team 层 per-member 队列），不是 agent log；loop 驱动 agent 时从 buffer 取出消息注入（§4.4）。⑧ **工具调用与结果归属 agent 输出**：player 的 tool 调用+结果作为其输出经 team 广播给 planner（抽象层面是"tools 的调用和结果"这一 agent 通用语义，"saolei 游戏过程"是更高层概念，落到 agent 层经由 tools 实现）——不经 saolei-loop 特殊注入，不破坏 team+群聊模型。⑨ **游戏事件流与 team 消息流解耦**：saolei-loop 持有游戏事件流（saolei-loop 定义、saolei 插件仅提供扫雷游戏实现，二者不做进一步解耦——收益不大）；游戏状态归 saolei-loop/saolei 插件侧，agent 不持有。⑩ 待定项处理：team API 泛化场景无关语义（`gameId`→泛化 context）；preset **分池**（与 player/planner 模型一致）；外置 memory 实现延后至 team 模型确定后单独调研；team section order 频段等关键词定义留到开发时决定。⑪ **工具与配套 prompt 一致性**：插件提供的 tools 与配套 prompt 必须配套使用——选择发生在 preset 的插件行/config 层（决策 ⑥ 的形态）则天然一致；**不得用 per-agent restriction 选工具**（dsh 已知不对称：restriction 不移除 guidance，§3.6）。
> **第四轮**：⑫ **buffer 由 team 自持轻量持久化**——llm 历史（session persistence）已持久化，team buffer 同样持久化（轻量实现，形态开发时定）。⑬ **tool result 原样广播**——保证其他成员看到的 tools 的 input/output 与原样一致（wire 序列化差异不算），不做摘要/引用化。⑭ **插件是 tools 使用的最小颗粒度，preset 实际上是选择插件而不是选择工具**——agent 对工具的引用与 preset 插件行对齐，不存在"插件提供的工具与 preset 插件行不一致"的情况（工具插件按角色划分包边界，行内不再拆分）。其余细节留开发时决定。
> **第五轮**：⑮ **buffer 从成员 log 派生重建（取代 ⑫ 的独立持久化形态）**——team buffer 不是独立事实源，而是各成员 session log 的**派生缓存**：广播条目 = sender log 产出的投影（决策 ⑧ 广播面严格限于成员 log 产出），消费状态 = receiver log 中 `team-broadcast` 消息的 `messageId` 锚点集合。重建即一致（构造即一致，无需独立持久化下的对账检查）；丢失广播重建自愈、已消费条目不重复（**exactly-once 语义天然成立**）。team 轻量持久化缩小为：团队注册事实（成员/goal）与（如需精确跨 log 顺序时）顺序锚点。边界：跨 log 顺序恢复在串行驱动（当前形态）下按驱动轮次/阶段事实推导；并发驱动需顺序锚点。**至此全部核心待定项已决策（除 memory 延后调研），可进入 spec/plan 阶段。**
> **日期**：2026-09-08（同日四轮修订）
> **术语变更**：**saolei-loop 的定义发生层级改变——从 agent loop（agent 驱动层，即替换官方 `dsh-agent-loop` 的那一层）成为 team loop（更高层次的团队编排层）**；agent 驱动沿用传统 agent loop（官方 `dsh-agent-loop` 行保留，§9.3）。前序调研（`survey/deepseek-harness-agent-loop-prereq.md`）中"saolei-loop"一词指其旧含义（agent loop 层的自研替换物），按此变更阅读。
> **前置调研**：`survey/deepseek-harness-agent-loop-prereq.md`（agent-loop 解剖；其 §5.6"单 agent 双角色"决策已被本文头部决策 ③ 取代）、`survey/deepseek-harness-preset.md`（preset 机制）、`survey/agent-team-mode.md`（LangGraph 侧 team mode）、`specs/047-dsh-chat-demo/research.md`（D5 多 session 实证）
> **范围**：双 agent 拓扑下的机制验证与设计输入：preset 池组织与角色差异承载、registry 之上 team 层（群聊模型）/两层分发、LLM 请求中区分角色消息的实现、**插件堆叠基线（§9，后续开发参考）**。**不含** saolei-loop/team spec 的最终方案设计。
> **说明**：本文为调研材料（源码级事实 + 机制结论 + 业界对照）+ 头部所列确认决策；除已标注确认项外不含采用决策。

---

## 1. 背景与调研问题

前序调研（`survey/deepseek-harness-agent-loop-prereq.md` §5.6）曾于 2026-08-28 确认"单 agent 双角色"拓扑。本次调研从"dsh-agent 插件支持一个 agent-loop 里使用两个 agent 作为两个角色"的假设出发，经两轮用户澄清（2026-09-08）**拍板为双 agent 拓扑**（loop 持有多 agent，头部决策 ③），调研问题随之定形：

1. **preset 池的组织方式与角色差异承载**：player/planner 各自 preset 池（每个 preset 天然含本角色固定工具）vs 公用池 + 模板；**角色差异（工具选择、persona）全部在 preset 编辑期固定，物化 agent 零定制**（头部决策 ⑥）。衍生：preset 是插件内容还是框架内容？是否需要自研 preset 插件？
2. **loop 持有多 agent 的机制**：planner 为顶层 agent（与 player 对等）；每 agent 的 history/store 一致性；**loop 层/agent 层两分**——loop 层一份权威 loop history（群聊记录），每个 agent 的 history 是投影视图，新消息由 agent 投递到 loop 层、loop 广播；且群聊层独立为 **team 插件**（只管群聊与消息同步，不含 saolei 逻辑；saolei-loop 经注册 API 注入分工，persona 与团队 prompt 分开管理，头部决策 ⑤）。
3. **LLM 请求中区分两个角色消息的具体实现**：广播与原消息 1:1、只有发送者标注无收件人定向（面向特定成员在内容内指出，头部决策 ④）。

信息源（本地物化 0.1.1-rc.2 源码 + 官方仓库文档 + 业界实践）：

- `node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_*/`（preset roster 服务，README 全文）
- `node_modules/.pnpm/@deepseek-ai+dsh-subagent@0.1.1-rc.2_*/`（subagent seam，README 全文）
- `node_modules/.pnpm/@deepseek-ai+dsh-session-reference@0.1.1-rc.2_*/`（跨 session 引用）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/`（Agent/AgentOptions/AgentRegistry/runtime events 类型）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/`（Message/MessageSource/ContextForm 类型）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*/`（Config `agents[]`）
- `node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_*/`（README：log/surface/deriveMessages）
- `node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_*/`（wire 序列化）
- 业界：[AutoGen 0.2 GroupChat 源码](https://github.com/microsoft/autogen/blob/v0.2.16/autogen/agentchat/groupchat.py)、[AutoGen 0.4 Group Chat 设计文档](https://microsoft.github.io/autogen/dev/user-guide/core-user-guide/design-patterns/group-chat.html)、[Anthropic multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system)、[OpenAI 社区：Responses API 移除 name 字段](https://community.openai.com/t/clarification-on-missing-name-field-in-responses-api-and-handling-multi-persona-multi-user-dialogues/1365804)、[OpenAI Chat Completions API reference](https://developers.openai.com/api/reference/resources/chat)

---

## 2. 机制事实基础（源码级）

以下事实是三个问题的共同依据，全部来自本地物化源码（0.1.1-rc.2 线）。

### 2.1 Agent 与 Session 严格 1:1

- `Agent.id: SessionId`——"The single identity shared with `session`"（`dsh-agent/lib/types/runtime-types.d.ts`）；`Agent.session: Session`——"The live session this agent drives; its log is the durable source of truth"。
- `AgentRegistry.enter(agent, owner)` 显式断言 `agent.id === agent.session.id`（`dsh-agent` README "Advanced ordered lifecycle" 节）。
- `dsh-session` README 第一句："A `Session` is the append-only source of truth for **an agent's** whole interaction history"。
- **推论**：不存在"一个 session 被两个 agent 驱动"；一个 dsh agent = 一个 session = 一个事件日志 = 一个模型请求历史。多 agent 必然多 session。

### 2.2 registry 天然多 agent（服务级）

- `AgentLoop.Config.agents[]`：声明式多 agent 配置，每行 `{id, sessionId?, cwd?, resumeSessionId?} & AgentOptions`，"Agents created or resumed at plugin startup"（`dsh-agent-loop/lib/types/index.d.ts`）。
- registry 为 `Map<SessionId, AgentEntry>`、每 agent 独立 fiber，并发会话一等公民（047 D5 实证，`specs/047-dsh-chat-demo/research.md`）。
- `AgentOptions` 仅 `provider/model/maxTokens`，**merge-extensible**（"Persona belongs to system-prompt sections"——persona 不在 AgentOptions）。
- **一个 AgentLoop 服务实例同时驱动 N 个 agent 是官方形态**；但每 agent 一个 `ReactLoopAgent` 驱动实例（inbox/turn/step 状态机 per-agent）。

### 2.3 preset 机制：独立插件 + 数据文件，挂载模型 N agents : 1 preset

（`dsh-agent-presets` README 全文 + `survey/deepseek-harness-preset.md`）

- **归属**：preset **机制**由独立插件 `dsh-agent-presets` 提供（host 平面 Service，ctx key `agentPresets`：`list/resolve/mount/composeFrom/recompose/standingKeyFor/read/copy/remove`）；preset **文件**是数据（一个目录 + 一个 `agent.cordis.yml` + 可选 `preset.yml` 展示元数据）。既非 cordis 框架内容，也非 dsh-agent/dsh-agent-loop 内容。
- **挂载模型**："the roster mounts it **ONCE per process** under a standing scope, and each session that names it **joins** by having its agent scope key parented to the mount's"——同 preset 的多个 agent 共享一份 standing mount（工具/prompt sections/projection units 只存在一份），插件状态按 Session/Agent key 保持隔离。工具/prompt 的 scope 解析链：`agent → preset → global`（近遮蔽远）。
- **一个 agent 恰好一个 preset**：`mount(agentCtx, id)` 唯一支持调用点是 agent factory 的 `setup(agentCtx)` hook；切换（`recompose`）仅限 blank agent，且"two compositions cannot coexist — both would register the same tool names into one layer"。
- preset id 是 durable 事实：`CreateAgentOptions.meta.agentPreset` 记入 SessionHeader；blank 切换落 `agent-preset/selected` 事件。
- **无 roster 部署（SDK/直组形态，B1/demo 即是）**："A deployment composing no preset roster joins nothing and needs nothing: its model-facing rows sit in the host composition, where the child already resolves them through the tool registry's global layer"——model-facing 行直接放 host 组合，所有 agent 经工具 registry 全局层共享。

### 2.4 subagent seam：机制事实（已排除用作 planner 形态，保留为排除依据与 team 层结构参照）

> planner 已确认为顶层 agent（头部决策 ①），subagent 不用作 saolei 的角色形态；本节机制事实保留两个用途：§3.4 的排除理由（child 输入由 parent 委派请求决定）与 §4.1 的结构参照（continuation manager 是"registry 之上编排器"的官方实现，saolei 的 team 层取其结构形态）。

（`dsh-subagent` README 全文）

- 一个 agent 经 named provider 委派 child：`ctx.subagents.start()`（one-shot）或 `startContinuable()`（持续 child：一个 durable Session + 至多一个进程内 Activation，parent→child 走 `followup()` FIFO turn，child→parent 走 `reportFrom()`）。
- **child 组合**：`applyChildComposition(childCtx, parent, composition)` 一次调用完成——先 `composeFrom` 加入 parent 的 standing composition，再应用 child 自己的 **`persona`（per-child persona）与 `toolFilter`**。即：**角色差异（persona/工具可见性）是 subagent 请求参数，不是 preset 内容**。
- child capabilities：`outputSchema/depthLimit/toolFilter/persona`。
- `CreateAgentOptions` 的 `meta.parentSession/seedLength/origin:'subagent'/delegationDepth` + 可选 `seed`（fork 时复制 parent 的"balanced completed-turn prefix of the parent's log"）——**fork 是"继承历史"的官方形态**。
- **continuable child 的编排不在 loop 里**：continuation manager 持有 child 的 `AgentHandle`、管理 residency（running/waiting/settled）、cold resume、settlement delivery。它是 **registry 之上的编排器**，不修改任何 Agent 的驱动。

### 2.5 跨 agent 信息通道：全部收敛到 user-role 消息 + provenance

- `Agent.send/followup/steer/inject` 的入参类型全部是 `UserMessage`（`dsh-agent` runtime-types）——**进入一个 agent inbox 的一切输入都是 user-role 消息**。
- `dsh-llm` 的 `Message`：`role: 'system' | 'user' | 'assistant'`，**无 speaker name 字段**；来源区分在 `source: MessageSource`（merge-extensible：`user/plugin{plugin,form}/model{provider,model}/tool{callId}`，dsh-subagent 扩展了 `subagent-settled{senderSessionId}`）与 `ContextForm`（`instructions/catalog/snapshot/notice/relay/recall`——`relay` 语义即"A message another agent addressed to this one"，`recall` 即从其他 session log 提取的内容）。
- `dsh-session` README："its typed `source` is the only channel that tells them apart"——direct human prompt / synthetic injection / goal round 落 log 后**只靠 source 区分**。
- 三个官方跨 agent/跨 session 通道：
  1. **UserMessage 注入**：subagent settlement notice——"One **user-role parent message** … followed by … the child's final assistant content … durable provenance `{ kind: 'subagent-settled', form: 'notice', senderSessionId }`"（与 `survey/deepseek-harness-agent-loop-prereq.md` §5.3 的挂点表一致）。
  2. **fork seed**：child 创建时复制 parent log 前缀（历史继承）。
  3. **`dsh-session-reference`**：`@[label](dsh-session:...)` mention → "two consecutive **user-role messages**"（当前消息 + `## Referenced sessions` 只读快照，source `{kind:'session-reference'}`，有注入防护：`<referenced-sessions>` JSON 序列化 + 不可信警告）。
- **wire 层**（`dsh-llm-deepseek` adapter 源码）：assistant 消息序列化只带 `role/content/tool_calls`，无 name；`source`/`form` 不进 wire——speaker 标识是 durable log 层的元数据。

### 2.6 history 与"store"的关系澄清（用户问题 2 的概念前提）

dsh 没有独立于日志的"store"概念（LangGraph 的 Store/checkpointer 二分在 dsh 不存在对应物）。实际分层：

| 层 | 内容 | 关系 |
|---|---|---|
| **Session event log**（append-only，持久化后端可选 jsonl/sqlite） | 全部事件：`user/message`、`assistant/chunk`、`assistant/message`、`tool/call`、`tool/result`、`turn/*`、`step/*`、`request/header`、merge 扩展事件（`agent/inbox/spliced`、`subagent/descriptor`、`compaction/*`…） | **唯一事实源** |
| **surface 投影** | log 上维护的"产生消息的事件"有序投影，供高效派生与 compaction | 派生物 |
| **model history** | `session.deriveMessages()` 从 surface 增量投影出的 LLM 请求 messages | 派生物；"Model-visible means logged"不变量 + agent-loop invariant 逐字节校验请求与派生一致 |
| **compaction** | `dsh-compaction-basic`（摘要 checkpoint 以 replacement `user/message` 落 log）、`tool-result-pruner`（内容替换 `tool/result`） | log 内 replacement entries，非删除 |

用户理解"对话历史和 store 分开存储、只有进入 store 的才会进入 llm 请求"**方向正确**，准确表述是：**log（durable store）是唯一事实源，llm 请求的 history 是派生投影；模型可见的必先落 log**。双向推论：双 agent = 两个 log，各自派生各自的请求 history，**没有"共享 history"机制**，跨 agent 只有 §2.5 的三条通道。

---

## 3. 调研问题 1：preset 池的组织方式与物化维度

### 3.1 结论总表

| 组织方式 | 机制判定 | 说明 |
|---|---|---|
| **分池**：player/planner 各自 preset 池，每个 preset 天然含本角色固定工具 | ✅ 直接支持 | roster 的多 roots 机制 + 目录即 preset；"池"不是 dsh 概念，用多个 root 目录（或单池 id 前缀）表达（§3.2） |
| **公用一个池 + preset 模板**（"preset 的 preset"，固定一部分配置） | ⚠️ 半支持 | dsh preset 层**无继承/模板语义**——官方立场是全量拷贝（copy 即 authoring 路径），patch 语义刻意留在 bundle 层（进程级）；"固定一部分"需上移到代码/host 层表达（§3.3） |
| team preset（一个 preset 同时包含 player 和 planner） | ❌ 不成立 | preset 原子单位是"一个 agent 的组合"；且 planner 已确认为顶层 agent，subagent 变体一并排除（头部决策，§3.4） |

### 3.2 分池：dsh roster 的直接支持

`dsh-agent-presets` Config 的 `roots` 是**有序目录数组**（"Scanned directories in precedence order"，先命中者胜）：

```yaml
# 分池示例：两个 root 目录即两个池
roots:
  - path: ~/dominion/presets/player    # 池内每个 preset 都写 player 工具行
    trust: user
  - path: ~/dominion/presets/planner   # 池内每个 preset 都写 planner 工具行
    trust: user
default: saolei-player-balanced
```

- preset id 即目录名（`[a-z0-9][a-z0-9-]*`），`list()` 返回**平铺列表**（无分类/标签/分组概念）——"池"的语义由 root 目录边界（或 id 前缀约定，如 `player-*`/`planner-*`）承载，选择时按前缀过滤是调用方逻辑。
- "工具对角色固定"在分池下的落实：**每个 preset 目录写同一行共享工具插件**（相对/绝对路径引用同一个包，或裸包名从 host 组合解析）。机制上无强制——一个 player preset 漏写该行则该会话缺工具；固定性靠"同池必含此行"的约定 + review 保证，preset 层没有"必须有"的约束原语。
- 同池多 preset（如 player 按难度/风格多套 persona）完全支持：每 session 创建时选一个，blank 前可 `recompose` 切换。

### 3.3 公用池 + 模板："preset 的 preset"在 dsh 的对应物（半支持）

用户设想的"一个池子 + 每角色一个 preset 模板（固定一部分配置）"，在 dsh 有三层候选对应物：

| 候选 | 机制 | 与"固定一部分配置"的差距 |
|---|---|---|
| **全量拷贝**（官方 authoring 路径） | `ctx.agentPresets.copy(from, id)` 整目录拷贝后各自编辑 | 拷贝是快照会漂移（"A copy is a snapshot that drifts"——官方 cordis/code preset 就是 standard 的全量拷贝）；改共享部分（如工具行升级）要同步改每个副本，"固定"退化为"约定" |
| **bundle patch**（`cordis.patch.yml`） | bundle 层的 patch 语义，官方明示"express 'standard plus one change'"的位置 | **patch 在 profile/bundle 层（进程级组合），不在 preset 层（会话级）**——能给"整个进程的所有 preset"打补丁，不能表达"player 池的 preset 都基于 X 模板"这种池内模板 |
| **代码固定层**（直组形态） | 角色固定的部分（工具集、基础 sections）在 `CreateAgentOptions.setup(agentCtx)` 或 host 组合层用**代码**注册；preset/config 只承载可变部分（persona 文本、模型参数） | 最贴合"固定一部分"语义——代码不可配置漂移；但这就不是 preset 层的表达，而是"preset 管可变、代码管固定"的分工 |

**判定（2026-09-08 用户确认，头部决策 ⑥）**：**角色差异全部由 preset 承载、与物化无关**——工具选择（player 的 `saolei-*` 工具行、planner 的 memory 工具行）在**编辑 preset 时**固定，物化 agent 时零定制（`create(presetId, AgentOptions)`，无 per-role setup 分支）；preset 的用户编辑面收敛到 persona。机制对应：

- **编辑期固定**：preset 目录的工具行即角色工具集（§3.2 分池天然承载）；"模板 preset + copy 后只编辑 persona"与官方 authoring 路径（`copy()` 即"duplicate, then edit"）吻合——固定性是约定级（preset 层无"必须有"约束原语），由 saolei 侧 authoring 流程（模板生成/copy 引导）保证，用户编辑面不暴露工具行。
- **物化零定制**：物化参数只有 preset id 与 model route（`AgentOptions`）；两个角色的 create 调用**同构**（差异全在 preset 内容）。team 接线（注册进团队，§4.5）对所有成员对称，不属于 per-role 定制。
- 由此 saolei 需要引入 preset roster（挂 `dsh-agent-presets` 行）；分池与单池+模板的取舍（§3.2/§3.3 前半）留待 spec，两者都满足"编辑期固定、物化零定制"。

### 3.4 team preset 为什么不成立（记录排除理由）

preset 的原子单位是"**一个 agent 的组合**"：一个 `agent.cordis.yml` 挂载到一个 agent scope，产出一份工具目录 + 一个 persona + 一组 prompt sections；一个 agent 恰好一个 preset（两个组合不能共存，§2.3）。"一个 preset 同时包含 player 和 planner"需要 preset 内声明两个角色、两次挂载——机制不存在。官方"一份组合文件覆盖多角色"的唯一形态是 subagent delegation 行（child 组合 = parent 组合 + per-child persona/toolFilter 委派参数），但 **planner 已确认为顶层 agent**（头部决策 ①）：subagent 路径下 planner 的输入由 player 的委派请求决定（压缩失真 + 目标被 player 左右），与"planner 拿原始游戏过程"的要求冲突，整条路径排除。

### 3.5 preset 的归属与"落点"问题（用户的核心疑虑）

- **preset 是插件内容还是框架内容**：preset **机制**是插件（`dsh-agent-presets`，host 平面 Service）；preset **文件**是数据。它既不在 cordis 框架核心，也不在 dsh-agent/dsh-agent-loop——与 agent-loop 是**平行的两面**（preset = agent 平面的组合清单；agent-loop = 驱动实现），不存在上下层依赖。
- **是否需要自研 preset 插件**：不需要。挂官方 `dsh-agent-presets` 行（config：`default`/`roots`）即可获得完整 roster/mount/recompose/copy 能力。需要自研的是 team 插件与 saolei-loop（§4.5）。
- **"若需要实现比 agent-loop 更底层的插件，saolei 落点就不能是 agent-loop"——该前提不成立**：不存在需要自研的"更底层 preset 插件"。team 模式的落点问题是编排逻辑放哪：registry 之上的 team 层（双 agent，§4），而不是 loop 内部。

### 3.6 工具与配套 prompt 的一致性（用户补充 1 的验证）

用户预期：插件提供的 tools 与配套 prompt 一致、配套使用——player preset 只选 `saolei-*` 工具则只注入 saolei 插件提示词；planner 只选 memory 工具则只注入 memory 插件提示词。**dsh 的所有权模型恰好按此设计，但有一个关键前提：选择必须发生在插件行（或行 config）层，不能发生在 per-agent restriction 层**。

| 选择方式 | 工具 schema | 配套 prompt section | 一致性 |
|---|---|---|---|
| **preset 行级选择**（决策 ⑥ 的形态：player 池的 preset 只写 saolei 工具插件行，不写 memory 行） | 插件不挂载 → 工具不存在（absence） | 同一插件的 `apply()` 内注册，插件不挂载 → section 不存在 | ✅ **天然一致**（工具与 guidance 同插件同生命周期，`survey/deepseek-harness-preset.md` §6.4"every fact in the prompt has exactly one owner"） |
| **行 config 开关**（spine 先例：`toolBash: false`——行级 config 整体关闭某工具族） | 同一插件代码路径，config false → 不注册工具 | 同一开关路径 → guidance 一并关闭 | ✅ 一致（开关在插件内，工具与 guidance 走同一分支） |
| **per-agent restriction**（`dsh-tools` 的 scope 限制：工具全注册、per-agent 限制可见性） | 从该 agent 的模型可见目录中排除 | **仍然注入**——`dsh-system-prompt` README 原文："Sections and schema providers are separate assembly inputs, so **a tool restriction does not remove independently registered guidance**" | ❌ **不一致**（player 会被限制掉 memory 工具却仍看到 memory 守则） |

对 saolei 的判定：

1. **决策 ⑥（工具选择在编辑 preset 时固定）恰好落在一致的一侧**——player preset 只写 saolei 工具插件行、planner preset 只写 memory 工具插件行，工具与配套 prompt 自动配套；物化零定制不引入 restriction，坑不触发。
2. **插件即工具使用的最小颗粒度（头部决策 ⑭）**：preset 实际上是**选择插件**而不是选择工具——工具插件按角色划分包边界（player 工具插件 / planner 工具插件各自提供工具+guidance），插件行内不再拆分；agent 对工具的引用与 preset 插件行对齐，"插件提供的工具与插件行不一致"的情况按此边界设计即不存在。
3. 验证方式提示：一致性可由"minimal 封口"同型的审查确认——挂某工具行后 system prompt 装配结果应恰好含该工具的 guidance，不含未挂载插件的任何 section（装配管线按 scope 收集，未挂载即无来源，`survey/deepseek-harness-preset.md` §6.2）。

---

## 4. 调研问题 2：两个顶层 agent 的机制、history/store 一致性与两层分发模型

### 4.1 "loop"的两层含义，分别回答

**服务级（AgentLoop 插件 / ctx.agents registry）——✅ 天然支持**：

- 官方 `AgentLoop` 本身就持有多 agent：`agents[]` config 启动创建多个，registry `Map<SessionId, AgentEntry>` 并发一等公民（§2.2）。047 demo 的 get-or-create 即多 session 并存实证。
- 自研 saolei-loop 若替换 factory（`ctx.agents.setFactory`），同样可以在其 config/setup 里创建两个顶层 agent（player/planner 各一个 handle，对等、无父子关系）。

**驱动级（ReactLoopAgent / 自研单 agent 驱动循环）——❌ 不是官方形态**：

- 每 agent 恰好一个驱动实例；`Agent` 与 `Session` 1:1（`agent.id === session.id` 强制，§2.1）；inbox/turn/step 状态机、abort 传播、turn 编号全部 per-agent。
- **"一个驱动循环交替驱动两个 agent"在官方代码中没有先例**。官方处理"一个逻辑单元内多 agent"的现成实现是 `dsh-subagent` 的 **continuation manager**：它持有多个 `AgentHandle`、管理 residency（running/waiting/settled）、路由 parent↔child 消息、cold resume——**它是 registry 之上的编排器，不进入任何 Agent 的驱动循环**。saolei 的 team 层（§4.5）应取其结构形态（持有成员 AgentHandle + 投递规则），但为平级拓扑（无 parent/child 语义）。
- 单 session 双 agent（一个 log 两个 driver 写）**被核心不变量阻止**：`deriveMessages` 的 request-reconstruction 不变量（请求 messages 与日志派生逐字节一致，`survey/deepseek-harness-agent-loop-prereq.md` §4.6）假设单一视角；两个 driver 的 assistant 流交错同一 log 无法锚定请求重建。

### 4.2 双 agent 的架构形态（拓扑已确认）

```
┌ team 层（team 插件，registry 之上——平级拓扑；只投递、不驱动，§4.4/§4.5）─┐
│ 群聊原语：成员注册、消息收集（发言 + tool 调用/结果）、1:1 广播投递、      │
│ team section、广播顺序权威                                               │
│   player buffer ──┐                     ┌── planner buffer              │
│ ┌ saolei-loop（游戏阶段机：局开始/结束、复盘触发；驱动权唯一归属，§4.4）─┐│
│ │ 经 team 注册 API 注入分工；决定何时驱动哪个 agent 并消费其 buffer      ││
│ └──┬─────────────────────────────────────┬─────────────────────────────┘│
└─────┼─────────────────────────────────────┼──────────────────────────────┘
      │ 驱动时取出 buffer 消息注入           │
┌─────▼──────┐                       ┌──────▼─────┐
│ player Agent│                       │ planner Agent│  ← 各自 ReactLoopAgent
│ session log │                       │ session log  │  ← 各自 append-only log
└─────────────┘                       └──────────────┘
   llm 请求 = deriveMessages(log A)         llm 请求 = deriveMessages(log B)
   （工具调用+结果落 player log，作为其输出被 team 收集广播 → planner buffer）
```

- planner 与 player 对等：各自独立的 log、inbox、驱动、`AgentOptions`（model route 各自声明，无需 `agent/request` waterfall 替换）；persona/工具可见性由各自 preset 承载（§3.3），物化零定制。
- planner 拿**原始游戏过程**的路径（头部决策 ⑧）：player 的 `tool/call`/`tool/result` 落 player log——**工具调用与结果是 player 的输出**——team 收集并 1:1 广播进 planner buffer；loop 驱动 planner 时取出注入。第一手、无 player 转述压缩（头部决策 ① 的理由由群聊通道满足，无需 saolei-loop 特殊注入）。

### 4.3 history 与 store 的一致性：双 agent 下独立成立

直接回答用户问题（澄清后的问题面：不共享 history，关注每 agent 的 history/store 一致性）：

- **一致性机制是 per-session 的，双 agent 不引入任何新风险**。dsh 中每个 agent 的 model history 不是独立存储，而是每 step 从自己的 session log 增量派生（`deriveMessages()`，§2.6）——history 是 log 的纯函数投影，**不存在"两份需要同步的数据"**。
- **一致性由官方 invariant 强制**：`dsh-agent-loop/invariant` 伴生在 `llm/stream` 断言每个请求的 messages 与 `session.deriveMessages()` **逐字节一致**（log-reconstruction desync 检测）、请求 frozen、携带 live session id（`survey/deepseek-harness-agent-loop-prereq.md` §4.6）。任何不一致 fail loud。自研 loop 若改动"请求从日志派生"环节，需提供自己的 invariant 伴生（同源调研 §4.7）。
- 双 agent = 该不变量在两个 session 上各自独立成立；消息注入只发生在 **loop 驱动消费 buffer 时**（`followup()` 进入目标 agent inbox 后**先落目标 log（`user/message`）再进请求**）——投递路径本身不提供绕过 log 直改 history 的通道，一致性结构上不可破坏。
- 唯一的纪律点：team 层/saolei-loop **不得**构造绕过 inbox 的消息路径（例如直接拼请求 messages）——dsh 类型系统也不存在该入口（`send/followup/steer/inject` 是唯一投递面，入参全为 UserMessage）。

### 4.4 loop 层 / agent 层两分：群聊模型与 buffer 投递

用户的设想：agent 层每人各自历史（视图）；loop 层一份权威 loop history（群聊记录）；新消息由 agent 投递到 loop 层、loop 广播。**广播职责由 team 层承载，且 team 只投递不驱动**（头部决策 ⑦）：投递目标是**成员 buffer**（team 层 per-member 队列），agent 何时执行由 saolei-loop 决定——驱动时从 buffer 取出消息注入 agent。dsh 映射：

| 群聊概念 | dsh 对应物 | 说明 |
|---|---|---|
| 群聊权威记录（一份） | **team 层消息流**：team 插件维护的群聊记录（成员产出——发言 + tool 调用/结果——与广播事实）；saolei 特定的游戏事件流由 saolei-loop 持有（决策 ⑨，两者解耦） | 权威记录不是"LLM 消息级合并 log"——不塞进任何 agent 请求；形态是结构化事实（`survey/agent-team-mode.md` §2.2 同型：gameState 是工具执行副产品，LLM 不参与同步） |
| 各成员 client 历史（视图） | 各 agent 的 session log → `deriveMessages()` | 每个视图自洽且与自己的 log 一致（§4.3）；**视图 = 群聊记录在"该成员被驱动时实际消费的消息"上的投影**——未消费的 buffer 消息不进视图；视图间一致的是因果（广播顺序），不是内容 |
| 成员发言 → 群聊记录 | agent 产出落**自己的 log**（`assistant/message`、`tool/call`/`tool/result`），team 订阅成员 `session/event` 收集（头部决策 ⑧：tool 调用与结果是 agent 输出的一部分） | planner 策略产出与 player 的工具调用+结果都走这一通道 |
| 群聊记录 → 广播成员 | team 合成广播条目（发送者标注 + source provenance，**与原消息 1:1**）→ 投入**除发送者外每个成员的 buffer** | buffer 是 team 层持有的 per-member 队列；**不直接写 agent log**（头部决策 ⑦） |
| 成员何时读消息 | **saolei-loop 决定驱动某成员时**，从该成员 buffer 取出消息 → 构造 UserMessage → `followup()`（驱动 turn）注入 agent | 消息此刻才落 agent log、进该 agent 的请求历史——"群聊里轮到你才读"；驱动权唯一归属 loop |

**模型的四个机制要点**：

1. **唤醒语义张力被 buffer 消解**：广播不直接驱动 agent（不调用 followup/inject 于广播时），上一轮记录的"followup 每条独占 turn / inject 可丢"的取舍不复存在——取舍上移到 loop 的驱动策略（何时驱动、驱动时消费哪些 buffer 消息），team 是纯消息基础设施。
2. **buffer 语义**：team 层 per-member 队列。未消费的 buffer 消息**不在任何 agent log 中**（"Model-visible means logged"按目标侧语义成立——只有驱动消费时落 log）；**buffer 是各成员 log 的派生缓存而非独立事实源**（头部决策 ⑮，§4.4a）。成员 dispose 时 buffer 随团队注册事实清理。
3. **1:1 粒度贯穿 buffer**：一条原消息（发言 / 一次 tool 调用+其结果）= 一条 buffer 条目 = 驱动时一条注入消息；不聚合。驱动一次可消费多条 buffer 条目（多条独立 UserMessage 同 turn 注入——`agent/pre-step` 的 enter 决策携带一批 messages 是官方语义）。
4. **时序权威**：广播顺序（"群聊记录"顺序）权威在 team 层；各成员 log 内因果序由各自 inbox/turn 状态机保证。全局序号不存在，需要时由 team 层在广播条目/source 中携带序号或时间戳。

**由此消息流完整闭环**（一局游戏）：

```
player 被驱动 → 调 saolei-* 工具 → tool/call+tool/result 落 player log
  → team 收集（player 输出）→ 1:1 广播进 planner buffer
局结束（saolei-loop 阶段机判定，依据其持有的游戏事件流）
  → loop 驱动 planner：取出 buffer（本局全部工具调用+结果条目）注入
  → planner 复盘产出 assistant/message 落 planner log
  → team 收集 → 1:1 广播进 player buffer
下一局开始 → loop 驱动 player：取出 buffer（策略消息）注入 → 执行
```

### 4.4a buffer 的持久化形态：从成员 log 派生重建（头部决策 ⑮）

buffer 持久化**不走独立存储 + 对账检查**路线（独立事实源必然引入"重建后 buffer 与 agent log 匹配"的一致性检查义务），而是**从成员 log 派生重建**——buffer 是派生缓存，不是事实源。这与 dsh 的核心模式同构（"LLM message history is *derived* from the log"、"不存在两份需要同步的数据"，§4.3）：**team-visible means logged**——team 能广播到的必先落成员 log。

**重建算法**（对每个成员 M 的 buffer）：

1. **条目来源**：收集其他成员 log 的产出事件——`assistant/message`（发言）与 `tool/call`+`tool/result`（按 callId 配对为一条广播单元）——投影为广播条目（加 sender 标注，格式 §5.3）。机制依据：同 log 内 seq 单调、turn/step 包围、same-step tool call/result 配对均为官方 invariant（`dsh-session/invariant`，重放校验）；`MessageId`/`CallId` "stable identity preserved across every representation boundary"。
2. **消费状态**：读 M 自己 log 中 `source.kind === 'team-broadcast'` 的 `user/message` 集合，取其 `messageId` 锚点——**已消费的条目（messageId 命中）不进 buffer**。§5.3 第 2 层的 `messageId` 字段由此承担双重角色：1:1 追溯锚 + 消费状态锚。
3. **顺序归并**：串行驱动（当前形态：loop 驱动权唯一，player/planner 驱动时段不重叠）下按驱动轮次/阶段事实归并即可；事件级时间戳可作辅助。

**由此获得的性质**：

- **重建即一致**：buffer 由 log 构造，无双事实源对账——用户提出的一致性检查问题被结构消解。
- **exactly-once 天然成立**：崩溃时丢失的广播（sender log 已有产出、未进 buffer）在重建时自动补齐（自愈）；已消费条目被 messageId 锚点排除（不重复）。比 at-least-once + 重放兜底更强。
- **读取面有官方先例**：live session store + optional persistence 直读（`dsh-subagent` `listChildren` 同模式："Reads the live session store and optional session persistence directly"）。

**team 持久化面缩小为**：团队注册事实（成员列表/goal——不在任何 agent log 中，可由 saolei-loop 重建时重新 register 兜底）与（未来并发驱动时的）跨 log 顺序锚点；buffer 本体不持久化。

**边界**：派生前提是 team 广播面**严格限于成员 log 产出**（决策 ⑧ 已保证——游戏事件流与 team 消息流解耦，saolei-loop 不经 team 广播游戏事件）；并发驱动形态下精确广播顺序需 team 自持顺序锚点（当前串行驱动不触发）。

### 4.5 team 层独立抽象（team 插件）：职责边界与机制落点

用户决策（头部决策 ⑤）：把"群聊"抽为独立的 **team 插件**，只负责群聊部分与消息同步，不处理 saolei 特定场景；saolei-loop 经注册 API 将 player/planner 分工注入；**agent persona 与团队 prompt 分开管理**。

**职责边界**（team 只投递不驱动，头部决策 ⑦）：

| 关注点 | team 插件（通用群聊原语） | saolei-loop（特定编排） |
|---|---|---|
| 成员管理 | 注册团队 API（成员 agent + 角色 label + 目标文本） | 调用注册 API 注入 player/planner 分工 |
| 消息收集 | 订阅成员 `session/event`：`assistant/message`（发言）+ `tool/call`/`tool/result`（工具调用与结果——agent 输出的一部分，头部决策 ⑧） | —— |
| 广播投递 | 1:1 合成广播条目（发送者标注 + source）投入除发送者外成员的 **buffer**；广播顺序权威 | —— |
| **agent 驱动** | **——（team 不驱动任何 agent）** | **唯一归属**：决定何时驱动哪个成员、消费其 buffer 中哪些消息（构造 UserMessage `followup()` 注入） |
| 团队 prompt | 团队介绍/目标/成员职责 section（统一渲染，注册进每个成员） | —— |
| 角色人格（persona） | —— | 由 preset 承载（§3，编辑期固定） |
| 游戏事件流（状态机事实：局开始/结束、胜负） | ——（team 不含 saolei 逻辑） | **saolei-loop 持有**（决策 ⑨；saolei 插件仅提供扫雷游戏实现，事件流与 team 消息流解耦） |
| 阶段机（局开始/结束、复盘触发） | —— | saolei-loop 持有 |

**注册 API 形态（示意，spec 定形）**：team 插件提供服务（如 `ctx.team`），`register({ goal, members: [{ agent, role }] })`——场景无关（头部决策 ⑩：泛化语义，不携带 saolei 概念；广播条目的关联键用泛化 `context` 字段而非 `gameId`）；buffer 读取面 `drain(member)` 或等价形态供 loop 驱动时消费。这是标准的 dsh Service 提供模式（插件为插件提供能力，`survey/deepseek-harness-agent-loop-prereq.md` §5.1 机制同构——`ctx.saoleiGame`/`ctx.desktopBridge` 先例）。

**persona 与团队 prompt 分开的机制落点**（利用 dsh system prompt 装配管线，`survey/deepseek-harness-preset.md` §6.2）：

| 内容 | 注册者 | 注册位置 | order 频段 |
|---|---|---|---|
| harness identity | `dsh-system-prompt` | 全局层 | -100（固定） |
| **persona（角色人格）** | preset（persona 行） | preset 层 | 0（唯一插槽） |
| **team section（团队介绍/目标/成员名册/协作规则）** | team 插件 | **每个成员的 agent scope**（经 `agent.ctx` 注册，内容同源统一渲染） | spec 定形（如 1–49 频段，persona 之后、工具守则之前） |
| 工具守则 | 各工具插件 | preset/全局层 | 100–199（固定） |

- **team section 不进 preset**：进 preset 会把团队事实分散进各角色副本（copy 漂移问题，§3.3），与"team 层统一管理团队 prompt"矛盾；**不进 host 全局层**：会进所有 agent 的 prompt（包括未来非成员 agent）；**per-agent scope 注册**是精确对应物——team 插件持有成员列表，经成员 `agent.ctx` 注册（`agent.ctx` 的贡献 agent-local、随 agent dispose 自动 unwind；agent 存活期间可注册，unpublished setup 阶段注册是官方组合期）。
- **前缀稳定性**：team section 内容在成员注册后固定（目标/名册不变），排序频段固定 → 各成员请求前缀稳定（KV cache 友好）；成员中途增减会失效前缀，但 saolei team 成员在一局内固定。
- 由此 system prompt 的三个来源各司其职：**preset 管角色（persona+工具），team 插件管团队（名册+协作规则），system-prompt 管框架（identity）**——persona 与团队 prompt 的分开管理在机制上是自然表达，无需新机制。

---

## 5. 调研问题 3：LLM 请求中区分两个角色消息的具体实现

### 5.1 dsh 的机制边界（为什么不能都用 assistant）

wire 层只有 `system/user/assistant` 三种 role，且无 speaker `name`（§2.5）。dsh 的角色区分是分层设计：

| 层 | 机制 | 表达 |
|---|---|---|
| durable log | `MessageSource`（merge-extensible）+ `ContextForm` | `model{provider,model}` / `plugin{plugin}` / `subagent-settled{senderSessionId}` / …；`relay`（"A message another agent addressed to this one"）/`recall` form |
| 模型可见投影 | 消息以 role + content 进入 `deriveMessages()` | **对方角色的话 = user-role 消息**；speaker 标识进入消息文本与 source（source 不进 wire） |
| wire | role + content（`ContentBlock`，`TextBlock{type:'text',text}` 是基本块）+ tool_calls | 无 name；DeepSeek adapter 实证（§2.5） |

**"两个 assistant 视角交错一个 history"在 dsh 类型系统里不可表达**：进 inbox 的输入类型只有 `UserMessage`（§2.5）；assistant 消息的唯一产生面是本 agent 的模型流（`assistant/chunk` 折叠为 `assistant/message`，由 loop 构造，带 provider/model provenance）。因此角色区分的实现落点是：**user-role 承载 + content 文本内嵌说话者标注 + log 层 source 记录 provenance + system prompt 声明格式约定**——四层配合，而非扩展 role。

### 5.2 业界对照（佐证选型）

| 做法 | 代表 | 机制 | 现状与评价 |
|---|---|---|---|
| **wire 层 speaker 字段**（name） | OpenAI Chat Completions、AutoGen 0.2 | message `name` 字段 | **Responses API 已完全移除且无回归承诺**；AutoGen 0.2 用它（`GroupChat.append` 设 `message["name"] = speaker.name`），**0.4 重构后放弃**。不可作为长期依赖 |
| **content 前缀/标签标注** | AutoGen 0.4 selector、OpenAI 社区建议 | 文本行 `"{source}: {content}"` | 模型可读但本质是不可信标注；适合群聊主持人场景 |
| **独立 history + user-role 转述** | **AutoGen 0.4**、**Anthropic subagent**、**dsh** | 每 agent 独立 history；他人消息以 user-role 注入自己视角 | **主流收敛方向**（AutoGen 0.4：他人消息以 `UserMessage(source=sender)` 入自己 history；Anthropic：subagent 结果以 tool result + user notice 回 parent） |
| **tool_call/tool_result 边界** | Anthropic Task tool、Claude Code、dsh subagent | 委派是工具调用，tool 名即角色名 | 适合委派语义；saolei planner 已确认为顶层 agent，不适用 |

### 5.3 具体实现设计（team 广播消息）

team 层收集成员输出后广播（进 buffer），loop 驱动成员消费 buffer 时构造合成 UserMessage 注入——格式设计覆盖这条链路的最终形态（广播条目与注入消息同构）。**两条已确认约束（头部决策 ④）：与原消息 1:1（一条产出对应一条广播，不聚合多塞）；包装只有发送者标注、无收件人定向（面向特定成员在 message 内容内指出）**。

**第 1 层：wire content 文本格式**（进 LLM 请求的唯一载体）。采用"发送者标注行 + 标签包裹正文"结构，仿官方 settlement notice（"Background subagent <id> finished … Its closing message:"）与 session-reference（`## Referenced sessions` + 标签包裹）的既有模式：

```text
[planner] 局后复盘策略（game #3）
<planner-message>
基于本局记录（15 步，负于 3x4 处雷）：
1. 开局固定走中心 5x5 区域…
2. 遇 1-2 边界优先沿已揭示区推进…
@player 请在下局执行以上开局策略。
</planner-message>
```

- 首行 `[sender] + 摘要`：仅发送者标注（群聊语义——消息属于群，不属于某个收件人）；摘要对应官方 `notice` form 的 `summary` 字段实践（官方上限 120 字符，`CONTEXT_SUMMARY_MAX_CHARS`）。
- 正文用固定标签对包裹（`<planner-message>…</planner-message>`）：比裸前缀更抗内容混淆；与 session-reference 的 `<referenced-sessions>` 标签实践同型。标签词汇在 team section 中声明（第 3 层），不与 dsh 自身标签冲突。
- **面向特定成员在内容内指出**（如 `@player`）：@mention 是消息内容的一部分（发送者的表达），不是包装元数据；成员被驱动消费消息的时机由 loop 决定（§4.4 要点 1），@mention 影响接收方对消息的处理，不影响投递。
- **1:1 粒度**：planner 一局复盘产出 N 条 assistant 消息 → N 条独立广播 user 消息；不把多条合并塞进一条。粒度对齐使各成员 history 中的群聊记录与原始消息流同构（可读性、`deriveMessages()` 投影自然）。

**第 2 层：log 层 source provenance**（不进 wire，供 UI/审计/过滤）。`MessageSourceMap` 的 merge 扩展（declaration-merge，先例：`subagent-settled`）：

```typescript
declare module "@deepseek-ai/dsh-llm/types" {
  interface MessageSourceMap {
    "team-broadcast": {
      kind: "team-broadcast";
      role: string;                       // 发送者角色（开放字符串——场景无关，头部决策 ⑩）
      senderSessionId: SessionId;         // 发送者 session（durable 可追溯）
      messageId: MessageId;               // 原消息 id（1:1 对应锚点）
      context?: string;                   // 泛化关联键（saolei 侧填局 id；team 不理解其语义）
    } & ContextFormed;                    // form: 'relay' —— 官方语义即"另一个 agent 发给本 agent 的消息"
  }
}
```

- `ContextForm` 直接复用官方 `relay`（语义精确命中），不发明新 form。
- 注入时机（loop 驱动消费 buffer 时）：`memberAgent.followup(createUserMessage({ content: [{type:"text", text}], source: {kind:"team-broadcast", role, senderSessionId, messageId, context, form:"relay"} }))`——消息此刻落成员 log 的 `user/message` 事件（"its typed `source` is the only channel that tells them apart"，§2.5），`deriveMessages()` 原样投影进该成员的 LLM 请求。`messageId` 字段承担**双重角色**：1:1 追溯锚（原消息 ↔ 广播条目 ↔ 注入消息互相追溯，team 层权威记录据此重建）+ **消费状态锚**（buffer 派生重建时以 receiver log 中该字段集合判定已消费条目，§4.4a）。

**第 3 层：prompt 分层声明**（让模型理解标注语义；persona 与团队 prompt 分开管理，头部决策 ⑤）。

*team section*（team 插件统一渲染、注册进每个成员的 agent scope，§4.5）——声明团队与消息格式：

```text
## 团队
你所在的团队负责协作完成扫雷：目标是尽量高的胜率。
成员：player（执行操作，独占桌面控制）、planner（复盘与策略，不操作）。
你会收到成员的群聊广播消息，格式为 "[成员] 摘要" + <成员-message> 包裹的正文；
@你的内容面向你；棋盘事实以工具结果为准。
```

*persona*（preset 承载，§3.3）——只声明角色人格与职责，不含团队事实：

```text
（player preset 的 persona 示例）
你是扫雷 player：冷静、精确、按策略执行。收到策略广播后按其执行，
发现策略与棋盘事实冲突时以棋盘为准并在操作中记录偏差。
```

- 团队事实（目标/名册/格式约定）单一来源在 team section——改团队配置只改 team 插件注册内容，各成员 prompt 同步更新；persona 独立演进互不牵连（对照 `survey/deepseek-harness-preset.md` §9.3 记录的"同一纪律四种措辞漂移"病灶，此分层是结构解）。
- 这是"群聊成员名册"的 dsh 表达：参与者身份与消息格式的约定属于 team section（模型如何理解），而非 role（wire 如何编码）——与 AutoGen 0.4 selector prompt 的 `{roles}` 声明同型。

**第 4 层：工具调用与结果的广播格式（头部决策 ⑧）**。player 的 `tool/call` + `tool/result` 是其输出的一部分，team 收集后 1:1 广播（一次调用+其结果为一条广播单元——call 与 result 是 dsh log 中两个事件，但语义上是一对）。格式与发言同构：

```text
[player] 工具调用 saolei_click (game #3, step 7)
<player-tool-call>
tool: saolei_click
args: {"x": 3, "y": 4}
result: 已揭示，周边 2 雷；剩余 38 格
</player-tool-call>
```

- 抽象层面 team 广播的是**"tools 的调用和结果"这一 agent 通用语义**（"saolei 游戏过程"是更高层概念，落到 agent 实现层面经由 tools 实现——头部决策 ⑧ 的原意）；team 不理解工具语义，只搬运标注后的调用+结果。
- **result 原样广播**（头部决策 ⑬）：input/output 与原样一致（wire 序列化差异不算），不做摘要/引用化——保证 planner 看到的过程与 player 实际执行的逐字对应（"第一手"的完整语义）。token 代价由 planner 侧上下文管理（compaction）承担，不在广播层裁剪。
- 仍遵守 1:1（一次调用+结果一条广播条目，不攒批聚合）。

**为什么不用 assistant role / wire name**（明确排除项）：多 speaker assistant 需要 wire `name`（Responses API 已移除、DeepSeek adapter 不用、GLM Responses 适配器自研不应引入）；伪造 assistant 消息绕过 inbox 直写 log 违反 UserMessage-only 投递面与请求重建不变量（§4.3）。**049 GLM Responses 适配器的约束输入：角色区分不依赖 wire schema 扩展**。

### 5.4 对单 agent 双角色方案（既有决策）的说明

拓扑已拍板为双 agent（头部决策 ③，"loop 持有多 agent 与模型更契合"）；单 agent 双角色方案（`survey/deepseek-harness-agent-loop-prereq.md` §5.6，2026-08-28 决策）被取代——记录保留该方案的存在与被取代事实，其阶段状态机三手段（persona/工具可见性/model route 阶段切换）不再作为 saolei 设计输入。§5.3 的广播实现设计是双 agent 拓扑的组成部分。

---

## 6. 综合结论与已定架构基线

**拓扑决策（2026-09-08，头部决策 ③）**：saolei 采用 **loop 持有多 agent**（player/planner 双顶层 agent + registry 之上的 team 层）拓扑，取代单 agent 双角色（2026-08-28）。

| 机制项 | 判定 | 说明 |
|---|---|---|
| dsh-agent 支持两个**顶层对等** agent | ✅（服务级）/ ❌（驱动级） | registry 多 agent 一等公民；驱动 per-agent；编排发生在 registry 之上的 team 层（§4.1） |
| planner 拿原始过程 | ✅ | player 的 tool 调用+结果 = player 输出 → team 1:1 广播 → planner buffer → 驱动时注入；不经转述、不经 saolei-loop 特殊注入（头部决策 ⑧，§4.2） |
| preset 分池 | ✅（已定，头部决策 ⑩） | player 池/planner 池两个 root 目录，与角色模型一致（§3.2） |
| 角色差异由 preset 承载、物化零定制 | ✅（已定） | 工具选择编辑期固定于 preset 行；物化只传 presetId + model route（§3.3） |
| 工具与配套 prompt 一致 | ✅（已定，决策 ⑥/⑭） | preset 选择单位 = 插件行，工具与 guidance 同插件同生命周期天然一致；per-agent restriction 不一致（官方已知不对称）——不得使用（§3.6） |
| team 只投递不驱动 | ✅（已定，头部决策 ⑦） | 投递目标是成员 buffer；驱动权唯一归 saolei-loop；唤醒张力被 buffer 消解（§4.4） |
| buffer 持久化 | ✅（已定，头部决策 ⑫→⑮） | **从成员 log 派生重建**（派生缓存，非事实源）：重建即一致、exactly-once 天然成立；team 持久化缩小为注册事实 + 顺序锚点（并发驱动时） |
| 每 agent 的 history/store 一致性 | ✅ 结构保证 | log 纯函数投影 + 官方 invariant 逐字节校验；per-session 独立成立；buffer 消息未消费不进 log（§4.3/§4.4） |
| loop 层权威记录 + 各 agent 视图 | ✅（已定） | team 消息流（team 持有）与游戏事件流（saolei-loop 持有）**解耦**（头部决策 ⑨）；视图=驱动消费的消息投影（§4.4） |
| team 层独立抽象（persona 与团队 prompt 分开） | ✅（已定） | team 插件：注册 API + 消息收集/广播（buffer）+ team section（per-agent scope 注册）；persona 留 preset（§4.5） |
| 广播 1:1 + sender-only 标注 | ✅（已定） | 一条原消息（发言/一次调用+结果）一条广播；`[sender]` 标注 + `@member` 在内容内；`messageId` 锚定（§5.3） |
| tool result 原样广播 | ✅（已定，头部决策 ⑬） | input/output 与原样一致（wire 差异不算）；token 管理由 planner 侧 compaction 承担，不在广播层裁剪（§5.3） |
| 插件 = 工具最小颗粒度 | ✅（已定，头部决策 ⑭） | preset 选择插件而非工具；工具插件按角色划分包边界，行内不拆分（§3.6） |
| LLM 请求区分两角色消息 | ✅ 有具体实现 | user-role + sender 标注/标签 + `team-broadcast` source（`context` 泛化键）+ team section 名册；不依赖 wire name（§5.3） |
| team preset（一个 preset 含两角色） | ❌ | preset 原子单位是单 agent 组合（§3.4） |
| 需要自研 preset 插件 | ❌ | 挂官方 `dsh-agent-presets` 行即可；自研的是 team 插件与 saolei-loop（§3.5） |
| 单 session 双 agent | ❌ | Agent↔Session 1:1 强制 + 请求重建不变量阻止（§4.1） |

**架构基线（自上而下）**：

```
saolei-loop（驱动权唯一归属：游戏阶段机、成员驱动时机与 buffer 消费策略；游戏事件流持有者）
   │  ctx.team.register({goal, members: [player, planner]})          │ 游戏事实
   ▼                                                                  ▼
team 插件（群聊原语：注册、收集成员输出【发言+tool调用/结果】、        saolei 插件
1:1 广播进成员 buffer、team section、广播顺序权威）                   （扫雷游戏实现）
   │ buffer（team 层 per-member 队列；loop 驱动时取出 → followup 注入）
   ▼
player Agent（player 池 preset：persona + saolei-* 工具行）  planner Agent（planner 池 preset：persona + memory 工具行）
   各自 session log（发言+工具调用/结果落 log → 被 team 收集广播）→ deriveMessages() → 各自 LLM 请求
```

- 已被取代/排除的方案（记录）：单 agent 双角色（被取代，头部决策 ③）、planner 作为 subagent（排除，头部决策 ①）、team preset（机制不成立，§3.4）、wire name/多 assistant role（不可依赖/不可表达，§5.3）、广播直投 agent inbox/广播驱动 agent（被 buffer 模型取代，头部决策 ⑦）、per-agent restriction 选工具（一致性坑，§3.6）。
- 该基线对既有判定的联动修订：`survey/deepseek-harness-agent-loop-prereq.md` §5.5"agent-scoped 游戏状态"与 §5.6"单 agent 双角色表达"不再适用（游戏状态归 saolei-loop/saolei 插件侧，agent 不持有——头部决策 ⑨）；该文档头部已加取代说明。

---

## 7. 风险与限制记录

1. **preset 机制与 subagent seam 均为 0.1.1-rc.2 时点快照**：developer preview 破坏性变更承诺（047 D10-5）；`dsh-agent-presets` 设计文档自述多轮踩坑修正。
2. **buffer 派生重建的两个边界**（头部决策 ⑮）：① 顺序恢复精度——串行驱动下按驱动轮次/阶段事实归并足够；并发驱动形态需 team 自持跨 log 顺序锚点。② 派生前提是 team 广播面严格限于成员 log 产出（决策 ⑧ 已保证；若未来把游戏事件经 team 广播，该前提破坏，需重新设计）。重建的读取面依赖 session persistence 可用（live store + persistence 直读，`listChildren` 同模式）。
3. **`recompose` 仅 blank agent**：游戏进行中不能切换 preset；若 preset 池支持运行中切换，需要重建 agent/session 的编排语义（超出 dsh 原生能力）。
4. **preset 的工具固定性是约定级**（决策已接受，头部决策 ⑥）：preset 层无"必须有某行"的约束原语，固定性由 saolei 侧 authoring 流程（模板 preset + copy 后仅编辑 persona）保证；且**不得用 per-agent restriction 替代行级选择**（guidance 残留不一致，§3.6）。
5. **`session-reference` 是快照非订阅**："No live link"——team 的成员消息收集必须自己订阅 `session/event`（这正是 team 插件的设计职责，非风险，记录机制边界）。
6. **无全局消息序号**：两个 log 之间无全局序；跨角色因果排序由 team 层在广播条目/source 中携带序号或时间戳（§4.4 要点 4）。
7. **tool result 原样广播的 token 代价（已接受决策，头部决策 ⑬）**：1:1 原样广播工具调用+结果（棋盘识别快照等大对象）会显著膨胀 planner 请求——决策已接受该代价，膨胀控制由 planner 侧上下文管理（`dsh-compaction` 系列）承担，不在广播层裁剪。
8. **多 preset 池的 roster 展示**：`list()` 平铺无分类，选择面（desktop UI）按 id 前缀/root 归属过滤是调用方逻辑。

---

## 8. 对后续设计的输入与待定项

已确认决策（2026-09-08 五轮，见头部）：双 agent 拓扑（loop 持有多 agent）；planner 顶层 agent（subagent 排除）；角色差异由 preset 承载、物化零定制、分池；team 层独立抽象（只投递不驱动，buffer 模型，**buffer 从成员 log 派生重建**；persona 与团队 prompt 分开管理）；广播 1:1、sender-only 标注、@mention 在内容内；工具调用与结果归属 agent 输出、**原样广播**（input/output 逐字一致，wire 差异不算）；游戏事件流（saolei-loop 持有）与 team 消息流解耦（saolei 插件仅是游戏实现）；team API 场景无关泛化（`context` 键）；**插件 = 工具使用最小颗粒度，preset 选择插件而非工具**。

**核心待定项全部已决策（除 memory 延后调研），本调研可进入 spec/plan 阶段。** 组合清单视角的堆叠基线（host 层行清单、两个 preset 池行清单、依赖方向、物化流程、无先例验证项）见 **§9**。

设计基线输入（§6 架构图）：

- **team 插件**：`register({goal, members})` + 成员输出订阅（`assistant/message` + `tool/call`/`tool/result`）+ 1:1 原样广播进成员 buffer（`team-broadcast` source + `form:'relay'` + `messageId`/`context`）+ team section（per-agent scope 注册）+ **buffer 从成员 log 派生重建（live+persistence 直读；消费状态由 receiver log 的 `messageId` 锚点判定）**；buffer 读取面（`drain(member)` 或等价）供 loop 驱动时消费；投递结构参照 `dsh-subagent` continuation manager（持有 AgentHandle + 投递规则），平级拓扑。
- **saolei-loop**：驱动权唯一归属（驱动时机与 buffer 消费策略）；游戏阶段机与游戏事件流持有者；preset 选择与物化（零定制）。
- **preset**：官方 `dsh-agent-presets` roster，player/planner 两个 root 池；选择单位 = 插件行（player 池含 saolei 工具插件行、planner 池含 memory 工具插件行，按角色划分包边界），persona 是唯一用户编辑面；**不用 per-agent restriction 选工具**（§3.6）。
- 049 GLM Responses 适配器不引入 wire `name` 依赖（§5.3 排除项）。

开发时再决定的细节（非待定项，记录免遗失）：team 插件 API 精确签名与 buffer 逐出/清理策略、广播条目序号/时间戳承载（并发驱动时的顺序锚点）、team 注册事实的持久化形态、team section order 频段与标签词汇表、authoring 流程（模板 preset 生成、persona 编辑面）。

延后调研：**外置 memory**（planner 的 memory 工具族实现形态，team 模型已确定，可单独立项调研——头部决策 ⑩）。

---

## 9. 插件堆叠基线（后续开发参考）

本节把前文全部决策收敛为**组合清单视角**的堆叠基线：哪些行、在哪一层、职责与依赖方向。可直接作为 saolei spec 的组合起点。

### 9.1 分层组合清单

**层 1：host 层（进程级组合，SDK 直组 cordis.yml / B1 嵌入）**——全部 session 共享的基础设施：

| 行 | 来源 | 职责 | 依据 |
|---|---|---|---|
| `dsh-app-boot` + cordis 底座（11 包闭包） | 官方 | B1 底座（`specs/047-dsh-chat-demo/research.md` D6 清单） | 既有基线 |
| `dsh-session` | 官方 | Session 事件日志（唯一事实源）、`deriveMessages()` | §2.6 |
| `dsh-session-persistence` + 后端（jsonl/sqlite） | 官方 | resume 冷恢复；**buffer 派生重建的读取前提**（live+persistence 直读，决策 ⑮） | §2.6、§4.4a |
| `dsh-llm` + **GLM Responses 适配器（自研）** | 官方 + 自研 | 消息构造/BlockAssembler；provider 路由（049 FR-007；不引入 wire `name`，§5.3） | 049 spec |
| `dsh-tools` | 官方 | 工具注册/调度管线（saolei 工具经 `ctx.tools` 注册） | prereq §3.1 |
| `dsh-system-prompt` | 官方 | prompt 装配管线（persona/team section/工具 guidance 三来源） | §4.5 |
| `dsh-scope` | 官方 | agent-scoped 注册边界 | prereq §3.1 |
| `dsh-agent` | 官方 | Agent 接口、Inbox、`ctx.agents` registry | §2.1/§2.2 |
| **`dsh-agent-loop`（保留官方行）** | 官方 | ReactLoopAgent 驱动（每 agent 一个）；提供 AgentFactory | §9.3 推论 |
| `dsh-agent-presets` | 官方 | preset roster；config：`roots: [player 池, planner 池]` + `default` | §3.2、决策 ⑩ |
| `dsh-llm-retry`（可选） | 官方 | 请求重试（挂行即得，无需实现） | prereq §4.4 |
| **team 插件（自研）** | 自研 | `ctx.team`：成员注册、输出收集（发言+tool 调用/结果）、1:1 原样广播进 buffer、buffer 从成员 log 派生重建（决策 ⑮，不独立持久化 buffer 本体）、team section | §4.4/§4.4a/§4.5 |
| **saolei-loop 插件（自研）** | 自研 | 编排：游戏阶段机、驱动权（驱动时机+buffer 消费策略）、游戏事件流持有、agent 物化（create + preset mount） | §4.2/§4.5、决策 ⑨ |
| **saolei 游戏插件（自研）** | 自研 | 扫雷游戏实现（`ctx.saoleiGame` 服务：棋盘/规则/操作执行）；游戏状态持有者 | 决策 ⑨ |
| **桥接插件（自研，grpc）** | 自研 | 对外面（gateway/desktop）；消费 `ctx.team`（群聊记录读面）与 `ctx.saoleiGame`（棋盘推送） | prereq §5.4（插件桥接决策） |

**层 2：preset 层——player 池**（`roots` 第一目录，每目录一个 preset）：

| 行 | 来源 | 职责 |
|---|---|---|
| persona 行 | 数据（用户编辑面，唯一可编辑项） | player 角色人格 |
| **saolei player 工具插件行（自研）** | 自研插件（决策 ⑭ 按角色拆包） | `saolei_*` 操作类工具（click/flag/init…）+ 配套 guidance（同包同生命周期，§3.6）；`inject: ["saoleiGame"]` |

**层 3：preset 层——planner 池**：

| 行 | 来源 | 职责 |
|---|---|---|
| persona 行 | 数据（用户编辑面） | planner 角色人格（复盘/策略职责） |
| **planner memory 工具插件行（自研，延后调研）** | 自研插件 | memory 工具族 + 配套 guidance（头部决策 ⑩：team 模型确定后单独调研，先占位） |

### 9.2 依赖方向（inject 关系）

```
saolei-loop        → ctx.team, ctx.agents, ctx.agentPresets, ctx.saoleiGame
team 插件          → ctx.agents（成员 handle）, ctx.sessions（session/event 订阅）,
                     ctx.systemPrompt（team section，经成员 agent.ctx 注册）
player 工具插件行  → ctx.saoleiGame（游戏实现）
桥接插件           → ctx.team（群聊记录/广播订阅）, ctx.saoleiGame（棋盘推送）
GLM 适配器         → dsh-llm 服务面（注册 provider 路由）
```

- 依赖生命周期由框架负责（required 服务消失 → 依赖插件自动 dispose、回来重载，prereq §5.1）——saolei-loop 与 team 插件的启停联动零手工代码。
- saolei-loop 不替换 `AgentFactory`（见 §9.3），因此与官方 `dsh-agent-loop` 行无 setFactory 冲突。

### 9.3 关键架构推论：saolei-loop 从 agent loop 变为 team loop（驱动沿用传统 agent loop）

**saolei-loop 的定义发生层级改变**：不再是 agent 驱动层的自研替换物（prereq 调研的原始设想），而是 registry 之上的 team loop（编排层）；agent 驱动沿用传统 agent loop——官方 `dsh-agent-loop` 行保留（ReactLoopAgent 每 agent 一个，`ctx.agents.setFactory` 由官方 loop 注册），saolei-loop 以普通编排插件消费 `ctx.agents.create()`。推论依据：**拓扑翻转（单 agent 双角色 → 双 agent + team 层）后，agent 驱动不再有任何 saolei 特化需求**：

| 需求 | 单 agent 双角色（原设想） | 双 agent + team 层（已定） |
|---|---|---|
| player/planner 阶段切换 | 驱动内阶段状态机（须自研 loop） | 编排层阶段机（saolei-loop），驱动无感知 |
| 复盘触发/策略注入 | 驱动内挂点改造 | team buffer + loop 驱动，走官方 `followup()` |
| 工具/模型差异 | 驱动内 per-step 切换 | 各自 preset + `AgentOptions`，静态 |
| 消息 role/历史管理 | 单 log 阶段混写 | 官方机制原样（§4.3 一致性自动成立） |

层级改变的**两个直接收益**：

1. **prereq §4.7 的 8 项"需在自研 loop 驱动中重写的模式"全部消解**——turn/step 状态机、abort 检查点、中断流落日志、wake latch、四决策点 waterfall/serial、driver containment、工厂所有权、resume 冷恢复，复用官方驱动即自动继承。
2. **prereq §4.6 的自研 invariant 伴生不再需要**——saolei 不改动"请求从日志派生"的任何环节（`team-broadcast` 是 `MessageSourceMap` 的 merge 扩展，合法且不触发 request-reconstruction 校验）；官方 invariant 直接生效。

prereq §3.1 的依赖最小集分析语义更新为：那 7 项必需 peers 是**官方 agent-loop 行的依赖**（挂行即解析），不再是自研 loop 的复制清单。

### 9.4 物化与运行流程（堆叠串联，一局游戏）

1. **物化**（saolei-loop）：`ctx.agents.create({ sessionId, agentOptions: {provider: "glm-responses", model: …}, meta: { agentPreset: "<player|planner 池 preset id>" }, setup: (agentCtx) => ctx.agentPresets.mount(agentCtx, presetId) })` × 2——两个角色调用**同构**（决策 ⑥：物化零定制，差异全在 preset）；`mount` 的唯一支持调用点即此 setup（§2.3）；`dsh-agent-loop` 的 `Config.agents[]` 留空（动态创建，不用 boot-time 声明）。
2. **组队**（saolei-loop）：`ctx.team.register({ goal, members: [{agent: player, role: "player"}, {agent: planner, role: "planner"}] })`——team section 注册进两成员 agent scope；成员输出订阅生效。
3. **对局**：loop 驱动 player（drain buffer → `followup` 注入）→ player 调 `saolei_*` 工具（工具插件 → `ctx.saoleiGame`）→ `tool/call`/`tool/result` 落 player log → team 收集广播进 planner buffer；桥接插件同步推 desktop。
4. **复盘**：局结束（saolei-loop 阶段机依其游戏事件流判定）→ loop 驱动 planner（drain buffer → 注入本局全部工具调用+结果条目，原样决策 ⑬）→ planner 产出落 log → team 广播进 player buffer。
5. **下一局**：loop 驱动 player（消费策略消息）→ 循环。
6. **收尾**：dispose 时 agent 卸载 → team 注册事实与 buffer 清理（agent-scoped 注册自动 unwind）；桥接/游戏插件为 host 行随进程生命周期。

### 9.5 无官方先例项（开发时验证，与既有风险记录对应）

1. **SDK 直组 + preset roster 组合无先例**：roster 的官方消费者是 web host；B1/SDK 直组形态（047 demo）不挂 roster。挂 `dsh-agent-presets` 行 + 动态 create + setup mount 的组合属探索项（同 B1 调研 §5.4"直组核心件"的探索属性），需在 spec/plan 阶段以最小 PoC 验证（两池各一 preset、双 agent 物化、mount 幂等）。
2. **双 agent 并发 + team 广播的压力面**：1:1 原样广播（决策 ⑬）下 planner buffer 单局条目数 = player 工具调用次数；drain 一次注入的消息量与 `agent/pre-step` enter 决策的批量语义需实测（官方语义支持一批 messages，§4.4 要点 3）。
3. **buffer 派生重建的验证**：重建算法（sender 投影 + receiver `messageId` 消费锚点 + 串行归并）与崩溃场景（广播中途崩溃自愈、已消费不重复）需 PoC 覆盖；team 注册事实的持久化形态（或 saolei-loop 重建时重新 register 兜底）开发时定（§4.4a、§7 风险 2）。

---

## 10. 引用来源汇总

仓库内（本地物化源码，0.1.1-rc.2 线）：

- `node_modules/.pnpm/@deepseek-ai+dsh-agent-presets@0.1.1-rc.2_5200ead8959daeaefdf3dd69ba905368/node_modules/@deepseek-ai/dsh-agent-presets/`（README 全文：roster 服务、standing mount/join、recompose、authoring、mount rejects）
- `node_modules/.pnpm/@deepseek-ai+dsh-subagent@0.1.1-rc.2_41d5ff529e06289e51a68d84fe32d51b/node_modules/@deepseek-ai/dsh-subagent/`（README 全文：Service API、capabilities、applyChildComposition、continuable/Activation、settlement delivery、Known Limitations）
- `node_modules/.pnpm/@deepseek-ai+dsh-session-reference@0.1.1-rc.2_535ff2087b76500c877cfd701efd5f97/node_modules/@deepseek-ai/dsh-session-reference/README.md`（跨 session 快照引用、snapshot semantics、No live link）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/runtime-types.d.ts`（Agent 接口、send/followup/steer/inject 入参 UserMessage、agent/* 事件）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/lib/types/index.d.ts`（CreateAgentOptions：sessionId/meta.agentPreset/parentSession/seed/origin/delegationDepth/setup；AgentHandle）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-agent/README.md`（registry 生命周期、enter 强制 agent.id === session.id、agent.ctx shadow）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/message.d.ts`（Message.role 三值、MessageSourceMap、ContextForm（relay/recall）、createAssistantMessage、CONTEXT_SUMMARY_MAX_CHARS）
- `experimental/dsh/demo/agent/node_modules/@deepseek-ai/dsh-llm/lib/types/types.d.ts`（ContentBlock/TextBlock 结构）
- `node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_ecf90c249e2d9dbb5492af64a9f4bd5d/node_modules/@deepseek-ai/dsh-agent-loop/lib/types/index.d.ts`（Config.agents[] 多 agent 声明）
- `node_modules/.pnpm/@deepseek-ai+dsh-session@0.1.1-rc.2_a4e4bb24a1f3580ac25e11cfa3c6b8cc/node_modules/@deepseek-ai/dsh-session/README.md`（log/surface/deriveMessages、source 是唯一区分通道、compaction replacement）
- `node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_a4f32a8d2888fcc325311de9471efdc6/node_modules/@deepseek-ai/dsh-llm-deepseek/lib/index.js`（wire 序列化：assistant 无 name）
- 前置调研：`survey/deepseek-harness-agent-loop-prereq.md`、`survey/deepseek-harness-preset.md`、`survey/agent-team-mode.md`、`survey/deepseek-harness-framework.md`
- 实证：`specs/047-dsh-chat-demo/research.md`（D5 registry 多 session）

仓库外（业界实践与 API 参考）：

- https://github.com/microsoft/autogen/blob/v0.2.16/autogen/agentchat/groupchat.py（AutoGen 0.2：GroupChat.append 设置 message.name = speaker.name）
- https://microsoft.github.io/autogen/dev/user-guide/core-user-guide/design-patterns/group-chat.html（AutoGen 0.4：source 字段 + 他人消息以 UserMessage 入自己 history + "{source}: {content}" 格式化）
- https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/selector-group-chat.html（selector history 格式 "{source}: {content}"）
- https://www.anthropic.com/engineering/multi-agent-research-system（orchestrator-worker、subagent 独立上下文、结果压缩回传）
- https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents（sub-agent 架构的上下文隔离与摘要回传）
- https://community.openai.com/t/clarification-on-missing-name-field-in-responses-api-and-handling-multi-persona-multi-user-dialogues/1365804（Responses API 移除 name 字段、无回归承诺、前缀标注建议）
- https://developers.openai.com/api/reference/resources/chat（Chat Completions message name 字段：可选 participant 标识）
- https://community.openai.com/t/role-management-in-the-chat-completions-api/929112（同 role 多参与者用 name 的社区实践）
