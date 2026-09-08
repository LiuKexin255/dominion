/**
 * Responses SSE wire for the GLM (OpenAI Responses protocol) endpoint:
 * event payload types, eventsource-parser driven frame feeding, and the
 * Responses event → dsh StreamChunk translation.
 *
 * Layering follows the official adapter cookbook's "Implementation
 * structure" (wire types / transport parsing / chunk translation as
 * separate responsibilities):
 * https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cookbook/adding-an-llm-adapter.md
 *
 * Event vocabulary and the mapping table are contractual:
 * specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §5; the wire
 * shapes come from the official Responses streaming events
 * (https://github.com/openai/openai-openapi/blob/main/openapi.yaml).
 */

import { CallId, LlmError } from "@deepseek-ai/dsh-llm";
import type { ContentBlock, StreamChunk, TokenUsage } from "@deepseek-ai/dsh-llm";
import { createParser } from "eventsource-parser";
import type { EventSourceMessage } from "eventsource-parser";

/** Lifecycle shell payloads (`response.created` / `response.in_progress`). */
export interface ResponseLifecycleEvent {
  type: "response.created" | "response.in_progress";
  response?: ResponsesResponse;
}

/** One output item of a Responses stream (message / reasoning / function_call). */
export interface ResponsesOutputItem {
  type?: string;
  id?: string;
  role?: string;
  /** The provider-issued tool call id on function_call items. */
  call_id?: string;
  name?: string;
  arguments?: string;
  content?: Array<{ type?: string; text?: string }>;
}

/** The response object carried by lifecycle and terminal events. */
export interface ResponsesResponse {
  id?: string;
  status?: string;
  error?: { code?: unknown; message?: string };
  usage?: ResponsesUsage;
}

/** Provider token accounting, in Responses wire vocabulary. */
export interface ResponsesUsage {
  input_tokens?: number;
  output_tokens?: number;
  output_tokens_details?: { reasoning_tokens?: number };
}

export interface OutputItemAddedEvent {
  type: "response.output_item.added";
  output_index: number;
  item: ResponsesOutputItem;
}

export interface OutputItemDoneEvent {
  type: "response.output_item.done";
  output_index: number;
  item: ResponsesOutputItem;
}

export interface ReasoningSummaryPartAddedEvent {
  type: "response.reasoning_summary_part.added";
  item_id?: string;
  output_index: number;
  part?: { type?: string };
}

/** OpenAI-standard reasoning delta vocabulary (summary text). */
export interface ReasoningSummaryTextDeltaEvent {
  type: "response.reasoning_summary_text.delta";
  item_id?: string;
  output_index: number;
  delta: string;
}

/** Direct-reasoning variant vocabulary (accepted alongside the summary form). */
export interface ReasoningTextDeltaEvent {
  type: "response.reasoning_text.delta";
  item_id?: string;
  output_index: number;
  delta: string;
}

export interface ContentPartAddedEvent {
  type: "response.content_part.added";
  item_id?: string;
  output_index: number;
  part?: { type?: string };
}

export interface ContentPartDoneEvent {
  type: "response.content_part.done";
  item_id?: string;
  output_index: number;
  part?: { type?: string; text?: string };
}

export interface OutputTextDeltaEvent {
  type: "response.output_text.delta";
  item_id?: string;
  output_index: number;
  delta: string;
}

export interface FunctionCallArgumentsDeltaEvent {
  type: "response.function_call_arguments.delta";
  item_id?: string;
  output_index: number;
  delta: string;
}

export interface ResponseCompletedEvent {
  type: "response.completed";
  response: ResponsesResponse;
}

export interface ResponseIncompleteEvent {
  type: "response.incomplete";
  response: ResponsesResponse;
}

export interface ResponseFailedEvent {
  type: "response.failed";
  response: ResponsesResponse;
}

export interface ErrorEvent {
  type: "error";
  code?: unknown;
  message?: string;
}

/** Every event payload this wire understands; unknown types are ignored. */
export type ResponsesStreamEvent =
  | ResponseLifecycleEvent
  | OutputItemAddedEvent
  | OutputItemDoneEvent
  | ReasoningSummaryPartAddedEvent
  | ReasoningSummaryTextDeltaEvent
  | ReasoningTextDeltaEvent
  | ContentPartAddedEvent
  | ContentPartDoneEvent
  | OutputTextDeltaEvent
  | FunctionCallArgumentsDeltaEvent
  | ResponseCompletedEvent
  | ResponseIncompleteEvent
  | ResponseFailedEvent
  | ErrorEvent;

/** One open-or-closed block tracked by output_index (first-seen order). */
interface WireBlock {
  index: number;
  kind: "reasoning" | "text" | "tool-call";
  callId?: ReturnType<typeof CallId>;
  name?: string;
  /** Accumulated text or raw JSON argument fragments. */
  text: string;
  ended: boolean;
}

