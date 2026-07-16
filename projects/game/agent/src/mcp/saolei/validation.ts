/**
 * @fileoverview Saolei legality validation — the foundational rule set
 * (FR-016..023, data-model.md §8). Each validator returns either `null` when
 * the operation/update is legal, or a machine-readable `{ reason }` when it
 * violates a rule. Tools translate `{ reason }` into a `SaoleiToolResult` with
 * `status: "rejected"` (D-5); rejected operations never reach the desktop.
 *
 * The rule set is intentionally non-exhaustive (FR-024); e.g. chord
 * flag-count match is deferred to follow-up work.
 */

import { NUMBER_CELL_STATES } from "./board";
import type { BoardState, CellState, CellUpdate } from "./board";
import { inBounds } from "./geometry";

/** A machine-readable rejection reason (data-model.md §7-8). */
export interface Rejection {
  reason: string;
}

/** Convenience: a validation result is `null` (legal) or a `Rejection`. */
export type ValidationResult = Rejection | null;

/**
 * Validate a positional operation (click/flag/double_click) at `(x, y)` BEFORE
 * dispatch. Encodes FR-016 (pre-init), FR-017 (awaiting-update block), FR-021
 * (out-of-bounds), and FR-023 (terminal-after-boom).
 */
export function validatePositionalOperation(
  board: BoardState,
  x: number,
  y: number,
): ValidationResult {
  if (board.lifecycle === "uninitialized") {
    return { reason: "not-initialized" };
  }
  if (board.lifecycle === "terminal") {
    return { reason: "terminal" };
  }
  if (board.lifecycle === "awaiting-update") {
    return { reason: "awaiting-update" };
  }
  if (!isInBoardBounds(board, x, y)) {
    return { reason: "out-of-bounds" };
  }
  return null;
}

/**
 * FR-018: `saolei_click` is valid only on a `block` cell.
 */
export function validateClickTarget(
  board: BoardState,
  x: number,
  y: number,
): ValidationResult {
  if (board.cellAt(x, y) !== "block") {
    return { reason: "cell-not-block" };
  }
  return null;
}

/**
 * `saolei_flag` toggles a flag on a covered cell. Valid only on a `block` or
 * `flag` cell (data-model.md: flag targets only `block` or `flag` cells).
 */
export function validateFlagTarget(
  board: BoardState,
  x: number,
  y: number,
): ValidationResult {
  const cell = board.cellAt(x, y);
  if (cell !== "block" && cell !== "flag") {
    return { reason: "cell-not-block-and-not-flag" };
  }
  return null;
}

/**
 * FR-019: `saolei_double_click` (chord) is valid only on a revealed number cell.
 */
export function validateChordTarget(
  board: BoardState,
  x: number,
  y: number,
): ValidationResult {
  if (!isNumberCell(board.cellAt(x, y))) {
    return { reason: "cell-not-number" };
  }
  return null;
}

/**
 * Validate a `saolei_update` batch (FR-022, SC-007). The batch is applied
 * atomically: if ANY cell transition is illegal OR any coordinate is out of
 * bounds, the whole batch is rejected and no state changes. Rejections carry
 * the offending coordinates in the reason.
 *
 * Rules enforced:
 * - board MUST be `awaiting-update` (otherwise `not-awaiting-update`).
 * - every coordinate MUST be in board bounds (FR-021).
 * - terminal cells (`zero`..`eight`, `boom`) MUST NOT transition away
 *   (data-model.md §1 invariant; FR-022).
 * - only a `flag` may appear/clear on a `flag`↔`block` toggle, and only via the
 *   flag path: a `flag`→`block` clear and a `block`→`flag` place are legal;
 *   transitions involving a `flag` against a non-`block`/non-`flag` prior are
 *   rejected as `illegal-flag-transition` (FR-020).
 */
export function validateUpdateBatch(
  board: BoardState,
  cells: readonly CellUpdate[],
): ValidationResult {
  if (board.lifecycle !== "awaiting-update") {
    return { reason: "not-awaiting-update" };
  }

  const offenders: string[] = [];
  for (const cell of cells) {
    if (!isInBoardBounds(board, cell.x, cell.y)) {
      return {
        reason: `out-of-bounds: (${cell.x},${cell.y})`,
      };
    }
    const prior = board.cellAt(cell.x, cell.y);
    const verdict = classifyCellTransition(prior, cell.state);
    if (verdict !== null) {
      offenders.push(`(${cell.x},${cell.y}) ${verdict}`);
    }
  }

  if (offenders.length > 0) {
    return { reason: `illegal-transition: ${offenders.join("; ")}` };
  }
  return null;
}

/**
 * Classify a single proposed `prior → next` cell transition. Returns `null`
 * when legal, or a short tag describing why it is illegal.
 *
 * Legal transitions:
 * - `block → flag` and `flag → block` (the flag toggle, FR-020).
 * - `block → number|boom` (a reveal).
 * - any cell to itself (idempotent observation, e.g. re-reporting a number).
 * Illegal transitions:
 * - a revealed number or boom transitioning to anything but itself (terminal
 *   cells, data-model.md §1).
 * - `flag → number|boom` (a flag cannot reveal; FR-020 — only the flag toggle
 *   is legal from a flag).
 * - `block/number/boom → flag` is illegal except `block → flag` (only the flag
 *   tool produces flags, and a flag targets only block/flag).
 */
function classifyCellTransition(
  prior: CellState,
  next: CellState,
): string | null {
  if (prior === next) {
    return null;
  }

  switch (prior) {
    case "block":
      // `block` is the only coverable, non-terminal state, so any transition
      // away from it is a legal reveal-or-flag result. `next` is already
      // constrained to a valid CellState by the zod enum in saolei_update, so
      // `block → flag` (place) and `block → number|boom` (reveal) are both
      // legal here; no further allow-listing is needed.
      return null;
    case "flag":
      // flag → block (clear) is legal; anything else is an illegal flag
      // transition (a flag cannot reveal or become a number).
      return next === "block" ? null : "illegal-flag-transition";
    default:
      // prior is a number or boom — terminal, never transitions away.
      return "terminal-cell-locked";
  }
}

function isInBoardBounds(board: BoardState, x: number, y: number): boolean {
  // Delegate to the single source of truth for the bounds check
  // (geometry.ts `inBounds`, data-model.md §4 / FR-021) instead of duplicating
  // the `0 <= x < width && 0 <= y < height` expression here.
  return inBounds(x, y, board.width, board.height);
}

function isNumberCell(cell: CellState): boolean {
  return (NUMBER_CELL_STATES as readonly string[]).includes(cell);
}
