/**
 * Driver state-machine tests for `SaoleiLoopAgent` (turn/step/abort/排队/
 * 事件面 — saolei-plugins.md §7.1): a real cordis Context carries the event
 * dispatch paths (waterfall/serial/scope carrier), a real detached
 * `Session` carries the durable log, and llm/tools/agents are injected
 * doubles provided on the context — no module interception
 * (style/javascript.md Mock convention).
 */

import { Context } from "@deepseek-ai/cordis";
import { CallId, createUserMessage } from "@deepseek-ai/dsh-llm";
import type {
  GenerateOptions,
  LlmCallConfig,
  StreamChunk,
  UserMessage,
} from "@deepseek-ai/dsh-llm";
import { Session, SessionId } from "@deepseek-ai/dsh-session";
import { TOOL_RUNTIME_SCHEDULER } from "@deepseek-ai/dsh-tools";
import type { ToolExecutionInput, ToolRunContext } from "@deepseek-ai/dsh-tools";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SaoleiLoopAgent } from "./driver.js";

const PROVIDER = "glm-responses";
const MODEL = "glm-test";

function userMessage(text: string): UserMessage {
  return createUserMessage({
    content: [{ type: "text", text }],
    source: { kind: "user" },
  });
}

/** One scripted model response: text-only (no tool calls). */
function textChunks(text: string): StreamChunk[] {
  return [
    { type: "block-start", index: 0, blockType: "text" },
    { type: "text-delta", index: 0, text },
    { type: "block-end", index: 0, block: { type: "text", text } },
    { type: "finish", reason: { kind: "stop" } },
  ];
}

/** One scripted model response requesting tool calls. */
function toolCallChunks(calls: Array<{ id: string; name: string; args: string }>): StreamChunk[] {
  const chunks: StreamChunk[] = [];
  calls.forEach((call, index) => {
    const id = CallId(call.id);
    chunks.push({ type: "block-start", index, blockType: "tool-call" });
    chunks.push({
      type: "tool-call-delta",
      index,
      id,
      name: call.name,
      argumentsDelta: call.args,
    });
    chunks.push({
      type: "block-end",
      index,
      block: { type: "tool-call", id, name: call.name, arguments: call.args },
    });
  });
  chunks.push({ type: "finish", reason: { kind: "tool-calls" } });
  return chunks;
}

/** A stream that completes with a provider-level failure finish reason. */
function failureChunks(message: string): StreamChunk[] {
  return [
    {
      type: "finish",
      reason: { kind: "error", failure: { message, code: "PROVIDER_ERROR" } },
    },
  ];
}

interface StreamHandle {
  release(): void;
  fail(reason: unknown): void;
}

interface Harness {
  ctx: Context;
  agent: SaoleiLoopAgent;
  /** Resolved per model request, in request order. */
  requests: GenerateOptions[];
  /** Script the NEXT model response; `hang` keeps the stream open until
   * released/failed (abort tests). */
  respond(script: StreamChunk[] | "hang" | "hang-empty"): StreamHandle;
  /** The tool-execution recorder: every dispatched call, in order. */
  toolCalls: Array<{ name: string; args: unknown }>;
  /** Resolve one hanging dispatched tool call (FIFO). */
  settleTool(resultText: string): void;
  /** Reject one hanging dispatched tool call (FIFO): the scheduler failure
   * surfaces as a turn error after the step's assistant/message. */
  failTool(error: unknown): void;
  statusEvents: Array<{ status: string }>;
  errorEvents: Array<{ turn: number; step: number; error: unknown }>;
  session: Session;
}

