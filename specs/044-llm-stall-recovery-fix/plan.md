# Implementation Plan: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Branch**: `044-llm-stall-recovery-fix` | **Date**: 2026-08-12 (**amended 2026-08-14** — resume scope, see [Update 2026-08-14](#update-2026-08-14--resume-scope-large-test-completion--config-channel)) | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/044-llm-stall-recovery-fix/spec.md`, grounded in the survey [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md). The 2026-08-14 amendment incorporates the user-directed resume goals recorded in [large-test-status.md](large-test-status.md) + [spec.md Clarifications Session 2026-08-14](spec.md): (1) complete the feature's large test and verify it by actual execution; (2) move the agent service's timeout parameters onto the service-config channel ([Feature 045](../045-deploy-config/spec.md)) so large tests can select a different, faster configuration.

**Working lineage note**: there is no local `044-llm-stall-recovery-fix` branch — all 044 commits (through `fd81521`) are carried by the `045-deploy-config` → `046-fake-llm-think-chunking` lineage (verified 2026-08-14: `git branch --contains fd81521` lists 043/045/046/cl-deploy-config). Resume work continues on that lineage (current checkout `046-fake-llm-think-chunking` contains 044+045+046 complete).

## Summary

This is a focused follow-up to [Feature 043](../043-llm-stream-stall-recovery/spec.md) that corrects two production problems without rewriting 043's shipped architecture:

1. **Over-aggressive stall detection (Problem 1).** 043's `STREAM_IDLE_TIMEOUT_MS = 30_000` (`projects/game/agent/src/llm.ts:43-44`) is the most aggressive chunk-idle threshold in the industry (4–10× tighter than LangChain 120s, OpenClaw 120s, Hermes 180s, Codex 300s). It is raised to **120s** (industry median), and a **per-reasoning-model floor** is added (DeepSeek family → 600s) so reasoning models like `deepseek-v4-flash` (~65s to first content token, per [hermes#61461](https://github.com/NousResearch/hermes-agent/issues/61461)) are no longer false-stalled during legitimate deep thinking. The floor follows Hermes's `max(default, floor)` semantics ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)); explicit operator config always wins.

2. **Partial output permanently lost on stall (Problem 2).** LangGraph's `idleTimeout` runs `task.writes.splice(0, …)` on abort (`@langchain/langgraph` `dist/pregel/timeout.js:200-211`), discarding the stalled node's buffered writes — so output already streamed to the frontend vanishes from the checkpoint and is absent from `ListMessages` after reconnect. `runTeamTurn` (`projects/game/agent/src/session-team.ts:725-912`) accumulates streamed `TurnBlock`s and, on catching a `NodeTimeoutError`, persists the **stalled node's** partial output to its per-agent channel via `graph.updateState` (partitioned by `err.node` to avoid duplicating already-checkpointed prior-node output), with an **"interrupted" flag** on the last content block, then re-throw so 043's `finishError` (warn + wait, retain buffer) runs unchanged. The desktop standardizes `WarnSignal` rendering as a conversation ⚠ bubble (the current idleTimeout style) and renders the interrupted flag on reconnect.

Automatic retry/fallback (survey §6.3) is explicitly out of scope (spec FR-009), deferred to a future spec.

**2026-08-14 resume scope (this amendment)**: implementation Phases 1–5 are done and unit-green; T012 large-test acceptance is blocked on uncontrolled deploy-side timings (env idle channel clamps at ≥60s; heartbeat interval has no channel at all) and an unexplained heartbeat false-stall at the 60s scale ([large-test-status.md](large-test-status.md) §2). With enablers [045](../045-deploy-config/spec.md) (service-config) and [046](../046-fake-llm-think-chunking/spec.md) (fake-llm think chunking) landed, the resume adds: a **service-config channel** for the agent's timeout parameters (test-grade block `agent_timeouts`, resolution env > config > default, config honored as-is — spec FR-008 amendment), **per-tick heartbeat logging** (root-cause discriminator, research.md R9), a **re-authored stall suite at 5s/2s timings** with one new think-gap case, and the **T012 → T013 completion path** (research.md R9/R10/R11; contracts/idle-timeout-contract.md §5; data-model.md §7; quickstart.md Phase D).

