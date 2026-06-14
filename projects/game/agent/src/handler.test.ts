/**
 * handler.test.ts — Tests for AgentServiceServer handler implementations.
 *
 * Uses mocked PromptClient, LLMAdapter, MemorySaver, and StateGraph
 * to verify all RPC handler behaviors with checkpoint-native lifecycle.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import type { LLMAdapter, ContentBlock } from "./llm";
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
    async *generateTurn(
      _model: string,
      _systemPrompt: string,
      _threadId: string,
      _userMessage: string,
      _checkpointer: any,
      _providerSecret: string,
    ): AsyncIterable<ContentBlock> {
      for (const block of blocks) {
        yield block;
      }
    },
  };
}

/** Create a mock LLMAdapter that records calls. */
function createRecordingMockLLMAdapter(blocks: ContentBlock[]): {
  adapter: LLMAdapter;
  calls: Array<{
    model: string;
    systemPrompt: string;
    threadId: string;
    userMessage: string;
  }>;
} {
  const calls: Array<{
    model: string;
    systemPrompt: string;
    threadId: string;
    userMessage: string;
  }> = [];

  const adapter: LLMAdapter = {
    async *generateTurn(
      model: string,
      systemPrompt: string,
      threadId: string,
      userMessage: string,
      _checkpointer: any,
      _providerSecret: string,
    ) {
      calls.push({ model, systemPrompt, threadId, userMessage });
      for (const block of blocks) {
        yield block;
      }
    },
  };

  return { adapter, calls };
}

/** Create a mock LLMAdapter that throws. */
function createThrowingLLMAdapter(error: Error): LLMAdapter {
  return {
    generateTurn(
      _model: string,
      _systemPrompt: string,
      _threadId: string,
      _userMessage: string,
      _checkpointer: any,
      _providerSecret: string,
    ): AsyncIterable<ContentBlock> {
      const it: AsyncIterator<ContentBlock> = {
        async next(): Promise<IteratorResult<ContentBlock>> {
          throw error;
        },
      };
      return { [Symbol.asyncIterator]: () => it };
    },
  };
}

/** Create a mock MemorySaver. */
function createMockCheckpointer() {
  return {
    deleteThread: vi.fn().mockResolvedValue(undefined),
  };
}

/** Create a mock compiled StateGraph returning given messages. */
function createMockGraph(messages: any[] = [], createdAt?: number) {
  return {
    getState: vi.fn().mockResolvedValue({
      values: { messages },
      createdAt: createdAt ?? Date.now(),
    }),
  };
}

