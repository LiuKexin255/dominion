/**
 * validate.ts — Same-game state compatibility checks for `SaoleiBoard.update`.
 *
 * The check is **monotonic, not linear**: an update may skip any number of
 * intermediate game steps. The only invariant is that a revealed cell (number
 * 0..8, or a mine) is permanent — it never reverts to INITIAL/FLAG or changes
 * its number. Flagging toggles INITIAL↔FLAG; an unopened cell may become any
 * revealed state in one update regardless of how many clicks happened in
 * between. So INITIAL→"3" (cross-step) is allowed; "3"→INITIAL or "3"→"4" is
 * not. The purpose is to catch misuse (e.g. a restarted game whose state was
 * not cleared — its revealed cells revert to unopened), not to enforce strict
 * step-by-step legality.
 *
 * `UNKNOWN` (a below-threshold recognition result) is treated permissively:
 * it must never block an otherwise-valid update, since it is a calibration
 * artefact, not evidence of a different game.
 */

import type { CellStatus, GameState } from "./types";

/** Revealed cell statuses that are permanent within a game. */
const REVEALED: ReadonlySet<CellStatus> = new Set<CellStatus>([
  "0",
  "1",
  "2",
  "3",
  "4",
  "5",
  "6",
  "7",
  "8",
  "HIT_MINE",
  "MINE",
]);

/** Outcome of a compatibility check. */
export type Compatibility =
  | { ok: true }
  | { ok: false; kind: "dimension" | "state"; reason: string };

/** Thrown by `updateFromScreenshot` when the board size changed. */
export class BoardDimensionMismatchError extends Error {
  readonly kind = "dimension" as const;
  constructor(
    readonly prev: { width: number; height: number },
    readonly next: { width: number; height: number },
  ) {
    super(
      `board size changed: ${prev.width}x${prev.height} -> ${next.width}x${next.height}; re-initialize with SaoleiBoard.init`,
    );
    this.name = "BoardDimensionMismatchError";
  }
}

/** Thrown by `updateFromScreenshot` when a cell transition is game-incompatible. */
export class BoardStateIncompatibleError extends Error {
  readonly kind = "state" as const;
  constructor(reason: string) {
    super(`incompatible state transition (${reason}); re-initialize with SaoleiBoard.init for a new game`);
    this.name = "BoardStateIncompatibleError";
  }
}

/**
 * Whether `next` is a legal same-game successor of `prev`. Checks dimensions
 * first, then each cell transition. Pure — usable in isolation from
 * recognition (style/javascript.md §测试 — DI seam).
 */
export function checkCompatible(
  prev: GameState,
  next: GameState,
): Compatibility {
  if (prev.width !== next.width || prev.height !== next.height) {
    return {
      ok: false,
      kind: "dimension",
      reason: `board size ${prev.width}x${prev.height} -> ${next.width}x${next.height}`,
    };
  }
  for (let y = 0; y < prev.height; y++) {
    for (let x = 0; x < prev.width; x++) {
      const a = prev.grid[y][x];
      const b = next.grid[y][x];
      const cell = compatibleCell(x, y, a, b);
      if (!cell.ok) return cell;
    }
  }
  return { ok: true };
}

/** Whether a single cell may transition `a -> b` within one game. */
function compatibleCell(
  x: number,
  y: number,
  a: CellStatus,
  b: CellStatus,
): Compatibility {
  if (a === b) return { ok: true };
  if (a === "UNKNOWN" || b === "UNKNOWN") return { ok: true };
  if (REVEALED.has(a)) {
    return {
      ok: false,
      kind: "state",
      reason: `cell (${x},${y}): ${a} -> ${b} (revealed cells are permanent)`,
    };
  }
  return { ok: true };
}
