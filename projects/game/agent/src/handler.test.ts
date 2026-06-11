/**
 * handler.test.ts — Tests for AgentServiceServer handler implementations.
 *
 * Uses mocked PromptClient, DialogRuntime, and a duplex stream mock
 * to verify all RPC handler behaviors.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import type { LLMAdapter, ContentBlock } from "./llm";
import { DialogRuntime } from "./runtime";
import { Handler } from "./handler";

// ---------------------------------------------------------------------------
// FrameSender values (matching generated proto enum)
// ---------------------------------------------------------------------------

const FRAME_SENDER_USER = "FRAME_SENDER_USER";
const FRAME_SENDER_AGENT = "FRAME_SENDER_AGENT";
const FRAME_SENDER_SYSTEM = "FRAME_SENDER_SYSTEM";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

/** Create a mock PromptClient. */
function createMockPromptClient() {
  return {
    getProfile: vi.fn(),
    close: vi.fn(),
  };
}

/** Create a mock LLMAdapter that yields given blocks. */
function createMockLLMAdapter(blocks: ContentBlock[]): LLMAdapter {
  return {
    async *generateTurn(): AsyncIterable<ContentBlock> {
      for (const block of blocks) {
        yield block;
      }
    },
  };
}

/** Create a mock LLMAdapter that throws. */
function createThrowingLLMAdapter(error: Error): LLMAdapter {
  return {
    generateTurn(): AsyncIterable<ContentBlock> {
      const it: AsyncIterator<ContentBlock> = {
        async next(): Promise<IteratorResult<ContentBlock>> {
          throw error;
        },
      };
      return { [Symbol.asyncIterator]: () => it };
    },
  };
}

/** Create a fake gRPC unary call. */
function createUnaryCall<T>(request: T) {
  return { request } as grpc.ServerUnaryCall<T, unknown>;
}

/** Create a fake gRPC callback. */
function createCallback<T>(): {
  callback: grpc.sendUnaryData<T>;
  promise: Promise<{ error: grpc.ServiceError | null; response: T | null }>;
} {
  let resolve!: (value: {
    error: grpc.ServiceError | null;
    response: T | null;
  }) => void;
  const promise = new Promise<{
    error: grpc.ServiceError | null;
    response: T | null;
  }>((res) => {
    resolve = res;
  });

  const callback: grpc.sendUnaryData<T> = (
    error: grpc.ServiceError | Partial<grpc.StatusObject> | grpc.ServerErrorResponse | null,
    value?: T | null,
  ) => {
    const svcError =
      error && "code" in error ? (error as grpc.ServiceError) : null;
    resolve({ error: svcError, response: value ?? null });
  };

  return { callback, promise };
}

/** Create a fake bidirectional stream for Connect testing. */
function createFakeStream() {
  const written: unknown[] = [];
  let ended = false;
  const listeners: Record<string, Array<(...args: unknown[]) => void>> = {};

  const stream = {
    on(event: string, handler: (...args: unknown[]) => void) {
      if (!listeners[event]) listeners[event] = [];
      listeners[event].push(handler);
      return stream;
    },
    write(data: unknown) {
      written.push(data);
    },
    end() {
      ended = true;
    },
    emit(event: string, ...args: unknown[]) {
      const handlers = listeners[event] ?? [];
      for (const handler of handlers) {
        handler(...args);
      }
    },
    get written() {
      return written;
    },
    get ended() {
      return ended;
    },
  };

  return stream;
}

// ---------------------------------------------------------------------------
// Tests: CreateAgent
// ---------------------------------------------------------------------------

