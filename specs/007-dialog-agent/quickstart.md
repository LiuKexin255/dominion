# Quickstart: Dialog Agent Planning Validation

This guide describes the validation surface the implementation must satisfy.

## Prerequisites

- Work from repository root `/mnt/code/dominion`.
- Read `.specify/memory/constitution.md`, `style/README.md`, `style/api.md`, `style/mongo.md`, and `style/large_test.md` before implementation.
- Use Bazel-managed commands only.

## 1. Dependency and package synchronization

If LangChain/deepagents dependencies are added, update the root catalog first and synchronize through Bazel-managed PNPM:

```bash
bazel run @pnpm -- --dir /mnt/code/dominion install
```

Expected outcome: root lockfile/catalog are synchronized; no package-level lockfiles are introduced.

## 2. Build the grpc-js agent service

```bash
bazel build //projects/game/agent:cmd_image
```

Expected outcome: the TypeScript grpc-js agent compiles and packages into a Node.js OCI artifact using `artifact_pkg_js` and `artifact_image`.

## 3. Run agent package tests

```bash
bazel test //projects/game/agent:lib_test
```

Expected outcomes:

- optional missing provider secret file returns an empty secret;
- fake LLM/model injection avoids real provider calls;
- creating an agent copies profile prompt/model data;
- deleting a profile after agent creation does not stop or mutate the active agent;
- user messages sent during processing are queued FIFO;
- thinking and final response frames are emitted in order.

## 4. Verify preserved Go services

```bash
bazel test //projects/game/session/... //projects/game/proxy/... //projects/game/gateway/... //projects/game/prompt/...
```

Expected outcome: existing session, proxy, gateway, and prompt behavior remains green after the agent replacement.

## 5. Validate the rewritten large-test plan

```bash
guitar validate projects/game/testplan/system_test.yaml
```

Expected outcome: one YAML file validates with multiple suites. Prompt/profile suites do not deploy agent/proxy; dialog suites use fake LLM wiring and do not call a real provider.

## 6. Run large tests through the real surface

```bash
guitar run projects/game/testplan/system_test.yaml
```

Expected outcomes:

- prompt/profile suite passes with only prompt-relevant deployment;
- agent dialog suite creates a session and agent, connects through gateway/proxy, sends text, receives visible thinking and final text response, and verifies queue behavior;
- no real provider call occurs;
- guitar performs cleanup.

## 7. Final repository verification

```bash
bazel build //...
bazel test //...
```

Expected outcome: whole repository build and tests pass, or any pre-existing unrelated blocker is documented with exact failing target and residual risk.

## 8. Profile management desktop UI (User Story 3 supplement)

### Prerequisites

- The prompt service and gateway are deployed (the prompt/profile large-test suite or a local gateway is sufficient).
- The desktop builds with the supplement changes.

### Go unit tests for new client methods and Wails bindings

```bash
bazel test //projects/game/desktop:all
```

Expected outcomes:

- `CreateAgentProfile` client method sends `POST /api/v1/prompts/agentProfiles` and returns an `AgentProfileView`.
- `GetAgentProfile` client method sends `GET /api/v1/prompts/agentProfiles/{name}` and returns an `AgentProfileView`.
- `DeleteAgentProfile` client method sends `DELETE /api/v1/prompts/agentProfiles/{name}` and returns nil on success.
- Wails bindings log, trace, and convert proto responses to view models consistent with `ListAgentProfiles`.

### Desktop UI acceptance (manual or test)

1. Open the desktop app, navigate from sessions page to the profile management page.
2. Create a new profile (name + model + system prompt) via the form; verify it appears in the list.
3. Verify the list shows all profiles with their names, models, and prompt previews.
4. Delete a profile; verify it disappears from the list.
5. Create an agent instance using a profile, then delete that profile; verify the agent instance continues running (covered by existing backend behavior and the large-test prompt/profile suite).

Expected outcome: all four US-3 acceptance scenarios pass through the desktop interface.

### Large-test prompt/profile suite

The existing prompt/profile suite in `system_test.yaml` already covers backend profile CRUD through the gateway. No new large-test suite is needed for the supplement unless desktop-level E2E testing is explicitly required.
