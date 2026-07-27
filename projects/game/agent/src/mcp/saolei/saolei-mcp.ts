/**
 * saolei-mcp.ts — Session-bound saolei MCP server with recognized text-board
 * state and strict pre-dispatch validation.
 *
 * Reverses [023 — Saolei MCP Refine] for the recognition + validation
 * dimension (spec 025 spec.md Relationship): the saolei tools no longer
 * return the raw screenshot for the model to read pixels from. Instead, each
 * tool recognizes the screenshot deterministically with
 * `@dominion/game-saolei-board` and returns a compact **text** board, and each
 * cell operation is validated **strictly** against the latest recognized state
 * before dispatch — illegal moves are rejected with a reason code and never
 * reach the desktop (spec 025 FR-012..FR-018, FR-020;
 * `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`).
 *
 * Tool surface is unchanged (FR-020): exactly four tools
 *   - `saolei_init`              → F2 new-game keypress; recognizes the first
 *                                 screenshot and seeds the session board.
 *   - `saolei_click(x, y)`       → LEFT_CLICK at the cell centre.
 *   - `saolei_flag(x, y)`        → RIGHT_CLICK at the cell centre.
 *   - `saolei_chord_click(x, y)` → LEFT_RIGHT_PRESS at the cell centre.
 *
 * Coordinate-space discipline (contract §5): recognition reads pixels in
 * **screenshot** space (originY 200, includes non-client chrome); the cell
 * tools dispatch clicks in **client** space via `./geometry.ts` `center()`
 * (originY 104, chrome subtracted). The two must not be mixed
 * (`projects/game/pkg/saolei-board/README.md` → "坐标空间注意").
 *
 * DI seam (`style/javascript.md` §测试 / [vitest Mocking Modules — Pitfalls]
 * (https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)): the
 * recognition engine is injected via `boardApi` so tests supply canned
 * `GameState`s / simulated recognition failures instead of real PNG bytes,
 * with NO cross-package `vi.mock`. The fake `OperationBridge` is the existing
 * DI seam. Every tool returns a single MCP **text** content block (MCP Tools
 * content blocks — https://modelcontextprotocol.io/docs/concepts/tools); the
 * structured status stays neutral (023 C15 — `TOOL_RESULT_STATUS_UNSPECIFIED`),
 * so a rejected move is a normal tool result the model can act on, not a tool
 * error that aborts the turn.
 *
 * Game-status surfacing + post-win terminal (spec 027 US4 / FR-012..015,
 * FR-021..023; `specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-
 * contract.md`): every tool-result body whose state is recognized carries a
 * `game status: won|lost|playing` line (derived loss-first via
 * `isTerminalState`, then `isWin` from `@dominion/game-saolei-board`, else
 * `playing`), and a recognized win is a terminal state symmetric with a loss
 * — any cell operation attempted after a recognized win is rejected before
 * dispatch with `game_won` (mirroring the existing loss `game_over`).
 * `saolei_init` is never terminal-blocked (it restarts the game).
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import { SaoleiBoard, isWin, renderBoardText } from "@dominion/game-saolei-board";
import type { CellStatus, GameState } from "@dominion/game-saolei-board";

import type { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import type { FlowPart } from "../../../game_types/projects/game/FlowPart";
import type { MouseClickAction } from "../../../game_types/projects/game/MouseClickAction";
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
 * real cursor would visually block cells in the screenshot the recognizer
 * reads); the desktop defaults `UNSPECIFIED` → `SIMULATED` for existing mouse
 * tools.
 */
const WINDOW_MESSAGE = "MOUSE_INPUT_METHOD_WINDOW_MESSAGE";

/** A cell-operation tool name (the three tools that take `(x, y)`). */
export type CellTool = "saolei_click" | "saolei_flag" | "saolei_chord_click";

/**
 * Verdict of a strict pre-dispatch validation pass
 * (`contracts/saolei-mcp-contract.md` §4). `ok: false` carries a stable reason
 * code the model can act on (FR-016).
 */
export type MoveVerdict = { ok: true } | { ok: false; reason: MoveRejection };

