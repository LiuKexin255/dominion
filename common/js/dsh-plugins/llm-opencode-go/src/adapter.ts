/**
 * OpencodeGoChatAdapter: the dsh LLM adapter for the opencode-go gateway's
 * OpenAI Chat Completions endpoint (POST {baseURL}/chat/completions, SSE
 * stream). Assembles serialize → fetch(SSE) → wire chunk flow and carries
 * the transport-side protocol obligations from the official adapter
 * cookbook:
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md
 * Contract: specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md
 * §3/§5 (HTTP failure classification and retry-policy declaration follow
 * specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md
 * §1-§2, the same obligation set as the GLM adapter).
 */

import {
  assertUsableApiKey,
  attributionHeaders,
  isContextWindowExceededError,
  isQuotaExceededError,
  LlmAdapter,
  LlmError,
  resolveRetryPolicy,
} from "@deepseek-ai/dsh-llm";
import type {
  AppIdentity,
  GenerateOptions,
  LlmModelInfo,
  LlmProviderInfo,
  LlmResolvedModelInfo,
  ResolvedRetryPolicy,
  RetryPolicyConfig,
  StreamChunk,
} from "@deepseek-ai/dsh-llm";
import { idleWatchdog, timeoutOf } from "@deepseek-ai/dsh-timeout";
import type { ReadableStreamReadResult } from "node:stream/web";

import { serializeRequest } from "./serialize.js";
import { createChatCompletionsWire } from "./wire.js";

/** Plugin configuration (cordis row: contract §2). */
export interface OpencodeGoConfig {
  /** Token environment variable name; empty value → the request carries no Authorization header. */
  apiKeyEnv: string;
  /** Endpoint base URL, version path included (default https://opencode.ai/zen/go/v1). */
  baseURL: string;
  /** Static model catalog (resolveModel's advisory source; never request validation). */
  models: ReadonlyArray<{ id: string; contextWindow: number }>;
  /** dsh RetryPolicySchema pass-through; omission selects the dsh default (normal/5/500ms→10s). */
  retryPolicy?: RetryPolicyConfig;
  /** Stream idle-watchdog window (ms), default 300000 (llm-failure-taxonomy.md §1 义务 4). */
  streamIdleTimeoutMs?: number;
}

/** The package name prefixed to api-key diagnostics (never the key itself). */
const PACKAGE_NAME = "@dominion/dsh-llm-opencode-go";

/** opencode-go provider display metadata for selectors and diagnostics. */
const PROVIDER_NAME = "OpenCode Go (Chat Completions)";

/** Idle watchdog window default, aligned with the official adapter. */
export const DEFAULT_STREAM_IDLE_TIMEOUT_MS = 300_000;

/** Capability-owned code stamped on the watchdog's timeout abort reason. */
const STREAM_IDLE_TIMEOUT_CODE = "LLM_STREAM_IDLE_TIMEOUT";

/**
 * Product identity for the gateway-required custom User-Agent (contract §3
 * and §5: product name plus version, never credentials). Passed to
 * `attributionHeaders` so the mandatory attribution header IS the custom
 * product UA; the parenthesized URL is the AppIdentity convention shared
 * with the official adapters.
 */
const APP_IDENTITY: AppIdentity = {
  product: "dominion-agent-v2",
  version: "1.0.0",
  url: "https://github.com/LiuKexin255/dominion",
};

export class OpencodeGoChatAdapter extends LlmAdapter {
  private readonly config: OpencodeGoConfig;
  private readonly fetchImpl: typeof fetch;
  private readonly streamIdleTimeoutMs: number;

  constructor(
    config: OpencodeGoConfig,
    // Test seam: the transport is injectable so adapter tests drive the
    // full protocol path against a vi.fn() without module interception
    // (style/javascript.md Mock 约定).
    fetchImpl?: typeof fetch,
  ) {
    super();
    this.config = config;
    this.fetchImpl = fetchImpl ?? globalThis.fetch.bind(globalThis);
    this.streamIdleTimeoutMs = config.streamIdleTimeoutMs ?? DEFAULT_STREAM_IDLE_TIMEOUT_MS;
  }

  override providerInfo(provider: string): LlmProviderInfo {
    return { id: provider, name: PROVIDER_NAME };
  }

  override providerRetryPolicy(_provider: string): ResolvedRetryPolicy {
    return resolveRetryPolicy(this.config.retryPolicy, `${PACKAGE_NAME}: retryPolicy`);
  }

  /**
   * The static model catalog: `config.models` projected onto LlmModelInfo,
   * with no endpoint call. This is the single source the agent_v2
   * ListModels RPC and UpdateTeam's model validation share; catalog
   * membership is advisory (llm-glm same semantics, FR-014).
   */
  override async listModels(provider: string): Promise<readonly LlmModelInfo[]> {
    return this.config.models.map((entry) => ({ provider, id: entry.id, name: entry.id }));
  }

