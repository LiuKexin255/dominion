---
name: saolei
description: Operate a grid-based Minesweeper game through the five saolei MCP tools (saolei_init, saolei_click, saolei_flag, saolei_chord_click, saolei_remain). Use this skill when the session profile has the saolei MCP enabled and you must start a new game, reveal cells, place flags, chord numbers, or query remaining mines on a bound Minesweeper window.
compatibility: opencode
metadata:
  audience: dominion
  scope: saolei-mcp
---

# saolei

This skill guides the model in operating a grid-based Minesweeper game through the saolei MCP tools. The agent hosts a per-session MCP server that exposes five tools — four operation tools (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) plus the read-only `saolei_remain` query; the model uses them — INSTEAD OF the raw mouse tools — to play the game.

Authority: `specs/025-desktop-image-state-refine/spec.md` (FR-012..FR-022), `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`, `projects/game/pkg/saolei-board/README.md`.

## Recognized text board (NOT a screenshot)

The agent **recognizes** the board deterministically from the desktop screenshot with `@dominion/game-saolei-board` and returns the board as **text**. Every tool result is a TEXT board — there is **no screenshot** returned for you to read. Do NOT expect or try to read pixels from a returned image; the board state arrives as a compact text grid you can reason about directly.

Before dispatching any cell operation, the agent **validates** the requested move against the latest recognized board. An illegal move is **rejected before dispatch** with a clear reason code (see "Move validation" below) — the desktop never receives an illegal operation. A legal move dispatches and the result returns the updated text board.

The agent holds the recognized board state across calls within a game (seeded on `saolei_init`, refreshed from each subsequent screenshot), so validation always uses the freshest board.

### Symbol legend

Each cell is rendered as one symbol (space-separated rows):

| Symbol | Meaning |
|---|---|
| `*` | Unrevealed (initial) cell |
| `0`–`8` | Revealed number |
| `F` | Flag |
| `X` | The triggered mine (you stepped on it — game lost) |
| `M` | A mine revealed at end-game (all mines shown on a loss) |
| `?` | Recognition uncertain (the agent is lenient here; treat it as possibly unrevealed) |

### Coordinate ruler (tagged `col<N>`/`row<N>`)

Every text board carries a **coordinate ruler** so you can locate any cell without counting symbols. Above the grid is a **column-index header row** whose first slot is blank and whose remaining slots are the column labels `col0`, `col1`, …; each grid row is prefixed with its **row label** `row0`, `row1`, …. The indices are **0-based, top-left origin** — identical to the `(x, y)` arguments of `saolei_click`/`saolei_flag`/`saolei_chord_click`, so `col0`/`row0` is the first column/row. Each label is **tagged** with the `col`/`row` prefix so it cannot be confused with the `0`–`8` cell values: `col3` is column index 3, `row1` is row index 1, while a bare `3` or `1` on the grid is a game-state number. To target a cell, read its `col<N>` header and `row<N>` prefix and pass those N values as `(x, y)`.

Example text board (a 9-wide board with a revealed region):

```
board size 9*9

     col0 col1 col2 col3 col4 col5 col6 col7 col8
row0    *    *    *    1    0    0    1    M    *
row1    *    *    2    1    0    0    1    2    *
row2    *    *    1    0    0    0    0    1    *
```

### Tool-result body shape

Every tool result is a single TEXT content block whose body has three layers, in this fixed order:

1. **Outcome line** — what happened: `new game started`, `<tool> at (x,y) → dispatched`, `rejected: <reason>`, or `unable to recognize board`.
2. **Game-status line** — `game status: won`, `game status: lost`, or `game status: playing`, derived from the recognized board (won = every cell is a revealed number or a flag AND the game's mine counter reads `000`, meaning the placed flag count equals the mine count; lost = a mine `X`/`M` is visible; playing = otherwise). This line tells you whether the game is finished — read it before parsing the board. **It is omitted only when there is no recognized board** (`no_active_game` rejection, or `unable to recognize board`).
3. **The text board** — the `board size <w>*<h>` header and the symbol grid (plus a `valid range: x 0..<w-1>, y 0..<h-1>` line on rejections).

A winning or losing status is surfaced on the very operation whose recognized board first reflects it. A win or loss is **terminal** — see "Move validation" below: once the status is `won` or `lost`, any further cell operation is rejected before dispatch.

Worked example — a legal `saolei_click` on an in-progress board:

```
saolei_click at (4,4) → dispatched
game status: playing

board size 9*9

     col0 col1 col2 col3 col4 col5 col6 col7 col8
row0    *    *    *    1    0    0    1    M    *
row1    *    *    2    1    0    0    1    2    *
row2    *    *    1    0    0    0    0    1    *
...
```

Worked example — a rejection on an in-progress board:

```
rejected: cell_already_revealed
game status: playing

board size 9*9

...
valid range: x 0..8, y 0..8
```

## Coordinate convention

All cell coordinates use a **top-left origin `(0, 0)`** grid:

- `x` = column index, increases left → right.
- `y` = row index, increases top → bottom.

Pixel geometry (board origin offset, cell size) is fixed inside the agent and is never sent by the model. The model reads the board bounds from the `board size <w>*<h>` header of every text board.

Example: the top-left cell is `(0, 0)`; one cell to its right is `(1, 0)`; one cell below it is `(0, 1)`.

## Tools

### saolei_init()

Start a new minesweeper game. Takes **no arguments**.

Behavior: dispatches an F2 keypress to the bound window (the Minesweeper new-game shortcut), recognizes the initial board from the returned screenshot, and returns it as a TEXT board.

Call this FIRST (before any cell operation), and again whenever the human restarts the game or changes the difficulty. Re-calling re-dispatches F2 (restarts the game) and re-seeds the board state.

### saolei_click(x, y)

Left-click (reveal) the cell at `(x, y)`.

Behavior: validates the move, dispatches a combined move + left-click via window messages at the cell's fixed pixel centre, then recognizes and returns the updated TEXT board. The click reveals the target cell; numbers cascade per Minesweeper rules (a revealed blank reveals its connected neighborhood). If the clicked cell is a mine, the game ends (the board shows `X`).

### saolei_flag(x, y)

Right-click to toggle a flag on the cell at `(x, y)`.

Behavior: validates the move, dispatches a combined move + right-click via window messages at the cell's fixed pixel centre, then recognizes and returns the updated TEXT board. A flag is a marker for your own reasoning; it does not by itself affect the game's win/loss detection. Use it to track where you believe mines are.

### saolei_chord_click(x, y)

Perform a chord — a **single simultaneous left+right button press** — on the cell at `(x, y)`. A chord reveals all unflagged neighbors of a satisfied number cell in one atomic action (a "satisfied" number is one whose adjacent flag count equals its number).

Behavior: validates the move, dispatches a combined move + chord via window messages at the cell's fixed pixel centre, then recognizes and returns the updated TEXT board. If any flag is misplaced (a real mine remains among the unflagged neighbors), the chord reveals that mine and the game ends.

Do NOT emulate a chord with two separate clicks; `saolei_chord_click` is the single atomic operation.

### saolei_remain()

A **read-only deduction aid**. Takes **no arguments**.

`saolei_remain` does **not** dispatch any operation to the desktop and does **not** change the board — it is purely computational. It reads the latest recognized board and returns a **remain grid** of the same dimensions, where each cell shows how many mines are still unflagged around it:

- For a revealed number cell (`1`–`8`), the value is `number − (count of adjacent flags)` — i.e. the remaining unmarked mines. This may be `0` (the number is fully satisfied by adjacent flags) or **negative** (more flags surround it than its value — an over-flag error you should correct).
- For every other cell (`0`/blank, `*`, `F`, `X`, `M`, `?`), the value is the literal `-` (a non-number carries no mine-deduction information).

The remain grid carries the same tagged coordinate ruler as the board grid (`col<N>` header + `row<N>` prefixes, plus the `board size <w>*<h>` header), so every remain cell lines up with its board cell. Like the operation tools, `saolei_remain` rejects with `no_active_game` when no board is recognized (call `saolei_init` first), but it is **not** blocked by a terminal `won`/`lost` board — it is a pure query.

Result body shape:

```
saolei_remain → computed
game status: playing

board size <w>*<h>

<the ruled remain grid — number cells show their remaining count, others show `-`>
```

Use `saolei_remain` when you want a ready-made "mines still left around each number" view instead of scanning the symbol grid and counting adjacent flags by hand. A negative value is a direct signal that a flag is misplaced; a `0` means that number's flag count is exactly right (a chord there would be safe). The lone `-` marker (no digit) is visually distinct from a negative count like `-1`.

## Move validation (illegal moves are rejected)

Before dispatching a cell operation, the agent checks the move against the recognized board. A legal move dispatches and returns the updated board; an illegal move is **rejected before dispatch** with a reason code, the current text board, and the valid coordinate range — so you can immediately choose a different, legal move. A rejection is a normal tool result (not an error); read the reason and the board, then pick a legal cell.

The rule categories:

| Rule (reason code) | When it applies |
|---|---|
| `no_active_game` | You called a cell tool before `saolei_init`, or the board state was invalidated by a recognition failure. Call `saolei_init` first. |
| `out_of_bounds` | The `(x, y)` coordinate is outside the board dimensions. The valid range is included in the result. |
| `game_over` | The current game is already lost (a mine `X`/`M` is visible). Call `saolei_init` to start a new game. |
| `game_won` | The current game is already won (`game status: won`). A win is terminal exactly like a loss — any cell operation after a win is rejected. Call `saolei_init` to start a new game. |
| `cell_already_revealed` | `saolei_click` on an already-revealed number (`0`–`8`) — a no-op. |
| `cell_is_flagged` | `saolei_click` on a flagged cell (`F`) — a flagged cell is protected. |
| `cannot_flag_revealed` | `saolei_flag` on a revealed number (`0`–`8`) — you cannot flag an open cell. |
| `chord_requires_number` | `saolei_chord_click` on anything but a revealed number `1`–`8` (i.e. on `0`, `*`, or `F`). A chord is only permitted on a revealed number. |
| `chord_no_unrevealed_neighbor` | `saolei_chord_click` on a revealed number `1`–`8` whose neighbors are all revealed numbers, flags, or mines — there is no unrevealed cell (`*`) and no uncertain cell (`?`) for the chord to reveal. The chord would be a guaranteed no-op; pick a different target or flag/unflag first. |

Important nuance: a chord whose adjacent-flag count does NOT equal the number is still a **legal** move and is NOT rejected — it may simply reveal nothing. Validation judges whether the target cell is a valid chord target with something to reveal, not whether the chord will succeed. So you will not see a rejection for "wrong flag count"; only for chording a non-number cell, or chording a number whose every neighbor is already revealed/flagged (nothing left to reveal).

A cell recognized as `?` (uncertain) is never rejected solely for being uncertain — treat `?` as possibly unrevealed and proceed.

If the agent cannot recognize the board from the screenshot (e.g. the window is not a Minesweeper board, or the screenshot is unreadable), the tool returns `unable to recognize board` and the state is invalidated; subsequent cell operations are rejected as `no_active_game` until you call `saolei_init` to re-seed.

## Example play flow

A simple supervised reveal sequence using only the saolei tools. Every result body carries `game status: <status>` between the outcome line and the board; the status here stays `playing` until the game ends.

```
1. saolei_init()
   → F2 dispatched; result body:
     new game started
     game status: playing

     board size 9*9
     ... (a grid of `*`) ...

2. saolei_click(x=4, y=4)
   → Legal (cell is `*`): dispatched; result body:
     saolei_click at (4,4) → dispatched
     game status: playing

     board size 9*9
     ... (the revealed region, a blank cascade with number boundaries) ...

3. saolei_click(x=7, y=7)
   → Another legal reveal — callable immediately, no intervening step needed.
     Same body shape as step 2 with the updated board.

4. saolei_flag(x=3, y=3)
   → Place a flag where a mine is suspected. Result body:
     saolei_flag at (3,3) → dispatched
     game status: playing

     board size 9*9
     ... (`F` at (3,3)) ...

5. saolei_chord_click(x=4, y=4)
   → Adjacent flag count satisfies the cell's number → chord dispatched.
     Result body:
     saolei_chord_click at (4,4) → dispatched
     game status: playing

     board size 9*9
     ... (the neighbors revealed) ...
```

If a click reveals the last safe cell (or flags the last mine), the same operation's result carries `game status: won` instead. The NEXT cell operation on that board is then rejected before dispatch:

```
rejected: game_won
game status: won

board size 9*9
... (the winning board — all cells `0`–`8` or `F`) ...
valid range: x 0..8, y 0..8
```

Call `saolei_init` to start a new game. (Symmetrically, a board with `X`/`M` carries `game status: lost` and rejects further ops as `game_over`.)

Key points demonstrated:

- `saolei_init` takes no arguments and returns a TEXT board.
- Every result body carries the `game status:` line between the outcome line and the board (omitted only when there is no recognized board).
- Operations are callable back-to-back; each returns the updated TEXT board.
- Read the `game status:` line first — it tells you whether the game is finished before you parse the board. A win or loss is terminal.
- Read the returned text board (symbols above) to track the game — never a screenshot.

## When NOT to use

- Do NOT call `mouse_move` or `mouse_click` — for a saolei-enabled profile those raw mouse tools are intentionally excluded. Use the saolei tools exclusively as the operation channel.
- Do NOT emulate a chord with two `saolei_click` calls — use `saolei_chord_click` (the atomic single simultaneous left+right press).
- There is no `saolei_update` tool and no need to report cell statuses to the agent — the agent recognizes the board itself and returns it as text after every operation.
- Do NOT try to read a returned screenshot — saolei tool results are text boards only.
