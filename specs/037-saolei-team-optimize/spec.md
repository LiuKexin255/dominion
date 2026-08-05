# Feature Specification: saolei Team 模板优化（上下文压缩 / 消息上限 / 工具信息注入 / 实时可见性修复 / 游戏统计）

**Feature Branch**: `037-saolei-team-optimize`

**Created**: 2026-08-05

**Status**: Draft

**Input**: User description: "规划的 team 模式已经实现并验证正常工作。现在希望对 saolei template 做出优化：(1) 每经过 5 局游戏，触发 player 和 planner 的历史上下文压缩；player 压缩后可自然停下等待用户输入；压缩内容为 agent message，desktop 上应可见（需确认无需改动）；压缩触发节点为游戏结束且 planner 返回后、继续执行 player 之前。(2) desktop session 对话页面增加显示消息数量上限，超出按先进先出队列移除旧消息；压缩时 desktop 无需显式清理旧数据，新消息滚动更新即可。(3) 为 planner 系统提示词注入 player 工具信息（仅工具描述，不注入工具本身），以便 planner 判断 player 是否充分利用工具。另外修复 bug：desktop 展示 planner 游戏历史 message，实时页面看不到但重新进入 session 后可以看到。(4) saolei MCP 在 game end 事件中增加游戏统计数据（操作次数 x、正确标记地雷数 y、每雷平均操作数 x/y 保留两位小数），并将统计数据加入给 planner 更新策略的 message 当中。"

## Clarifications

### Session 2026-08-05（需求确认与本特性边界）

本特性为 `specs/031-team-template-mode`（Team Template Mode）已实现并验证行为的**优化与缺陷修复**。所基于的现有实现契约见 `specs/031-team-template-mode/contracts/team-graph-contract.md`（saolei team StateGraph）与 `specs/031-team-template-mode/contracts/desktop-contract.md`（desktop 多标签页）。相关缺陷的根因记录见 `specs/031-team-template-mode/bug-analysis.md` Issue 2 的"复盘输入可见性"分析。以下记录关键决策，`plan.md` 阶段不再重新讨论：

- **压缩触发的计数口径 → 每局结束（won/lost）计一局，planner 处理后累加**：游戏计数器在每次游戏结束且 planner 复盘返回后递增 1（won 与 lost 均计数；未结束的 player 步骤不计）。当计数达到 5 的倍数（5、10、15…）时触发压缩。计数器为 per-session、in-process（与 ephemeral buffer 同生命周期，随 team 重建重置）。
- **压缩语义 → 整体替换为单条摘要 agent message**：压缩将 player / planner 各自通道（`playerMessages` / `plannerMessages`）的**全部**短期消息替换为**一条**摘要 agent message（AIMessage）。即压缩后该通道仅保留摘要一条消息（策略等长期记忆不受影响，与 `specs/031-team-template-mode/spec.md` FR-018 的"短期/长期解耦"一致）。采用整体替换而非滑动窗口，因其最简洁且与"player 压缩后停下、下局以摘要重建上下文"的语义契合。
- **压缩后 player 行为 → 自然停下、等待用户输入**：触发压缩后，player 在该 turn 内**不再继续**（不开新局），turn 结束（路由到 END）。下一次用户输入开启新 turn 时，player 以压缩后的摘要上下文重建并开始下一局。
- **压缩失败处理 → 直接 abort 连接、终止 loop（需求方澄清）**：压缩 LLM 调用失败时，系统**直接 abort 连接并终止 loop**（不降级、不重试、不静默吞错）——压缩失败视为致命错误。中断后的恢复/重连/状态一致性等统一中断场景处理**不在本特性范围内**，后续统一处理中断等特殊场景。
- **压缩摘要的实时可见性 → 复用 US1 的帧发射机制，desktop 无需额外改动**：经分析（见 US1 根因与 Assumptions），压缩摘要是一条写入通道的 agent message。其**重载时**（ListMessages）天然可见；其**实时可见性**取决于是否经帧发射推送，与 US1（planner 游戏历史实时可见性）共享同一根因与同一修复机制。因此 US1 建立的"非模型产出的通道消息实时发射为帧"的机制一经实现，压缩摘要即可实时可见——**desktop 侧无需为压缩可见性额外改动**（用户要求的确认项，结论为"无需额外改动，前提是 US1 机制已落地"）。
- **desktop 消息上限作用域 → 每个 agent tab 独立计数**：上限按 agent tab（player / planner 各自的对话桶）独立计算，超出时按先进先出移除该 tab 内最旧的消息。每个 tab 是独立的对话视图，独立计数符合其语义。
- **planner 工具信息注入 → 仅静态描述，不注入工具、不在 planner 工具集中出现**：planner 的系统提示词中追加一段 player 可用工具的**名称与描述**清单（静态，在 team 构建时一次性计算——工具集由模板固定装配，`specs/031-team-template-mode/spec.md` FR-028）。planner 的**实际工具集**仍仅 `update_strategy`（`specs/031-team-template-mode/spec.md` FR-012 不变）；planner 不能调用 player 工具，仅可"阅读"其描述以判断 player 是否充分利用。
- **Bug 根因（planner 游戏历史实时不可见）→ streamEvents 仅产出模型/工具事件，不产出 createAgent 的输入 HumanMessage**：planner 的复盘输入（携带完整 `gameLog` 的 HumanMessage）作为 createAgent 的**输入**注入，不产生 `messages`/`tools` 协议事件（`projects/game/agent/src/session-team.ts` `runTeamTurn` 仅订阅 `messages` 的 `content-block-finish` 与 `tools` 的 `tool-started`/`tool-finished`）。因此该复盘输入在**实时流**中不可见；但它在通道写入后经 ListMessages（重载）可见——这与用户报告的"实时看不到、重新进入看得到"一致。修复方向：在 planner 节点开始时将该复盘输入内容作为一帧实时发射到 planner tab（`specs/031-team-template-mode/bug-analysis.md` Issue 2 "复盘输入可见性"已指出此方向）。
- **游戏统计数据的计算口径与数据源 → MCP 第一手计算，基于识别状态 + 操作计数**：saolei MCP 在 game end 事件（`SaoleiEventSink.onGameEnd`）中增加三项游戏统计数据，均由 MCP 内部第一手计算（符合 `specs/031-team-template-mode/spec.md` FR-017"信号来自 MCP 内部第一手计算"的原则，不解析 tool result 文本）：
  - **操作次数 x** = 本局**成功的格子操作**次数（`saolei_click`/`saolei_flag`/`saolei_chord_click` 经校验通过并成功识别的执行次数）；**不计** `saolei_init`（开新局）、`saolei_remain`（只读查询）、被校验拒绝的落子、以及 LLM 调用工具次数（一次 LLM 调用可能未产生有效操作）。即 x = 本局触发了 `onMove` 回调的次数（`onMove` 仅在成功识别格子操作后触发，`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` registerCellTool 内 `runSink("onMove", …)`）。
  - **正确标记地雷数量 y** = 本局中被 player 正确标记（flag）的地雷数。该值可由识别状态第一手推导：终局时未标记的地雷以 `MINE`/`HIT_MINE` 显现（`projects/game/pkg/saolei-board/src/core/types.ts` CellStatus），故 y = 总地雷数 − 终局 `MINE` 格数 − `HIT_MINE` 格数；总地雷数取自开局识别状态的 mineCounter（开局时 flags=0，counter 值 = 总地雷数，`projects/game/pkg/saolei-board/src/core/counter.ts`）。胜利时 `MINE`/`HIT_MINE` 均为 0，y = 总地雷数（全部地雷被正确标记）。误标（flag 在非地雷格上）不计入 y。
  - **每雷平均操作数 x/y** = x ÷ y，保留两位小数。当 y = 0（无一地雷被正确标记，如开局即踩雷）时为除零情形，MUST 优雅处理（不崩溃，具体表示如 "N/A" 由 `plan.md` 决定，见 Assumptions）。
