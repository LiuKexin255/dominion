# Data Model: Desktop SSE Chat Message Delivery

**Feature**: 016-desktop-sse-chat-push | **Date**: 2026-06-27

This document defines the entities, identity, and state that the SSE chat push
channel introduces or changes. It builds on [research.md](research.md) (R-001 …
R-005) and is the source of truth for [contracts/chat-stream.md](contracts/chat-stream.md).

The existing protobuf model is **unchanged**: `AgentFrame`, `Message`,
`PartBlock`, `Part`, and the control signals (`WaitSignal`/`WarnSignal`/
`StatusSignal`) in [game.proto](../../../projects/game/game.proto) are reused
verbatim. The new entities below live entirely in the desktop backend and
frontend; no `.proto` change is required.

---

## 1. Entities

### 1.1 `ChatStream` — per-session ordered event log (新增, in-memory)

The single source of chat events for one session, owned by the desktop backend.
There is at most one `ChatStream` per active session.

| Field | Type | Notes |
|---|---|---|
| `sessionID` | `string` | The session this log belongs to. Key into the `ChatStreamRegistry`. |
| `events` | `[]ChatEvent` | Append-only ordered list; indexed by position. |
| `nextID` | `int64` | Monotonic counter for the next event's `id`. Starts at 1. |
| `subscribers` | `[]*subscriber` | Live SSE connections currently draining this log. |
| `token` | `string` | Current session-scoped bearer token (rotated on each `Open`). |
| `mu` | `sync.Mutex` (or RWMutex) | Guards `events`, `nextID`, `subscribers`, `token`. |

**Invariants**:
- `events[i].ID == i+1` for all `i` (the `id` is the 1-based position). This is
  what makes the id **stable** across history-replay, live, and reconnect-replay
  (FR-003a): the log is the single owner and is never re-numbered.
- The log is **append-only** and persists across frontend reconnects for the
  lifetime of the active session; it is dropped only on `CloseChatStream`
  (session leave) or backend shutdown.
- `nextID` increases strictly monotonically; ids are never reused.

**Object vs tuple semantics (style/golang.md §指针)**: `ChatStream` has object
semantics (a distinct per-session instance shared by the registry, the SSE
handler, and `recvLoop`) → referenced by pointer; its container is
`map[string]*ChatStream` per the "container elements use pointers" rule.

### 1.2 `ChatEvent` — one logical chat event in the log (新增)

| Field | Type | Notes |
|---|---|---|
| `ID` | `int64` | Stable, monotonic, 1-based. Becomes the SSE `id:` line. |
| `Frame` | `*game.AgentFrame` | The normalized event payload, always `AgentFrame`-shaped (history `Message`s are converted to a content `AgentFrame` on insertion). Carries one payload: `content` / `wait` / `warn` / `status`. |

**Identity & dedup (FR-003a)**: `ID` is the *only* identity the frontend dedups
on. It is identical whether the event was inserted from history replay or from a
live `recvLoop` frame, because both flow through the same `Append` on the same
`ChatStream`. The frontend tracks the set of rendered `ID`s and ignores any it
has already rendered, so reconnect replay cannot duplicate visible messages.

**Why not reuse `frame_id` / `message_id`**: those are disjoint ID spaces
([game.proto](../../../projects/game/game.proto): `AgentFrame.frame_id` vs
`Message.message_id`), so a live frame and its later persisted form would carry
different ids and duplicate on reconnect. The log-owned monotonic `ID` decouples
transport identity from application identity. See research.md R-003.

### 1.3 `subscriber` — one live SSE connection (新增, internal)

| Field | Type | Notes |
|---|---|---|
| `events` | `chan ChatEvent` | Buffered channel the SSE handler drains; `Append` sends to every subscriber. |
| `done` | `chan struct{}` | Closed when the SSE handler exits (client disconnect / context cancel). |
| `lastEventID` | `int64` | The `Last-Event-ID` the client sent on connect/reconnect (0 if none). |

A `subscriber` is created per HTTP connection, seeded with the backlog
(`events` with `ID > lastEventID`) and then live-appended events, and removed on
disconnect. The `ChatStream` fans out each `Append` to all current subscribers.

