# Implementation Plan: Real-Time Init Instruction Delivery

**Branch**: `041-realtime-init-push` | **Date**: 2026-08-09 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from [`/specs/041-realtime-init-push/spec.md`](./spec.md)

## Summary

Deliver the planner's init (calibration) instruction to the desktop in **real-time through the existing agent↔desktop connection** — instead of relying on `ListMessages` history polling — so it is visible on first session entry without leaving and re-entering. Three coordinated changes:

1. **Agent (TypeScript)** — register a *stream-bound display sink* on the `SessionTeam` at `Connect` time (independent of the TurnLoop lifecycle), so the fire-and-forget init turn can emit frames. Install `emitChannelFrame` in `runInitTurn` so the instruction node pushes the planner's instruction message (→ planner tab) and the player instruction write-back (→ player tab), each tagged with the producing agent and carrying `frameId == messageId` for dedup.
2. **Desktop backend (Go)** — replace the **per-turn `recvLoop`** (launched by `SendUserTurn`, terminates on `wait`) with a **continuous connection reader** started at `Connect` (after the one-shot status probe) that runs for the whole connection lifetime, survives the terminal `wait` signal, executes operations identically, and terminates cleanly on close. `SendUserTurn` no longer starts a reader.
3. **Frontend (Svelte) — NO change, NO proto change.** The frontend already renders `messageParts` from the SSE chatstream, routes by `frame.agent`, and dedups via `renderedMessageIds`; the typing indicator is already driven only by real user turns via the `isRunning`/`isBusy` split (already implemented, committed on the 040 branch HEAD `868c49b`).

The typing-indicator fix (US2) requires no new code: the status probe already excludes the background init turn (`isRunning()` excludes `initInFlight`; `isBusy()` gates destructive ops). Re-uses the established channel-frame emission pattern (`emitChannelFrame` + `buildTeamFrame` + `safeWrite`) and the established dedup anchor (`frameId == msg.id`, as used by the compress node).

## Technical Context

**Language/Version**: TypeScript 5.x (agent), Go 1.24 (desktop backend), Svelte 5 / TypeScript (frontend).

**Primary Dependencies**:
- Agent: LangGraph (`@langchain/core`, `langchain`) team graph + checkpointer; `@grpc/grpc-js` bidi streaming (`TeamService.Connect`); see `projects/game/agent/src/`.
- Desktop: Wails v2 (Go↔webview bridge + bound methods); `nhooyr.io/websocket` (gateway WS client, `projects/game/desktop/internal/api/websocket.go`); local SSE chatstream (`projects/game/desktop/internal/chatstream`).
- Frontend: Svelte 5 (`projects/game/desktop/frontend/src/App.svelte`, `chat-stream.ts`).
- Gateway: Go, bridges desktop WebSocket ↔ agent gRPC `Connect` (`projects/game/gateway/cmd/main.go:227-279`).

**Storage**: LangGraph checkpointer (per-session thread, `thread_id = sessionId`) persists the init instruction into `plannerMessages`/`playerMessages`; the desktop `chatstream.Registry` is an in-memory per-session SSE log (one-shot seed from `ListMessages`, no polling).

**Testing**: `vitest` (agent TS unit tests, e.g. `session-team.test.ts`, `handler.test.ts`); `go test` via `bazel test` (desktop `app_test.go`, `view_model_test.go`); large tests via the **testplan** skill (`tools/test/guitar`, `projects/game/testplan/saolei_team_test.go`) per `style/large_test.md`.

**Target Platform**: Windows desktop (Wails app) + Linux server (agent + gateway).

**Project Type**: desktop application (Wails) consuming a backend service (agent) over a gateway-bridged real-time channel.

