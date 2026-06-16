/**
 * llm.test.ts — Tests for AgentAdapterImpl.
 *
 * Mocks initChatModel so createAgent compiles with a fakeModel — no real
 * API calls. Validates: static systemPrompt caching, ContentBlock streaming,
 * WrapModelCall middleware (SystemMessage stripping), and error propagation.
 *
 * Pure functions (parseModelSpec, inferProvider) are tested without mocks.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";

// Mock initChatModel BEFORE importing AgentAdapterImpl so the module-level
// import in llm.ts resolves to the mock. createAgent and createMiddleware
// from "langchain" remain real — only the model factory is faked.
vi.mock("langchain/chat_models/universal", () => ({
  initChatModel: vi.fn(),
}));

import { AIMessage } from "@langchain/core/messages";
import { MemorySaver } from "@langchain/langgraph";
import { fakeModel } from "@langchain/core/testing";
import { initChatModel } from "langchain/chat_models/universal";

import {
  type ContentBlock,
  type AgentAdapter,
  AgentAdapterImpl,
  inferProvider,
  parseModelSpec,
} from "./llm";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Cast: initChatModel's real return type (ConfigurableModel) is structurally
// incompatible with FakeBuiltModel at the TS level, but at runtime both
// satisfy the BaseChatModel interface that createAgent expects.
const mockedInitChatModel = initChatModel as unknown as {
  (model: string, options?: Record<string, unknown>): Promise<unknown>;
} & {
  mockClear(): typeof mockedInitChatModel;
  mockResolvedValue(value: unknown): typeof mockedInitChatModel;
  mockRejectedValue(value: unknown): typeof mockedInitChatModel;
  toHaveBeenCalledWith(...args: unknown[]): boolean;
  toHaveBeenCalledTimes(n: number): boolean;
  mock: { calls: unknown[][] };
};

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

/** Create a fakeModel that responds with a text-only AIMessage. */
function fakeTextModel(text: string) {
  return fakeModel().respond(
    new AIMessage({
      content: [{ type: "text", text }],
    }),
  );
}

/** Create a fakeModel that responds with reasoning + text AIMessage. */
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
// Pure function: parseModelSpec
// ===========================================================================

