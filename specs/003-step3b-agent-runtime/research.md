# Research: Step3.b Agent Runtime

**Branch**: `003-step3b-agent-runtime` | **Date**: 2026-06-03

## R1: TypeScript gRPC service runtime

**Decision**: Implement the replacement agent service as a new TypeScript Node service using `@grpc/grpc-js`. Register `AgentService` with `server.addService(...)`, bind using `server.bindAsync(...)`, and use `tryShutdown(...)` for graceful termination.

**Rationale**:
- User direction explicitly requires grpc js/ts.
- Official `@grpc/grpc-js` examples use `new grpc.Server()`, `addService`, `bindAsync`, and `tryShutdown` for Node servers.
- The existing proxy already talks to an `AgentService` gRPC backend, so preserving the protobuf service while replacing only the agent service implementation keeps gateway/proxy/session paths unchanged.

**Alternatives considered**:
- Keep Go gRPC service and call DeepAgents out-of-process: rejected because the requested service switch is TypeScript grpc-js, and an extra bridge adds failure modes.
- Use Connect/HTTP directly in the agent service: rejected because proxy expects gRPC `AgentService` and public routing must stay unchanged.

**References**:
- `@grpc/grpc-js` server API: `grpc.Server`, `addService`, `bindAsync`, `tryShutdown`.
- `projects/game/game.proto` `AgentService` and `AgentFrame` definitions.

## R2: Proto binding strategy

**Decision**: Prefer Bazel-owned TypeScript protobuf/gRPC generation or generated descriptors at build time; do not commit generated source. If repository TS proto generation is not already established when tasks begin, use `@grpc/proto-loader` plus its official `proto-loader-gen-types` output as the first implementation path, with generated types produced during build/test only.

**Rationale**:
- Constitution forbids committed generated proto/grpc sources.
- `@grpc/proto-loader` supports TypeScript type generation (`ProtoGrpcType` and service handler interfaces) and loading package definitions into grpc-js service definitions.
- Build-time generation gives better type safety once Bazel TS proto rules are in place.

**Alternatives considered**:
- Handwrite TypeScript interfaces matching `game.proto`: rejected because it creates drift from the protobuf contract.
- Commit generated `.ts` service files: rejected by repository generated-file policy.

## R2b: gRPC health and reflection

**Decision**: Plan the TypeScript agent service to expose standard gRPC health checking and reflection when the chosen grpc-js packages fit Bazel packaging: `grpc-health-check` for `grpc.health.v1.Health` and `@grpc/reflection` for grpcurl/debugging.

**Rationale**:
- Current Go services register gRPC reflection for debugging, so the TypeScript replacement should preserve equivalent operational ergonomics.
- Standard health status lets deployment and large-test tooling distinguish a bound process from a ready agent service.
- Both packages integrate with grpc-js server registration rather than requiring protocol changes.

**Alternatives considered**:
- Skip health/reflection initially: acceptable only if Bazel packaging blocks the packages, but it weakens parity with existing service operations.
- Add custom health RPCs to `game.proto`: rejected because standard gRPC health avoids public contract churn.

## R3: DeepAgents integration shape

**Decision**: Use the official `deepagents` package with `createDeepAgent({ model, systemPrompt, tools, skills })` for a minimal single primary agent. Disable or omit subagents, long-term memory, shell/filesystem backends, and autonomous loops for this phase.

**Rationale**:
- LangChain DeepAgents TypeScript docs show `import { createDeepAgent } from "deepagents"` and `tool` from `langchain`.
- The spec requires only one real model-driven reasoning pass with a single primary agent and no subagents/long-term memory.
- Keeping the DeepAgent factory small makes profile/model/SKILL validation observable during `CreateAgent` and keeps invoke behavior deterministic enough for testplan acceptance.

**Alternatives considered**:
- Use plain LangChain `createAgent`: rejected because user explicitly requires TS DeepAgent.
- Enable DeepAgents subagents or long-term memory: rejected as out of scope and harder to verify for screenshot-only continuation.

**References**:
- LangChain DeepAgents JS overview: `deepagents`, `createDeepAgent`, `tools`, `systemPrompt`, `skills`.

## R4: Progressive event streaming

