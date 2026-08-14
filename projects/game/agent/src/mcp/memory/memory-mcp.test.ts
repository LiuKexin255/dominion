/**
 * memory-mcp.test.ts — Tests for the planner memory MCP server (spec 039
 * T015, `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
 * §1-2).
 *
 * Coverage:
 *   - old_text substring matching semantics (hermes
 *     `tools/memory_tool.py`): substring containment + case sensitivity,
 *     0-hit error text with the full current entries, multiple-distinct-hit
 *     error text with previews, all-identical → first, empty old_text.
 *   - generateMemoryId: deterministic, memory-service charset `[a-z0-9_-]+`.
 *   - Agent conversion: add → generated id + createMemory; replace/remove →
 *     listMemories + old_text location + updateMemory/deleteMemory;
 *     0/multiple hits → error text (no write); add dedupe → success no-write;
 *     operations batch → ordered atomic application with all-or-nothing
 *     failure; ambiguous/missing argument forms rejected.
 *   - createMemoryMcpServer registers EXACTLY ONE `memory` tool whose
 *     arguments carry neither template nor session (path-closure, FR-012);
 *     infrastructure errors surface as `memory failed: ...` TEXT (never a
 *     thrown tool exception — 031 C15 neutral status).
 *
 * DI pattern (`style/javascript.md` §测试): `memoryClient` is injected; a
 * `vi.fn()`-backed fake — no module-level `vi.mock` (vitest Mocking
 * Modules — Pitfalls: https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls).
 */

import { describe, it, expect, vi, beforeEach } from "vitest";

import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

import type { MemoryClient, MemoryEntry } from "../../memory-client";
import {
	MEMORY_ACTIONS,
	applyMemoryCall,
	createMemoryMcpServer,
	generateMemoryId,
	matchBySubstring,
	type MemoryToolArgs,
} from "./memory-mcp";

/** Fake MemoryClient surface (DI seam — matches the real class shape). */
function makeFakeClient(entries: MemoryEntry[] = []) {
	const state: MemoryEntry[] = entries.map((e) => ({ ...e }));
	return {
		state,
		createMemory: vi.fn(async (t: string, s: string, id: string, content: string) => {
			state.push({ memory_id: id, content });
		}),
		updateMemory: vi.fn(async (t: string, s: string, id: string, content: string) => {
			const e = state.find((x) => x.memory_id === id);
			if (e) e.content = content;
		}),
		deleteMemory: vi.fn(async (t: string, s: string, id: string) => {
			const i = state.findIndex((x) => x.memory_id === id);
			if (i >= 0) state.splice(i, 1);
		}),
		listMemories: vi.fn(async () => state.map((e) => ({ ...e }))),
	};
}

const TEMPLATE = "saolei";
const SESSION = "sess-1";

/** Invoke the registered `memory` tool's handler directly (SDK internals). */
function callTool(
	server: McpServer,
	args: MemoryToolArgs,
): Promise<{ content: { type: string; text: string }[] }> {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const tool = (server as any)._registeredTools.memory;
	return tool.handler(args);
}

describe("matchBySubstring (hermes old_text semantics, contract §1.1)", () => {
	const entries: MemoryEntry[] = [
		{ memory_id: "m1", content: "player 常误标边角" },
		{ memory_id: "m2", content: "开局先点中心更高效" },
		{ memory_id: "m3", content: "player 常误标边角" }, // identical to m1
	];

	it("matches by case-sensitive substring containment", () => {
		const hit = matchBySubstring(entries, "误标边角");
		expect("entry" in hit && hit.entry.memory_id).toBe("m1");
	});

	it("is case-sensitive (no match for a different case)", () => {
		const hit = matchBySubstring(
			[{ memory_id: "m1", content: "CaseSensitive" }],
			"casesensitive",
		);
		expect("error" in hit).toBe(true);
	});

	it("0 hits → error text containing the FULL current entry list", () => {
		const hit = matchBySubstring(entries, "不存在的子串");
		expect("error" in hit).toBe(true);
		if ("error" in hit) {
			expect(hit.error).toContain("no entry matched '不存在的子串'");
			expect(hit.error).toContain("player 常误标边角");
			expect(hit.error).toContain("开局先点中心更高效");
			expect(hit.error).not.toContain("m1"); // memory_id never rendered
		}
	});

	it("multiple DISTINCT hits → error text with hit previews (be more specific)", () => {
		const multi = matchBySubstring(
			[
				{ memory_id: "m1", content: "player 过度标记" },
				{ memory_id: "m2", content: "player 过度谨慎" },
			],
			"过度",
		);
		expect("error" in multi).toBe(true);
		if ("error" in multi) {
			expect(multi.error).toContain("multiple entries matched '过度'");
			expect(multi.error).toContain("player 过度标记");
			expect(multi.error).toContain("player 过度谨慎");
		}
	});

	it("all-identical hits → act on the FIRST entry (dedupe)", () => {
		const hit = matchBySubstring(entries, "误标边角");
		expect("entry" in hit && hit.entry).toEqual({ memory_id: "m1", content: "player 常误标边角" });
	});

	it("empty/whitespace old_text → error text + current entries", () => {
		for (const empty of ["", "   "]) {
			const hit = matchBySubstring(entries, empty);
			expect("error" in hit).toBe(true);
			if ("error" in hit) {
				expect(hit.error).toContain("old_text cannot be empty");
				expect(hit.error).toContain("player 常误标边角");
			}
		}
	});

	it("0 hits on an empty store → 'no entries' placeholder", () => {
		const hit = matchBySubstring([], "x");
		expect("error" in hit && hit.error).toContain("(no entries)");
	});
});

