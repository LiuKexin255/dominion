# Feature Specification: Fix Agent Service Crash on Desktop Disconnect

**Feature Branch**: `026-agent-abort-crash-fix`

**Created**: 2026-07-27

**Status**: Draft

**Input**: User description: "在运行中 desktop 断开 session 会导致 agent 服务重启。017-agent-loop-graceful-abort 已经实现了优雅中断，023-saolei-mcp-refine 的重构破坏了优雅中断的逻辑。修复该问题。"

## Motivation

`specs/017-agent-loop-graceful-abort/spec.md` 实现了 desktop 断开 bidi stream 后通过官方 LangChain `AbortSignal` 契约优雅终止 in-flight agent turn 的机制。该机制在 `specs/023-saolei-mcp-refine/` 的 Phase 2/Phase 3 重构后发生回归：desktop 断开连接时 agent **服务进程崩溃并重启**，导致所有正在进行的会话（不止被断开的那个）全部中断。

**根因（经 Phase 0 源码分析修订。** 初始假设为 `stream.output` rejection 未被消费 → unhandled rejection → crash，但阅读 `@langchain/langgraph@1.4.8` 安装源码（[research.md](./research.md) §A.2）后发现 `stream.output` 内部已有 `.catch(() => {})`，**不会产生 unhandled rejection**。实际崩溃链条（[research.md](./research.md) §D）为 `handler.ts` catch 块中**未保护的 `stream.write()` 调用**在 bidi stream 已关闭时抛出同步异常，该异常从 catch 块内部逃逸为 unhandled rejection。

具体链条：

1. 023 引入的 `mergeIterables` 在 abort 时先 flush 缓冲帧再抛异常（[research.md](./research.md) §C），在 bidi stream 已关闭但 abort 信号尚未传播的 race 窗口内触发额外 `stream.write()` 调用。
2. `stream.write()` 向已关闭的 gRPC stream 写入时抛出 `ERR_STREAM_DESTROYED`，被 handler.ts 的 try/catch 捕获。
3. 此时 `controller.signal.aborted` 可能仍为 `false`（[research.md](./research.md) §A.6），catch 块进入 `else` 分支，调用 `stream.write(warnFrame)` 和 `stream.write(waitFrame)` —— 这些写入**从 catch 块内部抛出新异常并逃逸**。
4. 逃逸异常从 `stream.on("data", async (frame) => {...})` 的 async listener 传播为 rejected Promise，成为 unhandled rejection。
5. `projects/game/agent/src/bootstrap.ts`（行 40-41）只注册了 `SIGTERM`/`SIGINT`，**无** `process.on("unhandledRejection", ...)`。Node.js ≥15 默认 `--unhandled-rejections=throw`，导致 `process.exit(1)` → agent 服务重启。

017 的单一消费者路径无 `mergeIterables` flush，abort 直接传播到 catch 块（`controller.signal.aborted === true`，走安全分支），崩溃仅理论上可能。023 的 flush 加宽了 race 窗口，使崩溃稳定复现。

`mergeIterables` 的注释（`projects/game/agent/src/llm.ts` 行 716-719）已识别到 unhandled rejection 风险类别并为 IIFE 添加了 catch，但未注意到 catch 块自身的 `stream.write()` 调用存在同一风险。

## Relationship

- 修复 `specs/023-saolei-mcp-refine/`（Phase 2 commit `c5a6d61`、Phase 3 commit `ece1569`）引入的回归。
- 恢复 `specs/017-agent-loop-graceful-abort/spec.md` FR-001/FR-004/FR-005/FR-007 的优雅中断保证（turn 在断开后 promptly 停止、不向 dead peer 发出 frames、释放 per-session mutex、正常 turn 路径不变）。
- 不变更 023 的内容模型（`MessagePart`/`FlowPart`）、tool 状态穿透、debug 抽屉等任何功能面；仅修复 abort 路径上的进程稳定性回归。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Desktop Disconnect Must Not Crash the Agent Service (Priority: P1)

一个 operator 运行着 agent 服务，多个 desktop 客户端同时连接着各自的 session。其中一个 desktop 在 agent turn 进行中（LLM 正在推理、或工具刚被分派）时关闭应用、断网或手动断开。agent 服务 MUST 保持运行 —— 不止为被断开的 session 保持运行，更重要的是**为所有其他正在使用服务的 session 保持运行**。被断开的 session 的 in-flight turn 按 017 的契约被优雅终止（停止消耗 LLM token、不向 dead peer 发出 frames、释放 mutex 以便重连）。

