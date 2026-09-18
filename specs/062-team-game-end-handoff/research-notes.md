# Research Notes（草稿）: 062-team-game-end-handoff 机制调研

> **状态**：草稿——由规划对话于 2026-09-12 产出，供 /speckit research 流程（`research.md` 正式产出）引用与复核；全部结论基于本地物化源码逐行阅读。
>
> **物化版本**：`@deepseek-ai/dsh-agent-loop@0.1.1-rc.2`、`@deepseek-ai/dsh-tools@0.1.1-rc.2`（node_modules 内两份 peer-hash 拷贝已核对 md5 一致）。行号引用以 `node_modules/.pnpm/@deepseek-ai+dsh-{agent-loop,tools}@0.1.1-rc.2_*/…` 通配（与 `specs/061-team-queue-steer/spec.md` 引用惯例一致）；下文 agent-loop 指 `…/dsh-agent-loop/lib/index.js`，dsh-tools 指两文件 `…/dsh-tools/lib/types/index.js` 与 `index.d.ts`。

## R1. dsh `concluded` 语义与收束 seam（调研项 1）

**结论：工具层存在官方 turn 收束 seam（`concludeTurn`/`concludesTurn`），完全满足"自然收束 + 无痕"；这是本 feature 的选型 seam。**

证据链（自底向上）：

1. **工具体的类型化 seam**：`ToolDefinition.execute(args, exec: ToolRunContext)`（dsh-tools `index.d.ts:106-119`）；`ToolRunContext.concludeTurn()` 的文档注释："Mark a successful final result as terminal for the current agent turn. The marker rides this execution's own result (`concludesTurn` exists only on ToolExecutionSuccess)"（`index.d.ts:291-299`）。
2. **实现**：`exec.concludeTurn()` 将该 execution 加入 `concludingExecutions` WeakSet（dsh-tools `index.js:796-798`）；成功结果物化时 `createSuccessResult` 读取 WeakSet 附着 `concludesTurn: true`（`index.js:1199-1206`）；`materializeFinalResult` 保留该标记（`index.js:1243`）；`tools/post-execute` 决策的 `markCanonical` 展开亦保留（`index.js:1160-1164`）。**失败结果不可携带**（`ToolExecutionFailure.concludesTurn?: never`，`index.d.ts:400-409`）。
3. **agent-loop 聚合**：`runGroup.commitReady` 在 `appendToolResult`（durable `tool/result` 提交）**之后**执行 `concluded ||= result.concludesTurn === true`（agent-loop `index.js:176-187`，关键行 182/184）；`executeToolCalls` 跨调用组聚合 `concluded` 且**不因 concluded 中断后续调用组**（`index.js:117-144`，循环体 132-142——同 step 内终局调用之后的其他工具调用仍执行并提交）。
4. **step/turn 收束**：`step()` 中 `const { concluded } = await executeToolCalls(...); return concluded ? { kind: "completed" } : null;`（`index.js:685-686`）；`turn()` 将其记为 `turnEnds = {kind:"completed"}`（`index.js:556`）——与"模型输出无工具调用"的自然完成同构（`index.js:683-684`）；next-step inbox 为空时经 `agent/turn-stopping` serial 后 break（`index.js:564-571`）；`turn/end` 事件携带 reason `{kind:"completed"}`（`index.js:590-598`）。
5. **无痕性**：`concludesTurn` 是执行局部信号，**不落 durable 事件**——`appendToolResult` 只持久化 content/isError/error.info/meta（agent-loop `index.js:302-318`）。终局收束后的 session log 形态：assistant/message（含 tool-call）→ tool/call → tool/result → step/end → turn/end{completed}。与自然停手的差异仅为"无后续模型调用/无收尾文本"——恰为用户裁定的等价目标（"日志等价于模型在终局步自然停手"）。
6. **cancel 对照**（否定路线固化）：cancel → `phase.abort.abort(cause)`（`index.js:405-411`）→ 流中断路径固化 `interrupted: true` 的 assistant/message（`index.js:629-649`）→ `turnEnds = {kind:"aborted", reason}`（`index.js:574-580`）→ 未启动调用追加合成错误结果 "Error: tool call aborted before dispatch"（`index.js:273-290`）。dsh cancel 默认 durable 清空全部 pending、无 step-only abort（`specs/061-team-queue-steer/research.md` 既有记载）。abort 标记不可移除 → 与"无痕性"不可调和，用户已否定。

