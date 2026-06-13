# Implementation Plan: Dialog Agent with Chat Interface

**Branch**: `007-dialog-agent` | **Date**: 2026-06-09 | **Supplemented**: 2026-06-13 | **Spec**: `specs/007-dialog-agent/spec.md`

**Input**: Feature specification from `specs/007-dialog-agent/spec.md` plus planning constraints: remove the current Go agent service and related agent-only dependencies first; add a grpc-js/TypeScript agent service; use a test LLM substitute for large tests without sending real provider messages; do not add a new production service config for the substitute; tolerate missing provider secret files by using an empty secret; rewrite large tests into one YAML testplan with multiple minimal-deploy suites. **Supplement (2026-06-13)**: The original plan omitted User Story 3 (Agent Profile Management desktop UI). The backend gateway REST endpoints and prompt service gRPC implementation for profile CRUD are already complete; only the desktop Go client methods, Wails bindings, and Svelte UI components are missing.

## Summary

Replace the existing Go `projects/game/agent/` service with a Node.js/CommonJS TypeScript grpc-js implementation that preserves the current `AgentService` surface used by proxy/gateway while adding pure text dialog behavior backed by a LangChain agent abstraction. The new agent service copies profile prompt data at agent creation time, keeps per-session conversation history in memory, queues messages while processing, emits thinking and final response frames through the existing `AgentFrame` stream, and reads provider credentials from an optional mounted secret file. Large-test acceptance remains through the desktop/gateway/proxy/agent surface but uses a deterministic LLM substitute wired at the LLM module boundary, and the testplan is reorganized into one YAML with multiple suites that deploy only the services each suite needs.

**Supplement scope**: Add agent profile management to the desktop (User Story 3). The gateway REST surface (`POST/GET/DELETE /api/v1/prompts/agentProfiles[/{name}]`) and prompt service gRPC implementation are already complete. The work covers three layers: (1) Go HTTP client methods in `projects/game/desktop/internal/api/client.go` for `CreateAgentProfile`, `GetAgentProfile`, and `DeleteAgentProfile`; (2) Wails `App` method bindings in `projects/game/desktop/app.go` exposing those methods to the frontend; (3) new Svelte components — `ProfileManagement.svelte` — and a new `'profiles'` page state in `App.svelte` providing create, list, and delete UI.

## Technical Context

**Language/Version**: TypeScript 6.x targeting Node.js, ES2020, CommonJS module output, strict type checking; existing Go services remain Go under Bazel.

**Primary Dependencies**: Existing catalog entries `@grpc/grpc-js`, `@grpc/proto-loader`, `@types/node`, `typescript`, `vitest`, `@dominion/common-js-logs`, `@dominion/common-js-otel`, `@dominion/common-js-grpc-otel`, and `@dominion/common-js-grpc-resolver`; add LangChain/deepagents packages through the root `pnpm-workspace.yaml` catalog if not already present. The production LLM adapter depends on the chosen opencode-go/provider client module; tests inject a fake model/provider at the LLM module boundary.

**Storage**: Agent instances and conversation history are in-memory inside the stateful agent instance. Agent profiles remain in the existing prompt service MongoDB storage. Provider credentials are read from an optional mounted secret file and never persisted by the application.

**Testing**: Vitest-under-Bazel for the TypeScript agent service and LLM adapter; existing Go unit tests for session/proxy/gateway/prompt; Go large tests through `guitar`/testplan using `go_largetest`; `guitar validate` and `guitar run` for `projects/game/testplan/system_test.yaml` after rewrite.

**Target Platform**: Node.js service running in the dominion deployment platform behind the existing Go proxy/gateway and desktop client; not browser-compatible.

**Project Type**: Multi-service system: TypeScript grpc-js backend service, existing Go gRPC/HTTP services, Wails desktop UI, and repository testplan assets.

**Performance Goals**: A text conversation turn emits thinking and final response within one provider/model call latency under normal provider behavior; queued messages are processed in send order without concurrent mutation of session history; inactive agent instances are removed within 1 minute after the 15-minute inactivity threshold.

**Constraints**: Use Bazel for build/test and Bazel-managed PNPM only; dependency versions must live in root `pnpm-workspace.yaml` catalog; do not commit generated proto/grpc source; TypeScript BUILD targets are manual where Gazelle cannot express them; generated Go BUILD files remain Gazelle-owned; do not send large-test traffic to a real provider; missing provider secret file is allowed and yields an empty provider secret; the fake LLM/provider test substitute is added as an artifact to an existing test deploy composition, not as a new production service config.

