import { describe, expect, it } from "vitest";

import { createAssistantMessage, createUserMessage, LlmError } from "@deepseek-ai/dsh-llm";
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";

import { serializeRequest } from "./serialize.js";

function baseOptions(overrides: Partial<GenerateOptions>): GenerateOptions {
  return {
    provider: "glm-responses",
    model: "glm-5.2",
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

describe("Responses request serialization", () => {
  it("maps a multi-turn user/assistant round-trip without replaying reasoning", () => {
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
            source: { provider: "glm-responses", model: "glm-5.2" },
          }),
          createUserMessage({
            content: [{ type: "text", text: "M2" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request).toEqual({
      model: "glm-5.2",
      instructions: "You are a game table assistant.",
      input: [
        { type: "message", role: "user", content: [{ type: "input_text", text: "M1" }] },
        { type: "message", role: "assistant", content: [{ type: "output_text", text: "R1" }] },
        { type: "message", role: "user", content: [{ type: "input_text", text: "M2" }] },
      ],
      stream: true,
    });
  });

  it("omits instructions when no system prompt is set", () => {
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

    expect(request.instructions).toBeUndefined();
    expect(request.input).toHaveLength(1);
  });

  it("skips assistant messages whose only content is reasoning", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "M1" }],
            source: { kind: "user" },
          }),
          createAssistantMessage({
            content: [{ type: "reasoning", text: "only thinking this turn" }],
            source: { provider: "glm-responses", model: "glm-5.2" },
          }),
          createUserMessage({
            content: [{ type: "text", text: "M2" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    // Reasoning is not replayed, so the pure-reasoning assistant message
    // has no sendable content and must not become an empty content array.
    expect(request.input).toEqual([
      { type: "message", role: "user", content: [{ type: "input_text", text: "M1" }] },
      { type: "message", role: "user", content: [{ type: "input_text", text: "M2" }] },
    ]);
  });

  it("maps temperature and maxTokens to the Responses fields", () => {
    const request = serializeRequest(
      baseOptions({
        temperature: 0.5,
        maxTokens: 256,
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "hi" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request.temperature).toBe(0.5);
    expect(request.max_output_tokens).toBe(256);
  });

  it("maps tool schemas to the flat Responses function tools array", () => {
    // GenerateOptions.tools must reach the wire or the model can never
    // issue a function call (specs/054-agent-v2-bugfixes/revisions/
    // t029-glm-tools-serialization.md §0). Each entry flattens to the
    // OpenAI Responses FunctionTool shape — type/name/description/
    // parameters at the top level.
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
        name: "saolei_operate",
        description: "Execute one or more minesweeper cell operations IN ORDER.",
        parameters: OPERATE_PARAMETERS,
      },
      {
        type: "function",
        name: "saolei_init",
        description: "Start a new minesweeper game.",
        parameters: {},
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
    const expectedBody = {
      model: "glm-5.2",
      input: [{ type: "message", role: "user", content: [{ type: "input_text", text: "hi" }] }],
      stream: true,
    };

    const withoutTools = serializeRequest(baseOptions({ messages }));
    expect(withoutTools).not.toHaveProperty("tools");
    // Full-body assertion guards against field drift, not just the
    // absence of tools.
    expect(withoutTools).toEqual(expectedBody);

    const emptyTools = serializeRequest(baseOptions({ tools: [], messages }));
    expect(emptyTools).not.toHaveProperty("tools");
    expect(emptyTools).toEqual(withoutTools);
  });

  it("never emits a strict field on serialized tools", () => {
    // Strict mode requires a closed JSON-schema subset; the saolei
    // dual-form schemas do not satisfy it, and explicit strict:false is
    // semantically equal to omitting the field
    // (specs/054-agent-v2-bugfixes/revisions/t029-glm-tools-serialization.md §1).
    const request = serializeRequest(
      baseOptions({
        tools: TOOL_FIXTURES,
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "hi" }],
            source: { kind: "user" },
          }),
        ],
      }),
    );

    expect(request.tools).toHaveLength(2);
    for (const tool of request.tools ?? []) {
      expect(tool).not.toHaveProperty("strict");
    }
  });

  it("throws UNSUPPORTED_CONTENT for image content", () => {
    // The image block is structurally fake (no attachment service in this
    // package's scope); any non-text user block must fail loudly rather
    // than reach the wire.
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

  it("maps an assistant tool-call block to a function_call input item", () => {
    // D11 (specs/051-agent-v2-dsh-migration/research.md): the assistant
    // tool-call block replays as the official `function_call` input item —
    // the 049 UNSUPPORTED_CONTENT placeholder is lifted.
    const request = serializeRequest(
      baseOptions({
        messages: [
          createAssistantMessage({
            content: [
              { type: "text", text: "Let me check the board." },
              {
                type: "tool-call",
                id: "call_1",
                name: "saolei_init",
                arguments: "{}",
              },
            ],
            source: { provider: "glm-responses", model: "glm-5.2" },
          }),
        ],
      }),
    );

    expect(request.input).toEqual([
      { type: "message", role: "assistant", content: [{ type: "output_text", text: "Let me check the board." }] },
      { type: "function_call", call_id: "call_1", name: "saolei_init", arguments: "{}" },
    ]);
  });

  it("maps a tool-result message to a function_call_output input item", () => {
    // createToolResultMessage fixes the user role and couples the tool
    // source to its result block; the rendered text becomes `output`,
    // joined to the call by call_id (OpenAI Responses input item shape).
    const request = serializeRequest(
      baseOptions({
        messages: [
          createUserMessage({
            content: [
              {
                type: "tool-result",
                toolCallId: "call_1",
                content: [{ type: "text", text: "new game started\nboard size 9*9" }],
              },
            ],
            source: { kind: "tool" },
          }),
        ],
      }),
    );

    expect(request.input).toEqual([
      {
        type: "function_call_output",
        call_id: "call_1",
        output: "new game started\nboard size 9*9",
      },
    ]);
  });

  it("serializes a full tool round-trip with steps interleaved in stream order", () => {
    // Multi-step turn round-trip: user ask → assistant text + tool call →
    // tool result → assistant summary. Items interleave in history order;
    // call_id links every function_call to its function_call_output.
    const request = serializeRequest(
      baseOptions({
        system: "You are a game table assistant.",
        messages: [
          createUserMessage({
            content: [{ type: "text", text: "Start a game." }],
            source: { kind: "user" },
          }),
          createAssistantMessage({
            content: [
              { type: "reasoning", text: "chain of thought" },
              { type: "text", text: "Opening the board." },
              { type: "tool-call", id: "call_1", name: "saolei_init", arguments: "{}" },
            ],
            source: { provider: "glm-responses", model: "glm-5.2" },
          }),
          createUserMessage({
            content: [
              {
                type: "tool-result",
                toolCallId: "call_1",
                content: [{ type: "text", text: "new game started" }],
              },
            ],
            source: { kind: "tool" },
          }),
          createAssistantMessage({
            content: [{ type: "text", text: "The game has started." }],
            source: { provider: "glm-responses", model: "glm-5.2" },
          }),
        ],
      }),
    );

    expect(request.input).toEqual([
      { type: "message", role: "user", content: [{ type: "input_text", text: "Start a game." }] },
      { type: "message", role: "assistant", content: [{ type: "output_text", text: "Opening the board." }] },
      { type: "function_call", call_id: "call_1", name: "saolei_init", arguments: "{}" },
      { type: "function_call_output", call_id: "call_1", output: "new game started" },
      { type: "message", role: "assistant", content: [{ type: "output_text", text: "The game has started." }] },
    ]);
  });

  it("joins multiple text blocks of one tool result with newlines", () => {
    const request = serializeRequest(
      baseOptions({
        messages: [
          createUserMessage({
            content: [
              {
                type: "tool-result",
                toolCallId: "call_9",
                content: [
                  { type: "text", text: "saolei_operate → executed 2 ops" },
                  { type: "text", text: "game status: playing" },
                ],
              },
            ],
            source: { kind: "tool" },
          }),
        ],
      }),
    );

    expect(request.input).toEqual([
      {
        type: "function_call_output",
        call_id: "call_9",
        output: "saolei_operate → executed 2 ops\ngame status: playing",
      },
    ]);
  });

  it("throws UNSUPPORTED_CONTENT for non-text tool result content", () => {
    const options = baseOptions({
      messages: [
        createUserMessage({
          content: [
            {
              type: "tool-result",
              toolCallId: "call_1",
              content: [{ type: "image", attachment: {} } as never],
            },
          ],
          source: { kind: "tool" },
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

  it("throws UNSUPPORTED for stop sequences", () => {
    const options = baseOptions({
      stop: ["END"],
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

  it("throws UNSUPPORTED for reasoning efforts", () => {
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
