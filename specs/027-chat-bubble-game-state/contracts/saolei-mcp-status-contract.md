# Contract: saolei MCP game-status output & validation rules

**Feature**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](../spec.md)

This contract fixes the agent-side interface changes for US3/US4/US5: the saolei-board **win predicate** signature (US3), the saolei MCP **text-result body format** with the new `game status:` line (US4), the **terminal-state** rules (loss `game_over` + win `game_won`, US4/FR-021..023), and the **chord-neighbor** validation rule (US5/FR-016..020). It is the authoritative interface description; the data shapes are in [data-model.md](../data-model.md); the decisions in [research.md](../research.md) D6..D9.

It **refines** (does not replace) [specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md](../../025-desktop-image-state-refine/contracts/saolei-mcp-contract.md): the four-tool surface, the desktop-facing operation Parts, the recognized-state lifecycle, the single-text-block return, and the existing rejection reasons are all unchanged. Two new rejection reasons and one new status line are added; one new library export is added.

---

## §1 — Library win predicate (US3)

**Signature** (`projects/game/pkg/saolei-board/src/core/win.ts`, exported via `src/core/index.ts`):

```ts
export function isWin(state: GameState): boolean;
```

| Postcondition | Holds |
|---|---|
| Returns `true` | iff **no** cell of `state.grid` is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` (every cell is a revealed number `"0"`..`"8"` or `FLAG`). |
| Returns `false` | if any cell is `INITIAL` (in progress), `HIT_MINE`/`MINE` (a loss), or `UNKNOWN` (lenient — recognition uncertain). |
| Purity | no I/O, no mutation, no side effects; a pure function of `GameState`. |
| Complexity | O(width × height), short-circuits on the first disqualifying cell. |

**Caller**: the saolei MCP (`gameStatus`, `validateMove`) imports `isWin` from `@dominion/game-saolei-board`. No other library export changes.

**Authoritative win fixture**: `testdata/saolei_10.png` (real 9×9 win board). The library's golden test asserts `recognizeBoard(saolei_10.png)` matches `saolei_10.golden.txt` AND `isWin(that state) === true`.

---

## §2 — Text-result body format (US4 / FR-012..015)

Every saolei tool result is a single MCP **text** content block (`{ type: "text", text: <body> }`) — 025 FR-012 preserved. The body gains one new line, `game status: <won|lost|playing>`, positioned **immediately after the outcome/rejection line and immediately before the text board**. The existing outcome line and the text-board body are unchanged.

### Body shapes (status line in **bold**)

| Builder | Trigger | Body |
|---|---|---|
| `initSuccessText` | `saolei_init` succeeded, state recognized | `new game started`<br>**`game status: <status>`**<br><br>`<renderBoardText(state)>` |
| `dispatchedText` | a cell op dispatched, state recognized | `<tool> at (<x>,<y>) → dispatched`<br>**`game status: <status>`**<br><br>`<renderBoardText(state)>` |
| `rejectionText` (has state) | illegal move, recognized state exists | `rejected: <reason>`<br>**`game status: <status>`**<br><br>`<renderBoardText(state)>`<br>`valid range: x 0..<w-1>, y 0..<h-1>` |
| `rejectionText` (no state) | `no_active_game` (pre-init / state invalidated) | `rejected: no_active_game`<br><br>`call saolei_init first to start a game.` *(NO status line — no state to derive from)* |
| `unrecognizableText` | recognition failed (no/invalid screenshot) | `unable to recognize board`<br><br>`call saolei_init to start a new game.` *(NO status line — state invalidated)* |

`<status>` is derived by `gameStatus(state)` (§3). `<reason>` is a `MoveRejection` value (§4).

### Invariants

- The status line is present **iff** a recognized state exists at the time the body is built. It is omitted for `no_active_game` and `unable to recognize board` (FR-015 — no fabricated status).
- The status line is part of the SAME text content block as the outcome line and the board (FR-014). It is NEVER a separate content block, NEVER an image block.
- The text board (`renderBoardText`) and the `valid range:` line (rejections with state) are unchanged from 025.

### Worked examples

```
# init on an in-progress board
new game started
game status: playing

board size 9*9
... (grid) ...

# a click that WINS the game on this operation
saolei_click at (4,4) → dispatched
game status: won

board size 9*9
... (grid — all revealed/flagged) ...

# a click that LOSES the game on this operation
saolei_click at (4,4) → dispatched
game status: lost

board size 9*9
... (grid — HIT_MINE present) ...

# a rejection on an in-progress board
rejected: cell_already_revealed
game status: playing

board size 9*9
... (grid) ...
valid range: x 0..8, y 0..8

# a rejection with no recognized state (no status line)
rejected: no_active_game

call saolei_init first to start a game.
```

---

## §3 — Game-status derivation (US4 / FR-012..013)

**Signature** (agent-internal, `saolei-mcp.ts`):

```ts
function gameStatus(state: GameState): "won" | "lost" | "playing";
```

| Condition | Returns |
|---|---|
| `isTerminalState(state)` is `true` (loss: any `HIT_MINE`/`MINE`) | `"lost"` |
| else `isWin(state)` is `true` (win: no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`) | `"won"` |
| otherwise | `"playing"` |

