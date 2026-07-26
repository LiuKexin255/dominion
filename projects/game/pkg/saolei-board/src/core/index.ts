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
  RGB,
  BoardGeometry,
  ColorProfile,
  CellDiagnostics,
  RecognizeOptions,
} from "./types";

export {
  DEFAULT_GEOMETRY,
  resolveGeometry,
  detectBoardSize,
  cellOrigin,
} from "./geometry";

export { decodePng, getRGB, pixelIndex, extractCellRegion } from "./decode";
export type { DecodedImage } from "./decode";

export { classifyCell, DEFAULT_COLOR_PROFILE } from "./classify";
export type { CellClassifyDiagnostics } from "./classify";

export { recognizeBoard, SaoleiBoard } from "./recognize";
export type {
  RecognizeResult,
  RecognizeBoardOptions,
  CellDiagInternal,
} from "./recognize";

export { renderBoardText, cellSymbol } from "./render";

export {
  checkCompatible,
  BoardDimensionMismatchError,
  BoardStateIncompatibleError,
} from "./validate";
export type { Compatibility } from "./validate";
