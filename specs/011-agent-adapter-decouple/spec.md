# Feature Specification: Agent Adapter Decoupling and LangChain Foundation

**Feature Branch**: `011-agent-adapter-decouple`

**Created**: 2026-06-15

**Status**: Draft

**Input**: User description: "将 deepagent 框架降级为 langchain，并且为将来自定义历史上下文做准备。重构 agent 服务以实现 SessionAgent 与 AgentAdapter 的分离，并清理不再需要的代码和设计。本方案替代 specs/010-langchain-agent-downgrade/。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Connect and Chat Without Agent Lifecycle Steps (Priority: P1)

A game operator creates a session, connects directly via WebSocket, and immediately starts chatting by selecting an agent profile — without performing any explicit agent creation, lookup, or deletion step. The agent processes messages using the selected profile's model and instructions, and the conversation history accumulates within the session. When the operator disconnects and reconnects later within the same process lifetime, prior messages remain visible and the conversation resumes seamlessly.

**Why this priority**: This is the core workflow. Removing the create/get/delete lifecycle steps is the primary product simplification. If this does not work, the entire feature has no value.

**Independent Test**: Can be fully tested by creating a session, connecting with a profile name, exchanging messages, disconnecting, reconnecting, and verifying that history persists and the conversation continues.

**Acceptance Scenarios**:

1. **Given** a newly created session with no prior conversation, **When** the operator connects and sends a text message specifying a profile, **Then** the agent responds with thinking and/or text frames followed by a wait frame, and the conversation history begins accumulating.
2. **Given** an active session with prior conversation, **When** the operator disconnects and reconnects to the same session, **Then** prior messages are listed in chronological order and the next response has the expected conversation context.
3. **Given** a session with prior conversation, **When** the operator lists messages, **Then** the messages are returned in chronological order with the same content and ordering as previously exchanged.
4. **Given** any client using the gateway WebSocket or HTTP surfaces, **When** it performs the connect-chat-disconnect-reconnect-list flow, **Then** the experience is functional without any agent lifecycle management step.

---

### User Story 2 - Switch Agent Profiles Mid-Conversation (Priority: P1)

An operator can switch between different agent profiles within the same session and WebSocket connection by sending a message that specifies a different profile name. The system unbinds the previous profile's execution adapter and creates a new one for the requested profile. Conversation history remains shared across all profiles within the session — the new adapter sees prior messages exchanged with other profiles.

**Why this priority**: Multi-profile support within a single session is a core architectural capability that distinguishes the new model from the previous one-agent-per-session design. It must work for the decoupling to have meaning.

**Independent Test**: Can be tested by connecting to a session, exchanging messages with profile A, switching to profile B by sending a frame with a different profile name, and verifying that profile B's response references prior conversation context.

**Acceptance Scenarios**:

1. **Given** an active connection using profile A with existing conversation, **When** the operator sends a text frame specifying profile B, **Then** the system switches to profile B's adapter and the response reflects awareness of the full session conversation history.
2. **Given** an active connection using profile A, **When** the operator sends a text frame specifying a non-existent profile, **Then** the system returns an error indication and the connection remains usable.
3. **Given** an active connection using profile A, **When** agent response frames are returned after switching to profile B, **Then** each response frame identifies profile B as the producing profile.

---

### User Story 3 - Foundation for Future Custom History Context (Priority: P2)

A product developer can identify a single, service-owned boundary where conversation history content and format can be customized in a future iteration. The agent execution layer no longer hides history construction behind an opaque abstraction, because the deepagent harness has been replaced with a LangChain foundation that exposes explicit middleware hooks. No operator-facing customization controls appear in this release, but the foundation is prepared.

**Why this priority**: This is the strategic goal that justifies the foundation work. It is P2 rather than P1 because it is a developer-facing capability, not an operator-facing one, and its value is realized in a future iteration.

**Independent Test**: Can be tested by reviewing the service design and code to confirm that the history construction path is explicit, identifiable, and replaceable without public API changes.

**Acceptance Scenarios**:

1. **Given** a future requirement to alter conversation history format, **When** developers inspect the agent service, **Then** there is a clearly owned, testable boundary where history content and formatting can be customized.
2. **Given** the current release scope, **When** operators use the agent normally, **Then** they do not see new history customization controls, settings, or behavior changes.
3. **Given** a normal multi-turn conversation, **When** the agent prepares context for a response, **Then** the service uses the same stored conversation data while keeping its representation controllable by service code.

