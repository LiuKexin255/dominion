/**
 * render.ts — Render a `GameState` as the compact text board the saolei MCP
 * returns to the model. Format (one symbol per cell, space-separated):
 *
 *   board size <w>*<h>
 *   <blank>
 *   <row 0>
 *   <row 1>
 *   ...
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

/** Render the full board state as a text board string. */
export function renderBoardText(state: GameState): string {
  const lines: string[] = [];
  lines.push(`board size ${state.width}*${state.height}`);
  lines.push("");
  for (let y = 0; y < state.height; y++) {
    const row = state.grid[y] ?? [];
    const symbols: string[] = [];
    for (let x = 0; x < state.width; x++) {
      symbols.push(cellSymbol(row[x] ?? "UNKNOWN"));
    }
    lines.push(symbols.join(" "));
  }
  return lines.join("\n");
}
