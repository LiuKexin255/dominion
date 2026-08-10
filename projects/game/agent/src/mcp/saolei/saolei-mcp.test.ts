/**
 * saolei-mcp.test.ts — Tests for the session-bound saolei MCP server with
 * recognized text-board state and strict pre-dispatch validation.
 *
 * Coverage (spec 025 FR-012..FR-022;
 * `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`;
 * the cell tools are merged into `saolei_operate` — Phase 2 US1, FR-001..005,
 * `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`):
 *   - Exactly three tools are exposed (`saolei_init`, `saolei_operate`,
 *     `saolei_remain`); the former `saolei_click`/`saolei_flag`/
 *     `saolei_chord_click` are gone. `saolei_remain` is the read-only third
 *     tool (spec 029 US2 / FR-006..013; `specs/029-saolei-coord-remain/
 *     contracts/saolei-remain-tool-contract.md`).
 *   - `saolei_operate` DUAL-FORM parameters: single `type`/`x`/`y` == a
 *     length-1 `operations` array (equivalent semantics, contract §3); both /
 *     neither / partial forms are rejected; empty operations is a no-op.
 *   - `saolei_init` takes no `width`/`height` arguments (FR-019).
 *   - Every tool returns a single TEXT board block — NEVER an image block
 *     (FR-012 / SC-003).
 *   - `saolei_init` returns the initial text board; a legal `saolei_operate`
 *     dispatches and returns the updated text board.
 *   - Each illegal rule is rejected BEFORE dispatch with the right reason
 *     code (contract §4): `cell_already_revealed`, `cell_is_flagged`,
 *     `cannot_flag_revealed`, `chord_requires_number`, `out_of_bounds`,
 *     `no_active_game`, `game_over`.
 *   - Batch triage (FR-002, contract §2): harmless no-ops (HARMLESS_NOOP_
 *     REASONS) are SKIPPED and the batch continues; structural rejections
 *     (STRUCTURAL_REASONS) and game end STOP the batch; earlier successful
 *     ops take effect; ONE result per call.
 *   - A chord on a revealed number with a mismatched adjacent-flag count is
 *     NOT rejected (FR-015e).
 *   - Recognition failure (`init`/`update` throwing, or no screenshot) →
 *     "unable to recognize board", state invalidated, subsequent ops rejected
 *     as `no_active_game` (FR-017).
 *   - The MCP `extra.signal` is forwarded to `bridge.dispatch` (T028).
 *   - The sink fires ONE `onOperate` per `saolei_operate` call (single or
 *     batch) with the full operations list (FR-004, contract §4).
 *
 * Pattern (`style/javascript.md` §测试 / [vitest Mocking Modules — Pitfalls]
 * (https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)): pure
 * DI — a fake `OperationBridge` provides a canned screenshot, and a fake
 * `SaoleiBoardApi` returns canned `GameState`s or throws to simulate a
 * recognition failure. No `vi.mock` of the workspace recognition package.
 */

import { describe, expect, it } from "vitest";

import { OperationBridge } from "../../operation-bridge";
import type { OperationResult } from "../../operation-bridge";
import {
	createSaoleiMcpServer,
	validateMove,
	isTerminalState,
	computeGameStats,
} from "./saolei-mcp";
import type {
	SaoleiBoardApi,
	SaoleiEventSink,
	GameStats,
	CellOperation,
} from "./saolei-mcp";
import { BOARD_ORIGIN_X_PX, BOARD_ORIGIN_Y_PX, CELL_SIZE_PX } from "./geometry";
import type { CellStatus, GameState, MineCounter } from "@dominion/game-saolei-board";
import type { FlowPart } from "../../../../game_types/projects/game/FlowPart";
import type { TeamFrame } from "../../../../game_types/projects/game/TeamFrame";
import type { KeyboardPressPart } from "../../../../game_types/projects/game/KeyboardPressPart";
import type { MouseMoveAndClickPart } from "../../../../game_types/projects/game/MouseMoveAndClickPart";

/**
 * String status carried by `OperationResult.status` (proto enum). Used to
 * label the canned fake-bridge result.
 */
const STATUS_SUCCEEDED = "TOOL_RESULT_STATUS_SUCCEEDED";

/** A captured operation FlowPart (the only kind saolei dispatches). */
type CapturedPart = FlowPart;

/** Symbol → CellStatus map, mirroring `saolei-board`'s `render.ts`. */
const SYMBOL_TO_STATUS: Record<string, CellStatus> = {
	"*": "INITIAL",
	"0": "0",
	"1": "1",
	"2": "2",
	"3": "3",
	"4": "4",
	"5": "5",
	"6": "6",
	"7": "7",
	"8": "8",
	F: "FLAG",
	X: "HIT_MINE",
	M: "MINE",
	"?": "UNKNOWN",
};

/**
 * Build a `GameState` from space-separated symbol rows (the same symbols the
 * text-board renderer emits), so tests read as the board the model sees. Example:
 * `board(["* *", "* 0"])` → a 2×2 board with cell (1,1) revealed as "0".
 *
 * `mineCounter` is optional — the counter-informed `isWin` (spec 028) returns
 * `false` when it is `undefined` (lenient), so a win-shape board MUST pass
 * `{ decoded: true; value: 0 }` to be classified `won`. The saolei_9-style
 * over-flag shape passes a decoded non-zero counter.
 */
function board(
	rows: string[],
	mineCounter?: MineCounter,
): GameState {
	const height = rows.length;
	const width = rows[0]?.split(/\s+/).length ?? 0;
	const grid = rows.map((r) =>
		r.split(/\s+/).map((s) => SYMBOL_TO_STATUS[s] ?? "UNKNOWN"),
	);
	return { width, height, grid, mineCounter };
}

/** A decoded `000` mine counter — `flags === mines` (the counter half of a win). */
const COUNTER_ZERO: MineCounter = { decoded: true, value: 0 };

/** A decoded `-01` mine counter — over-flagged (the `saolei_9` shape). */
const COUNTER_NEG_ONE: MineCounter = { decoded: true, value: -1 };

/**
 * Build a fake OperationBridge whose dispatch records the dispatched FlowPart
 * and resolves a canned SUCCEEDED result carrying a non-empty screenshot (so
 * the saolei tool proceeds to recognition). The fake simulates the desktop
 * side of the bidi stream — registerSink + handleResult — without spinning
 * up a real connection (`style/javascript.md` §测试 — DI seam).
 *
 * The fake also records the AbortSignal each dispatch received (T028) so tests
 * can assert the MCP `extra.signal` is forwarded to dispatch.
 */
function makeFakeBridge(
	canned: OperationResult = {
		status: STATUS_SUCCEEDED,
		message: "ok",
		screenshot: { data: "AAAA", widthPx: 332, heightPx: 508 },
	},
): {
	bridge: OperationBridge;
	dispatched: CapturedPart[];
	signals: AbortSignal[];
} {
	const bridge = new OperationBridge();
	const dispatched: CapturedPart[] = [];
	const signals: AbortSignal[] = [];
	bridge.registerSink((frame: TeamFrame) => {
		const op = frame.flowParts?.parts?.[0] as CapturedPart | undefined;
		if (!op) return;
		dispatched.push(op);
		const toolId =
			op.keyboardPress?.toolId ??
			op.mouseMove?.toolId ??
			op.mouseClick?.toolId ??
			op.mouseMoveAndClick?.toolId ??
			"";
		if (toolId) {
			bridge.handleResult({
				toolId,
				status: canned.status,
				message: canned.message,
				screenshot: canned.screenshot,
			} as any);
		}
	});
	const origDispatch = bridge.dispatch.bind(bridge);
	bridge.dispatch = (part: FlowPart, signal?: AbortSignal) => {
		if (signal) signals.push(signal);
		return origDispatch(part, signal);
	};
	return { bridge, dispatched, signals };
}

/**
 * Build a controllable fake recognition engine (DI seam). The test sets what
 * `init`/`update` return before each call: a `GameState`, or `"throw"` to
 * simulate a recognition failure (`BoardStateIncompatibleError` /
 * `BoardDimensionMismatchError` → the MCP maps any throw to "unable to
 * recognize"). Call counters let tests assert which path the tool took.
 */
function makeFakeBoardApi(initial: GameState): {
	api: SaoleiBoardApi;
	setInit: (s: GameState | "throw") => void;
	setUpdate: (s: GameState | "throw") => void;
	readonly initCalls: number;
	readonly updateCalls: number;
} {
	let initResult: GameState | "throw" = initial;
	let updateResult: GameState | "throw" = initial;
	let initCalls = 0;
	let updateCalls = 0;
	const api: SaoleiBoardApi = {
		init: () => {
			initCalls += 1;
			if (initResult === "throw") {
				throw new Error("fake recognition failure");
			}
			return initResult;
		},
		update: () => {
			updateCalls += 1;
			if (updateResult === "throw") {
				throw new Error("fake recognition failure");
			}
			return updateResult;
		},
	};
	return {
		api,
		setInit: (s) => {
			initResult = s;
		},
		setUpdate: (s) => {
			updateResult = s;
		},
		get initCalls() {
			return initCalls;
		},
		get updateCalls() {
			return updateCalls;
		},
	};
}

