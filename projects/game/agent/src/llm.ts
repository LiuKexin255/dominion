/**
 * llm.ts — LLM adapter wrapping LangChain createDeepAgent for text dialog.
 *
 * ContentBlock types match LangChain's block structure:
 *   - reasoning → { type: "reasoning", reasoning: string }
 *   - text      → { type: "text", text: string }
 *
 * The RealLLMAdapter uses initChatModel pointed at opencode-go's proxy
 * endpoint, selects the OpenAI or Anthropic wire format by model ID, wraps the
 * model in createDeepAgent with built-in defaults, and streams contentBlocks via
 * agent.streamEvents().
 */

import { HumanMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { createDeepAgent } from "deepagents";
import { initChatModel } from "langchain/chat_models/universal";
import { info, error as logError } from "@dominion/common-js-logs";

// ---------------------------------------------------------------------------
// ContentBlock types (discriminated union matching LangChain block structure)
// ---------------------------------------------------------------------------

export type ContentBlock =
  | { type: "reasoning"; reasoning: string }
  | { type: "text"; text: string };

// ---------------------------------------------------------------------------
// Provider selection
// ---------------------------------------------------------------------------

export type LLMProvider = "openai" | "anthropic";

const ANTHROPIC_MODEL_PREFIXES = ["minimax-", "qwen3."];

/**
 * Extract the bare model name from a `{provider}/{model}` spec.
 *
 * Profile model fields use the format `opencode-go/{model-name}`. This
 * function strips the provider prefix so the bare name can be passed to
 * LangChain's `initChatModel`. If no `/` is present, the input is returned
 * as-is for backward compatibility.
 */
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
 * OpenCode Go exposes most models through an OpenAI-compatible endpoint, but
 * MiniMax and Qwen models use an Anthropic-compatible `/messages` endpoint.
 * See: https://opencode.ai/docs/zh-cn/go/
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
// LLMAdapter interface
// ---------------------------------------------------------------------------

export interface LLMAdapter {
  /**
   * Generate a single conversational turn.
   *
   * @param model          - Model ID to use for this turn (per-profile).
   * @param systemPrompt   - System prompt text for agent personality/instructions.
   * @param threadId       - Stable checkpoint thread identifier (sessionId).
   * @param userMessage    - The user's message for this turn.
   * @param checkpointer   - Shared in-memory checkpointer (injected, NOT created per-call).
   * @param providerSecret - API key for the model provider.
   *
   * @returns Async iterable of ContentBlock objects in streaming order
   *          (reasoning blocks before text blocks).
   */
  generateTurn(
    model: string,
    systemPrompt: string,
    threadId: string,
    userMessage: string,
    checkpointer: MemorySaver,
    providerSecret: string,
  ): AsyncIterable<ContentBlock>;
}

// ---------------------------------------------------------------------------
// RealLLMAdapter — production implementation
// ---------------------------------------------------------------------------

export class RealLLMAdapter implements LLMAdapter {
  readonly baseUrl: string;

  readonly provider?: LLMProvider;

  constructor(baseUrl: string, provider?: LLMProvider) {
    this.baseUrl = baseUrl;
    this.provider = provider;
  }

  async *generateTurn(
    model: string,
    systemPrompt: string,
    threadId: string,
    userMessage: string,
    checkpointer: MemorySaver,
    providerSecret: string,
  ): AsyncIterable<ContentBlock> {
    const bareModel = parseModelSpec(model);
    const provider = this.provider ?? inferProvider(bareModel);

    info("initializing LLM model", {
      model: bareModel,
      provider,
      baseUrl: this.baseUrl,
    });

    let chatModel: Awaited<ReturnType<typeof initChatModel>>;
    try {
      chatModel = await initChatModel(bareModel, {
        modelProvider: provider,
        apiKey: providerSecret,
        ...(provider === "openai"
          ? { configuration: { baseURL: this.baseUrl } }
          : { anthropicApiUrl: this.baseUrl }),
      });
    } catch (err) {
      logError("LLM model initialization failed", {
        model: bareModel,
        provider,
        error: err instanceof Error ? err.message : String(err),
      });
      throw err;
    }

    yield* this.streamFromModel(chatModel, systemPrompt, userMessage, threadId, checkpointer);
  }

  /**
   * Core streaming logic separated for testability.
   *
   * Accepts any model supporting the BaseChatModel protocol (including
   * fakeModel from @langchain/core/testing) so tests can inject determinism
   * without hitting a real provider.
   *
   * The model parameter is typed loosely to accept both real ChatOpenAI
   * instances and fakeModel test doubles.
   */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private async *streamFromModel(
    model: any,
    systemPrompt: string,
    userMessage: string,
    threadId: string,
    checkpointer: MemorySaver,
  ): AsyncIterable<ContentBlock> {
    // createDeepAgent is synchronous in TypeScript (confirmed by spike).
    const agent = createDeepAgent({
      model,
      systemPrompt,
      checkpointer,
    });

    const stream = agent.streamEvents(
      {
        messages: [new HumanMessage(userMessage)],
      },
      {
        configurable: { thread_id: threadId },
        streamMode: "messages",
        version: "v2",
      },
    );

    for await (const event of stream) {
      const data = (event as Record<string, unknown>).data as
        | Record<string, unknown>
        | undefined;
      const chunk = data?.chunk as
        | { contentBlocks?: ContentBlock[] }
        | undefined;
      if (chunk && Array.isArray(chunk.contentBlocks)) {
        for (const block of chunk.contentBlocks) {
          if (block.type === "reasoning" || block.type === "text") {
            yield block as ContentBlock;
          }
        }
      }
    }
  }
}
