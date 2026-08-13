# Contract: Stream Idle Timeout Resolution

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 | **Spec**: [spec.md](../spec.md) FR-001/FR-002/FR-003/FR-008

This contract defines how the chunk-idle timeout for the team graph's `player`/`planner` nodes is resolved at graph-build time. It revises [Feature 043's `STREAM_IDLE_TIMEOUT_MS`](../../043-llm-stream-stall-recovery/spec.md) FR-001 (default 30s → 120s) and adds the per-reasoning-model floor.

## §1 Resolution rule

For each model-holding node (`player`, `planner`), the effective idle timeout is:

```
effectiveIdleTimeout(modelSpec?) =
    env GAME_STREAM_IDLE_TIMEOUT_MS is set (> 0)  →  that value            // explicit operator config — always wins, as-is
    else if getReasoningIdleTimeoutFloor(modelSpec) = F (non-null)         →  max(STREAM_IDLE_TIMEOUT_MS, F)
    else                                                                   →  STREAM_IDLE_TIMEOUT_MS
```

Where:
- `STREAM_IDLE_TIMEOUT_MS = Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) || 120_000` (`projects/game/agent/src/llm.ts:43-44`). **Default 120s** (was 30s).
- The minimum configurable value is **60s**: the env-var read site rejects/clamps values `< 60_000` to `120_000` (spec FR-001/US1.3).
- `getReasoningIdleTimeoutFloor` is defined in `projects/game/agent/src/reasoning-timeouts.ts` (§2).

### Invariants

1. **The floor only ever raises.** `max(default, floor)` — the effective timeout for a matching reasoning model is never below the default; for a non-matching model it equals the default.
2. **Explicit operator config always wins.** Because the env var is the source of `STREAM_IDLE_TIMEOUT_MS`, an operator setting it (even below a floor) fixes the base; the floor never overrides an explicit operator choice (spec FR-003, matching Hermes `max(default, floor)` semantics — [commit 27c486e](https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa)).
3. **Scope: `player`/`planner` only.** The timeout is applied via per-node `addNode({ timeout })`, NOT `setNodeDefaults` — `initInstruction`/`postCompactInstruction`/`compress` are excluded (unchanged from 043, `team/graph.ts:373-376`); they remain covered by the init-turn total timeout (043 FR-009).

## §2 Reasoning-floor matching

`getReasoningIdleTimeoutFloor(modelSpec: string): number | null`:

1. `bare = parseModelSpec(modelSpec)` — strips `{provider}/` (`projects/game/agent/src/model-provider.ts:26-32`).
2. Lowercase `bare`.
3. Match against `REASONING_IDLE_TIMEOUT_FLOOR` (an explicit `ReadonlyArray<readonly [substring, floorMs]>`), **longest-substring-first** (entries sorted by `substring.length` descending). Return the first matching entry's `floorMs`, else `null`.

The table and exact values are in [data-model.md §2](../data-model.md#2-reasoning-model-floor-allowlist-entity). DeepSeek family → 600_000ms (the proven case). The table is the single source of truth; adding a reasoning model = append a row + unit test.

## §3 Application point

`projects/game/agent/src/team/graph.ts:383-389` — `TeamGraphDeps` gains **optional** `playerModelSpec?: string` / `plannerModelSpec?: string`. The builder:

```typescript
const playerIdle = resolveStreamIdleTimeout(deps.playerModelSpec);
const plannerIdle = resolveStreamIdleTimeout(deps.plannerModelSpec);
graph
  .addNode("player", playerNode, { timeout: { idleTimeout: playerIdle, refreshOn: "auto" } })
  .addNode("planner", plannerNode, { timeout: { idleTimeout: plannerIdle, refreshOn: "auto" } });
```

`resolveStreamIdleTimeout(spec?)` returns `STREAM_IDLE_TIMEOUT_MS` when `spec` is omitted (so existing test call sites that pass no spec keep the default — backward compatible).

Production wiring (`projects/game/agent/src/server.ts:260,335`) passes the profile's model specs (available via `prompt-client.ts`).

## §4 Verification

- Unit: `STREAM_IDLE_TIMEOUT_MS === 120_000` (env unset); env override honored; values `< 60_000` clamped.
- Unit: `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first (`o3-mini-` distinct from `o3-`).
- Unit (`graph.test.ts`): when `playerModelSpec` is a reasoning model, `nodes.player.timeout.idleTimeout` reflects `max(default, floor)`; when omitted, equals `STREAM_IDLE_TIMEOUT_MS`.
- Large test: `deepseek-v4-flash` + saolei completes a game without a reasoning-thinking-induced stall (spec SC-001/SC-005).
