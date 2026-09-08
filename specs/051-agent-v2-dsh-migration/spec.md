# Feature Specification: Game Agent v2 — dsh 迁移 Step 2：游戏 agent 迁移与 desktop 退化

**Feature Branch**: `051-agent-v2-dsh-migration`

**Created**: 2026-08-31

**Status**: Draft

**Input**: User description: "继续 @specs/049-agent-v2-dsh-init/ 继续迁移 agent 到 dsh。1. web ui 优化：a. sesssion(n) 不要换行，现在 (n) 会被挤到第二行 b. 使用图标代替新建和刷新（常见的是加号和带箭头的圆环）。删除则是在 session 的右侧加一个 ··· 按钮，弹出一个菜单里面有删除。c. session 名称过长不再展示滑条框，改为超过长度的右侧内容虚化，鼠标放上内容会左右滑动，类似 chatgpt 页面处理chat 内容过长的情况。2. 迁移 agent 逻辑到 agent-v2 包括：2.1 prompt 服务编辑迁移到 agent-v2 和 web。2.2 agent-v2 新增一个插件负责与 desktop 通信，然后新增一个 saolei-loop 插件提供 agent loop 能力（参考官方的 agent-loop 插件）。前期调研 @survey/deepseek-harness-agent-loop-prereq.md 。本次先不引入 player/planner 双角色，先只使用 player 作为单agent。2.3 原 saolei mcp 进行拆分，游戏状态、游戏历史和与 desktop 通信融合进 saolei-loop，即 loop 本身持有游戏状态和控制。而其余的 tools 和tools 对应 prompt 新增一个 saolei 插件，包括工具和配套的提示词。这里要适配 dsh 的 prompt 系统（按照 dsh 框架提供的能力，插件本身可以提供 prompt 就不需要单独的 skill 了）。2.4 将 prompt 服务提供的agentprofile 与 dsh 的'预设'概念对齐。为agent-v2 增加 create 接口（设置预设）以及 refresh 接口（刷新预设内容和清理短期记忆）。2.5 将 desktop 退化为flow控制，移除session 和 prompt 编辑、session 对话等能力。仅保留连接 session、绑定创建和flow 控制执行等能力（即不与 web 重叠）。本次不包括双角色，不包括长期存储，先实现常规 agent 可以进行游戏。另外，需求内容里有一些假设，制定 spec 前先确认我的假设是否正确，如果不正确应当指出与我讨论。"

## Motivation

`specs/049-agent-v2-dsh-init/` 已交付 agent-v2（dsh 嵌入、GLM Responses 接入、零工具）与 web（session 管理 + 对话页）的最小闭环。本 feature 是 dsh 迁移的**第二步**：把**游戏 agent 逻辑**（saolei 工具、游戏状态、桌面操作链路）与**配置编辑**（原 prompt 服务的 TeamProfile）从 v1 链路迁移到 agent-v2/web，并将 desktop 退化为纯 flow 控制终端——达成"**常规单 agent（player）可以在网页驱动下进行扫雷游戏**"。

前置调研 `survey/deepseek-harness-agent-loop-prereq.md` 已确认：loop 可替换（`AgentFactory` 公共缝）、插件可为插件提供能力（desktop 桥接插件与游戏插件同构于官方服务）、单 agent 拓扑下游戏状态 agent-scoped、桥接形态取插件桥接（2026-08-28 用户确认）。本 feature 落地该调研的"player 单角色先行"子集：**不引入** player/planner 双角色、**不迁移**长期存储（memory）。

需求假设核对结论（2026-08-31 与用户确认）：

