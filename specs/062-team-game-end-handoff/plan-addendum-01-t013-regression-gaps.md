# Plan Addendum 01: T013 回归核对发现的缺口与补充决定

**Feature**: 062-team-game-end-handoff
**日期**: 2026-09-12
**性质**: 设计补充（执行期发现），不修改生产代码语义；全部决定为大型测试断言/注释与夹具文档的终态同步。

**输入**: [spec.md](spec.md)（FR-001/FR-002、SC-005、Assumptions"测试基建联动"、Edge Cases）、[tasks.md](tasks.md) T004/T013、[research.md](research.md) D6、[data-model.md](data-model.md) §1.2 判定矩阵、[plan.md](plan.md) Project Structure。

---

## 0. 语义基准（所有决定的判定依据）

- **FR-002 ①**（spec.md）：`saolei_init` 识别即终局的棋盘 → init 成功结果本身收束 turn——该 turn **只有 1 个 tool_result（init），无 operate 调用**（判定矩阵行 3，data-model.md §1.2）。
- **Edge Case "init 识别即终局的棋盘"**：init 不写 `gameEvent`（既有语义）→ 无复盘 → 链路静止于 player 激活。
- **SC-005 + Assumptions "测试基建联动"**：既有 team 大型测试"含因移除'脚本停手'假设而更新的断言"全量通过；fake-llm 夹具与大型测试断言**随收束语义同批更新**——这是下列全部编辑属于 062 范围（而非范围蔓延）的规范依据。
- **宪章原则 VII（终态表述）**：被 062 断言改写孤儿化的常量、描述 062 之前行为的注释，MUST 在交付物中同步为终态，不得残留。

---

## 1. 缺口 1：`agent_v2_conversation_test.go` `TestAgentV2TeamViewDataProjections`

### 现状（file:line）

- `projects/game/testplan/agent_v2_conversation_test.go:179-181` 注释："…with two settled tool calls per player game"——game 2 不再成立。
- `:221-231` 统计 player 的 tool-call blocks 并断言 `len(toolCalls) != 4`（"want 4 (init + operate per game)"）。该 flow 脚本为 `initBoards: [saoleiBoardCompatWinPNG（playing）, saoleiBoardWinPNG（terminal win）]` + `stepBoards: [saoleiBoardWinPNG]`。

### 062 下的事实链（逐项推导）

| 局 | 消耗 | tool_result | 收束点 |
|---|---|---|---|
| game 1 | initBoards[0]（compat，playing）+ stepBoards[0]（operate 批 click(0,0) 后识别 win → 批内结构性停批，第二个 op 不派发——故 stepBoards 恰 1 条） | 2（init + operate） | operate 终局结果（FR-001） |
| game 2 | initBoards[1]（win） | **1（仅 init）** | init 终局结果（FR-002 ①）；无 gameEvent → 无复盘 → 链路静止于 player |

**合计 3**（旧值 4 中 game 2 的 operate 为"对终局棋盘的结构性拒绝"，062 下该 operate 不再被请求）。注意：此错误为**运行期失败**（`t.Fatalf` 只在 guitar 实跑时触发），`bazel build` 不报警——在 T013 修正正是为了避免 T015 全量验收时的晚期红灯。

### 除计数外的全量断言核查（该测试 :162-306）

- `:190 waitTeamFlowScript`：flow 脚本消耗序 062 前后同为 init×2 + step×1（旧 game 2 的拒绝型 operate 为 pre-dispatch stop、不消耗 step board）→ **不变**。
- `:191 assertTeamStreamWellFormed` / `assertTeamMemberTurnWellFormed`：两个 player game turn 均以 settled 工具块收尾 COMPLETED，无 unsettled 调用 → **兼容**（turn 数保持 4：planner 开局、player 局 1、planner 复盘、player 局 2）。
- `:232-239` 逐 call 检查（SUCCEEDED、saolei_init/saolei_operate 名单）：3 个 call 全部满足 → **兼容**。
- `:241-289` 视角/跨视图一致性（messageId 锚定，内容无关）→ **兼容**。
- `:291-305` 第二次 Send（teamQueueMessage）到达 player：062 下链路静止于 player 激活（init 不写 gameEvent）→ **兼容**，注释 "(the current activation after the last game)" 仍准确。

### 决定

