# Research: Agent Engine Context Control Foundation

## Decision: Replace deepagents with the LangChain `createAgent` harness

**Rationale**: The requested future capability is explicit control over conversation-history format and content. Deep Agents is a batteries-included harness with built-in context compression, virtual filesystem, task planning, subagent spawning, and smart defaults ([Deep Agents overview](https://docs.langchain.com/oss/javascript/deepagents/overview)). Those defaults are useful for complex autonomous tasks, but they are not part of the current game operator contract and make the history-construction point opaque.

LangChain's `createAgent` is the official next layer below Deep Agents ([LangChain overview](https://docs.langchain.com/oss/javascript/langchain/overview)): it provides the same model + harness loop (including LangGraph checkpointing and resume) without the Deep Agents batteries ([Agents / `createAgent`](https://docs.langchain.com/oss/javascript/langchain/agents)). Crucially, `createAgent` exposes middleware hooks—especially `beforeModel` ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware))—that let service code explicitly inspect and reshape the message history before every model call. This gives the service the controllable "context preparation boundary" the future feature needs while preserving automatic thread-scoped state management.

**Alternatives considered**:

- Keep `createDeepAgent` and wait for a customization hook: rejected because the current feature exists specifically because the needed history control is not available enough for future requirements.
- Reimplement full Deep Agents parity on LangChain: rejected because clarification limited compatibility to public APIs, operator flows, message history, resume, deletion, model/profile behavior, and observable configured tool behavior.
- Drop to raw chat-model execution (`model.stream()` + manual checkpoint writes): rejected because it would discard LangGraph's automatic thread persistence and force the service to reimplement history storage, resume, and namespace bookkeeping that `createAgent` already provides. The previous implementation's `checkpointer.list()` namespace-scan hack is direct evidence that manual checkpoint management is fragile.
- Drop to raw provider SDKs: rejected because existing `langchain` / provider packages already provide standardized model interfaces and provider routing with less custom code.

## Decision: Preserve public contracts and observable tool behavior only

**Rationale**: The clarified compatibility baseline excludes deepagent-only internal abstractions that are not observable through public service, desktop, or operator workflows. This keeps planning aligned with the feature's purpose: unblock future context customization while keeping current users and clients stable.

**Alternatives considered**:

- Preserve every hidden deepagent capability: rejected as unnecessary scope expansion and likely to recreate the same context-control problem.
- Preserve only text replies and ignore profile tool behavior: rejected because configured profile behavior that is visible through existing surfaces is part of the operator-observable contract.

## Decision: Keep `game.proto`, gateway, proxy, desktop, and public message contracts unchanged

**Rationale**: The spec explicitly requires unchanged interfaces and functionality. Current public surfaces already support create/get/delete agent, WebSocket connect, and `ListMessages`. The foundation replacement can be implemented behind `LLMAdapter`/agent-service internals without changing proto fields or HTTP routes.

**Alternatives considered**:

- Add a new history customization API now: rejected because the feature explicitly defers custom history behavior.
- Add versioned agent APIs for the new foundation: rejected because no public contract change is needed and versioning would increase client/test scope.

## Decision: Inject per-session system prompts via `dynamicSystemPromptMiddleware`

**Rationale**: The agent profile provides a `systemPrompt` that is fixed at agent creation time but varies per session/profile. `createAgent` accepts a static `systemPrompt` parameter ([Agents / `createAgent`](https://docs.langchain.com/oss/javascript/langchain/agents)), but rebuilding the agent on every turn to change the prompt would be wasteful and complicate state management.

`dynamicSystemPromptMiddleware` (exported from `langchain`; documented in the [Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory) "Prompt" section) is the official mechanism for per-invocation system prompts. It accepts a function `(state, config) => string | SystemMessage` and can read per-run context (via `config.context`) to inject the correct system prompt before the model call. This lets the service construct one `createAgent` harness and supply the session-specific system prompt at invocation time, matching the existing "profile snapshot copied at CreateAgent" semantics.

**Alternatives considered**:

- Rebuild `createAgent` on every turn with the session's `systemPrompt`: rejected because it adds per-turn allocation overhead and duplicates the already-created model/checkpointer instances.
- Pass the system prompt as a `SystemMessage` in the incoming message list only: rejected because it would place the system prompt at the wrong position in history (after prior turns) and would be visible to history-listing logic in an awkward way. Middleware injection keeps the system prompt as a proper model instruction while preserving chronological message order in checkpoint state.
- Use a static `systemPrompt` and ignore profile changes per session: rejected because it would violate FR-008 (profile-driven agent configuration must be honored).

## Decision: Retain session-scoped in-memory continuity for this stage

**Rationale**: The previous redesign established in-memory checkpoint behavior keyed by `sessionId`; the new feature preserves that storage boundary. Because `createAgent` is built on LangGraph, it continues to use the same `MemorySaver`/`checkpointer` model with `configurable: { thread_id: sessionId }`. The refactor should keep the same lifecycle: process-local resume works, process restart recovery remains out of scope, and delete/recreate clears the session's active conversation state.

**Alternatives considered**:

- Introduce durable persistence while replacing the foundation: rejected as out of scope and a separate user-visible reliability feature.
- Remove checkpoint/state storage and rely only on visible message lists: rejected because resume and `ListMessages` behavior are explicit success criteria.

## Decision: Use `createAgent` middleware hooks as the service-owned context preparation boundary

**Rationale**: Future customization requires one identifiable point where the service reads stored conversation messages, applies system/profile instructions, chooses which messages are included, formats them, appends the current user turn, and sends them to the model. The current implementation hides this behind `createDeepAgent`.

With `createAgent`, the official hook for this is the `beforeModel` middleware stage ([Short-term memory](https://docs.langchain.com/oss/javascript/langchain/short-term-memory), [Middleware overview](https://docs.langchain.com/oss/javascript/langchain/middleware)). A service-owned middleware receives the LangGraph `state` (including `state.messages`), can inspect or rewrite the message list, inject the current turn, apply formatting policy, and return the updated state. This is the "Context Preparation Boundary": a single, testable, replaceable function inside the agent service that controls what the model sees. Future custom-history work can replace or parameterize this middleware without changing public APIs (e.g. swapping in [`summarizationMiddleware`](https://docs.langchain.com/oss/javascript/langchain/middleware/built-in)).

**Alternatives considered**:

- Keep history formatting scattered across handler and adapter: rejected because it would make future custom-history work risky and hard to test.
- Move context preparation to gateway/desktop clients: rejected because conversation context is service-owned state and must not leak into public clients.
- Hand-roll message assembly outside `createAgent` (raw `model.stream`): rejected because it discards LangGraph's state persistence and forces the service to reimplement checkpoint/resume semantics.

## Decision: Use existing tests and large-test plan as the acceptance baseline

**Rationale**: The feature is behavior-preserving. The strongest proof is the existing acceptance surface: TypeScript adapter/handler tests and Go large tests covering create/connect/dialog/FIFO/resume/list/delete-recreate/model behavior. Unit tests should be retargeted away from deepagent internals and toward service-owned context behavior.

**Alternatives considered**:

- Only run TypeScript unit tests: rejected because service changes require large-test acceptance under the repository constitution.
- Create a new parallel testplan for the same public behavior: rejected unless existing suites cannot express the needed assertions; duplication would reduce test clarity.

## Decision: Remove `deepagents` dependency only after source imports are gone

**Rationale**: `deepagents` currently appears in `projects/game/agent/package.json`, `projects/game/agent/BUILD.bazel`, and the root catalog. Removing it should be tied to implementation proof that no source/tests still import it. Dependency changes must be synchronized through Bazel/PNPM workflows and must not manually edit lockfiles.

**Alternatives considered**:

- Leave unused `deepagents` dependency installed: rejected because the feature goal is to downgrade away from deepagent and keeping the package obscures completion.
- Remove root catalog entry first: rejected because package/build changes should drive dependency resolution, not manual catalog churn.
