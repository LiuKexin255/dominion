# Quickstart: Desktop Conversation Debug Mode

**Feature**: `022-desktop-debug-mode` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md) | **Contract**: [contracts/debug-control-plane.md](./contracts/debug-control-plane.md)

A manual validation guide proving the feature works end-to-end. It is a *run/validation* guide — implementation details belong in `tasks.md`. Commands assume the repo root and the Bazel entrypoints in `AGENTS.md`.

## Prerequisites

- A built desktop binary (`bazel build` target for `projects/game/desktop`) and a reachable game gateway (default `https://game.liukexin.com`, configurable in the app's config area).
- A bound target window for the agent to operate on (so a tool operation that produces a tool result can be triggered).
- An agent profile that issues at least one tool call (e.g., a mouse click) within a turn.

## Build / compile gate (Constitution §IV — small test, part of development)

```bash
# Go desktop backend + agent: build and unit-test affected targets
bazel build //projects/game/desktop/...
bazel test  //projects/game/desktop/...
bazel test  //projects/game/agent/...      # operation-bridge timeout change

# Frontend: typecheck + build (no JS test runner exists)
bazel run @pnpm -- --dir "$PWD/projects/game/desktop/frontend" run typecheck
bazel run @pnpm -- --dir "$PWD/projects/game/desktop/frontend" run build
```

Expected: all green. `//projects/game/agent/...` must remain green after `DISPATCH_TIMEOUT_MS` is raised to 20 min (existing large tests at `projects/game/testplan/` cover dispatch; any test asserting the prior 5 s timeout is updated).

## Scenario 1 — Debug toggle surfaces verbose logs (spec US1)

1. Launch the desktop app, open/connect a session, enter the conversation page.
2. Confirm the log panel (bottom) shows **no** debug-level entries during a normal turn with Debug OFF.
3. Toggle the **Debug** switch in the conversation toolbar ON.
   - Expected: the switch reflects ON; the log panel begins showing DEBUG-level entries (from both the frontend and the Go backend, e.g., inbound frames and tool-execution steps) that were absent before.
4. Toggle Debug OFF.
   - Expected: no further DEBUG entries appear; previously-shown entries remain until "Clear Logs" is clicked.
5. Leave and re-enter the conversation page (or session).
   - Expected: Debug resets to OFF (not persisted).

**Pass**: DEBUG entries appear only while ON (spec SC-001); toggle is a single discoverable action (SC-002).

## Scenario 2 — Tool result held for confirmation before returning to the agent (spec US2)

1. Toggle Debug ON. Bind a target window. Send a user turn that makes the agent perform a tool operation (e.g., "click at …").
2. When the agent's tool operation executes, observe the conversation:
   - Expected: the tool-result bubble appears **with a "Confirm" control**, and the agent does **not** advance (no subsequent model output) — it is waiting for the result.
3. Inspect the held result (status / message / screenshot) in the bubble, then click **Confirm**.
   - Expected: the Confirm control disappears; the agent resumes and the turn continues.

**Pass**: every computed tool result is held and shows Confirm; clicking it returns the result and the agent resumes (spec SC-003).

## Scenario 3 — 15-minute auto-continue (spec US2 acceptance #4 / FR-013)

> Use a shortened internal timeout during development to avoid waiting 15 minutes; in the shipped build, wait the full 15 min or verify the timer branch via a Go unit test.

1. Toggle Debug ON, trigger a tool operation, and **do not** click Confirm.
2. Wait until the desktop's confirmation timeout elapses (15 min in production).
   - Expected: the held result is automatically returned to the agent and the Confirm control is dismissed; the turn continues as if confirmed.
3. Confirm (via logs) that the agent's 20-minute backstop did **not** fire (15 < 20).

**Pass**: auto-continue releases the result in 100% of cases and the agent backstop never fires during debug usage (spec SC-004).

## Scenario 4 — Transparency (spec US2 acceptance #5 / FR-007)

1. Run a turn with Debug ON, confirming each tool result.
2. Run the **same** turn (same user message, same window state) with Debug OFF.
3. Compare the two resulting conversations and the persisted session state.
   - Expected: identical tool results, agent decisions, and persisted conversation state. The pause only inserted a wait; it changed nothing about the result or outcome.

**Pass**: ON-with-confirmations and OFF produce identical outcomes (spec SC-005).

## Scenario 5 — Scope boundary / no regressions (spec FR-015 / SC-006)

1. With Debug toggled ON and OFF, exercise: the session list, agent profile management, a screenshot capture, and observe the log panel.
   - Expected: all remain fully functional; the SSE chat channel and the `game:log` forwarding mechanism are unchanged; the only agent-side change is the dispatch-timeout constant value.

**Pass**: no regressions outside the debug control plane (spec SC-006).

## Large-test note (Constitution §VI)

The desktop client is not a gRPC/HTTP service, so it is out of the large-test mandate (`style/large_test.md`). The only service touched is the agent, whose dispatch behavior is covered by existing large tests at `projects/game/testplan/`; those must remain green after the `DISPATCH_TIMEOUT_MS` change. Run them via the `testplan` skill:

```bash
guitar run projects/game/testplan/system_test.yaml
```
