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
import { createMouseTool } from "./mouse-tool";
import type { OperationBridge } from "./operation-bridge";
import type { ChatModel } from "./model-provider";

// ---------------------------------------------------------------------------
// ContentBlock types (discriminated union matching LangChain block structure)
// ---------------------------------------------------------------------------

export type ContentBlock =
	| { type: "reasoning"; reasoning: string }
	| { type: "text"; text: string };

/**
 * Per-turn user input for `generateTurn`.
 *
 * `screenshotId` is internal per-turn context for tools (read dynamically via
 * the OperationBridge) and is NEVER included in the HumanMessage content sent
 * to the model.  Only `text` and the `screenshot*` fields become content blocks.
 */
export interface TurnContent {
	text?: string;
	screenshotId?: string;
	screenshotData?: string;
	screenshotMimeType?: string;
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
		if (name === "mouse") {
			tools.push(createMouseTool(bridge));
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
	 * @param content  - Text and/or screenshot blocks for this turn.
	 * @returns Async iterable of ContentBlock in streaming order.
	 */
	generateTurn(
		threadId: string,
		content: TurnContent,
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

		info("compiling agent adapter", {
			systemPromptLength: systemPrompt.length,
			toolCount: tools.length,
		});

		this.agent = createAgent({
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
	): AsyncIterable<ContentBlock> {
		yield* this.streamFromAgent(threadId, content);
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
	): AsyncIterable<ContentBlock> {
		const contentBlocks: { type: string; [key: string]: unknown }[] = [];
		if (content.text) {
			contentBlocks.push({ type: "text", text: content.text });
		}
		if (content.screenshotData && content.screenshotMimeType) {
			contentBlocks.push({
				type: "image",
				source_type: "base64",
				data: content.screenshotData,
				mime_type: content.screenshotMimeType,
			});
		}

		const stream = await this.agent.streamEvents(
			{
				messages: [new HumanMessage({ content: contentBlocks })],
			},
			{
				configurable: { thread_id: threadId },
				version: "v3",
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
