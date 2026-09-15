# Feature Specification: agent_v2 team 优化——扫雷系统终局播报、记忆快照近因注入与广播格式提示词澄清

**Feature Branch**: `065-agent-v2-team-refine`

**Created**: 2026-09-15

**Status**: Draft

**Input**: User description: "对 @projects/game/agent_v2/ 进行优化 1. 增加一个 saolei-loop 增加一个 '扫雷系统'角色成员，其会在游戏结束（胜利或失败）时发送一条消息，作为本局游戏的概述。包括：a. 游戏结果 b. 游戏执行操作数量（只单个操作数量，不是 operate 调用数量） c. 每种操作(click/flag/chord)执行数量。saolei-loop 循环变成 player -> 游戏结束 -> saolei 系统 member 发送本局游戏统计 -> planner。这个不影响排队消息逻辑：游戏统计的发送排在 player turn 结束后，如果排队消息继续驱动 turn 新开一局、跳过了这一局，那就等下一局终态时再输出下一局统计。2. 在向 system prompt 注入长期历史时，按更新时间倒排注入最近 10 条。3. 补充下 team 的提示词，补充下 agent 不用自己输出 <角色-message> 的格式。我看有 agent 输出中自己模仿这个格式。"

## Motivation

agent_v2 team 模式（059 建立、060/062 增量优化）当前的三块差距：

| 维度 | 现状 | 目标 |
|---|---|---|
| 终局信息呈现 | planner 复盘输入只有 player 的终局工具单元广播（`<player-tool-call>` 全文）——原始棋盘文本，无结构化对局概述；用户在团队视图也看不到一局的汇总 | 终局交接时由"扫雷系统"角色成员发送一条本局统计消息（结果 + 操作总数 + 分项数），作为 planner 复盘与用户概览的共同锚点 |
| 长期记忆注入 | planner 记忆快照全量注入 system prompt（`renderMemorySnapshot` 渲染全部条目、服务序即 memory_id 升序，`common/js/dsh-plugins/memory-service/src/snapshot.ts`）；条目增多后 token 无界增长且旧条目稀释近因 | 按条目更新时间倒排，仅注入最近更新的 10 条 |
| 广播格式提示词 | team section 解释了收到的广播如何被包装（`<角色-message>`/`<角色-tool-call>`），但未说明这是**仅输入侧**的呈现形态；实际观察到 agent 在自己的输出中模仿这些标签格式 | team section 明确成员自身输出不使用（不需要）这些标签格式 |

现状锚点：终局交接路径由 062 建立（终局工具结果收束 player 回合 → idle 锚点 → `nextStep()` 评估：排队消化优先 → gameEnded 复盘 → 结构性续驱，`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`）；本 feature 在该路径的复盘交接处插入扫雷系统的统计播报，不改变收束与优先序机制。

## Clarifications

### Session 2026-09-15

- Q: 游戏结束时扫雷系统发出的统计消息，应该由哪些成员作为群聊输入消费——只有 planner，还是 player 和 planner 都消费？ → A: 全体真实成员消费（Option A）：planner 复盘输入含该消息；player 下次结构性驱动输入同批含该消息，其成员视图记录该广播。
- Q: 在 webUI 团队视图中，扫雷系统的统计消息应该按普通成员消息样式渲染，还是要有独立的系统公告样式？ → A: 普通成员消息样式（Option A）：复用既有成员消息渲染与发送者标签，前端零/最小改动，无独立系统公告样式。
- Q: 扫雷系统角色在团队消息流中使用的稳定 role 标签应该是什么？ → A: `saolei`（Option A）：广播标签 `<saolei-message>`，roster 摘要以中文"扫雷系统"职责描述。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 扫雷系统成员在终局交接时播报本局统计 (Priority: P1) 🎯 MVP