/**
 * Invoke a registered tool by name with literal arguments via the McpServer's
 * internal `tools/call` handler (no HTTP round-trip). The handler shape is
 * part of the SDK's stable surface; accessing it via
 * `(server as any).server._requestHandlers` mirrors the existing test pattern.
 */
function callTool(
	server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer,
	name: string,
	arguments_: Record<string, unknown>,
	extra?: { signal: AbortSignal },
): Promise<{
	isError?: boolean;
	content: { type: string; text?: string; data?: string; mimeType?: string }[];
}> {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	const handler = (server as any).server._requestHandlers.get("tools/call");
	const fakeExtra = extra ?? { signal: new AbortController().signal };
	return handler({
		method: "tools/call",
		params: { name, arguments: arguments_ },
	}, fakeExtra);
}

/** Pixel centre of cell (x, y) per `geometry.center`
 * (specs/024-tool-render-coord-fix/data-model.md §3). */
function centerX(x: number): number {
	return BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}
function centerY(y: number): number {
	return BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}

/** Assert the result is a single text block with no image blocks (FR-012). */
function expectTextOnly(
	result: { content: { type: string; text?: string }[] },
): string {
	expect(result.content).toHaveLength(1);
	expect(result.content[0].type).toBe("text");
	const text = result.content[0].text;
	expect(text).toBeDefined();
	return text as string;
}

// ── validateMove unit tests (pure rule table, contract §4) ─────────────────

