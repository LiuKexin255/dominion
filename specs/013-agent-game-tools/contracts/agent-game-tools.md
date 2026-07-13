# Contract: Agent Game Tools and Image Turns

This contract extends `projects/game/game.proto` and the generated desktop/frontend API. The implementation must keep proto as the source of truth and regenerate Go/TypeScript bindings through the existing Bazel/Gazelle flow.

## AgentProfile

Add:

```proto
repeated string tool_names = 10;
```

Contract:

- Prompt create/list/get responses include `tool_names`.
- Missing or empty `tool_names` means no tools are exposed.
- Unknown names are not exposed to LangChain tools and produce a warning visible to operators or tests.

## CreateAgentProfileRequest

Add `tool_names` field:

```proto
message CreateAgentProfileRequest {
  // existing fields 1-6 (agent_profile_name, model, system_prompt, skill_names, mcp_names, enabled)
  repeated string tool_names = 7;
}
```

## UpdateAgentProfile RPC

New RPC on `PromptService`:

```proto
rpc UpdateAgentProfile(UpdateAgentProfileRequest) returns (AgentProfile) {
  option (google.api.http) = {
    patch: "/api/v1/prompts/{name=agentProfiles/*}"
    body: "profile"
  };
}

message UpdateAgentProfileRequest {
  string name = 1;                              // resource name "agentProfiles/{id}"
  AgentProfile profile = 2;
  google.protobuf.FieldMask update_mask = 3;
}
```

Contract:

- `FieldMask` is used for partial updates, consistent with repository convention (`experimental/golang/mongo_demo` uses FieldMask).
- `UpdateMask` supports editing `tool_names` among other writable profile fields.
- HTTP method is `PATCH`, not `PUT`.
- `name` carries the full AgentProfile resource name (e.g. `agentProfiles/my-profile`); the prompt handler parses it to the business id before calling the domain layer, so the domain never sees the URI prefix.

## RefreshAgent RPC

RPC on **both** `AgentService` and `ProxyService`. On `ProxyService` it is user-facing with an HTTP annotation so the desktop can trigger it after a profile update:

```proto
// On ProxyService (proxy-side forwarding to agent owner node):
rpc RefreshAgent(RefreshAgentRequest) returns (google.protobuf.Empty) {
  option (google.api.http) = {
    post: "/api/v1/{name=sessions/*/agent}:refresh"
    body: "*"
  };
}

// On AgentService (agent-side implementation, internal gRPC only):
rpc RefreshAgent(RefreshAgentRequest) returns (google.protobuf.Empty);

message RefreshAgentRequest {
  string name = 1;   // Agent resource name "sessions/{session}/agent"
}
```

Contract:

- Routes: desktop → gateway → proxy → agent. The proxy handler resolves the owner for the session and forwards to the owning agent instance.
- Rejects with `FAILED_PRECONDITION` if a turn is in-flight for the given session (agent service tracks per-session mutex).
- The proxy-side RPC is user-facing via `POST /api/v1/sessions/{session}/agent:refresh`; desktop calls it after `UpdateAgentProfile` so the agent reloads its adapter with the new profile configuration.
- The agent-side handler parses the Agent resource `name` to recover the session id used as the adapter cache key.
- The profile's updated `tool_names` take effect on the next turn after the refresh completes.
- The proxy does NOT lazily create an owner for `RefreshAgent` (nor for `GetAgent`/`ListMessages`); an owner is allocated only by `ConnectAgent`. A missing owner returns `NOT_FOUND`.

## Resource Naming (AIP Compliance)

All resources in `game.proto` follow Google AIP resource-naming (`style/api.md`):

- `Session`, `Agent`, `AgentProfile`, `Skill`, and `Message` each declare `option (google.api.resource)` with a `pattern` and carry a canonical `name` field holding the full resource name.
- The redundant business-id peer fields (`AgentProfile.agent_profile_name`, `Skill.skill_name`) are removed from the resource messages; the id is the last path segment of `name`. Create requests still accept a user-supplied id field (`agent_profile_name`/`skill_name`) per AIP-133.
- Get/Update/Delete requests use a `name` field (full resource name) with `google.api.resource_reference`, not a bare id.
- HTTP patterns use `{name=<collection>/*}` so grpc-gateway captures the full resource name from the URL.
- `RefreshAgentRequest.name` carries the Agent resource name. The internal `AgentGetRequest` (agent-to-agent, no HTTP) keeps `session_id`.

## Proxy Handler Layer (No Separate Service)