- ✅ **成立**：desktop 通信以 agent-v2 插件形态交付（调研 §5.4 已判定插件桥接）；saolei-loop 参考/替换官方 agent-loop（`setFactory` 替换缝已实证）；saolei MCP 拆分方向成立——游戏状态（recognized board、per-game stats）、游戏历史（gameLog/gameEvent，`projects/game/agent/src/team/team-sink.ts` 的 ephemeral buffer）、desktop 通信（OperationBridge 语义）归 loop，工具面与配套提示词归 saolei 插件，且 dsh 插件可自带 prompt section（无需独立 skill 文件）。
- ⚠️ **术语修正**：用户所称 "agentprofile" 现名 **TeamProfile**（`projects/game/game.proto` `templates/{template}/profiles/{profile}`，`specs/031-team-template-mode` 已从 AgentProfile 改名）。本 feature 中该概念由 agent-v2 的 **preset 资源**承接（裁定见 Clarifications），v2 不再使用 TeamProfile 形态。
- ⚠️ **实证约束**：官方 `dsh-agent-spine-demo` **硬挂载**官方 AgentLoop 且无禁用配置，而 `AgentRegistry.setFactory` 不允许二次注册（"an agent factory is already registered"，`node_modules/.pnpm/@deepseek-ai+dsh-agent@0.1.1-rc.2_*/node_modules/@deepseek-ai/dsh-agent/lib/types/index.js:147`）——saolei-loop 上线时 agent-v2 的组合清单**必须从 spine 单行改为直组核心件**（调研 §3.2 预判的"直组核心件"探索项成为本 feature 的必经路径）。
- ⚠️ **dsh 模型路由事实**（用户裁定"按 dsh 的格式拆分模型"的依据）：dsh 的 preset（会话级组合内容）**不含模型路由**——模型是部署资源，per-agent 选择经 `ctx.agents.create({agentOptions: {provider, model}})` 缝在 agent 创建时传入（`@deepseek-ai/dsh-agent` `AgentOptions`，物化源码 `lib/types/runtime-types.d.ts`）；agent-v2 已在使用该缝（`projects/game/agent_v2/src/session.ts:327` 传 `agentOptions`，当前硬编码进程级常量）。

## Clarifications

### Session 2026-08-31（用户裁定）

- **Q1（v1 链路与 prompt 服务）→ 移除 prompt 服务，preset 归 agent-v2 管理**：v1 agent 与 prompt 服务（`projects/game/prompt/`）一并自 game 部署移除（代码保留仓库）；prompt 编辑能力迁移为 agent-v2 的 preset 资源管理 + web 编辑界面。memory 服务本 feature **不动**（保留部署与 gateway 路由；处置留给后续长期存储迁移 feature——"本次不包括长期存储"的最小动作解读）。
- **Q2（预设字段与接口规范）→ 按 dsh 格式拆分模型，接口遵循 AIP**：
  - preset **不含模型**；模型在**创建（物化）agent 时选择**（对齐 dsh：preset=组合内容，模型=host 平面 per-agent 选项）。
  - **Agent 为 session 的单例资源**（AIP-156，参考 v1 Team 形态）：原 ConversationService 更名（conversation 之名不再合适）；`Send` 保留为自定义方法（`:send` 流式）；`ListHistory` 改为**标准方法**（消息作为 agent 子资源的标准 List）；**`Dispose` 移除**——暂时不考虑 session 删除后的 agent 清理。
  - **preset 为 template 下的正常资源**：标准 CRUD 方法。
- **Q2 追加裁定（2026-08-31）→ refresh 并入 Update、Send 显式前置**：`:refresh` 自定义方法移除——refresh 用例由 Update 承载：Update 对不存在的 agent 为创建（物化），对已存在的 agent 为刷新（重读 preset 当前内容）并**同时清空短期记忆**（无论配置是否变化）；agent 经 Update **显式物化**（选择 preset + model），**Send 不再默认创建 agent**（未物化 session 的 Send 明确报错）。
- **Q3（desktop 链路）→ A**：完整复刻既有模式——gateway 新增 `/api/v2` 前缀的 WS 入口 → proxy 定向转发 → agent-v2 桌面桥接插件的 gRPC 面；**flow 控制流与对话流拆分，两者独立互不影响**。
- Q: agent 经 Update 显式物化时，preset 引用是必填还是可选？ → A: **必填**（选项 A）；且**不预置默认 preset 资源**——使用前需先创建 preset（preset 提示词留空仍回退模板默认 base），无"内置默认 persona"隐式形态。
- Q: web 物化表单的模型选项从哪里来？ → A: agent-v2 提供只读的可用模型目录查询（来源=组合配置的模型目录，llm-glm `models[]`），web 下拉选择；物化校验与目录**同源**（杜绝"下拉可选、提交被拒"分裂）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 在网页上驱动一局完整的扫雷游戏 (Priority: P1) 🎯 MVP

用户在 desktop 上选择窗口绑定并连接某个 game session（flow 控制就绪）；在 web 对话页向该 session 发送消息（如"开始一局扫雷"）；agent（player 单角色）经模型推理调用 saolei 工具（init/operate/remain），工具操作经桌面桥接下发到 desktop 真实执行（按键/点击 + 截图回传），agent 识别棋盘、校验并继续推理，直到游戏结束（won/lost）。整个过程中：web 对话页流式可见正文、思考与**工具调用块**（049 已建渲染能力的端到端首次兑现）；desktop 仅执行操作，不展示对话。