1. 注释 `:179-181` 改为（描述终态）：

   ```go
   // A won game that continues into a second one on the test's own desktop
   // half (V4): the merged sequence then carries both members' native
   // output. Game 1 settles init + operate; game 2's init already
   // recognizes the win board, so that turn concludes at the init result
   // (specs/062-team-game-end-handoff/spec.md FR-002 ①) and no operate
   // follows.
   ```

2. 断言 `:229-231` 改为：

   ```go
   if len(toolCalls) != 3 {
       t.Fatalf("merge player tool-call blocks = %d, want 3 (game 1 init + operate; game 2 init-terminal, no operate)", len(toolCalls))
   }
   ```

3. **不**追加 `assertTerminalTurnEndsWithToolBlock`（该测试关切为 List 双投影，收束形状见证由计数=3 + FR-002 ① 注释承载；避免关切蔓延，style/large_test.md §测试组织）。

**范围论证**: SC-005"因移除'脚本停手'假设而更新的断言"的直接实例；spec Edge Cases"init 识别即终局的棋盘"明文"按 FR-002 在 init 结果处收束"。
**归属 task**: T013 修订（见 §6）。
**验证**: `bazel build //projects/game/testplan:agent_v2_conversation_test`（编译面）；计数语义的实跑验证由 T015 `guitar run` 承载。

---

## 2. 缺口 2：`agent_v2_helpers_test.go` `agentV2WonRejectContains` 常量

### 现状（已 grep 确认）

- 声明：`projects/game/testplan/agent_v2_helpers_test.go:126`（`"stopped at click(0,0) (game_won)"`）。
- **全仓库零引用**（Go 未用常量不报编译错，属死代码）。
- Provenance（git 核实）：HEAD 上仅 `agent_v2_game_test.go:75`（WonChain 旧 operate 拒绝断言）与 `:189-190`（TerminalWon game 2 旧 operate 拒绝断言）引用——两处均被 062 Phase 3/4 的断言改写（T006/T008）移除。即：**该常量是被 062 自身改写孤儿化的**。
- `message_store_test.go` lockstep 经嵌入式 store 的规则对象锚定（如 `teamPlannerReviewContinue.HistoryKeywords`，`message_store_test.go:774-785`），**不经过** helpers 常量 → 移除不影响 lockstep。

### 决定：移除

- **FR-002 ②（判定矩阵行 9）大型测试覆盖现状论证**：062 语义下"对终局棋盘的 operate 结构性拒绝"在大型测试中**不可达**——所有涉及终局棋盘的拓扑（WonChain、TerminalWon game 2、MultiSessionIsolation connected、DesktopFlow）均在 init/operate 终局结果处收束 turn，模型不再被调用、无从发起对终局棋盘的 operate。MultiSessionIsolation **不使用**该文本（已读全文核实：connected 会话仅断言 ≥1 个 SUCCEEDED init）。该场景的验收面由 **SC-004 单测矩阵行 9** 承载（`runtime.test.ts` 断言拒绝结果携带收束标记，T002 已实现）——符合 spec 对 FR-002 ② 的验收划分。
- 保留一个零引用常量违反原则 VII（迭代残留）；其锚定的文本在大型测试中已无任何用例消费。

**编辑**: 删除 `agent_v2_helpers_test.go:126` 一行。剩余 4 常量对齐列由最长名 `agentV2LostStatusContains` 决定，gofmt 无连带变化。
**范围论证**: 消费者由 062 断言改写移除（SC-005 同批更新义务）；原则 VII。
**归属 task**: T013 修订（helpers 编入其文件清单）。
**验证**: `bazel build //projects/game/testplan:agent_v2_game_test //projects/game/testplan:agent_v2_conversation_test //projects/game/testplan:agent_v2_preset_test //projects/game/testplan:agent_v2_game_disconnect_test //projects/game/testplan:desktop_flow_test //projects/game/testplan:agent_v2_memory_down_test`（helpers 被全部六个 target 编入；未用常量本不致编译错，全量编译为防未知引用的廉价保险）。

---

## 3. 缺口 3：`desktop_flow_test.go` `TestDesktopFlowOperationDeliveryAndReceipt`

### 现状（:104-132）

