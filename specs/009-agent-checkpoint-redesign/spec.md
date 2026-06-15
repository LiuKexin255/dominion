# Feature Specification: Agent Checkpoint & Session UI Redesign

**Feature Branch**: `009-agent-checkpoint-redesign`

**Created**: 2026-06-14

**Status**: Draft

**Input**: User description: "使用 LangGraph 原生能力，对 DialogRuntime 进行重构或移除，不要手动实现 LangGraph 已有能力。使用 sessionId 当 thread_id 注意确保 agent 可以被正常删除/重建，不会遗留之前的数据。增加一个自定义 API 接口获取 agent 历史。需求：1. 对 UI/UX 设计状态流转图并进行重构和优化 2. 重构 agent 框架以支持 checkpoint 和断点续跑。本阶段不要求持久化 3. 修复已经发现的问题和 bug"

## Clarifications

### Session 2026-06-14

- Q: Should the agent messages API be a new dedicated request-response method, or reuse the existing WebSocket connection by replaying prior messages on connect? → A: A new unary gRPC method `ListMessages` on the agent service, proxied through the gateway as a REST GET endpoint. Messages are a first-class resource (`sessions/{session_id}/agent/messages/{message_id}`) addressed by the native LangChain `BaseMessage.id`, matching the existing GetAgent/CreateAgent request-response pattern.
- Q: At which point in the operator's flow should the automatic WebSocket connection be established? → A: On play page entry — the session detail page stays lightweight (metadata only, no connection); the connection establishes as part of entering the chat interface, with a fallback auto-connect if a message is sent while not yet connected.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - State-Driven Session & Play UI (Priority: P1)

A game operator selects a session from the list and enters the session detail page. The page adapts its layout entirely to the current agent state rather than showing all controls at once. When no agent exists yet, the operator sees only a profile selector and a create button. Once an agent exists, the page switches to showing an agent summary card (name, profile, creation time) and an "Enter Play" button — the profile selector and manual connect button disappear entirely. The session detail page does not open a network connection; it only displays metadata.

In the play page, the system automatically establishes the connection as part of page entry. The sidebar shows the current agent's key details (name, profile, model) and a button to view the full profile configuration. The operator never needs to manually click a "connect" button. The operator can leave the play page, return to the session later, and the agent is still present and ready.

**Why this priority**: The current session detail page shows all controls simultaneously regardless of state, causing operator confusion (e.g., seeing a profile selector even after an agent is already created and connected). The play sidebar lacks agent context entirely. This is the most visible problem and directly impacts daily usability.

**Independent Test**: Can be fully tested by selecting a session with no agent (verify only profile selector appears), creating an agent (verify UI switches to agent summary with Enter Play button), entering play (verify connection auto-establishes and sidebar shows agent details), and returning to the session later (verify agent is still present without recreation).

**Acceptance Scenarios**:

1. **Given** a session with no agent created, **When** the operator opens the session detail page, **Then** only the session info, profile selector, and "Create Agent" button are visible — no connect button, no agent summary.
2. **Given** a session with an existing agent, **When** the operator opens the session detail page, **Then** the page shows an agent summary card (name, profile, creation time) and an enabled "Enter Play" button — the profile selector is hidden and no network connection is opened.
3. **Given** a session with an existing agent, **When** the operator clicks "Enter Play", **Then** the system automatically establishes the connection and the chat interface opens with the sidebar showing the agent's name, profile name, model, and a "View Profile" button.
4. **Given** the operator is on the play page and the connection is not yet established, **When** the operator sends a message, **Then** the system connects automatically before sending, with no manual connect step required.
5. **Given** the operator navigates away from a session and returns later (same process lifetime), **When** the operator re-enters the session detail, **Then** the agent is shown as present and ready — the operator is never forced to recreate the agent.

---

### User Story 2 - Checkpoint-Based Agent Resume (Priority: P1)

A game operator has a conversation with an agent across multiple messages. The operator closes the play page, navigates to other sessions, and later returns to the same session to continue the conversation. The agent remembers the full prior conversation context — it does not restart from scratch. This "resume" capability is powered by the agent framework's native checkpoint mechanism using the session ID as the thread identifier, not by any hand-rolled conversation history.

