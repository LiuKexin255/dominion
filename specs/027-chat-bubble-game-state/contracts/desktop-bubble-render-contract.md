# Contract: desktop chat bubble rendering (think + tool)

**Feature**: `027-chat-bubble-game-state` | **Date**: 2026-07-27 | **Spec**: [spec.md](../spec.md)

This contract fixes the desktop-frontend rendering rules for US1 (think bubble) and US2 (tool bubble). It is the authoritative interface description for the two Svelte components; the reactive-state shapes are in [data-model.md](../data-model.md) §6/§7; the decisions in [research.md](../research.md) D1..D5.

It **refines** (does not replace) the bubble renderers made functional by [024 — Tool Bubble Rendering](../../024-tool-render-coord-fix/spec.md): the part-model (which `MessagePart` kinds exist, how a tool call + result merge into one evolving bubble keyed by `tool_id`, the status classification) is unchanged. Only the *presentation* of the think content area and the tool args/result changes — no new `MessagePart` kind, no proto change, no content-model change.

---

## §1 — Think bubble: hidden scrollbar (US1 / FR-001)

**Surface**: `projects/game/desktop/frontend/src/components/ChatMessage.svelte`, the `.thinking-content` element (the `<pre>` inside the expanded thinking bubble).

**Rule**: when the thinking content overflows the area's `max-height` (200 px), the platform-default scrollbar (track + thumb) on the right edge MUST NOT be visible. The area MUST remain scrollable (wheel / trackpad / keyboard).

**Mechanism** (CSS-only — D1):

```css
.thinking-content {
  /* existing rules unchanged: max-height: 200px; overflow-y: auto; … */
  scrollbar-width: none;                  /* Firefox / standard (CSS Scrollbars Styling L1) */
}
.thinking-content::-webkit-scrollbar {
  display: none;                          /* WebKit / Chromium — operative on Wails v2 WebView2 */
}
```

**Invariants**:
- `overflow-y: auto` and `max-height: 200px` are UNCHANGED — only the visible scrollbar UI is suppressed.
- A content area that fits within `max-height` (no overflow) shows no scrollbar either way (no change for short content — FR-001 acceptance 5).
- No JS is used to hide the scrollbar; the rule is pure CSS scoped to `.thinking-content` (does not affect the chat thread, the tool-result body, or any other scroll area).

**External references**:
- `scrollbar-width` — https://developer.mozilla.org/en-US/docs/Web/CSS/scrollbar-width
- `::-webkit-scrollbar` — https://developer.mozilla.org/en-US/docs/Web/CSS/::-webkit-scrollbar

---

## §2 — Think bubble: auto-scroll (US1 / FR-002..004)

**Surface**: same `.thinking-content` element, reactively driven as `part.thinking.content` grows (the streaming merge in `projects/game/desktop/frontend/src/App.svelte` folds consecutive `ThinkingPart`s into one trailing part — spec Motivation gap 2).

**Rules**:

| # | Rule | FR |
|---|---|---|
| 1 | When the bubble is **expanded** (`expanded` flips false→true), the content area opens scrolled to the bottom (`scrollTop = scrollHeight`) of the current content, on `requestAnimationFrame`. | FR-004 |
| 2 | When the bubble is expanded **and** `part.thinking.content` grows, IF the operator is at the bottom (`scrollTop + clientHeight >= scrollHeight − TOLERANCE`, `TOLERANCE = 8` px), scroll to bottom inside `tick().then(...)`. The at-bottom test MUST be evaluated **before** the DOM update (i.e. inside `$effect.pre`), against the `scrollHeight` the operator currently sees. | FR-002 |
| 3 | When the operator has scrolled UP away from the bottom (the at-bottom test is false), do NOT scroll on content growth — auto-scroll pauses. | FR-003 |
| 4 | When the operator scrolls back to the bottom, the at-bottom test becomes true again and auto-scroll resumes (rule 2). | FR-003 |

**Mechanism** (Svelte 5 runes — D2): two reactive hooks in `ChatMessage.svelte`, split by job:
- a regular `$effect` keyed on `expanded` → rule 1 (open-to-bottom). The `<pre>` is mounted during the preceding DOM-update phase, so `contentEl` is bound by the time the effect runs; `requestAnimationFrame` defers the scroll one paint so layout is final.
- an `$effect.pre` keyed on `part.thinking.content` → rules 2..4 (follow-if-at-bottom, else pause). `$effect.pre` runs **before** the DOM update, so `scrollHeight` is the height the operator currently sees and `scrollTop` is the operator's true position — the at-bottom test is meaningful. `tick().then(...)` then scrolls after the DOM update lands the new bottom.

