/**
 * saolei-mcp.ts — Session-bound saolei MCP server (research.md D3).
 *
 * Builds a `McpServer` from `@modelcontextprotocol/sdk` with the five saolei
 * tools registered per `specs/018-saolei-mcp/contracts/mcp-tool-contract.md`:
 *
 *   - `saolei_init(width, height)`     → fully implemented here (FR-006/FR-027)
 *   - `saolei_click(x, y)`             → fully implemented here (FR-007/FR-013)
 *   - `saolei_flag(x, y)`              → fully implemented here (FR-008/FR-014)
 *   - `saolei_chord_click(x, y)`       → fully implemented here (FR-009/FR-015)
 *   - `saolei_update(cells)`           → fully implemented here (FR-010/
 *                                        FR-011/FR-013..FR-016 — click/flag/
 *                                        chord validator dispatcher)
 *
 * Tool schemas are pinned verbatim against the contract; the
 * `saolei_update` handler delegates to `validateUpdate`, which routes by
 * `lastOp.kind` to the matching pure validator. All rule/business outcomes
 * (accept or reject) are normal MCP results (`isError:false`) so the
 * loopback client sees them via `@langchain/mcp-adapters` without a
 * ToolException (`research.md` D8).
 *
 * The MCP host (`mcp-host.ts`) lazily creates one of these per session and
 * binds a `StreamableHTTPServerTransport` to it; the server's tool handlers
 * close over that session's `OperationBridge` and `GameState`.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import { randomUUID } from "node:crypto";

import type { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import type { Part } from "../../../game_types/projects/game/Part";
import { createGameState } from "./game-state";
import type { GameState } from "./game-state";
import { center } from "./geometry";
import {
  validateChordPreDispatch,
  validateClickPreDispatch,
  validateFlagPreDispatch,
  validateUpdate,
} from "./validation";

/**
 * Wire value of `KeyboardKey.KEYBOARD_KEY_F2` (proto enum string, see
 * `projects/game/game.proto` `enum KeyboardKey`). Hardcoded here so this
 * module's only value-import from `game_types` stays an `import type`,
 * matching the BUILD convention (game_types is type-only at runtime; see
 * `BUILD.bazel` `:lib_test` data comment).
 */
const KEY_F2 = "KEYBOARD_KEY_F2";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_LEFT_CLICK` (proto enum
 * string, `projects/game/game.proto` `enum MouseClickAction`). Dispatched by
 * `saolei_click`. Hardcoded for the same reason as `KEY_F2` — keeps
 * `game_types` a type-only import. Phase 6 adds `RIGHT_CLICK` and
 * `LEFT_RIGHT_PRESS` for `saolei_flag` / `saolei_chord_click`.
 */
const LEFT_CLICK = "MOUSE_CLICK_ACTION_LEFT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_RIGHT_CLICK` (proto enum
 * string, `projects/game/game.proto` `enum MouseClickAction`). Dispatched by
 * `saolei_flag` (FR-008) to toggle a flag on the target cell. Hardcoded for
 * the same reason as `LEFT_CLICK` — keeps `game_types` a type-only import.
 */
const RIGHT_CLICK = "MOUSE_CLICK_ACTION_RIGHT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS` (proto
 * enum string, `projects/game/game.proto` `enum MouseClickAction`). Dispatched
 * by `saolei_chord_click` (FR-009) — a single simultaneous left+right button
 * press (research.md D7: one atomic chord, NOT two separate clicks and NOT a
 * left double-click). Hardcoded for the same reason as `LEFT_CLICK`.
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
 * String status carried by `OperationResult.status` (proto enum). Used to
 * surface the desktop dispatch outcome back to the model.
 */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";

/**
 * String value of `ToolResultStatus.TOOL_RESULT_STATUS_FAILED` (proto enum).
 * Used by `saolei_update` to label a display-only ToolResultPart as a
 * validation rejection when forwarding it via `bridge.pushResult`
 * (specs/021-agent-session-resync/data-model.md §3;
 * specs/021-agent-session-resync/research.md D5).
 */
