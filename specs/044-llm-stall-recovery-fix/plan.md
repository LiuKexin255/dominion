# Implementation Plan: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Branch**: `044-llm-stall-recovery-fix` | **Date**: 2026-08-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/044-llm-stall-recovery-fix/spec.md`, grounded in the survey [`survey/llm-stream-stall-recovery-revision.md`](../../survey/llm-stream-stall-recovery-revision.md).

## Summary

This is a focused follow-up to [Feature 043](../043-llm-stream-stall-recovery/spec.md) that corrects two production problems without rewriting 043's shipped architecture:

1. **Over-aggressive stall detection (Problem 1).** 043's `STREAM_IDLE_TIMEOUT_MS = 30_000` (`projects/game/agent/src/llm.ts:43-44`) is the most aggressive chunk-idle threshold in the industry (4–10× tighter than LangChain 120s, OpenClaw 120s, Hermes 180s, Codex 300s). It is raised to **120s** (industry median), and a **per-reasoning-model floor** is added (DeepSeek family → 600s) so reasoning models like `deepseek-v4-flash` (~65s to first content token, per [hermes#61461](https://github.com/NousResearch/hermes-agent/issues/61461)) are no longer false-stalled during legitimate deep thinking. The floor follows Hermes's `max(default, floor)` semantics ([commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)); explicit operator config always wins.

2. **Partial output permanently lost on stall (Problem 2).** LangGraph's `idleTimeout` runs `task.writes.splice(0, …)` on abort (`@langchain/langgraph` `dist/pregel/timeout.js:200-211`), discarding the stalled node's buffered writes — so output already streamed to the frontend vanishes from the checkpoint and is absent from `ListMessages` after reconnect. `runTeamTurn` (`projects/game/agent/src/session-team.ts:725-912`) will accumulate streamed `TurnBlock`s and, on catching a `NodeTimeoutError`, persist the **stalled node's** partial output to its per-agent channel via `graph.updateState` (partitioned by `err.node` to avoid duplicating already-checkpointed prior-node output), with an **"interrupted" flag** on the last content block, then re-throw so 043's `finishError` (warn + wait, retain buffer) runs unchanged. The desktop standardizes `WarnSignal` rendering as a conversation ⚠ bubble (the current idleTimeout style) and renders the interrupted flag on reconnect.

Automatic retry/fallback (survey §6.3) is explicitly out of scope (spec FR-009), deferred to a future spec.

## Technical Context

**Language/Version**: TypeScript (agent service) + Svelte/TypeScript (desktop frontend). Agent built with Bazel (`bazel test //projects/game/agent/...`); `gazelle`-generated `BUILD.bazel`.

