# Feature Specification: Agent Game Tools and Image Turns

**Feature Branch**: `013-agent-game-tools`

**Created**: 2026-06-21

**Status**: Draft

**Input**: User description: "为 ideas/llm_agent_play_game/README.md step.4 制定 spec. Tools use standard LangChain tool/function calling; configured tools convert commands into frames sent to desktop; desktop executes and sends frame results back to agent. Agent profiles add `tool_names`; undeclared tools are not exposed to the agent. Add left-right simultaneous mouse action. A user turn may send text and image together, using multipart text frames if needed. If agent history can include images, return them; if not, image history is not core. Desktop only displays user-published images, preferably in message history. Add a new operation result frame because AgentAckFrame is for tests. Do not auto-send screenshots after an operation; this stage is single-step and user-driven. Extend fake LLM testing for step.4."

## Clarifications

### Session 2026-06-21

- Q: What maximum image payload should a user turn allow? → A: 5 MiB maximum image payload per user turn.
- Q: What markdown safety policy should desktop use for agent text? → A: Render a safe markdown subset and strip raw HTML.
- Q: Should desktop auto-execute or require confirmation for an agent-requested mouse operation? → A: Auto-execute requested mouse operations per user-driven turn.

### Session 2026-06-21 (Round 2: Design Clarifications)

Decisions from Q1-Q17 recorded in the decision matrix:

- **Q1 (OperationBridge channel binding)**: Session-scoped, not WS-connection-scoped. Survives reconnect.
- **Q2 (RefreshAgent routing)**: desktop → gateway → proxy → agent.
- **Q3 (RefreshAgent during in-flight turn)**: Rejected with `FAILED_PRECONDITION`.
- **Q4 (Multiparty frame deprecation)**: New `AgentUserTurnFrame` bundles text and screenshot. Standalone user→agent frames deprecated.
- **Q5 (LLM screenshot_id)**: Not in tool schema; injected from turn context by agent.
- **Q6 (Sink timeout)**: 5-second configurable timeout, failure result on timeout.
- **Q7/Q9 (fake-llm config split)**: Two config sections (`messages`, `tools`) selected by last-message role.
- **Q8 (5 MiB validation)**: Image bytes only, validated by desktop before sending.
- **Q10 (UpdateAgentProfile)**: Uses `FieldMask`, `PATCH` route, on `PromptService`.
- **Q11 (Operation limit)**: Removed entirely; no per-turn operation counter.
- **Q12 (RefreshAgent shape)**: `rpc RefreshAgent(RefreshAgentRequest) returns (google.protobuf.Empty)`.
- **Q14 (ListMessages image replay)**: Extend; checkpoint preserves images.
- **Q15 (Deprecated desktop methods)**: Removed `ExecuteOperation`, `OperationResultView`, `SendNextScreenshot`, `SendScreenshot`. Retained `BindWindow`, `CaptureScreenshot`, `ListWindows`. Added `SendUserTurn`.
- **Q16 (gRPC max message)**: 8 MiB uniform across 3 hops.
- **Q17 (Message oneof)**: `oneof content { string text; bytes image_data; }`, not coupled to frame structs.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Send Text and Screenshot to Agent (Priority: P1)

A desktop operator enters a play session, binds a game window, captures a screenshot, optionally adds text instructions, and sends both as one user turn. The agent receives the image and text together so it can reason about the visible game state and answer or choose a single operation.

**Why this priority**: Image-grounded interaction is the foundation for game play. Without a text-plus-image turn, mouse tools have no reliable visual context.

**Independent Test**: Can be fully tested by binding a window, attaching one screenshot to an input message, sending optional text, and verifying the agent receives one coherent turn containing both the text and the screenshot.

**Acceptance Scenarios**:

1. **Given** a connected session with a selected profile and a bound window, **When** the operator captures a screenshot and sends it with text, **Then** the conversation shows the operator text and a collapsed image attachment for that same turn.
2. **Given** an attached screenshot before sending, **When** the operator removes the attachment, **Then** the turn is sent without image content and the removed image is not displayed.
3. **Given** the operator sends an image-only turn, **When** the agent receives the turn, **Then** the turn is valid and the agent can respond using the image as context.

---

### User Story 2 - Agent Requests One Mouse Operation (Priority: P1)