**Scale/Scope**: Replace one agent service implementation and its packaging, update proto/type generation as needed, add dialog runtime modules, update desktop chat UI, preserve existing session/proxy/gateway/prompt services, and rewrite one game testplan YAML into multiple suites with minimal deploy configs. Supplement: add 3 Go client methods, 3 Wails bindings, 1 new Svelte component, and 1 page-state addition to the desktop for agent profile management (User Story 3).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Planning read `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/mongo.md`, `style/large_test.md`, and this plan. Implementation tasks must require executors to re-read these files plus relevant service files before code changes. TypeScript follows the Google TypeScript style reference and existing `common/js/*` / `experimental/ts/grpc_hello_world` patterns.
- **Bazel Integrity**: PASS. Build/test/package flows use Bazel: `bazel run @pnpm -- --dir /mnt/code/dominion ...`, `bazel run //:gazelle projects/game`, `bazel build //...`, `bazel test //...`, and `guitar` for large tests. TS BUILD files and `artifact_pkg_js` packaging are manual where Gazelle cannot express them.
- **Generated Files & Dependencies**: PASS. Proto sources remain source-controlled but generated Go/TS proto outputs are not committed. New JS dependency versions go through the root catalog and package manifests use `catalog:`/`workspace:*` as appropriate. Go dependency/build metadata updates use Gazelle and `bazel mod tidy` when required.
- **Testing Strategy**: PASS. Plan requires unit/contract tests before or alongside implementation for LLM adapter injection, optional secret reading, agent instance lifecycle, message queueing/history, grpc-js handler behavior, and desktop chat UI state. Service-level acceptance uses a large-test plan with fake LLM substitution.
- **Behavioral Acceptance**: PASS. Acceptance is through the real user surface: desktop/gateway WebSocket to proxy to grpc-js agent and prompt/profile HTTP APIs, not only direct module tests.
- **Review Scope**: PASS. Tasks must include test-code review for fake-provider/large-test behavior and TypeScript/JS style review for JS service code and packaging.
- **Repository Verification**: PASS. Final validation includes targeted package/service builds/tests, `guitar validate`, `guitar run` for the rewritten system testplan, then `bazel build //...` and `bazel test //...` unless a pre-existing blocker is documented.
- **Testplan Execution**: PASS. The feature changes service code, so large-test execution is required through the `testplan` skill. Skips are allowed only for deployment infrastructure blockers and must document residual risk.

**Post-Design Re-check**: PASS. Phase 0 and Phase 1 artifacts preserve Bazel-first JS packaging, optional secret behavior, fake LLM large-test isolation, real-surface acceptance, and one multi-suite testplan YAML with minimal suite deployments.

**Supplement Re-check (2026-06-13)**:

- **Authority & Style**: PASS. Supplement tasks require executors to re-read `style/README.md`, existing `app.go` and `client.go` patterns, and `contracts/desktop-profile-management.md` before code changes. New Go methods follow the existing `ListAgentProfiles` pattern in `client.go` and `app.go`. New Svelte components follow the existing `SessionDetail.svelte` pattern.
- **Bazel Integrity**: PASS. No new dependencies. Go methods use existing `net/http`, `protojson`, and `game` proto packages already in `BUILD.bazel`. Svelte components use existing frontend toolchain. `bazel run //:gazelle` handles any `BUILD.bazel` updates for new Go files.
- **Generated Files & Dependencies**: PASS. No new dependencies, no generated files, no proto changes. Reuses existing generated Go proto types and existing frontend packages.
- **Testing Strategy**: PASS. Unit tests for new Go client methods (`client_test.go` or `app_test.go` additions) following existing test patterns. Svelte component tests if the frontend test runner supports them. Large-test coverage for profile CRUD via the existing prompt/profile suite in `system_test.yaml`.
- **Behavioral Acceptance**: PASS. Profile management acceptance is through the desktop UI: create a profile, verify it appears in the list, delete it, verify it disappears. The large-test prompt/profile suite covers backend CRUD through the gateway.
- **Review Scope**: PASS. Tasks include code quality review for Go method consistency and Svelte component style review.
- **Repository Verification**: PASS. Final validation includes `bazel build //...` and `bazel test //...`.
- **Testplan Execution**: PASS. The existing prompt/profile suite in `system_test.yaml` already covers profile CRUD. Supplement does not change service code, but if profile management desktop testing is needed, the existing testplan suite applies.
- **Test Impact Assessment**: PASS. New Go client methods require new unit tests in `app_test.go` or `client_test.go`. Existing tests are not modified. The `view_model_test.go` may need new test cases for profile-related converters if not already covered. No existing large tests need modification.
- **Change Classification**: PASS. Changes are classified as: **new** (3 Go client methods, 3 Wails bindings, `ProfileManagement.svelte`, profile page state and handlers in `App.svelte`) and **modify** (extend `App.svelte` page-state machine and navigation to include the profiles page). The modification is a refactoring of the page router: scope is the `page` state variable and its conditional rendering; goal is to add a fourth page without disrupting existing flows; invariants preserved are the sessions/detail/chat page transitions and their handlers.

