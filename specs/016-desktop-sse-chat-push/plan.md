# Implementation Plan: Desktop SSE Chat Message Delivery

**Branch**: `016-desktop-sse-chat-push` | **Date**: 2026-06-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/016-desktop-sse-chat-push/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Reroute desktop chat-dialog message delivery from the framework's host→webview
event channel (`runtime.EventsEmit("game:frame")`) — which silently drops
messages once the desktop window loses foreground (the root cause of the dialog
freeze introduced by feature 015) — onto a **renderer-initiated, one-way
[Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html)
channel** that rides the webview's native networking and is therefore robust to
focus changes. Both history replay and live streaming flow through this **single**
channel (one frontend consumer). The desktop backend owns a per-session,
in-memory ordered event log with stable monotonic event ids, giving automatic
`Last-Event-ID` reconnect with no duplicate rendering. Large payloads
(screenshots up to 5 MiB) are fragmented into small SSE events and reassembled,
so Chromium's per-event buffer never silently drops them. Only chat dialog
delivery migrates; log forwarding and all other notifications stay on their
existing mechanism.

The chosen transport is a **separate loopback `net/http.Server`** — verified
impossible to run through the Wails asset server on Windows (the platform's
response writer is fully buffered with no `http.Flusher`;
[wailsapp/wails#2847](https://github.com/wailsapp/wails/issues/2847)). See
[research.md](research.md) R-001 for the source-grounded decision.

## Technical Context

**Language/Version**: Go 1.23 (desktop backend), TypeScript 5.x / Svelte 5 (frontend). Inherited from feature 015 / [015 plan](../015-desktop-agent-refinement/plan.md).

**Primary Dependencies**: Go stdlib `net/http` (the SSE server — no new external dependency; stdlib HTTP fully supports `http.Flusher` streaming), [Wails v2.12.0](https://github.com/wailsapp/wails/releases/tag/v2.12.0) (desktop framework, vendored under `third_party/`), browser-native `EventSource` ([HTML §9.2](https://html.spec.whatwg.org/multipage/server-sent-events.html), supported in WebView2/Chromium — [CanIWebView](https://caniwebview.com/features/mdn-eventsource/)), `crypto/rand` (token generation), `google.golang.org/protobuf/encoding/protojson` (existing serialization, reused). **No new third-party dependency is introduced.**

**Storage**: N/A — the per-session event log is in-memory only (single-session desktop client); durable history recoverability comes from the existing agent `ListMessages` RPC, unchanged.

**Testing**: `bazel test` — Go unit tests via `go_test` using `net/http/httptest` for the SSE server, log, token rotation, chunking, and `Last-Event-ID` resume; vitest for the frontend chunk-reassembly + id-dedup pure logic; existing `app_test.go` extended to guard the `recvLoop` refactor. **Testing boundary**: the loopback SSE server is part of the desktop process, not a deployed service, so it is **not** covered by the cluster large-test testplan (`projects/game/testplan`, which targets gateway→session→proxy→agent); the focus-loss/reconnect end-to-end behavior is validated by the runnable scenarios in [quickstart.md](quickstart.md). This mirrors feature 015's desktop-frontend validation split (see [testplan/README.md §7](../../projects/game/testplan/README.md)).

**Target Platform**: Windows (desktop backend — WebView2; the defect's platform). The Go SSE server is cross-platform; Linux/macOS builds run but cannot reproduce the US1 foreground-loss trigger.

**Project Type**: desktop-app (Wails Go + Svelte) — unchanged.

**Performance Goals**: < 1 s per-event rendering in the dialog (SC-001, inherited from 015); the SSE server adds only loopback localhost hops (sub-millisecond) on top of the existing agent→desktop path.

**Constraints**: loopback-only binding, non-remote-reachable (FR-008); screenshot ≤ 5 MiB (inherited from 014/015); per-event SSE payload ≤ 48 KiB (chunking ceiling, R-002); session-scoped token rotated per stream opening (FR-007/FR-008); only chat dialog delivery migrates (FR-006).

**Scale/Scope**: single-session, single-window desktop automation. Change spans one new Go package (`internal/chatstream`), a refactor of one method (`recvLoop`) plus two new Wails bindings in `app.go`, a small `main.go` lifecycle wiring, and one frontend file (`App.svelte`) plus its `api.ts` types.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: every external fact, dependency choice, and API reference in this plan carries an inline `[description](URL)` link (HTML SSE spec sections, Wails issues/PRs/release, Fetch CORS, CanIWebView) with matching entries in `## References`. Findings are consolidated in [research.md](research.md) with full citations. ✅
- **Version pins or commit SHAs**: Wails v2.12.0 pinned (vendored under `third_party/github.com/wailsapp/wails`); the Windows `responseWriter` buffering claim is grounded in the vendored source at `pkg/assetserver/webview/responsewriter_windows.go`. ✅
- **All cited links resolve to publicly accessible resources**: HTML spec, GitHub issues/PRs/releases, Fetch standard, CanIWebView, MDN. ✅
- **Code Style Precedence (§II)**: every implementation task exported to `tasks.md` MUST reference `style/golang.md` (Go: three-group imports, object-vs-tuple pointer semantics, `new` for uninitialized pointers, table-driven tests, `nil` slices, otel observability) and `style/README.md` (TS: Google TypeScript Style) before code changes begin, and confirm review. ✅ — inherited by `tasks.md`.
- **External Dependency Research (§III)**: the only "external" components are Go stdlib `net/http`, the already-vendored Wails v2.12.0, and the browser-native `EventSource`. All three are researched against authoritative sources in [research.md](research.md): Wails v2.12.0 source (vendored) + upstream issues/docs for the streaming limitation (R-001); HTML §9.2 + Fetch §3.3 for SSE/CORS semantics (R-002). No new third-party dependency is introduced, so no version-selection research is outstanding. ✅
- **Refactoring-Oriented Changes (§IV)**: every change below is classified 新增 / 修改 / 删除. The single 修改 (`recvLoop`) is implemented as a refactor of the existing unit (delivery-hop destination change), not logic stacking, with an explicit design verdict. 删除 items (`EventsOn('game:frame')`, `handleLoadMessages` one-shot path) carry a verdict that the removed design is superseded by the unified channel. ✅

*Pre-Phase-0 gate*: no violations. No NEEDS CLARIFICATION remained after research — [research.md](research.md) R-001…R-005 resolved the transport choice, the security model, the event-id/dedup design, the chunking requirement, and the lifecycle/scope boundary.

## Project Structure

### Documentation (this feature)

```text
specs/016-desktop-sse-chat-push/
├── plan.md              # This file (/speckit.plan output)
├── research.md          # Phase 0 — R-001 … R-005 (transport, SSE spec, log model, security, lifecycle)
├── data-model.md        # Phase 1 — ChatStream / ChatEvent / subscriber / handoff + dedup + chunking
├── quickstart.md        # Phase 1 — runnable validation scenarios (US1/US2/US3 + scope boundary)
├── contracts/
│   └── chat-stream.md   # Phase 1 — SSE wire contract + OpenChatStream handoff + reconnect/chunking
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/desktop/
├── app.go                              # [修改] OpenChatStream/CloseChatStream bindings; recvLoop → chatstream.Append
├── view_model.go                       # [修改] add ChatStreamHandoff view model
├── main.go                             # [修改] start/stop the chatstream server in OnStartup/shutdown
├── internal/chatstream/                # [新增] loopback SSE server + per-session log + token + chunking
│   ├── server.go                       #      net/http.Server on 127.0.0.1:0; SSE handler; CORS; token check
│   ├── stream.go                       #      ChatStream per-session log: Append, subscribe, history seed
│   ├── stream_test.go                  #      (go_test) log ordering, ids, Last-Event-ID resume, fan-out
│   ├── server_test.go                  #      (go_test) loopback bind, 401 on bad/stale token, SSE framing
│   └── chunk.go (+ chunk_test.go)      #      serialize event; fragment > maxEventBytes; reassemble aid
└── frontend/src/
    ├── api.ts                          # [修改] OpenChatStream/CloseChatStream wrappers; ChatStreamHandoff type
    └── App.svelte                      # [修改] replace EventsOn('game:frame') + handleLoadMessages with EventSource
```

**Structure Decision**: Existing monorepo layout — **no new top-level directory or project**. The only new package is `projects/game/desktop/internal/chatstream/`, placed under the already-existing `desktop/internal/` package root alongside `api/`, `capture/`, `operation/`, `applog/`, `trace/`, following the `golang.md` layering convention (a `runtime`-style leaf package for an in-process subsystem). New files are added to already-existing directories (`app.go`'s package, `frontend/src/`).

## Change Classification (§IV)

### Desktop Go — new chatstream package (US1/US2/US3)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 1 | `internal/chatstream/server.go` | 新增 | `Server` owning a `net/http.Server` bound to `127.0.0.1:0`; serves `GET /api/v1/chat/stream` as `text/event-stream` with `http.Flusher`, CORS `Access-Control-Allow-Origin: *`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`; validates `?token=` against the session's `ChatStream` (401 on missing/stale); reads `Last-Event-ID` and replays `id > last` then streams live via the subscriber channel; optional `: keepalive` / `retry:` lines. | New module. Pure Go stdlib HTTP (full `http.Flusher` support), the only viable transport on Windows per R-001. |
| 2 | `internal/chatstream/stream.go` | 新增 | `ChatStream` (per-session append-only log, monotonic `nextID`, `subscribers`, current `token`), `ChatEvent{id, *AgentFrame}`, `subscriber{chan, done, lastEventID}`; `Append(frame)` assigns the next id and fans out; `Subscribe(lastEventID)` seeds the backlog then live; `SeedFromHistory([]*game.Message)` normalizes history `Message`→content `AgentFrame` and appends; `RotateToken()`; `Registry` (`map[string]*ChatStream`) keyed by session. | New module. The log is the single owner of event identity, making ids stable across history/live/reconnect (FR-003a). Object semantics → pointers per `style/golang.md`; container elements `map[string]*ChatStream`. |
| 3 | `internal/chatstream/chunk.go` | 新增 | `Serialize(frame) []byte` (protojson → reuse `frameToMap` projection); `Fragment(json, maxEventBytes=48KiB)` → `[]chunkPiece{groupId,index,total,fragment}`; constants/envelopes for the wire chunk protocol. | New module. Generic JSON-fragment chunking (not screenshot-specific) so any large payload is handled; keeps the `AgentFrame` shape unchanged. |
| 4 | `internal/chatstream/*_test.go` | 新增 | `go_test` (table-driven per `style/golang.md`): loopback-only listen; 401 missing/stale token; history-seed ordering; monotonic ids; `Last-Event-ID` resume replays `id>last`; >48KiB event fragments and reassembles to original; fan-out to multiple subscribers; screenshot (~5MiB) round-trip. | New tests; `httptest`-based (no network egress per `style/golang.md` 单测). |

### Desktop Go — `app.go` bindings + `recvLoop` refactor (US1/US2/US3)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 5 | `app.go` — `recvLoop` | 修改 | **Refactor the delivery hop**: replace both `runtime.EventsEmit(a.ctx, "game:frame", frameToMap(...))` call sites (received-frame emit + error-wait emit) with `a.chatStream.Append(sessionID, resp)` / `Append(... synthesized wait ...)`. The draining loop, the `frameCount` logging, the tool-request scan, and `handleInboundOperation` are preserved unchanged. | **Refactor**: `recvLoop`'s responsibility is "drain agent frames and deliver them to the frontend." Today that means `EventsEmit`; the defect is precisely that this hop silently drops. Rerouting the *same* frame set to `chatStream.Append` is the minimal, coherent change to the existing unit — no new branch or parallel path is added. `SendUserTurn`/`CloseAgent`/`handleInboundOperation`/`executeAgentOperation` and the agent WebSocket are untouched (Assumptions). The emission set is provably identical (exactly two `game:frame` call sites → two `Append` call sites). |
| 6 | `app.go` — `OpenChatStream` / `CloseChatStream` | 新增 (methods on existing `App`) | Two new exported methods bound by Wails: `OpenChatStream(sessionID) (*ChatStreamHandoff, error)` (get/create+history-seed the `ChatStream`, rotate token, return endpoint+token+lastEventId) and `CloseChatStream(sessionID) error` (drop the stream on session leave). | New methods on the existing `App` receiver (the Wails binding surface). They expose the chatstream subsystem to the frontend; no existing method's signature changes. |
| 7 | `app.go` — `App` struct | 修改 | Add fields `chatStream *chatstream.Registry` (or `*chatstream.Server`) and inject the running server set up by `main.go`. | Minimal extension of the existing struct to hold the new dependency; no existing field semantics change. |
| 8 | `view_model.go` — `ChatStreamHandoff` | 新增 | `ChatStreamHandoff{Endpoint, Token string; LastEventID int64}` with camelCase `json` tags, matching the existing view-model convention. | New view model for the FR-007 handoff; reuses the file's existing serialization conventions. |

### Desktop Go — lifecycle wiring

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 9 | `main.go` — `OnStartup` | 修改 | Construct `chatstream.NewServer(...)`, start it (`Start()` → listens on `127.0.0.1:0`), inject it into `App`, and register a shutdown (`OnShutdown` or the existing app context cancellation) to stop it cleanly. | Minimal lifecycle wiring mirroring how the logger sink is already wired in `OnStartup`. The server's lifetime is bound to the backend lifetime (FR-010). |

### Frontend — switch the chat delivery source (US1/US2/US3)

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 10 | `App.svelte` — `onMount` event source | 修改 / 删除 | **Remove** `runtime.EventsOn('game:frame', …)` (the silent-drop path). **Add** an `EventSource` connection lifecycle: on session entry call `openChatStream(sessionId)` → `new EventSource(endpoint + '?token=…')`; listen `event: chat` → dedup-by-id → `handleAgentFrame`; listen `event: chunk` → reassemble by `groupId` → `handleAgentFrame`; track rendered ids + completed groupIds for reconnect dedup; close the `EventSource` on session leave. | **Refactor of the chat-delivery source**: the dialog's single consumer (`handleAgentFrame` + `handleContentPayload`) is unchanged; only *where frames come from* changes, from the host event channel to the SSE stream. The old `game:frame` listener is deleted because it is superseded by the unified channel (FR-001/FR-004). |
| 11 | `App.svelte` — `handleLoadMessages` | 删除 / 修改 | **Remove** the one-shot `listMessages` history fetch from the session-entry flow; history now arrives as early events on the SSE stream (FR-004/FR-005). The `handleSelectSession`/`handleSendChatText` callers are updated to open/refresh the SSE stream instead. | **Verdict**: the one-shot history fetch is the second of the two data-push paths the user explicitly wants eliminated. It is fully superseded by history replay on the SSE channel; deleting it removes the divergence risk (history-vs-live rendering). `ListMessages` backend remains for the SSE history seed and any non-chat consumers; only the chat-view one-shot fetch is removed. |
| 12 | `App.svelte` — reconnect handling | 新增 | On `EventSource` `error`/reconnect, rely on the browser's auto-reconnect + `Last-Event-ID`; on a `401` (token rotated, e.g. after re-entry) treat as fresh and re-`openChatStream`. Add small pure helpers (chunk reassembly, id-dedup set) so they are unit-testable in vitest without the webview. | New reconnect/reassembly logic; isolated as pure functions for testability. |

### Frontend — types

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 13 | `api.ts` | 修改 | Add `ChatStreamHandoff` interface and `openChatStream`/`closeChatStream` wrappers (camelCase, matching the existing `WailsApp` interface + wrapper pattern). | Minimal type/wrapper additions following the file's existing convention; no existing type changes. |

### Out of scope (explicitly unchanged — FR-006 + Assumptions)

- `game.proto` (no proto change — `AgentFrame`/`Message`/`PartBlock` reused).
- `internal/api/` (desktop↔agent WebSocket: `ConnectAgent`, probe, `SendFrame`/`RecvFrame`).
- `game:log` log forwarding in `main.go` (stays on `runtime.EventsEmit`).
- Agent / proxy / session / gateway services.
- `handleAgentFrame` / `handleContentPayload` rendering (unchanged consumer).
- `ChatView.svelte`, `ChatMessage.svelte`, `ScreenshotModal.svelte` (rendering unchanged).

## Complexity Tracking

No Constitution Check violations. No complexity justifications needed. The single
non-obvious decision (separate `net/http.Server` vs Wails asset server) is forced
by a verified platform limitation (R-001), not a preference, and adds no new
external dependency (Go stdlib only).

## Constitution Check (post-Phase-1 re-check)

*GATE: Re-check after Phase 1 design.*

- **§I (Citations)**: plan, research.md, data-model.md, contracts/chat-stream.md all carry inline citations + consolidated References sections. ✅
- **§II (Style)**: every Go task targets `style/golang.md` (pointer semantics for `ChatStream`/`ChatEvent`/`subscribers`, three-group imports, table-driven `go_test`, `nil` empty slices, otel logging at entry points); every TS task targets `style/README.md` (Google TS Style). Flagged for `tasks.md` inheritance. ✅
- **§III (Dependency Research)**: Wails v2.12.0 (vendored + upstream), Go stdlib `net/http`, HTML §9.2, Fetch §3.3 all researched and cited in research.md. No new dependency left unresearched. ✅
- **§IV (Refactoring)**: 1 修改 (`recvLoop`) is a true delivery-hop refactor with an identical emission set and a recorded verdict; 2 删除 (`game:frame` listener, one-shot history fetch) carry verdicts that the removed design is superseded; all 新增 items are genuinely new modules/methods/types. The `App.svelte` source switch is classified as 修改/删除 of the existing chat-source unit, not logic stacking. ✅
- **Design-implementation coherence**: the design (this plan + data-model.md + contracts/chat-stream.md) and the planned implementation agree; the `AgentFrame` shape is unchanged end-to-end, so frontend rendering (`handleAgentFrame`) needs no change. ✅

No gates violated post-design.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation
- [HTML Living Standard §9.2 Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) — the one-way push transport chosen for chat delivery.
- [HTML §9.2.2 The EventSource interface](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-eventsource-interface) — no custom headers → token via query param.
- [HTML §9.2.3 Processing model / reestablishing the connection](https://html.spec.whatwg.org/multipage/server-sent-events.html#processing-model) — auto-reconnect + `Last-Event-ID`.
- [HTML §9.2.4 The Last-Event-ID header](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-last-event-id-header).
- [HTML §9.2.6 Interpreting an event stream](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream) — `id:`/`event:`/`data:`/`retry:` framing; comments.
- [Fetch Standard §3.3 CORS protocol](https://fetch.spec.whatwg.org/#cors-protocol) — `Access-Control-Allow-Origin` for cross-origin `EventSource`.
- [Wails v2 AssetServer options & feature matrix](https://wails.io/docs/reference/options#assetserver) — Windows "Response Body Streaming ❌".

### Repositories
- [wailsapp/wails#2847 — EventSource/SSE panics: `contentTypeSniffer is not http.Flusher`](https://github.com/wailsapp/wails/issues/2847) — confirmed failure + separate-`net/http.Server` workaround (the deciding evidence for R-001).
- [wailsapp/wails v2.12.0 release](https://github.com/wailsapp/wails/releases/tag/v2.12.0) — pinned version; no v2 streaming backports.
- [whatwg/html#2177 — EventSource custom headers](https://github.com/whatwg/html/issues/2177) — confirms no header support.
- [MicrosoftEdge/WebView2Feedback#3519](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3519) — EventSource works in WebView2 unless intercepted via `WebResourceRequested` (corroborates R-001).
- [wailsapp/wails#4418 — WebView2 Bridge silently drops ExecuteScript](https://github.com/wailsapp/wails/issues/4418) — the framework defect motivating this feature (cited in spec).
- [wailsapp/wails#2861 — bindings break when app is backgrounded](https://github.com/wailsapp/wails/issues/2861) — the focus-loss trigger (cited in spec).

### Articles & RFCs
- [SSE maximum payload size limits](https://www.server-sent-events.com/sse-protocol-fundamentals-architecture/understanding-the-event-stream-format/maximum-payload-size-limits-for-sse-streams/) — why large payloads (screenshots) must be chunked (R-002).
- [CanIWebView — EventSource](https://caniwebview.com/features/mdn-eventsource/) — full WebView2 support for `EventSource`.
