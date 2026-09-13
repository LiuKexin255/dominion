import { describe, expect, it } from "vitest";

import { LlmError } from "@deepseek-ai/dsh-llm";
import type { StreamChunk } from "@deepseek-ai/dsh-llm";

import { createChatCompletionsWire } from "./wire.js";

// frame renders one SSE frame the way the fake Chat Completions endpoint and
// the real opencode-go gateway emit them (`data:` line only, blank-line
// terminated).
function frame(data: unknown): string {
  const payload = typeof data === "string" ? data : JSON.stringify(data);
  return `data: ${payload}\n\n`;
}

// chunk builds one `chat.completion.chunk` payload in the fake-llm handler's
// vocabulary (projects/game/fake-llm/service/handler.go): a single choice
// carrying a delta and a null-or-string finish_reason.
function chunk(
  delta: Record<string, unknown>,
  finishReason: string | null = null,
  usage?: unknown,
): Record<string, unknown> {
  return {
    id: "chatcmpl_fake_1",
    object: "chat.completion.chunk",
    created: 0,
    model: "glm-5.3",
    choices: [{ index: 0, delta, finish_reason: finishReason }],
    ...(usage === undefined ? {} : { usage }),
  };
}

describe("Chat Completions wire mapping", () => {
  it("maps interleaved reasoning/text/tool-call deltas with buffered usage and finish at [DONE]", () => {
    const wire = createChatCompletionsWire();
    const chunks: StreamChunk[] = [
      ...wire.feed(frame(chunk({ role: "assistant", reasoning_content: "think" }))),
      ...wire.feed(frame(chunk({ reasoning_content: " harder" }))),
      ...wire.feed(frame(chunk({ content: "Hel" }))),
      ...wire.feed(frame(chunk({ content: "lo" }))),
      ...wire.feed(
        frame(
          chunk({
            tool_calls: [
              { index: 0, id: "call_1", function: { name: "saolei_init", arguments: "" } },
            ],
          }),
        ),
      ),
      ...wire.feed(
        frame(chunk({ tool_calls: [{ index: 0, function: { arguments: "{}" } }] })),
      ),
      ...wire.feed(frame(chunk({}, "tool_calls"))),
      // Trailing usage-only chunk (choices: []) must be cached until [DONE].
      ...wire.feed(
        frame({
          id: "chatcmpl_fake_1",
          object: "chat.completion.chunk",
          choices: [],
          usage: {
            prompt_tokens: 10,
            completion_tokens: 5,
            completion_tokens_details: { reasoning_tokens: 2 },
          },
        }),
      ),
      ...wire.feed(frame("[DONE]")),
    ];

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "think" },
      { type: "reasoning-delta", index: 0, text: " harder" },
      { type: "block-start", index: 1, blockType: "text" },
      { type: "text-delta", index: 1, text: "Hel" },
      { type: "text-delta", index: 1, text: "lo" },
      { type: "block-start", index: 2, blockType: "tool-call" },
      {
        type: "tool-call-delta",
        index: 2,
        id: "call_1",
        name: "saolei_init",
        argumentsDelta: "",
      },
      {
        type: "tool-call-delta",
        index: 2,
        id: "call_1",
        name: "saolei_init",
        argumentsDelta: "{}",
      },
      { type: "block-end", index: 0, block: { type: "reasoning", text: "think harder" } },
      { type: "block-end", index: 1, block: { type: "text", text: "Hello" } },
      {
        type: "block-end",
        index: 2,
        block: { type: "tool-call", id: "call_1", name: "saolei_init", arguments: "{}" },
      },
      { type: "usage", usage: { inputTokens: 10, outputTokens: 5, reasoningTokens: 2 } },
      { type: "finish", reason: { kind: "tool-calls" } },
    ]);
    expect(wire.end()).toEqual([]);
  });

  it("reuses one block per wire index across parallel tool calls", () => {
    const chunks = feedAll([
      frame(
        chunk({
          tool_calls: [
            { index: 0, id: "call_a", function: { name: "saolei_init", arguments: "" } },
            { index: 1, id: "call_b", function: { name: "saolei_operate", arguments: "" } },
          ],
        }),
      ),
      frame(chunk({ tool_calls: [{ index: 1, function: { arguments: '{"x":' } }] })),
      frame(chunk({ tool_calls: [{ index: 0, function: { arguments: "{}" } }] })),
      frame(chunk({ tool_calls: [{ index: 1, function: { arguments: "1}" } }] })),
      frame(chunk({}, "tool_calls")),
      frame("[DONE]"),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "tool-call" },
      {
        type: "tool-call-delta",
        index: 0,
        id: "call_a",
        name: "saolei_init",
        argumentsDelta: "",
      },
      { type: "block-start", index: 1, blockType: "tool-call" },
      {
        type: "tool-call-delta",
        index: 1,
        id: "call_b",
        name: "saolei_operate",
        argumentsDelta: "",
      },
      {
        type: "tool-call-delta",
        index: 1,
        id: "call_b",
        name: "saolei_operate",
        argumentsDelta: '{"x":',
      },
      {
        type: "tool-call-delta",
        index: 0,
        id: "call_a",
        name: "saolei_init",
        argumentsDelta: "{}",
      },
      {
        type: "tool-call-delta",
        index: 1,
        id: "call_b",
        name: "saolei_operate",
        argumentsDelta: "1}",
      },
      {
        type: "block-end",
        index: 0,
        block: { type: "tool-call", id: "call_a", name: "saolei_init", arguments: "{}" },
      },
      {
        type: "block-end",
        index: 1,
        block: {
          type: "tool-call",
          id: "call_b",
          name: "saolei_operate",
          arguments: '{"x":1}',
        },
      },
      { type: "finish", reason: { kind: "tool-calls" } },
    ]);
  });

  it("maps every finish_reason vocabulary to its finish kind", () => {
    const cases: Array<{ wire: string; expected: StreamChunk }> = [
      { wire: "stop", expected: { type: "finish", reason: { kind: "stop" } } },
      {
        wire: "tool_calls",
        expected: { type: "finish", reason: { kind: "tool-calls" } },
      },
      {
        wire: "length",
        expected: { type: "finish", reason: { kind: "max-tokens" } },
      },
      {
        wire: "content_filter",
        expected: {
          type: "finish",
          reason: {
            kind: "error",
            failure: { message: "model stopped: content_filter", code: "CONTENT_FILTER" },
          },
        },
      },
    ];

    for (const testCase of cases) {
      const chunks = feedAll([
        frame(chunk({ content: "partial" })),
        frame(chunk({}, testCase.wire)),
        frame("[DONE]"),
      ]);
      expect(chunks.at(-1)).toEqual(testCase.expected);
    }
  });

  it("maps a terminal stop with zero content blocks to an EMPTY_RESPONSE error finish", () => {
    // Degenerate completion (fake-llm `empty` injection: the role opening
    // frame and the terminal stop frame, no payload) must not become a
    // successful empty turn (FR-007, llm-failure-taxonomy.md §1 义务 3).
    const chunks = feedAll([
      frame(chunk({ role: "assistant" })),
      frame(chunk({}, "stop")),
      frame("[DONE]"),
    ]);

    expect(chunks).toEqual([
      {
        type: "finish",
        reason: {
          kind: "error",
          failure: {
            message: "OpenCode Go stream finished without any content blocks",
            code: "EMPTY_RESPONSE",
          },
        },
      },
    ]);
  });

  it("does not open blocks for empty reasoning/content strings", () => {
    const wire = createChatCompletionsWire();
    expect(
      wire.feed(frame(chunk({ role: "assistant", reasoning_content: "", content: "" }))),
    ).toEqual([]);
  });

  it("throws STREAM_CLOSED when the stream ends without [DONE]", () => {
    const wire = createChatCompletionsWire();
    expect(wire.feed(frame(chunk({ content: "partial" })))).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "partial" },
    ]);

    expect(() => wire.end()).toThrow(LlmError);
    try {
      wire.end();
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("STREAM_CLOSED");
    }
  });

  it("emits nothing after the [DONE] sentinel", () => {
    const wire = createChatCompletionsWire();
    expect(
      wire.feed(
        frame(chunk({ content: "ok" })) + frame(chunk({}, "stop")) + frame("[DONE]"),
      ),
    ).toHaveLength(4);
    expect(wire.feed(frame(chunk({ content: "late" })))).toEqual([]);
    expect(wire.feed(frame(chunk({}, "stop")) + frame("[DONE]"))).toEqual([]);
    expect(wire.end()).toEqual([]);
  });

  it("reassembles SSE frames split across feed boundaries", () => {
    const whole = frame(chunk({ content: "split" }));
    const wire = createChatCompletionsWire();
    const cut = Math.floor(whole.length / 2);
    expect(wire.feed(whole.slice(0, cut))).toEqual([]);
    expect(wire.feed(whole.slice(cut))).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "split" },
    ]);
  });

  it("throws a MALFORMED_RESPONSE LlmError on malformed event payloads", () => {
    const wire = createChatCompletionsWire();
    expect(() => wire.feed(frame("not-json"))).toThrow(LlmError);
    try {
      wire.feed(frame("not-json"));
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("MALFORMED_RESPONSE");
    }
  });

  it("surfaces SSE comment frames to the onComment pulse callback", () => {
    let pulses = 0;
    const wire = createChatCompletionsWire(() => {
      pulses += 1;
    });
    // A comment frame yields no StreamChunks but is transport activity.
    expect(wire.feed(": keep-alive\n\n")).toEqual([]);
    expect(pulses).toBe(1);
    // Comments do not disturb the event translation around them.
    expect(wire.feed(frame(chunk({ content: "ok" })) + frame(chunk({}, "stop")) + frame("[DONE]"))).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "block-end", index: 0, block: { type: "text", text: "ok" } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
    expect(pulses).toBe(1);
  });

  it("skips keep-alive frames with an empty data line instead of failing", () => {
    const chunks = feedAll([
      "data: \n\n",
      frame(chunk({ content: "ok" })),
      frame(chunk({}, "stop")),
      frame("[DONE]"),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "block-end", index: 0, block: { type: "text", text: "ok" } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("treats null usage and finish_reason as absent (OpenAI streams carry nulls)", () => {
    const chunks = feedAll([
      frame(chunk({ content: "ok" }, null)),
      frame(chunk({}, "stop")),
      frame("[DONE]"),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "block-end", index: 0, block: { type: "text", text: "ok" } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });
});

function feedAll(frames: string[]): StreamChunk[] {
  const wire = createChatCompletionsWire();
  const chunks = frames.flatMap((f) => wire.feed(f));
  return [...chunks, ...wire.end()];
}
