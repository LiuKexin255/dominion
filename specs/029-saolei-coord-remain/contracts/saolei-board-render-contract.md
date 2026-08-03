# Contract: Saolei board text renderer — coordinate ruler

**Feature**: `029-saolei-coord-remain` | **Date**: 2026-07-28 | **Spec**: [spec.md](../spec.md) | **Research**: [research.md](../research.md) D1/D2

This contract specifies the **text-board coordinate ruler** and the new library export that produces it. It **refines** (does not replace) [025 — saolei-mcp-contract.md §3](../../025-desktop-image-state-refine/contracts/saolei-mcp-contract.md) (the text-board return shape) and [027 — saolei-mcp-status-contract.md §2](../../027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md) (the body shapes): the `board size` header, the symbol legend, the one-symbol-per-cell grid, the `game status:` line, and the `valid range:` line are all unchanged; a coordinate ruler is added around the grid, and the renderer is decomposed for reuse by the remain grid.

The recognition algorithm, the `GameState` type, and the `cellSymbol` mapping are **unchanged**.

---

## §1 — Ruled text-board format

`renderBoardText(state: GameState): string` (`projects/game/pkg/saolei-board/src/core/render.ts`) produces:

```
board size <width>*<height>
<blank>
<column-index header row>
<data row 0>
...
<data row height-1>
```

Where the grid portion (header + data rows) is produced by the new `renderGridWithRuler` (§2), and:

- The `board size <width>*<height>` header line and the single blank separator are **retained verbatim** (spec FR-003).
- The symbol legend is unchanged: `*` INITIAL, `0`–`8` revealed number, `F` FLAG, `X` HIT_MINE, `M` MINE, `?` UNKNOWN.
- Each column index is tagged `col<N>` and each row index `row<N>` (0-based), so the ruler cannot be confused with the `0`–`8` cell values (spec FR-002).
- The ruler is rendered **iff** a grid is rendered. Bodies that carry no board (`no_active_game` with no state, `unable to recognize board`) are unchanged (spec FR-004).

### Worked examples

9×9 (columnWidth 4 — index labels `col0`..`col8` / `row0`..`row8` are 4 chars; symbols right-aligned to 4):
```
board size 9*9

        col0 col1 col2 col3 col4 col5 col6 col7 col8
row0       *    *    *    1    0    0    1    M    *
row1       *    *    2    1    0    0    1    2    *
row2       *    *    1    0    0    0    0    1    *
```

16×16 (columnWidth 5 — `col10`..`col15` / `row10`..`row15` are 5 chars):
```
board size 16*16

          col0  col1  col2  col3  col4  col5  col6  col7  col8  col9 col10 col11 col12 col13 col14 col15
row0         *     *     *     *     *     *     *     *     *     *     *     *     *     *     *     *
row1         *     *     *     *     *     *     *     *     *     *     *     *     *     *     *     *
```

### Invariants

- Indices are **0-based**, top-left origin: column index = `x`, row index = `y`, identical to the `(x, y)` arguments of `saolei_click`/`saolei_flag`/`saolei_chord_click` (spec FR-002). `col0`/`row0` is the first column/row.
- The Nth space-separated token of any data row (after the `row<N>` prefix) is the cell at `(x = N, y = rowIndex)`. (Visual alignment is best-effort for variable-width cell tokens; the positional/semantic mapping is exact.)
- The `valid range: x 0..<width-1>, y 0..<height-1>` line on rejection bodies is retained verbatim (spec FR-005).

---

## §2 — `renderGridWithRuler` (new export)

**Signature** (`projects/game/pkg/saolei-board/src/core/render.ts`, exported via `src/core/index.ts`):

```ts
export function renderGridWithRuler(
  width: number,
  height: number,
  tokenAt: (x: number, y: number) => string,
): string;
```

**Behavior**: renders `height + 1` lines (one column-index header row + `height` data rows), each a row of space-separated, right-aligned slots. Index labels are tagged: column labels are `col` + index, row labels are `row` + index (0-based).

