# Implementation Plan: Agent Loop Graceful Abort on Desktop Disconnect

**Branch**: `017-agent-loop-graceful-abort` | **Date**: 2026-07-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/017-agent-loop-graceful-abort/spec.md`

## Summary

Replace the custom throw-based disconnect-termination mechanism in the
`projects/game/agent/` service with the official LangChain JavaScript
cancellation contract: pass an `AbortSignal` via `RunnableConfig.signal`
([`RunnableConfig.signal` reference](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal))
into `agent.streamEvents()`, and abort it when the desktop bidi stream
disconnects. This stops the in-flight LLM stream, cancels the underlying
HTTP request, and leaves the conversation checkpoint intact for reconnect
— uniformly across all turn phases, without the error frames the current
throw path produces.

The change deletes the `ToolAbortOnDisconnect` middleware, the
`DesktopDisconnectedError` class, and the `hasSink()` method; modifies
`AgentAdapter.generateTurn` and `OperationBridge.dispatch()` to accept an
optional `AbortSignal`; and modifies the `Connect` handler to create and
abort a per-turn `AbortController`. Full research and rationale are in
[research.md](./research.md).

## Technical Context

**Language/Version**: TypeScript (ESM), Node.js. Toolchain via Bazel +
Gazelle; `pnpm` for JS deps. See
[`projects/game/agent/tsconfig.json`](../../../projects/game/agent/tsconfig.json)
and
[`projects/game/agent/BUILD.bazel`](../../../projects/game/agent/BUILD.bazel).

**Primary Dependencies**:
- `@langchain/langgraph` `^1.4.4` (catalog) — `createAgent`, `Pregel`,
  `streamEvents` with `signal` support via `combineAbortSignals()`.
- `@langchain/core` `^1.2.0` (catalog) — `RunnableConfig.signal`
  ([type definition](https://github.com/langchain-ai/langchainjs/blob/cc39f564a9df18655ee4ae92de3f32e251021db8/libs/langchain-core/src/runnables/types.ts#L101-L107)).
- `langchain` `^1.5.0` (catalog) — `createAgent` v1 API.
- `@grpc/grpc-js` (catalog) — bidi streaming for the desktop `Connect`
  RPC.
- `vitest` (catalog) — unit test runner.

**Storage**: `MemorySaver` checkpointer (in-process, per
[`session-agent.ts`](../../../projects/game/agent/src/session-agent.ts)).
No external storage introduced by this feature.

**Testing**: `vitest` via
[`projects/game/agent/run_vitest.mjs`](../../../projects/game/agent/run_vitest.mjs).
Existing tests in `*.test.ts` co-located with sources.

**Target Platform**: Linux server (gRPC service deployed via Bazel).

**Project Type**: gRPC microservice (the "agent" half of the game
session agent ↔ desktop bidi pair).

**Performance Goals**: disconnect-to-turn-stop within seconds (spec
SC-001); no LLM tokens billed after abort.

**Constraints**: must not change the normal-turn happy path (spec
FR-007, SC-004); must not break reconnect-resume from checkpoint (spec
FR-006, SC-003).

**Scale/Scope**: 6 source files touched, ~3 deleted symbols, 1 interface
signature change, ~5 test files updated. No new modules.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: ✅ Every external fact in this plan
  carries an inline link (LangChain docs, langchainjs/langgraphjs source,
  PRs). The full source list is in `## References` below and in
  [research.md](./research.md). `tasks.md` will inherit these citations.
- **Version pins**: ✅ `@langchain/core` `^1.2.0`,
  `@langchain/langgraph` `^1.4.4`, `langchain` `^1.5.0` — all from the
  root catalog
  ([`pnpm-workspace.yaml`](../../../pnpm-workspace.yaml)). The
  cancellation API is stable in all three.
- **Public accessibility**: ✅ All cited links resolve to public
  LangChain docs or GitHub.
- **Code Style Precedence (§II)**: ✅ Noted for `tasks.md` — every
  implementation task MUST reference `style/` guidelines (specifically
  the TypeScript style doc) before code changes begin. The
  [`style/`](../../../style/) directory is the authoritative source.
