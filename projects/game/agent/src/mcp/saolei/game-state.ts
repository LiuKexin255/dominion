/**
 * game-state.ts — Per-session minesweeper game state for the saolei MCP.
 *
 * One `GameState` instance lives alongside a session's `McpServer`
 * (`research.md` D3). All fields are in-memory only and reset by
 * `saolei_init` (FR-006 / FR-027). Data-model authority:
 * `specs/018-saolei-mcp/data-model.md` §1-3.
 */

/**
 * Cell status enumeration (FR-010 / data-model.md §2). Wire representation
 * on the MCP tool boundary is the string union sent by `saolei_update`; this
 * in-memory enum is internal.
 */
export const CellStatus = {
	INITIAL: "INITIAL",
	NUMBER_0: "0",
	NUMBER_1: "1",
	NUMBER_2: "2",
	NUMBER_3: "3",
	NUMBER_4: "4",
	NUMBER_5: "5",
	NUMBER_6: "6",
	NUMBER_7: "7",
	NUMBER_8: "8",
	FLAG: "FLAG",
	HIT_MINE: "HIT_MINE",
	MINE: "MINE",
} as const;

export type CellStatus = (typeof CellStatus)[keyof typeof CellStatus];

/** All numeric (revealed) cell statuses 0..8 — used by validation/connectivity. */
export const NUMBER_STATUSES: ReadonlySet<CellStatus> = new Set([
	CellStatus.NUMBER_0,
	CellStatus.NUMBER_1,
	CellStatus.NUMBER_2,
	CellStatus.NUMBER_3,
	CellStatus.NUMBER_4,
	CellStatus.NUMBER_5,
	CellStatus.NUMBER_6,
	CellStatus.NUMBER_7,
	CellStatus.NUMBER_8,
]);

/**
 * The operation awaiting `saolei_update` (data-model.md §3). `kind` drives
 * which validator runs on the next update batch. `saolei_init` does NOT set
 * a LastOp (it is exempt from the operate-then-update alternation).
 */
export interface LastOp {
	kind: "click" | "flag" | "chord_click";
	target: { x: number; y: number };
}

/**
 * Per-session minesweeper board model (data-model.md §1).
 *
 * `grid` is indexed `grid[y][x]` (y = row, x = col). `pendingUpdate` enforces
 * the operate-then-update alternation (FR-011). `lastOp` carries the kind
 * and target cell so the next `saolei_update` validates against the right
 * rule set (FR-013/014/015).
 */
export interface GameState {
	grid: CellStatus[][];
	width: number;
	height: number;
	pendingUpdate: boolean;
	lastOp: LastOp | null;
	initialized: boolean;
}

/**
 * Build a fresh `width`×`height` grid initialised entirely to INITIAL.
 * `width` = column count (x ∈ [0,width)); `height` = row count (y ∈ [0,height)).
 */
export function createGameState(width: number, height: number): GameState {
	const grid: CellStatus[][] = [];
	for (let y = 0; y < height; y++) {
		const row: CellStatus[] = [];
		for (let x = 0; x < width; x++) {
			row.push(CellStatus.INITIAL);
		}
		grid.push(row);
	}
	return {
		grid,
		width,
		height,
		pendingUpdate: false,
		lastOp: null,
		initialized: true,
	};
}
