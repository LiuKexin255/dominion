# Research: Fake LLM Service for Large-Test Integration

**Updated**: 2026-06-18

## Decision: Implement OpenAI Chat Completions only

**Rationale**: The agent's resolver-aware provider routes to one OpenAI-compatible base URL (the fake-llm HTTP service), and the spec explicitly defers Anthropic `/v1/messages`. Supporting only `POST /v1/chat/completions` keeps the fake service aligned with the path required to exercise `@langchain/openai` and `ChatOpenAI`.

**Alternatives considered**: Adding Anthropic wire-format support was rejected as out of scope. Keeping the in-process fake adapter was rejected because it bypasses `createAgent`, middleware, provider initialization, HTTP streaming, and content extraction.

## Decision: Use a standalone Go service under `projects/game/fake-llm`

**Rationale**: Existing game services use Go service layouts with `artifact_pkg_go`, `artifact_image`, `service.yaml`, and testplan deployment YAML. A normal service artifact lets large tests cover the same deployment and network boundaries as production services while keeping the agent source free of fake-specific branches.

**Alternatives considered**: A TypeScript fake service would add a second JS service path without matching current Go service deployment conventions. Embedding the fake in the agent was rejected because it recreates the coverage gap.

## Decision: Load JSON/YAML templates once at startup

**Rationale**: Test authors need data-driven responses without Go source edits. Loading once at boot matches testplan deployment behavior and avoids runtime reload semantics, file watchers, and concurrency hazards that are out of scope.

**Alternatives considered**: Dynamic reload was rejected by the spec. Hardcoded templates were rejected because every scenario change would require code rebuilds.

## Decision: Use stateless per-request matching (no active group)

**Rationale**: Stateless matching is simpler: each request is matched independently against the last user message. This removes all cross-request state and concurrency hazards, and each dialog turn is naturally independent. Test authors who need distinct responses across turns use distinct keywords per turn.

**Alternatives considered**: A stateful group-drain model was the initial design, but it introduced unnecessary complexity: active-group state, position counters, and exhaustion-triggered rematch logic contributed no test-coverage value beyond what stateless per-turn matching provides. Per-session state would require parsing application-level session identifiers, which are not part of the OpenAI wire contract.

## Decision: Resolve multi-match by alphabetical name after multi-file merge

**Rationale**: When multiple messages match a single request, returning the message whose `name` sorts first alphabetically provides a deterministic within-request tiebreak. Test authors control priority by choosing message names strategically. This is purely a within-request tiebreak, not a cross-turn "first match" concept.

**Alternatives considered**: First-match in data-file order was the initial approach, but it couples priority to file-ordering, which is fragile across multi-file merges. Alphabetical-by-name priority is explicit, visible to test authors, and independent of file layout.

## Decision: Stream `reasoning_content` before `content`

**Rationale**: Large tests must prove thinking frames come from the real provider streaming path. Streaming OpenAI-compatible chunks with `delta.reasoning_content` followed by `delta.content` matches the validated `@langchain/openai` behavior documented in the spec and upstream LangChain references.

**Alternatives considered**: Emitting only `content` would not test thinking-frame extraction. Emitting native LangChain `contentBlocks` is not possible at the HTTP OpenAI boundary.

## Decision: Random fallback for no-match scenarios

**Rationale**: When no message's keywords match the request, the service returns a uniformly random message from the full merged set instead of an error. This guarantees a valid 200 response for unmatched requests and is reserved for "don't care" test scenarios where the test does not assert on response content. Tests that require specific content must always match a keyword or add new data. The service logs a WARN line with the unmatched snippet and chosen message name for diagnosability.

**Alternatives considered**: Returning a minimal hardcoded response was the initial approach, but it left test authors unable to distinguish between true don't-care scenarios and misconfigured tests. Falling back to a random configured message makes the behavior more discoverable (the WARN log is visible) and still guarantees a valid response.

## Decision: Flatten data schema to per-message entries and forbid empty keywords

**Rationale**: The simplest matchable unit is one named message with its keyword list. Removing the `groups` wrapper and making `match_keywords` mandatory (non-empty) eliminates ambiguity: empty keywords previously acted as a "default fallback" that was hard to distinguish from a never-match condition. Now every message has explicit match criteria, and startup validation rejects empty keywords with a clear error.

**Alternatives considered**: Keeping optional keywords with default-fallback semantics was rejected because it introduced ambiguity and added a special case to the matching algorithm. Group wrappers added nesting without value when each message already has its own keyword list.

## Decision: Add generic reasoning extraction to `AgentAdapterImpl`

