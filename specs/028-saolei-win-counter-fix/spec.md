# Feature Specification: Saolei Win-Detection Counter Cross-Check (False-Positive Fix)

**Feature Branch**: `028-saolei-win-counter-fix`

**Created**: 2026-07-28

**Status**: Draft

**Input**: User description: "当前 projects/game/pkg/saolei-board/ 识别游戏胜利有bug，会把没有胜利的游戏识别为胜利，为此补充了 9 和 11 两个游戏胜利的 badcase。可以根据左上角数字是否为 000 判断 flag 数量是否与地雷数量相同。"

## Motivation

The saolei-board library's win predicate (`projects/game/pkg/saolei-board/src/core/win.ts` `isWin`, introduced by [027 FR-009..011](../027-chat-bubble-game-state/spec.md)) is **grid-only**: it returns `true` iff no cell is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` (i.e. every recognized cell is a revealed number `0..8` or a `FLAG`). This rule matches the *textbook* classic-Minesweeper winning board, but it has no independent check that the **flag count equals the mine count**. In real play the recognized grid can look fully revealed/flagged while the game is **not** actually won, so `isWin` emits a **false-positive win**. That false win propagates downstream: the saolei MCP prints `game status: won` ([027 FR-012/FR-013](../027-chat-bubble-game-state/spec.md)) and rejects all further cell operations as terminal `game_won` ([027 FR-021..023](../027-chat-bubble-game-state/spec.md)) — so the agent stops on a board it has not actually finished, and the operator cannot continue a game that is still in progress.

Two newly added badcase fixtures pin the two distinct failure modes (both untracked, added by the operator under `projects/game/pkg/saolei-board/testdata/`):

