# Feature Specification: Team Template Mode 缺陷修复

**Feature Branch**: `036-team-mode-bugfix`

**Created**: 2026-08-04

**Status**: Draft

**Input**: User description: "根据 `specs/031-team-template-mode/bug-analysis.md` 调研文档，制定 spec 修复文中提到的3个问题；同时修复 turn loop 没有基础外层 graph 的设置。"

## Clarifications

### Session 2026-08-04 (设计意图来自 bug-analysis.md 用户澄清)

本特性为 `specs/031-team-template-mode`（Team Template Mode）已实现行为的缺陷修复。四个缺陷的根因分析与修复方向已由调研文档 `specs/031-team-template-mode/bug-analysis.md`（Issue 1-3）及 plan 阶段分析（Issue 4）记录。其中 Issue 1 与 Issue 2 的"设计意图"经需求方澄清。以下记录关键决策，`plan.md` 阶段不再重新讨论：

- **Issue 1 设计意图（需求方澄清）**：游戏结束（won/lost）时应**立即**触发 planner，而不是等 player 的内部 agent loop 跑完后再检查。当前实现中 player 节点的 createAgent 内部 loop 在游戏结束后不自动停止（LLM 可能重开新局），导致后处理不可达、planner 不被触发。修复后，游戏结束→player loop 立即停止→触发 planner→planner 返回后→player 继续下一步。
- **Issue 2 设计意图（需求方澄清）**：planner 应该看到**完整的游戏过程**——player 的每一步操作以及操作后的游戏状态——而不是仅终局棋盘快照。这些内容应该在 planner tab 中可见。
- **Issue 3**：用户消息气泡未靠右对齐。根因为 ChatView 对 ChatMessage 外包了一层 `.msg-row`（无对齐类），内层 ChatMessage 的 `.msg-row.msg-user` 无法生效。
- **Issue 4**：player 与 planner 节点的 createAgent `invoke()` 未传递外层 graph 的 config（含 `recursionLimit`）。外层 team graph 被以 `recursionLimit: 1000` 调用（`session-team.ts:259`），但内部 createAgent 各自以默认 `recursionLimit: 25` 运行。LangGraph 节点函数签名为 `(state, config?)`，当前节点函数仅接受 `state`，未接受/转发 `config`。修复后，节点函数接受 `config` 并传递给内部 `invoke()`，使内部 createAgent 继承外层 graph 的递归上限。
- **复盘输入可见性**：Issue 2 的完整游戏过程在**历史加载**（ListMessages）时已在 planner tab 可见；是否需要在**实时流**中推送为可选增强（不作为本特性的硬性要求，见 Assumptions）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 游戏结束时立即停止 player loop 并触发 planner (Priority: P1)

当 player 在扫雷游戏中落子导致游戏以 won 或 lost 结束时，player 的内部 agent loop 应当在该步操作完成后立即停止（而非继续运行让 LLM 决定开新局），使控制权返回外层 team graph 节点。节点随后设置 `gameEnded` 标志，条件边将控制权路由给 planner 进行复盘。当前实现中，游戏失败（lost）后 LLM 通常调用 `saolei_init` 重开新局，多局累积步数超过递归上限后抛出 `GraphRecursionError`，使消费游戏事件的后处理代码不可达，导致 planner 永远不被触发。

**Why this priority**: 这是 `specs/031-team-template-mode/spec.md` FR-011（"planner MUST 在每局游戏结束时恰好触发一次"）的核心保证。没有此修复，游戏失败时 planner 完全不被触发，策略无法被更新，team 协作模式的核心价值（player 学习→planner 复盘→策略改进→player 改进）断裂。严重度为最高。

**Independent Test**: 可通过 fake-model + fake-tool 的单元/集成测试独立验证：构造一个"落子即输局"的 fake tool（sink 写入 lost 事件），验证 player 节点返回后 `gameEnded` 被设置为 `"lost"` 且条件边路由到 planner；构造一个"输局后 LLM 尝试重开"的场景，验证 player loop 在游戏结束时停止而非继续重开。

**Acceptance Scenarios**:

1. **Given** 一个进行中的 saolei 游戏，**When** player 落子导致游戏状态变为 `lost`，**Then** player 的内部 agent loop 在该步操作完成后立即停止（不再进行新的 model 调用或 tool 调用）。
2. **Given** player 落子导致游戏状态变为 `lost` 且 player loop 已停止，**When** player 节点的后处理运行，**Then** `gameEnded` 被设置为 `"lost"`，条件边将控制权路由到 planner。
3. **Given** player 落子导致游戏状态变为 `won`，**When** player loop 停止，**Then** 同样触发 planner 复盘（won 与 lost 行为一致）。
4. **Given** 一个输局场景中 LLM 在游戏结束后尝试调用 `saolei_init` 重开新局，**When** 游戏结束信号已写入 buffer，**Then** player loop 在下一次 model 调用前停止，`saolei_init` 不被执行。
5. **Given** player 的 agent invoke 即使因异常终止，**When** 后处理运行，**Then** 游戏结束事件仍被正确消费（不因异常导致后处理不可达）。
6. **Given** 游戏结束后 planner 复盘完成并返回，**When** 控制权回到 player，**Then** player 恢复执行（从包含游戏结束 tool 结果的消息历史重建输入，读取新策略），且 createAgent 第一次迭代正常进行 model 调用（不被前一轮的游戏结束信号阻塞）。

---

### User Story 2 - planner 复盘能看到完整游戏过程 (Priority: P2)

planner 被触发进行复盘时，其输入应包含完整的游戏过程——player 在本局中的每一步操作（工具名称、坐标）以及每步操作后的游戏状态（文本棋盘）——而不是仅看到终局棋盘快照。完整游戏过程应在 planner tab 中可见（历史加载时）。

**Why this priority**: planner 的复盘质量直接依赖能否看到完整的操作序列与中间状态。仅凭终局棋盘无法判断 player 在哪一步犯错、策略是否有效。这是策略迭代闭环质量的前提，但因 planner 至少能被触发（US1 修复后）而不完全阻塞核心流程，优先级为 P2。

**Independent Test**: 可通过单元测试独立验证：构造一个包含多次落子操作的 buffer，验证 planner 的复盘输入包含每步操作（工具名、坐标）及对应棋盘状态；验证复盘输入被写入 `plannerMessages` 通道（在 ListMessages 时可见）。

**Acceptance Scenarios**:

1. **Given** 一局游戏中 player 执行了多次落子操作，**When** planner 被触发进行复盘，**Then** planner 的复盘输入包含每一步操作的工具名称与坐标（如 `saolei_click(3,4)`）。
2. **Given** 一局游戏中的多次落子操作，**When** planner 复盘，**Then** 复盘输入包含每步操作后的棋盘状态（文本渲染），按操作先后顺序排列。
3. **Given** 游戏开始（`saolei_init`），**When** planner 复盘，**Then** 复盘输入包含游戏开始的初始棋盘状态。
4. **Given** 一局游戏的完整操作序列，**When** planner 复盘，**Then** 复盘输入以"请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy"的指令结尾。
5. **Given** planner 的复盘输入消息，**When** 通过 ListMessages 读取 planner 的消息历史，**Then** 完整游戏过程的复盘输入在 planner tab 中可见（以用户消息角色渲染文本内容）。
6. **Given** 一个没有发生任何游戏操作的会话（buffer 中无游戏日志），**When** planner 被触发，**Then** planner 收到一条说明无可用游戏记录的复盘请求（不崩溃、不产生空内容）。

---

### User Story 3 - 用户消息气泡右对齐 (Priority: P3)

在 desktop 对话页面中，用户发送的文本消息气泡应当靠右对齐显示，与 agent 消息靠左对齐形成视觉区分。当前实现中用户消息气泡出现在左侧而非右侧。

**Why this priority**: 这是一个纯视觉呈现问题，不影响功能正确性，但影响用户体验与消息归属的直观可读性。优先级最低。

**Independent Test**: 可通过前端组件测试独立验证：渲染一条用户角色文本消息，验证其气泡容器的 `justify-content` 为 `flex-end`（右对齐）。

**Acceptance Scenarios**:

1. **Given** desktop 对话页面中的一条用户文本消息，**When** 渲染时，**Then** 该消息气泡靠右对齐显示（容器 `justify-content: flex-end` 生效）。
2. **Given** 对话页面中同时存在用户消息与 agent 消息，**When** 渲染时，**Then** 用户消息靠右、agent 消息靠左，形成清晰的左右区分。
3. **Given** 一条标记为 pending（排队中）的用户消息，**When** 渲染时，**Then** 该消息气泡靠右对齐且保留 pending 的半透明视觉效果（opacity 样式不被破坏）。