1. Compute `columnWidth = max(1, width of "col"+String(max(width-1,height-1)), width of "row"+String(max(width-1,height-1)), maxTokenWidth)`, where `maxTokenWidth` is the length of the longest token returned by `tokenAt` over all `(x, y)` (0 when the grid is empty). (The `col`/`row` prefix is 3 chars, so the index labels drive `columnWidth` — e.g. 4 for a ≤9-wide/tall board, 5 for a 10–16 board.)
2. Header row slots: `["", "col0", "col1", ..., "col"+String(width - 1)]` (the first slot is the empty string, occupying the row-index column). Each slot right-aligned to `columnWidth`.
3. Data row `y` slots: `["row"+String(y), tokenAt(0, y), tokenAt(1, y), ..., tokenAt(width - 1, y)]`. Each slot right-aligned to `columnWidth`.
4. Join each row's slots with a single space; join the rows with `\n`.

**Postconditions**:
- Pure: no I/O, no mutation, no side effects; a pure function of `width`, `height`, and `tokenAt`.
- Returns exactly `height + 1` lines (`height ≥ 0`). For `height = 0` returns just the header row.
- The header's `col<N>` label is vertically aligned with data row `y`'s `tokenAt(N, y)` (both are slot `N + 1`, right-aligned to the same `columnWidth`).

**Relationship to `renderBoardText`**: `renderBoardText(state)` becomes

```ts
`board size ${state.width}*${state.height}\n\n` +
  renderGridWithRuler(state.width, state.height, (x, y) =>
    cellSymbol(state.grid[y]?.[x] ?? "UNKNOWN"));
```

i.e. the symbol grid is the ruled grid whose `tokenAt` maps each cell to its legend symbol. `cellSymbol` is unchanged. The `col`/`row` index tags are generated by `renderGridWithRuler` itself (the caller's `tokenAt` supplies only cell content), so the board grid and the remain grid automatically share the identical tagged ruler.

**Caller(s)**:
- `renderBoardText` (board grid) — internal.
- The agent's `saolei_remain` tool (remain grid) — imports `renderGridWithRuler` from `@dominion/game-saolei-board` and supplies a `tokenAt` that returns remain values / `-` (see [saolei-remain-tool-contract.md](saolei-remain-tool-contract.md) §3). This is the single point guaranteeing both grids share one ruler implementation (spec SC-004).

---

## §3 — What is NOT changed

- The recognition algorithm, `recognizeBoard`, `SaoleiBoard` (incl. `updateFromScreenshot` monotonic validation), and the `GameState` / `CellStatus` / `MineCounter` types — unchanged.
- `cellSymbol` and the symbol legend — unchanged.
- The `board size <w>*<h>` header text and the blank-line separator — unchanged.
- The CLI (`projects/game/pkg/saolei-board/src/cli/cli.ts`) — unchanged in code; it prints `renderBoardText` and therefore auto gains the ruler.
- The desktop chat bubble — renders tool-result text verbatim; no desktop change (verified: no board-text parsing in `projects/game/desktop`).

---

## §4 — Golden-fixture impact

`projects/game/pkg/saolei-board/src/core/golden.test.ts` compares `renderBoardText(recognizeBoard(png))` to `testdata/<name>.golden.txt` by exact equality. Adding the ruler changes every golden's text. The **code** of `golden.test.ts` is unchanged; the **9 golden fixtures** (`saolei_1`, `saolei_2`, `saolei_3`, `saolei_4`, `saolei_5`, `saolei_6`, `saolei_7`, `saolei_8`, `saolei_10`) are regenerated by re-running the CLI per the README calibration flow:

```bash
bazel run //projects/game/pkg/saolei-board:cli -- testdata/<name>.png > testdata/<name>.golden.txt
```

(preserving the header + blank line; the test strips trailing whitespace via `.replace(/\s+$/, "")`). Recognition is untouched — only the rendering format changes — so the regenerated goldens remain valid recognition references. The non-golden recognition assertions (`isWin`, `mineCounter`) in `golden.test.ts` are ruler-invariant.

### Library unit-test anchor updates

Exact-match assertions that encode the pre-ruler format are updated to the ruled format (the new expected strings are derivable from §1/§2):

- `render.test.ts`: the `lines[2]`/`lines[3]` (and second-row) assertions become the tagged header row and the prefixed data rows. `startsWith("board size 9*9\n\n")` is preserved.
- `recognize.test.ts`: `SaoleiBoard.renderText()` and `renderBoardText` become the tagged, ruled equivalents, e.g. `board size 2*1\n\n     col0 col1\nrow0    1    0` and `board size 3*1\n\n     col0 col1 col2\nrow0    *    0    2`.

These are mechanical updates; the contract format above is the source of truth for the expected strings.
