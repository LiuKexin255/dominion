/**
 * saolei-mcp.ts — Session-bound saolei MCP server (stateless).
 *
 * Builds a `McpServer` from `@modelcontextprotocol/sdk` with exactly four
 * stateless tools, each a pure dispatch-and-return over the session's
 * `OperationBridge`. Per
 * `specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md` §6
 * (stateless saolei) and `specs/023-saolei-mcp-refine/research.md` D7/D12:
 *
 *   - `saolei_init`           → F2 new-game keypress (no width/height)
 *   - `saolei_click(x, y)`    → LEFT_CLICK at the cell centre
 *   - `saolei_flag(x, y)`     → RIGHT_CLICK at the cell centre
 *   - `saolei_chord_click(x, y)` → LEFT_RIGHT_PRESS at the cell centre
 *
 * No per-session grid state, no validation, no `saolei_update`, no
 * operate-then-update alternation
 * (`specs/023-saolei-mcp-refine/spec.md` FR-016..FR-022). Tools are callable
 * back-to-back (FR-021). Each tool dispatches a `FlowPart` via
 * `bridge.dispatch(part, signal)` — the bridge mints the operation-channel id;
 * saolei passes no `toolId` (D10 decoupling). The `signal` is taken from the
 * MCP `RequestHandlerExtra` (the second callback argument; the SDK always
 * provides a non-undefined `extra.signal` — see `@modelcontextprotocol/sdk`
 * `shared/protocol.d.ts` `RequestHandlerExtra.signal: AbortSignal`).
 *
 * Each tool returns MCP content blocks (a status text block + an optional
 * screenshot image block). It does NOT construct a `ToolMessage` and does NOT
 * set `additional_kwargs` — the `@langchain/mcp-adapters` client wraps the
 * blocks into a `ToolMessage` whose status is neutral
 * (`TOOL_RESULT_STATUS_UNSPECIFIED`), which fixes the original spurious-FAILED
 * bug (D12; `specs/023-saolei-mcp-refine/data-model.md` §6).
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import type { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import type { FlowPart } from "../../../game_types/projects/game/FlowPart";
import { center } from "./geometry";

/**
 * Wire value of `KeyboardKey.KEYBOARD_KEY_F2` (proto enum string, see
 * `projects/game/game.proto` `enum KeyboardKey`). Hardcoded here so this
 * module's only value-import from `game_types` stays an `import type`,
 * matching the BUILD convention (game_types is type-only at runtime; see
 * `BUILD.bazel` `:lib_test` data comment). Dispatched by `saolei_init`
 * (FR-019).
 */
const KEY_F2 = "KEYBOARD_KEY_F2";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_LEFT_CLICK` (proto enum
 * string, `projects/game/game.proto` `enum MouseClickAction`). Dispatched by
 * `saolei_click`. Hardcoded to keep `game_types` a type-only import.
 */
const LEFT_CLICK = "MOUSE_CLICK_ACTION_LEFT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_RIGHT_CLICK` (proto enum
 * string). Dispatched by `saolei_flag`. Hardcoded to keep `game_types` a
 * type-only import.
 */
const RIGHT_CLICK = "MOUSE_CLICK_ACTION_RIGHT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS` (proto
 * enum string). Dispatched by `saolei_chord_click` — a single simultaneous
 * left+right button press (research.md D7: one atomic chord, NOT two separate
 * clicks and NOT a left double-click). Hardcoded to keep `game_types` a
 * type-only import.
 */
const LEFT_RIGHT_PRESS = "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS";

/**
 * Wire value of `MouseInputMethod.MOUSE_INPUT_METHOD_WINDOW_MESSAGE` (proto
 * enum string, `projects/game/game.proto` `enum MouseInputMethod`). Saolei
 * cell operations MUST dispatch with `WINDOW_MESSAGE` (research.md D5: the
 * real cursor would visually block cells in the screenshot the model reads);
 * the desktop defaults `UNSPECIFIED` → `SIMULATED` for existing mouse tools.
 */
