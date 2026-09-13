import { describe, expect, it } from "vitest";

import { LlmError } from "@deepseek-ai/dsh-llm";
import type { StreamChunk } from "@deepseek-ai/dsh-llm";

import { createResponsesWire } from "./wire.js";
import { serializeRequest } from "./serialize.js";

// frame renders one SSE frame the way the fake Responses endpoint and the
// real GLM endpoint emit them (event line + JSON data line).
function frame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

function feedAll(frames: string[]): StreamChunk[] {
  const wire = createResponsesWire();
  return frames.flatMap((f) => wire.feed(f));
}

function textFrameChunks(): StreamChunk[] {
  return feedAll([
    frame("response.created", { type: "response.created", response: { id: "resp_fake_1", status: "in_progress" } }),
    frame("response.output_item.added", {
      type: "response.output_item.added",
      output_index: 0,
      item: { type: "message", role: "assistant" },
    }),
    frame("response.output_text.delta", {
      type: "response.output_text.delta",
      item_id: "msg_fake_1",
      output_index: 0,
      delta: "Hello",
    }),
    frame("response.output_text.delta", {
      type: "response.output_text.delta",
      item_id: "msg_fake_1",
      output_index: 0,
      delta: "!",
    }),
    frame("response.output_item.done", {
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "message",
        role: "assistant",
        content: [{ type: "output_text", text: "Hello!" }],
      },
    }),
    frame("response.completed", {
      type: "response.completed",
      response: {
        id: "resp_fake_1",
        status: "completed",
        usage: {
          input_tokens: 10,
          output_tokens: 5,
          output_tokens_details: { reasoning_tokens: 2 },
        },
      },
    }),
  ]);
}