An agent profile declares a mouse tool, the agent chooses one mouse operation from the current screenshot, and the desktop automatically executes that single operation against the bound window. The desktop reports the operation result back to the agent as a normal frame. The system does not automatically capture or send the next screenshot; the operator decides whether to continue.

**Why this priority**: Single-step tool execution lets the operator supervise game play while validating the end-to-end action loop safely.

**Independent Test**: Can be tested by using a profile that declares the mouse tool, sending a screenshot, triggering one agent-selected operation, executing it on the desktop, and verifying a visible operation result is returned.

**Acceptance Scenarios**:

1. **Given** a profile whose `tool_names` includes the mouse tool, **When** the agent chooses a left, right, double-click, or simultaneous left-right action at screenshot-relative coordinates, **Then** the desktop receives one mouse operation frame with those coordinates and action details.
2. **Given** the agent requests one mouse operation, **When** the operation frame reaches the desktop, **Then** the desktop automatically executes it without an extra confirmation step.
3. **Given** the desktop executes a mouse operation successfully, **When** it reports the result, **Then** the conversation shows the operation as completed and the agent receives a result frame for that operation.
4. **Given** the desktop cannot execute a mouse operation, **When** it reports the failure, **Then** the conversation shows the failure reason and the agent receives a result frame indicating failure.
5. **Given** a mouse operation completes, **When** no user sends a new screenshot, **Then** the system does not automatically send a follow-up screenshot.

---

### User Story 3 - Profile-Scoped Tool Availability (Priority: P2)

A prompt/profile manager controls which built-in tools are available to an agent profile using a `tool_names` list. Tools not listed on the active profile are unavailable to that agent, even if the system implements them.

**Why this priority**: Profile-scoped tool access lets operators separate passive conversation profiles from profiles allowed to operate the game.

**Independent Test**: Can be tested by creating two profiles, one with the mouse tool and one without, then verifying only the tool-enabled profile can produce desktop mouse operations.

**Acceptance Scenarios**:

1. **Given** a profile with `tool_names = ["mouse"]`, **When** the agent handles a turn, **Then** the mouse operation capability is available to that profile.
2. **Given** a profile with an empty or missing `tool_names` list, **When** the agent handles a turn, **Then** no mouse operation capability is available.
3. **Given** the operator switches profiles mid-session, **When** the new profile has a different `tool_names` list, **Then** subsequent turns use only the new profile's declared tools.

---

### User Story 4 - Display Rich Conversation Content (Priority: P2)

The desktop conversation renders agent text as formatted markdown and displays user-published screenshot attachments as collapsed image entries that can be expanded. Tool operations are shown as structured entries so the operator can understand what the agent attempted.

**Why this priority**: Operators need readable agent output and auditable tool activity to supervise game play.

**Independent Test**: Can be tested by sending markdown text, a screenshot attachment, and a mouse operation result, then verifying each appears in the conversation with the expected presentation.

**Acceptance Scenarios**:

1. **Given** an agent response contains markdown, **When** it appears in the chat, **Then** common safe formatting such as lists, code spans, and links is rendered correctly while raw HTML is stripped.
2. **Given** a user-published screenshot is present, **When** the chat displays it, **Then** the image is collapsed by default and can be expanded by the operator.
3. **Given** an agent operation is requested or completed, **When** the chat displays it, **Then** the operation action, coordinates, and result status are visible.

---

### User Story 5 - Deterministic Step.4 Test Coverage (Priority: P3)

Developers and testers can validate image turns, tool availability, tool-call behavior, operation result frames, and rich display behavior without relying on a real model provider.

**Why this priority**: The feature changes multiple services and UI surfaces; deterministic tests are necessary to validate behavior repeatedly.

**Independent Test**: Can be tested by configuring deterministic fake model responses that request tool calls and by verifying the resulting frames and conversation entries.

**Acceptance Scenarios**:

1. **Given** deterministic fake responses configured for a tool-enabled profile, **When** a text-plus-image turn is sent, **Then** the fake response can request a mouse operation and the expected operation frame is produced.
2. **Given** deterministic fake responses configured for a profile without tools, **When** a turn asks for an operation, **Then** no operation frame is produced.
3. **Given** a deterministic operation result frame is returned, **When** the agent receives it, **Then** the result is associated with the correct operation.

### Edge Cases

