# Implementation Plan: Step3.b Agent Runtime

**Branch**: `003-step3b-agent-runtime` | **Date**: 2026-06-03 | **Spec**: `specs/003-step3b-agent-runtime/spec.md`

**Input**: Feature specification from `/specs/003-step3b-agent-runtime/spec.md`

## Summary

将 game agent 服务重新设计为 TypeScript gRPC 服务，使用 `@grpc/grpc-js` 承载现有 `AgentService` protobuf 协议，并以 TypeScript `deepagents` 的最小单 agent 能力执行截图驱动推理。外部 gateway/proxy/session/prompt 路径和 `AgentFrame` 流语义保持不变；旧 agent runtime 仅作为待替换目标，不作为内部设计参考。新设计围绕 profile 加载、tool-independent SKILL 注入、内置 desktop-operation tool、OpenCode Go provider 凭据校验、invoke 超时和 idle 清理建立可测试的运行时边界。

## Technical Context

**Language/Version**: TypeScript 6.0.2 / Node.js runtime managed through Bazel + pnpm workspace catalog; existing Go services remain Go through rules_go.

**Primary Dependencies**:
- `@grpc/grpc-js` — TypeScript/JavaScript gRPC server runtime for `AgentService`.
- `@grpc/proto-loader` with `proto-loader-gen-types` or Bazel-generated TS service definitions — proto descriptor loading and handler type generation without committed generated source.
- `@grpc/reflection` and `grpc-health-check` — grpc-js reflection/health surfaces when compatible with deployment and test tooling.
- `deepagents`, `langchain`, `@langchain/core` — TypeScript DeepAgents runtime via `createDeepAgent`.
- Provider adapters for default DeepAgents model path and OpenCode Go (`opencode-go/<model-id>`) through OpenAI/Anthropic-compatible endpoints.
- Existing `projects/game/game.proto`, prompt service, proxy routing, gateway WebSocket bridge, and deploy secret binding mechanism.

**Storage**: In-memory per-agent runtime state for step3.b agent instances; prompt/profile/SKILL storage remains prompt service MongoDB; provider credential material is read from mounted deployment secrets or equivalent runtime secret binding and is never persisted in agent data.

**Testing**: TypeScript unit/integration tests through Bazel-managed TS targets; Go compatibility tests for proxy/gateway/desktop clients where affected; game large-test/testplan through `testplan` skill for service-chain acceptance.

**Target Platform**: Linux server/Kubernetes service for the TypeScript agent runtime; desktop remains the user-facing client and Windows native operation execution remains manual acceptance when unavailable in CI.

**Project Type**: Stateful gRPC service plus existing multi-service game system and desktop frontend.

**Performance Goals**: Stream first user-visible DeepAgent progress frame during an accepted invoke before final operation; enforce default 10 minute invoke timeout; keep one pending desktop operation per invoke; idle cleanup after 30 minutes when no active invoke and no pending operation.

**Constraints**:
- Public paths and protobuf `AgentService` / `ProxyService.ConnectAgent` / `AgentFrame` compatibility remain stable.
- Do not commit generated proto/gRPC JS/TS outputs.
- Do not design from old agent implementation internals; use only protocol, routing, and deployment compatibility boundaries.
- Single primary DeepAgent only; no subagents, long-term memory, autonomous screenshot capture, or strategy-memory updates in this phase.
- OpenCode Go model refs must use `opencode-go/<model-id>` and fail during `CreateAgent` for malformed refs, missing/empty/unreadable/invalid/unauthorized credentials, or unsupported models.

**Scale/Scope**: One agent per session; one active invoke per agent; one outstanding operation awaiting screenshot-only continuation; profile references typically small (≤10 SKILL names, ≤5 MCP names); service-chain acceptance covers prompt, session, proxy, gateway, agent, and desktop surfaces.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Applicable files: `.specify/memory/constitution.md`, root `README.md`, `ideas/llm_agent_play_game/README.md`, `style/README.md`, `style/api.md`, `style/large_test.md`, Google TypeScript Style reference. Implementation tasks must require every executor to read these plus this plan before code changes.
- **Bazel Integrity**: PASS. TypeScript and pnpm commands must use Bazel-managed entrypoints (`bazel run @pnpm -- --dir <absolute path>`); Go commands use repository wrappers; proto/dependency changes require Gazelle/Bazel synchronization.
- **Generated Files & Dependencies**: PASS. Existing `projects/game/game.proto` remains the source contract; generated Go/TS proto/grpc sources are not committed. New dependencies are added through root package/workspace catalog and synchronized via Bazel/pnpm rules, `bazel run //:gazelle` where needed, and `bazel mod tidy` where Bazel module state changes.
- **Testing Strategy**: PASS. Plan requires tests before implementation where practical: TS runtime unit tests for state/provider/tool mapping, gRPC stream tests for `AgentService`, compatibility tests for proxy/gateway/desktop contract changes, and a testplan large-test for service-chain acceptance.
- **Behavioral Acceptance**: PASS. Acceptance drives real surfaces: `CreateAgent`, `ConnectAgent` WebSocket/gRPC streaming, screenshot-to-DeepAgent progress/operation, prompt/profile/SKILL CRUD from desktop, timeout/idle cleanup, and testplan deployment.
- **Review Scope**: PASS. Tasks must include code quality review, TypeScript style review, test-code review, and protocol contract review against `projects/game/game.proto`.
- **Repository Verification**: PASS. Final implementation verification must include targeted Bazel tests, `bazel build //...`, and `bazel test //...`; any skipped full-repo verification must document a concrete pre-existing blocker.
- **Testplan Execution**: PASS. Service code changes require game testplan execution through the `testplan` skill. Windows native operation execution may be manual when not available in the automated environment.

