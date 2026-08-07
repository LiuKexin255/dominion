# Feature Specification: Planner 长期记忆与校准指令 (Planner Memory & Calibration)

**Feature Branch**: `039-planner-memory-calibration`

**Created**: 2026-08-07

**Status**: Draft

**Input**: User description: "对 specs/031-team-template-mode/ 改进：1) 将 saolei mcp 的 click、flag、chord 改为支持批量操作的 saolei_operate 方法（保留操作顺序），planner 游戏历史同步改为 saolei_operate 含全部操作列表；2) 移除 player/planner 共享策略记忆，改为 planner 复盘完成后向 player 发送策略指令（team 初始化触发 planner 发送一次），player 不再持有长期存储，planner 持有自己的长期存储；3) planner 向 player 发送的指令不直接 invoke player，而是与下一条消息一起 invoke；4) 新建 memory mcp（agent 上）+ memory 服务（grpc-go）承载长期记忆，agent 转发请求到 memory 服务；5) memory 资源 pattern templates/{template}/sessions/{session}/memories/{memory}，注入系统提示词须含 memory_id，memory_add/update/remove 含 memory_id 参数。"

## Clarifications

本节记录需求方在本次改进中对既有 `specs/031-team-template-mode/` 核心契约（共享 StrategyStore、FR-013/014/015、super turn 流程）所作的破坏性变更决策。这些决策直接写入下列功能需求，方案（plan）阶段不再重新讨论。

### Session 2026-08-07（需求方 directive，本次立项）

- **共享策略记忆 → 废弃**：移除 player/planner 共享的 `StrategyStore`（`specs/031-team-template-mode/contracts/strategy-store-contract.md`）。player 不再持有任何长期存储，不再以"当前态势"读取策略（推翻 FR-013/FR-015）。planner 持有自己的内部长期记忆（见下）。
- **planner 长期记忆 → 独立长期存储（memory 服务）**：planner 的长期记忆经新建的 memory 服务承载，经 memory mcp（部署在 agent 服务上）暴露 add/update/remove 工具；agent 服务转发请求到 memory 服务，mcp server 不直连 memory 服务。前期调研结论（hermes 式冻结快照 + 压缩刷新边界）见 `survey/planner-memory-and-agent-communication.md` §3/§4/§5（D1/D2/D4/D5）。
- **planner→player 校准指令 → 取代策略注入**：planner 在复盘完成后向 player 发送策略指令；team 初始化时触发 planner 发送一次指令；当复盘触发压缩时，压缩完成后 planner 发送一次指令（语义同 team 初始化）。指令取代了原 FR-014/FR-015 的策略注入路径。
- **指令投递 → 分两种情形（澄清 Session 2026-08-07 修正）**：
  - **正常复盘（未触发压缩）**：planner 产生的指令在同一 team turn 内**立即**注入 player 上下文（紧跟最后一个 tool result，player 视角），graph 路由 planner→player，player 继续游戏；**是否自动进行下一局由 player LLM 决定，框架不做干扰**（保留 031 FR-009 多局能力；调研 D3 super-turn 循环适用此情形）。调研 D6：作为 HumanMessage 进入 player 通道。
  - **压缩后（以及 team 初始化）**：指令与 player 的**下一次激活**一同注入（不立即激活 player）。压缩后 turn 结束（player 停下、等待下一次消息），避免压缩后字段进入下一轮——与 037"压缩后自动停下"保持一致。team 初始化时无运行中的 player，指令自然随首次 player 激活注入。
