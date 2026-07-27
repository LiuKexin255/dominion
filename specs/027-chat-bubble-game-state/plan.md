# Implementation Plan: Chat Bubble UX Polish & Saolei Game-State Awareness

**Branch**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/027-chat-bubble-game-state/spec.md` (six polish/correctness gaps across the desktop chat UI, the saolei-board library, and the saolei MCP), plus Clarification Session 2026-07-27: a recognized win is a terminal state symmetric with a loss → post-win cell operations are rejected with a new `game_won` reason.

## Summary

Six targeted refinements of surfaces introduced by [023](../023-saolei-mcp-refine/spec.md) / [024](../024-tool-render-coord-fix/spec.md) / [025](../025-desktop-image-state-refine/spec.md), grouped into five user stories. No architectural change; no proto change; no new external dependency.

1. **Think-bubble polish (US1)** — hide the visible scrollbar on the `.thinking-content` overflow area (CSS-only, scroll capability preserved), and make the expanded bubble auto-scroll to follow the streaming reasoning — pausing when the operator scrolls up and resuming when they return to the bottom (the standard chat-auto-scroll pattern, modelled on the existing chat-thread `$effect` in `ChatView.svelte`).
2. **Tool-bubble polish (US2)** — render tool-call args **compact** (single-line, replacing the `prettyArgs` 2-space pretty-print); preserve the result message's native formatting (`white-space: pre-wrap`) so the saolei text board is readable; and collapse the result body behind a `<details>` toggle (status icon + label always visible) so a saolei turn no longer dumps the whole board into the conversation.
3. **saolei-board win predicate (US3)** — export a new pure `isWin(state)` predicate from `@dominion/game-saolei-board` (true iff no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` cell — every cell is a revealed number or a flag; lenient on `UNKNOWN`).
4. **saolei MCP game-status output (US4)** — every tool result gains a `game status: won|lost|playing` line (derived via the US3 predicate + the existing loss signal); and a recognized win is a **terminal state** symmetric with a loss, so any cell operation after a win is rejected with a new `game_won` reason (`saolei_init` still restarts).
5. **saolei_chord_click neighbor validation (US5)** — reject a chord whose non-flag neighbors contain no `INITIAL` (and no `UNKNOWN`) cell — i.e. nothing for the chord to reveal — with a new `chord_no_unrevealed_neighbor` reason.

Technical decisions are grounded in [research.md](./research.md) (D1..D12); the MCP text-result format + validation rules in [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md); the desktop bubble rendering rules in [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md); the entity shapes (win predicate, game-status derivation, `MoveRejection` union update, bubble render state) in [data-model.md](./data-model.md).

## Technical Context

**Language/Version**: TypeScript (agent `projects/game/agent`, Node.js; library `projects/game/pkg/saolei-board`) + Svelte 5 runes (desktop frontend `projects/game/desktop/frontend`). proto3 (`projects/game/game.proto`) — **unchanged** by this feature.

**Primary Dependencies**:
- *Existing (agent TS)*: `@langchain/langgraph`, `@langchain/core`, `zod`, `@modelcontextprotocol/sdk`, `@dominion/game-saolei-board` (workspace package, wired in 025).
- *Existing (library)*: `pngjs` (PNG decode, used by recognition — the win predicate adds no new dep).
- *Existing (frontend)*: Svelte 5 (`$state`/`$effect`/`$derived`), `marked`, `dompurify`, Vite.
- *No new external dependency* is introduced. **No proto change.**

**Storage**: In-memory only. The per-session recognized saolei board (in the per-session MCP server, 025 FR-013) and the LangChain `MemorySaver` checkpoint are co-located on the agent and lost together on restart. The win predicate is pure (evaluated against the in-memory state, no persistence).

**Testing**:
- *Library* (`@dominion/game-saolei-board`): `vitest` (`vitest_test` macro) — a new `win.test.ts` covers the predicate's pure logic; the golden suite gains a **win-board case** (`saolei_10`, added for this feature — `testdata/saolei_10.png` + generated `saolei_10.golden.txt`) and a golden-coupled assertion that `isWin` returns `true` on its recognized state. The golden suite's recognition output for existing cases is **unaffected** (the predicate does not change recognition).
- *Agent*: `vitest` — `saolei-mcp.test.ts` is extended for the game-status line, `game_won` terminal, and the chord-neighbor rejection (DI-based: fake `OperationBridge` + fake `SaoleiBoardApi`, no real recognition).
- *Frontend* (`projects/game/desktop/frontend`): **no unit-test infra** — its `BUILD.bazel` declares only `vite_build` (no `vitest_test`, no `*.test.ts`); US1/US2 are verified by `bazel build` + manual, consistent with the 023/024 assumption.
- *Large tests*: `testplan` skill (`style/large_test.md`); the agent is the large-test SUT (Constitution §VI). The testdata now spans all three outcomes — win (`saolei_10`), loss (`saolei_5`), in-progress (`saolei_1`/`saolei_2`) — so `game status: won|lost|playing`, the `game_won` post-win rejection, and the (existing) `game_over` post-loss rejection are all large-testable with real recognition (research.md D12).

