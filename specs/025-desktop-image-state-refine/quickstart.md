# Quickstart: Validation Guide

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-26

This is a runnable validation guide — the scenarios that prove the feature works end-to-end. It references the contracts and data model rather than duplicating them. Implementation detail belongs in `tasks.md`.

## Prerequisites

- A Windows desktop with classic Microsoft Minesweeper (`winmine.exe`) for the saolei scenarios (board geometry origin 24/200, cell 32×32px — see `projects/game/pkg/saolei-board/README.md`).
- The game services deployable via the testplan skill (`tools/test/guitar`); see `style/large_test.md`.
- Bazel workspace healthy: `bazel build //projects/game/...` succeeds.

## Build & unit gates (run before large tests)

```bash
bazel build //projects/game/...
bazel test  //projects/game/...
```

Relevant unit suites:
- `//projects/game/desktop:go_default_test` — window-select resolution, `FlowResultPart` framing, binary-WS round-trip.
- `//projects/game/gateway/...` — binary-proto `wsStream`, 10 MiB read limit.
- `//projects/game/agent/...` — `OperationBridge.handleResult(FlowResultPart)`, saolei MCP validation + text-board return.
- `//projects/game/pkg/saolei-board:lib_test` — recognition golden tests (unchanged; relied upon).

## Scenario 1 — Select window, then chat (no Capture pressed)

**Validates**: [window-select-contract.md](./contracts/window-select-contract.md) · spec FR-001..FR-006

1. Open the desktop session chat page; pick a window from the dropdown; **do not** press Capture.
2. Send a message that triggers an agent operation (e.g. a mouse click).
3. **Expect**: the operation executes against the selected window (no "no window bound" error), and a post-action screenshot is captured and returned.
4. Re-select a different window and repeat — the new selection is the target.
5. With no window selected, trigger an operation — **expect** a graceful "no window selected" failure (no crash).

## Scenario 2 — Large screenshot round-trip (no frame-size failure)

**Validates**: [image-transport-contract.md](./contracts/image-transport-contract.md) · spec FR-007..FR-011

1. Target a large/high-DPI window whose screenshot is near or above the prior 5 MiB ceiling (and well above the old 32 KiB WS default read limit).
2. Run a turn that sends a user-turn screenshot and at least one operation (post-action screenshot).
3. **Expect**: the screenshot is delivered to the agent and consumed (e.g. for recognition / mouse-tool display); the turn completes with no `ErrMessageTooBig` / WS teardown; no other turn is disrupted.

## Scenario 3 — Saolei text board + strict validation

**Validates**: [saolei-mcp-contract.md](./contracts/saolei-mcp-contract.md) · spec FR-012..FR-018, FR-022

1. Configure a saolei profile; open Minesweeper; select its window.
2. Have the agent call `saolei_init`.
3. **Expect**: the result is a **text** board (symbol legend), **no** screenshot in the result; the conversation bubble shows text only.
4. Call `saolei_click` on a legal unrevealed cell.
5. **Expect**: the result is the updated text board; the cell reflects the reveal.
6. Call `saolei_click` again on the **same** (now revealed) cell.
7. **Expect**: **rejected before dispatch** with reason `cell_already_revealed`; the desktop received no operation; the current text board + valid range are returned.
8. Negative checks (each rejected, no dispatch): out-of-bounds coordinate (`out_of_bounds`); a cell op before any `saolei_init` (`no_active_game`); a `saolei_flag` on a revealed cell (`cannot_flag_revealed`); a `saolei_chord_click` on a non-number (`chord_requires_number`); any cell op after a recognized terminal state (`game_over`).
9. Positive nuance: a `saolei_chord_click` on a revealed number whose adjacent-flag count does **not** match the number **is dispatched** (legal chord; may reveal nothing) — not rejected.

## Scenario 4 — Operation result on the control channel (FlowResultPart)

**Validates**: [flow-result-contract.md](./contracts/flow-result-contract.md) · spec FR-023..FR-026

1. Run any turn that triggers an operation (mouse or saolei).
2. **Expect**: the desktop reports the operation outcome as a `FlowResultPart` inside a `flow_parts` frame (not a display `tool_result` `MessagePart`).
3. **Expect**: the agent's `OperationBridge.handleResult` resolves the pending dispatch from that `FlowResultPart`.
4. **Expect**: the display `tool_result` `MessagePart` is emitted by the **agent** — text + screenshot for mouse tools, text-only for saolei (no screenshot in the saolei bubble).

## Large-test acceptance (Constitution principle VI)

The agent is a service, so acceptance MUST include an actual testplan execution (full deploy→test→cleanup), not a build check:

- The existing `projects/game/testplan` is extended/used to cover Scenarios 2–4 end-to-end against a deployed agent.
- Run via the testplan skill: `guitar run <plan.yaml>` (see `style/large_test.md` and the `testplan` SKILL).
- **Pass criterion**: all test cases pass. Any failed/flaky case = acceptance not met; fix and re-run until fully green.

## Expected outcomes (summary)

- Scenario 1: zero "no window bound" failures when a window is selected.
- Scenario 2: zero frame-size failures across the turn; image delivered intact.
- Scenario 3: every saolei result is a text board (never a model-facing image); every illegal move rejected before dispatch; every legal move dispatches and returns the updated board.
- Scenario 4: operation results travel `FlowResultPart` (control); display `tool_result` is agent-emitted and tool-appropriate.
