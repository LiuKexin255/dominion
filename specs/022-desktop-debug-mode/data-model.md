# Data Model: Desktop Conversation Debug Mode

**Feature**: `022-desktop-debug-mode` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

This feature is **stateless by design** (spec FR-010: the hold/confirmation carries no durable representation and is never persisted). The "data model" here is therefore the **transient in-memory state** added on the desktop (Go backend + frontend) and the **event payloads** exchanged over the existing Wails channels. No new persisted entities, no schema, no storage migration.

The agent service change is a single config constant (`DISPATCH_TIMEOUT_MS`), not a data entity.

## Entities (all transient / in-memory)

### DebugMode (transient boolean)

The debug-mode flag. Source of truth is the frontend (`App.svelte` `$state(false)`); a mirrored atomic copy lives on the Go `*App` for the result-return path to consult.

| Aspect | Frontend | Go backend |
|---|---|---|
| Field | `debugMode = $state(false)` in `App.svelte` | `debugEnabled atomic.Bool` on `*App` (new) + `applog.Logger.debugEnabled atomic.Bool` (new) |
| Default | `false` | `false` |
| Lifecycle | Reset to `false` on leaving/re-entering the conversation page or session (FR-002) | Set via `SetDebugMode`; reset implicitly when the process restarts |
| Persistence | none | none |

**Invariants**: never persisted (FR-002); defaults OFF; turning OFF releases all currently-held results (see HeldToolResult lifecycle).

### HeldToolResult (transient, Go in-memory)

A tool result computed by the desktop but deliberately not yet sent to the agent while debug mode is ON. One instance per held result, keyed by `tool_id`.

```
type hold struct {
    toolID    string             // correlation key (echoed in ToolResultPart)
    confirmCh chan struct{}      // closed to release (confirm OR release-all)
    // (the result frame itself is held in the handleInboundOperation stack frame,
    //  not duplicated here; the hold struct only carries the release signal)
}
```

Registry on `*App` (new):

```
holds   map[string]*hold   // toolID -> active hold
holdsMu sync.Mutex         // guards holds
```

**Key / identity**: `tool_id` (UUID), the same key the agent uses to correlate a tool result back to its dispatch (`projects/game/agent/src/operation-bridge.ts`, `projects/game/desktop/frontend/src/api.ts:108`). Unique per dispatch.

**Validation rules**:
- A `toolID` may appear in `holds` at most once at any time (duplicate confirm is a logged no-op).
- `holds` is empty whenever debug mode is OFF (turning OFF drains it).

**State transitions** (lifecycle of one held result):

```
                ┌──────────────────────────── debug mode ON ────────────────────────────┐
                │                                                                        │
  [Computed] ──►│ [Held] ──────────────────────────────────────────────────────────► [Released] ──► [Sent to agent]
   (execute    │   ▲   │   │   │                                                       (append already   (ws.SendFrame)
    AgentOp)   │   │   │   │   │                                                        happened at        ─► chatStreams.Append
                │   │   │   │   │                                                        Held entry)
                │   │   │   │   └─ ctx.Done() (app/session shutdown)        reason="shutdown"
                │   │   │   └───── time.After(15 min) auto-continue (FR-013) reason="timeout"
                │   │   └───────── ConfirmToolResult(toolID) (FR-009)        reason="confirmed"
                │   └───────────── SetDebugMode(false) release-all           reason="debug-off"
                │
                └─ debug mode OFF: skipped entirely; [Computed] ──► [Sent] ──► [Append] (today's order)
```

- **Computed → Held**: only when debug mode is ON (FR-006). On entry: append result frame to chat stream, register hold, emit `game:debug:result-held`.
- **Held → Released**: exactly one release trigger fires (confirm / 15-min / debug-off / shutdown). On exit: emit `game:debug:result-released { toolId, reason }`, delete hold from map.
- **Released → Sent**: the held result frame is sent to the agent (`ws.SendFrame`) — identical frame content to a non-debug run (FR-007 transparency).
- **OFF path**: Held is never entered; the result is sent then appended (today's order), no events (FR-011).

### Confirm Control (transient, frontend UI)

A per-held-result UI affordance rendered by `ChatView`. Not a data entity per se, but derived state:

- **Source**: `heldToolIds: Set<string>` in `App.svelte` (reactive `$state`).
- **Render rule**: a `toolResult` Part's bubble shows a "Confirm" button iff `heldToolIds.has(part.toolResult.toolId)` (FR-008, FR-012).
- **Lifecycle**: toolID added on `game:debug:result-held`; removed on `game:debug:result-released` (button disappears — FR-009).

### Dispatch Timeout Backstop (config constant)

| Aspect | Value |
|---|---|
| Location | `projects/game/agent/src/operation-bridge.ts:35` |
| Name | `DISPATCH_TIMEOUT_MS` |
| Before | `5_000` (5 s) |
| After | `1_200_000` (20 min) |
| Scope | global (not mode-aware) |
| Rationale | safety net > desktop's 15-min auto-continue (FR-014) |

## Event payloads (Wails runtime events, Go → frontend)

New, additive event names. They do **not** alter the existing `game:log` mechanism or the SSE chat stream (FR-015).

### `game:debug:result-held`

Emitted when the desktop begins holding a computed result (debug mode ON).

```jsonc
{ "toolId": "<uuid>" }
```

### `game:debug:result-released`

Emitted when the held result is released, before it is sent to the agent.

```jsonc
{ "toolId": "<uuid>", "reason": "confirmed" | "timeout" | "debug-off" | "shutdown" }
```

`reason` is informational (used for debug logging / an optional transient notice); it is not persisted and does not affect the returned result content (FR-010).

## Relationships

```
Frontend (source of truth)              Go *App (mirrored / acting)
─────────────────────────               ───────────────────────────
debugMode $state(false)  ──SetDebugMode──►  debugEnabled (atomic)
                         │                    │
                         │             (also) ├─► applog.debugEnabled
                         │                    │
heldToolIds Set<string]  ◄──result-held────── ├─ register hold{toolID} in holds
                         │                    │  (handleInboundOperation blocks on select)
Confirm click ────────────ConfirmToolResult──►│  signal hold.confirmCh
                         ◄──result-released── ┤  delete hold; ws.SendFrame to agent
```

- The `tool_result` content itself flows the unchanged SSE chat stream (`chatStreams.Append`); only its **timing** relative to the agent send changes, plus the two out-of-band events above.
- Correlation across all paths is the existing `tool_id` UUID — no new identity is introduced.
