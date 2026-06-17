# Feature Specification: Fake LLM Service for Large-Test Integration

**Feature Branch**: `012-fake-llm-service`

**Created**: 2026-06-16

**Status**: Draft

**Input**: User description: "将测试替身方案从 agent 内的 fake-llm 模块改为独立的 fake LLM HTTP 服务（OpenAI 格式），使大型测试覆盖 agent 的完整执行链路（createAgent、middleware、streamEvents、contentBlock 提取、provider 初始化）。fake 服务支持 JSON/YAML 配置响应模板，通过关键词匹配选择消息组（group-drain-then-rematch 模型：当前组未耗尽时不重新匹配，耗尽后才触发新一轮匹配），内存内跟踪活动组状态以实现线性返回。" 2026-06-17 更新：已通过 experimental/openai_llm 原型验证 `@langchain/openai` 对 `reasoning_content` 的实际解析行为。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Full Agent Pipeline Under Large Test (Priority: P1)

A test engineer runs the game system testplan. The deployment includes a fake LLM HTTP service alongside the real agent service (standard production artifact, not a test variant). The agent service's `OPENCODE_BASE_URL` environment variable points to the fake service, so all model requests are routed to it. When the test sends a text frame through the gateway WebSocket, the entire real code path executes: handler → SessionAgentStore → SessionAgent → AgentAdapterImpl → createAgent → ChatOpenAI → HTTP request to fake service → SSE streaming response → contentBlock extraction → frames written back to WebSocket.

The fake service uses a stateful group-drain-then-rematch model: keyword matching only runs when there is no active response group (either at startup or after the current group is fully drained). Once a group is selected, its messages are returned in linear order across successive calls without re-matching. When the group is exhausted, the next request triggers a fresh keyword match to select a new group. Responses contain streaming SSE chunks with `reasoning_content` (for thinking blocks) and `content` (for text blocks).

**Why this priority**: This is the core architectural goal. The current `FakeLlmAdapter` bypasses `createAgent`, all middleware, `streamEvents`, contentBlock extraction, and provider initialization — meaning large tests provide zero integration coverage for the spec-011 refactor. Without this, the LangChain migration has no end-to-end safety net.

**Independent Test**: Deploy the SUT with the fake LLM service, connect via WebSocket, send a text frame, and verify that thinking and text frames are received. The test exercises the real `AgentAdapterImpl` — if any part of the `createAgent` → `streamEvents` → contentBlock pipeline is broken, no frames are returned.

**Acceptance Scenarios**:

1. **Given** a deployed SUT with the fake LLM service and agent pointed at it, **When** the test connects to a session WebSocket and sends a text frame with a profile name, **Then** the agent service initializes the real `ChatOpenAI` model (not a fake adapter), sends an HTTP request to the fake service, and returns thinking + text frames through the real streaming pipeline.
2. **Given** a multi-turn conversation in the test with a selected response group of 3 messages, **When** messages 2 and 3 are sent, **Then** the fake service returns message[1] and message[2] in order (group stays active, no re-matching), and the agent's checkpoint-backed history accumulates correctly across turns.
3. **Given** the current response group is exhausted (all messages returned), **When** the next request arrives, **Then** keyword matching runs again to select a new group (which may be the same or different depending on request content), and the cycle repeats.
4. **Given** the full pipeline, **When** the agent processes the SSE stream, **Then** reasoning content from `delta.reasoning_content` is preserved by `@langchain/openai` in `AIMessageChunk.additional_kwargs.reasoning_content`, extracted by `AgentAdapterImpl`, and emitted as a thinking frame before the text frame from `delta.content`.

---

### User Story 2 - Configurable Response Templates via JSON/YAML (Priority: P1)

A test author configures the fake LLM service's response behavior by editing JSON or YAML data files, not by modifying service code. Each file defines one or more response groups, each with a keyword match rule and an ordered list of response messages. A response message specifies the reasoning text and response text that the fake service should return for that turn. The matching model is stateful: keyword matching selects a group only when no group is currently active; once selected, the group drains its messages linearly before a new match is triggered. This allows test authors to add new test scenarios or adjust response content without touching Go source code.