**Why this priority**: 服务进程崩溃是最严重的稳定性缺陷 —— 一次单连接断开导致全部会话中断、全部 in-flight turn 丢失、所有 desktop 被迫重连。这是 017 优雅中断机制存在意义的根本前提：如果断开会杀掉进程，"优雅"就无从谈起。

**Independent Test**: 启动 agent 服务并建立至少两个活跃 session；在其中一个 session 的 turn 进行中断开对应的 desktop bidi stream；确认（a）agent 服务进程**仍在运行**（PID 不变、健康检查通过、其他 session 的 turn 不受影响），（b）被断开 session 的 turn 已停止、不再消耗 LLM token，（c）被断开的 session 用相同 session id 重连后能从上一 checkpoint 继续新 turn。

**Acceptance Scenarios**:

1. **Given** 一个 agent turn 正在进行（LLM 正在流式输出文本），**When** 对应 desktop bidi stream 断开，**Then** agent 服务进程**不退出**，该 turn 在短时间内停止，且服务对其他 session 仍然可用。
2. **Given** 一个 agent turn 正在进行且一个工具操作已分派给 desktop，**When** desktop bidi stream 在工具结果返回前断开，**Then** agent 服务进程**不退出**，in-flight turn 被停止，不等待工具结果。
3. **Given** agent 服务承载着多个并发 session，**When** 其中一个 session 的 desktop 断开，**Then** 其他 session 的进行中 turn 完全不受影响（不中断、不丢帧）。

---

### User Story 2 - Graceful Abort Behavior Fully Restored (Parity with 017) (Priority: P1)

除了进程不崩溃之外，017 建立的全部优雅中断可观测行为 MUST 完整保留：断开后不向已断开的 desktop 发出 error/warn frames（FR-004 of 017）、per-session turn mutex 被释放以便重连（FR-005 of 017）、被中止的 turn 留下的对话 checkpoint 允许后续 turn 正确恢复（FR-006 of 017）、desktop 保持连接时的正常 turn 路径与修复前字节级一致（FR-007 of 017）。

**Why this priority**: "不崩溃"是地板，"优雅"是 017 承诺的天花板。修复 MUST 同时达成两者 —— 仅止住崩溃但重新引入 017 移除的噪音帧 / mutex 泄漏 / checkpoint 损坏，等于用一种回归换另一种回归。

**Independent Test**: 在 agent turn 进行中断开 desktop；确认 agent 日志中无面向已断开 session 的 error/warn frame 发出记录、该 session 的 mutex 被释放（重连后可立即开始新 turn）、重连后 `ListMessages` 返回的对话状态与断开前一致（无部分 LLM 消息、无孤立 tool 消息、无丢失用户输入）。再跑一次 desktop 全程保持连接的正常 turn，确认流式输出、closing wait frame、mutex 释放与修复前完全一致。

**Acceptance Scenarios**:

1. **Given** 一个 turn 因断开被中止，**When** 检查 agent 服务日志与发出的 frames，**Then** 没有任何 error/warn frame 被发向已断开的 session（017 FR-004）。
2. **Given** 一个 turn 因断开被中止，**When** 该 session 的 desktop 用相同 session id 重连并开始新 turn，**Then** 新 turn 立即被接受（mutex 已释放，017 FR-005），并从上一持久化 checkpoint 恢复（017 FR-006）。
3. **Given** desktop 在整个 turn 期间保持连接，**When** 用户提交一个正常完成的 turn，**Then** 所有 reasoning/text 帧、closing wait frame、mutex 释放行为与修复前完全一致（017 FR-007 / SC-004）。

---

### Edge Cases

