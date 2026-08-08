---
name: saolei
description: Operate a grid-based Minesweeper game through the three saolei MCP tools (saolei_init, saolei_operate, saolei_remain). saolei_operate executes cell operations in a dual form — a single type/x/y (click/flag/chord) or a batch operations array. Use this skill when the session profile has the saolei MCP enabled and you must start a new game, reveal cells, place flags, chord numbers, or query remaining mines on a bound Minesweeper window.
compatibility: opencode
metadata:
  audience: dominion
  scope: saolei-mcp
---

# saolei

This skill guides the model in operating a grid-based Minesweeper game through the saolei MCP tools. The agent hosts a per-session MCP server that exposes three tools — `saolei_init` (start a game), `saolei_operate` (execute one or more cell operations — click/flag/chord — in order), and the read-only `saolei_remain` query; the model uses them — INSTEAD OF the raw mouse tools — to play the game.

Authority: `specs/025-desktop-image-state-refine/spec.md` (FR-012..FR-022), `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`, `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` (saolei_operate dual form, FR-001..FR-005), `projects/game/pkg/saolei-board/README.md`.

## Recognized text board (NOT a screenshot)

The agent **recognizes** the board deterministically from the desktop screenshot with `@dominion/game-saolei-board` and returns the board as **text**. Every tool result is a TEXT board — there is **no screenshot** returned for you to read. Do NOT expect or try to read pixels from a returned image; the board state arrives as a compact text grid you can reason about directly.

Before dispatching any cell operation, the agent **validates** the requested move against the latest recognized board. An illegal move is **handled before dispatch** with a clear reason (see "Move validation" below) — the desktop never receives an illegal operation. A legal move dispatches and the result returns the updated text board.

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

Every text board carries a **coordinate ruler** so you can locate any cell without counting symbols. Above the grid is a **column-index header row** whose first slot is blank and whose remaining slots are the column labels `col0`, `col1`, …; each grid row is prefixed with its **row label** `row0`, `row1`, …. The indices are **0-based, top-left origin** — identical to the `(x, y)` arguments of `saolei_operate`, so `col0`/`row0` is the first column/row. Each label is **tagged** with the `col`/`row` prefix so it cannot be confused with the `0`–`8` cell values: `col3` is column index 3, `row1` is row index 1, while a bare `3` or `1` on the grid is a game-state number. To target a cell, read its `col<N>` header and `row<N>` prefix and pass those N values as `(x, y)`.

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

1. **Outcome line** — what happened. Per tool:
   - `saolei_init` success: `new game started`
   - `saolei_operate` normal completion: `saolei_operate → executed N ops`
   - `saolei_operate` with skipped no-ops: `saolei_operate → executed M ops, skipped S no-op ops`
   - `saolei_operate` stopped mid-batch: `saolei_operate → stopped at op K (reason)` (reason = a structural rejection code or `won`/`lost`)
   - `saolei_operate` illegal argument combination: `saolei_operate → rejected: ...` (exact texts in the "saolei_operate" section below)
   - No active game: `rejected: no_active_game` followed by `call saolei_init first to start a game.` (no game-status line, no board)
   - Recognition failure: `unable to recognize board` followed by `call saolei_init to start a new game.` (no game-status line, no board)
   - `saolei_remain` success: `saolei_remain → computed`