describe("validateMove: strict rule table (contract §4)", () => {
	it("rejects an out-of-bounds coordinate", () => {
		const s = board(["* *", "* *"]);
		expect(validateMove(s, "click", 5, 5)).toEqual({
			ok: false,
			reason: "out_of_bounds",
		});
	});

	it("rejects any cell op when the state is terminal (HIT_MINE present)", () => {
		const s = board(["X *", "* *"]);
		expect(validateMove(s, "click", 1, 0)).toEqual({
			ok: false,
			reason: "game_over",
		});
	});

	it("rejects any cell op when the state is terminal (MINE present)", () => {
		const s = board(["M *", "* *"]);
		expect(validateMove(s, "flag", 1, 0)).toEqual({
			ok: false,
			reason: "game_over",
		});
	});

	it("validateMove click: rejects a revealed number (cell_already_revealed)", () => {
		const s = board(["0 *", "* *"]);
		expect(validateMove(s, "click", 0, 0)).toEqual({
			ok: false,
			reason: "cell_already_revealed",
		});
	});

	it("validateMove click: rejects a flag (cell_is_flagged)", () => {
		const s = board(["F *", "* *"]);
		expect(validateMove(s, "click", 0, 0)).toEqual({
			ok: false,
			reason: "cell_is_flagged",
		});
	});

	it("validateMove click: allows an unrevealed cell", () => {
		const s = board(["* *", "* *"]);
		expect(validateMove(s, "click", 1, 1)).toEqual({ ok: true });
	});

	it("validateMove flag: rejects a revealed number (cannot_flag_revealed)", () => {
		const s = board(["3 *", "* *"]);
		expect(validateMove(s, "flag", 0, 0)).toEqual({
			ok: false,
			reason: "cannot_flag_revealed",
		});
	});

	it("validateMove flag: allows flagging an unrevealed cell and toggling a flag", () => {
		const initial = board(["* *", "* *"]);
		expect(validateMove(initial, "flag", 0, 0)).toEqual({ ok: true });
		const flagged = board(["F *", "* *"]);
		expect(validateMove(flagged, "flag", 0, 0)).toEqual({ ok: true });
	});

	it("validateMove chord: rejects 0 / INITIAL / FLAG (chord_requires_number)", () => {
		const zero = board(["0 *", "* *"]);
		expect(validateMove(zero, "chord", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
		const initial = board(["* *", "* *"]);
		expect(validateMove(initial, "chord", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
		const flag = board(["F *", "* *"]);
		expect(validateMove(flag, "chord", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
	});

	it("validateMove chord: allows a chord on a revealed number even when the adjacent-flag count does NOT match (FR-015e)", () => {
		// Cell (0,0) is "1" but no neighbor is flagged → flag-count (0) ≠ 1.
		// Validation judges target-cell compatibility, not predicted outcome.
		const s = board(["1 *", "* *"]);
		expect(validateMove(s, "chord", 0, 0)).toEqual({ ok: true });
	});

	it("validateMove chord: rejects when the target's 8 neighbors are all revealed numbers (chord_no_unrevealed_neighbor, FR-016..020)", () => {
		// 4×4 board: the 3×3 region around (1,1) is all numbers, the rest is
		// INITIAL so the board is NOT terminal-won. Target (1,1) = "1"; every
		// in-bounds neighbor of (1,1) is a revealed number → no INITIAL/UNKNOWN
		// neighbor for the chord to reveal.
		const s = board([
			"1 1 1 *",
			"1 1 1 *",
			"1 1 1 *",
			"* * * *",
		]);
		expect(validateMove(s, "chord", 1, 1)).toEqual({
			ok: false,
			reason: "chord_no_unrevealed_neighbor",
		});
	});

	it("validateMove chord: rejects when all neighbors are FLAG (chord_no_unrevealed_neighbor, FR-016..020)", () => {
		// (1,1) = "8" with 8 FLAG neighbors — every neighbor is a flag, so
		// hasInitialOrUnknownNeighbor is false. The "*" cells keep the board
		// non-terminal.
		const s = board([
			"F F F *",
			"F 8 F *",
			"F F F *",
			"* * * *",
		]);
		expect(validateMove(s, "chord", 1, 1)).toEqual({
			ok: false,
			reason: "chord_no_unrevealed_neighbor",
		});
	});

	it("validateMove chord: rejects at a corner/edge target with no in-bounds INITIAL neighbor (chord_no_unrevealed_neighbor, FR-016..020)", () => {
		// Corner target (0,0) = "1": its only in-bounds neighbors are (1,0),
		// (0,1), (1,1) — all numbers. The "*" cells keep the board non-terminal.
		const corner = board([
			"1 1 *",
			"1 1 *",
			"* * *",
		]);
		expect(validateMove(corner, "chord", 0, 0)).toEqual({
			ok: false,
			reason: "chord_no_unrevealed_neighbor",
		});
		// Edge target (1,0) = "1": its in-bounds neighbors are (0,0), (2,0),
		// (0,1), (1,1), (2,1) — all numbers.
		const edge = board([
			"1 1 1",
			"1 1 1",
			"* * *",
		]);
		expect(validateMove(edge, "chord", 1, 0)).toEqual({
			ok: false,
			reason: "chord_no_unrevealed_neighbor",
		});
	});

	it("validateMove chord: allows when at least one neighbor is INITIAL", () => {
		// (0,0) = "1"; neighbor (1,0) is INITIAL — a chord would reveal it.
		const s = board([
			"1 *",
			"1 1",
		]);
		expect(validateMove(s, "chord", 0, 0)).toEqual({ ok: true });
	});

	it("validateMove chord: lenient on UNKNOWN neighbor (FR-017) — allowed even when no INITIAL neighbor exists", () => {
		// (0,0) = "1"; the only non-revealed neighbor is UNKNOWN. Per FR-017
		// the chord is NOT rejected on this ground (UNKNOWN is treated as
		// possibly unrevealed).
		const s = board([
			"1 ?",
			"1 1",
		]);
		expect(validateMove(s, "chord", 0, 0)).toEqual({ ok: true });
	});

	it("validateMove chord: chord_requires_number still fires FIRST on a non-number target (FR-018 rule order)", () => {
		// (0,0) = INITIAL (a non-number) and NONE of its neighbors is INITIAL
		// or UNKNOWN — so the chord-neighbor rule would also fire if reached.
		// Assert the existing chord_requires_number rule wins (FR-018: the
		// new neighbor check is applied AFTER the existing chord-target check).
		const s = board([
			"* 1",
			"1 1",
		]);
		expect(validateMove(s, "chord", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
	});

	it("UNKNOWN target is always lenient (FR-018)", () => {
		const s = board(["? *", "* *"]);
		expect(validateMove(s, "click", 0, 0)).toEqual({ ok: true });
		expect(validateMove(s, "flag", 0, 0)).toEqual({ ok: true });
		expect(validateMove(s, "chord", 0, 0)).toEqual({ ok: true });
	});

	it("rejects any cell op when the state is a recognized win (game_won, FR-021..023)", () => {
		// All cells are revealed numbers or FLAG with a decoded 000 counter ⇒
		// isWin(state) === true (spec 028: grid-revealed AND counter 000).
		const win = board(["0 F", "1 1"], COUNTER_ZERO);
		expect(validateMove(win, "click", 0, 0)).toEqual({
			ok: false,
			reason: "game_won",
		});
		expect(validateMove(win, "flag", 1, 1)).toEqual({
			ok: false,
			reason: "game_won",
		});
		expect(validateMove(win, "chord", 1, 1)).toEqual({
			ok: false,
			reason: "game_won",
		});
	});

	it("allows cell ops on a grid-only-would-be-win board whose counter is non-zero (FR-012 / SC-004)", () => {
		// The saolei_9 over-flag shape: all cells revealed/flagged but the
		// counter reads `-01` ⇒ isWin(state) === false ⇒ NOT terminal-won ⇒
		// cell ops are NOT rejected as `game_won`. The grid half holds, so
		// the target-cell rules apply normally — the point of this test is
		// that NONE of these verdicts is `game_won` (the terminal-win gate
		// did not fire), and that toggling a flag is still `ok: true`.
		//   (0,0)="0" (revealed)  (1,0)="F" (FLAG)
		//   (0,1)="1" (revealed)  (1,1)="1" (revealed)
		const overFlag = board(["0 F", "1 1"], COUNTER_NEG_ONE);
		// A flag op on the FLAG at (1,0) ⇒ legal (place/toggle) ⇒ ok: true.
		expect(validateMove(overFlag, "flag", 1, 0)).toEqual({ ok: true });
		// A click op on the FLAG at (1,0) ⇒ cell-specific `cell_is_flagged`
		// (NOT `game_won` — the terminal-win gate did not fire).
		expect(validateMove(overFlag, "click", 1, 0)).toEqual({
			ok: false,
			reason: "cell_is_flagged",
		});
		// A flag op on the revealed "0" at (0,0) ⇒ cell-specific
		// `cannot_flag_revealed` (NOT `game_won`).
		expect(validateMove(overFlag, "flag", 0, 0)).toEqual({
			ok: false,
			reason: "cannot_flag_revealed",
		});
	});

	it("loss takes precedence over win: a HIT_MINE board is game_over, never game_won", () => {
		// A board with HIT_MINE is a loss; isWin returns false for it, so the
		// terminal reason is the existing game_over (FR-022 — the two terminal
		// reasons are mutually exclusive).
		const lost = board(["X 0", "0 0"]);
		expect(validateMove(lost, "click", 1, 0)).toEqual({
			ok: false,
			reason: "game_over",
		});
	});
});

describe("isTerminalState", () => {
	it("is false for an in-progress board", () => {
		expect(isTerminalState(board(["* 1", "2 *"]))).toBe(false);
	});
	it("is true when HIT_MINE is present (lost)", () => {
		expect(isTerminalState(board(["X *", "* *"]))).toBe(true);
	});
	it("is true when MINE is present (lost, mines revealed)", () => {
		expect(isTerminalState(board(["* M", "* *"]))).toBe(true);
	});
});

// ── Tool registration ──────────────────────────────────────────────────────

describe("createSaoleiMcpServer: tool registration (FR-020 / 039 US1)", () => {
	it("registers exactly the three saolei tools (no saolei_click/flag/chord_click, no saolei_update)", async () => {
		const { bridge } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		const result = await handler({ method: "tools/list", params: {} });

		const names = (result.tools as { name: string }[]).map((t) => t.name);
		// 039 US1 (FR-001): the three cell tools are merged into
		// `saolei_operate`; `saolei_init`/`saolei_remain` stay (FR-003).
		expect(names.sort()).toEqual(
			["saolei_init", "saolei_operate", "saolei_remain"].sort(),
		);
		expect(names).not.toContain("saolei_click");
		expect(names).not.toContain("saolei_flag");
		expect(names).not.toContain("saolei_chord_click");
		expect(names).not.toContain("saolei_update");
	});

	it("saolei_operate inputSchema exposes the dual form (type/x/y + operations with the type enum)", async () => {
		const { bridge } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		const result = await handler({ method: "tools/list", params: {} });

		const operate = (result.tools as {
			name: string;
			inputSchema?: { properties?: Record<string, unknown> };
		}[]).find((t) => t.name === "saolei_operate");
		expect(operate).toBeDefined();
		const props = operate?.inputSchema?.properties ?? {};
		// Single form: type/x/y; batch form: operations array.
		expect(props).toHaveProperty("type");
		expect(props).toHaveProperty("x");
		expect(props).toHaveProperty("y");
		expect(props).toHaveProperty("operations");
	});

	it("saolei_init inputSchema has no width/height properties (FR-019)", async () => {
		const { bridge } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		const result = await handler({ method: "tools/list", params: {} });

		const init = (result.tools as { name: string; inputSchema?: { properties?: Record<string, unknown> } }[]).find(
			(t) => t.name === "saolei_init",
		);
		expect(init).toBeDefined();
		const props = init?.inputSchema?.properties ?? {};
		expect(props).not.toHaveProperty("width");
		expect(props).not.toHaveProperty("height");
	});
});

// ── saolei_init ─────────────────────────────────────────────────────────────

describe("createSaoleiMcpServer: saolei_init (FR-012 / FR-019)", () => {
	it("dispatches F2 and returns the initial TEXT board (no image)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const initial = board(["* *", "* *"]);
		const fake = makeFakeBoardApi(initial);
		const server = createSaoleiMcpServer(bridge, fake.api);

		const result = await callTool(server, "saolei_init", {});

		// FR-019: exactly one KeyboardPressPart{F2} dispatched.
		expect(dispatched).toHaveLength(1);
		expect(dispatched[0].keyboardPress?.key).toBe("KEYBOARD_KEY_F2");
		expect(fake.initCalls).toBe(1);

		// FR-012: a single TEXT block with the initial board — NO image block.
		const text = expectTextOnly(result);
		expect(text).toContain("new game started");
		expect(text).toContain("board size 2*2");
		// Ruled grid (contract specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §1):
		// cells are right-aligned to columnWidth 4, so two adjacent single-char
		// cells are separated by 4 spaces, not 1.
		expect(text).toContain("*    *");
		// FR-012/FR-014: in-progress board ⇒ `game status: playing`.
		expect(text).toContain("game status: playing");
	});

	it("re-calling re-dispatches F2 and re-seeds the board (restart)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		await callTool(server, "saolei_init", {});

		expect(dispatched).toHaveLength(2);
		expect(
			(dispatched[0].keyboardPress as KeyboardPressPart).key,
		).toBe("KEYBOARD_KEY_F2");
		expect(fake.initCalls).toBe(2);
	});

	it("returns 'unable to recognize' when recognition fails (FR-017)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["*"]));
		fake.setInit("throw");
		const server = createSaoleiMcpServer(bridge, fake.api);

		const result = await callTool(server, "saolei_init", {});

		// F2 still dispatched (the desktop side ran); recognition failed.
		expect(dispatched).toHaveLength(1);
		const text = expectTextOnly(result);
		expect(text).toContain("unable to recognize board");
		// No board body on recognition failure (contract §3).
		expect(text).not.toContain("board size");
		// FR-015: no recognized state ⇒ NO fabricated game-status line.
		expect(text).not.toContain("game status:");
	});
});

// ── legal cell op dispatches + updated text board ───────────────────────────

describe("createSaoleiMcpServer: legal cell op dispatches and returns updated text (FR-012/FR-019)", () => {
	it("saolei_operate single-form click on an unrevealed cell dispatches and returns the updated text board", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// After the legal click, recognition reports cell (0,0) revealed as 0.
		fake.setUpdate(board(["0 *", "* *"]));

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		// FR-019: MouseMoveAndClickPart{LEFT_CLICK, WINDOW_MESSAGE} at the
		// cell centre in WM_* client space (originY 104).
		expect(dispatched).toHaveLength(2);
		const part = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(0));
		expect(part.yPx).toBe(centerY(0));
		expect(part.toolId).toBeTruthy();

		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → executed 1 ops");
		// Ruled grid (contract specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §1):
		// cells right-aligned to columnWidth 4 ⇒ 4-space separator.
		expect(text).toContain("0    *");
		// FR-012/FR-014: in-progress board ⇒ `game status: playing`.
		expect(text).toContain("game status: playing");
	});

	it("saolei_operate single-form flag dispatches a RIGHT_CLICK at the cell centre", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["F *", "* *"]));

		await callTool(server, "saolei_operate", { type: "flag", x: 0, y: 0 });

		const part = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(0));
		expect(part.yPx).toBe(centerY(0));
	});

	it("saolei_operate single-form chord on a revealed number dispatches a LEFT_RIGHT_PRESS (flag-count mismatch still legal)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		// (0,0) is "1" with NO neighbor flagged — mismatched, but legal (FR-015e).
		const fake = makeFakeBoardApi(board(["1 *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["1 *", "* *"]));

		const result = await callTool(server, "saolei_operate", {
			type: "chord",
			x: 0,
			y: 0,
		});

		// Chord dispatched (NOT rejected) despite the flag-count mismatch.
		expect(dispatched).toHaveLength(2);
		const part = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain("saolei_operate → executed 1 ops");
		// FR-012/FR-014: in-progress board ⇒ `game status: playing`.
		expect(result.content[0].text).toContain("game status: playing");
	});
});

// ── illegal ops triaged BEFORE dispatch (contract §4 / 039 FR-002) ──────────
//
// `saolei_operate` triages rejections by reason (contract §2): harmless
// no-ops (HARMLESS_NOOP_REASONS) are SKIPPED (execution continues, no
// dispatch); structural rejections (STRUCTURAL_REASONS) STOP the batch.
// Both leave the board untouched — no FlowPart is dispatched.

describe("createSaoleiMcpServer: illegal ops triaged before dispatch", () => {
	function setup(initial: GameState): {
		server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer;
		dispatched: CapturedPart[];
	} {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(initial);
		const server = createSaoleiMcpServer(bridge, fake.api);
		return { server, dispatched };
	}

	it("SKIPS a click on a revealed cell (cell_already_revealed) without dispatching", async () => {
		const { server, dispatched } = setup(board(["0 *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		// Only the init F2 dispatched; the no-op click did NOT.
		expect(dispatched).toHaveLength(1);
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → executed 0 ops, skipped 1 no-op ops");
		expect(text).toContain("board size 2*2");
	});

	it("SKIPS a click on a flag (cell_is_flagged)", async () => {
		const { server, dispatched } = setup(board(["F *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);
	});

	it("SKIPS flagging a revealed cell (cannot_flag_revealed)", async () => {
		const { server, dispatched } = setup(board(["0 *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_operate", { type: "flag", x: 0, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);
	});

	it("SKIPS a chord on 0 / INITIAL / FLAG (chord_requires_number)", async () => {
		// chord on "0"
		const onZero = setup(board(["0 *", "* *"]));
		await callTool(onZero.server, "saolei_init", {});
		const r0 = await callTool(onZero.server, "saolei_operate", { type: "chord", x: 0, y: 0 });
		expect(r0.content[0].text).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);
		expect(onZero.dispatched).toHaveLength(1);

		// chord on INITIAL
		const onInitial = setup(board(["* *", "* *"]));
		await callTool(onInitial.server, "saolei_init", {});
		const rInit = await callTool(onInitial.server, "saolei_operate", { type: "chord", x: 0, y: 0 });
		expect(rInit.content[0].text).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);

		// chord on FLAG
		const onFlag = setup(board(["F *", "* *"]));
		await callTool(onFlag.server, "saolei_init", {});
		const rFlag = await callTool(onFlag.server, "saolei_operate", { type: "chord", x: 0, y: 0 });
		expect(rFlag.content[0].text).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);
	});

	it("STOPS at an out-of-bounds coordinate (out_of_bounds — structural)", async () => {
		const { server, dispatched } = setup(board(["* *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_operate", { type: "click", x: 5, y: 5 });

		expect(dispatched).toHaveLength(1);
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → stopped at click(5,5) (out_of_bounds)");
		expect(text).toContain("board size 2*2");
	});

	it("rejects any cell op before init (no_active_game, FR-015a)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		// No dispatch at all — no game has been started.
		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: no_active_game");
		expect(text).toContain("call saolei_init first");
		// FR-015: no recognized state ⇒ NO fabricated game-status line.
		expect(text).not.toContain("game status:");
	});

	it("STOPS any cell op when the state is terminal (game_over, FR-015f)", async () => {
		const { server, dispatched } = setup(board(["X *", "* *"]));
		const initState = await callTool(server, "saolei_init", {});
		// FR-013: a losing board (HIT_MINE present) ⇒ `game status: lost`.
		expect(initState.content[0].text).toContain("game status: lost");

		// (1,0) is INITIAL, but the board is terminal (HIT_MINE at (0,0)).
		const result = await callTool(server, "saolei_operate", { type: "click", x: 1, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain(
			"saolei_operate → stopped at click(1,0) (game_over)",
		);
		// The stop result carries the status line for the losing state.
		expect(result.content[0].text).toContain("game status: lost");
	});
});

// ── recognition failure handling (FR-017) ───────────────────────────────────

describe("createSaoleiMcpServer: recognition failure (FR-017)", () => {
	it("a failed init invalidates the state; subsequent ops are rejected as no_active_game", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["*"]));
		fake.setInit("throw");
		const server = createSaoleiMcpServer(bridge, fake.api);

		const initResult = await callTool(server, "saolei_init", {});
		expect(initResult.content[0].text).toContain("unable to recognize board");
		// FR-015: no recognized state ⇒ NO fabricated game-status line.
		expect(initResult.content[0].text).not.toContain("game status:");

		// State is invalid → cell op rejected as no_active_game (not dispatched).
		const clickResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(dispatched).toHaveLength(1); // only the F2
		expect(clickResult.content[0].text).toContain("rejected: no_active_game");
		// FR-015: no_active_game carries NO status line either.
		expect(clickResult.content[0].text).not.toContain("game status:");
	});

	it("a failed update (post-dispatch) invalidates the state; subsequent ops are rejected", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// The legal click dispatches, but the post-action screenshot cannot be
		// recognized → state invalidated.
		fake.setUpdate("throw");

		const clickResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(clickResult.content[0].text).toContain("unable to recognize board");
		// FR-015: state invalidated ⇒ NO game-status line on this result.
		expect(clickResult.content[0].text).not.toContain("game status:");
		// The click DID dispatch (recognition fails on the post-action frame).
		expect(dispatched).toHaveLength(2);

		// Subsequent op → no_active_game (state invalid).
		const next = await callTool(server, "saolei_operate", { type: "click", x: 1, y: 0 });
		expect(next.content[0].text).toContain("rejected: no_active_game");
	});

	it("returns 'unable to recognize' when the desktop attaches no screenshot", async () => {
		// A dispatch result with no screenshot data: recognition cannot proceed.
		const { bridge } = makeFakeBridge({
			status: STATUS_SUCCEEDED,
			message: "ok",
		});
		const fake = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		const result = await callTool(server, "saolei_init", {});
		expect(result.content[0].text).toContain("unable to recognize board");
		// FR-015: no recognized state ⇒ NO fabricated game-status line.
		expect(result.content[0].text).not.toContain("game status:");
	});
});

// ── game status line + post-win terminal (FR-012..015, FR-021..023) ─────────

describe("createSaoleiMcpServer: game status line + post-win terminal (US4 / FR-012..015, FR-021..023)", () => {
	it("a winning board surfaces 'game status: won' on init and rejects subsequent cell ops as game_won (no dispatch)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		// All cells revealed numbers / FLAG with a decoded 000 counter ⇒
		// isWin(state) === true (spec 028: grid-revealed AND counter 000).
		const winning = board(["0 F", "1 1"], COUNTER_ZERO);
		const fake = makeFakeBoardApi(winning);
		const server = createSaoleiMcpServer(bridge, fake.api);

		// init recognizes the winning board: the init result carries
		// `game status: won` (FR-012/FR-013).
		const initResult = await callTool(server, "saolei_init", {});
		const initText = expectTextOnly(initResult);
		expect(initText).toContain("new game started");
		expect(initText).toContain("game status: won");
		expect(initText).toContain("board size 2*2");

		// A subsequent cell op STOPS as game_won BEFORE dispatch (FR-021):
		// the desktop receives NO operation for it — only the init F2 was
		// dispatched.
		const clickResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(dispatched).toHaveLength(1); // the init F2 only
		const clickText = expectTextOnly(clickResult);
		expect(clickText).toContain("saolei_operate → stopped at click(0,0) (game_won)");
		// The stop result carries the status line for the won state (FR-023).
		expect(clickText).toContain("game status: won");
		expect(clickText).toContain("board size 2*2");

		// flag and chord ops are equally terminal-blocked after the win
		// (FR-021 — "any cell operation").
		const flagResult = await callTool(server, "saolei_operate", { type: "flag", x: 1, y: 0 });
		expect(flagResult.content[0].text).toContain(
			"saolei_operate → stopped at flag(1,0) (game_won)",
		);
		const chordResult = await callTool(server, "saolei_operate", { type: "chord", x: 1, y: 1 });
		expect(chordResult.content[0].text).toContain(
			"saolei_operate → stopped at chord(1,1) (game_won)",
		);
		// Still no further dispatches — only the init F2.
		expect(dispatched).toHaveLength(1);
	});

	it("a winning board's game_won rejection has NO dispatched FlowPart", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const winning = board(["0 F", "1 1"], COUNTER_ZERO);
		const fake = makeFakeBoardApi(winning);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// Pre-condition: only the init F2 has dispatched.
		expect(dispatched).toHaveLength(1);
		const initDispatched = dispatched.length;

		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		// FR-020/FR-021: the post-win op is rejected pre-dispatch; the
		// dispatched count is unchanged (the cell op added NO FlowPart).
		expect(dispatched).toHaveLength(initDispatched);
	});

	it("saolei_init is NOT blocked by a recognized win — it restarts the game (FR-021)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const winning = board(["0 F", "1 1"], COUNTER_ZERO);
		const fake = makeFakeBoardApi(winning);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// After the win, a cell op is terminal-blocked (game_won)…
		const blocked = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(blocked.content[0].text).toContain(
			"saolei_operate → stopped at click(0,0) (game_won)",
		);

		// …but saolei_init re-dispatches F2 unconditionally (contract §6).
		const restart = await callTool(server, "saolei_init", {});
		expect(dispatched.filter((p) => p.keyboardPress?.key === "KEYBOARD_KEY_F2"))
			.toHaveLength(2);
		// The post-restart init result still carries the status line for
		// whatever the new screenshot recognizes (here: still the winning
		// canned board, so `game status: won`).
		expect(restart.content[0].text).toContain("game status: won");
	});
});

// ── counter-informed win (spec 028 FR-012 / SC-004) ─────────────────────────

describe("createSaoleiMcpServer: counter-informed win (028 FR-012 / SC-004)", () => {
	it("an over-flag board (grid all-revealed, counter -01) surfaces 'playing' and is NOT terminal-won (no game_won)", async () => {
		// The saolei_9 over-flag shape: every cell is a revealed number or
		// FLAG (the grid half of a win holds) BUT the mine counter reads
		// `-01` (11 flags > 10 mines) ⇒ isWin(state) === false ⇒ the board is
		// NOT terminal-won. So `game status: playing` is surfaced on init,
		// and a following cell op is NOT rejected as `game_won` (the
		// terminal-win gate did not fire) — it reaches the cell-specific
		// rules normally. This is the false-positive fix (FR-012 / SC-004).
		const { bridge, dispatched } = makeFakeBridge();
		const overFlag = board(["0 F", "1 1"], COUNTER_NEG_ONE);
		const fake = makeFakeBoardApi(overFlag);
		const server = createSaoleiMcpServer(bridge, fake.api);

		// init recognizes the over-flag board ⇒ `game status: playing` (NOT
		// `won` — the counter half fails).
		const initResult = await callTool(server, "saolei_init", {});
		const initText = expectTextOnly(initResult);
		expect(initText).toContain("new game started");
		expect(initText).toContain("game status: playing");
		expect(initText).not.toContain("game status: won");

		// A following cell op is NOT rejected as `game_won` — the
		// terminal-win gate did not fire, so the cell-specific rules apply.
		// A `flag` op on the revealed "0" at (0,0) ⇒ `cannot_flag_revealed`
		// (a cell-specific no-op, NOT `game_won`): SKIPPED, no dispatch, no
		// new FlowPart.
		const preDispatchCount = dispatched.length;
		const flagResult = await callTool(server, "saolei_operate", { type: "flag", x: 0, y: 0 });
		const flagText = expectTextOnly(flagResult);
		expect(flagText).toContain(
			"saolei_operate → executed 0 ops, skipped 1 no-op ops",
		);
		expect(flagText).not.toContain("game_won");
		expect(flagText).toContain("game status: playing");
		expect(dispatched).toHaveLength(preDispatchCount);
	});

	it("an over-flag board's cell op on a FLAG dispatches (NOT rejected as game_won)", async () => {
		// Symmetric to the above but on a FLAG cell — toggling a flag is legal
		// on the over-flag board (the cell-specific rules ALLOW it), so the
		// op DISPATCHES (proving the terminal-win gate `game_won` did not
		// fire). After the dispatch the post-action screenshot recognizes the
		// same over-flag board ⇒ `game status: playing`.
		const { bridge, dispatched } = makeFakeBridge();
		const overFlag = board(["0 F", "1 1"], COUNTER_NEG_ONE);
		const fake = makeFakeBoardApi(overFlag);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const preDispatchCount = dispatched.length;

		// Toggle the flag at (1,0) — legal on a FLAG cell (place/toggle).
		const result = await callTool(server, "saolei_operate", { type: "flag", x: 1, y: 0 });
		const text = expectTextOnly(result);

		// The op DISPATCHED (a new FlowPart beyond the init F2).
		expect(dispatched).toHaveLength(preDispatchCount + 1);
		const part = dispatched[dispatched.length - 1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");

		// The result body carries the dispatched outcome and `game status:
		// playing` (NOT `won`, NOT `game_won`).
		expect(text).toContain("saolei_operate → executed 1 ops");
		expect(text).toContain("game status: playing");
		expect(text).not.toContain("game status: won");
	});
});

// ── signal forwarding (T028) ────────────────────────────────────────────────

describe("createSaoleiMcpServer: signal forwarding (T028)", () => {
	it("forwards the MCP extra.signal to bridge.dispatch", async () => {
		const { bridge, signals } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const controller = new AbortController();
		await callTool(server, "saolei_init", {}, { signal: controller.signal });

		expect(signals).toHaveLength(1);
		expect(signals[0]).toBe(controller.signal);
	});
});

// ── saolei_remain (read-only query, US2 / FR-006..013) ──────────────────────
//
// `saolei_remain` is the read-only fifth tool
// (`specs/029-saolei-coord-remain/contracts/saolei-remain-tool-contract.md`):
// it takes no arguments, dispatches nothing, and returns a per-cell remain
// grid (number cell → `number − adjacent flags`, raw/negative; non-number →
// `-`) sharing the board grid's coordinate ruler. Its only rejection is
// `no_active_game`; terminal `won`/`lost` boards are NOT blocked.

describe("createSaoleiMcpServer: saolei_remain (read-only)", () => {
	it("returns the remain grid for a recognized board (FR-008..010)", async () => {
		const { bridge } = makeFakeBridge();
		// (0,0)="3" with one adjacent FLAG (1,0)="F" → remain 2; every other
		// cell is a non-number → "-".
		const fake = makeFakeBoardApi(board(["3 F", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const result = await callTool(server, "saolei_remain", {});

		const text = expectTextOnly(result);
		expect(text).toContain("saolei_remain → computed");
		expect(text).toContain("game status: playing");
		expect(text).toContain("board size 2*2");
		// Tagged ruler (FR-010 / SC-004 — shared with the board grid).
		expect(text).toContain("col0");
		expect(text).toContain("col1");
		expect(text).toContain("row0");
		expect(text).toContain("row1");
		// Remain 2 at (0,0); the rest of the grid is "-".
		// columnWidth=4 ⇒ row0 is "row0    2    -" and row1 is "row1    -    -".
		expect(text).toMatch(/row0\s+2\s+-/);
		expect(text).toMatch(/row1\s+-\s+-/);
	});

	it("dispatches NOTHING and does not mutate recognized (FR-007 / SC-003)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const initial = board(["3 F", "* *"]);
		const fake = makeFakeBoardApi(initial);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const initDispatched = dispatched.length;

		// First saolei_remain — dispatches nothing.
		const r1 = await callTool(server, "saolei_remain", {});
		expect(dispatched).toHaveLength(initDispatched);
		const t1 = expectTextOnly(r1);
		expect(t1).toContain("saolei_remain → computed");

		// A second saolei_remain returns the SAME remain grid — recognized
		// was not mutated (FR-007: MUST NOT mutate recognized). If
		// saolei_remain had nulled or altered recognized, this would differ.
		const r2 = await callTool(server, "saolei_remain", {});
		expect(dispatched).toHaveLength(initDispatched);
		expect(expectTextOnly(r2)).toBe(t1);

		// A subsequent saolei_operate click sees the UNCHANGED recognized
		// board: the click at the INITIAL cell (1,1) is legal (proves
		// recognized still holds `initial`), dispatches exactly one new
		// FlowPart, and the post-click board is the canned update — no
		// corruption by the prior saolei_remain calls.
		fake.setUpdate(initial);
		const clickResult = await callTool(server, "saolei_operate", { type: "click", x: 1, y: 1 });
		expect(dispatched).toHaveLength(initDispatched + 1);
		const clickText = expectTextOnly(clickResult);
		expect(clickText).toContain("saolei_operate → executed 1 ops");
		// Cell (0,0) is still "3" in the post-click board — saolei_remain
		// did not mutate recognized.
		expect(clickText).toContain("3");
	});

	it("rejects with no_active_game before init (FR-011)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_remain", {});

		// No dispatch at all — no game has been started.
		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: no_active_game");
		expect(text).toContain("call saolei_init first");
		// No status line, no grid (contract §4 — byte-identical to the cell
		// tools' no_active_game body).
		expect(text).not.toContain("game status:");
		expect(text).not.toContain("board size");
	});

	it("returns the remain grid on a terminal WON board (FR-012)", async () => {
		const { bridge } = makeFakeBridge();
		// All cells revealed numbers / FLAG with a decoded 000 counter ⇒
		// isWin(state) === true ⇒ game status: won.
		const winning = board(["0 F", "1 1"], COUNTER_ZERO);
		const fake = makeFakeBoardApi(winning);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const result = await callTool(server, "saolei_remain", {});

		const text = expectTextOnly(result);
		// NOT rejected — read-only; no move attempted, so the terminal-win
		// guard in validateMove never runs (contract §5 / FR-012).
		expect(text).toContain("saolei_remain → computed");
		expect(text).not.toContain("rejected:");
		expect(text).toContain("game status: won");
		expect(text).toContain("board size 2*2");
	});

	it("returns the remain grid on a terminal LOST board (FR-012)", async () => {
		const { bridge } = makeFakeBridge();
		// HIT_MINE present ⇒ terminal loss.
		const losing = board(["X *", "* *"]);
		const fake = makeFakeBoardApi(losing);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const result = await callTool(server, "saolei_remain", {});

		const text = expectTextOnly(result);
		expect(text).toContain("saolei_remain → computed");
		expect(text).not.toContain("rejected:");
		expect(text).toContain("game status: lost");
		expect(text).toContain("board size 2*2");
	});

	it("surfaces a NEGATIVE remain for an over-flagged cell (FR-013, raw not clamped)", async () => {
		const { bridge } = makeFakeBridge();
		// (1,0)="1" with TWO adjacent FLAGs → remain -1 (over-flagged, raw —
		// NOT clamped to 0). The lone "-" non-number marker occupies its own
		// slot, distinct from the "-1" negative remain token.
		const overFlag = board(["F 1 F"]);
		const fake = makeFakeBoardApi(overFlag);
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const result = await callTool(server, "saolei_remain", {});

		const text = expectTextOnly(result);
		expect(text).toContain("saolei_remain → computed");
		expect(text).toContain("board size 3*1");
		// Raw -1 present (columnWidth=4 ⇒ row0 is "row0    -   -1   -").
		expect(text).toMatch(/row0\s+-\s+-1\s+-/);
	});
});

// ── event sink (US3 / FR-019..FR-022 + 039 US1 FR-004) ──────────────────────
//
// `SaoleiEventSink` is the optional out-of-band event sink
// (`specs/031-team-template-mode/contracts/saolei-sink-contract.md`):
// `onGameStart` after a successful `saolei_init` recognition; `onOperate`
// after a `saolei_operate` call completes (ONE callback per call, single or
// batch, carrying the full operations list — FR-004,
// `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`
// §4); `onGameEnd` (structured `"won"|"lost"` enum, FR-022) when `gameStatus`
// turns terminal. `saolei_remain` (read-only) never fires. A
// throwing/rejecting sink must not affect the tool result (contract §5). No
// sink registered ⇒ behaviour unchanged (FR-020) — covered by every other
// describe in this file, which builds servers without a sink.

/** A recorded sink call, discriminated by `kind`. */
type SinkCall =
	| { kind: "onGameStart"; state: GameState }
	| {
			kind: "onOperate";
			operations: CellOperation[];
			state: GameState;
			stats?: GameStats;
	  }
	| {
			kind: "onGameEnd";
			state: GameState;
			status: "won" | "lost";
			stats?: GameStats;
	  };

/** Build a recording `SaoleiEventSink` (DI seam — no `vi.mock`). */
function makeRecordingSink(): {
	sink: SaoleiEventSink;
	calls: SinkCall[];
} {
	const calls: SinkCall[] = [];
	return {
		sink: {
			onGameStart: (state) => {
				calls.push({ kind: "onGameStart", state });
			},
			onOperate: (operations, state, stats) => {
				calls.push({ kind: "onOperate", operations, state, stats });
			},
			onGameEnd: (state, status, stats) => {
				calls.push({ kind: "onGameEnd", state, status, stats });
			},
		},
		calls,
	};
}

describe("createSaoleiMcpServer: event sink (FR-019..FR-022)", () => {
	it("fires onGameStart with the initial state after a successful saolei_init", async () => {
		const { bridge } = makeFakeBridge();
		const initial = board(["* *", "* *"]);
		const fake = makeFakeBoardApi(initial);
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		const result = await callTool(server, "saolei_init", {});

		expectTextOnly(result);
		expect(recording.calls).toHaveLength(1);
		const start = recording.calls[0] as Extract<SinkCall, { kind: "onGameStart" }>;
		expect(start.kind).toBe("onGameStart");
		expect(start.state).toEqual(initial);
	});

	it("does NOT fire onGameStart when init recognition fails (FR-017 path)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["*"]));
		fake.setInit("throw");
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});

		expect(recording.calls).toHaveLength(0);
	});

	it("fires onOperate once per saolei_operate call with the FULL operations list and the updated state (FR-004)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0; // drop the onGameStart call
		const updated = board(["0 *", "* *"]);
		fake.setUpdate(updated);

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 0, y: 0 },
				{ type: "flag", x: 1, y: 0 },
			],
		});

		expectTextOnly(result);
		// ONE callback per call (single or batch — never one per op, FR-004).
		expect(recording.calls).toHaveLength(1);
		const op = recording.calls[0] as Extract<SinkCall, { kind: "onOperate" }>;
		expect(op.kind).toBe("onOperate");
		expect(op.operations).toEqual([
			{ type: "click", x: 0, y: 0 },
			{ type: "flag", x: 1, y: 0 },
		]);
		expect(op.state).toEqual(updated);
	});

	it("fires onOperate then onGameEnd('won') when a move completes a win", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;
		// The post-move screenshot recognizes the counter-informed winning
		// board (grid fully revealed/flagged AND decoded 000 counter — spec
		// 028), so `gameStatus` reads `won` (FR-017 first-hand computation).
		fake.setUpdate(board(["0 F", "1 1"], COUNTER_ZERO));

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		expectTextOnly(result);
		expect(recording.calls.map((c) => c.kind)).toEqual([
			"onOperate",
			"onGameEnd",
		]);
		const end = recording.calls[1] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(end.status).toBe("won");
	});

	it("fires onOperate then onGameEnd('lost') when a move loses the game", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;
		// HIT_MINE present ⇒ terminal loss (`isTerminalState`).
		fake.setUpdate(board(["X *", "* *"]));

		const result = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		expectTextOnly(result);
		expect(recording.calls.map((c) => c.kind)).toEqual([
			"onOperate",
			"onGameEnd",
		]);
		const end = recording.calls[1] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(end.status).toBe("lost");
	});

	it("fires ONLY onOperate for an in-progress call (no onGameEnd)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;
		fake.setUpdate(board(["0 *", "* *"])); // still playing

		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		expect(recording.calls.map((c) => c.kind)).toEqual(["onOperate"]);
	});

	it("does NOT fire any sink event for saolei_remain (read-only, contract §3)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["3 F", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;

		await callTool(server, "saolei_remain", {});

		expect(recording.calls).toHaveLength(0);
	});

	it("isolates a THROWING sink — the tool result is unchanged (contract §5)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const throwing: SaoleiEventSink = {
			onGameStart: () => {
				throw new Error("sink boom");
			},
			onOperate: () => {
				throw new Error("sink boom");
			},
			onGameEnd: () => {
				throw new Error("sink boom");
			},
		};
		const server = createSaoleiMcpServer(bridge, fake.api, throwing);

		const initResult = await callTool(server, "saolei_init", {});
		expect(initResult.content[0].text).toContain("new game started");

		fake.setUpdate(board(["0 *", "* *"]));
		const moveResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(moveResult.content[0].text).toContain("saolei_operate → executed 1 ops");
	});

	it("isolates a REJECTING (async) sink — the tool result is unchanged (contract §5)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const rejecting: SaoleiEventSink = {
			onGameStart: () => Promise.reject(new Error("sink boom")),
			onOperate: () => Promise.reject(new Error("sink boom")),
			onGameEnd: () => Promise.reject(new Error("sink boom")),
		};
		const server = createSaoleiMcpServer(bridge, fake.api, rejecting);

		const initResult = await callTool(server, "saolei_init", {});
		const text = expectTextOnly(initResult);
		expect(text).toContain("new game started");

		fake.setUpdate(board(["0 *", "* *"]));
		const moveResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		expect(moveResult.content[0].text).toContain("saolei_operate → executed 1 ops");
	});

	it("isolates a THROWING sink on a game-ending call — onOperate+onGameEnd throws leave the tool result unchanged", async () => {
		// Terminal-move variant of the throwing-sink case: the post-move
		// screenshot recognizes the counter-informed winning board (grid
		// fully revealed/flagged AND decoded 000 counter, spec 028), so the
		// click triggers BOTH `onOperate` and `onGameEnd("won")` — both throws
		// must be isolated by `runSink` (contract §5) and the tool result
		// must still be the normal operate result text.
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const throwing: SaoleiEventSink = {
			onGameStart: () => {
				throw new Error("sink boom");
			},
			onOperate: () => {
				throw new Error("sink boom");
			},
			onGameEnd: () => {
				throw new Error("sink boom");
			},
		};
		const server = createSaoleiMcpServer(bridge, fake.api, throwing);

		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["0 F", "1 1"], COUNTER_ZERO));

		const moveResult = await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		expect(moveResult.content[0].text).toContain(
			"saolei_operate → stopped at click(0,0) (won)",
		);
		// The terminal path ran: the result carries the won status line.
		expect(moveResult.content[0].text).toContain("game status: won");
	});
});

