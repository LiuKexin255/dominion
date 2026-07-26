/**
 * decode.ts — PNG decoding (pngjs) and raw-pixel access helpers.
 *
 * The desktop captures screenshots as PNG bytes
 * (`projects/game/desktop/internal/capture/capture.go` `CaptureWindow`); the
 * agent receives them base64-encoded (`OperationScreenshot.data`). Callers
 * pass the raw PNG bytes (a `Buffer.from(b64, "base64")` or a file read) to
 * `decodePng`. pngjs is pure JavaScript (no native dependency), so it is
 * Bazel-friendly.
 *
 * Pixel access: pngjs stores RGBA channels contiguously, so the byte index of
 * pixel (x, y) is `(width * y + x) << 2` (×4) — see
 * https://github.com/pngjs/pngjs (the API is stable across v6/v7).
 */

import { PNG } from "pngjs";

import type { RGB } from "./types";

/** Decoded image: dimensions + raw RGBA byte buffer. */
export interface DecodedImage {
  width: number;
  height: number;
  /** RGBA bytes, length = width × height × 4. */
  data: Uint8Array;
}

/** Synchronously decode PNG bytes into an RGBA image. */
export function decodePng(bytes: Buffer | Uint8Array): DecodedImage {
  const png = PNG.sync.read(Buffer.from(bytes));
  return {
    width: png.width,
    height: png.height,
    data: png.data as unknown as Uint8Array,
  };
}

/** Byte offset of pixel (x, y) in an RGBA buffer of the given width. */
export function pixelIndex(width: number, x: number, y: number): number {
  return (width * y + x) << 2;
}

/** Read the RGB value of pixel (x, y) from an RGBA buffer. Alpha is dropped. */
export function getRGB(
  data: Uint8Array,
  width: number,
  x: number,
  y: number,
): RGB {
  const i = pixelIndex(width, x, y);
  return { r: data[i], g: data[i + 1], b: data[i + 2] };
}

/**
 * Extract a `cellSize × cellSize` region of RGB pixels starting at
 * `(originX, originY)`. Returns a flat array of `RGB` (row-major within the
 * cell) plus the cell's pixel width. Used to feed `classifyCell`.
 */
export function extractCellRegion(
  img: DecodedImage,
  originX: number,
  originY: number,
  cellSize: number,
): { pixels: RGB[]; width: number; height: number } {
  const pixels: RGB[] = [];
  for (let dy = 0; dy < cellSize; dy++) {
    const py = originY + dy;
    if (py < 0 || py >= img.height) {
      for (let dx = 0; dx < cellSize; dx++) pixels.push({ r: 0, g: 0, b: 0 });
      continue;
    }
    for (let dx = 0; dx < cellSize; dx++) {
      const px = originX + dx;
      if (px < 0 || px >= img.width) {
        pixels.push({ r: 0, g: 0, b: 0 });
        continue;
      }
      pixels.push(getRGB(img.data, img.width, px, py));
    }
  }
  return { pixels, width: cellSize, height: cellSize };
}