| Fixture | Recognized grid (by the current engine) | Top-left mine counter (decoded from the screenshot) | Actual game | Current `isWin` | Correct? |
|---|---|---|---|---|---|
| `saolei_9.png`  | all revealed numbers + `FLAG` (no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`) — **11 flags** | **`-01`** (over-flagged: mines − flags = −1) | **not won** | `true` ❌ | false-positive — grid-only rule is insufficient |
| `saolei_10.png` | all revealed numbers + `FLAG` | **`000`** (flags == mines) | **won** | `true` ✓ | genuine win, must stay `true` |
| `saolei_11.png` | many `INITIAL` (`*`) cells + a flag cluster | **`000`** (flags == mines) | **not won** | `false` ✓ (today) | guards the counter dimension: counter==000 alone is NOT a win |

`saolei_9` is the live false positive: the grid is fully revealed/flagged yet the game is not won — the player **over-flagged** (11 flags on a 10-mine board), so `mines − flags = −1` and the counter reads **`-01`** (a leading minus sign in the leftmost digit). `saolei_11` is the symmetric guard: the counter reads `000` (flags == mines) but the board still has unrevealed cells, so it is not a win either — it ensures a counter-based fix does not over-correct into "counter==000 ⇒ win".

**Root-cause insight.** The classic Win32 minesweeper top-left LED is the **game's own ground truth** for `mines − flags`. It reads `000` exactly when the flag count equals the mine count, and a **negative** value (leading minus sign) when the player over-flags. The grid alone cannot prove `flags == mines` (the engine may mis-fill a still-`INITIAL` cell, or the player may have over-flagged — as in `saolei_9`, whose 11 flags exceed the 10 mines), so the grid-only rule is necessary but not sufficient. The robust win condition is the **conjunction**: the recognized grid is fully revealed/flagged **AND** the mine counter reads `000`. The two badcases are exactly the two halves of that conjunction:

- `saolei_9` ⇒ grid-revealed is **not** sufficient (counter reads `-01`, over-flagged).
- `saolei_11` ⇒ counter==000 is **not** sufficient (grid has `INITIAL`).
- `saolei_10` ⇒ both hold ⇒ genuine win.

Reading the counter requires a new recognition capability (decoding the 3-digit 7-segment red LED in the screenshot header), which the library does not have today — it currently recognizes only the cell grid.

## Relationship

- **Fixes a defect introduced by [027 — Chat Bubble UX Polish & Saolei Game-State Awareness](../027-chat-bubble-game-state/spec.md) US3 / FR-009..011.** [027] added the grid-only `isWin` predicate and wired it into the MCP (`gameStatus`, `validateMove`'s `game_won`). This feature strengthens that predicate with a mine-counter cross-check so the win decision is correct; the [027] MCP text-result contract (`game status: won|lost|playing`, the `game_won` terminal rejection, the single-text-block contract [025 FR-012](../025-desktop-image-state-refine/spec.md)) is **unchanged** — only the *accuracy* of the `won` decision improves.
- **Builds on the library's existing recognition core** (`projects/game/pkg/saolei-board/src/core/`): the new counter decoder is a peer of `classify.ts` (it consumes the same decoded PNG via `decode.ts` and the same fixed-geometry approach as `geometry.ts`), and the strengthened `isWin` keeps its pure-function character ([027 FR-011](../027-chat-bubble-game-state/spec.md)).
- **Independent of the desktop UI work** in [027] US1/US2 and of the chord-neighbor validation [027] US5. This feature is confined to `@dominion/game-saolei-board`'s `core/` (the counter decoder + the strengthened predicate + the fixtures) and the single `isWin` consumption site in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (which must pass the counter through to the predicate). No proto change, no new external dependency.
- **Interface (Constitution §III)**: the library gains a new recognition output (the decoded mine-counter value) and the `isWin` decision gains a second input (the counter). The exact API shape by which the counter flows from recognition to `isWin` (a field carried alongside `GameState`, a parameter, or a richer recognized-board object) is settled in **planning**, constrained by: `isWin` stays a pure function, the [027] MCP text contract is preserved, and the counter is read once per screenshot (the same single recognition pass that produces the grid). The downstream `game status:` line and the `game_won`/`game_over` rejection reasons keep their existing wording.

## Clarifications

Resolved during specification by reasonable defaults (documented in **Assumptions**). No `[NEEDS CLARIFICATION]` markers remain. The decisions settled by reasonable default are:

- **The mine-counter geometry** is operator-measured and stated verbatim in the user input: the 3-digit LED sits in **screenshot space** at **X = 32 px** from the window's left edge, **Y = 120 px** from the top, with a size of **82 × 50 px** (X 32..113, Y 120..169). This is consistent with the decoded red-LED band in the fixtures (red pixels concentrate at X 38..111, Y 126..167). The exact per-digit sub-rects (each digit ≈ 22 px wide, three digits with inter-digit gaps) are a plan-time calibration detail constrained by this bounding box.
- **The win rule is a conjunction, not a replacement.** A board is a win **only when** the recognized grid is fully revealed/flagged (no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`) **AND** the mine counter reads exactly `000`. The counter does not *replace* the grid check — `saolei_11` (counter `000` + `INITIAL` cells) proves counter-alone is insufficient; the grid check does not *replace* the counter — `saolei_9` (grid all-revealed + counter `-01`, over-flagged) proves grid-alone is insufficient.
- **Leniency on counter-recognition uncertainty**, mirroring the library's existing `UNKNOWN` cell handling ([027 FR-010](../027-chat-bubble-game-state/spec.md)) and [025 FR-018](../025-desktop-image-state-refine/spec.md)): if the counter cannot be decoded confidently, the predicate MUST NOT claim a win (it returns `false`, status stays `playing`). The library prefers a missed win (false negative, recoverable — the operator/agent simply continues) over a false win (terminal, blocks all further play). The exact confidence criterion is a plan-time detail.
- **Counter value exposure.** Recognition produces the decoded counter value (not merely a boolean) so it is reusable for calibration (`--debug`), for future features (e.g. reporting remaining mines), and for the win check. The win check itself only needs "is the value exactly 0".
- **Scope is the mine counter only.** The top-right timer LED is not decoded (out of scope; the user named only the left counter). The smiley-face win indicator (top-center) is not used.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The Library Reads the Top-Left Mine Counter from the Screenshot (Priority: P1)