**Why this priority**: 本 feature 的最终目标（"先实现常规 agent 可以进行游戏"）的最小完整切片：单此故事即可演示"web 驱动 + dsh agent + saolei 工具 + desktop 执行"的端到端价值，也是后续双角色/复盘机制的基础。

**Independent Test**: 部署后（大型测试以确定性 fake LLM + fake desktop 替换真实端点），经 web（或 `/api/v2`）发送游戏指令，断言：工具调用块在对话页可见、desktop（或 fake desktop）收到对应操作、工具结果携带文本棋盘与 `game status:` 行、直至 `won`/`lost` 终局。

**Acceptance Scenarios**:

1. **Given** session S 已显式物化 agent（经 Update 选定 preset 与 model）且 desktop 已连接并绑定窗口，**When** 用户在 web 对话页发送"开始一局扫雷"，**Then** agent 调用 saolei 工具发起游戏，web 对话页以工具调用块流式呈现调用与结果（文本棋盘），desktop 收到并执行新局操作。
2. **Given** 一局进行中，**When** agent 依推理连续调用操作工具（含批量操作），**Then** 每次操作经校验后下发 desktop 执行，非法操作按既有拒绝码在工具结果中呈现（不下发），棋盘随结果刷新。
3. **Given** 某操作触发终局（won/lost），**When** 该操作的工具结果返回，**Then** 结果携带终局状态，其后的单元格操作按 `game_won`/`game_over` 拒绝，游戏历史（本局操作序列与统计）由 loop 记录。
4. **Given** desktop 未连接或未绑定窗口，**When** agent 调用需要下发的工具，**Then** 工具以明确的可读错误结果返回（模型可见、非假成功），回合不崩溃、进程存活。
5. **Given** desktop 中途断开，**When** 游戏进行中，**Then** 在途操作按错误结果返回，desktop 重连后游戏可继续（棋盘状态保留，必要时经 init 重播种）。
6. **Given** 对话流与 flow 控制流并行工作（web 正在收流式回复、desktop 同时执行操作），**When** 任一流断开或故障（如 desktop 掉线），**Then** 另一条流不受影响（对话流不因 desktop 断开而中断，flow 流不因 web 断开而中断——两流独立，Q3 裁定）。

---

### User Story 2 - preset 由 agent-v2 管理、在 web 编辑，agent 按单例资源物化 (Priority: P1)

用户在 web 上管理 **preset**（原 prompt 服务 TeamProfile 编辑的迁移形态，template 下的正常资源）：查看列表、新建/编辑/删除（内容为 player 提示词；**模型不在 preset 中**）；在 session 的 **agent 单例资源**上经 Update 选择 preset（必选）与模型（可选，缺省进程默认）完成物化（对齐 dsh"模型在创建 agent 时选择"）；编辑 preset 后再次 Update 该 agent 即为刷新（重读 preset 当前内容并清空短期记忆——refresh 用例并入 Update）。preset 数据持久化，agent-v2 重启不丢失。

**Why this priority**: 用户 2.1/2.4 的直接要求与 AIP 接口规范化裁定；US1 的"设置预设/模型"入口与 049 对话能力的配置化演进（对齐 v1 的 Team 单例物化/RefreshTeam 语义与 dsh 的"预设=会话级组合内容、模型=创建时选择"概念）。

**Independent Test**: 通过 web（或 agent-v2 API）完成 preset CRUD → agent 物化（含模型选择）→ 对话一轮 → 编辑 preset → 再次 Update（刷新）→ 断言新内容生效且历史清空、未物化的 Send 被明确拒绝、preset 数据重启后仍存在。

**Acceptance Scenarios**:

1. **Given** 用户在 web 的 preset 管理界面，**When** 新建/编辑/删除 preset，**Then** 标准资源操作成功且列表即时反映，数据经 agent-v2 持久化（重启后仍在）。
2. **Given** preset P 已存在，**When** 对 session S 设置 agent（物化，指定必选的 P 与可留空的 M），**Then** S 的 agent 以 P 的提示词与 M 物化，后续对话体现 P 的 persona；未指定模型时以进程默认模型物化。
3. **Given** session S 的 agent 已按 (P, M) 对话多轮，**When** 经 Update 修改 agent 配置（换 preset 或换模型），**Then** 短期记忆被清理、按新配置重新物化（对齐 dsh 创建期模型选项与 blank 切换约束）。
4. **Given** session S 的 agent 绑定 preset P，**When** P 被编辑为 P' 后对 S 的 agent 再次 Update（配置不变），**Then** S 的短期记忆被清理（对话历史清空）、P' 内容生效；在途回合按既定终止语义处理。
5. **Given** session S 未物化 agent，**When** 用户直接发送消息（Send），**Then** 请求被明确拒绝并提示需先物化 agent；web 对话页在未物化 session 上引导用户完成物化（选择 preset 与 model）后再对话。
6. **Given** preset 提示词留空，**When** 以该 preset 物化 agent，**Then** persona 回退到模板默认 base（对齐 v1 FR-034 空值回退语义）。
7. **Given** 物化时指定了未知的模型 id，**When** 提交设置，**Then** 请求被明确拒绝（fail-fast），不产生半物化状态。

