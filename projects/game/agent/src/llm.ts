/**
 * llm.ts — Shared LLM-facing types and helpers for the saolei team graph.
 *
 * Retains the turn/content model shared across the team architecture
 * (`ContentBlock`, `TurnContent`, `toParts`, mouse `buildTools`,
 * `McpClientFactory`/`buildSaoleiMcpTools`) after the single-agent
 * `AgentAdapter`/`AdapterFactory`/`AgentAdapterImpl` path was removed
 * (specs/031-team-template-mode/tasks.md T022 — the team graph's
 * player/planner nodes are built by `team/player.ts` / `team/planner.ts`,
 * and per-session turns are driven by `SessionTeam`'s graph-invoke runner).
 *
 * Contract: specs/031-team-template-mode/contracts/team-graph-contract.md;
 * the turn content model is spec 030's (specs/030-queued-chat-input/
 * research.md D3).
 */

import type { BaseMessage } from "@langchain/core/messages";
import { HumanMessage } from "@langchain/core/messages";
import type { StructuredToolInterface } from "@langchain/core/tools";
import { MultiServerMCPClient } from "@langchain/mcp-adapters";
import { createMouseClickTool } from "./tools/mouse_click/mouse-click";
import { createMouseMoveTool } from "./tools/mouse_move/mouse-move";
import type { OperationBridge } from "./operation-bridge";
import { DEFAULT_MCP_PORT } from "./mcp-host";

/**
 * LangGraph recursion limit (super-steps) per team turn. The framework
 * default of 25 aborts a turn at ~12 model→tool rounds via
 * GraphRecursionError; 1000 permits extended tool chains while still
 * bounding runaway loops.
 */
export const RECURSION_LIMIT = 1000;

/**
 * Chunk-idle timeout for the team graph's player/planner nodes (ms). When
 * the LLM SSE stream stalls (TCP alive but no events), this idle period
 * elapses and LangGraph raises `NodeTimeoutError`, triggering stall recovery
 * (specs/043-llm-stream-stall-recovery/spec.md FR-001). FR-001 requires the
 * idle period to be at least 15s; the 30s default satisfies it and matches
 * the community 15–30s consensus (specs/043-llm-stream-stall-recovery/
 * spec.md FR-011, Assumptions).
 */
export const STREAM_IDLE_TIMEOUT_MS =
	Number(process.env.GAME_STREAM_IDLE_TIMEOUT_MS) || 30_000;

/**
 * Total execution timeout for the async init instruction turn (ms). A
 * stalled planner LLM during `runInitTurn` must degrade within this window
 * instead of hanging and blocking the first user turn
 * (specs/043-llm-stream-stall-recovery/spec.md FR-009/FR-010).
 */
export const INIT_TURN_TIMEOUT_MS =
	Number(process.env.GAME_INIT_TURN_TIMEOUT_MS) || 120_000;

/**
 * Idle-heartbeat interval for MCP tool invocations (ms). While a wrapped MCP
 * client tool's invoke awaits the desktop result (via the MCP server's
 * `bridge.dispatch`), `config.heartbeat()` runs at this cadence to refresh
 * the LangGraph idle timer on the client side — without it, a tool wait
 * longer than `STREAM_IDLE_TIMEOUT_MS` would raise a false `NodeTimeoutError`
 * mid-tool (specs/043-llm-stream-stall-recovery/research.md R7.2). MUST be <
 * `STREAM_IDLE_TIMEOUT_MS` so the idle timer can never elapse during a tool
 * wait; the 10s default satisfies this (specs/043-llm-stream-stall-recovery/
 * research.md R7).
 */
export const TOOL_HEARTBEAT_INTERVAL_MS = 10_000;

// ---------------------------------------------------------------------------
// ContentBlock types (discriminated union matching LangChain block structure)
// ---------------------------------------------------------------------------

export type ContentBlock =
	| { type: "reasoning"; reasoning: string }
	| { type: "text"; text: string }
	| {
			type: "tool_call";
			name: string;
			args: unknown;
			toolCallId: string;
	  }
	| {
			type: "tool_result";
			toolCallId: string;
			status: string;
			message: string;
			screenshot?: {
				data: string;
				widthPx: number;
				heightPx: number;
			};
	  };

