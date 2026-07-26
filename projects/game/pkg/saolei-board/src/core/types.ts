/**
 * types.ts — Public types for the saolei board recognition library.
 *
 * The `CellStatus` union follows the cell-state semantics of the saolei MCP
 * (`specs/018-saolei-mcp/data-model.md` §2): INITIAL, 0..8, FLAG, HIT_MINE,
 * MINE. An extra `UNKNOWN` is exposed by this library so the CLI `--debug`
 * path can flag cells whose recognition is below threshold (to be resolved
 * during calibration); the eventual MCP integration decides how to handle it.
 */

/** A single cell's recognized status. `"UNKNOWN"` is library-only. */
export type CellStatus =
  | "INITIAL"
  | "0"
  | "1"
  | "2"
  | "3"
  | "4"
  | "5"
  | "6"
  | "7"
  | "8"
  | "FLAG"
  | "HIT_MINE"
  | "MINE"
  | "UNKNOWN";

/**
 * Recognized board state. `grid` is indexed `[y][x]` where `x` is the column
 * (0..width-1) and `y` is the row (0..height-1) — the same convention as the
 * saolei MCP (`specs/018-saolei-mcp/data-model.md` §1).
 */
export interface GameState {
  width: number;
  height: number;
  grid: CellStatus[][];
}

/** RGB triple (0..255 each). Alpha is ignored for recognition. */
export type RGB = { r: number; g: number; b: number };

/**
 * Fixed board layout in **screenshot** (full-window) pixel coordinates — NOT
 * the WM_* client coordinates used for clicking. The screenshot captured by
 * the desktop includes the non-client chrome
 * (`projects/game/desktop/internal/capture/capture.go` `CaptureWindow`), so
 * the board top is at Y=200 in screenshot space; the agent's
 * `projects/game/agent/src/mcp/saolei/geometry.ts` subtracts a 96 px chrome
 * offset to get the client-space Y=104 used for `WM_LBUTTONDOWN`. Recognition
 * reads pixels from the screenshot, so it uses the screenshot-space values.
 */
export interface BoardGeometry {
  originXPx: number;
  originYPx: number;
  cellSizePx: number;
}

/**
 * Tunable recognition profile. The default targets the classic Win32
 * Microsoft Minesweeper (winmine.exe) appearance; re-tune via the CLI
 * `--debug` output against real screenshots. Number reference colors follow
 * the classic palette (1=blue, 2=green, 3=red, 4=navy, 5=maroon, 6=teal,
 * 7=black, 8=gray — https://online.games.narkive.com/FUc9B1QB/colors-in-minesweeper).
 */
export interface ColorProfile {
  /** Revealed-cell background color (classic: 192,192,192). */
  background: RGB;
  /** Tolerance: a pixel whose Euclidean distance from `background` exceeds
   * this is treated as part of a glyph (digit / mine / flag). */
  glyphThreshold: number;
  /** Glyph pixels below this count ⇒ revealed blank ("0"). */
  minGlyphPixels: number;
  /** White-pixel bevel test: a pixel counts as "white" when R, G, and B are
   * all ≥ this (default 240). Unopened cells carry a pure-white highlight band
   * on their inner top-left edge; revealed cells are flat grey with no white. */
  bevelWhiteChannelMin: number;
  /** Minimum white-pixel count to classify a cell as beveled (unopened). The
   * real classic layout paints a multi-pixel white band, so a small threshold
   * is robust to exact bevel position. */
  bevelWhiteMinPixels: number;
  /** Width (px) of the border ring scanned for the white bevel highlight. The
   * unopened-cell white band sits on the inner top-left EDGE; mine/number glyph
   * white highlights sit in the CENTRE — restricting the scan to the border
   * ring prevents a mine icon's centre white pixel from masquerading as a
   * bevel (which would misclassify HIT_MINE as FLAG). */
  bevelBorderMargin: number;
  /** A centre pixel counts as "black" (mine glyph) when R, G, and B are all
   * ≤ this (default 60). Used to detect the dense mine blob by count before
   * number-colour voting (so a red-background HIT_MINE is not out-voted by its
   * own red background into "3"). */
  mineBlackChannelMax: number;
  /** Red-pixel test for the flag glyph: R≥flagRedMinR, G≤flagRedMaxG,
   * B≤flagRedMaxB; at least `flagMinPixels` such pixels in the center ⇒ FLAG. */
  flagRedMinR: number;
  flagRedMaxG: number;
  flagRedMaxB: number;
  flagMinPixels: number;
  /** Reference colors for digits 1..8 (keyed by the CellStatus digit string). */
  numberRefs: { status: CellStatus; color: RGB }[];
  /** Black reference (the mine glyph and the "7" digit are both near-black). */
  mineRef: RGB;
  /** Glyph-pixel count above which a black winner is classified MINE rather
   * than "7" (a mine blob has substantially more black pixels than the "7"
   * stroke). */
  minePixelThreshold: number;
  /** Mean redness of the cell background above which a mine is HIT_MINE
   * (triggered mine renders on a red background) rather than MINE. */
  redBgThreshold: number;
}

/** Result of classifying a single cell, including diagnostics for `--debug`. */
export interface CellDiagnostics {
  x: number;
  y: number;
  status: CellStatus;
  /** Sampled mean color of the center glyph region. */
  centerMean: RGB;
  /** Whether the bevel test reported the cell as unopened. */
  beveled: boolean;
  /** Count of glyph pixels (differ from background). */
  glyphPixels: number;
  /** Count of red pixels in the center region. */
  redPixels: number;
}

/** Options accepted by `recognizeBoard` and `SaoleiBoard`. */
export interface RecognizeOptions {
  /** Partial geometry override (merged onto the default). */
  geometry?: Partial<BoardGeometry>;
  /** Recognition profile override (defaults to classic Win32). */
  profile?: ColorProfile;
  /** Explicit board dimensions; overrides auto-detection from screenshot size. */
  width?: number;
  height?: number;
}
