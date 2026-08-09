# Feature Specification: Real-Time Init Instruction Delivery

**Feature Branch**: `041-realtime-init-push`

**Created**: 2026-08-09

**Status**: Draft

**Input**: User description: "修复首次进入 session 时 init instruction 不可见 + typing indicator 卡住的问题；应通过实时通道推送消息，而非依赖 ListMessages 拉取"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Init Instruction Visible on First Entry (Priority: P1)

When a user first enters a new game session and the team is materialized, the planner's initial calibration instruction should appear in real-time in both the planner tab (the planner's instruction message) and the player tab (the instruction delivered to the player). The user should NOT need to exit and re-enter the session to see the instruction.

**Why this priority**: The init instruction is the planner's opening guidance for the player. If it is not visible on first entry, the user has no context for the session and must re-enter to see it — a broken first-use experience. This is the primary bug reported in production (session `6c4a7195e8690c4b647440fcf2f0298c`).

**Independent Test**: Create a new session, select a TeamProfile to materialize the team, and connect. Verify both the planner tab and player tab show the init instruction within the planner model's response time — without leaving and re-entering.

**Acceptance Scenarios**:

1. **Given** a newly created session with no team materialized, **When** the user selects a TeamProfile and enters the session, **Then** the planner's init instruction appears in the planner tab AND the instruction appears in the player tab — without leaving and re-entering the session.
2. **Given** a session where the init instruction was already produced (re-entry), **When** the user enters the session, **Then** the instruction is immediately visible from history (no delay, no duplicate rendering).
3. **Given** a session where the init turn is still in progress when the user connects, **When** the init turn completes, **Then** the instruction appears in real-time as new messages delivered through the connection (not via a history re-fetch).

---

### User Story 2 - Typing Indicator Reflects Actual State (Priority: P2)

When a user enters a new session, the "Agent is typing" indicator should NOT be stuck on. The indicator should be driven only by actual user turns, not by the background init instruction turn.

**Why this priority**: A stuck "typing" indicator is confusing — it implies the agent is working on something, but nothing ever arrives. This undermines user trust and makes the session appear broken.

**Independent Test**: Enter a new session. Verify the typing indicator is OFF on entry (no user turn running — the background init turn does not drive it). Send a message. Verify the typing indicator turns ON. When the turn completes, verify it turns OFF.

**Acceptance Scenarios**:

1. **Given** a newly materialized session with the init turn running in the background, **When** the user connects and the status probe responds, **Then** the typing indicator is OFF — the init turn does not drive the typing indicator.
2. **Given** a connected session where the user sends a message, **When** the agent processes the turn, **Then** the typing indicator is ON and turns OFF when the turn completes (terminal signal received).
3. **Given** a session where the user triggers a destructive operation (refresh team, profile change) while the init turn is in flight, **Then** the operation is rejected (the init still gates destructive operations, distinct from the status probe).

---

### User Story 3 - Continuous Real-Time Channel (Priority: P3)

The agent-to-desktop connection should continuously deliver messages in real-time, not only during user turns. Background agent activity (init instruction, and future background tasks) should reach the desktop through the real-time channel without requiring the user to interact first.

**Why this priority**: The init instruction is the first instance of a broader pattern — any background agent task that produces messages needs a real-time delivery path. The current architecture only reads from the connection during user turns, leaving background-produced messages stranded until a manual refresh or re-entry. Establishing a continuous delivery path once benefits all current and future background tasks.

**Independent Test**: Connect to a session. Without sending any user message, verify that agent-produced frames (init instruction) arrive in real-time through the connection.

**Acceptance Scenarios**:

1. **Given** a connected session where no user turn is in flight, **When** the agent produces a background message (e.g., init instruction), **Then** the message is delivered to the desktop in real-time through the existing connection.
2. **Given** a connected session with the continuous reader active, **When** the user sends a message, **Then** the user turn's response frames (text, tool calls, operations, terminal signal) are delivered through the same continuous reader — no separate per-turn read mechanism is needed.
3. **Given** a connected session, **When** the connection is closed (session leave, disconnect), **Then** the continuous reader terminates cleanly and resources are released.

---

### Edge Cases

