# Feature Specification: Team Template Mode (StateGraph 升级)

**Feature Branch**: `031-team-template-mode`

**Created**: 2026-07-29

**Status**: Draft

**Input**: User description: "将 agent 提升至 langchain stateGraph 模型，前期调研见 `survey/agent-team-mode.md`。新增模板（Team 模板）顶层资源并调整 API 层级；将当前单 agent 架构改写为 saolei 模板（player + planner 协作）；游戏操作/状态经 saolei MCP 旁路 sink 输出；profile 按模板特化；desktop 多标签页按 agent 分屏。"

## Clarifications

### Session 2026-07-29

本节记录需求方对前期调研 `survey/agent-team-mode.md` §8 待决策项的澄清结论（原话见需求描述第 7 点）。这些决策直接写入下列功能需求，方案（plan）阶段不再重新讨论（除下方标注为「方案阶段决策」的项外）。

- **survey Q1（teamMemoryId 来源）→ 复用 session id**：team 的长期记忆（策略）以 session id 为键，不引入独立 team 标识。
- **survey Q2（player 读 strategy 注入形式）→ 作为"当前态势"注入 prompt**：策略作为当前态势前置注入 player prompt（非追加到 system prompt 的长期指令，也不给 player 额外读取工具）。
- **survey Q3（planner 模型与 profile 结构）→ profile 模型重构**：废弃单 `AgentProfile`，改为按模板特化的 `TeamProfile`；saolei 模板的 profile 仅含 player/planner 两个 LLM 模型选择。
- **survey Q4（planner 工具集）→ 仅 `update_strategy`，暂无额外只读工具**：planner 不复用 saolei 工具（不控制桌面），仅持有写策略工具；策略读取由代码层完成（LLM 不参与同步）。
- **survey Q5（gameEnded 标志生命周期）→ 方案阶段决策**：planner 处理后由谁清除 gameEnded/gameEvent 标志、如何避免重复触发，留待 `plan.md` 确定具体实现，本 spec 仅约束"planner 每局仅触发一次"的行为结果。
- **survey Q6（清空历史时机/策略）→ `RefreshAgent` 改为 `RefreshTeam`，执行时清理全部短期记忆**：`RefreshTeam` 清空短期记忆（messages），但策略（长期记忆）保留不受影响。

### Session 2026-07-29 (补充澄清)

需求方对 spec 初稿中记录的三处推断/未决项给出确认（均为"确认采用"而非开放问题）：

- **历史数据兼容 → 忽略，不考虑迁移**：本特性为破坏性重构，无需考虑历史数据（既有 session / `AgentProfile` / `Skill`）的兼容与迁移。原 spec 中作为 Assumption 的"clean break"现为需求方确认的决策。
- **Message 路径 → 会话级作用域（确认）**：Message 完整路径为 `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}`（需求方确认初稿简写遗漏了 `sessions/{session}/` 段）。
- **短期记忆自动清空 → 不需要**：短期记忆仅由 `RefreshTeam`（FR-018）显式清空，MUST NOT 在每局开始或其他时机自动清空。

### Session 2026-07-29 (clarify)

- Q: 多局游戏如何推进（自主循环 vs 每 turn 一局）？ → A: graph schema **不**强制多局自动循环逻辑；多局能力由 LLM（player 决定开新局）或用户（新输入）驱动产生，策略作为长期记忆跨局持久。"能完成多局"指 team 具备进行多局的能力，而非要求自动连续多局。
- Q: 用户输入路由到哪个 agent？ → A: 路由给当前选中 tab 的 agent；并为 team 内每个 agent 在模板 graph schema 中增加一个"是否接受用户输入"参数，屏蔽设计上不接受用户输入的 agent（如 saolei 的 `planner`）。saolei 中 `player` 接受用户输入，`planner` 屏蔽（tab 仅作观察视图）。

### Session 2026-07-29 (plan)

方案评审后的决策（落实于 `plan.md`/`research.md`/`contracts/`）：

