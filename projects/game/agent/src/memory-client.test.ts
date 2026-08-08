/**
 * memory-client.test.ts — Tests for MemoryClient (spec 039 T014).
 *
 * Reliable pattern (style/javascript.md §测试 — DI seam): the MemoryClient
 * ctor accepts an injected gRPC client, so every RPC test passes a
 * `vi.fn()`-backed fake and needs NO module-level `vi.mock`
 * ([vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)).
 * The channel-option tests reuse prompt-client's exported
 * `buildChannelOptionsForTest()` seam — the shared keepalive/round_robin
 * constants are imported from prompt-client (single source of truth,
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` §3).
 */

import { describe, it, expect, vi, beforeEach } from "vitest";

import {
	MemoryClient,
	memoryName,
	MEMORY_SERVICE_TARGET,
} from "./memory-client";
import { buildChannelOptionsForTest } from "./prompt-client";

/**
 * The real gRPC stub contract (memory-client.ts `call`) is
 * `rpc(request, metadata, options, callback)`. Fakes must match that
 * 4-arg shape so the 4th positional arg is the callback.
 */
interface FakeRpc {
	createMemory: ReturnType<typeof vi.fn>;
	updateMemory: ReturnType<typeof vi.fn>;
	deleteMemory: ReturnType<typeof vi.fn>;
	listMemories: ReturnType<typeof vi.fn>;
	close: ReturnType<typeof vi.fn>;
}

function makeFakeClient(): FakeRpc {
	return {
		createMemory: vi.fn(),
		updateMemory: vi.fn(),
		deleteMemory: vi.fn(),
		listMemories: vi.fn(),
		close: vi.fn(),
	};
}

/** Resolve a fake RPC successfully (or reject with `err` when given). */
function respond(
	fn: ReturnType<typeof vi.fn>,
	response: unknown,
	err?: Error,
): void {
	fn.mockImplementation(
		(
			_req: unknown,
			_metadata: unknown,
			_options: { deadline: Date },
			cb: (e: Error | null, r: unknown) => void,
		) => {
			cb(err ?? null, response);
		},
	);
}

