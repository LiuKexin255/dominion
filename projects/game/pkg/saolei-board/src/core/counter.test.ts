/**
 * counter.test.ts — Unit tests for the mine-counter decoder.
 *
 * Pure-function tests: each case builds a synthetic `DecodedImage` with red
 * pixels painted at the segment cores for the glyphs under test (everything
 * else stays black, mirroring the real red-on-black LED), then asserts
 * `decodeMineCounter`'s output. No I/O, no fixtures — the algorithm is
 * exercised directly (style/javascript.md §测试 — DI / pure-function testing).
 * Real-screenshot coverage lives in `golden.test.ts`.
 */

import { describe, expect, it } from "vitest";

import { DEFAULT_COUNTER_PROFILE, decodeMineCounter } from "./counter.js";
import type { DecodedImage } from "./decode.js";
import type { CounterProfile, SegmentId } from "./types.js";

const SEGMENT_IDS: ReadonlyArray<SegmentId> = [
  "a",
  "b",
  "c",
  "d",
  "e",
  "f",
  "g",
];

/** Inverse of the glyph table in `counter.ts` (research.md D4): glyph → ON
 *  segments. Used to paint synthetic LED cells. */
const GLYPH_SEGMENTS: Record<string, ReadonlyArray<SegmentId>> = {
  "0": ["a", "b", "c", "d", "e", "f"],
  "1": ["b", "c"],
  "2": ["a", "b", "d", "e", "g"],
  "3": ["a", "b", "c", "d", "g"],
  "4": ["b", "c", "f", "g"],
  "5": ["a", "c", "d", "f", "g"],
  "6": ["a", "c", "d", "e", "f", "g"],
  "7": ["a", "b", "c"],
  "8": ["a", "b", "c", "d", "e", "f", "g"],
  "9": ["a", "b", "c", "d", "f", "g"],
  "-": ["g"],
};

/** Build a blank (all-black, opaque-alpha) image large enough to hold the
 *  counter region. */
function makeBlankImage(
  profile: CounterProfile = DEFAULT_COUNTER_PROFILE,
): DecodedImage {
  const w = profile.regionX + profile.regionW;
  const h = profile.regionY + profile.regionH;
  const data = new Uint8Array(w * h * 4);
  for (let i = 0; i < w * h; i++) {
    data[i * 4 + 3] = 255;
  }
  return { width: w, height: h, data };
}

/** Paint a single segment's core red at the given cell. */
function paintSegment(
  img: DecodedImage,
  cellIndex: number,
  segId: SegmentId,
  profile: CounterProfile = DEFAULT_COUNTER_PROFILE,
): void {
  const origin = profile.cellOrigins[cellIndex];
  const core = profile.segments[segId];
  for (let dy = core.y0; dy <= core.y1; dy++) {
    for (let dx = core.x0; dx <= core.x1; dx++) {
      const px = origin.x + dx;
      const py = origin.y + dy;
      if (px < 0 || px >= img.width || py < 0 || py >= img.height) continue;
      const i = (img.width * py + px) << 2;
      img.data[i] = 255;
      img.data[i + 1] = 0;
      img.data[i + 2] = 0;
    }
  }
}

/** Paint all ON-segment cores for `glyph` at the given cell (OFF cores stay
 *  black). */
function paintCell(
  img: DecodedImage,
  cellIndex: number,
  glyph: string,
  profile: CounterProfile = DEFAULT_COUNTER_PROFILE,
): void {
  const onSegs = new Set<SegmentId>(GLYPH_SEGMENTS[glyph]);
  for (const segId of SEGMENT_IDS) {
    if (onSegs.has(segId)) {
      paintSegment(img, cellIndex, segId, profile);
    }
  }
}

describe("decodeMineCounter", () => {
  describe("each digit glyph 0-9 (segment pattern → glyph)", () => {
    for (let d = 0; d <= 9; d++) {
      it(`decodes all-${d} cells as value ${111 * d}`, () => {
        const img = makeBlankImage();
        const glyph = String(d);
        paintCell(img, 0, glyph);
        paintCell(img, 1, glyph);
        paintCell(img, 2, glyph);
        expect(decodeMineCounter(img)).toEqual({
          decoded: true,
          value: 111 * d,
        });
      });
    }
  });

  it("decodes the lone {g} ON-set in cell 0 as the minus sign", () => {
    // `-` + `0` + `1` ⇒ value −1 (research.md D4 sign semantics).
    const img = makeBlankImage();
    paintCell(img, 0, "-");
    paintCell(img, 1, "0");
    paintCell(img, 2, "1");
    expect(decodeMineCounter(img)).toEqual({ decoded: true, value: -1 });
  });

  describe("value-with-sign computation", () => {
    it("computes a negative value when cell 0 is `-` (-01 ⇒ -1)", () => {
      const img = makeBlankImage();
      paintCell(img, 0, "-");
      paintCell(img, 1, "0");
      paintCell(img, 2, "1");
      expect(decodeMineCounter(img)).toEqual({ decoded: true, value: -1 });
    });

    it("computes a positive value for all-digit cells (040 ⇒ 40)", () => {
      const img = makeBlankImage();
      paintCell(img, 0, "0");
      paintCell(img, 1, "4");
      paintCell(img, 2, "0");
      expect(decodeMineCounter(img)).toEqual({ decoded: true, value: 40 });
    });

    it("computes 000 ⇒ 0 (the win signal)", () => {
      const img = makeBlankImage();
      paintCell(img, 0, "0");
      paintCell(img, 1, "0");
      paintCell(img, 2, "0");
      expect(decodeMineCounter(img)).toEqual({ decoded: true, value: 0 });
    });
  });

  describe("leniency (undecodable)", () => {
    it("returns { decoded: false } when a cell's ON-set matches no glyph", () => {
      // Cell 1 has only segment `a` ON → ON-set {a} matches no table entry.
      const img = makeBlankImage();
      paintCell(img, 0, "0");
      paintCell(img, 2, "0");
      paintSegment(img, 1, "a");
      expect(decodeMineCounter(img)).toEqual({ decoded: false });
    });

    it("returns { decoded: false } for a fully-blank (all-off) cell", () => {
      const img = makeBlankImage();
      paintCell(img, 0, "0");
      paintCell(img, 2, "0");
      // Cell 1 left entirely black → ON-set {} matches no entry.
      expect(decodeMineCounter(img)).toEqual({ decoded: false });
    });
  });
});
