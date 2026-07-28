# Feature Specification: Saolei Board Coordinate Ruler & Remain Tool

**Feature Branch**: `029-saolei-coord-remain`

**Created**: 2026-07-28

**Status**: Draft

**Input**: User description: "为 @projects/game/agent/ saolei mcp 1. 工具输出游戏状态时，在左侧和顶部增加行号和列号。2. 增加 saolei_remain 工具，返回每个数字格周围剩余未标记的地雷数量，非数字格使用 '-' 作为输出。"

## Clarifications

### Session 2026-07-28

- Q: Should a revealed `0` (blank) cell output `-` or `0` in the remain grid? → A: `-` (Option A — a `0`/blank has no adjacent mines, carries no deduction information, and is treated as a non-number for the remain view, mirroring the existing `CHORD_NUMBERS` `1`–`8` precedent).
- Q: When adjacent flags exceed a number cell's value (over-flagged), should the remain value be the raw negative number or clamped to 0? → A: Raw negative (Option A — exposes the over-flag error to the model; a clamped `0` would be indistinguishable from a correctly-satisfied cell).
- Q (user feedback 2026-07-28): Should the ruler indices carry `col`/`row` tags to avoid confusion with the 0–8 game-state cell values? → A: Yes — every column index is tagged `col<N>` and every row index `row<N>` (e.g. `col3`, `row1`). Indices stay **0-based** so they remain consistent with the `saolei_click(x, y)` tool arguments (a 1-based display would introduce the very off-by-one confusion this is meant to avoid). The example `row1`/`col3` thus denote 0-based index 1 and 3.

## User Scenarios & Testing *(mandatory)*

This feature refines the saolei MCP text-board output ([025 — Desktop Image State Refine](../025-desktop-image-state-refine/spec.md), [027 — Chat Bubble Game State](../027-chat-bubble-game-state/spec.md)) and the shared board-text renderer (`projects/game/pkg/saolei-board/src/core/render.ts` `renderBoardText`). It adds two independent, user-facing capabilities to the saolei MCP that the model uses to play Minesweeper: (1) a coordinate ruler on every text board, and (2) a new read-only `saolei_remain` query tool.

### User Story 1 - Text Board Gains a Coordinate Ruler (Priority: P1)

Every saolei MCP tool result that includes a text board — `saolei_init` success, a legal cell-op success, a rejection that carries the current board — now renders that board with a **column-index header row** across the top and a **row-index prefix** on the left of each row, using the same 0-based top-left-origin `(x, y)` convention the tools already use (`x` = column, `y` = row). Each index is **tagged** — columns as `col<N>` and rows as `row<N>` (e.g. `col3`, `row1`) — so the ruler cannot be confused with the `0`–`8` game-state cell values. This lets the model (and a human reading the tool bubble) locate any cell by reading its tagged indices directly off the board, instead of counting symbols from the top-left corner. The `board size <w>*<h>` header and the symbol legend (`*`, `0`–`8`, `F`, `X`, `M`, `?`) are unchanged; the ruler is added around the existing grid.

**Why this priority**: Coordinate disambiguation is the foundation of correct cell operations. Today the model must count cells to map a symbol back to an `(x, y)`; a wrong count produces an `out_of_bounds` or wrong-cell operation. The ruler removes that counting step for every board-bearing result, so it is the highest-leverage change.

**Independent Test**: Configure a saolei profile, call `saolei_init`, and confirm the returned text board shows a column-index header line above the grid and a row-index prefix on each row, with the `board size` header still present. This delivers value on its own (clearer boards) with no dependency on the new tool.

**Acceptance Scenarios**:

1. **Given** a saolei-enabled session with a recognized 9×9 board, **When** `saolei_init` succeeds, **Then** the text-board body shows a `board size 9*9` header, a blank line, a column-index header line listing `col0` through `col8`, and each grid row prefixed with its 0-based row label `row0`..`row8` — so the symbol at column `4`, row `2` sits under header `col4` and beside prefix `row2`.
2. **Given** a saolei session with a recognized board, **When** a legal `saolei_click(x, y)` dispatches, **Then** the updated text board carries the same column header and row prefix.
3. **Given** a saolei session, **When** a cell op is rejected with a reason that includes the current board (e.g. `cell_already_revealed`), **Then** the board in the rejection body also carries the column header and row prefix, and the existing `valid range:` line is unchanged.
4. **Given** a saolei session with no active game, **When** a cell op is rejected as `no_active_game` (no recognized state), **Then** the rejection body is unchanged (guidance to call `saolei_init` first; no board, hence no ruler) — the ruler only appears when a board is rendered.

