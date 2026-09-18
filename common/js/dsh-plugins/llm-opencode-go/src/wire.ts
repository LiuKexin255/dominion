/**
 * Chat Completions SSE wire for the opencode-go gateway: payload types,
 * eventsource-parser driven frame feeding, and the `chat.completion.chunk`
 * → dsh StreamChunk translation.
 *
 * Layering follows the official adapter cookbook's "Implementation
 * structure" (wire types / transport parsing / chunk translation as
 * separate responsibilities):
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md
 *
 * Payload vocabulary and the mapping table are contractual:
 * specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §4;
 * the shapes come from the OpenAI Chat Completions streaming objects and the
 * official chat-completions adapter (node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_*).
 */

import { CallId, EMPTY_RESPONSE_CODE, LlmError } from "@deepseek-ai/dsh-llm";
import type { ContentBlock, FinishReason, StreamChunk, TokenUsage } from "@deepseek-ai/dsh-llm";
import { createParser } from "eventsource-parser";
import type { EventSourceMessage } from "eventsource-parser";

/** The wire `usage` object carried by any chunk. */
export interface ChatCompletionsUsage {
  prompt_tokens?: number;
  completion_tokens?: number;
  completion_tokens_details?: { reasoning_tokens?: number };
}

/** One entry of a streaming `delta.tool_calls` array. */
export interface ChatToolCallDelta {
  /** Wire-local call index; reused across the call's argument fragments. */
  index?: number;
  id?: string;
  function?: { name?: string; arguments?: string };
}

/** The streaming delta object of one choice. */
export interface ChatDelta {
  role?: string;
  /** GLM/Kimi interleaved reasoning text (never a replayed request field). */
  reasoning_content?: string;
  content?: string;
  tool_calls?: ChatToolCallDelta[];
}

/** One `choices[]` entry of a chat completion chunk. */
export interface ChatChoice {
  index?: number;
  delta?: ChatDelta;
  /** Null (or absent) until the provider's terminal chunk. */
  finish_reason?: string | null;
}

/** One `chat.completion.chunk` SSE payload. */
export interface ChatCompletionChunk {
  choices?: ChatChoice[];
  usage?: ChatCompletionsUsage | null;
}

/** One open block tracked by first-seen stream order. */
interface WireBlock {
  index: number;
  kind: "reasoning" | "text" | "tool-call";
  callId?: string;
  name?: string;
  /** Accumulated text or raw JSON argument fragments. */
  text: string;
}

/**
 * A stateful Chat Completions SSE → StreamChunk translator. `feed` accepts
 * raw stream text (partial chunks allowed) and returns the StreamChunks the
 * fed events produced, in order. Protocol obligations implemented here:
 * block indexes allocated in first-seen stream order and reused for every
 * delta of the same block; finish reason and usage buffered until the
 * `[DONE]` sentinel, which flushes all `block-end`s, then usage, then
 * finish (nothing after finish); a stop (or absent) finish with zero opened
 * blocks is a degenerate completion mapped to an EMPTY_RESPONSE error
 * finish.
 */
export interface ChatCompletionsWire {
  feed(chunk: string): StreamChunk[];
  /**
   * Close the stream. Returns nothing when the `[DONE]` sentinel was seen;
   * throws `STREAM_CLOSED` when the stream ended without it (a truncated
   * response the model call cannot be trusted to have completed).
   */
  end(): StreamChunk[];
}

/**
 * Create a translator for one response stream. One instance per stream: the
 * underlying SSE parser and block registry must not be shared across
 * responses. `onComment` receives every SSE comment frame (keep-alives) so
 * the adapter's idle watchdog can treat them as transport activity
 * (llm-failure-taxonomy.md §1 义务 4).
 */
