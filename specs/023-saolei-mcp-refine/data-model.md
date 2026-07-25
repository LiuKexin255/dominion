# Data Model: Conversation Content-Model Refactor & Saolei MCP Simplification

**Feature**: 023-saolei-mcp-refine | **Date**: 2026-07-25 | **Spec**: [spec.md](../spec.md) | **Research**: [research.md](../research.md) | **Proto contract**: [contracts/content-model-contract.md](../contracts/content-model-contract.md) | **Dispatch contract**: [contracts/tool-dispatch-contract.md](../contracts/tool-dispatch-contract.md)

This document describes the data entities after the refactor. The authoritative proto shapes are in [contracts/content-model-contract.md](../contracts/content-model-contract.md); this document covers the conceptual model, the live≡history invariant, the evolving-bubble view model, the tool-result status provenance, and the (removed) saolei state model.

---

## §1 — Content categories (the spine)

The single `Part` oneof is split into two disjoint categories. A content block belongs to exactly one (spec FR-001).

### MessagePart (display only)

What the conversation renders and what `Message.content` carries. Carried live by `AgentFrame.message_parts` and replayed from history by `Message.content` (both are `MessageParts { repeated MessagePart }`).

| Kind | Field | Purpose |
|---|---|---|
| `text` | `TextPart.content` | Visible text (user or agent). |
| `thinking` | `ThinkingPart.content` | Reasoning / intermediate process. |
| `image` | `ImagePart` | An image (e.g. user-attached screenshot). |
| `tool_call` | `ToolCallPart` (new) | The model's tool invocation: `tool_id`, `name`, `args_json`. |
| `tool_result` | `ToolResultPart` | The outcome of a tool call: `tool_id`, `status`, `message`, `screenshot`. |

### FlowPart (control only — never rendered in the conversation)

Drives desktop execution and turn control. Carried by `AgentFrame.flow_parts` (`FlowParts { repeated FlowPart }`). Never appears in `Message.content`. Never appended to the chat stream as a renderable entry (operations); signal FlowParts reach the frontend only to drive state (typing/warning), not bubbles.

| Kind | Field | Purpose |
|---|---|---|
| `mouse_move` | `MouseMovePart` | Move cursor (desktop executes). |
| `mouse_click` | `MouseClickPart` | Click at cursor (desktop executes). |
| `keyboard_press` | `KeyboardPressPart` | Press a key (desktop executes). |
| `mouse_move_and_click` | `MouseMoveAndClickPart` | Atomic move+click (desktop executes). |
| `wait` | `WaitSignal` | Agent finished turn, awaiting input. |
| `warn` | `WarnSignal` | Recoverable warning. |
| `status` | `StatusSignal` | Lifecycle/connectivity (ACTIVE/IDLE). |

`WaitSignal`/`WarnSignal`/`StatusSignal` keep their existing field shapes (`projects/game/game.proto:367`/`372`/`390`); they move from the `AgentFrame.payload` oneof into `FlowPart` kinds (spec C3 / FR-003). The operation Part messages (`MouseMovePart`, etc.) keep their existing fields including `tool_id`, `MouseInputMethod`, `KeyboardKey`, `MouseClickAction` — unchanged from 018 (`specs/018-saolei-mcp/contracts/proto-operation-contract.md`).

---

## §2 — `ToolCallPart` (new)

```proto
message ToolCallPart {
  string tool_id   = 1;  // LangChain tool_call.id — links call↔result↔FlowPart op (FR-008)
  string name      = 2;  // tool name, e.g. "saolei_click"
  string args_json = 3;  // JSON.stringify(tool_call.args) — display-only (research.md D3)
}
```

- `tool_id` is the single correlation key shared with the `tool_result` MessagePart and the `FlowPart` operation the call dispatches (spec FR-008).
- `args_json` is the model's arguments verbatim; display-only. Example: a `saolei_click(x=3,y=4)` call → `args_json = "{\"x\":3,\"y\":4}"`.

---

## §3 — `AgentFrame.payload` and `Message.content` (restructured)

