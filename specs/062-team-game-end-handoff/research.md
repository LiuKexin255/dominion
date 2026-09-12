# Research: 062-team-game-end-handoff

**输入**: [spec.md](spec.md)（含 Session 2026-09-12 裁定与终局处理流程总览）、[research-notes.md](research-notes.md)（clarify 阶段源码级调研草稿）。
本文为 plan 阶段正式调研结论；全部源码结论基于本地物化 `@deepseek-ai/dsh-agent-loop@0.1.1-rc.2` / `@deepseek-ai/dsh-tools@0.1.1-rc.2` 与仓库现状逐行核对（核对动作与行号见 research-notes.md R1–R6 及本文各条）。

---

## D1. 收束 seam 选型：工具体 `ToolRunContext.concludeTurn()`

**Decision**: 经 dsh-tools 官方工具层 seam 收束——工具体在终局成功结果返回前调用 `exec.concludeTurn()`，标记经 `ToolExecutionSuccess.concludesTurn` 携带，由 dsh-agent-loop 聚合消费（`runGroup.commitReady` 在 durable `tool/result` 提交**之后**执行 `concluded ||= result.concludesTurn === true`，agent-loop `lib/index.js:176-187`）。

**Rationale**: 唯一同时满足"自然收束 + 无痕"的官方路径：`step()` 返回 `{kind:"completed"}`（`lib/index.js:685-686`）→ turn/end reason `completed`（`:590-598`，与纯文本自然停手同因）；`concludesTurn` 是执行局部信号，**不落 durable 事件**（`appendToolResult` 只持久化 content/isError/error/info/meta，`:302-318`）——日志与自然停手不可区分（FR-003 无痕性的机制基础）。失败结果类型上不可携带（`ToolExecutionFailure.concludesTurn?: never`，dsh-tools `index.d.ts:400-409`），与 FR-001"isError MUST NOT 收束"天然对齐。

**Alternatives considered**（均劣于上述，证据链见 research-notes.md R1）:
- **cancel/abort 编排**：固化 `interrupted: true` assistant message、`turn/end{aborted}`、未启动调用合成错误结果（`lib/index.js:629-649,574-580,273-290`）——与无痕性不可调和，用户明确否定。
- **`tools/execute` around-dispatch 包装器**：可技术上调用 `concludeTurn()`，但 `ToolDispatchExecution` 类型（`Omit<ToolExecution,'signal'> & {signal}`，`index.d.ts:271-274`）未声明该方法——依赖未类型化面。
- **`tools/post-execute` 决策附加**：`PostToolDecision` 各 kind 均无 `concludesTurn` 字段（`index.d.ts:430-460` 一带），不可附加。
- **`agent/turn-stopping` 钩子**：turn 已决定停止后才派发的 listener（`lib/index.js:564-570`），不是收束触发器。
- **编排层新通知通道**：runtime 无事件面，但 turn 收束 → `agent/status` idle 本身即通知，且是"至多驱动一个成员"不变量下复盘的最早可能时刻（research-notes R2）——专用回调无增益。

## D2. `ToolOutcome` 契约扩展：success variant 增加可选 `concludesTurn: true`

**Decision**: `common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:90-92` 的 `ToolOutcome` 成功分支扩展为 `{ isError: false; text: string; concludesTurn?: true }`（错误分支不变，类型上保持不可携带）。标记计算规则（纯内容驱动）：**返回结果的识别棋盘 `gameStatus(state) ∈ {won, lost}` 即置位**。工具层 `executeOutcome`（`common/js/dsh-plugins/saolei/src/index.ts:109-118`）映射：`outcome.concludesTurn === true` 时调用 `exec.concludeTurn()` 后返回 `{result: outcome.text}`。

**Rationale**: 单一判定函数覆盖全部收束面且无需逐路径特判（见 [data-model.md](data-model.md) §1 判定矩阵）：FR-001 致终局 operate（`endedStatus` 置位后 finalState 必为终局）、FR-002 ① init 即终局（识别出的 state 终局）、FR-002 ② 终局棋盘结构性拒绝（stop 分支的 finalState 即终局棋盘）、空操作列表返回终局棋盘（`runtime.ts:220-228` 早退路径，同一规则自然覆盖）；playing/无棋盘（`no_active_game`、`unable to recognize board`）/`remain`（只读，永不置位）/isError 一律不置位。标记在 runtime 计算、经契约携带、在工具层消费——三层各自无状态（Session 裁定：工具层不感知编排/排队状态）。