- Q: player 节点的 agent-loop 形态？ → A: player 为 `createAgent`，内部 agent loop 跑到 LLM 自行决定停下为止（一次运行 = 一次对局）；graph 不在 loop 内中途移交 planner，gameEnded 在 createAgent 返回后由节点后处理读入。
- Q: 策略存储归属？ → A: strategy 由 **agent 服务自身**持久化到 mongo（mongo-backed memory），**不经 prompt 服务**；prompt 服务仅管 TeamProfile。
- Q: 策略初始值？ → A: 无记录时 `get` 返回**空字符串 `""`**（无"预设策略"）；策略内容由 planner 首次 `update_strategy` 写入。
- Q: planner 触发与 update_strategy 重试？ → A: 每局结束进入 planner **恰好一次**；`update_strategy` 的重试由 planner 节点内部处理，graph 调度层不重路由 planner；graph 在 planner 节点返回后无条件清 gameEnded。
- Q: session 删除是否清理 strategy？ → A: 暂不清理（strategy 管理后续优化）。
- ✅ LangGraph JS ^1.4.8 关键 API（`REMOVE_ALL_MESSAGES`、createAgent 作外层图节点、middleware 钩子）已由 spike `experimental/ts/team_graph_spike/` 实测确认（见 `research.md` D14）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Template 与 Team 资源层级重构 (Priority: P1)

运维/开发者通过 API 与 desktop 操作游戏会话时，资源模型从"单 session 单 agent"重构为"模板顶层 → session → team（多 agent）"。Template 成为顶层资源路径段（当前固定常量仅 `saolei`，无 List/CRUD RPC）；Session 挂在模板下；原 Agent 资源被 Team 资源取代；WebSocket connect、Message、TeamProfile 路径相应调整；现有 `AgentProfile` 与 `Skill` 资源（API 管理的自定义 skill）被废弃，TeamProfile 由现有 prompt 服务承接管理。

**Why this priority**: 资源层级是整个控制面的契约地基。下游的 team graph 行为、desktop 多标签页、profile 特化全部依赖新的资源路径与 proto 契约。不先确定契约，其余故事无法落地（宪法原则 III 接口优先设计）。

**Independent Test**: 可通过 proto 契约与各服务 API 路由独立验证：Template 为固定路径段常量（仅 `saolei`，gameconst），Session/Team/Connect/Message/TeamProfile 路径符合契约，`AgentProfile`/`Skill` 的 RPC 与资源消息已移除，prompt 服务转而管理 TeamProfile，MCP 配套的内置 skill 不受影响。

**Acceptance Scenarios**:

1. **Given** API 客户端，**When** 创建/获取会话时，**Then** 路径为 `templates/{template}/sessions/{session}`（`{template}` ∈ {`saolei`}），且不存在顶层 `sessions/*` 路径。
2. **Given** 一个会话，**When** 获取其执行主体时，**Then** 返回的是 `templates/{template}/sessions/{session}/team`（Team 资源），而非 `sessions/{session}/agent`（Agent 资源已被取代）。
3. **Given** 一个会话，**When** 建立 WebSocket 双向流时，**Then** 连接端点为 `templates/{template}/sessions/{session}/connect`。
4. **Given** 一个 team 中的某个 agent（如 `player`），**When** 列出其消息历史时，**Then** 路径为 `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}`（消息按 team 内 agent 分区，`{agent}` 为模板 schema 中已知的 agent 名称）。
5. **Given** prompt 服务，**When** 管理 team 配置时，**Then** 资源为 `templates/{template}/profiles/{profile}`（TeamProfile），且 `AgentProfile`（`prompts/agentProfiles/*`）与 `Skill`（`prompts/skills/*`）的 RPC 及资源消息已废弃移除。
6. **Given** API，**When** 查询模板列表时，**Then** 不存在 Template 的 List/Get/Create/Update/Delete RPC（模板为固定常量，仅作路径段）。
7. **Given** API 客户端，**When** 经 `CreateTeam` 创建 team 时，**Then** 端点为 POST `templates/{template}/sessions/{session}/team`（AIP-133，请求携带 profile——TeamProfile 完整资源名），重复调用（profile 相同）幂等返回既有 Team（FR-033）；Team 未创建时 `GetTeam`/`Connect` 返回 NOT_FOUND（无隐式/懒加载创建）。

---

### User Story 2 - saolei 模板的 player+planner 协作 (Priority: P1)

saolei 模板的 team 是一个 LangGraph StateGraph，包含两个 agent：`player`（独占桌面控制，配置 saolei MCP 执行游戏操作）与 `planner`（不控制桌面，每局结束触发一次复盘并判断是否更新策略）。策略作为 player 与 planner 之间共享的长期记忆（以 session id 为键）；其余 message 作为短期记忆存储于内存缓存。策略作为 system 注入 planner prompt（初始为空字符串，之后由 planner 通过 `update_strategy` 工具更新）；player 则将策略作为"当前态势"注入 prompt。`RefreshTeam` 操作清理全部短期记忆，但策略保留。

