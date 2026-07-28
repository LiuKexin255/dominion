# Research: Chat Bubble UX Polish & Saolei Game-State Awareness

**Feature**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

Phase 0 research resolves every plan-time unknown in [plan.md](./plan.md) Technical Context. Each decision records what was chosen, why, and what alternatives were rejected. All decisions are grounded in the actual code (read during research) and cited external references.

The spec already resolved the six item-level decisions during `/speckit.specify` (recorded in spec.md `## Clarifications`) and the post-win-blocking question during `/speckit.clarify` (Session 2026-07-27). The decisions below (D1..D12) resolve the **implementation-level** unknowns deferred to planning — the exact CSS rule, the reactive wiring, the function names, the text-line position, the reason-code spelling, the validator refactor shape, the test split.

---

## D1 — Hidden-scrollbar CSS technique (US1 / FR-001)

**Decision**: Scope a scrollbar-hiding CSS rule to `.thinking-content` in `projects/game/desktop/frontend/src/components/ChatMessage.svelte`:

```css
.thinking-content {
  /* existing: max-height, overflow-y: auto, … */
  scrollbar-width: none;                /* Firefox / standard */
}
.thinking-content::-webkit-scrollbar {
  display: none;                        /* WebKit / Chromium (Wails v2 WebView2) */
}
```

The area keeps `overflow-y: auto` and its `max-height: 200px` — only the visible scrollbar track/thumb is suppressed; wheel/trackpad/keyboard scrolling still works.