// ── game stats (037 US5 / FR-026..FR-033) ──────────────────────────────────
//
// `computeGameStats` is the MCP's first-hand per-game statistics computation
// (contracts/game-stats-contract.md §3): operationCount = successful cell
// operations, correctFlags = totalMines − MINE − HIT_MINE (totalMines from
// the init mineCounter, which reads the mine total when flags = 0), and
// avgOpsPerMine = operationCount / correctFlags rounded to 2 decimals.
// Pure-function tests (a)–(e) cover the arithmetic and the FR-029/FR-033
// degradations; the flow tests (f)–(g) cover the MCP closure's counting
// discipline (rejected moves / init / remain are NOT counted — FR-027).

describe("computeGameStats: pure per-game statistics (037 US5 / FR-026..FR-033)", () => {
	it("(a) known game: operationCount, correctFlags and avgOpsPerMine match the expected values", () => {
		// 5 mines at start; the final board exposes 1 MINE + 1 HIT_MINE
		// (unflagged mines), so correctFlags = 5 − 1 − 1 = 3. With 7
		// operations, avg = round(7/3 × 100)/100 = 2.33 (rounded to 2
		// decimals — FR-029).
		const init = board(["* *", "* *"], { decoded: true, value: 5 });
		const finalState = board(["M X *", "* * *", "* * *"]);
		expect(computeGameStats(init, finalState, 7)).toEqual({
			operationCount: 7,
			correctFlags: 3,
			avgOpsPerMine: 2.33,
		});
	});

	it("(b) won game: MINE=0 and HIT_MINE=0 ⇒ correctFlags = totalMines", () => {
		// Won board: every mine correctly flagged, no MINE/HIT_MINE cell
		// (specs/037-saolei-team-optimize/spec.md FR-028 / contract §7).
		const init = board(["* * *", "* * *", "* * *"], { decoded: true, value: 2 });
		const won = board(["F 1 1", "1 F 1", "1 1 1"], COUNTER_ZERO);
		expect(computeGameStats(init, won, 10)).toEqual({
			operationCount: 10,
			correctFlags: 2,
			avgOpsPerMine: 5,
		});
	});

	it("(c) lost game: correctFlags = totalMines − MINE − HIT_MINE", () => {
		// 10 mines at start; the final board shows 4 MINE + 2 HIT_MINE ⇒
		// correctFlags = 10 − 4 − 2 = 4.
		const init = board(["* * * *", "* * * *", "* * * *", "* * * *"], {
			decoded: true,
			value: 10,
		});
		const lost = board(["M M M M", "X X * *", "* * * *", "* * * *"]);
		expect(computeGameStats(init, lost, 8)).toEqual({
			operationCount: 8,
			correctFlags: 4,
			avgOpsPerMine: 2,
		});
	});

	it("(d) y=0 (instant loss): avgOpsPerMine = 'N/A' — no NaN/Infinity (FR-029)", () => {
		// First click hits a mine: all 3 mines end revealed, none flagged ⇒
		// correctFlags = 0 ⇒ the division is guarded to "N/A".
		const init = board(["* * *"], { decoded: true, value: 3 });
		const lost = board(["X M M"]);
		expect(computeGameStats(init, lost, 1)).toEqual({
			operationCount: 1,
			correctFlags: 0,
			avgOpsPerMine: "N/A",
		});
	});

	it("(e) undecodable init counter ⇒ correctFlags = null, avgOpsPerMine = 'N/A' (FR-033)", () => {
		const lost = board(["X M", "* *"]);
		// { decoded: false } — a digit pattern matched no glyph entry.
		const undecoded = board(["* *", "* *"], { decoded: false });
		expect(computeGameStats(undecoded, lost, 5)).toEqual({
			operationCount: 5,
			correctFlags: null,
			avgOpsPerMine: "N/A",
		});
		// Absent counter (`undefined`) degrades identically (FR-033).
		const noCounter = board(["* *", "* *"]);
		expect(computeGameStats(noCounter, lost, 5)).toEqual({
			operationCount: 5,
			correctFlags: null,
			avgOpsPerMine: "N/A",
		});
	});

	it("is lenient when initState is null (no init captured) — correctFlags = null", () => {
		// An onGameEnd without a prior successful init: totalMines unknown ⇒
		// correctFlags = null (FR-033), no crash.
		const lost = board(["X *", "* *"]);
		expect(computeGameStats(null, lost, 3)).toEqual({
			operationCount: 3,
			correctFlags: null,
			avgOpsPerMine: "N/A",
		});
	});
});

