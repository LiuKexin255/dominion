# Data Model: Agent Engine Context Control Foundation

## Agent

Represents the one active assistant instance owned by a session.

### Fields

- `name`: resource name `sessions/{session_id}/agent`.
- `session_id`: session identity and conversation continuity key.
- `agent_profile_name`: profile copied at creation time.
- `create_time`: creation timestamp returned through existing public APIs.

### Relationships

- Belongs to exactly one `Session`.
- Has one copied `Agent Profile Snapshot` at creation time.
- Owns one active `Conversation State` for the current process lifetime.

### Validation Rules

- A session has at most one active agent.
- Creating an agent requires an existing profile name.
- Deleting an agent removes metadata and its conversation continuity state.

## Agent Profile Snapshot

Immutable service-side copy of profile fields needed to run an agent after creation.

### Fields

- `agent_profile_name`: original profile name.
- `model`: model configured by the profile; used for every turn.
- `system_prompt`: system instruction copied from the profile.
- `observable tool configuration`: current configured profile tool/skill behavior only where it is observable through existing public surfaces.

### Relationships

- Copied into `Agent` metadata at creation time.
- Does not require the source profile to continue existing for the created agent to respond.

### Validation Rules

- Profile model behavior must be preserved after the foundation replacement.
- Hidden deepagent-only defaults are not part of this entity unless they affect observable public behavior.

## Conversation State

Process-local state used to resume a session conversation and list prior messages. It is stored in the shared LangGraph `MemorySaver` checkpointer under `configurable: { thread_id: sessionId }`; the `createAgent` harness reads and writes this state automatically.

### Fields

- `thread_id` (`session_id`): unique key for the conversation thread passed to the checkpointer.
- `messages`: ordered collection of `Conversation Message` entries held in the LangGraph state (`state.values.messages` when using `MessagesAnnotation`).
- `updated_at`: best-effort timestamp for state updates when available.

### Relationships

- Scoped to one active `Agent` and one `Session`.
- Read by `Context Preparation` middleware via the LangGraph `state` object before each model call.
- Read by `ListMessages` via `agent.getState({ configurable: { thread_id: sessionId } })` to reconstruct public `Message` resources.

### Validation Rules

- Messages are isolated by `thread_id` (`session_id`).
- Same-session turns are appended in send order by the `createAgent` harness.
- Conversation state is removed on agent deletion via `checkpointer.deleteThread(sessionId)`.
- Conversation state is not durable across service process restarts in this feature.

## Conversation Message

One context-bearing or visible message in a session conversation.

### Fields

- `message_id`: stable message identifier used in `Message.name` when available.
- `sender`: user, agent, or system.
- `type`: `text`, `thinking`, or `warn` for public message listing and frame compatibility.
- `content`: visible or context-bearing message text.
- `create_time`: timestamp when available from the continuity state.

### Relationships

- Belongs to one `Conversation State`.
- May be emitted as one or more `AgentFrame` values during live streaming.
- May be returned as a public `Message` from `ListMessages`.

### Validation Rules

- `ListMessages` returns messages in chronological order.
- Empty/transient control messages are not surfaced as public conversation messages.
- `wait` frames are live stream control signals and are not conversation messages.

## Context Preparation

Service-owned boundary that prepares model input from stored conversation data. In this design it is implemented as one or more `createAgent` middleware functions running at the `beforeModel` stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)).

### Fields / Inputs

- `state.messages`: ordered prior messages from the LangGraph checkpoint (the `Conversation State`).
- `config.context.system_prompt`: profile snapshot instruction injected per invocation.
- `config.context.session_id` / `thread_id`: conversation continuity key passed at invocation time.
- `current_user_message`: incoming user text appended to the message list by the handler before invocation.
- `format_policy`: currently fixed to existing behavior; future custom-history work may parameterize or replace the middleware.

### Middleware Components

- **`dynamicSystemPromptMiddleware`**: LangChain-provided middleware ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory)) that injects the session's system prompt before each model call. It reads `config.context.systemPrompt` (supplied by the handler) and produces a `SystemMessage`.
- **Service-owned `beforeModel` middleware** (the future customization point): receives the current `state`, applies the fixed format policy (e.g., prepend system prompt, keep all prior messages, append current user turn), and returns the updated state. In the current release this middleware is a no-op or identity transform beyond what `dynamicSystemPromptMiddleware` already does; in the future it can be replaced with summarization, trimming, or custom formatting.

### Relationships

- Reads `Conversation State` via the LangGraph `state` object before each model invocation.
- Produces model input for the `createAgent` harness.
- `createAgent` automatically writes resulting user/AI messages back to `Conversation State` through the checkpointer.

### Validation Rules

- The current release has exactly one behavior-compatible format policy.
- The boundary must be identifiable in implementation and tests so future custom-history work can replace or parameterize the middleware without changing public APIs.
- Public clients cannot supply or alter the format policy in this feature.
- The system prompt must be injected as a model instruction, not as a visible conversation message in `ListMessages`.

## State Transitions

```text
No Agent
  └─ CreateAgent(profile) → Agent Active + Empty Conversation State

Agent Active + Empty Conversation State
  └─ Connect/send text → Agent Active + Conversation State Appended

Agent Active + Conversation State Appended
  ├─ ListMessages → Agent Active + Same Conversation State
  ├─ Connect/send text → Agent Active + Conversation State Appended
  └─ DeleteAgent → No Agent + Conversation State Removed

No Agent after process restart
  └─ CreateAgent(profile) → Agent Active + Empty Conversation State
```

## Compatibility Invariants

- Public `Agent`, `AgentFrame`, and `Message` shapes stay unchanged.
- `session_id` remains the continuity and isolation key.
- Model/profile behavior remains copied-at-create and used for every turn.
- Same-session message generation remains serialized.
- Deepagent-only internal planning, filesystem, and subagent abstractions are not modeled because they are out of scope unless observable through existing surfaces.
