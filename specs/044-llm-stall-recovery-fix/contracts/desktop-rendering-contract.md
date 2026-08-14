# Contract: Desktop Rendering — WarnSignal Bubble & Interrupted Indicator

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](../spec.md) FR-012/FR-013

This contract defines two desktop rendering behaviors and reconciles the proto content-model comment. It spans the agent (no change to warn emission) and the desktop frontend (formalize + extend).

## §1 Two distinct signals (lifecycle summary)

| Signal | Carrier | Channel | Lives in checkpoint? | Visible when |
|---|---|---|---|---|
| ⚠ warn bubble (FR-012) | `FlowPart.warn` (WarnSignal) | live Connect stream (`flowParts`) | **No** — FlowParts never persist to history (`projects/game/game.proto:741`, `desktop/frontend/src/api.ts:398`) | during the stall (live only — gone after reconnect) |
| interrupted indicator (FR-013) | `TextPart.completion`/`ThinkingPart.completion` proto enum field (`PART_COMPLETION_INTERRUPTED`) on a persisted `Message` content block | `ListMessages` (history) | **Yes** | after reconnect (and live, once the block is seeded) |

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

**Marker on the wire — proto enum field (FR-010 controlled exception)**: the interrupted marker is carried by a **formal proto enum field**, `PartCompletion`, added to `TextPart` and `ThinkingPart` in `projects/game/game.proto` (field number 2 on each; both currently have only `string content = 1`). This is a **controlled exception** to FR-010's "no proto wire change" boundary — authorized by the user for this single purpose (the interrupted marker). All other proto wire remains unchanged (see plan.md Constitution Check).

Proto definition (design draft — the developer implements this verbatim in `game.proto`):

```proto
// Describes whether a content part was fully produced or truncated mid-stream.
// Set on the tail content block of a partial reply persisted after a stream
// stall (spec 044 FR-005): the block mid-stream when the stall terminated the
// turn carries PART_COMPLETION_INTERRUPTED. All other parts — including earlier
// fully-streamed blocks in the same partial — carry the implicit
// PART_COMPLETION_UNSPECIFIED (a complete part). protojson omits the zero value,
// so a normal part serializes without the field; clients predating this field
// see no field (forward-compatible).
enum PartCompletion {
  PART_COMPLETION_UNSPECIFIED = 0;
  PART_COMPLETION_INTERRUPTED = 1;
}
```

`TextPart`/`ThinkingPart` each gain `PartCompletion completion = 2;` (see data-model.md §4 for the field placement rationale — Option B: on the two part kinds that can be mid-stream, not on `MessagePart`).

**Why a proto field (not loose JSON)**: the prior design assumed the marker could ride a "lenient JSON channel" (an additive `interrupted:true` the desktop tolerates as an extra field). This was proven infeasible — every hop on the network path strips unknown fields: (1) the agent serializes via `@grpc/proto-loader` against `game.proto` (no `interrupted` field → dropped at serialize); (2) proxy `grpc-go` is strict proto; (3) gateway `grpc-gateway` protojson emits only known fields; (4) the desktop Go client `protojson.UnmarshalOptions{DiscardUnknown: true}` (`projects/game/desktop/internal/api/client.go:312`) discards unknown fields; (5) `view_model.go` `protoToJSONMap` marshals via strict `protojson.Marshal` (`projects/game/desktop/view_model.go:222-234`) — only known fields survive. A marker that is not a declared proto field therefore cannot cross the network. Making it a declared field means every hop preserves it naturally (known field → not discarded, included in strict marshal). The Go layers need **no logic change** — the field is a known proto field, so `DiscardUnknown` keeps it and strict `protojson.Marshal` emits it.

**Wire shape (protojson)**: because protojson omits zero-value enums, a normal part serializes as `{"text": {"content": "..."}}` (no `completion` field), while an interrupted part serializes as `{"text": {"content": "...", "completion": "PART_COMPLETION_INTERRUPTED"}}` (or `thinking` variant). This matches the `ToolResultStatus` precedent (`projects/game/proto_test.go:380` confirms protojson emits enum-name strings; `api.ts:242` documents zero-value omission).

**Translation point**: the marker originates in the agent checkpoint as `additional_kwargs.interrupted = true` on an AIMessage content-block (set by `mergePartialBlocks` in `session-team.ts` — the checkpoint layer, which survives the `MemorySaver` serde). The agent's `ListMessages` reconstruction (`handler.ts:804-847`) **translates** that checkpoint-layer marker into the proto `completion` field on the emitted `TextPart`/`ThinkingPart` (see partial-output-contract.md §4).

**Desktop changes**:
- `api.ts`: add `PartCompletion` enum + `completion?: PartCompletion | string` on `TextPart`/`ThinkingPart` (mirrors the `ToolResultStatus` pattern).
- `ChatView.svelte` agent text bubble (`:284-292`): when the text part carries `completion === INTERRUPTED`, render a "中断" indicator (a small badge/suffix inside the bubble, reusing the `.warn-bubble` visual language).
- `ChatMessage.svelte` thinking bubble (`:104-115`): same indicator on an interrupted thinking part.
- A pure helper `partInterrupted(part): boolean` (in a `.ts` module, e.g. `api.ts` alongside `classifyToolResultStatus`) reads the enum — this is the unit-testable surface (the desktop `lib_test` only globs `.ts`; no Svelte mount). See tasks.md T009.

**Semantics**: ONLY the interrupted block is marked — earlier completed blocks in the same partial reply are unmarked (spec FR-005). Live streaming never carries the marker (a live stall surfaces via the transient ⚠ warn bubble); `stream-merge.ts` is therefore unaffected (the `appendToEntry` live-merge path rebuilds the trailing part without `completion`, which is correct — interrupted never appears live).

## §4 Verification

- Desktop unit (pure helper, `.ts`): `partInterrupted({ text: { content, completion: "PART_COMPLETION_INTERRUPTED" } })` → `true`; a normal part (no `completion` / `UNSPECIFIED`) → `false`; both text and thinking variants; defensive against numeric and string enum forms (mirrors `classifyToolResultStatus`). Run `bazel test //projects/game/desktop/frontend:lib_test`.
- Proto round-trip (`projects/game/proto_test.go`): a `TextPart`/`ThinkingPart` with `completion = PART_COMPLETION_INTERRUPTED` round-trips through `protojson.Marshal`/`Unmarshal`; the JSON contains `"PART_COMPLETION_INTERRUPTED"`; a default `TextPart` (UNSPECIFIED) omits the field. (Extends the existing protojson test pattern at `proto_test.go:370-399`.)
- Large test: stall mid-reply → ⚠ warn bubble appears live → reconnect → `ListMessages` returns the partial with the interrupted part's `completion` field set → desktop shows the partial reply with the interrupted indicator (spec SC-003). The Go large-test assertion reads `part.GetText().GetCompletion()` (or `GetThinking().GetCompletion()`) `== game.PartCompletion_PART_COMPLETION_INTERRUPTED`.
- Proto: the `game.proto:451-453` comment reflects the `warn` exception (diff review, T010); the new `PartCompletion` enum + `TextPart`/`ThinkingPart` fields are present with AIP-192 comments.
