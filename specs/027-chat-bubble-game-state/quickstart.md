# Quickstart: Chat Bubble UX Polish & Saolei Game-State Awareness

**Feature**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

Runnable validation scenarios that prove the feature works end-to-end. Each scenario lists prerequisites, commands, and the pass criterion. Implementation details (full code, complete test suites) belong in `tasks.md`; this is a validation/run guide. Format references: [data-model.md](./data-model.md), [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md), [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md).

**Build/test entrypoint** (per `AGENTS.md`): `bazel`. Large tests via the `testplan` skill (`guitar run <plan.yaml>`), per `style/large_test.md`.

---

## Scenario 1 — Think bubble: no visible scrollbar, follows the stream (US1, manual)

**What it proves**: FR-001 (hidden scrollbar) + FR-002..004 (auto-scroll, pause-on-scroll-up, open-to-bottom).

**Prerequisites**: A built desktop client (`bazel build //projects/game/desktop:desktop`) running on a Windows host against a live agent.

**Steps**:
1. Start a session whose agent produces >200 px of thinking content (any non-trivial turn).
2. Expand the think bubble (click `▸ Thinking…`).
3. Observe the `.thinking-content` area: confirm NO scrollbar track/thumb is visible on the right, even though the content overflows the 200 px cap.
4. While expanded, watch new reasoning stream in: confirm the area scrolls to keep the latest line visible.
5. Scroll up manually (wheel/drag): confirm subsequent streaming does NOT yank the view back to the bottom (auto-scroll paused).
6. Scroll back to the bottom: confirm auto-scroll resumes.
7. Collapse, then re-expand: confirm it opens scrolled to the bottom of the current content.

**Pass criterion**: steps 3–7 all hold. The area remains scrollable throughout (wheel works) despite the hidden scrollbar.

**Build gate**: `bazel build //projects/game/desktop/...` passes (the frontend has no unit-test infra — `style/large_test.md` + the 023/024 assumption: build + manual).

---

## Scenario 2 — Tool bubble: compact args, formatted result, collapsed body (US2, manual)

**What it proves**: FR-005 (compact args) + FR-006 (formatted result) + FR-007/008 (collapsible result body, independent screenshot toggle).

**Prerequisites**: A built desktop client running; a saolei-enabled session that calls `saolei_flag(7,7)` then `saolei_click(4,4)`.

**Steps**:
1. Trigger a turn that calls `saolei_flag(7,7)`.
2. Inspect the tool bubble: confirm the args render on ONE line next to the tool name (e.g. `saolei_flag {"x":7,"y":7}`), NOT split into 4 indented lines.
3. Trigger `saolei_click(4,4)`.
4. Inspect the resolved click bubble: confirm only the status icon + label (e.g. `› done`) are visible by default — the result body is COLLAPSED.
5. Click the toggle to expand: confirm the full message appears with its multi-line structure preserved (the `board size 9*9` header, blank line, and the grid rows are visible as a grid, not a run-on line).
6. (If a screenshot is present — native mouse tools) confirm the screenshot has its own separate sub-toggle, independent of the body toggle.

**Pass criterion**: steps 2, 4, 5 hold (step 6 if applicable). The args are single-line; the result is collapsed by default; the expanded message preserves newlines.

**Build gate**: `bazel build //projects/game/desktop/...` passes.

---

## Scenario 3 — Library: `isWin` predicate + golden win board (US3, unit)