---

### User Story 4 - 内部 agent loop 继承外层 graph 的递归上限 (Priority: P2)

player 和 planner 节点内部的 createAgent 在被调用时应当继承外层 team graph 的配置（特别是 `recursionLimit`），而非各自使用默认的递归上限（25）。当前实现中，节点函数仅接受 `state` 参数，不接收 LangGraph 传入的 `config`，导致内部的 `createAgent.invoke()` 不携带任何配置——内部 createAgent 以默认 `recursionLimit: 25` 运行，而外层 team graph 以 `recursionLimit: 1000` 运行。这意味着即使单局游戏需要超过 25 步 model 调用，createAgent 也会抛出 `GraphRecursionError`。

**Why this priority**: 此问题与 Issue 1 互为补充——即使 Issue 1 的 middleware 修复在游戏结束时停止了 loop，在游戏未结束的"正常进行中"阶段，单局步数仍可能超过 25 步（扫雷一局可能需要数十步操作）。不修复此问题，单局步数较多时仍会崩溃。但因与 Issue 1 独立（影响的是正常进行阶段而非游戏结束阶段），优先级为 P2。

**Independent Test**: 可通过构造一个需要超过 25 步 model 调用的 fake-model 场景，验证 player 节点不会在第 25 步抛出 `GraphRecursionError`（内部 createAgent 继承了外层 graph 的更高递归上限）。

**Acceptance Scenarios**:

1. **Given** 外层 team graph 以 `recursionLimit: 1000` 调用，**When** player 的 createAgent 内部 loop 执行超过 25 步 model 调用（游戏仍在进行中），**Then** createAgent 不抛出 `GraphRecursionError`（内部 createAgent 继承了外层 graph 的递归上限）。
2. **Given** planner 节点的 createAgent 被调用，**When** planner 内部 loop 执行，**Then** planner 的 createAgent 也继承外层 graph 的递归上限。
3. **Given** 外层 team graph 的 abort 信号（`AbortSignal`），**When** player 节点执行中收到 abort，**Then** abort 信号被传递到内部 createAgent（config 中的 `signal` 随 config 一并传递）。

---

### Edge Cases

- **player 持续落子但游戏不结束**：若一局迟迟不结束（player 陷入循环），player loop 受 createAgent 的递归上限保护（继承外层 graph 的 `recursionLimit: 1000`，而非默认 25），递归超限时 player 节点后处理仍应执行（try/finally 保障），且 `gameEnded` 为 null（无结束事件）→ 条件边路由到 END。
- **游戏结束后 planner 返回、player 恢复**：player 恢复时，前一轮的游戏结束事件已被标记为 consumed，createAgent 第一次迭代的 beforeModel 检查不触发跳转，model 正常调用。LLM 直接看到上一轮含 game-over tool result 的消息历史。
- **多局连续游戏的游戏日志**：每局游戏开始时（`onGameStart`/`saolei_init`），游戏日志应重置为本局的初始状态，而非累积多局操作（planner 仅复盘当前这一局）。
- **gameLog 为空时 planner 复盘**：若因异常导致 buffer 中无游戏日志，planner 应收到一条说明性复盘请求而非空内容或崩溃。
- **用户消息仅含图片（无文本）**：Issue 3 的对齐修复针对 ChatMessage 组件渲染的文本/thinking 消息；纯图片消息使用 ChatView 自身的 `.msg-image` 对齐逻辑，不受影响。
- **config 传递兼容性**：节点函数新增 `config` 参数后，需确保现有测试中的 fake-model 调用路径不受影响（config 的传递是附加性的，不改变现有 invoke 的消息处理逻辑）。

## Requirements *(mandatory)*

### Functional Requirements

#### Issue 1 — player loop 在游戏结束时停止并触发 planner

以下需求修复 `specs/031-team-template-mode/spec.md` FR-011（"planner MUST 在每局游戏结束时恰好触发一次"）在 lost 场景下的违背。当前 player 的 createAgent 内部 loop 在游戏结束后不自动停止，导致后处理不可达。