**替代 seam 评估**（均劣于上述）：

- `tools/execute` around-dispatch 包装器：运行时收到的是 registry 铸造的同一 execution 对象（dsh-tools `index.js:963-967` 的 `mutableExec`），技术上可调 `concludeTurn()`，但 TS 类型 `ToolDispatchExecution = Omit<ToolExecution,'signal'> & {signal}`（`index.d.ts:271-274`）**未声明** `concludeTurn`——依赖未类型化面，劣于工具体路径。
- `tools/post-execute` 决策：`PostToolDecision` 各 kind 均无 `concludesTurn` 字段（`index.d.ts:430-460` 一带），不可附加。
- `agent/turn-stopping` 钩子：在 turn **已决定停止后**才派发的 listener（`index.js:564-570`），不是收束触发器。
- 编排层 cancel 组合：见上文 cancel 对照，被用户否定。

## R2. 终局通知通道（调研项 2）

**结论：无需新增 runtime→编排器通知机制——turn 自身的 idle 转换即通知，且是"至多驱动一个成员"不变量下复盘的最早可能时刻。**

- 现状：runtime 无事件/回调面，`endedStatus !== null` 时同步置 `this.gameEvent`（`common/js/dsh-plugins/saolei-loop/src/game/runtime.ts:277-285`），编排器轮询 `peekGameEvent()`（`runtime.ts:301-303`；`orchestrator.ts:796`）。
- 收束路径下：`operate()` 内 gameEvent 写入先于工具返回 → 工具结果提交 → turn 收束 → `kick()` finally `setPhase(idle)` 发 `agent/status` idle（agent-loop `index.js:477-491,383-389`）→ 编排器 status 监听 `settleIdle`（`orchestrator.ts:614-618,862-867`）解除 `drive()` 的 idle 等待（`orchestrator.ts:841-848`）→ pump 重评估。复盘只能在 player turn settle 后开始（`drive()` 的互斥不变量，`orchestrator.ts:829-838`），故 idle 即最早时刻，专用回调无增益。

## R3. relay 完整性（调研项 3）

**结论：终局 step 的广播单元（call+result 配对）由既有机制保证完整 relay 给 planner。**

- durable 提交先于收束判定：`appendToolResult`（提交）在 `concluded ||=`（读标记）之前同迭代执行（agent-loop `index.js:176-187`）。
- team 服务事件驱动 relay：成员 `session/event` 订阅（`common/js/dsh-plugins/team/src/team.ts:255-259`）；`tool/result` 到达即与等待中的 `tool/call` 配对成完整单元 relay 进所有接收方 pending（`team.ts:315-360`，配对/清理 344-359）。
- drain 读权威回读：`drain(planner)` 先 `reconcile`（以 sender log 派生重建 pending，自愈 + exactly-once，`team.ts:397-425`）再逐单元回读渲染（`team.ts:185-203`）；单元完整性要求 call+result 配对（`common/js/dsh-plugins/team/src/broadcast.ts:121-155`）；渲染为 `<player-tool-call>` 标签对内 tool/args/result 全文（`broadcast.ts:232-263`，060 contracts/team-api.md §4 终态格式）。
- 非空论证：复盘评估时 `drain(planner)` 必含终局工具单元（player 自上次 planner 驱动后的全部产出，终局 step 必然在内；planner 非驱动态不消费）→ `nextStep()` case 4 的 `relays.length > 0` 条件必然成立（`orchestrator.ts:796-805`）。

## R4. 切换时序——"优雅路径"成立（调研项 4）

**结论：turn 经 concluded 自然收束后，现有 post-idle 的 nextStep 复盘逻辑原样接管，编排器零改动。**

