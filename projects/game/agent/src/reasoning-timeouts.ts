/**
 * reasoning-timeouts.ts — Per-reasoning-model idle-timeout floor for the team
 * graph's player/planner nodes (specs/044-llm-stall-recovery-fix/spec.md
 * FR-002/FR-003).
 *
 * Reasoning models (e.g. `deepseek-v4-flash`) consume `reasoning_content` for
 * an extended period before emitting the first `content` token — Hermes
 * measured ~65s for this exact model+gateway
 * (https://github.com/NousResearch/hermes-agent/issues/61461) — so a
 * deep-thinking phase that emits no chunks would false-stall under the plain
 * default. The floor raises the effective idle timeout for known reasoning
 * models via `max(default, floor)`, mirroring Hermes's
 * `_REASONING_STALE_TIMEOUT_FLOORS`
 * (https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa);
 * an explicit operator env configuration always wins as-is, even below a
 * floor (specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md
 * §1).
 */

import {
	STREAM_IDLE_TIMEOUT_MS,
	STREAM_IDLE_TIMEOUT_EXPLICIT,
} from "./llm";
import { parseModelSpec } from "./model-provider";

/**
 * The reasoning-model floor allowlist: `[substring, floorMs]` pairs matched
 * against the bare (provider-stripped, lowercased) model name. Values are
 * FROZEN here and MUST stay consistent with the authority table in
 * specs/044-llm-stall-recovery-fix/data-model.md §2 — adding a reasoning
 * model = append a row in BOTH places + a unit test.
 */
export const REASONING_IDLE_TIMEOUT_FLOOR: ReadonlyArray<
	readonly [substring: string, floorMs: number]
> = [
	["deepseek-r1", 600_000],
	["deepseek-reasoner", 600_000],
	["deepseek-v4-", 600_000],
	["o1-", 600_000],
	["o3-", 600_000],
	["o3-mini-", 300_000],
	["o4-mini-", 300_000],
	["claude-opus-", 240_000],
];

/**
 * Entries sorted by substring length DESCENDING for longest-substring-first
 * matching: `o3-mini-` must be tested before `o3-`, and `o1-` must never
 * match `olmo-1` (a Hermes-documented pitfall,
 * https://github.com/NousResearch/hermes-agent/commit/27c486e3b1b6da00d5f5dbeabffe03b7ba3bbcfa).
 */
const FLOOR_ENTRIES_BY_LONGEST_FIRST = [...REASONING_IDLE_TIMEOUT_FLOOR].sort(
	(a, b) => b[0].length - a[0].length,
);

/**
 * Match a model spec against the floor allowlist
 * (specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md §2):
 * strip the `{provider}/` prefix, lowercase, then longest-substring-first
 * match. Returns the first matching entry's `floorMs`, or `null` when no
 * entry matches.
 */
export function getReasoningIdleTimeoutFloor(
	modelSpec: string,
): number | null {
	const bare = parseModelSpec(modelSpec).toLowerCase();
	for (const [substring, floorMs] of FLOOR_ENTRIES_BY_LONGEST_FIRST) {
		if (bare.includes(substring)) {
			return floorMs;
		}
	}
	return null;
}

/**
 * Resolve the effective chunk-idle timeout for a model-holding node
 * (specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md §1):
 *
 * 1. env `GAME_STREAM_IDLE_TIMEOUT_MS` explicitly set
 *    (`STREAM_IDLE_TIMEOUT_EXPLICIT`) → `STREAM_IDLE_TIMEOUT_MS` as-is —
 *    an explicit operator config always wins, even below a floor (spec
 *    FR-003/US2.3). The explicit branch MUST come first: a bare
 *    `max(STREAM_IDLE_TIMEOUT_MS, floor)` would raise an explicit low value
 *    (e.g. env=90s + DeepSeek 600s floor) to 600s, violating FR-003.
 * 2. Otherwise, when `modelSpec` matches a floor F → `max(STREAM_IDLE_TIMEOUT_MS, F)`.
 * 3. Otherwise (or when `modelSpec` is omitted — backward compatible with
 *    call sites that pass no spec) → `STREAM_IDLE_TIMEOUT_MS`.
 */
export function resolveStreamIdleTimeout(modelSpec?: string): number {
	if (STREAM_IDLE_TIMEOUT_EXPLICIT) {
		return STREAM_IDLE_TIMEOUT_MS;
	}
	if (modelSpec === undefined) {
		return STREAM_IDLE_TIMEOUT_MS;
	}
	const floor = getReasoningIdleTimeoutFloor(modelSpec);
	if (floor !== null) {
		return Math.max(STREAM_IDLE_TIMEOUT_MS, floor);
	}
	return STREAM_IDLE_TIMEOUT_MS;
}
