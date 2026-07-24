# Contract: Agent Session Lifecycle (Sink, Adapter, Profile Guard)

**Feature**: `021-agent-session-resync` | **Date**: 2026-07-24

This contract specifies the agent-internal behaviors that fix reconnect dispatch reliability and simplify the adapter lifecycle. All are pure agent TypeScript changes in `projects/game/agent/src/`; no proto change (research.md D8).

---

## 1. OperationBridge sink ownership (stream-scoped)

**Source:** `projects/game/agent/src/operation-bridge.ts`.

### Interface (changed/added)

```
type OperationSink = (frame: AgentFrame) => void;

class OperationBridge {
  registerSink(writeFn: OperationSink): SinkHandle;   // records ownership; returns handle
  unregisterSink(handle?: SinkHandle): void;          // clears sink ONLY if handle owns current
  pushResult(toolResult: ToolResultPart): void;       // display-only write (channel contract §2)
  dispatch(part, signal?): Promise<OperationResult>;  // unchanged
  handleResult(result): void;                         // unchanged
}
```

`SinkHandle` is an opaque identity (the sink reference or a unique token) that the `Connect` handler stores per session alongside `activeSessions`.

### Guarantees

- **Compare-and-delete:** `unregisterSink(handle)` clears `this.sink` only when `handle` identifies the currently-registered sink. A stale close from a superseded stream is a **no-op**. This is the fix for the reconnect-dispatch bug (research.md D3).
- **Live-stream dispatch:** `dispatch` resolves `FAILED "desktop disconnected"` only when no live stream owns the sink — never because a closed stream's cleanup clobbered a fresh registration.
- **In-flight on close:** a closing stream's in-flight dispatches are resolved `FAILED "aborted"` by the existing per-turn `AbortController`; no `pending` cleanup change is required.
- **pushResult** writes a content frame to the current sink without creating a `pending` entry or awaiting a result; no-op when no sink.

### Connect handler integration (`projects/game/agent/src/handler.ts`)

- On turn start, record the sink installed: `const handle = sa.getBridge().registerSink(sinkFn); mySinks.set(sessionId, handle);` (replacing the current `registerSink` + `activeSessions.add`).
- `cleanupSinks` iterates `mySinks` and calls `sa.getBridge().unregisterSink(handle)` per session.

---

## 2. SessionAgent adapter lifecycle (simplified)

**Source:** `projects/game/agent/src/session-agent.ts`.

### Behavior (changed)

```
getOrCreateAdapter(profileName, profileFetcher):
  if this.adapter: return this.adapter          // cached — serve (profile match ensured by the guard §3)
  return serializeBind(profileName, profileFetcher)   // build once (first turn / post-Refresh)
```

### Guarantees

- **No implicit switch:** a differing `profileName` NEVER rebuilds an existing adapter (FR-012). The `activeProfileName !== profileName ⇒ rebuild` branch is removed.
- **Build-on-null only:** the adapter is built lazily when `null` (first turn, or after `invalidateAdapter` from Refresh). `profileName`/`profileFetcher` are used only for that build.
- **Refresh is the sole rebuild path:** `invalidateAdapter` (Refresh) sets adapter `null` and schedules async `cleanup()` — unchanged.
- `getAdapterState()` returns `{ activeProfileName, isBound }` — unchanged; consumed by the status derivation and the profile guard.

---

## 3. Profile-name guard (turn entry)

**Source:** `projects/game/agent/src/handler.ts` `Connect` content-frame handler.

### Location

After `effectiveProfileName` resolution (including the empty⇒bound fallback) and **before** `acquireMutex`.

### Behavior

```
state = sessionAgent.getAdapterState()
if state.isBound && state.activeProfileName && state.activeProfileName !== effectiveProfileName:
    stream.write(WarnSignal { message:
        "profile mismatch: session bound to '<activeProfileName>' but turn targets '<effectiveProfileName>'; call Refresh to switch profiles" })
    stream.write(WaitSignal { })      // return desktop to ready (clears typing indicator)
    return                            // no mutex acquired, no adapter invoked
// else: proceed to acquireMutex + getOrCreateAdapter + generateTurn
```

### Guarantees

- **Reject, don't rebuild:** a mismatched turn is blocked from entering the agent; the adapter is NOT rebuilt (FR-012a).
- **Non-fatal:** the guard only reads state and writes frames — it cannot throw/panic the session agent; it acquires no mutex, so it cannot block a later turn (FR-012a/SC-004).
- **Returns desktop to ready:** the `WaitSignal` after the `WarnSignal` clears the desktop's `processing` flag so the operator can immediately send the next message (FR-012b). *(This also fixes the latent gap where the existing "agent_profile_name required" warn path omits the `WaitSignal`.)*
- **Skips when unbound:** when `!state.isBound` (first turn / post-Refresh), the guard does not block; the turn proceeds to build the adapter for the supplied profile (FR-012c).
- **Empty profile name:** the existing fallback resolves empty⇒bound `activeProfileName` when bound, so an empty name never mismatches a bound adapter. Post-Refresh with an empty name still hits the existing "agent_profile_name required" warn (unchanged).

---

## 4. Status derivation (agent side)

**Source:** `projects/game/agent/src/handler.ts` inbound `status` branch.

The response value is derived per [data-model.md §1](../data-model.md):

```
status = isMutexHeld(sessionId)             ? ACTIVE
       : sessionAgent.getAdapterState().isBound ? IDLE
       :                                         UNSPECIFIED
```

`isMutexHeld` is the shared per-session turn mutex (already used by `RefreshAgent`).
