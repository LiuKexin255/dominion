# Data Model: Saolei Win-Detection Counter Cross-Check

**Feature**: 028-saolei-win-counter-fix | **Phase**: 1 (design) | **Date**: 2026-07-28

This document specifies the **types and data** the feature introduces or changes. It is the authoritative source for the `contracts/` (which fix the public shapes) and for `tasks.md` (which implements them). All coordinates are **screenshot space** (full window incl. non-client chrome — the same space as `DEFAULT_GEOMETRY.originYPx = 200`), consistent with the library's existing recognition (see `projects/game/pkg/saolei-board/src/core/geometry.ts` and `specs/018-saolei-mcp/research.md` D6).

## Entities

### 1. `MineCounter` (NEW tagged union)

The decoded top-left mine counter — the game's own `mines − flags` display.

```ts
/** The decoded mine counter, or "undecodable". Carried on `GameState.mineCounter`. */
export type MineCounter =
  | { decoded: true; value: number }   // value = mines − flags (may be negative, e.g. over-flagged)
  | { decoded: false };                // undecodable (region absent, non-classic header, or a digit pattern did not match the table)
```

**Fields**:
- `decoded` — discriminant tag.
- `value` (present iff `decoded: true`) — the integer displayed: `0` ⇔ counter reads `000` ⇔ `flags === mines`; negative when the player over-flags (e.g. `-01`). Range realistically `[−99, 999]`; the win check only cares about `=== 0`.

**Validation rules** (enforced by the decoder, FR-003/FR-004):
- `decoded: false` MUST be reported (never a fabricated digit) when the counter region is absent OR any digit cell's ON-segment set matches no entry in the glyph table (D4).
- `value` MUST equal the displayed 3-glyph value with sign semantics: cell-0 `-` ⇒ `-(10·d1 + d2)`; else `100·d0 + 10·d1 + d2` (D4).

**Relationships**: carried as `GameState.mineCounter` (entity 2); consumed by `isWin` (entity 4) and surfaced in CLI `--debug` diagnostics.

### 2. `GameState` (CHANGED — add optional `mineCounter`)

```ts
export interface GameState {
  width: number;
  height: number;
  grid: CellStatus[][];
  /** The decoded top-left mine counter, or `undefined` when not decoded in this
   *  pass (e.g. a synthetic state). `isWin` treats `undefined` and
   *  `{ decoded: false }` identically (lenient ⇒ not a win). */
  mineCounter?: MineCounter;
}
```

**Change rationale (D8)**: the counter is part of the recognized board state (decoded from the same screenshot, same pass as the grid). The field is **optional** so existing `GameState` construction (tests, ad-hoc callers) stays valid; `undefined` is treated as "not decoded" (lenient).

**Invariants preserved / clarified**:
- `renderBoardText(state)` renders **grid only** — `mineCounter` is NOT part of the text board (the MCP text-result contract [025 FR-012](../025-desktop-image-state-refine/spec.md) is unchanged; the counter is an internal win signal, not surfaced to the model as text).
- `checkCompatible(prev, next)` (`validate.ts`) compares **grid only** — `mineCounter` is NOT part of the monotonic state-compatibility check (the counter legitimately moves up/down as flags are placed/removed within one game; only revealed cells are permanent). The grid-transition rules ([027](../027-chat-bubble-game-state/spec.md) monotonic validation) are unchanged.
- `width`/`height`/`grid` semantics are unchanged.

**State transitions**: none new. `mineCounter` is repopulated on every recognition pass (`recognizeBoard` / `SaoleiBoard.updateFromScreenshot`) — it is a per-screenshot snapshot, not a monotonic accumulator.

### 3. `CounterProfile` (NEW decode profile — peer of `ColorProfile` / `BoardGeometry`)

The tunable thresholds for the 7-segment decode.

```ts
/** Tunable 7-segment mine-counter decode profile. Defaults target classic Win32. */
export interface CounterProfile {
  /** Counter region in screenshot space (the 82×50 box). */
  regionX: number;       // default 32
  regionY: number;       // default 120
  regionW: number;       // default 82
  regionH: number;       // default 50
  /** Per-digit cell origins (screenshot space) and size. */
  cellOrigins: ReadonlyArray<{ x: number; y: number }>; // default [{38,126},{64,126},{90,126}]
  cellW: number;         // default 22
  cellH: number;         // default 42
  /** Segment-core sub-rects in LOCAL cell coords (x0,x1,y0,y1 inclusive). See D3. */
  segments: Record<SegmentId, { x0: number; x1: number; y0: number; y1: number }>;
  /** Red-pixel test: R ≥ r, G ≤ gMax, B ≤ bMax. */
  redMinR: number; redMaxG: number; redMaxB: number;   // default 150 / 80 / 80
  /** A segment is ON iff its core red-pixel ratio exceeds this (default 0.5;
   *  measured ON ≥ 0.90, OFF = 0.00 — wide margin). */
  segmentOnRatio: number; // default 0.5
}

/** The 7 segments + the middle bar that doubles as the minus sign. */
export type SegmentId = "a" | "b" | "c" | "d" | "e" | "f" | "g";
```

