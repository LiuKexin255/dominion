# Implementation Plan: Agent Adapter Decoupling and LangChain Foundation

**Branch**: `011-agent-adapter-decouple` | **Date**: 2026-06-15 | **Spec**: `specs/011-agent-adapter-decouple/spec.md`

**Input**: Feature specification from `specs/011-agent-adapter-decouple/spec.md`

## Summary

Refactor the game agent service to decouple the session-level conversation agent (SessionAgent) from the profile-specific execution adapter (AgentAdapter), replace the deepagent harness with LangChain's `createAgent`, simplify the agent service API by removing explicit agent lifecycle operations, and add multi-profile support within a single session. SessionAgent owns conversation history (MemorySaver, `thread_id = sessionId`) and lives for the session's lifetime. AgentAdapter is a stateless processor built on-demand from an agent profile when a user sends a text frame specifying a profile name. Users connect directly to a session via WebSocket, select profiles per-message, and switch adapters mid-conversation — all without any Create/Delete agent step.

The API surface changes: CreateAgent and DeleteAgent RPCs are removed from both AgentService and ProxyService; the WebSocket connect path moves from `/sessions/{id}/agent/connect` to `/sessions/{id}/connect`; ListMessages moves to `/sessions/{id}/messages`; AgentFrame gains an optional `agent_profile_name` field; and GetAgent is retained with modified semantics (returns current adapter state). The deepagent dependency is removed once no source imports it. A service-owned `beforeModel` middleware serves as the future context-customization boundary.

## Technical Context

**Language/Version**: TypeScript 6.x/ES2020 CommonJS for `projects/game/agent`; Go for gateway, proxy, desktop clients, and large tests; protobuf/gRPC contracts in `projects/game/game.proto`.

**Primary Dependencies**: `langchain` (provides `createAgent`), `@langchain/core` (messages, testing), `@langchain/langgraph` (`MemorySaver`), `@langchain/openai` and `@langchain/anthropic` (provider packages), existing `@grpc/grpc-js` / `@grpc/proto-loader`. `deepagents` is removed from the agent service dependency surface once no source or test imports it.

**Storage**: In-memory only. Conversation history remains in the shared `MemorySaver` checkpointer keyed by `thread_id = sessionId`. AgentAdapter instances are held in handler-owned session maps and cleaned up on disconnect. No durable storage, cross-process recovery, or data migration is in scope.

**Testing**: Vitest-under-Bazel for TypeScript agent service (adapter, handler, middleware, fake-LLM behavior); Go large tests through `projects/game/testplan/system_test.yaml` using the `testplan` skill; final repository validation via `bazel build //...` and `bazel test //...`.

**Target Platform**: Linux-hosted game backend services and Wails desktop client using the existing gateway/proxy/agent path.

**Project Type**: Multi-service system with a TypeScript gRPC agent service, Go gateway/proxy/desktop services, protobuf API surface, Wails desktop app, Svelte frontend, and YAML-orchestrated large tests.

**Performance Goals**: Normal message history remains visible within 2 seconds when entering play; same-session rapid sends preserve FIFO order in 100% of tested cases; adapter switching does not add measurable latency beyond normal model response time; adapter unbinding on disconnect completes synchronously without blocking new connections.

**Constraints**: Use Bazel wrappers for all builds/tests and Bazel-managed PNPM for package operations. Preserve SessionService and PromptService public contracts. Do not introduce custom history controls in this feature. Preserve `sessionId` as the conversation continuity key (`thread_id`). Preserve in-memory-only lifecycle semantics. Keep dependency versions centralized in root `pnpm-workspace.yaml`. The service-owned context-preparation boundary must be a `beforeModel` middleware identifiable and replaceable without public API changes. AgentFrame `agent_profile_name` is an optional field (field number 21). Only one WebSocket connection per session at any time.

**Scale/Scope**: Source changes span `projects/game/game.proto` (proto-level RPC removal, path changes, new field), `projects/game/agent/src/` (complete handler rewrite, adapter refactor, new middleware, server simplification, test rewrites), `projects/game/proxy/` (handler and client changes), `projects/game/gateway/` (route changes), `projects/game/desktop/` (Go API client, WS client, Wails app, frontend Svelte), `projects/game/testplan/` (helper and test rewrites), `projects/game/agent/package.json` and `BUILD.bazel` (deepagents removal), and root dependency metadata.

