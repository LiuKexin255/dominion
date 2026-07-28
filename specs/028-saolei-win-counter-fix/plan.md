# Implementation Plan: Saolei Win-Detection Counter Cross-Check (False-Positive Fix)

**Branch**: `028-saolei-win-counter-fix` | **Date**: 2026-07-28 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/028-saolei-win-counter-fix/spec.md`

## Summary

The saolei-board library's `isWin` predicate is grid-only — it reports a win whenever the recognized grid has no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` cell. That produces **false-positive wins**: a board can look fully revealed/flagged yet not be won (the player over-flagged, so flags ≠ mines). The fix adds a **mine-counter cross-check**: recognition additionally decodes the top-left 3-digit red LED (the game's own `mines − flags` display), and `isWin` returns `true` **only when** the grid is fully revealed/flagged **and** the counter reads exactly `000`. The technical approach — fixed-geometry 7-segment segment-core decoding — is validated against the fixtures with a clean margin (ON segments ≥ 90% red in their core, OFF segments 0%), so no OCR or ML is needed.

The counter is carried as an optional `mineCounter` field on `GameState` (the recognized-board state the MCP already threads everywhere), keeping `isWin` a single-argument pure function and leaving the [027] MCP text-result contract (`game status:` line, `game_won` rejection) unchanged in wording — only the `won` decision becomes counter-informed. See [research.md](./research.md) for the algorithm + API-shape decisions and [data-model.md](./data-model.md) for the types.

## Technical Context

**Language/Version**: TypeScript (library `@dominion/game-saolei-board`, `projects/game/pkg/saolei-board/`); the single downstream consumer is the agent MCP (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`), also TypeScript.

**Primary Dependencies**: `pngjs` (pure-JS PNG decode, already a dependency — `projects/game/pkg/saolei-board/src/core/decode.ts`); `vitest` (tests). No new dependency.

**Storage**: N/A (pure in-memory recognition; the counter is decoded per screenshot and not persisted).

**Testing**: `vitest` unit + golden suite (`projects/game/pkg/saolei-board/src/core/*.test.ts`, `golden.test.ts`); the agent MCP DI-based unit test (`projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`); the agent large test (`projects/game/testplan/agent_saolei_test.go`, run via the testplan skill). Build/test entry is `bazel`.

**Target Platform**: Node.js (library + CLI); the agent runs as a service. Recognition targets the classic Win32 Microsoft Minesweeper (`winmine.exe`) screenshot appearance.

**Project Type**: library (the recognition core) + a one-line consumer change in a service (the saolei MCP).

**Performance Goals**: recognition is a single synchronous pass over a 332×508 screenshot; the counter decode adds 3 digit cells × 7 small segment rectangles ≈ a few hundred pixel reads — negligible vs. the 9×9 grid scan already performed.

**Constraints**: `isWin` MUST remain a pure function (FR-009). The MCP text-result contract MUST be unchanged in wording/shape (FR-012). No OCR / no native dependency / no new external library (pngjs only). Counter decode MUST be robust to the LED's anti-aliased red-on-black rendering and to a leading minus sign (FR-004).

**Scale/Scope**: confined to `projects/game/pkg/saolei-board/src/core/` (new counter decoder + `isWin` + types + tests + 2 fixture assertions) and the single `isWin` consumption site in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (+ its test + the large test + README/SKILL docs). No proto change.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.* Constitution version 1.3.0 (`.specify/memory/constitution.md`).

| Principle / Gate | Status | Evidence |
|---|---|---|
| **§I Citation & Provenance** | ✅ pass | Every design decision in [research.md](./research.md) cites the fixture pixel analysis (first-party) or the external reference; data-model/contracts cite source files by repo-relative path. |
| **§II Refactor Over Patching** | ✅ pass | The counter is modelled as a first-class recognition output (a typed `MineCounter` on `GameState`), not a special-case patch over `isWin`. The 7-segment decoder is a peer of `classify.ts` (fixed-geometry + colour analysis), reusing the existing decode/geometry infrastructure rather than layering a parallel path. See [research.md](./research.md) D8 for the API-shape decision and rejected alternatives. |
| **§III Interface-First Design** | ✅ pass | The new/changed interfaces are specified before implementation in [data-model.md](./data-model.md) and [contracts/](./contracts/): the `MineCounter` type, the optional `GameState.mineCounter` field, the `decodeMineCounter` recognition function, the `CounterProfile` decode profile, and the strengthened `isWin` contract. The MCP consumption contract is unchanged in wording (additive accuracy only). |
| **§IV Test Granularity & Cadence** | ✅ pass | Compile + unit/golden tests run per code change as part of the dev task (not a separate task). The large test is the acceptance gate (see §VI). |
| **§V Read Before Code** | ⏳ deferred to tasks | tasks.md will list per-phase documents (code style `style/javascript.md`, the library files touched, this plan's research/data-model/contracts). N/A at plan time. |
| **§VI Large-Test Acceptance (services)** | ⚠️ applies — large test MUST be updated & executed | The agent is a service SUT with an existing large test (`projects/game/testplan/agent_saolei_test.go`) that asserts `game status: won/lost/playing`. The false-positive fix changes *when* `won` is reported, so the large test MUST be extended to cover the counter-informed win (a grid-only-would-be-win board with counter ≠ `000` ⇒ `playing`) and MUST be executed via the testplan skill (full deploy→test→cleanup) with all cases passing. A win still surfaces `game status: won` and `game_won` (the genuine-win path, saolei_10) is preserved. |

**Post-Phase-1 re-check**: the design (data-model + contracts) preserves all gates — `isWin` stays pure (§III/FR-009), the MCP text contract is unchanged (§III/FR-012), and the large-test acceptance path is identified (§VI). No violations to justify; no Complexity Tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/028-saolei-win-counter-fix/
├── plan.md              # This file
├── research.md          # Phase 0 — algorithm + API-shape decisions (grounded in fixture pixel analysis)
├── data-model.md        # Phase 1 — MineCounter type, GameState change, CounterProfile, decode model
├── quickstart.md        # Phase 1 — validation guide (unit/golden + MCP unit + large test)
├── contracts/
│   ├── saolei-board-api.md      # Library public API: decodeMineCounter + strengthened isWin
│   └── saolei-mcp-win-contract.md  # MCP consumption: counter-informed won decision, text contract unchanged
└── tasks.md             # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/pkg/saolei-board/src/core/
├── counter.ts           # NEW — fixed-geometry 7-segment mine-counter decoder (decodeMineCounter)
├── counter.test.ts      # NEW — unit tests for the decoder (synthetic + fixture-derived cases)
├── types.ts             # EDIT — add MineCounter type + optional GameState.mineCounter + CounterProfile
├── geometry.ts          # EDIT — add the counter-region + per-digit-cell screenshot-space constants
├── recognize.ts         # EDIT — recognizeBoard / SaoleiBoard decode the counter and populate state.mineCounter
├── win.ts               # EDIT — isWin now also requires mineCounter decoded & value === 0
├── win.test.ts          # EDIT — positive cases set a decoded 000 counter; add counter≠000 / undecodable cases
├── render.ts            # UNCHANGED — text board is grid-only (counter is not rendered)
├── validate.ts          # UNCHANGED in logic — checkCompatible ignores mineCounter (counter is non-monotonic)
├── golden.test.ts       # EDIT — add saolei_9 / saolei_11 win-classification assertions (⇒ false); saolei_10 stays true
├── classify.ts          # UNCHANGED
├── decode.ts            # UNCHANGED (reused: getRGB / decoded image)
└── index.ts             # EDIT — export decodeMineCounter, MineCounter, CounterProfile
projects/game/pkg/saolei-board/src/cli/
└── cli.ts                # EDIT — surface the decoded counter in --debug diagnostics
projects/game/pkg/saolei-board/testdata/
├── saolei_9.png         # ALREADY PRESENT (untracked) — badcase: grid all-revealed, counter -01 ⇒ not won
├── saolei_11.png        # ALREADY PRESENT (untracked) — badcase: counter 000 + INITIAL ⇒ not won
└── (saolei_9 / saolei_11 .golden.txt added only if the calibration flow requires board output)
projects/game/agent/src/mcp/saolei/
├── saolei-mcp.ts        # EDIT — isWin/isTerminalState/gameStatus/validateMove now read state.mineCounter (signatures unchanged: still GameState)
└── saolei-mcp.test.ts   # EDIT — win test cases supply a decoded counter; add the counter≠000 ⇒ playing case
projects/game/agent/src/skill/saolei/SKILL.md  # EDIT — document the counter-informed win condition
projects/game/pkg/saolei-board/README.md        # EDIT — document the counter cross-check + calibration
projects/game/testplan/agent_saolei_test.go     # EDIT — extend large test for counter-informed win (SC-004)
```

**Structure Decision**: Single-project library change (`projects/game/pkg/saolei-board/`) + its one service consumer (`projects/game/agent/`). The new `counter.ts` is a peer of `classify.ts` (both consume `decode.ts`'s decoded image and use fixed `geometry.ts` constants). No new package, no proto change, no new external dependency. The two badcase PNGs are already in `testdata/` (untracked); only their test assertions are added.

## Complexity Tracking

> No Constitution-Check violations to justify. (The optional `mineCounter` field on `GameState` is a model extension, not over-engineering — see [research.md](./research.md) D8 for why the wrapper/2-arg alternatives were rejected as higher-churn for the same correctness gain.)