**Why this priority**: Configuration-driven test data is essential for maintainability. Hardcoded response templates in Go source would recreate the rigidity problem that the current `FakeLlmAdapter` has. Without external data files, every test scenario change requires a code change and rebuild.

**Independent Test**: Create a JSON/YAML data file with a response group matching keyword "greeting" containing 3 messages, deploy the fake service, send 4 sequential requests containing "greeting", and verify the first 3 responses match the configured messages in order while the 4th triggers a re-match cycle.

**Acceptance Scenarios**:

1. **Given** a data file defining a response group with `match_keywords: ["hello", "greeting"]` and three response messages, **When** three sequential requests containing "hello" are sent (no other group active), **Then** the first response uses message[0], the second message[1], and the third message[2] — keyword matching does not run on requests 2 and 3 because the group is still active.
2. **Given** the response group from scenario 1 is now exhausted, **When** a fourth request containing "hello" arrives, **Then** keyword matching runs again, re-selects the group (or selects a different one if content differs), and the drain cycle restarts from message[0].
3. **Given** a data file with no `match_keywords` field (or empty), **When** a keyword match is triggered (no active group) and no other group's keywords match the request, **Then** this group is used as the default fallback.
4. **Given** a keyword match is triggered (no active group) and no group matches and no default exists, **Then** the fake service returns a minimal valid response (empty reasoning, generic text) rather than an error, and no group becomes active.
5. **Given** the data file is loaded at service startup, **When** the service receives requests, **Then** the configured templates are used without requiring restart mid-test (data is loaded once at boot).

---

### User Story 3 - Remove In-Process Fake Adapter and Test Artifact (Priority: P1)

The test engineer removes the in-process `FakeLlmAdapter`, its test bootstrap (`bootstrap-test.ts`), and the test-only Bazel artifacts (`server_pkg_test`, `cmd_image_test`, `agent_test` service entry). Large tests use the standard production `agent` artifact. The only difference between production and test deployment is environment configuration: `OPENCODE_BASE_URL` and the `provider` secret value.

**Why this priority**: Eliminating the parallel test code path is the prerequisite for the fake-service approach. As long as the fake adapter exists, it is the path of least resistance and the coverage gap persists.

**Independent Test**: Verify that no source file under `projects/game/agent/src/` references `FakeLlmAdapter` or `bootstrap-test`, that `BUILD.bazel` has no `*_test` artifact targets for the agent, and that `deploy_agent.yaml` references the `agent` (not `agent_test`) artifact.

**Acceptance Scenarios**:

1. **Given** the agent service codebase after removal, **When** a developer searches for `FakeLlmAdapter`, **Then** no production or test source file imports it.
2. **Given** the agent `BUILD.bazel` after removal, **When** a developer inspects artifact targets, **Then** only `server_pkg` and `cmd_image` exist — no `server_pkg_test` or `cmd_image_test`.
3. **Given** `deploy_agent.yaml` after update, **When** the testplan deploys the agent, **Then** it uses the standard `agent` artifact with environment variables pointing to the fake LLM service.

---

### User Story 4 - Testplan Documentation and Test Rewrite (Priority: P2)

The testplan directory gains a README documenting the fake LLM service: how it works, how to configure response templates, how the agent is pointed at it in deployment, and how tests assert on response content. The existing large-test cases (`agent_dialog_test.go`, `agent_lifecycle_test.go`, `agent_checkpoint_test.go`) are rewritten to use the new deployment topology and assert against the fake service's configured response templates instead of the old hardcoded `FakeLlmAdapter` strings.

**Why this priority**: Documentation ensures the testing approach is reproducible and maintainable. Test rewrites are needed because the old assertions checked strings baked into Go source (`"Processing your message..."`, `"Hello! This is a simulated response..."`) — these must be updated to reference the fake service's data-file-defined templates.

**Independent Test**: Run the game system testplan with the `testplan` skill and verify all agent-related suites pass against the fake LLM service.

**Acceptance Scenarios**:

1. **Given** the testplan README, **When** a new developer reads it, **Then** they understand how to deploy the fake service, configure response templates, and write new tests that assert against configured content.
2. **Given** the rewritten `agent_dialog_test.go`, **When** the dialog suite runs, **Then** thinking-before-text ordering is verified, response content matches the fake service's configured template, and FIFO multi-message ordering holds.
3. **Given** the rewritten `agent_checkpoint_test.go`, **When** the checkpoint suite runs, **Then** ListMessages returns checkpoint-backed messages from the real `createAgent` pipeline (not from a hand-written StateGraph).

---

### Edge Cases

- What happens when keyword matching (triggered because no group is active) finds multiple matching groups? The first matching group (in data file order) is selected; subsequent groups are not evaluated. This match only runs when there is no active group.
- What happens when the active response group's message list is exhausted? The next request clears the active group and triggers keyword matching to select a new group. The new group's drain cycle starts from message[0]. This means the previous group's last message is not repeated — the group transitions to a fresh match.
- What happens when keyword matching (no active group) finds no match and no default/fallback group? The service returns a minimal valid OpenAI-format response (empty reasoning content, generic text response) with HTTP 200, and no group becomes active. The next request will also trigger matching.
- What happens when the agent sends a non-streaming request? The fake service handles both `stream: true` (SSE chunks) and `stream: false` (single JSON response) modes.
- What happens when the fake service's data file is malformed? The service fails at startup with a clear error message identifying the file and the parse error.
- What happens to the active group and position counter when the fake service is restarted mid-test? All in-memory state resets — no active group, position at 0. This is acceptable because a service restart mid-test is an infrastructure failure, not a test-scenario concern.
- What happens when the agent sends the `model` field in the request? The fake service ignores the model name entirely — it accepts any model and returns from its response groups. This allows test profiles to use any model name.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a standalone HTTP service (`fake-llm`) under `projects/game/` that implements the OpenAI Chat Completions API endpoint (`POST /v1/chat/completions`) with both streaming (`stream: true`, SSE) and non-streaming (`stream: false`) response modes.
- **FR-002**: The fake LLM service MUST emit streaming responses containing `delta.reasoning_content` for reasoning/thinking blocks and `delta.content` for text blocks. The agent's `AgentAdapterImpl` MUST extract reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (fallback to native `contentBlocks` of type `reasoning` if a future `@langchain/openai` version provides them) and text content from `contentBlocks` of type `text`, so that thinking frames and text frames are produced through the real streaming pipeline.
- **FR-003**: The fake LLM service MUST load response template data from external JSON or YAML files at startup, allowing test authors to define response groups without modifying service source code.
- **FR-004**: Each response group in the data file MUST support: a list of keyword strings for matching (`match_keywords`), an ordered list of response messages, and each response message MUST specify reasoning text and response text.
- **FR-005**: The fake LLM service MUST use a stateful group-drain-then-rematch model for response selection. The service maintains a single "active group" and a position counter. Keyword matching runs ONLY when there is no active group (initial state, or after the previous group was exhausted). While a group is active, requests receive messages in linear order without re-matching.
- **FR-006**: When keyword matching is triggered (no active group), the fake service MUST match the incoming request's last user message text against response group keywords. The first group (in data file order) whose keywords contain a case-insensitive substring match of the request text is selected. The selected group becomes the active group at position 0.
- **FR-007**: When a request arrives and an active group exists with remaining messages (position < message count), the fake service MUST return the message at the current position and increment the position counter. Keyword matching MUST NOT run in this case.
- **FR-008**: When a request arrives and the active group is exhausted (position >= message count), the fake service MUST clear the active group, run keyword matching against the current request to select a new group, and return message[0] of the newly selected group. The new group becomes active at position 1.
- **FR-009**: When keyword matching is triggered and no group matches (and no default/fallback group exists), the fake service MUST return a minimal valid response and no group becomes active. The next request will trigger matching again.
- **FR-010**: The fake LLM service MUST accept any API key (Bearer token) and return HTTP 200 — no authentication is enforced.
- **FR-011**: The fake LLM service MUST ignore the `model` field in requests — any model name is accepted and the same response-template logic applies.
- **FR-012**: The system MUST remove the in-process `FakeLlmAdapter` (`fake-llm.ts`, `fake-llm.test.ts`), the test bootstrap (`bootstrap-test.ts`), and the test-only Bazel artifact targets (`server_pkg_test`, `cmd_image_test`) from the agent service.
- **FR-013**: The agent `service.yaml` MUST remove the `agent_test` artifact entry. Only the standard `agent` artifact remains.
- **FR-014**: The `deploy_agent.yaml` testplan deployment MUST deploy the standard `agent` artifact (not `agent_test`) with `OPENCODE_BASE_URL` environment variable pointing to the fake LLM service's HTTP endpoint.
- **FR-015**: The test deployment MUST provide a `provider` secret for the agent service (any dummy value) so that `readSecret` succeeds — the fake LLM service ignores the API key.
- **FR-016**: The agent service source MUST NOT be modified to add a fake-specific provider or model entry. The only permitted agent source change is the generic extraction of reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (FR-021). No test-specific code branches exist in production agent source.
- **FR-017**: The `deploy_agent.yaml` MUST deploy the fake LLM service as an additional service artifact alongside the agent, session, proxy, prompt, and gateway services.
- **FR-018**: The testplan directory MUST include a README documenting: the fake LLM service's role, deployment topology, response-template data file format, the group-drain-then-rematch matching model, and how to write tests that assert against configured content.
- **FR-019**: The existing large-test cases MUST be rewritten to assert against the fake service's configured response templates (from data files), replacing the old hardcoded assertions that checked `FakeLlmAdapter` strings.
- **FR-020**: The fake LLM service MUST be built as a Go service using the repository's Bazel Go toolchain (`artifact_pkg_go` + `artifact_image`), consistent with the existing game service pattern.
- **FR-021**: The agent's `AgentAdapterImpl` MUST extract reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` when present, and yield it as a `{ type: "reasoning" }` content block before yielding text blocks. It MAY also accept native `contentBlocks` of type `reasoning` if future `@langchain/openai` versions produce them.