- **（澄清 Session 2026-08-07）** `- Q: 一次 team turn 中 player 能否自主连续多局，还是每局复盘后 turn 即结束等待下次激活？ → A: "与下一条消息一起 invoke" 仅限压缩后；正常游戏结束指令紧跟最后一个 tool result 立即注入 player（同 turn 继续），是否开下一局由 LLM 决定、框架不干扰。`
- **（澄清 Session 2026-08-07）** `- Q: saolei_operate 批量列表中某操作被校验拒绝时，剩余操作如何处理？ → A: 按拒绝原因细分——无害空操作（不影响游戏，如在数字格上 click/flag）跳过并继续剩余操作；游戏结束或结构性/上下文错误（越界、无活动局）在该处停止、剩余不执行。`
- **（澄清 Session 2026-08-07）** `- 补充（正常游戏结束的消息顺序）：确认正常复盘情形 player 通道可见顺序为 player tool_calling → tool_result（游戏结束）→ planner 指令 → player 下一条 message output；planner 复盘在 planner 自身通道、对 player 不可见。指令在 player 响应游戏结束 tool_result（即其下一条输出）之前注入。`
- **（澄清 Session 2026-08-07）** `- 补充（memory 存储隔离）：memory 服务使用与 agent/prompt 同一个 mongo 实例作为存储，但按 style/mongo.md 要求，memory 服务 MUST 使用自己独立的数据库，MUST NOT 与 agent（或 prompt）服务的数据库混用。`
- **（澄清 Session 2026-08-07）** `- 补充（指令为可选 + 新建发送工具）：planner 通过新建的"指令发送工具"（功能类似原 update_strategy，但投递给 player 对话流）向 player 发送指令；planner 仅在需要时发送，**非每次游戏结束都必须发送**（由 planner 复盘后决定）。`
- **（澄清 Session 2026-08-07）** `- 补充（两场景拆分）：正常游戏结束 = 复盘 + 可选指令（携带游戏历史）；team 初始化 / 压缩后 = 无游戏历史指令产出（因 player 指令历史已被清理），且这两次产出都不触发 player invoke。建议拆分两场景的 node 逻辑（plan 落实）。（原措辞"强制发送"经 R4 修正为 prompt 引导、LLM 决定，见下。）`
- **（修正 Session 2026-08-07，plan 评审）** `- R4 修正：init/compact 场景不强制检验 instruct_player，仅在 prompt 要求 planner 给予 player 指令，最终是否调用由 LLM 决定；与正常 gameend 的 prompt（"必要时才调用"）区分。两场景均为 LLM 决定，差异在 prompt 强度（要求 vs 必要时）与游戏历史有无。`
- **（修正 Session 2026-08-07，plan 评审）** `- R2 修正：team 初始化的 initInstruction 改为**异步**执行（`UpdateTeam(allow_missing=true)` 物化路径——graph 首建——后即返回，不等 LLM；原"CreateTeam 构建 graph 后"表述被 specs/040-team-singleton-conformance/ supersede）；需解决与 desktop 的状态同步——desktop 正确进入 agent typing 状态，且此期间到达的 user message 须正确排在 planner 指令之后。`
- **（修正 Session 2026-08-07，plan 评审）** `- R3 修正：memory mcp 不必与 saolei mcp 共用单 path；每个 mcp 一个独立 path，且 path 须包含 template 字段（template-scoped，team 改造前的路径风格）。`
- **（修正 Session 2026-08-07，plan 评审）** `- R6 修正：MemoryService.ListMemories MUST 支持分页（AIP-158，`style/api.md` 引用），含 page_size/page_token/next_page_token。`
- **saolei 操作 → 批量化**：将 `saolei_click`/`saolei_flag`/`saolei_chord_click` 三个落子工具合并为支持批量操作的 `saolei_operate`（保留操作顺序）；`saolei_init`/`saolei_remain` 保持独立不变。批量执行只返回一次结果。同步给 planner 的游戏历史改为记录 `saolei_operate`（含全部操作列表），注入 planner 的工具描述须说明 `saolei_operate` 及其支持的全部操作（click/flag/chord），与游戏历史对应。
- **memory 资源 pattern**：`templates/{template}/sessions/{session}/memories/{memory}`（集合段采用复数 `memories`，与本仓库 `messages/{message}`、`profiles/{profile}` 的 AIP-122 约定一致；"memory"作为可数资源条目，复数 "memories"）。template 与 session 经独立 mcp server path 闭包注入（同 saolei mcp 既有做法）。注入系统提示词时 MUST 以 `memory_id: 内容` 形式呈现，使 LLM 能定位要修改的条目；`memory_add`/`memory_update`/`memory_remove` MUST 包含 `memory_id` 参数，其余参数参考 hermes（content 等）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - saolei 批量落子操作 (Priority: P1)

player（独占桌面控制的 agent）在观察扫雷棋盘后经常需要一次性连续执行多个落子动作（揭示一片、连续标记、双击展开等）。当前每个落子是独立工具调用（`saolei_click`/`saolei_flag`/`saolei_chord_click`），每次返回一次结果，LLM 批量执行大量操作时产生多次往返。本故事将这三个落子工具合并为单个支持批量操作的 `saolei_operate` 工具：一次调用接收一个有序操作列表，按列表顺序依次校验并执行，只返回一次结果（反映执行后的最终棋盘与状态）。`saolei_init`（开局）与 `saolei_remain`（只读查询）保持独立不变。同步给 planner 的游戏历史也改为以 `saolei_operate` 为单位记录（每次批量调用作为一条含全部操作列表的历史项），并使注入 planner 的工具描述说明 `saolei_operate` 及其支持的全部操作类型，与游戏历史一一对应。

**Why this priority**: 落子工具是 player 控制游戏的唯一执行面，也是 planner 复盘所依据的游戏过程的来源。批量化直接降低 LLM 批量操作时的调用往返与结果开销，且统一了 player 工具面与 planner 历史面的一致性。它是其余故事的执行基础（planner 复盘、校准指令都依赖一致的游戏过程记录），但本身与记忆改造解耦，可独立验证。

**Independent Test**: 可在具备桌面（或桩桌面）的环境中独立验证：`saolei_operate` 接收一个有序操作列表，操作按顺序依次执行，只返回一次结果；单元素列表等价于单次落子；`saolei_init`/`saolei_remain` 行为不变；同步给 planner 的历史以 `saolei_operate`（含全部操作）为单位记录，且 planner 工具描述包含 `saolei_operate` 与 click/flag/chord 三种操作。

**Acceptance Scenarios**:

1. **Given** player 的工具面，**When** 审查可用工具，**Then** 落子类仅有 `saolei_operate`（无 `saolei_click`/`saolei_flag`/`saolei_chord_click`），且 `saolei_init`、`saolei_remain` 仍存在。
2. **Given** 一个已开局的游戏，**When** player 调用 `saolei_operate` 传入一个含多个操作（click/flag/chord 混合）的有序列表，**Then** 操作按列表顺序依次校验并执行，工具只返回一次结果（反映最终棋盘与游戏状态）。
3. **Given** `saolei_operate` 的单元素操作列表，**When** 执行，**Then** 其行为等价于原对应单次落子工具（同样校验、同样落子、同样返回棋盘与状态行）。
4. **Given** 一个批量调用中某操作使游戏以 won/lost 结束，**When** 执行到该操作，**Then** 该操作生效、之前已成功操作生效，工具在该处停止，单次结果反映结束时的棋盘与 won/lost 状态，剩余操作不执行。
5. **Given** 一个批量调用中某操作因目标格状态成为无害空操作被拒（如对已揭示数字格 click），**When** 执行到该操作，**Then** 该操作被跳过（不改变棋盘），批量继续执行剩余操作，单次结果反映全部处理后的最终棋盘。
6. **Given** 一个批量调用中某操作因结构性/上下文原因被拒（越界 / 无活动局），**When** 执行到该操作，**Then** 在该处停止、之前已成功操作生效，单次结果反映停止时状态与原因，剩余操作不执行。
7. **Given** 同步给 planner 的游戏历史，**When** player 执行一次 `saolei_operate` 批量调用，**Then** 该次调用在历史中记录为一条含全部操作列表的 `saolei_operate` 项（而非拆成多条单步记录）。
8. **Given** 注入 planner 的工具描述，**When** 审查该描述，**Then** 其说明 `saolei_operate` 工具及其支持的全部操作类型（click/flag/chord），且与游戏历史中记录的 `saolei_operate` 操作类型一一对应。

---

### User Story 2 - planner 长期记忆与 memory 服务 (Priority: P1)

planner 作为复盘规划者需要跨多局累积对 player 的校准经验与自身认知演化。当前 planner 与 player 共享一个被整体替换的策略文本（`StrategyStore`），LLM 每次从零重写，既贵又易丢失累积洞察。本故事为 planner 建立其**自身**的长期记忆能力：新建一个 memory 服务（grpc-go）承载长期记忆条目，并在 agent 服务上新建 memory mcp server 暴露 `memory_add`/`memory_update`/`memory_remove` 工具（仅 planner 持有）；memory mcp server 由 agent 服务将请求转发到 memory 服务，而非 mcp server 直连 memory 服务。planner 的长期记忆以**冻结快照**方式注入 planner 系统提示词（一次烘焙、跨多次复盘保持冻结，调研 D2），并在压缩边界刷新（调研 D4）；注入时每条记忆以 `memory_id: 内容` 呈现，使 LLM 能精确定位要修改的条目。memory 资源以会话为作用域：`templates/{template}/sessions/{session}/memories/{memory}`，其中 template 与 session 经独立 mcp server path 闭包注入（同 saolei mcp 既有做法）。

**Why this priority**: planner 的长期认知演化是"团队随多局游戏变好"的核心机制。它把"整体替换的单文档策略"升级为"可逐条增删改的有界记忆集合"（调研 §3.1.3 Collection 模式），并引入独立 memory 服务作为承载。它是校准指令（US3）的认知来源，且与 US1 解耦，可独立验证。

**Independent Test**: 可独立验证：memory 服务持久化记忆条目并经 agent 转发可被 memory mcp 工具增删改；planner 持有且仅持有这三个记忆工具；记忆以冻结快照注入 planner 系统提示词且每条带 `memory_id`；记忆跨进程重启持久；mcp server 不直连 memory 服务（请求经 agent 转发）。

**Acceptance Scenarios**:

1. **Given** planner 的工具面，**When** 审查其工具，**Then** planner 持有 `memory_add`/`memory_update`/`memory_remove` 三个记忆工具，且无任何策略读取/写入工具（无 `update_strategy`）。
2. **Given** memory mcp server 与 memory 服务，**When** planner 调用 `memory_add`，**Then** 请求经 agent 服务转发到 memory 服务，记忆被持久化（条目可经 `memory_update`/`memory_remove` 以 `memory_id` 定位修改/删除）。
3. **Given** memory mcp server 的连接拓扑，**When** 审查其实现，**Then** mcp server 部署在 agent 服务上，由 agent 服务向 memory 服务转发请求；mcp server MUST NOT 直接连接到 memory 服务。
4. **Given** `memory_add`/`memory_update`/`memory_remove` 三个工具，**When** 审查其参数，**Then** 三者 MUST 均包含 `memory_id` 参数（用以标识新增的目标 id / 定位要更新或删除的条目）；其余参数（如 content）参考 hermes 既有记忆工具。
5. **Given** planner 的系统提示词，**When** 注入长期记忆冻结快照，**Then** 每条记忆以 `memory_id: 内容` 形式呈现，使 LLM 能据 `memory_id` 调用 update/remove。
6. **Given** 已写入的记忆，**When** planner 跨多次复盘被激活，**Then** 其系统提示词中的记忆快照在刷新边界之前保持冻结（不随每次激活重读变化；调研 D2/D5 冻结缓存）。
7. **Given** memory 资源，**When** 审查其作用域，**Then** 记忆以会话为作用域，资源 pattern 为 `templates/{template}/sessions/{session}/memories/{memory}`；template 与 session 经独立 mcp server path 闭包注入（player 不读取记忆）。
8. **Given** 已写入的记忆，**When** 服务进程重启，**Then** 记忆仍可被读取/更新/删除（持久化）。

---

