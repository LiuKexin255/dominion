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
// frames and then closes. With `stallSignal` the body stays open after the
// frames and errors when the signal aborts — the mid-stream transport
// abort fixture (without it the test would consume the frames to a clean
// end before the abort lands).
function sseResponse(frames: string[], stallSignal?: AbortSignal): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) {
        controller.enqueue(encoder.encode(f));
      }
      if (stallSignal === undefined) {
        controller.close();
        return;
      }
      stallSignal.addEventListener("abort", () => {
        controller.error(new DOMException("The operation was aborted.", "AbortError"));
      });
    },
  });
  return new Response(stream, { status: 200 });
}

function frame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

// errorResponse builds a non-2xx Response with an optional raw error body
// and headers (Retry-After), the shape the failure-classification tests
// inject through fetchImpl.
function errorResponse(
  status: number,
  body?: string,
  headers?: Record<string, string>,
): Response {
  return new Response(body ?? null, { status, headers });
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
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
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
      code: "SERVER",
    });
    await expect(collect(adapter.stream(baseOptions({})))).rejects.toSatisfy((error: LlmError) => {
      expect(error.message).toBe("GLM endpoint returned HTTP 500");
      expect(error.message).not.toContain("secret-key-no-leak");
      // The raw body is chained as cause, never echoed in the message
      // (llm-failure-taxonomy.md §1 义务 1).
      expect(String(error.cause)).toContain("upstream exploded");
      return true;
    });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("maps HTTP status and provider error detail onto the shared failure taxonomy", async () => {
    const cases: Array<{
      status: number;
      body?: string;
      headers?: Record<string, string>;
      code: string;
    }> = [
      { status: 401, code: "AUTH" },
      { status: 403, code: "AUTH" },
      {
        status: 429,
        body: JSON.stringify({ error: { message: "rate limit exceeded" } }),
        code: "RATE_LIMIT",
      },
      {
        status: 429,
        body: JSON.stringify({ error: { message: "insufficient quota" } }),
        code: "QUOTA",
      },
      {
        status: 400,
        body: JSON.stringify({ error: { message: "context length exceeded" } }),
        code: "CONTEXT_WINDOW_EXCEEDED",
      },
      {
        status: 400,
        body: JSON.stringify({ error: { message: "bad tool schema" } }),
        code: "INVALID_REQUEST",
      },
      { status: 400, code: "INVALID_REQUEST" },
      // Quota wording wins over the status mapping on any status.
      {
        status: 400,
        body: JSON.stringify({ error: { message: "out of credits" } }),
        code: "QUOTA",
      },
      { status: 500, code: "SERVER" },
      {
        status: 503,
        body: JSON.stringify({ error: { message: "upstream exploded" } }),
        code: "SERVER",
      },
      { status: 404, code: "HTTP_404" },
      {
        status: 422,
        body: JSON.stringify({ error: { type: "unprocessable" } }),
        code: "HTTP_422",
      },
    ];

    for (const testCase of cases) {
      const fetchImpl = vi.fn(async () =>
        errorResponse(testCase.status, testCase.body, testCase.headers),
      );
      const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

      await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
        code: testCase.code,
      });
      expect(fetchImpl).toHaveBeenCalledOnce();
    }
  });

  it("classifies quota wording from the error body without echoing the body", async () => {
    process.env[API_KEY_ENV] = "secret-key-no-leak";
    const rawBody = JSON.stringify({
      error: {
        code: "insufficient_quota",
        message: "insufficient quota for this billing window",
      },
    });
    const fetchImpl = vi.fn(async () => errorResponse(429, rawBody));
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("QUOTA");
    expect(error.message).toBe("GLM endpoint returned HTTP 429");
    expect(error.message).not.toContain("insufficient quota");
    expect(error.message).not.toContain("secret-key-no-leak");
    expect(String(error.cause)).toContain("insufficient quota");
    expect(error.failure.status).toBe(429);
  });

  it("parses Retry-After seconds and HTTP dates into providerRetryAfterMs", async () => {
    async function httpFailure(headers: Record<string, string>): Promise<LlmError> {
      const fetchImpl = vi.fn(async () => errorResponse(429, undefined, headers));
      const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);
      return (await collect(adapter.stream(baseOptions({}))).catch(
        (caught: unknown) => caught,
      )) as LlmError;
    }

    const seconds = await httpFailure({ "Retry-After": "2" });
    expect(seconds.code).toBe("RATE_LIMIT");
    expect(seconds.failure.providerRetryAfterMs).toBe(2000);

    const date = await httpFailure({
      "Retry-After": new Date(Date.now() + 5000).toUTCString(),
    });
    expect(date.failure.providerRetryAfterMs).toBeGreaterThan(0);
    expect(date.failure.providerRetryAfterMs).toBeLessThanOrEqual(5000);

    const invalid = await httpFailure({ "Retry-After": "soon" });
    expect(invalid.code).toBe("RATE_LIMIT");
    expect(invalid.failure.providerRetryAfterMs).toBeUndefined();

    const absent = await httpFailure({});
    expect(absent.failure.providerRetryAfterMs).toBeUndefined();
  });

  it("maps fetch failures to TRANSPORT with the original error as cause", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("TRANSPORT");
    expect(error.message).toContain("fetch failed");
    expect(error.cause).toBeInstanceOf(TypeError);
    expect(fetchImpl).toHaveBeenCalledOnce();
  });

  it("maps non-abort body read failures to TRANSPORT", async () => {
    const fetchImpl = vi.fn(async () => {
      const encoder = new TextEncoder();
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(
            encoder.encode(
              frame("response.output_item.added", {
                type: "response.output_item.added",
                output_index: 0,
                item: { type: "message", role: "assistant" },
              }),
            ),
          );
          controller.error(new Error("socket reset"));
        },
      });
      return new Response(stream, { status: 200 });
    });
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("TRANSPORT");
    expect(error.message).toContain("socket reset");
    expect(error.cause).toBeInstanceOf(Error);
  });

  it("maps a successful response without a body to EMPTY_RESPONSE", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 200 }));
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
      code: "EMPTY_RESPONSE",
    });
  });

  it("maps a stream ending without a terminal event to STREAM_CLOSED", async () => {
    const fetchImpl = vi.fn(async () =>
      sseResponse([
        frame("response.output_item.added", {
          type: "response.output_item.added",
          output_index: 0,
          item: { type: "message", role: "assistant" },
        }),
        frame("response.output_text.delta", {
          type: "response.output_text.delta",
          output_index: 0,
          delta: "partial",
        }),
      ]),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("STREAM_CLOSED");
    expect(error.message).toContain("terminal event");
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

  it("sends the request without an Authorization header when the key env is empty", async () => {
    // Conditional Authorization (glm-llm-plugin.md §3 义务 6, §6 测试义务 4):
    // the host tolerates a missing secret (research.md D9), so the request
    // goes out headerless and the stream is consumed normally.
    process.env[API_KEY_ENV] = "";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({})));

    expect(fetchImpl).toHaveBeenCalledOnce();
    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers).not.toHaveProperty("Authorization");
    // attribution headers ride every request regardless of the key.
    expect(headers["user-agent"]).toBeTruthy();
    expect(chunks.at(-1)?.type).toBe("finish");
  });

  it("sends the request without an Authorization header when the key env is blank", async () => {
    process.env[API_KEY_ENV] = "   ";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({})));

    expect(fetchImpl).toHaveBeenCalledOnce();
    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers).not.toHaveProperty("Authorization");
    expect(headers["user-agent"]).toBeTruthy();
    expect(chunks.at(-1)?.type).toBe("finish");
  });

  it("advertises the static config.models catalog without an endpoint call", async () => {
    // D4 (specs/051-agent-v2-dsh-migration/research.md): the catalog is
    // static — projected from config.models with no network I/O — and is
    // the same source UpdateAgent's model validation consults.
    const fetchImpl = vi.fn(async () => sseResponse(textTurnFrames()));
    const adapter = new GlmResponsesAdapter(
      {
        apiKeyEnv: API_KEY_ENV,
        baseURL: "https://glm.test/api/v1",
        models: [
          { id: "glm-5.2", contextWindow: 1000000 },
          { id: "glm-5-turbo", contextWindow: 128000 },
        ],
      },
      fetchImpl as unknown as typeof fetch,
    );

    const models = await adapter.listModels("glm-responses");

    expect(models).toEqual([
      { provider: "glm-responses", id: "glm-5.2", name: "glm-5.2" },
      { provider: "glm-responses", id: "glm-5-turbo", name: "glm-5-turbo" },
    ]);
    // Positive assertion that the static catalog truly bypasses the
    // transport (style/javascript.md: every intercepted call must be
    // asserted — here, asserted to NOT happen).
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

  it("resolves the configured retry policy and falls back to the dsh default", () => {
    const defaultAdapter = new GlmResponsesAdapter(testConfig());
    expect(defaultAdapter.providerRetryPolicy("glm-responses")).toEqual({
      mode: "normal",
      maxRetries: 5,
      retryableCodes: ["EMPTY_RESPONSE", "RATE_LIMIT", "SERVER", "TIMEOUT", "TRANSPORT"],
      initialDelayMs: 500,
      maxDelayMs: 10000,
      jitterRatio: 0.1,
    });

    const configured = new GlmResponsesAdapter({
      ...testConfig(),
      retryPolicy: { mode: "normal", maxRetries: 2, retryableCodes: ["SERVER"], backoff: { initialDelayMs: 250 } },
    });
    expect(configured.providerRetryPolicy("glm-responses")).toEqual({
      mode: "normal",
      maxRetries: 2,
      retryableCodes: ["SERVER"],
      initialDelayMs: 250,
      maxDelayMs: 10000,
      jitterRatio: 0.1,
    });
  });

  it("aborts the transport and cancels the body when the consumer stops early", async () => {
    let cancelled = false;
    const encoder = new TextEncoder();
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            frame("response.output_item.added", {
              type: "response.output_item.added",
              output_index: 0,
              item: { type: "message", role: "assistant" },
            }),
          ),
        );
      },
      cancel() {
        cancelled = true;
      },
    });
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) =>
        new Response(stream, { status: 200 }),
    );
    const adapter = new GlmResponsesAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const iterator = adapter.stream(baseOptions({}))[Symbol.asyncIterator]();
    const first = await iterator.next();
    expect(first.value?.type).toBe("block-start");
    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    expect(init.signal?.aborted).toBe(false);

    await iterator.return?.(undefined);

    expect(init.signal?.aborted).toBe(true);
    expect(cancelled).toBe(true);
  });

  it("turns a stalled stream into a TIMEOUT LlmError via the idle watchdog", async () => {
    vi.useFakeTimers();
    try {
      const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) =>
        stalledResponse(init?.signal instanceof AbortSignal ? init.signal : undefined),
      );
      const adapter = new GlmResponsesAdapter(
        { ...testConfig(), streamIdleTimeoutMs: 1000 },
        fetchImpl as unknown as typeof fetch,
      );

      const iterator = adapter.stream(baseOptions({}))[Symbol.asyncIterator]();
      const pending = iterator.next();
      // Let the fetch settle and the first body read go outstanding.
      await vi.advanceTimersByTimeAsync(0);
      const rejection = expect(pending).rejects.toMatchObject({ code: "TIMEOUT" });

      await vi.advanceTimersByTimeAsync(1000);

      await rejection;
      expect(fetchImpl).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("resets the idle watchdog on SSE comment frames", async () => {
    vi.useFakeTimers();
    try {
      const encoder = new TextEncoder();
      let controller!: ReadableStreamDefaultController<Uint8Array>;
      // The body stays open and errors when the watchdog signal aborts, so
      // a timeout reaches the pending read like a real aborted fetch.
      const fetchImpl = vi.fn(
        async (_url: string | URL | Request, init?: RequestInit) => {
          const signal = init?.signal instanceof AbortSignal ? init.signal : undefined;
          const stream = new ReadableStream<Uint8Array>({
            start(c) {
              controller = c;
              signal?.addEventListener("abort", () => c.error(signal.reason));
            },
          });
          return new Response(stream, { status: 200 });
        },
      );
      const adapter = new GlmResponsesAdapter(
        { ...testConfig(), streamIdleTimeoutMs: 1000 },
        fetchImpl as unknown as typeof fetch,
      );

      const iterator = adapter.stream(baseOptions({}))[Symbol.asyncIterator]();
      const pending = iterator.next();
      await vi.advanceTimersByTimeAsync(0);

      // A comment keep-alive arrives 800ms into the window and must reset
      // it (the guarded demand is still outstanding).
      await vi.advanceTimersByTimeAsync(800);
      controller.enqueue(encoder.encode(": keep-alive\n\n"));
      await vi.advanceTimersByTimeAsync(0);

      // 1100ms since the demand began, but only 300ms since the pulse: no
      // timeout yet.
      const settled = vi.fn();
      void pending.then(settled, settled);
      await vi.advanceTimersByTimeAsync(300);
      expect(settled).not.toHaveBeenCalled();

      const rejection = expect(pending).rejects.toMatchObject({ code: "TIMEOUT" });
      await vi.advanceTimersByTimeAsync(800);
      await rejection;
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not trip the watchdog when data arrives within the window", async () => {
    vi.useFakeTimers();
    try {
      const encoder = new TextEncoder();
      let controller!: ReadableStreamDefaultController<Uint8Array>;
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c;
        },
      });
      const fetchImpl = vi.fn(async () => new Response(stream, { status: 200 }));
      const adapter = new GlmResponsesAdapter(
        { ...testConfig(), streamIdleTimeoutMs: 1000 },
        fetchImpl as unknown as typeof fetch,
      );

      const chunks: StreamChunk[] = [];
      const drain = (async () => {
        for await (const chunk of adapter.stream(baseOptions({}))) {
          chunks.push(chunk);
        }
      })();

      // Deltas arrive every 700ms, inside the 1000ms window, then the
      // terminal event closes the stream normally.
      for (const delta of ["a", "b", "c"]) {
        await vi.advanceTimersByTimeAsync(700);
        controller.enqueue(
          encoder.encode(
            frame("response.output_text.delta", {
              type: "response.output_text.delta",
              output_index: 0,
              delta,
            }),
          ),
        );
      }
      controller.enqueue(
        encoder.encode(
          frame("response.completed", {
            type: "response.completed",
            response: { status: "completed" },
          }),
        ),
      );
      controller.close();
      await drain;

      expect(chunks.at(-1)).toEqual({ type: "finish", reason: { kind: "stop" } });
      expect(fetchImpl).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });
});

// stalledResponse keeps the body open until the request signal aborts and
// then errors the stream the way a real aborted fetch does; without a
// signal the first read stays pending forever (the stalled-transport
// fixture the watchdog tests need).
function stalledResponse(signal: AbortSignal | undefined): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      signal?.addEventListener("abort", () => {
        controller.error(signal.reason);
      });
    },
  });
  return new Response(stream, { status: 200 });
}
