# Contract: Debug Operation-Confirm Drawer (session-top, operation channel)

**Feature**: 023-saolei-mcp-refine
**Date**: 2026-07-25
**Status**: Phase 1 contract — MUST be satisfied by implementation. Settled BEFORE any frontend/backend change (Constitution Principle III).

This contract pins the **debug-mode operation-confirm drawer**: a session-top UI surface that asks the user to approve a held operation result before the desktop returns it to the agent. It **replaces** the 022 Confirm-on-conversation-bubble design (022 `contracts/debug-control-plane.md` §3), which was coupled to the now-decoupled `tool_id` (research.md D10/D11). The drawer is driven entirely by the **operation channel** (the FlowPart operation the desktop executed + its bridge-minted `tool_id`); it is independent of the conversation renderer.

## Authority

- Spec: `spec.md` FR-023..FR-027 (as revised by Clarifications C13/C14 — Confirm moves to a session-top drawer; control path no longer associates with the conversation bubble).
- Research: `research.md` D10 (decouple channels), D11 (drawer), D8 (hold point retained).
- Prior contract: `specs/022-desktop-debug-mode/contracts/debug-control-plane.md` — the `SetDebugMode` / `ConfirmToolResult` methods and `game:debug:result-held` / `result-released` event NAMES are REUSED (this contract extends the `result-held` payload and replaces the §3 rendering rule).
- Current code: `projects/game/desktop/app.go` `handleInboundOperation` (hold point + `holdAndRelease`), `projects/game/desktop/frontend/src/App.svelte` (`heldToolIds`/`onConfirm`/event listeners — to be replaced), `projects/game/desktop/frontend/src/components/ChatView.svelte` (Confirm-on-bubble — to be removed).

## §1. What is reused from 022 (unchanged)

- **`SetDebugMode(enabled: bool)`** (`app.go`, `api.ts`) — toggles debug mode + DEBUG logging + releases all holds on disable. Unchanged.
- **`ConfirmToolResult(toolID: string)`** (`app.go`, `api.ts`) — releases the held result identified by `toolID` (the operation-channel bridge-minted id). Unchanged signature/semantics.
- **Event names**: `game:debug:result-held` / `game:debug:result-released` (Wails runtime event channel). Unchanged names.
- **`result-released` payload**: `{ toolId, reason: "confirmed"|"timeout"|"debug-off"|"shutdown" }`. Unchanged.

## §2. `game:debug:result-held` payload (EXTENDED)

The 022 payload `{ toolId }` is extended with the **operation request content** so the drawer can render a human-readable description without proto knowledge. The desktop Go backend builds it from the `FlowPart` it received and executed.

```jsonc
{
  "toolId": "<string, the bridge-minted operation id on the FlowPart / ToolResultPart>",
  "operation": {
    "kind": "mouse_move" | "mouse_click" | "keyboard_press" | "mouse_move_and_click",
    "summary": "<string, human-readable, e.g. \"移动并点击 (136, 344) · 左键 · 窗口消息\" / \"按键 F2\">",
    "details": {
      // raw FlowPart operation fields, present as applicable:
      "xPx": 136, "yPx": 344,         // mouse_move / mouse_move_and_click
      "click": "MOUSE_CLICK_ACTION_LEFT_CLICK",   // mouse_click / mouse_move_and_click
      "method": "MOUSE_INPUT_METHOD_WINDOW_MESSAGE", // mouse_* (desktop execution method)
      "key": "KEYBOARD_KEY_F2"         // keyboard_press
    }
  }
}
```

- `toolId` is the **operation-channel** id (the bridge-minted UUID on the `FlowPart.tool_id`), NOT the conversation `tool_call.id` (D10: the two are independent). `ConfirmToolResult(toolId)` and `result-released.toolId` use this same operation-channel id.
- `operation.kind` is the FlowPart oneof variant name (snake_case, matching proto field names).
- `operation.summary` is a localized, single-line description built by the Go backend from `details`. The frontend renders it verbatim (no proto decoding in the frontend).
- `operation.details` carries the raw operation fields for optional richer rendering/debugging.
- The event is emitted by `handleInboundOperation` in the debug branch, **after** `executeAgentOperation` and **before** blocking on the hold (same point 022 emitted `{ toolId }`).

## §3. Frontend rendering — session-top drawer (REPLACES 022 §3)

The Confirm control is NO LONGER on a conversation bubble. `ChatView` does NOT receive `heldToolIds` / `onConfirm`.