### 1.4 `ChatStreamHandoff` — connection handoff view model (新增)

Returned to the frontend by the `OpenChatStream` Wails binding (FR-007).

| Field | Type | Notes |
|---|---|---|
| `Endpoint` | `string` | Full SSE URL, e.g. `http://127.0.0.1:<port>/api/v1/chat/stream`. |
| `Token` | `string` | Session-scoped bearer token; presented as `?token=`. |
| `LastEventID` | `int64` | The current highest `ID` in the log at open time (informational; the frontend starts fresh on a new session entry). |

`json` tags use camelCase to match the existing Wails view-model convention
([view_model.go](../../../projects/game/desktop/view_model.go)): `endpoint`,
`token`, `lastEventId`.

---

## 2. Unified stream: history + live → one `AgentFrame`-shaped sequence

Both inputs are normalized to `*game.AgentFrame` before `Append`, so the frontend
has exactly one consumer (the existing `handleAgentFrame`) and history/live
render identically (FR-004/FR-005).

| Source | Conversion to `AgentFrame` |
|---|---|
| History `*game.Message` (from `client.ListMessages`) | `SessionId`←session, `FrameId`←`message.message_id` (carried for traceability, not used as the SSE id), `Sender`←`message.sender`, `CreateTime`←`message.create_time`, `Payload`←`AgentFrame_Content{ Content: message.content }`. |
| Live frame from `recvLoop` (received `*game.AgentFrame`) | Passed through unchanged. |
| Synthesized error-wait (on `RecvFrame` error) | Same as today: an `AgentFrame` with `Wait{}` payload (settles the turn). |

**Seamless history→live transition (FR-005)**: opening a stream seeds the log
with history (ids `1..H`) once; subsequent live frames append at `H+1…` on the
same log. Because seeding happens at session entry (between turns, before any
`recvLoop` is running for this session) and the log is never re-seeded, history
and live never overlap and there is no switching point the frontend must handle.

---

## 3. Chunking model — large payloads (screenshots)

Per research.md R-002, a single SSE event must stay under a per-event ceiling
(`maxEventBytes = 48 * 1024`) because Chromium buffers each event before
dispatch and silently drops oversized ones. Screenshots up to 5 MiB raw PNG
(~6.7 MiB base64) MUST therefore be fragmented.

A logical `ChatEvent` whose serialized JSON exceeds `maxEventBytes` is delivered
as a **chunk group** on the wire (see [contracts/chat-stream.md](contracts/chat-stream.md)
for the exact wire format). Conceptually:

| Piece | Carries | SSE `id:` |
|---|---|---|
| `chunk` × N | A fragment of the serialized JSON (`fragment`), plus `groupId`, `index`, `total` | Each piece gets its **own** monotonic `ID` (so `Last-Event-ID` resume is per-piece). |
| (reassembled) | The full `AgentFrame` JSON, reconstructed by concatenating `fragment` in `index` order, then parsed. | — |

**Frontend reassembly & dedup**:
- The frontend buffers `fragment`s per `groupId`. When all `total` pieces arrive,
  it concatenates, parses the `AgentFrame`, and dispatches it to `handleAgentFrame`
  exactly like a small event.