- **External Dependency Research (§III)**: ✅ LangChain JS cancellation
  contract researched against official docs and source in
  [research.md §A](./research.md#a-langchain-javascript-cancellation-contract).
  Findings: `signal: AbortSignal` on `RunnableConfig` is the official
  primitive; `streamEvents` stops cleanly on abort (no throw to
  consumer); LLM HTTP is cancelled; completed-superstep checkpoints are
  preserved. Version pins recorded above.
- **Refactoring-Oriented Changes (§IV)**: ✅ Every change is classified
  新增 / 修改 / 删除 in the **Design** section below, with an explicit
  verdict on whether the existing design still serves the new goal.
  No "out of scope" deferrals.

## Project Structure

### Documentation (this feature)

```text
specs/017-agent-loop-graceful-abort/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── adapter-abort-contract.md
└── tasks.md             # Phase 2 output (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
projects/game/agent/src/
├── handler.ts              # 修改 — Connect: per-turn AbortController, abortAllTurns on stream end/error
├── llm.ts                  # 修改/删除 — AgentAdapter.generateTurn +signal; delete ToolAbortOnDisconnect middleware
├── operation-bridge.ts     # 修改/删除 — dispatch +signal, no-sink→FAILED; delete DesktopDisconnectedError + hasSink
├── session-agent.ts        # unchanged (bridge ownership unaffected)
├── mouse-tool.ts           # 修改 — pass signal through to dispatch (if dispatch gains signal param)
├── handler.test.ts         # 修改 — update disconnect tests for abort-based path
├── operation-bridge.test.ts # 修改/删除 — remove DesktopDisconnectedError tests, add signal-abort tests
├── llm.test.ts             # 修改 — add generateTurn-with-signal coverage
└── mouse-tool.test.ts      # 修改 — add dispatch-with-signal coverage
```

**Structure Decision**: single-service layout — all changes are inside
the existing `projects/game/agent/src/` directory. No new modules, no
new packages, no directory moves. The feature is a behavior refactor of
existing files.

## Design

### Cancellation flow

```text
Desktop bidi stream (handler.ts Connect)
  │
  ├─ user content frame → acquireMutex(sessionId)
  │     ├─ new AbortController()                    ← per-turn
  │     ├─ activeTurns.set(sessionId, controller)
  │     ├─ adapter.generateTurn(sid, content, controller.signal)
  │     │     └─ agent.streamEvents(input, { signal, ... })   ← LangChain cancels here
  │     └─ finally: activeTurns.delete(sessionId); releaseMutex(sessionId)
  │
  ├─ stream.on("end") / stream.on("error")
  │     ├─ abortAllTurns()   ← controller.abort() for every activeTurns entry
  │     └─ cleanupSinks()    ← existing unregisterSink for every activeSessions entry
  │
  └─ tool dispatch (operation-bridge.ts)
        └─ dispatch(part, signal?)   ← resolves FAILED on signal abort, no throw
```

The `AbortSignal` flows: `Connect` handler → `generateTurn(threadId,
content, signal)` → `streamFromAgent` → `agent.streamEvents(input,
{ signal })`. Per
[`Pregel.streamEvents()` source](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946),
LangGraph combines it with its internal abort controller via
`combineAbortSignals()`. When aborted, the LLM HTTP request is cancelled
and the `for await (const message of stream.messages)` loop exits
cleanly — no exception reaches the handler's catch block for the
disconnect case.

### Change classification table (Constitution §IV)

| # | File | Change | Class | Existing-design verdict (§IV) |
|---|---|---|---|---|
| 1 | `handler.ts` `Connect` | Add per-turn `AbortController` creation, stream-scoped `activeTurns: Map<string, AbortController>`, and `abortAllTurns()` invoked from `stream.on("end")` / `stream.on("error")` alongside the existing `cleanupSinks()`. Pass `controller.signal` into `adapter.generateTurn(...)`. | **修改** | The `Connect` handler's design — stream lifecycle + per-turn driving under a per-session mutex — still serves the goal. Abort-controller tracking is a natural extension of the turn-driving responsibility, not a new concern bolted on. The existing `activeSessions` set (sink tracking) and the new `activeTurns` map (abort tracking) are kept separate because they have different lifecycles: sink registration persists across turns on the same stream; abort controllers live only during a turn. |
| 2 | `handler.ts` catch block (L332-351) | Disconnect no longer arrives as a thrown `DesktopDisconnectedError`; it arrives as the `for await` loop exiting early. No change to the catch block's logic for *genuine* LLM errors, but the disconnect case no longer emits `warn`+`wait` frames (the stream is dead). | **修改** | The catch block was designed to surface LLM-processing failures as `warn` frames. That design still applies for real errors. The disconnect case was an accidental passenger via `DesktopDisconnectedError`; removing it returns the catch block to its original purpose. |
| 3 | `llm.ts` `AgentAdapter.generateTurn` signature | Add optional `signal?: AbortSignal` parameter. | **修改** | The interface was designed as a per-turn streaming boundary with `threadId` and `content` as the only variable inputs. Adding `signal` is a natural extension — it is another per-turn input that controls the turn's lifecycle. The interface's shape and intent remain coherent. See [contracts/adapter-abort-contract.md](./contracts/adapter-abort-contract.md). |
| 4 | `llm.ts` `AgentAdapterImpl.streamFromAgent` | Pass `signal` into the `agent.streamEvents(input, { ..., signal })` options object. | **修改** | The method already constructs the options object (`configurable`, `metadata`, `version`, `recursionLimit`). Adding `signal` is a one-line extension of the same options construction — no structural change. |
| 5 | `llm.ts` `ToolAbortOnDisconnect` middleware (L177-187) | Delete entirely. Remove from the `middleware` array at L198. | **删除** | This middleware was the proactive disconnect-detection side channel. With cancellation moved to the `AbortSignal`, it is fully redundant. The design it served (intercept tool calls to check desktop connectivity) is superseded by the signal-driven orchestration shutdown. Keeping it would mean two termination mechanisms for the same event — exactly the parallel code path §IV forbids. |
| 6 | `llm.ts` import of `DesktopDisconnectedError` (L18) | Delete the import. | **删除** | Sole runtime import of a class that is being deleted (change #8). |
| 7 | `operation-bridge.ts` `dispatch()` throw (L156-161) | Remove the `throw new DesktopDisconnectedError(...)` when `!sink`. Replace with `return { status: STATUS_FAILED, message: "desktop disconnected" }`. | **修改** | `dispatch()` was designed as a sink-dispatch correlation layer. The throw was a disconnect-detection side channel bolted onto it. With cancellation moved to the signal, a no-sink dispatch returns a FAILED result (consistent with the existing timeout and invalid-part paths) rather than throwing. The method's design as a result-correlation layer is restored. |
| 8 | `operation-bridge.ts` `dispatch()` signal awareness | Add optional `signal?: AbortSignal` parameter. When the signal aborts, resolve the pending dispatch with FAILED (`message: "aborted"`), clear the timer, and remove the map entry. | **修改** | Natural extension of the existing timeout-based cleanup. The pending-dispatch map and timer pattern already exists; adding an abort-signal listener is the same cleanup path triggered by a different event. See [contracts/adapter-abort-contract.md](./contracts/adapter-abort-contract.md). |
| 9 | `operation-bridge.ts` `DesktopDisconnectedError` class (L70-75) | Delete the class. | **删除** | No throwers remain after changes #5 and #7. Verified: no runtime callers outside `projects/game/agent/src/` (see [research.md §B.5](./research.md)). The class is fully dead code. |
| 10 | `operation-bridge.ts` `hasSink()` method (L131-133) | Delete the method. | **删除** | Only caller was the `ToolAbortOnDisconnect` middleware at `llm.ts:180` (being deleted in #5). No other runtime caller. Fully dead code. |
| 11 | `mouse-tool.ts` `createMouseMoveTool` / `createMouseClickTool` | If `dispatch()` gains a `signal` parameter (change #8), pass through the signal available in the LangChain tool context. | **修改** | The tools are thin wrappers around `bridge.dispatch()`. Passing the signal through is the minimal change to give `dispatch()` abort awareness. The tool's design as a dispatch wrapper is unchanged. |
| 12 | `handler.test.ts` | Update disconnect tests: replace `DesktopDisconnectedError`-throw assertions with abort-signal assertions. Add test: "stream end aborts in-flight turn via AbortController". | **修改** | Tests must reflect the new cancellation path. Existing test structure (stream lifecycle simulation) is reused. |
| 13 | `operation-bridge.test.ts` | Delete the `DesktopDisconnectedError`-throw test (L80-86) and `hasSink()` test (L115-121). Add: "dispatch with aborted signal → FAILED", "dispatch with no sink → FAILED (no throw)". | **修改/删除** | Tests for deleted symbols are removed; tests for new behavior are added. |
| 14 | `llm.test.ts` | Add: "generateTurn respects AbortSignal — stream stops on abort". | **修改** | New coverage for the signal-passthrough path. |
| 15 | `mouse-tool.test.ts` | Add: "dispatch signal abort propagates to tool result". | **修改** | New coverage for the signal-passthrough in tools. |

### What is NOT changing

- **`SessionAgent` / `SessionAgentStore`** — bridge ownership and
  per-session adapter caching are unaffected. The bridge still survives
  stream reconnects.
- **`OperationBridge.registerSink()` / `unregisterSink()`** — still
  needed for stream lifecycle signaling. `cleanupSinks()` in `handler.ts`
  still calls `unregisterSink()` on stream end/error.
- **`activeSessions` Set in `handler.ts`** — still tracks which sessions
  have a sink registered on this stream, for `cleanupSinks()`. Kept
  separate from the new `activeTurns` map (different lifecycles).
- **Per-session mutex** — unchanged. The `finally` block still calls
  `releaseMutex(sessionId)`.
- **Checkpointer (`MemorySaver`)** — unchanged. LangGraph's per-superstep
  checkpointing semantics guarantee that an aborted in-progress superstep
  is discarded while completed supersteps survive.
- **Normal turn path** — when the desktop stays connected, no signal is
  aborted; `generateTurn` runs to completion identically to today.

## Complexity Tracking

No Constitution Check violations. This section is intentionally empty.

## Constitution Check (post-design re-evaluation)

*Re-checked after Phase 1 design.*

- **§I (Citations)**: ✅ All design decisions cite LangChain official
  docs or source. The `AbortSignal` contract cites
  [`RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal).
  The `streamEvents` abort behavior cites
  [PR #9900](https://github.com/langchain-ai/langchainjs/pull/9900). The
  `combineAbortSignals` mechanism cites
  [`Pregel.streamEvents()` source](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946).
  Full list in `## References`.
- **§II (Style)**: ✅ Deferred to `tasks.md` — each implementation task
  will reference `style/` TypeScript guidelines before code changes.
- **§III (Dependency Research)**: ✅ LangChain JS cancellation contract
  fully researched in [research.md §A](./research.md). Versions pinned:
  `@langchain/core` `^1.2.0`, `@langchain/langgraph` `^1.4.4`,
  `langchain` `^1.5.0`. No version bump needed.
- **§IV (Refactoring)**: ✅ All 15 changes classified 新增/修改/删除 in
  the table above. Every 修改 and 删除 carries an explicit verdict on
  whether the existing design still serves the new goal. No "out of
  scope" deferrals. The two deletions (`ToolAbortOnDisconnect`,
  `DesktopDisconnectedError` + `hasSink()`) are the outdated design
  elements that this change removes to bring the codebase back into
  coherence with the signal-based cancellation model.

## References

### Official Documentation

- [LangChain JS — `RunnableConfig.signal`](https://reference.langchain.com/javascript/langchain-core/runnables/RunnableConfig/signal) — the official cancellation option reference. Stable since `@langchain/core` 0.1.x.
- [LangChain JS — Agents (streaming)](https://docs.langchain.com/oss/javascript/langchain/agents#streaming) — `createAgent` + `streamEvents` v3 usage.
- [LangGraph JS — Fault Tolerance](https://docs.langchain.com/oss/javascript/langgraph/fault-tolerance#graceful-shutdown) — confirms `AbortSignal` cancels in-flight work; `RunControl.requestDrain()` is cooperative-only and does not cancel async tasks.
- [LangChain v1 release notes](https://docs.langchain.com/oss/javascript/releases/langchain-v1) — `createAgent` as the v1 standard API.

### Repositories

- [`RunnableConfig` type — langchainjs `runnables/types.ts` L101-107](https://github.com/langchain-ai/langchainjs/blob/cc39f564a9df18655ee4ae92de3f32e251021db8/libs/langchain-core/src/runnables/types.ts#L101-L107) — `signal?: AbortSignal` definition. Pin: `@langchain/core` `^1.2.0`.
- [`PregelOptions` — langgraphjs `pregel/types.ts` L80](https://github.com/langchain-ai/langgraphjs/blob/aa6deb60c7bd0575f6d8629ed9023b944cedbc7b/libs/langgraph-core/src/pregel/types.ts#L80) — extends `RunnableConfig`, inheriting `signal`. Pin: `@langchain/langgraph` `^1.4.4`.
- [`Pregel.streamEvents()` — langgraphjs `pregel/index.ts` L1922-1946](https://github.com/langchain-ai/langgraphjs/blob/4468bdf5124454d8ceaa27f07a1e22e1c76212a5/libs/langgraph-core/src/pregel/index.ts#L1922-L1946) — `combineAbortSignals(options?.signal, abortController.signal)`.
- [PR #7917 — `orchestratorAbortBehavior`](https://github.com/langchain-ai/langchainjs/pull/7917) — three abort modes; default `"throw_immediately"` is what this feature relies on.
- [PR #9900 — provider call/stream abort signal handling](https://github.com/langchain-ai/langchainjs/pull/9900) — `stream()` returns early on abort (clean termination); `invoke()` throws `AbortError`.

### Articles & RFCs

- No external articles or RFCs cited.
