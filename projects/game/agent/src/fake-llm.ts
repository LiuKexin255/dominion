/**
 * fake-llm.ts — Deterministic fake LLM adapter for testing.
 *
 * Implements the same LLMAdapter interface as llm.ts but returns
 * hardcoded responses without any real API calls or randomness.
 * Used as a BUILD-level replacement for RealLLMAdapter in tests.
 */

import type { BaseMessage } from "@langchain/core/messages";

import type { ContentBlock, LLMAdapter } from "./llm";

// ---------------------------------------------------------------------------
// FakeLlmAdapter — deterministic, no-network implementation
// ---------------------------------------------------------------------------

export class FakeLlmAdapter implements LLMAdapter {
  async *generateTurn(
    _systemPrompt: string,
    _history: BaseMessage[],
    userMessage: string,
    _providerSecret: string,
  ): AsyncIterable<ContentBlock> {
    yield { type: "reasoning", reasoning: "Processing your message..." };
    yield {
      type: "text",
      text: `Hello! This is a simulated response. You said: ${userMessage}`,
    };
  }
}
