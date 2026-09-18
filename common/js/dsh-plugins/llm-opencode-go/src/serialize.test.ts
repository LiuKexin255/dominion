import { describe, expect, it } from "vitest";

import {
  CallId,
  createAssistantMessage,
  createToolResultMessage,
  createUserMessage,
  LlmError,
} from "@deepseek-ai/dsh-llm";
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";

import { serializeRequest } from "./serialize.js";

function baseOptions(overrides: Partial<GenerateOptions>): GenerateOptions {
  return {
    provider: "opencode-go",
    model: "glm-5.3",
    messages: [],
    ...overrides,
  };
}

// Real tool-surface shapes (common/js/dsh-plugins/saolei/src/index.ts):
// saolei_operate's dual-form parameters schema (single op via type/x/y, or
// a batch via the operations array) plus a no-argument tool.
const OPERATE_PARAMETERS = {
  type: {
    type: "string",
    enum: ["click", "flag", "chord"],
    description: "Single form: the operation type (mutually exclusive with operations)",
  },
  x: { type: "integer", description: "Single form: column index (0-based)" },
  y: { type: "integer", description: "Single form: row index (0-based)" },
  operations: {
    type: "array",
    description: "Batch form: ordered cell operations (mutually exclusive with type/x/y)",
    items: {
      type: "object",
      additionalProperties: false,
      properties: {
        type: { type: "string", enum: ["click", "flag", "chord"], required: true },
        x: { type: "integer", required: true },
        y: { type: "integer", required: true },
      },
    },
  },
};

const TOOL_FIXTURES = [
  {
    name: "saolei_operate",
    description: "Execute one or more minesweeper cell operations IN ORDER.",
    parameters: OPERATE_PARAMETERS,
  },
  {
    name: "saolei_init",
    description: "Start a new minesweeper game.",
    parameters: {},
  },
];