---

### User Story 3 - desktop 退化为 flow 控制终端 (Priority: P2)

desktop 不再提供 session 管理、prompt/preset 编辑、session 对话等界面（这些能力由 web 承担，不重叠）；保留：**连接 session**（选择/指定一个 game session 并建立与 agent-v2 的 flow 控制流）、**绑定创建**（窗口枚举与绑定）、**flow 控制执行**（接收并执行操作请求、回传结果与截图、操作确认抽屉等既有执行语义）。游戏期间 desktop 可见操作执行情况（日志/状态），但**不展示对话内容**。

**Why this priority**: 用户 2.5 的直接要求；是 US1 的执行端前提（desktop 是唯一真实操作执行者），改造本身以"移除"为主、风险低。

**Independent Test**: 启动 desktop 断言：会话管理/preset 编辑/对话界面不再存在；连接 session → 绑定窗口 → 收到 agent 操作请求并执行 → 回传结果截图的链路可用（与 US1 联测）。

**Acceptance Scenarios**:

1. **Given** desktop 启动，**When** 用户浏览界面，**Then** 不存在 session 新建/删除/切换管理、preset（TeamProfile）编辑、session 对话/消息展示入口。
2. **Given** desktop 已配置连接地址，**When** 用户选择一个 session 并连接，**Then** 连接建立（含探测确认），连接状态可见；重复连接按既有"新连接接管、旧连接关闭"语义处理。
3. **Given** 已连接且已绑定窗口，**When** agent 下发操作（按键/点击/移动点击），**Then** desktop 执行并回传结果（含截图），确认模式下经确认抽屉放行。
4. **Given** session 经 web 被删除，**When** desktop 仍连接该 session 的 flow 流，**Then** 连接与 agent 保持存续（本 feature 裁定：暂不考虑 session 删除后的 agent/连接清理；残留为已接受限制，见 Edge Cases）。

---

### User Story 4 - web session 侧栏交互优化 (Priority: P2)

侧栏标题 `Sessions (n)` 中的计数不再被挤到第二行；新建/刷新改用图标按钮（加号、带箭头圆环）；删除入口移到每个 session 条目右侧的 `···` 按钮弹出菜单中；session 名称过长时不再出现滚动条框，改为超出部分右侧虚化（渐隐遮罩）、鼠标悬停时内容横向滚动展示（对齐 ChatGPT 对超长会话名的处理方式）。

**Why this priority**: 纯前端交互打磨（用户 1），独立可交付、可组件级验证；不影响其他故事的数据面。

**Independent Test**: 组件测试驱动长名称/多 session 数据，断言：标题不换行、图标按钮可点且可辨识、`···` 菜单内删除可用、长名悬停滚动且无滚动条。

**Acceptance Scenarios**:

1. **Given** 侧栏宽度固定且 session 数量较多，**When** 渲染标题，**Then** `Sessions (n)` 保持单行（计数不换行）。
2. **Given** 侧栏操作区，**When** 用户需要新建/刷新，**Then** 以图标按钮（加号/圆环箭头）呈现，悬停有可辨识提示，行为与原文字按钮一致。
3. **Given** 某个 session 条目，**When** 点击其右侧 `···` 按钮，**Then** 弹出菜单包含"删除"，执行后该 session 按本 feature 的删除编排（仅删除 session 元数据，见 FR-007）移除。
4. **Given** session 名称超过条目宽度，**When** 未悬停，**Then** 超出部分以右侧渐隐虚化呈现（无滚动条）；**When** 鼠标悬停，**Then** 内容横向滚动可看全名。

---

### Edge Cases

