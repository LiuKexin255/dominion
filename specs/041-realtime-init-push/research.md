# Research: Real-Time Init Instruction Delivery

**Feature**: `041-realtime-init-push` | **Date**: 2026-08-09 | **Spec**: [spec.md](./spec.md)

This document resolves every NEEDS CLARIFICATION and records the design decisions (D1–D9) with rationale and alternatives. It is the Phase 0 output consumed by [data-model.md](./data-model.md), [contracts/realtime-channel-contract.md](./contracts/realtime-channel-contract.md), and [quickstart.md](./quickstart.md).

## Context: why the init instruction is invisible today

The init instruction turn is invisible on the real-time channel by **three compounding, documented causes** (all at `projects/game/agent/src/session-team.ts:322-337`):

1. **Agent — no emitter**: `runInitTurn` runs one `graph.invoke` **without** installing `emitChannelFrame` in its `configurable`. The instruction node (`projects/game/agent/src/team/instruction-node.ts:164-178`) *would* emit a planner frame if an emitter were present, but for the init scenario none is injected ("此处对 init 自然跳过").
2. **Agent — no sink target**: even if an emitter were installed, it writes through `this.turnLoopEmit?.(frame)` (`session-team.ts:530-555`), and `turnLoopEmit` is `null` until the **first user `submit`** (`session-team.ts:378-390`). The init turn runs before any user message, so the emit would be a no-op.
3. **Desktop — no reader between turns**: `recvLoop` (`projects/game/desktop/app.go:643-717`) is launched **only by `SendUserTurn`** (`app.go:605-621`) and **terminates on the `wait` signal** (`app.go:707-713`). Between turns, nothing reads the WebSocket — any background frame sits unread.
4. **Timing**: the desktop connects **strictly after** `UpdateTeam` returns (`projects/game/desktop/frontend/src/App.svelte:455-456` `await updateTeam(...)` → `continueSessionEntry` → `await handleConnect()`), while the init turn is still in-flight in the background.

The instruction only becomes visible on the *next* session entry, when the one-shot seed (`OpenChatStream` → `chatstream.Registry.Open`, seeded once from `ListMessages`, `app.go:1818-1851`) and `loadAgentHistories` (`App.svelte:491-528`) read the now-persisted message. This is the production bug (session `6c4a7195e8690c4b647440fcf2f0298c`).

## Key finding: the frontend needs NO change

Exploration confirms the receive pipeline is already complete and generic:

- The frontend renders `messageParts` frames arriving over the **SSE chatstream** (`App.svelte:761-825` `handleAgentFrame` → `handleMessageParts`), not Wails events. The Go `recvLoop` appends inbound `messageParts` to the chatstream (`app.go:677`), so any frame the continuous reader receives is delivered to the renderer with no frontend work.
- Tab routing uses `frame.agent ?? seedAgent` (`App.svelte:587-595`). Init frames tagged `agent=planner` route to the planner tab; `agent=player` route to the player tab. No routing change.
- Dedup already exists: `renderedMessageIds: Set<string>` (`App.svelte:103`, used at `:501-504` history and `:720-722` live) keys on `frameId == messageId`. The Go seed (`chatstream.SeedFromHistory`, `internal/chatstream/stream.go:199-210`) stamps `FrameId = msg.GetMessageId()`, collapsing seed-vs-history onto one id namespace.
- The typing indicator is already driven only by real user turns: the connect status probe uses `deriveStatusSignal(team?.isRunning() …)` (`handler.ts:384-387`), and `isRunning()` **deliberately excludes** `initInFlight` (`session-team.ts:392-411`); `isBusy()` gates destructive ops (`session-team.ts:425-427`). The `isRunning`/`isBusy` split is already implemented (uncommitted in the working tree — spec Assumption line 106).

**Conclusion**: the work is **agent-side (stream sink + init emission)** + **desktop Go-side (continuous reader)**. Frontend and proto are untouched. This narrows scope and risk substantially.

---

## D1 — Stream-bound display sink on `SessionTeam` (bind at `Connect`, clear at stream end)

**Decision**: Introduce a single **stream-bound display sink** on `SessionTeam` — `bindStreamSink(sink, handle)` / `clearStreamSink(handle)` — whose lifecycle is tied to the `Connect` stream, **not** to the TurnLoop. The Connect handler binds the sink as soon as it sees a session on the stream (the first inbound frame — the status probe); `emitChannelFrame` and the TurnLoop both emit through it. Cleared on stream `end`/`error` (compare-and-delete via a per-stream handle, mirroring the existing OperationBridge sink pattern at `handler.ts:480-485`, `cleanupSinks` `:292-305`).

