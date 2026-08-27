/**
 * index.ts — Public API barrel for @dominion/game-saolei-board.
 *
 * Saolei (minesweeper) board recognition: decode a desktop screenshot PNG and
 * produce a compact text board state for the saolei MCP to return to the model
 * (replacing the raw image). See `specs/018-saolei-mcp/` for the MCP contract.
 */

export type {
  CellStatus,
  GameState,
  MineCounter,
  SegmentId,
  CounterProfile,
  RGB,
  BoardGeometry,
  ColorProfile,
  CellDiagnostics,
  RecognizeOptions,
} from "./types.js";

export {
  DEFAULT_GEOMETRY,
  resolveGeometry,
  detectBoardSize,
  cellOrigin,
} from "./geometry.js";

export { decodePng, getRGB, pixelIndex, extractCellRegion } from "./decode.js";
export type { DecodedImage } from "./decode.js";

export { classifyCell, DEFAULT_COLOR_PROFILE } from "./classify.js";
export type { CellClassifyDiagnostics } from "./classify.js";

export { decodeMineCounter, DEFAULT_COUNTER_PROFILE } from "./counter.js";

export { recognizeBoard, SaoleiBoard } from "./recognize.js";
export type {
  RecognizeResult,
  RecognizeBoardOptions,
  CellDiagInternal,
} from "./recognize.js";

export { renderBoardText, renderGridWithRuler, cellSymbol } from "./render.js";

export {
  checkCompatible,
  BoardDimensionMismatchError,
  BoardStateIncompatibleError,
} from "./validate.js";
export type { Compatibility } from "./validate.js";

export { isWin } from "./win.js";
