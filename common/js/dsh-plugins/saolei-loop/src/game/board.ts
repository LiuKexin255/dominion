/**
 * Board rules and the recognition seam behind the GameRuntime
 * (specs/051-agent-v2-dsh-migration/data-model.md §2.5): the strict
 * pre-dispatch validation table, the SKIP/STOP batch-triage reason sets, the
 * loss-first counter-informed status derivation, and the per-game statistics.
 * The semantics follow spec 051 A6 (recognition/validation/win rules) with
 * this module as their saolei-loop carrying implementation.
 */

import { SaoleiBoard, isWin } from "@dominion/game-saolei-board";
import type { CellStatus, GameState } from "@dominion/game-saolei-board";

/**
 * Recognition engine seam. The default implementation wraps
 * `@dominion/game-saolei-board`'s stateful `SaoleiBoard` (monotonic
 * cross-screenshot validation). Tests inject a fake whose `init`/`update`
 * return canned `GameState`s or throw to simulate a recognition failure —
 * no cross-package `vi.mock` (style/javascript.md Mock convention).
 *
 * `init` recognizes the FIRST screenshot of a game; `update` recognizes a
 * subsequent screenshot of the SAME game (throws
 * `BoardStateIncompatibleError`/`BoardDimensionMismatchError` on a
 * dimension change or non-monotonic regression).
 */
export interface SaoleiBoardApi {
  init(png: Buffer): GameState;
  update(png: Buffer): GameState;
}

/** Build the default recognition engine wrapping `SaoleiBoard`. A fresh
 * `SaoleiBoard` is created on each `init` (new game / re-seed). */