- **desktop 缺席/断连**：工具下发得到明确错误结果（模型可见），回合继续、进程存活；重连后可继续（US1 场景 4/5）。
- **窗口未绑定**：需要执行的操作返回明确错误（与 desktop 缺席同语义），不产生半执行状态。
- **识别失败**：棋盘识别失败时游戏状态失效，后续单元格操作按 `no_active_game` 拒绝直至重新 init（v1 契约保持）。
- **Update（重新物化）与在途回合并发**：按既定终止语义处理（在途回合终止、排队消息作废）后清空记忆重新物化，不产生半清理状态。
- **preset 被删除时仍有 agent 引用**：已物化的 agent 不受影响（preset 内容已固化）；再次物化/切换引用该 preset 得到明确错误。
- **session 删除后的残留**（Q1/Q2 裁定：不考虑清理）：删除 session 仅移除元数据，agent-v2 内存中的 agent（含历史、物化配置与 flow 连接）残留；以**相同资源名**新建 session 后残留 agent 仍处于已物化状态（Send 直接可用，旧配置与旧历史生效，无需 Update）——已接受限制，清理机制留待后续 feature。
- **并发多 session 游戏**：多个 session 各自的游戏状态/历史互不串扰（loop 按 session 隔离）。
- **同一 session 多个 desktop 连接**：按"新连接接管"语义处理（v1 基线），旧连接关闭。
- **agent-v2 重启**：仅 preset 数据持久不丢；agent 整体为内存态——对话历史、游戏状态与物化配置（preset/model 绑定）随进程丢失；重启后再次对话前 MUST 重新经 Update 物化（Send 不再懒物化，FR-007）。
- **未物化的 Send**：明确报错（要求先经 Update 物化），进程与会话元数据状态不受影响。
- **web 刷新/回填**：049 的历史回填行为保持（经标准 List 方法），工具调用块在回填中按 tool_id 关联呈现。
- **非法输入**：空 preset id、非法 session 名、空消息、未知模型 id 等按标准错误语义明确报错（400/INVALID_ARGUMENT/NOT_FOUND）；未物化 agent 的 Send 按前置条件错误明确报错（见上一条）。

## Requirements *(mandatory)*

### Functional Requirements

#### Web UI（用户需求 1）

- **FR-001**: web 侧栏标题 `Sessions (n)` MUST 保持单行呈现——计数部分不得因空间不足换行到第二行（侧栏任意常规宽度下）。
- **FR-002**: 侧栏"新建/刷新"操作 MUST 以图标按钮呈现（加号=新建、带箭头圆环=刷新），行为与现文字按钮一致（含 loading 态禁用）；图标 MUST 带可辨识的悬停提示（tooltip/aria-label）。
- **FR-003**: 删除入口 MUST 迁移为每个 session 条目右侧的 `···` 按钮：点击弹出菜单，菜单含"删除"项；条目级操作不依赖当前选中态。
- **FR-004**: session 名称超过条目宽度时 MUST 以右侧渐隐虚化呈现超出的内容（MUST NOT 出现横向滚动条）；鼠标悬停时内容 MUST 横向滚动以展示完整名称（移出后复位）。

#### preset 资源与 agent API（用户需求 2.1/2.4 + 2026-08-31 裁定）

