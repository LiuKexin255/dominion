# Feature Specification: Queued Chat Input During Agent Run

**Feature Branch**: `030-queued-chat-input`

**Created**: 2026-07-29

**Status**: Draft

**Input**: User description: "desktop session 对话在 agent 运行期间允许输入，消息改为排队，再下一次 agent turn 添加到 message 中输入到llm当中。可以参考 https://github.com/anomalyco/opencode 在运行中输入排队的逻辑。"

**Relationship**: Extends the conversation-input behavior established by [feature 007 — Dialog Agent](../007-dialog-agent/spec.md) and refined by [feature 015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md). Feature 007's FR-016 mandated a server-side queue for messages arriving while an agent is processing, satisfied today by the per-session FIFO turn mutex; feature 015 made `SendUserTurn` non-blocking. In practice, however, the desktop chat input is *disabled* while an agent turn is in progress (`projects/game/desktop/frontend/src/components/ChatView.svelte:358` — `disabled={processing}`), so a user cannot type or submit anything until the turn completes. This feature changes that: the input stays editable during a run, submitted messages are queued and visibly held, and each queued message is fed into the next agent turn's input to the LLM — mirroring the input-while-running queue behavior of [opencode](https://github.com/anomalyco/opencode).

## Motivation

While the agent is executing a turn — which can be long, spanning many reasoning steps and tool operations — the user may form a follow-up instruction, a correction, or additional context they want the agent to act on next. Today they cannot express it until the agent fully stops and the input re-enables. By the time the turn ends, the user may have forgotten the nuance, or the agent may have already gone down a path the user wanted to redirect. The ability to queue a message while the agent works — and have it automatically become the next turn's input — keeps the human-in-the-loop tight and lets the user "think ahead" of the agent, exactly as opencode's TUI/desktop allows.

A vestigial queue affordance already exists in the desktop frontend (`queueCount` state and a `{n} message(s) queued` indicator at `projects/game/desktop/frontend/src/components/ChatView.svelte:335-339`) but is never activated because the input is disabled mid-turn. This feature activates that path properly and defines the end-to-end semantics for queued input.

## Clarifications

### Session 2026-07-29

- Q: Does a queued message interrupt or alter the currently in-flight agent turn? → A: No. The in-flight turn MUST run to its normal completion (the `wait` signal). A queued message only ever affects the *next* turn, never the current one. This matches the user's phrasing "下一次 agent turn" (the next agent turn) and opencode's behavior.
- Q: When multiple messages are queued during a single run, are they combined into one next-turn input or processed as separate turns? → A: All messages queued during a single in-flight run are combined into the next single agent turn's input (one aggregated turn), preserving submission (FIFO) order and each message's text and any screenshot attachment. (Confirmed during `/speckit.clarify`; supersedes the earlier "one turn per message" assumption.)
- Q: If the system fails to deliver the combined queued input when its turn arrives (e.g., the send to the agent errors), what happens to the queued messages? → A: The messages MUST be retained in the queue and a visible error surfaced; they are NOT dropped. The exact retry strategy (automatic vs manual, retry count, backoff) is a plan-level detail. (Confirmed during `/speckit.clarify`.)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Type and Submit While the Agent Is Working (Priority: P1)

While an agent turn is in progress (the "Agent is typing…" indicator is showing and the turn has not yet emitted its `wait` signal), the user can continue to type into the chat input and submit a message. The input box is not locked, and submitting does not error or get silently dropped. The submitted message is visibly captured as a queued (pending) message so the user knows it was received and will be acted upon, rather than vanishing.

**Why this priority**: This is the core capability the feature exists to deliver — the entire point is removing the "input disabled during run" restriction. Without it, none of the downstream queue/feedback behavior is reachable from the normal UI.

**Independent Test**: Start an agent turn (e.g., ask the agent to do a multi-step task). While the "Agent is typing…" indicator is visible, type a follow-up message and click Send. Verify the input was accepted (not blocked), the message appears as a queued/pending item, and no error is shown.

**Acceptance Scenarios**:

1. **Given** an agent turn is in progress, **When** the user attempts to type into the chat input, **Then** the input is editable and accepts text (it is not disabled).
2. **Given** an agent turn is in progress and the user has typed text, **When** the user clicks Send (or presses the submit shortcut), **Then** the message is accepted without error and is visibly held as a queued message, distinct from the live turn's stream.
3. **Given** an agent turn is in progress, **When** the user submits a queued message, **Then** the in-flight turn continues unchanged — the queued message does not alter, split, or abort the current reasoning/tool stream.

---

### User Story 2 - Queued Message Becomes the Next Turn Automatically (Priority: P1)

When the in-flight agent turn completes (the `wait` / turn-complete signal arrives), if there is a queued message waiting, the system automatically uses that queued message as the user input for the next agent turn — it is added to the message and passed to the LLM — without the user having to click Send again. The user experiences a seamless "queue then continue": they queued an instruction mid-run, and the agent picks it up as soon as it finishes the current turn.

**Why this priority**: This is the payoff of the queue. A queue that only *holds* messages but never *delivers* them is worthless; the automatic hand-off to the next turn is what makes queued input useful. It is co-equal with US1 because the two halves (accept-while-running + deliver-on-next-turn) only deliver value together.

**Independent Test**: Start an agent turn, submit a follow-up message while it runs (per US1), and wait for the turn to complete. Verify that, on completion, a new agent turn begins automatically using the queued message as its input (the queued message appears as the user turn and the agent responds to it), with no second click required from the user.

**Acceptance Scenarios**:

1. **Given** an agent turn is in progress and exactly one message is queued, **When** the in-flight turn emits its `wait` (turn-complete) signal, **Then** the system automatically starts a new agent turn whose user input is the queued message, and the agent's response addresses the queued message's content.
2. **Given** a queued message has just been consumed as the next turn's input, **Then** it is no longer shown as pending/queued — it has transitioned into the normal conversation as the current user turn.
3. **Given** an agent turn completes with NO queued message waiting, **When** the `wait` signal arrives, **Then** the session returns to the normal idle/ready state exactly as today (no spurious empty turn is started) — the queue-hand-off only triggers when the queue is non-empty.

---

### User Story 3 - Multiple Queued Messages Combined Into the Next Turn (Priority: P2)

If the user submits more than one message while an agent turn is in progress, all of those queued messages are combined into the next single agent turn's input — in the exact order the user submitted them — rather than each producing its own separate turn. The user can rely on the combined ordering being preserved regardless of how long the agent takes on the current turn.

**Why this priority**: Once input-while-running is allowed (US1) and auto-delivery works (US2), multi-message queuing is the natural next case a user will hit. It is secondary because the single-queued-message case (US1+US2) is the dominant usage; multi-message combination is an ordering/aggregation guarantee layered on top.

**Independent Test**: Start a long agent turn. Submit message A, then message B, then message C while it runs. Verify that, when the turn completes, exactly ONE next turn begins whose combined input contains A, B, and C in that order, and the agent's single response addresses all three.

**Acceptance Scenarios**:

1. **Given** messages A, B, and C are queued during a single in-flight turn (submitted in that order), **When** the turn completes, **Then** exactly ONE next agent turn begins whose input combines A, B, and C in FIFO order (not three separate turns).
2. **Given** one combined turn is in progress (built from earlier queued messages), **When** the user submits another message during that new turn, **Then** the newly submitted message is held in the queue and combined into the next turn's input after the current one completes.

---

### User Story 4 - Clear Visibility and Control of the Queue (Priority: P3)

The user can always tell whether there are messages waiting in the queue (a count or list of pending messages is shown), and can remove a queued message before it is consumed if they change their mind. This prevents the user from accidentally feeding a now-unwanted instruction to the agent and makes the queue state transparent.

**Why this priority**: Transparency and the ability to undo a premature submission improve trust in the queue, but the feature delivers its core value (US1–US2) without edit/remove affordances. This is a usability hardening on top of the core flow.

**Independent Test**: Queue a message during a run; verify the queue indicator shows the pending message; remove it; verify the indicator clears and the message is not delivered when the turn completes.

**Acceptance Scenarios**:

1. **Given** one or more messages are queued, **When** the user views the chat, **Then** the number (and ideally the content) of queued messages is clearly indicated, distinct from already-sent and currently-streaming messages.
2. **Given** a message is queued but not yet consumed, **When** the user removes/cancels it, **Then** it is removed from the queue and is NOT delivered as a subsequent turn.

---

### Edge Cases

- **Queue a message, then the turn finishes almost immediately**: the user submits a message a split second before the `wait` signal arrives. The message MUST still be treated as queued-for-the-next-turn and not be folded into the just-finishing turn; it becomes the input of the next turn. (The turn boundary defined by the `wait` signal is the sole delimiter.)
- **Queue a message with an attached screenshot during a run**: the screenshot attachment must be preserved with the queued message and delivered as part of that next turn's input to the LLM, exactly as a normally-sent screenshot-bearing message would be.
- **User aborts/stops the current turn while messages are queued**: an explicit user abort (the stop control, per [feature 017](../017-agent-loop-graceful-abort/spec.md)) is treated as a full stop — queued messages MUST be discarded, not carried into an automatically-started next turn. The rationale is that an abort signals the user wants to halt; auto-continuing into queued work would contradict that intent. (If the turn ends by *normal* completion, queued messages are delivered per US2.)
- **Desktop disconnects mid-turn with messages queued**: queued-but-unsent messages are client-side state. On reconnect to the same session, the queue MUST be retained (the user does not lose what they typed) and the normal hand-off semantics resume. If the desktop is closed entirely, queued-unsent messages are lost (they were never delivered to the agent); persisting them across app restarts is out of scope.
- **Very large queued message or many queued messages**: the queue must not block the live turn's rendering or cause the UI to stall; queued messages are held until their turn, not rendered into the live stream.
- **Queued message that supersedes an earlier queued message**: when multiple messages are queued and one is a correction of an earlier one, both are still combined into the next turn's input in submission order (the agent sees both). To avoid sending a now-unwanted instruction, the user must use the remove affordance (FR-010) to drop the superseded message before the hand-off.
- **Hand-off delivery fails**: if the send of the combined queued input errors when its turn arrives, the queued messages MUST be retained (not dropped) and a visible error surfaced (FR-015); the user does not lose what they queued. The recovery mechanism (automatic retry vs a manual retry affordance) is a plan-level concern.
- **Queued message arrives at the agent while it is between turns (idle)**: if the message is submitted during the idle window between turns, it is delivered as a normal turn (there is no in-flight turn to queue behind); the queue only matters relative to an in-flight turn.
- **Session re-entry with a non-empty queue**: when the user re-enters a session, the queue indicator must reflect only messages genuinely still pending for that session; it must not show a stale count from a prior view.

## Requirements *(mandatory)*

### Functional Requirements

**Input Enabled During Run**

- **FR-001**: The desktop chat input MUST remain editable and submittable while an agent turn is in progress. The input MUST NOT be disabled solely because the agent is processing. (Today the input is `disabled={processing}` at `projects/game/desktop/frontend/src/components/ChatView.svelte:358`; this restriction MUST be removed for the text input and the Send control.)
- **FR-002**: Submitting a message while an agent turn is in progress MUST NOT interrupt, alter, split, or abort the in-flight turn. The in-flight turn MUST continue to its normal completion (the `wait` / turn-complete signal), identical to how it would run with no message queued.

**Queuing Semantics**

- **FR-003**: A message submitted while an agent turn is in progress MUST be held in an ordered queue rather than being processed as part of the in-flight turn or being discarded. The queue is per-session.
- **FR-004**: Queued messages MUST preserve First-In-First-Out (FIFO) submission order when combined into the next turn's input — the order in which the user submitted them — regardless of how long the in-flight turn takes.
- **FR-005**: All messages queued during a single in-flight turn MUST be combined into the next single agent turn's input (one aggregated turn), preserving their submission (FIFO) order and each message's text and any screenshot attachment. That combined input is what is passed to the LLM for the next turn.
- **FR-006**: When the in-flight agent turn completes (`wait` signal) and the queue is non-empty, the system MUST automatically start ONE next agent turn whose input combines ALL currently-queued messages (in FIFO order), WITHOUT requiring the user to submit again. When the queue is empty at turn completion, the system MUST return to the normal idle/ready state and MUST NOT start an empty turn.
- **FR-007**: A message submitted while there is no in-flight turn (the session is idle) MUST be delivered as a normal turn immediately, indistinguishable from today's behavior — the queue only alters behavior relative to an in-flight turn.

**Visibility and Control**

- **FR-008**: The desktop MUST visibly indicate when one or more messages are queued (e.g., a count and/or a pending representation of each queued message), so the user can tell their input was captured and is pending. (The existing `queueCount` affordance and `.queue-indicator` at `projects/game/desktop/frontend/src/components/ChatView.svelte:335-339` is the intended surface; it MUST reflect genuinely-queued messages.)
- **FR-009**: Queued messages that have been consumed as the next turn's combined input MUST transition out of the pending/queued representation into the normal conversation view as the user turn(s); they MUST NOT remain shown as queued.
- **FR-010**: The user MUST be able to remove a queued message before it is consumed, and a removed message MUST NOT be delivered as a subsequent turn.

**Lifecycle and Scope Boundaries**

- **FR-011**: An explicit user abort of the in-flight turn (the stop control) MUST discard all currently-queued messages; the system MUST NOT auto-continue into queued work after an abort. (Contrast FR-006, which governs normal turn completion.)
- **FR-012**: A screenshot attachment submitted with a queued message MUST be preserved and delivered as part of that queued message's subsequent turn input, behaving identically to a screenshot attached to a normally-sent message.
- **FR-013**: The existing turn-completion signal (`wait` signal, emitted at the end of `generateTurn`) MUST remain the sole delimiter of a turn boundary for the purposes of queue hand-off; queued messages MUST be handed off only at `wait`, never mid-turn. **[SUPERSEDED — see Feature 038]**: [Feature 038 — Queued Input Mid-Turn Injection & Bubble Continuity](../038-queue-input-mid-turn/spec.md) FR-002 supersedes this requirement — queued messages are now handed off at the earliest mid-turn delivery point (the turn's first reasoning step or the reasoning step following a tool-result boundary), falling back to the `wait` boundary when the turn involves no tool calls or the message arrives after the turn's last reasoning step. The `wait` boundary remains the fallback hand-off point. The authoritative contract for the mid-turn drain is [feature 038's TurnLoop drain contract](../038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md).
- **FR-014**: This feature MUST NOT change the agent's per-session FIFO turn serialization, the `SendUserTurn` non-blocking contract (feature 015), the graceful-abort mechanism (feature 017), or the session status reconciliation on re-entry (feature 021). These behaviors MUST remain intact; the queue is layered on top of them.
- **FR-015**: If delivery of the combined queued input fails when its turn arrives (e.g., the send to the agent errors), the system MUST retain those messages in the queue and MUST surface a visible error; it MUST NOT silently drop the queued messages. (The retry strategy — automatic vs manual, count, backoff — is a plan-level detail.)

### Key Entities *(include if feature involves data)*

- **Queued Message**: A user-authored message (text, optionally with a screenshot attachment) submitted while an agent turn is in progress. It is held in a per-session ordered queue, visibly pending, and is later combined with all other queued messages (in FIFO order) into the input of a single next agent turn. Its lifecycle is: submitted-during-run → queued (pending, visible, removable) → consumed (combined with all other queued messages, in FIFO order, into the next turn's single input) → rendered in the normal conversation view.
- **Input Queue (per session)**: The ordered (FIFO) holding structure for Queued Messages belonging to one session. It is non-empty only while an agent turn is in flight; at idle it is empty. On each `wait` boundary, if the queue is non-empty, its entire contents are combined (in FIFO order) into the input of a single next turn, then the queue is cleared. It is the entity that US2/US3 drain on each `wait` boundary.
- **Turn Boundary (`wait` signal)**: The existing turn-complete signal emitted at the end of an agent turn. This feature treats the `wait` boundary as the single hand-off point: it is the moment at which the queued messages (if any) are combined and promoted into a single new turn. This feature does not change what the `wait` signal means; it changes what the system does upon receiving it when the queue is non-empty.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: While an agent turn is in progress, a user can type and submit a message in 100% of attempts — the input is never disabled by the processing state — and every such submission is visibly captured as a queued message (zero silent drops).
- **SC-002**: In 100% of cases, a message queued during a run does not alter the in-flight turn: the in-flight turn's streamed content, tool operations, and closing `wait` signal are identical to a run with nothing queued.
- **SC-003**: When an agent turn completes with exactly one queued message waiting, a new agent turn begins automatically using that queued message as input in 100% of cases, with no second submission required, and the agent's response addresses the queued message's content.
- **SC-004**: When multiple messages are queued during a single run, they are combined into ONE next turn whose input preserves FIFO submission order in 100% of cases (no reordering, no loss of any message's content or attachment).
- **SC-005**: An explicit user abort discards all queued messages in 100% of cases; no auto-continued turn occurs after an abort.
- **SC-006**: Non-queued behavior is unchanged: messages submitted while the session is idle are delivered as normal turns identical to today, the `wait` signal still closes a turn, and the existing per-session FIFO serialization, graceful-abort, and re-entry reconciliation exhibit zero regressions.

## Assumptions

- This feature extends the existing conversation model from [feature 007](../007-dialog-agent/spec.md) (FR-016's server-side FIFO queue, satisfied by the per-session turn mutex at `projects/game/agent/src/handler.ts:60-99`) and the non-blocking `SendUserTurn` from [feature 015](../015-desktop-agent-refinement/spec.md). Those mechanisms are retained; this feature adds client-side queueing + automatic next-turn hand-off.
- The turn boundary is defined by the existing `wait` signal (emitted at the end of `generateTurn`, e.g. `projects/game/agent/src/handler.ts`), which the desktop already uses to clear its `processing` state. This feature consumes that same boundary as the queue hand-off point; it does not introduce a new boundary signal.
- "Next agent turn" means the next full `generateTurn` invocation (the one that starts after the current turn's `wait`), NOT an injection mid-turn into the current LLM step. Queued input is never spliced into the in-flight turn's tool/reasoning loop. **[SUPERSEDED — see Feature 038]**: [Feature 038](../038-queue-input-mid-turn/spec.md) introduces mid-turn injection of queued input at reasoning-step boundaries (the turn's first reasoning step and each reasoning step following a tool-result boundary); the `wait`-boundary hand-off remains as the fallback for no-tool turns and late arrivals. The authoritative contract for the mid-turn drain is [feature 038's TurnLoop drain contract](../038-queue-input-mid-turn/contracts/turn-loop-drain-contract.md).
- All messages queued during a single in-flight run are combined into the next single agent turn's input in FIFO order (FR-005), as confirmed during `/speckit.clarify`. The exact merge representation (e.g., how multiple text bodies and multiple screenshot attachments are assembled into one user message) is a plan-phase implementation concern; the spec only requires that submission order and each message's content/attachment are preserved.
- An explicit user abort (stop control) discards the queue (FR-011); only normal turn completion triggers queue hand-off. This treats "abort" as a hard stop, consistent with the user's intent to halt.
- Queued-but-unsent messages are client-side (desktop frontend) state. They are retained across a mid-session reconnect to the same session but are NOT persisted across a full application restart; cross-restart persistence is out of scope.
- Where the queue physically lives (frontend client-side queue that dispatches on `wait`, vs. backend-side queue extending the existing mutex) is an implementation decision for `plan.md`, to be resolved per Constitution §III. The spec only fixes the observable behavior (accept-while-run, hold-and-indicate, FIFO, auto-hand-off on `wait`, discard-on-abort). The existing `queueCount` / `.queue-indicator` frontend affordance (`projects/game/desktop/frontend/src/components/ChatView.svelte:335-339`, `projects/game/desktop/frontend/src/App.svelte:78`) is the intended visible surface but its final shape is a plan-phase concern.
- The opencode reference ([https://github.com/anomalyco/opencode](https://github.com/anomalyco/opencode)) is cited as the behavioral inspiration for input-while-running queuing; dominion's architecture (gRPC bidi stream, LangGraph checkpointer, `wait` signal) differs from opencode's, so the reference informs behavior, not implementation. Exact opencode source locations (session/prompt/run-state modules) will be re-examined in `plan.md` per Constitution §III.
- This feature is scoped to the desktop session conversation only. It does not change how the agent service processes a single turn, how conversation history is checkpointed, or any non-chat frontend event delivery (per the scope boundary of [feature 016](../016-desktop-sse-chat-push/spec.md)).

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- No third-party official API documentation is depended upon at the specification phase. The LangGraph/LangChain turn and `streamEvents` contracts underpin the existing turn model and remain unchanged; specific version-pinned references will be added in `plan.md` per Constitution §III if implementation touches them.

### Repositories

- [anomalyco/opencode — the open source coding agent](https://github.com/anomalyco/opencode) — the reference explicitly named in the feature request as the model for "input while running is queued" behavior. Its session/prompt/run-state modules (`packages/opencode/src/session/`) inform the behavioral design (type-and-queue while a run is active; deliver the queued input on the next prompt). Cited as behavioral inspiration only; dominion's transport and turn model differ.

### In-Repository Sources

- `projects/game/desktop/frontend/src/components/ChatView.svelte:335-368` — the chat input that is currently `disabled={processing}` (line 358), and the existing `queueCount` / `.queue-indicator` affordance (lines 335-339) that this feature activates properly.
- `projects/game/desktop/frontend/src/App.svelte:78,627-655` — the `processing` state, the existing `queueCount` state, and `handleSendChatText` (which currently sends immediately and only nominally touches `queueCount`).
- `projects/game/agent/src/handler.ts:60-99` — the per-session FIFO turn mutex (`acquireMutex`/`releaseMutex`) that already serializes concurrent user turns (the server-side realization of feature 007 FR-016); retained as the serialization safety net.
- `projects/game/game.proto:90,482-504,340-342,448-450` — the `Connect` bidi stream, `AgentFrame`, `FlowParts`, and `WaitSignal` definitions; the `wait` signal is the turn boundary this feature uses for queue hand-off.

### Articles & RFCs

- No external articles or RFCs cited.

### Related Specifications

- [Feature 007 — Dialog Agent](../007-dialog-agent/spec.md) (FR-016: queue messages sent while processing) — the original queue requirement this feature makes user-facing.
- [Feature 015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md) — established the non-blocking `SendUserTurn`; this feature preserves that contract.
- [Feature 016 — Desktop SSE Chat Push](../016-desktop-sse-chat-push/spec.md) — the chat message delivery channel; queued messages must render through the same path once delivered.
- [Feature 017 — Agent Loop Graceful Abort](../017-agent-loop-graceful-abort/spec.md) — defines the abort behavior this feature interacts with (FR-011: abort discards the queue).
- [Feature 038 — Queued Input Mid-Turn Injection & Bubble Continuity](../038-queue-input-mid-turn/spec.md) — extends this feature: supersedes FR-013 and the "next agent turn" assumption by adding mid-turn delivery of queued messages at reasoning-step boundaries (tool-result boundaries and the turn's first reasoning step), with the `wait` boundary retained as fallback; also fixes a rendering defect that split the agent's streaming bubble when a message is queued mid-stream.
- [Feature 021 — Agent Session Resync](../021-agent-session-resync/spec.md) — defines session re-entry status reconciliation; queue state must remain consistent with it.
