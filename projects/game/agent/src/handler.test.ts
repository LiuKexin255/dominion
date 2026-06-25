/**
 * handler.test.ts — Tests for AgentServiceServer handler implementations.
 *
 * Uses mocked PromptClient + SessionAgentStore, and REAL MemorySaver +
 * StateGraph for ListMessages round-trip tests.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import { HumanMessage, AIMessage, SystemMessage } from "@langchain/core/messages";
import {
  MemorySaver,
  StateGraph,
  MessagesAnnotation,
} from "@langchain/langgraph";

import type { AgentAdapter, AdapterFactory, ContentBlock, AdapterStateSnapshot, TurnContent } from "./llm";
import { Handler } from "./handler";
import { SessionAgent } from "./session-agent";
import { OperationBridge } from "./operation-bridge";

const FRAME_SENDER_USER = "FRAME_SENDER_USER";
const FRAME_SENDER_AGENT = "FRAME_SENDER_AGENT";
const FRAME_SENDER_SYSTEM = "FRAME_SENDER_SYSTEM";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

function createMockPromptClient(
  profiles: Record<string, { model: string; systemPrompt: string }> = {},
) {
  return {
    getProfile: vi.fn(async (name: string) => {
      const profile = profiles[name];
      if (!profile) {
        throw new Error(`Agent profile not found: ${name}`);
      }
      return profile;
    }),
    close: vi.fn(),
  };
}

function createMockAdapter(blocks: ContentBlock[]): AgentAdapter {
  return {
    async *generateTurn(): AsyncIterable<ContentBlock> {
      for (const block of blocks) yield block;
    },
    async getState() { return null; },
  };
}

function createRecordingAdapter(blocks: ContentBlock[]): {
  adapter: AgentAdapter;
  calls: Array<{ threadId: string; content: TurnContent }>;
} {
  const calls: Array<{ threadId: string; content: TurnContent }> = [];
  const adapter: AgentAdapter = {
    async *generateTurn(threadId, content) {
      calls.push({ threadId, content });
      for (const block of blocks) yield block;
    },
    async getState() { return null; },
  };
  return { adapter, calls };
}

interface MockBridge {
  registerSink: ReturnType<typeof vi.fn>;
  unregisterSink: ReturnType<typeof vi.fn>;
  handleResult: ReturnType<typeof vi.fn>;
  dispatch: ReturnType<typeof vi.fn>;
}

interface MockSessionAgent {
  getOrCreateAdapter: ReturnType<typeof vi.fn>;
  getAdapterState: ReturnType<typeof vi.fn>;
  getAdapter: ReturnType<typeof vi.fn>;
  invalidateAdapter: ReturnType<typeof vi.fn>;
  getBridge: ReturnType<typeof vi.fn>;
  bridge: MockBridge;
}

interface MockSessionAgentStore {
  getOrCreate: ReturnType<typeof vi.fn>;
  get: ReturnType<typeof vi.fn>;
  _getAgent(sessionId: string): MockSessionAgent;
  _setBinding(sessionId: string, profileName: string, adapter: AgentAdapter): void;
  _clear(): void;
}

function createMockBridge(): MockBridge {
  return {
    registerSink: vi.fn(),
    unregisterSink: vi.fn(),
    handleResult: vi.fn(),
    dispatch: vi.fn(),
  };
}

function createMockSessionAgentStore(): MockSessionAgentStore {
  const agents = new Map<string, MockSessionAgent>();

  function getAgent(sessionId: string): MockSessionAgent {
    if (!agents.has(sessionId)) {
      const bridge = createMockBridge();
      agents.set(sessionId, {
        getOrCreateAdapter: vi.fn(),
        getAdapterState: vi.fn(() => ({
          activeProfileName: null,
          isBound: false,
        })),
        getAdapter: vi.fn(() => null),
        invalidateAdapter: vi.fn(),
        getBridge: vi.fn(() => bridge),
        bridge,
      });
    }
    return agents.get(sessionId)!;
  }

  return {
    getOrCreate: vi.fn((sessionId: string) => getAgent(sessionId)),
    get: vi.fn((sessionId: string) => getAgent(sessionId)),
    _getAgent: getAgent,
    _setBinding(sessionId, profileName, adapter) {
      const agent = getAgent(sessionId);
      agent.getOrCreateAdapter.mockResolvedValue(adapter);
      agent.getAdapter.mockReturnValue(adapter);
      agent.getAdapterState.mockReturnValue({
        activeProfileName: profileName,
        isBound: true,
      });
    },
    _clear() {
      agents.clear();
    },
  };
}

function createUnaryCall<T>(request: T) {
  return { request } as grpc.ServerUnaryCall<T, unknown>;
}

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
  const callback: grpc.sendUnaryData<T> = (error, value) => {
    const svcError =
      error && "code" in error ? (error as grpc.ServiceError) : null;
    resolve({ error: svcError, response: value ?? null });
  };
  return { callback, promise };
}

interface FakeStream {
  on(event: string, handler: (...args: unknown[]) => void): FakeStream;
  write(data: unknown): void;
  end(): void;
  emit(event: string, ...args: unknown[]): void;
  written: unknown[];
  ended: boolean;
}

function createFakeStream(): FakeStream {
  const written: unknown[] = [];
  let ended = false;
  const listeners: Record<string, Array<(...args: unknown[]) => void>> = {};
  const stream: FakeStream = {
    on(event, handler) {
      if (!listeners[event]) listeners[event] = [];
      listeners[event].push(handler);
      return stream;
    },
    write(data) {
      written.push(data);
    },
    end() {
      ended = true;
    },
    emit(event, ...args) {
      const handlers = listeners[event] ?? [];
      for (const handler of handlers) {
        handler(...args);
      }
    },
    written,
    get ended() {
      return ended;
    },
  };
  return stream;
}

function userTurnFrame(
  sessionId: string,
  invokeId: string,
  text: string,
  profileName?: string,
) {
  return {
    sessionId,
    invokeId,
    payload: "user_turn",
    userTurn: { text },
    sender: FRAME_SENDER_USER,
    ...(profileName ? { agentProfileName: profileName } : {}),
  };
}

function userTurnWithImageFrame(
  sessionId: string,
  invokeId: string,
  text: string,
  image: { data: Uint8Array | string; encoding: string },
  profileName?: string,
) {
  return {
    sessionId,
    invokeId,
    payload: "user_turn",
    userTurn: { text, image },
    sender: FRAME_SENDER_USER,
    ...(profileName ? { agentProfileName: profileName } : {}),
  };
}

function flush(ms = 50): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

interface HandlerDeps {
  promptClient: ReturnType<typeof createMockPromptClient>;
  sessionAgentStore: MockSessionAgentStore;
}

function createHandler(deps: HandlerDeps): Handler {
  const HandlerCtor = Handler as any;
  return new HandlerCtor(
    deps.promptClient,
    deps.sessionAgentStore,
  );
}

// ===========================================================================
// Tests: Connect — user_turn frame produces thinking/text/wait frames
// ===========================================================================

describe("Handler.Connect user_turn frame", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "helpful-assistant": {
        model: "opencode-go/deepseek-v4",
        systemPrompt: "You are helpful.",
      },
    });
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("produces thinking + text + wait frames for profile-bound user_turn frame", async () => {
    const blocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "Let me think..." },
      { type: "text", text: "The answer is 42." },
    ];
    sessionAgentStore._setBinding(
      "sess-1",
      "helpful-assistant",
      createMockAdapter(blocks),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-1", "turn-1", "What is the answer?", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(3);

    const f0 = stream.written[0] as Record<string, unknown>;
    expect(f0.sender).toBe(FRAME_SENDER_AGENT);
    expect(f0.thinking).toEqual({ content: "Let me think..." });
    expect(f0.invokeId).toBe("turn-1");
    expect(f0.sequence).toBe(0);

    const f1 = stream.written[1] as Record<string, unknown>;
    expect(f1.sender).toBe(FRAME_SENDER_AGENT);
    expect(f1.text).toEqual({ content: "The answer is 42." });
    expect(f1.sequence).toBe(1);

    const f2 = stream.written[2] as Record<string, unknown>;
    expect(f2.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f2.wait).toEqual({});
    expect(f2.sequence).toBe(2);
  });

  it("produces text + wait for response with no thinking", async () => {
    sessionAgentStore._setBinding(
      "sess-nt",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "Direct answer" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-nt", "turn-2", "hello", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(2);
    expect((stream.written[0] as Record<string, unknown>).text).toEqual({
      content: "Direct answer",
    });
    expect((stream.written[1] as Record<string, unknown>).wait).toEqual({});
  });

  it("resets sequence on new invokeId within same connection", async () => {
    sessionAgentStore._setBinding(
      "sess-seq",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "reply" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-seq", "turn-a", "msg-1", "helpful-assistant"));
    await flush();
    stream.emit("data", userTurnFrame("sess-seq", "turn-b", "msg-2", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(4);
    expect((stream.written[0] as Record<string, unknown>).sequence).toBe(0);
    expect((stream.written[0] as Record<string, unknown>).invokeId).toBe("turn-a");
    expect((stream.written[2] as Record<string, unknown>).sequence).toBe(0);
    expect((stream.written[2] as Record<string, unknown>).invokeId).toBe("turn-b");
  });

  it("generates unique frameId per frame", async () => {
    sessionAgentStore._setBinding(
      "sess-uuid",
      "helpful-assistant",
      createMockAdapter([
        { type: "reasoning", reasoning: "hmm" },
        { type: "text", text: "ok" },
      ]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-uuid", "turn-u", "go", "helpful-assistant"));
    await flush();

    const frameIds = stream.written.map(
      (f) => (f as Record<string, unknown>).frameId,
    );
    expect(new Set(frameIds).size).toBe(frameIds.length);
  });
});

// ===========================================================================
// Tests: Connect — missing profile name
// ===========================================================================

describe("Handler.Connect missing profile name", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("returns warn frame when profile name is missing and no adapter is bound", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-missing", "inv-x", "hello"));
    await flush();

    expect(stream.written).toHaveLength(1);
    const f0 = stream.written[0] as Record<string, unknown>;
    expect(f0.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f0.warn).toBeDefined();
    expect((f0.warn as Record<string, unknown>).message).toContain(
      "agent_profile_name",
    );
    expect(f0.invokeId).toBe("inv-x");
  });

  it("does NOT call getOrCreateAdapter when profile is missing", async () => {
    const { adapter, calls } = createRecordingAdapter([
      { type: "text", text: "should not happen" },
    ]);
    sessionAgentStore._setBinding("sess-no-profile", "some-profile", adapter);

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-fresh", "inv-x", "hello"));
    await flush();

    expect(calls).toHaveLength(0);
  });
});

// ===========================================================================
// Tests: Connect — profile switch mid-connection
// ===========================================================================

describe("Handler.Connect profile switch", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "profile-a": { model: "model-a", systemPrompt: "Prompt A" },
      "profile-b": { model: "model-b", systemPrompt: "Prompt B" },
    });
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("second message with different profile uses new adapter from getOrCreateAdapter", async () => {
    const adapterA = createMockAdapter([{ type: "text", text: "Response from A" }]);
    const adapterB = createMockAdapter([{ type: "text", text: "Response from B" }]);

    const calls: string[] = [];
    const agent = sessionAgentStore._getAgent("sess-switch");
    agent.getOrCreateAdapter.mockImplementation(async (profileName: string) => {
      calls.push(profileName);
      return profileName === "profile-a" ? adapterA : adapterB;
    });
    agent.getAdapterState.mockReturnValue({ activeProfileName: null, isBound: false });

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-switch", "turn-1", "msg1", "profile-a"));
    await flush();

    stream.emit("data", userTurnFrame("sess-switch", "turn-2", "msg2", "profile-b"));
    await flush();

    expect(calls).toEqual(["profile-a", "profile-b"]);

    const texts = stream.written
      .filter(
        (f) =>
          (f as Record<string, unknown>).sender === FRAME_SENDER_AGENT &&
          (f as Record<string, unknown>).text,
      )
      .map(
        (f) =>
          ((f as Record<string, unknown>).text as Record<string, unknown>)
            .content,
      );
    expect(texts).toEqual(["Response from A", "Response from B"]);
  });
});

// ===========================================================================
// Tests: Connect — failed profile switch does not corrupt history
// ===========================================================================

describe("Handler.Connect failed profile switch", () => {
  it("emits warn frame and does not call generateTurn when profile not found", async () => {
    const promptClient = createMockPromptClient({
      "valid-profile": {
        model: "model-x",
        systemPrompt: "prompt-x",
      },
    });

    const { adapter, calls } = createRecordingAdapter([
      { type: "text", text: "OK" },
    ]);
    const sessionAgentStore = createMockSessionAgentStore();
    const agent = sessionAgentStore._getAgent("sess-fail");

    agent.getOrCreateAdapter
      .mockResolvedValueOnce(adapter)
      .mockRejectedValueOnce(new Error("Agent profile not found: nonexistent-profile"));

    agent.getAdapterState.mockReturnValue({ activeProfileName: "valid-profile", isBound: true });

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-fail", "turn-ok", "hello", "valid-profile"));
    await flush();

    stream.emit("data", userTurnFrame("sess-fail", "turn-fail", "switch me", "nonexistent-profile"));
    await flush();

    const warnFrames = stream.written.filter(
      (f) => (f as Record<string, unknown>).warn,
    );
    expect(warnFrames).toHaveLength(1);
    expect(
      ((warnFrames[0] as Record<string, unknown>).warn as Record<string, unknown>)
        .message,
    ).toContain("Agent profile not found");

    expect(calls).toHaveLength(1);
    expect(calls[0].content.text).toBe("hello");
  });
});

// ===========================================================================
// Tests: Connect — AgentOperationResultFrame dispatches to bridge.handleResult
// ===========================================================================

describe("Handler.Connect operation_result frame", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("calls getBridge().handleResult when operation_result frame arrives", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    const result = { operationId: "op-1", status: "AGENT_OPERATION_RESULT_STATUS_SUCCEEDED", message: "ok" };
    stream.emit("data", {
      sessionId: "sess-or",
      invokeId: "inv-or",
      payload: "operation_result",
      operationResult: result,
      sender: FRAME_SENDER_USER,
    });
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-or").bridge;
    expect(bridge.handleResult).toHaveBeenCalledTimes(1);
    expect(bridge.handleResult).toHaveBeenCalledWith(result);
  });

  it("does not write any frame for operation_result", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-or2",
      invokeId: "inv-or2",
      payload: "operation_result",
      operationResult: { operationId: "op-2", status: 1, message: "" },
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(0);
  });
});

// ===========================================================================
// Tests: Connect — OperationBridge sink lifecycle (register/unregister)
// ===========================================================================

describe("Handler.Connect bridge lifecycle", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "helpful-assistant": {
        model: "opencode-go/deepseek-v4",
        systemPrompt: "You are helpful.",
      },
    });
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("registers bridge sink on user_turn frame", async () => {
    sessionAgentStore._setBinding(
      "sess-sink",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-sink", "turn-1", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-sink").bridge;
    expect(bridge.registerSink).toHaveBeenCalledTimes(1);
    expect(typeof bridge.registerSink.mock.calls[0][0]).toBe("function");
  });

  it("calls generateTurn with text+image TurnContent when both provided", async () => {
    const { adapter, calls } = createRecordingAdapter([
      { type: "text", text: "done" },
    ]);
    sessionAgentStore._setBinding("sess-tc", "helpful-assistant", adapter);

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    const pngBytes = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a]);
    stream.emit(
      "data",
      userTurnWithImageFrame(
        "sess-tc",
        "turn-tc",
        "look at this",
        { data: pngBytes, encoding: "IMAGE_ENCODING_PNG" },
        "helpful-assistant",
      ),
    );
    await flush();

    expect(calls).toHaveLength(1);
    expect(calls[0].content).toEqual({
      text: "look at this",
      imageData: pngBytes.toString("base64"),
      imageMimeType: "image/png",
    });
  });

  it("calls generateTurn with text-only TurnContent when no image", async () => {
    const { adapter, calls } = createRecordingAdapter([
      { type: "text", text: "done" },
    ]);
    sessionAgentStore._setBinding("sess-tc2", "helpful-assistant", adapter);

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-tc2", "turn-tc2", "plain text", "helpful-assistant"));
    await flush();

    expect(calls).toHaveLength(1);
    expect(calls[0].content).toEqual({ text: "plain text" });
  });

  it("unregisters bridge sink on stream end for active sessions", async () => {
    sessionAgentStore._setBinding(
      "sess-end",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-end", "turn-end", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-end").bridge;
    expect(bridge.unregisterSink).not.toHaveBeenCalled();

    stream.emit("end");
    expect(bridge.unregisterSink).toHaveBeenCalledTimes(1);
  });

  it("unregisters bridge sink on stream error", async () => {
    sessionAgentStore._setBinding(
      "sess-err-sink",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-err-sink", "turn-err", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-err-sink").bridge;
    expect(bridge.unregisterSink).not.toHaveBeenCalled();

    stream.emit("error", new Error("socket reset"));
    expect(bridge.unregisterSink).toHaveBeenCalledTimes(1);
  });

  it("sink callback writes operation envelope to stream", async () => {
    sessionAgentStore._setBinding(
      "sess-write",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-write", "turn-w", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-write").bridge;
    const sinkFn = bridge.registerSink.mock.calls[0][0] as (f: unknown) => void;
    const envelope = { payload: "operation", operation: { operationId: "x" } };
    const before = stream.written.length;
    sinkFn(envelope);
    expect(stream.written.length).toBe(before + 1);
    expect(stream.written[stream.written.length - 1]).toBe(envelope);
  });
});

// ===========================================================================
// Tests: Connect — status & echo probes
// ===========================================================================

describe("Handler.Connect probes", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("responds to status probe with 'unknown' for unbound session", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-status",
      invokeId: "inv-st",
      payload: "status",
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(1);
    const f = stream.written[0] as Record<string, unknown>;
    expect(f.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f.status).toEqual({ status: "unknown" });
  });

  it("responds to status probe with 'idle' for bound session", async () => {
    sessionAgentStore._setBinding(
      "sess-bound",
      "some-profile",
      createMockAdapter([]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-bound",
      invokeId: "inv-st",
      payload: "status",
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(1);
    const f = stream.written[0] as Record<string, unknown>;
    expect(f.status).toEqual({ status: "idle" });
  });

  it("echoes echo payload back", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-echo",
      invokeId: "inv-ec",
      payload: "echo",
      echo: { data: "hello-echo" },
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(1);
    const f = stream.written[0] as Record<string, unknown>;
    expect(f.echo).toEqual({ data: "hello-echo" });
  });
});

// ===========================================================================
// Tests: Connect — LLM error handling
// ===========================================================================

describe("Handler.Connect LLM error", () => {
  it("emits warn frame on LLM error and keeps stream open", async () => {
    const promptClient = createMockPromptClient({
      "error-profile": { model: "m", systemPrompt: "s" },
    });
    const throwingAdapter: AgentAdapter = {
      generateTurn(): AsyncIterable<ContentBlock> {
        const it: AsyncIterator<ContentBlock> = {
          async next() {
            throw new Error("Provider timeout");
          },
        };
        return { [Symbol.asyncIterator]: () => it };
      },
      async getState() { return null; },
    };
    const sessionAgentStore = createMockSessionAgentStore();
    sessionAgentStore._setBinding("sess-err", "error-profile", throwingAdapter);

    const handler = createHandler({
      promptClient,
      sessionAgentStore,
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-err", "turn-err", "break me", "error-profile"));
    await flush();

    expect(stream.written.length).toBeGreaterThanOrEqual(1);
    expect(stream.ended).toBe(false);

    const warnFrames = stream.written.filter(
      (f) => (f as Record<string, unknown>).warn,
    );
    expect(warnFrames.length).toBeGreaterThanOrEqual(1);
  });
});

// ===========================================================================
// Tests: Connect — same-session serialization
// ===========================================================================

describe("Handler.Connect same-session serialization", () => {
  it("serializes concurrent user_turn frames on same session (FIFO)", async () => {
    const promptClient = createMockPromptClient({
      "test-profile": { model: "m", systemPrompt: "s" },
    });

    let concurrentCount = 0;
    let maxConcurrent = 0;
    const processedMessages: string[] = [];

    const adapter: AgentAdapter = {
      async *generateTurn(_threadId: string, content: TurnContent) {
        const userMessage = content.text ?? "";
        concurrentCount++;
        maxConcurrent = Math.max(maxConcurrent, concurrentCount);
        processedMessages.push(userMessage);
        await new Promise((r) => setTimeout(r, 10));
        yield { type: "text", text: `Reply to: ${userMessage}` };
        concurrentCount--;
      },
      async getState() { return null; },
    };

    const sessionAgentStore = createMockSessionAgentStore();
    sessionAgentStore._setBinding("sess-conc", "test-profile", adapter);

    const handler = createHandler({
      promptClient,
      sessionAgentStore,
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userTurnFrame("sess-conc", "turn-a", "msg-1", "test-profile"));
    stream.emit("data", userTurnFrame("sess-conc", "turn-b", "msg-2", "test-profile"));

    await new Promise((r) => setTimeout(r, 100));

    expect(maxConcurrent).toBe(1);
    expect(processedMessages).toEqual(["msg-1", "msg-2"]);
  });
});

// ===========================================================================
// Tests: GetAgent
// ===========================================================================

describe("Handler.GetAgent", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;

  beforeEach(() => {
    promptClient = createMockPromptClient();
  });

  it("returns adapter state with active profile when bound", async () => {
    const sessionAgentStore = createMockSessionAgentStore();
    sessionAgentStore._setBinding(
      "sess-bound",
      "my-profile",
      createMockAdapter([]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });

    const call = createUnaryCall({ sessionId: "sess-bound" });
    const { callback, promise } = createCallback<{
      name: string;
      sessionId: string;
      agentProfileName: string;
      createTime?: unknown;
    }>();

    handler.GetAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response!.sessionId).toBe("sess-bound");
    expect(response!.agentProfileName).toBe("my-profile");
    expect(response!.name).toBe("sessions/sess-bound/agent");
    expect(response!.createTime).toBeDefined();
  });

  it("returns empty profile for never-connected session (200 OK)", async () => {
    const sessionAgentStore = createMockSessionAgentStore();

    const handler = createHandler({ promptClient, sessionAgentStore });

    const call = createUnaryCall({ sessionId: "never-connected" });
    const { callback, promise } = createCallback<{
      agentProfileName: string;
    }>();

    handler.GetAgent(call, callback);
    const { error, response } = await promise;

    expect(error).toBeNull();
    expect(response!.agentProfileName).toBe("");
  });
});

// ===========================================================================
// Tests: RefreshAgent
// ===========================================================================

describe("Handler.RefreshAgent", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  function refreshAgent(handler: Handler, sessionId: string) {
    const call = createUnaryCall({ name: `sessions/${sessionId}/agent` });
    const { callback, promise } = createCallback<{}>();
    handler.RefreshAgent(call, callback);
    return promise;
  }

  it("succeeds and invalidates adapter when no turn is in-flight", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });

    const { error, response } = await refreshAgent(handler, "sess-refresh");

    expect(error).toBeNull();
    expect(response).toEqual({});

    const agent = sessionAgentStore._getAgent("sess-refresh");
    expect(agent.invalidateAdapter).toHaveBeenCalledTimes(1);
  });

  it("returns FAILED_PRECONDITION when a turn is in-flight", async () => {
    const blocks: ContentBlock[] = [{ type: "text", text: "late reply" }];
    const slowAdapter: AgentAdapter = {
      async *generateTurn(): AsyncIterable<ContentBlock> {
        await new Promise((r) => setTimeout(r, 50));
        for (const block of blocks) yield block;
      },
      async getState() { return null; },
    };
    sessionAgentStore._setBinding(
      "sess-busy",
      "test-profile",
      slowAdapter,
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userTurnFrame("sess-busy", "turn-1", "hello", "test-profile"),
    );

    await new Promise((r) => setTimeout(r, 10));

    const { error, response } = await refreshAgent(handler, "sess-busy");

    expect(error).not.toBeNull();
    expect(error!.code).toBe(grpc.status.FAILED_PRECONDITION);
    expect(response).toBeNull();

    const agent = sessionAgentStore._getAgent("sess-busy");
    expect(agent.invalidateAdapter).not.toHaveBeenCalled();

    await new Promise((r) => setTimeout(r, 60));
  });

  it("releases mutex so subsequent RefreshAgent calls still succeed", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });

    const first = await refreshAgent(handler, "sess-repeat");
    expect(first.error).toBeNull();

    const second = await refreshAgent(handler, "sess-repeat");
    expect(second.error).toBeNull();

    const agent = sessionAgentStore._getAgent("sess-repeat");
    expect(agent.invalidateAdapter).toHaveBeenCalledTimes(2);
  });

  it("does not block subsequent Connect turns after RefreshAgent", async () => {
    const adapter = createMockAdapter([
      { type: "text", text: "after refresh" },
    ]);
    const handler = createHandler({ promptClient, sessionAgentStore });

    const refreshResult = await refreshAgent(handler, "sess-post");
    expect(refreshResult.error).toBeNull();

    const agent = sessionAgentStore._getAgent("sess-post");
    agent.getOrCreateAdapter.mockResolvedValue(adapter);
    agent.getAdapterState.mockReturnValue({
      activeProfileName: "p",
      isBound: true,
    });

    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    stream.emit(
      "data",
      userTurnFrame("sess-post", "turn-after", "go", "p"),
    );
    await flush();

    expect(stream.written.length).toBeGreaterThanOrEqual(1);
    const textFrames = stream.written.filter(
      (f) =>
        (f as Record<string, unknown>).sender === FRAME_SENDER_AGENT &&
        (f as Record<string, unknown>).text,
    );
    expect(textFrames).toHaveLength(1);
  });
});

// ===========================================================================
// Tests: SessionAgent.invalidateAdapter + getBridge (integration)
// ===========================================================================

describe("SessionAgent.invalidateAdapter integration", () => {
  it("nulls adapter so next getOrCreateAdapter rebuilds", async () => {
    const created: AgentAdapter[] = [];
    const factory: AdapterFactory = async () => {
      const adapter = createMockAdapter([
        { type: "text", text: `adapter-${created.length}` },
      ]);
      created.push(adapter);
      return adapter;
    };

    const throwProvider = async () => {
      throw new Error("not used");
    };
    const agent = new SessionAgent(throwProvider, factory, new MemorySaver());

    const fetcher = async () => ({
      model: "m",
      systemPrompt: "s",
      toolNames: [],
    });

    const first = await agent.getOrCreateAdapter("p", fetcher);
    expect(created).toHaveLength(1);
    expect(agent.getAdapterState().isBound).toBe(true);

    agent.invalidateAdapter();
    expect(agent.getAdapter()).toBeNull();
    expect(agent.getAdapterState()).toEqual({
      activeProfileName: null,
      isBound: false,
    });

    const second = await agent.getOrCreateAdapter("p", fetcher);
    expect(second).not.toBe(first);
    expect(created).toHaveLength(2);
  });

  it("invalidateAdapter on never-bound session is a no-op", () => {
    const agent = new SessionAgent(
      async () => { throw new Error("x"); },
      async () => { throw new Error("x"); },
      new MemorySaver(),
    );

    expect(() => agent.invalidateAdapter()).not.toThrow();
    expect(agent.getAdapter()).toBeNull();
  });

  it("getBridge returns a stable OperationBridge instance", () => {
    const agent = new SessionAgent(
      async () => { throw new Error("x"); },
      async () => { throw new Error("x"); },
      new MemorySaver(),
    );

    const b1 = agent.getBridge();
    const b2 = agent.getBridge();
    expect(b1).toBe(b2);
    expect(b1).toBeInstanceOf(OperationBridge);
  });
});



describe("Handler.ListMessages (real MemorySaver)", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  function createStateAdapter(): {
    adapter: AgentAdapter;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    graph: any;
  } {
    const checkpointer = new MemorySaver();
    const graph = new StateGraph(MessagesAnnotation)
      .addNode("pass", async () => ({}))
      .addEdge("__start__", "pass")
      .addEdge("pass", "__end__")
      .compile({ checkpointer });

    const adapter: AgentAdapter = {
      async *generateTurn() {},
      async getState(threadId: string): Promise<AdapterStateSnapshot | null> {
        const snapshot = await graph.getState({
          configurable: { thread_id: threadId },
        });
        if (!snapshot) return null;
        return {
          values: snapshot.values ?? {},
          createdAt: snapshot.createdAt,
        };
      },
    };
    return { adapter, graph };
  }

  async function writeMessages(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    graph: any,
    sessionId: string,
    messages: Array<HumanMessage | AIMessage | SystemMessage>,
  ) {
    await graph.invoke(
      { messages },
      { configurable: { thread_id: sessionId } },
    );
  }

  async function listMessages(handler: Handler, sessionId: string) {
    const call = createUnaryCall({
      parent: `sessions/${sessionId}`,
    });
    const { callback, promise } = createCallback<{
      messages?: Array<Record<string, unknown>>;
    }>();
    handler.ListMessages(call, callback);
    return promise;
  }

  it("round-trips text messages (human + ai) in chronological order", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-text-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-text-rt", [
      new HumanMessage("Hello"),
      new AIMessage("Hi there!"),
    ]);

    const { error, response } = await listMessages(handler, "sess-text-rt");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);

    expect(response!.messages![0].sender).toBe(FRAME_SENDER_USER);
    expect(response!.messages![0].type).toBe("text");
    expect(response!.messages![0].content).toBe("Hello");

    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
    expect(response!.messages![1].type).toBe("text");
    expect(response!.messages![1].content).toBe("Hi there!");
  });

  it("maps AIMessage with only reasoning blocks to type 'thinking'", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-think-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-think-rt", [
      new HumanMessage("Question"),
      new AIMessage({
        content: [{ type: "reasoning", reasoning: "Let me analyze..." }],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-think-rt");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);

    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
    expect(response!.messages![1].type).toBe("thinking");
    expect(response!.messages![1].content).toBe("Let me analyze...");
  });

  it("maps AIMessage with mixed reasoning + text to type 'text'", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-mixed-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-mixed-rt", [
      new HumanMessage("Why?"),
      new AIMessage({
        content: [
          { type: "reasoning", reasoning: "Step 1" },
          { type: "text", text: "The answer is 42." },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-mixed-rt");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);
    expect(response!.messages![1].type).toBe("text");
  });

  it("reconstructs image content blocks as imageData", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-image-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-image-rt", [
      new HumanMessage({
        content: [
          { type: "text", text: "What is in this image?" },
          {
            type: "image_url",
            image_url: { url: "data:image/png;base64,base64imagedata" },
          },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-image-rt");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);

    expect(response!.messages![0].sender).toBe(FRAME_SENDER_USER);
    expect(response!.messages![0].type).toBe("text");
    expect(response!.messages![0].content).toBe("text");
    expect(response!.messages![0].text).toBe("What is in this image?");

    expect(response!.messages![1].sender).toBe(FRAME_SENDER_USER);
    expect(response!.messages![1].type).toBe("image");
    expect(response!.messages![1].content).toBe("imageData");
    expect(response!.messages![1].imageData).toBe("base64imagedata");
  });

  it("reconstructs image content blocks when data is a Uint8Array", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-image-uint8", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    const imageBytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);
    const expectedBase64 = Buffer.from(imageBytes).toString("base64");

    await writeMessages(graph, "sess-image-uint8", [
      new HumanMessage({
        content: [
          { type: "text", text: "look" },
          {
            type: "image",
            data: imageBytes,
            mime_type: "image/png",
          },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-image-uint8");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);
    expect(response!.messages![1].type).toBe("image");
    expect(response!.messages![1].imageData).toBe(expectedBase64);
  });

  it("filters out SystemMessages from the result", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-sys-filter", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-sys-filter", [
      new SystemMessage("You are a system prompt."),
      new HumanMessage("Hello"),
      new AIMessage("Hi!"),
      new SystemMessage("Another system message."),
    ]);

    const { error, response } = await listMessages(handler, "sess-sys-filter");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);
    for (const msg of response!.messages!) {
      expect(msg.type).not.toBe("warn");
    }
    expect(response!.messages![0].sender).toBe(FRAME_SENDER_USER);
    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
  });

  it("returns empty messages for session with no adapter bound", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });

    const { error, response } = await listMessages(handler, "never-bound");

    expect(error).toBeNull();
    expect(response!.messages ?? []).toHaveLength(0);
  });

  it("preserves chronological ordering across multiple turns", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-chrono", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-chrono", [new HumanMessage("first")]);
    await writeMessages(graph, "sess-chrono", [new AIMessage("second")]);
    await writeMessages(graph, "sess-chrono", [new HumanMessage("third")]);
    await writeMessages(graph, "sess-chrono", [new AIMessage("fourth")]);

    const { error, response } = await listMessages(handler, "sess-chrono");

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(4);
    expect(response!.messages![0].content).toBe("first");
    expect(response!.messages![1].content).toBe("second");
    expect(response!.messages![2].content).toBe("third");
    expect(response!.messages![3].content).toBe("fourth");
  });
});
