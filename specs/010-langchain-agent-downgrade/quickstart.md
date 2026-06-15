# Quickstart: Agent Engine Context Control Foundation

## Purpose

Validate that the agent service no longer depends on deepagent internals while preserving the existing public agent behavior through unit tests, build checks, and large-test acceptance.

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

- Adapter tests prove `createAgent` with `dynamicSystemPromptMiddleware` and a service-owned `beforeModel` middleware preserves existing behavior.
- Middleware tests prove the context-preparation boundary is identifiable, testable, and behavior-compatible.
- Handler tests prove Create/Get/Delete/ListMessages/Connect behavior remains compatible.
- Fake LLM tests prove deterministic thinking/text output is unchanged.
- No production source imports `deepagents`.

## Dependency and Build Metadata Validation

After removing deepagent imports and package/build deps, verify no source or build metadata still references the removed harness unexpectedly:

```bash
rg "deepagents|createDeepAgent" projects/game/agent pnpm-workspace.yaml
```

Expected outcomes:

- No production `src/*.ts` import uses `deepagents` or `createDeepAgent`.
- `projects/game/agent/package.json` and `projects/game/agent/BUILD.bazel` no longer include `deepagents` once the package is unused.
- If root dependency metadata changes, it is synchronized through the repository PNPM/Bazel workflow, not manual lock editing.

## Large-Test Acceptance

Run the existing game system testplan with the `testplan` skill. The affected cases are the agent dialog and checkpoint suites.

Expected behavioral flow:

1. Create an agent profile.
2. Create a session.
3. Create an agent under the session.
4. Connect through the WebSocket gateway surface.
5. Send text and receive compatible thinking/text/wait frames.
6. Leave and re-enter play.
7. List prior messages and verify chronological history.
8. Send a follow-up and verify resume behavior.
9. Delete and recreate the agent.
10. Verify the recreated agent has no stale conversation influence.
11. Send rapid same-session messages and verify FIFO responses.

Expected test targets / files:

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

- Public proto/API/desktop contracts are unchanged.
- Operator flow still completes with zero additional steps: create agent → chat → leave → return → view history → resume chat → delete → recreate.
- Prior messages load within the existing 2-second target for normal conversation sizes.
- Delete/recreate isolation succeeds.
- Same-session rapid sends remain ordered.
- The implementation exposes one service-owned `beforeModel` middleware as the context preparation boundary for future custom-history work.
