# Research: Agent Adapter Decoupling and LangChain Foundation

## Decision: Replace deepagents with the LangChain `createAgent` harness

**Rationale**: The requested future capability is explicit control over conversation-history format and content. Deep Agents is a batteries-included harness with built-in context compression, virtual filesystem, task planning, subagent spawning, and smart defaults ([Deep Agents overview](https://docs.langchain.com/oss/javascript/deepagents/overview)). Those defaults make the history-construction point opaque.

LangChain's `createAgent` is the official next layer below Deep Agents ([LangChain overview](https://docs.langchain.com/oss/javascript/langchain/overview)): it provides the same model + harness loop (including LangGraph checkpointing and resume) without the Deep Agents batteries ([Agents / `createAgent`](https://docs.langchain.com/oss/javascript/langchain/agents)). Crucially, `createAgent` exposes middleware hooks — especially `beforeModel` ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)) — that let service code explicitly inspect and reshape the message history before every model call.

**Alternatives considered**:

- Keep `createDeepAgent` and wait for a customization hook: rejected because the current feature exists specifically because the needed history control is not available.
- Drop to raw chat-model execution (`model.stream()` + manual checkpoint writes): rejected because it would discard LangGraph's automatic thread persistence and force the service to reimplement history storage and resume.
- Drop to raw provider SDKs: rejected because existing `langchain` / provider packages already provide standardized model interfaces with less custom code.

## Decision: Separate SessionAgent from AgentAdapter

**Rationale**: The current architecture conflates the session-level conversation agent with the execution engine. In the current code, `createDeepAgent` is rebuilt on every turn inside `generateTurn`, and session metadata is stored separately in a `Map<sessionId, AgentMetadata>`. This makes the agent instance a transient artifact rather than a managed resource, and hides the fact that conversation history (in the checkpointer) is independent of the execution instance.

The new architecture makes this explicit:

- **SessionAgent**: the logical agent bound 1:1 to a session. Owns conversation history (MemorySaver, `thread_id = sessionId`). Lifecycle = session lifecycle. Does not require explicit creation. At most one active AgentAdapter is bound at any time.
- **AgentAdapter**: the profile-specific execution processor. Built from an agent profile using `createAgent`. Stateless — does not own history; reads/writes through the shared MemorySaver. Created on-demand when a user sends a text frame specifying a profile. Can be swapped mid-connection. Cleaned up asynchronously on disconnect.

This separation is grounded in LangChain's design: `createAgent` returns a compiled graph that is stateless — all conversation state is externalized to the checkpointer, keyed by `thread_id` ([Checkpointers](https://docs.langchain.com/oss/javascript/langchain/agents), [MemorySaver source](https://github.com/langchain-ai/langgraphjs/blob/main/libs/checkpoint/src/memory.ts#L71-L79)). The compiled graph can be invoked with different `thread_id` values, with each thread maintaining its own checkpoint state. `BaseCheckpointSaver` is itself stateless ([base.ts source](https://github.com/langchain-ai/langgraphjs/blob/main/libs/checkpoint/src/base.ts#L14-L58)).

**Alternatives considered**:

- Share one `createAgent` instance across all sessions (cross-session pool keyed by profile): rejected because it adds cache-invalidation complexity and the user explicitly wants per-session isolation.
- Pre-create all profile instances per session: rejected because it wastes resources for unused profiles and adds a daemon process the user does not want.
- Keep per-turn instance construction (current pattern): rejected because it wastes per-turn graph compilation and contradicts the adapter concept.

## Decision: Create adapter on-demand at message time, not at connection time

**Rationale**: The user sends a text frame with `agent_profile_name`. The agent service checks whether the session already has an adapter for that profile; if not, it creates one. If the session has an adapter for a different profile, it unbinds the old one (synchronously) and creates the new one. This lazy approach means:

- No daemon process needed.
- No pre-creation of unused profiles.
- Profile configuration is always fresh (fetched from prompt service at creation time).
- The connect WebSocket endpoint does not need to know the profile at connection time — it's in the frames.

**Alternatives considered**:

- Create adapter at WebSocket connection time (profile in URL): rejected because the user wants per-message profile selection without reconnection.
- Pre-create all profile adapters for a session: rejected as resource-wasteful and unnecessary.

## Decision: Profile name in AgentFrame, not in URL or gRPC metadata

**Rationale**: The user sends `agent_profile_name` as an optional field in each AgentFrame text message. This enables:

- Per-message profile selection (switch profiles without reconnecting).
- Response attribution (agent frames identify which profile produced them).
- No need for URL query parameters or gRPC metadata headers.
- Control frames (status, echo, wait) do not carry the field.

**Alternatives considered**:

- Profile in WebSocket URL query parameter: rejected because switching profiles would require reconnection.
- Profile in gRPC metadata header: rejected because it couples connection establishment to profile selection and adds a non-standard transport mechanism.

## Decision: Single WebSocket connection per session with kick-old behavior

**Rationale**: Multiple concurrent WebSocket connections to the same session would cause frame routing ambiguity (which connection receives the response?). The game model is single-operator-per-session. When a second connection opens, the first is forcibly closed.

**Alternatives considered**:

- Reject second connection with an error: rejected because it creates poor UX when switching windows or recovering from a network blip.
- Allow multiple connections sharing one SessionAgent: rejected because response frame routing becomes ambiguous.

## Decision: Synchronous unbind + async cleanup on disconnect

**Rationale**: When a WebSocket connection drops, the adapter must be unbound from the SessionAgent so the session is immediately available for new connections. However, adapter resource cleanup (garbage collecting the compiled graph, releasing model connections) can be slow. By splitting the operation:

1. Synchronous: unbind adapter from SessionAgent (fast — just remove the reference).
2. Asynchronous: clean up adapter resources (slow — let it happen in background).

A new connection arriving immediately after disconnect can create and bind a new adapter without waiting for the old one's cleanup.

**Alternatives considered**:

- Full synchronous cleanup on disconnect: rejected because it could delay new connection establishment.
- No cleanup at all (rely on GC): rejected because it could leak resources under high churn.

## Decision: Remove CreateAgent and DeleteAgent RPCs

**Rationale**: With adapters created on-demand at message time and SessionAgent lifecycle tied to the session, explicit agent lifecycle operations are unnecessary. The user confirmed this simplification during specification:

- `CreateAgent` is replaced by implicit adapter creation when the first message with a profile is processed.
- `DeleteAgent` is replaced by session deletion cascading to clean up conversation history.
- `GetAgent` is retained but returns current adapter state (active profile, connection status) rather than created-agent metadata.

This reduces the client-side state management burden: desktop/frontend no longer need to track agent lifecycle, call create before connect, or call delete before session deletion.

**Alternatives considered**:

- Keep CreateAgent as a no-op for backward compatibility: rejected because the user explicitly wants API simplification and the old semantics no longer apply.
- Keep DeleteAgent for explicit history clearing: rejected because session deletion already cascades.

## Decision: Change connect and list-messages paths to session level

**Rationale**: The old paths (`/sessions/{id}/agent/connect`, `/sessions/{id}/agent/messages`) imply the agent is a sub-resource of the session that must be explicitly managed. The new paths (`/sessions/{id}/connect`, `/sessions/{id}/messages`) reflect the fact that the logical agent is implicit — it exists when the session exists.

**Alternatives considered**:

- Keep old paths: rejected because they imply a resource model that no longer exists (explicit agent lifecycle).

## Decision: Use `createAgent` middleware hooks as the service-owned context preparation boundary

**Rationale**: Future customization requires one identifiable point where the service reads stored conversation messages, applies system/profile instructions, chooses which messages are included, formats them, appends the current user turn, and sends them to the model. With `createAgent`, the official hook for this is the `beforeModel` middleware stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)).