### User Story 3 - planner→player 校准指令与共享记忆废弃 (Priority: P1)

planner 每局复盘后应把对 player 的近期校准作为对话内容投递给 player，而非把整体策略作为隐藏的系统注入。本故事废弃 player/planner 共享的 `StrategyStore`：player 不再持有任何长期存储，也不再以"当前态势"读取策略；改为 planner 通过一个**指令发送工具**（功能类似原 `update_strategy`，但投递进 player 对话流）向 player 发送**策略指令**（作为 player 对话流的一部分，累积、可引用，调研 D6 openclaw 式 HumanMessage）。指令分两个场景（澄清 Session 2026-08-07）：**正常游戏结束复盘**——planner 复盘后**可选**地经工具发送指令（由 planner 决定是否需要，非每次都必须发送），携带游戏历史；该指令（若发送）在同一 team turn 内立即注入 player 上下文（紧跟游戏结束 tool_result），graph 路由回 player 继续、是否开下一局由 player LLM 决定（保留 031 FR-009，框架不干扰）。**team 初始化 / 压缩后**——因 player 的指令历史已被清理，**经 prompt 引导** planner 产出一次**无游戏历史**的指令（prompt **要求** planner 给 player 指令，但最终是否调用 `instruct_player` 由 LLM 决定，区别于正常复盘 prompt 的"必要时才调用"），且这两次发送都**不触发 player invoke**：压缩后 turn 结束（player 停下、与下次激活一同注入，避免压缩后字段进入下一轮，与 037"压缩后自动停下"一致），初始化时**异步**产出（`UpdateTeam(allow_missing=true)` 物化后不等 LLM）随首次激活注入。两场景建议拆分 node 逻辑（FR-019）。

**Why this priority**: 这是本次改进对团队协作模型的根本重塑——把"隐藏的整体策略注入"改为"对话内可见的校准指令循环"，使 player 的对局行为可被 planner 显式引导、且指令在对话历史中可追溯。它与 US2 共同构成新的记忆架构（planner 内部长期记忆 + planner→player 近期指令分工），依赖 US2 的记忆能力作为指令的认知来源，但指令投递语义本身可独立验证。

**Independent Test**: 可在具备 LLM 与桌面（或桩桌面）的环境中独立验证：player 不再读取任何策略/长期记忆；planner 持有指令发送工具，正常复盘后按 prompt"必要时才调用"（可选）；team 初始化与压缩后经 prompt 引导产出无游戏历史指令（LLM 决定是否调用）、且不触发 player invoke（压缩后 turn 结束、与下次激活一同注入；初始化异步产出随首次激活注入）。

**Acceptance Scenarios**:

1. **Given** 共享策略记忆（`StrategyStore`），**When** 本特性落地，**Then** 该共享记忆及其 get/put 契约、`update_strategy` 工具、player"当前态势"注入路径均被移除（player 不持有任何长期存储）。
2. **Given** 一个新创建的 team（team 初始化），**When** `UpdateTeam(allow_missing=true)` 物化返回（initInstruction 异步触发，仅 graph 首建；profile 变更重建不重跑 init），**Then** planner 经 prompt 引导产出一次**无游戏历史**的策略指令（prompt 要求给指令、LLM 决定是否调用 instruct_player）；初始化时无运行中的 player，该发送不触发 player invoke，指令随 player 的首次激活一同注入（异步产出期间到达的 user message 须排在指令之后）。
3. **Given** planner 完成一次**正常**复盘（未触发压缩），**When** 复盘结束，**Then** planner 按 prompt"必要时才调用"**可选**地经指令发送工具向 player 发送一条策略指令；若发送，该指令在 player 通道（`playerMessages`）的消息顺序上紧跟游戏结束的 tool_result 之后、player 下一条消息输出之前（可见顺序：`tool_calling → tool_result → planner 指令 → player message output`），且 planner 的复盘过程对 player 不可见（在 `plannerMessages`）；graph 路由回 player 继续，是否自动开下一局由 player LLM 决定、框架不干扰。若 planner 未发送指令，graph 亦路由回 player 继续（无新指令）。
4. **Given** planner 完成一次**触发压缩**的复盘，**When** 压缩完成，**Then** planner 经 prompt 引导产出一次**无游戏历史**的策略指令（player 指令历史已被压缩清理；LLM 决定是否调用），team turn 随即结束（player 停下），该发送不触发 player invoke，指令与 player 的下一次激活（通常由 user message 触发）一同注入（避免压缩后字段进入下一轮，与 037"压缩后自动停下"一致）。
5. **Given** player 的系统提示词与工具面，**When** 审查，**Then** player 不再以"当前态势"注入策略，也不持有任何记忆/策略读取工具；planner 指令作为 player 对话流的一部分（可累积、可引用）。
6. **Given** 多局游戏，**When** 跨局观察，**Then** planner→player 的近期指令随局数累积（player 持续接收新指令），而 planner 自身的长期认知演化存放于 planner 内部长期记忆（US2），两者分工清晰。

---

### Edge Cases