  override async resolveModel(
    provider: string,
    model: string,
    _signal?: AbortSignal,
  ): Promise<LlmResolvedModelInfo> {
    const known = this.config.models.find((entry) => entry.id === model);
    if (known === undefined) {
      // Catalog membership is advisory: an unlisted model id resolves to
      // minimal metadata and is never rejected (FR-014).
      return { provider, id: model, name: model };
    }
    return {
      provider,
      id: model,
      name: model,
      context: { contextWindow: known.contextWindow },
    };
  }

  /**
   * Stream one model call through a consumer-facing watchdog-guarded
   * iterator over {@link request}. Each outstanding `next()` arms the idle
   * watchdog; SSE comments pulse it; a stall aborts the underlying fetch
   * and surfaces as a retryable TIMEOUT. Every exit path (consumer
   * return()/throw, error, normal end) aborts the transport and closes the
   * body (llm-failure-taxonomy.md §1 义务 4-6).
   */
  override async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    const consumer = new AbortController();
    const signal =
      options.signal === undefined
        ? consumer.signal
        : AbortSignal.any([options.signal, consumer.signal]);
    const watchdog = idleWatchdog(signal, this.streamIdleTimeoutMs, STREAM_IDLE_TIMEOUT_CODE);
    const iterator = this.request(options, watchdog.signal, () => {
      watchdog.pulse();
    })[Symbol.asyncIterator]();
    let exhausted = false;
    try {
      for (;;) {
        let result: IteratorResult<StreamChunk>;
        try {
          result = await watchdog.next(iterator);
        } catch (error) {
          if (timeoutOf(watchdog.signal, STREAM_IDLE_TIMEOUT_CODE) !== undefined) {
            throw new LlmError(
              `OpenCode Go stream idle timeout after ${this.streamIdleTimeoutMs}ms`,
              "TIMEOUT",
              { cause: error },
            );
          }
          if (options.signal?.aborted) {
            // Caller cancellation keeps its existing terminal
            // finish{kind:'aborted'} semantics and never surfaces as a
            // retryable error (FR-005, llm-failure-taxonomy.md §1 义务 6).
            yield abortedFinish();
            return;
          }
          if (error instanceof LlmError) {
            throw error;
          }
          throw new LlmError(`OpenCode Go stream failed: ${errorMessage(error)}`, "TRANSPORT", {
            cause: error,
          });
        }
        if (result.done) {
          exhausted = true;
          return;
        }
        yield result.value;
      }
    } finally {
      // Independent controller: aborting it tears down the fetch and the
      // response body even when the consumer stopped between reads; the
      // guarded iterator is then closed so its own finally releases the
      // reader lock and cancels the remaining body (FR-008).
      consumer.abort();
      watchdog[Symbol.dispose]();
      if (!exhausted && iterator.return !== undefined) {
        try {
          await iterator.return(undefined);
        } catch {
          // The transport is already being torn down; a failing teardown
          // must not mask the original outcome.
        }
      }
    }
  }

  /**
   * The transport half: one fetch plus SSE body iteration, yielding wire
   * chunks as they arrive. Throws the classified LlmError for every
   * transport/protocol failure; the caller of `stream()` maps watchdog
   * timeouts and caller cancellation before rethrowing (see `stream`).
   */
  private async *request(
    options: GenerateOptions,
    signal: AbortSignal,
    pulse: () => void,
  ): AsyncIterable<StreamChunk> {
    // Conditional Authorization (contract §3): the host's three-level token
    // resolution tolerates a missing secret, so an empty/whitespace key
    // sends the request WITHOUT an Authorization header — a real endpoint's
    // 401 then surfaces as turn_end{ERROR}. A non-empty key is validated
    // (assertUsableApiKey's header-safe-character diagnostics) and sent as
    // Bearer. The key value never enters a message or log; only its env
    // reference does.
    const rawApiKey = process.env[this.config.apiKeyEnv] ?? "";
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "Accept": "text/event-stream",
      ...attributionHeaders(APP_IDENTITY),
    };
    if (rawApiKey.trim() !== "") {
      const apiKey = assertUsableApiKey(
        rawApiKey,
        PACKAGE_NAME,
        `env ${this.config.apiKeyEnv}`,
      );
      headers["Authorization"] = `Bearer ${apiKey}`;
    }
    // Stable session identity for gateway routing and prompt caching
    // (opencode.ai Go guidance); omitted when the caller provides none.
    if (options.sessionId !== undefined) {
      headers["x-opencode-session"] = String(options.sessionId);
    }

    const request = serializeRequest(options);
    let response: Response;
    try {
      response = await this.fetchImpl(`${this.config.baseURL}/chat/completions`, {
        method: "POST",
        headers,
        body: JSON.stringify(request),
        signal,
      });
    } catch (error) {
      throw new LlmError(
        `OpenCode Go endpoint unreachable: ${errorMessage(error)}`,
        "TRANSPORT",
        { cause: error },
      );
    }

    if (!response.ok) {
      // The error body is parsed for the shared quota/context classifiers
      // only and chained as `cause`; it never enters `message` (a provider
      // or intermediate proxy may reflect request headers in error bodies,
      // and token zero-leakage must hold on every path —
      // specs/063-llm-reliability-opencode-go/spec.md SC-003).
      const rawBody = await response.text();
      const retryAfterMs = providerRetryAfterMs(response.headers.get("retry-after"));
      throw new LlmError(
        `OpenCode Go endpoint returned HTTP ${response.status}`,
        httpErrorCode(response.status, providerErrorDetail(rawBody)),
        {
          cause: new Error(
            rawBody.length > 0 ? rawBody : `OpenCode Go endpoint returned HTTP ${response.status}`,
          ),
          status: response.status,
          ...(retryAfterMs === undefined ? {} : { providerRetryAfterMs: retryAfterMs }),
        },
      );
    }
    if (response.body === null) {
      throw new LlmError(
        "OpenCode Go endpoint returned a response without a body",
        "EMPTY_RESPONSE",
      );
    }

    const wire = createChatCompletionsWire(pulse);
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    try {
      for (;;) {
        let read: ReadableStreamReadResult<Uint8Array>;
        try {
          read = await reader.read();
        } catch (error) {
          throw new LlmError(
            `OpenCode Go stream interrupted: ${errorMessage(error)}`,
            "TRANSPORT",
            { cause: error },
          );
        }
        if (read.done) {
          break;
        }
        for (const chunk of wire.feed(decoder.decode(read.value, { stream: true }))) {
          yield chunk;
        }
      }
      for (const chunk of wire.feed(decoder.decode())) {
        yield chunk;
      }
      // The wire owns the [DONE] sentinel: a stream that ends without it
      // throws STREAM_CLOSED here, classifying the truncated response
      // (llm-failure-taxonomy.md §1).
      for (const chunk of wire.end()) {
        yield chunk;
      }
    } finally {
      // Every exit path (consumer return, watchdog abort, protocol error,
      // clean end) cancels the remaining body and releases the lock so no
      // connection is left dangling (FR-008). A refusal here (the stream
      // is already errored) must not mask the original outcome.
      try {
        await reader.cancel();
      } catch {
        // ignored: the stream is already errored or closed
      }
      reader.releaseLock();
    }
  }
}

