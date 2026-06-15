# Contract: Service-Owned Context Foundation

## Scope

This contract defines the internal service boundary required to support future custom conversation-history formatting while preserving current public behavior.

## Context Preparation Boundary

The agent service must have one identifiable boundary that prepares model input for each turn. This boundary is implemented as `createAgent` middleware running at the `beforeModel` stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)), plus `dynamicSystemPromptMiddleware` for system-prompt injection.

### Inputs

- `state.messages`: ordered prior conversation messages loaded from the LangGraph checkpoint by `thread_id = session_id`.
- `config.context.systemPrompt`: system prompt copied from the agent profile snapshot, injected per invocation.
- `config.context.session_id` / `config.configurable.thread_id`: conversation continuity key.
- `current_user_message`: text from the incoming user frame, appended by the handler before invocation.
- `provider_secret`: existing provider secret used when initializing the bound chat model.

### Output

- A model input message sequence equivalent to current behavior for this release.
- Streamed `ContentBlock` values of type `reasoning` and/or `text` for the existing handler.
- Updated conversation state containing the current user turn and agent response, written automatically by the `createAgent` harness through the checkpointer.

### Required Behavior

- The boundary must be owned by agent service code (a service-written `beforeModel` middleware) rather than hidden inside deepagent defaults.
- The current release must use a fixed behavior-compatible format policy.
- Future custom history work must be able to replace or parameterize the service-owned middleware without public API changes.
- The boundary must not accept custom history policy from public clients in this feature.
- The system prompt must be injected via `dynamicSystemPromptMiddleware` reading per-invocation context, so that the same `createAgent` harness can serve multiple sessions with different profile snapshots.

## Conversation State Rules

- Conversation state is keyed by `session_id` and stored in the shared `MemorySaver` checkpointer via `createAgent`'s LangGraph state.
- `createAgent` reads/writes checkpoint state automatically; the service-owned `beforeModel` middleware observes and reshapes `state.messages` before each model call.
- Context reads and writes for a session are serialized by the existing same-session mutex at the handler level.
- `ListMessages` reads the conversation state through the standard `createAgent` graph state API (`agent.getState({ configurable: { thread_id: sessionId } })`), not through deepagent-specific namespace scanning.
- Delete agent removes the session's context state before the session can be recreated cleanly.
- Process restart may lose state, matching the current in-memory scope.

## Tool / Profile Behavior Rules

- Profile model selection remains per-turn input to the model execution path.
- Observable configured tool/skill behavior must remain equivalent where existing public surfaces expose it.
- Hidden deepagent-only tool harness behavior is not required unless it affects an existing public acceptance flow.

## Test Contract

Unit tests must prove:

1. Prior messages and the current user message are assembled through service-owned `createAgent` middleware (`beforeModel` hook and `dynamicSystemPromptMiddleware`).
2. The profile model is passed to the `createAgent` model initialization path and used for every turn.
3. The session-specific system prompt is injected via `dynamicSystemPromptMiddleware` reading per-invocation context.
4. Model response blocks are converted to existing `ContentBlock` outputs.
5. Context state updates are session-scoped and ordered through the LangGraph checkpointer.
6. No production source imports `deepagents` after the refactor.

Large tests must prove:

1. The public create/connect/chat/history/resume/delete/recreate flow remains unchanged.
2. Message order and visible content remain compatible.
3. Same-session rapid sends remain FIFO.