**References**: Official LangChain documentation underpins every design decision. See the [References](#references) appendix for the full linked index.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Plan is based on `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`, the active spec, and existing `projects/game/agent` / `projects/game/testplan` patterns. Implementation tasks must require executors to re-read these before code changes.
- **Bazel Integrity**: PASS. Planned validation uses Bazel-managed commands: `bazel test //projects/game/agent:lib_test`, `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/agent ...` only if package metadata changes require PNPM operations, `bazel run //:gazelle` if BUILD generation/sync is required, `bazel mod tidy` if dependency graph changes require it, `bazel build //...`, and `bazel test //...`.
- **Generated Files & Dependencies**: PASS. Proto changes go through Bazel proto rules and `ts_proto_library`; no generated proto/grpc files are committed. If `deepagents` becomes unused, remove it from the agent package and Bazel runtime deps through repository PNPM/Bazel workflow.
- **Testing Strategy**: PASS. Adapter and handler unit tests are updated before/with the refactor to prove explicit context construction, adapter lifecycle, profile switching, connection exclusivity, and model selection. Service-level behavior requires large-test acceptance through existing game testplans (rewritten for the new API surface).
- **Behavioral Acceptance**: PASS. Acceptance validates real surfaces: gateway HTTP get-agent, WebSocket connect/send frames with profile selection, desktop-equivalent play flow, and fake-agent test deployment.
- **Review Scope**: PASS. Review must include TypeScript service refactor, Go proxy/gateway/desktop changes, proto contract review, dependency/build metadata, test-code review, and style review.
- **Repository Verification**: PASS. Final validation includes targeted agent tests, affected large-test plan, `bazel build //...`, and `bazel test //...` unless environment blockers are documented.
- **Testplan Execution**: PASS. Agent service behavior changes require running the game testplan via `testplan` skill or documenting deployment/infrastructure blockers and residual risk.
- **Test Impact Assessment**: PASS. Affected tests are listed in this plan: all TypeScript agent tests (`llm.test.ts`, `handler.test.ts`, `fake-llm.test.ts`, `spike.test.ts`, new `context-middleware.test.ts`), proxy tests (`handler_test.go`, `connect_agenter_test.go`), gateway tests (`main_test.go`), desktop tests (`app_test.go`, `client_test.go`, `websocket_test.go`), and all large tests (`agent_lifecycle_test.go`, `agent_dialog_test.go`, `agent_checkpoint_test.go`, `helpers_test.go`, `system_test.yaml`).
- **Change Classification**: PASS. Changes are classified below; modifications to existing code are explicit refactorings with declared scope, goal, and preserved invariants.
- **Citation Links**: PASS. External sources (LangChain documentation, LangGraph source, GitHub issues) are cited with URLs in the [References](#references) appendix.

## Project Structure

### Documentation (this feature)

```text
specs/011-agent-adapter-decouple/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── agent-service-api.md
│   └── context-foundation.md
└── tasks.md              # Created by /speckit.tasks, not /speckit.plan
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                              # Remove Create/Delete RPCs; add agent_profile_name field; change paths
├── agent/
│   ├── package.json                        # Remove deepagents dependency
│   ├── BUILD.bazel                         # Remove deepagents npm/runtime deps
│   └── src/
│       ├── llm.ts                          # Evolve LLMAdapter → AgentAdapter: createAgent + static systemPrompt + beforeModel
│       ├── context-middleware.ts           # NEW: service-owned beforeModel middleware (context preparation boundary)
│       ├── adapter-manager.ts              # NEW: SessionAgent + AgentAdapter lifecycle management (create/switch/unbind/cleanup)
│       ├── fake-llm.ts                     # Align deterministic adapter with new AgentAdapter contract
│       ├── server.ts                       # Simplify: remove StateGraph, adjust wiring for new handler
│       ├── handler.ts                      # Rewrite: Connect with profile switching + connection exclusivity; GetAgent returns adapter state; remove Create/Delete
│       ├── prompt-client.ts                # Unchanged (fetches profiles from prompt service)
│       ├── llm.test.ts                     # Rewrite: AgentAdapter construction, middleware, profile switching
│       ├── context-middleware.test.ts      # NEW: beforeModel middleware behavior
│       ├── adapter-manager.test.ts         # NEW: adapter lifecycle (create/switch/unbind/cleanup)
│       ├── fake-llm.test.ts                # Update for new AgentAdapter contract
│       ├── handler.test.ts                 # Rewrite: Connect flow, profile switching, connection exclusivity, GetAgent, ListMessages
│       └── spike.test.ts                   # Remove V1 (createDeepAgent); keep V2-V4
├── proxy/
│   ├── handler/
│   │   ├── handler.go                      # Remove CreateAgent/DeleteAgent handlers; keep GetAgent/ListMessages/ConnectAgent
│   │   └── handler_test.go                 # Remove Create/Delete test cases
│   ├── runtime/agentclient/
│   │   ├── client.go                       # Remove CreateAgent/DeleteAgent methods from Client interface
│   │   └── client_test.go (if exists)      # Update interface mocks
│   └── service/
│       ├── connect_agenter.go              # Adapt for new connect path and protocol
│       └── connect_agenter_test.go         # Update for new protocol
├── gateway/
│   └── cmd/
│       ├── main.go                         # WS path → /sessions/{id}/connect; remove Create/Delete gRPC-gateway routes; ListMessages path change
│       └── main_test.go                    # Update path assertions
├── desktop/
│   ├── app.go                              # Remove CreateAgent/DeleteAgent Go methods; ConnectAgent no longer requires prior create; SendAgentText carries profile
│   ├── app_test.go                         # Remove create/delete tests; update connect tests
│   ├── internal/api/
│   │   ├── client.go                       # Remove CreateAgent/DeleteAgent HTTP methods; ListMessages URL change
│   │   ├── client_test.go                  # Remove create/delete test cases
│   │   ├── websocket.go                    # WS URL change to /sessions/{id}/connect
│   │   └── websocket_test.go               # Update URL assertions
│   ├── view_model.go                       # Update Agent view model for adapter state
│   └── frontend/src/
│       ├── api.ts                          # Remove createAgent/deleteAgent bindings; connectAgent signature change; sendAgentText carries profile
│       ├── App.svelte                      # Remove create/delete agent flows; add profile selection UI
│       ├── components/SessionDetail.svelte # Remove create/delete agent buttons
│       ├── components/AgentSidebar.svelte  # Show active profile
│       └── components/ChatView.svelte      # Carry profile in sent messages; show source profile on responses
└── testplan/
    ├── helpers_test.go                     # Rewrite: remove createAgent/deleteAgent helpers; WS path change; frame carries profile
    ├── agent_lifecycle_test.go             # Rewrite: connect-with-profile, profile switching, connection exclusivity
    ├── agent_dialog_test.go                # Rewrite: connect → send text (with profile) → receive frames
    ├── agent_checkpoint_test.go            # Rewrite: cross-connection history, profile switch history persistence, ListMessages new path
    ├── system_test.yaml                    # Update affected suites
    └── BUILD.bazel                         # Update only if source set changes
```

**Structure Decision**: Keep the existing multi-service layout. The feature touches all agent-facing layers (proto → agent service → proxy → gateway → desktop → tests) because it changes the public API surface. SessionService and PromptService directories are not modified.

## Change Classification and Refactoring Scope

### Proto (game.proto)

- **Modify / Refactor**: Remove `CreateAgent` and `DeleteAgent` RPCs from `AgentService` (lines 106-107, 112) and `ProxyService` (lines 59-66, 78-84). Remove corresponding request messages (`AgentCreateRequest`, `AgentDeleteRequest`, `CreateAgentRequest`, `DeleteAgentRequest`). Refactoring goal: eliminate explicit agent lifecycle from the API since adapters are created/destroyed implicitly during Connect. Preserved invariants: SessionService, PromptService, remaining AgentService/ProxyService RPCs unchanged.
- **Modify**: `ListMessages` HTTP annotation path changes from `/api/v1/{parent=sessions/*/agent}/messages` to `/api/v1/{parent=sessions/*}/messages`. Refactoring goal: message listing moves from agent-level to session-level since the logical agent is implicit per session. Preserved invariants: ListMessagesRequest and ListMessagesResponse message structures unchanged.
- **Modify**: `ConnectAgent` WebSocket handling in the gateway changes from `/sessions/{id}/agent/connect` to `/sessions/{id}/connect`. No proto annotation change needed (ConnectAgent has no HTTP annotation; the path is handled by gateway code). Preserved invariants: stream AgentFrame message type unchanged.
- **Modify**: `AgentFrame` gains `string agent_profile_name = 21;` (optional). Refactoring goal: enable per-message profile selection and response attribution. Preserved invariants: all existing AgentFrame fields and payload oneof unchanged.
- **Modify**: `Agent` message semantics change — `agent_profile_name` now reflects the currently active profile (or empty if not connected). `create_time` reflects when the session was created (SessionAgent lifecycle = session lifecycle). Message structure unchanged.

### Agent Service (TypeScript)

- **New**: `context-middleware.ts` — service-owned `beforeModel` middleware implementing the context-preparation boundary. In this release it is behavior-compatible (identity-like beyond system-prompt injection); future custom-history work can replace or parameterize it.
- **New**: `adapter-manager.ts` — manages SessionAgent state and AgentAdapter lifecycle: on-demand creation, profile switching (unbind old + create new), synchronous unbind on disconnect, async cleanup, connection exclusivity (kick old connection).
- **Modify / Refactor**: `llm.ts` — `LLMAdapter` evolves into `AgentAdapter`. Replace `createDeepAgent` with `createAgent` from `langchain`, configured with a static `systemPrompt` parameter (from the profile) and the service-owned `beforeModel` middleware. The adapter instance is created once per adapter binding (not per turn). `generateTurn` signature adapts to accept `thread_id`, `system_prompt`, and `user_message` from the adapter context. Preserved invariants: streams `ContentBlock` values; provider model selection; provider error propagation.
- **Modify / Refactor**: `handler.ts` — complete rewrite. Remove `CreateAgent` and `DeleteAgent` handlers. `GetAgent` returns current adapter state (active profile, connection status). `Connect` reads `agent_profile_name` from text frames, delegates to `AdapterManager` for adapter lifecycle, enforces same-session mutex (FIFO), enforces single-connection-per-session (kick old). `ListMessages` reads from `MemorySaver` checkpoint via standard `createAgent` graph state API. Preserved invariants: AgentFrame frame behavior (thinking/text/wait/warn/status/echo), frame sequencing, same-session serialization.
- **Modify / Refactor**: `server.ts` — remove redundant external `StateGraph(MessagesAnnotation).compile({checkpointer})`. Wire the shared `MemorySaver` to the handler. Preserved invariants: startup, TLS detection, prompt client wiring, fake adapter override, gRPC registration, OTel/logging behavior.
- **Modify / Refactor**: `fake-llm.ts` — align with new `AgentAdapter` contract. Preserved invariants: no network calls; deterministic output.
- **Delete**: `spike.test.ts` V1 tests (`createDeepAgent` import checks). Keep V2 (`initChatModel`), V3 (`fakeModel`), V4 (`contentBlocks`/streaming).
- **Modify**: `package.json` and `BUILD.bazel` — remove `deepagents` dependency.

### Proxy (Go)

- **Modify / Refactor**: `handler/handler.go` — remove `CreateAgent` and `DeleteAgent` handler methods. Keep `GetAgent`, `ListMessages`, `ConnectAgent`. Refactoring goal: align proxy surface with the simplified agent service API. Preserved invariants: `GetAgent` and `ListMessages` forwarding behavior; `ConnectAgent` stream bridging.
- **Modify / Refactor**: `runtime/agentclient/client.go` — remove `CreateAgent` and `DeleteAgent` from the `Client` interface and `AgentClient` implementation. Preserved invariants: `GetAgent`, `ListMessages`, `Connect` methods.
- **Modify / Refactor**: `service/connect_agenter.go` — adapt for the new connect protocol (no first-frame owner lookup needed for agent creation; profile is in frames). Preserved invariants: stream bridging between proxy and agent service.

### Gateway (Go)

- **Modify / Refactor**: `cmd/main.go` — WebSocket path changes from `/sessions/{id}/agent/connect` to `/sessions/{id}/connect`. Remove gRPC-gateway mux handlers for `POST /sessions/{id}/agent` (CreateAgent) and `DELETE /sessions/{id}/agent` (DeleteAgent). Update `isWebSocketConnectPath` matching. Preserved invariants: TLS, env header, rate limiting, CORS behavior.

### Desktop Go (Wails)

- **Modify / Refactor**: `app.go` — remove `CreateAgent`, `CreateAgentWithProfile`, `DeleteAgent` Go methods. `ConnectAgent` no longer requires prior agent creation. `SendAgentText` includes `agent_profile_name` in the frame. Preserved invariants: WebSocket lifecycle management, frame sending/receiving.
- **Modify / Refactor**: `internal/api/client.go` — remove `CreateAgent`, `CreateAgentWithProfile`, `DeleteAgent` HTTP methods. Change `ListMessages` URL. Preserved invariants: profile/skill management methods, session methods.
- **Modify / Refactor**: `internal/api/websocket.go` — change WS URL from `/sessions/{id}/agent/connect` to `/sessions/{id}/connect`. Preserved invariants: send/receive/close logic.
- **Modify**: `view_model.go` — update Agent view model to reflect adapter state semantics.

### Desktop Frontend (Svelte)

- **Modify / Refactor**: `api.ts` — remove `createAgent`, `deleteAgent`, `createAgentWithProfile` bindings. Update `connectAgent` signature (no prior create needed). `sendAgentText` carries `agent_profile_name`. Preserved invariants: profile/skill management bindings.
- **Modify / Refactor**: `App.svelte` — remove `handleCreateAgent`, `handleDeleteAgent`, `handleAutoGetAgent` flows. Connect flow simplified. Add profile selector for message sending. Preserved invariants: session management, profile management, chat display.
- **Modify**: `SessionDetail.svelte` — remove create/delete agent buttons and callbacks.
- **Modify**: `AgentSidebar.svelte` — display active profile name and connection status.
- **Modify**: `ChatView.svelte` — include `agent_profile_name` in sent frames; display source profile on agent responses.

### Large Tests

- **Modify / Refactor**: `helpers_test.go` — remove `createAgent`, `getAgent`, `deleteAgent`, `createAgentWithProfile`, `createAgentWithBody` helpers. Change `connectAgentWS` path to `/sessions/{id}/connect`. Add `agent_profile_name` to frame construction helpers. Preserved invariants: session helpers, profile/skill helpers, WS read/write helpers.
- **Rewrite**: `agent_lifecycle_test.go` — remove all create/delete/cascade tests. Add tests for: connect-with-profile (no prior create), profile switching mid-connection, connection exclusivity (kick old), GetAgent returns adapter state, disconnect-reconnect history persistence.
- **Rewrite**: `agent_dialog_test.go` — update for new connect path and frame format (text frames carry `agent_profile_name`). Preserved invariants: thinking+text+wait frame assertions, FIFO ordering.
- **Rewrite**: `agent_checkpoint_test.go` — update for new ListMessages path and profile switching history persistence. Preserved invariants: chronological message ordering, cross-connection resume, delete-session cleanup.
- **Modify**: `system_test.yaml` — update affected suite names and descriptions if needed.

## Test Impact Assessment

- `projects/game/agent/src/llm.test.ts`: rewrite to test `AgentAdapter` construction with `createAgent` (static `systemPrompt`), and service-owned `beforeModel` middleware; verify profile model selection, `HumanMessage`/AI response handling, reasoning/text block extraction, and provider error propagation.
- `projects/game/agent/src/context-middleware.test.ts` (new): prove the `beforeModel` middleware receives `state.messages`, observes the current user turn, and returns behavior-compatible state; provide a placeholder test showing how a future format policy can be swapped in.
- `projects/game/agent/src/adapter-manager.test.ts` (new): prove adapter creation on first message, profile switching (unbind + create), synchronous unbind on disconnect, async cleanup, and connection exclusivity.
- `projects/game/agent/src/fake-llm.test.ts`: verify the fake adapter still yields deterministic blocks under the new `AgentAdapter` contract.
- `projects/game/agent/src/handler.test.ts`: rewrite to test the new Connect flow (profile selection from frames, adapter switching, connection kick), GetAgent (adapter state), ListMessages (session-level path), and same-session serialization.
- `projects/game/agent/src/spike.test.ts`: remove V1 (`createDeepAgent`); keep V2 (`initChatModel`), V3 (`fakeModel`), V4 (`contentBlocks`/streaming).
- `projects/game/proxy/handler/handler_test.go`: remove `TestCreateAgent`, `TestDeleteAgent` and related error-path cases; keep `TestGetAgent`, `TestListMessages` with updated mocks.
- `projects/game/proxy/service/connect_agenter_test.go`: update for new connect protocol (no first-frame owner lookup for agent creation).
- `projects/game/gateway/cmd/main_test.go`: update `isWebSocketConnectPath` assertions for new path.
- `projects/game/desktop/app_test.go`: remove create/delete agent tests; update connect tests.
- `projects/game/desktop/internal/api/client_test.go`: remove create/delete HTTP test cases.
- `projects/game/desktop/internal/api/websocket_test.go`: update URL assertions.
- `projects/game/testplan/helpers_test.go`: rewrite agent helpers (remove create/delete, change WS path, add profile to frames).
- `projects/game/testplan/agent_lifecycle_test.go`: rewrite for connect-with-profile, switching, exclusivity.
- `projects/game/testplan/agent_dialog_test.go`: rewrite for new frame format and connect path.
- `projects/game/testplan/agent_checkpoint_test.go`: rewrite for new ListMessages path and cross-profile history persistence.
- `projects/game/testplan/system_test.yaml`: ensure affected suites are selected.

## Complexity Tracking

No constitution violations. The API surface change (removing Create/Delete, changing paths) is a deliberate product decision confirmed by the user during the specification phase. The old spec 010's "preserve public APIs" constraint is explicitly superseded by this feature. The feature adds complexity in the form of adapter lifecycle management and connection exclusivity, but removes complexity by eliminating explicit agent lifecycle operations, the deepagent dependency, and the redundant external StateGraph.

## Phase 0 Research Summary

See `research.md` for decisions. Key decisions: replace deepagents with LangChain `createAgent` harness; separate SessionAgent (session-scoped history owner) from AgentAdapter (stateless profile-specific processor); use static `systemPrompt` parameter in `createAgent` (one prompt per adapter, no dynamic injection needed); use a service-owned `beforeModel` middleware as the context-preparation boundary; adapter created on-demand at message time, not at connection time; synchronous unbind + async cleanup on disconnect; single WebSocket connection per session with kick-old behavior; remove Create/Delete RPCs and change connect/list-messages paths; AgentFrame gains optional `agent_profile_name`.

## Phase 1 Design Summary

See `data-model.md` for SessionAgent and AgentAdapter entity definitions, state transitions, and compatibility invariants; `contracts/agent-service-api.md` for the new API surface contract; `contracts/context-foundation.md` for the context preparation boundary; and `quickstart.md` for validation scenarios and commands.

## Constitution Check (Post-Design Re-check)

- **Authority & Style**: PASS. Design artifacts preserve the read-before-edit requirements and reference active repository style docs.
- **Bazel Integrity**: PASS. Quickstart and plan use Bazel wrappers and Bazel-managed PNPM/dependency workflow.
- **Generated Files & Dependencies**: PASS. Proto changes go through Bazel proto rules; dependency removal is documented through package, BUILD, and lock/module synchronization.
- **Testing Strategy**: PASS. Unit, service contract, and large-test validation paths are explicit and mapped to affected files.
- **Behavioral Acceptance**: PASS. Validation drives real public surfaces (HTTP, WebSocket) rather than checking only code changes.
- **Review Scope**: PASS. Review includes service refactor, proto changes, proxy/gateway/desktop changes, dependency/build metadata, tests, and style.
- **Repository Verification**: PASS. Final whole-repository build/test and targeted agent tests are included.
- **Testplan Execution**: PASS. Game large-test execution through `testplan` skill is required unless environment blockers are documented.
- **Test Impact Assessment**: PASS. Affected unit and large-test files are explicitly listed.
- **Change Classification**: PASS. Modify/delete/new scopes and preserved invariants are documented.
- **Citation Links**: PASS. LangChain documentation and GitHub source/issues cited with URLs.

## References

### LangChain (the downgrade target layer)

- **LangChain overview** — https://docs.langchain.com/oss/javascript/langchain/overview
  Defines the three-layer model (Deep Agents → LangChain `createAgent` → LangGraph) and confirms `createAgent` is the official harness below Deep Agents.
- **Agents (`createAgent`)** — https://docs.langchain.com/oss/javascript/langchain/agents
  Documents `createAgent({ model, tools, systemPrompt, middleware, checkpointer, contextSchema })`, invocation with `thread_id`, and streaming. Confirms the compiled graph is stateless — all state is externalized to the checkpointer.
- **Short-term memory** — https://docs.langchain.com/oss/javascript/langchain/short-term-memory
  Documents thread-scoped checkpoint persistence, state extension via middleware, and the `beforeModel`/`afterModel` hooks. Confirms `thread_id` is the primary key for checkpoint state isolation.
- **Middleware overview** — https://docs.langchain.com/oss/javascript/langchain/middleware
  Confirms middleware runs inside the compiled LangGraph that `createAgent` returns. Lists hook lifecycle (`beforeModel` / `afterModel` / `wrapModelCall`).
- **Prebuilt middleware** — https://docs.langchain.com/oss/javascript/langchain/middleware/built-in
  Catalog of production-ready middleware. Confirms `FilesystemMiddleware`, `createSubAgentMiddleware`, `MemoryMiddleware`, `SkillsMiddleware` come from `deepagents` and are excluded by its removal.
- **`createAgent` API reference** — https://reference.langchain.com/javascript/langchain/index/createAgent
  Authoritative signature and parameter reference.
- **Checkpointers** — https://docs.langchain.com/oss/javascript/langgraph/checkpointers
  Documents `thread_id` as the storage and retrieval key for checkpoint state.

### LangGraph source (thread isolation evidence)

- **MemorySaver source** — https://github.com/langchain-ai/langgraphjs/blob/main/libs/checkpoint/src/memory.ts#L71-L79
  Confirms `MemorySaver.storage` is keyed by `thread_id` at the outer level, ensuring per-thread isolation.
- **BaseCheckpointSaver source** — https://github.com/langchain-ai/langgraphjs/blob/main/libs/checkpoint/src/base.ts#L14-L58
  Confirms `BaseCheckpointSaver` is stateless (only holds a `serde` instance).
- **Thread isolation regression tests (PR #10693)** — https://github.com/langchain-ai/langchainjs/pull/10693
  Added explicit tests ensuring `beforeAgent` and `wrapModelCall` middleware state does not leak across `thread_id` boundaries.
- **Cross-thread contamination investigation (Issue #2040)** — https://github.com/langchain-ai/langgraphjs/issues/2040
  Documents that checkpoint-level isolation is sound; observed contamination was traced to LLM provider prompt caching and `AsyncLocalStorageProviderSingleton`, not the checkpointer.

### Deep Agents (the layer being removed)

- **Deep Agents overview** — https://docs.langchain.com/oss/javascript/deepagents/overview
  Documents the batteries-included harness (context compression, virtual filesystem, task planning, subagent spawning) that this feature removes.

### Repository-internal references

- **Constitution** — `.specify/memory/constitution.md` (v1.4.0).
- **Superseded spec** — `specs/010-langchain-agent-downgrade/` (removed).
- **Baseline spec** — `specs/009-agent-checkpoint-redesign/` (behavioral baseline for session/checkpoint concepts).
- **Style guides** — `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`.
- **Current implementation** — `projects/game/agent/src/llm.ts` (`createDeepAgent` usage), `handler.ts` (Create/Delete/Connect handlers), `server.ts` (StateGraph wiring).