**Why this priority**: 这是本次升级的核心行为价值——把单 agent 升级为可协作、可记忆演进的 team。它是把"模板 + StateGraph"概念落到具体能力的关键故事，其余故事（sink、desktop 分屏、profile）都是围绕它展开的支撑/展现。

**Independent Test**: 可在具备 LLM 与桌面（或桩桌面）的环境中独立验证：team 运行一局扫雷时 player 执行操作、planner 不参与操作；当局以 won/lost 结束时 planner 恰好被触发一次并可经 `update_strategy` 写入策略；该策略在后续局中被 player 作为当前态势读取；执行 `RefreshTeam` 后短期消息被清空而策略仍可被读取。

**Acceptance Scenarios**:

1. **Given** 一个 saolei 模板的会话，**When** team 运行时，**Then** 只有 `player` 持有并调用 saolei MCP 工具控制游戏，`planner` 不发起任何桌面操作。
2. **Given** 一局进行中的游戏，**When** player 每次执行落子工具，**Then** `planner` 不被触发（planner 不按步触发）。
3. **Given** 一局游戏，**When** 游戏状态变为 `won` 或 `lost`（局结束），**Then** `planner` 恰好被触发一次进行复盘。
4. **Given** `planner` 复盘，**When** planner 决定更新策略，**Then** 通过 `update_strategy` 工具写入策略到长期记忆（以 session id 为键），且 planner 暂不持有其他读取工具。
5. **Given** 已写入的策略，**When** 下一局 player 被调用，**Then** player 的 prompt 中包含该策略作为"当前态势"（代码层注入，player 无读取工具）。
6. **Given** 已写入的策略，**When** `planner` 再次被触发，**Then** 其 system 上下文中携带当前策略（初始为空字符串，之后为最近一次 `update_strategy` 的结果）。
7. **Given** 一个会话已有若干短期消息与一份策略，**When** 执行 `RefreshTeam`，**Then** 短期记忆（messages）被清空，而策略（长期记忆）保持不变且仍可被读取。
8. **Given** 控制面，**When** 触发刷新，**Then** 调用的是 `RefreshTeam`（取代原 `RefreshAgent`）。

---

### User Story 3 - saolei MCP 旁路事件 sink (Priority: P1)

saolei MCP 提供一个可注册的旁路事件 sink 接口（仅定义事件形状，如游戏开始/落子/结束），自身不实现旁路、不感知 team/strategy/store 概念；默认无 sink 时行为零变化（向后兼容）。模板（team）侧注册 sink 实现，将游戏状态与（结构化的）结束事件写入共享 state，作为 planner 触发与状态可见性的稳定来源（信号来自 MCP 内部第一手计算的 `won|lost|playing` 状态，不解析 tool result 文本）。

**Why this priority**: planner 的"每局结束触发"与游戏状态可见性完全依赖 sink 输出的结构化信号；同时 sink 是 MCP 与 team mode 解耦的契约边界（宪法原则 II：通过设计解耦而非打补丁）。它与 US2 互为支撑，是 team 行为可靠性的前提。

**Independent Test**: 可独立验证 sink 接口契约与注册机制：MCP 在不注册 sink 时与升级前行为一致；注册一个记录型 sink 后，落子/结束事件按契约被回调；事件中的结束状态为结构化枚举（非文本）。

**Acceptance Scenarios**:

1. **Given** saolei MCP 创建时未传入 sink，**When** 执行任何游戏工具，**Then** 行为与升级前完全一致（无旁路输出，无异常）。
2. **Given** 一个已注册的 sink，**When** player 执行落子工具，**Then** sink 收到携带游戏状态的事件回调。
3. **Given** 一个已注册的 sink，**When** 一局游戏状态变为 `won` 或 `lost`，**Then** sink 收到结构化的结束事件（状态为枚举值，非文本）。
4. **Given** sink 接口定义，**When** 审查其类型，**Then** 接口仅描述事件形状，不引用 team/strategy/store/teamMemoryId 等 team mode 概念（MCP 不耦合 team mode）。
5. **Given** team 侧 sink 实现，**When** sink 收到结束事件，**Then** 将游戏状态/结束事件写入共享 state（供 player 节点读取并驱动 planner 触发），该写入在代码层完成、LLM 不参与。

---

### User Story 4 - Desktop 多 Agent 标签页与模板控制面 (Priority: P2)

