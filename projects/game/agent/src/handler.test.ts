/**
 * handler.test.ts — Tests for AgentServiceServer handler implementations.
 *
 * Uses mocked PromptClient + AdapterManager, real ConnectionRegistry,
 * and REAL MemorySaver + StateGraph for ListMessages round-trip tests.
 *
 * No CreateAgent/DeleteAgent — agent binding is on-demand via Connect.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import { HumanMessage, AIMessage, SystemMessage } from "@langchain/core/messages";
import {
  MemorySaver,
  StateGraph,
  MessagesAnnotation,
} from "@langchain/langgraph";

import type { LLMAdapter, ContentBlock } from "./llm";
import type { AgentAdapter } from "./llm";
import { Handler } from "./handler";
import { ConnectionRegistry } from "./connection-registry";
import type { Connection } from "./connection-registry";

// ---------------------------------------------------------------------------
// FrameSender values (matching generated proto enum)
// ---------------------------------------------------------------------------

const FRAME_SENDER_USER = "FRAME_SENDER_USER";
const FRAME_SENDER_AGENT = "FRAME_SENDER_AGENT";
const FRAME_SENDER_SYSTEM = "FRAME_SENDER_SYSTEM";

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

interface MockProfile {
  model: string;
  systemPrompt: string;
}

function createMockPromptClient(
  profiles: Record<string, MockProfile> = {},
) {
  return {
    getProfile: vi.fn(async (name: string): Promise<MockProfile> => {
      const profile = profiles[name];
      if (!profile) {
        throw new Error(`Profile not found: ${name}`);
      }
      return profile;
    }),
    close: vi.fn(),
  };
}

function createMockLLMAdapter(blocks: ContentBlock[]): LLMAdapter {
  return {
    async *generateTurn(
      _model: string,
      _systemPrompt: string,
      _threadId: string,
      _userMessage: string,
      _checkpointer: unknown,
      _providerSecret: string,
    ): AsyncIterable<ContentBlock> {
      for (const block of blocks) {
        yield block;
      }
    },
  };
}

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
    ) {
      calls.push({ model, systemPrompt, threadId, userMessage });
      for (const block of blocks) {
        yield block;
      }
    },
  };
  return { adapter, calls };
}

interface MockAdapterManager {
  getOrCreateAdapter: ReturnType<typeof vi.fn>;
  getAdapterState: ReturnType<typeof vi.fn>;
  _setBinding(sessionId: string, profileName: string, adapter: LLMAdapter): void;
  _clearBindings(): void;
}

function createMockAdapterManager(
  defaultAdapter?: LLMAdapter,
): MockAdapterManager {
  const bindings = new Map<
    string,
    { adapter: LLMAdapter; profileName: string }
  >();

  return {
    getOrCreateAdapter: vi.fn(
      async (
        sessionId: string,
        profileName: string,
      ): Promise<LLMAdapter> => {
        const existing = bindings.get(sessionId);
        if (existing && existing.profileName === profileName) {
          return existing.adapter;
        }
        const adapter = defaultAdapter ?? createMockLLMAdapter([]);
        bindings.set(sessionId, { adapter, profileName });
        return adapter;
      },
    ),
    getAdapterState: vi.fn((sessionId: string) => {
      const binding = bindings.get(sessionId);
      return binding
        ? { activeProfileName: binding.profileName, isBound: true }
        : { activeProfileName: null, isBound: false };
    }),
    _setBinding(sessionId, profileName, adapter) {
      bindings.set(sessionId, { adapter, profileName });
    },
    _clearBindings() {
      bindings.clear();
    },
  };
}

/** Build a real MemorySaver + compiled StateGraph (MessagesAnnotation). */
function createRealGraph() {
  const checkpointer = new MemorySaver();
  const graph = new StateGraph(MessagesAnnotation)
    .addNode("pass", async () => ({}))
    .addEdge("__start__", "pass")
    .addEdge("pass", "__end__")
    .compile({ checkpointer });
  return { graph, checkpointer };
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

/** Standard text frame emitted by the client. */
function textFrame(
  sessionId: string,
  invokeId: string,
  content: string,
  profileName?: string,
) {
  return {
    sessionId,
    invokeId,
    payload: "text",
    text: { content },
    sender: FRAME_SENDER_USER,
    ...(profileName ? { agentProfileName: profileName } : {}),
  };
}

function flush(ms = 50): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// ---------------------------------------------------------------------------
// Handler factory
// ---------------------------------------------------------------------------

interface HandlerDeps {
  promptClient: ReturnType<typeof createMockPromptClient>;
  adapterManager: MockAdapterManager;
  connectionRegistry: ConnectionRegistry;
  checkpointer: MemorySaver;
  graph: unknown;
  providerSecret: string;
}

// Handler has an index signature `[name: string]: any` which prevents
// TypeScript from infusing constructor/method parameter types, so we
// cast loosely here.
function createHandler(deps: HandlerDeps): Handler {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const HandlerCtor = Handler as any;
  return new HandlerCtor(
    deps.promptClient,
    deps.adapterManager,
    deps.connectionRegistry,
    deps.checkpointer,
    deps.graph,
    deps.providerSecret,
  );
}

// ===========================================================================
// Tests: Connect — text frame produces thinking/text/wait frames
// ===========================================================================

describe("Handler.Connect text frame", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "helpful-assistant": {
        model: "opencode-go/deepseek-v4",
        systemPrompt: "You are helpful.",
      },
    });
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("produces thinking + text + wait frames for profile-bound text frame", async () => {
    const blocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "Let me think..." },
      { type: "text", text: "The answer is 42." },
    ];
    adapterManager._setBinding(
      "sess-1",
      "helpful-assistant",
      createMockLLMAdapter(blocks),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-1", "turn-1", "What is the answer?", "helpful-assistant"));
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
    adapterManager._setBinding(
      "sess-nt",
      "helpful-assistant",
      createMockLLMAdapter([{ type: "text", text: "Direct answer" }]),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-nt", "turn-2", "hello", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(2);
    expect((stream.written[0] as Record<string, unknown>).text).toEqual({
      content: "Direct answer",
    });
    expect((stream.written[1] as Record<string, unknown>).wait).toEqual({});
  });

  it("resets sequence on new invokeId within same connection", async () => {
    adapterManager._setBinding(
      "sess-seq",
      "helpful-assistant",
      createMockLLMAdapter([{ type: "text", text: "reply" }]),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-seq", "turn-a", "msg-1", "helpful-assistant"));
    await flush();
    stream.emit("data", textFrame("sess-seq", "turn-b", "msg-2", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(4);
    expect((stream.written[0] as Record<string, unknown>).sequence).toBe(0);
    expect((stream.written[0] as Record<string, unknown>).invokeId).toBe("turn-a");
    expect((stream.written[2] as Record<string, unknown>).sequence).toBe(0);
    expect((stream.written[2] as Record<string, unknown>).invokeId).toBe("turn-b");
  });

  it("generates unique frameId per frame", async () => {
    adapterManager._setBinding(
      "sess-uuid",
      "helpful-assistant",
      createMockLLMAdapter([
        { type: "reasoning", reasoning: "hmm" },
        { type: "text", text: "ok" },
      ]),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-uuid", "turn-u", "go", "helpful-assistant"));
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
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("returns warn frame when profile name is missing and no adapter is bound", async () => {
    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-missing", "inv-x", "hello"));
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

  it("does NOT call getOrCreateAdapter or generateTurn when profile is missing", async () => {
    const { adapter, calls } = createRecordingMockLLMAdapter([
      { type: "text", text: "should not happen" },
    ]);
    adapterManager._setBinding("sess-no-profile", "some-profile", adapter);

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-fresh", "inv-x", "hello"));
    await flush();

    expect(calls).toHaveLength(0);
  });
});

// ===========================================================================
// Tests: Connect — profile switch mid-connection
// ===========================================================================

describe("Handler.Connect profile switch", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "profile-a": { model: "model-a", systemPrompt: "Prompt A" },
      "profile-b": { model: "model-b", systemPrompt: "Prompt B" },
    });
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("second message with different profile uses new adapter from getOrCreateAdapter", async () => {
    const adapterA = createMockLLMAdapter([
      { type: "text", text: "Response from A" },
    ]);
    const adapterB = createMockLLMAdapter([
      { type: "text", text: "Response from B" },
    ]);

    const calls: string[] = [];
    let currentProfile = "profile-a";

    adapterManager.getOrCreateAdapter.mockImplementation(
      async (_sessionId: string, profileName: string) => {
        calls.push(profileName);
        currentProfile = profileName;
        return profileName === "profile-a" ? adapterA : adapterB;
      },
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-switch", "turn-1", "msg1", "profile-a"));
    await flush();

    stream.emit("data", textFrame("sess-switch", "turn-2", "msg2", "profile-b"));
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
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("emits warn frame and does not call generateTurn when profile not found", async () => {
    const promptClient = createMockPromptClient({
      "valid-profile": {
        model: "model-x",
        systemPrompt: "prompt-x",
      },
    });

    const { adapter, calls } = createRecordingMockLLMAdapter([
      { type: "text", text: "OK" },
    ]);
    const adapterManager = createMockAdapterManager();
    adapterManager._setBinding("sess-fail", "valid-profile", adapter);

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-fail", "turn-ok", "hello", "valid-profile"));
    await flush();

    stream.emit("data", textFrame("sess-fail", "turn-fail", "switch me", "nonexistent-profile"));
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
    expect(calls[0].userMessage).toBe("hello");

    expect(adapterManager.getAdapterState("sess-fail").activeProfileName).toBe(
      "valid-profile",
    );
  });
});

// ===========================================================================
// Tests: Connect — connection kick
// ===========================================================================

describe("Handler.Connect connection kick", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient({
      "helpful-assistant": {
        model: "model-x",
        systemPrompt: "prompt-x",
      },
    });
    adapterManager = createMockAdapterManager(
      createMockLLMAdapter([{ type: "text", text: "reply" }]),
    );
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("second connection for same session kicks the first (ends its stream)", async () => {
    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const stream1 = createFakeStream();
    handler.Connect(
      stream1 as unknown as Parameters<typeof handler.Connect>[0],
    );

    stream1.emit(
      "data",
      textFrame("sess-kick", "turn-1", "first", "helpful-assistant"),
    );
    await flush();

    expect(stream1.ended).toBe(false);

    const stream2 = createFakeStream();
    handler.Connect(
      stream2 as unknown as Parameters<typeof handler.Connect>[0],
    );

    stream2.emit(
      "data",
      textFrame("sess-kick", "turn-2", "second", "helpful-assistant"),
    );
    await flush();

    expect(stream1.ended).toBe(true);
    expect(stream2.ended).toBe(false);
  });

  it("kicked connection stops receiving frames mid-stream", async () => {
    const slowBlocks: ContentBlock[] = [
      { type: "reasoning", reasoning: "thinking 1" },
      { type: "reasoning", reasoning: "thinking 2" },
      { type: "text", text: "final answer" },
    ];
    const slowAdapter: LLMAdapter = {
      async *generateTurn() {
        for (const b of slowBlocks) {
          await new Promise((r) => setTimeout(r, 20));
          yield b;
        }
      },
    };
    adapterManager._setBinding("sess-mid", "helpful-assistant", slowAdapter);

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const stream1 = createFakeStream();
    handler.Connect(
      stream1 as unknown as Parameters<typeof handler.Connect>[0],
    );

    stream1.emit(
      "data",
      textFrame("sess-mid", "turn-slow", "long message", "helpful-assistant"),
    );
    await new Promise((r) => setTimeout(r, 30));

    const stream2 = createFakeStream();
    handler.Connect(
      stream2 as unknown as Parameters<typeof handler.Connect>[0],
    );
    stream2.emit(
      "data",
      textFrame("sess-mid", "turn-kick", "kick", "helpful-assistant"),
    );

    await new Promise((r) => setTimeout(r, 100));

    expect(stream1.ended).toBe(true);

    const agentFramesStream1 = stream1.written.filter(
      (f) => (f as Record<string, unknown>).sender === FRAME_SENDER_AGENT,
    );
    expect(agentFramesStream1.length).toBeLessThan(slowBlocks.length);
  });
});

// ===========================================================================
// Tests: Connect — deprecated payloads silently ignored
// ===========================================================================

describe("Handler.Connect deprecated payloads", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  function setupStream() {
    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);
    return stream;
  }

  it("silently ignores screenshot payload", async () => {
    const stream = setupStream();
    stream.emit("data", {
      sessionId: "sess-d",
      invokeId: "inv-s",
      payload: "screenshot",
      sender: FRAME_SENDER_USER,
    });
    await flush();
    expect(stream.written).toHaveLength(0);
  });

  it("silently ignores operation payload", async () => {
    const stream = setupStream();
    stream.emit("data", {
      sessionId: "sess-d",
      invokeId: "inv-o",
      payload: "operation",
      sender: FRAME_SENDER_USER,
    });
    await flush();
    expect(stream.written).toHaveLength(0);
  });
});

// ===========================================================================
// Tests: Connect — status & echo probes
// ===========================================================================

describe("Handler.Connect probes", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("responds to status probe with 'unknown' for unbound session", async () => {
    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
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
    adapterManager._setBinding(
      "sess-bound",
      "some-profile",
      createMockLLMAdapter([]),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
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
    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });
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
    const throwingAdapter: LLMAdapter = {
      generateTurn(): AsyncIterable<ContentBlock> {
        const it: AsyncIterator<ContentBlock> = {
          async next() {
            throw new Error("Provider timeout");
          },
        };
        return { [Symbol.asyncIterator]: () => it };
      },
    };
    const adapterManager = createMockAdapterManager();
    adapterManager._setBinding("sess-err", "error-profile", throwingAdapter);
    const connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer: real.checkpointer,
      graph: real.graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-err", "turn-err", "break me", "error-profile"));
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
  it("serializes concurrent text frames on same session (FIFO)", async () => {
    const promptClient = createMockPromptClient({
      "test-profile": { model: "m", systemPrompt: "s" },
    });

    let concurrentCount = 0;
    let maxConcurrent = 0;
    const processedMessages: string[] = [];

    const adapter: LLMAdapter = {
      async *generateTurn(
        _model: string,
        _systemPrompt: string,
        _threadId: string,
        userMessage: string,
      ) {
        concurrentCount++;
        maxConcurrent = Math.max(maxConcurrent, concurrentCount);
        processedMessages.push(userMessage);
        await new Promise((r) => setTimeout(r, 10));
        yield { type: "text", text: `Reply to: ${userMessage}` };
        concurrentCount--;
      },
    };

    const adapterManager = createMockAdapterManager();
    adapterManager._setBinding("sess-conc", "test-profile", adapter);
    const connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer: real.checkpointer,
      graph: real.graph,
      providerSecret: "secret",
    });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", textFrame("sess-conc", "turn-a", "msg-1", "test-profile"));
    stream.emit("data", textFrame("sess-conc", "turn-b", "msg-2", "test-profile"));

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
  let connectionRegistry: ConnectionRegistry;
  let checkpointer: MemorySaver;
  let graph: unknown;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    connectionRegistry = new ConnectionRegistry();
    const real = createRealGraph();
    checkpointer = real.checkpointer;
    graph = real.graph;
  });

  it("returns adapter state with active profile when bound", async () => {
    const adapterManager = createMockAdapterManager();
    adapterManager._setBinding(
      "sess-bound",
      "my-profile",
      createMockLLMAdapter([]),
    );

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

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
    const adapterManager = createMockAdapterManager();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

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
// Tests: ListMessages — REAL MemorySaver round-trip
// ===========================================================================

describe("Handler.ListMessages (real MemorySaver)", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let adapterManager: MockAdapterManager;
  let connectionRegistry: ConnectionRegistry;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    adapterManager = createMockAdapterManager();
    connectionRegistry = new ConnectionRegistry();
  });

  async function writeMessages(
    graph: unknown,
    sessionId: string,
    messages: Array<HumanMessage | AIMessage | SystemMessage>,
  ) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    await (graph as any).invoke(
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
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const sessionId = "sess-text-rt";
    await writeMessages(graph, sessionId, [
      new HumanMessage("Hello"),
      new AIMessage("Hi there!"),
    ]);

    const { error, response } = await listMessages(handler, sessionId);

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
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const sessionId = "sess-think-rt";
    await writeMessages(graph, sessionId, [
      new HumanMessage("Question"),
      new AIMessage({
        content: [{ type: "reasoning", reasoning: "Let me analyze..." }],
      }),
    ]);

    const { error, response } = await listMessages(handler, sessionId);

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);

    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
    expect(response!.messages![1].type).toBe("thinking");
    expect(response!.messages![1].content).toBe("Let me analyze...");
  });

  it("maps AIMessage with mixed reasoning + text to type 'text'", async () => {
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const sessionId = "sess-mixed-rt";
    await writeMessages(graph, sessionId, [
      new HumanMessage("Why?"),
      new AIMessage({
        content: [
          { type: "reasoning", reasoning: "Step 1" },
          { type: "text", text: "The answer is 42." },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, sessionId);

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);
    expect(response!.messages![1].type).toBe("text");
  });

  it("filters out SystemMessages from the result", async () => {
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const sessionId = "sess-sys-filter";
    await writeMessages(graph, sessionId, [
      new SystemMessage("You are a system prompt."),
      new HumanMessage("Hello"),
      new AIMessage("Hi!"),
      new SystemMessage("Another system message."),
    ]);

    const { error, response } = await listMessages(handler, sessionId);

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(2);
    for (const msg of response!.messages!) {
      expect(msg.type).not.toBe("warn");
    }
    expect(response!.messages![0].sender).toBe(FRAME_SENDER_USER);
    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
  });

  it("returns empty messages for session with no checkpoint state", async () => {
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const { error, response } = await listMessages(handler, "never-written");

    expect(error).toBeNull();
    expect(response!.messages ?? []).toHaveLength(0);
  });

  it("preserves chronological ordering across multiple turns", async () => {
    const { graph, checkpointer } = createRealGraph();

    const handler = createHandler({
      promptClient,
      adapterManager,
      connectionRegistry,
      checkpointer,
      graph,
      providerSecret: "secret",
    });

    const sessionId = "sess-chrono";
    await writeMessages(graph, sessionId, [new HumanMessage("first")]);
    await writeMessages(graph, sessionId, [new AIMessage("second")]);
    await writeMessages(graph, sessionId, [new HumanMessage("third")]);
    await writeMessages(graph, sessionId, [new AIMessage("fourth")]);

    const { error, response } = await listMessages(handler, sessionId);

    expect(error).toBeNull();
    expect(response!.messages).toHaveLength(4);
    expect(response!.messages![0].content).toBe("first");
    expect(response!.messages![1].content).toBe("second");
    expect(response!.messages![2].content).toBe("third");
    expect(response!.messages![3].content).toBe("fourth");
  });
});
