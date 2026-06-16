/**
 * fake-llm.test.ts — Tests for FakeLlmAdapter under the AgentAdapter contract.
 *
 * Validates deterministic behavior: reasoning block before text block,
 * same input always produces same output, ContentBlock type conformance.
 */

import { describe, expect, it } from "vitest";
import { MemorySaver } from "@langchain/langgraph";

import type { AgentAdapter, ContentBlock } from "./llm";
import { FakeLlmAdapter } from "./fake-llm";

async function collect(
  iter: AsyncIterable<ContentBlock>,
): Promise<ContentBlock[]> {
  const blocks: ContentBlock[] = [];
  for await (const block of iter) {
    blocks.push(block);
  }
  return blocks;
}

describe("FakeLlmAdapter ContentBlock ordering", () => {
  it("yields reasoning block before text block", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("m", "sys", "t-order", "Hello", cp, ""),
    );

    expect(blocks).toHaveLength(2);
    expect(blocks[0].type).toBe("reasoning");
    expect(blocks[1].type).toBe("text");
  });

  it("reasoning block contains processing message", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("m", "sys", "t-r", "Hello", cp, ""),
    );

    expect(blocks[0]).toEqual({
      type: "reasoning",
      reasoning: "Processing your message...",
    });
  });

  it("text block echoes user message", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();
    const userMessage = "What is the capital of France?";
    const blocks = await collect(
      adapter.generateTurn("m", "sys", "t-text", userMessage, cp, ""),
    );

    expect(blocks[1]).toEqual({
      type: "text",
      text: `Hello! This is a simulated response. You said: ${userMessage}`,
    });
  });
});

describe("FakeLlmAdapter deterministic output", () => {
  it("same input produces identical output across calls", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();

    const blocks1 = await collect(
      adapter.generateTurn("model-a", "prompt", "t-det-1", "Test", cp, "key-1"),
    );
    const blocks2 = await collect(
      adapter.generateTurn("model-a", "prompt", "t-det-1", "Test", cp, "key-1"),
    );

    expect(blocks1).toEqual(blocks2);
  });

  it("output is unaffected by providerSecret value", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();

    const blocksA = await collect(
      adapter.generateTurn("m", "p", "t-secret-1", "Hi", cp, ""),
    );
    const blocksB = await collect(
      adapter.generateTurn("m", "p", "t-secret-1", "Hi", cp, "some-api-key"),
    );

    expect(blocksA).toEqual(blocksB);
  });

  it("different user messages produce different text blocks", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();

    const blocks1 = await collect(
      adapter.generateTurn("m", "p", "t-diff-1", "message-one", cp, ""),
    );
    const blocks2 = await collect(
      adapter.generateTurn("m", "p", "t-diff-2", "message-two", cp, ""),
    );

    const text1 = blocks1.find((b) => b.type === "text");
    const text2 = blocks2.find((b) => b.type === "text");
    expect(text1).not.toEqual(text2);
  });
});

describe("FakeLlmAdapter AgentAdapter contract", () => {
  it("satisfies the AgentAdapter interface", () => {
    const adapter: AgentAdapter = new FakeLlmAdapter();
    expect(typeof adapter.generateTurn).toBe("function");
  });

  it("generateTurn accepts 6 parameters", () => {
    const adapter = new FakeLlmAdapter();
    expect(adapter.generateTurn.length).toBe(6);
  });

  it("generateTurn returns an AsyncIterable<ContentBlock>", async () => {
    const adapter = new FakeLlmAdapter();
    const cp = new MemorySaver();
    const result = adapter.generateTurn("", "", "", "", cp, "");
    expect(typeof result[Symbol.asyncIterator]).toBe("function");

    const blocks = await collect(result);
    for (const block of blocks) {
      expect(["reasoning", "text"]).toContain(block.type);
    }
  });
});