```proto
message AgentFrame {
  // ... session_id, frame_id, create_time, sender, agent_profile_name unchanged
  oneof payload {
    MessageParts message_parts = 11;  // display channel
    FlowParts    flow_parts    = 12;  // control channel
  }
  reserved 10, 20, 21, 22;            // old content / wait / warn / status (clean break, C2)
  reserved "content", "wait", "warn", "status";
}

message Message {
  // ... name, message_id, sender, create_time unchanged
  MessageParts content = 5;           // was PartBlock (display blocks only)
}

message MessageParts { repeated MessagePart parts = 1; }
message FlowParts    { repeated FlowPart    parts = 1; }
```

(Field numbers are pinned in [contracts/content-model-contract.md](../contracts/content-model-contract.md).) A frame carries exactly one payload case — either a batch of display blocks or a batch of control blocks. In practice the agent emits one entry per frame (one tool_call, one operation, one signal), but the repeated shape allows batching without a schema change.

---

## §4 — The live≡history invariant (single source of truth); channels decoupled (D10)

Live frames and replayed history both project from the **same** LangChain `BaseMessage` stream, so they render identically (spec FR-009). The **conversation channel** (display) and the **operation channel** (control) are **decoupled** — they do not share an id (research.md D10):

| `BaseMessage` (checkpoint) | Live emission (turn loop) | History reconstruction (`ListMessages`) |
|---|---|---|
| `HumanMessage` | `message_parts` frame: `text` (+`image` if screenshot attached) | `Message` content: `text` (+`image`) |
| `AIMessage` text/reasoning blocks | `message_parts` frame: `text` / `thinking` | content: `text` / `thinking` (from `contentBlocks`) |
| `AIMessage.tool_calls[]` | `message_parts` frame: one `tool_call` per entry (conversation-channel `tool_call.id`) | content: one `tool_call` per entry (`tool_id=call.id`, `name`, `args_json`) |
| `ToolMessage` | `message_parts` frame: `tool_result` (`tool_id`=`tool_call_id`, status from `additional_kwargs.toolResultStatus` — real for native tools, neutral for MCP/saolei per D12, message + screenshot from content blocks) | content: `tool_result` (same reconstruction) |
| operation dispatched by a tool | `flow_parts` frame: the `MouseMoveAndClickPart`/`KeyboardPressPart` (operation-channel **bridge-minted** `tool_id`, independent of `tool_call.id`) | **not reconstructed** — operations are control-only; history shows the `tool_call` (semantic) instead |

The operation `FlowPart` is live-only (the desktop must execute it); it carries a bridge-minted id for dispatch↔result correlation, NOT the conversation `tool_call.id`. History shows the *semantic* `tool_call` instead — which is the whole point of the split (live and history now show the same semantic call, not physical-vs-semantic). The decoupling means MCP tools (saolei) dispatch without needing the LangChain `tool_call.id` (D10).

---

## §5 — Evolving tool bubble (conversation channel) + Debug drawer (operation channel)

> **Decoupled (D10/D11):** the conversation bubble groups by the LangChain `tool_call.id`; the debug Confirm is a **separate session-top drawer** on the operation channel (bridge-minted id). They no longer associate via a shared id.

### Conversation bubble (grouped by conversation-channel `tool_call.id`)

One conversation bubble per `tool_call.id` (spec FR-007 / C5). State machine:

```
[ none ]
   | tool_call MessagePart (tool_id=T [= LangChain tool_call.id], name, args_json)
   v
[ call ] ── shows: name + args_json
   | tool_result MessagePart (tool_id=T, status, message, screenshot)
   v
[ resolved ] ── shows: name + args_json + status + message + screenshot
```

- Grouping key: the conversation-channel `tool_call.id` (shared by `ToolCallPart.tool_id` and `ToolResultPart.tool_id`). For native tools this is `config.toolCall.id`; for MCP (saolei) tools it is the `tool_call_id` the adapter sets on the `ToolMessage` — LangChain wires both automatically (D2 revised).
- This id is NOT the operation-channel id (D10). The bubble knows nothing about the dispatched FlowPart operation.
- A `tool_result` whose `tool_id` has no preceding `tool_call` (graceless history edge) creates the bubble from the result alone (no call header).
- A `tool_call` whose `tool_result` never arrives (turn aborted, or debug hold not yet released) stays in `[ call ]` — not a half-updated inconsistent state (spec Edge Case).
- Multiple tool calls in one turn → one bubble each, updated independently by `tool_call.id` (spec Edge Case).

