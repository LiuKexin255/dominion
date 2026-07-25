---
name: saolei
description: Operate a grid-based Minesweeper game through the four stateless saolei MCP tools (saolei_init, saolei_click, saolei_flag, saolei_chord_click). Use this skill when the session profile has the saolei MCP enabled and you must start a new game, reveal cells, place flags, or chord numbers on a bound Minesweeper window.
compatibility: opencode
metadata:
  audience: dominion
  scope: saolei-mcp
---

# saolei

This skill guides the model in operating a grid-based Minesweeper game through the saolei MCP tools. The agent hosts a per-session MCP server that exposes four stateless tools; the model uses them — INSTEAD OF the raw mouse tools — to play the game.

Authority: `specs/023-saolei-mcp-refine/spec.md` (FR-016..FR-022), `specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md` §6, `specs/023-saolei-mcp-refine/data-model.md` §7.

## Stateless design

The four tools are **stateless dispatchers**: each dispatches its operation to the bound desktop window and returns the post-action screenshot. There is no agent-side grid-state model, no validation, and no `saolei_update` tool. Tools are callable back-to-back with no intervening step — you do NOT need to report cell statuses to the agent between operations. Instead, you read the returned screenshot to track the board state yourself.

## Coordinate convention

All cell coordinates use a **top-left origin `(0, 0)`** grid:

- `x` = column index, increases left → right.
- `y` = row index, increases top → bottom.

Pixel geometry (board origin offset, cell size) is fixed inside the agent and is never sent by the model. The model infers board bounds from the returned screenshot.

Example: on a board, the top-left cell is `(0, 0)`; one cell to its right is `(1, 0)`; one cell below it is `(0, 1)`.

## Tools

### saolei_init()

Start a new minesweeper game. Takes **no arguments**.

Behavior: dispatches an F2 keypress to the bound window (the Minesweeper new-game shortcut) and returns the post-init screenshot.

Call this FIRST (before any cell operation), and again whenever the human restarts the game or changes the difficulty. Re-calling re-dispatches F2 (restarts the game).

### saolei_click(x, y)

Left-click (reveal) the cell at `(x, y)`.

Behavior: dispatches a combined move + left-click via window messages at the cell's fixed pixel centre and returns the post-action screenshot. The click reveals the target cell; numbers cascade per Minesweeper rules (a revealed blank reveals its connected neighborhood). If the clicked cell is a mine, the game ends.

### saolei_flag(x, y)

Right-click to toggle a flag on the cell at `(x, y)`.

Behavior: dispatches a combined move + right-click via window messages at the cell's fixed pixel centre and returns the post-action screenshot. A flag is a marker for your own reasoning; it does not by itself affect the game's win/loss detection. Use it to track where you believe mines are.

### saolei_chord_click(x, y)

Perform a chord — a **single simultaneous left+right button press** — on the cell at `(x, y)`. A chord reveals all unflagged neighbors of a satisfied number cell in one atomic action (a "satisfied" number is one whose adjacent flag count equals its number).

Behavior: dispatches a combined move + chord via window messages at the cell's fixed pixel centre and returns the post-action screenshot. If any flag is misplaced (a real mine remains among the unflagged neighbors), the chord reveals that mine and the game ends.

Do NOT emulate a chord with two separate clicks; `saolei_chord_click` is the single atomic operation.

## Reading the screenshot

Every tool returns a screenshot of the board after the operation. **Read the returned screenshot to track the board state** — identify which cells are still INITIAL, which are revealed numbers, which are flagged, and whether the game has been won or lost. The agent holds no grid state, so the screenshot is your sole source of truth for the current board.

## Example play flow

A simple supervised reveal sequence using only the saolei tools:

```
1. saolei_init()
   → F2 dispatched; new game started. Read the returned screenshot to see
     the fresh board (all cells INITIAL).

2. saolei_click(x=4, y=4)
   → Left-click dispatched at the centre cell. Read the returned screenshot
     to see the revealed region (a blank cascade with number boundaries).

3. saolei_click(x=7, y=7)
   → Another left-click — callable immediately, no intervening step needed.
     Read the returned screenshot.

4. saolei_flag(x=3, y=3)
   → Right-click dispatched; place a flag where a mine is suspected.
     Read the returned screenshot to confirm the flag.

5. saolei_chord_click(x=4, y=4)
   → Adjacent flag count equals the cell's number → chord dispatched.
     Read the returned screenshot to see the revealed neighbors.
```

Key points demonstrated:

- `saolei_init` takes no arguments.
- Operations are callable back-to-back with no intervening step.
- You read the returned screenshot after every operation to track the board.

## When NOT to use

- Do NOT call `mouse_move` or `mouse_click` — for a saolei-enabled profile those raw mouse tools are intentionally excluded. Use the saolei tools exclusively as the operation channel.
- Do NOT emulate a chord with two `saolei_click` calls — use `saolei_chord_click` (the atomic single simultaneous left+right press).
- There is no `saolei_update` tool — cell statuses are NOT reported to the agent. Read the screenshot instead.
