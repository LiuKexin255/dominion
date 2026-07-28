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
 * The decoded top-left mine counter — the game's own `mines − flags` display
 * (the 3-digit red LED at screenshot-space X 32..113, Y 120..169). A tagged
 * union so the win predicate (`win.ts`) can distinguish a confidently-decoded
 * value from an undecodable read and stay lenient (never claim a win on an
 * uncertain counter). `decoded: true` carries the integer shown (negative when
 * the player over-flags; `value === 0` ⇔ the counter reads `000` ⇔
 * `flags === mines`); `decoded: false` covers an absent region, a non-classic
 * header, or a digit pattern that matched no glyph entry. See
 * `specs/028-saolei-win-counter-fix/data-model.md` entity 1.
 */
export type MineCounter =
  | { decoded: true; value: number }
  | { decoded: false };

/**
 * Recognized board state. `grid` is indexed `[y][x]` where `x` is the column
 * (0..width-1) and `y` is the row (0..height-1) — the same convention as the
 * saolei MCP (`specs/018-saolei-mcp/data-model.md` §1).
 */
export interface GameState {
  width: number;
  height: number;
  grid: CellStatus[][];
  /**
   * The decoded top-left mine counter, or `undefined` when not decoded in this
   * pass (e.g. a synthetic state). `isWin` treats `undefined` and
   * `{ decoded: false }` identically — lenient, never a win on an absent or
   * uncertain counter (`specs/028-saolei-win-counter-fix/spec.md` FR-008).
   * `renderBoardText` does NOT render it (the text board stays grid-only) and
   * `checkCompatible` does NOT compare it (the counter is non-monotonic within
   * a game).
   */
  mineCounter?: MineCounter;
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

/** The 7 segments of a counter digit cell. The middle bar `g` doubles as the
 *  minus-sign detector: a lone `{g}` ON-set maps to `-` (no digit `0`-`9` has
 *  that pattern). See `specs/028-saolei-win-counter-fix/data-model.md`
 *  entity 3. */
export type SegmentId = "a" | "b" | "c" | "d" | "e" | "f" | "g";

/**
 * Tunable 7-segment mine-counter decode profile (a peer of `ColorProfile` and
 * `BoardGeometry`). The default targets classic Win32 Microsoft Minesweeper
 * (winmine.exe); the measured constants live in
 * `specs/028-saolei-win-counter-fix/research.md` D2/D3/D5 and are re-tunable
 * via the CLI `--debug` calibration flow. See
 * `specs/028-saolei-win-counter-fix/data-model.md` entity 3.
 */
export interface CounterProfile {
  /** Counter region origin in screenshot space (default X=32, Y=120). */
  regionX: number;
  regionY: number;
  /** Counter region size in px (default 82×50). */
  regionW: number;
  regionH: number;
  /** Per-digit cell origins in screenshot space
   *  (default [{x:38,y:126},{x:64,y:126},{x:90,y:126}]). */
  cellOrigins: ReadonlyArray<{ x: number; y: number }>;
  /** Per-digit cell size in px (default 22×42). */
  cellW: number;
  cellH: number;
  /** Segment-core sub-rects in LOCAL cell coords (x0..x1, y0..y1 inclusive),
   *  measured in `specs/028-saolei-win-counter-fix/research.md` D3. A segment
   *  is ON iff its core red-pixel ratio exceeds `segmentOnRatio`. */
  segments: Record<SegmentId, { x0: number; x1: number; y0: number; y1: number }>;
  /** Red-pixel test threshold: R ≥ this ⇒ red candidate (default 150). The
   *  full test (R ≥ `redMinR`, G ≤ `redMaxG`, B ≤ `redMaxB`) is the same
   *  family as `ColorProfile.flagRed*`, tightened on G/B because the LED red
   *  is saturated on near-black (`specs/028-saolei-win-counter-fix/research.md`
   *  D5). */
  redMinR: number;
  /** G upper bound for the red-pixel test (default 80). */
  redMaxG: number;
  /** B upper bound for the red-pixel test (default 80). */
  redMaxB: number;
  /** A segment is ON iff its core red-pixel ratio exceeds this (default 0.5;
   *  measured ON ≥ 0.90, OFF = 0.00 — a wide margin, see
   *  `specs/028-saolei-win-counter-fix/research.md` D1). */
  segmentOnRatio: number;
}