export function createDefaultBoardApi(): SaoleiBoardApi {
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

/** The kind of a single cell operation: `click` (left-click reveal), `flag`
 * (right-click toggle), `chord` (simultaneous left+right press). */
export type OperationType = "click" | "flag" | "chord";

/**
 * Verdict of a strict pre-dispatch validation pass. `ok: false` carries a
 * stable reason code surfaced verbatim to the model.
 */
export type MoveVerdict = { ok: true } | { ok: false; reason: MoveRejection };

/**
 * Stable reason codes for a rejected move (the spec 051 A6 rule table).
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
 * Rejection reasons that are harmless no-ops — the operation would NOT change
 * the board. A batch SKIPS these and continues (data-model.md §2.5 SKIP set).
 */
export const SKIP_REASONS: ReadonlySet<MoveRejection> = new Set<MoveRejection>([
  "cell_already_revealed",
  "cell_is_flagged",
  "cannot_flag_revealed",
  "chord_requires_number",
  "chord_no_unrevealed_neighbor",
]);

/**
 * Rejection reasons that stop a batch: structural/contextual rejections plus
 * terminal game end. Remaining ops are NOT executed (data-model.md §2.5 STOP
 * set).
 */
export const STOP_REASONS: ReadonlySet<MoveRejection> = new Set<MoveRejection>([
  "out_of_bounds",
  "no_active_game",
  "game_over",
  "game_won",
]);

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
 * Terminal-LOSS indicators: `HIT_MINE` (the triggered mine) and `MINE` (an
 * end-game revealed mine) only appear once the game is lost. A win does not
 * expose mines as `MINE`/`HIT_MINE` (they are auto-flagged as `F`), so their
 * presence is a definitive terminal-LOSS signal; a terminal WIN is detected
 * separately via `isWin` (loss takes precedence).
 */
const TERMINAL_CELLS: ReadonlySet<CellStatus> = new Set<CellStatus>([
  "HIT_MINE",
  "MINE",
]);

/** The 8 Moore-neighbor offsets (row-major, top row first). */
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
 * Whether a recognized state is a terminal LOSS (any revealed mine). Loss and
 * win are mutually exclusive terminal statuses.
 */
export function isTerminalState(state: GameState): boolean {
  for (const row of state.grid) {
    for (const cell of row) {
      if (TERMINAL_CELLS.has(cell)) return true;
    }
  }
  return false;
}

/** The game-status token emitted in every tool-result body. */
export type GameStatus = "won" | "lost" | "playing";

/**
 * Derive the game status from a recognized state, loss-first. `isWin` is
 * counter-informed: it returns `true` only when the grid is fully
 * revealed/flagged AND `state.mineCounter` reads exactly `000`. Pure
 * function of `state`.
 */
export function gameStatus(state: GameState): GameStatus {
  if (isTerminalState(state)) return "lost";
  if (isWin(state)) return "won";
  return "playing";
}

/**
 * Per-game quantitative statistics, computed first-hand at game end and
 * carried by the terminal game event (the spec 051 A6 stats contract; the
 * per-type breakdown is the spec 065 game-stats entity,
 * specs/065-agent-v2-team-refine/data-model.md §1.3).
 */
export interface GameStats {
  /** Successful cell-operation dispatch count this game (init/remain and
   * rejected/skipped ops excluded). */
  operationCount: number;
  /** Successful dispatches per operation type; the parts always sum to
   * {@link operationCount}. */
  operationsByType: Record<OperationType, number>;
  /** Correctly flagged mines; null = init mineCounter undecodable. */
  correctFlags: number | null;
  /** operationCount / correctFlags, rounded to 2 decimals; "N/A" when
   * correctFlags is 0 or null. */
  avgOpsPerMine: number | "N/A";
}

/**
 * Compute the per-game statistics at game end. `operationsByType` is the
 * runtime's per-type counter for the finished game (same success-dispatch
 * accounting as `operationCount`,
 * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §1).
 * correctFlags = totalMines − terminal MINE cells − HIT_MINE cells;
 * totalMines comes from `initState.mineCounter` (flags = 0 at game start, so
 * the counter reads the mine total). An undecodable counter ⇒ correctFlags =
 * null; correctFlags = 0/null ⇒ avgOpsPerMine = "N/A" (no NaN/Infinity on an
 * instant loss). Pure function.
 */
export function computeGameStats(
  initState: GameState | null,
  finalState: GameState,
  operationCount: number,
  operationsByType: Record<OperationType, number>,
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

  return {
    operationCount,
    operationsByType: { ...operationsByType },
    correctFlags,
    avgOpsPerMine,
  };
}

/** In-bounds Moore neighbors of `(x, y)`; `GameState.grid` is indexed
 * `[y][x]`. */
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
 * `UNKNOWN` — a chord acts only on unrevealed cells and is lenient on
 * `UNKNOWN` (an uncertain neighbor is treated as possibly unrevealed).
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
 * Strict pre-dispatch validation. Judges **target-cell compatibility** —
 * never predicted outcome (a chord whose adjacent-flag count ≠ the number is
 * still legal). Check order: structural (out-of-bounds) → state-level
 * (terminal loss `game_over`, then terminal win `game_won`) → cell-specific
 * per-type rule. `UNKNOWN` targets are always lenient. `no_active_game` is
 * NOT produced here — it is the runtime-level check for a missing recognized
 * state.
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
  // Never reject solely on recognition uncertainty.
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
      return { ok: true };
    case "flag":
      if (REVEALED_NUMBERS.has(cell)) {
        return { ok: false, reason: "cannot_flag_revealed" };
      }
      return { ok: true };
    case "chord":
      if (!CHORD_NUMBERS.has(cell)) {
        return { ok: false, reason: "chord_requires_number" };
      }
      if (!hasInitialOrUnknownNeighbor(state, x, y)) {
        return { ok: false, reason: "chord_no_unrevealed_neighbor" };
      }
      return { ok: true };
  }
}

/**
 * Token resolver for the read-only remain grid: each revealed number cell
 * (`1`–`8`) carries its raw `number − adjacent FLAG count` (may be 0 or
 * negative); every other cell is `-`. Consumed by `renderGridWithRuler` so
 * the remain grid's ruler matches the board grid's exactly.
 */
export function remainTokenAt(
  state: GameState,
): (x: number, y: number) => string {
  return (x: number, y: number): string => {
    const cell = state.grid[y][x];
    if (CHORD_NUMBERS.has(cell)) {
      const flagCount = neighbors(state, x, y).filter(
        (s) => s === "FLAG",
      ).length;
      return String(Number(cell) - flagCount);
    }
    return "-";
  };
}
