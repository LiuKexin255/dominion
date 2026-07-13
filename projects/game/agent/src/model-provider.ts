/**
 * model-provider.ts — Per-model singleton cache for LangChain ChatModel
 * instances.
 *
 * The provider (ChatModel) manages HTTP connection pools and is expensive
 * to create.  It is cached by bare model name so that all sessions using
 * the same model share one instance.
 *
 * AgentAdapter instances, by contrast, are lightweight and created per
 * session inside SessionAgent.
 */

import { initChatModel } from "langchain/chat_models/universal";
import type { BaseChatModel } from "@langchain/core/language_models/chat_models";
import { info, error as logError } from "@dominion/common-js-logs";

// ---------------------------------------------------------------------------
// Provider selection helpers
// ---------------------------------------------------------------------------

export type LLMProvider = "openai" | "anthropic";

const ANTHROPIC_MODEL_PREFIXES = ["minimax-", "qwen3."];

/** Strip the `{provider}/` prefix from a model spec, returning the bare name. */
export function parseModelSpec(modelSpec: string): string {
  const slashIndex = modelSpec.indexOf("/");
  if (slashIndex === -1) {
    return modelSpec;
  }
  return modelSpec.slice(slashIndex + 1);
}

/**
 * Infer the provider wire format from the model ID.
 *
 * OpenCode Go exposes most models through an OpenAI-compatible endpoint,
 * but MiniMax and Qwen models use an Anthropic-compatible /messages endpoint.
 * The caller is responsible for supplying the matching base URL for each
 * provider family.
 */
export function inferProvider(modelName: string): LLMProvider {
  const lower = modelName.toLowerCase();
  for (const prefix of ANTHROPIC_MODEL_PREFIXES) {
    if (lower.startsWith(prefix)) {
      return "anthropic";
    }
  }
  return "openai";
}

// ---------------------------------------------------------------------------
// ChatModel type — broad enough for createAgent and test fakes
// ---------------------------------------------------------------------------

export type ChatModel = BaseChatModel;

// ---------------------------------------------------------------------------
// Provider factory (overridable for testing)
// ---------------------------------------------------------------------------

export type ProviderFactory = (
  bareModel: string,
  provider: LLMProvider,
  baseUrl: string,
  providerSecret: string,
) => Promise<ChatModel>;

const defaultProviderFactory: ProviderFactory = async (
  bareModel,
  provider,
  baseUrl,
  providerSecret,
) => {
  try {
    return await initChatModel(bareModel, {
      modelProvider: provider,
      apiKey: providerSecret,
      ...(provider === "openai"
        ? { configuration: { baseURL: baseUrl } }
        : { anthropicApiUrl: baseUrl }),
    });
  } catch (err) {
    logError("LLM model initialization failed", {
      model: bareModel,
      provider,
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
};

// ---------------------------------------------------------------------------
// ModelProviderCache
// ---------------------------------------------------------------------------

export class ModelProviderCache {
  private cache = new Map<string, ChatModel>();

  constructor(
    private readonly openaiBaseUrl: string,
    private readonly anthropicBaseUrl: string,
    private readonly providerSecret: string,
    private readonly factory: ProviderFactory = defaultProviderFactory,
  ) {}

  async getProvider(modelSpec: string): Promise<ChatModel> {
    const bareModel = parseModelSpec(modelSpec);
    const existing = this.cache.get(bareModel);
    if (existing) {
      return existing;
    }

    const provider = inferProvider(bareModel);
    const baseUrl =
      provider === "openai" ? this.openaiBaseUrl : this.anthropicBaseUrl;
    info("creating model provider", { model: bareModel, provider });
    const chatModel = await this.factory(
      bareModel,
      provider,
      baseUrl,
      this.providerSecret,
    );
    this.cache.set(bareModel, chatModel);
    return chatModel;
  }
}
