/**
 * classify.ts — Single-cell state classification from a raw RGB region.
 *
 * Pipeline (per cell), grounded in the community-proven color-analysis approach
 * for fixed-layout classic Win32 Minesweeper
 * (https://www.besthub.dev/articles/how-to-build-an-automated-minesweeper-bot-with-python-and-win32-api-d1d7ef54e731
 * — the centre/region colour determines the state; a `tile.getcolors()`
 * histogram match is the most robust variant):
 *
 *   1. Bevel test — an unopened cell carries a pure-white highlight band on
 *      its inner top-left edge (the raised-button look); a revealed cell is
 *      flat grey with no pure-white pixels. Detected by counting white pixels
 *      across the cell (robust to the exact bevel position).
 *   2. Flag test — an unopened cell with a red flag glyph has red pixels
 *      (R high, G/B low) in the centre region.
 *   3. Glyph analysis (revealed cells) — collect centre pixels that differ from
 *      the background; classify by nearest reference colour. The mine glyph and
 *      the "7" digit are both near-black, so they are disambiguated by glyph
 *      pixel count (a mine blob is denser than the "7" stroke). HIT_MINE vs
 *      MINE is decided by background redness (a triggered mine renders on red).
 *
 * All thresholds live in `ColorProfile`; tune via the CLI `--debug` output
 * against real screenshots. Number reference colours follow the classic
 * palette (1=blue, 2=green, 3=red, 4=navy, 5=maroon, 6=teal, 7=black, 8=gray
 * — https://online.games.narkive.com/FUc9B1QB/colors-in-minesweeper).
 */

import type { CellStatus, ColorProfile, RGB } from "./types.js";

/** Classic Win32 Microsoft Minesweeper recognition profile. */
export const DEFAULT_COLOR_PROFILE: ColorProfile = {
  background: { r: 192, g: 192, b: 192 },
  glyphThreshold: 40,
  minGlyphPixels: 8,
  bevelWhiteChannelMin: 240,
  bevelWhiteMinPixels: 8,
  bevelBorderMargin: 4,
  mineBlackChannelMax: 60,
  flagRedMinR: 150,
  flagRedMaxG: 100,
  flagRedMaxB: 100,
  flagMinPixels: 5,
  numberRefs: [
    { status: "1", color: { r: 0, g: 0, b: 255 } },
    { status: "2", color: { r: 0, g: 128, b: 0 } },
    { status: "3", color: { r: 255, g: 0, b: 0 } },
    { status: "4", color: { r: 0, g: 0, b: 128 } },
    { status: "5", color: { r: 128, g: 0, b: 0 } },
    { status: "6", color: { r: 0, g: 128, b: 128 } },
    { status: "7", color: { r: 0, g: 0, b: 0 } },
    { status: "8", color: { r: 128, g: 128, b: 128 } },
  ],
  mineRef: { r: 0, g: 0, b: 0 },
  minePixelThreshold: 80,
  redBgThreshold: 120,
};

/** Squared Euclidean distance between two RGB colours (avoids sqrt). */
function distSq(a: RGB, b: RGB): number {
  const dr = a.r - b.r;
  const dg = a.g - b.g;
  const db = a.b - b.b;
  return dr * dr + dg * dg + db * db;
}

/** Index in a flat row-major cell buffer of size `w × h` for (cx, cy). */
function idx(cx: number, cy: number, w: number): number {
  return cy * w + cx;
}

/** Mean colour of a rectangular sub-region of a cell buffer. */
function regionMean(
  pixels: RGB[],
  w: number,
  h: number,
  x0: number,
  y0: number,
  x1: number,
  y1: number,
): RGB {
  let r = 0;
  let g = 0;
  let b = 0;
  let n = 0;
  for (let cy = y0; cy < y1; cy++) {
    for (let cx = x0; cx < x1; cx++) {
      if (cx < 0 || cx >= w || cy < 0 || cy >= h) continue;
      const p = pixels[idx(cx, cy, w)];
      r += p.r;
      g += p.g;
      b += p.b;
      n++;
    }
  }
  if (n === 0) return { r: 0, g: 0, b: 0 };
  return { r: r / n, g: g / n, b: b / n };
}

/**
 * Classify a single cell from its raw RGB pixel buffer and dimensions, plus
 * diagnostics used by the CLI `--debug` path. Pure and side-effect free so it
 * can be unit-tested with synthetic pixel data (style/javascript.md §测试 — DI).
 */
