# Implementation Plan: Agent Engine Context Control Foundation

**Branch**: `010-langchain-agent-downgrade` | **Date**: 2026-06-15 | **Spec**: `specs/010-langchain-agent-downgrade/spec.md`

**Input**: Feature specification from `specs/010-langchain-agent-downgrade/spec.md`

## Summary

Refactor the game agent service so response generation no longer depends on the `deepagents` harness while preserving every public behavior delivered by the checkpoint/session redesign. The `RealLLMAdapter` becomes a thin wrapper around LangChain's `createAgent` harness: it constructs a `createAgent` instance per model/provider configuration, uses `dynamicSystemPromptMiddleware` to inject the per-session system prompt at invocation time, and exposes a service-owned `beforeModel` middleware as the future context-customization boundary. The harness automatically reads from and writes to the session-scoped LangGraph checkpoint, appends the current user turn, invokes the configured model, and yields the same `thinking`/`text` blocks consumed by the existing `Handler.Connect` stream.

No public API, proto, gateway/proxy route, desktop binding, or operator workflow changes are planned. The feature removes the opaque deepagent layer because its built-in context management, virtual filesystem, planning, and subagent defaults are not part of the observable contract and block future custom history formatting. Existing checkpoint resume, `ListMessages`, delete/recreate isolation, profile model selection, same-session serialization, and deterministic fake-agent large tests remain the acceptance baseline.

## Technical Context

**Language/Version**: TypeScript 6.x/ES2020 CommonJS for `projects/game/agent`; Go for existing gateway/proxy/desktop clients and large tests; protobuf/gRPC contracts in `projects/game/game.proto` remain unchanged.

**Primary Dependencies**: Existing `langchain` (provides `createAgent`, `dynamicSystemPromptMiddleware`, and built-in middleware), `@langchain/core`, provider packages `@langchain/openai` and `@langchain/anthropic`, existing `@langchain/langgraph` `MemorySaver` for in-memory session continuity, existing `@grpc/grpc-js` / `@grpc/proto-loader`, existing repository JS logging/OTel packages. `deepagents` is removed from the agent service dependency surface and Bazel package runtime deps once no source or test imports it.

**Storage**: In-memory only. Agent metadata remains in the handler's `Map<sessionId, AgentMetadata>`; conversation continuity remains in the shared `MemorySaver` checkpointer managed by the `createAgent` harness, keyed by `thread_id = sessionId`. No durable storage, cross-process recovery, or data migration is in scope.

**Testing**: Vitest-under-Bazel for TypeScript agent adapter/handler/server/fake-LLM behavior; Go large tests through `projects/game/testplan/system_test.yaml` using the `testplan` skill; final repository validation via `bazel build //...` and `bazel test //...` unless a documented infrastructure blocker prevents completion. **Test sequencing is tests-first where practical**: update or add adapter/handler tests before the service refactor; large-test acceptance remains post-build validation.

**Target Platform**: Linux-hosted game backend services and Wails desktop client using the existing gateway/proxy/agent path.

**Project Type**: Multi-service system with a TypeScript gRPC agent service, Go gateway/proxy/session/prompt services, protobuf API surface, Wails desktop app, Svelte frontend, and YAML-orchestrated large tests.

**Performance Goals**: Normal message history remains visible within 2 seconds when entering play; same-session rapid sends preserve FIFO order in 100% of tested cases; response latency remains bounded by provider/model latency plus small in-process history construction overhead.

**Constraints**: Use Bazel wrappers for all builds/tests and Bazel-managed PNPM for package operations. Preserve `game.proto` public contracts. Do not introduce custom history controls in this feature. Preserve `sessionId` as the conversation continuity key (`thread_id`). Preserve in-memory-only lifecycle semantics. Do not reintroduce `DialogRuntime`-style manual runtime ownership, inactivity cleanup, or public API changes. Keep dependency versions centralized in root `pnpm-workspace.yaml`; removing `deepagents` must update `projects/game/agent/package.json`, `projects/game/agent/BUILD.bazel`, and lock/module state through repository workflow. The service-owned context-preparation boundary must be implemented as a `beforeModel` middleware that is identifiable and replaceable without public API changes.