/**
 * Wire value of `ToolResultStatus.TOOL_RESULT_STATUS_UNSPECIFIED` (proto enum
 * string). The live `tool_result` block's status defaults to UNSPECIFIED when a
 * tool's `ToolMessage.additional_kwargs.toolResultStatus` is absent — NEVER
 * `FAILED` (spec FR-014/FR-015). The real status is carried into the
 * checkpoint by US2 (`buildToolResultMessage` writes
 * `additional_kwargs.toolResultStatus`), and history reconstruction
 * (handler.ts `ListMessages`) reads the same field off the checkpointed
 * message, so live and history render identically (spec 023 FR-009).
 */
export const STATUS_UNSPECIFIED = "TOOL_RESULT_STATUS_UNSPECIFIED";

/**
 * One text or image fragment of a turn's user input.
 *
 * A multi-part turn (US3 aggregated input,
 * `specs/030-queued-chat-input/research.md` D3) carries N text parts + M
 * image parts in FIFO order. When building model content blocks each text
 * part becomes a `{type:"text"}` block and each image part becomes an
 * `{type:"image_url"}` block immediately followed by its pixel-size
 * annotation block (the existing convention — mouse tool coordinates are
 * interpreted relative to this pixel space).
 */
export interface TurnContentPart {
	text?: string;
	image?: {
		data: string;
		mimeType: string;
		widthPx?: number;
		heightPx?: number;
	};
}

/**
 * Per-turn user input.
 *
 * Two shapes, both accepted (`specs/030-queued-chat-input/research.md` D3):
 * - **Multi-part** (`parts` present): N text parts + M image parts, FIFO. Used
 *   by `combineAll` (`turn-loop.ts`)
 *   when ≥2 queued messages are merged into one aggregated turn. When `parts`
 *   is non-empty it takes precedence over the flat fields.
 * - **Flat single-message** (the legacy fields below): one text + one optional
 *   image. This is the N=1/M∈{0,1} case.
 */
export interface TurnContent {
	parts?: TurnContentPart[];
	text?: string;
	imageData?: string;
	imageMimeType?: string;
	imageWidthPx?: number;
	imageHeightPx?: number;
}

/**
 * Normalize a `TurnContent` into an ordered array of parts (FIFO).
 *
 * - If `parts` is present and non-empty (multi-part aggregated input), return
 *   it as-is.
 * - Otherwise (flat single-message shape) build a one-element parts array from
 *   the flat `text`/`image*` fields. Returns `[]` when neither text nor image
 *   is present.
 */
export function toParts(content: TurnContent): TurnContentPart[] {
	if (content.parts && content.parts.length > 0) {
		return content.parts;
	}
	const part: TurnContentPart = {};
	if (content.text) part.text = content.text;
	if (content.imageData && content.imageMimeType) {
		part.image = {
			data: content.imageData,
			mimeType: content.imageMimeType,
			...(content.imageWidthPx !== undefined
				? { widthPx: content.imageWidthPx }
				: {}),
			...(content.imageHeightPx !== undefined
				? { heightPx: content.imageHeightPx }
				: {}),
		};
	}
	if (!part.text && !part.image) return [];
	return [part];
}

/**
 * Build the model content-block array for a `TurnContent` (the shape carried
 * by the `HumanMessage` the team-turn runner submits to the graph).
 *
 * Each text part → a `{type:"text"}` block; each image part → an
 * `{type:"image_url"}` block immediately followed by its pixel-size
 * annotation block (mouse tool coordinates are interpreted relative to this
 * pixel space — `specs/030-queued-chat-input/research.md` D3; spec 023).
 */
export function buildContentBlocks(
	content: TurnContent,
): { type: string; [key: string]: unknown }[] {
	const contentBlocks: { type: string; [key: string]: unknown }[] = [];
	for (const part of toParts(content)) {
		if (part.text) {
			contentBlocks.push({ type: "text", text: part.text });
		}
		if (part.image) {
			contentBlocks.push({
				type: "image_url",
				image_url: {
					url: `data:${part.image.mimeType};base64,${part.image.data}`,
				},
			});
			const w = part.image.widthPx;
			const h = part.image.heightPx;
			if (
				typeof w === "number" &&
				typeof h === "number" &&
				w > 0 &&
				h > 0
			) {
				contentBlocks.push({
					type: "text",
					text: `[图片像素尺寸：${w}×${h}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]`,
				});
			}
		}
	}
	return contentBlocks;
}

/**
 * Raw mouse tool names that are excluded from a saolei-enabled profile
 * (spec 018-saolei-mcp FR-012). When the profile's `mcp_names` includes
 * `saolei`, the saolei MCP tools replace the raw mouse tools as the
 * LLM-facing operation channel. In the team architecture the saolei template
 * fixes its own tool assembly (FR-028) — the player holds the saolei MCP
 * tools, never the raw mouse tools.
 */
