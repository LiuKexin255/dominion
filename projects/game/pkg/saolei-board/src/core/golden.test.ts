import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { recognizeBoard } from "./recognize";
import { renderBoardText } from "./render";
import { isWin } from "./win";

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

const TESTDATA = path.join(__dirname, "..", "..", "testdata");
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
});
