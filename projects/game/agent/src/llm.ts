/**
 * llm.ts — AgentAdapter wrapping LangChain createAgent for text dialog.
 *
 * The adapter receives a pre-created ChatModel (from ModelProviderCache),
 * a systemPrompt, and a checkpointer at construction time.  The compiled
 * agent is created eagerly in the constructor.  generateTurn only needs
 * the threadId and userMessage.
 */

import { info, warn } from "@dominion/common-js-logs";
import type { BaseMessage } from "@langchain/core/messages";
import { HumanMessage } from "@langchain/core/messages";
import type { MemorySaver } from "@langchain/langgraph";
import type { StructuredToolInterface } from "@langchain/core/tools";
import { createAgent, createMiddleware } from "langchain";
import { beforeModelMiddleware } from "./context-middleware";
import { createMouseClickTool, createMouseMoveTool } from "./mouse-tool";
import { createSaoleiTools } from "./mcp/saolei/saolei-tools";
import type { SaoleiMcp } from "./mcp/saolei/saolei-mcp";
import type { OperationBridge } from "./operation-bridge";
import type { ChatModel } from "./model-provider";

/**
 * LangGraph recursion limit (super-steps) per agent turn. The framework
 * default of 25 aborts a turn at ~12 model→tool rounds via
 * GraphRecursionError; 1000 permits extended tool chains while still
 * bounding runaway loops.
 */
const RECURSION_LIMIT = 1000;

// ---------------------------------------------------------------------------
// ContentBlock types (discriminated union matching LangChain block structure)
// ---------------------------------------------------------------------------

export type ContentBlock =
	| { type: "reasoning"; reasoning: string }
	| { type: "text"; text: string };

/**
 * Per-turn user input for `generateTurn`.
 *
 * Only `text` and the `image*` fields become content blocks sent to the model.
 * `imageWidthPx`/`imageHeightPx` are used to append a size-annotation text
 * block so the model knows the exact pixel dimensions of the screenshot
 * (mouse tool coordinates are interpreted relative to this pixel space).
 */
export interface TurnContent {
	text?: string;
	imageData?: string;
	imageMimeType?: string;
	imageWidthPx?: number;
	imageHeightPx?: number;
}

/**
 * Resolve profile `toolNames` AND `mcpNames` into a flat LangChain tool array
 * bound to the session-scoped bridge (and, for MCPs, the session-scoped state).
 *
 * This is a name→factory registry (plan.md Changes verdict): individual tools
 * are looked up by name, and each declared MCP contributes its bundled tool
 * set. Adding the saolei MCP adds one registry entry, not a seventh branch.
 * Unknown tool names are silently skipped; unknown MCP names are warned and
 * skipped (FR-035), consistent with existing unknown-tool_names handling.
 *
 * @param toolNames  - Individual tool names (e.g. "mouse_move").
 * @param mcpNames   - MCP bundle names (e.g. "saolei").
 * @param bridge     - The session-scoped OperationBridge.
 * @param saoleiMcp  - The session-scoped SaoleiMcp instance, or null when the
 *                     profile does not declare the saolei MCP.
 */
