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
import { MultiServerMCPClient } from "@langchain/mcp-adapters";
import { beforeModelMiddleware } from "./context-middleware";
import { createMouseClickTool } from "./tools/mouse_click/mouse-click";
import { createMouseMoveTool } from "./tools/mouse_move/mouse-move";
import type { OperationBridge } from "./operation-bridge";
import type { ChatModel } from "./model-provider";
import { DEFAULT_MCP_PORT } from "./mcp-host";

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
 * Raw mouse tool names that are excluded from a saolei-enabled profile
 * (spec 018-saolei-mcp FR-012). When the profile's `mcp_names` includes
 * `saolei`, the saolei MCP tools replace the raw mouse tools as the
 * LLM-facing operation channel.
 */
export const MOUSE_TOOL_NAMES: ReadonlySet<string> = new Set([
	"mouse_move",
	"mouse_click",
]);

/**
 * Map profile `toolNames` entries to LangChain tool instances bound to the
 * session-scoped bridge. Unknown names are silently skipped.
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
	/**
	 * MCP integrations enabled on the profile (spec 018-saolei-mcp FR-021).
	 * When `saolei` is present the factory builds MCP-client tools and
	 * excludes raw mouse tools (FR-012); otherwise it preserves the
	 * existing mouse-tools behaviour.
	 */
	mcpNames: string[],
	/**
	 * The dominion session id (used to build the per-session MCP endpoint
	 * URL `http://localhost:${MCP_PORT}/internal/mcp/${sessionId}`, FR-001).
	 */
	sessionId: string,
) => Promise<AgentAdapter>;

// ---------------------------------------------------------------------------
// AgentAdapterImpl — production implementation
// ---------------------------------------------------------------------------

/**
 * The `MultiServerMCPClient` constructor type. Exported so tests can inject
 * a fake factory (`research.md` D2 / `style/javascript.md` §测试 — DI seam)
 * without depending on `vi.mock("@langchain/mcp-adapters")` (which the
 * pre-compiled `:lib` bypasses under Bazel `js_test`).
 */
export type McpClientFactory = (
	config: Record<string, unknown>,
) => Promise<{ getTools(): Promise<StructuredToolInterface[]> }>;

/**
 * Production default for `McpClientFactory`: a thin async wrapper over the
 * real `MultiServerMCPClient`. The wrapper is async to match the
 * `Promise<...>` return type — construction itself is sync in the SDK, but
 * wrapping it async gives tests a natural place to inject a stub that
 * resolves on the next tick.
 *
 * Spec 018-saolei-mcp FR-002b / `research.md` D2: the loopback client is
 * the official `@langchain/mcp-adapters` `MultiServerMCPClient`.
 */
const defaultMcpClientFactory: McpClientFactory = async (config) => {
	return new MultiServerMCPClient(config as ConstructorParameters<
		typeof MultiServerMCPClient
	>[0]);
};

/**
 * Build the per-session MCP-client tools for a saolei profile (FR-002b).
 *
 * Constructs a `MultiServerMCPClient` over the loopback streamable-HTTP
 * transport pointing at this session's MCP endpoint and returns its
 * `getTools()` output (LangChain `DynamicStructuredTool[]`). The MCP server
 * bound at `/internal/mcp/{sessionId}` (`mcp-host.ts`) supplies the five
 * saolei tools.
 *
 * @param sessionId   The dominion session id (path segment of the MCP URL).
 * @param mcpPort     The MCP host port (default `DEFAULT_MCP_PORT`).
 * @param clientFactory DI seam — defaults to the real
 *   `MultiServerMCPClient`. Tests inject a `vi.fn()` to assert the URL and
 *   to short-circuit the HTTP round-trip.
 */
async function buildSaoleiMcpTools(
	sessionId: string,
	mcpPort: number,
	clientFactory: McpClientFactory,
): Promise<StructuredToolInterface[]> {
	const client = await clientFactory({
		saolei: {
			transport: "http",
			url: `http://localhost:${mcpPort}/internal/mcp/${sessionId}`,
		},
	});
	return client.getTools();
}

export class AgentAdapterImpl implements AgentAdapter {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	private readonly agent: any;

