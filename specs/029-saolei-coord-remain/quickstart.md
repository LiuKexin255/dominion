# Quickstart: Saolei Board Coordinate Ruler & Remain Tool

**Feature**: `029-saolei-coord-remain` | **Date**: 2026-07-28 | **Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

Runnable validation scenarios that prove the feature works end-to-end. Implementation details belong in `tasks.md`; this is a validation/run guide. Format references: [saolei-board-render-contract.md](contracts/saolei-board-render-contract.md), [saolei-remain-tool-contract.md](contracts/saolei-remain-tool-contract.md); data shapes in [data-model.md](data-model.md).

Large-test organisation follows `style/large_test.md`: scenarios are **cases in the existing** `projects/game/testplan/system_test.yaml` suite `agent-saolei` → `projects/game/testplan/agent_saolei_test.go`, **not** a new test-plan YAML. Per Constitution §VI, acceptance = actual execution via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), all cases passing — a build check alone is NOT acceptance.

---

## Prerequisites

- A Bazel-working checkout (`bazel build //...` clean).
- The `testplan` skill available (`tools/test/guitar`) for the large-test scenarios.
- No new external dependency; `@dominion/game-saolei-board` is an existing workspace package.

---

## Scenario 1 — Library renderer: ruler + `renderGridWithRuler` (unit)

**File**: `projects/game/pkg/saolei-board/src/core/render.test.ts` (updated) + `renderGridWithRuler` cases.

**Run**:
```bash
bazel test //projects/game/pkg/saolei-board:lib_test
```

**Pass criteria**:
1. `renderGridWithRuler(width, height, tokenAt)` produces a header row (blank first slot + tagged column labels `col0`, `col1`, …) followed by `height` data rows (each prefixed with its tagged row label `row<y>`); every slot is right-aligned to the computed `columnWidth`.
2. A ≤9-wide grid renders at `columnWidth = 4` (labels `col0`..`col8`); a ≥10-wide grid renders at `columnWidth = 5` (labels `col10`.. aligned under the wider `col10`/`row10`).
3. `renderBoardText(state)` = `board size <w>*<h>` + blank + the ruled, tagged grid (symbols via the unchanged legend). The `startsWith("board size …\n\n")` assertion still holds.

---

## Scenario 2 — Library golden fixtures regenerated (unit/golden)

**Files**: `projects/game/pkg/saolei-board/testdata/*.golden.txt` (all 9 regenerated), `src/core/golden.test.ts` (code unchanged).

**Regenerate** (per `projects/game/pkg/saolei-board/README.md` "校准与 Golden 测试"):
```bash
for n in 1 2 3 4 5 6 7 8 10; do
  bazel run //projects/game/pkg/saolei-board:cli -- testdata/saolei_${n}.png > testdata/saolei_${n}.golden.txt
done
```

**Pass criteria**:
1. Each regenerated `.golden.txt` begins with `board size <w>*<h>`, a blank line, then the tagged column-label header row (`col0`…) and the ruled symbol grid (each row prefixed `row<y>`).
2. `bazel test //projects/game/pkg/saolei-board:lib_test` is green (the exact-equality golden match holds against the new renderer).
3. The recognition-only assertions in `golden.test.ts` (`isWin`, `mineCounter` decode for saolei_9/10/11) remain green — recognition is untouched.

**Notes**: `recognize.test.ts` exact assertions (`renderText()` / `renderBoardText`) are updated to the tagged, ruled format (e.g. `board size 2*1\n\n     col0 col1\nrow0    1    0`).

---

## Scenario 3 — Agent: `saolei_remain` tool (unit)

**File**: `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` (new cases).

**Run**:
```bash
bazel test //projects/game/agent:lib_test     # (the agent unit-test target — confirm exact label in tasks.md)
```

**Pass criteria**:
1. Listing tools returns exactly five: `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_remain` (`saolei_update` still absent).
2. On a recognized board with a `3` cell that has one adjacent `F`, `saolei_remain()` returns a single text block containing `saolei_remain → computed`, `game status: playing`, `board size …`, and a ruled remain grid with `2` at that cell and `-` elsewhere.
3. The fake `OperationBridge` records **zero** dispatches for the `saolei_remain` call; a subsequent `saolei_click` still returns the same board (state unmutated).
4. With no recognized state, `saolei_remain()` returns `rejected: no_active_game` (no `game status:` line, no grid).
5. On a terminal `won`/`lost` board, `saolei_remain()` returns the grid with the corresponding `game status:` line (not rejected).
6. An over-flagged `1` cell with two adjacent `F` yields remain token `-1` (raw, not clamped).

Also confirm the existing ruler-on-board behaviour at the unit level: a `saolei_init` / `saolei_click` / rejection body's grid carries the tagged column header (`col0`…) + tagged row prefix (`row0`…) (the existing `toContain("board size …")`, `toContain("game status: …")` assertions stay green).

---

## Scenario 4 — Skill updated (unit/manual)

**File**: `projects/game/agent/src/skill/saolei/SKILL.md`; the `skill-loader.test.ts` name/body assertions still hold.

**Pass criteria**: the skill body describes the ruler in the board output and documents `saolei_remain` (read-only, no-dispatch, number-minus-flags / `-` rule, negative-over-flag signal); the tool count wording is updated to five tools. `bazel test //projects/game/agent:lib_test` (skill-loader cases) stays green.

---

## Scenario 5 — End-to-end `saolei_remain` over the deployed agent (large)

**Plan**: `projects/game/testplan/system_test.yaml` suite `agent-saolei` → `projects/game/testplan/agent_saolei_test.go` (new case added).

**Acceptance gate** (Constitution §VI — MUST execute, not merely build):
```bash
guitar run projects/game/testplan/system_test.yaml
```

**Pass criteria** (all cases pass; the suite's existing `strings.Contains("game status: …")` assertions are ruler-invariant):
1. Deploy the agent SUT via the suite's deploy config (fake-LLM fixture driving saolei tools).
2. Drive `saolei_init` (dispatches `KeyboardPressPart{F2}`; recognizes saolei_1.png → 16×16 board) → the result text contains `new game started`, `game status: playing`, and `board size 16*16` (the ruled grid is returned as text).
3. Drive `saolei_remain()` → the result text contains `saolei_remain → computed`, `game status: playing`, and `board size 16*16`; and **no** `FlowPart` operation is dispatched for this call (the desktop receives nothing).
4. The existing init/click/flag/chord + status flows in the suite remain green (ruler does not change `game status:` substrings or the dispatched proto operations).

If any case fails or is flaky, the acceptance is NOT met — fix and re-run until all cases pass.

---

## Notes for the implementer

- Compile + unit-test after every code change (`bazel build //...` + the relevant `bazel test` targets) — these are part of the dev task, not separate tasks (Constitution §IV).
- The large test (Scenario 5) is the service acceptance gate (Constitution §VI); run it via the `testplan` skill after the unit scenarios are green.
- The ruler format source of truth is [saolei-board-render-contract.md](contracts/saolei-board-render-contract.md) §1/§2 — derive every expected string (tests, goldens) from it.
