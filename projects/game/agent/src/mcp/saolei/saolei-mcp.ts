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
 * Tool surface (spec 039 US1 / FR-001..FR-005 merges the three cell-operation
 * tools into one dual-form `saolei_operate`:
 * `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`):
 *   - `saolei_init`              → F2 new-game keypress; recognizes the first
 *                                 screenshot and seeds the session board.
 *   - `saolei_operate`           → dual-form cell operations: single
 *                                 `type`/`x`/`y` OR a batch `operations`
 *                                 array; validated in order, ONE result.
 *   - `saolei_remain`            → read-only remain-grid query (no dispatch).
 *
 * The three former cell tools (`saolei_click` / `saolei_flag` /
 * `saolei_chord_click`) are merged into `saolei_operate` (FR-001): the entry
 * normalizes the dual form to an operations list (single = length-1 array,
 * contract §3), then each op is validated strictly (`validateMove`) and
 * dispatched in order. Failures are triaged by reason (contract §2 / FR-002):
 * harmless no-ops (HARMLESS_NOOP_REASONS) are SKIPPED and the batch continues;
 * structural/contextual rejections (STRUCTURAL_REASONS) and game end STOP the
 * batch; earlier successful ops take effect. `saolei_init`/`saolei_remain`
 * are unchanged (FR-003).
 *
 * `saolei_remain` (spec 029 US2 / FR-006..013;
 * `specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`)
 * is the read-only fifth tool: it computes the per-cell remain grid (number
 * cell → `number − adjacent flags`, raw/negative; non-number → `-`) and
 * returns it with the same coordinate ruler as the board grid. It dispatches
 * NOTHING and mutates NOTHING; its only rejection is `no_active_game`.
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
 *
 * Counter-informed win (spec 028 FR-005..010 / FR-012;
 * `specs/028-saolei-win-counter-fix/contracts/saolei-mcp-win-contract.md`):
 * the `won` decision is now a conjunction — `isWin(state)` returns `true`
 * only when the grid is fully revealed/flagged AND `state.mineCounter` reads
 * exactly `000`. The recognized `GameState` carries `mineCounter` (populated
 * by the library's recognition pass — see `SaoleiBoardApi`), so this module
 * needs NO signature change: `gameStatus` / `validateMove` /
 * `isTerminalState` already take `state: GameState` and read `isWin(state)`
 * through it. The MCP text-result contract (the `game status:` line, the
 * `game_won` / `game_over` rejection bodies) is UNCHANGED in wording — only
 * *when* `won` is decided is more accurate (a grid-only-would-be-win board
 * whose counter ≠ `000` ⇒ `playing`, cell ops allowed).
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import { warn } from "@dominion/common-js-logs";
import {
	SaoleiBoard,
	isWin,
	renderBoardText,
	renderGridWithRuler,
} from "@dominion/game-saolei-board";
import type { CellStatus, GameState } from "@dominion/game-saolei-board";

import type { OperationBridge } from "../../operation-bridge.js";
import type { OperationResult } from "../../operation-bridge.js";
import type { FlowPart } from "../../../game_types/projects/game/FlowPart.js";
import type { MouseClickAction } from "../../../game_types/projects/game/MouseClickAction.js";
import { center } from "./geometry.js";

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
 * string, `projects/game/game.proto` `enum MouseClickAction`). Dispatched for
 * `saolei_operate` ops of type `click`. Hardcoded to keep `game_types` a
 * type-only import.
 */
const LEFT_CLICK = "MOUSE_CLICK_ACTION_LEFT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_RIGHT_CLICK` (proto enum
 * string). Dispatched for `saolei_operate` ops of type `flag`. Hardcoded to
 * keep `game_types` a type-only import.
 */
const RIGHT_CLICK = "MOUSE_CLICK_ACTION_RIGHT_CLICK";

/**
 * Wire value of `MouseClickAction.MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS` (proto
 * enum string). Dispatched for `saolei_operate` ops of type `chord` — a
 * single simultaneous left+right button press (research.md D7: one atomic
 * chord, NOT two separate clicks and NOT a left double-click). Hardcoded to
 * keep `game_types` a type-only import.
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

/**
 * The kind of a single cell operation exposed by `saolei_operate`
 * (`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`
 * §1): `click` (left-click reveal), `flag` (right-click toggle), `chord`
 * (simultaneous left+right press). A string enum union — NOT a bare string.
 */