### 3.1 State (`App.svelte`)

Replace `heldToolIds: Set<string>` with an ordered list of held operations:

```ts
// Each entry = one held operation awaiting confirmation.
interface HeldOperation {
  toolId: string;            // operation-channel id (ConfirmToolResult key)
  kind: string;              // FlowPart variant
  summary: string;           // human-readable operation description
  details: Record<string, unknown>;
}
let heldOperations = $state<HeldOperation[]>([]);
```

Event listeners (registered once in `onMount`, same pattern as 022):
- on `game:debug:result-held` (payload per §2): append `{ toolId, kind, summary, details }` to `heldOperations` (preserve arrival order).
- on `game:debug:result-released` (`{ toolId, reason }`): remove the entry whose `toolId` matches.

### 3.2 Drawer component

A new component (e.g. `projects/game/desktop/frontend/src/components/OperationConfirmDrawer.svelte`) rendered at the **top of the chat view** (in `App.svelte`, between `.chat-top-bar` and `<ChatView>`, inside `.chat-main`), shown iff `heldOperations.length > 0`:

- Renders one row per held operation: `summary` text + a **Confirm** button (`onclick` → `confirmToolResult(entry.toolId)`).
- Drawer style: a distinct, drawer-like panel pinned to the top of the session chat area (visually separable from the conversation transcript below). Multiple simultaneous holds stack vertically in arrival order.
- The drawer is **purely operation-channel**: it does not read or reference any conversation `tool_call.id` or bubble. It is independent of `ChatView`.

### 3.3 `ChatView` changes (decoupling)

`ChatView.svelte`:
- REMOVE the `heldToolIds` / `onConfirm` props and the Confirm-on-bubble rendering (022 §3 rule).
- The conversation transcript renders ONLY `MessagePart`s (tool_call/tool_result bubbles grouped by conversation-channel `tool_call.id`) — no held-state branching.

## §4. `handleInboundOperation` (Go backend) changes

- The debug branch retains: `executeAgentOperation` → `holdAndRelease(toolID)` (blocks) → on release, `ws.SendFrame` (return result to agent). The chat-stream result mirror is already removed (US1 T013).
- NEW: before blocking, emit `game:debug:result-held` with the EXTENDED payload (§2) — build `operation.summary`/`details` from the inbound `FlowPart` operation decoded in `recvLoop`.
- `ConfirmToolResult(toolID)` / `SetDebugMode(false)` / timeout / shutdown release paths are unchanged (they close the hold's `confirmCh` and emit `game:debug:result-released`).
- The operation content (for the payload) is available because `recvLoop` already decoded the `FlowPart` to route it to `handleInboundOperation`; pass the decoded operation descriptor through.

## §5. Multiple holds & ordering

- Multiple operations may be held simultaneously (e.g. the agent dispatches a second operation while the first is still held — possible because the agent turn loop can pipeline). Each hold emits its own `result-held` event; the drawer stacks one row per held `toolId`.
- Confirm releases only the matching `toolId` (its row disappears on `result-released`).
- Arrival order in the drawer follows `result-held` emit order (FIFO).

## §6. What does NOT change (scope boundary)

- The hold POINT (after execute, before return to agent) — D8/022.
- `SetDebugMode` / `ConfirmToolResult` method signatures and the `result-released` event payload — 022.
- The 15-min auto-continue (`debugHoldTimeout`) and the agent 20-min `DISPATCH_TIMEOUT_MS` backstop — 022/023.
- The conversation renderer (`ChatView`) bubble logic — it simply loses the held-state props; its `MessagePart` rendering is unchanged.
- The operation channel (`OperationBridge.dispatch`↔`handleResult` via bridge-minted id) — D10.

## §7. Compatibility

- The `result-held` payload extension is **additive** (`operation` field added; `toolId` retained). An older frontend that ignores `operation` still functions at the 022 level (though 023's frontend is updated to use it). The 022 `heldToolIds`-on-bubble rendering is removed by this feature's frontend change.
- No protocol/proto version bump: the drawer rides on the existing Wails event channel + the unchanged proto `FlowPart`/`ToolResultPart` (the `operation` descriptor is built Go-side from the decoded FlowPart, not a new proto field).

## Out of scope for this contract

- The proto content model → [content-model-contract.md](content-model-contract.md).
- The tool↔bridge dispatch (operation-channel correlation) → [tool-dispatch-contract.md](tool-dispatch-contract.md) §1.
- The evolving-bubble conversation view model → `data-model.md` §5.