describe("createSaoleiMcpServer: game stats on onGameEnd (037 US5 / FR-026..FR-032)", () => {
	it("(f) rejected moves are NOT counted in operationCount (FR-027)", async () => {
		const { bridge } = makeFakeBridge();
		// 2×2 board, 2 mines at start (counter = mines when flags = 0).
		const initial = board(["* *", "* *"], { decoded: true, value: 2 });
		const fake = makeFakeBoardApi(initial);
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;

		// A rejected move (out of bounds, structural) is NOT a successful
		// cell op — the batch stops before dispatch and the op is NOT
		// counted in operationCount (FR-027, asserted below). The call
		// itself still happened on an ACTIVE game, so it records ONE
		// gameLog entry via onOperate (039 FR-004 — one entry per call,
		// however the batch went).
		const rejected = await callTool(server, "saolei_operate", { type: "click", x: 5, y: 5 });
		expect(rejected.content[0].text).toContain(
			"saolei_operate → stopped at click(5,5) (out_of_bounds)",
		);
		// 039 FR-004: the structurally-rejected call on an active game
		// still fires exactly ONE onOperate, carrying the rejected op + the
		// unchanged board — but the op is NOT counted in operationCount.
		expect(recording.calls).toHaveLength(1);
		const rejectedOp = recording.calls[0] as Extract<SinkCall, { kind: "onOperate" }>;
		expect(rejectedOp.kind).toBe("onOperate");
		expect(rejectedOp.operations).toEqual([{ type: "click", x: 5, y: 5 }]);
		expect(rejectedOp.state).toEqual(initial);

		// The single legal move ends the game (hits a mine ⇒ lost).
		fake.setUpdate(board(["X *", "* *"]));
		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		// The last call (a legal op) is the only one that ended the game:
		// its onOperate + onGameEnd sit AFTER the rejected call's onOperate,
		// so the final element is still the onGameEnd.
		const end = recording.calls[
			recording.calls.length - 1
		] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(end.stats).toEqual({
			// Only the legal move counted — the rejection was NOT (FR-027).
			operationCount: 1,
			// 2 mines − 0 MINE − 1 HIT_MINE.
			correctFlags: 1,
			avgOpsPerMine: 1,
		});
	});

	it("(g) init and remain are NOT counted in operationCount (FR-027)", async () => {
		const { bridge } = makeFakeBridge();
		const initial = board(["* *", "* *"], { decoded: true, value: 2 });
		const fake = makeFakeBoardApi(initial);
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;

		// saolei_remain is read-only — no sink event, no operation count.
		await callTool(server, "saolei_remain", {});
		expect(recording.calls).toHaveLength(0);

		// One legal move wins the game (both mines correctly flagged ⇒ the
		// won board carries 2 FLAGs and no MINE/HIT_MINE).
		fake.setUpdate(board(["F 0", "F 1"], COUNTER_ZERO));
		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });

		const end = recording.calls[
			recording.calls.length - 1
		] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(end.stats).toEqual({
			// Only the legal move counted — init + remain were NOT (FR-027).
			operationCount: 1,
			// Won: MINE=0, HIT_MINE=0 ⇒ correctFlags = totalMines = 2.
			correctFlags: 2,
			avgOpsPerMine: 0.5,
		});
	});

	it("resets operationCount across games (FR-027 — per-game counter)", async () => {
		const { bridge } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"], { decoded: true, value: 2 }));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		// Game 1: one legal move wins.
		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["F 0", "F 1"], COUNTER_ZERO));
		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		const game1End = recording.calls[
			recording.calls.length - 1
		] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(game1End.stats?.operationCount).toBe(1);

		// Game 2 (re-init): the counter must reset — two legal moves then a
		// loss ⇒ operationCount = 2 (not 3 — the game-1 move is excluded).
		recording.calls.length = 0;
		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["0 *", "* *"])); // first move: playing
		await callTool(server, "saolei_operate", { type: "click", x: 0, y: 0 });
		fake.setUpdate(board(["X *", "* *"])); // second move: lost
		await callTool(server, "saolei_operate", { type: "click", x: 1, y: 0 });

		const game2End = recording.calls[
			recording.calls.length - 1
		] as Extract<SinkCall, { kind: "onGameEnd" }>;
		expect(game2End.stats).toEqual({
			operationCount: 2,
			correctFlags: 1,
			avgOpsPerMine: 2,
		});
	});
});