- 注释 `:106-107`："The win board at init: the operate batch rejects pre-dispatch, so the single F2 reply closes the chain."——描述 062 前行为（init 即胜利棋盘后模型仍发起 operate 批、被 runtime pre-dispatch 拒绝）。062 下 init 终局结果收束 turn，**operate 批不再被请求**（拒绝路径根本不发生）。
- 断言 `:121-124`：`len(results) == 0` 即 fatal，随后仅检查 `results[0]`（init SUCCEEDED + win board 文本）——**兼容但无见证力**：若收束机制回归（operate 批被再次请求 → `game_won` pre-dispatch 拒绝成为第二个 SUCCEEDED result），现状断言静默通过。

### 决定

1. 注释 `:106-107` 改为：

   ```go
   // The win board at init: the terminal init result concludes the player
   // turn (specs/062-team-game-end-handoff/spec.md FR-002 ①), so the
   // scripted operate batch is never requested — the single F2 reply closes
   // the chain.
   ```

2. 断言 `:121-124` 收紧（对齐 WonChain 先例 `agent_v2_game_test.go:66-69`，T008）：

   ```go
   results := teamTurnToolResults(playerTurns[0])
   if len(results) != 1 {
       t.Fatalf("tool_result count = %d, want 1 (saolei_init only — the terminal init result concludes the turn)", len(results))
   }
   ```

3. 其余断言（player turns = 1、init SUCCEEDED、win board 文本、flow script 恰一条 init 消耗）不变。

**范围论证**: desktop_flow_test.go 本应列入 T013 清单而遗漏（plan.md Project Structure 亦未列）；注释描述的是 062 之前的链路，属"测试基建联动"终态同步；断言收紧是 SC-005"更新的断言"在该拓扑上的最小见证（与 T008 同型）。
**归属 task**: T013 修订（补入该文件）。
**验证**: `bazel build //projects/game/testplan:desktop_flow_test`。

---

## 4. 缺口 4：`agent_v2_game_test.go` `TestAgentV2TeamGameMultiSessionIsolation`

### 现状（:357-434）

- 注释 `:369-370`："The win board at init: the operate batch is rejected pre-dispatch, so one F2 reply completes the chain."——同缺口 3，描述 062 前行为。
- 断言 `:405-408`：`len(connectedResults) == 0 || connectedResults[0] != SUCCEEDED`——兼容但无见证力（理由同缺口 3；absent 会话的 FAILED init 为 isError 不收束面，**保持原样不收紧**——非 062 语义，避免无意义 churn）。
- 其余断言（turn id 隔离、history marker 隔离、well-formed）经核查与 062 语义无交互：connected 会话链 = planner 开局 + player 单 turn（init 收束、无 gameEvent、无复盘、静止于 player）。

### 决定

1. 注释 `:369-370` 改为：

   ```go
   // The win board at init: the terminal init result concludes the player
   // turn (specs/062-team-game-end-handoff/spec.md FR-002 ①), so the
   // scripted operate batch is never requested — one F2 reply completes the
   // chain.
   ```

2. 断言 `:405-408` 收紧：

   ```go
   connectedResults := teamTurnToolResults(teamTurnsForMember(eventsConnected, "player")[0])
   if len(connectedResults) != 1 || connectedResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
       t.Fatalf("connected session tool results = %+v, want the single SUCCEEDED saolei_init (the terminal init concludes the turn)", connectedResults)
   }
   ```

**范围论证**: tasks.md T013 原文"1 个 SUCCEEDED init 结果断言兼容"仅对了一半——断言兼容但注释过时且无见证力；SC-005 同批更新义务。
**归属 task**: T013 修订（订正其对 MultiSessionIsolation 的"断言兼容"表述）。
**验证**: `bazel build //projects/game/testplan:agent_v2_game_test`。

---

## 5. 追加扫描结果（要求 3：同类遗漏核查）

### 5.1 已核查、确认零改动（T013 保留为核对项）

