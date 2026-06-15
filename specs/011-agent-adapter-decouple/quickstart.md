# Quickstart: Agent Adapter Decoupling and LangChain Foundation

## Purpose

Validate that the agent service implements the SessionAgent/AgentAdapter decoupling, multi-profile support, simplified API (no Create/Delete), and LangChain foundation (no deepagent dependency) through unit tests, build checks, and large-test acceptance.

## Prerequisites

- Work from repository root `/mnt/code/dominion`.
- Read before implementation: `.specify/memory/constitution.md`, `README.md`, `style/README.md`, `style/api.md`, `style/golang.md`, `style/large_test.md`, and this feature plan.
- Use Bazel wrappers for build/test commands.
- Use `testplan` skill for large-test execution.

## Targeted Unit Validation

Run the agent TypeScript unit test suite:

```bash
bazel test //projects/game/agent:lib_test
```

Expected outcomes:

- Adapter tests prove `AgentAdapter` is constructed with `createAgent` (static `systemPrompt`), service-owned `beforeModel` middleware, and shared `MemorySaver` checkpointer.
- Adapter manager tests prove on-demand creation, profile switching (unbind + create), synchronous unbind on disconnect, async cleanup, and connection exclusivity.
- Middleware tests prove the context-preparation boundary is identifiable, testable, and behavior-compatible.
- Handler tests prove the new Connect flow (profile selection from frames, adapter switching, connection kick), GetAgent (adapter state), ListMessages (session-level), and same-session serialization.
- Fake LLM tests prove deterministic thinking/text output under the new `AgentAdapter` contract.
- No production source imports `deepagents`.

## Dependency and Build Metadata Validation

After removing deepagent imports and package/build deps, verify no source or build metadata still references the removed harness:

```bash
rg "deepagents|createDeepAgent" projects/game/agent pnpm-workspace.yaml
```

Expected outcomes:

- No production `src/*.ts` import uses `deepagents` or `createDeepAgent`.
- `projects/game/agent/package.json` and `projects/game/agent/BUILD.bazel` no longer include `deepagents`.
- Root dependency metadata changes are synchronized through repository PNPM/Bazel workflow.

## Proto Validation

Verify the proto changes are correct:

```bash
bazel build //projects/game:game_proto
```

Expected outcomes:

- `AgentService` no longer has `CreateAgent` or `DeleteAgent` RPCs.
- `ProxyService` no longer has `CreateAgent` or `DeleteAgent` RPCs.
- `AgentFrame` has `agent_profile_name` field (field number 21).
- `ListMessages` HTTP annotation uses session-level path.
- Removed request messages (`AgentCreateRequest`, `AgentDeleteRequest`, `CreateAgentRequest`, `DeleteAgentRequest`) are absent.

## Large-Test Acceptance

Run the existing game system testplan with the `testplan` skill. The affected cases are the agent lifecycle, dialog, and checkpoint suites — all rewritten for the new API surface.

Expected behavioral flow:

1. Create an agent profile via PromptService.
2. Create a session.
3. Connect WebSocket to `/api/v1/sessions/{id}/connect` (no prior agent creation).
4. Send a text frame with `agent_profile_name` → receive thinking/text/wait frames.
5. Send a text frame with a different `agent_profile_name` → adapter switches, response references prior context.
6. Disconnect and reconnect → prior messages visible, conversation resumes.
7. List messages at `/api/v1/sessions/{id}/messages` → chronological history.
8. Open a second WebSocket to the same session → first connection is closed.
9. Delete the session → conversation history is cleaned up.
10. Send rapid same-session messages → FIFO responses.

Expected test targets / files:

- `projects/game/testplan/agent_lifecycle_test.go`
- `projects/game/testplan/agent_dialog_test.go`
- `projects/game/testplan/agent_checkpoint_test.go`
- `projects/game/testplan/system_test.yaml`

## Repository Verification

Run final repository checks after targeted tests and large-test acceptance:

```bash
bazel build //...
bazel test //...
```

Expected outcomes:

- Whole-repository build succeeds.
- Whole-repository tests pass, or any pre-existing/infrastructure blocker is documented with residual validation risk.

## Manual Acceptance Checklist

- CreateAgent and DeleteAgent RPCs are removed from proto and all services.
- WebSocket connect path is `/api/v1/sessions/{id}/connect`.
- ListMessages path is `/api/v1/sessions/{id}/messages`.
- AgentFrame carries optional `agent_profile_name` (field 21).
- Users can connect, chat, switch profiles, and view history without any explicit agent lifecycle steps.
- Conversation history persists across profile switches within the same session.
- Only one WebSocket connection per session; new connection replaces old.
- Prior messages load within the existing 2-second target for normal conversation sizes.
- Same-session rapid sends remain ordered.
- The implementation exposes one service-owned `beforeModel` middleware as the context preparation boundary.
- No source imports `deepagents`.

## Reference Documentation

Authoritative sources for the APIs this feature depends on. See `plan.md` [References](plan.md#references) for the full annotated index.

- LangChain overview: https://docs.langchain.com/oss/javascript/langchain/overview
- Agents (`createAgent`): https://docs.langchain.com/oss/javascript/langchain/agents
- Short-term memory (`beforeModel`): https://docs.langchain.com/oss/javascript/langchain/short-term-memory
- Middleware overview: https://docs.langchain.com/oss/javascript/langchain/middleware
- Prebuilt middleware: https://docs.langchain.com/oss/javascript/langchain/middleware/built-in
- Deep Agents overview (the layer being removed): https://docs.langchain.com/oss/javascript/deepagents/overview
- `createAgent` API reference: https://reference.langchain.com/javascript/langchain/index/createAgent
