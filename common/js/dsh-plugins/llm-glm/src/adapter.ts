/**
 * GlmResponsesAdapter: the dsh LLM adapter for the GLM codingplan OpenAI
 * Responses endpoint (POST {baseURL}/responses, SSE stream). Assembles
 * serialize → fetch(SSE) → wire chunk flow and carries the transport-side
 * protocol obligations from the official adapter cookbook:
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md
 * Contract: specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §3.
 */

import {
  assertUsableApiKey,
  attributionHeaders,
  LlmAdapter,
  LlmError,
} from "@deepseek-ai/dsh-llm";
import type {
  GenerateOptions,
  LlmProviderInfo,
  LlmResolvedModelInfo,
  StreamChunk,
} from "@deepseek-ai/dsh-llm";

import { serializeRequest } from "./serialize.js";
import { createResponsesWire } from "./wire.js";

/** Plugin configuration (cordis row: data-model.md §2.6). */
export interface GlmConfig {
  /** GLM API token 的环境变量名；值可为空——空 key 请求不携带 Authorization（§3 义务 6，宿主三级解析容忍缺失的终点，research.md D9）；非空值经 assertUsableApiKey 校验。 */
  apiKeyEnv: string;
  /** OpenAI Responses 端点，含版本路径（如 https://open.bigmodel.cn/api/v1）。 */
  baseURL: string;
  /** 显式模型目录（resolveModel 依据；catalog advisory 不做请求校验）。 */
  models: ReadonlyArray<{ id: string; contextWindow: number }>;
}

/** The package name prefixed to api-key diagnostics (never the key itself). */
const PACKAGE_NAME = "@dominion/dsh-llm-glm";

/** GLM provider display metadata for selectors and diagnostics. */
const PROVIDER_NAME = "GLM (OpenAI Responses)";

export class GlmResponsesAdapter extends LlmAdapter {
  private readonly config: GlmConfig;
  private readonly fetchImpl: typeof fetch;

  constructor(
    config: GlmConfig,
    // Test seam: the transport is injectable so adapter tests drive the
    // full protocol path against a vi.fn() without module interception
    // (style/javascript.md Mock 约定).
    fetchImpl?: typeof fetch,
  ) {
    super();
    this.config = config;
    this.fetchImpl = fetchImpl ?? globalThis.fetch.bind(globalThis);
  }

  override providerInfo(provider: string): LlmProviderInfo {
    return { id: provider, name: PROVIDER_NAME };
  }

  override async resolveModel(
    provider: string,
    model: string,
    _signal?: AbortSignal,
  ): Promise<LlmResolvedModelInfo> {
    const known = this.config.models.find((entry) => entry.id === model);
    if (known === undefined) {
      // Catalog membership is advisory: an unlisted model id resolves to
      // minimal metadata and is never rejected.
      return { provider, id: model, name: model };
    }
    return {
      provider,
      id: model,
      name: model,
      context: { contextWindow: known.contextWindow },
    };
  }

  override async *stream(options: GenerateOptions): AsyncIterable<StreamChunk> {
    // Conditional Authorization (glm-llm-plugin.md §3 义务 6): the host's
    // three-level token resolution tolerates a missing secret (research.md
    // D9), so an empty/whitespace key sends the request WITHOUT an
    // Authorization header — the fake endpoint ignores credentials and a
    // real endpoint's 401 surfaces as turn_end{ERROR}. A non-empty key is
    // validated (assertUsableApiKey's header-safe-character diagnostics)
    // and sent as Bearer. The key value never enters a message or log; only
    // its env reference does.
    const rawApiKey = process.env[this.config.apiKeyEnv] ?? "";
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "Accept": "text/event-stream",
      ...attributionHeaders(),
    };
    if (rawApiKey.trim() !== "") {
      const apiKey = assertUsableApiKey(
        rawApiKey,
        PACKAGE_NAME,
        `env ${this.config.apiKeyEnv}`,
      );
      headers["Authorization"] = `Bearer ${apiKey}`;
    }

    const request = serializeRequest(options);
    let response: Response;
    try {
      response = await this.fetchImpl(`${this.config.baseURL}/responses`, {
        method: "POST",
        headers,
        body: JSON.stringify(request),
        signal: options.signal,
      });
    } catch (error) {
      if (isAbort(error, options.signal)) {
        yield abortedFinish();
        return;
      }
      throw new LlmError(
        `GLM endpoint unreachable: ${errorMessage(error)}`,
        "GLM_TRANSPORT",
      );
    }

    if (!response.ok) {
      // The response body is deliberately not echoed: a provider or an
      // intermediate proxy may reflect request headers (including the
      // Authorization value) in error bodies, and token zero-leakage
      // (specs/049-agent-v2-dsh-init/spec.md SC-004) must hold on every
      // path. The stable code plus the status give diagnosis enough
      // context.
      throw new LlmError(
        `GLM endpoint returned HTTP ${response.status}`,
        `GLM_HTTP_${response.status}`,
      );
    }
    if (response.body === null) {
      throw new LlmError("GLM endpoint returned a response without a body", "GLM_PROTOCOL");
    }

    const wire = createResponsesWire();
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let sawFinish = false;
    try {
      for (;;) {
        let read: ReadableStreamReadResult<Uint8Array>;
        try {
          read = await reader.read();
        } catch (error) {
          if (isAbort(error, options.signal)) {
            yield abortedFinish();
            return;
          }
          throw new LlmError(
            `GLM stream interrupted: ${errorMessage(error)}`,
            "GLM_TRANSPORT",
          );
        }
        if (read.done) {
          break;
        }
        for (const chunk of wire.feed(decoder.decode(read.value, { stream: true }))) {
          if (chunk.type === "finish") {
            sawFinish = true;
          }
          yield chunk;
        }
      }
      for (const chunk of wire.feed(decoder.decode())) {
        if (chunk.type === "finish") {
          sawFinish = true;
        }
        yield chunk;
      }
    } finally {
      reader.releaseLock();
    }

    if (!sawFinish) {
      // A stream that ends without a terminal event is a protocol failure:
      // consumers could not observe usage/finish (cookbook: exactly one
      // finish, nothing after it).
      throw new LlmError(
        "GLM stream ended without a terminal event",
        "GLM_PROTOCOL",
      );
    }
  }
}

function abortedFinish(): StreamChunk {
  return {
    type: "finish",
    reason: {
      kind: "aborted",
      failure: { message: "GLM stream aborted by caller", code: "ABORTED" },
    },
  };
}

function isAbort(error: unknown, signal: AbortSignal | undefined): boolean {
  if (signal?.aborted) {
    return true;
  }
  return error instanceof Error && error.name === "AbortError";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
