# Implementation Plan: Queued Chat Input During Agent Run

**Branch**: `030-queued-chat-input` | **Date**: 2026-07-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/030-queued-chat-input/spec.md`, with the user's planning directive: **prefer LangChain native capabilities to implement message queuing** (no bespoke frontend queue, no custom mutex-patch queue).

## Summary

Allow the user to type and submit chat messages while an agent turn is in progress; submitted messages are queued and, on turn completion, combined into the next agent turn's input which is passed to the LLM (FIFO). The queue is implemented with **LangGraph-native primitives** — a per-session agent loop that buffers pending `HumanMessage`s and drains them turn-by-turn via repeated `streamEvents` invocations on the same `MemorySaver`-backed thread — rather than a custom frontend array or a custom backend queue/mutex patch. Conversation continuity across the auto-continued turns is provided natively by the LangGraph checkpointer (the documented multi-turn pattern). The desktop input is un-disabled during a run; a minimal, additive queue-state signal over the existing flow channel lets the frontend render the pending queue and transition items to normal as they are consumed.

## Technical Context

**Language/Version**: TypeScript (agent service `projects/game/agent`), Go (desktop backend `projects/game/desktop`), Svelte 5 runes (desktop frontend `projects/game/desktop/frontend`).

**Primary Dependencies**:
- `langchain` ^1.5.3 — `createAgent` (the compiled ReactAgent used by `AgentAdapterImpl`, `projects/game/agent/src/llm.ts:325`).
- `@langchain/langgraph` ^1.4.8 — `MemorySaver` checkpointer, `streamEvents(v3)` streaming loop.
- `@langchain/core` ^1.2.3 — `HumanMessage` / content blocks.
- gRPC bidi streaming (`nice-grpc`) — `Connect(stream AgentFrame)` (`projects/game/game.proto:90`).
- Wails — desktop host↔webview bridge (`SendUserTurn`, `projects/game/desktop/app.go:701`).

**Storage**: `MemorySaver` (in-process, per-session checkpointer) — the existing conversation-state store. No new persistence is introduced; the queue is in-memory transient state.

**Testing**: Vitest (`bazel test //projects/game/agent/...`, `js_test`) for unit/contract tests; Bazel `bazel build`/`bazel test`. Large-test acceptance via the testplan skill (`tools/test/guitar`, `style/large_test.md`).

**Target Platform**: Desktop application (Wails on Windows) + co-located Node agent service; single-user, local.

**Project Type**: desktop-app + service.

**Performance Goals**: Queue hand-off is prompt — when the in-flight turn completes and the queue is non-empty, the next (combined) turn begins without perceptible delay (sub-second). No throughput target (single user).

**Constraints**:
- MUST NOT alter the in-flight turn (spec FR-002); queued input only affects the next turn.
- MUST be LangGraph-native (user directive): conversation continuity via the checkpointer; no bespoke history/queue persistence.
- MUST preserve the `wait`-signal turn-boundary semantics and the spec-021 status reconciliation (`STATUS_ACTIVE` while a turn/loop is in flight, `STATUS_IDLE` when idle).
- MUST preserve the non-blocking `SendUserTurn` contract (feature 015) and the graceful-abort mechanism (feature 017).

**Scale/Scope**: Single desktop user; one session at a time active. Queue holds a small number of short user messages.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle (Constitution §) | Status | Note |
|---|---|---|---|
| 1 | §I Citation & Provenance | PASS | Plan cites LangGraph docs (multi-turn/streaming-loop/HITL), the opencode reference, and in-repo files with paths+lines. |
| 2 | §II Refactoring Over Patching | PASS | The per-frame `acquireMutex→generateTurn→releaseMutex` path (`projects/game/agent/src/handler.ts:390-542`) does not fit "queue + auto-continue + combine". It is refactored into a per-session turn loop that is the single-flight mechanism; `status-signal.ts`'s pure `deriveStatusSignal` is reused (fed `loop.isRunning()`). This is a design-level change delivered with the feature, not a patch. |
| 3 | §III Interface-First Design | PASS | Two contracts defined before implementation: the internal **TurnLoop** module API (`contracts/turn-loop-contract.md`) and the agent↔desktop **queue-state channel** (`contracts/queue-channel-contract.md`). |
| 4 | §IV Test Granularity & Cadence | PASS | Unit/contract tests (Vitest) per code change; large-test acceptance via testplan after the feature is complete. |
| 5 | §V Read Before Code | PASS (deferred to tasks.md) | tasks.md will list, per phase, the code-spec docs (`style/javascript.md`, `style/api.md`, `style/large_test.md` + their external refs) and the LangGraph/opencode docs cited here. |
| 6 | §VI Large Test Acceptance for Services | PASS (plan) | This is a service/desktop feature; a large test (deploy→run→cleanup via `guitar run`) covering queue-while-running end-to-end MUST pass for acceptance. |

