import { describe, expect, it } from "vitest";

import { cellSymbol, renderBoardText } from "./render";
import type { GameState } from "./types";

function state(grid: GameState["grid"]): GameState {
  return { width: grid[0]?.length ?? 0, height: grid.length, grid };
}

describe("cellSymbol", () => {
  it("maps each status to its board symbol", () => {
    expect(cellSymbol("INITIAL")).toBe("*");
    expect(cellSymbol("0")).toBe("0");
    expect(cellSymbol("8")).toBe("8");
    expect(cellSymbol("FLAG")).toBe("F");
    expect(cellSymbol("HIT_MINE")).toBe("X");
    expect(cellSymbol("MINE")).toBe("M");
    expect(cellSymbol("UNKNOWN")).toBe("?");
  });
});

describe("renderBoardText", () => {
  it("renders a header line with width*height and a blank separator", () => {
    const out = renderBoardText({
      width: 9,
      height: 9,
      grid: [["INITIAL"]],
    });
    expect(out.startsWith("board size 9*9\n\n")).toBe(true);
  });

  it("renders each row space-separated", () => {
    const out = renderBoardText(
      state([
        ["INITIAL", "0", "1"],
        ["FLAG", "HIT_MINE", "MINE"],
      ]),
    );
    const lines = out.split("\n");
    expect(lines[0]).toBe("board size 3*2");
    expect(lines[2]).toBe("* 0 1");
    expect(lines[3]).toBe("F X M");
  });
});