- **Abort 恰好发生在 `stream.output` 已 resolve 之后**：turn 已正常完成、stream 已关闭，此时 desktop 断开。服务 MUST 保持 idle（017 FR-009），不执行任何 abort 工作、不发出 frames、不变更状态。
- **Abort 时无 turn 在进行中**（session idle）：desktop 断开时没有 in-flight turn。服务 MUST 保持 idle，不执行 abort 工作（017 Edge Case: Disconnect while no turn is in flight）。
- **Abort 时多个并发流消费者处于不同阶段**：`consumeMessages` / `consumeToolCalls` / `consumeToolResults` 各自可能已结束、正在迭代、或即将抛出异常。无论哪种组合，`stream.output` 的最终状态（resolved / rejected / pending）MUST 被妥善消费，不产生 unhandled rejection。
- **重连发生在被中止 turn 的流尚未完全拆除时**：新连接 MUST 能注册新 sink 并开始新 turn，正在拆除的被中止 turn MUST 不干扰新 turn（017 Edge Case: Reconnect during aborted turn）。
- **Desktop 在工具结果返回后、agent 产生 LLM tool 结果之前断开**：turn 被中止，部分 tool 结果可能已写入 checkpoint。重连后的 `ListMessages` MUST 反映一致状态（无半更新的 tool 气泡）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 当 desktop bidi stream 断开导致 in-flight agent turn 被 abort 时，agent 服务进程 MUST NOT 崩溃、退出或重启。
- **FR-002**: 在 abort 路径上，agent 服务运行期间 MUST NOT 产生 unhandled promise rejection。任何代码路径（正常完成、消费者抛出异常、yield 提前退出）导致的 Promise rejection 都 MUST 被妥善消费，不得逃逸。具体包括：(a) catch 块 error-handling 代码路径中向已关闭 stream 写入导致的异常 MUST 被捕获而非逃逸为 unhandled rejection（[research.md](./research.md) §D）；(b) `streamEvents` 返回对象上承载运行结果的 Promise 在任何退出路径下 MUST NOT 成为 unhandled rejection（[research.md](./research.md) §A.2 确认 LangGraph v1.4.8 内部已有保护，修复仍需验证所有路径）。
- **FR-003**: abort 后，agent 服务 MUST NOT 向已断开的 desktop 发出 error 或 warning frames（恢复 `specs/017-agent-loop-graceful-abort/spec.md` FR-004）。
- **FR-004**: abort 后，per-session turn mutex MUST 被释放，使 session 可接受来自重连 desktop 的新 turn（恢复 017 FR-005）。
- **FR-005**: 被 abort 的 turn MUST 留下允许后续 turn 正确恢复的对话 checkpoint —— 无部分 LLM 消息、无孤立 tool 消息、无丢失用户输入（恢复 017 FR-006）。
- **FR-006**: desktop 保持连接时的正常 turn 路径 MUST 与修复前行为完全一致：相同的帧以相同顺序流式输出、相同的 closing wait frame 关闭 turn、mutex 以相同方式释放（恢复 017 FR-007）。
- **FR-007**: 断开时无 in-flight turn 的情况下，服务 MUST 保持 idle —— 不执行 abort 工作、不发出 frames、不变更状态（恢复 017 FR-009）。
- **FR-008**: 修复 MUST NOT 改变 023 引入的并发流消费语义（`consumeMessages` / `consumeToolCalls` / `consumeToolResults` 的职责划分与 `mergeIterables` 的合并行为）—— 这些是 023 tool_call/tool_result 实时渲染的正确设计，仅 abort 路径的资源清理需要修复。

### Key Entities *(include if feature involves data)*

- **Agent Run Result Promise**（即 `stream.output`）：`agent.streamEvents(input, { signal })` 返回对象上代表整个 LangGraph agent 运行最终结果的 thenable。abort 时它会 reject；本 feature 的核心是确保该 rejection 在所有退出路径下都被消费。
- **Per-turn AbortController**：017 引入的、per-session turn 粒度的取消控制器（`projects/game/agent/src/handler.ts` 中 `activeTurns` map）。其 `signal` 流经 adapter 接口传入 `streamEvents`。本 feature 不改变其生命周期，仅修复下游消费 signal 的代码路径。
- **Concurrent Stream Consumers**：023 引入的三路并发消费者（`consumeMessages` 迭代 `stream.messages`、`consumeToolCalls` 迭代 `stream.toolCalls`、`consumeToolResults` 迭代 `stream` 本身）。它们经 `mergeIterables` 合并。abort 时它们的退出行为是触发 `stream.output` dangling rejection 的前置条件。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 在 100% 的"desktop 在 turn 进行中断开"场景下，agent 服务进程在断开发生后保持运行（进程不退出、PID 不变、对其他 session 持续可用）。
- **SC-002**: 修复后，agent 服务运行期间产生 **零** unhandled promise rejection（可通过 Node.js 进程级 `unhandledRejection` 事件监控或容器控制台输出验证）。
- **SC-003**: 被 abort 的 session 用相同 session id 重连后，能立即开始新 turn（mutex 已释放）并从上一持久化 checkpoint 恢复（100% 的 pre-disconnect 用户消息与已完成的 assistant 消息保留）。
- **SC-004**: desktop 保持连接的正常 turn 路径与修复前字节级一致：流式帧、closing wait frame、mutex 释放行为完全相同（跨 text-only、image+text、tool-using 三类 turn）。
- **SC-005**: 源码检查可确认：abort 路径上不存在任何 dangling Promise（其 rejection 可能在 `await` 被跳过时变成 unhandled rejection 的 thenable）。

