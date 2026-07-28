# Research: Saolei Board Coordinate Ruler & Remain Tool

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-07-28

This document records the design decisions that resolve the deferred items from `/speckit.clarify` (ruler rendering location, exact ruler format, large-board alignment, remain computation placement, consumer/test impact) and the technical investigation that backs them. Each decision states what was chosen, why, and what alternatives were rejected.

---

## D1 — Where does the coordinate ruler live?

**Decision**: In the **shared library renderer** `@dominion/game-saolei-board` (`projects/game/pkg/saolei-board/src/core/render.ts`). `renderBoardText(state)` gains the ruler, and a new lower-level `renderGridWithRuler(width, height, tokenAt)` is exported so the agent's remain grid can reuse the exact same ruler logic.

**Rationale**:
- `renderBoardText` is already the single source of the text board consumed by **all four** saolei MCP tool bodies (`initSuccessText` / `dispatchedText` / `rejectionText` in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:441,458,480`), the `saolei-recognize` CLI (`projects/game/pkg/saolei-board/src/cli/cli.ts:110`), and `SaoleiBoard.renderText()` (`projects/game/pkg/saolei-board/src/core/recognize.ts:240`). Adding the ruler there fixes every board-bearing output at once (Constitution §II — refactor over patch).
- The remain grid (US2) is a different cell content (remain values vs symbols) but MUST share the identical ruler format (spec SC-004). Extracting `renderGridWithRuler` makes the ruler format exist in exactly one place; `renderBoardText` and the agent's remain renderer both call it. No duplicated ruler logic, no drift.
- Separation of concerns is preserved: the library already owns "render the recognized board as text" (the symbol legend, the `board size` header). A coordinate ruler is a rendering concern of that same text board, so it belongs with the renderer, not in each MCP tool body.

**Alternatives rejected**:
- *MCP-only ruler wrapper* (build the ruler inside each saolei tool body, leave `renderBoardText` alone): rejected — duplicates ruler logic across tool bodies, diverges from the CLI/golden output, and the remain grid would need yet another ruler implementation. Violates §II (patch over refactor).
- *Desktop-side ruler* (render the ruler in the chat bubble): rejected — the desktop renders tool-result text **verbatim** and does not parse the board grid (verified: no `board size` / grid-splitting code in `projects/game/desktop`). The ruler must be in the text the model receives, which is produced agent/library-side.

---

## D2 — Exact ruler format (tagged indices, column-width-aware, right-aligned)

**Decision**: Every rendered grid (board grid and remain grid) is a matrix of **slots** joined by single spaces, where each slot is **right-aligned** to a computed `columnWidth`. Index labels are **tagged** to avoid confusion with the `0`–`8` cell values: columns are `col<N>`, rows are `row<N>` (0-based). The first column is the **row label** (data rows) or **blank** (the header row); the header row lists the **column labels** `col0..col<width-1>`.

```
columnWidth = max(1, len("col"+maxIndex), len("row"+maxIndex), max cell-token width)   // maxIndex = max(width-1,height-1)
headerRow   = ["", "col0", "col1", ..., "col"+(width-1)]   // first slot blank (row-index column)
dataRow(y)  = ["row"+y, token(0,y), token(1,y), ...]
each slot right-aligned to columnWidth, slots joined by " "
```

`renderBoardText` output becomes `board size <w>*<h>` + blank line + header row + data rows.

**Why tagged** (user feedback 2026-07-28): a bare index like `3` is indistinguishable from a revealed-number cell `3`. Prefixing `col`/`row` makes every ruler slot textually distinct from any cell value. Indices stay **0-based** so they remain consistent with the 0-based `saolei_click(x, y)` arguments — a 1-based display would reintroduce the off-by-one confusion the tags exist to eliminate (the user's example `row1`/`col3` denotes 0-based index 1 and 3).

**Worked examples**:

9×9 board grid (index labels `col0`..`col8` / `row0`..`row8` = 4 chars; symbols 1 char ⇒ columnWidth = 4):
```
board size 9*9

        col0 col1 col2 col3 col4 col5 col6 col7 col8
row0       *    *    *    1    0    0    1    M    *
row1       *    *    2    1    0    0    1    2    *
row2       *    *    1    0    0    0    0    1    *
```

16×16 board grid (index labels `col10`..`col15` / `row10`..`row15` = 5 chars ⇒ columnWidth = 5):
```
board size 16*16

          col0  col1  col2  col3  col4  col5  col6  col7  col8  col9 col10 col11 col12 col13 col14 col15
row0         *     *     *     *     *     *     *     *     *     *     *     *     *     *     *     *
row1         *     *     *     *     *     *     *     *     *     *     *     *     *     *     *     *
```

9×9 remain grid (index labels 4 chars; remain tokens up to 2 chars ⇒ columnWidth = 4):
```
board size 9*9

        col0 col1 col2 col3 col4 col5 col6 col7 col8
row0       -    -    -    -    -    -    -    -    -
row1       -    -    0    1    -    -    -    -    -
row2       -    -    1    -    -    -    -    -    -
```

**Rationale**:
- The golden test set contains **both 9×9 and 16×16** boards (`projects/game/pkg/saolei-board/testdata/*.golden.txt`: saolei_1/3/4/5 are 16×16; saolei_2/6/7/8/10 are 9×9). A 16-wide board has column indices `10`–`15`; with the `col`/`row` tags those labels are 5 chars while `col0` is 4, so a fixed-width header would misalign. Right-aligning every slot to a computed `columnWidth` (driven by the widest label) keeps the header aligned with the cells for **all** board sizes.
- Tagged indices (`col<N>`/`row<N>`) are textually distinct from the `0`–`8` cell values, so the model cannot mistake a ruler index for a revealed number — the user's stated motivation.
- The remain grid's tokens are variable-width (`-`, `0`, `2`, `-1`). Right-aligning to `columnWidth` keeps the remain grid internally aligned too (index labels, not tokens, drive the width). Each grid is internally aligned; SC-004 is about index **correctness**, not cross-grid pixel alignment.
- The lone `-` non-number marker stays visually distinct from a negative number (`-1`): the marker is a slot containing exactly `-`; a negative remain is a slot containing e.g. `-1`.

**Alternatives rejected**:
- *Untagged bare indices*: rejected by user feedback — a bare `3` is ambiguous against a cell value `3`.
- *Fixed-width zero-padded indices*: rejected — `col00`/`row00` is uglier than right-aligned `col0`/`row0`, and zero-padding could be misread. Right-align without zero-pad is cleaner.
- *1-based tagged indices*: rejected — would disagree with the 0-based `saolei_click(x, y)` arguments and reintroduce off-by-one confusion.

---

## D3 — Remain computation: where and how

**Decision**: The remain grid is computed in the **agent** saolei MCP (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`), inside the new `saolei_remain` tool handler. It is **not** a library function — the library recognizes boards; remain is a deduction view specific to this MCP tool.

Computation reuses the existing building blocks already in `saolei-mcp.ts`:
- `neighbors(state, x, y)` (`saolei-mcp.ts:253`) — the in-bounds Moore-neighborhood helper.
- The `CHORD_NUMBERS` set (`"1".."8"`, `saolei-mcp.ts:159`) — exactly the cells that get a remain value (a revealed `0` is excluded; see Clarification 2026-07-28).

Per cell `(x, y)`:
- If `state.grid[y][x]` ∈ `{"1".."8"}`: `remain = parseInt(cell) − count(neighbors(state,x,y) that are FLAG)`. Output `String(remain)` (may be `0` or negative — Clarification 2026-07-28).
- Else (`"0"`, `INITIAL`, `FLAG`, `HIT_MINE`, `MINE`, `UNKNOWN`): output `"-"`.

The grid is then rendered with the library's `renderGridWithRuler` (D1) so its ruler matches the board grid's exactly.

**Rationale**:
- Remain is a pure function of the recognized `GameState` — no I/O, no dispatch (spec FR-007). It belongs with the tool that exposes it.
- Reusing `neighbors` and the `1..8` set avoids a parallel neighborhood/number-set implementation and keeps the cell-status semantics identical to the validation layer (e.g. the same definition of "FLAG" neighbor used by the chord rule).

**Alternatives rejected**:
- *Add `computeRemain(state)` to the library*: rejected — remain is a deduction/view concern, not recognition. Keeping it in the library would blur the library's responsibility and couple a recognition library to a specific play-assistant view. The library's only new export is the generic `renderGridWithRuler` (a rendering primitive), which is appropriately library-level.

---

## D4 — `saolei_remain` body shape and lifecycle

**Decision**: `saolei_remain` follows the **same body contract** as the other saolei tools (refines 025 §3 / 027 §2), with one new outcome line and the remain grid in place of the symbol grid:

| Trigger | Body |
|---|---|
| recognized board exists | `saolei_remain → computed`<br>`game status: <won\|lost\|playing>`<br><br>`board size <w>*<h>`<br>`<remain grid with ruler>` |
| no recognized board | `rejected: no_active_game`<br><br>`call saolei_init first to start a game.` (NO status line, NO grid — identical to the cell tools' `no_active_game` body) |

Lifecycle rules (spec FR-007/FR-011/FR-012):
- **No dispatch**: the handler never calls `OperationBridge.dispatch` and never mutates `recognized`.
- **`no_active_game`**: when `recognized == null` (pre-init or state invalidated by a prior recognition failure), reuse the existing `rejectionText("no_active_game", null)` body.
- **Terminal states NOT blocked**: `saolei_remain` does **not** call `validateMove` (no target cell), so `won`/`lost` boards still return the remain grid; the `game status:` line reflects the terminal state. (The cell tools reject on terminal because they would dispatch a move; a pure query has no move to reject.)
- **Status semantics**: MCP-neutral `TOOL_RESULT_STATUS_UNSPECIFIED` (023 C15), like every saolei tool — the outcome is conveyed by the body text.

**Rationale**: Maximizes consistency with the existing four tools — one body grammar, one rejection vocabulary, one status-derivation path. The only novelty is the outcome line `saolei_remain → computed` (paralleling `<tool> at (x,y) → dispatched`) and the remain grid.

**Alternatives rejected**:
- *Block `saolei_remain` on terminal states (mirror the cell tools)*: rejected — a read-only query has no move to reject; showing the remain grid on a finished board is harmless and useful (the model can see the final deduction state).
- *Return both the symbol board and the remain grid in one body*: rejected — the user asked specifically for the remain values (spec US2). The model can call the operation tools for the symbol board. Mixing two grids in one body would muddy the ruler alignment and the body grammar.

---

## D5 — Consumer & test impact (verified)

**Consumers of `renderBoardText` (grep-verified, the complete set)**:
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts:441,458,480` — the three board-bearing body builders. The agent unit tests (`saolei-mcp.test.ts`) assert board content with `toContain` on substrings (`board size 2*2`, `* *`, `0 *`, `game status: …`, `valid range: …`). These substrings **survive** the ruler: `board size` is unchanged, and grid rows like `0 * *` still contain `* *`. No agent unit-test assertion is an exact full-body match.
- `projects/game/pkg/saolei-board/src/cli/cli.ts:110` — prints `renderBoardText`; auto gains the ruler, no code change.
- `projects/game/pkg/saolei-board/src/core/recognize.ts:240` — `SaoleiBoard.renderText()` delegates to `renderBoardText`; auto gains the ruler.
- **Desktop**: verified NO board-text parsing in `projects/game/desktop` (no `board size` / grid-splitting code). The chat bubble renders tool-result text verbatim (027). **No desktop change.**

**Test churn (library — exact-match assertions that the ruler changes)**:
- `golden.test.ts`: code unchanged; all **9** `testdata/*.golden.txt` are regenerated (ruler added to each) by re-running the CLI per the README calibration flow. Recognition itself is untouched — only the rendering format changes, so the regenerated goldens remain valid recognition references.
- `render.test.ts`: `lines[2]`/`lines[3]` exact assertions (and the `lines[3]`/`lines[4]` for the second row) updated to the ruled format. The `startsWith("board size 9*9\n\n")` assertion is preserved (ruler sits after the blank line).
- `recognize.test.ts`: two exact assertions (`renderText()` = `board size 2*1\n\n1 0` and `renderBoardText` = `board size 3*1\n\n* 0 2`) updated to the ruled format.

**Large tests (`projects/game/testplan/agent_saolei_test.go`)**: assertions are `strings.Contains` on `game status: …` substrings only — **ruler-invariant**, no breakage. A new E2E case is added for `saolei_remain` (init → `saolei_remain` returns the remain grid with **zero** dispatched operations); see [quickstart.md](quickstart.md).

---

## D6 — Tool-surface growth: five tools

**Decision**: The saolei MCP surface becomes **exactly five tools**: `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click` (the four operation tools — desktop-facing contract unchanged), plus the read-only `saolei_remain`. This **refines** (does not remove) the "exactly four tools" invariants in 023/025 — those specs counted the *operation* surface; `saolei_remain` is a non-dispatching query, not an operation.

**Rationale**: The skill (`projects/game/agent/src/skill/saolei/SKILL.md`) currently says "four tools". It is updated to "five tools" (four operations + one read-only query), documents the ruler, and describes `saolei_remain` (spec FR-014). The model-selection exclusion of raw mouse tools for saolei profiles (018/023 FR-012) is unchanged — `saolei_remain` is a saolei tool, not a mouse tool.

**Alternatives rejected**:
- *Fold remain into an existing tool (e.g. an extra field on every board result)*: rejected — the user explicitly asked for a separate `saolei_remain` tool (spec US2), and a separate query tool avoids bloating every operation result with a second grid the model may not always want.

---

## Open questions

None. All NEEDS CLARIFICATION items were resolved in `/speckit.clarify` (the `0`-cell and negative-remain decisions), and every deferred design question is settled above (D1–D6). The remaining work is implementation, captured in [contracts/](contracts) and to be decomposed in `tasks.md`.