## Technical Context

**Language/Version**: TypeScript (agent service) + Svelte/TypeScript (desktop frontend). Agent built with Bazel (`bazel test //projects/game/agent/...`); `gazelle`-generated `BUILD.bazel`.

**Primary Dependencies**:
- `@langchain/langgraph` 1.4.8 — `StateGraph`, `TimeoutPolicy.idleTimeout` (per-node `addNode` option), `NodeTimeoutError` (exposes `.node` / `.kind` / `.idleTimeout`, `dist/errors.d.ts:103-125`), `messagesStateReducer` (channel reducer — appends/dedups by id), `MemorySaver` (per-session checkpointer).
- `@langchain/openai` 1.5.5 — ChatModel; **no** client-layer chunk-idle guard ([langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)), so LangGraph's `idleTimeout` remains the sole chunk-idle defense.
- `langchain/chat_models/universal` `initChatModel` — model factory (`projects/game/agent/src/model-provider.ts`).
- `@dominion/common-js-config` (`common/js/config`, [045 sdk-js contract](../045-deploy-config/contracts/sdk-js.md)) — **added 2026-08-14**: `readConfig<T>(block, key, defaults)` reads `{DOMINION_CONFIG_DIR}/{block}/{key}`, YAML-parses, deep-merges over defaults; throws when the block is not selected (the agent treats that as "absent" — contract §5). Runtime deps wiring mirrors the 045 reference consumer (`experimental/ts/grpc_hello_world`: package.json dependency + `//common/js/config:runtime_pkg` runtime_dep).
- `@dominion/common-js-logs` — agent structured logging (`info`/`warn`); heartbeat per-tick logs (R9) reuse the existing import pattern (`server.ts:37`, `turn-loop.ts:55`).

**Storage**: LangGraph `MemorySaver` checkpointer (in-memory, per-session, `projects/game/agent/src/team/graph.ts:365`). Per-agent channels `playerMessages` / `plannerMessages` (`messagesStateReducer`). `ListMessages` reads `team.getTeamState()` → `graph.getState().values[channel]` (`projects/game/agent/src/handler.ts:619-620`).

**Testing**: Vitest via Bazel `js_test` (`bazel test //projects/game/agent/src:llm_test`, `:graph_test`, `:session_team_test`, `:handler_test`, `:turn_loop_test`). Large tests via the testplan skill (`guitar run <plan.yaml>`, `tools/test/guitar`), per `style/large_test.md`.

**Target Platform**: Node.js 24+ (agent service), desktop (Svelte frontend).

**Project Type**: web-service (agent) + desktop-app (frontend). The agent is the primary change site; the desktop receives two rendering requirements (FR-012/FR-013).

**Performance Goals**: N/A — this feature is a latency-threshold tuning + data-integrity fix, not a throughput feature. The tradeoff is documented: real-stall detection latency rises 30s → 120s in exchange for eliminating reasoning-model false positives (the dominant production failure).

**Constraints**:
- MUST NOT change 043's shipped behaviors (spec FR-010): tool-execution heartbeat (`withIdleHeartbeat`, `projects/game/agent/src/llm.ts:328-348`), queued-buffer retention (043 FR-006/FR-007), warn+wait recovery terminal (`turn-loop.ts:412-424`), abort semantics (043 FR-011), init-turn total timeout (`INIT_TURN_TIMEOUT_MS` — its **default** (120s) is unchanged; the 2026-08-14 FR-008 amendment adds a service-config tier that MAY supply an explicit `initTurnTimeoutMs`, per [contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §5).
- Partial output MUST round-trip through `ListMessages` identically to a normal AIMessage (reconstruction at `handler.ts:668-717` reads `msg.content` array → reasoning/text/image parts + `tool_calls`).
- `graph.updateState` is called AFTER the stall's AbortSignal fired — must be confirmed feasible (research.md R4 spike).