const WINDOW_MESSAGE = "MOUSE_INPUT_METHOD_WINDOW_MESSAGE";

/**
 * Content block types returned by saolei tools: text (always) and an
 * optional screenshot image (when the desktop attached one). These are plain
 * MCP content blocks; the `@langchain/mcp-adapters` client wraps them into a
 * `ToolMessage` WITHOUT `additional_kwargs`, so the structured status is
 * neutral (D12).
 */
type SaoleiTextBlock = { type: "text"; text: string };
type SaoleiImageBlock = { type: "image"; data: string; mimeType: string };
type SaoleiContentBlock = SaoleiTextBlock | SaoleiImageBlock;

/**
 * Build the optional image content block from a desktop dispatch result.
 * The screenshot is forwarded verbatim so the model can read the post-action
 * board state (FR-020: the desktop-facing contract is unchanged; the returned
 * screenshot is the model's feedback). Returns `undefined` when the desktop
 * did not include one.
 */
function imageBlockFromResult(
	result: OperationResult,
): SaoleiImageBlock | undefined {
	if (!result.screenshot || !result.screenshot.data) return undefined;
	return {
		type: "image",
		data: result.screenshot.data,
		mimeType: "image/png",
	};
}

/**
 * Build a normal MCP result combining a text block with an optional
 * screenshot image block. The text records the dispatch outcome for the
 * model/user to read; the structured `toolResultStatus` is neutral (D12 —
 * the adapter-wrapped `ToolMessage` carries no `additional_kwargs`).
 */
function resultFromDispatch(
	text: string,
	result: OperationResult,
): { content: SaoleiContentBlock[] } {
	const content: SaoleiContentBlock[] = [{ type: "text", text }];
	const image = imageBlockFromResult(result);
	if (image) content.push(image);
	return { content };
}

/**
 * Minimal shape of the MCP `RequestHandlerExtra` consumed by saolei tool
 * handlers. The full type (`RequestHandlerExtra<ServerRequest, ServerNotification>`
 * from `@modelcontextprotocol/sdk` `shared/protocol.d.ts`) always carries a
 * non-undefined `signal: AbortSignal`; we read only that field and forward it
 * to `bridge.dispatch` (T028). Declaring the narrow shape avoids importing the
 * SDK's internal protocol types into this module.
 */
type SaoleiToolExtra = { signal: AbortSignal };

/**
 * Build a session-bound saolei `McpServer` with exactly four stateless tools
 * registered (`specs/023-saolei-mcp-refine/contracts/tool-dispatch-contract.md`
 * §6; `specs/023-saolei-mcp-refine/data-model.md` §7).
 *
 * @param bridge The session's `OperationBridge` — used to dispatch the
 *   FlowPart operations to the desktop. Passed via DI so tests can inject a
 *   fake bridge (`style/javascript.md` §测试 — DI seam).
 * @returns The session-bound `McpServer`. The MCP host (`mcp-host.ts`) lazily
 *   creates one per session and binds a `StreamableHTTPServerTransport` to it.
 */