/**
 * Stable reason codes for a rejected move, mirroring the rule table in
 * `specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md`
 * §4. Surfaced verbatim to the model in the rejection outcome line so the
 * model can choose a different move.
 */
export type MoveRejection =
	| "no_active_game"
	| "out_of_bounds"
	| "game_over"
	| "game_won"
	| "cell_already_revealed"
	| "cell_is_flagged"
	| "cannot_flag_revealed"
	| "chord_requires_number"
	| "chord_no_unrevealed_neighbor";

/** Revealed numeric cell statuses (permanent within a game). */
const REVEALED_NUMBERS: ReadonlySet<CellStatus> = new Set<CellStatus>([
	"0",
	"1",
	"2",
	"3",
	"4",
	"5",
	"6",
	"7",
	"8",
]);

/** Chordable numbers: a chord is permitted only on a revealed `1`–`8`. */
const CHORD_NUMBERS: ReadonlySet<CellStatus> = new Set<CellStatus>([
	"1",
	"2",
	"3",
	"4",
	"5",
	"6",
	"7",
	"8",
]);

/**
 * Terminal-LOSS indicators: `HIT_MINE` (the triggered mine, red background) and
 * `MINE` (an end-game revealed mine, grey background) only appear once the
 * game is lost — classic Minesweeper reveals all mines on a loss, and never
 * surfaces a bare `MINE` while the game is in progress. A win does not expose
 * mines as `MINE`/`HIT_MINE` (they are auto-flagged as `F`), so the presence
 * of either symbol is a definitive terminal-LOSS signal. `isTerminalState`
 * below uses this set; a terminal-WIN is detected separately via `isWin`
 * (`specs/027-chat-bubble-game-state/data-model.md` §3 — loss takes precedence
 * but both are terminal, surfacing `game_over` and `game_won` respectively).
 */
const TERMINAL_CELLS: ReadonlySet<CellStatus> = new Set<CellStatus>([
	"HIT_MINE",
	"MINE",
]);

/**
 * The 8 Moore-neighbor offsets (row-major, top row first), bounded by the
 * board dims at call time (`specs/027-chat-bubble-game-state/data-model.md`
 * §4 — `neighbors` helper).
 */
const NEIGHBOR_OFFSETS: ReadonlyArray<readonly [number, number]> = [
	[-1, -1],
	[0, -1],
	[1, -1],
	[-1, 0],
	[1, 0],
	[-1, 1],
	[0, 1],
	[1, 1],
];

/**
 * Whether a recognized state is a terminal LOSS. True when any cell is a
 * revealed mine (`HIT_MINE` / `MINE`) — see `TERMINAL_CELLS`. A terminal WIN
 * is detected separately via `isWin` (the post-win `game_won` check in
 * `validateMove`); this helper stays loss-only so the two terminal reasons
 * stay disjoint and mutually exclusive (FR-022).
 */
export function isTerminalState(state: GameState): boolean {
	for (const row of state.grid) {
		for (const cell of row) {
			if (TERMINAL_CELLS.has(cell)) return true;
		}
	}
	return false;
}

/**
 * The game-status token emitted in every tool-result body (FR-012..015; `specs/
 * 027-chat-bubble-game-state/data-model.md` §3, `specs/027-chat-bubble-game-
 * state/contracts/saolei-mcp-status-contract.md` §3).
 */
type GameStatus = "won" | "lost" | "playing";

/**
 * Derive the game status from a recognized state. Loss takes precedence over
 * win: a board with `HIT_MINE`/`MINE` is a loss (and `isWin` returns `false`
 * for it anyway), but loss-first is explicit so the "loss takes precedence"
 * edge case is unambiguous (`specs/027-chat-bubble-game-state/data-model.md`
 * §3 / research.md D7). Pure function of `state`.
 */
function gameStatus(state: GameState): GameStatus {
	if (isTerminalState(state)) return "lost";
	if (isWin(state)) return "won";
	return "playing";
}

