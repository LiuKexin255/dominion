# Implementation Plan: Fix Agent Service Crash on Desktop Disconnect

**Branch**: `026-agent-abort-crash-fix` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/026-agent-abort-crash-fix/spec.md`

## Summary

Fix the agent service process crash (restart) that occurs when a desktop
disconnects during an in-flight agent turn — a regression introduced by
`specs/023-saolei-mcp-refine/` Phase 2/3.

The root cause (confirmed by reading the installed
`@langchain/langgraph@1.4.8` source — see [research.md](./research.md)) is
**unprotected `stream.write()` calls in `handler.ts`'s catch block**.
When the bidi gRPC stream closes (peer gone) but `controller.signal.aborted`
is still `false` in a race window, the catch block's `else`-branch calls
`stream.write(warnFrame)` / `stream.write(waitFrame)`, which throw on the
closed stream. The throw escapes the catch block (it's thrown from inside
the catch body), propagates out of the `stream.on("data", async ...)`
async listener, becomes an unhandled promise rejection, and triggers
Node.js's default `process.exit(1)`.

023's `mergeIterables` flush-then-throw pattern **widens the race window**
by yielding buffered blocks (each triggering a `stream.write()`) during
the abort-propagation delay, making the crash practically reproducible
where 017's single-consumer path only had a theoretical narrow race.

The fix has three layers (research.md §E, decisions D1–D4):

1. **Guard all `stream.write()` calls** in the data callback so a
   write-failure on a closed stream is caught and logged, never thrown
   (D1/D2/D3). This closes the crash vector.
2. **Register a global `unhandledRejection` handler** in `bootstrap.ts`
   that logs but does not exit — defense-in-depth against future
   regressions of the same category (D4).
3. **Preserve 023's concurrent stream consumption model** unchanged —
   the bug is in the error-handling perimeter, not the architecture (D5).

## Technical Context

**Language/Version**: TypeScript (ESM), Node.js. Toolchain via Bazel +
Gazelle; `pnpm` for JS deps. See
[`projects/game/agent/tsconfig.json`](../../../projects/game/agent/tsconfig.json)
and
[`projects/game/agent/BUILD.bazel`](../../../projects/game/agent/BUILD.bazel).

**Primary Dependencies**:
- `@langchain/langgraph` `^1.4.8` (catalog) — `GraphRunStream`,
  `StreamMux`, `StreamChannel`; the abort-throw behavior investigated in
  [research.md §A](./research.md).
- `@grpc/grpc-js` (catalog) — bidi streaming; `ServerWritableStream.write()`
  on a closed stream throws or emits `'error'` (research.md §B).
- `vitest` (catalog) — unit test runner.

**Storage**: `MemorySaver` checkpointer (in-process). No storage changes.

**Testing**: `vitest` via
[`projects/game/agent/run_vitest.mjs`](../../../projects/game/agent/run_vitest.mjs).
Existing tests in `*.test.ts` co-located with sources. Large tests via
the `testplan` skill (`style/large_test.md`).

**Target Platform**: Linux server (gRPC service deployed via Bazel/oci).

**Project Type**: gRPC microservice (the "agent" half of the game
session agent ↔ desktop bidi pair).

**Performance Goals**: No new latency target. The fix adds only
try-catch guards around existing `stream.write()` calls — zero overhead
on the happy path.

**Constraints**:
- Must not change 023's concurrent stream consumption model (FR-008).
- Must restore 017's full graceful-abort behavior (FR-003..FR-007).
- Must not change the normal-turn happy path (FR-006 / SC-004).

**Scale/Scope**: 2 source files touched (`handler.ts`,
`bootstrap.ts`), ~1 new helper function (`safeWrite`), ~1 new global
handler. No new modules, no proto changes, no dependency changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` (v1.3.0). All
principles satisfied.

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **I — Citation & Provenance** | PASS | Every external fact cites the installed LangGraph source file + line, Node.js docs, or PR. research.md §A-§D carries full provenance. |
| 2 | **II — Refactoring Over Patching** | PASS | The fix targets the actual gap (unguarded writes in the error-handling perimeter), not a workaround bolted onto the stream-consumption core. 023's `mergeIterables` / `consumeToolResults` design is preserved (research.md D5). The global handler is defense-in-depth, not a patch. |
| 3 | **III — Interface-First Design** | PASS | No new external interface. The `safeWrite` helper is an internal utility whose contract is documented in [contracts/stream-abort-contract.md](./contracts/stream-abort-contract.md). The global `unhandledRejection` handler behavior is documented in the same contract. |
| 4 | **IV — Test Granularity & Cadence** | PASS (planned) | Compile (`bazel build`) + unit (`bazel test`) per code-change task. Large test (disconnect-doesn't-crash) as acceptance via testplan skill. |
| 5 | **V — Read Before Code** | PASS (planned) | `tasks.md` will declare per-phase reading lists in the three mandatory categories. |
| 6 | **VI — Large Test Acceptance for Services** | PASS (planned) | Agent is a service; large test MUST be executed via `guitar run` with all cases passing. Existing `projects/game/testplan/system_test.yaml` suites are the acceptance vehicle (no new suite needed — the disconnect-during-turn case fits `checkpoint-resume`). |

## Project Structure

### Documentation (this feature)

```text
specs/026-agent-abort-crash-fix/
├── plan.md              # This file
├── research.md          # Phase 0 — LangGraph abort behavior, crash chain, decisions D1-D5
├── data-model.md        # Phase 1 — safeWrite contract, global handler semantics
├── quickstart.md        # Phase 1 — validation scenarios 1..5
├── contracts/
│   └── stream-abort-contract.md  # Phase 1 — safeWrite + unhandledRejection handler contract
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/agent/src/
├── handler.ts            # 修改 — guard all stream.write() calls with safeWrite;
│                         #   add safeWrite helper (catches write errors on closed stream)
├── bootstrap.ts          # 修改 — register process.on("unhandledRejection") handler
│                         #   (logs, does not exit)
└── handler.test.ts       # 修改 — add "stream.write on closed stream does not crash" test;
                          #   add "disconnect during turn does not emit frames to dead peer" test
```

**Structure Decision**: single-service fix — all changes are inside the
existing `projects/game/agent/src/` directory. No new modules, no new
files (the `safeWrite` helper is a private function in `handler.ts`).
The fix is a behavior-quality patch to existing files.

## Design

### `safeWrite` helper

A private helper in `handler.ts` that wraps `stream.write()` in a
try-catch. On a closed/destroyed stream, the write error is logged at
`warn` level (not `error` — a write failure on a disconnecting stream
is expected, not an anomaly) and swallowed. This prevents the write
error from propagating as an unhandled rejection.

```text
safeWrite(stream, frame, sessionId):
    try:
        stream.write(frame)
    catch err:
        warn("stream write failed (peer disconnected?)", { sessionId, error: String(err) })
```

See [contracts/stream-abort-contract.md](./contracts/stream-abort-contract.md) §1.

### Global `unhandledRejection` handler

In `bootstrap.ts`, after the OTel/reporter initialization, register:

```text
process.on("unhandledRejection", (reason) => {
    error("unhandled promise rejection", { reason: String(reason) });
    // Do NOT process.exit(1) — a single rejection must not terminate
    // all sessions on a multi-session server.
});
```

This is defense-in-depth: even if D1/D2/D3 miss a future code path, the
service stays alive. The rejection is logged for diagnosis.

See [contracts/stream-abort-contract.md](./contracts/stream-abort-contract.md) §2.

### Change classification table (Constitution §II)

| # | File | Change | Class | Existing-design verdict (§II) |
|---|---|---|---|---|
| 1 | `handler.ts` `safeWrite` helper | Add a private function that wraps `stream.write()` in try-catch, logging write failures at `warn` level. | **新增** | The handler's design (stream lifecycle + per-turn driving) is unchanged. `safeWrite` is a natural utility for a handler that writes to a stream whose peer can disconnect at any time — it formalizes what the catch block already attempts (don't crash on disconnect) but fails to fully achieve (the catch block's own writes are unguarded). |
| 2 | `handler.ts` all `stream.write()` in data callback | Replace every bare `stream.write(frame)` in the `stream.on("data", ...)` callback with `safeWrite(stream, frame, sessionId)`. Covers: status response (line 268), profile-mismatch warn/wait (348/357/372/386), think/text/toolCall/toolResult frames in the `for await` loop (434/446/466/490), post-loop wait (509), and **critically** the catch block's warn/wait (527/537). | **修改** | The writes themselves are unchanged in semantics (the same frames are written when the stream is healthy). The change only adds error containment so a write to a dead stream is a no-op + log rather than a process crash. The handler's design as a bidi-stream frame emitter is preserved. |
| 3 | `bootstrap.ts` global `unhandledRejection` handler | Register `process.on("unhandledRejection", ...)` that logs but does not exit. Placed after OTel init so the log is structured. | **修改** | The bootstrap's design (OTel init → server start → graceful shutdown) is unchanged. Adding a global rejection handler is a natural extension of the process-lifecycle responsibility — it is the safety net for any async operation in the service that might produce an unhandled rejection. The current absence of this handler is the gap that made the crash terminal rather than merely observable. |
| 4 | `handler.test.ts` | Add tests: (a) "safeWrite catches write error on closed stream"; (b) "disconnect during turn → catch block else-branch write does not crash"; (c) "disconnect during turn → no frames emitted to dead peer" (017 FR-004 parity). | **修改** | Tests must reflect the new error-containment perimeter. Existing test structure (stream lifecycle simulation) is reused. |

### What is NOT changing

- **`llm.ts` `streamFromAgent` / `mergeIterables` / `consumeToolResults`** — the concurrent stream consumption model is the correct design for 023's tool_call/tool_result live rendering (research.md D5). The bug is in the error-handling perimeter (handler.ts), not in the consumption core.
- **`operation-bridge.ts`** — the signal-aware dispatch and abort handling are unchanged.
- **Proto model** — no proto changes.
- **017's AbortController lifecycle** — `activeTurns`, `abortAllTurns`, per-turn controller creation/deletion, signal propagation through `generateTurn` → `streamEvents` — all preserved exactly.
- **handler.ts catch block logic** — the `if (controller.signal.aborted)` / `else` branching is preserved. The only change is that `stream.write()` calls inside both branches are guarded by `safeWrite`.
- **Normal turn path** — when the desktop stays connected, `safeWrite` delegates to `stream.write()` which succeeds; behavior is identical to today.

## Complexity Tracking

No Constitution Check violations. This section is intentionally empty.

## Constitution Check (post-design re-evaluation)

*Re-checked after producing research.md, data-model.md, contracts/,
quickstart.md.*

| Principle | Status | Notes |
|---|---|---|
| I — Citation & Provenance | PASS | Every design decision in research.md cites the installed LangGraph source (file + line), Node.js docs, or PR. |
| II — Refactoring Over Patching | PASS | The fix targets the real gap (unguarded writes) without changing the stream-consumption architecture. The global handler is defense-in-depth, not a patch masking the bug. |
| III — Interface-First Design | PASS | `safeWrite` and the global handler are documented in [contracts/stream-abort-contract.md](./contracts/stream-abort-contract.md) before implementation. |
| IV — Test Granularity & Cadence | PASS | quickstart Scenarios 1–3 are unit tests; Scenarios 4–5 are large-test cases. |
| V — Read Before Code | PASS (planned) | Deferred to `tasks.md`. |
| VI — Large Test Acceptance for Services | PASS (planned) | Large test via testplan skill; all cases must pass. |

No design change introduced a constitution violation.

## References

### Official Documentation

- [Node.js — `process` event: `'unhandledRejection'`](https://nodejs.org/api/process.html#event-unhandledrejection) — default behavior and the `--unhandled-rejections` flag.
- [Node.js — `EventEmitter.captureRejections`](https://nodejs.org/api/events.html#capture-rejections-of-promises) — explains why async listener rejections become unhandled rejections by default.

### Repositories (installed source)

- `@langchain/langgraph@1.4.8` `dist/stream/run-stream.js` — `GraphRunStream.output`, `#valuesDone.catch(() => {})`.
- `@langchain/langgraph@1.4.8` `dist/stream/stream-channel.js` — `StreamChannel.fail()` / `iterate().next()` throw behavior.
- `@langchain/langgraph@1.4.8` `dist/stream/mux.js` — `StreamMux.fail()`, `pump()`.
- `@langchain/langgraph@1.4.8` `dist/stream/transformers/messages.js` — `createMessagesTransformer.fail()`.

### Repository-Internal Sources

- `specs/017-agent-loop-graceful-abort/spec.md` / `research.md` / `plan.md` — the feature whose behavior is being restored.
- `specs/023-saolei-mcp-refine/plan.md` / `tasks.md` — the feature that introduced the regression.
- `projects/game/agent/src/handler.ts` — crash site (catch block lines 511-542).
- `projects/game/agent/src/bootstrap.ts` — missing `unhandledRejection` handler.
- `projects/game/agent/src/llm.ts` — `mergeIterables` flush-then-throw (preserved, not changed).
