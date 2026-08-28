import { afterEach, describe, expect, it, vi } from "vitest";

import { LlmError } from "@deepseek-ai/dsh-llm";
import type { GenerateOptions, StreamChunk } from "@deepseek-ai/dsh-llm";

import { GlmResponsesAdapter } from "./adapter.js";

const API_KEY_ENV = "GLM_ADAPTER_TEST_API_KEY";

function baseOptions(overrides: Partial<GenerateOptions>): GenerateOptions {
  return {
    provider: "glm-responses",
    model: "glm-5.2",
    messages: [
      {
        id: "m1" as GenerateOptions["messages"][number]["id"],
        role: "user",
        content: [{ type: "text", text: "hello" }],
        source: { kind: "user" },
      },
    ],
    ...overrides,
  };
}

function testConfig() {
  return {
    apiKeyEnv: API_KEY_ENV,
    baseURL: "https://glm.test/api/v1",
    models: [{ id: "glm-5.2", contextWindow: 1000000 }],
  };
}

// sseResponse builds a real Response whose body streams the given SSE
// frames and then closes; the stream only errors when `failSignal` fires
// (used to simulate a mid-stream transport abort instead of a clean end).
function sseResponse(frames: string[], failSignal?: AbortSignal): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) {
        controller.enqueue(encoder.encode(f));
      }
      if (failSignal === undefined) {
        controller.close();
        return;
      }
      failSignal.addEventListener("abort", () => {
        controller.error(new DOMException("The operation was aborted.", "AbortError"));
      });
    },
  });
  return new Response(stream, { status: 200 });
}

function frame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

function textTurnFrames(): string[] {
  return [
    frame("response.output_item.added", {
      type: "response.output_item.added",
      output_index: 0,
      item: { type: "message", role: "assistant" },
    }),
    frame("response.output_text.delta", {
      type: "response.output_text.delta",
      item_id: "msg_1",
      output_index: 0,
      delta: "He",
    }),
    frame("response.output_text.delta", {
      type: "response.output_text.delta",
      item_id: "msg_1",
      output_index: 0,
      delta: "y",
    }),
    frame("response.output_item.done", {
      type: "response.output_item.done",
      output_index: 0,
      item: { type: "message", role: "assistant", content: [{ type: "output_text", text: "Hey" }] },
    }),
    frame("response.completed", {
      type: "response.completed",
      response: {
        status: "completed",
        usage: { input_tokens: 3, output_tokens: 1, output_tokens_details: { reasoning_tokens: 0 } },
      },
    }),
  ];
}

async function collect(chunks: AsyncIterable<StreamChunk>): Promise<StreamChunk[]> {
  const out: StreamChunk[] = [];
  for await (const chunk of chunks) {
    out.push(chunk);
  }
  return out;
}

describe("GlmResponsesAdapter", () => {
  afterEach(() => {
    delete process.env[API_KEY_ENV];
    vi.restoreAllMocks();
  });

  it("streams the SSE wire through the adapter with auth and attribution headers", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) =>
      sseResponse(textTurnFrames(), init?.signal instanceof AbortSignal ? init.signal : undefined),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({ system: "be nice" })));

    // Positive assertion that the injected transport was actually exercised
    // (style/javascript.md: every intercepted call must be asserted).
    expect(fetchImpl).toHaveBeenCalledOnce();
    const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://glm.test/api/v1/responses");
    expect(init.method).toBe("POST");
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer test-key-value");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers["Accept"]).toBe("text/event-stream");
    // attributionHeaders() must reach the wire (user-agent identity).
    expect(headers["user-agent"]).toBeTruthy();

    expect(JSON.parse(init.body as string)).toEqual({
      model: "glm-5.2",
      instructions: "be nice",
      input: [
        { type: "message", role: "user", content: [{ type: "input_text", text: "hello" }] },
      ],
      stream: true,
    });

    // usage precedes finish; finish is last; deltas of one block reuse the
    // same index; the assembled block-end matches the delta concatenation.
    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "He" },
      { type: "text-delta", index: 0, text: "y" },
      { type: "block-end", index: 0, block: { type: "text", text: "Hey" } },
      { type: "usage", usage: { inputTokens: 3, outputTokens: 1, reasoningTokens: 0 } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("emits nothing after finish even when the provider keeps streaming", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const frames = [
      ...textTurnFrames(),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "after finish",
      }),
      frame("response.completed", { type: "response.completed", response: { status: "completed" } }),
    ];
    const fetchImpl = vi.fn(async () => sseResponse(frames));
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({})));

    const finishes = chunks.filter((c) => c.type === "finish");
    expect(finishes).toHaveLength(1);
    expect(chunks.at(-1)).toEqual({ type: "finish", reason: { kind: "stop" } });
    expect(chunks.some((c) => c.type === "text-delta" && (c as { text: string }).text === "after finish")).toBe(false);
  });

  it("throws a stable LlmError on HTTP failures without leaking the key", async () => {
    process.env[API_KEY_ENV] = "secret-key-no-leak";
    const fetchImpl = vi.fn(async () => new Response("upstream exploded", { status: 500 }));
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
      code: "GLM_HTTP_500",
    });
    await expect(collect(adapter.stream(baseOptions({})))).rejects.toSatisfy((error: LlmError) => {
      expect(error.message).not.toContain("secret-key-no-leak");
      return true;
    });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("ends with an aborted finish when the signal fires mid-stream", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const controller = new AbortController();
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, init?: RequestInit) =>
        sseResponse(textTurnFrames(), init?.signal instanceof AbortSignal ? init.signal : undefined),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const iterator = adapter.stream(baseOptions({ signal: controller.signal }))[Symbol.asyncIterator]();

    // Abort once the first chunk has been observed, so the abort lands
    // mid-body rather than during connect.
    const first = await iterator.next();
    expect(first.value?.type).toBe("block-start");
    controller.abort();

    const chunks: StreamChunk[] = [first.value as StreamChunk];
    for (;;) {
      const { done, value } = await iterator.next();
      if (done) {
        break;
      }
      chunks.push(value as StreamChunk);
    }

    expect(chunks.at(-1)?.type).toBe("finish");
    expect(chunks.at(-1)).toMatchObject({ reason: { kind: "aborted" } });
    expect(fetchImpl).toHaveBeenCalledOnce();
  });

  it("throws when the API key env is empty instead of sending the request", async () => {
    process.env[API_KEY_ENV] = "";
    const fetchImpl = vi.fn();
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
      code: "INVALID_CREDENTIAL",
    });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("resolves catalog models with their context window and unknown models minimally", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const fetchImpl = vi.fn(async () => sseResponse(textTurnFrames()));
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    expect(await adapter.resolveModel("glm-responses", "glm-5.2")).toEqual({
      provider: "glm-responses",
      id: "glm-5.2",
      name: "glm-5.2",
      context: { contextWindow: 1000000 },
    });
    expect(await adapter.resolveModel("glm-responses", "glm-unknown")).toEqual({
      provider: "glm-responses",
      id: "glm-unknown",
      name: "glm-unknown",
    });
    expect(adapter.providerInfo("glm-responses")).toEqual({
      id: "glm-responses",
      name: "GLM (OpenAI Responses)",
    });
  });
});
