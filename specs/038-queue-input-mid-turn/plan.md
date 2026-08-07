# Implementation Plan: Queued Input Mid-Turn Injection & Bubble Continuity

**Branch**: `038-queue-input-mid-turn` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

**Revision**: 2026-08-06 — summary aligned with spec v2 (first-model-call injection point; FR-001/FR-004 amended to define "mid-turn delivery point" = turn's first reasoning step + tool-result boundaries).

**Input**: Feature specification from `specs/038-queue-input-mid-turn/spec.md`

## Summary

Two fixes to the queued-chat-input system (spec 030):

1. **Mid-turn injection** (FR-001..FR-004): Queued user messages are delivered to the agent at the earliest mid-turn boundary within a running turn — the turn's first model call (messages queued before the first reasoning step) or the model call immediately after the agent finishes processing tool results (tool-result boundaries). This is implemented via a `beforeModel` middleware on the player's `createAgent` that drains the `TurnLoop` buffer through a `configurable.drainQueuedInput` callback and injects the drained content as a `HumanMessage`. The existing turn-end drain (`runLoop` buffer check) is retained as a fallback for no-tool turns and last-boundary arrivals.

2. **Bubble continuity** (FR-005..FR-007): The frontend streaming merge logic in `App.svelte:handleMessageParts` is changed to search backwards past interleaved USER entries when finding the merge target for agent text/thinking chunks, so a queued user message no longer splits the agent's continuous bubble.

Reference: [opencode](https://github.com/anomalyco/opencode) steer delivery (`packages/core/src/session/input.ts` `promoteSteers`, `packages/core/src/session/runner/llm.ts` run loop) — behavioral inspiration only; dominion's LangGraph architecture differs.

## Technical Context

**Language/Version**: TypeScript (agent), Svelte 5 + TypeScript (desktop frontend)

**Primary Dependencies**:
- `langchain` (^pinned) — `createAgent`, `beforeModel` middleware (`langchain/dist/agents/middleware/types.d.ts`)
- `@langchain/langgraph` (^1.4.8) — StateGraph, MemorySaver, streamEvents
- `@langchain/core/messages` — HumanMessage, BaseMessage
- Svelte 5 runes (`$state`, `$derived`, `$effect`)

**Storage**: N/A (transient in-memory queue — no persistence change)

**Testing**: vitest (unit, via `vitest_test` Bazel macro — `style/javascript.md` §js_test 执行模型); testplan/guitar (large tests — `style/large_test.md`)

**Target Platform**: Linux server (agent gRPC service) + Windows desktop (Wails v2 WebView2)

**Project Type**: desktop-app (agent backend + Svelte frontend)

**Performance Goals**: Mid-turn injection adds zero latency — the `beforeModel` hook is synchronous and the drain is an array clear. No measurable overhead vs the existing turn-end-only drain.

**Constraints**: The injection MUST NOT disturb the in-flight `createAgent` loop (FR-012). The `beforeModel` hook returns a state update (`{ messages: [...] }`) that LangGraph applies through `messagesStateReducer` before the model call — this is LangGraph-native and does not interrupt tool execution.

**Scale/Scope**: Per-session, single-user. One `TurnLoop` per session, one player `createAgent` per session graph.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| **I. Citation & Provenance** | ✅ Pass | All references use repo-relative paths (`projects/game/agent/src/turn-loop.ts:356`) or full URLs (opencode source). No bare references. |
| **II. Refactoring Over Patching** | ✅ Pass | The `beforeModel` middleware is the natural LangGraph extension point for pre-model injection (already used by `gameEndGuard` in `player.ts:157-169`). `TurnLoop.drainQueue()` extends the existing loop's drain vocabulary rather than patching the loop body. The frontend merge-logic fix is a refinement of the existing `handleMessageParts` merge algorithm, not a workaround. No architectural mismatch — the change extends the existing design. |
| **III. Interface-First Design** | ✅ Pass | New internal interface: `TurnLoop.drainQueue(): TurnContent \| null` (contract in `contracts/turn-loop-drain-contract.md`). Injection seam: `configurable.drainQueuedInput` callback (contract in `contracts/injection-seam-contract.md`). Both are defined before implementation. |
| **IV. Test Granularity & Cadence** | ✅ Pass | Unit tests (`turn-loop.test.ts` for `drainQueue`, frontend merge logic) as part of code-change tasks. Large tests (testplan) for end-to-end mid-turn injection validation. |
| **V. Read Before Code** | ✅ Pass | Document lists will be in `tasks.md` (Phase 2). |
| **VI. Large Test Acceptance** | ✅ Pass | The agent is a service-type application. Large tests via testplan skill covering mid-turn injection end-to-end. |

**Gate result**: All gates pass. No violations requiring justification.

## Project Structure

### Documentation (this feature)

```text
specs/038-queue-input-mid-turn/
├── plan.md                         # This file
├── research.md                     # Phase 0 output
├── data-model.md                   # Phase 1 output
├── quickstart.md                   # Phase 1 output
├── contracts/
│   ├── turn-loop-drain-contract.md     # TurnLoop.drainQueue() interface
│   └── injection-seam-contract.md      # configurable.drainQueuedInput seam
└── tasks.md                        # Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
projects/game/
├── agent/src/
│   ├── turn-loop.ts                # NEW: drainQueue() method
│   ├── turn-loop.test.ts           # MODIFIED: drainQueue tests
│   ├── session-team.ts             # MODIFIED: runTeamTurn configurable adds drainQueuedInput
│   ├── team/player.ts              # MODIFIED: queueDrain beforeModel middleware
│   └── llm.ts                      # REFERENCED: buildContentBlocks (existing, reused)
└── desktop/frontend/src/
    ├── App.svelte                  # MODIFIED: handleMessageParts backward-scan merge
    └── components/ChatView.svelte  # REFERENCED: render logic (unchanged)
```

**Structure Decision**: No structural change — the feature modifies existing files in the existing `agent/src/` and `desktop/frontend/src/` layout. No new directories, no new modules.

## Complexity Tracking

> No constitution violations. Table intentionally empty.