When the operator deletes an agent and creates a new one for the same session, the new agent starts with a clean slate — no leftover conversation data from the previous agent bleeds into the new session.

**Why this priority**: The current agent framework maintains conversation history in a hand-rolled runtime object that duplicates capabilities the underlying agent framework already provides natively. This runtime is evicted after 15 minutes of inactivity, causing the agent to appear non-existent and forcing the operator to recreate it, losing all conversation context. Using the native checkpoint mechanism eliminates this entire class of problems.

**Independent Test**: Can be tested by creating an agent, sending several messages, leaving the session, returning, and sending a follow-up message that references earlier conversation content — verifying the agent recalls the prior context. Then deleting the agent, recreating it, and verifying the new agent has no memory of the old conversation.

**Acceptance Scenarios**:

1. **Given** an agent with an existing multi-turn conversation, **When** the operator returns to the session (same process lifetime) and sends a new message referencing prior content, **Then** the agent responds with awareness of the earlier conversation.
2. **Given** an agent that was created but has been idle beyond any inactivity threshold, **When** the operator returns and sends a message, **Then** the agent is still present and continues the conversation — no recreation is needed and no context is lost.
3. **Given** an agent for a session, **When** the operator deletes the agent and creates a new one with a different profile, **Then** the new agent has no memory of the deleted agent's conversation — the checkpoint state is fully cleared.
4. **Given** two concurrent messages sent to the same agent, **When** both are processed, **Then** they are handled in send order without concurrent conflicts — the second message waits for the first to complete.

---

### User Story 3 - Agent Messages API (Priority: P2)

A game operator enters the play page for a session that has an existing agent with prior conversation. The chat interface is populated with the previous messages (both operator and agent messages) so the operator can see what was discussed before, not just an empty chat window. These messages are retrieved from the agent's checkpoint state via the new `ListMessages` API, each addressed by its native framework message id. Transient stream control signals (such as `wait` frames) are not part of these messages — only content needed to reconstruct the visible conversation is returned.

**Why this priority**: Without prior-message display, the operator re-enters a session and sees an empty chat, creating the impression that the agent forgot everything even though the underlying context is preserved. This is a UX-completing feature that makes the resume capability visible and trustworthy.

**Independent Test**: Can be tested by having a multi-turn conversation, leaving the play page, re-entering it, and verifying that the chat interface displays all prior messages in chronological order and that each message's `message_id` matches the native checkpoint message id.

**Acceptance Scenarios**:

1. **Given** an agent with prior conversation turns, **When** the operator enters the play page, **Then** the chat interface displays all previous messages (user and agent) in chronological order before any new input, each carrying its native `message_id`.
2. **Given** a newly created agent with no conversation, **When** the operator enters the play page, **Then** the chat interface shows an empty state with a prompt to start a conversation.
3. **Given** an agent whose messages are being retrieved, **When** the retrieval takes time, **Then** the interface shows a loading indicator until the messages are populated.

---

### User Story 4 - Bug Fixes: Profile Model & Checkpointer Continuity (Priority: P1)

The agent framework currently has two latent bugs that must be fixed as part of this redesign:

1. **Profile model is ignored**: When an agent is created with a profile that specifies a particular model, that model specification is copied into the agent's metadata but never actually passed to the language model invocation. All agents end up using the same default model regardless of what their profiles specify.

2. **Checkpointer is defeated**: The agent framework creates a fresh checkpoint store and a random thread identifier on every single message turn, which means the native checkpoint mechanism provides zero continuity between turns. The hand-rolled conversation history array is the only thing carrying context, which is fragile and duplicates framework capability.

**Why this priority**: The profile model bug means profile configuration is misleading — operators configure models that are silently ignored. The defeated checkpointer means the "native checkpoint" appears to be in use but isn't, creating a false sense of reliability.

**Independent Test**: Can be tested by creating two agents with different profile models, sending messages to both, and verifying each uses its configured model. The checkpointer fix is verified by confirming that conversation context persists across turns via the checkpoint mechanism alone (with no hand-rolled history).

