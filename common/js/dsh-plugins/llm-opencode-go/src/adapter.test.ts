import { afterEach, describe, expect, it, vi } from "vitest";

import { createUserMessage, LlmError } from "@deepseek-ai/dsh-llm";
import type { GenerateOptions, StreamChunk } from "@deepseek-ai/dsh-llm";

import { DEFAULT_STREAM_IDLE_TIMEOUT_MS, OpencodeGoChatAdapter } from "./adapter.js";
import { Config, DEFAULT_MODELS, inject, name } from "./index.js";

const API_KEY_ENV = "OPENCODE_ADAPTER_TEST_API_KEY";

function baseOptions(overrides: Partial<GenerateOptions>): GenerateOptions {
  return {
    provider: "opencode-go",
    model: "glm-5.3",
    messages: [],
    ...overrides,
  };
}

function testConfig() {
  return {
    apiKeyEnv: API_KEY_ENV,
    baseURL: "https://opencode.test/api/v1",
    models: [{ id: "glm-5.3", contextWindow: 1_000_000 }],
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

function frame(data: unknown): string {
  const payload = typeof data === "string" ? data : JSON.stringify(data);
  return `data: ${payload}\n\n`;
}

// chunk builds one chat.completion.chunk payload in the fake-llm handler's
// vocabulary (projects/game/fake-llm/service/handler.go).
function chunk(
  delta: Record<string, unknown>,
  finishReason: string | null = null,
  usage?: unknown,
): Record<string, unknown> {
  return {
    id: "chatcmpl_fake_1",
    object: "chat.completion.chunk",
    created: 0,
    model: "glm-5.3",
    choices: [{ index: 0, delta, finish_reason: finishReason }],
    ...(usage === undefined ? {} : { usage }),
  };
}

function errorResponse(
  status: number,
  body?: string,
  headers?: Record<string, string>,
): Response {
  return new Response(body ?? null, { status, headers });
}

function textTurnFrames(): string[] {
  return [
    frame(chunk({ role: "assistant", reasoning_content: "think" })),
    frame(chunk({ content: "He" })),
    frame(chunk({ content: "y" })),
    frame(chunk({}, "stop")),
    frame("[DONE]"),
  ];
}

async function collect(chunks: AsyncIterable<StreamChunk>): Promise<StreamChunk[]> {
  const out: StreamChunk[] = [];
  for await (const chunk of chunks) {
    out.push(chunk);
  }
  return out;
}

describe("OpencodeGoChatAdapter", () => {
  afterEach(() => {
    delete process.env[API_KEY_ENV];
    vi.restoreAllMocks();
  });

  it("streams the chat wire through the adapter with auth, session, and product UA headers", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(
      adapter.stream(
        baseOptions({
          system: "be nice",
          sessionId: "sess_test_1" as GenerateOptions["sessionId"],
          messages: [
            createUserMessage({
              content: [{ type: "text", text: "hello" }],
              source: { kind: "user" },
            }),
          ],
        }),
      ),
    );

    // Positive assertion that the injected transport was actually exercised
    // (style/javascript.md: every intercepted call must be asserted).
    expect(fetchImpl).toHaveBeenCalledOnce();
    const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://opencode.test/api/v1/chat/completions");
    expect(init.method).toBe("POST");
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer test-key-value");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers["Accept"]).toBe("text/event-stream");
    expect(headers["x-opencode-session"]).toBe("sess_test_1");
    // The gateway-required custom User-Agent (product name + version) IS
    // the mandatory attribution header; no key value may appear in it.
    expect(headers["user-agent"]).toContain("dominion-agent-v2/1.0.0");
    expect(headers["user-agent"]).not.toContain("test-key-value");

    // The chat-completions request body (contract §3).
    expect(JSON.parse(init.body as string)).toEqual({
      model: "glm-5.3",
      messages: [
        { role: "system", content: "be nice" },
        { role: "user", content: "hello" },
      ],
      stream: true,
      stream_options: { include_usage: true },
    });

    // usage precedes finish; finish is last; deltas of one block reuse the
    // same index; the assembled block-end matches the delta concatenation.
    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "think" },
      { type: "block-start", index: 1, blockType: "text" },
      { type: "text-delta", index: 1, text: "He" },
      { type: "text-delta", index: 1, text: "y" },
      { type: "block-end", index: 0, block: { type: "reasoning", text: "think" } },
      { type: "block-end", index: 1, block: { type: "text", text: "Hey" } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("emits nothing after finish even when the provider keeps streaming", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const frames = [
      ...textTurnFrames(),
      frame(chunk({ content: "after finish" })),
      frame(chunk({}, "stop")),
      frame("[DONE]"),
    ];
    const fetchImpl = vi.fn(async () => sseResponse(frames));
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({})));

    const finishes = chunks.filter((c) => c.type === "finish");
    expect(finishes).toHaveLength(1);
    expect(chunks.at(-1)).toEqual({ type: "finish", reason: { kind: "stop" } });
    expect(
      chunks.some(
        (c) => c.type === "text-delta" && (c as { text: string }).text === "after finish",
      ),
    ).toBe(false);
  });

  it("sends the request without an Authorization header when the key env is empty", async () => {
    // Conditional Authorization (contract §3, §6.4): the host tolerates a
    // missing secret, so the request goes out headerless (session header
    // and attribution still present) and the stream is consumed normally.
    process.env[API_KEY_ENV] = "";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(
      adapter.stream(
        baseOptions({ sessionId: "sess_empty_key" as GenerateOptions["sessionId"] }),
      ),
    );

    expect(fetchImpl).toHaveBeenCalledOnce();
    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers).not.toHaveProperty("Authorization");
    expect(headers["x-opencode-session"]).toBe("sess_empty_key");
    expect(headers["user-agent"]).toBeTruthy();
    expect(chunks.at(-1)?.type).toBe("finish");
  });

  it("sends the request without an Authorization header when the key env is blank", async () => {
    process.env[API_KEY_ENV] = "   ";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({})));

    expect(fetchImpl).toHaveBeenCalledOnce();
    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers).not.toHaveProperty("Authorization");
    expect(headers["user-agent"]).toBeTruthy();
    expect(chunks.at(-1)?.type).toBe("finish");
  });

  it("omits the session header when the caller provides no sessionId", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) => sseResponse(textTurnFrames()),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await collect(adapter.stream(baseOptions({})));

    const [, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers).not.toHaveProperty("x-opencode-session");
  });

  it("throws a stable LlmError on HTTP failures without leaking the key", async () => {
    process.env[API_KEY_ENV] = "secret-key-no-leak";
    const fetchImpl = vi.fn(async () => new Response("upstream exploded", { status: 500 }));
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
      code: "SERVER",
    });
    await expect(collect(adapter.stream(baseOptions({})))).rejects.toSatisfy((error: LlmError) => {
      expect(error.message).toBe("OpenCode Go endpoint returned HTTP 500");
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
      const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

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
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("QUOTA");
    expect(error.message).toBe("OpenCode Go endpoint returned HTTP 429");
    expect(error.message).not.toContain("insufficient quota");
    expect(error.message).not.toContain("secret-key-no-leak");
    expect(String(error.cause)).toContain("insufficient quota");
    expect(error.failure.status).toBe(429);
  });

  it("parses Retry-After seconds and HTTP dates into providerRetryAfterMs", async () => {
    async function httpFailure(headers: Record<string, string>): Promise<LlmError> {
      const fetchImpl = vi.fn(async () => errorResponse(429, undefined, headers));
      const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);
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
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

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
          controller.enqueue(encoder.encode(frame(chunk({ content: "partial" }))));
          controller.error(new Error("socket reset"));
        },
      });
      return new Response(stream, { status: 200 });
    });
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("TRANSPORT");
    expect(error.message).toContain("socket reset");
    expect(error.cause).toBeInstanceOf(Error);
  });

  it("maps a successful response without a body to EMPTY_RESPONSE", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 200 }));
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    await expect(collect(adapter.stream(baseOptions({})))).rejects.toMatchObject({
      code: "EMPTY_RESPONSE",
    });
  });

  it("maps a stream ending without [DONE] to STREAM_CLOSED", async () => {
    const fetchImpl = vi.fn(async () =>
      sseResponse([
        frame(chunk({ content: "partial" })),
        frame(chunk({}, "stop")),
      ]),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const error = (await collect(adapter.stream(baseOptions({}))).catch(
      (caught: unknown) => caught,
    )) as LlmError;

    expect(error.code).toBe("STREAM_CLOSED");
    expect(error.message).toContain("[DONE]");
  });

  it("ends with an aborted finish when the signal fires mid-stream", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const controller = new AbortController();
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, init?: RequestInit) =>
        sseResponse(textTurnFrames(), init?.signal instanceof AbortSignal ? init.signal : undefined),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

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

  it("ends with an aborted finish when the signal is already aborted at connect", async () => {
    // Pre-aborted connect-stage cancellation: the transport observes the
    // aborted signal, the adapter never surfaces a retryable error, and the
    // terminal aborted finish (existing cancel semantics) is preserved.
    process.env[API_KEY_ENV] = "test-key-value";
    const controller = new AbortController();
    controller.abort();
    const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
      expect(init?.signal?.aborted).toBe(true);
      throw new DOMException("The operation was aborted.", "AbortError");
    });
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    const chunks = await collect(adapter.stream(baseOptions({ signal: controller.signal })));

    expect(fetchImpl).toHaveBeenCalledOnce();
    expect(chunks).toEqual([
      {
        type: "finish",
        reason: {
          kind: "aborted",
          failure: { message: "OpenCode Go stream aborted by caller", code: "ABORTED" },
        },
      },
    ]);
  });

  it("advertises the static config.models catalog without an endpoint call", async () => {
    const fetchImpl = vi.fn(async () => sseResponse(textTurnFrames()));
    const adapter = new OpencodeGoChatAdapter(
      {
        apiKeyEnv: API_KEY_ENV,
        baseURL: "https://opencode.test/api/v1",
        models: [
          { id: "glm-5.3", contextWindow: 1_000_000 },
          { id: "kimi-k3", contextWindow: 1_048_576 },
        ],
      },
      fetchImpl as unknown as typeof fetch,
    );

    const models = await adapter.listModels("opencode-go");

    expect(models).toEqual([
      { provider: "opencode-go", id: "glm-5.3", name: "glm-5.3" },
      { provider: "opencode-go", id: "kimi-k3", name: "kimi-k3" },
    ]);
    // Positive assertion that the static catalog truly bypasses the
    // transport (style/javascript.md: every intercepted call must be
    // asserted — here, asserted to NOT happen).
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("resolves catalog models with their context window and unknown models minimally", async () => {
    process.env[API_KEY_ENV] = "test-key-value";
    const fetchImpl = vi.fn(async () => sseResponse(textTurnFrames()));
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

    expect(await adapter.resolveModel("opencode-go", "glm-5.3")).toEqual({
      provider: "opencode-go",
      id: "glm-5.3",
      name: "glm-5.3",
      context: { contextWindow: 1_000_000 },
    });
    // Catalog membership is advisory: an unlisted id resolves to minimal
    // metadata and is never rejected (FR-014, contract §6.6).
    expect(await adapter.resolveModel("opencode-go", "catalog-outsider")).toEqual({
      provider: "opencode-go",
      id: "catalog-outsider",
      name: "catalog-outsider",
    });
    expect(adapter.providerInfo("opencode-go")).toEqual({
      id: "opencode-go",
      name: "OpenCode Go (Chat Completions)",
    });
  });

  it("resolves the configured retry policy and falls back to the dsh default", () => {
    const defaultAdapter = new OpencodeGoChatAdapter(testConfig());
    expect(defaultAdapter.providerRetryPolicy("opencode-go")).toEqual({
      mode: "normal",
      maxRetries: 5,
      retryableCodes: ["EMPTY_RESPONSE", "RATE_LIMIT", "SERVER", "TIMEOUT", "TRANSPORT"],
      initialDelayMs: 500,
      maxDelayMs: 10000,
      jitterRatio: 0.1,
    });

    const configured = new OpencodeGoChatAdapter({
      ...testConfig(),
      retryPolicy: {
        mode: "normal",
        maxRetries: 2,
        retryableCodes: ["SERVER"],
        backoff: { initialDelayMs: 250 },
      },
    });
    expect(configured.providerRetryPolicy("opencode-go")).toEqual({
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
        controller.enqueue(encoder.encode(frame(chunk({ content: "Hel" }))));
      },
      cancel() {
        cancelled = true;
      },
    });
    const fetchImpl = vi.fn(
      async (_url: string | URL | Request, _init?: RequestInit) =>
        new Response(stream, { status: 200 }),
    );
    const adapter = new OpencodeGoChatAdapter(testConfig(), fetchImpl as unknown as typeof fetch);

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
      const adapter = new OpencodeGoChatAdapter(
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
      const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
        const signal = init?.signal instanceof AbortSignal ? init.signal : undefined;
        const stream = new ReadableStream<Uint8Array>({
          start(c) {
            controller = c;
            signal?.addEventListener("abort", () => c.error(signal.reason));
          },
        });
        return new Response(stream, { status: 200 });
      });
      const adapter = new OpencodeGoChatAdapter(
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
      const adapter = new OpencodeGoChatAdapter(
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
        controller.enqueue(encoder.encode(frame(chunk({ content: delta }))));
      }
      controller.enqueue(encoder.encode(frame(chunk({}, "stop"))));
      controller.enqueue(encoder.encode(frame("[DONE]")));
      controller.close();
      await drain;

      expect(chunks.at(-1)).toEqual({ type: "finish", reason: { kind: "stop" } });
      expect(fetchImpl).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the default catalog exactly the 16 official Chat Completions models", () => {
    // Contract §2/§6.7: the Config default catalog is the gateway's
    // documented Chat Completions route set — no Responses/Anthropic-routed
    // models and no gateway-only undocumented ids.
    const config = Config({} as Parameters<typeof Config>[0]);

    expect(config.apiKeyEnv).toBe("OPENCODE_API_KEY");
    expect(config.baseURL).toBe("https://opencode.ai/zen/go/v1");
    expect(config.streamIdleTimeoutMs).toBe(DEFAULT_STREAM_IDLE_TIMEOUT_MS);
    expect(config.retryPolicy).toBeUndefined();

    expect(DEFAULT_MODELS).toEqual([
      { id: process.env.OPENCODE_MODEL || "glm-5.3", contextWindow: 1_000_000 },
      { id: "glm-5.3-flash", contextWindow: 1_000_000 },
      { id: "glm-5.2", contextWindow: 1_000_000 },
      { id: "glm-5.1", contextWindow: 202_752 },
      { id: "kimi-k3", contextWindow: 1_048_576 },
      { id: "kimi-k2.7-code", contextWindow: 262_144 },
      { id: "kimi-k2.6", contextWindow: 262_144 },
      { id: "longcat-2.0", contextWindow: 1_000_000 },
      { id: "deepseek-v4.1-flash", contextWindow: 1_000_000 },
      { id: "deepseek-v4-pro", contextWindow: 1_000_000 },
      { id: "deepseek-v4-flash", contextWindow: 1_000_000 },
      { id: "deepseek-v4-flash-vision-exp", contextWindow: 1_000_000 },
      { id: "mimo-v2.5", contextWindow: 1_000_000 },
      { id: "mimo-v2.5-pro", contextWindow: 1_048_576 },
      { id: "hy4-preview", contextWindow: 1_024_000 },
      { id: "hy3", contextWindow: 256_000 },
    ]);
    expect(config.models).toEqual(DEFAULT_MODELS);
    expect(new Set(DEFAULT_MODELS.map((entry) => entry.id)).size).toBe(16);

    const ids = DEFAULT_MODELS.map((entry) => entry.id);
    for (const excluded of [
      // Responses-route models.
      "grok-4.6",
      "gpt-5.6-luna",
      "muse-spark-1.3-contributor",
      "muse-spark-1.2-contributor",
      // Anthropic Messages-route models.
      "minimax-m3",
      "minimax-m2.7",
      "qwen3.8-max",
      "qwen3.8-flash",
      // Gateway-only undocumented ids.
      "glm-5",
      "kimi-k2.5",
      "mimo-v2-pro",
      "mimo-v2-omni",
      "qwen3.5-plus",
      "grok-4.5",
      "omen-alpha",
      "ox-alpha-free",
    ]) {
      expect(ids).not.toContain(excluded);
    }

    expect(name).toBe("llm-opencode-go");
    expect(inject).toEqual(["llm"]);
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
