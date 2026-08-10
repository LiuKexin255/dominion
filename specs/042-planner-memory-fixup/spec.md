# Feature Specification: planner 记忆校准实现修复 (Planner Memory Calibration Fixups)

**Feature Branch**: `042-planner-memory-fixup`

**Created**: 2026-08-10

**Status**: Draft

**Input**: User description: "对 specs/039-planner-memory-calibration/ 实现有一些问题：1) saolei_operate 停下时，提示内容用具体的操作参数替换第几个操作；2) init instruction 可以正常在 player 页面显示，但后续正常的 instruction 内容不会显示在 player 的对话列表里。但我看到 player 回复他应该是看到了指令内容；3) refresh team 之后应该与 init team 一样执行 init instruction。"

**Related**: [`specs/039-planner-memory-calibration/spec.md`](../039-planner-memory-calibration/spec.md)（本特性为其实现修复）

## User Scenarios & Testing *(mandatory)*

### User Story 1 - saolei_operate 停止提示显示具体操作参数 (Priority: P1)

`saolei_operate`（[`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`](../039-planner-memory-calibration/contracts/saolei-operate-contract.md)）在批量操作中途因游戏结束或结构性原因停止时，当前返回的结果行格式为 `saolei_operate → stopped at op K (reason)`，其中 K 是操作在列表中的序号（第几个操作）。对于 LLM 而言，序号本身缺乏语义——它需要回溯自己的 operations 列表才能定位是哪个具体操作触发了停止。本故事要求停止提示用**导致停止的具体操作的参数**（操作类型 + 坐标）替换序号，使 LLM 一眼即可识别是哪个操作被拒绝或触发了终局。

**Why this priority**: 这是一个影响 player LLM 决策质量的可用性问题。当 player 在批量操作中遇到停止（如某操作踩雷导致 game over），序号提示无法帮助 LLM 快速理解"是哪个操作出了问题"，而具体操作参数（如 `click(4,4)`）能让 LLM 立即判断后续策略。它独立于其他两个修复，改动范围最小（仅 `operateResultText` 的结果行格式），可独立验证与交付。

**Independent Test**: 可独立验证：调用 `saolei_operate` 传入一个有序操作列表，使其中某个操作触发停止（游戏结束或越界等结构性拒绝），检查返回的单次结果行中是否包含该停止操作的具体参数（类型 + 坐标）而非仅有序号。正常完成（无停止）和跳过（无害空操作）的结果行格式不变。

**Acceptance Scenarios**:

1. **Given** 一个已开局的游戏，**When** player 调用 `saolei_operate` 传入一个批量操作列表，其中第 K 个操作使游戏以 won/lost 结束，**Then** 单次结果的停止行显示导致结束的**具体操作的参数**（操作类型 + 坐标），而非仅显示序号 K。
2. **Given** 一个已开局的游戏，**When** player 调用 `saolei_operate` 传入一个批量操作列表，其中第 K 个操作因结构性原因被拒（如 `out_of_bounds`），**Then** 单次结果的停止行显示该**具体操作的参数**（操作类型 + 坐标）与拒绝原因。
3. **Given** `saolei_operate` 正常完成全部操作（无停止），**When** 审查返回结果行，**Then** 结果行格式不变（如 `executed N ops`，可含 `skipped S no-op ops`），停止行不存在。
4. **Given** `saolei_operate` 以单次参数形态调用（普通参数 `type`/`x`/`y`）且该操作触发停止，**When** 审查返回结果行，**Then** 停止行同样显示该操作的参数（与批量形态一致）。
5. **Given** `saolei_operate` 批量操作触发停止，**When** 审查同步给 planner 的 gameLog，**Then** gameLog 仍以一次调用为单位记录一条含全部操作的历史项，记录格式不变（FR-004）。

---

### User Story 2 - 正常复盘指令在 player 对话列表可见 (Priority: P1)

