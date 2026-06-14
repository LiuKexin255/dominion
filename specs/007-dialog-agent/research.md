# Phase 0 Research: Dialog Agent with Chat Interface

## Decision: Replace the Go agent service in-place with a TypeScript grpc-js service

**Rationale**: The user explicitly requires removing the existing agent service and related dependencies before adding the grpc-js version. Keeping the service identity at `projects/game/agent/service.yaml` preserves the existing deploy name (`game/agent:grpc`), proxy stateful resolver target, and gateway/proxy call chain while swapping the implementation and packaging from `artifact_pkg_go` to `artifact_pkg_js`.

**Alternatives considered**:
- Add `projects/game/agent-js/` as a parallel service: rejected because it introduces a new service identity and forces wider proxy/deploy migration.
- Keep the Go agent as a wrapper around JS: rejected because it preserves the old service and duplicates runtime boundaries.

## Decision: Use grpc-js with runtime proto loading plus generated TypeScript types

**Rationale**: `experimental/ts/grpc_hello_world` already proves the repository pattern: `ts_proto_library` for compile-time type safety, `@grpc/proto-loader` plus `grpc.loadPackageDefinition()` for runtime descriptors, `grpc.Server.bindAsync()`, and `artifact_pkg_js`/`artifact_image` for deployment. This avoids committing generated proto/grpc source and matches existing Bazel-first JS service packaging.

**Alternatives considered**:
- Static JS gRPC stub generation: rejected because current repository grpc-js support intentionally uses dynamic proto loading plus generated types only.
- A Go grpc-gateway adapter in front of the agent service: rejected for production because proxy/gateway already provide the service-facing adapters.

## Decision: Use a small LangChain agent/model boundary, not built-in tools/MCP/skills

**Rationale**: The spec excludes tools, MCP, and skills in this version. Research shows LangChain JS accepts model instances in `createAgent()`/DeepAgents entrypoints, and official `fakeModel()` is a drop-in `BaseChatModel` test substitute. The implementation should isolate model calls behind `llm.ts` so production can use the configured opencode-go provider and tests can inject `fakeModel()` or a deterministic fake adapter without reaching a real provider.

**Alternatives considered**:
- Use DeepAgents full batteries-included filesystem/subagent stack: deferred because this version is pure conversation and explicitly excludes tools/MCP/skills.
- Use deprecated `FakeListChatModel`: rejected because official LangChain JS testing guidance prefers `fakeModel()` and `FakeListChatModel` lacks modern tool-binding behavior.

## Decision: Large tests use an LLM substitute at the LLM module boundary

**Rationale**: User explicitly forbids large tests from sending messages to the real provider and prefers substituting the LLM call module so the rest of the system remains exercised. The agent package will expose a deterministic fake LLM mode selected by deploy-time artifact environment/configuration for test suites, preserving real gateway/proxy/agent behavior while avoiding external provider traffic.

**Alternatives considered**:
- Mock the whole agent service in large tests: rejected because it would skip grpc-js agent behavior.
- Add a new production service config for a fake provider: rejected by user instruction. If a separate fake provider process is needed later, it must live under testplan material and be wired only from test deploy YAML, not production service config.

## Decision: Missing provider secret files are valid and yield an empty secret

**Rationale**: User explicitly requires secret files to be optional. The secret reader should return an empty string when the configured secret path does not exist, while malformed/unreadable existing files can produce a clear startup/runtime error depending on implementation contract. This allows local and fake-test operation without provisioning real credentials.

**Alternatives considered**:
- Fail startup on missing secret: rejected by user instruction.
- Store provider credentials in config/env directly: rejected by the spec’s credential-security requirement.

## Decision: Agent instances copy profile prompt/config at creation time

**Rationale**: Clarification resolved that profiles and agent instances are loosely coupled after creation. Agent creation fetches the profile from prompt service, copies prompt/model fields into the agent instance, and subsequent profile deletion does not affect active instances.

**Alternatives considered**:
- Live profile reference for every message: rejected because deleting or editing a profile would mutate active instances and contradict the clarification.

## Decision: Queue concurrent user messages per agent instance

**Rationale**: Clarification resolved that messages sent during processing are queued and processed in send order. This avoids concurrent conversation-history mutation and makes visible thinking/final response sequencing deterministic.

**Alternatives considered**:
- Reject messages while processing: rejected by clarification.
- Run multiple model calls concurrently: rejected due to ordering and context consistency risk.

## Decision: Rewrite one game testplan YAML with multiple minimal-deploy suites

**Rationale**: Existing `projects/game/testplan/system_test.yaml` already has multiple suites but deploys identical full service sets. The user requires one YAML file with multiple suites and minimal per-suite deployments. Prompt/profile suites should deploy only prompt/gateway/mongo as needed, not agent/proxy; agent dialog suites should deploy session/proxy/agent/gateway/mongo and fake LLM configuration; full surface suites deploy the full chain only where necessary.

**Alternatives considered**:
- Multiple testplan YAML files: rejected by user instruction.
- One full deploy for all suites: rejected because it wastes resources and hides service dependencies.

## Decision: Use `createDeepAgent` with default built-in capabilities

**Rationale**: The `deepagents` npm package exports `createDeepAgent()` as the primary entry point. Built-in capabilities (conversation memory, tool-use scaffolding) are kept at their defaults since this version is a pure conversational agent with no tools/MCP/skills enabled.

**Alternatives considered**:
- Manual LangChain agent assembly: rejected because `createDeepAgent` bundles memory, prompt templating, and model binding in one call with sensible defaults, reducing boilerplate.

## Decision: Use `initChatModel` Option B — custom `baseUrl` for opencode-go

