# Quickstart: Agent Session Resync & Adapter Simplification

**Feature**: `021-agent-session-resync` | **Date**: 2026-07-24

Runnable validation scenarios that prove the feature works. Compile (`bazel build //...`) + unit tests (`bazel test //...`) run as part of each code-change task (not separately tasked). Cross-process reconnect scenarios are large tests executed via the `testplan` skill (`style/large_test.md`).

Design references: [research.md](research.md), [data-model.md](data-model.md), [contracts/agent-desktop-channel-contract.md](contracts/agent-desktop-channel-contract.md), [contracts/agent-session-lifecycle-contract.md](contracts/agent-session-lifecycle-contract.md).

## Prerequisites

- A built agent (`projects/game/agent`) and desktop (`projects/game/desktop`).
- A saolei-enabled agent profile (per `specs/018-saolei-mcp`) and a bound minesweeper window, for the cross-process scenarios.

## Scenario 1 — Status derivation reports ACTIVE/IDLE (unit)

**Covers:** data-model §1; channel contract §1; spec FR-001/FR-002.

1. Unit-test the agent status derivation with a fake `SessionAgent` + mutex holder.
2. Assert: with the session mutex held → `ACTIVE`; mutex free + adapter bound → `IDLE`; mutex free + no adapter → `UNSPECIFIED`.

**Expected:** the three branches map exactly as specified.

## Scenario 2 — Sink compare-and-delete prevents stale clobber (unit)

**Covers:** lifecycle contract §1; data-model §2; spec FR-005/FR-006.

1. Unit-test `OperationBridge`: `registerSink(A)` → `registerSink(B)` (B supersedes A) → `unregisterSink(handleA)` (stale close of A).
2. Assert: `this.sink` still equals B (A's cleanup was a no-op); a subsequent `dispatch` resolves via B, not `FAILED "desktop disconnected"`.
3. Also assert: `unregisterSink(handleB)` clears the sink; `dispatch` then resolves `FAILED "desktop disconnected"`.

**Expected:** stale cleanup never invalidates a fresh registration; genuine disconnect still stops dispatch.

## Scenario 3 — Profile guard rejects mismatched, non-fatal, returns to ready (unit)

**Covers:** lifecycle contract §3; data-model §5; spec FR-012a/b/c, SC-004.

1. Unit-test the `Connect` content handler with a fake stream + bound adapter (profile X): send a user-turn frame targeting profile Y.
2. Assert: a `WarnSignal` (naming X vs Y) **and** a `WaitSignal` are written; `acquireMutex` is never called; no adapter invocation.
3. Then send a user-turn frame targeting profile X: assert it is accepted normally (subsequent message works).

**Expected:** mismatch rejected with warn+wait, no mutex leak, no panic; matching turn then accepted.

## Scenario 4 — Adapter rebuilds only via Refresh (unit)

**Covers:** lifecycle contract §2; data-model §4; spec FR-012/FR-013/FR-014.

1. Unit-test `SessionAgent.getOrCreateAdapter`: build adapter for profile X; call `getOrCreateAdapter("Y", …)` while an adapter exists → assert the **same** cached adapter is returned (no rebuild, profile Y ignored).
2. Call `invalidateAdapter()` (Refresh) → `getOrCreateAdapter("Y", …)` → assert a **new** adapter built for Y.

**Expected:** no implicit switch; Refresh is the sole rebuild path.

## Scenario 5 — saolei_update result forwarded and rendered, display-only (integration)

**Covers:** channel contract §2; data-model §3; spec FR-008/FR-009/FR-010, SC-003.

1. In an agent integration test (or scripted turn) that drives `saolei_update`: assert the agent emits a display-only `ToolResultPart` via `pushResult` (status `SUCCEEDED` on acceptance, `FAILED` on a validation rejection, with a self-descriptive message).
2. Assert the desktop `recvLoop` appends the frame to the chat stream and does **not** call `handleInboundOperation` for it (no input action).

**Expected:** the update is visible as a result card; no input is performed for it.

## Scenario 6 — Reconnect resilience end-to-end (large test, testplan)

**Covers:** spec User Stories 1 & 2, SC-001/SC-002/SC-005; research.md D1/D3.

A `testplan/` large test under the `testplan` skill (`style/large_test.md`):

1. Start a saolei session, begin a turn, then have the desktop **leave and re-enter** the session (close + reopen the agent connection).
2. Assert: the "Agent is typing…" indicator is cleared after re-entry (status ping-pong returned IDLE).
3. Start a new turn and call a saolei operation tool (`saolei_click`): assert it dispatches to the desktop and returns `SUCCEEDED` (not `FAILED`).

**Expected:** no stuck typing indicator; no spurious FAILED tool results across an exit→re-enter cycle. Confirm via tracing (signoz): no `dispatch` resolving `FAILED "desktop disconnected"` on the post-reconnect stream while the desktop is connected.

## Scenario 7 — Profile switch via Refresh only (large test, testplan)

**Covers:** spec User Story 4, SC-004.

1. Large test: run a session under profile A; attempt a turn under profile B **without** Refresh → assert the turn is rejected with a profile-mismatch warning and the desktop returns to ready.
2. Trigger Refresh, then run a turn under profile B → assert the adapter rebuilt with B's tools/model.

**Expected:** profile changes take effect only through Refresh; mismatched turns are rejected cleanly.

---

## Verification commands (per AGENTS.md)

```bash
bazel build //...                                    # compile
bazel test //...                                     # unit + integration
# large tests via the testplan skill (style/large_test.md)
```
