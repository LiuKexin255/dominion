# Data Model: Agent Adapter Decoupling and LangChain Foundation

## SessionAgent

The logical conversation agent bound 1:1 to a session. Owns conversation history.

### Fields

- `session_id`: session identity and conversation continuity key. Also used as `thread_id` for the LangGraph checkpointer.
- `active_adapter`: the currently bound AgentAdapter, or null if no adapter is active (no connection or between unbind and new bind).
- `active_profile_name`: the profile name of the currently bound adapter, or empty.
- `create_time`: best-effort timestamp; matches session creation time since SessionAgent lifecycle = session lifecycle.

### Relationships

- Belongs to exactly one Session (1:1).
- Owns one Conversation History scoped by `thread_id = session_id`.
- Has at most one active AgentAdapter at any time.
- Future: may own sub-agents (each with their own SessionAgent and independent history). Out of scope for this release.

### Validation Rules

- SessionAgent exists implicitly when its Session exists. No explicit creation step.
- At most one AgentAdapter is bound at any time.
- SessionAgent lifecycle matches Session lifecycle. Session deletion may leave orphaned MemorySaver entries (acceptable for in-memory scope; cleared on process restart).
- A SessionAgent with no prior conversation has empty history (not an error state).

## AgentAdapter

The profile-specific execution processor. Stateless — does not own history.

### Fields

- `profile_name`: the agent profile name used to construct this adapter.
- `model`: model identifier from the profile, used for every turn.
- `system_prompt`: system instruction from the profile, passed as the static `systemPrompt` parameter to `createAgent` at adapter construction time.
- `agent`: the compiled `createAgent` graph instance (LangChain). Stateless; all conversation state externalized to the checkpointer.

### Relationships

- Bound to at most one SessionAgent at a time.
- Reads/writes Conversation History through the shared `MemorySaver` checkpointer via `thread_id = session_id`.
- Does not persist between profile switches. Created on-demand, cleaned up when a different profile is selected. Stays bound across WebSocket disconnects (reused on reconnect).

### Lifecycle

1. **Creation**: when a user sends a text frame with `agent_profile_name = X` and no adapter for X is bound:
   - Fetch profile X from the prompt service.
   - Build `createAgent({ model, systemPrompt, middleware: [beforeModelMiddleware], checkpointer })`.
   - Bind to SessionAgent.
2. **Switching**: when a user sends a text frame with `agent_profile_name = Y` while adapter X is bound (Y ≠ X):
   - Synchronously unbind adapter X from SessionAgent.
   - Create adapter Y (same as creation).
   - Bind adapter Y to SessionAgent.
   - Asynchronously clean up adapter X.
3. **Disconnect**: when the WebSocket connection drops, the adapter stays bound. It is reused on reconnect or until a profile switch triggers cleanup.

### Validation Rules

- Only one adapter may be bound to a SessionAgent at any time.
- Adapter creation requires a valid profile name from the prompt service.
- If the profile does not exist, a warning frame is returned and the current adapter (if any) remains bound.
- The adapter does not own conversation history. History persists in the checkpointer regardless of adapter state.

## Conversation History

Session-scoped message collection stored in the shared LangGraph `MemorySaver` checkpointer.

### Fields

- `thread_id` (`session_id`): unique key for the conversation thread.
- `messages`: ordered collection of conversation messages held in the LangGraph state (`state.values.messages` when using `MessagesAnnotation`). Shared across all profile adapters within the same session.
- `updated_at`: best-effort timestamp from checkpoint state.

### Relationships

- Scoped to one SessionAgent and one Session.
- Read by the Context Preparation middleware via the LangGraph `state` object before each model call.
- Read by `ListMessages` via standard `createAgent` graph state API.
- Written automatically by the `createAgent` harness after each model response.

### Validation Rules

