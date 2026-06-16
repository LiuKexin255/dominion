/**
 * llm.test.ts — Tests for AgentAdapterImpl.
 *
 * The adapter receives a pre-created ChatModel (from ModelProviderCache)
 * at construction time.  createAgent compiles eagerly.  generateTurn
 * only takes threadId and userMessage.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

import { AIMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { fakeModel } from "@langchain/core/testing";

import { type ContentBlock, AgentAdapterImpl } from "./llm";

// Helpers

async function collect(
  iter: AsyncIterable<ContentBlock>,
): Promise<ContentBlock[]> {
  const blocks: ContentBlock[] = [];
  for await (const block of iter) {
    blocks.push(block);
  }
  return blocks;
}

function fakeTextModel(text: string) {
  return fakeModel().respond(
    new AIMessage({
      content: [{ type: "text", text }],
    }),
  );
}

function fakeThinkingModel(reasoning: string, text: string) {
  return fakeModel().respond(
    new AIMessage({
      content: [
        { type: "reasoning", reasoning },
        { type: "text", text },
      ],
    }),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

// ===========================================================================
// AgentAdapterImpl constructor
// ===========================================================================

describe("AgentAdapterImpl constructor", () => {
  it("implements the AgentAdapter interface", () => {
    const adapter = new AgentAdapterImpl(
      fakeTextModel("hi"),
      "prompt",
      new MemorySaver(),
    );
    expect(typeof adapter.generateTurn).toBe("function");
  });

  it("generateTurn accepts 2 parameters (threadId, userMessage)", () => {
    const adapter = new AgentAdapterImpl(
      fakeTextModel("hi"),
      "prompt",
      new MemorySaver(),
    );
    expect(adapter.generateTurn.length).toBe(2);
  });
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — ContentBlock streaming
// ===========================================================================

describe("AgentAdapterImpl.generateTurn ContentBlock streaming", () => {
  it("yields text ContentBlock for text-only response", async () => {
    const model = fakeTextModel("The answer is 42.");
    const adapter = new AgentAdapterImpl(model, "prompt", new MemorySaver());
    const blocks = await collect(adapter.generateTurn("t-text", "Hi"));

    expect(blocks.length).toBeGreaterThan(0);
    const textBlocks = blocks.filter((b) => b.type === "text");
    expect(textBlocks.length).toBeGreaterThanOrEqual(1);
    expect(textBlocks[0].text).toBe("The answer is 42.");
  });

  it("yields reasoning ContentBlock before text ContentBlock", async () => {
    const model = fakeThinkingModel("Let me think...", "Done.");
    const adapter = new AgentAdapterImpl(model, "prompt", new MemorySaver());
    const blocks = await collect(adapter.generateTurn("t-think", "Why?"));

    const reasoningBlocks = blocks.filter((b) => b.type === "reasoning");
    const textBlocks = blocks.filter((b) => b.type === "text");

    expect(reasoningBlocks.length).toBeGreaterThanOrEqual(1);
    expect(textBlocks.length).toBeGreaterThanOrEqual(1);

    let lastReasoningIdx = -1;
    let firstTextIdx = -1;
    for (let i = 0; i < blocks.length; i++) {
      if (blocks[i].type === "reasoning") lastReasoningIdx = i;
      if (blocks[i].type === "text" && firstTextIdx === -1) firstTextIdx = i;
    }
    if (lastReasoningIdx >= 0 && firstTextIdx >= 0) {
      expect(lastReasoningIdx).toBeLessThan(firstTextIdx);
    }
  });

  it("all yielded blocks have type 'reasoning' or 'text'", async () => {
    const model = fakeThinkingModel("Thinking", "Text");
    const adapter = new AgentAdapterImpl(model, "prompt", new MemorySaver());
    const blocks = await collect(adapter.generateTurn("t-types", "go"));

    for (const block of blocks) {
      expect(["reasoning", "text"]).toContain(block.type);
    }
  });
});

// ===========================================================================
// AgentAdapterImpl — WrapModelCall middleware (SystemMessage stripping)
// ===========================================================================

describe("AgentAdapterImpl WrapModelCall middleware", () => {
  it("strips SystemMessages from state before model invocation", async () => {
    const model = fakeModel().respond(
      new AIMessage({ content: [{ type: "text", text: "OK" }] }),
    );
    const cp = new MemorySaver();

    const adapter = new AgentAdapterImpl(model, "system-prompt-1", cp);

    await collect(adapter.generateTurn("t-mw", "msg1"));
    await collect(adapter.generateTurn("t-mw", "msg2"));

    for (const call of model.calls) {
      const systemMsgs = call.messages.filter(
        (m: any) => m._getType?.() === "system",
      );
      expect(systemMsgs).toHaveLength(0);
    }
  });
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — error propagation
// ===========================================================================

describe("AgentAdapterImpl.generateTurn error propagation", () => {
  it("propagates error when the model responds with an error", async () => {
    const model = fakeModel().respond(new Error("SIMULATED MODEL ERROR"));
    const adapter = new AgentAdapterImpl(model, "prompt", new MemorySaver());

    await expect(
      collect(adapter.generateTurn("t-err2", "hi")),
    ).rejects.toThrow();
  });
});