saolei team 增加"扫雷系统"角色成员——一个不持有 preset/model、不由 LLM 驱动的系统角色。一局游戏终局（胜利或失败）、player 回合收束后、planner 复盘驱动前，该角色发送一条团队消息作为本局游戏的概述，内容至少包含：a) 游戏结果（胜/负）；b) 本局执行的单个操作总数（按成功执行的格子操作计，不是 `saolei_operate` 调用次数——一次批量调用按其中实际执行的单个操作数计入）；c) 每种操作类型（click/flag/chord）的执行数量。循环变为：player →（游戏终局、player 回合收束）→ 扫雷系统发送本局统计 → planner 复盘。统计消息进入团队归并序列（webUI 团队视图以"扫雷系统"角色标注可见），并经既有团队广播机制进入 planner 的复盘驱动输入。

**Why this priority**: 本 feature 的核心增量——结构化的对局概述同时服务 planner 复盘质量（结构化统计 vs 原始棋盘文本）与用户可读性（团队视图一局一报），并建立"扫雷系统"这一系统角色的位置。

**Independent Test**: 大型测试（fake-llm + fake-desktop，多局 won/lost 链路）：每局交接后，团队归并序列中恰有一条以扫雷系统角色为发送者的统计消息，正文含游戏结果、与该局实际派发一致的操作总数与 click/flag/chord 分项数（对照 fake-desktop 收到的派发序列）；planner 复盘 turn 的模型输入含该消息。

**Acceptance Scenarios**:

1. **Given** player 回合内某次 operate 致终局（won/lost）、回合收束且无排队消息，**When** 终局交接评估触发 planner 复盘，**Then** 交接中扫雷系统角色发送一条本局统计消息（含结果/总数/分项数），planner 复盘 turn 的输入包含该消息（与其未消费的 player 广播同批进入）。
2. **Given** 一次批量 operate 内多个操作成功执行，**When** 终局统计播报，**Then** 操作总数与分项数按实际成功执行的单个操作计（批量调用不按 1 计）。
3. **Given** 统计消息发送，**When** webUI 团队视图消费，**Then** 该消息按既有成员消息样式渲染、以扫雷系统角色标注呈现（与 user/player/planner 并存的发送者标签，无独立系统公告样式），ListTeamMessages 回填与 team_message 实时帧同源同值。
4. **Given** 复盘驱动失败后重试（既有 pendingReview 保持语义），**When** 重试驱动发生，**Then** 扫雷系统统计消息不重复发送、不重复进入历史（每局至多一条）。

---

### User Story 2 - 排队消息优先序不被统计播报干扰 (Priority: P1)

统计播报不改变既有排队消息逻辑：终局后若编排 FIFO 有排队用户消息，player 仍以新回合优先消化（既有 case 1 优先序），统计消息的发送随终局交接顺延；若排队消息驱动 player 开新局、本局交接被跳过（该局终局记录被新局终局记录覆盖——既有"仅交接最新终局记录"语义），则被跳过的局不再补发统计，下一次统计在下一局终局交接时输出。

**Why this priority**: 用户裁定的不变量——统计播报是交接路径的插入物，不得抢占或改写排队消化优先序；该优先序是 059/061/062 共同收敛的行为面。

**Independent Test**: 大型测试：终局 player 回合收束时注入排队用户消息（fake-llm 驱动 player 消化并 init 新局、新局致终局），断言被跳过的局在团队历史中无统计消息、新局交接时恰有一条新局统计消息；对照用例（终局后无排队消息）统计即时播报。

**Acceptance Scenarios**:

1. **Given** 终局 player 回合收束且 FIFO 有排队消息，**When** pump 评估，**Then** 排队消息优先由 player 以新回合消化（统计播报不先于、不阻断消化）。
2. **Given** 排队消化驱动 player 开新局且新局终局，**When** 新局交接触发，**Then** 播报的是新局的统计（被覆盖的旧局不补发）。
3. **Given** 排队消化后 player 静止且原终局记录未被覆盖，**When** 交接评估触发，**Then** 原局统计照常播报（顺延而非丢弃）。

---

### User Story 3 - planner 长期记忆快照按更新时间倒排注入最近 10 条 (Priority: P2)

