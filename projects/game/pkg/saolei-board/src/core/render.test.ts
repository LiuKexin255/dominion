import { describe, expect, it } from "vitest";

import { cellSymbol, renderBoardText, renderGridWithRuler } from "./render";
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

describe("renderGridWithRuler", () => {
  // Format source of truth:
  // specs/029-saolei-coord-remain/contracts/saolei-board-render-contract.md §2.

  it("renders tagged col<N>/row<N> labels with columnWidth 4 for ≤9-index boards", () => {
    // maxIndex = max(3-1, 1-1) = 2 → "col2"/"row2" = 4 chars → columnWidth 4.
    // Header's first slot is blank (the row-index column); cells right-align to 4.
    const out = renderGridWithRuler(3, 1, (x) => ["1", "0", "2"][x] ?? "?");
    expect(out).toBe("     col0 col1 col2\nrow0    1    0    2");
  });

  it("renders an empty first header slot and tags every index", () => {
    // columnWidth 4: the blank header slot pads to "    " then a join space
    // precedes "col0"; the data row leads with the "row0" label.
    const out = renderGridWithRuler(2, 1, () => "*");
    const [header, row0] = out.split("\n");
    expect(header).toBe("     col0 col1");
    expect(row0).toBe("row0    *    *");
  });

  it("uses columnWidth 5 when the max index is ≥10 (2-digit col/row labels)", () => {
    // maxIndex = max(11-1, 0-1) = 10 → "col10" = 5 chars → columnWidth 5.
    // Single-digit col labels get a leading space (" col0"); col10 fills the slot.
    const out = renderGridWithRuler(11, 0, () => "*");
    expect(out).toBe(
      "       col0  col1  col2  col3  col4  col5  col6  col7  col8  col9 col10",
    );
  });

  it("returns only the header row when height=0", () => {
    const out = renderGridWithRuler(2, 0, () => "*");
    expect(out).toBe("     col0 col1");
    expect(out).not.toContain("\n");
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
    expect(lines[2]).toBe("     col0 col1 col2");
    expect(lines[3]).toBe("row0    *    0    1");
    expect(lines[4]).toBe("row1    F    X    M");
  });
});
