# Data Model: Agent Game Tools and Image Turns

## Tool Declaration

- **Owner**: Prompt service agent profile.
- **Fields**:
  - `tool_names: string[]` — built-in agent tool names declared for the profile.
- **Validation rules**:
  - Missing or empty list means no tools.
  - Unknown tool names are ignored for runtime exposure and surfaced as profile validation warnings.
  - For this milestone, the only recognized value is `mouse`.
- **Relationships**: Read by the agent adapter when binding or switching the active profile for a session.

## Screenshot Attachment

- **Owner**: Desktop play UI until sent; shared frame after send.
- **Fields**:
  - `screenshot_id: string`
  - `capture_id: string`
  - `encoding: IMAGE_ENCODING_PNG`
  - `data: bytes`
  - `width_px: int32`
  - `height_px: int32`
  - `scale_factor: double`
  - `window_title: string`
  - `capture_time: timestamp`
- **Validation rules**:
  - Payload must be PNG.
  - Payload must be at most 5 MiB before a user turn is sent.
  - A pending attachment can be removed before send.
  - Coordinates for later mouse operations are interpreted relative to `width_px` and `height_px`.
- **State transitions**:
  - `captured` → `pending attachment` → `published with user turn`.
  - `pending attachment` → `removed` before send.

## User Multimodal Turn (AgentUserTurnFrame)

- **Owner**: Desktop sends; agent consumes.
- **Fields**:
  - `text?: string` — optional user text.
  - `screenshot?: ScreenshotAttachment` — optional screenshot frame.
- **Model**:
  - The user turn is carried by a single `AgentUserTurnFrame` proto message bundling optional text and optional screenshot fields.
  - This replaces the earlier multi-frame aggregation approach (multiple frames sharing one `invoke_id`).
- **Validation rules**:
  - At least one of text or screenshot must be present.
  - The agent MUST construct one LangChain multimodal `HumanMessage` using content blocks from the frame fields: a text block when `text` is present, an image block when `screenshot` is present, following the LangChain multimodal content block model ([LangChain multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1), [LangChain multimodal content block types (V2)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/messages/content/multimodal.ts)).

## Mouse Operation Tool

- **Owner**: Agent service.
- **Fields**:
  - `name: "mouse"`
  - `schema.x_px: int32`
  - `schema.y_px: int32`
  - `schema.action: MouseAction`
- **Validation rules**:
  - Exposed only when active profile `tool_names` includes `mouse`.
  - Coordinates must be screenshot-relative pixels.
  - Action must be one of left click, left double click, right click, right double click, or simultaneous left-right press.
  - `screenshot_id` is NOT in the LLM tool schema; the agent injects it from the current turn context.

## Operation Request

- **Owner**: Agent creates; desktop executes.
- **Fields**:
  - `operation_id: string`
  - `screenshot_id: string`
  - `mouse: AgentMouseOperation`
- **Validation rules**:
  - Out-of-bounds coordinates are rejected by desktop and produce a failed result.
- **State transitions**:
  - `requested` → `executing` → `succeeded` or `failed`.

## Operation Result

- **Owner**: Desktop creates; agent consumes; UI displays.
- **Fields**:
  - `operation_id: string`
  - `status: OPERATION_RESULT_STATUS_SUCCEEDED | OPERATION_RESULT_STATUS_FAILED`
  - `message: string`
- **Validation rules**:
  - Exactly one result frame is returned for every auto-executed operation request.
  - Result frames are not acknowledgement frames.
  - No screenshot is automatically captured or sent after a result.

## Tool Config (Fake-LLM)

- **Owner**: Fake-LLM service configuration.
- **Fields**:
  - `name: string` — config entry name.
  - `tool_name: string` — the tool name to match against (used in `tools` config section only).
  - `match_result_contains: string[]` — optional keywords to match against tool result content.
  - `respond_with.text: string` — final text response to return.
  - `respond_with.tool_call.name: string` — tool name for tool-call response.
  - `respond_with.tool_call.arguments: object` — tool arguments for tool-call response.
- **Sections**:
  - `messages` — matched when last message role is `user` or `assistant`; uses existing keyword/substring semantics.
  - `tools` — matched when last message role is `tool`; matches by `tool_name` plus optional `match_result_contains`.

## OperationBridge

- **Owner**: Agent service session manager.
- **Scope**: One bridge per session, owned by `SessionAgent`.
- **Sink registration**: Bidi WebSocket handler registers as sink on Connect, unregisters on stream end.
- **Dispatch**: Mouse tool writes operation to bridge; bridge routes to current sink.
- **Timeout**: 5-second configurable timeout when no sink is registered; returns failure tool result on timeout.
- **Correlation**: Results matched to requests by `operation_id`.

## Rich Conversation Entry

- **Owner**: Desktop frontend view model and agent message listing.
- **Fields**:
  - `type: "text" | "thinking" | "warn" | "image" | "operation" | "operation_result"`
  - `sender: FrameSender`
  - `content?: string`
  - `image?: ScreenshotAttachment summary`
  - `operation?: OperationRequest summary`
  - `operation_result?: OperationResult summary`
- **Content variants** (corresponding to `Message.oneof content`):
  - When `type == "image"`, the entry carries raw image data (`bytes`) sourced from `Message.image_data`.
  - When `type` is any other value, the entry carries string text sourced from `Message.text`.
- **Validation rules**:
  - User-published images are collapsed by default and expandable.
  - Agent text markdown is parsed then sanitized with a strict allow-list because Marked does not sanitize output by itself ([Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md), [DOMPurify allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)).

## References

### Official Documentation

- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1)
- [Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md)
- [DOMPurify security goals and allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model)

### Repositories

- [LangChain multimodal content block types (V2)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/messages/content/multimodal.ts) — HumanMessage v1 multimodal constructor shape with `type: "text"` / `type: "image"` content blocks.
- [LangGraph checkpoint base.ts (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/base.ts) — MemorySaver delegates serialization to JsonPlusSerializer; content blocks pass through JSON.stringify/JSON.parse intact.
- [LangGraph checkpoint JSON plus serializer (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/serde/jsonplus.ts) — preserves Uint8Array via _default/_reviver special handling.

### Articles & RFCs

- No article or RFC references.
