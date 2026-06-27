# Contract: Chat Push Stream (SSE)

**Feature**: 016-desktop-sse-chat-push | **Date**: 2026-06-27

This is the interface contract for the local chat push channel — the one-way,
renderer-initiated push stream over which **all** chat dialog messages
(streaming text, reasoning, tool operations, tool results, screenshots, status,
turn-complete) are delivered to the frontend (FR-001). It replaces the framework
host→webview `game:frame` event channel as the chat delivery hop.

Grounded in [research.md](../research.md) (R-001 … R-005) and the entity model in
[../data-model.md](../data-model.md). Transport semantics follow
[HTML Living Standard §9.2 Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html).

---

## 1. Topology

```
┌──────────────── Wails desktop process (Go) ────────────────────┐
│                                                                  │
│   recvLoop ──Append(frame)──▶ ChatStream (per-session log)       │
│      ▲                              │                           │
│      │ ws (UNCHANGED)               │ fan-out                   │
│   agent ◀── gateway/proxy ──        ▼                           │
│                              net/http.Server                    │
│                              127.0.0.1:<ephemeral>              │
│                                  │  SSE (text/event-stream)     │
└──────────────────────────────────┼──────────────────────────────┘
                                   │  cross-origin EventSource (CORS)
                                   ▼
                       WebView2 frontend (Svelte)
```

- The SSE server is a **separate `net/http.Server`** bound to **`127.0.0.1:0`**
  (loopback only, OS-assigned ephemeral port), started in `OnStartup` and stopped
  on shutdown (FR-008, FR-010). It cannot ride the Wails asset server (R-001).
- The agent↔desktop WebSocket (`ConnectAgent`, the probe, `SendFrame`/`RecvFrame`)
  is **unchanged**; only the final frontend-delivery hop changes.
- **Only** chat dialog frames migrate here. Log forwarding (`game:log`) and every
  other frontend notification stay on `runtime.EventsEmit` (FR-006).

---

## 2. Connection handoff — `OpenChatStream` (FR-007)

The frontend obtains the endpoint + token from the backend via a **single**
Wails-bound call when opening a session stream (not per message).

**Wails binding** (Go → `window.go.main.App`):
```go
func (a *App) OpenChatStream(sessionID string) (*ChatStreamHandoff, error)
```

**Returns** `ChatStreamHandoff` (camelCase JSON, [view_model.go](../../../projects/game/desktop/view_model.go) convention):
```jsonc
{
  "endpoint":   "http://127.0.0.1:<port>/api/v1/chat/stream",
  "token":      "<session-scoped opaque secret>",
  "lastEventId": 0
}
```

**Semantics**:
- Creates the session's `ChatStream` if absent, seeding it from
  `client.ListMessages` (history replay, FR-004/FR-005). If the stream already
  exists (reconnect within the same session), it is reused — **not** re-seeded —
  so event ids stay stable (FR-003a).
- **Rotates** the token on every call; the previous token is invalidated (FR-008,
  clarification 2026-06-27).
- `lastEventId` is the highest event id currently in the log (informational).

**Companion binding** to tear a stream down on session leave:
```go
func (a *App) CloseChatStream(sessionID string) error
```
Closes subscribers and drops the `ChatStream`. Idempotent.

---

## 3. SSE endpoint — `GET /api/v1/chat/stream`

### 3.1 Request

| Element | Value |
|---|---|
| Method | `GET` |
| Path | `/api/v1/chat/stream` |
| Query | `token=<ChatStreamHandoff.token>` (required) |
| Header | `Accept: text/event-stream` (set automatically by `EventSource`) |
| Header | `Last-Event-ID: <id>` (sent automatically by the browser **on reconnect only**) |

