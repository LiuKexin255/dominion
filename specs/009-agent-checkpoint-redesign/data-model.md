# Data Model: Agent Checkpoint & Session UI Redesign

## Entity: Session

Represents an existing game session. A session owns at most one active agent at a time.

**Fields**:
- `sessionId`: stable unique session identifier; also used as checkpoint `thread_id` for the active agent.
- `name`: resource name.
- `createTime`: session creation timestamp.

**Relationships**:
- Has zero or one Agent Metadata record.
- Has zero or one Checkpoint State thread while an agent exists.

## Entity: Agent Metadata

Lightweight in-memory configuration for a created agent. It records how to construct/invoke the agent but does not hold conversation state.

**Fields**:
- `sessionId`: owner session and checkpoint thread identifier.
- `name`: agent resource name (`sessions/{sessionId}/agent`).
- `agentProfileName`: profile used at creation.
- `model`: copied profile model used for every invocation.
- `systemPrompt`: copied profile prompt used for every invocation.
- `createTime`: creation timestamp.

**Validation rules**:
- `sessionId` is required.
- `agentProfileName` is required at creation.
- One metadata record per session.
- Recreating an agent for the same session is allowed only after deleting or replacing the previous metadata and clearing its checkpoint state.

**State transitions**:
```text
Absent ──CreateAgent(profile)──► Present
Present ──GetAgent─────────────► Present
Present ──DeleteAgent──────────► Absent + Checkpoint State cleared
Present ──Session deleted──────► Absent + Checkpoint State cleared
```

## Entity: Checkpoint State

In-memory conversation state owned by the native checkpoint mechanism.

**Fields**:
- `threadId`: equal to `sessionId`.
- `messages`: chronological conversation messages stored by the checkpointed graph.
- `checkpoint metadata`: framework-managed metadata such as checkpoint identifiers and step metadata.

**Validation rules**:
- `threadId` MUST equal the owning session's `sessionId`.
- Deleting an agent MUST delete the full thread checkpoint state.
- Recreating an agent for the same session MUST start with no prior messages.

**State transitions**:
```text
Empty thread ──first turn────────────► Has messages
Has messages ──subsequent turn──────► Has more messages
Has messages ──ListMessages─────────► Has messages (read-only)
Has messages ──DeleteAgent/Session──► Deleted thread
```

## Entity: Message

Normalized desktop-facing representation of one checkpoint message, addressable by a stable resource name.

**Resource name**: `sessions/{sessionId}/agent/messages/{message_id}`

**Fields**:
- `message_id`: the native LangChain `BaseMessage.id` (UUID v4) carried inside checkpoint state. The framework's messages reducer assigns it; the agent service surfaces it directly without minting a parallel identifier.
- `sender`: `USER`, `AGENT`, or `SYSTEM`.
- `type`: `text`, `thinking`, or `warn`.
- `content`: visible message content.
- `create_time`: best-effort timestamp sourced from the checkpoint that introduced the message (`StateSnapshot.createdAt`); omitted when unavailable.

**Validation rules**:
- Messages are returned in chronological display order.
- Empty content entries are omitted unless needed to preserve an explicit warning/system state.
- `wait` frames and any other transient stream control signal not required for breakpoint reconnect MUST NOT be materialized as messages.
- `thinking` content is best-effort: included only when the provider persisted reasoning into the checkpoint message.
- `ListMessages` returns an empty list for a newly created agent with no conversation turns.

## Entity: Agent Profile

Pre-existing prompt service entity used to configure an agent.

**Fields used by this feature**:
- `agentProfileName`
- `name`
- `model`
- `systemPrompt`
- `skillNames`
- `mcpNames`
- `enabled`
- `createTime`
- `updateTime`

**Relationships**:
- Agent Metadata copies profile `agentProfileName`, `model`, and `systemPrompt` at creation time.
- Desktop uses profile details to populate the play sidebar's view-profile control.

## UI State Model

### Session Detail State

```text
No session selected
  └─ select session → checking agent metadata

checking agent metadata
  ├─ no agent → setup required
  ├─ agent exists → agent ready
  └─ load error → error with retry

setup required
  └─ create agent(profile) → agent ready

agent ready
  ├─ enter play → play connecting
  ├─ delete agent → setup required
  └─ delete session → sessions list
```

### Play State

```text
play connecting
  ├─ connected → loading messages
  └─ connection error → error with retry/send fallback

loading messages
  ├─ messages loaded → chat ready
  └─ messages error → chat ready with warning

chat ready
  ├─ send message → processing turn
  ├─ back to detail → agent ready
  └─ connection lost → reconnecting/error

processing turn
  └─ wait frame received → chat ready
```

## Deletion and Rebuild Invariants

- Agent deletion clears metadata and checkpoint thread data for the session.
- Session deletion clears agent metadata and checkpoint thread data if present.
- Creating a new agent for a session after deletion starts from an empty checkpoint state.
- The `ListMessages` API must never return messages from a deleted agent.
