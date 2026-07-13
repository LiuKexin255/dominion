# Research: Agent Game Tools and Image Turns

## Decision: Use LangChain `createAgent` tools for the mouse tool

**Rationale**: LangChain JavaScript agents accept an explicit `tools` array created with tool definitions, and the source API exposes `tools` alongside model, middleware, and checkpointer parameters ([LangChain agents docs](https://docs.langchain.com/oss/javascript/langchain/agents), [createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts)). The game agent should build the tool list from the active profile's `tool_names`, with missing/empty lists producing `tools: []`.

**Alternatives considered**: Custom non-LangChain tool parsing was rejected because FR-023 requires standard LangChain tool/function calling. Global tool exposure was rejected because profile-scoped access is a product requirement.

## Decision: Represent text-plus-image turns as LangChain content blocks

**Rationale**: LangChain v1 multimodal messages construct a `HumanMessage` with `contentBlocks` containing `{ type: "text" }` and `{ type: "image" }` blocks ([LangChain migration notes](https://docs.langchain.com/oss/javascript/migrate/langchain-v1), [LangChain message docs](https://docs.langchain.com/oss/javascript/langchain/messages)). The proto frame contract should therefore carry screenshot bytes and optional text in one logical invoke so the agent can build one multimodal `HumanMessage`.

**Alternatives considered**: Sending screenshot and text as separate user turns was rejected because it loses the single-turn semantic required by FR-005. Persisting historical image replay was deferred because the spec makes image history best effort.

## Decision: Add a dedicated operation result frame

**Rationale**: Existing `AgentAckFrame` is an acknowledgement/testing frame, while FR-012 and FR-013 require a business result frame for desktop operation success/failure. The result must include `operation_id`, status, and a human-readable message so both agent and UI can associate the result with the request.

**Alternatives considered**: Reusing `AgentAckFrame` was rejected by spec. Encoding result text in `AgentWarnFrame` or `AgentTextFrame` was rejected because it would make tests and UI infer operation state from free text.

## Decision: Extend mouse action as semantic action enum instead of button/click pair only

**Rationale**: The current proto has `AgentMouseButton` plus `AgentMouseClickType`, which covers left/right single/double clicks but cannot express simultaneous left-right press without invalid combinations. A single action enum for `LEFT_CLICK`, `LEFT_DOUBLE_CLICK`, `RIGHT_CLICK`, `RIGHT_DOUBLE_CLICK`, and `LEFT_RIGHT_PRESS` preserves screenshot-relative coordinates while making operation validation exhaustive.

**Alternatives considered**: Adding a second button field was rejected because it permits unsupported combinations. Reusing keyboard operation was rejected because the desktop operation executor already separates mouse and keyboard paths.

## Decision: Use marked plus DOMPurify for safe markdown rendering

**Rationale**: `marked` 18.x is a browser-compatible MIT markdown parser ([marked v18.0.5 package.json](https://github.com/markedjs/marked/blob/v18.0.5/package.json)), but Marked documentation states output HTML is not sanitized and recommends filtering generated HTML ([Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)). DOMPurify 3.x is a browser sanitizer with configurable allowed tags/attributes and compatible package exports ([DOMPurify allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model), [DOMPurify v3.4.11 package.json](https://github.com/cure53/DOMPurify/blob/3.4.11/package.json)). The desktop frontend should parse markdown to HTML, sanitize with a strict allow-list, and render only sanitized output.

**Alternatives considered**: Rendering markdown manually was rejected because full common markdown support is costly and error-prone. Rendering raw Marked HTML was rejected because the spec requires raw HTML stripping.

## Decision: Extend the fake LLM service for deterministic tool-call and multimodal tests

**Rationale**: Existing spec-012 made fake LLM a standalone OpenAI-compatible service for deterministic large tests; this milestone needs deterministic tool-call responses and request assertions over text/image blocks. The fake service should remain stateless and data-driven while adding configured tool-call outputs for matched messages.

**Alternatives considered**: Reintroducing an in-process fake adapter was rejected because spec-012 removed that coverage bypass. Relying on real model providers was rejected because tests must be deterministic.

## Decision: Keep desktop operation execution single-step and user-driven

**Rationale**: The spec constrains this milestone to auto-executed mouse operations per user turn and explicitly forbids automatic follow-up screenshots. Desktop should execute requested operations, send one operation result frame, and then stop until the operator sends another turn. The per-turn operation counter is removed (Q11); the agent can request multiple operations if needed, though FR-014 (no auto-follow-up-screenshot) naturally limits useful multi-step scenarios.

**Alternatives considered**: A loop that auto-captures and sends the next screenshot was rejected by FR-014. Requiring confirmation for each operation was rejected by the clarification that desktop auto-executes requested mouse operations per user-driven turn.

## Decision: Multipart user-turn frame instead of multi-frame aggregation

**Rationale**: Earlier design represented a text-plus-image user turn as multiple frames (a screenshot frame and a text frame) sharing one `invoke_id`, with the agent responsible for aggregating them. The decision matrix (Q4) replaces this with a single `AgentUserTurnFrame` that bundles optional text and optional screenshot in one proto message. This eliminates the need for cross-frame aggregation logic on the agent side and makes the turn boundary explicit at the proto level.

**Alternatives considered**: Keeping the multi-frame approach was rejected because it requires agent-side state to correlate frames and delays tool invocation until all frames arrive. Sending text and image in separate turns was rejected by FR-005. Adding a separate top-level `oneof` for user input was rejected because `AgentUserTurnFrame` slots cleanly into the existing `AgentFrame.oneof payload`.

## Decision: Session-scoped OperationBridge for mouse tool

**Rationale**: The mouse tool dispatches operations to desktop via an `OperationBridge` bound to the session (Q1). This design survives WebSocket reconnects because the bridge is owned by `SessionAgent`, which outlives individual WS connections. The bidi handler registers and unregisters as the sink on connect/disconnect. A 5-second sink timeout (Q6) handles the case where no WS is connected when the tool fires, returning a failure tool result instead of hanging indefinitely.

**Alternatives considered**: A WS-connection-scoped channel was rejected because it would lose operations during reconnection. A direct gRPC call from agent to desktop was rejected because the desktop does not expose a gRPC server and the existing WS bidi stream is the established communication path.

## Decision: RefreshAgent RPC for adapter cache invalidation

**Rationale**: When the operator updates a profile's `tool_names` via `UpdateAgentProfile`, the running agent adapter holds a cached copy of the profile. `RefreshAgent` (Q2, Q12) triggers the adapter to reload the profile from the prompt service and rebuild the tool list. It routes desktop → gateway → proxy → agent and rejects with `FAILED_PRECONDITION` (Q3) if a turn is in-flight, using the existing per-session mutex.

**Alternatives considered**: Automatic cache expiration on a timer was rejected because tool changes must take effect immediately for the next turn. Having the agent poll for changes was rejected because it adds latency and complexity. Forcing the user to create a new session was rejected because it loses conversation continuity.

## Decision: UpdateAgentProfile with FieldMask

**Rationale**: `UpdateAgentProfile` (Q10) uses `google.protobuf.FieldMask` for partial updates, following repository convention (`experimental/golang/mongo_demo` uses FieldMask). The HTTP method is `PATCH` at `/api/v1/prompts/agentProfiles/{name}`. This enables mid-session editing of `tool_names` without requiring a full profile replacement.

**Alternatives considered**: A full `PUT` replacement was rejected because it requires the client to send the complete profile and risks overwriting concurrent changes. A dedicated `SetProfileTools` RPC was rejected as too narrow; FieldMask covers `tool_names` and any future writable fields uniformly.

## Decision: Remove per-turn operation limit

**Rationale**: Q11 removes the "at most one operation per turn" constraint. No per-turn operation counter is needed. The agent can request multiple mouse operations within a single turn if the LLM chooses to call the tool multiple times. FR-014 (no auto-follow-up-screenshot) naturally limits useful multi-step execution and prevents runaway loops. Removing the limit simplifies the tool implementation and removes a potential failure mode (tool call rejected due to counter).

**Alternatives considered**: Keeping the limit was rejected because the product requirement is satisfied by the no-auto-screenshot constraint. A configurable limit was rejected because no use case requires it at this milestone.

## Decision: Fake-LLM config split (messages + tools)

**Rationale**: The fake-LLM service previously matched all requests against a single config list. With the addition of tool-call flows, the last message role determines which config section to use (Q7/Q9): role `user` or `assistant` uses the `messages` config (existing keyword/substring semantics), while role `tool` uses the new `tools` config (matched by `tool_name` + optional `match_result_contains`). Each config entry declares its own trigger condition rather than a global policy.

**Alternatives considered**: A single config section with complex multi-condition matching was rejected because it mixes unrelated concerns. Matching tool responses by message content alone was rejected because tool results are structured JSON, not natural language.

## Decision: screenshot_id implicit injection

**Rationale**: Q5 decides that `screenshot_id` is NOT in the LLM tool schema. The agent injects it from the current turn context when constructing the `AgentOperationFrame`. This keeps the LLM schema simple (only `x_px`, `y_px`, `action`) and avoids leaking frame-level identifiers into the model's tool interface. The mouse tool reads `screenshot_id` from the turn context rather than from the tool arguments.

**Alternatives considered**: Including `screenshot_id` in the tool schema was rejected because it adds unnecessary complexity for the LLM and ties the schema to frame internals. Having the LLM choose the screenshot was rejected because the active screenshot is always the one from the current user turn.

## Decision: Message proto oneof content for image history

**Rationale**: Q17 extends the `Message` proto with `oneof content { string text; bytes image_data; }` to support image replay via `ListMessages`. V1 verifies that LangGraph `MemorySaver` preserves image content blocks through `JSON.stringify`/`JSON.parse` ([LangGraph checkpoint base.ts](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/base.ts), [JSON plus serializer](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/serde/jsonplus.ts)). The oneof is not coupled to frame structs; future unification is deferred per design decision.

**Alternatives considered**: Extending frame structs to carry image content was rejected because the user specified that message and frame types should not be unified at this stage ("等稳定了再把消息结构体统一"). A separate `ImageMessage` type was rejected because it duplicates the existing Message shape.

## References

### Official Documentation

- [LangChain JavaScript agents documentation](https://docs.langchain.com/oss/javascript/langchain/agents)
- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1)
- [LangChain JavaScript message content blocks](https://docs.langchain.com/oss/javascript/langchain/messages)
- [Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)
- [DOMPurify security goals and allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)

### Repositories

- [LangChain createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts)
- [LangChain multimodal content block types (V2)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/messages/content/multimodal.ts)
- [LangChain tool() function definition (V3)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/tools/index.ts)
- [LangChain ChatOpenAI completions converter (V4)](https://github.com/langchain-ai/langchainjs/blob/d43194b62/langchain-openai/src/converters/completions.ts)
- [LangGraph checkpoint base.ts (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/base.ts)
- [LangGraph checkpoint JSON plus serializer (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/serde/jsonplus.ts)
- [marked package.json](https://github.com/markedjs/marked/blob/v18.0.5/package.json)
- [DOMPurify package.json](https://github.com/cure53/DOMPurify/blob/3.4.11/package.json)

### Articles & RFCs

- No article or RFC references.