describe("Chat Completions request serialization", () => {
  it("maps a multi-turn system/user/assistant round-trip in history order", () => {
    const request = serializeRequest(
      baseOptions({
        system: "You are a game table assistant.",
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "M1" }],
            source: { kind: "user" },
          }),
          createAssistantMessage({
            content: [
              { type: "reasoning", text: "chain of thought" },
              { type: "text", text: "R1" },
            ],
            source: { provider: "opencode-go", model: "glm-5.3" },
          }),
          createUserMessage({
            content: [{ type: "text", text: "M2" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    // Full-body assertion guards against field drift, not just the
    // presence of the mapped rows.
    expect(request).toEqual({
      model: "glm-5.3",
      messages: [
        { role: "system", content: "You are a game table assistant." },
        { role: "user", content: "M1" },
        { role: "assistant", content: "R1" },
        { role: "user", content: "M2" },
      ],
      stream: true,
      stream_options: { include_usage: true },
    });
  });

  it("omits the system message when no system prompt is set", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "hi" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request.messages[0]?.role).toBe("user");
    expect(request).toEqual({
      model: "glm-5.3",
      messages: [{ role: "user", content: "hi" }],
      stream: true,
      stream_options: { include_usage: true },
    });
  });

  it("never replays assistant reasoning content", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createAssistantMessage({
            content: [
              { type: "reasoning", text: "only thinking this turn" },
              { type: "text", text: "answer" },
            ],
            source: { provider: "opencode-go", model: "glm-5.3" },
          }),
        ],
      }),
    );

    // The reasoning text must not surface anywhere on the wire.
    expect(JSON.stringify(request)).not.toContain("only thinking this turn");
    expect(request.messages).toEqual([{ role: "assistant", content: "answer" }]);
  });

  it("maps an assistant tool-call and its tool result to paired wire messages", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "Start a game." }],
            source: { kind: "user" },
          }),
          createAssistantMessage({
            content: [
              { type: "text", text: "Opening the board." },
              { type: "tool-call", id: CallId("call_1"), name: "saolei_init", arguments: "{}" },
            ],
            source: { provider: "opencode-go", model: "glm-5.3" },
          }),
          createToolResultMessage({
            callId: CallId("call_1"),
            content: [{ type: "text", text: "new game started\nboard size 9*9" }],
            isError: false,
          }),
        ],
      }),
    );

    expect(request.messages).toEqual([
      { role: "user", content: "Start a game." },
      {
        role: "assistant",
        content: "Opening the board.",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: { name: "saolei_init", arguments: "{}" },
          },
        ],
      },
      {
        role: "tool",
        tool_call_id: "call_1",
        content: "new game started\nboard size 9*9",
      },
    ]);
  });

  it("renders an empty tool result as the (no output) placeholder", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createToolResultMessage({
            callId: CallId("call_9"),
            content: [],
            isError: false,
          }),
        ],
      }),
    );

    expect(request.messages).toEqual([
      { role: "tool", tool_call_id: "call_9", content: "(no output)" },
    ]);
  });

  it("maps temperature, maxTokens, and stop to the chat fields", () => {
    const request = serializeRequest(
      baseOptions({
        temperature: 0.5,
        maxTokens: 256,
        stop: ["END"],
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "hi" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request.temperature).toBe(0.5);
    expect(request.max_tokens).toBe(256);
    expect(request.stop).toEqual(["END"]);
  });

  it("maps tool schemas to the nested chat completions function tools array", () => {
    const request = serializeRequest(
      baseOptions({
        tools: TOOL_FIXTURES,
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "Start a game." }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request.tools).toEqual([
      {
        type: "function",
        function: {
          name: "saolei_operate",
          description: "Execute one or more minesweeper cell operations IN ORDER.",
          parameters: OPERATE_PARAMETERS,
        },
      },
      {
        type: "function",
        function: {
          name: "saolei_init",
          description: "Start a new minesweeper game.",
          parameters: {},
        },
      },
    ]);
  });

  it("omits the tools field when tool schemas are absent or empty", () => {
    const messages = [
      createUserMessage({
        content: [{ type: "text", text: "hi" }],
        source: { kind: "user" },
      }),
    ];

    const withoutTools = serializeRequest(baseOptions({ messages }));
    expect(withoutTools).not.toHaveProperty("tools");
    expect(withoutTools).toEqual({
      model: "glm-5.3",
      messages: [{ role: "user", content: "hi" }],
      stream: true,
      stream_options: { include_usage: true },
    });

    const emptyTools = serializeRequest(baseOptions({ tools: [], messages }));
    expect(emptyTools).not.toHaveProperty("tools");
    expect(emptyTools).toEqual(withoutTools);
  });

  it("throws UNSUPPORTED_CONTENT for image content", () => {
    // The image block is structurally fake (no attachment service in this
    // package's scope); any image must fail loudly rather than reach the
    // wire (contract §3 text-only v1).
    const options = baseOptions({
      messages: [
        createUserMessage({
          content: [
            { type: "text", text: "look" },
            { type: "image", attachment: {} } as never,
          ],
          source: { kind: "user" },
        }),
      ],
    });

    expect(() => serializeRequest(options)).toThrowError(LlmError);
    try {
      serializeRequest(options);
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("UNSUPPORTED_CONTENT");
    }
  });

  it("throws UNSUPPORTED_CONTENT for non-text tool result content", () => {
    const options = baseOptions({
      messages: [
        createToolResultMessage({
          callId: CallId("call_1"),
          content: [{ type: "image", attachment: {} } as never],
          isError: false,
        }),
      ],
    });

    expect(() => serializeRequest(options)).toThrowError(LlmError);
    try {
      serializeRequest(options);
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("UNSUPPORTED_CONTENT");
    }
  });

  it("throws UNSUPPORTED for reasoning efforts the wire cannot express", () => {
    const options = baseOptions({
      reasoningEffort: "high" as never,
      messages: [
        createUserMessage({
          content: [{ type: "text", text: "hi" }],
          source: { kind: "user" },
        }),
      ],
    });

    expect(() => serializeRequest(options)).toThrowError(LlmError);
    try {
      serializeRequest(options);
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("UNSUPPORTED");
    }
  });
});
