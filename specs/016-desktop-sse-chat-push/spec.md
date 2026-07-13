# Feature Specification: Desktop SSE Chat Message Delivery

**Feature Branch**: `016-desktop-sse-chat-push`

**Created**: 2026-06-27

**Status**: Draft

**Input**: User description: "按上面讨论的方案，对 desktop 对话框消息推送进行重构：1. 使用 sse 代替 ws，反正是做单向推送；2. 仅使用 sse 代替对话框消息更新，其他消息推送暂时不变；3. 历史消息和实时消息都走 sse 推送，保持一致性，避免出现同一个页面两种数据推送方式。"

**Relationship**: Fixes the real-time dialog-update regression introduced by feature [015 — Desktop Agent Interaction Refinement](../015-desktop-agent-refinement/spec.md) US1. Feature 015 made `SendUserTurn` non-blocking and delivered agent frames to the frontend via the framework's host→webview event channel; that channel silently drops messages (a known framework defect on the target platform) once the desktop window loses foreground — which happens routinely when the agent's click operation brings the target window forward. This feature reroutes chat message delivery onto a channel that does not depend on host→webview script injection.

## Motivation

The desktop conversation dialog stops updating partway through a continuous agent run: the first tool operation and its result render, but from the second tool onward nothing updates, and the "agent is typing" indicator persists until the user leaves and re-entries the session. Investigation traced this to a known framework defect — the desktop UI framework delivers backend→frontend events by injecting JavaScript into the webview, and those injections are silently dropped (no error, no callback) once the webview process is throttled or the host window is backgrounded ([wailsapp/wails#4418](https://github.com/wailsapp/wails/issues/4418), [#2861](https://github.com/wailsapp/wails/issues/2861)). The trigger is the agent's click executor bringing the target window to the foreground, which backgrounds the desktop window. The chosen fix is to deliver chat messages over a renderer-initiated, one-way push channel ([Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html)) that rides the webview's native networking instead of host-injected scripts, making delivery robust to focus changes.

## Clarifications

### Session 2026-06-27

- Q: How should reconnect/history replay prevent duplicate chat messages? → A: Every chat stream event has a stable unique ID; the frontend ignores already-rendered IDs after reconnect/history replay.
- Q: What lifetime should the local push endpoint authorization token have? → A: Token is scoped to one session and rotated when a new stream is opened; stale tokens are rejected.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Live Chat Updates Survive Window Focus Changes (Priority: P1)

During a continuous agent run that performs multiple tool operations — including clicks that bring the target window to the foreground and background the desktop window — the conversation dialog renders every event (streaming text, reasoning, tool operations, tool results, screenshots) as it is produced, in real time. The dialog never freezes mid-run, and the user never has to leave and re-enter the session to see what the agent did.

**Why this priority**: This is the defect being fixed. Today the dialog freezes at the second tool operation whenever the agent clicks (because the click steals foreground from the desktop window), making the agent impossible to monitor during the exact operations where monitoring matters most. It is the sole reason for this feature; every other story is consistency hardening around it.

**Independent Test**: Start an agent run that performs at least two tool operations where at least one is a click on an external target window (causing the desktop window to lose foreground). Observe the dialog throughout the run: streaming text, tool operations, tool results, and screenshots must all continue to appear in real time after the focus change, and the "agent is typing" indicator must clear when the run completes — without leaving and re-entering the session.

**Acceptance Scenarios**:

1. **Given** an agent run is in progress and has performed its first click that brought an external window to the foreground, **When** the agent continues producing text, tool operations, and tool results, **Then** every subsequent event renders in the dialog within perceptual real-time (under 1 second), not deferred or lost.
2. **Given** the desktop window is not in the foreground for the entire remainder of an agent run, **When** the run completes, **Then** the dialog shows the complete, ordered conversation (all text, all tool operations, all results, all screenshots) and the "agent is typing" indicator clears — with no requirement to refocus, leave, or re-enter the session.
3. **Given** an agent run produces a large result (e.g., a screenshot), **When** it is delivered, **Then** it arrives complete and in order over the new channel, with no silent truncation or loss.
4. **Given** an agent run that previously froze the dialog at the second tool operation, **When** the same run is repeated after this feature, **Then** the dialog updates continuously through all tool operations to completion.

---

### User Story 2 - Single Consistent Delivery Channel for History and Live (Priority: P2)

When the user enters a session, the historical conversation loads through the same delivery channel that subsequently carries live updates, so there is exactly one data path feeding the dialog. The historical messages render identically to how live messages render, and the transition from history replay to live streaming is seamless to the user.

**Why this priority**: The user explicitly requires that the same page not use two different data-push methods (one for history, one for live). A single channel removes divergence bugs (history rendering one way, live another) and simplifies the frontend to one consumer. It is secondary to US1 because live-update reliability is the blocking defect, but it is part of the same refactor and must land with it to avoid shipping two parallel mechanisms.

**Independent Test**: Enter a session that has existing history, then send a new message that triggers an agent run. Verify the history messages and the live messages both arrive through the same channel and render identically; verify there is no separate fetch mechanism used for history versus live.

**Acceptance Scenarios**:

1. **Given** a session with prior conversation history, **When** the user enters the session, **Then** the historical messages are delivered through the live push channel (history replayed first), not through a separate one-shot fetch mechanism.
2. **Given** history has finished replaying on the channel, **When** the agent begins producing new live content, **Then** the live content follows the history on the same channel with no switching, gap, or duplicate rendered messages.
3. **Given** a historical message and an equivalent live message of the same type (text, tool operation, tool result, screenshot), **When** both are rendered, **Then** they are indistinguishable in layout and content — because they traveled the same path.

---

### User Story 3 - Resilient Connection Without Session Re-entry (Priority: P3)

If the push connection drops transiently during a session (e.g., the backend restarts the channel, or the connection is briefly interrupted), the frontend automatically re-establishes the connection and resumes receiving messages. The user does not need to leave and re-enter the session to recover the live view.

**Why this priority**: A push channel that silently dies re-introduces the exact symptom this feature is meant to eliminate (frozen dialog requiring re-entry). Auto-reconnect makes the fix durable rather than swapping one fragile path for another. It is the lowest priority because, unlike US1, a connection drop is not the currently observed failure mode — but hardening the new channel against it prevents regression.

**Independent Test**: While viewing a session, force the push connection to drop (e.g., restart the backend channel or disconnect the client). Verify the frontend reconnects automatically and continues receiving live messages for subsequent agent runs without leaving the session.

**Acceptance Scenarios**:

1. **Given** the push connection drops while a session is open, **When** the connection becomes available again, **Then** the frontend reconnects automatically and resumes receiving messages, without the user leaving and re-entering the session.
2. **Given** the push connection drops and reconnects during an agent run, **When** reconnection completes and the channel replays history, **Then** the dialog reflects the current conversation state without duplicate rendered messages, because already-rendered event IDs are ignored while missed event IDs are applied.

---

### Edge Cases

- What happens when the push connection drops in the middle of an agent run? The frontend reconnects; on reconnect, the channel replays the session's message history (which includes everything persisted so far), so any events produced during the gap are recovered rather than lost. Each stream event has a stable unique ID, so replayed events already rendered before the drop are ignored rather than duplicated. Live streaming then resumes.
- What happens when the user switches between sessions? The push stream is scoped to the active session; switching sessions opens a stream for the new session and closes the old one, so messages from the previous session do not bleed into the new view.
- What happens when a delivered message is very large (e.g., a high-resolution screenshot)? The channel must deliver it complete and in order; very large frames must not cause subsequent smaller frames to be dropped or reordered.
- What happens to the local push endpoint's security? The endpoint must bind to loopback only and must not be reachable from remote hosts; a connection from any process other than the desktop frontend must be rejected. The authorization token is scoped to one session and rotated when a new stream is opened, so stale tokens are rejected.
- What happens when the frontend connects before any history exists? The channel delivers an empty history replay (or a sentinel) and then waits for live content, without erroring.
- What happens to other frontend notifications (e.g., log forwarding)? They must remain on their existing delivery mechanism and must not be affected by this change; only chat dialog messages migrate.

## Requirements *(mandatory)*

### Functional Requirements

**Chat Message Delivery Channel**

- **FR-001**: Chat dialog messages — streaming text, reasoning, tool operations, tool results, screenshots, status, and the turn-complete signal — MUST be delivered to the frontend over a one-way, renderer-initiated push channel ([Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html)) that travels the webview's native networking, NOT over the framework's host→webview script-injection event channel.
- **FR-002**: Message delivery over the new channel MUST continue to reach the frontend regardless of whether the desktop window is in the foreground, backgrounded, or has lost focus to another window — the silent-drop failure mode of the previous channel MUST be eliminated.
- **FR-003**: Messages delivered over the new channel MUST preserve their original ordering and full content (text chunks appended in order, tool results and screenshots complete); no message may be silently lost, truncated, or reordered.
- **FR-003a**: Every chat stream event MUST carry a stable unique event ID that remains identical when the same event appears in history replay, reconnect replay, or live streaming; the frontend MUST ignore event IDs it has already rendered so reconnect replay cannot duplicate visible messages.

**Unified History and Live Path**

- **FR-004**: Both historical messages (replayed when entering a session) and real-time messages (produced during an agent run) MUST be delivered through the same push channel, so the dialog consumes exactly one data path. The separate one-shot history fetch used previously for the chat view MUST be superseded by history replay on this channel.
- **FR-005**: When the channel is opened for a session, it MUST replay the session's existing message history first, then continue with live messages as they are produced, so the transition from history to live is seamless and requires no client-side switching between mechanisms.

**Scope Boundary**

- **FR-006**: ONLY chat dialog message delivery is migrated to the new channel. All other frontend notifications — including log forwarding and any non-chat events — MUST remain on their existing delivery mechanism and MUST NOT be changed or broken by this feature.

**Lifecycle and Resilience**

- **FR-007**: The frontend MUST obtain the connection details for the local push channel (e.g., endpoint path and authorization token) from the backend via a single request when opening a session stream, not once per message.
- **FR-008**: The push channel MUST be scoped to the local machine: it MUST bind to loopback only and MUST reject any connection that is not from the desktop frontend (e.g., via the token from FR-007); it MUST NOT be reachable from remote hosts. The token MUST be scoped to one session and rotated when a new stream is opened; stale tokens MUST be rejected.
- **FR-009**: If the push connection drops while a session is open, the frontend MUST automatically re-establish the connection and resume receiving messages, without requiring the user to leave and re-enter the session. On reconnect, any messages produced during the gap MUST be recovered (via history replay) so no content is permanently lost.
- **FR-010**: The push channel's server lifecycle MUST be bound to the desktop backend lifecycle — it starts when the backend starts and stops cleanly when the backend stops — and MUST select a local endpoint that avoids conflicts with other local services.

### Key Entities *(include if feature involves data)*

- **Local Chat Push Channel**: A one-way, loopback-only push stream ([Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html)) owned by the desktop backend, scoped to a single session, that replays the session's message history and then streams live chat messages to the frontend. It replaces the framework's host→webview event channel as the delivery path for chat dialog content.
- **Chat Message Stream (unified)**: The single, ordered sequence of chat dialog events (text, reasoning, tool operations, tool results, screenshots, status, turn-complete) that flows over the Local Chat Push Channel — serving both history replay and live streaming, so the frontend has one consumer instead of two. Each event has a stable unique ID used to deduplicate replayed events after reconnect.
- **Channel Connection Handoff**: The exchange when opening a session stream through which the frontend receives the local channel's connection details (endpoint and session-scoped authorization token) from the backend, after which the frontend owns the connection and all chat content flows over it. A new stream opening rotates the token and invalidates stale tokens.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: During an agent run that performs multiple tool operations including clicks on external windows (causing the desktop window to lose foreground), 100% of produced events (text, tool operations, tool results, screenshots) render in the dialog in real time (under 1 second each) through to run completion — with zero silent losses and no requirement to re-enter the session.
- **SC-002**: After this feature, the specific failure mode "dialog freezes at the second tool operation after a click steals foreground" is eliminated in 100% of test cases.
- **SC-003**: Entering a session with existing history and then running a live agent turn shows history and live content arriving over a single delivery path, with history and equivalent live messages rendering identically in 100% of test cases.
- **SC-004**: After a forced mid-session connection drop, the frontend reconnects automatically and resumes receiving messages with no permanent content loss, no duplicate rendered messages, and no session re-entry, in 100% of test cases.
- **SC-005**: Non-chat frontend notifications (e.g., log forwarding) remain functional and unchanged — zero regressions — confirming the scope boundary.

## Assumptions

- This feature fixes a regression introduced by feature 015 US1, where `SendUserTurn` was made non-blocking and agent frames were delivered via the framework's host→webview event channel. See [feature 015 spec](../015-desktop-agent-refinement/spec.md). The non-blocking `recvLoop` design is retained; only the final frontend-delivery hop changes.
- The user explicitly chose Server-Sent Events over WebSocket because chat delivery is strictly one-way (backend → frontend); SSE is lighter and sufficient. This decision is accepted scope, not a plan-phase choice.
- The previously separate one-shot history fetch used by the chat view (loading stored messages when entering a session) is superseded by history replay on the new channel. Whether the underlying storage/retrieval function is retained for other consumers is a plan-phase decision; for the chat view, only the new channel is used.
- The local channel binds to loopback and uses a session-scoped token rotated per stream opening; detailed threat modeling (e.g., whether other local user processes are considered a trust boundary) is a plan-phase concern, but the channel MUST NOT be remotely reachable.
- The new channel's transport specifics (port selection strategy, event framing, reconnect backoff, history-replay semantics on reconnect) are plan-phase implementation concerns constrained by the functional requirements above.
- Only the chat dialog message path migrates; log forwarding and any other existing frontend events remain on the framework's event channel and are out of scope.
- The agent-side and desktop↔service WebSocket paths are unchanged; the new channel is purely the local desktop-backend → desktop-frontend hop.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [HTML Living Standard — 9.2 Server-sent events (EventSource)](https://html.spec.whatwg.org/multipage/server-sent-events.html) — the one-way push mechanism chosen for chat delivery; renderer-initiated, rides native networking rather than host script injection.

### Repositories

- [wailsapp/wails#4418 — WebView2 Bridge Loses JavaScript Promises Under Heavy Load / Silently dropped ExecuteScript](https://github.com/wailsapp/wails/issues/4418) — the framework defect motivating this feature: host→webview event delivery via `ExecuteScript` silently drops calls with no error or callback. Cited as the root cause of the live-update freeze.
- [wailsapp/wails#2861 — App window not showing (Efficiency Mode): bindings break when the app is backgrounded](https://github.com/wailsapp/wails/issues/2861) — the focus-loss trigger: when the desktop window loses foreground (e.g., the agent's click brings the target window forward), the webview is throttled and host-injected events stop being delivered.

### Articles & RFCs

- No additional articles or RFCs referenced. The mechanism choice (SSE) and the defect evidence (the two issues above) are the only external material; all further dependency and API research occurs in `plan.md` per Constitution §III.