在 039 实现的 planner→player 校准指令投递中，`team 初始化`（init instruction）场景的指令可以正常在 desktop player 页面的对话列表中**实时显示**——因为 init 指令节点（`instruction-node.ts`）在将指令写入 `playerMessages` 通道时同时发射了实时显示帧。然而，**正常游戏结束复盘**场景（review 节点，`planner.ts`）的指令却没有在 player 对话列表中实时显示。指令确实被写入了 checkpoint（`playerMessages` 通道），player LLM 在下次激活时能读到它（player 的回复内容证实它看到了指令），但 desktop 的 player 页面对话列表里看不到这条指令，只有重新加载页面（触发 `ListMessages`）才会出现。

本故事要求：正常复盘场景下 planner 经 `instruct_player` 工具发送的校准指令，**MUST** 像 init 指令一样在 desktop player 页面对话列表中实时可见（与 init/compact 场景的指令投递行为一致，[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §4 / FR-017 消息顺序要求指令作为 player 对话流的一部分、可累积、可引用）。

**Why this priority**: 这是对指令投递核心用户体验的直接破坏。指令的全部价值在于 player 能"看到"它并据此调整行为；如果指令只在 checkpoint 中存在而 player 页面对话列表看不到，则用户无法追踪 planner 对 player 的校准历史，desktop player 页面与实际对话状态不一致。init 指令能显示但 review 指令不能显示的行为不一致也是架构缺陷。

**Independent Test**: 可在具备 desktop（或桩 desktop）的环境中独立验证：让 planner 在正常复盘后调用 `instruct_player` 发送指令，观察 desktop player 页面对话列表是否在不重新加载的情况下实时出现该指令；对比 init 指令的显示行为，两者应一致。

**Acceptance Scenarios**:

1. **Given** planner 完成一次正常游戏结束复盘（未触发压缩）并决定发送校准指令，**When** 指令被写入 `playerMessages` 通道，**Then** desktop player 页面对话列表**实时显示**该指令（无需重新加载页面），行为与 init/compact 场景的指令投递一致。
2. **Given** player 通道（`playerMessages`）的消息顺序，**When** 正常复盘指令被发送，**Then** 该指令在 player 对话列表中的位置紧跟游戏结束的 tool_result 之后、player 下一条消息输出之前（[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §2.2 / FR-017 的消息顺序不变）。
3. **Given** init/compact 场景与 review 场景的指令投递，**When** 对比两者在 desktop player 页面的显示行为，**Then** 两者行为一致（均在发送后实时出现在 player 对话列表中）。

---

### User Story 3 - RefreshTeam 后重新产出初始指令 (Priority: P1)

[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §7 定义了 RefreshTeam 的行为：清空 `playerMessages`/`plannerMessages` 两个短期消息通道、重置 `gameCounter`，清除通道中残留的 init/compact 指令。这意味着 RefreshTeam 后 player 的对话历史（包含此前收到的所有 planner 校准指令）被清空——player 处于与 team 初始化时**完全相同**的"无指令历史"状态。

然而，当前的 RefreshTeam 在清空通道后**不会**重新触发指令产出。这与 team 初始化的行为不一致：team 初始化时（`SessionTeamStore.update` 首次物化），planner 会异步产出一次无游戏历史的初始校准指令（FR-015 / contract §6），帮助 player 在无历史的情况下开始。RefreshTeam 后 player 同样处于无指令历史的状态，却得不到新的初始指令引导。

本故事要求：RefreshTeam 在清空短期消息通道后，MUST **像 team 初始化一样**触发一次无游戏历史的初始指令产出（planner 经 prompt 引导产出，LLM 决定是否调用 `instruct_player`，FR-015 语义），使 player 在清空后的首次激活时获得新的校准指令。

**Why this priority**: 这是 team"重新开始"语义的完整性问题。RefreshTeam 的语义是清除短期记忆、让团队"重新开始"；如果初始化会给 player 初始指令但刷新后不给，则刷新后的 player 在无引导状态下开始，体验退化。与 init 行为对齐使 RefreshTeam 成为真正的"重置到初始状态"操作。

**Independent Test**: 可独立验证：对一个已有对话历史的 session 执行 RefreshTeam，随后观察 planner 是否产出了新的初始校准指令（无游戏历史），以及该指令是否进入 player 对话列表并在 player 首次激活时可见。

**Acceptance Scenarios**:

1. **Given** 一个已有对话历史（含此前指令）的 session，**When** 执行 RefreshTeam（清空短期消息通道），**Then** RefreshTeam 后系统触发一次无游戏历史的初始指令产出（与 team 初始化行为一致），指令进入 `playerMessages` 通道并在 player 首次激活时可见。
2. **Given** RefreshTeam 触发的初始指令产出，**When** 该指令产出正在异步运行期间，**Then** 期间到达的 user message 排在指令之后（与 FR-015 的排序语义一致），且 desktop 正确显示 planner 工作中的状态。
3. **Given** RefreshTeam 触发的初始指令产出失败（如 planner model 不可用），**When** 该产出失败，**Then** 降级行为与 team 初始化的 init instruction 降级一致（记日志、跳过指令，不阻断 RefreshTeam 完成）。
4. **Given** RefreshTeam 的前置条件（turn in-flight 守卫），**When** 有 turn 正在运行时调用 RefreshTeam，**Then** 仍然被拒绝（FAILED_PRECONDITION，既有行为不变）；RefreshTeam 触发的指令产出期间再次调用 RefreshTeam 同样应被守卫拒绝。
5. **Given** 连续两次 RefreshTeam（每次均完成后），**When** 每次执行后检查指令产出，**Then** 每次均触发一次新的无游戏历史初始指令产出（FR-013，非一次性，区别于 team 初始化的 init instruction 仅触发一次）。
6. **Given** RefreshTeam 触发的初始指令产出正在运行，**When** 检查 desktop 显示状态，**Then** planner 工作状态正确反映（FR-011：与 team 初始化行为一致——`isRunning()` 为 false 不驱动 typing indicator，planner 活动经实时帧显示）。

---

### Edge Cases

- **停止操作参数的格式**：停止行中具体操作参数的呈现格式（如 `click(4,4)` / `{type:"click", x:4, y:4}`）属实现细节，本 spec 约束核心信息完整（操作类型 + 坐标均可识别），精确措辞由 plan 决定。
- **单次形态的停止行**：`saolei_operate` 以普通参数（单次操作）调用时触发的停止，其停止行同样应包含该操作的参数——单次操作参数即为停止操作参数，格式与批量一致。
- **review 指令实时显示与 player 仍在运行的关系**：review 指令发生在 player 的 createAgent 已因 game-end 停止之后、graph 路由回 player 之前，此时 player 暂未重新激活。实时显示帧在指令写入通道时发射，player 下次激活时既从 checkpoint 读到指令、也在对话列表中看到它——二者一致（无重复）。
- **RefreshTeam 指令产出与 init 指令产出的复用关系**：RefreshTeam 触发的指令产出在语义上与 team 初始化的 init instruction 完全一致（无游戏历史、prompt 引导、LLM 决定是否调用工具、不触发 player invoke、异步产出）。实现上是否复用同一节点逻辑（`initInstruction` 节点）由 plan 决定；本 spec 约束行为结果一致。
- **连续 RefreshTeam**：多次连续调用 RefreshTeam（每次均完成后），每次都应触发一次指令产出（非一次性）。但指令产出期间（in-flight）的再次 RefreshTeam 应被守卫拒绝。
- **RefreshTeam 指令产出期间的 user message 排序**：与 team 初始化的 init instruction 语义一致——异步产出期间到达的 user message 须排在指令之后（player 首次激活时先读指令、再处理 user message）。

## Requirements *(mandatory)*

### Functional Requirements

#### saolei_operate 停止提示显示具体操作参数

- **FR-001**: `saolei_operate` 在批量操作中途因游戏结束（won/lost）或结构性/上下文拒绝而停止时，返回的单次结果行 MUST 包含**导致停止的具体操作的参数**（操作类型 + 坐标），而非仅显示该操作在列表中的序号。MUST 使 LLM 无需回溯 operations 列表即可识别是哪个操作触发了停止。
- **FR-002**: FR-001 的停止行格式 MUST 对单次形态（普通参数 `type`/`x`/`y`）与批量形态（`operations` 数组）的调用保持一致——单次形态触发停止时同样显示该操作的参数。
- **FR-003**: `saolei_operate` 的非停止结果行（正常完成 `executed N ops`、含跳过 `skipped S no-op ops`）格式 MUST 保持不变。仅停止行的信息发生变化（用具体操作参数替换序号）。
- **FR-004**: 同步给 planner 的游戏历史（gameLog）中 `saolei_operate` 操作的记录格式 MUST NOT 因本变更而改变——gameLog 仍以一次 `saolei_operate` 调用为单位记录一条含全部操作的历史项（[`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`](../039-planner-memory-calibration/contracts/saolei-operate-contract.md) §4 / FR-004 不变）。

#### 正常复盘指令在 player 对话列表实时可见

- **FR-005**: 正常游戏结束复盘场景（review 节点）下，planner 经 `instruct_player` 工具发送的校准指令被写入 `playerMessages` 通道时，系统 MUST 发射实时显示帧使指令在 desktop player 页面对话列表中**实时可见**（无需重新加载页面），行为与 init/compact 场景的指令投递一致。
- **FR-006**: FR-005 的实时显示 MUST NOT 改变既有消息顺序语义（FR-017：指令在 player 通道紧跟游戏结束 tool_result 之后、player 下一条消息输出之前）。实时显示帧与 checkpoint 中持久化的指令应为同一条消息（避免重复显示）。
- **FR-007**: init/compact 场景（instruction-node）与 review 场景（planner）的指令投递在 desktop player 页面的实时显示行为 MUST 一致——此为 FR-005 的验收对比断言（US2 acceptance scenario 3），不引入独立的实现要求；若实现层面无法区分两场景的显示路径，则两者自动一致。

#### RefreshTeam 后重新产出初始指令

- **FR-008**: RefreshTeam 在清空短期消息通道后，MUST 触发一次无游戏历史的初始指令产出——与 team 初始化的 init instruction（[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §6 / FR-015）行为一致：planner 经 prompt 引导产出初始校准指令（LLM 决定是否调用 `instruct_player`），指令进入 `playerMessages` 通道，不触发 player invoke（随 player 首次激活一同注入）。
- **FR-009**: FR-008 的初始指令产出 MUST 为**异步**执行（RefreshTeam 清空通道后即返回，不等 LLM），与 team 初始化的 init instruction 异步语义（[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §6 / R2）一致。异步产出期间到达的 user message MUST 排在指令之后（player 首次激活时先注入指令、再处理 user message）。
- **FR-010**: FR-008 的初始指令产出失败时（如 planner model 不可用），MUST 降级为记日志、跳过指令，MUST NOT 阻断 RefreshTeam 的完成（与 team 初始化的 init instruction 降级一致，[`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`](../039-planner-memory-calibration/contracts/team-graph-contract.md) §6）。
- **FR-011**: RefreshTeam 触发的初始指令产出 MUST 在 desktop 上正确反映 planner 工作状态（与 team 初始化的 init instruction 行为一致）。具体状态同步机制（typing-state / 实时帧）由 plan 决定，约束为与 init instruction 场景行为一致。
- **FR-012**: RefreshTeam 的既有守卫（turn in-flight 时拒绝、FAILED_PRECONDITION）MUST 保持不变。RefreshTeam 触发的指令产出期间（in-flight），再次调用 RefreshTeam 或 profile-change rebuild MUST 同样被守卫拒绝（与既有 `isBusy` 守卫语义一致）。
- **FR-013**: 多次连续调用 RefreshTeam（每次均完成后），每次 MUST 都触发一次指令产出（非一次性，区别于 team 初始化的 init instruction 仅触发一次）。

#### 大型测试（验收）

- **FR-014**: 本特性 MUST 提供大型测试覆盖（经 testplan skill 执行，宪法原则 VI），至少包含：`saolei_operate` 停止行含具体操作参数（游戏结束停止 / 结构性拒绝停止）；正常复盘指令在 player 对话列表实时可见（对比 init 指令显示行为一致）；RefreshTeam 后初始指令产出（指令进入 playerMessages、异步产出期间 user message 排序、降级行为、连续 RefreshTeam 每次均产出）。验收标准为所有测试用例全部通过。

### Key Entities

- **`saolei_operate` 停止行（stopped outcome line）**：`saolei_operate` 在批量操作中途停止时返回的结果行。当前格式含停止操作的序号（`stopped at op K`），本特性要求改为含具体操作参数（类型 + 坐标）。定义于 [`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`](../../projects/game/agent/src/mcp/saolei/saolei-mcp.ts) `operateResultText`。
- **review 场景指令实时显示帧（review instruction display frame）**：正常复盘场景下 planner 经 `instruct_player` 发送指令时，随指令写入 `playerMessages` 通道发射的实时显示帧。当前 review 节点（[`projects/game/agent/src/team/planner.ts`](../../projects/game/agent/src/team/planner.ts)）缺少此帧发射（init/compact 节点 [`projects/game/agent/src/team/instruction-node.ts`](../../projects/game/agent/src/team/instruction-node.ts) 有），导致 review 指令在 player 页面不实时显示。
- **RefreshTeam 初始指令产出（post-refresh instruction）**：RefreshTeam 清空短期消息通道后触发的一次无游戏历史初始指令产出。语义与 team 初始化的 init instruction（FR-015）完全一致，区别在于可多次触发（每次 RefreshTeam 后均产出，而非仅首次物化时一次性触发）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `saolei_operate` 因游戏结束或结构性原因停止时，返回的结果行包含导致停止的具体操作参数（类型 + 坐标），LLM 无需回溯 operations 列表即可定位问题操作。
- **SC-002**: 正常复盘场景下 planner 发送的校准指令在 desktop player 页面对话列表中实时可见（无需重新加载），与 init/compact 场景的指令显示行为一致。
- **SC-003**: RefreshTeam 在清空短期消息通道后触发一次无游戏历史初始指令产出，player 在清空后的首次激活时获得新的校准指令；行为与 team 初始化的 init instruction 一致（异步产出、user message 排序、降级）。
- **SC-004**: 大型测试（经 testplan skill 完整部署→测试→清理执行）全部用例通过，覆盖 SC-001/SC-002/SC-003 所述行为。

## Assumptions

- **基于 039 既有实现修复**：本特性是对 [`specs/039-planner-memory-calibration/`](../039-planner-memory-calibration/) 已落地实现的修复，不改变 039 的核心架构与契约（memory 服务、冻结快照、指令投递两场景节点、saolei_operate 双形态），仅修正三个实现缺陷。
- **既有实时显示帧机制可复用**：desktop 的实时显示帧发射机制（`emitChannelFrame` / `ChannelFrameEmitter`）已由 init/compact 场景（[`specs/041-realtime-init-push/`](../041-realtime-init-push/)）验证可用；review 场景的指令实时显示复用同一机制（而非新建设计）。
- **既有 initInstruction 节点逻辑可复用**：RefreshTeam 触发的指令产出在语义上与 team 初始化的 init instruction 完全一致，实现上可复用 `initInstruction` 节点（`instruction-node.ts`）与 `runInitTurn` 图执行逻辑；具体复用方式（提取公共方法 vs 新触发点）由 plan 决定。
- **不影响 039 既有大型测试**：本特性的修复应使 039 既有大型测试在更新断言后继续通过（如停止行格式断言更新），不引入回归。
