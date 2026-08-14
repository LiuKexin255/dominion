/**
 * reasoning-timeouts.test.ts — Tests for the per-reasoning-model idle-timeout
 * floor (044 US2 T003, specs/044-llm-stall-recovery-fix/tasks.md T003 —
 * quickstart.md A2): allowlist matching (longest-substring-first, case
 * insensitive, provider-prefix stripping) and the resolution rule order of
 * `resolveStreamIdleTimeout`
 * (specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md §1).
 *
 * `resolveStreamIdleTimeout` reads module-level constants from `llm.ts` that
 * are evaluated at import time, so the env-dependent cases re-import the
 * module after mutating the env var (vi.resetModules + dynamic import — no
 * module mocking, per style/javascript.md §Mock 约定; same pattern as the
 * 044 US1 block at the end of llm.test.ts).
 */

import { afterEach, describe, expect, it, vi } from "vitest";

import { getReasoningIdleTimeoutFloor } from "./reasoning-timeouts";

// ===========================================================================
// getReasoningIdleTimeoutFloor — allowlist matching (env-independent)
// ===========================================================================

describe("getReasoningIdleTimeoutFloor (044 US2 T003 — floor matching)", () => {
	it("strips the {provider}/ prefix and matches deepseek-v4-flash to the 600s floor", () => {
		expect(getReasoningIdleTimeoutFloor("openai/deepseek-v4-flash")).toBe(
			600_000,
		);
	});

	it("matches a bare spec without a provider prefix", () => {
		expect(getReasoningIdleTimeoutFloor("deepseek-r1")).toBe(600_000);
		expect(getReasoningIdleTimeoutFloor("deepseek-reasoner")).toBe(600_000);
	});

	it("returns null for a model not in the allowlist", () => {
		expect(getReasoningIdleTimeoutFloor("gpt-4")).toBeNull();
	});

	it("matches longest-substring-first: o3-mini- resolves to 300_000, not o3-'s 600_000", () => {
		expect(getReasoningIdleTimeoutFloor("openai/o3-mini-high")).toBe(300_000);
	});

	it("matches o4-mini- to the 300s floor", () => {
		expect(getReasoningIdleTimeoutFloor("openai/o4-mini-2026-08-13")).toBe(
			300_000,
		);
	});

	it("never matches olmo-1 against the o1- substring (Hermes-documented pitfall)", () => {
		expect(getReasoningIdleTimeoutFloor("openai/olmo-1")).toBeNull();
	});

	it("matches claude-opus- to the 240s floor", () => {
		expect(getReasoningIdleTimeoutFloor("anthropic/claude-opus-4")).toBe(
			240_000,
		);
	});

	it("is case-insensitive", () => {
		expect(
			getReasoningIdleTimeoutFloor("OpenAI/DeepSeek-V4-Flash"),
		).toBe(600_000);
	});
});

// ===========================================================================
// resolveStreamIdleTimeout — resolution rule order (env-dependent)
// ===========================================================================

describe("resolveStreamIdleTimeout (044 US2 T003 — resolution rule order)", () => {
	const envKey = "GAME_STREAM_IDLE_TIMEOUT_MS";
	const original = process.env[envKey];

	afterEach(() => {
		if (original === undefined) delete process.env[envKey];
		else process.env[envKey] = original;
	});

	async function reloadModule(): Promise<{
		mod: typeof import("./reasoning-timeouts");
		llm: typeof import("./llm");
	}> {
		vi.resetModules();
		// Both modules are re-evaluated in the same reset cycle, so
		// `llm.STREAM_IDLE_TIMEOUT_MS` is the instance the reloaded
		// `reasoning-timeouts` reads.
		const mod = await import("./reasoning-timeouts");
		const llm = await import("./llm");
		return { mod, llm };
	}

	it("honors an explicit env value as-is even below the floor (FR-003/US2.3)", async () => {
		process.env[envKey] = "90000";
		const { mod } = await reloadModule();
		// The explicit branch MUST precede the floor: a bare max() would raise
		// 90s to the 600s DeepSeek floor and violate FR-003.
		expect(mod.resolveStreamIdleTimeout("openai/deepseek-v4-flash")).toBe(
			90_000,
		);
	});

	it("applies max(default, floor) when the env is unset and the spec matches", async () => {
		delete process.env[envKey];
		const { mod } = await reloadModule();
		expect(mod.resolveStreamIdleTimeout("openai/deepseek-v4-flash")).toBe(
			Math.max(120_000, 600_000),
		);
		expect(mod.resolveStreamIdleTimeout("openai/deepseek-v4-flash")).toBe(
			600_000,
		);
	});

	it("returns STREAM_IDLE_TIMEOUT_MS when the spec is omitted (backward compatible)", async () => {
		delete process.env[envKey];
		const { mod, llm } = await reloadModule();
		expect(mod.resolveStreamIdleTimeout()).toBe(llm.STREAM_IDLE_TIMEOUT_MS);
		expect(mod.resolveStreamIdleTimeout(undefined)).toBe(120_000);
	});

	it("returns the default for a non-matching model", async () => {
		delete process.env[envKey];
		const { mod } = await reloadModule();
		expect(mod.resolveStreamIdleTimeout("openai/gpt-4")).toBe(120_000);
	});

	it("an explicit env value wins as-is for any spec, including a floor-matching one (planner side)", async () => {
		process.env[envKey] = "90000";
		const { mod } = await reloadModule();
		expect(mod.resolveStreamIdleTimeout("anthropic/claude-opus-4")).toBe(
			90_000,
		);
	});
});
