# Quickstart: Saolei MCP Validation

**Feature**: 018-saolei-mcp
**Date**: 2026-07-20
**Status**: Phase 1 — runnable validation guide. Proves the feature works end-to-end without duplicating implementation detail.

This guide describes how to validate the saolei MCP feature against its contracts (`contracts/proto-operation-contract.md`, `contracts/mcp-tool-contract.md`) and data model (`data-model.md`). Implementation steps live in `tasks.md`.

## Prerequisites

- A clean Bazel workspace: `bazel build //...` and `bazel test //...` pass before starting.
- A Windows host with Microsoft Minesweeper (the fixed board geometry — `data-model.md` §5: origin 24 px left / 200 px top, 32×32 px cells — targets this layout).
- The desktop app built and runnable (`projects/game/desktop/`).
- An LLM profile backend (or the fake-LLM test path from `specs/012-fake-llm-service`) for model-driven scenarios.

## Build the changed surfaces

```bash
# After proto + agent + desktop code changes:
bazel run //:gazelle projects/game/agent projects/game/desktop
bazel build //...
bazel test //...
```

The proto change (`contracts/proto-operation-contract.md`) regenerates TS types via `ts_proto_library("game_types")` and Go types via the `dominion/projects/game` import.

## Scenario 1 — Profile config exposes the saolei MCP (unit + UI)

Validates: FR-020, FR-021, FR-022, SC-005.

1. Launch the desktop; open Agent Profiles (`ProfileManagement.svelte`).
2. Create a profile: pick a model, toggle the new **MCP: saolei** chip on, save.
3. **Expected**: the profile card shows a saolei/MCP badge; a GET of the profile returns `mcp_names: ["saolei"]`.
4. Edit the profile, toggle saolei off, save.
5. **Expected**: `mcp_names` is empty (or omits saolei) and the badge disappears; the update-mask included `mcp_names`.

Automated check: a desktop unit test asserts `mcp_names` round-trips through create/update and that the editor writes the field.

## Scenario 2 — Per-session MCP server exposes the five tools (integration)

Validates: FR-001, FR-002, FR-002a, FR-002b, FR-003, FR-005, SC-001.

