# Data Model: Real-Time Init Instruction Delivery

**Feature**: `041-realtime-init-push` | **Date**: 2026-08-09 | **Spec**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

This feature adds **no new persisted entities and no proto schema changes** (research.md D8). It introduces two **runtime-only** constructs on the agent (`SessionTeam` stream sink) and desktop (continuous reader) sides, and formalizes the **frame model** the init turn emits plus the **dedup rules** that keep real-time and history consistent. Persisted data (the init instruction in `plannerMessages`/`playerMessages`) is unchanged from spec 039.

Reference proto: `projects/game/game.proto`.

---

## 1. Entities (runtime constructs)

### 1.1 Stream Display Sink (agent, runtime — NEW)

A single bound write target on a `SessionTeam` representing the **current live `Connect` stream** for that session. Lifecycle is tied to the stream, independent of the TurnLoop.

| Field | Type | Source / note |
|---|---|---|
| `sink` | `(frame: TeamFrame) => void` | Closure capturing the stream's `safeWrite(stream, frame, sessionId)` (research.md D1). |
| `handle` | `SinkHandle` (opaque) | Per-stream identity for compare-and-delete on clear (mirrors `OperationBridge` sink handles, `handler.ts:480-485`). |

**State transitions**:
```
UNBOUND ──bindStreamSink(sink, handle)──▶ BOUND ──clearStreamSink(handle)──▶ UNBOUND
                                              │ (rebind on new Connect: only after clear of prior)
```
- **Bound when**: the `Connect` handler receives the first inbound frame for a session (the status probe), and the team exists.
- **Cleared when**: the stream emits `error`/`end` (`handler.ts:507-522` cleanup path), via compare-and-delete so a newer stream's sink is not cleared.
- **Read by**: `emitChannelFrame` (init turn + compress/review nodes) and the TurnLoop emit — both resolve `this.streamSink?.(frame)` live, so rebind/clear is reflected immediately.

**Invariants**:
- At most one bound sink per session (`Connect` is exclusive — `specs/.../saolei_team_test.go TestTeamConnectExclusiveEmit`).
- Emitting while UNBOUND is a no-op (best-effort — research.md D9).

### 1.2 Continuous Connection Reader (desktop, runtime — REPLACES per-turn `recvLoop`)

The single goroutine that reads `TeamFrame`s from the gateway WebSocket for the entire connection lifetime.

| Field | Type | Source / note |
|---|---|---|
| `recvDone` | `chan struct{}` | Closed when the reader exits (`app.go:209`). `CloseAgent` waits on it (`app.go:1791-1796`); `Connect` waits on the **prior** `recvDone` before starting a new reader (reconnect handover). |
| `recvMu` | `sync.Mutex` | Guards `recvDone` reassignment across `Connect`/`CloseAgent`. |
| `sessionID` | `string` | From `a.sessionID` (set at `Connect`). |

**State transitions**:
```
STOPPED ──Connect (after probe recv)──▶ RUNNING ──RecvFrame error / CloseAgent──▶ STOPPED
                                            │
                                            ├─ wait FlowPart ──▶ forward (NO exit) ──▶ RUNNING
                                            ├─ messageParts    ──▶ append to chatstream ──▶ RUNNING
                                            └─ operation FlowPart ──▶ execute + send result ──▶ RUNNING
```

**Invariants** (FR-008, FR-011, FR-012):
- Exactly ONE reader per connection (protocol hard constraint — two `RecvFrame` goroutines corrupt the WS stream, `app.go:594-604`).
- `wait` updates frontend processing state (forwards to chatstream) but does **not** terminate the reader (FR-008).
- `SendUserTurn` does NOT start a reader (FR-012); it only sends the `UserFrame`.

---

## 2. Init Instruction Turn (unchanged persisted entity — formalized)

The background agent turn that produces the planner's opening instruction. Persisted shape is unchanged from spec 039 (`projects/game/agent/src/team/instruction-node.ts`); this feature only adds its **real-time emission**.

| Aspect | Value | Source |
|---|---|---|
| Trigger | `SessionTeamStore.update` after FIRST graph materialization | `session-team.ts:796-805` |
| Semantics | Fire-and-forget; `UpdateTeam` returns immediately (FR-005) | `session-team.ts:307-316` |
| Runner | One `graph.invoke` with `runInitInstruction: true` (NOT `streamEvents`) | `session-team.ts:342-368` |
| Scope | Once per session lifecycle; profile-change rebuild never re-triggers (040 FR-005) | `session-team.ts:833-880` |
| Produces | `plannerMessages`: [request HumanMessage, response AIMessage(tool_call)]; `playerMessages`: [HumanMessage(instruction)] | `instruction-node.ts:216-228` |
| Degrade | planner failure → `warn` + `return {}`; promise resolves; `initInFlight` clears | `instruction-node.ts:191-201`, `session-team.ts:310-315` |

**NEW with this feature**: the runner installs `emitChannelFrame` in its `configurable` (research.md D2), so the produced messages are pushed in real-time (§3).

---

## 3. Frame model emitted by the init turn

All frames are `TeamFrame` (`game.proto:711-735`) built by `buildTeamFrame` (`turn-loop.ts:129-148`). Each carries `frameId == <persisted message id>` (dedup anchor — §4). The init turn emits, for one successful invoke:

### 3.1 Planner request frame (→ planner tab)

| Field | Value |
|---|---|
| `agent` | `planner` (`PLANNER_AGENT_NAME`) |
| `role` | `MESSAGE_ROLE_USER` |
| `payload` | `messageParts.parts[0] = { text: { content: <buildInstructionRequest("init") text> } }` |
| `frameId` | request `HumanMessage.id` |

### 3.2 Planner response frame (→ planner tab) — faithful tool-call mirroring (research.md D3)

| Field | Value |
|---|---|
| `agent` | `planner` |
| `role` | `MESSAGE_ROLE_AGENT` |
| `payload` | `messageParts.parts[0] = { toolCall: { toolName: "instruct_player", argsJson: <JSON args incl. the instruction> } }` |
| `frameId` | response `AIMessage.id` |

Renders identically to `ListMessages(planner)` history (a tool-call bubble), so first-entry and re-entry are consistent.

### 3.3 Player instruction write-back frame (→ player tab)

| Field | Value |
|---|---|
| `agent` | `player` (`PRIMARY_AGENT_NAME`) |
| `role` | `MESSAGE_ROLE_USER` |
| `payload` | `messageParts.parts[0] = { text: { content: <instruction text> } }` |
| `frameId` | write-back `HumanMessage.id` |

**Ordering**: emitted in production order (request → response → write-back) after the planner invoke resolves. Batch (non-streaming) — acceptable for SC-001.

**Tagging**: `agent` is the producing/routing agent (FR-006); `role` mirrors the message type so the renderer picks the correct bubble style (USER → pre-wrap text path; AGENT → markdown). See `session-team.ts:148-156` for the role rationale.

---

## 4. Dedup rules (FR-004)

**Anchor**: `TeamFrame.frameId == Message.message_id`. This is the established pattern (`session-team.ts:140-146`; seed `FrameId = msg.GetMessageId()` at `internal/chatstream/stream.go:199-210`).

**Frontend dedup set**: `renderedMessageIds: Set<string>` (`App.svelte:103`):
- `loadAgentHistories` path (`App.svelte:501-504`): skips/adds by `m.messageId`.
- live/seed path (`App.svelte:720-722`): skips/adds by `frame.frameId`.

Because seed and history use the same id namespace as the real-time frames, a frame delivered by both paths is rendered exactly once.

**Why duplicate risk is low** (research.md D7): the seed and `loadAgentHistories` are both one-shot at connect; the real-time push fires only when the sink is bound (post-connect) and the init completes during the connection. Dedup is therefore defensive (covers reconnect/re-entry races — spec edge case 5).

| Scenario | Seed/History | Real-time push | Dup? |
|---|---|---|---|
| Init done before connect | delivers (one-shot at connect) | no-op (sink unbound) | No |
| Init done after connect | ran pre-init (empty); not re-run | delivers | No |
| Reconnect mid-init | seed reuses stream (no re-seed) | delivers on completion | No (ids match) |
| Race (history + push same id) | one delivers, other skipped by `renderedMessageIds` | — | No (dedup) |

---

## 5. Status / typing-indicator signals (unchanged — formalized)

No new signals. Existing behaviour retained (research.md D6):

| Signal | Source | Drives | Init effect |
|---|---|---|---|
| Status probe response (`StatusSignal`) | `deriveStatusSignal(team?.isRunning(), team !== undefined)` (`handler.ts:384-387`) | frontend `processing` (`App.svelte:540-545`) | IDLE (init excluded from `isRunning`) — FR-003 |
| `wait` FlowPart | TurnLoop terminals (`turn-loop.ts:403-430`); desktop synthesized on RecvFrame error (`app.go:655-665`) | clears `processing` (`App.svelte:778-788`) | init emits NO `wait` (outside TurnLoop) |
| `isBusy()` | `initInFlight \|\| isRunning()` (`session-team.ts:425-427`) | gates RefreshTeam/rebuild → FAILED_PRECONDITION (`handler.ts:258-265`, `session-team.ts:839-848`) | TRUE during init — FR-007 |

The continuous reader forwards `wait` to the chatstream (so the frontend clears `processing`) but, per FR-008, does **not** terminate on it.

---

## 6. Validation rules (from requirements)

- **FR-001**: init delivered via connection when it completes after connect → guaranteed by D1+D2 (sink bound at Connect + emitter installed).
- **FR-002**: continuous read after connect → §1.2 continuous reader.
- **FR-003 / FR-007**: probe IDLE during init; destructive ops rejected → `isRunning`/`isBusy` split (already implemented; retained).
- **FR-004**: no duplicate delivery → §4 dedup anchor.
- **FR-005**: `UpdateTeam` fire-and-forget retained → `session-team.ts:307-316` unchanged.
- **FR-006**: both tabs tagged with producing agent → §3 `agent` field.
- **FR-008**: reader survives `wait` → §1.2 state machine.
- **FR-009**: operations identical → reader reuses `handleInboundOperation`/`executeAgentOperation`.
- **FR-010**: clean close + clear pending push → `clearStreamSink` at stream end; reader exits on RecvFrame error.
- **FR-011 / FR-012**: single reader; `SendUserTurn` starts no reader → §1.2 invariants.