**Primary Dependencies**:
- `@langchain/langgraph` 1.4.8 — `StateGraph`, `TimeoutPolicy.idleTimeout` (per-node `addNode` option), `NodeTimeoutError` (exposes `.node` / `.kind` / `.idleTimeout`, `dist/errors.d.ts:103-125`), `messagesStateReducer` (channel reducer — appends/dedups by id), `MemorySaver` (per-session checkpointer).
- `@langchain/openai` 1.5.5 — ChatModel; **no** client-layer chunk-idle guard ([langchainjs #9088](https://github.com/langchain-ai/langchainjs/issues/9088)), so LangGraph's `idleTimeout` remains the sole chunk-idle defense.
- `langchain/chat_models/universal` `initChatModel` — model factory (`projects/game/agent/src/model-provider.ts`).

**Storage**: LangGraph `MemorySaver` checkpointer (in-memory, per-session, `projects/game/agent/src/team/graph.ts:365`). Per-agent channels `playerMessages` / `plannerMessages` (`messagesStateReducer`). `ListMessages` reads `team.getTeamState()` → `graph.getState().values[channel]` (`projects/game/agent/src/handler.ts:619-620`).

**Testing**: Vitest via Bazel `js_test` (`bazel test //projects/game/agent/src:llm_test`, `:graph_test`, `:session_team_test`, `:handler_test`, `:turn_loop_test`). Large tests via the testplan skill (`guitar run <plan.yaml>`, `tools/test/guitar`), per `style/large_test.md`.

**Target Platform**: Node.js 24+ (agent service), desktop (Svelte frontend).

**Project Type**: web-service (agent) + desktop-app (frontend). The agent is the primary change site; the desktop receives two rendering requirements (FR-012/FR-013).

**Performance Goals**: N/A — this feature is a latency-threshold tuning + data-integrity fix, not a throughput feature. The tradeoff is documented: real-stall detection latency rises 30s → 120s in exchange for eliminating reasoning-model false positives (the dominant production failure).

**Constraints**:
- MUST NOT change 043's shipped behaviors (spec FR-010): tool-execution heartbeat (`withIdleHeartbeat`, `projects/game/agent/src/llm.ts:302-322`), queued-buffer retention (043 FR-006/FR-007), warn+wait recovery terminal (`turn-loop.ts:412-424`), abort semantics (043 FR-011), init-turn total timeout (`INIT_TURN_TIMEOUT_MS`).
- Partial output MUST round-trip through `ListMessages` identically to a normal AIMessage (reconstruction at `handler.ts:668-717` reads `msg.content` array → reasoning/text/image parts + `tool_calls`).
- `graph.updateState` is called AFTER the stall's AbortSignal fired — must be confirmed feasible (research.md R4 spike).

**Scale/Scope**: 5 agent files modified (`llm.ts`, `team/graph.ts`, `session-team.ts`, `handler.ts`, `server.ts`) + 1 new agent module (`reasoning-timeouts.ts`) + 2 proto changes in `game.proto` (new `PartCompletion` enum + `completion` field on `TextPart`/`ThinkingPart` — the interrupted marker wire carrier, FR-010 controlled exception; + comment-only `warn` reconciliation for FR-012) + proto code regeneration (Go `game_go_proto` via bazel build; agent `game_types` via `ts_proto_library`) + 3 desktop files (`api.ts`, `components/ChatView.svelte`, `components/ChatMessage.svelte`) + stall large-test re-baseline（`deploy_agent_stall.yaml` env 15s→60s、`agent_stall_test.go` 时序、`system_test.yaml` suite 11 注释）. 8 implementation tasks across 4 phases (Phase 2–5).

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

No regressions; all gates hold post-design.

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
├── spec.md                       # /speckit.specify output (done)
├── checklists/requirements.md    # /speckit.specify output (done)
├── research.md                   # Phase 0 output (/speckit.plan)
├── data-model.md                 # Phase 1 output (/speckit.plan)
├── quickstart.md                 # Phase 1 output (/speckit.plan)
└── contracts/                    # Phase 1 output (/speckit.plan)
    ├── idle-timeout-contract.md      # default revision + reasoning-floor resolution
    ├── partial-output-contract.md    # checkpoint write semantics on stall
    └── desktop-rendering-contract.md # WarnSignal ⚠ bubble + interrupted flag + proto reconciliation
```

### Source Code (repository root)

```text
projects/game/agent/src/
├── llm.ts                        # MODIFY: STREAM_IDLE_TIMEOUT_MS default 30_000 → 120_000 (FR-001)
├── reasoning-timeouts.ts         # NEW: REASONING_IDLE_TIMEOUT_FLOOR table + getReasoningIdleTimeoutFloor() + resolveStreamIdleTimeout() (FR-002/FR-003)
├── team/
│   ├── graph.ts                  # MODIFY: addNode player/planner — apply resolveStreamIdleTimeout(modelSpec); TeamGraphDeps gains optional playerModelSpec/plannerModelSpec (FR-002/FR-003)
│   ├── graph.test.ts             # EXTEND: assert per-node resolved idleTimeout reflects floor
│   └── player.ts / planner.ts    # NO CHANGE (NodeTimeoutError re-throw unchanged from 043)
├── session-team.ts               # MODIFY: runTeamTurn — accumulate partial TurnBlocks, catch NodeTimeoutError, persist stalled-node partial via updateState, re-throw; mergePartialBlocks() helper; tests
├── session-team.test.ts          # EXTEND: mock-stall persistence assertions
├── handler.ts                    # MODIFY: ListMessages reconstruction (:804-847) translates the checkpoint-layer `additional_kwargs.interrupted` into the proto `completion` field (`PART_COMPLETION_INTERRUPTED`) on emitted TextPart/ThinkingPart (replaces the prior `as unknown as MessagePart` loose-JSON cast, which cannot cross the network — desktop-rendering-contract §3; partial-output-contract §4)
├── handler.test.ts               # EXTEND: ListMessages returns partial output with interrupted indicator
├── llm.test.ts                   # EXTEND: STREAM_IDLE_TIMEOUT_MS default assertion
├── turn-loop.ts                  # NO CHANGE (runLoop/finishError unchanged — partial persistence happens upstream in runTeamTurn before re-throw)
└── server.ts                     # MODIFY: pass profile model specs to buildTeamGraph call sites (:260,335 — playerModelSpec/plannerModelSpec from prompt-client.ts; FR-002/FR-003)

projects/game/desktop/frontend/src/
├── App.svelte                    # VERIFY/EXTEND: fp.warn → warn-bubble (already present at :789-802); history-seed path renders interrupted flag (FR-013)
└── components/ChatView.svelte    # EXTEND: render interrupted indicator on a flagged block (FR-013); warn-bubble already at :271-279 (FR-012 standardization)

projects/game/
└── game.proto                    # MODIFY: (1) NEW `PartCompletion` enum + `completion` field on `TextPart`/`ThinkingPart` (field 2 each) — the interrupted marker's wire carrier (FR-005/FR-013, controlled exception to FR-010); (2) comment-only reconcile of :451-453 "FlowParts never rendered" → document warn as the rendered exception (FR-012, T010)
```

**Structure Decision**: Single repo, two change surfaces — the agent service (`projects/game/agent/src/`, the primary) and the desktop frontend (`projects/game/desktop/frontend/src/`). The proto gains a new `PartCompletion` enum + `completion` field on `TextPart`/`ThinkingPart` (the interrupted marker's wire carrier — a controlled exception to FR-010, see Constitution Check) and a comment-only reconcile of the FlowPart render comment (FR-012, T010). No new top-level directories; `reasoning-timeouts.ts` is co-located with `llm.ts` (same LLM-config domain).

## Complexity Tracking

> Not applicable — Constitution Check has no violations to justify.