| 位置 | 核查结论 |
|---|---|
| `agent_v2_game_disconnect_test.go:55-56,104-105`（"want 2 (init + operate)"）与 `:66,113`（`agentV2ProgSummaryText`） | progressive 链全程 **playing**（operate 后仍 playing → 不收束 → 模型继续 → 总结文本照常）——正是"playing 不收束 + 自然停手"的回归面，断言终态正确 |
| `agent_v2_preset_test.go:635-636,782-783`（两处 "want 4 (opening, game, memory review, stop ack)"） | lost 拓扑（init playing + operate 致 lost）：game turn 由 2 个 tool_result + 旧总结文本变为 2 个 tool_result 收尾，但两测试**均不断言 game turn 形态**（只断言 turns 计数、turns[2] memory add、turns[3] stop ack）→ 兼容；4-turn 链形状 062 下保持 |
| `agent_v2_game_test.go:142,266,591`（"want 4"turn 链） | 已为 062 终态（Phase 3/4 改写产物）：4-turn 链形状不变，仅 player game turn 内部形态变化，断言已相应更新 |
| `saolei_fixtures_test.go:28-33,38-45`（"any following cell op is rejected pre-dispatch as game_won/game_over"） | 描述 **runtime 结构性拒绝能力**（fixture 语义层），062 下该能力仍存在且为 FR-002 ② 收束触发点——陈述仍准确，不改 |
| `agent_v2_memory_down_test.go` / `memory_test.go` / `web_test` | 无 game 链拓扑（物化失败/v1 面），与 062 无交互 |
| `message_store_test.go` lockstep | 经嵌入式 store 规则对象锚定，不经过 helpers 常量（缺口 2 移除安全） |
| `agent_v2_saolei_tools.yaml` / `team_player.yaml` / `team_planner.yaml` | 已按 T004/零改动要求处于 062 终态（工作区已核） |

### 5.2 追加发现的同步项（同类遗漏，随批修正）

- **A. `projects/game/fake-llm/service/testdata/agent_v2_saolei.yaml:3-5`** 头部"…chain through init → operate → terminal summary"——链尾描述过时（062 下 won/lost 链终止于终局工具块，总结规则为"已脚本化不执行"零执行断言面）。将该句改为："…chain through init → operate; a terminal result concludes the turn at the tool block (specs/062-team-game-end-handoff/spec.md FR-001/FR-002), so the scripted terminal summaries are never requested."。**归属 T004 修订**（补入该文件；同为 fake-llm 夹具注释同步）。
- **B. `projects/game/testplan/README.md:149-153`**"…and the `game status: won` result resolves to the final summary text"——同上过时。将该句改为："…to drive the operate batch, and a terminal (`game status: won/lost`) result would resolve to the final summary text — under the 062 turn conclusion such a result instead ends the player turn at the tool block, so those summary rules stay as the never-requested zero-execution face (specs/062-team-game-end-handoff/research.md D6)."。**归属 T013 修订**。
- **C. `specs/062-team-game-end-handoff/plan.md:87-88`**"agent_v2_game_disconnect_test.go / agent_v2_conversation_test.go / agent_v2_preset_test.go …仅核对，预计零改动"——被缺口 1 证伪（conversation 需改）；修订 tasks.md 时同步将该行改为"disconnect/preset 仅核对零改动（playing 拓扑）；conversation 的 ViewDataProjections 计数 4→3 与注释同步（T013）"，并可在 fixture 清单（:78-80）补 `agent_v2_saolei.yaml` 头部一行。

### 5.3 范围外观察（明确不在 062 内处理）

- `agent_v2_helpers_test.go:139` `agentV2DisconnectSummary`（"桌面连接中断…"）在 **HEAD 即零引用**（git 核实：非 062 改写所致的死代码；其对应规则 `agent-v2-saolei-operate-nodesktop` 为休眠规则）。**不随 062 移除**——避免范围蔓延；建议记入独立的测试基建清理事项。

---

## 6. tasks.md 归属决定与修订文本

**总原则**: 全部编辑折叠进 **T013 的描述修订** + **T004 的文件清单增补**，**不新增 task ID**——这些编辑都是 T013 回归核对动作的直接产物，独立 sub-task 会碎片化微小变更且打断 T012→T013→T014→T015 依赖链。两 task 均未勾选，修订后勾选时描述即反映最终实际工作（原则 VII 终态一致性）。

### T004 修订（文件清单增补一项）

在 `projects/game/fake-llm/service/testdata/team_player.yaml` 头部同步之后追加：
"`projects/game/fake-llm/service/testdata/agent_v2_saolei.yaml` 头部链尾描述同步（init → operate → 终局结果收束、总结规则为零执行断言面）"。验证不变（`bazel test //projects/game/fake-llm/service/...`）。

### T013 替换文本（建议整条替换）