---

### User Story 4 - Single Connection Per Session (Priority: P2)

The system ensures that only one WebSocket connection is active per session at any time. If a second connection opens to a session that already has an active connection, the first connection is closed and the second takes over. This prevents frame routing ambiguity and reflects the single-operator-per-session game model.

**Why this priority**: Connection exclusivity is a correctness invariant, not a feature enhancement. It is P2 because it is a guardrail rather than a primary user-facing capability.

**Independent Test**: Can be tested by opening two connections to the same session and verifying that the first is closed when the second connects.

**Acceptance Scenarios**:

1. **Given** an active WebSocket connection to a session, **When** a second connection opens to the same session, **Then** the first connection is closed and the second connection becomes the active one.
2. **Given** a connection that was replaced, **When** the operator attempts to send a message on the old connection, **Then** the connection reports an error or is already closed.

---

### User Story 5 - Clean Adapter Lifecycle on Disconnect (Priority: P2)

When a WebSocket connection disconnects, the system synchronously unbinds the active adapter from the session agent so the session is immediately available for new connections, then asynchronously cleans up the adapter's resources without blocking message processing.

**Why this priority**: Non-blocking cleanup is an operational quality concern. It ensures quick reconnection even when adapter cleanup involves resource-intensive teardown.

**Independent Test**: Can be tested by disconnecting, immediately reconnecting, and verifying that the new connection is functional without delay attributable to the previous adapter's cleanup.

**Acceptance Scenarios**:

1. **Given** an active connection with a bound adapter, **When** the connection disconnects, **Then** the adapter is unbound from the session agent synchronously and a new connection can bind a new adapter immediately.
2. **Given** a disconnected adapter undergoing async cleanup, **When** a new connection arrives for the same session, **Then** the new connection is not blocked by the cleanup of the previous adapter.

---

### Edge Cases

- What happens when a user sends a text frame without specifying a profile name? The system returns a warning frame indicating that a profile name is required, and the connection remains usable.
- What happens when a user switches to a profile that does not exist in the prompt service? The system returns a warning frame with an appropriate error message, and the current adapter (if any) remains bound.
- What happens when a message arrives while the adapter is being switched (mid-unbind, pre-new-bind)? Same-session message serialization ensures only one message is processed at a time; the switch completes before the next message is processed.
- What happens when the process restarts? All in-memory conversation state is lost, matching the current in-memory lifecycle scope. Sessions created before the restart start fresh.
- What happens when a status or echo frame arrives (not a text frame)? These control frames are handled without requiring or affecting the adapter; they do not carry a profile name.
- What happens when a connection drops mid-streaming (response in progress)? The partial response is not completed; the conversation state reflects whatever was written to the checkpoint before the drop.
- What happens to the old spec (010-langchain-agent-downgrade)? It is superseded and removed; this spec is its replacement.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The agent service MUST replace its deepagent-based execution foundation with a LangChain-based foundation that exposes a service-owned context preparation boundary identifiable in code and tests.
- **FR-002**: The system MUST separate the session-level conversation agent (SessionAgent) from the profile-specific execution adapter (AgentAdapter) so that conversation history belongs to the session and the adapter is a stateless processor that reads and writes through a shared checkpoint store.
- **FR-003**: Users MUST be able to connect to a session and interact with an agent without performing any explicit agent creation step; the adapter is created on demand when the first message specifying a profile is processed.
- **FR-004**: Users MUST be able to select and switch agent profiles within an active session connection by specifying a profile name in each text message frame.
- **FR-005**: The system MUST ensure only one WebSocket connection is active per session at any time; a new connection MUST replace and close any existing connection to that session.
- **FR-006**: Conversation history MUST remain keyed to the session identity and persist across profile switches and connection reconnections within the process lifetime.
- **FR-007**: The system MUST remove the CreateAgent and DeleteAgent operations from both the agent service internal API and the public proxy/gateway surface.
- **FR-008**: The system MUST provide a query endpoint that returns the current agent adapter state (active profile name and connection status) for a session.
- **FR-009**: The system MUST unbind an adapter synchronously on connection disconnect and clean up adapter resources asynchronously so that the session is immediately available for new connections.
- **FR-010**: The connect WebSocket endpoint MUST be accessible at a session-level path (`/sessions/{session_id}/connect`) rather than an agent-level path.
- **FR-011**: The message listing endpoint MUST be accessible at a session-level path (`/sessions/{session_id}/messages`) rather than an agent-level path.
- **FR-012**: Each communication frame MUST carry an optional agent profile name field; user text frames use it to select the processing profile, and agent response frames use it to identify the producing profile.
- **FR-013**: Same-session message processing MUST remain serialized (FIFO) regardless of profile selection; no concurrent response generation may occur for a single session.
- **FR-014**: Session deletion MUST clean up the associated conversation history and any bound adapter state.
- **FR-015**: The implementation plan MUST explicitly identify all affected tests and large-test acceptance flows and document the required changes.
- **FR-016**: The implementation plan MUST classify all changes as new, modify, or delete and document preserved invariants where applicable.
- **FR-017**: The system MUST remove the deepagent dependency from the agent service once no source or test imports it.

