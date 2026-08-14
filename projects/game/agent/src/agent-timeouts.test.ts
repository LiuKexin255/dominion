/**
 * agent-timeouts.test.ts — Resolution matrix + absence semantics for the
 * agent timeout config channel (specs/044-llm-stall-recovery-fix/quickstart.md
 * Phase D scenarios D1/D2; tasks.md T014).
 *
 * `resolveAgentTimeouts` is tested as a pure function with synthetic
 * env/overrides; `loadAgentTimeoutOverrides` receives an injected `vi.fn()`
 * reader — no module mocking (style/javascript.md §Mock 约定).
 */

import { describe, expect, it, vi } from "vitest";

import {
	AGENT_TIMEOUTS_CONFIG_BLOCK,
	AGENT_TIMEOUTS_CONFIG_KEY,
	DEFAULT_AGENT_TIMEOUTS,
	loadAgentTimeoutOverrides,
	resolveAgentTimeouts,
} from "./agent-timeouts";

// ===========================================================================
// resolveAgentTimeouts — D1 matrix (idle-timeout-contract.md §1/§5)
// ===========================================================================

describe("resolveAgentTimeouts (D1 matrix)", () => {
	const emptyEnv = {};

	it("defaults to 120s/10s/120s with no env and no overrides", () => {
		const r = resolveAgentTimeouts(emptyEnv);
		expect(r).toEqual({
			...DEFAULT_AGENT_TIMEOUTS,
			streamIdleExplicit: false,
		});
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.toolHeartbeatIntervalMs).toBe(10_000);
		expect(r.initTurnTimeoutMs).toBe(120_000);
	});

	it("clamps an env idle value below the 60s minimum to 120_000 (env-scoped clamp)", () => {
		const r = resolveAgentTimeouts({ streamIdleTimeoutMs: "45000" });
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("clamps env idle '0' to 120_000 but still marks it explicit", () => {
		// T002 boundary: "0" is explicitly SET — the flag must stay true
		// (idle-timeout-contract.md §1 uses !== undefined, not Number truthiness).
		const r = resolveAgentTimeouts({ streamIdleTimeoutMs: "0" });
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("honors an env idle value >= 60s as-is", () => {
		const r = resolveAgentTimeouts({ streamIdleTimeoutMs: "90000" });
		expect(r.streamIdleTimeoutMs).toBe(90_000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("clamps a non-numeric env idle value to the default (NaN < 60s) but marks it explicit", () => {
		const r = resolveAgentTimeouts({ streamIdleTimeoutMs: "abc" });
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("honors a config idle override as-is, including below 60s (no clamp on the config tier)", () => {
		// A sub-60s idle must pair with a sub-idle heartbeat to satisfy the
		// FR-003 invariant; the idle value itself is taken as-is, no clamp
		// (the shipped test-grade pair — idle-timeout-contract.md §5).
		const r = resolveAgentTimeouts(emptyEnv, {
			streamIdleTimeoutMs: 5000,
			toolHeartbeatIntervalMs: 2000,
		});
		expect(r.streamIdleTimeoutMs).toBe(5000);
		expect(r.toolHeartbeatIntervalMs).toBe(2000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("env beats config when both supply the idle timeout", () => {
		const r = resolveAgentTimeouts(
			{ streamIdleTimeoutMs: "90000" },
			{ streamIdleTimeoutMs: 5000 },
		);
		expect(r.streamIdleTimeoutMs).toBe(90_000);
		expect(r.streamIdleExplicit).toBe(true);
	});

	it("ignores an invalid config idle override (non-finite) and falls back to the default", () => {
		expect(
			resolveAgentTimeouts(emptyEnv, {
				streamIdleTimeoutMs: Number.NaN,
			}).streamIdleTimeoutMs,
		).toBe(120_000);
		expect(
			resolveAgentTimeouts(emptyEnv, {
				streamIdleTimeoutMs: 0,
			}).streamIdleTimeoutMs,
		).toBe(120_000);
		expect(
			resolveAgentTimeouts(emptyEnv, {
				streamIdleTimeoutMs: -1,
			}).streamIdleTimeoutMs,
		).toBe(120_000);
	});

	it("honors a config heartbeat override as-is", () => {
		const r = resolveAgentTimeouts(emptyEnv, {
			toolHeartbeatIntervalMs: 2000,
		});
		expect(r.toolHeartbeatIntervalMs).toBe(2000);
	});

	it("falls back to the default heartbeat without a config override (no env channel)", () => {
		// heartbeat has NO env tier (research.md R10) — an env-only idle
		// override must not affect it.
		const r = resolveAgentTimeouts({ streamIdleTimeoutMs: "90000" });
		expect(r.toolHeartbeatIntervalMs).toBe(10_000);
	});

	it("throws when the resolved heartbeat >= resolved idle (config idle 5000 + default heartbeat 10s)", () => {
		// The idle parameter alone resolves as-is to 5000 — the throw is the
		// whole-pair FR-003 invariant, not a rejection of the idle value.
		expect(() =>
			resolveAgentTimeouts(emptyEnv, { streamIdleTimeoutMs: 5000 }),
		).toThrowError(/toolHeartbeatIntervalMs \(10000ms, from default\)/);
		expect(() =>
			resolveAgentTimeouts(emptyEnv, { streamIdleTimeoutMs: 5000 }),
		).toThrowError(/streamIdleTimeoutMs \(5000ms, from config\)/);
		expect(() =>
			resolveAgentTimeouts(emptyEnv, { streamIdleTimeoutMs: 5000 }),
		).toThrowError(/043 FR-003 invariant/);
	});

	it("throws on the equality boundary (heartbeat == idle)", () => {
		expect(() =>
			resolveAgentTimeouts(emptyEnv, {
				streamIdleTimeoutMs: 10_000,
			}),
		).toThrowError(/must be strictly less/);
	});

	it("throws when a config heartbeat override exceeds the default idle", () => {
		expect(() =>
			resolveAgentTimeouts(emptyEnv, {
				toolHeartbeatIntervalMs: 200_000,
			}),
		).toThrowError(
			/toolHeartbeatIntervalMs \(200000ms, from config\).*streamIdleTimeoutMs \(120000ms, from default\)/,
		);
	});

	it("fills unset override fields with defaults (partial overrides)", () => {
		const r = resolveAgentTimeouts(emptyEnv, {
			toolHeartbeatIntervalMs: 2000,
		});
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.toolHeartbeatIntervalMs).toBe(2000);
		expect(r.initTurnTimeoutMs).toBe(120_000);
	});

	it("resolves the init-turn timeout from env GAME_INIT_TURN_TIMEOUT_MS", () => {
		const r = resolveAgentTimeouts({ initTurnTimeoutMs: "30000" });
		expect(r.initTurnTimeoutMs).toBe(30_000);
	});

	it("falls back to the 120s init-turn default for a falsy env value", () => {
		// `Number(...) || default` semantics — the pre-amendment read site.
		expect(
			resolveAgentTimeouts({ initTurnTimeoutMs: "abc" }).initTurnTimeoutMs,
		).toBe(120_000);
		expect(
			resolveAgentTimeouts({ initTurnTimeoutMs: "0" }).initTurnTimeoutMs,
		).toBe(120_000);
	});

	it("honors a config init-turn override as-is", () => {
		const r = resolveAgentTimeouts(emptyEnv, { initTurnTimeoutMs: 60_000 });
		expect(r.initTurnTimeoutMs).toBe(60_000);
	});

	it("env beats config for the init-turn timeout", () => {
		const r = resolveAgentTimeouts(
			{ initTurnTimeoutMs: "30000" },
			{ initTurnTimeoutMs: 60_000 },
		);
		expect(r.initTurnTimeoutMs).toBe(30_000);
	});

	it("marks streamIdleExplicit true iff env is set OR config supplies the idle field", () => {
		// Truth table: env only / config only / both / neither.
		expect(
			resolveAgentTimeouts({ streamIdleTimeoutMs: "90000" })
				.streamIdleExplicit,
		).toBe(true);
		expect(
			resolveAgentTimeouts(emptyEnv, {
				streamIdleTimeoutMs: 5000,
				toolHeartbeatIntervalMs: 2000,
			}).streamIdleExplicit,
		).toBe(true);
		expect(
			resolveAgentTimeouts(
				{ streamIdleTimeoutMs: "90000" },
				{ streamIdleTimeoutMs: 5000 },
			).streamIdleExplicit,
		).toBe(true);
		expect(
			resolveAgentTimeouts(emptyEnv, {
				initTurnTimeoutMs: 60_000,
			}).streamIdleExplicit,
		).toBe(false);
		// An env init-turn override does not make the idle explicit.
		expect(
			resolveAgentTimeouts({ initTurnTimeoutMs: "30000" })
				.streamIdleExplicit,
		).toBe(false);
	});

	it("treats a config idle override that is merely present as explicit, even when invalid", () => {
		// "overrides provide the idle" = the field is supplied; the value's
		// validity only affects the resolved number (data-model.md §7.2).
		const r = resolveAgentTimeouts(emptyEnv, {
			streamIdleTimeoutMs: Number.NaN,
		});
		expect(r.streamIdleTimeoutMs).toBe(120_000);
		expect(r.streamIdleExplicit).toBe(true);
	});
});

// ===========================================================================
// loadAgentTimeoutOverrides — D2 absence semantics (idle-timeout-contract.md
// §5; data-model.md §7.4). Reader injected as a parameter (vi.fn()).
// ===========================================================================

describe("loadAgentTimeoutOverrides (D2 absence semantics)", () => {
	it("returns undefined when the reader throws (unset dir / unselected block / unparseable)", () => {
		const reader = vi.fn(() => {
			throw new Error("DOMINION_CONFIG_DIR is not set; ...");
		});
		expect(loadAgentTimeoutOverrides(reader)).toBeUndefined();
		// Positive assertion that the injected reader was actually exercised
		// (style/javascript.md §规则：验证 mock 确实生效).
		expect(reader).toHaveBeenCalledWith(
			AGENT_TIMEOUTS_CONFIG_BLOCK,
			AGENT_TIMEOUTS_CONFIG_KEY,
			{},
		);
	});

	it("returns the parsed partial overrides when the reader succeeds", () => {
		const reader = vi.fn(() => ({
			streamIdleTimeoutMs: 5000,
			toolHeartbeatIntervalMs: 2000,
		}));
		expect(loadAgentTimeoutOverrides(reader)).toEqual({
			streamIdleTimeoutMs: 5000,
			toolHeartbeatIntervalMs: 2000,
		});
	});

	it("returns a partial shape for a reader that supplies only some fields", () => {
		const reader = vi.fn(() => ({ initTurnTimeoutMs: 60_000 }));
		expect(loadAgentTimeoutOverrides(reader)).toEqual({
			initTurnTimeoutMs: 60_000,
		});
	});

	it("returns an empty partial for an empty entry (deep-merged defaults stay empty)", () => {
		const reader = vi.fn(() => ({}));
		expect(loadAgentTimeoutOverrides(reader)).toEqual({});
	});
});
