/**
 * memory-snapshot.test.ts — Tests for the planner's frozen memory snapshot
 * (spec 039 T017, `specs/039-planner-memory-calibration/contracts/
 * team-graph-contract.md` §3 — D5 plan b; survey D2/D4).
 *
 * Coverage:
 *   - refresh re-bakes the entries from `listMemories` (the pagination walk
 *     to an empty `next_page_token` lives in the MemoryClient —
 *     memory-client.test.ts; the snapshot consumes its aggregated result).
 *   - refresh failure keeps the PREVIOUS snapshot and does not throw
 *     (contract §5 — memory unavailability must not block the team run).
 *   - toSystemMessage renders PURE content (no `memory_id`, FR-011) with
 *     the `planner-memory-snapshot` id; empty snapshot renders the header
 *     only.
 *
 * DI pattern (style/javascript.md §测试): the MemoryClient is injected per
 * refresh call — a `vi.fn()`-backed fake, no module-level `vi.mock`.
 */

import { describe, it, expect, vi } from "vitest";

import type { MemoryClient } from "../memory-client.js";
import {
	FrozenMemorySnapshot,
	PLANNER_MEMORY_SNAPSHOT_ID,
} from "./memory-snapshot.js";

function makeFakeClient(entries: Array<{ memory_id: string; content: string }>): {
	fake: MemoryClient;
	listMemories: ReturnType<typeof vi.fn>;
} {
	const listMemories = vi.fn(async () => entries.map((e) => ({ ...e })));
	return {
		fake: { listMemories } as unknown as MemoryClient,
		listMemories,
	};
}

describe("FrozenMemorySnapshot (contract §3)", () => {
	describe("refresh", () => {
		it("re-bakes the entries via listMemories and stamps bakedAt", async () => {
			const { fake, listMemories } = makeFakeClient([
				{ memory_id: "m1", content: "player 常误标边角" },
				{ memory_id: "m2", content: "开局先点中心更高效" },
			]);
			const snapshot = new FrozenMemorySnapshot();
			expect(snapshot.bakedAt).toBeNull();

			await snapshot.refresh(fake, "saolei", "sess-1");

			expect(listMemories).toHaveBeenCalledTimes(1);
			expect(listMemories).toHaveBeenCalledWith("saolei", "sess-1");
			expect(snapshot.bakedAt).toBeTypeOf("number");
			// The rendered message reflects the re-baked entries.
			const msg = snapshot.toSystemMessage();
			expect(String(msg.content)).toBe(
				"长期记忆：\nplayer 常误标边角\n开局先点中心更高效",
			);
		});

		it("keeps the PREVIOUS snapshot (and bakedAt) when the re-read fails (contract §5)", async () => {
			const { fake, listMemories } = makeFakeClient([{ memory_id: "m1", content: "旧内容" }]);
			const snapshot = new FrozenMemorySnapshot();
			await snapshot.refresh(fake, "saolei", "sess-1");
			const bakedAtBefore = snapshot.bakedAt;
			expect(bakedAtBefore).not.toBeNull();

			// Next refresh fails (memory service unavailable) — the snapshot
			// must stay frozen at the previous bake, not throw.
			listMemories.mockRejectedValueOnce(
				Object.assign(new Error("memory service unavailable"), { code: 14 }),
			);
			await expect(snapshot.refresh(fake, "saolei", "sess-1")).resolves.toBeUndefined();

			expect(snapshot.bakedAt).toBe(bakedAtBefore);
			expect(String(snapshot.toSystemMessage().content)).toBe(
				"长期记忆：\n旧内容",
			);
		});
	});

	describe("toSystemMessage (FR-011 — pure content, no memory_id)", () => {
		it("renders each entry as its content only — memory_id never enters LLM text", async () => {
			const { fake } = makeFakeClient([
				{ memory_id: "mem-hidden-1", content: "条目甲" },
				{ memory_id: "mem-hidden-2", content: "条目乙" },
			]);
			const snapshot = new FrozenMemorySnapshot();
			await snapshot.refresh(fake, "saolei", "sess-1");

			const msg = snapshot.toSystemMessage();

			expect(msg.getType()).toBe("system");
			expect(msg.id).toBe(PLANNER_MEMORY_SNAPSHOT_ID);
			const text = String(msg.content);
			expect(text).toBe("长期记忆：\n条目甲\n条目乙");
			// The internal ids are NOT rendered (FR-011 — the LLM locates
			// entries by content via old_text substrings instead).
			expect(text).not.toContain("mem-hidden");
			expect(text).not.toContain("memory_id");
		});

		it("renders the header only for an empty (never-refreshed) snapshot", () => {
			const snapshot = new FrozenMemorySnapshot();
			const msg = snapshot.toSystemMessage();
			expect(msg.id).toBe(PLANNER_MEMORY_SNAPSHOT_ID);
			expect(String(msg.content)).toBe("长期记忆：\n");
		});
	});
});
