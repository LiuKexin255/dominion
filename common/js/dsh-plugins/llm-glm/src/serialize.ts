/**
 * Request serialization: harness GenerateOptions → OpenAI Responses
 * request body for the GLM codingplan endpoint. Mapping table is
 * contractual: specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §4
 * (tool items per specs/051-agent-v2-dsh-migration/research.md D11, tool
 * definitions per
 * specs/054-agent-v2-bugfixes/revisions/t029-glm-tools-serialization.md);
 * input item and tool shapes follow the official Responses schema
 * (https://github.com/openai/openai-openapi/blob/main/openapi.yaml).
 */

import { LlmError } from "@deepseek-ai/dsh-llm";
import type { ContentBlock, GenerateOptions } from "@deepseek-ai/dsh-llm";

/** One message item of the Responses `input` array. */
export interface ResponsesInputMessage {
  type: "message";
  role: "user" | "assistant";
  content: Array<{ type: "input_text" | "output_text"; text: string }>;
}

/**
 * One function tool call the model issued in a previous step, replayed so
 * the next request's history stays consistent with its function_call_output
 * (OpenAI Responses `function_call` input item).
 */
export interface ResponsesFunctionCallItem {
  type: "function_call";
  call_id: string;
  name: string;
  /** Raw JSON arguments string, as produced by the model. */
  arguments: string;
}

/**
 * The rendered result of one function tool call, joined to its call by
 * call_id (OpenAI Responses `function_call_output` input item).
 */
export interface ResponsesFunctionCallOutputItem {
  type: "function_call_output";
  call_id: string;
  output: string;
}

/** One item of the Responses `input` array. */
export type ResponsesInputItem =
  | ResponsesInputMessage
  | ResponsesFunctionCallItem
  | ResponsesFunctionCallOutputItem;

/**
 * One entry of the Responses `tools` array: the OpenAI Responses API
 * `FunctionTool` flat shape (`type`/`name`/`parameters` at the top level,
 * unlike chat-completions' nested `function` object;
 * https://github.com/openai/openai-openapi `FunctionTool` schema).
 */
export interface ResponsesFunctionTool {
  type: "function";
  name: string;
  description: string;
  parameters: Record<string, unknown>;
}

/** The Responses request body this adapter posts to `{baseURL}/responses`. */
export interface ResponsesRequest {
  model: string;
  instructions?: string;
  input: ResponsesInputItem[];
  stream: true;
  tools?: ResponsesFunctionTool[];
  temperature?: number;
  max_output_tokens?: number;
}

/**
 * Render a tool result's content into the function_call_output text. The
 * harness tool surface is text-only (tool outputs render as result text),
 * so text blocks concatenate and any other block type fails loudly instead
 * of silently degrading the model-visible result.
 */
function renderToolResultContent(content: ReadonlyArray<{ type: string; text?: string }>): string {
  const parts: string[] = [];
  for (const block of content) {
    if (block.type === "text") {
      parts.push(block.text ?? "");
      continue;
    }
    throw new LlmError(
      `GLM Responses adapter supports text-only tool result content; got ${block.type}`,
      "UNSUPPORTED_CONTENT",
    );
  }
  return parts.join("\n");
}

/**
 * Render one system message's content to the `instructions` text. The
 * rendered system prompt is text-only; any other block type fails loudly
 * instead of silently dropping instructions.
 */
function renderSystemText(content: ReadonlyArray<ContentBlock>): string {
  const parts: string[] = [];
  for (const block of content) {
    if (block.type === "text") {
      parts.push(block.text);
      continue;
    }
    throw new LlmError(
      `GLM Responses adapter supports text-only system content; got ${block.type}`,
      "UNSUPPORTED_CONTENT",
    );
  }
  return parts.join("");
}

/**
 * Serialize one fully-assembled model call into the Responses request
 * body. A system-role message maps to the top-level `instructions` text
 * (the loop-built request carries the rendered prompt as a leading
 * system message). Assistant reasoning blocks are intentionally not
 * replayed (GLM regenerates reasoning each turn; contract §4). Tool calls
 * replay as `function_call` items and a tool-role result message replays
 * as `function_call_output` (D11) so a follow-up tool step can carry its
 * results, and tool definitions map to the top-level `tools` array so the
 * model can issue function calls at all. Unsupported content (images,
 * mid-conversation system messages) and unsupported options (stop
 * sequences, reasoning efforts) fail loudly instead of being silently
 * dropped.
 */