desktop 将模板作为顶层控制面进行切换（使用本地模板常量，不调用模板列表 API）；不同模板控制面大部分页面通用，少数页面按模板特性化。session 对话页面由单列改为多标签页，每个 tab 对应 team 中的一个 agent；`AgentFrame` 移除 `agent_profile_name`，改为携带 agent 名称（如 `player`），用于表示该消息来自 team 中的哪个 agent，使各 tab 能正确归位消息。

**Why this priority**: 这是 team 模型对最终用户的呈现层。它依赖 US1/US2 的契约与行为落地，本身不改变后端能力，但对"可观测地理解多 agent 协作"至关重要。优先级 P2 是因为它可在核心后端就绪后作为独立展现层交付。

**Independent Test**: 可在 US1/US2 契约就绪后独立验证：desktop 顶层可切换模板（本地常量）；进入会话后对话区按 team 的 agent 分为多个 tab；收到的 frame 按 agent 名称归入对应 tab；frame 不再携带 `agent_profile_name` 而携带 agent 名称。

**Acceptance Scenarios**:

1. **Given** desktop 顶层控制面，**When** 用户切换模板时，**Then** 切换基于本地模板常量完成，且不发起任何模板列表 API 请求。
2. **Given** 模板控制面，**When** 进入某模板时，**Then** 大部分页面通用，少数页面按该模板特性化（saolei 的 profile 页面特化处理）。
3. **Given** 一个 saolei 会话的对话页面，**When** 渲染时，**Then** 对话区呈现多个标签页，每个 tab 对应 team 中的一个 agent（`player`、`planner`）。
4. **Given** 来自不同 agent 的消息，**When** 收到一个 `AgentFrame`，**Then** 该 frame 携带 agent 名称（而非 `agent_profile_name`），并被归入对应 agent 的 tab。
5. **Given** `AgentFrame` 契约，**When** 审查其字段，**Then** 原 `agent_profile_name` 字段已移除，改为表示 team 内 agent 名称的字段。
6. **Given** saolei 会话的多标签页，**When** 用户发送输入时，**Then** 输入路由给当前选中且声明为"接受用户输入"的 agent（`player`）；`planner` 的 tab 因声明为不接受用户输入而屏蔽输入（仅观察视图，见 FR-031/FR-032）。

---

### User Story 5 - 模板特化的 TeamProfile 配置 (Priority: P2)

每个模板对应一套自己的 graph schema，并各自拥有自己的 profile 格式；saolei 模板的 profile 不是"通用 agent 配置"，而仅包括 player 与 planner 的 LLM 模型选择——其余 tools 与 mcp 由模板自身决定，无需配置。TeamProfile 资源由现有 prompt 服务承接管理（路径 `templates/{template}/profiles/{profile}`），desktop 为每个模板的 profile 页面特化处理。

**Why this priority**: 配置面让运维能为 saolei team 选择 player/planner 模型；其"模板特化"语义确立了未来多模板扩展的形态。依赖 US1 的 TeamProfile 资源契约，可在后端就绪后独立交付。

**Independent Test**: 可独立验证：prompt 服务在 `templates/{template}/profiles/{profile}` 下管理 TeamProfile；saolei 的 TeamProfile 仅含 player/planner 两个模型字段；该模板的 tools/mcp 由模板决定、不可在 profile 中配置；desktop 的 profile 页面对 saolei 模板特化渲染（仅模型选择）。

**Acceptance Scenarios**:

1. **Given** prompt 服务，**When** 创建/读取 saolei 模板的配置时，**Then** 资源路径为 `templates/saolei/profiles/{profile}`，且其内容为模板特化的 TeamProfile。
2. **Given** saolei 的 TeamProfile，**When** 审查其可配置字段，**Then** 仅包含 player 模型与 planner 模型的选择，不含 tools/mcp/skill 等字段（这些由模板决定）。
3. **Given** saolei 模板，**When** team 构建时，**Then** player/planner 绑定各自所选模型，而 tools（saolei MCP、`update_strategy`）与 mcp 由模板固定装配，不读取 profile 中的工具配置。
4. **Given** desktop 的 profile 页面，**When** 进入 saolei 模板时，**Then** 页面针对该模板特化（仅渲染 player/planner 模型选择），而非通用 agent 配置表单。

---

### Edge Cases