**Scale/Scope**: 5 agent files modified (`llm.ts`, `team/graph.ts`, `session-team.ts`, `handler.ts`, `server.ts`) + 1 new agent module (`reasoning-timeouts.ts`) + 2 proto changes in `game.proto` (new `PartCompletion` enum + `completion` field on `TextPart`/`ThinkingPart` — the interrupted marker wire carrier, FR-010 controlled exception; + comment-only `warn` reconciliation for FR-012) + proto code regeneration (Go `game_go_proto` via bazel build; agent `game_types` via `ts_proto_library`) + 3 desktop files (`api.ts`, `components/ChatView.svelte`, `components/ChatMessage.svelte`) + stall large-test re-baseline（`deploy_agent_stall.yaml` env 15s→60s、`agent_stall_test.go` 时序、`system_test.yaml` suite 11 注释）. 8 implementation tasks across 4 phases (Phase 2–5). **2026-08-14 resume additions**: +1 new agent module (`agent-timeouts.ts` + tests), `llm.ts` constants re-sourced + heartbeat tick logs, `service.yaml` config block, `deploy_agent_stall.yaml` env→config, `agent_stall_test.go` rescale (5s/2s/12s) + new think-gap case + comment updates, `system_test.yaml` suite-11 description update, fake-llm `think-interrupt-gap` delay hygiene (90s→15s, plus lockstep sync of the 046 pin assertion in `message_store_test.go:819-821` — 2026-08-14 amendment).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution version 1.3.0 (`.specify/memory/constitution.md`). Evaluation:

| # | Principle / Gate | Status | Evidence |
|---|---|---|---|
| 1 | **V — Read Before Code** (doc-reading gate) | ✅ PASS | tasks.md (next phase) will declare all docs per phase in the mandatory three-bucket format (代码规范 / 官方文档 / 技术文章), including `style/golang.md`/`style/javascript.md`, `style/api.md`, `style/large_test.md`, the LangGraph `TimeoutPolicy`/`NodeTimeoutError` docs, and the survey. The planner has read each cited file to confirm it contains real content. |
| 2 | **II — Refactoring Over Patching** (implementation gate) | ✅ PASS | (a) The idle default is a value correction, not a patch. (b) The reasoning floor is a NEW, cleanly-separated module (`reasoning-timeouts.ts`) applied at the existing `addNode` seam — not a fork of LangGraph. (c) Partial-output persistence is layered on `runTeamTurn` (the layer that owns the event stream AND has `graph.updateState` access) — evaluated against alternatives in research.md R3 (not patched into `turn-loop.ts` which lacks graph access, nor into LangGraph internals). (d) Desktop warn rendering formalizes existing behavior + reconciles the proto comment, not a workaround. |
| 3 | **III — Interface-First Design** (implementation gate) | ✅ PASS | All three interfaces are defined as contracts BEFORE code: `contracts/idle-timeout-contract.md` (effective timeout resolution), `contracts/partial-output-contract.md` (checkpoint write semantics: when/where/what/how, `NodeTimeoutError.node` partitioning, merge rules, interrupted flag), `contracts/desktop-rendering-contract.md` (WarnSignal ⚠ bubble + interrupted-flag rendering, proto reconciliation). |
| 4 | **IV — Test Granularity & Cadence** (compile+unit gate) | ✅ PASS | Unit tests per change (no separate task — part of each implementation task): `STREAM_IDLE_TIMEOUT_MS === 120_000`, `getReasoningIdleTimeoutFloor("deepseek-v4-flash") === 600_000`, mock-stall `updateState` assertion, desktop warn-bubble/interrupted rendering. `bazel build` + `bazel test` on every change. |
| 5 | **I — Citation & Provenance** (citation gate) | ✅ PASS | All artifacts cite repo-internal sources by relative path with line numbers and external sources by full URL (survey §8 is the evidence index). The inaccurate 043 "15–30s consensus" citation is corrected with accurate anchors. |
| 6 | **VI — Large Test Acceptance for Services** (large-test gate) | ✅ PASS (planned) | The agent is a service. quickstart.md mandates large-test execution via `guitar run`: saolei + reasoning model completes a game without reasoning-induced stall; stall → reconnect → `ListMessages` returns the partial output with the interrupted flag. Acceptance = **all large-test cases pass** (build-only is explicitly NOT acceptance, per Constitution VI). |