export function serializeRequest(options: GenerateOptions): ResponsesRequest {
  if (options.stop !== undefined && options.stop.length > 0) {
    throw new LlmError(
      "GLM Responses adapter does not support stop sequences",
      "UNSUPPORTED",
    );
  }
  if (options.reasoningEffort !== undefined) {
    throw new LlmError(
      "GLM Responses adapter does not support reasoning effort selection",
      "UNSUPPORTED",
    );
  }

  const input: ResponsesInputItem[] = [];
  const systemTexts: string[] = [];
  for (const message of options.messages) {
    switch (message.role) {
      case "system": {
        // The Responses `instructions` slot can only express one prompt, so
        // system-role messages join into it only while they form the leading
        // run, before any input item has been pushed. A system message
        // positioned after any input item fails loudly rather than being
        // folded into the one-shot prompt out of position.
        if (input.length > 0) {
          throw new LlmError(
            "GLM Responses adapter does not support mid-conversation system messages",
            "UNSUPPORTED_CONTENT",
          );
        }
        const text = renderSystemText(message.content);
        if (text.length > 0) {
          systemTexts.push(text);
        }
        continue;
      }
      case "user": {
        const content: Array<{ type: "input_text"; text: string }> = [];
        for (const block of message.content) {
          if (block.type === "text") {
            content.push({ type: "input_text", text: block.text });
            continue;
          }
          throw new LlmError(
            `GLM Responses adapter supports text-only user content; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        // An empty user message produces no item; the Responses endpoint
        // rejects an empty content array.
        if (content.length > 0) {
          input.push({ type: "message", role: "user", content });
        }
        continue;
      }
      case "tool": {
        input.push({
          type: "function_call_output",
          call_id: message.toolCallId,
          output: renderToolResultContent(message.content),
        });
        continue;
      }
      case "assistant": {
        const content: Array<{ type: "output_text"; text: string }> = [];
        const toolItems: ResponsesInputItem[] = [];
        for (const block of message.content) {
          if (block.type === "text") {
            content.push({ type: "output_text", text: block.text });
            continue;
          }
          if (block.type === "reasoning") {
            // Reasoning is not replayed (contract §4).
            continue;
          }
          if (block.type === "tool-call") {
            toolItems.push({
              type: "function_call",
              call_id: block.id,
              name: block.name,
              arguments: block.arguments,
            });
            continue;
          }
          throw new LlmError(
            `GLM Responses adapter supports text-only assistant history; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        // Assistant text message first, then its function calls (matching
        // the model's output-item order); reasoning-only messages produce
        // no message item — an empty content array is a shape the
        // Responses endpoint can reject.
        if (content.length > 0) {
          input.push({ type: "message", role: "assistant", content });
        }
        input.push(...toolItems);
        continue;
      }
      default:
        throw new LlmError(
          `GLM Responses adapter does not accept ${message.role} messages in the input array`,
          "UNSUPPORTED_CONTENT",
        );
    }
  }

  const request: ResponsesRequest = {
    model: options.model,
    input,
    stream: true,
  };
  // The system slot joins the one-shot `options.system` with any rendered
  // system-role message, mirroring the official adapter's `[options.system,
  // historySystem].join("\n\n")` precedence.
  const instructions = [options.system, ...systemTexts]
    .filter((text): text is string => text !== undefined && text.length > 0)
    .join("\n\n");
  if (instructions.length > 0) {
    request.instructions = instructions;
  }
  if (options.temperature !== undefined) {
    request.temperature = options.temperature;
  }
  if (options.maxTokens !== undefined) {
    request.max_output_tokens = options.maxTokens;
  }
  // Tool definitions ride the top-level `tools` array in the flat
  // FunctionTool shape (dsh-llm GenerateOptions.tools contract: "adapters
  // map to the provider's tools field"). A `strict` flag is never emitted:
  // the saolei dual-form schemas do not satisfy strict mode's constraints,
  // and an explicit `strict:false` is semantically equal to omitting the
  // field
  // (specs/054-agent-v2-bugfixes/revisions/t029-glm-tools-serialization.md §1).
  if (options.tools !== undefined && options.tools.length > 0) {
    request.tools = options.tools.map((tool) => ({
      type: "function" as const,
      name: tool.name,
      description: tool.description,
      parameters: tool.parameters,
    }));
  }
  return request;
}
