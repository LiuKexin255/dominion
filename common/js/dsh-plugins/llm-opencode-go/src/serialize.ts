/**
 * Request serialization: harness GenerateOptions → OpenAI Chat Completions
 * request body for the opencode-go gateway. Mapping table is contractual:
 * specs/063-llm-reliability-opencode-go/contracts/opencode-go-plugin.md §3;
 * message and tool shapes follow the official chat-completions adapter
 * (node_modules/.pnpm/@deepseek-ai+dsh-llm-deepseek@0.1.1-rc.2_*).
 */

import { LlmError } from "@deepseek-ai/dsh-llm";
import type { ContentBlock, GenerateOptions } from "@deepseek-ai/dsh-llm";

/** The `function` object of one chat-completions tool definition. */
export interface ChatFunctionDefinition {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
}

/**
 * One entry of the request's `tools` array: the OpenAI chat-completions
 * nested shape (`{type:'function', function:{...}}`), unlike the Responses
 * API's flat `FunctionTool`.
 */
export interface ChatFunctionTool {
  type: "function";
  function: ChatFunctionDefinition;
}

/** One tool call replayed on an assistant message. */
export interface ChatToolCall {
  id: string;
  type: "function";
  function: {
    name: string;
    /** Raw JSON arguments string, as produced by the model. */
    arguments: string;
  };
}

/** One message of the chat-completions `messages` array. */
export interface ChatRequestMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string;
  tool_calls?: ChatToolCall[];
  /** Set on `role:'tool'` messages only; joins the result to its call. */
  tool_call_id?: string;
}

/** The request body this adapter posts to `{baseURL}/chat/completions`. */
export interface ChatCompletionsRequest {
  model: string;
  messages: ChatRequestMessage[];
  stream: true;
  stream_options: { include_usage: true };
  tools?: ChatFunctionTool[];
  temperature?: number;
  max_tokens?: number;
  stop?: string[];
}

/**
 * Join the text blocks of one message's content in stream order. The
 * chat-completions wire keeps text-only content in the string form
 * (contract §3), so a message's text blocks collapse into one string.
 */
function flattenText(blocks: ReadonlyArray<{ type: string; text?: string }>): string {
  return blocks
    .filter((block) => block.type === "text")
    .map((block) => block.text ?? "")
    .join("");
}

/**
 * Render a tool result's content into the `tool` message's text. The
 * harness tool surface is text-only (tool outputs render as result text),
 * so text blocks concatenate and any other block type fails loudly instead
 * of silently degrading the model-visible result. An empty result renders
 * as the official adapter's "(no output)" placeholder (contract §3).
 */
function renderToolResultContent(content: ReadonlyArray<ContentBlock>): string {
  const parts: string[] = [];
  for (const block of content) {
    if (block.type === "text") {
      parts.push(block.text);
      continue;
    }
    throw new LlmError(
      `OpenCode Go adapter supports text-only tool result content; got ${block.type}`,
      "UNSUPPORTED_CONTENT",
    );
  }
  const rendered = parts.join("");
  return rendered.length > 0 ? rendered : "(no output)";
}

/**
 * Serialize one fully-assembled model call into the chat-completions
 * request body. Assistant reasoning blocks are intentionally not replayed
 * (the contract's §3 decision: reasoning regenerates each turn). Tool calls
 * replay as assistant `tool_calls`, and each tool result becomes a
 * standalone `role:'tool'` message joined by `tool_call_id`, so a follow-up
 * tool step can carry its results. Unsupported content (images) fails
 * loudly instead of being silently dropped.
 */
export function serializeRequest(options: GenerateOptions): ChatCompletionsRequest {
  if (options.reasoningEffort !== undefined) {
    throw new LlmError(
      "OpenCode Go adapter does not support reasoning effort selection",
      "UNSUPPORTED",
    );
  }

  const messages: ChatRequestMessage[] = [];
  if (options.system !== undefined) {
    messages.push({ role: "system", content: options.system });
  }

  for (const message of options.messages) {
    switch (message.role) {
      case "user": {
        const text = flattenText(message.content);
        const toolMessages: ChatRequestMessage[] = [];
        for (const block of message.content) {
          if (block.type === "text") {
            continue;
          }
          if (block.type === "tool-result") {
            toolMessages.push({
              role: "tool",
              tool_call_id: block.toolCallId,
              content: renderToolResultContent(block.content),
            });
            continue;
          }
          throw new LlmError(
            `OpenCode Go adapter supports text-only user content; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        // Message text first, then the tool outputs — a pure tool-result
        // message (the harness tool-source shape) contributes no empty
        // user message, matching the official adapter.
        if (text.length > 0 || toolMessages.length === 0) {
          messages.push({ role: "user", content: text });
        }
        messages.push(...toolMessages);
        continue;
      }
      case "assistant": {
        const text = flattenText(message.content);
        const toolCalls: ChatToolCall[] = [];
        for (const block of message.content) {
          if (block.type === "text") {
            continue;
          }
          if (block.type === "reasoning") {
            // Reasoning is not replayed (contract §3).
            continue;
          }
          if (block.type === "tool-call") {
            toolCalls.push({
              id: block.id,
              type: "function",
              function: { name: block.name, arguments: block.arguments },
            });
            continue;
          }
          throw new LlmError(
            `OpenCode Go adapter supports text-only assistant history; got ${block.type}`,
            "UNSUPPORTED_CONTENT",
          );
        }
        messages.push({
          role: "assistant",
          content: text,
          ...(toolCalls.length > 0 ? { tool_calls: toolCalls } : {}),
        });
        continue;
      }
      default:
        throw new LlmError(
          `OpenCode Go adapter does not accept ${message.role} messages in the messages array`,
          "UNSUPPORTED_CONTENT",
        );
    }
  }

  const request: ChatCompletionsRequest = {
    model: options.model,
    messages,
    stream: true,
    stream_options: { include_usage: true },
  };
  if (options.tools !== undefined && options.tools.length > 0) {
    request.tools = options.tools.map((tool) => ({
      type: "function" as const,
      function: {
        name: tool.name,
        description: tool.description,
        parameters: tool.parameters,
      },
    }));
  }
  if (options.temperature !== undefined) {
    request.temperature = options.temperature;
  }
  if (options.maxTokens !== undefined) {
    request.max_tokens = options.maxTokens;
  }
  if (options.stop !== undefined) {
    request.stop = options.stop;
  }
  return request;
}
