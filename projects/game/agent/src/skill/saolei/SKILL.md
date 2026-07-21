---
name: saolei
description: Operate a grid-based Minesweeper game through the five saolei MCP tools (saolei_init, saolei_click, saolei_flag, saolei_chord_click, saolei_update). Use this skill when the session profile has the saolei MCP enabled and you must reveal cells, place flags, or chord numbers on a bound Minesweeper window.
compatibility: opencode
metadata:
  audience: dominion
  scope: saolei-mcp
---

# saolei

This skill guides the model in operating a grid-based Minesweeper game through the saolei MCP tools. The agent hosts a per-session MCP server that exposes five tools; the model uses them — INSTEAD OF the raw mouse tools — to play the game.

Authority: `specs/018-saolei-mcp/spec.md` (FR-005..FR-019), `specs/018-saolei-mcp/contracts/mcp-tool-contract.md`, `specs/018-saolei-mcp/data-model.md`.

## Coordinate convention

All cell coordinates use a **top-left origin `(0, 0)`** grid:

- `x` = column index, increases left → right. Valid range: `0..width-1`.
- `y` = row index, increases top → bottom. Valid range: `0..height-1`.

`width` and `height` (cell counts, NOT pixels) are declared at `saolei_init`. Pixel geometry (board origin offset, cell size) is fixed inside the agent and is never sent by the model.

Example: on a 9×9 board, the top-left cell is `(0, 0)`; the bottom-right cell is `(8, 8)`.

## Cell status enum

Each cell is in exactly one status. The status is sent as a string in `saolei_update`:

| Status | Meaning |
|---|---|
| `INITIAL` | Unopened cell, no marker (the default after `saolei_init`). |
| `0` | Revealed blank cell (no adjacent mines). Triggers a cascade. |
| `1`..`8` | Revealed number cell (adjacent mine count). |
| `FLAG` | Mine marker placed via right-click on an unopened cell. |
| `HIT_MINE` | The mine **directly triggered** by the current operation (game-ending). |
| `MINE` | A mine revealed at game end that was **not** triggered by the current operation. |

The distinction between `HIT_MINE` and `MINE`: `HIT_MINE` is the mine you just clicked or chorded that ended the game; `MINE` covers other mines the game reveals on a loss (not triggered by the current op).

## Tools

### saolei_init(width, height)

Initialize or restart the Minesweeper game for this session.

- `width` (integer ≥ 1): column count (x ranges `0..width-1`).
- `height` (integer ≥ 1): row count (y ranges `0..height-1`).

Behavior: dispatches an F2 keypress to the bound window (the Minesweeper new-game shortcut) and resets the per-session board model to a `width`×`height` grid of `INITIAL` cells.

**Exempt from the operation→update alternation** — no `saolei_update` is required after `saolei_init`. Re-calling `saolei_init` re-dispatches F2 and resets the state to the new dimensions (prior state is discarded).

Call this FIRST (before any cell operation), and again whenever the human restarts the game or changes the difficulty.

### saolei_click(x, y)

Left-click (reveal) the cell at `(x, y)`.

- Precondition: the target cell MUST be `INITIAL` (not flagged, not already revealed). A rejected click does NOT lock the alternation — retry immediately on a valid cell.
- After success: the cell is revealed. Numbers cascade per Minesweeper rules (a revealed `0` reveals its connected neighborhood). You MUST call `saolei_update` before any further operation.
- If the clicked cell is a mine: the game ends; the subsequent `saolei_update` reflects `HIT_MINE` on the clicked cell.

### saolei_flag(x, y)

Right-click to toggle a flag on the cell at `(x, y)`.

- Precondition: the target cell MUST be `INITIAL`. Toggling only transitions a cell between `INITIAL` and `FLAG` (unflagging a flagged cell returns it to `INITIAL`).
- After success: you MUST call `saolei_update` before any further operation.
- A flag is a marker for your own reasoning; it does not by itself affect the game's win/loss detection. Use it to track where you believe mines are.

### saolei_chord_click(x, y)

Perform a chord — a **single simultaneous left+right button press** — on the cell at `(x, y)`. A chord reveals all unflagged neighbors of a satisfied number cell in one atomic action.

- Precondition: the target MUST be a non-`0` number (`1`..`8`) AND the count of adjacent `FLAG` cells MUST equal the cell's number.
- After success: you MUST call `saolei_update` before any further operation.
- If any flag is misplaced (a real mine remains among the unflagged neighbors), the chord reveals that mine and the game ends (`HIT_MINE`).

Do NOT emulate a chord with two separate clicks; `saolei_chord_click` is the single atomic operation.

### saolei_update(cells)

Batch-update the cell statuses you observe on the board after the most recent operation.

