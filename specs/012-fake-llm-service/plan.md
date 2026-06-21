# Implementation Plan: Fake LLM Service for Large-Test Integration

**Branch**: `012-fake-llm-service` | **Date**: 2026-06-17 | **Updated**: 2026-06-18 (core matching model redesigned to stateless per-request; agent-side wiring changed to resolver-aware provider via agent_test artifact — Option B) | **Spec**: `specs/012-fake-llm-service/spec.md`

**Input**: Feature specification from `specs/012-fake-llm-service/spec.md`

## Summary

Replace the agent service's in-process `FakeLlmAdapter` test path with a standalone Go `fake-llm` HTTP service that implements the OpenAI Chat Completions API. Large tests will deploy the `agent_test` artifact (a narrow test-only artifact with a resolver-aware provider) alongside fake-llm. The resolver-aware provider resolves the fixed dominion target `dominion:///game/fake-llm:8080` via the core resolver (`createResolver()` from `@dominion/common-js-resolver`) to obtain the pod IP, then builds the base URL for the real `ChatOpenAI`. The real LangChain pipeline (`createAgent` → `ChatOpenAI` → HTTP/SSE → `streamEvents` → content-block extraction → WebSocket frames) runs unchanged. The fake service loads JSON/YAML response messages at startup into a flat, alphabetically-sorted set and uses a stateless per-request matching model (any-keyword substring match of the last user message, alphabetical-by-name tiebreak on multi-match, random fallback when no keyword matches) to return deterministic reasoning/text templates.

## Technical Context

**Language/Version**: Go for the new `projects/game/fake-llm` HTTP service, deployment/testplan updates, and large tests; TypeScript 6.x/ES2020 CommonJS for the existing `projects/game/agent` extraction change.

**Primary Dependencies**: Existing repository Go HTTP/service stack and Bazel `rules_go`; `gopkg.in/yaml.v3` or existing YAML parser if already available; standard `encoding/json`; existing TypeScript `@langchain/openai`, `@langchain/core`, `langchain`, and `@langchain/langgraph`; existing release rules `artifact_pkg_go` and `artifact_image`.

**Storage**: No durable storage. Fake service loads response message files at startup into a read-only merged set; it holds NO per-request mutable state (no active group, no position counter). Matching is stateless per request.

**Testing**: Go unit tests for fake service parsing, matching, streaming, and non-streaming responses; TypeScript Vitest-under-Bazel for `AgentAdapterImpl` reasoning-content extraction; Go large tests through `projects/game/testplan/system_test.yaml` using the `testplan` skill; final `bazel build //...` and `bazel test //...`.

**Target Platform**: Linux-hosted game backend/test infrastructure deployed by repository release/testplan tooling.

**Project Type**: Multi-service game system. This feature adds one Go HTTP service, modifies the TypeScript agent service only at the generic reasoning extraction boundary, and rewrites large-test deployment/test assertions.

**Performance Goals**: Fake service responses should stream immediately after receiving a request; deterministic multi-turn tests must preserve response order in 100% of agent-related large-test suites. No throughput/SLO target beyond large-test reliability is required.

**Constraints**: Use Bazel wrappers for Go/TypeScript build and test commands. Keep generated `BUILD.bazel` ownership with Gazelle except release targets that require manual artifact rules. Do not add fake-specific provider/model branches to production agent source. The resolver-aware provider lives in the test-only `bootstrap-test.ts` entrypoint. The `agent_test` artifact is used for large tests; the standard `agent` artifact is used in production. Response data loads once at boot; no runtime reload, auth, rate limiting, persistence, Anthropic wire format, tool-call simulation, or multimodal support.

**Scale/Scope**: Source changes span `projects/game/fake-llm/`, `projects/game/agent/src/llm.ts`, `projects/game/agent/src/fake-llm.ts`, `projects/game/agent/src/fake-llm.test.ts`, `projects/game/agent/src/bootstrap-test.ts` (rewritten, not deleted), `projects/game/agent/BUILD.bazel` (add resolver dep; retain test targets), `projects/game/agent/service.yaml` (retain both `agent` and `agent_test`), `projects/game/testplan/`, and Bazel module/dependency files if resolver or YAML dependencies change.

