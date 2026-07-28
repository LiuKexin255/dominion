# Contract: saolei MCP — Counter-Informed Win (028)

**Feature**: 028-saolei-win-counter-fix | **Scope**: how the saolei MCP consumes the strengthened `isWin`; the text-result contract.

Source: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`. This contract is **additive in accuracy only** — the text the model reads is unchanged in wording/shape; only *when* `won` is decided changes.

## What changes

### `gameStatus(state)` / `validateMove(state, ...)` / `isTerminalState(state)` — read `state.mineCounter`

These pure helpers keep their `state: GameState` signatures (the counter rides inside the state — [research.md](../research.md) D8). Their behaviour now derives `won` via the counter-informed `isWin`:

- `gameStatus`: `isTerminalState(state)` (loss: `HIT_MINE`/`MINE`) ⇒ `"lost"`; else `isWin(state)` (now requires `mineCounter === {decoded:true, value:0}` AND grid-revealed) ⇒ `"won"`; else `"playing"`. **Loss still takes precedence over win** ([027](../../027-chat-bubble-game-state/spec.md) preserved).
- `validateMove`'s terminal check: a loss ⇒ existing `game_over`; a win (per the strengthened `isWin`) ⇒ existing `game_won`. Mutually exclusive as before.

Because the genuine win now additionally requires the counter to read `000`, a board the grid-only rule would have called a win (e.g. `saolei_9`, over-flagged, counter `-01`) is now `playing` — so it is **no longer terminal** and cell operations are **not** rejected as `game_won`. This is the bug fix (FR-012, SC-004).

### `SaoleiBoardApi` seam — UNCHANGED shape

```ts
export interface SaoleiBoardApi {
  init(png: Buffer): GameState;     // the returned GameState now carries mineCounter
  update(png: Buffer): GameState;   // likewise
}
```
The seam still returns `GameState`; the counter is carried on it (populated by the default `createDefaultBoardApi`, which wraps `SaoleiBoard`). Test fakes (`makeFakeBoardApi`) supply `GameState`s whose `mineCounter` is set to match the scenario (decoded `000` for a win, decoded non-zero for the `saolei_9`-style case, etc.).

## What does NOT change (the [027] text contract, preserved verbatim)

- **Tool surface**: exactly `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`.
- **Single text content block** per tool result ([025 FR-012](../../025-desktop-image-state-refine/spec.md)).
- **`game status:` line** — `game status: won` / `game status: lost` / `game status: playing`, on its own line, alongside the outcome line and the (grid-only) text board. Same wording; emitted when a recognized state exists, omitted when none ([027 FR-012..015](../../027-chat-bubble-game-state/spec.md)).
- **Rejection bodies** — `rejected: <reason>` + current text board + valid coordinate range + the `game status:` line, per [027 FR-016/FR-023](../../027-chat-bubble-game-state/spec.md). The reason codes are unchanged (`game_won`, `game_over`, `out_of_bounds`, `cell_already_revealed`, `cell_is_flagged`, `cannot_flag_revealed`, `chord_requires_number`, the chord-neighbor reason).
- **`saolei_init` is never terminal-blocked** — it always re-dispatches F2 to restart ([027 FR-021](../../027-chat-bubble-game-state/spec.md)).
- **Text board body** — `renderBoardText(state)`, grid only; `mineCounter` is NOT surfaced as text.

## Observable behaviour delta (the whole point of the feature)

| Board (recognized) | Before 028 | After 028 |
|---|---|---|
| grid all-revealed, counter `000` (saolei_10) | `game status: won`; next cell op ⇒ `game_won` | **unchanged** — `won`; `game_won` |
| grid all-revealed, counter `-01` (saolei_9, over-flagged) | `game status: won`; next cell op ⇒ `game_won` ❌ | `game status: playing`; cell ops **allowed** ✓ |
| counter `000`, grid has `INITIAL` (saolei_11) | `game status: playing` | **unchanged** — `playing` |
| any loss (`HIT_MINE`/`MINE`) | `game status: lost`; `game_over` | **unchanged** — `lost`; `game_over` |

## Large-test acceptance (Constitution §VI)

The agent large test (`projects/game/testplan/agent_saolei_test.go`) MUST be extended to cover the new `playing`-on-overflag row (a canned board whose `mineCounter` is decoded non-zero and whose grid is all-revealed ⇒ `game status: playing`, cell op allowed) and MUST continue to pass the genuine-win row (`won` + `game_won`). The large test MUST be executed via the testplan skill (full deploy→test→cleanup) with all cases passing — build-only is not acceptance. See [quickstart.md](../quickstart.md).