- **游戏统计数据的流转路径 → MCP onGameEnd → ephemeral buffer → planner 复盘输入**：统计数据经 `onGameEnd` 事件携带（扩展 `SaoleiEventSink.onGameEnd` 的参数），team sink 将其写入 ephemeral buffer（随 gameEvent 一并存储），planner 的复盘输入（`buildReviewInput`，`projects/game/agent/src/team/planner.ts`）渲染时将统计数据纳入复盘 message，使 planner 据此评估 player 的操作效率与标记准确性以更新策略。统计数据是**游戏概念**（非 team/strategy/store 概念），扩展 sink 接口不违背 `specs/031-team-template-mode/spec.md` FR-019（接口仅描述事件形状，不耦合 team mode）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - planner 游戏历史消息实时可见 (Priority: P1)

当一局扫雷游戏结束、planner 被触发进行复盘时，planner 收到的复盘输入（包含完整游戏过程的游戏历史消息）应当在 desktop 的 planner 标签页中**实时**显示，而不是仅在用户重新进入 session（经历史加载）后才出现。当前实现中，该复盘输入消息作为 planner 的 createAgent 输入注入，不产生实时流事件，导致实时页面看不到但重新进入后可以看到。

**Why this priority**: 这是一个**正确性缺陷**，直接影响 team 协作的可观测性。用户无法在 planner 复盘发生时实时看到其复盘依据（游戏历史），只能事后重载才能看到，破坏了多 agent 协作的实时观察体验。同时，US1 建立的"非模型产出的通道消息实时发射为帧"机制是 US2（上下文压缩摘要实时可见）的**前提**——压缩摘要同样是非模型产出的通道消息，复用同一机制。因此 US1 是本特性的基础，优先级最高。

**Independent Test**: 可通过 fake-model + fake-tool 的集成测试独立验证：构造一个产生游戏结束的 fake tool（sink 写入 gameLog 与结束事件），触发 planner 复盘；验证在 planner 节点运行期间，复盘输入的内容被作为一帧发射（携带 `agent="planner"`），且其内容包含完整游戏过程（每步操作的工具名、坐标、状态与棋盘）。在 desktop 层可独立验证：收到该帧后，planner tab 实时出现该游戏历史消息（无需重载）。

**Acceptance Scenarios**:

1. **Given** 一局游戏结束（won 或 lost），**When** planner 被触发进行复盘，**Then** planner 的复盘输入（游戏历史消息）在 desktop 的 planner 标签页中**实时**显示（无需重新进入 session）。
2. **Given** planner 的复盘输入携带完整游戏过程（多步操作），**When** 该消息实时显示，**Then** 其内容包含每一步操作的工具名称、坐标与操作后的游戏状态（与重载后 ListMessages 显示的内容一致）。
3. **Given** planner 实时显示的复盘输入消息，**When** 用户重新进入 session（重载历史），**Then** 该消息仍然存在（实时发射与历史加载不重复、不丢失，二者内容一致）。
4. **Given** 一个没有发生任何游戏操作的会话（gameLog 为空），**When** planner 被触发，**Then** planner tab 实时显示"无可用游戏记录"的说明性消息（`specs/036-team-mode-bugfix/spec.md` FR-009 的实时对应）。
5. **Given** 实时发射的复盘输入帧，**When** 审查其归属，**Then** 该帧携带 `agent="planner"`，被归入 planner 标签页（不串入 player 标签页）。