describe("MemoryClient", () => {
	let fake: FakeRpc;

	beforeEach(() => {
		fake = makeFakeClient();
	});

	describe("memoryName (resource name construction, contract §3)", () => {
		it("builds templates/{template}/sessions/{session}/memories/{memoryId}", () => {
			expect(memoryName("saolei", "sess-1", "mem-abc")).toBe(
				"templates/saolei/sessions/sess-1/memories/mem-abc",
			);
		});
	});

	describe("createMemory", () => {
		it("calls CreateMemory with the embedded Memory body (AIP-133 request shape)", async () => {
			respond(fake.createMemory, {});
			const client = new MemoryClient(fake as never);

			await client.createMemory("saolei", "sess-1", "mem-abc", "内容一");

			expect(fake.createMemory).toHaveBeenCalledTimes(1);
			// The resource is EMBEDDED: parent + memory_id + Memory{name, content}
			// (Phase 1/3 request-shape correction — content is NOT top-level).
			expect(fake.createMemory).toHaveBeenCalledWith(
				{
					parent: "templates/saolei/sessions/sess-1",
					memoryId: "mem-abc",
					memory: {
						name: "templates/saolei/sessions/sess-1/memories/mem-abc",
						content: "内容一",
					},
				},
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
		});

		it("rejects with the gRPC error (e.g. ALREADY_EXISTS)", async () => {
			respond(
				fake.createMemory,
				null,
				Object.assign(new Error("memory already exists"), { code: 6 }),
			);
			const client = new MemoryClient(fake as never);

			await expect(
				client.createMemory("saolei", "sess-1", "mem-abc", "内容"),
			).rejects.toThrow("memory already exists");
		});
	});

	describe("updateMemory", () => {
		it("calls UpdateMemory with Memory{name, content} + FieldMask content path", async () => {
			respond(fake.updateMemory, {});
			const client = new MemoryClient(fake as never);

			await client.updateMemory("saolei", "sess-1", "mem-abc", "新内容");

			expect(fake.updateMemory).toHaveBeenCalledTimes(1);
			// AIP-134: body = the resource + update_mask limited to "content"
			// (the only mutable Memory field — Phase 1/3 correction).
			expect(fake.updateMemory).toHaveBeenCalledWith(
				{
					memory: {
						name: "templates/saolei/sessions/sess-1/memories/mem-abc",
						content: "新内容",
					},
					updateMask: { paths: ["content"] },
				},
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
		});
	});

	describe("deleteMemory", () => {
		it("calls DeleteMemory with the resource name", async () => {
			respond(fake.deleteMemory, {});
			const client = new MemoryClient(fake as never);

			await client.deleteMemory("saolei", "sess-1", "mem-abc");

			expect(fake.deleteMemory).toHaveBeenCalledTimes(1);
			expect(fake.deleteMemory).toHaveBeenCalledWith(
				{ name: "templates/saolei/sessions/sess-1/memories/mem-abc" },
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
		});
	});

	describe("listMemories", () => {
		it("returns {memory_id, content} entries from a single page", async () => {
			respond(fake.listMemories, {
				memories: [
					{ memoryId: "mem-a", content: "甲" },
					{ memoryId: "mem-b", content: "乙" },
				],
				nextPageToken: "",
			});
			const client = new MemoryClient(fake as never);

			const entries = await client.listMemories("saolei", "sess-1");

			expect(entries).toEqual([
				{ memory_id: "mem-a", content: "甲" },
				{ memory_id: "mem-b", content: "乙" },
			]);
			expect(fake.listMemories).toHaveBeenCalledTimes(1);
			expect(fake.listMemories).toHaveBeenCalledWith(
				{ parent: "templates/saolei/sessions/sess-1", pageToken: undefined },
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
		});

		it("walks pages until next_page_token is empty (AIP-158)", async () => {
			// Page 1 → token "p2"; page 2 → token "".
			fake.listMemories.mockImplementation(
				(
					req: { pageToken?: string },
					_metadata: unknown,
					_options: { deadline: Date },
					cb: (e: Error | null, r: unknown) => void,
				) => {
					if (!req.pageToken) {
						cb(null, {
							memories: [{ memoryId: "mem-a", content: "甲" }],
							nextPageToken: "p2",
						});
					} else {
						cb(null, {
							memories: [{ memoryId: "mem-b", content: "乙" }],
							nextPageToken: "",
						});
					}
				},
			);
			const client = new MemoryClient(fake as never);

			const entries = await client.listMemories("saolei", "sess-1");

			expect(entries).toEqual([
				{ memory_id: "mem-a", content: "甲" },
				{ memory_id: "mem-b", content: "乙" },
			]);
			expect(fake.listMemories).toHaveBeenCalledTimes(2);
			expect(fake.listMemories).toHaveBeenNthCalledWith(
				1,
				{ parent: "templates/saolei/sessions/sess-1", pageToken: undefined },
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
			expect(fake.listMemories).toHaveBeenNthCalledWith(
				2,
				{ parent: "templates/saolei/sessions/sess-1", pageToken: "p2" },
				expect.any(Object),
				expect.any(Object),
				expect.any(Function),
			);
		});

		it("rejects with the gRPC error", async () => {
			respond(
				fake.listMemories,
				null,
				Object.assign(new Error("memory service unavailable"), { code: 14 }),
			);
			const client = new MemoryClient(fake as never);

			await expect(client.listMemories("saolei", "sess-1")).rejects.toThrow(
				"memory service unavailable",
			);
		});
	});

	describe("close", () => {
		it("closes the underlying gRPC client", () => {
			const client = new MemoryClient(fake as never);
			client.close();
			expect(fake.close).toHaveBeenCalledTimes(1);
		});
	});

	describe("warmup", () => {
		it("resolves true when the channel is already READY", async () => {
			const channel = {
				getConnectivityState: vi.fn(() => 2), // READY
				watchConnectivityState: vi.fn(),
			};
			const client = new MemoryClient({ getChannel: () => channel } as never);

			await expect(client.warmup()).resolves.toBe(true);
			expect(channel.getConnectivityState).toHaveBeenCalledWith(true);
			expect(channel.watchConnectivityState).not.toHaveBeenCalled();
		});

		it("resolves false when watchConnectivityState times out", async () => {
			const channel = {
				getConnectivityState: vi.fn(() => 1), // CONNECTING forever
				watchConnectivityState: vi.fn(
					(_state: number, _deadline: Date, cb: (err?: Error) => void) => {
						cb(new Error("Deadline exceeded"));
					},
				),
			};
			const client = new MemoryClient({ getChannel: () => channel } as never);

			await expect(client.warmup()).resolves.toBe(false);
		});
	});

	describe("service target", () => {
		it("resolves the memory service via the dominion resolver", () => {
			expect(MEMORY_SERVICE_TARGET).toBe("dominion:///game/memory:50051");
		});

		it("shares the keepalive / round_robin channel options with prompt-client", () => {
			// The channel constants are imported from prompt-client (single
			// source of truth) — assert they still reach the channel options.
			const options = buildChannelOptionsForTest();
			expect(options?.["grpc.keepalive_time_ms"]).toBe(300_000);
			const serviceConfig = JSON.parse(
				options?.["grpc.service_config"] as string,
			);
			expect(serviceConfig.loadBalancingConfig).toEqual([
				{ round_robin: {} },
			]);
		});
	});
});
