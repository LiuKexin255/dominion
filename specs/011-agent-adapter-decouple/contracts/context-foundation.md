# Contract: Service-Owned Context Foundation

## Scope

This contract defines the internal service boundary required to support future custom conversation-history formatting while preserving current public behavior.

## Context Preparation Boundary

The agent service must have one identifiable boundary that prepares model input for each turn. This boundary is implemented as a `createAgent` middleware running at the `beforeModel` stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)).

### Inputs

- `state.messages`: ordered prior conversation messages loaded from the LangGraph checkpoint by `thread_id = session_id`.
- `config.configurable.thread_id`: conversation continuity key.
- `systemPrompt`: the profile's system prompt, passed as a static construction parameter to `createAgent`. Each adapter is built for one profile, so the prompt is a compile-time constant — no dynamic injection is needed.
- `current_user_message`: text from the incoming user frame, appended by the handler before invocation.

### Output

- A model input message sequence equivalent to current behavior for this release.
- Streamed `ContentBlock` values of type `reasoning` and/or `text` for the existing handler.
- Updated conversation state containing the current user turn and agent response, written automatically by the `createAgent` harness through the checkpointer.

### Required Behavior

- The boundary must be owned by agent service code (a service-written `beforeModel` middleware) rather than hidden inside deepagent defaults.
- The current release must use a fixed behavior-compatible format policy.
- Future custom history work must be able to replace or parameterize the service-owned middleware without public API changes.
- The boundary must not accept custom history policy from public clients in this feature.
- The system prompt is injected via `createAgent`'s built-in `systemPrompt` parameter at adapter construction time. It must appear as a model instruction, not as a visible conversation message in `ListMessages`.

## Conversation State Rules

- Conversation state is keyed by `session_id` and stored in the shared `MemorySaver` checkpointer via `createAgent`'s LangGraph state.
- `createAgent` reads/writes checkpoint state automatically; the service-owned `beforeModel` middleware observes and reshapes `state.messages` before each model call.
- Context reads and writes for a session are serialized by the existing same-session mutex at the handler level.
- `ListMessages` reads the conversation state through the standard `createAgent` graph state API.
- Conversation state is shared across all profile adapters within the same session — switching profiles does not create a new history thread.
- Session deletion removes the session's conversation state.
- Process restart may lose state, matching the current in-memory scope.

## Adapter Lifecycle Rules

- An AgentAdapter is created on-demand when a user sends a text frame specifying a profile name and no adapter for that profile is currently bound.
- If a different profile's adapter is bound, the old adapter is synchronously unbound before the new one is created.
- On disconnect, the adapter stays bound to the SessionAgent and is reused on reconnect. Only profile switching triggers cleanup (unbind old + create new).
- Only one adapter may be bound to a SessionAgent at any time.
- Adapter creation requires a valid profile from the prompt service; invalid profiles produce a warning frame and do not affect the current adapter.

## Tool / Profile Behavior Rules

- Profile model selection is used at adapter construction time and fixed for the adapter's lifetime.
- Observable configured tool/skill behavior must remain equivalent where existing public surfaces expose it.
- Hidden deepagent-only tool harness behavior is not required.

## Test Contract

Unit tests must prove:

1. Prior messages and the current user message are assembled through the service-owned `beforeModel` middleware.
2. The profile model is passed to `createAgent` at adapter construction and used for every turn.
3. The system prompt is injected via `createAgent`'s `systemPrompt` parameter (static, not dynamic).
4. Model response blocks are converted to existing `ContentBlock` outputs.
5. Context state updates are session-scoped and ordered through the LangGraph checkpointer.
6. No production source imports `deepagents` after the refactor.
7. Adapter switching preserves shared conversation history (same `thread_id`).

Large tests must prove:

1. The public connect/chat/switch-profile/history flow remains functional.
2. Message order and visible content remain compatible.
3. Same-session rapid sends remain FIFO.
