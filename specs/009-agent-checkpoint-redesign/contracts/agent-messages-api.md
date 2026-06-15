# Contract: Agent Messages API

## Purpose

Expose checkpoint-backed conversation messages through a dedicated request-response API so the desktop play page can hydrate prior messages before accepting new input. Each message is addressable by a stable resource name whose identifier is the native framework message id — no parallel identity system is introduced.

## Resource: Message

A `Message` is one normalized conversation entry reconstructed from checkpoint state. Transient stream control signals (e.g. `wait` frames) and anything not required for breakpoint reconnect MUST NOT be materialized as a `Message`.

**Resource name pattern**: `sessions/{session_id}/agent/messages/{message_id}`

- `{session_id}` is the owning session identifier and the checkpoint `thread_id`.
- `{message_id}` is **the native LangChain `BaseMessage.id`** (UUID v4) carried inside checkpoint state. The agent service MUST surface this framework-owned identifier directly. It MUST NOT mint, wrap, or hash a parallel identifier. This keeps message identity consistent across LangGraph operations (`RemoveMessage`, the messages reducer) and the REST/gRPC surface.

**Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Full resource name `sessions/{session_id}/agent/messages/{message_id}`. |
| `message_id` | string | The native LangChain `BaseMessage.id`. Read-only; assigned by the framework's messages reducer. |
| `sender` | `FrameSender` | `USER`, `AGENT`, or `SYSTEM`. |
| `type` | string | `text`, `thinking`, or `warn`. Derived from the underlying checkpoint message shape. |
| `content` | string | Visible display content. |
| `create_time` | timestamp | Best-effort message timestamp sourced from the checkpoint that introduced the message (`StateSnapshot.createdAt`). Omitted when unavailable. |

## Agent Service RPC

### `ListMessages`

**Request**: `ListMessagesRequest`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `parent` | string | yes | The parent agent resource name `sessions/{session_id}/agent` whose messages are requested. |

**Response**: `ListMessagesResponse`

| Field | Type | Description |
|-------|------|-------------|
| `messages` | repeated `Message` | Chronological display messages. Empty for a new agent. |

## Gateway REST Surface

### `GET /api/v1/{parent=sessions/*/agent}/messages`

Returns the same logical data as `ListMessagesResponse` in JSON form. The gateway forwards the request through the existing proxy/agent path. The `parent` path parameter captures `sessions/{session_id}/agent`.

Individual messages are addressable by the resource pattern `GET /api/v1/{name=sessions/*/agent/messages/*}` for future retrieval; only list is required by this feature.

## Behavior

- Existing agent with no turns: returns `200 OK` and empty `messages`.
- Existing agent with turns: returns `200 OK` and messages in chronological order, each carrying its native `message_id`.
- Missing/deleted agent: returns not-found semantics consistent with existing GetAgent behavior.
- Deleted/recreated agent: returns only the new agent's messages; old checkpoint messages MUST NOT appear.
- Agent service process restart: returns not-found if in-memory metadata/checkpoint state is gone.
- **Transient stream signals excluded**: `wait` frames and any other control signal not required for breakpoint reconnect MUST NOT appear in messages. Only `text`, `thinking`, and `warn` content carried by checkpoint state is materialized.
- **`thinking` is best-effort**: included only when the provider/model persisted reasoning content into the checkpoint message; silently omitted otherwise.
- **`create_time` is best-effort**: populated from the introducing checkpoint's `createdAt`; omitted when the framework does not expose one.

## Validation

- Unit tests cover agent-side message extraction from checkpoint state, including `message_id` provenance from `BaseMessage.id`, chronological ordering, and exclusion of `wait`/control signals.
- Contract tests cover gateway REST mapping to the agent `ListMessages` RPC.
- Desktop tests or manual QA verify the chat page shows prior messages before new input.