describe("parseModelSpec", () => {
  it("strips the provider prefix from a {provider}/{model} spec", () => {
    expect(parseModelSpec("opencode-go/deepseek-v4-pro")).toBe(
      "deepseek-v4-pro",
    );
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

// ===========================================================================
// Pure function: inferProvider
// ===========================================================================

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

// ===========================================================================
// ContentBlock type validation
// ===========================================================================

describe("ContentBlock type", () => {
  it("reasoning block has type 'reasoning' with reasoning string", () => {
    const block: ContentBlock = {
      type: "reasoning",
      reasoning: "Step 1",
    };
    expect(block.type).toBe("reasoning");
    expect(block).toHaveProperty("reasoning");
  });

  it("text block has type 'text' with text string", () => {
    const block: ContentBlock = { type: "text", text: "Hello" };
    expect(block.type).toBe("text");
    expect(block).toHaveProperty("text");
  });
});

// ===========================================================================
// AgentAdapterImpl constructor & interface
// ===========================================================================

describe("AgentAdapterImpl constructor", () => {
  it("stores the baseUrl property", () => {
    const baseUrl = "https://custom-opencode.example.com/v1";
    const adapter = new AgentAdapterImpl(baseUrl);
    expect(adapter.baseUrl).toBe(baseUrl);
  });

  it("does not set provider when not specified", () => {
    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    expect(adapter.provider).toBeUndefined();
  });

  it("stores explicit provider from constructor", () => {
    const adapter = new AgentAdapterImpl(
      "https://test.example.com/v1",
      "anthropic",
    );
    expect(adapter.provider).toBe("anthropic");
  });

  it("implements the AgentAdapter interface", () => {
    const adapter: AgentAdapter = new AgentAdapterImpl("https://example.com/v1");
    expect(typeof adapter.generateTurn).toBe("function");
  });

  it("generateTurn accepts 6 parameters matching the AgentAdapter contract", () => {
    const adapter = new AgentAdapterImpl("https://example.com/v1");
    expect(adapter.generateTurn.length).toBe(6);
  });
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — caching & static systemPrompt
// ===========================================================================

describe("AgentAdapterImpl.generateTurn caching", () => {
  it("calls initChatModel only once when model + systemPrompt are unchanged (static systemPrompt)", async () => {
    const model = fakeTextModel("Hello");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await collect(
      adapter.generateTurn("m1", "prompt-a", "t1", "msg1", cp, "key"),
    );
    await collect(
      adapter.generateTurn("m1", "prompt-a", "t1", "msg2", cp, "key"),
    );

    expect(mockedInitChatModel).toHaveBeenCalledTimes(1);
  });

  it("re-creates agent when systemPrompt changes (profile switch)", async () => {
    const model = fakeTextModel("Hello");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await collect(
      adapter.generateTurn("m1", "prompt-a", "t1", "msg1", cp, "key"),
    );
    await collect(
      adapter.generateTurn("m1", "prompt-b", "t1", "msg2", cp, "key"),
    );

    expect(mockedInitChatModel).toHaveBeenCalledTimes(2);
  });

  it("re-creates agent when model changes", async () => {
    const model = fakeTextModel("Hello");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await collect(
      adapter.generateTurn("m1", "prompt", "t1", "msg1", cp, "key"),
    );
    await collect(
      adapter.generateTurn("m2", "prompt", "t1", "msg2", cp, "key"),
    );

    expect(mockedInitChatModel).toHaveBeenCalledTimes(2);
  });

  it("re-creates agent when checkpointer changes", async () => {
    const model = fakeTextModel("Hello");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp1 = new MemorySaver();
    const cp2 = new MemorySaver();

    await collect(
      adapter.generateTurn("m1", "prompt", "t1", "msg1", cp1, "key"),
    );
    await collect(
      adapter.generateTurn("m1", "prompt", "t1", "msg2", cp2, "key"),
    );

    expect(mockedInitChatModel).toHaveBeenCalledTimes(2);
  });

  it("strips provider prefix via parseModelSpec before initChatModel", async () => {
    const model = fakeTextModel("Hello");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await collect(
      adapter.generateTurn(
        "opencode-go/deepseek-v4",
        "prompt",
        "t1",
        "hi",
        cp,
        "key",
      ),
    );

    expect(mockedInitChatModel).toHaveBeenCalledWith(
      "deepseek-v4",
      expect.objectContaining({ apiKey: "key" }),
    );
  });
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — ContentBlock streaming
// ===========================================================================

describe("AgentAdapterImpl.generateTurn ContentBlock streaming", () => {
  it("yields text ContentBlock for text-only response", async () => {
    const model = fakeTextModel("The answer is 42.");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("m", "prompt", "t-text", "Hi", cp, "key"),
    );

    expect(blocks.length).toBeGreaterThan(0);
    const textBlocks = blocks.filter((b) => b.type === "text");
    expect(textBlocks.length).toBeGreaterThanOrEqual(1);
    expect(textBlocks[0].text).toBe("The answer is 42.");
  });

  it("yields reasoning ContentBlock before text ContentBlock", async () => {
    const model = fakeThinkingModel("Let me think...", "Done.");
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("m", "prompt", "t-think", "Why?", cp, "key"),
    );

    const reasoningBlocks = blocks.filter((b) => b.type === "reasoning");
    const textBlocks = blocks.filter((b) => b.type === "text");

    expect(reasoningBlocks.length).toBeGreaterThanOrEqual(1);
    expect(textBlocks.length).toBeGreaterThanOrEqual(1);

    // Reasoning must come before text.
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
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();
    const blocks = await collect(
      adapter.generateTurn("m", "prompt", "t-types", "go", cp, "key"),
    );

    for (const block of blocks) {
      expect(["reasoning", "text"]).toContain(block.type);
    }
  });
});

// ===========================================================================
// AgentAdapterImpl.generateTurn — WrapModelCall middleware (SystemMessage stripping)
// ===========================================================================

describe("AgentAdapterImpl WrapModelCall middleware", () => {
  it("strips SystemMessages from state before model invocation", async () => {
    // The fake model records what messages it actually received.
    const model = fakeModel().respond(
      new AIMessage({ content: [{ type: "text", text: "OK" }] }),
    );
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    // Turn 1: createAgent injects systemPrompt as a SystemMessage into state.
    await collect(
      adapter.generateTurn("m", "system-prompt-1", "t-mw", "msg1", cp, "key"),
    );

    // Turn 2: middleware should strip prior SystemMessages before model call.
    await collect(
      adapter.generateTurn("m", "system-prompt-1", "t-mw", "msg2", cp, "key"),
    );

    // The model must NOT have received any SystemMessages — middleware strips them.
    for (const call of model.calls) {
      const systemMsgs = call.messages.filter(
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
  it("propagates error when initChatModel throws", async () => {
    mockedInitChatModel.mockRejectedValue(new Error("SIMULATED INIT ERROR"));

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await expect(
      collect(
        adapter.generateTurn("m", "prompt", "t-err", "hi", cp, "key"),
      ),
    ).rejects.toThrow("SIMULATED INIT ERROR");
  });

  it("propagates error when the model responds with an error", async () => {
    const model = fakeModel().respond(new Error("SIMULATED MODEL ERROR"));
    mockedInitChatModel.mockResolvedValue(model);

    const adapter = new AgentAdapterImpl("https://test.example.com/v1");
    const cp = new MemorySaver();

    await expect(
      collect(
        adapter.generateTurn("m", "prompt", "t-err2", "hi", cp, "key"),
      ),
    ).rejects.toThrow();
  });
});
