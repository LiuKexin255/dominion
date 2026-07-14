# Quickstart: Saolei MCP Validation

**Feature**: 018-saolei-mcp | **Date**: 2026-07-14

Runnable validation scenarios that prove the feature works end-to-end. Implementation code lives in `tasks.md`; this is a validation/run guide. Refer to [contracts/](./contracts/) and [data-model.md](./data-model.md) for exact shapes.

## Prerequisites

- Bazel workspace at repo root; `pnpm`/`go`/`python` hermetic toolchains via `bazel run //:go` etc. (see `AGENTS.md`).
- A Windows host with the target Minesweeper (扫雷) game for the end-to-end scenario (the `PostMessage` executor is Windows-only).
- Board geometry constants pinned: `TOP_OFFSET=200`, `LEFT_OFFSET=24`, `BLOCK_LENGTH=32` px ([plan.md](./plan.md) Technical Context).

## Unit validation (vitest, agent service)

Run from repo root:

```bash
bazel test //projects/game/agent/...
```

Scenarios (each maps to a `*.test.ts` under `mcp/saolei/`):

1. **Board state machine** — drive `SaoleiMcp` through `init → click → update → click` and assert: init sets all-`block`+ready; click enters awaiting-update; a second click before update is rejected (`awaiting-update`); a valid update returns to ready; a `boom` update enters terminal and blocks further ops until re-init; no auto-timeout out of awaiting-update.
2. **Validation** — assert rejections (structured `status:"rejected"`, not thrown) for: pre-init op, click non-`block`, chord non-number, out-of-bounds coord, `saolei_update` with an illegal transition (batch applied atomically — no partial state).
3. **Coordinate computation** (`geometry.ts`) — assert cell-centre pixels for `(0,0)`→`(40,216)`, `(1,1)`→`(72,248)`, and the board's bottom-right corner cell; assert bounds check rejects `x>=width`/`y>=height` and negatives.
4. **Tool registry** — `buildTools(toolNames=[], mcpNames=["saolei"], bridge)` returns exactly the five saolei tools; unknown mcp names are ignored.
5. **Skill assembly** — `AgentAdapterImpl` composes `effectiveSystemPrompt = systemPrompt + SEPARATOR + skillContents`; assert the skill body is present when `skillNames=["saolei"]`.
6. **Per-session isolation** — two `SaoleiMcp` instances; operations/updates in one do not affect the other; a rebuilt instance starts `uninitialized` with no carry-over.

## Desktop validation (Go)

```bash
bazel test //projects/game/desktop/...
```

Scenarios:

7. **Input delivery executor** — feed a `WINDOW_MESSAGE` `PartBlock` `[MouseMovePart, MouseClickPart{LEFT_CLICK}]` and a `KeyPart{F2}` to the desktop executor against a stub window; assert `PostMessage` calls (WM_LBUTTONDOWN/UP, WM_KEYDOWN/UP) fire at the right client coords, and **no** `SetCursorPos`/`SendInput` is called for `WINDOW_MESSAGE` delivery (occlusion-free — SC-003). Also assert `SIMULATE` delivery (unset/default) still uses the physical path.
8. **Profile view-model round-trip** — `CreateAgentProfileView` with `SkillNames`/`McpNames` persists and reads back via `AgentProfileView` (closes FR-033).

## OperationBridge integration (vitest)

9. **PartBlock multi-part dispatch** — `OperationBridge.dispatch(PartBlock)` (e.g. `[MouseMovePart, MouseClickPart]` and `[KeyPart]`) correlates by `tool_id`, sink-writes a content frame, and resolves the single `ToolResultPart` (or 5s timeout) — using the existing `operation-bridge.test.ts` harness extended for multi-part blocks.

## End-to-end (large test — `testplan/`)

Per `AGENTS.md`, large tests live in `testplan/` and run via the `testplan` skill (read `style/large_test.md` first). Scenario:

10. **Accurate minesweeper play** — bind the Minesweeper window; create a profile with `mcp_names:["saolei"]` + `skill_names:["saolei"]`; from a turn, the agent calls `saolei_init(9,9)` → `saolei_click(4,4)` → observes → `saolei_update(...)`. Expected: the click lands inside cell (4,4); the post-operation screenshot shows **no OS cursor marker over the target cell** (SC-003); the computed coordinate is inside the intended cell (SC-004 ≥95% across a sampled grid).

## Desktop UI validation (manual / Svelte)

11. **Profile MCP+skill selection** — in `ProfileManagement`, create a profile toggling the `saolei` mcp + skill chips; list it back (chips show active); edit to toggle them off/on; assert each change round-trips through create/edit/list with the expected `mcp_names`/`skill_names` (SC-005, <30s).

## Expected outcomes (mapping to spec SCs)

| Scenario | Validates |
|---|---|
| 1, 2 | SC-002 (structured rejections), state machine correctness |
| 3 | SC-004 (coordinate accuracy ≥95%) |
| 6 | SC-008 (per-session isolation) |
| 7 | SC-003 (no cursor occlusion) |
| 9 | FR-022/SC-007 (atomic batch), bridge generality |
| 10 | SC-001 (full turn via MCP), SC-003/SC-004 |
| 11 | SC-005 (UI round-trip <30s), SC-006 (reorg) |

A passing run of scenarios 1–9 plus 11 is the deterministic gate; scenario 10 is the real-game acceptance test.