planner 物化时预取的长期记忆快照注入 system prompt 时，按条目更新时间倒序排列，且仅注入最近更新的 10 条：最常被 planner 近期修订/新增的记忆排最前，更早的旧记忆被截断在快照之外。不足 10 条时全量注入；无记忆时快照不渲染（既有语义）。快照的冻结时机不变（物化时预取、实例生命周期内固定、运行中写入待下次物化生效）。

**Why this priority**: 直接约束 planner 上下文的 token 规模并提升近因记忆的信噪比；独立于 US1/US4，可单独交付验证。

**Independent Test**: 单测：构造超过 10 条、更新时间互异的记忆条目，断言快照仅含最近更新的 10 条且按更新时间倒序；构造不足 10 条与 0 条的对照组断言全量与不渲染。

**Acceptance Scenarios**:

1. **Given** 记忆 scope 下有 15 条条目（更新时间互异），**When** planner 物化预取并注入 system prompt，**Then** 快照恰含更新时间最近的 10 条，最旧 5 条不出现，顺序为最新在前。
2. **Given** 记忆 scope 下有 3 条条目，**When** 快照注入，**Then** 3 条全量注入（按更新时间倒序）。
3. **Given** 条目更新时间存在并列，**When** 快照排序，**Then** 排序结果确定（并列以稳定次序打破，不因请求/渲染时点漂移）。

---

### User Story 4 - team 提示词澄清：成员自身输出不使用广播标签格式 (Priority: P2)

team section（全员共享的团队提示词）补充说明：`<角色-message>`/`<角色-tool-call>` 等标签对只是**系统呈现其他成员输出**的包装形态，成员自己的输出**不需要也不应该**使用这些标签格式（直接输出正文与工具调用即可）。消除实际观察到的 agent 输出自我模仿问题。

**Why this priority**: 低成本修正实际观察到的模型行为偏差（agent 在自己的输出中模仿广播标签），防止自我包装污染广播链路（自我包装的正文会在他人视角被二次包装）。

**Independent Test**: 单测断言 team section 文本包含"成员自身输出不使用广播标签格式"的明确表述；大型测试既有链路回归通过（fake-llm 输出不含自我包装标签时链路不受影响）。

**Acceptance Scenarios**:

1. **Given** 任意成员（player/planner）读到 team section，**When** 其产生自己的输出，**Then** 提示词明确告知不使用 `<角色-message>`/`<角色-tool-call>` 等格式自我包装（正文直接输出、工具调用走工具协议）。
2. **Given** 提示词更新，**When** 成员间广播照常发生，**Then** 广播渲染语义零变化（接收方看到的包装由系统生成，非发送者输出的一部分）。

---

### Edge Cases

- **init 即终局的棋盘**（062 既有边界）：`saolei_init` 不写终局记录（无复盘）→ 无统计播报；链路静止于 player 激活的既有语义不变。
- **对已复盘终局棋盘的结构性拒绝**（`game_won`/`game_over` stop）：不重写终局记录、不重复复盘（既有 `reviewedGameEvent` 去重）→ 不产生新的统计播报。
- **取消（Cancel）与终局交接竞态**：终局后取消暂停自动续驱，统计随交接顺延——之后用户再 Send 恢复且交接评估仍命中未复盘终局记录时播报；若先刷新（UpdateTeam）则作废重建，未交接局的统计不再发送。
- **刷新（UpdateTeam）**：在途与未交接的终局统计随 team 作废清空（新 team 从零计数/播报）。
- **统计口径边界**：被跳过的无害 no-op（SKIP 集）与结构性拒绝（STOP 集）操作不计入执行数；桌面派发失败（isError）的操作不计入；init（F2 重开）与 remain 查询不是格子操作、不计入。
- **快照更新时间并列**：以稳定次序键打破并列（确定性排序，具体键由 plan 决定）。
- **快照截断与 memory 工具并存**：截断只影响 system prompt 注入面，memory 工具的写路径与 `old_text` 定位语义零变化（工具始终面向全量存储操作）。
- **系统角色不出现在 proto 成员面**：`UpdateTeam` 校验（members 恰 2、role 集合恰为 {player, planner}）、`GetTeam.members`、`active_member` 均不包含扫雷系统角色——它是团队消息流中的发送者，不是物化成员。