**No violations.** No Complexity Tracking entries needed.

### Post-design re-check (after Phase 0 + Phase 1 artifacts)

Re-evaluated against the now-complete design artifacts ([research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)):

- **Gate 2 (II/III)** — all three interfaces are now defined as contracts (`idle-timeout-contract.md`, `partial-output-contract.md`, `desktop-rendering-contract.md`); the layering decision (persistence in `runTeamTurn`, floor in `reasoning-timeouts.ts`) is justified against alternatives in research.md R2/R3. ✅ still PASS.
- **Gate 3 (IV)** — every change has a named unit test in quickstart.md (A1–A3, B1–B6, C1–C2). ✅ still PASS.
- **Gate 5 (VI)** — three large-test scenarios (A4 reasoning-model game completion, B6 stall→reconnect partial survival, C3 desktop rendering) mandated via `guitar run`; acceptance = all cases pass (build-only explicitly excluded). ✅ still PASS.
- The one empirical unknown (R4: `updateState` after abort) is a **gating spike** (quickstart B1) with a clear expected outcome and documented contingency — not a NEEDS CLARIFICATION.

### 2026-08-14 amendment re-check (resume scope)

Re-evaluated against the amendment's additions ([research.md](research.md) R9–R11, [idle-timeout-contract.md](contracts/idle-timeout-contract.md) §5, [data-model.md](data-model.md) §7, [quickstart.md](quickstart.md) Phase D):

- **Gate 2 (II — Refactoring Over Patching)** — the config channel is designed as a proper resolution layer (`agent-timeouts.ts` with a pure resolver + explicit absence semantics), integrated at the existing constant seam (`llm.ts` re-exports) rather than scattered `readConfig` calls; the clamp's re-scoping (env-only) is a documented semantic decision (contract §1), not a bypass bolted onto the env read. ✅ PASS.
- **Gate 3 (III — Interface-First)** — the channel's contract (block/entry/fields, precedence matrix, validation, absence semantics, observability) is fully specified in idle-timeout-contract §5 + data-model §7 BEFORE any resume implementation. ✅ PASS.
- **Gate 5 (VI — Large Test Acceptance)** — the resume scope's whole point is completing Constitution-VI acceptance: T012 must actually run via `guitar run` (deploy→test→cleanup) with ALL cases green at the config-driven timings; build-only remains explicitly non-acceptance. The heartbeat contingency (R9) is a fix-and-rerun loop until fully green. ✅ PASS (planned).
- **Gate 1 (V)** — the resume tasks (to be authored via `/speckit.tasks` for the amended Phase 5) must declare the 045 SDK/runtime contracts and this plan's §5 references in their document lists; the planner has read them (sdk-js.md, runtime-contract.md, yaml-schema.md §1–§2 all verified to contain the cited content). ✅ PASS.
- **SC-005**: remains a recorded spec-owner decision (α default / β / γ-optional) — NOT resolved by this amendment; large-test-status.md §4 stays authoritative. No gate violation (acceptance does not silently claim SC-005).

No regressions; all gates hold post-amendment.

### FR-010 controlled exception — `PartCompletion` proto field

FR-010 states this feature "MUST NOT change [Feature 043]'s other agent-side behaviors" and scopes the desktop change to rendering only. The original plan interpreted this as "no proto wire change" and routed the interrupted marker through a "lenient JSON channel" (an additive `interrupted:true` field the desktop tolerates as extra JSON).