	/**
	 * Sync constructor: compiles the LangGraph agent eagerly. Used for the
	 * non-saolei code path (mouse tools) and as the tail of the async
	 * `create()` factory after MCP tools are resolved.
	 *
	 * @param createAgentFn Optional factory overriding `langchain`'s `createAgent`
	 *   (dependency-injection seam). Tests inject a `vi.fn()` spy to assert the
	 *   `tools`/options passed without relying on module-level `vi.mock("langchain")`,
	 *   which the pre-compiled `:lib` bypasses under Bazel `js_test` (see
	 *   `style/javascript.md` §测试 and research.md §2). Defaults to the real
	 *   `createAgent`.
	 * @param tools Pre-built tool list (defaults to `buildTools(toolNames, bridge)`).
	 *   The async `create()` factory supplies this when the saolei profile
	 *   merges MCP-client tools with the (mouse-filtered) native tool list.
	 */
	constructor(
		chatModel: ChatModel,
		systemPrompt: string,
		toolNames: string[],
		bridge: OperationBridge,
		checkpointer: MemorySaver,
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		createAgentFn?: (config: any) => any,
		tools?: StructuredToolInterface[],
	) {
		const resolvedTools = tools ?? buildTools(toolNames, bridge);

		info("compiling agent adapter", {
			systemPromptLength: systemPrompt.length,
			toolCount: resolvedTools.length,
		});

		this.agent = (createAgentFn ?? createAgent)({
			model: chatModel,
			systemPrompt,
			tools: resolvedTools,
			middleware: [beforeModelMiddleware, wrapModelCallMiddleware],
			checkpointer,
		});
	}

	/**
	 * Async factory that resolves the saolei MCP-client tools (when the
	 * profile has `mcp_names` including `saolei`) and then constructs the
	 * adapter with the merged tool list. For non-saolei profiles this
	 * delegates straight to the sync constructor (existing mouse-tools
	 * behaviour, unchanged — FR-012 backward compatibility).
	 *
	 * Spec 018-saolei-mcp FR-002b / FR-012:
	 *   - When `mcpNames` contains `"saolei"`: mouse tools are excluded
	 *     from the native tool list and saolei tools come from the MCP
	 *     client (the loopback `MultiServerMCPClient`).
	 *   - Otherwise: native mouse tools are added as today; no MCP client
	 *     is built.
	 *
	 * @param clientFactory DI seam for the MCP client (see `McpClientFactory`).
	 *   Defaults to a wrapper over the real `MultiServerMCPClient`. Tests
	 *   inject a `vi.fn()` to assert the URL and stub `getTools()`.
	 */
	static async create(
		chatModel: ChatModel,
		systemPrompt: string,
		toolNames: string[],
		bridge: OperationBridge,
		checkpointer: MemorySaver,
		mcpNames: string[],
		sessionId: string,
		opts: {
			// eslint-disable-next-line @typescript-eslint/no-explicit-any
			createAgentFn?: (config: any) => any;
			mcpClientFactory?: McpClientFactory;
			mcpPort?: number;
		} = {},
	): Promise<AgentAdapterImpl> {
		const isSaolei = mcpNames.includes("saolei");
		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const createAgentFn = opts.createAgentFn;

		if (!isSaolei) {
			// Backward-compatible path: existing mouse tools, no MCP client.
			return new AgentAdapterImpl(
				chatModel,
				systemPrompt,
				toolNames,
				bridge,
				checkpointer,
				createAgentFn,
			);
		}

		// FR-012: exclude mouse tools for saolei profiles.
		const filteredToolNames = toolNames.filter(
			(n) => !MOUSE_TOOL_NAMES.has(n),
		);
		const nativeTools = buildTools(filteredToolNames, bridge);

		// FR-002b: fetch saolei tools from the loopback MCP client.
		const factory =
			opts.mcpClientFactory ?? defaultMcpClientFactory;
		const port = opts.mcpPort ?? DEFAULT_MCP_PORT;
		const mcpTools = await buildSaoleiMcpTools(
			sessionId,
			port,
			factory,
		);

		info("saolei profile adapter", {
			sessionId,
			mcpPort: port,
			nativeToolCount: nativeTools.length,
			mcpToolCount: mcpTools.length,
		});

		return new AgentAdapterImpl(
			chatModel,
			systemPrompt,
			filteredToolNames,
			bridge,
			checkpointer,
			createAgentFn,
			[...nativeTools, ...mcpTools],
		);
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