/**
 * In-bounds Moore neighbors of `(x, y)`. Returns up to 8 cell statuses
 * (fewer on the board edge — 5 on an edge, 3 on a corner), intersecting
 * `NEIGHBOR_OFFSETS` with `[0, width) × [0, height)`. Indexing follows the
 * `GameState.grid` order `[y][x]` (`projects/game/pkg/saolei-board/src/core/
 * types.ts`). Pure function of `state`, `x`, `y`
 * (`specs/027-chat-bubble-game-state/data-model.md` §4).
 */
function neighbors(state: GameState, x: number, y: number): CellStatus[] {
	const out: CellStatus[] = [];
	for (const [dx, dy] of NEIGHBOR_OFFSETS) {
		const nx = x + dx;
		const ny = y + dy;
		if (nx >= 0 && ny >= 0 && nx < state.width && ny < state.height) {
			out.push(state.grid[ny][nx]);
		}
	}
	return out;
}

/**
 * True iff some in-bounds Moore neighbor of `(x, y)` is `INITIAL` or
 * `UNKNOWN` (`specs/027-chat-bubble-game-state/data-model.md` §4 /
 * `specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md`
 * §5.1). `FLAG` / revealed-number / `HIT_MINE` / `MINE` neighbors do NOT
 * count — a chord acts only on `INITIAL` cells and is lenient on `UNKNOWN`
 * per 025 FR-018 (an `UNKNOWN` neighbor is treated as possibly unrevealed).
 */
function hasInitialOrUnknownNeighbor(
	state: GameState,
	x: number,
	y: number,
): boolean {
	return neighbors(state, x, y).some(
		(c) => c === "INITIAL" || c === "UNKNOWN",
	);
}

/**
 * Strict pre-dispatch validation (FR-014/FR-015,
 * `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md` §4;
 * `specs/027-chat-bubble-game-state/data-model.md` §4 rule order). Judges
 * **target-cell compatibility** — never predicted outcome (so a chord whose
 * adjacent-flag count ≠ the number is still legal and NOT rejected; FR-015e).
 * Pure: takes the recognized state and the requested move, returns a verdict
 * with a stable reason code.
 *
 * Check order: structural (out-of-bounds) → state-level (terminal loss
 * `game_over`, then terminal win `game_won` — FR-021..023, mutually exclusive
 * but loss-first per `specs/027-chat-bubble-game-state/contracts/saolei-mcp-
 * status-contract.md` §5) → cell-specific (per-tool rule). `UNKNOWN` targets
 * are always lenient (FR-018): a move is never rejected solely because the
 * target cell's recognition is uncertain.
 *
 * `no_active_game` is NOT produced here — it is a session-level check (no
 * recognized state exists) handled by the caller before invoking this helper.
 */
export function validateMove(
	state: GameState,
	tool: CellTool,
	x: number,
	y: number,
): MoveVerdict {
	if (x < 0 || y < 0 || x >= state.width || y >= state.height) {
		return { ok: false, reason: "out_of_bounds" };
	}
	if (isTerminalState(state)) {
		return { ok: false, reason: "game_over" };
	}
	if (isWin(state)) {
		return { ok: false, reason: "game_won" };
	}
	const cell = state.grid[y][x];
	// FR-018: never reject solely on recognition uncertainty.
	if (cell === "UNKNOWN") {
		return { ok: true };
	}
	switch (tool) {
		case "saolei_click":
			if (REVEALED_NUMBERS.has(cell)) {
				return { ok: false, reason: "cell_already_revealed" };
			}
			if (cell === "FLAG") {
				return { ok: false, reason: "cell_is_flagged" };
			}
			// INITIAL (HIT_MINE/MINE caught by the terminal check) → legal reveal.
			return { ok: true };
		case "saolei_flag":
			if (REVEALED_NUMBERS.has(cell)) {
				return { ok: false, reason: "cannot_flag_revealed" };
			}
			// INITIAL (place) or FLAG (toggle/unflag) → legal.
			return { ok: true };
		case "saolei_chord_click":
			if (!CHORD_NUMBERS.has(cell)) {
				return { ok: false, reason: "chord_requires_number" };
			}
			// FR-016..020: a chord reveals `INITIAL` neighbors (and is lenient
			// on `UNKNOWN`). If no in-bounds neighbor is `INITIAL` or
			// `UNKNOWN` — i.e. every neighbor is a revealed number, `FLAG`,
			// `HIT_MINE`, or `MINE` — the chord is a guaranteed no-op and is
			// rejected before dispatch. Checked AFTER `chord_requires_number`
			// so a chord on a non-number still reports `chord_requires_number`
			// (FR-018 rule order; `specs/027-chat-bubble-game-state/contracts/
			// saolei-mcp-status-contract.md` §5).
			if (!hasInitialOrUnknownNeighbor(state, x, y)) {
				return { ok: false, reason: "chord_no_unrevealed_neighbor" };
			}
			// Chord on a revealed 1–8 with at least one INITIAL/UNKNOWN
			// neighbor is legal regardless of adjacent-flag count (FR-015e:
			// it may reveal nothing — still legal, NOT rejected).
			return { ok: true };
	}
}