export type OperationType = "click" | "flag" | "chord";

/**
 * A single cell operation of the `saolei_operate` dual-form tool (contract
 * §1): top-left origin (0,0), x = column, y = row (existing convention).
 */
export interface CellOperation {
	type: OperationType;
	x: number;
	y: number;
}

/**
 * Per-game quantitative statistics, computed first-hand by the MCP at game
 * end and carried by `onGameEnd` → ephemeral buffer → planner review input
 * (`specs/037-saolei-team-optimize/contracts/game-stats-contract.md` §1;
 * `specs/037-saolei-team-optimize/spec.md` FR-026..FR-033). A game concept —
 * no team/strategy/store coupling (FR-019 unchanged).
 */
export interface GameStats {
	/** Count of successful cell operations this game (successful dispatch
	 *  count — one per legal op executed by `saolei_operate`).
	 *  Excludes init, remain, rejected/skipped moves, and LLM call count. */
	operationCount: number;
	/** Number of correctly flagged mines this game.
	 *  null = init mineCounter undecodable (totalMines unknown). */
	correctFlags: number | null;
	/** operationCount / correctFlags, rounded to 2 decimals.
	 *  "N/A" = correctFlags is 0 or null (division by zero / unknown). */
	avgOpsPerMine: number | "N/A";
}

/**
 * Optional out-of-band event sink for the session-bound saolei MCP
 * (`specs/031-team-template-mode/contracts/saolei-sink-contract.md` §1).
 *
 * The interface describes ONLY event shapes — it does NOT reference team /
 * strategy / store / teamMemoryId concepts (FR-019; spec 031 spec.md). The
 * team side registers an implementation that writes game state and the
 * structured end event into its own shared state as the stable signal source
 * for the planner (FR-021/FR-022). No sink registered ⇒ zero behaviour change
 * (FR-020).
 *
 * All callbacks may return a promise; the MCP awaits them but isolates any
 * rejection so it never affects the tool result (contract §5).
 */
export interface SaoleiEventSink {
	/** A new game started: after `saolei_init` recognize succeeds. */
	onGameStart(state: GameState): void | Promise<void>;
	/**
	 * A `saolei_operate` call completed (single or batch — one callback per
	 * call, FR-004; `specs/039-planner-memory-calibration/contracts/
	 * saolei-operate-contract.md` §4). `operations` carries the call's FULL
	 * op list (including skipped/stopped ones); `finalState` is the board
	 * after processing. Fires once per `saolei_operate` call when an active
	 * game (recognized board) exists, REGARDLESS of how many ops executed —
	 * covers all-skip batches and early structural stops (FR-004: every call
	 * is recorded as one gameLog entry). Does NOT fire on a `no_active_game`
	 * rejection or an unrecognizable recognition failure (no state to
	 * report). `stats` rides along when the call ended the game.
	 */
	onOperate(
		operations: CellOperation[],
		finalState: GameState,
		stats?: GameStats,
	): void | Promise<void>;
	/** The game ended: `gameStatus(state)` ∈ {won, lost} after a move.
	 *  `stats` is optional (backward compatible — unupgraded sink
	 *  implementations ignore it; `specs/037-saolei-team-optimize/contracts/
	 *  game-stats-contract.md` §2). */
	onGameEnd(
		state: GameState,
		status: "won" | "lost",
		stats?: GameStats,
	): void | Promise<void>;
}

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

/**
 * Rejection reasons that are HARMLESS NO-OPS — the operation would NOT change
 * the board (e.g. click on a revealed number / flag, flag a revealed cell,
 * chord a non-number, chord a number with no unrevealed neighbor). The
 * `saolei_operate` batch SKIPS these and continues with the remaining ops
 * (FR-002; `specs/039-planner-memory-calibration/contracts/
 * saolei-operate-contract.md` §2).
 */
export const HARMLESS_NOOP_REASONS: ReadonlySet<MoveRejection> = new Set<
	MoveRejection
>([
	"cell_already_revealed",
	"cell_is_flagged",
	"cannot_flag_revealed",
	"chord_requires_number",
	"chord_no_unrevealed_neighbor",
]);