- **FR-005**: agent-v2 MUST 提供 **preset 资源**（template 下的正常资源，`templates/{template}/presets/{preset}`）的**标准 CRUD 方法**（Create/Get/List/Update/Delete，AIP-133/131/132/134/135），并**持久化**存储 preset 数据（agent-v2 重启不丢失）；preset 字段为 player 提示词内容（+资源元数据），**MUST NOT 包含模型字段**（模型在 agent 物化时选择，Q2 裁定）；web MUST 提供 preset 编辑界面消费该 API。原 desktop 的 TeamProfile 编辑界面与 prompt 服务的 TeamProfile 资源随 FR-019 移除。
- **FR-006**: agent-v2 MUST 将 agent 建模为 **session 的单例资源**（AIP-156，`templates/{template}/sessions/{session}/agent`，参考 v1 Team 形态——无 Create/Delete RPC），经 **Update（allow_missing=true，AIP-134 create-or-update）显式物化**（**preset 引用必填**——不预置默认 preset 资源，需先创建 preset；模型可选、缺省用进程默认模型，物化时 MUST 对模型 id 做校验，未知 id 拒绝）。Update 的统一语义（2026-08-31 追加裁定，refresh 用例并入）：agent 不存在则**创建**；已存在则**刷新**——重读所引用 preset 的**当前内容**并清空短期记忆（对话历史、排队消息、游戏状态；在途回合终止）后按所给配置重新物化，**无论配置是否变化**；结果状态幂等（重复 Update 得到相同配置的干净 agent）。preset 提示词为空时 persona 回退模板默认 base（v1 FR-034 语义延续）。MUST 提供 Get 方法查询当前物化配置。物化时模型经 per-agent 选项传入（dsh `AgentOptions` 缝，`projects/game/agent_v2/src/session.ts:327` 已用该缝）。agent-v2 MUST 提供只读的可用模型目录查询（来源=组合配置的模型目录 llm-glm `models[]`），web 物化表单的模型选项与物化校验 MUST 与该目录同源。
- **FR-007**: 对话 API 面 MUST 按 AIP 规范化调整（服务更名，原 ConversationService 之名不再合适）：
  - `Send` 保留为**自定义方法**（`:send`，server-streaming，事件模型不变），且 MUST NOT 懒物化 agent（2026-08-31 追加裁定）——未物化 session 的 Send MUST 以明确错误拒绝（提示先经 Update 物化）；
  - `ListHistory` MUST 改为**标准方法**——消息作为 agent 单例的子资源（`templates/{template}/sessions/{session}/agent/messages`）的标准 List（AIP-132），语义（保序/保分类/回填）与 049 FR-014 一致；
  - `Dispose` RPC MUST 移除：session 删除**不联动** agent 清理（Q2 裁定"暂不考虑"）；web 的 session 删除编排相应简化为仅删除 session 元数据（不再调用 dispose）；同名重建 session 命中残留 agent 为已接受限制（Edge Cases）。
- **FR-008**: agent-v2 MUST NOT 提供 refresh 自定义方法（2026-08-31 追加裁定：refresh 用例由 FR-006 的 Update 统一承载——Update 已存在的 agent 即"重读 preset 当前内容 + 清空短期记忆"）；web MUST 提供 agent 物化入口（preset 必选、model 可选——选项来自 FR-006 的模型目录查询）以支撑显式物化流程（未物化 session 的对话页引导物化，替代 049 的懒创建行为）。

#### saolei-loop 与桌面桥接（用户需求 2.2 + Q3 裁定）

- **FR-009**: agent-v2 MUST 新增 **desktop 通信插件**：提供与 desktop 的**双向 flow 控制流**——下行（agent → desktop）操作请求下发，上行（desktop → agent）操作结果与截图回传、连接探测；同一 session 的重复连接 MUST 按新连接接管语义处理（v1 基线：旧连接关闭）。链路 MUST 为：desktop → gateway（新增 `/api/v2` 前缀的 WS 入口，形态对齐 v1 connect 模式）→ proxy（定向转发）→ agent-v2 桥接插件 gRPC 面（Q3 裁定 A；desktop 既有 GatewayURL 配置零改动）。**flow 控制流与对话流 MUST 独立互不影响**（任一断开/故障不影响另一条）。
- **FR-010**: agent-v2 MUST 新增 **saolei-loop 插件**替换官方 agent-loop 驱动（参考官方 `@deepseek-ai/dsh-agent-loop` 实现模式）：提供 agent 工厂（`AgentFactory` 替换缝）、以 player 单角色驱动每 session 一个 agent 的 turn/step 循环；MUST 继承调研 §4.7 所列官方异常处理模式（abort 贯穿、事件面、turn 终局语义、工具调度与取消、失败可见不伪造结果）。049 的对话能力（Send 流式/排队/历史回填）在替换后 MUST 行为零回归（除 FR-007 裁定的接口调整）。
- **FR-011**: saolei-loop MUST 持有**每 session 的游戏状态与游戏历史**（session 生命周期内）：游戏状态含最近识别棋盘与逐局统计（operationCount/correctFlags/avgOpsPerMine，v1 game-stats 契约语义）；游戏历史含本局操作序列（一次工具调用记一条含完整操作列表）、终局事件（won/lost + 统计）——即 v1 `EphemeralGameBuffer`/`SaoleiEventSink` 语义的承载者（`projects/game/agent/src/team/team-sink.ts`）；MUST 经 desktop 通信插件下发操作（loop 即游戏控制点）。游戏状态 MUST NOT 经隐藏通道外泄（模型可见事实经工具结果呈现，v1 契约保持）。
- **FR-012**: agent-v2 组合清单 MUST 从 spine 单行改为**直组核心件 + saolei-loop**（官方 spine 硬挂载 AgentLoop 且 factory 不允许重复注册——见 Motivation 约束）；被替换掉的官方 loop 不得残留于组合中。

#### saolei 工具插件与提示词（用户需求 2.3）