/**
 * Recognition engine seam. The default implementation wraps
 * `@dominion/game-saolei-board`'s stateful `SaoleiBoard` (monotonic
 * cross-screenshot validation per its README). Tests inject a fake whose
 * `init`/`update` return canned `GameState`s or throw to simulate a
 * recognition failure — no `vi.mock` of the workspace package
 * (`style/javascript.md` §测试).
 *
 * `init` is the explicit entry point for the FIRST screenshot of a game (or
 * any new game). `update` recognizes a subsequent screenshot of the SAME game
 * (throws `BoardStateIncompatibleError` / `BoardDimensionMismatchError` on a
 * dimension change or non-monotonic regression, per the lib README).
 */
export interface SaoleiBoardApi {
	init(png: Buffer): GameState;
	update(png: Buffer): GameState;
}

/**
 * Build the default recognition engine wrapping `SaoleiBoard`. A fresh
 * `SaoleiBoard` is created on each `init` (new game / re-seed); `update`
 * delegates to the live board's `updateFromScreenshot`.
 */
function createDefaultBoardApi(): SaoleiBoardApi {
	let board: SaoleiBoard | null = null;
	return {
		init(png: Buffer): GameState {
			board = SaoleiBoard.init(png);
			return board.state;
		},
		update(png: Buffer): GameState {
			if (!board) {
				throw new Error("saolei board not initialized");
			}
			return board.updateFromScreenshot(png);
		},
	};
}

/**
 * Decode the base64 PNG string carried by `OperationResult.screenshot.data`
 * (always base64 per `operation-bridge.ts` `toOperationScreenshot`) into the
 * raw `Buffer` `@dominion/game-saolei-board` consumes
 * (`projects/game/pkg/saolei-board/README.md` → "核心库用法"). Returns
 * `undefined` when no screenshot is present — the caller treats that as a
 * recognition failure (FR-017: the post-action screenshot cannot be
 * recognized).
 */
function decodeScreenshot(result: OperationResult): Buffer | undefined {
	if (!result.screenshot?.data) return undefined;
	return Buffer.from(result.screenshot.data, "base64");
}

/**
 * MCP text content block. Saolei tools return exactly one text block per call
 * — never an image block (FR-012/FR-022; the screenshot is consumed for
 * recognition only and stays on the control channel as `FlowResultPart`).
 */
type SaoleiTextBlock = { type: "text"; text: string };

/** Build a single-block MCP result from a body string. */
function textResult(text: string): { content: SaoleiTextBlock[] } {
	return { content: [{ type: "text", text }] };
}

/** Outcome line for `saolei_init` success (contract §3). */
const INIT_OUTCOME = "new game started";

/** Outcome line prefix for a rejected move (contract §3). */
const REJECT_PREFIX = "rejected:";

/** Outcome line for a recognition failure (contract §3 / FR-017). */
const UNRECOGNIZABLE_OUTCOME = "unable to recognize board";

/**
 * Build the `saolei_init` success body: outcome + game-status line + initial
 * text board (`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-
 * contract.md` §2). The status line sits immediately after the outcome line
 * and before the board (FR-012..014).
 */
function initSuccessText(state: GameState): string {
	return `${INIT_OUTCOME}\ngame status: ${gameStatus(state)}\n\n${renderBoardText(state)}`;
}

