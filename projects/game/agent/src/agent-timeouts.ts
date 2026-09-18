/**
 * agent-timeouts.ts — Agent timeout parameter resolution over the 045
 * service-config channel (specs/044-llm-stall-recovery-fix/contracts/
 * idle-timeout-contract.md §5; specs/044-llm-stall-recovery-fix/data-model.md
 * §7).
 *
 * Each timeout parameter resolves as
 * `env (explicit, clamped) > service config (explicit, as-is) > code default`
 * (idle-timeout-contract.md §1). The config tier is an OPTIONAL override
 * channel: production and the standard suite select no block, so
 * `loadAgentTimeoutOverrides` treats a missing or unparseable entry as
 * absence — the SDK throws the same plain `Error` for unset
 * `DOMINION_CONFIG_DIR`, an unselected block and unparseable content
 * (specs/045-deploy-config/contracts/sdk-js.md §1), so they are uniformly
 * treated as "not selected" (the deliberate divergence from 045 US3.3 is
 * recorded in idle-timeout-contract.md §5).
 */

import { readConfig } from "@dominion/common-js-config";

/**
 * The agent's timeout parameters. Defaults (code tier):
 * 120s chunk idle / 10s tool heartbeat / 120s init-turn total.
 */
export interface AgentTimeouts {
	/** Chunk-idle timeout for the team graph's player/planner nodes (ms). */
	streamIdleTimeoutMs: number;
	/** Idle-heartbeat cadence for MCP tool invocations (ms). */
	toolHeartbeatIntervalMs: number;
	/** Total execution timeout for the async init instruction turn (ms). */
	initTurnTimeoutMs: number;
}

/** Code-tier defaults (idle-timeout-contract.md §1; data-model.md §7.2). */
export const DEFAULT_AGENT_TIMEOUTS: AgentTimeouts = {
	streamIdleTimeoutMs: 120_000,
	toolHeartbeatIntervalMs: 10_000,
	initTurnTimeoutMs: 120_000,
};

/** Config block name (first `readConfig` addressing parameter). */
export const AGENT_TIMEOUTS_CONFIG_BLOCK = "agent_timeouts";

/** Config entry name within the block (second `readConfig` parameter). */
export const AGENT_TIMEOUTS_CONFIG_KEY = "timeouts";

/**
 * The `readConfig` call signature, instantiated to plain-object defaults so a
 * test can inject a `vi.fn()` with a concrete return shape (the generic
 * `readConfig` is assignable to this non-generic form).
 */
type ConfigReader = (
	block: string,
	key: string,
	defaults: object,
) => object;

/**
 * Read the optional timeout overrides from the 045 service-config channel.
 *
 * Returns `undefined` when the entry is absent: `DOMINION_CONFIG_DIR` unset,
 * the block not selected by the deploy, or the file unparseable — the SDK
 * throws the same `Error` for all three
 * (specs/045-deploy-config/contracts/sdk-js.md §1), so none is individually
 * discriminable and all are treated as absence (idle-timeout-contract.md §5;
 * data-model.md §7.4). The reader is injected for testability
 * (style/javascript.md §Mock 约定 — no module mocking).
 */
export function loadAgentTimeoutOverrides(
	reader: ConfigReader = readConfig,
): Partial<AgentTimeouts> | undefined {
	try {
		return reader(
			AGENT_TIMEOUTS_CONFIG_BLOCK,
			AGENT_TIMEOUTS_CONFIG_KEY,
			{},
		) as Partial<AgentTimeouts>;
	} catch {
		return undefined;
	}
}

/** Resolved timeout parameters plus the idle-explicitness flag. */
export interface ResolvedAgentTimeouts extends AgentTimeouts {
	/**
	 * Whether the resolved idle timeout came from an explicit source — the
	 * env var is set OR the config entry supplies `streamIdleTimeoutMs`
	 * (idle-timeout-contract.md §1, explicit tiers 1/2). Consumed by
	 * `resolveStreamIdleTimeout` (reasoning-timeouts.ts) to honor explicit
	 * operator config as-is, even below a reasoning floor (spec FR-003).
	 */
	streamIdleExplicit: boolean;
}

