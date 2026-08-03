# Data Model: Saolei Board Coordinate Ruler & Remain Tool

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

This feature introduces **no new persistent state** and **no new proto types**. It adds one derived (computed) view and changes the text rendering of an existing entity. The entities below extend the existing `GameState` model from `@dominion/game-saolei-board` (`projects/game/pkg/saolei-board/src/core/types.ts`) and the per-session recognized state in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`.

---

## Entity 1 — Text board (with ruler)

**What it represents**: The recognized `GameState` rendered as the compact text board returned by every saolei MCP tool result that carries a board, and printed by the `saolei-recognize` CLI. Unchanged in *meaning* (header + symbol grid per the existing legend); gains a coordinate ruler.

**Source**: `renderBoardText(state: GameState): string` in `projects/game/pkg/saolei-board/src/core/render.ts`.

**Shape** (after this feature):

```
board size <width>*<height>
<blank>
<column-index header row>
<data row 0>
<data row 1>
...
<data row height-1>
```

- `board size <width>*<height>` — unchanged header line.
- `<blank>` — unchanged single blank separator.
- **Column-index header row** — NEW. The string `""` (blank, for the row-index column) followed by column labels `col0..col<width-1>` (0-based, tagged), every slot right-aligned to `columnWidth`, joined by single spaces.
- **Data rows** — each is the row label `row<y>` (0-based, tagged) followed by the cell symbols (`*`, `0`–`8`, `F`, `X`, `M`, `?` per the legend), every slot right-aligned to `columnWidth`, joined by single spaces.
- `columnWidth = max(1, width of "col"/"row"+largest index, max cell-token width)`. Index labels are ≥4 chars (`col0`/`row0`), so `columnWidth` is driven by the index labels (4 for a ≤9-wide/tall board, 5 for 10–16); cell symbols (1 char) are right-aligned within that width.

**Attributes**: none beyond `GameState` (`width`, `height`, `grid`, optional `mineCounter`). The renderer is a pure function of `GameState`.

**Validation rules**: none — the text board is a rendering; recognition/validation of `GameState` is unchanged.

**State transitions**: none — derived on every render from the latest `GameState`.

**Authoritative format reference**: [contracts/saolei-board-render-contract.md](contracts/saolei-board-render-contract.md) §1.

---

## Entity 2 — Remain grid

**What it represents**: A per-cell deduction view, isomorphic to the board grid (same `width`/`height`), giving for each revealed number `1`–`8` the count of mines still unflagged among its neighbors; `-` for every other cell.

**Source**: computed in the `saolei_remain` tool handler (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`); rendered with the library's `renderGridWithRuler`.

**Shape**: identical outer structure to the text board (Entity 1) — `board size <w>*<h>`, blank, ruled grid — but each data-row cell token is a **remain value** instead of a symbol.

**Cell value rule** (spec FR-009, Clarification 2026-07-28):

| `state.grid[y][x]` | Remain token |
|---|---|
| `"1"`..`"8"` | `String(parseInt(cell) − countOfAdjacentFLAG)` — may be `0` (satisfied) or negative (over-flagged); **not** clamped |
| `"0"` (blank) | `"-"` (a blank has no adjacent mines; excluded from the remain view) |
| `"INITIAL"` (`*`), `"FLAG"` (`F`), `"HIT_MINE"` (`X`), `"MINE"` (`M`), `"UNKNOWN"` (`?`) | `"-"` |

- "Adjacent" = the 8 Moore neighbors intersected with the board bounds (edge = 5, corner = 3), via the existing `neighbors(state, x, y)` helper (`saolei-mcp.ts:253`).
- "FLAG" neighbor = a neighbor whose `CellStatus` is `"FLAG"`.
- `columnWidth = max(1, width of "col"/"row"+largest index, max remain-token width)`. Remain tokens are up to 2 chars (`-1`); index labels are ≥4 chars, so `columnWidth ≥ 4` (5 on boards ≥10 wide/tall).

**Attributes**: none of its own — derived purely from `GameState` at call time.

**Validation rules**: none — `saolei_remain` performs no move validation (it is read-only). The only "rejection" is `no_active_game` when no `GameState` exists.

**Lifecycle / state transitions**:
- Computed on demand from the session's latest `recognized: GameState | null`.
- Never persisted, never dispatched, never mutates `recognized`.
- `recognized == null` (pre-`saolei_init`, or invalidated by a recognition failure) ⇒ `no_active_game` (no grid rendered).
- Terminal `won`/`lost` boards ⇒ the remain grid **is** returned (the query is not blocked); the body's `game status:` line reflects the terminal state.

**Authoritative format reference**: [contracts/saolei-remain-tool-contract.md](contracts/saolei-remain-tool-contract.md).

---

## Relationship to existing entities

- **`GameState`** (`projects/game/pkg/saolei-board/src/core/types.ts`): unchanged. Both the text board (Entity 1) and the remain grid (Entity 2) are pure functions of it.
- **Per-session `recognized` state** (`saolei-mcp.ts:536`): unchanged type (`GameState | null`) and lifecycle. `saolei_remain` reads it; it does not write it.
- **`OperationBridge`**: `saolei_remain` does not touch it (no dispatch). The four operation tools' bridge usage is unchanged.
