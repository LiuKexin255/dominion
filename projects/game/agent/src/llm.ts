/**
 * llm.ts — AgentAdapter wrapping LangChain createAgent for text dialog.
 *
 * The adapter receives a pre-created ChatModel (from ModelProviderCache),
 * a systemPrompt, and a checkpointer at construction time.  The compiled
 * agent is created eagerly in the constructor.  generateTurn only needs
 * the threadId and userMessage.
 */

import { info } from "@dominion/common-js-logs";
import type { BaseMessage } from "@langchain/core/messages";
import { HumanMessage } from "@langchain/core/messages";
import type { MemorySaver } from "@langchain/langgraph";
import type { StructuredToolInterface } from "@langchain/core/tools";
import { createAgent, createMiddleware } from "langchain";
import { beforeModelMiddleware } from "./context-middleware";
import { createMouseClickTool, createMouseMoveTool } from "./mouse-tool";
import { DesktopDisconnectedError, type OperationBridge } from "./operation-bridge";
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
 * Map profile `toolNames` entries to LangChain tool instances bound to the
 * session-scoped bridge.  Unknown names are silently skipped.
 */
export function buildTools(
	toolNames: string[],
	bridge: OperationBridge,
): StructuredToolInterface[] {
	const tools: StructuredToolInterface[] = [];
	for (const name of toolNames) {
		if (name === "mouse_move") {
			tools.push(createMouseMoveTool(bridge));
		} else if (name === "mouse_click") {
			tools.push(createMouseClickTool(bridge));
		}
	}
	return tools;
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

/**
 * Strips all SystemMessage entries from state.messages before the model
 * invocation.  This prevents profile-switch contamination when the same
 * thread_id is used across different systemPrompts.
 */
const wrapModelCallMiddleware = createMiddleware({
	name: "StripSystemMessages",
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	wrapModelCall: async (request: any, handler: any) => {
		const state = request?.state;
		if (state?.messages && Array.isArray(state.messages)) {
			const filtered = state.messages.filter(
				(m: any) => m._getType?.() !== "system",
			);
			return handler({
				...request,
				state: { ...state, messages: filtered },
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
// model; the test factory ignores it entirely.  toolNames and bridge are
// forwarded so the adapter can wire LangChain tools (e.g. mouse) at compile
// time.
// ---------------------------------------------------------------------------

export type AdapterFactory = (
	getProvider: () => Promise<ChatModel>,
	systemPrompt: string,
	toolNames: string[],
	bridge: OperationBridge,
	checkpointer: MemorySaver,
) => Promise<AgentAdapter>;

// ---------------------------------------------------------------------------
// AgentAdapterImpl — production implementation
// ---------------------------------------------------------------------------

export class AgentAdapterImpl implements AgentAdapter {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	private readonly agent: any;

	constructor(
		chatModel: ChatModel,
		systemPrompt: string,
		toolNames: string[],
		bridge: OperationBridge,
		checkpointer: MemorySaver,
	) {
		const tools = buildTools(toolNames, bridge);

		// Abort tool execution when the desktop is disconnected. Throwing from
		// wrapToolCall propagates as a middleware error — ToolNode re-throws it
		// (bypassing defaultHandleToolErrors) so the stream errors out and the
		// turn ends without feeding a tool-error ToolMessage back to the LLM.
		const toolAbortOnDisconnect = createMiddleware({
			name: "ToolAbortOnDisconnect",
			wrapToolCall: async (_request, handler) => {
				if (!bridge.hasSink()) {
					throw new DesktopDisconnectedError(
						"desktop disconnected: tool execution aborted",
					);
				}
				return handler(_request);
			},
		});

		info("compiling agent adapter", {
			systemPromptLength: systemPrompt.length,
			toolCount: tools.length,
		});

		this.agent = createAgent({
			model: chatModel,
			systemPrompt,
			tools,
			middleware: [beforeModelMiddleware, wrapModelCallMiddleware, toolAbortOnDisconnect],
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
