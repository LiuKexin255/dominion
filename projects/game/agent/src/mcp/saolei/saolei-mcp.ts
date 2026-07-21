/**
 * saolei-mcp.ts — Session-bound saolei MCP server (research.md D3).
 *
 * Builds a `McpServer` from `@modelcontextprotocol/sdk` with the five saolei
 * tools registered per `specs/018-saolei-mcp/contracts/mcp-tool-contract.md`:
 *
 *   - `saolei_init(width, height)`     → fully implemented here (FR-006/FR-027)
 *   - `saolei_click(x, y)`             → placeholder (Phase 5 / US2)
 *   - `saolei_flag(x, y)`              → placeholder (Phase 6 / US3)
 *   - `saolei_chord_click(x, y)`       → placeholder (Phase 6 / US3)
 *   - `saolei_update(cells)`           → placeholder (Phase 5 / US2)
 *
 * Tool schemas are pinned verbatim against the contract; placeholders return
 * "not yet implemented" as normal MCP text results so the loopback client
 * sees them via `@langchain/mcp-adapters` without a ToolException
 * (`research.md` D8 — `isError:false` for rule/business outcomes).
 *
 * The MCP host (`mcp-host.ts`) lazily creates one of these per session and
 * binds a `StreamableHTTPServerTransport` to it; the server's tool handlers
 * close over that session's `OperationBridge` and `GameState`.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import type { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import type { Part } from "../../../game_types/projects/game/Part";
import { createGameState } from "./game-state";
import type { GameState } from "./game-state";

/**
 * Wire value of `KeyboardKey.KEYBOARD_KEY_F2` (proto enum string, see
 * `projects/game/game.proto` `enum KeyboardKey`). Hardcoded here so this
 * module's only value-import from `game_types` stays an `import type`,
 * matching the BUILD convention (game_types is type-only at runtime; see
 * `BUILD.bazel` `:lib_test` data comment).
 */
const KEY_F2 = "KEYBOARD_KEY_F2";

/**
 * String status carried by `OperationResult.status` (proto enum). Used to
 * surface the desktop dispatch outcome back to the model.
 */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";

/**
 * Cell status union sent by `saolei_update` (contracts/mcp-tool-contract.md
 * `saolei_update` schema). Validated server-side before any state mutation
 * in Phase 5+.
 */
const UPDATE_STATUS_ENUM = [
	"INITIAL",
	"0", "1", "2", "3", "4", "5", "6", "7", "8",
	"FLAG", "HIT_MINE", "MINE",
] as const;

/**
 * A normal MCP text result (research.md D8). `isError` is omitted/false:
 * the `@langchain/mcp-adapters` TypeScript adapter raises `ToolException`
 * on `isError:true` and never returns the error to the model as a tool
 * message — so rejections use a text block describing the violated rule.
 */
function textResult(text: string): { content: SaoleiContentBlock[] } {
	return { content: [{ type: "text", text }] };
}

/**
 * Content block types returned by saolei tools: text (always) and an
 * optional screenshot image (when the desktop attached one). Defined as a
 * union so tool handlers can build multimodal results without re-declaring
 * the shape; reused by `saolei_init` here and by `saolei_click`/`flag`/
 * `chord_click` in Phase 5/6.
 */
type SaoleiTextBlock = { type: "text"; text: string };
type SaoleiImageBlock = { type: "image"; data: string; mimeType: string };
type SaoleiContentBlock = SaoleiTextBlock | SaoleiImageBlock;

/**
 * Build the optional image content block from a desktop dispatch result.
 * The screenshot is forwarded verbatim so the model can read the post-action
 * board state. Returns `undefined` when the desktop did not include one.
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
 * screenshot image block (research.md D8: result surfacing for accepted
 * operations). Reused by `saolei_init` here and by the cell-operation
 * tools in Phase 5/6 — each accepts/dispatches, then returns its outcome
 * text plus the post-action screenshot the desktop captured.
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
 * Outcome of `createSaoleiMcpServer`: the session-bound `McpServer` plus
 * the freshly initialised `GameState`. The host holds both for the session's
 * lifetime; tool handlers close over them.
 */
export interface SaoleiMcpHandle {
	server: McpServer;
	state: GameState;
}

/**
 * Build a session-bound saolei `McpServer` with all five tools registered.
 *
 * @param bridge The session's `OperationBridge` — used to dispatch the F2
 *   keypress (FR-006) and (in later phases) cell-operation Parts to the
 *   desktop. Passed via DI so tests can inject a fake bridge
 *   (`style/javascript.md` §测试 — DI seam).
 * @param initialState Optional pre-existing state (e.g. when re-initialising
 *   a session). When omitted a fresh, un-initialised state is created;
 *   `saolei_init` populates the grid on first call.
 */
