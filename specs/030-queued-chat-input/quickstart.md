# Quickstart: Queued Chat Input During Agent Run

**Feature**: `specs/030-queued-chat-input` | **Date**: 2026-07-29

This is a **validation guide** (not implementation). It lists the runnable scenarios that prove the feature works end-to-end, with prerequisites, commands, and expected outcomes. Implementation detail belongs in `tasks.md`.

## Prerequisites

- Bazel workspace builds: `bazel build //projects/game/...`.
- Agent unit/contract tests pass: `bazel test //projects/game/agent/...` (Vitest `js_test`).
- A large-test plan exists under `testplan/` for queue-while-running (Constitution §VI) and is runnable via the testplan skill: `guitar run <plan.yaml>` (see `style/large_test.md`).
- References: behavior in [data-model.md](./data-model.md); interfaces in [contracts/turn-loop-contract.md](./contracts/turn-loop-contract.md) and [contracts/queue-channel-contract.md](./contracts/queue-channel-contract.md); decisions in [research.md](./research.md).

## Scenario 1 — Input is editable/submittable during a run (FR-001/FR-002)

**Goal**: prove the desktop input is no longer disabled while an agent turn is in progress, and that submitting does not disturb the in-flight turn.

1. Start a session and trigger a multi-step agent turn (e.g., an instruction that causes ≥2 tool calls).
2. While the "Agent is typing…" indicator is visible, type a follow-up and click Send.
3. **Expected**: the input accepted the text and the submission succeeded (no error); the in-flight turn continued unchanged — its streamed text/tool results completed as if nothing was queued.

## Scenario 2 — Queued message becomes the next turn automatically (FR-005/FR-006)

**Goal**: prove that on turn completion the queued message is fed to the LLM as the next turn without a second submission.

1. While a turn is in progress, submit exactly one follow-up message (Scenario 1).
2. Wait for the in-flight turn to finish.
3. **Expected**: a **new** turn begins automatically using the queued message; the agent's response addresses the queued message's content. Exactly one `wait` is seen at the very end (not between the two turns). (Unit-testable by asserting the TurnLoop emits blocks for turn 2 and a single terminal `wait` — see `turn-loop-contract.md` loop body.)

## Scenario 3 — Multiple queued messages combined into one turn, FIFO (FR-004/FR-005)

**Goal**: prove ≥2 queued messages are combined into a single aggregated turn in submission order.

1. While a turn is in progress, submit A, then B, then C.
2. Wait for the in-flight turn to finish.
3. **Expected**: exactly **one** next turn begins whose combined input contains A, B, C in that order (one aggregated `HumanMessage`, multi content blocks — `research.md` D3); the agent's single response addresses all three. `QueueSignal.queued_count` goes 1→2→3 on submit, then 0 when the combined turn starts.

## Scenario 4 — Queue visibility & consume transition (FR-008/FR-009)

**Goal**: prove the pending queue is visible and items transition to normal on consume.

1. While a turn is in progress, submit a message; observe the queue indicator and the pending visual state.
2. Wait for the turn to complete and the queued message to be consumed.
3. **Expected**: the indicator reflected the queued count (driven by `QueueSignal`), and the pending message transitioned into the normal conversation view when consumed.

## Scenario 5 — Abort discards the queue (FR-011)

**Goal**: prove an explicit abort clears the queue (no auto-continue after abort).

1. While a turn is in progress with ≥1 queued message, trigger the stop control (or drop the stream).
2. **Expected**: the in-flight turn aborts, the queued message(s) are discarded (not delivered), and a single `wait` returns the desktop to ready. `QueueSignal.queued_count` → 0.

## Scenario 6 — Delivery failure retains the queue (FR-015)

**Goal**: prove a turn error at hand-off does not drop queued messages.

1. Force a `generateTurn` error at the turn boundary with a queued message present (e.g., inject a failing adapter in a unit test, or a transient backend fault in the large test).
2. **Expected**: a visible `warn`/error is surfaced; the queued message is **retained** (not lost); the user can recover without retyping. (Retry strategy is implementation-defined.)

## Scenario 7 — Status reconciliation unchanged (spec 021)

**Goal**: prove no regression in ACTIVE/IDLE status.

1. Probe status (desktop re-entry / connectivity probe) during a run and while queued work drains.
2. **Expected**: `STATUS_ACTIVE` while the TurnLoop is RUNNING (in-flight **or** draining); `STATUS_IDLE` when fully idle. (Derived by the unchanged pure `deriveStatusSignal`, fed `turnLoop.isRunning()` — `research.md` D5.)

## Large-test acceptance (Constitution §VI)

The service/desktop feature MUST be accepted via the testplan skill: author a `testplan/*.yaml` covering Scenarios 1–7 (deploy → run → cleanup) and run `guitar run <plan.yaml>`; **all cases must pass**. Build-only checks do not constitute acceptance.

## Unit/contract coverage (per code change, Constitution §IV)

- `turn-loop.test.ts`: submit-while-idle starts loop; submit-while-running buffers + emits `QueueSignal`; drain combines into one `HumanMessage` FIFO; empty buffer ⇒ single terminal `wait`; abort clears buffer + emits `wait`; non-abort error retains buffer.
- `handler`/`session-agent` tests recast the existing FIFO serialization test to assert loop behavior (no concurrent turns; combined/order preserved).
- `llm` test: multi-part aggregated `HumanMessage` builds content blocks in order (text + multiple images + size annotations).