---

### User Story 2 - New `saolei_remain` Query Tool (Priority: P1)

The saolei MCP exposes a new **read-only** tool `saolei_remain()` that takes no arguments and does **not** dispatch any operation to the desktop. It reads the latest recognized board and returns, for every cell, the **remaining unmarked mine count** — i.e. for a revealed number cell, the count of mines still not flagged among its neighbors; for any non-number cell, a single `-`. This gives the model a ready-made deduction view (a "how many mines are left around each number" grid) in one call, instead of forcing it to scan the symbol grid, identify each number, and count adjacent flags by hand. The tool shares the recognized-state lifecycle of the other tools: it rejects with `no_active_game` when no board is recognized, but otherwise returns the remain grid regardless of whether the game is playing/won/lost (it is a pure query).

**Why this priority**: Counting remaining mines per number is the core arithmetic of Minesweeper deduction. Automating it removes a frequent source of model error (miscounted neighbors / flags) and is requested directly by the user.

**Independent Test**: Configure a saolei profile, play to a board with at least one revealed number and one adjacent flag, call `saolei_remain`, and confirm the returned grid shows the number cell's value-minus-adjacent-flags and `-` for non-number cells — with no desktop dispatch occurring.

**Acceptance Scenarios**:

1. **Given** a saolei session with a recognized board containing a revealed `3` cell that has exactly one adjacent `F` (flag), **When** the model calls `saolei_remain()`, **Then** the result is a single text block whose grid shows `2` at that cell's position (3 − 1) and `-` at every non-number cell position.
2. **Given** a saolei session with a recognized board, **When** `saolei_remain()` is called, **Then** no operation is dispatched to the desktop (the tool performs no `OperationBridge.dispatch`) and the recognized board state is not mutated.
3. **Given** a saolei session with no recognized board (pre-`saolei_init`, or state invalidated by a prior recognition failure), **When** `saolei_remain()` is called, **Then** it returns `rejected: no_active_game` with the same guidance body as the cell tools (call `saolei_init` first) and renders no grid.
4. **Given** a saolei session whose recognized board is in a terminal state (`won` or `lost`), **When** `saolei_remain()` is called, **Then** it still returns the remain grid (read-only; terminal states do not block a pure query) and the body carries the corresponding `game status: won|lost` line.
5. **Given** a saolei session, **When** the model lists the MCP tools, **Then** `saolei_remain` is present alongside `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`.

---

### Edge Cases

- **Boards wider or taller than 10 cells**: column/row indices become multi-digit (e.g. a 30-wide expert board has column indices `0`–`29`). The ruler lists the indices in order; for such boards the header/prefix tokens are wider than a single cell symbol, so visual column alignment is best-effort rather than pixel-perfect. The indices remain correct and unambiguous. (The common classic board is 9×9, where single-digit indices align cleanly with single-char symbols.) The precise alignment algorithm (right-align every slot to a computed `columnWidth`) is defined in [contracts/saolei-board-render-contract.md](contracts/saolei-board-render-contract.md) §2 — "best-effort" here refers only to cross-grid visual alignment between the board grid and remain grid, not to internal grid alignment which is exact.
- **Fresh / all-`*` board**: `saolei_remain` returns a grid that is entirely `-` (no revealed numbers yet) — valid, not an error.
- **Over-flagged number cell** (a number surrounded by more flags than its value): the remain value is negative (e.g. a `1` cell with two adjacent flags → `-1`), surfaced as-is to signal the over-flag error to the model. The authoritative computation is in [contracts/saolei-remain-tool-contract.md](contracts/saolei-remain-tool-contract.md) §3 — this and FR-013 both follow that definition.
- **Fully-satisfied number cell** (adjacent flags equal the number): the remain value is `0` (a `0` is a legitimate remain value for a number cell, distinct from the non-number marker `-`).
- **`0` (blank) cells**: a `0` cell has no adjacent mines by definition; `saolei_remain` outputs `-` for it (the remain concept does not carry mine-deduction information for a blank).
- **`?` (UNKNOWN) cells**: output `-` in the remain grid (uncertain recognition is not a number).
- **`saolei_remain` before `saolei_init`**: rejected as `no_active_game`.
- **Recognition failure mid-game**: as with the cell tools, a `saolei_remain` call after the state is invalidated returns `no_active_game` until `saolei_init` re-seeds.