**Precedence**: loss is checked before win. They are mutually exclusive (a loss board has `HIT_MINE`/`MINE` ⇒ `isWin` returns `false`), so exactly one applies; loss-first makes the "loss takes precedence" rule explicit.

---

## §4 — `MoveRejection` union (US4/US5 / FR-019, FR-023)

The union (`saolei-mcp.ts`) gains two members. Full value set after this feature:

```
no_active_game | out_of_bounds | game_over | game_won
| cell_already_revealed | cell_is_flagged | cannot_flag_revealed
| chord_requires_number | chord_no_unrevealed_neighbor
```

(`game_over` and `chord_requires_number` are existing; `game_won` and `chord_no_unrevealed_neighbor` are new.) Each is surfaced verbatim in the `rejected: <reason>` outcome line and documented in the skill (`SKILL.md`, FR-024).

---

## §5 — Validation rule order (US4/US5 / FR-016..023)

`validateMove(state, tool, x, y)` checks in this order; the first failing rule wins:

| # | Rule | Reason | Scope | Status |
|---|---|---|---|---|
| 1 | `x`/`y` outside `state.width`/`state.height` | `out_of_bounds` | all cell tools | existing |
| 2 | `isTerminalState(state)` (loss: `HIT_MINE`/`MINE` present) | `game_over` | all cell tools | existing |
| 3 | `isWin(state)` (win) | `game_won` | all cell tools | **NEW (FR-021..023)** |
| 4a | `saolei_click` target is a revealed number | `cell_already_revealed` | click | existing |
| 4b | `saolei_click` target is `FLAG` | `cell_is_flagged` | click | existing |
| 4c | `saolei_flag` target is a revealed number | `cannot_flag_revealed` | flag | existing |
| 4d | `saolei_chord_click` target is not `1..8` | `chord_requires_number` | chord | existing |
| 4e | `saolei_chord_click`: no in-bounds neighbor is `INITIAL` or `UNKNOWN` | `chord_no_unrevealed_neighbor` | chord | **NEW (FR-016..020)** |
| 5 | (none of the above) | `{ ok: true }` (legal) | — | existing |

**New-rule placement invariants** (FR-018):

- Rule 3 (`game_won`) is placed **immediately after** rule 2 (`game_over`) — both are state-level terminal checks; loss-first (§3 precedence).
- Rule 4e (`chord_no_unrevealed_neighbor`) is placed **immediately after** rule 4d (`chord_requires_number`) — both are chord-specific; a chord on a non-number still reports `chord_requires_number`, not the neighbor reason.
- A move is NEVER rejected solely because its target cell is `UNKNOWN` (025 FR-018 retained). The new chord rule honours this: an `UNKNOWN` neighbor counts as "possibly unrevealed", so the chord is not rejected under 4e if any neighbor is `UNKNOWN`.

### §5.1 — Chord-neighbor rule detail (FR-016..017)

`hasInitialOrUnknownNeighbor(state, x, y)` returns `true` iff at least one in-bounds Moore neighbor of `(x, y)` is `INITIAL` or `UNKNOWN`. Rule 4e fires when it returns `false` — i.e. **every** in-bounds neighbor is a revealed number, `FLAG`, `HIT_MINE`, or `MINE` (nothing for the chord to reveal, and no `UNKNOWN` to be lenient about).

- `FLAG` neighbors are excluded from "actionable" because a chord does NOT touch flagged cells (they are marked mines the player accounted for) — checking "is any neighbor `INITIAL` or `UNKNOWN`" naturally excludes flags (`FLAG` is neither).
- Edge/corner cells: the neighbor set is the 8 Moore offsets intersected with the board bounds (edge = 5 neighbors, corner = 3); the rule applies to the in-bounds neighbors only.
- A chord whose adjacent-flag count ≠ the number is still **legal** (025 FR-015e retained) — rule 4e judges whether there is anything to reveal, not whether the chord will succeed.

---

## §6 — `saolei_init` is never terminal-blocked

`saolei_init` is registered separately and does NOT call `validateMove`. It always re-dispatches `KeyboardPressPart{F2}` (025 FR-019) and re-seeds the recognized state. Therefore:

- After a `game_won` rejection, `saolei_init` is the recovery action (restarts the game).
- After a `game_over` (loss) rejection, same.
- `saolei_init` itself returns `game status: <status-of-the-new-board>` (typically `playing` for a fresh all-INITIAL board, but whatever the post-init screenshot recognizes to).

This is unchanged from 025 operationally; it is restated here because the terminal-win path makes `saolei_init`'s non-terminal status load-bearing (FR-021: "`saolei_init` is unaffected").

---

## §7 — What is NOT changed

- The four-tool surface (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`) — unchanged.
- The desktop-facing operation Parts (`KeyboardPressPart{F2}` for init; `MouseMoveAndClickPart` at the fixed client-space geometry with `WINDOW_MESSAGE` for cell ops — 024/025) — unchanged.
- The single-text-block return contract (025 FR-012) — the status line is part of the same text block, not a new content-block kind.
- The existing rejection reasons and their semantics — unchanged (two are added, none removed, none redefined).
- The recognized-state lifecycle (025 FR-013: in-memory, per-session, lost on agent restart) — unchanged; the win predicate is evaluated against the same state.
- The `UNKNOWN`-leniency principle (025 FR-018) — retained in both new rules (§5).
