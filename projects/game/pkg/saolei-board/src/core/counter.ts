/**
 * counter.ts — Fixed-geometry 7-segment mine-counter decoder (peer of
 * `classify.ts`).
 *
 * Decodes the classic Win32 Microsoft Minesweeper top-left mine counter — the
 * 3-digit red 7-segment LED at screenshot-space X 32..113, Y 120..169 — into an
 * integer `value` (= `mines − flags`; negative when the player over-flags) or
 * `{ decoded: false }` when a digit pattern matches no glyph entry (lenient —
 * never fabricate a digit). The value is the game's own ground-truth display;
 * `value === 0` ⇔ the counter reads `000` ⇔ `flags === mines`.
 *
 * Algorithm (per the fixed-geometry colour-analysis approach validated by
 * first-party fixture pixel analysis —
 * https://www.besthub.dev/articles/how-to-build-an-automated-minesweeper-bot-with-python-and-win32-api-d1d7ef54e731
 * — the centre/region colour determines the state; here the segment-core
 * red-pixel ratio determines ON/OFF):
 *
 *   For each of the 3 fixed digit cells:
 *     1. For each segment s ∈ {a,b,c,d,e,f,g}, count red pixels (R ≥ redMinR,
 *        G ≤ redMaxG, B ≤ redMaxB — the same family as `ColorProfile.flagRed*`,
 *        tightened on G/B because the LED red is saturated on near-black) in
 *        its core sub-rect; mark s ON iff `count / coreArea > segmentOnRatio`.
 *     2. Look up the ON-segment set in the glyph table (`0`-`9`, or the lone
 *        `{g}` ⇒ `-` minus sign).
 *     3. If the ON-set matches no entry ⇒ the whole counter is undecodable.
 *   Then compute `value` per sign semantics: cell-0 `-` ⇒
 *   `-(10·d1 + d2)`; else `100·d0 + 10·d1 + d2`.
 *
 * The measured constants (region box, per-digit-cell origins, segment-core
 * sub-rects, red-pixel test, ON ratio) live in `DEFAULT_COUNTER_PROFILE` and are
 * re-tunable via the CLI `--debug` calibration flow. See
 * `specs/028-saolei-win-counter-fix/research.md` D1/D2/D3/D4/D5 and
 * `specs/028-saolei-win-counter-fix/data-model.md` entity 3/5.
 *
 * Pure: no I/O, no mutation, no side effects — consumes the already-decoded
 * image only (style/javascript.md §测试 — DI / pure-function testing).
 */

import type { DecodedImage } from "./decode";
import { getRGB } from "./decode";
import type { CounterProfile, MineCounter, SegmentId } from "./types";

/** Classic Win32 Microsoft Minesweeper mine-counter decode profile, using the
 *  measured constants from `specs/028-saolei-win-counter-fix/research.md`
 *  D2 (digit-cell geometry), D3 (segment-core sub-rects), D5 (red-pixel test),
 *  D1 (segmentOnRatio — measured ON ≥ 0.90, OFF = 0.00, so 0.5 sits in a wide
 *  margin). */
export const DEFAULT_COUNTER_PROFILE: CounterProfile = {
  regionX: 32,
  regionY: 120,
  regionW: 82,
  regionH: 50,
  cellOrigins: [
    { x: 38, y: 126 },
    { x: 64, y: 126 },
    { x: 90, y: 126 },
  ],
  cellW: 22,
  cellH: 42,
  segments: {
    // Horizontal cores (research.md D3); `g` doubles as the minus-sign bar.
    a: { x0: 2, x1: 19, y0: 0, y1: 1 },
    g: { x0: 2, x1: 19, y0: 20, y1: 21 },
    d: { x0: 2, x1: 19, y0: 40, y1: 41 },
    // Vertical cores.
    f: { x0: 0, x1: 5, y0: 4, y1: 17 },
    b: { x0: 16, x1: 21, y0: 4, y1: 17 },
    e: { x0: 0, x1: 5, y0: 24, y1: 37 },
    c: { x0: 16, x1: 21, y0: 24, y1: 37 },
  },
  redMinR: 150,
  redMaxG: 80,
  redMaxB: 80,
  segmentOnRatio: 0.5,
};

