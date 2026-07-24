# Research: Desktop Conversation Debug Mode

**Feature**: `022-desktop-debug-mode` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

This document resolves the technical unknowns left open by the spec (the "plan-phase implementation concerns" noted in its Assumptions) and records the chosen approach for each, with rationale and alternatives. All citations follow Constitution §I (repository-relative paths or full URLs).

## Context recap

The feature has two halves, both scoped to the desktop app plus one agent-side constant (per spec FR-015):

1. **Debug logging** — a gated DEBUG level emitted from both the frontend logger (`projects/game/desktop/frontend/src/logger.ts`) and the desktop Go backend logger (`projects/game/desktop/internal/applog/logger.go`), surfaced in the existing log panel.
2. **Tool-result hold-for-confirmation** — at the desktop result-return boundary `projects/game/desktop/app.go` `handleInboundOperation` (between result computation at line 612 and the agent-bound `ws.SendFrame` at line 638), the desktop, when debug mode is ON, holds the computed result until the user confirms (or the 15-minute auto-continue fires), then returns it.

The agent service is touched only by raising `DISPATCH_TIMEOUT_MS` (`projects/game/agent/src/operation-bridge.ts:35`) from 5 s to 20 min (FR-014).

## Provenance: how the existing desktop app wires Go ↔ frontend

The design reuses three already-established mechanisms verbatim (no new transport introduced):

