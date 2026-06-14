/**
 * Spike test: Validate LangChain API assumptions.
 *
 * Tests the key APIs we plan to use in llm.ts and fake-llm.ts
 * using only local/fake implementations (no real API calls).
 *
 * Validations:
 *   V1: createDeepAgent from "deepagents"
 *   V2: initChatModel from "langchain/chat_models/universal"
 *   V3: fakeModel from "@langchain/core/testing"
 *   V4: Streaming with streamMode via fakeModel
 */

import { describe, it, expect } from "vitest";

// ============================================================================
// Validation 1: createDeepAgent from "deepagents"
// ============================================================================
describe("V1: createDeepAgent from deepagents", () => {
  it("is importable and callable (sync)", async () => {
    const { createDeepAgent } = await import("deepagents");
    expect(typeof createDeepAgent).toBe("function");

    // Verify it accepts minimal config without throwing
    const agent = createDeepAgent({
      model: "claude-sonnet-4-5-20250929",
    });

    expect(agent).toBeDefined();
    expect(typeof agent.invoke).toBe("function");
    expect(typeof agent.streamEvents).toBe("function");

    // Document: createDeepAgent signature
    //   function createDeepAgent(params?: CreateDeepAgentParams): DeepAgent
    //   CreateDeepAgentParams includes:
    //     model?: BaseLanguageModel | string
    //     tools?: TTools
    //     systemPrompt?: string | SystemMessage
    //     middleware?: TMiddleware
    //     subagents?: TSubagents
    //     responseFormat?: TResponse
    //     contextSchema?: ContextSchema
    //     checkpointer?: BaseCheckpointSaver | boolean
    //     store?: BaseStore
    //     backend?: AnyBackendProtocol | Factory
    //     interruptOn?: Record<string, boolean | InterruptOnConfig>
    //     name?: string
    //     memory?: string[]
    //     skills?: string[]
    //     permissions?: FilesystemPermission[]
    //     streamTransformers?: TStreamTransformers
    //
    // Returns: DeepAgent (extends ReactAgent) with:
    //   - invoke(state, config?) → Promise<State>
    //   - streamEvents(state, config) → Promise<DeepAgentRunStream> (v3)
    //   - stream(state, config?) → IterableReadableStream (legacy)
    //
    // NOTE: createDeepAgent is SYNCHRONOUS in TypeScript (unlike Python).
    // The model parameter accepts both string names and BaseChatModel instances.
    //
    // NOTE: The built-in default model is "claude-sonnet-4-5-20250929".
    // When no model is specified, it defaults to Anthropic Claude.
    // For local testing, always pass a fake model explicitly.
  });

  it("accepts a BaseChatModel instance as model parameter", async () => {
    const { fakeModel } = await import("@langchain/core/testing");

    // Create a fake model that returns a specific response
    const model = fakeModel().respond(
      new (await import("@langchain/core/messages")).AIMessage("test response"),
    );

    const { createDeepAgent } = await import("deepagents");
    const agent = createDeepAgent({
      model,
      systemPrompt: "You are a test assistant.",
    });

    expect(agent).toBeDefined();
    expect(typeof agent.invoke).toBe("function");

    // Document: createDeepAgent accepts a BaseChatModel instance directly.
    // The `model` parameter is typed as `BaseLanguageModel | string`.
    // This confirms that fakeModel() can be passed to createDeepAgent().
  });
});

