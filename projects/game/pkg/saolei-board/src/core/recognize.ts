/**
 * recognize.ts — Board-level orchestration: decode a screenshot, segment it
 * into cells at the fixed geometry, classify each cell, AND decode the
 * top-left mine counter (via `counter.ts`), assembling a `GameState` whose
 * `mineCounter` field carries the decoded counter value. Also exposes the
 * stateful `SaoleiBoard` helper that maintains the current state and refreshes
 * it (grid + counter) from each new screenshot.
 */

import { classifyCell, DEFAULT_COLOR_PROFILE } from "./classify";
import { DEFAULT_COUNTER_PROFILE, decodeMineCounter } from "./counter";
import { decodePng, extractCellRegion } from "./decode";
import { cellOrigin, detectBoardSize, resolveGeometry } from "./geometry";
import { renderBoardText } from "./render";
import type {
  CellStatus,
  ColorProfile,
  CounterProfile,
  GameState,
  RecognizeOptions,
} from "./types";
import {
  BoardDimensionMismatchError,
  BoardStateIncompatibleError,
  checkCompatible,
} from "./validate";

/** Internal result of recognizing a board, with optional per-cell diagnostics. */
export interface RecognizeResult {
  state: GameState;
  /** Present only when `collectDiagnostics` is set on the options. */
  diagnostics?: CellDiagInternal[][];
}

/** Per-cell internal diagnostics (mirrors classify.ts with grid coords). */
export interface CellDiagInternal {
  x: number;
  y: number;
  status: CellStatus;
  centerMean: { r: number; g: number; b: number };
  beveled: boolean;
  glyphPixels: number;
  blackPixels: number;
  redPixels: number;
  winnerRef: string | null;
  bgRedness: number;
}

/** Options for `recognizeBoard`/`SaoleiBoard` extended with a debug flag. */
export interface RecognizeBoardOptions extends RecognizeOptions {
  /** Collect per-cell diagnostics (CLI `--debug`). Default false. */
  collectDiagnostics?: boolean;
}

/**
 * Recognize the full board from PNG bytes. Board dimensions auto-detect from
 * the screenshot size unless `width`/`height` are supplied. Pure: returns a new
 * `GameState` each call (does not mutate caller state).
 */
export function recognizeBoard(
  png: Buffer | Uint8Array,
  options: RecognizeBoardOptions = {},
): RecognizeResult {
  const geometry = resolveGeometry(options.geometry);
  const profile = options.profile ?? DEFAULT_COLOR_PROFILE;
  const counterProfile = options.counterProfile ?? DEFAULT_COUNTER_PROFILE;
  const img = decodePng(png);

  const { width, height } =
    options.width && options.height
      ? { width: options.width, height: options.height }
      : detectBoardSize(img.width, img.height, geometry);

  const grid: CellStatus[][] = [];
  let diagnostics: CellDiagInternal[][] | undefined = options.collectDiagnostics
    ? []
    : undefined;

  for (let y = 0; y < height; y++) {
    const row: CellStatus[] = [];
    const diagRow: CellDiagInternal[] | undefined = options.collectDiagnostics
      ? []
      : undefined;
    for (let x = 0; x < width; x++) {
      const origin = cellOrigin(x, y, geometry);
      const region = extractCellRegion(
        img,
        origin.x,
        origin.y,
        geometry.cellSizePx,
      );
      const { status, diagnostics: d } = classifyCell(
        region.pixels,
        region.width,
        region.height,
        profile,
      );
      row.push(status);
      if (diagRow && d) {
        diagRow.push({
          x,
          y,
          status,
          centerMean: d.centerMean,
          beveled: d.beveled,
          glyphPixels: d.glyphPixels,
          blackPixels: d.blackPixels,
          redPixels: d.redPixels,
          winnerRef: d.winnerRef,
          bgRedness: d.bgRedness,
        });
      }
    }
    grid.push(row);
    if (diagnostics && diagRow) diagnostics.push(diagRow);
  }

  // Decode the top-left mine counter in the same pass over the decoded image
  // (FR-001/FR-002). The counter is non-monotonic within a game (flags are
  // placed/removed), so it is re-decoded on every screenshot and is NOT part
  // of `checkCompatible`.
  const mineCounter = decodeMineCounter(img, counterProfile);

  return {
    state: { width, height, grid, mineCounter },
    diagnostics,
  };
}