**Target Platform**: Agent = Linux container (deployed via `oci_image`). Desktop = Windows (Wails v2, WebView2/Chromium webview — relevant to the scrollbar-hiding CSS in US1).

**Project Type**: web-service (agent) + desktop-app (desktop) + shared library (saolei-board). A multi-project change confined to the existing `projects/game/{agent/, desktop/, pkg/saolei-board/}` tree. No new project.

**Performance Goals**: None. The win predicate is a single O(w·h) grid pass; the game-status derivation is constant-time after the predicate; the chord-neighbor scan is O(1) (8 cells); the bubble-rendering changes are CSS + a small reactive `$effect`. No new latency on any hot path.

**Constraints**:
- No proto change, no new external interface, no new `MessagePart`/`FlowPart` kind (the status line is part of the existing single MCP text content block — 025 FR-012 preserved).
- The desktop-facing operation contract (`KeyboardPressPart{F2}` for `saolei_init`; `MouseMoveAndClickPart` at fixed client-space geometry with `WINDOW_MESSAGE` for cell ops — 024/025) is **unchanged**.
- The four-tool surface (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) is **unchanged**.
- protojson omits default-value fields; the frontend is already robust to this (024 FR-002) — unaffected here.

**Scale/Scope**: Single supervised session per operator. The change touches two Svelte components, one agent MCP module (+ its test), one library file (+ a new test + the barrel export), one agent skill doc, and the large-test suite. No new project or external dependency.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.3.0). All principles satisfied; no unjustified complexity. Quality gates (in execution order):

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc reading gate) | PASS (planned) | `tasks.md` (next command) MUST declare per-phase docs under the three mandatory categories (代码规范文档 / 官方文档 / 技术文章). Required reading includes `style/javascript.md`, `style/large_test.md`, the saolei-board README, this feature's contracts + data-model, and (for US1) the CSS scrollbar-styling references cited in research.md D1. |
| 2 | **III — Interface-First Design** | PASS | Interfaces designed in Phase 1 before implementation: [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md) fixes the text-result format (status-line position), the `MoveRejection` union additions (`game_won`, `chord_no_unrevealed_neighbor`), and the win-predicate signature; [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md) fixes the think/tool bubble render rules. No new external/RPC/HTTP/proto interface is introduced (internal module + renderer refinements only). |
| 3 | **II — Refactoring Over Patching** | PASS | Each change completes or simplifies an existing surface rather than stacking a patch: US1/US2 finish the 024 tool/think-bubble renderer (compact args, formatted result, collapsible body, clean scroll); US3 factors the win rule into the library where the recognized state already lives (one pure predicate, not an agent-side re-derivation); US4 makes win/loss terminal handling symmetric (one terminal-state concept, not a special-case); US5 adds one rule to the existing strict validator in the existing rule order. No new layer of indirection. |
| 4 | **I — Citation & Provenance** | PASS | All design docs cite sources: repo-relative paths for internal refs, full URLs for external (CSS scrollbar-styling, MDN, prior spec/contract/code paths). See References in [spec.md](./spec.md) and each artifact. |
| 5 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build //projects/game/...`) + unit (`bazel test //projects/game/...`) per code-change task, as part of the task — not separately tasked. Library win predicate → `win.test.ts`; agent status/game_won/chord → `saolei-mcp.test.ts`; frontend → `bazel build` (no frontend unit-test infra — see Testing). |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | The agent is a service; large tests REQUIRED and MUST be executed via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), not merely built. The `agent-saolei` suite (`agent_saolei_test.go`) is UPDATED to assert the `game status:` line and the terminal rejections end-to-end across all three outcomes — win (`saolei_10`), loss (`saolei_5`), in-progress (`saolei_1`) — using the REAL recognition engine on real screenshots; the `chord_no_unrevealed_neighbor` rule (a narrow non-terminal configuration no testdata board exposes) is unit-test-verified via DI (research.md D12). All large-test cases MUST pass. |