- `cells`: array of `{ x, y, status }` entries (status from the enum above).
- Precondition: an operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`) MUST be pending an update. Calling `saolei_update` with no operation pending is rejected.
- The batch MUST be consistent with the operation just performed:
  - After `saolei_click`: the target cell MUST change; all updated number cells MUST be mutually connected (8-connectivity — horizontal, vertical, or diagonal adjacency — consistent with the cascade/flood-fill reveal of blank cells).
  - After `saolei_flag`: ONLY the target cell changes, between `INITIAL` and `FLAG`. No other cell may change.
  - After `saolei_chord_click`: target-adjacent `FLAG` cells are unchanged; other target-adjacent non-number cells are updated (except when a mine is hit, in which case only the hit mine need be updated); each connected component of updated number cells includes at least one cell adjacent to the chord target.
- All coordinates MUST be within `[0,width)×[0,height)`.
- On rejection: state is unchanged; send a corrected `saolei_update`. You CANNOT start a new operation until `saolei_update` is accepted.
- On accept: the alternation unlocks; the next operation becomes allowed.

`saolei_update` is how you inform the agent of the visible board state. Read the latest screenshot, identify which cells changed, and report them.

## Operation → update alternation

The five tools follow a strict alternation (except `saolei_init`):

```
saolei_init                                            (no update required; may repeat)
[ saolei_click | saolei_flag | saolei_chord_click ]    → dispatches, then:
saolei_update                                           → validates + applies; then:
[ next operation ]                                      → alternates
```

Rules:

- A second operation BEFORE an accepted `saolei_update` is rejected ("must update first").
- A validation-REJECTED operation (rejected pre-dispatch) does NOT lock the alternation — retry immediately with a valid operation.
- A validation-REJECTED `saolei_update` leaves the session pending — send a corrected `saolei_update` (you cannot start a new operation).
- `saolei_init` is exempt: no `saolei_update` is required before or after it.

## Validation expectations

The agent validates every operation and every update against the rules above and rejects illegal ones with a clear reason (returned as a normal tool result). On rejection, the board state is unchanged and (for operations) the alternation is NOT locked.

Typical rejection reasons:

- `saolei_click` on a non-`INITIAL` or flagged cell.
- `saolei_flag` on a non-`INITIAL` cell.
- `saolei_chord_click` on a non-number, a `0`, or a number whose adjacent flag count ≠ the number.
- `saolei_update` with out-of-bounds coordinates.
- `saolei_update` whose changes are inconsistent with the recorded operation (wrong connectivity after a click; wrong single-cell transition after a flag; a changed target-adjacent flag or missing neighbor update after a chord).

When a tool result reports a rejection, read the reason, correct the inputs, and retry.

## Example play flow

A simple supervised reveal sequence using only the saolei tools:

```
1. saolei_init(width=9, height=9)
   → F2 dispatched; 9×9 grid of INITIAL cells ready. (No update required.)

2. saolei_click(x=4, y=4)
   → Left-click dispatched at the centre cell.

3. saolei_update(cells=[
       {x:4, y:4, status:"0"},
       {x:3, y:3, status:"1"}, {x:3, y:4, status:"1"}, {x:3, y:5, status:"1"},
       {x:4, y:3, status:"1"}, {x:4, y:5, status:"1"},
       {x:5, y:3, status:"1"}, {x:5, y:4, status:"1"}, {x:5, y:5, status:"1"},
   ])
   → The 0 cascade revealed a connected region; the boundary cells are 1s.
     All updated number cells are connected through the target. Accepted.

4. saolei_flag(x=3, y=3)
   → Right-click dispatched; place a flag where a mine is suspected.

5. saolei_update(cells=[{x:3, y:3, status:"FLAG"}])
   → Only the target transitioned INITIAL → FLAG. Accepted.

6. saolei_chord_click(x=4, y=4)
   → Adjacent flag count (1) equals the cell's number (1). Chord dispatched.

7. saolei_update(cells=[{x:2, y:3, status:"2"}, {x:2, y:4, status:"3"}, ...])
   → Target-adjacent flag unchanged; other neighbors updated; each connected
     component of updated number cells includes a target-adjacent cell. Accepted.
```

Key points demonstrated:

- `saolei_init` needs no `saolei_update`.
- Every cell operation is followed by exactly one `saolei_update` before the next operation.
- The `saolei_update` batch matches the operation kind: click → target changed + connected numbers; flag → only the target `INITIAL`↔`FLAG`; chord → flag preserved + neighbors updated.

## When NOT to use

- Do NOT call `mouse_move` or `mouse_click` — for a saolei-enabled profile those raw mouse tools are intentionally excluded (FR-012). Use the saolei tools exclusively as the operation channel.
- Do NOT send a `saolei_update` without a pending operation — it is rejected.
- Do NOT emulate a chord with two `saolei_click` calls — use `saolei_chord_click` (the atomic single simultaneous left+right press).