- **既有数据迁移**：现有 session/profile（基于旧层级）在 API 层级重构后**不考虑兼容与迁移**（需求方确认忽略历史数据）；按本仓库既有惯例（见 `specs/023-saolei-mcp-refine` 等的 clean break）为破坏性重构，开发/测试环境重建。
- **player 持续落子但游戏不结束**：若一局迟迟不结束（player 陷入循环），planner 不被触发（仅局结束触发）；recursion/超时等保护由现有 turn 基础设施承载（本特性不改变）。
- **planner 复盘期间出错**：planner 的 LLM 调用或 `update_strategy` 失败时，不应使整局崩溃或重复触发 planner；具体重试/降级语义由 plan 决定（survey Q5 关联）。
- **gameEnded 标志生命周期**：planner 处理后由谁清除结束标志、如何避免重复触发，为 survey Q5 明确留待方案阶段决策的项；本 spec 仅约束"planner 每局仅触发一次"的行为结果。
- **同一会话多局**：策略作为长期记忆在多局间累积；多局由 LLM（player 开新局）或用户（新输入）驱动，graph schema 不强制多局自动循环（FR-009）。短期记忆**不在每局开始或其他时机自动清空**（需求方确认），仅由 `RefreshTeam`（FR-018）显式清空。
- **模板切换与会话并存**：当前固定常量仅 `saolei`，模板切换与会话并存场景暂不构成实际路径；未来引入第二模板时再约束。
- **agent 名称未知/缺失**：若 frame 携带的 agent 名称不在当前模板 schema 内，desktop 归位策略（丢弃/归入默认 tab）由 plan 决定，本 spec 约束契约字段存在。
- **MCP sink 回调抛错**：sink 实现抛错不应影响 MCP 工具主流程（游戏操作仍正常返回）；具体隔离方式由 plan 决定。

## Requirements *(mandatory)*

### Functional Requirements

#### 资源层级与 API 契约

- **FR-001**: 系统 MUST 引入 Template 作为顶层资源路径段。Template 为资源（`message Template`，pattern `templates/{template}`，无任何 RPC）；具体模板值以 gameconst 常量（资源对象，当前仅 `saolei`）表示，非 proto enum、非裸 string。系统 MUST NOT 提供 Template 的 List/Get/Create/Update/Delete RPC（无模板列表 API）。
- **FR-002**: Session 资源 MUST 嵌套于模板下，资源路径 MUST 为 `templates/{template}/sessions/{session}`；MUST NOT 保留顶层 `sessions/{session}` 路径（破坏性重构，clean break）。
- **FR-003**: 原 Agent 资源 MUST 被 Team 资源取代：Team 资源路径 MUST 为 `templates/{template}/sessions/{session}/team`。Agent 资源消息与相关 RPC MUST 移除。Team 的创建语义见 FR-033（显式 `CreateTeam`，非隐式创建）。
- **FR-004**: WebSocket 双向流连接端点 MUST 为 `templates/{template}/sessions/{session}/connect`（原 `sessions/{session}/connect` 调整）。
- **FR-005**: Message 资源 MUST 按 team 内 agent 分区，路径 MUST 为 `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}`，其中 `{agent}` 为模板 graph schema 中已知的 agent 名称（如 `player`/`planner`），非资源 id。消息 MUST 会话级隔离（以 session id 为作用域）。
- **FR-006**: TeamProfile 资源 MUST 由现有 prompt 服务承接管理，路径 MUST 为 `templates/{template}/profiles/{profile}`。
- **FR-007**: 现有 `AgentProfile` 资源（`prompts/agentProfiles/*`）及其 CRUD RPC MUST 废弃移除；现有 `Skill` 资源（`prompts/skills/*`，即 API 管理的自定义 skill）及其 RPC MUST 废弃移除。MCP 配套的内置 skill（built-in skill，见 `projects/game/agent/src/skill/`）MUST NOT 受本特性影响。
- **FR-008**: 原 `RefreshAgent` 操作 MUST 更名为 `RefreshTeam`（含 RPC、HTTP 路径与 desktop 调用），语义见 FR-018。

#### saolei 模板 Team 行为

