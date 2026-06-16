/**
 * llm.ts — AgentAdapter wrapping LangChain createAgent for text dialog.
 *
 * The adapter receives a pre-created ChatModel (from ModelProviderCache),
 * a systemPrompt, and a checkpointer at construction time.  The compiled
 * agent is created eagerly in the constructor.  generateTurn only needs
 * the threadId and userMessage.
 */

import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { createAgent, createMiddleware } from "langchain";
import { info } from "@dominion/common-js-logs";
import { beforeModelMiddleware } from "./context-middleware";
import type { ChatModel } from "./model-provider";

// ---------------------------------------------------------------------------
// ContentBlock types (discriminated union matching LangChain block structure)
// ---------------------------------------------------------------------------

export type ContentBlock =
  | { type: "reasoning"; reasoning: string }
  | { type: "text"; text: string };

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

/**
 * Strips all SystemMessage entries from state.messages before the model
 * invocation.  This prevents profile-switch contamination when the same
 * thread_id is used across different systemPrompts.
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
// AgentAdapter interface
// ---------------------------------------------------------------------------

export interface AdapterStateSnapshot {
  values: { messages?: BaseMessage[] };
  createdAt?: string;
}

export interface AgentAdapter {
  /**
   * Generate a single conversational turn.
   *
   * The adapter was compiled at construction time with a specific model,
   * systemPrompt, and checkpointer.  Only the threadId and userMessage
   * vary per turn.
   *
   * @param threadId    - Stable checkpoint thread identifier (sessionId).
   * @param userMessage - The user's message for this turn.
   * @returns Async iterable of ContentBlock in streaming order.
   */
  generateTurn(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock>;

  /**
   * Read the checkpoint state for a thread.
   *
   * Uses the adapter's own compiled graph so the checkpoint — which was
   * written by the same graph — is correctly deserialised.  Returns null
   * when no checkpoint exists for the thread.
   */
  getState(threadId: string): Promise<AdapterStateSnapshot | null>;

  /** Optional cleanup hook called when the adapter is unbound. */
  cleanup?(): void;
}

// ---------------------------------------------------------------------------
// AdapterFactory — used by SessionAgent to create adapter instances
//
// The factory receives a lazy getProvider callback rather than a pre-fetched
// ChatModel.  The production factory calls getProvider() to obtain the shared
// model; the test factory ignores it entirely.
// ---------------------------------------------------------------------------

export type AdapterFactory = (
  getProvider: () => Promise<ChatModel>,
  systemPrompt: string,
  checkpointer: MemorySaver,
) => Promise<AgentAdapter>;

// ---------------------------------------------------------------------------
// AgentAdapterImpl — production implementation
// ---------------------------------------------------------------------------

export class AgentAdapterImpl implements AgentAdapter {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private readonly agent: any;

  constructor(
    chatModel: ChatModel,
    systemPrompt: string,
    checkpointer: MemorySaver,
  ) {
    info("compiling agent adapter", {
      systemPromptLength: systemPrompt.length,
    });

    this.agent = createAgent({
      model: chatModel,
      systemPrompt,
      middleware: [beforeModelMiddleware, wrapModelCallMiddleware],
      checkpointer,
    });
  }

  async *generateTurn(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock> {
    yield* this.streamFromAgent(threadId, userMessage);
  }

  async getState(threadId: string): Promise<AdapterStateSnapshot | null> {
    const snapshot = await this.agent.getState({
      configurable: { thread_id: threadId },
    });
    if (!snapshot) return null;
    return {
      values: snapshot.values ?? {},
      createdAt: snapshot.createdAt,
    };
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private async *streamFromAgent(
    threadId: string,
    userMessage: string,
  ): AsyncIterable<ContentBlock> {
    const stream = this.agent.streamEvents(
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
