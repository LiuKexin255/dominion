/**
 * handler.test.ts — Tests for AgentServiceServer handler implementations.
 *
 * Uses mocked PromptClient + SessionAgentStore, and REAL MemorySaver +
 * StateGraph for ListMessages round-trip tests.
 *
 * Part-model contract: user turns and agent output are content frames
 * (PartBlock) distinguished by `sender`; tool results are content frames
 * carrying a ToolResultPart. No invoke_id/sequence/echo machinery.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import * as grpc from "@grpc/grpc-js";

import { HumanMessage, AIMessage, SystemMessage, ToolMessage } from "@langchain/core/messages";
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

/** Build an inbound user messageParts frame (TextPart, sender USER). */
function userContentFrame(
  sessionId: string,
  text: string,
  profileName?: string,
) {
  return {
    sessionId,
    payload: "messageParts",
    messageParts: { parts: [{ text: { content: text } }] },
    sender: FRAME_SENDER_USER,
    ...(profileName ? { agentProfileName: profileName } : {}),
  };
}

/** Build an inbound user messageParts frame carrying text + image. */
function userContentWithImageFrame(
  sessionId: string,
  text: string,
  image: { data: Uint8Array | string; encoding: string },
  profileName?: string,
) {
  return {
    sessionId,
    payload: "messageParts",
    messageParts: {
      parts: [
        { text: { content: text } },
        { image: { data: image.data, encoding: image.encoding } },
      ],
    },
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
// Tests: Connect — user content frame produces thinking/text/wait frames
// ===========================================================================

describe("Handler.Connect user content frame", () => {
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

  it("produces thinking + text + wait frames for profile-bound user content frame", async () => {
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

    stream.emit("data", userContentFrame("sess-1", "What is the answer?", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(3);

    const f0 = stream.written[0] as Record<string, unknown>;
    expect(f0.sender).toBe(FRAME_SENDER_AGENT);
    expect(f0.payload).toBe("messageParts");
    expect(f0.messageParts).toEqual({
      parts: [{ thinking: { content: "Let me think..." } }],
    });

    const f1 = stream.written[1] as Record<string, unknown>;
    expect(f1.sender).toBe(FRAME_SENDER_AGENT);
    expect(f1.messageParts).toEqual({
      parts: [{ text: { content: "The answer is 42." } }],
    });

    const f2 = stream.written[2] as Record<string, unknown>;
    expect(f2.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f2.payload).toBe("flowParts");
    expect(f2.flowParts).toEqual({ parts: [{ wait: {} }] });
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

    stream.emit("data", userContentFrame("sess-nt", "hello", "helpful-assistant"));
    await flush();

    expect(stream.written).toHaveLength(2);
    expect((stream.written[0] as Record<string, unknown>).messageParts).toEqual({
      parts: [{ text: { content: "Direct answer" } }],
    });
    const waitFrame = stream.written[1] as Record<string, unknown>;
    expect(waitFrame.flowParts).toEqual({ parts: [{ wait: {} }] });
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

    stream.emit("data", userContentFrame("sess-uuid", "go", "helpful-assistant"));
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

  it("returns warn + wait frames when profile name is missing and no adapter is bound", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-missing", "hello"));
    await flush();

    // WarnSignal (the rejection) followed by a WaitSignal so the desktop's
    // typing indicator clears and the operator can retry
    // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3).
    expect(stream.written).toHaveLength(2);
    const f0 = stream.written[0] as Record<string, unknown>;
    expect(f0.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f0.payload).toBe("flowParts");
    const f0Warn = (f0.flowParts as { parts: { warn?: { message?: string } }[] }).parts[0].warn;
    expect(f0Warn).toBeDefined();
    expect(f0Warn!.message).toContain("agent_profile_name");

    const f1 = stream.written[1] as Record<string, unknown>;
    expect(f1.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f1.payload).toBe("flowParts");
    const f1Wait = (f1.flowParts as { parts: { wait?: unknown }[] }).parts[0].wait;
    expect(f1Wait).toBeDefined();
    // No profile name on this path → agentProfileName is the empty string.
    expect(f1.agentProfileName).toBe("");
  });

  it("does NOT call getOrCreateAdapter when profile is missing", async () => {
    const { adapter, calls } = createRecordingAdapter([
      { type: "text", text: "should not happen" },
    ]);
    sessionAgentStore._setBinding("sess-no-profile", "some-profile", adapter);

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-fresh", "hello"));
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

    stream.emit("data", userContentFrame("sess-switch", "msg1", "profile-a"));
    await flush();

    stream.emit("data", userContentFrame("sess-switch", "msg2", "profile-b"));
    await flush();

    expect(calls).toEqual(["profile-a", "profile-b"]);

    const texts = stream.written
      .filter(
        (f) =>
          (f as Record<string, unknown>).sender === FRAME_SENDER_AGENT &&
          (f as Record<string, unknown>).payload === "messageParts",
      )
      .map((f) => {
        const parts = ((f as Record<string, unknown>).messageParts as { parts: { text?: { content?: string } }[] }).parts;
        return parts[0]?.text?.content ?? "";
      });
    expect(texts).toEqual(["Response from A", "Response from B"]);
  });
});

// ===========================================================================
// Tests: Connect — profile-name guard rejects a mismatched turn (non-fatal)
// (specs/021-agent-session-resync/quickstart.md Scenario 3;
// specs/021-agent-session-resync/data-model.md §5;
// specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §3)
// ===========================================================================

describe("Handler.Connect profile-name guard", () => {
  it("rejects a mismatched turn with warn+wait and never invokes the adapter", async () => {
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

    // The adapter is bound once for the matching turn; a later mismatched
    // turn MUST NOT reach getOrCreateAdapter (the guard rejects it first).
    agent.getOrCreateAdapter.mockResolvedValue(adapter);
    agent.getAdapterState.mockReturnValue({ activeProfileName: "valid-profile", isBound: true });

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // Matching turn proceeds normally.
    stream.emit("data", userContentFrame("sess-fail", "hello", "valid-profile"));
    await flush();
    expect(calls).toHaveLength(1);
    expect(calls[0].content.text).toBe("hello");

    // Isolate the mismatched turn's output from the matching turn's frames.
    stream.written.length = 0;

    // Mismatched turn: bound to "valid-profile" but targets "nonexistent-profile".
    stream.emit("data", userContentFrame("sess-fail", "switch me", "nonexistent-profile"));
    await flush();

    // warn + wait are now FlowParts (a warn FlowPart, then a wait FlowPart).
    const flowWarnFrames = stream.written.filter(
      (f) => {
        const fr = f as Record<string, unknown>;
        return fr.payload === "flowParts" &&
          (fr.flowParts as { parts: { warn?: unknown }[] }).parts.some((p) => p.warn);
      },
    );
    expect(flowWarnFrames).toHaveLength(1);
    const warnMessage = ((flowWarnFrames[0] as Record<string, unknown>).flowParts as { parts: { warn?: { message?: string } }[] }).parts.find((p) => p.warn)!.warn!.message;
    expect(warnMessage).toContain("profile mismatch");
    expect(warnMessage).toContain("valid-profile");
    expect(warnMessage).toContain("nonexistent-profile");

    // The WaitSignal returns the desktop to ready (clears the typing indicator).
    const flowWaitFrames = stream.written.filter(
      (f) => {
        const fr = f as Record<string, unknown>;
        return fr.payload === "flowParts" &&
          (fr.flowParts as { parts: { wait?: unknown }[] }).parts.some((p) => p.wait);
      },
    );
    expect(flowWaitFrames).toHaveLength(1);
    expect(
      (flowWaitFrames[0] as Record<string, unknown>).agentProfileName,
    ).toBe("nonexistent-profile");

    // The mismatched turn never acquired the mutex / invoked the adapter:
    // generateTurn was still called only once (the matching turn), and
    // getOrCreateAdapter was called only once.
    expect(calls).toHaveLength(1);
    expect(agent.getOrCreateAdapter).toHaveBeenCalledTimes(1);
  });
});

// ===========================================================================
// Tests: Connect — FlowResultPart control frame dispatches to bridge.handleResult
// ===========================================================================

describe("Handler.Connect flow result control frame", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("calls getBridge().handleResult when a FlowResultPart control frame arrives", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    const flowResult = {
      toolId: "tool-1",
      status: "TOOL_RESULT_STATUS_SUCCEEDED",
      message: "ok",
    };
    stream.emit("data", {
      sessionId: "sess-or",
      payload: "flowParts",
      flowParts: { parts: [{ flowResult }] },
      sender: FRAME_SENDER_SYSTEM,
    });
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-or").bridge;
    expect(bridge.handleResult).toHaveBeenCalledTimes(1);
    expect(bridge.handleResult).toHaveBeenCalledWith(flowResult);
  });

  it("does not write any frame for a flow result flowParts frame", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-or2",
      payload: "flowParts",
      flowParts: { parts: [{ flowResult: { toolId: "tool-2", status: 1, message: "" } }] },
      sender: FRAME_SENDER_SYSTEM,
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

  it("registers bridge sink on user content frame", async () => {
    sessionAgentStore._setBinding(
      "sess-sink",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-sink", "hi", "helpful-assistant"));
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
      userContentWithImageFrame(
        "sess-tc",
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

    stream.emit("data", userContentFrame("sess-tc2", "plain text", "helpful-assistant"));
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

    stream.emit("data", userContentFrame("sess-end", "hi", "helpful-assistant"));
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

    stream.emit("data", userContentFrame("sess-err-sink", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-err-sink").bridge;
    expect(bridge.unregisterSink).not.toHaveBeenCalled();

    stream.emit("error", new Error("socket reset"));
    expect(bridge.unregisterSink).toHaveBeenCalledTimes(1);
  });

  it("sink callback writes content envelope to stream", async () => {
    sessionAgentStore._setBinding(
      "sess-write",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-write", "hi", "helpful-assistant"));
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-write").bridge;
    const sinkFn = bridge.registerSink.mock.calls[0][0] as (f: unknown) => void;
    const envelope = { payload: "flowParts", flowParts: { parts: [{ mouseMove: { toolId: "x", xPx: 1, yPx: 2 } }] } };
    const before = stream.written.length;
    sinkFn(envelope);
    expect(stream.written.length).toBe(before + 1);
    expect(stream.written[stream.written.length - 1]).toBe(envelope);
  });

  it("forwards the registerSink handle to unregisterSink on stream end", async () => {
    // T007: cleanupSinks must pass the per-session handle (the value returned
    // by registerSink) to unregisterSink, so the bridge's compare-and-delete
    // clears only THIS stream's sink — a stale close cannot clobber a fresh
    // reconnect's registration
    // (specs/021-agent-session-resync/contracts/agent-session-lifecycle-contract.md §1).
    const sentinelHandle = (): void => {};
    sessionAgentStore._setBinding(
      "sess-handle",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "ok" }]),
    );
    const agent = sessionAgentStore._getAgent("sess-handle");
    agent.bridge.registerSink.mockReturnValue(sentinelHandle);

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", userContentFrame("sess-handle", "hi", "helpful-assistant"));
    await flush();

    stream.emit("end");

    expect(agent.bridge.unregisterSink).toHaveBeenCalledTimes(1);
    expect(agent.bridge.unregisterSink).toHaveBeenCalledWith(sentinelHandle);
  });
});

// ===========================================================================
// Tests: Connect — desktop disconnect aborts in-flight turn
// ===========================================================================

describe("Handler.Connect abort lifecycle", () => {
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

  // Adapter that captures the signal passed to generateTurn and parks the
  // generator on the signal's abort event after each yield, so a test can
  // emit stream end/error mid-turn and observe the abort.
  function createAbortAwareAdapter(blocks: ContentBlock[]): {
    adapter: AgentAdapter;
    capturedSignal: () => AbortSignal | undefined;
    yieldedCount: () => number;
  } {
    let signal: AbortSignal | undefined;
    let count = 0;
    const adapter: AgentAdapter = {
      async *generateTurn(_threadId, _content, sig) {
        signal = sig;
        for (const block of blocks) {
          if (sig?.aborted) return;
          count++;
          yield block;
          if (sig) {
            await new Promise<void>((resolve) => {
              if (sig.aborted) {
                resolve();
                return;
              }
              sig.addEventListener("abort", () => resolve(), { once: true });
            });
          }
        }
      },
      async getState() { return null; },
    };
    return { adapter, capturedSignal: () => signal, yieldedCount: () => count };
  }

  it("stream end aborts in-flight turn via AbortController", async () => {
    const blocks: ContentBlock[] = [
      { type: "text", text: "chunk-1" },
      { type: "text", text: "chunk-2" },
      { type: "text", text: "chunk-3" },
    ];
    const { adapter, capturedSignal, yieldedCount } =
      createAbortAwareAdapter(blocks);
    sessionAgentStore._setBinding(
      "sess-abort-end",
      "helpful-assistant",
      adapter,
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userContentFrame("sess-abort-end", "hi", "helpful-assistant"),
    );
    await flush();

    stream.emit("end");
    await flush();

    const signal = capturedSignal();
    expect(signal).toBeDefined();
    expect(signal!.aborted).toBe(true);
    expect(yieldedCount()).toBeLessThan(blocks.length);

    // wait/warn now ride as FlowParts kinds; assert none were emitted on abort.
    const waitWarnFrames = stream.written.filter(
      (f) => {
        const fr = f as { payload?: string; flowParts?: { parts?: { wait?: unknown; warn?: unknown }[] } };
        if (fr.payload !== "flowParts") return false;
        return (fr.flowParts?.parts ?? []).some((p) => p.wait || p.warn);
      },
    );
    expect(waitWarnFrames).toHaveLength(0);
  });

  it("stream error aborts in-flight turn", async () => {
    const blocks: ContentBlock[] = [
      { type: "text", text: "chunk-1" },
      { type: "text", text: "chunk-2" },
      { type: "text", text: "chunk-3" },
    ];
    const { adapter, capturedSignal, yieldedCount } =
      createAbortAwareAdapter(blocks);
    sessionAgentStore._setBinding(
      "sess-abort-err",
      "helpful-assistant",
      adapter,
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userContentFrame("sess-abort-err", "hi", "helpful-assistant"),
    );
    await flush();

    stream.emit("error", new Error("socket reset"));
    await flush();

    const signal = capturedSignal();
    expect(signal).toBeDefined();
    expect(signal!.aborted).toBe(true);
    expect(yieldedCount()).toBeLessThan(blocks.length);

    // wait/warn now ride as FlowParts kinds; assert none were emitted on abort.
    const waitWarnFrames = stream.written.filter(
      (f) => {
        const fr = f as { payload?: string; flowParts?: { parts?: { wait?: unknown; warn?: unknown }[] } };
        if (fr.payload !== "flowParts") return false;
        return (fr.flowParts?.parts ?? []).some((p) => p.wait || p.warn);
      },
    );
    expect(waitWarnFrames).toHaveLength(0);
  });

  it("turn boundary disconnect: abort is a no-op on empty map", async () => {
    sessionAgentStore._setBinding(
      "sess-boundary",
      "helpful-assistant",
      createMockAdapter([{ type: "text", text: "done" }]),
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit(
      "data",
      userContentFrame("sess-boundary", "hi", "helpful-assistant"),
    );
    await flush();

    const bridge = sessionAgentStore._getAgent("sess-boundary").bridge;
    expect(bridge.unregisterSink).not.toHaveBeenCalled();

    // Turn already completed: finally block deleted the controller from
    // activeTurns. abortAllTurns must be a no-op, not throw.
    stream.emit("end");
    expect(bridge.unregisterSink).toHaveBeenCalledTimes(1);
  });
});

// ===========================================================================
// Tests: Connect — status probe
// ===========================================================================

describe("Handler.Connect probes", () => {
  let promptClient: ReturnType<typeof createMockPromptClient>;
  let sessionAgentStore: MockSessionAgentStore;

  beforeEach(() => {
    promptClient = createMockPromptClient();
    sessionAgentStore = createMockSessionAgentStore();
  });

  it("responds to status probe with 'unspecified' for unbound session", async () => {
    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    stream.emit("data", {
      sessionId: "sess-status",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(1);
    const f = stream.written[0] as Record<string, unknown>;
    expect(f.sender).toBe(FRAME_SENDER_SYSTEM);
    expect(f.payload).toBe("flowParts");
    // The status response rides as a FlowPart kind (spec 023 C3 / FR-003).
    // proto-loader enums:String serializes StatusSignalStatus as the full proto
    // name; the handler emits the UNSPECIFIED variant for an unbound session.
    const statusPart = (f.flowParts as { parts: { status?: { status?: string } }[] }).parts[0].status;
    expect(statusPart).toEqual({ status: "STATUS_SIGNAL_STATUS_UNSPECIFIED" });
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
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
      sender: FRAME_SENDER_USER,
    });
    await flush();

    expect(stream.written).toHaveLength(1);
    const f = stream.written[0] as Record<string, unknown>;
    const statusPart = (f.flowParts as { parts: { status?: { status?: string } }[] }).parts[0].status;
    expect(statusPart).toEqual({ status: "STATUS_SIGNAL_STATUS_IDLE" });
  });

  it("responds to status probe with 'active' while a turn is in-flight", async () => {
    // A turn in-flight = the per-session turn mutex is held (acquired before
    // adapter invocation, released in the turn finally). Drive it with a slow
    // adapter so the mutex stays held across the probe; isMutexHeld is the
    // ACTIVE source (specs/021-agent-session-resync/data-model.md §1).
    const slowAdapter: AgentAdapter = {
      async *generateTurn(): AsyncIterable<ContentBlock> {
        await new Promise((r) => setTimeout(r, 50));
        yield { type: "text", text: "late reply" };
      },
      async getState() { return null; },
    };
    sessionAgentStore._setBinding(
      "sess-active",
      "test-profile",
      slowAdapter,
    );

    const handler = createHandler({ promptClient, sessionAgentStore });
    const stream = createFakeStream();
    handler.Connect(stream as unknown as Parameters<typeof handler.Connect>[0]);

    // Start a turn — acquires the session mutex and parks inside generateTurn.
    stream.emit("data", userContentFrame("sess-active", "hello", "test-profile"));
    await new Promise((r) => setTimeout(r, 10));

    // Probe while the turn is in-flight (mutex held). The status branch writes
    // its response synchronously during emit.
    stream.emit("data", {
      sessionId: "sess-active",
      payload: "flowParts",
      flowParts: { parts: [{ status: {} }] },
      sender: FRAME_SENDER_USER,
    });
    await flush();

    // The session is bound, so without the in-flight turn the response would
    // be IDLE; ACTIVE proves isMutexHeld was consulted.
    const statusFrame = stream.written.find(
      (f) => {
        const fr = f as Record<string, unknown>;
        return fr.payload === "flowParts" &&
          (fr.flowParts as { parts: { status?: unknown }[] }).parts.some((p) => p.status);
      },
    ) as Record<string, unknown> | undefined;
    expect(statusFrame).toBeDefined();
    expect(statusFrame!.sender).toBe(FRAME_SENDER_SYSTEM);
    const statusPart = (statusFrame!.flowParts as { parts: { status?: { status?: string } }[] }).parts.find((p) => p.status)!.status;
    expect(statusPart).toEqual({ status: "STATUS_SIGNAL_STATUS_ACTIVE" });

    // Let the in-flight turn complete so its finally releases the mutex.
    await new Promise((r) => setTimeout(r, 60));
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

    stream.emit("data", userContentFrame("sess-err", "break me", "error-profile"));
    await flush();

    expect(stream.written.length).toBeGreaterThanOrEqual(1);
    expect(stream.ended).toBe(false);

    const warnFrames = stream.written.filter(
      (f) => {
        const fr = f as Record<string, unknown>;
        return fr.payload === "flowParts" &&
          (fr.flowParts as { parts: { warn?: unknown }[] }).parts.some((p) => p.warn);
      },
    );
    expect(warnFrames.length).toBeGreaterThanOrEqual(1);
  });
});

// ===========================================================================
// Tests: Connect — same-session serialization
// ===========================================================================

describe("Handler.Connect same-session serialization", () => {
  it("serializes concurrent user content frames on same session (FIFO)", async () => {
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

    stream.emit("data", userContentFrame("sess-conc", "msg-1", "test-profile"));
    stream.emit("data", userContentFrame("sess-conc", "msg-2", "test-profile"));

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

    const call = createUnaryCall({ name: "sessions/sess-bound/agent" });
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

    const call = createUnaryCall({ name: "sessions/never-connected/agent" });
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
      userContentFrame("sess-busy", "hello", "test-profile"),
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
      userContentFrame("sess-post", "go", "p"),
    );
    await flush();

    expect(stream.written.length).toBeGreaterThanOrEqual(1);
    const textFrames = stream.written.filter(
      (f) =>
        (f as Record<string, unknown>).sender === FRAME_SENDER_AGENT &&
        (f as Record<string, unknown>).payload === "messageParts",
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
    messages: Array<HumanMessage | AIMessage | SystemMessage | ToolMessage>,
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

  /** Extract the single Part's content string from a Message's PartBlock. */
  function firstPartText(msg: Record<string, unknown>): string {
    const parts = (msg.content as { parts: { text?: { content?: string } }[] }).parts;
    return parts[0]?.text?.content ?? "";
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
    expect(firstPartText(response!.messages![0])).toBe("Hello");

    expect(response!.messages![1].sender).toBe(FRAME_SENDER_AGENT);
    expect(firstPartText(response!.messages![1])).toBe("Hi there!");
  });

  it("maps AIMessage with only reasoning blocks to a ThinkingPart", async () => {
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
    const parts = (response!.messages![1].content as { parts: { thinking?: { content?: string } }[] }).parts;
    expect(parts[0]?.thinking?.content).toBe("Let me analyze...");
  });

  it("maps AIMessage with mixed reasoning + text to both parts", async () => {
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
    const parts = (response!.messages![1].content as { parts: { thinking?: { content?: string }; text?: { content?: string } }[] }).parts;
    expect(parts).toHaveLength(2);
    expect(parts[0]?.thinking?.content).toBe("Step 1");
    expect(parts[1]?.text?.content).toBe("The answer is 42.");
  });

  it("reconstructs image content blocks as an ImagePart alongside text", async () => {
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
    expect(response!.messages).toHaveLength(1);

    expect(response!.messages![0].sender).toBe(FRAME_SENDER_USER);
    const parts = (response!.messages![0].content as { parts: { text?: { content?: string }; image?: { data?: unknown; encoding?: string } }[] }).parts;
    expect(parts).toHaveLength(2);
    expect(parts[0]?.text?.content).toBe("What is in this image?");
    expect(parts[1]?.image?.data).toBe("base64imagedata");
    expect(parts[1]?.image?.encoding).toBe("IMAGE_ENCODING_PNG");
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
    expect(response!.messages).toHaveLength(1);
    const parts = (response!.messages![0].content as { parts: { image?: { data?: unknown } }[] }).parts;
    expect(parts[1]?.image?.data).toBe(expectedBase64);
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
    expect(firstPartText(response!.messages![0])).toBe("first");
    expect(firstPartText(response!.messages![1])).toBe("second");
    expect(firstPartText(response!.messages![2])).toBe("third");
    expect(firstPartText(response!.messages![3])).toBe("fourth");
  });

  it("emits a tool_call MessagePart for an AIMessage with a mouse_move tool_call", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-op-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-op-rt", [
      new HumanMessage("click the button"),
      new AIMessage({
        content: "I'll move the mouse first.",
        tool_calls: [
          {
            name: "mouse_move",
            args: { x_px: 150, y_px: 250 },
            id: "call-move-1",
            type: "tool_call" as const,
          },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-op-rt");

    expect(error).toBeNull();
    const opMsg = response!.messages!.find((m) => {
      const parts = (m.content as { parts: { toolCall?: unknown }[] } | undefined)?.parts ?? [];
      return parts.some((p) => p.toolCall);
    });
    expect(opMsg).toBeDefined();
    expect(opMsg!.sender).toBe(FRAME_SENDER_AGENT);
    const callPart = (opMsg!.content as { parts: { toolCall?: { toolId?: string; name?: string; argsJson?: string } }[] }).parts.find((p) => p.toolCall)!.toolCall;
    // tool_call carries the semantic invocation: id, name, args_json (FR-002).
    expect(callPart?.toolId).toBe("call-move-1");
    expect(callPart?.name).toBe("mouse_move");
    expect(callPart?.argsJson).toBe(JSON.stringify({ x_px: 150, y_px: 250 }));
  });

  it("emits a tool_call MessagePart for a mouse_click tool_call", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-opclick-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-opclick-rt", [
      new HumanMessage("click here"),
      new AIMessage({
        content: "",
        tool_calls: [
          {
            name: "mouse_click",
            args: { click_type: "LEFT_CLICK" },
            id: "call-click-1",
            type: "tool_call" as const,
          },
        ],
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-opclick-rt");

    expect(error).toBeNull();
    const opMsg = response!.messages!.find((m) => {
      const parts = (m.content as { parts: { toolCall?: unknown }[] } | undefined)?.parts ?? [];
      return parts.some((p) => p.toolCall);
    });
    expect(opMsg).toBeDefined();
    const callPart = (opMsg!.content as { parts: { toolCall?: { name?: string; argsJson?: string } }[] }).parts.find((p) => p.toolCall)!.toolCall;
    expect(callPart?.name).toBe("mouse_click");
    expect(callPart?.argsJson).toBe(JSON.stringify({ click_type: "LEFT_CLICK" }));
  });

  it("emits a ToolResultPart for a ToolMessage with reconstructed fields", async () => {
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-opres-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-opres-rt", [
      new HumanMessage("move the mouse"),
      new AIMessage({
        content: "",
        tool_calls: [
          {
            name: "mouse_move",
            args: { x_px: 10, y_px: 20 },
            id: "call-2",
            type: "tool_call" as const,
          },
        ],
      }),
      new ToolMessage({
        content: [
          { type: "text", text: "ok" },
          {
            type: "image_url",
            image_url: { url: "data:image/png;base64,screenshotdata" },
          },
          {
            type: "text",
            text: "[图片像素尺寸：800×600（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]",
          },
        ],
        tool_call_id: "call-2",
        // US2 carries the real status in additional_kwargs; ListMessages reads
        // it verbatim (FR-012/FR-013). Included here to exercise the carried-
        // status path.
        additional_kwargs: { toolResultStatus: "TOOL_RESULT_STATUS_SUCCEEDED" },
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-opres-rt");

    expect(error).toBeNull();

    // The ToolMessage produces a Message carrying a tool_result MessagePart.
    const resultMsg = response!.messages!.find((m) => {
      const parts = (m.content as { parts: { toolResult?: unknown }[] } | undefined)?.parts ?? [];
      return parts.some((p) => p.toolResult);
    });
    expect(resultMsg).toBeDefined();
    expect(resultMsg!.sender).toBe(FRAME_SENDER_SYSTEM);
    const tr = (resultMsg!.content as { parts: { toolResult?: { toolId?: string; status?: string; message?: string; screenshot?: { data?: string; encoding?: string; widthPx?: number; heightPx?: number } } }[] }).parts.find((p) => p.toolResult)!.toolResult;
    expect(tr?.toolId).toBe("call-2");
    expect(tr?.status).toBe("TOOL_RESULT_STATUS_SUCCEEDED");
    expect(tr?.message).toBe("ok");
    expect(tr?.screenshot).toBeDefined();
    expect(tr?.screenshot?.data).toBe("screenshotdata");
    expect(tr?.screenshot?.widthPx).toBe(800);
    expect(tr?.screenshot?.heightPx).toBe(600);
  });

  it("shows UNSPECIFIED status (not FAILED) when a ToolMessage carries no real status (FR-014/FR-015)", async () => {
    // No text inference: a ToolMessage whose message lacks "ok"/"succeeded"
    // AND whose additional_kwargs carries no toolResultStatus reconstructs to
    // UNSPECIFIED (neutral), NEVER FAILED. inferToolResultStatus is gone.
    const { adapter, graph } = createStateAdapter();
    sessionAgentStore._setBinding("sess-opfail-rt", "test-profile", adapter);
    const handler = createHandler({ promptClient, sessionAgentStore });

    await writeMessages(graph, "sess-opfail-rt", [
      new HumanMessage("do something"),
      new AIMessage({
        content: "",
        tool_calls: [
          {
            name: "mouse_click",
            args: { click_type: "RIGHT_CLICK" },
            id: "call-3",
            type: "tool_call" as const,
          },
        ],
      }),
      new ToolMessage({
        content: [{ type: "text", text: "operation timed out" }],
        tool_call_id: "call-3",
      }),
    ]);

    const { error, response } = await listMessages(handler, "sess-opfail-rt");

    expect(error).toBeNull();
    const resultMsg = response!.messages!.find((m) => {
      const parts = (m.content as { parts: { toolResult?: unknown }[] } | undefined)?.parts ?? [];
      return parts.some((p) => p.toolResult);
    });
    expect(resultMsg).toBeDefined();
    const tr = (resultMsg!.content as { parts: { toolResult?: { status?: string } }[] }).parts.find((p) => p.toolResult)!.toolResult;
    expect(tr?.status).toBe("TOOL_RESULT_STATUS_UNSPECIFIED");
  });
});