export function createSaoleiMcpServer(bridge: OperationBridge): McpServer {
	const server = new McpServer(
		{ name: "saolei", version: "0.1.0" },
		{ capabilities: { tools: {} } },
	);

	// ── saolei_init — no arguments ─ FR-019 ──────────────────────────────
	// Drops the prior width/height args (they affected only agent-side state,
	// now removed — spec C11). A bare F2 new-game dispatch; re-calling
	// re-dispatches F2.
	server.registerTool(
		"saolei_init",
		{
			description:
				"Start a new minesweeper game. Dispatches an F2 keypress " +
				"(the new-game shortcut) to the bound desktop window and " +
				"returns the post-init screenshot. Takes no arguments — " +
				"the board bounds are inferred from the returned screenshot. " +
				"Re-calling re-dispatches F2 (restarts the game).",
		},
		async (_extra: SaoleiToolExtra) => {
			const part: FlowPart = {
				keyboardPress: { key: KEY_F2 },
			};
			const result = await bridge.dispatch(part, _extra.signal);
			return resultFromDispatch(
				"saolei_init: F2 dispatched (new game)",
				result,
			);
		},
	);

	// ── saolei_click(x, y) — LEFT_CLICK ─ FR-020 ─────────────────────────
	server.registerTool(
		"saolei_click",
		{
			description:
				"Left-click (reveal) the cell at grid coordinate (x, y). " +
				"Top-left origin (0, 0); x = column, y = row. Dispatches a " +
				"combined move+left-click via window messages at the cell's " +
				"fixed pixel centre and returns the post-action screenshot. " +
				"Callable back-to-back with no intervening step.",
			inputSchema: cellInputSchema(),
		},
		async (args, extra: SaoleiToolExtra) => {
			const { x, y } = args;
			const { xPx, yPx } = center(x, y);
			const part: FlowPart = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: LEFT_CLICK,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part, extra.signal);
			return resultFromDispatch(
				`saolei_click dispatched at (${x},${y})`,
				result,
			);
		},
	);

	// ── saolei_flag(x, y) — RIGHT_CLICK ─ FR-020 ─────────────────────────
	server.registerTool(
		"saolei_flag",
		{
			description:
				"Right-click to toggle a flag on the cell at grid coordinate " +
				"(x, y). Top-left origin (0, 0); x = column, y = row. " +
				"Dispatches a combined move+right-click via window messages " +
				"at the cell's fixed pixel centre and returns the post-action " +
				"screenshot. Callable back-to-back with no intervening step.",
			inputSchema: cellInputSchema(),
		},
		async (args, extra: SaoleiToolExtra) => {
			const { x, y } = args;
			const { xPx, yPx } = center(x, y);
			const part: FlowPart = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: RIGHT_CLICK,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part, extra.signal);
			return resultFromDispatch(
				`saolei_flag dispatched at (${x},${y})`,
				result,
			);
		},
	);

	// ── saolei_chord_click(x, y) — LEFT_RIGHT_PRESS ─ FR-020 ─────────────
	// A single simultaneous left+right button press (research.md D7: NOT two
	// clicks, NOT a double-click).
	server.registerTool(
		"saolei_chord_click",
		{
			description:
				"Chord — a single simultaneous left+right button press on the " +
				"cell at grid coordinate (x, y). Top-left origin (0, 0); x = " +
				"column, y = row. Dispatches a combined move+chord via window " +
				"messages at the cell's fixed pixel centre and returns the " +
				"post-action screenshot. Callable back-to-back with no " +
				"intervening step.",
			inputSchema: cellInputSchema(),
		},
		async (args, extra: SaoleiToolExtra) => {
			const { x, y } = args;
			const { xPx, yPx } = center(x, y);
			const part: FlowPart = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: LEFT_RIGHT_PRESS,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part, extra.signal);
			return resultFromDispatch(
				`saolei_chord_click dispatched at (${x},${y})`,
				result,
			);
		},
	);

	return server;
}

/**
 * Shared zod input schema for the three cell-operation tools
 * (`saolei_click` / `saolei_flag` / `saolei_chord_click`). Top-left origin
 * `(x, y)` grid convention (FR-020; `specs/018-saolei-mcp/contracts/proto-operation-contract.md`).
 * No range upper-bound: with validation removed, an out-of-bounds coordinate
 * dispatches to whatever pixel the fixed formula yields and the returned
 * screenshot is the model's feedback (spec Edge Case — accepted tradeoff).
 */
function cellInputSchema(): {
	x: z.ZodNumber;
	y: z.ZodNumber;
} {
	return {
		x: z.number().int().min(0).describe("column index (0-based)"),
		y: z.number().int().min(0).describe("row index (0-based)"),
	};
}
