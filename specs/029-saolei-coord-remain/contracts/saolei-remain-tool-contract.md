# Contract: `saolei_remain` MCP tool

**Feature**: `029-saolei-coord-remain` | **Date**: 2026-07-28 | **Spec**: [spec.md](../spec.md) | **Research**: [research.md](../research.md) D3/D4/D6

This contract specifies the new **read-only** `saolei_remain` saolei MCP tool. It **refines** (does not replace) [025 — saolei-mcp-contract.md](../../025-desktop-image-state-refine/contracts/saolei-mcp-contract.md) and [027 — saolei-mcp-status-contract.md](../../027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md): the four operation tools, their desktop-facing Parts, the recognized-state lifecycle, the single-text-block return, the status-line derivation, and the `MoveRejection` vocabulary are all unchanged. `saolei_remain` is added as a fifth, non-dispatching tool.

The coordinate ruler shared with the board grid is specified in [saolei-board-render-contract.md](saolei-board-render-contract.md).

---

## §1 — Tool surface

The saolei MCP exposes **exactly five tools** after this feature:

| Tool | Args | Dispatches? | Returns |
|---|---|---|---|
| `saolei_init` | none | yes — `KeyboardPressPart{F2}` | ruled text board |
| `saolei_click(x, y)` | `x`, `y` non-negative ints | yes — `MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE}` | ruled text board |
| `saolei_flag(x, y)` | `x`, `y` non-negative ints | yes — `MouseMoveAndClickPart{RIGHT_CLICK, WINDOW_MESSAGE}` | ruled text board |
| `saolei_chord_click(x, y)` | `x`, `y` non-negative ints | yes — `MouseMoveAndClickPart{LEFT_RIGHT_PRESS, WINDOW_MESSAGE}` | ruled text board |
| **`saolei_remain`** | **none** | **no** | **ruled remain grid** |

The four operation tools are unchanged (their desktop-facing contract per 024/025; their validation per 025/027). `saolei_remain` is the only addition.

The `"exactly four tools"` invariants in 023/025 counted the **operation** surface; this feature refines that to "exactly four operation tools **plus one read-only query tool**". The raw-mouse-tool exclusion for saolei profiles (018/023 FR-012) is unchanged — `saolei_remain` is a saolei tool, not a mouse tool.

---

## §2 — `saolei_remain` input & dispatch

- **Input schema**: none. Registered with no `inputSchema` (like `saolei_init`).
- **Dispatch**: the handler MUST NOT call `OperationBridge.dispatch` and MUST NOT mutate the session's `recognized` state. It is purely computational (spec FR-007). `saolei_remain` is therefore callable back-to-back with any other tool, any number of times, with no side effects.

---

## §3 — Remain computation

For a recognized `GameState` (`recognized: GameState`), the remain token at each cell `(x, y)` is:

```
cell = state.grid[y][x]
if cell ∈ {"1","2","3","4","5","6","7","8"}:
    remain = parseInt(cell) − (count of in-bounds Moore neighbors of (x,y) whose status is "FLAG")
    token  = String(remain)        // may be "0" or negative e.g. "-1"; NOT clamped
else:
    token = "-"
```

