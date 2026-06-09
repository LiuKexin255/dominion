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

## Decision: Preserve existing session/proxy/gateway/prompt architecture

**Rationale**: The spec requires preserving the session-agent architecture. The Go proxy already resolves `game/agent:grpc` as a stateful service and forwards bidirectional `AgentFrame` streams. The Go gateway already bridges desktop WebSocket traffic to proxy. Reusing this topology minimizes integration risk while the agent implementation changes runtime.

**Alternatives considered**:
- Let desktop call the agent service directly: rejected because it bypasses session ownership and proxy routing.
- Merge proxy and agent: rejected because it changes established architecture and increases blast radius.