```markdown
- [ ] T013 [US4] 回归核对与同步修正（SC-005；核对发现的 062 语义联动随批修正，
  依据 specs/062-team-game-end-handoff/plan-addendum-01-t013-regression-gaps.md；
  同 step 多工具组不新增断言——由 dsh 原生调度保证，见 spec.md Assumptions）：
  1. `projects/game/testplan/agent_v2_conversation_test.go` 的
     `TestAgentV2TeamViewDataProjections`：merge player tool-call 计数 4 → 3
     （game 2 init 即终局收束、无 operate，FR-002 ①）+ :179-181 脚本拓扑注释同步；
  2. `projects/game/testplan/desktop_flow_test.go` 的
     `TestDesktopFlowOperationDeliveryAndReceipt`：:106-107 注释改述"终局 init 结果
     收束 turn、operate 批不再被请求"（FR-002 ①）+ tool_result 断言收紧为恰 1 个
     （对齐 WonChain 先例）；
  3. `projects/game/testplan/agent_v2_game_test.go` 的
     `TestAgentV2TeamGameMultiSessionIsolation`：:369-370 注释同步（同 2）+ connected
     会话 tool_result 断言收紧为恰 1 个；
  4. `projects/game/testplan/agent_v2_helpers_test.go`：移除孤儿常量
     `agentV2WonRejectContains`（断言改写后零引用；FR-002 ② 场景大型测试不可达、
     由 SC-004 单测矩阵行 9 承载）；
  5. `projects/game/testplan/README.md` §fake-llm fixtures 段链尾描述同步（终局结果
     收束、总结规则为 never-requested 零执行断言面，research.md D6）;
  6. 零改动确认项（逐项核对记录）：`TestAgentV2TeamGameDesktopAbsent`（isError 不收束
     → nodesktop 总结文本保持）、`agent_v2_game_disconnect_test.go`（progressive 全程
     playing）、`agent_v2_preset_test.go` 两处 4-turn 链（lost 拓扑，game turn 形态不在
     断言面）、`projects/game/fake-llm/service/message_store_test.go` lockstep（经嵌入式
     store 对象锚定，不经过 helpers 常量）；
  验证：`bazel build //projects/game/testplan:agent_v2_conversation_test //projects/game/testplan:agent_v2_game_test //projects/game/testplan:agent_v2_game_disconnect_test //projects/game/testplan:agent_v2_preset_test //projects/game/testplan:desktop_flow_test` 通过
  （helpers 编入全部 agent_v2 target，上述集合覆盖缺口 2 的编译面；agent_v2_memory_down_test
  可一并构建作廉价保险）
```

**附带调整**: T013 摘除 `[P]` 标记——其第 3 项编辑 `agent_v2_game_test.go`（MultiSessionIsolation 区域），与 T012（ActiveMemberTransitions 区域）虽不同函数但同文件，按本 tasks.md Notes 的同文件串行纪律执行；Dependencies 一节的 Parallel Opportunities 中涉及 T013 的表述随之删除。

---

## 7. 明确否决的备选（防止执行期回潮）

| 备选 | 否决理由 |
|---|---|
| `ViewDataProjections` 追加 `assertTerminalTurnEndsWithToolBlock` | 关切分离（List 投影 vs turn 形状）；计数=3 + FR-002 ① 注释已是充分见证 |
| 收紧 MultiSessionIsolation 的 `absentResults` 为恰 1 | isError 面（FR-001 失败不收束）非 062 新语义，收束回归不会改变其值，纯 churn |
| 随批移除 `agentV2DisconnectSummary` | HEAD 即死、非 062 造成——范围蔓延（原则 II 简化边界） |
| 新增独立 sub-task 承载缺口修正 | 微小编辑碎片化；T013 即其发现来源，折叠保持依赖链与 review 粒度 |
| 保留 `agentV2WonRejectContains` 加注释说明 | 零引用常量 + 注释无法进入任何断言面，违反原则 VII 且误导后续读者 |

---

## 8. 执行顺序与验证门禁

1. T004 增补项（agent_v2_saolei.yaml 注释）→ `bazel test //projects/game/fake-llm/service/...` 全绿。
2. T013 修订版（1-6 项）→ 上述 bazel build 全绿；Go 文件格式化归 T014（`bazel run //:go -- fmt`）。
3. 终态验收归 T015：`guitar run projects/game/testplan/system_test.yaml` 全量全绿（缺口 1 的计数 3、缺口 3/4 的恰 1 断言在此获得实跑证明；宪章原则 VI）。
