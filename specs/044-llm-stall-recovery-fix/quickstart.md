# Quickstart: LLM Stream Stall Recovery — Timeout Tuning & Partial Output Persistence

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12

**Spec**: [spec.md](spec.md) | **Contracts**: [idle-timeout](contracts/idle-timeout-contract.md) · [partial-output](contracts/partial-output-contract.md) · [desktop-rendering](contracts/desktop-rendering-contract.md) | **Data model**: [data-model.md](data-model.md) | **Research**: [research.md](research.md)

## Prerequisites

- Bazel build system (`bazel build //...`, `bazel test //...`)
- The agent service's team graph (`projects/game/agent/src/team/graph.ts`) with 043's stall recovery shipped
- LangGraph 1.4.8+ (`TimeoutPolicy.idleTimeout`, `NodeTimeoutError.node`/`.kind`, `messagesStateReducer`, `MemorySaver`)
- Node.js 24+
- For large tests: the testplan skill (`tools/test/guitar`), per `style/large_test.md`

This feature is a follow-up to [Feature 043](../043-llm-stream-stall-recovery/quickstart.md); 043's scenarios remain valid and unchanged.

---

## Phase A — Idle-timeout calibration + reasoning floor (spec US1/US2)

### Scenario A1 (unit): default idle timeout is 120s; min configurable 60s

**Validates**: FR-001 (spec US1.3). `STREAM_IDLE_TIMEOUT_MS` default raised 30s → 120s; env override honored; values `< 60_000` clamped.

**How**: in `projects/game/agent/src/llm.test.ts`, assert `STREAM_IDLE_TIMEOUT_MS === 120_000` with the env var unset. Set `GAME_STREAM_IDLE_TIMEOUT_MS=45000` and assert it is clamped to `120_000` (min 60s). Set `GAME_STREAM_IDLE_TIMEOUT_MS=90000` and assert it is honored as-is.

```bash
bazel test //projects/game/agent/src:llm_test
```

### Scenario A2 (unit): reasoning-floor matching (longest-first)