**Alternatives considered**:
- 工具层自行解析 `text` 中的 `game status:` 行判定终局：文本耦合脆弱（文本渲染变更即破坏），且判定逻辑离开数据持有者（runtime）。
- runtime 持有"待收束"状态位（有状态 conclude）：违反 Session 裁定一/三（无状态、纯内容驱动），且需处理状态清理。
- 新增独立 `ConcludeSignal` 通道类型：多一个契约面无收益；`ToolOutcome` 是三工具唯一 outcome 契约，收束语义就属于它。

## D3. 零改动面（编排器/team/relay/webUI/宿主）

**Decision**: 生产代码仅改 `runtime.ts`（标记计算 + 契约扩展）与 `saolei/src/index.ts`（`executeOutcome` 映射）两文件。

**Rationale**（逐面证据，复核 research-notes R2–R6）:
- **编排器**（`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts`）：turn 收束 → `kick()` finally `setPhase(idle)` 发 `agent/status` idle → `settleIdle`（`orchestrator.ts:614-618,862-867`）解除 `drive()` 等待（`:841-848`）→ `nextStep()` 求值（`:763-808`：FIFO 消化 `:772-779` → pendingReview 重试 → phase 续驱 → gameEnded 评估 `:796-805` `peekGameEvent() !== reviewedGameEvent` → 结构性续驱）。FR-004 编排器零改动成立。
- **relay**（`common/js/dsh-plugins/team/src/team.ts`）：`tool/result` 到达即与 `tool/call` 配对 relay（`:315-360`）；durable 提交先于收束判定（agent-loop `lib/index.js:176-187` 同迭代序）——终局工具单元必然完整进入 planner 待消费列表，`drain` 非空论证见 research-notes R3。
- **webUI**（`projects/game/agent_v2/src/history.ts`）：`MemberCollector.onStatus` idle 时 outcome = `pendingOutcome ??（无 failure → COMPLETED）`（`:764-805`）；`pendingOutcome` 仅 Cancel/刷新标记（`:574-575`）——收束路径无 failure 无标记 → `TURN_STATUS_COMPLETED`（FR-003）既有导出。
- **player 后续上下文**：`step()` 每次经 `session.deriveMessages()` 组装（agent-loop `lib/index.js:613`），终局 call+result 已在 session log——FR-005 既有导出。

**Alternatives considered**: 无——本 feature 的设计目标即"最小 seam"（Motivation 表：编排器零改动）。

## D4. FR-002 范围与终局记录同一性

**Decision**: 收束范围 = 任何返回终局棋盘的成功结果（spec Session 裁定一）；对终局棋盘的结构性拒绝**不重写** `gameEvent`（`runtime.ts:236-261`——`endedStatus` 仅在 `kind === "ok"` 且非 playing 时置位，`gameEvent` 写入在 `:277-285` 仅随 `endedStatus`）。

**Rationale**: 拒绝不重写记录 ⇒ `peekGameEvent()` 返回**同一记录对象** ⇒ `event !== this.reviewedGameEvent`（`orchestrator.ts:796-797`，引用比较）为 false ⇒ 已复盘的终局棋盘上的后续操作（含其收束）不重复复盘——"同一局不重复切换"由既有去重承担，零新增逻辑。复盘后 player 操作旧终局棋盘属 LLM 行为范畴，不属 team 协作语义（Session 裁定一）。

**Alternatives considered**: 终局记录改"未复盘队列"保证每局专属复盘一次——改 059 LATEST 槽位契约 + 编排器连带变化，为边界竞态窗口引入复杂度，用户否定。

## D5. 排队消息零特判（编排 FIFO）

**Decision**: 收束与切换判定不因排队输入做任何特判（spec Session 裁定、FR-004 条款）。编排 FIFO（`orchestrator.ts` `this.queue`）位于编排层、不进 agent 侧 next-step inbox：终局流程照常 `game end → step end → turn end` → saolei-loop idle 分支（队列空 → 复盘；队列非空 → player 新 turn 消化 `:772-779`，终局结果经 history 自然在场）。

**Rationale**: saolei-loop 只能看到 FIFO。无状态内容驱动判定 + 零特判 = 与任何未来输入机制（含 061 的排队输入语义，由其需求文档定义）天然无冲突（spec Assumptions"061 关系"）。

