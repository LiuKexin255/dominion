# Feature Specification: Chat Bubble UX Polish & Saolei Game-State Awareness

**Feature Branch**: `027-chat-bubble-game-state`

**Created**: 2026-07-27

**Status**: Draft

**Input**: User description: "对 projects/game/desktop/ 和 projects/game/agent/ 做一些优化 1. desktop 的 session 对话框中 think 气泡的右侧取消滑动块（仅 UI 取消）2. think 气泡点开后，可以跟随输出自动滚动。3. tool 气泡工具入参不需要格式化，例如 saolei_flag { \"x\": 7, \"y\": 7 } (x,y) 的参数分成了4行，没必要。并且输出却没有格式化，看不出来输出的格式。默认可以折叠 tool_result，需要看的时候点开查看详情即可。4. projects/game/pkg/saolei-board/ 输出游戏胜利识别。5. projects/game/agent/ saolei_mcp 操作输出增加游戏状态。6. projects/game/agent/ saolei_chord_click 操作校验增加校验目标周围是否有未揭开的格子（初始状态），如果没有未揭开的格子 chord 操作没有意义。"

## Motivation

Six independent polish/correctness gaps surfaced across the desktop chat UI, the saolei-board recognition library, and the saolei MCP — all small, all affecting the day-to-day saolei play loop. None requires architectural change; each is a targeted refinement of an existing surface introduced by [023](../023-saolei-mcp-refine/spec.md) / [024](../024-tool-render-coord-fix/spec.md) / [025](../025-desktop-image-state-refine/spec.md).

| # | Surface | Gap (confirmed by reading the code) |
|---|---|---|
| 1 | desktop think bubble | The expanded thinking content (`projects/game/desktop/frontend/src/components/ChatMessage.svelte` `.thinking-content`, `max-height: 200px; overflow-y: auto`) renders the **platform-default scrollbar** on the right whenever the streaming reasoning overflows the 200 px cap. The user wants the scrollbar **hidden visually** while keeping the area scrollable ("仅 UI 取消"). |
| 2 | desktop think bubble | The thinking bubble has **no auto-scroll**: as `part.thinking.content` grows (consecutive streaming `ThinkingPart`s are folded into one trailing part by `App.svelte:525-538`), the `.thinking-content` `pre` stays pinned at the top, so the latest reasoning scrolls out of view and the operator must drag manually. The user wants the expanded bubble to **follow the output**. |
| 3 | desktop tool bubble | Three sub-gaps in `ChatView.svelte`'s tool-bubble renderer: (a) `prettyArgs()` (L179-186) calls `JSON.stringify(JSON.parse(argsJson), null, 2)` → a 2-key input like `saolei_flag {"x":7,"y":7}` is blown up to 4 lines (the user's example); (b) the result message lives in `<span class="op-result-message">` whose style has **no `white-space: pre-wrap`**, so the multi-line text board returned by the saolei MCP (e.g. `saolei_click at (4,4) → dispatched\n\nboard size 9*9\n\n* * * …`) collapses to a single run-on line — "看不出来输出的格式"; (c) the full result body is always expanded, so every saolei turn dumps the whole text board into the conversation even when the operator only wants the status. |
| 4 | saolei-board library | `projects/game/pkg/saolei-board` recognises cells (`INITIAL`/`0..8`/`FLAG`/`HIT_MINE`/`MINE`/`UNKNOWN`) but exposes **no win detection**. The saolei MCP's `isTerminalState` (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts:165-172`) only treats `HIT_MINE`/`MINE` as terminal — a win is invisible to both the library and the agent, so the model is never told it has won and may keep operating on a finished board. |
| 5 | saolei MCP | Each saolei tool result (`initSuccessText` / `dispatchedText` / `rejectionText` / `unrecognizableText` in `saolei-mcp.ts`) reports the **board** but not the **game status** (won / lost / in-progress). The model must infer status from the cell grid; with no win detection (gap 4) a win is undetectable. The user wants the game status surfaced as an explicit part of every operation outcome. |
| 6 | saolei MCP chord validation | `validateMove` (`saolei-mcp.ts:189-230`) permits a chord on any revealed number `1..8` regardless of outcome (FR-015e: a chord whose flag count ≠ the number is still legal). But it also permits a chord whose **non-flag neighbors are all already revealed** — i.e. once the `FLAG` neighbors (the cells the player has marked as mines and that the chord does NOT touch) are set aside, there is no `INITIAL` neighbor left for the chord to reveal. Such a chord is a guaranteed no-op (it reveals nothing), so dispatching it wastes a turn and a screenshot. The user wants it rejected as meaningless. |

## Relationship

- **Continues [024 — Tool Bubble Rendering & Saolei Coordinate Accuracy](../024-tool-render-coord-fix/spec.md)** for the desktop chat surface: US1 and US2 refine the same `ChatView.svelte` / `ChatMessage.svelte` tool- and think-bubble renderers that 024 made functional. No proto change, no content-model change — purely how the existing `MessagePart`s are presented.
- **Builds on [025 — Desktop Image-State Refine](../025-desktop-image-state-refine/spec.md)** for the saolei MCP dimension: US3 extends `@dominion/game-saolei-board` (the recognition engine wired in by 025) with a win predicate; US4 surfaces that predicate (plus the existing loss predicate) in the MCP text outcome; US5 tightens the strict pre-dispatch validation 025 introduced (FR-015) with one additional rule for `saolei_chord_click`. The four-tool surface, the proto `FlowPart` operation contract, the recognized-state lifecycle, and the text-only return contract (025 FR-012) are all **unchanged**.
- **Independent of [023](../023-saolei-mcp-refine/spec.md)'s content-model split and debug drawer**: the think/tool bubble changes operate entirely within the existing `MessagePart` render path; the MCP changes operate entirely within the existing text-result builder. No new `MessagePart`/`FlowPart` kind, no proto change, no debug-hold change.
- **Interface (Constitution §III)**: no external interface changes. The saolei tool **text-result contract** gains a status line (US4) — this is a backward-compatible additive change to the text body the model already reads (the existing outcome line + board are unchanged; a new short status token is prepended or appended). The saolei-board library gains an exported pure predicate (US3) — a new public function, no change to existing exports. The new chord rejection reason (US5) is one new value in the already-open `MoveRejection` union.

