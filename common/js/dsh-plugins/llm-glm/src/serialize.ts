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
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";

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
 * Serialize one fully-assembled model call into the Responses request
 * body. Assistant reasoning blocks are intentionally not replayed (GLM
 * regenerates reasoning each turn; contract §4). Tool calls and results
 * replay as `function_call` / `function_call_output` input items (D11) so
 * a follow-up tool step can carry its results, and tool definitions map to
 * the top-level `tools` array so the model can issue function calls at
 * all. Unsupported content (images) and unsupported options (stop
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
  for (const message of options.messages) {
    switch (message.role) {
      case "user": {
        const content: Array<{ type: "input_text"; text: string }> = [];
        const toolItems: ResponsesInputItem[] = [];
        for (const block of message.content) {
          if (block.type === "text") {
            content.push({ type: "input_text", text: block.text });
            continue;
          }
          if (block.type === "tool-result") {
            toolItems.push({
              type: "function_call_output",
              call_id: block.toolCallId,
              output: renderToolResultContent(block.content),
            });
            continue;
          }
          throw new LlmError(
            `GLM Responses adapter supports text-only user content; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        // Message text first, then the tool outputs — an empty content
        // array (a pure tool-result message) produces no message item.
        if (content.length > 0) {
          input.push({ type: "message", role: "user", content });
        }
        input.push(...toolItems);
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
  if (options.system !== undefined && options.system.length > 0) {
    request.instructions = options.system;
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
