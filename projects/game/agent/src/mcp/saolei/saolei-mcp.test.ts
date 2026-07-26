/**
 * saolei-mcp.test.ts — Tests for the session-bound saolei MCP server with
 * recognized text-board state and strict pre-dispatch validation.
 *
 * Coverage (Phase 4 / US3, spec 025 FR-012..FR-022;
 * `specs/025-desktop-image-state-refine/contracts/saolei-mcp-contract.md`):
 *   - Exactly four tools are exposed (`saolei_init`, `saolei_click`,
 *     `saolei_flag`, `saolei_chord_click`); `saolei_update` is absent.
 *   - `saolei_init` takes no `width`/`height` arguments (FR-019).
 *   - Every tool returns a single TEXT board block — NEVER an image block
 *     (FR-012 / SC-003).
 *   - `saolei_init` returns the initial text board; a legal `saolei_click`
 *     dispatches and returns the updated text board.
 *   - Each illegal rule is rejected BEFORE dispatch with the right reason
 *     code (contract §4): `cell_already_revealed`, `cell_is_flagged`,
 *     `cannot_flag_revealed`, `chord_requires_number`, `out_of_bounds`,
 *     `no_active_game`, `game_over`.
 *   - A chord on a revealed number with a mismatched adjacent-flag count is
 *     NOT rejected (FR-015e).
 *   - Recognition failure (`init`/`update` throwing, or no screenshot) →
 *     "unable to recognize board", state invalidated, subsequent ops rejected
 *     as `no_active_game` (FR-017).
 *   - The MCP `extra.signal` is forwarded to `bridge.dispatch` (T028).
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
} from "./saolei-mcp";
import type { SaoleiBoardApi, CellTool } from "./saolei-mcp";
import { BOARD_ORIGIN_X_PX, BOARD_ORIGIN_Y_PX, CELL_SIZE_PX } from "./geometry";
import type { CellStatus, GameState } from "@dominion/game-saolei-board";
import type { FlowPart } from "../../../../game_types/projects/game/FlowPart";
import type { AgentFrame } from "../../../../game_types/projects/game/AgentFrame";
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
 */
