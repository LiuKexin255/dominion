# Feature Specification: Dialog Agent with Chat Interface

**Feature Branch**: `007-dialog-agent`

**Created**: 2026-06-09

**Status**: Draft

**Input**: 为 agent 服务增加对话 agent 实现，使用 langchain deepagent 框架。包含 agent 对话能力、桌面端聊天界面、agent profile 管理和 provider 凭证配置。

## Clarifications

### Session 2026-06-09

- Q: When a user sends another message while the agent is still processing the previous one, what should happen? → A: Queue the new message and process it after the current response finishes.
- Q: What should happen when a user attempts to delete an agent profile that is currently used by an active agent instance? → A: Allow deletion but keep existing active instances running with a copied prompt.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Text Conversation with AI Agent (Priority: P1)

A user creates or selects a session, connects to the agent within that session, and engages in a text-based conversation. The user types messages, and the agent responds with a visible thinking process followed by a final response. The conversation history accumulates across the session so the agent can reference earlier exchanges.

**Why this priority**: This is the core value proposition — enabling interactive dialog between a user and an AI agent. Everything else supports this capability.

**Independent Test**: Can be fully tested by creating a session, connecting to its agent, sending text messages, and verifying that thinking output and responses appear in sequence. Delivers immediate value as a working chat agent.

**Acceptance Scenarios**:

1. **Given** a user has an active session with a connected agent, **When** the user sends a text message, **Then** the agent displays a thinking output followed by a final text response within a reasonable time.
2. **Given** a user has exchanged multiple messages with the agent, **When** the user sends a new message referencing prior conversation content, **Then** the agent's response demonstrates awareness of the earlier messages.
3. **Given** a user sends a message to the agent, **When** the agent is processing, **Then** the thinking output and the final response are displayed as distinct, clearly separated sections.
4. **Given** the agent is processing a message, **When** the user sends another message, **Then** the new message is queued and processed after the current response finishes.

---

### User Story 2 - Desktop Chat Interface (Priority: P1)

After connecting to an agent within a session, the user is presented with a chat interface consisting of a main dialog area and an information sidebar. The dialog area shows the full conversation thread — user messages, agent thinking, and agent responses — in chronological order. The sidebar displays metadata about the active agent instance.

**Why this priority**: The chat interface is the primary user-facing surface for interacting with the agent. Without it, users cannot effectively use the dialog capability.

**Independent Test**: Can be tested by connecting to an agent and verifying that the chat layout renders correctly with a dialog area and sidebar, and that messages appear in real time.

**Acceptance Scenarios**:

1. **Given** a user connects to an agent within a session, **When** the connection is established, **Then** the desktop displays a chat interface with a dialog area and an agent information sidebar.
2. **Given** a user is in the chat interface, **When** a message exchange occurs, **Then** the user's input, the agent's thinking, and the agent's response each appear as visually distinct entries in the dialog area.
3. **Given** the agent information sidebar is visible, **When** the user views it, **Then** it shows relevant agent metadata (profile name, status, etc.).

---

### User Story 3 - Agent Profile Management (Priority: P2)

A user manages agent personality profiles through the desktop interface. Each profile contains a descriptive prompt that defines how the agent should behave. The user can create new profiles, view a list of existing profiles, and delete profiles they no longer need. When creating an agent instance, the user selects a profile to determine the agent's personality.<!-- AMENDMENT: Profile update/edit capability removed per interview decision (no UpdateAgentProfile RPC). -->

**Why this priority**: Profile management enables customization of agent behavior. While important, a default profile can serve initially, making this secondary to the core conversation capability.

**Independent Test**: Can be tested by creating, listing, and deleting agent profiles through the desktop interface without involving a live agent conversation.

**Acceptance Scenarios**:

1. **Given** the user opens the profile management module, **When** the user creates a new profile with a name and description, **Then** the profile is saved and appears in the profile list.
2. **Given** an existing agent profile, **When** the user deletes the profile, **Then** the profile is removed and no longer available for new agent instances.
3. **Given** multiple agent profiles exist, **When** the user views the profile list, **Then** all profiles are displayed with their names and descriptions.
4. **Given** an agent profile was used to create an active agent instance, **When** the user deletes that profile, **Then** the profile is removed for future agent creation while the existing active instance continues running with the prompt copied at creation time.

---

### User Story 4 - Agent Instance Lifecycle Management (Priority: P2)

Agent instances are automatically cleaned up after 15 minutes of inactivity, where inactivity means no messages have been received and the agent is not currently processing. This ensures system resources are freed when conversations lapse while preserving active sessions.

**Why this priority**: Resource management is essential for system health but is an operational concern rather than a primary user-facing feature.

**Independent Test**: Can be tested by creating an agent instance, waiting past the inactivity threshold, and verifying the instance is removed.

**Acceptance Scenarios**:

1. **Given** an agent instance has received no messages and is not actively processing for 15 minutes, **When** the cleanup check runs, **Then** the agent instance is removed and its resources are freed.
2. **Given** an agent instance is actively processing or has received a message within the last 15 minutes, **When** the cleanup check runs, **Then** the agent instance is preserved.
3. **Given** an agent instance has been cleaned up, **When** the user attempts to send a message, **Then** the system handles the situation gracefully (e.g., notifies the user or creates a new instance).

---

### User Story 5 - AI Provider Credential Configuration (Priority: P3)

An administrator configures credentials for the AI provider that powers agent conversations. Credentials are provisioned through deployment tooling as mounted secret files, never stored in application configuration or source code. The system uses exactly one AI provider in this version.

**Why this priority**: Necessary for the system to function, but typically a one-time setup task performed by an administrator rather than an ongoing user interaction.

**Independent Test**: Can be tested by configuring provider credentials via the deployment mechanism and verifying the agent can successfully generate responses.

**Acceptance Scenarios**:

1. **Given** provider credentials are configured as a mounted secret file, **When** the agent service starts, **Then** it reads the credentials and can successfully communicate with the AI provider.
2. **Given** provider credentials are not configured or are invalid, **When** the agent attempts to process a message, **Then** the system reports a clear error without exposing credential details.

---

### Edge Cases

- What happens when the AI provider is unreachable or returns an error during a conversation?
- What happens when a user tries to create an agent instance with a profile name that does not exist?
- When a user deletes an agent profile that was used to create an active agent instance, the existing instance continues running with the prompt copied at creation time because agent instances and profiles are loosely coupled after creation.
- When the agent is processing a message and the user sends another message before the response completes, the system queues the new message and processes it after the current response finishes.
- What happens when the agent instance is cleaned up while the user is still viewing the chat interface?
- What happens when the provider secret file is missing or malformed at agent startup?
- What happens when conversation history grows very large over a long session?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow users to send text messages to an agent within a session and receive text responses.
- **FR-002**: System MUST display the agent's intermediate thinking output as a distinct element, separate from the final response, in the conversation view.
- **FR-003**: System MUST maintain conversation history within an agent instance and provide the full history as context for each new message.
- **FR-004**: System MUST NOT use external tools, MCP integrations, or skill capabilities in this version — the agent operates as a pure conversational agent.
- **FR-005**: System MUST allow users to create agent personality profiles, each with a unique name and a descriptive prompt that shapes agent behavior.
- **FR-006**: System MUST allow users to view ~~update,~~ and delete existing agent personality profiles.<!-- AMENDMENT: Update/edit capability removed (no UpdateAgentProfile RPC). Create is covered by FR-005. -->
- **FR-007**: System MUST create an agent instance by loading the description from a specified agent profile.
- **FR-008**: System MUST automatically remove agent instances that have been inactive (no messages received and not actively processing) for more than 15 minutes.
- **FR-009**: System MUST provide a desktop chat interface with a dialog area for the conversation thread and a sidebar for agent information.
- **FR-010**: System MUST render user messages, agent thinking, and agent responses as visually distinct entries in the conversation thread, ordered chronologically.
- **FR-011**: System MUST display agent metadata (including profile name and instance status) in the chat interface sidebar.
- **FR-012**: System MUST securely manage AI provider credentials through deployment-provisioned secret files, separate from application configuration.
- **FR-013**: System MUST support at least one AI provider for powering agent conversations.
- **FR-014**: System MUST preserve the existing session-agent architecture model established in prior milestones.
- **FR-015**: ~~System MUST preserve the existing desktop window binding and screenshot capture capabilities from the prior milestone, integrated alongside the new chat interface.~~<!-- REMOVED: Screenshot capture and window binding UI removed per interview decision. Chat interface replaces entirely. -->
- **FR-016**: System MUST queue user messages sent while an agent is processing and process queued messages in send order after the current response finishes.
- **FR-017**: System MUST allow deletion of agent profiles that were used to create active agent instances; existing instances MUST continue running with the prompt copied at creation time because agent instances and profiles are loosely coupled after creation.