// ── 039 US1: saolei_operate dual-form / batch triage (FR-001..FR-005) ───────
//
// `specs/039-planner-memory-calibration/contracts/saolei-operate-contract.md`:
//   §1  — dual-form parameters (single type/x/y OR operations array);
//   §2  — in-order execution with failure triage (SKIP no-ops / STOP
//         structural & game end, one result);
//   §3  — dual-form equivalence (single == length-1 operations);
//   §5  — ambiguous (both/neither/partial) and empty-combination handling.

describe("saolei_operate: dual-form equivalence (contract §3 / FR-001)", () => {
	it("single type/x/y is equivalent to a length-1 operations array (same validation, same dispatch, same result)", async () => {
		// Two IDENTICAL fresh servers (one per form — a second call on the
		// same server would see the first call's revealed cell as a no-op).
		// Each post-action screenshot recognizes the same "playing" board,
		// so the two calls' result bodies must be byte-identical (contract
		// §3 — single == length-1 batch, also equivalent to the former
		// single-click tool).
		const { bridge: bridgeA, dispatched: dispatchedA } = makeFakeBridge();
		const fakeA = makeFakeBoardApi(board(["* *", "* *"]));
		const serverA = createSaoleiMcpServer(bridgeA, fakeA.api);
		await callTool(serverA, "saolei_init", {});
		fakeA.setUpdate(board(["0 *", "* *"]));

		const { bridge: bridgeB, dispatched: dispatchedB } = makeFakeBridge();
		const fakeB = makeFakeBoardApi(board(["* *", "* *"]));
		const serverB = createSaoleiMcpServer(bridgeB, fakeB.api);
		await callTool(serverB, "saolei_init", {});
		fakeB.setUpdate(board(["0 *", "* *"]));

		const single = await callTool(serverA, "saolei_operate", {
			type: "click",
			x: 0,
			y: 0,
		});
		const batch = await callTool(serverB, "saolei_operate", {
			operations: [{ type: "click", x: 0, y: 0 }],
		});

		// Both dispatched exactly one LEFT_CLICK at the same cell centre
		// (init F2 + the single op in each server).
		expect(dispatchedA).toHaveLength(2);
		expect(dispatchedB).toHaveLength(2);
		const singlePart = dispatchedA[1].mouseMoveAndClick as MouseMoveAndClickPart;
		const batchPart = dispatchedB[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(singlePart.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(batchPart.click).toBe("MOUSE_CLICK_ACTION_LEFT_CLICK");
		expect(singlePart.xPx).toBe(batchPart.xPx);
		expect(singlePart.yPx).toBe(batchPart.yPx);

		// Byte-identical text results (contract §3 — the single form is the
		// length-1 batch; both are "executed 1 ops" with the same board).
		expect(expectTextOnly(single)).toBe(expectTextOnly(batch));
		expect(expectTextOnly(single)).toContain("saolei_operate → executed 1 ops");
	});
});

describe("saolei_operate: in-order batch execution (FR-001/FR-002)", () => {
	it("executes a mixed click/flag/chord batch IN ORDER and returns ONE result", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		// (0,0)="1" (chordable, has INITIAL neighbors); (1,0) and (0,1) are
		// INITIAL (clickable/flagable); (1,1) is INITIAL too.
		const fake = makeFakeBoardApi(board(["1 *", "1 *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const initDispatched = dispatched.length;

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 1, y: 0 },
				{ type: "flag", x: 1, y: 1 },
				{ type: "chord", x: 0, y: 0 },
			],
		});

		// THREE dispatches in the batch's declared order (init + 3 ops).
		expect(dispatched).toHaveLength(initDispatched + 3);
		const clicks = dispatched
			.slice(initDispatched)
			.map((p) => p.mouseMoveAndClick?.click);
		expect(clicks).toEqual([
			"MOUSE_CLICK_ACTION_LEFT_CLICK",
			"MOUSE_CLICK_ACTION_RIGHT_CLICK",
			"MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS",
		]);
		// ONE result body (FR-002 — a single MCP text block per call).
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → executed 3 ops");
		expect(text).toContain("game status: playing");
	});
});