/**
 * Rejection reasons that are STRUCTURAL / contextual — the operation cannot
 * be judged against a usable board (out of bounds, or no active game). The
 * `saolei_operate` batch STOPS at these; remaining ops are NOT executed
 * (FR-002; contract §2).
 */
export const STRUCTURAL_REASONS: ReadonlySet<MoveRejection> = new Set<
	MoveRejection
>(["out_of_bounds", "no_active_game"]);

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
 *
 * `isWin(state)` is counter-informed as of spec 028 FR-005..010: it returns
 * `true` only when the grid is fully revealed/flagged AND `state.mineCounter`
 * reads exactly `000`. A grid-only-would-be-win board whose counter ≠ `000`
 * (e.g. the `saolei_9` over-flagged shape) ⇒ `playing` here, not `won` — the
 * text contract is unchanged, only the `won` decision is more accurate
 * (`specs/028-saolei-win-counter-fix/contracts/saolei-mcp-win-contract.md`).
 */
function gameStatus(state: GameState): GameStatus {
	if (isTerminalState(state)) return "lost";
	if (isWin(state)) return "won";
	return "playing";
}

/**
 * Compute the per-game statistics at game end
 * (`specs/037-saolei-team-optimize/contracts/game-stats-contract.md` §3;
 * `specs/037-saolei-team-optimize/spec.md` FR-026..FR-033). Pure function of
 * the init state, the final state, and the MCP's first-hand operation
 * counter — no I/O, no side effects, directly unit-testable
 * (`style/javascript.md` §测试).
 *
 * correctFlags = totalMines − terminal MINE cells − HIT_MINE cells; totalMines
 * is taken from `initState.mineCounter` (at game start flags = 0, so the
 * counter reads the mine total — `projects/game/pkg/saolei-board/src/core/
 * counter.ts`). An undecodable/absent counter ⇒ totalMines unknown ⇒
 * correctFlags = null (FR-033 degradation). avgOpsPerMine = operationCount /
 * correctFlags rounded to 2 decimals; correctFlags = 0 or null ⇒ "N/A"
 * (FR-029 — no NaN/Infinity on an instant loss).
 */