### Debug Confirm drawer (operation channel — D11)

The 022 debug Confirm is **re-anchored off the bubble** onto a session-top drawer driven by the operation channel. When the desktop holds an operation result (debug mode), it emits `game:debug:result-held { toolId, operation: { kind, summary, details } }` where `toolId` is the **operation-channel** bridge-minted id (NOT the conversation `tool_call.id`). The frontend renders a drawer at the top of the session chat view: one row per held operation (operation `summary` + a Confirm button). Clicking Confirm calls `ConfirmToolResult(toolId)` (releases that held result); `game:debug:result-released { toolId, reason }` dismisses the row. The conversation `ChatView` is NOT involved (no `heldToolIds`/`onConfirm` props). Full interface: [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md).

During a debug hold the conversation bubble stays in `[ call ]` (the agent is blocked waiting for the tool result, so no `tool_result` is produced yet); it reaches `[ resolved ]` only after the hold releases and the agent produces the tool's LLM result. The screenshot is NOT shown during the hold (C12). The execution outcome is reachable in the log (C7/FR-011).

---

## §6 — Tool-result status provenance (the history fix; native vs MCP — D12)

The real `ToolResultStatus` flows into the checkpoint **for native tools**:

```
desktop executes operation
   → ToolResultPart{ status, message, screenshot }  (over WS, desktop→agent, operation-channel tool_id)
   → OperationBridge.handleResult resolves OperationResult{ status, ... }
   → [native tool] returns ToolMessage{ content=blocks, additional_kwargs.toolResultStatus=status,
                                tool_call_id=config.toolCall.id, name }  (buildToolResultMessage, D4)
   → MemorySaver checkpoint persists the ToolMessage (content + additional_kwargs)
   → ListMessages reads additional_kwargs.toolResultStatus → tool_result MessagePart.status
```

For **MCP tools** (saolei) the handler returns MCP content blocks; the `@langchain/mcp-adapters` client builds the `ToolMessage` **without** `additional_kwargs`, so the status is neutral (D12). This fixes the original spurious-FAILED bug (the text-heuristic is gone) and is FR-014-compliant.

Status resolution table (spec FR-013..FR-015):

| Tool kind | Real status (from desktop) | `additional_kwargs.toolResultStatus` | Rendered history status |
|---|---|---|---|
| **native (mouse)** | `SUCCEEDED` | `TOOL_RESULT_STATUS_SUCCEEDED` | succeeded |
| **native (mouse)** | `FAILED` | `TOOL_RESULT_STATUS_FAILED` | failed |
| **native (mouse)** | unavailable / absent | (absent) → default `TOOL_RESULT_STATUS_UNSPECIFIED` | neutral/unspecified — **never** `FAILED` |
| **MCP (saolei)** | any (status not carried) | (absent) → `TOOL_RESULT_STATUS_UNSPECIFIED` | neutral — **never** `FAILED` (D12) |

The text-heuristic `inferToolResultStatus` (current `projects/game/agent/src/handler.ts`) is **removed**. No fallback infers status from result text; the only fallback is neutral (FR-015). For saolei the actual outcome remains visible via the result message text + the returned screenshot; only the structured status badge is neutral.

---

## §7 — Saolei tool set (stateless)

Four tools, each a pure dispatch-and-return over `OperationBridge` (`bridge.dispatch(part, signal)` — no toolId, D10). No per-session mutable state (spec FR-016..FR-022). Each returns MCP content blocks (text + screenshot); the adapter-wrapped `ToolMessage` carries **neutral** status (D12):

