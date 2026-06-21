# Feature Specification: Fake LLM Service for Large-Test Integration

**Feature Branch**: `012-fake-llm-service`

**Created**: 2026-06-16

**Updated**: 2026-06-18

**Status**: Draft

**Input**: User description: "将测试替身方案从 agent 内的 fake-llm 模块改为独立的 fake LLM HTTP 服务（OpenAI 格式），使大型测试覆盖 agent 的完整执行链路（createAgent、middleware、streamEvents、contentBlock 提取、provider 初始化）。fake 服务支持 JSON/YAML 配置响应模板，采用无状态单次请求匹配模型：每次请求独立匹配最后一条 user 消息的关键词，无活动组/位置计数器，无跨请求状态。" 2026-06-17 更新：已通过 experimental/openai_llm 原型验证 `@langchain/openai` 对 `reasoning_content` 的实际解析行为。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Full Agent Pipeline Under Large Test (Priority: P1)

A test engineer runs the game system testplan. The deployment includes a fake LLM HTTP service alongside the `agent_test` artifact (a narrow test-only agent artifact, not the standard production `agent` artifact). The `agent_test` artifact uses a resolver-aware provider in its test bootstrap: it resolves the fixed dominion target `dominion:///game/fake-llm:8080` via the core dominion resolver (`createResolver()` from `@dominion/common-js-resolver` at `common/js/resolver`), obtains the current pod IP, and builds the base URL for the real `ChatOpenAI`. When the test sends a text frame through the gateway WebSocket, the entire real code path executes: handler → SessionAgentStore → SessionAgent → AgentAdapterImpl → createAgent → ChatOpenAI → HTTP request to fake service → SSE streaming response → contentBlock extraction → frames written back to WebSocket. Only the provider's network-discovery layer differs from production; the real pipeline runs unchanged.

The fake service uses a stateless per-request matching model: each request independently matches keywords against the text of the last `role: "user"` message. There is no active group, no position counter, and no cross-request state. When multiple messages match, the alphabetically-first-by-name message is returned. When no message matches, a uniformly random message is returned from the full set. Responses contain streaming SSE chunks with `reasoning_content` (for thinking blocks) and `content` (for text blocks).

**Why this priority**: This is the core architectural goal. The current `FakeLlmAdapter` bypasses `createAgent`, all middleware, `streamEvents`, contentBlock extraction, and provider initialization — meaning large tests provide zero integration coverage for the spec-011 refactor. Without this, the LangChain migration has no end-to-end safety net.

**Independent Test**: Deploy the SUT with the fake LLM service and `agent_test`, connect via WebSocket, send a text frame, and verify that thinking and text frames are received. The test exercises the real `AgentAdapterImpl` — if any part of the `createAgent` → `streamEvents` → contentBlock pipeline is broken, no frames are returned.

**Acceptance Scenarios**:

1. **Given** a deployed SUT with the fake LLM service and `agent_test` (resolver-aware), **When** the test connects to a session WebSocket and sends a text frame with a profile name, **Then** the agent service initializes the real `ChatOpenAI` model (not a fake adapter), the resolver-aware provider resolves the fixed fake-llm target and routes the model call to it, and the agent returns thinking + text frames through the real streaming pipeline.
2. **Given** a multi-turn conversation in the test, **When** each turn sends a request whose last user message contains one of the configured keywords, **Then** the fake service matches independently per turn (stateless), and the agent's checkpoint-backed history accumulates correctly across turns.
3. **Given** a request whose last user message matches keywords from multiple configured messages, **When** the request is sent, **Then** the alphabetically-first-by-name message is returned. **Given** a request whose last user message matches no keyword, **When** sent, **Then** a random message from the full merged set is returned (only for don't-care scenarios).
4. **Given** the full pipeline, **When** the agent processes the SSE stream, **Then** reasoning content from `delta.reasoning_content` is preserved by `@langchain/openai` in `AIMessageChunk.additional_kwargs.reasoning_content`, extracted by `AgentAdapterImpl`, and emitted as a thinking frame before the text frame from `delta.content`.

---

### User Story 2 - Configurable Response Templates via JSON/YAML (Priority: P1)

A test author configures the fake LLM service's response behavior by editing JSON or YAML data files, not by modifying service code. Each file contains a flat list of messages (no `groups` wrapper). Each message has a `name` (globally unique), `match_keywords` (non-empty array of strings), `reasoning` (may be empty), and `text` (may be empty). Multiple data files are merged into one alphabetically-sorted-by-name set at startup.

Matching is stateless per request: the service extracts the last `role: "user"` message text, checks if any keyword is a case-insensitive substring of that text, and returns the alphabetically-first matching message. If no message matches, a uniformly random message is returned from the full set. Tests requiring explicit content must always match a keyword or add new data to the files. The random fallback logs a WARN with the unmatched snippet and chosen name for diagnosability.

**Why this priority**: Configuration-driven test data is essential for maintainability. Hardcoded response templates in Go source would recreate the rigidity problem that the current `FakeLlmAdapter` has. Without external data files, every test scenario change requires a code change and rebuild.

**Independent Test**: Create a JSON/YAML data file with two messages: one with `match_keywords: ["hello"]` and another with `match_keywords: ["hi"]`, deploy the fake service, send a request containing "hello", and verify the hello-matching message is returned.

**Acceptance Scenarios**:

1. **Given** a data file with a message containing `match_keywords: ["hello", "greeting"]` and configured `reasoning`/`text`, **When** a request whose last user message contains "hello" is sent, **Then** the message's configured `reasoning_content` and `content` are returned.
2. **Given** two messages — `A-hello` with `match_keywords: ["hello"]` and `B-hello` with `match_keywords: ["hello"]`, **When** a request containing "hello" is sent, **Then** `A-hello` is returned (alphabetically first by name).
3. **Given** a data file with a message that has empty `match_keywords`, **When** the service starts, **Then** startup fails with a clear error naming the file and field.
4. **Given** a data file with messages whose keywords do not match the request, **When** the request is sent, **Then** a random message from the full set is returned, and a WARN log line records the unmatched snippet and chosen name.
5. **Given** the data file is loaded at service startup, **When** the service receives requests across multiple turns, **Then** the configured templates are used without requiring restart mid-test (data is loaded once at boot).

---

### User Story 3 - Remove In-Process Fake Adapter (Coverage Bypass), Retain Resolver-Aware Test Artifact (Priority: P1)

The test engineer removes the in-process `FakeLlmAdapter` (`fake-llm.ts`, `fake-llm.test.ts`). The `bootstrap-test.ts` is NOT deleted — it is rewritten to inject a resolver-aware provider feeding the real `AgentAdapterImpl`. The test-only Bazel artifact targets (`server_pkg_test`, `cmd_image_test`, `agent_test`) are RETAINED — the test artifact now exercises the real pipeline with only a resolver-aware provider swap. Large tests use the `agent_test` artifact (resolver-aware), not the standard production `agent` artifact. The key deletion is the `FakeLlmAdapter` coverage bypass, not the test artifact.

**Why this priority**: The `FakeLlmAdapter` bypassed `createAgent`/middleware/streaming/contentBlock — that is the coverage gap that must be eliminated. The retained `agent_test` artifact is fundamentally different: it runs the real pipeline with only the network-discovery layer swapped, so the end-to-end safety net for the spec-011 LangChain migration is preserved.

**Independent Test**: Verify that no source file under `projects/game/agent/src/` references the `FakeLlmAdapter` class/import, that `BUILD.bazel` retains `server_pkg`, `server_pkg_test`, `cmd_image`, `cmd_image_test`, and that `deploy_agent.yaml` references the `agent_test` artifact.

**Acceptance Scenarios**:

1. **Given** the agent service codebase after removal, **When** a developer searches for `FakeLlmAdapter`, **Then** no production or test source file imports it.
2. **Given** the agent `BUILD.bazel` after update, **When** a developer inspects artifact targets, **Then** `server_pkg`, `server_pkg_test`, `cmd_image`, and `cmd_image_test` all exist — the test targets are retained for the resolver-aware artifact.
3. **Given** `deploy_agent.yaml` after update, **When** the testplan deploys the agent, **Then** it uses the `agent_test` artifact with the resolver-aware provider reaching fake-llm via the fixed target `dominion:///game/fake-llm:8080`.

---

### User Story 4 - Testplan Documentation and Test Rewrite (Priority: P2)

The testplan directory gains a README documenting the fake LLM service: how it works, how to configure response templates, how the agent is pointed at it in deployment, and how tests assert on response content via independent per-turn matching. The existing large-test cases (`agent_dialog_test.go`, `agent_lifecycle_test.go`, `agent_checkpoint_test.go`) are rewritten to use the new deployment topology and assert against the fake service's configured response templates instead of the old hardcoded `FakeLlmAdapter` strings.

**Why this priority**: Documentation ensures the testing approach is reproducible and maintainable. Test rewrites are needed because the old assertions checked strings baked into Go source (`"Processing your message..."`, `"Hello! This is a simulated response..."`) — these must be updated to reference the fake service's data-file-defined templates.

**Independent Test**: Run the game system testplan with the `testplan` skill and verify all agent-related suites pass against the fake LLM service.

**Acceptance Scenarios**:

1. **Given** the testplan README, **When** a new developer reads it, **Then** they understand how to deploy the fake service, configure response templates, and write new tests that assert against configured content.
2. **Given** the rewritten `agent_dialog_test.go`, **When** the dialog suite runs, **Then** thinking-before-text ordering is verified, response content matches the fake service's configured template, and FIFO multi-message ordering holds.
3. **Given** the rewritten `agent_checkpoint_test.go`, **When** the checkpoint suite runs, **Then** ListMessages returns checkpoint-backed messages from the real `createAgent` pipeline (not from a hand-written StateGraph).

---

### Edge Cases

- What happens when multiple messages' keywords match a single request? The message whose `name` sorts first alphabetically across the merged set is returned. This is a within-request deterministic tiebreak, not a cross-turn "first match."
- What happens when no message's keywords match the request? A uniformly random message from the full merged set is returned. The service logs a WARN line with the unmatched user-message snippet and the chosen message name. This random fallback is legitimate only for test scenarios that do not assert on response content.
- What happens when a message has empty or missing `match_keywords`? The service refuses to start with a clear error naming the file and field. Every message must have at least one non-empty keyword.
- What happens when two data files define a message with the same `name`? The service refuses to start with a clear error naming the conflicting names and files. Message names must be globally unique.
- What happens when the agent sends a non-streaming request? The fake service handles both `stream: true` (SSE chunks) and `stream: false` (single JSON response) modes. Non-streaming responses include `reasoning_content` in the message object.
- What happens when the fake service's data file is malformed? The service fails at startup with a clear error message identifying the file and the parse error.
- What happens when the agent sends the `model` field in the request? The fake service ignores the model name entirely — it accepts any model and returns from its configured messages. This allows test profiles to use any model name.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a standalone HTTP service (`fake-llm`) under `projects/game/` that implements the OpenAI Chat Completions API endpoint (`POST /v1/chat/completions`) with both streaming (`stream: true`, SSE) and non-streaming (`stream: false`) response modes.
- **FR-002**: The fake LLM service MUST emit streaming responses containing `delta.reasoning_content` for reasoning/thinking blocks and `delta.content` for text blocks. The agent's `AgentAdapterImpl` MUST extract reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (fallback to native `contentBlocks` of type `reasoning` if a future `@langchain/openai` version provides them) and text content from `contentBlocks` of type `text`, so that thinking frames and text frames are produced through the real streaming pipeline.
- **FR-003**: The fake LLM service MUST load response template data from external JSON or YAML files at startup, allowing test authors to define message templates without modifying service source code.
- **FR-004**: Each message in the data file MUST specify: a globally unique `name` (string), a non-empty array of keyword strings (`match_keywords`), `reasoning` text (may be empty), and `text` (may be empty).
- **FR-005**: The fake LLM service MUST use a stateless per-request matching model. The service MUST NOT maintain any active group or position state. Every request is matched independently against the last user message.
- **FR-006**: Matching MUST use ONLY the text of the last `role: "user"` message (case-insensitive role). Historical/context messages MUST NOT be considered. A message matches if ANY of its `match_keywords` is a case-insensitive substring of that text.
- **FR-007**: When one or more messages match a single request, the service MUST return the message whose `name` sorts first alphabetically across the merged set. This is a within-request deterministic tiebreak, not a cross-turn concept.
- **FR-008**: When ZERO messages match, the service MUST return a uniformly random message from the full merged set. The random fallback MUST log a WARN-level line including the unmatched user-message snippet and the randomly-selected message name (e.g., `no keyword matched for "...", returning random message "<name>"`). The random selection MUST use `math/rand/v2` (or explicitly seed the source) to avoid the Go global-source zero-seed determinism bug.
- **FR-009**: Every message's `match_keywords` array MUST be non-empty, with every element being a non-empty string. Empty or missing `match_keywords` MUST cause startup failure with a clear error naming the file and field.
- **FR-010**: Every message's `name` MUST be globally unique across all merged data files. Duplicate names MUST cause startup failure with a clear error naming the conflicting names and files.
- **FR-011**: Multiple data files MUST be merged into one flat set, sorted alphabetically by `name`. This sort order is the deterministic tiebreak used when multiple messages match a single request.
- **FR-012**: Non-streaming response messages MUST include `reasoning_content` in the message object, mirroring streaming semantics (not only `content`).
- **FR-013**: The fake LLM service MUST accept any API key (Bearer token) and return HTTP 200 — no authentication is enforced.
- **FR-014**: The fake LLM service MUST ignore the `model` field in requests — any model name is accepted and the same matching logic applies.
- **FR-015**: The system MUST remove the coverage-bypassing in-process `FakeLlmAdapter` (`fake-llm.ts`, `fake-llm.test.ts`). The test bootstrap `bootstrap-test.ts` is NOT deleted — it is REWRITTEN to inject a resolver-aware provider (FR-025) feeding the real `AgentAdapterImpl`. The test-only Bazel artifact targets `server_pkg_test` and `cmd_image_test` and the `agent_test` artifact entry ARE RETAINED (the test artifact now exercises the real pipeline).
- **FR-016**: The `agent_test` artifact is RETAINED as a narrow test-only artifact. It differs from the standard `agent` artifact ONLY by the test bootstrap (`bootstrap-test.ts`), which injects the resolver-aware provider (FR-025). Production agent source is identical to the standard `agent` artifact except there is no production source divergence — `agent_test` simply uses a different entrypoint that wires the resolver-aware provider. The standard `agent` artifact remains the production artifact.
- **FR-017**: The `deploy_agent.yaml` testplan deployment MUST deploy the `agent_test` artifact (resolver-aware) and the `fake-llm` service. The agent reaches fake-llm via the resolver-aware provider resolving the fixed dominion target `dominion:///game/fake-llm:8080` (FR-025). No fixed-hostname `OPENCODE_BASE_URL` environment variable is set on the agent. (Rationale: dominion internal service discovery is registry-resolver based; fixed hostnames do not resolve.)
- **FR-018**: The test deployment MUST provide a `provider` secret for the agent service (any dummy value) so that `readSecret` succeeds — the fake LLM service ignores the API key.
- **FR-019**: The agent service source MUST NOT be modified to add a fake-specific provider or model entry. The only permitted agent source change is the generic extraction of reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (FR-024). No test-specific code branches exist in production agent source. The resolver-aware provider (FR-025) lives in the TEST-ONLY `bootstrap-test.ts` entrypoint, not in production agent source.
- **FR-025**: The `agent_test` test bootstrap (`bootstrap-test.ts`) MUST inject a resolver-aware provider: it resolves the FIXED dominion target `dominion:///game/fake-llm:8080` via `createResolver()` imported from `@dominion/common-js-resolver` (the core resolver at `common/js/resolver`, NOT the gRPC resolver plugin), obtains the current pod IP, builds `baseURL = http://<resolved-endpoint>/v1` (the resolved endpoint already includes the port, e.g. `http://10.0.0.9:8080/v1`), and constructs the real `ChatOpenAI` feeding the real `AgentAdapterImpl`. The full real pipeline (`createAgent` → middleware → `streamEvents` → contentBlock extraction → reasoning extraction) MUST run unchanged. The agent `BUILD.bazel` MUST add `:node_modules/@dominion/common-js-resolver` to the `lib` ts_project deps and `//common/js/resolver:runtime_pkg` to `server_pkg_test`'s runtime_deps. The target is hardcoded; no environment variable supplies it.
- **FR-020**: The `deploy_agent.yaml` MUST deploy the fake LLM service as an additional service artifact alongside the agent, session, proxy, prompt, and gateway services.
- **FR-021**: The testplan directory MUST include a README documenting: the fake LLM service's role, deployment topology, response-template data file format, the stateless per-request matching model (keyword substring match, alphabetical-by-name tiebreak, random fallback), and how to write tests that assert against configured content.
- **FR-022**: The existing large-test cases MUST be rewritten to assert against the fake service's configured response templates (from data files), replacing the old hardcoded assertions that checked `FakeLlmAdapter` strings.
- **FR-023**: The fake LLM service MUST be built as a Go service using the repository's Bazel Go toolchain (`artifact_pkg_go` + `artifact_image`), consistent with the existing game service pattern.
- **FR-024**: The agent's `AgentAdapterImpl` MUST extract reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` when present, and yield it as a `{ type: "reasoning" }` content block before yielding text blocks. It MAY also accept native `contentBlocks` of type `reasoning` if future `@langchain/openai` versions produce them.

### Key Entities *(include if feature involves data)*

- **Fake LLM Service**: A standalone HTTP service implementing the OpenAI Chat Completions API. Deployed alongside the `agent_test` artifact in test environments. The agent reaches it via the resolver-aware provider resolving the fixed target `dominion:///game/fake-llm:8080` to a pod IP (not via a fixed hostname). Returns pre-configured streaming responses with reasoning and text content. Holds only the read-only merged message set in memory; no per-request mutable state.
- **Response Message**: The atomic response template unit. Contains `name` (globally unique), `match_keywords` (non-empty array), `reasoning` text (emitted as `reasoning_content`), and `text` (emitted as `content`). Belongs to the merged flat set sorted alphabetically by `name`.
- **Data File**: A JSON or YAML file loaded at fake service startup. Contains a flat `messages` list (no `groups` wrapper). Multiple files are merged into one alphabetically-sorted set. Located in `projects/game/fake-llm/testdata/` and bundled into the service's container image.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Large tests exercise the real `AgentAdapterImpl` code path (createAgent → ChatOpenAI → streamEvents → contentBlock extraction) in 100% of agent-related test suites — verified by zero `FakeLlmAdapter` imports AND by the `agent_test` bootstrap building the real `AgentAdapterImpl` (not a fake adapter). The retained test artifact differs ONLY by resolver-aware provider injection.
- **SC-002**: The fake LLM service returns responses that produce both thinking frames and text frames through the real agent pipeline in 100% of tested dialog flows — verified by observing thinking frames emitted from `AgentAdapterImpl`'s extraction of `additional_kwargs.reasoning_content` (or native reasoning content blocks) followed by text frames from `contentBlocks`.
- **SC-003**: A test author can add a new response scenario by creating or editing a JSON/YAML data file without modifying Go source code — verified by deploying a data file change and seeing the new response content without a code rebuild.
- **SC-004**: Stateless matching deterministically selects the correct message via keyword matching with alphabetical-by-name tiebreak, and returns a random message only when no keyword matches — verified in 100% of tested multi-turn scenarios.
- **SC-005**: The production `agent` artifact is the standard one; large tests use a narrow `agent_test` artifact whose only divergence is the resolver-aware test bootstrap — verified by `service.yaml` exposing `agent` (production) and `agent_test` (test), and by diffing that their source is identical except the bootstrap entrypoint.
- **SC-006**: All existing large-test suites (agent-lifecycle, agent-dialog, checkpoint-resume, per-profile-model, concurrent-serialization) pass against the fake LLM service deployment.

## Assumptions

- The `@langchain/openai` package's `ChatOpenAI` preserves `delta.reasoning_content` from SSE streaming chunks in `AIMessageChunk.additional_kwargs.reasoning_content`, but does **not** convert it into a native `contentBlocks` entry of type `reasoning` in version `1.4.7`. Therefore, `AgentAdapterImpl` must explicitly read `additional_kwargs.reasoning_content` to produce thinking frames. This behavior was verified by the `experimental/openai_llm` prototype on 2026-06-17.
- Dominion internal service discovery is registry-resolver based (the core resolver at `common/js/resolver` queries the deploy registry); there is no k8s DNS for internal services. The agent reaches fake-llm via the resolver resolving the fixed target `dominion:///game/fake-llm:8080`, not via a fixed hostname. The resolver relies on platform-injected `DOMINION_ENVIRONMENT` and `SERVICE_APP` env vars.
- The fake LLM service's process lifetime matches the SUT deployment lifetime (deployed at test start, removed at test end). The service holds no per-request mutable state, only the read-only merged message set.
- The existing `deploy_agent.yaml` infrastructure supports adding additional service artifacts and setting per-service environment variables.
- The agent service reads the `provider` secret from `/etc/secrets/provider` — the test deployment must mount a dummy secret file at that path.
- The fake service only needs to support the OpenAI Chat Completions format. Anthropic-format (`/v1/messages`) support is deferred to a future iteration if needed.
- Non-streaming mode is supported but streaming is the primary mode exercised by the agent (which uses `streamEvents`).
- `@langchain/openai` `ChatOpenAI` `configuration.baseURL` is the sole base URL used for HTTP calls to the LLM provider. The resolver-aware provider sets this to `http://<resolved-ip>/v1`.

### Out of Scope (Explicit)

- **No Anthropic wire format**: The fake service implements only OpenAI Chat Completions (`/v1/chat/completions`). Anthropic `/v1/messages` format is deferred.
- **No fake service persistence**: The service holds no per-request mutable state and no durable storage. Durable state is out of scope.
- **No fake-specific production agent branches**: The agent service source MUST NOT add fake-provider, fake-model, or test-only configuration branches to production code. The only production agent source change permitted is the generic extraction of reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (FR-024). The resolver-aware provider (FR-025) is test-only, in `bootstrap-test.ts`.
- **No fake service authentication or rate limiting**: The service is deployed only in isolated test environments.
- **No dynamic data file reload**: Response templates are loaded once at startup. Mid-test updates require service restart.
- **No image/vision/multimodal support**: The fake service handles text-only chat completions.
- **No tool-call / function-calling simulation**: The fake service does not emit `tool_calls` in responses. Tool-call testing is out of scope for this iteration.
- **No empty-keyword messages**: Messages with empty or missing `match_keywords` are rejected at startup. Every message must have explicit match criteria.
- **No default-fallback-group concept**: The old default-fallback-group concept is replaced by the random fallback. When no keyword matches, a random message from the full set is returned (legitimate only for don't-care scenarios).
- **No token counting accuracy**: The fake service may return dummy `usage` values. Accurate token accounting is not required for testing.

## References

- `@langchain/openai` v1.4.7 source: `dist/converters/completions.js` preserves `delta.reasoning_content` in `additional_kwargs.reasoning_content` (streaming path, line ~264) but does not convert it to a native `contentBlocks` reasoning block. Verified in the repository Bazel cache at `node_modules/.aspect_rules_js/@langchain+openai@1.4.7_*/node_modules/@langchain/openai/dist/converters/completions.js`.
- LangChain Python issue confirming analogous behavior in `langchain-openai`: [ChatOpenAI drops reasoning_content field from reasoning models (o1, grok) · Issue #34706](https://github.com/langchain-ai/langchain/issues/34706)
- LangChain Python PR preserving `reasoning_content` in `additional_kwargs`: [fix(openai): preserve reasoning_content from reasoning models in AIMessage · PR #34705](https://github.com/langchain-ai/langchain/pull/34705)
- Repository internal prototype: `experimental/openai_llm/testplan/interface_test.yaml` and `experimental/openai_llm/testplan/interface_test.go`, executed 2026-06-17, produced `hasNativeReasoningBlock=false` and `additionalKwargs.reasoning_content="thinking..."` against `@langchain/openai` v1.4.7.