No unjustified violations. Complexity notes in **Complexity Tracking** below.

## Project Structure

### Documentation (this feature)

```text
specs/030-queued-chat-input/
├── plan.md              # This file
├── research.md          # Phase 0 — LangGraph-native queue mechanism + decisions
├── data-model.md        # Phase 1 — TurnLoop / QueuedMessage / state transitions
├── quickstart.md        # Phase 1 — runnable validation scenarios
├── contracts/
│   ├── turn-loop-contract.md          # internal: SessionAgent/AgentAdapter turn-loop + queue API
│   └── queue-channel-contract.md      # agent↔desktop: queue-state signal over Connect
└── tasks.md             # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                                   # additive: QueueSignal FlowPart (queue-state push)
├── agent/src/
│   ├── llm.ts                                    # generalize content-block building for merged multi-part input
│   ├── session-agent.ts                          # owns the per-session TurnLoop; isRunning() status seam
│   ├── turn-loop.ts                              # NEW: the LangGraph-native queue+loop (buffer → streamEvents → drain → combine)
│   ├── handler.ts                                # route user frames to TurnLoop; emit wait only on full drain
│   ├── status-signal.ts                          # unchanged (pure fn); fed loop.isRunning() as isInFlight
│   └── *.test.ts                                 # unit/contract coverage
└── desktop/
    ├── app.go                                    # SendUserTurn remains non-blocking (no change to contract)
    └── frontend/src/
        ├── App.svelte                            # processing semantics across queued-turn boundary; queueCount wiring
        ├── components/ChatView.svelte            # remove disabled={processing}; pending-queue rendering
        └── api.ts                                # (no change unless proto field surfaces to frontend)
```

**Structure Decision**: The feature touches the existing desktop-app + service layout. The one new module is `projects/game/agent/src/turn-loop.ts` (the LangGraph-native queue/loop), intentionally kept at the LangChain boundary (it wraps `AgentAdapter.generateTurn` = `streamEvents`). The `game.proto` change is purely additive (a new `QueueSignal` FlowPart). No new top-level directories.

## Complexity Tracking

> Constitution §II requires any complexity to be justified against a simpler alternative.

| Item | Why Needed | Simpler Alternative Rejected Because |
|------|-----------|--------------------------------------|
| New `turn-loop.ts` module (replaces per-frame mutex path) | The per-frame `acquireMutex→generateTurn→releaseMutex` cannot express "buffer during run, combine, auto-continue on `wait`" without ad-hoc patches. A dedicated loop is the LangGraph-native single-flight + drain unit. | A frontend-only queue that dispatches on `wait` was rejected by the user's directive ("LangChain native"); it also cannot atomically combine messages or guarantee order under reconnect. |
| Additive `QueueSignal` FlowPart in `game.proto` | The frontend must render pending messages and transition them to normal on consume (FR-008/FR-009); it cannot detect an auto-continued turn from the stream alone. | Frontend-optimistic-only tracking was rejected because it cannot satisfy FR-009 reliably (no signal that a queued message became the current turn). |
| Generalize `streamFromAgent`/`TurnContent` for multi-part merged input | Combining multiple queued messages (text + multiple screenshots) into one aggregated turn (FR-005) needs >1 image / multiple text blocks. | Keeping single-image `TurnContent` would silently drop screenshots when ≥2 queued messages carry images (violates FR-012). |

No further unjustified complexity introduced.