**Alternatives considered**: 编排层快照/排队终局记录以保证每局专属复盘——改 059 契约且属已否定的特判复杂度。

## D6. 测试基建策略

**Decision**:
1. **单测**（SC-004）：`runtime.test.ts` 断言收束标记矩阵（致终局 operate / init 即终局 / 终局结构性拒绝 → 置位；失败/非终局/remain/无棋盘 → 不置位）；`index.test.ts` 的 `fakeExec` 增加 `concludeTurn` spy，断言工具层映射（置位 → 调用；不置位 → 不调用）。既有终局棋盘用例的 `toEqual` 断言随可选字段出现同步更新。
2. **fake-llm 夹具**：`agent_v2_saolei_tools.yaml` 既有终局链规则（`agent-v2-saolei-operate-won/lost`、`agent-v2-saolei-init-lost`、`agent-v2-saolei-init-operate` 的 operate 批）**保留原样**——它们正是"终局后仍备有的后续脚本步骤"（SC-001 断言面：这些规则在收束路径下永不匹配请求）；仅更新注释说明其新角色。`team_player.yaml` 头部行为脚本注释同步。`team_planner.yaml` **零改动**——review 条目锚定 `history_keywords`（"game status: won/lost"，relay 内工具结果原文）而非总结文本（`team_planner.yaml:89-99`），移除总结不破坏 planner 脚本。
3. **大型测试断言更新**（SC-001/002/005）：
   - `agentV2WonSummaryText`/`agentV2LostSummaryText` 断言退役：player 终局 turn 最后输出块 = 终局工具调用块（无收尾文本）；`teamTurnBlocks` 文本断言改为"无文本块/最后块为工具块"形态断言。
   - `TestAgentV2TeamGameWonChainOnExecutor`：拓态变化——init 即识别胜利棋盘 → turn 在 init 后收束（tool_result 数 2 → 1，operate 断言移除，改为"无第二次模型输出"断言）；终局记录不新增（init 不写 gameEvent）→ 无复盘、链路静止。
   - `TestAgentV2TeamGameTerminalWonAndReviewContinues`：game 1 turn 在 operate 终局结果后收束（无总结文本）；game 2 init 即终局 → turn 在 init 后收束（tool_result 数 2 → 1）；4-turn 链形状保持。
   - `TestAgentV2TeamGameTerminalLostAndReviewStops`：game turn 在 operate 失败终局后收束（无总结文本）；4-turn 链形状保持。
   - `TestAgentV2TeamGameConversationStreamIndependentOfFlow`：quiescence 轮询锚点从 won 总结文本改为终局工具结果文本（"game status: won"）。
   - `agentV2NodesktopSummary` 各处（`agent_v2_game_test.go:330`、`agent_v2_conversation_test.go:118`、`agent_v2_game_disconnect_test.go:89`、`agent_v2_preset_test.go:560`）**不受影响**——init dispatch 失败为 isError，不收束，模型正常输出总结文本（FR-001 失败不收束面的天然回归断言）。
   - `TestAgentV2TeamGameActiveMemberTransitions`：flow 脚本消耗序不变（init×2 + step×1）；game 2 断言随 init-terminal 收束调整（无 operate 派发）；末尾 active_member = player 静止断言保持。
   - `message_store_test.go` 索引 lockstep：无新增/删除条目则零改动（注释变更不影响）。

**Rationale**: fake-llm 的规则匹配天然构成"零执行"证明——规则在则请求必答，规则未答 ⇔ 请求未发生；比"删除后续步骤"更强的断言面（脚本备有后续步骤而终局后零执行 = 恰好复刻真实 LLM 连续游戏倾向的生产缺陷场景）。

**Alternatives considered**: 删除终局链后续规则并另造"显式未执行"标记——弱化断言（无法区分"没有后续步骤"与"有但未执行"）；为 fake-llm 新增请求计数断言面——现有流断言（turn 内块序列、tool_result 计数）已可表达，无需新基建。

## D7. 验收执行方式

**Decision**: 大型测试经 testplan skill 实际执行（`guitar run <plan.yaml>`，部署→测试→清理闭环），全部用例通过为验收（constitution 原则 VI）；单测随每次变更 `bazel test`。

**Rationale**: 宪章强制；且本 feature 的核心断言（turn 收束切面）只能在实际集成环境验证。

**Alternatives considered**: 无（构建检查不构成验收，宪章明文禁止）。
