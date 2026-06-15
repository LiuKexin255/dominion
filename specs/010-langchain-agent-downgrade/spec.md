# Feature Specification: Agent Engine Context Control Foundation

**Feature Branch**: `010-langchain-agent-downgrade`

**Created**: 2026-06-15

**Status**: Draft

**Input**: User description: "@specs/009-agent-checkpoint-redesign/ 已经实现但现在有个新的问题，就是 deepagent 好像不能自己控制历史上下文的格式和内容，这将不满足未来的预期。所以计划将 agent 服务使用的 deepagent 降级为 langChain 以满足可以自定义的需求。目标：将 agent 服务的 deepagent 降级为 langChain，接口与功能保持不变。本次目标不涉及自定义历史上下文，但未来要做历史上下文自定义。参考文档：https://docs.langchain.com/oss/python/deepagents/overview，https://docs.langchain.com/oss/python/langchain/overview"

## Clarifications

### Session 2026-06-15

- Q: What compatibility baseline should the downgrade preserve? → A: Preserve public APIs, desktop/operator flows, message history, resume, deletion, model/profile behavior, and current configured agent tool behavior only when observable through existing surfaces.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Preserve Existing Agent Behavior (Priority: P1)

A game operator uses the already implemented checkpoint-backed session flow exactly as before: create an agent for a session, enter play, exchange messages, leave the session, return later, view prior messages, continue the conversation, and delete/recreate the agent when needed. The internal agent foundation changes, but the operator-visible workflow and all existing service surfaces remain unchanged.

**Why this priority**: This change is a foundation replacement, not a product behavior redesign. Any visible regression would break the completed checkpoint/session redesign and make the migration unacceptable.

**Independent Test**: Can be fully tested by running the existing session/agent acceptance flow from `specs/009-agent-checkpoint-redesign/`: create agent, chat, leave, return, load history, resume context, delete, recreate, and verify no stale data appears.

**Acceptance Scenarios**:

1. **Given** a session with no agent, **When** the operator creates an agent from an existing profile, **Then** the agent is created with the same visible metadata and the session detail/play entry flow behaves as before.
2. **Given** an existing agent with a multi-turn conversation, **When** the operator leaves and re-enters play during the same service lifetime, **Then** prior messages are shown and the next response has the expected conversation context.
3. **Given** an agent for a session, **When** the operator deletes it and creates a new agent for the same session, **Then** the new agent starts without any visible or conversational data from the deleted agent.
4. **Given** any existing client using the agent service, gateway, proxy, desktop binding, or play UI surfaces, **When** it performs previously supported operations, **Then** request and response semantics are unchanged.
5. **Given** an existing profile configuration whose tool behavior is visible through the current agent surfaces, **When** the migrated agent handles an equivalent interaction, **Then** the visible tool-related behavior remains equivalent.

---

### User Story 2 - Enable Future Conversation Context Customization (Priority: P1)

A product developer needs the agent service foundation to allow explicit control over the format and content of conversation history in a future iteration. After this change, the service no longer depends on an opaque agent abstraction that prevents that control. The current release does not expose custom history formatting to operators, but it establishes a foundation where such customization can be planned without another service-surface rewrite.

**Why this priority**: The current foundation cannot satisfy expected future requirements around custom history content and formatting. Replacing it now prevents the newly implemented checkpoint redesign from being built on a dead-end abstraction.

**Independent Test**: Can be tested by reviewing the completed service behavior and design artifacts to confirm that the agent execution foundation has an explicit conversation-context construction point while all existing user-facing behavior remains equivalent.

**Acceptance Scenarios**:

1. **Given** a future requirement to alter the conversation history format, **When** developers inspect the agent service design, **Then** there is a clearly owned service boundary where history content and formatting can be customized without changing public agent APIs.
2. **Given** the current release scope, **When** operators use the agent normally, **Then** they do not see new history customization controls, settings, or behavior changes.
3. **Given** a normal multi-turn conversation, **When** the agent prepares context for a response, **Then** the service uses the same stored session conversation data while keeping its representation controllable by service code.

---

### User Story 3 - Maintain Checkpoint and History Guarantees (Priority: P2)

A game operator relies on the checkpoint-backed guarantees delivered by the previous feature: stable session-based continuity, listable prior messages, and clean deletion. The foundation replacement must retain these guarantees while making future context customization possible.

**Why this priority**: The previous redesign solved concrete operator problems. The migration must preserve those guarantees independently of the internal agent foundation change.

**Independent Test**: Can be tested by comparing pre- and post-change behavior for message history retrieval, same-session resume, and delete/recreate isolation using the existing observable client surfaces.

**Acceptance Scenarios**:

1. **Given** a session with prior conversation messages, **When** the operator enters play, **Then** the visible message history is returned in chronological order with the same message identity behavior as before.
2. **Given** multiple messages sent to the same agent, **When** they are processed, **Then** same-session ordering and context continuity are preserved.
3. **Given** service-managed in-memory state, **When** the service remains running, **Then** the agent remains resumable for the process lifetime; when the process restarts, the existing in-memory limitation remains unchanged.

---

### Edge Cases

- What happens when migration changes the internal response-generation path but not public contracts? Existing contract and acceptance tests must continue to pass with identical externally observable behavior.
- What happens when a conversation has no prior messages? The existing empty-history behavior remains unchanged.
- What happens when an agent is deleted while history exists? Both active agent metadata and conversation continuity data are cleared as before.
- What happens when future customization is not yet implemented? No operator-facing customization controls appear; the foundation is prepared but dormant.
- What happens when existing checkpoints contain messages created before the foundation change? Within the current in-memory process scope, the service must either continue to read the same conversation state or start only where the existing in-memory lifecycle permits; no new persisted migration is required.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The agent service MUST replace its current opaque agent execution foundation with one that lets service code explicitly construct and control conversation history content and format in a future iteration.
- **FR-002**: The migration MUST preserve all existing public agent capabilities from the checkpoint/session redesign, including create agent, get agent, delete agent, connect/play messaging, list messages, resume conversation, clean recreate behavior, and currently configured profile tool behavior when that behavior is observable through existing public surfaces.
- **FR-003**: The migration MUST NOT introduce new operator-facing controls, settings, or workflow steps for custom history formatting in this release.
- **FR-004**: Existing request/response and streaming semantics visible to gateway, proxy, desktop, and play UI clients MUST remain compatible with the current implemented behavior.
- **FR-005**: Conversation continuity MUST remain keyed to the session identity so that same-session messages resume context and different sessions remain isolated.
- **FR-006**: Agent deletion MUST continue to remove the session's active agent metadata and conversation continuity data so that recreating an agent starts cleanly.
- **FR-007**: Prior-message listing MUST continue to return the same user-visible conversation content and ordering guarantees as the existing implementation.
- **FR-008**: Profile-driven agent configuration, including selected model behavior, MUST continue to be honored after the foundation replacement.
- **FR-009**: Same-session message handling MUST preserve the existing ordering guarantee and MUST NOT create concurrent response generation for a single session.
- **FR-010**: The implementation plan MUST explicitly identify all tests and large-test acceptance flows affected by the foundation replacement and must verify the observable behavior through the real agent service path.
- **FR-011**: The implementation plan MUST classify the service change as a refactoring of existing behavior and document the invariants that must be preserved.

### Key Entities *(include if feature involves data)*

- **Agent**: The session-bound assistant instance visible to operators; retains existing metadata, lifecycle, and one-agent-per-session behavior.
- **Conversation History**: The ordered user-visible and context-bearing messages for a session; currently behaves as before but must become controllable by service-owned context preparation in future work.
- **Checkpoint State**: The service-managed continuity data for a session during the current process lifetime; supports resume, message listing, and delete/recreate isolation.
- **Agent Profile**: Existing configuration selected when creating an agent; remains the source of model and prompt-related behavior.
- **Session**: Existing game session identity that scopes one active agent and its conversation continuity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of previously supported agent lifecycle operations complete with unchanged client-visible results in the migrated service.
- **SC-002**: An operator can complete the flow create agent → chat → leave → return → view history → resume chat → delete → recreate with zero additional workflow steps.
- **SC-003**: Prior conversation messages remain visible in chronological order within 2 seconds for normal conversation sizes after re-entering play.
- **SC-004**: Delete/recreate isolation succeeds in 100% of acceptance runs, with no old conversation content influencing the newly created agent.
- **SC-005**: A future implementation plan can identify one service-owned point for customizing conversation history format without requiring public client contract changes.
- **SC-006**: Same-session rapid message sends preserve send order in 100% of tested cases.

## Assumptions

- The previous checkpoint/session redesign in `specs/009-agent-checkpoint-redesign/` is the behavioral baseline and must remain externally unchanged.
- This feature is a foundation refactor for the agent service only; desktop UI redesign, new history customization controls, and new public APIs are out of scope unless required to preserve current behavior.
- In-memory process lifetime remains the storage boundary for this stage; durable persistence and restart recovery remain out of scope.
- The future custom-history feature will be specified separately after this foundation can support it.
- The existing gateway, proxy, desktop, profile, and session services remain conceptually unchanged except where verification is needed to prove compatibility.

### Out of Scope (Explicit)

- No custom history formatting behavior is exposed to operators in this release.
- No new user-facing history settings, profile fields, or desktop controls are added.
- No change to the existing public message listing contract is intended.
- No durable migration of historical conversation data across service restarts is required.
- No full parity requirement exists for deepagent-only internal abstractions that are not observable through public service, desktop, or operator workflows.
