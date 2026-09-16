/**
 * MemoryClient tests — migrated from the v1 agent's memory-client.test.ts
 * (spec 039 T014; removed with agent v1 in spec 059 US1). The DI seam is the
 * injected gRPC client, so every RPC test passes a `vi.fn()`-backed fake and
 * needs no module-level `vi.mock`. Channel options are asserted directly
 * through the exported builder (no real channel constructed).
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  MEMORY_SERVICE_TARGET,
  MemoryClient,
  buildChannelOptions,
  memoryName,
} from "./client.js";

/**
 * The real gRPC stub contract is `rpc(request, metadata, options, callback)`;
 * fakes must match that 4-arg shape so the 4th positional arg is the callback.
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

  describe("memoryName (resource name construction)", () => {
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
    it("returns {memory_id, content} entries without options (full-accumulation mode)", async () => {
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

    it("given pageSize, issues exactly one request with pageSize/orderBy and returns the first page", async () => {
      respond(fake.listMemories, {
        memories: [{ memoryId: "mem-a", content: "甲" }],
        nextPageToken: "p2",
      });
      const client = new MemoryClient(fake as never);

      const entries = await client.listMemories("saolei", "sess-1", {
        orderBy: "update_time desc",
        pageSize: 10,
      });

      expect(entries).toEqual([{ memory_id: "mem-a", content: "甲" }]);
      expect(fake.listMemories).toHaveBeenCalledTimes(1);
      expect(fake.listMemories).toHaveBeenCalledWith(
        {
          parent: "templates/saolei/sessions/sess-1",
          pageToken: undefined,
          pageSize: 10,
          orderBy: "update_time desc",
        },
        expect.any(Object),
        expect.any(Object),
        expect.any(Function),
      );
    });

    it("without options walks pages until next_page_token is empty", async () => {
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

  describe("service target and channel options", () => {
    it("resolves the memory service via the dominion resolver", () => {
      expect(MEMORY_SERVICE_TARGET).toBe("dominion:///game/memory:50051");
    });

    it("uses no app-level keepalive and round_robin (v1 client policy)", () => {
      const options = buildChannelOptions();
      expect(options?.["grpc.keepalive_time_ms"]).toBeUndefined();
      expect(options?.["grpc.keepalive_permit_without_calls"]).toBeUndefined();
      const serviceConfig = JSON.parse(
        options?.["grpc.service_config"] as string,
      );
      expect(serviceConfig.loadBalancingConfig).toEqual([{ round_robin: {} }]);
      expect(options?.["grpc.initial_reconnect_backoff_ms"]).toBe(1_000);
      expect(options?.["grpc.max_reconnect_backoff_ms"]).toBe(15_000);
    });
  });
});
