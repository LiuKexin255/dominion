# Feature Specification: Agent v2 Team 模式迁移（player + planner 双 agent）

**Feature Branch**: `059-agent-v2-team-mode`

**Created**: 2026-09-09

**Status**: Draft

**Input**: User description: "迁移 agent v1 的 team 模式到 agent v2，前期调研参考 @survey/deepseek-harness-agent-loop-prereq.md、@survey/deepseek-harness-memory-plugin.md、@survey/deepseek-harness-team-mode.md、@survey/deepseek-harness-roster-verification.md。1. 完全移除 game 项目下 agent v1 内容和引用。2. agent v2 先不做 compact 相关的功能。3. web ui 对于 saolei team 对话有两种视图：一是团队视图，所有的消息以原生的方式显示在对话列表中（即视图中消息取所属 agent 原始输出的内容，不是 wire 后的）；第二种是每个 agent 视角的视图，展示该 agent 的 history 消息列表。例如 team 模式下消息为 user: xxx / agent_1: yyy / agent_2: zzz，而对应的 agent_1 的视图则为 user: xxx / agent: yyy / user: [agent_2]zzz。团队视图有 1 个，agent 视图数量 = agent 数量。4. preset 分为 player 和 planner，preset 仍然只包括 persona，并根据自身角色锁定工具插件（根据前期调研，preset 选择的是插件行而不是单个工具，以保证工具组和配套 prompt 一起使用）。5. saolei-loop 驱动方式与 agent v1 类似：游戏开始时驱动 planner 输出指令，然后驱动 player 进行游戏，游戏结束后驱动 planner 总结。但注意原有的基于 langchain 的驱动模型不再适用，不要过度迁移旧版本方案；另外 planner 和 player 驱动时不需要额外的 message（agent v1 在驱动 planner 需要额外的提示词作为消息触发），新版本因为有群聊模型，可以直接将团队的消息历史输入给 agent 执行。6. web UI 调整为 session 下面 team 模型，而不是现在的单 agent 模型；对于 team 当中的每个 agent，可以查看 agent 实例的 system prompt 内容。"

## Motivation

`specs/051-agent-v2-dsh-migration/` 已交付 player 单角色的 dsh 迁移最小闭环（单 agent 物化、扫雷工具、desktop flow 控制、preset 管理）。本 feature 是迁移的**第三步**：把 agent v1 的 team 模式（player + planner 双 agent 协作）迁移到 agent v2，并将 session 的组织模型从单 agent 升级为 team；同时**完全移除 agent v1 的代码与引用**，终结双版本并存。

目标与现状的差距（本 feature 要完成的工作）：

| 维度 | 现状（051/054/055/057 后） | 目标 |
|---|---|---|
| session 组织 | session → agent 单例（AIP-156，仅 player） | session → team（player + planner 两成员，各持独立历史） |
| 驱动 | 自研 saolei-loop 作为 agent loop 层（`setFactory` 替换官方 agent-loop） | saolei-loop 升为 team loop 编排层（游戏阶段机、驱动时机）；agent 驱动回归官方 agent-loop 行 |
| preset | Mongo 单字段 `player_prompt`（无角色、无工具概念） | 按 role 分池（player/planner）；内容仅 persona；按角色锁定工具插件行（工具组+配套提示词一致生效） |
| 消息模型 | 单 agent 视角历史 | 群聊模型：用户输入与成员输出（发言、工具调用与结果）构成团队消息流，成员间 1:1 原样广播 |
| web UI | 单 agent 对话视图 | team 模型：1 个团队视图（原生输出归并）+ 每成员 1 个视角视图；成员 system prompt 可查看 |
| v1 残留 | agent v1 服务已退出部署，但代码、协议（TeamService/PromptService）、prompt 服务、v1 专属测试夹具仍存留 | 完全移除 |
| compact | — | 明确排除（不迁移 v1 的每 5 局压缩） |

前期调研已拍板全部架构决策，本 spec 直接以其为基线：

- **拓扑与消息模型**：`survey/deepseek-harness-team-mode.md`（2026-09-08 五轮决策）——双顶层 agent + team 层群聊模型；team 只投递不驱动（buffer 模型，buffer 从成员 log 派生重建）；广播 1:1、仅发送者标注、tool result 原样广播；preset 分池、角色差异编辑期固定于 preset 插件行、物化零定制；**saolei-loop 从 agent loop 层变为 team loop 层，官方 `dsh-agent-loop` 行保留**（该文 §9.3）。
- **planner 记忆**：`survey/deepseek-harness-memory-plugin.md`（2026-09-08 两轮决策 ①–⑧）——memory 单工具 + system prompt 快照注入（agent 启动时读一次、生命周期固定）；存储沿用 memory 服务。
- **roster 前提实证**：`survey/deepseek-harness-roster-verification.md`（058）——B1 直组形态下 preset roster 机制全部验证点通过（分池 roots、行级工具↔提示词一致、物化零定制、copy-then-patch 创作），team-mode 调研 §9.5 的"SDK 直组 + roster 无先例"探索项已消解。
- **agent-loop 机制**：`survey/deepseek-harness-agent-loop-prereq.md`——官方 agent-loop 依赖面与异常处理结论（其 §5.6"单 agent 双角色"决策已被 team-mode 取代，头部已注明）。