### Key Entities *(include if feature involves data)*

- **Fake LLM Service**: A standalone HTTP service implementing the OpenAI Chat Completions API. Deployed alongside the agent in test environments. Returns pre-configured streaming responses with reasoning and text content. Stateless across restarts; maintains in-memory active-group state (group reference + position counter) within a single process lifetime.
- **Active Group**: The currently selected response group. At most one group is active at any time. While a group is active, requests drain its messages linearly without keyword re-matching. The active group is cleared when exhausted, triggering a fresh keyword match on the next request. Keyword matching selects the active group from response groups in the data file based on case-insensitive substring matching of the request's last user message.
- **Response Group**: A named collection of response messages with an associated keyword match rule (`match_keywords`). One data file may contain multiple response groups. A group with no keywords (or empty keywords) acts as the default fallback during matching. Only the first matching group (data file order) becomes active.
- **Response Message**: A single turn's response template within a response group. Contains `reasoning` text (emitted as `reasoning_content` in SSE deltas) and `text` text (emitted as `content` in SSE deltas). Returned in linear order (by position counter) within the active group.
- **Data File**: A JSON or YAML file loaded at fake service startup. Contains one or more response groups. Located in `projects/game/fake-llm/` and bundled into the service's container image.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Large tests exercise the real `AgentAdapterImpl` code path (createAgent → ChatOpenAI → streamEvents → contentBlock extraction) in 100% of agent-related test suites — verified by confirming no `FakeLlmAdapter` import exists in any deployed artifact.
- **SC-002**: The fake LLM service returns responses that produce both thinking frames and text frames through the real agent pipeline in 100% of tested dialog flows — verified by observing thinking frames emitted from `AgentAdapterImpl`'s extraction of `additional_kwargs.reasoning_content` (or native reasoning content blocks) followed by text frames from `contentBlocks`.
- **SC-003**: A test author can add a new response scenario by creating or editing a JSON/YAML data file without modifying Go source code — verified by deploying a data file change and seeing the new response content without a code rebuild.
- **SC-004**: The group-drain-then-rematch model selects the correct response group on first match, drains messages in linear order without re-matching, and re-matches after exhaustion — verified in 100% of tested multi-turn scenarios.
- **SC-005**: The agent service has exactly one artifact target (`agent`), not two — verified by `BUILD.bazel` and `service.yaml` inspection.
- **SC-006**: All existing large-test suites (agent-lifecycle, agent-dialog, checkpoint-resume, per-profile-model, concurrent-serialization) pass against the fake LLM service deployment.