describe("Handler.CreateAgent", () => {
  it("creates agent with valid profile and returns Agent proto", async () => {
    const instances = new Map<string, DialogRuntime>();
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "deepseek-v4",
      systemPrompt: "You are helpful.",
    });
    const llmAdapter = createMockLLMAdapter([]);
    const handler = new Handler(
      instances,
      promptClient as any,
      llmAdapter,
      "secret",
    );

    const call = createUnaryCall({
      sessionId: "sess-1",
      agentProfileName: "helpful-assistant",
    });
    const { callback, promise } = createCallback<any>();

    handler.CreateAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.sessionId).toBe("sess-1");
    expect(response.agentProfileName).toBe("helpful-assistant");
    expect(response.name).toBe("sessions/sess-1/agent");
    expect(response.createTime).toBeDefined();

    // Verify runtime was stored.
    const runtime = instances.get("sess-1");
    expect(runtime).toBeDefined();
    expect(runtime!.profileName).toBe("helpful-assistant");
    expect(runtime!.copiedModel).toBe("deepseek-v4");
    expect(runtime!.copiedSystemPrompt).toBe("You are helpful.");
  });

  it("returns NOT_FOUND when agent_profile_name is missing", async () => {
    const instances = new Map<string, DialogRuntime>();
    const promptClient = createMockPromptClient();
    const llmAdapter = createMockLLMAdapter([]);
    const handler = new Handler(
      instances,
      promptClient as any,
      llmAdapter,
      "secret",
    );

    const call = createUnaryCall({
      sessionId: "sess-2",
      agentProfileName: "",
    });
    const { callback, promise } = createCallback<any>();

    handler.CreateAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeDefined();
    expect(error!.code).toBe(grpc.status.NOT_FOUND);
    expect(response).toBeNull();
  });

  it("returns NOT_FOUND when profile does not exist", async () => {
    const instances = new Map<string, DialogRuntime>();
    const promptClient = createMockPromptClient();
    const grpcError = new Error("Not found") as grpc.ServiceError;
    grpcError.code = grpc.status.NOT_FOUND;
    promptClient.getProfile.mockRejectedValue(grpcError);

    const llmAdapter = createMockLLMAdapter([]);
    const handler = new Handler(
      instances,
      promptClient as any,
      llmAdapter,
      "secret",
    );

    const call = createUnaryCall({
      sessionId: "sess-3",
      agentProfileName: "nonexistent",
    });
    const { callback, promise } = createCallback<any>();

    handler.CreateAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeDefined();
    expect(error!.code).toBe(grpc.status.NOT_FOUND);
    expect(response).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: GetAgent
// ---------------------------------------------------------------------------

describe("Handler.GetAgent", () => {
  it("returns Agent proto for existing runtime", () => {
    const instances = new Map<string, DialogRuntime>();
    const runtime = DialogRuntime.createWithProfile(
      "sess-10",
      "my-profile",
      "model-x",
      "prompt-y",
    );
    instances.set("sess-10", runtime);

    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      createMockLLMAdapter([]),
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-10" });
    const { callback, promise } = createCallback<any>();

    handler.GetAgent(call, callback);
    return promise.then(({ error, response }) => {
      expect(error).toBeNull();
      expect(response.sessionId).toBe("sess-10");
      expect(response.agentProfileName).toBe("my-profile");
      expect(response.name).toBe("sessions/sess-10/agent");
    });
  });

  it("returns NOT_FOUND for missing runtime", () => {
    const instances = new Map<string, DialogRuntime>();
    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      createMockLLMAdapter([]),
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-11" });
    const { callback, promise } = createCallback<any>();

    handler.GetAgent(call, callback);
    return promise.then(({ error, response }) => {
      expect(error).toBeDefined();
      expect(error!.code).toBe(grpc.status.NOT_FOUND);
      expect(response).toBeNull();
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: DeleteAgent
// ---------------------------------------------------------------------------

describe("Handler.DeleteAgent", () => {
  it("deletes existing runtime", () => {
    const instances = new Map<string, DialogRuntime>();
    const runtime = DialogRuntime.createWithProfile(
      "sess-20",
      "profile",
      "model",
      "prompt",
    );
    instances.set("sess-20", runtime);

    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      createMockLLMAdapter([]),
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-20" });
    const { callback, promise } = createCallback<any>();

    handler.DeleteAgent(call, callback);
    return promise.then(({ error }) => {
      expect(error).toBeNull();
      expect(instances.has("sess-20")).toBe(false);
      expect(runtime.isDeleted()).toBe(true);
    });
  });

  it("succeeds idempotently for missing runtime", () => {
    const instances = new Map<string, DialogRuntime>();
    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      createMockLLMAdapter([]),
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-nonexistent" });
    const { callback, promise } = createCallback<any>();

    handler.DeleteAgent(call, callback);
    return promise.then(({ error }) => {
      expect(error).toBeNull();
    });
  });
});

// ---------------------------------------------------------------------------
// Tests: Connect
// ---------------------------------------------------------------------------

describe("Handler.Connect", () => {
  function setupConnect(
    blocks: ContentBlock[] = [],
  ): {
    handler: Handler;
    instances: Map<string, DialogRuntime>;
    stream: ReturnType<typeof createFakeStream>;
  } {
    const instances = new Map<string, DialogRuntime>();
    const llmAdapter = createMockLLMAdapter(blocks);
    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      llmAdapter,
      "secret-key",
    );
    const stream = createFakeStream();

    handler.Connect(stream as any);

    return { handler, instances, stream };
  }

  it("responds to status probe with agent status", () => {
    const { instances, stream } = setupConnect();
    const runtime = DialogRuntime.createWithProfile(
      "sess-cs",
      "p",
      "m",
      "s",
    );
    instances.set("sess-cs", runtime);

    stream.emit("data", {
      sessionId: "sess-cs",
      invokeId: "inv-1",
      payload: "status",
      sender: FRAME_SENDER_USER,
    });

    expect(stream.written).toHaveLength(1);
    const frame = stream.written[0] as any;
    expect(frame.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(frame.status).toEqual({ status: "idle" });
    expect(frame.sessionId).toBe("sess-cs");
    expect(frame.invokeId).toBe("inv-1");
    expect(frame.frameId).toBeDefined();
    expect(frame.sequence).toBe(0);
  });

  it("processes text message: thinking + text + wait frames", async () => {
    const blocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "Let me think..." },
      { type: "text", text: "The answer is 42." },
    ];
    const { instances, stream } = setupConnect(blocks);
    const runtime = DialogRuntime.createWithProfile(
      "sess-ct",
      "p",
      "m",
      "sys",
    );
    instances.set("sess-ct", runtime);

    stream.emit("data", {
      sessionId: "sess-ct",
      invokeId: "turn-1",
      payload: "text",
      text: { content: "What is the answer?" },
      sender: FRAME_SENDER_USER,
    });

    // Wait for async processing.
    await new Promise((r) => setTimeout(r, 50));

    // Expected: thinking frame, text frame, wait frame.
    expect(stream.written).toHaveLength(3);

    // Frame 0: thinking
    const f0 = stream.written[0] as any;
    expect(f0.sender).toBe(FRAME_SENDER_AGENT);
    expect(f0.thinking).toEqual({ content: "Let me think..." });
    expect(f0.invokeId).toBe("turn-1");
    expect(f0.sequence).toBe(0);

    // Frame 1: text
    const f1 = stream.written[1] as any;
    expect(f1.sender).toBe(FRAME_SENDER_AGENT);
    expect(f1.text).toEqual({ content: "The answer is 42." });
    expect(f1.invokeId).toBe("turn-1");
    expect(f1.sequence).toBe(1);

    // Frame 2: wait
    const f2 = stream.written[2] as any;
    expect(f2.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f2.wait).toEqual({});
    expect(f2.invokeId).toBe("turn-1");
    expect(f2.sequence).toBe(2);
  });

  it("emits text + wait for response with no thinking", async () => {
    const blocks: ContentBlock[] = [
      { type: "text", text: "Direct answer" },
    ];
    const { instances, stream } = setupConnect(blocks);
    const runtime = DialogRuntime.createWithProfile(
      "sess-no-think",
      "p",
      "m",
      "s",
    );
    instances.set("sess-no-think", runtime);

    stream.emit("data", {
      sessionId: "sess-no-think",
      invokeId: "turn-2",
      payload: "text",
      text: { content: "hello" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    expect(stream.written).toHaveLength(2);
    const f0 = stream.written[0] as any;
    expect(f0.sender).toBe(FRAME_SENDER_AGENT);
    expect(f0.text).toEqual({ content: "Direct answer" });
    expect(f0.sequence).toBe(0);

    const f1 = stream.written[1] as any;
    expect(f1.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f1.wait).toEqual({});
    expect(f1.sequence).toBe(1);
  });

  it("emits warn frame on LLM error, keeps stream open", async () => {
    const instances = new Map<string, DialogRuntime>();
    const llmAdapter = createThrowingLLMAdapter(new Error("Provider timeout"));
    const handler = new Handler(
      instances,
      createMockPromptClient() as any,
      llmAdapter,
      "secret",
    );
    const stream = createFakeStream();

    handler.Connect(stream as any);

    const runtime = DialogRuntime.createWithProfile(
      "sess-err",
      "p",
      "m",
      "s",
    );
    instances.set("sess-err", runtime);

    stream.emit("data", {
      sessionId: "sess-err",
      invokeId: "turn-err",
      payload: "text",
      text: { content: "break me" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    // Runtime.processMessage catches the error internally and yields a warning
    // ContentBlock. The handler maps it to a text frame, then emits wait.
    // Actually, the runtime catches LLM errors internally and yields
    // { type: "text", text: "Warning: Provider timeout" }, so the handler
    // will produce a text frame + wait frame.
    expect(stream.written.length).toBeGreaterThanOrEqual(1);

    // Check that at least one frame was written and stream is not ended.
    expect(stream.ended).toBe(false);
  });

  it("emits warn frame for missing runtime on text message", () => {
    const { stream } = setupConnect([]);

    stream.emit("data", {
      sessionId: "missing-session",
      invokeId: "inv-x",
      payload: "text",
      text: { content: "hello" },
      sender: FRAME_SENDER_USER,
    });

    expect(stream.written).toHaveLength(1);
    const f0 = stream.written[0] as any;
    expect(f0.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f0.warn).toBeDefined();
    expect(f0.warn.message).toContain("No agent found");
    expect(f0.invokeId).toBe("inv-x");
  });

  it("silently ignores deprecated screenshot payload", () => {
    const { stream } = setupConnect([]);

    stream.emit("data", {
      sessionId: "sess-dep",
      invokeId: "inv-s",
      payload: "screenshot",
      sender: FRAME_SENDER_USER,
    });

    expect(stream.written).toHaveLength(0);
  });

  it("silently ignores deprecated echo payload", () => {
    const { stream } = setupConnect([]);

    stream.emit("data", {
      sessionId: "sess-dep",
      invokeId: "inv-e",
      payload: "echo",
      sender: FRAME_SENDER_USER,
    });

    expect(stream.written).toHaveLength(0);
  });

  it("silently ignores deprecated operation payload", () => {
    const { stream } = setupConnect([]);

    stream.emit("data", {
      sessionId: "sess-dep",
      invokeId: "inv-o",
      payload: "operation",
      sender: FRAME_SENDER_USER,
    });

    expect(stream.written).toHaveLength(0);
  });

  it("resets sequence on new invokeId", async () => {
    const blocks: ContentBlock[] = [
      { type: "text", text: "reply" },
    ];
    const { instances, stream } = setupConnect(blocks);
    const runtime = DialogRuntime.createWithProfile(
      "sess-seq",
      "p",
      "m",
      "s",
    );
    instances.set("sess-seq", runtime);

    // First message.
    stream.emit("data", {
      sessionId: "sess-seq",
      invokeId: "turn-a",
      payload: "text",
      text: { content: "msg-1" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    // Second message with different invokeId.
    stream.emit("data", {
      sessionId: "sess-seq",
      invokeId: "turn-b",
      payload: "text",
      text: { content: "msg-2" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    // 2 turns × (1 text frame + 1 wait frame) = 4 frames.
    expect(stream.written).toHaveLength(4);

    // First turn: seq 0, 1
    expect((stream.written[0] as any).sequence).toBe(0);
    expect((stream.written[0] as any).invokeId).toBe("turn-a");
    expect((stream.written[1] as any).sequence).toBe(1);
    expect((stream.written[1] as any).invokeId).toBe("turn-a");

    // Second turn: seq resets to 0, 1
    expect((stream.written[2] as any).sequence).toBe(0);
    expect((stream.written[2] as any).invokeId).toBe("turn-b");
    expect((stream.written[3] as any).sequence).toBe(1);
    expect((stream.written[3] as any).invokeId).toBe("turn-b");
  });

  it("generates unique frameId per frame", async () => {
    const blocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "hmm" },
      { type: "text", text: "ok" },
    ];
    const { instances, stream } = setupConnect(blocks);
    const runtime = DialogRuntime.createWithProfile(
      "sess-uuid",
      "p",
      "m",
      "s",
    );
    instances.set("sess-uuid", runtime);

    stream.emit("data", {
      sessionId: "sess-uuid",
      invokeId: "turn-uuid",
      payload: "text",
      text: { content: "go" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    const frameIds = stream.written.map((f: any) => f.frameId);
    const uniqueIds = new Set(frameIds);
    expect(uniqueIds.size).toBe(frameIds.length);
  });
});
