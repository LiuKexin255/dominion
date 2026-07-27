# Data Model: Chat Bubble UX Polish & Saolei Game-State Awareness

**Feature**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

This document fixes the entity shapes and value domains introduced or modified by this feature. It is the authoritative source for the structures referenced by the contracts ([saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md), [desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md)) and the tasks. No proto message is added or changed (spec Relationship: no proto change); all new structures are TypeScript/agent-internal or Svelte-internal.

---

## §1 — Library: win predicate (US3 / FR-009..011)

**Location**: `projects/game/pkg/saolei-board/src/core/win.ts` (new file); exported via `projects/game/pkg/saolei-board/src/core/index.ts`.

```ts
import type { GameState, CellStatus } from "./types";

/** Cell statuses that are NOT compatible with a win — i.e. a board containing
 *  any of these is not won (FR-009/FR-010). */
const NON_WIN_CELLS: ReadonlySet<CellStatus> = new Set<CellStatus>([
  "INITIAL",   // an unrevealed cell ⇒ game still in progress
  "HIT_MINE",  // a triggered mine ⇒ a loss
  "MINE",      // an end-game revealed mine ⇒ a loss
  "UNKNOWN",   // recognition uncertain ⇒ lenient (do not claim a win)
]);

/**
 * Pure win classifier (FR-009..011). Returns true iff NO cell is INITIAL,
 * HIT_MINE, MINE, or UNKNOWN — i.e. every cell is a revealed number ("0".."8")
 * or FLAG (the classic Minesweeper win condition: all non-mine cells revealed
 * and all mines auto-flagged). Single short-circuiting pass over state.grid.
 */
export function isWin(state: GameState): boolean {
  for (const row of state.grid) {
    for (const cell of row) {
      if (NON_WIN_CELLS.has(cell)) return false;
    }
  }
  return true;
}
```

**Domain rules** (FR-009/FR-010):

| Board contains… | `isWin` returns | Status the agent derives (§3) |
|---|---|---|
| only `"0"`..`"8"` and `"FLAG"` | `true` | `won` |
| any `"INITIAL"` | `false` | `playing` (unless also terminal-loss) |
| any `"HIT_MINE"` or `"MINE"` | `false` | `lost` (loss takes precedence over the win check — §3) |
| any `"UNKNOWN"` | `false` | `playing` (lenient — do not claim a win on an uncertain board) |

**Golden fixture**: `testdata/saolei_10.png` (real 9×9 win board, user-supplied) is recognized as the grid below; `isWin` MUST return `true` on it (asserted in the golden test). Generated golden text `testdata/saolei_10.golden.txt`:

```
board size 9*9

0 0 0 1 3 F F F F
0 0 0 1 F F 4 3 2
0 0 0 1 2 2 1 0 0
0 0 0 0 0 0 0 0 0
0 0 0 0 0 0 0 0 0
0 0 0 1 2 2 1 0 0
0 1 1 2 F F 2 0 0
0 1 F 2 3 F 2 0 0
0 1 1 1 1 1 1 0 0
```

**Purity**: `isWin` is a pure function of `GameState` — no I/O, no mutation, no side effects (FR-011). It does not import the recognition engine; it reasons over an already-recognized `GameState`.

---

## §2 — Agent: `MoveRejection` union update (US4/US5 / FR-019, FR-023)

**Location**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (the existing `MoveRejection` union).

Current (025):

```ts
export type MoveRejection =
  | "no_active_game"
  | "out_of_bounds"
  | "game_over"
  | "cell_already_revealed"
  | "cell_is_flagged"
  | "cannot_flag_revealed"
  | "chord_requires_number";
```

**After this feature** (two new members added):

```ts
export type MoveRejection =
  | "no_active_game"
  | "out_of_bounds"
  | "game_over"                     // existing: terminal LOSS (HIT_MINE/MINE present)
  | "game_won"                      // NEW (FR-023): terminal WIN (isWin, post-win cell op)
  | "cell_already_revealed"
  | "cell_is_flagged"
  | "cannot_flag_revealed"
  | "chord_requires_number"
  | "chord_no_unrevealed_neighbor"; // NEW (FR-019): chord target has no INITIAL/UNKNOWN non-flag neighbor
```