/**
 * Build the legal-cell-op success body: `<tool> at (x,y) → dispatched` +
 * game-status line + the updated text board (`specs/027-chat-bubble-game-
 * state/contracts/saolei-mcp-status-contract.md` §2; FR-012..014).
 */
function dispatchedText(
	tool: CellTool,
	x: number,
	y: number,
	state: GameState,
): string {
	return (
		`${tool} at (${x},${y}) → dispatched\n` +
		`game status: ${gameStatus(state)}\n\n` +
		`${renderBoardText(state)}`
	);
}

/**
 * Build the rejection body: `rejected: <reason>` + game-status line + the
 * current text board + the valid coordinate range (FR-016/FR-023; `specs/027-
 * chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §2). When
 * the state is unavailable (`no_active_game`), substitutes guidance to call
 * `saolei_init` first and OMITS the status line — no fabricated status
 * (FR-015).
 */
function rejectionText(
	reason: MoveRejection,
	state: GameState | null,
): string {
	if (!state) {
		return `${REJECT_PREFIX} ${reason}\n\ncall saolei_init first to start a game.`;
	}
	return (
		`${REJECT_PREFIX} ${reason}\n` +
		`game status: ${gameStatus(state)}\n\n` +
		`${renderBoardText(state)}\n` +
		`valid range: x 0..${state.width - 1}, y 0..${state.height - 1}`
	);
}

/**
 * Build the recognition-failure body (FR-017) + re-init guidance. No status
 * line is emitted — a recognition failure invalidates the state, so there is
 * no recognized board to derive a status from (FR-015).
 */