describe("saolei_operate: failure triage (FR-002 / contract §2)", () => {
	it("SKIPS a harmless no-op and CONTINUES the batch (earlier/later ops still execute)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		// (0,0) is already revealed ("0") — a click there is a harmless no-op.
		const fake = makeFakeBoardApi(board(["0 *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const initDispatched = dispatched.length;

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 0, y: 0 }, // no-op → SKIP
				{ type: "click", x: 1, y: 1 }, // legal → executes
			],
		});

		// Only the legal op dispatched (init + 1); the no-op did NOT.
		expect(dispatched).toHaveLength(initDispatched + 1);
		const text = expectTextOnly(result);
		expect(text).toContain(
			"saolei_operate → executed 1 ops, skipped 1 no-op ops",
		);
		expect(text).toContain("game status: playing");
	});

	it("STOPS at a structural rejection (out_of_bounds); remaining ops do NOT execute (earlier ops take effect)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		const initDispatched = dispatched.length;

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 0, y: 0 }, // legal → executes
				{ type: "click", x: 5, y: 5 }, // out of bounds → STOP
				{ type: "flag", x: 1, y: 1 }, // MUST NOT execute
			],
		});

		// The first op took effect; the third never ran (init + 1).
		expect(dispatched).toHaveLength(initDispatched + 1);
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → stopped at click(5,5) (out_of_bounds)");
		expect(text).toContain("game status: playing");
	});

	it("STOPS when an op ends the game; remaining ops do NOT execute (the winning/losing op takes effect)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;
		const initDispatched = dispatched.length;
		// The first op's post-action screenshot recognizes a terminal LOSS.
		fake.setUpdate(board(["X *", "* *"]));

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 0, y: 0 }, // hits the mine → game lost
				{ type: "flag", x: 1, y: 1 }, // MUST NOT execute
			],
		});

		// The losing op took effect; the batch stopped at click(0,0) (init + 1).
		expect(dispatched).toHaveLength(initDispatched + 1);
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → stopped at click(0,0) (lost)");
		expect(text).toContain("game status: lost");
		// The sink fired ONCE per call: onOperate (with the FULL op list,
		// including the unexecuted one) + onGameEnd (FR-004/FR-022).
		expect(recording.calls.map((c) => c.kind)).toEqual([
			"onOperate",
			"onGameEnd",
		]);
		const op = recording.calls[0] as Extract<SinkCall, { kind: "onOperate" }>;
		expect(op.operations).toEqual([
			{ type: "click", x: 0, y: 0 },
			{ type: "flag", x: 1, y: 1 },
		]);
	});

	it("fires onOperate ONCE for an all-skip batch — every call on an active game is recorded, even when no op executed (FR-004)", async () => {
		// Both ops target already-revealed cells → the whole batch is
		// harmless no-ops (executed = 0, skipped = 2). FR-004 still records
		// the call as ONE gameLog entry: onOperate MUST fire with the full
		// op list + the final (unchanged) state.
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["0 0", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		recording.calls.length = 0;
		const initDispatched = dispatched.length;

		const result = await callTool(server, "saolei_operate", {
			operations: [
				{ type: "click", x: 0, y: 0 }, // revealed → SKIP
				{ type: "flag", x: 1, y: 0 }, // revealed → SKIP
			],
		});

		// Zero dispatches (nothing executed)…
		expect(dispatched).toHaveLength(initDispatched);
		const text = expectTextOnly(result);
		expect(text).toContain(
			"saolei_operate → executed 0 ops, skipped 2 no-op ops",
		);
		// …but the call IS recorded: exactly ONE onOperate (no onGameEnd —
		// the board is unchanged and still playing).
		expect(recording.calls).toHaveLength(1);
		const op = recording.calls[0] as Extract<SinkCall, { kind: "onOperate" }>;
		expect(op.kind).toBe("onOperate");
		expect(op.operations).toEqual([
			{ type: "click", x: 0, y: 0 },
			{ type: "flag", x: 1, y: 0 },
		]);
		expect(op.state).toEqual(board(["0 0", "* *"]));
	});
});