**Rationale**: The Wails v2 Windows webview is WebView2 (Chromium-based), so the `::-webkit-scrollbar { display: none }` rule is the operative one on the target platform; `scrollbar-width: none` is the standard (CSS Scrollbars Styling Module Level 1) fallback that makes the rule portable to Firefox should the webview change. Both rules together are the canonical cross-browser "hide scrollbar, keep scrolling" technique (MDN: https://developer.mozilla.org/en-US/docs/Web/CSS/scrollbar-width; MDN `::-webkit-scrollbar`: https://developer.mozilla.org/en-US/docs/Web/CSS/::-webkit-scrollbar). Pure CSS — no JS, no layout change, no new dependency.

**Alternatives rejected**:
- *JS-managed custom scrollbar* (e.g. overlay a thin div) — over-engineering; the requirement is to *hide* the native scrollbar, not to render a custom one.
- *`overflow: hidden`* — removes scroll capability entirely (violates FR-001 "the area MUST remain scrollable").
- *Reduce content to fit / truncate* — does not address long reasoning streams; defeats the bubble's purpose.

---

## D2 — Think-bubble auto-scroll reactive wiring (US1 / FR-002..004)

**Decision**: In `ChatMessage.svelte`, bind the `.thinking-content` `<pre>` to a local `$state` element ref (`bind:this={contentEl}`). Two reactive hooks, split by job:

1. **Open-to-bottom** — a regular `$effect` keyed on `expanded`: when `expanded` flips false→true, unconditionally set `contentEl.scrollTop = contentEl.scrollHeight` on `requestAnimationFrame` (FR-004: open scrolled to the bottom). The `<pre>` is mounted during the preceding DOM-update phase (`{#if expanded}`), so `contentEl` is bound by the time the effect runs; no at-bottom check is needed.
2. **Follow-or-pause** — an `$effect.pre` keyed on `part.thinking.content` (a reactive prop): when `expanded` is true and the content grows, evaluate `atBottom = contentEl.scrollTop + contentEl.clientHeight >= contentEl.scrollHeight − TOLERANCE` **before** the DOM update; if `atBottom`, scroll to bottom inside `tick().then(...)` (FR-002: follow the stream); if not `atBottom`, do nothing (FR-003: pause while the operator is scrolled up).

`TOLERANCE` is a small constant (8 px) to absorb sub-pixel float jitter so "at the bottom" is stable.

**Rationale**: The at-bottom guard (`scrollTop + clientHeight >= scrollHeight − tol`) is the standard chat-auto-scroll-with-pause pattern (used by Slack, Discord, terminal viewers). But the guard's correctness depends on **when** `scrollHeight` is read relative to the DOM update that appends the new reasoning:

- A regular `$effect` runs *after* the DOM update. By then `scrollHeight` already reflects the newly-appended reasoning while `scrollTop` is still the operator's pre-update position, so `scrollTop + clientHeight >= scrollHeight − TOLERANCE` is false on every content growth — the bubble freezes at the top and never follows the stream. This was the original implementation (commit `eb99d27`) and is the bug confirmed in practice: the operator opens the bubble at the bottom (the open-to-bottom `$effect` works because it has no at-bottom check), but as more reasoning streams in the view stays pinned and the operator must scroll manually.
- `$effect.pre` runs *before* the DOM update, so `scrollHeight` is the height the operator currently sees and `scrollTop` is the operator's true position — the at-bottom test is meaningful. `tick().then(...)` then performs the scroll after the DOM update lands the new bottom. This is the Svelte autoscroll pattern documented at https://svelte.dev/docs/svelte/$effect#$effect.pre (the canonical example reads `messages.length` for dependency tracking and gates the `tick().then(...)` scroll on an at-bottom test computed from the pre-update geometry).

The open-to-bottom hook keeps the regular `$effect` + `requestAnimationFrame` form (it has no at-bottom check, so the DOM-update timing is harmless — it unconditionally jumps to the bottom regardless of when `scrollHeight` is read; `requestAnimationFrame` defers one paint so layout is final). Splitting the two jobs (`$effect` for open, `$effect.pre` for follow) keeps each hook single-purpose.

The "did `expanded` just flip?" detection: track the previous `expanded` value in a `$state`/closure and compare inside one combined hook, OR split into two hooks — one keyed on `expanded` (open-to-bottom), one keyed on `part.thinking.content` (follow-or-pause). The two-hook split is cleaner (each has one job); chosen.

**Alternatives rejected**:
- *Always scroll to bottom on content change* — violates FR-003 (must pause when the operator scrolls up).
- *Svelte action `use:autoscroll`* — adds indirection; inline hooks match the existing ChatView pattern and keeps the scroll logic local to the component that owns the element.
- *`IntersectionObserver` on a sentinel* — over-engineering; the `scrollTop`/`scrollHeight` comparison is simpler, dependency-free, and matches the codebase idiom.
- *Follow-or-pause as a regular `$effect` + `requestAnimationFrame` (the original `eb99d27` implementation)* — **rejected, confirmed broken in practice**: `$effect` runs after the DOM update, so `scrollHeight` already includes the new reasoning and the at-bottom test is false on every growth; the bubble never follows the stream. Switched to `$effect.pre` + `tick().then(...)`.

---

## D3 — Compact tool-args rendering (US2 / FR-005)

**Decision**: In `projects/game/desktop/frontend/src/components/ChatView.svelte`, replace the `prettyArgs(argsJson)` helper (currently `JSON.stringify(JSON.parse(argsJson), null, 2)` — the 2-space pretty-print that blows `{"x":7,"y":7}` up to 4 lines) with a compact renderer:

```ts
function compactArgs(argsJson?: string): string {
  if (!argsJson) return "";
  try {
    return JSON.stringify(JSON.parse(argsJson));   // compact, no indent
  } catch {
    return argsJson;                                // invalid JSON → raw string
  }
}
```

Render the result **inline** with the tool name (not in a separate multi-line `<pre>`), e.g. `<span class="tool-name">saolei_flag</span> <code class="tool-args-inline">{"x":7,"y":7}</code>`. Restyle the existing `.tool-args` rule (a `<pre>`) into an inline `.tool-args-inline` (a `<code>`) so the args sit on the same line as the name.

**Rationale**: The model emits compact JSON in `tool_call.args`; pretty-printing it was the bug. `JSON.stringify(JSON.parse(argsJson))` (no indent argument) normalizes any incidental whitespace to a single line and is safe — the `try/catch` falls back to the raw string for invalid JSON (FR-005). Inline rendering (name + compact args on one row) matches the user's desired `saolei_flag {"x":7,"y":7}` shape and removes the 4-line waste. The `<code>` element gives monospace inline without forcing a block.

**Alternatives rejected**:
- *Show the raw `argsJson` verbatim (no parse)* — fragile if upstream ever emits indented JSON (it would stay multi-line); the `JSON.parse` + compact `stringify` normalizes defensively.
- *Keep the `<pre>` but compact* — a `<pre>` is block-level; even compact it sits on its own line(s), defeating "on one line with the name".
- *Drop the args entirely* — loses information the operator wants at a glance.

---

## D4 — Tool-result message formatting (US2 / FR-006)

**Decision**: Render the result message in an element with `white-space: pre-wrap; word-break: break-word;` so the saolei text board's `\n`-delimited structure is preserved while long lines wrap. Concretely: change the existing `<span class="op-result-message">` (currently no `white-space` rule → newlines collapse) to a `<pre class="op-result-message">` (or add `white-space: pre-wrap` to the span's CSS). `<pre>` chosen for semantic correctness (the message is pre-formatted text).