## Requirements *(mandatory)*

### Functional Requirements

**扫雷系统终局播报（核心）**

- **FR-001**: saolei team MUST 增加一个"扫雷系统"系统角色成员：不持有 preset/model、不经 LLM 驱动、不进入 proto `TeamMember` 成员列表与 `active_member`（saolei 场景的 UpdateTeam 两成员校验零变化）。其唯一行为是按 FR-002/FR-003 在终局交接时发送本局统计消息，消息以稳定 role 标签 **`saolei`**（区别于 `player`/`planner`，不与 `user` 保留值冲突；广播标签形态 `<saolei-message>`）为发送者落地团队消息流。
- **FR-002**: 统计消息正文 MUST 呈现：a) 游戏结果（胜/负）；b) 本局成功执行的**单个**格子操作总数（不是 `saolei_operate` 调用次数——批量调用按其中实际成功执行的操作数计入）；c) click/flag/chord 各自的成功执行数量（分项之和 = 总数）。统计口径沿用游戏运行时既有"成功派发"定义：SKIP（无害 no-op）与 STOP（结构性拒绝）操作、桌面派发失败的操作、init 与 remain 调用一律不计入。
- **FR-003**: 统计消息的触发时机 MUST 绑定终局交接路径：player 回合收束后、planner 复盘驱动前发送，循环为 player →（终局、回合收束）→ 扫雷系统播报 → planner 复盘。排队消息优先序 MUST 零变化：终局后 FIFO 有排队消息时先由 player 消化（播报随交接顺延）；排队消化驱动 player 开新局致本局交接被跳过（终局记录被覆盖）时，该局 MUST NOT 补发统计，下一次播报在下一局终局交接时输出。统计消息使 planner 的复盘输入必非空——存在未复盘终局记录时复盘驱动 MUST 启动（不落入"无输入则续驱 player"的分支）。
- **FR-004**: 统计消息 MUST 以团队消息形态落地：进入团队归并序列（`ListTeamMessages` 可见、`team_message` 帧实时扇出、webUI 团队视图以发送者角色标注渲染），并经既有团队广播/消费机制进入**全体真实成员**的驱动输入——planner 的复盘驱动输入（与其未消费的 player 广播同批）与 player 的下一次结构性驱动输入（其成员视图记录该广播，呈现为来源扫雷系统角色的注入消息）；每个被交接的局至多一条，复盘驱动失败重试 MUST NOT 重复发送或重复入历史。
- **FR-005**: 游戏运行时 MUST 按局维护各操作类型（click/flag/chord）的成功执行计数：init 重开时随局面清零、随终局记录携带——统计消息的唯一数据源（运行时不渲染消息正文，消息由扫雷系统角色侧生成）。
- **FR-006**: 成员提示词 MUST 使成员知晓扫雷系统角色（如 team section roster 增加该角色的一行说明），使 planner/player 能理解其消息的来源与含义。

**长期记忆快照近因注入**

- **FR-007**: planner 长期记忆快照注入 system prompt 时 MUST 按条目更新时间倒序排列，且仅注入最近更新的 10 条：条目更新时间取 memory 服务返回的 `update_time`（proto `Memory.update_time`，OUTPUT_ONLY）；不足 10 条全量注入、无条目不渲染（既有语义）；排序 MUST 确定（更新时间并列以稳定次序键打破）。快照冻结时机（物化时预取、实例生命周期固定）与 memory 工具写路径 MUST 零变化。

**team 提示词澄清**

- **FR-008**: team section MUST 明确表述：广播标签格式（`<角色-message>`/`<角色-tool-call>` 等）仅是系统向接收方呈现其他成员输出的包装形态，成员自身输出 MUST NOT 使用这些标签格式自我包装（正文直接输出、工具调用走工具协议）；表述对全员生效（team section 为全员共享）。

### Key Entities