## Requirements *(mandatory)*

### Functional Requirements

#### Text-board coordinate ruler (User Story 1)

- **FR-001**: Every saolei MCP tool result body that renders the recognized board (init success, legal cell-op success, and any rejection body that includes the current board) MUST render the board with a **column-index header row** placed immediately above the first grid row and a **row-index prefix** at the start of every grid row.
- **FR-002**: The ruler indices MUST be **0-based** and follow the existing top-left-origin convention: column indices run `0..width-1` left→right; row indices run `0..height-1` top→bottom. They MUST be consistent with the `(x, y)` arguments accepted by `saolei_click`/`saolei_flag`/`saolei_chord_click`. Each column index MUST be tagged `col<N>` and each row index `row<N>` (e.g. `col3`, `row1`) so the ruler is unambiguous against the `0`–`8` game-state cell values.
- **FR-003**: The `board size <w>*<h>` header line and the blank-line separator MUST be retained. The symbol legend (`*`, `0`–`8`, `F`, `X`, `M`, `?`) and the one-symbol-per-cell, space-separated row format MUST be unchanged.
- **FR-004**: The ruler MUST NOT appear when no board is rendered (the `no_active_game` rejection with no state, and the `unable to recognize board` body) — those bodies are unchanged.
- **FR-005**: The `valid range: x 0..<w-1>, y 0..<h-1>` line on rejection bodies MUST be retained verbatim.

#### `saolei_remain` tool (User Story 2)

- **FR-006**: The saolei MCP MUST expose a new tool `saolei_remain` that takes **no arguments**.
- **FR-007**: `saolei_remain` MUST be **read-only**: it MUST NOT dispatch any operation to the desktop (no `OperationBridge.dispatch`) and MUST NOT mutate the recognized board state.
- **FR-008**: For a recognized board, `saolei_remain` MUST return a single text content block whose body is: an outcome line, a `game status: <won|lost|playing>` line (derived identically to the other tools), a blank line, and a **remain grid** of the same dimensions as the board.
- **FR-009**: The remain grid MUST be computed cell-by-cell: for each revealed number cell (`1`–`8`), the value is `<number> − <count of adjacent FLAG cells>` (Moore neighborhood, in-bounds only); for every other cell (`0`, `INITIAL`, `FLAG`, `HIT_MINE`, `MINE`, `UNKNOWN`), the value is the literal string `-`.
- **FR-010**: The remain grid MUST carry the same coordinate ruler as the board grid (FR-001/FR-002): a column-index header row and a row-index prefix per row, plus the `board size <w>*<h>` header.
- **FR-011**: `saolei_remain` MUST reject with `no_active_game` (same body shape as the cell tools — guidance to call `saolei_init` first, no grid) when no recognized board state exists.
- **FR-012**: `saolei_remain` MUST NOT be blocked by terminal game states: it returns the remain grid for `won` and `lost` boards as well as `playing` (it performs no move and validates no target cell).
- **FR-013**: The remain value for an over-flagged number cell (adjacent flags exceed the number) MUST be the true arithmetic result (a negative integer), not clamped — so the model can detect the over-flag error.
- **FR-014**: The built-in saolei skill (`projects/game/agent/src/skill/saolei/SKILL.md`) MUST be updated to document the ruler in the board output and to describe `saolei_remain` (its purpose, that it is read-only/non-dispatching, the number-cell-vs-`-` rule, and the negative-over-flag signal).

### Key Entities *(include if feature involves data)*