export function computeGameStats(
	initState: GameState | null,
	finalState: GameState,
	operationCount: number,
): GameStats {
	const counter = initState?.mineCounter;
	let correctFlags: number | null;
	if (counter?.decoded === true) {
		const totalMines = counter.value;
		let mineCells = 0;
		let hitMineCells = 0;
		for (const row of finalState.grid) {
			for (const cell of row) {
				if (cell === "MINE") mineCells++;
				if (cell === "HIT_MINE") hitMineCells++;
			}
		}
		correctFlags = totalMines - mineCells - hitMineCells;
	} else {
		correctFlags = null;
	}

	let avgOpsPerMine: number | "N/A";
	if (correctFlags !== null && correctFlags > 0) {
		avgOpsPerMine = Math.round((operationCount / correctFlags) * 100) / 100;
	} else {
		avgOpsPerMine = "N/A";
	}

	return { operationCount, correctFlags, avgOpsPerMine };
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
	tool: OperationType,
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
		case "click":
			if (REVEALED_NUMBERS.has(cell)) {
				return { ok: false, reason: "cell_already_revealed" };
			}
			if (cell === "FLAG") {
				return { ok: false, reason: "cell_is_flagged" };
			}
			// INITIAL (HIT_MINE/MINE caught by the terminal check) → legal reveal.
			return { ok: true };
		case "flag":
			if (REVEALED_NUMBERS.has(cell)) {
				return { ok: false, reason: "cannot_flag_revealed" };
			}
			// INITIAL (place) or FLAG (toggle/unflag) → legal.
			return { ok: true };
		case "chord":
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
 * Rejection body for supplying BOTH the single and the batch form (contract
 * §5 / D12.1 — refuse ambiguity over silent precedence).
 */
const AMBIGUOUS_ARGS_TEXT =
	"saolei_operate → rejected: provide EITHER type/x/y (single operation) " +
	"OR operations (batch), not both.";

/** Rejection body for supplying NEITHER form (contract §5 / D12.1). */
const MISSING_ARGS_TEXT =
	"saolei_operate → rejected: provide EITHER type/x/y (single operation) " +
	"OR an operations array (batch).";

/** Rejection body for a PARTIAL single form (e.g. type without x/y). */
const INCOMPLETE_ARGS_TEXT =
	"saolei_operate → rejected: the single-operation form requires ALL of " +
	"type, x and y together.";

/**
 * Build the `saolei_init` success body: outcome + game-status line + initial
 * text board (`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-
 * contract.md` §2). The status line sits immediately after the outcome line
 * and before the board (FR-012..014).
 */
function initSuccessText(state: GameState): string {
	return `${INIT_OUTCOME}\ngame status: ${gameStatus(state)}\n\n${renderBoardText(state)}`;
}

/** The `MouseClickAction` dispatched per operation type (contract §1). */
const OPERATION_ACTIONS: Record<OperationType, MouseClickAction> = {
	click: LEFT_CLICK,
	flag: RIGHT_CLICK,
	chord: LEFT_RIGHT_PRESS,
};

/**
 * Build the `saolei_operate` result body: outcome line + game-status line +
 * the final text board (FR-002; `specs/039-planner-memory-calibration/
 * contracts/saolei-operate-contract.md` §2). The outcome line reflects the
 * batch's triage: a normal completion (`executed N ops`, with `skipped S
 * no-op ops` when any were skipped), or a stop at the triggering op with its
 * parameters and reason (structural rejection code, or `won`/`lost` for a
 * game end) — `specs/042-planner-memory-fixup/contracts/
 * saolei-operate-stop-format.md` §1.2: `stopped at {type}({x},{y})`.
 */
function operateResultText(
	executed: number,
	skipped: number,
	state: GameState,
	stoppedOp: CellOperation | null,
	stoppedReason: MoveRejection | "won" | "lost" | null,
): string {
	let line: string;
	if (stoppedOp != null) {
		line = `saolei_operate → stopped at ${stoppedOp.type}(${stoppedOp.x},${stoppedOp.y}) (${stoppedReason})`;
	} else if (skipped > 0) {
		line = `saolei_operate → executed ${executed} ops, skipped ${skipped} no-op ops`;
	} else {
		line = `saolei_operate → executed ${executed} ops`;
	}
	return (
		`${line}\n` +
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
 * Outcome line for the read-only `saolei_remain` query
 * (`specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`
 * §4).
 */
const REMAIN_OUTCOME = "saolei_remain → computed";

/**
 * Build the `saolei_remain` body: outcome + game-status line + the remain
 * grid rendered with the shared coordinate ruler
 * (`specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`
 * §3/§4). Each revealed number cell (`1`–`8`, the existing `CHORD_NUMBERS`)
 * carries its raw `number − adjacent FLAG count` (may be `0` or negative —
 * not clamped); every other cell (`"0"`, `INITIAL`, `FLAG`, `HIT_MINE`,
 * `MINE`, `UNKNOWN`) is `-`. The grid reuses the library's
 * `renderGridWithRuler` so its ruler matches the board grid's exactly (spec
 * FR-010 / SC-004). Pure: takes a `GameState`, returns a string, dispatches
 * nothing.
 *
 * `Number(cell)` is used instead of `parseInt` per `style/javascript.md` →
 * Google TypeScript Style Guide "Type coercion" (prefer `Number()`); the
 * cell is guaranteed to be a single digit `"1".."8"` by the `CHORD_NUMBERS`
 * membership check, so parse failure is impossible here.
 */
function remainText(state: GameState): string {
	const tokenAt = (x: number, y: number): string => {
		const cell = state.grid[y][x];
		if (CHORD_NUMBERS.has(cell)) {
			const flagCount = neighbors(state, x, y).filter(
				(s) => s === "FLAG",
			).length;
			return String(Number(cell) - flagCount);
		}
		return "-";
	};
	return (
		`${REMAIN_OUTCOME}\n` +
		`game status: ${gameStatus(state)}\n\n` +
		`board size ${state.width}*${state.height}\n\n` +
		renderGridWithRuler(state.width, state.height, tokenAt)
	);
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
 * Build a session-bound saolei `McpServer` with exactly five tools (four
 * operation tools plus the read-only `saolei_remain`), holding a
 * per-session recognized board state and validating each cell move strictly
 * before dispatch (`specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`;
 * the fifth tool is specified in
 * `specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`).
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
 * @param sink    Optional out-of-band event sink (contract
 *   `specs/031-team-template-mode/contracts/saolei-sink-contract.md` §2).
 *   Defaults to `undefined` — no sink registered ⇒ tool behaviour is
 *   unchanged (FR-020). Sink callbacks are error-isolated: a throwing sink
 *   never affects the tool result (contract §5).
 * @returns The session-bound `McpServer`.
 */
export function createSaoleiMcpServer(
	bridge: OperationBridge,
	boardApi: SaoleiBoardApi = createDefaultBoardApi(),
	sink?: SaoleiEventSink,
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
	 * Per-game statistics tracking (`specs/037-saolei-team-optimize/contracts/
	 * game-stats-contract.md` §3). `initState` = the recognized state captured
	 * at `saolei_init` (its mineCounter supplies the total-mine count at game
	 * end — at game start flags = 0, so counter = mines); `operationCount` =
	 * successful cell-op count, reset on every new game.
	 */
	let initState: GameState | null = null;
	let operationCount = 0;

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

	/**
	 * Error-isolated sink invocation (`specs/031-team-template-mode/contracts/
	 * saolei-sink-contract.md` §5): a throwing or rejecting sink callback
	 * MUST NOT affect the tool's return value — the error is logged as a
	 * warning and swallowed. `fn` receives the (possibly `undefined`) sink,
	 * so a server built without a sink performs a no-op call.
	 */
	async function runSink(
		event: string,
		fn: (sink: SaoleiEventSink) => void | Promise<void>,
	): Promise<void> {
		if (!sink) return;
		try {
			await fn(sink);
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			warn("saolei mcp sink callback failed", {
				event,
				error: msg,
			});
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
			// Stats tracking (contract §3): capture the initial state (its
			// mineCounter = total mines when flags = 0) and reset the
			// per-game operation counter.
			initState = state;
			operationCount = 0;
			// Sink: a new game started (contract §3 — `onGameStart` fires
			// after a successful init recognition; FR-019/FR-021).
			await runSink("onGameStart", (s) => s.onGameStart(state));
			return textResult(initSuccessText(state));
		},
	);

	// ── saolei_remain — read-only, no arguments, no dispatch ───────────
	// Pure query: reads `recognized`, computes the remain grid, returns it.
	// Performs NO `bridge.dispatch` and does NOT mutate `recognized`
	// (`specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`
	// §2 / spec FR-007). Rejects ONLY with `no_active_game` when no board is
	// recognized; terminal `won`/`lost` boards are NOT blocked (no move is
	// attempted, so `validateMove` is never invoked — contract §5 / FR-012).
	server.registerTool(
		"saolei_remain",
		{
			description:
				"Read-only deduction view. Takes NO arguments and dispatches " +
				"NOTHING to the desktop. Reads the latest recognized board and " +
				"returns, for every cell, the remaining unmarked mine count: " +
				"for a revealed number cell (1–8), the value is " +
				"`number − adjacent flags` (Moore neighbourhood; may be 0 or " +
				"NEGATIVE when over-flagged); for every other cell (0, *, F, " +
				"X, M, ?), the value is `-`. The grid carries the same " +
				"coordinate ruler as the board grid. Rejects with " +
				"`no_active_game` only when no board is recognized.",
		},
		async () => {
			if (!recognized) {
				return textResult(rejectionText("no_active_game", null));
			}
			return textResult(remainText(recognized));
		},
	);

	// ── saolei_operate — dual-form cell operations (single OR batch) ─────
	// Merges the former saolei_click / saolei_flag / saolei_chord_click into
	// one tool (FR-001; `specs/039-planner-memory-calibration/contracts/
	// saolei-operate-contract.md` §1): the entry normalizes the dual form to
	// an operations list, then every op is validated strictly and dispatched
	// in order — ONE result (FR-002). Failures are triaged by reason
	// (contract §2): harmless no-ops SKIP and the batch continues, structural
	// rejections and game end STOP it; earlier successful ops take effect.

	/**
	 * Validate + dispatch + recognize ONE cell operation
	 * (`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`
	 * §2). The shared per-op execution unit of the batch loop; returns a
	 * discriminated result so the loop can triage without re-reading the
	 * session state.
	 */
	type OpExecution =
		| { kind: "ok"; state: GameState; status: GameStatus }
		| { kind: "skip"; reason: MoveRejection }
		| { kind: "stop"; reason: MoveRejection }
		| { kind: "unrecognizable" };

	async function executeOperation(
		op: CellOperation,
		extra: SaoleiToolExtra,
	): Promise<OpExecution> {
		// FR-015a: no active game (pre-init, or state invalidated by a prior
		// recognition failure). Structural — stops the batch.
		if (!recognized) {
			return { kind: "stop", reason: "no_active_game" };
		}
		// Strict pre-dispatch validation (FR-014/FR-015).
		const verdict = validateMove(recognized, op.type, op.x, op.y);
		if (!verdict.ok) {
			if (HARMLESS_NOOP_REASONS.has(verdict.reason)) {
				return { kind: "skip", reason: verdict.reason };
			}
			// Structural / contextual — or terminal `game_over`/`game_won`
			// (the game already ended): stop the batch (contract §2).
			return { kind: "stop", reason: verdict.reason };
		}

		// Legal move: dispatch, then recognize the updated board.
		const { xPx, yPx } = center(op.x, op.y);
		const part: FlowPart = {
			mouseMoveAndClick: {
				xPx,
				yPx,
				click: OPERATION_ACTIONS[op.type],
				method: WINDOW_MESSAGE,
			},
		};
		const result = await bridge.dispatch(part, extra.signal);
		const state = recognize(result, (png) => boardApi.update(png));
		if (!state) {
			return { kind: "unrecognizable" };
		}
		// Stats tracking (contract §3): count ONLY successful cell
		// operations — init / remain / rejected or skipped ops never reach
		// here (rejections return before dispatch).
		operationCount++;
		return { kind: "ok", state, status: gameStatus(state) };
	}

	server.registerTool(
		"saolei_operate",
		{
			description:
				"Execute one or more minesweeper cell operations IN ORDER and " +
				"return ONE result with the final TEXT board. Supported " +
				"operation types (click/flag/chord): click = left-click to " +
				"reveal a cell; flag = right-click to place/toggle a flag; " +
				"chord = simultaneous left+right press on a revealed number " +
				"1–8 to expand it. Two mutually exclusive argument forms: a " +
				"single operation via type/x/y, OR a batch via the operations " +
				"array [{type, x, y}, ...] (order preserved). Top-left origin " +
				"(0, 0); x = column, y = row. Each op is validated strictly " +
				"against the recognized board before dispatch: a no-op " +
				"rejection (click a revealed/flagged cell, flag a revealed " +
				"cell, chord a non-number, chord a number with no unrevealed " +
				"neighbor) is SKIPPED and execution continues; a structural " +
				"rejection (out-of-bounds, no active game) or a game end " +
				"STOPS the batch — earlier successful operations take effect.",
			inputSchema: operateInputSchema(),
		},
		async (args, extra: SaoleiToolExtra) => {
			// Dual-form normalization (contract §3): single type/x/y == a
			// length-1 operations array. Both forms / neither form is an
			// ambiguous call and is REJECTED (D12.1 — refuse ambiguity over
			// silent precedence; spec Edge Case "非法组合"). The single form
			// must be a COMPLETE type/x/y triple (the three schema fields are
			// independently optional — a partial triple is invalid).
			const hasSingle =
				args.type != null || args.x != null || args.y != null;
			const hasBatch = args.operations != null;
			if (hasSingle && hasBatch) {
				return textResult(AMBIGUOUS_ARGS_TEXT);
			}
			if (!hasSingle && !hasBatch) {
				return textResult(MISSING_ARGS_TEXT);
			}
			if (
				hasSingle &&
				(args.type == null || args.x == null || args.y == null)
			) {
				return textResult(INCOMPLETE_ARGS_TEXT);
			}
			const operations: CellOperation[] = hasBatch
				? (args.operations as CellOperation[])
				: [
						{
							type: args.type as OperationType,
							x: args.x as number,
							y: args.y as number,
						},
					];

			// Empty operations list: no-op by definition (spec Edge Case —
			// MUST NOT produce any board side effect). Return the current
			// state (or the no_active_game guidance when no board exists).
			if (operations.length === 0) {
				if (!recognized) {
					return textResult(rejectionText("no_active_game", null));
				}
				return textResult(
					operateResultText(0, 0, recognized, null, null),
				);
			}

			let executed = 0;
			let skipped = 0;
			let stoppedOp: CellOperation | null = null;
			let stoppedReason: MoveRejection | "won" | "lost" | null = null;
			let endedStatus: "won" | "lost" | null = null;

			for (let i = 0; i < operations.length; i += 1) {
				const result = await executeOperation(operations[i], extra);
				if (result.kind === "ok") {
					executed += 1;
					// Game end (`gameStatus` ∈ {won, lost} — the MCP's
					// first-hand computation, FR-017): the winning op takes
					// effect, then the batch STOPS (contract §2).
					if (result.status !== "playing") {
						endedStatus = result.status;
						stoppedOp = operations[i];
						stoppedReason = result.status;
						break;
					}
				} else if (result.kind === "skip") {
					skipped += 1;
				} else if (result.kind === "stop") {
					stoppedOp = operations[i];
					stoppedReason = result.reason;
					break;
				} else {
					// Recognition failure (FR-017): the state is invalidated —
					// the batch cannot continue meaningfully; surface the
					// existing unrecognizable guidance.
					return textResult(unrecognizableText());
				}
			}

			// Structural `no_active_game` stop (never init'd, or the state
			// was invalidated): no board exists to report — reuse the existing
			// rejection body guiding `saolei_init` (contract §2). A successful
			// op can never precede it (any dispatch re-seeds `recognized`).
			const finalState = recognized;
			if (!finalState) {
				return textResult(rejectionText("no_active_game", null));
			}

			// Sink (contract §4 / FR-004): ONE `onOperate` per
			// `saolei_operate` call (single or batch) — fired whenever an
			// active game (recognized board) exists, REGARDLESS of how many
			// ops actually executed. An all-skip batch or an early
			// structural stop still records the call with its full op list
			// (FR-004: one gameLog entry per call, however the batch went);
			// the `no_active_game` / recognition-failure paths returned
			// above, so `finalState` is always non-null here. When the call
			// ended the game, `onGameEnd` fires AFTER `onOperate` with the
			// structured status (FR-022) and the per-game statistics
			// (FR-026/FR-030); the same stats ride along on `onOperate`.
			const stats = endedStatus
				? computeGameStats(initState, finalState, operationCount)
				: undefined;
			await runSink("onOperate", (s) =>
				s.onOperate(operations, finalState, stats),
			);
			if (endedStatus) {
				await runSink("onGameEnd", (s) =>
					s.onGameEnd(finalState, endedStatus, stats),
				);
			}
			return textResult(
				operateResultText(
					executed,
					skipped,
					finalState,
					stoppedOp,
					stoppedReason,
				),
			);
		},
	);

	return server;
}

/**
 * Shared zod input schema for `saolei_operate`'s dual form
 * (`specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`
 * §1): single `type`/`x`/`y` OR batch `operations: CellOperation[]`. `type`
 * is a strict enum (not a bare string). The upper bound of x/y is NOT fixed
 * in the schema — it depends on the recognized board dimensions and is
 * enforced by `validateMove` (`out_of_bounds`) against the live state.
 */
function operateInputSchema(): {
	type?: z.ZodOptional<z.ZodEnum<["click", "flag", "chord"]>>;
	x?: z.ZodOptional<z.ZodNumber>;
	y?: z.ZodOptional<z.ZodNumber>;
	operations?: z.ZodOptional<
		z.ZodArray<
			z.ZodObject<{
				type: z.ZodEnum<["click", "flag", "chord"]>;
				x: z.ZodNumber;
				y: z.ZodNumber;
			}>
		>
	>;
} {
	const cellSchema = {
		type: z.enum(["click", "flag", "chord"]).describe(
			"operation type: click (reveal), flag (toggle), chord (expand)",
		),
		x: z.number().int().min(0).describe("column index (0-based)"),
		y: z.number().int().min(0).describe("row index (0-based)"),
	};
	return {
		type: z.enum(["click", "flag", "chord"]).optional().describe(
			"single-operation form: the operation type (mutually exclusive with operations)",
		),
		x: z.number().int().min(0).optional().describe(
			"single-operation form: column index (0-based)",
		),
		y: z.number().int().min(0).optional().describe(
			"single-operation form: row index (0-based)",
		),
		operations: z
			.array(z.object(cellSchema))
			.optional()
			.describe(
				"batch form: ordered cell operations (mutually exclusive with type/x/y)",
			),
	};
}
