# Contract: Desktop Rendering — WarnSignal Bubble & Interrupted Indicator

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](../spec.md) FR-012/FR-013

This contract defines two desktop rendering behaviors and reconciles the proto content-model comment. It spans the agent (no change to warn emission) and the desktop frontend (formalize + extend).

## §1 Two distinct signals (lifecycle summary)

| Signal | Carrier | Channel | Lives in checkpoint? | Visible when |
|---|---|---|---|---|
| ⚠ warn bubble (FR-012) | `FlowPart.warn` (WarnSignal) | live Connect stream (`flowParts`) | **No** — FlowParts never persist to history (`projects/game/game.proto:741`, `desktop/frontend/src/api.ts:398`) | during the stall (live only — gone after reconnect) |
| interrupted indicator (FR-013) | `additional_kwargs.interrupted` on a persisted `Message` content block | `ListMessages` (history) | **Yes** | after reconnect (and live, once the block is seeded) |

There is exactly ONE `warn` mechanism — the `WarnSignal` FlowPart (`game.proto:505,644-648`), emitted by the stall-recovery terminal (`projects/game/agent/src/turn-loop.ts:522` `warnFrame()`). (`warn()` from `@dominion/common-js-logs` is a separate server-side log function — unrelated.)

Because the ⚠ warn bubble is **transient**, it CANNOT serve as the surviving truncation marker — that is why the interrupted flag MUST ride on the persisted `Message` (partial-output-contract.md §4).

## §2 FR-012 — WarnSignal ⚠ bubble (standardize + reconcile)

**Behavior**: the desktop renders every `FlowPart.warn` (WarnSignal) as a conversation warning bubble — the current idleTimeout-style rendering. This is the standard for ALL warn sources.

**Existing implementation (formalized, no agent change)**:
- `projects/game/desktop/frontend/src/App.svelte:789-802` — on `fp.warn`, builds a chat entry `{ messageId, role: AGENT, timestamp, warnMessage: fp.warn.message }`.
- `projects/game/desktop/frontend/src/components/ChatView.svelte:271-279` — renders `.msg-row.msg-warn` > `.msg-bubble.warn-bubble` with a ⚠ icon (`&#9888;`) and the message text.

**Proto reconciliation**: the comment at `projects/game/game.proto:451-453` (spec 023: "FlowPart … control only — NEVER rendered as conversation entries … A FlowPart is consumed, not displayed") is updated to document `warn` as the **rendered exception**:

> FlowParts are never rendered as conversation entries, with one documented exception: `WarnSignal` (`warn`) is surfaced by the desktop as a distinct warning bubble (⚠) in the conversation view — it is a control signal that carries a user-facing recoverable-error notice. All other FlowPart kinds remain consumed, not displayed.

This is a comment-only change to `game.proto` (no wire-format change). Operation/keyboard FlowParts and `wait`/`status`/`queue` signals remain non-rendered.

## §3 FR-013 — Interrupted indicator (render after reconnect)

**Behavior**: when `ListMessages` returns a `MessagePart` carrying the interrupted marker (set by partial-output-contract.md §4), the desktop renders a visual "中断"/truncated indicator on that bubble, so the user can tell the reply was cut off by a stall — visible after reconnection (not only during the live stall via the ⚠ warn bubble).

**Marker on the wire**: the `MessagePart` carries `interrupted: true` (propagated from the AIMessage content block's `additional_kwargs.interrupted` by the `ListMessages` reconstruction in `handler.ts:668-717`). Exact proto/JSON shape: an additive boolean on the `TextPart`/`ThinkingPart` payload (e.g. `{ text: { content, interrupted: true } }`). If the proto `TextPart`/`ThinkingPart` messages do not carry an `interrupted` field, the marker is carried as part of the JSON the desktop already reads leniently (`App.svelte`/`api.ts` tolerate extra fields); a formal proto field can be added in a follow-up if needed. (This keeps the change out of the proto wire format for v1 — the desktop reads the additive field; tasks.md confirms.)

**Desktop changes**:
- `ChatView.svelte` render branch: a part with `interrupted: true` renders an additional "中断" indicator (e.g., a small badge or suffix) inside the bubble.
- `App.svelte` history-seed path: when building chat entries from `ListMessages`, the `interrupted` marker on a part flows into the rendered entry (the entry already carries per-part data).

**Semantics**: ONLY the interrupted block is marked — earlier completed blocks in the same partial reply are unmarked (spec FR-005).

## §4 Verification

- Desktop component test: a `warn` FlowPart renders a ⚠ bubble; a `ListMessages` part with `interrupted:true` renders the interrupted indicator; a normal part renders no indicator.
- Large test: stall mid-reply → ⚠ warn bubble appears live → reconnect → `ListMessages` returns the partial with the interrupted part → desktop shows the partial reply with the interrupted indicator (spec SC-003).
- Proto: the `game.proto:451-453` comment reflects the `warn` exception (diff review).