**Default values** are the measured constants from [research.md](./research.md) D2/D3/D5. The profile is tunable via the CLI `--debug` calibration flow, exactly as `ColorProfile` is (`projects/game/pkg/saolei-board/README.md` calibration section).

**Relationships**: consumed by `decodeMineCounter` (entity 5); surfaced in `--debug` so an operator can re-measure against new screenshots.

### 4. Win predicate (CHANGED semantics — `isWin`)

```ts
/** A board is a win iff (a) no cell is INITIAL/HIT_MINE/MINE/UNKNOWN AND
 *  (b) the mine counter is decoded and reads 000. Pure; no I/O. (FR-005..010) */
export function isWin(state: GameState): boolean;
```

**Decision rule** (FR-005..010, [research.md](./research.md) D7):
```
isWin(state) =
  (∀ cell ∈ state.grid: cell ∉ {INITIAL, HIT_MINE, MINE, UNKNOWN})
  ∧ (state.mineCounter === { decoded: true, value: 0 })
```
- Grid half (unchanged from [027 FR-009](../027-chat-bubble-game-state/spec.md)): every cell is a revealed number `0..8` or `FLAG`.
- Counter half (NEW): `mineCounter` is decoded AND `value === 0`.
- Leniency: `mineCounter === undefined` or `{ decoded: false }` ⇒ `false` (never claim a win on an absent/uncertain counter — FR-008).
- Loss precedence (unchanged): a `HIT_MINE`/`MINE` cell ⇒ grid half fails ⇒ `false` regardless of the counter (FR-010). (`isTerminalState` stays loss-only and is checked before `isWin` in the MCP, [027](../027-chat-bubble-game-state/spec.md).)

**Boundary cases** (the fixtures):
| state | grid half | counter half | `isWin` |
|---|---|---|---|
| `saolei_10` | ✓ all revealed/flagged | `{decoded:true, value:0}` | **true** |
| `saolei_9` (11 flags > 10 mines) | ✓ all revealed/flagged | `{decoded:true, value:-1}` | **false** (FR-006) |
| `saolei_11` (counter 000 but INITIAL cells) | ✗ has `INITIAL` | `{decoded:true, value:0}` | **false** (FR-007) |
| synthetic: all-revealed, `mineCounter` undefined | ✓ | `undefined` | **false** (FR-008) |

### 5. `decodeMineCounter` (NEW recognition function — peer of `classifyCell`)

```ts
/** Decode the top-left mine counter from a decoded screenshot image.
 *  Pure: no I/O, no mutation. Returns { decoded: false } when the region is
 *  absent or a digit pattern is unrecognised (FR-003). */
export function decodeMineCounter(
  img: DecodedImage,
  profile?: CounterProfile,
): MineCounter;
```

**Algorithm** (for each of the 3 fixed digit cells, [research.md](./research.md) D1/D3/D4):
1. For each segment `s ∈ {a,b,c,d,e,f,g}`, count red pixels in its core sub-rect; mark `s` ON iff `count / coreArea > segmentOnRatio`.
2. Look up the ON-set in the glyph table (`0`-`9`, or `{g}` ⇒ `-`).
3. If any cell's ON-set matches no table entry ⇒ return `{ decoded: false }`.
4. Else compute `value` per sign semantics (D4) and return `{ decoded: true, value }`.

**Inputs**: the decoded image (`decode.ts` `DecodedImage`) — the SAME image `recognizeBoard` already decodes for the grid; no second PNG decode. Optional `CounterProfile` override (defaults to classic Win32).

**Relationships**: called once per `recognizeBoard` / `SaoleiBoard.updateFromScreenshot` pass; result written to `state.mineCounter`.

## Recognition output change (`RecognizeResult`)

`RecognizeResult` (`recognize.ts`) gains no new field of its own — the counter is carried on `state.mineCounter`. The recognition pass gains one step: after building the grid, call `decodeMineCounter(img, counterProfile)` and set `state.mineCounter`. (Per-cell `diagnostics` are unaffected; a counter diagnostics block MAY be added for `--debug` but is not required by the contract.)

## Stateful `SaoleiBoard` change

`SaoleiBoard.init` and `updateFromScreenshot` populate `current.mineCounter` from the same decoded image. The counter is re-read on every update (non-monotonic — it moves freely within a game). The dimension/profile fixed at `init` now also fixes the `CounterProfile` (alongside `ColorProfile`/`BoardGeometry`).

## What does NOT change

- `CellStatus` union, `renderBoardText` output (text board = grid only), `checkCompatible` grid rules, the four-tool MCP surface, the `game status:` line wording, the `game_won`/`game_over` rejection bodies, the single-text-block contract, the proto. See [contracts/](./contracts/).
