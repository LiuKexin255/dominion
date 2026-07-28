# Contract: `@dominion/game-saolei-board` Public API (028)

**Feature**: 028-saolei-win-counter-fix | **Scope**: the library's public surface changed/added by this feature.

This contract fixes the public shapes the library exposes (and the agent consumes) so the change is reviewable independently of implementation. Source: `projects/game/pkg/saolei-board/src/core/index.ts` (public barrel). All coordinates are **screenshot space**.

## NEW exports

### `decodeMineCounter(img, profile?): MineCounter`

Pure mine-counter decoder (peer of `classifyCell`). Decodes the top-left 3-digit 7-segment red LED at screenshot-space X 32..113, Y 120..169.

- **Params**: `img: DecodedImage` (the already-decoded PNG — same image `recognizeBoard` uses for the grid; no second decode); optional `profile?: CounterProfile` (defaults to `DEFAULT_COUNTER_PROFILE`, classic Win32).
- **Returns**: `MineCounter` — `{ decoded: true; value }` (value = `mines − flags`, may be negative) or `{ decoded: false }` (region absent or a digit pattern matched no glyph).
- **Purity**: no I/O, no mutation, no side effects.
- **Spec**: FR-001, FR-003, FR-004. Design: [data-model.md](../data-model.md) entity 5; algorithm: [research.md](../research.md) D1/D3/D4.

### `MineCounter` (type)

```ts
export type MineCounter =
  | { decoded: true; value: number }
  | { decoded: false };
```
`value === 0` ⇔ counter reads `000` ⇔ `flags === mines`. See [data-model.md](../data-model.md) entity 1.

### `CounterProfile` (type) + `DEFAULT_COUNTER_PROFILE` (value)

The tunable 7-segment decode profile (peer of `ColorProfile`/`BoardGeometry`). `DEFAULT_COUNTER_PROFILE` targets classic Win32 with the measured constants. See [data-model.md](../data-model.md) entity 3 and [research.md](../research.md) D2/D3/D5.

### `SegmentId` (type)

`"a" | "b" | "c" | "d" | "e" | "f" | "g"` — the 7 segments (the middle bar `g` doubles as the minus-sign detector).

## CHANGED exports

### `GameState` — adds optional `mineCounter`

```ts
export interface GameState {
  width: number;
  height: number;
  grid: CellStatus[][];
  mineCounter?: MineCounter;   // NEW — decoded by recognizeBoard; undefined ⇒ isWin treats as undecodable
}
```
- **Backward compatible**: the field is optional; existing construction stays valid.
- **Render invariant**: `renderBoardText(state)` renders **grid only** — `mineCounter` is never in the text board.
- **Validation invariant**: `checkCompatible(prev, next)` compares **grid only** — `mineCounter` is not part of the monotonic check.

### `isWin(state): boolean` — strengthened semantics, SAME signature

```ts
export function isWin(state: GameState): boolean;
```
- **Signature UNCHANGED** (single `GameState` arg; pure). Existing callers compile without change.
- **Semantics CHANGED** (FR-005..010): returns `true` **iff** (a) no cell is `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` **and** (b) `state.mineCounter === { decoded: true, value: 0 }`. `mineCounter === undefined` or `{ decoded: false }` ⇒ `false` (lenient, FR-008).
- **Supersedes** the grid-only [027 FR-009](../../027-chat-bubble-game-state/spec.md) (the grid condition is retained as one half of the conjunction).

### `recognizeBoard` / `SaoleiBoard` — populate `state.mineCounter`

- `recognizeBoard(png, opts)` decodes the counter (via `decodeMineCounter`) in the same pass that builds the grid and sets `state.mineCounter` on the returned `state`.
- `SaoleiBoard.init` fixes the `CounterProfile` alongside `ColorProfile`/`BoardGeometry`; `updateFromScreenshot` re-decodes the counter on every update (the counter is non-monotonic within a game).
- `RecognizeResult` shape is otherwise unchanged (the counter rides on `state`, not as a new top-level field).

## UNCHANGED exports (explicitly)

`CellStatus`, `renderBoardText`/`cellSymbol` (grid-only text board), `classifyCell`/`DEFAULT_COLOR_PROFILE`, `decodePng`/`getRGB`/`extractCellRegion`, `DEFAULT_GEOMETRY`/`resolveGeometry`/`detectBoardSize`/`cellOrigin`, `checkCompatible`/`BoardDimensionMismatchError`/`BoardStateIncompatibleError`, the `SaoleiBoard` method surface (`init`/`state`/`dimensions`/`updateFromScreenshot`/`renderText`).

## Calibration flow (`--debug`)

The CLI `--debug` path surfaces the decoded counter (per-cell segment ON/OFF + the decided glyphs + the final value), mirroring the per-cell diagnostics, so an operator can re-tune `DEFAULT_COUNTER_PROFILE` against new screenshots using the existing calibration flow (`projects/game/pkg/saolei-board/README.md`).