No violations. No Complexity Tracking entries required.

## Project Structure

### Documentation (this feature)

```text
specs/027-chat-bubble-game-state/
├── plan.md                              # This file
├── research.md                          # Phase 0 — decisions D1..D12
├── data-model.md                        # Phase 1 — win predicate, game-status, MoveRejection update, bubble render state
├── quickstart.md                        # Phase 1 — validation scenarios 1..5
├── contracts/
│   ├── saolei-mcp-status-contract.md    # Phase 1 — text-result format + win/loss terminal + chord-neighbor validation
│   └── desktop-bubble-render-contract.md # Phase 1 — think-bubble scrollbar/auto-scroll + tool-bubble args/result/collapse
└── tasks.md                             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── pkg/saolei-board/                            # recognition library (US3)
│   ├── src/core/
│   │   ├── win.ts                               # NEW: isWin(state) pure predicate (FR-009..011)
│   │   ├── win.test.ts                          # NEW: unit tests for isWin (all-revealed, INITIAL/HIT_MINE/MINE/UNKNOWN → false)
│   │   ├── golden.test.ts                       # UPDATE: + "saolei_10" in CASES (real win board); + isWin-on-golden assertion
│   │   └── index.ts                             # + export { isWin } (barrel)
│   └── testdata/
│       ├── saolei_10.png                        # ADDED (user-supplied): real win board screenshot (9×9, all revealed/flagged)
│       └── saolei_10.golden.txt                 # ADDED (generated via CLI): expected recognized board; isWin → true
│
├── agent/                                       # TypeScript agent (US4/US5)
│   └── src/
│       ├── mcp/saolei/
│       │   ├── saolei-mcp.ts                    # + import isWin; + gameStatus(state) helper;
│       │   │                                    #   validateMove: + game_won terminal (FR-021..023),
│       │   │                                    #     + chord_no_unrevealed_neighbor (FR-016..020);
│       │   │                                    #   MoveRejection union: + "game_won" | "chord_no_unrevealed_neighbor";
│       │   │                                    #   initSuccessText/dispatchedText/rejectionText: + "game status:" line (FR-012..015)
│       │   └── saolei-mcp.test.ts               # UPDATE: status-line assertions, game_won terminal, chord-neighbor rejection
│       └── skill/saolei/
│           └── SKILL.md                         # UPDATE (FR-024): document game-status line + game_won + chord_no_unrevealed_neighbor
│
├── desktop/                                     # Go + Svelte desktop app (US1/US2)
│   └── frontend/src/components/
│       ├── ChatMessage.svelte                   # US1: .thinking-content hidden-scrollbar CSS + auto-scroll $effect (FR-001..004)
│       └── ChatView.svelte                      # US2: compact args (replace prettyArgs), pre-wrap result message,
│                                                #   collapsible result <details> (FR-005..008)
│
└── testplan/                                    # Large-test suites (Constitution §VI)
    ├── BUILD.bazel                              # UPDATE: + "testdata/saolei_10.png" + "testdata/saolei_5.png" in agent_saolei_test embedsrcs
    ├── agent_saolei_test.go                     # UPDATE: + //go:embed testdata/saolei_10.png (win) + testdata/saolei_5.png (loss) vars;
    │                                            #   assert "game status: won|lost|playing" + game_won/game_over terminal
    │                                            #   across win(saolei_10)/loss(saolei_5)/in-progress(saolei_1) flows (D12)
    └── testdata/
        ├── saolei_10.png                        # ADDED: copy of the library's win fixture (mirrors saolei_1/2 which
        │                                        #   already exist in both testdata dirs); //go:embed source for the win flow
        └── saolei_5.png                         # ADDED: copy of the library's loss fixture (16×16, has X/M); //go:embed source for the loss flow
```

**Structure Decision**: Multi-project change across the existing `projects/game/{agent/, desktop/, pkg/saolei-board/}` tree. No new top-level project, no new external dependency, **no proto change**. The library change is one new pure-predicate file (`win.ts`) + its test + a golden-test CASES entry + a barrel-export line — mirroring the existing `validate.ts` (pure-predicate) pattern; two library testdata fixtures (`saolei_10.png` user-supplied, `saolei_10.golden.txt` CLI-generated) are auto-picked-up by the `lib_test` glob (no BUILD edit). The large-test win AND loss flows reuse library screenshots: copies of `saolei_10.png` (win) and `saolei_5.png` (loss) are placed in `projects/game/testplan/testdata/` (mirroring how `saolei_1`/`saolei_2` already exist in both testdata dirs) and declared in the testplan `BUILD.bazel` `embedsrcs` + `//go:embed` directives (a tasks.md implementation step — the fixture files are already in place). The agent change is in-place edits to the existing MCP module, its test, and the skill doc. The frontend change is in-place edits to the two existing Svelte components.

