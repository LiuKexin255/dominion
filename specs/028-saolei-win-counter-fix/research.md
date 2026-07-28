# Research: Saolei Win-Detection Counter Cross-Check

**Feature**: 028-saolei-win-counter-fix | **Phase**: 0 (research) | **Date**: 2026-07-28

This document resolves every technical decision for the feature. Each decision is **grounded in first-party evidence** — a direct pixel analysis of the fixtures `testdata/saolei_9.png` (badcase: grid all-revealed, counter `-01`), `testdata/saolei_10.png` (genuine win, counter `000`), `testdata/saolei_11.png` (badcase: counter `000` + `INITIAL` cells), plus `saolei_1..5` (generalization check). The analysis decoded the PNGs (pure-Python, no deps) and measured the counter region the user specified (screenshot-space X 32..114, Y 120..170, 82 × 50 px).

## D1 — Counter decode algorithm: fixed-geometry 7-segment segment-core detection

**Decision**: Decode each of the three counter digits by testing the 7 classic segments (`a,b,c,d,e,f,g`) plus the minus sign via fixed rectangular **segment-core** sub-regions inside each digit cell; a segment is ON when its core's red-pixel ratio exceeds a threshold. Map the ON-segment set to a glyph (`0`-`9` or `-`).

**Rationale (first-party evidence)**: The classic Win32 minesweeper counter is a fixed-position, fixed-size red-on-black 7-segment LED — exactly the kind of fixed-geometry target the library already handles for cells (`classify.ts`). A segment-core count is deterministic, dependency-free, and — measured on the fixtures — separates ON from OFF with a wide margin:

| Fixture / glyph | segment | ON-set | min ON-segment red ratio | max OFF-segment red ratio |
|---|---|---|---|---|
| saolei_10 cell0 (`0`) | a,b,c,d,e,f | `abcdef` | 0.90 | 0.00 |
| saolei_9 cell1 (`0`) | a,b,c,d,e,f | `abcdef` | 0.90 | 0.00 |
| saolei_9 cell2 (`1`) | b,c | `bc` | 0.90 | 0.00 |
| saolei_9 cell0 (`-`) | g | `g` | 1.00 | 0.00 |

So a threshold of **0.50** (segment ON iff > 50% of its core pixels are red) sits in a 0.90-wide gap — the decode is robust, not marginal. The decoder was run on **all** fixtures (not just the 3 win-boundary ones) and produced plausible values — `saolei_1`⇒`040`, `saolei_2`⇒`010`, `saolei_4`/`saolei_5`⇒`033` — confirming it generalizes rather than over-fitting.