**Rationale**: The saolei MCP returns a multi-line text board (`saolei_click at (4,4) → dispatched\n\nboard size 9*9\n\n* * * …`). A plain `<span>` collapses `\n` to a space (the "看不出来输出的格式" symptom). `white-space: pre-wrap` preserves newlines AND wraps overlong lines (unlike `pre`, which would horizontally scroll). `word-break: break-word` handles unbroken long tokens. This is the minimal change that makes the board readable.

**Alternatives rejected**:
- *`<pre>` without `pre-wrap`* — long lines would horizontally overflow the bubble.
- *Re-parse the board into an HTML table* — over-engineering; the text board is already model-readable; the operator just needs the newlines visible.

---

## D5 — Collapsible result body (US2 / FR-007/008)

**Decision**: Wrap the resolved tool-result body in a `<details>` element with no `open` attribute (collapsed by default). The `<summary>` holds the always-visible status icon + label (`✓ done` / `✗ failed` / `› done`); expanding reveals the formatted message (`<pre class="op-result-message">` per D4). The screenshot stays in its existing separate nested `<details class="op-result-details">` (unchanged — already collapsed). The pending state (`running…`) renders OUTSIDE the `<details>` (it shows when the result has not arrived yet — nothing to collapse).

Resulting structure (resolved case):

```html
<div class="tool-bubble …">
  <div class="tool-head"><span class="tool-name">…</span><code class="tool-args-inline">…</code></div>
  <details class="tool-result-details">                      <!-- collapsed by default -->
    <summary><span class="op-result-icon">✓</span><span class="op-result-status">done</span></summary>
    <pre class="op-result-message">…formatted board…</pre>    <!-- D4 -->
    <details class="op-result-screenshot-details">            <!-- existing screenshot sub-toggle, unchanged -->
      <summary>Result screenshot</summary>
      <img …/>
    </details>
  </details>
</div>
```

**Rationale**: HTML `<details>`/`<summary>` is the existing pattern in `ChatView.svelte` (the screenshot already uses `<details class="op-result-details">`), so the result-body toggle is consistent with it. Default-closed satisfies FR-007; the summary's icon+label is the always-visible part. The screenshot's own `<details>` stays nested and independent (FR-008: expanding one does not force the other). Native `<details>` needs no JS and is accessible by default (keyboard-toggleable, ARIA-exposed).

**Alternatives rejected**:
- *Svelte-managed toggle (`$state` boolean)* — more code for the same behaviour; native `<details>` is simpler and matches the existing screenshot toggle.
- *Collapse the whole `.tool-bubble` (including the call)* — the tool name + args (the call) should stay visible so the operator can see what was invoked without expanding; only the result body collapses.
- *Expand-by-default with a collapse toggle* — contradicts FR-007 (collapsed by default).

---

## D6 — Win predicate (US3 / FR-009..011)

**Decision**: Add a new pure function to `@dominion/game-saolei-board`:

- **File**: `projects/game/pkg/saolei-board/src/core/win.ts` (new file, mirrors the existing `validate.ts` pattern — pure predicates over `GameState` live in their own file).
- **Signature**: `export function isWin(state: GameState): boolean`.
- **Semantics**: returns `true` iff **no** cell is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` — i.e. every cell is a revealed number (`"0"`..`"8"`) or `FLAG`. Single pass over `state.grid`; short-circuits on the first disqualifying cell.
- **Export**: add `export { isWin } from "./win";` to the public barrel `projects/game/pkg/saolei-board/src/core/index.ts`.

**Rationale**: The win rule is a classification predicate over `GameState`, conceptually identical in kind to `checkCompatible`/`REVEALED` in `validate.ts` (pure functions of `GameState`). A dedicated `win.ts` keeps the rule isolated, independently unit-testable, and reusable beyond the MCP (e.g. the CLI could surface it later). The `index.ts` barrel export follows the established convention (every public core function is re-exported there). Lenient on `UNKNOWN` (returns `false` if any `UNKNOWN` cell) per FR-010 — a board the library is not sure about is not classified a win.

**Name choice**: `isWin` — short, predicate-style (`isX`), reads naturally (`isWin(state)`), and parallels the agent's existing `isTerminalState` naming.

**Alternatives rejected**:
- *Add to `recognize.ts`* — that file is board-level orchestration (decode + segment + classify + assemble), not a home for standalone predicates.
- *Add to `classify.ts`* — that file is per-cell classification (one cell's pixels → one `CellStatus`); a board-level win check does not belong there.
- *Name `hasWon` / `isWinningBoard` / `isGameWon`* — longer without adding clarity; `isWin` matches the `isX` predicate convention.
- *Return a richer type (`"won" | "lost" | "playing"`)* — the library should not know about "lost" (that is the agent's existing `isTerminalState`/`HIT_MINE`-`MINE` signal); the library exports the single missing capability (win), and the agent composes win+loss into a status (D7). Separation of concerns.

---

## D7 — Game-status derivation + status-line position (US4 / FR-012..015)

**Decision**:

(a) **Derivation helper** (agent-side, in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`):