## Clarifications

> 本节为已裁定决策的来源记录（决策依据）；各裁定的终态规范已编码于 FR-008~FR-011、FR-014、FR-017 与 Assumptions 条款（2026-09-10 切换锚点澄清、无自动首驱/切换零合成消息与 team 流裁定的终态规范另编码于 [data-model.md](data-model.md) §5、[contracts/dsh-plugins.md](contracts/dsh-plugins.md) §2 与 [contracts/team-api.md](contracts/team-api.md) §3；proto 场景解耦裁定的终态规范另编码于 [contracts/team-api.md](contracts/team-api.md) §1–§3/§5、[contracts/preset-api.md](contracts/preset-api.md) 与 [data-model.md](data-model.md) §2/§4），规范冲突时以 FR/Assumptions 为准。

### Session 2026-09-09（用户裁定）

- **Q1（planner memory 插件范围）→ 纳入本 feature（选项 A）**：planner preset 锁定的工具插件组 = memory 插件——memory 单工具 + planner system prompt 记忆快照注入 + 存储沿用既有 memory 服务，完整对齐 v1 team 的跨局记忆功能。设计与验证依据 `survey/deepseek-harness-memory-plugin.md` 已拍板决策 ①–⑧（功能对齐 v1、插件两功能面、快照实例生命周期固定、team 广播下 player 可见已接受、存储沿用 memory 服务、挂载路径 A、scope 键 (template, session)、物化首读 fail-loud）。
- Q: team 的多局循环在 planner 复盘后是否自动继续下一局？ → A：编排层继续驱动 player 进入下一轮（结构性续驱），是否继续游戏（开启新局）由 player（LLM）自行决定。
- Q: Cancel 是否同时暂停自动续驱循环？ → A：取消 = 终止在途回合 + 暂停自动续驱；用户再次发送消息即恢复（消息由当前激活成员处理，循环继续）。
- Q: 用户消息是否可定向 planner（@planner）？ → 系统层面无定向：用户输入进入团队记录并广播全员，由**当前激活成员**处理（player/planner 交替激活）；排队消息同样驱动当前激活成员，且若消化排队消息时恰逢编排层即将切换成员（如 planner 回合结束准备切换 player），切换延后至排队消息消化完成后再继续编排；@标记仅为消息内容表达，不影响系统流程；planner 无需指令工具（v1 的 instruct_player 不迁移），成员间通信经团队消息流承载。

### Session 2026-09-10（用户澄清与裁定）

