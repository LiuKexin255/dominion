/**
 * fake-llm.ts — Deterministic fake LLM adapter for testing.
 *
 * Implements the same LLMAdapter interface as llm.ts but returns
 * hardcoded responses without any real API calls or randomness.
 * Used as a BUILD-level replacement for RealLLMAdapter in tests.
 */

import { MemorySaver } from "@langchain/langgraph";

import type { ContentBlock, LLMAdapter } from "./llm";

// ---------------------------------------------------------------------------
// FakeLlmAdapter — deterministic, no-network implementation
// ---------------------------------------------------------------------------

export class FakeLlmAdapter implements LLMAdapter {
  async *generateTurn(
    _model: string,
    _systemPrompt: string,
    _threadId: string,
    userMessage: string,
    _checkpointer: MemorySaver,
    _providerSecret: string,
  ): AsyncIterable<ContentBlock> {
    yield { type: "reasoning", reasoning: "Processing your message..." };
    yield {
      type: "text",
      text: `Hello! This is a simulated response. You said: ${userMessage}`,
    };
  }
}