```ts
type GameStatus = "won" | "lost" | "playing";
function gameStatus(state: GameState): GameStatus {
  if (isTerminalState(state)) return "lost";   // existing loss signal (HIT_MINE/MINE)
  if (isWin(state)) return "won";              // US3 predicate (imported from the library)
  return "playing";
}
```

Loss is checked first. Loss and win are mutually exclusive (a board with `HIT_MINE`/`MINE` makes `isWin` return `false`), so the order is unambiguous, but loss-first is explicit and matches the "loss takes precedence" edge case (spec Edge Cases).

(b) **Status-line position** in the text outcome body: the line `game status: <won|lost|playing>` is emitted **immediately after the outcome/rejection line and immediately before the text board**. The existing outcome line and the text-board body are unchanged.

Worked examples (the `game status:` line is the new addition in each):

| Builder | Body (new line in **bold**) |
|---|---|
| `initSuccessText` | `new game started`<br>**`game status: playing`**<br><br>`board size 9*9` … |
| `dispatchedText` (in-progress) | `saolei_click at (4,4) → dispatched`<br>**`game status: playing`**<br><br>`board size 9*9` … |
| `dispatchedText` (won on this op) | `saolei_click at (4,4) → dispatched`<br>**`game status: won`**<br><br>`board size 9*9` … |
| `dispatchedText` (lost on this op) | `saolei_click at (4,4) → dispatched`<br>**`game status: lost`**<br><br>`board size 9*9` … |
| `rejectionText` (has state) | `rejected: cell_already_revealed`<br>**`game status: playing`**<br><br>`board size 9*9` …<br>`valid range: x 0..8, y 0..8` |
| `rejectionText` (no state — `no_active_game`) | `rejected: no_active_game`<br><br>`call saolei_init first to start a game.` *(no status line — no state)* |
| `unrecognizableText` | `unable to recognize board`<br><br>`call saolei_init to start a new game.` *(no status line — state invalidated)* |

**Rationale**: The derivation composes the library win predicate with the agent's existing loss signal — the library exports the single missing capability, the agent owns the status taxonomy (D6 separation of concerns). Loss-first matches the precedence edge case. The status-line position (after outcome, before board) gives the model a natural reading funnel: *what happened* (outcome) → *am I done?* (status) → *the details* (board) — the status is the most actionable token (it tells the model whether to stop), placed right after the outcome so the model reads it before parsing the board. The line is a stable, single-token, parseable format. The no-state cases (`no_active_game`, `unable to recognize`) omit the line (FR-015 — no fabricated status).

**Alternatives rejected**:
- *Status line before the outcome line* — the outcome line ("what happened") is the primary result; leading with status buries it. Also breaks the existing "outcome-line-first" format every consumer already parses.
- *Status line after the board* — the model would parse the whole board before learning it is done; the status is most useful *before* the board.
- *Return a richer MCP content block (structured)* — violates 025 FR-012 (single text block); the status line is part of the same text body.
- *Derive status in the library* — the library would need to know about "lost" (HIT_MINE/MINE), coupling it to the loss taxonomy; keep the library exporting only the win predicate.

---

## D8 — Post-win terminal handling refactor (US4 / FR-021..023)

