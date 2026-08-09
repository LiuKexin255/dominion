# Contract: Real-Time Channel — Init Delivery, Continuous Reader & Sink Lifecycle

**Feature**: `041-realtime-init-push` | **Date**: 2026-08-09 | **Spec**: [spec.md](../spec.md) | **Research**: [research.md](../research.md)

This contract specifies **behaviors added over the existing agent↔desktop bidirectional `Connect` channel** (`TeamService.Connect`, `projects/game/game.proto:102`) and the **desktop-side read model**. **No proto schema is added** (research.md D8); these are protocol/runtime/semantic contracts over existing `TeamFrame` / `MessagePart` / `FlowPart` messages.

Reference messages (`projects/game/game.proto`):
- `TeamFrame { session_id, template_id, frame_id, create_time, agent, role, oneof payload { message_parts | flow_parts } }` (lines 711-735)
- `MessagePart` oneof `text | thinking | image | tool_call | tool_result` (478-486); `ToolCallPart { tool_name, args_json }` (468-472)
- `FlowPart` oneof `… | wait | warn | status | …` (498-517); `StatusSignalStatus ∈ {UNSPECIFIED, ACTIVE, IDLE}` (651-658)
- `MessageRole ∈ {UNSPECIFIED, USER, AGENT}` (385-389)

Related contracts (unchanged, referenced for boundary):
- [`specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md`](../../021-agent-session-resync/contracts/agent-desktop-channel-contract.md) §1 — status probe (retained).
- [`specs/030-queued-chat-input/contracts/turn-loop-contract.md`](../../030-queued-chat-input/contracts/turn-loop-contract.md) — `wait`/queue signal semantics (retained; the reader no longer terminates on `wait`).

---

## §1. Stream display sink lifecycle (agent → desktop write target)

A single **display sink** (canonical name **stream display sink**; abbreviated **stream sink** in tasks.md; code identifiers `streamSink` / `bindStreamSink` / `clearStreamSink`) is bound on the `SessionTeam` per live `Connect` stream, independent of the TurnLoop. It is the write target for ALL display-channel frames from that session: the init turn's `emitChannelFrame`, the compress/review channel frames, and the TurnLoop's display frames.

### §1.1 Bind

**When**: the `Connect` handler (`projects/game/agent/src/handler.ts`) receives the **first inbound `UserFrame` carrying a given `session_id`** on a stream (in practice the status probe — the desktop sends it first at connect, `projects/game/desktop/app.go:1677-1685`), AND the team for that session exists (`sessionTeamStore.get(sessionId)`).

**Action**: `team.bindStreamSink((frame: TeamFrame) => safeWrite(stream, frame, sessionId), handle)`. Track the bound `(sessionId, handle)` in a per-stream set (e.g. `boundDisplaySinks: Set<string>`), mirroring the existing `sessionSinkHandles`/`activeLoopSessions` pattern (`handler.ts:291-323`).

**Field on `SessionTeam`** (`projects/game/agent/src/session-team.ts`):
- `streamSink: ((frame: TeamFrame) => void) | null` — `null` while unbound.
- `bindStreamSink(sink, handle)` / `clearStreamSink(handle)` — compare-and-delete on `handle` so a newer stream's sink is not cleared.

### §1.2 Emit (unified read path)

`emitChannelFrame` (the closure installed in `configurable`, `session-team.ts:530-555`) and the TurnLoop's display emit both resolve `this.streamSink?.(frame)` **live** (closure over `this`), so a rebind/clear during the connection is reflected on the next emit without reconstructing the TurnLoop.

**Guarantee**: emitting while `streamSink` is `null` (unbound) is a **no-op** (best-effort — research.md D9). This is the degrade path for "init completes before connect" (the seed/history delivers it instead).

### §1.3 Clear

**When**: the stream emits `error` or `end` (`handler.ts:507-522`).

**Action**: for each bound session, `team.clearStreamSink(handle)` (compare-and-delete). This is the **pending background push callback clearance** of FR-010 — after clear, the init turn (if still in-flight) emits to `null` and frames are not written to a dead connection.

### §1.4 Invariants