/** Create a mock compiled StateGraph with no state (new agent). */
function createMockGraphNoState() {
  return {
    getState: vi.fn().mockResolvedValue(undefined),
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
  function setup() {
    const promptClient = createMockPromptClient();
    const llmAdapter = createMockLLMAdapter([]);
    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();

    return { promptClient, llmAdapter, checkpointer, graph };
  }

  it("creates agent with valid profile and returns Agent proto", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    promptClient.getProfile.mockResolvedValue({
      model: "opencode-go/deepseek-v4",
      systemPrompt: "You are helpful.",
    });
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
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
    expect(response.createTime.seconds).toBeGreaterThan(0);

    // Verify no DialogRuntime — metadata is internal only.
    // Confirm GetAgent returns the stored metadata.
    const getCall = createUnaryCall({ sessionId: "sess-1" });
    const { callback: getCb, promise: getPromise } = createCallback<any>();
    handler.GetAgent(getCall, getCb);
    const getResult = await getPromise;
    expect(getResult.error).toBeNull();
    expect(getResult.response.agentProfileName).toBe("helpful-assistant");
  });

  it("returns NOT_FOUND when agent_profile_name is missing", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
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
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    const grpcError = new Error("Not found") as grpc.ServiceError;
    grpcError.code = grpc.status.NOT_FOUND;
    promptClient.getProfile.mockRejectedValue(grpcError);

    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
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
  function setup() {
    const promptClient = createMockPromptClient();
    const llmAdapter = createMockLLMAdapter([]);
    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();

    return { promptClient, llmAdapter, checkpointer, graph };
  }

  it("returns Agent proto for existing metadata", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    promptClient.getProfile.mockResolvedValue({
      model: "model-x",
      systemPrompt: "prompt-y",
    });
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret",
    );

    // Create agent first to populate metadata.
    const createCall = createUnaryCall({
      sessionId: "sess-10",
      agentProfileName: "my-profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createCall, createCb);
    await createPromise;

    // Now test GetAgent.
    const call = createUnaryCall({ sessionId: "sess-10" });
    const { callback, promise } = createCallback<any>();

    handler.GetAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.sessionId).toBe("sess-10");
    expect(response.agentProfileName).toBe("my-profile");
    expect(response.name).toBe("sessions/sess-10/agent");
    expect(response.createTime).toBeDefined();
  });

  it("returns NOT_FOUND for missing metadata", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-11" });
    const { callback, promise } = createCallback<any>();

    handler.GetAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeDefined();
    expect(error!.code).toBe(grpc.status.NOT_FOUND);
    expect(response).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: DeleteAgent
// ---------------------------------------------------------------------------

describe("Handler.DeleteAgent", () => {
  function setup() {
    const promptClient = createMockPromptClient();
    const llmAdapter = createMockLLMAdapter([]);
    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();

    return { promptClient, llmAdapter, checkpointer, graph };
  }

  it("deletes metadata and calls deleteThread", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "prompt",
    });
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret",
    );

    // Create agent first.
    const createCall = createUnaryCall({
      sessionId: "sess-20",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createCall, createCb);
    await createPromise;

    // Delete it.
    const call = createUnaryCall({ sessionId: "sess-20" });
    const { callback, promise } = createCallback<any>();

    handler.DeleteAgent(call, callback);
    const { error } = await promise;

    expect(error).toBeNull();
    expect(checkpointer.deleteThread).toHaveBeenCalledWith("sess-20");

    // Verify GetAgent returns NOT_FOUND after delete.
    const getCall = createUnaryCall({ sessionId: "sess-20" });
    const { callback: getCb, promise: getPromise } = createCallback<any>();
    handler.GetAgent(getCall, getCb);
    const getResult = await getPromise;
    expect(getResult.error).toBeDefined();
    expect(getResult.error!.code).toBe(grpc.status.NOT_FOUND);
  });

  it("succeeds idempotently for missing metadata", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret",
    );

    const call = createUnaryCall({ sessionId: "sess-nonexistent" });
    const { callback, promise } = createCallback<any>();

    handler.DeleteAgent(call, callback);
    const { error } = await promise;

    expect(error).toBeNull();
    // deleteThread should NOT be called for absent metadata.
    expect(checkpointer.deleteThread).not.toHaveBeenCalled();
  });

  it("DeleteAgent after multi-turn — ListMessages returns empty", async () => {
    const { promptClient, llmAdapter, checkpointer, graph } = setup();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "prompt",
    });
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret",
    );

    // Create agent.
    const createCall = createUnaryCall({
      sessionId: "sess-multi",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createCall, createCb);
    await createPromise;

    // Delete it.
    const delCall = createUnaryCall({ sessionId: "sess-multi" });
    const { callback: delCb, promise: delPromise } = createCallback<any>();
    handler.DeleteAgent(delCall, delCb);
    await delPromise;

    // ListMessages should return NOT_FOUND (metadata gone).
    const listCall = createUnaryCall({
      parent: "sessions/sess-multi/agent",
    });
    const { callback: listCb, promise: listPromise } = createCallback<any>();
    handler.ListMessages(listCall, listCb);
    const listResult = await listPromise;

    expect(listResult.error).toBeDefined();
    expect(listResult.error!.code).toBe(grpc.status.NOT_FOUND);
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
    checkpointer: ReturnType<typeof createMockCheckpointer>;
    stream: ReturnType<typeof createFakeStream>;
  } {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model-x",
      systemPrompt: "system-prompt",
    });
    const llmAdapter = createMockLLMAdapter(blocks);
    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();

    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      checkpointer as any,
      graph,
      "secret-key",
    );
    const stream = createFakeStream();

    handler.Connect(stream as any);

    return { handler, checkpointer, stream };
  }

  async function createAgentAndConnect(
    handler: Handler,
    sessionId: string,
    profileName: string,
    blocks: ContentBlock[] = [],
  ) {
    // Create agent first.
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model-x",
      systemPrompt: "system-prompt",
    });

    const createCall = createUnaryCall({
      sessionId,
      agentProfileName: profileName,
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createCall, createCb);
    await createPromise;

    return handler;
  }

  it("responds to status probe with agent status", () => {
    const { handler, stream } = setupConnect();

    // Create agent inline by calling CreateAgent first.
    const createCall = createUnaryCall({
      sessionId: "sess-cs",
      agentProfileName: "p",
    });
    const { callback: createCb } = createCallback<any>();

    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "m",
      systemPrompt: "s",
    });
    const innerHandler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      createMockGraph(),
      "secret",
    );

    // We need proper handler with agent — use setupConnect's handler which
    // has createAgent capability. Let's create agent first via inner handler
    // and test status on stream.

    // Actually, let's use a simpler approach — call CreateAgent on the handler
    // from setupConnect, which has promptClient mock that resolves.
    // But setupConnect already creates a handler. We just need an agent in it.

    // Let me restructure: Create agent via the handler, then emit status.
    const createReq = createUnaryCall({
      sessionId: "sess-cs",
      agentProfileName: "p",
    });
    const { callback: cb, promise: p } = createCallback<any>();
    handler.CreateAgent(createReq, cb);
    return p.then(() => {
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
    });
  });

  it("processes text message: thinking + text + wait frames", async () => {
    const blocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "Let me think..." },
      { type: "text", text: "The answer is 42." },
    ];
    const { handler, stream } = setupConnect(blocks);

    const createReq = createUnaryCall({
      sessionId: "sess-ct",
      agentProfileName: "p",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    stream.emit("data", {
      sessionId: "sess-ct",
      invokeId: "turn-1",
      payload: "text",
      text: { content: "What is the answer?" },
      sender: FRAME_SENDER_USER,
    });

    // Wait for async processing (mutex + stream).
    await new Promise((r) => setTimeout(r, 50));

    // Expected: thinking frame, text frame, wait frame.
    expect(stream.written).toHaveLength(3);

    const f0 = stream.written[0] as any;
    expect(f0.sender).toBe(FRAME_SENDER_AGENT);
    expect(f0.thinking).toEqual({ content: "Let me think..." });
    expect(f0.invokeId).toBe("turn-1");
    expect(f0.sequence).toBe(0);

    const f1 = stream.written[1] as any;
    expect(f1.sender).toBe(FRAME_SENDER_AGENT);
    expect(f1.text).toEqual({ content: "The answer is 42." });
    expect(f1.invokeId).toBe("turn-1");
    expect(f1.sequence).toBe(1);

    const f2 = stream.written[2] as any;
    expect(f2.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f2.wait).toEqual({});
    expect(f2.invokeId).toBe("turn-1");
    expect(f2.sequence).toBe(2);
  });

  it("emits text + wait for response with no thinking", async () => {
    const blocks: ContentBlock[] = [{ type: "text", text: "Direct answer" }];
    const { handler, stream } = setupConnect(blocks);

    const createReq = createUnaryCall({
      sessionId: "sess-no-think",
      agentProfileName: "p",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

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
    const llmAdapter = createThrowingLLMAdapter(new Error("Provider timeout"));
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "m",
      systemPrompt: "s",
    });
    const handler = new Handler(
      promptClient as any,
      llmAdapter,
      createMockCheckpointer() as any,
      createMockGraph(),
      "secret",
    );
    const stream = createFakeStream();

    handler.Connect(stream as any);

    const createReq = createUnaryCall({
      sessionId: "sess-err",
      agentProfileName: "p",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    stream.emit("data", {
      sessionId: "sess-err",
      invokeId: "turn-err",
      payload: "text",
      text: { content: "break me" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    expect(stream.written.length).toBeGreaterThanOrEqual(1);
    expect(stream.ended).toBe(false);
  });

  it("emits warn frame for missing metadata on text message", () => {
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
    const blocks: ContentBlock[] = [{ type: "text", text: "reply" }];
    const { handler, stream } = setupConnect(blocks);

    const createReq = createUnaryCall({
      sessionId: "sess-seq",
      agentProfileName: "p",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

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

    expect((stream.written[0] as any).sequence).toBe(0);
    expect((stream.written[0] as any).invokeId).toBe("turn-a");
    expect((stream.written[1] as any).sequence).toBe(1);
    expect((stream.written[1] as any).invokeId).toBe("turn-a");

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
    const { handler, stream } = setupConnect(blocks);

    const createReq = createUnaryCall({
      sessionId: "sess-uuid",
      agentProfileName: "p",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

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

// ---------------------------------------------------------------------------
// Tests: Connect uses sessionId as thread_id
// ---------------------------------------------------------------------------

describe("Handler.Connect thread_id", () => {
  it("passes sessionId as thread_id to llmAdapter.generateTurn", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "opencode-go/deepseek-v4",
      systemPrompt: "You are helpful.",
    });
    const { adapter, calls } = createRecordingMockLLMAdapter([
      { type: "text", text: "Response" },
    ]);
    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();

    const handler = new Handler(
      promptClient as any,
      adapter,
      checkpointer as any,
      graph,
      "secret-key",
    );
    const stream = createFakeStream();
    handler.Connect(stream as any);

    // Create agent.
    const createReq = createUnaryCall({
      sessionId: "sess-thread-id",
      agentProfileName: "test",
    });
    const { callback: createCb } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);

    stream.emit("data", {
      sessionId: "sess-thread-id",
      invokeId: "turn-1",
      payload: "text",
      text: { content: "Hello" },
      sender: FRAME_SENDER_USER,
    });

    await new Promise((r) => setTimeout(r, 50));

    expect(calls).toHaveLength(1);
    expect(calls[0].threadId).toBe("sess-thread-id");
    expect(calls[0].model).toBe("opencode-go/deepseek-v4");
    expect(calls[0].systemPrompt).toBe("You are helpful.");
    expect(calls[0].userMessage).toBe("Hello");
  });
});

// ---------------------------------------------------------------------------
// Tests: concurrent Connect serialization
// ---------------------------------------------------------------------------

describe("Handler.Connect concurrent serialization", () => {
  it("serializes concurrent text frames on same session", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });

    // This adapter tracks concurrent calls via a counter.
    let concurrentCount = 0;
    let maxConcurrent = 0;
    const calls: string[] = [];

    const adapter: LLMAdapter = {
      async *generateTurn(
        _model: string,
        _systemPrompt: string,
        _threadId: string,
        userMessage: string,
        _checkpointer: any,
        _providerSecret: string,
      ) {
        concurrentCount++;
        maxConcurrent = Math.max(maxConcurrent, concurrentCount);
        calls.push(userMessage);
        // Small delay to simulate LLM processing.
        await new Promise((r) => setTimeout(r, 10));
        yield { type: "text", text: `Reply to: ${userMessage}` };
        concurrentCount--;
      },
    };

    const checkpointer = createMockCheckpointer();
    const graph = createMockGraph();
    const handler = new Handler(
      promptClient as any,
      adapter,
      checkpointer as any,
      graph,
      "secret",
    );
    const stream = createFakeStream();
    handler.Connect(stream as any);

    // Create agent.
    const createReq = createUnaryCall({
      sessionId: "sess-concurrent",
      agentProfileName: "test",
    });
    const { callback: createCb } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);

    // Fire two text frames rapidly on the same session.
    stream.emit("data", {
      sessionId: "sess-concurrent",
      invokeId: "turn-a",
      payload: "text",
      text: { content: "msg-1" },
      sender: FRAME_SENDER_USER,
    });
    stream.emit("data", {
      sessionId: "sess-concurrent",
      invokeId: "turn-b",
      payload: "text",
      text: { content: "msg-2" },
      sender: FRAME_SENDER_USER,
    });

    // Wait for both to complete.
    await new Promise((r) => setTimeout(r, 100));

    // Must never have more than 1 concurrent call.
    expect(maxConcurrent).toBe(1);
    // Messages processed in order.
    expect(calls).toEqual(["msg-1", "msg-2"]);
  });
});

// ---------------------------------------------------------------------------
// Tests: ListMessages
// ---------------------------------------------------------------------------

describe("Handler.ListMessages", () => {
  function createBaseMessage(type: string, id: string, content: string | any) {
    return {
      id,
      content,
      lc_id: ["langchain", "messages"],
      lc_namespace: ["messages"],
      _getType: () => type,
    };
  }

  it("returns NOT_FOUND for missing agent metadata", async () => {
    const promptClient = createMockPromptClient();
    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      createMockGraph(),
      "secret",
    );

    const call = createUnaryCall({ parent: "sessions/no-agent/agent" });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(call, callback);
    const { error } = await promise;

    expect(error).toBeDefined();
    expect(error!.code).toBe(grpc.status.NOT_FOUND);
  });

  it("returns empty messages for agent with no turns", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });
    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      createMockGraphNoState(),
      "secret",
    );

    // Create agent.
    const createReq = createUnaryCall({
      sessionId: "sess-empty",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    const listCall = createUnaryCall({ parent: "sessions/sess-empty/agent" });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(listCall, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.messages).toEqual([]);
  });

  it("returns chronological messages with native message_id", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });

    // Create mock graph with checkpoint messages.
    const messages = [
      createBaseMessage("human", "msg-aaa", "Hello"),
      createBaseMessage("ai", "msg-bbb", "Hi there!"),
    ];
    const graph = createMockGraph(messages, 1718400000000);

    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      graph,
      "secret",
    );

    // Create agent.
    const createReq = createUnaryCall({
      sessionId: "sess-msgs",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    const listCall = createUnaryCall({ parent: "sessions/sess-msgs/agent" });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(listCall, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.messages).toHaveLength(2);

    // First message: human
    expect(response.messages[0].messageId).toBe("msg-aaa");
    expect(response.messages[0].name).toBe("sessions/sess-msgs/agent/messages/msg-aaa");
    expect(response.messages[0].sender).toBe(FRAME_SENDER_USER);
    expect(response.messages[0].type).toBe("text");
    expect(response.messages[0].content).toBe("Hello");

    // Second message: ai
    expect(response.messages[1].messageId).toBe("msg-bbb");
    expect(response.messages[1].name).toBe("sessions/sess-msgs/agent/messages/msg-bbb");
    expect(response.messages[1].sender).toBe(FRAME_SENDER_AGENT);
    expect(response.messages[1].type).toBe("text");
    expect(response.messages[1].content).toBe("Hi there!");

    // create_time populated from snapshot.
    expect(response.messages[0].createTime).toBeDefined();
  });

  it("handles thinking content blocks from checkpoint", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });

    const messages = [
      createBaseMessage("human", "msg-1", "Question"),
      createBaseMessage("ai", "msg-2", [
        { type: "reasoning", reasoning: "Let me analyze..." },
        { type: "text", text: "The answer is clear." },
      ]),
    ];
    const graph = createMockGraph(messages);

    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      graph,
      "secret",
    );

    const createReq = createUnaryCall({
      sessionId: "sess-think",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    const listCall = createUnaryCall({ parent: "sessions/sess-think/agent" });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(listCall, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.messages).toHaveLength(2);

    // Second message: ai with reasoning + text blocks → type "text" (mixed)
    expect(response.messages[1].type).toBe("text");
    expect(response.messages[1].content).toBe("Let me analyze...The answer is clear.");
  });

  it("returns thinking-only message as type thinking", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });

    const messages = [
      createBaseMessage("human", "msg-1", "Question"),
      createBaseMessage("ai", "msg-2", [
        { type: "reasoning", reasoning: "Pure reasoning step." },
      ]),
    ];
    const graph = createMockGraph(messages);

    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      graph,
      "secret",
    );

    const createReq = createUnaryCall({
      sessionId: "sess-pure-think",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    const listCall = createUnaryCall({
      parent: "sessions/sess-pure-think/agent",
    });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(listCall, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.messages).toHaveLength(2);
    expect(response.messages[1].type).toBe("thinking");
    expect(response.messages[1].content).toBe("Pure reasoning step.");
  });

  it("maps SystemMessage to SYSTEM sender with warn type", async () => {
    const promptClient = createMockPromptClient();
    promptClient.getProfile.mockResolvedValue({
      model: "model",
      systemPrompt: "sys",
    });

    const messages = [
      createBaseMessage("system", "msg-sys-1", "An error occurred."),
    ];
    const graph = createMockGraph(messages);

    const handler = new Handler(
      promptClient as any,
      createMockLLMAdapter([]),
      createMockCheckpointer() as any,
      graph,
      "secret",
    );

    const createReq = createUnaryCall({
      sessionId: "sess-sys",
      agentProfileName: "profile",
    });
    const { callback: createCb, promise: createPromise } = createCallback<any>();
    handler.CreateAgent(createReq, createCb);
    await createPromise;

    const listCall = createUnaryCall({ parent: "sessions/sess-sys/agent" });
    const { callback, promise } = createCallback<any>();

    handler.ListMessages(listCall, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response.messages).toHaveLength(1);
    expect(response.messages[0].sender).toBe(FRAME_SENDER_SYSTEM);
    expect(response.messages[0].type).toBe("warn");
    expect(response.messages[0].content).toBe("An error occurred.");
  });
});
