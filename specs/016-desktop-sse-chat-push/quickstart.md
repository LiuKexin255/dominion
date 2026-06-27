# Quickstart: Desktop SSE Chat Message Delivery

**Feature**: 016-desktop-sse-chat-push | **Date**: 2026-06-27

Runnable validation scenarios that prove the feature works end-to-end. They map
1:1 to the spec's User Stories and Success Criteria. This is a *validation/run
guide* — implementation lives in `tasks.md`. Refer to
[contracts/chat-stream.md](contracts/chat-stream.md) and
[data-model.md](data-model.md) for the contract details, not duplicated here.

> **Testing boundary**: the loopback SSE server is part of the desktop process,
> not a deployed service, so it is **not** covered by the cluster large-test
> testplan (`projects/game/testplan`, which targets gateway→session→proxy→agent).
> The SSE channel is validated by Go unit tests (`httptest`) for the server +
> log + chunking/dedup, vitest unit tests for the frontend reassembly/dedup
> logic, and the manual runnable scenarios below for the end-to-end
> focus-loss/reconnect behavior (the desktop UI cannot be driven by the cluster
> testplan). This mirrors how feature 015's desktop-frontend changes were
> validated (see `projects/game/testplan/README.md` §7).

---

## Prerequisites

- Windows host (the defect's platform; the agent's click steals foreground from
  the desktop window). Linux/macOS builds run but cannot reproduce US1's
  foreground-loss trigger.
- A bound target window that the agent can click (any application window).
- The desktop built via Bazel:
  ```bash
  bazel build //projects/game/desktop/frontend:dist
  bazel build //projects/game/desktop
  ```
- A gateway + agent reachable from the desktop (the dev/lt environment), and an
  agent profile configured.

---

## Build & unit-test gates (run before the manual scenarios)

```bash
# Go: SSE server, per-session log, token rotation, chunking, Last-Event-ID resume
bazel test //projects/game/desktop/internal/chatstream:chatstream_test

# Go: recvLoop now appends to the chatstream log (refactor regression guard)
bazel test //projects/game/desktop:desktop_test

# Frontend: build + Svelte typecheck (no frontend unit-test target exists in this
# repo today; chunk-reassembly/dedup protocol correctness is covered by the Go
# round-trip test in chatstream_test, and the wiring is validated by the manual
# scenarios below)
bazel build //projects/game/desktop/frontend:dist
```

**Expected**: all green. The Go tests assert, at minimum: loopback-only listen;
`401` on missing/stale token; history-seed ordering; monotonic ids; reconnect
replays `id > Last-Event-ID`; a >48 KiB event is fragmented and reassembles to
the original payload; `recvLoop` append path produces the same frame set it did
via `EventsEmit`.

---

## Scenario A — Live updates survive window focus changes (US1, SC-001/SC-002)

*The defect being fixed: the dialog froze at the second tool op after a click
stole foreground.*

1. Start the desktop, select a session, bind a target window.
2. Send a turn that makes the agent perform **≥2 tool operations, at least one
   being a click on the external target window** (so the desktop window loses
   foreground).
3. Keep the desktop window backgrounded for the rest of the run.

**Expected**: streaming text, each tool operation, each tool result, and every
screenshot continue to render in the dialog in real time (< 1 s each) through to
run completion; the "agent is typing" indicator clears on completion. **No
re-entry into the session is required.** Repeating the previously-failing run no
longer freezes at the second tool op.

**Pass criterion**: SC-001 (100% of events render, zero silent losses) and
SC-002 (the freeze failure mode is eliminated).

---

## Scenario B — Single delivery path for history + live (US2, SC-003)

1. Select a session that already has conversation history.
2. Open it; observe history load, then send a new message triggering an agent run.

**Expected**: history messages and the new live messages render **identically**
(bubbles, operation/tool-result layout, screenshots). Confirm via devtools that
there is **no** separate one-shot history fetch — history arrived as early events
on the same SSE stream, and live events followed on the same connection (one
`EventSource`, no `ListMessages` XHR).

**Pass criterion**: SC-003 (history and equivalent live messages render
identically over one delivery path, 100%).

---

## Scenario C — Resilient reconnect without re-entry (US3, SC-004)

1. With a session open, simulate a push-connection drop: from devtools, abort the
   `EventSource` fetch (or restart the desktop backend's SSE listener). Do **not**
   touch the session.
2. Observe the frontend reconnect automatically.
3. Send another turn (or let an in-flight one continue).

**Expected**: the frontend reconnects on its own, resumes receiving messages,
and the dialog reflects the current conversation with **no duplicate** rendered
messages and **no permanent content loss**. No leaving/re-entering the session.

**Pass criterion**: SC-004 (auto-reconnect, no loss, no duplicates, no re-entry,
100%).

---

## Scenario D — Large screenshot integrity (US1-AS3, FR-003)

1. Trigger an agent run that produces a high-resolution screenshot (tool result
   with image). Repeat with progressively larger captures up to the 5 MiB cap.

**Expected**: every screenshot arrives **complete and in order**, rendered
correctly in the tool-result bubble and the zoom modal, with no silent
truncation or loss, and subsequent smaller frames are not dropped/reordered.
(Implementation note: these are delivered as chunked SSE fragments per
[contracts/chat-stream.md §4.2](contracts/chat-stream.md); devtools shows many
small `chunk` events rather than one oversized event.)

**Pass criterion**: FR-003 acceptance (large result complete and in order).

---

## Scenario E — Scope boundary: logs unaffected (FR-006, SC-005)

1. During any run above, confirm the Log Panel still populates in real time.

**Expected**: log forwarding (`game:log`) continues to work unchanged — zero
regressions — confirming only chat dialog delivery migrated. (Devtools: log
entries still arrive via the `game:log` Wails event, not over SSE.)

**Pass criterion**: SC-005 (non-chat notifications unchanged).

---

## Inspecting the stream (devtools, all scenarios)

- **Network tab**: one persistent `GET …/api/v1/chat/stream?session=<id>&token=…`
  request of type `eventsource`, staying `pending` (open). On reconnect it
  reopens and carries a `Last-Event-ID` request header.
- **Event stream**: `chat` events for small frames; `chunk` events (sharing a
  `groupId`) for screenshots. `id:` lines are monotonic and never repeat for the
  active session.
- **Response headers**: `content-type: text/event-stream`, `access-control-allow-origin: *`,
  `cache-control: no-cache`.

If a `data:` field ever exceeds ~1 MiB in a single event, that is a chunking bug
(the screenshot should have been fragmented) and would reproduce the original
silent-drop symptom — fail the run.
