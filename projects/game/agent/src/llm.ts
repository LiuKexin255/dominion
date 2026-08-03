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
 * Build the per-session saolei MCP-client tools (FR-002b / FR-010).
 *
 * Constructs a `MultiServerMCPClient` over the loopback streamable-HTTP
 * transport pointing at this session's MCP endpoint and returns its
 * `getTools()` output (LangChain `DynamicStructuredTool[]`). The MCP server
 * bound at `/internal/mcp/{sessionId}` (`mcp-host.ts`) supplies the saolei
 * tools; the player node is the ONLY holder (FR-010).
 *
 * @param sessionId   The dominion session id (path segment of the MCP URL).
 * @param mcpPort     The MCP host port (default `DEFAULT_MCP_PORT`).
 * @param clientFactory DI seam — defaults to the real
 *   `MultiServerMCPClient`. Tests inject a `vi.fn()` to assert the URL and
 *   to short-circuit the HTTP round-trip.
 */
export async function buildSaoleiMcpTools(
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