A service-owned middleware receives the LangGraph `state` (including `state.messages`), can inspect or rewrite the message list, inject the current turn, apply formatting policy, and return the updated state. Future custom-history work can replace or parameterize this middleware without changing public APIs (e.g., swapping in [`summarizationMiddleware`](https://docs.langchain.com/oss/javascript/langchain/middleware/built-in)).

**Alternatives considered**:

- Keep history formatting scattered across handler and adapter: rejected because it makes future custom-history work risky.
- Move context preparation to gateway/desktop clients: rejected because conversation context is service-owned state.

## Decision: Use static `systemPrompt` parameter in `createAgent`

**Rationale**: Each AgentAdapter is built for a specific profile and is stateless. The system prompt is a construction-time constant — it does not change during the adapter's lifetime. When the user switches profiles, a new adapter is created with the new profile's system prompt. Therefore, `createAgent`'s built-in `systemPrompt` parameter is sufficient; no dynamic injection mechanism is needed.

This simplifies the middleware stack: only the service-owned `beforeModel` middleware is included, without `dynamicSystemPromptMiddleware`. The `runtime.context` mechanism is not needed for system prompt injection in this release.

**Alternatives considered**:

- Use `dynamicSystemPromptMiddleware` for per-invocation system prompts: rejected because the system prompt is fixed per adapter (one profile = one prompt). Dynamic injection adds complexity without benefit when each adapter is rebuilt on profile switch.
- Bind `systemPrompt` outside `createAgent` (manual system message injection): rejected because `createAgent`'s built-in `systemPrompt` parameter handles this cleanly at the harness level.

## Decision: Retain session-scoped in-memory continuity

**Rationale**: The previous redesign established in-memory checkpoint behavior keyed by `sessionId`. This feature preserves that storage boundary. `createAgent` continues to use `MemorySaver`/checkpointer with `configurable: { thread_id: sessionId }`. Process-local resume works; process restart recovery remains out of scope.

**Alternatives considered**:

- Introduce durable persistence: rejected as out of scope.
- Remove checkpoint/state storage: rejected because resume and `ListMessages` behavior are explicit success criteria.

## Decision: Shared conversation history across profiles within a session

**Rationale**: The user confirmed that conversation history belongs to the session (SessionAgent), not to individual profiles. When a user switches from profile A to profile B within the same session, profile B's adapter reads the same checkpoint (thread_id = sessionId), which includes messages exchanged with profile A. This creates a multi-agent conversation model where different profiles take turns in the same linear history.

This is technically sound because:
- `MemorySaver` isolates by `thread_id` ([MemorySaver source](https://github.com/langchain-ai/langgraphjs/blob/main/libs/checkpoint/src/memory.ts#L71-L79)).
- The compiled `createAgent` graph is stateless — all state lives in the checkpointer.
- Thread isolation regression tests confirm middleware state does not leak across `thread_id` boundaries ([PR #10693](https://github.com/langchain-ai/langchainjs/pull/10693)).

The known cross-thread contamination concern ([Issue #2040](https://github.com/langchain-ai/langgraphjs/issues/2040)) was traced to LLM provider prompt caching, not the checkpointer. The same-session serialization mutex prevents concurrent invocations on the same thread.

**Alternatives considered**:

- Separate history per (session, profile) pair: rejected because the user wants one linear conversation per session and the model would lose context from other profiles.

## Decision: Remove `deepagents` dependency after source imports are gone

**Rationale**: `deepagents` currently appears in `projects/game/agent/package.json`, `projects/game/agent/BUILD.bazel`, and the root catalog. Removing it should be tied to implementation proof that no source/tests still import it. Dependency changes must be synchronized through Bazel/PNPM workflows.

**Alternatives considered**:

- Leave unused `deepagents` installed: rejected because the feature goal is to downgrade away from deepagent.