- **FR-009**: saolei 模板的 team MUST 为一个 LangGraph StateGraph，包含恰好两个 agent：`player` 与 `planner`。graph schema MUST NOT 强制内置多局自动循环逻辑——"能完成多局"指 team 具备进行多局的能力，而非自动连续多局；多局由 LLM（`player` 决定开新局，如调用 `saolei_init`）或用户（新输入）驱动产生，策略作为长期记忆（FR-013）跨局持久。
- **FR-010**: `player` MUST 独占桌面控制：仅 `player` 持有并调用 saolei MCP 工具执行游戏操作；`planner` MUST NOT 发起任何桌面操作。
- **FR-011**: `planner` MUST 在每局游戏结束时（状态变为 `won` 或 `lost`）恰好触发一次复盘；MUST NOT 在每次落子时触发。
- **FR-012**: `planner` MUST 仅持有 `update_strategy` 工具（用于写策略）；本特性 MUST NOT 为 `planner` 提供额外的策略/状态读取工具。
- **FR-013**: 策略 MUST 作为 player 与 planner 之间共享的长期记忆存储，其命名空间键 MUST 使用 session id（teamMemoryId = session id）；策略 MUST 独立于短期消息存储（策略在长期记忆层，短期消息在内存 checkpointer 层）。
- **FR-014**: 当前策略（初始为空字符串）MUST 作为 system 注入 `planner` 的 prompt；之后策略的更新 MUST 仅通过 `planner` 调用 `update_strategy` 完成。
- **FR-015**: 策略 MUST 作为"当前态势"由代码层注入 `player` 的 prompt（非追加为长期 system 指令，`player` 无策略读取工具）；MUST NOT 由 player LLM 主动读取。
- **FR-016**: 其余 message MUST 作为短期记忆存储于内存缓存（in-memory checkpointer）。
- **FR-017**: planner 触发所依据的局结束信号 MUST 来自 saolei MCP 内部第一手计算的游戏状态（`won|lost`），经结构化途径传递；MUST NOT 依赖对 tool result 文本的解析。

#### 刷新与记忆清理

- **FR-018**: `RefreshTeam` 操作执行时 MUST 清理该会话的全部短期记忆（messages）；MUST NOT 清理或影响策略（长期记忆）——策略在 `RefreshTeam` 后仍可被 player/planner 读取。短期记忆 MUST 仅由 `RefreshTeam` 显式清空；系统 MUST NOT 在每局开始、每局结束或其他时机自动清空短期记忆（需求方确认不需要自动清空）。

#### saolei MCP 旁路 sink

- **FR-019**: saolei MCP MUST 提供一个可选的旁路事件 sink 注册接口（在创建 MCP server 时传入）。sink 接口 MUST 仅定义事件形状（至少包含游戏结束事件及其结构化状态），MUST NOT 引用 team/strategy/store/teamMemoryId 等 team mode 概念。
- **FR-020**: 当未注册 sink 时，saolei MCP 的游戏工具行为 MUST 与升级前完全一致（零行为变化，向后兼容）。
- **FR-021**: saolei 模板（team）侧 MUST 注册一个 sink 实现，在事件回调中将游戏状态与结束事件写入共享 state；该写入 MUST 在代码层完成，LLM MUST NOT 参与该同步。
- **FR-022**: sink 回调 MUST 携带结构化的局结束状态（`won|lost` 枚举），作为 planner 触发的稳定信号源。

#### AgentFrame 与 desktop

- **FR-023**: `AgentFrame` MUST 移除 `agent_profile_name` 字段，改为携带 team 内 agent 名称的字段（如 `player`），用于表示该消息来自 team 中的哪个 agent。
- **FR-024**: desktop MUST 将模板作为顶层控制面进行切换，且 MUST 使用本地模板常量；MUST NOT 发起模板列表 API 请求。
- **FR-025**: desktop 的 session 对话页面 MUST 呈现多个标签页，每个 tab 对应 team 中的一个 agent；来自某 agent 的 frame MUST 归入其对应 tab。
- **FR-026**: desktop 不同模板的控制面 MUST 大部分页面通用，少数页面按模板特性化；saolei 模板的 profile 页面 MUST 特化处理（见 FR-029）。

#### TeamProfile 配置

- **FR-027**: saolei 模板的 TeamProfile MUST 仅包含 player 模型与 planner 模型的选择；MUST NOT 包含 tools/mcp/skill 等可配置字段。
- **FR-028**: saolei 模板的 tools（saolei MCP、`update_strategy`）与 mcp MUST 由模板自身固定装配，MUST NOT 读取 profile 中的工具配置。
- **FR-029**: desktop 的 profile 页面 MUST 对 saolei 模板特化渲染（仅 player/planner 模型选择），而非通用 agent 配置表单。

#### 大型测试（验收）

- **FR-030**: 本特性 MUST 提供大型测试（large test，经 testplan skill 执行），覆盖 saolei 模板 team 的端到端关键行为，至少包含：player 独占控制、planner 每局结束恰好触发一次、策略跨局持久与共享、`RefreshTeam` 清空短期记忆而保留策略。验收标准为所有测试用例全部通过（宪法原则 VI）。