- **FR-001**: 当 player 的 createAgent 内部 loop 中执行的工具操作导致游戏结束（`gameEvent` 被写入 buffer 且未消费），player loop MUST 在下一次 model 调用前停止——不进行新的 model 调用或 tool 调用，使 `invoke()` 正常返回（不抛出异常）。
- **FR-002**: player loop 停止后，player 节点的后处理 MUST 可靠执行：无论 `invoke()` 正常返回还是抛出异常（含 `GraphRecursionError`），消费游戏结束事件（`consumeGameEvent`）的代码 MUST 被执行（如 try/finally 保障），确保 `gameEnded` 被正确设置。
- **FR-003**: 游戏结束触发 player loop 停止的判定 MUST 基于 buffer 中是否存在未消费的游戏结束事件（结构化信号），MUST NOT 依赖对 tool result 文本的解析（与 `specs/031-team-template-mode/spec.md` FR-017 一致）。
- **FR-004**: 游戏结束后 planner 复盘完成、控制权返回 player 时，createAgent 的首次迭代 MUST 正常进行 model 调用（前一轮的游戏结束事件已被消费标记，不阻塞新的 loop 迭代）。
- **FR-005**: won 与 lost 场景 MUST 行为一致：两种游戏结束状态都触发 player loop 停止并路由到 planner。

#### Issue 2 — planner 复盘输入包含完整游戏过程

以下需求修复 `specs/031-team-template-mode/spec.md` FR-011/FR-014 中 planner 复盘输入不完整的问题。当前 planner 仅获得终局棋盘快照。

- **FR-006**: 游戏过程中的每一步操作（游戏开始、每次落子、游戏结束）MUST 被记录为游戏日志条目，包含：工具名称、操作坐标（如适用）、操作后的棋盘状态、操作后的游戏状态（`won`/`lost`/`playing`）。
- **FR-007**: 每局游戏开始时（`saolei_init`/`onGameStart`），游戏日志 MUST 重置为本局的初始状态（push 初始条目），MUST NOT 累积上一局的日志。
- **FR-008**: planner 的复盘输入 MUST 渲染完整游戏日志：按操作先后顺序列出每一步操作（工具名称、坐标）及操作后的棋盘状态（文本渲染），并以复盘指令结尾。
- **FR-009**: 当游戏日志为空时（无操作记录），planner 的复盘输入 MUST 为一条说明无可用游戏记录的请求消息（不产生空内容）。
- **FR-010**: planner 的复盘输入消息 MUST 被写入 `plannerMessages` 通道，使其在 ListMessages（历史加载）时以用户消息角色在 planner tab 中可见。

#### Issue 3 — 用户消息气泡右对齐

以下需求修复用户消息气泡未右对齐的视觉缺陷。

- **FR-011**: 用户角色的文本/thinking 消息气泡 MUST 靠右对齐显示（ChatMessage 组件自身的 `.msg-row.msg-user`（`justify-content: flex-end`）MUST 作为对话流容器的直接 flex 子项生效，不被外层容器的默认 `flex-start` 覆盖）。
- **FR-012**: 修复 MUST NOT 破坏 pending（排队中）用户消息的半透明视觉效果（`.msg-pending` 的 `opacity` 样式仍生效）。

#### Issue 4 — 内部 createAgent 继承外层 graph 配置

以下需求修复 player 与 planner 节点的 createAgent 不继承外层 graph 配置（特别是 `recursionLimit`）的问题。当前节点函数仅接受 `state`，不接收/转发 LangGraph 的 `config`。

- **FR-013**: player 与 planner 节点函数 MUST 接受 LangGraph 传入的 `config` 参数（节点函数签名为 `(state, config?)`），并将该 `config` 传递给内部 createAgent 的 `invoke()` 调用，使内部 createAgent 继承外层 graph 的配置（含 `recursionLimit`、`signal` 等）。
- **FR-014**: 修复后，player 的 createAgent 内部 loop 在外层 graph 设置的 `recursionLimit`（当前为 1000）范围内运行，MUST NOT 使用默认的 25 递归上限。

### Key Entities *(include if feature involves data)*