describe("saolei_operate: empty / ambiguous argument combinations (contract §5)", () => {
	it("empty operations list is a no-op returning the current state (no dispatch, no sink)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const recording = makeRecordingSink();
		const server = createSaoleiMcpServer(bridge, fake.api, recording.sink);

		await callTool(server, "saolei_init", {});
		const initDispatched = dispatched.length;
		recording.calls.length = 0;

		const result = await callTool(server, "saolei_operate", {
			operations: [],
		});

		// Zero side effects (spec Edge Case — no board mutation, no sink).
		expect(dispatched).toHaveLength(initDispatched);
		expect(recording.calls).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("saolei_operate → executed 0 ops");
		expect(text).toContain("game status: playing");
	});

	it("empty operations list before init returns the no_active_game guidance (no side effect)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_operate", {
			operations: [],
		});

		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: no_active_game");
		expect(text).toContain("call saolei_init first");
	});

	it("rejects supplying BOTH the single form and operations (ambiguous)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_operate", {
			type: "click",
			x: 0,
			y: 0,
			operations: [{ type: "click", x: 0, y: 0 }],
		});

		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("provide EITHER type/x/y (single operation) OR operations (batch), not both");
	});

	it("rejects supplying NEITHER the single form nor operations", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_operate", {});

		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("provide EITHER type/x/y (single operation) OR an operations array (batch)");
	});

	it("rejects a PARTIAL single form (type without x/y)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_operate", {
			type: "click",
		});

		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("requires ALL of type, x and y together");
	});
});