- **Text board (with ruler)**: The recognized `GameState` rendered as text. Unchanged in meaning (`board size` header + symbol grid per the existing legend); gains a column-index header row and a row-index prefix column. Single source of truth is the shared board-text renderer consumed by the MCP tools and the `saolei-recognize` CLI.
- **Remain grid**: A per-cell view, isomorphic to the board grid (same `width`/`height`), where each number cell (`1`–`8`) carries its remaining-unmarked-mine count (`number − adjacent flags`, possibly `0` or negative) and every other cell carries `-`. Derived purely from the latest recognized `GameState`; never persisted, never dispatched.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All board rendering code paths produce the ruler: every tool body that renders a recognized board (via `renderBoardText`) carries a column-index header row (each index tagged `col<N>`) and a per-row row-index prefix (each tagged `row<N>`), using the 0-based `(x, y)` convention. This is verified by unit tests covering every `renderBoardText` call site in the MCP tool bodies, golden-fixture exact-match assertions, and large-test E2E output checks — any cell can be located by reading its tagged column and row labels without counting symbols and without confusing them with the `0`–`8` cell values.
- **SC-002**: In 100% of cases, the remain grid returned by `saolei_remain` has the same dimensions as the recognized board, and each `1`–`8` cell's value equals its number minus its adjacent-flag count (verifiable by an independent count over the same board).
- **SC-003**: `saolei_remain` performs zero desktop dispatches in 100% of calls (it is purely computational), and never alters the recognized board state observed by a subsequent tool call.
- **SC-004**: The coordinate indices on the board grid and on the remain grid are mutually consistent in 100% of cases (the same `(x, y)` refers to the same physical cell in both grids and in the click/flag/chord arguments).

## Assumptions

- **Ruler rendering location**: The board text (and therefore the ruler) is produced by the shared `renderBoardText` renderer in `@dominion/game-saolei-board` (`projects/game/pkg/saolei-board/src/core/render.ts`), consumed by both the saolei MCP tools and the `saolei-recognize` CLI. Adding the ruler at that single source of truth is assumed (Constitution §II — refactor over patch); the exact placement is a plan-time decision. This means the CLI output and the library's golden text fixtures (`testdata/*.golden.txt`) will also gain the ruler and be regenerated — a known, intended side effect, not a separate feature.
- **`0` cells in the remain grid** (confirmed — Clarification 2026-07-28): A revealed `0` (blank) has no adjacent mines, so its "remaining mines" is trivially `0` and carries no mine-deduction information. `saolei_remain` outputs `-` for `0` cells (treating `0` like a non-number for the remain view). This mirrors the existing `CHORD_NUMBERS` (`1`–`8`) precedent in `saolei-mcp.ts`, where `0` is excluded from the actionable-number set.
- **Over-flag (negative remain)** (confirmed — Clarification 2026-07-28): Per [contracts/saolei-remain-tool-contract.md](contracts/saolei-remain-tool-contract.md) §3: the remain value is the raw `number − adjacent flags`, surfaced as-is (e.g. `-1`, not clamped), so the model can detect the error. The non-number marker is a lone `-` (no digit), distinguishable from a negative remain token like `-1`.
- **Ruler alignment on large boards**: Indices are tagged (`col<N>`/`row<N>`), so every ruler slot is at least 4 characters wide; the renderer right-aligns all slots (indices and cell tokens) to a common column width derived from the widest slot, keeping the grid aligned for all board sizes (9×9 and 16×16 both in the golden set). Visual alignment is best-effort for variable-width remain tokens; the positional/semantic mapping (the Nth token = column N) stays exact.
- **Ruler index base** (confirmed — Clarification 2026-07-28): The tagged indices stay **0-based** (`col0`/`row0` = first column/row) to remain consistent with the 0-based `saolei_click(x, y)` arguments; a 1-based display is rejected because it would introduce the off-by-one confusion the tags are meant to eliminate. The user's example `row1`/`col3` denotes 0-based index 1 and 3.
- **Shared coordinate convention**: The ruler reuses the existing top-left-origin 0-based `(x, y)` grid already used by the cell tools and documented in the skill; no coordinate-system change.
- **Tool surface growth**: `saolei_remain` is added to the existing four tools (the surface becomes five). The prior "exactly four tools" invariants in earlier specs (023/025) are refined by this feature to "exactly five tools" — the four operation tools plus the read-only `saolei_remain`. The desktop-facing operation contract of the four operation tools is unchanged.
- **Status semantics**: `saolei_remain` follows the same neutral MCP status as the other saolei tools (`TOOL_RESULT_STATUS_UNSPECIFIED`, 023 C15); its outcome is conveyed by the body text, like a rejection.