The recognition library `@dominion/game-saolei-board` decodes the classic Win32 minesweeper **top-left mine counter** — the 3-digit red 7-segment LED at screenshot-space X 32..113, Y 120..169 (82 × 50 px) — and exposes its decoded value alongside the recognized cell grid. The value is the game's own `mines − flags` display (e.g. `010` at a fresh beginner board, a **negative** value such as `-01` when the player over-flags, `000` when flags == mines). This is the ground-truth signal the win check (US2) cross-references, and it is available to the CLI `--debug` path for calibration exactly like the per-cell diagnostics.

**Why this priority**: This is the enabling capability. Without reading the counter there is no way to know whether the flag count matches the mine count, so the false-positive win (US2) cannot be fixed. It lives in the library (where the screenshot pixels and the fixed-geometry recognition already live) and is independently testable against the existing fixtures.

**Independent Test**: Run the recognizer on `saolei_9.png`, `saolei_10.png`, `saolei_11.png`; confirm the decoded counter reads **`-01`** (non-zero; a leading minus sign — over-flagged) for `saolei_9`, exactly `000` for `saolei_10`, and exactly `000` for `saolei_11`.

**Acceptance Scenarios**:

1. **Given** a screenshot whose top-left LED displays `000` (e.g. `saolei_10.png`, `saolei_11.png`), **When** the recognizer decodes the counter region, **Then** it yields the value `000` (all three digits zero).
2. **Given** a screenshot whose top-left LED displays a non-zero value, including a **negative** value with a leading minus sign (e.g. `saolei_9.png`, whose counter reads `-01` because 11 flags exceed the 10 mines), **When** the recognizer decodes the counter region, **Then** it yields that non-zero value (the decoded integer ≠ 0; the leading minus sign is recognized, not misread as a digit).
3. **Given** a recognized screenshot, **When** a consumer inspects the recognition output, **Then** the decoded mine-counter value is present alongside the cell grid (available to the win predicate, the CLI `--debug` path, and library tests).
4. **Given** the counter region cannot be decoded confidently (e.g. an occluded or non-classic header), **When** the recognizer decodes the counter, **Then** it reports "counter undecodable" rather than a fabricated digit value (lenient — downstream never trusts a guessed counter).

---

### User Story 2 - A Win Requires BOTH a Fully-Revealed Grid AND the Mine Counter Reading 000 (Priority: P1)

The win predicate is strengthened so that a recognized board is a win **only when** two conditions hold together: (a) the recognized grid is fully revealed/flagged — no `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` cell (the existing grid rule); **and** (b) the mine counter reads exactly `000` (flag count == mine count). Either condition alone is **not** a win: `saolei_9` (grid all-revealed, counter `-01` — over-flagged) is not a win; `saolei_11` (counter `000`, grid with `INITIAL` cells) is not a win. The genuine-win fixture `saolei_10` (both hold) remains a win. This eliminates the false-positive win that today causes the agent to stop on a board it has not finished.

**Why this priority**: This is the user-visible fix — it stops the false `game status: won` and the premature `game_won` terminal rejection on non-won boards. It depends on US1 (the counter value) and is the whole point of the feature.

**Independent Test**: Construct recognized states (grid + decoded counter) for the three fixtures — `saolei_9` (all-revealed grid + counter `-01`), `saolei_10` (all-revealed grid + counter `000`), `saolei_11` (grid with `INITIAL` + counter `000`) — and confirm the strengthened win predicate returns `false`, `true`, `false` respectively.

**Acceptance Scenarios**:

1. **Given** a recognized board whose grid is fully revealed/flagged (no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`) **and** whose mine counter reads `000`, **When** the win predicate is evaluated, **Then** it returns `true` (a genuine win — `saolei_10`).
2. **Given** a recognized board whose grid is fully revealed/flagged **but** whose mine counter reads a non-zero value (flags ≠ mines — including the negative over-flagged case, e.g. `saolei_9`'s `-01` with 11 flags > 10 mines), **When** the win predicate is evaluated, **Then** it returns `false` (the false-positive fix — `saolei_9`).
3. **Given** a recognized board whose mine counter reads `000` **but** whose grid still contains any `INITIAL` cell (game in progress), **When** the win predicate is evaluated, **Then** it returns `false` (counter-alone is not a win — `saolei_11`).
4. **Given** a recognized board whose mine counter is undecodable, **When** the win predicate is evaluated, **Then** it returns `false` (lenient — never claim a win on an uncertain counter), even if the grid looks fully revealed.
5. **Given** a recognized board with a `HIT_MINE`/`MINE` cell (a loss) regardless of the counter, **When** the win predicate is evaluated, **Then** it returns `false` (loss is not a win; the existing loss signal and its precedence over win are unchanged — [027 FR-010](../027-chat-bubble-game-state/spec.md)).
6. **Given** the strengthened predicate, **When** the saolei MCP derives `gameStatus` and the `game_won` terminal check, **Then** a non-won board no longer surfaces `game status: won` nor rejects subsequent operations as `game_won` (the [027] MCP text contract is preserved; only the `won` decision is more accurate).

---

### Edge Cases

- **Grid all-revealed with a non-zero counter** (`saolei_9`): not a win — the counter is the authority that flags ≠ mines (US2 acceptance 2). `saolei_9` is specifically the **over-flagged** sub-case: 11 flags on a 10-mine board, so the counter reads the negative value `-01`.
- **Counter `000` with unrevealed cells** (`saolei_11`): not a win — flags == mines does not imply all safe cells are revealed; the grid rule still applies (US2 acceptance 3).
- **Counter `000` and grid all-revealed** (`saolei_10`): a genuine win — both halves of the conjunction hold (US2 acceptance 1).
- **Undecodable counter** (e.g. a non-classic header, an occluded LED, or an edge digit below the decode confidence): the predicate returns `false` (lenient). The library does not fabricate a counter value and never claims a win on an uncertain read (US2 acceptance 4). This mirrors the existing `UNKNOWN`-cell leniency ([027 FR-010](../027-chat-bubble-game-state/spec.md)).
- **Negative counter** (`saolei_9` is the concrete example): classic minesweeper displays a negative remaining-mine count — a leading **minus sign** in the leftmost digit position — when the player over-flags (flags > mines). Such a value is non-zero ⇒ not a win; the decode MUST recognize the minus sign (not misread it as a digit) and MUST classify the value as "not 000" without crashing.
- **Loss takes precedence over the win check**: a board with `HIT_MINE`/`MINE` is a loss and the predicate returns `false` for it regardless of the counter ([027](../027-chat-bubble-game-state/spec.md) loss-first precedence preserved — US2 acceptance 5).
- **First-move / fresh board**: an all-`INITIAL` board has `INITIAL` cells and the counter reads the full mine count (e.g. `010`), so it is not a win (grid rule fails and counter ≠ 000 — both consistent).
- **Stateful `SaoleiBoard` updates**: the counter is re-read on every screenshot (the same recognition pass that re-reads the grid), so a win is detected on the screenshot whose counter first reaches `000` together with a fully-revealed grid — symmetric with how the grid is re-recognized each update. The monotonic state-compatibility check (`validate.ts` `checkCompatible`) continues to apply to the grid; the counter is not part of that monotonic check (the counter may legitimately move up/down as flags are placed/removed within one game).
- **CLI `--debug` calibration**: the decoded counter (per-digit decode + the decided value) is surfaced in the diagnostics path so an operator can tune the digit-decode thresholds against real screenshots, exactly as per-cell diagnostics are tuned today (library README calibration flow).
- **Downstream contract preservation**: the MCP `game status:` line still reads `won`/`lost`/`playing`, the terminal reasons stay `game_won`/`game_over`, and the single-text-block contract ([025 FR-012](../025-desktop-image-state-refine/spec.md)) is unchanged. The only behavioral change is that some boards formerly reported `won` are now correctly reported `playing`.

## Requirements *(mandatory)*

### Mine-counter recognition (US1)

- **FR-001**: `recognizeBoard` MUST decode the top-left mine counter from the screenshot. The counter occupies screenshot-space pixels **X = 32..113, Y = 120..169** (82 × 50 px; operator-measured, stated in the user input). The decode MUST read the 3-digit 7-segment red-LED value displayed there.
- **FR-002**: The decoded mine-counter value MUST be part of the recognition output (produced by the same recognition pass that produces the cell grid) and MUST be available to the win predicate, to the CLI `--debug` path, and to library tests.
- **FR-003**: When the counter cannot be decoded confidently, recognition MUST report an explicit "undecodable" status (it MUST NOT fabricate a digit value). Digit-decode confidence thresholds live in the color/geometry profile (peer of `ColorProfile`/`BoardGeometry`) and are tunable via the calibration flow.
- **FR-004**: The counter decode MUST be robust to the classic LED's anti-aliased red-on-black rendering and MUST correctly yield `000` for a "flags == mines" display, the displayed non-zero value otherwise, and a **negative** value (leading minus sign) when the player over-flags — verified against the three fixtures: `saolei_9` ⇒ `-01` (non-zero), `saolei_10` ⇒ `000`, `saolei_11` ⇒ `000`. The leading minus sign MUST be recognized as a sign, not misread as a digit.

### Counter-informed win predicate (US2)

- **FR-005**: The win predicate MUST return `true` **only when both** (a) the recognized grid contains no `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` cell (every cell is a revealed number `0..8` or `FLAG`), **and** (b) the decoded mine counter reads exactly `000`. (Supersedes the grid-only [027 FR-009](../027-chat-bubble-game-state/spec.md); the grid condition is retained as one half of the conjunction.)
- **FR-006**: The win predicate MUST return `false` when the grid condition (a) holds but the counter is non-zero (flags ≠ mines — including the negative over-flagged counter `-01`) — the `saolei_9` false-positive case.
- **FR-007**: The win predicate MUST return `false` when the counter reads `000` but the grid contains any `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` cell — the `saolei_11` case (counter-alone is not a win).
- **FR-008**: The win predicate MUST return `false` when the counter is undecodable (lenient — never claim a win on an uncertain counter), even if the grid looks fully revealed/flagged.
- **FR-009**: The win predicate MUST remain a pure function of its inputs (the recognized grid and the decoded counter) — no I/O, no mutation, no side effects ([027 FR-011](../027-chat-bubble-game-state/spec.md) preserved) — exported from the library's public barrel (`projects/game/pkg/saolei-board/src/core/index.ts`).
- **FR-010**: A board with any `HIT_MINE`/`MINE` cell MUST yield `false` from the win predicate regardless of the counter (loss is not a win; loss precedence over win is unchanged — [027 FR-010](../027-chat-bubble-game-state/spec.md)).

### Fixtures & downstream (US1/US2)

- **FR-011**: The two badcase fixtures `testdata/saolei_9.png` and `testdata/saolei_11.png` MUST be added to the golden test suite as win-classification regression cases: `saolei_9` (grid all-revealed, counter non-zero) asserts `isWin` ⇒ `false`; `saolei_11` (counter `000`, grid with `INITIAL`) asserts `isWin` ⇒ `false`; and the existing genuine-win case `saolei_10` continues to assert `isWin` ⇒ `true`. (These two PNGs are already present under `testdata/` as untracked files; this requirement covers their golden/classification assertions.)
- **FR-012**: The saolei MCP consumption site (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `gameStatus` and `validateMove`'s `game_won` check) MUST evaluate the strengthened predicate against the recognized board (grid + decoded counter). The MCP text-result contract — the `game status: won|lost|playing` line ([027 FR-012..015](../027-chat-bubble-game-state/spec.md)) and the `game_won` rejection body ([027 FR-021..023](../027-chat-bubble-game-state/spec.md)) — MUST remain unchanged in wording and shape; only the `won` decision becomes counter-informed.
- **FR-013**: The library README (`projects/game/pkg/saolei-board/README.md`) and the built-in saolei skill (`projects/game/agent/src/skill/saolei/SKILL.md`) MUST be updated to document the mine-counter cross-check as the win condition (grid fully revealed/flagged **and** counter `000`), so the model and operators read the corrected rule. (The skill's authority on the result format per [027 FR-024](../027-chat-bubble-game-state/spec.md) is preserved.)

### Key Entities *(include if feature involves data)*

- **Mine counter (decoded)**: the integer value (or "undecodable") read from the top-left 3-digit red LED at screenshot-space X 32..113, Y 120..169. Represents the game's `mines − flags` display — the ground-truth signal that `flags == mines` exactly when it reads `000`. Produced by recognition alongside the cell grid; an input to the win predicate.
- **Counter-informed win predicate**: the strengthened `isWin` — a board is a win iff the grid is fully revealed/flagged (no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`) **and** the counter reads `000`. The single source of truth for "is this recognized board a win", supersedes the grid-only predicate from [027](../027-chat-bubble-game-state/spec.md) US3.
- **Counter decode profile**: the tunable digit-decode thresholds (7-segment segment-detection / red-pixel / geometry constants), a peer of `ColorProfile` and `BoardGeometry`, tuned via the CLI `--debug` calibration flow against real screenshots.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of cases, the mine-counter decoder yields the correct value for the three fixtures — **`-01`** (non-zero, leading minus sign) for `saolei_9`, `000` for `saolei_10`, `000` for `saolei_11` — verified by the library unit/golden suite.
- **SC-002**: In 100% of cases, the strengthened win predicate returns `false` for `saolei_9` (grid all-revealed, counter `-01` — over-flagged) — the false-positive fix — verified by the golden win-classification assertions.
- **SC-003**: In 100% of cases, the strengthened win predicate returns `false` for `saolei_11` (counter `000`, grid with `INITIAL`) and `true` for `saolei_10` (both conditions hold) — the conjunction is correct in both directions.
- **SC-004**: In 100% of cases where the recognized board is not actually won, the saolei MCP no longer surfaces `game status: won` nor rejects subsequent cell operations as `game_won` (the downstream symptom of the bug is eliminated), verified by the MCP unit tests and the agent large test.
- **SC-005**: In 100% of cases where the counter is undecodable, the win predicate returns `false` (no win is claimed on an uncertain counter read) — the lenient guarantee.

