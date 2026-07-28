# Implementation Plan: Saolei Board Coordinate Ruler & Remain Tool

**Branch**: `029-saolei-coord-remain` | **Date**: 2026-07-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/029-saolei-coord-remain/spec.md`

## Summary

Two refinements to the saolei MCP that the LLM uses to play Minesweeper:

1. **Coordinate ruler on every text board.** The shared board-text renderer (`@dominion/game-saolei-board` `renderBoardText`, consumed by the four saolei MCP tools, the `saolei-recognize` CLI, and the library's golden tests) now emits a **column-index header row** above the grid and a **row-index prefix** on every row, 0-based, top-left origin, consistent with the `(x, y)` arguments the cell tools already take. Each index is **tagged** (`col<N>` / `row<N>`, e.g. `col3`, `row1`) so it cannot be confused with the `0`–`8` game-state cell values. The ruler is produced by a single source of truth — a new exported `renderGridWithRuler(width, height, tokenAt)` helper that right-aligns every slot (tagged indices and tokens) to a computed column width, so it stays aligned for both 9×9 and 16×16 boards.
2. **New read-only `saolei_remain()` tool.** A fifth saolei MCP tool that takes no arguments, dispatches nothing to the desktop, and returns a **remain grid** — for each revealed number `1`–`8`, `number − adjacent flags` (raw, possibly `0` or negative); `-` for every other cell (`0`, `*`, `F`, `X`, `M`, `?`). It rejects with `no_active_game` when no board is recognized (like the cell tools) but is not blocked by terminal states (pure query). The remain grid reuses the same `renderGridWithRuler` so its coordinate ruler is identical to the board grid's.

The desktop-facing operation contract of the four existing tools is unchanged; the desktop renders tool-result text verbatim and does not parse the board grid, so it needs no change.

## Technical Context

**Language/Version**: TypeScript (agent + library), ES module target via `ts_project`/`swc`; Go (large-test cases in `projects/game/testplan`).

**Primary Dependencies**: `@modelcontextprotocol/sdk` (MCP server), `zod` (input schema), `@dominion/game-saolei-board` (recognition + text rendering — the library this feature modifies), `vitest` (unit/golden tests). No new external dependency.

**Storage**: N/A — recognized board state is in-memory, per-session, in the MCP server closure (`saolei-mcp.ts` `recognized`), co-located with the LangChain checkpoint (inherited from 025 §2). The remain grid is computed on demand, never persisted.

**Testing**: `vitest` (library `:lib_test`, agent unit tests); large tests via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`, suite `agent-saolei`).

**Target Platform**: Linux server (agent service) + the classic Win32 Minesweeper window reached through the desktop; single supervised session.

**Project Type**: library (`@dominion/game-saolei-board`) + service (the agent's per-session saolei MCP server).

**Performance Goals**: N/A — rendering and remain computation are O(width × height) over a ≤30×16 grid; negligible vs. the desktop round-trip that dominates every tool call.

**Constraints**: The board-text format is a load-bearing contract consumed by the model, the CLI, golden fixtures, and (verbatim) the desktop chat bubble. The recognition algorithm is untouched — only the **rendering** changes.

**Scale/Scope**: Single supervised session per operator. Touches one library (`projects/game/pkg/saolei-board`), the agent saolei MCP + skill + unit tests, and one large-test file. No new project, no new external dependency, no proto change.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Status | Evidence |
|---|-----------|--------|----------|
| 1 | **I — Citation & Provenance** | PASS | Plan, contracts, and code comments cite repo-relative paths (`projects/game/pkg/saolei-board/src/core/render.ts`) and the refined specs (025 §3, 027 §2). |
| 2 | **II — Refactoring Over Patching** | PASS | The ruler is added at the **single source of truth** (`renderBoardText` / new `renderGridWithRuler`), not patched into each MCP tool body; the remain tool reuses the existing `neighbors` helper and the shared grid renderer. Both grids draw their ruler from one implementation (no duplicated ruler logic). |
| 3 | **III — Interface-First Design** | PASS | Interface contracts settled BEFORE implementation: [contracts/saolei-board-render-contract.md](contracts/saolei-board-render-contract.md) (the ruler format + `renderGridWithRuler` export + golden impact) and [contracts/saolei-remain-tool-contract.md](contracts/saolei-remain-tool-contract.md) (the `saolei_remain` tool surface, body shape, remain computation, `no_active_game`, terminal-not-blocked). |
| 4 | **IV — Test Granularity & Cadence** | PASS | Library + agent unit/golden tests updated and added per change (compile + `bazel test` every change, not a separate task). Large-test acceptance is a separate task (gate 6). |
| 5 | **V — Read Before Code** | PASS (planned) | tasks.md will list the style docs (`style/javascript.md`), the refined contracts (025/027/028), and this feature's contracts per phase. |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED and MUST be executed via the `testplan` skill. The existing `agent-saolei` suite is re-run (its `strings.Contains` assertions on `game status:` are ruler-invariant), and a new `saolei_remain` E2E case is added (init → `saolei_remain` returns the remain grid with **zero** dispatched operations). Acceptance = `guitar run projects/game/testplan/system_test.yaml`, all cases pass. |

## Project Structure

### Documentation (this feature)

```text
specs/029-saolei-coord-remain/
├── plan.md                              # This file
├── research.md                          # Phase 0 — design decisions
├── data-model.md                        # Phase 1 — entities (text board w/ ruler, remain grid)
├── quickstart.md                        # Phase 1 — validation scenarios (unit + large)
├── contracts/
│   ├── saolei-board-render-contract.md  # Phase 1 — ruler format + renderGridWithRuler + golden impact
│   └── saolei-remain-tool-contract.md   # Phase 1 — the saolei_remain MCP tool
└── tasks.md                             # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/pkg/saolei-board/          # LIBRARY — ruler lives here (single source of truth)
├── src/core/
│   ├── render.ts                        # ADD renderGridWithRuler; renderBoardText gains ruler via it
│   ├── render.test.ts                   # UPDATE exact-match assertions for the ruler
│   ├── index.ts                         # EXPORT renderGridWithRuler
│   ├── recognize.test.ts                # UPDATE 2 exact-match assertions (renderText/renderBoardText)
│   └── golden.test.ts                   # UNCHANGED code; testdata/*.golden.txt REGENERATED (9 files)
├── testdata/
│   └── *.golden.txt                     # REGENERATE all 9 (ruler added to each)
└── src/cli/cli.ts                       # UNCHANGED (prints renderBoardText → auto gains ruler)

projects/game/agent/                     # SERVICE — new tool + skill
├── src/mcp/saolei/
│   ├── saolei-mcp.ts                    # ADD saolei_remain tool; reuse neighbors/renderGridWithRuler
│   └── saolei-mcp.test.ts               # ADD remain cases; existing substring asserts survive ruler
└── src/skill/saolei/SKILL.md            # UPDATE: document ruler + saolei_remain

projects/game/testplan/                  # LARGE TESTS
├── system_test.yaml                     # suite agent-saolei (re-run; add remain case config if needed)
└── agent_saolei_test.go                 # ADD saolei_remain E2E case (no-dispatch query)
```

**Structure Decision**: Single-library + single-service change across the existing `projects/game/{pkg/saolei-board, agent, testplan}` tree. No new project, no new external dependency, no proto change. The ruler is anchored in the shared library renderer (Constitution §II); the new tool is anchored in the agent saolei MCP. Deleted files: none.

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. The ruler is a single-source-of-truth refactor (Principle II); the remain tool is a small, self-contained read-only addition reusing existing helpers. No entries.