- At most one bound display sink per session (`Connect` is exclusive — `projects/game/testplan/saolei_team_test.go::TestTeamConnectExclusiveEmit`).
- The display sink is **distinct from** the `OperationBridge` sink (`handler.ts:480-485`): the bridge routes operation `flow_result`s; the display sink routes `messageParts`. Both write to the same stream via `safeWrite` but are managed independently. **No change to the bridge sink lifecycle** in this feature.

---

## §2. Init instruction real-time frames (agent → desktop)

When the init turn's `graph.invoke` succeeds, the instruction node emits display frames for each newly-produced message (research.md D2/D3). Emitted through the bound display sink (§1), so they reach the desktop iff the sink is bound at emit time.

### §2.1 Emission point

`runInitTurn` (`session-team.ts:342-368`) installs `emitChannelFrame` in its `configurable` (same key/shape as the user-turn path). The instruction node (`projects/game/agent/src/team/instruction-node.ts`) calls it for each produced message after the planner invoke resolves. Emission is **batch** (post-invoke), not token-streaming.

### §2.2 Frame shapes

Each frame is a `TeamFrame` built by `buildTeamFrame` (`turn-loop.ts:129-148`). `frameId == <persisted message id>` (dedup anchor — §4).

| # | `agent` | `role` | `messageParts.parts[0]` | `frameId` | Routes to |
|---|---|---|---|---|---|
| 1 | `planner` | `USER` | `text.content` = init request prompt | request `HumanMessage.id` | planner tab |
| 2 | `planner` | `AGENT` | `toolCall { tool_name:"instruct_player", args_json:<instruction args> }` | response `AIMessage.id` | planner tab |
| 3 | `player` | `USER` | `text.content` = instruction text | write-back `HumanMessage.id` | player tab |

**Faithful mirroring** (research.md D3): frame 2 carries a `toolCall` `MessagePart` (not text) so the planner tab renders identically to `ListMessages(planner)` on re-entry. The message→part conversion reuses the TurnLoop's frame-building logic (`turn-loop.ts:444-496`).

### §2.3 Best-effort / degrade

- Planner model failure → the node's existing degrade (`instruction-node.ts:191-201`): `warn` + `return {}`. **No frame emitted.** The init promise resolves; `initInFlight` clears. Session remains usable. (spec edge case 1.)
- Sink unbound at emit time → no-op (§1.2). The instruction is in the checkpoint; the seed/`loadAgentHistories` deliver it (spec edge case 3).

### §2.4 What is NOT emitted by the init turn

- No `wait` FlowPart (the init runs outside the TurnLoop — FR-003; the typing indicator must not be driven by it).
- No `status` FlowPart (the probe is a separate one-shot exchange, §5).
- No operation `FlowPart`s (the init turn calls only `instruct_player`; it performs no desktop automation).

---

## §3. Continuous connection reader (desktop)

The desktop replaces the per-turn `recvLoop` (`projects/game/desktop/app.go:643-717`, launched by `SendUserTurn`, terminates on `wait`) with a **continuous reader** that spans the whole connection.

### §3.1 Lifecycle