- What happens when a screenshot is captured from a window that is no longer available? The operator sees a clear capture failure and no image attachment is sent.
- What happens when a mouse operation references coordinates outside the screenshot dimensions? The desktop rejects the operation, reports a failed operation result, and does not click the screen.
- What happens when an agent profile names an unknown tool? Unknown tool names are ignored for that profile and surfaced as a profile validation warning to operators or testers.
- What happens when an image is larger than 5 MiB? The system rejects the send with a user-visible error before starting the turn.
- What happens when an operation result arrives after the operator switches profile? The result remains associated with the operation and session turn that requested it; it does not enable tools on the new profile. `RefreshAgent` handles mid-session tool changes by reloading the active profile's `tool_names` for the next turn.
- What happens when the agent history cannot preserve image messages? Current live turns still support images; historical image replay is best effort and not required for this milestone.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST allow an operator to bind a desktop game window before sending screenshots for a play session.
- **FR-002**: The system MUST allow an operator to capture the bound window as an image attachment before sending a message.
- **FR-003**: The system MUST allow one user turn to contain text, one image, or both text and image together.
- **FR-004**: The system MUST allow the operator to remove a pending screenshot attachment before sending the turn.
- **FR-005**: The system MUST deliver image content in the same logical user turn as the accompanying text, when both are present.
- **FR-005a**: The system MUST reject user turns whose image payload exceeds 5 MiB before the turn is sent.
- **FR-006**: The system MUST expose only the tools listed in the active profile's `tool_names` list to that profile's agent.
- **FR-007**: The system MUST treat missing or empty `tool_names` as no declared tools.
- **FR-008**: The system MUST support a mouse operation tool that accepts screenshot-relative `x` and `y` coordinates and an action value.
- **FR-009**: The mouse action set MUST include left single click, left double click, right single click, right double click, and simultaneous left-right press.
- **FR-010**: The system MUST convert an agent-selected mouse operation into a desktop operation frame that includes operation identity, screenshot identity when available, action, and screenshot-relative coordinates.
- **FR-011**: The desktop MUST convert screenshot-relative coordinates into the correct window-relative or screen-absolute operation target before execution.
- **FR-011a**: The desktop MUST automatically execute requested mouse operations without requiring a separate operator confirmation.
- **FR-012**: The desktop MUST return a dedicated operation result frame for every requested mouse operation, indicating success or failure and a human-readable result message.
- **FR-013**: The system MUST NOT use acknowledgement frames as operation result frames.
- **FR-014**: The system MUST NOT automatically capture or send a follow-up screenshot after an operation result in this milestone.
- **FR-015**: The agent MUST receive desktop operation result frames so the result can be considered in the same session conversation.
- **FR-016**: The desktop conversation MUST display user-published images, collapsed by default and expandable on demand.
- **FR-017**: The desktop conversation SHOULD include user-published images in message history when the underlying session history can preserve them; lack of historical image replay MUST NOT block this feature.
- **FR-018**: The desktop conversation MUST display mouse operation requests and results with action, coordinates, and status.
- **FR-019**: The desktop conversation MUST render agent text messages with a safe markdown subset while stripping raw HTML and preserving readable display.
- **FR-020**: The system MUST support deterministic test responses that can exercise text-plus-image turns, profile-scoped tools, mouse operation requests, and operation result handling without a real model provider.
- **FR-021**: Existing text-only conversation behavior MUST remain functional for profiles without tools and for turns without images.
- **FR-022**: The system MUST preserve session-scoped conversation continuity across profile switches, with tool availability recalculated from the active profile for each turn.
- **FR-023**: The system MUST use the standard tool/function-calling behavior documented for LangChain agents ([LangChain agents documentation](https://docs.langchain.com/oss/javascript/langchain/agents)) for model-requested tool actions.
- **FR-024**: The system MUST represent user text-plus-image input using a multimodal message shape compatible with LangChain content blocks ([LangChain multimodal message migration notes](https://docs.langchain.com/oss/javascript/migrate/langchain-v1)).
- **FR-025**: The system MUST support an `UpdateAgentProfile` RPC on `PromptService` using `FieldMask` for partial updates, enabling mid-session editing of `tool_names` ([FieldMask convention reference](https://protobuf.dev/reference/protobuf/google.protobuf/#field-mask)).
- **FR-026**: The system MUST support a `RefreshAgent` RPC on both `ProxyService` (HTTP-facing via `POST /api/v1/sessions/{session}/agent:refresh`) and `AgentService` (internal). The RPC reloads the active profile's `tool_names` for a session and MUST reject with `FAILED_PRECONDITION` if a turn is in-flight. The desktop MUST call `RefreshAgent` after `UpdateAgentProfile` so the agent reloads its adapter with the new configuration. The request carries the Agent resource `name` (`sessions/{session}/agent`).
- **FR-027**: The user-to-agent direction MUST use a single `AgentUserTurnFrame` that bundles optional text and optional screenshot; the agent MUST construct one multimodal `HumanMessage` from the frame.
- **FR-028**: The `Message` proto MUST support image replay via a `oneof content` with `string text` and `bytes image_data` variants, verified against LangGraph checkpoint serialization.

### Key Entities *(include if feature involves data)*

- **Tool Declaration**: A profile-level list named `tool_names` that declares which built-in agent tools are available for a profile. Missing or empty lists mean no tools.
- **Mouse Operation Tool**: The agent-facing capability that represents one supervised mouse action against the current screenshot context.
- **Screenshot Attachment**: A user-published image captured from the bound game window and optionally sent with user text in one turn.
- **Operation Request**: A session-scoped request produced by the agent for the desktop to execute a mouse operation.
- **Operation Result**: A session-scoped frame returned by desktop after execution, containing operation identity, status, and a human-readable message.
- **Rich Conversation Entry**: A visible chat entry representing text, thinking, warning, user image, operation request, or operation result.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In at least 95% of test attempts, an operator can bind a window, attach a screenshot, send it with text, and see both the text and collapsed image in the conversation in under 10 seconds after capture.
- **SC-001a**: In 100% of oversized image tests, image payloads above 5 MiB are rejected before the turn starts with a user-visible error.
- **SC-002**: In 100% of deterministic tests, profiles without the mouse tool declared produce no mouse operation requests.
- **SC-003**: In 100% of deterministic tests, profiles with the mouse tool declared can produce one valid mouse operation request from a text-plus-image turn.
- **SC-004**: In 100% of operation execution tests, every requested mouse operation produces exactly one operation result frame, either success or failure.
- **SC-005**: In 100% of operation result tests, the system does not automatically send a follow-up screenshot after the result.
- **SC-006**: Operators can identify the requested mouse action, coordinates, and result status from the conversation without inspecting logs.
- **SC-007**: Markdown-formatted agent responses render readable formatting for common safe markdown constructs in all tested chat messages, and raw HTML is stripped in 100% of markdown safety tests.
- **SC-008**: Existing text-only chat scenarios continue to pass without requiring image attachments or tool declarations.

## Assumptions

- This milestone is a supervised, single-step game interaction loop: the operator advances play by sending screenshots or instructions, and the system does not autonomously continue after one operation.
- Only user-published screenshots are required to appear in the desktop conversation. Agent-internal images or tool result images are out of scope unless they are also user-published.
- Historical image display is best effort. Live display of the current user-published image is required; replaying old images from session history is not required if session history cannot preserve image payloads.
- Mouse coordinates are screenshot-relative pixel coordinates, and the desktop owns conversion to the actual execution target.
- The simultaneous left-right action means both mouse buttons are pressed as one combined action for the target coordinate, then released as one operation.
- The initial tool set for this milestone is the mouse operation tool. Keyboard tools, strategy self-update, and fully autonomous multi-step loops remain out of scope.
- Operation results are distinct from test/connectivity acknowledgements; acknowledgement frames remain available for tests but are not business operation results.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangChain JavaScript agents documentation](https://docs.langchain.com/oss/javascript/langchain/agents) — documents agent tool/function calling used as the tool behavior baseline.
- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1) — documents text and image content blocks for multimodal turns.

### Repositories

- No external repository references.

### Articles & RFCs

- No article or RFC references.

### Repository-Internal References

- `ideas/llm_agent_play_game/README.md` — source milestone description for step.4.
- `specs/007-dialog-agent/spec.md` — prior dialog-agent behavior and profile management baseline.
- `specs/011-agent-adapter-decouple/spec.md` — current session-agent/adapter and profile-switching baseline.
- `specs/012-fake-llm-service/spec.md` — current fake model testing baseline to extend for step.4.