**References**: OpenAI-compatible response shape and SSE conventions follow the OpenAI Chat Completions API reference: https://platform.openai.com/docs/api-reference/chat/create. LangChain/OpenAI reasoning-content behavior follows the validated spec references in `specs/012-fake-llm-service/spec.md` and upstream issues: https://github.com/langchain-ai/langchain/issues/34706 and https://github.com/langchain-ai/langchain/pull/34705.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Plan is based on `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`, the active spec, and existing `projects/game` service/testplan patterns. Implementation tasks must require executors to re-read these files before code changes.
- **Bazel Integrity**: PASS. Planned work uses `bazel run //:go -- fmt`, `bazel run //:gazelle`, `bazel mod tidy` when Go dependencies change, Bazel-managed TypeScript tests, `bazel build //...`, and `bazel test //...`. No direct ecosystem build/test commands are planned.
- **Generated Files & Dependencies**: PASS. New Go `BUILD.bazel` files are Gazelle-owned except manually required `artifact_pkg_go`/`artifact_image` release targets. No generated proto/grpc source is committed. Any YAML dependency addition must update Go/Bazel dependency state through repository commands.
- **Testing Strategy**: PASS. Unit/adapter tests and large-test cases are planned before implementation. Service behavior is validated through the real HTTP/WebSocket surfaces and the game system testplan.
- **Behavioral Acceptance**: PASS. Acceptance drives the fake service through OpenAI-format HTTP requests and drives the agent through gateway WebSocket frames, proving the real `ChatOpenAI`/`streamEvents` pipeline.
- **Review Scope**: PASS. Review covers Go service code, response-template data, agent extraction change, removed fake adapter/test artifacts, deployment YAML, testplan docs, large tests, Bazel metadata, and style.
- **Repository Verification**: PASS. Final validation includes targeted fake-service tests, targeted agent tests, affected large tests through `testplan`, `bazel build //...`, and `bazel test //...` unless infrastructure blockers are documented.
- **Testplan Execution**: PASS. Game system testplan execution through the `testplan` skill is required because this is service/test-infrastructure behavior.
- **Test Impact Assessment**: PASS. Affected tests are listed below with expected changes.
- **Change Classification**: PASS. New/modify/delete scopes and preserved invariants are documented below.
- **Citation Links**: PASS. External API and LangChain behavior references include URLs; internal repository references use relative paths.

## Project Structure

### Documentation (this feature)

```text
specs/012-fake-llm-service/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── fake-llm-api.md
│   └── large-test-deployment.md
└── tasks.md              # Created by /speckit.tasks, not /speckit.plan
```

### Source Code (repository root)

```text
projects/game/
├── fake-llm/
│   ├── BUILD.bazel                    # Gazelle + manual artifact targets
│   ├── service.yaml                   # fake-llm artifact metadata
│   ├── cmd/                           # service entrypoint
│   ├── app/                           # HTTP server wiring
│   ├── handler/                       # OpenAI-compatible HTTP handler
│   ├── service/                       # template selection and response streaming
│   └── testdata/                      # JSON/YAML response template examples
├── agent/
│   ├── BUILD.bazel                    # add resolver dep; retain server_pkg_test/cmd_image_test/agent_test
│   ├── service.yaml                   # expose both agent (production) and agent_test (test, resolver-aware)
│   └── src/
│       ├── llm.ts                     # extract additional_kwargs.reasoning_content
│       ├── fake-llm.ts                # delete (coverage bypass removed)
│       ├── fake-llm.test.ts           # delete (coverage bypass removed)
│       └── bootstrap-test.ts          # rewrite with resolver-aware provider (FR-025)
└── testplan/
    ├── README.md                      # fake service topology/config guide
    ├── deploy_agent.yaml              # add fake-llm, use agent_test (resolver-aware) + dummy provider secret
    ├── system_test.yaml               # update descriptions and suite expectations
    ├── helpers_test.go                # read configured response expectations
    ├── agent_dialog_test.go           # assert configured reasoning/text through WS
    ├── agent_lifecycle_test.go        # agent_test artifact + fake service deployment behavior
    ├── agent_checkpoint_test.go       # checkpoint assertions through real pipeline
    └── BUILD.bazel                    # update only if testdata/source sets change
```

**Structure Decision**: Add `projects/game/fake-llm` as a normal game service so large tests deploy it exactly like session/proxy/prompt/gateway services. Keep the production agent service unchanged except for generic reasoning extraction. The `agent_test` artifact (resolver-aware) is the test entrypoint; the `FakeLlmAdapter` coverage bypass is deleted.

