# Contract: Checkpointed Agent Service Lifecycle

## CreateAgent

Creates lightweight agent metadata from the requested profile.

**Required behavior**:
- Fetch profile at creation time.
- Copy profile name, model, and system prompt into metadata.
- Do not create a `DialogRuntime` or manual history object.
- Return an Agent including `agentProfileName`.

## GetAgent

Returns agent metadata for a session.

**Required behavior**:
- Return not-found when metadata is absent.
- Return profile name, model-derived metadata when present.
- Do not depend on a live runtime instance or WebSocket connection.

## DeleteAgent

Deletes metadata and checkpoint thread state.

**Required behavior**:
- Remove metadata for `sessionId`.
- Delete all checkpoints and pending writes for `thread_id=sessionId`.
- Be idempotent when metadata/checkpoints are absent.
- Ensure subsequent CreateAgent for the same session starts with empty history.
- Acquire the same-session execution mutex before deleting; wait for any in-flight turn to complete before removing metadata and checkpoint state. This prevents deletion-during-invocation races.

## Connect

Streams frames for interactive play.

**Required behavior**:
- Use `sessionId` as the checkpoint `thread_id` for every turn.
- Use one shared in-memory checkpointer for all sessions.
- Use the copied profile model for model calls.
- Serialize same-session turns in send order.
- Emit thinking/text/warn/wait frames with the existing visible semantics.
- Do not maintain manual conversation history or manual runtime cleanup.

## ListMessages

Reads checkpoint state and returns normalized display messages.

**Required behavior**:
- Read latest checkpoint state for `thread_id=sessionId`.
- Reconstruct messages in chronological order from the checkpoint's message channel.
- Surface each message's native LangChain `BaseMessage.id` as `message_id` — do not mint a parallel identifier.
- Exclude `wait` frames and any transient stream control signal not required for breakpoint reconnect.
- Include `thinking` content only when the provider persisted it into the checkpoint message.
- Return empty messages for an existing agent with no turns.
- Return not-found for missing agent metadata.
