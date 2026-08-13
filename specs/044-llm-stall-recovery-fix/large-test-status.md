# Large-Test Execution Status & Blockers (Feature 044)

**Created**: 2026-08-13 | **Feature**: [spec.md](spec.md) | **Tasks**: [tasks.md](tasks.md)

This file records the execution state of Feature 044's large-test acceptance
(Phase 5, T011/T012/T013), the blocker encountered, and the fallback/design
decisions made. Work is **paused** pending an external enabler (deploy-tool
config-file support — see "Resume plan"). When resuming, read this file plus
[tasks.md](tasks.md) Phase 5.

---

## 1. Overall progress

| Phase | Status | Commit |
|---|---|---|
| Phase 1 (T001) baseline | ✅ done (build + unit green) | — (verification only) |
| Phase 2 (T002) US1 — idle timeout default 30s→120s + 60s clamp + `STREAM_IDLE_TIMEOUT_EXPLICIT` | ✅ done | `f953e9a` |
| Phase 3 (T003/T004) US2 — `reasoning-timeouts.ts` floor + graph/server wiring | ✅ done | `8cf212e` |
| Phase 4 (T005–T008) US3 — partial-output persistence (checkpoint layer) | ✅ done | `1c13b4e` |
| docs — redesign interrupted marker as `PartCompletion` proto field | ✅ done (designer) | `c355f9d` |
| Phase 4 (T008a/T008b/T009) US3 — proto `PartCompletion` marker end-to-end (agent translate + desktop render) | ✅ done | `3531228` |
| Phase 5 (T010) FR-012 — proto WarnSignal comment reconciliation | ✅ done | `166683e` |
| docs — cancel US2 floor large-test case (untestable in stall deploy) | ✅ done (designer) | `87cb986` |
| Phase 5 (T011) large-test cases (b)/(c) + 60s re-baseline | ✅ code complete, gofmt clean, compiles | `fd81521` |
| Phase 5 (T012) large-test acceptance (`guitar run`) | ⛔ **BLOCKED** — see §2 | — |
| Phase 5 (T013) housekeeping + whole-tree final build/test | ⏸ pending T012 | — |

All agent + proto + desktop unit tests are green
(`bazel test //projects/game/agent:lib_test`, `//projects/game:game_test`,
`//projects/game/desktop/frontend:lib_test`; `dist` build green). Every
implementation phase passed developer → executor verify → reviewer → fix-loop.

---

## 2. T012 large-test blocker

**Suite**: `agent-stall` in `projects/game/testplan/system_test.yaml`
(deploy `projects/game/testplan/deploy_agent_stall.yaml`).

**First `guitar run ... --suite agent-stall` result** (env `game.ltum8zvw`):

| Case | Result | Duration | trace_id |
|---|---|---|---|
| `TestAgentStallRecoveryWithQueuedMessage` (043 existing) | ✅ PASS | 60.35s | `1d308791f8c2527056dc539d24a7ddbf` |
| `TestAgentStallToolExecutionNotFalselyDetected` (043 existing, heartbeat) | ❌ **FAIL** | 65.19s | `843f54736610e7cee177f6646a75b7cc` |
| `TestAgentStallDetectedWithinConfiguredWindow` (044 case b, NEW) | ✅ PASS | 60.10s | `f362c37bbae7c0a92e687615cb60740d` |
| `TestAgentStallPersistsPartialOutput` (044 case c, NEW) | ✅ PASS | 60.10s | `f69055189ecdf9358f1ed3e8d1e84a23` |

**Failure**: `agent_stall_test.go:323` —
"turn terminated with a wait frame while the saolei tool was executing
> idleTimeout (false stall, FR-003/SC-003)". A `NodeTimeoutError(idle)` fired
during the tool wait that the client-side heartbeat (`withIdleHeartbeat`) was
supposed to prevent.

**Key observation**: both NEW 044 cases (b) and (c) **PASS**. The failure is in
an **existing 043 heartbeat test**, triggered by the 60s re-baseline.

### Re-baseline context (the trigger)