**Rationale**: The `initChatModel()` helper from `@langchain/core` accepts a `baseUrl` configuration. Pointing `baseUrl` at the opencode-go OpenAI-compatible endpoint allows using a stock `ChatOpenAI`-compatible interface without custom adapters or API gateway code.

**Alternatives considered**:
- Option A (`ChatOpenAI` directly with `configuration.baseURL`): equivalent behavior, but `initChatModel` is the idiomatic LangChain entry point used in official `createDeepAgent` examples.
- Custom LangChain model class wrapping opencode-go HTTP: rejected because `initChatModel` with `baseUrl` already handles the OpenAI wire format.

## Decision: Use `streamMode: "messages"` for streaming output

**Rationale**: `streamMode: "messages"` produces `contentBlocks` arrays from the LLM. Filtering `contentBlocks` by block type enables clean separation: `reasoning` blocks → thinking frames, `text` blocks → assistant response frames. This avoids custom chunk parsing or string-based heuristic splitting.

**Alternatives considered**:
- `streamMode: "values"`: rejects because it streams the full state object on every tick, requiring application-level diffing to extract new content.
- Raw `BaseMessageChunk` parsing: rejected because `contentBlocks` is a structured, documented API in LangChain JS.

## Decision: Preserve existing session/proxy/gateway/prompt architecture

**Rationale**: The spec requires preserving the session-agent architecture. The Go proxy already resolves `game/agent:grpc` as a stateful service and forwards bidirectional `AgentFrame` streams. The Go gateway already bridges desktop WebSocket traffic to proxy. Reusing this topology minimizes integration risk while the agent implementation changes runtime.

**Alternatives considered**:
- Let desktop call the agent service directly: rejected because it bypasses session ownership and proxy routing.
- Merge proxy and agent: rejected because it changes established architecture and increases blast radius.

---

## Supplement: User Story 3 — Agent Profile Management Desktop UI

The original plan omitted the profile management desktop page (User Story 3). The following decisions resolve the gap.

## Decision: Add a dedicated profile management page in the desktop navigation

**Rationale**: The spec (US-3, FR-005, FR-006, SC-003) requires users to create, list, and delete agent profiles "through the desktop interface." The existing desktop has a page-state machine (`'sessions' | 'detail' | 'chat'`). Adding a `'profiles'` page is the minimal structural change that satisfies all four US-3 acceptance scenarios without disrupting existing flows. Users reach it via a navigation button on the sessions page.

**Alternatives considered**:
- Modal/dialog overlay from the session detail page: rejected because profile management is a standalone activity, not session-scoped. A modal over session detail conflates concerns.
- Separate Wails window: rejected because the desktop is a single-window app; adding multi-window complexity is unjustified for P2 scope.
- Profile management only within the chat sidebar: rejected because the sidebar is agent-instance-scoped and too small for a create form with multiple fields.

## Decision: Reuse the existing Go HTTP client pattern for new profile CRUD methods

**Rationale**: `projects/game/desktop/internal/api/client.go` already has `ListAgentProfiles` following a consistent pattern: build HTTP request with context, set common headers, execute, check status, unmarshal via `protojson`. Adding `CreateAgentProfile`, `GetAgentProfile`, and `DeleteAgentProfile` follows the same pattern against the already-implemented gateway REST endpoints (`POST/GET/DELETE /api/v1/prompts/agentProfiles[/{name}]`). No new HTTP infrastructure, middleware, or service connections are needed.

**Alternatives considered**:
- Call the prompt gRPC service directly from the desktop: rejected because the desktop already uses the gateway REST surface for all other operations. Adding gRPC to the desktop would introduce a new transport and defeat the gateway abstraction.
- Use the Wails runtime to make raw fetch calls from TypeScript: rejected because all other operations go through Go app methods for logging, tracing, and error handling consistency.

## Decision: Reuse existing view models and proto converters — no new view model types needed

**Rationale**: `projects/game/desktop/view_model.go` already defines `AgentProfileView`, `ListAgentProfilesView`, `agentProfileViewFromProto`, and `listAgentProfilesViewFromProto`. The `CreateAgentProfile` and `GetAgentProfile` operations return a single `*game.AgentProfile`, which `agentProfileViewFromProto` already converts. No new types or converters are required.

**Alternatives considered**:
- None needed — the existing view models already cover all CRUD response shapes.

## Decision: Profile management UI uses Svelte 5 runes for state, matching existing components

**Rationale**: The desktop frontend already uses Svelte 5 with `$state`, `$derived`, and `$props` runes (see `App.svelte`, `SessionDetail.svelte`, `AgentSidebar.svelte`). New profile management components follow the same pattern for consistency. The new `ProfileManagement.svelte` component receives profiles and callbacks as props, matching the existing parent-child data flow.

**Alternatives considered**:
- Use a Svelte store for global profile state: rejected because profiles are already managed at the App.svelte level via `$state` and passed down as props. Introducing a store for a single page is inconsistent.
- Use a form library (sveltekit-superforms, etc.): rejected because the create form has three fields (name, model, system prompt). Native Svelte bindings are sufficient and add no dependency.

## Decision: Delete confirmation uses a simple inline confirm, not a modal dialog

**Rationale**: The spec (US-3 acceptance scenario 4) requires that deleting a profile used by an active agent instance preserves the existing instance. A confirmation step prevents accidental deletion. A lightweight inline confirm (two-click pattern: Delete → "Are you sure?" → Confirm) avoids modal complexity and matches the existing danger-zone pattern in `SessionDetail.svelte`.

**Alternatives considered**:
- Modal dialog with backdrop: rejected as heavyweight for a two-field confirmation.
- No confirmation: rejected because accidental profile deletion is irreversible.
