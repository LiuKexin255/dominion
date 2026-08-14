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
| Phase 5 (T012) large-test acceptance (`guitar run`) | ⛔ **BLOCKED** (historical — resolved 2026-08-14, see §7) | — |
| Phase 5 (T013) housekeeping + whole-tree final build/test | ⏸ pending T012 (historical — done 2026-08-14, see §7) | — |
| Phase 5A (T014–T017) service-config channel (`agent_timeouts`) + heartbeat tick logs | ✅ done | `50f8d4f` |
| Phase 5 (T011) stall-suite rescale (5s/2s/12s) + think-gap case (d) | ✅ done | `c4aae69` |
| Phase 5 (T018) env→config comments (system_test.yaml / helpers_test.go / README.md / BUILD.bazel) + size large→medium | ✅ done | `c4aae69` (+docs `e929b60`) |
| Phase 5 (T019) fake-llm `think-interrupt-gap` 90s→15s + 046 pin assertion sync | ✅ done | `c4aae69` (+docs `2328861`) |
| Phase 5 (T021) greeting keyword hygiene (T012 first-run fix) | ✅ done | `a84a6a2` (+docs `bd3c47a`) |
| Phase 5 (T012) large-test acceptance (`guitar run`) | ✅ **ALL GREEN** — 11/11 suites; agent-stall 5/5（首轮 4/5 → T021 修复 → 重跑全绿；另单独 suite 复跑全绿） | — (verification only) |
| Phase 5 (T013) housekeeping + whole-tree final build/test + status docs close-out | ✅ done | — (docs) |

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

**2026-08-14 close-out (working default α executed — final ruling still
pending)**: the resume scope completed under the **working default α**
([research.md](research.md) R11 — unit-layer substitution): SC-001/SC-005 are
validated at the unit layer by T003 (`resolveStreamIdleTimeout` /
`getReasoningIdleTimeoutFloor`) and T004 (graph-node `idleTimeout` application),
as recorded in [quickstart.md](quickstart.md) A4 ("SC-005 note"). Option **γ**
(floor large-test case, [tasks.md](tasks.md) T020) was **NOT executed** — it is
gated on the spec owner's ruling, which remains **pending**; the executor's
completion report requests the ruling confirmation from the spec owner. β was
not taken (spec.md wording untouched).

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

---

## 6. 2026-08-14 update — enablers landed, plan amended, resume scope defined

Both external enablers are complete and green on the current lineage (`046-fake-llm-think-chunking` carries all 044+045+046 commits; no separate 044 branch exists):

- **045 deploy-config** (44/44 tasks): service.yaml `configs` blocks + deploy selection + Go/JS SDK. See [spec](../045-deploy-config/spec.md), [runtime contract](../045-deploy-config/contracts/runtime-contract.md).
- **046 fake-llm think chunking** (15/15 tasks): `reasoning_chunks`/`chunk_delays`/`stall_after`; scenario-grouped testdata incl. `think-interrupt-gap` (resumable 90s mid-thinking gap at 046 ship time; 044 [tasks.md](tasks.md) T019 rescales it to 15s for the config-driven 5s window) in `projects/game/fake-llm/service/testdata/stall_recovery.yaml`. See [spec](../046-fake-llm-think-chunking/spec.md).

§5's resume plan has been designed into the amended [plan.md](plan.md) ("Update 2026-08-14 — Resume Scope"), with research ([research.md](research.md) R9–R11), contract ([contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1/§5), data model ([data-model.md](data-model.md) §7) and acceptance ([quickstart.md](quickstart.md) Phase D) updated, and spec.md amended (Clarifications Session 2026-08-14 + FR-008 config tier). Key deltas vs §5 as originally sketched:

1. **Root-cause query (§5 step 1) executed 2026-08-14**: trace `843f5473...` spans expired; surviving logs show only the ~65s teardown timeline, and the env contains **no heartbeat/stall logs at all** — (a)/(b) is NOT discriminable from stale data (R9). Resolution is procedural: per-tick heartbeat logging + a short-scale (5s/2s) re-run whose per-tick logs discriminate any recurrence.
2. **Config channel design settled** (R10): single test-grade block `agent_timeouts` (5s idle / 2s heartbeat), resolution env > config > default, config honored as-is (env-scoped 60s clamp unchanged), floor suppressed by any explicit tier; stall deploy drops the env and selects the block.
3. **`wsReadTimeout` needs NO rescale** (deadline ceiling, not a sleep) — §5's "Files most likely to change" list is narrower in the final design.
4. **SC-005 §4 remains open** with the spec owner; γ's cost dropped materially (R11: resumable-silence template exists; default topology already deployed by `deploy_agent.yaml`) and is pre-analyzed as optional item 6 in the resume scope. Working default stays α.