describe("generateMemoryId (FR-008 — agent-side id, invisible to the LLM)", () => {
	it("is deterministic for the same content", () => {
		expect(generateMemoryId("内容")).toBe(generateMemoryId("内容"));
	});

	it("differs for different content", () => {
		expect(generateMemoryId("内容A")).not.toBe(generateMemoryId("内容B"));
	});

	it("matches the memory service memory_id charset [a-z0-9_-]+ (Phase 3 T011)", () => {
		for (const content of ["player 过度标记", "win-rate", "1+1=2"]) {
			expect(generateMemoryId(content)).toMatch(/^[a-z0-9_-]+$/);
		}
	});
});

describe("applyMemoryCall — agent conversion (contract §1)", () => {
	let fake: ReturnType<typeof makeFakeClient>;

	beforeEach(() => {
		fake = makeFakeClient();
	});

	describe("add", () => {
		it("generates the internal id and calls createMemory (add → CreateMemory)", async () => {
			const text = "player 重复误标地雷";
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "add",
				content: text,
			});

			expect(result).toBe("memory added");
			expect(fake.createMemory).toHaveBeenCalledTimes(1);
			expect(fake.createMemory).toHaveBeenCalledWith(
				TEMPLATE,
				SESSION,
				generateMemoryId(text),
				text,
			);
			// The generated id is the service-internal key; no id from the LLM.
			expect(fake.listMemories).toHaveBeenCalledTimes(1); // dedupe check
		});

		it("dedupes an equivalent existing content → success, NO createMemory", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "已存在的洞察" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "add",
				content: "已存在的洞察",
			});

			expect(result).toBe("memory already exists (no duplicate added)");
			expect(fake.createMemory).not.toHaveBeenCalled();
		});

		it("empty content → error text", async () => {
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "add",
				content: "  ",
			});
			expect(result).toBe("memory: content cannot be empty");
			expect(fake.createMemory).not.toHaveBeenCalled();
		});

		it("a CreateMemory ALREADY_EXISTS race → dedupe SUCCESS text (m1: catch code 6)", async () => {
			// A concurrent writer created the same content between the
			// dedupe check and the RPC — since the id is a content digest
			// the conflict can only be for the SAME content, so it is the
			// dedupe success (hermes "no duplicate added"), NOT an error.
			fake.createMemory.mockRejectedValueOnce(
				Object.assign(new Error("memory already exists"), { code: 6 }),
			);

			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "add",
				content: "竞态内容",
			});

			expect(result).toBe("memory already exists (no duplicate added)");
			expect(fake.createMemory).toHaveBeenCalledTimes(1);
			expect(fake.createMemory).toHaveBeenCalledWith(
				TEMPLATE,
				SESSION,
				generateMemoryId("竞态内容"),
				"竞态内容",
			);
		});

		it("a NON-ALREADY_EXISTS createMemory failure still propagates as an infra error", async () => {
			fake.createMemory.mockRejectedValueOnce(
				Object.assign(new Error("memory service unavailable"), { code: 14 }),
			);

			await expect(
				applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
					action: "add",
					content: "内容",
				}),
			).rejects.toThrow("memory service unavailable");
		});
	});

	describe("replace", () => {
		it("locates by old_text substring and calls updateMemory (replace → UpdateMemory)", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "player 常误标边角" },
				{ memory_id: "m2", content: "开局先点中心更高效" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "replace",
				old_text: "误标边角",
				content: "player 已改掉误标习惯",
			});

			expect(result).toBe("memory replaced");
			expect(fake.updateMemory).toHaveBeenCalledTimes(1);
			expect(fake.updateMemory).toHaveBeenCalledWith(
				TEMPLATE,
				SESSION,
				"m1",
				"player 已改掉误标习惯",
			);
			expect(fake.deleteMemory).not.toHaveBeenCalled();
		});

		it("0 hits → error text with the current entries, NO write", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "唯一条目" }]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "replace",
				old_text: "无此内容",
				content: "新内容",
			});

			expect(result).toContain("no entry matched '无此内容'");
			expect(result).toContain("唯一条目");
			expect(fake.updateMemory).not.toHaveBeenCalled();
		});

		it("multiple distinct hits → error text (be more specific), NO write", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "player 过度标记" },
				{ memory_id: "m2", content: "player 过度谨慎" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "replace",
				old_text: "过度",
				content: "新内容",
			});

			expect(result).toContain("multiple entries matched '过度'");
			expect(result).toContain("player 过度标记");
			expect(result).toContain("player 过度谨慎");
			expect(fake.updateMemory).not.toHaveBeenCalled();
		});

		it("all-identical hits → updates the FIRST entry", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "重复条目" },
				{ memory_id: "m2", content: "重复条目" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "replace",
				old_text: "重复",
				content: "合并后的条目",
			});

			expect(result).toBe("memory replaced");
			expect(fake.updateMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m1", "合并后的条目");
		});

		it("missing old_text → error text with current entries", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "条目" }]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "replace",
				content: "新内容",
			});
			expect(result).toContain("old_text cannot be empty");
			expect(result).toContain("条目");
			expect(fake.updateMemory).not.toHaveBeenCalled();
		});
	});

	describe("remove", () => {
		it("locates by old_text substring and calls deleteMemory (remove → DeleteMemory)", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "过时的条目" },
				{ memory_id: "m2", content: "保留的条目" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "remove",
				old_text: "过时的",
			});

			expect(result).toBe("memory removed");
			expect(fake.deleteMemory).toHaveBeenCalledTimes(1);
			expect(fake.deleteMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m1");
			expect(fake.updateMemory).not.toHaveBeenCalled();
		});

		it("0 hits → error text, NO delete", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "条目" }]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "remove",
				old_text: "无",
			});
			expect(result).toContain("no entry matched '无'");
			expect(fake.deleteMemory).not.toHaveBeenCalled();
		});
	});

	describe("argument forms (contract §1 — single vs batch, mutually exclusive)", () => {
		it("rejects providing BOTH action and operations", async () => {
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				action: "add",
				content: "x",
				operations: [{ action: "add", content: "y" }],
			});
			expect(result).toContain("not both");
			expect(fake.createMemory).not.toHaveBeenCalled();
		});

		it("rejects providing NEITHER", async () => {
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {});
			expect(result).toContain("provide EITHER");
			expect(fake.createMemory).not.toHaveBeenCalled();
		});
	});

	describe("operations batch (contract §1 — atomic all-or-nothing)", () => {
		it("applies a mixed batch in order (add + replace + remove)", async () => {
			fake = makeFakeClient([
				{ memory_id: "m1", content: "旧条目A" },
				{ memory_id: "m2", content: "待删条目" },
			]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				operations: [
					{ action: "add", content: "新条目" },
					{ action: "replace", old_text: "旧条目A", content: "更新后的A" },
					{ action: "remove", old_text: "待删" },
				],
			});

			expect(result).toBe("memory: applied 3 operation(s)");
			expect(fake.createMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, generateMemoryId("新条目"), "新条目");
			expect(fake.updateMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m1", "更新后的A");
			expect(fake.deleteMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, "m2");
		});

		it("skips duplicate adds inside the batch (idempotent, hermes)", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "已存在" }]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				operations: [
					{ action: "add", content: "已存在" },
					{ action: "add", content: "全新" },
				],
			});

			expect(result).toBe("memory: applied 1 operation(s)");
			expect(fake.createMemory).toHaveBeenCalledTimes(1);
			expect(fake.createMemory).toHaveBeenCalledWith(TEMPLATE, SESSION, generateMemoryId("全新"), "全新");
		});

		it("a failing op aborts the WHOLE batch — nothing is written (all-or-nothing)", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "唯一条目" }]);
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				operations: [
					{ action: "add", content: "新条目" },
					{ action: "remove", old_text: "无此内容" },
				],
			});

			expect(result).toContain("operation 2 failed");
			expect(result).toContain("No operations were applied");
			expect(result).toContain("唯一条目");
			expect(fake.createMemory).not.toHaveBeenCalled();
			expect(fake.deleteMemory).not.toHaveBeenCalled();
		});

		it("empty operations list → error text", async () => {
			const result = await applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
				operations: [],
			});
			expect(result).toBe("memory: operations list is empty");
			expect(fake.createMemory).not.toHaveBeenCalled();
		});

		it("commit-phase infrastructure failure: earlier writes stay applied; error surfaces as text (M1 — v1 no rollback)", async () => {
			fake = makeFakeClient([{ memory_id: "m1", content: "旧条目A" }]);
			// Preflight passes for BOTH ops; the SECOND commit RPC
			// (updateMemory) fails with an infrastructure error — the FIRST
			// (add) has already been committed by then.
			fake.updateMemory.mockRejectedValueOnce(
				Object.assign(new Error("memory service unavailable"), { code: 14 }),
			);
			const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);

			// The infra failure propagates out of applyBatch (applyMemoryCall
			// does not swallow infrastructure errors)…
			await expect(
				applyMemoryCall(fake as unknown as MemoryClient, TEMPLATE, SESSION, {
					operations: [
						{ action: "add", content: "新条目" },
						{ action: "replace", old_text: "旧条目A", content: "更新后的A" },
					],
				}),
			).rejects.toThrow("memory service unavailable");

			// …and the FIRST op was already applied before the failure — v1
			// documents partial application (applyBatch doc comment: true
			// all-or-nothing needs a service-side transaction API; the
			// contract's batch is v1-optional).
			expect(fake.createMemory).toHaveBeenCalledTimes(1);
			expect(fake.state.some((e) => e.content === "新条目")).toBe(true);
			// The replace never landed (its RPC is the one that failed).
			expect(fake.state.some((e) => e.content === "更新后的A")).toBe(false);

			// Tool surface: the same failure is a TEXT result (`memory
			// failed: ...`), never a thrown tool error (031 C15 neutral).
			fake.updateMemory.mockRejectedValueOnce(
				Object.assign(new Error("memory service unavailable"), { code: 14 }),
			);
			const result = await callTool(server, {
				operations: [
					{ action: "add", content: "新条目2" },
					{ action: "replace", old_text: "旧条目A", content: "更新后的A" },
				],
			});
			expect(result.content[0].type).toBe("text");
			expect(result.content[0].text).toBe("memory failed: memory service unavailable");
		});
	});
});