### Key Entities *(include if feature involves data)*

- **SessionAgent**: The logical conversation agent bound one-to-one to a session. Owns the conversation history context. Lifecycle matches the session lifecycle — it exists implicitly when a session exists, with empty history if no conversation has occurred. Does not require explicit creation. Future iterations may add sub-agents, each with their own independent history.
- **AgentAdapter**: The profile-specific execution processor that handles message generation. Built from an agent profile (model, system prompt, middleware configuration). Stateless — does not own conversation history; reads and writes through the shared checkpoint store keyed by session identity. Created on demand when a user sends a message specifying a profile. Can be swapped mid-connection. Cleaned up asynchronously on disconnect.
- **Agent Profile**: Existing configuration (model, system prompt, skills) managed by the prompt service. Remains the source of adapter construction parameters. Unchanged in this feature.
- **Conversation History**: The ordered, session-scoped message collection. Stored in the shared in-memory checkpoint store, keyed by session identity. Shared across all profile adapters within the same session. Persists across reconnections within the process lifetime.
- **Session**: Existing top-level container. Its lifecycle implicitly governs the SessionAgent lifecycle. Session deletion cascades to clean up conversation history and adapter state.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can complete the flow create session → connect with profile → chat → switch profile → chat → disconnect → reconnect → view history → resume chat with zero explicit agent lifecycle steps (no create, get, or delete agent operations).
- **SC-002**: Conversation history persists across profile switches within the same session in 100% of tested cases; the switched-to profile's responses reference prior context exchanged with other profiles.
- **SC-003**: Prior conversation messages remain visible in chronological order within 2 seconds for normal conversation sizes after reconnecting.
- **SC-004**: Only one active WebSocket connection exists per session at any time; a second connection replaces the first in 100% of tested cases.
- **SC-005**: Adapter unbinding on disconnect completes synchronously without measurable delay to new connection establishment; async cleanup does not block subsequent message processing.
- **SC-006**: A future implementation plan can identify one service-owned boundary for customizing conversation history format without requiring public API changes.
- **SC-007**: Same-session rapid message sends preserve send order in 100% of tested cases, regardless of profile selection.

## Assumptions

- The gateway/proxy/desktop/frontend architecture remains conceptually the same; only the agent-service-facing API surface and the agent service internals change.
- In-memory process lifetime remains the storage boundary for this stage; durable persistence and restart recovery remain out of scope.
- Sub-agent support (multiple logical agents per session with independent histories) is explicitly out of scope for this release but the naming and architecture must not preclude it.
- The existing PromptService (agent profile and skill management) and SessionService (session CRUD) remain unchanged.
- Profile configuration is fetched from the prompt service at adapter creation time; no local profile caching or daemon-based synchronization is needed.
- The existing agent frame payload types (text, thinking, status, echo, warn, wait, etc.) remain unchanged; only a new optional top-level field is added.
- This spec supersedes and replaces `specs/010-langchain-agent-downgrade/`; the directory and all its contents will be removed once this spec is adopted.

### Out of Scope (Explicit)

- No durable persistence of conversation history across service restarts.
- No sub-agent support (multiple logical agents per session with independent histories).
- No operator-facing custom history formatting controls or settings.
- No change to SessionService, PromptService, or their resource models.
- No change to agent frame payload types beyond the new optional profile name field.
- No new profile listing or refresh endpoints on the agent service; available profiles remain queryable through the existing PromptService.
