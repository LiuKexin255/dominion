import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { recognizeBoard } from "./recognize.js";
import { renderBoardText } from "./render.js";
import { isWin } from "./win.js";

/**
 * golden.test.ts — Golden recognition tests against real desktop screenshots.
 *
 * Each `testdata/saolei_N.png` is a captured Microsoft Minesweeper window; its
 * `testdata/saolei_N.golden.txt` holds the reviewed-and-accepted text board.
 * These tests pin the recognition output so a regression (algorithm change,
 * threshold tweak) is caught before the model ever sees a wrong board. To
 * update a golden after an intentional change, re-run the CLI and overwrite the
 * `.golden.txt` — do NOT edit golden files by hand without re-reviewing the
 * screenshot.
 */

const TESTDATA = path.join(import.meta.dirname, "..", "..", "testdata");
const CASES = [
  "saolei_1",
  "saolei_2",
  "saolei_3",
  "saolei_4",
  "saolei_5",
  "saolei_6",
  "saolei_7",
  "saolei_8",
  "saolei_10",
];

describe("golden recognition (real screenshots)", () => {
  for (const name of CASES) {
    it(`recognizes ${name}.png matching ${name}.golden.txt`, () => {
      const png = readFileSync(path.join(TESTDATA, `${name}.png`));
      const golden = readFileSync(
        path.join(TESTDATA, `${name}.golden.txt`),
        "utf8",
      ).replace(/\s+$/, "");
      const { state } = recognizeBoard(png);
      expect(renderBoardText(state)).toBe(golden);
    });
  }

  it("classifies the real win screenshot (saolei_10.png) as a win", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_10.png"));
    const { state } = recognizeBoard(png);
    expect(isWin(state)).toBe(true);
  });

  // Counter-informed win classification (FR-005..010 / SC-002 / SC-003): the
  // two badcase fixtures exercise the two halves of the win conjunction —
  //   saolei_9  — grid all-revealed/flagged but the counter reads `-01`
  //               (over-flagged: 11 flags > 10 mines) ⇒ the grid half holds,
  //               the counter half fails ⇒ NOT a win (the false-positive fix).
  //   saolei_11 — counter `000` (flags == mines) but the grid has `INITIAL`
  //               cells ⇒ the counter half holds, the grid half fails ⇒ NOT a
  //               win (counter-alone is not a win).
  // The genuine-win fixture saolei_10 (both halves hold) stays a win —
  // asserted above (FR-005/SC-003, unchanged).
  it("classifies the over-flagged screenshot (saolei_9.png) as NOT a win (FR-006/SC-002)", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_9.png"));
    const { state } = recognizeBoard(png);
    expect(isWin(state)).toBe(false);
  });

  it("classifies the counter-000-but-unrevealed screenshot (saolei_11.png) as NOT a win (FR-007/SC-003)", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_11.png"));
    const { state } = recognizeBoard(png);
    expect(isWin(state)).toBe(false);
  });

  // Counter decode against the three win-boundary fixtures (SC-001 / FR-004):
  //   saolei_9  — grid all-revealed, over-flagged (11 flags > 10 mines) ⇒ -01
  //   saolei_10 — genuine win (flags == mines) ⇒ 000
  //   saolei_11 — counter 000 but grid has INITIAL ⇒ not won, but counter 000
  it("decodes saolei_9.png mine counter as -1 (over-flagged `-01`)", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_9.png"));
    const { state } = recognizeBoard(png);
    expect(state.mineCounter).toEqual({ decoded: true, value: -1 });
  });

  it("decodes saolei_10.png mine counter as 0 (`000`)", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_10.png"));
    const { state } = recognizeBoard(png);
    expect(state.mineCounter).toEqual({ decoded: true, value: 0 });
  });

  it("decodes saolei_11.png mine counter as 0 (`000`)", () => {
    const png = readFileSync(path.join(TESTDATA, "saolei_11.png"));
    const { state } = recognizeBoard(png);
    expect(state.mineCounter).toEqual({ decoded: true, value: 0 });
  });
});