| Tool | Args | Dispatches (proto FlowPart) | Returns |
|---|---|---|---|
| `saolei_init` | (none — `width`/`height` dropped, C11) | `KeyboardPressPart{ key: KEYBOARD_KEY_F2 }` | MCP content blocks → `ToolMessage` (text + screenshot, **neutral** status) |
| `saolei_click` | `x`, `y` (grid) | `MouseMoveAndClickPart{ xPx: centerX(x), yPx: centerY(y), click: LEFT_CLICK, method: WINDOW_MESSAGE }` | MCP content blocks → `ToolMessage` (neutral) |
| `saolei_flag` | `x`, `y` | `MouseMoveAndClickPart{ ..., click: RIGHT_CLICK, method: WINDOW_MESSAGE }` | MCP content blocks → `ToolMessage` (neutral) |
| `saolei_chord_click` | `x`, `y` | `MouseMoveAndClickPart{ ..., click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }` | MCP content blocks → `ToolMessage` (neutral) |

Grid→pixel geometry is the unchanged fixed formula (`projects/game/agent/src/mcp/saolei/geometry.ts`): `centerX(x) = 24 + x*32 + 16`, `centerY(y) = 200 + y*32 + 16`. The desktop-facing operation contract is unchanged from 018 (FR-020 / Assumption).

`saolei_update`, `GameState`, `CellStatus`, `LastOp`, the alternation (`pendingUpdate`/`lastOp`), and the validators (`validation.ts`) are **removed**. Tools are callable back-to-back with no intervening step (FR-021); an out-of-bounds coordinate dispatches to whatever pixel the fixed formula yields and the returned screenshot is the model's feedback (spec Edge Case — accepted tradeoff of removing agent-side validation).

---

## §8 — Removed entities (cleanup)

| Entity | Location | Disposition | Reason |
|---|---|---|---|
| `Part` (oneof), `PartBlock` | `game.proto:271`/`278` | removed | replaced by `MessagePart`/`FlowPart` + `MessageParts`/`FlowParts` (D1) |
| `AgentFrame.payload` cases `content`/`wait`/`warn`/`status` | `game.proto:412` | removed (reserved) | replaced by `message_parts`/`flow_parts`; signals became FlowPart kinds (D1) |
| `inferToolResultStatus`, `toolCallToPart`, `reconstructToolResult` | `handler.ts:776`/`756`/`795` | removed | replaced by real-status path (D4) + `ToolCallPart` reconstruction (D5) |
| `OperationBridge.pushResult` | `operation-bridge.ts:281` | removed | consumer-less after `saolei_update` removal (D7) |
| `GameState`, `CellStatus`, `LastOp`, `createGameState` | `game-state.ts` | removed (file deleted) | stateless saolei (D7) |
| saolei validators (`validateUpdate`, `validateClick/Flag/Chord*`, connectivity helpers) | `validation.ts` | removed (file deleted) | no agent-side validation (D7) |
| `saolei_update` tool registration | `saolei-mcp.ts:479` | removed | stateless saolei (D7) |
| desktop result→chatstream mirror | `app.go:759`/`783` | removed | screenshot comes from the LLM tool result (D8 / FR-010) |
| `heldToolIds`/Confirm-on-bubble (022 §3) | `App.svelte`/`ChatView.svelte` | removed | debug Confirm re-anchored onto a session-top drawer (D11; the conversation no longer carries held state) |

---

## §9 — What does NOT change (scope boundary)

- The four saolei tool *names*, their `(x, y)` grid convention, and the proto operation Parts they dispatch (`KeyboardPressPart{F2}`, `MouseMoveAndClickPart` + `WINDOW_MESSAGE`) — unchanged (FR-020).
- `MouseClickAction`, `MouseInputMethod`, `KeyboardKey`, `ToolResultStatus` enums — unchanged.
- `OperationBridge.dispatch`/`handleResult` correlation and the 20-min `DISPATCH_TIMEOUT_MS` backstop (022 FR-014) — unchanged. `dispatch` mints its own operation id internally (D10: the `toolId` parameter is removed; the bridge always mints a UUID).
- The 022 debug control-plane contract (`SetDebugMode`, `ConfirmToolResult`, `game:debug:result-held`/`result-released`) — unchanged method/event NAMES; the `result-held` payload gains an `operation` field and the Confirm surface moves from a conversation bubble to a session-top drawer (D11, [contracts/debug-drawer-contract.md](contracts/debug-drawer-contract.md)).
- `MemorySaver` checkpoint of `BaseMessage`s — the persistence format; the refactor changes only the proto reconstruction layer, not the checkpoint.
- Session/agent/profile/skill services and their RPCs — untouched.