| New reason | Produced by | Meaning (surfaced verbatim to the model) |
|---|---|---|
| `game_won` | `validateMove` — terminal check, after the loss check (`isWin(state)` true) | The current game is already won; any further cell operation is rejected. Call `saolei_init` to restart. |
| `chord_no_unrevealed_neighbor` | `validateMove` — `saolei_chord_click` branch, after `chord_requires_number` passes | The chord target's non-flag neighbors are all revealed/mines — there is no `INITIAL` cell to reveal (and no `UNKNOWN` to be lenient about); the chord would be a no-op. |

The two terminal reasons (`game_over`, `game_won`) are mutually exclusive for a given board (a board with `HIT_MINE`/`MINE` is a loss, and `isWin` returns false for it). See §3 for the precedence rule.

---

## §3 — Agent: game-status derivation (US4 / FR-012..015)

**Location**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (new internal helper).

```ts
import { isWin } from "@dominion/game-saolei-board";

/** The game-status token emitted in every tool-result body (FR-012..015). */
type GameStatus = "won" | "lost" | "playing";

/** Derive the status from a recognized state. Loss takes precedence over win
 *  (a board with HIT_MINE/MINE is a loss; isWin returns false for it anyway,
 *  but loss-first is explicit — §4 precedence). Pure function of state. */
function gameStatus(state: GameState): GameStatus {
  if (isTerminalState(state)) return "lost";   // existing loss signal (HIT_MINE/MINE)
  if (isWin(state)) return "won";              // US3 predicate (library)
  return "playing";
}
```