#### Agent 用户输入属性与桌面输入路由

- **FR-031**: team 内每个 agent 在模板 graph schema 中 MUST 声明一个"是否接受用户输入"属性（布尔）。saolei 模板中 `player` MUST 声明为接受用户输入，`planner` MUST 声明为不接受用户输入。
- **FR-032**: desktop MUST 仅对声明为接受用户输入的 agent 开放输入；声明为不接受用户输入的 agent（saolei 的 `planner`）其 tab MUST 屏蔽输入（仅作该 agent 消息流的观察视图）。用户输入 MUST 路由给当前选中且接受用户输入的 agent。

#### Team 显式创建（CreateTeam）

- **FR-033**: Team MUST 经 `CreateTeam` RPC 显式创建（AIP-133）：请求 MUST 携带 parent（Session 资源名 `templates/{template}/sessions/{session}`）与 profile（TeamProfile 完整资源名 `templates/{template}/profiles/{profile}`，其 template 段 MUST 与 parent 一致，由 handler 校验）；响应为 Team 资源。系统 MUST NOT 提供隐式/懒加载创建（如随 `Connect` 自动创建）。重复 `CreateTeam`：请求 profile 与既有 Team 创建时的 profile 相同 → MUST 幂等返回既有 Team；不同 → MUST 返回 ALREADY_EXISTS（details 携带既有 profile）。Team 未创建时，`GetTeam`/`Connect`/`ListMessages`/`RefreshTeam` MUST 返回 NOT_FOUND。

### Key Entities *(include if feature involves data)*

- **Template**：顶层资源路径段，对应一套 graph schema 及配套组件。资源消息（`message Template`，pattern `templates/{template}`），无 CRUD/List RPC；具体值以 gameconst 常量表示（当前仅 `saolei`）。是 API 资源层级的根。
- **Team**：会话内的多 agent 执行主体，取代原 Agent 资源（`templates/{template}/sessions/{session}/team`）。一个 Team 对应模板的一个 StateGraph 实例，含若干按模板 schema 定义的 agent（saolei 为 `player`+`planner`）。**经 `CreateTeam` 显式创建（FR-033，请求携带 profile 构建 team）**，非随 Connect 隐式存在。
- **Agent（team 内）**：Team 中由模板 graph schema 定义的执行角色，由其名称标识（如 `player`/`planner`）。非独立资源，是消息分区与 frame 归位的维度。每个 agent 在 schema 中声明"是否接受用户输入"属性（FR-031）：`player` 独占桌面控制且接受用户输入，`planner` 每局结束复盘且不接受用户输入（desktop 屏蔽其输入）。
- **TeamProfile**：模板特化的 team 配置资源，由 prompt 服务承接管理（`templates/{template}/profiles/{profile}`）。每个模板自有 profile 格式；saolei 的 TeamProfile 仅含 player/planner 模型选择。
- **Strategy（策略，长期记忆）**：player 与 planner 共享的长期记忆，以 session id 为键存储于长期记忆层，独立于短期消息。初始由 system 注入 planner，之后由 `update_strategy` 更新；player 经代码层作为"当前态势"读取。`RefreshTeam` 不影响策略。
- **Short-term messages（短期记忆）**：team 运行过程中的对话/工具消息，存储于内存 checkpointer；`RefreshTeam` 时被清空。
- **SaoleiEventSink（旁路事件 sink）**：saolei MCP 提供的可选事件注册接口，仅定义事件形状（含结构化局结束状态）；默认无 sink 时行为不变，模板侧注册实现将游戏状态/结束事件写入共享 state。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 运维通过新层级（`templates/saolei/sessions/...`）即可完成会话的创建、连接、消息列举与刷新，且旧层级（`sessions/*`、`prompts/agentProfiles/*`、`prompts/skills/*`）不再可达。
- **SC-002**: saolei 模板的 team 具备完成多局扫雷的能力（多局由 LLM/用户驱动，非 graph 强制自动循环）：每局中仅 player 操作桌面，且每局以 won/lost 结束时 planner 恰好被触发一次（同一局内不重复触发）。
- **SC-003**: 策略在会话内跨局累积并被 player/planner 共享——planner 经 `update_strategy` 写入的策略，能在后续局中被 player 作为当前态势读取、被 planner 作为 system 上下文读取。
- **SC-004**: 执行 `RefreshTeam` 后，短期对话消息被清空，而策略仍可被读取（长期与短期记忆解耦可验证）。
- **SC-005**: saolei MCP 在不注册 sink 时与升级前行为完全一致；注册 sink 后能输出结构化局结束状态，且 MCP 代码不引用任何 team mode 概念（解耦可由代码审查验证）。
- **SC-006**: desktop 进入会话后，对话按 team 内 agent 分为多个标签页，frame 按 agent 名称正确归位；用户输入仅对声明为"接受用户输入"的 agent 开放（saolei 仅 `player`），其余 agent 的 tab 屏蔽输入；顶层模板切换基于本地模板常量、无网络请求。
- **SC-007**: saolei 模板的配置仅含 player/planner 两个模型选择，tools/mcp 由模板固定装配、不可经 profile 配置；desktop profile 页面对该模板特化渲染。
- **SC-008**: 大型测试（经 testplan skill 完整部署→测试→清理执行）全部用例通过，覆盖 SC-002/SC-003/SC-004 所述团队行为。

