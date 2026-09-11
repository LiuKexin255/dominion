# Feature Specification: Agent v2 team 排队消息 step 边界进入与 turn 语义表述修正

**Feature Branch**: `061-team-queue-steer`

**Created**: 2026-09-11

**Status**: Draft

**Input**: User description: "1. 修正 agent v2 team 对于排队消息的处理。作为单 agent 的扩展，team 的排队消息应该在当前激活的 agent 下一个 step 开始时随工具结果一起作为 message 进入（或者 turn 结束后直接进入，这里注意 turn 结束的情况与 member 切换的节点仍然不变，切换仍然在排队消息完成后切换）。对于 team 视图来说，排队消息应该排在输入时的那个 step 之后，具体是下一个 step 进入还是 turn 进入，取决于 step 结果。2. 修正 spec 与注释中关于 turn 冗余的描述，turn 本身包含 agent 所有工作完成，不再有新的 llm 调用，多余的形容词容易形成干扰"

## Motivation

排队消息的"mid-turn 进入"语义在 v1 桌面版已建立并被实现（`specs/038-queue-input-mid-turn/spec.md` FR-001：排队消息在 turn 内下一个"推理步"随工具结果一起投递），但 agent v2 迁移时被降级为"回合结束后进入"（`specs/049-agent-v2-dsh-init/research.md` D 系列决策将 mid-turn 注入错误归入"不迁移的 observe-only 等扩展"），team 模式延续了该降级语义（059 FR-011：排队消息在成员回合结束后消化）。术语层面，dsh 的 turn/step 语义已经源码级核实（`survey/deepseek-harness-turn-step-semantics.md`：**turn = 控制焦点的一次完整移交——turn 结束即成员本轮全部工作完成、不再有新的模型调用；step = 一次模型请求 + 其工具执行——工具欠账延续同 turn 的下一个 step，永远不会开新 turn**）。基于该基准复核发现：059 家族文档与编排器注释中的切换锚点描述使用了不存在的"工具调用引发的后续 turn"触发源，是 turn≈step 旧心智模型的残留表述。

目标与现状的差距（本 feature 要完成的工作）：

| 维度 | 现状 | 目标 |
|---|---|---|
| 排队消息进入时机（turn 在途） | 等当前成员 turn **全部 step 结束**（编排层 FIFO，idle 后才消费）——多步工具回合期间用户输入完全无法进入 | 当前激活成员的**下一个 step 开始时随工具结果一起**作为用户消息进入成员上下文 |
| 排队消息进入时机（turn 收尾） | turn 结束后由当前激活成员以新回合消化 | 保持（不变）——进入路径不预判，由当前 step 的结果自然决定（有工具调用则存在下一 step→step 进入；纯文本收尾→turn 结束→回退路径） |
| member 切换节点 | 排队消息消化完成后切换（消化优先于切换） | 保持（不变）——mid-turn 进入的消息视为已消化，不再占用消化路径 |
| turn 语义表述 | "turn 结束**并且不会再触发新的 turn**（两种新 turn 触发源都不存在：**工具调用引发的后续 turn**、待消化排队消息）"——枚举了不存在的触发源，冗余且误导（5 处 spec + 编排器注释） | turn 按本义使用（结束即全部工作完成、不再有新的 LLM 调用）；切换条件收敛为"turn 已结束且无待消化排队消息" |
| 049 排队基线引用 | 声称"对齐 030、038 基线"但实现仅迁移 030 语义；mid-turn 注入被错误归类为"observe-only 等扩展" | 增补勘误/supersession 注记：mid-turn 注入是 038 的核心行为（FR-001/FR-002），其语义经本 feature 在 team 形态恢复 |

## Clarifications

> 本节记录已裁定/已验证事项的结论来源；终态规范编码于 FR 与 Assumptions。

### 语义基准（已核实，2026-09-11）