- It records every `id` it has *fully applied* (small events on their own id;
  chunk groups on all of their piece ids once reassembled). On reconnect replay
  it ignores any id already applied, and ignores any `groupId` already completed,
  so a partially-replayed group (client's `Last-Event-ID` landed mid-group) does
  not duplicate or half-render.

**Why a generic JSON-fragment scheme (not screenshot-specific)**: it is
transport-level, applies uniformly to any future large content, and keeps the
application event shape (`AgentFrame`) unchanged. Screenshots are simply the
first — and currently only — payload large enough to trigger it.

---

## 4. State transitions

### 4.1 `ChatStream` lifecycle (per active session)

```
                 OpenChatStream(sessionID)            CloseChatStream(sessionID)
   <absent> ──────────────────────────────▶ <open> ───────────────────────────▶ <absent>
        ▲        │                                                       │
        │        │ (first open: seed history via ListMessages)            │
        │        ▼                                                       │
        │   Append(frame) ◀── recvLoop (live, per turn)                   │
        │        │                                                       │
        │        ▼                                                       │
        └── fan-out to subscribers (live SSE connections) ────────────────┘
```

- **`<absent>` → `<open>`**: `OpenChatStream` creates the `ChatStream`, seeds
  history if newly created, rotates the token, registers a subscriber-context,
  and returns the handoff. Idempotent for reconnect within the same session
  (reuses the existing log + rotates token; does **not** re-seed).
- **`<open>` (steady)**: `recvLoop` `Append`s live frames; subscribers drain.
- **`<open>` → `<absent>`**: `CloseChatStream` (session leave / `handleBackToSessions`)
  closes subscribers and drops the log. A subsequent session entry starts a fresh
  log with ids from 1, and the frontend resets its chat view (existing
  `handleSelectSession` clears `chatMessages`).

### 4.2 `recvLoop` lifecycle (unchanged shape, new destination)

`recvLoop` keeps its current per-turn lifecycle — launched by `SendUserTurn`,
exits on a `wait` signal or `RecvFrame` error, joined by `CloseAgent`
([app.go](../../../projects/game/desktop/app.go)). The **only** change is the
destination of each frame: `runtime.EventsEmit("game:frame", …)` →
`chatStream.Append(frame)`. The tool-execution flow (`handleInboundOperation` →
`executeAgentOperation` → `ws.SendFrame`) is untouched.

### 4.3 SSE connection lifecycle (frontend `EventSource`)

```
 connecting ──▶ open ──▶ (drop) ──▶ connecting ──▶ open …   (auto-reconnect, Last-Event-ID)
                 │                                    │
                 └── onmessage: chunk-reassemble → dedup by id → handleAgentFrame
```

`EventSource` manages `connecting↔open` itself per HTML §9.2.3
([research.md R-002](research.md)). The frontend closes the `EventSource`
(`readyState = CLOSED`) when leaving the session; the desktop tears down the
matching subscriber.

---

## 5. Validation rules (from requirements)

| Rule | Source | Enforcement |
|---|---|---|
| Bind loopback only; reject non-frontend connections. | FR-008 | `net.Listen("tcp", "127.0.0.1:0")`; token check in the SSE handler rejects missing/mismatched tokens with HTTP 401. |
| Token scoped to one session; rotated on new stream; stale rejected. | FR-007, FR-008, clarification 2026-06-27 | `OpenChatStream` rotates `ChatStream.token`; handler compares the `?token=` to the current token. |
| Endpoint avoids conflicts. | FR-010 | Ephemeral port via `:0`. |
| No message lost/truncated/reordered. | FR-003 | Append-only log + monotonic ids + fan-out in insertion order + chunk reassembly with index ordering. |
| Reconnect cannot duplicate. | FR-003a | Stable ids + frontend dedup set + `Last-Event-ID` resume replaying `id > last`. |
| Only chat migrates; logs unaffected. | FR-006 | `game:log` stays on `runtime.EventsEmit`; SSE handler serves only `/api/v1/chat/stream`. |

---

## 6. Relationship to existing model (no proto change)

```
game.proto (UNCHANGED)
├── AgentFrame  ── (live) ────────────────┐
│   └── payload: content | wait | warn | status
├── Message     ── (history) ─────────────┤── both normalized to AgentFrame ──▶ ChatStream.Append
│   └── content: PartBlock                │                                       │
└── PartBlock / Part (shared content) ────┘                                       ▼
                                                                          SSE event (id, AgentFrame-JSON)
                                                                                  │ (chunk if > 48 KiB)
                                                                                  ▼
                                                                       frontend handleAgentFrame (UNCHANGED consumer)
```

`frameToMap` ([app.go](../../../projects/game/desktop/app.go)) and
`protoToJSONMap`/`ToMessageViewModels` ([view_model.go](../../../projects/game/desktop/view_model.go))
already produce the identical camelCase, oneof-flattened, base64-bytes JSON shape
the frontend expects; the SSE serialization reuses this exact projection so no
frontend rendering logic changes.