/**
 * Stateful board view: maintains the current `GameState` and refreshes it from
 * each new screenshot within the SAME game.
 *
 * Construction is via the static `init` factory, which recognizes the first
 * screenshot and fixes the board dimensions + recognition profile. Subsequent
 * screenshots of the same game go through `updateFromScreenshot`, which
 * validates that the new screenshot yields the same board size and that every
 * cell transition is legal within one game (revealed cells are permanent). A
 * screenshot of a DIFFERENT game violates these — the update throws and the
 * caller MUST re-initialize via `SaoleiBoard.init`.
 */
export class SaoleiBoard {
  private readonly width: number;
  private readonly height: number;
  private readonly geometry: ReturnType<typeof resolveGeometry>;
  private readonly profile: ColorProfile;
  private readonly counterProfile: CounterProfile;
  private current: GameState;

  private constructor(
    width: number,
    height: number,
    geometry: ReturnType<typeof resolveGeometry>,
    profile: ColorProfile,
    counterProfile: CounterProfile,
    current: GameState,
  ) {
    this.width = width;
    this.height = height;
    this.geometry = geometry;
    this.profile = profile;
    this.counterProfile = counterProfile;
    this.current = current;
  }

  /**
   * Initialize a board from a screenshot — the explicit entry point for the
   * first screenshot of a game or any new game. Recognizes the board
   * (auto-detecting dimensions from the screenshot size unless `width`/
   * `height` are supplied) and fixes the geometry/profile/counter-profile used
   * for all subsequent updates. Use this (not `updateFromScreenshot`) whenever
   * the screenshot may belong to a different game than the board's current one.
   */
  static init(
    png: Buffer | Uint8Array,
    options: RecognizeOptions = {},
  ): SaoleiBoard {
    const geometry = resolveGeometry(options.geometry);
    const profile = options.profile ?? DEFAULT_COLOR_PROFILE;
    const counterProfile = options.counterProfile ?? DEFAULT_COUNTER_PROFILE;
    const { state } = recognizeBoard(png, {
      geometry,
      profile,
      counterProfile,
      width: options.width,
      height: options.height,
    });
    return new SaoleiBoard(
      state.width,
      state.height,
      geometry,
      profile,
      counterProfile,
      state,
    );
  }

  /** The current recognized state (a snapshot; mutating it does not affect the
   * board's internal state). */
  get state(): GameState {
    return this.current;
  }

  /** The fixed board dimensions set at initialization. */
  get dimensions(): { width: number; height: number } {
    return { width: this.width, height: this.height };
  }

  /**
   * Re-recognize the board from a new screenshot of the SAME game and store +
   * return the result. Validates that the screenshot yields the same board
   * dimensions as initialization and that every cell transition is legal within
   * one game (a revealed cell cannot revert). On mismatch throws
   * `BoardDimensionMismatchError` or `BoardStateIncompatibleError` — the caller
   * MUST re-initialize via `SaoleiBoard.init` for a different game. The current
   * state is left unchanged on any rejection.
   */
  updateFromScreenshot(png: Buffer | Uint8Array): GameState {
    const { state: next } = recognizeBoard(png, {
      geometry: this.geometry,
      profile: this.profile,
      counterProfile: this.counterProfile,
    });
    const compat = checkCompatible(this.current, next);
    if (!compat.ok) {
      if (compat.kind === "dimension") {
        throw new BoardDimensionMismatchError(
          { width: this.width, height: this.height },
          { width: next.width, height: next.height },
        );
      }
      throw new BoardStateIncompatibleError(compat.reason);
    }
    this.current = next;
    return this.current;
  }

  /** Render the current state as a text board (`renderBoardText`). */
  renderText(): string {
    return renderBoardText(this.current);
  }
}