## Project Structure

### Documentation (this feature)

```text
specs/007-dialog-agent/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── agent-service.md
│   ├── desktop-chat-ui.md
│   └── testplan.md
└── tasks.md              # Created later by /speckit.tasks
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                         # AgentFrame/AgentService contract updates if needed
├── agent/                             # Replace Go implementation with TypeScript grpc-js service
│   ├── BUILD.bazel                    # ts_project, ts_proto_library, artifact_pkg_js, artifact_image
│   ├── package.json                   # @dominion/game-agent, catalog/workspace deps
│   ├── tsconfig.json
│   ├── .swcrc
│   ├── run_vitest.mjs
│   ├── service.yaml                   # keep service name `agent`, stateful grpc port 50051
│   └── src/
│       ├── bootstrap.ts               # OTel/log init before grpc-js load, graceful shutdown
│       ├── server.ts                  # grpc-js server startup, proto-loader registration
│       ├── handler.ts                 # AgentService handlers
│       ├── runtime.ts                 # per-session dialog runtime, queue, history, cleanup
│       ├── prompt-client.ts           # prompt service grpc-js client adapter
│       ├── llm.ts                     # real LLM adapter boundary
│       ├── fake-llm.ts                # test/large-test deterministic substitute module
│       ├── secrets.ts                 # optional provider secret file reader
│       └── *.test.ts
├── desktop/
│   ├── app.go                         # preserve Wails backend, expose chat helpers as needed;
│   │                                  # SUPPLEMENT: add CreateAgentProfile, GetAgentProfile,
│   │                                  # DeleteAgentProfile Wails bindings
│   ├── internal/api/client.go         # SUPPLEMENT: add CreateAgentProfile, GetAgentProfile,
│   │                                  # DeleteAgentProfile Go HTTP client methods
│   └── frontend/src/
│       ├── App.svelte                 # SUPPLEMENT: add 'profiles' page state, navigation,
│       │                              # profile management handlers
│       ├── api.ts
│       └── components/                # add chat dialog/sidebar components;
│           ├── ProfileManagement.svelte  # SUPPLEMENT: profile list + create form + delete
│           └── ...                     # existing chat components
└── testplan/
    ├── system_test.yaml               # one YAML, multiple suites
    ├── deploy_session.yaml            # session/proxy/gateway regression subset as needed
    ├── deploy_prompt.yaml             # prompt/gateway/mongo only
    ├── deploy_agent.yaml              # session/proxy/agent/gateway/mongo + fake LLM artifact/env
    ├── deploy_full.yaml               # full dialog surface when needed
    ├── *_test.go                      # suite-specific Go large tests
    └── BUILD.bazel                    # go_largetest targets per suite

common/js/grpc/resolver/               # consume dominion grpc-js resolver for service discovery
common/js/logs/, common/js/otel/, common/js/grpc/otel/
                                        # consume existing JS observability packages
projects/game/session/, proxy/, gateway/, prompt/
                                        # preserve existing Go services, update only integration points
```

**Structure Decision**: Keep the deploy service name and path `projects/game/agent/service.yaml` so proxy/deploy references remain stable, but replace the Go implementation below `projects/game/agent/` with a TypeScript grpc-js package following `experimental/ts/grpc_hello_world` and `common/js/*` patterns. This satisfies the user instruction to remove the old agent service first while avoiding a new service identity. Test-only fake behavior belongs in testplan/deploy wiring or an agent artifact mode, not in a new production service config.

**Supplement Structure Decision**: The profile management supplement adds code only within `projects/game/desktop/` — no new service, no proto changes, no new external dependencies. The Go client methods and Wails bindings extend existing files (`client.go`, `app.go`) following the `ListAgentProfiles` pattern already established there. The new `ProfileManagement.svelte` component follows the prop-driven pattern of `SessionDetail.svelte` and `AgentSidebar.svelte`, receiving data and callbacks from `App.svelte`. The page-state machine in `App.svelte` gains one new value (`'profiles'`) alongside the existing `'sessions' | 'detail' | 'chat'` states.

## Complexity Tracking

No constitution violations. The main complexity is cross-runtime migration: the service implementation changes from Go to TypeScript while preserving the existing Go proxy/gateway/session/prompt contracts and adding fake LLM acceptance without real provider traffic.
