# Quickstart: Saolei Win-Detection Counter Cross-Check (028)

**Feature**: 028-saolei-win-counter-fix | **Purpose**: runnable validation that proves the feature works end-to-end.

This is a **validation/run guide** — it tells you what to run and what to observe. Implementation detail belongs in `tasks.md`. Commands use the repo's `bazel` entry (see `AGENTS.md`).

## Prerequisites

- The two badcase fixtures are present under `projects/game/pkg/saolei-board/testdata/`: `saolei_9.png` (grid all-revealed, 11 flags, counter `-01` — not won) and `saolei_11.png` (counter `000`, grid with `INITIAL` — not won). They are currently **untracked**; `git add` them as part of the change. The genuine-win fixture `saolei_10.png` (+ its `.golden.txt`) already exists.
- Read the contracts before validating: [contracts/saolei-board-api.md](./contracts/saolei-board-api.md) and [contracts/saolei-mcp-win-contract.md](./contracts/saolei-mcp-win-contract.md); types in [data-model.md](./data-model.md).

## Scenario 1 — Library: the counter decoder (US1, SC-001)

**Proves**: `decodeMineCounter` reads the correct value for each fixture.

```bash
# Unit + golden suite (includes the new counter decoder unit tests)
bazel test //projects/game/pkg/saolei-board:lib_test
```

**Expected**: all green. The new `counter.test.ts` asserts:
- `saolei_9.png` counter ⇒ `{ decoded: true, value: -1 }` (glyphs `-`, `0`, `1`).
- `saolei_10.png` counter ⇒ `{ decoded: true, value: 0 }`.
- `saolei_11.png` counter ⇒ `{ decoded: true, value: 0 }`.
- a synthetic undecodable region ⇒ `{ decoded: false }`.

**Manual cross-check** (optional, calibration): run the CLI in debug mode on each fixture and confirm the printed counter diagnostics match:

```bash
BAZEL_BINDIR=. ./bazel-bin/projects/game/pkg/saolei-board/cli_/cli \
  "$(pwd)/projects/game/pkg/saolei-board/testdata/saolei_9.png" --debug
# Observe: per-digit segment ON/OFF, glyphs ['-', '0', '1'], value -1.
```

## Scenario 2 — Library: the strengthened `isWin` (US2, SC-002/SC-003/SC-005)

**Proves**: the conjunction (grid-revealed AND counter `000`) classifies the three fixtures correctly.

```bash
bazel test //projects/game/pkg/saolei-board:lib_test
```

**Expected**: `golden.test.ts` win-classification assertions:
- `saolei_9` (all-revealed grid, counter `-01`) ⇒ `isWin(state) === false` (the false-positive fix).
- `saolei_11` (counter `000`, grid has `INITIAL`) ⇒ `isWin(state) === false`.
- `saolei_10` (both conditions) ⇒ `isWin(state) === true` (unchanged).

`win.test.ts` additionally covers the pure-logic boundary cases: all-revealed + `mineCounter undefined` ⇒ `false` (lenient, SC-005); all-revealed + decoded `000` ⇒ `true`.

## Scenario 3 — Agent MCP unit: counter-informed `won` (FR-012, SC-004)

**Proves**: the MCP derives `game status:` from the strengthened predicate; the text contract is unchanged.

```bash
bazel test //projects/game/agent/src/mcp/saolei/...
```

**Expected**: green. `saolei-mcp.test.ts` cases:
- A canned all-revealed board with `mineCounter = { decoded:true, value:0 }` ⇒ result body contains `game status: won`; a following cell op is rejected `game_won` (the genuine-win path preserved).
- A canned all-revealed board with `mineCounter = { decoded:true, value:-1 }` (the saolei_9-style over-flag) ⇒ result body contains `game status: playing`; a following cell op is **allowed** (the bug fix — no false `game_won`).
- A loss board (`HIT_MINE`) ⇒ `game status: lost`; `game_over` (unchanged).

## Scenario 4 — Agent large test (Constitution §VI acceptance, SC-004)

**Proves**: the integrated service behaves end-to-end. This is the **acceptance gate** — build-only does NOT constitute acceptance.

Use the **testplan** skill (loads `tools/test/guitar`). Before writing/extending a plan, read `style/large_test.md`.

```bash
# Validate + run the saolei large-test plan (full deploy → test → cleanup)
# via the testplan skill: guitar run <plan.yaml>
```

**Expected (all cases MUST pass)**:
- The genuine-win flow (a winning board whose counter reads `000`) ⇒ `game status: won` and a following cell op rejected `game_won` (preserved).
- The over-flag flow (a grid-only-would-be-win board whose counter reads non-zero) ⇒ `game status: playing` and the following cell op **dispatched** (the false-positive fix).
- The loss flow ⇒ `game status: lost` and `game_over` (unchanged).

If any case fails or is flaky, the feature is NOT accepted — fix and re-run until fully green.

## Scenario 5 — Docs (FR-013)

**Proves**: the corrected win rule is documented for the model and operators.

- `projects/game/pkg/saolei-board/README.md` documents the mine-counter cross-check (win = grid fully revealed/flagged **and** counter `000`) and the counter `--debug` calibration.
- `projects/game/agent/src/skill/saolei/SKILL.md` documents the win condition (the skill is the model's authority on the result format — [027 FR-024](../027-chat-bubble-game-state/spec.md)).

## Full build/test sweep (run before declaring done)

```bash
bazel build //...
bazel test //projects/game/pkg/saolei-board/... //projects/game/agent/src/mcp/saolei/...
```

Then the large test via the testplan skill (Scenario 4). All green + large test all-pass = feature accepted.
