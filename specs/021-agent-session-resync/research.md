# Research: Agent Session Resync & Adapter Simplification

**Feature**: `021-agent-session-resync` | **Date**: 2026-07-24 | **Spec**: [spec.md](spec.md)

This document records the root-cause analysis and design decisions (D1–D8) that resolve every spec unknown. Findings are grounded in the current `018-saolei-mcp` implementation: agent TypeScript (`projects/game/agent/src/`), desktop Go + Svelte (`projects/game/desktop/`), and `projects/game/game.proto`.

## Context recap (the four areas)

| # | Symptom | Area |
|---|---|---|
| 1 | `saolei_update` (agent-internal tool) is not visible on the desktop | display |
| 2 | After session exit→re-enter, all saolei MCP tools report FAILED | reconnect bug |
| 3 | After session exit→re-enter, "Agent is typing…" stays forever | reconnect bug |
| 4 | Remove the implicit per-turn profile-switch adapter rebuild; add a profile-name guard | simplification + guard |

---

## D1 — Root cause of "typing stuck" (#3) and the fix surface

**Finding.** The desktop's `processing` flag (the typing-indicator state) is set `true` when the operator sends a turn (`App.svelte` `handleSendChatText`) and cleared `false` only when a `WaitSignal` frame arrives (`handleAgentFrame`). On session re-entry, `handleSelectSession` → `resetPlayPageState` resets screenshots/windows but **does NOT reset `processing`** (confirmed: `resetPlayPageState` omits `processing`; `processing` is a `$state(false)` retained across in-component navigation). So a turn left in-flight when the operator backed out leaves `processing = true` permanently after re-entry, because the in-flight turn's `WaitSignal` was sent on the now-closed stream and is never seen.

A status probe already exists: the Go desktop `ConnectAgent` sends a `StatusSignal` and reads one response frame (`app.go` `ConnectAgent`), and the agent already responds to inbound `status` frames (`handler.ts` Connect `stream.on("data")` `payload === "status"` branch). But (a) the agent's response only reports `isBound` → `IDLE`/`UNSPECIFIED`, never `ACTIVE`; and (b) the Go side discards the response ("Accept any response — the round-trip itself proves the path is alive"), so the frontend never reconciles `processing`.

**Decision.** Reuse the existing connect-time status probe as the ping-pong vehicle (no new channel):

1. The agent reports its **actual** working state: `ACTIVE` when a turn is in-flight for the session, else `IDLE` when an adapter is bound, else `UNSPECIFIED`.
2. The Go `ConnectAgent` captures and returns the probe response's `StatusSignalStatus` to the frontend (instead of discarding it).
3. The frontend reconciles `processing`/`playState` against the returned status on session entry, and `resetPlayPageState` defensively resets `processing = false`.

**Rationale.** The probe already traverses the full desktop→gateway→agent path at exactly the re-entry moment; reusing it avoids any new message type or extra round trip. Resetting `processing` defensively plus refining it from the probe covers both true reconnects (turn aborted → agent `IDLE` → cleared) and in-place re-entry where a turn is genuinely still running (`ACTIVE` → kept).

**Alternatives considered.**
- *Periodic agent heartbeat (`ACTIVE` every 5s, 10s liveness timeout):* explicitly **out of scope** — the user's mechanism is client-initiated request-response used on re-entry, not a heartbeat timer. The proto's `ACTIVE` comment is preserved for future use but not built here.
- *Frontend-only reset of `processing`:* insufficient — it cannot distinguish "agent actually idle" from "agent still working" on re-entry, so it would wrongly clear the indicator when a turn is genuinely in flight.

---

## D2 — How the agent determines ACTIVE vs IDLE

**Finding.** The handler serializes turns per session with a shared mutex (`acquireMutex`/`releaseMutex`/`isMutexHeld`). A turn is in-flight exactly while that mutex is held (acquired before adapter invocation, released in the `finally`). `isMutexHeld(sessionId)` is already used by `RefreshAgent` to reject refresh during an in-flight turn.

**Decision.** The status response derives its value from the per-session turn state:
- `isMutexHeld(sessionId)` → `STATUS_SIGNAL_STATUS_ACTIVE`
- else, adapter bound (`getAdapterState().isBound`) → `STATUS_SIGNAL_STATUS_IDLE`
- else → `STATUS_SIGNAL_STATUS_UNSPECIFIED`

**Rationale.** The mutex is the single source of truth for "a turn is running on this session"; it is shared across streams (not per-Connect-handler), so it correctly reflects in-flight work regardless of which stream probes.

---

## D3 — Root cause of "all MCP tools fail after reconnect" (#2) and the fix

**Finding.** `OperationBridge` is owned by `SessionAgent` and **survives stream reconnects** (by design — only a write callback is held, not a stream reference). `registerSink(writeFn)` unconditionally replaces `this.sink`; `unregisterSink()` **unconditionally** sets `this.sink = null`. Each `Connect` handler tracks the sessions whose sink it registered in a per-stream `activeSessions` set, and its `cleanupSinks()` iterates that set calling `bridge.unregisterSink()` on the **shared** bridge.