---

### User Story 2 - 每 5 局触发 player/planner 历史上下文压缩 (Priority: P1)

在持续多局游戏过程中，player 与 planner 的短期消息历史会不断累积（每局产生大量落子、工具结果与复盘消息），导致上下文过度膨胀。系统应在**每经过 5 局游戏**时，触发 player 与 planner 各自历史上下文的压缩：将各自通道的全部短期消息压缩为一条摘要 agent message。压缩在**游戏结束且 planner 复盘返回后、继续执行 player 之前**触发；压缩后 player 自然停下（不开新局），等待用户输入开启下一局。压缩内容为 agent message，在 desktop 上可见。

**Why this priority**: 这是本次优化的核心价值——控制持续游戏下的上下文增长，避免 token 膨胀导致成本上升与模型上下文溢出。它直接依赖 US1 的帧发射机制实现压缩摘要的实时可见性（压缩摘要同样是非模型产出的通道消息）。与 US1 同为 P1，二者构成"实时可见性 + 上下文治理"的基础闭环。

**Independent Test**: 可通过 fake-model + fake-tool 的集成测试独立验证：构造连续 5 局游戏结束的流程（每局 planner 触发一次），验证第 5 局 planner 返回后、player 不继续（turn 结束、路由到 END），且 `playerMessages` 与 `plannerMessages` 各被替换为一条摘要 agent message（通道长度收缩为 1）；验证策略（StrategyStore）不受压缩影响；验证第 6 局（用户输入后）player 以摘要上下文重建。

**Acceptance Scenarios**:

1. **Given** 一个会话已连续完成 4 局游戏（每局 planner 复盘一次），**When** 第 5 局游戏结束且 planner 复盘返回，**Then** 触发历史上下文压缩（player 与 planner 各自通道被压缩）。
2. **Given** 触发压缩，**When** 压缩执行，**Then** player 通道（`playerMessages`）的全部短期消息被替换为**一条**摘要 agent message，planner 通道（`plannerMessages`）的全部短期消息被替换为**一条**摘要 agent message。
3. **Given** 压缩完成，**When** 该 turn 结束，**Then** player **不再继续**（不开新局），turn 路由到 END，等待用户输入。
4. **Given** 压缩后用户发送新输入，**When** 新 turn 开始，**Then** player 以压缩后的摘要上下文（通道中仅摘要一条消息）重建，并开始下一局游戏。
5. **Given** 压缩触发，**When** 压缩执行，**Then** 策略（长期记忆，StrategyStore）**不受影响**（压缩仅作用于短期消息通道，与 `specs/031-team-template-mode/spec.md` FR-013/FR-018 的"短期/长期解耦"一致）。
6. **Given** 压缩产生的摘要 agent message，**When** 该消息写入通道，**Then** 其在 desktop 上**实时可见**（复用 US1 帧发射机制），并在重载（ListMessages）后仍可见。
7. **Given** 会话继续完成第 6–10 局游戏，**When** 第 10 局游戏结束且 planner 复盘返回，**Then** 再次触发压缩（计数器达到 10 = 5 的倍数）。
8. **Given** 压缩后执行 `RefreshTeam`（`specs/031-team-template-mode/spec.md` FR-018），**When** 刷新执行，**Then** 摘要消息与其他短期消息一并被清空，策略保留（RefreshTeam 语义不变）。
9. **Given** 第 5 局之前（计数 < 5），**When** 任一局游戏结束且 planner 返回，**Then** **不触发**压缩，player 正常继续（planner → player 边不变）。

---

### User Story 3 - planner 系统提示词注入 player 工具描述 (Priority: P2)

planner 的系统提示词中应包含 player 可用工具的**描述信息**（工具名称与描述），以便 planner 在复盘时判断 player 是否充分利用了所提供的工具。**注意**：注入的仅为工具的描述（静态文本），而非为 planner 注入 player 的工具本身——planner 的实际工具集仍仅 `update_strategy`，planner 不能调用 player 工具。

**Why this priority**: 这是一项增强 planner 复盘判断质量的优化，不阻塞核心流程（planner 已能正常复盘）。它使 planner 能评估 player 的工具使用充分性（例如是否合理使用标记、是否查询剩余雷数），从而产出更高质量的策略更新。优先级 P2，可在 US1/US2 落地后独立交付。

**Independent Test**: 可通过单元测试独立验证：构建 team graph 时捕获 planner 的 createAgent `systemPrompt`，验证其中包含每个 player 工具的名称与描述文本；验证 planner 的**工具集**仍仅 `update_strategy`（未被注入 player 工具）。

**Acceptance Scenarios**:

1. **Given** saolei 模板的 team 构建完成，**When** 审查 planner 的系统提示词，**Then** 其中包含一段 player 可用工具的描述清单，列出每个工具的名称与描述。
2. **Given** planner 的系统提示词中的工具描述清单，**When** 审查其内容，**Then** 包含 player 持有的全部 saolei MCP 工具（如 `saolei_init`/`saolei_click`/`saolei_flag`/`saolei_remain` 等）的描述。
3. **Given** planner 的实际工具集，**When** 审查其工具，**Then** 仍仅包含 `update_strategy`（FR-012 不变）——player 工具**未被注入**为 planner 可调用工具，仅其描述出现在系统提示词中。
4. **Given** player 工具集由模板固定装配（`specs/031-team-template-mode/spec.md` FR-028，不读 profile），**When** team 构建，**Then** 注入 planner 提示词的工具描述基于模板固定装配的工具集计算（与 profile 无关）。
5. **Given** planner 复盘 player 的游戏表现，**When** planner 评估策略，**Then** planner 可参考工具描述判断 player 是否充分利用了可用工具（如是否在适当时机标记可疑格子）。

---

### User Story 4 - desktop 对话消息显示数量上限（先进先出） (Priority: P2)

desktop 的 session 对话页面应为每个 agent 标签页的显示消息设置数量上限。当某 agent tab 的消息数量超出上限时，按先进先出（FIFO）队列移除最旧的消息，仅保留最新的若干条。当上下文压缩（US2）发生时，desktop 无需显式清理旧数据——压缩产生的摘要消息作为新消息到来后，旧的压缩前消息会随 FIFO 自然滚动移除。

**Why this priority**: 这是一项前端内存与渲染治理优化，防止持续游戏下消息无限累积导致 DOM 膨胀与卡顿。它与 US2 的压缩形成互补：压缩治理后端 LLM 上下文，FIFO 上限治理前端显示内存。优先级 P2，可在 US1/US2/US3 落地后独立交付，本身不依赖后端改动。

**Independent Test**: 可通过前端组件测试独立验证：向某 agent tab 注入超过上限数量的消息，验证超出后最旧消息被移除、仅保留上限数量的最新消息；验证不同 agent tab 的计数相互独立。

**Acceptance Scenarios**:

1. **Given** desktop 某 agent 标签页已显示达到上限数量的消息，**When** 一条新消息到达，**Then** 最旧的一条消息被移除（FIFO），显示数量保持在上限以内。
2. **Given** 多个 agent 标签页（player / planner），**When** player tab 的消息数量超出上限，**Then** 仅 player tab 移除其最旧消息，planner tab 不受影响（各 tab 独立计数）。
3. **Given** 上下文压缩发生（US2），**When** 压缩摘要消息作为新消息到达某 agent tab，**Then** desktop 无需显式清理旧的压缩前消息——摘要消息进入后，旧消息随后续 FIFO 自然滚动移除（不因压缩而清空本地已显示的历史）。
4. **Given** 历史加载（ListMessages）返回的消息数量超过上限，**When** 消息被加载到某 agent tab，**Then** 仅保留最新的上限数量条，超出部分的最旧消息被丢弃。
5. **Given** 实时流消息与历史加载消息混合，**When** tab 内消息总数超出上限，**Then** FIFO 统一生效（不区分消息来源，统一按到达顺序淘汰最旧）。

---

### User Story 5 - saolei MCP game end 事件增加游戏统计数据并注入 planner 复盘 (Priority: P2)

saolei MCP 在游戏结束（game end）事件中应增加本局的游戏统计数据，包括：**操作次数 x**（本局执行的成功格子操作次数，不含 init、不含只读查询、不含被拒落子、不含 LLM 调用次数）、**正确标记地雷数量 y**（本局被 player 正确 flag 的地雷数）、以及**每雷平均操作数 x/y**（保留两位小数）。这些统计数据应被纳入给 planner 更新策略的复盘 message 当中，使 planner 能据此评估 player 的操作效率与标记准确性。

**Why this priority**: 这是一项增强 planner 复盘质量的优化——量化统计数据（操作次数、标记准确率、每雷操作效率）让 planner 能基于客观数据而非仅凭棋盘过程判断 player 表现，从而产出更高质量的策略更新。它不阻塞核心流程（planner 已能正常复盘），优先级 P2，可在 US1–US4 落地后独立交付。统计数据由 MCP 第一手计算（符合 FR-017 原则），经 game end 事件 → ephemeral buffer → planner 复盘输入流转。

**Independent Test**: 可通过单元/集成测试独立验证：(a) 构造一局已知操作序列与已知地雷布局的 fake 游戏，验证 MCP 在 game end 事件中携带的操作次数、正确标记数、每雷平均操作数与预期一致；(b) 验证 planner 复盘输入（buildReviewInput 渲染）包含这三项统计数据；(c) 验证 y=0 的除零情形被优雅处理。

**Acceptance Scenarios**:

1. **Given** 一局游戏中 player 执行了若干次成功的格子操作（click/flag/chord），**When** 游戏结束触发 onGameEnd，**Then** 事件携带的操作次数 x 等于本局成功格子操作的次数（不含 init、不含 saolei_remain、不含被校验拒绝的落子）。
2. **Given** 一局游戏中 player 正确标记了若干地雷，**When** 游戏结束，**Then** 事件携带的正确标记地雷数量 y 等本局被正确 flag 的地雷数（误标在非地雷格上的 flag 不计入 y）。
3. **Given** 操作次数 x 与正确标记数 y，**When** 计算每雷平均操作数，**Then** 其值 = x ÷ y 且保留两位小数。
4. **Given** 一局游戏以 won 结束（全部地雷被正确标记），**When** 游戏结束，**Then** y = 总地雷数（无 MINE/HIT_MINE 格），x/y 反映通关总操作效率。
5. **Given** 一局游戏以 lost 结束（部分地雷未被标记），**When** 游戏结束，**Then** y = 总地雷数 − 终局 MINE 格数 − HIT_MINE 格数（即未被踩中且未被揭示的地雷 = 已正确标记的地雷）。
6. **Given** 一局游戏 y = 0（无一地雷被正确标记，如开局即踩雷），**When** 计算每雷平均操作数，**Then** 除零情形被优雅处理（不崩溃，以明确的"不可计算"语义表示，不产生 NaN/Infinity）。
7. **Given** 游戏统计数据，**When** planner 被触发复盘，**Then** planner 的复盘输入 message 中包含操作次数、正确标记地雷数量、每雷平均操作数三项统计数据（使 planner 据此评估 player 表现）。
8. **Given** onGameEnd 事件携带统计数据，**When** 审查 sink 接口，**Then** 统计数据作为游戏概念（非 team/strategy/store 概念）携带，不违背 FR-019（接口仅描述事件形状，不耦合 team mode）。
9. **Given** 游戏统计数据纳入复盘输入，**When** 该复盘输入实时发射（US1）与重载（ListMessages），**Then** 统计数据在两种路径下均可见且一致。

---

### Edge Cases

- **压缩 LLM 调用失败 → 直接 abort**：压缩摘要由 LLM 生成（与 player/planner 复用各自模型的合理默认见 Assumptions）。若压缩 LLM 调用失败，系统**直接 abort 连接、终止 loop**（FR-013，不降级、不重试）。中断后的恢复/重连/状态一致性等统一中断场景处理不在本特性范围内（后续统一处理）。
- **压缩与游戏计数的并发**：游戏计数器为 per-session、in-process，由 team turn 串行驱动（单飞 TurnLoop，`specs/031-team-template-mode/contracts/team-graph-contract.md` §6），不存在跨 turn 的并发竞态。
- **压缩时恰好无短期消息**：若某通道在压缩触发时为空（例如 planner 通道因异常无复盘消息），压缩该通道应是无害的空操作（不产生空摘要消息、不崩溃）。
- **压缩后立即 RefreshTeam**：压缩已将通道替换为摘要；RefreshTeam 随后清空该摘要（FR-018 清全部短期消息）。策略保留。语义自洽，无冲突。
- **desktop 消息上限与重载一致性**：重载（ListMessages）后，desktop 重新填充某 tab 时可能收到超过上限的消息——仅保留最新上限条。这可能导致重载后比实时流期间看到的消息更少（实时期间累积的旧消息被重载时的上限截断），这是预期行为（上限的目的即限制显示量）。
- **player 工具描述变更**：注入 planner 提示词的工具描述基于模板固定装配的工具集（FR-028）。若未来模板工具集变化，描述随之变化；当前工具集固定，描述在 team 构建时一次性计算（静态）。
- **planner 复盘输入实时发射与历史去重**：US1 实时发射的复盘输入帧与重载时 ListMessages 返回的同一条消息（相同 messageId/frameId）MUST 去重，不重复显示（与现有 `renderedMessageIds` 去重机制一致，`projects/game/desktop/frontend/src/App.svelte`）。
- **压缩摘要消息的桌面归属**：压缩摘要是 agent message，实时发射时携带所属 agent（player 摘要 → player tab；planner 摘要 → planner tab），归入对应标签页。
- **游戏统计 y=0 除零**：当本局无一地雷被正确标记（如开局首点即踩雷），每雷平均操作数 x/y 除零。系统 MUST 优雅处理（不崩溃、不产生 NaN/Infinity），以明确的"不可计算"语义表示；具体表示形式（如 "N/A"）由 `plan.md` 决定。
- **开局 mineCounter 不可解码**：正确标记数 y 的推导依赖开局识别状态的 mineCounter（取总地雷数）。若开局 counter 不可解码（`{ decoded: false }` 或 `undefined`，`projects/game/pkg/saolei-board/src/core/types.ts` MineCounter），总地雷数不可知，y 无法精确推导。系统 MUST 降级处理（不崩溃；具体降级如标记 y 为不可用或采用替代推导，由 `plan.md` 决定）。
- **被拒落子与只读查询不计入操作次数**：操作次数 x 仅计**成功的格子操作**。被校验拒绝的落子（validateMove 返回 ok:false，`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`）未执行游戏操作，不计；`saolei_remain`（只读查询）与 `saolei_init`（开新局）不计。
- **游戏统计数据与压缩的交互**：统计数据随 gameEvent 存于 ephemeral buffer（per-game，每局 onGameStart 时随 gameLog 重置），不进入短期消息通道、不受压缩（FR-008）与 RefreshTeam（FR-018）影响——压缩/刷新作用于短期消息，统计数据是 per-game 的瞬时游戏概念。

## Requirements *(mandatory)*

### Functional Requirements

#### planner 游戏历史消息实时可见（Bug 修复）

- **FR-001**: 当 planner 节点被触发进行复盘时，其复盘输入（携带完整游戏历史的消息）MUST 在 planner 节点开始执行时，作为一帧实时发射到实时流（携带 `agent="planner"`），使 desktop 的 planner 标签页无需重载即可实时显示该游戏历史消息。
- **FR-002**: 实时发射的复盘输入帧内容 MUST 与重载（ListMessages）时该消息显示的内容一致（均包含完整游戏过程：每步操作的工具名称、坐标、状态与棋盘）。
- **FR-003**: 实时发射的复盘输入帧 MUST 与重载时的同一条消息（相同 messageId/frameId）去重，不在 desktop 重复显示。
- **FR-004**: 当 gameLog 为空时，planner 的"无可用游戏记录"说明性复盘请求（`specs/036-team-mode-bugfix/spec.md` FR-009）MUST 同样实时发射显示。
- **FR-005**: 实时发射机制 MUST 可复用于其他"非模型产出的通道消息"（如 US2 的压缩摘要），即该机制是通用的"通道消息→实时帧"发射，而非仅针对复盘输入的硬编码。