export const MOUSE_TOOL_NAMES: ReadonlySet<string> = new Set([
	"mouse_move",
	"mouse_click",
]);

/**
 * Map tool name entries to LangChain tool instances bound to the
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
// Saolei MCP client tools (player-only, FR-010 / FR-028)
// ---------------------------------------------------------------------------

/**
 * The `MultiServerMCPClient` constructor type. Exported so tests can inject
 * a fake factory (DI seam) without depending on `vi.mock("@langchain/mcp-adapters")`
 * (which the pre-compiled `:lib` bypasses under Bazel `js_test` —
 * `style/javascript.md` §测试).
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
 */
export const defaultMcpClientFactory: McpClientFactory = async (config) => {
	return new MultiServerMCPClient(config as ConstructorParameters<
		typeof MultiServerMCPClient
	>[0]);
};

/**
 * Wrap a LangChain tool so that during its invoke, LangGraph's
 * `config.heartbeat` (installed on the node-attempt config by `wrapConfig`)
 * is called immediately and then every `TOOL_HEARTBEAT_INTERVAL_MS`.
 *
 * The production saolei/memory MCP tools cross the MCP HTTP boundary: the
 * MCP server's `bridge.dispatch` runs in a different async context and has
 * no access to `config.heartbeat` (mcp-adapters `_callTool` forwards only
 * `config.signal`), so the idle timer refresh MUST be driven client-side
 * (specs/043-llm-stream-stall-recovery/research.md R7.1/R7.2). `heartbeat`
 * is present on the tool's invoke config because the ToolNode spreads
 * `...config` into it (langchain `dist/agents/nodes/ToolNode.js:229-241`);
 * LangGraph's `wrapped.heartbeat` calls `scope.touch()`, refreshing
 * `lastProgress` unconditionally (installed `dist/pregel/timeout.js:100-102`).
 *
 * The wrapper returns a `StructuredToolInterface` that shares the original
 * tool's prototype chain (via `Object.create`) — `name`/`description`/
 * `schema`/Runnable methods and `instanceof` are preserved, so it remains
 * acceptable to `createAgent`/`ToolNode` — with only `invoke` overridden on
 * the instance. The interval is cleared in a `finally` block on
 * resolve/reject/abort — no leaked timers. When `config.heartbeat` is absent
 * (non-LangGraph invocation, unit tests), the wrapper degrades to a direct
 * passthrough (no interval).
 */
export function withIdleHeartbeat(
	tool: StructuredToolInterface,
): StructuredToolInterface {
	const wrapped = Object.create(tool) as StructuredToolInterface;
	wrapped.invoke = (async (input, config) => {
		const heartbeat = (
			config as { heartbeat?: () => void } | undefined
		)?.heartbeat;
		if (typeof heartbeat !== "function") {
			return tool.invoke(input, config);
		}
		heartbeat();
		const timer = setInterval(heartbeat, TOOL_HEARTBEAT_INTERVAL_MS);
		try {
			return await tool.invoke(input, config);
		} finally {
			clearInterval(timer);
		}
	}) as StructuredToolInterface["invoke"];
	return wrapped;
}

/**
 * Build the per-session saolei MCP-client tools (FR-002b / FR-010).
 *
 * Constructs a `MultiServerMCPClient` over the loopback streamable-HTTP
 * transport pointing at this session's saolei MCP endpoint and returns its
 * `getTools()` output (LangChain `DynamicStructuredTool[]`). The MCP server
 * bound at `/internal/mcp/{template}/{session}/saolei` (`mcp-host.ts`)
 * supplies the saolei tools; the player node is the ONLY holder (FR-010).
 *
 * The URL is the template-scoped multi-path scheme (R3 —
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
 * §4): each mcp kind owns its own path and the path carries the template;
 * the saolei path is `/internal/mcp/{template}/{session}/saolei` (the
 * former flat `/internal/mcp/{sessionId}` path was migrated — clean break,
 * spec 039 Assumptions).
 *
 * @param template   The template path segment (e.g. `"saolei"`).
 * @param sessionId  The dominion session id (path segment of the MCP URL).
 * @param mcpPort    The MCP host port (default `DEFAULT_MCP_PORT`).
 * @param clientFactory DI seam — defaults to the real
 *   `MultiServerMCPClient`. Tests inject a `vi.fn()` to assert the URL and
 *   to short-circuit the HTTP round-trip.
 */