- **FR-013**: agent-v2 MUST 新增 **saolei 插件**：提供三个工具（`saolei_init`/`saolei_operate`/`saolei_remain`）注册进工具面，工具行为契约与 v1 保持（文本棋盘 + 坐标标尺 + `game status:` 行、双形式 operate、无操作批处理、拒绝码三元组语义、remain 只读计算——`projects/game/agent/src/skill/saolei/SKILL.md` 所载契约）；工具的操作执行与状态读写 MUST 经 saolei-loop 持有的游戏状态/桥接完成（工具是无状态面向模型的人口，状态与控制在 loop）。
- **FR-014**: saolei 插件 MUST 以 dsh prompt 机制提供配套提示词（插件自注册 prompt section，内容承载现 SKILL.md 的工具使用守则），**不单独保留 skill 文件形态**；提示词与工具同插件交付、随插件启停生效（dsh 所有权原则）。
- **FR-015**: 工具结果的棋盘识别 MUST 复用既有确定性识别能力（`@dominion/game-saolei-board` 语义与坐标空间纪律：截图空间识别、client 空间下发）——本 feature 不改变识别与校验规则本身。

#### desktop 退化（用户需求 2.5）

- **FR-016**: desktop MUST 移除以下能力与界面：session 管理（新建/列表管理/删除/切换对话）、Team/preset（TeamProfile）编辑与管理、session 对话与消息展示（含聊天流、消息气泡）。
- **FR-017**: desktop MUST 保留并延续以下能力：**连接 session**（含连接探测与接管语义，目标为 agent-v2 会话的 flow 控制流）、**绑定创建**（窗口枚举/选择/绑定）、**flow 控制执行**（操作请求执行、结果与截图回传、操作确认抽屉、调试模式相关执行语义）、配置与日志查看。
- **FR-018**: desktop 连接的目标 MUST 是 agent-v2 的 flow 控制流（经 FR-009 桥接面）；desktop MUST NOT 再连接 v1 agent 的 TeamService 面。

#### 范围与验收

- **FR-019**: v1 链路处置（Q1 裁定）：v1 agent（`projects/game/agent/`）与 prompt 服务（`projects/game/prompt/`）MUST 自 game 部署移除（代码保留仓库），随之下线：gateway 的 `/api/v1` TeamService REST 路由与 WS connect 入口、PromptService 路由及对应连接、proxy 的 TeamService 转发面、相关既有 testplan suites；memory 服务及 gateway memory 路由本 feature **不动**（保留部署，处置留给长期存储迁移 feature）。范围排除（用户明确）：MUST NOT 引入 player/planner 双角色；MUST NOT 迁移长期存储；MUST NOT 实现 planner 复盘/策略机制。
- **FR-020**: 系统 MUST 附带大型测试（testplan）：以确定性 fake LLM（既有 fake-llm Responses 端点）与 **fake desktop 执行器**（响应操作请求、回传可识别的确定性截图/结果）部署，验证 US1 端到端游戏闭环（工具链路、棋盘反馈、终局、异常分支、两流独立性）、US2 preset/agent 物化闭环（CRUD/持久化/物化与模型选择/Update 刷新语义/未物化 Send 拒绝）、US3 desktop 链路（连接/绑定/执行以 fake desktop 或真 desktop 冒烟记录）与 US4 组件级验证，完成清理；验收标准为经 testplan skill（`guitar run`）实际执行完整部署→测试→清理闭环且**全部用例通过**（`.specify/memory/constitution.md` 原则 VI）。

### Key Entities