- Messages are isolated by `thread_id` (`session_id`).
- Same-session turns are appended in send order by the `createAgent` harness.
- Conversation state is removed when the session is deleted.
- Conversation state is not durable across service process restarts.
- History is shared across all profile adapters within the same session — switching profiles does not create a new history thread.

## Conversation Message

One context-bearing or visible message in a session conversation.

### Fields

- `message_id`: stable message identifier.
- `sender`: user, agent, or system.
- `type`: `text`, `thinking`, or `warn` for public message listing.
- `content`: visible or context-bearing message text.
- `create_time`: timestamp when available from the continuity state.

### Validation Rules

- `ListMessages` returns messages in chronological order.
- Empty/transient control messages are not surfaced as public conversation messages.
- `wait` frames are live stream control signals and are not conversation messages.

## Context Preparation

Service-owned boundary that prepares model input from stored conversation data. Implemented as `createAgent` middleware at the `beforeModel` stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)).

### Inputs

- `state.messages`: ordered prior messages from the LangGraph checkpoint.
- `system_prompt`: profile instruction passed as the static `systemPrompt` parameter to `createAgent` at adapter construction time.
- `config.configurable.thread_id`: conversation continuity key.
- `current_user_message`: incoming user text appended to the message list by the handler before invocation.

### Middleware Components

- **`createAgent` built-in `systemPrompt`**: the profile's system prompt is passed as a construction parameter to `createAgent`. Since each adapter is built for one profile, the prompt is a static constant — no dynamic injection is needed.
- **Service-owned `beforeModel` middleware** (future customization point): receives the current `state`, applies the fixed format policy, and returns the updated state. In this release it is behavior-compatible; future work can replace it with summarization, trimming, or custom formatting.

### Validation Rules

- The boundary must be identifiable in implementation and tests.
- Public clients cannot supply or alter the format policy in this feature.
- The system prompt must be injected as a model instruction, not as a visible conversation message in `ListMessages`.

## Agent Profile Snapshot

Immutable service-side copy of profile fields used to construct an AgentAdapter.

### Fields

- `agent_profile_name`: original profile name.
- `model`: model configured by the profile.
- `system_prompt`: system instruction from the profile.

### Relationships

- Fetched from the prompt service at adapter creation time.
- Does not require the source profile to continue existing for the adapter to function during its bound lifetime.

## State Transitions

```text
Session Created
  └─ SessionAgent exists (empty history, no adapter)

SessionAgent (no adapter)
  └─ User sends text frame with profile X → SessionAgent + Adapter X bound

SessionAgent + Adapter X bound
  ├─ User sends text frame with profile X → Message processed, history appended
  ├─ User sends text frame with profile Y → Adapter X unbound + Adapter Y created + bound
  ├─ Connection disconnects → Adapter stays bound → SessionAgent (same adapter, reused on reconnect)
  └─ New connection opens (kick old if active) → SessionAgent (no adapter until first message)

SessionAgent + Adapter Y bound (after switch from X)
  └─ History includes messages from both X and Y (shared thread_id = session_id)

Session Deleted
  └─ SessionAgent state becomes orphaned (history remains in MemorySaver until process restart)
```

## Connection State Transitions

```text
No connection
  └─ WebSocket connect → Connection Active

Connection Active
  ├─ New WebSocket connect to same session → Old connection closed, new Connection Active
  └─ WebSocket disconnect → No connection (adapter stays bound, reused on reconnect)
```

## Compatibility Invariants

- `session_id` remains the conversation continuity and isolation key (`thread_id`).
- Conversation history is shared across all profile adapters within the same session.
- Same-session message generation remains serialized (FIFO).
- Model/profile behavior is fetched fresh from the prompt service at adapter creation time.
- AgentFrame payload types (text, thinking, status, echo, warn, wait, etc.) remain unchanged.
- SessionService and PromptService contracts remain unchanged.
- Sub-agent support (multiple logical agents per session with independent histories) is architecturally precluded by naming but not implemented in this release.