**Rationale**:
- The init turn (triggered by `UpdateTeam`, fire-and-forget) runs *before* any user message, so it cannot rely on `turnLoopEmit` (which is set only at first `submit`). A sink bound at `Connect` exists independently of turn lifecycle, so the in-flight init turn can emit to it.
- The desktop dials Connect immediately after `UpdateTeam` returns; the planner model call (the slow part of the init turn, ~seconds) overlaps the connect sequence. By the time the planner responds and the instruction node emits, the sink is bound → frames reach the desktop.
- Consolidating onto one sink (rather than adding a parallel "push sink") honours Constitution Principle II (refactoring over patching): the display-emission path is unified, and the TurnLoop reads the current sink via a closure over `this` so rebind/clear is reflected live.

**Timing / best-effort semantics** (spec edge cases 1–3):
- Init completes **before** the sink is bound → emit is a no-op (sink `null`) → the instruction is already in the checkpoint → the one-shot seed + `loadAgentHistories` deliver it on connect. No real-time push needed. ✓
- Init completes **after** the sink is bound → real-time push delivers it; `loadAgentHistories` already ran (with pre-init state) and is not re-run → no duplicate. ✓
- Init still running at connect, completes during the connection → pushed on completion through the bound sink (US1 acceptance scenario 3). ✓

**Alternatives considered**:
- *Register `turnLoopEmit` earlier (at `submit` of a synthetic turn)* — rejected: would conflate the init turn with the TurnLoop, violating the `isRunning`/`isBusy` split (the init must NOT drive the typing indicator) and FR-003.
- *Add a separate ad-hoc "init push sink" field alongside `turnLoopEmit`* — rejected: two parallel display sinks on one team is a patch, not a refactor (Principle II); it also risks divergence on which sink is "current."

---

## D2 — Install `emitChannelFrame` in `runInitTurn`; emit instruction frames with `frameId == messageId`

**Decision**: `runInitTurn` injects `emitChannelFrame` into its `configurable` (same key the user-turn path uses, `session-team.ts:530-555`), reading the now-bound stream sink (D1). The instruction turn emits display frames for each **newly-produced** message, each tagged with the producing agent and carrying `frameId == <message id>` for exact dedup against history.

**What is produced and emitted** (mirrors `instruction-node.ts:216-228` return + `result.messages`):

| Frame | `agent` | `role` | content | `frameId` |
|---|---|---|---|---|
| Planner request (the scenario prompt) | `planner` | `USER` | the `buildInstructionRequest("init")` text | request `HumanMessage.id` |
| Planner response (the instruction, via `instruct_player` tool call) | `planner` | `AGENT` | the tool-call `MessagePart` (faithful — see D3) | response `AIMessage.id` |
| Player instruction write-back | `player` | `USER` | the instruction text | write-back `HumanMessage.id` |

**Rationale**:
- This is the **established channel-frame pattern** (`emitChannelFrame` + `buildTeamFrame` + `safeWrite`) already used by the compress/review planner nodes (`compress.ts:210-268`, `planner.ts:321-344`). We reuse it, not invent a new push mechanism (Principle II).
- `frameId == messageId` is the **established dedup anchor** (`session-team.ts:140-146`; `turn-loop.ts:118-122`; `compress.ts:24-32` use `frameId == summary msg.id`). Reusing it makes the real-time frames dedup-exact against the seed and `ListMessages` (D7).
- Emitting each produced message (not just one summary frame) guarantees **first-entry and re-entry render identically** (D3).

**Alternatives considered**:
- *Emit only the player write-back (one frame)* — rejected: US1 acceptance scenario 1 requires the instruction in **both** the planner tab and the player tab in real-time; the planner tab is not seeded (only `loadAgentHistories`, one-shot), so without a planner push the planner tab stays empty until re-entry.
- *Emit a single text "instruction" frame to both tabs* — rejected: dedup would be inexact (planner history is a tool-call `AIMessage`, not text) and first-entry vs re-entry rendering would diverge (D3).

---

## D3 — Faithful message→frame mirroring (planner-tab parity between real-time and history)

**Decision**: Init frames mirror the **renderable content** of the persisted messages (text → `text` part; tool-call → `toolCall` part with `argsJson`), using the same frame-building the TurnLoop uses (`turn-loop.ts:444-496`). Concretely: extract/share the message→`MessagePart` conversion so the init path and the TurnLoop render tool calls identically. The planner's response is a `tool_call` (`instruct_player(instruction)`); the real-time planner frame therefore carries a `toolCall` `MessagePart` (not plain text), exactly as `ListMessages(planner)` would later return.