- **扫雷系统角色成员（saolei system member）**：新增的非 LLM 系统角色——团队消息流中的合法发送者（稳定 role 标签），非物化成员（无 preset/model/session log）；唯一行为是终局统计播报。
- **终局统计消息（game stats message）**：以扫雷系统角色为发送者的团队消息：结果（won/lost）+ 单个操作总数 + click/flag/chord 分项数。
- **每局操作分项统计（per-type operation counts）**：游戏运行时按局维护的 click/flag/chord 成功执行计数（init 清零、随终局记录携带）。
- **终局交接（game-end handoff）**：既有实体（062：终局工具结果收束 player 回合 → idle 评估 → planner 复盘）——统计播报插入的路径锚点，优先序不变。
- **长期记忆快照（memory snapshot）**：既有实体（planner system prompt 的 `memory:snapshot` section）——注入策略变更为更新时间倒排 + 最近 10 条截断。
- **广播标签格式（broadcast tag format）**：既有实体（060 §4 单一 XML 标注形态）——提示词澄清其仅输入侧呈现的定位。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试（多局 won/lost 链路）：每个被交接的局在团队归并序列中恰有一条扫雷系统角色的统计消息，正文含游戏结果、操作总数与分项数，数值与该局 fake-desktop 实际收到的成功派发序列一致（含批量调用按单个操作计的对照）；planner 复盘 turn 的模型输入含该消息，player 成员视图含该广播（下次结构性驱动输入同批）；`GetTeam.members`/`active_member` 不含该角色。
- **SC-002**: 大型测试（排队跳局场景）：终局后有排队消息驱动 player 开新局时，被跳过的局无统计消息；新局交接时恰有一条新局统计；对照用例（终局后无排队）统计即时播报——排队消化优先序与 062 既有断言回归通过。
- **SC-003**: 单测：快照注入按更新时间倒序且截取最近 10 条（>10 条截断、不足 10 条全量、空不渲染、并列确定）；快照冻结与写路径回归通过。
- **SC-004**: 单测：team section 文本含"成员自身输出不使用广播标签格式"的明确表述，且含扫雷系统角色的 roster/介绍；广播渲染语义回归通过。
- **SC-005**: 回归：既有 agent_v2 大型测试全量通过（多局闭环、排队消化、取消、刷新重建、双视图回填、断开收敛）。

## Assumptions

- **统计消息受众为全体真实成员**（2026-09-15 裁定，编码于 FR-004）：planner 复盘消费 + player 下次结构性驱动同批收到——结构性续驱本就发生，不新增 player turn。
- **消息内容仅 Input 所列 a/b/c 三项**：既有统计字段（correctFlags/avgOpsPerMine）与对局时长等不进入消息正文；消息语言与措辞由 plan 决定（面向模型与用户可读）。
- **系统角色 wire 标签为 `saolei`**（2026-09-15 裁定，编码于 FR-001）：与既有 role 词汇同构（小写英文 token），中文"扫雷系统"由 roster 摘要与消息正文承载；经广播标签名规范化无歧义（`<saolei-message>`）。
- **本 feature 修订 059 FR-009/FR-010 的表述边界**：驱动输入仍 = 成员未消费的团队消息 + 排队用户消息；扫雷系统的统计消息是**系统产生的团队消息**（非编排层以 LLM 成员名义合成的驱动内容），经既有广播/消费机制进入驱动输入——059 的"编排不合成驱动消息"约束在"消息的发送者是系统角色本身"意义上保持。
- **memory 服务侧无改动预期**：proto `Memory.update_time` 已由服务返回（`projects/game/game.proto`；Go 服务 `ListMemories` 携带），JS 客户端捕获该字段即可；若 plan 发现捕获表示法（proto-loader Timestamp）需归一化，属实现细节。
- **快照截断不标注条目总数**：注入文本不附加"共 N 条，显示最近 10 条"类元信息（保持纯条目列表的既有形态）。
- **测试基建联动**：fake-llm 夹具与 agent_v2 大型测试断言随播报语义同批更新（constitution 原则 VI：大型测试全量通过作为验收）。