**Why follow-or-pause MUST use `$effect.pre` + `tick()`, not `$effect` + `requestAnimationFrame`**: a regular `$effect` runs *after* the DOM update. By then `scrollHeight` already reflects the newly-appended reasoning while `scrollTop` is still the operator's pre-update position, so `scrollTop + clientHeight >= scrollHeight − TOLERANCE` is false on every content growth — the bubble freezes at the top and never follows the stream. `$effect.pre` reads `scrollHeight` before the new content renders, so the at-bottom test reflects the operator's true position; `tick().then(...)` performs the scroll after the DOM update. This is the Svelte autoscroll pattern (https://svelte.dev/docs/svelte/$effect#$effect.pre).

**Invariants**:
- A collapsed bubble does not auto-scroll (no observable effect — the content is hidden).
- The pause/resume is governed solely by the at-bottom test; there is no separate "paused" flag to manage.
- `TOLERANCE` (8 px) absorbs sub-pixel float jitter so "at the bottom" is stable across browsers.

---

## §3 — Tool bubble: compact args (US2 / FR-005)

**Surface**: `projects/game/desktop/frontend/src/components/ChatView.svelte`, the tool-call head (`.tool-head`).

**Rule**: a tool call's input arguments render **compact** — a multi-key JSON object renders on one line (e.g. `saolei_flag {"x":7,"y":7}`), NOT pretty-printed with indentation that splits each key onto its own line.

**Mechanism** (D3): replace the existing `prettyArgs(argsJson)` (`JSON.stringify(JSON.parse(argsJson), null, 2)` — the 2-space pretty-print) with:

```ts
function compactArgs(argsJson?: string): string {
  if (!argsJson) return "";
  try { return JSON.stringify(JSON.parse(argsJson)); }   // compact, no indent
  catch { return argsJson; }                              // invalid JSON → raw string
}
```

Render inline with the tool name: `<span class="tool-name">{name}</span> <code class="tool-args-inline">{compactArgs(argsJson)}</code>`. The existing `.tool-args` `<pre>` block (block-level, multi-line) is removed/replaced by the inline `<code>`.

**Invariants**:
- Invalid-JSON `argsJson` falls back to the raw string (no throw, no pretty-print attempt) — FR-005.
- Empty/absent `argsJson` renders nothing extra (just the tool name) — unchanged from today.
- The tool name remains visible at all times (it is in `.tool-head`, outside the collapsible result body — §5).

---

## §4 — Tool bubble: formatted result message (US2 / FR-006)

**Surface**: the result message element inside the tool bubble.

**Rule**: a tool result's message MUST preserve its native formatting — newlines and multi-line structure are visible. A multi-line saolei text board (`board size 9*9\n\n* * * …`) renders as a readable multi-line grid, NOT a run-on single line.

**Mechanism** (D4): render the message in a `<pre class="op-result-message">` with:

```css
.op-result-message {
  white-space: pre-wrap;     /* preserve newlines, wrap overlong lines */
  word-break: break-word;    /* break unbroken long tokens */
  /* font, size, color per existing aesthetic */
}
```

(The existing `<span class="op-result-message">` — which has no `white-space` rule and collapses `\n` — is replaced by this `<pre>`.)

**Invariants**:
- Newlines in the message are preserved regardless of source (saolei text board, native-tool message).
- An empty message renders an empty body (no broken layout) — spec Edge Cases.

---

## §5 — Tool bubble: collapsible result body (US2 / FR-007/008)

**Surface**: the resolved tool-result body inside `.tool-bubble`.

**Rule**: the result body (status message + any text board) is **collapsed by default** behind a toggle. The always-visible part is the status icon + label only (e.g. `✓ done` / `✗ failed` / `› done`). Expanding the toggle reveals the full formatted message. The screenshot (native mouse tools) stays behind its own existing sub-toggle, independent of the new body toggle.

**Mechanism** (D5): wrap the resolved result in a `<details>` with no `open` attribute (collapsed by default):

```
.tool-bubble(.tool-resolved-success | -failure | -neutral)
  .tool-head
    span.tool-name
    code.tool-args-inline                    ; §3 compact args
  details.tool-result-details                ; collapsed by default (no `open`)
    summary
      span.op-result-icon                    ; ✓ (succeeded) | ✗ (failed) | › (neutral)
      span.op-result-status                  ; "succeeded" | "failed" | "done"
    pre.op-result-message                    ; §4 formatted message (only if message non-empty)
    details.op-result-screenshot-details     ; existing screenshot sub-toggle (unchanged)
      summary
      img.screenshot-img
```

The pending state (`running…`, shown when the result has not arrived) renders OUTSIDE the `<details>` — there is nothing to collapse before resolution.

**Invariants**:
- Default state of the result-body `<details>` is **closed** (FR-007). The status icon + label in `<summary>` are always visible.
- The screenshot `<details>` keeps its own independent open/closed state; expanding the result body MUST NOT force the screenshot open, and vice versa (FR-008).
- Native (mouse) tool results, which carry `SUCCEEDED`/`FAILED` and a screenshot, render through the same structure — the screenshot is reachable by expanding the body, then the screenshot sub-toggle.
- The status classification (`classifyToolResultStatus`, from 024) is UNCHANGED — `succeeded` / `failed` / neutral (absent-or-`UNSPECIFIED` → neutral). Only the container's default-collapsed state and the message formatting change.

---

## §6 — What is NOT changed

- The `MessagePart` content model (text / thinking / image / tool_call / tool_result) and the `FlowPart` control channel — unchanged (no new kind, no proto change).
- The evolving-bubble grouping (one bubble per `tool_call.id`, updated in place by the matching `tool_result`) — unchanged (023 FR-007 / 024).
- The status classification logic (`classifyToolResultStatus`: succeeded/failed/neutral, absent→neutral) — unchanged (024 FR-002..004).
- The chat-thread auto-scroll (`ChatView.svelte` `scrollContainer`) — unchanged; the new auto-scroll is scoped to `.thinking-content` only.
- The tool-bubble pending state (`running…`) — unchanged; only the resolved body becomes collapsible.
- The agent-side text-result body content (the `game status:` line, etc.) — that is the agent's contract ([saolei-mcp-status-contract.md](./saolei-mcp-status-contract.md)); the desktop renders whatever text the agent emits, preserving its formatting (§4).