function createHarness(): Harness {
  const ctx = new Context();
  const requests: GenerateOptions[] = [];
  const toolCalls: Harness["toolCalls"] = [];
  const pendingTools: Array<(text: string) => void> = [];
  const pendingToolFailures: Array<(error: unknown) => void> = [];
  const statusEvents: Array<{ status: string }> = [];
  const errorEvents: Array<{ turn: number; step: number; error: unknown }> = [];
  /** One entry per opened model stream, in order: script provider + the
   * release/fail controls of its (potential) hang. */
  const openStreams: Array<{
    script: (script: StreamChunk[] | "hang" | "hang-empty") => void;
    release: () => void;
    fail: (reason: unknown) => void;
  }> = [];

  ctx.provide("agents", {
    withInitiator: (_agent: unknown, operation: () => unknown) => operation(),
    requireInitiator: () => harness.agent,
  });
  ctx.provide("systemPrompt", {
    assemble: async () => ({
      sections: [{ name: "deployment:persona", order: 0, text: "test persona" }],
      tools: [],
      variables: {},
    }),
  });
  ctx.provide("llm", {
    prepareCall: async (config: LlmCallConfig) => ({
      config,
      retryPolicy: { maxAttempts: 1 },
      adapterDefaults: {},
      stream: (request: GenerateOptions) => makeStream(request),
    }),
    stream: (request: GenerateOptions) => makeStream(request),
  });
  ctx.provide("tools", {
    executionMode: () => ({ kind: "exclusive" }),
    [TOOL_RUNTIME_SCHEDULER]: {
      prepare: async (input: ToolExecutionInput) => {
        toolCalls.push({ name: input.name, args: input.arguments });
        return {
          kind: "dispatch",
          exec: {
            ...input,
            rootCallId: input.callId,
            token: Symbol(),
          } as ToolRunContext,
        };
      },
      // Cooperative cancellation: an abort settles the started call with a
      // normal result (the driver drains started calls, mirrors a real
      // tool body observing exec.signal).
      dispatch: (exec: ToolRunContext) =>
        new Promise((resolve, reject) => {
          const settle = (text: string) => {
            exec.signal.removeEventListener("abort", onAbort);
            resolve({
              kind: "final-result" as const,
              result: {
                isError: false,
                value: null,
                content: [{ type: "text", text }],
              },
            });
          };
          const onAbort = () => settle("aborted-dispatch");
          if (exec.signal.aborted) {
            onAbort();
            return;
          }
          exec.signal.addEventListener("abort", onAbort, { once: true });
          pendingTools.push(settle);
          pendingToolFailures.push(reject);
        }),
      finalize: async () => {
        throw new Error("finalize not expected in this harness");
      },
      finish: (_exec: unknown, result: unknown) => result,
    },
  });

  function makeStream(request: GenerateOptions): AsyncGenerator<StreamChunk> {
    // Stream creation IS request dispatch: record it synchronously so tests
    // can wait for the Nth request before scripting its response.
    requests.push(request);
    const signal = request.signal;
    let release: () => void = () => {};
    let fail: (reason: unknown) => void = () => {};
    const hangOutcome = new Promise<never>((_, reject) => {
      release = () => reject(new Error("__release__"));
      fail = reject;
    });
    const scripted = new Promise<StreamChunk[] | "hang" | "hang-empty">((resolve) => {
      openStreams.push({ script: resolve, release, fail });
    });
    return (async function* () {
      // Wait for the test's script decision for THIS request (FIFO with
      // respond()).
      const script = await scripted;
      if (script === "hang" || script === "hang-empty") {
        if (script === "hang") {
          yield { type: "block-start", index: 0, blockType: "text" } as StreamChunk;
          yield { type: "text-delta", index: 0, text: "partial" } as StreamChunk;
        }
        // A real adapter stream honors the caller signal: abort or an
        // explicit fail rejects here; release() lets the stream finish.
        const aborted = new Promise<never>((_, rejectAbort) => {
          const onAbort = () => {
            rejectAbort(
              signal?.reason instanceof Error ? signal.reason : new Error("aborted"),
            );
          };
          if (signal?.aborted) {
            onAbort();
            return;
          }
          signal?.addEventListener("abort", onAbort, { once: true });
        });
        try {
          await Promise.race([hangOutcome, aborted]);
        } catch (reason) {
          if (reason instanceof Error && reason.message === "__release__") {
            yield { type: "finish", reason: { kind: "stop" } } as StreamChunk;
            return;
          }
          throw reason;
        }
        yield { type: "finish", reason: { kind: "stop" } } as StreamChunk;
        return;
      }
      for (const chunk of script) {
        yield chunk;
      }
      // Late abort after a scripted stream: surface it like a real adapter.
      if (signal?.aborted) {
        throw signal.reason instanceof Error ? signal.reason : new Error("aborted");
      }
    })();
  }

  const session = Session.create(SessionId("templates/saolei/sessions/t1"));
  const harness: Harness = {
    ctx,
    requests,
    toolCalls,
    statusEvents,
    errorEvents,
    session,
    respond(script) {
      const stream = openStreams.shift();
      if (stream === undefined) {
        throw new Error("no model request awaiting a script");
      }
      stream.script(script);
      return { release: stream.release, fail: stream.fail };
    },
    settleTool(resultText: string) {
      const settle = pendingTools.shift();
      if (settle === undefined) {
        throw new Error("no tool call awaiting settlement");
      }
      settle(resultText);
    },
    failTool(error: unknown) {
      const reject = pendingToolFailures.shift();
      if (reject === undefined) {
        throw new Error("no tool call awaiting failure");
      }
      reject(error);
    },
    agent: undefined as unknown as SaoleiLoopAgent,
  };
  ctx.on("agent/status", (payload: { status: string }) => {
    statusEvents.push(payload);
  });
  ctx.on("agent/error", (payload: { turn: number; step: number; error: unknown }) => {
    errorEvents.push(payload);
  });
  harness.agent = new SaoleiLoopAgent(
    ctx,
    SessionId("templates/saolei/sessions/t1"),
    { provider: PROVIDER, model: MODEL },
    session,
    { maxParallelToolCalls: 2 },
  );
  return harness;
}