The token MUST be a query parameter because `EventSource` cannot set custom
headers ([HTML §9.2.2](https://html.spec.whatwg.org/multipage/server-sent-events.html#the-eventsource-interface);
[whatwg/html#2177](https://github.com/whatwg/html/issues/2177)).

### 3.2 Response — success

```
HTTP/1.1 200 OK
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
Connection: keep-alive
Access-Control-Allow-Origin: *
X-Accel-Buffering: no
```

Then an unbounded stream of SSE events (§4), beginning with the backlog
(events with `id > Last-Event-ID`; all events if no `Last-Event-ID`) and
continuing with live events as they are appended. The connection is held open
and flushed after every event.

- `Access-Control-Allow-Origin: *` — cross-origin `EventSource` from the
  `http://wails.localhost` webview origin, no credentials (token is in the query)
  ([Fetch §3.3](https://fetch.spec.whatwg.org/#cors-protocol)). No preflight is
  required (simple CORS GET).
- `X-Accel-Buffering: no` — defensive hint against any proxy buffering, for
  environments that route through one.

A `: keepalive\n\n` comment line ([HTML §9.2.6](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream))
MAY be emitted on an idle timer to keep intermediaries from closing the
connection.

### 3.3 Response — failure

| Condition | Status | Body |
|---|---|---|
| Missing / mismatched / stale `token` | `401 Unauthorized` | plain text — **non**-`text/event-stream`, so `EventSource` treats it as a failed connection and auto-reconnects (FR-009). A stale token after rotation naturally returns 401 until the frontend re-`Open`s. |
| Wrong method | `405 Method Not Allowed` | — |

The server MUST NOT return `204` (that would tell the client to stop
reconnecting, [HTML §9.2.1](https://html.spec.whatwg.org/multipage/server-sent-events.html))
except as an explicit future "drain and stop" signal, which this feature does not use.

---

## 4. Event wire format

Every logical chat event is serialized to the **same JSON shape** the frontend
already consumes (`frameToMap` / `protoToJSONMap` output: an `AgentFrame` with
camelCase fields, flattened oneof payload, base64 `bytes`). The framing follows
[HTML §9.2.6](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream).

### 4.1 Small event (payload ≤ `maxEventBytes` = 48 KiB)

```
id: 7
event: chat
data: {"sessionId":"s","frameId":"f","sender":"FRAME_SENDER_AGENT","content":{"parts":[{"text":{"content":"hi"}}]}}

```
(Terminated by a blank line.) `id` is the log's monotonic event id (FR-003a);
`event: chat` is a fixed event type the frontend listens for.

### 4.2 Large event (payload > 48 KiB) — chunked group

The serialized event JSON is split into `total` fragments, each ≤
`maxEventBytes`. The whole group shares **one** logical event `id`; the SSE `id:`
line is emitted **only on the final fragment** (the earlier fragments carry no
`id:` line). This makes `Last-Event-ID` resume **per logical event**: a drop
before the final fragment leaves the client's `Last-Event-ID` un-advanced for
that event, so reconnect replays the **whole group** — correctness does not
depend on the frontend's partial-group buffer surviving across reconnect.

```
event: chunk
data: {"groupId":"g1","index":0,"total":3,"fragment":"<json-bytes-0..47999>"}

event: chunk
data: {"groupId":"g1","index":1,"total":3,"fragment":"<json-bytes-48000..95999>"}

id: 8
event: chunk
data: {"groupId":"g1","index":2,"total":3,"fragment":"<json-bytes-96000..>"}

```

**Reassembly (frontend)**: buffer `fragment` per `groupId`; once all `total`
pieces arrive (indices `0..total-1`), concatenate fragments in index order to
rebuild the full event JSON, parse it to an `AgentFrame`, and dispatch it exactly
like a small `chat` event. `groupId` is opaque (a UUID); `total` ≥ 2. The
frontend evicts a partial `groupId` buffer on session leave and after a
staleness window, so a never-completing group does not leak.

> Screenshots are the only payload currently large enough to trigger chunking
> (a 5 MiB PNG → ~6.7 MiB base64 → ~145 fragments). Text, reasoning, tool
> operations, and tool-result metadata stay well under the ceiling.

### 4.3 Connection-control lines (optional)

The server MAY emit, at most every ~15 s of idleness:
```
: keepalive

```
Comments are ignored by the parser ([HTML §9.2.6](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream)).

The server MAY emit once near the start of a connection:
```
retry: 2000

```
to set the reconnect delay to 2 s ([HTML §9.2.6 "retry"](https://html.spec.whatwg.org/multipage/server-sent-events.html#interpreting-an-event-stream)).
Default browser delay (a few seconds) is acceptable if omitted.

---

## 5. Reconnect & dedup (FR-003a, FR-009)

1. On a transient drop, `EventSource` auto-reconnects and sends
   `Last-Event-ID: <last id received>` ([HTML §9.2.3](https://html.spec.whatwg.org/multipage/server-sent-events.html#processing-model)).
2. The server replays the backlog with `id > Last-Event-ID`, then resumes live.
   During the disconnect the `recvLoop` kept appending to the same `ChatStream`,
   so events produced in the gap are recovered (FR-009), not lost.
3. The frontend **dedups by `id`**: it tracks every id it has fully applied
   (small events on their own id; chunk groups on all of their fragment ids once
   reassembled) and ignores any id already applied. It also ignores any
   `groupId` already completed, so a partially-replayed group (the client's
   `Last-Event-ID` landed mid-group) does not half-render or duplicate.
4. If the token was rotated (a new `OpenChatStream` happened, e.g. the user left
   and re-entered the session), reconnect gets `401`; the frontend treats that as
   a fresh session and re-`Open`s, resetting its chat view.

Event ids are stable across history-replay, live, and reconnect-replay because
the `ChatStream` is the single owner and is never re-numbered for the active
session (FR-003a). See [../data-model.md §1.1–1.2](../data-model.md).

---

## 6. Loopback / security guarantees (FR-008)

- The listener is created with `net.Listen("tcp", "127.0.0.1:0")` — **loopback
  only**, unreachable from remote hosts.
- Every connection is authenticated by the `?token=` query parameter, compared to
  the `ChatStream`'s current token. Missing/mismatched/stale → `401`.
- The token is a cryptographically-random opaque secret, scoped to one session,
  rotated on each `OpenChatStream`, and never logged.
- The ephemeral port is chosen by the OS (`:0`), avoiding conflicts with other
  local services (FR-010).

---

## 7. What does NOT change (scope boundary, FR-006)

- **`game:log`** (log forwarding) keeps using `runtime.EventsEmit` — out of scope.
- **`ConnectAgent`** WebSocket handshake + probe — unchanged.
- **`SendUserTurn` / `handleInboundOperation` / `executeAgentOperation`** —
  unchanged (the agent↔desktop path and tool execution are untouched).
- **`AgentFrame` / `Message` / `PartBlock` proto** — unchanged.
- The frontend's **`handleAgentFrame`** rendering logic — unchanged; only its
  *source* switches from `runtime.EventsOn('game:frame')` to the `EventSource`
  `chat`/`chunk` handlers (plus id-dedup + reassembly).
