/**
 * fake-llm.test.ts — Tests for FakeLlmAdapter.
 *
 * Validates deterministic behavior: no randomness, no real API calls,
 * same input always produces same output. Updated for 6-param contract.
 */

import { describe, expect, it } from "vitest";
import { MemorySaver } from "@langchain/langgraph";

import type { ContentBlock } from "./llm";
import { FakeLlmAdapter } from "./fake-llm";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Collect every ContentBlock from an async iterable. */
async function collect(
  iter: AsyncIterable<ContentBlock>,
): Promise<ContentBlock[]> {
  const blocks: ContentBlock[] = [];
  for await (const block of iter) {
    blocks.push(block);
  }
  return blocks;
}

// ---------------------------------------------------------------------------
// Test 1: Returns deterministic thinking + text contentBlocks
// ---------------------------------------------------------------------------

describe("ContentBlock structure", () => {
  it("yields exactly 2 blocks: reasoning then text", async () => {
    const adapter = new FakeLlmAdapter();
    const checkpointer = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("test-model", "You are helpful.", "thread-1", "Hello", checkpointer, ""),
    );

    expect(blocks).toHaveLength(2);
    expect(blocks[0]).toEqual({
      type: "reasoning",
      reasoning: "Processing your message...",
    });
    expect(blocks[1]).toEqual({
      type: "text",
      text: "Hello! This is a simulated response. You said: Hello",
    });
  });
});

// ---------------------------------------------------------------------------
// Test 2: Response includes echo of input message
// ---------------------------------------------------------------------------

describe("User message echo", () => {
  it("includes the user message in the text block", async () => {
    const adapter = new FakeLlmAdapter();
    const checkpointer = new MemorySaver();
    const userMessage = "What is the capital of France?";

    const blocks = await collect(
      adapter.generateTurn("test-model", "You are helpful.", "thread-2", userMessage, checkpointer, ""),
    );

    const textBlock = blocks.find(
      (b): b is { type: "text"; text: string } => b.type === "text",
    );
    expect(textBlock).toBeDefined();
    expect(textBlock?.text).toContain(userMessage);
  });

  it("preserves special characters in the echo", async () => {
    const adapter = new FakeLlmAdapter();
    const checkpointer = new MemorySaver();
    const userMessage = "a <b>bold</b> & \"quoted\"";

    const blocks = await collect(
      adapter.generateTurn("test-model", "You are helpful.", "thread-3", userMessage, checkpointer, ""),
    );

    const textBlock = blocks.find(
      (b): b is { type: "text"; text: string } => b.type === "text",
    );
    expect(textBlock).toBeDefined();
    expect(textBlock?.text).toContain(userMessage);
  });
});

// ---------------------------------------------------------------------------
// Test 3: Same input always produces same output (idempotent)
// ---------------------------------------------------------------------------

describe("Idempotent output", () => {
  it("produces identical output for the same input", async () => {
    const adapter = new FakeLlmAdapter();
    const systemPrompt = "You are a test assistant.";
    const userMessage = "Test message";
    const checkpointer = new MemorySaver();

    const blocks1 = await collect(
      adapter.generateTurn("model-a", systemPrompt, "thread-4", userMessage, checkpointer, "secret-1"),
    );
    const blocks2 = await collect(
      adapter.generateTurn("model-a", systemPrompt, "thread-4", userMessage, checkpointer, "secret-2"),
    );

    expect(blocks1).toEqual(blocks2);
  });

  it("is unaffected by providerSecret value", async () => {
    const adapter = new FakeLlmAdapter();
    const systemPrompt = "You are helpful.";
    const userMessage = "Hi";
    const checkpointer = new MemorySaver();

    const blocksA = await collect(
      adapter.generateTurn("m", systemPrompt, "t", userMessage, checkpointer, ""),
    );
    const blocksB = await collect(
      adapter.generateTurn("m", systemPrompt, "t", userMessage, checkpointer, "some-api-key"),
    );

    expect(blocksA).toEqual(blocksB);
  });
});

// ---------------------------------------------------------------------------
// Test 4: Implements LLMAdapter interface correctly
// ---------------------------------------------------------------------------

describe("LLMAdapter interface conformance", () => {
  it("has a generateTurn method", () => {
    const adapter = new FakeLlmAdapter();
    expect(typeof adapter.generateTurn).toBe("function");
  });

  it("generateTurn returns an AsyncIterable", () => {
    const adapter = new FakeLlmAdapter();
    const checkpointer = new MemorySaver();
    const result = adapter.generateTurn("", "", "", "", checkpointer, "");
    expect(result).toBeDefined();
    expect(typeof result[Symbol.asyncIterator]).toBe("function");
  });

  it("accepts all parameters matching the LLMAdapter interface", () => {
    const adapter = new FakeLlmAdapter();
    expect(adapter.generateTurn.length).toBe(6);
  });
});