2. **Game-status line** — `game status: won`, `game status: lost`, or `game status: playing`, derived from the recognized board (won = every cell is a revealed number or a flag AND the game's mine counter reads `000`, meaning the placed flag count equals the mine count; lost = a mine `X`/`M` is visible; playing = otherwise). This line tells you whether the game is finished — read it before parsing the board. **It is omitted only when there is no recognized board** (`no_active_game` rejection, `unable to recognize board`) or on an illegal-argument rejection (no board is consulted).
3. **The text board** — the `board size <w>*<h>` header and the symbol grid. The `valid range: x 0..<w-1>, y 0..<h-1>` line appears on `rejected: <reason>` bodies only; a `saolei_operate → stopped at op K (...)` body carries the status line and board but no valid-range line.

A winning or losing status is surfaced on the very operation whose recognized board first reflects it. A win or loss is **terminal** — see "Move validation" below: once the status is `won` or `lost`, any further cell operation stops before dispatch with reason `game_won` or `game_over`.

Worked example — a legal `saolei_operate` click on an in-progress board:

```
saolei_operate → executed 1 ops
game status: playing

board size 9*9

     col0 col1 col2 col3 col4 col5 col6 col7 col8
row0    *    *    *    1    0    0    1    M    *
row1    *    *    2    1    0    0    1    2    *
row2    *    *    1    0    0    0    0    1    *
...
```

Worked example — a single harmless no-op (a click on an already-revealed number is SKIPPED, not rejected):

```
saolei_operate → executed 0 ops, skipped 1 no-op ops
game status: playing

board size 9*9

...
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

### saolei_operate(type, x, y) / saolei_operate(operations: [{type, x, y}, ...])

Execute one or more cell operations **in order** and return ONE result with the final TEXT board (`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md` §1-3). The two argument forms are **mutually exclusive** and **semantically equivalent** — a single `type`/`x`/`y` call is exactly a length-1 `operations` batch:

- **Single form** — `type` + `x` + `y`: one operation. `type` MUST be one of `click` / `flag` / `chord`, and all three of `type`/`x`/`y` MUST be present together (a partial triple is rejected).
- **Batch form** — `operations`: an ordered array `[{type, x, y}, ...]`. Operations are validated and executed in the declared order.

Operation types (absorbing the former `saolei_click` / `saolei_flag` / `saolei_chord_click` tools):

- `click` — left-click (reveal) the cell at `(x, y)`. The click reveals the target cell; numbers cascade per Minesweeper rules (a revealed blank reveals its connected neighborhood). If the clicked cell is a mine, the game ends (the board shows `X`).
- `flag` — right-click to toggle a flag on the cell at `(x, y)`. A flag is a marker for your own reasoning; it does not by itself affect the game's win/loss detection. Use it to track where you believe mines are.
- `chord` — a **single simultaneous left+right button press** on the cell at `(x, y)`. A chord reveals all unflagged neighbors of a satisfied number cell in one atomic action (a "satisfied" number is one whose adjacent flag count equals its number). If any flag is misplaced (a real mine remains among the unflagged neighbors), the chord reveals that mine and the game ends. Do NOT emulate a chord with two separate clicks — a chord op is the single atomic operation.

Every op is validated strictly against the recognized board before dispatch. Failures are triaged by reason (contract §2 / FR-002):

- **Harmless no-op** (click a revealed/flagged cell, flag a revealed cell, chord a non-number, chord a number with no unrevealed neighbor) — the op is **SKIPPED** (board unchanged) and the batch continues with the remaining ops. A single no-op call shows `saolei_operate → executed 0 ops, skipped 1 no-op ops`.
- **Structural rejection / game end** — an out-of-bounds or no-active-game rejection, or an op that ends the game (won/lost) — the batch **STOPS** at that op; earlier successful operations take effect and the remaining ops are NOT executed. The result shows `saolei_operate → stopped at op K (reason)`.
- An empty `operations` list is a no-op: it returns the current board with `saolei_operate → executed 0 ops` and produces no side effect.

Argument-combination rejections (the call is refused, NOTHING is dispatched; the exact texts):

- Both forms together — `saolei_operate → rejected: provide EITHER type/x/y (single operation) OR operations (batch), not both.`
- Neither form — `saolei_operate → rejected: provide EITHER type/x/y (single operation) OR an operations array (batch).`
- Partial single form (e.g. `type` without `x`/`y`) — `saolei_operate → rejected: the single-operation form requires ALL of type, x and y together.`

### saolei_remain()

A **read-only deduction aid**. Takes **no arguments**.

`saolei_remain` does **not** dispatch any operation to the desktop and does **not** change the board — it is purely computational. It reads the latest recognized board and returns a **remain grid** of the same dimensions, where each cell shows how many mines are still unflagged around it:

- For a revealed number cell (`1`–`8`), the value is `number − (count of adjacent flags)` — i.e. the remaining unmarked mines. This may be `0` (the number is fully satisfied by adjacent flags) or **negative** (more flags surround it than its value — an over-flag error you should correct).
- For every other cell (`0`/blank, `*`, `F`, `X`, `M`, `?`), the value is the literal `-` (a non-number carries no mine-deduction information).

The remain grid carries the same tagged coordinate ruler as the board grid (`col<N>` header + `row<N>` prefixes, plus the `board size <w>*<h>` header), so every remain cell lines up with its board cell. Like `saolei_init`/`saolei_operate`, `saolei_remain` rejects with `no_active_game` when no board is recognized (call `saolei_init` first), but it is **not** blocked by a terminal `won`/`lost` board — it is a pure query.

Result body shape:

```
saolei_remain → computed
game status: playing

board size <w>*<h>

<the ruled remain grid — number cells show their remaining count, others show `-`>
```

Use `saolei_remain` when you want a ready-made "mines still left around each number" view instead of scanning the symbol grid and counting adjacent flags by hand. A negative value is a direct signal that a flag is misplaced; a `0` means that number's flag count is exactly right (a chord there would be safe). The lone `-` marker (no digit) is visually distinct from a negative count like `-1`.

## Move validation (illegal moves are triaged before dispatch)

Before dispatching a cell operation, the agent checks the move against the recognized board. A legal move dispatches and returns the updated board. An illegal move is **handled before dispatch** — the desktop never receives it — but the handling depends on the reason (contract §2 / FR-002):

- **Harmless no-op reasons** (`cell_already_revealed`, `cell_is_flagged`, `cannot_flag_revealed`, `chord_requires_number`, `chord_no_unrevealed_neighbor`) — the op does not change the board and is **SKIPPED**; the batch (if any) continues. A single no-op call shows `saolei_operate → executed 0 ops, skipped 1 no-op ops`.
- **Structural / terminal reasons** (`out_of_bounds`, `no_active_game`, `game_over`, `game_won`) — the batch **STOPS** at that op with `saolei_operate → stopped at op K (reason)`; earlier successful ops take effect. (`no_active_game` with no board at all uses the `rejected: no_active_game` body instead — see below.)

A rejection is a normal tool result (not an error); read the reason and the board, then pick a legal cell.

The rule categories:

| Rule (reason code) | When it applies |
|---|---|
| `no_active_game` | You called a cell operation before `saolei_init`, or the board state was invalidated by a recognition failure. Call `saolei_init` first. When no board exists at all the result is `rejected: no_active_game` with guidance; the status line and board are omitted. |
| `out_of_bounds` | The `(x, y)` coordinate is outside the board dimensions. The op stops the batch (`stopped at op K (out_of_bounds)`); earlier successful ops take effect. |
| `game_over` | The current game is already lost (a mine `X`/`M` is visible). The op stops the batch (`stopped at op K (game_over)`). Call `saolei_init` to start a new game. |
| `game_won` | The current game is already won (`game status: won`). A win is terminal exactly like a loss — any cell operation after a win stops before dispatch. The op stops the batch (`stopped at op K (game_won)`). Call `saolei_init` to start a new game. |
| `cell_already_revealed` | A `click` op on an already-revealed number (`0`–`8`) — a no-op, SKIPPED (the batch continues). |
| `cell_is_flagged` | A `click` op on a flagged cell (`F`) — a flagged cell is protected, SKIPPED. |
| `cannot_flag_revealed` | A `flag` op on a revealed number (`0`–`8`) — you cannot flag an open cell, SKIPPED. |
| `chord_requires_number` | A `chord` op on anything but a revealed number `1`–`8` (i.e. on `0`, `*`, or `F`). A chord is only permitted on a revealed number, SKIPPED. |
| `chord_no_unrevealed_neighbor` | A `chord` op on a revealed number `1`–`8` whose neighbors are all revealed numbers, flags, or mines — there is no unrevealed cell (`*`) and no uncertain cell (`?`) for the chord to reveal. The chord would be a guaranteed no-op, SKIPPED; pick a different target or flag/unflag first. |

Important nuance: a chord whose adjacent-flag count does NOT equal the number is still a **legal** move and is NOT rejected — it may simply reveal nothing. Validation judges whether the target cell is a valid chord target with something to reveal, not whether the chord will succeed. So you will not see a rejection for "wrong flag count"; only for chording a non-number cell, or chording a number whose every neighbor is already revealed/flagged (nothing left to reveal).

A cell recognized as `?` (uncertain) is never rejected solely for being uncertain — treat `?` as possibly unrevealed and proceed.

If the agent cannot recognize the board from the screenshot (e.g. the window is not a Minesweeper board, or the screenshot is unreadable), the tool returns `unable to recognize board` and the state is invalidated; subsequent cell operations are rejected as `no_active_game` until you call `saolei_init` to re-seed.

## Example play flow

### Example A — single-form operations (one op per call)

A simple supervised reveal sequence using the single form of `saolei_operate`. Every result body carries `game status: <status>` between the outcome line and the board; the status here stays `playing` until the game ends.

```
1. saolei_init()
   → F2 dispatched; result body:
     new game started
     game status: playing

     board size 9*9
     ... (a grid of `*`) ...

2. saolei_operate(type="click", x=4, y=4)
   → Legal (cell is `*`): dispatched; result body:
     saolei_operate → executed 1 ops
     game status: playing

     board size 9*9
     ... (the revealed region, a blank cascade with number boundaries) ...

3. saolei_operate(type="click", x=7, y=7)
   → Another legal reveal — callable immediately, no intervening step needed.
     Same body shape as step 2 with the updated board.

4. saolei_operate(type="flag", x=3, y=3)
   → Place a flag where a mine is suspected. Result body:
     saolei_operate → executed 1 ops
     game status: playing

     board size 9*9
     ... (`F` at (3,3)) ...

5. saolei_operate(type="chord", x=4, y=4)
   → Adjacent flag count satisfies the cell's number → chord dispatched.
     Result body:
     saolei_operate → executed 1 ops
     game status: playing

     board size 9*9
     ... (the neighbors revealed) ...
```

### Example B — batch form (multiple ops in ONE call, ONE result)

The batch form executes the ops in order and returns a SINGLE result reflecting the final board — use it to cut round trips when you already know several consecutive ops (e.g. reveal a region, then flag its boundary):

```
1. saolei_init()
   → new game started / game status: playing / board size 9*9 (all `*`)

2. saolei_operate(operations=[{type:"click",x:4,y:4},{type:"click",x:5,y:4},
                              {type:"flag",x:3,y:3},{type:"chord",x:4,y:4}])
   → All four ops validated and executed IN ORDER; ONE result body:
     saolei_operate → executed 4 ops
     game status: playing

     board size 9*9
     ... (the final board after all four ops) ...
```

If an op in the batch is a harmless no-op, it is skipped and the batch continues (`executed 3 ops, skipped 1 no-op ops`). If an op is a structural rejection or ends the game, the batch stops there (`stopped at op 3 (out_of_bounds)` / `stopped at op 4 (lost)`) and the remaining ops are not executed.

### Game end

If an operation reveals the last safe cell (or flags the last mine), the same operation's result carries `game status: won` instead. The NEXT cell operation on that board then stops before dispatch:

```
saolei_operate → stopped at op 1 (game_won)
game status: won

board size 9*9
... (the winning board — all cells `0`–`8` or `F`) ...
```

Call `saolei_init` to start a new game. (Symmetrically, a board with `X`/`M` carries `game status: lost` and further ops stop with `game_over`; an op that ITSELF ends the game shows `stopped at op K (lost)`.)

Key points demonstrated:

- `saolei_init` takes no arguments and returns a TEXT board.
- Every result body carries the `game status:` line between the outcome line and the board (omitted only when there is no recognized board, or on an illegal-argument rejection).
- Operations are callable back-to-back; each returns the updated TEXT board.
- Batch operations execute in order and return ONE result — read the final board, and the outcome line tells you whether any op was skipped (`executed M ops, skipped S no-op ops`) or where the batch stopped (`stopped at op K (reason)`).
- Read the `game status:` line first — it tells you whether the game is finished before you parse the board. A win or loss is terminal.
- Read the returned text board (symbols above) to track the game — never a screenshot.

## When NOT to use

- Do NOT call `mouse_move` or `mouse_click` — for a saolei-enabled profile those raw mouse tools are intentionally excluded. Use the saolei tools exclusively as the operation channel.
- Do NOT emulate a chord with two separate `saolei_operate` click ops — use a single `saolei_operate` op of type `chord` (the atomic single simultaneous left+right press).
- There is no `saolei_update` tool and no need to report cell statuses to the agent — the agent recognizes the board itself and returns it as text after every operation.
- Do NOT try to read a returned screenshot — saolei tool results are text boards only.