逐行核对 `orchestrator.ts` `nextStep()`（763-808）：case 1 排队消化（772-779）→ pendingReview 重试（781-787）→ planning/reviewing 相位续驱 player（789-794）→ case 4 gameEnded 评估（796-805：`peekGameEvent()` 非空且 ≠ `reviewedGameEvent` → `drain(planner)` → 复盘驱动 + pendingReview 登记）→ 结构性续驱（806-807）。收束只是使"player turn idle"这一前提在终局时刻即刻成立；`drive()` 的 idle 等待注册先于唤醒（`orchestrator.ts:854-859`），无丢失窗口。

## R5. 后续上下文（调研项 5）

**结论：planner 复盘输入含终局工具单元；之后 player 再驱动时其模型输入含自身终局 call+result——均由既有机制导出。**

- planner 复盘回合：驱动输入 = `drain(planner)` 注入的 relay 用户消息（含终局 `<player-tool-call>` 单元全文，R3）。
- player 后续回合：`step()` 每次以 `this.session.deriveMessages()` 组装模型输入（agent-loop `index.js:613`），终局 tool call+result 已在 player session log（R3 第一条），自然进入后续请求历史；会话历史为进程内存态（059 既有设计）。

## R6. 边界与联动（调研项 6）

- **多操作批量中途终局**：既有结构性停批——终局 op 生效后 `break`，之前成功 op 生效（`runtime.ts:236-261`）；收束随本次调用的单一结果发生（R1 第 3 条）。
- **排队用户消息（现行编排 FIFO）**：终局收束后由 `nextStep()` case 1 以 player 新回合消化（消化优先于 gameEnded 评估，`common/js/dsh-plugins/saolei-loop/src/orchestrator.ts:772-805` 既有优先序）。
- **061 交互**：排队输入机制与终局边界的交互语义由 061 需求文档定义，本文不赘述；无冲突保证 = 收束标记无状态、纯内容驱动（工具层不感知任何排队输入），见 spec Assumptions"061 关系"。
- **Cancel/刷新/暂停零改动**：Cancel 仍 `agent.cancel({kind:"user"})` + CANCELED 帧（`projects/game/agent_v2/src/session.ts:284-285`）；刷新 ABORTED（`session.ts:599`）；暂停/恢复语义不涉收束路径。
- **turn_end 帧呈现**：`MemberCollector.onStatus`——idle 时 outcome = `pendingOutcome ??（无 failure → COMPLETED）`（`projects/game/agent_v2/src/history.ts:764-805`，关键 776-780/799-800）；`pendingOutcome` 仅由 Cancel（CANCELED）/刷新（ABORTED）标记（`history.ts:574-575`）。收束路径无 failure、无标记 → `TURN_STATUS_COMPLETED`。
- **webUI 各视图**：tool_result 帧按 tool_id settle 块（`history.ts:645-661`）；归并序列/成员视图/List 回填走既有投影——终局工具结果即 player turn 最后内容块，无缺失标记。终局收束后 player 不再有收尾文本（fake-llm 既有"终局总结文本"断言需随 SC-005 更新，`projects/game/testplan/agent_v2_game_test.go:78-80,170-172,275-277`）。
- **作用边界**：saolei 工具无 runtime 支撑时 fail-loud（`common/js/dsh-plugins/saolei/src/index.ts:91-104`）→ planner 误调不收束；agent_v2 生产 team-only（`createAgentGameRuntime` 仅经编排器挂载，全仓引用核实）。

## 生产实证（缺陷证据，本对话观察）

会话 `templates/saolei/sessions/d4f677888cd38df8897f514eadb0c0fb`（运行时数据，不在仓库内）：局 1 失败后 player 在同一 turn 内自行 `saolei_init` 局 2，planner 视角零 relay 注入——复盘锚点（player turn idle）永不到达。fake-llm player 脚本在局后停手（输出总结文本收尾），大型测试全绿未暴露。

## 遗留核对项（供 plan 阶段）

- `ToolOutcome` 契约扩展形态（runtime→工具的收束标记载体）与 051 `contracts/saolei-plugins.md` §3 一族契约文档更新。
- fake-llm 夹具：player 脚本"终局后仍备有后续步骤"的具体构造与"后续步骤零执行"断言方式（已由 research.md D6 收敛：规则在则请求必答，规则未答 ⇔ 请求未发生）。
- FR-002 确认后的 `TestAgentV2TeamGameWonChainOnExecutor` 形态调整（init 即终局棋盘拓扑）。