/** The 7 segments in canonical (alphabetical) order so an ON-set built by
 *  iterating this array joins into an already-sorted key. */
const SEGMENT_IDS: ReadonlyArray<SegmentId> = [
  "a",
  "b",
  "c",
  "d",
  "e",
  "f",
  "g",
];

/**
 * Standard 7-segment glyph table (research.md D4). Keyed by the sorted string
 * of ON segments; the lone `{g}` ⇒ `-` minus sign (no digit `0`-`9` has that
 * pattern).
 */
const GLYPH_TABLE: ReadonlyMap<string, string> = new Map<string, string>([
  ["abcdef", "0"],
  ["bc", "1"],
  ["abdeg", "2"],
  ["abcdg", "3"],
  ["bcfg", "4"],
  ["acdfg", "5"],
  ["acdefg", "6"],
  ["abc", "7"],
  ["abcdefg", "8"],
  ["abcdfg", "9"],
  ["g", "-"],
]);

/**
 * Decode the top-left mine counter from an already-decoded screenshot image
 * (FR-001/FR-003/FR-004). Pure: no I/O, no mutation. Returns
 * `{ decoded: true; value }` (value = `mines − flags`, may be negative) or
 * `{ decoded: false }` when a digit pattern matched no glyph entry (lenient).
 *
 * Algorithm and value semantics: `specs/028-saolei-win-counter-fix/research.md`
 * D1/D3/D4 and `specs/028-saolei-win-counter-fix/data-model.md` entity 5.
 */
export function decodeMineCounter(
  img: DecodedImage,
  profile: CounterProfile = DEFAULT_COUNTER_PROFILE,
): MineCounter {
  const glyphs: (string | null)[] = [];
  for (const origin of profile.cellOrigins) {
    glyphs.push(decodeCell(img, origin.x, origin.y, profile));
  }

  if (glyphs.some((g) => g === null)) {
    return { decoded: false };
  }

  const [g0, g1, g2] = glyphs as string[];
  let value: number;
  if (g0 === "-") {
    value = -(10 * Number(g1) + Number(g2));
  } else {
    value = 100 * Number(g0) + 10 * Number(g1) + Number(g2);
  }
  return { decoded: true, value };
}

/** Decode a single 3-cell digit cell into a glyph (`0`-`9`, `-`) or `null`
 *  when its ON-segment set matches no table entry. */
function decodeCell(
  img: DecodedImage,
  cellX: number,
  cellY: number,
  profile: CounterProfile,
): string | null {
  let onKey = "";
  for (const segId of SEGMENT_IDS) {
    if (isSegmentOn(img, cellX, cellY, profile.segments[segId], profile)) {
      onKey += segId;
    }
  }
  return GLYPH_TABLE.get(onKey) ?? null;
}

/** Count red pixels in the segment's core sub-rect; ON iff the ratio exceeds
 *  `segmentOnRatio` (strictly greater — research.md D3). */
function isSegmentOn(
  img: DecodedImage,
  cellX: number,
  cellY: number,
  core: { x0: number; x1: number; y0: number; y1: number },
  profile: CounterProfile,
): boolean {
  let redCount = 0;
  let total = 0;
  for (let dy = core.y0; dy <= core.y1; dy++) {
    for (let dx = core.x0; dx <= core.x1; dx++) {
      const px = cellX + dx;
      const py = cellY + dy;
      total++;
      if (px < 0 || px >= img.width || py < 0 || py >= img.height) {
        continue;
      }
      const { r, g, b } = getRGB(img.data, img.width, px, py);
      if (r >= profile.redMinR && g <= profile.redMaxG && b <= profile.redMaxB) {
        redCount++;
      }
    }
  }
  if (total === 0) return false;
  return redCount / total > profile.segmentOnRatio;
}