**What it proves**: FR-009..011 (the predicate's pure logic + the real-screenshot win fixture).

**Prerequisites**: none (pure unit + golden test).

**Commands**:
```bash
bazel test //projects/game/pkg/saolei-board:lib_test
```

**What runs**:
- `win.test.ts` — synthetic grids: an all-revealed/all-flagged board → `isWin === true`; a board with any `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` cell → `isWin === false`.
- `golden.test.ts` (now includes `saolei_10`) — `recognizeBoard(testdata/saolei_10.png)` matches `testdata/saolei_10.golden.txt`, AND `isWin(that state) === true` (the real win screenshot is recognized as a win).

**Pass criterion**: all cases green. The `saolei_10` golden case proves recognition + win classification on a real win board; `win.test.ts` proves every branch of the predicate.

**Reference**: [data-model.md §1](../data-model.md), [contracts/saolei-mcp-status-contract.md §1](../contracts/saolei-mcp-status-contract.md).

---

## Scenario 4 — Agent: game-status line, `game_won`, chord-neighbor rejection (US4/US5, unit)

**What it proves**: FR-012..015 (status line in every result) + FR-016..020 (chord-neighbor rejection) + FR-021..023 (post-win `game_won` rejection). DI-based: a fake `OperationBridge` + a fake `SaoleiBoardApi` supply canned `GameState`s (no real recognition).

**Prerequisites**: none.

**Commands**:
```bash
bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test
```

**What runs** (extended `saolei-mcp.test.ts`):
- Every tool result on an in-progress board contains the line `game status: playing`.
- A canned WINNING `GameState` → the dispatching tool result contains `game status: won`; a following cell op is rejected with `game_won` (no FlowPart dispatched).
- A canned LOSING `GameState` (HIT_MINE) → the result contains `game status: lost`; a following cell op is rejected with `game_over` (existing) carrying `game status: lost`.
- `validateMove` on a chord target whose non-flag neighbors are all revealed numbers/flags (no `INITIAL`, no `UNKNOWN`) → `{ ok: false, reason: "chord_no_unrevealed_neighbor" }`; with an `INITIAL` or `UNKNOWN` neighbor → `{ ok: true }`.
- `no_active_game` and `unable to recognize` outcomes omit the status line.

**Pass criterion**: all assertions green.

**Reference**: [data-model.md §2..§5](../data-model.md), [contracts/saolei-mcp-status-contract.md §2..§5](../contracts/saolei-mcp-status-contract.md).

---

## Scenario 5 — Large test: status line + terminal rejections end-to-end (US3/US4, large)

**What it proves**: the `game status:` line and the `game_won` / `game_over` terminal rejections survive the full deployed-agent chain (MCP text result → `FlowResultPart` recognition → `ToolResultPart.message` → ListMessages) using the REAL recognition engine, across all three outcomes.

**Prerequisites**: the `testplan` skill (`style/large_test.md`); the deployed agent SUT. Testdata spans the three outcomes (copies placed in `projects/game/testplan/testdata/` during planning, mirroring `saolei_1`/`saolei_2`): win (`saolei_10.png`, 9×9), loss (`saolei_5.png`, 16×16 — contains `X`/`MINE`), in-progress (`saolei_1.png` — all INITIAL).

**Commands** (via the testplan skill):
```bash
guitar run projects/game/testplan/system_test.yaml
```
This deploys the SUT, runs the `agent-saolei` suite (`projects/game/testplan/agent_saolei_test.go`), and tears down.

**What runs** (the updated `agent_saolei_test.go`):
- **In-progress flow** (existing `TestAgentSaoleiTextBoardFlow`, extended): `saolei_init` → reply `saolei_1.png` → the init result text contains `game status: playing`; `saolei_click` → reply `saolei_1.png` → result contains `game status: playing`.
- **Win flow** (new): `saolei_init` → reply `saolei_10.png` (recognized as the 9×9 win board) → the init result text contains `game status: won`; a following `saolei_click(x,y)` is rejected with `game_won` — NO operation FlowPart reaches the desktop.
- **Loss flow** (new): `saolei_init` → reply `saolei_5.png` (a 16×16 loss board with `X`/`M`) → the init result text contains `game status: lost`; a following cell op is rejected with `game_over` (existing terminal-loss) whose body carries `game status: lost`.

**Pass criterion**: **all** test cases pass (Constitution §VI — a failed/flaky case means acceptance is not met; fix and re-run until fully green). Build-only checks do NOT constitute acceptance — the testplan MUST actually execute (`guitar run`), not merely compile the test target.

**Note on the chord-neighbor rule**: `chord_no_unrevealed_neighbor` is NOT covered here — it needs a non-terminal board where a number's non-flag neighbors are all revealed/flagged (no `INITIAL`/`UNKNOWN`), a configuration no testdata board exposes (the win board is terminal-won first). That rule is unit-test-verified (Scenario 4) — [research.md D12](../research.md).

**Reference**: [research.md D12](../research.md), [contracts/saolei-mcp-status-contract.md §2/§5](../contracts/saolei-mcp-status-contract.md).

---

## Cross-scenario notes

- **Compile + unit cadence** (Constitution IV): every code-change task includes `bazel build //projects/game/...` + `bazel test //projects/game/...` (relevant targets) as part of the task — Scenarios 3 and 4 are those unit gates, not separate "test tasks".
- **Large-test gate** (Constitution VI): Scenario 5 is the agent acceptance gate. US1/US2 (frontend) are Scenario 1/2 (build + manual) — no frontend unit-test infra exists.
- **Ordering suggestion for `/speckit.tasks`**: library win predicate (Scenario 3) → agent MCP status/validation/skill (Scenario 4) → desktop think bubble (Scenario 1) → desktop tool bubble (Scenario 2) → large-test update + run (Scenario 5). Each is independently verifiable; the library phase unlocks the agent phase; the large-test phase runs last as the acceptance gate.