The race: stream-A closes when the operator leaves the session; its `"end"`/`"error"` event is asynchronous (network teardown). If the operator re-enters quickly, stream-B opens, a new turn registers its sink on the same bridge (`this.sink = sink-B`), and **then** stream-A's late `cleanupSinks()` runs `unregisterSink()` → `this.sink = null`, clobbering sink-B. Every subsequent `bridge.dispatch()` then resolves `FAILED "desktop disconnected"` (because `this.sink` is null), so all saolei operation tools (`init`/`click`/`flag`/`chord_click`) report FAILED. (`saolei_update` does not dispatch, so it is unaffected — consistent with bug #1 being separate.)

**Decision.** Make the sink lifecycle **stream-scoped** via compare-and-delete:
- `registerSink(writeFn)` keeps storing `this.sink = writeFn`, but returns/records the sink identity it installed.
- `unregisterSink(handle)` clears `this.sink` **only if** the currently-registered sink is the one this stream installed (reference equality, or an opaque token returned by `registerSink`). A stale close from a different stream becomes a no-op.
- The `Connect` handler records the sink it registered per session (alongside `activeSessions`) and passes that identity to cleanup.

**Rationale.** This is the minimal change that eliminates the "stale cleanup invalidates a fresh registration" category of bug without introducing per-stream sink maps or fan-out. In-flight dispatches on the closing stream are still resolved `FAILED "aborted"` by the existing per-turn `AbortController` (`abortAllTurns` on stream end), so no separate cleanup of `pending` is needed.

**Verification guidance.** Per `AGENTS.md`, confirm the reproduction and the fix via tracing/logs (signoz): look for a `dispatch` resolving `FAILED "desktop disconnected"` on the post-reconnect stream while the desktop WS is in fact connected. After the fix, no such spurious FAILED should appear across an exit→re-enter cycle.

**Alternatives considered.**
- *Per-stream sink `Map<streamId, sink>` with fan-out on dispatch:* heavier; the bridge would need to choose/replicate writes. Rejected — only one live stream per session is expected at a time; compare-and-delete is simpler and sufficient.
- *Keep unconditional clear but never unregister on close:* wrong — a genuinely disconnected desktop must stop dispatching to a dead sink.

---

## D4 — Display of agent-internal tool results (#1)

**Finding.** `saolei_update` resolves entirely server-side (validates + mutates `GameState`) and returns a text result to the model; it produces **no** operation `Part`, so nothing reaches the desktop and the operator cannot observe it. By contrast, the saolei *operation* tools dispatch a `MouseMoveAndClickPart`/`KeyboardPressPart` via `bridge.dispatch`; the desktop `recvLoop` executes those and mirrors the resulting `ToolResultPart` into the local chat stream, where `ChatView` renders it as a result card.

Critically, the desktop `recvLoop` **already appends every inbound content frame to the chat stream** (`app.go` `recvLoop`) and only *executes* operation parts (`MouseMovePart`/`MouseClickPart`/`KeyboardPressPart`/`MouseMoveAndClickPart`); a `ToolResultPart` is appended (rendered) and **not** executed. `recvLoop` runs for the duration of a turn — and `saolei_update` is called mid-turn, so a frame forwarded then **will** be read and rendered with zero desktop-side change.

**Decision.** Add a **display-only, non-correlating** write path on `OperationBridge` (e.g. `pushResult(toolResult)`) that wraps a `ToolResultPart` in a content frame and writes it to the current sink (no-op if no sink; no `pending` entry, no awaited result). The `saolei_update` handler calls it after resolving, with:
- `status` = `SUCCEEDED` when the update is accepted, `FAILED` when validation rejects it (decision D5 / spec C3),
- `message` = a self-descriptive string (tool name + outcome/reason),
- `toolId` = a display-only id (not correlated to any dispatch).

The agent sets the frame envelope fields (`sender`, `frameId`, `agentProfileName`) so the card renders correctly.

**Rationale.** The desktop display path is reused unchanged (the `toolResult` kind is already rendered); the only new behavior is the agent emitting a display-only result. Scoping this to server-side-only tools (today `saolei_update`) keeps it minimal and avoids duplicating the operation tools' existing mirrored results.

**Note on status semantics.** The saolei tools never return an MCP-level error (018 decision D8). The `FAILED` status here is a **display-only** affordance the agent applies when forwarding a logically-rejected update; it is not an MCP error and does not change what the model receives.

---

## D5 — Forwarded tool-result status semantics (spec C3, recorded)

**Decision.** Reflect the logical outcome: `SUCCEEDED` on acceptance, `FAILED` on validation rejection (reason in the message). Confirmed in spec C3; no further analysis needed.

---

## D6 — Adapter lifecycle simplification (remove auto-switch)

