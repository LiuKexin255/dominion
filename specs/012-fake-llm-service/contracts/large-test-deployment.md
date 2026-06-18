# Contract: Large-Test Deployment and Agent Integration

**Updated**: 2026-06-18

## Deployment topology

`projects/game/testplan/deploy_agent.yaml` must deploy:

- session service
- proxy service
- prompt service
- `agent_test` artifact (resolver-aware, narrow test-only artifact)
- fake LLM service artifact named `fake-llm`
- gateway service

The `agent_test` artifact is used (not standard `agent`) because the resolver-aware provider lives in its test bootstrap. The standard `agent` artifact remains the production artifact.

## Agent environment contract

The agent service receives:

- No `OPENCODE_BASE_URL` environment variable. The agent reaches fake-llm via the resolver-aware provider (FR-025) resolving the fixed target `dominion:///game/fake-llm:8080` using the core resolver (`createResolver()` from `@dominion/common-js-resolver`). A fixed-hostname URL would not resolve — dominion service discovery is registry-resolver based, not k8s DNS.
- `provider` secret: dummy value mounted where the production agent already reads provider credentials. The fake service accepts any API key.

No agent source change may introduce fake-specific provider/model names or test-only branches in production code. The resolver-aware provider (FR-025) is test-only in `bootstrap-test.ts`.

Test profile model names MUST NOT start with `minimax-` or `qwen3.` — those prefixes route to the Anthropic wire format via the agent's `inferProvider`, and would hit `/v1/messages` which the fake does NOT implement.

## Artifact contract

`projects/game/agent/service.yaml` exposes:

- `agent` → `:cmd_image` (production)
- `agent_test` → `:cmd_image_test` (test, resolver-aware)

Removed source:

- `FakeLlmAdapter` (`fake-llm.ts`, `fake-llm.test.ts`) — coverage bypass deleted.

Retained artifacts (now resolver-aware, not bypass):

- `agent_test`
- `server_pkg_test`
- `cmd_image_test`

## Test assertion contract

Agent-related large tests must verify behavior through the public gateway/WebSocket and message-history surfaces. The pipeline under test is the real one reached through the `agent_test` artifact:

- Connect to `/api/v1/sessions/{id}/connect` through gateway WebSocket.
- Send text frames with `agent_profile_name`.
- Observe thinking frame content sourced from fake-service `reasoning` templates.
- Observe text frame content sourced from fake-service `text` templates.
- Verify thinking-before-text ordering.
- Verify independent per-turn keyword matching: the same keyword returns the same message each turn (stateless); distinct keywords across turns return different configured messages.
- Verify `ListMessages` reflects checkpoint-backed messages from the real agent pipeline.

## Documentation contract

`projects/game/testplan/README.md` must explain:

- Why fake LLM is a standalone service.
- How `agent_test` reaches fake-llm via the resolver-aware provider resolving the fixed target `dominion:///game/fake-llm:8080` (not via a fixed-hostname env var).
- JSON/YAML response-template format.
- Stateless per-request matching behavior (keyword substring match, alphabetical-by-name tiebreak, random fallback).
- How to add new response messages and update assertions.
- How to run the game system testplan with the `testplan` skill.