- Q: team 在 player 与 planner 之间切换的确切节点是什么？ → A：① **player → planner**：切换发生在游戏结束后——gameEnded 事实的确立来源是 **saolei 工具返回游戏终局结果**（工具结果层面，如失败/胜利返回）；切换评估遵循既有状态机优先级（player 回合结束时：排队消息先消化 → gameEnded? 是 → 驱动 planner 复盘 / 否 → 结构性续驱 player）。② **planner → player**：切换节点是 planner **完成静止**——turn 结束**并且**不会再触发新的 turn（两种新 turn 触发源都不存在：工具调用引发的后续 turn、待消化排队消息）；即"planner 回合结束"的切换评估必须确认 planner 已静止，才续驱 player。
- Q: spec 是否需要任何额外驱动消息？"物化后无需用户 Send 即自动出现 planner 开局策略"是否为需求？ → A（用户裁定）：**不是需求**。spec 不需要任何额外驱动消息；开始游戏的**首次驱动由用户消息触发**——物化（UpdateTeam）成功后 team 静止等待，不自动驱动任何成员（初始激活成员 = planner、初始相位 = planning），首条用户消息由 planner 处理。planner 与 player 的所有切换/续驱（planner→player 结构性续驱、player→planner gameEnded 复盘、排队消化）**不使用任何编排层合成的驱动消息**，直接以群聊消息（drain 返回的未消费团队消息历史 + 排队用户消息）驱动目标 agent（FR-010 的完全落实）；若无未消费群聊消息且无排队消息（无输入），编排层保持静止在当前激活成员——不合成消息、不空转（"结构性续驱"只在存在未消费输入时发生）。取消暂停后再次 Send 恢复（FR-017 既有语义：再次 Send 即有输入即驱动）。
- Q: 编排自动驱动的回合事件如何到达前端（web-views.md §2 悬置项）？实时承载应基于 Send 创建的 stream 流还是 List 面定时拉取？ → A（用户裁定）：**使用 Send 创建的 stream 流，并将流从 agent 回合提升为 team turn 持续流**——从发起持续输出，覆盖后续所有编排自动驱动的成员回合（结构性续驱 player、gameEnded 复盘、排队消化、多局循环），直到 team 静止（编排层无在途回合且无待消化输入）才结束流。同一流同时返回 agent message（成员原生事件 + member 标注）与 team message（归并序列条目 + seq 单调锚，与 ListTeamMessages 返回元素同构）；流是编排事件的订阅面——客户端断开/流取消不终止编排循环（取消编排仍走 Cancel RPC）；流的自然终点为 team 静止（含自然收敛与 Cancel 后的暂停静止），异常断开由客户端重连 + List 回填补齐。终态规范编码于 [contracts/team-api.md](contracts/team-api.md) §3（FR-017 流式响应对象）。
- Q: proto 会话面的 team 及相关定义是否应携带 saolei 场景特化（TeamRole/PresetRole 枚举、player_preset/planner_preset 等双角色字段）？ → A（用户裁定）：**team 及相关定义与 saolei 场景解耦**——role 用开放字符串：成员 role 为场景词汇（saolei 下 "player"/"planner"），用户消息标注为保留值 "user"，空字符串=未设置；Team 物化输入泛化为 members 列表（每成员 {role, preset, model?}），不设场景特化字段；saolei 场景约束（members 恰 2、roles 恰为 player/planner、preset.role 与成员 role 字符串相等、model 在目录）由 agent_v2 服务端校验承载（仍 INVALID_ARGUMENT，错误不含"proto 限制"色彩）。泛化层级：完全泛化（采纳）> oneof（备选不采用——泛化可行时徒增分支）> bytes（禁止——破坏接口协议鲁棒性）。终态规范编码于 [contracts/team-api.md](contracts/team-api.md)、[contracts/preset-api.md](contracts/preset-api.md)、[data-model.md](data-model.md)。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 完全移除 agent v1，交付纯净的 v2 基线 (Priority: P1)

`projects/game/agent`（LangChain/LangGraph 实现的 v1 服务）及其全部引用从 game 项目中移除：v1 专属的协议定义（TeamService、PromptService 及其消息类型）、v1 专属的 prompt 配置服务（Go）、workspace/Bazel 引用、v1 专属测试夹具与过期注释一并清理。被 v2 复用的共享资产（Session 资源、desktop 桥帧类型、memory 服务及其管理路由）保留。移除后全仓构建与测试通过，不存在任何指向 v1 的悬空引用。

**Why this priority**: 用户要求 1 的直接交付物；是后续所有 team 改造的干净起点——避免新旧两套 team 语义并存造成的混淆，也消除 v1 专属资产（协议、夹具）对 v2 演进的牵制。

**Independent Test**: 移除后执行全仓构建与测试；对 v1 服务目录、包名、协议 service 名、v1 专属夹具做代码检索，断言零残留；既有 v2 大型测试全部通过（回归不受影响）。

**Acceptance Scenarios**:

1. **Given** 仓库当前状态，**When** 移除完成，**Then** `projects/game/agent` 目录不存在，工作区配置与构建文件中无该包/目录的引用。
2. **Given** 协议文件中存在仅被 v1 实现与消费的 service（TeamService、PromptService）及其专属消息，**When** 移除完成，**Then** 这些定义不存在；仍被 v2 使用的共享类型（Session、desktop 桥帧）与 SessionService 不受影响，`/api/v1` 会话管理路由继续可用。
3. **Given** prompt 配置服务（Go）仅被 v1 消费且未部署，**When** 移除完成，**Then** 该服务从仓库移除；memory 服务及其既有管理路由保留且不受影响。
4. **Given** 测试基建中存在 v1 专属夹具（如 fake-llm 的 planner 场景夹具引用 v1 源码路径），**When** 移除完成，**Then** v1 专属夹具被移除或改写为不依赖 v1 的形态，fake-llm/fake-desktop 基建仍可支撑 v2 大型测试。
5. **Given** 移除完成，**When** 执行全仓构建与测试（含大型测试部署闭环），**Then** 全部通过。

---

### User Story 2 - 在 web 上与 team 协作完成多局扫雷游戏 (Priority: P1) 🎯 MVP

用户为 session 物化一个 team（选定 player preset 与 planner preset 及各自模型）后，team 静止等待用户消息；用户发送第一条消息即触发工作流（由当前激活成员 planner 处理）：planner 产出开局策略指令 → player 按策略执行游戏（调用扫雷工具，经 desktop 真实操作）→ 游戏结束（won/lost）→ planner 复盘总结并产出下一局策略 → 编排层自动驱动 player 进入下一轮（是否继续开局由 player 自行决定，典型情形为按策略开始下一局），循环往复。用户随时可以发消息参与（进入团队消息流广播全员，由当前激活成员处理）。全程的驱动不需要任何人工注入的额外触发提示词——编排层直接以团队消息历史驱动对应成员。web 对话页实时可见团队各成员的响应流。