/** Wait until the driver has opened its Nth model request. */
async function waitForRequests(harness: Harness, count: number): Promise<void> {
  await vi.waitFor(() => {
    expect(harness.requests.length).toBe(count);
  });
}

function eventsOf(session: Session): Array<{ type: string; data: Record<string, unknown> }> {
  return session.events.map((event) => ({
    type: event.type,
    data: event.data as Record<string, unknown>,
  }));
}

describe("SaoleiLoopAgent", () => {
  let harness: Harness;

  beforeEach(() => {
    harness = createHarness();
  });

  afterEach(async () => {
    harness.agent.cancel({ kind: "disposed" });
    await harness.agent.whenIdle().catch(() => {});
    await harness.agent.scope.dispose();
  });

  it("followup drives one turn to completion and publishes the event surface", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("开始一局扫雷"));
    await waitForRequests(harness, 1);
    harness.respond(textChunks("好的，我来开新局。"));

    await agent.whenIdle();

    expect(agent.status).toBe("idle");
    const events = eventsOf(session);
    const types = events.map((event) => event.type);
    expect(types).toEqual([
      "agent/inbox/spliced",
      "turn/start",
      "agent/inbox/spliced",
      "step/start",
      "user/message",
      "request/header",
      "request/context",
      "assistant/chunk",
      "assistant/chunk",
      "assistant/chunk",
      "assistant/chunk",
      "assistant/message",
      "step/end",
      "turn/end",
    ]);
    const turnEnd = events.at(-1)!.data as { turn: number; reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("completed");
    // Status surface: exactly one running→idle cycle for the turn.
    expect(harness.statusEvents.map((event) => event.status)).toEqual(["running", "idle"]);
    // Derived history: user prompt + assistant reply.
    const history = session.deriveMessages();
    expect(history).toHaveLength(2);
    expect(history[0]?.content[0]).toMatchObject({ text: "开始一局扫雷" });
    expect(history[1]?.content[0]).toMatchObject({ text: "好的，我来开新局。" });
  });

  it("executes tool calls through the scheduler and continues to a closing step", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("开新局"));
    await waitForRequests(harness, 1);
    harness.respond(
      toolCallChunks([{ id: "call-1", name: "saolei_init", args: "{}" }]),
    );
    await vi.waitFor(() => expect(harness.toolCalls).toHaveLength(1));
    harness.settleTool("new game started");
    await waitForRequests(harness, 2);
    harness.respond(textChunks("已开局"));

    await agent.whenIdle();

    const events = eventsOf(session);
    const toolCall = events.find((event) => event.type === "tool/call");
    const toolResult = events.find((event) => event.type === "tool/result");
    expect(toolCall?.data).toMatchObject({ name: "saolei_init", callId: "call-1" });
    // The result event cites its call's seq (model-ordered correlation); the
    // ToolResultMessage wraps the rendered content in its tool-result block.
    expect(toolResult?.data).toMatchObject({ turn: 1, step: 1 });
    expect(toolResult?.data.message).toMatchObject({
      content: [
        {
          type: "tool-result",
          toolCallId: "call-1",
          isError: false,
          content: [{ type: "text", text: "new game started" }],
        },
      ],
    });
    const turnEnd = events.at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("completed");
  });

  it("queues a followup behind a running turn and runs it automatically", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("第一句"));
    await waitForRequests(harness, 1);
    const handle = harness.respond("hang");

    // Arrives while turn 1 is running: queued, no second request yet.
    agent.followup(userMessage("第二句"));
    await vi.waitFor(() => {
      expect(agent.inbox.hasPending).toBe(true);
    });
    expect(harness.requests).toHaveLength(1);

    // Release turn 1; the driver must open turn 2 without another wake.
    handle.release();
    await waitForRequests(harness, 2);
    harness.respond(textChunks("回复二"));
    await agent.whenIdle();

    const turns = eventsOf(session).filter((event) => event.type === "turn/start");
    expect(turns).toHaveLength(2);
    const userTexts = session
      .deriveMessages()
      .filter((message) => message.role === "user")
      .map((message) => (message.content[0] as { text: string }).text);
    expect(userTexts).toEqual(["第一句", "第二句"]);
    expect(agent.status).toBe("idle");
  });

  it("aborts mid-stream: interrupted prefix lands in the log, turn closes aborted", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("长回复"));
    await waitForRequests(harness, 1);
    harness.respond("hang");
    await vi.waitFor(() => {
      // Let the driver consume the two scripted prefix chunks first.
      expect(
        session.events.filter((event) => event.type === "assistant/chunk"),
      ).toHaveLength(2);
    });

    agent.cancel({ kind: "user" });
    await agent.whenIdle();

    const events = eventsOf(session);
    const interrupted = events.find(
      (event) =>
        event.type === "assistant/message" && (event.data as { interrupted?: boolean }).interrupted === true,
    );
    expect(interrupted).toBeDefined();
    expect((interrupted!.data.message as { content: unknown[] }).content[0]).toMatchObject({
      text: "partial",
    });
    const turnEnd = events.at(-1)!.data as {
      reason: { kind: string; reason: { kind: string } };
    };
    expect(turnEnd.reason.kind).toBe("aborted");
    expect(turnEnd.reason.reason).toMatchObject({ kind: "user" });
    expect(agent.status).toBe("idle");
    // Aborts are lifecycle, not failures: no agent/error emission.
    expect(harness.errorEvents).toHaveLength(0);
  });

  it("redirects a wake sent during an aborted turn to the next turn (wakingAfterAbort)", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("第一条"));
    await waitForRequests(harness, 1);
    harness.respond("hang");
    await vi.waitFor(() => {
      expect(session.events.some((event) => event.type === "assistant/chunk")).toBe(true);
    });

    // Abort, then wake while the aborted turn has not converged yet.
    agent.cancel({ kind: "user" });
    agent.followup(userMessage("第二条"));

    // The signal-aware fake stream rejects on the abort: the aborted turn
    // closes and the redirected wake (no explicit release) opens turn 2;
    // script its response to let it complete.
    await waitForRequests(harness, 2);
    harness.respond(textChunks("第二条回复"));
    await agent.whenIdle();

    const turns = eventsOf(session).filter((event) => event.type === "turn/start");
    expect(turns.length).toBe(2);
    const userTexts = session
      .deriveMessages()
      .filter((message) => message.role === "user")
      .map((message) => (message.content[0] as { text: string }).text);
    expect(userTexts).toContain("第二条");
    expect(agent.status).toBe("idle");
  });

  it("abort during tool dispatch synthesizes results for unstarted calls (tool_call 必有 tool_result)", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("连续操作"));
    await waitForRequests(harness, 1);
    harness.respond(
      toolCallChunks([
        { id: "call-1", name: "saolei_operate", args: '{"type":"click","x":1,"y":1}' },
        { id: "call-2", name: "saolei_operate", args: '{"type":"click","x":2,"y":2}' },
      ]),
    );
    await vi.waitFor(() => expect(harness.toolCalls).toHaveLength(1));

    agent.cancel({ kind: "user" });
    await agent.whenIdle();

    const events = eventsOf(session);
    const results = events.filter((event) => event.type === "tool/result");
    expect(results).toHaveLength(2);
    const synthetic = results[1]!.data.message as {
      content: Array<{ content: Array<{ text: string }> }>;
    };
    expect(synthetic.content[0]?.content[0]?.text).toBe(
      "Error: tool call aborted before dispatch",
    );
    const turnEnd = events.at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("aborted");
  });

  it("emits agent/error and closes the turn with error on a model failure, then stays usable", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("会失败的话"));
    await waitForRequests(harness, 1);
    const handle = harness.respond("hang");
    handle.fail(new Error("fake llm unreachable"));

    await agent.whenIdle();

    expect(harness.errorEvents).toHaveLength(1);
    expect(harness.errorEvents[0]?.error).toBeInstanceOf(Error);
    const turnEnd = eventsOf(session).at(-1)!.data as {
      reason: { kind: string; error: { message: string; code: string } };
    };
    expect(turnEnd.reason.kind).toBe("error");
    expect(turnEnd.reason.error).toMatchObject({ code: "UNKNOWN" });
    expect(agent.status).toBe("idle");

    // Driver containment: the next turn works.
    agent.followup(userMessage("再来一次"));
    await waitForRequests(harness, 2);
    harness.respond(textChunks("成功了"));
    await agent.whenIdle();
    const turns = eventsOf(session).filter((event) => event.type === "turn/start");
    expect(turns).toHaveLength(2);
  });

  it("agent/request-error waterfall retry recovers a failed attempt", async () => {
    const { agent, session } = harness;
    let retries = 0;
    harness.ctx.on(
      "agent/request-error",
      (_payload: unknown, next: () => Promise<undefined>) => {
        retries += 1;
        return Promise.resolve({ kind: "retry" as const }) as unknown as Promise<undefined>;
      },
    );
    agent.followup(userMessage("重试一次"));
    await waitForRequests(harness, 1);
    // A completed stream carrying an error finish reason is the
    // request-error path (a thrown stream is terminal, see the error test).
    harness.respond(failureChunks("transient"));

    await vi.waitFor(() => expect(harness.requests.length).toBeGreaterThanOrEqual(2));
    harness.respond(textChunks("重试成功"));
    await agent.whenIdle();

    expect(retries).toBeGreaterThanOrEqual(1);
    const turnEnd = eventsOf(session).at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("completed");
  });

  it("fixates the partial stream prefix when the stream throws without abort (data-model.md §2)", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("会失败的话"));
    await waitForRequests(harness, 1);
    const handle = harness.respond("hang");
    await vi.waitFor(() => {
      // Let the driver consume the two scripted prefix chunks first.
      expect(
        session.events.filter((event) => event.type === "assistant/chunk"),
      ).toHaveLength(2);
    });
    handle.fail(new Error("fake llm unreachable"));

    await agent.whenIdle();

    // Same fixation shape as the abort path: one `assistant/message` with
    // `interrupted: true` holding the produced prefix only.
    const events = eventsOf(session);
    const interrupted = events.find(
      (event) =>
        event.type === "assistant/message" && (event.data as { interrupted?: boolean }).interrupted === true,
    );
    expect(interrupted).toBeDefined();
    expect((interrupted!.data as { turn: number; step: number }).turn).toBe(1);
    expect((interrupted!.data.message as { content: unknown[] }).content[0]).toMatchObject({
      text: "partial",
    });
    expect((interrupted!.data as { usage?: unknown }).usage).toBeUndefined();
    const turnEnd = events.at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("error");
  });

  it("fixates produced blocks before a failure finish surfaces as LlmError", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("provider 失败"));
    await waitForRequests(harness, 1);
    // A completed stream carrying content plus an error finish reason: the
    // produced text block must land before the turn closes error.
    harness.respond([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "半截回复" },
      {
        type: "block-end",
        index: 0,
        block: { type: "text", text: "半截回复" },
      },
      { type: "usage", usage: { inputTokens: 3, outputTokens: 5 } },
      {
        type: "finish",
        reason: { kind: "error", failure: { message: "upstream exploded", code: "PROVIDER_ERROR" } },
      },
    ]);

    await agent.whenIdle();

    const events = eventsOf(session);
    const interrupted = events.find(
      (event) =>
        event.type === "assistant/message" && (event.data as { interrupted?: boolean }).interrupted === true,
    );
    expect(interrupted).toBeDefined();
    expect((interrupted!.data.message as { content: unknown[] }).content[0]).toMatchObject({
      text: "半截回复",
    });
    // Usage rides along when the stream reported it (same conditional as the
    // normal append).
    expect((interrupted!.data as { usage?: unknown }).usage).toMatchObject({
      inputTokens: 3,
      outputTokens: 5,
    });
    expect(harness.errorEvents).toHaveLength(1);
    const turnEnd = events.at(-1)!.data as {
      reason: { kind: string; error: { message: string; code: string } };
    };
    expect(turnEnd.reason.kind).toBe("error");
    expect(turnEnd.reason.error).toMatchObject({ code: "PROVIDER_ERROR", message: "upstream exploded" });
  });

  it("fixates nothing when the failed stream produced no content", async () => {
    const { agent, session } = harness;
    // Empty assembler on the thrown-stream path.
    agent.followup(userMessage("立即失败"));
    await waitForRequests(harness, 1);
    const handle = harness.respond("hang-empty");
    handle.fail(new Error("immediate failure"));
    await agent.whenIdle();
    expect(eventsOf(session).some((event) => event.type === "assistant/message")).toBe(false);

    // Empty assembler on the failure-finish path.
    agent.followup(userMessage("再来一次"));
    await waitForRequests(harness, 2);
    harness.respond(failureChunks("terminal"));
    await agent.whenIdle();
    const messages = eventsOf(session).filter((event) => event.type === "assistant/message");
    expect(messages).toHaveLength(0);
    const turnEnd = eventsOf(session).at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("error");
    expect(agent.status).toBe("idle");
  });

  it("request-error retries skip fixation: the failed attempt leaves no assistant/message", async () => {
    const { agent, session } = harness;
    harness.ctx.on(
      "agent/request-error",
      (_payload: unknown, next: () => Promise<undefined>) =>
        Promise.resolve({ kind: "retry" as const }) as unknown as Promise<undefined>,
    );
    agent.followup(userMessage("重试一次"));
    await waitForRequests(harness, 1);
    harness.respond([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "将被丢弃的" },
      {
        type: "finish",
        reason: { kind: "error", failure: { message: "transient", code: "PROVIDER_ERROR" } },
      },
    ]);
    await vi.waitFor(() => expect(harness.requests.length).toBeGreaterThanOrEqual(2));
    harness.respond(textChunks("重试成功"));

    await agent.whenIdle();

    // Fixation happens only before the terminal error; a retried attempt
    // re-assembles from scratch and the successful attempt appends normally.
    const messages = eventsOf(session).filter((event) => event.type === "assistant/message");
    expect(messages).toHaveLength(1);
    expect((messages[0]!.data as { interrupted?: boolean }).interrupted).toBeUndefined();
    expect((messages[0]!.data.message as { content: unknown[] }).content[0]).toMatchObject({
      text: "重试成功",
    });
    const turnEnd = eventsOf(session).at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("completed");
  });

  it("tool dispatch failure keeps the step's assistant/message already appended (append precedes executeToolCalls)", async () => {
    const { agent, session } = harness;
    agent.followup(userMessage("工具会炸"));
    await waitForRequests(harness, 1);
    harness.respond(
      toolCallChunks([{ id: "call-1", name: "saolei_init", args: "{}" }]),
    );
    await vi.waitFor(() => expect(harness.toolCalls).toHaveLength(1));
    harness.failTool(new Error("scheduler boom"));

    await agent.whenIdle();

    // The normal append lands before executeToolCalls opens, so the step's
    // produced blocks are already in the log when the failure bubbles
    // (research D5: 工具执行异常路径零改动，顺序断言)。
    const events = eventsOf(session);
    const messageIdx = events.findIndex((event) => event.type === "assistant/message");
    const callIdx = events.findIndex((event) => event.type === "tool/call");
    expect(messageIdx).toBeGreaterThanOrEqual(0);
    expect(callIdx).toBeGreaterThan(messageIdx);
    const message = events[messageIdx]!;
    expect((message.data as { interrupted?: boolean }).interrupted).toBeUndefined();
    expect((message.data.message as { content: unknown[] }).content[0]).toMatchObject({
      type: "tool-call",
      id: "call-1",
      name: "saolei_init",
    });
    const turnEnd = events.at(-1)!.data as {
      reason: { kind: string; error: { message: string; code: string } };
    };
    expect(turnEnd.reason.kind).toBe("error");
    expect(turnEnd.reason.error).toMatchObject({ code: "UNKNOWN" });
  });

  it("fixates the produced prefix when abort lands during the request-error waterfall", async () => {
    const { agent, session } = harness;
    let enterWaterfall: (() => void) | undefined;
    const entered = new Promise<void>((resolve) => {
      enterWaterfall = resolve;
    });
    let releaseWaterfall: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      releaseWaterfall = resolve;
    });
    harness.ctx.on("agent/request-error", (_payload: unknown, _next: () => Promise<undefined>) => {
      enterWaterfall!();
      return gate.then(() => undefined) as unknown as Promise<undefined>;
    });

    agent.followup(userMessage("waterfall 中止"));
    await waitForRequests(harness, 1);
    harness.respond([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "waterfall 前缀" },
      {
        type: "finish",
        reason: { kind: "error", failure: { message: "boom", code: "PROVIDER_ERROR" } },
      },
    ]);

    // Park the driver inside the waterfall listener, then cancel while it
    // waits — the window where the abort used to skip fixation (revision
    // §7-2).
    await entered;
    agent.cancel({ kind: "user" });
    releaseWaterfall!();

    await agent.whenIdle();

    const events = eventsOf(session);
    const interrupted = events.find(
      (event) =>
        event.type === "assistant/message" && (event.data as { interrupted?: boolean }).interrupted === true,
    );
    expect(interrupted).toBeDefined();
    expect((interrupted!.data.message as { content: unknown[] }).content[0]).toMatchObject({
      text: "waterfall 前缀",
    });
    // The abort closes the turn aborted (lifecycle, not a failure).
    const turnEnd = events.at(-1)!.data as {
      reason: { kind: string; reason?: { kind: string } };
    };
    expect(turnEnd.reason.kind).toBe("aborted");
    expect(turnEnd.reason.reason).toMatchObject({ kind: "user" });
    expect(harness.errorEvents).toHaveLength(0);
    expect(agent.status).toBe("idle");
  });

  it("agent/pre-step reject closes the turn blocked", async () => {
    const { agent, session } = harness;
    harness.ctx.on("agent/pre-step", () => {
      return Promise.resolve({ kind: "reject" as const });
    });
    agent.followup(userMessage("被拒绝"));
    await agent.whenIdle();

    const events = eventsOf(session);
    const turnEnd = events.at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("blocked");
    // A rejected step never logs user/message or opens a step.
    expect(events.some((event) => event.type === "user/message")).toBe(false);
    expect(events.some((event) => event.type === "step/start")).toBe(false);
  });

  it("agent/turn-stopping listener that steers keeps the turn open for another step", async () => {
    const { agent, session } = harness;
    let steered = false;
    harness.ctx.on("agent/turn-stopping", () => {
      if (!steered) {
        steered = true;
        harness.agent.steer(userMessage("补充一句"));
      }
    });
    agent.followup(userMessage("第一句"));
    await waitForRequests(harness, 1);
    harness.respond(textChunks("回复一"));
    await waitForRequests(harness, 2);
    harness.respond(textChunks("回复二"));
    await agent.whenIdle();

    expect(steered).toBe(true);
    const steps = eventsOf(session).filter((event) => event.type === "step/start");
    expect(steps).toHaveLength(2);
    const turnEnd = eventsOf(session).at(-1)!.data as { reason: { kind: string } };
    expect(turnEnd.reason.kind).toBe("completed");
    const turnEnds = eventsOf(session).filter((event) => event.type === "turn/end");
    expect(turnEnds).toHaveLength(1);
  });
});