## Change Classification and Refactoring Scope

### New: Fake LLM Go service

- **New**: `projects/game/fake-llm` service with OpenAI-compatible `POST /v1/chat/completions` HTTP endpoint, streaming SSE and non-streaming JSON response modes, flat response-message loading with multi-file merge + alphabetical sort + startup validation, stateless per-request keyword matching (no active-group/position state), health/startup logging, unit tests, `service.yaml`, `artifact_pkg_go`, and `artifact_image`.
- **Preserved invariants**: The service is test-only by deployment topology, not by fake-specific branches in agent source. It accepts any bearer token and model name.

### Modify / Refactor: Agent service

- **Modify / Refactor**: `projects/game/agent/src/llm.ts` content extraction. Refactoring goal: preserve LangChain-native text extraction while adding generic support for `AIMessageChunk.additional_kwargs.reasoning_content` before text blocks. Preserved invariants: existing `streamEvents` use, `ContentBlock` union shape, provider initialization, middleware, and checkpointing behavior.
- **Delete**: `projects/game/agent/src/fake-llm.ts`, `fake-llm.test.ts`. Coverage-bypassing fake adapter is removed.
- **Rewrite (not delete)**: `projects/game/agent/src/bootstrap-test.ts` to inject a resolver-aware provider (core resolver from `@dominion/common-js-resolver` resolving the fixed target `dominion:///game/fake-llm:8080`) feeding the real `AgentAdapterImpl`. The test bootstrap is the ONLY difference from the production artifact.
- **Modify / Refactor**: `projects/game/agent/BUILD.bazel` — add `:node_modules/@dominion/common-js-resolver` to `lib` ts_project deps and `//common/js/resolver:runtime_pkg` to `server_pkg_test`'s runtime_deps. Retain `server_pkg_test`, `cmd_image_test`, `agent_test` targets. **Do NOT delete test artifact targets**.
- **Modify / Refactor**: `projects/game/agent/service.yaml` must expose both `agent` (production) and `agent_test` (test, resolver-aware) artifacts. Preserved invariant: standard `agent` artifact still builds from `src/bootstrap.js` and keeps provider secret requirement.

### Modify / Refactor: Test deployment and large tests

- **Modify / Refactor**: `projects/game/testplan/deploy_agent.yaml` adds fake-llm service, uses `agent_test` artifact (resolver-aware), and provides a dummy `provider` secret so `readSecret` succeeds. No `OPENCODE_BASE_URL` environment variable is set — the resolver-aware provider reaches fake-llm by resolving the fixed target `dominion:///game/fake-llm:8080`. Preserved invariants: same public gateway endpoint and existing session/proxy/prompt/gateway topology.
- **Modify**: `projects/game/testplan/system_test.yaml` suite descriptions reference fake service plus `agent_test` instead of `agent(test)`.
- **Modify / Refactor**: `agent_dialog_test.go`, `agent_lifecycle_test.go`, `agent_checkpoint_test.go`, and helpers assert configured response templates, thinking-before-text ordering, independent per-turn keyword matching (stateless: same keyword returns the same message each turn; distinct keywords return different configured messages), checkpoint visibility, and continued per-profile/FIFO behavior through the real agent pipeline.
- **New**: `projects/game/testplan/README.md` documents topology, data-file format, matching model, resolver-based discovery (fixed target resolved via core resolver), and assertion patterns.

## Test Impact Assessment

- `projects/game/fake-llm/...`: add unit tests for JSON/YAML parsing, malformed startup errors, empty/missing `match_keywords` startup failure, duplicate-name startup failure, stateless per-request matching, alphabetical-by-name tiebreak on multi-match, random fallback on no-match (with WARN log), streaming SSE chunks with `reasoning_content` before `content`, non-streaming response (including `reasoning_content`), model/API-key ignore behavior.
- `projects/game/agent/src/llm.test.ts` or replacement extraction test: add coverage for `additional_kwargs.reasoning_content` yielding a reasoning block before text content; keep native reasoning `contentBlocks` fallback behavior.
- `projects/game/agent/src/fake-llm.test.ts`: delete with `FakeLlmAdapter`; no weakened behavior, because fake behavior moves to Go fake-service tests and large tests.
- `projects/game/testplan/agent_dialog_test.go`: replace hardcoded `FakeLlmAdapter` strings with template-backed expected reasoning/text and assert WebSocket frame order.
- `projects/game/testplan/agent_lifecycle_test.go`: verify `agent_test` artifact can connect/send via fake service through resolver-aware provider, and preserve connection/profile lifecycle checks.
- `projects/game/testplan/agent_checkpoint_test.go`: verify ListMessages/checkpoint behavior reflects real `createAgent` pipeline output and multi-turn independent matching behavior.
- `projects/game/testplan/helpers_test.go`: update deployment/test helpers to create profiles pointing at OpenAI-compatible provider/model values and expected response templates.
- `projects/game/testplan/system_test.yaml`: update suite descriptions and ensure agent-related suites run against `deploy_agent.yaml` with fake service.