export function buildTools(
  toolNames: string[],
  mcpNames: string[],
  bridge: OperationBridge,
  saoleiMcp: SaoleiMcp | null,
): StructuredToolInterface[] {
  const tools: StructuredToolInterface[] = [];

  // Individual-tool name → factory registry. Each entry binds the bridge.
  const toolFactories: Record<string, () => StructuredToolInterface> = {
    mouse_move: () => createMouseMoveTool(bridge),
    mouse_click: () => createMouseClickTool(bridge),
  };
  for (const name of toolNames) {
    const factory = toolFactories[name];
    if (factory) {
      tools.push(factory());
    }
  }

  // MCP name → bundled tool set. "saolei" contributes its five tools bound to
  // the session-scoped SaoleiMcp + bridge.
  for (const name of mcpNames) {
    if (name === "saolei") {
      if (saoleiMcp) {
        tools.push(...createSaoleiTools(saoleiMcp, bridge));
      } else {
        warn("saolei mcp declared but no instance provided", { mcpName: name });
      }
    } else {
      warn("unknown mcp name ignored", { mcpName: name });
    }
  }

  return tools;
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

/**
 * Strips STALE SystemMessage entries from the model prompt to prevent
 * cross-profile contamination when the same thread_id is shared across
 * profiles.
 *
 * Per `specs/011-agent-adapter-decouple/research.md` L117-126, createAgent
 * injects the current profile's `systemPrompt` each turn, and conversation
 * history is shared across profiles on the same thread_id (L137-139). When the
 * thread is reused after a profile switch, the prompt may carry a prior
 * profile's SystemMessage; this middleware removes those stale entries so only
 * the current systemPrompt reaches the model. The current systemPrompt is
 * retained because createAgent re-injects it via `request.systemPrompt`
 * (`specs/011-.../plan.md` L255 — wrapModelCall thread_id isolation).
 *
 * The filter targets `request.messages` (the prompt the model actually
 * receives — langchain `ModelRequest`, agents/nodes/types.d.ts), NOT
 * `request.state.messages` (the checkpoint state); filtering the latter leaves
 * the prompt untouched.
 */
const wrapModelCallMiddleware = createMiddleware({
	name: "StripStaleSystemMessages",
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	wrapModelCall: async (request: any, handler: any) => {
		const messages = request?.messages;
		if (Array.isArray(messages)) {
			return handler({
				...request,
				messages: messages.filter(
					(m: any) => m._getType?.() !== "system",
				),
			});
		}
		return handler(request);
	},
});

// ---------------------------------------------------------------------------
// AgentAdapter interface
// ---------------------------------------------------------------------------

export interface AdapterStateSnapshot {
	values: { messages?: BaseMessage[] };
	createdAt?: string;
}

export interface AgentAdapter {
	/**
	 * Generate a single conversational turn from multimodal user input.
	 *
	 * The adapter was compiled at construction time with a specific model,
	 * systemPrompt, tools, and checkpointer.  Only the threadId and the
	 * per-turn content vary.
	 *
	 * @param threadId - Stable checkpoint thread identifier (sessionId).
	 * @param content  - Text and/or image blocks for this turn.
	 * @param signal   - Optional AbortSignal; when aborted, LangGraph cancels
	 *                   the in-flight run.
	 * @returns Async iterable of ContentBlock in streaming order.
	 */
	generateTurn(
		threadId: string,
		content: TurnContent,
		signal?: AbortSignal,
	): AsyncIterable<ContentBlock>;

	/**
	 * Read the checkpoint state for a thread.
	 *
	 * Uses the adapter's own compiled graph so the checkpoint — which was
	 * written by the same graph — is correctly deserialised.  Returns null
	 * when no checkpoint exists for the thread.
	 */
	getState(threadId: string): Promise<AdapterStateSnapshot | null>;

	/** Optional cleanup hook called when the adapter is unbound. */
	cleanup?(): void;
}

// ---------------------------------------------------------------------------
// AdapterFactory — used by SessionAgent to create adapter instances
//
// The factory receives a lazy getProvider callback rather than a pre-fetched
// ChatModel.  The production factory calls getProvider() to obtain the shared
// model; the test factory ignores it entirely.  toolNames, mcpNames, the
// bridge, and the session-scoped SaoleiMcp (null when the profile declares no
// saolei MCP) are forwarded so the adapter can wire LangChain tools at compile
// time.
// ---------------------------------------------------------------------------

export type AdapterFactory = (
  getProvider: () => Promise<ChatModel>,
  systemPrompt: string,
  toolNames: string[],
  mcpNames: string[],
  bridge: OperationBridge,
  saoleiMcp: SaoleiMcp | null,
  checkpointer: MemorySaver,
) => Promise<AgentAdapter>;

// ---------------------------------------------------------------------------
// AgentAdapterImpl — production implementation
// ---------------------------------------------------------------------------

export class AgentAdapterImpl implements AgentAdapter {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  private readonly agent: any;

  /**
   * @param createAgentFn Optional factory overriding `langchain`'s `createAgent`
   *   (dependency-injection seam). Tests inject a `vi.fn()` spy to assert the
   *   `tools`/options passed without relying on module-level `vi.mock("langchain")`,
   *   which the pre-compiled `:lib` bypasses under Bazel `js_test` (see
   *   `style/javascript.md` §测试 and research.md §2). Defaults to the real
   *   `createAgent`.
   */
  constructor(
    chatModel: ChatModel,
    systemPrompt: string,
    toolNames: string[],
    mcpNames: string[],
    bridge: OperationBridge,
    saoleiMcp: SaoleiMcp | null,
    checkpointer: MemorySaver,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    createAgentFn?: (config: any) => any,
  ) {
    const tools = buildTools(toolNames, mcpNames, bridge, saoleiMcp);

    info("compiling agent adapter", {
      systemPromptLength: systemPrompt.length,
      toolCount: tools.length,
      mcpNames: mcpNames.join(","),
    });

		this.agent = (createAgentFn ?? createAgent)({
			model: chatModel,
			systemPrompt,
			tools,
			middleware: [beforeModelMiddleware, wrapModelCallMiddleware],
			checkpointer,
		});
	}

	async *generateTurn(
		threadId: string,
		content: TurnContent,
		signal?: AbortSignal,
	): AsyncIterable<ContentBlock> {
		yield* this.streamFromAgent(threadId, content, signal);
	}

	async getState(threadId: string): Promise<AdapterStateSnapshot | null> {
		const snapshot = await this.agent.getState({
			configurable: { thread_id: threadId },
		});
		if (!snapshot) return null;
		return {
			values: snapshot.values ?? {},
			createdAt: snapshot.createdAt,
		};
	}

	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	private async *streamFromAgent(
		threadId: string,
		content: TurnContent,
		signal?: AbortSignal,
	): AsyncIterable<ContentBlock> {
		const contentBlocks: { type: string; [key: string]: unknown }[] = [];
		const hasImage = !!(content.imageData && content.imageMimeType);

		// User text is required (enforced by desktop SendUserTurn). No
		// synthetic default is injected when text is missing — the caller is
		// responsible for providing meaningful instructions.
		if (content.text) {
			contentBlocks.push({ type: "text", text: content.text });
		}
		if (hasImage) {
			contentBlocks.push({
				type: "image_url",
				image_url: {
					url: `data:${content.imageMimeType};base64,${content.imageData}`,
				},
			});
			// Append an explicit size-annotation text block so the model knows
			// the screenshot's exact pixel dimensions. Mouse tool coordinates
			// are interpreted relative to this pixel space, so telling the
			// model the real width×height prevents it from guessing a
			// different resolution and picking mis-targeted coordinates.
			const w = content.imageWidthPx;
			const h = content.imageHeightPx;
			if (typeof w === "number" && typeof h === "number" && w > 0 && h > 0) {
				contentBlocks.push({
					type: "text",
					text: `[图片像素尺寸：${w}×${h}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`,
				});
			}
		}

		const stream = await this.agent.streamEvents(
			{
				messages: [new HumanMessage({ content: contentBlocks })],
			},
		{
			configurable: { thread_id: threadId },
			metadata: { session_id: threadId },
			version: "v3",
			recursionLimit: RECURSION_LIMIT,
			signal,
		},
		);

		for await (const message of stream.messages) {
			for await (const reasoning of message.reasoning) {
				yield { type: "reasoning", reasoning };
			}
			for await (const text of message.text) {
				yield { type: "text", text };
			}
		}

		await stream.output;
	}
}
