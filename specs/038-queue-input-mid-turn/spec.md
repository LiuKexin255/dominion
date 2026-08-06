# Feature Specification: Queued Input Mid-Turn Injection & Bubble Continuity

**Feature Branch**: `038-queue-input-mid-turn`

**Created**: 2026-08-06

**Status**: Draft

**Revision**: 2026-08-06 (v2) — aligned the mid-turn delivery-point definitions (FR-001, FR-004, Key Entities, edge cases) with the design's first-reasoning-step injection point (resolved analyze finding I1); added supersession notes to spec 030 FR-013 and its "next turn" assumption (resolved analyze finding I3).

**Input**: User description: "desktop agent 输出时 user 输入消息排队存在两个问题：1) user 输出会打断 session 对话页面当前的气泡（如 think 被拆成两个气泡）；2) user message 排队后不会在下次 tool result 返回时跟 result 一起返回给 agent，而是一直排队等待 llm 停下。参考 opencode 的处理方式。"

**Relationship**: Extends and partially supersedes [feature 030 — Queued Chat Input During Agent Run](../030-queued-chat-input/spec.md). Feature 030 established queued chat input (accept-while-running, FIFO buffer, auto-hand-off on turn completion). This feature fixes two behavioral defects in that implementation: (1) a rendering bug where a queued user message splits the agent's streaming text/thinking bubble into two separate bubbles (violating spec 030 SC-002), and (2) a latency defect where queued messages are held until the *entire* turn completes rather than being delivered at the next tool-result boundary within the turn. The behavioral change in item (2) **supersedes spec 030 FR-013** ("queued messages MUST be handed off only at `wait`, never mid-turn") — queued messages are now handed off at the earliest mid-turn delivery point within the turn — the turn's first reasoning step, or the reasoning step immediately following a tool-result boundary — falling back to the `wait` boundary when the turn involves no tool calls or the message arrives after the turn's last reasoning step.

## Motivation

When an agent is mid-turn — reasoning, calling tools, and processing results — a user who queues a follow-up message expects the agent to act on it *promptly*. Today the message sits idle in the queue until the agent's entire turn (which may span many tool calls and reasoning steps) finishes and emits its `wait` signal. If the user queued a correction or additional context, the agent may have already proceeded several steps down a path the user wanted to redirect — by the time the queued message is delivered, the opportunity to steer has passed.