## Assumptions

- **破坏性重构（clean break，需求方确认）**：本特性对 API 层级与 proto 采取破坏性重构，**不考虑历史数据兼容与迁移**（需求方确认忽略既有 session、`AgentProfile`、`Skill` 数据）；不提供旧层级→新层级的在线迁移或双写，开发/测试环境重建。参考本仓库既有惯例（如 `specs/023-saolei-mcp-refine` 的 clean break）。
- **Message 路径为会话级作用域（需求方确认）**：Message 完整路径确认为 `templates/{template}/sessions/{session}/team/agents/{agent}/messages/{message}`（需求方确认初稿简写遗漏了 `sessions/{session}/` 段，见 Clarifications「补充澄清」）。`plan.md` 据此设计 proto 资源 pattern。
- **Template 为资源消息、无 RPC（设计修订已决）**：Template 定义为 `message Template`（pattern `templates/{template}`，无任何 RPC），作为 `Session.template`/`TeamProfile.template` 的 typed 引用目标并驱动 codegen（`ParseTemplateName`）；具体模板值以 gameconst 常量（`game.TemplateName` 资源对象）表示，非 proto enum、非裸 string。原"仅路径段、无资源消息"的假定被该设计修订取代（见 `contracts/api-contract.md` §3.1）。
- **`AgentFrame` agent 名称字段的 proto 表示**：需求方要求"移除 `agent_profile_name`，改为 agent 名称"。具体采用字段重命名（复用字段号）还是移除旧字段+新增字段，属 proto 兼容性细节，由 `plan.md` 依据本仓库 clean break 惯例决定；本 spec 仅约束语义（字段表示 team 内 agent 名称）。
- **survey Q5（gameEnded 标志生命周期）明确留待方案阶段决策**：planner 处理后由谁清除 gameEvent/gameEnded 标志、如何避免重复触发，本 spec 不约束具体实现，仅在 FR-011/FR-017 约束"planner 每局仅触发一次"的行为结果。
- **planner 复盘期间错误处理**：planner 的 LLM 调用或 `update_strategy` 失败时的重试/降级语义未在需求中指定，留待 `plan.md` 设计（与 survey Q5 关联）。
- **sink 回调错误的隔离**：sink 实现抛错时不应阻断 MCP 工具主流程的具体隔离机制，留待 `plan.md`。
- **AgentFrame 字段语义变更的影响面**：`agent_profile_name`→agent 名称的变更会影响 desktop 现有的 frame 归位与历史重建逻辑（见 `projects/game/desktop/frontend/src/App.svelte` `handleMessageParts` 中按 `agentProfileName` 合并消息的逻辑）；该适配属于 US4 的实现范围。
- **既有 turn/队列基础设施复用**：现有 `TurnLoop`、`OperationBridge`、`SessionAgent` 等单 session 单 agent 基础设施是否扩展为承载 team graph、还是被 team graph 取代，属架构实现决策，由 `plan.md` 依据宪法原则 II（重构式变更）决定。
- **参考资料**：前期调研见 `survey/agent-team-mode.md`（含 LangGraph StateGraph/Store/`REMOVE_ALL_MESSAGES`、saoleiEventSink 接口草图、player+planner 数据流与对现有代码的影响预估）。相关现有代码：proto `projects/game/game.proto`；agent `projects/game/agent/src/{session-agent,llm,context-middleware,mcp-host}.ts` 与 `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`（`gameStatus` 于 `:253`、`createSaoleiMcpServer` 于 `:581`）；desktop `projects/game/desktop/frontend/src/{api.ts,App.svelte,components/}`。