### Key Entities

- **Agent Profile**: A named template that defines an agent's personality and behavior through a descriptive prompt. Created and managed by users. Used as the blueprint when instantiating an agent.
- **Agent Instance**: A running agent bound to a specific session, created from an agent profile. Holds the active conversation history and tracks its last activity timestamp for lifecycle management.
- **Conversation Message**: A single entry in the conversation thread. Each message has a role (user input, agent thinking, or agent response) and text content.
- **AI Provider Credential**: Configuration required to communicate with the external AI service. Provisioned securely via deployment tooling and never exposed in application code or logs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can complete a full message exchange (send text, see thinking, see response) with the agent within each conversation turn.
- **SC-002**: Agent instances that have been inactive for 15 minutes are removed within 1 minute of the threshold being crossed.
- **SC-003**: Users can successfully perform all profile management operations (create, read, delete) through the desktop interface.<!-- AMENDMENT: Narrowed to create, read (list), delete operations per interview decision. Update removed. -->
- **SC-004**: The agent's responses demonstrate awareness of all prior messages exchanged in the current session.
- **SC-005**: The chat interface clearly visually separates user input, agent thinking, and agent response entries so that a user can distinguish them at a glance.
- **SC-006**: Provider credentials are never logged, displayed in configuration, or included in error messages.
- **SC-007**: Existing session and agent architecture continues to function correctly after the introduction of the dialog agent.<!-- AMENDMENT: Narrowed to session/agent architecture continuity only per interview decision. Window binding and screenshot capabilities are removed from scope. -->

## Assumptions

- The agent service will be implemented as a Node.js-based service (the service is converted from Go to a JavaScript runtime in this milestone).
- The agent uses the LangChain deepagent framework for managing conversation context and AI interactions.
- This version intentionally excludes tools, MCP, and skill integrations; these are planned for future milestones to enable game-playing capabilities.
- The initial and only AI provider for this version is opencode-go.
- Provider credentials are provisioned as a secret file mounted by the deployment tooling; future providers will follow the same pattern.
- The existing session-agent architecture (session service, proxy service, gateway service) remains unchanged.
- The desktop chat interface replaces the prior milestone's screenshot capture and window binding UI entirely. Screenshot and window binding features are removed from this milestone.
- Agent profiles are managed by a dedicated prompt service that stores and retrieves profile definitions.
- The agent implementation uses `createDeepAgent` from the `deepagents` npm package with built-in capabilities at their defaults.
- LLM integration uses `initChatModel` with a custom `baseUrl` pointing to the opencode-go OpenAI-compatible endpoint.
- Thinking output is streamed using `streamMode: "messages"` with `contentBlocks` filtering (reasoning blocks → thinking frames, text blocks → assistant response frames).
- The `AgentFrame` proto includes a `FrameSender` enum (USER / AGENT / SYSTEM) and a `turnId` field mapped from the gRPC `invoke_id`.