- "Moore neighbors" uses the existing `neighbors(state, x, y)` helper (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:253`); edge/corner cells yield the in-bounds subset (5 / 3).
- The number-cell set is exactly the existing `CHORD_NUMBERS` (`"1".."8"`). A revealed `"0"` is excluded → `"-"` (Clarification 2026-07-28: a blank has no adjacent mines and carries no deduction info).
- The value is the **raw** `number − adjacent flags`. It may be `0` (fully satisfied) or **negative** (over-flagged); it is not clamped (Clarification 2026-07-28: a negative value exposes the over-flag error; a clamped `0` would be indistinguishable from a satisfied cell). The lone `-` non-number marker (no digit) is visually distinct from a negative number like `-1`.

The remain grid is rendered with the library's `renderGridWithRuler(state.width, state.height, tokenAt)` ([saolei-board-render-contract.md](saolei-board-render-contract.md) §2), so its ruler format is identical to the board grid's (spec FR-010 / SC-004). The agent prepends `board size <w>*<h>\n\n` to match the board body's header.

---

## §4 — Return shape (single text block)

`saolei_remain` returns one MCP **text** content block (`{ type: "text", text: <body> }`). Status semantics are MCP-neutral (`TOOL_RESULT_STATUS_UNSPECIFIED`, 023 C15) — the outcome is conveyed by the body text, exactly like a rejection.

| Trigger | Outcome line | Body |
|---|---|---|
| `recognized != null` | `saolei_remain → computed` | `saolei_remain → computed`<br>`game status: <won\|lost\|playing>`<br><br>`board size <w>*<h>`<br>`<ruled remain grid>` |
| `recognized == null` (pre-init, or state invalidated by a prior recognition failure) | `rejected: no_active_game` | `rejected: no_active_game`<br><br>`call saolei_init first to start a game.` (NO status line, NO grid) |

`<status>` is derived by the existing `gameStatus(state)` (027 §3: loss-first via `isTerminalState`, then `isWin`, else `playing`). The `no_active_game` body is byte-identical to the cell tools' `rejectionText("no_active_game", null)` (`saolei-mcp.ts:474`) — reuse that builder.

### Worked example

Board (3×3):
```
board size 3*3

       col0 col1 col2
row0      1    F    *
row1      2    3    *
row2      *    *    *
```
`saolei_remain` on this board (cell (0,0)=`1` has one adjacent `F` → remain 0; (0,1)=`2` has one adjacent `F` → remain 1; (1,1)=`3` has one adjacent `F` → remain 2):
```
saolei_remain → computed
game status: playing

board size 3*3

       col0 col1 col2
row0      0    -    -
row1      1    2    -
row2      -    -    -
```

---

## §5 — Lifecycle & terminal states (spec FR-011 / FR-012)

- **`no_active_game`**: `saolei_remain` reads `recognized`; when it is `null` it returns the `no_active_game` rejection body (§4). This is the **only** rejection path — `saolei_remain` performs no `validateMove` (no target cell), so no other `MoveRejection` reason applies.
- **Terminal `won` / `lost`**: `saolei_remain` is **not** blocked. Because it performs no move, the terminal-state guards in `validateMove` (`game_over` / `game_won`) do not apply; the handler returns the remain grid and the body's `game status:` line reflects `won`/`lost`. (A terminal board's `HIT_MINE`/`MINE` cells are non-numbers → `-`; its number cells still carry their remain value.)
- **Recognition failure mid-game**: handled by the existing `recognized = null` invalidation — the next `saolei_remain` returns `no_active_game` until `saolei_init` re-seeds (same as the cell tools, 025 §4).

---

## §6 — Skill update (spec FR-014)

`projects/game/agent/src/skill/saolei/SKILL.md` is updated to:
- State that every text board now carries a column-index header row and a row-index prefix (0-based, top-left origin), with the worked example updated to the ruled format.
- Describe `saolei_remain`: it takes no arguments, dispatches nothing, and returns the remain grid (number cell → `number − adjacent flags`, possibly `0` or negative; non-number → `-`); it is a read-only deduction aid.
- Update the "Tools" list and any "four tools" / tool-count wording to five tools (four operations + `saolei_remain`).
- Leave the coordinate convention, the symbol legend, the move-validation rules, and the `saolei_init`-first lifecycle guidance unchanged.

---

## §7 — What is NOT changed

- The four operation tools (`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_chord_click`) — surface, input schema, validation, desktop-facing Parts — unchanged (they merely return the now-ruled board via `renderBoardText`).
- The recognized-state lifecycle (025 §2; in-memory, per-session, lost on agent restart) — unchanged; `saolei_remain` only reads it.
- The `gameStatus` derivation (027 §3) — reused as-is.
- The `MoveRejection` vocabulary (027 §4) — no new reason codes; `saolei_remain` reuses only `no_active_game`.
- MCP status semantics (023 C15 neutral) — `saolei_remain` is neutral like the other saolei tools.

---

## §8 — Test anchors

- **Unit (agent, `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`)**:
  - `saolei_remain` listed among the five tools; `saolei_update` still absent.
  - On a recognized board with a `3` cell + one adjacent `F` → the body contains `saolei_remain → computed`, `game status: playing`, `board size …`, and a remain grid showing `2` at that cell and `-` elsewhere; the body's grid carries the tagged column header (`col0`…) + tagged row prefix (`row0`…).
  - `saolei_remain` dispatches nothing (fake bridge records zero operations across the call) and does not change the board returned by a subsequent `saolei_click`.
  - `recognized == null` → `rejected: no_active_game` body, no `game status:` line, no grid.
  - Terminal `won`/`lost` board → `saolei_remain` still returns the grid with the corresponding `game status:` line (not rejected).
  - Over-flagged `1` cell with two adjacent `F` → remain token `-1` (raw, not clamped).
- **Unit (library, `render.test.ts`)**: `renderGridWithRuler` produces the header + prefixed rows with correct `columnWidth` for both a ≤9-wide grid (columnWidth 4) and a ≥10-wide grid (columnWidth 5); `renderBoardText` prepends the `board size` header + blank.
- **Large (agent service, `projects/game/testplan/agent_saolei_test.go`)**: end-to-end `saolei_init` → `saolei_remain` returns a text result containing `saolei_remain → computed`, `game status: playing`, and `board size`, with **zero** `FlowPart` operations dispatched for the `saolei_remain` call — see [quickstart.md](../quickstart.md).