`deploy_agent_stall.yaml` previously set `GAME_STREAM_IDLE_TIMEOUT_MS: "15000"`
(below FR-001's new 60s minimum → T002's clamp silently changes it to 120s,
breaking the old 15s timing). T011 re-baselined:
- deploy env `15000` → `60000` (the 60s minimum).
- `stallToolReplyDelay` 20s → 65s (must exceed the new 60s window to keep the
  heartbeat validation meaningful).
- `wsReadTimeout` 30s → 75s (must exceed the 60s stall window).

The heartbeat test delays the desktop reply by `stallToolReplyDelay` (65s) and
asserts NO warn/wait frame appears during the wait (the heartbeat, firing every
`TOOL_HEARTBEAT_INTERVAL_MS=10s`, should keep the node's idle timer alive past
the 60s window). At the **old** 15s window + 20s delay this passed (a single
heartbeat at T=10s refreshed the 15s timer past the 20s reply). At the **new**
60s window + 65s delay it FAILED — a false stall fired around the 60s mark.

### Root cause (NOT yet confirmed — trace query was interrupted)

Hypothesis: the heartbeat-based idle-timer refresh does **not** reliably keep
the node's `idleTimeout` alive across a ~60s tool wait at the 10s heartbeat
cadence (it worked at the 15s scale where only one refresh was needed). The
failing trace `843f54736610e7cee177f6646a75b7cc` (service `game/agent`,
env `game.ltum8zvw`) has **not been inspected** — when work resumes, query it
via the signoz skill to confirm whether:
(a) the heartbeat events stopped firing / stopped refreshing the timer before
    60s (a real agent bug that 043's 15s window masked), or
(b) the test simply needs more controlled config values than the clamped 60s
    minimum allows (a test-design limitation).

### Why this is hard to fix without an external enabler

The deploy hardcodes the idle-timeout env on the agent container; the only
values that survive T002's 60s clamp are `>= 60s`, forcing every stall/heartbeat
test to wait ≥60s wall-clock and pinning the heartbeat test to a window where
the heartbeat appears unreliable. There is **no way to give the test a shorter,
controlled idle window or a controlled heartbeat interval** through the current
deploy mechanism. This is the core problem blocking reliable, fast large tests
for the stall-recovery feature.

---

## 3. Fallback / design decisions made during execution

These are settled decisions (with rationale) — record so they are not re-litigated
when work resumes.

1. **interrupted marker carries via a proto field, not lenient JSON.** The
   original design (desktop-rendering-contract.md §3) assumed the marker could
   ride a "lenient JSON channel" to the desktop. This was proven **infeasible**:
   every hop on the agent→proxy→gateway→desktop-Go wire path strips undeclared
   fields (proto-loader serialize, grpc-go, grpc-gateway protojson, desktop
   `internal/api/client.go:312` `DiscardUnknown`, `view_model.go:222-234` strict
   `protojson.Marshal`). Resolution (user-authorized): add a formal
   `PartCompletion` enum (`PART_COMPLETION_UNSPECIFIED=0`,
   `PART_COMPLETION_INTERRUPTED=1`) as field 2 on `TextPart`/`ThinkingPart`
   (`game.proto`). This is a controlled exception to FR-010's no-proto-wire-change
   boundary (documented in [plan.md](plan.md) "FR-010 controlled exception").
   Forward-compatible (protojson omits the zero value). Two-layer architecture:
   checkpoint layer `additional_kwargs.interrupted` (agent-internal, survives
   MemorySaver serde) → `handler.ts` translates → wire layer `completion` field.
   Commits `c355f9d` (docs) + `3531228` (impl).

2. **US2 reasoning floor large-test case (T011 case (a)) CANCELLED.** In the
   stall deploy the env is explicit (`60000`); per FR-003/US2.3 explicit env
   overrides the floor, so the floor never raises the effective timeout and is
   untestable there. fake-llm also has no recoverable-silence template. US2
   floor correctness stays validated at the unit level (T003
   `resolveStreamIdleTimeout` + T004 graph-node `idleTimeout === 600_000`).
   Commit `87cb986` (docs). See [tasks.md](tasks.md) Phase 5 T011 case (a) for
   the full rationale.

3. **FR-005 marking rule = overall-last streamed block (designer ruling A).**
   The interrupted flag is placed on the merged text/reasoning block ONLY when
   the overall-last streamed `TurnBlock` (filtered to the stalled node) is a
   text/reasoning block; a tool-gap turn (last block tool_call/tool_result)
   marks nothing (FR-005: fully-streamed blocks MUST NOT be marked). This
   corrected the earlier `tailKind` in-loop tracking which falsely marked
   pre-tool text. Documented in
   [contracts/partial-output-contract.md](contracts/partial-output-contract.md)
   §3 "Marking rule" + §4 "Tool-gap case"; implemented in `mergePartialBlocks`
   (`session-team.ts`).

---

## 4. Pending decision: SC-005 (needs spec owner)

[spec.md](spec.md) **SC-005**: "The production `saolei` template completes a
full game session without a stall-induced interruption caused by the reasoning
model's normal thinking time, **in the large-test validation**."

The designer determined SC-005's large-test component is **unsatisfiable in the
current topology** (fake-llm has no recoverable-silence template; the explicit
deploy env suppresses the floor). `spec.md` was deliberately **not modified**.
Three options were identified — **a decision is still needed**:

- **(α)** Accept unit-level substitution (recommended by designer) + add a note
  to spec.md Assumptions. (tasks.md/quickstart.md already updated toward this.)
- **(β)** Revise SC-005 wording.
- **(γ)** Invest in an env-unset deploy variant (high cost; would essentially
  test LangGraph's honor-the-timeout contract).

Note: this decision is independent of the T012 heartbeat blocker and of the
deploy-tool config-file enabler. Whichever option is chosen does not affect
cases (b)/(c).

---

## 5. Resume plan (after deploy-tool config-file support lands)

The user's plan: add **config-file support to the deploy tool** so large tests
can use controlled config values (shorter/controlled idle timeouts, heartbeat
intervals) instead of being locked to the clamped 60s minimum. Once that lands:

1. **Confirm the T012 heartbeat-failure root cause** by inspecting trace
   `843f54736610e7cee177f6646a75b7cc` (signoz skill, service `game/agent`).
   Determine whether it is (a) a real agent heartbeat bug or (b) a test-config
   limitation.
2. **Re-author the stall/heartbeat large-tests to use controlled config** enabled
   by the deploy-tool config-file support (e.g., a short idle window + matching
   heartbeat cadence that makes the heartbeat test fast and reliable). Revisit
   the 60s re-baseline in T011 (`stallToolReplyDelay`, `wsReadTimeout`,
   `deploy_agent_stall.yaml` env) — these may be replaced by config-driven
   values.
3. **Re-run `guitar run projects/game/testplan/system_test.yaml --suite agent-stall`**
   (T012) until ALL cases pass (Constitution VI — full green required).
4. **T013 housekeeping**: confirm proto regen, `gazelle`, `go mod tidy`,
   `bazel mod tidy`; final `bazel build //...` + `bazel test //projects/game/agent/...`
   (+ desktop build) for whole-tree green.
5. **Resolve SC-005** (§4) with the spec owner.
6. Mark all tasks `[X]` in [tasks.md](tasks.md) and deliver the completion report.

### Files most likely to change on resume
- `projects/game/testplan/deploy_agent_stall.yaml` (env → config-driven values)
- `projects/game/testplan/agent_stall_test.go` (timing constants + possibly the
  heartbeat test assertions once the root cause is confirmed)
- `projects/game/testplan/helpers_test.go` (`wsReadTimeout` etc.)
- Possibly `projects/game/agent/src/llm.ts` heartbeat/idle constants if (a)
  turns out to be a real bug.

### Unchanged on resume (do NOT redo)
- Phases 1–4 implementation (US1/US2/US3) — done, committed, unit-green.
- The `PartCompletion` proto-field design (decision #1).
- US2 floor large-test cancellation (decision #2).
- FR-005 marking rule (decision #3).
- Phase 5 T010 (proto comment) — done.