/**
 * Resolve the agent's timeout parameters (idle-timeout-contract.md §1/§5
 * matrix). Pure function — no I/O, no module state.
 *
 * Per parameter, env > overrides > default:
 * - idle: env set → `Number(env) >= 60_000 ? value : 120_000` (the 60s
 *   minimum clamp is env-scoped — FR-001, a typo-guard for the raw env
 *   channel); else config `streamIdleTimeoutMs` (finite && > 0) as-is, NO
 *   clamp (a code-reviewed service-definition tier); else 120_000.
 * - heartbeat: config `toolHeartbeatIntervalMs` (finite && > 0) as-is; else
 *   10_000. There is intentionally NO env channel for the heartbeat
 *   (research.md R10 — Q1 decision).
 * - init: env `GAME_INIT_TURN_TIMEOUT_MS` (`Number(...) || 120_000`); else
 *   config `initTurnTimeoutMs` (finite && > 0); else 120_000.
 *
 * Invariant (043 FR-003): the resolved heartbeat MUST be strictly less than
 * the resolved idle timeout — violation throws at startup with both values
 * and their sources, rather than silently enabling false stalls mid-tool.
 */
export function resolveAgentTimeouts(
	env: { streamIdleTimeoutMs?: string; initTurnTimeoutMs?: string },
	overrides?: Partial<AgentTimeouts>,
): ResolvedAgentTimeouts {
	// -- stream idle -------------------------------------------------------
	const envIdle = env.streamIdleTimeoutMs;
	const configIdleMs = overrides?.streamIdleTimeoutMs;
	let streamIdleTimeoutMs: number;
	let idleSource: "env" | "config" | "default";
	if (envIdle !== undefined) {
		const value = Number(envIdle);
		streamIdleTimeoutMs =
			value >= 60_000 ? value : DEFAULT_AGENT_TIMEOUTS.streamIdleTimeoutMs;
		idleSource = "env";
	} else if (
		configIdleMs !== undefined &&
		Number.isFinite(configIdleMs) &&
		configIdleMs > 0
	) {
		streamIdleTimeoutMs = configIdleMs;
		idleSource = "config";
	} else {
		streamIdleTimeoutMs = DEFAULT_AGENT_TIMEOUTS.streamIdleTimeoutMs;
		idleSource = "default";
	}

	// -- tool heartbeat (no env channel — research.md R10) ----------------
	const configHeartbeatMs = overrides?.toolHeartbeatIntervalMs;
	let toolHeartbeatIntervalMs: number;
	let heartbeatSource: "config" | "default";
	if (
		configHeartbeatMs !== undefined &&
		Number.isFinite(configHeartbeatMs) &&
		configHeartbeatMs > 0
	) {
		toolHeartbeatIntervalMs = configHeartbeatMs;
		heartbeatSource = "config";
	} else {
		toolHeartbeatIntervalMs = DEFAULT_AGENT_TIMEOUTS.toolHeartbeatIntervalMs;
		heartbeatSource = "default";
	}

	// -- init turn ----------------------------------------------------------
	const envInit = env.initTurnTimeoutMs;
	const configInitMs = overrides?.initTurnTimeoutMs;
	let initTurnTimeoutMs: number;
	if (envInit !== undefined) {
		initTurnTimeoutMs =
			Number(envInit) || DEFAULT_AGENT_TIMEOUTS.initTurnTimeoutMs;
	} else if (
		configInitMs !== undefined &&
		Number.isFinite(configInitMs) &&
		configInitMs > 0
	) {
		initTurnTimeoutMs = configInitMs;
	} else {
		initTurnTimeoutMs = DEFAULT_AGENT_TIMEOUTS.initTurnTimeoutMs;
	}

	if (toolHeartbeatIntervalMs >= streamIdleTimeoutMs) {
		throw new Error(
			`toolHeartbeatIntervalMs (${toolHeartbeatIntervalMs}ms, from ${heartbeatSource}) ` +
				`must be strictly less than streamIdleTimeoutMs (${streamIdleTimeoutMs}ms, ` +
				`from ${idleSource}) — 043 FR-003 invariant ` +
				"(specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md §1)",
		);
	}

	return {
		streamIdleTimeoutMs,
		toolHeartbeatIntervalMs,
		initTurnTimeoutMs,
		streamIdleExplicit:
			envIdle !== undefined || configIdleMs !== undefined,
	};
}
