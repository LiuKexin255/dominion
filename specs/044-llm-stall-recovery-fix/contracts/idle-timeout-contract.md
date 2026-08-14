# Contract: Stream Idle Timeout Resolution

**Feature**: 044-llm-stall-recovery-fix | **Date**: 2026-08-12 (amended 2026-08-14) | **Spec**: [spec.md](../spec.md) FR-001/FR-002/FR-003/FR-008

This contract defines how the chunk-idle timeout for the team graph's `player`/`planner` nodes is resolved at graph-build time. It revises [Feature 043's `STREAM_IDLE_TIMEOUT_MS`](../../043-llm-stream-stall-recovery/spec.md) FR-001 (default 30s → 120s) and adds the per-reasoning-model floor. The 2026-08-14 amendment adds the **service-config channel** (spec Clarifications Session 2026-08-14; enabler [Feature 045](../../045-deploy-config/spec.md)) as a second explicit-configuration tier.

## §1 Resolution rule

For each model-holding node (`player`, `planner`), the effective idle timeout is:

```
effectiveIdleTimeout(modelSpec?) =
    env GAME_STREAM_IDLE_TIMEOUT_MS is set (!== undefined)                  →  env value (clamped: < 60_000 → 120_000)   // explicit tier 1 — deploy-time operator knob
    else config agent_timeouts/timeouts.streamIdleTimeoutMs is present     →  that value, as-is (NO clamp)              // explicit tier 2 — service-definition knob (§5)
    else if getReasoningIdleTimeoutFloor(modelSpec) = F (non-null)         →  max(120_000, F)
    else                                                                   →  120_000
```

Where:
- The env read site keeps FR-001's **60s minimum clamp**: values `< 60_000` resolve to `120_000` (`projects/game/agent/src/llm.ts`). The clamp is scoped to the raw env channel — a typo-guard, not a property of "explicitness" itself.
- The config value is honored **as-is** (validated finite and `> 0`), including values below 60s: a config block is a deliberate, code-reviewed declaration in `service.yaml`, selected knowingly by a deploy (045 FR-008 selection-only) — a different trust tier from a raw env string. This is the relaxation that lets large tests run at short windows.
- `getReasoningIdleTimeoutFloor` is defined in `projects/game/agent/src/reasoning-timeouts.ts` (§2).
- `STREAM_IDLE_TIMEOUT_EXPLICIT` (consumed by `resolveStreamIdleTimeout`) is true when **either** the env var is set **or** the config entry supplies `streamIdleTimeoutMs` — both are "the operator's explicit configuration" per FR-003.

### Invariants