export function classifyCell(
  pixels: RGB[],
  width: number,
  height: number,
  profile: ColorProfile = DEFAULT_COLOR_PROFILE,
): { status: CellStatus; diagnostics?: CellClassifyDiagnostics } {
  const center = centerRegion(width, height);
  const centerMean = regionMean(
    pixels,
    width,
    height,
    center.x0,
    center.y0,
    center.x1,
    center.y1,
  );

  // Bevel test: count pure-white pixels in the BORDER ring only. An unopened
  // cell's white highlight band runs along its inner top-left EDGE; a mine
  // icon's white highlight sits in the CENTRE. Scanning just the border ring
  // keeps a mine's centre white pixel from masquerading as a bevel (which would
  // misclassify a HIT_MINE — red background + black mine — as FLAG).
  const wmin = profile.bevelWhiteChannelMin;
  const bm = profile.bevelBorderMargin;
  let whitePixels = 0;
  for (let cy = 0; cy < height; cy++) {
    for (let cx = 0; cx < width; cx++) {
      const inBorder =
        cx < bm || cx >= width - bm || cy < bm || cy >= height - bm;
      if (!inBorder) continue;
      const p = pixels[idx(cx, cy, width)];
      if (p.r >= wmin && p.g >= wmin && p.b >= wmin) whitePixels++;
    }
  }
  const beveled = whitePixels >= profile.bevelWhiteMinPixels;

  // Count red pixels in the centre region (flag glyph).
  let redPixels = 0;
  for (let cy = center.y0; cy < center.y1; cy++) {
    for (let cx = center.x0; cx < center.x1; cx++) {
      const p = pixels[idx(cx, cy, width)];
      if (
        p.r >= profile.flagRedMinR &&
        p.g <= profile.flagRedMaxG &&
        p.b <= profile.flagRedMaxB
      ) {
        redPixels++;
      }
    }
  }

  if (beveled) {
    const status: CellStatus =
      redPixels >= profile.flagMinPixels ? "FLAG" : "INITIAL";
    return {
      status,
      diagnostics: {
        centerMean,
        beveled,
        glyphPixels: 0,
        blackPixels: 0,
        redPixels,
        winnerRef: null,
        bgRedness: 0,
      },
    };
  }

  // Revealed cell: scan the centre once, collecting glyph pixels (differ from
  // the grey background) and counting near-black pixels (the mine blob).
  const glyphPixels: RGB[] = [];
  let blackPixels = 0;
  const bmax = profile.mineBlackChannelMax;
  for (let cy = center.y0; cy < center.y1; cy++) {
    for (let cx = center.x0; cx < center.x1; cx++) {
      const p = pixels[idx(cx, cy, width)];
      if (p.r <= bmax && p.g <= bmax && p.b <= bmax) blackPixels++;
      const d = Math.sqrt(distSq(p, profile.background));
      if (d > profile.glyphThreshold) glyphPixels.push(p);
    }
  }
  // Border redness: a triggered mine renders on a red background; an end-game
  // revealed mine on grey.
  const bgRedness = borderRedness(pixels, width, height, center);

  // Mine check FIRST (by blob density, not colour voting): a dense black blob
  // is a mine. This must precede number voting so a HIT_MINE's red background
  // does not out-vote the mine into "3".
  if (blackPixels >= profile.minePixelThreshold) {
    const status: CellStatus =
      bgRedness >= profile.redBgThreshold ? "HIT_MINE" : "MINE";
    return {
      status,
      diagnostics: {
        centerMean,
        beveled,
        glyphPixels: glyphPixels.length,
        blackPixels,
        redPixels,
        winnerRef: "__MINE__",
        bgRedness,
      },
    };
  }

  if (glyphPixels.length < profile.minGlyphPixels) {
    return {
      status: "0",
      diagnostics: {
        centerMean,
        beveled,
        glyphPixels: glyphPixels.length,
        blackPixels,
        redPixels,
        winnerRef: null,
        bgRedness,
      },
    };
  }

  // Number voting: nearest reference colour among the digit refs (1..8, with
  // "7" = black). The mine is handled above by blob density, so a sparse black
  // "7" stroke votes "7" while a dense mine blob never reaches here.
  const refs = profile.numberRefs.map((r) => ({
    status: r.status,
    color: r.color,
  }));
  const votes = new Map<string, number>();
  let bestStatus = "UNKNOWN";
  let bestVotes = -1;
  for (const p of glyphPixels) {
    let nearest = refs[0];
    let nearestD = Infinity;
    for (const ref of refs) {
      const d = distSq(p, ref.color);
      if (d < nearestD) {
        nearestD = d;
        nearest = ref;
      }
    }
    const k = nearest.status as string;
    votes.set(k, (votes.get(k) ?? 0) + 1);
    if (votes.get(k)! > bestVotes) {
      bestVotes = votes.get(k)!;
      bestStatus = k;
    }
  }

  return {
    status: bestStatus as CellStatus,
    diagnostics: {
      centerMean,
      beveled,
      glyphPixels: glyphPixels.length,
      blackPixels,
      redPixels,
      winnerRef: bestStatus,
      bgRedness,
    },
  };
}

/** Internal diagnostics shape (richer than the public CellDiagnostics). */
export interface CellClassifyDiagnostics {
  centerMean: RGB;
  beveled: boolean;
  glyphPixels: number;
  blackPixels: number;
  redPixels: number;
  winnerRef: string | null;
  bgRedness: number;
}

/** Centre region of a cell, leaving a margin for the bevel/border. */
function centerRegion(
  w: number,
  h: number,
): { x0: number; y0: number; x1: number; y1: number } {
  const margin = Math.floor(Math.min(w, h) / 4);
  return {
    x0: margin,
    y0: margin,
    x1: w - margin,
    y1: h - margin,
  };
}

/**
 * Mean redness of the cell border (the frame outside the centre region).
 * Redness = R − (G+B)/2; pure red ⇒ 255, grey ⇒ 0. Used to tell a triggered
 * mine (red background) from an end-game revealed mine (grey background).
 */
function borderRedness(
  pixels: RGB[],
  w: number,
  h: number,
  center: { x0: number; y0: number; x1: number; y1: number },
): number {
  let sum = 0;
  let n = 0;
  for (let cy = 0; cy < h; cy++) {
    for (let cx = 0; cx < w; cx++) {
      const inCenter =
        cx >= center.x0 && cx < center.x1 && cy >= center.y0 && cy < center.y1;
      if (inCenter) continue;
      const p = pixels[idx(cx, cy, w)];
      sum += p.r - (p.g + p.b) / 2;
      n++;
    }
  }
  return n > 0 ? sum / n : 0;
}