const STATUS_FAILED = "TOOL_RESULT_STATUS_FAILED";

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
		async (args) => {
			const { x, y } = args;

			// FR-011 alternation: a second operation before the pending
			// `saolei_update` is rejected ("must update first"). The
			// flag stays `true` so the model cannot skip the update.
			if (state.pendingUpdate) {
				return textResult(
					"rejected: a previous operation is awaiting " +
						"saolei_update; call saolei_update before issuing " +
						"another operation",
				);
			}

			// FR-013 pre-dispatch: target MUST be INITIAL. A rejected
			// operation does NOT enter the pending state (Clarification
			// Q3 → A) — the model may retry immediately with a valid
			// target without an intervening `saolei_update`.
			//
			// contracts/mcp-tool-contract.md `saolei_click` Result
			// (reject) prefixes the reason with "rejected: " so the
			// model can distinguish accept vs reject from the text.
			const preResult = validateClickPreDispatch(state, { x, y });
			if (!preResult.ok) {
				return textResult(`rejected: ${preResult.reason}`);
			}

			// FR-007: dispatch LEFT_CLICK via WINDOW_MESSAGE at the cell's
			// window-client centre (`data-model.md` §5 formula in
			// `geometry.center`). Combined move+click Part because window
			// messages carry the coordinate in the WM_* lParam (no
			// separate move step) — research.md D5.
			const { xPx, yPx } = center(x, y);
			const part: Part = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: LEFT_CLICK,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part);

			// FR-011: a dispatched operation (regardless of dispatch
			// outcome — see spec Edge Case "click dispatched but the
			// desktop is disconnected") enters the pending state. The
			// model must call `saolei_update` before the next operation.
			state.pendingUpdate = true;
			state.lastOp = { kind: "click", target: { x, y } };

			// D8: normal MCP text result + optional screenshot so the
			// model can read the post-click board state.
			return resultFromDispatch(
				`click dispatched at (${x},${y}); ` +
					`call saolei_update with the observed cell changes`,
				result,
			);
		},
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
		async (args) => {
			const { x, y } = args;

			// FR-011 alternation: a second operation before the pending
			// `saolei_update` is rejected. The flag stays `true` so the
			// model cannot skip the update.
			if (state.pendingUpdate) {
				return textResult(
					"rejected: a previous operation is awaiting " +
						"saolei_update; call saolei_update before issuing " +
						"another operation",
				);
			}

			// FR-014 pre-dispatch: target MUST be INITIAL. A rejected
			// operation does NOT enter the pending state (Clarification
			// Q3 → A) — the model may retry immediately with a valid
			// target without an intervening `saolei_update`.
			const preResult = validateFlagPreDispatch(state, { x, y });
			if (!preResult.ok) {
				return textResult(`rejected: ${preResult.reason}`);
			}

			// FR-008: dispatch RIGHT_CLICK via WINDOW_MESSAGE at the cell's
			// window-client centre (`data-model.md` §5 formula in
			// `geometry.center`). Combined move+click Part because window
			// messages carry the coordinate in the WM_* lParam (no
			// separate move step) — research.md D5.
			const { xPx, yPx } = center(x, y);
			const part: Part = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: RIGHT_CLICK,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part);

			// FR-011: a dispatched operation enters the pending state. The
			// model must call `saolei_update` before the next operation.
			state.pendingUpdate = true;
			state.lastOp = { kind: "flag", target: { x, y } };

			// D8: normal MCP text result + optional screenshot so the
			// model can read the post-flag board state.
			return resultFromDispatch(
				`flag dispatched at (${x},${y}); ` +
					`call saolei_update with the observed cell changes`,
				result,
			);
		},
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
		async (args) => {
			const { x, y } = args;

			// FR-011 alternation: a second operation before the pending
			// `saolei_update` is rejected. The flag stays `true` so the
			// model cannot skip the update.
			if (state.pendingUpdate) {
				return textResult(
					"rejected: a previous operation is awaiting " +
						"saolei_update; call saolei_update before issuing " +
						"another operation",
				);
			}

			// FR-015 pre-dispatch: target MUST be a non-0 number (1..8)
			// AND adjacent FLAG count == target's number (the "satisfied
			// number" rule, https://rarepike.com/minesweeper/chord-technique/ ).
			// A rejected operation does NOT enter the pending state
			// (Clarification Q3 → A).
			const preResult = validateChordPreDispatch(state, { x, y });
			if (!preResult.ok) {
				return textResult(`rejected: ${preResult.reason}`);
			}

			// FR-009: dispatch LEFT_RIGHT_PRESS via WINDOW_MESSAGE at the
			// cell's window-client centre — a single simultaneous
			// left+right press (research.md D7: NOT two clicks, NOT a
			// double-click). Combined move+click Part because window
			// messages carry the coordinate in the WM_* lParam.
			const { xPx, yPx } = center(x, y);
			const part: Part = {
				mouseMoveAndClick: {
					xPx,
					yPx,
					click: LEFT_RIGHT_PRESS,
					method: WINDOW_MESSAGE,
				},
			};
			const result = await bridge.dispatch(part);

			// FR-011: a dispatched operation enters the pending state. The
			// model must call `saolei_update` before the next operation.
			state.pendingUpdate = true;
			state.lastOp = { kind: "chord_click", target: { x, y } };

			// D8: normal MCP text result + optional screenshot so the
			// model can read the post-chord board state.
			return resultFromDispatch(
				`chord_click dispatched at (${x},${y}); ` +
					`call saolei_update with the observed cell changes`,
				result,
			);
		},
	);

	// ── saolei_update(cells) ─ FR-010 / FR-011 / FR-013..016 ──────────────
	// Phase 5 wires the click validator; Phase 6 routes flag + chord_click
	// through the same `validateUpdate` dispatcher (`validation.ts`).
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
		async (args) => {
			const { cells } = args;

			// FR-011 precondition: an operation must be pending update.
			// `saolei_init` is exempt and does not set this flag, so an
			// `init`-only session correctly rejects `saolei_update`.
			if (!state.pendingUpdate || state.lastOp === null) {
				// Forward a display-only FAILED result so the desktop shows
				// the rejection as a result card (data-model.md §3; D5).
				bridge.pushResult({
					toolId: randomUUID(),
					status: STATUS_FAILED,
					message: "saolei_update rejected: no operation awaiting update",
				});
				return textResult(
					"rejected: no operation awaiting update " +
						"(call saolei_click / saolei_flag / saolei_chord_click first)",
				);
			}

			// FR-013 / FR-014 / FR-015 / FR-016: validate the batch against
			// the recorded operation. `validateUpdate` runs the FR-016 range
			// check, then routes to the validator matching `lastOp.kind`:
			// click (FR-013 connectivity + FR-018 HIT_MINE relaxation),
			// flag (FR-014 single-cell INITIAL↔FLAG), or chord_click
			// (FR-015 adjacency + flag-preservation + connectivity +
			// FR-019 mine-hit exception).
			const lastOp = state.lastOp;
			const result = validateUpdate(state, lastOp, cells);
			if (!result.ok) {
				// State unchanged; `pendingUpdate` stays true so the
				// model must send a corrected `saolei_update` rather
				// than starting a new operation (D8 normal text result).
				// contracts/mcp-tool-contract.md `saolei_update` Result
				// (reject) prefixes the reason with "rejected: ".
				// Forward a display-only FAILED result (data-model.md §3; D5).
				bridge.pushResult({
					toolId: randomUUID(),
					status: STATUS_FAILED,
					message: `saolei_update rejected: ${result.reason}`,
				});
				return textResult(`rejected: ${result.reason}`);
			}

			// Apply the batch to the grid. Order matters only if the
			// model sends duplicate coordinates (the last entry wins);
			// the click validator already verified the rule-consistent
			// shape, so we mutate in place.
			for (const cell of cells) {
				state.grid[cell.y][cell.x] = cell.status;
			}

			// FR-011: clear the pending state; the next operation may
			// now be issued.
			state.pendingUpdate = false;
			state.lastOp = null;

			// Forward a display-only SUCCEEDED result so the desktop renders
			// the update as a result card (data-model.md §3; D5).
			bridge.pushResult({
				toolId: randomUUID(),
				status: STATUS_SUCCEEDED,
				message: "saolei_update: state updated",
			});
			return textResult(
				`state updated; ${cells.length} cells changed; ` +
					`ready for next operation`,
			);
		},
	);

	return { server, state };
}

// Re-export the status constant for tests that assert the F2 dispatch result.
export { STATUS_SUCCEEDED };
