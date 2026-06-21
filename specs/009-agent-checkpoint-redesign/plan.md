# Implementation Plan: Agent Checkpoint & Session UI Redesign

**Branch**: `009-agent-checkpoint-redesign` | **Date**: 2026-06-14 | **Spec**: `specs/009-agent-checkpoint-redesign/spec.md`

**Input**: Feature specification from `specs/009-agent-checkpoint-redesign/spec.md`

## Summary

Refactor the game desktop session/play experience and the agent service lifecycle so created agents are state-driven, resumable, and never depend on a hand-written `DialogRuntime` conversation store. The desktop session detail page becomes a metadata-only state screen: it shows profile selection only before an agent exists, shows an agent summary once created, and defers WebSocket connection until the operator enters play. The play page sidebar shows the current agent/profile/model details, loads checkpoint-backed prior messages through the dedicated `ListMessages` endpoint, and auto-connects on entry or before first send.

The agent service replaces per-session `DialogRuntime` instances with lightweight agent metadata plus one shared LangGraph checkpoint store. Every turn uses `sessionId` as the checkpoint `thread_id`; deleting an agent removes both metadata and the thread checkpoints via the checkpointer delete API, allowing clean recreation. The existing bugs where profile model settings are ignored and checkpoint continuity is defeated by per-turn random thread IDs are fixed as part of the same refactor.

## Technical Context

**Language/Version**: TypeScript 6.x/ES2020 CommonJS for `projects/game/agent`; Go for gateway/proxy/desktop backend; Svelte/TypeScript for Wails desktop frontend; protobuf/gRPC contracts in `projects/game/game.proto`.

**Primary Dependencies**: Existing `deepagents`, `@langchain/langgraph` (`MemorySaver`, `graph.getState`, `graph.getStateHistory`, `checkpointer.deleteThread`), `@grpc/grpc-js`, `@grpc/proto-loader`, Wails bindings, existing Go gateway/proxy/session/prompt clients. No new package dependency is planned.

**Storage**: In-memory only for this stage. Agent metadata is held in an in-process map keyed by `sessionId`; conversation state is held in a single shared in-memory LangGraph checkpoint store keyed by `thread_id=sessionId`. No durable storage or migration is in scope.

**Testing**: Vitest-under-Bazel for TypeScript agent/LLM/checkpoint behavior; Go unit tests for proto/gateway/proxy/desktop client and view model conversions; Svelte build/type validation and manual UI validation through the Wails desktop surface; system large-test acceptance through the existing `projects/game/testplan/system_test.yaml` using the `testplan` skill. **Test sequencing is tests-after (no TDD)**: implementation precedes unit tests, and all service-level changes require large-test acceptance.

**Target Platform**: Linux-hosted game backend services and Wails desktop client; desktop operator uses the existing gateway/proxy/agent path.

**Project Type**: Multi-service system with a TypeScript gRPC agent service, Go gateway/proxy/session/prompt services, protobuf API surface, Wails desktop app, and Svelte frontend.

**Performance Goals**: Session detail state switch is immediate after agent metadata retrieval; play page history loads within 2 seconds for normal conversation sizes; entering play establishes the WebSocket without a separate manual step; message turns remain limited by provider/model latency; same-session concurrent sends are serialized with no concurrent model invocation.

**Constraints**: Use Bazel wrappers for builds/tests and Bazel-managed PNPM for JS/TS commands. Do not commit generated proto/grpc files. Use `sessionId` as checkpoint `thread_id`. Use in-memory checkpointing only in this stage. Do not reintroduce `DialogRuntime`-style manual conversation history, manual queue, inactivity cleanup, or runtime-owned lifecycle state. Use the existing profile CRUD surface and existing prompt service data. History API is a dedicated unary agent-service RPC exposed through the gateway as REST GET.

