# Quickstart: Fake LLM Service for Large-Test Integration

**Updated**: 2026-06-18

## Prerequisites

- Read `.specify/memory/constitution.md`, root `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, and `style/large_test.md` before implementation.
- Use Bazel wrappers only for Go, Gazelle, dependency, build, and test commands.
- Use the `testplan` skill for large-test execution.

## Validation scenario 1: fake service API surface

1. Build the fake service target:

   ```bash
   bazel build //projects/game/fake-llm:cmd_image
   ```

2. Run fake-service unit tests:

   ```bash
   bazel test //projects/game/fake-llm/...
   ```

3. Start the service through its normal binary target or deployment harness.

4. Send a streaming OpenAI-compatible request to `POST /v1/chat/completions` with a user message matching a configured message's keyword.

5. Expect SSE chunks containing `delta.reasoning_content`, then `delta.content`, then `[DONE]`.

## Validation scenario 2: stateless matching and multi-match tiebreak

1. Configure two messages in a JSON/YAML data file:
   - `name: "A-first"`, `match_keywords: ["hello"]`, `reasoning: "thinking A"`, `text: "response A"`
   - `name: "B-second"`, `match_keywords: ["hello"]`, `reasoning: "thinking B"`, `text: "response B"`
2. Send a request whose last user message includes "hello". Expect the `A-first` message (alphabetically first by name) to be returned with its configured reasoning and text.
3. Send a second request containing the same keyword "hello". Expect `A-first` again (stateless — same keyword returns the same message each time).
4. Send a request whose last user message contains no keyword. Expect a random message from the full set to be returned, and a WARN log line recording the unmatched snippet and chosen name.
5. Note: the random fallback is only valid for tests that do not assert on response content. Tests requiring explicit content must always match a keyword or add new data to the files.

## Validation scenario 3: agent extraction path

1. Run the targeted agent tests:

   ```bash
   bazel test //projects/game/agent:lib_test
   ```

2. Expect coverage proving `AIMessageChunk.additional_kwargs.reasoning_content` is yielded as a reasoning block before text blocks while the normal `contentBlocks` text path still works.

## Validation scenario 4: artifact cleanup (FakeLlmAdapter removal, agent_test retention)

1. Inspect agent source and build metadata:

   ```bash
   rg -n "FakeLlmAdapter" projects/game/agent/src/
   ```

2. Expect no `FakeLlmAdapter` references in source after removal.

3. Verify `projects/game/agent/service.yaml` exposes both `agent` (production) and `agent_test` (test, resolver-aware) artifacts.

4. Verify `BUILD.bazel` retains `server_pkg`, `server_pkg_test`, `cmd_image`, `cmd_image_test` targets.

## Validation scenario 5: game system large tests

1. Use the `testplan` skill to run `projects/game/testplan/system_test.yaml` or the affected agent suites.
2. Expect the deployment to include `fake-llm` and `agent_test` (resolver-aware artifact).
3. Expect agent dialog, lifecycle, checkpoint-resume, per-profile-model, and concurrent-serialization suites to pass using configured fake-service templates. The `agent_test` artifact reaches fake-llm via the resolver-aware provider resolving the fixed target `dominion:///game/fake-llm:8080`.

## Final repository validation

Run after targeted validation succeeds:

```bash
bazel build //...
bazel test //...
```

If large-test deployment infrastructure fails for reasons unrelated to this feature, record the failing command, blocker, trace/log investigation path, and residual validation risk before implementation is considered incomplete.