**Why this priority**: 本 feature 的核心价值切片——"team 模式的多 agent 协作游戏"端到端成立；单独此故事即可演示 v1 team 模式在 v2 上的完整复刻与超越（群聊模型、零合成驱动消息）。

**Independent Test**: 部署后（大型测试以 fake LLM + fake desktop 替换真实端点）物化 team，断言：物化后 team 静止等待（无任何成员被驱动、不出现编排层合成的驱动消息）；用户发送第一条消息后 planner 产出开局策略消息；player 随后被驱动开始游戏并产出工具调用与结果；终局后 planner 自动产出复盘与下一局策略；下一局 player 按策略执行；除首条用户消息外全程无需用户发送任何额外触发消息。

**Acceptance Scenarios**:

1. **Given** session S 未物化 team，**When** 用户发送消息，**Then** 请求被明确拒绝并引导先完成 team 物化。
2. **Given** session S 已物化 team（player preset P1 + planner preset P2 + 各自模型），**When** 物化完成，**Then** team 静止等待用户消息（当前激活成员为 planner，不自动驱动任何成员、不出现任何编排层合成的驱动消息）；用户发送第一条消息后，planner 被驱动处理该消息并产出开局策略指令，随后 player 被驱动按策略开始游戏（调用扫雷工具）。
3. **Given** 一局游戏进行中（player 回合中），**When** 工具操作依次执行，**Then** 每次工具调用与结果对 team 全员可见（planner 能在后续被驱动时看到本局完整过程），desktop 收到并执行对应操作。
4. **Given** 某操作触发终局（won/lost），**When** 该操作结果返回，**Then** 编排层自动驱动 planner 复盘总结并产出下一局策略；planner 回合结束后编排层自动驱动 player 进入下一轮（无需用户触发；是否开启新局由 player 自行决定，典型情形为按策略开始下一局）。
5. **Given** 一局终局后 planner 被驱动复盘，**When** planner 调用 memory 工具记录跨局观察，**Then** 修改立即持久化到既有 memory 服务（经其管理路由可查证），调用与结果进入 planner 视角历史并经团队消息流对 player 可见（可区分来自 planner），planner system prompt 中的记忆快照保持不变（新快照待下次物化生效）。
6. **Given** 一局或多局进行中，**When** 用户发送消息，**Then** 消息进入团队消息流广播全员；若当前激活成员回合进行中则排队，回合结束后由当前激活成员消化；若此时编排层即将切换成员，切换延后至排队消息消化完成后继续。
7. **Given** team 已物化并对话多轮，**When** 用户刷新 team 配置（改 preset 或模型），**Then** 在途回合按既定终止语义处理、短期记忆清空、按新配置重建 team 并重新开始工作流。
8. **Given** desktop 未连接或中途断开，**When** player 调用需要桌面执行的工具，**Then** 工具以明确可读的错误结果返回（模型可见、非假成功），团队流程不崩溃、进程存活，重连后可继续。
9. **Given** team 的自动循环运行中（某成员回合进行中或等待续驱），**When** 用户执行取消，**Then** 在途回合按取消语义终止、排队消息保留为已固化历史且不触发新驱动、自动续驱暂停、活跃 team 流在静止点结束；用户再次发送消息后循环恢复（消息由当前激活成员处理）。

---

### User Story 3 - preset 分池管理与角色工具锁定 (Priority: P1)

用户在 web 上管理两类 preset：player preset 与 planner preset（分池）。preset 的可编辑内容仅 persona 一项；preset 所属的池决定其绑定的工具插件集——player preset 绑定扫雷游戏工具组，planner preset 绑定 memory 工具组——工具组与其配套提示词作为一个整体生效或缺席，不存在"有工具无守则"或"有守则无工具"的组合。物化 team 时分别从两个池中选择 preset；物化本身不引入任何角色定制。

**Why this priority**: 用户要求 4 的直接交付物；team 物化（US2）的配置前提，也是调研拍板的"插件 = 工具最小颗粒度、preset 选择插件行"决策的落地。

**Independent Test**: 通过 web（或服务 API）完成两类 preset 的创建/编辑/删除；分别以 player preset 与 planner preset 物化 team 成员，断言：player 成员只能使用扫雷工具组及其守则，planner 成员只能使用 memory 工具组及其守则；persona 编辑后重新物化即生效。

**Acceptance Scenarios**:

1. **Given** web 的 preset 管理界面，**When** 用户创建/编辑/删除 preset，**Then** 每个 preset 归属于明确的池（player 或 planner），可编辑字段仅 persona，标准资源操作成功且数据持久化（服务重启不丢失）。
2. **Given** player 池的 preset P，**When** 以 P 物化 team 的 player 成员，**Then** 该成员的可用工具恰为扫雷游戏工具组，其 system prompt 含对应工具守则、不含任何 planner 工具组的痕迹（反之亦然）。
3. **Given** preset 的 persona 留空，**When** 以该 preset 物化成员，**Then** persona 回退到该角色的默认基线提示词（对齐现有空值回退语义）。
4. **Given** team 已按 (P_player, P_planner) 物化，**When** 其中某个 preset 被编辑后刷新 team，**Then** 新 persona 生效且短期记忆清空（对齐既有刷新语义）。
5. **Given** 某个 preset 被 team 引用期间被删除，**When** team 继续对话，**Then** 已物化成员不受影响（内容已固化）；再次物化引用该 preset 被明确拒绝（对齐既有删除无 fan-out 语义）。

---

### User Story 4 - team 对话双视图：团队视图与成员视角视图 (Priority: P2)

对每个 saolei team 会话，web 对话页提供两类视图：**团队视图**（1 个）——所有消息以原生形态显示在对话列表中：用户消息显示为用户消息，各成员的消息取该成员自己的原始输出（正文、思考、工具调用与结果），不显示其他成员收到的转发/包装形态；**成员视角视图**（数量 = 成员数，即 player 与 planner 各一个）——展示单个成员视角的消息历史：用户消息显示为 user，该成员自己的输出显示为 agent，其他成员的消息以标注来源的 user 消息呈现（如 planner 视角中 player 的消息显示为 `user: [player]...`）。

**Why this priority**: 用户要求 3 的直接交付物；建立在 US2 的消息流之上，是 team 对话可观测性的核心——团队视图回答"团队里发生了什么"，成员视图回答"每个 agent 看到了什么"。

**Independent Test**: 完成一局含用户消息与双成员产出的对话后，断言：团队视图 1 个且内容为原生输出按时间归并；成员视角视图恰 2 个且各自内容符合"自己=agent、他人=标注来源的 user"形态；同一消息在团队视图与成员视图中的正文一致。

**Acceptance Scenarios**:

1. **Given** team 会话中已有用户消息、player 产出（含工具调用与结果）、planner 产出（含复盘正文），**When** 用户打开团队视图，**Then** 全部消息按时间顺序归并显示，每条成员消息以其原始输出形态（含工具调用块、思考块）归属到所属成员名下。
2. **Given** 同一对话，**When** 用户打开 player 的视角视图，**Then** 用户消息显示为 user、player 自己的输出显示为 agent、planner 的消息以标注来源的 user 消息显示（如 `user: [planner]...`，含其原样转发的工具调用与正文）。
3. **Given** 同一对话，**When** 用户打开 planner 的视角视图，**Then** 用户消息显示为 user、planner 自己的输出显示为 agent、player 的消息（含工具调用与结果全文）以标注来源的 user 消息显示。
4. **Given** 某成员的回合正在进行，**When** 用户查看任一视图，**Then** 该成员的流式输出实时呈现于团队视图与该成员自己的视角视图（其他成员视角视图在其消费该消息前不显示）。
5. **Given** team 配置刷新（短期记忆清空），**When** 用户查看各视图，**Then** 各视图与新生命周期一致（历史按清空后状态重建）。

---

### User Story 5 - 查看 team 成员的 system prompt (Priority: P2)

对 team 中的每个成员，用户可以在 web UI 查看该 agent 实例**当前生效的完整 system prompt 内容**（persona、团队名册与协作规则、工具守则，以及 planner 成员的记忆快照的装配结果）。内容与该实例实际发给模型的系统提示词一致，用于理解各成员的行为差异与调试配置。

**Why this priority**: 用户要求 6 的后半部分；多 agent 透明性的基础能力，实现成本低、独立可验证。

**Independent Test**: 物化 team 后查看两个成员的 system prompt，断言：内容完整可读、两者因角色不同而不同（persona、工具守则、player 无记忆快照而 planner 有）、与物化配置一致；刷新 team 后内容随新配置更新。

**Acceptance Scenarios**:

1. **Given** team 已物化（player: P1, planner: P2），**When** 用户查看 player 成员的 system prompt，**Then** 完整内容可见，含 P1 的 persona、团队名册、扫雷工具守则，不含 memory 工具守则与记忆快照。
2. **Given** 同一 team，**When** 用户查看 planner 成员的 system prompt，**Then** 内容含 P2 的 persona、团队名册、memory 工具守则与记忆快照，与 player 的 system prompt 明确不同。
3. **Given** 用户编辑了 P1 的 persona 并刷新 team，**When** 再次查看 player 成员的 system prompt，**Then** 内容反映新 persona。