#### 每 5 局触发历史上下文压缩

- **FR-006**: 系统 MUST 维护一个 per-session、in-process 的游戏计数器，在每次游戏结束（won 或 lost）且 planner 复盘返回后递增 1。当计数达到 5 的倍数（5、10、15…）时，MUST 触发历史上下文压缩；计数非 5 的倍数时 MUST NOT 触发。
- **FR-007**: 压缩触发节点 MUST 为：游戏结束且 planner 复盘返回后、继续执行 player 之前（即当前 planner → player 边的位置插入压缩判定）。
- **FR-008**: 压缩 MUST 同时作用于 player 通道（`playerMessages`）与 planner 通道（`plannerMessages`）：将各通道的全部短期消息替换为一条摘要 agent message（AIMessage）。
- **FR-009**: 压缩 MUST NOT 影响策略（长期记忆，StrategyStore）——压缩仅作用于短期消息通道，与 `specs/031-team-template-mode/spec.md` FR-013/FR-018 的"短期/长期解耦"一致。
- **FR-010**: 压缩完成后，player MUST 在该 turn 内不再继续（不开新局），turn 路由到 END，等待用户输入。下一次用户输入开启新 turn 时，player MUST 以压缩后的摘要上下文（通道中仅摘要一条消息）重建并开始下一局。
- **FR-011**: 压缩产生的摘要 agent message MUST 在 desktop 上实时可见（复用 FR-005 的帧发射机制），并在重载（ListMessages）后仍可见。
- **FR-012**: 压缩摘要 MUST 是一条有意义的 agent message（概括已发生的游戏与复盘要点，使 player/planner 能据此继续），MUST NOT 为空消息或无意义占位。
- **FR-013**: 当压缩 LLM 调用失败时，系统 MUST 直接 abort 连接并终止 loop（不降级、不重试、不静默吞错）。即压缩失败视为致命错误，立即中断当前 turn 与连接。中断后的恢复/重连等统一中断场景处理不在本特性范围内（后续统一处理）。
- **FR-014**: 压缩后执行 `RefreshTeam`（FR-018）时，摘要消息与其他短期消息 MUST 一并被清空，策略保留（RefreshTeam 语义不变）。
- **FR-015**: 若某通道在压缩触发时为空，压缩该通道 MUST 为无害空操作（不产生空摘要消息、不崩溃）。

#### planner 系统提示词注入 player 工具描述

- **FR-016**: planner 的系统提示词 MUST 包含一段 player 可用工具的描述清单，列出 player 持有的每个工具的名称与描述。
- **FR-017**: 注入的工具描述 MUST 基于 player 工具集（saolei MCP 工具，由模板固定装配，`specs/031-team-template-mode/spec.md` FR-028）在 team 构建时一次性计算（静态），MUST NOT 读取 profile 中的工具配置。
- **FR-018**: planner 的实际工具集 MUST 仍仅 `update_strategy`（`specs/031-team-template-mode/spec.md` FR-012 不变）——player 工具 MUST NOT 被注入为 planner 可调用工具，仅其描述出现在 planner 系统提示词中。
- **FR-019**: 注入的工具描述 MUST NOT 改变 player 的工具集或行为（仅影响 planner 的系统提示词内容）。

#### desktop 对话消息显示数量上限（FIFO）

- **FR-020**: desktop 的 session 对话页面 MUST 为每个 agent 标签页的显示消息设置数量上限（按 agent tab 独立计数）。
- **FR-021**: 当某 agent tab 的消息数量超出上限时，MUST 按先进先出（FIFO）移除该 tab 内最旧的消息，仅保留最新的上限数量条。
- **FR-022**: 不同 agent 标签页的消息计数 MUST 相互独立——某 tab 超出上限仅移除该 tab 的最旧消息，不影响其他 tab。
- **FR-023**: 上下文压缩（FR-008）发生时，desktop MUST NOT 显式清理旧的压缩前消息——压缩摘要作为新消息到来后，旧消息随后续 FIFO 自然滚动移除（不因压缩而清空本地已显示的历史）。
- **FR-024**: 历史加载（ListMessages）返回的消息数量超过上限时，MUST 仅保留最新的上限数量条，超出部分的最旧消息被丢弃。
- **FR-025**: FIFO 上限 MUST 统一生效于实时流消息与历史加载消息（不区分消息来源，统一按到达顺序淘汰最旧）。

#### saolei MCP game end 游戏统计数据