function abortedFinish(): StreamChunk {
  return {
    type: "finish",
    reason: {
      kind: "aborted",
      failure: { message: "OpenCode Go stream aborted by caller", code: "ABORTED" },
    },
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Join the provider error body's code/type/message into one classifier
 * input; anything not matching the expected JSON shape contributes "".
 * The result is consumed only by the shared quota/context predicates and
 * never surfaces in messages (llm-failure-taxonomy.md §1 义务 1).
 */
function providerErrorDetail(rawBody: string): string {
  let parsed: unknown;
  try {
    parsed = JSON.parse(rawBody);
  } catch {
    return "";
  }
  if (parsed === null || typeof parsed !== "object") {
    return "";
  }
  const error = (parsed as { error?: unknown }).error;
  if (error === null || typeof error !== "object") {
    return "";
  }
  const fields = error as { code?: unknown; type?: unknown; message?: unknown };
  return [fields.code, fields.type, fields.message]
    .filter((field): field is string => typeof field === "string")
    .join(" ");
}

/**
 * Map an HTTP status plus provider error detail onto the shared failure
 * taxonomy (llm-failure-taxonomy.md §1): AUTH for 401/403; QUOTA for quota
 * wording on any status; RATE_LIMIT for 429; SERVER for 5xx;
 * CONTEXT_WINDOW_EXCEEDED / INVALID_REQUEST for 400; HTTP_<status> for the
 * rest.
 */
function httpErrorCode(status: number, detail: string): string {
  if (status === 401 || status === 403) {
    return "AUTH";
  }
  if (isQuotaExceededError(detail)) {
    return "QUOTA";
  }
  if (status === 429) {
    return "RATE_LIMIT";
  }
  if (status >= 500) {
    return "SERVER";
  }
  if (status === 400) {
    if (isContextWindowExceededError(detail)) {
      return "CONTEXT_WINDOW_EXCEEDED";
    }
    return "INVALID_REQUEST";
  }
  return `HTTP_${status}`;
}

/**
 * Parse a `Retry-After` header into the LlmError `providerRetryAfterMs`
 * option: delta-seconds or an HTTP date; anything invalid or non-positive
 * yields undefined (llm-retry then falls back to local backoff).
 */
function providerRetryAfterMs(value: string | null): number | undefined {
  if (value === null) {
    return undefined;
  }
  if (/^\d+$/.test(value)) {
    const delay = Number(value) * 1000;
    return Number.isFinite(delay) && delay > 0 ? delay : undefined;
  }
  const delay = Date.parse(value) - Date.now();
  return Number.isFinite(delay) && delay > 0 ? delay : undefined;
}