- What happens when the init turn fails (planner model unavailable)? The instruction is skipped (degrade); no message is pushed; the session remains usable for user turns. The init promise resolves (errors are swallowed internally).
- What happens when the user disconnects during the init turn and reconnects? On reconnect, if the init already completed, the instruction is visible from the history seed; if still running, it is pushed on completion through the new connection.
- What happens when the init completes before the user connects? The instruction is already in the checkpoint; the history seed (loaded on connect) delivers it immediately — no real-time push needed.
- What happens when multiple background tasks produce messages concurrently? Each task's messages are delivered independently through the real-time channel, tagged with the producing agent for correct tab routing.
- What happens when the init instruction was already loaded from history (seed) and the real-time push also delivers it? The frontend deduplicates by message/frame identifier — no duplicate rendering.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST deliver the init instruction to the desktop in real-time when the init turn completes after the user has connected — through the existing agent-desktop connection, not solely via the history-listing API.
- **FR-002**: The desktop backend MUST continuously read from the agent-desktop connection after the user connects, not only during user turns, so that background-produced messages reach the desktop without user interaction.
- **FR-003**: The connection status probe MUST report IDLE (not ACTIVE) when only the background init turn is in flight — the init turn MUST NOT drive the typing indicator.
- **FR-004**: The system MUST NOT deliver duplicate messages: if the init instruction was already loaded from history (seed or history listing), the real-time push MUST be deduplicated by the frontend via message/frame identifier matching.
- **FR-005**: The system MUST retain the init turn's fire-and-forget materialization semantics for `UpdateTeam` — the RPC returns immediately after team materialization; the init turn's real-time delivery is a separate concern handled through the connection after the desktop connects.
- **FR-006**: The real-time push MUST deliver both the planner's instruction message (to the planner tab) and the player's instruction write-back (to the player tab), each tagged with the producing agent name for correct tab routing.
- **FR-007**: Destructive operations (RefreshTeam, profile-change rebuild) MUST be rejected with FAILED_PRECONDITION while the init turn is in flight — the init still gates these operations through a separate "busy" signal, distinct from the status probe's "running" signal.
- **FR-008**: The continuous connection reader MUST handle the terminal turn signal (wait) by updating the desktop's processing state WITHOUT terminating the reader — the reader continues processing subsequent frames.
- **FR-009**: The continuous connection reader MUST handle operation requests (desktop automation) identically to the current per-turn reader — operations are executed and results returned through the connection.
- **FR-010**: On connection close (session leave, disconnect, error), the continuous reader MUST terminate cleanly, and any pending background push callback MUST be cleared to avoid writing to a dead connection.
- **FR-011**: The continuous reader MUST be the sole reader on the connection — no concurrent readers, as the connection protocol allows only one concurrent read operation.
- **FR-012**: User turn submission (SendUserTurn) MUST NOT start a separate reader when the continuous reader is already active — the continuous reader handles all inbound frames including user turn responses.

### Key Entities *(include if feature involves data)*

- **Init Instruction Turn**: A background agent turn that produces the planner's initial calibration instruction (planner message + player instruction write-back) right after team materialization. Runs once per session lifecycle; never re-triggered on re-entry or profile rebuild.
- **Connection Status Probe**: A one-shot probe sent on connect that reports the session's working state (IDLE/ACTIVE). MUST exclude background init activity from the ACTIVE signal. The typing indicator is driven by this probe's response and by turn lifecycle signals (start/terminal).
- **Continuous Connection Reader**: A persistent frame reader on the agent-desktop connection that processes all inbound frames (display messages, operations, control signals) for the entire connection lifetime, replacing the previous per-turn read model.
- **Running vs Busy Signals**: Two distinct signals — "running" (a real user turn is in flight; drives the status probe and typing indicator) and "busy" (running OR init turn in flight; gates destructive operations). The init turn sets "busy" but NOT "running".

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On first entry to a new session, users see the init instruction in both the planner and player tabs without needing to leave and re-enter — verified by the instruction being visible within 10 seconds of team materialization (the planner model's typical response time).
- **SC-002**: The typing indicator is OFF when entering a new session with only the background init in flight — verified by the status probe returning IDLE on connect.
- **SC-003**: Background-produced messages (init instruction) are delivered through the real-time connection without any user interaction — verified by the message appearing after connect with no message sent and no manual refresh.
- **SC-004**: The continuous reader correctly processes user turn responses (including automation operations and terminal signals) identically to the previous per-turn model — no regression in turn-level behavior (all existing turn-level tests pass).
- **SC-005**: The "busy" signal (init gating) correctly rejects destructive operations during the init turn — verified by RefreshTeam and profile-change rebuild returning FAILED_PRECONDITION while init is in flight.

## Assumptions

- The gRPC keepalive fix (restored keepalive options in the prompt service client) is already deployed, ensuring the init turn's planner model call completes in a timely manner (seconds, not minutes).
- The `isRunning`/`isBusy` split is already implemented and committed on the 040 branch (`040-team-singleton-conformance`, HEAD `868c49b`) — this spec formalizes the requirement (FR-003/FR-007).
- The existing channel-frame emission mechanism (used by the compress/review planner nodes to push real-time frames during user turns) is the established pattern for real-time frame delivery.
- The desktop backend's one-shot seed model (history loaded once on stream open, no polling) is retained — the real-time push supplements, not replaces, the seed for history delivery.
- The desktop frontend's message-identifier dedup mechanism (`renderedMessageIds`) correctly prevents duplicate rendering of pushed frames that were already loaded from history.
- The init turn's degrade semantics (planner model failure → skip instruction, log, resolve promise) are retained — real-time push is best-effort and never blocks the session.