---

### Edge Cases

- **刷新 team 与在途回合并发**：按既定终止语义处理（在途回合终止、排队消息作废）后清空记忆重新物化，不产生半清理状态（对齐现状 Update 语义）。
- **desktop 缺席/断连/未绑定窗口**：需要桌面执行的工具以明确错误结果返回（模型可见），团队流程不崩溃；重连后可继续（对齐现状语义）。
- **游戏进行中的用户消息**：进入团队消息流并排队，当前回合结束后由当前激活成员消化；若恰逢编排层即将切换成员，切换延后至排队消息消化完成；不中断进行中的游戏。
- **成员驱动失败**（如模型请求失败）：按既有错误终止语义处理该成员回合，错误对用户可见（团队视图错误呈现），team 不进入未定义状态；可再次驱动。
- **终局判定异常**（识别失败等）：游戏状态失效时后续游戏操作按既有拒绝语义处理直至重新开局，team 驱动不进入死循环。
- **planner 上下文增长**（compact 明确排除）：多局后 planner 视角的工具调用历史持续增长，本 feature 接受为已知限制（不裁剪、不压缩），治理留待后续 feature。
- **planner 物化时 memory 服务不可达**：该次 team 物化失败并整体回滚（无半物化状态），可重试（fail-loud，对齐既有物化校验语义）。
- **运行中经管理路由外部修改 memory**：已物化 planner 的记忆快照不变（实例生命周期内固定），下次物化生效（最终一致语义）。
- **preset 被删除时仍被 team 引用**：已物化成员不受影响；再次物化引用被明确拒绝（对齐现状）。
- **team 未物化时的操作**：Send 被明确拒绝；视图与配置面板呈现未物化引导态（对齐现状单 agent 引导模式）。

## Requirements *(mandatory)*

### Functional Requirements

**v1 移除**

- **FR-001**: 系统 MUST 完全移除 agent v1 服务（`projects/game/agent` 目录）及其在构建系统、工作区配置中的全部引用。
- **FR-002**: 系统 MUST 移除仅被 agent v1 使用的协议定义（TeamService、PromptService 及其专属消息类型）与 v1 专属的 prompt 配置服务；被 v2 复用的共享资产（Session 资源与 SessionService、desktop 桥帧类型、memory 服务及其管理路由）MUST 保留且行为不变。
- **FR-003**: v1 专属测试夹具 MUST 移除或改写为不依赖 v1 的形态；测试基建 MUST 继续支撑 v2（含本 feature team 模式）的大型测试。

**team 模型与 preset**

- **FR-004**: session 下的组织模型 MUST 从单 agent 调整为 team：一个 saolei session 至多物化一个 team；team 恰含 player 与 planner 两个成员（agent 实例），成员各自独立持有对话历史、persona 与 system prompt。
- **FR-005**: 用户 MUST 能为 session 物化 team：分别指定 player preset 与 planner preset（各自必选，所属池必须匹配角色）与各自模型（可选，缺省为部署默认）；对已存在 team 的再次物化为刷新（终止在途回合、清空全部成员短期记忆、按当前配置重建）。
- **FR-006**: preset MUST 按角色分为 player 池与 planner 池；preset 的用户可编辑内容 MUST 仅含 persona；player 池的 preset MUST 绑定扫雷游戏工具插件组、planner 池的 preset MUST 绑定 memory 工具插件组（FR-007）——工具组与其配套提示词 MUST 作为整体生效或缺席（选择单位是插件组而非单个工具），任何成员 MUST NOT 出现工具与守则不一致的组合。
- **FR-007**: planner preset MUST 绑定 memory 工具插件组，提供对 planner 长期记忆的单一 memory 工具：支持新增/替换/移除与批量原子操作（全成功才提交），以子串定位待修改内容，无独立读取动作；操作结果（含失败）MUST 以普通文本结果呈现、不中断对话；memory 修改 MUST 立即持久化于既有 memory 服务（scope 键沿用 (template, session) 资源模型）。planner 实例的 system prompt MUST 注入物化时刻的长期记忆快照，快照在实例生命周期内固定（不随写刷新，运行中的外部修改待下次物化生效）；记忆修改过程 MUST 经 planner 的工具调用历史对 planner 可见，并经团队消息流对其他成员可见（可区分来自 planner）。

**团队消息流与驱动**