| `isTerminalState(state)` (loss) | `isWin(state)` (win) | `gameStatus` | Note |
|---|---|---|---|
| `true` | (don't care — will be `false`) | `"lost"` | loss takes precedence |
| `false` | `true` | `"won"` | terminal-win |
| `false` | `false` | `"playing"` | in-progress (or uncertain — `UNKNOWN` cells keep `isWin` false) |

**Status-line emission**: every text-result builder appends a line `game status: <status>` to the body, positioned **after the outcome/rejection line and before the text board** (D7 / contract §2). When no recognized state exists (`no_active_game` rejection, `unable to recognize board` outcome), the line is **omitted** (FR-015 — no fabricated status).

---

## §4 — Agent: `validateMove` rule order (US4/US5 / FR-016..023)

**Location**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` `validateMove`.

The check order (structural → state-level → cell-specific), with the two new rules placed per FR-018 (after the existing rules of their kind):

```
1. out_of_bounds            (x,y outside state dims)              — existing, unchanged
2. game_over                (isTerminalState — loss: HIT_MINE/MINE) — existing, unchanged
3. game_won                 (isWin — win)                          — NEW (FR-021..023)
4. cell-specific (per tool):
   saolei_click:      cell_already_revealed | cell_is_flagged     — existing
   saolei_flag:       cannot_flag_revealed                        — existing
   saolei_chord_click:
     a. chord_requires_number (target not 1..8)                   — existing, unchanged
     b. chord_no_unrevealed_neighbor                              — NEW (FR-016..020)
```

**Terminal precedence**: step 2 (loss) runs before step 3 (win). They are mutually exclusive (a loss board has `HIT_MINE`/`MINE` ⇒ `isWin` is `false`), so exactly one fires; loss-first makes the "loss takes precedence" edge case explicit (spec Edge Cases).

**Chord-neighbor helper** (new, pure):

```ts
/** The 8 Moore-neighbor offsets, bounded by the board dims at call time. */
const NEIGHBOR_OFFSETS: ReadonlyArray<readonly [number, number]> = [
  [-1, -1], [0, -1], [1, -1],
  [-1,  0],          [1,  0],
  [-1,  1], [0,  1], [1,  1],
];

/** In-bounds Moore neighbors of (x, y). */
function neighbors(state: GameState, x: number, y: number): CellStatus[] {
  const out: CellStatus[] = [];
  for (const [dx, dy] of NEIGHBOR_OFFSETS) {
    const nx = x + dx, ny = y + dy;
    if (nx >= 0 && ny >= 0 && nx < state.width && ny < state.height) {
      out.push(state.grid[ny][nx]);
    }
  }
  return out;
}

/** True iff some in-bounds neighbor is INITIAL or UNKNOWN. FLAG / number /
 *  HIT_MINE / MINE neighbors do NOT count (a chord acts only on INITIAL cells
 *  and is lenient on UNKNOWN per 025 FR-018). */
function hasInitialOrUnknownNeighbor(state: GameState, x: number, y: number): boolean {
  return neighbors(state, x, y).some((c) => c === "INITIAL" || c === "UNKNOWN");
}
```

The new chord branch (after `chord_requires_number` passes):

```ts
case "saolei_chord_click":
  if (!CHORD_NUMBERS.has(cell)) {
    return { ok: false, reason: "chord_requires_number" };      // existing
  }
  if (!hasInitialOrUnknownNeighbor(state, x, y)) {
    return { ok: false, reason: "chord_no_unrevealed_neighbor" }; // NEW (FR-016..020)
  }
  return { ok: true };
```

**Edge/corner cells**: `neighbors()` intersects the 8 offsets with `[0,width)×[0,height)` — an edge cell yields 5 neighbors, a corner 3. The rejection applies to the in-bounds non-flag neighbors only (spec Edge Cases).

---

## §5 — Agent: text-result body format (US4 / FR-012..015)

**Location**: `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — the four builders (`initSuccessText`, `dispatchedText`, `rejectionText`, `unrecognizableText`).

The body is a single MCP **text** content block (025 FR-012 preserved — no new content-block kind). The status line is inserted at a fixed position. Grammar:

```
body        ::= outcome-line LF "game status: " status LF LF board   ; when state exists
             | outcome-line LF LF guidance                           ; when no state (no status line)

outcome-line ::= "new game started"                                  ; initSuccessText
               | <tool> " at (" x "," y ") → dispatched"             ; dispatchedText
               | "rejected: " <reason>                               ; rejectionText
               | "unable to recognize board"                         ; unrecognizableText

status      ::= "won" | "lost" | "playing"

board       ::= renderBoardText(state) LF "valid range: x 0.." w-1 ", y 0.." h-1   ; rejectionText (has state)
             | renderBoardText(state)                                              ; init/dispatched

guidance    ::= "call saolei_init first to start a game."          ; no_active_game
             | "call saolei_init to start a new game."             ; unable to recognize
```

Worked examples — see [contracts/saolei-mcp-status-contract.md §2](./contracts/saolei-mcp-status-contract.md) for the full table.

**Single-text-block invariant**: the entire body is the `text` field of one `{ type: "text", text: body }` MCP content block (025 FR-012). The status line MUST NOT be a separate content block.

---

## §6 — Desktop: think-bubble render state (US1 / FR-001..004)

**Location**: `projects/game/desktop/frontend/src/components/ChatMessage.svelte`.

Local reactive state (Svelte 5 runes), owned per bubble:

```ts
let expanded = $state(false);          // existing — the collapse toggle
let contentEl: HTMLPreElement | undefined = $state();   // bind:this on .thinking-content
```

Auto-scroll behaviour (two `$effect`s — D2):

- **Open-to-bottom** (FR-004): when `expanded` flips false→true, set `contentEl.scrollTop = contentEl.scrollHeight` on `requestAnimationFrame`.
- **Follow-or-pause** (FR-002/FR-003): when `expanded` is true and `part.thinking.content` grows, compute `atBottom = scrollTop + clientHeight >= scrollHeight − TOLERANCE` (`TOLERANCE = 8` px); if `atBottom`, scroll to bottom on `requestAnimationFrame`; else do nothing (pause while the operator is scrolled up).

CSS (D1): the existing `.thinking-content` rule (`max-height: 200px; overflow-y: auto`) gains:

```css
.thinking-content { scrollbar-width: none; }              /* Firefox / standard */
.thinking-content::-webkit-scrollbar { display: none; }   /* WebKit / Chromium (Wails WebView2) */
```

Scroll capability is preserved (`overflow-y: auto` unchanged); only the visible scrollbar track/thumb is suppressed (FR-001).

---

## §7 — Desktop: tool-bubble render shapes (US2 / FR-005..008)

**Location**: `projects/game/desktop/frontend/src/components/ChatView.svelte`.

**Compact args renderer** (D3, replaces `prettyArgs`):

```ts
function compactArgs(argsJson?: string): string {
  if (!argsJson) return "";
  try { return JSON.stringify(JSON.parse(argsJson)); }   // compact, no indent
  catch { return argsJson; }                              // invalid JSON → raw string
}
```

Rendered inline: `<span class="tool-name">{name}</span> <code class="tool-args-inline">{compactArgs}</code>` (the `.tool-args` `<pre>` block is removed; FR-005).

**Result body structure** (D5, the resolved case — pending "running…" stays outside):

```
.tool-bubble(.tool-resolved-*)
  .tool-head
    span.tool-name
    code.tool-args-inline                 ; compact args (FR-005)
  details.tool-result-details             ; collapsed by default (no `open` attr) — FR-007
    summary
      span.op-result-icon                 ; ✓ / ✗ / ›
      span.op-result-status               ; succeeded / failed / done
    pre.op-result-message                 ; the message, white-space: pre-wrap (FR-006)
    details.op-result-screenshot-details  ; existing screenshot sub-toggle (unchanged) — FR-008
      summary
      img.screenshot-img
```

**Default-collapsed**: the outer `<details>` has no `open` attribute → the status icon + label (in `<summary>`) are always visible; the message + screenshot are hidden until expanded (FR-007). The screenshot sub-`<details>` is independently collapsible (FR-008).

**Message formatting** (D4, FR-006): `.op-result-message` is a `<pre>` with `white-space: pre-wrap; word-break: break-word;` so the saolei text board's newlines are preserved and long lines wrap.

---

## Entity summary

| Entity | Kind | Location | New/Changed |
|---|---|---|---|
| `isWin(state)` | pure predicate (`GameState → boolean`) | `pkg/saolei-board/src/core/win.ts` | NEW (US3) |
| `NON_WIN_CELLS` | `ReadonlySet<CellStatus>` | `win.ts` | NEW |
| `MoveRejection` | TS union (string literals) | `agent/src/mcp/saolei/saolei-mcp.ts` | +`"game_won"`, +`"chord_no_unrevealed_neighbor"` (US4/US5) |
| `gameStatus(state)` | pure helper (`GameState → "won"\|"lost"\|"playing"`) | `saolei-mcp.ts` | NEW (US4) |
| `hasInitialOrUnknownNeighbor(state,x,y)` / `neighbors(state,x,y)` | pure helpers | `saolei-mcp.ts` | NEW (US5) |
| text-result body | MCP text content block (`{type:"text", text}`) | `saolei-mcp.ts` builders | +`game status:` line (US4) |
| `expanded`, `contentEl` | Svelte `$state` | `ChatMessage.svelte` | `contentEl` NEW (US1) |
| `.thinking-content` scrollbar CSS | CSS rules | `ChatMessage.svelte` `<style>` | NEW (US1) |
| `compactArgs(argsJson)` | pure helper | `ChatView.svelte` | NEW (replaces `prettyArgs`, US2) |
| tool-result `<details>` structure | template + CSS | `ChatView.svelte` | NEW (US2) |
| `saolei_10.png` / `saolei_10.golden.txt` | testdata fixtures | `pkg/saolei-board/testdata/` | NEW (US3 golden win case) |