export function createSaoleiMcpServer(
	bridge: OperationBridge,
	initialState?: GameState,
): SaoleiMcpHandle {
	const state: GameState = initialState ?? {
		grid: [],
		width: 0,
		height: 0,
		pendingUpdate: false,
		lastOp: null,
		initialized: false,
	};

	const server = new McpServer(
		{ name: "saolei", version: "0.1.0" },
		{ capabilities: { tools: {} } },
	);

	// ── saolei_init(width, height) ─ FR-006 / FR-027 ──────────────────────
	server.registerTool(
		"saolei_init",
		{
			description:
				"Initialise / restart the minesweeper game for this session. " +
				"Dispatches an F2 keypress (the new-game shortcut) to the bound " +
				"desktop window and resets the per-session board model to a " +
				"`width` x `height` grid (cell counts, not pixels) of INITIAL " +
				"cells. Exempt from the operate-then-update alternation — no " +
				"`saolei_update` is required after it. Re-calling re-dispatches " +
				"F2 and resets state to the new dimensions.",
			inputSchema: {
				width: z
					.number()
					.int()
					.min(1)
					.describe("column count (x in 0..width-1)"),
				height: z
					.number()
					.int()
					.min(1)
					.describe("row count (y in 0..height-1)"),
			},
		},
		async (args) => {
			const { width, height } = args;
			// FR-006 (a): dispatch an F2 KeyboardPressPart through the bridge.
			const part: Part = {
				keyboardPress: { key: KEY_F2 },
			};
			const result = await bridge.dispatch(part);

			// FR-006 (b) / FR-027: reset the board to width×height INITIAL,
			// discarding any prior state (re-init semantics).
			const fresh = createGameState(width, height);
			state.grid = fresh.grid;
			state.width = fresh.width;
			state.height = fresh.height;
			state.pendingUpdate = false;
			state.lastOp = null;
			state.initialized = true;

			// contracts/mcp-tool-contract.md `saolei_init` Result: a text
			// block plus, if the desktop returned a screenshot, an image
			// content block so the model can read the post-init board state.
			// Note (FR-006): saolei_init is exempt from the operate-then-
			// update alternation; pendingUpdate stays false after init.
			return resultFromDispatch(
				`game initialized (F2 dispatched); ` +
					`grid ${width}x${height}, all cells INITIAL`,
				result,
			);
		},
	);

	// ── saolei_click(x, y) ─ FR-007 / FR-013 (Phase 5 / US2) ──────────────
	server.registerTool(
		"saolei_click",
		{
			description:
				"Left-click (reveal) an unopened, unflagged cell at grid " +
				"coordinate (x, y). Top-left origin (0, 0); x = column, y = row. " +
				"After this dispatches, you MUST call `saolei_update` with the " +
				"observed cell changes before any further operation.",
			inputSchema: {
				x: z.number().int().min(0).describe("column index (0-based)"),
				y: z.number().int().min(0).describe("row index (0-based)"),
			},
		},
		async () => textResult("saolei_click: not yet implemented (Phase 5)"),
	);

	// ── saolei_flag(x, y) ─ FR-008 / FR-014 (Phase 6 / US3) ───────────────
	server.registerTool(
		"saolei_flag",
		{
			description:
				"Right-click to toggle a flag on an unopened cell at (x, y). " +
				"Toggles only between INITIAL and FLAG states. After this " +
				"dispatches, you MUST call `saolei_update` before any further " +
				"operation.",
			inputSchema: {
				x: z.number().int().min(0).describe("column index (0-based)"),
				y: z.number().int().min(0).describe("row index (0-based)"),
			},
		},
		async () => textResult("saolei_flag: not yet implemented (Phase 6)"),
	);

	// ── saolei_chord_click(x, y) ─ FR-009 / FR-015 (Phase 6 / US3) ────────
	server.registerTool(
		"saolei_chord_click",
		{
			description:
				"Chord — a single simultaneous left+right button press on a " +
				"satisfied number cell (a non-0 number whose adjacent FLAG count " +
				"equals its number). Reveals all unflagged neighbors. Atomic " +
				"operation, NOT two separate clicks. After this dispatches, you " +
				"MUST call `saolei_update` before any further operation.",
			inputSchema: {
				x: z.number().int().min(0).describe("column index (0-based)"),
				y: z.number().int().min(0).describe("row index (0-based)"),
			},
		},
		async () => textResult("saolei_chord_click: not yet implemented (Phase 6)"),
	);

	// ── saolei_update(cells) ─ FR-010 / FR-013..016 (Phase 5+ / US2..US4) ─
	server.registerTool(
		"saolei_update",
		{
			description:
				"Batch-update cell statuses observed after the most recent " +
				"operation. Required after every successful `saolei_click`, " +
				"`saolei_flag`, or `saolei_chord_click` before the next operation " +
				"is permitted. Status enum: INITIAL; 0..8 (revealed numbers; 0 = " +
				"blank); FLAG; HIT_MINE (mine detonated by this op); MINE (other " +
				"mines shown at game end).",
			inputSchema: {
				cells: z
					.array(
						z.object({
							x: z.number().int().min(0),
							y: z.number().int().min(0),
							status: z.enum(UPDATE_STATUS_ENUM),
						}),
					)
					.min(1)
					.describe(
						"Observed cell changes after the most recent operation. " +
							"Each entry sets grid[y][x] to the given status.",
					),
			},
		},
		async () => textResult("saolei_update: not yet implemented (Phase 5)"),
	);

	return { server, state };
}

// Re-export the status constant for tests that assert the F2 dispatch result.
export { STATUS_SUCCEEDED };
