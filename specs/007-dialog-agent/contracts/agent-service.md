# Contract: Agent Service and Dialog Frames

## Service boundary

The production service remains the dominion `game/agent:grpc` service discovered by the existing proxy. The implementation changes to grpc-js, but the service identity, stateful deployment, and gRPC port remain stable.

## Existing RPCs preserved

From `projects/game/game.proto`:

- `AgentService.CreateAgent(AgentCreateRequest) returns (Agent)`
- `AgentService.GetAgent(AgentGetRequest) returns (Agent)`
- `AgentService.DeleteAgent(AgentDeleteRequest) returns (google.protobuf.Empty)`
- `AgentService.Connect(stream AgentFrame) returns (stream AgentFrame)`

## Dialog behavior over `Connect`

The dialog agent uses the existing `AgentFrame` stream. A user text message is represented by an `AgentFrame` carrying text content from the desktop/gateway side. The agent responds with:

1. one or more `thinking` frames containing visible intermediate thinking text;
2. one final `text` frame containing the assistant response;
3. optional `warn` frames for user-visible recoverable errors.

Frame ordering is observable and significant:

```text
user text frame -> thinking frame(s) -> final text frame
queued user text frame -> thinking frame(s) -> final text frame
```

## Frame protocol mapping

The `AgentFrame` message carries frame-level metadata that maps to higher-level conversation concepts:

| AgentFrame field | Mapping | Description |
|---|---|---|
| `sender` | `FrameSender` enum | One of `USER`, `AGENT`, or `SYSTEM`. Identifies which side produced the frame. |
| `invoke_id` | `turnId` | Every frame in the same conversational turn shares the same `invoke_id`. The gateway and desktop use this value as the `turnId` on `ConversationMessage` entries. |
| `seq` | Sequence counter | Monotonically increasing integer scoped to the stream. The first frame sent by either side is `seq=1`. Enables ordered delivery and gap detection. |

**Frame type to sender mapping**:

| Frame type | Sender | Description |
|---|---|---|
| User text | `USER` | Message originating from the desktop user. |
| Thinking | `AGENT` | Intermediate reasoning output from the agent LLM. |
| Text (response) | `AGENT` | Final assistant response. |
| Warn | `SYSTEM` | Recoverable error or warning (e.g., LLM timeout, rate limit). |
| Error / Disconnect | `SYSTEM` | Unrecoverable error — gateway should translate to a disconnect signal. |

## CreateAgent semantics

- `agent_profile_name` is required.
- The service fetches the profile from PromptService during creation.
- The agent instance copies model and prompt data at creation time.
- Existing active instances continue with copied prompt data after profile deletion or update.
- Missing profile returns a non-OK gRPC status that the gateway exposes as a non-2xx HTTP response.

## Queueing semantics

- If a user text frame arrives while the instance is processing, the message is appended to the per-instance FIFO queue.
- Queued messages are processed after the current final response is emitted.
- The runtime must not run two model calls concurrently for the same session.

## Cleanup semantics

- An agent inactive for more than 15 minutes is eligible for cleanup when it is not processing.
- Cleanup must occur within 1 minute after the threshold in normal runtime operation.
- A processing instance is preserved even if its last received message is older than 15 minutes.

## Secret handling

- Provider credential content must not be logged, included in errors, or returned through any RPC.
- Missing mounted secret file is allowed and becomes an empty provider secret.
- Existing unreadable or malformed secret files must produce descriptive errors without exposing content.
