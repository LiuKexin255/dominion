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

### Scenario A4 (large test): reasoning model completes a saolei game without reasoning-induced stall

**Validates**: SC-001/SC-005. `deepseek-v4-flash` (600s floor) plays a saolei game; its deep-thinking phase (previously > 30s → false stall) no longer aborts the turn.

**How**: deploy the SUT via `guitar run <plan.yaml>` with a reasoning model configured; play one game; assert no `NodeTimeoutError` fires during reasoning phases and the game completes.

```bash
# via the testplan skill — full deploy → test → cleanup loop
guitar run projects/game/testplan/system_test.yaml   # agent-stall suite（A4 为其 case (a)）
```

**Acceptance**: all test cases pass (a reasoning-thinking-induced stall is a failure).

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

**How**: deploy via `guitar run`; use a fake-llm that simulates a mid-stream stall; verify `ListMessages` after reconnect.

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

## Configuration Override

| Environment Variable | Default | Description |
|---|---|---|
| `GAME_STREAM_IDLE_TIMEOUT_MS` | `120000` (was `30000`) | Chunk-idle timeout base for player/planner (FR-001). MUST be ≥ `60000`; values below are clamped. A reasoning-model floor (FR-002) raises the effective timeout above this via `max(default, floor)`. |
| `GAME_INIT_TURN_TIMEOUT_MS` | `120000` | Init instruction turn total timeout (043 FR-009 — unchanged). |

To test with shorter effective timeouts (faster cycles — respects the 60s minimum):

```bash
GAME_STREAM_IDLE_TIMEOUT_MS=60000 bazel test //projects/game/agent/src:graph_test
```

## Verification Checklist

- [ ] `STREAM_IDLE_TIMEOUT_MS === 120_000`; min 60s enforced (unit, A1)
- [ ] reasoning-floor matching, longest-first (unit, A2)
- [ ] graph node `idleTimeout` reflects the floor when a spec is supplied (unit, A3)
- [ ] reasoning model completes a saolei game without reasoning-induced stall (large, A4)
- [ ] `updateState` succeeds after AbortSignal fired — gating spike (unit, B1)
- [ ] `mergePartialBlocks` rules: text/reasoning, tool_call+result kept, tool_call-without-result dropped (unit, B2)
- [ ] stall persists partial output to the stalled node's channel; error re-thrown; warn+wait + retained buffer unchanged (unit, B3)
- [ ] multi-node turn partitions by `err.node` — no duplication (unit, B4)
- [ ] `ListMessages` returns partial output with `completion = PART_COMPLETION_INTERRUPTED` on the tail part (unit, B5)
- [ ] proto round-trip: `TextPart`/`ThinkingPart` with `INTERRUPTED` round-trips through protojson; default omits the field (proto_test.go)
- [ ] stall → reconnect → partial output survives, `completion == PART_COMPLETION_INTERRUPTED` (large, B6)
- [ ] WarnSignal renders ⚠ bubble (diff review, C1)
- [ ] `partInterrupted` pure helper: interrupted text/thinking → indicator; normal → none (unit, C2)
- [ ] live warn + reconnect interrupted indicator (large, C3)
- [ ] `game.proto:451-453` comment documents `warn` as the rendered exception (diff review, FR-012)
- [ ] `PartCompletion` enum + `TextPart`/`ThinkingPart` `completion` field present with AIP-192 comments (diff review, FR-005)
- [ ] 043 behaviors unchanged — tool heartbeat, buffer retention, abort semantics (regression)