**Rationale**:
- The current `ChannelFrameEmitter` type is text-only (`(agent, content, frameId?, role?) => void`). To faithfully mirror a tool-call response, the emission must carry pre-built `MessagePart[]` (or the node builds the part). Extending the emission to accept `MessagePart[]` is a clean, reusable generalization (Principle II) and keeps the planner-tab rendering consistent between first-entry (real-time push) and re-entry (history) — satisfying US1 acceptance scenario 2 ("no duplicate rendering" implies consistent rendering).
- Without this, the planner tab would show the instruction as a text bubble on first entry but as a tool-call bubble on re-entry — an inconsistency that violates the spirit of FR-004 and Principle II.

**Alternatives considered**:
- *Text-only planner frame (instruction text, fresh `frameId`)* — rejected: dedup-inexact and rendering-inconsistent (see above).
- *Stream the planner response token-by-token via `streamEvents` (like user turns)* — rejected: the init turn is intentionally a background turn outside the TurnLoop (FR-003); streaming would require TurnLoop machinery and would re-introduce typing-indicator coupling. Batch emission after the `invoke` resolves is sufficient for SC-001 ("visible within 10 s") and far simpler.

**Note on the request frame**: the request (scenario prompt) is internal scaffolding, but `ListMessages(planner)` *does* persist and return it (it is appended to `plannerMessages`). Emitting it preserves exact parity; its `USER` role renders as a user-style bubble in the planner tab, matching re-entry. If product review later wants it hidden, that is a separate rendering concern — out of scope here.

---

## D4 — Desktop continuous connection reader (replaces per-turn `recvLoop`)

**Decision**: Replace the per-turn `recvLoop` with a **continuous reader** with these properties (FR-002, FR-008, FR-009, FR-011, FR-012):

- **Started at `Connect`**, immediately **after** the one-shot status probe send+recv completes (D5). It runs for the entire connection lifetime.
- **Survives `wait`**: a `wait` `FlowPart` is still forwarded to the chatstream (so the frontend clears `processing`), but it does **NOT** terminate the reader (FR-008). The reader continues to the next `RecvFrame`.
- **Single reader** (FR-011): exactly one reader goroutine per connection. `SendUserTurn` (FR-012) **only sends** the `UserFrame`; it no longer starts a reader and no longer carries the `recvMu`/`recvDone` check-and-start block (`app.go:605-621` is removed).
- **Operations identical** (FR-009): `handleInboundOperation` / `executeAgentOperation` are reader-agnostic and are reused unchanged — operation `FlowPart`s are executed inline and their `FlowResultPart`s sent back over the WS.
- **Clean close** (FR-010): on `RecvFrame` error (connection closed/`CloseNow`), the reader synthesizes a terminal `wait` (preserving the current graceful-frontend-settle behaviour, `app.go:655-665`) and exits, closing `recvDone`. `CloseAgent` tears the socket down then waits on `recvDone` (as today, `app.go:1791-1796`).
- **Reconnect handover**: `Connect` closes any prior WS first (`app.go:1655-1657`); it MUST also wait on the prior `recvDone` (under `recvMu`) before starting a new reader, so the old reader has exited and cannot close the new `recvDone`.

**Rationale**:
- A continuous reader is the only way background-produced frames (init, and future background tasks) reach the desktop without user interaction (US3). The per-turn model fundamentally cannot receive between turns.
- Removing the wait-terminates behaviour (FR-008) is the precise change that lets the reader span multiple turns: the TurnLoop emits `wait` only at turn boundaries (`turn-loop.ts:403-430`); the reader treats `wait` as a state update, not a stop signal.
- The single-reader invariant (FR-011) is a protocol hard constraint (`app.go:594-604` documents that two `RecvFrame` goroutines corrupt the WS stream); the continuous reader preserves it by construction.

**Alternatives considered**:
- *Keep per-turn `recvLoop` and add a separate "background reader" between turns* — rejected: violates the single-reader invariant (two readers corrupt the stream) and is a patch (Principle II).
- *Poll `ListMessages` on a timer* — rejected: contradicts the spec ("through the existing connection, not solely via the history-listing API", FR-001) and the one-shot seed model (spec Assumption); re-introduces the latency/dup problems polling causes.

---

## D5 — Status probe stays a one-shot sync exchange at `Connect` (continuous reader starts after)

**Decision**: The connect status probe remains a **synchronous send+recv** at the start of `Connect` (`app.go:1677-1720`), returning the `StatusSignalStatus` enum name to the frontend. The continuous reader (D4) is started **after** the probe `RecvFrame` returns, so there is never a second concurrent reader during the probe.

**Rationale**:
- The frontend synchronously reconciles the typing indicator from the probe result (`App.svelte:540-545`). Folding the probe into the continuous reader would require correlating the probe response among arbitrary inbound frames — more complexity for no gain.
- Starting the reader strictly after the probe `RecvFrame` preserves the single-reader invariant trivially.

**Alternatives considered**:
- *Fold the probe into the continuous reader (probe response arrives via the reader)* — rejected: adds frame correlation complexity and races the probe timeout against the reader lifecycle; the sync probe is simple and proven.