## Complexity Tracking

No constitution violations. The feature adds a standalone fake service, but this is the simpler alternative compared with adding fake-only branches to the production agent code: the fake service uses an external OpenAI-compatible boundary and therefore increases end-to-end coverage while reducing in-process test harness complexity.

## Phase 0 Research Summary

See `research.md` for decisions. Key decisions: implement only OpenAI Chat Completions for this iteration; use stateless per-request matching (no active group or position state) with any-keyword substring matching of the last user message, alphabetical-by-name tiebreak on multi-match, and random fallback on no-match; load flat response message templates once at startup from JSON/YAML (multi-file merge, unique names, non-empty `match_keywords`); stream reasoning via `delta.reasoning_content` and text via `delta.content`; preserve `additional_kwargs.reasoning_content` in `AgentAdapterImpl`; deploy `agent_test` artifact (resolver-aware) that resolves the fixed target `dominion:///game/fake-llm:8080` via the core resolver (`@dominion/common-js-resolver`), NOT via `OPENCODE_BASE_URL`; dummy provider secret; test profiles avoid `minimax-`/`qwen3.` model prefixes; remove `FakeLlmAdapter` only (retain `agent_test`, `server_pkg_test`, `cmd_image_test`).

## Phase 1 Design Summary

See `data-model.md` for entity definitions and state transitions; `contracts/fake-llm-api.md` for the OpenAI-compatible fake-service API; `contracts/large-test-deployment.md` for deployment/testplan contracts; and `quickstart.md` for validation scenarios.

## Constitution Check (Post-Design Re-check)

- **Authority & Style**: PASS. Design artifacts preserve the read-before-edit requirements and reference active style docs.
- **Bazel Integrity**: PASS. Quickstart and plan use Bazel wrappers, Gazelle, Bazel module sync, and the `testplan` skill.
- **Generated Files & Dependencies**: PASS. New Go build metadata is Gazelle-managed with manual release targets only where existing service patterns require them.
- **Testing Strategy**: PASS. Unit, adapter extraction, deployment, and large-test validation paths are explicit and mapped to files.
- **Behavioral Acceptance**: PASS. Validation uses OpenAI-format HTTP and gateway WebSocket surfaces through the real agent pipeline.
- **Review Scope**: PASS. Review includes fake service, agent extraction, artifact removal, deployment, docs, tests, and style.
- **Repository Verification**: PASS. Targeted tests, testplan execution, `bazel build //...`, and `bazel test //...` are included.
- **Testplan Execution**: PASS. Game system testplan execution is required unless infrastructure blockers are recorded.
- **Test Impact Assessment**: PASS. Affected unit and large-test files are explicitly listed.
- **Change Classification**: PASS. New/modify/delete scopes and preserved invariants are documented.
- **Citation Links**: PASS. OpenAI and LangChain external references include URLs.

## References

- OpenAI Chat Completions API — https://platform.openai.com/docs/api-reference/chat/create
- OpenAI streaming guide — https://platform.openai.com/docs/api-reference/streaming
- LangChain Python issue confirming reasoning-content preservation gap — https://github.com/langchain-ai/langchain/issues/34706
- LangChain Python PR preserving `reasoning_content` in `additional_kwargs` — https://github.com/langchain-ai/langchain/pull/34705
- Repository prototype evidence — `experimental/openai_llm/testplan/interface_test.yaml` and `experimental/openai_llm/testplan/interface_test.go`
- Current agent fake adapter path — `projects/game/agent/src/fake-llm.ts`, `projects/game/agent/src/bootstrap-test.ts`, `projects/game/agent/BUILD.bazel`
- Current test deployment path — `projects/game/testplan/deploy_agent.yaml`, `projects/game/testplan/system_test.yaml`