- dsh turn/step 语义经源码级核实（`survey/deepseek-harness-turn-step-semantics.md`）：turn 打开于 input claim 之前、关闭于"nothing is owed"；**工具欠账延续当前 turn 的下一个 step，不开新 turn**；turn 边界的 claim 原子性 = 全部 next-step 输入 + 恰好一条排队 next-turn 消息（step 间只 claim next-step 输入——这正是 mid-turn 进入的机制基础）。
- v1 排队语义谱系：030（turn 结束后 hand-off，多条合并为聚合输入）→ 038（**supersede 030 FR-013**：mid-turn 注入——下一个推理步随工具结果投递；turn 无工具调用时回退 turn 结束路径）。v1 实现已随 059 phase 5 移除，其语义以 spec 038 为准。
- 现行 team 实现的排队路径：`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 的编排层 FIFO（`submit`/`nextStep`）——仅在 drive（= 一个成员 turn）边界消费；`projects/game/agent_v2/src/session.ts` 的 Send 在成员回合在途时返回 `queued{position}` 帧并 enqueue 即固化入归并序列。
- **dsh inbox 机制确认**（本地物化源码 0.1.1-rc.2：`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/` README §inbox + `lib/index.js`）：排队机制底座为**双待处理列表**（durable 投影，重启回放）——`nextTurn`（每条独占一个 turn 的 FIFO）与 `nextStep`（等待下一个 step 边界的输入）；**三入口**由调用方选择：`followup`（next-turn + 唤醒，≈ opencode queue 模式）、`steer`（next-step + 唤醒；**idle 时同步开新 turn**，running 时下一 step 边界消费，≈ opencode steer 模式）、`inject`（next-step 不唤醒）；**claim 批语义**：step 边界取走全部 next-step 输入，turn 边界取走全部 next-step + 恰好一条 next-turn（loop 内建，无需编排层晋升逻辑）；**cancel 默认 durable 清空全部 pending**（`keepInbox` 可保留），无 step-only abort。inbox 另提供可变更面（append/prepend/replace/remove/clear，replace/remove 为 durable 取消）。入口选择与编排续跑策略（消化优先于切换）是调用方职责，dsh 不提供用户级排队指示/位置序号。
- **opencode 对齐确认**（源码级，2026-09-11）：本 feature 语义与 opencode 输入体系逐点对齐（`SessionInput`，delivery = `steer` | `queue`；源码 https://github.com/anomalyco/opencode/blob/dev/packages/core/src/session/input.ts 与 https://github.com/anomalyco/opencode/blob/dev/packages/core/src/session/runner/llm.ts ）——① steer 输入在**下一次 provider turn 开头**晋升（run 循环每次 provider turn 结束后 `promotion = "steer"`），作为**普通用户消息**进入投影历史，**位于上一条 assistant message（≈ dsh step）的工具结果之后**、下一次模型调用之前，不并入工具结果；② 晋升输入与最初 prompt 走同一 `Prompted` 事件投影为 user message——无特殊语义（即本 feature"消费后无特殊语义"裁定）；③ 模型已收尾（无后续 provider turn）时 pending steer 仍强制续跑新 provider turn（step 重置 1）——等价于 FR-002 的 turn 结束后新回合消化；④ queue 输入仅在 run 收敛后逐条晋升、每条获得完整 run——对应 FR-002 的逐条新回合。以上仅作**语义参照**；实现以 dsh 原生能力为准（Assumptions"实现优先 dsh 原生能力"裁定），不迁移 opencode 代码。

### 用户裁定（本 feature Input，2026-09-11）

- 排队消息应在**当前激活成员的下一个 step 开始时随工具结果一起作为 message 进入**；或 turn 结束后直接进入——**不预判，取决于 step 结果**（当前 step 有工具调用则存在下一 step；纯文本收尾则 turn 结束）。
- **"随工具结果一起进入"的消息形态**（澄清，2026-09-11）：下一个 step 的模型输入中，**工具结果之后跟随一个独立的用户消息块**（普通 message）——不是把排队消息并入工具结果内容。
- **消费后无特殊语义**（裁定，2026-09-11）：排队消息被消费（作为普通用户消息进入成员上下文）后即为普通用户消息，**不再视作排队消息**——消除特殊语义、保持一致；排队相关的特殊行为（排队指示、取消保留、消化路径）仅存在于消费前。
- **member 切换节点不变**：切换仍在排队消息（全部）完成后进行；turn 结束情况的消化路径与切换优先级（消化优先于切换）保持现状。
- **team 视图顺序非约束**：排队消息进入是下一个 step 还是 turn **不影响消息在视图上的顺序**——无论哪个分支，排队消息都排在输入时当前 step 内容之后。这不是本 feature 的目标或约束，而是正确实现自然导出的结果（归并序列 enqueue 即固化 + 成员视角按消费时刻投影，两个既有机制自然产生该顺序）；Input 中"排在输入时的那个 step 之后"是对该自然结果的描述。
- **turn 表述修正原则**：turn 本身已蕴含"agent 所有工作完成，不再有新的 llm 调用"；"不会再触发新的 turn（工具调用引发的后续 turn…）"式的多余形容应删除。

### Session 2026-09-11

- Q: 表述修正（US4/FR-006/FR-007）的交付范围是否要补齐两处 059 遗留——为 059 FR-011 的"回合结束后消化"子句加 supersession 注记，以及处理 059 的"team turn 持续流"命名？ → A（用户裁定，选项 A）：**两处均纳入**——059 FR-011 加 supersession 注记（与 049 注记同构）；"team turn 持续流"改名或加注，消除与 dsh turn 的词面冲突（该词横跨多个成员 turn，非 dsh turn）。
- Q: Cancel 时未消费的排队消息，下次 Send 时是否应作为正常历史用户消息进入 LLM 上下文？ → A（用户裁定）：**是**——恢复 054 FR-017 第三个子句的 team 形态：Cancel 将全部未消费消息（编排 FIFO + steered-unclaimed）落地（进入 team 归并序列的固化历史，现状已有）且不驱动 agent；**下次 Send 触发的成员回合 MUST 使这些落地消息作为普通用户消息进入 LLM 上下文（不单独触发处理）**。team 会话历史为进程内存态（059 既有设计，无持久 store），不受本裁定影响。
- Q: landed 的投递实现是否为"Cancel 后的 Send"定制在途分支（Cancel 后正常时序下次 Send 必走静止路径，在途+landed 组合近乎不可达）？ → A（用户裁定）：**不定制——landed 作为解耦层**：Cancel 侧只写入 landed 队列；Send 侧只有统一 flush 规则（任何驱动装配与任何 steer 之前先 flush landed，来源无关）；US1/US2（FR-001/FR-002）对 Cancel 后的情况零定制。保留在途路径的 flush 并非死代码——Send 与 agent 状态是弱耦合快照，Cancel 收敛窗口（abort 传播、drivingMember 未清空）内 Send 仍会判定在途，统一规则以不变应竞态；专门感知 Cancel 后状态反而加大复杂度。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 排队消息在下一个 step 随工具结果进入 (Priority: P1) 🎯 MVP

当前激活成员（如 player）的回合正在执行多步工具循环（step 1 模型请求→工具执行→step 2 模型请求→…）。用户在工具执行期间发送消息（补充指令、纠正、追加上下文）：消息先入队并呈现排队指示；当当前 step 完成、下一个 step 开始时，该消息作为**独立的普通用户消息块跟随工具结果之后**进入成员上下文（正常 message，不是工具结果的组成部分）——成员在下一个 step 就能看到并回应用户输入，而不是等整个回合（全部 step）结束。若消息到达时成员回合尚未发出第一次模型请求（回合刚开），该消息在第一个 step 进入。

**Why this priority**: 本 feature 的核心行为修正；恢复 v1（038 FR-001）建立、迁移中丢失的 mid-turn 进入语义。多步工具回合是扫雷 team 的常态（player 每次操作都是一步工具调用），等整个回合结束意味着用户输入在最需要及时介入的窗口内永远无法生效。

**Independent Test**: 大型测试（fake-llm + fake-desktop）构造 player 多步工具回合，在工具执行期间排队用户消息，断言：该消息作为独立用户消息块出现在成员下一个 step 的模型输入历史中（位于工具结果之后），且该 step 的模型输出对该消息内容有响应；成员视角视图在消息被消费时实时呈现该输入。

**Acceptance Scenarios**:

1. **Given** player 回合在途且刚完成一次工具调用（存在后续 step），**When** 用户发送消息，**Then** 该消息作为独立用户消息块出现在 player 下一个 step 的模型输入中、位于该工具结果之后，后续输出体现对它的响应。
2. **Given** 成员回合已开启但首次模型请求尚未发生，**When** 用户发送消息，**Then** 该消息在第一个 step 进入成员上下文。
3. **Given** 成员回合在途且用户先后发送多条消息，**When** 下一个 step 开始，**Then** 到达时尚未消费的排队消息一并进入该 step 的输入（同边界批量，提交顺序保持）。
4. **Given** 消息在 step N 执行中被排队并在 step N+1 进入，**When** 查看成员视角，**Then** 该输入呈现于 step N 与 step N+1 的输出之间（消费时点的直接证据——视图顺序由既有投影机制自然导出，非本 feature 约束）。

---

### User Story 2 - turn 结束回退路径与切换节点保持不变 (Priority: P1)

若消息到达后当前 turn 不再有下一个 step（模型以纯文本收尾、回合即将结束），消息不强行注入，而是在 turn 结束后按既有消化路径进入：由当前激活成员以新回合处理（消化驱动输入 = 未消费团队消息 + 排队消息）。member 切换节点不变：切换评估仍在**全部**排队消息消化完成后进行（消化优先于切换；player→planner 的"排队先消化 → gameEnded 评估 → 结构性续驱"优先级序保持）。回合内被消费的排队消息自此为普通用户消息（不再是排队消息），不占用消化路径。

**Why this priority**: 与 US1 共同构成完整的进入语义（不预判路径，由 step 结果自然决定）；同时锚定不变量，防止 mid-turn 引入改变编排切换行为——切换语义的稳定性是 team 工作流（planner⇄player 交替）正确性的基础。

**Independent Test**: 大型测试构造两类场景断言：(a) 消息恰在回合最后一个 step 执行中到达（此后纯文本收尾）→ turn 结束后由当前激活成员消化为新回合、消化完成后才发生切换；(b) 回合中途 mid-turn 消化了消息 → turn 结束后无额外消化回合、直接按既有优先级切换/续驱。

**Acceptance Scenarios**:

1. **Given** 成员回合最后一个 step 执行中（此后无工具调用、turn 即将结束），**When** 用户发送消息，**Then** 该消息不进入当前回合，turn 结束后由当前激活成员以新回合消化（现状路径，行为不回归）。
2. **Given** planner 回合结束时有未消化排队消息，**When** 切换评估执行，**Then** 排队消息先由 planner 消化（消化优先于切换），消化完成且 planner 无待消化输入后才续驱 player（现状语义保持）。
3. **Given** 排队消息已在当前回合内被消费（mid-turn 进入），**When** turn 结束，**Then** 该消息已是普通用户消息，不触发额外消化回合，切换评估按既有优先级进行。
4. **Given** player 回合结束，**When** 切换评估执行，**Then** 优先级序保持：排队消息先消化 → gameEnded 评估（是则驱动 planner 复盘 / 否则结构性续驱 player）。

---

### User Story 3 - 排队指示与取消语义的一致性 (Priority: P2)

排队指示（`queued{position}` 帧与前端 pending 呈现）在消息**被消费时**消除（mid-turn 进入即在对应 step 边界消除，不等 turn 结束）。取消（Cancel）语义与 059 FR-017 保持一致并覆盖 mid-turn 情形：被消费的消息即普通用户消息（随在途回合终止呈现，不回滚，无排队特殊语义）；尚未消费的排队消息保留为已固化历史（Send 接受时已入归并序列）且不触发新驱动，并在**下次 Send** 触发的成员回合中作为普通用户消息进入 LLM 上下文（不单独触发处理——Session 2026-09-11 裁定）。

**Why this priority**: 排队指示的真实性（消费即消除，对齐 038 FR-008）与取消语义的确定性是用户信任排队功能的呈现面；mid-turn 进入引入了新的消费时点，指示与取消必须覆盖该时点。

**Independent Test**: 组件/模块测试断言排队指示随消费消除（step 边界）；大型测试断言 Cancel 后：已被消费（mid-turn 进入）的消息作为普通用户消息呈现在终止回合的上下文中、尚未消费的排队消息以历史用户消息形态存在于归并序列且不触发新驱动；再次 Send 后落地消息与新消息一同出现在该回合的 LLM 请求历史中（fake-llm 断言）。

**Acceptance Scenarios**:

1. **Given** 用户排队一条消息且其已被下一个 step 消费，**When** 查看对话页，**Then** 排队指示已消除、消息呈现于成员视角（消费时点），后续 step 输出流式可见。
2. **Given** 成员回合在途、存在已被消费（mid-turn 进入）与仍在排队的消息，**When** 用户执行取消，**Then** 在途回合终止（既有终态呈现）、已消费的消息作为普通用户消息保留在成员上下文与归并序列、仍在排队的消息保留为已固化历史且不触发新驱动、team 静止后再次 Send 恢复（现状语义）。
3. **Given** 取消后有落地消息（未消费而被保留为已固化历史），**When** 用户再次 Send，**Then** 新消息触发的成员回合中，落地消息与新消息一同作为普通用户消息进入 LLM 上下文（落地消息不单独触发回合；fake-llm 可断言其出现在该回合请求历史中）。
4. **Given** team 静止（无在途回合），**When** 用户发送消息，**Then** 直接由当前激活成员处理（现状路径，无排队、无回归）。

---

### User Story 4 - turn 冗余表述修正（spec 与注释） (Priority: P2)

修正 059 家族文档与编排器注释中切换锚点的冗余表述：删除"turn 结束并且不会再触发新的 turn（两种新 turn 触发源都不存在：工具调用引发的后续 turn、待消化排队消息）"式的触发源枚举——其中"工具调用引发的后续 turn"在 dsh 语义下不存在（工具欠账延续同 turn 的下一个 step），"turn 结束"本身已蕴含"全部工作完成、不再有新的 LLM 调用"。切换条件收敛为"turn 已结束且无待消化排队消息"。同时为 049 的排队基线引用增补勘误注记（mid-turn 注入是 038 核心行为而非"observe-only 等扩展"，其语义经本 feature 在 team 形态恢复）。范围另含两处 059 遗留（Session 2026-09-11 裁定）：为 059 FR-011 的"回合结束后消化"子句加 supersession 注记（与 049 注记同构）；消除"team turn 持续流"命名的 turn 词面冲突（该词横跨多个成员 turn，非 dsh turn——改名或加注）。

**Why this priority**: 用户要求 2 的直接交付物；本对话中已实证该冗余表述源于 turn≈step 旧心智模型，会持续误导后续读者（枚举不存在的触发源、诱导错误的 turn/step 映射）。表述修正独立于行为修正，可单独交付与验证。

**Independent Test**: 文本检索断言：059 家族文档（`spec.md`、`data-model.md`、`research.md`、`contracts/dsh-plugins.md`、`contracts/team-api.md`）与 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 注释中"工具调用引发的后续 turn"零残留；切换锚点表述为收敛式；`specs/049-agent-v2-dsh-init/` 与 `specs/059-agent-v2-team-mode/`（FR-011）含勘误/supersession 注记；"team turn"词面冲突消除（059 家族 `rg 'team turn'` 零命中，或每条命中同行含"非 dsh turn"澄清语）。

**Acceptance Scenarios**:

1. **Given** 059 家族文档与编排器注释，**When** 修正完成，**Then** 切换锚点表述不再枚举"工具调用引发的后续 turn"；turn 一词按本义使用（结束即不再有新的模型调用）。
2. **Given** `specs/049-agent-v2-dsh-init/`（FR-012 与 research 排队决策）与 `specs/059-agent-v2-team-mode/`（FR-011），**When** 注记完成，**Then** 明确 mid-turn 注入为 038 核心行为、059 FR-011 的"回合结束后消化"子句由本 feature supersede（mid-turn 进入 + turn 结束回退），team 形态排队语义以本 feature 为准（038 对 030 加 supersession note 的既有实践）。
3. **Given** 修正后的全部文档，**When** 审阅 turn/step 相关表述，**Then** 与 `survey/deepseek-harness-turn-step-semantics.md` 的语义基准一致（无新增的 turn/step 混用）。
4. **Given** 059 的流生命周期表述（"team turn 持续流"，出现于 `spec.md`、`research.md`、`contracts/team-api.md`），**When** 修正完成，**Then** 该命名在全部出现位置不再与 dsh turn 词面冲突（改名〔如"team 持续流（至 team 静止）"〕或加注澄清二择一：流横跨多个成员 turn、至 team 静止）。

---

### Edge Cases

- **消息恰在 step 边界瞬间到达**（claim 已发生 vs 未发生）：由成员输入消费机制自然裁决——本 step 未 claim 则下一 step 进入，不丢失（enqueue 即固化保证归并序列可见性）、不重复消费。
- **消息在最后一个 step 执行中到达**：此后无下一个 step → 走 turn 结束回退路径（US2 场景 1）。
- **Cancel 与 mid-turn 输入的竞态**：已被消费的消息即普通用户消息，随在途回合终止呈现（成员上下文不回滚）；已投递成员输入队列但尚未被消费的消息随取消清出（不触发新驱动），其已固化历史保留（Send 接受时入归并序列）并转为落地消息（下次 Send 进入 LLM 上下文，FR-004）。
- **多次取消的落地累积**：落地消息在编排层累积（取消可多次发生）；后续活跃取消清空成员 inbox 时，仍未消费的落地消息保持落地状态（下次 Send 重新投递）；落地消息 MUST NOT 单独触发驱动。
- **多条消息同边界到达**：一并进入该 step 输入（提交顺序保持）；具体消息形态（独立多条 / 聚合）由 plan 决定，语义等价（模型可见内容一致）。
- **team 静止时发送**：现状路径（直接驱动当前激活成员），无排队、无行为变化。
- **刷新（UpdateTeam）与排队并发**：既有作废语义不变（在途回合终止、排队作废、清空重建）。
- **relay（团队广播）不受影响**：广播消费仍在编排驱动边界（"team 只投递不驱动"不变）；mid-turn 进入仅适用于用户排队消息。

## Requirements *(mandatory)*

### Functional Requirements

**排队消息进入语义（team）**

- **FR-001**: 当前激活成员的 turn 在途时，用户发送的消息 MUST 在该成员**下一个 step 开始时**进入成员上下文：在下一个 step 的模型输入中，消息作为**独立的普通用户消息块跟随工具结果之后**（顺序：工具结果 → 用户消息；MUST NOT 并入工具结果内容）；消息在首次模型请求发生前的到达 MUST 在第一个 step 进入。进入路径 MUST NOT 预判——由当前 step 的结果自然决定（存在后续 step 即 step 进入）。
- **FR-002**: 消息到达后当前 turn 不再产生后续 step 时（纯文本收尾 / turn 结束），消息 MUST 在 turn 结束后由当前激活成员以新回合消化（既有消化路径：enqueue 即固化、消化驱动输入含未消费团队消息与该消息）。
- **FR-003**: member 切换节点 MUST 保持现状：切换评估仍在全部排队消息消化完成后进行（消化优先于切换；player→planner 的"排队先消化 → gameEnded 评估 → 结构性续驱"优先级序不变）；回合内被消费的排队消息自此为普通用户消息（不再是排队消息），MUST NOT 触发额外消化回合。
- **FR-004**: 排队指示 MUST 在消息被消费时消除（mid-turn 路径即在对应 step 边界消除，不等 turn 结束）；排队消息的特殊语义 MUST 仅存在于消费前——被消费（作为普通用户消息进入成员上下文）后即为普通用户消息，不再视作排队消息（取消时随在途回合终止呈现、不回滚，后续作为普通上下文参与）。Cancel 时尚未消费的排队消息（编排 FIFO 与已 steer 未 claim 的全部）MUST 保留为已固化历史（059 FR-017 语义保持）且 MUST NOT 触发新驱动；**下次 Send 触发的成员回合 MUST 使这些落地消息作为普通用户消息进入 LLM 上下文（不单独触发处理——054 FR-017 第三个子句的 team 形态恢复，Session 2026-09-11 裁定）**。

**作用边界**

- **FR-005**: 团队广播消息（relay）的消费时机 MUST NOT 改变（仍在编排驱动边界消费，"team 只投递不驱动"保持）；本 feature 的 mid-turn 进入 MUST 仅适用于用户排队消息。team 静止时发送消息的现有直接驱动路径 MUST 无回归；既有视图呈现语义（成员视角消费时实时呈现、List 回填与实时一致、归并序列 enqueue 即固化）MUST 无回归——视图排序为正确实现的自然结果（Clarifications 2026-09-11），本 feature 不为排序增设约束。

**turn 语义表述修正**

- **FR-006**: `specs/059-agent-v2-team-mode/` 家族文档（`spec.md` 切换节点 Clarification、`data-model.md` §5 切换锚点、`research.md` 续驱规则与决策⑦、`contracts/dsh-plugins.md` 结构性续驱条目）与 `common/js/dsh-plugins/saolei-loop/src/orchestrator.ts` 文件头注释中，"turn 结束并且不会再触发新的 turn（两种新 turn 触发源：工具调用引发的后续 turn、待消化排队消息）"式表述 MUST 修正为：turn 按本义使用（turn 结束即成员本轮全部工作完成、不再有新的模型调用；工具欠账延续同 turn 的下一个 step，不开新 turn），切换条件收敛表述为"turn 已结束且无待消化排队消息"。另 MUST 消除该家族"team turn 持续流"命名与 dsh turn 的词面冲突（`spec.md` 流生命周期 Clarification、`research.md` 流生命周期表述与 Alternatives 行、`contracts/team-api.md` §3.1——该词横跨多个成员 turn、非 dsh turn；改名〔如"team 持续流（至 team 静止）"〕或加注澄清二择一，Session 2026-09-11 裁定；验证判据见 SC-004）。
- **FR-007**: 排队语义 supersession 注记 MUST 落于两处既有 spec（均不重写原文，历史记录保持；Session 2026-09-11 裁定补齐第二处）：① `specs/049-agent-v2-dsh-init/`（FR-012 及 research 的排队最小行为决策）——038 的 mid-turn 注入（FR-001/FR-002，supersede 030 FR-013）是核心行为而非"observe-only 等扩展"，其单 agent 迁移丢失的语义经本 feature 在 team 形态恢复；② `specs/059-agent-v2-team-mode/` FR-011——"回合结束后由当前激活成员消化"子句由本 feature FR-001/FR-002 supersede（mid-turn 进入 + turn 结束回退），其余子句（排队呈现、广播、切换延后）保持（对齐 038 为 030 加 supersession note 的既有实践）。

### Key Entities

- **排队消息（用户输入）**：Send 接受即固化入 team 归并序列的待消费用户消息（既有语义）；进入路径二择（下一个 step 跟随工具结果之后的独立用户消息块 / turn 结束后新回合），由 step 结果自然决定，不预判。**生命周期终止于消费**（终态规范见 FR-004）：被消费后即为普通用户消息，不再具有排队特殊行为。
- **step 边界（mid-turn 进入点）**：turn 内一次模型请求的起点；mid-turn 进入发生于此——该 step 的模型输入中，工具结果之后跟随独立的用户消息块（普通 message，非工具结果的组成部分；机制上即 dsh 的 next-step 输入 claim）。
- **切换锚点**：planner/player 切换评估的条件——turn 已结束且无待消化输入；本 feature 修正其表述（删除不存在的"工具调用引发的后续 turn"触发源）并保持其语义。
- **turn（术语基准）**：控制焦点的一次完整移交；turn 结束即成员本轮全部工作完成、不再有新的模型调用（依据 `survey/deepseek-harness-turn-step-semantics.md`；工具欠账延续同 turn 的下一个 step）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试断言（fake-llm + fake-desktop）：player 多步工具回合中排队的用户消息出现在成员**下一个 step 的模型输入**中（独立用户消息块，位于工具结果之后），该 step 输出对消息内容有响应；首个 step 前到达的消息在第一个 step 进入。
- **SC-002**: 大型测试断言：回合尾段（无后续 step）到达的消息经 turn 结束后由当前激活成员消化为新回合，切换在全部排队消息消化完成后发生（消化优先于切换回归通过）；回合内已消费的消息（普通用户消息）不触发额外消化回合；Cancel 后再次 Send，落地消息与新消息一同出现在该回合的 LLM 请求历史中且落地消息不单独触发回合。
- **SC-003**: 视图回归断言：mid-turn 进入的输入在成员视角于消费时点实时呈现（呈现于输入时 step 与下一 step 输出之间——进入时点的直接证据）；List 回填与实时呈现收敛一致；排队指示随消费消除；归并序列排序行为与现状一致（enqueue 即固化，正确实现的自然结果，非独立约束）。
- **SC-004**: 文本检索断言：059 家族文档与编排器注释中"工具调用引发的后续 turn"零残留，切换锚点为收敛式表述（"turn 已结束且无待消化排队消息"）；049 与 059 FR-011 含 supersession 注记且原文未被重写；"team turn"词面冲突已消除（059 家族 `rg 'team turn'` 零命中，或每条命中同行含"非 dsh turn"澄清语——改名/加注二择一）。
- **SC-005**: 回归：既有 team 大型测试（多局闭环、排队消化、取消、刷新重建、回填/断开收敛/多流去重）全量通过。

## Assumptions

- **术语基准**：turn/step 语义以 `survey/deepseek-harness-turn-step-semantics.md` 为准（源码级核实）；mid-turn 进入的机制对应 dsh 的 next-step 输入 claim（step 间只 claim next-step 输入；消息作为独立 user/message 落成员日志、位于工具结果之后），具体机制选型（成员输入队列的使用方式、编排层队列与成员输入的所有权划分）由 plan 阶段决定，spec 只约束行为。
- **实现优先 dsh 原生能力**（用户裁定，2026-09-11）：语义与 opencode 对齐（排队消息进入时机/位置/消费后无特殊语义——见 Clarifications 对齐确认），但实现 MUST 优先使用 dsh 及官方插件提供的能力（inbox 三入口 `followup`/`steer`/`inject` 与 claim 批语义、事件面、`agent/status`，见 Clarifications 机制确认），MUST NOT 迁移或复刻 opencode 代码——其 `SessionInput`/promotion 体系是 opencode 自身 runner 的内部实现，仅作语义参照，不引入本仓库。
- **多条消息合并形态**：同一边界内到达的多条排队消息一并进入该 step 输入（提交顺序保持）；独立多条与聚合为一条在模型可见内容上语义等价，具体形态由 plan 决定（v1 038 为聚合形态，dsh claim 为多条独立形态）。
- **实现机制留给 plan**：编排层（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`）与宿主（`projects/game/agent_v2/src/session.ts`/`history.ts`）如何基于 dsh 原生能力组合（inbox 入口选择、编排层队列与成员 inbox 的所有权划分、turn 在途的感知方式）、fake-llm 夹具如何断言 step 级输入历史——均不改变 FR 的终态行为，且选型范围以"实现优先 dsh 原生能力"裁定为界。
- **relay 边界**：团队广播仅在编排驱动边界消费（FR-005），不因本 feature 改变；本 feature 不触碰 team 编排的其他语义（交替激活、结构性续驱、gameEnded 复盘、取消暂停/恢复）。
- **视图顺序非约束**（用户澄清 2026-09-11，规范编码于 FR-005 与 Clarifications 的用户裁定记录）：spec 不为排序增设要求，测试仅以既有排序行为作回归断言（SC-003）。
- **049/059 为已交付 feature 的历史记录**：仅增补注记/修正表述，不重写原文语义（059 FR-011 的"回合结束后消化"子句由本 feature FR-001/FR-002 supersede，其余子句——排队呈现、广播、切换延后——保持）。
- **测试基建联动**：fake-llm 夹具与大型测试断言随进入语义变更同批更新（constitution 原则 VI：大型测试全量通过作为验收）。