**That routing was proven infeasible**: every hop on the network path strips unknown (undeclared) fields — (1) `@grpc/proto-loader` serializes against `game.proto` (no `interrupted` field → dropped); (2) proxy `grpc-go` is strict proto; (3) gateway `grpc-gateway` protojson emits only known fields; (4) the desktop Go client `protojson.UnmarshalOptions{DiscardUnknown: true}` (`client.go:312`) discards unknown fields; (5) `view_model.go` strict `protojson.Marshal` (`:222-234`) emits only known fields. A marker that is not a declared proto field cannot cross the network.

**Exception (user-authorized, scoped)**: the user directed that the interrupted marker be carried by **adding an enum parameter to the parts that need marking** (text/thinking). This authorizes a single, scoped proto-wire addition — the `PartCompletion` enum and a `completion` field (number 2) on `TextPart`/`ThinkingPart` — exclusively for the interrupted marker (FR-005/FR-013). It does **not** authorize any other proto change: the FlowPart/WarnSignal/MessagePart/ToolResultPart messages are unchanged (T010 is comment-only); no new RPC, no field renumbering, no semantic change to existing fields. The addition is **forward-compatible**: the default `PART_COMPLETION_UNSPECIFIED = 0` is omitted by protojson, so clients predating this field see no field and behave exactly as before (a normal complete part). The Go desktop layers need **no logic change** — the field is a known proto field, naturally preserved by `DiscardUnknown`-off-for-known-fields and emitted by strict `protojson.Marshal`.

This exception is recorded here so FR-010's "no proto wire change" reading does not conflict with the implementation. The spec.md FR-010 wording (agent-side behaviors) is not contradicted — the proto field carries only the desktop-rendering marker (FR-013); it does not alter 043's agent behaviors (heartbeat, buffer retention, warn+wait, abort, init-turn timeout).

## Project Structure

### Documentation (this feature)

```text
specs/044-llm-stall-recovery-fix/
├── plan.md                       # This file
├── spec.md                       # /speckit.specify output (done; amended 2026-08-14 — Clarifications + FR-008)
├── checklists/requirements.md    # /speckit.specify output (done)
├── large-test-status.md          # execution state & blockers (T012 pause record; resume context)
├── research.md                   # Phase 0 output (/speckit.plan; R9–R11 added 2026-08-14)
├── data-model.md                 # Phase 1 output (/speckit.plan; §7 added 2026-08-14)
├── quickstart.md                 # Phase 1 output (/speckit.plan; Phase D added 2026-08-14)
└── contracts/                    # Phase 1 output (/speckit.plan)
    ├── idle-timeout-contract.md      # default revision + reasoning-floor resolution (§1 amended, §5 added 2026-08-14)
    ├── partial-output-contract.md    # checkpoint write semantics on stall
    └── desktop-rendering-contract.md # WarnSignal ⚠ bubble + interrupted flag + proto reconciliation
```

### Source Code (repository root)