## Complexity Tracking

> Not applicable — no Constitution Check violations to justify. Each change completes/simplifies an existing surface (net simplification: one win rule in the library instead of agent-side inference; one symmetric terminal-state concept; one validator rule in the existing order; the bubble renderer's leftover rough edges finished). No entries.

## Phase 0 / Phase 1 Outputs

- **Phase 0 — Research**: [research.md](./research.md) (decisions D1..D12; all plan-time unknowns resolved — scrollbar CSS, auto-scroll wiring, compact-args rendering, result formatting, collapsible body, win predicate, status-line position, terminal refactor, chord-neighbor rule, reason-code spelling, frontend testability, large-test split).
- **Phase 1 — Data model**: [data-model.md](./data-model.md).
- **Phase 1 — Contracts**: [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md), [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md).
- **Phase 1 — Quickstart**: [quickstart.md](./quickstart.md) (Scenarios 1..5).

## Next step

Run `/speckit.tasks` to generate `tasks.md`, phasing the implementation against the contracts + data model above. Suggested phase ordering (each independently testable): (1) library win predicate — unlocks US4; (2) agent MCP status-line + game_won terminal + chord-neighbor validation + skill doc — US3/US4/US5; (3) desktop think-bubble polish — US1; (4) desktop tool-bubble polish — US2. Each phase's document list MUST follow the constitution's three mandatory categories (代码规范文档 / 官方文档 / 技术文章), and each code phase MUST include compile (`bazel build //projects/game/...`) + unit (`bazel test //projects/game/...`) as part of the task. The `agent-saolei` large-test suite is the agent acceptance gate and MUST be executed via the `testplan` skill (`guitar run projects/game/testplan/system_test.yaml`), not merely built — all cases MUST pass. US1/US2 (frontend, no unit-test infra) are verified by `bazel build` + manual.

## Post-Phase-1 Constitution Re-check

Re-evaluated after producing [research.md](./research.md), [data-model.md](./data-model.md), [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md), [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md), [quickstart.md](./quickstart.md):

| Principle | Status | Notes |
|---|---|---|
| I — Citation & Provenance | PASS | Every decision in `research.md` and every contract clause cites a repo-relative path or full URL (MDN scrollbar docs, prior spec/contract/code paths, the saolei-board README). |
| II — Refactoring Over Patching | PASS | US1/US2 finish the 024 renderer (no special-case layer). US3 puts the win rule in the library (one source of truth). US4 makes win/loss terminal handling symmetric (one concept). US5 adds one rule in the existing validator order. No patch-over; net simplification. |
| III — Interface-First Design | PASS | The win-predicate signature, the `MoveRejection` union additions, the text-result status-line position, and the bubble render rules are all settled before implementation, in the two contracts + data-model. |
| IV — Test Granularity & Cadence | PASS | Library → `win.test.ts` (unit, pure logic) + `golden.test.ts` (+`saolei_10` win case + `isWin`-on-golden assertion); agent → `saolei-mcp.test.ts` (unit, DI); large → `agent_saolei_test.go` (via `guitar run`); frontend → `bazel build` + manual (no frontend unit-test infra). quickstart Scenarios 1..2 are build/manual; Scenario 3 is library unit + golden; Scenario 4 is agent unit; Scenario 5 is large-test across all three outcomes. |
| V — Read Before Code | PASS (planned) | Deferred to `tasks.md` (next command) — each phase MUST declare its three-category doc list. |
| VI — Large Test Acceptance for Services | PASS (planned) | The `agent-saolei` large-test suite is updated to cover `game status: won|lost|playing` + the `game_won`/`game_over` terminal rejections across win (`saolei_10`) / loss (`saolei_5`) / in-progress (`saolei_1`) flows using real recognition, and MUST be executed via `guitar run`; the `chord_no_unrevealed_neighbor` rule (narrow non-terminal config, no fitting testdata) is unit-test-verified via DI (research.md D12). |

No design change introduced a constitution violation. No complexity-tracking entries needed.