- **Preset（预设）**: template 下的正常资源（`templates/{template}/presets/{preset}`）——player 提示词内容 + 资源元数据；**不含模型**；agent-v2 提供标准 CRUD 并持久化；是 agent 物化时 persona 的来源（与 dsh"预设=会话级组合内容"概念对齐）。
- **Agent（会话 agent 单例）**: session 的单例资源（`templates/{template}/sessions/{session}/agent`，AIP-156）——绑定一个 preset 引用 + 模型选择（物化时经 per-agent 选项生效）；短期记忆=对话历史（内存态）+ 游戏 runtime（随 Update 重新物化清理）。
- **Message（消息子资源）**: agent 单例下的消息集合（标准 List）；内容块分类（text/think/tool call/tool result）语义延续 049。
- **Game State（游戏状态）**: 每 session 的最近识别棋盘 + 逐局统计；saolei-loop 持有；init 播种/操作刷新/识别失败失效。
- **Game History（游戏历史）**: 本局操作序列（gameLog）+ 终局事件（won/lost + stats）；saolei-loop 持有。
- **Desktop Bridge（桌面桥接/flow 控制流）**: agent-v2 内的 desktop 通信能力（插件）：连接管理、操作下发/结果回传；被 saolei-loop 消费；与对话流独立（Q3 裁定）。
- **Desktop Binding（窗口绑定）**: desktop 本地的目标窗口选择（绑定创建），操作执行的目标。
- **Saolei Tool Surface（工具面）**: init/operate/remain 三工具 + 配套 prompt section（saolei 插件交付）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 大型测试全部通过：US1 游戏闭环（含异常分支与两流独立性）、US2 preset/物化闭环、US4 组件用例 100% 通过（零 failed、零 flaky），执行期间零外部网络依赖（fake LLM + fake desktop）。
- **SC-002**: 行为零回归（除裁定调整项）：049 对话用例（流式 text/think、多轮、排队、历史回填——经更名后的标准 List 方法）在 loop 替换后保持通过；saolei 工具结果契约（文本棋盘/标尺/状态行/拒绝码/批量语义）与 v1 契约用例一致；Dispose 移除、删除编排简化与 Send 显式物化前置（不再懒创建）为裁定内变更。
- **SC-003**: web 侧栏四项交互（标题单行/图标按钮/`···` 删除菜单/长名虚化+悬停滚动）全部经组件级测试验证可用。
- **SC-004**: desktop 能力清单达成：会话管理/preset 编辑/对话界面不复存在；连接/绑定/执行链路经大型测试或冒烟记录验证可用。
- **SC-005**: v1 链路处置落地：game 部署不再含 v1 agent 与 prompt 服务，gateway `/api/v1` team/prompt 路由与 WS 入口、proxy TeamService 转发面清理完整（memory 路由保留）；代码保留仓库。
- **SC-006**: token 零泄漏延续（049 SC-004）：全部交付物中不存在明文模型 token。

## Assumptions

- **A1（memory 服务默认不动）**: 用户未对 memory 服务显式裁定；按"本次不包括长期存储"取最小动作——保留部署与 gateway 路由，处置留给后续长期存储迁移 feature（可在 review 时否决调整）。
- **A2（agent 与其配置同为内存态，2026-08-31 用户确认）**: agent（含对话历史、游戏状态与 preset/model 物化配置）整体为 agent-v2 进程内存态：重启后全部丢失，需重新经 Update 物化方可对话（Send 不再懒物化，FR-007）；**绑定关系不持久化**。仅 preset 资源数据持久化（FR-005）。
- **A3（服务与资源命名）**: 原 ConversationService 更名（如 AgentService）与各资源/方法的最终命名、proto 组织、服务分组（单服务承载 agent+preset 或分立）在 plan 阶段契约文档确定；`/api/v2` 前缀与 `:send` 自定义方法命名延续（`:refresh` 已随追加裁定移除）。
- **A4（desktop 选 session 的形态）**: desktop 移除会话管理后保留**只读的 session 选择**（列表或输入，用于指定连接目标），不提供任何管理操作——与 web 不重叠。
- **A5（游戏状态可见性）**: 游戏状态对模型的呈现仍只经工具结果（文本棋盘）；web 对话页经工具调用块可见操作流；desktop 不展示对话（仅执行与状态/日志）。
- **A6（识别与校验规则不变）**: 棋盘识别、坐标空间、校验拒绝码、胜负判定（counter-informed win）全部沿用 v1 语义，仅迁移承载位置。
- **A7（049 机制延续）**: 排队（049 FR-012）、内存历史回填（049 FR-014，经标准 List 方法）、token 三级解析与双 artifact（049 FR-008）等语义全部延续；session 删除的"立即终止释放"（049 FR-015）按本 feature Q2 裁定**废止**（不清理、dispose 移除）；049 的 Send 懒物化（get-or-create 默认 persona）按追加裁定同样**废止**（显式物化前置）。
- **A8（样板与版本线）**: dsh 家族维持 0.1.1-rc.2 精确 pin；saolei-loop 实现参照官方 agent-loop 物化源码（`node_modules/.pnpm/@deepseek-ai+dsh-agent-loop@0.1.1-rc.2_*`）"抄设计"而非继承代码（调研 §7.2 风险 1）。
- **A9（大型测试的 fake desktop）**: fake desktop 执行器作为测试基础设施交付（对操作请求回传确定性结果/截图，截图可被既有识别库识别——复用 saolei-board 测试图数据思路），不进生产部署。