## Project Structure

### Documentation (this feature)

```text
specs/003-step3b-agent-runtime/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── agent-runtime-grpc.md
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
# Shared protobuf contract; generated outputs remain Bazel-owned
projects/game/
├── game.proto
├── proxy/               # Existing Go proxy keeps AgentService client compatibility
├── prompt/              # Existing Go prompt service owns AgentProfile and Skill resources
├── gateway/             # Existing Go gateway keeps HTTP/WebSocket public paths
├── desktop/             # Desktop prompt/play UI changes and real surface manual QA
└── testplan/            # Service-chain testplan additions

# New TypeScript agent service root (exact package path finalized in tasks)
projects/game/agent-ts/
├── package.json         # Workspace package; dependencies from root catalog when possible
├── tsconfig.json
├── BUILD.bazel          # Gazelle/manual Bazel TS targets as required
├── src/
│   ├── main.ts          # grpc-js server bootstrap and graceful shutdown
│   ├── grpc/            # AgentService handlers and protobuf binding adapter
│   ├── runtime/         # DeepAgent factory, invoke coordinator, lifecycle manager
│   ├── prompt/          # PromptService client adapter for profile/SKILL loading
│   ├── provider/        # Default provider and OpenCode Go credential validation
│   ├── tools/           # Built-in desktop operation tool and MCP registry
│   └── frames/          # AgentFrame sequencing and DeepAgent event mapping
└── test/                # Unit/integration tests for TS runtime and grpc-js surface
```

**Structure Decision**: Create a new TypeScript service package for the agent runtime instead of modifying the old agent internals. Existing Go proxy/gateway/session/prompt services remain compatibility surfaces. The shared protobuf contract stays in `projects/game/game.proto`; TypeScript service definitions are generated or loaded during build/runtime and are not committed as generated source.

## Complexity Tracking

> No Constitution violations to justify.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none)    |            |                                     |

## Phase 0: Research Summary

See `research.md` for resolved decisions:

1. TypeScript service runtime uses `@grpc/grpc-js` and binds `AgentService` directly.
2. DeepAgent integration uses `deepagents.createDeepAgent` with one primary agent, explicit tools, explicit `systemPrompt`, and no subagents/long-term memory.
3. Progressive DeepAgent events map to `AgentFrame` text/thinking/status/warn/operation frames during the same stream.
4. OpenCode Go is handled as an explicit model-provider path with secret-backed credentials and create-time validation.
5. Lifecycle state is modeled around one invoke and one pending operation per agent.

## Phase 1: Design Summary

See `data-model.md` and `contracts/agent-runtime-grpc.md` for entities, validation rules, state transitions, and observable service contracts. The design preserves `projects/game/game.proto` public semantics while documenting the TypeScript runtime-only internals needed for implementation.

## Post-Design Constitution Check

- **Authority & Style**: PASS. Design artifacts name the active style/API/testplan guidance and avoid copying old runtime design.
- **Bazel Integrity**: PASS. Quickstart and plan require Bazel-managed TypeScript, pnpm, Gazelle, and full build/test verification.
- **Generated Files & Dependencies**: PASS. Contracts identify proto as source-of-truth and forbid committed generated TS/Go artifacts.
- **Testing Strategy**: PASS. Data model and quickstart define TS unit/integration, Go compatibility, desktop UI, and testplan acceptance coverage.
- **Behavioral Acceptance**: PASS. Quickstart drives real `CreateAgent`, `ConnectAgent`, screenshot streaming, operation emission, timeout/idle cleanup, and desktop timeline surfaces.
- **Review Scope**: PASS. Implementation tasks must include style, test-code, and contract review.
- **Repository Verification**: PASS. Quickstart includes targeted Bazel tests plus `bazel build //...` and `bazel test //...`.
- **Testplan Execution**: PASS. Quickstart requires `testplan` skill/guitar execution for game service-chain acceptance.