**Acceptance Scenarios**:

1. **Given** two agent profiles configured with different models, **When** agents are created from each profile and sent messages, **Then** each agent uses its own configured model — verifiable through agent response characteristics or logs.
2. **Given** the checkpointer fix is in place, **When** an agent processes multiple turns, **Then** each turn uses the same thread identifier (the session ID) and the same checkpoint store instance, providing automatic context restoration.

---

### Edge Cases

- What happens when the operator sends a message while the automatic connection is still being established? The system should queue the message and send it once the connection completes, or show a brief "connecting" state.
- What happens when the agent service process restarts (losing all in-memory state)? The operator sees the agent as not found and can recreate it — this is acceptable for the in-memory checkpoint stage; persistence is explicitly out of scope.
- What happens when the operator deletes a session (not just the agent)? Both the agent metadata and the checkpoint state for that session are cleared.
- What happens when the `ListMessages` API is called for a session whose agent was just deleted? The API returns an empty result or a not-found response, not stale data.
- What happens when concurrent messages arrive on the same session? They are serialized in send order; the second message waits for the first turn to complete before processing.
- What stream content is excluded from `ListMessages`? `wait` frames and any other transient control signal not required for breakpoint reconnect MUST NOT appear. Only `text`, `thinking`, and `warn` content carried by checkpoint state is materialized; `thinking` is best-effort and omitted when the provider did not persist it.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The session detail page MUST display different controls based on the current agent state — profile selector only when no agent exists, agent summary only when an agent exists.
- **FR-002**: The system MUST automatically establish the agent connection when an agent exists and the operator enters the play page, or when a message is sent while not yet connected — no manual "connect" action is required. The session detail page MUST NOT open a network connection; it displays agent metadata only.
- **FR-003**: The manual agent connection control MUST be removed from all pages.
- **FR-004**: The play page sidebar MUST display the current agent's name, profile name, and model.
- **FR-005**: The play page sidebar MUST provide a control to view the full agent profile configuration (system prompt, skills, MCPs, enabled status).
- **FR-006**: The agent framework MUST use the session identifier as the checkpoint thread identifier for all conversation turns within a session.
- **FR-007**: The agent framework MUST use a single shared checkpoint store instance across all turns and sessions, not a fresh instance per turn.
- **FR-008**: The agent framework MUST NOT maintain a hand-rolled conversation history array or a manual message queue — conversation state is managed exclusively by the native checkpoint mechanism.
- **FR-009**: The agent framework MUST NOT evict agent metadata or checkpoint state based on inactivity — agents remain available for the entire process lifetime.
- **FR-010**: When an agent is deleted, the system MUST clear both the agent metadata and the checkpoint state for that session so a newly created agent starts with no prior conversation data.
- **FR-011**: The system MUST provide a `ListMessages` request-response API — a new unary RPC on the agent service that reads checkpoint state, exposed through the gateway as `GET /api/v1/{parent=sessions/*/agent}/messages` — returning a chronologically ordered list of `Message` resources for a given session's agent. Each `Message` is addressable as `sessions/{session_id}/agent/messages/{message_id}` where `message_id` is the native LangChain `BaseMessage.id` surfaced directly from checkpoint state (no parallel identifier).
- **FR-012**: The desktop play page MUST populate the chat interface with prior conversation messages retrieved via `ListMessages` when entering a session with an existing agent.
- **FR-013**: The agent framework MUST use the model specified in the agent's profile for all language model invocations — not a fixed default model.
- **FR-014**: Concurrent messages to the same agent MUST be processed in send order without concurrent language model calls for the same session.
- **FR-015**: The agent metadata returned to the desktop MUST include the agent profile name so the desktop can display it and fetch profile details. The proto field `Agent.agent_profile_name` (`game.proto:214`) and the agent service handler population (`handler.ts:93,138`) already exist; this requirement closes the gap in the desktop Go `AgentView` view model (`view_model.go`) and the frontend `api.ts` types, which currently omit the field.
- **FR-016**: `ListMessages` MUST exclude `wait` frames and any transient stream control signal not required for breakpoint reconnect. `thinking` content is best-effort: included only when the provider persisted it into the checkpoint message. `message_id` MUST be the native framework message id, never a parallel identifier.

