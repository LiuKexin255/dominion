# Research: Desktop SSE Chat Message Delivery

**Feature**: 016-desktop-sse-chat-push | **Date**: 2026-06-27

This document records the research that grounds the implementation plan. Every
finding carries an inline citation per Constitution §I; the research itself was
performed against official documentation and source per Constitution §III.

---

## R-001 — SSE cannot stream through the Wails v2 AssetServer on Windows

**Decision**: The chat push channel MUST run on a **separate Go `net/http.Server`
bound to `127.0.0.1`**, NOT on the Wails `AssetServer.Options.Handler`. The
frontend connects to it cross-origin via the browser-native `EventSource` API.

**Rationale (authoritative, from vendored + upstream source)**:

The desktop targets Windows, where Wails v2.12.0 serves the frontend through
WebView2 by intercepting every request via the `WebResourceRequested` event
([vendored `internal/frontend/desktop/windows/frontend.go` `processRequest`](../../../../third_party/github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/frontend.go)).
The response the handler produces is built by the Windows `responseWriter`, which
**fully buffers the body in a `bytes.Buffer` and ships it once, atomically, in
`Finish()` via `resp.PutByteContent(rw.body.Bytes())`** — there is **no
`Flush()` method**, so the writer does not implement `http.Flusher`
([vendored `pkg/assetserver/webview/responsewriter_windows.go`](../../../../third_party/github.com/wailsapp/wails/v2/pkg/assetserver/webview/responsewriter_windows.go)).
The `contentTypeSniffer` wrapper applied in the request chain also lacks
`Flush()` in v2 ([vendored `pkg/assetserver/content_type_sniffer.go`](../../../../third_party/github.com/wailsapp/wails/v2/pkg/assetserver/content_type_sniffer.go)).