1. **The floor only ever raises the code default.** `max(120s, floor)` — for a matching reasoning model on the default path; for a non-matching model it equals the default. Any explicit source (env or config) **suppresses** the floor entirely (explicit wins as-is, even below the floor — spec FR-003, Hermes `max(default, floor)` semantics).
2. **Explicit operator config always wins, ordered env > config.** When both are set the deploy-time env override beats the service-definition value (the deploy is the more specific operational layer, preserving 043/044's existing env semantics); in practice a deploy sets at most one of them (the stall deploy sets config only).
3. **Heartbeat < idle is load-bearing.** The resolved `toolHeartbeatIntervalMs` MUST be strictly less than the resolved idle timeout (043 FR-003); violation throws at startup (§5 validation) rather than silently enabling false stalls mid-tool.
4. **Scope: `player`/`planner` only.** The timeout is applied via per-node `addNode({ timeout })`, NOT `setNodeDefaults` — `initInstruction`/`postCompactInstruction`/`compress` are excluded (unchanged from 043, `team/graph.ts:373-376`); they remain covered by the init-turn total timeout (043 FR-009).

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

- Unit: `STREAM_IDLE_TIMEOUT_MS === 120_000` (env unset, config absent); env override honored; env values `< 60_000` clamped (env-only — the config tier bypasses the clamp by design).
- Unit: `getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash") === 600_000`; `getReasoningIdleTimeoutFloor("gpt-4") === null`; longest-first (`o3-mini-` distinct from `o3-`).
- Unit (`graph.test.ts`): when `playerModelSpec` is a reasoning model, `nodes.player.timeout.idleTimeout` reflects `max(default, floor)`; when omitted, equals `STREAM_IDLE_TIMEOUT_MS`.
- Unit (agent-timeouts): the §5 resolution matrix — config as-is including `< 60_000`; env beats config when both set; heartbeat ≥ idle throws; absent block / unset `DOMINION_CONFIG_DIR` → defaults.
- Large test: the agent-stall suite runs at the §5 test-grade values — detection within the bracketed configured window, heartbeat-bridged tool wait > window with no false stall, stall cases complete in seconds instead of minutes.

## §5 Service-config channel (2026-08-14 amendment)

**Enabler**: [Feature 045 — Deploy Config Support](../../045-deploy-config/spec.md) (JS SDK `readConfig<T>(block, key, defaults)` from `@dominion/common-js-config`; runtime contract [runtime-contract.md](../../045-deploy-config/contracts/runtime-contract.md) — `DOMINION_CONFIG_DIR` discovery, `{dir}/{block}/{key}` layout, error when the block is not selected).

### Declaration & selection

`projects/game/agent/service.yaml` gains ONE top-level config block whose shipped values are **test-grade**:

```yaml
configs:
  # agent_timeouts — TEST-GRADE timeout overrides for the stall-recovery
  # large-test suite. Values below FR-001's 60s env minimum are allowed ONLY
  # through this deliberate, code-reviewed channel (idle-timeout-contract §1:
  # the 60s clamp is env-scoped). Production deploys MUST NOT select this
  # block; unselected, the agent runs code defaults (120s idle / 10s
  # heartbeat / 120s init-turn) exactly as before.
  - name: agent_timeouts
    data:
      - name: timeouts
        value: |
          streamIdleTimeoutMs: 5000
          toolHeartbeatIntervalMs: 2000
        type: yaml
```

`projects/game/testplan/deploy_agent_stall.yaml` selects it on the `agent_test` artifact and drops the former `GAME_STREAM_IDLE_TIMEOUT_MS` env (avoiding the both-set ambiguity; the config tier is the suite's single explicit source):

```yaml
  - artifact:
      path: //projects/game/agent/service.yaml
      name: agent_test
      configs:
        - agent_timeouts
```

### Resolution & wiring

New module `projects/game/agent/src/agent-timeouts.ts`:

```typescript
export interface AgentTimeouts {
  streamIdleTimeoutMs: number;   // default 120_000
  toolHeartbeatIntervalMs: number; // default 10_000
  initTurnTimeoutMs: number;     // default 120_000
}
export const AGENT_TIMEOUTS_CONFIG_BLOCK = "agent_timeouts";
export const AGENT_TIMEOUTS_CONFIG_KEY = "timeouts";
```

- `loadAgentTimeoutOverrides(reader = readConfig): Partial<AgentTimeouts> | undefined` — `try { reader(block, key, {}) } catch { return undefined }`. Swallowing the SDK's errors (unset `DOMINION_CONFIG_DIR`, block not selected, unparseable content — the SDK throws the same `Error` type for all three, [045 sdk-js.md](../../045-deploy-config/contracts/sdk-js.md) §1, so they are NOT individually discriminable without fragile message sniffing) is a **deliberate divergence** from 045 US3.3 ("error indicates environment mismatch"): the agent treats the block as an *optional override channel* because production and standard deploys legitimately run without it. The tradeoff for a **present-but-malformed** file is a silent fallback to defaults — acceptable because the only selector of this block is the stall suite, whose window-bracket assertions fail loudly (detection at 120s ≫ the 10s bracket) rather than passing vacuously; this is documented rather than engineered around (the alternative — message sniffing or an SDK error-kind API — adds coupling for a test-grade channel).
- `resolveAgentTimeouts(env, overrides?)` — pure function implementing the matrix (per parameter, `env > overrides > default`; env idle clamped per FR-001; config values validated finite & `> 0` and taken as-is; heartbeat ≥ idle → `throw`). Returns the resolved values plus `streamIdleExplicit` (env set OR overrides supply the idle).
- `llm.ts` re-exports its existing constant names sourced from the resolver (`STREAM_IDLE_TIMEOUT_MS`, `STREAM_IDLE_TIMEOUT_EXPLICIT = streamIdleExplicit`, `INIT_TURN_TIMEOUT_MS`, `TOOL_HEARTBEAT_INTERVAL_MS`), so `reasoning-timeouts.ts`, `team/graph.ts`, and `session-team.ts` imports are unchanged. The read happens once at module load (SDK is synchronous-by-design, startup-time).

### Testability

- The pure resolver is unit-tested directly with synthetic `env`/`overrides` (no `vi.mock` — per `style/javascript.md` DI conventions).
- In the unit-test environment `DOMINION_CONFIG_DIR` is unset → `loadAgentTimeoutOverrides()` naturally returns `undefined` → existing `vi.resetModules` + dynamic-import env tests keep working unchanged.

### Observability (heartbeat ticks)

`withIdleHeartbeat` (`projects/game/agent/src/llm.ts`) gains a structured `info` log per tick (`@dominion/common-js-logs`, mirroring fake-llm 046 FR-018's per-chunk pattern): tool name, configured interval, tick sequence — plus a wrapper start log. This is the discriminator for the R9 root-cause contingency: ticks-logged + false stall → LangGraph `touch()` path issue; ticks-absent → wrapper timer lifecycle bug.