describe("createMemoryMcpServer (contract §2)", () => {
	it("registers EXACTLY ONE `memory` tool (FR-007/FR-008 — no other tools)", () => {
		const fake = makeFakeClient();
		const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const tools = (server as any)._registeredTools as Record<string, unknown>;
		expect(Object.keys(tools)).toEqual(["memory"]);
	});

	it("tool arguments carry neither template nor session (path closure, FR-012)", () => {
		const fake = makeFakeClient();
		const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const registered = (server as any)._registeredTools.memory;
		// The SDK wraps the raw shape into a zod object — `.shape` holds the
		// field-name map.
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const fields = Object.keys((registered.inputSchema as any).shape ?? {});
		expect(fields.sort()).toEqual(["action", "content", "old_text", "operations"]);
		expect(fields).not.toContain("memory_id");
		expect(fields).not.toContain("target");
		expect(fields).not.toContain("template");
		expect(fields).not.toContain("session");
	});

	it("the registered tool converts a call through the injected memoryClient", async () => {
		const fake = makeFakeClient();
		const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);

		const result = await callTool(server, { action: "add", content: "一条记忆" });

		expect(result.content).toHaveLength(1);
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toBe("memory added");
		expect(fake.createMemory).toHaveBeenCalledWith(
			TEMPLATE,
			SESSION,
			generateMemoryId("一条记忆"),
			"一条记忆",
		);
	});

	it("infrastructure failure → `memory failed: ...` TEXT result, never a thrown tool error", async () => {
		const fake = makeFakeClient();
		fake.listMemories.mockRejectedValueOnce(
			Object.assign(new Error("memory service unavailable"), { code: 14 }),
		);
		const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);

		const result = await callTool(server, { action: "add", content: "x" });

		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toBe("memory failed: memory service unavailable");
	});

	it("tool description exists (rich guidance lives in the memory skill, FR-020)", () => {
		const fake = makeFakeClient();
		const server = createMemoryMcpServer(fake as unknown as MemoryClient, TEMPLATE, SESSION);
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const tool = (server as any)._registeredTools.memory as { description?: string };
		expect(tool.description?.length).toBeGreaterThan(0);
		expect(tool.description).toContain("memory");
	});

	it("action enum is restricted to add/replace/remove", () => {
		expect(MEMORY_ACTIONS).toEqual(["add", "replace", "remove"]);
	});
});