1. Start a session under a saolei-enabled profile (driver: the agent's `Connect` flow).
2. From a test MCP client (or the agent's own loopback `MultiServerMCPClient`), connect to `http://localhost:{MCP_PORT}/internal/mcp/{session_id}` (streamable HTTP).
3. Call `tools/list`.
4. **Expected**: exactly `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_update` are listed.
5. Connect to a path with an unknown `{session_id}`.
6. **Expected**: the server responds `404` "Session not found" (FR-003); no game state is created.

Automated check: an agent-side test spins up the MCP host on a random port, creates a `SessionAgent`, and asserts `getTools()` returns the five tools bound to that session's bridge; an unknown session_id returns 404.

## Scenario 3 — Operation dispatch + alternation (integration)

Validates: FR-006..FR-011, FR-018, SC-002, SC-003.

Using a fake desktop (or a stubbed `OperationBridge`) that records dispatched `Part`s and returns a canned `ToolResultPart`:

1. Call `saolei_init(9, 9)` (width=9, height=9 for a beginner board).
2. **Expected**: a `KeyboardPressPart{ key: KEYBOARD_KEY_F2 }` is dispatched; the result confirms initialization of a 9×9 grid; no `saolei_update` is required.
3. Call `saolei_click(3, 4)`.
4. **Expected**: a `MouseMoveAndClickPart{ x: 24+3*32+16, y: 200+4*32+16, click: LEFT_CLICK, method: WINDOW_MESSAGE }` is dispatched (coordinates per `data-model.md` §5).
5. Before calling `saolei_update`, call `saolei_click(5, 5)`.
6. **Expected**: rejected with "must update first" (`pendingUpdate` is true); no second dispatch.
7. Call `saolei_update` with a connected number batch including the target.
8. **Expected**: accepted; `pendingUpdate` cleared.
9. Repeat for `saolei_flag` (RIGHT_CLICK) and `saolei_chord_click` (LEFT_RIGHT_PRESS — a single simultaneous left+right press, not two clicks), verifying each dispatches with `method: WINDOW_MESSAGE`.

Automated check: an agent-side test asserts the exact `Part` shape dispatched for each tool and the alternation state transitions.

## Scenario 4 — Validation rejects illegal operations/updates (unit)

Validates: FR-013..FR-016, FR-019, SC-004.

Against an initialized `GameState` (no desktop needed — validation is pre-dispatch and pure):

1. **click**: flag a cell, then `saolei_click` it → rejected (target not INITIAL); retry on an INITIAL cell → accepted. Confirm a rejected click does NOT set `pendingUpdate` (Clarification Q3 → A) — immediately retrying a valid click is allowed.
2. **flag**: `saolei_flag` an already-numbered cell → rejected.
3. **chord**: `saolei_chord_click` on a number whose adjacent flags ≠ the number → rejected; add flags to satisfy, then accepted.
4. **update connectivity**: after a click, send a `saolei_update` with disconnected number cells → rejected (FR-013); state unchanged.
5. **update range**: send an update with an out-of-bounds coordinate → rejected (FR-016).
6. **chord update**: after a chord, send an update that changes a target-adjacent flag → rejected (FR-015).

Automated check: pure unit tests on the validation module, keyed by `lastOp.kind`, covering each rule's accept and reject paths.

## Scenario 5 — Skill auto-injection (integration)

Validates: FR-023, FR-024, FR-025, SC-006.

1. Create two profiles: one with saolei MCP on, one without.
2. Bind a session under each; capture the assembled system prompt passed to `AgentAdapterImpl`.
3. **Expected**: the saolei profile's prompt contains the body of `projects/game/agent/src/skill/saolei/SKILL.md`; the non-saolei profile's prompt does not.

Automated check: an agent-side test asserts the saolei SKILL.md body is appended to the prompt iff `mcpNames` includes `saolei`.

## Scenario 6 — Desktop executes new Parts (large test)

Validates: FR-004a..FR-004d, SC-002 (desktop side).

On a Windows host with Minesweeper bound:

1. Send a `KeyboardPressPart{ key: KEYBOARD_KEY_F2 }` → the game restarts (new game).
2. Send a `MouseMoveAndClickPart{ ..., method: WINDOW_MESSAGE }` at a known-safe first cell → the cell reveals, and a screenshot shows **no OS cursor** sitting on the board (the cursor-blocking rationale, research.md D5).
3. Send the same coordinates with `method: SIMULATED` → the existing `SetCursorPos`+`SendInput` path runs and the cursor is visible (backward-compat).

Large-test plan: authored under `testplan/` and executed via the `testplan` skill (per `style/large_test.md`); assert the desktop handles each new `Part`/`method` correctly against a real Minesweeper window.

## Scenario 7 — End-to-end model reveal sequence (large test)

Validates: SC-007.

With a real (or fake) model driving a saolei profile against a bound Minesweeper window, observe a full sequence:

```text
saolei_init → saolei_click → saolei_update → saolei_flag → saolei_update → saolei_chord_click → saolei_update
```

**Expected**: each step validates and dispatches correctly; the game state tracked by the agent matches the visible board at each `saolei_update`.

Large-test plan: under `testplan/`, driven by the fake-LLM service (`specs/012-fake-llm-service`) to make the sequence deterministic.

## References

- Contracts: `contracts/proto-operation-contract.md`, `contracts/mcp-tool-contract.md`.
- Data model: `data-model.md`.
- Research: `research.md`.
- Spec: `spec.md` (FRs + Clarifications).
- Large-test conventions: `style/large_test.md`; fake-LLM baseline: `specs/012-fake-llm-service/spec.md`.