- **FR-008**: team MUST 以群聊模型同步消息：用户输入与各成员的输出（发言、工具调用及其结果）构成团队消息流；任一成员的输出 MUST 以 1:1 原样形态对其他成员可见——注入其他成员的消息为"发送者标注头行 + 标签对包裹的原样正文"结构：正文 MUST 含完整原文（发言全文、工具调用的完整输入与结果全文），头行可含简短摘要标签仅作标注，MUST NOT 以摘要/引用替代或截断正文。成员间通信（含 planner 向 player 传达策略）MUST 经团队消息流承载，MUST NOT 引入专用指令传递工具（v1 的 instruct_player 工具不迁移）。
- **FR-009**: 成员的驱动时机 MUST 由编排层统一持有：team 物化成功后 MUST 处于静止等待（初始激活成员 = planner、初始相位 = planning），MUST NOT 自动驱动任何成员——开始游戏的首次驱动由用户第一条消息触发（由当前激活成员 planner 处理，FR-011），planner 产出开局策略指令；随后驱动 player 执行游戏；游戏结束（won/lost）后驱动 planner 复盘总结；planner 回合结束后编排层 MUST 自动驱动 player 进入下一轮（结构性续驱，无需用户触发），是否继续游戏（开启新局）由 player 自行决定；循环持续直到用户干预（取消/刷新 team）。一切切换与续驱（planner→player 结构性续驱、player→planner gameEnded 复盘、排队消息消化）MUST 直接以群聊消息（drain 返回的未消费团队消息历史 + 排队用户消息）驱动目标成员，编排层 MUST NOT 合成任何驱动消息（FR-010 的完全落实）；无未消费团队消息且无排队消息时，编排层 MUST 保持静止在当前激活成员（不合成消息、不空转）。team 成员 MUST NOT 被团队消息自动唤醒（谁执行、何时执行只由编排层决定）。
- **FR-010**: 驱动任一成员时 MUST 以该成员尚未消费的团队消息历史作为驱动输入；编排层 MUST NOT 依赖额外合成的触发提示词消息（区别于 agent v1 驱动 planner 需内部构建复盘请求提示词的形态）。
- **FR-011**: 用户在会话中发送的消息 MUST 进入团队消息流并广播全员（系统层面不存在对特定角色的定向投递；消息内容中的 @ 标记仅为内容表达，不影响系统流程）；消息 MUST 由编排层驱动当前激活成员处理（任一时刻至多一个成员被驱动，player/planner 交替激活）；当前成员回合进行中到达的用户消息 MUST 排队，回合结束后由当前激活成员消化——若此时编排层即将切换成员（如 planner 回合结束准备切换 player），切换 MUST 延后至排队消息消化完成后继续。
- **FR-012**: 桌面控制 MUST 由 player 独占；desktop 连接与绑定语义（以 session 为单位、新连接接管、断连错误语义）在 team 模型下保持不变。

**web UI**

- **FR-013**: web UI MUST 以 session → team 模型组织（替换现有单 agent 模型）：team 配置面板支持物化/刷新（选两个池的 preset 与模型）、状态呈现（物化状态、desktop 连接、成员清单）。
- **FR-014**: web UI MUST 为每个 team 会话提供恰好 1 个团队视图：全部消息（用户与各成员）按时间归并，各成员消息取该成员的原始输出（正文、思考、工具调用与结果），MUST NOT 显示转发/包装形态。团队视图的实时呈现与 List 回填 MUST 共用同一归并序锚（`seq`，与 ListTeamMessages 同源，经 team 流的 `team_message` 帧到达，[contracts/team-api.md](contracts/team-api.md) §3）。
- **FR-015**: web UI MUST 为 team 中每个成员提供 1 个视角视图（数量 = 成员数）：该成员视角下用户消息为 user、自己的输出为 agent、其他成员的消息为标注来源的 user 消息。
- **FR-016**: web UI MUST 支持查看 team 中每个成员实例当前生效的完整 system prompt 内容，且内容 MUST 与该实例实际使用的系统提示词一致。
- **FR-017**: 既有对话能力（流式响应、排队消息、取消、历史回填、未物化拒绝与引导）MUST 在 team 模型下继续成立（对象从单 agent 扩展为 team 及其成员）。流式响应的承载 MUST 为 **team 流**（[contracts/team-api.md](contracts/team-api.md) §3）：Send 建立的流 MUST 从发起持续输出至 team 静止（编排层无在途回合且无待消化输入），覆盖其间全部成员回合（含编排自动驱动的回合）；同一流 MUST 同时承载成员事件帧（member 标注）与 team message 帧（归并序列条目，seq 与 ListTeamMessages 同源同值）；流 MUST 为编排事件的订阅面——客户端断开 MUST NOT 终止编排循环，取消编排仅经 Cancel。取消在 team 模型下的语义 MUST 为：终止在途回合（无论哪个成员被驱动）+ 暂停编排层自动续驱 + 排队消息保留为已固化历史且不触发新驱动；用户再次发送消息即恢复循环（消息进入团队消息流由当前激活成员处理，FR-011）；操作幂等。

**范围排除**

