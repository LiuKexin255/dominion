# Contract: Step3.b TypeScript Agent Runtime gRPC Surface

## Scope

The TypeScript agent service implements the existing protobuf `AgentService` from `projects/game/game.proto` using `@grpc/grpc-js`. This contract documents runtime behavior and compatibility expectations; `game.proto` remains the source of truth for wire messages.

## Service: `projects.game.AgentService`

### RPC: `CreateAgent(AgentCreateRequest) returns (Agent)`

**Preconditions**:
- `session_id` is non-empty.
- `agent_profile_name` is non-empty.
- Prompt service is reachable for profile/SKILL lookup.
- Runtime MCP registry and provider adapters are initialized.

**Behavior**:
1. Load enabled `AgentProfile` by `agent_profile_name` from prompt service.
2. Load every referenced enabled `Skill` from prompt service.
3. Validate every profile `mcp_names` entry against the runtime-owned MCP/tool registry.
4. Resolve `model`:
   - default DeepAgents provider path, or
   - `opencode-go/<model-id>` with OpenCode Go credential validation.
5. Construct one `createDeepAgent` runtime with profile `system_prompt`, loaded SKILL content, selected model, and built-in desktop-operation tool.
6. Store runtime state in memory and return existing `Agent` resource fields including `agent_profile_name`.

**Error Contract**:
- Missing/disabled profile: configuration error; no agent is created.
- Missing/disabled SKILL: configuration error naming the SKILL; no agent is created.
- Unsupported MCP: configuration error naming the MCP; no agent is created.
- Malformed/unsupported OpenCode Go model ref: provider configuration error; no agent is created.
- Missing/empty/unreadable/invalid/unauthorized OpenCode Go credential: provider credential error; no agent is created.

### RPC: `GetAgent(AgentGetRequest) returns (Agent)`

**Behavior**:
- Returns current in-memory agent for `session_id`.
- Returns not-found if no agent exists or it has been idle-deleted.

### RPC: `DeleteAgent(AgentDeleteRequest) returns (google.protobuf.Empty)`

**Behavior**:
- Cancels active invoke if present.
- Releases DeepAgent/runtime resources.
- Deletes pending operation and lifecycle timers.
- Is idempotent: missing or already deleted agents return success.

### RPC: `Connect(stream AgentFrame) returns (stream AgentFrame)`

**Preconditions**:
- First or early input identifies `session_id` for an existing runtime agent.
- Frames follow existing monotonic sequence semantics.

**Input Frames**:
- `screenshot`: starts a new invoke if idle, or continues a pending operation if sequence and screenshot-only continuation are valid.
- `status`/`echo`: handled compatibly with step3.a expectations.
- Out-of-order or stale frames: do not advance runtime state and produce `warn` frame.

**Output Frames**:
- `ack`: acknowledges accepted input frames when applicable.
- `status`: lifecycle changes such as invoking, timeout, waiting, deleted.
- `thinking`: DeepAgent reasoning/progress content suitable for desktop timeline display.
- `text`: user-visible model output.
- `operation`: at most one desktop operation per invoke.
- `warn`: protocol, provider, timeout, invalid operation, or sequence warnings.
- `wait` / `screenshot_request`: emitted when waiting for next screenshot after operation or requiring user input.

**Streaming Guarantees**:
- Progressive DeepAgent events are emitted before final invoke completion whenever the model/tool runtime produces them.
- All output frames for one invoke share `invoke_id` and use monotonically increasing `sequence`.
- Operation coordinates are screenshot-relative pixels and must reference the originating `screenshot_id`.
- No separate operation-result frame is required; the next accepted screenshot is the only main-flow observation after a desktop operation.

## Runtime Tool Contract: Desktop Operation Tool

**Tool Input Schema**:

```text
operation_kind: "mouse" | "keyboard"
mouse.button: "LEFT" | "RIGHT"
mouse.click_type: "SINGLE" | "DOUBLE"
mouse.x_px: integer within screenshot width
mouse.y_px: integer within screenshot height
keyboard.key_codes: non-empty string
screenshot_id: non-empty string from current input screenshot
```

**Postconditions**:
- Valid tool call emits one `AgentOperationFrame`.
- After one operation is emitted, additional operation attempts in the same invoke are rejected with a warning/tool error.
- Out-of-bounds mouse coordinates are rejected before desktop execution.

## Provider Contract: OpenCode Go

**Model Reference**: `opencode-go/<model-id>`.

**Credential Source**: deployment secret material exposed to the TypeScript service at runtime. Profile, prompt, desktop request, and frame payloads must not contain raw provider keys.

**Validation Timing**: during `CreateAgent`, before returning `Agent`.

**Supported Endpoint Families**:
- OpenAI-compatible models: `https://opencode.ai/zen/go/v1/chat/completions`.
- Anthropic-compatible models: `https://opencode.ai/zen/go/v1/messages`.
- Model discovery/metadata: `https://opencode.ai/zen/go/v1/models` when live validation is required.

## Compatibility Contract

- Public HTTP resources remain:
  - `/api/v1/sessions/{session_id}`
  - `/api/v1/sessions/{session_id}/agent`
  - `/api/v1/sessions/{session_id}/agent/connect`
  - prompt resources under `/api/v1/prompts/...` as currently defined in `game.proto`.
- Proxy/gateway/session external routing remains unchanged.
- `projects/game/game.proto` message semantics remain compatible; any required proto extension must be planned as a separate explicit contract change and generated code must remain uncommitted.

## Acceptance Scenarios

1. Create profile + SKILL through prompt service, then `CreateAgent` with that profile returns usable `Agent`.
2. Connect through gateway WebSocket path, send PNG screenshot frame, observe at least one progressive thinking/text/tool/status frame before final operation.
3. Operation frame references originating screenshot and contains a supported mouse/keyboard action.
4. Sending next screenshot after desktop operation continues the same gameplay loop as the only operation-result observation.
5. Unsupported MCP, missing SKILL, and OpenCode Go credential failures all reject `CreateAgent` before an agent is visible.
6. Invoke timeout and idle cleanup produce observable status/warn or deletion behavior.
