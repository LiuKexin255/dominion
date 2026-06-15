# Phase 0 Research: Agent Checkpoint & Session UI Redesign

## Decision: Use one shared LangGraph.js MemorySaver for short-term agent conversation state

**Rationale**: LangGraph.js documents `MemorySaver` as the in-memory checkpointer for short-term, thread-level persistence. Compiling/invoking a graph with `configurable.thread_id` restores the thread's message state across turns. This directly replaces the manual `DialogRuntime.history` array and satisfies the stage constraint that persistence across process restart is not required.

**Alternatives considered**:
- Keep `DialogRuntime.history`: rejected because it duplicates native checkpoint capability and is explicitly disallowed.
- Use database-backed checkpointer now: rejected because the spec explicitly says this stage does not require persistence.
- Create a fresh saver per turn: rejected because it defeats checkpoint continuity, matching one of the discovered bugs.

## Decision: Use `sessionId` as checkpoint `thread_id` and `MemorySaver.deleteThread(sessionId)` on agent delete

**Rationale**: The feature requires `sessionId` as the thread identifier and clean delete/recreate. LangGraph.js `MemorySaver` exposes `deleteThread(threadId)`, which deletes all checkpoints and pending writes for a thread. This allows clean recreation without generation suffixes or private-state manipulation.

**Alternatives considered**:
- Use random thread IDs per agent creation: rejected because the user explicitly requires `sessionId` as `thread_id`.
- Use `sessionId:generation` as `thread_id`: rejected for the same reason and because it leaves old checkpoint threads behind unless separately cleaned.
- Clear messages through graph-state remove operations: rejected because deleting the whole thread is simpler and more exact for agent deletion.

## Decision: Replace DialogRuntime with lightweight Agent Metadata plus a same-session execution mutex

**Rationale**: Agent existence must not depend on a runtime object containing manual history, queue, status, inactivity cleanup, or deleted flags. Lightweight metadata (session ID, profile name, copied model, copied system prompt, creation time) is enough to rebuild/invoke the agent graph on demand. A same-session mutex only prevents concurrent model calls and preserves send ordering; it does not own conversation state.

**Alternatives considered**:
- Store compiled graph instances per session: rejected because it risks recreating runtime-like lifecycle ownership.
- Keep `DialogRuntime.queue`: rejected because it is part of the disallowed manual runtime model.
- Rely entirely on checkpointer concurrency behavior: rejected because explicit serialization gives deterministic FIFO behavior matching existing acceptance tests.

## Decision: Expose checkpoint state as `ListMessages` over a dedicated unary RPC, with `Message` as a first-class resource addressed by the native LangChain `BaseMessage.id`

**Rationale**: Clarification selected a dedicated request-response API. The method is named `ListMessages` and the response is a list of `Message` resources, each addressable as `sessions/{session_id}/agent/messages/{message_id}`. The `message_id` segment is the native LangChain `BaseMessage.id` (UUID v4) carried inside checkpoint state and assigned by the framework's messages reducer — it is surfaced directly, never re-minted, so message identity is consistent across LangGraph operations (`RemoveMessage`, reducer merge) and the REST/gRPC surface. Gateway REST exposure (`GET /api/v1/{parent=sessions/*/agent}/messages`) matches existing Create/Get/Delete agent request-response patterns and lets the desktop load prior messages independently from the WebSocket stream. Verified: `BaseMessage.id` is auto-populated by `messagesStateReducer` via `v4()` when absent, round-trips through checkpoint serialize/deserialize, and is the canonical key used by `RemoveMessage`.

**Alternatives considered**:
- Name the method `GetAgentHistory` and return anonymous entries with a synthetic `sequence` field: rejected because `Message` is a first-class addressable resource and the native message id is the correct identifier — inventing a parallel id duplicates framework-owned identity.
- Replay prior messages over the Connect WebSocket: rejected by clarification; it couples message loading to streaming lifecycle.
- Add a new server-streaming messages API: rejected because current need is page-entry hydration, not live streaming.

## Decision: Connect WebSocket on play page entry, not session detail entry

**Rationale**: Clarification selected play-entry connection. Session detail remains a lightweight metadata page and avoids opening a WebSocket for operators who only inspect session details. The play page still feels automatic because Enter Play performs the connection; send-message fallback covers edge cases.

**Alternatives considered**:
- Connect on session detail entry: rejected because it opens network connections before the operator needs chat.
- Connect only on first message: rejected because it makes the chat page appear ready while the first send must absorb all connection latency.

## Decision: Use profile model from Agent Metadata for every model invocation

**Rationale**: Current behavior copies profile model but does not use it. Per-profile model selection is a required bug fix and necessary for the play sidebar model details to reflect real behavior.

**Alternatives considered**:
- Keep process-level default model: rejected because it silently ignores profile configuration.
- Read the live profile on every turn: rejected because the existing agent creation contract copies profile data so later profile edits/deletes do not affect active agents.

## Decision: `ListMessages` returns `Message` resources reconstructed from checkpoint messages, excluding transient stream control signals

**Rationale**: The desktop needs chronological user/agent/system entries compatible with existing chat rendering. Each `Message` carries its native `message_id`, sender, type, content, and a best-effort `create_time` sourced from the introducing checkpoint's `createdAt`. `wait` frames and any other transient control signal not required for breakpoint reconnect MUST NOT be materialized — only `text`, `thinking`, and `warn` content carried by checkpoint state is returned. `thinking` content is best-effort: included only when the provider persisted reasoning into the checkpoint message, silently omitted otherwise.

**Alternatives considered**:
- Return raw LangGraph message objects: rejected because it leaks framework internals to desktop/gateway clients.
- Return final text only: rejected because it may not faithfully reconstruct what the operator saw.
- Persist `wait` frames as messages: rejected because they are flow-control signals for the live stream, not conversation content; including them would pollute breakpoint reconnect with ephemeral state.

## Decision: Large-test acceptance remains required

**Rationale**: This feature changes gRPC/HTTP service behavior and the desktop/gateway/proxy/agent flow. The constitution and `style/large_test.md` require large-test acceptance for service changes. Existing game testplan infrastructure should be extended rather than replaced.

**Alternatives considered**:
- Unit tests only: rejected because they cannot prove the real desktop/gateway/proxy/agent path works.
- Manual desktop QA only: rejected because service contract regressions require automated coverage.