/**
 * A stateful Responses SSE → StreamChunk translator. `feed` accepts raw
 * stream text (partial chunks allowed); it returns the StreamChunks the
 * fed events produced, in order. Protocol obligations implemented here:
 * block indexes allocated in first-seen stream order and reused for every
 * delta of the same block; usage flushed before finish and nothing after
 * finish (the usage+finish pair is buffered until the terminal event);
 * unknown event types ignored (forward-compat); both reasoning delta
 * vocabularies accepted; start/end dedupe for the content_part forms.
 */
export interface ResponsesWire {
  feed(chunk: string): StreamChunk[];
}

/**
 * Create a translator for one response stream. One instance per stream:
 * the underlying SSE parser and block registry must not be shared across
 * responses.
 */
export function createResponsesWire(): ResponsesWire {
  const blocks = new Map<number, WireBlock>();
  const pending: EventSourceMessage[] = [];
  const pendingChunks: StreamChunk[] = [];
  let nextIndex = 0;
  let finished = false;
  let sawToolCall = false;
  let usage: TokenUsage | undefined;

  const parser = createParser({
    onEvent: (message) => {
      pending.push(message);
    },
  });

  // startBlock registers (and announces) the block at output_index unless
  // one already exists there — the content_part/reasoning_summary_part
  // forms must not double-start a block announced by output_item.added.
  function startBlock(
    outputIndex: number,
    kind: WireBlock["kind"],
    item?: ResponsesOutputItem,
  ): void {
    if (finished || blocks.has(outputIndex)) {
      return;
    }
    const block: WireBlock = {
      index: nextIndex++,
      kind,
      callId: item?.call_id === undefined ? undefined : CallId(item.call_id),
      name: item?.name,
      text: "",
      ended: false,
    };
    blocks.set(outputIndex, block);
    if (kind === "tool-call") {
      sawToolCall = true;
    }
    // The tool call's provider id and name first surface on the block_end
    // chunk's tool-call block (the dsh StreamChunk block-start carries no
    // id field); the agent_v2 collector and the web store correlate from
    // there (chat.ts blockEndTerminal).
    pendingChunks.push({ type: "block-start", index: block.index, blockType: kind });
  }

  function blockAt(outputIndex: number): WireBlock | undefined {
    return blocks.get(outputIndex);
  }

  // delta appends one fragment to the block at output_index, implicitly
  // starting the block first when the stream opened mid-item (variant
  // providers that skip output_item.added) so every delta has an owner.
  // The implicit block carries no call id: a delta's item_id is the item
  // id (fc_*), not the provider tool-call id (call_*), so mislabeling it
  // would corrupt tool-call correlation.
  function delta(
    outputIndex: number,
    kind: WireBlock["kind"],
    fragment: string,
  ): void {
    if (finished) {
      return;
    }
    let block = blockAt(outputIndex);
    if (block === undefined) {
      startBlock(outputIndex, kind);
      block = blockAt(outputIndex);
      if (block === undefined) {
        return;
      }
    }
    if (block.ended || block.kind !== kind) {
      return;
    }
    block.text += fragment;
    switch (block.kind) {
      case "tool-call":
        pendingChunks.push({
          type: "tool-call-delta",
          index: block.index,
          id: block.callId ?? CallId(""),
          argumentsDelta: fragment,
        });
        return;
      case "text":
        pendingChunks.push({ type: "text-delta", index: block.index, text: fragment });
        return;
      case "reasoning":
        pendingChunks.push({ type: "reasoning-delta", index: block.index, text: fragment });
        return;
    }
  }

  function assembledBlock(block: WireBlock, item?: ResponsesOutputItem): ContentBlock {
    switch (block.kind) {
      case "reasoning":
        return { type: "reasoning", text: block.text };
      case "text":
        return { type: "text", text: block.text };
      case "tool-call":
        return {
          type: "tool-call",
          id: block.callId ?? CallId(item?.call_id ?? ""),
          name: block.name ?? item?.name ?? "",
          arguments: block.text.length > 0 ? block.text : (item?.arguments ?? ""),
        };
    }
  }

  // endBlock emits the assembled terminal block once per block; the
  // content_part.done / output_item.done pair must not double-close.
  function endBlock(outputIndex: number, item?: ResponsesOutputItem): void {
    if (finished) {
      return;
    }
    const block = blockAt(outputIndex);
    if (block === undefined || block.ended) {
      return;
    }
    block.ended = true;
    pendingChunks.push({
      type: "block-end",
      index: block.index,
      block: assembledBlock(block, item),
    });
  }

  function extractUsage(response: ResponsesResponse | undefined): TokenUsage | undefined {
    // OpenAPI examples carry "usage": null / "error": null on lifecycle
    // and completed payloads, so null must be treated as absent.
    const u = response?.usage;
    if (u == null) {
      return undefined;
    }
    const mapped: TokenUsage = {
      inputTokens: u.input_tokens ?? 0,
      outputTokens: u.output_tokens ?? 0,
    };
    const reasoning = u.output_tokens_details?.reasoning_tokens;
    if (reasoning !== undefined) {
      mapped.reasoningTokens = reasoning;
    }
    return mapped;
  }

  // flushFinish emits the buffered usage (before finish) plus the finish
  // chunk; afterwards every further event is ignored — nothing may follow
  // a finish (adapter cookbook protocol obligation).
  function flushFinish(finish: Extract<StreamChunk, { type: "finish" }>): void {
    if (usage !== undefined) {
      pendingChunks.push({ type: "usage", usage });
      usage = undefined;
    }
    pendingChunks.push(finish);
    finished = true;
  }

  function failureOf(response: ResponsesResponse | undefined, fallback: ErrorEvent | undefined): {
    message: string;
    code: string;
  } {
    // OpenAPI examples carry "error": null on non-failed responses, so
    // null must be treated as absent.
    if (response?.error != null) {
      return {
        message: response.error.message ?? "GLM Responses stream failed",
        code: typeof response.error.code === "string" ? response.error.code : "GLM_PROVIDER_ERROR",
      };
    }
    if (fallback != null) {
      return {
        message: fallback.message ?? "GLM Responses stream failed",
        code: typeof fallback.code === "string" ? fallback.code : "GLM_PROVIDER_ERROR",
      };
    }
    return { message: "GLM Responses stream failed", code: "GLM_PROVIDER_ERROR" };
  }

  function mapEvent(evt: ResponsesStreamEvent): void {
    if (finished) {
      return;
    }
    switch (evt.type) {
      case "response.created":
      case "response.in_progress":
        return;
      case "response.output_item.added": {
        const item = evt.item;
        switch (item.type) {
          case "reasoning":
            startBlock(evt.output_index, "reasoning", item);
            return;
          case "message":
            startBlock(evt.output_index, "text", item);
            return;
          case "function_call":
            startBlock(evt.output_index, "tool-call", item);
            return;
          default:
            return;
        }
      }
      case "response.reasoning_summary_part.added":
        // GLM variant tolerance: announce the reasoning block when the
        // provider skips output_item.added; otherwise it is a no-op. The
        // contract scopes this form to summary_text parts; other part
        // types are ignored.
        if (evt.part?.type === "summary_text") {
          startBlock(evt.output_index, "reasoning");
        }
        return;
      case "response.reasoning_summary_text.delta":
      case "response.reasoning_text.delta":
        delta(evt.output_index, "reasoning", evt.delta);
        return;
      case "response.content_part.added":
        // Complementary start form for message items: deduped by
        // startBlock when output_item.added already announced the block.
        // The contract scopes this form to output_text parts; other part
        // types are ignored.
        if (evt.part?.type === "output_text") {
          startBlock(evt.output_index, "text");
        }
        return;
      case "response.output_text.delta":
        delta(evt.output_index, "text", evt.delta);
        return;
      case "response.function_call_arguments.delta":
        delta(evt.output_index, "tool-call", evt.delta);
        return;
      case "response.output_item.done":
        endBlock(evt.output_index, evt.item);
        return;
      case "response.content_part.done":
        endBlock(evt.output_index);
        return;
      case "response.completed":
        usage = extractUsage(evt.response);
        flushFinish({ type: "finish", reason: { kind: sawToolCall ? "tool-calls" : "stop" } });
        return;
      case "response.incomplete":
        usage = extractUsage(evt.response);
        flushFinish({ type: "finish", reason: { kind: "max-tokens" } });
        return;
      case "response.failed":
        flushFinish({
          type: "finish",
          reason: { kind: "error", failure: failureOf(evt.response, undefined) },
        });
        return;
      case "error":
        flushFinish({
          type: "finish",
          reason: { kind: "error", failure: failureOf(undefined, evt) },
        });
        return;
      default:
        // Unknown event types are ignored (forward-compat).
        return;
    }
  }

  return {
    feed(chunk: string): StreamChunk[] {
      parser.feed(chunk);
      const events = pending.splice(0, pending.length);
      for (const message of events) {
        if (finished) {
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
            "GLM Responses stream carried a malformed event payload",
            "GLM_PROTOCOL",
          );
        }
        const record = parsed as { type?: unknown };
        const type = typeof record.type === "string" ? record.type : message.event;
        if (typeof type !== "string") {
          continue;
        }
        mapEvent({ ...record, type } as ResponsesStreamEvent);
      }
      return pendingChunks.splice(0, pendingChunks.length);
    },
  };
}
