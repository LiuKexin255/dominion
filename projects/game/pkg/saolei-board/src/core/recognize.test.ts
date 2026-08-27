import { describe, expect, it } from "vitest";

import { recognizeBoard, SaoleiBoard } from "./recognize.js";
import { renderBoardText } from "./render.js";
import {
  blankCell,
  buildScreenshot,
  flagCell,
  hitMineCell,
  mineCell,
  numberCell,
  unopenedCell,
} from "./test-helpers.js";
import {
  BoardDimensionMismatchError,
  BoardStateIncompatibleError,
} from "./validate.js";

describe("recognizeBoard", () => {
  it("decodes a synthetic 3×2 screenshot and classifies each cell", () => {
    const cells = [
      [unopenedCell(), blankCell(), numberCell("1")],
      [flagCell(), mineCell(), hitMineCell()],
    ];
    const png = buildScreenshot(cells);
    const { state } = recognizeBoard(png, { width: 3, height: 2 });

    expect(state.width).toBe(3);
    expect(state.height).toBe(2);
    expect(state.grid[0]).toEqual(["INITIAL", "0", "1"]);
    expect(state.grid[1]).toEqual(["FLAG", "MINE", "HIT_MINE"]);
  });

  it("auto-detects board dimensions from the screenshot size", () => {
    const cells = [[unopenedCell(), unopenedCell(), unopenedCell()]];
    const png = buildScreenshot(cells);
    const { state } = recognizeBoard(png);
    expect(state.width).toBe(3);
    expect(state.height).toBe(1);
  });

  it("collects diagnostics when requested", () => {
    const cells = [[numberCell("3"), unopenedCell()]];
    const png = buildScreenshot(cells);
    const { diagnostics } = recognizeBoard(png, {
      width: 2,
      height: 1,
      collectDiagnostics: true,
    });
    expect(diagnostics).toBeDefined();
    expect(diagnostics?.[0]?.[0]?.winnerRef).toBe("3");
    expect(diagnostics?.[0]?.[1]?.beveled).toBe(true);
  });
});

describe("SaoleiBoard", () => {
  it("init recognizes the first screenshot and fixes dimensions", () => {
    const png = buildScreenshot([
      [unopenedCell(), blankCell(), numberCell("1")],
    ]);
    const board = SaoleiBoard.init(png);
    expect(board.dimensions).toEqual({ width: 3, height: 1 });
    expect(board.state.grid).toEqual([["INITIAL", "0", "1"]]);
  });

  it("updateFromScreenshot applies a same-game successor", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[unopenedCell(), unopenedCell()]]),
    );
    expect(board.state.grid).toEqual([["INITIAL", "INITIAL"]]);

    const state = board.updateFromScreenshot(
      buildScreenshot([[numberCell("5"), flagCell()]]),
    );
    expect(state.grid).toEqual([["5", "FLAG"]]);
    expect(board.state.grid).toEqual([["5", "FLAG"]]);
  });

  it("updateFromScreenshot allows flag toggling (INITIAL↔FLAG)", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[flagCell(), unopenedCell()]]),
    );
    const state = board.updateFromScreenshot(
      buildScreenshot([[unopenedCell(), flagCell()]]),
    );
    expect(state.grid).toEqual([["INITIAL", "FLAG"]]);
  });

  it("updateFromScreenshot rejects a dimension change", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[unopenedCell(), unopenedCell()]]),
    );
    expect(() =>
      board.updateFromScreenshot(buildScreenshot([[unopenedCell()]])),
    ).toThrow(BoardDimensionMismatchError);
    // current state unchanged
    expect(board.dimensions).toEqual({ width: 2, height: 1 });
  });

  it("updateFromScreenshot rejects an incompatible transition (revealed -> INITIAL)", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[numberCell("3"), unopenedCell()]]),
    );
    expect(() =>
      board.updateFromScreenshot(
        buildScreenshot([[unopenedCell(), unopenedCell()]]),
      ),
    ).toThrow(BoardStateIncompatibleError);
    // current state unchanged
    expect(board.state.grid).toEqual([["3", "INITIAL"]]);
  });

  it("updateFromScreenshot keeps a revealed number stable", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[numberCell("3"), unopenedCell()]]),
    );
    const state = board.updateFromScreenshot(
      buildScreenshot([[numberCell("3"), numberCell("2")]]),
    );
    expect(state.grid).toEqual([["3", "2"]]);
  });

  it("renderText produces the compact board format", () => {
    const board = SaoleiBoard.init(
      buildScreenshot([[numberCell("1"), blankCell()]]),
    );
    expect(board.renderText()).toBe(
      "board size 2*1\n\n     col0 col1\nrow0    1    0",
    );
  });
});

describe("recognizeBoard end-to-end render", () => {
  it("renders a recognizable board as the text format", () => {
    const grid = [[unopenedCell(), blankCell(), numberCell("2")]];
    const png = buildScreenshot(grid);
    const { state } = recognizeBoard(png, { width: 3, height: 1 });
    expect(renderBoardText(state)).toBe(
      "board size 3*1\n\n     col0 col1 col2\nrow0    *    0    2",
    );
  });
});