describe("Responses wire mapping", () => {
  it("maps a text-only stream to indexed text chunks with usage before finish", () => {
    const chunks = textFrameChunks();

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "Hello" },
      { type: "text-delta", index: 0, text: "!" },
      { type: "block-end", index: 0, block: { type: "text", text: "Hello!" } },
      {
        type: "usage",
        usage: { inputTokens: 10, outputTokens: 5, reasoningTokens: 2 },
      },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("emits nothing after finish", () => {
    const wire = createResponsesWire();
    feedAllInto(wire, [
      frame("response.completed", { type: "response.completed", response: { status: "completed" } }),
    ]);
    expect(wire.feed(frame("response.output_text.delta", {
      type: "response.output_text.delta",
      output_index: 0,
      delta: "late",
    }))).toEqual([]);
    expect(wire.feed(frame("response.completed", {
      type: "response.completed",
      response: { status: "completed" },
    }))).toEqual([]);
  });

  it("maps think+text streams with per-item index allocation and reuse", () => {
    const chunks = feedAll([
      frame("response.created", { type: "response.created", response: { id: "resp_fake_1", status: "in_progress" } }),
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "reasoning", id: "rs_fake_1" },
      }),
      frame("response.reasoning_summary_text.delta", {
        type: "response.reasoning_summary_text.delta",
        item_id: "rs_fake_1",
        output_index: 0,
        delta: "thinking ",
      }),
      frame("response.reasoning_summary_text.delta", {
        type: "response.reasoning_summary_text.delta",
        item_id: "rs_fake_1",
        output_index: 0,
        delta: "hard",
      }),
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 1,
        item: { type: "message", role: "assistant" },
      }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        item_id: "msg_fake_1",
        output_index: 1,
        delta: "answer",
      }),
      frame("response.output_item.done", {
        type: "response.output_item.done",
        output_index: 1,
        item: { type: "message", role: "assistant", content: [{ type: "output_text", text: "answer" }] },
      }),
      frame("response.completed", {
        type: "response.completed",
        response: { id: "resp_fake_1", status: "completed", usage: { input_tokens: 7, output_tokens: 3 } },
      }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "thinking " },
      { type: "reasoning-delta", index: 0, text: "hard" },
      { type: "block-start", index: 1, blockType: "text" },
      { type: "text-delta", index: 1, text: "answer" },
      { type: "block-end", index: 1, block: { type: "text", text: "answer" } },
      { type: "usage", usage: { inputTokens: 7, outputTokens: 3 } },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("accepts the reasoning_text delta vocabulary and dedupes content_part starts", () => {
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "reasoning", id: "rs_fake_1" },
      }),
      frame("response.reasoning_text.delta", {
        type: "response.reasoning_text.delta",
        item_id: "rs_fake_1",
        output_index: 0,
        delta: "direct reasoning",
      }),
      frame("response.reasoning_summary_part.added", {
        type: "response.reasoning_summary_part.added",
        item_id: "rs_fake_1",
        output_index: 0,
        part: { type: "summary_text" },
      }),
      frame("response.output_item.done", {
        type: "response.output_item.done",
        output_index: 0,
        item: { type: "reasoning", id: "rs_fake_1" },
      }),
    ]);

    // The summary_part.added must NOT double-start the block announced by
    // output_item.added.
    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "direct reasoning" },
      { type: "block-end", index: 0, block: { type: "reasoning", text: "direct reasoning" } },
    ]);
  });

  it("starts a reasoning block from summary_part.added when item.added was skipped", () => {
    const chunks = feedAll([
      frame("response.reasoning_summary_part.added", {
        type: "response.reasoning_summary_part.added",
        output_index: 0,
        part: { type: "summary_text" },
      }),
      frame("response.reasoning_summary_text.delta", {
        type: "response.reasoning_summary_text.delta",
        output_index: 0,
        delta: "variant",
      }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "variant" },
    ]);
  });

  it("maps function_call items to tool-call chunks and finishes with tool-calls", () => {
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "function_call", id: "fc_1", call_id: "call_1", name: "mouse_move" },
      }),
      frame("response.function_call_arguments.delta", {
        type: "response.function_call_arguments.delta",
        item_id: "fc_1",
        output_index: 0,
        delta: '{"x":',
      }),
      frame("response.function_call_arguments.delta", {
        type: "response.function_call_arguments.delta",
        item_id: "fc_1",
        output_index: 0,
        delta: "1}",
      }),
      frame("response.output_item.done", {
        type: "response.output_item.done",
        output_index: 0,
        item: { type: "function_call", id: "fc_1", call_id: "call_1", name: "mouse_move", arguments: '{"x":1}' },
      }),
      frame("response.completed", {
        type: "response.completed",
        response: { status: "completed", usage: { input_tokens: 3, output_tokens: 2 } },
      }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "tool-call" },
      { type: "tool-call-delta", index: 0, id: "call_1", argumentsDelta: '{"x":' },
      { type: "tool-call-delta", index: 0, id: "call_1", argumentsDelta: "1}" },
      {
        type: "block-end",
        index: 0,
        block: { type: "tool-call", id: "call_1", name: "mouse_move", arguments: '{"x":1}' },
      },
      { type: "usage", usage: { inputTokens: 3, outputTokens: 2 } },
      { type: "finish", reason: { kind: "tool-calls" } },
    ]);
  });

  it("maps an interleaved reasoning/message/tool-call stream with distinct indexes and a round-trippable block", () => {
    // Multi-block tool turn: reasoning and text interleave before the
    // function_call; each block keeps its own first-seen index. The
    // block-end tool-call block is exactly the shape serializeRequest
    // consumes, so the streamed call replays as a `function_call` input
    // item on the next step (D11 round-trip seam).
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "reasoning", id: "rs_1" },
      }),
      frame("response.reasoning_summary_text.delta", {
        type: "response.reasoning_summary_text.delta",
        item_id: "rs_1",
        output_index: 0,
        delta: "thinking",
      }),
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 1,
        item: { type: "message", role: "assistant" },
      }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        item_id: "msg_1",
        output_index: 1,
        delta: "Opening the board.",
      }),
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 2,
        item: { type: "function_call", id: "fc_1", call_id: "call_1", name: "saolei_init" },
      }),
      frame("response.function_call_arguments.delta", {
        type: "response.function_call_arguments.delta",
        item_id: "fc_1",
        output_index: 2,
        delta: "{}",
      }),
      frame("response.output_item.done", {
        type: "response.output_item.done",
        output_index: 2,
        item: { type: "function_call", id: "fc_1", call_id: "call_1", name: "saolei_init", arguments: "{}" },
      }),
      frame("response.completed", {
        type: "response.completed",
        response: { status: "completed", usage: { input_tokens: 5, output_tokens: 4 } },
      }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "reasoning" },
      { type: "reasoning-delta", index: 0, text: "thinking" },
      { type: "block-start", index: 1, blockType: "text" },
      { type: "text-delta", index: 1, text: "Opening the board." },
      // The tool identity rides the block-end block; block-start carries
      // only the index and block type (same shape as the single-call case
      // above).
      { type: "block-start", index: 2, blockType: "tool-call" },
      { type: "tool-call-delta", index: 2, id: "call_1", argumentsDelta: "{}" },
      {
        type: "block-end",
        index: 2,
        block: { type: "tool-call", id: "call_1", name: "saolei_init", arguments: "{}" },
      },
      { type: "usage", usage: { inputTokens: 5, outputTokens: 4 } },
      { type: "finish", reason: { kind: "tool-calls" } },
    ]);

    // Round-trip seam: the terminal tool-call block feeds the request
    // serializer unchanged and comes back out as the official input item.
    const toolCallBlock = (chunks.find((c) => c.type === "block-end") as { block: unknown }).block;
    const request = serializeRequest({
      provider: "glm-responses",
      model: "glm-5.2",
      messages: [
        {
          id: "a1" as never,
          role: "assistant",
          content: [toolCallBlock as never],
          source: { provider: "glm-responses", model: "glm-5.2" },
        } as never,
      ],
    });
    expect(request.input).toEqual([
      { type: "function_call", call_id: "call_1", name: "saolei_init", arguments: "{}" },
    ]);
  });

  it("ignores unknown event types (forward-compat)", () => {
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "message", role: "assistant" },
      }),
      frame("response.unknown_event.v99", { type: "response.unknown_event.v99", delta: "mystery" }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "ok",
      }),
      frame("response.completed", { type: "response.completed", response: { status: "completed" } }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("maps response.incomplete to a max-tokens finish", () => {
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "message", role: "assistant" },
      }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "partial",
      }),
      frame("response.incomplete", {
        type: "response.incomplete",
        response: { status: "incomplete", usage: { input_tokens: 4, output_tokens: 1 } },
      }),
    ]);

    expect(chunks.at(-2)).toEqual({
      type: "usage",
      usage: { inputTokens: 4, outputTokens: 1 },
    });
    expect(chunks.at(-1)).toEqual({ type: "finish", reason: { kind: "max-tokens" } });
  });

  it("maps response.failed to an in-band error finish without a usage event", () => {
    const chunks = feedAll([
      frame("response.failed", {
        type: "response.failed",
        response: {
          id: "resp_fake_1",
          status: "failed",
          error: { code: "glm_test_failure", message: "injected failure" },
        },
      }),
    ]);

    expect(chunks).toEqual([
      {
        type: "finish",
        reason: { kind: "error", failure: { code: "glm_test_failure", message: "injected failure" } },
      },
    ]);
  });

  it("maps bare error events to an in-band error finish", () => {
    const chunks = feedAll([
      frame("error", { type: "error", code: "server_error", message: "boom" }),
    ]);

    expect(chunks).toEqual([
      {
        type: "finish",
        reason: { kind: "error", failure: { code: "server_error", message: "boom" } },
      },
    ]);
  });

  it("reassembles SSE frames split across feed boundaries", () => {
    const whole = frame("response.output_text.delta", {
      type: "response.output_text.delta",
      output_index: 0,
      delta: "split",
    });
    const wire = createResponsesWire();
    const cut = Math.floor(whole.length / 2);
    const first = wire.feed(whole.slice(0, cut));
    const second = wire.feed(whole.slice(cut));

    expect(first).toEqual([]);
    // The delta names no started block, so the implicit-start tolerance
    // announces one before translating the delta.
    expect(second).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "split" },
    ]);
  });

  it("throws a MALFORMED_RESPONSE LlmError on malformed event payloads", () => {
    const wire = createResponsesWire();
    expect(() => wire.feed("event: response.output_text.delta\ndata: not-json\n\n")).toThrow(
      LlmError,
    );
    try {
      wire.feed("event: response.output_text.delta\ndata: not-json\n\n");
      expect.unreachable();
    } catch (err) {
      expect((err as LlmError).code).toBe("MALFORMED_RESPONSE");
    }
  });

  it("maps a terminal event with zero content blocks to an EMPTY_RESPONSE error finish", () => {
    // Degenerate completion (fake-llm `empty` injection: created →
    // completed with no output items) must not become a successful empty
    // turn (FR-007, llm-failure-taxonomy.md §1 义务 3).
    const completed = feedAll([
      frame("response.created", {
        type: "response.created",
        response: { id: "resp_fake_1", status: "in_progress" },
      }),
      frame("response.completed", {
        type: "response.completed",
        response: { status: "completed", usage: { input_tokens: 4, output_tokens: 0 } },
      }),
    ]);
    expect(completed).toEqual([
      { type: "usage", usage: { inputTokens: 4, outputTokens: 0 } },
      {
        type: "finish",
        reason: {
          kind: "error",
          failure: {
            message: "GLM Responses stream finished without any content blocks",
            code: "EMPTY_RESPONSE",
          },
        },
      },
    ]);

    const incomplete = feedAll([
      frame("response.incomplete", {
        type: "response.incomplete",
        response: { status: "incomplete" },
      }),
    ]);
    expect(incomplete).toEqual([
      {
        type: "finish",
        reason: {
          kind: "error",
          failure: {
            message: "GLM Responses stream finished without any content blocks",
            code: "EMPTY_RESPONSE",
          },
        },
      },
    ]);
  });

  it("surfaces SSE comment frames to the onComment pulse callback", () => {
    let pulses = 0;
    const wire = createResponsesWire(() => {
      pulses += 1;
    });
    // A comment frame yields no StreamChunks but is transport activity.
    expect(wire.feed(": keep-alive\n\n")).toEqual([]);
    expect(pulses).toBe(1);
    // Comments do not disturb the event translation around them.
    expect(
      wire.feed(
        frame("response.completed", {
          type: "response.completed",
          response: { status: "completed" },
        }),
      ),
    ).toEqual([
      {
        type: "finish",
        reason: {
          kind: "error",
          failure: {
            message: "GLM Responses stream finished without any content blocks",
            code: "EMPTY_RESPONSE",
          },
        },
      },
    ]);
    expect(pulses).toBe(1);
  });

  it("ignores comment frames without an onComment callback", () => {
    const wire = createResponsesWire();
    expect(wire.feed(": keep-alive\n\n")).toEqual([]);
  });

  it("treats null usage/error fields as absent (OpenAPI examples carry nulls)", () => {
    const chunks = feedAll([
      frame("response.output_item.added", {
        type: "response.output_item.added",
        output_index: 0,
        item: { type: "message", role: "assistant" },
      }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "ok",
      }),
      frame("response.completed", {
        type: "response.completed",
        response: { id: "resp_fake_1", status: "completed", error: null, usage: null },
      }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("skips keep-alive frames with an empty data line instead of failing", () => {
    const chunks = feedAll([
      "event: response.output_text.delta\ndata: \n\n",
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "ok",
      }),
      frame("response.completed", { type: "response.completed", response: { status: "completed" } }),
    ]);

    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "ok" },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });

  it("ignores content_part/reasoning_summary_part forms whose part.type is out of scope", () => {
    const chunks = feedAll([
      frame("response.content_part.added", {
        type: "response.content_part.added",
        output_index: 0,
        part: { type: "refusal" },
      }),
      frame("response.reasoning_summary_part.added", {
        type: "response.reasoning_summary_part.added",
        output_index: 1,
        part: { type: "other" },
      }),
      frame("response.output_text.delta", {
        type: "response.output_text.delta",
        output_index: 0,
        delta: "late",
      }),
      frame("response.completed", { type: "response.completed", response: { status: "completed" } }),
    ]);

    // Out-of-scope part types must not start blocks; the delta's implicit
    // start is the only block.
    expect(chunks).toEqual([
      { type: "block-start", index: 0, blockType: "text" },
      { type: "text-delta", index: 0, text: "late" },
      { type: "finish", reason: { kind: "stop" } },
    ]);
  });
});

function feedAllInto(wire: ReturnType<typeof createResponsesWire>, frames: string[]): void {
  for (const f of frames) {
    wire.feed(f);
  }
}
