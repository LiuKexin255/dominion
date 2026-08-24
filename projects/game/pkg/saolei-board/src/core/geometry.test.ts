import { describe, expect, it } from "vitest";

import { cellOrigin, detectBoardSize, DEFAULT_GEOMETRY } from "./geometry.js";

describe("detectBoardSize", () => {
  it("derives cols/rows from screenshot size at the default geometry", () => {
    // Beginner 9×9: width = 24 + 9*32 + margin; height = 200 + 9*32 + margin.
    const imgW = 24 + 9 * 32 + 8;
    const imgH = 200 + 9 * 32 + 8;
    expect(detectBoardSize(imgW, imgH)).toEqual({ width: 9, height: 9 });
  });

  it("derives intermediate 16×16", () => {
    const imgW = 24 + 16 * 32 + 8;
    const imgH = 200 + 16 * 32 + 8;
    expect(detectBoardSize(imgW, imgH)).toEqual({ width: 16, height: 16 });
  });

  it("clamps to at least 1×1 for a tiny image", () => {
    expect(detectBoardSize(10, 10)).toEqual({ width: 1, height: 1 });
  });
});

describe("cellOrigin", () => {
  it("computes the screenshot-space top-left of a cell", () => {
    const o = cellOrigin(3, 4, DEFAULT_GEOMETRY);
    expect(o.x).toBe(24 + 3 * 32);
    expect(o.y).toBe(200 + 4 * 32);
  });
});