## Clarifications

Resolved during specification by reasonable defaults (documented in **Assumptions**). No `[NEEDS CLARIFICATION]` markers remain. The decisions settled by reasonable default are:

- **Think-bubble auto-scroll vs. manual scroll (US1)**: auto-scroll to bottom while the bubble is expanded **and** the operator is already at/near the bottom; if the operator scrolls up away from the bottom, auto-scroll pauses (so they can read history) and resumes when they scroll back to the bottom. This is the standard chat-app pattern and matches "跟随输出自动滚动" without trapping the operator at the bottom when they want to scroll up.
- **Tool-args compact rendering (US2)**: render the raw `argsJson` as received (single-line / as-arrived), do NOT re-format via `JSON.stringify(…, null, 2)`. A short, single-line shape (e.g. `saolei_flag {"x":7,"y":7}`) is the goal.
- **Tool-result collapsibility (US2)**: the result body (status message + any text board) is wrapped in a `<details>` that is **collapsed by default**; the always-visible part is the status icon + label (e.g. `✓ done` / `› done`); expanding reveals the full message (with formatting preserved). The screenshot sub-`<details>` (already collapsed) stays as-is.
- **Win-detection rule (US3)**: classic Win32 Minesweeper auto-flags all unflagged mines on a win, so a winning board has **no `INITIAL` cells and no `HIT_MINE`/`MINE` cells** (every cell is a revealed number `0..8` or a `FLAG`). To stay lenient toward recognition uncertainty (mirroring `saolei-board`'s `UNKNOWN` handling and 025 FR-018), a board that still contains any `UNKNOWN` cell is **not** classified as a win (the predicate returns false; the status stays "in-progress"). A loss (`HIT_MINE` or `MINE` present) takes precedence over the win check.
- **Game-status surface in MCP output (US4)**: a short status token is added to each tool-result body — `game status: won` / `game status: lost` / `game status: playing` — on its own line, alongside the existing outcome line and text board. It is part of the same single MCP text content block (no new content-block kind; 025 FR-012 single-text-block contract preserved).
- **Chord-neighbor validation strictness (US5)**: a chord acts on `INITIAL` cells — it reveals the unrevealed, unflagged neighbors of a satisfied number. `FLAG` cells are **marked mines** the player has already accounted for; the chord does NOT touch them, so they are excluded from the "is there anything to reveal?" check. The rule: reject a chord when, after excluding `FLAG` neighbors, **none** of the remaining neighbors is `INITIAL` (i.e. every non-flag neighbor is a revealed number, `HIT_MINE`, or `MINE`). Because `INITIAL` and `FLAG` are disjoint cell states, this is equivalent to "reject when no neighbor is `INITIAL`" — but the flag-exclusion is stated explicitly so the rationale (flags are not action targets) is clear. Per 025 FR-018 (lenient on `UNKNOWN`), an `UNKNOWN` neighbor is treated as possibly unrevealed — if any non-flag neighbor is `UNKNOWN`, the chord is **not** rejected on this ground. A new stable reason code is added to the `MoveRejection` union. This refines (does not replace) 025 FR-015e: a chord whose adjacent-flag count ≠ the number is still legal and not rejected; only the "no `INITIAL` (and no `UNKNOWN`) non-flag neighbor to act on" case is new.

### Session 2026-07-27

- Q: After a win is recognized, should subsequent saolei cell operations be rejected as terminal (symmetric with loss), or is the `game status: won` line purely informational? → A: **Reject post-win operations with a new `game_won` reason (symmetric with loss's `game_over`).** A win is a terminal state; the agent extends its terminal-state check to include wins (via the US3 win predicate) and rejects any cell operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`) attempted after a recognized win with `game_won`, guiding the model to call `saolei_init` to start a new game. `saolei_init` itself is unaffected (it always re-dispatches F2). This makes win and loss handling symmetric, prevents the model from despoiling a winning board if it ignores the status line, and adds `game_won` to the `MoveRejection` union alongside the chord-neighbor reason.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The Think Bubble Has No Visible Scrollbar and Follows the Streaming Output (Priority: P1)

When an agent's thinking content overflows its 200 px cap in the desktop session conversation, the expanded think bubble shows **no visible scrollbar** on the right (the area stays scrollable — the operator can still scroll — but the platform-default scrollbar track/thumb is hidden). And when the bubble is expanded, the content **auto-scrolls to keep the latest reasoning visible** as it streams in; if the operator scrolls up away from the bottom, the auto-scroll pauses so they can read history, and resumes when they scroll back to the bottom.

**Why this priority**: The think bubble is the operator's primary window into the model's reasoning during a saolei turn. A loud scrollbar on a narrow dashed bubble is visual noise, and reasoning that scrolls out of view defeats the purpose of expanding the bubble. Both are daily-UX papercuts on the most-watched surface; they are tightly coupled (same `.thinking-content` element) and ship together.

**Independent Test**: Run a turn whose thinking content exceeds 200 px; expand the think bubble; confirm (a) no scrollbar track/thumb is visible on the right even though the content overflows, and (b) as more reasoning streams in, the bubble scrolls to show the latest line; then scroll up manually and confirm the auto-scroll pauses until you scroll back to the bottom.

**Acceptance Scenarios**:

1. **Given** an expanded think bubble whose content overflows its max-height, **When** it is rendered, **Then** no visible scrollbar track or thumb appears on the right edge (the content remains scrollable via wheel/trackpad/keyboard).
2. **Given** an expanded think bubble and new thinking content streaming in, **When** the content grows, **Then** the bubble auto-scrolls so the latest line is visible at the bottom.
3. **Given** an expanded think bubble that is auto-scrolling, **When** the operator scrolls up away from the bottom, **Then** auto-scroll pauses (subsequent streaming does not yank the view back to the bottom) until the operator scrolls back to the bottom, at which point auto-scroll resumes.
4. **Given** a collapsed think bubble, **When** it is expanded, **Then** it opens scrolled to the bottom of the current content (the latest reasoning is visible immediately).
5. **Given** a think bubble whose content fits within the max-height (no overflow), **When** it is rendered, **Then** there is no scrollbar (visible or otherwise) and no auto-scroll behavior is observable.

---

### User Story 2 - The Tool Bubble Shows Compact Args, a Formatted Collapsible Result (Priority: P1)

A tool-call bubble in the desktop session conversation shows the tool name and its input arguments **compactly** — a multi-key input like `saolei_flag` with `{"x":7,"y":7}` renders on one line (or as-arrived), NOT pretty-printed across multiple lines. The tool **result** preserves its native formatting (a multi-line text board stays multi-line — newlines and structure are visible), and the result body is **collapsed by default** behind a toggle: the always-visible part is the status icon + label (e.g. `✓ done` / `› done`), and expanding the toggle reveals the full formatted result message (the text board for saolei; the message text for native tools). The screenshot sub-toggle (already collapsed) is unchanged.

**Why this priority**: The tool bubble is the second-most-watched surface in a saolei turn. Today a 2-key input wastes 4 lines and a multi-line text board collapses to an unreadable run-on line, and every turn dumps the whole board into the conversation. This is the most visible of the six gaps and is purely desktop-side.

**Independent Test**: Run a saolei turn that calls `saolei_flag(7,7)` then `saolei_click(4,4)`; confirm (a) the flag bubble's args render on one line (not 4), (b) the click bubble's result message preserves the multi-line text board structure when expanded, and (c) the result body is collapsed by default (only the status icon + label visible until clicked).

**Acceptance Scenarios**:

1. **Given** a tool-call bubble whose input args are a JSON object, **When** the args are rendered, **Then** they appear **compact** (single-line / as-arrived) — they are NOT pretty-printed with indentation that splits each key onto its own line.
2. **Given** a tool result whose message contains newlines (e.g. the saolei text board `board size 9*9\n\n* * * …`), **When** the result is rendered (and expanded), **Then** the newlines and structure are preserved (the board is readable as a grid, not a run-on line).
3. **Given** a resolved tool bubble, **When** it is first rendered, **Then** the result body is **collapsed by default** — only the status icon + label (e.g. `✓ done`) are visible; the full message is hidden until the operator expands the toggle.
4. **Given** a collapsed tool result, **When** the operator clicks the toggle, **Then** the full formatted message expands into view; clicking again collapses it.
5. **Given** a tool result with a screenshot (native mouse tools), **When** the result is rendered, **Then** the screenshot remains behind its own existing sub-toggle (collapsed), independent of the new result-body toggle.

---

### User Story 3 - saolei-board Exposes a Win Predicate (Priority: P1)

The recognition library `@dominion/game-saolei-board` exposes a pure predicate that classifies a recognized `GameState` as a **win**. A board is a win when no cell is `INITIAL` and no cell is `HIT_MINE`/`MINE` (every cell is a revealed number `0..8` or a `FLAG` — the classic Minesweeper win condition, where on a win all unflagged mines are auto-flagged). To stay lenient toward recognition uncertainty, a board that still contains any `UNKNOWN` cell is **not** classified as a win. The predicate complements the existing loss signal (`HIT_MINE`/`MINE` presence) so that a recognized board's terminal status is fully determinable from the library.

**Why this priority**: This is the enabling capability for US4 (the agent surfacing game status). It lives in the library (where the recognized state and the per-cell recognition logic already live) so the win rule is defined once, tested with the golden suite, and reusable beyond the MCP. It is independent of the desktop UI changes.

**Independent Test**: Construct synthetic `GameState`s — one that is a win (all `0..8`/`FLAG`, no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`), one that is a loss (`HIT_MINE` present), one in-progress (some `INITIAL`), and one with `UNKNOWN` cells that would otherwise look like a win — and confirm the predicate returns `true` only for the first.

**Acceptance Scenarios**:

1. **Given** a `GameState` whose every cell is a revealed number (`0..8`) or a `FLAG`, **When** the win predicate is evaluated, **Then** it returns `true`.
2. **Given** a `GameState` with at least one `INITIAL` cell, **When** the predicate is evaluated, **Then** it returns `false` (the board is not yet terminal-won).
3. **Given** a `GameState` with a `HIT_MINE` or `MINE` cell (a loss), **When** the predicate is evaluated, **Then** it returns `false` (loss is not a win; loss detection is a separate, already-existing signal).
4. **Given** a `GameState` that would otherwise be a win but contains at least one `UNKNOWN` cell, **When** the predicate is evaluated, **Then** it returns `false` (lenient on recognition uncertainty — do not claim a win the library is not sure about).
5. **Given** the win predicate is exported from the library's public barrel, **When** a consumer imports it, **Then** it is callable as a pure function taking a `GameState` and returning a `boolean`, with no side effects.

---

### User Story 4 - The saolei MCP Surfaces Game Status in Every Operation Outcome (Priority: P1)

Every saolei tool result — `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click` — reports the current **game status** as part of its text outcome: `game status: won`, `game status: lost`, or `game status: playing`. The status is derived from the recognized state via the library's win predicate (US3) and the existing loss signal (`HIT_MINE`/`MINE`), and is emitted on its own line alongside the existing outcome line and text board. A win or loss status is surfaced the moment the recognized state reflects it (i.e. on the operation that produced the terminal board), so the model is told it has finished and can stop operating. A win is a **terminal state symmetric with a loss**: once the recognized state is a win, any subsequent cell operation is **rejected before dispatch** with a new `game_won` reason (the model is guided to call `saolei_init` to restart), mirroring how a loss rejects further operations with `game_over`.

**Why this priority**: Without a status line, the model must infer end-of-game from the cell grid, and — because no win detection exists today — a win is invisible, so the model keeps issuing operations on a finished board (each rejected as `game_over` after the first loss, but a win is never signalled at all). Surfacing the status explicitly closes the loop. Depends on US3 (the win predicate) and is independent of the desktop UI.

**Independent Test**: Configure a saolei profile with a fake `SaoleiBoardApi` that returns a canned winning board (all cells revealed/flagged) on the next `saolei_click`; call `saolei_init` then `saolei_click`; confirm the click result contains the line `game status: won` (and that an in-progress board yields `game status: playing`, and a board with `HIT_MINE` yields `game status: lost`). Then call another cell operation on the won board; confirm it is **rejected** with `game_won` and not dispatched (the desktop receives no operation); confirm `saolei_init` is still accepted and restarts.

**Acceptance Scenarios**:

1. **Given** a recognized state that is a win (per the US3 predicate), **When** any saolei tool result is built, **Then** the text outcome contains the line `game status: won`.
2. **Given** a recognized state that is a loss (any `HIT_MINE`/`MINE`), **When** any saolei tool result is built, **Then** the text outcome contains the line `game status: lost`.
3. **Given** a recognized state that is neither won nor lost, **When** any saolei tool result is built, **Then** the text outcome contains the line `game status: playing`.
4. **Given** any saolei tool result, **When** the model receives it, **Then** the result is still a single MCP text content block (025 FR-012 preserved) — the status line is part of the same text body as the outcome line and the text board, not a new content-block kind.
5. **Given** a rejection outcome (an illegal move, `rejected: <reason>`) or an `unable to recognize board` outcome, **When** it is built, **Then** it includes the game status line where a recognized state exists (e.g. `game status: playing` alongside `rejected: cell_already_revealed`); when no recognized state exists (`no_active_game` / `unable to recognize`), the status line is omitted (no fabricated status).
6. **Given** a recognized state that is a win, **When** any cell operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`) is attempted **after** the win, **Then** it is **rejected before dispatch** with the `game_won` reason, and the desktop receives no operation — symmetric with how a loss rejects further operations with `game_over`.
7. **Given** a post-win `game_won` rejection, **When** its outcome is built, **Then** it follows the existing rejection contract (025 FR-016): the body contains `rejected: game_won` + the current text board + the valid coordinate range + the `game status: won` line.
8. **Given** a recognized state that is a win, **When** `saolei_init` is called, **Then** it is **accepted** and re-dispatches F2 to start a new game (the recovery action; `saolei_init` is never blocked by a terminal state).

---

### User Story 5 - saolei_chord_click Is Rejected When the Target Has No Initial-State Neighbor to Reveal (Priority: P2)

Before dispatching a `saolei_chord_click`, the MCP checks the target's 8 neighbors. A chord acts on `INITIAL` cells (it reveals the unrevealed, unflagged neighbors of a satisfied number); `FLAG` cells are marked mines the chord does NOT touch and are excluded from this check. If, after excluding `FLAG` neighbors, **none** of the remaining neighbors is an `INITIAL` cell — i.e. every non-flag neighbor is a revealed number, `HIT_MINE`, or `MINE` — the chord is **rejected before dispatch** with a new stable reason code, because there is no `INITIAL` cell for the chord to reveal and the operation is a guaranteed no-op. Per 025 FR-018 (lenient on `UNKNOWN`), if any non-flag neighbor is `UNKNOWN`, the chord is **not** rejected on this ground (an `UNKNOWN` neighbor is treated as possibly unrevealed). This refines (does not replace) 025 FR-015e: a chord whose adjacent-flag count ≠ the number is still legal and not rejected.

**Why this priority**: This is a turn-efficiency refinement, not a correctness bug — a meaningless chord today wastes one dispatch + one screenshot + one recognition pass before the model discovers nothing changed. It is the lowest-priority of the six but is a clean, additive rule on the existing strict validator. Independent of US1–US4.

**Independent Test**: Configure a recognized board where a revealed `3` at `(4,4)` has 3 `FLAG` neighbors and 5 revealed-number neighbors (so the non-flag neighbors are all revealed numbers — no `INITIAL` among them); call `saolei_chord_click(4,4)`; confirm it is **rejected** with the new reason and the desktop receives no operation. Then reconfigure so the same target's non-flag neighbors include one `INITIAL`; confirm the chord dispatches normally.

**Acceptance Scenarios**:

1. **Given** a recognized board where the chord target is a revealed number (`1..8`) and, after excluding `FLAG` neighbors, **none** of the remaining neighbors is `INITIAL` (every non-flag neighbor is a revealed number, `HIT_MINE`, or `MINE`), **When** `saolei_chord_click(x,y)` is called, **Then** it is **rejected before dispatch** with the new reason code, and the desktop receives no operation.
2. **Given** a recognized board where the chord target is a revealed number and **at least one non-flag neighbor is `INITIAL`**, **When** `saolei_chord_click(x,y)` is called, **Then** it dispatches normally (the existing legal-chord path).
3. **Given** a recognized board where the chord target is a revealed number whose non-flag neighbors are all revealed numbers (no `INITIAL`) **but at least one neighbor is `UNKNOWN`**, **When** `saolei_chord_click(x,y)` is called, **Then** it dispatches normally (lenient on `UNKNOWN` per 025 FR-018 — the `UNKNOWN` neighbor is treated as possibly unrevealed, so the chord is not rejected on this ground).
4. **Given** the new rejection, **When** its outcome is built, **Then** it follows the existing rejection contract (025 FR-016): the rejection body contains the reason code, the current text board, and the valid coordinate range, plus the game-status line per US4.
5. **Given** the existing `chord_requires_number` rule (target is not a revealed `1..8`), **When** a chord on a non-number cell is attempted, **Then** it is still rejected with `chord_requires_number` (the existing rule is unchanged; the new rule is checked only after the existing chord-target rule passes).

---

### Edge Cases

- **Think bubble opened mid-stream**: when the operator expands the think bubble while thinking is still streaming, the bubble opens scrolled to the bottom of the content-so-far and continues to follow the stream (US1 acceptance 4).
- **Think bubble with very long content**: hiding the scrollbar MUST NOT remove scroll capability — the operator can still scroll the overflow with wheel/trackpad/keyboard; only the visible scrollbar track/thumb is suppressed.
- **Operator scrolls up then content stops streaming**: the auto-scroll stays paused at the operator's chosen position; it does not jump to the bottom when streaming ends.
- **Tool args that are not valid JSON**: the renderer MUST fall back to showing the raw `argsJson` string compactly (no crash, no pretty-print attempt that throws).
- **Tool result with an empty message**: the collapsed toggle still shows the status icon + label; expanding reveals an empty (or placeholder) body — no broken layout.
- **Tool result with both a multi-line message and a screenshot**: both are independently collapsible (the message behind the new toggle, the screenshot behind its existing sub-toggle); expanding one does not force-expand the other.
- **Win detection on a board with mixed `UNKNOWN` and otherwise-winning cells**: the predicate returns `false` (lenient — US3 acceptance 4). The model is told `game status: playing`, not `won`, so it does not stop early on an uncertain board.
- **Win detection on the very first `saolei_init` board**: an all-`INITIAL` fresh board is not a win (US3 acceptance 2); the predicate returns `false`.
- **Game-status line ordering in the text outcome**: the status line appears in a fixed, documented position relative to the outcome line and the board (settled in planning) so the model can parse it reliably; the single-text-block contract (025 FR-012) is preserved.
- **Game status when the state was just invalidated (`unable to recognize board`)**: no status line is emitted (US4 acceptance 5) — there is no recognized state to derive a status from.
- **Chord target on the board edge/corner**: the neighbor set is the intersection of the 8 surrounding positions with the board bounds (an edge cell has 5 neighbors; a corner has 3); the "no `INITIAL` non-flag neighbor" rejection applies to the in-bounds non-flag neighbors only.
- **Chord target whose neighbors are all `FLAG`**: this is a "no `INITIAL` non-flag neighbor" case → rejected (every neighbor is `FLAG`, so after excluding flags there are no neighbors left to act on), unless any neighbor is `UNKNOWN` (lenient). The flags are marked mines the chord does not touch, so there is nothing to reveal.
- **Chord target whose non-flag neighbors are all revealed numbers** (e.g. a `3` surrounded by 3 `FLAG`s and 5 revealed numbers): rejected — after excluding the 3 flags, the remaining 5 neighbors are all revealed numbers, so no `INITIAL` cell exists for the chord to reveal. (Unless any neighbor is `UNKNOWN` — lenient.)
- **Chord target with a mix of `FLAG` and `INITIAL` neighbors**: allowed — the `INITIAL` neighbors are exactly the cells the chord will reveal when the flag count satisfies the number; flags are expected alongside them (they are the mines the player marked).
- **Existing chord rules preserved**: the new `no-initial-non-flag-neighbor` check is added **after** the existing `chord_requires_number` check (and after the structural/state-level checks); it MUST NOT relax or replace any existing rule.
- **Post-win `saolei_flag` toggle on an already-flagged cell**: rejected as `game_won` (the win is terminal; no cell operation — not even a flag toggle — is permitted after a win). Before this feature such a toggle would have been allowed and would have mutated the winning board.
- **Post-win `saolei_init`**: accepted — `saolei_init` always re-dispatches F2 to start a new game and is never blocked by a terminal state; it is the recovery action the `game_won` rejection guides the model toward.
- **Win + `UNKNOWN` cells (not a win)**: a board that still contains any `UNKNOWN` cell is not classified as a win (US3 FR-010, lenient), so it is not terminal-won; cell operations are NOT rejected as `game_won`. The status line reads `game status: playing`. Only a board the library is sure about (no `UNKNOWN`) can be terminal-won.
- **Loss takes precedence over win check**: a board with `HIT_MINE`/`MINE` is a loss and the win predicate returns `false` for it, so the terminal reason is `game_over`, never `game_won`. The two terminal reasons are mutually exclusive.

## Requirements *(mandatory)*

### Functional Requirements

**Think bubble polish (US1)**

- **FR-001**: The expanded think bubble's content area (`projects/game/desktop/frontend/src/components/ChatMessage.svelte` `.thinking-content`) MUST NOT show a visible scrollbar track or thumb on the right edge, even when the content overflows its max-height. The area MUST remain scrollable (wheel/trackpad/keyboard) — only the visible scrollbar UI is suppressed.
- **FR-002**: When the think bubble is expanded and new thinking content streams in, the content area MUST auto-scroll to keep the latest content visible at the bottom.
- **FR-003**: When the operator scrolls up away from the bottom, auto-scroll MUST pause (subsequent streaming MUST NOT yank the view back to the bottom); when the operator scrolls back to the bottom, auto-scroll MUST resume.
- **FR-004**: When a collapsed think bubble is expanded, the content area MUST open scrolled to the bottom of the current content (the latest reasoning is visible immediately).

**Tool bubble polish (US2)**

- **FR-005**: A tool-call bubble's input args MUST be rendered **compact** (single-line / as-arrived) — the renderer MUST NOT pretty-print `argsJson` with indentation that splits each key onto its own line. Invalid-JSON args MUST fall back to the raw string compactly.
- **FR-006**: A tool result's message MUST preserve its native formatting (newlines, multi-line structure) when rendered. A multi-line text board (the saolei result body) MUST be readable as a multi-line grid, not collapsed to a run-on line.
- **FR-007**: A resolved tool bubble's result body (status message + any text board) MUST be **collapsed by default** behind a toggle. The always-visible part MUST be the status icon + label only (e.g. `✓ done` / `✗ failed` / `› done`); expanding the toggle reveals the full formatted message.
- **FR-008**: The screenshot sub-toggle (existing `<details>` for native mouse-tool results) MUST remain independently collapsible and unchanged; the new result-body toggle (FR-007) and the screenshot sub-toggle MUST NOT force one another open/closed.

**saolei-board win detection (US3)**

- **FR-009**: `@dominion/game-saolei-board` MUST export a pure predicate that classifies a `GameState` as a win. The predicate MUST return `true` if and only if **no** cell is `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN` (i.e. every cell is a revealed number `0..8` or a `FLAG`).
- **FR-010**: The win predicate MUST return `false` for a board that contains any `INITIAL` cell (game in progress), any `HIT_MINE`/`MINE` cell (a loss — loss is not a win), or any `UNKNOWN` cell (lenient on recognition uncertainty).
- **FR-011**: The win predicate MUST be a pure function of `GameState` (no side effects, no I/O, no mutation), exported from the library's public barrel (`projects/game/pkg/saolei-board/src/core/index.ts`), callable by the saolei MCP (US4) and by library tests.

**saolei MCP game-status output (US4)**

- **FR-012**: Every saolei tool result body — `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click` — MUST include a **game-status line** (`game status: won` / `game status: lost` / `game status: playing`) derived from the recognized state, when a recognized state exists.
- **FR-013**: The status MUST be `won` when the recognized state satisfies the US3 win predicate; `lost` when the recognized state contains any `HIT_MINE`/`MINE` (the existing loss signal); `playing` otherwise.
- **FR-014**: The status line MUST be part of the same single MCP text content block as the existing outcome line and text board (025 FR-012 single-text-block contract preserved). No new content-block kind is introduced.
- **FR-015**: A rejection outcome (`rejected: <reason>`) MUST include the game-status line when a recognized state exists (e.g. `game status: playing` alongside `rejected: cell_already_revealed`). An `unable to recognize board` outcome, or a `no_active_game` rejection (no recognized state), MUST omit the status line — no status is fabricated.

**saolei_chord_click neighbor validation (US5)**

- **FR-016**: `validateMove` for `saolei_chord_click` MUST reject the move — with a new stable reason code and without dispatching — when **no in-bounds neighbor is `INITIAL`** (i.e. every non-flag in-bounds neighbor is a revealed number `0..8`, `HIT_MINE`, or `MINE`). `FLAG` neighbors are excluded from the check because they are marked mines the chord does NOT touch and are not action targets (equivalently: since `INITIAL` and `FLAG` are disjoint states, "no in-bounds neighbor is `INITIAL`" is the same condition as "no non-flag neighbor is `INITIAL`").
- **FR-017**: The new check MUST be lenient toward `UNKNOWN` (025 FR-018): if any in-bounds neighbor is `UNKNOWN`, the chord MUST NOT be rejected on this ground (the `UNKNOWN` neighbor is treated as possibly unrevealed). Concretely: the rejection applies only when every in-bounds neighbor is a revealed number, `FLAG`, `HIT_MINE`, or `MINE` (i.e. no `INITIAL` and no `UNKNOWN` neighbor).
- **FR-018**: The new check MUST be applied **after** the existing `chord_requires_number` rule (target must be a revealed `1..8`) and the structural/state-level checks (out-of-bounds, terminal). The existing rules MUST remain unchanged.
- **FR-019**: The new reason code MUST be added to the `MoveRejection` union (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`) and surfaced verbatim in the rejection outcome line, following the existing rejection contract (025 FR-016: rejection body = `rejected: <reason>` + current text board + valid coordinate range + game-status line per US4).
- **FR-020**: A chord rejected under FR-016 MUST NOT be dispatched to the desktop (the desktop receives no operation for it), consistent with the existing rejection path.

**Post-win terminal handling (US4)**

- **FR-021**: A recognized win MUST be treated as a terminal state symmetric with a loss. Any cell operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`) attempted AFTER the recognized state is a win MUST be **rejected before dispatch** with a new `game_won` reason code, and the desktop MUST receive no operation for it. (`saolei_init` is unaffected — it always re-dispatches F2 to start a new game, and is the recovery action the `game_won` rejection guides the model toward.)
- **FR-022**: The terminal-state check MUST distinguish won from lost and return the matching reason: a loss (`HIT_MINE`/`MINE`) → the existing `game_over`; a win (per the US3 win predicate) → the new `game_won`. The two are mutually exclusive in a single recognized state (a board with `HIT_MINE`/`MINE` is a loss, not a win — US3 FR-010), so exactly one terminal reason applies. The terminal check (win or loss) MUST run before the per-tool cell-specific rules, consistent with the existing `game_over` placement in `validateMove`.
- **FR-023**: `game_won` MUST be added to the `MoveRejection` union (`projects/game/agent/src/mcp/saolei/saolei-mcp.ts`) and surfaced verbatim in the rejection outcome line, following the existing rejection contract (025 FR-016: rejection body = `rejected: game_won` + current text board + valid coordinate range + `game status: won` line per US4).

**Built-in saolei skill documentation (US4/US5)**

- **FR-024**: The built-in saolei skill (`projects/game/agent/src/skill/saolei/SKILL.md`) MUST be updated to document: (a) the `game status: won|lost|playing` line present in every tool result body; (b) the new `game_won` rejection reason — a cell operation after a recognized win is rejected; call `saolei_init` to restart; and (c) the new chord-neighbor rejection reason (FR-019) — a chord whose non-flag neighbors contain no `INITIAL` (and no `UNKNOWN`) cell is rejected. The skill is the model's authority on the result format and the rejection-reason table.

### Key Entities *(include if feature involves data)*

- **Think bubble content area**: the `.thinking-content` element in `ChatMessage.svelte`. Owns its own scroll state (auto-follow + pause-on-scroll-up) and its own scrollbar visibility (hidden track/thumb, scrollable content).
- **Tool bubble args render shape**: the compact, single-line rendering of `ToolCallPart.argsJson` (replacing the current `prettyArgs` pretty-print). Owned by `ChatView.svelte`.
- **Tool result collapsible body**: the `<details>` (or equivalent toggle) wrapping the result message + text board, collapsed by default. Owned by `ChatView.svelte`.
- **Win predicate (library)**: a new pure exported function `GameState → boolean` in `@dominion/game-saolei-board`. The single source of truth for "is this recognized board a win".
- **Game status (MCP outcome line)**: a short token (`won`/`lost`/`playing`) derived from the recognized state via the win predicate + the loss signal, emitted as one line of the saolei tool text result.
- **Chord-neighbor reason code**: a new value in the `MoveRejection` union, surfaced in the rejection outcome when a chord target has no `INITIAL` and no `UNKNOWN` non-flag neighbor (i.e. every non-flag neighbor is a revealed number or a `HIT_MINE`/`MINE`). `FLAG` neighbors are excluded from the check (they are marked mines, not chord action targets).
- **`game_won` rejection reason**: a new value in the `MoveRejection` union, returned when a cell operation is attempted after the recognized state is a win (per the US3 predicate). Symmetric with the existing `game_over` (loss); guides the model to call `saolei_init` to restart. A win and a loss are mutually exclusive terminal states, so exactly one of `game_won` / `game_over` applies to a given rejection.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of turns whose thinking content overflows the think-bubble max-height, the expanded bubble shows no visible scrollbar track/thumb on the right while remaining scrollable.
- **SC-002**: In 100% of turns, an expanded think bubble auto-scrolls to follow the streaming output; when the operator scrolls up, auto-scroll pauses until they return to the bottom.
- **SC-003**: In 100% of tool-call bubbles, multi-key input args render on a single line (not pretty-printed across multiple lines); invalid-JSON args fall back to the raw string.
- **SC-004**: In 100% of resolved tool bubbles, the result message preserves its native multi-line formatting (when expanded), and the result body is collapsed by default (only the status icon + label visible).
- **SC-005**: In 100% of cases, the saolei-board win predicate correctly classifies a recognized board (true for an all-revealed/all-flagged board with no `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN`; false otherwise) — verified by the library unit tests including the golden board states.
- **SC-006**: In 100% of saolei tool results (where a recognized state exists), the text outcome contains the correct `game status: won|lost|playing` line; a win/loss is surfaced on the operation whose recognized state first reflects it.
- **SC-007**: In 100% of `saolei_chord_click` calls whose non-flag neighbors contain no `INITIAL` and no `UNKNOWN` cell (i.e. every non-flag neighbor is a revealed number or a mine), the chord is rejected before dispatch with the new reason code; the desktop receives no operation. A chord with at least one `INITIAL` (or `UNKNOWN`) non-flag neighbor dispatches normally. `FLAG` neighbors are never counted as action targets.
- **SC-008**: In 100% of cases where the recognized state is a win, any subsequent cell operation (`saolei_click` / `saolei_flag` / `saolei_chord_click`) is rejected before dispatch with `game_won` and the desktop receives no operation; `saolei_init` remains callable to restart. Win and loss terminal handling are symmetric (both reject further cell operations until a new game is started).

## Assumptions

- The desktop conversation renderer and the saolei-board library and the saolei MCP are the only surfaces touched; no new project, no proto change, no new external dependency is introduced. The think/tool bubble changes are confined to `ChatView.svelte` / `ChatMessage.svelte`; the win predicate is confined to `@dominion/game-saolei-board`'s `core/`; the game-status and chord-neighbor changes are confined to `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (+ its test).
- The classic Win32 Minesweeper win condition is: on a win, all unflagged mines are auto-flagged and all non-mine cells are revealed, so the winning board has **only** revealed numbers (`0..8`) and flags (`F`) — no `INITIAL`, `HIT_MINE`, `MINE`, or `UNKNOWN`. This is the rule the US3 predicate encodes; it is the standard behaviour of `winmine.exe` (the target the library recognises per its README).
- The existing loss signal (presence of `HIT_MINE` or `MINE`) is retained unchanged; US3 adds a win predicate alongside it, it does not alter loss detection. The two are disjoint: a board with `HIT_MINE`/`MINE` is a loss (win predicate returns false); a board with neither but with `INITIAL` is in-progress; a board with neither `INITIAL` nor `HIT_MINE`/`MINE`/`UNKNOWN` is a win.
- The hidden-scrollbar technique (US1) is a CSS-only concern (e.g. a webkit/firefox scrollbar-hidden rule scoped to `.thinking-content`); it does not change the scroll mechanism or the max-height. The exact CSS approach is a plan-time detail constrained by FR-001.
- The auto-scroll behaviour (US2) tracks the `.thinking-content` element's `scrollTop`/`scrollHeight` reactively as `part.thinking.content` updates (Svelte `$effect` / `$derived`); the "is the operator at the bottom?" check compares `scrollTop + clientHeight` to `scrollHeight` with a small tolerance. The exact reactive wiring is a plan-time detail constrained by FR-002/FR-003/FR-004.
- The compact-args rendering (FR-005) renders the raw `argsJson` as received (single-line); it does NOT re-`JSON.parse` + re-`stringify` (which would be a no-op for an already-compact string and a throw for invalid JSON). If the arriving `argsJson` is already pretty-printed upstream, the renderer may collapse it via `JSON.stringify(JSON.parse(argsJson))` (no indent) — the requirement is the **displayed** shape is compact; the exact technique is a plan-time detail.
- The collapsible result body (FR-007) uses the HTML `<details>`/`<summary>` element (or an equivalent Svelte-managed toggle), consistent with the existing screenshot sub-toggle. The default state is `closed`. The toggle is per-bubble (independent for each tool result).
- The game-status line position within the text outcome body (US4) is fixed and documented (settled in planning) so the model can parse it; the existing outcome line (`new game started` / `<tool> at (x,y) → dispatched` / `rejected: <reason>` / `unable to recognize board`) and the text-board body are preserved unchanged.
- The new chord-neighbor reason code (FR-019) is one new value in the `MoveRejection` union (e.g. `chord_no_unrevealed_neighbor`); the exact spelling is settled in planning, constrained by being stable, lowercase-snake-case, and self-explanatory to the model. The check excludes `FLAG` neighbors (marked mines the chord does not touch) and is lenient on `UNKNOWN` (treated as possibly unrevealed); it rejects only when no non-flag neighbor is `INITIAL` and no neighbor is `UNKNOWN`.
- The saolei MCP's per-session recognized-state lifecycle (025 FR-013) is unchanged: the win predicate is evaluated against the same in-memory recognized state, with no new persistence. On agent restart the state is lost together with the LLM checkpoint (025 assumption), so a win/loss is only signalled within a live session — consistent with the existing loss handling.
- A recognized win is a terminal state symmetric with a loss (Clarification Session 2026-07-27). The agent's terminal-state check (currently loss-only via `isTerminalState` at `saolei-mcp.ts:165-172`) is extended to include wins via the US3 predicate; a post-win cell operation is rejected with the new `game_won` reason (FR-021..FR-023), mirroring the existing `game_over` for losses. `saolei_init` is never blocked by a terminal state (it restarts the game). The exact refactor of `isTerminalState` (e.g. returning a terminal-kind vs. two separate checks) is a plan-time detail constrained by FR-022.
- The agent service remains the large-test SUT (Constitution principle VI); the desktop client is verified by build + unit + manual per `style/large_test.md`. The saolei-board library change is verified by its existing unit + golden test suite.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Repository-Internal References

- `specs/023-saolei-mcp-refine/spec.md` — the content-model split and tool-bubble design that US1/US2 refine; FR-007 (evolving tool bubble), the think/text part rendering path.
- `specs/024-tool-render-coord-fix/spec.md`, `specs/024-tool-render-coord-fix/data-model.md` — the tool-bubble renderer that US2 polishes (`.tool-bubble` / `.tool-args` / `.tool-result` classes, status classification).
- `specs/025-desktop-image-state-refine/spec.md`, `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md` — the recognized-text-state + strict-validation design that US3/US4/US5 build on; FR-012 (single text block), FR-015 (strict validation, including 015e chord-permitted-on-mismatched-flag-count), FR-016 (rejection contract), FR-018 (lenient on `UNKNOWN`).
- `projects/game/desktop/frontend/src/components/ChatView.svelte` — the tool-bubble renderer: `prettyArgs` (L179-186, pretty-prints args — gap 3a), `<span class="op-result-message">` (no `white-space: pre-wrap` — gap 3b), the always-expanded result body (L267-292 — gap 3c), the screenshot sub-`<details>` (L276-285).
- `projects/game/desktop/frontend/src/components/ChatMessage.svelte` — the think-bubble renderer: `.thinking-content` style (`max-height: 200px; overflow-y: auto` — gap 1, no auto-scroll — gap 2), the expand toggle (L47-54).
- `projects/game/desktop/frontend/src/App.svelte` L509-538 — the streaming text/thinking merge that grows `part.thinking.content` over time (the input to US1/US2 auto-scroll).
- `projects/game/pkg/saolei-board/src/core/types.ts` — `CellStatus` union, `GameState` (the inputs to the US3 win predicate).
- `projects/game/pkg/saolei-board/src/core/recognize.ts` — `recognizeBoard`, `SaoleiBoard` (the recognition engine whose output the US3 predicate classifies).
- `projects/game/pkg/saolei-board/src/core/index.ts` — the public barrel where the US3 predicate is exported.
- `projects/game/pkg/saolei-board/src/core/render.ts` — `renderBoardText`, `cellSymbol` (the text-board symbols the win predicate reasons over).
- `projects/game/pkg/saolei-board/src/core/validate.ts` — the existing `REVEALED` set (revealed numbers + `HIT_MINE`/`MINE` are permanent) — the conceptual basis for the win predicate's "all cells revealed/flagged" rule.
- `projects/game/pkg/saolei-board/README.md` — the library's public contract and symbol legend; US3 extends it with a win predicate.
- `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` — `isTerminalState` (L165-172, loss-only — gap 4), `validateMove` (L189-230, chord-permitted-any-number — gap 6), `MoveRejection` union (L110-117), `initSuccessText`/`dispatchedText`/`rejectionText`/`unrecognizableText` (L307-346, no game-status line — gap 5).
- `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` — the DI-based test pattern (fake `OperationBridge` + fake `SaoleiBoardApi`) that US3/US4/US5 tests extend.
- `projects/game/agent/src/skill/saolei/SKILL.md` — the built-in saolei skill; US4/US5/FR-024 require updating the symbol-legend / outcome-line / rejection-reason documentation to include the new game-status line, the new `game_won` post-win rejection reason, and the new chord rejection reason (the skill is the model's authority on the result format).

### External

- Classic Microsoft Minesweeper (`winmine.exe`) win condition — on a win, all unflagged mines are auto-flagged and all non-mine cells are revealed; this is the rule the US3 predicate encodes. The library's existing recognition targets this implementation (per its README). No single normative external document is newly authoritative; the rule is common knowledge for the game and is already the basis of the library's `HIT_MINE`/`MINE`/`FLAG` semantics.
- CSS scrollbar-styling (US1) — the standard cross-browser technique to suppress the visible scrollbar while keeping scroll capability (e.g. `scrollbar-width: none` for Firefox; `::-webkit-scrollbar { display: none }` for WebKit/Chromium; Wails v2 uses WebKit on Windows). Background only; the exact rule is a plan-time detail constrained by FR-001.