- **GameLogEntry（游戏日志条目）**：记录游戏中单步操作的完整信息，包括工具名称（如 `saolei_init`/`saolei_click`）、操作坐标（x/y，如适用）、操作后的棋盘状态（`GameState`）、操作后的游戏状态（`won`/`lost`/`playing`）。一组 `GameLogEntry` 构成一局的完整游戏过程，供 planner 复盘使用。生命周期为单局（每局开始时重置），存储于 per-session ephemeral buffer 中。
- **EphemeralGameBuffer（扩展）**：在现有 `gameState` + `gameEvent` 基础上新增 `gameLog: GameLogEntry[]` 字段，由 team sink 回调写入、planner 节点读取。buffer 仍为 per-session、in-process 的普通对象（非持久化）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: saolei 模板的 team 运行一局扫雷，无论游戏以 won 还是 lost 结束，planner 都被恰好触发一次进行复盘（通过验证 `plannerMessages` 非空或策略被写入来确认）。
- **SC-002**: 在"游戏失败后 LLM 尝试重开新局"的场景中，player loop 在游戏结束时停止，不执行重开操作，不触发 `GraphRecursionError`。
- **SC-003**: planner 的复盘输入包含完整的游戏操作序列与每步棋盘状态（每一步操作的工具名称、坐标与操作后的棋盘均可验证存在），使 planner 能据此判断策略有效性。
- **SC-004**: 完整游戏过程的复盘输入在 planner tab 中可见（通过 ListMessages 读取 planner 消息历史验证）。
- **SC-005**: desktop 对话页面中用户消息气泡靠右对齐、agent 消息靠左对齐，视觉区分清晰（通过前端组件渲染验证 `justify-content` 属性生效）。
- **SC-006**: player 的 createAgent 内部 loop 在外层 graph 设置的递归上限范围内运行，单局超过 25 步操作不触发 `GraphRecursionError`（继承外层 `recursionLimit: 1000`）。
- **SC-007**: 大型测试（经 testplan skill 完整部署→测试→清理执行）覆盖游戏失败场景下 planner 的触发与策略更新，全部用例通过（宪法原则 VI）。

## Assumptions

- **本特性为 031-spec 已实现行为的缺陷修复，非新功能**：四个缺陷的根因、设计意图与修复方向已由调研文档 `specs/031-team-template-mode/bug-analysis.md`（Issue 1-3）及 plan 阶段分析（Issue 4）记录，包含 LangChain middleware 能力的源码级确认与时序分析。`plan.md` 阶段直接基于该文档设计实现方案。
- **createAgent 递归上限继承机制**：LangGraph 节点函数签名为 `(state, config?)`，其中 `config` 包含外层 graph 的 `recursionLimit`。当前 player/planner 节点函数仅接受 `state`，修复后接受 `config` 并传递给内部 `createAgent.invoke()`（bug-analysis.md 已确认内部 createAgent 默认 recursionLimit = 25）。
- **复盘输入实时流可见性为可选增强**：Issue 2 的完整游戏过程在历史加载（ListMessages）时已在 planner tab 可见，满足"在 planner tab 中可见"的需求。是否需要在实时流（`streamEvents`）中推送复盘输入为可选增强，不作为本特性的硬性要求。
- **Issue 3 影响范围仅限 ChatMessage 组件渲染路径**：纯图片消息使用 ChatView 自身的 `.msg-image` 对齐逻辑（已有 `msg-image-user` 处理右对齐），不受本次修复影响；agent markdown 文本消息使用 ChatView 自身的 `.msg-row.msg-agent`（已正确左对齐），不受影响。
- **现有测试覆盖**：031-spec 的测试基础设施（fake-model + fake-tool DI 模式，见 `projects/game/agent/src/team/graph.test.ts`）可直接复用。现有测试中的 fake player tool 在每次调用时触发 `sink.onGameEnd("won")`，需新增"输局后 LLM 尝试重开"场景的测试用例以覆盖 Issue 1。
- **Issue 4 的 config 传递是附加性的**：节点函数新增 `config` 参数后传递给 `invoke()`，不改变现有 invoke 的消息处理逻辑。现有测试中 graph.invoke 传递的 `recursionLimit: 50` 将被内部 createAgent 继承（而非使用默认 25），但现有测试的 fake-model 步数远小于 50，不受影响。
- **参考资料**：根因分析与修复方向见 `specs/031-team-template-mode/bug-analysis.md`；middleware 能力确认见 `experimental/ts/team_graph_spike/FINDINGS.md` A4；现有代码见 `projects/game/agent/src/team/{player,planner,team-sink,graph}.ts` 与 `projects/game/desktop/frontend/src/components/{ChatView,ChatMessage}.svelte`。
