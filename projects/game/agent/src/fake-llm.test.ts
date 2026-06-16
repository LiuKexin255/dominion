/**
 * fake-llm.test.ts — Tests for FakeLlmAdapter under the AgentAdapter contract.
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
    const adapter = new FakeLlmAdapter(new MemorySaver());
    const blocks = await collect(adapter.generateTurn("t-order", "Hello"));

    expect(blocks).toHaveLength(2);
    expect(blocks[0].type).toBe("reasoning");
    expect(blocks[1].type).toBe("text");
  });

  it("reasoning block contains processing message", async () => {
    const adapter = new FakeLlmAdapter(new MemorySaver());
    const blocks = await collect(adapter.generateTurn("t-r", "Hello"));

    expect(blocks[0]).toEqual({
      type: "reasoning",
      reasoning: "Processing your message...",
    });
  });

  it("text block echoes user message", async () => {
    const adapter = new FakeLlmAdapter(new MemorySaver());
    const userMessage = "What is the capital of France?";
    const blocks = await collect(adapter.generateTurn("t-text", userMessage));

    expect(blocks[1]).toEqual({
      type: "text",
      text: `Hello! This is a simulated response. You said: ${userMessage}`,
    });
  });
});

describe("FakeLlmAdapter deterministic output", () => {
  it("same input produces identical output across calls", async () => {
    const cp = new MemorySaver();
    const adapter = new FakeLlmAdapter(cp);

    const blocks1 = await collect(adapter.generateTurn("t-det-1", "Test"));
    const blocks2 = await collect(adapter.generateTurn("t-det-1", "Test"));

    expect(blocks1).toEqual(blocks2);
  });

  it("different user messages produce different text blocks", async () => {
    const adapter = new FakeLlmAdapter(new MemorySaver());

    const blocks1 = await collect(adapter.generateTurn("t-diff-1", "message-one"));
    const blocks2 = await collect(adapter.generateTurn("t-diff-2", "message-two"));

    const text1 = blocks1.find((b) => b.type === "text");
    const text2 = blocks2.find((b) => b.type === "text");
    expect(text1).not.toEqual(text2);
  });
});

describe("FakeLlmAdapter AgentAdapter contract", () => {
  it("satisfies the AgentAdapter interface", () => {
    const adapter: AgentAdapter = new FakeLlmAdapter(new MemorySaver());
    expect(typeof adapter.generateTurn).toBe("function");
  });

  it("generateTurn accepts 2 parameters", () => {
    const adapter = new FakeLlmAdapter(new MemorySaver());
    expect(adapter.generateTurn.length).toBe(2);
  });

  it("generateTurn returns an AsyncIterable<ContentBlock>", async () => {
    const adapter = new FakeLlmAdapter(new MemorySaver());
    const result = adapter.generateTurn("", "");
    expect(typeof result[Symbol.asyncIterator]).toBe("function");

    const blocks = await collect(result);
    for (const block of blocks) {
      expect(["reasoning", "text"]).toContain(block.type);
    }
  });
});