**Rationale**: `@langchain/openai` v1.4.7 preserves `delta.reasoning_content` in `AIMessageChunk.additional_kwargs.reasoning_content`, not as native reasoning `contentBlocks`. The agent must read this generic provider field and yield the existing `{ type: "reasoning" }` block before text blocks.

**Alternatives considered**: Adding a fake provider branch was rejected by FR-019. Waiting for a future native `contentBlocks` behavior was rejected because current large tests need coverage now.

## Decision: Reach fake-llm via a resolver-aware test provider (Option B), not a fixed-hostname env var

**Rationale**: Dominion service discovery is registry-resolver based, not k8s DNS. The core resolver at `common/js/resolver` (package `@dominion/common-js-resolver`) resolves a target like `dominion:///game/prompt:50051` to current pod IPs by querying the deploy registry over HTTP. The deploy compiler emits no k8s Service/ClusterIP/DNS. Therefore an internal hostname like `http://fake-llm:8080` does not resolve, and pod IPs are dynamic. ChatOpenAI's HTTP fetch (through `configuration.baseURL`) cannot use the gRPC resolver. So a fixed-hostname OPENCODE_BASE_URL cannot reach fake-llm.

The solution is a narrow `agent_test` artifact whose test bootstrap injects a resolver-aware provider that resolves the fixed target `dominion:///game/fake-llm:8080` via the core resolver (`createResolver()` from `@dominion/common-js-resolver`), uses the resolved pod IP to build `baseURL = http://<ip>/v1`, and feeds that into the real `ChatOpenAI`. The real pipeline runs unchanged. Only the provider's network-discovery layer differs from production.

**Alternatives considered**:
- (a) Fixed-hostname `OPENCODE_BASE_URL` env var — rejected; hostnames don't resolve (no k8s DNS, dynamic IPs).
- (b) Resolve-at-deploy + inject IP via env — rejected; needs new deploy tooling, and IP could drift since ModelProviderCache caches the model across requests.
- (c) Production code change to make baseURL resolver-aware — rejected; test-only complexity in production code. The resolver-aware provider is test-only in bootstrap-test.ts.
- (d) Keeping the old `FakeLlmAdapter` as fallback — rejected; it preserves the low-coverage path and bypasses the real pipeline.

## Decision: Use core resolver (`@dominion/common-js-resolver` at `common/js/resolver`), not the gRPC resolver plugin

**Rationale**: The test bootstrap needs the HTTP-based core resolver (`createResolver()`) that resolves dominion targets to pod IPs via the deploy registry. The gRPC resolver plugin at `common/js/grpc/resolver` is for gRPC channel resolution — it's a different mechanism. The core resolver reads platform-injected `DOMINION_ENVIRONMENT` and `SERVICE_APP` env vars and is already used by the game prompt and session services.

## Decision: Remove the FakeLlmAdapter bypass only; retain test artifact targets

**Rationale**: The acceptance goal shifted from "single artifact" to "remove the coverage bypass while preserving the resolver-aware test artifact." The `agent_test` artifact with resolver-aware provider exercises the real pipeline; the old `FakeLlmAdapter` bypassed it. Retaining `server_pkg_test`, `cmd_image_test`, and `agent_test` is correct because they now run the real pipeline through the resolver-aware provider.

**Alternatives considered**: Removing all test artifacts was the original plan (Option A), but it assumed fixed-hostname `OPENCODE_BASE_URL` would work. Option B requires the test artifact to host the resolver-aware entrypoint.

## Decision: Validate through unit tests plus game system testplan

**Rationale**: Fake-service unit tests prove parsing and response selection; adapter tests prove reasoning extraction; the game testplan proves deployment and the full agent WebSocket pipeline. This satisfies the constitution's observable delivery and large-test requirements.

**Alternatives considered**: Unit tests alone were rejected because they cannot prove `createAgent`/HTTP/SSE integration. Manual deployment-only validation was rejected because it would not preserve regression coverage.

## References

- OpenAI Chat Completions API: https://platform.openai.com/docs/api-reference/chat/create
- OpenAI streaming API: https://platform.openai.com/docs/api-reference/streaming
- LangChain reasoning-content issue: https://github.com/langchain-ai/langchain/issues/34706
- LangChain reasoning-content PR: https://github.com/langchain-ai/langchain/pull/34705
- Internal prototype: the openai_llm prototype interface testplan, since removed in `specs/048-js-esm-migration/tasks.md` T024 (its YAML cases referenced a bazel target name that never matched the BUILD declaration, so it could not run as delivered)