export async function buildSaoleiMcpTools(
	template: string,
	sessionId: string,
	mcpPort: number,
	clientFactory: McpClientFactory,
): Promise<StructuredToolInterface[]> {
	const client = await clientFactory({
		saolei: {
			transport: "http",
			url: `http://localhost:${mcpPort}/internal/mcp/${template}/${sessionId}/saolei`,
		},
	});
	const tools = await client.getTools();
	return tools.map(withIdleHeartbeat);
}

/**
 * Build the per-session planner memory MCP tools (039 T022 — FR-007/FR-008).
 *
 * Same `MultiServerMCPClient` pattern as {@link buildSaoleiMcpTools}, over
 * the session's memory mcp path `/internal/mcp/{template}/{session}/memory`
 * (`mcp-host.ts` — template-scoped multi-path scheme, R3 — memory-mcp-
 * contract.md §4). The host-side `createMemoryMcpServer` exposes EXACTLY ONE
 * hermes-style `memory` tool (action/content/old_text/operations — no
 * `memory_id`, no `target`); `getTools()` returns it as a LangChain
 * `DynamicStructuredTool`, and the planner node is the ONLY holder (FR-009).
 *
 * The mcp server forwards to the MemoryService via the agent — it NEVER
 * connects to the memory service directly (FR-007, memory-mcp-contract.md
 * §4). The tools are profile-independent and bound to the session's cached
 * host server, so a profile-change rebuild reuses them without reconnecting
 * (team-rebuild-contract.md §3/§4 — same as the saolei tools).
 *
 * @param template   The template path segment (e.g. `"saolei"`).
 * @param sessionId  The dominion session id (path segment of the MCP URL).
 * @param mcpPort    The MCP host port (default `DEFAULT_MCP_PORT`).
 * @param clientFactory DI seam — defaults to the real
 *   `MultiServerMCPClient`. Tests inject a `vi.fn()` to assert the URL and
 *   to short-circuit the HTTP round-trip.
 */
export async function buildMemoryMcpTools(
	template: string,
	sessionId: string,
	mcpPort: number,
	clientFactory: McpClientFactory,
): Promise<StructuredToolInterface[]> {
	const client = await clientFactory({
		memory: {
			transport: "http",
			url: `http://localhost:${mcpPort}/internal/mcp/${template}/${sessionId}/memory`,
		},
	});
	const tools = await client.getTools();
	return tools.map(withIdleHeartbeat);
}

// ---------------------------------------------------------------------------
// History helpers (handler.ts ListMessages / turn-runner message conversion)
// ---------------------------------------------------------------------------

/**
 * Minimal shape of a LangChain tool_call carried on an AIMessage.
 */
export interface ToolCallLike {
	name?: string;
	args?: Record<string, unknown>;
	id?: string;
}

/** Extract tool_calls from a BaseMessage (AIMessage carries them directly). */
export function extractToolCalls(msg: BaseMessage): ToolCallLike[] {
	const calls = (msg as unknown as { tool_calls?: unknown }).tool_calls;
	return Array.isArray(calls) ? (calls as ToolCallLike[]) : [];
}

/**
 * Read the real ToolResultStatus from a ToolMessage's additional_kwargs. The
 * status is carried there by US2 (buildToolResultMessage) so history reflects
 * the actual outcome (spec 023 FR-012..FR-015). Absent → UNSPECIFIED (neutral,
 * NEVER FAILED — no text inference).
 */
export function readToolResultStatus(msg: BaseMessage): string {
	const status = (
		msg as unknown as { additional_kwargs?: { toolResultStatus?: unknown } }
	).additional_kwargs?.toolResultStatus;
	return typeof status === "string" && status.length > 0
		? status
		: STATUS_UNSPECIFIED;
}

// ---------------------------------------------------------------------------
// (Removed in T022 — single-agent adapter path)
//
// `AgentAdapter` / `AdapterStateSnapshot` / `AdapterFactory` /
// `AgentAdapterImpl` / `wrapModelCallMiddleware` and the private stream
// consumers were deleted when the team architecture replaced the single
// agent (specs/031-team-template-mode/research.md D10). Per-session turns are
// now one team graph invoke (`SessionTeam`), and `context-middleware.ts`
// provides the RefreshTeam channel-clearing helpers instead of a
// beforeModel middleware.
// ---------------------------------------------------------------------------

/** Re-export HumanMessage for the turn-runner input construction. */
export { HumanMessage };