## Assumptions

- `specs/017-agent-loop-graceful-abort/research.md` §A.3 的结论仍然成立：`stream.messages`（以及基于同一 mux 的 `stream.toolCalls` 投影）在 abort 时干净退出、不向消费者抛出异常（依据 [langchain-ai/langchainjs#9900](https://github.com/langchain-ai/langchainjs/pull/9900)）。
- `consumeToolResults` 直接迭代底层流（`stream[Symbol.asyncIterator]`）的 abort 行为**未被 017 验证** —— 它可能在 abort 时抛出异常而非干净退出。本 feature 的修复 MUST 对此鲁棒（无论该消费者是否抛出异常，`stream.output` 都不应成为 dangling rejection）。
- `stream.output` 在 abort 时会 reject（LangGraph 取消运行后，代表运行结果的 Promise 进入 rejected 状态）。这是触发 dangling rejection 的必要条件；即使某些 LangGraph 版本下 `stream.output` 在 abort 时保持 pending 而非 reject，修复仍 MUST 妥善处理 reject 情形以向前兼容。
- Node.js 运行时对 unhandled promise rejection 的默认行为是终止进程（`--unhandled-rejections=throw` 自 Node.js v15 起为默认）。`projects/game/agent/src/bootstrap.ts` 当前未注册 `unhandledRejection` 处理器，故默认行为生效。
- 本 feature 的修复范围限于 agent 服务的 abort 路径稳定性；不涉及 desktop 侧、proto 模型、testplan 断言的变更（除非大型测试需要新增"断开后服务存活"的验收用例，见 plan 阶段）。

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/017-agent-loop-graceful-abort/spec.md` — 被回归的优雅中断原始规范（FR-001..FR-009, SC-001..SC-005）；本 feature 恢复其全部可观测行为保证。
- `specs/017-agent-loop-graceful-abort/research.md` §A.3 — `stream.messages` 在 abort 时干净退出的验证依据。
- `specs/017-agent-loop-graceful-abort/plan.md` — 017 的取消流设计（per-turn AbortController、signal 传入 `streamEvents`、handler catch 块检查 `controller.signal.aborted`）。
- `specs/023-saolei-mcp-refine/plan.md` — 引入回归的重构（Phase 2 commit `c5a6d61`、Phase 3 commit `ece1569`）。
- `specs/023-saolei-mcp-refine/tasks.md` — Phase 2/Phase 3 任务定义（T003–T020 引入并发流消费）。
- `projects/game/agent/src/llm.ts` 行 486-527（`streamFromAgent`：并发消费 + `await stream.output`）、行 635-663（`consumeToolResults`：直接迭代底层流）、行 716-780（`mergeIterables`：throw 传播与 IIFE catch）—— 回归的代码现场。
- `projects/game/agent/src/handler.ts` 行 204-215（`activeTurns` / `abortAllTurns`）、行 390-542（per-turn controller 生命周期 + catch/finally）、行 546-561（stream end/error → abortAllTurns）—— 017 机制保留部分。
- `projects/game/agent/src/bootstrap.ts` 行 40-41 —— 仅 SIGTERM/SIGINT，无 `unhandledRejection` 处理器。

### External

- [langchain-ai/langchainjs#9900 — full AbortSignal handling across providers](https://github.com/langchain-ai/langchainjs/pull/9900) — `stream()` / `streamEvents()` 在 abort 时干净退出（不向消费者抛出异常）的依据；`invoke()` 则抛出 `AbortError`。
- [LangChain JS — `RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal) — 官方取消契约。
- [Node.js — unhandled-rejections behavior](https://nodejs.org/api/process.html#event-unhandledrejection) — Node.js 默认对 unhandled promise rejection 终止进程的行为说明。