- **批量操作中途游戏结束**：`saolei_operate` 的有序列表中若某个操作使游戏以 won/lost 结束，该操作生效、之前已成功操作生效，工具在该处停止，单次结果反映结束时的棋盘与 won/lost 状态；后续未执行的操作不执行。
- **批量操作中途校验拒绝（按原因细分，澄清 Session 2026-08-07）**：无害空操作拒绝（操作因目标格状态成为不改变棋盘的空操作，如在数字格上 click/flag）→ 跳过该操作、继续剩余；结构性/上下文拒绝（越界、无活动局）→ 在该处停止、剩余不执行。已成功操作生效；单次结果反映处理后的最终棋盘与（若有）跳过/停止原因。
- **空操作列表**：`saolei_operate` 收到空操作列表时的行为（视为无操作返回当前状态 / 视为非法）由 plan 决定；本 spec 仅约束不产生任何落子副作用。
- **指令产生时 player 正在运行**：planner 产生指令发生在复盘后（player 的 createAgent 已因 game-end 停止）。正常复盘情形指令在同一 turn 立即注入、player 继续（是否开下一局由 LLM 决定）；压缩情形 turn 结束、player 停下，指令等待下次激活。team 初始化触发 planner 产指令时无运行中 player，指令随首次激活注入。
- **压缩与指令的时序**：当复盘触发压缩时，顺序为"planner 复盘 → 压缩（含 planner 记忆快照刷新）→ planner 发送指令 → turn 结束（player 停下）"；压缩失败的处理沿用 037 既有语义（致命错误 abort，不降级），指令在压缩成功完成后发送。
- **memory_id 冲突**：`memory_add` 使用 LLM 提供的 `memory_id`；若该 id 已存在，MUST 拒绝（返回已存在错误），由 LLM 改用 `memory_update`（add 与 update 语义区分，见 Assumptions）。
- **记忆条目上限**：planner 长期记忆为有界集合（参考 hermes 有界快照）；达到上限时的处理（报错让 LLM 自行 consolidate，调研 §3.2.2）由 plan 决定具体上限值与策略。
- **memory 服务不可用**：memory 服务短暂不可用时（记忆工具调用失败、或压缩边界刷新快照失败），MUST NOT 中断 player 游戏或使整局崩溃；planner 的记忆操作降级（工具错误反馈给 LLM、快照保持上一次冻结值），team 继续运行。具体降级机制（重试/退避）由 plan 决定，本 spec 约束"不因 memory 不可用而阻断 gameplay"。
- **session 删除与记忆清理**：session 删除时其作用域下的 memory 条目是否级联清理，留待 plan 决定（与 031 对 strategy 的"暂不清理"决策对齐，后续优化）。
- **player 不再读策略后的旧数据**：本特性为破坏性重构（同 031 clean break 惯例），不考虑既有 `strategies` 集合数据的兼容与迁移；开发/测试环境重建。

## Requirements *(mandatory)*

### Functional Requirements

#### saolei 批量落子操作

