# Data Model: Saolei MCP

**Feature**: 018-saolei-mcp
**Date**: 2026-07-20
**Status**: Phase 1 design — derives from `spec.md` and `research.md`.

This document defines the in-memory data structures for the saolei MCP. The external interface contracts (proto Part extensions, MCP tool schemas) live in `contracts/`.

## 1. Game State (per session)

One instance per dominion session, co-located with that session's `OperationBridge` (`projects/game/agent/src/session-agent.ts`). Owned by the session-bound `McpServer` (see `research.md` D3).

```text
GameState {
  grid:        CellStatus[][]     // grid[y][x], indexed by (x=col, y=row)
  width:       int                // column count (x in 0..width-1) — set by saolei_init
  height:      int                // row count (y in 0..height-1) — set by saolei_init
  pendingUpdate: boolean          // alternation flag (see §4)
  lastOp:      LastOp | null      // the operation awaiting saolei_update (see §3)
  initialized: boolean            // whether saolei_init has run
}
```

- `grid` is sized to `height` rows × `width` cols and initialized to all `INITIAL` by `saolei_init(width, height)` (FR-006) and reset on re-init (FR-027). `width` = column count, `height` = row count (cell counts, not pixels).
- `width`/`height` are supplied by the model at `saolei_init` (it reads the board size from the screenshot). Out-of-range coordinates are rejected (FR-016).
- `pendingUpdate` is the single bit enforcing the operate-then-update alternation (FR-011). Set `true` only on a successfully dispatched operation; cleared by an accepted `saolei_update`. A validation-rejected operation never sets it (Clarification Q3 → A).
- `lastOp` records what the model just did so `saolei_update` validation can check the update matches the operation (FR-013/014/015).
- No persistence — in-memory only, consistent with `SessionAgentStore` (lost on agent restart).

## 2. Cell Status enum

Authoritative enumeration (FR-010). Maps 1:1 to the `saolei_update` status values the model sends.

| Status | Meaning | Set by |
|---|---|---|
| `INITIAL` | Unopened cell, no marker | `saolei_init` (default) |
| `0` | Revealed blank cell (no adjacent mines) | `saolei_update` after a click |
| `1`..`8` | Revealed number cell (adjacent mine count) | `saolei_update` after a click/chord |
| `FLAG` | Mine marker placed via right-click on an unopened cell | `saolei_update` after `saolei_flag` |
| `HIT_MINE` | The mine **directly triggered** by the current operation (game-ending) | `saolei_update` after a click/chord that hit a mine |
| `MINE` | A mine revealed at game end that was **not** triggered by the current operation | `saolei_update` at game end |

Wire representation: a string union on the MCP tool boundary (see `contracts/mcp-tool-contract.md`); the in-memory representation is an internal enum. The distinction `HIT_MINE` vs `MINE` is fixed by spec Clarification Round 2 (D6).

## 3. LastOp (operation awaiting update)

```text
LastOp {
  kind:      "click" | "flag" | "chord_click"
  target:    { x: int, y: int }     // the operation's target cell
  // for chord_click: the target's pre-op number & adjacent flags are implicit in grid
}
```

Recorded when an operation is dispatched; consumed and cleared by the next accepted `saolei_update`. `saolei_init` does not set `lastOp` (it is exempt from alternation — FR-006/FR-011).

## 4. Alternation state machine (FR-011)

```text
            saolei_init (re)initializes; pendingUpdate=false; lastOp=null
                                  |
                                  v
   +------- (idle: pendingUpdate=false) -------+
   |                |                |         |
 saolei_click   saolei_flag   saolei_chord_click   saolei_update (no lastOp -> reject)
   |  validate target pre-dispatch              |
   |  reject -> stay idle (Q3: no lock)         |
   |  accept -> dispatch, set pendingUpdate=true, lastOp=<op>
   v                                            v
   +------- (pending: pendingUpdate=true) -------+
   |                                             |
   any operation -> REJECT (must update first)   saolei_update
                                                 |  validate vs lastOp (FR-013/014/015)
                                                 |  reject -> stay pending (state unchanged)
                                                 |  accept -> apply, pendingUpdate=false, lastOp=null
                                                 v
                                              (idle)
```

