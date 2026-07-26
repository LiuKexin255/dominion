import { describe, expect, it } from "vitest";

import { classifyCell } from "./classify";
import {
  blankCell,
  flagCell,
  hitMineCell,
  mineCell,
  numberCell,
  CELL,
  unopenedCell,
} from "./test-helpers";

describe("classifyCell", () => {
  it("classifies an unopened beveled cell as INITIAL", () => {
    const { status } = classifyCell(unopenedCell(), CELL, CELL);
    expect(status).toBe("INITIAL");
  });

  it("classifies a flat grey revealed cell as 0", () => {
    const { status } = classifyCell(blankCell(), CELL, CELL);
    expect(status).toBe("0");
  });

  it("classifies a flagged cell as FLAG", () => {
    const { status } = classifyCell(flagCell(), CELL, CELL);
    expect(status).toBe("FLAG");
  });

  it.each(["1", "2", "3", "4", "5", "6", "7", "8"] as const)(
    "classifies a revealed number-%s cell by its reference colour",
    (n) => {
      const { status } = classifyCell(numberCell(n), CELL, CELL);
      expect(status).toBe(n);
    },
  );

  it("classifies a dense black blob on grey as MINE", () => {
    const { status } = classifyCell(mineCell(), CELL, CELL);
    expect(status).toBe("MINE");
  });

  it("classifies a dense black blob on red as HIT_MINE", () => {
    const { status } = classifyCell(hitMineCell(), CELL, CELL);
    expect(status).toBe("HIT_MINE");
  });

  it("emits diagnostics when requested", () => {
    const { diagnostics } = classifyCell(numberCell("3"), CELL, CELL);
    expect(diagnostics).toBeDefined();
    expect(diagnostics?.winnerRef).toBe("3");
    expect(diagnostics?.glyphPixels).toBeGreaterThan(0);
  });
});