---

## D6 — Typing indicator: unchanged (already correct via `isRunning`/`isBusy` split)

**Decision**: No typing-indicator code is added. The continuous reader forwards `wait` (clears `processing`) exactly as the per-turn reader did; background init frames are `messageParts` only (no `wait`, no `status`), so `processing` is unaffected by the init turn. The `isRunning`/`isBusy` split (already implemented) ensures: status probe → IDLE during init (FR-003); RefreshTeam/rebuild → FAILED_PRECONDITION during init (FR-007).

**Rationale** (from `session-team.ts:392-427`): the init turn runs outside the TurnLoop and emits no `wait`; if it drove `isRunning` (ACTIVE), the one-shot probe would stick the typing indicator ON with nothing to clear it. The split cleanly separates "running" (typing) from "busy" (destructive-op gating).

**Alternatives considered**: none — the split is the correct, already-implemented design; this feature must not regress it.

---

## D7 — Dedup model: `frameId == messageId`; practical duplicate risk is low

**Decision**: Every real-time init frame carries `frameId == <persisted message id>` (D2). The frontend's existing `renderedMessageIds` set (`App.svelte:103`, `:720-722`) then dedups against any same-id frame from the seed or `loadAgentHistories`.

**Duplicate-risk analysis** (why the risk is low, and dedup is defensive):
- The seed (`chatstream.Registry.Open`) and `loadAgentHistories` are **both one-shot at connect time**. The real-time push fires only when the stream sink is bound (post-connect) **and** the init completes during the connection.
- **Case A — init completes before connect**: sink unbound during init → emit no-op → no real-time frame. Seed + `loadAgentHistories` deliver it once. No dup.
- **Case B — init completes after connect**: real-time push delivers it. `loadAgentHistories` already ran with pre-init state and is not re-run; the seed ran at connect before the player write-back existed. No dup.
- Dedup (`renderedMessageIds`) is therefore **defensive** — it covers reconnect/re-entry races and the spec's edge case 5 ("already loaded from history and real-time push also delivers it"). FR-004 is satisfied by reusing the existing mechanism with the correct anchor; no new dedup logic is added.

**Alternatives considered**: *introduce a server-side dedup gate* — rejected: unnecessary; the frontend already dedups correctly given the right anchor, and a server gate would add statefulness to a stateless stream.

---

## D8 — No proto changes

**Decision**: No `game.proto` edits. All init delivery uses existing messages:
- `TeamFrame` (`game.proto:711-735`) with `messageParts` payload for instruction content; `flowParts` for the existing `wait`/`status` signals.
- `MessagePart` oneof (`game.proto:478-486`): `text` for the request/write-back; `toolCall` (`game.proto:468-472`) for the planner's `instruct_player` response.
- `TeamFrame.agent` (`game.proto`, field 5) for tab routing; `TeamFrame.role` (field 6) for USER/AGENT rendering.

**Rationale**: the contracts are **semantic/behavioral** over the existing schema (like spec 021, `specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md`). Adding fields would balloon scope and risk back-compat issues for zero functional gain.

---

## D9 — Best-effort / degrade semantics retained

**Decision**: Real-time push is **best-effort and never blocks**. If the sink is unbound at emit time, the emit is a no-op (D1). If the planner model fails, the instruction node's existing degrade path runs (`instruction-node.ts:191-201` — `warn` + `return {}`); no frame is emitted; the init promise resolves; `initInFlight` clears in `finally` (`session-team.ts:310-315`). The session remains fully usable for user turns.

**Rationale**: matches spec edge case 1 and Assumption ("real-time push is best-effort and never blocks the session"). The init turn's `graph.invoke` failure handling is unchanged; only the *success* path gains emission.

---

## Open questions resolved

| Question | Resolution |
|---|---|
| Does the frontend need changes for US1/US3? | **No** — SSE→`handleMessageParts`→`renderedMessageIds` + `frame.agent` routing already handle it (verified by exploration). |
| Does the proto need changes? | **No** — reuse `TeamFrame`/`MessagePart`/`FlowPart` (D8). |
| When is the stream sink bound, given Connect is bidi and session-scoped? | On the **first inbound frame for a session** (the status probe), via a per-stream bound-sessions set, cleared at stream end (D1). |
| Does the continuous reader subsume the probe? | **No** — probe stays a sync one-shot; reader starts after it (D5). |
| How is the single-reader invariant preserved across reconnect? | `Connect` waits on the prior `recvDone` before starting a new reader (D4). |
| How is planner-tab rendering kept consistent between first-entry and re-entry? | Faithful message→frame mirroring incl. tool-call parts (D3). |

No NEEDS CLARIFICATION remains.