export function createChatCompletionsWire(onComment?: () => void): ChatCompletionsWire {
  const pendingEvents: EventSourceMessage[] = [];
  const pendingChunks: StreamChunk[] = [];
  const order: WireBlock[] = [];
  const toolBlocks = new Map<number, WireBlock>();
  let nextIndex = 0;
  let finished = false;
  let reasoningBlock: WireBlock | undefined;
  let textBlock: WireBlock | undefined;
  let pendingFinish: FinishReason | undefined;
  let pendingUsage: TokenUsage | undefined;

  const parser = createParser({
    onEvent: (message) => {
      pendingEvents.push(message);
    },
    onComment: () => {
      onComment?.();
    },
  });

  function open(kind: WireBlock["kind"]): WireBlock {
    const block: WireBlock = { index: nextIndex++, kind, text: "" };
    order.push(block);
    return block;
  }

  /**
   * Map the wire `finish_reason` vocabulary to the harness FinishReason:
   * stop/tool_calls/length map to their kinds; unrecognized values
   * (content_filter, …) become `{kind:'error'}` with the uppercased value
   * as `code` (contract §4).
   */
  function mapFinishReason(reason: string): FinishReason {
    switch (reason) {
      case "stop":
        return { kind: "stop" };
      case "tool_calls":
        return { kind: "tool-calls" };
      case "length":
        return { kind: "max-tokens" };
      default:
        return {
          kind: "error",
          failure: { message: `model stopped: ${reason}`, code: reason.toUpperCase() },
        };
    }
  }

  /**
   * Map the wire usage fields onto TokenUsage (contract §4): prompt_tokens
   * → inputTokens, completion_tokens → outputTokens, and
   * completion_tokens_details.reasoning_tokens → reasoningTokens when
   * reported.
   */
  function mapUsage(usage: ChatCompletionsUsage): TokenUsage {
    const mapped: TokenUsage = {
      inputTokens: usage.prompt_tokens ?? 0,
      outputTokens: usage.completion_tokens ?? 0,
    };
    const reasoning = usage.completion_tokens_details?.reasoning_tokens;
    if (reasoning !== undefined) {
      mapped.reasoningTokens = reasoning;
    }
    return mapped;
  }

  /** Assemble the terminal ContentBlock for one open block. */
  function assembledBlock(block: WireBlock): ContentBlock {
    switch (block.kind) {
      case "reasoning":
        return { type: "reasoning", text: block.text };
      case "text":
        return { type: "text", text: block.text };
      case "tool-call":
        return {
          type: "tool-call",
          id: CallId(block.callId ?? ""),
          name: block.name ?? "",
          arguments: block.text,
        };
    }
  }

  /**
   * Flush the buffered terminal state at the `[DONE]` sentinel: every
   * opened block's `block-end` in first-seen order, then usage, then finish.
   * The usage+finish pair stays buffered until here so a trailing
   * usage-only chunk (choices: []) is still accounted for and nothing
   * follows finish.
   */
  function flushDone(): void {
    for (const block of order) {
      pendingChunks.push({ type: "block-end", index: block.index, block: assembledBlock(block) });
    }
    if (pendingUsage !== undefined) {
      pendingChunks.push({ type: "usage", usage: pendingUsage });
      pendingUsage = undefined;
    }
    const reason = pendingFinish ?? { kind: "stop" };
    if (reason.kind === "stop" && order.length === 0) {
      // A terminal stop with zero opened blocks is a degenerate provider
      // completion, not a successful empty turn: no durable output was
      // produced, so EMPTY_RESPONSE classifies it for the retry decision
      // (FR-007, llm-failure-taxonomy.md §1 义务 3).
      pendingChunks.push({
        type: "finish",
        reason: {
          kind: "error",
          failure: {
            message: "OpenCode Go stream finished without any content blocks",
            code: EMPTY_RESPONSE_CODE,
          },
        },
      });
    } else {
      pendingChunks.push({ type: "finish", reason });
    }
    finished = true;
  }

  function mapChunk(chunk: ChatCompletionChunk): void {
    if (finished) {
      return;
    }
    const choice = chunk.choices?.[0];
    const delta = choice?.delta;

    const reasoning = delta?.reasoning_content;
    if (typeof reasoning === "string" && reasoning.length > 0) {
      if (reasoningBlock === undefined) {
        reasoningBlock = open("reasoning");
        pendingChunks.push({
          type: "block-start",
          index: reasoningBlock.index,
          blockType: "reasoning",
        });
      }
      reasoningBlock.text += reasoning;
      pendingChunks.push({
        type: "reasoning-delta",
        index: reasoningBlock.index,
        text: reasoning,
      });
    }

    const content = delta?.content;
    if (typeof content === "string" && content.length > 0) {
      if (textBlock === undefined) {
        textBlock = open("text");
        pendingChunks.push({ type: "block-start", index: textBlock.index, blockType: "text" });
      }
      textBlock.text += content;
      pendingChunks.push({ type: "text-delta", index: textBlock.index, text: content });
    }

    for (const call of delta?.tool_calls ?? []) {
      // The wire index keys the block registry; a missing index (some
      // providers omit it) shares the index-0 block, matching the official
      // adapter's undefined-key behavior.
      const key = call.index ?? 0;
      let block = toolBlocks.get(key);
      if (block === undefined) {
        block = open("tool-call");
        toolBlocks.set(key, block);
        pendingChunks.push({ type: "block-start", index: block.index, blockType: "tool-call" });
      }
      if (call.id !== undefined) {
        block.callId = call.id;
      }
      if (call.function?.name !== undefined) {
        block.name = call.function.name;
      }
      const fragment = call.function?.arguments ?? "";
      block.text += fragment;
      pendingChunks.push({
        type: "tool-call-delta",
        index: block.index,
        id: CallId(block.callId ?? ""),
        ...(block.name !== undefined ? { name: block.name } : {}),
        argumentsDelta: fragment,
      });
    }

    if (typeof choice?.finish_reason === "string") {
      pendingFinish = mapFinishReason(choice.finish_reason);
    }
    if (chunk.usage != null) {
      pendingUsage = mapUsage(chunk.usage);
    }
  }

  return {
    feed(chunk: string): StreamChunk[] {
      parser.feed(chunk);
      const events = pendingEvents.splice(0, pendingEvents.length);
      for (const message of events) {
        if (finished) {
          break;
        }
        if (message.data === "[DONE]") {
          flushDone();
          break;
        }
        // Keep-alive frames carry an empty data line; skipping them must
        // not be mistaken for a protocol failure.
        if (message.data.trim().length === 0) {
          continue;
        }
        let parsed: object;
        try {
          parsed = JSON.parse(message.data) as object;
        } catch {
          throw new LlmError(
            "OpenCode Go stream carried a malformed event payload",
            "MALFORMED_RESPONSE",
          );
        }
        mapChunk(parsed as ChatCompletionChunk);
      }
      return pendingChunks.splice(0, pendingChunks.length);
    },
    end(): StreamChunk[] {
      if (!finished) {
        throw new LlmError("OpenCode Go stream ended without [DONE]", "STREAM_CLOSED");
      }
      return pendingChunks.splice(0, pendingChunks.length);
    },
  };
}
