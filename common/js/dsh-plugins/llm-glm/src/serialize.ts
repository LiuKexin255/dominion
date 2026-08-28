/**
 * Request serialization: harness GenerateOptions → OpenAI Responses
 * request body for the GLM codingplan endpoint. Mapping table is
 * contractual: specs/049-agent-v2-dsh-init/contracts/glm-llm-plugin.md §4;
 * input item shapes follow the official Responses schema
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

/** The Responses request body this adapter posts to `{baseURL}/responses`. */
export interface ResponsesRequest {
  model: string;
  instructions?: string;
  input: ResponsesInputMessage[];
  stream: true;
  temperature?: number;
  max_output_tokens?: number;
}

/**
 * Serialize one fully-assembled model call into the Responses request
 * body. Assistant reasoning blocks are intentionally not replayed (GLM
 * regenerates reasoning each turn; contract §4). Unsupported content
 * (images, tool calls/results) and unsupported options (stop sequences,
 * reasoning efforts) fail loudly instead of being silently dropped.
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

  const input: ResponsesInputMessage[] = [];
  for (const message of options.messages) {
    switch (message.role) {
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
        input.push({ type: "message", role: "user", content });
        continue;
      }
      case "assistant": {
        const content: Array<{ type: "output_text"; text: string }> = [];
        for (const block of message.content) {
          if (block.type === "text") {
            content.push({ type: "output_text", text: block.text });
            continue;
          }
          if (block.type === "reasoning") {
            // Reasoning is not replayed (contract §4).
            continue;
          }
          throw new LlmError(
            `GLM Responses adapter supports text-only assistant history; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        // Reasoning is not replayed, so an assistant message that carried
        // only reasoning would serialize to an empty content array — a
        // shape the Responses endpoint can reject. Skip it: the message
        // has no sendable content.
        if (content.length === 0) {
          continue;
        }
        input.push({ type: "message", role: "assistant", content });
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
  return request;
}