```text
projects/game/agent/
├── service.yaml                  # MODIFY (2026-08-14): top-level configs block `agent_timeouts` (entry `timeouts`, test-grade 5000/2000) — idle-timeout-contract §5
└── src/
    ├── agent-timeouts.ts         # NEW (2026-08-14): DEFAULT_AGENT_TIMEOUTS + loadAgentTimeoutOverrides() + pure resolveAgentTimeouts(env, overrides) + validation
    ├── agent-timeouts.test.ts    # NEW (2026-08-14): resolution matrix + absence semantics (quickstart D1/D2)
    ├── llm.ts                    # MODIFY: STREAM_IDLE_TIMEOUT_MS default 30_000 → 120_000 (FR-001, done); 2026-08-14: constants re-sourced from agent-timeouts resolver (STREAM_IDLE_TIMEOUT_EXPLICIT = env||config), withIdleHeartbeat gains per-tick info logs (R9)
    ├── reasoning-timeouts.ts     # floor table + resolution (done; unchanged — consumes llm.ts exports whose derivation changed)
    ├── team/
    │   ├── graph.ts              # MODIFY (done): apply resolveStreamIdleTimeout(modelSpec) at addNode
    │   └── player.ts / planner.ts # NO CHANGE
    ├── session-team.ts           # MODIFY (done): partial-output persistence in runTeamTurn
    ├── handler.ts                # MODIFY (done): completion-field translation in ListMessages
    └── server.ts                 # MODIFY (done): pass profile model specs; 2026-08-14: BUILD runtime_deps for @dominion/common-js-config

projects/game/testplan/
├── deploy_agent_stall.yaml       # MODIFY (2026-08-14): drop GAME_STREAM_IDLE_TIMEOUT_MS env; add configs: [agent_timeouts] on agent_test artifact
├── agent_stall_test.go           # MODIFY (2026-08-14): rescale constants (stallWindow 60s→5s, stallDetectMin/Max →3s/10s, stallToolReplyDelay 65s→12s), env→config comment updates, NEW think-gap case (think-interrupt-gap template)
├── helpers_test.go               # NO CHANGE (wsReadTimeout is a read-deadline ceiling, not a sleep — no rescale needed)
└── system_test.yaml              # MODIFY (2026-08-14): suite-11 agent-stall description + §11 comment block: env 60000 → config-driven 5s/2s

projects/game/fake-llm/service/
├── message_store_test.go          # MODIFY (2026-08-14 amendment — T019): think-interrupt-gap pin assertion :819-821 ["1s","90s"] → ["1s","15s"] (lockstep with the testdata rescale)
└── testdata/
    └── stall_recovery.yaml        # MODIFY (2026-08-14, hygiene): think-interrupt-gap chunk_delays 90s → 15s (detection fires ~5s into the gap; shorter residual wait on regression paths)

projects/game/desktop/frontend/src/
├── App.svelte                    # done (warn bubble verified; history-seed interrupted flag)
└── components/ChatView.svelte    # done (interrupted indicator)

projects/game/
└── game.proto                    # done (PartCompletion enum + completion field; warn comment reconciliation)
```