**Finding.** `SessionAgent.getOrCreateAdapter(profileName, profileFetcher)` today rebuilds the adapter whenever the incoming profile name differs from `activeProfileName` (the implicit "auto-switch"). Combined with the new profile guard (D7), the only situations `getOrCreateAdapter` is reached in are:
- adapter is `null` (first turn, or after `RefreshAgent` invalidated it) → build it for the supplied profile, **or**
- adapter exists and the profile name matches the guard's check → return the cached adapter.

So the profile-name-mismatch rebuild branch becomes dead code.

**Decision.** Simplify `getOrCreateAdapter` to: return the cached adapter if one exists; otherwise build once via `serializeBind` using the supplied profile (the initial/rebuild build). Remove the `activeProfileName !== profileName` rebuild path. The profile name is used **only** for the initial/rebuild build; `invalidateAdapter` (Refresh) and `getAdapterState` are unchanged in behavior.

**Rationale.** With the guard (D7) ensuring a bound adapter is never asked to serve a mismatched profile, `getOrCreateAdapter` no longer needs to detect or react to profile changes — Refresh is the single, explicit rebuild path. This removes an implicit, hard-to-reason-about switching path (Constitution §II — simplify when over-designed for the new need).

**Alternatives considered.**
- *Drop the `profileName`/`profileFetcher` parameters entirely:* tempting, but the initial/rebuild build still needs them, and the existing `RefreshAgent`/handler call sites pass them. Keeping the signature minimizes churn; the simplification is removing the rebuild-on-mismatch branch.

---

## D7 — Profile-name guard on turn entry (spec C4)

**Finding.** The handler resolves `effectiveProfileName` (falling back to the bound profile when the frame omits it), then `acquireMutex`, then `getOrCreateAdapter`. A guard must reject a mismatched profile **before** the turn runs, and must be non-fatal (no panic, no mutex leak, no blocking of later turns).

**Decision.** Insert the guard after `effectiveProfileName` resolution and **before** `acquireMutex`:

```
state = sessionAgent.getAdapterState()
if state.isBound && state.activeProfileName && state.activeProfileName !== effectiveProfileName:
    send WarnSignal (message: profile mismatch: bound=<X> received=<Y>; call Refresh to switch)
    send WaitSignal   // return the desktop to ready (clears its typing indicator)
    return            // no mutex acquired, no adapter invocation
```

Because the guard runs before `acquireMutex`, it consumes no turn mutex; because it only reads state and writes frames, it cannot panic the session agent; because it emits the `WaitSignal`, the desktop's `processing` clears and the operator can immediately send the next message.

**Guard does not block initial/rebuild.** When `state.isBound` is false (first turn, or post-Refresh), the condition is false and the turn proceeds to build the adapter for the supplied profile (FR-012c).

**Empty profile name.** The existing fallback resolves an empty `effectiveProfileName` to the bound `activeProfileName` when bound, so an empty name never mismatches a bound adapter. Post-Refresh (bound=false, name empty) still hits the existing "agent_profile_name required" warn path — unchanged.

**Why reject rather than silently run against the current adapter.** Silently running a turn meant for profile Y against profile X's adapter would use the wrong model/tools/MCPs/skills — confusing and risky. A clear rejection tells the operator to Refresh.

---

## D8 — Proto impact: none

**Decision.** All four areas reuse existing proto messages with no schema change:
- Status ping-pong: `StatusSignal` / `StatusSignalStatus` (`UNSPECIFIED`/`ACTIVE`/`IDLE`) — already defined.
- Tool-result forwarding: `ToolResultPart` (already a `Part.kind` variant; already rendered by `ChatView`).
- Sink lifecycle + adapter simplification + profile guard: pure agent-side code; reuse `WarnSignal`/`WaitSignal`.

No new `Part` kinds, no new signals, no enum additions. The change is behavioral over the existing message set.

---

## Summary of decisions

| ID | Topic | Decision |
|----|-------|----------|
| D1 | Typing-stuck root cause + surface | Reuse connect-time status probe; reset `processing` defensively; reconcile from probe response |
| D2 | ACTIVE vs IDLE source | Derive from `isMutexHeld` (ACTIVE) / adapter-bound (IDLE) / else UNSPECIFIED |
| D3 | MCP-fail root cause + fix | Stream-scoped sink lifecycle via compare-and-delete (`unregisterSink` only clears if it owns the current sink) |
| D4 | Tool-result display | New display-only `OperationBridge.pushResult`; `saolei_update` forwards a `ToolResultPart`; desktop renders unchanged |
| D5 | Forwarded result status | SUCCEEDED on accept, FAILED on reject (C3) |
| D6 | Adapter simplification | `getOrCreateAdapter` returns cached or builds once; remove rebuild-on-mismatch |
| D7 | Profile guard | Reject mismatched turn (warn+wait) before mutex; non-fatal; skip when unbound |
| D8 | Proto impact | None — all reuse existing messages |

All spec unknowns resolved; no NEEDS CLARIFICATION remains.