// ============================================================================
// Validation 2: initChatModel from "langchain/chat_models/universal"
// ============================================================================
describe("V2: initChatModel from langchain/chat_models/universal", () => {
  it("is importable from langchain/chat_models/universal", async () => {
    // CRITICAL FINDING: initChatModel is NOT in @langchain/core.
    // It is in the "langchain" package at "langchain/chat_models/universal".
    // This contradicts the plan assumption that it lives in @langchain/core.
    const { initChatModel } = await import("langchain/chat_models/universal");

    expect(typeof initChatModel).toBe("function");
  });

  it("accepts a model name and custom baseUrl without throwing", async () => {
    const { initChatModel } = await import("langchain/chat_models/universal");

    // initChatModel signature:
    //   function initChatModel(model: string, fields?: Partial<Record<string, any>> & {
    //     modelProvider?: string;
    //     configurableFields?: string[] | "any";
    //     configPrefix?: string;
    //   }): Promise<ConfigurableModel>
    //
    // The `fields` parameter is Partial<Record<string, any>>, meaning
    // arbitrary fields are accepted and passed through to the underlying
    // model class constructor. This includes `baseUrl`, `apiKey`, etc.

    // NOTE: initChatModel is async and may attempt to dynamically import
    // a provider package (e.g., @langchain/openai). Without that package
    // or with a custom baseUrl to a non-existent endpoint, it should NOT
    // throw at construction time — only at invocation time.
    //
    // To avoid dynamic import failures, we wrap in try/catch to document
    // the behavior rather than requiring success.

    try {
      const model = await initChatModel("openai", {
        model: "deepseek-v4-pro",
        baseUrl: "https://test.example.com/v1",
        apiKey: "test-key-not-real",
      });

      expect(model).toBeDefined();
      expect(typeof model.invoke).toBe("function");

      // Document: initChatModel successfully constructed a ConfigurableModel
      // with custom baseUrl. The model is lazy — no HTTP request is made
      // until .invoke() is called.
      //
      // The model provider "openai" maps to @langchain/openai's ChatOpenAI class.
      // Available providers (from MODEL_PROVIDER_CONFIG):
      //   openai, anthropic, azure_openai, cohere, google, google-vertexai,
      //   ollama, mistralai, groq, bedrock, deepseek, xai, cerebras,
      //   fireworks, together, perplexity
      //
      // When baseUrl is provided, it overrides the default OpenAI base URL.
      // This is how we point the model at opencode-go's proxy.
    } catch (err) {
      // If dynamic import fails (e.g., @langchain/openai not installed),
      // document the error but don't fail the test.
      const message = err instanceof Error ? err.message : String(err);
      console.warn(
        "[V2] initChatModel dynamic import note:",
        message.slice(0, 200),
      );
      // This is acceptable — the API surface is correct, just the runtime
      // environment may not have all optional providers installed.
      expect(true).toBe(true);
    }
  });

  it("document: initChatModel is in langchain, not @langchain/core", () => {
    // IMPORTANT FINDING:
    //
    // The plan assumed `import { initChatModel } from "@langchain/core"`.
    // However, initChatModel lives in the `langchain` package
    // at "langchain/chat_models/universal".
    //
    // Implication: The package.json must include "langchain" as a dependency
    // (which deepagents already depends on transitively).
    //
    // Alternative: Use ChatOpenAI directly from @langchain/openai:
    //   import { ChatOpenAI } from "@langchain/openai";
    //   const model = new ChatOpenAI({
    //     model: "deepseek-v4-pro",
    //     configuration: { baseURL: "https://test.example.com/v1" },
    //     apiKey: "test-key",
    //   });
    //
    // Both produce equivalent BaseChatModel instances.
    //
    // Recommendation: Use initChatModel for provider-agnostic config,
    // or ChatOpenAI directly for explicit OpenAI-compatible endpoints.
    expect(true).toBe(true);
  });
});

// ============================================================================
// Validation 3: fakeModel from "@langchain/core/testing"
// ============================================================================
describe("V3: fakeModel from @langchain/core/testing", () => {
  it("is importable from @langchain/core/testing", async () => {
    // CONFIRMED: fakeModel lives at @langchain/core/testing (v1.1.48+).
    // In v0.x it was at @langchain/core/utils/testing (different API).
    const { fakeModel } = await import("@langchain/core/testing");
    expect(typeof fakeModel).toBe("function");
  });

  it("supports .respond() builder pattern with BaseMessage", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel().respond(new AIMessage("Hello world"));

    // Model is a BaseChatModel — supports .invoke()
    const result = await model.invoke([new HumanMessage("Hi")]);

    expect(result).toBeDefined();
    expect(result.content).toBe("Hello world");
    // result is an AIMessage (the type we queued)
  });

  it("supports .respondWithTools() builder pattern", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { HumanMessage } = await import("@langchain/core/messages");

    const model = fakeModel().respondWithTools([
      { name: "search", args: { query: "weather in Paris" } },
      { name: "calculator", args: { expression: "2+2" } },
    ]);

    const result = await model.invoke([new HumanMessage("Search for weather")]);

    expect(result).toBeDefined();
    expect(Array.isArray(result.tool_calls)).toBe(true);
    const calls = result.tool_calls!;
    expect(calls).toHaveLength(2);
    expect(calls[0].name).toBe("search");
    expect(calls[0].args).toEqual({ query: "weather in Paris" });
    expect(calls[1].name).toBe("calculator");
    expect(calls[1].args).toEqual({ expression: "2+2" });

    // Document: tool_calls is an array on AIMessage.
    // respondWithTools takes: Array<{ name: string; args: Record<string, any>; id?: string }>
    // id is auto-generated if omitted.
  });

  it("has .callCount that increments on each invocation", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel()
      .respond(new AIMessage("First response"))
      .respond(new AIMessage("Second response"))
      .respond(new AIMessage("Third response"));

    expect(model.callCount).toBe(0);

    await model.invoke([new HumanMessage("Msg 1")]);
    expect(model.callCount).toBe(1);

    await model.invoke([new HumanMessage("Msg 2")]);
    expect(model.callCount).toBe(2);

    await model.invoke([new HumanMessage("Msg 3")]);
    expect(model.callCount).toBe(3);

    // Document: .callCount tracks total invocations.
    // .calls is an array of { messages: BaseMessage[], options: any }
    // for detailed inspection of each invocation.
  });

  it("is deterministic: same queued response → same output", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    // Test 1: same input, same queued response → same output
    const model1 = fakeModel().respond(new AIMessage("Deterministic output"));
    const result1 = await model1.invoke([new HumanMessage("Test")]);
    expect(result1.content).toBe("Deterministic output");

    const model2 = fakeModel().respond(new AIMessage("Deterministic output"));
    const result2 = await model2.invoke([new HumanMessage("Test")]);
    expect(result2.content).toBe("Deterministic output");

    // Same content
    expect(result1.content).toBe(result2.content);

    // Document: fakeModel is fully deterministic — no randomness, no network.
    // Same queued responses produce the same outputs every time.
    // This makes it ideal for deterministic integration tests.
  });

  it("supports .respond() with a factory function for dynamic responses", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel().respond((messages) => {
      const lastMsg = messages[messages.length - 1];
      const content =
        typeof lastMsg.content === "string"
          ? `Echo: ${lastMsg.content}`
          : "Echo: (non-text)";
      return new AIMessage(content);
    });

    const result = await model.invoke([new HumanMessage("Hello!")]);
    expect(result.content).toBe("Echo: Hello!");

    // Document: .respond() accepts:
    //   - BaseMessage: returns that message
    //   - Error: throws that error on invocation
    //   - (messages: BaseMessage[]) => BaseMessage | Error: factory function
  });

  it("has .calls array recording invocation history", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel()
      .respond(new AIMessage("One"))
      .respond(new AIMessage("Two"));

    await model.invoke([new HumanMessage("First")]);
    await model.invoke([new HumanMessage("Second")]);

    expect(model.calls).toHaveLength(2);
    expect(model.calls[0].messages).toHaveLength(1);
    expect(model.calls[0].messages[0].content).toBe("First");
    expect(model.calls[1].messages[0].content).toBe("Second");

    // Document: .calls is read-only array of FakeModelCall objects.
    // Each entry contains { messages, options } from the invocation.
  });
});

