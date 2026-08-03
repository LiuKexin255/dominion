/**
 * test-helpers.ts — Synthetic cell/image builders for unit tests.
 *
 * No real screenshots are needed: these construct 32×32 RGB cell buffers that
 * match the classic Win32 Minesweeper appearance so `classifyCell` can be
 * exercised deterministically. Tuned to the DEFAULT_COLOR_PROFILE.
 */

import { PNG } from "pngjs";

import type { RGB } from "./types";

export const CELL = 32;

const BG: RGB = { r: 192, g: 192, b: 192 };
const WHITE: RGB = { r: 255, g: 255, b: 255 };
const DARK: RGB = { r: 128, g: 128, b: 128 };

/** Build a flat cellSize×cellSize buffer filled with one colour. */
export function fillCell(color: RGB, size = CELL): RGB[] {
  const out: RGB[] = [];
  for (let i = 0; i < size * size; i++) out.push({ ...color });
  return out;
}

/** Paint a bevel (white TL edge, dark BR edge) onto a buffer — makes it look
 * unopened. Edge thickness in pixels. */
export function paintBevel(pixels: RGB[], size = CELL, thickness = 2): RGB[] {
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const i = y * size + x;
      if (x < thickness || y < thickness) pixels[i] = { ...WHITE };
      if (x >= size - thickness || y >= size - thickness) pixels[i] = { ...DARK };
    }
  }
  return pixels;
}

/** Paint a solid rectangle of `color` in the centre region (margin = size/4). */
export function paintCenterBlob(
  pixels: RGB[],
  color: RGB,
  size = CELL,
): RGB[] {
  const m = Math.floor(size / 4);
  for (let y = m; y < size - m; y++) {
    for (let x = m; x < size - m; x++) {
      pixels[y * size + x] = { ...color };
    }
  }
  return pixels;
}

/** Paint a sparse glyph (a few pixels) of `color` in the centre region — enough
 * to vote as the winner without reaching the mine-pixel threshold. */
export function paintSparseGlyph(
  pixels: RGB[],
  color: RGB,
  count: number,
  size = CELL,
): RGB[] {
  const m = Math.floor(size / 4);
  let n = 0;
  for (let y = m; y < size - m && n < count; y++) {
    for (let x = m; x < size - m && n < count; x++) {
      pixels[y * size + x] = { ...color };
      n++;
    }
  }
  return pixels;
}

/** Build an unopened (INITIAL) cell. */
export function unopenedCell(): RGB[] {
  return paintBevel(fillCell(BG));
}

/** Build a revealed blank ("0") cell. */
export function blankCell(): RGB[] {
  return fillCell(BG);
}

/** Build a revealed number cell (sparse glyph of the digit's reference colour). */
export function numberCell(status: "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8"): RGB[] {
  const refs: Record<string, RGB> = {
    "1": { r: 0, g: 0, b: 255 },
    "2": { r: 0, g: 128, b: 0 },
    "3": { r: 255, g: 0, b: 0 },
    "4": { r: 0, g: 0, b: 128 },
    "5": { r: 128, g: 0, b: 0 },
    "6": { r: 0, g: 128, b: 128 },
    "7": { r: 0, g: 0, b: 0 },
    "8": { r: 128, g: 128, b: 128 },
  };
  // "7" is black and sparse (a stroke); keep count below the mine threshold.
  const count = status === "7" ? 40 : 60;
  return paintSparseGlyph(fillCell(BG), refs[status], count);
}

/** Build a flagged cell (unopened + red centre). */
export function flagCell(): RGB[] {
  return paintSparseGlyph(unopenedCell(), { r: 255, g: 0, b: 0 }, 20);
}

/** Build a revealed mine (dense black blob on grey). */
export function mineCell(): RGB[] {
  return paintCenterBlob(fillCell(BG), { r: 0, g: 0, b: 0 });
}

/** Build a triggered mine (dense black blob on red background). */
export function hitMineCell(): RGB[] {
  const red = fillCell({ r: 255, g: 0, b: 0 });
  return paintCenterBlob(red, { r: 0, g: 0, b: 0 });
}

/**
 * Compose a full PNG screenshot of the given cells laid out on a board, using
 * the default geometry (origin 24,200; cell 32). Returns PNG bytes (Buffer).
 */
export function buildScreenshot(cells: RGB[][][], opts?: {
  originX?: number;
  originY?: number;
  cellSize?: number;
  margin?: number;
}): Buffer {
  const ox = opts?.originX ?? 24;
  const oy = opts?.originY ?? 200;
  const cs = opts?.cellSize ?? CELL;
  const margin = opts?.margin ?? 8;
  const rows = cells.length;
  const cols = cells[0]?.length ?? 0;
  const width = ox + cols * cs + margin;
  const height = oy + rows * cs + margin;
  const png = new PNG({ width, height });

  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const i = (width * y + x) << 2;
      png.data[i] = 0;
      png.data[i + 1] = 0;
      png.data[i + 2] = 0;
      png.data[i + 3] = 255;
    }
  }
  for (let cy = 0; cy < rows; cy++) {
    for (let cx = 0; cx < cols; cx++) {
      const cell = cells[cy][cx];
      for (let py = 0; py < cs; py++) {
        for (let px = 0; px < cs; px++) {
          const x = ox + cx * cs + px;
          const y = oy + cy * cs + py;
          const i = (width * y + x) << 2;
          const c = cell[py * cs + px];
          png.data[i] = c.r;
          png.data[i + 1] = c.g;
          png.data[i + 2] = c.b;
          png.data[i + 3] = 255;
        }
      }
    }
  }
  return PNG.sync.write(png) as unknown as Buffer;
}