- **FR-026**: saolei MCP 在游戏结束（game end）事件中 MUST 计算并携带三项游戏统计数据：操作次数（operationCount）、正确标记地雷数（correctFlags）、每雷平均操作数（avgOpsPerMine）。统计数据 MUST 由 MCP 内部第一手计算（符合 `specs/031-team-template-mode/spec.md` FR-017），MUST NOT 解析 tool result 文本。
- **FR-027**: 操作次数（operationCount）MUST 等于本局**成功的格子操作**次数——即 `saolei_click`/`saolei_flag`/`saolei_chord_click` 经校验通过并成功识别的执行次数。MUST NOT 计入 `saolei_init`（开新局）、`saolei_remain`（只读查询）、被校验拒绝的落子、或 LLM 调用工具次数（一次 LLM 调用可能未产生有效操作）。操作次数计数器 MUST 在每局开始（onGameStart）时重置为本局起点。
- **FR-028**: 正确标记地雷数（correctFlags）MUST 等于本局被 player 正确 flag 的地雷数（误标在非地雷格上的 flag 不计入）。该值 MUST 可由识别状态第一手推导：correctFlags = 总地雷数 − 终局 `MINE` 格数 − `HIT_MINE` 格数；总地雷数取自开局识别状态的 mineCounter（开局 flags=0 时 counter 值 = 总地雷数）。
- **FR-029**: 每雷平均操作数（avgOpsPerMine）MUST = operationCount ÷ correctFlags，保留两位小数。当 correctFlags = 0 时，MUST 优雅处理除零（不崩溃、不产生 NaN/Infinity，以明确的"不可计算"语义表示）。
- **FR-030**: `SaoleiEventSink.onGameEnd` 事件 MUST 携带游戏统计数据（扩展 onGameEnd 的参数）。统计数据为游戏概念（非 team/strategy/store 概念），扩展 sink 接口 MUST NOT 引入对 team mode 的耦合（`specs/031-team-template-mode/spec.md` FR-019 不变）。
- **FR-031**: team sink（`projects/game/agent/src/team/team-sink.ts`）MUST 将 onGameEnd 携带的统计数据写入 ephemeral buffer（随 gameEvent 一并存储），供 planner 复盘读取。
- **FR-032**: planner 的复盘输入（`buildReviewInput`，`projects/game/agent/src/team/planner.ts`）MUST 包含本局的游戏统计数据（操作次数、正确标记地雷数、每雷平均操作数），使 planner 据此评估 player 的操作效率与标记准确性以更新策略。
- **FR-033**: 当开局 mineCounter 不可解码（总地雷数不可知）导致 correctFlags 无法精确推导时，系统 MUST 降级处理（不崩溃；统计数据以不可用语义表示或采用替代推导，由 `plan.md` 决定）。

#### 大型测试（验收）

- **FR-034**: 本特性 MUST 提供大型测试（large test，经 testplan skill 执行），覆盖：planner 游戏历史消息实时可见、每 5 局触发压缩（player 停下、通道收缩为摘要、策略保留）、planner 工具描述注入提示词、game end 游戏统计数据（操作次数/正确标记数/每雷平均操作数）计算正确并纳入 planner 复盘。验收标准为所有测试用例全部通过（宪法原则 VI）。

### Key Entities *(include if feature involves data)*

- **游戏计数器（Game Counter）**：per-session、in-process 的整数计数器，记录已完成（且 planner 复盘过）的游戏局数。每次游戏结束且 planner 返回后递增；达到 5 的倍数时触发压缩。生命周期与 ephemeral buffer 一致（随 team 重建重置）。非持久化。
- **压缩摘要消息（Compression Summary Message）**：压缩产生的一条 agent message（AIMessage），概括某通道（player/planner）在压缩前的全部短期消息要点。压缩后该通道仅保留此一条消息。为短期消息（受 `RefreshTeam` 清空），区别于策略（长期记忆）。
- **通道消息实时帧（Channel-Message Live Frame）**：由 US1 建立的通用机制产物——将"非模型产出的通道消息"（如 planner 复盘输入、压缩摘要）在写入通道的同时，作为一帧实时发射到对应 agent 的标签页。区别于 createAgent 内部 loop 产生的 `messages`/`tools` 协议事件（那些天然经 streamEvents 实时发射）。
- **游戏统计数据（Game Stats）**：每局游戏结束时由 saolei MCP 第一手计算的量化指标，包括操作次数（operationCount，本局成功格子操作数）、正确标记地雷数（correctFlags）、每雷平均操作数（avgOpsPerMine，保留两位小数）。为 per-game 的瞬时游戏概念，随 gameEvent 存于 ephemeral buffer（每局 onGameStart 时重置），不进入短期消息通道、不受压缩与 RefreshTeam 影响。经 onGameEnd 事件 → buffer → planner 复盘输入流转，供 planner 评估 player 操作效率与标记准确性。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: planner 被触发复盘时，其游戏历史消息在 desktop 的 planner 标签页**实时**显示（无需重新进入 session），且与重载后显示的内容一致。
- **SC-002**: 会话连续完成 5 局游戏后，第 5 局 planner 复盘返回时触发压缩，player 与 planner 各自通道收缩为一条摘要 agent message，player 停下等待用户输入；策略不受影响。
- **SC-003**: 压缩后用户输入开启的新局中，player 以摘要上下文正常继续游戏（无上下文断裂、无丢失策略）。
- **SC-004**: 压缩摘要消息在 desktop 实时可见（复用 SC-001 的机制），重载后仍可见。
- **SC-005**: planner 的系统提示词包含 player 工具描述清单，但 planner 工具集仍仅 `update_strategy`（player 工具未被注入为可调用工具）。
- **SC-006**: desktop 各 agent 标签页显示消息受数量上限约束，超出时最旧消息按 FIFO 移除；压缩时无需显式清理旧消息（自然滚动）。
- **SC-007**: 大型测试（经 testplan skill 完整部署→测试→清理执行）全部用例通过，覆盖 SC-001/SC-002/SC-003/SC-005/SC-008（宪法原则 VI）。
- **SC-008**: saolei MCP 在游戏结束时正确计算游戏统计数据（操作次数 = 成功格子操作数、正确标记地雷数 = 总地雷数 − 终局 MINE/HIT_MINE 格数、每雷平均操作数 = x/y 保留两位小数），且该数据被纳入 planner 复盘 message（planner 据此评估 player 操作效率与标记准确性）。