**Structure Decision**: Single repo, two change surfaces — the agent service (`projects/game/agent/src/`, the primary) and the desktop frontend (`projects/game/desktop/frontend/src/`). The proto gains a new `PartCompletion` enum + `completion` field on `TextPart`/`ThinkingPart` (the interrupted marker's wire carrier — a controlled exception to FR-010, see Constitution Check) and a comment-only reconcile of the FlowPart render comment (FR-012, T010). No new top-level directories; `reasoning-timeouts.ts` is co-located with `llm.ts` (same LLM-config domain). The 2026-08-14 resume keeps the same surfaces plus the deploy/testplan layer (`service.yaml`, `deploy_agent_stall.yaml`, `agent_stall_test.go`, `system_test.yaml`) and one fake-llm testdata value (plus its store-level pin assertion in `message_store_test.go` — 2026-08-14 amendment); `agent-timeouts.ts` is co-located with `llm.ts` (same timeout-config domain, consumed by it).

## Update 2026-08-14 — Resume Scope (large-test completion + config channel)

Implements the user-directed resume goals. Full context: [large-test-status.md](large-test-status.md) (pause record), [research.md](research.md) R9–R11, [contracts/idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1/§5, [data-model.md](data-model.md) §7, [quickstart.md](quickstart.md) Phase D. **This section supersedes the 60s re-baseline instructions inside [tasks.md](tasks.md) Phase 5 T011 (the remainder of T011–T013 stands); the Phase 5 tasks should be re-authored via `/speckit.tasks` from this section before execution.**

### Work items

1. **Agent config channel** (goal 2): new `agent-timeouts.ts` (+ unit tests, quickstart D1/D2) per contract §5 — `DEFAULT_AGENT_TIMEOUTS`, `loadAgentTimeoutOverrides()` (SDK read, absence-tolerant), pure `resolveAgentTimeouts(env, overrides)` (env > config > default; env-clamp scoped to env; config as-is; heartbeat ≥ idle → throw). `llm.ts` re-sources its four exported constants from the resolver (`STREAM_IDLE_TIMEOUT_EXPLICIT` = env OR config); `withIdleHeartbeat` gains per-tick `info` logs (tool, interval, tick index — R9 discriminator). `projects/game/agent/service.yaml` gains the `agent_timeouts` block (test-grade `5000`/`2000`). BUILD: `@dominion/common-js-config` package dependency + `//common/js/config:runtime_pkg` runtime_dep; `pnpm up` + `gazelle` + `bazel mod tidy` per AGENTS.md.
2. **Deploy switch** (goal 2): `deploy_agent_stall.yaml` — remove `GAME_STREAM_IDLE_TIMEOUT_MS` env, add `configs: [agent_timeouts]` on the `agent_test` artifact. Production (`projects/game/deploy.yaml`) and standard suite (`deploy_agent.yaml`) unchanged (no selection → defaults).
3. **Stall-suite rescale + new case** (goal 1): `agent_stall_test.go` — `stallWindow` 60s→5s, `stallDetectMin`/`stallDetectMax` →3s/10s, `stallToolReplyDelay` 65s→12s (≈2.4× window; ~6 heartbeat ticks — exercises REPEATED refresh, the exact failure mode from R9); update the env→config references in file/const doc comments; add `TestAgentStallThinkInterruptGapDetected` (046 `think-interrupt-gap`: finite mid-thinking gap detected within the window; both pre-gap reasoning chunks persist as the partial with the interrupted tail marker). `system_test.yaml` suite-11 description + §11 comments updated to the config-driven 5s/2s. fake-llm `stall_recovery.yaml`: `think-interrupt-gap` second delay 90s→15s (detection fires ~5s in; hygiene for regression paths), **plus the 046 store-level pin assertion in lockstep** — `message_store_test.go:819-821` pins `["1s","90s"]` and is rescaled to `["1s","15s"]` by the same task (2026-08-14 amendment: corrects the original premise that the pin test does not pin `chunk_delays` and needs no sync). `helpers_test.go` **unchanged** (`wsReadTimeout` is a deadline ceiling, not a sleep).
4. **T012 acceptance** (goal 1, Constitution VI): `guitar run projects/game/testplan/system_test.yaml` (agent-stall suite) — full deploy→test→cleanup, ALL cases green. **Contingency (R9)**: if the heartbeat case fails again, query the run's trace/logs via signoz — ticks present + false stall → LangGraph `touch()`/`checkIdle` path issue; ticks absent → wrapper timer lifecycle bug; fix at the identified layer and re-run until fully green. Build-only is NOT acceptance.
5. **T013 housekeeping**: `bazel run //:gazelle` (agent src + testplan), `bazel run //:go -- fmt` for changed Go files, `bazel mod tidy`, final `bazel build //...` + `bazel test //projects/game/agent/...` (+ desktop dist build) whole-tree green; mark tasks `[X]`; append the outcome to [large-test-status.md](large-test-status.md).
6. **Optional (pending spec-owner ruling on SC-005 — research.md R11)**: γ floor large-test case on the default topology (existing `deploy_agent.yaml`, deepseek profile, a ~150s-gap resumable template, riding an existing standard-suite module file — NOT the stall suite). Do not start unless the ruling selects γ.

### Open decisions carried (not blocking execution of items 1–5)

- **SC-005 α/β/γ** — spec owner; working default α (unit substitution, quickstart A4). Item 6 is the pre-analyzed γ.
- Confirmation requests (non-blocking, recommended defaults baked into the contracts): config-tier as-is semantics incl. sub-60s test values (Q1), param set idle+heartbeat+init-turn (Q2), heartbeat env channel intentionally NOT added (config-only), new think-gap case inclusion (Q7).

## Complexity Tracking

> Not applicable — Constitution Check has no violations to justify.
