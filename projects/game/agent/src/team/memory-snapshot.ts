/**
 * memory-snapshot.ts — Planner's frozen long-term-memory snapshot (spec 039
 * T017; `specs/039-planner-memory-calibration/contracts/team-graph-contract.md`
 * §3 — frozen snapshot, survey D5 plan b).
 *
 * The planner's long-term memory is presented as a FROZEN snapshot: baked
 * once (team init) and re-baked only at the compression boundary (every 5
 * games, contract §2.4). Mid-session `memory`-tool writes persist to the
 * memory service but do NOT refresh this snapshot (FR-010 — freezing keeps
 * the planner's system-prompt-adjacent input stable across reviews).
 *
 * The snapshot renders as a PURE-CONTENT SystemMessage (hermes style — no
 * `memory_id` prefixes, FR-011/Session 2026-08-08): the LLM locates entries
 * by content via the `memory` tool's `old_text` substring matching, so
 * `memory_id` stays internal to `entries` (used for re-location after a
 * refresh) and never enters LLM-visible text.
 *
 * Refresh degradation (contract §5 / survey D4): a failed re-read keeps the
 * PREVIOUS snapshot (or empty) and does NOT break the team run — memory
 * unavailability must not block gameplay.
 *
 * DI seam (`style/javascript.md` §测试): `memoryClient` is injected per
 * refresh call; tests pass a fake with no `vi.mock`.
 */

import { SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { warn } from "@dominion/common-js-logs";

import type { MemoryClient, MemoryEntry } from "../memory-client.js";

/** The SystemMessage id used by the frozen snapshot (filtered on write-back). */
export const PLANNER_MEMORY_SNAPSHOT_ID = "planner-memory-snapshot";

/**
 * Frozen planner long-term-memory snapshot (contract §3).
 *
 * `entries` holds `{memory_id, content}` — the memory_id ONLY for internal
 * re-location (e.g. the memory tool's replace/remove re-reads the service
 * itself); `toSystemMessage()` renders pure content.
 */
export class FrozenMemorySnapshot {
	/** The baked entries (frozen until the next refresh). */
	private entries: MemoryEntry[] = [];

	/**
	 * Epoch millis of the last successful bake; `null` before the first
	 * `refresh`.
	 */
	bakedAt: number | null = null;

	/**
	 * Re-bake the snapshot: re-read ALL entries from the memory service
	 * (listMemories walks every page until `next_page_token` is empty).
	 *
	 * On failure (memory service unavailable) the PREVIOUS snapshot is kept
	 * and the team continues (contract §5 — memory must not block gameplay).
	 *
	 * @param memoryClient The MemoryService gRPC client (DI seam).
	 * @param template The template path segment (e.g. `"saolei"`).
	 * @param session The session id path segment.
	 */
	async refresh(
		memoryClient: MemoryClient,
		template: string,
		session: string,
	): Promise<void> {
		try {
			this.entries = await memoryClient.listMemories(template, session);
			this.bakedAt = Date.now();
		} catch (err) {
			// contract §5 / survey D4: keep the last frozen snapshot (or the
			// empty initial one); log and continue — do not break the run.
			const msg = err instanceof Error ? err.message : String(err);
			warn("memory snapshot refresh failed; keeping previous snapshot", {
				template,
				session,
				error: msg,
			});
		}
	}

	/**
	 * Render the snapshot as a pure-content SystemMessage (FR-011): each
	 * entry appears as its content only — NO `memory_id` prefixes — so the
	 * LLM can locate entries by content with `old_text` substrings.
	 */
	toSystemMessage(): BaseMessage {
		const text = this.entries.map((e) => e.content).join("\n");
		return new SystemMessage({
			id: PLANNER_MEMORY_SNAPSHOT_ID,
			content: `长期记忆：\n${text}`,
		});
	}
}
