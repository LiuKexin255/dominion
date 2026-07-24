# Contract: Desktop Debug Control Plane (frontend ↔ Go backend)

**Feature**: `022-desktop-debug-mode` | **Date**: 2026-07-24 | **Spec**: [spec.md](../spec.md) | **Research**: [research.md](../research.md) | **Data model**: [data-model.md](../data-model.md)

This is the interface contract for the debug-mode control plane between the Svelte 5 frontend and the Wails v2 Go backend of the desktop app. It is designed before implementation per Constitution §III (Interface-First Design). The agent service is out of this contract (it only gets a constant change, see §3).

All methods are exposed via Wails v2 method binding (`projects/game/desktop/main.go:82` binds `app`; exported `*App` methods become `window.go.main.App.<method>` camelCased — see [research.md D1/D2](../research.md)). All events use the Wails v2 runtime event channel (`runtime.EventsEmit` Go→frontend, `window.runtime.EventsOn` frontend — see `projects/game/desktop/main.go:44`, `projects/game/desktop/frontend/src/main.ts:18`).

This contract is **additive**: it adds two bound methods and two event names. It does **not** modify any existing bound method, the SSE chat stream, or the `game:log` forwarding mechanism (spec FR-015).

## §1. Bound methods (frontend → Go)

Both are added to `*App` in `projects/game/desktop/app.go` and declared on the `WailsApp` interface in `projects/game/desktop/frontend/src/api.ts`, with a wrapper export each (mirroring the existing pattern, e.g., `openChatStream`).

### 1.1 `SetDebugMode`

```go
// SetDebugMode enables or disables desktop debug mode. When enabling, the Go
// backend mirrors the flag (atomic) and enables DEBUG-level logging in applog.
// When disabling, it clears the flag, disables DEBUG logging, and immediately
// releases every currently-held tool result (reason "debug-off") so no turn is
// left blocked. Idempotent.
func (a *App) SetDebugMode(enabled bool) error
```

- **Frontend TS**: `SetDebugMode(enabled: boolean): Promise<void>` on `WailsApp`; wrapper `export async function setDebugMode(enabled: boolean): Promise<void>`.
- **Call site**: `App.svelte` on every Debug-switch toggle.
- **Semantics**:
  - `enabled=true`: set `debugEnabled` atomic; `a.logger.SetDebug(true)`; emit DEBUG log "debug mode enabled".
  - `enabled=false`: set `debugEnabled=false`; `a.logger.SetDebug(false)`; release all holds (close each `confirmCh`, emit `game:debug:result-released { reason: "debug-off" }` for each); emit DEBUG log "debug mode disabled".
- **Errors**: returns `nil` in normal operation (no failure mode for a boolean flag); reserved for future validation.
- **Side effects**: the released holds then proceed to `ws.SendFrame` in their blocked `handleInboundOperation` callers.

### 1.2 `ConfirmToolResult`

```go
// ConfirmToolResult releases the held tool result identified by toolID,
// causing handleInboundOperation to send it to the agent. It is a logged
// no-op (returns nil) if toolID is not currently held — e.g., the 15-minute
// auto-continue already released it, or debug mode was turned off.
func (a *App) ConfirmToolResult(toolID string) error
```

- **Frontend TS**: `ConfirmToolResult(toolID: string): Promise<void>` on `WailsApp`; wrapper `export async function confirmToolResult(toolID: string): Promise<void>`.
- **Call site**: `ChatView` "Confirm" button `onclick` → `onConfirm(toolId)` prop → `App.svelte` → `confirmToolResult(toolId)`.
- **Semantics**: under `holdsMu`, look up `holds[toolID]`; if present, close its `confirmCh` (the blocked `handleInboundOperation` selects on it) and delete the entry. The blocked caller then emits `game:debug:result-released { reason: "confirmed" }` and sends the result to the agent.
- **Errors**: returns `nil` whether or not the toolID was held (no-op is not an error — the result may already have been released).

### 1.3 Argument escaping