**Validates**: FR-002/FR-003. `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first (`o3-mini-` distinct from `o3-`).

**How**: in a new `reasoning-timeouts.test.ts`, assert the matching rules in [idle-timeout-contract.md §2](contracts/idle-timeout-contract.md#2-reasoning-floor-matching).

```bash
bazel test //projects/game/agent/src:reasoning_timeouts_test   # gazelle-generated target
```

### Scenario A3 (unit): graph node timeout reflects the floor

**Validates**: FR-002/FR-003 application. `buildTeamGraph({ playerModel, plannerModel, playerModelSpec: "openai/deepseek-v4-flash" })` → `nodes.player.timeout.idleTimeout === max(120_000, 600_000) === 600_000`; with no spec → `=== STREAM_IDLE_TIMEOUT_MS`; with `GAME_STREAM_IDLE_TIMEOUT_MS=90000` explicitly set + DeepSeek spec → `=== 90_000` (explicit config honored as-is, even below the floor — FR-003/US2.3).

**How**: extend the existing assertion at `projects/game/agent/src/team/graph.test.ts:2684-2689` (already asserts `nodes[name].timeout?.idleTimeout === STREAM_IDLE_TIMEOUT_MS`) to cover the spec-supplied floor case.

```bash
bazel test //projects/game/agent/src:graph_test
```

### Scenario A4 (large-test case DROPPED — see rationale): reasoning-model floor

**Validates**: SC-001/SC-005 **floor-resolution layer (unit-validated)**. The reasoning-model floor's *resolution* (`getReasoningIdleTimeoutFloor` longest-first matching) and *application* (graph-node `idleTimeout === max(default, floor)` when env+config unset; `=== explicit value` when either explicit tier is set, even below floor — FR-003/US2.3) are validated at the **unit layer** by A2 (`reasoning-timeouts.test.ts`) and A3 (`graph.test.ts`). T004's `nodes.player.timeout.idleTimeout === 600_000` assertion proves the floor reaches the graph node correctly; LangGraph honoring the resolved `idleTimeout` is LangGraph's contract, not dominion logic.

**Why T011 case (a) is dropped (no large test for US2's floor in the stall suite)**:

1. **Any explicit idle source suppresses the floor.** The stall deploy selects the `agent_timeouts` config block (2026-08-14; previously explicit env — same semantics). Per [idle-timeout-contract.md §1](contracts/idle-timeout-contract.md#1-resolution-rule) + FR-003/US2.3, an explicit configuration always wins as-is — the floor only raises via `max(default, floor)` on the default path. So in this deploy the deepseek 600s floor never engages, and the model spec (deepseek vs gpt-4) has **zero effect** on the observable outcome. Any deepseek-spec variant is an observable-behavior duplicate of the existing gpt-4 stall tests — it can detect neither a floor regression nor a wiring regression.
2. **A floor large-test requires the default path → >120s observation.** The floor's value is tolerating *resumable* reasoning silence (silent > default 120s, < floor, then content). Since any explicit idle (env or config) suppresses the floor, testing it means env+config unset and a >120s silent gap — inherently a multi-minute case on a default-topology deploy.
3. **Option γ (default-topology variant) cost re-assessed 2026-08-14 — still not taken by default.** [046](../046-fake-llm-think-chunking/spec.md) removed the old hard blocker (`think-interrupt-gap` in `projects/game/fake-llm/service/testdata/stall_recovery.yaml` IS a resumable-silence template) and `deploy_agent.yaml` already provides the default topology (no new deploy needed), so γ is now a single ~3min optional case (deepseek profile + a ~150s-gap template) rather than a new-deploy investment. It remains **optional pending the spec owner's SC-005 ruling** (see below); if approved it rides an existing standard-suite module file — NOT this suite's topology.

**SC-005 note (flagged to spec owner — do NOT edit spec.md yourself)**: SC-005's literal large-test component ("reasoning model's normal thinking time causes no stall-induced interruption, in the large-test validation") remains **not covered by a shipped large test**; the floor's silence-tolerance benefit is validated at the unit layer (resolution A2 + application A3) only. The decision (accept unit-layer substitution α / amend SC-005 wording β / approve the now-cheaper γ) is still pending with the spec owner — see [large-test-status.md](large-test-status.md) §4 and [research.md](research.md) R11. Default working assumption: **α**.

---

## Phase B — Partial-output persistence (spec US3)

### Scenario B1 (unit, gating spike): `updateState` succeeds after AbortSignal fired

**Validates**: partial-output-contract.md §5 (research.md R4). `graph.updateState` is callable from the `NodeTimeoutError` catch block and the values are readable via `getState`.

**How**: in `session-team.test.ts`, build a graph with a `MemorySaver`; start `streamEvents`; trigger an idle `NodeTimeoutError`; in the catch call `updateState(...)` then `getState()` — assert the values are present. **This MUST pass before any other Phase B task proceeds.**

```bash
bazel test //projects/game/agent/src:session_team_test -- --grep "updateState after abort"
```

### Scenario B2 (unit): `mergePartialBlocks` rules

**Validates**: partial-output-contract.md §3 / data-model.md §3.2. Text+reasoning → AIMessage content array; tool_call + result → retained `tool_calls`; tool_call without result → dropped; tool_result → ToolMessage; empty → no-op; interrupted flag on the tail block.

**How**: unit-test `mergePartialBlocks` directly with crafted `TurnBlock[]` inputs covering each rule.

### Scenario B3 (unit): stall persists partial output to the stalled node's channel; error re-thrown

**Validates**: FR-004/FR-006/FR-007 (spec US3.1/US3.5). A mock stream yields blocks then rejects with idle `NodeTimeoutError{ node: "player" }`; `updateState` is called with the merged AIMessage in `playerMessages`; the error re-throws; `finishError` still emits warn+wait with retained buffer.

**How**: in `session-team.test.ts`, inject a fake stream (the existing DI seam used by 043 tests) that yields N blocks then throws `NodeTimeoutError`. Assert `updateState` args + the re-throw + `turn-loop.test.ts` confirms warn+wait/buffer retention.

```bash
bazel test //projects/game/agent/src:session_team_test //projects/game/agent/src:turn_loop_test
```

### Scenario B4 (unit): multi-node turn partitions by `err.node` (no duplication)

**Validates**: research.md R7. A turn that streams player blocks (complete) then planner blocks (stall) → `updateState` writes ONLY `plannerMessages`; player's output not duplicated.

### Scenario B5 (unit): `ListMessages` returns partial output with the interrupted marker

**Validates**: FR-005 (spec US3.3/SC-003). A checkpointed AIMessage whose last content block carries `additional_kwargs.interrupted = true` is returned by `ListMessages` with the corresponding `TextPart`/`ThinkingPart` `completion = PART_COMPLETION_INTERRUPTED` (translated by `handler.ts`); a normal AIMessage emits no `completion` (UNSPECIFIED).

**How**: extend `handler.test.ts` `Handler.ListMessages` (at `:1368`) with a fixture containing an interrupted-block AIMessage. Assert `part.text.completion === "PART_COMPLETION_INTERRUPTED"` (or enum).

```bash
bazel test //projects/game/agent/src:handler_test
```

### Scenario B6 (large test): stall → reconnect → partial output survives

**Validates**: SC-002/SC-003. Start a turn, stream a partial reply, simulate an LLM stall (fake-llm: send partial chunks then stop, connection alive). After the stall (warn + wait), re-enter the session and call `ListMessages` — assert the partial reply is present and its interrupted block carries `completion == game.PartCompletion_PART_COMPLETION_INTERRUPTED` (read via `part.GetText().GetCompletion()` / `part.GetThinking().GetCompletion()` on the Go test side).

**How**: deploy via `guitar run`; use a fake-llm that simulates a mid-stream stall; verify `ListMessages` after reconnect. Since the 2026-08-14 config channel the stall deploy's window is the config-driven 5s (see Phase D) — the case completes in seconds.

```bash
guitar run projects/game/testplan/system_test.yaml   # agent-stall suite（B6 为其 case (c)）
```

**Acceptance**: all test cases pass — the partial reply is NOT lost (the core bug fix).

---

## Phase C — Desktop rendering (spec FR-012/FR-013)

### Scenario C1 (unit): WarnSignal renders a ⚠ bubble (FR-012)

**Validates**: a `FlowPart.warn` renders the `.msg-warn`/`.warn-bubble` (existing behavior, now standardized). This is existing code formalized — verified by diff review (`App.svelte:789-802`, `ChatView.svelte:271-279`); no new unit test needed (no new logic).

### Scenario C2 (unit): interrupted marker renders an indicator (FR-013)

**Validates**: a `ListMessages` part with `completion = PART_COMPLETION_INTERRUPTED` renders a visual "中断"/truncated indicator; a normal part does not. Tested via the pure helper `partInterrupted(part)` (in `api.ts`, alongside `classifyToolResultStatus`) — the desktop `lib_test` globs `.ts` only (no Svelte mount), so the indicator logic is a unit-tested pure function; the Svelte render branches (`ChatView.svelte` agent-text `:284-292`, `ChatMessage.svelte` thinking `:104-115`) consume the helper.

**How**: unit-test `partInterrupted` in `api.test.ts` — interrupted text/thinking → true; normal → false; numeric and string enum forms.

### Scenario C3 (large test): live warn + reconnect interrupted indicator

**Validates**: FR-012/FR-013 end-to-end. During a stall the desktop shows the ⚠ warn bubble; after reconnect it shows the partial reply with the interrupted indicator (the warn bubble is gone — transient).

```bash
guitar run projects/game/testplan/system_test.yaml   # agent-stall suite（C3 的 ListMessages 部分并入 case (c)）
```

---

## Phase D — Service-config channel + large-test rescale (2026-08-14 resume scope)

Spec Clarifications Session 2026-08-14 + [idle-timeout-contract.md](contracts/idle-timeout-contract.md) §5 + [data-model.md](data-model.md) §7. Enablers: [045 deploy-config](../045-deploy-config/spec.md) (config SDK) + [046 fake-llm think chunking](../046-fake-llm-think-chunking/spec.md) (controlled think gaps). Purpose: complete T012 at controlled, fast timings (goal 1) via config-driven timeout selection (goal 2).

### Scenario D1 (unit): `agent-timeouts` resolution matrix

**Validates**: idle-timeout-contract §1/§5. In `agent-timeouts.test.ts` (pure `resolveAgentTimeouts(env, overrides)` — no `vi.mock`): env unset + no overrides → 120s/10s/120s; env idle `45000` → clamped 120s; env idle `90000` → 90s as-is; overrides `{streamIdleTimeoutMs: 5000}` → 5s **as-is (no clamp)**; env + overrides both set → env wins; `heartbeat ≥ idle` (e.g. overrides `{streamIdleTimeoutMs: 5000}` + default 10s heartbeat) → throws; overrides absent fields keep defaults; `streamIdleExplicit` true iff env set OR overrides supply the idle.

```bash
bazel test //projects/game/agent/src:agent_timeouts_test
```

### Scenario D2 (unit): `loadAgentTimeoutOverrides` absence semantics

**Validates**: idle-timeout-contract §5 "absence semantics". `DOMINION_CONFIG_DIR` unset → `undefined`; injected reader rejecting with the SDK's not-selected error → `undefined`; a present entry → parsed partial overrides (deep-merged shape). Reader injected as a parameter (`vi.fn()`), per `style/javascript.md`.

### Scenario D3 (large test): stall suite green at config-driven timings

**Validates**: the config channel end-to-end + all 044 large-test cases (T012, Constitution VI). The suite deploys with `configs: [agent_timeouts]` (5s idle / 2s heartbeat): detection within the bracketed window (~5s), heartbeat-bridged tool wait (12s > 5s window) with NO false stall, partial-output persistence, and — new — a think-gap case: the `think-interrupt-gap` template's finite 15s mid-thinking gap (rescaled from 046's shipped 90s by tasks.md T019) trips the detector during the thinking phase and the two pre-gap reasoning chunks persist as the partial with the interrupted tail marker (validates the 046→044 enabler pair). If the heartbeat case fails again, the per-tick heartbeat logs (D4) discriminate the root cause per [research.md](research.md) R9 — fix then re-run until fully green.

```bash
guitar run projects/game/testplan/system_test.yaml   # agent-stall suite（全 case 通过 = T012 验收）
```

### Scenario D4 (observability, verified during D3): heartbeat per-tick logs

`withIdleHeartbeat` emits a structured `info` log per tick (tool, interval, tick index) + a wrapper start log (`@dominion/common-js-logs`). During D3's heartbeat case these are visible in signoz for the agent service — the discriminator for R9's contingency. No separate test target; confirmed by querying signoz while the suite runs (or by the passing case, since ticks are logged on every long tool wait).

---

## Configuration Override

Two explicit tiers (env > config > code default — [idle-timeout-contract.md](contracts/idle-timeout-contract.md) §1/§5):

| Tier | Knob | Default | Notes |
|---|---|---|---|
| env | `GAME_STREAM_IDLE_TIMEOUT_MS` | `120000` (was `30000`) | Chunk-idle timeout base (FR-001). Values `< 60000` clamp to `120000` (env-scoped guard). Explicit env suppresses the reasoning floor (FR-003). |
| env | `GAME_INIT_TURN_TIMEOUT_MS` | `120000` | Init instruction turn total timeout (043 FR-009). |
| config | `agent_timeouts/timeouts.streamIdleTimeoutMs` | (absent → 120000) | Honored **as-is** — no 60s clamp (code-reviewed service-definition tier). Suppresses the floor. Test-grade value shipped: `5000`. |
| config | `agent_timeouts/timeouts.toolHeartbeatIntervalMs` | (absent → 10000) | Heartbeat cadence (043 FR-003). MUST be `<` resolved idle (startup fail-fast). Test-grade value shipped: `2000`. |
| config | `agent_timeouts/timeouts.initTurnTimeoutMs` | (absent → 120000) | Init-turn total bound. |

To test with shorter effective timeouts (fast cycles, below the env 60s minimum — use the config tier; the stall deploy selects it):

```bash
guitar run projects/game/testplan/system_test.yaml   # agent-stall suite deploys with agent_timeouts (5s/2s)
```

For env-tier overrides (≥60s only) the existing pattern applies:

```bash
GAME_STREAM_IDLE_TIMEOUT_MS=60000 bazel test //projects/game/agent/src:graph_test
```

## Verification Checklist

- [ ] `STREAM_IDLE_TIMEOUT_MS === 120_000`; min 60s enforced at the env read site (unit, A1)
- [ ] reasoning-floor matching, longest-first (unit, A2)
- [ ] graph node `idleTimeout` reflects the floor when a spec is supplied (unit, A3)
- [ ] reasoning-model floor: longest-first matching (unit, A2) + graph-node `idleTimeout` application incl. env-below-floor as-is (unit, A3) — **large-test case dropped** (A4 rationale: any explicit idle source suppresses the floor; a floor large-test needs the default path + >120s gap — γ optional pending spec-owner ruling)
- [ ] `updateState` succeeds after AbortSignal fired — gating spike (unit, B1)
- [ ] `mergePartialBlocks` rules: text/reasoning, tool_call+result kept, tool_call-without-result dropped (unit, B2)
- [ ] stall persists partial output to the stalled node's channel; error re-thrown; warn+wait + retained buffer unchanged (unit, B3)
- [ ] multi-node turn partitions by `err.node` — no duplication (unit, B4)
- [ ] `ListMessages` returns partial output with `completion = PART_COMPLETION_INTERRUPTED` on the tail part (unit, B5)
- [ ] proto round-trip: `TextPart`/`ThinkingPart` with `INTERRUPTED` round-trips through protojson; default omits the field (proto_test.go)
- [ ] agent-timeouts resolution matrix: env clamp / config as-is / env>config / heartbeat≥idle throws (unit, D1)
- [ ] `loadAgentTimeoutOverrides` absence semantics: unset dir / unselected block → defaults (unit, D2)
- [ ] heartbeat per-tick structured logs present on long tool waits (observability, D4)
- [ ] stall → reconnect → partial output survives, `completion == PART_COMPLETION_INTERRUPTED` (large, B6/D3)
- [ ] think-gap case: finite mid-thinking gap detected + chunked-reasoning partial persisted with interrupted tail (large, D3, new)
- [ ] heartbeat-bridged tool wait > configured window with NO false stall — at config-driven 5s/2s timings (large, D3)
- [ ] WarnSignal renders ⚠ bubble (diff review, C1)
- [ ] `partInterrupted` pure helper: interrupted text/thinking → indicator; normal → none (unit, C2)
- [ ] live warn + reconnect interrupted indicator (large, C3)
- [ ] `game.proto:451-453` comment documents `warn` as the rendered exception (diff review, FR-012)
- [ ] `PartCompletion` enum + `TextPart`/`ThinkingPart` `completion` field present with AIP-192 comments (diff review, FR-005)
- [ ] 043 behaviors unchanged — tool heartbeat, buffer retention, abort semantics (regression)