**Scale/Scope**: One desktop UI flow pair (`SessionDetail.svelte`, `AgentSidebar.svelte`, App state flow) plus agent/proxy/gateway/desktop bindings for history and checkpointed resume. Scope includes unit/contract/large-test updates for changed behavior. Persistence across process restart, multi-operator concurrency on one session, profile editing, and long-term memory store are out of scope.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Plan is based on `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`, the active feature spec, and existing code patterns in `projects/game/agent`, `projects/game/desktop`, and `projects/game/game.proto`. Implementation tasks must require executors to re-read these before code changes.
- **Bazel Integrity**: PASS. Plan uses Bazel as build/test entrypoint: `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/agent ...`, `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/desktop/frontend ...` where applicable, `bazel run //:go -- ...`, `bazel run //:gazelle projects/game`, `bazel build //...`, `bazel test //...`, and `testplan`/`guitar` for large tests.
- **Generated Files & Dependencies**: PASS. Proto source changes remain source-controlled; generated Go/TS proto outputs are not committed. No new JS dependency is planned; if implementation discovers a missing catalog dependency, it must be added through root `pnpm-workspace.yaml` and synchronized through Bazel/Pnpm workflow.
- **Testing Strategy**: PASS. Tests are planned before implementation: unit tests for checkpointer thread reuse/delete/history extraction/model selection, handler/proxy/gateway contracts, desktop bindings/view models, and UI state behavior. Service changes require large-test acceptance through the existing game testplan or documented infrastructure blocker.
- **Behavioral Acceptance**: PASS. Acceptance validates the real operator surface: desktop selects session, creates agent, enters play, sends messages, leaves/re-enters, loads history, resumes context, deletes/recreates agent, and verifies no old data leaks.
- **Review Scope**: PASS. Plan includes code quality review of service refactor, test-code review, TypeScript/Svelte style review, Go/proto/API style review, and UI visual QA after frontend changes.
- **Repository Verification**: PASS. Final validation includes targeted agent/desktop/gateway/proxy tests, formatting/gazelle as needed, relevant testplan run, then `bazel build //...` and `bazel test //...` unless a pre-existing infrastructure blocker is documented.
- **Testplan Execution**: PASS. This changes gRPC/HTTP service behavior and desktop flow, so large-test execution via `testplan` is required unless deployment infrastructure blocks it.
- **Test Impact Assessment**: PASS. Existing impacted tests are listed in this plan: `projects/game/agent/src/runtime.test.ts` deleted/replaced, `handler.test.ts`, `llm.test.ts`, `fake-llm.test.ts`, `server` wiring tests, `projects/game/desktop/view_model_test.go`, `app_test.go`, `internal/api/client_test.go`, gateway/proxy tests for new `ListMessages` endpoint, and game testplan cases.
- **Change Classification**: PASS. Changes are classified below; all modifications are scoped refactorings with preserved invariants.

## Project Structure

### Documentation (this feature)

```text
specs/009-agent-checkpoint-redesign/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── agent-messages-api.md
│   ├── checkpoint-agent-service.md
│   └── desktop-ui-state.md
└── tasks.md              # Created by /speckit.tasks, not /speckit.plan
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                              # Add history RPC/messages and any HTTP annotations needed by gateway/proxy path
├── agent/
│   ├── src/
│   │   ├── handler.ts                      # Refactor Map<sessionId, DialogRuntime> to metadata + checkpointed turn handling
│   │   ├── llm.ts                          # Use per-profile model, shared checkpointer, stable thread_id
│   │   ├── fake-llm.ts                     # Match new LLM adapter contract
│   │   ├── server.ts                       # Remove runtime cleanup interval; wire shared MemorySaver
│   │   ├── runtime.ts                      # Delete
│   │   ├── runtime.test.ts                 # Delete/replace with checkpoint-focused tests
│   │   ├── handler.test.ts                 # Update for metadata/history/delete semantics
│   │   ├── llm.test.ts                     # Update for model selection + checkpoint config
│   │   └── fake-llm.test.ts                # Update for new adapter contract
│   └── BUILD.bazel                         # Update generated/hand-written targets if source/test set changes
├── proxy/                                  # Forward new history RPC through existing agent proxy layer
├── gateway/                                # Expose REST GET ListMessages endpoint through gateway surface
├── desktop/
│   ├── app.go                              # Add ListMessages binding; ensure auto-connect on play entry path
│   ├── view_model.go                       # Add agentProfileName and Message view models
│   ├── internal/api/client.go              # Add ListMessages REST client method
│   └── frontend/src/
│       ├── api.ts                          # Add agentProfileName/Message types and wrapper
│       ├── App.svelte                      # State flow: metadata detail, connect on play entry, load history
│       └── components/
│           ├── SessionDetail.svelte        # State-driven redesign; no connect/profile selector once agent exists
│           ├── AgentSidebar.svelte         # Agent/profile/model display + view profile details
│           └── ChatView.svelte             # Loading/history population states as needed
└── testplan/
    ├── system_test.yaml                    # Add/adjust suites for checkpoint resume/history/delete behavior
    └── *_test.go                           # Add large-test cases through desktop/gateway/proxy/agent path
```

**Structure Decision**: Keep the existing multi-service layout. The feature cuts across agent runtime, API/proxy/gateway contract, and desktop UI, so implementation remains in existing service directories rather than adding a new service. The deleted `runtime.ts` is replaced by checkpoint-native behavior inside the agent service boundary; desktop state redesign stays inside the Wails/Svelte frontend and Wails Go bindings.

## Change Classification and Refactoring Scope