**Decision**: In `validateMove` (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`), the existing terminal check is:

```ts
if (isTerminalState(state)) {
  return { ok: false, reason: "game_over" };
}
```

Extend it with a win check **immediately after** (loss-check first, win-check second — mutually exclusive but explicit):

```ts
if (isTerminalState(state)) {
  return { ok: false, reason: "game_over" };   // loss (existing, unchanged)
}
if (isWin(state)) {
  return { ok: false, reason: "game_won" };    // win (new, FR-021..023)
}
```

Add `"game_won"` to the `MoveRejection` union. Import `isWin` from `@dominion/game-saolei-board`. `isTerminalState` stays as-is (loss-only, still exported, still tested) — no parallel concept is introduced; the win check is a sibling line in the same rule block.

`saolei_init` is unaffected — it is registered separately and never calls `validateMove` (it always re-dispatches F2), so it remains the recovery action after a `game_won` (or `game_over`) rejection.

**Rationale**: Minimal, in-place change — two lines + one union member + one import. Loss-check-then-win-check is the natural reading order and makes the "loss takes precedence" edge case explicit (a board cannot be both). Keeping `isTerminalState` loss-only preserves its existing tested semantics and the existing `game_over` tests; the win path is additive. This is a refactor toward one symmetric terminal-state concept (a terminal board rejects further cell ops), not a special-case patch — both terminal reasons live in the same rule block in the same function.

**Alternatives rejected**:
- *Refactor `isTerminalState` to return a terminal-kind (`"won"|"lost"|null`)* — a larger change to a tested function for no behavioural gain; the two-line composition is clearer and keeps `isTerminalState` stable.
- *Add the win check inside `isTerminalState`* — would conflate loss and win in one boolean, losing the reason distinction; the agent needs to return *which* terminal reason.
- *Block `saolei_init` after a win too* — wrong; `saolei_init` is the documented recovery (restarts the game). The terminal check applies to cell ops only, which is exactly the existing `game_over` scope.

---

## D9 — Chord-neighbor validation (US5 / FR-016..020)

**Decision**: In `validateMove`'s `case "saolei_chord_click"` branch, **after** the existing `chord_requires_number` check passes (target is a revealed `1..8`), add a neighbor scan:

```ts
case "saolei_chord_click":
  if (!CHORD_NUMBERS.has(cell)) {
    return { ok: false, reason: "chord_requires_number" };   // existing, unchanged
  }
  // NEW (FR-016..020): a chord reveals INITIAL neighbors; if there are none
  // (and no UNKNOWN neighbor to be lenient about), the chord is a no-op.
  if (!hasInitialOrUnknownNeighbor(state, x, y)) {
    return { ok: false, reason: "chord_no_unrevealed_neighbor" };
  }
  return { ok: true };
```

with a small pure helper:

```ts
/** Yields the in-bounds Moore neighbors of (x, y). */
function neighbors(state: GameState, x: number, y: number): CellStatus[] { … }

/** True if any in-bounds neighbor of (x,y) is INITIAL or UNKNOWN.
 *  (FLAG / revealed-number / HIT_MINE / MINE neighbors do NOT count — a chord
 *  acts only on INITIAL cells and is lenient on UNKNOWN per 025 FR-018.) */
function hasInitialOrUnknownNeighbor(state: GameState, x: number, y: number): boolean {
  return neighbors(state, x, y).some((c) => c === "INITIAL" || c === "UNKNOWN");
}
```

**Reason code**: `"chord_no_unrevealed_neighbor"`.

**Rationale**: The check is added after `chord_requires_number` (FR-018: only chord targets that are revealed numbers reach this check) and after the structural/state-level checks (out-of-bounds, terminal — those run first in `validateMove`). The flag-exclusion (FR-016) is implicit and clean: a `FLAG` neighbor is neither `INITIAL` nor `UNKNOWN`, so it does not satisfy `hasInitialOrUnknownNeighbor` — a chord whose every neighbor is `FLAG`/number/mine is rejected. The `UNKNOWN` leniency (FR-017) falls out of the same `||` (`UNKNOWN` counts as "possibly unrevealed"). The `neighbors` helper is the bounded Moore set (8 positions intersected with `[0,width)×[0,height)`) — handles edge/corner cells (spec Edge Cases). The reason code spelling matches the existing snake_case convention (`chord_requires_number`, `cell_already_revealed`, …) and is self-explanatory to the model.

**Why "reject when no INITIAL AND no UNKNOWN" rather than "reject when no INITIAL regardless"**: a board region where a neighbor is `UNKNOWN` might have an unrevealed cell there (recognition uncertain); rejecting the chord would forgo a possibly-meaningful reveal. Per 025 FR-018 the validator is lenient on `UNKNOWN` everywhere; this rule inherits that. Concretely the rejection fires only when every neighbor is *definitively* not an action target (a number, a flag, or a mine).

**Alternatives rejected**:
- *Reason code `chord_no_initial_neighbor`* — "initial" is the internal `CellStatus` name; "unrevealed" matches the user's wording ("未揭开") and is clearer to the model.
- *Reason code `chord_nothing_to_reveal`* — less precise; the model cannot tell from "nothing to reveal" what condition to fix.
- *Also reject when the adjacent-flag count ≠ the number* — explicitly out of scope (025 FR-015e: a mismatched-flag chord is legal and may reveal nothing); the spec preserves that. Only the "no INITIAL/UNKNOWN neighbor" no-op is new.
- *Check neighbors before `chord_requires_number`* — wrong order (FR-018); a chord on a non-number must still report `chord_requires_number`, not the neighbor reason.

---

## D10 — SKILL.md update (FR-024)

**Decision**: Update `projects/game/agent/src/skill/saolei/SKILL.md`:

1. **Result-format section** ("Recognized text board" / "Example play flow"): document that every tool result body now includes a `game status: won|lost|playing` line, positioned after the outcome line and before the board. Update the worked examples to show the line.
2. **Rejection-reason table** ("Move validation"): add two rows:
   - `game_won` — "The current game is already won. Call `saolei_init` to start a new game." (symmetric with the existing `game_over` row for losses.)
   - `chord_no_unrevealed_neighbor` — "The chord target's neighbors are all revealed or flagged — there is no unrevealed cell for the chord to reveal. The chord would be a no-op; pick a different target or flag/unflag first."
3. **Clarify** (in the validation narrative) that a win is terminal exactly like a loss: after `game status: won`, any cell operation is rejected as `game_won` until `saolei_init` restarts.

**Rationale**: The skill is the model's authority on the result format and the rejection-reason table (025 FR-021 precedent). Without these updates the model cannot reliably parse the new status line or understand the two new rejection reasons — it would see `game status: won` and `rejected: chord_no_unrevealed_neighbor` as opaque strings. The skill is consumed at prompt-assembly time (`projects/game/agent/src/skill/saolei/SKILL.md` is injected for a saolei profile), so updating it directly improves model behaviour.

**Alternatives rejected**:
- *Skip the skill update* — the model would discover the status line by pattern-matching, but would not know what to *do* with `game_won` (the recovery action `saolei_init` is not signalled). The skill is the cheapest place to close that loop.
- *Add the status line to the system prompt instead* — the system prompt is profile-configured; the skill is the saolei-specific authority and is the right home.

---

## D11 — Frontend test infrastructure (US1/US2 verification)

**Decision**: US1 and US2 are verified by `bazel build` (compile check on the Vite build) + manual visual inspection on Windows. **No new frontend unit-test infrastructure is introduced.**

**Rationale**: `projects/game/desktop/frontend/BUILD.bazel` declares only a `vite_build` target — there is no `vitest_test`, no `*.test.ts`, and no test runner wired for the frontend (confirmed by reading the BUILD file). This is the established pattern documented in the 023/024 assumptions: the desktop client is verified by build + unit(manual) + manual, while the agent (a service) carries the automated unit + large-test burden. Introducing vitest for the frontend is a separate infrastructure effort out of scope for this polish feature. The US1/US2 changes are CSS + a small reactive `$effect` + template restructuring — low-risk, visually-verifiable, and build-gated.

**Alternatives rejected**:
- *Introduce vitest for the frontend in this feature* — scope creep; a frontend test infra rollout is its own feature with its own BUILD/tooling concerns. The 023/024/025 features shipped frontend changes on build+manual verification without it.
- *Port the components to a Storybook/Histoire visual test* — even larger scope; not justified for two small component edits.

---

## D12 — Test split across library / agent-unit / large (US3/US4/US5 verification)

**Decision**: Verification is split across three levels, each covering the behaviour it can reach deterministically. The testdata fixtures now span all three game outcomes — a real **win** board (`testdata/saolei_10.png`, added for this feature; its golden `saolei_10.golden.txt` generated via the CLI per the library README), real **loss** boards (`testdata/saolei_5.png` / `saolei_7.png`, already present — both contain `X`/`M` cells), and **in-progress** boards (`testdata/saolei_1.png` all-INITIAL, `saolei_2.png` partially revealed) — so the win/loss/playing statuses and both terminal rejections are large-testable with the REAL recognition engine. Specifically:

| Behaviour | Verified by | Why |
|---|---|---|
| `isWin` predicate — pure logic (all-revealed/all-flagged → true; INITIAL/HIT_MINE/MINE/UNKNOWN → false) | **Library unit** (`win.test.ts`) | Pure function over `GameState`; synthetic grids cover every branch precisely (FR-009/FR-010). |
| `isWin` on a REAL win screenshot | **Library golden test** (`golden.test.ts`, +`saolei_10` case) + a golden-coupled assertion that `isWin(recognizeBoard(saolei_10.png).state) === true` | Proves the predicate returns true on a real recognized win board, not just synthetic grids. The golden case also pins recognition of the win board. |
| `game status: playing` line in the tool_result text | **Large test** (`TestAgentSaoleiTextBoardFlow`) | Achievable on the all-INITIAL `saolei_1.png` testdata board — every result carries `game status: playing`. Extend the existing text assertions. |
| `game status: won` line + `game_won` terminal rejection | **Large test** (new flow) + **agent unit** (`saolei-mcp.test.ts`) | Large: `saolei_init` replied with `saolei_10.png` (9×9 win) → the init result carries `game status: won`; a following `saolei_click` is rejected as `game_won` (no dispatch). Real recognition on the real win screenshot, end-to-end. Unit (DI): the same logic with a canned winning `GameState`, covering the rule in isolation. |
| `game status: lost` line (+ the existing `game_over` rejection now carrying the status) | **Large test** (new flow) + **agent unit** | Large: `saolei_init` replied with `saolei_5.png` (a loss board with `X`/`M`) → the init result carries `game status: lost`; a following cell op is rejected as `game_over` (existing terminal-loss behaviour) and that rejection body carries `game status: lost`. Unit (DI): canned losing `GameState`. |
| `chord_no_unrevealed_neighbor` rejection | **Agent unit** (`saolei-mcp.test.ts`) only | Requires a chord target whose non-flag neighbors are ALL revealed/flagged (no `INITIAL`/`UNKNOWN`) on a NON-terminal board. The win board (`saolei_10`) has no `INITIAL` neighbors but is terminal-won, so a chord there is rejected as `game_won` *before* the chord-neighbor check fires. No existing testdata board exposes the exact non-terminal chord-no-unrevealed-neighbor configuration, so the DI unit test (canned boards) is the precise, reliable cover. |

The large-test assertions target the **status-line presence and the terminal-rejection behaviour** end-to-end (init→status, op→rejection) — the integration the large test exists to verify (the status line must survive the MCP text result → `ToolResultPart.message` → ListMessages chain). The unit tests cover the **rule logic** (predicate branches, terminal classification, chord-neighbor scan) in isolation via DI. `saolei_10.png` is added to the `lib_test` data automatically (the BUILD `glob(["testdata/*.png"]) + glob(["testdata/*.golden.txt"])` picks it up — no BUILD edit for the fixtures); the implementation adds `"saolei_10"` to the `CASES` array in `golden.test.ts`.

**Rationale**: The large test runs against the DEPLOYED agent with the REAL recognition engine (no DI seam in a deployed service — `agent_saolei_test.go` embeds real PNGs and lets `SaoleiBoard` decode them). With win (`saolei_10`) and loss (`saolei_5`/`saolei_7`) boards now both present as real screenshots, the full status taxonomy (won/lost/playing) and both terminal paths are reachable end-to-end without fabricating synthetic PNGs — the recognition engine decodes the real boards, and the agent's status derivation + terminal check run for real. This is stronger than the DI-only unit coverage originally considered (the unit tests still run, in parallel, for rule-logic precision). The single board configuration that resists real-screenshot synthesis — a NON-terminal chord target with no INITIAL/UNKNOWN neighbor — stays unit-test-only; it is a narrow rule and the DI test covers it exactly.

**testdata dimension note**: each large-test status flow is a single-game `saolei_init` (which fixes the board dimensions for that game), so the 9×9 win board (`saolei_10`) and the loss boards (`saolei_5`/`saolei_7`) are used as the init screenshot of their own flow — no cross-dimension `updateFromScreenshot` transition is attempted (which would throw `BoardDimensionMismatchError`). The in-progress flow keeps using `saolei_1` (16×16) for init + updates, unchanged from the existing `TestAgentSaoleiTextBoardFlow`.

**testdata fixture copy**: the large-test package (`projects/game/testplan/`) has its OWN `testdata/` dir with copies of `saolei_1.png` / `saolei_2.png` reused from the library (the testplan `BUILD.bazel` declares them in `embedsrcs`, and `agent_saolei_test.go` `//go:embed`s them). The win and loss flows follow the same pattern: `saolei_10.png` (the win board) AND `saolei_5.png` (a 16×16 loss board — verified to decode to a grid with `X`/`M` cells) are copied into `projects/game/testplan/testdata/` (already done — `cmp`-identical to their library originals), and the implementation adds both to the testplan `BUILD.bazel` `embedsrcs` + `//go:embed` vars (`saoleiBoardWinPNG`, `saoleiBoardLossPNG`) in `agent_saolei_test.go`. The library golden test consumes the library copies; the large test consumes the testplan copies — identical bytes, two locations, matching the `saolei_1`/`saolei_2` precedent. (`saolei_7.png` is also a loss board in the library testdata but `saolei_5.png` suffices for the one loss flow.)

**Alternatives rejected**:
- *Fabricate a synthetic win PNG* — unnecessary now that a real win screenshot (`saolei_10.png`) exists; real-recognition coverage is stronger and is the large test's purpose.
- *Drop the large-test status/terminal assertions and rely only on unit tests* — the large test is the acceptance gate for the deployed agent (Constitution §VI); asserting the status line and the terminal rejection reach the model end-to-end is exactly the integration it must verify. The unit test cannot prove the line survives the full MCP→ToolResultPart→ListMessages chain.
- *Force the chord-neighbor rejection into the large test* — would require a hand-crafted PNG decoded to a specific non-terminal board with a number surrounded by revealed/flagged cells; brittle and coupling-prone. The DI unit test is the precise, reliable cover for that narrow rule.

---

## Summary of decisions

| ID | Topic | Decision |
|---|---|---|
| D1 | Hidden-scrollbar CSS (US1) | `scrollbar-width: none` + `::-webkit-scrollbar{display:none}` scoped to `.thinking-content`; keep `overflow-y:auto` |
| D2 | Auto-scroll wiring (US1) | An `$effect` (open→bottom) + an `$effect.pre` (content-growth→follow-if-at-bottom via `tick().then`, tol 8px) in `ChatMessage.svelte`; `$effect.pre` is required so the at-bottom test reads pre-update `scrollHeight` (Svelte autoscroll pattern) |
| D3 | Compact args (US2) | `compactArgs()` = `JSON.stringify(JSON.parse(x))` w/ raw fallback; render inline `<code>` next to the name |
| D4 | Result formatting (US2) | `<pre class="op-result-message">` with `white-space:pre-wrap; word-break:break-word` |
| D5 | Collapsible result (US2) | `<details>` (default closed) wrapping the result body; summary = status icon+label; screenshot keeps its own nested `<details>` |
| D6 | Win predicate (US3) | New `isWin(state): boolean` in `win.ts`; exported from `index.ts`; lenient on `UNKNOWN` |
| D7 | Status derivation + position (US4) | `gameStatus(state)` = lost→won→playing; line `game status: <x>` after outcome line, before board; omitted when no state |
| D8 | Post-win terminal (US4) | Add `if (isWin(state)) return game_won` after the existing `game_over` check in `validateMove`; `isTerminalState` stays loss-only |
| D9 | Chord-neighbor rule (US5) | After `chord_requires_number`: `if (!hasInitialOrUnknownNeighbor) return chord_no_unrevealed_neighbor`; `neighbors()` helper (bounded Moore set) |
| D10 | SKILL.md update (FR-024) | Document status line + `game_won` + `chord_no_unrevealed_neighbor` rows + win-is-terminal narrative |
| D11 | Frontend test infra | No new infra; US1/US2 verified by `bazel build` + manual (matches 023/024) |
| D12 | Test split | `isWin` pure logic → library unit; `isWin` on real win PNG → golden test (+`saolei_10` case); `game status: playing/won/lost` + `game_won`/`game_over` terminal → large test (win=`saolei_10`, loss=`saolei_5`/`saolei_7`, playing=`saolei_1`) AND agent unit (DI); `chord_no_unrevealed_neighbor` → agent unit only (narrow config, no fitting testdata) |

All plan-time unknowns are resolved. No `[NEEDS CLARIFICATION]` remains.
