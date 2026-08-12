# Feature Specification: LLM Stream Stall Recovery

**Feature Branch**: `043-llm-stream-stall-recovery`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "LLM 在 think 阶段的 SSE 流中途停滞（TCP 连接存活但不再发送数据）时，agent turn 无限阻塞，所有后续用户消息进入 buffer 但永不处理，连接断开时 abort 清空 buffer 导致消息丢失。需要检测流停滞、快速失败、保留用户排队消息。"

## Motivation

当 LLM 服务异常导致 SSE 流中途停滞（连接存活、已开始接收 chunk 但随后不再有数据到达）时，整个 agent session 陷入无限阻塞状态。根因分析（见 [Session a7cb3d62f0269fa88410093380f79def 事件分析](https://github.com/anomalyco/opencode/pull/25575)）表明：

1. **turn 无限阻塞**：agent turn 的流事件循环卡在等待永远不会到达的下一个 LLM 事件上，`running` 状态保持 `true`。没有 turn 级超时来检测和打破停滞。
2. **排队消息被困**：用户在停滞期间发送的所有消息进入 TurnLoop 的 FIFO buffer（[Feature 030](../030-queued-chat-input/spec.md) FR-002），但 turn 永远不会到达 drain 检查点（[Feature 030](../030-queued-chat-input/spec.md) FR-006），消息永远不被处理。
3. **恢复时消息丢失**：bidi 流最终因 idle 超时断开，触发 abort，而 abort 按 [Feature 030](../030-queued-chat-input/spec.md) FR-011 **清空 buffer**，用户的排队消息全部丢弃。
4. **init turn 无超时**：异步 init instruction turn（[Feature 039](../039-planner-memory-calibration/spec.md) US3）同样可能因 planner LLM 停滞而无限挂起，阻塞后续用户 turn（用户 turn 会 await init turn）。

社区实践证实这是 agent 框架的共性难题：opencode 通过 `chunkTimeout`（[PR #25575](https://github.com/anomalyco/opencode/pull/25575)）检测 "silent stream dropout where TCP connection stays open but SSE chunks stop arriving"；OpenClaw 通过 `readIdleTimeoutMs` + `firstEventTimeoutMs` 分层超时（[openclaw/openclaw `proxy.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/runtime/proxy.ts)）。两者的共识：**需要独立于总请求超时的 chunk-idle 超时，且超时触发后应可恢复而非崩溃**。

## Clarifications

### Session 2026-08-11

- Q: Does the desktop's transition from "agent is typing" to idle (observed before sending a new message) contradict the "TCP connection alive but no data" hypothesis? → A: No — the transition is an indirect consequence. The LLM stream stalls (TCP alive, no SSE data) → TurnLoop blocked (`running=true`, desktop shows "typing") → no TeamFrames flow → the bidi WebSocket appears idle → after 1–4 minutes the WebSocket idle timeout fires → bidi stream drops → `abortLoops()` → `finishAbort` (clears buffer, emits `wait` to the dead stream — swallowed) → desktop detects the drop and reconnects → status probe returns IDLE (`isRunning=false` after abort) → desktop shows idle. The user perceives "typing → idle" but the underlying cause is the LLM stall triggering a WebSocket drop cascade, not the turn completing normally. The chunk-idle watchdog detects the stall in ~30s (before the WebSocket drops) and surfaces a visible `warn` through the still-alive stream.
- Q: How does the LangChain/LangGraph framework itself handle this problem? → A: **LangGraph 1.4.8 has a built-in `TimeoutPolicy`** (`@langchain/langgraph/dist/pregel/utils/timeout.d.ts`) with `idleTimeout` (resets on LangChain callback events including model tokens and tool lifecycle) and `runTimeout` (hard ceiling). It raises `NodeTimeoutError` on expiry, using cooperative AbortSignal cancellation. This is the exact chunk-idle-watchdog mechanism the spec calls for, built into the framework. LangChain's `ChatOpenAI.timeout` only covers the initial HTTP request, not mid-stream SSE stalls (same gap identified by opencode issue #13841). The implementation should leverage LangGraph's built-in `TimeoutPolicy.idleTimeout` rather than building a custom watchdog — this is a plan-level decision per Constitution §II (Refactoring Over Patching: evaluate existing architecture first).
- Q: Does LangGraph's `idleTimeout` alone cover tool-execution phases (FR-003)? → A: **No — not during the tool's execution.** The idle timer refreshes on callback events (tool start/end) at the tool's **boundaries**, but a long-running saolei MCP tool dispatch (up to 20 min) emits no events between `handleToolStart` and `handleToolEnd`, and the watchdog is a wall-clock timer (verified in installed `dist/pregel/timeout.js`). A bare `idleTimeout` would therefore raise a false `NodeTimeoutError` mid-tool. The plan adds a **client-side heartbeat wrapper** (research.md R7.2): `withIdleHeartbeat(tool)` wraps each MCP client tool (applied in `buildSaoleiMcpTools`); during the tool's invoke it calls `config.heartbeat()` every `TOOL_HEARTBEAT_INTERVAL_MS`, which refreshes the idle timer unconditionally. This is REQUIRED because the production saolei tools (`saolei_operate`, `saolei_init`) cross the MCP HTTP boundary — `config.heartbeat` is available on the MCP client tool's invoke config (the ToolNode spreads `...config` from the node-attempt config) but cannot reach the MCP server's `bridge.dispatch` on the other side of the HTTP boundary (research.md R7.1). This satisfies FR-003's observable requirement (no false stall during tool execution) without custom pause/resume logic.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Agent Recovers From a Stalled LLM Stream (Priority: P1)

The agent is mid-turn (streaming reasoning/thinking output to the user), and the LLM service stalls — the SSE stream was delivering content but suddenly stops sending data while the connection remains alive. The agent detects this stall within a bounded time window (e.g., 30 seconds of no streaming activity), aborts the stalled model call, surfaces a visible error notice to the user, and returns to the ready (idle) state. The user can then send a new message and get a response — the session is not permanently stuck.

**Why this priority**: This is the core capability: without stall detection, a single LLM service hiccup permanently blocks the entire session until an external event (connection drop) happens to break the deadlock. This is the problem the feature exists to solve.

**Independent Test**: Start an agent turn. Simulate the LLM service stalling mid-stream (connection alive but no data). Verify that within the configured idle window (e.g., 30s), the agent emits an error notice + `wait` (returns to idle), and a subsequent user message produces a normal agent response.

**Acceptance Scenarios**:

1. **Given** an agent turn is in progress and the LLM stream has started delivering content, **When** the stream stops sending events for longer than the configured idle timeout, **Then** the agent detects the stall, aborts the stalled model call, and returns to the idle state (emits `wait`).
2. **Given** a stall was detected and the agent returned to idle, **When** the user sends a new message, **Then** a normal agent turn begins and the agent produces a response — the session is fully recovered.
3. **Given** a stall was detected, **When** the agent surfaces the recovery, **Then** a visible notice is emitted to the user (e.g., a `warn` flow signal) so the user understands why the turn stopped.
4. **Given** an agent turn where the LLM stream is actively delivering content (no stall), **When** content continues to flow normally, **Then** the idle timeout does NOT fire — the turn runs to its normal completion.

---

### User Story 2 - Queued Messages Survive a Stream Stall (Priority: P1)

When the LLM stream stalls mid-turn and messages were queued by the user during the stall (per [Feature 030](../030-queued-chat-input/spec.md) — the user can type while the agent is working), those queued messages MUST be preserved — NOT discarded — when the stall is detected and the turn is terminated. After the agent returns to idle, the retained queued messages are delivered as the next turn's input automatically, exactly as [Feature 030](../030-queued-chat-input/spec.md) FR-006 specifies for normal turn completion.

**Why this priority**: Without message retention, every stream stall silently eats the user's queued input. This is co-equal with US1 because stall detection without message preservation merely converts "stuck forever" into "lose your messages" — neither is acceptable.

**Independent Test**: Start an agent turn, queue a message while the turn is in progress, then simulate an LLM stream stall. Verify that the stall is detected, the agent returns to idle, and the queued message is automatically delivered as the next turn's input (the agent responds to it) — it was NOT lost.

**Acceptance Scenarios**:

1. **Given** an agent turn is in progress and one or more messages are queued, **When** the LLM stream stalls and the idle timeout fires, **Then** the queued messages are RETAINED in the buffer (not cleared).
2. **Given** a stall was detected and the agent returned to idle with retained queued messages, **When** the idle state is reached, **Then** the retained messages are automatically delivered as the next turn's combined input (per [Feature 030](../030-queued-chat-input/spec.md) FR-006 drain semantics), and the agent responds to them.
3. **Given** a stall-induced turn termination, **When** the user observes the session, **Then** the queued messages are visible as delivered (transitioned to normal conversation), NOT shown as permanently pending or silently vanished.

---

### User Story 3 - Tool Execution Is Not Falsely Detected as a Stall (Priority: P1)

The agent's saolei tools involve desktop operations dispatched via the OperationBridge, which legitimately take time — up to 20 minutes — as they wait for the desktop user to respond. The stall detection MUST NOT fire during tool execution: the idle timer is kept alive by a client-side heartbeat wrapper during tool execution, so a long-running tool is never mistaken for a stalled LLM stream.

**Why this priority**: Without tool-awareness, the idle timeout would fire during every non-trivial tool operation, producing false stall detections that abort legitimate turns. This would make the feature worse than the status quo for tool-heavy workflows.

**Independent Test**: Start an agent turn that invokes a saolei tool (e.g., a `saolei_operate` dispatch that waits for the desktop). Verify that the tool execution time (which may exceed the idle timeout) does NOT trigger a stall detection — the turn continues normally after the tool returns.

**Acceptance Scenarios**:

1. **Given** an agent turn in which a tool has been invoked and is executing, **When** the tool execution takes longer than the idle timeout, **Then** no stall is detected — the idle timer is kept alive by the client-side heartbeat wrapper for the duration of tool execution.
2. **Given** a tool has finished executing and the model call resumes, **When** the model resumes streaming, **Then** the idle timer is active again and resets on each received streaming event.
3. **Given** a tool has finished but the model call stalls (no streaming events after the tool result), **When** the idle timeout fires (no events for the configured period post-tool-completion), **Then** the stall IS detected — the post-tool model stall is not masked by the prior tool execution.

---

### User Story 4 - Init Instruction Turn Does Not Hang Indefinitely (Priority: P2)

The asynchronous init instruction turn ([Feature 039](../039-planner-memory-calibration/spec.md) US3, [Feature 042](../042-planner-memory-fixup/spec.md) US3) — which runs a planner model call to produce an initial calibration instruction — MUST have a bounded execution time. If the planner LLM stalls during the init turn, the turn MUST time out and degrade gracefully (skip the instruction, log a warning) within a bounded window, rather than hanging indefinitely and blocking the first user turn (which awaits the init turn's completion).

**Why this priority**: The init turn blocking the first user turn is a real but secondary concern — it only manifests on session start with a degraded LLM, and the degrade path (skip instruction) already exists for planner-model outages. The timeout adds a bounded failure mode to a path that currently has none.

**Independent Test**: Provision a new session (UpdateTeam) and simulate the planner LLM stalling during the init instruction turn. Verify that within the configured init-turn timeout (e.g., 2 minutes), the init turn degrades (logs a warning, resolves), and a subsequent user message produces a normal agent response — the session is not stuck waiting for the init.

**Acceptance Scenarios**:

1. **Given** a newly provisioned session where the async init instruction turn has started, **When** the planner LLM stream stalls during the init turn, **Then** the init turn times out within a bounded window and degrades (skips the instruction, resolves the init promise).
2. **Given** the init turn degraded due to timeout, **When** the user sends their first message, **Then** the user turn proceeds without waiting indefinitely — the init promise has resolved (via degrade), so the user turn is unblocked.

---

### Edge Cases

- **Stall during the very first streaming event (time-to-first-byte)**: if the model accepts the connection but never sends the first chunk, the idle timeout fires from the turn start (the timer starts when the turn begins, not when the first chunk arrives). The stall is detected and handled identically to a mid-stream stall.
- **Stall immediately after a tool finishes**: the heartbeat wrapper stops refreshing the idle timer when the tool completes; if the model then stalls, the full idle window must elapse before detection — the prior tool execution does not "credit" a shorter detection window.
- **Rapid stall → recover → stall cycle**: if the LLM service is persistently degraded, each user turn will stall, timeout, and return to idle. The user experiences repeated timeout notices. This is acceptable (the user can see the LLM is down) — the session is never permanently stuck, and recovery is immediate when the LLM recovers.
- **Stall detected simultaneously with a user abort**: if the idle timeout fires at the same moment the user (or connection drop) triggers an abort, the abort takes precedence — the buffer is cleared (per [Feature 030](../030-queued-chat-input/spec.md) FR-011, abort clears the buffer — unchanged). The stall detection's buffer-retention only applies when the turn terminates due to the stall timeout, not due to an external abort.
- **Mid-turn queue drain (Feature 038) interaction**: if queued messages were already injected mid-turn via the [Feature 038](../038-queue-input-mid-turn/spec.md) drain, and then the turn stalls, the already-injected messages are part of the turn's conversation (checkpointed by the graph). The retained buffer contains only messages that were NOT yet drained. The next turn picks up from the checkpoint with the retained buffer appended — no duplication, no loss.
- **Connection drops during the stall detection window**: if the bidi stream drops before the idle timeout fires, the existing abort mechanism (per [Feature 030](../030-queued-chat-input/spec.md) FR-011, clear buffer) takes effect as today. The stall detection is a fallback for when the connection stays alive but the LLM does not — the two recovery paths are independent and do not conflict.

## Requirements *(mandatory)*

### Functional Requirements

**Stream Stall Detection**

- **FR-001**: The agent MUST detect when the LLM streaming response has stalled — defined as no streaming events arriving from the model for longer than a configurable idle period — while a turn is in progress. The idle period MUST be at least 15 seconds and MUST be configurable (default: 30 seconds).
- **FR-002**: The stall detection timer MUST reset each time a streaming event arrives from the model during a turn. A stall is only declared when the full idle period elapses with zero events.
- **FR-003**: The stall detection MUST distinguish between model-streaming phases (where the idle timer is active) and tool-execution phases (where the timer is kept alive by a heartbeat). The idle timer MUST be refreshed periodically when a tool begins executing and stops being refreshed when the tool completes (success or error), so that legitimate tool wait time is never mistaken for a model stall.

**Stall Recovery**

- **FR-004**: When a stream stall is detected (FR-001), the agent MUST abort the stalled model call and terminate the current turn. The turn MUST NOT continue running after the stall is detected.
- **FR-005**: When a turn terminates due to a detected stream stall (as opposed to a user-initiated abort per [Feature 030](../030-queued-chat-input/spec.md) FR-011), the agent MUST emit a visible notice to the user (a `warn` flow signal) explaining that the turn timed out, followed by a `wait` signal returning the session to idle.
- **FR-006**: When a turn terminates due to a detected stream stall, the queued-message buffer MUST be RETAINED — NOT cleared. This is the key distinction from a user-initiated abort (per [Feature 030](../030-queued-chat-input/spec.md) FR-011, a user abort clears the buffer): a stall is an infrastructure failure, not a user intent to halt, so the user's queued input MUST be preserved.
- **FR-007**: After a stall-induced turn termination, the retained queued messages MUST be delivered as the next turn's combined input automatically (per [Feature 030](../030-queued-chat-input/spec.md) FR-006 drain semantics), so the user does not need to re-submit them. If the buffer is empty at stall recovery, the session returns to idle normally.
- **FR-008**: A stall-induced turn termination MUST be distinguishable from a user-initiated abort at the TurnLoop level: the stall takes the non-abort error terminal (emit `warn` + `wait`, retain buffer), while a user abort takes the abort terminal (emit `QueueSignal(0)` + `wait`, clear buffer). The two paths MUST NOT be conflated.

**Init Instruction Turn Timeout**

- **FR-009**: The asynchronous init instruction turn ([Feature 039](../039-planner-memory-calibration/spec.md) US3) MUST have a bounded total execution time (default: 120 seconds, configurable). If the planner LLM call stalls during the init turn, the turn MUST time out and degrade (skip the instruction, log a warning, resolve the init promise) within the bounded window — it MUST NOT hang indefinitely.
- **FR-010**: An init-turn timeout MUST NOT block the first user turn. Because the user turn awaits the init turn's completion (per [Feature 039](../039-planner-memory-calibration/spec.md) FR-015), the timeout ensures the init promise resolves (via degrade) within a bounded window, so the user turn is unblocked even if the planner LLM is degraded.

**Configuration**

- **FR-011**: The stream stall idle period (FR-001) and the init instruction turn timeout (FR-009) MUST be configurable via environment variables. Sensible defaults MUST be provided so the system works without explicit configuration (idle period: 30 seconds; init turn timeout: 120 seconds).

**Scope Boundaries**

- **FR-012**: This feature MUST NOT change the existing abort semantics for user-initiated abort ([Feature 030](../030-queued-chat-input/spec.md) FR-011 — abort clears the buffer) or connection-drop abort ([Feature 026](../026-agent-abort-crash-fix/spec.md) — stream close triggers abort). The stall detection is a NEW recovery path that coexists with the existing abort paths; only stall-induced termination retains the buffer.
- **FR-013**: This feature MUST NOT add automatic retry of the stalled turn. On stall recovery, the agent returns to idle with the buffer retained; the user can resend or the buffer is auto-drained on the next turn. Retry logic is explicitly out of scope (deferred to a future feature if needed).
- **FR-014**: This feature MUST NOT change the mid-turn queue drain behavior ([Feature 038](../038-queue-input-mid-turn/spec.md)) or the per-session FIFO turn serialization ([Feature 030](../030-queued-chat-input/spec.md) FR-014). The stall detection is layered on top of the existing turn-loop state machine without altering its IDLE/RUNNING transitions beyond adding the stall-terminated error path.

### Key Entities *(include if feature involves data)*

- **Stream Stall**: An infrastructure-level condition where the LLM's streaming response has started (at least the connection is established) but stops delivering events while the connection remains alive. It is detected by the chunk-idle watchdog (FR-001) and triggers a stall recovery (FR-004–FR-008). It is distinct from a user abort (intentional halt, clears buffer) and a connection drop (network failure, also clears buffer).
- **Chunk-Idle Watchdog**: The mechanism that monitors streaming-event activity during a turn. It maintains an idle timer that resets on each received event (FR-002), pauses during tool execution (FR-003), and fires (declaring a stall) when the full idle period elapses without activity. Its timeout-induced termination is classified as a non-abort error at the TurnLoop level (FR-008), ensuring buffer retention (FR-006).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of cases where the LLM stream stalls mid-turn (connection alive, no events), the agent detects the stall and returns to idle within the configured idle period + a small margin (e.g., 30s + 2s) — the session is never permanently stuck on a stalled stream.
- **SC-002**: In 100% of stall-recovery cases where messages were queued before the stall, the queued messages are retained and delivered as the next turn's input — zero messages lost to a stream stall.
- **SC-003**: In 100% of tool-execution phases that exceed the idle timeout duration, no false stall is detected — the idle timer is correctly kept alive by the client-side heartbeat wrapper for the full duration of tool execution.
- **SC-004**: After a stall recovery, the user can send a new message and receive a normal agent response in 100% of cases — the session is fully functional after recovery.
- **SC-005**: A stalled init instruction turn degrades within the configured init-turn timeout (e.g., 120s) in 100% of cases, and the first user turn is never blocked longer than that timeout.
- **SC-006**: Existing abort semantics (user abort clears buffer, connection-drop abort clears buffer) exhibit zero regressions — only stall-induced termination retains the buffer.

## Assumptions

- The LLM streaming protocol is SSE-based (Server-Sent Events), where a "stall" means the TCP connection remains alive but no SSE data chunks arrive. This matches the observed failure mode in the incident analysis and the failure mode documented by opencode ([PR #25575](https://github.com/anomalyco/opencode/pull/25575)) and OpenClaw ([`proxy.ts` `readIdleTimeoutMs`](https://github.com/openclaw/openclaw/blob/main/src/agents/runtime/proxy.ts)).
- The stall detection operates at the team-graph event-stream level (the `streamEvents` output consumed by the turn runner), not at the raw HTTP/SSE level. This is because the agent consumes LLM output through the LangGraph event stream, and tool-execution events flow through the same stream — the watchdog must distinguish model-streaming from tool-execution phases (FR-003; implemented as boundary refresh + client-side MCP heartbeat wrapper per research.md R7.2). The LangGraph `streamEvents` signal-propagation mechanism (already used by the existing abort path, [Feature 026](../026-agent-abort-crash-fix/spec.md)) is relied upon to abort the in-flight model call when the watchdog fires.
- The idle-period default of 30 seconds is based on the community consensus from opencode (recommended 15–30s, [PR #25575](https://github.com/anomalyco/opencode/pull/25575)) and OpenClaw (`readIdleTimeoutMs`). It is short enough to detect a stall quickly but long enough to avoid false positives during brief network jitter or model "thinking" pauses between chunks.
- Tool execution can legitimately take up to 20 minutes (the OperationBridge dispatch timeout, `projects/game/agent/src/operation-bridge.ts:56`). FR-003's requirement — no false stall during tool execution — is satisfied by the idle-timeout boundary refresh (tool start/end events) plus the **client-side MCP heartbeat wrapper** (`withIdleHeartbeat`, research.md R7.2), never by assuming tool events stream continuously. The wrapper is REQUIRED because the production saolei tools (`saolei_operate`, `saolei_init`) cross the MCP HTTP boundary: `config.heartbeat` lives in the LangGraph run and cannot reach the MCP server's `bridge.dispatch` (research.md R7.1).
- The stall-induced error path reuses the existing TurnLoop `finishError` terminal (retain buffer, emit `warn` + `wait`). This path already exists for non-abort turn errors ([Feature 030](../030-queued-chat-input/spec.md) FR-015); this feature adds a new trigger (stall timeout) for that terminal without changing its semantics.
- No automatic retry is added (FR-013). The rationale: the user's queued messages are retained (FR-006) and auto-drained on the next turn (FR-007), so manual recovery is one message away. Automatic retry adds complexity (generator restart, checkpoint resume) disproportionate to the marginal benefit, and was explicitly deferred per the design discussion.
- A total turn-level timeout (hard ceiling) is NOT included in this feature. The chunk-idle watchdog (FR-001) + tool-execution no-false-stall mechanism (FR-003) covers the observed failure mode (mid-stream stall), and the existing recursion limit (RECURSION_LIMIT=1000) bounds the number of super-steps. A total timeout may be added in a future feature if new failure modes emerge.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangGraph — `TimeoutPolicy` (node-level `idleTimeout` / `runTimeout`)](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/src/pregel/utils/timeout.ts) — LangGraph 1.4.8's built-in per-node timeout policy. `idleTimeout` with `refreshOn: "auto"` resets on LangChain callback events (model tokens, tool start/end), writes, and heartbeats — the exact chunk-idle-watchdog mechanism. Raises `NodeTimeoutError` via cooperative AbortSignal cancellation. This is the framework-native implementation path for FR-001–FR-003, discovered during the plan-phase langchain research (see Clarifications).
- [Vercel AI SDK — `timeout.chunkMs` (stream chunk idle timeout)](https://sdk.vercel.ai/docs/reference/ai-sdk-core/stream-text) — the Vercel AI SDK's built-in chunk-idle timeout, used by opencode's `chunkTimeout` propagation (PR #25575). Referenced as the industry-standard approach to silent stream dropout detection.

### Repositories

- [anomalyco/opencode — PR #25575: propagate chunkTimeout to streamText timeout.chunkMs for silent stream dropout detection](https://github.com/anomalyco/opencode/pull/25575) — opencode's fix for the identical failure mode: "Catches silent provider dropouts where TCP connection stays open but SSE chunks stop arriving. Without chunk-level idle timeout, the session hangs indefinitely with no retry triggered." Recommended values: 15–30 seconds.
- [anomalyco/opencode — issue #13841: Explore subagent hangs indefinitely](https://github.com/anomalyco/opencode/issues/13841) — in-depth analysis of the multi-layer timeout gap: "A stream-idle timeout should reset on each received chunk, while a total request timeout is a separate hard ceiling. Both are needed." Documents that individual tools have their own timeouts but "the LLM stream itself has none."
- [anomalyco/opencode — issue #26667: session.processor crashes on unhandled AbortError](https://github.com/anomalyco/opencode/issues/26667) — the importance of graceful AbortError handling: stream interruptions must be caught and cleaned up, not propagated as crashes. Dominion's TurnLoop already has this (the catch terminal), but the principle informs FR-008 (stall must take the error terminal, not crash).
- [openclaw/openclaw — `src/agents/runtime/proxy.ts`](https://github.com/openclaw/openclaw/blob/main/src/agents/runtime/proxy.ts) — OpenClaw's layered timeout pattern: `readIdleTimeoutMs` (chunk-idle), `firstEventTimeoutMs` (time-to-first-byte), `buildProxyRequestAbort(callerSignal, idleTimeoutMs)` (composite abort signal combining caller abort + idle timeout). The composite-signal pattern directly informs the design: the watchdog's abort is combined with the TurnLoop's caller abort into one signal passed to the graph, so either source can terminate the model call.
- [openclaw/openclaw — `src/agents/openai-transport-stream.ts`](https://github.com/openclaw/openclaw/blob/8d535fb0/src/agents/openai-transport-stream.ts) — OpenClaw's `withFirstStreamEventTimeout` and `throwIfModelStreamAborted` patterns for per-event abort checking in the stream loop.
- [NousResearch/hermes-agent — `run_agent.py` `_interruptible_streaming_api_call()`](https://github.com/NousResearch/hermes-agent/blob/main/run_agent.py) — Hermes's interruptible streaming pattern: the API call runs in a background thread while the main loop monitors an abort event, enabling cancellation of a stalled stream.

### In-Repository Sources

- `projects/game/agent/src/turn-loop.ts:254-264,334-392,403-410,413-424` — the TurnLoop state machine: `submit()` (buffer-or-start), `runLoop()` (the `for await` that blocks on a stalled runner), `finishAbort()` (clears buffer, per [Feature 030](../030-queued-chat-input/spec.md) FR-011), `finishError()` (retains buffer, [Feature 030](../030-queued-chat-input/spec.md) FR-015). The stall detection adds a new trigger for `finishError` (FR-008).
- `projects/game/agent/src/session-team.ts:710-897` — `runTeamTurn`: the `for await (const event of stream)` loop that blocks on stalled LLM events; the `signal` passed to `graph.streamEvents` (line 764) is the existing abort-propagation seam the watchdog builds on.
- `projects/game/agent/src/session-team.ts:439-478` — `runInitTurn`: the async init instruction turn using `graph.invoke` (non-streaming, no event-level watchdog applicable — uses a total timeout per FR-009). The existing degrade path (catch → warn → resolve, lines 469-477) is reused for timeout-induced degradation.
- `projects/game/agent/src/team/player.ts:145-197` — the player node's `createAgent` with `gameEndGuard` and `queueDrain` beforeModel middleware; the `queueDrain` middleware (line 171) is where mid-turn drain happens ([Feature 038](../038-queue-input-mid-turn/spec.md)), and its interaction with stall recovery is addressed in Edge Cases.
- `projects/game/agent/src/model-provider.ts:69-91` — the ChatModel factory (`initChatModel`) with no timeout configuration; this feature does not modify the model provider (the watchdog operates at the event-stream level, which is provider-agnostic).
- `projects/game/agent/src/operation-bridge.ts:56` — the 20-minute OperationBridge dispatch timeout (`DISPATCH_TIMEOUT_MS = 1_200_000`), the basis for the tool-execution no-false-stall requirement (FR-003). The dispatch runs inside the MCP server (post-031 production path); `config.heartbeat` cannot reach it across the MCP HTTP boundary, so the heartbeat is driven client-side by the wrapper (research.md R7.2).

### Articles & RFCs

- No external articles or RFCs cited beyond the repository-linked issues and PRs above.

### Related Specifications

- [Feature 030 — Queued Chat Input During Agent Run](../030-queued-chat-input/spec.md) — defines the TurnLoop buffer and its drain/clear semantics. This feature's FR-006/FR-007 (stall retains buffer, auto-drains on next turn) extend the `finishError` path already defined in 030's FR-015 (non-abort error retains buffer). This feature does NOT change 030's FR-011 (user abort clears buffer).
- [Feature 038 — Queued Input Mid-Turn Injection & Bubble Continuity](../038-queue-input-mid-turn/spec.md) — defines the mid-turn drain (`queueDrain` beforeModel middleware). The stall detection's tool-execution heartbeat (FR-003) operates on the same event stream that feeds the mid-turn drain; the two mechanisms are independent and composable (Edge Cases addresses their interaction).
- [Feature 017 — Agent Loop Graceful Abort](../017-agent-loop-graceful-abort/spec.md) — defines the abort behavior. This feature preserves all existing abort semantics (FR-012) and adds stall recovery as a distinct, non-abort termination path.
- [Feature 026 — Agent Abort Crash Fix](../026-agent-abort-crash-fix/spec.md) — defines the `safeWrite` error containment and the stream-close → abort → `finishAbort` path. This feature's stall recovery is a separate path that does not interfere with connection-drop abort.
- [Feature 039 — Planner Memory Calibration](../039-planner-memory-calibration/spec.md) — defines the async init instruction turn (US3, contract §6). This feature's FR-009/FR-010 add a bounded timeout to that turn's execution, ensuring it cannot hang indefinitely.
- [Feature 042 — Planner Memory Fixup](../042-planner-memory-fixup/spec.md) — extends the init instruction turn to refresh-triggered instruction turns. FR-009's timeout applies equally to both team-init and post-refresh instruction turns (they share the same `runInitTurn` / `startInstructionTurn` code path).