| Step | Where | Action |
|---|---|---|
| Start | `Connect` (`app.go:1629-1754`), **after** the one-shot probe `RecvFrame` returns (`app.go:1709`) | `a.recvDone = make(chan struct{}); go a.readLoop(a.sessionID)` |
| Reconnect handover | `Connect`, after closing prior WS (`app.go:1655-1657`) | Wait on the **prior** `a.recvDone` (under `a.recvMu`) before starting a new reader — so the old reader has exited and cannot close the new `recvDone`. |
| Exit | `readLoop`, on `RecvFrame` error | Synthesize a terminal `wait` `FlowPart` appended to the chatstream (preserve current graceful-settle, `app.go:655-665`); `defer close(a.recvDone)`. |
| Close | `CloseAgent` (`app.go:1777-1799`) | `a.ws.Close()` (unblocks the reader's `RecvFrame`); wait on `a.recvDone`; `a.ws = nil`. |

### §3.2 Per-frame handling (preserves current semantics — FR-009)

For each `TeamFrame` from `RecvFrame`:
- `messageParts` → `chatStreams.Append(sessionID, resp)` (`app.go:677`) — renders in conversation (covers init frames §2 and all turn display frames).
- `flowParts`:
  - Operation kinds (`MouseMove`/`MouseClick`/`KeyboardPress`/`MouseMoveAndClick`) → `handleInboundOperation` (`app.go:685`) — execute + send `FlowResultPart` back; **NOT** appended to chatstream (FR-005). **Unchanged.**
  - Signal kinds (`wait`/`warn`/`status`) → re-wrap and `chatStreams.Append` (`app.go:697-706`).

### §3.3 `wait` does NOT terminate the reader (FR-008)

The current `wait` branch returns (`app.go:707-713`). The continuous reader **forwards** the `wait` to the chatstream (so the frontend clears `processing`, `App.svelte:778-788`) and then **continues** to the next `RecvFrame` — it does **not** `return`. This is the core change that lets one reader span multiple turns and receive background frames.

### §3.4 Invariants

- **Single reader** (FR-011): exactly one `readLoop` per connection. The WS protocol allows only one concurrent `RecvFrame` (`app.go:594-604`).
- **`SendUserTurn` starts no reader** (FR-012): `SendUserTurn` (`app.go:519-623`) only validates + `SendFrame`. The `recvMu`/`recvDone` check-and-start block (`app.go:605-621`) is **removed**.
- **Operations identical** (FR-009): `handleInboundOperation`/`executeAgentOperation` are reader-agnostic and reused unchanged.

---

## §4. Dedup contract (FR-004)

**Anchor**: `TeamFrame.frame_id == Message.message_id`.

| Delivery path | id carried | Dedup set entry |
|---|---|---|
| Seed replay (Go `chatstream.SeedFromHistory`) | `FrameId = msg.GetMessageId()` (`internal/chatstream/stream.go:199-210`) | `renderedMessageIds` (frontend `App.svelte:720-722`) |
| `loadAgentHistories` (frontend `listMessages`) | `m.messageId` (`App.svelte:501-504`) | `renderedMessageIds` |
| Real-time push (init frames §2) | `frameId = <message id>` | `renderedMessageIds` |

**Guarantee**: because all three paths key on the same id namespace, a message delivered by more than one path is rendered exactly once. The init frames MUST set `frameId` to the producing message's id (NOT a fresh `randomUUID`) — this is mandatory for FR-004.

**Requirement on the agent**: the init emission (§2.2) MUST use the LangGraph message id (`HumanMessage.id` / `AIMessage.id`) as `frameId`, exactly as the compress node does (`compress.ts:24-32`, `session-team.ts:140-146`).

---

## §5. What is unchanged (explicit non-changes)

| Behavior | Source | Note |
|---|---|---|
| Status probe (one-shot, sync, 10s timeout) | `app.go:1677-1720`, `handler.ts:366-395` | Retained verbatim; continuous reader starts **after** it (§3.1). Probe returns IDLE during init (`isRunning` excludes `initInFlight`). |
| `isRunning` / `isBusy` split | `session-team.ts:392-427` | Already implemented, committed on the 040 branch HEAD `868c49b`. `isRunning` → probe/typing; `isBusy` → destructive-op gate. This feature MUST NOT regress it (FR-003/FR-007). |
| `UpdateTeam` fire-and-forget | `session-team.ts:307-316`, `handler.ts:101-172` | RPC returns immediately after materialization; init delivery is a separate concern via the connection (FR-005). |
| Destructive-op gating | `handler.ts:258-265` (RefreshTeam), `session-team.ts:839-848` (rebuild) | FAILED_PRECONDITION while `isBusy()` — covers the init turn (FR-007). |
| Operation execution | `app.go:743-1044`, `app_operation.go` | Reused unchanged by the continuous reader (FR-009). |
| OperationBridge sink lifecycle | `handler.ts:480-485`, `:292-305` | Distinct from the display sink (§1.4); unchanged. |
| Frontend rendering / dedup / tab routing | `App.svelte:587-595, 712-759` | NO change — already handles `messageParts` from SSE, routes by `frame.agent`, dedups via `renderedMessageIds`. |
| Proto schema | `projects/game/game.proto` | NO change (research.md D8). |