function unrecognizableText(): string {
	return `${UNRECOGNIZABLE_OUTCOME}\n\ncall saolei_init to start a new game.`;
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
 * Build a session-bound saolei `McpServer` with exactly four tools, holding a
 * per-session recognized board state and validating each cell move strictly
 * before dispatch (`specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`).
 *
 * Per-session state: the MCP host (`mcp-host.ts`) creates one server per
 * session via this factory, so the closure variables below are per-session.
 * The recognized `GameState` (or `null` = no active / invalid game) lives
 * here, co-located with the LangChain checkpoint and lost together on agent
 * restart (spec Clarification Q1).
 *
 * @param bridge  The session's `OperationBridge` — used to dispatch the
 *   FlowPart operations to the desktop (DI seam).
 * @param boardApi The recognition engine (DI seam). Defaults to the
 *   `@dominion/game-saolei-board` wrapper; tests inject a fake.
 * @returns The session-bound `McpServer`.
 */
export function createSaoleiMcpServer(
	bridge: OperationBridge,
	boardApi: SaoleiBoardApi = createDefaultBoardApi(),
): McpServer {
	const server = new McpServer(
		{ name: "saolei", version: "0.1.0" },
		{ capabilities: { tools: {} } },
	);

	/**
	 * Latest recognized board state for this session. `null` before the first
	 * `saolei_init`, and after any recognition failure (FR-017 marks the state
	 * invalid). `null` ⇒ cell ops are rejected with `no_active_game`
	 * (FR-015a) until a `saolei_init` re-seeds.
	 */
	let recognized: GameState | null = null;

	/**
	 * Recognize a freshly-dispatched screenshot, updating the session state.
	 * On any recognition failure (the recognizer throws, or no screenshot was
	 * attached) the state is invalidated (`recognized = null`) and the caller
	 * surfaces the `unable to recognize` outcome (FR-017). Returns the new
	 * state on success, or `null` on failure.
	 */
	function recognize(
		result: OperationResult,
		recognizer: (png: Buffer) => GameState,
	): GameState | null {
		const png = decodeScreenshot(result);
		if (!png) return null;
		try {
			recognized = recognizer(png);
			return recognized;
		} catch {
			recognized = null;
			return null;
		}
	}

	// ── saolei_init — no arguments ─ FR-019 ──────────────────────────────
	// Dispatches F2 (new game), then seeds the recognized state from the
	// returned screenshot. Re-calling re-dispatches F2 and re-seeds (restart).
	server.registerTool(
		"saolei_init",
		{
			description:
				"Start a new minesweeper game. Dispatches an F2 keypress " +
				"(the new-game shortcut) to the bound desktop window, " +
				"recognizes the post-init board, and returns it as a TEXT " +
				"board (no image). Takes no arguments — the board bounds are " +
				"inferred from the returned screenshot. Re-calling " +
				"re-dispatches F2 (restarts the game) and re-seeds the board.",
		},
		async (extra: SaoleiToolExtra) => {
			const part: FlowPart = {
				keyboardPress: { key: KEY_F2 },
			};
			const result = await bridge.dispatch(part, extra.signal);
			const state = recognize(result, (png) => boardApi.init(png));
			if (!state) {
				return textResult(unrecognizableText());
			}
			return textResult(initSuccessText(state));
		},
	);

	/**
	 * Register a cell-operation tool that validates, dispatches, and returns
	 * the updated text board. Shared by `saolei_click` / `saolei_flag` /
	 * `saolei_chord_click` — only the proto click action and description
	 * differ.
	 */
	function registerCellTool(
		name: CellTool,
		action: MouseClickAction,
		description: string,
	): void {
		server.registerTool(
			name,
			{ description, inputSchema: cellInputSchema() },
			async (args, extra: SaoleiToolExtra) => {
				const { x, y } = args;

				// FR-015a: no active game (pre-init, or state invalidated by a
				// prior recognition failure).
				if (!recognized) {
					return textResult(rejectionText("no_active_game", null));
				}

				// Strict pre-dispatch validation (FR-014/FR-015).
				const verdict = validateMove(recognized, name, x, y);
				if (!verdict.ok) {
					return textResult(rejectionText(verdict.reason, recognized));
				}

				// Legal move: dispatch, then recognize the updated board.
				const { xPx, yPx } = center(x, y);
				const part: FlowPart = {
					mouseMoveAndClick: {
						xPx,
						yPx,
						click: action,
						method: WINDOW_MESSAGE,
					},
				};
				const result = await bridge.dispatch(part, extra.signal);
				const state = recognize(result, (png) => boardApi.update(png));
				if (!state) {
					return textResult(unrecognizableText());
				}
				return textResult(dispatchedText(name, x, y, state));
			},
		);
	}

	registerCellTool(
		"saolei_click",
		LEFT_CLICK,
		"Left-click (reveal) the cell at grid coordinate (x, y). " +
			"Top-left origin (0, 0); x = column, y = row. Validates the move " +
			"against the recognized board (rejects a click on a revealed or " +
			"flagged cell), dispatches a combined move+left-click via window " +
			"messages at the cell's fixed pixel centre, then recognizes and " +
			"returns the updated TEXT board.",
	);
	registerCellTool(
		"saolei_flag",
		RIGHT_CLICK,
		"Right-click to toggle a flag on the cell at grid coordinate (x, y). " +
			"Top-left origin (0, 0); x = column, y = row. Validates the move " +
			"against the recognized board (rejects flagging a revealed " +
			"cell), dispatches a combined move+right-click via window " +
			"messages at the cell's fixed pixel centre, then recognizes and " +
			"returns the updated TEXT board.",
	);
	registerCellTool(
		"saolei_chord_click",
		LEFT_RIGHT_PRESS,
		"Chord — a single simultaneous left+right button press — on the cell " +
			"at grid coordinate (x, y). Top-left origin (0, 0); x = column, " +
			"y = row. Validates the move against the recognized board " +
			"(rejects a chord on anything but a revealed number 1–8; a chord " +
			"whose adjacent-flag count does not match is still legal and is " +
			"NOT rejected), dispatches a combined move+chord via window " +
			"messages at the cell's fixed pixel centre, then recognizes and " +
			"returns the updated TEXT board.",
	);

	return server;
}

/**
 * Shared zod input schema for the three cell-operation tools
 * (`saolei_click` / `saolei_flag` / `saolei_chord_click`). Top-left origin
 * `(x, y)` grid convention (FR-020). The upper bound is NOT fixed in the
 * schema — it depends on the recognized board dimensions and is enforced by
 * `validateMove` (`out_of_bounds`) against the live state.
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
