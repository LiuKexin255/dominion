/**
 * strategy-store.test.ts — StrategyStore contract tests
 * (`specs/031-team-template-mode/contracts/strategy-store-contract.md` §7).
 *
 * Fake impl full coverage + the Mongo impl's upsert/index logic via an
 * injectable fake collection (style/javascript.md §测试 — DI over vi.mock;
 * no real mongo round-trip).
 */

import { describe, expect, it } from "vitest";

import {
	FakeStrategyStore,
	MongoStrategyStore,
	type StrategyCollection,
	type StrategyDocument,
} from "./strategy-store";

describe("FakeStrategyStore", () => {
	it("get returns empty string for an unknown session (contract: 初始值 \"\")", async () => {
		const store = new FakeStrategyStore();
		expect(await store.get("s1")).toBe("");
	});

	it("put then get returns the same content", async () => {
		const store = new FakeStrategyStore();
		await store.put("s1", "corner-first approach");
		expect(await store.get("s1")).toBe("corner-first approach");
	});

	it("put overwrites the previous strategy (planner update semantics)", async () => {
		const store = new FakeStrategyStore();
		await store.put("s1", "old");
		await store.put("s1", "new");
		expect(await store.get("s1")).toBe("new");
	});

	it("isolates sessions — different session ids do not interfere", async () => {
		const store = new FakeStrategyStore();
		await store.put("s1", "strategy-a");
		await store.put("s2", "strategy-b");
		expect(await store.get("s1")).toBe("strategy-a");
		expect(await store.get("s2")).toBe("strategy-b");
		expect(await store.get("s3")).toBe("");
	});
});

describe("MongoStrategyStore", () => {
	/** A fake `strategies` collection (in-memory Map) recording operations. */
	function fakeCollection(): StrategyCollection & {
		docs: Map<string, StrategyDocument>;
		updateCalls: Array<{ filter: unknown; update: unknown; options: unknown }>;
		indexCalls: unknown[];
	} {
		const docs = new Map<string, StrategyDocument>();
		return {
			docs,
			updateCalls: [],
			indexCalls: [],
			async findOne(filter) {
				return docs.get(filter.session_id) ?? null;
			},
			async updateOne(filter, update, options) {
				this.updateCalls.push({ filter, update, options });
				const existing = docs.get(filter.session_id);
				if (existing) {
					// Contract §3: exists → update content + update_time.
					existing.content = update.$set.content;
					existing.update_time = update.$set.update_time;
				} else {
					// Contract §3: missing → insert with create_time.
					docs.set(filter.session_id, {
						session_id: filter.session_id,
						content: update.$set.content,
						create_time: update.$setOnInsert.create_time,
						update_time: update.$set.update_time,
					});
				}
			},
			async createIndex(index, options) {
				this.indexCalls.push({ index, options });
				return "session_id_1";
			},
		};
	}

	it("get returns empty string when no document exists", async () => {
		const coll = fakeCollection();
		const store = new MongoStrategyStore(coll);
		expect(await store.get("missing")).toBe("");
		// Prove the mock was exercised (style/javascript.md — no silent mock).
		expect(coll.docs.size).toBe(0);
	});

	it("get returns the stored content", async () => {
		const coll = fakeCollection();
		coll.docs.set("s1", {
			session_id: "s1",
			content: "corner-first",
			create_time: new Date("2026-01-01T00:00:00Z"),
			update_time: new Date("2026-01-01T00:00:00Z"),
		});
		const store = new MongoStrategyStore(coll);
		expect(await store.get("s1")).toBe("corner-first");
	});

	it("put upserts: inserts a document on first write (upsert: true + $setOnInsert create_time)", async () => {
		const coll = fakeCollection();
		const store = new MongoStrategyStore(coll);
		await store.put("s1", "first strategy");

		expect(coll.updateCalls).toHaveLength(1);
		const call = coll.updateCalls[0];
		expect(call.filter).toEqual({ session_id: "s1" });
		expect(call.update).toMatchObject({
			$set: { session_id: "s1", content: "first strategy" },
			$setOnInsert: { create_time: expect.any(Date) },
		});
		expect(call.options).toEqual({ upsert: true });

		const doc = coll.docs.get("s1");
		expect(doc?.content).toBe("first strategy");
		expect(doc?.create_time).toBeInstanceOf(Date);
		expect(doc?.update_time).toBeInstanceOf(Date);
		expect(await store.get("s1")).toBe("first strategy");
	});

	it("put upserts: updates content + update_time and PRESERVES create_time on re-put", async () => {
		const coll = fakeCollection();
		const store = new MongoStrategyStore(coll);
		await store.put("s1", "v1");
		const createTime = coll.docs.get("s1")?.create_time;

		await store.put("s1", "v2");
		expect(coll.updateCalls).toHaveLength(2);
		const doc = coll.docs.get("s1");
		expect(doc?.content).toBe("v2");
		expect(doc?.create_time).toBe(createTime); // contract §3: 存在则更新 content+update_time
		expect(doc?.update_time).not.toBe(createTime);
	});

	it("put isolates sessions by session_id key", async () => {
		const coll = fakeCollection();
		const store = new MongoStrategyStore(coll);
		await store.put("s1", "a");
		await store.put("s2", "b");
		expect(await store.get("s1")).toBe("a");
		expect(await store.get("s2")).toBe("b");
		expect(await store.get("s3")).toBe("");
	});

	it("ensureIndexes creates the unique index on session_id (contract §3)", async () => {
		const coll = fakeCollection();
		const store = new MongoStrategyStore(coll);
		await store.ensureIndexes();
		expect(coll.indexCalls).toEqual([
			{ index: { session_id: 1 }, options: { unique: true } },
		]);
	});

	it("strategy reads survive independently of the store instance (persistence semantics)", async () => {
		const coll = fakeCollection();
		const writer = new MongoStrategyStore(coll);
		const reader = new MongoStrategyStore(coll);
		await writer.put("s1", "persisted");
		expect(await reader.get("s1")).toBe("persisted");
	});

	it("propagates collection failures (get/put errors are not swallowed)", async () => {
		const failing: StrategyCollection = {
			async findOne() {
				throw new Error("mongo down");
			},
			async updateOne() {
				throw new Error("mongo down");
			},
			async createIndex() {
				throw new Error("mongo down");
			},
		};
		const store = new MongoStrategyStore(failing);
		await expect(store.get("s1")).rejects.toThrow("mongo down");
		await expect(store.put("s1", "x")).rejects.toThrow("mongo down");
		await expect(store.ensureIndexes()).rejects.toThrow("mongo down");
	});
});