This directly contradicts the reference behavior of [opencode](https://github.com/anomalyco/opencode), which delivers queued ("steer") messages at the next provider-turn boundary (after tools complete, before the next reasoning step), keeping the human-in-the-loop tight.

A second, separate defect compounds the frustration: when the user submits a message during the agent's streaming output (e.g., while thinking content is streaming), the optimistic user-message insertion **splits** the agent's continuous text/thinking bubble into two visually disconnected bubbles. The user sees a fragmented conversation rather than one coherent agent response with their queued message below it.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Queued Message Delivered at Next Tool Result (Priority: P1)

While an agent turn is in progress and the agent has just completed a tool call (receiving a tool result), a message the user queued earlier is delivered to the agent alongside that tool result — the agent sees both the tool outcome and the user's follow-up in its next reasoning step, and acts on the user's input immediately rather than waiting for the entire turn to finish. The user experiences responsive steering: they type a correction mid-run, and the agent picks it up at the very next decision point.

**Why this priority**: This is the core behavioral change. Without mid-turn delivery, the queue is only useful for after-the-fact follow-ups; the ability to *steer* a running agent — the entire value proposition of input-while-running — depends on prompt delivery at tool boundaries. This is co-equal with US2 because both halves (responsive delivery + clean rendering) must work together for the feature to feel correct.

**Independent Test**: Start an agent turn that involves multiple tool calls. While the agent is executing its first tool, queue a follow-up message. Verify that after the tool result returns, the agent's next reasoning step addresses the queued message (not after the full turn completes).

**Acceptance Scenarios**:

1. **Given** an agent turn is in progress with the agent making tool calls, **When** the user queues a message while a tool is executing, **Then** after the tool result is returned to the agent, the agent's next reasoning step incorporates both the tool result and the queued user message — the agent does NOT wait until the full turn completes.
2. **Given** the agent makes several sequential tool calls within a single turn, **When** the user queues a message between two tool calls, **Then** the message is delivered at the boundary between those tool calls (before the agent's next reasoning step after the intervening tool result).
3. **Given** a queued message has been consumed mid-turn (injected alongside a tool result), **Then** it transitions out of the pending/queued visual state immediately (the queue indicator decrements at consumption, not at turn end).

---

### User Story 2 - Agent Bubble Stays Continuous (Priority: P1)

When the user submits a message during the agent's streaming output (text or thinking), the agent's streaming bubble must NOT be split into two separate bubbles. The agent's continuous text/thinking output remains one coherent bubble in its original position; the queued user message appears below it as a separate, pending item. The user sees a clean conversation: one agent response, their queued message beneath it.

**Why this priority**: A split bubble is a visible, confusing rendering defect that directly violates the promise that queued messages "do not alter the in-flight turn's streamed content" (spec 030 SC-002). Even if mid-turn delivery works perfectly, a fragmented conversation view undermines user trust. Co-equal with US1.

**Independent Test**: Start an agent turn that produces streaming thinking output. While thinking is streaming, submit a queued message. Verify the thinking content appears as ONE continuous bubble (not two), with the user's message below it as a pending item.

**Acceptance Scenarios**:

1. **Given** the agent is streaming thinking content (one continuous thinking bubble), **When** the user submits a queued message mid-stream, **Then** subsequent thinking chunks merge into the SAME bubble — the thinking content is NOT split into two bubbles separated by the user message.
2. **Given** the agent is streaming text content (one continuous text bubble), **When** the user submits a queued message mid-stream, **Then** subsequent text chunks merge into the SAME bubble — the text content is NOT split.
3. **Given** the agent's streaming bubble has been split in the current implementation, **Then** after this fix the same scenario produces one continuous bubble with the user message below it.

---

### User Story 3 - Fallback When No Tools Are Called (Priority: P2)

If the agent's turn does not involve any tool calls (pure reasoning that completes without invoking tools), queued messages cannot be delivered mid-turn (there is no tool-result boundary). In this case, queued messages MUST still be delivered at the turn-completion boundary (the existing `wait` signal behavior from spec 030), so the user's input is never lost — just deferred to the next available boundary.

**Why this priority**: This is a fallback safety net. The primary path (tool-based turns) is covered by US1; this ensures the no-tools edge case degrades gracefully to the existing behavior rather than dropping messages.

**Independent Test**: Start an agent turn where the agent reasons and responds without calling any tools. Queue a message during the turn. Verify the message is delivered as the next turn's input after the current turn completes (the existing spec 030 behavior).

**Acceptance Scenarios**:

1. **Given** an agent turn that completes without calling any tools, **When** the user queues a message during the turn, **Then** the message is delivered at the turn-completion boundary as the next turn's input (the existing spec 030 FR-006 behavior — unchanged).
2. **Given** a turn that calls tools initially but then stops calling tools and finishes with pure reasoning, **When** the user queues a message after the last tool result, **Then** the message is delivered at the turn-completion boundary (there is no subsequent tool boundary to inject at).

---

### Edge Cases

- **Message queued in the gap between tool result and the next reasoning step**: the message arrives a split second after a tool result is processed. It MUST be injected at the *next* tool-result boundary (if the agent calls more tools) or at the turn boundary (if the turn completes). It MUST NOT be lost or duplicated.
- **Message queued before the agent's first reasoning step**: a message submitted after the turn starts but before the agent begins its first reasoning step MUST be injected at that first reasoning step — the earliest mid-turn delivery point, reached before any tool-result boundary.
- **Multiple messages queued between two tool calls**: all messages queued in the window between one tool result and the next reasoning step MUST be combined into a single injected user message (FIFO order preserved), identical to the multi-message combination at turn boundaries (spec 030 FR-005).
- **User aborts mid-turn after a message was injected**: an explicit user abort (stop control) discards all *remaining* queued messages. Messages already injected into the agent's context before the abort are not rolled back — the abort halts the turn but does not undo consumed input (consistent with spec 030 FR-011 semantics for the buffer).
- **Queued message with a screenshot attachment**: the screenshot MUST be preserved and delivered as part of the mid-turn injection, identical to a normally-sent screenshot-bearing message (spec 030 FR-012).
- **Agent turn involves no tool calls**: see US3 — queued messages fall back to the turn-completion boundary.
- **Message queued right as the turn is about to complete**: if the message arrives after the last tool-result boundary but before the `wait` signal, it MUST be delivered at the turn-completion boundary (the turn-end drain, unchanged from spec 030).
- **Queue indicator timing**: the pending/queued indicator MUST decrement at the moment a message is consumed (injected mid-turn or handed off at turn end), not at some later signal. The user must see the indicator update as soon as the message leaves the queue.

## Requirements *(mandatory)*

### Functional Requirements

**Mid-Turn Delivery**

- **FR-001**: A message queued while an agent turn is in progress MUST be delivered to the agent at the earliest mid-turn delivery point within that turn — the turn's first reasoning step (for messages queued before the agent begins its first reasoning step) or the reasoning step immediately following a tool-result boundary (for messages queued during tool execution, at the point where the agent has finished processing one or more tool results and is about to reason again). This delivery MUST happen without waiting for the full turn to complete.
- **FR-002**: This requirement **supersedes spec 030 FR-013** ("queued messages MUST be handed off only at `wait`, never mid-turn"). Queued messages are now handed off at the earliest mid-turn delivery point — the turn's first reasoning step or the reasoning step following a tool-result boundary — falling back to the `wait` boundary when no tools are called during the turn or the message arrives after the turn's last reasoning step.
- **FR-003**: When multiple messages are queued between two consecutive tool-result boundaries, they MUST be combined into one aggregated user message (FIFO order preserved) and injected together at the next boundary — identical combination semantics to spec 030 FR-005, applied at the finer mid-turn granularity.
- **FR-004**: When the agent's turn does not involve any tool calls, there is no tool-result boundary to inject at; queued messages that arrive after the turn's single reasoning step MUST be delivered at the turn-completion boundary (the `wait` signal), preserving the existing spec 030 FR-006 behavior unchanged. This does not preclude the FR-001 first-reasoning-step injection for messages queued before the turn's first reasoning step.

**Rendering Continuity**

- **FR-005**: The agent's streaming text output MUST appear as one continuous bubble when a user message is submitted mid-stream. The streaming merge logic MUST NOT create a new bubble solely because a user message was interleaved between two streaming chunks of the same kind (text or thinking).
- **FR-006**: The agent's streaming thinking output MUST appear as one continuous bubble under the same conditions as FR-005.
- **FR-007**: The queued user message MUST appear as a separate item below the agent's continuous bubble, visually marked as pending until consumed. This satisfies spec 030 FR-008 at the rendering level.

**Queue Visibility Timing**

- **FR-008**: The pending/queued visual indicator MUST reflect the true queue depth at all times. When a message is consumed (injected mid-turn or handed off at turn end), the indicator MUST decrement immediately, not at a later signal.

**Lifecycle and Scope Boundaries**

- **FR-009**: An explicit user abort (stop control) MUST discard all remaining queued messages (those not yet consumed), consistent with spec 030 FR-011. Messages already consumed (injected into the agent's context) are not rolled back.
- **FR-010**: Screenshot attachments on queued messages MUST be preserved through mid-turn injection, identical to turn-boundary delivery (spec 030 FR-012).
- **FR-011**: This feature MUST NOT alter the agent's per-session FIFO turn serialization, the `SendUserTurn` non-blocking contract (spec 015), the graceful-abort mechanism (spec 017), or the session status reconciliation on re-entry (spec 021).
- **FR-012**: The mid-turn injection MUST NOT disturb or interrupt the agent's in-flight tool execution. Injected messages take effect at the next reasoning step boundary, never mid-tool.

### Key Entities *(include if feature involves data)*

- **Tool-Result Boundary (Mid-Turn)**: The point within a running agent turn when the agent has completed processing one or more tool results and is about to begin its next reasoning step. Each tool-result boundary is a mid-turn delivery point for queued messages. A turn with N tool-call rounds has N tool-result boundaries. These boundaries, together with the turn's first reasoning step (the earliest delivery point, present in every turn), replace spec 030's "wait signal only" as the primary hand-off point for turns that involve tools.
- **Turn-Completion Boundary (`wait` signal)**: The existing turn-complete signal (spec 030). Remains the fallback hand-off point when a turn involves no tool calls, or for messages that arrive after the final tool-result boundary but before the turn ends.
- **Queued Message**: (extends spec 030's entity) A user-authored message held in the per-session FIFO queue. Its lifecycle is now: submitted-during-run → queued (pending, visible) → consumed at the next mid-turn delivery point (the turn's first reasoning step or a tool-result boundary, whichever is reached first) or turn-completion boundary (fallback) → rendered in the normal conversation view.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of cases where the agent calls tools during a turn, a message queued while a tool is executing is delivered to the agent's next reasoning step after the tool result — the agent incorporates the queued message before the full turn completes (zero cases of unnecessary turn-end delay).
- **SC-002**: In 100% of cases, submitting a user message during the agent's streaming text or thinking output does NOT split the agent's bubble — the streaming content remains one continuous bubble with the user message below it.
- **SC-003**: In 100% of cases where the agent's turn involves no tool calls, queued messages are delivered at the turn-completion boundary with zero regressions to the existing spec 030 behavior.
- **SC-004**: The pending/queued indicator decrements at the exact moment a message is consumed (mid-turn or turn-end), in 100% of cases — the indicator never shows a stale count after a message has left the queue.
- **SC-005**: An explicit user abort discards all unconsumed queued messages in 100% of cases; no auto-continued turn occurs from unconsumed messages after an abort.

## Assumptions

- This feature extends the existing queued-chat-input infrastructure from [feature 030](../030-queued-chat-input/spec.md) (per-session FIFO queue, `QueueSignal` flow-part, optimistic frontend insertion). Those mechanisms are retained; this feature changes the delivery timing and fixes a rendering defect.
- Mid-turn delivery occurs at "reasoning-step boundaries" within the agent turn: the turn's first reasoning step (the earliest delivery point, present in every turn) and each reasoning step that follows a tool-result boundary — the natural seam between tool execution and the next reasoning step. Tool-result boundaries exist in any turn where the agent calls at least one tool; the first-reasoning-step boundary exists in every turn.
- opencode's "steer" delivery mode ([https://github.com/anomalyco/opencode](https://github.com/anomalyco/opencode)) is the behavioral reference for mid-turn injection. dominion's architecture (LangGraph team graph, `createAgent` internal loop, `streamEvents`) differs from opencode's provider-turn loop, so the reference informs the *behavior* (prompt delivery at tool boundaries), not the implementation.
- The existing turn-completion drain (spec 030's `wait`-boundary hand-off) is retained as a fallback. Mid-turn injection is an *additional* earlier drain point; it does not remove the turn-end drain.
- The rendering fix (FR-005/FR-006) applies to the desktop session chat view (`projects/game/desktop/frontend/src/components/ChatView.svelte` / `App.svelte`). It does not change the data model — it changes only how streaming chunks are merged into bubbles.
- Abort semantics (FR-009) are consistent with spec 030 FR-011: the abort discards the unconsumed queue. Messages already injected mid-turn are part of the agent's context and cannot be "un-injected"; the abort stops the turn.
- The mid-turn injection does NOT apply to agents that do not accept user input (e.g., saolei `planner`, spec 031 FR-031). The `drainQueuedInput` mechanism is scoped to the primary input-accepting agent (`player`).

## References *(mandatory per Constitution §I — Citation & Provenance)*

### Repositories

- [anomalyco/opencode — the open source coding agent](https://github.com/anomalyco/opencode) — behavioral reference for mid-turn ("steer") message delivery. Relevant source: `packages/core/src/session/input.ts` (`promoteSteers` / `promoteNextQueued`), `packages/core/src/session/runner/llm.ts` (the `run` loop checking for steers after each provider turn). Cited as behavioral inspiration; dominion's LangGraph architecture differs.

### In-Repository Sources

- `specs/030-queued-chat-input/spec.md` — the feature this extends and partially supersedes (FR-013 supersession documented in FR-002 above).
- `specs/030-queued-chat-input/contracts/turn-loop-contract.md` — the TurnLoop contract whose loop body drain timing is changed by this feature.
- `specs/030-queued-chat-input/contracts/queue-channel-contract.md` — the QueueSignal channel contract; this feature adds a mid-turn QueueSignal(0) emission point.
- `projects/game/agent/src/turn-loop.ts` — the TurnLoop implementation whose `runLoop` drain (line 356) currently fires only at turn-end.
- `projects/game/agent/src/session-team.ts:286-466` — `runTeamTurn` (the graph-invoke runner) and its `configurable` object (the injection seam for a drain callback).
- `projects/game/agent/src/team/player.ts:148-170` — the player's `createAgent` middleware (`gameEndGuard` `beforeModel` hook) — proves the `beforeModel` middleware mechanism is already in use.
- `projects/game/desktop/frontend/src/App.svelte:708-759` — `handleMessageParts` (the streaming merge logic with the bubble-split defect).

### Related Specifications

- [Feature 030 — Queued Chat Input During Agent Run](../030-queued-chat-input/spec.md) — the base feature; FR-013 is superseded by this feature's FR-002.
- [Feature 015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md) — `SendUserTurn` non-blocking contract (preserved).
- [Feature 017 — Agent Loop Graceful Abort](../017-agent-loop-graceful-abort/spec.md) — abort behavior (FR-009 interaction).
- [Feature 031 — Team Template Mode](../031-team-template-mode/spec.md) — team agent architecture (`player`/`planner`, accepts-user-input flag FR-031/FR-032).
