/**
 * render.ts — Render a `GameState` as the compact text board the saolei MCP
 * returns to the model. Format (coordinate ruler around the symbol grid):
 *
 *   board size <w>*<h>
 *   <blank>
 *   <column-index header row: "" col0 col1 ... col<w-1>>
 *   <data row 0: row0 <sym(0,0)> ... <sym(w-1,0)>>
 *   ...
 *
 * The tagged (`col<N>`/`row<N>`, 0-based) ruler and the right-aligned grid
 * layout are produced by `renderGridWithRuler` — the single source shared with
 * the remain grid. See
 * specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §1/§2.
 *
 * Symbols: `*`=INITIAL, `0`..`8`=numbers, `F`=FLAG, `X`=HIT_MINE (triggered
 * mine), `M`=MINE (end-game revealed mine), `?`=UNKNOWN (below threshold —
 * flagged for calibration).
 */

import type { CellStatus, GameState } from "./types";

const SYMBOLS: Record<CellStatus, string> = {
  INITIAL: "*",
  "0": "0",
  "1": "1",
  "2": "2",
  "3": "3",
  "4": "4",
  "5": "5",
  "6": "6",
  "7": "7",
  "8": "8",
  FLAG: "F",
  HIT_MINE: "X",
  MINE: "M",
  UNKNOWN: "?",
};

/** Render a single cell status to its board symbol. */
export function cellSymbol(status: CellStatus): string {
  return SYMBOLS[status] ?? "?";
}

/**
 * Render a width×height grid with a tagged coordinate ruler.
 *
 * Produces `height + 1` lines: a column-index header row (`""` + `col0..col<w-1>`)
 * followed by one data row per `y` (`row<y>` + `tokenAt(0,y)..tokenAt(w-1,y)`).
 * Every slot is right-aligned to `columnWidth` and slots are joined by single
 * spaces; rows are joined by `\n`. Pure: a function of `width`, `height`, and
 * `tokenAt` only — no I/O, no mutation, no side effects.
 *
 * `columnWidth = max(1, len("col"+maxIndex), len("row"+maxIndex), maxTokenWidth)`
 * where `maxIndex = max(width-1, height-1)` and `maxTokenWidth` is the longest
 * token returned by `tokenAt` (0 when the grid is empty). Index labels drive
 * the width (4 for a ≤9-index board, 5 for a 10+ index board).
 *
 * Contract: specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §2.
 */
export function renderGridWithRuler(
  width: number,
  height: number,
  tokenAt: (x: number, y: number) => string,
): string {
  const maxIndex = Math.max(width - 1, height - 1);
  const indexLabelWidth = Math.max(
    `col${maxIndex}`.length,
    `row${maxIndex}`.length,
  );

  const tokens: string[][] = [];
  let maxTokenWidth = 0;
  for (let y = 0; y < height; y++) {
    const row: string[] = [];
    for (let x = 0; x < width; x++) {
      const token = tokenAt(x, y);
      if (token.length > maxTokenWidth) maxTokenWidth = token.length;
      row.push(token);
    }
    tokens.push(row);
  }

  const columnWidth = Math.max(1, indexLabelWidth, maxTokenWidth);
  const align = (slot: string): string => slot.padStart(columnWidth, " ");

  const lines: string[] = [];

  const headerSlots: string[] = [""];
  for (let x = 0; x < width; x++) {
    headerSlots.push(`col${x}`);
  }
  lines.push(headerSlots.map(align).join(" "));

  for (let y = 0; y < height; y++) {
    const slots: string[] = [`row${y}`];
    for (let x = 0; x < width; x++) {
      slots.push(tokens[y][x]);
    }
    lines.push(slots.map(align).join(" "));
  }

  return lines.join("\n");
}

/**
 * Render the full board state as a text board string: the `board size <w>*<h>`
 * header, a blank separator, then the ruled symbol grid (each cell mapped to
 * its legend symbol via `cellSymbol`).
 *
 * Contract: specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §2
 * (Relationship to `renderBoardText`).
 */
export function renderBoardText(state: GameState): string {
  return (
    `board size ${state.width}*${state.height}\n\n` +
    renderGridWithRuler(state.width, state.height, (x, y) =>
      cellSymbol(state.grid[y]?.[x] ?? "UNKNOWN"),
    )
  );
}