The proxy no longer has a `service/` package. `ProxyHandler` in `proxy/handler` owns owner resolution, agent-client routing, and stream binding directly:

- `GetAgent`/`ListMessages`/`RefreshAgent` require an existing owner (`ownerStore.Get`); they return `NOT_FOUND` when no owner exists.
- `ConnectAgent` is the only RPC that allocates an owner (pick + persist).
- The previous `resolveOwner` lazy-create wrapper is removed.

The session service also drops its (empty) `service/` package; `SessionHandler` in `session/handler` holds the repository directly.


## AgentFrame Payloads

Extend `AgentFrame.oneof payload` with:

```proto
AgentOperationResultFrame operation_result = 22;
AgentUserTurnFrame user_turn = 23;
```

Existing `AgentScreenshotFrame`, `AgentTextFrame`, `AgentOperationFrame`, and `AgentUserTurnFrame` are frame payloads. `AgentUserTurnFrame` is the sole user-to-agent input carrier, bundling optional text and optional screenshot in one frame. Standalone `AgentScreenshotFrame` as a direct user-to-agent payload is deprecated. `AgentTextFrame` is retained for agent-to-desktop text output only.

### AgentUserTurnFrame

```proto
message AgentUserTurnFrame {
  string text = 1;
  AgentScreenshotFrame screenshot = 2;
}
```

Contract:

- `AgentUserTurnFrame` replaces the multi-frame aggregation approach for user input. The agent receives text and screenshot data together in one logical frame.
- At least one of `text` or `screenshot` MUST be present.
- The agent constructs a single LangChain multimodal `HumanMessage` from the frame fields: text block when `text` is present, image block when `screenshot` is present.

## Mouse Action

Replace or supersede the current button/click pair for mouse operations with an action enum capable of simultaneous left-right press:

```proto
enum AgentMouseAction {
  AGENT_MOUSE_ACTION_UNSPECIFIED = 0;
  AGENT_MOUSE_ACTION_LEFT_CLICK = 1;
  AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK = 2;
  AGENT_MOUSE_ACTION_RIGHT_CLICK = 3;
  AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK = 4;
  AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS = 5;
}

message AgentMouseOperation {
  AgentMouseAction action = 1;
  int32 x_px = 2;
  int32 y_px = 3;
}
```

Contract:

- `x_px` and `y_px` are screenshot-relative pixels.
- Desktop validates bounds against the referenced screenshot before converting to screen coordinates.
- Desktop auto-executes requested mouse operations without requiring a separate operator confirmation.

## Operation Result Frame

Add:

```proto
enum AgentOperationResultStatus {
  AGENT_OPERATION_RESULT_STATUS_UNSPECIFIED = 0;
  AGENT_OPERATION_RESULT_STATUS_SUCCEEDED = 1;
  AGENT_OPERATION_RESULT_STATUS_FAILED = 2;
}

message AgentOperationResultFrame {
  string operation_id = 1;
  AgentOperationResultStatus status = 2;
  string message = 3;
}
```

Contract:

- Desktop sends exactly one result frame for every operation request it auto-executes.
- The result frame uses the same session and invoke lineage as the operation request.
- `AgentAckFrame` is not used for operation results.
- Result emission does not trigger automatic screenshot capture.

## Message Proto Oneof Content

The `Message` proto gains a `oneof content` to support image data alongside text:

```proto
message Message {
  string name = 1;
  string message_id = 2;
  FrameSender sender = 3;
  string type = 4;   // "text" | "thinking" | "warn" | "image"
  google.protobuf.Timestamp create_time = 6;
  oneof content {
    string text = 5;
    bytes image_data = 7;
  }
}
```

Contract:

- When `type == "image"`, `content` is `image_data`.
- When `type` is any other value, `content` is `text`.
- Field 5 (`text`) moves from a standalone string field into the oneof (breaking change, acceptable per feature scope).
- Image replay via `ListMessages` uses this oneof shape; the LangGraph MemorySaver checkpoint is verified to preserve image content blocks ([LangGraph checkpoint base.ts](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/base.ts), [JSON plus serializer](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/serde/jsonplus.ts)).

## OperationBridge Contract

The OperationBridge is a session-scoped communication channel between the LangChain mouse tool and the desktop WebSocket handler:

- **Scope**: One bridge per session, owned by `SessionAgent`.
- **Sink registration**: The bidi WebSocket handler registers as the current sink on Connect and unregisters on stream end.
- **Tool dispatch**: When the mouse tool fires, it writes to the bridge; the bridge routes to the currently registered sink.
- **Timeout**: If no sink is registered when the tool fires, the bridge waits up to 5 seconds. On timeout, the tool returns a failure result to LangChain. The timeout is a configurable constant.
- **Result correlation**: The bridge correlates operation results by `operation_id`.
- **Reconnect survival**: Because the bridge is session-scoped, it survives WebSocket reconnects.

## LangChain Tool Contract

The agent exposes a LangChain tool named `mouse` only for profiles declaring `tool_names` containing `mouse`. LangChain JavaScript agents support explicit `tools` arrays and tool definitions in `createAgent` ([LangChain agents docs](https://docs.langchain.com/oss/javascript/langchain/agents), [createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts)).

Tool input schema:

```json
{
  "x_px": 120,
  "y_px": 240,
  "action": "LEFT_CLICK"
}
```

Note: `screenshot_id` is NOT part of the LLM tool schema. The agent injects `screenshot_id` from the current turn context into the `AgentOperationFrame.screenshot_id` field, so the LLM does not need to know it.

Tool behavior:

- It returns a normal tool result to LangChain after desktop reports the operation result.
- It also emits an `AgentOperationFrame` to desktop for execution.

## Multimodal User Turn Contract

LangChain multimodal input uses text and image content blocks in a single `HumanMessage` ([LangChain multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1), [LangChain message content blocks](https://docs.langchain.com/oss/javascript/langchain/messages)). The agent must construct one user message with:

- A text block when user text is present.
- An image block when a screenshot is present, using PNG MIME type and the screenshot data URL or equivalent provider-compatible image URL field.

## Desktop Markdown Contract

Agent text is rendered as markdown by parsing through Marked and sanitizing the generated HTML with DOMPurify. Marked output is not sanitized by default and must be filtered ([Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)); DOMPurify supports strict allowed tag and attribute lists for markdown/comment surfaces ([DOMPurify allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)).

Allowed tags for this milestone: `p`, `br`, `strong`, `em`, `code`, `pre`, `ul`, `ol`, `li`, `blockquote`, `a`. Allowed attributes: `href`, `title`. Raw HTML outside the allow-list is stripped.

## Fake-LLM Contract

The fake-LLM service supports two config sections for deterministic test responses, selected by the role of the last message in the request:

### `messages` config (existing)
- When the last message role is `user` or `assistant`, matching follows existing keyword/substring semantics.

### `tools` config (new)
- When the last message role is `tool`, matching is by `tool_name` plus optional `match_result_contains` keywords.

Config entry shape:

```yaml
- name: "mouse-success-response"
  tool_name: "mouse"
  match_result_contains: []    # optional
  respond_with:
    text: "..."                # final text response
    # OR
    tool_call:                 # another tool call (supports multi-step)
      name: "mouse"
      arguments: { x_px: 120, y_px: 240, action: "LEFT_CLICK" }
```

Contract:

- Each fake config entry declares its own trigger condition.
- When the last message role is `user` or `assistant`, the `messages` config branch is used.
- When the last message role is `tool`, the `tools` config branch is used, matching by `tool_name`.
- The fake-LLM MUST parse incoming `image_url` formatted content blocks (as used by ChatOpenAI in `@langchain/openai` 1.5.x [completions converter (V4)](https://github.com/langchain-ai/langchainjs/blob/d43194b62/langchain-openai/src/converters/completions.ts)) when responding to multimodal requests.
- Config entries are stateless and independently matchable; no ordering guarantee.

## References

### Official Documentation

- [LangChain JavaScript agents documentation](https://docs.langchain.com/oss/javascript/langchain/agents)
- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1)
- [LangChain JavaScript message content blocks](https://docs.langchain.com/oss/javascript/langchain/messages)
- [Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)
- [DOMPurify security goals and allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)

### Repositories

- [LangChain createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts) — used to verify `tools` parameter in `createAgent`.
- [LangChain multimodal content block types (V2)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/messages/content/multimodal.ts) — HumanMessage v1 multimodal constructor shape with `type: "text"` / `type: "image"` content blocks.
- [LangChain tool() function definition (V3)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/tools/index.ts) — `tool()` factory re-exported from `langchain` package, requires Zod schema for structured input.
- [LangChain ChatOpenAI completions converter (V4)](https://github.com/langchain-ai/langchainjs/blob/d43194b62/langchain-openai/src/converters/completions.ts) — ChatOpenAI serializes image content as `{ type: "image_url", image_url: { url: "data:..." } }` in Chat Completions API.

### Articles & RFCs

- No article or RFC references.