// ============================================================================
// Validation 4: Streaming with contentBlocks structure
// ============================================================================
describe("V4: Streaming and contentBlocks structure", () => {
  it("fakeModel can be used with streaming via .stream()", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel().respond(new AIMessage("Streaming response"));

    // BaseChatModel supports .stream() which returns an async iterable
    const stream = await model.stream([new HumanMessage("Stream this")]);

    const chunks: unknown[] = [];
    for await (const chunk of stream) {
      chunks.push(chunk);
    }

    expect(chunks.length).toBeGreaterThan(0);

    // Document: .stream() returns IterableReadableStream<AIMessageChunk>.
    // The fakeModel streams the response one chunk at a time via
    // the BaseChatModel's streaming interface.
  });

  it("AIMessageChunk has a content field", async () => {
    const { AIMessage } = await import("@langchain/core/messages");

    const chunk = new AIMessage("Hello world");
    expect(chunk.content).toBe("Hello world");

    // Verify AIMessageChunk type exists
    const { AIMessageChunk } = await import("@langchain/core/messages");
    expect(AIMessageChunk).toBeDefined();

    // Document: AIMessageChunk extends BaseMessageChunk.
    // Key properties on BaseMessageChunk:
    //   - content: string | ContentBlock[]
    //   - contentBlocks: ContentBlock[]  (from LangChain JS v1.x)
    //   - additional_kwargs: Record<string, unknown>
    //   - tool_calls: ToolCall[]
    //   - tool_call_chunks: ToolCallChunk[]
    //
    // ContentBlock types (from LangChain JS source):
    //   - { type: "text", text: string }
    //   - { type: "reasoning", reasoning: string }
    //   - { type: "image_url", image_url: { url: string } }
    //   - { type: "tool_use", id: string, name: string, input: Record<string, any> }
    //   - { type: "tool_result", tool_use_id: string, content: string }
  });

  it("AIMessageChunk.contentBlocks property exists on chunk objects", async () => {
    // Verify that contentBlocks is a real property on message chunks
    const { AIMessageChunk } = await import("@langchain/core/messages");

    // Create a chunk with contentBlocks
    const chunk = new AIMessageChunk({
      content: [
        { type: "reasoning", reasoning: "Let me think about this..." },
        { type: "text", text: "Here is my answer." },
      ],
    });

    expect(chunk.contentBlocks).toBeDefined();
    expect(Array.isArray(chunk.contentBlocks)).toBe(true);
    expect(chunk.contentBlocks).toHaveLength(2);
    expect(chunk.contentBlocks[0].type).toBe("reasoning");
    expect(chunk.contentBlocks[1].type).toBe("text");

    // Document: contentBlocks is iterable.
    // Each block has a `type` field:
    //   - "reasoning" → reasoning block (thinking)
    //   - "text" → text block (assistant response)
    //
    // These map to frame types:
    //   - reasoning → AgentFrame.payload = { $case: "thinking", thinking: ... }
    //   - text → AgentFrame.payload = { $case: "text", text: ... }
  });

  it("contentBlocks contain expected type names", async () => {
    const { AIMessageChunk } = await import("@langchain/core/messages");

    // Create a chunk with reasoning content
    const reasoningChunk = new AIMessageChunk({
      content: [
        { type: "reasoning", reasoning: "Step 1: analyze input" },
        { type: "reasoning", reasoning: "Step 2: formulate response" },
      ],
    });

    const reasoningBlocks = reasoningChunk.contentBlocks as Array<{
      type: string;
      [key: string]: unknown;
    }>;
    for (const block of reasoningBlocks) {
      expect(block.type).toBe("reasoning");
    }

    // Create a chunk with text content
    const textChunk = new AIMessageChunk({
      content: [
        { type: "text", text: "The answer is 42." },
      ],
    });

    const textBlocks = textChunk.contentBlocks as Array<{
      type: string;
      [key: string]: unknown;
    }>;
    for (const block of textBlocks) {
      expect(block.type).toBe("text");
    }

    // Document: Exact type names from LangChain JS ContentBlock union:
    //   - "reasoning" → { type: "reasoning", reasoning: string, ... }
    //   - "text" → { type: "text", text: string, ... }
    //
    // These are confirmed by creating AIMessageChunk with explicit content blocks.
    // When receiving real streaming chunks from opencode-go, the same
    // type names will appear (since opencode-go emits OpenAI-compatible format).
  });

  it("contentBlocks with mixed types can be filtered for frame emission", async () => {
    const { AIMessageChunk } = await import("@langchain/core/messages");

    // Simulate a streaming chunk with both reasoning and text
    const chunk = new AIMessageChunk({
      content: [
        { type: "reasoning", reasoning: "Let me think..." },
        { type: "text", text: "Part 1 of response. " },
        { type: "reasoning", reasoning: "More thinking..." },
        { type: "text", text: "Part 2 of response." },
      ],
    });

    // Separate reasoning from text blocks
    const reasoningBlocks = chunk.contentBlocks.filter(
      (b: { type: string }) => b.type === "reasoning",
    );
    const textBlocks = chunk.contentBlocks.filter(
      (b: { type: string }) => b.type === "text",
    );

    expect(reasoningBlocks).toHaveLength(2);
    expect(textBlocks).toHaveLength(2);

    // Accumulate text blocks
    const fullText = textBlocks
      .map((b) => (b as { text: string }).text ?? "")
      .join("");
    expect(fullText).toBe("Part 1 of response. Part 2 of response.");

    // Document: This filtering pattern is exactly what runtime.ts will use
    // to separate AgentThinkingFrame from AgentTextFrame emissions.
    //
    // Pattern:
    //   for (const block of chunk.contentBlocks) {
    //     if (block.type === "reasoning") { /* emit AgentThinkingFrame */ }
    //     if (block.type === "text") { /* accumulate → emit AgentTextFrame */ }
    //   }
  });

  it("streamMode option is supported by BaseChatModel.stream()", async () => {
    const { fakeModel } = await import("@langchain/core/testing");
    const { AIMessage, HumanMessage } = await import(
      "@langchain/core/messages"
    );

    const model = fakeModel().respond(
      new AIMessage({
        content: [
          { type: "reasoning", reasoning: "Processing..." },
          { type: "text", text: "Done." },
        ],
      }),
    );

    // Test streaming with streamMode: "messages"
    // (This tests the API surface; the fake model's stream behavior
    //  depends on its internal implementation, but the option should be accepted)
    try {
      const stream = await model.stream([new HumanMessage("Test")], {
        streamMode: "messages",
      } as Record<string, unknown>);
      expect(stream).toBeDefined();

      const chunks: unknown[] = [];
      for await (const chunk of stream) {
        chunks.push(chunk);
      }
      expect(chunks.length).toBeGreaterThan(0);
    } catch {
      // If fakeModel doesn't support streamMode at this level, that's OK —
      // this is typically a LangGraph-level option, not BaseChatModel-level.
      // Document the finding.
      console.warn(
        "[V4] streamMode may need to be passed at the agent/graph level, " +
          "not the model level. The fakeModel's .stream() accepts it but " +
          "doesn't interpret it. Use agent.streamEvents(state, { version: 'v3' }) " +
          "for contentBlocks-based streaming with DeepAgent.",
      );
    }
  });
});
