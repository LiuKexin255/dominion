# Quickstart: Tool Bubble Rendering & Saolei Coordinate Accuracy

**Feature**: [024-tool-render-coord-fix](./spec.md) | **Date**: 2026-07-26

Runnable validation scenarios that prove the two fixes work end-to-end. Scenarios 1–2 are build + manual (frontend; the desktop frontend has no unit-test infra — [research.md](./research.md) D6). Scenario 3 is large-test + manual. Scenario 4 is a manual Windows integration gate (as in 023 T020) plus the agent large test for the geometry. Details live in [data-model.md](./data-model.md) and [contracts/coordinate-space-contract.md](./contracts/coordinate-space-contract.md); this is a validation guide, not an implementation.

## Prerequisites

- Agent buildable: `bazel build //projects/game/...` and `bazel test //projects/game/...` (agent unit tests, incl. `saolei-mcp.test.ts`).
- Desktop buildable: `bazel build //projects/game/desktop/...` (the `vite_build` compiles the Svelte frontend).
- For Scenario 4: a Windows host with Microsoft Minesweeper installed and bound to a desktop session (the target layout the geometry constants are calibrated against — [contracts/coordinate-space-contract.md](./contracts/coordinate-space-contract.md) §4).

---

## Scenario 1 — A tool call renders as a styled bubble (US1, Defect 1a)

**Validates**: FR-001, SC-001.

1. Build the desktop: `bazel build //projects/game/desktop/...`.
2. Run a session turn in which the model calls a tool (e.g. `saolei_init`, then `saolei_click`).
3. **Expected**: each tool call appears as a **bordered bubble** containing the tool name and its input arguments (e.g. `saolei_init` / `{}`), distinct from plain agent text — not unstyled text. When the result arrives, the **same** bubble updates in place to show the outcome + message (+ screenshot).
4. **Regression check**: text/thinking/image messages still render as before.

## Scenario 2 — A neutral tool result does NOT read "failed" (US1, Defect 1b)

**Validates**: FR-002, FR-003, FR-004, SC-002.

1. Run a saolei turn (`saolei_init` succeeds — F2 dispatched).
2. **Expected**: the `saolei_init` result bubble shows a **neutral** state (neutral glyph + `done`/neutral label, muted colour) — NOT `✗ failed`. (The status carried from the LLM stream is neutral for MCP tools — [data-model.md](./data-model.md) §1; protojson omits the zero-value `status`, so the renderer must treat absent-as-neutral.)
3. **Native-tool regression check**: run a native mouse-tool operation that genuinely succeeds → it still reads `✓ succeeded`; one that genuinely fails → `✗ failed`. (Native tools carry non-zero `SUCCEEDED`/`FAILED`, always emitted by protojson.)
4. **Agent-side status correctness** (unit/large): `bazel test //projects/game/agent:lib_test` covers the agent carrying neutral for saolei; the `agent-saolei` large suite ([Scenario 4](#scenario-4)) re-verifies end-to-end.

## Scenario 3 — Live and history render the tool bubble identically (US1)

**Validates**: FR-005, SC-004.

1. Run a saolei turn with at least one tool call (live).
2. Leave the session and re-enter it (history replay via `ListMessages`).
3. **Expected**: the tool bubbles (styling + status + message + screenshot) are identical between the live view and the replayed history — single source of truth (the LLM messages).

---

## Scenario 4 — A saolei cell click lands on the intended cell (US2)

**Validates**: FR-006, FR-007, FR-008, FR-009, SC-003. This is the manual Windows integration gate plus the agent large test.

### 4a. Geometry (agent large test — Constitution §VI)

1. Update and run the `agent-saolei` suite via the testplan skill: `guitar run projects/game/testplan/system_test.yaml` (full deploy→test→cleanup loop).
2. **Expected (all cases pass)**: the agent dispatches `MouseMoveAndClickPart` with the **new client-space** coords for each grid `(x,y)` — e.g. `center(4,4) → (168, 248)` with `CHROME_OFFSET_Y_PX = 96` ⇒ client `BOARD_ORIGIN_Y_PX = 104` (per [contracts/coordinate-space-contract.md](./contracts/coordinate-space-contract.md) §4). This verifies the geometry; it does NOT verify the click lands on the target window (that is 4b).

### 4b. Click-landing (manual Windows gate)

1. On a Windows host, bind the Microsoft Minesweeper window to a desktop session.
2. `saolei_init` (F2 → new game), then `saolei_click(4, 4)`.
3. **Expected**: the click lands on cell `(4, 4)` — verified from the returned screenshot (the revealed/affected cell is row 4, not several rows too low). No chrome-height downward drift (the 96 px non-client offset is compensated in the agent).
4. Repeat for a few `(x, y)` values and for `saolei_flag` / `saolei_chord_click` — each lands on its intended cell.
5. Record the result (and the finalized `BOARD_ORIGIN_*` constants) in the test record. If a Windows host is unavailable, record this as a manual integration gate (as 023 T020 did).

## Notes

- The frontend (Scenarios 1–3) is verified by `bazel build` + manual — no frontend unit-test target exists ([research.md](./research.md) D6). The agent status correctness is unit + large tested.
- Scenario 4a (large test) MUST be executed via `guitar run`, not merely built (Constitution §VI v1.3.0); all cases MUST pass. Scenario 4b is inherently a Windows-host verification.
