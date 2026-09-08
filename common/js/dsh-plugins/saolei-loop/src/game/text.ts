/**
 * Model-visible result-text builders for the GameRuntime, migrated verbatim
 * from v1 projects/game/agent/src/mcp/saolei/saolei-mcp.ts (spec A6/FR-013:
 * the text contract is unchanged — three-layer body: outcome line, `game
 * status:` line, ruler board; `valid range:` on rejections; rejections are
 * NORMAL results while desktop/bridge failures are error outcomes).
 */

import { renderBoardText, renderGridWithRuler } from "@dominion/game-saolei-board";
import type { GameState } from "@dominion/game-saolei-board";

import { gameStatus, remainTokenAt } from "./board.js";
import type { MoveRejection, OperationType } from "./board.js";

/** Outcome line for `saolei_init` success. */
export const INIT_OUTCOME = "new game started";

/** Outcome line prefix for a rejected move. */
export const REJECT_PREFIX = "rejected:";

/** Outcome line for a recognition failure. */
export const UNRECOGNIZABLE_OUTCOME = "unable to recognize board";

/** Outcome line for the read-only `saolei_remain` query. */
export const REMAIN_OUTCOME = "saolei_remain → computed";

/**
 * Build the `saolei_init` success body: outcome + game-status line + initial
 * text board.
 */
export function initSuccessText(state: GameState): string {
  return `${INIT_OUTCOME}\ngame status: ${gameStatus(state)}\n\n${renderBoardText(state)}`;
}

/** The triggering operation of a batch stop (`stopped at {type}({x},{y})`). */
export type StoppedOp = { type: OperationType; x: number; y: number };

/**
 * Build the `saolei_operate` result body: outcome line + game-status line +
 * the final text board. The outcome line reflects the batch triage: a normal
 * completion (`executed N ops`, `skipped S no-op ops` when any were skipped)
 * or a stop at the triggering op with its parameters and reason
 * (`stopped at {type}({x},{y}) ({reason})`).
 */
export function operateResultText(
  executed: number,
  skipped: number,
  state: GameState,
  stoppedOp: StoppedOp | null,
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
 * current text board + the valid coordinate range. When no state exists
 * (`no_active_game`), substitutes guidance to call `saolei_init` first and
 * OMITS the status line — no fabricated status.
 */
export function rejectionText(
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
 * Build the recognition-failure body + re-init guidance. No status line — a
 * recognition failure invalidates the state, so there is no recognized board
 * to derive a status from.
 */
export function unrecognizableText(): string {
  return `${UNRECOGNIZABLE_OUTCOME}\n\ncall saolei_init to start a new game.`;
}

/**
 * Build the `saolei_remain` body: outcome + game-status line + the remain
 * grid rendered with the shared coordinate ruler.
 */
export function remainText(state: GameState): string {
  return (
    `${REMAIN_OUTCOME}\n` +
    `game status: ${gameStatus(state)}\n\n` +
    `board size ${state.width}*${state.height}\n\n` +
    renderGridWithRuler(state.width, state.height, remainTokenAt(state))
  );
}