**Scale/Scope**: Source changes are expected to stay primarily in `projects/game/agent/src/llm.ts` (wrap `createAgent` with `dynamicSystemPromptMiddleware` and a service-owned `beforeModel` middleware), a new or updated middleware module for the context-preparation boundary, `fake-llm.ts` to align with the `createAgent`/`fakeModel` path, `server.ts` to remove the now-redundant external `StateGraph(MessagesAnnotation)` and wire the shared checkpointer, `handler.ts` to simplify `ListMessages` (replace the deepagent namespace-scan hack with standard `agent.getState()`), TypeScript tests, `projects/game/agent/package.json`, `projects/game/agent/BUILD.bazel`, root dependency metadata once `deepagents` is unused, and existing testplan cases if assertions need behavior-preserving adjustments. Gateway/proxy/desktop/proto files are validation surfaces, not planned change targets.

**References**: Official LangChain documentation underpins every design decision below. See the [References](#references) appendix for the full linked index (LangChain overview, agents/`createAgent`, short-term memory, middleware, prebuilt middleware, Deep Agents overview, and API reference).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Plan is based on `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`, the active spec, and existing `projects/game/agent` / `projects/game/testplan` patterns. Implementation tasks must require executors to re-read these before code changes.
- **Bazel Integrity**: PASS. Planned validation uses Bazel-managed commands: `bazel test //projects/game/agent:lib_test`, `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/agent ...` only if package metadata changes require PNPM operations, `bazel run //:gazelle projects/game/agent` if BUILD generation/sync is required, `bazel mod tidy` if dependency graph changes require it, `bazel build //...`, and `bazel test //...`.
- **Generated Files & Dependencies**: PASS. No proto contract change is planned and no generated proto/grpc files are committed. If `deepagents` becomes unused, remove it from the agent package and Bazel runtime deps; if root catalog/lock changes are needed, perform them through repository PNPM/Bazel workflow instead of manual lock edits.
- **Testing Strategy**: PASS. Adapter and handler unit tests are updated before/with the refactor to prove explicit context construction, model selection, frame ordering, checkpoint/list-message behavior, delete/recreate isolation, and no deepagent import. Service-level behavior requires large-test acceptance through existing game testplans.
- **Behavioral Acceptance**: PASS. Acceptance validates real surfaces: gateway HTTP create/get/delete/list messages, WebSocket connect/send frames, desktop-equivalent play flow, and fake-agent test deployment.
- **Review Scope**: PASS. Review must include TypeScript service refactor, dependency/build metadata, test-code review, large-test contract review, and style review.
- **Repository Verification**: PASS. Final validation includes targeted agent tests, affected large-test plan, `bazel build //...`, and `bazel test //...` unless environment blockers are documented.
- **Testplan Execution**: PASS. Agent service behavior changes require running the game testplan via `testplan` skill or documenting deployment/infrastructure blockers and residual risk.
- **Test Impact Assessment**: PASS. Affected tests are listed in this plan: `projects/game/agent/src/llm.test.ts`, `context-middleware.test.ts`, `fake-llm.test.ts`, `handler.test.ts`, `bootstrap-test.ts`/server wiring tests as applicable, `projects/game/testplan/agent_dialog_test.go`, `agent_checkpoint_test.go`, and `system_test.yaml` if suite/case wiring needs updates.
- **Change Classification**: PASS. Changes are classified below; modifications to existing code are explicit refactorings preserving public invariants.

## Project Structure

### Documentation (this feature)

```text
specs/010-langchain-agent-downgrade/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── agent-service-compatibility.md
│   └── context-foundation.md
└── tasks.md              # Created by /speckit.tasks, not /speckit.plan
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                              # Public contract baseline; no planned change
├── agent/
│   ├── package.json                        # Remove deepagents dependency if unused
│   ├── BUILD.bazel                         # Remove deepagents npm/runtime deps if unused
│   └── src/
│       ├── llm.ts                          # Refactor RealLLMAdapter to wrap createAgent with dynamicSystemPromptMiddleware and service-owned beforeModel middleware
│       ├── context-middleware.ts           # Service-owned beforeModel middleware: the explicit context-preparation boundary
│       ├── fake-llm.ts                     # Align deterministic adapter behavior with createAgent/fakeModel path while keeping LLMAdapter contract
│       ├── server.ts                       # Remove redundant external StateGraph(MessagesAnnotation); wire shared MemorySaver checkpointer
│       ├── handler.ts                      # Preserve public RPC/stream behavior; simplify ListMessages to use standard createAgent graph state
│       ├── llm.test.ts                     # Replace deepagent-specific streaming tests with explicit context/history/model tests
│       ├── fake-llm.test.ts                # Verify deterministic fake response contract remains stable
│       ├── handler.test.ts                 # Verify public behavior still passes through new adapter contract and session serialization
│       └── spike.test.ts                   # Delete or update if deepagent import checks are obsolete
└── testplan/
    ├── agent_dialog_test.go                # Existing large-test acceptance for create/connect/dialog/FIFO
    ├── agent_checkpoint_test.go            # Existing large-test acceptance for resume/list/delete/recreate/model/FIFO
    ├── system_test.yaml                    # Ensure affected agent suites are included
    └── BUILD.bazel                         # Update only if large-test source set changes
```

**Structure Decision**: Keep the existing multi-service layout and limit implementation to the agent service unless validation exposes an observable contract regression. The feature is an internal foundation replacement; changing proto/gateway/proxy/desktop would contradict the clarified compatibility baseline.

## Change Classification and Refactoring Scope

- **Modify / Refactor**: `projects/game/agent/src/llm.ts` replaces `createDeepAgent` usage with `createAgent` from `langchain`, configured with `dynamicSystemPromptMiddleware` and a service-owned `beforeModel` middleware. Refactoring goal: keep LangGraph-managed checkpoint/resume while making conversation-history construction explicit and future-customizable. Preserved invariants: `LLMAdapter.generateTurn` streams `ContentBlock` values; profile model/provider selection works; provider errors propagate to handler warn frames; no public client behavior changes.
- **New**: `projects/game/agent/src/context-middleware.ts` (or equivalent module) contains the service-owned `beforeModel` middleware that implements the context-preparation boundary. Refactoring goal: provide one identifiable, testable, replaceable hook where future custom-history formatting can be plugged in. Preserved invariants: current behavior is identity-like beyond system-prompt injection; public clients cannot influence the policy.
- **Modify / Refactor**: `projects/game/agent/src/server.ts` removes the redundant external `StateGraph(MessagesAnnotation).compile({checkpointer})` and continues to supply the shared `MemorySaver` to the `LLMAdapter`. Refactoring goal: eliminate the dead graph whose namespace only conflicted with deepagent. Preserved invariants: startup, TLS detection, prompt client wiring, fake adapter override, gRPC registration, and OTel/logging behavior.
- **Modify / Refactor**: `projects/game/agent/src/handler.ts` simplifies `ListMessages` by reading conversation state through the `createAgent` graph (`agent.getState()`) instead of the deepagent namespace-scan hack. Refactoring goal: align checkpoint reads with the single graph that now owns both generation and state. Preserved invariants: Create/Get/Delete/ListMessages/Connect semantics, status/echo/deprecated payload handling, same-session mutex, wait/warn/text/thinking frame behavior, chronological message ordering.
- **Modify / Refactor**: `projects/game/agent/src/fake-llm.ts` aligns deterministic behavior with the `createAgent`/`fakeModel` path so that fake-adapter sessions also populate checkpoint state and can validate resume/list behavior. Refactoring goal: keep fake and real adapters behaviorally equivalent at the public service surface. Preserved invariants: no network calls in unit tests; deterministic output.
- **Modify / Refactor**: TypeScript tests are updated to lock behavior against the new foundation rather than deepagent internals. Refactoring goal: test public adapter/handler behavior, middleware context construction, and dynamic system-prompt injection. Preserved invariants: no network calls in unit tests; fake model/fake adapter determinism.
- **Modify**: `projects/game/agent/package.json`, `projects/game/agent/BUILD.bazel`, root dependency metadata to remove `deepagents` once no source/test imports it. Preserved invariants: Bazel-managed package/build workflow and centralized catalog rules.
- **Modify**: `projects/game/testplan/*` only if current assertions are tied to deepagent-specific frame artifacts rather than public behavior. Preserved invariants: large tests validate real HTTP/WebSocket surfaces using deterministic test deployment.
- **Delete**: Remove obsolete deepagent spike assertions/import checks (`spike.test.ts` V1) because deepagent is no longer a supported dependency; retain V2–V4 LangChain API validations that the new path still relies on. Do not delete public acceptance tests.
- **No planned change**: `projects/game/game.proto`, proxy, gateway, desktop Go bindings, and desktop frontend remain contract surfaces for verification, not implementation targets.

## Test Impact Assessment

- `projects/game/agent/src/llm.test.ts`: replace deepagent-specific tests with tests that prove `createAgent` is constructed with `dynamicSystemPromptMiddleware` and the service-owned `beforeModel` middleware; verify `HumanMessage`/AI response handling, reasoning/text block extraction, provider/model selection, and provider error propagation through the harness.
- `projects/game/agent/src/context-middleware.test.ts` (new): prove the service-owned `beforeModel` middleware receives `state.messages`, observes the current user turn, and returns a state equivalent to current behavior; provide a placeholder test showing how a future format policy can be swapped in.
- `projects/game/agent/src/fake-llm.test.ts`: verify the fake adapter still yields deterministic thinking and text blocks under the `LLMAdapter` contract; if the fake adapter is refactored to use `createAgent` + `fakeModel`, verify checkpoint state is populated and readable.
- `projects/game/agent/src/handler.test.ts`: keep public RPC/stream expectations; add or update cases that prove `sessionId`/`thread_id` and shared continuity state are passed to the adapter, same-session serialization remains FIFO, and `ListMessages` reads through the standard graph state.
- `projects/game/agent/src/bootstrap-test.ts` / `server.ts` tests: update to assert the server no longer constructs the external `StateGraph(MessagesAnnotation)`; assert shared in-memory `MemorySaver` continuity wiring remains single-instance and test override still works.
- `projects/game/agent/src/spike.test.ts`: remove V1 (`createDeepAgent`) tests; keep V2 (`initChatModel`), V3 (`fakeModel`), and V4 (`contentBlocks`/streaming) because they validate APIs the `createAgent` path still depends on. Consider renaming the file to reflect LangChain API validation rather than deepagent spike.
- `projects/game/testplan/agent_dialog_test.go`: rerun unchanged to prove create/connect/text/thinking/wait/FIFO behavior through gateway/WebSocket remains compatible.
- `projects/game/testplan/agent_checkpoint_test.go`: rerun unchanged to prove resume/list/delete-recreate/model/FIFO behavior remains compatible; adjust only if assertions depend on deepagent-only non-contract artifacts.
- `projects/game/testplan/system_test.yaml`: ensure affected agent dialog/checkpoint suites are selected for large-test execution.

## Complexity Tracking

No constitution violations. The feature is intentionally narrower than full deepagent parity: the clarified baseline preserves public APIs, operator flows, message history, resume, deletion, model/profile behavior, and observable configured tool behavior. Reimplementing hidden deepagent planning/filesystem/subagent internals would add complexity without user-visible value and would undermine the future custom-history goal.

## Phase 0 Research Summary

See `research.md` for decisions. Key decisions: replace deepagents with the LangChain `createAgent` harness (not raw chat-model execution) instead of full deepagent parity; keep LangGraph as the execution/persistence layer; use `dynamicSystemPromptMiddleware` for per-session system prompts; use a service-owned `beforeModel` middleware as the context-preparation boundary; keep existing public agent contracts unchanged; preserve session-scoped in-memory continuity; remove `deepagents` dependencies only after source imports are gone; and validate via existing unit and large-test surfaces.

## Phase 1 Design Summary

See `data-model.md` for service-owned context entities/state transitions; `contracts/agent-service-compatibility.md` for public contract invariants; `contracts/context-foundation.md` for the future-customizable context preparation boundary; and `quickstart.md` for validation scenarios and commands.

## Constitution Check (Post-Design Re-check)

- **Authority & Style**: PASS. Design artifacts preserve the read-before-edit requirements and reference active repository style docs.
- **Bazel Integrity**: PASS. Quickstart and plan use Bazel wrappers and Bazel-managed PNPM/dependency workflow.
- **Generated Files & Dependencies**: PASS. No generated proto files are planned; dependency removal is documented through package, BUILD, and lock/module synchronization.
- **Testing Strategy**: PASS. Unit, service contract, and large-test validation paths are explicit and mapped to affected files.
- **Behavioral Acceptance**: PASS. Validation drives real public surfaces rather than checking only code changes.
- **Review Scope**: PASS. Review includes service refactor, dependency/build metadata, tests, and style.
- **Repository Verification**: PASS. Final whole-repository build/test and targeted agent tests are included.
- **Testplan Execution**: PASS. Game large-test execution through `testplan` skill is required unless environment blockers are documented.
- **Test Impact Assessment**: PASS. Affected unit and large-test files are explicitly listed.
- **Change Classification**: PASS. Modify/delete/no-change scopes and preserved invariants are documented.

## References

Official documentation that grounds the design decisions in this plan. Cited inline where relevant; collected here for quick lookup.

### LangChain (the downgrade target layer)

- **LangChain overview** — https://docs.langchain.com/oss/javascript/langchain/overview
  Defines the three-layer model (Deep Agents → LangChain `createAgent` → LangGraph) and confirms `createAgent` is the official "highly configurable harness" below Deep Agents. Basis for the [Summary](#summary) downgrade target.
- **Agents (`createAgent`)** — https://docs.langchain.com/oss/javascript/langchain/agents
  Documents `createAgent({ model, tools, systemPrompt, middleware, checkpointer, contextSchema })`, the model/tools/system-prompt/structured-output core components, invocation with `thread_id`, and streaming. Confirms `createAgent` does NOT include filesystem/planning/subagent by default (those are opt-in Deep Agents middleware), which makes the migration behavior-preserving by construction.
- **Short-term memory** — https://docs.langchain.com/oss/javascript/langchain/short-term-memory
  Documents thread-scoped checkpoint persistence, state extension via `StateSchema` + middleware, and the `beforeModel`/`afterModel` hooks. The `beforeModel` hook is the official "Context Preparation Boundary" used by this plan. Also documents `dynamicSystemPromptMiddleware` for per-invocation system prompts.
- **Middleware overview** — https://docs.langchain.com/oss/javascript/langchain/middleware
  Confirms middleware runs inside the compiled LangGraph that `createAgent` returns (so checkpoint/resume and hooks travel together), and lists the hook lifecycle (`beforeModel` / `afterModel`).
- **Prebuilt middleware** — https://docs.langchain.com/oss/javascript/langchain/middleware/built-in
  Catalog of production-ready middleware. Relevant future options: `summarizationMiddleware` (history compression), `toolCallLimitMiddleware`, `modelRetryMiddleware`. Confirms `FilesystemMiddleware`, `createSubAgentMiddleware`, `MemoryMiddleware`, `SkillsMiddleware` come from the `deepagents` package and are therefore excluded by removing `deepagents`.
- **`createAgent` API reference** — https://reference.langchain.com/javascript/langchain/index/createAgent
  Authoritative signature and parameter reference for implementation.
- **`initChatModel` API reference** — https://reference.langchain.com/javascript/langchain/index/initChatModel
  Provider-routing model initializer retained from the current implementation.

### Deep Agents (the layer being removed)

- **Deep Agents overview** — https://docs.langchain.com/oss/javascript/deepagents/overview
  Documents the batteries-included harness (context compression, virtual filesystem, task planning, subagent spawning) that this feature removes because it is not part of the game operator contract.

### Repository-internal references

- **Constitution** — `.specify/memory/constitution.md` (v1.3.0): authority order, Bazel integrity, TDD/observable delivery, Spec Kit workflow, test impact assessment, and change-classification gates.
- **Baseline spec** — `specs/009-agent-checkpoint-redesign/`: the behavioral baseline this refactor must preserve.
- **Style guides** — `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`.
- **Current implementation** — `projects/game/agent/src/llm.ts` (`createDeepAgent` usage to replace), `server.ts` (redundant external `StateGraph`), `handler.ts` (`ListMessages` namespace-scan hack at the `checkpointer.list()` call).