## Assumptions

- The `@langchain/openai` package's `ChatOpenAI` preserves `delta.reasoning_content` from SSE streaming chunks in `AIMessageChunk.additional_kwargs.reasoning_content`, but does **not** convert it into a native `contentBlocks` entry of type `reasoning` in version `1.4.7`. Therefore, `AgentAdapterImpl` must explicitly read `additional_kwargs.reasoning_content` to produce thinking frames. This behavior was verified by the `experimental/openai_llm` prototype on 2026-06-17.
- The agent service's `OPENCODE_BASE_URL` environment variable (read in `server.ts:92`) is the sole base URL for all model providers via `ModelProviderCache`. In the test deployment, all model names route to this single base URL — no per-model routing is needed.
- The fake LLM service's process lifetime matches the SUT deployment lifetime (deployed at test start, removed at test end). In-memory state does not need to survive restarts.
- The existing `deploy_agent.yaml` infrastructure supports adding additional service artifacts and setting per-service environment variables.
- The agent service reads the `provider` secret from `/etc/secrets/provider` — the test deployment must mount a dummy secret file at that path.
- The fake service only needs to support the OpenAI Chat Completions format. Anthropic-format (`/v1/messages`) support is deferred to a future iteration if needed.
- Non-streaming mode is supported but streaming is the primary mode exercised by the agent (which uses `streamEvents`).
- The testplan `guitar` infrastructure can resolve the fake LLM service's internal hostname for the agent's `OPENCODE_BASE_URL` (e.g., `http://fake-llm.game.svc:8080` or equivalent internal service DNS).

### Out of Scope (Explicit)

- **No Anthropic wire format**: The fake service implements only OpenAI Chat Completions (`/v1/chat/completions`). Anthropic `/v1/messages` format is deferred.
- **No fake service persistence**: In-memory active-group state resets on restart. Durable state is out of scope.
- **No fake-specific agent branches**: The agent service source MUST NOT add fake-provider, fake-model, or test-only configuration branches. The only agent source change permitted is the generic extraction of reasoning content from `AIMessageChunk.additional_kwargs.reasoning_content` (FR-021). The fake service is reached purely through the `OPENCODE_BASE_URL` environment variable override.
- **No fake service authentication or rate limiting**: The service is deployed only in isolated test environments.
- **No dynamic data file reload**: Response templates are loaded once at startup. Mid-test updates require service restart.
- **No image/vision/multimodal support**: The fake service handles text-only chat completions.
- **No tool-call / function-calling simulation**: The fake service does not emit `tool_calls` in responses. Tool-call testing is out of scope for this iteration.
- **No token counting accuracy**: The fake service may return dummy `usage` values. Accurate token accounting is not required for testing.

## References

- `@langchain/openai` v1.4.7 source: `dist/converters/completions.js` preserves `delta.reasoning_content` in `additional_kwargs.reasoning_content` (streaming path, line ~264) but does not convert it to a native `contentBlocks` reasoning block. Verified in the repository Bazel cache at `node_modules/.aspect_rules_js/@langchain+openai@1.4.7_*/node_modules/@langchain/openai/dist/converters/completions.js`.
- LangChain Python issue confirming analogous behavior in `langchain-openai`: [ChatOpenAI drops reasoning_content field from reasoning models (o1, grok) · Issue #34706](https://github.com/langchain-ai/langchain/issues/34706)
- LangChain Python PR preserving `reasoning_content` in `additional_kwargs`: [fix(openai): preserve reasoning_content from reasoning models in AIMessage · PR #34705](https://github.com/langchain-ai/langchain/pull/34705)
- Repository internal prototype: `experimental/openai_llm/testplan/interface_test.yaml` and `experimental/openai_llm/testplan/interface_test.go`, executed 2026-06-17, produced `hasNativeReasoningBlock=false` and `additionalKwargs.reasoning_content="thinking..."` against `@langchain/openai` v1.4.7.