**Next action**: re-author Phase 5 tasks via `/speckit.tasks` from plan.md "Update 2026-08-14 — Resume Scope" (items 1–5, item 6 gated on the SC-005 ruling), then execute through T012 full-green.

---

## 7. 2026-08-14 close-out — T012 all-green, T021 root cause, R9 ruling

### T012 acceptance runs (Constitution VI — actual `guitar run`, deploy→test→cleanup)

| Run | Environment | Result | Notes |
|---|---|---|---|
| First run (pre-T021) | `game.lt5ho95n` | 10/11 suites green; agent-stall **4/5** | case (d) `TestAgentStallThinkInterruptGapDetected` failed — fake-llm keyword collision (see below), **not an agent regression**; heartbeat case PASSED 12.31s (R9 false stall did not recur) |
| Re-run (post-T021, full plan) | `game.ltxaldut` | **11/11 suites ALL GREEN** | agent-stall five cases PASS: T1 5.35s / T2 (heartbeat) 12.31s / T3 5.17s / T4 5.09s / T5 5.10s; suite 33.1s — well within the medium size budget (300s) |
| Isolated re-confirmation (`guitar run --suite agent-stall`) | `game.ltkuywyo` | agent-stall **ALL GREEN** 33.0s | standalone suite re-run confirms the green is not order/topology dependent |

### First-run failure root cause — fake-llm keyword collision (T021)

Case (d) failed because the prompt `"think interrupt gap"` matched BOTH
`greeting` (2-char keyword `hi` ⊂ "t(hi)nk",
`projects/game/fake-llm/service/testdata/chat.yaml`) and `think-interrupt-gap`
(full keyword, `testdata/stall_recovery.yaml`); under [012 spec
FR-006/FR-007](../012-fake-llm-service/spec.md) (case-insensitive substring
match + alphabetical-lowest-`name` tie-break) `greeting` wins **deterministically**
— the first received thinking frame was greeting's reasoning, and the second
frame hit the 75s read timeout (trace `fbac92826519a78413780294766e772b`).
Mechanical enumeration over all 7 testdata files showed the collision is unique
to these two templates, and ALL three 046 think templates ("think" keywords)
were equally hijacked. Fix = **greeting keyword hygiene** (drop `hi`,
[T021](tasks.md) — commit `a84a6a2`, designer ruling docs `bd3c47a`; full record
in [tasks.md](tasks.md) header "T012 首跑修订"); `matcher.go`/`matcher_test.go`
and the 012 FR-006/FR-007 contract semantics are unchanged.

### R9 ruling — heartbeat false-stall did NOT recur (contingency not needed)

- The heartbeat case `TestAgentStallToolExecutionNotFalselyDetected` passed in
  **all three runs** (12.31s in the re-run) — the R9 contingency branch
  ([research.md](research.md) R9) was never triggered.
- **Tick-path observability works**: signoz query
  `service.name='game/agent' AND body CONTAINS 'tool heartbeat'` returns
  `tool heartbeat wrapper started` for `saolei_init`/`saolei_operate` with
  `intervalMs=10000` (standard-suite envs, e.g. `game.lt1hf596`).
- **Stall-env (2s heartbeat) tick logs did not persist to the log store**: all
  three stall envs show 0 records (T2 trace
  `e956a177c5fc966370b90e859b924c88` also not exported). Cause: the ~33s env
  lifetime is shorter than the telemetry batch-export delay — the pod is
  deleted while data is still queued. This is an objective limitation of
  very-short-lived deploys, **not a functional defect**; the R9 discriminator
  is only needed if the false stall recurs.

### SC-005 status

Working default **α** executed in full (unit-layer substitution — T003/T004,
see [quickstart.md](quickstart.md) A4); **γ** (T020) not executed; the spec
owner's final ruling remains pending — requested in the executor's completion
report (see §4).