**Performance Goals**: init instruction visible within ~10 s of team materialization (the planner model's typical response time — SC-001); continuous reader adds no per-turn latency (it is always-on, not per-turn-started).

**Constraints**: single concurrent reader on the WebSocket (protocol-level — two `RecvFrame` goroutines corrupt the stream, `app.go:594-604`); `Connect` is exclusive per session (one live stream); the init turn is best-effort/degrade (planner failure skips the instruction, never blocks the session — spec edge case 1).

**Scale/Scope**: single-session desktop client; one connection per session. Touches 3 agent files, 1 desktop file (reader + connect lifecycle), 0 frontend files, 0 proto files.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Against `.specify/memory/constitution.md` v1.3.0:

| Principle | Status | Evidence |
|---|---|---|
| **I. Citation & Provenance** | ✅ PASS | All references carry repo-relative paths or full URLs (see research.md, contracts, data-model). Code comments will cite spec FRs + file:line. |
| **II. Refactoring Over Patching** | ✅ PASS | The per-turn `recvLoop` model is **replaced** by a continuous reader (architectural change to the connection read model), not patched. The display-sink emission is **consolidated** into a single stream-bound sink (decoupled from the TurnLoop lifecycle) rather than adding a parallel ad-hoc push path. |
| **III. Interface-First Design** | ✅ PASS | Channel behaviors are specified first in [`contracts/realtime-channel-contract.md`](./contracts/realtime-channel-contract.md) (sink lifecycle, init frame shapes, continuous-reader contract, dedup anchors) before any implementation. No new RPC/proto — contracts are semantic over existing `TeamFrame`/`MessagePart`/`FlowPart`. |
| **IV. Test Granularity & Cadence** | ✅ PASS | Unit tests (agent TS + desktop Go) per change, run via `bazel build`+`bazel test` as part of dev tasks (not separate). Large test via testplan as acceptance (gate 5). |
| **V. Read Before Code** | ✅ PASS | Deferred to `tasks.md` (Phase 2). Each phase will declare its document set in the three mandatory categories. |
| **VI. Large Test Acceptance** | ✅ PASS | Service-type change to a real-time channel → large test (testplan) is mandatory acceptance: full deploy→test→cleanup via `guitar run`, all cases must pass. Planned in quickstart.md + a dedicated acceptance task. |

**Gate evaluation**: no violations to justify. Proceeding to Phase 0.

## Project Structure

### Documentation (this feature)

```text
specs/041-realtime-init-push/
├── plan.md                          # This file
├── research.md                      # Phase 0 — design decisions (D1–D9)
├── data-model.md                    # Phase 1 — entities, frame model, dedup rules
├── quickstart.md                    # Phase 1 — validation/run guide
├── contracts/
│   └── realtime-channel-contract.md # Phase 1 — channel behaviors (sink, frames, reader, dedup)
└── tasks.md                         # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

The change spans the game project's agent (TS), desktop backend (Go). The frontend and proto are intentionally untouched.

```text
projects/game/
├── game.proto                              # NO change — reuses TeamFrame/MessagePart/FlowPart
├── agent/src/                              # TypeScript agent service
│   ├── session-team.ts                     # SessionTeam: add stream-sink bind/clear; install emitChannelFrame in runInitTurn; isRunning/isBusy (already done)
│   ├── handler.ts                          # Connect handler: bind display sink on first per-session frame; clear on stream end/error
│   ├── turn-loop.ts                        # (reused, not modified) message→part frame builder at turn-loop.ts:444-496, reused for faithful init-frame mirroring
│   └── team/instruction-node.ts            # (minor) init emission of produced messages with frameId == msg.id (parity)
└── desktop/
    ├── app.go                              # Connect: start continuous reader after probe; SendUserTurn: drop reader-start; recvLoop→continuous (survives wait); CloseAgent: wait on reader
    └── frontend/src/                       # NO change — already renders SSE messageParts, routes by frame.agent, dedups via renderedMessageIds
```

**Structure Decision**: Multi-project monorepo (agent TS + desktop Go + Svelte frontend). This feature touches the **agent** (`projects/game/agent/src/`) and the **desktop Go backend** (`projects/game/desktop/app.go`) only. The frontend (`projects/game/desktop/frontend/src/`) and the proto (`projects/game/game.proto`) are deliberately unchanged — verified by exploration that the existing SSE→`handleMessageParts`→`renderedMessageIds` pipeline and `frame.agent` tab routing already satisfy US1/US3 on the receive side, and the `isRunning`/`isBusy` split already satisfies US2.

## Complexity Tracking

> No Constitution Check violations — table left empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
