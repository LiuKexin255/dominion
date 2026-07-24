# Data Model: Agent Session Resync & Adapter Simplification

**Feature**: `021-agent-session-resync` | **Date**: 2026-07-24

This feature is primarily **behavioral** over the existing `AgentFrame`/`StatusSignal`/`ToolResultPart` message set (no proto change — see [research.md](research.md) D8). This document captures the in-process state and lifecycle shapes the implementation touches, and the validation/state-transition rules they enforce.

## 1. Agent working state (per session) — status derivation

The status ping-pong reports a value derived from existing per-session state. No new persisted field is introduced; the derivation rule is:

| Condition | Reported `StatusSignalStatus` |
|---|---|
| A turn is in-flight for the session (`isMutexHeld(sessionId)`) | `STATUS_SIGNAL_STATUS_ACTIVE` |
| No turn in-flight **and** an adapter is bound (`getAdapterState().isBound`) | `STATUS_SIGNAL_STATUS_IDLE` |
| No turn in-flight **and** no adapter bound | `STATUS_SIGNAL_STATUS_UNSPECIFIED` |

**Invariants.**
- The "in-flight" signal is the **shared per-session turn mutex**, not a per-stream flag, so it is correct regardless of which stream issues the probe.
- After a true reconnect, the closing stream's in-flight turn is aborted (`abortAllTurns` on stream end) and the mutex released in the turn `finally`, so the agent reports `IDLE`/`UNSPECIFIED` — which is what clears the desktop's stuck typing indicator.

## 2. OperationBridge sink lifecycle (stream-scoped)

The bridge keeps its existing single `sink: OperationSink | null` plus `pending: Map<toolId, PendingDispatch>`. The change is that registration/unregistration become **owned** by the registering stream:

```
registerSink(writeFn): SinkHandle      // records identity of the installed sink; returns a handle
unregisterSink(handle?): void          // clears this.sink ONLY if handle owns the current sink
pushResult(toolResult): void           // display-only write (see §3); no-op if no sink
```

- `SinkHandle` is an opaque identity (the sink reference or a unique token) the `Connect` handler stores alongside its `activeSessions` entry and passes to its `cleanupSinks`.
- A stale `unregisterSink(handle)` from a closed stream whose sink was already superseded is a **no-op** (compare-and-delete). This is the fix for the reconnect-dispatch bug (research.md D3).
- `dispatch(part, signal)` is unchanged: it still reads `this.sink`, stamps a `toolId`, awaits `handleResult`, and resolves `FAILED "desktop disconnected"` when no sink — but now the sink is only null when no live stream owns it.

**State transition (sink ownership):**

```
(no sink)
   │ registerSink(A)            ┌─ owns: A ─┐
   ▼                             │           │
[owns: A] ──registerSink(B)──▶ [owns: B]     │
   │                             │           │
   │ unregisterSink(A) → no-op   │ unregisterSink(B) → clears
   │ (A no longer owns)          │           │
   ▼                             ▼           │
[owns: B]                      (no sink) ◀───┘
```

## 3. Display-only tool result (forwarded for agent-internal tools)

Forwarded for tools that resolve server-side with no desktop operation (today: `saolei_update`). It is a normal `ToolResultPart` payload written via `pushResult`, **not** routed through `dispatch`/`handleResult` (no `pending` entry, no awaited result):

| Field | Value |
|---|---|
| `toolId` | a display-only id (not correlated to any dispatch) |
| `status` | `SUCCEEDED` when the tool's outcome is acceptance; `FAILED` when it is a validation rejection (research.md D5 / spec C3) |
| `message` | self-descriptive: tool name + outcome/reason (e.g. `saolei_update: state updated` or `saolei_update rejected: <reason>`) |
| `screenshot` | none |

**Envelope.** Wrapped in a content `AgentFrame` with `sender = SYSTEM`, a fresh `frameId`, and the bound `agentProfileName`, so the desktop `ChatView` renders it as a result card.

**Desktop rendering (unchanged).** The desktop `recvLoop` appends every inbound content frame to the chat stream and executes only operation parts; a `ToolResultPart` is rendered and never executed. No desktop display-path change.

## 4. SessionAgent adapter lifecycle (simplified)

```
[unbound] ──first turn / post-Refresh turn──▶ getOrCreateAdapter(name, fetcher)
                                                 │ adapter == null ⇒ serializeBind(name, fetcher)
                                                 ▼
                                             [bound: name]
                                                 │  next turn, name matches guard ⇒ return cached (no rebuild)
                                                 │  RefreshAgent ⇒ invalidateAdapter ⇒ [unbound]
                                                 ▼
                                             (serves turns until Refresh)
```

- `getOrCreateAdapter`: returns the cached adapter if one exists; otherwise builds once via `serializeBind` for the supplied profile. **Removed:** the `activeProfileName !== profileName ⇒ rebuild` branch (the auto-switch).
- `invalidateAdapter` (Refresh): unchanged — sets adapter `null`, schedules async `cleanup()`.
- `getAdapterState()`: unchanged — `{ activeProfileName, isBound }`, used by the status derivation (§1) and the profile guard (§5).

## 5. Profile-name guard (turn entry) — state machine

Inserted in the `Connect` content-frame handler, after `effectiveProfileName` resolution and **before** `acquireMutex`:

```
                      inbound user-turn content frame
                                 │
                                 ▼
                    resolve effectiveProfileName
                    (empty ⇒ bound activeProfileName when bound)
                                 │
              ┌──────────────────┴───────────────────┐
              ▼                                       ▼
   bound && activeProfileName                       not bound
   && activeProfileName != effective                    │
              │◄─────────────(match)─────────          │
              ▼                  │                     │
        REJECT:                  │ allow               │ allow
        WarnSignal(mismatch)     │                     │
        WaitSignal               │                     │
        return (no mutex)        │                     │
                                 ▼                     ▼
                          acquireMutex; run turn   acquireMutex; build adapter; run turn
```

**Validation rules (enforced):**
- A bound adapter is never asked to serve a turn whose resolved profile name differs from `activeProfileName` (FR-012/FR-012a).
- Rejection emits `WarnSignal` **and** `WaitSignal` (the latter clears the desktop's typing indicator so it returns to ready) (FR-012b).
- Rejection acquires no mutex and invokes no adapter, so it cannot panic the session agent and cannot block a later turn (FR-012a/SC-004).
- When not bound (first turn, or post-Refresh), any profile name is accepted and the adapter is built for it (FR-012c).

## 6. Entities touched (no schema additions)

| Entity | Source | Change |
|---|---|---|
| `OperationBridge` | `projects/game/agent/src/operation-bridge.ts` | stream-scoped sink handle (§2); new `pushResult` (§3) |
| `SessionAgent` | `projects/game/agent/src/session-agent.ts` | `getOrCreateAdapter` simplified (§4) |
| Connect handler (status + guard) | `projects/game/agent/src/handler.ts` | status derives ACTIVE/IDLE (§1); profile guard (§5) |
| `saolei_update` handler | `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` | forwards display `ToolResultPart` (§3) |
| Go `ConnectAgent` | `projects/game/desktop/app.go` | returns probe response status (§1) |
| Frontend connect/state | `projects/game/desktop/frontend/src/{api.ts,App.svelte}` | `connectAgent` returns status; reconcile `processing`; reset on entry (§1) |