- **Delete**: `projects/game/agent/src/runtime.ts` and `runtime.test.ts` are removed because manual history/queue/cleanup duplicates native checkpoint behavior and is explicitly disallowed by the feature.
- **Modify / Refactor**: `handler.ts`, `llm.ts`, `server.ts`, and related tests are refactored from runtime-instance ownership to metadata + shared checkpoint ownership. Refactoring goal: make checkpoint state the only conversation state source. Preserved invariants: Create/Get/Delete/Connect RPC names and existing streaming frame behavior for text/thinking/wait/warn frames.
- **Modify / Refactor**: `game.proto`, proxy, gateway, desktop API/client/view model files gain a `ListMessages` retrieval path. Refactoring goal: expose checkpoint-backed messages through the existing request-response API style. Preserved invariants: existing session/agent/profile CRUD semantics and existing Connect stream behavior.
- **Modify / Refactor**: `App.svelte`, `SessionDetail.svelte`, `AgentSidebar.svelte`, `ChatView.svelte` are refactored into explicit UI states. Refactoring goal: remove contradictory controls and defer WebSocket connection to play entry. Preserved invariants: sessions page navigation, profile selection before agent creation, chat send/receive behavior.
- **New**: `Message` resource and `ListMessages` request/response messages, plus desktop `Message` view models; contract docs under `contracts/`; large-test cases for resume/messages/delete if missing.

## Test Impact Assessment

- `projects/game/agent/src/runtime.test.ts`: delete; replaced by checkpoint-focused tests proving stable `thread_id`, `MemorySaver` reuse, `deleteThread(sessionId)`, model selection by profile, same-session serialization, and `ListMessages` extraction (including native `message_id` provenance and `wait`-frame exclusion).
- `projects/game/agent/src/handler.test.ts`: update Create/Get/Delete/Connect/history expectations from runtime instance to metadata/checkpoint semantics.
- `projects/game/agent/src/llm.test.ts`: update adapter contract and verify per-profile model is used, not process default.
- `projects/game/agent/src/fake-llm.test.ts`: update fake adapter to new contract.
- `projects/game/agent/src/server.ts` tests or bootstrap tests: update to assert no cleanup interval/runtime map wiring remains and shared checkpointer is wired.
- `projects/game/desktop/view_model_test.go`: add `agentProfileName` and `Message` view-model conversion coverage.
- `projects/game/desktop/app_test.go` and `internal/api/client_test.go`: add ListMessages binding/client tests and verify play-entry auto-connect behavior where practical.
- Gateway/proxy tests: add contract tests for REST ListMessages endpoint mapping to the agent ListMessages RPC.
- `projects/game/testplan/*`: add or adjust system test cases for create/chat/re-enter/list-messages/resume/delete/recreate no-leak flow.

## Complexity Tracking

No constitution violations. The feature is cross-service, but that complexity is inherent because the user-visible behavior spans desktop UI, gateway/proxy API surface, and the agent service checkpoint state. A narrower single-service change would not satisfy `ListMessages` retrieval or desktop resume acceptance.

## Phase 0 Research Summary

See `research.md` for decisions. Key resolved decisions: use LangGraph.js `MemorySaver` as one process-wide checkpointer, use `sessionId` as `thread_id`, delete checkpoints with `checkpointer.deleteThread(sessionId)`, expose checkpoint state as `ListMessages` (with `Message` as a first-class resource addressed by the native `BaseMessage.id`) surfaced as REST GET, and connect WebSocket on play page entry rather than session detail entry.

## Phase 1 Design Summary

See `data-model.md` for entities/state transitions; `contracts/agent-messages-api.md` for the `ListMessages` / `Message` API contract; `contracts/checkpoint-agent-service.md` for agent service lifecycle/stream contracts; `contracts/desktop-ui-state.md` for desktop UI states; and `quickstart.md` for validation scenarios.

## Constitution Check (Post-Design Re-check)

- **Authority & Style**: PASS. Design artifacts reference the active spec and required style docs; implementation tasks must preserve this read-before-edit discipline.
- **Bazel Integrity**: PASS. All planned verification uses Bazel-managed commands and repository tools.
- **Generated Files & Dependencies**: PASS. Proto source changes are planned without committing generated code; no new dependency currently required.
- **Testing Strategy**: PASS. Unit, contract, UI, and large-test validation paths are explicit and mapped to changed files.
- **Behavioral Acceptance**: PASS. Quickstart validates through desktop/gateway/proxy/agent real surface.
- **Review Scope**: PASS. Review must include service refactor, UI/UX, test-code, and style.
- **Repository Verification**: PASS. Final whole-repo build/test included, with blockers documented if environment prevents completion.
- **Testplan Execution**: PASS. Large-test plan execution is required via `testplan` skill.
- **Test Impact Assessment**: PASS. Affected tests are explicitly listed above.
- **Change Classification**: PASS. Delete/modify/new scopes are classified and refactoring invariants are documented.