Key invariant: `pendingUpdate` transitions to `true` **only** on a dispatched (accepted) cell operation; it transitions to `false` **only** on an accepted `saolei_update`. Validation rejections are no-ops on the flag (Clarification Q3 → A).

## 5. Board geometry (fixed constants — research.md D6)

```text
BOARD_ORIGIN_X_PX = 24     // grid left edge offset from window's left edge
BOARD_ORIGIN_Y_PX = 200    // grid top edge offset from window's top edge
CELL_SIZE_PX      = 32     // cell width = cell height

// cell (x, y) center, in window-client coordinates:
centerX(x) = BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2   // = 24 + x*32 + 16
centerY(y) = BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2   // = 200 + y*32 + 16
```

These are **window-client coordinates** (relative to the bound window's top-left, DPI-corrected). They are used directly by `WINDOW_MESSAGE` mouse (packed into `WM_*` `lParam`) with **no** screen-offset addition. Constants live in the saolei MCP integration so a future board layout can be parameterized without changing tool contracts.

## 6. Proto Part model (extended — see contracts/proto-operation-contract.md for field numbers)

The agent↔desktop `Part.kind` oneof (`projects/game/game.proto:253-263`) is extended additively:

```text
Part.kind oneof {
  ...existing: text, thinking, image, mouse_move, mouse_click, tool_result...
  KeyboardPressPart     keyboard_press       = 7   // NEW (saolei_init F2)
  MouseMoveAndClickPart mouse_move_and_click = 8   // NEW (saolei cell ops)
}

KeyboardPressPart {
  string tool_id = 1;
  KeyboardKey key = 2;          // enum; KEY_F2 etc. (see contracts)
}

MouseMoveAndClickPart {
  string          tool_id  = 1;
  int32           x_px     = 2;   // window-client X (per §5 formula)
  int32           y_px     = 3;   // window-client Y
  MouseClickAction click   = 4;   // existing enum (game.proto:219-226)
  MouseInputMethod method  = 5;   // NEW enum
}

// NEW enum, also added as a field on MouseMovePart & MouseClickPart:
enum MouseInputMethod {
  MOUSE_INPUT_METHOD_UNSPECIFIED     = 0;
  MOUSE_INPUT_METHOD_SIMULATED       = 1;   // existing: SetCursorPos + SendInput
  MOUSE_INPUT_METHOD_WINDOW_MESSAGE  = 2;   // new: PostMessage WM_* to HWND
}
```

- Existing `MouseMovePart`/`MouseClickPart` gain a `MouseInputMethod method` field (default `UNSPECIFIED`, treated as `SIMULATED` → backward compatible).
- AgentFrame (`game.proto:346-365`) is unchanged structurally — it still carries `PartBlock` of `Part`s and remains tool-agnostic (FR-004).

## 7. Operation → dispatch mapping

| Tool call | `lastOp.kind` | Dispatched `Part` | Desktop action |
|---|---|---|---|
| `saolei_init` | — (none) | `KeyboardPressPart{ key: KEY_F2 }` | Post key message (F2) to bound HWND |
| `saolei_click(x,y)` | `click` | `MouseMoveAndClickPart{ x: centerX(x), y: centerY(y), click: LEFT_CLICK, method: WINDOW_MESSAGE }` | Post `WM_LBUTTONDOWN`/`UP` |
| `saolei_flag(x,y)` | `flag` | `MouseMoveAndClickPart{ ..., click: RIGHT_CLICK, method: WINDOW_MESSAGE }` | Post `WM_RBUTTONDOWN`/`UP` |
| `saolei_chord_click(x,y)` | `chord_click` | `MouseMoveAndClickPart{ ..., click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }` | Post chord `WM_LBUTTONDOWN`+`WM_RBUTTONDOWN` (+ups) — single simultaneous press |

All dispatches go through `OperationBridge.dispatch(part)` (`projects/game/agent/src/operation-bridge.ts:132`), which stamps `tool_id`, wraps in a content `AgentFrame`, and awaits the matching `ToolResultPart`.

## 8. Validation rules (formalization of FR-013..FR-016)

Connectivity is **8-connectivity** (research.md D10). "Adjacent" = the up-to-8 neighbors.

**FR-013 (click)** — on `saolei_click(x,y)`: require `grid[y][x] == INITIAL`. On the following `saolei_update`:
- The update MUST change `grid[y][x]` (to a number `0..8`, or `HIT_MINE`).
- Let `N` = the set of cells updated to a number (`0..8`). The cells in `N` MUST form a single connected region **that includes the target** `(x,y)` if the target is a number; if the target is `HIT_MINE`, no connectivity requirement (game over). (Encodes the 0-cell cascade: revealing a 0 reveals a connected region whose number-boundary is `N`.)

**FR-014 (flag)** — on `saolei_flag(x,y)`: require `grid[y][x] == INITIAL`. On the following `saolei_update`:
- The update MUST change only the target cell, and only between `INITIAL` ↔ `FLAG`. No other cell may change; no other transition is permitted.

**FR-015 (chord)** — on `saolei_chord_click(x,y)`: require `grid[y][x]` is a number `1..8` AND the count of adjacent `FLAG` cells equals that number. On the following `saolei_update`:
- No target-adjacent `FLAG` cell may change.
- Every other target-adjacent non-number cell MUST be updated to a number or `HIT_MINE`/`MINE` — **except** when the operation hit a mine, in which case only the hit mine need be updated.
- Let `N` = updated number cells. Each connected component of `N` MUST contain at least one cell adjacent to the chord target.

**FR-016 (general)** — reject any `saolei_update` with coordinates outside `[0,width)×[0,height)`, or statuses inconsistent with the recorded `lastOp` (e.g., a flag transition after a click). State unchanged on rejection.

The rule set is explicitly extensible (FR-017); rules ship as composable validators keyed by `lastOp.kind`.

## 9. Entities touched outside the agent

| Entity | Location | Change |
|---|---|---|
| `AgentProfile` proto | `projects/game/game.proto:414-435` | No schema change (`mcp_names` field 6 already exists); desktop UI now writes it (FR-020/021). |
| `ProfileData` / `ProfileResult` (TS) | `projects/game/agent/src/session-agent.ts:18`, `prompt-client.ts:41` | Add `mcpNames: string[]` (consumed by the adapter factory + skill loader). |
| `AdapterFactory` (TS) | `projects/game/agent/src/llm.ts:165-171` | Receives `mcpNames`; when `saolei` present, constructs MCP client + loads saolei skill instead of/alongside `buildTools`. |
| `buildTools` (TS) | `projects/game/agent/src/llm.ts:57-71` | For a saolei profile, mouse tools are NOT added (FR-012); saolei tools come from the MCP client. |
| Desktop `executeAgentOperation` | `projects/game/desktop/app.go:661-815` | Handle `KeyboardPressPart` + `MouseMoveAndClickPart`; honor `MouseInputMethod`. |
| Desktop `operation` package | `projects/game/desktop/internal/operation/` | Add `PostMessage`-based mouse (WINDOW_MESSAGE) + real keyboard impl; implement `ExecuteKeyPress`. |
| Desktop profile editor | `projects/game/desktop/frontend/src/components/ProfileManagement.svelte` | Add MCP chip (`saolei`); include `mcp_names` in create + update-mask. |
| Built-in skill | `projects/game/agent/src/skill/saolei/SKILL.md` | NEW — authored per `specs/020-agent-resources-layout/contracts/skill-md-format.md`. |
| MCP integration | `projects/game/agent/src/mcp/saolei/` | NEW — the MCP server tool handlers + game state + validation (per `src/mcp/README.md`). |

## References

- `spec.md` — FR-001..FR-027, Clarifications (Round 1 + Round 2).
- `research.md` — decisions D1..D10.
- `contracts/proto-operation-contract.md` — exact proto field numbers + semantics.
- `contracts/mcp-tool-contract.md` — the 5 MCP tool schemas.
- `specs/020-agent-resources-layout/contracts/{directory-layout.md,skill-md-format.md}` — directory + SKILL.md format.
- `projects/game/game.proto:253-309` — current Part model.