- **Wails v2 method binding** — `projects/game/desktop/main.go:82` binds `app` via `Bind: []interface{}{app}`. Every exported method on `*App` is auto-exposed to the frontend as `window.go.main.App.<Method>` (camelCased). The frontend wrappers live in `projects/game/desktop/frontend/src/api.ts` (the `WailsApp` interface, lines 281–312). Authority: [Wails v2 — How does it work](https://wails.io/docs/howdoesitwork), [Wails v2 — Application Development](https://wails.io/docs/guides/application-development).
- **Wails v2 runtime events (Go → frontend)** — `projects/game/desktop/main.go:44` emits `runtime.EventsEmit(ctx, "game:log", entry)`; `projects/game/desktop/frontend/src/main.ts:18` receives it via `window.runtime.EventsOn('game:log', ...)`. Authority: [Wails v2 — Events](https://wails.io/docs/guides/application-development) (`window.runtime.EventsEmit` / `EventsOn`).
- **Append-only SSE chat stream** — `projects/game/desktop/internal/chatstream/stream.go` `Registry.Append(sessionID, frame)` (line 344) fans a frame out to every SSE subscriber. The stream is strictly append-only: there is no per-event mutation or deletion API. The frontend consumes it via `projects/game/desktop/frontend/src/chat-stream.ts`.

## Decisions

### D1 — Debug-mode state lives in the frontend (source of truth), propagated to Go via a bound method

**Decision**: The Debug switch is a Svelte 5 `$state(false)` boolean owned by `App.svelte` (conversation page). On every toggle it calls a new Wails-bound method `SetDebugMode(enabled bool)` on `*App`, which stores the flag on the Go side (atomically) and, when turning OFF, releases every currently-held result (see D4). The frontend never needs a Go→frontend "mode changed" event because it is the source of truth.

**Rationale**: The toggle is a UI affordance; the frontend already owns all conversation-page state (`projects/game/desktop/frontend/src/App.svelte` lines 36–111). Making the frontend the source of truth avoids a round-trip and a redundant event. Go only needs the flag to decide whether `handleInboundOperation` holds.

**Alternatives rejected**: (a) Go-owned state queried by the frontend — rejected, adds polling/round-trips and splits source of truth across two layers. (b) Persisting the flag — rejected by FR-002 (must reset to OFF on page/session exit, never stored).

### D2 — Confirm-click is a Wails-bound method keyed by `tool_id`

**Decision**: A new bound method `ConfirmToolResult(toolID string) error` on `*App`. The frontend calls it when the user clicks a result's "Confirm" control. The Go side looks up the pending hold for `toolID` and signals it (see D4). It is a no-op (logged) if the toolID is not currently held — e.g., the 15-minute auto-continue already released it.

**Rationale**: `tool_id` is already the correlation key end-to-end (`projects/game/agent/src/operation-bridge.ts` stamps a UUID on the outbound tool Part; the desktop echoes it in the `ToolResultPart`, `projects/game/desktop/frontend/src/api.ts:108`). Reusing it for the confirm signal adds no new identity scheme.

**Alternatives rejected**: (a) A Wails runtime event frontend→Go (`EventsEmit`) — Wails v2 supports frontend→backend events, but a bound method gives a clean request/error return and matches the existing pattern (all other frontend→Go calls are bound methods in `api.ts`). (b) A "confirm next" (no toolID) signal — rejected; multiple holds are theoretically possible (spec Edge Case "multiple tool results produced in one inbound frame") and must be addressed individually, though in practice the blocking `recvLoop` serializes them (see D7).

### D3 — The held-result indicator is an out-of-band Wails event, NOT a marker inside the SSE frame

**Decision**: When the desktop holds a result it emits a new Go→frontend event `game:debug:result-held` carrying `{ toolId }`. When the result is released (confirm / auto-continue / debug-off / disconnect) it emits `game:debug:result-released` carrying `{ toolId, reason }`. The frontend keeps a reactive `Set<string>` of held toolIDs; `ChatView` renders a "Confirm" button on any `toolResult` Part whose `toolId` is in that set, and removes it when the toolID leaves the set.

Crucially, **the tool-result data itself still travels the unchanged SSE chat stream** (`chatStreams.Append`), exactly as today — only its *timing* relative to the agent send changes, plus the two out-of-band debug events. The chat stream remains append-only and is not mutated.

**Rationale**: This satisfies FR-012 for free — display-only results pushed by the agent (`pushResult`) and history-replayed results never receive a `result-held` event, so they never show a Confirm button. It also satisfies FR-015: the SSE chat channel and the `game:log` forwarding mechanism are unchanged; `game:debug:*` is a new, additive event name. Ordering is robust: if `result-held` arrives before the SSE frame renders, the toolID is simply pre-loaded into the held set and the Confirm button appears once the bubble renders; if the SSE frame never renders (SSE disconnected), the hold still auto-continues at 15 min, so nothing hangs.

**Alternatives rejected**: (a) Embedding a `pending` flag on the `ToolResultPart` in the appended frame, then appending a second "released" frame — rejected: the stream is append-only with no mutation/delete, so this would either leave a stale button forever or pollute the message thread with control frames; it also changes the on-wire Part shape. (b) A dedicated debug SSE stream — rejected, adds a second transport for no benefit; the Wails event channel already exists.

### D4 — The hold is a blocking `select` in `handleInboundOperation`, with a 15-minute auto-continue and disconnect awareness

**Decision**: In `handleInboundOperation` (`projects/game/desktop/app.go:611`), when debug mode is ON, the sequence becomes:

1. Compute the result (`executeAgentOperation`, line 612) — unchanged.
2. Append the result frame to the chat stream (`chatStreams.Append`) — moved **before** the agent send, so the user sees the result while it is held.
3. Register a pending hold `{ toolID, confirmCh chan struct{} }` in a new per-App holds map (mutex-guarded) and emit `game:debug:result-held { toolId }`.
4. `select` on: `<-confirmCh` (user confirmed via `ConfirmToolResult`), `<-time.After(15*time.Minute)` (auto-continue, FR-013), or `<-a.ctx.Done()` (app/session shutdown). On any branch, emit `game:debug:result-released { toolId, reason }`, remove the hold from the map, then proceed to step 5.
5. Send the result to the agent (`ws.SendFrame`, line 638) — unchanged.

When debug mode is OFF, steps 2–4 are skipped and the existing `compute → send → append` order is preserved (FR-011).

**Rationale**: A goroutine blocking on a channel for up to 15 min is free (the agent is itself just waiting for the result, so no frames arrive during the hold — the react-agent loop does not advance past a tool call until its result returns). The `ctx.Done()` arm guarantees no turn is left hanging if the app/session shuts down (spec Edge Case "leaving the session with a held result"); this composes with feature 017's disconnect handling.

**Alternatives rejected**: (a) A non-blocking queue with a separate dispatcher goroutine — rejected, adds concurrency complexity for no gain since `recvLoop` already serializes operations and the agent sends one tool call at a time. (b) Holding on a timer only (no manual confirm) — rejected by FR-008/FR-009.

### D5 — DEBUG logging: gated emission in both the frontend logger and the Go applog

**Decision**:
- **Frontend** (`projects/game/desktop/frontend/src/logger.ts`): add a module-level `debugEnabled` flag (default false) set via a new `setDebugEnabled(bool)`. Add a `logDebug(source, message, fields?)` helper (or extend `log` to short-circuit when `level === 'debug' && !debugEnabled`). When debug is OFF, DEBUG entries are never pushed to the sink nor to the console. The frontend owns the flag (it owns the toggle) and calls `setDebugEnabled` directly on toggle.
- **Go backend** (`projects/game/desktop/internal/applog/logger.go`): add a `Debug(source, msg, fields...)` method and an atomic `debugEnabled` flag (`atomic.Bool`) settable via a new `SetDebug(bool)`. When the flag is false, `Debug` is a no-op (no append, no event-sink push) so there is zero overhead in normal operation. `*App.SetDebugMode` (D1) calls `a.logger.SetDebug(enabled)`.
- The existing `game:log` event sink already carries a `level` field (`projects/game/desktop/internal/applog/logger.go:10`), and `LogPanel.svelte` already styles by level (`log-{entry.level.toLowerCase()}`, line 35), so a `log-debug` style is added for visual distinction. This keeps the `game:log` forwarding mechanism unchanged (FR-015) — only new DEBUG entries flow through it when debug is ON.

**Rationale**: Per the clarification in [spec.md Clarifications](./spec.md#clarifications), DEBUG entries originate in both layers. An atomic/flag-gated `Debug` method gives zero-cost no-op in production (debug OFF is the default). Reusing the `level` field and existing sink avoids any new transport.

**What constitutes a DEBUG entry** (plan-level, constrained by FR-004): frontend — inbound chat frames / SSE events as processed by the view; Go backend — tool execution steps, result computation, and hold lifecycle events (held/released). Exact log messages are finalized during implementation.

**Alternatives rejected**: (a) Always emit DEBUG and filter in the panel — rejected, leaks verbose entries over `game:log` even when off and adds panel-side filtering. (b) A separate debug log sink — rejected, fragments the unified panel.

### D6 — Agent-side timeout is a one-constant change, validated against existing agent large tests

**Decision**: Change `DISPATCH_TIMEOUT_MS` (`projects/game/agent/src/operation-bridge.ts:35`) from `5_000` to `1_200_000` (20 min). No other agent-side change. This is global (the constant is not mode-aware), per FR-014.

**Rationale**: Because 20 min > 15 min (the desktop auto-continue), the agent backstop never fires during normal debug usage; it only catches a genuinely stalled desktop. In non-debug operation results return in milliseconds, so the longer timeout is dormant (spec Edge Case "non-debug tool-result latency").

**Validation note**: The agent service is a gRPC service with existing large tests at `projects/game/testplan/` (e.g., `agent_operation_test.go`, `system_test.yaml`). The constant change must be checked against any test that asserts the prior 5 s timeout; tests exercising normal dispatch (fast result return) are unaffected. Per `style/large_test.md`, large tests are scoped to gRPC/HTTP services — the agent qualifies, the desktop client does not.

**Alternatives rejected**: (a) Mode-gating the timeout (only long when a debug signal is present) — rejected by the user's explicit decision ("agent 服务端扩大超时时间到 20 分钟作为兜底") and would require a desktop→agent debug signal, expanding scope into the agent's execution path. (b) Keeping 5 s — rejected, breaks transparency for any confirmation > 5 s (spec Q2).

### D7 — Go synchronization: mutex-guarded holds map + atomic debug flag

**Decision**: Add to `*App` a `holds map[string]*hold` guarded by a new `holdsMu sync.Mutex`, and an `atomic.Bool` (or mutex-guarded bool) for debug mode. `ConfirmToolResult`, `SetDebugMode` (release-all-on-off), and the hold's `select`-release path all operate under `holdsMu` when touching the map. The debug flag read in `handleInboundOperation` uses the atomic load.

**Rationale**: `handleInboundOperation` runs on the `recvLoop` goroutine; `ConfirmToolResult`/`SetDebugMode` run on Wails-bound-method goroutines. The holds map is the shared state between them and needs a mutex. An atomic bool for the debug flag avoids taking the mutex on the hot read path. The blocking `select` serializes tool results naturally (the next inbound tool Part is not processed until the current hold releases), matching spec Edge Case "multiple tool results ... confirm each in turn".

**Alternatives rejected**: A channel-based debug bus — rejected, over-engineered for a single boolean and a small map.

### D8 — Frontend held-state as a reactive prop into ChatView

**Decision**: `App.svelte` keeps `let heldToolIds = $state<Set<string>>(new Set())`, subscribes to `game:debug:result-held` / `game:debug:result-released` (registered once in `onMount`, like the existing `game:log` listener in `main.ts`), and passes `heldToolIds` plus an `onConfirm` callback down to `ChatView`. `ChatView` renders a "Confirm" button on a `toolResult` Part when `heldToolIds.has(part.toolResult.toolId)`; clicking calls `onConfirm(toolId)` → `api.confirmToolResult(toolId)` (a new wrapper for `ConfirmToolResult`). Because Svelte 5 `$state` is deeply reactive and `Set` reactivity is achieved by reassigning a new Set on change (`heldToolIds = new Set(...)`), the button appears/disappears reactively.

**Rationale**: Follows the existing prop/callback pattern `App.svelte` already uses to wire `ChatView` (lines 804–814 pass messages + callbacks). Authority for Svelte 5 reactivity: [Svelte 5 — $state](https://svelte.dev/docs/svelte/$state), [Svelte 5 — $props](https://svelte.dev/docs/svelte/$props), and the v5 event-handler shorthand (`onclick`, [Svelte 5 migration guide](https://svelte.dev/docs/svelte/v5-migration-guide)).

**Alternatives rejected**: A Svelte store for held IDs — rejected, `$state` runes are the project's established pattern (`App.svelte` uses `$state` throughout); a store would be inconsistent.

### D9 — Testing strategy (Constitution §IV, §VI)

**Decision**:
- **Go unit tests** (small, per-change — Constitution §IV): test the new holds map + `SetDebugMode`/`ConfirmToolResult` logic and the `handleInboundOperation` hold/release/auto-continue branches in isolation (the hold waits on a channel/timer, trivially unit-testable by signalling the channel or shortening the timeout in a test). Existing Go test files: `projects/game/desktop/app_test.go`, `projects/game/desktop/view_model_test.go`.
- **Frontend compile gate** (small): `svelte-check` typecheck + `vite build` (the frontend has no JS test runner — `projects/game/desktop/frontend/package.json` lists only `typecheck` and `build`). No JS unit tests are added; this matches the repo's frontend testing baseline.
- **Agent timeout**: the `DISPATCH_TIMEOUT_MS` constant change is verified by the existing agent large tests at `projects/game/testplan/` (Constitution §VI / `style/large_test.md` — gRPC/HTTP services). No new large-test plan is created for the desktop (it is a client, out of large-test scope per `style/large_test.md` "测试对象"); the feature's end-to-end behavior is validated manually via [quickstart.md](./quickstart.md).
- **Large-test acceptance (Constitution §VI)**: the desktop client is not a gRPC/HTTP service, so it is out of the large-test mandate; the only service touched is the agent, whose existing large tests cover dispatch and must remain green after the timeout change.

**Rationale**: Constitution §IV requires compile+unit per change as part of development (not a separate task). Constitution §VI requires large tests for service-type applications; per `style/large_test.md` that means gRPC/HTTP services — the agent qualifies and is covered by existing tests, the desktop client does not qualify.

## Open items deferred to tasks/implementation

- Exact DEBUG log message wording in each layer (constrained by FR-004).
- Exact copy/label of the "Confirm" control and the Debug switch (spec defers to implementation).
- Whether to show a transient "auto-continued" notice after the 15-min timeout (spec Edge Case; a debug log entry is the minimum).
- Visual style for `log-debug` entries in `LogPanel.svelte`.