function board(rows: string[]): GameState {
	const height = rows.length;
	const width = rows[0]?.split(/\s+/).length ?? 0;
	const grid = rows.map((r) =>
		r.split(/\s+/).map((s) => SYMBOL_TO_STATUS[s] ?? "UNKNOWN"),
	);
	return { width, height, grid };
}

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
	bridge.registerSink((frame: AgentFrame) => {
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
		expect(validateMove(s, "saolei_click", 5, 5)).toEqual({
			ok: false,
			reason: "out_of_bounds",
		});
	});

	it("rejects any cell op when the state is terminal (HIT_MINE present)", () => {
		const s = board(["X *", "* *"]);
		expect(validateMove(s, "saolei_click", 1, 0)).toEqual({
			ok: false,
			reason: "game_over",
		});
	});

	it("rejects any cell op when the state is terminal (MINE present)", () => {
		const s = board(["M *", "* *"]);
		expect(validateMove(s, "saolei_flag", 1, 0)).toEqual({
			ok: false,
			reason: "game_over",
		});
	});

	it("saolei_click: rejects a revealed number (cell_already_revealed)", () => {
		const s = board(["0 *", "* *"]);
		expect(validateMove(s, "saolei_click", 0, 0)).toEqual({
			ok: false,
			reason: "cell_already_revealed",
		});
	});

	it("saolei_click: rejects a flag (cell_is_flagged)", () => {
		const s = board(["F *", "* *"]);
		expect(validateMove(s, "saolei_click", 0, 0)).toEqual({
			ok: false,
			reason: "cell_is_flagged",
		});
	});

	it("saolei_click: allows an unrevealed cell", () => {
		const s = board(["* *", "* *"]);
		expect(validateMove(s, "saolei_click", 1, 1)).toEqual({ ok: true });
	});

	it("saolei_flag: rejects a revealed number (cannot_flag_revealed)", () => {
		const s = board(["3 *", "* *"]);
		expect(validateMove(s, "saolei_flag", 0, 0)).toEqual({
			ok: false,
			reason: "cannot_flag_revealed",
		});
	});

	it("saolei_flag: allows flagging an unrevealed cell and toggling a flag", () => {
		const initial = board(["* *", "* *"]);
		expect(validateMove(initial, "saolei_flag", 0, 0)).toEqual({ ok: true });
		const flagged = board(["F *", "* *"]);
		expect(validateMove(flagged, "saolei_flag", 0, 0)).toEqual({ ok: true });
	});

	it("saolei_chord_click: rejects 0 / INITIAL / FLAG (chord_requires_number)", () => {
		const zero = board(["0 *", "* *"]);
		expect(validateMove(zero, "saolei_chord_click", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
		const initial = board(["* *", "* *"]);
		expect(validateMove(initial, "saolei_chord_click", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
		const flag = board(["F *", "* *"]);
		expect(validateMove(flag, "saolei_chord_click", 0, 0)).toEqual({
			ok: false,
			reason: "chord_requires_number",
		});
	});

	it("saolei_chord_click: allows a chord on a revealed number even when the adjacent-flag count does NOT match (FR-015e)", () => {
		// Cell (0,0) is "1" but no neighbor is flagged → flag-count (0) ≠ 1.
		// Validation judges target-cell compatibility, not predicted outcome.
		const s = board(["1 *", "* *"]);
		expect(validateMove(s, "saolei_chord_click", 0, 0)).toEqual({ ok: true });
	});

	it("UNKNOWN target is always lenient (FR-018)", () => {
		const s = board(["? *", "* *"]);
		expect(validateMove(s, "saolei_click", 0, 0)).toEqual({ ok: true });
		expect(validateMove(s, "saolei_flag", 0, 0)).toEqual({ ok: true });
		expect(validateMove(s, "saolei_chord_click", 0, 0)).toEqual({ ok: true });
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

describe("createSaoleiMcpServer: tool registration (FR-020)", () => {
	it("registers exactly the four saolei tools (no saolei_update)", async () => {
		const { bridge } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		// eslint-disable-next-line @typescript-eslint/no-explicit-any
		const inner = (server as any).server;
		const handler = inner._requestHandlers.get("tools/list");
		const result = await handler({ method: "tools/list", params: {} });

		const names = (result.tools as { name: string }[]).map((t) => t.name);
		expect(names.sort()).toEqual(
			[
				"saolei_chord_click",
				"saolei_click",
				"saolei_flag",
				"saolei_init",
			].sort(),
		);
		expect(names).not.toContain("saolei_update");
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
		expect(text).toContain("* *");
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
	});
});

// ── legal cell op dispatches + updated text board ───────────────────────────

describe("createSaoleiMcpServer: legal cell op dispatches and returns updated text (FR-012/FR-019)", () => {
	it("saolei_click on an unrevealed cell dispatches and returns the updated text board", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// After the legal click, recognition reports cell (0,0) revealed as 0.
		fake.setUpdate(board(["0 *", "* *"]));

		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

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
		expect(text).toContain("saolei_click at (0,0) → dispatched");
		expect(text).toContain("0 *");
	});

	it("saolei_flag dispatches a RIGHT_CLICK at the cell centre", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["F *", "* *"]));

		await callTool(server, "saolei_flag", { x: 0, y: 0 });

		const part = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(part.xPx).toBe(centerX(0));
		expect(part.yPx).toBe(centerY(0));
	});

	it("saolei_chord_click on a revealed number dispatches a LEFT_RIGHT_PRESS (flag-count mismatch still legal)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		// (0,0) is "1" with NO neighbor flagged — mismatched, but legal (FR-015e).
		const fake = makeFakeBoardApi(board(["1 *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		fake.setUpdate(board(["1 *", "* *"]));

		const result = await callTool(
			server,
			"saolei_chord_click",
			{ x: 0, y: 0 },
		);

		// Chord dispatched (NOT rejected) despite the flag-count mismatch.
		expect(dispatched).toHaveLength(2);
		const part = dispatched[1].mouseMoveAndClick as MouseMoveAndClickPart;
		expect(part.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
		expect(part.method).toBe("MOUSE_INPUT_METHOD_WINDOW_MESSAGE");
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain(
			"saolei_chord_click at (0,0) → dispatched",
		);
	});
});

// ── illegal moves rejected BEFORE dispatch (contract §4) ────────────────────

describe("createSaoleiMcpServer: illegal moves rejected before dispatch", () => {
	function setup(initial: GameState): {
		server: import("@modelcontextprotocol/sdk/server/mcp.js").McpServer;
		dispatched: CapturedPart[];
	} {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(initial);
		const server = createSaoleiMcpServer(bridge, fake.api);
		return { server, dispatched };
	}

	it("rejects a click on a revealed cell (cell_already_revealed) without dispatching", async () => {
		const { server, dispatched } = setup(board(["0 *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		// Only the init F2 dispatched; the illegal click did NOT.
		expect(dispatched).toHaveLength(1);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: cell_already_revealed");
		expect(text).toContain("board size 2*2");
		expect(text).toContain("valid range: x 0..1, y 0..1");
	});

	it("rejects a click on a flag (cell_is_flagged)", async () => {
		const { server, dispatched } = setup(board(["F *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain("rejected: cell_is_flagged");
	});

	it("rejects flagging a revealed cell (cannot_flag_revealed)", async () => {
		const { server, dispatched } = setup(board(["0 *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_flag", { x: 0, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain("rejected: cannot_flag_revealed");
	});

	it("rejects a chord on 0 / INITIAL / FLAG (chord_requires_number)", async () => {
		// chord on "0"
		const onZero = setup(board(["0 *", "* *"]));
		await callTool(onZero.server, "saolei_init", {});
		const r0 = await callTool(onZero.server, "saolei_chord_click", { x: 0, y: 0 });
		expect(r0.content[0].text).toContain("rejected: chord_requires_number");
		expect(onZero.dispatched).toHaveLength(1);

		// chord on INITIAL
		const onInitial = setup(board(["* *", "* *"]));
		await callTool(onInitial.server, "saolei_init", {});
		const rInit = await callTool(onInitial.server, "saolei_chord_click", { x: 0, y: 0 });
		expect(rInit.content[0].text).toContain("rejected: chord_requires_number");

		// chord on FLAG
		const onFlag = setup(board(["F *", "* *"]));
		await callTool(onFlag.server, "saolei_init", {});
		const rFlag = await callTool(onFlag.server, "saolei_chord_click", { x: 0, y: 0 });
		expect(rFlag.content[0].text).toContain("rejected: chord_requires_number");
	});

	it("rejects an out-of-bounds coordinate with the valid range (out_of_bounds)", async () => {
		const { server, dispatched } = setup(board(["* *", "* *"]));
		await callTool(server, "saolei_init", {});

		const result = await callTool(server, "saolei_click", { x: 5, y: 5 });

		expect(dispatched).toHaveLength(1);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: out_of_bounds");
		expect(text).toContain("valid range: x 0..1, y 0..1");
	});

	it("rejects any cell op before init (no_active_game, FR-015a)", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const { api } = makeFakeBoardApi(board(["*"]));
		const server = createSaoleiMcpServer(bridge, api);

		const result = await callTool(server, "saolei_click", { x: 0, y: 0 });

		// No dispatch at all — no game has been started.
		expect(dispatched).toHaveLength(0);
		const text = expectTextOnly(result);
		expect(text).toContain("rejected: no_active_game");
		expect(text).toContain("call saolei_init first");
	});

	it("rejects any cell op when the state is terminal (game_over, FR-015f)", async () => {
		const { server, dispatched } = setup(board(["X *", "* *"]));
		await callTool(server, "saolei_init", {});

		// (1,0) is INITIAL, but the board is terminal (HIT_MINE at (0,0)).
		const result = await callTool(server, "saolei_click", { x: 1, y: 0 });

		expect(dispatched).toHaveLength(1);
		expect(result.content[0].text).toContain("rejected: game_over");
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

		// State is invalid → cell op rejected as no_active_game (not dispatched).
		const clickResult = await callTool(server, "saolei_click", { x: 0, y: 0 });
		expect(dispatched).toHaveLength(1); // only the F2
		expect(clickResult.content[0].text).toContain("rejected: no_active_game");
	});

	it("a failed update (post-dispatch) invalidates the state; subsequent ops are rejected", async () => {
		const { bridge, dispatched } = makeFakeBridge();
		const fake = makeFakeBoardApi(board(["* *", "* *"]));
		const server = createSaoleiMcpServer(bridge, fake.api);

		await callTool(server, "saolei_init", {});
		// The legal click dispatches, but the post-action screenshot cannot be
		// recognized → state invalidated.
		fake.setUpdate("throw");

		const clickResult = await callTool(server, "saolei_click", { x: 0, y: 0 });
		expect(clickResult.content[0].text).toContain("unable to recognize board");
		// The click DID dispatch (recognition fails on the post-action frame).
		expect(dispatched).toHaveLength(2);

		// Subsequent op → no_active_game (state invalid).
		const next = await callTool(server, "saolei_click", { x: 1, y: 0 });
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