**Alternatives considered**:
- *Whole-glyph template matching* (compare each digit cell against 11 stored bitmap templates `0..9,-`). Rejected: equivalent robustness but needs 11 calibrated templates and a similarity metric; segment detection needs only 7 small rectangles + a pattern table and is more interpretable to calibrate (each segment is independently observable in `--debug`).
- *OCR / Tesseract*. Rejected: heavy native dependency, Bazel-unfriendly, and overkill for a 7-segment LED the library already recognises by colour geometry.
- *Counting grid flags vs. a configured mine count*. Rejected: the library does not know the mine count a priori, and — more importantly — counting flags trusts the grid recognition, which is exactly what produced the false positive (the counter is the **game's own** ground truth, independent of the grid recognition).
- *Smiley-face win indicator (top-center)*. Rejected: out of scope (user named only the left counter) and adds a second new recognition surface for no incremental win-detection value over the counter.

## D2 — Digit-cell geometry (fixed screenshot-space sub-rects)

**Decision**: The counter occupies screenshot-space **X 32..113, Y 120..169** (82 × 50 px, as stated by the operator). It contains three **fixed** digit cells, each **22 × 42 px**, at:

| digit | screenshot-space origin (x, y) | size |
|---|---|---|
| cell 0 (leftmost — digit or minus sign) | (38, 126) | 22 × 42 |
| cell 1 | (64, 126) | 22 × 42 |
| cell 2 | (90, 126) | 22 × 42 |

**Rationale**: The three `0` digits of `saolei_10`/`saolei_11` span exactly x 38..59, 64..85, 90..111 (y 126..167). The `saolei_9` minus sign (x 40..57) and `1` (x 106..111) are narrower glyphs drawn **within** their fixed cells (cell 0 and cell 2 respectively) — so the cells are fixed and the glyph varies, NOT the other way around. Decoding must therefore sample **fixed** cell sub-rects (consistent with the library's fixed-geometry philosophy), not dynamically group red columns (which would mis-segment the narrow minus/`1` glyphs). The constants are peers of `DEFAULT_GEOMETRY` (`geometry.ts`).

## D3 — Segment-core sub-rects (local cell coordinates, 22 × 42)

**Decision**: Each segment is sampled over a fixed rectangular "core" in **local cell coordinates** (origin = cell's top-left, 22 wide × 42 tall). A segment is ON iff the red-pixel count in its core exceeds 50% of the core area. The cores (measured from the clean `0` in `saolei_10` cell0 and the `-` / `1` in `saolei_9`):

| segment | core (local x0..x1, y0..y1 inclusive) | meaning |
|---|---|---|
| `a` | x 2..19, y 0..1 | top horizontal |
| `g` | x 2..19, y 20..21 | middle horizontal (also the **minus-sign** bar, which spans y 18..23) |
| `d` | x 2..19, y 40..41 | bottom horizontal |
| `f` | x 0..5, y 4..17 | upper-left vertical |
| `b` | x 16..21, y 4..17 | upper-right vertical |
| `e` | x 0..5, y 24..37 | lower-left vertical |
| `c` | x 16..21, y 24..37 | lower-right vertical |

**Rationale**: These cores sit on the **solid centre** of each segment (avoiding the LED's beveled/diamond transitions at segment ends), so an ON segment reads ≥ 0.90 red and an OFF segment reads 0.00 (D1 table). The `g` core doubles as the minus-sign detector: the minus is exactly "only `g` on" (no digit `0`-`9` has the lone-`g` pattern), so `{g}` ⇒ `-` unambiguously.

## D4 — Glyph pattern table & value semantics

**Decision**: Map the ON-segment set to a glyph via the standard 7-segment table:

| glyph | ON segments | glyph | ON segments |
|---|---|---|---|
| `0` | a,b,c,d,e,f | `5` | a,c,d,f,g |
| `1` | b,c | `6` | a,c,d,e,f,g |
| `2` | a,b,d,e,g | `7` | a,b,c |
| `3` | a,b,c,d,g | `8` | a,b,c,d,e,f,g |
| `4` | b,c,f,g | `9` | a,b,c,d,f,g |
| `-` (minus) | g | | |

The numeric **value** of the counter:
- if cell 0 is `-`: `value = -(10·digit1 + digit2)` (the minus occupies the leftmost position; classic minesweeper shows the magnitude in the remaining two cells — `-01` ⇒ −1).
- else: `value = 100·digit0 + 10·digit1 + digit2`.

The win check needs only **`value === 0`**, which is true iff cell 0 is a digit `0` (not `-`) **and** digit1 `=== 0` **and** digit2 `=== 0`.

**Rationale**: Verified by decode — `saolei_9` ⇒ glyphs `['-','0','1']` ⇒ value −1 (matches the operator-stated `-01`); `saolei_10` / `saolei_11` ⇒ `['0','0','0']` ⇒ value 0. `saolei_1` ⇒ `['0','4','0']` ⇒ 40, etc. (D1 generalization).

**Undecodable handling**: if a cell's ON-segment set matches **no** entry in the table (e.g. a noisy read yields `{a,g}`), that cell — and therefore the whole counter — is reported `{ decoded: false }` (FR-003). The win check then returns `false` (lenient, FR-008). (Optionally, a nearest-pattern-by-Hamming-distance fallback may be offered behind the confidence threshold, but the default is strict-and-lenient — never fabricate a digit.)

## D5 — Red-pixel test

**Decision**: A pixel counts as "counter-red" iff `R ≥ 150 ∧ G ≤ 80 ∧ B ≤ 80` (the same family of test as `classify.ts`'s flag-red test `flagRedMinR=150, flagRedMaxG=100, flagRedMaxB=100`, tightened on G/B because the LED red is saturated against a black background). The threshold lives in `CounterProfile` (tunable via `--debug`).

**Rationale**: The counter LED is saturated red on near-black; measuring the fixtures, this test cleanly separates the LED red from the surrounding black bezel and the grey board (D1's 0.90/0.00 margin used exactly this test). Reusing the flag-red threshold family keeps the colour-analysis approach uniform with `classify.ts`.

## D6 — `MineCounter` result type

**Decision**:

```ts
export type MineCounter =
  | { decoded: true; value: number }   // value = mines − flags (may be negative)
  | { decoded: false };                // undecodable (lenient)
```

`value === 0` ⇔ the counter displays `000` ⇔ `flags === mines`. `decoded: false` covers both "region absent / non-classic header" and "a cell's pattern did not match the table".

**Rationale**: The win check needs a three-way distinction — definitely-zero (⇒ candidate win), definitely-non-zero (⇒ not a win, the `saolei_9` fix), and undecodable (⇒ not a win, lenient). A bare `number | null` would conflate "undecodable" with "not yet decoded"; the tagged union makes the three states explicit and keeps `isWin`'s leniency rule (FR-008) unambiguous.

## D7 — Confidence / leniency rule

**Decision**: `isWin` returns `true` **only when** (a) the grid has no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` cell **and** (b) `state.mineCounter` is `{ decoded: true, value: 0 }`. If `mineCounter` is `undefined` (not populated) or `{ decoded: false }`, the predicate returns `false` — a missed win (false negative, recoverable) is preferred over a false win (terminal, blocks all further play). This mirrors the library's existing `UNKNOWN`-cell leniency ([027 FR-010](../027-chat-bubble-game-state/spec.md)) and [025 FR-018](../025-desktop-image-state-refine/spec.md).

**Rationale**: Forced by FR-005..008 and the two badcases. The leniency direction (false-negative over false-positive) is the same the library already follows for uncertain cells.

## D8 — API shape: how the counter flows from recognition to `isWin`

**Decision**: Carry the decoded counter as an **optional field `mineCounter?: MineCounter` on `GameState`**, populated by `recognizeBoard` / `SaoleiBoard` during the same pass that produces the grid. `isWin(state: GameState)` stays a **single-argument pure function** (FR-009 preserved) and reads `state.mineCounter` internally. The MCP seam (`SaoleiBoardApi.init/update → GameState`), the session variable (`recognized: GameState | null`), and the pure helpers (`gameStatus`, `isTerminalState`, `validateMove`, `neighbors`, …) keep their `state: GameState` signatures — they automatically carry the counter through the state object they already thread.

**Rationale**: This is the lowest-risk change that fixes the bug cleanly:
- `GameState` already represents "the recognized board state" — the MCP's session variable is literally `recognized: GameState`. The mine counter is part of that recognized state (decoded from the same screenshot, same pass), so carrying it on `GameState` is a legitimate model extension, analogous to `width`/`height` being non-cell metadata already on the type.
- The seam, the session variable, and ~8 pure helpers (and ~10 test fakes) need **no signature changes** — the counter rides inside the value they already pass. `isWin`'s public signature is unchanged.
- An **optional** field keeps the type backward-compatible: any `GameState` constructed without it (existing tests, ad-hoc callers) has `mineCounter === undefined`, which `isWin` treats as undecodable ⇒ `false` (D7). The `win.test.ts` positive cases are updated to set a decoded `000` counter — an expected change, since the win *semantics* changed.
- `renderBoardText` ignores `mineCounter` (the text board the model sees is the grid only — MCP text contract unchanged, FR-012). `checkCompatible` ignores `mineCounter` (the counter is non-monotonic — it moves freely as flags are placed/removed within one game; only the grid is monotonically checked).

**Alternatives considered (rejected)**:
- *Broaden the seam to return a `RecognizedBoard = { state; mineCounter }` bundle and retype the session/helpers/tests.* Clean type separation, but ~2× the churn (retype the seam, the session variable, ~8 helper signatures, ~10 test fakes) for the same correctness — and it needlessly widens the blast radius of a bug fix. Rejected under §II's "simplify when the architecture is sufficient" half.
- *`isWin(state, mineCounter)` two-argument form.* Makes the counter dependency explicit in the signature, but forces every caller (MCP helpers + tests) to thread a second value alongside `state` — exactly the churn the optional-field choice avoids, with no robustness gain (the counter is still decoded once per screenshot and carried with the state). Rejected.
- *A new top-level `RecognizedBoard` wrapper retyped across the library + agent.* Most "correct" in the abstract, but disproportionate to a single-field addition; rejected as over-engineering (§II).

**Conclusion**: the optional `mineCounter` field is the refactor that fits the existing architecture with minimal, safe churn — neither a patch (it is a first-class typed recognition output) nor over-engineering (no new wrapper type or seam retype).

## D9 — Large-test (acceptance) impact

**Decision**: The agent large test (`projects/game/testplan/agent_saolei_test.go`) currently asserts `game status: won/lost/playing` and the `game_won`/`game_over` terminal rejections on canned winning/losing boards. Because the win decision is now counter-informed, the large test MUST be extended with a case where a board the grid-only rule would call a win is reported `playing` because the counter ≠ `000` (e.g. a `saolei_9`-style over-flagged board), and the genuine-win path (`saolei_10`, counter `000`) MUST still surface `won` + `game_won`. The large test MUST be executed via the testplan skill (full deploy→test→cleanup) with all cases passing — build-only does NOT constitute acceptance (Constitution §VI).

**Rationale**: Constitution principle VI (services require large-test acceptance with real execution, all cases green). The MCP unit test covers the logic; the large test covers the integrated service behaviour end-to-end.

## Open questions

None. All decisions are resolved by first-party evidence (fixture pixel analysis) and the operator-stated counter geometry. No `[NEEDS CLARIFICATION]` remains.