## Assumptions

- **本特性为 031-spec 已实现行为的优化与缺陷修复，非架构重构**：所基于的现有实现契约见 `specs/031-team-template-mode/contracts/team-graph-contract.md`（team graph）、`specs/031-team-template-mode/contracts/desktop-contract.md`（desktop）、`specs/031-team-template-mode/contracts/strategy-store-contract.md`（策略存储）。Bug 根因见 `specs/031-team-template-mode/bug-analysis.md` Issue 2"复盘输入可见性"。`plan.md` 阶段直接基于这些契约与根因设计实现方案。
- **压缩语义为整体替换（非滑动窗口）**：压缩将通道全部短期消息替换为一条摘要 agent message（需求方描述"压缩"的自然语义，且与"player 压缩后停下、下局以摘要重建上下文"契合）。是否保留最近 N 条原始消息（滑动窗口）为 `plan.md` 可细化项，但本 spec 默认整体替换。
- **压缩摘要由 LLM 生成，复用各自 agent 的模型**：player 通道摘要用 player 模型、planner 通道摘要用 planner 模型（合理默认——各自模型最了解各自上下文）。具体模型选择与摘要提示词由 `plan.md` 设计。
- **压缩 LLM 失败 → 直接 abort（需求方澄清）**：压缩失败不降级、不重试，直接 abort 连接并终止 loop（FR-013）。中断后的恢复/重连/状态一致性等统一中断场景处理不在本特性范围内——后续统一处理中断等特殊场景。
- **desktop 消息上限默认值为合理常量**：上限的具体数值未由需求方指定，本特性取一个合理默认值（如每 agent tab 200 条，由 `plan.md`/实现确定具体数值并作为命名常量）。该数值是 UX 细节，有合理默认，不构成阻塞。
- **游戏计数器计 won 与 lost**：游戏结束无论输赢均计一局（won/lost 都触发 planner，都是完整一局）。未结束的 player 步骤不计。
- **player 工具描述注入为静态文本**：工具描述在 team 构建时一次性计算（工具集由模板固定装配，FR-028），作为静态文本段追加到 planner 系统提示词。不引入运行时动态查询。
- **US1（实时帧发射）是 US2（压缩摘要实时可见）的前提**：压缩摘要与 planner 复盘输入同属"非模型产出的通道消息"，复用同一帧发射机制。US1 落地后，US2 的压缩摘要实时可见无需额外 desktop 改动（用户确认项的结论）。
- **现有测试基础设施可复用**：031/036-spec 的测试基础设施（fake-model + fake-tool DI 模式，见 `projects/game/agent/src/team/graph.test.ts`）可直接复用于压缩、工具描述注入与实时帧发射的测试。
- **desktop FIFO 上限为纯前端改动**：消息上限在 `projects/game/desktop/frontend/src/App.svelte` 的 `chatMessages` 状态管理中实现（`handleMessageParts`、`loadAgentHistories`、warn 处理等追加点），不依赖后端改动。
- **游戏统计 correctFlags 推导已确认可第一手计算**：终局时未标记地雷以 `MINE`/`HIT_MINE` 显现（`projects/game/pkg/saolei-board/src/core/types.ts` CellStatus + `win.ts` NON_WIN_CELLS），故 correctFlags = 总地雷数 − 终局 MINE 格数 − HIT_MINE 格数；总地雷数取自开局识别状态 mineCounter（开局 flags=0，counter 值 = 总地雷数，`projects/game/pkg/saolei-board/src/core/counter.ts`）。胜利局 MINE/HIT_MINE 均为 0，correctFlags = 总地雷数。误标（flag 在非地雷格）不计入（其非地雷，不影响地雷计数等式）。该推导由 MCP 内部完成（第一手），具体实现（如是否复用现有计数器/分类辅助）由 `plan.md` 设计。
- **游戏统计 y=0 与 counter 不可解码的降级留待 `plan.md`**：本 spec 约束"不崩溃、以明确不可用语义表示"（FR-029/FR-033）；具体表示形式（如 "N/A"）与 counter 不可解码时的替代推导由 `plan.md` 决定。
- **操作次数计 onMove 触发次数**：`onMove` 仅在格子操作经校验通过并成功识别后触发（`saolei-mcp.ts` registerCellTool 内），init/remain/被拒落子均不触发 onMove，故 operationCount = 本局 onMove 触发次数，口径与需求一致。
- **参考资料**：team graph 契约 `specs/031-team-template-mode/contracts/team-graph-contract.md`；desktop 契约 `specs/031-team-template-mode/contracts/desktop-contract.md`；saolei sink 契约 `specs/031-team-template-mode/contracts/saolei-sink-contract.md`；bug 根因 `specs/031-team-template-mode/bug-analysis.md` Issue 2；现有代码 `projects/game/agent/src/team/{graph,player,planner,team-sink,state}.ts`、`projects/game/agent/src/{session-team,handler}.ts`、`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`、`projects/game/pkg/saolei-board/src/core/{types,win,counter}.ts`、`projects/game/desktop/frontend/src/{App.svelte,components/ChatView.svelte}`。