- **FR-001**: saolei MCP MUST 将原 `saolei_click`/`saolei_flag`/`saolei_chord_click` 三个落子工具合并为单个 `saolei_operate` 工具。`saolei_operate` MUST 接收一个有序操作列表，列表中每个操作声明其类型（click/flag/chord 之一）与坐标 `(x, y)`；MUST 按列表顺序依次对该操作进行既有严格校验并执行，保留操作顺序。
- **FR-002**: `saolei_operate` MUST 按列表顺序依次对每个操作进行既有严格校验并执行，并对一次批量调用只返回一次结果（单一 MCP 文本内容块，反映处理后的最终棋盘与游戏状态）。批量内的失败处理按拒绝原因**细分**（澄清 Session 2026-08-07）：
  - **游戏结束**：某操作使游戏以 won/lost 结束 → 在该操作处停止，游戏结束后剩余操作不执行；
  - **无害空操作拒绝**：操作因目标格当前状态而成为**不会改变棋盘**的空操作（例如对已揭示数字格 click/flag、对已标记格 click、对已揭示格 flag、对非数字格 chord、对无未揭示邻居的数字格 chord）→ **跳过该操作**（不执行、不改变棋盘），继续执行剩余操作（不中断批量）；
  - **结构性/上下文拒绝**：如越界（`out_of_bounds`）、无活动局（`no_active_game`）→ 在该操作处停止，剩余操作不执行。
  已成功执行的操作生效；单次结果反映上述处理后的最终棋盘、游戏状态，以及（若有）停止/跳过原因。具体每条拒绝原因码到上述分类的映射依据 saolei MCP 既有 `validateMove` 拒绝码（`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `MoveRejection`），由 plan 落实。
- **FR-003**: `saolei_init`（开局）与 `saolei_remain`（只读查询）MUST 保持为独立工具不变，MUST NOT 并入 `saolei_operate`。
- **FR-004**: 同步给 planner 的游戏历史（gameLog）MUST 以 `saolei_operate` 为单位记录——一次批量调用记录为一条含其全部操作列表的历史项；MUST NOT 拆分为多条单步记录。
- **FR-005**: 注入 planner 的工具描述 MUST 说明 `saolei_operate` 工具及其支持的全部操作类型（click/flag/chord），且 MUST 与游戏历史中记录的 `saolei_operate` 操作类型一一对应（使 planner 能据历史判断 player 对工具的使用）。

#### planner 长期记忆与 memory 服务

- **FR-006**: 系统 MUST 新建一个 memory 服务（grpc-go），承载 planner 的长期记忆条目；记忆条目 MUST 持久化（跨进程重启可读）。memory 服务 MUST 使用与 agent/prompt 相同的 mongo **实例**作为存储，但按 `style/mongo.md` 要求 MUST 使用自己**独立的数据库**（MUST NOT 与 agent 的 `game_prompt` 库或 prompt 服务的库混用）。
- **FR-007**: 系统 MUST 在 agent 服务上新建 memory mcp server，向 planner 暴露 `memory_add`/`memory_update`/`memory_remove` 三个记忆工具；该 mcp server MUST 经 agent 服务将记忆请求转发到 memory 服务，MUST NOT 由 mcp server 直接连接到 memory 服务。
- **FR-008**: `memory_add`/`memory_update`/`memory_remove` 三个工具 MUST 均包含 `memory_id` 参数；其余参数（如记忆内容 content）参考 hermes 既有记忆工具的 add/replace/remove 语义。`memory_update`/`memory_remove` MUST 以 `memory_id` 定位目标条目。`memory_add` 遇已存在的 `memory_id` MUST 拒绝（add 与 update 为独立工具、语义须区分；见 Assumptions）。
- **FR-009**: 仅 `planner` 持有并调用 `memory_add`/`memory_update`/`memory_remove`；`player` MUST NOT 持有任何记忆工具，MUST NOT 持有任何长期存储。
- **FR-010**: planner 的长期记忆 MUST 以**冻结快照**方式注入 planner 系统提示词（一次烘焙、跨多次复盘保持冻结，调研 D2/D5）；记忆快照的刷新边界为压缩节点（调研 D4，同 037 每 5 局压缩）。MUST NOT 在 planner 每次激活时重读并刷新该快照（冻结语义）。
- **FR-011**: 注入 planner 系统提示词的记忆快照中，每条记忆 MUST 以 `memory_id: 内容` 形式呈现，使 LLM 能据 `memory_id` 调用 `memory_update`/`memory_remove`。
- **FR-012**: memory 资源 MUST 以会话为作用域，资源 pattern MUST 为 `templates/{template}/sessions/{session}/memories/{memory}`；template 与 session MUST 经独立 mcp server 的 path 闭包注入（同 saolei mcp 既有做法），MUST NOT 硬编码于工具参数。

#### 共享策略记忆废弃与 planner→player 校准指令

- **FR-013**: 系统 MUST 移除 player/planner 共享的 `StrategyStore`（其 get/put 契约、`strategies` 集合访问、`update_strategy` 工具、player"当前态势"注入、planner system 注入策略等全部路径）。player MUST NOT 持有任何长期存储，MUST NOT 读取任何策略。
- **FR-014**: planner MUST 持有一个**指令发送工具**（功能类似原 `update_strategy`，但将策略指令投递进 player 的对话流而非写入共享存储；作为 player 对话流的一部分，可累积、可引用，调研 D6 HumanMessage 模式）。在**正常游戏结束复盘**场景下，planner **MAY** 经该工具向 player 发送一条策略指令——**由 planner 决定是否需要发送，非每次游戏结束都必须发送**（澄清 Session 2026-08-07）。该场景携带游戏历史。
- **FR-015**: team 初始化时 MUST **异步**触发一次**无游戏历史**的策略指令产出（触发点随 [`specs/040-team-singleton-conformance/`](../../040-team-singleton-conformance/) supersede 迁移至 `UpdateTeam(allow_missing=true)` 物化路径（graph 首建）——物化后即返回、不等 LLM；initInstruction 在后台以 prompt **要求** planner 给 player 指令的方式运行，LLM 最终决定是否调用 `instruct_player`）；此时无运行中的 player、无游戏记录。**该触发点 MUST 仅在 graph 首建（物化）时生效；profile 变更引发的 graph 重建（040 FR-005）MUST NOT 重跑 initInstruction。** 该指令 MUST 随 player 的首次激活一同注入（不立即激活 player）；异步产出期间到达的 user message MUST 排在该指令之后。此为独立于正常复盘的场景（两场景拆分，澄清 Session 2026-08-07）。
- **FR-016**: 当一次复盘触发压缩时，MUST 在压缩完成后以 prompt 引导产出一次**无游戏历史**的策略指令——因压缩已清理 player 的指令历史，需重新建立引导（LLM 最终决定是否调用 `instruct_player`）；该情形下 team turn MUST 结束（player 停下、等待下一次消息），指令 MUST 与 player 的下一次激活（通常由 user message 触发）一同注入——避免压缩后字段进入下一轮，与 037"压缩后自动停下"一致。压缩成功是产出该指令与结束 turn 的前提（压缩失败沿用 037 致命 abort 语义）。此为独立于正常复盘的场景（两场景拆分，澄清 Session 2026-08-07）。
- **FR-017**: 正常复盘（未触发压缩）**若 planner 决定发送**策略指令（FR-014，可选），该指令 MUST 在同一 team turn 内注入 player 的消息通道（`playerMessages`），且在消息顺序上**紧跟游戏结束的 tool_result 之后、player 生成下一条消息输出之前**——即 player 通道的可见顺序为 `player tool_calling → tool_result（游戏结束）→ planner 指令 → player 下一条 message output → …`。planner 的复盘过程发生在 planner 自身通道（`plannerMessages`），**对 player 不可见**（不进入 `playerMessages`）。player 对游戏结束 tool_result 的回应（即其下一条 message output）发生在指令注入之后的下一次 player 激活。graph 随后路由回 player 继续；**是否自动进行下一局由 player LLM 决定，框架 MUST NOT 干扰**（保留 031 FR-009 多局能力）。仅压缩后（FR-016）与 team 初始化（FR-015）的指令采用"与下一次激活一同注入"的延迟投递。

#### 大型测试（验收）

- **FR-018**: 本特性 MUST 提供大型测试（large test，经 testplan skill 执行），覆盖关键行为，至少包含：`saolei_operate` 批量执行与单次返回（含无害空操作跳过 / 结构性拒绝停止）；planner 长期记忆经 memory 服务持久化与冻结快照注入（含 `memory_id`）；planner 指令发送工具（正常复盘 prompt"必要时才调用" + 消息顺序、team 初始化/压缩后 prompt 引导无历史产出且不触发 player invoke）；player 不再读取策略；memory 服务使用独立数据库；ListMemories 分页。验收标准为所有测试用例全部通过（宪法原则 VI）。
- **FR-019**: 正常游戏结束复盘（FR-014，携带游戏历史、prompt"必要时才调用"）与 team 初始化/压缩后（FR-015/FR-016，无游戏历史、prompt 引导产出）MUST 作为两个不同的场景处理。两场景的 team-graph node 逻辑 SHOULD 拆分实现（正常复盘节点 = 复盘 + 可选指令；init/compact 节点 = 无历史 prompt 引导产出）。init/compact 的指令产出 MUST NOT 触发 player invoke。两场景产出指令与否最终均由 LLM 决定（无强制检验）。具体 node 拆分与路由由 plan 落实。

### Key Entities *(include if feature involves data)*

- **Memory（planner 长期记忆条目）**：planner 自身的长期记忆，以会话为作用域（`templates/{template}/sessions/{session}/memories/{memory}`），由新建的 memory 服务（grpc-go）承载并持久化。经 memory mcp（部署在 agent 服务上，转发到 memory 服务）暴露的 `memory_add`/`memory_update`/`memory_remove` 工具增删改（均含 `memory_id`）。仅 planner 持有；以冻结快照注入 planner 系统提示词（每条 `memory_id: 内容`），压缩边界刷新。取代原共享 Strategy（FR-013）。
- **Calibration Instruction（planner→player 校准指令）**：planner 经指令发送工具向 player 投递的策略指令，作为 player 对话流的一部分（累积、可引用）。分两场景（均由 LLM 决定是否调用工具，无强制检验）：**正常复盘**——prompt"必要时才调用"（可选，携带游戏历史），若发送则同 turn 内立即注入（紧跟游戏结束 tool_result，player 继续、是否开下一局由 LLM 决定）；**team 初始化 / 压缩后**——prompt **要求**给 player 指令、产出**无游戏历史**指令（player 指令历史已被清理），不触发 player invoke（压缩后 turn 结束、与下次激活一同注入；初始化异步产出随首次激活注入，期间 user message 排在指令之后）。取代原 player"当前态势"策略注入（FR-013/FR-014/FR-015/FR-016/FR-017/FR-019）。
- **Instruction-sending tool（指令发送工具）**：planner 持有的工具（功能类似原 `update_strategy`，但将指令投递进 player 对话流而非写入共享存储）。两场景均由 planner LLM 决定是否调用（正常复盘 prompt"必要时才调用"；init/compact prompt 要求给指令）。仅 planner 持有；player 不持有。
- **saolei_operate（批量落子操作）**：合并原 `saolei_click`/`saolei_flag`/`saolei_chord_click` 的单一落子工具，接收有序操作列表，按序校验执行，单次返回最终棋盘与状态。`saolei_init`/`saolei_remain` 不变。
- **memory 服务（grpc-go）**：新建服务，承载 planner 长期记忆条目的持久化与读写；agent 服务经转发访问，mcp server 不直连。
- **memory mcp server（agent）**：新建于 agent 服务的 mcp server，向 planner 暴露记忆工具，由 agent 转发请求到 memory 服务；template/session 经 path 闭包注入。
- **被移除的实体**：Strategy（共享策略长期记忆）、StrategyStore（`get`/`put` 接口 + `MongoStrategyStore`）、`update_strategy` 工具、player"当前态势"注入路径、planner system 策略注入路径（clean break，FR-013）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: player 批量落子时，一次 `saolei_operate` 调用即可完成多个有序操作并只收到一次结果（相较原先每个落子一次往返，批量场景的调用往返显著减少）。
- **SC-002**: planner 的长期记忆经独立 memory 服务持久化，跨进程重启可读；planner 持有且仅持有 `memory_add`/`memory_update`/`memory_remove`（均含 `memory_id`），并能据此逐条增删改自身记忆。
- **SC-003**: planner 系统提示词中的长期记忆以冻结快照呈现（每条 `memory_id: 内容`），跨多次复盘保持冻结，仅在压缩边界刷新。
- **SC-004**: player 不再读取任何策略/长期记忆；planner 持有指令发送工具——正常复盘按 prompt"必要时才调用"（若发送则同 turn 内紧跟游戏结束 tool_result 注入、player 继续），team 初始化与压缩后经 prompt 引导产出无游戏历史指令（LLM 决定是否调用）、且不触发 player invoke（压缩后 turn 结束、与下次激活一同注入；初始化异步产出随首次激活注入、期间 user message 排在指令之后）；不产生仅因指令而触发的独立 player 激活。
- **SC-005**: 共享 `StrategyStore` 及其全部访问路径（get/put、`update_strategy`、player 当前态势、planner system 注入）被完全移除（代码审查可验证无残留引用）。
- **SC-006**: memory mcp server 部署在 agent 服务上并由 agent 转发请求到 memory 服务，mcp server 不直连 memory 服务（连接拓扑可验证）。
- **SC-007**: 同步给 planner 的游戏历史以 `saolei_operate`（含全部操作列表）为单位记录，注入 planner 的工具描述与历史中的操作类型一一对应。
- **SC-008**: 大型测试（经 testplan skill 完整部署→测试→清理执行）全部用例通过，覆盖 SC-001/SC-002/SC-003/SC-004 所述行为。

## Assumptions

- **破坏性重构（clean break）**：本特性对 031 的核心记忆契约（共享 StrategyStore、FR-013/014/015、super turn 流程）采取破坏性变更，**不考虑历史数据兼容与迁移**（既有 `strategies` 集合数据不迁移）；开发/测试环境重建。参考本仓库 clean break 惯例（`specs/023-saolei-mcp-refine`、`specs/031-team-template-mode` Assumptions）。产出形态为独立 spec 目录（`specs/039-planner-memory-calibration`），而非 031 目录内增量（调研 `survey/planner-memory-and-agent-communication.md` §7.1 已倾向独立目录）。
- **memory 资源 pattern 集合段用复数 `memories`**：与用户提问"memory 有复数形式吗？"对应——"memory"作为可数资源条目时复数为 "memories"；本仓库 AIP-122 资源 pattern 一致采用"复数集合 / 单数 id"（`messages/{message}`、`profiles/{profile}`），故采用 `memories/{memory}`。需求方描述中写作 `memory/{memory}`，此处按仓库约定统一为复数集合段。
- **`memory_add` 的 memory_id 由调用方提供，冲突时拒绝**：需求方明确要求 `memory_add` MUST 包含 `memory_id` 参数，故新增条目的 id 由 planner LLM 提供（而非服务端自动生成并返回）。`memory_add` 与 `memory_update` 为两个独立工具，语义须区分（add=新建、update=改既有），故 `memory_add` 遇已存在的 `memory_id` MUST 拒绝（返回已存在错误），由 LLM 改用 `memory_update`；这避免了 add 与 update 语义重叠。
- **冻结快照刷新边界复用 037 压缩节点**：planner 长期记忆快照的刷新边界为 037 已落地的每 5 局压缩节点（调研 D4），本特性不新增独立刷新机制；可借鉴 hermes retain-vs-rebuild 优化（调研 §4.2），是否落地留待 plan。
- **指令投递的存储机制与两种情形**：planner→player 指令投递分两种情形（澄清 Session 2026-08-07）：正常复盘指令在同一 turn 立即注入（紧跟最后一个 tool result），压缩后/初始化指令与下次激活一同注入（turn 结束）。具体存储（pending 指令槽 vs 直接写入 player 通道）属实现细节，由 plan 依据调研 D6（HumanMessage 进入 player 通道）与压缩作用域决定；本 spec 约束两种情形的行为结果。正常复盘保留 031 FR-009 多局能力（是否开下一局由 player LLM 决定、框架不干扰）。
- **批量操作中途失败的处理（已定调，澄清 Session 2026-08-07）**：`saolei_operate` 按拒绝原因细分——无害空操作（不改变棋盘，如在数字格上 click/flag）跳过并继续剩余；游戏结束或结构性/上下文错误（越界、无活动局）在该处停止、剩余不执行；空操作列表行为（无操作返回当前状态 / 非法）仍留 plan 决定。每条拒绝原因码到分类的映射依据 saolei MCP 既有 `MoveRejection` 码。
- **记忆条目上限与 session 删除清理**：planner 长期记忆为有界集合，上限值与达上限策略（参考 hermes 报错让 LLM 自行 consolidate）留待 plan；session 删除时 memory 条目是否级联清理留待 plan（与 031 对 strategy"暂不清理"对齐，后续优化）。
- **player 模型/工具装配不变**：除落子工具合并为 `saolei_operate`、移除策略注入外，player 的模型绑定、base 提示词取自 `SaoleiProfile.player_prompt`（FR-034 不变）、saolei skill body 追加等既有装配不受本特性影响。
- **参考资料**：前期调研见 `survey/planner-memory-and-agent-communication.md`（hermes 冻结快照/记忆工具/压缩刷新、openclaw HumanMessage 投递、LangGraph 通道）。相关现有契约：`specs/031-team-template-mode/contracts/strategy-store-contract.md`（待废弃）、`specs/031-team-template-mode/contracts/team-graph-contract.md`（player/planner 节点、策略/记忆流）、`specs/031-team-template-mode/contracts/saolei-sink-contract.md`（游戏事件 sink）。相关现有代码：`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（落子工具、`SaoleiEventSink`）、`projects/game/agent/src/team/{player,planner}.ts`、`projects/game/agent/src/strategy-store.ts`（待废弃）、`projects/game/agent/src/mcp-host.ts`（mcp server path 闭包注入既有做法）、`projects/game/agent/src/server.ts`（服务装配）。
