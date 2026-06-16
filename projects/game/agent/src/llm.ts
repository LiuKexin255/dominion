/**
 * llm.ts — Agent Adapter wrapping LangChain createAgent for text dialog.
 *
 * ContentBlock types match LangChain's block structure:
 *   - reasoning → { type: "reasoning", reasoning: string }
 *   - text      → { type: "text", text: string }
 *
 * The AgentAdapter uses initChatModel pointed at opencode-go's proxy
 * endpoint, selects the OpenAI or Anthropic wire format by model ID, wraps the
 * model in createAgent with middleware (beforeModel placeholder +
 * wrapModelCall stripping SystemMessages), and streams contentBlocks via
 * agent.streamEvents().
 *
 * The agent is created once per binding (not per turn) and cached internally
 * until the model, systemPrompt, or checkpointer changes.
 */

import { HumanMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { createAgent, createMiddleware } from "langchain";
import { initChatModel } from "langchain/chat_models/universal";
import { info, error as logError } from "@dominion/common-js-logs";
import { beforeModelMiddleware } from "./context-middleware";

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
// Middleware
// ---------------------------------------------------------------------------

/**
 * Strips all SystemMessage entries from state.messages before the model
 * invocation. This prevents profile-switch contamination when the same
 * thread_id is used across different systemPrompts (per the systemPrompt
 * PERSISTS assumption confirmed by the spike test).
 */
const wrapModelCallMiddleware = createMiddleware({
  name: "StripSystemMessages",
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  wrapModelCall: async (request: any, handler: any) => {
    const state = request?.state;
    if (state?.messages && Array.isArray(state.messages)) {
      const filtered = state.messages.filter(
        (m: any) => m._getType?.() !== "system",
      );
      return handler({
        ...request,
        state: { ...state, messages: filtered },
      });
    }
    return handler(request);
  },
});

// ---------------------------------------------------------------------------
// AgentAdapter interface — backward compatibility re-export below
// ---------------------------------------------------------------------------

export interface AgentAdapter {
  /**
   * Generate a single conversational turn.
   *
   * The first invocation (or when model / systemPrompt / checkpointer
   * changes) compiles a new agent via createAgent.  Subsequent turns
   * reuse the bound agent.
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
// AgentAdapter — production implementation  (was RealLLMAdapter)
// ---------------------------------------------------------------------------

export class AgentAdapterImpl implements AgentAdapter {
  readonly baseUrl: string;

  readonly provider?: LLMProvider;

  /** Cached compiled agent from createAgent.  Re-created on profile switch. */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private boundAgent: any = null;

  private boundModelSpec = "";

  private boundSystemPrompt = "";

  private boundCheckpointer: MemorySaver | null = null;

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
    const modelSpecChanged =
      model !== this.boundModelSpec ||
      systemPrompt !== this.boundSystemPrompt ||
      checkpointer !== this.boundCheckpointer;

    if (!this.boundAgent || modelSpecChanged) {
      const bareModel = parseModelSpec(model);
      const provider = this.provider ?? inferProvider(bareModel);

      info("binding agent adapter", {
        model: bareModel,
        provider,
        systemPromptLength: systemPrompt.length,
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

      this.boundAgent = createAgent({
        model: chatModel,
        systemPrompt,
        middleware: [beforeModelMiddleware, wrapModelCallMiddleware],
        checkpointer,
      });

      this.boundModelSpec = model;
      this.boundSystemPrompt = systemPrompt;
      this.boundCheckpointer = checkpointer;
    }

    yield* this.streamFromBoundAgent(threadId, userMessage);
  }

  /** Stream contentBlocks from the pre-bound agent. */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private async *streamFromBoundAgent(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock> {
    const stream = this.boundAgent.streamEvents(
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

// ---------------------------------------------------------------------------
// Backward-compatible exports (remove after downstream callers migrate)
// ---------------------------------------------------------------------------

/** @deprecated Use AgentAdapterImpl directly. */
export const AgentAdapter = AgentAdapterImpl;

/** @deprecated Use AgentAdapterImpl. */
export const RealLLMAdapter = AgentAdapterImpl;

/** @deprecated Use AgentAdapter or AgentAdapterImpl. */
export type LLMAdapter = AgentAdapter;