Wails v2 binds Go methods with camelCased names and JSON-marshals struct args. `SetDebugMode(bool)` and `ConfirmToolResult(string)` take primitives only — no struct args, no escaping concerns.

## §2. Runtime events (Go → frontend)

Two new event names on the existing Wails runtime event channel. Payload is a single JSON object passed as the event argument (consumed via `window.runtime.EventsOn`).

### 2.1 `game:debug:result-held`

Emitted by `handleInboundOperation` when, in debug mode, a computed result begins to be held (after appending the result frame to the chat stream, before blocking).

```jsonc
{ "toolId": "<uuid, matches ToolResultPart.toolId>" }
```

### 2.2 `game:debug:result-released`

Emitted by `handleInboundOperation` when the held result is released (immediately before sending it to the agent).

```jsonc
{
  "toolId": "<uuid>",
  "reason": "confirmed" | "timeout" | "debug-off" | "shutdown"
}
```

- `confirmed` — user clicked Confirm (`ConfirmToolResult`).
- `timeout` — 15-minute auto-continue fired (FR-013).
- `debug-off` — `SetDebugMode(false)` released the hold.
- `shutdown` — app/session context done.

`reason` is informational only (debug log + optional transient UI notice); it is not persisted and does not alter the returned result content (spec FR-010).

### 2.3 Frontend subscription

Registered once in `App.svelte` `onMount` (same pattern as the existing `game:log` listener in `projects/game/desktop/frontend/src/main.ts:18`):

- on `result-held`: `heldToolIds = new Set(heldToolIds).add(payload.toolId)`.
- on `result-released`: `const next = new Set(heldToolIds); next.delete(payload.toolId); heldToolIds = next`.

(Reassigning a new `Set` triggers Svelte 5 `$state` reactivity.)

## §3. Rendering contract (ChatView ↔ held state)

`App.svelte` passes to `ChatView` (`projects/game/desktop/frontend/src/components/ChatView.svelte`):

- `heldToolIds: Set<string>` — the currently-held toolIDs.
- `onConfirm: (toolID: string) => void` — invokes `confirmToolResult(toolID)`.

`ChatView` rendering rule (added in the existing `toolResult` branch, lines 197–221):

- Render the existing result card (unchanged).
- **Additionally**, iff `part.toolResult?.toolId != null && heldToolIds.has(part.toolResult.toolId)`: render a "Confirm" control whose `onclick` calls `onConfirm(part.toolResult.toolId)`.
- When the toolID leaves `heldToolIds` (reactive), the control disappears — no other state.

This naturally satisfies spec FR-012: display-only results (agent `pushResult`) and history-replayed results never receive a `result-held` event, so they are never in `heldToolIds`, so they never render the control.

## §4. Agent-side change (out of this control plane)

Not part of the frontend↔backend contract, recorded here for completeness (Constitution §I):

- `projects/game/agent/src/operation-bridge.ts:35` — `DISPATCH_TIMEOUT_MS`: `5_000` → `1_200_000` (20 min). Global constant; no API/contract change; validated by existing agent large tests at `projects/game/testplan/`.

## §5. What does NOT change (scope boundary, spec FR-015)

- The SSE chat stream (`chatStreams.Append`, `projects/game/desktop/internal/chatstream/stream.go`) — remains append-only; the result frame travels it unchanged, only its send timing changes.
- The `game:log` forwarding mechanism — unchanged; DEBUG entries (when enabled) flow through the existing `Entry.Level` field and event sink.
- The `ToolResultPart` on-wire shape — unchanged.
- The agent↔desktop tool correlation (`tool_id`) — unchanged; reused for the confirm signal.
- All other pages (sessions, profiles) and the agent execution logic.

## §6. Versioning & compatibility

- New methods/events are purely additive; an older frontend against the new backend (or vice versa) degrades gracefully: if the frontend never calls `SetDebugMode`, debug stays OFF and behavior is identical to today (FR-011); unhandled `game:debug:*` events are simply ignored by an older frontend.
- No protocol version bump is required.