- **FR-018**: 本 feature MUST NOT 实现 compact/上下文压缩功能（v1 的每 5 局通道压缩与压缩后指令不迁移）；成员上下文增长接受为已知限制。

### Key Entities

- **Team**：session 下的团队组织（协作目标、成员名册）；一个 session 至多一个 team；是配置（物化/刷新）与呈现（团队视图）的单位；任一时刻至多一个成员处于激活（被驱动）状态，player/planner 交替激活。
- **Team Member（成员 agent 实例）**：player（执行游戏操作，独占桌面控制）或 planner（开局策略、局后复盘、长期记忆）；各持独立的对话历史、视角视图与 system prompt。
- **Preset**：按角色分池的配置模板；内容 = persona（唯一用户编辑面）+ 所属池固定的工具插件组（工具 + 配套提示词整体）。
- **团队消息流（Team Conversation）**：用户输入与各成员原生输出的归并序列；团队视图的数据源；成员间以 1:1 原样转发相互可见。
- **成员视角历史（Agent View History）**：单个成员被驱动时实际消费与产生的消息序列；成员视角视图的数据源。
- **System Prompt（实例化结果）**：成员实例生效的完整系统提示词装配结果（persona + 团队名册与协作规则 + 工具守则 + 记忆快照[仅 planner]）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 全仓构建与测试通过；对 v1 服务目录、包名、v1 专属协议 service、v1 专属夹具的代码检索确认零残留引用。
- **SC-002**: 部署环境可完成完整多局闭环——物化后 team 静止等待、用户首条消息触发 planner 产出开局策略、一局游戏至终局、自动复盘总结与下一局策略、下一局执行——除首条用户消息外全程无需用户发送任何额外触发消息（大型测试全部用例通过，含本 feature 新增用例与既有回归用例，无 failed/flaky）。
- **SC-003**: 每个已物化的 team 会话提供 1 个团队视图 + 数量等于成员数的成员视角视图；三类典型消息（用户消息、含工具调用的成员产出、策略正文产出）在团队视图与各成员视角视图中的呈现符合 FR-014/FR-015 定义，同一消息跨视图正文一致。
- **SC-004**: player 成员与 planner 成员的可用工具集和 system prompt 因角色严格分化（player 恰为扫雷工具组、planner 恰为 memory 工具组并含记忆快照），不存在工具与守则不一致的组合；preset 数据持久化（服务重启不丢失）；memory 修改即时持久化于既有 memory 服务（快照在实例生命周期内固定）。
- **SC-005**: 每个成员的完整 system prompt 可在 web UI 查看且与实际生效内容一致；配置刷新后视图与 system prompt 随新配置更新。

## Assumptions

- **v1 移除边界**（依据前期调研与用户"完全移除"要求）：memory Go 服务及既有 memory 管理路由保留（`survey/deepseek-harness-memory-plugin.md` 决策 ⑤：存储沿用 memory 服务）；prompt 配置服务（Go）随 v1 移除（v1 专属消费方、未部署）；SessionService 与 desktop 桥帧类型保留（v2 复用中）。
- **API 形态**：现有 `/api/v2` 的 agent 单例模型被 team 模型替换（用户明确"session 下面 team 模型，而不是现在的单 agent 模型"）；agent 为进程内存态，无需存量数据迁移，旧 session 在新模型下呈现未物化引导态。
- **开局驱动时机**：team 物化后静止等待用户消息（初始激活成员 = planner、初始相位 = planning，不自动驱动任何成员）；游戏的首次驱动由用户第一条消息触发（由 planner 处理）；后续局的策略来自 planner 复盘产出。
- **用户输入语义**：用户消息进入团队消息流广播全员，由当前激活成员处理（player/planner 交替激活，对齐 v1 交替驱动形态）；@标记仅为内容层面表达，系统不解析、不影响投递与驱动；planner 无指令工具（成员间通信经团队消息流，v1 的 instruct_player 不迁移）。
- **compact 排除的衍生限制**：planner 视角历史随局数持续增长（工具结果原样转发，不裁剪）；本 feature 接受该 token 代价（调研决策 ⑬ 同源结论），治理留待后续 feature。
- **memory 快照刷新边界**：快照在 planner 实例物化时读取一次、生命周期内固定（`survey/deepseek-harness-memory-plugin.md` 决策 ③）；v1 的"每 5 局压缩边界刷新"不迁移（与 compact 排除一致），快照与写入差异由 planner 自己的调用历史补偿呈现。
- **测试基建**：大型测试继续以 fake LLM + fake desktop 支撑确定性验证；需要 team 模式的双角色（player/planner）对话夹具（随 FR-003 的 v1 夹具处置一并建设）。
- **模型选择**：两成员的模型独立选择（可相同可不同），沿用现有"模型在物化时选择、与目录同源校验"的机制。
