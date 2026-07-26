# Contract: Saolei MCP — text-board return & strict move validation

**Feature**: [spec.md](../spec.md) (FR-012..FR-018, FR-020..FR-022) | **Research**: [research.md](../research.md) D4/D5/D6 | **Recognition lib**: `projects/game/pkg/saolei-board/README.md`

This contract specifies (a) the saolei MCP tool **return shape** (text board, no image), (b) the per-session **recognized-state** management, and (c) the **strict** pre-dispatch **validation rules**.

## 1. Tool surface (unchanged)

Exactly four tools, unchanged from 023 (FR-020): `saolei_init`, `saolei_click(x,y)`, `saolei_flag(x,y)`, `saolei_chord_click(x,y)`. `x` = column, `y` = row, 0-based, top-left origin. Input schema (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:288-295`) keeps `x`/`y` as non-negative integers.

The desktop-facing operation contract is unchanged (FR-019): `saolei_init` → `KeyboardPressPart{F2}`; the cell tools → `MouseMoveAndClickPart` at `projects/game/agent/src/mcp/saolei/geometry.ts` `center(x,y)` with `WINDOW_MESSAGE` and the respective click action.

## 2. Recognized state (per session)

- A `SaoleiBoard` instance from `@dominion/game-saolei-board` is held in the per-session MCP server closure (the server is created per-session by `projects/game/agent/src/mcp-host.ts`).
- `saolei_init`: dispatch F2 → receive `FlowResultPart` → decode `screenshot.data` (base64 → `Buffer`) → `SaoleiBoard.init(buffer)`.
- Each legal cell op: dispatch → receive `FlowResultPart` → decode screenshot → `board.updateFromScreenshot(buffer)` (monotonic cross-screenshot validation per the lib README).
- Lifecycle: in-memory, co-located with the LangChain checkpoint; lost together on agent restart (spec Clarification Q1). No persistence, no reconnect recovery.

## 3. Return shape (text board, no image)

Every saolei tool returns MCP content blocks: **one text block** carrying `renderBoardText(state)` (the symbol legend from `projects/game/pkg/saolei-board/README.md`: `*` INITIAL, `0`–`8` revealed number, `F` FLAG, `X` HIT_MINE, `M` MINE, `?` UNKNOWN), preceded by a short outcome line. **No image block** is returned (FR-012/FR-022) — the screenshot is consumed for recognition only and stays in the control channel (`FlowResultPart`).

| Tool / outcome | Outcome line | Body |
|---|---|---|
| `saolei_init` success | `new game started` | initial text board |
| legal cell op success | `<tool> at (x,y) → dispatched` | updated text board |
| rejected move (illegal) | `rejected: <reason>` (FR-016) | current text board + valid coordinate range |
| recognition failure | `unable to recognize board` (FR-017) | (no board; guidance to `saolei_init`) |

Status semantics follow 023 C15 (MCP tools → neutral `TOOL_RESULT_STATUS_UNSPECIFIED`); the actual outcome is conveyed by the outcome line + text board. (A rejected move is reported as a normal tool result with the rejection text, so the model can choose a different move — it is not a tool error that aborts the turn.)

## 4. Validation rules (strict)

Validation runs **before** dispatch, against the latest recognized state (FR-014/FR-015). It judges **target-cell compatibility**, not predicted outcome. Cell symbols: `*` INITIAL, `0`–`8` revealed, `F` FLAG, `?` UNKNOWN.

| Op | Target cell | Verdict | Reason code |
|---|---|---|---|
| `saolei_click` | `*` | dispatch | `ok` |
| `saolei_click` | `0`–`8` | **reject** | `cell_already_revealed` |
| `saolei_click` | `F` | **reject** | `cell_is_flagged` |
| `saolei_flag` | `*` | dispatch | `ok` (place flag) |
| `saolei_flag` | `F` | dispatch | `ok` (toggle/unflag) |
| `saolei_flag` | `0`–`8` | **reject** | `cannot_flag_revealed` |
| `saolei_chord_click` | `1`–`8` | dispatch | `ok` (legal chord; may reveal nothing if flag-count ≠ number — still legal, **not** rejected) |
| `saolei_chord_click` | `0` / `*` / `F` | **reject** | `chord_requires_number` |
| any cell op | `?` (UNKNOWN target) | dispatch (lenient) | FR-018 — never reject solely on uncertainty |
| any cell op | no active game (pre-`init`, or state invalid) | **reject** | `no_active_game` (FR-015a) |
| any cell op | `(x,y)` outside recognized dimensions | **reject** | `out_of_bounds` + valid range (FR-015b) |
| any cell op | terminal state (won/lost recognized) | **reject** | `game_over` (FR-015f) |

A rejected move returns the reason code, the current text board, and the valid coordinate range; it does **not** dispatch and the desktop receives no operation (FR-014).

### Recognition-failure handling (FR-017)

If `init`/`updateFromScreenshot` throws (`BoardStateIncompatibleError` / `BoardDimensionMismatchError`) or the screenshot is not recognizable as a saolei board:
- Return `unable to recognize board`.
- Mark the session state invalid.
- Subsequent cell ops are rejected with `no_active_game` until a `saolei_init` re-seeds the state.

## 5. Coordinate-space discipline (do not mix)

- **Recognition** (`saolei-board`): screenshot space — `originYPx = 200` (includes non-client chrome). The captured screenshot is a full-window capture, so screenshot-space is correct for reading pixels.
- **Clicks** (`geometry.ts`): client space — `BOARD_ORIGIN_Y_PX = 104` (Y minus 96 px chrome offset) for `WM_*` `lParam`.

Both use `originX = 24`, `cell = 32`. They must not be mixed (per `projects/game/pkg/saolei-board/README.md` → "坐标空间注意"). This feature does not change either geometry.

## 6. Skill update (FR-021)

`projects/game/agent/src/skill/saolei/SKILL.md` is updated to: describe the four tools; state that results return a **text** board (with the symbol legend); state that illegal moves are **rejected with a reason** (and list the rule categories); and remove any guidance saying the model should read a returned screenshot.

## 7. Test anchors

- Unit (agent): `saolei_init` → returns text board (no image) and seeds state; a legal `saolei_click` dispatches and returns updated text board; an illegal move (e.g. click on a revealed cell, out-of-bounds, pre-init, post-terminal) is rejected **without** dispatch, with the right reason code.
- Unit (agent): recognition failure → `unable to recognize board`, state invalid, subsequent ops rejected.
- Golden: recognition correctness is covered by `projects/game/pkg/saolei-board`'s own golden tests (`src/core/golden.test.ts`); the MCP layer relies on the lib's contract, not its internals.
- Large test: end-to-end saolei turn returns text boards and rejects an illegal move ([quickstart.md](../quickstart.md)).
