/**
 * llm.test.ts — Tests for RealLLMAdapter using fakeModel() only.
 *
 * No real API calls. All model invocations go through fakeModel()
 * from @langchain/core/testing.
 */

import { describe, expect, it } from "vitest";
import { AIMessage } from "@langchain/core/messages";
import { fakeModel } from "@langchain/core/testing";

import { type ContentBlock, inferProvider, parseModelSpec, RealLLMAdapter } from "./llm";

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
// Test 1: Text response → contentBlocks with type: "text"
// ---------------------------------------------------------------------------

describe("Text response", () => {
  it("produces contentBlocks with type: 'text'", async () => {
    const model = fakeModel().respond(
      new AIMessage({
        content: [{ type: "text", text: "Hello, world!" }],
      }),
    );

    const adapter = new RealLLMAdapter("test-model", "https://test.example.com/v1");
    const blocks = await collect(
      adapter.streamFromModel(model, "You are helpful.", [], "Hi"),
    );

    expect(blocks.length).toBeGreaterThan(0);
    for (const block of blocks) {
      if (block.type === "text") {
        expect(block.text).toBeDefined();
      }
      expect(["reasoning", "text"]).toContain(block.type);
    }
  });
});

// ---------------------------------------------------------------------------
// Test 2: Thinking + text response → contentBlocks with reasoning then text
// ---------------------------------------------------------------------------

describe("Thinking + text response", () => {
  it("produces contentBlocks with reasoning before text", async () => {
    const model = fakeModel().respond(
      new AIMessage({
        content: [
          { type: "reasoning", reasoning: "Step 1: analyze input" },
          { type: "reasoning", reasoning: "Step 2: formulate response" },
          { type: "text", text: "The answer is 42." },
        ],
      }),
    );

    const adapter = new RealLLMAdapter("test-model", "https://test.example.com/v1");
    const blocks = await collect(
      adapter.streamFromModel(model, "You are helpful.", [], "Why?"),
    );

    expect(blocks.length).toBeGreaterThanOrEqual(3);

    // Reasoning blocks should come before text blocks.
    const reasoningBlocks = blocks.filter((b) => b.type === "reasoning");
    const textBlocks = blocks.filter((b) => b.type === "text");

    expect(reasoningBlocks.length).toBeGreaterThanOrEqual(1);
    expect(textBlocks.length).toBeGreaterThanOrEqual(1);

    // Verify ordering: reasoning blocks appear before text blocks
    let lastReasoningIndex = -1;
    let firstTextIndex = -1;
    for (let i = 0; i < blocks.length; i++) {
      if (blocks[i].type === "reasoning") lastReasoningIndex = i;
      if (blocks[i].type === "text" && firstTextIndex === -1) firstTextIndex = i;
    }
    if (lastReasoningIndex >= 0 && firstTextIndex >= 0) {
      expect(lastReasoningIndex).toBeLessThan(firstTextIndex);
    }
  });
});

// ---------------------------------------------------------------------------
// Test 3: Error response → error thrown
// ---------------------------------------------------------------------------

describe("Error response", () => {
  it("throws when the model responds with an error", async () => {
    const model = fakeModel().respond(new Error("SIMULATED MODEL ERROR"));

    const adapter = new RealLLMAdapter("test-model", "https://test.example.com/v1");

    await expect(
      collect(
        adapter.streamFromModel(model, "You are helpful.", [], "Hi"),
      ),
    ).rejects.toThrow();
  });
});

// ---------------------------------------------------------------------------
// Test 4: Custom baseUrl is passed through
// ---------------------------------------------------------------------------

describe("Custom baseUrl", () => {
  it("stores the baseUrl property from the constructor", () => {
    const baseUrl = "https://custom-opencode.example.com/v1";
    const adapter = new RealLLMAdapter("opencode-go/my-model", baseUrl);

    expect(adapter.baseUrl).toBe(baseUrl);
  });

  it("stores the parsed modelName property from the constructor", () => {
    const adapter = new RealLLMAdapter("opencode-go/deepseek-v4-pro", "https://test.example.com/v1");

    expect(adapter.modelName).toBe("deepseek-v4-pro");
  });

  it("implements the LLMAdapter interface", () => {
    const adapter = new RealLLMAdapter("opencode-go/model", "https://example.com/v1");

    expect(typeof adapter.generateTurn).toBe("function");
    expect(typeof adapter.streamFromModel).toBe("function");
  });
});

describe("parseModelSpec", () => {
  it("strips the provider prefix from a {provider}/{model} spec", () => {
    expect(parseModelSpec("opencode-go/deepseek-v4-pro")).toBe("deepseek-v4-pro");
    expect(parseModelSpec("opencode-go/qwen3.7-max")).toBe("qwen3.7-max");
  });

  it("preserves slashes in the model name after the provider prefix", () => {
    expect(parseModelSpec("opencode-go/meta/llama-3")).toBe("meta/llama-3");
  });

  it("returns the input as-is when no provider prefix is present", () => {
    expect(parseModelSpec("deepseek-v4-pro")).toBe("deepseek-v4-pro");
  });

  it("returns empty string for provider-only spec", () => {
    expect(parseModelSpec("opencode-go/")).toBe("");
  });
});

describe("inferProvider", () => {
  it("routes DeepSeek models through the OpenAI-compatible endpoint", () => {
    expect(inferProvider("deepseek-v4-pro")).toBe("openai");
    expect(inferProvider("deepseek-v4-flash")).toBe("openai");
  });

  it("routes Kimi and GLM models through the OpenAI-compatible endpoint", () => {
    expect(inferProvider("kimi-k2.7-code")).toBe("openai");
    expect(inferProvider("glm-5.1")).toBe("openai");
  });

  it("routes Qwen models through the Anthropic-compatible endpoint", () => {
    expect(inferProvider("qwen3.7-max")).toBe("anthropic");
    expect(inferProvider("qwen3.7-plus")).toBe("anthropic");
    expect(inferProvider("qwen3.6-plus")).toBe("anthropic");
  });

  it("routes MiniMax models through the Anthropic-compatible endpoint", () => {
    expect(inferProvider("minimax-m3")).toBe("anthropic");
    expect(inferProvider("minimax-m2.7")).toBe("anthropic");
  });

  it("is case-insensitive", () => {
    expect(inferProvider("Qwen3.7-Max")).toBe("anthropic");
    expect(inferProvider("DeepSeek-V4-Pro")).toBe("openai");
  });
});

describe("RealLLMAdapter provider selection", () => {
  it("infers the provider from the model name by default", () => {
    const openaiAdapter = new RealLLMAdapter(
      "opencode-go/deepseek-v4-pro",
      "https://opencode.ai/zen/go/v1",
    );
    expect(openaiAdapter.provider).toBe("openai");

    const anthropicAdapter = new RealLLMAdapter(
      "opencode-go/qwen3.7-max",
      "https://opencode.ai/zen/go/v1",
    );
    expect(anthropicAdapter.provider).toBe("anthropic");
  });

  it("allows an explicit provider to override model-name inference", () => {
    const adapter = new RealLLMAdapter(
      "opencode-go/deepseek-v4-pro",
      "https://opencode.ai/zen/go/v1",
      "anthropic",
    );
    expect(adapter.provider).toBe("anthropic");
  });
});