This is an intentional, documented platform limitation. The Wails v2 feature
matrix marks **"Response Body Streaming: Windows ❌ / macOS ✅ / Linux ✅"**
([wails.io/docs/reference/options#assetserver](https://wails.io/docs/reference/options)).
A real user attempting SSE through the asset server hit the exact panic
`*assetserver.contentTypeSniffer is not http.Flusher: missing method Flush`
([wailsapp/wails#2847](https://github.com/wailsapp/wails/issues/2847)); the
community-verified resolution in that thread (and #1568, #2116) is to run a
separate `net/http.Server` on `127.0.0.1:<port>`. All `Flush()` fixes
(PR #3537, #4128, #4245) landed in **v3 only**; the v2.12.0 release notes contain
no streaming/SSE changes ([v2.12.0 release](https://github.com/wailsapp/wails/releases/tag/v2.12.0)).

A separate `net/http.Server` works because WebView2 lets requests whose host
differs from the Wails `startURL.Host` (`http://wails.localhost` on Windows,
[vendored `frontend.go` `const startURL`](../../../../third_party/github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/frontend.go))
fall through to the default network stack instead of the asset interceptor, so a
cross-origin `EventSource` to `http://127.0.0.1:<port>` is a normal browser
request.

**Alternatives considered**:
- *Serve SSE through `AssetServer.Options.Handler` (same origin, no CORS, no
  port).* Rejected: impossible on the target platform (Windows) per the source
  above — responses are fully buffered, so an SSE handler would either never
  deliver (handler never returns) or buffer the whole stream until return,
  defeating SSE entirely, and a `Flush()` call panics. This would also re-fail
  under backgrounding because delivery still depends on the host→webview path.
- *Upgrade to Wails v3.* Rejected: v3 is alpha; the production desktop is pinned
  to v2.12.0 (`third_party/github.com/wailsapp/wails`), and even v3's Windows
  writer remains buffered.

---

## R-002 — EventSource / SSE transport semantics

**Decision**: Use the browser-native `EventSource` over a loopback HTTP server.
The server assigns a **monotonic integer event `id:`** per logical chat event,
relies on the browser's automatic `Last-Event-ID` reconnect, and **chunks any
event whose payload exceeds ~48 KiB** so screenshots (up to ~6.7 MiB base64) are
never sent as a single SSE event.

**Rationale (HTML Living Standard §9.2)**:

- An SSE message is a set of `field: value` lines — `data:`, `event:`, `id:`,
  `retry:` — terminated by a blank line; the MIME type MUST be
  `text/event-stream` and the body decoded as UTF-8
  ([HTML §9.2.5 parsing](https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream),
  [§9.2.6 interpreting](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream)).
- `EventSource` reconnects automatically after a drop (default delay a few
  seconds; tunable via `retry:`) and **on reconnect the browser automatically
  sends the `Last-Event-ID` request header carrying the last `id:` it received**
  ([HTML §9.2.3 reestablishing the connection](https://html.spec.whatwg.org/multipage/server-sent-events.html#processing-model),
  [§9.2.4 the Last-Event-ID header](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-last-event-id-header)).
  The server therefore replays only events with `id > Last-Event-ID`. An HTTP
  `204` response tells the client to stop reconnecting.
- `EventSource` cannot set custom headers — `EventSourceInit` has only
  `withCredentials` ([HTML §9.2.2 interface](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-eventsource-interface);
  [whatwg/html#2177](https://github.com/whatwg/html/issues/2177)). The
  session-scoped authorization token is therefore passed as a **URL query
  parameter** (`?token=…`), the standard workaround.
- `EventSource` is a CORS-capable "simple" GET request needing no preflight. With
  no credentials, `Access-Control-Allow-Origin: *` suffices
  ([Fetch §3.3 CORS](https://fetch.spec.whatwg.org/#cors-protocol)).
- EventSource is fully supported in WebView2/Chromium
  ([CanIWebView — EventSource](https://caniwebview.com/features/mdn-eventsource/)).

**Chunking large payloads (screenshots)**: The SSE spec imposes no event-size
limit, but Chromium buffers each event in memory before dispatching it to JS and
**silently drops events that exceed an implementation-defined bound (~1 MiB)**.
A ~6.7 MiB base64 screenshot in a single `data:` field would be lost — directly
violating FR-003 (screenshots complete, no truncation/loss). The standard SSE
pattern is to fragment large payloads into a sequence of small events with a
group/sentinel protocol that the client reassembles
([SSE maximum payload limits](https://www.server-sent-events.com/sse-protocol-fundamentals-architecture/understanding-the-event-stream-format/maximum-payload-size-limits-for-sse-streams/)).
The contract (`contracts/chat-stream.md`) defines this chunking protocol with a
~48 KiB per-event ceiling (well under the buffer bound, leaving headroom for the
envelope and concurrent frames).

**Alternatives considered**:
- *Single giant SSE event per screenshot.* Rejected: silently dropped by
  Chromium, violating FR-003/US1-AS3.
- *Ship screenshot bytes via a separate loopback GET (SSE carries only a
  reference).* Rejected: reintroduces a second fetch path the user explicitly
  wants eliminated (FR-004: one delivery path) and complicates ordering.
- *`fetch()` + `ReadableStream` instead of `EventSource` (to set headers).*
  Rejected: loses automatic `Last-Event-ID` reconnect (FR-009), which is the
  whole point of choosing SSE; the token-in-query workaround keeps `EventSource`.

---

## R-003 — Unified history + live stream owned by the desktop backend

**Decision**: The desktop backend owns a **per-session in-memory ordered event
log** that is the single source for the SSE channel. Opening a stream seeds the
log from the existing history RPC (`ListMessages`), and the existing `recvLoop`
appends live agent frames to it. Each event is assigned a **stable monotonic
integer id** by the log. The log persists across frontend reconnects (same
desktop process, same session) so the same logical event carries the same id in
history-replay, live, and reconnect-replay — satisfying FR-003a. On reconnect
the server replays log entries with `id > Last-Event-ID`; the frontend ignores
ids it has already rendered.

**Rationale (from the codebase + spec)**:

- Today there are two separate delivery paths to the chat dialog: a one-shot
  history fetch (`App.ListMessages` → `client.ListMessages`, [view_model.go
  `ToMessageViewModels`](../../projects/game/desktop/view_model.go)) and live
  push (`recvLoop` → `runtime.EventsEmit("game:frame", …)`, [app.go
  recvLoop](../../projects/game/desktop/app.go)). History `Message` and live
  `AgentFrame` carry **different ID spaces** (`message_id` vs `frame_id`,
  [game.proto](../../projects/game/game.proto)), so neither ID is stable across
  history and live. A unified stream therefore needs **its own** stable id; the
  desktop-owned monotonic log is the natural place because the desktop is the
  sole consumer of both history and live on the way to the frontend.
- The log lives for the active session and is **not** rebuilt on reconnect, so
  ids are stable for the Last-Event-ID resume. The `recvLoop` reads from the
  agent WebSocket (`a.ws`), which is independent of the SSE HTTP connection, so
  it keeps appending frames during a frontend disconnect; those frames are
  recovered on reconnect (FR-009) instead of being lost.
- Both history `Message.content` (a `PartBlock`) and live `AgentFrame.content`
  (a `PartBlock`) are the **same shape** ([game.proto](../../projects/game/game.proto)),
  so the SSE channel can normalize every event to an `AgentFrame`-shaped JSON and
  the frontend keeps exactly one consumer (`handleAgentFrame`) — history and live
  render identically (FR-004/FR-005).
- The emission set is exactly the two `runtime.EventsEmit("game:frame", …)` call
  sites in `recvLoop` ([app.go:545](../../projects/game/desktop/app.go) error
  wait, [app.go:556](../../projects/game/desktop/app.go) each received frame);
  the refactor only changes their destination from `EventsEmit` to
  `chatstream.Append`. The agent↔desktop WebSocket (`ConnectAgent`, the probe,
  `SendFrame`/`RecvFrame`) is unchanged, per the feature's Assumptions.

**Alternatives considered**:
- *Re-seed history on every reconnect and reuse `message_id`/`frame_id` as the
  event id.* Rejected: history and live have disjoint id spaces, so a live frame
  and its later persisted form would get different ids and duplicate on
  reconnect; re-seeding also re-numbers events, breaking `Last-Event-ID` resume.
- *Persist the SSE event log to disk.* Rejected (out of scope): the desktop is a
  single-session client; an in-memory log cleared on session leave is sufficient,
  and history recoverability across app restarts already comes from the agent's
  `ListMessages`.

---

## R-004 — Security model: loopback bind + per-stream token

**Decision**: The SSE server binds to **`127.0.0.1` only** (never `0.0.0.0`) on
an **ephemeral port** (OS-assigned via `:0`, reported back to the frontend).
Every connection MUST present a **session-scoped token**; a new stream opening
rotates the token and invalidates the stale one. The token is delivered to the
frontend through a single Wails-bound handoff call when opening a session stream
(FR-007), and presented by `EventSource` as a `?token=` query parameter.

**Rationale (from the spec + best practice)**:
- FR-008 mandates loopback-only binding and rejection of any non-frontend
  connection. `127.0.0.1` binding makes the endpoint unreachable from remote
  hosts; the token rejects other local processes (FR-008 edge case). Ephemeral
  port selection (`:0`) "avoids conflicts with other local services" (FR-010)
  without a brittle fixed-port scheme.
- The handoff call (FR-007) returns endpoint + token in one request, and a new
  opening rotates the token so stale tokens are rejected (spec clarification
  2026-06-27). Loopback HTTP is not HTTPS, but the threat model is local-only;
  the token is a bearer secret shared in-process, never logged.

**Alternatives considered**:
- *Fixed port with retry.* Rejected: more brittle, more collision-prone, and
  needlessly discoverable.
- *Same-origin asset-server handler (no token needed).* Rejected: impossible per
  R-001, and the spec explicitly prescribes a loopback endpoint + token.

---

## R-005 — Lifecycle and scope boundary

**Decision**: The SSE server starts when the desktop backend starts (`OnStartup`)
and stops cleanly when it stops (FR-010). **Only** chat dialog frames migrate to
it; the `game:log` event (log forwarding) and all other frontend notifications
stay on `runtime.EventsEmit` unchanged (FR-006).

**Rationale (from the codebase)**:
- `main.go` `OnStartup` is where the app context is set and the log event sink is
  wired ([main.go](../../projects/game/desktop/main.go)); starting the SSE server
  there and wiring its shutdown mirrors the existing lifecycle. The server needs
  the Wails `ctx` only indirectly (it owns its own `context`/`net.Listener`).
- Log forwarding is emitted by `main.go`'s `logger.SetEventSink` as
  `runtime.EventsEmit(ctx, "game:log", entry)` — a separate event name from
  `game:frame` ([main.go](../../projects/game/desktop/main.go)). It is explicitly
  out of scope (FR-006) and is left untouched.

**Alternatives considered**: none. The lifecycle and scope follow directly from
FR-006/FR-010 and the existing code.

---

## References

### Official Documentation
- [HTML Living Standard §9.2 Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) — the one-way push transport.
- [HTML §9.2.2 The EventSource interface](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-eventsource-interface) — `withCredentials`-only options (no custom headers).
- [HTML §9.2.3 Processing model / reestablishing the connection](https://html.spec.whatwg.org/multipage/server-sent-events.html#processing-model) — auto-reconnect + `Last-Event-ID`.
- [HTML §9.2.4 The Last-Event-ID header](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-last-event-id-header).
- [Fetch Standard §3.3 CORS protocol](https://fetch.spec.whatwg.org/#cors-protocol) — `Access-Control-Allow-Origin` semantics.
- [Wails v2 AssetServer options & feature matrix](https://wails.io/docs/reference/options#assetserver) — Windows response-body streaming ❌.

### Repositories
- [wailsapp/wails#2847 — EventSource/SSE panics: `contentTypeSniffer is not http.Flusher`](https://github.com/wailsapp/wails/issues/2847) — the confirmed failure + separate-`net/http.Server` workaround.
- [wailsapp/wails v2.12.0 release](https://github.com/wailsapp/wails/releases/tag/v2.12.0) — no v2 streaming/SSE backports.
- [whatwg/html#2177 — EventSource custom headers](https://github.com/whatwg/html/issues/2177) — confirms no header support; token-in-query is the workaround.
- [MicrosoftEdge/WebView2Feedback#3519](https://github.com/MicrosoftEdge/WebView2Feedback/issues/3519) — EventSource works in WebView2 unless intercepted via `WebResourceRequested`.
- [CanIWebView — EventSource](https://caniwebview.com/features/mdn-eventsource/) — full WebView2 support.

### Articles & RFCs
- [SSE maximum payload size limits](https://www.server-sent-events.com/sse-protocol-fundamentals-architecture/understanding-the-event-stream-format/maximum-payload-size-limits-for-sse-streams/) — why large payloads must be chunked.