## Assumptions

- The classic Win32 Microsoft Minesweeper top-left LED displays `mines − flags` as a 3-digit 7-segment red-on-black display, reading `000` exactly when the flag count equals the mine count. This is the rule the counter cross-check encodes; it is the standard behaviour of `winmine.exe` (the target the library recognises per its README) and is consistent with the decoded values of the three fixtures.
- The counter region geometry is operator-measured and fixed for the target window layout: screenshot-space X = 32..113, Y = 120..169 (82 × 50 px), stated verbatim in the user input and consistent with the decoded red-LED band in the fixtures (red at X 38..111, Y 126..167). Per-digit sub-rects (three ≈22 px-wide digits with inter-digit gaps) are a plan-time calibration detail within this bounding box.
- The win rule is the **conjunction** of (grid fully revealed/flagged) and (counter `000`); neither half alone is a win. This is forced by the two badcases (`saolei_9` ⇒ grid-alone insufficient; `saolei_11` ⇒ counter-alone insufficient) and is non-negotiable for the fix.
- Recognition is lenient on counter uncertainty, mirroring the library's `UNKNOWN`-cell leniency ([027 FR-010](../027-chat-bubble-game-state/spec.md)) and [025 FR-018](../025-desktop-image-state-refine/spec.md)): an undecodable counter ⇒ not a win (false negative preferred over false positive). The exact decode-confidence criterion is a plan-time detail.
- The counter is read once per screenshot, in the same recognition pass that produces the cell grid. The exact API shape by which the decoded counter flows from recognition to `isWin` (a field on a recognized-board object, a field added to the recognized state, or a parameter) is a plan-time interface decision (Constitution §III) constrained by: `isWin` stays pure (FR-009); the [027] MCP text contract is preserved (FR-012); and the stateful `SaoleiBoard` re-reads the counter on each update (the counter is not part of the monotonic `checkCompatible` grid check — it may move freely as flags change within one game).
- The top-right timer LED and the top-center smiley are out of scope; only the left mine counter is decoded.
- The two badcase PNGs (`saolei_9.png`, `saolei_11.png`) are already present under `testdata/` as untracked files; FR-011 covers adding their golden/classification assertions (and, if the calibration flow requires, their `.golden.txt` board outputs). The genuine-win fixture `saolei_10.png` and its golden already exist.
- The downstream surfaces are confined to the library `core/` and the single `isWin` consumption site in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`; no proto change, no new external dependency. The agent service remains the large-test SUT (Constitution principle VI); the library change is verified by its unit + golden suite, the agent change by the MCP unit test and the agent large test.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `projects/game/pkg/saolei-board/src/core/win.ts` — the current grid-only `isWin` predicate (FR-009..011 of [027](../027-chat-bubble-game-state/spec.md)); the defect site to be strengthened (FR-005..010).
- `projects/game/pkg/saolei-board/src/core/recognize.ts` — `recognizeBoard` / `SaoleiBoard`; the recognition pass that MUST additionally decode the counter (FR-001/FR-002) and re-read it on each `updateFromScreenshot`.
- `projects/game/pkg/saolei-board/src/core/geometry.ts` — `DEFAULT_GEOMETRY` / screenshot-space layout helpers; the counter region (X 32..113, Y 120..169) is a peer screenshot-space constant.
- `projects/game/pkg/saolei-board/src/core/decode.ts` — `decodePng`, `getRGB`, `extractCellRegion`; the raw-pixel access the counter decoder consumes (same decoded image as the cell classifier).
- `projects/game/pkg/saolei-board/src/core/classify.ts` — the per-cell color-analysis profile/pipeline; the counter digit decoder is its structural peer (fixed-geometry + red-pixel analysis, no OCR).
- `projects/game/pkg/saolei-board/src/core/types.ts` — `GameState`, `CellStatus`, `ColorProfile`, `BoardGeometry`; the recognized-state and profile types the counter output joins.
- `projects/game/pkg/saolei-board/src/core/index.ts` — the public barrel; exports the counter decode and the strengthened predicate.
- `projects/game/pkg/saolei-board/src/core/golden.test.ts` — the golden test harness (loops `testdata/saolei_N` + the `saolei_10` win assertion); FR-011 extends it with `saolei_9` / `saolei_11` win-classification assertions.
- `projects/game/pkg/saolei-board/testdata/saolei_9.png`, `saolei_10.png`, `saolei_11.png` — the three fixtures defining the win boundary (9: grid all-revealed + counter non-zero ⇒ not won; 10: both ⇒ won; 11: counter `000` + `INITIAL` ⇒ not won).
- `projects/game/pkg/saolei-board/README.md` — the library's public contract, symbol legend, and calibration flow; FR-013 updates the win-condition documentation.
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — `gameStatus` (uses `isWin`), `validateMove`'s `game_won` check (uses `isWin`), `isTerminalState`; the consumption site that MUST pass the decoded counter to the strengthened predicate (FR-012), preserving the [027] text contract.
- `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` — the DI-based MCP test pattern; extends to assert non-won boards are no longer reported `won` (SC-004).
- `projects/game/agent/src/skill/saolei/SKILL.md` — the built-in saolei skill; FR-013 updates the documented win condition.
- `projects/game/agent/src/mcp/saolei/geometry.ts` — documents the screenshot↔client chrome offset (96 px); the counter region is in **screenshot** space (full window), consistent with the board's screenshot-space `originYPx = 200`.
- `specs/027-chat-bubble-game-state/spec.md` — the [027] feature that introduced the grid-only `isWin` (FR-009..011), the `game status:` line (FR-012..015), and the `game_won` terminal rejection (FR-021..023); this feature strengthens FR-009 and preserves the rest.
- `specs/025-desktop-image-state-refine/spec.md` — the recognized-text-state + strict-validation design; FR-012 (single text block) and FR-018 (lenient on `UNKNOWN`) are preserved.
- `specs/018-saolei-mcp/research.md` — the screenshot-space board geometry (D6: board top Y=200, X=24, cell 32) that the counter region sits above.

### External

- Classic Microsoft Minesweeper (`winmine.exe`) mine-counter behaviour — the top-left 3-digit red LED displays `mines − flags`, reading `000` exactly when the flag count equals the mine count (and a negative/overflow display when over-flagged). This is the ground-truth signal the counter cross-check encodes; the library's existing recognition targets this implementation (per its README). No single normative external document is newly authoritative; the rule is common knowledge for the game and is already the basis of the library's `FLAG`/`HIT_MINE`/`MINE` semantics.
- Classic minesweeper number/LED palette reference (community) — https://online.games.narkive.com/FUc9B1QB/colors-in-minesweeper — already cited by the library's `classify.ts`/`types.ts` for the digit colours; the counter's red-on-black LED uses the same classic red.
