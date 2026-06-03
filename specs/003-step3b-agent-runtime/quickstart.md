# Quickstart: Step3.b Agent Runtime

**Branch**: `003-step3b-agent-runtime` | **Date**: 2026-06-03

This quickstart is the implementation and validation guide for Atlas/tasks. It assumes the feature has not been implemented yet.

## 1. Required Reading

Before editing code, read:

1. `.specify/memory/constitution.md`
2. `README.md`
3. `ideas/llm_agent_play_game/README.md`
4. `style/README.md`
5. `style/api.md`
6. `style/large_test.md`
7. `specs/003-step3b-agent-runtime/plan.md`
8. `specs/003-step3b-agent-runtime/contracts/agent-runtime-grpc.md`

Do not use the old agent runtime internals as the implementation design reference. Use only the protobuf contract, routing compatibility, and acceptance behavior.

## 2. Dependency and Build Setup

Use Bazel-managed commands only.

```bash
bazel run @pnpm -- --dir /mnt/code/dominion add -w deepagents langchain @langchain/core @grpc/grpc-js @grpc/proto-loader @grpc/reflection grpc-health-check zod
bazel run @pnpm -- --dir /mnt/code/dominion add -Dw @types/node typescript
```

If package versions need centralization, update the root workspace catalog rather than creating subpackage lockfiles. After dependency/build target changes, run the repository synchronization commands required by AGENTS.md:

```bash
bazel run //:gazelle
bazel mod tidy
```

## 3. Implementation Outline

1. Create a new TypeScript agent service package, e.g. `projects/game/agent-ts/`.
2. Bind `projects.game.AgentService` with `@grpc/grpc-js`.
3. Add a prompt service client adapter to load `AgentProfile` and `Skill` resources.
4. Add provider resolver:
   - default DeepAgents provider path,
   - OpenCode Go `opencode-go/<model-id>` with secret-backed credential validation during `CreateAgent`.
5. Add DeepAgent factory using `createDeepAgent({ model, systemPrompt, tools, skills })` with one primary agent only.
6. Add desktop-operation tool that emits one validated operation per invoke.
7. Add invoke coordinator that maps DeepAgent stream events to `AgentFrame` text/thinking/status/warn/operation frames.
8. Add lifecycle manager for 10-minute invoke timeout, 30-minute idle cleanup, and DeleteAgent idempotency.
9. Update desktop prompt/play UI for prompt management and conversation-style timeline.
10. Extend game testplan for TS agent service-chain acceptance.

## 4. Test-First Plan

Write or update tests before implementation where practical:

### TypeScript runtime tests

- Profile load succeeds only for enabled profile and enabled SKILLS.
- Unsupported MCP rejects `CreateAgent`.
- `opencode-go/<model-id>` parser rejects malformed/unsupported refs.
- Missing/empty/unreadable/unauthorized OpenCode Go credential rejects `CreateAgent`.
- Invoke coordinator emits progressive frame(s) before final operation using a fake DeepAgent/model.
- Operation tool rejects second operation in one invoke and out-of-bounds mouse coordinates.
- Fake clock verifies 10-minute invoke timeout and 30-minute idle cleanup skip/trigger rules.

### gRPC integration tests

- `CreateAgent`/`GetAgent`/`DeleteAgent` over grpc-js match `game.proto` messages.
- Bidirectional `Connect` accepts screenshot frames and streams ordered `AgentFrame` responses.
- Delete missing agent returns success.

### Compatibility tests

- Existing proxy/gateway WebSocket path `/api/v1/sessions/{session_id}/agent/connect` still reaches the agent stream.
- Existing step3.a sequence warning and screenshot-only continuation behavior still passes.

### Desktop tests/manual QA

- Prompt/profile/SKILL management surfaces create/list/inspect/delete resources.
- Play timeline collapses desktop screenshots by default and distinguishes thinking/text/tool/operation/warning/execution status.

## 5. Large-Test / Manual Acceptance Surface

Use the `testplan` skill for the game service-chain testplan once implementation exists.

Expected automated acceptance flow:

1. Deploy session, prompt, proxy, gateway, and new TypeScript agent service.
2. Create enabled SKILL.
3. Create enabled profile referencing model, system prompt, SKILL, and supported MCP.
4. Create session.
5. Create agent with profile.
6. Connect desktop/gateway WebSocket stream.
7. Send PNG screenshot frame with dimensions and screenshot id.
8. Observe at least one progressive DeepAgent event frame.
9. Observe one valid operation frame tied to the screenshot.
10. Send next screenshot as screenshot-only continuation.
11. Validate timeout, idle cleanup, sequence warnings, and DeleteAgent idempotency.

Windows native mouse/keyboard execution remains manual acceptance when unavailable in the automated environment.

## 6. Verification Commands

Targeted commands will be finalized after tasks create exact Bazel labels. The final implementation must include:

```bash
bazel run //:gazelle
bazel mod tidy
bazel build //...
bazel test //...
```

For TypeScript package-manager actions, always use:

```bash
bazel run @pnpm -- --dir /mnt/code/dominion <pnpm-args>
```

For large-test execution, load the `testplan` skill and run the relevant game testplan through guitar. If a deployment or environment blocker prevents execution, record the blocker and residual validation risk in the implementation report.