### Key Entities *(include if feature involves data)*

- **Agent Metadata**: Lightweight, immutable configuration for a created agent — includes session identifier, profile name, model, system prompt, and creation time. Stored in memory for the process lifetime. Does not include conversation state.
- **Checkpoint State**: The conversation state for a session, managed by the native checkpoint mechanism and keyed by the session identifier. Includes all messages exchanged in the session's conversation. In-memory only for this stage.
- **Message**: A normalized, addressable representation of one checkpoint message (`sessions/{session_id}/agent/messages/{message_id}`), where `message_id` is the native LangChain `BaseMessage.id`. Returned by `ListMessages`; excludes transient stream control signals.
- **Agent Profile**: Pre-existing configuration entity (name, model, system prompt, skills, MCPs, enabled) stored in the prompt service. Copied into agent metadata at creation time.
- **Session**: Pre-existing entity representing a game session. One session has at most one agent at a time.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can re-enter a session after any duration (within process lifetime) and continue the conversation with full context — no agent recreation or context loss occurs in 100% of cases.
- **SC-002**: The session detail page shows exactly one state-appropriate control set at any time — an operator never sees contradictory controls (e.g., profile selector alongside a connected agent).
- **SC-003**: An operator entering the play page sees prior conversation messages populated within 2 seconds of page load.
- **SC-004**: Two agents created from profiles with different models each use their respective configured model in 100% of invocations.
- **SC-005**: Deleting an agent and creating a new one for the same session results in zero conversation data leaking from the deleted agent in 100% of cases.
- **SC-006**: The operator completes the full flow (select session → create agent → chat → leave → return → resume chat) with zero manual connection actions required.

## Assumptions

- The agent service process lifetime is the boundary for in-memory state retention — persistence across process restarts is explicitly out of scope for this stage and will be addressed in a future iteration.
- The existing session service remains unchanged. The gateway and proxy gain one new forwarded RPC (`ListMessages`) following their existing request-response forwarding patterns; no other gateway/proxy behavior changes.
- The existing agent profile CRUD API and prompt service remain unchanged — agent metadata still copies profile data at creation time.
- The desktop client maintains its current Wails architecture and Svelte frontend toolchain.
- The operator has a stable network connection to the gateway during active chat sessions.
- The existing proto-based gRPC contract between proxy and agent service can be extended with new RPC methods for the `ListMessages` API.
- Concurrent message scenarios are limited to a single operator sending messages rapidly — multi-operator concurrent access to the same session is out of scope.
- When a future iteration replaces the in-memory `MemorySaver` with a database-backed checkpointer (Postgres/Mongo), the chosen checkpointer's `deleteThread(threadId)` implementation MUST be verified for atomicity and correctness, since `DeleteAgent`'s clean-recreate guarantee depends on it.

### Out of Scope (Explicit)

- **No pagination on `ListMessages`**: this stage returns the full message list; pagination/offset/cursor is deferred.
- **No automatic reconnection infrastructure**: transient connection drops show a `connection lost → error` state with operator-initiated retry only. No exponential-backoff or background auto-reconnect is built.
- **No per-message timestamp synthesis**: `Message.create_time` is sourced only from the direct checkpoint metadata (`StateSnapshot.createdAt`); it is never reverse-engineered from step numbers, sequence positions, or other indirect sources. Omitted when unavailable.
- **No tool-call materialization**: tool invocations and tool results inside checkpoint state are not surfaced as `Message` entries; only `text`, `thinking`, and `warn` content is materialized.
- **No session-deletion cascade inside the agent service**: the agent service clears its own metadata/checkpoint state on `DeleteAgent`; cascading deletion when a session resource itself is deleted is the session service's responsibility.
- **No new npm/Go dependencies**: all work uses existing `@langchain/langgraph`, `deepagents`, and current Go modules.