**Decision**: Wrap each DeepAgent invoke in an invoke coordinator that consumes DeepAgent streaming events (`streamEvents` or equivalent streaming API available in the selected DeepAgents version) and maps them to `AgentFrame` payloads on the existing bidirectional stream. Final operation is emitted through a dedicated desktop-operation tool and capped at one pending operation per invoke.

**Rationale**:
- Spec requires real-time thinking/text/tool progress before invoke completion.
- DeepAgents public examples and tests expose streaming event support.
- A runtime-owned operation tool is the cleanest boundary between model/tool calls and protobuf `AgentOperationFrame` validation.

**Alternatives considered**:
- Wait for `agent.invoke(...)` completion and emit frames afterward: rejected because desktop must receive progressive events in real time.
- Let the model emit JSON text and parse it into operations: rejected because a typed tool schema is safer and easier to validate.

## R5: Prompt/profile/SKILL loading

**Decision**: `CreateAgent` must call the existing prompt service to load the selected enabled `AgentProfile`, then load every referenced enabled `Skill`, validate supported MCP names, validate model/provider credentials, and only then create the runtime agent.

**Rationale**:
- The prompt service owns tool-independent SKILLS and profile storage.
- Creation-time validation prevents unusable agents and satisfies explicit credential/MCP/SKILL failure requirements.
- Runtime-owned MCP/tool SKILLS remain compiled into the TypeScript service and are selected by supported MCP registry names.

**Alternatives considered**:
- Lazy-load profile or credentials on first screenshot: rejected because spec requires `CreateAgent` failure before an unusable agent is reported.
- Store copied SKILL content in agent persistence: rejected because prompt service remains source of truth and step3.b uses in-memory runtime state.

## R6: OpenCode Go provider support

**Decision**: Treat OpenCode Go as an explicit provider model reference: `opencode-go/<model-id>`. Validate the model id against the supported OpenCode Go model list or `/models` endpoint, read the credential from deployment secret material, and fail `CreateAgent` for missing, empty, unreadable, invalid, unauthorized, malformed, or unsupported configurations.

**Rationale**:
- OpenCode Go docs define model IDs in `opencode-go/<model-id>` form and expose OpenAI-compatible `/v1/chat/completions` plus Anthropic-compatible `/v1/messages` endpoints depending on model.
- The feature depends on deploy secret configuration, so provider keys must be supplied as mounted secret material, not profile content.
- Silent fallback would violate spec and hide configuration errors.

**Alternatives considered**:
- Use arbitrary OpenAI-compatible provider URLs from profile: rejected because only default DeepAgents provider and OpenCode Go are in scope.
- Validate credentials during first invoke: rejected because `CreateAgent` must fail up front.

## R7: Lifecycle and concurrency model

**Decision**: Each runtime agent maintains a state machine with at most one active invoke and at most one pending operation awaiting the next screenshot observation. Invoke timeout defaults to 10 minutes. Idle deletion triggers after 30 minutes only when state is idle, no active invoke exists, and no pending operation awaits screenshot continuation.

**Rationale**:
- Matches spec requirements and keeps screenshot-only continuation unambiguous.
- Prevents concurrent invokes from interleaving `AgentFrame.sequence` and operation ids.
- Makes timeout and cleanup directly testable with fake clocks.

**Alternatives considered**:
- Allow concurrent screenshot invokes: rejected because operation sequencing and screenshot-only continuation would become ambiguous.
- Delete all agents after wall-clock inactivity including pending operations: rejected because pending desktop operation is an active gameplay state.

## R8: Acceptance and observability

**Decision**: Validate through layered tests plus game testplan: TS unit tests for runtime/provider/tool/state, grpc-js integration tests for `AgentService`, Go compatibility tests for proxy/gateway when contracts change, desktop UI tests/manual QA for prompt/play timeline, and large-test execution via `testplan` skill.

**Rationale**:
- `style/large_test.md` requires grpc/http services to use testplan-style large tests.
- Constitution requires observable behavior through the real surface, not file-only validation.
- SigNoz skill remains the debugging path for deployed test failures.

**Alternatives considered**:
- Unit tests only: rejected because service-chain compatibility is the highest-risk part of the runtime replacement.
